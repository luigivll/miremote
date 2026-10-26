//go:build windows

// Package input implementa InputSink sobre la API SendInput de Windows.
//
// Detalles que se suelen hacer mal y aquí se cuidan:
//   - Coordenadas absolutas del ratón en el rango 0..65535 relativas al
//     ESCRITORIO VIRTUAL completo, usando MOUSEEVENTF_VIRTUALDESK, para que
//     funcione con varios monitores.
//   - El texto se inyecta como Unicode con KEYEVENTF_UNICODE (code points UTF-16),
//     no como scancodes, de modo que caracteres como € o acentos funcionan sin
//     depender de la distribución de teclado activa.
//   - ReleaseAll lleva registro de lo que está pulsado y lo suelta al cerrar o
//     perder la conexión, para no dejar teclas/botones trabados.
package input

import (
	"sync"
	"syscall"
	"unsafe"

	mrwebrtc "example.com/miremote/agent/internal/webrtc"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSendInput    = user32.NewProc("SendInput")
	procGetSystemMet = user32.NewProc("GetSystemMetrics")
	procVkKeyScan    = user32.NewProc("VkKeyScanW")
	procMapVirtualK  = user32.NewProc("MapVirtualKeyW")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfRightDown   = 0x0008
	mouseeventfRightUp     = 0x0010
	mouseeventfMiddleDown  = 0x0020
	mouseeventfMiddleUp    = 0x0040
	mouseeventfWheel       = 0x0800
	mouseeventfHWheel      = 0x01000
	mouseeventfAbsolute    = 0x8000
	mouseeventfVirtualDesk = 0x4000

	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004
	keyeventfScancode = 0x0008

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	wheelDelta = 120
)

// mouseInput y keyInput coinciden con las estructuras INPUT de Win32.
type mouseInput struct {
	typ       uint32
	dx        int32
	dy        int32
	mouseData uint32
	dwFlags   uint32
	time      uint32
	dwExtra   uintptr
	_pad      [8]byte // INPUT union alcanza el tamaño del miembro mayor
}

type keyInput struct {
	typ     uint32
	wVk     uint16
	wScan   uint16
	dwFlags uint32
	time    uint32
	dwExtra uintptr
	_pad    [8]byte
}

type WindowsInput struct {
	mu           sync.Mutex
	pressedKeys  map[string]bool // code DOM -> pulsada
	pressedBtns  map[string]bool // button -> pulsado
}

// New crea el InputSink de Windows.
func New() (mrwebrtc.InputSink, error) {
	return &WindowsInput{
		pressedKeys: make(map[string]bool),
		pressedBtns: make(map[string]bool),
	}, nil
}

func getMetric(n int) int32 {
	r, _, _ := procGetSystemMet.Call(uintptr(n))
	return int32(r)
}

func sendInputs(n int, p unsafe.Pointer, size uintptr) {
	procSendInput.Call(uintptr(n), uintptr(p), size)
}

func (w *WindowsInput) Apply(ev mrwebrtc.InputEvent) error {
	switch ev.Type {
	case "mouse.move":
		w.mouseMove(ev.X, ev.Y)
	case "mouse.down":
		w.mouseButton(ev.Button, true)
	case "mouse.up":
		w.mouseButton(ev.Button, false)
	case "mouse.wheel":
		w.mouseWheel(ev.DX, ev.DY)
	case "key.down":
		w.key(ev.Code, true)
	case "key.up":
		w.key(ev.Code, false)
	case "text":
		w.typeText(ev.Value)
	}
	return nil
}

// mouseMove convierte coordenadas normalizadas 0..1 del escritorio virtual a
// 0..65535 absolutas, con VIRTUALDESK para abarcar todos los monitores.
func (w *WindowsInput) mouseMove(x, y float64) {
	// Normalizado respecto al escritorio virtual completo.
	absX := int32(x*65535.0 + 0.5)
	absY := int32(y*65535.0 + 0.5)
	if absX < 0 {
		absX = 0
	}
	if absX > 65535 {
		absX = 65535
	}
	if absY < 0 {
		absY = 0
	}
	if absY > 65535 {
		absY = 65535
	}

	in := mouseInput{
		typ:     inputMouse,
		dx:      absX,
		dy:      absY,
		dwFlags: mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualDesk,
	}
	sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))
}

