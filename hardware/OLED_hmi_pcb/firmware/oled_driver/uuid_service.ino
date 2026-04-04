/*
  uuid_service.ino
  author: tobychui

  This file contains code that uniquely
  identify a USB-KVM downstream device for
  multi-device setups.

  Command Structure
  <Length> 0x62 <UUID String>
*/

// UUID encoding: 1st char = device_type (2 = display, 0x02), 2nd char = display_type (1 = 0.96 inch i2c OLED module, 0x01)
#define DEVICE_UUID "216291a8-13fa-4c27-8659-af008e3c5e69" //UUIDv4, Change this UUID for each device (keep first 2 chars as '21')

void print_device_uuid() {
  USBSerial_write(0x25); //Length byte
  USBSerial_write(0x62); //Command identifier
  USBSerial_print(DEVICE_UUID); //UUID string
}
