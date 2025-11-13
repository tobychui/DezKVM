/*
  USB Switch

  This script handle commands that switch the USB mass storage device
  to the host or slave side
*/
#include "usbkvm_fw.h"

//Entry point for switching USB bus signals, value only support 0x00 (A) and 0x01 (B)
uint8_t usb_switch_emulation(uint8_t subtype, uint8_t value) {
  switch (subtype) {
    case SUBTYPE_SWITCH_USBHID:
      if (value == 0x00) {
        digitalWrite(HID_SW_SEL, LOW);
        return resp_ok;
      } else if (value == 0x01) {
        digitalWrite(HID_SW_SEL, HIGH);
        return resp_ok;
      } else if (value == 0x02){
        //Software reset with HID Switch reconnections
        digitalWrite(HID_SW_SEL, HIGH);
        delay(100);
        SoftwareReset();
        //After reset the switch is automatically pull LOW
        return resp_ok; //Should be not reachable
      }
      return resp_invalid_key_value;
    case SUBTYPE_SWITCH_USBMASS:
      if (value == 0x00) {
        digitalWrite(USB_SW_SEL, LOW);
        return resp_ok;
      } else if (value == 0x01) {
        digitalWrite(USB_SW_SEL, HIGH);
        return resp_ok;
      }
      return resp_invalid_key_value;
    default:
      return resp_invalid_opr_type;
  }
}
