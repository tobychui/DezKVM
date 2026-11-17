package kvmaux

/*
	leds.go

	Status LED Control for DezkVM Auxiliary MCU

	This file defines functions to control the status LED on the auxiliary MCU (CH552G)
	used in DezkVM port unit.
*/

import "fmt"

type StatusLEDPattern int

const (
	StatusLEDOff StatusLEDPattern = iota
	StatusLEDOn
	StatusLEDBlinkSlow
	StatusLEDBlinkFast
)

// LED modes as defined in protocol v2
const (
	ledModeOff  byte = 0
	ledModeOn   byte = 1
	ledModeSlow byte = 2
	ledModeFast byte = 3
)

func (c *AuxMcu) SetStatusLED(pattern StatusLEDPattern) error {
	var mode byte
	switch pattern {
	case StatusLEDOff:
		mode = ledModeOff
	case StatusLEDOn:
		mode = ledModeOn
	case StatusLEDBlinkSlow:
		mode = ledModeSlow
	case StatusLEDBlinkFast:
		mode = ledModeFast
	default:
		return fmt.Errorf("invalid status LED pattern: %d", pattern)
	}
	_, err := c.transact(cmdSetLED, []byte{mode}, auxDefaultDeadline)
	return err
}

// ledModeToPattern converts a protocol LED mode back to a StatusLEDPattern
func ledModeToPattern(mode byte) StatusLEDPattern {
	switch mode {
	case ledModeOff:
		return StatusLEDOff
	case ledModeOn:
		return StatusLEDOn
	case ledModeFast:
		return StatusLEDBlinkFast
	default:
		return StatusLEDBlinkSlow
	}
}
