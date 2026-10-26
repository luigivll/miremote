//go:build windows

// Indicador de sesión en Windows mediante un icono en la bandeja del sistema
// (Shell_NotifyIcon) con un menú contextual que incluye "Detener sesión".
// Implementado directamente sobre Win32 por syscall para no añadir dependencias.
//
// Además del icono de bandeja, muestra un globo (balloon) al iniciar la sesión
// con el texto "Sesión remota activa: <móvil>", de modo que el aviso sea
// visible aunque el usuario no esté mirando la bandeja. Esto cumple la
// restricción de transparencia: nunca hay control remoto sin señal visible.
package indicator

import (
	"sync"
	"syscall"
	"unsafe"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	user32w            = syscall.NewLazyDLL("user32.dll")
	kernel32w          = syscall.NewLazyDLL("kernel32.dll")
	procShellNotify    = shell32.NewProc("Shell_NotifyIconW")
	procRegisterClass  = user32w.NewProc("RegisterClassExW")
	procCreateWindow   = user32w.NewProc("CreateWindowExW")
	procDefWindowProc  = user32w.NewProc("DefWindowProcW")
	procGetMessage     = user32w.NewProc("GetMessageW")
	procTranslateMsg   = user32w.NewProc("TranslateMessage")
	procDispatchMsg    = user32w.NewProc("DispatchMessageW")
	procPostQuitMsg    = user32w.NewProc("PostQuitMessage")
	procCreatePopupM   = user32w.NewProc("CreatePopupMenu")
	procAppendMenu     = user32w.NewProc("AppendMenuW")
	procTrackPopupMenu = user32w.NewProc("TrackPopupMenu")
	procGetCursorPos   = user32w.NewProc("GetCursorPos")
	procSetForeground  = user32w.NewProc("SetForegroundWindow")
	procLoadIcon       = user32w.NewProc("LoadIconW")
	procGetModuleH     = kernel32w.NewProc("GetModuleHandleW")
)

const (
	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	wmUser       = 0x0400
	wmTrayCB     = wmUser + 1
	wmCommand    = 0x0111
	wmRButtonUp  = 0x0205
	wmLButtonUp  = 0x0202
	wmDestroy    = 0x0002

	idmStop = 1001

	mfString = 0x00000000

	tpmLeftAlign  = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002

	idiApplication = 32512
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

type point struct{ X, Y int32 }

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

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type WindowsIndicator struct {
	mu      sync.Mutex
	hwnd    syscall.Handle
	nid     notifyIconData
	onStop  func()
	started bool
	menu    syscall.Handle
}

func New() (mrwebrtc.SessionIndicator, error) {
	return &WindowsIndicator{}, nil
}

func utf16ToArr(s string, arr []uint16) {
	u := syscall.StringToUTF16(s)
	for i := 0; i < len(arr); i++ {
		if i < len(u) {
			arr[i] = u[i]
		} else {
			arr[i] = 0
		}
	}
}

// ensureWindow crea una ventana oculta que recibe los mensajes de la bandeja.
func (w *WindowsIndicator) ensureWindow() error {
	if w.started {
		return nil
	}
	hInst, _, _ := procGetModuleH.Call(0)
	className := syscall.StringToUTF16Ptr("MiRemoteTrayClass")

	wndProc := syscall.NewCallback(func(hwnd syscall.Handle, m uint32, wParam, lParam uintptr) uintptr {
		switch m {
		case wmTrayCB:
			// Clic derecho (o izquierdo) sobre el icono: mostrar menú.
			if lParam == wmRButtonUp || lParam == wmLButtonUp {
				w.showMenu(hwnd)
			}
		case wmCommand:
			if wParam&0xffff == idmStop {
				w.fireStop()
			}
		case wmDestroy:
			procPostQuitMsg.Call(0)
		}
		r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(m), wParam, lParam)
		return r
	})

	wc := wndClassEx{
		lpfnWndProc:   wndProc,
		hInstance:     syscall.Handle(hInst),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindow.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("MiRemote"))),
		0, 0, 0, 0, 0, 0, 0,
		uintptr(hInst), 0,
	)
	w.hwnd = syscall.Handle(hwnd)
	w.started = true

	// Bucle de mensajes en su propia goroutine.
	go w.messageLoop()
	return nil
}

func (w *WindowsIndicator) messageLoop() {
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMsg.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (w *WindowsIndicator) showMenu(hwnd syscall.Handle) {
	hMenu, _, _ := procCreatePopupM.Call()
	procAppendMenu.Call(hMenu, mfString, idmStop,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Detener sesión remota"))))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForeground.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(hMenu, tpmLeftAlign|tpmBottomAlign|tpmRightButton,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
}

func (w *WindowsIndicator) fireStop() {
	w.mu.Lock()
	f := w.onStop
	w.mu.Unlock()
	if f != nil {
		f()
	}
}

func (w *WindowsIndicator) Show(deviceName string, perms []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureWindow(); err != nil {
		return err
	}

	hIcon, _, _ := procLoadIcon.Call(0, uintptr(idiApplication))

	w.nid = notifyIconData{
		hWnd:             w.hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip | nifInfo,
		uCallbackMessage: wmTrayCB,
		hIcon:            syscall.Handle(hIcon),
	}
	w.nid.cbSize = uint32(unsafe.Sizeof(w.nid))
	utf16ToArr("MiRemote — Sesión remota activa: "+deviceName, w.nid.szTip[:])
	utf16ToArr("Sesión remota activa: "+deviceName, w.nid.szInfo[:])
	utf16ToArr("MiRemote", w.nid.szInfoTitle[:])

	procShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&w.nid)))
	return nil
}

func (w *WindowsIndicator) Hide() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return nil
	}
	procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&w.nid)))
	return nil
}

func (w *WindowsIndicator) OnStop(f func()) {
	w.mu.Lock()
	w.onStop = f
	w.mu.Unlock()
}
