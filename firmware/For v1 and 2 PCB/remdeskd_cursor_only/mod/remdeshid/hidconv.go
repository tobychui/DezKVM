package remdeshid

/*
	hidconv.go

	This file contains functions to convert HID commands to bytes
	that can be sent over the USBKVM device
*/

// Operation Types
const (
	// Frontend Opr Types
	FRONTEND_OPR_TYPE_KEYBOARD_WRITE = "kw"
	FRONTEND_OPR_TYPE_MOUSE_WRITE    = "mw"
	FRONTEND_OPR_TYPE_MOUSE_MOVE     = "mm"
	FRONTEND_OPR_TYPE_MOUSE_SCROLL   = "ms"

	// USBKVM Operation Types
	OPR_TYPE_RESERVED       = 0x00
	OPR_TYPE_KEYBOARD_WRITE = 0x01
	OPR_TYPE_MOUSE_WRITE    = 0x02
	OPR_TYPE_MOUSE_MOVE     = 0x03
	OPR_TYPE_MOUSE_SCROLL   = 0x04
	OPR_TYPE_DATA_RESET     = 0xFF
)

// Operation Sub-types
const (
	SUBTYPE_RESERVED = 0x00
)

// Keyboard Subtypes
const (
	// Frontend Keyboard Opr Types
	FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN  = "kd"
	FRONTEND_SUBTYPE_KEYBOARD_KEY_UP    = "ku"
	FRONTEND_SUBTYPE_KEYBOARD_KEY_CLICK = "kc"

	// USBKVM Keyboard Subtypes
	SUBTYPE_KEYBOARD_ASCII_WRITE          = 0x01
	SUBTYPE_KEYBOARD_ASCII_PRESS          = 0x02
	SUBTYPE_KEYBOARD_ASCII_RELEASE        = 0x03
	SUBTYPE_KEYBOARD_MODIFIER_PRESS       = 0x04
	SUBTYPE_KEYBOARD_MODIFIER_RELEASE     = 0x05
	SUBTYPE_KEYBOARD_FUNCTKEY_PRESS       = 0x06
	SUBTYPE_KEYBOARD_FUNCTKEY_RELEASE     = 0x07
	SUBTYPE_KEYBOARD_OTHERKEY_PRESS       = 0x08
	SUBTYPE_KEYBOARD_OTHERKEY_RELEASE     = 0x09
	SUBTYPE_KEYBOARD_NUMPAD_PRESS         = 0x0A
	SUBTYPE_KEYBOARD_NUMPAD_RELEASE       = 0x0B
	SUBTYPE_KEYBOARD_SPECIAL_PAUSE        = 0xF9
	SUBTYPE_KEYBOARD_SPECIAL_PRINT_SCREEN = 0xFA
	SUBTYPE_KEYBOARD_SPECIAL_SCROLL_LOCK  = 0xFB
	SUBTYPE_KEYBOARD_SPECIAL_NUMLOCK      = 0xFC
	SUBTYPE_KEYBOARD_SPECIAL_CTRLALTDEL   = 0xFD
	SUBTYPE_KEYBOARD_SPECIAL_RESET        = 0xFE
	SUBTYPE_KEYBOARD_SPECIAL_RESERVED     = 0xFF

	// Numpad Buttons IDs
	PAYLOAD_KEYBOARD_NUMPAD_0       = 0x00
	PAYLOAD_KEYBOARD_NUMPAD_1       = 0x01
	PAYLOAD_KEYBOARD_NUMPAD_2       = 0x02
	PAYLOAD_KEYBOARD_NUMPAD_3       = 0x03
	PAYLOAD_KEYBOARD_NUMPAD_4       = 0x04
	PAYLOAD_KEYBOARD_NUMPAD_5       = 0x05
	PAYLOAD_KEYBOARD_NUMPAD_6       = 0x06
	PAYLOAD_KEYBOARD_NUMPAD_7       = 0x07
	PAYLOAD_KEYBOARD_NUMPAD_8       = 0x08
	PAYLOAD_KEYBOARD_NUMPAD_9       = 0x09
	PAYLOAD_KEYBOARD_NUMPAD_DOT     = 0x0A
	PAYLOAD_KEYBOARD_NUMPAD_TIMES   = 0x0B
	PAYLOAD_KEYBOARD_NUMPAD_DIV     = 0x0C
	PAYLOAD_KEYBOARD_NUMPAD_PLUS    = 0x0D
	PAYLOAD_KEYBOARD_NUMPAD_MINUS   = 0x0E
	PAYLOAD_KEYBOARD_NUMPAD_ENTER   = 0x0F
	PAYLOAD_KEYBOARD_NUMPAD_NUMLOCK = 0x10

	// Modifier Keys IDs
	PAYLOAD_KEY_LEFT_CTRL   = 0x00
	PAYLOAD_KEY_LEFT_SHIFT  = 0x01
	PAYLOAD_KEY_LEFT_ALT    = 0x02
	PAYLOAD_KEY_LEFT_GUI    = 0x03
	PAYLOAD_KEY_RIGHT_CTRL  = 0x04
	PAYLOAD_KEY_RIGHT_SHIFT = 0x05
	PAYLOAD_KEY_RIGHT_ALT   = 0x06
	PAYLOAD_KEY_RIGHT_GUI   = 0x07
)

