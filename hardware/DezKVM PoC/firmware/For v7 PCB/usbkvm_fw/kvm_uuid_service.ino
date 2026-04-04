/*
  kvm_uuid_service.ino
  author: tobychui

  This file contains code that uniquely
  identify a USB-KVM downstream device for
  multi-device setups.

  The UUID can change during power cycle,
  as soon as it is unique amoung other 
  IP-KVM it is good to go.
*/

#define DEVICE_UUID "df75279d-c691-4be5-9001-c18e37593ffc"

void print_device_uuid() {
  USBSerial_println(DEVICE_UUID);
}
