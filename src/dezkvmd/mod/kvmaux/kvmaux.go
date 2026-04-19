package kvmaux

/*
	DezAux - Auxiliary MCU Control for DezkVM

	This module provides functions to interact with the auxiliary MCU (CH552G)
	used in DezkVM for managing USB switching and power/reset button simulation.
*/

import (
	"fmt"
	"sync"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/kvmaux/serial"
)

type USB_mass_storage_side int

const (
	USB_MASS_STORAGE_UNKNOWN USB_mass_storage_side = iota
	USB_MASS_STORAGE_KVM
	USB_MASS_STORAGE_REMOTE
)

type AuxMcu struct {
	/* Mass Storage Switch */
	usb_mass_storage_side USB_mass_storage_side

	/* ATX States */
	pwr_led_on bool
	hdd_led_on bool

	/* Communication */
	port *serial.Port
	mu   sync.Mutex
}

// NewAuxOutbandController initializes a new AuxMcu instance
func NewAuxOutbandController(portName string, baudRate int) (*AuxMcu, error) {
	port, err := serial.OpenPort(&serial.Config{
		Name:        portName,
		Baud:        baudRate,
		ReadTimeout: time.Second * 2,
	})
	if err != nil {
		return nil, err
	}
	return &AuxMcu{
		usb_mass_storage_side: USB_MASS_STORAGE_KVM, //Default to KVM side, defined in MCU firmware
		port:                  port,
	}, nil
}

func (c *AuxMcu) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port != nil {
		c.port.Flush()
		err := c.port.Close()
		c.port = nil
		return err
	}
	return nil
}

// sendCommand writes a single byte command to the serial port
func (c *AuxMcu) sendCommand(cmd byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.port.Write([]byte{cmd})
	return err
}

// SwitchUSBToKVM switches USB mass storage to KVM side
func (c *AuxMcu) SwitchUSBToKVM() error {
	c.usb_mass_storage_side = USB_MASS_STORAGE_KVM
	return c.sendCommand('m')
}

// SwitchUSBToRemote switches USB mass storage to remote computer
func (c *AuxMcu) SwitchUSBToRemote() error {
	c.usb_mass_storage_side = USB_MASS_STORAGE_REMOTE
	return c.sendCommand('n')
}

// PressPowerButton simulates pressing the power button
func (c *AuxMcu) PressPowerButton() error {
	return c.sendCommand('p')
}

// ReleasePowerButton simulates releasing the power button
func (c *AuxMcu) ReleasePowerButton() error {
	return c.sendCommand('s')
}

// PressResetButton simulates pressing the reset button
func (c *AuxMcu) PressResetButton() error {
	return c.sendCommand('r')
}

// ReleaseResetButton simulates releasing the reset button
func (c *AuxMcu) ReleaseResetButton() error {
	return c.sendCommand('d')
}

