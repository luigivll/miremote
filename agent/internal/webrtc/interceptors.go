package webrtc

import (
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// interceptorRegistryWrap configura los interceptores por defecto de Pion, que
// incluyen NACK, RTCP reports y la generación de PLI, necesarios para que el
// bucle readRTCP reciba las solicitudes de keyframe del receptor.
type interceptorRegistryWrap struct {
	registry *interceptor.Registry
}

func (w *interceptorRegistryWrap) register(m *webrtc.MediaEngine) error {
	w.registry = &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, w.registry); err != nil {
		return err
	}
	return nil
}
