package dezkvm

import (
	"errors"
	"log"

	"github.com/google/uuid"
	"imuslab.com/dezkvm/dezkvmd/mod/kvmaux"
	"imuslab.com/dezkvm/dezkvmd/mod/kvmhid"
	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
)

func (i *UsbKvmDeviceInstance) UUID() string {
	return i.uuid
}

func (i *UsbKvmDeviceInstance) Start() error {
	if i.Config.USBKVMDevicePath == "" {
		return errors.New("USB KVM device path is not specified")
	}
	if i.Config.USBKVMBaudrate == 0 {
		//Use default baudrate if not specified
		i.Config.USBKVMBaudrate = 115200
	}

	/* --------- Start HID Controller --------- */
	usbKVM := kvmhid.NewHIDController(&kvmhid.Config{
		PortName:          i.Config.USBKVMDevicePath,
		BaudRate:          i.Config.USBKVMBaudrate,
		ScrollSensitivity: 0x01, // Set mouse scroll sensitivity
	})

	//Start the HID controller
	err := usbKVM.Connect()
	if err != nil {
		return err
	}

	i.usbKVMController = usbKVM

	/* --------- Start AuxMCU Controller --------- */
	//Check if AuxMCU is configured, if so, start the connection
	if i.Config.AuxMCUDevicePath != "" {
		if i.Config.AuxMCUBaudrate == 0 {
			//Use default baudrate if not specified
			i.Config.AuxMCUBaudrate = 115200
		}

		auxMCU, err := kvmaux.NewAuxOutbandController(i.Config.AuxMCUDevicePath, i.Config.AuxMCUBaudrate)
		if err != nil {
			return err
		}
		i.auxMCUController = auxMCU

		//Try to get the UUID from the AuxMCU
		uuid, err := auxMCU.GetUUID()
		if err != nil {
			return err
		}
		i.uuid = uuid

	} else {
		// Randomly generate a UUIDv4 if AuxMCU is not present
		uuid, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		i.uuid = uuid.String()
		i.uuid = "10" + i.uuid[2:] //USB KVM device category is "1", type is "0" (unknown/unspecified)
	}

	/* --------- Start USB Capture Device --------- */
	usbCaptureDevice, err := usbcapture.NewInstance(i.captureConfig)
	if err != nil {
		return err
	}

	err = usbCaptureDevice.StartVideoCapture(i.videoResoltuionConfig)
	if err != nil {
		usbCaptureDevice.Close()
		return err
	}
	i.usbCaptureDevice = usbCaptureDevice

	/* --------- Load Preferences --------- */
	if i.parent != nil {
		prefs, err := i.parent.LoadPreferences(i.uuid)
		if err != nil {
			log.Printf("Warning: failed to load preferences for %s: %v\n", i.uuid, err)
		}
		if prefs != nil {
			i.Preferences = prefs
		}
	}
	if i.Preferences == nil {
		i.Preferences = DefaultPreferences()
	}
	i.ApplyPreferences()

	// All components started successfully — turn off the status LED
	if i.auxMCUController != nil {
		_ = i.auxMCUController.SetStatusLED(kvmaux.StatusLEDOff)
	}
	return nil
}

func (i *UsbKvmDeviceInstance) Stop() error {
	if i.usbKVMController != nil {
		i.usbKVMController.Close()
		i.usbKVMController = nil
	}
	if i.auxMCUController != nil {
		i.auxMCUController.Close()
		i.auxMCUController = nil
	}
	if i.usbCaptureDevice != nil {
		i.usbCaptureDevice.Close()
		i.usbCaptureDevice = nil
	}
	return nil
}

// Remove removes the USB KVM device instance from its parent DezkVM manager.
func (i *UsbKvmDeviceInstance) Remove() error {
	return i.parent.RemoveUsbKvmDevice(i.UUID())
}

func (i *UsbKvmDeviceInstance) SetLEDStatus(status kvmaux.StatusLEDPattern) error {
	if i.auxMCUController == nil {
		return errors.New("AuxMCU controller is not initialized")
	}
	return i.auxMCUController.SetStatusLED(status)
}

// ApplyPreferences applies the current preferences to the running HID controller.
func (i *UsbKvmDeviceInstance) ApplyPreferences() {
	if i.Preferences == nil || i.usbKVMController == nil {
		return
	}
	// Scroll sensitivity
	sens := i.Preferences.ScrollSensitivity
	if sens == 0 {
		sens = 1
	}
	i.usbKVMController.Config.ScrollSensitivity = sens

	// Invert scroll direction
	i.usbKVMController.Config.InvertScrollDirection = i.Preferences.InvertScrollDirection

	// Mouse jiggler
	if i.Preferences.EnableMouseJiggler {
		i.usbKVMController.StartMouseJiggler()
	} else {
		i.usbKVMController.StopMouseJiggler()
	}
}
