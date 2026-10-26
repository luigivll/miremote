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
	// Sin argumentos (p.ej. doble clic en Windows): interfaz gráfica.
	if len(os.Args) < 2 {
		runUI()
		return
	}
	cmd := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	switch cmd {
	case "ui":
		runUI()
	case "setup":
		cmdSetup()
	case "pair":
		cmdPair()
	case "run":
		cmdRun()
	case "id":
		cmdID()
	case "menu":
		interactiveMenu()
	default:
		usage()
		os.Exit(1)
	}
}

// pause espera a que el usuario pulse Enter, para que la consola no se cierre
// sola cuando se abrió con doble clic.
func pause() {
	fmt.Print("\nPulsa Enter para continuar...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// interactiveMenu ofrece un menú de texto cuando el agente se abre sin
// argumentos. Mantiene la ventana abierta y guía la configuración.
func interactiveMenu() {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("\n=============================\n")
		fmt.Printf("   MiRemote — Agente de PC\n")
		fmt.Printf("=============================\n")
		fmt.Printf("Datos en: %s\n\n", dataDir())
		fmt.Printf("  1) Configurar servidor de señalización\n")
		fmt.Printf("  2) Emparejar con el móvil (mostrar QR)\n")
		fmt.Printf("  3) Iniciar el agente (atender conexiones)\n")
		fmt.Printf("  4) Ver mi identidad (device_id y huellas)\n")
		fmt.Printf("  5) Salir\n\n")
		fmt.Print("Elige una opción [1-5]: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return // EOF (p.ej. sin consola): salir limpio
		}
		switch strings.TrimSpace(line) {
		case "1":
			menuSetup(reader)
		case "2":
			runPair()
			pause()
		case "3":
			fmt.Println("\nIniciando agente. Cierra la ventana o pulsa Ctrl-C para detener.")
			runAgent()
		case "4":
			printIdentity()
			pause()
		case "5", "q", "salir":
			return
		default:
			fmt.Println("Opción no válida.")
		}
	}
}

func menuSetup(reader *bufio.Reader) {
	fmt.Print("\nURL del servidor de señalización (p.ej. https://miservidor.com): ")
	line, _ := reader.ReadString('\n')
	url := strings.TrimRight(strings.TrimSpace(line), "/")
	if url == "" {
		fmt.Println("No se cambió nada.")
		return
	}
	dir := dataDir()
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	cfg.SignalingURL = url
	cfg.SignalingWSURL = toWS(url) + "/v1/ws"
	if err := cfg.Save(dir); err != nil {
		fmt.Printf("Error al guardar: %v\n", err)
		return
	}
	fmt.Printf("Guardado. Señalización: %s\n", url)
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

func cmdID() { printIdentity() }

func printIdentity() {
	a, err := agentcore.New(dataDir())
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("device_id:   %s\n", a.DeviceID())
	fmt.Printf("huella pub:  %s\n", a.Identity().PublicKeyFingerprint())
	fp, _ := a.Identity().DTLSFingerprint()
	fmt.Printf("huella DTLS: %s\n", fp)
}

func cmdPair() { runPair() }

func runPair() {
	ctx := context.Background()
	a, err := agentcore.New(dataDir())
	if err != nil {
		fmt.Printf("error: %v\n", err)
		if isNoSignaling(err) {
			fmt.Println("Primero configura el servidor (opción 1 del menú).")
		}
		return
	}
	if err := a.Register(ctx); err != nil {
		fmt.Printf("error de registro: %v\n", err)
		fmt.Println("¿Está en marcha el servidor de señalización y es correcta su URL?")
		return
	}

	qrPayload, inv, err := a.CreatePairing(ctx)
	if err != nil {
		fmt.Printf("error al crear la invitación: %v\n", err)
		return
	}

	q, err := qrcode.New(qrPayload, qrcode.Medium)
	if err != nil {
		fmt.Printf("error al generar el QR: %v\n", err)
		return
	}
	fmt.Println(q.ToSmallString(false))

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
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println("Emparejamiento completado.")
}

func cmdRun() { runAgent() }

func runAgent() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := agentcore.New(dataDir())
	if err != nil {
		fmt.Printf("error: %v\n", err)
		if isNoSignaling(err) {
			fmt.Println("Primero configura el servidor (opción 1 del menú).")
		}
		return
	}

	if err := a.Start(ctx); err != nil {
		fmt.Printf("error al iniciar: %v\n", err)
		return
	}

	fmt.Printf("agente en marcha (device_id=%s). Ctrl-C para salir.\n", a.DeviceID())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	a.StopSession()
	fmt.Println("\nagente detenido")
}

func isNoSignaling(err error) bool {
	return strings.Contains(err.Error(), "signaling_url")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
