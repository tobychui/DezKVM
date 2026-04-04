# Firmware

## Introduction
The firmware folder contains all the firmware for different PCB versions. Pick one that matches
the PCB version you are using. 

### Onboard ICs and their purposes
There are 5 important ICs on the PCB

1. SL2.1A - A USB 2.0 hub that also act as a device tree mapper that let the host computer knows the following chips are belongs to single USB-KVM device
2. CH552G - The onboard MCU that provide a ttyACM0 device interface for toggling USB mass storage device (technically the physical port) between host and remote, as well as ATX power control and serving a UUID to identify which USB tree is a unique USB-KVM device
3. CH340 - The USB to UART converter for CH9329
4. CH9329 - The UART to USB HID bytecode converter (i.e. this chip emulate your keyboard and mouse)
5. CH440G - USB signal switch, for switching the USB port between your host computer (running dezuKVM control software) and your remote computer

For the slot on the PCB and the USB2.0 port, it is for HDMI capture card (MS2109). You can get them online for a really low price and output MJPEG stream using internal hardware encoder. See more over the dezukvmd README file.

## Flashing the CH552G onboard MCU
The usbkvm is developed with Arduino IDE. You will need the [ch55xduino broad API](https://github.com/DeqingSun/ch55xduino) installed in order to compile and flash the firmware to the USB-KVM hardware.

For first time flashing the firmware, **follow the steps below carefully**.

1. Prepare the firmware code in your Arduino
2. Press compile button and wait for the compiled binary get cached (to speed up the flash process)
3. Hold down the FLASH button on the USB-KVM PCB. **Do not release it until you are told to do so.**
4. Insert the USB cable into the USB-KVM host side port and connect it to your computer. You should see multiple device pops up in your device manager. If all parts are soldered correctly, you will see a USB hub with 3 devices under it, the USB capture card (which provide a video and audio device file), a USB to UART adapter and an unknown device. That Unknown device is our CH552g (without firmware)
5. Press the upload button in your Arduino IDE with no serial port selected
6. **Just after the code compiled, immediately release the FLASH button** (Notes: if you do this too slow, you will get a timeout in your Arduino IDE console. If you release too early, the MCU will reset itself and back to runtime mode. Try a few time if you keep having issues with this step)
7.  After the firmware upload, you should see a Serial device pop up in your device manager and the unknown device is gone. 


For future firmware re-flash or update, simply upload using the serial port like an ordinary Arduino dev board. 

## CH9329 Baudrate Change
The CH9329 defaults to 9600 baudrate which is not enough for DezuKVM to operate the keyboard and mouse emulation service smoothly. 
After the first time flashing (which now the device on board CH552G MCU contains an UUID and can be scanned by dezukvmd), following the steps below to change the CH9329 baudrate to 115200.

1. Power on the device by plugging the host side USB power cable into your computer
2. Connect the keyboard virtual machine (KVM) USB type C port (the one closer to the HDMI port for v6+ PCB) into a 5V power brick
3. Get and build dezukvmd and run `src/dezukvmd/configure_chip.sh` (If your CH340 that connects to the CH9329 was not on the expected default device path of `/dev/USBtty0`, you might want to modify `./config/usbkvm.json` relative to the dezukvmd binary executable to set a proper device path for your setup )
4. After the configure chip completed without error, unplug the KVM USB port (the one you plugged in on step 2)
5. Connect your remote computer (the computer that you want to control) to the KVM USB port and start dezukvmd or the usbkvm-app. Now you should be able to control the remote cursor and keyboard using your browser.

## Command List

### Protocol Notes
- **v1/v2**: Uses 3-byte protocol (operation type, subtype, payload)
- **v3/v4**: Uses length-prefixed commands (length + command + data)
- **v5+**: Uses single ASCII character commands

### Keyboard Commands
- `0x01` / `0x01`: Keyboard key press [v3+] (deprecated in v5)
- `0x02` / `0x02`: Keyboard key release [v3+] (deprecated in v5)
- `0x03` / `0x03`: Keyboard modifier key press [v3+] (deprecated in v5)
- `0x04` / `0x04`: Keyboard modifier key release [v3+] (deprecated in v5)

### Mouse Commands
- `0x05`: Mouse button press [v3+] (deprecated in v5)
- `0x06`: Mouse button release [v3+] (deprecated in v5)
- `0x07`: Mouse scroll up [v3+] (deprecated in v5)
- `0x08`: Mouse scroll down [v3+] (deprecated in v5)
- `0x09`: Mouse move absolute [v3+] (deprecated in v5)
- `0x0A`: Mouse move relative [v3+] (deprecated in v5)

### System Commands
- `0x0B`: Get keyboard LED status [v3+] (deprecated in v5)
- `0x00`: ATX power button press [v4+] (deprecated in v5)
- `0x01`: ATX power button release [v4+] (deprecated in v5)
- `0x02`: ATX reset button press [v4+] (deprecated in v5)
- `0x03`: ATX reset button release [v4+] (deprecated in v5)
- `0x04`: Get ATX LED status [v4+] (deprecated in v5)

### Simplified Commands (v5+)
- `1`: ATX power button press [v5] (deprecated in v6)
- `2`: ATX power button release [v5] (deprecated in v6)
- `3`: ATX reset button press [v5] (deprecated in v6)
- `4`: ATX reset button release [v5] (deprecated in v6)
- `5`: Switch USB mass storage to KVM [v5] (deprecated in v6)
- `6`: Switch USB mass storage to remote [v5] (deprecated in v6)

### Current Commands (v6+)
- `p`: ATX power button press [v6+]
- `s`: ATX power button release [v6+]
- `r`: ATX reset button press [v6+]
- `d`: ATX reset button release [v6+]
- `m`: Switch USB mass storage to KVM [v6+]
- `n`: Switch USB mass storage to remote [v6+]
- `u`: Get device UUID [v6+]
- `k`: Force reset HDMI capture card [v7+]
- `l`: Power off HDMI capture card [v7+]
- `j`: Power on HDMI capture card [v7+]
- `z`: Regenerate device UUID (debug only) [v6+]


## Changelog

### v7 (Latest)
- Added HDMI capture card power control and reset functionality
- New commands: 'k' (force reset HDMI), 'l' (power off HDMI), 'j' (power on HDMI)
- Added USB_HDMI_PWR pin for HDMI capture card management

### v6
- Introduced device UUID service for unique device identification
- Changed command set to single characters: 'p' (power press), 's' (power release), 'r' (reset press), 'd' (reset release), 'm' (USB MS to KVM), 'n' (USB MS to remote), 'u' (get UUID)
- Added optional ATX control with build flag ENABLE_ATX_CTRL
- Improved initialization with UUID setup

### v5
- Added USB mass storage switching with power control
- Implemented LED programming indicator for command feedback
- Added debug mode with build flag ENABLE_DEBUG
- Simplified command interface with numeric commands (1-6)
- Enhanced power switching delays for reliable USB MS operation

### v4
- Added ATX power and reset button control
- Implemented LED status reading for power and HDD indicators
- Increased UART baud rate to 19200 for better performance
- Added custom header bytes for RemdesKVM communication
- Introduced USB mass storage switch pin (currently unused)

### v3
- Switched to CH9329 chip for enhanced keyboard and mouse emulation
- Added keyboard LED status retrieval functionality
- Improved command processing with length-prefixed messages
- Added manufacturer and product string configuration
- Enhanced mouse support with absolute positioning

### v1/v2
- Initial firmware with basic USB HID keyboard and mouse emulation
- Implemented USB switch functionality for signal routing
- Basic serial communication protocol for KVM operations
- Support for keyboard writing, mouse movement, scrolling, and switching
