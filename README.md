

![title image](img/title.png)

# DezKVM

Budget & platform independent IP-KVM solution built for systems running Linux. By using all USB protocol based hardware and involving the upstream v4l2 and alsa USB device drivers, DezKVM is able to run on most Linux embedded systems with no custom drivers required.

> [!WARNING]
> This project is in its very early stage and not production ready. Use with your own risk. 


## Build

### Dezkvmd (DezKVM daemon)

The Dezkvmd is a golang written piece of code that runs on a x86 or ARM computer with Debian based Linux installed. Require v4l2 and alsa with kernel 6.1 or above.  

To build the Remdeskd, you will need go compiler. The go package manager will take care of the dependencies during your first build. 

```bash
cd dezkvmd/
go mod tidy
go build

sudo ./dezkvmd
# or use ./dezkvmd -h to show all start options
```
### Setup Systemd Service

Create a systemd service file at `/etc/systemd/system/dezkvmd.service`:

```ini
[Unit]
Description=dezKVM IP-KVM Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/path/to/DezKVM/src/dezkvmd
ExecStart=/path/to/DezKVM/src/dezkvmd/dezkvmd -mode=ipkvm
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Replace `/path/to/dezKVM` with the actual path to your dezKVM installation directory.

Then enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable dezkvmd
sudo systemctl start dezkvmd
```

Check the service status:

```bash
sudo systemctl status dezkvmd
```

#### Install Dependencies

Some packages are required to run DezKVM (Require Linux kernel 6.1 or above, Debian based distro recommended). If your `go build` command above failed due to missing v4l2 types or user space properties, you can install the dependencies by running the following command. 

```
sudo apt install build-essential libv4l-dev v4l-utils 
```

If your system fail to capture audio output from remote device, or show invalid audio device properties, install the missing alsa utilities with the following command. 

```
sudo apt install alsa-utils
```

(Or you can just run both before go build, I mean why not? )

### Hardware

![usb-kvm](img/README/usb-kvm.jpg)

See `hardware/pcbs/README.md`

### USB-KVM Firmware Flashing
See `firmware/README.md`

## Usage




### Video Capture Configs

By default, MS2109 HDMI capture card support the following resolutions. If you are connecting to your RemdesKVM via the internet (not recommended), pick a resolution and fps combination that best fit your network **upload** bandwidth. If you are using your RemdesKVM within your internal management network, you can just pick the FHD 25 / 30fps option since local area network are at least 100mbps at the time of writing.

```
// FHD
1920 x 1080 30fps = 50Mbps
1920 x 1080 25fps = 40Mbps
1920 x 1080 20fps = 30Mbps
1920 x 1080 10fps = 15Mbps

// HD
1360 x 768 60fps = 28Mbps
1360 x 768 30fps = 25Mbps
1360 x 768 25fps = 20Mbps
1360 x 768 20fps = 18Mbps
1360 x 768 10fps = 10Mbps
```

### USBKVM-App

The USBKVM-App is a debug tool for the USB-KVM section of the DezKVM build that also serve as a USB-KVM viewer. It is used for checking if the USB-KVM subsystem works before integrating into a larger IP-KVM system. See `tools/usbkvm-app/` for more information.

## License

DezKVM
Copyright (C) 2025 Toby Chui

DezKVM is free software; You can redistribute it and/or modify it under the terms of:
  - the GNU Affero General Public License version 3 as published by the Free Software Foundation.
You don't have to do anything special to accept the license and you don’t have to notify anyone which that you have made that decision.

DezKVM is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY;
without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
See your chosen license for more details.

You should have received a copy of both licenses along with DezKVM.
If not, see <http://www.gnu.org/licenses/>.

**Note: There will be no support if you are using 3rd party parts or systems. If you are creating a new issue, make sure you are using the official implementation here with the recommended hardware and software setups**



