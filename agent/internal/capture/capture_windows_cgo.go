//go:build windows && cgo

// Puente cgo hacia el componente nativo winshot (DXGI + codificación hardware).
// Compilar con CGO_ENABLED=1 y las bibliotecas de Windows enlazadas.
package capture

/*
#cgo CFLAGS: -I${SRCDIR}/../../native/winshot
#cgo LDFLAGS: -L${SRCDIR}/../../native/winshot -lwinshot -ld3d11 -ldxgi -lmfplat -lmfuuid -lmfreadwrite -lole32
#include "winshot.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

type cgoSource struct {
	ctx      *C.winshot_ctx
	fps      int
	duration time.Duration
	mu       sync.Mutex
	closed   bool
}

func New(width, height, fps int) (mrwebrtc.VideoSource, error) {
	bitrate := 6_000_000
	c := C.winshot_create(C.int(width), C.int(height), C.int(fps), C.int(bitrate))
	if c == nil {
		return nil, fmt.Errorf("winshot_create falló (¿DXGI/MF no disponibles?)")
	}
	return &cgoSource{
		ctx:      c,
		fps:      fps,
		duration: time.Second / time.Duration(maxIntC(fps, 1)),
	}, nil
}

func (s *cgoSource) Next(ctx context.Context) (mrwebrtc.EncodedFrame, error) {
	for {
		select {
		case <-ctx.Done():
			return mrwebrtc.EncodedFrame{}, ctx.Err()
		default:
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return mrwebrtc.EncodedFrame{}, context.Canceled
		}
		var buf *C.uint8_t
		var length C.int
		var keyframe C.int
		rc := C.winshot_next_frame(s.ctx, &buf, &length, &keyframe, C.int(100))
		s.mu.Unlock()

		switch {
		case rc == 1:
			// timeout: escritorio sin cambios, seguir esperando
			continue
		case rc < 0:
			return mrwebrtc.EncodedFrame{}, fmt.Errorf("winshot_next_frame rc=%d", int(rc))
		}
		if length == 0 || buf == nil {
			continue
		}
		data := C.GoBytes(unsafe.Pointer(buf), length)
		return mrwebrtc.EncodedFrame{
			Data:     data,
			Duration: s.duration,
			Keyframe: keyframe == 1,
		}, nil
	}
}

func (s *cgoSource) RequestKeyframe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	C.winshot_request_keyframe(s.ctx)
	return nil
}

func (s *cgoSource) SetBitrate(bps int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	C.winshot_set_bitrate(s.ctx, C.int(bps))
	return nil
}

func (s *cgoSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	C.winshot_destroy(s.ctx)
	s.ctx = nil
	return nil
}

func maxIntC(a, b int) int {
	if a > b {
		return a
	}
	return b
}
