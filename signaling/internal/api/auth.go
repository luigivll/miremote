package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"example.com/miremote/signaling/internal/store"
)

// deviceIDFromPub deriva el device_id de una clave pública Ed25519.
// Debe coincidir con la lógica del agente y del móvil.
func deviceIDFromPub(pub ed25519.PublicKey, role string) string {
	sum := sha256.Sum256(pub)
	id := base64.RawURLEncoding.EncodeToString(sum[:])[:16]
	switch role {
	case "host":
		return "p-" + id[2:]
	default:
		return "m-" + id[2:]
	}
}

// issueNonce genera un nonce aleatorio de un solo uso, aún no consumido.
func (s *Server) issueNonce() string {
	return randToken(24)
}

func randToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// verifyDeviceAuth valida una cabecera "X-Device-Auth: v1 <id> <nonce> <sig>".
// Devuelve el dispositivo autenticado. Consume el nonce (anti-repetición).
func (s *Server) verifyDeviceAuth(ctx context.Context, header string) (*store.Device, error) {
	parts := strings.Fields(header)
	if len(parts) != 4 || parts[0] != "v1" {
		return nil, errors.New("formato de X-Device-Auth inválido")
	}
	deviceID, nonce, sigB64 := parts[1], parts[2], parts[3]

	dev, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, errors.New("dispositivo desconocido")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, errors.New("firma no decodificable")
	}
	if !ed25519.Verify(ed25519.PublicKey(dev.PublicKey), []byte(nonce), sig) {
		return nil, errors.New("firma inválida")
	}
	// Consumir el nonce: si ya se usó, es repetición.
	if err := s.store.ConsumeNonce(ctx, nonce, s.cfg.NonceTTL); err != nil {
		return nil, err
	}
	return dev, nil
}

// verifySignature valida una firma Ed25519 sin consumir nonce (se usa dentro
// del WS donde el nonce ya se comprueba por separado).
func verifySignature(pub ed25519.PublicKey, msg []byte, sigB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// --- JWT HS256 mínimo, sin dependencias externas ---

type sessionClaims struct {
	Sub   string   `json:"sub"`
	Aud   string   `json:"aud"`
	Sid   string   `json:"sid"`
	Perms []string `json:"perms"`
	Exp   int64    `json:"exp"`
	Iat   int64    `json:"iat"`
}

func (s *Server) issueSessionToken(sub, aud, sid string, perms []string) (string, error) {
	claims := sessionClaims{
		Sub: sub, Aud: aud, Sid: sid, Perms: perms,
		Iat: time.Now().Unix(),
		Exp: time.Now().Add(s.cfg.SessionTTL).Unix(),
	}
	return signJWT(claims, s.cfg.JWTSecret)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
