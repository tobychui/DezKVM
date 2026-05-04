//go:build linux

package main

import (
	"path/filepath"
	"sort"
)

// listPorts returns available serial port device paths by globbing common
// USB CDC ACM and USB serial converter device nodes.
func listPorts() []string {
	var ports []string
	for _, pattern := range []string{"/dev/ttyACM*", "/dev/ttyUSB*"} {
		matches, _ := filepath.Glob(pattern)
		ports = append(ports, matches...)
	}
	sort.Strings(ports)
	return ports
}