// readByte reads a single byte from the serial port directly
func (c *AuxMcu) readByte() (byte, error) {
	buf := make([]byte, 1)
	_, err := c.port.ReadFull(buf)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

// readBytes reads exactly n bytes from the serial port directly
func (c *AuxMcu) readBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := c.port.ReadFull(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// GetUUID requests the device UUID and returns it as a string
// Protocol: <Length> 0x62 <UUID String>
func (c *AuxMcu) GetUUID() (string, error) {
	if err := c.sendCommand('u'); err != nil {
		return "", err
	}

	// Read length byte
	lengthBuf, err := c.readByte()
	if err != nil {
		return "", fmt.Errorf("failed to read length byte: %w", err)
	}

	length := int(lengthBuf)

	// Read command identifier (should be 0x62)
	cmdBuf, err := c.readByte()
	if err != nil {
		return "", fmt.Errorf("failed to read command identifier: %w", err)
	}
	if cmdBuf != 0x62 {
		return "", fmt.Errorf("invalid command identifier: expected 0x62, got 0x%02x", cmdBuf)
	}

	// Read UUID string (length - 1 bytes, since length includes the command identifier byte)
	uuidBuf, err := c.readBytes(length - 1)
	if err != nil {
		return "", fmt.Errorf("failed to read UUID: %w", err)
	}

	return string(uuidBuf), nil
}

// GetUSBMassStorageSide queries the device for the current USB mass storage side
// Protocol: 0x02 0x63 <side, 0x00=kvm, 0x01=remote>
func (c *AuxMcu) GetUSBMassStorageSide() USB_mass_storage_side {
	if c == nil {
		return USB_MASS_STORAGE_UNKNOWN
	}

	if err := c.sendCommand('y'); err != nil {
		return USB_MASS_STORAGE_UNKNOWN
	}

	// Read length byte (should be 0x02)
	lengthBuf, err := c.readByte()
	if err != nil {
		return USB_MASS_STORAGE_UNKNOWN
	}
	if lengthBuf != 0x02 {
		return USB_MASS_STORAGE_UNKNOWN
	}

	// Read command identifier (should be 0x63)
	cmdBuf, err := c.readByte()
	if err != nil {
		return USB_MASS_STORAGE_UNKNOWN
	}
	if cmdBuf != 0x63 {
		return USB_MASS_STORAGE_UNKNOWN
	}

	// Read side byte (0x00=KVM, 0x01=remote)
	sideBuf, err := c.readByte()
	if err != nil {
		return USB_MASS_STORAGE_UNKNOWN
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	switch sideBuf {
	case 0x00:
		c.usb_mass_storage_side = USB_MASS_STORAGE_KVM
		return USB_MASS_STORAGE_KVM
	case 0x01:
		c.usb_mass_storage_side = USB_MASS_STORAGE_REMOTE
		return USB_MASS_STORAGE_REMOTE
	}

	return USB_MASS_STORAGE_UNKNOWN
}

// GetATXState queries the device for ATX power and HDD LED status
// Protocol: 0x02 0x61 <Status Byte>
// Status Byte format:
//
//	Bit 0: PWR LED status
//	Bit 1: HDD LED status
//	Bit 2: USB Mass Storage mounted side
//	Bit 3-7: Reserved
func (c *AuxMcu) GetATXState() error {
	if c == nil {
		return fmt.Errorf("AuxMcu is nil")
	}

	if err := c.sendCommand('a'); err != nil {
		return err
	}

	// Read length byte (should be 0x02)
	lengthBuf, err := c.readByte()
	if err != nil {
		return fmt.Errorf("failed to read length byte: %w", err)
	}
	if lengthBuf != 0x02 {
		return fmt.Errorf("invalid length byte: expected 0x02, got 0x%02x", lengthBuf)
	}

	// Read command identifier (should be 0x61)
	cmdBuf, err := c.readByte()
	if err != nil {
		return fmt.Errorf("failed to read command identifier: %w", err)
	}
	if cmdBuf != 0x61 {
		return fmt.Errorf("invalid command identifier: expected 0x61, got 0x%02x", cmdBuf)
	}

	// Read status byte
	statusBuf, err := c.readByte()
	if err != nil {
		return fmt.Errorf("failed to read status byte: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Parse status byte
	c.pwr_led_on = (statusBuf & 0x01) != 0
	c.hdd_led_on = (statusBuf & 0x02) != 0

	// Update USB mass storage side from bit 2
	if (statusBuf & 0x04) != 0 {
		c.usb_mass_storage_side = USB_MASS_STORAGE_REMOTE
	} else {
		c.usb_mass_storage_side = USB_MASS_STORAGE_KVM
	}

	return nil
}

// GetPowerLEDState returns the cached power LED state
func (c *AuxMcu) GetPowerLEDState() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pwr_led_on
}

// GetHDDLEDState returns the cached HDD LED state
func (c *AuxMcu) GetHDDLEDState() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hdd_led_on
}
