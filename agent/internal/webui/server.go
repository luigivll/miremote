// Package webui sirve la interfaz local del agente de PC (una página web oscura
// embebida) y expone una pequeña API HTTP que la interfaz usa para configurar,
// emparejar y controlar el agente. El .exe arranca este servidor en 127.0.0.1 y
// abre una ventana de app apuntando a él.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"example.com/miremote/agent/internal/agentcore"
)

//go:embed assets/index.html
var assets embed.FS

type Server struct {
	agent *agentcore.Agent
}

func New(a *agentcore.Agent) *Server { return &Server{agent: a} }

// Serve arranca el servidor en 127.0.0.1 con un puerto libre y devuelve la URL.
// El servidor queda corriendo en segundo plano.
func (s *Server) Serve() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("assets/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.agent.Status())
	})

	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Server string `json:"server"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "json inválido")
			return
		}
		if err := s.agent.SetServer(body.Server); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/pair/start", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		res, err := s.agent.StartPairing(ctx)
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, res)
	})

	mux.HandleFunc("GET /api/pair/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		st, err := s.agent.PairStatus(ctx)
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, st)
	})

	mux.HandleFunc("POST /api/pair/decision", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Approve bool `json:"approve"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "json inválido")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := s.agent.DecidePairing(ctx, body.Approve); err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/agent/start", func(w http.ResponseWriter, r *http.Request) {
		if err := s.agent.StartAgent(); err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/agent/stop", func(w http.ResponseWriter, r *http.Request) {
		s.agent.StopAgent()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/session/stop", func(w http.ResponseWriter, r *http.Request) {
		s.agent.StopSession()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)

	return fmt.Sprintf("http://%s/", ln.Addr().String()), nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
