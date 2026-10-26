package api

import (
	"net/http"

	"example.com/miremote/signaling/internal/turn"
)

// POST /v1/turn/credentials  (autenticado)
func (s *Server) handleTURN(w http.ResponseWriter, r *http.Request) {
	dev, err := s.verifyDeviceAuth(r.Context(), r.Header.Get("X-Device-Auth"))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	creds := turn.Generate(s.cfg.TURNSecret, dev.DeviceID, s.cfg.TURNCredTTL, s.cfg.TURNUrls)
	writeJSON(w, http.StatusOK, creds)
}

type sessionTokenReq struct {
	TargetDeviceID string `json:"target_device_id"` // device_id del PC al que se conecta
	SessionID      string `json:"session_id"`
}

// POST /v1/session/token  (móvil autenticado)
// Emite un JWT corto con audiencia = PC destino y permisos del vínculo.
func (s *Server) handleSessionToken(w http.ResponseWriter, r *http.Request) {
	mobile, err := s.verifyDeviceAuth(r.Context(), r.Header.Get("X-Device-Auth"))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req sessionTokenReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "json inválido")
		return
	}

	link, err := s.store.GetLink(r.Context(), req.TargetDeviceID, mobile.DeviceID)
	if err != nil {
		writeErr(w, http.StatusForbidden, "sin vínculo aprobado con ese PC")
		return
	}
	if req.SessionID == "" {
		req.SessionID = newSessionID()
	}
	token, err := s.issueSessionToken(mobile.DeviceID, req.TargetDeviceID,
		req.SessionID, link.Permissions)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "no se pudo emitir el token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": token,
		"session_id":    req.SessionID,
		"permissions":   link.Permissions,
		"expires_in":    int(s.cfg.SessionTTL.Seconds()),
	})
}

func newSessionID() string {
	return randToken(16)
}
