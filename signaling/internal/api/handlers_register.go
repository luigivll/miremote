package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"

	"example.com/miremote/signaling/internal/store"
)

type registerHostReq struct {
	PublicKey string `json:"public_key"` // base64 Ed25519
	Name      string `json:"name"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"` // firma del nonce con la clave privada
}

// POST /v1/hosts/register
// Registra (o reconfirma) la identidad de un PC. La autorización es la posesión
// de la clave privada: se exige firmar un nonce válido. El device_id se deriva
// de la clave pública, de modo que nadie puede registrar un id que no controla.
func (s *Server) handleRegisterHost(w http.ResponseWriter, r *http.Request) {
	var req registerHostReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "json inválido")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		writeErr(w, http.StatusBadRequest, "clave pública inválida")
		return
	}
	if !verifySignature(ed25519.PublicKey(pub), []byte(req.Nonce), req.Signature) {
		writeErr(w, http.StatusUnauthorized, "firma inválida")
		return
	}
	if err := s.store.ConsumeNonce(r.Context(), req.Nonce, s.cfg.NonceTTL); err != nil {
		writeErr(w, http.StatusUnauthorized, "nonce inválido o repetido")
		return
	}

	deviceID := deviceIDFromPub(pub, "host")
	dev := store.Device{
		DeviceID:  deviceID,
		PublicKey: pub,
		Name:      req.Name,
		Platform:  "windows",
		Role:      "host",
	}
	if err := s.store.UpsertDevice(r.Context(), dev); err != nil {
		writeErr(w, http.StatusInternalServerError, "no se pudo registrar")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"device_id": deviceID})
}
