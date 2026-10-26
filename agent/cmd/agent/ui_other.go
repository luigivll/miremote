//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
)

// platformUI (no-Windows): abre el navegador del sistema y bloquea. Sirve para
// desarrollo; la experiencia de app con bandeja es específica de Windows.
func platformUI(url string) {
	fmt.Printf("MiRemote — panel disponible en: %s\n", url)
	openBrowser(url)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}

func platformFatal(msg string) { fmt.Fprintln(os.Stderr, msg) }

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
