package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// Config de una sesión del agente.
type SessionConfig struct {
	Certificate webrtc.Certificate // certificado DTLS PERSISTENTE
	ICEServers  []webrtc.ICEServer // STUN/TURN
	Permissions []string           // permisos concedidos a esta sesión

	Video     VideoSource
	Input     InputSink
	Indicator SessionIndicator
	DeviceName string // nombre del móvil, para el indicador

	// SendSignal envía un mensaje de señalización (answer/ice) al móvil a
	// través del WebSocket. Lo inyecta la capa de señalización.
	SendSignal func(msgType string, data any) error

	// OnClosed se llama cuando la sesión termina por cualquier motivo.
	OnClosed func()
}

// Session representa una conexión WebRTC activa con un móvil.
type Session struct {
	cfg SessionConfig
	pc  *webrtc.PeerConnection

	videoTrack *webrtc.TrackLocalStaticSample

	mu          sync.Mutex
	controlDC   *webrtc.DataChannel
	closed      bool
	inputActive bool // true solo si se concedió input.control y la sesión está viva

	cancel context.CancelFunc
}

func hasPerm(perms []string, p string) bool {
	for _, x := range perms {
		if x == p {
			return true
		}
	}
	return false
}

// NewSession crea el PeerConnection con el certificado persistente y la pista
// de vídeo H.264, y conecta los DataChannels de control.
func NewSession(cfg SessionConfig) (*Session, error) {
	settingEngine := webrtc.SettingEngine{}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}

	// Interceptores por defecto (incluyen generación de PLI/receptores RTCP).
	ir := &interceptorRegistryWrap{}
	if err := ir.register(mediaEngine); err != nil {
		return nil, err
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithInterceptorRegistry(ir.registry),
	)

	pcCfg := webrtc.Configuration{
		ICEServers:   cfg.ICEServers,
		Certificates: []webrtc.Certificate{cfg.Certificate}, // <-- huella DTLS estable
	}

	pc, err := api.NewPeerConnection(pcCfg)
	if err != nil {
		return nil, err
	}

	s := &Session{cfg: cfg, pc: pc}

	// Pista de vídeo H.264.
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "miremote-screen")
	if err != nil {
		pc.Close()
		return nil, err
	}
	s.videoTrack = track

	rtpSender, err := pc.AddTrack(track)
	if err != nil {
		pc.Close()
		return nil, err
	}

	// Leer RTCP del sender: PLI/FIR => pedir keyframe; REMB/TWCC => ajustar bitrate.
	go s.readRTCP(rtpSender)

	// DataChannels: el móvil los crea; aquí se reciben.
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		switch dc.Label() {
		case "control":
			s.setupControlChannel(dc)
		case "cursor":
			s.setupCursorChannel(dc)
		default:
			log.Printf("datachannel desconocido: %s", dc.Label())
		}
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		_ = cfg.SendSignal("ice", c.ToJSON())
	})

	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		log.Printf("estado PC: %s", st.String())
		switch st {
		case webrtc.PeerConnectionStateConnected:
			s.onConnected()
		case webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			s.Close()
		}
	})

	return s, nil
}

// HandleOffer aplica la oferta del móvil y devuelve la answer (SDP) que la
// capa de señalización reenviará.
func (s *Session) HandleOffer(offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	if err := s.pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}
	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	if err := s.pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}
	return s.pc.LocalDescription(), nil
}

// AddICECandidate añade un candidato ICE recibido del móvil.
func (s *Session) AddICECandidate(c webrtc.ICECandidateInit) error {
	return s.pc.AddICECandidate(c)
}

// onConnected arranca el bombeo de vídeo y el indicador. El control de entrada
// se habilita solo si hay permiso y el indicador pudo mostrarse.
func (s *Session) onConnected() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Indicador visible OBLIGATORIO. Política conservadora: si no se puede
	// mostrar el aviso, no se permite el control de entrada.
	indicatorOK := true
	if s.cfg.Indicator != nil {
		if err := s.cfg.Indicator.Show(s.cfg.DeviceName, s.cfg.Permissions); err != nil {
			log.Printf("no se pudo mostrar el indicador: %v", err)
			indicatorOK = false
		}
		s.cfg.Indicator.OnStop(func() {
			log.Printf("sesión detenida por el usuario local")
			s.Close()
		})
	}

	s.mu.Lock()
	s.inputActive = indicatorOK && hasPerm(s.cfg.Permissions, PermInputControl)
	s.mu.Unlock()

	// Informar al móvil de los permisos efectivos.
	s.sendControl(map[string]any{
		"t":       "session.permissions",
		"granted": s.effectivePerms(indicatorOK),
	})

	// Arrancar vídeo.
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go s.pumpVideo(ctx)
}

