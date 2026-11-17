package kvmaux

/*
	DezAux - Auxiliary MCU Control for DezkVM

	This module provides functions to interact with the auxiliary MCU (CH552G)
	used in DezkVM for managing USB switching and power/reset button simulation.

	It speaks protocol v2 (firmware usbkvm_fw_v2): framed, checksummed,
	strictly request/response. Nothing is cached as the source of truth, the
	MCU is queried with GET_STATUS whenever its state is needed.
*/

import (
	"errors"
	"fmt"
	"log"
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

// ResetCause tells why the AUX MCU last started
type ResetCause byte

const (
	ResetCauseSoftware ResetCause = 0
	ResetCausePowerOn  ResetCause = 1
	ResetCauseWatchdog ResetCause = 2
	ResetCausePin      ResetCause = 3
)

func (r ResetCause) String() string {
	switch r {
	case ResetCauseSoftware:
		return "software"
	case ResetCausePowerOn:
		return "power-on"
	case ResetCauseWatchdog:
		return "watchdog"
	case ResetCausePin:
		return "reset-pin"
	default:
		return fmt.Sprintf("unknown(%d)", byte(r))
	}
}

// FirmwareInfo is returned by PING
type FirmwareInfo struct {
	ProtocolVersion byte
	VersionMajor    byte
	VersionMinor    byte
	HasATXControl   bool
}

// Status is a snapshot of everything the AUX MCU knows
type Status struct {
	PowerLEDOn         bool
	HDDLEDOn           bool
	USBSide            USB_mass_storage_side
	PowerButtonPressed bool
	ResetButtonPressed bool
	HostConnected      bool
	LEDMode            StatusLEDPattern
	ResetCause         ResetCause
	Uptime             time.Duration
}

const (
	auxReadTimeout     = 100 * time.Millisecond // Per read() quiet timeout (VTIME)
	auxDefaultDeadline = 500 * time.Millisecond // Reply deadline for normal commands
	auxSwitchDeadline  = 1500 * time.Millisecond
	auxRetries         = 3
	maxButtonDuration  = 10 * time.Second // Firmware hard limit
)

type AuxMcu struct {
	/* Communication, mu serializes whole request/response transactions */
	port *serial.Port
	mu   sync.Mutex
	seq  byte

	/* Last known values, only used for display and reboot detection */
	stateMu    sync.Mutex
	lastStatus Status
	haveStatus bool
	firmware   FirmwareInfo
}

// NewAuxOutbandController opens the AUX MCU port and checks that it speaks protocol v2
func NewAuxOutbandController(portName string, baudRate int) (*AuxMcu, error) {
	port, err := serial.OpenPort(&serial.Config{
		Name:        portName,
		Baud:        baudRate,
		ReadTimeout: auxReadTimeout,
	})
	if err != nil {
		return nil, err
	}
	c := &AuxMcu{port: port}

	info, err := c.Ping()
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("AUX MCU on %s did not answer a v2 PING (is it running usbkvm_fw_v2?): %w", portName, err)
	}
	if info.ProtocolVersion != protoVersion2 {
		port.Close()
		return nil, fmt.Errorf("AUX MCU on %s speaks protocol v%d, expected v%d", portName, info.ProtocolVersion, protoVersion2)
	}
	log.Printf("AUX MCU on %s: firmware v%d.%d, protocol v%d, ATX control: %v\n",
		portName, info.VersionMajor, info.VersionMinor, info.ProtocolVersion, info.HasATXControl)
	return c, nil
}

// Close closes the serial port. Nothing needs to be sent before closing:
// the firmware releases the ATX buttons and resets its LED when DTR drops.
func (c *AuxMcu) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port != nil {
		err := c.port.Close()
		c.port = nil
		return err
	}
	return nil
}

