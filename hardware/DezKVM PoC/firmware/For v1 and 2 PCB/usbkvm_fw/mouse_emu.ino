/*
  mouse_emu.ino
  Author: tobychui

  This code file handle keyboard emulation related functionality
  When opr_type is set to 0x02, the sub-handler will process the
  request here.

 -- Mouse Write (opr_type = 0x02) --
  0x00 = Reserved
  0x01 = Mouse Click
  0x02 = Mouse Press
  0x03 = Mouse Release
    0x00 = Left Button 
    0x01 = Right Button
    0x02 = Middle button
  0x04 = Mouse Position Presets
    0x00 = [0,0] (top left)
    0x01 = [0, max] (bottom left)
    0x02 = [max, 0] (top right)
    0x03 = [max, max] (bottom right)
  0x05 = Release All Mouse Buttons

-- Mouse Move (opr_type = 0x03) --
  This operation is a special case that
  takes an additional 4 payloads
  byte[0] opr_type (0x03)
  byte[1] val_x (max 0x7E)
  byte[2] val_y (max 0x7E)
  byte[3] signed_x (0x00 = positive, 0x01 = negative)
  byte[4] signed_y (0x00 = positive, 0x01 = negative)

-- Mouse Scroll (opr_type = 0x04) --
  This operation is another special case
  that use opr_subtype field as direction and 
  opr_payload as tilt value
  Note: the value is directly written to the USB_HID report
  the resp is dependent to the OS

  opr_subtype
  0x00 = positive
  0x01 = negative
  opr_value = tilt (max 0x7E) 
*/

#include "usbkvm_fw.h"
//#define ENABLE_MOUSE_DEBUG

//Move cursor back to [0,0] position
void reset_cursor_to_home() {
  for (int i = 0; i < 16; i++) {
    //16 loops should be able to home a mouse to [0,0] from a 4k display
    Mouse_move(-1 * (int8_t)127, -1 * (int8_t)127);
  }
}

//Move the cursor to target position, with multiplier of 10x
void move_cursor_to_pos(uint8_t x, uint8_t y) {
  for (int i = 0; i < 10; i++) {
    Mouse_move((int8_t)x, (int8_t)y);
  }
}
//Handle mouse move, x, y, and direction of x, y (positive: 0x00, negative 0x01)
uint8_t mouse_move(uint8_t ux, uint8_t uy, uint8_t dx, uint8_t dy) {
  if (ux > 0x7E) ux = 0x7E;
  if (uy > 0x7E) uy = 0x7E;

  int8_t x = ux;
  int8_t y = uy;
  if (dx == 0x01)
    x = -x;
  if (dy == 0x01)
    y = -y;
#ifdef ENABLE_MOUSE_DEBUG
  Serial0_write(resp_start_of_info_msg);
  Serial0_write(ux);
  Serial0_write(uy);
  Serial0_write(resp_end_of_msg);
#endif
  Mouse_move(x, y);
  return resp_ok;
}

//Handle mouse move, direction accept 0x00 (down / right) or 0x01 (up / left)
uint8_t mouse_wheel(uint8_t direction, uint8_t utilt) {
  if (utilt >= 0x7E) {
    utilt = 0x7E;
  }

  int8_t tilt = utilt;
  if (direction == 0x01) {
    //Down
    tilt = -tilt;
  }
#ifdef ENABLE_MOUSE_DEBUG
  Serial0_write(resp_start_of_info_msg);
  Serial0_write(tilt);
  Serial0_write(resp_end_of_msg);
#endif
  Mouse_scroll(tilt);
  Mouse_scroll(0);                                  
  return resp_ok;
}

uint8_t mouse_emulation(uint8_t subtype, uint8_t value) {
  switch (subtype) {
    case SUBTYPE_MOUSE_CLICK:
      if (value == PAYLOAD_MOUSE_BTN_LEFT) {
        Mouse_click(MOUSE_LEFT);
      } else if (value == PAYLOAD_MOUSE_BTN_RIGHT) {
        Mouse_click(MOUSE_RIGHT);
      } else if (value == PAYLOAD_MOUSE_BTN_MID) {
        Mouse_click(MOUSE_MIDDLE);
      } else {
        return resp_invalid_key_value;
      }
      return resp_ok;
    case SUBTYPE_MOUSE_PRESS:
      if (value == PAYLOAD_MOUSE_BTN_LEFT) {
        Mouse_press(MOUSE_LEFT);
      } else if (value == PAYLOAD_MOUSE_BTN_RIGHT) {
        Mouse_press(MOUSE_RIGHT);
      } else if (value == PAYLOAD_MOUSE_BTN_MID) {
        Mouse_press(MOUSE_MIDDLE);
      } else {
        return resp_invalid_key_value;
      }
      return resp_ok;
    case SUBTYPE_MOUSE_RELEASE:
      if (value == PAYLOAD_MOUSE_BTN_LEFT) {
        Mouse_release(MOUSE_LEFT);
      } else if (value == PAYLOAD_MOUSE_BTN_RIGHT) {
        Mouse_release(MOUSE_RIGHT);
      } else if (value == PAYLOAD_MOUSE_BTN_MID) {
        Mouse_release(MOUSE_MIDDLE);
      } else {
        return resp_invalid_key_value;
      }
      return resp_ok;
    case SUBTYPE_MOUSE_SETPOS:
      if (value == 0x00) {
        reset_cursor_to_home();
      } else if (value == 0x01) {
        reset_cursor_to_home();
        move_cursor_to_pos(0, 127);
      } else if (value == 0x02) {
        reset_cursor_to_home();
        move_cursor_to_pos(127, 0);
      } else if (value == 0x03) {
        reset_cursor_to_home();
        move_cursor_to_pos(127, 127);
      } else {
        return resp_invalid_key_value;
      }
      return resp_ok;
    case SUBTYPE_MOUSE_RESET:
      Mouse_release(MOUSE_LEFT);
      Mouse_release(MOUSE_RIGHT);
      Mouse_release(MOUSE_MIDDLE);
      Mouse_scroll(0);
      delay(MIN_KEY_EVENTS_DELAY);
      return resp_ok;
    default:
      return resp_invalid_opr_type;
  }
  return resp_invalid_opr_type;
}