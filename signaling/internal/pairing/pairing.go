// Package pairing genera los secretos de un solo uso del QR y el payload del
// propio QR.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

// NewSecret genera el secreto de arranque. `raw` va dentro del QR y se entrega
// una sola vez al creador; `digest` es lo único que persiste el servidor.
func NewSecret() (raw string, digest []byte, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	d := sha256.Sum256([]byte(raw))
	return raw, d[:], nil
}

// HashSecret recalcula el digest de un secreto recibido para compararlo.
func HashSecret(raw string) []byte {
	d := sha256.Sum256([]byte(raw))
	return d[:]
}

// NewPairingID genera un identificador público de invitación.
func NewPairingID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// NewPollToken genera el token opaco con el que el PC sondea el estado.
func NewPollToken() string {
	buf := make([]byte, 18)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// QRPayload es el contenido JSON que se codifica en el QR.
type QRPayload struct {
	Version         int    `json:"version"`
	Server          string `json:"server"`
	PairingID       string `json:"pairing_id"`
	Secret          string `json:"secret"`
	HostFingerprint string `json:"host_key_fingerprint"`
	ExpiresAt       string `json:"expires_at"`
}

func BuildQRPayload(server, pairingID, secret, hostFP string, expires time.Time) (string, error) {
	p := QRPayload{
		Version:         1,
		Server:          server,
		PairingID:       pairingID,
		Secret:          secret,
		HostFingerprint: hostFP,
		ExpiresAt:       expires.UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(p)
	return string(b), err
}
