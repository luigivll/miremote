//go:build windows

package input

// domCodeToVK mapea códigos físicos de tecla estilo DOM KeyboardEvent.code a
// virtual-keys de Windows. Cubre el teclado habitual; se puede ampliar.
// El texto imprimible se envía mejor como "text" (Unicode); esta tabla es para
// teclas de control y combinaciones (Ctrl, Alt, flechas, F1-F12, etc.).
var domCodeToVK = map[string]uint16{
	// Letras (VK coincide con ASCII mayúscula).
	"KeyA": 0x41, "KeyB": 0x42, "KeyC": 0x43, "KeyD": 0x44, "KeyE": 0x45,
	"KeyF": 0x46, "KeyG": 0x47, "KeyH": 0x48, "KeyI": 0x49, "KeyJ": 0x4A,
	"KeyK": 0x4B, "KeyL": 0x4C, "KeyM": 0x4D, "KeyN": 0x4E, "KeyO": 0x4F,
	"KeyP": 0x50, "KeyQ": 0x51, "KeyR": 0x52, "KeyS": 0x53, "KeyT": 0x54,
	"KeyU": 0x55, "KeyV": 0x56, "KeyW": 0x57, "KeyX": 0x58, "KeyY": 0x59,
	"KeyZ": 0x5A,

	// Dígitos.
	"Digit0": 0x30, "Digit1": 0x31, "Digit2": 0x32, "Digit3": 0x33, "Digit4": 0x34,
	"Digit5": 0x35, "Digit6": 0x36, "Digit7": 0x37, "Digit8": 0x38, "Digit9": 0x39,

	// Control y navegación.
	"Enter": 0x0D, "Escape": 0x1B, "Backspace": 0x08, "Tab": 0x09, "Space": 0x20,
	"ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
	"Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22,
	"Insert": 0x2D, "Delete": 0x2E,
	"CapsLock": 0x14, "PrintScreen": 0x2C,

	// Modificadores.
	"ShiftLeft": 0xA0, "ShiftRight": 0xA1,
	"ControlLeft": 0xA2, "ControlRight": 0xA3,
	"AltLeft": 0xA4, "AltRight": 0xA5,
	"MetaLeft": 0x5B, "MetaRight": 0x5C,

	// Función.
	"F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73, "F5": 0x74, "F6": 0x75,
	"F7": 0x76, "F8": 0x77, "F9": 0x78, "F10": 0x79, "F11": 0x7A, "F12": 0x7B,

	// Signos comunes (por si se envían como key en vez de text).
	"Minus": 0xBD, "Equal": 0xBB, "BracketLeft": 0xDB, "BracketRight": 0xDD,
	"Semicolon": 0xBA, "Quote": 0xDE, "Backquote": 0xC0, "Backslash": 0xDC,
	"Comma": 0xBC, "Period": 0xBE, "Slash": 0xBF,
}
