// Comando: agente MiRemote (PC).
//
// Subcomandos:
//   setup   -- escribe la URL de señalización en la configuración
//   pair    -- genera un QR de emparejamiento y espera la confirmación
//   run     -- se conecta a la señalización y atiende sesiones remotas
//   id      -- muestra el device_id y la huella de la clave pública
//
// En Windows, "run" se ejecuta normalmente como servicio (ver deploy/), que
// a su vez lanza el agente en la sesión interactiva del usuario. Aquí el mismo
// binario sirve para ambos; la separación servicio/agente se documenta en
// deploy/windows.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"example.com/miremote/agent/internal/agentcore"
	"example.com/miremote/agent/internal/config"

	qrcode "github.com/skip2/go-qrcode"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	switch cmd {
	case "setup":
		cmdSetup()
	case "pair":
		cmdPair()
	case "run":
		cmdRun()
	case "id":
		cmdID()
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `MiRemote agent
Uso:
  agent setup -signaling https://host -ws wss://host/v1/ws
  agent pair      # genera un QR y espera confirmación
  agent run       # atiende sesiones remotas
  agent id        # muestra device_id y huella
`)
}

func dataDir() string {
	if d := os.Getenv("MR_DATA_DIR"); d != "" {
		return d
	}
	// Por defecto, modo usuario (no servicio).
	return config.DataDir(false)
}

func cmdSetup() {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	signalingURL := fs.String("signaling", "", "URL HTTPS de la señalización")
	wsURL := fs.String("ws", "", "URL WSS de la señalización")
	fs.Parse(os.Args[1:])

	if *signalingURL == "" {
		fmt.Fprintln(os.Stderr, "se requiere -signaling")
		os.Exit(1)
	}
	dir := dataDir()
	cfg, err := config.Load(dir)
	if err != nil {
		fatal(err)
	}
	cfg.SignalingURL = strings.TrimRight(*signalingURL, "/")
	if *wsURL != "" {
		cfg.SignalingWSURL = *wsURL
	} else {
		cfg.SignalingWSURL = toWS(cfg.SignalingURL) + "/v1/ws"
	}
	if err := cfg.Save(dir); err != nil {
		fatal(err)
	}
	fmt.Printf("configuración guardada en %s\n", dir)
}

func toWS(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	return "ws://" + strings.TrimPrefix(httpURL, "http://")
}

func cmdID() {
	a, err := agentcore.New(dataDir())
	if err != nil {
		fatal(err)
	}
	fmt.Printf("device_id:   %s\n", a.DeviceID())
	fmt.Printf("huella pub:  %s\n", a.Identity().PublicKeyFingerprint())
	fp, _ := a.Identity().DTLSFingerprint()
	fmt.Printf("huella DTLS: %s\n", fp)
}

func cmdPair() {
	ctx := context.Background()
	a, err := agentcore.New(dataDir())
	if err != nil {
		fatal(err)
	}
	// Registrar el host antes de poder crear invitaciones.
	if err := a.Register(ctx); err != nil {
		fatal(fmt.Errorf("registro: %w", err))
	}

	qrPayload, inv, err := a.CreatePairing(ctx)
	if err != nil {
		fatal(err)
	}

	// QR en terminal.
	q, err := qrcode.New(qrPayload, qrcode.Medium)
	if err != nil {
		fatal(err)
	}
	fmt.Println(q.ToSmallString(false))

	// Y como PNG junto a la configuración, por si se prefiere mostrar en pantalla.
	pngPath := dataDir() + "/pairing_qr.png"
	_ = q.WriteFile(256, pngPath)
	fmt.Printf("QR también guardado en: %s\n", pngPath)
	fmt.Printf("Caduca: %s\n\n", inv.ExpiresAt)
	fmt.Println("Escanea el código con la app móvil. Esperando...")

	err = a.WaitAndDecide(ctx, inv, func(name, platform, fingerprint string) bool {
		fmt.Printf("\nSolicitud de emparejamiento:\n")
		fmt.Printf("  Dispositivo: %s (%s)\n", name, platform)
		fmt.Printf("  Huella:      %s\n", fingerprint)
		fmt.Print("¿Aprobar este dispositivo? [s/N]: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		return line == "s" || line == "si" || line == "y" || line == "yes"
	})
	if err != nil {
		fatal(err)
	}
	fmt.Println("Emparejamiento completado.")
}

func cmdRun() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := agentcore.New(dataDir())
	if err != nil {
		fatal(err)
	}

	if err := a.Start(ctx); err != nil {
		fatal(err)
	}

	fmt.Printf("agente en marcha (device_id=%s). Ctrl-C para salir.\n", a.DeviceID())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	a.StopSession()
	fmt.Println("\nagente detenido")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
