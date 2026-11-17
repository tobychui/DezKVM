package kvmaux

/*
	protocol.go

	Framing for the AUX MCU protocol v2. See
	firmware/Production/usbkvm_fw_v2/PROTOCOL.md for the full description.

	Frame: 0xA5 LEN CMD SEQ PAYLOAD CRC8
	LEN covers CMD + SEQ + PAYLOAD, CRC8 (poly 0x07, init 0x00) covers LEN..PAYLOAD
	Replies use CMD | 0x80, echo SEQ and start their payload with a status byte
*/

import (
	"errors"
	"fmt"
)

const (
	frameSync     byte = 0xA5
	frameMinLen        = 2
	frameMaxLen        = 40
	replyFlag     byte = 0x80
	protoVersion2 byte = 0x02
)

// Command IDs
const (
	cmdPing           byte = 0x01
	cmdGetUUID        byte = 0x02
	cmdGetStatus      byte = 0x03
	cmdSetUSBSide     byte = 0x10
	cmdPulsePower     byte = 0x20
	cmdPulseReset     byte = 0x21
	cmdReleaseButtons byte = 0x22
	cmdSetLED         byte = 0x30
)

// Reply status codes
const (
	statusOK         byte = 0x00
	statusUnknownCmd byte = 0x01
	statusBadArg     byte = 0x02
)

// GET_STATUS flag bits
const (
	flagPowerLED      byte = 0x01
	flagHDDLED        byte = 0x02
	flagUSBRemote     byte = 0x04
	flagPowerButton   byte = 0x08
	flagResetButton   byte = 0x10
	flagHostConnected byte = 0x20
)

var (
	ErrUnknownCommand = errors.New("AUX MCU does not support this command")
	ErrBadArgument    = errors.New("AUX MCU rejected the command arguments")
)

// crc8 computes CRC-8 with polynomial 0x07 and initial value 0x00
func crc8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// encodeFrame builds a request frame
func encodeFrame(cmd byte, seq byte, payload []byte) ([]byte, error) {
	length := 2 + len(payload)
	if length > frameMaxLen {
		return nil, fmt.Errorf("payload too long: %d bytes", len(payload))
	}
	frame := make([]byte, 0, length+3)
	frame = append(frame, frameSync, byte(length), cmd, seq)
	frame = append(frame, payload...)
	frame = append(frame, crc8(frame[1:]))
	return frame, nil
}

// statusError converts a reply status byte into an error
func statusError(status byte) error {
	switch status {
	case statusOK:
		return nil
	case statusUnknownCmd:
		return ErrUnknownCommand
	case statusBadArg:
		return ErrBadArgument
	default:
		return fmt.Errorf("AUX MCU returned unknown status 0x%02x", status)
	}
}
