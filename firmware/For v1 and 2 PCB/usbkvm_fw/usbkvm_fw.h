/*
  usbkvm_fw.h
  Author: tobychui

*/
#ifndef _usbkvm_
#define _usbkvm_

/* ----------- Hardware Configurations -----------*/
#define MIN_KEY_EVENTS_DELAY 20  //ms
#define USB_SW_SEL 32            // Switching pin number for connecting / disconnecting USB Mass Storage Drive
#define HID_SW_SEL 11            // Switching pin number for connecting / disconnecting USB HID

/* ----------- Operation Types -----------*/
#define OPR_TYPE_RESERVED 0x00        // Reserved
#define OPR_TYPE_KEYBOARD_WRITE 0x01  // Keyboard-related operation
#define OPR_TYPE_MOUSE_WRITE 0x02     // Mouse-related operation
#define OPR_TYPE_MOUSE_MOVE 0x03      // Mouse-move operation (Notes: When opr_type is OPR_TYPE_MOUSE_MOVE, opr_subtype is the X position value)
#define OPR_TYPE_MOUSE_SCROLL 0x04    // Mouse scroll (Notes: when opr_type is OPR_TYPE_MOUSE_SCROLL, opr_subtype is up/down and payload is scroll tilt valie (max 127))
#define OPR_TYPE_SWITCH_SET 0x05      // Switch/button operation

#define OPR_TYPE_RESET_INSTR_COUNT 0xFE  // Reset instruction counter
#define OPR_TYPE_DATA_RESET 0xFF         //Reset opr data queue, if state of device is unknown, clear before use

/* Operation Sub-types */
#define SUBTYPE_RESERVED 0x00

/* ----------- Keyboard Subtypes ----------- */
#define SUBTYPE_KEYBOARD_ASCII_WRITE 0x01           // Write ASCII characters (32-127)
#define SUBTYPE_KEYBOARD_ASCII_PRESS 0x02           // Press a key (ASCII 32-127)
#define SUBTYPE_KEYBOARD_ASCII_RELEASE 0x03         // Release a key (ASCII 32-127)
#define SUBTYPE_KEYBOARD_MODIFIER_PRESS 0x04        // Modifier key write (bit flags)
#define SUBTYPE_KEYBOARD_MODIFIER_RELEASE 0x05      // Modifier key press (bit flags)
#define SUBTYPE_KEYBOARD_FUNCTKEY_PRESS 0x06        //Function key press
#define SUBTYPE_KEYBOARD_FUNCTKEY_RELEASE 0x07      //Function key release
#define SUBTYPE_KEYBOARD_OTHERKEY_PRESS 0x08        //Other keys press
#define SUBTYPE_KEYBOARD_OTHERKEY_RELEASE 0x09      //Other keys release
#define SUBTYPE_KEYBOARD_NUMPAD_PRESS 0x0A          //Numpad numeric press
#define SUBTYPE_KEYBOARD_NUMPAD_RELEASE 0x0B        //Numpad numeric release
#define SUBTYPE_KEYBOARD_SPECIAL_PAUSE 0xF9         //Pause | Break (hardware offload)
#define SUBTYPE_KEYBOARD_SPECIAL_PRINT_SCREEN 0xFA  //Print Screen (hardware offload)
#define SUBTYPE_KEYBOARD_SPECIAL_SCROLL_LOCK 0xFB   //Scroll Lock (hardware offload)
#define SUBTYPE_KEYBOARD_SPECIAL_NUMLOCK 0xFC       //Toggle NumLock (hardware offload)
#define SUBTYPE_KEYBOARD_SPECIAL_CTRLALTDEL 0xFD    //Ctrl + Alt + Del (hardware offload)
#define SUBTYPE_KEYBOARD_SPECIAL_RESET 0xFE         //Reset all keypress state
#define SUBTYPE_KEYBOARD_SPECIAL_RESERVED 0xFF      //Reserved

/* Modifier Keys IDs */
#define PAYLOAD_KEY_LEFT_CTRL 0x00
#define PAYLOAD_KEY_LEFT_SHIFT 0x01
#define PAYLOAD_KEY_LEFT_ALT 0x02
#define PAYLOAD_KEY_LEFT_GUI 0x03
#define PAYLOAD_KEY_RIGHT_CTRL 0x04
#define PAYLOAD_KEY_RIGHT_SHIFT 0x05
#define PAYLOAD_KEY_RIGHT_ALT 0x06
#define PAYLOAD_KEY_RIGHT_GUI 0x07

/* Numpad Buttons IDs */
#define PAYLOAD_NUMPAD_0 0x00
#define PAYLOAD_NUMPAD_1 0x01
#define PAYLOAD_NUMPAD_2 0x02
#define PAYLOAD_NUMPAD_3 0x03
#define PAYLOAD_NUMPAD_4 0x04
#define PAYLOAD_NUMPAD_5 0x05
#define PAYLOAD_NUMPAD_6 0x06
#define PAYLOAD_NUMPAD_7 0x07
#define PAYLOAD_NUMPAD_8 0x08
#define PAYLOAD_NUMPAD_9 0x09
#define PAYLOAD_NUMPAD_DOT 0x0A
#define PAYLOAD_NUMPAD_TIMES 0x0B
#define PAYLOAD_NUMPAD_DIV 0x0C
#define PAYLOAD_NUMPAD_PLUS 0x0D
#define PAYLOAD_NUMPAD_MINUS 0x0E
#define PAYLOAD_NUMPAD_ENTER 0x0F
#define PAYLOAD_NUMPAD_NUMLOCK 0x10

/* ----------- Mouse Subtypes -----------*/
#define SUBTYPE_MOUSE_CLICK 0x01
#define SUBTYPE_MOUSE_PRESS 0x02
#define SUBTYPE_MOUSE_RELEASE 0x03
#define SUBTYPE_MOUSE_SETPOS 0x04
#define SUBTYPE_MOUSE_RESET 0x05

/* Mouse Buttons IDs */
#define PAYLOAD_MOUSE_BTN_LEFT 0x01
#define PAYLOAD_MOUSE_BTN_RIGHT 0x02
#define PAYLOAD_MOUSE_BTN_MID 0x03

/* ----------- Switches Subtypes -----------*/
#define SUBTYPE_SWITCH_USBHID 0x01
#define SUBTYPE_SWITCH_USBMASS 0x02

/* ----------- Response Codes ----------- */
#define resp_ok 0x00
#define resp_unknown_opr 0x01
#define resp_invalid_opr_type 0x02
#define resp_invalid_key_value 0x03
#define resp_not_implemented 0x04

/* Debug */
#define resp_start_of_info_msg 0xED
#define resp_end_of_msg 0xEF
#endif