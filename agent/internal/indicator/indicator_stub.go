//go:build !windows

// Indicador de sesión para desarrollo (no-Windows): registra en log en vez de
// mostrar un icono de bandeja. La versión Windows usa el systray real.
package indicator

import (
	"log"
	"sync"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

type StubIndicator struct {
	mu     sync.Mutex
	onStop func()
}

func New() (mrwebrtc.SessionIndicator, error) {
	return &StubIndicator{}, nil
}

func (s *StubIndicator) Show(deviceName string, perms []string) error {
	log.Printf("[INDICADOR] ● Sesión remota ACTIVA — %s — permisos: %v", deviceName, perms)
	return nil
}

func (s *StubIndicator) Hide() error {
	log.Printf("[INDICADOR] ○ Sesión remota finalizada")
	return nil
}

func (s *StubIndicator) OnStop(f func()) {
	s.mu.Lock()
	s.onStop = f
	s.mu.Unlock()
}

// TriggerStop simula pulsar "Detener" (útil en pruebas).
func (s *StubIndicator) TriggerStop() {
	s.mu.Lock()
	f := s.onStop
	s.mu.Unlock()
	if f != nil {
		f()
	}
}
