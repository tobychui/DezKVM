/*
  RemdesKVM USB-KVM Firmware
  Author: tobychui

  This is the USB KVM part of RemdesKVM. 
  It can be used seperately as a dedicated USB-KVM 
  module as well as connected to a Linux SBC running
  remdeskvmd via a USB 2.0 connection.

  Upload Settings
  CH552G  
  24Mhz (Internal)
  USB CODE /w 148B USB RAM
*/

#ifndef USER_USB_RAM
#error "This firmware needs to be compiled with a USER USB setting"
#endif

//#define ENABLE_DEBUG_PRINT

#include "usbkvm_fw.h"
#include "src/remdesHid/USBHIDKeyboardMouse.h"


/*
  instr_count store the current read index
  index of current instruction, all instructions have 3 bytes
  byte orders as follows
  byte 0 = opr_type
  byte 1 = opr_subtype
  byte 2 = data
*/
uint8_t instr_count = 0;

/*
  opr_type defines the type of the incoming data
  for the next byte. See usbkvm_fw.h for details.
*/
uint8_t opr_type = 0x00;

/*
  opr_subtype defines the sub-type of the operation
  Based on opr_type, there will be different catergory
  of operations. However, 0x00 is always reserved
*/

uint8_t opr_subtype = 0x00;
uint8_t opr_payload = 0x00;

/*
  cursor_direction_data handles special subtype data
  that requires to move the cursor position
*/
uint8_t cursor_direction_data[2] = { 0x00, 0x00 };
uint8_t serial_data = 0x00;

/* Function Prototypes */
uint8_t keyboard_emulation(uint8_t, uint8_t);
uint8_t mouse_emulation(uint8_t, uint8_t);
uint8_t mouse_move(uint8_t, uint8_t, uint8_t, uint8_t);
uint8_t mouse_wheel(uint8_t, uint8_t);
uint8_t usb_switch_emulation(uint8_t, uint8_t);

/* KVM Operation Execution Catergory*/
uint8_t kvm_execute_opr() {
  switch (opr_type) {
    case OPR_TYPE_RESERVED:
      //Ping test
      return resp_ok;
    case OPR_TYPE_KEYBOARD_WRITE:
      //keyboard operations
      return keyboard_emulation(opr_subtype, opr_payload);
    case OPR_TYPE_MOUSE_WRITE:
      //mouse operations
      return mouse_emulation(opr_subtype, opr_payload);
    case OPR_TYPE_MOUSE_SCROLL:
      //mouse scroll
      //for larger scroll tilt value, use the multipler
      return mouse_wheel(opr_subtype, opr_payload);
    case OPR_TYPE_SWITCH_SET:
      //set USB signal bus switch state
      return usb_switch_emulation(opr_subtype, opr_payload);
    default:
      return resp_unknown_opr;
  }
}

void setup() {
  // Start USB HID emulation
  USBInit();

  // Start CH340 UART COM
  Serial0_begin(115200);
  // Set both USB switch to LOW
  pinMode(USB_SW_SEL, OUTPUT);
  pinMode(HID_SW_SEL, OUTPUT);
  digitalWrite(HID_SW_SEL, LOW);
  digitalWrite(USB_SW_SEL, LOW);
  delay(100);
}

void loop() {
  if (Serial0_available()) {
    serial_data = 0x00;
    serial_data = Serial0_read();

//Debug print the Serial input message
#ifdef ENABLE_DEBUG_PRINT
    Serial0_write(resp_start_of_info_msg);
    Serial0_write(serial_data);
    Serial0_write(resp_end_of_msg);
#endif

    if (serial_data == OPR_TYPE_DATA_RESET) {
      //Reset opr data
      opr_type = OPR_TYPE_RESERVED;
      opr_subtype = SUBTYPE_RESERVED;
      opr_payload = 0x00;
      instr_count = 0;
      Serial0_write(resp_ok);
    } else if (instr_count == 0) {
      //Set opr type
      opr_type = serial_data;
      instr_count++;
      Serial0_write(resp_ok);
    } else if (instr_count == 1) {
      //Set opr subtype
      opr_subtype = serial_data;
      instr_count++;
      Serial0_write(resp_ok);
    } else if (opr_type == OPR_TYPE_MOUSE_MOVE) {
      //Special case where mouse move requires 4 opcodes
      if (instr_count == 2) {
        opr_payload = serial_data;  //y-steps, reusing payload for lower memory consumption
        instr_count++;
      } else if (instr_count == 3) {
        cursor_direction_data[0] = serial_data;  //x-direction
        instr_count++;
      } else if (instr_count == 4) {
        cursor_direction_data[1] = serial_data;  //y-direction
        opr_type = OPR_TYPE_RESERVED;
        instr_count = 0;
        mouse_move(opr_subtype, opr_payload, cursor_direction_data[0], cursor_direction_data[1]);
        cursor_direction_data[0] = 0x00;
        cursor_direction_data[1] = 0x00;
        opr_subtype = 0x00;
      }
      Serial0_write(resp_ok);
    } else {
      opr_payload = serial_data;
      //Execute the kvm operation
      uint8_t err = kvm_execute_opr();
      Serial0_write(err);

      //Reset the instruction counter and ready for the next instruction
      instr_count = 0;
    }
  }
  delay(1);
}