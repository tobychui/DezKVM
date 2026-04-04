/*
    eeprom_i2c.ino

    This script handles i2c communication with the 24LC32 EEPROM module
    ASCII data is stored in the EEPROM, and can be read back to the OLED screen
    Address mapping as followings. (FONT_CHAR_SIZE = 8 bytes for 8x8 font)

    Characters                ASCII Range    Index Range       EEPROM  Address Range
    Space to / (symbols)      0x20–0x2F      0–15              0x0000–0x007F
    0–9                       0x30–0x39      16–25             0x0080–0x00CF
    : to @ (symbols)          0x3A–0x40      26–32             0x00D0–0x0107
    A–Z                       0x41–0x5A      33–58             0x0108–0x01D7
    [ to ` (symbols)          0x5B–0x60      59–64             0x01D8–0x0207
    a–z                       0x61–0x7A      65–90             0x0208–0x02D7
    { to ~ (symbols)          0x7B–0x7E      91–94             0x02D8–0x02F7

    Following ASCII code is the boot animation frames stored in the EEPROM,
    each frame is 128 x 64 pixels, 1 bit per pixel = 1024 bytes, stored in SSD1306 page format.
    Boot animation data starts at page-aligned address 0x0300.
    Max number of frames can be stored in the EEPROM is 3 (with 256 bytes unused).
    Frame                    Index              EEPROM  Address Range
    Frame 0                  0                  0x0300–0x06FF
    Frame 1                  1                  0x0700–0x0AFF
    Frame 2                  2                  0x0B00–0x0EFF
*/

#include <SoftI2C.h>
#include "oled_driver.h"

extern uint8_t cmd_bytes[CMD_MAX_LENGTH];
extern uint8_t font_buf[FONT_CHAR_SIZE];

// Write font_buf contents to EEPROM for the given ASCII character
void EEPROM_WriteFontData(uint8_t ascii_code) {
  // Calculate EEPROM address: (ascii_code - 0x20) * 32
  uint16_t eeprom_addr = ((uint16_t)(ascii_code - FONT_ASCII_OFFSET)) * FONT_CHAR_SIZE;

  // 24LC32 has 32-byte page writes, so we can write all 32 bytes in one operation
  // Prepare write buffer: [high_addr] [low_addr] [FONT_CHAR_SIZE bytes data]
  uint8_t write_buf[FONT_CHAR_SIZE + 2];
  write_buf[0] = (uint8_t)(eeprom_addr >> 8);    // High byte of address
  write_buf[1] = (uint8_t)(eeprom_addr & 0xFF);  // Low byte of address

  // Copy font data to write buffer
  for (uint8_t i = 0; i < FONT_CHAR_SIZE; i++) {
    write_buf[i + 2] = font_buf[i];
  }

  // Write to EEPROM
  Wire_writeBytes(EEPROM_ADDR, write_buf, FONT_CHAR_SIZE + 2);

  // Wait for write cycle to complete (24LC32 needs ~5ms)
  delay(10);
}

// Load font data from EEPROM into font_buf for the given ASCII character
void EEPROM_LoadFontData(uint8_t ascii_code) {
  // Calculate EEPROM address: (ascii_code - 0x20) * 32
  uint16_t eeprom_read_addr = ((uint16_t)(ascii_code - FONT_ASCII_OFFSET)) * FONT_CHAR_SIZE;

  //Clear font_buf
  for (int i = 0; i < FONT_CHAR_SIZE; i++) {
    font_buf[i] = 0x00;
  }

  //Read to font_buf
  Wire_readRegister16bitAddr(EEPROM_ADDR, eeprom_read_addr, font_buf, FONT_CHAR_SIZE);

  // Wait for read cycle to complete (24LC32 needs ~5ms)
  delay(10);
}

// Write raw data to EEPROM at a 16-bit address (max 32 bytes per call, must not cross page boundary)
void EEPROM_WriteRaw(uint16_t addr, uint8_t* data, uint8_t len) {
  uint8_t write_buf[34]; // 2 address bytes + up to 32 data bytes
  write_buf[0] = (uint8_t)(addr >> 8);
  write_buf[1] = (uint8_t)(addr & 0xFF);
  for (uint8_t i = 0; i < len; i++) {
    write_buf[i + 2] = data[i];
  }
  Wire_writeBytes(EEPROM_ADDR, write_buf, len + 2);
  delay(10);
}

// Read raw data from EEPROM at a 16-bit address into buf
void EEPROM_ReadRaw(uint16_t addr, uint8_t* buf, uint8_t len) {
  Wire_readRegister16bitAddr(EEPROM_ADDR, addr, buf, len);
  delay(5);
}
