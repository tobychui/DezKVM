package dezukvm

import (
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"imuslab.com/dezukvm/dezukvmd/mod/kvmaux"
	"imuslab.com/dezukvm/dezukvmd/mod/usbcapture"
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
	USBKVMDevicePath   string   // e.g. /dev/ttyUSB0
	AuxMCUDevicePath   string   // e.g. /dev/ttyACM0
	CaptureDevicePaths []string // e.g. /dev/video0, /dev/video1, etc.
	AlsaDevicePaths    []string // e.g. /dev/snd/pcmC1D0c, etc.
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

// populateUsbKvmUUID tries to get the UUID from the AuxMCU device
func populateUsbKvmUUID(dev *UsbKvmDevice) error {
	if dev.AuxMCUDevicePath == "" {
		return nil
	}

	// The standard baudrate for AuxMCU is 115200
	aux, err := kvmaux.NewAuxOutbandController(dev.AuxMCUDevicePath, 115200)
	if err != nil {
		return err
	}
	defer aux.Close()

	uuid, err := aux.GetUUID()
	if err != nil {
		return err
	}

	dev.UUID = uuid
	return nil
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

	for _, t := range ttys {
		hub := getHub(t.sysPath)
		if hub != "" {
			if hubs[hub] == nil {
				hubs[hub] = &hubGroup{}
			}
			if strings.Contains(t.path, "ACM") {
				hubs[hub].acms = append(hubs[hub].acms, t.path)
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

	// Populate UUIDs
	for _, dev := range result {
		err := populateUsbKvmUUID(dev)
		if err != nil {
			log.Printf("Warning: could not get UUID for AuxMCU %s: %v, is this a third party device?", dev.AuxMCUDevicePath, err)
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
