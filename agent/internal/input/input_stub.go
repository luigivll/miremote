//go:build !windows

// Stub de InputSink para plataformas distintas de Windows. Permite compilar y
// ejecutar el agente en Linux/macOS para desarrollo (registra los eventos en
// vez de inyectarlos). La implementación real por SO se añade por build tag.
package input

import (
	"log"
	"sync"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

type StubInput struct {
	mu          sync.Mutex
	pressedKeys map[string]bool
	pressedBtns map[string]bool
	Verbose     bool
}

func New() (mrwebrtc.InputSink, error) {
	return &StubInput{
		pressedKeys: make(map[string]bool),
		pressedBtns: make(map[string]bool),
	}, nil
}

func (s *StubInput) Apply(ev mrwebrtc.InputEvent) error {
	s.mu.Lock()
	switch ev.Type {
	case "key.down":
		s.pressedKeys[ev.Code] = true
	case "key.up":
		delete(s.pressedKeys, ev.Code)
	case "mouse.down":
		s.pressedBtns[ev.Button] = true
	case "mouse.up":
		delete(s.pressedBtns, ev.Button)
	}
	s.mu.Unlock()
	if s.Verbose {
		log.Printf("[input-stub] %+v", ev)
	}
	return nil
}

func (s *StubInput) ReleaseAll() error {
	s.mu.Lock()
	n := len(s.pressedKeys) + len(s.pressedBtns)
	s.pressedKeys = make(map[string]bool)
	s.pressedBtns = make(map[string]bool)
	s.mu.Unlock()
	if s.Verbose && n > 0 {
		log.Printf("[input-stub] ReleaseAll: liberados %d", n)
	}
	return nil
}

func (s *StubInput) Close() error { return s.ReleaseAll() }
