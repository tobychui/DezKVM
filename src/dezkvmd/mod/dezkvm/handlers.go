package dezkvm

import (
	"encoding/json"
	"net/http"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/kvmaux"
	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
)

func (d *DezkVM) HandleVideoStreams(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	// Serve the video stream
	targetInstance.usbCaptureDevice.ServeVideoStream(w, r)
}

func (d *DezkVM) HandleAudioStreams(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	pcmDevicePath := targetInstance.captureConfig.AudioDeviceName
	targetInstance.usbCaptureDevice.AudioStreamingHandler(w, r, pcmDevicePath)
}

func (d *DezkVM) HandleHIDEvents(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	// Set status LED to blinking pattern while connection is active
	if targetInstance.auxMCUController != nil {
		_ = targetInstance.auxMCUController.SetStatusLED(kvmaux.StatusLEDOn)
	}
	targetInstance.usbKVMController.HIDWebSocketHandler(w, r)
	if targetInstance.auxMCUController != nil {
		// Set status LED back to solid on after connection ends
		_ = targetInstance.auxMCUController.SetStatusLED(kvmaux.StatusLEDOff)
	}
}

// HandleMassStorageSideSwitch handles the request to switch the USB mass storage side.
// there is only two state for the USB mass storage side, KVM side or Remote side.
// isKvmSide = true means switch to KVM side, otherwise switch to Remote side.
func (d *DezkVM) HandleMassStorageSideSwitch(w http.ResponseWriter, r *http.Request, instanceUuid string, isKvmSide bool) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	if targetInstance.auxMCUController == nil {
		http.Error(w, "Auxiliary MCU controller not initialized or missing", http.StatusInternalServerError)
		return
	}
	if isKvmSide {
		err = targetInstance.auxMCUController.SwitchUSBToKVM()
	} else {
		err = targetInstance.auxMCUController.SwitchUSBToRemote()
	}
	if err != nil {
		http.Error(w, "Failed to switch USB mass storage side: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (d *DezkVM) HandleListInstances(w http.ResponseWriter, r *http.Request) {
	instances := []map[string]interface{}{}
	for _, instance := range d.UsbKvmInstance {
		instances = append(instances, map[string]interface{}{
			"uuid":                    instance.UUID(),
			"video_capture_dev":       instance.Config.VideoCaptureDevicePath,
			"audio_capture_dev":       instance.Config.AudioCaptureDevicePath,
			"video_resolution_width":  instance.Config.CaptureVideoResolutionWidth,
			"video_resolution_height": instance.Config.CaptureeVideoResolutionHeight,
			"video_framerate":         instance.Config.CaptureeVideoFPS,
			"audio_sample_rate":       instance.Config.CaptureAudioSampleRate,
			"audio_channels":          instance.Config.CaptureAudioChannels,
			"stream_info":             instance.usbCaptureDevice.GetStreamInfo(),
			"usb_kvm_device":          instance.Config.USBKVMDevicePath,
			"aux_mcu_device":          instance.Config.AuxMCUDevicePath,
			"usb_mass_storage_side":   instance.auxMCUController.GetUSBMassStorageSide(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(instances)
}

// HandleGetSupportedResolutions returns the supported resolutions for a given USB KVM device instance
func (d *DezkVM) HandleGetSupportedResolutions(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	// Get the supported resolutions from the capture device
	supportedResolutions := targetInstance.usbCaptureDevice.GetSupportedResolutions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(supportedResolutions)
}

// HandleGetCurrentResolution returns the current resolution for a given USB KVM device instance
func (d *DezkVM) HandleGetCurrentResolution(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	// Get the current resolution from the instance config
	currentResolution := targetInstance.videoResoltuionConfig
	if currentResolution == nil {
		http.Error(w, "Current resolution not set", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentResolution)
}

// HandleChangeResolution handles the request to change the capture device resolution
func (d *DezkVM) HandleChangeResolution(w http.ResponseWriter, r *http.Request, instanceUuid string, newResolution *usbcapture.CaptureResolution) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	// Change the resolution
	err = targetInstance.usbCaptureDevice.ChangeResolution(newResolution)
	if err != nil {
		http.Error(w, "Failed to change resolution: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update the instance config
	targetInstance.videoResoltuionConfig = newResolution
	targetInstance.Config.CaptureVideoResolutionWidth = newResolution.Width
	targetInstance.Config.CaptureeVideoResolutionHeight = newResolution.Height
	targetInstance.Config.CaptureeVideoFPS = newResolution.FPS

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Resolution changed successfully. Please reconnect to the stream.",
	})
}

// HandleScreenshot handles the request to capture a screenshot from the video device
func (d *DezkVM) HandleScreenshot(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	// Serve the screenshot
	targetInstance.usbCaptureDevice.ServeScreenshot(w, r)
}

// HandleMouseJiggler toggles the mouse jiggler for a given instance.
// POST enables or disables based on the JSON body {"enabled": true/false}.
// GET returns the current state.
func (d *DezkVM) HandleMouseJiggler(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"enabled": targetInstance.usbKVMController.IsMouseJigglerEnabled(),
		})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Enabled {
		targetInstance.usbKVMController.StartMouseJiggler()
	} else {
		targetInstance.usbKVMController.StopMouseJiggler()
	}

	// Update preferences
	if targetInstance.Preferences == nil {
		targetInstance.Preferences = DefaultPreferences()
	}
	targetInstance.Preferences.EnableMouseJiggler = req.Enabled
	_ = d.SavePreferences(targetInstance)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{
		"enabled": req.Enabled,
	})
}

// HandleGetPreferences returns the current preferences for a given instance.
func (d *DezkVM) HandleGetPreferences(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	if targetInstance.Preferences == nil {
		targetInstance.Preferences = DefaultPreferences()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targetInstance.Preferences)
}

// HandleSetPreferences updates the preferences for a given instance and persists them to disk.
func (d *DezkVM) HandleSetPreferences(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	var prefs UsbKvmPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	targetInstance.Preferences = &prefs
	targetInstance.ApplyPreferences()

	// Persist to disk
	if err := d.SavePreferences(targetInstance); err != nil {
		http.Error(w, "Failed to save preferences: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targetInstance.Preferences)
}

// HandleReconnectCapture closes the V4L2 and audio devices and restarts them.
// The frontend should reload the page after this completes to re-establish streams.
func (d *DezkVM) HandleReconnectCapture(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	if targetInstance.usbCaptureDevice == nil {
		http.Error(w, "Capture device not initialized", http.StatusInternalServerError)
		return
	}

	// Close the existing capture device (stops video + audio)
	targetInstance.usbCaptureDevice.Close()
	targetInstance.usbCaptureDevice = nil

	time.Sleep(1 * time.Second) // Short delay to ensure device is released

	// Re-create and start the capture device
	newDevice, err := usbcapture.NewInstance(targetInstance.captureConfig)
	if err != nil {
		http.Error(w, "Failed to re-initialize capture device: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = newDevice.StartVideoCapture(targetInstance.videoResoltuionConfig)
	if err != nil {
		newDevice.Close()
		http.Error(w, "Failed to restart video capture: "+err.Error(), http.StatusInternalServerError)
		return
	}

	targetInstance.usbCaptureDevice = newDevice

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Capture device reconnected",
	})
}
