package remdeshid

/*
	hidcomm.go

	This file contains functions to convert HID commands to bytes
	that can be sent over the USBKVM device

*/
import (
	"fmt"
)

// Append the keyboard event subtypes to the data
func appendKeyboardEventSubtypes(data []byte, cmd HIDCommand) ([]byte, error) {
	/* Keyboard Subtypes */
	if len(cmd.Data) == 1 && cmd.Data[0] >= 32 && cmd.Data[0] <= 127 {
		//Valid ASCII character
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_ASCII_PRESS)
		} else if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_UP {
			data = append(data, SUBTYPE_KEYBOARD_ASCII_RELEASE)
		} else {
			//Key Click
			data = append(data, SUBTYPE_KEYBOARD_ASCII_WRITE)
		}
		data = append(data, cmd.Data[0])
		return data, nil
	} else if isFuncKey(cmd.Data) {
		//Function Key
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_FUNCTKEY_PRESS)
		} else {
			data = append(data, SUBTYPE_KEYBOARD_FUNCTKEY_RELEASE)
		}
		data = append(data, funcKeyToByte(cmd.Data))
		if data[0] == 0xFF {
			return nil, fmt.Errorf("invalid function key: %v", cmd.Data)
		}
		return data, nil
	} else if isModifierKey(cmd.Data) {
		//Modifier Key
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_MODIFIER_PRESS)
		} else {
			data = append(data, SUBTYPE_KEYBOARD_MODIFIER_RELEASE)
		}
		data = append(data, modifierKeyToByte(cmd.Data))
		if data[0] == 0xFF {
			return nil, fmt.Errorf("invalid modifier key: %v", cmd.Data)
		}
		return data, nil
	} else if isOtherKeys(cmd.Data) {
		//Other Keys
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_OTHERKEY_PRESS)
		} else if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_UP {
			data = append(data, SUBTYPE_KEYBOARD_OTHERKEY_RELEASE)
		} else {
			return nil, fmt.Errorf("invalid HID command subtype: %v", cmd.Data)
		}
		data = append(data, nonAsciiKeysToBytes(cmd.Data)...)
		return data, nil
	} else if isNumpadKey(cmd.Data) {
		//Numpad Keys
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_NUMPAD_PRESS)
		} else if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_UP {
			data = append(data, SUBTYPE_KEYBOARD_NUMPAD_RELEASE)
		} else {
			return nil, fmt.Errorf("invalid HID command subtype: %v", cmd.Data)
		}
		data = append(data, numpadKeyToByte(string(cmd.Data)))
		return data, nil
	} else if cmd.Data == "NumLock" {
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_SPECIAL_NUMLOCK)
			data = append(data, 0x00)
			return data, nil
		}
		return nil, fmt.Errorf("numLock do not support key up")
	} else if cmd.Data == "Pause" {
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_SPECIAL_PAUSE)
			data = append(data, 0x00)
			return data, nil
		}

		return nil, fmt.Errorf("pause do not support key up")
	} else if cmd.Data == "PrintScreen" {
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_UP {
			data = append(data, SUBTYPE_KEYBOARD_SPECIAL_PRINT_SCREEN)
			data = append(data, 0x00)
			return data, nil
		}

		return nil, fmt.Errorf("printScreen do not support key down")
	} else if cmd.Data == "ScrollLock" {
		if cmd.EventSubType == FRONTEND_SUBTYPE_KEYBOARD_KEY_DOWN {
			data = append(data, SUBTYPE_KEYBOARD_SPECIAL_SCROLL_LOCK)
			data = append(data, 0x00)
			return data, nil
		}

		return nil, fmt.Errorf("scrollLock do not support key up")
	} else if cmd.Data == "ContextMenu" {
		//Special Key: ContextMenu
		//TODO: Implement ContextMenu
		return nil, fmt.Errorf("ContextMenu not implemented")
	} else if cmd.Data == "Ctrl+Alt+Del" {
		//Special Key: Ctrl+Alt+Del
		data = append(data, SUBTYPE_KEYBOARD_SPECIAL_CTRLALTDEL)
		data = append(data, 0x00)
		return data, nil
	} else {
		return nil, fmt.Errorf("invalid HID command subtype: %v", cmd.Data)
	}
}

