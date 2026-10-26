package api

// JWT HS256 autocontenido para evitar dependencias externas. Solo se usan los
// tokens de sesión cortos emitidos por este servidor, así que basta con lo
// mínimo: firmar y verificar HS256 con comparación en tiempo constante.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrTokenInvalid = errors.New("token inválido")
var ErrTokenExpired = errors.New("token caducado")

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func signJWT(claims sessionClaims, secret []byte) (string, error) {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := b64(payloadBytes)
	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := b64(mac.Sum(nil))
	return signingInput + "." + sig, nil
}

// parseJWT verifica firma y caducidad y devuelve los claims. También lo usa el
// agente de PC (copia equivalente) para validar el token de sesión.
func parseJWT(token string, secret []byte) (*sessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrTokenInvalid
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, got) {
		return nil, ErrTokenInvalid
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenInvalid
	}
	var claims sessionClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrTokenInvalid
	}
	if time.Now().Unix() > claims.Exp {
		return nil, ErrTokenExpired
	}
	return &claims, nil
}
