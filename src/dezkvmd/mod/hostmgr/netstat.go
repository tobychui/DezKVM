package hostmgr

import (
	"bufio"
	"bytes"
	"math"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NetInterface is one network interface with its current throughput.
type NetInterface struct {
	Name    string   `json:"name"`
	MAC     string   `json:"mac"`
	Up      bool     `json:"up"`
	Addrs   []string `json:"addrs"`
	RxBytes uint64   `json:"rx_bytes"` // since boot
	TxBytes uint64   `json:"tx_bytes"`
	RxBps   float64  `json:"rx_bps"` // bits per second over the last sample
	TxBps   float64  `json:"tx_bps"`
}

type ifaceCounters struct{ rx, tx uint64 }

// readNetDev parses /proc/net/dev into per-interface byte counters.
func readNetDev() (map[string]ifaceCounters, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	return parseNetDev(data), nil
}

func parseNetDev(data []byte) map[string]ifaceCounters {
	out := map[string]ifaceCounters{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		name, rest, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue // the two header lines
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[strings.TrimSpace(name)] = ifaceCounters{rx, tx}
	}
	return out
}

// netSampler measures the throughput of every interface once per second.
type netSampler struct {
	mu     sync.RWMutex
	last   map[string]ifaceCounters
	lastAt time.Time
	rates  map[string][2]float64 // bits per second: rx, tx
}

func (n *netSampler) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	n.sample(time.Now())
	for now := range ticker.C {
		n.sample(now)
	}
}

func (n *netSampler) sample(now time.Time) {
	counters, err := readNetDev()
	if err != nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	rates := map[string][2]float64{}
	if secs := now.Sub(n.lastAt).Seconds(); n.last != nil && secs > 0 {
		for name, c := range counters {
			prev, ok := n.last[name]
			if !ok || c.rx < prev.rx || c.tx < prev.tx {
				continue // new interface or counter reset
			}
			rates[name] = [2]float64{
				math.Round(float64(c.rx-prev.rx) * 8 / secs),
				math.Round(float64(c.tx-prev.tx) * 8 / secs),
			}
		}
	}
	n.last, n.lastAt, n.rates = counters, now, rates
}

// interfaces lists the interfaces (except loopback) with their addresses
// and current rates, sorted by name.
func (n *netSampler) interfaces() []NetInterface {
	n.mu.RLock()
	counters, rates := n.last, n.rates
	n.mu.RUnlock()

	list := []NetInterface{}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		entry := NetInterface{
			Name:  iface.Name,
			MAC:   iface.HardwareAddr.String(),
			Up:    iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0,
			Addrs: []string{},
		}
		if addrs, err := iface.Addrs(); err == nil {
			for _, a := range addrs {
				entry.Addrs = append(entry.Addrs, a.String())
			}
		}
		if c, ok := counters[iface.Name]; ok {
			entry.RxBytes, entry.TxBytes = c.rx, c.tx
		}
		if r, ok := rates[iface.Name]; ok {
			entry.RxBps, entry.TxBps = r[0], r[1]
		}
		list = append(list, entry)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// totalBits returns the bits received and sent by all non-loopback
// interfaces since boot (the usage history derives the rate from it).
func totalBits() (rx int64, tx int64, err error) {
	counters, err := readNetDev()
	if err != nil {
		return 0, 0, err
	}
	for name, c := range counters {
		if name == "lo" {
			continue
		}
		rx += int64(c.rx * 8)
		tx += int64(c.tx * 8)
	}
	return rx, tx, nil
}
