//go:build windows

package main

import (
	"sort"

	"golang.org/x/sys/windows/registry"
)

// listPorts returns available serial port names by reading the Windows registry
// key HKLM\HARDWARE\DEVICEMAP\SERIALCOMM, which lists all active COM ports.
func listPorts() []string {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`HARDWARE\DEVICEMAP\SERIALCOMM`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return nil
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil
	}

	var ports []string
	for _, name := range names {
		val, _, err := key.GetStringValue(name)
		if err == nil && val != "" {
			ports = append(ports, val)
		}
	}
	sort.Strings(ports)
	return ports
}
