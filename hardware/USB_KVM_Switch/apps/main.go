package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"imuslab.com/dezkvm/switchapp/internal/icon"
	"imuslab.com/dezkvm/switchapp/internal/kvmswitch"
)

const maxPortSlots = 20
const settingFile = "settings.json"

var (
	mu                sync.Mutex
	currentDev        *kvmswitch.Device
	currentPorts      []string
	lastConnectedPort string // port the user last successfully connected to
	watchingForReturn bool   // true while the port-watcher goroutine is running
)

// Global menu item references — assigned in onReady before any goroutine uses them.
var (
	mSelectPort   *systray.MenuItem
	mUUID         *systray.MenuItem
	mSide         *systray.MenuItem
	mSwitchPC1    *systray.MenuItem
	mSwitchPC2    *systray.MenuItem
	mSwitchToggle *systray.MenuItem
	portSlots     []*systray.MenuItem
)

type Settings struct {
	LastPort string `json:"last_port"`
}

func main() {
	systray.Run(onReady, onExit)
}

func getDefaultSettings() *Settings {
	return &Settings{
		LastPort: "",
	}
}

func saveSettingsToFile() {
	mu.Lock()
	setting := &Settings{
		LastPort: lastConnectedPort,
	}
	mu.Unlock()
	settingData, err := json.MarshalIndent(setting, "", "  ")
	if err != nil {
		log.Printf("marshal settings failed: %v", err)
		return
	}
	err = os.WriteFile(settingFile, settingData, 0644)
	if err != nil {
		log.Printf("write settings file failed: %v", err)
	}
	log.Printf("settings saved to %s", settingFile)
}

func loadSettingsFromFile() {
	setting := getDefaultSettings()
	settingFileContent, err := os.ReadFile(settingFile)
	if err == nil {
		err = json.Unmarshal(settingFileContent, &setting)
		if err != nil {
			log.Printf("unmarshal settings file failed: %v", err)
			log.Println("continuing with default settings")
		}
	}

	lastConnectedPort = setting.LastPort
	if lastConnectedPort != "" {
		// Start the port watcher to automatically reconnect when the port appears.
		go func() {
			// Wait a moment for the UI to initialize before starting the watcher.
			time.Sleep(1 * time.Second)
			mu.Lock()
			watchingForReturn = false // ensure only one watcher runs at a time
			mu.Unlock()
			startPortWatcher()
		}()
	}
}

// refreshDeviceUI updates menu items to reflect the current device state.
func refreshDeviceUI() {
	mu.Lock()
	dev := currentDev
	mu.Unlock()

	if dev == nil {
		mUUID.SetTitle("UUID: (not connected)")
		mSide.SetTitle("Current Side: —")
		mSwitchPC1.Hide()
		mSwitchPC2.Hide()
		mSwitchToggle.Hide()
		return
	}

	mUUID.SetTitle("UUID: " + dev.DeviceUUID)
	sideLabel := "PC1"
	if dev.CurrentSide == 1 {
		sideLabel = "PC2"
	}
	mSide.SetTitle("Current Side: " + sideLabel)

	// DeviceType == 1 means toggle-style: the MCU itself moves to the
	// remote machine on switch, so only a single toggle action is offered.
	if dev.DeviceType == 1 {
		mSwitchPC1.Hide()
		mSwitchPC2.Hide()
		mSwitchToggle.Show()
	} else {
		mSwitchToggle.Hide()
		mSwitchPC1.Show()
		mSwitchPC2.Show()
	}
}

// connectToPort closes any existing connection and opens portName.
func connectToPort(portName string) error {
	mu.Lock()
	if currentDev != nil {
		currentDev.Close()
		currentDev = nil
	}
	mu.Unlock()

	mSelectPort.SetTitle(fmt.Sprintf("Select Port: %s (connecting…)", portName))
	refreshDeviceUI()

	dev, err := kvmswitch.NewKVMSwitchDevice(portName, 115200)
	if err != nil {
		log.Printf("connect %s: %v", portName, err)
		mSelectPort.SetTitle(fmt.Sprintf("Select Port: %s (retrying…)", portName))
		return err
	}

	mu.Lock()
	currentDev = dev
	lastConnectedPort = portName
	mu.Unlock()

	mSelectPort.SetTitle("Select Port: " + portName)
	refreshDeviceUI()
	saveSettingsToFile()

	// Start the port watcher if not already running, so we can detect
	// disconnections from physical button presses on the KVM switch.
	startPortWatcher()

	return nil
}

