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
	"errors"
	"fmt"
	"regexp"
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

const (
	sniffAttempts        = 3                      // Number of UUID requests before giving up
	sniffReadTimeout     = 300 * time.Millisecond // Per read() quiet timeout (VTIME)
	sniffReplyDeadline   = 2 * time.Second        // Max time to wait for a full UUID reply
	sniffDrainDeadline   = time.Second            // Max time spent discarding stale bytes
	sniffUUIDReplyLength = 0x25                   // <Length> byte of a UUID reply: 0x62 + 36 UUID chars
)

// parserResetSequence resets the command parser of display (HMI) MCUs that might
// be stuck mid-command (e.g. a previous session was killed while sending a frame).
// KVM port MCUs treat these bytes as unknown commands and their replies are drained.
var parserResetSequence = []byte{0xCF, 0xCF, 0xCF}

// uuidPattern matches the UUID string returned by the device
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// sniffDeviceType opens the serial device at devicePath, sends the 'u' command
// to retrieve its UUID, and extracts the device category and type from the
// first two characters of the UUID string.
// It also returns the full UUID string so callers can reuse it without reopening.
// Before asking, it discards stale bytes and resets HMI parsers, and it resyncs
// on the <Length> 0x62 header instead of assuming the reply starts at the first byte.
// Uses direct serial I/O (no bufio.Reader) to avoid Close() hanging on Linux CDC ACM devices.
func sniffDeviceType(devicePath string) (DeviceCatergory, DeviceType, string, error) {
	port, err := serial.OpenPort(&serial.Config{
		Name:        devicePath,
		Baud:        115200,
		ReadTimeout: sniffReadTimeout,
	})
	if err != nil {
		return "", "", "", fmt.Errorf("failed to open serial device %s: %w", devicePath, err)
	}
	defer port.Close()

	// Reset any stuck HMI parser, then discard whatever was pending or was
	// replied to the reset bytes
	drainSerial(port, sniffDrainDeadline)
	if _, err := port.Write(parserResetSequence); err != nil {
		return "", "", "", fmt.Errorf("failed to send reset sequence to %s: %w", devicePath, err)
	}
	time.Sleep(150 * time.Millisecond)
	drainSerial(port, sniffDrainDeadline)

	var lastErr error
	for attempt := 1; attempt <= sniffAttempts; attempt++ {
		uuid, err := requestUUID(port)
		if err == nil {
			category := DeviceCatergory(string(uuid[0]))
			deviceType := DeviceType(string(uuid[1]))
			return category, deviceType, uuid, nil
		}
		lastErr = err
		drainSerial(port, sniffDrainDeadline)
	}
	return "", "", "", fmt.Errorf("failed to read UUID from %s after %d attempts: %w", devicePath, sniffAttempts, lastErr)
}

// requestUUID sends the 'u' command and scans the incoming bytes for the
// <Length> 0x62 <UUID String> reply
func requestUUID(port *serial.Port) (string, error) {
	if _, err := port.Write([]byte{'u'}); err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	deadline := time.Now().Add(sniffReplyDeadline)
	var prev byte
	havePrev := false
	for {
		b, err := readByteBefore(port, deadline)
		if err != nil {
			return "", fmt.Errorf("no UUID reply header: %w", err)
		}
		// Only accept the exact UUID header. Hex digits are valid length
		// values, so "3b" inside a stale partial UUID would otherwise look
		// like a header. 0x25 ('%') never appears inside a UUID.
		if havePrev && b == 0x62 && prev == sniffUUIDReplyLength {
			uuidBuf := make([]byte, int(prev)-1)
			for i := range uuidBuf {
				uuidBuf[i], err = readByteBefore(port, deadline)
				if err != nil {
					return "", fmt.Errorf("incomplete UUID (%d/%d bytes): %w", i, len(uuidBuf), err)
				}
			}
			uuid := string(uuidBuf)
			if !uuidPattern.MatchString(uuid) {
				return "", fmt.Errorf("invalid UUID received: %q", uuid)
			}
			return uuid, nil
		}
		prev = b
		havePrev = true
	}
}

// readByteBefore reads a single byte, retrying on read timeouts until deadline
func readByteBefore(port *serial.Port, deadline time.Time) (byte, error) {
	buf := make([]byte, 1)
	for {
		n, err := port.Read(buf)
		if n == 1 {
			return buf[0], nil
		}
		if err != nil && !errors.Is(err, serial.ErrTimeout) {
			return 0, err
		}
		if time.Now().After(deadline) {
			return 0, serial.ErrTimeout
		}
	}
}

// drainSerial discards incoming bytes until the line is quiet for one read
// timeout or the deadline passes
func drainSerial(port *serial.Port, maxDuration time.Duration) {
	_ = port.Flush()
	deadline := time.Now().Add(maxDuration)
	buf := make([]byte, 64)
	for time.Now().Before(deadline) {
		n, err := port.Read(buf)
		if n == 0 || err != nil {
			return
		}
	}
}
