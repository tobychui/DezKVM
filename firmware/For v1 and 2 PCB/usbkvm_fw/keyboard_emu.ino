/*
  keyboard_emu.ino
  Author: tobychui

  This code file handle keyboard emulation related functionality
  When opr_type is set to 0x01, the sub-handler will process the
  request here.

 -- Keyboard opr_type --
  0x01 = Keyboard Write

 -- Keyboard opr_subtype --
  0x00 = Reserved
  0x01 = keyboard write
    opr_payload: (ASCII bytes in range of 32 to 127)
  0x02 = keyboard press
    opr_payload: (ASCII bytes in range of 32 to 127)
  0x03 = keyboard release
    opr_payload: (ASCII bytes in range of 32 to 127)

  0x04 = Modifier key combination press 
    opr_payload: See usbkvm_fw.h "Modifier Keys IDs" defination
  0x05 = Modifier key combination release 
    opr_payload: See usbkvm_fw.h "Modifier Keys IDs" defination
  
  0x06 = Function key press
    opr_payload: (key ID same as release)
  0x07 = Function key release
    opr_payload: IDs follows USB_HID numbers
    0xC2 = KEY_F1
    0xC3 = KEY_F2
    0xC4 = KEY_F3
    ...
    0xCC = KEY_F11 
    0xCD = KEY_F12
    0xF0 = KEY_F13
    ...
    0xFB = KEY_F24
  
  0x08 = Other keys press
    opr_payload: (key ID same as release)
  0x09 = Other keys release
    opr_payload: IDs follows USB_HID numbers
    0xDA = KEY_UP_ARROW
    0xD9 = KEY_DOWN_ARROW
    0xD8 = KEY_LEFT_ARROW
    0xD7 = KEY_RIGHT_ARROW
    0xB2 = KEY_BACKSPACE
    0xB3 = KEY_TAB
    0xB0 = KEY_RETURN
    0xB1 = KEY_ESC
    0xD1 = KEY_INSERT
    0xD4 = KEY_DELETE
    0xD3 = KEY_PAGE_UP
    0xD6 = KEY_PAGE_DOWN
    0xD2 = KEY_HOME
    0xD5 = KEY_END
    0xC1 = KEY_CAPS_LOCK

  0x0A = Numpad key press
    opr_payload: See usbkvm_fw.h "Numpad Buttons IDs" defination
  0x0B = Numpad key release
    opr_payload: See usbkvm_fw.h "Numpad Buttons IDs" defination

  -- Special opr_subtypes --
  Notes: These special subtypes do not have opr_payload, but you 
  still need to fill in some value for subtypes as the min opr
  sequence length is 3. 0x00 will be a good opr_payload placeholder.

  0xF9 = Pause | Break (hardware offload)
  0xFA = Print Screen (hardware offload)
  0xFB = Scroll Lock (hardware offload)
  0xFC = Toggle NumLock (hardware offload)
  0xFD = Ctrl + Alt + Delete (hardware offload)
  0xFE = Reset all keys state

  0xFF = Reserved
*/

#include "usbkvm_fw.h"

//Check if the value is a supported ascii code
bool is_ascii(uint8_t value) {
  return value >= 32 && value <= 127;
}

//Check if the value is a valid function key value
bool is_funckey(uint8_t value) {
  return ((value >= 0xC2 && value <= 0xCD) || (value >= 0xF0 && value <= 0xFB));
}

//Check if the key is valid key that do not belongs in ASCII or function key
bool is_validkeys(uint8_t key) {
  if (key >= 0xD7 && key <= 0xDA) {
    //Arrow keys
    return true;
  }

  if (key >= 0xB0 && key <= 0xB3) {
    //ESC, tabs and other left-most keys
    return true;
  }

  if (key >= 0xD1 && key <= 0xD6) {
    //Keys above arrow keys like pageup and down
    return true;
  }

  //CAP_LOCK
  return key == 0xC1;
}

//Check if the nth bit is set in the value
bool isBitSet(uint8_t value, uint8_t n) {
  if (n > 7) return false;  // Ensure n is within 0-7
  return (value & (1 << n)) != 0;
}

