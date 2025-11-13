/*
  RemdesKVM USB-KVM
  Firmware for PCB design v5

  Author: tobychui

  Upload Settings
  CH552G  
  24Mhz (Internal)
*/
#include <Serial.h>

/* Build flags */
#define ENABLE_DEBUG 1  //Enable debug print to Serial, do not use this in IP-KVM setup

/* Enums */
#define USB_MS_SIDE_KVM_HOST 0
#define USB_MS_SIDE_REMOTE_PC 1

/* Pins definations */
#define LED_PROG 14
#define ATX_PWR_LED 15
#define ATX_HDD_LED 16
#define ATX_RST_BTN 33
#define ATX_PWR_BTN 34
#define USB_MS_PWR 31  //Active high, set to HIGH to enable USB 5V power and LOW to disable
#define USB_MS_SW 30   //LOW = remote computer, HIGH = KVM

/* Runtime defs */
#define USB_PWR_SW_PWR_DELAY 100  //ms
#define USB_PWR_SW_DATA_DELAY 10  //ms

/* Runtime variables */
uint8_t atx_status[2] = { 0, 0 };  //PWR LED, HDD LED
uint8_t usb_ms_side = 1;           //0 = KVM Controller, 1 = Computer being controlled
char c;
int led_tmp;
bool led_status = true;  //Default LED PROG state is high, on every command recv it switch state

void update_atx_led_status() {
  led_tmp = digitalRead(ATX_PWR_LED);
  atx_status[0] = led_tmp;
  led_tmp = digitalRead(ATX_HDD_LED);
  atx_status[1] = led_tmp;
}

void switch_usbms_to_kvm() {
  if (usb_ms_side == USB_MS_SIDE_KVM_HOST) {
    //Already on the KVM side
    return;
  }

#if ENABLE_DEBUG == 1
  USBSerial_println("[DEBUG] Switching USB Mass Storage node to KVM host side");
#endif
  //Disconnect the power to USB
  digitalWrite(USB_MS_PWR, LOW);
  delay(USB_PWR_SW_PWR_DELAY);

  //Switch over the device
  digitalWrite(USB_MS_PWR, HIGH);
  delay(USB_PWR_SW_DATA_DELAY);
  digitalWrite(USB_MS_SW, HIGH);
  usb_ms_side = USB_MS_SIDE_KVM_HOST;
}

void switch_usbms_to_remote() {
  if (usb_ms_side == USB_MS_SIDE_REMOTE_PC) {
    //Already on Remote Side
    return;
  }

#if ENABLE_DEBUG == 1
  USBSerial_println("[DEBUG] Switching USB Mass Storage node to remote computer side");
#endif
  //Disconnect the power to USB
  digitalWrite(USB_MS_PWR, LOW);
  delay(USB_PWR_SW_PWR_DELAY);

  //Switch over the device
  digitalWrite(USB_MS_PWR, HIGH);
  delay(USB_PWR_SW_DATA_DELAY);
  digitalWrite(USB_MS_SW, LOW);
  usb_ms_side = USB_MS_SIDE_REMOTE_PC;
}

void report_status() {
  //Report status of ATX and USB mass storage switch in 1 byte
  //Bit 0: PWR LED status
  //Bit 1: HDD LED status
  //Bit 2: USB Mass Storage mounted side
  //Bit 3 - 7: Reserved
  uint8_t status = 0x00;
  status |= (atx_status[0] & 0x01);
  status |= (atx_status[1] & 0x01) << 1;
  status |= (usb_ms_side & 0x01) << 2;
#if ENABLE_DEBUG == 1
  USBSerial_print("[DEBUG] ATX State");
  USBSerial_print("PWR=");
  USBSerial_print(atx_status[0]);
  USBSerial_print(" HDD=");
  USBSerial_print(atx_status[1]);
  USBSerial_print(" USB_MS=");
  USBSerial_println(usb_ms_side);
#endif
  USBSerial_print(status);
}

void execute_cmd(char c) {
  switch (c) {
    case '1':
      //Press down the power button
      digitalWrite(ATX_PWR_BTN, HIGH);
      break;
    case '2':
      //Release the power button
      digitalWrite(ATX_PWR_BTN, LOW);
      break;
    case '3':
      //Press down the reset button
      digitalWrite(ATX_RST_BTN, HIGH);
      break;
    case '4':
      //Release the reset button
      digitalWrite(ATX_RST_BTN, LOW);
      break;
    case '5':
      //Switch USB mass storage to KVM side
      switch_usbms_to_kvm();
      break;
    case '6':
      //Switch USB mass storage to remote computer
      switch_usbms_to_remote();
      break;
    default:
      //Unknown command
      break;
  }
}


void setup() {
  pinMode(ATX_PWR_LED, INPUT);
  pinMode(ATX_HDD_LED, INPUT);
  pinMode(ATX_RST_BTN, OUTPUT);
  pinMode(ATX_PWR_BTN, OUTPUT);
  pinMode(USB_MS_PWR, OUTPUT);
  pinMode(USB_MS_SW, OUTPUT);
  pinMode(LED_PROG, OUTPUT);

  digitalWrite(LED_PROG, HIGH);
  digitalWrite(ATX_RST_BTN, LOW);
  digitalWrite(ATX_PWR_BTN, LOW);
  digitalWrite(USB_MS_PWR, LOW);
  digitalWrite(USB_MS_SW, LOW);

  //Blink 10 times for initiations
  for (int i = 0; i < 10; i++) {
    digitalWrite(LED_PROG, HIGH);
    delay(100);
    digitalWrite(LED_PROG, LOW);
    delay(100);
  }
  digitalWrite(LED_PROG, HIGH);
  delay(1000);
  //Switch the USB thumbnail to host
  switch_usbms_to_kvm();
}

void loop() {
  if (USBSerial_available()) {
    c = USBSerial_read();
#if ENABLE_DEBUG == 1
    USBSerial_print("[DEBUG] Serial Recv: ");
    USBSerial_println(c);
#endif
    execute_cmd(c);
    led_status = !led_status;
    digitalWrite(LED_PROG, led_status ? HIGH : LOW);
  }

  //update_atx_led_status();
  //report_status();
  delay(100);
}