// Package config carga la configuración del servidor de señalización desde
// variables de entorno. No hay valores secretos por defecto: el servidor se
// niega a arrancar si faltan.
package config

import (
	"errors"
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
	c := &Config{
		ListenAddr:    env("MR_LISTEN", ":8443"),
		TLSCert:       os.Getenv("MR_TLS_CERT"),
		TLSKey:        os.Getenv("MR_TLS_KEY"),
		DBPath:        env("MR_DB", "miremote-signaling.db"),
		InvitationTTL: mustDur("MR_INVITATION_TTL", 120*time.Second),
		SessionTTL:    mustDur("MR_SESSION_TTL", 5*time.Minute),
		TURNCredTTL:   mustDur("MR_TURN_TTL", 10*time.Minute),
		NonceTTL:      mustDur("MR_NONCE_TTL", 30*time.Second),
	}

	js := os.Getenv("MR_JWT_SECRET")
	if len(js) < 32 {
		return nil, errors.New("MR_JWT_SECRET requerido y de al menos 32 bytes")
	}
	c.JWTSecret = []byte(js)

	ts := os.Getenv("MR_TURN_SECRET")
	if len(ts) < 16 {
		return nil, errors.New("MR_TURN_SECRET requerido y de al menos 16 bytes")
	}
	c.TURNSecret = []byte(ts)

	turnUrls := os.Getenv("MR_TURN_URLS")
	if turnUrls == "" {
		return nil, errors.New("MR_TURN_URLS requerido (CSV de urls turn:/turns:)")
	}
	c.TURNUrls = splitCSV(turnUrls)

	if o := os.Getenv("MR_ALLOWED_ORIGINS"); o != "" {
		c.AllowedOrigins = splitCSV(o)
	}

	return c, nil
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
