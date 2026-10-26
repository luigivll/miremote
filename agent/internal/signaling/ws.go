package signaling

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/coder/websocket"
)

// Envelope coincide con el sobre del servidor (shared/proto §3).
type Envelope struct {
	Type string          `json:"type"`
	To   string          `json:"to,omitempty"`
	From string          `json:"from,omitempty"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// WSHandlers recibe los eventos entrantes relevantes para el agente.
type WSHandlers struct {
	OnOffer         func(from string, data json.RawMessage)
	OnICE           func(from string, data json.RawMessage)
	OnPairingClaim  func(data json.RawMessage)
	OnPeerOnline    func(deviceID string)
	OnPeerOffline   func(deviceID string)
}

// WSConn es la conexión viva al servidor.
type WSConn struct {
	conn *websocket.Conn
}

// Connect abre el WebSocket, envía el hello firmado y arranca el bucle de
// lectura. Devuelve cuando el hello se ha aceptado; el bucle sigue en segundo
// plano hasta que el contexto termine o la conexión caiga.
func (c *Client) ConnectWS(ctx context.Context, h WSHandlers) (*WSConn, error) {
	conn, _, err := websocket.Dial(ctx, c.wsURL, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(1 << 20)

	// hello firmado.
	nonce, err := c.getNonce(ctx)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "nonce")
		return nil, err
	}
	hello := map[string]any{
		"device_id": c.id.DeviceID,
		"nonce":     nonce,
		"signature": c.id.Sign([]byte(nonce)),
	}
	helloData, _ := json.Marshal(hello)
	if err := writeEnvelope(ctx, conn, Envelope{Type: "hello", Data: helloData}); err != nil {
		conn.Close(websocket.StatusInternalError, "hello")
		return nil, err
	}

	wc := &WSConn{conn: conn}
	go wc.readLoop(ctx, h)
	return wc, nil
}

func (wc *WSConn) readLoop(ctx context.Context, h WSHandlers) {
	for {
		_, data, err := wc.conn.Read(ctx)
		if err != nil {
			log.Printf("ws cerrado: %v", err)
			return
		}
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case "offer":
			if h.OnOffer != nil {
				h.OnOffer(env.From, env.Data)
			}
		case "ice":
			if h.OnICE != nil {
				h.OnICE(env.From, env.Data)
			}
		case "pairing-claimed":
			if h.OnPairingClaim != nil {
				h.OnPairingClaim(env.Data)
			}
		case "peer-online":
			if h.OnPeerOnline != nil {
				h.OnPeerOnline(peerID(env.Data))
			}
		case "peer-offline":
			if h.OnPeerOffline != nil {
				h.OnPeerOffline(peerID(env.Data))
			}
		case "welcome", "error":
			// informativo
		}
	}
}

// Send envía un sobre dirigido a un dispositivo (answer/ice).
func (wc *WSConn) Send(ctx context.Context, msgType, to string, data any) error {
	raw, _ := json.Marshal(data)
	return writeEnvelope(ctx, wc.conn, Envelope{Type: msgType, To: to, Data: raw})
}

func (wc *WSConn) Close() {
	wc.conn.Close(websocket.StatusNormalClosure, "")
}

func writeEnvelope(ctx context.Context, conn *websocket.Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

func peerID(data json.RawMessage) string {
	var m struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.Unmarshal(data, &m)
	return m.DeviceID
}

// KeepAlive envía pings periódicos para mantener viva la conexión y detectar
// caídas. Bloquea hasta que el contexto termine.
func (wc *WSConn) KeepAlive(ctx context.Context) {
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wc.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

var _ = fmt.Sprintf
