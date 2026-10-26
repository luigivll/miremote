// Package webrtc contiene los contratos de dominio del agente (fuente y
// consumidor de vídeo, entrada, indicador) y la sesión Pion que los une.
package webrtc

import (
	"context"
	"time"
)

// EncodedFrame es una unidad de acceso H.264 completa (no un fragmento).
type EncodedFrame struct {
	Data     []byte
	Duration time.Duration
	Keyframe bool
}

// VideoSource produce fotogramas H.264 ya codificados. La implementación real
// en Windows es la captura DXGI + codificación hardware (ver internal/capture).
type VideoSource interface {
	// Next bloquea hasta el siguiente fotograma o hasta que el contexto termine.
	Next(context.Context) (EncodedFrame, error)
	// RequestKeyframe pide al codificador un IDR cuanto antes (tras un PLI/FIR).
	RequestKeyframe() error
	// SetBitrate ajusta el bitrate objetivo (tras informes de congestión).
	SetBitrate(bitsPerSecond int) error
	Close() error
}

// InputEvent es un evento de entrada recibido del móvil. Coordenadas
// normalizadas 0..1 sobre el escritorio virtual completo.
type InputEvent struct {
	Type   string  `json:"t"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	Code   string  `json:"code"`   // tecla física, estilo DOM KeyboardEvent.code
	Button string  `json:"button"` // left | right | middle
	Value  string  `json:"value"`  // texto Unicode (t == "text")
}

// InputSink aplica eventos de entrada al SO. ReleaseAll libera todo lo que
// quedara pulsado, para no dejar teclas o botones trabados al desconectar.
type InputSink interface {
	Apply(InputEvent) error
	ReleaseAll() error
	Close() error
}

// SessionIndicator es el aviso visible de que hay una sesión remota activa.
type SessionIndicator interface {
	Show(deviceName string, permissions []string) error
	Hide() error
	// OnStop registra el callback que dispara el botón "Detener".
	OnStop(func())
}

// Permission son las capacidades concedidas a una sesión.
const (
	PermScreenView   = "screen.view"
	PermInputControl = "input.control"
)
