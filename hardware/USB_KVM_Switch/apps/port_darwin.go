//go:build darwin

package main

import (
	"path/filepath"
	"sort"
)

// listPorts returns available serial port device paths on macOS by globbing
// common USB serial device nodes.
func listPorts() []string {
	var ports []string
	// Common macOS serial port patterns for CH552g-based KVM switch devices include:
	// - /dev/tty.usbmodem* : Apple USB modem devices (including some CDC ACM devices)
	for _, pattern := range []string{
		"/dev/tty.usbserial*",
		"/dev/tty.SLAB_USBtoUART*",
		"/dev/tty.wchuart*",
		"/dev/tty.usbmodem*",
	} {
		matches, _ := filepath.Glob(pattern)
		ports = append(ports, matches...)
	}
	sort.Strings(ports)
	return ports
}
