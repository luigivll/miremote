// Package turn genera credenciales temporales compatibles con el mecanismo
// use-auth-secret de coturn (REST API for TURN, draft de Uberti/Thaler).
//
// coturn, arrancado con `use-auth-secret` y `static-auth-secret=<SECRET>`,
// acepta como usuario cualquier cadena de la forma "<expiry_unix>:<texto>" y
// como contraseña base64(HMAC-SHA1(SECRET, usuario)). Las credenciales caducan
// solas: no hay cuentas fijas que mantener ni revocar.
package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strconv"
	"time"
)

type Credentials struct {
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
	TTL        int      `json:"ttl"`
	URLs       []string `json:"urls"`
}

// Generate crea credenciales TURN con caducidad. `id` suele ser el device_id
// del solicitante, para poder correlacionar en los logs de coturn.
func Generate(secret []byte, id string, ttl time.Duration, urls []string) Credentials {
	expiry := time.Now().Add(ttl).Unix()
	username := strconv.FormatInt(expiry, 10) + ":" + id

	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(username))
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return Credentials{
		Username:   username,
		Credential: credential,
		TTL:        int(ttl.Seconds()),
		URLs:       urls,
	}
}
