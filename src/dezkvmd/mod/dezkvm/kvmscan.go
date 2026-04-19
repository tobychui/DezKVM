package dezkvm

import (
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
)

/*
Each of the USB-KVM device has the same set of USB devices
connected under a single USB hub chip. This function
will scan the USB device tree to find the connected
USB devices and match them to the configured device paths.

Commonly found devices are:
- USB hub (the main hub chip)
-- USB UART device (HID KVM)
-- USB CDC ACM device (auxiliary MCU)
-- USB Video Class device (webcam capture)
-- USB Audio Class device (audio capture)

The AuxMCU will provide a UUID to uniquely identify
the USB KVM device subtree.
*/
type UsbKvmDevice struct {
	UUID               string   // 16 bytes UUID obtained from AuxMCU, might change after power cycle
	IsReady            bool     // Whether the device is ready for use (serial port and video device opened successfully)
	USBKVMDevicePath   string   // e.g. /dev/ttyUSB0
	AuxMCUDevicePath   string   // e.g. /dev/ttyACM0
	CaptureDevicePaths []string // e.g. /dev/video0, /dev/video1, etc.
	AlsaDevicePaths    []string // e.g. /dev/snd/pcmC1D0c, etc.
}

// ScannedTTYDevice represents a TTY serial device discovered during scanning,
// along with its sniffed device category and type.
type ScannedTTYDevice struct {
	DevicePath string          `json:"device_path"`
	Category   DeviceCatergory `json:"category"`
	Type       DeviceType      `json:"type"`
	UUID       string          `json:"uuid"` // Full UUID obtained during sniffing
}

var (
	scannedTTYDevices []*ScannedTTYDevice
	scannedTTYMu      sync.RWMutex
)

// GetScannedTTYDevices returns all TTY devices discovered during the last scan,
// along with their category and type. Other modules can use this to find
// specific device types (e.g. OLED displays) without rescanning.
func GetScannedTTYDevices() []*ScannedTTYDevice {
	scannedTTYMu.RLock()
	defer scannedTTYMu.RUnlock()
	copy := make([]*ScannedTTYDevice, len(scannedTTYDevices))
	for i, d := range scannedTTYDevices {
		cloned := *d
		copy[i] = &cloned
	}
	return copy
}

// GetScannedTTYDevicesByCategory returns all scanned TTY devices matching
// the given category. For example, pass DeviceCatergoryDisplay to get
// all OLED display devices.
func GetScannedTTYDevicesByCategory(cat DeviceCatergory) []*ScannedTTYDevice {
	scannedTTYMu.RLock()
	defer scannedTTYMu.RUnlock()
	var result []*ScannedTTYDevice
	for _, d := range scannedTTYDevices {
		if d.Category == cat {
			cloned := *d
			result = append(result, &cloned)
		}
	}
	return result
}

// ScanConnectedUsbKvmDevices scans and lists all connected USB KVM devices in the system.
func ScanConnectedUsbKvmDevices() ([]*UsbKvmDeviceOption, error) {
	possibleKvmDeviceGroup, err := DiscoverUsbKvmSubtree()
	if err != nil {
		return nil, err
	}

	if len(possibleKvmDeviceGroup) == 0 {
		return nil, errors.New("no USB KVM devices found")
	}

	result := []*UsbKvmDeviceOption{}
	for _, dev := range possibleKvmDeviceGroup {
		option := &UsbKvmDeviceOption{
			USBKVMDevicePath:       dev.USBKVMDevicePath,
			AuxMCUDevicePath:       dev.AuxMCUDevicePath,
			VideoCaptureDevicePath: "",
			AudioCaptureDevicePath: "",
		}
		for _, videoPath := range dev.CaptureDevicePaths {
			isCaptureCard := usbcapture.IsCaptureCardVideoInterface(videoPath)
			if isCaptureCard {
				option.VideoCaptureDevicePath = videoPath
			}
		}

		// In theory one capture card shd only got 1 alsa audio device file
		if len(dev.AlsaDevicePaths) > 0 {
			option.AudioCaptureDevicePath = dev.AlsaDevicePaths[0] // Use the first audio device by default
		}
		result = append(result, option)
	}
	return result, nil
}

