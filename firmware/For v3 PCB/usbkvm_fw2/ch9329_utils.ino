/*
  ch9329_utils.ino

  This file contain codes that communicate with the ch9329 IC via UART0
*/

// Checksum = sum of all bytes except last
uint8_t calcChecksum(uint8_t* data, uint8_t len) {
  uint8_t sum = 0;
  for (uint8_t i = 0; i < len; i++) sum += data[i];
  return sum;
}

// Helper to write an array of uint8_t to CH9329
void Serial0_writeBuf(const uint8_t* data, uint8_t len) {
  for (uint8_t i = 0; i < len; i++) {
    Serial0_write(data[i]);
  }
}

// flush the serial RX (blocking)
void flush_cmd_resp() {
  delay(100);
  while (Serial0_available()) {
    uint8_t b = Serial0_read();
    USBSerial_print("0x");
    if (b < 0x10) USBSerial_print("0");
    USBSerial_print(b, HEX);
    USBSerial_print(" ");
  }
  USBSerial_println("");
}


// Send a CH9329 cmd via UART0
void send_cmd(uint8_t cmd, uint8_t* data, uint8_t length) {
  uint8_t sum = 0;
  Serial0_write(0x57);
  sum += 0x57;
  Serial0_write(0xAB);
  sum += 0xAB;
  Serial0_write(0x00);

  Serial0_write(cmd);
  sum += cmd;
  Serial0_write(length);
  sum += length;
  for (int i = 0; i < length; i++) {
    Serial0_write(data[i]);
    sum += data[i];
  }
  Serial0_write(sum);
}