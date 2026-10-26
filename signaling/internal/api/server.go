package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"example.com/miremote/signaling/internal/config"
	"example.com/miremote/signaling/internal/store"
	"example.com/miremote/signaling/internal/ws"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	hub   *ws.Hub

	// nonces emitidos pero aún no usados (para GET /v1/auth/nonce). El consumo
	// definitivo se hace en el store; esto solo limita la ventana de validez.
	nmu          sync.Mutex
	issuedNonces map[string]time.Time
}

func NewServer(cfg *config.Config, st *store.Store, hub *ws.Hub) *Server {
	s := &Server{
		cfg: cfg, store: st, hub: hub,
		issuedNonces: make(map[string]time.Time),
	}
	go s.nonceJanitor()
	return s
}

// JWTSecret expone el secreto para el handler WS (verificación de session-start).
func (s *Server) JWTSecret() []byte { return s.cfg.JWTSecret }

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/auth/nonce", s.handleNonce)
	mux.HandleFunc("POST /v1/hosts/register", s.handleRegisterHost)
	mux.HandleFunc("POST /v1/pairing/invitations", s.handleCreateInvitation)
	mux.HandleFunc("POST /v1/pairing/claim", s.handleClaim)
	mux.HandleFunc("GET /v1/pairing/invitations/{pairing_id}", s.handlePoll)
	mux.HandleFunc("POST /v1/pairing/invitations/{pairing_id}/decision", s.handleDecision)
	mux.HandleFunc("POST /v1/turn/credentials", s.handleTURN)
	mux.HandleFunc("POST /v1/session/token", s.handleSessionToken)
	mux.HandleFunc("GET /v1/ws", s.handleWS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return logMiddleware(mux)
}

func (s *Server) nonceJanitor() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		now := time.Now()
		s.nmu.Lock()
		for n, exp := range s.issuedNonces {
			if now.After(exp) {
				delete(s.issuedNonces, n)
			}
		}
		s.nmu.Unlock()
		_ = s.store.GC(context.Background())
	}
}

func (s *Server) handleNonce(w http.ResponseWriter, r *http.Request) {
	n := s.issueNonce()
	s.nmu.Lock()
	s.issuedNonces[n] = time.Now().Add(s.cfg.NonceTTL)
	s.nmu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"nonce": n, "ttl": int(s.cfg.NonceTTL.Seconds()),
	})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start // enganchar aquí un logger estructurado si se desea
	})
}
