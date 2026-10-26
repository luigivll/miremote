package webrtc_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"example.com/miremote/agent/internal/capture"
	"example.com/miremote/agent/internal/indicator"
	"example.com/miremote/agent/internal/input"
	mrwebrtc "example.com/miremote/agent/internal/webrtc"

	pion "github.com/pion/webrtc/v4"
)

// fakeInput cuenta eventos aplicados y registra ReleaseAll.
type fakeInput struct {
	mu          sync.Mutex
	applied     int
	releaseAll  int
	lastEvent   mrwebrtc.InputEvent
}

func (f *fakeInput) Apply(ev mrwebrtc.InputEvent) error {
	f.mu.Lock()
	f.applied++
	f.lastEvent = ev
	f.mu.Unlock()
	return nil
}
func (f *fakeInput) ReleaseAll() error { f.mu.Lock(); f.releaseAll++; f.mu.Unlock(); return nil }
func (f *fakeInput) Close() error      { return f.ReleaseAll() }

// TestEndToEndSession monta agente (Session) + móvil (PeerConnection Pion) y
// verifica: negociación SDP, recepción de la pista de vídeo, y que un evento de
// entrada enviado por el DataChannel de control llega al InputSink.
func TestEndToEndSession(t *testing.T) {
	// Certificado persistente del agente (ECDSA P-256, como en identity).
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := pion.GenerateCertificate(priv)
	if err != nil {
		t.Fatal(err)
	}

	vid, _ := capture.New(640, 480, 15)
	fin := &fakeInput{}
	ind, _ := indicator.New()

	// Canal para pasar la answer del agente al móvil.
	answerCh := make(chan pion.SessionDescription, 1)
	iceToMobile := make(chan pion.ICECandidateInit, 32)

	sess, err := mrwebrtc.NewSession(mrwebrtc.SessionConfig{
		Certificate: *cert,
		Permissions: []string{mrwebrtc.PermScreenView, mrwebrtc.PermInputControl},
		Video:       vid,
		Input:       fin,
		Indicator:   ind,
		DeviceName:  "móvil-test",
		SendSignal: func(msgType string, data any) error {
			switch msgType {
			case "answer":
				sd := data.(*pion.SessionDescription)
				answerCh <- *sd
			case "ice":
				b, _ := json.Marshal(data)
				var c pion.ICECandidateInit
				json.Unmarshal(b, &c)
				select {
				case iceToMobile <- c:
				default:
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// --- Lado móvil ---
	mobile, err := pion.NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer mobile.Close()

	gotTrack := make(chan struct{}, 1)
	mobile.OnTrack(func(tr *pion.TrackRemote, _ *pion.RTPReceiver) {
		select {
		case gotTrack <- struct{}{}:
		default:
		}
		// Consumir algunos paquetes.
		buf := make([]byte, 1500)
		for i := 0; i < 3; i++ {
			if _, _, err := tr.Read(buf); err != nil {
				return
			}
		}
	})

	// El móvil declara que quiere recibir vídeo (recvonly).
	if _, err := mobile.AddTransceiverFromKind(pion.RTPCodecTypeVideo,
		pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}

	control, err := mobile.CreateDataChannel("control", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mobile.CreateDataChannel("cursor", &pion.DataChannelInit{
		Ordered: boolPtr(false), MaxRetransmits: u16Ptr(0),
	})
	if err != nil {
		t.Fatal(err)
	}

	controlOpen := make(chan struct{}, 1)
	control.OnOpen(func() { controlOpen <- struct{}{} })

	// ICE del móvil hacia el agente.
	mobile.OnICECandidate(func(c *pion.ICECandidate) {
		if c == nil {
			return
		}
		_ = sess.AddICECandidate(c.ToJSON())
	})
	// ICE del agente hacia el móvil.
	go func() {
		for c := range iceToMobile {
			_ = mobile.AddICECandidate(c)
		}
	}()

	// Oferta del móvil.
	offer, err := mobile.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mobile.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	// El agente procesa la oferta y produce la answer (que en producción el
	// agentcore reenvía por el WebSocket; aquí se usa directamente).
	answer, err := sess.HandleOffer(*mobile.LocalDescription())
	if err != nil {
		t.Fatal(err)
	}
	if err := mobile.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}
	_ = answerCh

	// Esperar la pista de vídeo.
	select {
	case <-gotTrack:
	case <-time.After(10 * time.Second):
		t.Fatal("el móvil no recibió la pista de vídeo")
	}

	// Esperar a que abra el canal de control y enviar un evento de entrada.
	select {
	case <-controlOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("el canal de control no abrió")
	}

	ev, _ := json.Marshal(mrwebrtc.InputEvent{Type: "mouse.down", Button: "left"})
	if err := control.SendText(string(ev)); err != nil {
		t.Fatal(err)
	}

	// Verificar que el InputSink recibió el evento.
	deadline := time.After(5 * time.Second)
	for {
		fin.mu.Lock()
		n := fin.applied
		fin.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("el evento de entrada no llegó al InputSink")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Cerrar la sesión debe disparar ReleaseAll.
	sess.Close()
	time.Sleep(200 * time.Millisecond)
	fin.mu.Lock()
	ra := fin.releaseAll
	fin.mu.Unlock()
	if ra == 0 {
		t.Fatal("Close no liberó la entrada (ReleaseAll no se llamó)")
	}
}

var _ = input.New

func boolPtr(b bool) *bool    { return &b }
func u16Ptr(u uint16) *uint16 { return &u }
