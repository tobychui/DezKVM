/*
  mouse_emu.ino

  This file contain code that emulate mouse movements
*/

uint8_t mouse_button_state = 0x00;

//Move the mouse to given position, range 0 - 4096 for both x and y value
int mouse_move_absolute(uint8_t x_lsb, uint8_t x_msb, uint8_t y_lsb, uint8_t y_msb) {
  uint8_t packet[13] = {
    0x57, 0xAB, 0x00, 0x04, 0x07, 0x02,
    mouse_button_state,
    x_lsb,  // X LSB
    x_msb,  // X MSB
    y_lsb,  // Y LSB
    y_msb,  // Y MSB
    0x00,   // Scroll
    0x00    // Checksum placeholder
  };

  packet[12] = calcChecksum(packet, 12);
  Serial0_writeBuf(packet, 13);
  return 0;
}

//Move the mouse to given relative position
int mouse_move_relative(uint8_t dx, uint8_t dy, uint8_t wheel) {
  //Make sure 0x80 is not used
  dx = (dx == 0x80)?0x81:dx;
  dy = (dy == 0x80)?0x81:dy;
  
  uint8_t packet[11] = {
    0x57, 0xAB, 0x00, 0x05, 0x05, 0x01,
    mouse_button_state,
    dx,
    dy,
    wheel,
    0x00  // Checksum placeholder
  };

  packet[10] = calcChecksum(packet, 10);
  Serial0_writeBuf(packet, 11);
  return 0;
}


int mouse_scroll_up(uint8_t tilt) {
  if (tilt > 0x7F)
    tilt = 0x7F;
  if (tilt == 0) {
    //No need to move
    return 0;
  }
  mouse_move_relative(0, 0, tilt);
  return 0;
}

int mouse_scroll_down(uint8_t tilt) {
  if (tilt > 0x7E)
    tilt = 0x7E;
  if (tilt == 0) {
    //No need to move
    return 0;
  }
  mouse_move_relative(0, 0, 0xFF-tilt);
  return 0;
}

//handle mouse button press events
int mouse_button_press(uint8_t opcode) {
  switch (opcode) {
    case 0x01:  // Left
      mouse_button_state |= 0x01;
      break;
    case 0x02:  // Right
      mouse_button_state |= 0x02;
      break;
    case 0x03:  // Middle
      mouse_button_state |= 0x04;
      break;
    default:
      return -1;
  }
  // Send updated button state with no movement
  mouse_move_relative(0, 0, 0);
  return 0;
}

//handle mouse button release events
int mouse_button_release(uint8_t opcode) {
  switch (opcode) {
    case 0x00:  // Release all
      mouse_button_state = 0x00;
      break;
    case 0x01:  // Left
      mouse_button_state &= ~0x01;
      break;
    case 0x02:  // Right
      mouse_button_state &= ~0x02;
      break;
    case 0x03:  // Middle
      mouse_button_state &= ~0x04;
      break;
    default:
      return -1;
  }
  // Send updated button state with no movement
  mouse_move_relative(0, 0, 0);
  return 0;
}

//Reset all mouse clicks state
void mouse_reset(){
  mouse_button_state = 0x00;
  mouse_move_relative(0, 0, 0);
}