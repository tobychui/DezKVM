/*
  DezKVM Production Form Factor
  Firmware for PCB design PFFv1

  Author: tobychui

  Upload Settings
  CH552G
  24Mhz (Internal)

  Currently supported commands (hex):
  'm' - Switch USB mass storage to KVM side
  'n' - Switch USB mass storage to remote computer
  'u' - Return the UUID of this device
  'a' - Report status of ATX (if ENABLE_ATX_CTRL is defined)
  'i' - Enable automatic ATX status report (if ENABLE_ATX_CTRL is defined)
  'o' - Disable automatic ATX status report (if ENABLE_ATX_CTRL is defined)
  'p' - Press down the power button (if ENABLE_ATX_CTRL is defined)
  's' - Release the power button (if ENABLE_ATX_CTRL is defined)
  'r' - Press down the reset button (if ENABLE_ATX_CTRL is defined)
  'd' - Release the reset button (if ENABLE_ATX_CTRL is defined)
  '0' - Set LED PROG to LOW
  '1' - Set LED PROG to HIGH
  '2' - Set LED PROG to fast blink
  '3' - Set LED PROG to slow blink
  'f' - Reset echo, return 0xFFFF to host

  The reply follows the format:
  <Length> <Command Identifier> <Payload>
  Note: Length includes the Command Identifier and Payload bytes only, not the Length byte itself.

  Replies command structure:
  0x01 0xFE - Unknown command
  0x01 0xFF - Host read reset echo
  <Length> 0x60 <UUID String> - Reserved for future use
  0x02 0x61 <Status Byte> - ATX status report
  <Length> 0x62 <UUID String> - Device UUID
*/
#include <Serial.h>

/* Build flags */
//#define ENABLE_DEBUG 1     //Enable debug print to Serial, do not use this in IP-KVM setup
#define ENABLE_ATX_CTRL 1  //Enable ATX power control

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

/* Software definations */
#define USB_PWR_SW_PWR_DELAY 100  //ms
#define USB_PWR_SW_DATA_DELAY 10  //ms


/* Runtime variables */
uint8_t atx_status[2] = { 0, 0 };  //PWR LED, HDD LED
uint8_t usb_ms_side = USB_MS_SIDE_REMOTE_PC;
char c;
int led_tmp;
bool led_status = true;  //Default LED PROG state is high, on every command recv it switch state
bool enable_auto_atx_report = false; //Enable automatic ATX status report

/* LED Control Variables */
#define LED_MODE_AUTO 0      //Auto toggle on command receive
#define LED_MODE_OFF 1       //LED forced off
#define LED_MODE_ON 2        //LED forced on
#define LED_MODE_FAST 3      //Fast blink (200ms)
#define LED_MODE_SLOW 4      //Slow blink (1000ms)
uint8_t led_mode = LED_MODE_SLOW; //Default LED mode is slow blink
unsigned long led_last_toggle = 0;
bool led_blink_state = false;

/* Function Prototypes */
void report_status();
void update_atx_led_status();
void switch_usbms_to_kvm();
void switch_usbms_to_remote();
void print_device_uuid();
void get_usb_mass_storage_side();

//execute_cmd match and execute host to remote commands
void execute_cmd(char c) {
  switch (c) {
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
    case 'y':
      //Get USB mass storage side
      get_usb_mass_storage_side();
      break;
#ifdef ENABLE_ATX_CTRL
    case 'a':
      //Report status of ATX
      report_status();
      break;
    case 'i':
      //Enable automatic ATX status report
      enable_auto_atx_report = true;
      break;
    case 'o':
      //Disable automatic ATX status report
      enable_auto_atx_report = false;
      break;
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
#endif /* ~ENABLE_ATX_CTRL */
    /* LED Control Signals */
    case '0':
      //Set LED PROG to LOW
      led_mode = LED_MODE_OFF;
      digitalWrite(LED_PROG, LOW);
      break;
    case '1':
      //Set LED PROG to HIGH
      led_mode = LED_MODE_ON;
      digitalWrite(LED_PROG, HIGH);
      break;
    case '2':
      //Set LED PROG to fast blink
      led_mode = LED_MODE_FAST;
      led_last_toggle = millis();
      led_blink_state = false;
      break;
    case '3':
      //Set LED PROG to slow blink
      led_mode = LED_MODE_SLOW;
      led_last_toggle = millis();
      led_blink_state = false;
      break;
    /* ~LED Control Signals */
    case 'f':
      //Reset echo, return 0xFFFF to host
      USBSerial_print(0x01); //Length
      USBSerial_print(0xFF); //Ack: Reset Echo
      break;
    default:
      //Unknown command
      USBSerial_println(0x01); //Length
      USBSerial_println(0xFE); // Ack: Error: Unknown Command
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
#ifdef ENABLE_DEBUG
  USBSerial_println("[WARNING] The current firmware has debug mode turned on. Do not use in production or IP-KVM setup!");
#endif /* ENABLE_DEBUG */
  //Switch the USB thumbnail to host
  switch_usbms_to_kvm();
  delay(1000);
}

void loop() {
  if (USBSerial_available()) {
    c = USBSerial_read();
#ifdef ENABLE_DEBUG
    USBSerial_print("[DEBUG] Serial Recv: ");
    USBSerial_println(c);
#endif /* ENABLE_DEBUG */
    execute_cmd(c);

    //Only toggle LED on command receive if in auto mode
    if (led_mode == LED_MODE_AUTO) {
      led_status = !led_status;
      digitalWrite(LED_PROG, led_status ? HIGH : LOW);
    }
  }

  //Handle LED blinking modes
  if (led_mode == LED_MODE_FAST || led_mode == LED_MODE_SLOW) {
    unsigned long current_time = millis();
    unsigned long blink_interval = (led_mode == LED_MODE_FAST) ? 200 : 1000;

    if (current_time - led_last_toggle >= blink_interval) {
      led_blink_state = !led_blink_state;
      digitalWrite(LED_PROG, led_blink_state ? HIGH : LOW);
      led_last_toggle = current_time;
    }
  }

#ifdef ENABLE_ATX_CTRL
  update_atx_led_status();
  if (enable_auto_atx_report) {
    report_status();
  }
#endif /* ENABLE_ATX_CTRL */

  delay(100);
}
