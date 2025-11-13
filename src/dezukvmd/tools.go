package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"

	"imuslab.com/dezukvm/dezukvmd/mod/dezukvm"
	"imuslab.com/dezukvm/dezukvmd/mod/usbcapture"
)

func handle_debug_tool() error {
	switch *tool {
	case "dependency-precheck":
		err := run_dependency_precheck()
		if err != nil {
			return err
		}
	case "list-usbkvm-json":
		result, err := dezukvm.DiscoverUsbKvmSubtree()
		if err != nil {
			return err
		}
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(jsonData))
	case "audio-devices":
		err := list_all_audio_devices()
		if err != nil {
			return err
		}
	case "list-usbkvm":
		err := list_usb_kvm_devcies()
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("please specify a valid tool with -tool option")
	}
	return nil
}

// run_dependency_precheck checks if required dependencies are available in the system
func run_dependency_precheck() error {
	log.Println("Running precheck...")
	// Dependencies of USB capture card
	if _, err := exec.LookPath("v4l2-ctl"); err != nil {
		return fmt.Errorf("v4l2-ctl not found in PATH")
	}
	if _, err := exec.LookPath("arecord"); err != nil {
		return fmt.Errorf("arecord not found in PATH")
	}
	log.Println("v4l2-ctl and arecord found in PATH.")
	return nil
}

// list_usb_kvm_devcies lists all discovered USB KVM devices and their associated sub-devices
func list_usb_kvm_devcies() error {
	result, err := dezukvm.DiscoverUsbKvmSubtree()
	if err != nil {
		return err
	}
	for i, dev := range result {
		log.Printf("USB KVM Device Tree %d:\n", i)
		log.Printf(" - USB KVM Device: %s\n", dev.USBKVMDevicePath)
		log.Printf(" - Aux MCU Device: %s\n", dev.AuxMCUDevicePath)
		for _, cap := range dev.CaptureDevicePaths {
			log.Printf(" - Capture Device: %s\n", cap)
		}
		for _, snd := range dev.AlsaDevicePaths {
			log.Printf(" - ALSA Device: %s\n", snd)
		}
	}
	return nil
}

// list_all_audio_devices lists all available audio capture devices
func list_all_audio_devices() error {
	log.Println("Starting in List Audio Devices mode...")
	// Get the audio devices
	path, err := usbcapture.FindHDMICapturePCMPath()
	if err != nil {
		return err
	}
	log.Printf("Found HDMI capture PCM path: %s\n", path)
	// List all audio capture devices
	captureDevs, err := usbcapture.ListCaptureDevices()
	if err != nil {
		return err
	}
	log.Println("Available audio capture devices:")
	for _, dev := range captureDevs {
		log.Printf(" - %s\n", dev)
	}
	return nil
}
