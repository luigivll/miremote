// Package config gestiona la configuración persistente del agente y las rutas
// por sistema operativo (punto 2 del diseño).
//
// Rutas de datos:
//   Windows: %ProgramData%\MiRemote\
//   macOS:   ~/Library/Application Support/MiRemote/
//   Linux:   /var/lib/miremote (servicio) o ~/.local/share/miremote (usuario)
//
// La protección de secretos (envolver antes de escribir) se delega en
// protect/unprotect, que en Windows usan DPAPI (ver dpapi_windows.go) y en el
// resto escriben con permisos restrictivos.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	SchemaVersion       int      `json:"schema_version"`
	DeviceID            string   `json:"device_id"`
	SignalingURL        string   `json:"signaling_url"`       // https://...
	SignalingWSURL      string   `json:"signaling_ws_url"`    // wss://...
	RequireLocalApproval bool    `json:"require_local_approval"`
	ShowSessionIndicator bool    `json:"show_session_indicator"`
	DefaultPermissions  []string `json:"default_permissions"`

	// Vídeo.
	Width   int `json:"width"`
	Height  int `json:"height"`
	FPS     int `json:"fps"`
	Bitrate int `json:"bitrate"`
}

func Default() *Config {
	return &Config{
		SchemaVersion:        1,
		RequireLocalApproval: true,
		ShowSessionIndicator: true,
		DefaultPermissions:   []string{"screen.view", "input.control"},
		Width:                1920,
		Height:               1080,
		FPS:                  30,
		Bitrate:              6_000_000,
	}
}

// DataDir devuelve el directorio de datos persistente según el SO.
// `service` indica si corre como servicio del sistema (true) o como usuario.
func DataDir(service bool) string {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "MiRemote")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "MiRemote")
	default: // linux y otros
		if service {
			return "/var/lib/miremote"
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "miremote")
	}
}

func configPath(dir string) string { return filepath.Join(dir, "config.json") }

// Load lee la configuración de `dir`, devolviendo Default si no existe.
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(configPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Save escribe la configuración de forma atómica.
func (c *Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configPath(dir))
}