func DiscoverUsbKvmSubtree() ([]*UsbKvmDevice, error) {
	// Scan all /dev/tty*, /dev/video*, /dev/snd/pcmC* devices
	getMatchingDevs := func(pattern string) ([]string, error) {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		return files, nil
	}

	// Get all ttyUSB*, ttyACM*
	ttyDevs1, _ := getMatchingDevs("/dev/ttyUSB*")
	ttyDevs2, _ := getMatchingDevs("/dev/ttyACM*")
	ttyDevs := append(ttyDevs1, ttyDevs2...)

	// Get all video*
	videoDevs, _ := getMatchingDevs("/dev/video*")

	// Get all ALSA PCM devices (USB audio is usually card > 0)
	alsaDevs, _ := getMatchingDevs("/dev/snd/pcmC*")

	type devInfo struct {
		path    string
		sysPath string
	}

	getSys := func(devs []string) []devInfo {
		var out []devInfo
		for _, d := range devs {
			sys, err := getDeviceFullPath(d)
			if err == nil {
				out = append(out, devInfo{d, sys})
			}
		}
		return out
	}

	ttys := getSys(ttyDevs)
	videos := getSys(videoDevs)
	alsas := getSys(alsaDevs)

	// Find common USB root hub prefix
	hubPattern := regexp.MustCompile(`^\d+-\d+(\.\d+)*$`)
	getHub := func(sys string) string {
		parts := strings.Split(sys, "/")
		for i := range parts {
			// Look for USB hub pattern (e.g. 1-2, 2-1, etc.)
			if hubPattern.MatchString(parts[i]) {
				return strings.Join(parts[:i+1], "/")
			}
		}
		return ""
	}

	// Map hub -> device info
	type hubGroup struct {
		ttys   []string
		acms   []string
		videos []string
		alsas  []string
	}
	hubs := make(map[string]*hubGroup)

	// Sniff all ACM devices to determine their category and type,
	// and populate the scanned TTY device cache
	var newScannedDevices []*ScannedTTYDevice
	acmCategories := make(map[string]*ScannedTTYDevice) // path -> sniffed info
	for _, t := range ttys {
		if strings.Contains(t.path, "ACM") {
			cat, devType, uuid, err := sniffDeviceType(t.path)
			if err != nil {
				log.Printf("Warning: could not sniff device type for %s: %v", t.path, err)
			} else {
				sniffed := &ScannedTTYDevice{
					DevicePath: t.path,
					Category:   cat,
					Type:       devType,
					UUID:       uuid,
				}
				newScannedDevices = append(newScannedDevices, sniffed)
				acmCategories[t.path] = sniffed
			}
		}
	}

	// Update the global scanned device cache
	scannedTTYMu.Lock()
	scannedTTYDevices = newScannedDevices
	scannedTTYMu.Unlock()

	for _, t := range ttys {
		hub := getHub(t.sysPath)
		if hub != "" {
			if hubs[hub] == nil {
				hubs[hub] = &hubGroup{}
			}
			if strings.Contains(t.path, "ACM") {
				// Only include ACM devices that are identified as KVM port category
				if sniffed, ok := acmCategories[t.path]; ok && sniffed.Category == DeviceCatergoryKVMPort {
					hubs[hub].acms = append(hubs[hub].acms, t.path)
				} else {
					log.Printf("Skipping non-KVM ACM device %s (category: %v)", t.path, acmCategories[t.path])
				}
			} else {
				hubs[hub].ttys = append(hubs[hub].ttys, t.path)
			}
		}
	}
	for _, v := range videos {
		hub := getHub(v.sysPath)
		if hub != "" {
			if hubs[hub] == nil {
				hubs[hub] = &hubGroup{}
			}
			hubs[hub].videos = append(hubs[hub].videos, v.path)
		}
	}
	for _, alsa := range alsas {
		hub := getHub(alsa.sysPath)
		if hub != "" {
			if hubs[hub] == nil {
				hubs[hub] = &hubGroup{}
			}
			hubs[hub].alsas = append(hubs[hub].alsas, alsa.path)
		}
	}

	var result []*UsbKvmDevice
	for _, g := range hubs {
		// At least one tty or acm, one video, optionally alsa
		if (len(g.ttys) > 0 || len(g.acms) > 0) && len(g.videos) > 0 {
			// Pick the first tty as USBKVMDevicePath, first acm as AuxMCUDevicePath
			usbKvm := ""
			auxMcu := ""
			if len(g.ttys) > 0 {
				usbKvm = g.ttys[0]
			}
			if len(g.acms) > 0 {
				auxMcu = g.acms[0]
			}
			result = append(result, &UsbKvmDevice{
				USBKVMDevicePath:   usbKvm,
				AuxMCUDevicePath:   auxMcu,
				CaptureDevicePaths: g.videos,
				AlsaDevicePaths:    g.alsas,
			})
		}
	}

	// Populate UUIDs from sniffed data (avoids reopening serial ports)
	for _, dev := range result {
		if dev.AuxMCUDevicePath != "" {
			if sniffed, ok := acmCategories[dev.AuxMCUDevicePath]; ok && sniffed.UUID != "" {
				dev.UUID = sniffed.UUID
				dev.IsReady = true
			} else {
				log.Printf("Warning: no UUID available for AuxMCU %s, is this a third party device?", dev.AuxMCUDevicePath)
			}
		}
	}

	if len(result) == 0 {
		return nil, errors.New("no USB KVM device found")
	}
	return result, nil
}

func resolveSymlink(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func getDeviceFullPath(devicePath string) (string, error) {
	resolvedPath, err := resolveSymlink(devicePath)
	if err != nil {
		return "", err
	}

	// Use udevadm to get the device chain
	out, err := exec.Command("udevadm", "info", "-q", "path", "-n", resolvedPath).Output()
	if err != nil {
		return "", err
	}
	sysPath := strings.TrimSpace(string(out))
	if sysPath == "" {
		return "", errors.New("could not resolve sysfs path")
	}

	fullPath := "/sys" + sysPath
	return fullPath, nil
}
