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

#define UUID_SIZE 16              // 16-byte UUID

uint8_t device_uuid[UUID_SIZE];

void init_device_uuid() {
  // Use an unused analog pin as noise source (floating pin gives better entropy)
  const uint8_t noise_pin = 11; // P1.1, but could also be 14, 15, or 32

  pinMode(noise_pin, INPUT);

  for (uint8_t i = 0; i < UUID_SIZE; i++) {
    // Mix analog noise with millis() and loop index
    int noise = analogRead(noise_pin);   // 0–1023
    uint8_t entropy = (uint8_t)(noise ^ (millis() >> (i % 8)) ^ (rand() & 0xFF));
    device_uuid[i] = entropy;
    delay(5); // small delay to let ADC vary
  }
}

void print_device_uuid() {
  for (uint8_t i = 0; i < UUID_SIZE; i++) {
    USBSerial_print(device_uuid[i], HEX);
  }
  USBSerial_println();
}

void renew_device_uuid() {
  init_device_uuid();
}