// transact sends one command and waits for its matching reply. It retries
// with a new sequence number on timeout; every v2 command is safe to repeat.
// It returns the reply payload after the status byte.
func (c *AuxMcu) transact(cmd byte, payload []byte, deadline time.Duration) ([]byte, error) {
	if c == nil {
		return nil, errors.New("AuxMcu is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return nil, errors.New("AUX MCU port is closed")
	}

	var lastErr error
	for attempt := 0; attempt < auxRetries; attempt++ {
		c.seq++
		seq := c.seq
		frame, err := encodeFrame(cmd, seq, payload)
		if err != nil {
			return nil, err
		}

		// Drop anything left over from an earlier timed-out request
		_ = c.port.Flush()
		if _, err := c.port.Write(frame); err != nil {
			return nil, fmt.Errorf("failed to write command 0x%02x: %w", cmd, err)
		}

		reply, err := c.readReply(cmd|replyFlag, seq, time.Now().Add(deadline))
		if err == nil {
			if len(reply) < 1 {
				return nil, fmt.Errorf("reply to command 0x%02x has no status byte", cmd)
			}
			if err := statusError(reply[0]); err != nil {
				return nil, err
			}
			return reply[1:], nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no reply to command 0x%02x after %d attempts: %w", cmd, auxRetries, lastErr)
}

// readReply scans the incoming bytes for a valid frame with the expected
// command and sequence number, discarding anything else. Returns CMD/SEQ-stripped payload.
func (c *AuxMcu) readReply(wantCmd byte, wantSeq byte, deadline time.Time) ([]byte, error) {
	for {
		b, err := c.readByteBefore(deadline)
		if err != nil {
			return nil, err
		}
		if b != frameSync {
			continue
		}

		length, err := c.readByteBefore(deadline)
		if err != nil {
			return nil, err
		}
		if length < frameMinLen+1 || length > frameMaxLen {
			// Not a reply frame, keep scanning
			continue
		}

		body := make([]byte, int(length)+1) // CMD SEQ PAYLOAD CRC
		for i := range body {
			body[i], err = c.readByteBefore(deadline)
			if err != nil {
				return nil, err
			}
		}
		crc := body[len(body)-1]
		body = body[:len(body)-1]
		if crc8(append([]byte{length}, body...)) != crc {
			continue
		}
		if body[0] != wantCmd || body[1] != wantSeq {
			// Stale reply to an earlier request
			continue
		}
		return body[2:], nil
	}
}

// readByteBefore reads a single byte, retrying on read timeouts until deadline
func (c *AuxMcu) readByteBefore(deadline time.Time) (byte, error) {
	buf := make([]byte, 1)
	for {
		n, err := c.port.Read(buf)
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

// Ping returns the firmware and protocol version
func (c *AuxMcu) Ping() (FirmwareInfo, error) {
	reply, err := c.transact(cmdPing, nil, auxDefaultDeadline)
	if err != nil {
		return FirmwareInfo{}, err
	}
	if len(reply) < 4 {
		return FirmwareInfo{}, fmt.Errorf("short PING reply: %d bytes", len(reply))
	}
	info := FirmwareInfo{
		ProtocolVersion: reply[0],
		VersionMajor:    reply[1],
		VersionMinor:    reply[2],
		HasATXControl:   reply[3]&0x01 != 0,
	}
	c.stateMu.Lock()
	c.firmware = info
	c.stateMu.Unlock()
	return info, nil
}

// GetUUID requests the device UUID and returns it as a string
func (c *AuxMcu) GetUUID() (string, error) {
	reply, err := c.transact(cmdGetUUID, nil, auxDefaultDeadline)
	if err != nil {
		return "", err
	}
	if len(reply) != 36 {
		return "", fmt.Errorf("invalid UUID length: %d", len(reply))
	}
	return string(reply), nil
}

// GetStatus queries the full state of the AUX MCU
func (c *AuxMcu) GetStatus() (Status, error) {
	reply, err := c.transact(cmdGetStatus, nil, auxDefaultDeadline)
	if err != nil {
		return Status{}, err
	}
	if len(reply) < 7 {
		return Status{}, fmt.Errorf("short GET_STATUS reply: %d bytes", len(reply))
	}
	flags := reply[0]
	side := USB_MASS_STORAGE_KVM
	if flags&flagUSBRemote != 0 {
		side = USB_MASS_STORAGE_REMOTE
	}
	uptimeSec := uint32(reply[3])<<24 | uint32(reply[4])<<16 | uint32(reply[5])<<8 | uint32(reply[6])
	status := Status{
		PowerLEDOn:         flags&flagPowerLED != 0,
		HDDLEDOn:           flags&flagHDDLED != 0,
		USBSide:            side,
		PowerButtonPressed: flags&flagPowerButton != 0,
		ResetButtonPressed: flags&flagResetButton != 0,
		HostConnected:      flags&flagHostConnected != 0,
		LEDMode:            ledModeToPattern(reply[1]),
		ResetCause:         ResetCause(reply[2]),
		Uptime:             time.Duration(uptimeSec) * time.Second,
	}

	c.stateMu.Lock()
	if c.haveStatus && status.Uptime < c.lastStatus.Uptime {
		log.Printf("Warning: AUX MCU restarted (reset cause: %s), its outputs are back to power-on defaults\n", status.ResetCause)
	}
	c.lastStatus = status
	c.haveStatus = true
	c.stateMu.Unlock()
	return status, nil
}

// GetUSBMassStorageSide queries the device for the current USB mass storage side
func (c *AuxMcu) GetUSBMassStorageSide() USB_mass_storage_side {
	if c == nil {
		return USB_MASS_STORAGE_UNKNOWN
	}
	status, err := c.GetStatus()
	if err != nil {
		return USB_MASS_STORAGE_UNKNOWN
	}
	return status.USBSide
}

// setUSBSide switches the USB mass storage mux and checks the side reported back
func (c *AuxMcu) setUSBSide(side byte) error {
	reply, err := c.transact(cmdSetUSBSide, []byte{side}, auxSwitchDeadline)
	if err != nil {
		return err
	}
	if len(reply) < 1 || reply[0] != side {
		return fmt.Errorf("USB mass storage switch did not take effect")
	}
	return nil
}

// SwitchUSBToKVM switches USB mass storage to KVM side
func (c *AuxMcu) SwitchUSBToKVM() error {
	return c.setUSBSide(0x00)
}

// SwitchUSBToRemote switches USB mass storage to remote computer
func (c *AuxMcu) SwitchUSBToRemote() error {
	return c.setUSBSide(0x01)
}

// pulse presses a button for duration; the firmware releases it on its own
func (c *AuxMcu) pulse(cmd byte, duration time.Duration) error {
	if duration <= 0 || duration > maxButtonDuration {
		return fmt.Errorf("button press duration must be between 1ms and %v", maxButtonDuration)
	}
	ms := uint16(duration / time.Millisecond)
	if ms == 0 {
		ms = 1
	}
	_, err := c.transact(cmd, []byte{byte(ms >> 8), byte(ms)}, auxDefaultDeadline)
	return err
}

// PulsePowerButton presses the power button for duration, released by the firmware
func (c *AuxMcu) PulsePowerButton(duration time.Duration) error {
	return c.pulse(cmdPulsePower, duration)
}

// PulseResetButton presses the reset button for duration, released by the firmware
func (c *AuxMcu) PulseResetButton(duration time.Duration) error {
	return c.pulse(cmdPulseReset, duration)
}

// ReleaseButtons releases both ATX buttons immediately
func (c *AuxMcu) ReleaseButtons() error {
	_, err := c.transact(cmdReleaseButtons, nil, auxDefaultDeadline)
	return err
}

// GetATXState queries the device for ATX power and HDD LED status and
// updates the values returned by GetPowerLEDState and GetHDDLEDState
func (c *AuxMcu) GetATXState() error {
	if c == nil {
		return fmt.Errorf("AuxMcu is nil")
	}
	_, err := c.GetStatus()
	return err
}

// GetPowerLEDState returns the power LED state from the last status query
func (c *AuxMcu) GetPowerLEDState() bool {
	if c == nil {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.lastStatus.PowerLEDOn
}

// GetHDDLEDState returns the HDD LED state from the last status query
func (c *AuxMcu) GetHDDLEDState() bool {
	if c == nil {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.lastStatus.HDDLEDOn
}