const (
	//Frontend Mouse Opr Types
	FRONTEND_MOUSE_CLICK   = "mc"
	FRONTEND_MOUSE_PRESS   = "md"
	FRONTEND_MOUSE_RELEASE = "mu"

	FRONTEND_MOUSE_BTN_LEFT   = "0"
	FRONTEND_MOUSE_BTN_MIDDLE = "1"
	FRONTEND_MOUSE_BTN_RIGHT  = "2"

	// Mouse Subtypes
	SUBTYPE_MOUSE_CLICK   = 0x01 // Mouse button click
	SUBTYPE_MOUSE_PRESS   = 0x02 // Mouse button press
	SUBTYPE_MOUSE_RELEASE = 0x03 // Mouse button release
	SUBTYPE_MOUSE_SETPOS  = 0x04 // Mouse presets position
	SUBTYPE_MOUSE_RESET   = 0x05 // Reset all mouse button states

	// Mouse Buttons IDs
	PAYLOAD_MOUSE_BTN_LEFT  = 0x01
	PAYLOAD_MOUSE_BTN_RIGHT = 0x02
	PAYLOAD_MOUSE_BTN_MID   = 0x03
)

// Control Code
const (
	FRONT_END_OPR_RESET = "reset" // Reset all the mouse and keyboard states
)

// Response Codes
const (
	RESP_OK                = 0x00
	RESP_UNKNOWN_OPR       = 0x01
	RESP_INVALID_OPR_TYPE  = 0x02
	RESP_INVALID_KEY_VALUE = 0x03
	RESP_NOT_IMPLEMENTED   = 0x04
)

//isModifierKey checks if a key is a modifier key
func isModifierKey(key string) bool {
	switch key {
	case "LEFT_Shift", "RIGHT_Shift", "LEFT_Control", "RIGHT_Control", "LEFT_Alt", "RIGHT_Alt", "Meta", "ContextMenu":
		return true
	default:
		return false
	}
}

//Convert modifier key string to byte
func modifierKeyToByte(key string) byte {
	switch key {
	case "LEFT_Shift":
		return PAYLOAD_KEY_LEFT_SHIFT
	case "RIGHT_Shift":
		return PAYLOAD_KEY_RIGHT_SHIFT
	case "LEFT_Control":
		return PAYLOAD_KEY_LEFT_CTRL
	case "RIGHT_Control":
		return PAYLOAD_KEY_RIGHT_CTRL
	case "LEFT_Alt":
		return PAYLOAD_KEY_LEFT_ALT
	case "RIGHT_Alt":
		return PAYLOAD_KEY_RIGHT_ALT
	case "Meta":
		return PAYLOAD_KEY_LEFT_GUI
	case "ContextMenu":
		return PAYLOAD_KEY_RIGHT_GUI
	default:
		return 0xFF
	}
}

