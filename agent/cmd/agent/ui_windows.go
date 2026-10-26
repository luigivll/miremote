//go:build windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	muser32          = syscall.NewLazyDLL("user32.dll")
	mshell32         = syscall.NewLazyDLL("shell32.dll")
	mkernel32        = syscall.NewLazyDLL("kernel32.dll")
	pRegisterClass   = muser32.NewProc("RegisterClassExW")
	pCreateWindow    = muser32.NewProc("CreateWindowExW")
	pDefWindowProc   = muser32.NewProc("DefWindowProcW")
	pGetMessage      = muser32.NewProc("GetMessageW")
	pTranslateMsg    = muser32.NewProc("TranslateMessage")
	pDispatchMsg     = muser32.NewProc("DispatchMessageW")
	pPostQuit        = muser32.NewProc("PostQuitMessage")
	pCreatePopup     = muser32.NewProc("CreatePopupMenu")
	pAppendMenu      = muser32.NewProc("AppendMenuW")
	pTrackPopup      = muser32.NewProc("TrackPopupMenu")
	pGetCursorPos    = muser32.NewProc("GetCursorPos")
	pSetForeground   = muser32.NewProc("SetForegroundWindow")
	pLoadIcon        = muser32.NewProc("LoadIconW")
	pMessageBox      = muser32.NewProc("MessageBoxW")
	pShellNotify     = mshell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandle = mkernel32.NewProc("GetModuleHandleW")
)

const (
	cNIMAdd    = 0x0
	cNIMDelete = 0x2
	cNIFMsg    = 0x1
	cNIFIcon   = 0x2
	cNIFTip    = 0x4
	cWMUser    = 0x400
	cTrayCB    = cWMUser + 1
	cWMCommand = 0x111
	cWMDestroy = 0x2
	cRBtnUp    = 0x205
	cLBtnUp    = 0x202
	cMFString  = 0x0
	cIDIApp    = 32512
	cIDOpen    = 1
	cIDQuit    = 2
	cMBIcon    = 0x10 // MB_ICONERROR
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            syscall.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uTimeout         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     syscall.Handle
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type tMsg struct {
	hwnd   syscall.Handle
	msg    uint32
	wParam uintptr
	lParam uintptr
	time   uint32
	ptX    int32
	ptY    int32
}

type tPoint struct{ X, Y int32 }

var trayURL string
var trayNID notifyIconData

func platformFatal(msg string) {
	t, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("MiRemote")
	pMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(title)), cMBIcon)
}

// platformUI abre la ventana de app y entra en el bucle de bandeja.
func platformUI(url string) {
	trayURL = url
	openBrowser(url)
	runTray()
}

func runTray() {
	runtime.LockOSThread()

	hInst, _, _ := pGetModuleHandle.Call(0)
	className, _ := syscall.UTF16PtrFromString("MiRemoteTray")

	wndProc := syscall.NewCallback(func(hwnd syscall.Handle, m uint32, wParam, lParam uintptr) uintptr {
		switch m {
		case cTrayCB:
			if lParam == cRBtnUp || lParam == cLBtnUp {
				showTrayMenu(hwnd)
			}
		case cWMCommand:
			switch wParam & 0xffff {
			case cIDOpen:
				openBrowser(trayURL)
			case cIDQuit:
				pShellNotify.Call(cNIMDelete, uintptr(unsafe.Pointer(&trayNID)))
				pPostQuit.Call(0)
			}
		case cWMDestroy:
			pPostQuit.Call(0)
		}
		r, _, _ := pDefWindowProc.Call(uintptr(hwnd), uintptr(m), wParam, lParam)
		return r
	})

	wc := wndClassEx{
		lpfnWndProc:   wndProc,
		hInstance:     syscall.Handle(hInst),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))

	name, _ := syscall.UTF16PtrFromString("MiRemote")
	hwnd, _, _ := pCreateWindow.Call(0,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0, 0, 0, uintptr(hInst), 0)

	hIcon, _, _ := pLoadIcon.Call(0, uintptr(cIDIApp))
	trayNID = notifyIconData{
		hWnd:             syscall.Handle(hwnd),
		uID:              1,
		uFlags:           cNIFMsg | cNIFIcon | cNIFTip,
		uCallbackMessage: cTrayCB,
		hIcon:            syscall.Handle(hIcon),
	}
	trayNID.cbSize = uint32(unsafe.Sizeof(trayNID))
	copyUTF16("MiRemote — Agente en marcha", trayNID.szTip[:])
	pShellNotify.Call(cNIMAdd, uintptr(unsafe.Pointer(&trayNID)))

	var msg tMsg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}
	os.Exit(0)
}

func showTrayMenu(hwnd syscall.Handle) {
	hMenu, _, _ := pCreatePopup.Call()
	openLabel, _ := syscall.UTF16PtrFromString("Abrir panel")
	quitLabel, _ := syscall.UTF16PtrFromString("Salir")
	pAppendMenu.Call(hMenu, cMFString, cIDOpen, uintptr(unsafe.Pointer(openLabel)))
	pAppendMenu.Call(hMenu, cMFString, cIDQuit, uintptr(unsafe.Pointer(quitLabel)))

	var pt tPoint
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForeground.Call(uintptr(hwnd))
	// TPM_LEFTALIGN|TPM_BOTTOMALIGN|TPM_RIGHTBUTTON
	pTrackPopup.Call(hMenu, 0x0|0x20|0x2, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
}

func copyUTF16(s string, dst []uint16) {
	u, _ := syscall.UTF16FromString(s)
	for i := range dst {
		if i < len(u) {
			dst[i] = u[i]
		} else {
			dst[i] = 0
		}
	}
}

// openBrowser abre la UI como ventana de aplicación. Intenta Edge y Chrome en
// modo --app (ventana sin barra de navegador); si no, el navegador por defecto.
func openBrowser(url string) {
	appArg := "--app=" + url
	candidates := []string{"msedge", "chrome"}
	for _, b := range candidates {
		cmd := exec.Command(b, appArg, "--window-size=760,900")
		if err := cmd.Start(); err == nil {
			return
		}
	}
	// Fallback: navegador por defecto (pestaña normal).
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
