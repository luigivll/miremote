package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"example.com/miremote/signaling/internal/pairing"
	"example.com/miremote/signaling/internal/store"
	"example.com/miremote/signaling/internal/ws"
)

// POST /v1/pairing/invitations  (host autenticado)
// Crea una invitación y devuelve el secreto (una sola vez) para el QR.
func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	host, err := s.verifyDeviceAuth(r.Context(), r.Header.Get("X-Device-Auth"))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if host.Role != "host" {
		writeErr(w, http.StatusForbidden, "solo un host puede crear invitaciones")
		return
	}

	raw, digest, err := pairing.NewSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "no se pudo generar el secreto")
		return
	}
	pairingID := pairing.NewPairingID()
	pollToken := pairing.NewPollToken()
	expires := time.Now().Add(s.cfg.InvitationTTL)

	inv := store.Invitation{
		PairingID:  pairingID,
		HostID:     host.DeviceID,
		SecretHash: digest,
		PollToken:  pollToken,
		ExpiresAt:  expires,
	}
	if err := s.store.CreateInvitation(r.Context(), inv); err != nil {
		writeErr(w, http.StatusInternalServerError, "no se pudo crear la invitación")
		return
	}

	hostFP := "sha256:" + hex.EncodeToString(func() []byte { h := sha256.Sum256(host.PublicKey); return h[:] }())

	writeJSON(w, http.StatusCreated, map[string]any{
		"pairing_id": pairingID,
		"secret":     raw, // única vez que se expone
		"poll_token": pollToken,
		"expires_at": expires.UTC().Format(time.RFC3339),
		"host_fingerprint": hostFP,
	})
}

type claimReq struct {
	PairingID string `json:"pairing_id"`
	Secret    string `json:"secret"`
	Device    struct {
		DeviceID  string `json:"device_id"`
		PublicKey string `json:"public_key"` // base64 Ed25519
		Name      string `json:"name"`
		Platform  string `json:"platform"`
	} `json:"device"`
}

// POST /v1/pairing/claim  (no autenticado: el secreto es la prueba)
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var req claimReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.Device.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		writeErr(w, http.StatusBadRequest, "clave pública inválida")
		return
	}
	// El device_id debe derivar de la clave pública: evita suplantar un id ajeno.
	if req.Device.DeviceID != deviceIDFromPub(pub, "mobile") {
		writeErr(w, http.StatusBadRequest, "device_id no coincide con la clave pública")
		return
	}

	digest := pairing.HashSecret(req.Secret)
	inv, err := s.store.ClaimInvitation(r.Context(), req.PairingID, digest,
		req.Device.DeviceID, pub, req.Device.Name, req.Device.Platform)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrBadSecret), errors.Is(err, store.ErrNotFound):
			// Mismo mensaje para no distinguir "no existe" de "secreto malo".
			writeErr(w, http.StatusUnauthorized, "invitación o secreto inválidos")
		case errors.Is(err, store.ErrAlreadyClaim):
			writeErr(w, http.StatusConflict, "invitación ya usada o caducada")
		default:
			writeErr(w, http.StatusInternalServerError, "error al reclamar")
		}
		return
	}

	host, _ := s.store.GetDevice(r.Context(), inv.HostID)
	hostFP := ""
	if host != nil {
		h := sha256.Sum256(host.PublicKey)
		hostFP = "sha256:" + hex.EncodeToString(h[:])
	}

	// Avisar al host por WS (si está en línea) de que hay una solicitud pendiente.
	claimData, _ := json.Marshal(map[string]any{
		"pairing_id":  inv.PairingID,
		"device_id":   inv.MobileID,
		"name":        inv.MobileName,
		"platform":    inv.MobilePlat,
		"fingerprint": func() string { h := sha256.Sum256(inv.MobilePub); return "sha256:" + hex.EncodeToString(h[:]) }(),
	})
	s.hub.SystemNotify(r.Context(), inv.HostID, ws.Envelope{
		Type: "pairing-claimed", Data: claimData,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "pending_host_approval",
		"host_fingerprint": hostFP,
	})
}

// GET /v1/pairing/invitations/{pairing_id}?poll_token=...
func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	pairingID := r.PathValue("pairing_id")
	pollToken := r.URL.Query().Get("poll_token")

	inv, err := s.store.GetInvitation(r.Context(), pairingID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "invitación no encontrada")
		return
	}
	if pollToken == "" || pollToken != inv.PollToken {
		writeErr(w, http.StatusUnauthorized, "poll_token inválido")
		return
	}

	resp := map[string]any{"status": inv.Status}
	if inv.Status == "claimed" {
		h := sha256.Sum256(inv.MobilePub)
		resp["mobile"] = map[string]any{
			"device_id":   inv.MobileID,
			"name":        inv.MobileName,
			"platform":    inv.MobilePlat,
			"fingerprint": "sha256:" + hex.EncodeToString(h[:]),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type decisionReq struct {
	Decision    string   `json:"decision"` // approve | reject
	Permissions []string `json:"permissions"`
}

// POST /v1/pairing/invitations/{pairing_id}/decision  (host autenticado)
func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	host, err := s.verifyDeviceAuth(r.Context(), r.Header.Get("X-Device-Auth"))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	pairingID := r.PathValue("pairing_id")
	var req decisionReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	if req.Decision != "approve" && req.Decision != "reject" {
		writeErr(w, http.StatusBadRequest, "decisión inválida")
		return
	}
	if len(req.Permissions) == 0 {
		req.Permissions = []string{"screen.view", "input.control"}
	}

	if err := s.store.DecideInvitation(r.Context(), pairingID, host.DeviceID,
		req.Decision, req.Permissions); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Decision + "d"})
}
