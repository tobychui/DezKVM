/*
  RemdesKVM USB-KVM
  Firmware for PCB design v4 or above

  Author: tobychui

  Upload Settings
  CH552G  
  24Mhz (Internal)
*/
#include <Serial.h>

//Pins definations
#define ATX_PWR_LED 15
#define ATX_HDD_LED 16
#define ATX_RST_BTN 33
#define ATX_PWR_BTN 34
#define USB_MS_SW 32

//Custom header bytes for RemdesKVM
#define HEADER_BYTE_0 0x58
#define HEADER_BYTE_1 0xEE
#define MAX_DATA_LEN 32  //Max resp data length from remdesKVM control signal

//Runtime
uint8_t trigger = 0;  //Counter for header matching logic
uint8_t c = 0x00;
uint8_t data[MAX_DATA_LEN];

// execute a remdeskvm only command
void execute_remdeskvm_cmd(uint8_t cmd) {
  uint8_t res = 0, len = 0, sum = 0;
  memset(data, 0, sizeof(data));
  res = cmd | 0x80;
  switch (cmd) {
    case 0x00:
      //ATX Power button ON
      digitalWrite(ATX_PWR_BTN, HIGH);
      break;
    case 0x01:
      //ATX Power button OFF
      digitalWrite(ATX_PWR_BTN, LOW);
      break;
    case 0x02:
      //Reset button ON
      digitalWrite(ATX_RST_BTN, HIGH);
      break;
    case 0x03:
      //Reset button OFF
      digitalWrite(ATX_RST_BTN, LOW);
      break;
    case 0x04:
      //Get LED status
      data[0] = digitalRead(ATX_PWR_LED);
      data[1] = digitalRead(ATX_HDD_LED);
      len = 2;
      break;
    default:
      //Unknown command
      res = cmd | 0x0C;
  }

  //Write the result back to control software
  USBSerial_print(HEADER_BYTE_0);  //Header byte
  sum += HEADER_BYTE_0;
  USBSerial_print(HEADER_BYTE_1);  //Header byte
  sum += HEADER_BYTE_1;
  USBSerial_print(res);  //Status
  sum += res;
  USBSerial_print(len);  //Length
  sum += len;
  for (int i = 0; i < len; i++) {
    USBSerial_print(data[i]);  //Data payload
    sum += data[i];
  }
  USBSerial_print(sum);  //Checksum
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

void setup() {
  //Setup ATX Pins
  pinMode(ATX_PWR_LED, INPUT);
  pinMode(ATX_HDD_LED, INPUT);
  pinMode(ATX_RST_BTN, OUTPUT);
  pinMode(ATX_PWR_BTN, OUTPUT);
  pinMode(USB_MS_SW, OUTPUT); //Currently not used

  digitalWrite(ATX_RST_BTN, LOW);
  digitalWrite(ATX_PWR_BTN, LOW);

  //Serial0_begin(9600);  // CH9329 UART default baudrate (uncomment this before running -cfgchip)
  Serial0_begin(19200);
  delay(100);
  setup_keyboard_chip_cfg();
}

void loop() {
  // Read characters into buffer
  if (USBSerial_available()) {
    c = USBSerial_read();
    Serial0_write(c);
    if (c == HEADER_BYTE_0 && trigger == 0) {
      trigger++;
    } else if (c == HEADER_BYTE_1 && trigger == 1) {
      trigger++;
    } else if (trigger == 2) {
      //This is the command for remdeskvm
      execute_remdeskvm_cmd(c);
      trigger = 0;
    }
  }

  if (Serial0_available()) {
    c = Serial0_read();
    USBSerial_write(c);
  }
}