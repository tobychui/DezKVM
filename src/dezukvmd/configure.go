package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"imuslab.com/dezukvm/dezukvmd/mod/kvmhid"
)

type UsbKvmConfig struct {
	ListeningAddress        string
	USBKVMDevicePath        string
	AuxMCUDevicePath        string
	VideoCaptureDevicePath  string
	AudioCaptureDevicePath  string
	CaptureResolutionWidth  int
	CaptureResolutionHeight int
	CaptureResolutionFPS    int
	USBKVMBaudrate          int
	AuxMCUBaudrate          int
}

var (
	/* Internal variables for USB-KVM mode only */
	usbKVM              *kvmhid.Controller
	defaultUsbKvmConfig = &UsbKvmConfig{
		ListeningAddress:        ":9000",
		USBKVMDevicePath:        "/dev/ttyUSB0",
		AuxMCUDevicePath:        "/dev/ttyACM0",
		VideoCaptureDevicePath:  "/dev/video0",
		AudioCaptureDevicePath:  "/dev/snd/pcmC1D0c",
		CaptureResolutionWidth:  1920,
		CaptureResolutionHeight: 1080,
		CaptureResolutionFPS:    25,
		USBKVMBaudrate:          115200,
		AuxMCUBaudrate:          115200,
	}
)

func loadUsbKvmConfig() (*UsbKvmConfig, error) {
	if _, err := os.Stat(USB_KVM_CFG_PATH); os.IsNotExist(err) {
		file, err := os.OpenFile(USB_KVM_CFG_PATH, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
		if err != nil {
			return nil, err
		}

		// Save default config as JSON
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		if err := enc.Encode(defaultUsbKvmConfig); err != nil {
			file.Close()
			return nil, err
		}
		file.Close()
		return defaultUsbKvmConfig, nil
	}

	// Load config from file
	file, err := os.Open(USB_KVM_CFG_PATH)
	if err != nil {
		return nil, err
	}

	cfg := &UsbKvmConfig{}
	dec := json.NewDecoder(file)
	if err := dec.Decode(cfg); err != nil {
		file.Close()
		return nil, err
	}
	file.Close()
	return cfg, nil
}

func SetupHIDCommunication(config *UsbKvmConfig) error {
	// Initiate the HID controller
	usbKVM = kvmhid.NewHIDController(&kvmhid.Config{
		PortName:          config.USBKVMDevicePath,
		BaudRate:          config.USBKVMBaudrate,
		ScrollSensitivity: 0x01, // Set mouse scroll sensitivity
	})

	//Start the HID controller
	err := usbKVM.Connect()
	if err != nil {
		log.Fatal(err)
	}

	time.Sleep(1 * time.Second) // Wait for the controller to initialize
	log.Println("Updating chip baudrate to 115200...")
	//Configure the HID controller
	err = usbKVM.ConfigureChipTo115200()
	if err != nil {
		log.Fatalf("Failed to configure chip baudrate: %v", err)
		return err
	}
	time.Sleep(1 * time.Second)

	log.Println("Setting chip USB device properties...")
	time.Sleep(2 * time.Second) // Wait for the controller to initialize
	_, err = usbKVM.WriteChipProperties()
	if err != nil {
		log.Fatalf("Failed to write chip properties: %v", err)
		return err
	}

	log.Println("Configuration command sent. Unplug the device and plug it back in to apply the changes.")
	return nil
}
