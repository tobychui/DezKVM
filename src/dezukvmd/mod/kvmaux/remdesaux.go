package kvmaux

/*
	RemdesAux - Auxiliary MCU Control for RemdeskVM

	This module provides functions to interact with the auxiliary MCU (CH552G)
	used in RemdeskVM for managing USB switching and power/reset button simulation.
*/

import (
	"bufio"
	"strings"
	"sync"
	"time"

	"github.com/tarm/serial"
)

type USB_mass_storage_side int

const (
	USB_MASS_STORAGE_KVM USB_mass_storage_side = iota
	USB_MASS_STORAGE_REMOTE
)

type AuxMcu struct {
	usb_mass_storage_side USB_mass_storage_side
	port                  *serial.Port
	reader                *bufio.Reader
	mu                    sync.Mutex
}

// NewAuxOutbandController initializes a new AuxMcu instance
func NewAuxOutbandController(portName string, baudRate int) (*AuxMcu, error) {
	c := &serial.Config{
		Name:        portName,
		Baud:        baudRate,
		ReadTimeout: time.Second * 2,
	}
	port, err := serial.OpenPort(c)
	if err != nil {
		return nil, err
	}
	return &AuxMcu{
		usb_mass_storage_side: USB_MASS_STORAGE_KVM, //Default to KVM side, defined in MCU firmware
		port:                  port,
		reader:                bufio.NewReader(port),
	}, nil
}

func (c *AuxMcu) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port != nil {
		return c.port.Close()
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

// GetUUID requests the device UUID and returns it as a string
func (c *AuxMcu) GetUUID() (string, error) {
	if err := c.sendCommand('u'); err != nil {
		return "", err
	}
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)
	return line, nil
}

func (c *AuxMcu) GetUSBMassStorageSide() USB_mass_storage_side {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usb_mass_storage_side
}
