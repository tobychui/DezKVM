package dezkvm

/*
	deviceType.go

	Since there might be multiple serial devices on the same KVM host
	some of them are used for other stuffs other than KVM AUX controls
	(e.g. OLED display rendering, IO panels or buttons)
	All devices accept serial baudrate at 115200 and accept the serial
	command of 'u' (no new line) which will returns its UUID

	The first two characters of the UUID can tell us about what device
	this serial device is. The first char is device catergory, and the second
	char is device type. For example, 11 is KVM port - Form factor 1 and 21
	is Display - standard i2c OLED HMI module
*/

import (
	"fmt"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/kvmaux/serial"
)

type DeviceCatergory string

const (
	DeviceCatergoryKVMPort DeviceCatergory = "1"
	DeviceCatergoryDisplay DeviceCatergory = "2"
	// To be continued with more device catergories
)

type DeviceType string

const (
	//KVM Port units
	DeviceTypeKVMPortFormFactor1 DeviceType = "1"

	//Displays
	DeviceTypeDisplayStandardOLED DeviceType = "1"

	//To be continued with more device types
)

// sniffDeviceType opens the serial device at devicePath, sends the 'u' command
// to retrieve its UUID, and extracts the device category and type from the
// first two characters of the UUID string.
// It also returns the full UUID string so callers can reuse it without reopening.
// Uses direct serial I/O (no bufio.Reader) to avoid Close() hanging on Linux CDC ACM devices.
func sniffDeviceType(devicePath string) (DeviceCatergory, DeviceType, string, error) {
	port, err := serial.OpenPort(&serial.Config{
		Name:        devicePath,
		Baud:        115200,
		ReadTimeout: time.Second * 2,
	})
	if err != nil {
		return "", "", "", fmt.Errorf("failed to open serial device %s: %w", devicePath, err)
	}
	defer port.Close()

	// Send 'u' command
	_, err = port.Write([]byte{'u'})
	if err != nil {
		return "", "", "", fmt.Errorf("failed to send command to %s: %w", devicePath, err)
	}

	// Read response: <Length> 0x62 <UUID String>
	header := make([]byte, 2)
	_, err = port.ReadFull(header)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read response header from %s: %w", devicePath, err)
	}

	length := int(header[0])
	if header[1] != 0x62 {
		return "", "", "", fmt.Errorf("invalid command identifier from %s: expected 0x62, got 0x%02x", devicePath, header[1])
	}

	if length < 2 {
		return "", "", "", fmt.Errorf("invalid response length from %s: %d", devicePath, length)
	}

	uuidBuf := make([]byte, length-1)
	_, err = port.ReadFull(uuidBuf)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read UUID from %s: %w", devicePath, err)
	}

	uuid := string(uuidBuf)
	if len(uuid) < 2 {
		return "", "", "", fmt.Errorf("UUID too short from %s: %q", devicePath, uuid)
	}

	category := DeviceCatergory(string(uuid[0]))
	deviceType := DeviceType(string(uuid[1]))
	return category, deviceType, uuid, nil
}