//Handle modifying key set or unset
void keyboard_modifying_key_set(bool isPress, uint8_t key) {
  if (isPress) {
    switch (key) {
      case PAYLOAD_KEY_LEFT_CTRL:
        Keyboard_press(KEY_LEFT_CTRL);
        return;
      case PAYLOAD_KEY_LEFT_SHIFT:
        Keyboard_press(KEY_LEFT_SHIFT);
        return;
      case PAYLOAD_KEY_LEFT_ALT:
        Keyboard_press(KEY_LEFT_ALT);
        return;
      case PAYLOAD_KEY_LEFT_GUI:
        Keyboard_press(KEY_LEFT_GUI);
        return;
      case PAYLOAD_KEY_RIGHT_CTRL:
        Keyboard_press(KEY_RIGHT_CTRL);
        return;
      case PAYLOAD_KEY_RIGHT_SHIFT:
        Keyboard_press(KEY_RIGHT_SHIFT);
        return;
      case PAYLOAD_KEY_RIGHT_ALT:
        Keyboard_press(KEY_RIGHT_ALT);
        return;
      case PAYLOAD_KEY_RIGHT_GUI:
        Keyboard_press(KEY_RIGHT_GUI);
        return;
    }
  } else {
    switch (key) {
      case PAYLOAD_KEY_LEFT_CTRL:
        Keyboard_release(KEY_LEFT_CTRL);
        return;
      case PAYLOAD_KEY_LEFT_SHIFT:
        Keyboard_release(KEY_LEFT_SHIFT);
        return;
      case PAYLOAD_KEY_LEFT_ALT:
        Keyboard_release(KEY_LEFT_ALT);
        return;
      case PAYLOAD_KEY_LEFT_GUI:
        Keyboard_release(KEY_LEFT_GUI);
        return;
      case PAYLOAD_KEY_RIGHT_CTRL:
        Keyboard_release(KEY_RIGHT_CTRL);
        return;
      case PAYLOAD_KEY_RIGHT_SHIFT:
        Keyboard_release(KEY_RIGHT_SHIFT);
        return;
      case PAYLOAD_KEY_RIGHT_ALT:
        Keyboard_release(KEY_RIGHT_ALT);
        return;
      case PAYLOAD_KEY_RIGHT_GUI:
        Keyboard_release(KEY_RIGHT_GUI);
        return;
    }
  }
}

//Set a specific numpad key value
void numpad_key_set(bool isPress, uint8_t value) {
  if (isPress) {
    switch (value) {
      case PAYLOAD_NUMPAD_0:
        Keyboard_press('\352');  //0
        return;
      case PAYLOAD_NUMPAD_1:
        Keyboard_press('\341');  //1
        return;
      case PAYLOAD_NUMPAD_2:
        Keyboard_press('\342');  //2
        return;
      case PAYLOAD_NUMPAD_3:
        Keyboard_press('\343');  //3
        return;
      case PAYLOAD_NUMPAD_4:
        Keyboard_press('\344');  //4
        return;
      case PAYLOAD_NUMPAD_5:
        Keyboard_press('\345');  //5
        return;
      case PAYLOAD_NUMPAD_6:
        Keyboard_press('\346');  //6
        return;
      case PAYLOAD_NUMPAD_7:
        Keyboard_press('\347');  //7
        return;
      case PAYLOAD_NUMPAD_8:
        Keyboard_press('\350');  //8
        return;
      case PAYLOAD_NUMPAD_9:
        Keyboard_press('\351');  //9
        return;
      case PAYLOAD_NUMPAD_DOT:
        Keyboard_press('\353');  //.
        return;
      case PAYLOAD_NUMPAD_TIMES:
        Keyboard_press('\335');  //*
        return;
      case PAYLOAD_NUMPAD_DIV:
        Keyboard_press('\334');  // /
        return;
      case PAYLOAD_NUMPAD_PLUS:
        Keyboard_press('\337');  // +
        return;
      case PAYLOAD_NUMPAD_MINUS:
        Keyboard_press('\336');  // -
        return;
      case PAYLOAD_NUMPAD_ENTER:
        Keyboard_press('\340');  // Enter
        return;
      case PAYLOAD_NUMPAD_NUMLOCK:
        Keyboard_press('\333');  // NumLock
        return;
    }
  } else {
    switch (value) {
      case PAYLOAD_NUMPAD_0:
        Keyboard_release('\352');  //0
        return;
      case PAYLOAD_NUMPAD_1:
        Keyboard_release('\341');  //1
        return;
      case PAYLOAD_NUMPAD_2:
        Keyboard_release('\342');  //2
        return;
      case PAYLOAD_NUMPAD_3:
        Keyboard_release('\343');  //3
        return;
      case PAYLOAD_NUMPAD_4:
        Keyboard_release('\344');  //4
        return;
      case PAYLOAD_NUMPAD_5:
        Keyboard_release('\345');  //5
        return;
      case PAYLOAD_NUMPAD_6:
        Keyboard_release('\346');  //6
        return;
      case PAYLOAD_NUMPAD_7:
        Keyboard_release('\347');  //7
        return;
      case PAYLOAD_NUMPAD_8:
        Keyboard_release('\350');  //8
        return;
      case PAYLOAD_NUMPAD_9:
        Keyboard_release('\351');  //9
        return;
      case PAYLOAD_NUMPAD_DOT:
        Keyboard_release('\353');  //.
        return;
      case PAYLOAD_NUMPAD_TIMES:
        Keyboard_release('\335');  //*
        return;
      case PAYLOAD_NUMPAD_DIV:
        Keyboard_release('\334');  // /
        return;
      case PAYLOAD_NUMPAD_PLUS:
        Keyboard_release('\337');  // +
        return;
      case PAYLOAD_NUMPAD_MINUS:
        Keyboard_release('\336');  // -
        return;
      case PAYLOAD_NUMPAD_ENTER:
        Keyboard_release('\340');  // Enter
        return;
      case PAYLOAD_NUMPAD_NUMLOCK:
        Keyboard_release('\333');  // NumLock
        return;
    }
  }
}

