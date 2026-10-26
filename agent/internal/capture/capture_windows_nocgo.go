//go:build windows && !cgo

// Fallback cuando se compila para Windows SIN cgo. La captura real (DXGI +
// codificación hardware) necesita el componente nativo enlazado por cgo, así
// que aquí se devuelve un error explicativo en vez de capturar.
//
// Para producción en Windows, compilar con CGO_ENABLED=1 y la DLL nativa
// (ver native/winshot y capture_windows_cgo.go).
package capture

import (
	"context"
	"fmt"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

type noCGOSource struct{}

func New(width, height, fps int) (mrwebrtc.VideoSource, error) {
	return nil, fmt.Errorf("captura de pantalla no disponible: recompila el agente con CGO_ENABLED=1 y la DLL winshot (DXGI + codificación hardware)")
}

func (noCGOSource) Next(context.Context) (mrwebrtc.EncodedFrame, error) {
	return mrwebrtc.EncodedFrame{}, fmt.Errorf("no implementado")
}
func (noCGOSource) RequestKeyframe() error   { return nil }
func (noCGOSource) SetBitrate(int) error     { return nil }
func (noCGOSource) Close() error             { return nil }
