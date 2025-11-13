/*
  RemdesKVM USB-KVM
  Firmware for PCB design v7

  Author: tobychui

  Upload Settings
  CH552G
  24Mhz (Internal)
*/
#include <Serial.h>

/* Build flags */
//#define ENABLE_DEBUG 1     //Enable debug print to Serial, do not use this in IP-KVM setup
//#define ENABLE_ATX_CTRL 1  //Enable ATX power control


/* Enums */
#define USB_MS_SIDE_KVM_HOST 0
#define USB_MS_SIDE_REMOTE_PC 1

/* Pins definations */
#define LED_PROG 14
#define ATX_PWR_LED 15
#define ATX_HDD_LED 16
#define USB_HDMI_PWR 17 //Active high, set to HIGH to enable USB 5V power to HDMI capture card
#define ATX_RST_BTN 33
#define ATX_PWR_BTN 34
#define USB_MS_PWR 31  //Active high, set to HIGH to enable USB 5V power and LOW to disable
#define USB_MS_SW 30   //LOW = remote computer, HIGH = KVM

/* Software definations */
#define USB_PWR_SW_PWR_DELAY 100  //ms
#define USB_PWR_SW_DATA_DELAY 10  //ms


/* Runtime variables */
uint8_t atx_status[2] = { 0, 0 };  //PWR LED, HDD LED
uint8_t usb_ms_side = USB_MS_SIDE_REMOTE_PC;
char c;
int led_tmp;
bool led_status = true;  //Default LED PROG state is high, on every command recv it switch state

/* Function Prototypes */
void report_status();
void update_atx_led_status();
void switch_usbms_to_kvm();
void switch_usbms_to_remote();
void print_device_uuid();

//execute_cmd match and execute host to remote commands
void execute_cmd(char c) {
  switch (c) {
    case 'p':
      //Press down the power button
      digitalWrite(ATX_PWR_BTN, HIGH);
      break;
    case 's':
      //Release the power button
      digitalWrite(ATX_PWR_BTN, LOW);
      break;
    case 'r':
      //Press down the reset button
      digitalWrite(ATX_RST_BTN, HIGH);
      break;
    case 'd':
      //Release the reset button
      digitalWrite(ATX_RST_BTN, LOW);
      break;
    case 'k':
      //Force reset HDMI capture card
      digitalWrite(USB_HDMI_PWR, LOW);
      delay(1000);
      digitalWrite(USB_HDMI_PWR, HIGH);
      break;
    case 'l':
      //Software controlled power RST for HDMI capture card
      digitalWrite(USB_HDMI_PWR, LOW);
      break;
    case 'j':
      //Software controlled power RST for HDMI capture card
      digitalWrite(USB_HDMI_PWR, HIGH);
      break;
    case 'm':
      //Switch USB mass storage to KVM side
      switch_usbms_to_kvm();
      break;
    case 'n':
      //Switch USB mass storage to remote computer
      switch_usbms_to_remote();
      break;
    case 'u':
      //Return the UUID of this device
      print_device_uuid();
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
  pinMode(USB_HDMI_PWR, OUTPUT);
  pinMode(LED_PROG, OUTPUT);

  digitalWrite(LED_PROG, HIGH);
  digitalWrite(ATX_RST_BTN, LOW);
  digitalWrite(ATX_PWR_BTN, LOW);
  digitalWrite(USB_MS_PWR, LOW);
  digitalWrite(USB_HDMI_PWR, LOW);
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
#ifdef ENABLE_DEBUG
  USBSerial_println("[WARNING] The current firmware has debug mode turned on. Do not use in production or IP-KVM setup!");
#endif
  //Switch the USB thumbnail to host
  switch_usbms_to_kvm();
  delay(1000);

  //Set HDMI capture power on
  digitalWrite(USB_HDMI_PWR, LOW);
}

void loop() {
  if (USBSerial_available()) {
    c = USBSerial_read();
#ifdef ENABLE_DEBUG
    USBSerial_print("[DEBUG] Serial Recv: ");
    USBSerial_println(c);
#endif
    execute_cmd(c);
    led_status = !led_status;
    digitalWrite(LED_PROG, led_status ? HIGH : LOW);
  }

#ifdef ENABLE_ATX_CTRL
  update_atx_led_status();
  report_status();
#endif

  delay(100);
}
