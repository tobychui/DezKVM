package kvmswitch

/*
	kvmswitch.go

	This file contains the serial communication logic
	for operation a KVM switch using a USB CDC Serial device
	interface.

	The switch control MCU will appears as a USB CDC serial device
	and accept the following commands:
	'0' - Switch to PC 1
	'1' - Switch to PC 2
	'?' - Get current switch state (returns '0' or '1')
	'u' - Get the uuid of the switch, The uuidv4 string should start with '3'
	(catergory: KVM switch) follow by the device type (e.g. 1 for dezKVM switch)
	and the remaining bytes are the unique identifier for the device.
	A full example: 316eb07f-1577-4996-9f4f-90ff5e11c693
*/

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"imuslab.com/dezkvm/switchapp/internal/kvmswitch/serial"
)

// uuidResponseLen is the number of bytes in a UUID response from the device.
// Protocol: 0x25 (length byte) + 0x62 (command id) + 36-char UUID string = 38 bytes total.
const uuidResponseLen = 38

type Device struct {
	DevicePath  string // e.g. "COM3" on Windows or "/dev/ttyACM0" on Linux
	Baudrate    int    // Default to 115200
	DeviceUUID  string // The uuid of the device, obtained by sending 'u' command to the device
	DeviceType  int    // The type of the device, obtained from the uuid (e.g. 1 for dezKVM switch)
	CurrentSide int    // The current switch state, obtained by sending '?' command to the device (0 or 1)

	port *serial.Port
}

