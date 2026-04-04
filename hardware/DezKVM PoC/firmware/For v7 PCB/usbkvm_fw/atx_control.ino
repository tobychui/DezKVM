/*
  atx_control.ino
  author: tobychui

  This file include functions that handles
  ATX power control and switching.

  Note: Not all versions of RemdesKVM have
  ATX hardware populated
*/


void update_atx_led_status() {
  led_tmp = digitalRead(ATX_PWR_LED);
  atx_status[0] = led_tmp;
  led_tmp = digitalRead(ATX_HDD_LED);
  atx_status[1] = led_tmp;
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
