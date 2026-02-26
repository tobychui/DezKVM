# DezKVM Hardware

## Introduction

The DezKVM is compose of the following hardware connected together

- 1 x Host Computer (e.g. Orange Pi, Raspberry Pi, NanoPi)
- 1 x Power Management Board
- n x DezKVM Port Module
- (Optional) Display Module

### Picking the correct host computer

This is the most tricky part. Since the host computer IO limits the number of Port modules you can connects (i.e. the number of port your KVM could have), it is best to first decide how many ports you need and pick the suitable host computer.

The maximum bandwidth of each of the Port module is around 80 - 90Mbps at 1080p 25fps MJPEG mode with 48kHz PCM stereo channels. So the bottleneck would always be either the USB tree or the ethernet speed. However, depending on how you use it (for example you prefer switching between one of remotes, or connect to multiple sessions at once), configuration might varies according to your purpose. Here are some examples and recommendations.

| Single Board Computer | Ethernet Bandwidth | USB Ports | Recommended No. of Port Modules                              | Max Concurrent Sessions                                      |
| --------------------- | ------------------ | --------- | ------------------------------------------------------------ | ------------------------------------------------------------ |
| Orange Pi Zero LTS    | 100Mbps            | 3         | 1 - 2                                                        | 1                                                            |
| Orange Pi Zero 2      | 1Gbps              | 3         | 2 - 4 (2 host port each with 1 hub, each hub have 2 port module) | 4 (All ports)                                                |
| NanoPi Zero2          | 1Gbps              | 1         | 1 - 4 (1 host port with 1 hub,  4 port modules as downstream) | 4 (90Mbps x 4 will max out the only USB 2.0 port real world speed) |
| Raspberry Pi 3+       | 300Mbps            | 4         | 2 (USB port and ethernet chip share the same bus)            | 2 (All ports)                                                |
| Raspberry Pi 4        | 1Gbps              | 4         | 4                                                            | 4                                                            |

Here are just examples. **You can always connects more Port modules, just make sure you have enough bandwidth when connecting to multiple sessions at the same time.** 





![usb-kvm](../img/README/usb-kvm.jpg)

## DezKVM Port Module – Technical Specifications

The key feature of the DezKVM Port module is the modularity and ease of building. It use all hardware components and no emulated USB composite device, allowing highest compatibility with existing computer / OSes. 

- Fully USB protocol-based architecture
- Modular and scalable
- No FPGA required
- No custom Linux drivers
- Designed for multi-port IP-KVM chaining



### General

| Item                           | Specification                    |
| ------------------------------ | -------------------------------- |
| Module Type                    | USB-based KVM Extension Module   |
| Architecture                   | USB Hub + Downstream USB Devices |
| Host Interface                 | USB 2.0 High-Speed (480Mbps)     |
| Operating System Support       | Linux (Upstream Drivers Only)    |
| Custom Kernel Drivers Required | No                               |
| Expansion Method               | USB Hub Chaining                 |

---

### USB Hub Core

| Item             | Specification      |
| ---------------- | ------------------ |
| USB Hub IC       | SL2.1A             |
| Upstream Ports   | 1                  |
| Downstream Ports | 4                  |
| USB Version      | USB 2.0            |
| Clock            | 12 MHz Crystal     |
| Power            | 5V USB Bus Powered |

---

### Video Capture Subsystem

| Item              | Specification                        |
| ----------------- | ------------------------------------ |
| Capture Interface | HDMI Input                           |
| Capture Chip      | MS2109 (USB UVC Device)              |
| Linux Support     | V4L2 (UVC Standard Driver)           |
| Max Resolution    | Up to 1080p 25fps (Module Dependent) |
| Audio Support     | HDMI Embedded Audio                  |
| Audio Driver      | ALSA (USB Audio Class)               |

---

### HID Emulation

| Item               | Specification                                          |
| ------------------ | ------------------------------------------------------ |
| HID Controller     | CH9329                                                 |
| Function           | USB Keyboard + Mouse Emulation                         |
| Control Interface  | UART                                                   |
| Status LED         | TX/RX Activity Indicators                              |
| Driver Requirement | Native USB HID <br />(No Driver Needed on remote side) |

---

### Control Channel

| Item            | Specification                    |
| --------------- | -------------------------------- |
| USB-UART Bridge | CH340C                           |
| Purpose         | SBC to HID/Control Communication |
| Linux Driver    | ch341 (Upstream)                 |
| Baud Rate       | 115200bps                        |

---

### USB Mass Storage Switching

| Item          | Specification                                       |
| ------------- | --------------------------------------------------- |
| USB Switch IC | CH440G                                              |
| Power Control | AO3400A / AO3401A  MOSFET<br />(VCC side switching) |
| Function      | Dynamically switch USB mass storage device          |
| Use Case      | ISO mounting / recovery tools                       |

---

### ATX Power Control Module

| Item           | Specification                              |
| -------------- | ------------------------------------------ |
| MCU            | CH552G                                     |
| ATX Signals    | PWR_SW, RST_SW                             |
| LED Monitoring | PWR_LED, HDD_LED                           |
| Isolation      | EL817 Optocouplers x 4                     |
| Compatibility  | PiKVM ATX Pinout Compatible (To be tested) |

---

### Electrical

| Item            | Specification                |
| --------------- | ---------------------------- |
| Input Voltage   | 5V USB Powered               |
| Power Filtering | Bulk + 100nF Decoupling      |
| Status LEDs     | Power, Activity, Programming |
| ESD Protection  | USB Port Level               |

