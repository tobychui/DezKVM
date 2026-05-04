/*
  uuid_service.ino
  author: tobychui

  This file contains code that uniquely
  identify a USB-KVM downstream device for
  multi-device setups.

  Command Structure
  <Length> 0x62 <UUID String>
*/

// UUID encoding: 1st char = device_type (3 = KVM switch, 0x02), 2nd char = switch type (Basic USB2.0 6 ports KVM Switch, 0x01)
#define DEVICE_UUID "316eb07f-1577-4996-9f4f-90ff5e11c693" //UUIDv4, Change this UUID for each device (keep first 2 chars as '31')

void print_device_uuid() {
  USBSerial_write(0x25); //Length byte
  USBSerial_write(0x62); //Command identifier
  USBSerial_print(DEVICE_UUID); //UUID string
}
