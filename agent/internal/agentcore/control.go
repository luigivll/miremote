package agentcore

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Status es el estado que consume la interfaz.
type Status struct {
	DeviceID      string `json:"device_id"`
	Fingerprint   string `json:"fingerprint"`
	Configured    bool   `json:"configured"`
	Server        string `json:"server"`
	Running       bool   `json:"running"`
	SessionActive bool   `json:"session_active"`
	SessionPeer   string `json:"session_peer"`
}

func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Status{
		DeviceID:      a.id.DeviceID,
		Fingerprint:   a.id.PublicKeyFingerprint(),
		Configured:    a.cfg.SignalingURL != "",
		Server:        a.cfg.SignalingURL,
		Running:       a.running,
		SessionActive: a.session != nil,
		SessionPeer:   a.sessionPeer,
	}
}

// SetServer guarda la URL del servidor de señalización y reinicia el cliente.
func (a *Agent) SetServer(url string) error {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" {
		return fmt.Errorf("URL vacía")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	a.mu.Lock()
	a.cfg.SignalingURL = url
	a.cfg.SignalingWSURL = toWSURL(url) + "/v1/ws"
	a.rebuildClientLocked()
	a.mu.Unlock()
	return a.cfg.Save(a.dataDir)
}

func toWSURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	return "ws://" + strings.TrimPrefix(httpURL, "http://")
}

// MobileInfo describe un móvil que ha reclamado la invitación.
type MobileInfo struct {
	DeviceID    string `json:"device_id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Fingerprint string `json:"fingerprint"`
}

// PairResult es la respuesta al iniciar el emparejamiento.
type PairResult struct {
	QRDataURL string `json:"qr"`       // PNG en data: URL para <img>
	PairingID string `json:"pairing_id"`
	ExpiresAt string `json:"expires_at"`
}

// StartPairing registra el host, crea una invitación y devuelve el QR como
// imagen lista para mostrar.
func (a *Agent) StartPairing(ctx context.Context) (*PairResult, error) {
	if err := a.Register(ctx); err != nil {
		return nil, err
	}
	qrPayload, inv, err := a.CreatePairing(ctx)
	if err != nil {
		return nil, err
	}
	png, err := qrcode.Encode(qrPayload, qrcode.Medium, 320)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.curInv = inv
	a.mu.Unlock()
	return &PairResult{
		QRDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		PairingID: inv.PairingID,
		ExpiresAt: inv.ExpiresAt,
	}, nil
}

// PairState es el estado del emparejamiento en curso.
type PairState struct {
	Status string      `json:"status"` // idle|waiting|claimed|approved|rejected|expired
	Mobile *MobileInfo `json:"mobile,omitempty"`
}

// PairStatus sondea el estado de la invitación en curso.
func (a *Agent) PairStatus(ctx context.Context) (*PairState, error) {
	a.mu.Lock()
	inv := a.curInv
	a.mu.Unlock()
	if inv == nil {
		return &PairState{Status: "idle"}, nil
	}
	res, err := a.sig.Poll(ctx, inv.PairingID, inv.PollToken)
	if err != nil {
		return nil, err
	}
	ps := &PairState{Status: res.Status}
	if res.Status == "pending" {
		ps.Status = "waiting"
	}
	if res.Mobile != nil {
		ps.Mobile = &MobileInfo{
			DeviceID:    res.Mobile.DeviceID,
			Name:        res.Mobile.Name,
			Platform:    res.Mobile.Platform,
			Fingerprint: res.Mobile.Fingerprint,
		}
	}
	return ps, nil
}

// DecidePairing aprueba o rechaza el emparejamiento en curso.
func (a *Agent) DecidePairing(ctx context.Context, approve bool) error {
	a.mu.Lock()
	inv := a.curInv
	a.mu.Unlock()
	if inv == nil {
		return fmt.Errorf("no hay emparejamiento en curso")
	}
	decision := "reject"
	if approve {
		decision = "approve"
	}
	if err := a.sig.Decide(ctx, inv.PairingID, decision, a.cfg.DefaultPermissions); err != nil {
		return err
	}
	a.mu.Lock()
	a.curInv = nil
	a.mu.Unlock()
	return nil
}

// StartAgent arranca el agente en segundo plano (registro + WebSocket).
func (a *Agent) StartAgent() error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		cancel()
		return err
	}
	a.mu.Lock()
	a.running = true
	a.runCancel = cancel
	a.mu.Unlock()
	return nil
}

// StopAgent detiene el agente y cualquier sesión activa.
func (a *Agent) StopAgent() {
	a.mu.Lock()
	cancel := a.runCancel
	ws := a.ws
	sess := a.session
	a.running = false
	a.runCancel = nil
	a.mu.Unlock()

	if sess != nil {
		sess.StopFromLocal()
	}
	if cancel != nil {
		cancel()
	}
	if ws != nil {
		ws.Close()
	}
}

// VideoProfile expone la configuración de vídeo para la UI.
func (a *Agent) VideoProfile() (w, h, fps int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Width, a.cfg.Height, a.cfg.FPS
}
