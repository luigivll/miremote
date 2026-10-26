package main

import (
	"fmt"
	"os"

	"example.com/miremote/agent/internal/agentcore"
	"example.com/miremote/agent/internal/webui"
)

// runUI arranca la interfaz gráfica: servidor web local + ventana de app +
// icono de bandeja (en Windows). Es lo que ocurre al abrir el .exe con doble
// clic.
func runUI() {
	a, err := agentcore.New(dataDir())
	if err != nil {
		platformFatal(fmt.Sprintf("No se pudo iniciar MiRemote:\n%v", err))
		os.Exit(1)
	}
	srv := webui.New(a)
	url, err := srv.Serve()
	if err != nil {
		platformFatal(fmt.Sprintf("No se pudo abrir la interfaz:\n%v", err))
		os.Exit(1)
	}
	// Entrega el control a la capa de plataforma: abre la ventana de app y
	// mantiene el proceso vivo (bandeja en Windows).
	platformUI(url)
}