// Append the mouse click event subtypes to the data
func appendMouseClickEventSubtypes(data []byte, cmd HIDCommand) ([]byte, error) {
	/* Mouse Click Subtypes */
	isPress := cmd.EventSubType == FRONTEND_MOUSE_PRESS
	if isPress {
		//Mouse Button Press
		data = append(data, 0x02)
	} else {
		//Mouse Button Release
		data = append(data, 0x03)
	}

	if cmd.Data == FRONTEND_MOUSE_BTN_LEFT {
		data = append(data, PAYLOAD_MOUSE_BTN_LEFT)
	} else if cmd.Data == FRONTEND_MOUSE_BTN_MIDDLE {
		data = append(data, PAYLOAD_MOUSE_BTN_MID)
	} else if cmd.Data == FRONTEND_MOUSE_BTN_RIGHT {
		data = append(data, PAYLOAD_MOUSE_BTN_RIGHT)
	} else {
		return nil, fmt.Errorf("invalid HID command subtype: %v", cmd.Data)
	}
	return data, nil
}

// Append the mouse move event subtypes to the data
func appendMouseMoveEventSubtypes(data []byte, cmd HIDCommand) ([]byte, error) {
	//The mouse move command requires x_pos, y_pos, x_sign, y_sign
	x_pos := cmd.PosX
	y_pos := cmd.PosY
	xIsNegative := x_pos < 0
	yIsNegative := y_pos < 0
	x_sign := 0
	y_sign := 0
	if xIsNegative {
		x_pos = -x_pos
		x_sign = 1
	}

	if yIsNegative {
		y_pos = -y_pos
		y_sign = 1
	}

	//The max value for x_pos and y_pos are 0xFE, make sure they are within the range
	if x_pos > 0xFE {
		x_pos = 0xFE
	}

	if y_pos > 0xFE {
		y_pos = 0xFE
	}

	data = append(data, byte(x_pos), byte(y_pos), byte(x_sign), byte(y_sign))
	return data, nil

}

// Append the mouse scroll event subtypes to the data
func appendMouseScrollEventSubtypes(data []byte, cmd HIDCommand) ([]byte, error) {
	//The mouse scroll command PosY contains the scroll value
	//The scroll command require a direction byte and a scroll value byte
	scrollValue := cmd.PosY
	var sensitivity byte = 0x02
	if scrollValue < 0 {
		//Scroll up
		data = append(data, 0x00)        //Up
		data = append(data, sensitivity) //Sensitive
	} else {
		//Scroll down
		data = append(data, 0x01)        //Down
		data = append(data, sensitivity) //Sensitive
	}

	return data, nil
}

// Entry function for converting a HIDCommand to bytes that can be sent over the USBKVM device
func ConvHIDCommandToBytes(cmd HIDCommand) ([]byte, error) {
	// Convert the HID command to bytes
	var data []byte
	if cmd.EventType == FRONTEND_OPR_TYPE_KEYBOARD_WRITE {
		/* Keyboard Write Event */
		data = []byte{OPR_TYPE_KEYBOARD_WRITE}
		return appendKeyboardEventSubtypes(data, cmd)
	} else if cmd.EventType == FRONTEND_OPR_TYPE_MOUSE_WRITE {
		/* Mouse Write Event */
		data = []byte{OPR_TYPE_MOUSE_WRITE}
		return appendMouseClickEventSubtypes(data, cmd)
	} else if cmd.EventType == FRONTEND_OPR_TYPE_MOUSE_MOVE {
		/* Mouse Move Event */
		data = []byte{OPR_TYPE_MOUSE_MOVE}
		return appendMouseMoveEventSubtypes(data, cmd)
	} else if cmd.EventType == FRONTEND_OPR_TYPE_MOUSE_SCROLL {
		/* Mouse Scroll Event */
		data = []byte{OPR_TYPE_MOUSE_SCROLL}
		return appendMouseScrollEventSubtypes(data, cmd)
	} else if cmd.EventType == FRONT_END_OPR_RESET {
		/* Reset Event */
		data = []byte{OPR_TYPE_DATA_RESET, //Reset the data queue
			OPR_TYPE_KEYBOARD_WRITE, SUBTYPE_KEYBOARD_SPECIAL_RESET, 0x00, //Reset the keyboard press state
			OPR_TYPE_MOUSE_WRITE, SUBTYPE_MOUSE_RESET, 0x00} //Reset the mouse press state
		return data, nil
	}

	return nil, fmt.Errorf("invalid HID command type: %s", cmd.EventType)
}
