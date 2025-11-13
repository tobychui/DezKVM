/*
  ketboard_emu.ino

  This file handle keyboard emulation and key writes
*/
#define KEYBOARD_HID_KEYCODE_LENGTH 6

/* Modifier keycode for CH9329 */
#define MOD_LCTRL 0x01
#define MOD_LSHIFT 0x02
#define MOD_LALT 0x04
#define MOD_LGUI 0x08
#define MOD_RCTRL 0x10
#define MOD_RSHIFT 0x20
#define MOD_RALT 0x40
#define MOD_RGUI 0x80
#define MOD_RENTER 0x58

/* Runtime variables */
uint8_t keyboard_state[KEYBOARD_HID_KEYCODE_LENGTH] = { 0x00, 0x00, 0x00, 0x00, 0x00, 0x00 };
uint8_t keyboard_pressing_key_count = 0;  //No. of key currently pressing, max is 6
uint8_t keyboard_modifiers = 0x00;        //Current modifier key state

//byte 0: Computer connected
//byte 1:
//      bit 0: NUM LOCK
//      bit 1: CAPS LOCK
//      bit 2: SCROLL LOCK
uint8_t keyboard_leds[2] = { 0x00, 0x00 };
extern uint8_t kbCmdBuf[MAX_CMD_LEN];
extern uint8_t kbCmdIndex;
extern uint8_t kbCmdCounter;

/* Function Prototypes */
void send_cmd(uint8_t cmd, uint8_t* data, uint8_t length);

/* Request CH9329 response with current keyboard status */
void keyboard_get_info() {
  send_cmd(0x01, NULL, 0x00);
}

/* Callback handler for setting keyboard status */
void handle_keyboard_get_info_reply() {
  keyboard_leds[0] = kbCmdBuf[6];
  keyboard_leds[1] = kbCmdBuf[7];

  //Print the result to USBSerial
  USBSerial_print(keyboard_leds[0], HEX);  //USB connected
  USBSerial_print(keyboard_leds[1], HEX);  //LED state bits
}


/* Send key combinations to CH9329*/
int keyboard_send_key_combinations() {
  uint8_t packet[14] = {
    0x57, 0xAB, 0x00, 0x02, 0x08,
    keyboard_modifiers, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00
  };

  //Populate the HID keycodes
  for (uint8_t i = 0; i < KEYBOARD_HID_KEYCODE_LENGTH; i++) {
    packet[7 + i] = keyboard_state[i];
  }

  packet[13] = calcChecksum(packet, 13);
  Serial0_writeBuf(packet, 14);
  return 0;
}

/* Release all keypress on the current emulated keyboard */
void keyboard_release_all_keys() {
  uint8_t packet[] = {
    0x57, 0xAB, 0x00, 0x02, 0x08,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00  // checksum placeholder
  };
  packet[13] = calcChecksum(packet, 13);
  Serial0_writeBuf(packet, 14);
}

/* Convert JavaScrpt Keycode into HID keycode */
uint8_t javascript_keycode_to_hid_opcode(uint8_t keycode) {
  // Letters A-Z
  if (keycode >= 65 && keycode <= 90) {
    return (keycode - 65) + 0x04;  // 'A' is 0x04
  }

  // Numbers 1-9 (top row, not numpad)
  if (keycode >= 49 && keycode <= 57) {
    return (keycode - 49) + 0x1E;  // '1' is 0x1E
  }

  //Numpad 1-9
  if (keycode >= 97 && keycode <= 105) {
    return (keycode - 97) + 0x59;  // '1' (numpad) is 0x59
  }

  //F1 to F12
  if (keycode >= 112 && keycode <= 123) {
    return (keycode - 112) + 0x3A;  // 'F1' is 0x3A
  }

  switch (keycode) {
    case 8:
      return 0x2A;  // Backspace
    case 9:
      return 0x2B;  // Tab
    case 13:
      return 0x28;  // Enter
    case 16:
      return 0xE1;  //Left shift
    case 17:
      return 0xE0;  //Left Ctrl
    case 18:
      return 0xE6;  //Left Alt
    case 19:
      return 0x48;  //Pause
    case 20:
      return 0x39;  // Caps Lock
    case 27:
      return 0x29;  // Escape
    case 32:
      return 0x2C;  // Spacebar
    case 33:
      return 0x4B;  // Page Up
    case 34:
      return 0x4E;  // Page Down
    case 35:
      return 0x4D;  // End
    case 36:
      return 0x4A;  // Home
    case 37:
      return 0x50;  // Left Arrow
    case 38:
      return 0x52;  // Up Arrow
    case 39:
      return 0x4F;  // Right Arrow
    case 40:
      return 0x51;  // Down Arrow
    case 44:
      return 0x46;  //Print Screen or F13 (Firefox)
    case 45:
      return 0x49;  // Insert
    case 46:
      return 0x4C;  // Delete
    case 48:
      return 0x27;  // 0 (not Numpads)

    // 49 - 57 Number row 1 - 9 handled above
    // 58 not supported
    case 59:
      return 0x33;  // ';'

    // 60 not supported
    case 61:
      return 0x2E;  // '='

    //62 - 90 not supported
    case 91:
      return 0xE3;  // Left GUI (Windows)
    case 92:
      return 0xE7;  // Right GUI
    case 93:
      return 0x65;  // Menu key
    // 64 - 65 not supported
    case 96:
      return 0x62;  //0 (Numpads)
    //Numpad 1 to 9 handled above
    case 106:
      return 0x55;  //* (Numpads)
    case 107:
      return 0x57;  //+ (Numpads)
    case 109:
      return 0x56;  //- (Numpads)
    case 110:
      return 0x63;  // dot (Numpads)
    case 111:
      return 0x54;  // divide (Numpads)

    // 112 - 123 F1 to F12 handled above
    // 124 - 143 F13 to F32 not supported
    case 144:
      return 0x53;  // Num Lock
    case 145:
      return 0x47;  // Scroll Lock

    /* 
      SPECIAL CASES 
      We are using a specific range of the JavaScript unused keycode
      for special keys that cannot be mapped normally
    */
    case 146:
      return 0x58; //Numpad enter
    case 173:
      return 0x2D; // -
    //End of special cases
    case 186:
      return 0x33;  // ;
    case 187:
      return 0x2E;  // =
    case 188:
      return 0x36;  // ,
    case 189:
      return 0x2D;  // -
    case 190:
      return 0x37;  // .
    case 191:
      return 0x38;  // /
    case 192:
      return 0x35;  // `
    case 219:
      return 0x2F;  // [
    case 220:
      return 0x31;  // backslash
    case 221:
      return 0x30;  // ]
    case 222:
      return 0x34;  // '

    default:
      return 0x00;  // Unknown / unsupported
  }
}

