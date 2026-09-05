package dezkvm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/dezkvm/storage"
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

	// Never switch the USB mux while an image write is running: pulling the
	// disk away mid-write would corrupt it.
	if targetInstance.isoWriteJob != nil && targetInstance.isoWriteJob.IsRunning() {
		http.Error(w, "An image write is currently in progress on this device", http.StatusConflict)
		return
	}

	// Check if the current side is the same as the requested side, if so, return early
	currentSide := targetInstance.auxMCUController.GetUSBMassStorageSide()
	if (isKvmSide && currentSide == kvmaux.USB_MASS_STORAGE_KVM) || (!isKvmSide && currentSide == kvmaux.USB_MASS_STORAGE_REMOTE) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}

	// Switch the USB mass storage side using the AuxMCU controller
	if isKvmSide {
		// Take a snapshot of the current storage device list
		sdlist, _ := storage.GetStorageDeviceLists()
		ogListLength := len(sdlist)
		err = targetInstance.auxMCUController.SwitchUSBToKVM()
		if err != nil {
			http.Error(w, "Failed to switch USB mass storage to KVM side: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rescanCount := 0
		// After switching to KVM side, poll for the new storage device to appear (with a timeout)
		for {
			time.Sleep(1 * time.Second) // Wait before rescanning
			sdlist, _ = storage.GetStorageDeviceLists()
			if len(sdlist) > ogListLength {
				break // New device appeared, exit the loop
			}
			rescanCount++
			if rescanCount > 5 { // Timeout after 5 seconds
				log.Println("Timeout waiting for mass storage device to appear after switching to KVM side")
				http.Error(w, "Timeout waiting for mass storage device to appear after switching to KVM side", http.StatusInternalServerError)
				return
			}
		}

		// Attempt to mount the mass storage device
		mountPoint, err := targetInstance.MountMassStorage()
		if err != nil {
			log.Println("Failed to mount mass storage after switching to KVM side:", err)
			http.Error(w, "Failed to mount mass storage after switching to KVM side: "+err.Error(), http.StatusInternalServerError)
			return
		}
		targetInstance.massStorageMountPoint = mountPoint
	} else {
		// If switching to remote side, clear the mount point and unmount if necessary
		err := targetInstance.UnmountMassStorage()
		if err != nil {
			log.Println("Failed to unmount mass storage after switching to remote side:", err)
			http.Error(w, "Failed to unmount mass storage after switching to remote side: "+err.Error(), http.StatusInternalServerError)
			return
		}

		time.Sleep(2 * time.Second) // Short delay to ensure unmount completes
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
		storage_device_path, _ := instance.GetMassStoragePath()
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
			"storage_device":          storage_device_path,
			"storage_uuid":            instance.massStorageUUID,
			"storage_browsable":       instance.IsStorageBrowsable(),
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

	// A resolution change restarts the capture pipeline; drop any WebRTC
	// session so it does not hold the old stream.
	targetInstance.closeWebRTCSession()

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

	// Drop any WebRTC session before tearing the capture device down.
	targetInstance.closeWebRTCSession()

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

// ---------------------------------------------------------------------------
// Storage volume info
// ---------------------------------------------------------------------------

// HandleStorageVolumeInfo returns disk-usage statistics for the mass storage
// device of the given instance (total, used, free bytes + category breakdown),
// plus the disk model name and filesystem type.
func (d *DezkVM) HandleStorageVolumeInfo(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	mountPoint, err := targetInstance.GetMassStorageMountPoint()
	if err != nil {
		http.Error(w, "Mass storage not mounted: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	info, err := storage.GetVolumeInfo(mountPoint)
	if err != nil {
		http.Error(w, "Failed to get volume info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Enrich the response with disk model and filesystem type.
	diskModel := ""
	fsType := ""
	if devPath, devErr := targetInstance.GetMassStoragePath(); devErr == nil {
		if diskInfo, diskErr := storage.GetDiskInfoFromDevicePath(devPath); diskErr == nil {
			diskModel = diskInfo.Model
			if len(diskInfo.Partitions) > 0 {
				fsType = diskInfo.Partitions[0].FSType
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		*storage.VolumeInfo
		DiskModel string `json:"disk_model"`
		FSType    string `json:"fs_type"`
	}{
		VolumeInfo: info,
		DiskModel:  diskModel,
		FSType:     fsType,
	})
}

// ---------------------------------------------------------------------------
// File manager handlers
// ---------------------------------------------------------------------------

// getMountPointOrError is a shared helper that resolves the mount point for
// the instance and writes an HTTP error when it cannot be found.
func (d *DezkVM) getMountPointOrError(w http.ResponseWriter, instanceUuid string) (string, bool) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return "", false
	}
	mountPoint, err := targetInstance.GetMassStorageMountPoint()
	if err != nil {
		http.Error(w, "Mass storage not mounted: "+err.Error(), http.StatusServiceUnavailable)
		return "", false
	}
	return mountPoint, true
}

// HandleFileManagerListDir lists the directory contents at the path given by
// the "path" query parameter (defaults to "/").
func (d *DezkVM) HandleFileManagerListDir(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		relPath = "/"
	}

	entries, err := storage.ListDir(mountPoint, relPath)
	if err != nil {
		http.Error(w, "Failed to list directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// HandleFileManagerCreate creates a file or directory. The JSON body must
// contain "path" (relative path) and "type" ("file" or "dir").
func (d *DezkVM) HandleFileManagerCreate(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	var req struct {
		Path string `json:"path"`
		Type string `json:"type"` // "file" or "dir"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "Missing path", http.StatusBadRequest)
		return
	}

	var err error
	switch req.Type {
	case "dir":
		err = storage.CreateDir(mountPoint, req.Path)
	default: // "file" or empty
		err = storage.CreateFile(mountPoint, req.Path)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create %s: %s", req.Type, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleFileManagerDelete deletes the file or directory at the "path" query
// parameter (recursive for directories).
func (d *DezkVM) HandleFileManagerDelete(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	if err := storage.Delete(mountPoint, relPath); err != nil {
		http.Error(w, "Failed to delete: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleFileManagerRename renames the entry at "path" to "new_name"
// (bare filename, no separators). Both are provided in the JSON body.
func (d *DezkVM) HandleFileManagerRename(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	var req struct {
		Path    string `json:"path"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Path == "" || req.NewName == "" {
		http.Error(w, "Missing path or new_name", http.StatusBadRequest)
		return
	}

	if err := storage.Rename(mountPoint, req.Path, req.NewName); err != nil {
		http.Error(w, "Failed to rename: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleFileManagerMove moves the entry at "src" to "dst". Both paths are
// relative and provided in the JSON body.
func (d *DezkVM) HandleFileManagerMove(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	var req struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Src == "" || req.Dst == "" {
		http.Error(w, "Missing src or dst", http.StatusBadRequest)
		return
	}

	if err := storage.Move(mountPoint, req.Src, req.Dst); err != nil {
		http.Error(w, "Failed to move: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleFileManagerCopy copies the entry at "src" to "dst". Both paths are
// relative and provided in the JSON body.
func (d *DezkVM) HandleFileManagerCopy(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	var req struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Src == "" || req.Dst == "" {
		http.Error(w, "Missing src or dst", http.StatusBadRequest)
		return
	}

	if err := storage.Copy(mountPoint, req.Src, req.Dst); err != nil {
		http.Error(w, "Failed to copy: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleFileManagerUpload handles a multipart file upload. The destination
// directory is taken from the "path" form field (defaults to "/"). Each file
// in the multipart form is written under that directory using its original name.
// Max upload size is 4 GB.
func (d *DezkVM) HandleFileManagerUpload(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	const maxUploadSize = 4 << 30 // 4 GB
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "Failed to parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}

	destDir := r.FormValue("path")
	if destDir == "" {
		destDir = "/"
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "No files provided", http.StatusBadRequest)
		return
	}

	uploaded := make([]string, 0, len(files))
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			http.Error(w, "Failed to open uploaded file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		relPath := filepath.Join(destDir, filepath.Base(fh.Filename))
		if err := storage.UploadFile(mountPoint, relPath, f); err != nil {
			f.Close()
			http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		f.Close()
		uploaded = append(uploaded, fh.Filename)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"uploaded": uploaded,
	})
}

// HandleFileManagerDownload serves the file at the "path" query parameter
// as an HTTP download (Content-Disposition: attachment).
func (d *DezkVM) HandleFileManagerDownload(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	mountPoint, ok := d.getMountPointOrError(w, instanceUuid)
	if !ok {
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	if err := storage.ServeFile(mountPoint, relPath, w, r); err != nil {
		http.Error(w, "Failed to serve file: "+err.Error(), http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// Disk format handlers
// ---------------------------------------------------------------------------

// formatRequestToSpecs converts a format API request body to a partition-spec
// list.  A request either carries an explicit "partitions" array or the legacy
// single-partition "filesystem"/"label" fields.
func formatRequestToSpecs(filesystem, label string, partitions []storage.PartitionSpec) ([]storage.PartitionSpec, error) {
	if len(partitions) > 0 {
		return partitions, nil
	}
	if filesystem == "" {
		return nil, fmt.Errorf("missing required field: filesystem or partitions")
	}
	return []storage.PartitionSpec{{Filesystem: filesystem, Label: label}}, nil
}

// HandleStorageFormatPreview returns a JSON description of the partition layout
// that would result from formatting the mass storage device.  No data is
// written to disk.
//
//	GET  – legacy single-partition preview; query parameters: filesystem
//	       (required), label (optional).
//	POST – multi-partition preview; JSON body:
//	       {"part_table":"msdos|gpt|" , "partitions":[{"size_mb":...,"filesystem":"...","label":"..."}]}
func (d *DezkVM) HandleStorageFormatPreview(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	devPath, err := targetInstance.GetMassStoragePath()
	if err != nil {
		http.Error(w, "Mass storage device not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	partTable := ""
	var specs []storage.PartitionSpec
	if r.Method == http.MethodPost {
		var req struct {
			PartTable  string                  `json:"part_table"`
			Filesystem string                  `json:"filesystem"`
			Label      string                  `json:"label"`
			Partitions []storage.PartitionSpec `json:"partitions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		partTable = req.PartTable
		specs, err = formatRequestToSpecs(req.Filesystem, req.Label, req.Partitions)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		specs = []storage.PartitionSpec{{
			Filesystem: r.URL.Query().Get("filesystem"),
			Label:      r.URL.Query().Get("label"),
		}}
	}

	preview, err := storage.GetFormatPreviewParts(devPath, partTable, specs)
	if err != nil {
		http.Error(w, "Format preview failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
}

// HandleStorageDiskFormat formats the mass storage device with the requested
// partition layout.  Accepts POST only.  The JSON body contains either a
// "partitions" array (with optional "part_table") or the legacy single
// partition "filesystem"/"label" fields.
//
// The disk must currently be on the KVM side.  All partitions are unmounted
// and all data on the disk is permanently erased.
func (d *DezkVM) HandleStorageDiskFormat(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	var req struct {
		PartTable  string                  `json:"part_table"`
		Filesystem string                  `json:"filesystem"`
		Label      string                  `json:"label"`
		Partitions []storage.PartitionSpec `json:"partitions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	specs, err := formatRequestToSpecs(req.Filesystem, req.Label, req.Partitions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	devPath, err := targetInstance.GetMassStoragePath()
	if err != nil {
		http.Error(w, "Mass storage device not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Refuse to format while an image write is running on the same disk.
	if targetInstance.isoWriteJob != nil && targetInstance.isoWriteJob.IsRunning() {
		http.Error(w, "An image write is currently in progress on this device", http.StatusConflict)
		return
	}

	// Snapshot state that must be restored on failure.
	originalMountPoint := targetInstance.massStorageMountPoint

	// Clear the cached mount point – FormatDiskParts will unmount all partitions.
	targetInstance.massStorageMountPoint = ""

	if err := storage.FormatDiskParts(devPath, req.PartTable, specs); err != nil {
		// Format failed – attempt to restore the previous mount so the instance
		// remains usable without requiring a manual remount.
		if originalMountPoint != "" {
			if remountErr := remountDevice(devPath, originalMountPoint); remountErr != nil {
				log.Printf("Warning: restore-remount to %s failed after format error for instance %s: %v\n",
					originalMountPoint, instanceUuid, remountErr)
			} else {
				targetInstance.massStorageMountPoint = originalMountPoint
				log.Printf("Restored mount point %s for instance %s after format failure\n",
					originalMountPoint, instanceUuid)
			}
		}
		http.Error(w, "Format failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-mount after a successful format so the file manager is immediately usable.
	// The partition table UUID changes every time parted writes a new label, so
	// we must rescan the device and refresh massStorageUUID before calling
	// MountMassStorage (which uses massStorageUUID to find the disk).
	if newPTUUID, scanErr := storage.GetPTUUIDFromDevicePath(devPath); scanErr != nil {
		log.Printf("Warning: could not read new PTUUID for %s after format (instance %s): %v\n",
			devPath, instanceUuid, scanErr)
	} else {
		targetInstance.massStorageUUID = newPTUUID
		log.Printf("Updated massStorageUUID to %s for instance %s after format\n", newPTUUID, instanceUuid)
	}

	mountPoint, err := targetInstance.MountMassStorage()
	if err != nil {
		// Not fatal – format succeeded; client can refresh manually.
		log.Printf("Warning: re-mount after format failed for instance %s: %v\n", instanceUuid, err)
	} else {
		targetInstance.massStorageMountPoint = mountPoint
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Disk formatted successfully",
	})
}

// remountDevice is a best-effort helper that re-mounts a block device (or its
// first partition) back to mountPoint after a failed format attempt.
// It mirrors the logic in UsbKvmDeviceInstance.MountMassStorage.
func remountDevice(devPath, mountPoint string) error {
	// If the disk has partitions, prefer mounting the first one.
	mountTarget := devPath
	if parts, err := storage.GetDevicePartitionsInfoFromPath(devPath); err == nil && len(parts) >= 1 {
		mountTarget = "/dev/" + parts[0].Name
	}

	// Check if already mounted at the expected point.
	if active, err := storage.GetMountPointFromDevicePath(mountTarget); err == nil && active == mountPoint {
		return nil
	}

	cmd := exec.Command("mount", mountTarget, mountPoint)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mount %s → %s: %w — %s", mountTarget, mountPoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// ISO / disk-image write handlers
// ---------------------------------------------------------------------------

// prepareDiskForImageWrite validates that the mass storage device can be
// overwritten right now and unmounts it.  It returns the raw disk device path.
func (d *DezkVM) prepareDiskForImageWrite(targetInstance *UsbKvmDeviceInstance) (string, error) {
	if targetInstance.auxMCUController == nil {
		return "", fmt.Errorf("auxiliary MCU controller not initialized")
	}
	if targetInstance.auxMCUController.GetUSBMassStorageSide() != kvmaux.USB_MASS_STORAGE_KVM {
		return "", fmt.Errorf("mass storage is not on the KVM side; switch it to KVM side first")
	}

	devPath, err := targetInstance.GetMassStoragePath()
	if err != nil {
		return "", fmt.Errorf("mass storage device not available: %w", err)
	}

	// Unmount everything on the disk before overwriting it.
	targetInstance.massStorageMountPoint = ""
	if err := storage.UnmountAllPartitions(devPath); err != nil {
		return "", fmt.Errorf("failed to unmount mass storage: %w", err)
	}
	return devPath, nil
}

// finalizeAfterImageWrite refreshes the kernel partition table, the cached
// mass-storage UUID and attempts a best-effort remount after a disk image has
// been written.  All failures are logged only: the image itself was written
// successfully at this point.
func (d *DezkVM) finalizeAfterImageWrite(targetInstance *UsbKvmDeviceInstance, devPath string) {
	storage.RescanPartitions(devPath)

	// Writing an image replaces the partition table, so the cached UUID used
	// to locate the disk must be refreshed.
	if newUUID, err := storage.GetPTUUIDFromDevicePath(devPath); err != nil {
		log.Printf("Warning: could not read new PTUUID for %s after image write (instance %s): %v\n",
			devPath, targetInstance.uuid, err)
	} else {
		targetInstance.massStorageUUID = newUUID
		log.Printf("Updated massStorageUUID to %s for instance %s after image write\n", newUUID, targetInstance.uuid)
	}

	// Best-effort remount so the file manager can browse the written image
	// (ISO9660 filesystems mount read-only).
	if mountPoint, err := targetInstance.MountMassStorage(); err != nil {
		log.Printf("Note: mount after image write failed for instance %s (this is normal for some images): %v\n",
			targetInstance.uuid, err)
	} else {
		targetInstance.massStorageMountPoint = mountPoint
	}
}

// validateImageFitsDisk errors when the image is larger than the target disk.
// A size of 0 (unknown) is accepted.
func validateImageFitsDisk(devPath string, imageSize int64) error {
	diskInfo, err := storage.GetDiskInfoFromDevicePath(devPath)
	if err != nil {
		return fmt.Errorf("cannot read disk info: %w", err)
	}
	if imageSize > 0 && uint64(imageSize) > diskInfo.Size {
		return fmt.Errorf("image (%d bytes) is larger than the disk (%d bytes)", imageSize, diskInfo.Size)
	}
	return nil
}

// HandleISOWriteStatus returns the state of the current / last image write on
// the instance's mass storage device.
func (d *DezkVM) HandleISOWriteStatus(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targetInstance.isoWriteJob.Status())
}

// HandleISOWriteStart writes a stored image file (isoPath, resolved by the
// caller from the ISO library) to the instance's mass storage device.  The
// write runs in the background; poll HandleISOWriteStatus for progress.
func (d *DezkVM) HandleISOWriteStart(w http.ResponseWriter, r *http.Request, instanceUuid string, isoPath string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	isoFile, err := os.Open(isoPath)
	if err != nil {
		http.Error(w, "Failed to open image: "+err.Error(), http.StatusInternalServerError)
		return
	}
	isoInfo, err := isoFile.Stat()
	if err != nil {
		isoFile.Close()
		http.Error(w, "Failed to stat image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reserve the job before touching the disk so concurrent start requests
	// are rejected and status polls immediately reflect the new run.
	isoName := filepath.Base(isoPath)
	job := targetInstance.isoWriteJob
	if err := job.Begin(isoName, isoInfo.Size()); err != nil {
		isoFile.Close()
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	devPath, err := d.prepareDiskForImageWrite(targetInstance)
	if err != nil {
		isoFile.Close()
		job.Abort(err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := validateImageFitsDisk(devPath, isoInfo.Size()); err != nil {
		isoFile.Close()
		job.Abort(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	go func() {
		defer isoFile.Close()
		if err := job.WriteImage(devPath, isoFile); err != nil {
			// The job already carries the error state for the frontend.
			log.Printf("Image write of %s to %s failed for instance %s: %v\n", isoName, devPath, instanceUuid, err)
			return
		}
		log.Printf("Image %s written to %s for instance %s\n", isoName, devPath, instanceUuid)
		d.finalizeAfterImageWrite(targetInstance, devPath)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Image write started",
	})
}

// HandleISOWriteUpload streams an uploaded image directly onto the instance's
// mass storage device without persisting it on the KVM host first.  The
// multipart body should contain an optional "size" field (image size in
// bytes, sent before the file) followed by the image file itself under the
// field name "iso".  The request blocks until the write completes; progress
// can be polled via HandleISOWriteStatus in parallel.
func (d *DezkVM) HandleISOWriteUpload(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}

	job := targetInstance.isoWriteJob
	if job.IsRunning() {
		http.Error(w, "An image write is already in progress on this device", http.StatusConflict)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Failed to parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}

	var totalSize int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			http.Error(w, "No image file provided in upload", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "Failed to read upload: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Optional "size" field lets the client provide the exact image size
		// for accurate progress reporting.
		if part.FormName() == "size" && part.FileName() == "" {
			sizeBuf, _ := io.ReadAll(io.LimitReader(part, 32))
			part.Close()
			if v, convErr := strconv.ParseInt(strings.TrimSpace(string(sizeBuf)), 10, 64); convErr == nil && v > 0 {
				totalSize = v
			}
			continue
		}

		if (part.FormName() != "iso" && part.FormName() != "files") || part.FileName() == "" {
			part.Close()
			continue
		}

		// Found the image part — start writing it to the device.
		isoName := filepath.Base(part.FileName())
		if beginErr := job.Begin(isoName, totalSize); beginErr != nil {
			part.Close()
			http.Error(w, beginErr.Error(), http.StatusConflict)
			return
		}
		devPath, prepErr := d.prepareDiskForImageWrite(targetInstance)
		if prepErr != nil {
			part.Close()
			job.Abort(prepErr)
			http.Error(w, prepErr.Error(), http.StatusServiceUnavailable)
			return
		}
		if fitErr := validateImageFitsDisk(devPath, totalSize); fitErr != nil {
			part.Close()
			job.Abort(fitErr)
			http.Error(w, fitErr.Error(), http.StatusBadRequest)
			return
		}

		writeErr := job.WriteImage(devPath, part)
		part.Close()
		if writeErr != nil {
			log.Printf("Streamed image write of %s to %s failed for instance %s: %v\n", isoName, devPath, instanceUuid, writeErr)
			http.Error(w, "Image write failed: "+writeErr.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("Streamed image %s written to %s for instance %s\n", isoName, devPath, instanceUuid)
		d.finalizeAfterImageWrite(targetInstance, devPath)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"message": "Image written successfully",
		})
		return
	}
}

// ---------------------------------------------------------------------------
// ATX power control handlers
// ---------------------------------------------------------------------------

// atxButtonDurations maps trigger actions to how long the simulated button is
// held down.
var atxButtonDurations = map[string]time.Duration{
	"power_click": 300 * time.Millisecond, // power on / request soft shutdown
	"power_hold":  6 * time.Second,        // force power off
	"reset_click": 300 * time.Millisecond, // hardware reset
}

// HandleATXState returns the remote computer's ATX front-panel LED states.
func (d *DezkVM) HandleATXState(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	if targetInstance.auxMCUController == nil {
		http.Error(w, "Auxiliary MCU controller not initialized or missing", http.StatusInternalServerError)
		return
	}
	if err := targetInstance.auxMCUController.GetATXState(); err != nil {
		http.Error(w, "Failed to read ATX state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{
		"power_led": targetInstance.auxMCUController.GetPowerLEDState(),
		"hdd_led":   targetInstance.auxMCUController.GetHDDLEDState(),
	})
}

// HandleATXTrigger simulates pressing a front-panel button on the remote
// computer.  POST with JSON body {"action":"power_click"|"power_hold"|"reset_click"}.
// The request blocks for the duration of the button press (up to 6 s for
// power_hold).
func (d *DezkVM) HandleATXTrigger(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	mcu := targetInstance.auxMCUController
	if mcu == nil {
		http.Error(w, "Auxiliary MCU controller not initialized or missing", http.StatusInternalServerError)
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	holdDuration, ok := atxButtonDurations[req.Action]
	if !ok {
		http.Error(w, "Invalid action: must be power_click, power_hold or reset_click", http.StatusBadRequest)
		return
	}

	press := mcu.PressPowerButton
	release := mcu.ReleasePowerButton
	if req.Action == "reset_click" {
		press = mcu.PressResetButton
		release = mcu.ReleaseResetButton
	}

	if err := press(); err != nil {
		// Try to release in case the press command partially registered.
		_ = release()
		http.Error(w, "Failed to press button: "+err.Error(), http.StatusInternalServerError)
		return
	}
	time.Sleep(holdDuration)
	if err := release(); err != nil {
		http.Error(w, "Failed to release button (check the KVM unit): "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("ATX action %s executed on instance %s\n", req.Action, instanceUuid)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"action": req.Action,
	})
}
