package api

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"

	"example.com/miremote/signaling/internal/ws"
)

// GET /v1/ws — upgrade a WebSocket. Autentica con el primer mensaje `hello`,
// que contiene device_id + nonce + firma Ed25519 del nonce.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	s.hub.Accept(w, r, ws.Options{
		AllowedOrigins: s.cfg.AllowedOrigins,
		Authenticate: func(ctx context.Context, h ws.HelloData) (string, error) {
			dev, err := s.store.GetDevice(ctx, h.DeviceID)
			if err != nil {
				return "", errors.New("dispositivo desconocido")
			}
			if !verifySignature(ed25519.PublicKey(dev.PublicKey), []byte(h.Nonce), h.Signature) {
				return "", errors.New("firma inválida")
			}
			if err := s.store.ConsumeNonce(ctx, h.Nonce, s.cfg.NonceTTL); err != nil {
				return "", err
			}
			return dev.DeviceID, nil
		},
	})
}