//Is a key a function key
func isFuncKey(key string) bool {
	switch key {
	case "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12",
		"F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20", "F21", "F22", "F23", "F24":
		return true
	default:
		return false
	}
}

//Convert function key string to byte
func funcKeyToByte(key string) byte {
	switch key {
	case "F1":
		return 0xC2
	case "F2":
		return 0xC3
	case "F3":
		return 0xC4
	case "F4":
		return 0xC5
	case "F5":
		return 0xC6
	case "F6":
		return 0xC7
	case "F7":
		return 0xC8
	case "F8":
		return 0xC9
	case "F9":
		return 0xCA
	case "F10":
		return 0xCB
	case "F11":
		return 0xCC
	case "F12":
		return 0xCD
	case "F13":
		return 0xF0
	case "F14":
		return 0xF1
	case "F15":
		return 0xF2
	case "F16":
		return 0xF3
	case "F17":
		return 0xF4
	case "F18":
		return 0xF5
	case "F19":
		return 0xF6
	case "F20":
		return 0xF7
	case "F21":
		return 0xF8
	case "F22":
		return 0xF9
	case "F23":
		return 0xFA
	case "F24":
		return 0xFB
	default:
		return 0xFF
	}
}

/* Check for other keys */
func isOtherKeys(key string) bool {
	return nonAsciiKeysToBytes(key)[0] != 0xFF
}

func nonAsciiKeysToBytes(key string) []byte {
	switch key {
	case "ArrowUp":
		return []byte{0xDA}
	case "ArrowDown":
		return []byte{0xD9}
	case "ArrowLeft":
		return []byte{0xD8}
	case "ArrowRight":
		return []byte{0xD7}
	case "Backspace":
		return []byte{0xB2}
	case "Tab":
		return []byte{0xB3}
	case "Enter":
		return []byte{0xB0}
	case "Escape":
		return []byte{0xB1}
	case "Insert":
		return []byte{0xD1}
	case "Delete":
		return []byte{0xD4}
	case "PageUp":
		return []byte{0xD3}
	case "PageDown":
		return []byte{0xD6}
	case "Home":
		return []byte{0xD2}
	case "End":
		return []byte{0xD5}
	case "CapsLock":
		return []byte{0xC1}
	default:
		return []byte{0xFF}
	}
}

/* Numpad keys */
func isNumpadKey(key string) bool {
	return len(key) > 7 && key[:7] == "NUMPAD_"
}

func numpadKeyToByte(key string) byte {
	switch key {
	case "NUMPAD_0":
		return PAYLOAD_KEYBOARD_NUMPAD_0
	case "NUMPAD_1":
		return PAYLOAD_KEYBOARD_NUMPAD_1
	case "NUMPAD_2":
		return PAYLOAD_KEYBOARD_NUMPAD_2
	case "NUMPAD_3":
		return PAYLOAD_KEYBOARD_NUMPAD_3
	case "NUMPAD_4":
		return PAYLOAD_KEYBOARD_NUMPAD_4
	case "NUMPAD_5":
		return PAYLOAD_KEYBOARD_NUMPAD_5
	case "NUMPAD_6":
		return PAYLOAD_KEYBOARD_NUMPAD_6
	case "NUMPAD_7":
		return PAYLOAD_KEYBOARD_NUMPAD_7
	case "NUMPAD_8":
		return PAYLOAD_KEYBOARD_NUMPAD_8
	case "NUMPAD_9":
		return PAYLOAD_KEYBOARD_NUMPAD_9
	case "NUMPAD_.":
		return PAYLOAD_KEYBOARD_NUMPAD_DOT
	case "NUMPAD_*":
		return PAYLOAD_KEYBOARD_NUMPAD_TIMES
	case "NUMPAD_/":
		return PAYLOAD_KEYBOARD_NUMPAD_DIV
	case "NUMPAD_+":
		return PAYLOAD_KEYBOARD_NUMPAD_PLUS
	case "NUMPAD_-":
		return PAYLOAD_KEYBOARD_NUMPAD_MINUS
	case "NUMPAD_Enter":
		return PAYLOAD_KEYBOARD_NUMPAD_ENTER
	default:
		return 0xFF
	}
}
