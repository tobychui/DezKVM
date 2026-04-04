package main

/*
	OLED driver Linux example for Raspberry Pi and similar devices.

	Displays hostname, CPU%, RAM%, local IPv4 addresses and internet
	connectivity on a 128x64 OLED via the PicoHMI serial protocol.

	Usage:
	  ./example [-port /dev/ttyACM0]
*/

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	picohmi "imuslab.com/dezkvm/oled_sysinfo/mod/PicoHMI"
)

var ttyPort = flag.String("port", defaultSerialPort, "Serial port connected to the OLED board")
var refreshInterval = flag.Duration("refresh", 2*time.Second, "Screen refresh interval (screen takes 3 seconds to update, so total will be refresh + 3s)")
var internetCheckInterval = flag.Duration("netcheck", 30*time.Second, "Interval for checking internet connectivity when connected")
var internetCheckIntervalDisconnected = flag.Duration("netcheck-retry", 5*time.Second, "Interval for checking internet connectivity when disconnected")
var frameToggle = false               // Used to alternate screen content for visual interest
var internetConnected = "Checking..." // Current internet connection status
var internetStatusMutex sync.Mutex    // Mutex to protect internetConnected access

func main() {
	flag.Parse()

	cfg := &picohmi.Config{
		SerialDevice: *ttyPort,
		BaudRate:     115200,
	}

	disp, err := picohmi.NewDisplay(cfg)
	if err != nil {
		log.Fatalf("NewDisplay: %v", err)
	}

	if err := disp.Connect(); err != nil {
		log.Fatalf("Connect: %v", err)
	}
	defer disp.Disconnect()

	log.Printf("Connected to OLED on %s", *ttyPort)

	// Get and display device UUID - retry every 5 seconds until successful
	var uuid string
	for {
		var err error
		uuid, err = disp.GetUUID()
		if err == nil {
			log.Printf("Device UUID: %s", uuid)
			break
		}
		log.Printf("Warning: Failed to get UUID: %v - retrying in 5 seconds...", err)
		time.Sleep(5 * time.Second)
	}

	// Start background internet connectivity monitor
	log.Println("Starting background internet connectivity monitor...")
	go monitorInternetConnectivity()

	log.Println("Entering main display loop...")
	time.Sleep(1 * time.Second)

	for {
		screen := buildScreen()
		if err := disp.DrawText(screen); err != nil {
			log.Printf("DrawText error: %v", err)
		} else {
			log.Printf("Screen updated:\n%s", screen)
		}
		time.Sleep(*refreshInterval)
	}
}

// monitorInternetConnectivity checks internet connectivity in the background
// with adaptive intervals: 30s when connected, 5s when disconnected.
func monitorInternetConnectivity() {
	checkConnectivity := func() bool {
		client := &http.Client{
			Timeout: 3 * time.Second,
		}
		resp, err := client.Head("http://clients3.google.com/generate_204")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK
	}

	for {
		isConnected := checkConnectivity()

		internetStatusMutex.Lock()
		if isConnected {
			internetConnected = "Connected"
		} else {
			internetConnected = "No Internet"
		}
		internetStatusMutex.Unlock()

		// Use adaptive interval based on connection state
		if isConnected {
			time.Sleep(*internetCheckInterval) // 30s when connected
		} else {
			time.Sleep(*internetCheckIntervalDisconnected) // 5s when disconnected
		}
	}
}

// buildScreen gathers system info and formats it into an 8-row × 16-col string.
func buildScreen() string {
	hostname := getHostname()
	cpuPct := getCPUPercent()
	ramPct := getRAMPercent()
	ips := getLocalIPs()
	internet := getInternetStatus()

	var sb strings.Builder

	// Row 0: hostname (truncated to 16 chars)
	sb.WriteString(truncate(hostname, 16))
	sb.WriteByte('\n')

	// Row 1: separator
	sb.WriteString("-----------------")
	sb.WriteByte('\n')

	// Row 2: CPU
	sb.WriteString(truncate(fmt.Sprintf("CPU: %.1f%%", cpuPct), 16))
	sb.WriteByte('\n')

	// Row 3: RAM
	sb.WriteString(truncate(fmt.Sprintf("RAM: %.1f%%", ramPct), 16))
	sb.WriteByte('\n')

	// Row 4: "Network:"
	sb.WriteString("Network:")
	sb.WriteByte('\n')

	// Rows 5-6: up to 2 IP addresses
	for i := 0; i < 2; i++ {
		if i < len(ips) {
			sb.WriteString(truncate(ips[i], 16))
		}
		sb.WriteByte('\n')
	}

	// Row 7: internet status
	connState := truncate(internet, 16)
	if len(connState) < 16 {
		connState += strings.Repeat(" ", 16-len(connState))
	}
	// Update the last characters to - and | for different frames
	if frameToggle {
		connState = connState[:15] + "-"
	} else {
		connState = connState[:15] + "|"
	}
	frameToggle = !frameToggle
	sb.WriteString(connState)
	sb.WriteByte('\n')

	return sb.String()
}

// getHostname returns the machine hostname, or "unknown" on error.
func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// getCPUPercent and getRAMPercent are implemented in platform-specific files:
// - sysinfo_linux.go (for Linux)
// - sysinfo_windows.go (for Windows)

// getLocalIPs returns up to 2 IPv4 addresses to display.
// Preference is given to addresses in the 192.168.x.x range (sorted ascending).
// If fewer than 2 such addresses exist the remaining slots are filled with
// other unicast addresses. If there are more than 2 192.168 addresses only
// those are returned (sorted), limited to 2.
func getLocalIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var preferred []string // 192.168.*
	var others []string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			ipStr := ip4.String()
			if strings.HasPrefix(ipStr, "192.168.") {
				preferred = append(preferred, ipStr)
			} else {
				others = append(others, ipStr)
			}
		}
	}

	// Sort 192.168 addresses
	sort.Slice(preferred, func(i, j int) bool {
		return ipLess(preferred[i], preferred[j])
	})

	combined := append(preferred, others...)
	if len(combined) > 2 {
		combined = combined[:2]
	}
	return combined
}

// ipLess compares two dotted-decimal IPv4 strings numerically.
func ipLess(a, b string) bool {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 4 && i < len(pa) && i < len(pb); i++ {
		na, _ := strconv.Atoi(pa[i])
		nb, _ := strconv.Atoi(pb[i])
		if na != nb {
			return na < nb
		}
	}
	return false
}

// getInternetStatus returns the cached internet connection status.
// The actual connectivity check happens in the background goroutine.
func getInternetStatus() string {
	internetStatusMutex.Lock()
	defer internetStatusMutex.Unlock()
	return internetConnected
}

// truncate shortens s to at most n runes.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