// updatePorts rescans serial ports and updates the submenu.
func updatePorts() {
	ports := listPorts()
	mu.Lock()
	currentPorts = ports
	mu.Unlock()
	for i, slot := range portSlots {
		if i < len(ports) {
			slot.SetTitle(ports[i])
			slot.Show()
		} else {
			slot.Hide()
		}
	}
}

// startPortWatcher launches a background goroutine that continuously monitors
// the KVM switch connection. It:
// - Detects when the device is connected and updates UI status accordingly
// - Detects disconnection (e.g., physical button press on the switch)
// - Automatically reconnects when the port reappears
// Only one watcher runs at a time. It runs continuously as long as lastConnectedPort is set.
func startPortWatcher() {
	mu.Lock()
	if watchingForReturn {
		mu.Unlock()
		return
	}
	port := lastConnectedPort
	watchingForReturn = true
	mu.Unlock()

	if port == "" {
		mu.Lock()
		watchingForReturn = false
		mu.Unlock()
		return
	}

	go func() {
		defer func() {
			mu.Lock()
			watchingForReturn = false
			mu.Unlock()
		}()

		for {
			time.Sleep(2 * time.Second)

			mu.Lock()
			dev := currentDev
			mu.Unlock()

			if dev != nil {
				// Actively probe the device to detect physical disconnections
				// (e.g. user pressing the physical button on the KVM switch).
				// A plain IsConnected() check is insufficient because it only
				// tests whether the port struct is non-nil, not whether the OS
				// device node is still present.
				if err := dev.Probe(); err != nil {
					log.Printf("device probe failed – physical disconnect detected: %v", err)
					if closeErr := dev.Close(); closeErr != nil {
						log.Printf("close after probe failure: %v", closeErr)
					}
					mu.Lock()
					if currentDev == dev {
						currentDev = nil
					}
					mu.Unlock()
					updatePorts()
					refreshDeviceUI()
					mSelectPort.SetTitle(fmt.Sprintf("Select Port: %s (waiting…)", port))
					// Fall through to reconnection logic below
				} else {
					// Device is still alive; update side label in case it changed
					refreshDeviceUI()
					continue
				}
			}

			// Device is disconnected; check if port has reappeared
			for _, p := range listPorts() {
				if p == port {
					log.Printf("port %s reappeared – reconnecting automatically", port)
					err := connectToPort(port)
					if err != nil {
						log.Printf("reconnect %s: %v", port, err)
						// Continue retrying until it works, since the port is already there and likely to be functional soon.
						break
					}
					updatePorts()
					break
				}
			}
		}
	}()
}

