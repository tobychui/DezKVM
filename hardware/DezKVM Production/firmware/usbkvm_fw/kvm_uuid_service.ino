/*
  kvm_uuid_service.ino
  author: tobychui

  This file contains code that uniquely
  identify a USB-KVM downstream device for
  multi-device setups.

  Command Structure
  <Length> 0x62 <UUID String>
*/

#define DEVICE_UUID "1108b1c9-8ffb-4fb2-a3a6-7c08feae29f1" //UUIDv4, Change this UUID for each device

void print_device_uuid() {
  USBSerial_write(0x25); //Length byte
  USBSerial_write(0x62); //Command identifier
  USBSerial_print(DEVICE_UUID); //UUID string
}
