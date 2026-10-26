// Package ws implementa el hub de señalización: mantiene las conexiones WSS
// vivas de PCs y móviles y reenvía mensajes (offer/answer/ice/session-start)
// únicamente entre dispositivos con un vínculo aprobado y ambos en línea.
//
// El hub NO inspecciona el contenido del medio; solo enruta sobres por
// device_id de destino tras comprobar autorización.
package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"example.com/miremote/signaling/internal/store"
)

// Envelope es el sobre común del protocolo WS (ver shared/proto/protocol.md §3).
type Envelope struct {
	Type string          `json:"type"`
	To   string          `json:"to,omitempty"`
	From string          `json:"from,omitempty"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Conn abstrae una conexión viva de un dispositivo.
type Conn interface {
	Send(ctx context.Context, env Envelope) error
	DeviceID() string
	Close()
}

type Hub struct {
	mu    sync.RWMutex
	conns map[string]Conn // device_id -> conexión viva
	store *store.Store
}

func NewHub(st *store.Store) *Hub {
	return &Hub{conns: make(map[string]Conn), store: st}
}

// Register añade una conexión y notifica a sus pares vinculados que está en línea.
func (h *Hub) Register(ctx context.Context, c Conn) {
	h.mu.Lock()
	// Si había una conexión previa del mismo dispositivo, se cierra.
	if old, ok := h.conns[c.DeviceID()]; ok {
		old.Close()
	}
	h.conns[c.DeviceID()] = c
	h.mu.Unlock()

	h.notifyPeers(ctx, c.DeviceID(), "peer-online")

	// Enviar welcome con los pares vinculados que estén en línea.
	peers := h.onlinePeers(ctx, c.DeviceID())
	data, _ := json.Marshal(map[string]any{"peers": peers})
	_ = c.Send(ctx, Envelope{Type: "welcome", Data: data})
}

// Unregister elimina la conexión y avisa a los pares.
func (h *Hub) Unregister(ctx context.Context, deviceID string) {
	h.mu.Lock()
	delete(h.conns, deviceID)
	h.mu.Unlock()
	h.notifyPeers(ctx, deviceID, "peer-offline")
}

// SystemNotify envía un sobre generado por el servidor a un dispositivo
// concreto sin exigir vínculo. Se usa para avisos de emparejamiento
// (pairing-claimed) donde el vínculo todavía no existe. Devuelve false si el
// destino no está en línea.
func (h *Hub) SystemNotify(ctx context.Context, deviceID string, env Envelope) bool {
	h.mu.RLock()
	c, ok := h.conns[deviceID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	env.From = "server"
	_ = c.Send(ctx, env)
	return true
}

// Route reenvía un sobre hacia env.To si existe vínculo aprobado entre el
// emisor y el destino y el destino está en línea.
func (h *Hub) Route(ctx context.Context, from string, env Envelope) error {
	if env.To == "" {
		return errSendf("mensaje sin destino")
	}
	authorized, err := h.linked(ctx, from, env.To)
	if err != nil {
		return err
	}
	if !authorized {
		return errSendf("sin vínculo aprobado con el destino")
	}

	h.mu.RLock()
	dst, ok := h.conns[env.To]
	h.mu.RUnlock()
	if !ok {
		return errSendf("el destino no está en línea")
	}

	env.From = from
	return dst.Send(ctx, env)
}

// linked comprueba que a y b formen un par host-móvil aprobado (en cualquier orden).
func (h *Hub) linked(ctx context.Context, a, b string) (bool, error) {
	if _, err := h.store.GetLink(ctx, a, b); err == nil {
		return true, nil
	}
	if _, err := h.store.GetLink(ctx, b, a); err == nil {
		return true, nil
	}
	return false, nil
}

func (h *Hub) onlinePeers(ctx context.Context, deviceID string) []string {
	links, err := h.store.LinksForDevice(ctx, deviceID)
	if err != nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []string
	for _, l := range links {
		peer := l.MobileID
		if l.MobileID == deviceID {
			peer = l.HostID
		}
		if _, ok := h.conns[peer]; ok {
			out = append(out, peer)
		}
	}
	return out
}

func (h *Hub) notifyPeers(ctx context.Context, deviceID, eventType string) {
	links, err := h.store.LinksForDevice(ctx, deviceID)
	if err != nil {
		return
	}
	data, _ := json.Marshal(map[string]string{"device_id": deviceID})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, l := range links {
		peer := l.MobileID
		if l.MobileID == deviceID {
			peer = l.HostID
		}
		if c, ok := h.conns[peer]; ok {
			ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = c.Send(ctx2, Envelope{Type: eventType, Data: data})
			cancel()
		}
	}
}

type sendError struct{ msg string }

func (e sendError) Error() string { return e.msg }
func errSendf(m string) error     { return sendError{m} }
