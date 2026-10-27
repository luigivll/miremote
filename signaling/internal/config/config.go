// Package config carga la configuración del servidor de señalización desde
// variables de entorno. Arranca sin configuración: si no se da MR_JWT_SECRET,
// genera y persiste uno; TURN es opcional (sin él se usa solo STUN).
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Dirección de escucha, p.ej. ":8443".
	ListenAddr string

	// Rutas del certificado TLS. Si están vacías, se asume terminación TLS
	// en un proxy delante (nginx/caddy) y el servidor habla HTTP en texto
	// claro SOLO sobre localhost.
	TLSCert string
	TLSKey  string

	// Secreto HMAC para firmar JWT de sesión (HS256). >= 32 bytes.
	JWTSecret []byte

	// Secreto compartido con coturn (use-auth-secret). >= 16 bytes.
	TURNSecret []byte
	TURNUrls   []string

	// Vida de las credenciales e invitaciones.
	InvitationTTL time.Duration
	SessionTTL    time.Duration
	TURNCredTTL   time.Duration
	NonceTTL      time.Duration

	// Ruta del fichero SQLite.
	DBPath string

	// Orígenes permitidos para WebSocket (CSV). Vacío = cualquiera (dev).
	AllowedOrigins []string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() (*Config, error) {
	// Puerto: MR_LISTEN tiene prioridad; si no, se usa $PORT (lo fija el hosting
	// en la nube, p.ej. Cloud Run); si no, :8443.
	listen := os.Getenv("MR_LISTEN")
	if listen == "" {
		if p := os.Getenv("PORT"); p != "" {
			listen = ":" + p
		} else {
			listen = ":8443"
		}
	}
	c := &Config{
		ListenAddr:    listen,
		TLSCert:       os.Getenv("MR_TLS_CERT"),
		TLSKey:        os.Getenv("MR_TLS_KEY"),
		DBPath:        env("MR_DB", "miremote-signaling.db"),
		InvitationTTL: mustDur("MR_INVITATION_TTL", 120*time.Second),
		SessionTTL:    mustDur("MR_SESSION_TTL", 5*time.Minute),
		TURNCredTTL:   mustDur("MR_TURN_TTL", 10*time.Minute),
		NonceTTL:      mustDur("MR_NONCE_TTL", 30*time.Second),
	}

	// Secreto JWT: si no se da, se genera y persiste junto a la BD, de modo que
	// el servidor arranca sin configuración y conserva el secreto entre reinicios.
	js := os.Getenv("MR_JWT_SECRET")
	if len(js) >= 32 {
		c.JWTSecret = []byte(js)
	} else {
		secret, err := loadOrCreateSecret(c.DBPath + ".jwt")
		if err != nil {
			return nil, fmt.Errorf("no se pudo preparar el secreto JWT: %w", err)
		}
		c.JWTSecret = secret
	}

	// TURN opcional. Sin TURN, se usa solo STUN (funciona en muchas redes
	// domésticas; detrás de NAT simétrico haría falta un TURN, ver deploy/).
	ts := os.Getenv("MR_TURN_SECRET")
	if len(ts) >= 16 {
		c.TURNSecret = []byte(ts)
	}
	if turnUrls := os.Getenv("MR_TURN_URLS"); turnUrls != "" {
		c.TURNUrls = splitCSV(turnUrls)
	}

	if o := os.Getenv("MR_ALLOWED_ORIGINS"); o != "" {
		c.AllowedOrigins = splitCSV(o)
	}

	return c, nil
}

// loadOrCreateSecret lee un secreto de 32 bytes del fichero dado o lo crea.
func loadOrCreateSecret(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	// Persistir en base64 legible; ignorar error de escritura (efímero si falla).
	enc := []byte(base64.StdEncoding.EncodeToString(buf))
	_ = os.WriteFile(path, enc, 0o600)
	return enc, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func mustDur(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	// Permite "120s", "5m" o número en segundos.
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	fmt.Fprintf(os.Stderr, "aviso: %s inválido (%q), usando %s\n", key, v, def)
	return def
}
