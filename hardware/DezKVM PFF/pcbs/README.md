## Change Log

Change log for USB-KVM hardware design

| Version | Changes                                                      | Issues                                                       |
| ------- | ------------------------------------------------------------ | ------------------------------------------------------------ |
| v1 - v2 | Concept prototypes                                           | -                                                            |
| v3      | Reduce complexity                                            | HDMI child board not working                                 |
| v4      | Restructure board form factor                                | CH552G do not handle UART signal fast enough for cursor movements |
| v5      | Added USB mass storage port and dedicated USB UART chip for UART to HID chip communication | USB mass storage power switch causes overcurrent on USB VBUS line and resets the whole bus |
| v6      | Fixed PMOS design issue                                      | N/A                                                          |
| v7      | Added power control to HDMI capture card                     | N/A                                                          |