// NewKVMSwitchDevice initializes a new Device struct by communicating with the KVM switch.
func NewKVMSwitchDevice(devicePath string, baudrate int) (*Device, error) {
	if baudrate == 0 {
		baudrate = 115200
	}

	port, err := serial.OpenPort(&serial.Config{
		Name:        devicePath,
		Baud:        baudrate,
		ReadTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open serial port: %w", err)
	}

	d := &Device{
		DevicePath: devicePath,
		Baudrate:   baudrate,
		port:       port,
	}

	// Give the MCU's USB CDC stack time to finish initialising before we
	// start sending commands.  A human using Arduino Serial Monitor naturally
	// waits a second or two; our code must do the same.
	time.Sleep(1 * time.Second)

	// Get UUID and device type — retry up to 3 times with a delay in case
	// the MCU is still initializing.
	var uuid string
	var devType int
	for attempt := 1; attempt <= 3; attempt++ {
		uuid, devType, err = d.queryUUID()
		if err == nil {
			break
		}
		if attempt < 3 {
			port.Flush()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("query uuid (after 3 attempts): %w", err)
	}
	d.DeviceUUID = uuid
	d.DeviceType = devType

	// Get current switch state — retry up to 3 times.
	var side int
	for attempt := 1; attempt <= 3; attempt++ {
		side, err = d.querySide()
		if err == nil {
			break
		}
		if attempt < 3 {
			port.Flush()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("query current side (after 3 attempts): %w", err)
	}
	d.CurrentSide = side

	return d, nil
}

// SwitchToPC1 switches the KVM to PC 1 (side 0).
// The firmware responds with 0xFF on success.
func (d *Device) SwitchToPC1() error {
	return d.switchTo(0)
}

// SwitchToPC2 switches the KVM to PC 2 (side 1).
// The firmware responds with 0xFF on success.
func (d *Device) SwitchToPC2() error {
	return d.switchTo(1)
}

// GetCurrentSide queries the device for the current switch state and returns 0 or 1.
// If the port is no longer open it returns the cached CurrentSide value.
func (d *Device) GetCurrentSide() (int, error) {
	if d.port == nil {
		return d.CurrentSide, nil
	}
	side, err := d.querySide()
	if err != nil {
		return 0, err
	}
	d.CurrentSide = side
	return side, nil
}

// Close closes the underlying serial port.
func (d *Device) Close() error {
	if d.port != nil {
		err := d.port.Close()
		d.port = nil
		return err
	}
	return nil
}

// IsConnected reports whether the underlying serial port is still open.
// For DeviceType 1 this returns false after a successful switch because the
// MCU moves the USB connection to the other PC.
func (d *Device) IsConnected() bool {
	return d.port != nil
}

// Probe actively checks if the device is still reachable by sending a '?'
// command and reading back the response. Returns nil if the device responds
// correctly, or an error if the port is gone or unresponsive. On success it
// also updates CurrentSide.
func (d *Device) Probe() error {
	if d.port == nil {
		return fmt.Errorf("port not open")
	}
	side, err := d.querySide()
	if err != nil {
		_ = d.port.Close()
		d.port = nil
		return err
	}
	d.CurrentSide = side
	return nil
}

// --- internal helpers ---

// switchTo sends command '0' or '1' and reads the 0xFF acknowledgement byte.
func (d *Device) switchTo(side int) error {
	if d.port == nil {
		return fmt.Errorf("device not connected")
	}

	cmd := byte('0')
	if side == 1 {
		cmd = '1'
	}

	if err := d.port.Flush(); err != nil {
		// non-fatal
		_ = err
	}

	if _, err := d.port.Write([]byte{cmd}); err != nil {
		return fmt.Errorf("write switch command: %w", err)
	}

	// DeviceType 1 (dezKVM): the MCU physically re-routes the USB bus to the
	// other PC as soon as it receives the command.  The serial port on this PC
	// disappears immediately, so no 0xFF ack will ever arrive.  A successful
	// write is sufficient to confirm the switch; close the now-dead port.
	if d.DeviceType == 1 {
		d.CurrentSide = side
		_ = d.port.Close()
		d.port = nil
		return nil
	}

	ack := make([]byte, 1)
	if _, err := d.port.ReadFull(ack); err != nil {
		return fmt.Errorf("read ack: %w", err)
	}
	if ack[0] != 0xFF {
		return fmt.Errorf("unexpected ack byte: 0x%02X", ack[0])
	}

	d.CurrentSide = side
	return nil
}

// querySide sends '?' and reads back a single ASCII digit ('0' or '1') followed by CRLF.
func (d *Device) querySide() (int, error) {
	if err := d.port.Flush(); err != nil {
		_ = err
	}

	if _, err := d.port.Write([]byte{'?'}); err != nil {
		return 0, fmt.Errorf("write '?' command: %w", err)
	}

	// Firmware sends USBSerial_println which appends "\r\n" — read up to 3 bytes.
	buf := make([]byte, 3)
	n, err := d.port.Read(buf)
	if err != nil {
		return 0, fmt.Errorf("read side response: %w", err)
	}
	if n == 0 {
		return 0, fmt.Errorf("no response from device")
	}

	ch := buf[0]
	if ch != '0' && ch != '1' {
		return 0, fmt.Errorf("unexpected side byte: 0x%02X", ch)
	}

	side, err := strconv.Atoi(string(ch))
	if err != nil {
		return 0, fmt.Errorf("parse side: %w", err)
	}
	return side, nil
}

// queryUUID sends 'u' and reads the binary UUID response:
//
//	byte[0] = 0x25 (length, 37 = 1 cmd byte + 36 uuid chars)
//	byte[1] = 0x62 (command identifier)
//	bytes[2..37] = 36-char UUID string (e.g. "316eb07f-1577-4996-9f4f-90ff5e11c693")
//
// Returns the UUID string and the device type encoded in uuid[1] ('1', '2', …).
func (d *Device) queryUUID() (string, int, error) {
	if err := d.port.Flush(); err != nil {
		_ = err
	}

	if _, err := d.port.Write([]byte{'u'}); err != nil {
		return "", 0, fmt.Errorf("write 'u' command: %w", err)
	}

	d.port.Flush()

	resp := make([]byte, uuidResponseLen)
	if _, err := d.port.ReadFull(resp); err != nil {
		return "", 0, fmt.Errorf("read uuid response: %w", err)
	}

	if resp[0] != 0x25 {
		return "", 0, fmt.Errorf("bad length byte: 0x%02X", resp[0])
	}
	if resp[1] != 0x62 {
		return "", 0, fmt.Errorf("bad command byte: 0x%02X", resp[1])
	}

	uuidStr := strings.TrimRight(string(bytes.TrimRight(resp[2:], "\x00")), "\r\n ")
	if len(uuidStr) != 36 {
		return "", 0, fmt.Errorf("unexpected uuid length %d: %q", len(uuidStr), uuidStr)
	}

	// Device type is encoded as the second character of the UUID string.
	// e.g. "316eb07f-..." → devType = 1
	devType, err := strconv.Atoi(string(uuidStr[1]))
	if err != nil {
		return "", 0, fmt.Errorf("parse device type from uuid: %w", err)
	}

	return uuidStr, devType, nil
}
