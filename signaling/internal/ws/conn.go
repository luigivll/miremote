package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// wsConn implementa Conn sobre coder/websocket. Serializa las escrituras con
// un mutex porque el WS no permite escritores concurrentes.
type wsConn struct {
	c        *websocket.Conn
	deviceID string
	mu       sync.Mutex
}

func newConn(c *websocket.Conn, deviceID string) *wsConn {
	return &wsConn{c: c, deviceID: deviceID}
}

func (w *wsConn) DeviceID() string { return w.deviceID }

func (w *wsConn) Send(ctx context.Context, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return w.c.Write(ctx, websocket.MessageText, b)
}

func (w *wsConn) Close() {
	_ = w.c.Close(websocket.StatusNormalClosure, "")
}
