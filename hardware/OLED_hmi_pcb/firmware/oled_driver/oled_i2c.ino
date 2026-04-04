/*
  oled_i2c.ino

  This script handles i2c communication with the 0.96 inch OLED module
  using the SSD1306 driver
*/
#include <SoftI2C.h>
#include "oled_driver.h"

/* OLED runtime variables */
extern uint8_t font_buf[FONT_CHAR_SIZE]; 
uint8_t display_contents[OLED_MAX_CHAR_COUNT]; //ASCII on screen

void OLED_WriteCmd(uint8_t command) {
  uint8_t buf[2];
  buf[0] = 0x00;       // Co = 0, D/C# = 0
  buf[1] = command;
  Wire_writeBytes(OLED_ADDR, buf, 2);
}

void OLED_WriteData(uint8_t data) {
  uint8_t buf[2];
  buf[0] = 0x40;       // Co = 0, D/C# = 1
  buf[1] = data;
  Wire_writeBytes(OLED_ADDR, buf, 2);
}

//Start the OLED screen in desired mode
void initializeOLED() {
  OLED_WriteCmd(SSD1306_DISPLAYOFF);
  OLED_WriteCmd(SSD1306_SETDISPLAYCLOCKDIV);
  OLED_WriteCmd(0x80);  // Suggested value
  OLED_WriteCmd(SSD1306_SETMULTIPLEX);
  OLED_WriteCmd(0x3F);
  OLED_WriteCmd(SSD1306_SETDISPLAYOFFSET);
  OLED_WriteCmd(0x00);
  OLED_WriteCmd(SSD1306_SETSTARTLINE | 0x0);
  OLED_WriteCmd(SSD1306_CHARGEPUMP);
  OLED_WriteCmd(0x14);
  OLED_WriteCmd(SSD1306_MEMORYMODE);
  OLED_WriteCmd(0x00);
  OLED_WriteCmd(SSD1306_SEGREMAP | 0x1);
  OLED_WriteCmd(SSD1306_COMSCANDEC);
  OLED_WriteCmd(SSD1306_SETCOMPINS);
  OLED_WriteCmd(0x12);
  OLED_WriteCmd(SSD1306_SETCONTRAST);
  OLED_WriteCmd(0xCF);
  OLED_WriteCmd(SSD1306_SETPRECHARGE);
  OLED_WriteCmd(0xF1);
  OLED_WriteCmd(SSD1306_SETVCOMDETECT);
  OLED_WriteCmd(0x40);
  OLED_WriteCmd(SSD1306_DISPLAYALLON_RESUME);
  OLED_WriteCmd(SSD1306_NORMALDISPLAY);
  OLED_WriteCmd(SSD1306_DISPLAYON);
}

//Clear the screen with setting all bits to 0
void clearScreen() {
  for (uint8_t i = 0; i < 8; i++) {
    OLED_WriteCmd(0xB0 + i);  // Set page address
    OLED_WriteCmd(0x00);      // Set lower column address
    OLED_WriteCmd(0x10);      // Set higher column address
    for (uint8_t j = 0; j < 128; j++) {
      OLED_WriteData(0x00);
    }
  }
}

//Clear the screen with setting all bits to 1
void lightScreen() {
  for (uint8_t i = 0; i < 8; i++) {
    OLED_WriteCmd(0xB0 + i);  // Set page address
    OLED_WriteCmd(0x00);      // Set lower column address
    OLED_WriteCmd(0x10);      // Set higher column address
    for (uint8_t j = 0; j < 128; j++) {
      OLED_WriteData(0xFF);
    }
  }
}

void OLED_clear_screen(){
  for(int i = 0; i < OLED_MAX_CHAR_COUNT; i++){
    display_contents[i] = 0x20; //Space
  }
  OLED_render_screen();
}

// Set the given char inside display_contents to given ascii code
void OLED_set_char(uint8_t idx, uint8_t ascii){
  display_contents[idx] = ascii;
}

// Render the content from font_buf to target page
// for special font that is no in EEPROM, the master
// control software will send in the raw bytes for the 
// font_buf and call to this function to render to target
// location where that font is suppose to be located
void OLED_render_buf_to_screen(uint8_t idx){
  uint8_t char_col   = idx % OLED_COLS;
  uint8_t char_row   = idx / OLED_COLS;
  uint8_t start_col  = char_col * FONT_GRID_SIZE;
  uint8_t start_page = char_row * OLED_PAGES_PER_CHAR;

  for (uint8_t page = 0; page < OLED_PAGES_PER_CHAR; page++) {
    OLED_WriteCmd(0xB0 + start_page + page);
    OLED_WriteCmd(0x00 | (start_col & 0x0F));   // lower column nibble
    OLED_WriteCmd(0x10 | (start_col >> 4));      // upper column nibble

    for (uint8_t col = 0; col < FONT_GRID_SIZE; col++) {
      uint8_t byte_val = 0;
      for (uint8_t row = 0; row < 8; row++) {
        uint8_t font_row = page * 8 + row;
        uint8_t byte_idx = col / 8;
        uint8_t bit_idx  = 7 - (col % 8);
        if (font_buf[font_row * FONT_BYTES_PER_ROW + byte_idx] & (1 << bit_idx)) {
          byte_val |= (1 << row);
        }
      }
      OLED_WriteData(byte_val);
    }
  }
}

// Render the content from display_contents to screen
void OLED_render_screen(){
  for (int i = 0; i < OLED_MAX_CHAR_COUNT; i++){
    EEPROM_LoadFontData(display_contents[i]);
    OLED_render_buf_to_screen(i);
  }
}

// Render the boot animation frames stored in the EEPROM to the OLED screen
// exit after reciving first bytes from Serial, the master control software 
// will send a byte to trigger exit from the boot animation and render to 
// the screen the content in display_contents
void OLED_ender_boot_animation_loop() {
  uint8_t chunk_buf[32];
  uint8_t led_state = 0;
  
  while (!USBSerial_available()) {
    for (uint8_t frame = 0; frame < BOOT_ANIMATION_FRAME_COUNT; frame++) {
      if (USBSerial_available()) return;

      uint16_t frame_addr = BOOT_ANIMATION_START_ADDR + (uint16_t)frame * BOOT_ANIMATION_FRAME_SIZE;

      for (uint8_t page = 0; page < 8; page++) {
        // Set OLED to write at this page, column 0
        OLED_WriteCmd(0xB0 + page);
        OLED_WriteCmd(0x00);  // Column lower nibble = 0
        OLED_WriteCmd(0x10);  // Column upper nibble = 0

        // Read 128 bytes (4 x 32-byte chunks) from EEPROM and stream to OLED
        for (uint8_t chunk = 0; chunk < 4; chunk++) {
          uint16_t read_addr = frame_addr + (uint16_t)page * 128 + (uint16_t)chunk * 32;
          EEPROM_ReadRaw(read_addr, chunk_buf, 32);
          for (uint8_t i = 0; i < 32; i++) {
            OLED_WriteData(chunk_buf[i]);
          }
        }
      }

      delay(BOOT_ANIMATION_FRAME_DELAY);
      digitalWrite(LED_PIN, led_state);
      led_state = !led_state;
    }
  }
   
  // Clear screen after recv first byte
  OLED_clear_screen();
}
