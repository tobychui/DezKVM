/*
   DezKVM OLED Driver Board
   Firmware

   Author: tobychui

   Upload Settings
   CH552G
   24Mhz (Internal)

*/

// Comment the line below to disable touch buttons
//#define ENABLE_TOUCH_BUTTONS

#include <Serial.h>
#include <SoftI2C.h>
#include <TouchKey.h>
#include "oled_driver.h"

/* Runtime command variables */
uint8_t cmd_bytes[CMD_MAX_LENGTH];
uint8_t next_cmd_def = CMD_DEF_LENGTH;
uint8_t cmd_len_remain = 0;
int cmd_write_pos = 0;

/* Font memory buffer */
uint8_t font_buf[FONT_CHAR_SIZE];

/* Emergency reset tracking */
uint8_t enable_reset_bytes = 1;
uint8_t reset_byte_count = 0;

/* Display content buffer, see oled_i2c.ino */
extern uint8_t display_contents[OLED_MAX_CHAR_COUNT];

//process_cmd process cmd_bytes when the whole command is recv
void process_cmd() {
  uint8_t cmd_length = cmd_bytes[0];
  uint8_t cmd_type = cmd_bytes[1];

  if (cmd_type == CMD_CLEAR_SCREEN) {
    // 0x01: Clear screen
    // [0x01] [0x01]
    OLED_clear_screen();
    USBSerial_write(0xF0);
    USBSerial_flush();
  } else if (cmd_type == CMD_SET_SCREEN_CONTENT) {
    // 0x03: Set screen content and render
    // [len] [0x03] [OLED_MAX_CHAR_COUNT ASCII bytes]
    int padding = 0;
    int payload_length = cmd_length - 1; //remove the type byte
    if (payload_length < OLED_MAX_CHAR_COUNT) {
      padding = OLED_MAX_CHAR_COUNT - payload_length;
    }
    for (uint8_t i = 0; i < OLED_MAX_CHAR_COUNT; i++) {
      display_contents[i] = cmd_bytes[2 + i];
    }
    for (uint8_t i = 0; i < padding; i++) {
      display_contents[payload_length + i] = 0x20;
    }
    OLED_render_screen();
    USBSerial_write(0xF0);
    USBSerial_flush();
  } else if (cmd_type == CMD_WRITE_RAW_FONT_BUF) {
    // 0x04: Take the input bytes and fill the font buffer directly
    // font buffer size are either 8x8 or 16x16 depeneds on oled_dirver.h definations
    // [len] [0x04] [render position] [Raw bytes to write to font_buffer]

    // Check if cmd_bytes payload matches the font char size
    // cmd_length = type(1) + position(1) + font_data, so font_data = cmd_length - 2
    if (cmd_length - 2 != FONT_CHAR_SIZE) {
      //corrupted data
      USBSerial_write(0xF1);
      USBSerial_flush();
      return;
    }

    // Check if render position is within range
    uint8_t render_position = cmd_bytes[2];
    if (render_position >= OLED_MAX_CHAR_COUNT) {
      //out of bound
      USBSerial_write(0xF1);
      USBSerial_flush();
      return;
    }

    // Copy data from cmd_bytes to font_buf
    // font data starts at cmd_bytes[3], ends at cmd_bytes[2 + FONT_CHAR_SIZE]
    for (uint8_t i = 3; i < FONT_CHAR_SIZE + 3; i++) {
      font_buf[i - 3] = cmd_bytes[i];
    }
    //Append it to display
    OLED_render_buf_to_screen(render_position);
    USBSerial_write(0xF0);
    USBSerial_flush();
  } else if (cmd_type == CMD_CLEAR_FONT_BUF) {
    // 0x05: Clear font buffer
    for (uint8_t i = 0; i < FONT_CHAR_SIZE; i++) {
      font_buf[i] = 0x20; //Space, no pixels
    }
    USBSerial_write(0xF0);
    USBSerial_flush();
  } else if (cmd_type == CMD_WRITE_CHAR_AT_IDX) {
    // 0x06: Write / append font at given index
    // [len] [0x06] [render position] [ascii_code]
    uint8_t font_idx = cmd_bytes[2];
    if (font_idx >= OLED_MAX_CHAR_COUNT) {
      //out of bound
      USBSerial_write(0xF1);
      USBSerial_flush();
      return;
    }

    // Load and render to screen
    uint8_t ascii_code = cmd_bytes[3];
    EEPROM_LoadFontData(ascii_code);
    OLED_render_buf_to_screen(font_idx);
    USBSerial_write(0xF0);
    USBSerial_flush();
  } else if (cmd_type == CMD_WRITE_FONT_DATA) {
    // 0xC3: Write font data to EEPROM
    // [len] [0xC3] [ascii_code] [FONT_CHAR_SIZE bytes font data]
    uint8_t ascii_code = cmd_bytes[2];
    // Move the font data from cmd to font_buf
    // cmd_bytes[3..cmd_length] = 32 bytes of font data (indices 3 to 34 inclusive)
    for (int i = 3; i <= cmd_length; i++) {
      font_buf[i - 3] = cmd_bytes[i];
    }
    //Write data to EEPROM
    EEPROM_WriteFontData(ascii_code);
    // Success, return 0xF0
    USBSerial_write(0xF0);
    USBSerial_flush();

  } else if (cmd_type == CMD_LOAD_FONT_DATA) {
    // 0xC4: Read font from EEPROM, print to serial
    // [0x03] [0xC4] [ascii_code]
    uint8_t ascii_code = cmd_bytes[2];
    EEPROM_LoadFontData(ascii_code);
    for (uint8_t i = 0; i < FONT_CHAR_SIZE; i++) {
      USBSerial_write(font_buf[i]);
    }
    USBSerial_flush();

  } else if (cmd_type == CMD_LOAD_FONT_TO_OLED) {
    // 0xC5: Load font from EEPROM, display at first position
    // [0x03] [0xC5] [ascii_code]
    uint8_t ascii_code = cmd_bytes[2];
    OLED_clear_screen();
    OLED_set_char(0, ascii_code);
    OLED_render_screen();
    USBSerial_write(0xF0);
    USBSerial_flush();

  } else if (cmd_type == CMD_WRITE_EEPROM_RAW) {
    // 0xC6: Write raw data to EEPROM at 16-bit address
    // [len] [0xC6] [addr_h] [addr_l] [data... up to 32 bytes]
    uint16_t addr = ((uint16_t)cmd_bytes[2] << 8) | cmd_bytes[3];
    uint8_t data_len = cmd_bytes[0] - 3; // length - type(1) - addr(2)
    if (data_len > 32) {
      USBSerial_write(0xF1);
      USBSerial_flush();
      return;
    }
    EEPROM_WriteRaw(addr, &cmd_bytes[4], data_len);
    USBSerial_write(0xF0);
    USBSerial_flush();

  } else if (cmd_type == CMD_READ_EEPROM_RAW) {
    // 0xC7: Read raw data from EEPROM at 16-bit address
    // [0x04] [0xC7] [addr_h] [addr_l] [count]
    uint16_t addr = ((uint16_t)cmd_bytes[2] << 8) | cmd_bytes[3];
    uint8_t read_len = cmd_bytes[4];
    if (read_len > 32) read_len = 32;
    uint8_t read_buf[32];
    EEPROM_ReadRaw(addr, read_buf, read_len);
    for (uint8_t i = 0; i < read_len; i++) {
      USBSerial_write(read_buf[i]);
    }
    USBSerial_flush();
  }
}


