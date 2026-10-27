// Package agentcore orquesta el agente: identidad, conexión con la
// señalización, emparejamiento por QR y gestión de la sesión WebRTC entrante.
package agentcore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"example.com/miremote/agent/internal/capture"
	"example.com/miremote/agent/internal/config"
	"example.com/miremote/agent/internal/identity"
	"example.com/miremote/agent/internal/indicator"
	"example.com/miremote/agent/internal/input"
	"example.com/miremote/agent/internal/pairing"
	"example.com/miremote/agent/internal/signaling"
	mrwebrtc "example.com/miremote/agent/internal/webrtc"

	pion "github.com/pion/webrtc/v4"
)

type Agent struct {
	cfg *config.Config
	id  *identity.Identity
	sig *signaling.Client
	ws  *signaling.WSConn

	mu          sync.Mutex
	session     *mrwebrtc.Session
	iceSrv      []pion.ICEServer
	running     bool
	runCancel   context.CancelFunc
	curInv      *signaling.Invitation
	sessionPeer string

	dataDir string
}

func New(dataDir string) (*Agent, error) {
	cfg, err := config.Load(dataDir)
	if err != nil {
		return nil, err
	}
	id, err := identity.Load(dataDir, config.Protect, config.Unprotect)
	if err != nil {
		return nil, err
	}
	cfg.DeviceID = id.DeviceID
	_ = cfg.Save(dataDir)

	// La identidad no necesita servidor; el signaling_url solo se exige al
	// emparejar o arrancar (ver requireSignaling).
	a := &Agent{
		cfg:     cfg,
		id:      id,
		sig:     signaling.NewClient(cfg.SignalingURL, cfg.SignalingWSURL, id),
		dataDir: dataDir,
	}
	return a, nil
}

// rebuildClientLocked recrea el cliente de señalización con la config actual.
// Debe llamarse con a.mu tomado.
func (a *Agent) rebuildClientLocked() {
	a.sig = signaling.NewClient(a.cfg.SignalingURL, a.cfg.SignalingWSURL, a.id)
}

// requireSignaling comprueba que haya servidor configurado para las acciones
// que lo necesitan (emparejar, arrancar).
func (a *Agent) requireSignaling() error {
	if a.cfg.SignalingURL == "" {
		return fmt.Errorf("signaling_url no configurado en %s", a.dataDir)
	}
	return nil
}

func (a *Agent) DeviceID() string    { return a.id.DeviceID }
func (a *Agent) Identity() *identity.Identity { return a.id }

// Register registra la identidad del host en la señalización (idempotente).
func (a *Agent) Register(ctx context.Context) error {
	if err := a.requireSignaling(); err != nil {
		return err
	}
	return a.sig.Register(ctx, "MiRemote Host")
}

// Start registra el host, abre el WebSocket y queda a la espera de ofertas.
func (a *Agent) Start(ctx context.Context) error {
	if err := a.requireSignaling(); err != nil {
		return err
	}
	if err := a.sig.Register(ctx, "MiRemote Host"); err != nil {
		return fmt.Errorf("registro: %w", err)
	}
	log.Printf("host registrado, device_id=%s", a.id.DeviceID)

	if err := a.refreshICE(ctx); err != nil {
		log.Printf("aviso: no se pudieron obtener credenciales TURN: %v", err)
	}

	return a.connectWS(ctx)
}

func (a *Agent) connectWS(ctx context.Context) error {
	ws, err := a.sig.ConnectWS(ctx, signaling.WSHandlers{
		OnOffer:        a.handleOffer,
		OnICE:          a.handleICE,
		OnPairingClaim: a.handlePairingClaim,
	})
	if err != nil {
		return err
	}
	a.ws = ws
	go ws.KeepAlive(ctx)
	log.Printf("conectado a la señalización por WebSocket")
	return nil
}

func (a *Agent) refreshICE(ctx context.Context) error {
	creds, err := a.sig.TURN(ctx)
	if err != nil {
		return err
	}
	ice := []pion.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}
	// Solo añadir TURN si el servidor lo tiene configurado (URLs no vacías).
	if len(creds.URLs) > 0 {
		ice = append(ice, pion.ICEServer{
			URLs:       creds.URLs,
			Username:   creds.Username,
			Credential: creds.Credential,
		})
	}
	a.mu.Lock()
	a.iceSrv = ice
	a.mu.Unlock()
	return nil
}

