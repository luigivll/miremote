// Package config carga la configuración del relevo WoL desde un fichero JSON.
// El fichero define qué dispositivos (PCs) se pueden despertar y qué móviles
// están autorizados a pedirlo.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Device struct {
	ID        string `json:"id"`        // identificador lógico, p.ej. "pc-salon"
	MAC       string `json:"mac"`       // MAC Ethernet del PC a despertar
	Broadcast string `json:"broadcast"` // broadcast de la LAN, p.ej. 192.168.1.255
	Port      int    `json:"port"`      // normalmente 9 o 7
}

type AuthorizedMobile struct {
	DeviceID  string `json:"device_id"`  // device_id del móvil (m-...)
	PublicKey string `json:"public_key"` // base64 Ed25519
	Name      string `json:"name"`
}

type Config struct {
	ListenAddr string `json:"listen_addr"` // p.ej. ":9797" (solo LAN)

	// TLS opcional. Si se omite, el relevo solo debe exponerse en LAN de confianza.
	TLSCert string `json:"tls_cert"`
	TLSKey  string `json:"tls_key"`

	// Límite de frecuencia por móvil (segundos entre peticiones aceptadas).
	RateLimitSeconds int `json:"rate_limit_seconds"`

	Devices          []Device           `json:"devices"`
	AuthorizedMobiles []AuthorizedMobile `json:"authorized_mobiles"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("json inválido en %s: %w", path, err)
	}
	if c.ListenAddr == "" {
		c.ListenAddr = ":9797"
	}
	if c.RateLimitSeconds == 0 {
		c.RateLimitSeconds = 10
	}
	if len(c.Devices) == 0 {
		return nil, fmt.Errorf("no hay dispositivos configurados")
	}
	return &c, nil
}

func (c *Config) DeviceByID(id string) (*Device, bool) {
	for i := range c.Devices {
		if c.Devices[i].ID == id {
			return &c.Devices[i], true
		}
	}
	return nil, false
}

func (c *Config) MobileByID(id string) (*AuthorizedMobile, bool) {
	for i := range c.AuthorizedMobiles {
		if c.AuthorizedMobiles[i].DeviceID == id {
			return &c.AuthorizedMobiles[i], true
		}
	}
	return nil, false
}
