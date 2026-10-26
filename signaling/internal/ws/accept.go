package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// HelloData es el contenido del primer mensaje que envía el cliente.
type HelloData struct {
	DeviceID  string `json:"device_id"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"` // firma Ed25519 de Nonce, base64
}

// Authenticator verifica un hello y devuelve el device_id autenticado o error.
type Authenticator func(ctx context.Context, h HelloData) (string, error)

// Options agrupa las dependencias del handler WS.
type Options struct {
	AllowedOrigins []string
	Authenticate   Authenticator
}

// Accept hace el upgrade, autentica con el primer mensaje `hello`, registra la
// conexión en el hub y bombea mensajes entrantes hacia hub.Route.
func (h *Hub) Accept(w http.ResponseWriter, r *http.Request, opts Options) {
	acceptOpts := &websocket.AcceptOptions{}
	if len(opts.AllowedOrigins) > 0 {
		acceptOpts.OriginPatterns = opts.AllowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true // solo dev; en prod fijar OriginPatterns
	}

	c, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		return
	}

	ctx := r.Context()

	// Primer mensaje: hello (con límite de tiempo).
	helloCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	typ, data, err := c.Read(helloCtx)
	if err != nil || typ != websocket.MessageText {
		c.Close(websocket.StatusPolicyViolation, "se esperaba hello")
		return
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != "hello" {
		c.Close(websocket.StatusPolicyViolation, "hello inválido")
		return
	}
	var hello HelloData
	if err := json.Unmarshal(env.Data, &hello); err != nil {
		c.Close(websocket.StatusPolicyViolation, "datos de hello inválidos")
		return
	}

	deviceID, err := opts.Authenticate(ctx, hello)
	if err != nil {
		c.Close(websocket.StatusPolicyViolation, "autenticación fallida")
		return
	}

	conn := newConn(c, deviceID)
	h.Register(ctx, conn)
	defer h.Unregister(context.Background(), deviceID)

	// Bucle de lectura. El servidor solo reenvía offer/answer/ice/session-start.
	c.SetReadLimit(1 << 20)
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var in Envelope
		if err := json.Unmarshal(data, &in); err != nil {
			continue
		}
		if !routableType(in.Type) {
			_ = conn.Send(ctx, errorEnv("tipo no enrutaba: "+in.Type))
			continue
		}
		if err := h.Route(ctx, deviceID, in); err != nil {
			_ = conn.Send(ctx, errorEnv(err.Error()))
		}
	}
}

func routableType(t string) bool {
	switch t {
	case "offer", "answer", "ice", "session-start", "ping":
		return true
	}
	return false
}

func errorEnv(msg string) Envelope {
	d, _ := json.Marshal(map[string]string{"message": msg})
	return Envelope{Type: "error", Data: d}
}

var ErrUnauthenticated = errors.New("no autenticado")
