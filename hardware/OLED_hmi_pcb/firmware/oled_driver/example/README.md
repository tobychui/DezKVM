# OLED System Monitor

Cross-platform system monitor that displays hostname, CPU%, RAM%, network IPs, and internet status on a 128x64 OLED via PicoHMI serial protocol.

## Features

- **Cross-platform**: Supports both Linux and Windows
- **Real-time monitoring**: CPU, RAM, network status
- **Smart IP display**: Prioritizes 192.168.x.x addresses
- **Internet connectivity check**: Shows online/offline status
- **Visual feedback**: Alternating frame indicator

## Building

### Windows
```cmd
# Build for Windows (default: COM3)
build_windows.bat

# Or using PowerShell
.\build_windows.ps1

# Or manually
go build -o oled_sysinfo.exe
```

### Linux
```bash
# Build for Linux (default: /dev/ttyACM0)
./build_arm64.sh

# Or for x86_64
go build -o oled_sysinfo
```

### Cross-compilation
```bash
# From Windows, build for Linux ARM64
$env:GOOS='linux'; $env:GOARCH='arm64'; go build

# From Linux, build for Windows
GOOS=windows GOARCH=amd64 go build -o oled_sysinfo.exe
```

## Usage

### Windows
```cmd
# Use default COM3 port
.\oled_sysinfo.exe

# Specify custom port
.\oled_sysinfo.exe -port COM5
```

### Linux
```bash
# Use default /dev/ttyACM0 port
./oled_sysinfo

# Specify custom port
./oled_sysinfo -port /dev/ttyUSB0
```

## Architecture

The project uses Go build tags for platform-specific implementations:

- `main.go` - Main application logic and cross-platform code
- `sysinfo_linux.go` - Linux-specific CPU/RAM monitoring (uses /proc filesystem)
- `sysinfo_windows.go` - Windows-specific CPU/RAM monitoring (uses Win32 APIs)
- `defaults_linux.go` - Linux default serial port (/dev/ttyACM0)
- `defaults_windows.go` - Windows default serial port (COM3)

### Platform-specific implementations

**Linux:**
- CPU: Reads `/proc/stat` twice with 200ms interval
- RAM: Reads `/proc/meminfo` for total/available memory

**Windows:**
- CPU: Uses `GetSystemTimes()` Win32 API with 200ms interval
- RAM: Uses `GlobalMemoryStatusEx()` Win32 API

**Cross-platform:**
- Hostname: `os.Hostname()` (works everywhere)
- Network IPs: Go's `net.Interfaces()` package
- Internet check: HTTP HEAD request to clients3.google.com

## Display Format

```
NanoPi-Zero2      <- Hostname (truncated to 16 chars)
-----------------
CPU: 12.5%        <- CPU usage percentage
RAM: 28.7%        <- RAM usage percentage
Network:
192.168.0.20      <- Primary IP (prefers 192.168.x.x)
192.168.196.170   <- Secondary IP
Connected-        <- Internet status (- or | alternates each refresh)
```

## Install as Systemd service
Edit `/etc/systemd/system/oled.service` and add the following contents

```
[Unit]
Description=Simple OLED status monitor

[Service]
Type=simple

# Optional, run as root

User=root
Group=root

# Change the paths according to your needs!
WorkingDirectory=/home/pi/oled
ExecStart=/home/pi/oled/oled_sysinfo -port="/dev/ttyACM0"

[Install]
WantedBy=multi-user.target
```

## Dependencies

- `go.bug.st/serial` - Cross-platform serial port communication
- Windows: Uses syscall to Win32 APIs (kernel32.dll)
- Linux: Direct /proc filesystem access

## Firmware

Make sure to flash the updated firmware with the ACK fix to avoid timeout issues on slow I2C operations.
