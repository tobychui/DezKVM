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

func (c *AuxMcu) SetStatusLED(pattern StatusLEDPattern) error {
	var cmd byte
	switch pattern {
	case StatusLEDOff:
		cmd = '0'
	case StatusLEDOn:
		cmd = '1'
	case StatusLEDBlinkSlow:
		cmd = '2'
	case StatusLEDBlinkFast:
		cmd = '3'
	default:
		return fmt.Errorf("invalid status LED pattern: %d", pattern)
	}
	return c.sendCommand(cmd)
}
