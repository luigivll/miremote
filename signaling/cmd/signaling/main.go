// Comando: servidor de señalización MiRemote.
//
// Variables de entorno (ver internal/config):
//   MR_LISTEN, MR_TLS_CERT, MR_TLS_KEY, MR_JWT_SECRET, MR_TURN_SECRET,
//   MR_TURN_URLS, MR_DB, MR_ALLOWED_ORIGINS, MR_*_TTL
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/miremote/signaling/internal/api"
	"example.com/miremote/signaling/internal/config"
	"example.com/miremote/signaling/internal/store"
	"example.com/miremote/signaling/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuración: %v", err)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	hub := ws.NewHub(st)
	srv := api.NewServer(cfg, st, hub)

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Sin WriteTimeout global: el WS es de larga duración.
	}

	go func() {
		log.Printf("señalización escuchando en %s", cfg.ListenAddr)
		var err error
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			err = httpSrv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			log.Printf("AVISO: sin TLS; usar solo detrás de un proxy TLS en localhost")
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("servidor: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Printf("señalización detenida")
}
