/*
  RemdesKVM USB-KVM
  Firmware for PCB design v4 or above

  Author: tobychui

  Upload Settings
  CH552G  
  24Mhz (Internal)
*/
#include <Serial.h>

#define MAX_CMD_LEN 32

//Recv buffer from control software
uint8_t cmdBuf[MAX_CMD_LEN];
uint8_t c;
uint8_t cmdIndex = 0;
uint8_t cmdMaxLen = 0;  //Expected length of the LTV command
//Recv buffer from CH9329
uint8_t kbCmdBuf[MAX_CMD_LEN];
uint8_t kbCmdIndex = 0;
uint8_t kbCmdCounter = MAX_CMD_LEN;  //How many index left to terminate

/* Function Prototypes */
//ch9329_utils.ino
void send_cmd(uint8_t cmd, uint8_t* data, uint8_t length);
void flush_cmd_resp();

//keyboard_emu.ino
void keyboard_get_info();
void keyboard_reset();
void handle_keyboard_get_info_reply();
int keyboard_press_key(uint8_t keycode);
int keyboard_release_key(uint8_t keycode);
int keyboard_press_modkey(uint8_t opcode);
int keyboard_release_modkey(uint8_t opcode);

//mouse_emu.ino
void mouse_reset();
int mouse_button_press(uint8_t opcode);
int mouse_button_release(uint8_t opcode);
int mouse_scroll_up(uint8_t tilt);
int mouse_scroll_down(uint8_t tilt);
int mouse_move_relative(uint8_t dx, uint8_t dy, uint8_t wheel);
int mouse_move_absolute(uint8_t x_lsb, uint8_t x_msb, uint8_t y_lsb, uint8_t y_msb);


// Set the USB descriptor exposed to the slave device
void setup_keyboard_chip_cfg() {
  //Set manufacturer string
  uint8_t manufacturer_string[9] = { 0x00, 0x07, 'i', 'm', 'u', 's', 'l', 'a', 'b' };
  send_cmd(0x0B, manufacturer_string, 9);
  flush_cmd_resp();
  //Set product string
  uint8_t product_string[11] = { 0x01, 0x09, 'R', 'e', 'm', 'd', 'e', 's', 'K', 'V', 'M' };
  send_cmd(0x0B, product_string, 11);
  flush_cmd_resp();
}

//This function handles the incoming data from CH9329
void handle_cmd_processing() {
  uint8_t cmd = kbCmdBuf[3];
  uint8_t value = kbCmdBuf[5];  //The value of 1 byte length reply
  switch (cmd) {
    case 0x81:
      //CMD_GET_INFO
      handle_keyboard_get_info_reply();
      break;
    case 0x82:
      //CMD_SEND_KB_GENERAL_DATA
    case 0x84:
      //CMD_SEND_MS_ABS_DATA
    case 0x85:
      //CMD_SEND_MS_REL_DATA
      break;
  }
}

//This function will handle the incoming cmd from RemdesKVM control software
//return 0x00 if success and 0x01 if error
void handle_ctrl_cmd() {
  uint8_t cmd = cmdBuf[1];    //Type of operation
  uint8_t value = cmdBuf[2];  //values for 2 bytes cmd
  switch (cmd) {
    case 0x01:
      //keyboard press
      keyboard_press_key(value);
      break;
    case 0x02:
      //keyboard release
      keyboard_release_key(value);
      break;
    case 0x03:
      //keyboard modifier key press
      keyboard_press_modkey(value);
      break;
    case 0x04:
      //keyboard modifier key release
      keyboard_release_modkey(value);
      break;
    case 0x05:
      //mouse button press
      mouse_button_press(value);
      break;
    case 0x06:
      //mouse button release
      mouse_button_release(value);
      break;
    case 0x07:
      //Mouse scroll up
      mouse_scroll_up(value);
      break;
    case 0x08:
      //Mouse scroll down
      mouse_scroll_down(value);
      break;
    case 0x09:
      //Mouse move absolute
      mouse_move_absolute(cmdBuf[2], cmdBuf[3], cmdBuf[4], cmdBuf[5]);
      break;
    case 0x0A:
      //Mouse move relative
      mouse_move_relative(cmdBuf[2], cmdBuf[3], 0);
      break;
    case 0x0B:
      //Get keyboard LED state
      keyboard_get_info();
      break;
    default:
      //unknown operation, do nothing
      break;
  }
}

void setup() {
  Serial0_begin(9600);  // CH9329 UART default baudd
  delay(100);
  setup_keyboard_chip_cfg();
}

void loop() {
  // Read characters into buffer
  if (USBSerial_available()) {
    c = USBSerial_read();
    if (c == 0xFF) {
      //Reset keyboard state
      keyboard_reset();
      mouse_reset();

      //Reset incoming cmd buff
      memset(cmdBuf, 0, sizeof(cmdBuf));
      cmdIndex = 0;
      cmdMaxLen = MAX_CMD_LEN;

      //Reset ch9329 read buf
      memset(kbCmdBuf, 0, sizeof(kbCmdBuf));
      kbCmdIndex = 0;
      kbCmdCounter = MAX_CMD_LEN;
    } else {
      //Normal cmd
      if (cmdIndex == 0) {
        //This is the length value
        cmdMaxLen = c + 1;
      }
      cmdBuf[cmdIndex] = c;
      cmdIndex++;
      cmdMaxLen--;
      if (cmdMaxLen == 0) {
        //End of ctrl cmd, handle it
        handle_ctrl_cmd();
        //Clear ctrl cmd buf
        memset(cmdBuf, 0, sizeof(cmdBuf));
        cmdIndex = 0;
        cmdMaxLen = MAX_CMD_LEN;
      }
    }
  }

  /*
  while (Serial0_available()) {
    uint8_t b = Serial0_read();
    if (b == 0x57) {
      //Start of a new cmd. Clear the buffer
      memset(kbCmdBuf, 0, sizeof(kbCmdBuf));
      kbCmdIndex = 0;
      kbCmdCounter = MAX_CMD_LEN;
    } else if (kbCmdIndex == 4) {
      //Data length of this cmd
      kbCmdCounter = b + 0x02;  //Sum byte + this Byte
    }

    //Append to buffer
    kbCmdBuf[kbCmdIndex] = b;
    kbCmdIndex++;
    kbCmdCounter--;
    if (kbCmdCounter == 0) {
      //End of command, verify checksum
      uint8_t expected_sum = kbCmdBuf[kbCmdIndex - 1];
      for (int i = 0; i < kbCmdIndex - 1; i++) {
        expected_sum -= kbCmdBuf[i];
      }
      if (expected_sum == 0) {
        //Correct, process the command
        handle_cmd_processing();
      } else {
        //Invalid checksum, skip
      }
      //Clear the buffer
      memset(kbCmdBuf, 0, sizeof(kbCmdBuf));
      kbCmdIndex = 0;
      kbCmdCounter = MAX_CMD_LEN;
      break; //One resp processed. Let write loop take over
    }
  }
  */
}