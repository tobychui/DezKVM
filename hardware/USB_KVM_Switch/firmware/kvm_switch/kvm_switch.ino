/*
  DezKVM USB KVM switch
  Firmware for Prototype v2

  Author: tobychui

  Upload Settings
  CH552G
  24Mhz (Internal)

*/

#include <Serial.h>

/* Hardware Definations */
#define HOST1_LED 14
#define HOST2_LED 16
#define USB_SIGNAL_SW 15
#define HDMI_SW 17 //Low = HDMI1, HIGH = HDMI0
#define TOGGLE_BTN 34 //Physical toggle button
#define SLAVE_SIGNAL_SW 32 //For daisy chain slave switches
#define PWR_ST_1 30
#define PWR_ST_2 31

// Debounce settings
const int DEBOUNCE_MS = 1000;

// State tracking
uint8_t currentHost = 0; // 0 = Host0, 1 = Host1

void setHost(uint8_t host) {
  // host == 0 -> select Host 0: HDMI_SW LOW, USB_SIGNAL_SW LOW
  // host == 1 -> select Host 1: HDMI_SW HIGH, USB_SIGNAL_SW HIGH
  if (host == 0) {
    // The port on the left, since it is the port that powers the HDMI switch chip
    // it must be the default port that turns on when the device initiatae
    digitalWrite(HDMI_SW, LOW); 
    digitalWrite(USB_SIGNAL_SW, LOW);
    digitalWrite(HOST1_LED, HIGH); // Host1 LED ON for Host0 selected
    digitalWrite(HOST2_LED, LOW);
    currentHost = 0;
  } else {
    digitalWrite(HDMI_SW, HIGH); // The right HDMI port
    digitalWrite(USB_SIGNAL_SW, HIGH);
    digitalWrite(HOST1_LED, LOW);
    digitalWrite(HOST2_LED, HIGH); // Host2 LED ON for Host1 selected
    currentHost = 1;
  }
}

void setup() {
  // Setup output pins
  pinMode(HOST1_LED, OUTPUT);
  pinMode(HOST2_LED, OUTPUT);
  pinMode(USB_SIGNAL_SW, OUTPUT);
  pinMode(HDMI_SW, OUTPUT);
  pinMode(SLAVE_SIGNAL_SW, OUTPUT);
  pinMode(TOGGLE_BTN, INPUT);
  pinMode(PWR_ST_1, INPUT);
  pinMode(PWR_ST_2, INPUT);

  // Setup default USB port signal to HOST 1
  setHost(0);
  delay(1000);
}

void loop() {
  // --- Handle physical toggle button with debounce ---
  int reading = digitalRead(TOGGLE_BTN);
  if (reading == HIGH){
    setHost((currentHost == 0)?1:0);
    delay(DEBOUNCE_MS);
  }

  // --- Handle serial input ---
  if (USBSerial_available()) {
    uint8_t c = USBSerial_read();
    // ignore CR/LF
    if (c == '0') {
      setHost(0);
      USBSerial_write(0xFF);
    } else if (c == '1') {
      setHost(1);
      USBSerial_write(0xFF);
    } else if (c == '?'){
      // Return which side we currently on
      USBSerial_println(currentHost==0?"0":"1");
    } else if (c == 'u'){
      print_device_uuid();
    }
  }
}