void handle_income_byte() {
  uint8_t cmd = USBSerial_read();

  // Check for emergency reset sequence (3 consecutive 0xCF bytes)
  if (cmd == 0xCF && enable_reset_bytes) {
    reset_byte_count++;
    if (reset_byte_count >= 3) {
      // Perform immediate system reset
      cmd_write_pos = 0;
      cmd_len_remain = 0;
      for (int i = 0; i < CMD_MAX_LENGTH; i++) {
        cmd_bytes[i] = 0x00;
      }
      next_cmd_def = CMD_DEF_LENGTH;
      reset_byte_count = 0;
      digitalWrite(LED_PIN, HIGH);
      return;
    }
  } else {
    reset_byte_count = 0;
  }

  if (next_cmd_def == CMD_DEF_LENGTH) {
    // Check if this is the UUID request command
    if (cmd == 'u') {
      //Send UUID and return
      print_device_uuid();
      return;
    }

    // Check if length is longer than max supported cmd bytes
    if (cmd > CMD_MAX_LENGTH) {
      //Cmd too long, not supported
      USBSerial_write(0xF1);
      USBSerial_flush();
      return;
    }

    // Length byte, set next byte to TYPE
    next_cmd_def = CMD_DEF_TYPE;
    // Set remaining length left
    cmd_len_remain = (uint8_t)cmd;
    // Write recv cmd into cmd_bytes
    cmd_bytes[cmd_write_pos] = cmd;
    cmd_write_pos++;
    digitalWrite(LED_PIN, LOW);
    return;
  } else if (next_cmd_def == CMD_DEF_TYPE) {
    //Type byte, set next byte onward to VALUE
    next_cmd_def = CMD_DEF_VALUE;

    // Check if this is EEPROM Write commands,
    // if yes, disable emergency reset function
    if (cmd == CMD_WRITE_EEPROM_RAW || cmd == CMD_WRITE_FONT_DATA) {
      enable_reset_bytes = 0;
    }

    // Write recv cmd into cmd_bytes
    cmd_bytes[cmd_write_pos] = cmd;
    cmd_write_pos++;
    cmd_len_remain--;
    digitalWrite(LED_PIN, HIGH);
    if (cmd_len_remain > 0) {
      //Still cmd bytes to recv
      return;
    }
  } else {
    //Values bytes
    cmd_bytes[cmd_write_pos] = cmd;
    cmd_write_pos++;
    cmd_len_remain--;
    digitalWrite(LED_PIN, (cmd_write_pos % 2) == 0);
    if (cmd_len_remain > 0) {
      //Still cmd bytes to recv
      return;
    }
  }

  // End of cmd, call to process
  process_cmd();

  // Reset state
  cmd_write_pos = 0;
  cmd_len_remain = 0;
  for (int i = 0; i < CMD_MAX_LENGTH; i++) {
    cmd_bytes[i] = 0x00;
  }
  next_cmd_def = CMD_DEF_LENGTH;
  reset_byte_count = 0;
  enable_reset_bytes = 1;
}


void setup() {
  // Setup LED
  pinMode(LED_PIN, OUTPUT);
  digitalWrite(LED_PIN, HIGH);

  // Setup TouchKeys
  touch_key_init();

  // Start SoftI2C
  Wire_begin(I2C_SCL, I2C_SDA);
  initializeOLED();
  clearScreen();

  // Set cmd_bytes to all 0x00
  for (int i = 0; i < CMD_MAX_LENGTH; i++) {
    cmd_bytes[i] = 0x00;
  }
  reset_byte_count = 0;

  // Start Boot Animation
  OLED_ender_boot_animation_loop();
}

void loop() {
  if (USBSerial_available()) {
    handle_income_byte();
  }
#ifdef ENABLE_TOUCH_BUTTONS
  process_touch_key_events();
#endif
}
