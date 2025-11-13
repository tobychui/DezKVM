/*
  usb_mass_storage.ino
  author: tobychui

  This file include functions that handles
  USB mass storage switching
*/


void switch_usbms_to_kvm() {
  if (usb_ms_side == USB_MS_SIDE_KVM_HOST) {
    //Already on the KVM side
    return;
  }

#if ENABLE_DEBUG == 1
  USBSerial_println("[DEBUG] Switching USB Mass Storage node to KVM host side");
#endif
  //Disconnect the power to USB
#ifndef COMPATIBLE_VERSION_FIVE_PCB
  digitalWrite(USB_MS_PWR, LOW);
  delay(USB_PWR_SW_PWR_DELAY);
#endif

  //Switch over the device
  digitalWrite(USB_MS_PWR, HIGH);
  delay(USB_PWR_SW_DATA_DELAY);
  digitalWrite(USB_MS_SW, HIGH);
  usb_ms_side = USB_MS_SIDE_KVM_HOST;
}

void switch_usbms_to_remote() {
  if (usb_ms_side == USB_MS_SIDE_REMOTE_PC) {
    //Already on Remote Side
    return;
  }

#if ENABLE_DEBUG == 1
  USBSerial_println("[DEBUG] Switching USB Mass Storage node to remote computer side");
#endif
  //Disconnect the power to USB
#ifndef COMPATIBLE_VERSION_FIVE_PCB
  digitalWrite(USB_MS_PWR, LOW);
  delay(USB_PWR_SW_PWR_DELAY);
#endif

  //Switch over the device
  digitalWrite(USB_MS_PWR, HIGH);
  delay(USB_PWR_SW_DATA_DELAY);
  digitalWrite(USB_MS_SW, LOW);
  usb_ms_side = USB_MS_SIDE_REMOTE_PC;
}