func onReady() {
	systray.SetIcon(icon.Data)
	systray.SetTooltip("DezKVM Switch")

	// --- Port selection submenu ---
	mSelectPort = systray.AddMenuItem("Select Port: (none)", "Select a COM/TTY port to connect")
	portSlots = make([]*systray.MenuItem, maxPortSlots)
	for i := range portSlots {
		portSlots[i] = mSelectPort.AddSubMenuItem("", "")
		portSlots[i].Hide()
	}
	mRefreshPorts := mSelectPort.AddSubMenuItem("Refresh Ports", "Rescan available ports")

	systray.AddSeparator()

	// --- Device info (read-only display items) ---
	mUUID = systray.AddMenuItem("UUID: (not connected)", "Device UUID")
	mUUID.Disable()
	mSide = systray.AddMenuItem("Current Side: —", "Current active side")
	mSide.Disable()

	systray.AddSeparator()

	// --- Switch controls (shown/hidden based on device type) ---
	mSwitchPC1 = systray.AddMenuItem("Switch to PC1", "Switch KVM to PC 1")
	mSwitchPC2 = systray.AddMenuItem("Switch to PC2", "Switch KVM to PC 2")
	mSwitchToggle = systray.AddMenuItem("Switch PC", "Toggle KVM to the other PC")
	mSwitchPC1.Hide()
	mSwitchPC2.Hide()
	mSwitchToggle.Hide()

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit DezKVM Switch")

	// Initial port scan.
	updatePorts()

	// Port slot clicks.
	for i := 0; i < maxPortSlots; i++ {
		idx := i
		go func() {
			for range portSlots[idx].ClickedCh {
				mu.Lock()
				ports := make([]string, len(currentPorts))
				copy(ports, currentPorts)
				mu.Unlock()
				if idx < len(ports) {
					connectToPort(ports[idx])
				}
			}
		}()
	}

	// Refresh ports.
	go func() {
		for range mRefreshPorts.ClickedCh {
			updatePorts()
		}
	}()

	// Switch to PC1.
	go func() {
		for range mSwitchPC1.ClickedCh {
			mu.Lock()
			dev := currentDev
			mu.Unlock()
			if dev == nil {
				continue
			}
			if err := dev.SwitchToPC1(); err != nil {
				log.Printf("SwitchToPC1: %v", err)
			}
			time.Sleep(100 * time.Millisecond) // brief pause to allow device state to update
			if !dev.IsConnected() {
				mu.Lock()
				if currentDev == dev {
					currentDev = nil
				}
				mu.Unlock()
				updatePorts()
				mSelectPort.SetTitle(fmt.Sprintf("Select Port: detached (waiting…)"))
			}
			refreshDeviceUI()
		}
	}()

	// Switch to PC2.
	go func() {
		for range mSwitchPC2.ClickedCh {
			mu.Lock()
			dev := currentDev
			mu.Unlock()
			if dev == nil {
				continue
			}
			if err := dev.SwitchToPC2(); err != nil {
				log.Printf("SwitchToPC2: %v", err)
			}
			time.Sleep(100 * time.Millisecond) // brief pause to allow device state to update
			if !dev.IsConnected() {
				mu.Lock()
				if currentDev == dev {
					currentDev = nil
				}
				mu.Unlock()
				updatePorts()
				mSelectPort.SetTitle(fmt.Sprintf("Select Port: detached (waiting…)"))
			}
			refreshDeviceUI()
		}
	}()

	// Toggle switch (devType == 1).
	go func() {
		for range mSwitchToggle.ClickedCh {
			mu.Lock()
			dev := currentDev
			mu.Unlock()
			if dev == nil {
				continue
			}
			// Use cached side — avoids a round-trip and works even if the
			// port closed after the previous switch.
			var switchErr error
			if dev.CurrentSide == 0 {
				switchErr = dev.SwitchToPC2()
			} else {
				switchErr = dev.SwitchToPC1()
			}
			if switchErr != nil {
				log.Printf("Switch: %v", switchErr)
			}
			// For DeviceType 1 the MCU physically switches USB to the other PC;
			// the serial port on this machine is now gone. The port watcher
			// will detect the disconnection and wait for reconnection.
			if !dev.IsConnected() {
				mu.Lock()
				if currentDev == dev {
					currentDev = nil
				}
				mu.Unlock()
				updatePorts()
				mSelectPort.SetTitle(fmt.Sprintf("Select Port: detached (waiting…)"))
			}
			refreshDeviceUI()
		}
	}()

	// Load saved settings — starts port watcher if a previous port is known.
	loadSettingsFromFile()

	// Quit.
	go func() {
		<-mQuit.ClickedCh
		mu.Lock()
		if currentDev != nil {
			currentDev.Close()
			currentDev = nil
		}
		mu.Unlock()
		systray.Quit()
	}()
}

func onExit() {
	mu.Lock()
	if currentDev != nil {
		currentDev.Close()
		currentDev = nil
	}
	mu.Unlock()
}