// handleOffer crea una sesión WebRTC nueva al recibir una oferta del móvil.
func (a *Agent) handleOffer(from string, data json.RawMessage) {
	log.Printf("oferta recibida de %s", from)

	// Permisos: aquí se usan los por defecto; en una versión completa se
	// consultarían los permisos del vínculo (vienen en el token de sesión).
	perms := a.cfg.DefaultPermissions

	vid, err := capture.New(a.cfg.Width, a.cfg.Height, a.cfg.FPS)
	if err != nil {
		log.Printf("captura: %v", err)
		return
	}
	in, err := input.New()
	if err != nil {
		log.Printf("entrada: %v", err)
		return
	}
	var ind mrwebrtc.SessionIndicator
	if a.cfg.ShowSessionIndicator {
		ind, _ = indicator.New()
	}

	a.mu.Lock()
	iceSrv := a.iceSrv
	a.mu.Unlock()

	sess, err := mrwebrtc.NewSession(mrwebrtc.SessionConfig{
		Certificate: a.id.Certificate(),
		ICEServers:  iceSrv,
		Permissions: perms,
		Video:       vid,
		Input:       in,
		Indicator:   ind,
		DeviceName:  from,
		SendSignal: func(msgType string, d any) error {
			return a.ws.Send(context.Background(), msgType, from, d)
		},
		OnClosed: func() {
			a.mu.Lock()
			a.session = nil
			a.sessionPeer = ""
			a.mu.Unlock()
		},
	})
	if err != nil {
		log.Printf("crear sesión: %v", err)
		return
	}

	a.mu.Lock()
	// Una sola sesión a la vez: cerrar la previa.
	if a.session != nil {
		a.session.Close()
	}
	a.session = sess
	a.sessionPeer = from
	a.mu.Unlock()

	var offer pion.SessionDescription
	if err := json.Unmarshal(data, &offer); err != nil {
		log.Printf("offer inválida: %v", err)
		return
	}
	answer, err := sess.HandleOffer(offer)
	if err != nil {
		log.Printf("handle offer: %v", err)
		sess.Close()
		return
	}
	if err := a.ws.Send(context.Background(), "answer", from, answer); err != nil {
		log.Printf("enviar answer: %v", err)
	}
}

func (a *Agent) handleICE(from string, data json.RawMessage) {
	a.mu.Lock()
	sess := a.session
	a.mu.Unlock()
	if sess == nil {
		return
	}
	var cand pion.ICECandidateInit
	if err := json.Unmarshal(data, &cand); err != nil {
		return
	}
	if err := sess.AddICECandidate(cand); err != nil {
		log.Printf("add ice: %v", err)
	}
}

// --- Emparejamiento ---

// CreatePairing genera una invitación y devuelve el payload del QR.
func (a *Agent) CreatePairing(ctx context.Context) (qrPayload string, inv *signaling.Invitation, err error) {
	inv, err = a.sig.CreateInvitation(ctx)
	if err != nil {
		return "", nil, err
	}
	fp := a.id.PublicKeyFingerprint()
	expires, _ := time.Parse(time.RFC3339, inv.ExpiresAt)
	qrPayload, err = pairing.BuildQRPayload(a.cfg.SignalingURL, a.id.DeviceID,
		inv.PairingID, inv.Secret, fp, expires)
	return qrPayload, inv, err
}

// WaitAndDecide sondea el estado de la invitación y, al llegar una reclamación,
// invoca `confirm(nombre, huella)`; si devuelve true, aprueba.
func (a *Agent) WaitAndDecide(ctx context.Context, inv *signaling.Invitation,
	confirm func(name, platform, fingerprint string) bool) error {

	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		res, err := a.sig.Poll(ctx, inv.PairingID, inv.PollToken)
		if err != nil {
			continue
		}
		switch res.Status {
		case "claimed":
			approve := res.Mobile != nil &&
				confirm(res.Mobile.Name, res.Mobile.Platform, res.Mobile.Fingerprint)
			decision := "reject"
			if approve {
				decision = "approve"
			}
			if err := a.sig.Decide(ctx, inv.PairingID, decision, a.cfg.DefaultPermissions); err != nil {
				return err
			}
			if approve {
				log.Printf("móvil %s aprobado", res.Mobile.Name)
			}
			return nil
		case "expired", "rejected":
			return fmt.Errorf("emparejamiento %s", res.Status)
		}
	}
}

func (a *Agent) handlePairingClaim(data json.RawMessage) {
	// Aviso en tiempo real de que una invitación fue reclamada. La decisión se
	// toma en WaitAndDecide (sondeo); aquí solo se registra.
	log.Printf("solicitud de emparejamiento recibida: %s", string(data))
}

// StopSession detiene la sesión activa (lo llama el servicio o el indicador).
func (a *Agent) StopSession() {
	a.mu.Lock()
	sess := a.session
	a.mu.Unlock()
	if sess != nil {
		sess.StopFromLocal()
	}
}
