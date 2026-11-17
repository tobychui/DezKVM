package usageinfo

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
)

// MemInfo is the system memory reported by /proc/meminfo, in bytes.
type MemInfo struct {
	Total     uint64
	Available uint64
	SwapTotal uint64
	SwapFree  uint64
}

// Used returns the memory in use, as `free` counts it (total - available).
func (m MemInfo) Used() uint64 {
	if m.Available > m.Total {
		return 0
	}
	return m.Total - m.Available
}

// ReadMemInfo parses /proc/meminfo (Linux). It is much cheaper than running
// `free` once per second on a small board.
func ReadMemInfo() (MemInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemInfo{}, err
	}
	return parseMemInfo(data)
}

func parseMemInfo(data []byte) (MemInfo, error) {
	var m MemInfo
	var haveTotal, haveAvail bool
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		key, rest, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		bytesValue := kb * 1024
		switch key {
		case "MemTotal":
			m.Total, haveTotal = bytesValue, true
		case "MemAvailable":
			m.Available, haveAvail = bytesValue, true
		case "SwapTotal":
			m.SwapTotal = bytesValue
		case "SwapFree":
			m.SwapFree = bytesValue
		}
	}
	if !haveTotal || !haveAvail || m.Total == 0 {
		return MemInfo{}, errors.New("MemTotal / MemAvailable missing in /proc/meminfo")
	}
	return m, nil
}
