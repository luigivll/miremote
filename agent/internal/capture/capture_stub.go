//go:build !windows

// Fuente de vídeo de desarrollo (no-Windows). No captura pantalla real: emite
// Annex-B H.264 a un ritmo fijo si se le proporciona un fichero .h264 de
// muestra por la variable de entorno MR_TEST_H264; en su defecto, emite marcos
// vacíos marcados, suficiente para ejercitar la tubería Pion (WriteSample,
// keyframe request, bitrate) sin hardware.
//
// La captura real vive en capture_windows.go (DXGI + codificación hardware).
package capture

import (
	"context"
	"os"
	"sync"
	"time"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

type StubSource struct {
	mu           sync.Mutex
	fps          int
	wantKeyframe bool
	bitrate      int
	frames       [][]byte // marcos pregrabados opcionales
	idx          int
	count        int
	closed       bool
}

func New(width, height, fps int) (mrwebrtc.VideoSource, error) {
	s := &StubSource{fps: fps, bitrate: 2_000_000}
	if path := os.Getenv("MR_TEST_H264"); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			s.frames = splitAnnexB(data)
		}
	}
	return s, nil
}

func (s *StubSource) Next(ctx context.Context) (mrwebrtc.EncodedFrame, error) {
	interval := time.Second / time.Duration(maxInt(s.fps, 1))
	select {
	case <-ctx.Done():
		return mrwebrtc.EncodedFrame{}, ctx.Err()
	case <-time.After(interval):
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return mrwebrtc.EncodedFrame{}, context.Canceled
	}

	var data []byte
	keyframe := s.wantKeyframe || s.count == 0 // primer frame siempre IDR
	s.wantKeyframe = false
	s.count++
	if len(s.frames) > 0 {
		data = s.frames[s.idx%len(s.frames)]
		s.idx++
	} else {
		// Marco de relleno: un NAL Annex-B con header de slice (tipo 1,
		// non-IDR) y algo de carga. No es decodificable como imagen real, pero
		// es un NAL válido que el empaquetador H.264 de Pion sí convierte en
		// RTP, de modo que la tubería (WriteSample -> pista -> receptor) se
		// ejercita de verdad. En keyframe se marca como tipo 5 (IDR).
		nalType := byte(0x61) // forbidden_zero=0, nri=3, type=1 (non-IDR)
		if keyframe {
			nalType = 0x65 // type=5 (IDR)
		}
		data = []byte{0x00, 0x00, 0x00, 0x01, nalType,
			0x88, 0x84, 0x00, 0x33, 0xff, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32}
	}
	return mrwebrtc.EncodedFrame{Data: data, Duration: interval, Keyframe: keyframe}, nil
}

func (s *StubSource) RequestKeyframe() error {
	s.mu.Lock()
	s.wantKeyframe = true
	s.mu.Unlock()
	return nil
}

func (s *StubSource) SetBitrate(bps int) error {
	s.mu.Lock()
	s.bitrate = bps
	s.mu.Unlock()
	return nil
}

func (s *StubSource) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// splitAnnexB parte un flujo Annex-B en unidades de acceso por el start code
// de AUD/VCL. Simplificado: parte por cada start code 00 00 00 01.
func splitAnnexB(data []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i+3 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			if start >= 0 {
				out = append(out, data[start:i])
			}
			start = i
		}
	}
	if start >= 0 {
		out = append(out, data[start:])
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