func (w *WindowsInput) mouseButton(button string, down bool) {
	var flag uint32
	switch button {
	case "left", "":
		flag = pick(down, mouseeventfLeftDown, mouseeventfLeftUp)
	case "right":
		flag = pick(down, mouseeventfRightDown, mouseeventfRightUp)
	case "middle":
		flag = pick(down, mouseeventfMiddleDown, mouseeventfMiddleUp)
	default:
		return
	}
	in := mouseInput{typ: inputMouse, dwFlags: flag}
	sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))

	w.mu.Lock()
	if down {
		w.pressedBtns[button] = true
	} else {
		delete(w.pressedBtns, button)
	}
	w.mu.Unlock()
}

func (w *WindowsInput) mouseWheel(dx, dy float64) {
	if dy != 0 {
		in := mouseInput{
			typ:       inputMouse,
			mouseData: uint32(int32(-dy)), // rueda: hacia arriba = positivo
			dwFlags:   mouseeventfWheel,
		}
		sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))
	}
	if dx != 0 {
		in := mouseInput{
			typ:       inputMouse,
			mouseData: uint32(int32(dx)),
			dwFlags:   mouseeventfHWheel,
		}
		sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))
	}
}

// key inyecta una tecla física (DOM code) como scancode. Se mapea el code DOM a
// un virtual-key de Windows y de ahí a scancode. Para texto con significado
// Unicode se usa typeText.
func (w *WindowsInput) key(domCode string, down bool) {
	vk, ok := domCodeToVK[domCode]
	if !ok {
		return
	}
	scan, _, _ := procMapVirtualK.Call(uintptr(vk), 0 /* MAPVK_VK_TO_VSC */)
	var flags uint32 = keyeventfScancode
	if !down {
		flags |= keyeventfKeyUp
	}
	in := keyInput{
		typ:     inputKeyboard,
		wScan:   uint16(scan),
		dwFlags: flags,
	}
	sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))

	w.mu.Lock()
	if down {
		w.pressedKeys[domCode] = true
	} else {
		delete(w.pressedKeys, domCode)
	}
	w.mu.Unlock()
}

// typeText inyecta texto Unicode carácter a carácter con KEYEVENTF_UNICODE.
// Cada code point se envía como su(s) unidad(es) UTF-16, down + up.
func (w *WindowsInput) typeText(text string) {
	utf16units := syscall.StringToUTF16(text)
	// StringToUTF16 añade un 0 final; se ignora.
	for _, u := range utf16units {
		if u == 0 {
			continue
		}
		down := keyInput{typ: inputKeyboard, wScan: u, dwFlags: keyeventfUnicode}
		up := keyInput{typ: inputKeyboard, wScan: u, dwFlags: keyeventfUnicode | keyeventfKeyUp}
		sendInputs(1, unsafe.Pointer(&down), unsafe.Sizeof(down))
		sendInputs(1, unsafe.Pointer(&up), unsafe.Sizeof(up))
	}
}

// ReleaseAll suelta todo lo que quedara pulsado.
func (w *WindowsInput) ReleaseAll() error {
	w.mu.Lock()
	keys := make([]string, 0, len(w.pressedKeys))
	for k := range w.pressedKeys {
		keys = append(keys, k)
	}
	btns := make([]string, 0, len(w.pressedBtns))
	for b := range w.pressedBtns {
		btns = append(btns, b)
	}
	w.pressedKeys = make(map[string]bool)
	w.pressedBtns = make(map[string]bool)
	w.mu.Unlock()

	for _, k := range keys {
		if vk, ok := domCodeToVK[k]; ok {
			scan, _, _ := procMapVirtualK.Call(uintptr(vk), 0)
			in := keyInput{typ: inputKeyboard, wScan: uint16(scan),
				dwFlags: keyeventfScancode | keyeventfKeyUp}
			sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))
		}
	}
	for _, b := range btns {
		var flag uint32
		switch b {
		case "left", "":
			flag = mouseeventfLeftUp
		case "right":
			flag = mouseeventfRightUp
		case "middle":
			flag = mouseeventfMiddleUp
		}
		in := mouseInput{typ: inputMouse, dwFlags: flag}
		sendInputs(1, unsafe.Pointer(&in), unsafe.Sizeof(in))
	}
	return nil
}

func (w *WindowsInput) Close() error { return w.ReleaseAll() }

func pick(cond bool, a, b uint32) uint32 {
	if cond {
		return a
	}
	return b
}
