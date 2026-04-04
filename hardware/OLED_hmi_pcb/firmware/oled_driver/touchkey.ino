/*
   touchkey.ino

   This script handle touch events on the PCB
   touch pads
*/
#include <Serial.h>
#include <TouchKey.h>

// Debug flag for touchkey events, uncomment to enable debug printout
//#define ENABLE_TOUCHKEY_DEBUG 1

// Single byte to represent touchkey state,
// bit 3: TouchKeyA, bit 4: TouchkeyB (matches TIN3/TIN4 bit positions)
static uint8_t touchkeyStates = 0x0;

// Timestamps (ms) of when each key was pressed; index 0 = A, index 1 = B
static uint32_t touchkeyPressTime[2] = {0, 0};

// Bitmask tracking which keys have already fired a long-press event this hold
static uint8_t touchkeyLongPressed = 0x0;

void touch_key_init() {
  /*
     The touch key on CH55xduino use a single byte to represent their address
     TIN0(P1.0), TIN1(P1.1), TIN2(P1.4), TIN3(P1.5), TIN4(P1.6), TIN5(P1.7)
     translate to bit position at
     (1 << 0) | (1 << 1) | (1 << 2) | (1 << 3) | (1 << 4) | (1 << 5)

     Since in DezKVM OLED HMI driver board use case, we are using pin 15 and 16 as
     Touchpad A and touchpad B, we only need to enable 3 anad 4th bit
  */
  TouchKey_begin( (1 << 3) | (1 << 4));
}

void process_touch_key_events() {
  TouchKey_Process();
  uint8_t touchResult = TouchKey_Get();
  // Compare change in touchkey results
  if (touchResult) {
    digitalWrite(LED_PIN, HIGH);
  } else {
    digitalWrite(LED_PIN, LOW);
  }

  // --- Touch Key A (TIN3, bit 3) ---
  bool aCurr = !!(touchResult   & (1 << 3));
  bool aPrev = !!(touchkeyStates & (1 << 3));
  if (aCurr && !aPrev) {
    // Key A just pressed
#ifdef ENABLE_TOUCHKEY_DEBUG
    USBSerial_println("A keydown");
#endif
    USBSerial_write(CMD_EVENT_TOUCH_A_DOWN);
    USBSerial_flush();
    touchkeyPressTime[0]  = millis();
    touchkeyLongPressed  &= ~(1 << 3);
  } else if (!aCurr && aPrev) {
    // Key A just released
#ifdef ENABLE_TOUCHKEY_DEBUG
    USBSerial_println("A keyup");
#endif
    USBSerial_write(CMD_EVENT_TOUCH_A_UP);
    USBSerial_flush();
    touchkeyLongPressed  &= ~(1 << 3);
  } else if (aCurr && !(touchkeyLongPressed & (1 << 3))) {
    // Key A still held — check long-press threshold
    if (millis() - touchkeyPressTime[0] >= (uint32_t)TOUCHKEY_LONGPRESS_SECOND_THRESHOLD * 1000) {
#ifdef ENABLE_TOUCHKEY_DEBUG
      USBSerial_println("A longpress");
#endif
      USBSerial_write(CMD_EVENT_TOUCH_A_LONG_PRESS);
      USBSerial_flush();
      touchkeyLongPressed |= (1 << 3);
    }
  }

  // --- Touch Key B (TIN4, bit 4) ---
  bool bCurr = !!(touchResult   & (1 << 4));
  bool bPrev = !!(touchkeyStates & (1 << 4));
  if (bCurr && !bPrev) {
    // Key B just pressed
#ifdef ENABLE_TOUCHKEY_DEBUG
    USBSerial_println("B keydown");
#endif
    USBSerial_write(CMD_EVENT_TOUCH_B_DOWN);
    USBSerial_flush();
    touchkeyPressTime[1]  = millis();
    touchkeyLongPressed  &= ~(1 << 4);
  } else if (!bCurr && bPrev) {
    // Key B just released
#ifdef ENABLE_TOUCHKEY_DEBUG
    USBSerial_println("B keyup");

#endif
    USBSerial_write(CMD_EVENT_TOUCH_B_UP);
    USBSerial_flush();
    touchkeyLongPressed  &= ~(1 << 4);
  } else if (bCurr && !(touchkeyLongPressed & (1 << 4))) {
    // Key B still held — check long-press threshold
    if (millis() - touchkeyPressTime[1] >= (uint32_t)TOUCHKEY_LONGPRESS_SECOND_THRESHOLD * 1000) {
#ifdef ENABLE_TOUCHKEY_DEBUG
      USBSerial_println("B longpress");
#endif
      USBSerial_write(CMD_EVENT_TOUCH_B_LONG_PRESS);
      USBSerial_flush();
      touchkeyLongPressed |= (1 << 4);
    }
  }

  // Update touchkey state
  touchkeyStates = touchResult;
}
