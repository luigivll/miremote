// Comando: relevo Wake-on-LAN de MiRemote.
//
// Debe ejecutarse en un equipo que permanezca encendido dentro de la LAN
// (router con contenedores, NAS, Raspberry Pi). Expone una API mínima en la
// LAN para que un móvil autorizado pida despertar un PC configurado.
//
// Uso: wolrelay -config /etc/miremote/wol-relay.json
//
// SEGURIDAD: no publiques este servicio en Internet directamente. Si necesitas
// acceso remoto, hazlo a través del servidor de señalización / un túnel, no
// abriendo este puerto al exterior. El puerto UDP 9 NUNCA debe exponerse.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"example.com/miremote/wolrelay/internal/auth"
	"example.com/miremote/wolrelay/internal/config"
	"example.com/miremote/wolrelay/internal/wol"
)

type server struct {
	cfg     *config.Config
	nonces  *auth.NonceStore
	limiter *auth.RateLimiter
}

func main() {
	configPath := flag.String("config", "/etc/miremote/wol-relay.json", "ruta del fichero de configuración")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("configuración: %v", err)
	}

	s := &server{
		cfg:     cfg,
		nonces:  auth.NewNonceStore(30 * time.Second),
		limiter: auth.NewRateLimiter(time.Duration(cfg.RateLimitSeconds) * time.Second),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/nonce", s.handleNonce)
	mux.HandleFunc("POST /v1/wake", s.handleWake)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("relevo WoL escuchando en %s (%d dispositivos, %d móviles)",
		cfg.ListenAddr, len(cfg.Devices), len(cfg.AuthorizedMobiles))

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		log.Fatal(httpSrv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey))
	} else {
		log.Printf("AVISO: sin TLS; usar solo en LAN de confianza")
		log.Fatal(httpSrv.ListenAndServe())
	}
}

func (s *server) handleNonce(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"nonce": s.nonces.Issue(), "ttl": 30})
}

type wakeReq struct {
	DeviceID  string `json:"device_id"`  // móvil solicitante
	Target    string `json:"target"`     // id lógico del PC a despertar
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`  // firma del nonce con la clave del móvil
}

func (s *server) handleWake(w http.ResponseWriter, r *http.Request) {
	var req wakeReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json inválido"})
		return
	}

	// 1. ¿Móvil autorizado?
	mobile, ok := s.cfg.MobileByID(req.DeviceID)
	if !ok {
		writeJSON(w, 403, map[string]string{"error": "móvil no autorizado"})
		return
	}

	// 2. Nonce válido (anti-repetición).
	if err := s.nonces.Consume(req.Nonce); err != nil {
		writeJSON(w, 401, map[string]string{"error": "nonce inválido"})
		return
	}

	// 3. Firma correcta (prueba de posesión de la clave privada).
	if err := auth.Verify(mobile.PublicKey, req.Nonce, req.Signature); err != nil {
		writeJSON(w, 401, map[string]string{"error": "firma inválida"})
		return
	}

	// 4. Límite de frecuencia.
	if err := s.limiter.Allow(req.DeviceID); err != nil {
		writeJSON(w, 429, map[string]string{"error": "demasiadas peticiones"})
		return
	}

	// 5. ¿Dispositivo de destino configurado?
	dev, ok := s.cfg.DeviceByID(req.Target)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "dispositivo de destino desconocido"})
		return
	}

	// 6. Enviar el paquete mágico. La MAC/broadcast vienen de configuración,
	//    nunca de la petición.
	if err := wol.Wake(dev.MAC, dev.Broadcast, dev.Port); err != nil {
		log.Printf("WoL fallo target=%s: %v", req.Target, err)
		writeJSON(w, 500, map[string]string{"error": "no se pudo enviar el paquete"})
		return
	}

	log.Printf("WoL enviado: móvil=%s (%s) target=%s mac=%s",
		req.DeviceID, mobile.Name, req.Target, dev.MAC)
	writeJSON(w, 200, map[string]string{"status": "magic_packet_sent"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