func (s *Session) effectivePerms(indicatorOK bool) []string {
	var out []string
	if hasPerm(s.cfg.Permissions, PermScreenView) {
		out = append(out, PermScreenView)
	}
	if indicatorOK && hasPerm(s.cfg.Permissions, PermInputControl) {
		out = append(out, PermInputControl)
	}
	return out
}

// pumpVideo lee fotogramas de la fuente y los escribe en la pista como samples.
func (s *Session) pumpVideo(ctx context.Context) {
	if s.cfg.Video == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		frame, err := s.cfg.Video.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				return
			}
			log.Printf("fuente de vídeo: %v", err)
			return
		}
		if err := s.videoTrack.WriteSample(media.Sample{
			Data:     frame.Data,
			Duration: frame.Duration,
		}); err != nil {
			log.Printf("WriteSample: %v", err)
			return
		}
	}
}

// readRTCP procesa la realimentación del receptor.
func (s *Session) readRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		n, _, err := sender.Read(buf)
		if err != nil {
			return
		}
		pkts, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		for _, pkt := range pkts {
			switch p := pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				if s.cfg.Video != nil {
					_ = s.cfg.Video.RequestKeyframe()
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				if s.cfg.Video != nil && p.Bitrate > 0 {
					_ = s.cfg.Video.SetBitrate(int(p.Bitrate))
				}
			}
		}
	}
}

// --- DataChannels ---

func (s *Session) setupControlChannel(dc *webrtc.DataChannel) {
	s.mu.Lock()
	s.controlDC = dc
	s.mu.Unlock()

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		s.handleInputMessage(msg.Data, true)
	})
}

func (s *Session) setupCursorChannel(dc *webrtc.DataChannel) {
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		s.handleInputMessage(msg.Data, false)
	})
}

func (s *Session) handleInputMessage(data []byte, isControl bool) {
	var ev InputEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}

	// Mensajes de control de sesión (no de entrada).
	switch ev.Type {
	case "keyframe.request":
		if s.cfg.Video != nil {
			_ = s.cfg.Video.RequestKeyframe()
		}
		return
	case "session.stop":
		s.Close()
		return
	}

	// Entrada real: requiere permiso e indicador activo.
	s.mu.Lock()
	active := s.inputActive && !s.closed
	s.mu.Unlock()
	if !active {
		return
	}
	if s.cfg.Input == nil {
		return
	}
	// Validación básica de coordenadas.
	if ev.X < 0 || ev.X > 1 || ev.Y < 0 || ev.Y > 1 {
		if ev.Type == "mouse.move" || ev.Type == "mouse.down" || ev.Type == "mouse.up" {
			return
		}
	}
	if err := s.cfg.Input.Apply(ev); err != nil {
		log.Printf("aplicar entrada: %v", err)
	}
}

func (s *Session) sendControl(v any) {
	s.mu.Lock()
	dc := s.controlDC
	s.mu.Unlock()
	if dc == nil {
		return
	}
	b, _ := json.Marshal(v)
	_ = dc.SendText(string(b))
}

// Close cierra la sesión liberando entrada, ocultando el indicador y cerrando
// el PeerConnection. Idempotente.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.inputActive = false
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Liberar cualquier tecla/botón retenido.
	if s.cfg.Input != nil {
		_ = s.cfg.Input.ReleaseAll()
	}
	if s.cfg.Indicator != nil {
		_ = s.cfg.Indicator.Hide()
	}
	if s.cfg.Video != nil {
		_ = s.cfg.Video.Close()
	}
	_ = s.pc.Close()

	if s.cfg.OnClosed != nil {
		s.cfg.OnClosed()
	}
	log.Printf("sesión cerrada")
}

// StopFromLocal lo invoca el indicador local (botón Detener) o el servicio.
func (s *Session) StopFromLocal() {
	s.sendControl(map[string]any{"t": "session.stop", "reason": "user_local"})
	time.Sleep(50 * time.Millisecond)
	s.Close()
}