//Entry point for keyboard emulation
uint8_t keyboard_emulation(uint8_t subtype, uint8_t value) {
  //Check if the input is a supported ascii value

  switch (subtype) {
    /*
      Alphanumerical Key-events
    */
    case SUBTYPE_KEYBOARD_ASCII_WRITE:
      if (!is_ascii(value))
        return resp_invalid_key_value;
      Keyboard_write(value);
      return resp_ok;
    case SUBTYPE_KEYBOARD_ASCII_PRESS:
      if (!is_ascii(value))
        return resp_invalid_key_value;
      Keyboard_press(value);
      return resp_ok;
    case SUBTYPE_KEYBOARD_ASCII_RELEASE:
      if (!is_ascii(value))
        return resp_invalid_key_value;
      Keyboard_release(value);
      return resp_ok;
    /*
      Modifier Key-events
    */
    case SUBTYPE_KEYBOARD_MODIFIER_PRESS:
      keyboard_modifying_key_set(true, value);
      return resp_ok;
    case SUBTYPE_KEYBOARD_MODIFIER_RELEASE:
      keyboard_modifying_key_set(false, value);
      return resp_ok;
    /*
      Function Key-events (F1 to F24)
    */
    case SUBTYPE_KEYBOARD_FUNCTKEY_PRESS:
      if (!is_funckey(value))
        return resp_invalid_key_value;
      Keyboard_press(value);
      return resp_ok;
    case SUBTYPE_KEYBOARD_FUNCTKEY_RELEASE:
      if (!is_funckey(value))
        return resp_invalid_key_value;
      Keyboard_release(value);
      return resp_ok;
    /*
      Other Key-events
    */
    case SUBTYPE_KEYBOARD_OTHERKEY_PRESS:
      if (!is_validkeys(value))
        return resp_invalid_key_value;
      Keyboard_press(value);
      return resp_ok;
    case SUBTYPE_KEYBOARD_OTHERKEY_RELEASE:
      if (!is_validkeys(value))
        return resp_invalid_key_value;
      Keyboard_release(value);
      return resp_ok;
    /*
      Numpad Key-events
    */
    case SUBTYPE_KEYBOARD_NUMPAD_PRESS:
      if (value > PAYLOAD_NUMPAD_NUMLOCK)
        return resp_invalid_key_value;
      numpad_key_set(true, value);
      return resp_ok;
    case SUBTYPE_KEYBOARD_NUMPAD_RELEASE:
      if (value > PAYLOAD_NUMPAD_NUMLOCK)
        return resp_invalid_key_value;
      numpad_key_set(false, value);
      return resp_ok;
    /* 
      Hardware Offload Key-events 

      These key-events are offloaded to hardware (the MCU)
      for handling the press and release events
    */
    case SUBTYPE_KEYBOARD_SPECIAL_PAUSE:
      Keyboard_press('\320');
      delay(100);
      Keyboard_release('\320');
      return resp_ok;
    case SUBTYPE_KEYBOARD_SPECIAL_PRINT_SCREEN:
      Keyboard_press('\316');
      delay(100);
      Keyboard_release('\316');
      return resp_ok;
    case SUBTYPE_KEYBOARD_SPECIAL_SCROLL_LOCK:
      Keyboard_press('\317');
      delay(100);
      Keyboard_release('\317');
      return resp_ok;
    case SUBTYPE_KEYBOARD_SPECIAL_NUMLOCK:
      Keyboard_press('\333');
      delay(100);
      Keyboard_release('\333');
      return resp_ok;
    case SUBTYPE_KEYBOARD_SPECIAL_CTRLALTDEL:
      // Press Ctrl + Alt + Del
      Keyboard_press(KEY_LEFT_CTRL);
      Keyboard_press(KEY_LEFT_ALT);
      Keyboard_press(KEY_DELETE);
      delay(100);  // Give a little longer time for all keys to be registered together
      // Release Ctrl + Alt + Del in reverse order
      Keyboard_release(KEY_DELETE);
      delay(MIN_KEY_EVENTS_DELAY);
      Keyboard_release(KEY_LEFT_ALT);
      delay(MIN_KEY_EVENTS_DELAY);
      Keyboard_release(KEY_LEFT_CTRL);
      delay(MIN_KEY_EVENTS_DELAY);
      return resp_ok;
    case SUBTYPE_KEYBOARD_SPECIAL_RESET:
      //Release all pressed keys
      Keyboard_releaseAll();
      return resp_ok;
    default:
      return resp_invalid_opr_type;
  }
}