//Send a keyboard press by JavaScript keycode
int keyboard_press_key(uint8_t keycode) {
  //Convert javascript keycode to HID
  keycode = javascript_keycode_to_hid_opcode(keycode);
  if (keycode == 0x00) {
    //Not supported
    return -1;
  }

  // Already pressed? Skip
  for (int i = 0; i < 6; i++) {
    if (keyboard_state[i] == keycode) {
      return 0;  // Already held
    }
  }

  //Get the empty slot in the current HID list
  for (int i = 0; i < 6; i++) {
    if (keyboard_state[i] == 0x00) {
      keyboard_state[i] = keycode;
      keyboard_pressing_key_count++;
      keyboard_send_key_combinations();
      return 0;
    }
  }

  //No space left
  return -1;
}

//Send a keyboard release by JavaScript keycode
int keyboard_release_key(uint8_t keycode) {
  //Convert javascript keycode to HID
  keycode = javascript_keycode_to_hid_opcode(keycode);
  if (keycode == 0x00) {
    //Not supported
    return -1;
  }

  //Get the position where the key is pressed
  for (int i = 0; i < 6; i++) {
    if (keyboard_state[i] == keycode) {
      keyboard_state[i] = 0x00;
      if (keyboard_pressing_key_count > 0) {
        keyboard_pressing_key_count--;
      }
      keyboard_send_key_combinations();
      return 0;
    }
  }

  //That key is not pressed
  return 0;
}

uint8_t keyboard_get_modifier_bit_from_opcode(uint8_t opcode) {
  switch (opcode) {
    case 0x01:
      return MOD_LCTRL;
    case 0x02:
      return MOD_LSHIFT;
    case 0x03:
      return MOD_LALT;
    case 0x04:
      return MOD_LGUI;
    case 0x05:
      return MOD_RCTRL;
    case 0x06:
      return MOD_RSHIFT;
    case 0x07:
      return MOD_RALT;
    case 0x08:
      return MOD_RGUI;
    case 0x09:
      //Ctrl + Alt + Del
      return MOD_LCTRL | MOD_LALT;
    case 0x0A:
      //Win + Shift + S
      return MOD_LSHIFT | MOD_LGUI;
      break;
    //To be added
    default:
      return 0x00;
  }
}

//Set the modifier key bit
int keyboard_press_modkey(uint8_t opcode) {
  if (opcode == 0x00) {
    //Reset the modkeys
    keyboard_modifiers = 0;
    keyboard_send_key_combinations();
    return 0;
  }
  uint8_t mask = keyboard_get_modifier_bit_from_opcode(opcode);
  if (mask == 0x00) {
    return -1;
  }
  keyboard_modifiers |= mask;
  keyboard_send_key_combinations();
  return 0;
}

//Unset modifier key bit
int keyboard_release_modkey(uint8_t opcode) {
  if (opcode == 0x00) {
    //Reset the modkeys
    keyboard_modifiers = 0;
    keyboard_send_key_combinations();
    return 0;
  }
  uint8_t mask = keyboard_get_modifier_bit_from_opcode(opcode);
  if (mask == 0x00) {
    return -1;
  }
  keyboard_modifiers &= ~mask;
  keyboard_send_key_combinations();
  return 0;
}

//Reset all keypress on keyboard
void keyboard_reset() {
  memset(keyboard_state, 0x00, sizeof(keyboard_state));
  keyboard_modifiers = 0x00;
  keyboard_send_key_combinations();
}