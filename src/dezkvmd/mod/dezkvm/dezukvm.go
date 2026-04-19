package dezkvm

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"

	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
)

// NewKvmHostInstance creates a new instance of DezkVM, which can manage multiple USB KVM devices.
func NewKvmHostInstance(option *RuntimeOptions) *DezkVM {
	confFolder := option.ConfigFolderPath
	if confFolder == "" {
		confFolder = "./config/instances"
	}
	// Create the config folder if it doesn't exist
	if err := os.MkdirAll(confFolder, 0750); err != nil {
		log.Printf("Warning: failed to create config folder %s: %v\n", confFolder, err)
	}
	return &DezkVM{
		UsbKvmInstance:   []*UsbKvmDeviceInstance{},
		ConfigFolderPath: confFolder,
		occupiedUUIDs:    make(map[string]bool),
		option:           option,
	}
}

// AddUsbKvmDevice adds a new USB KVM device instance to the DezkVM manager.
func (d *DezkVM) AddUsbKvmDevice(config *UsbKvmDeviceOption) error {
	//Build the capture config from the device option
	// Audio config
	if config.AudioCaptureDevicePath == "" {
		return errors.New("audio capture device path is not specified")
	}
	defaultAudioConfig := usbcapture.GetDefaultAudioConfig()
	if config.CaptureAudioSampleRate == 0 {
		config.CaptureAudioSampleRate = defaultAudioConfig.SampleRate
	}
	if config.CaptureAudioChannels == 0 {
		config.CaptureAudioChannels = defaultAudioConfig.Channels
	}
	if config.CaptureAudioBytesPerSample == 0 {
		config.CaptureAudioBytesPerSample = defaultAudioConfig.BytesPerSample
	}
	if config.CaptureAudioFrameSize == 0 {
		config.CaptureAudioFrameSize = defaultAudioConfig.FrameSize
	}

	//Remap the audio config
	audioCaptureCfg := &usbcapture.AudioConfig{
		SampleRate:     config.CaptureAudioSampleRate,
		Channels:       config.CaptureAudioChannels,
		BytesPerSample: config.CaptureAudioBytesPerSample,
		FrameSize:      config.CaptureAudioFrameSize,
	}

	//Setup video capture configs
	if config.VideoCaptureDevicePath == "" {
		return errors.New("video capture device path is not specified")
	}
	if config.CaptureVideoResolutionWidth == 0 {
		config.CaptureVideoResolutionWidth = 1920
	}
	if config.CaptureeVideoResolutionHeight == 0 {
		config.CaptureeVideoResolutionHeight = 1080
	}
	if config.CaptureeVideoFPS == 0 {
		config.CaptureeVideoFPS = 25
	}

	// Setup video config
	videoConfig := &usbcapture.VideoConfig{
		UseH264: true, //TODO: make it configurable later
		Profile: "1080p",
	}

	// capture config
	captureCfg := &usbcapture.Config{
		VideoDeviceName: config.VideoCaptureDevicePath,
		AudioDeviceName: config.AudioCaptureDevicePath,
		AudioConfig:     audioCaptureCfg,
		VideoConfig:     videoConfig,
	}

	// video resolution config
	videoResolutionConfig := &usbcapture.CaptureResolution{
		Width:  config.CaptureVideoResolutionWidth,
		Height: config.CaptureeVideoResolutionHeight,
		FPS:    config.CaptureeVideoFPS,
	}

	instance := &UsbKvmDeviceInstance{
		Config: config,

		captureConfig:         captureCfg,
		videoResoltuionConfig: videoResolutionConfig,

		uuid:             "", // Will be set when starting the instance
		usbKVMController: nil,
		auxMCUController: nil,
		usbCaptureDevice: nil,
		parent:           d,
	}
	d.UsbKvmInstance = append(d.UsbKvmInstance, instance)
	return nil
}

// RemoveUsbKvmDevice removes a USB KVM device instance by its UUID.
func (d *DezkVM) RemoveUsbKvmDevice(uuid string) error {
	for i, dev := range d.UsbKvmInstance {
		if dev.UUID() == uuid {
			d.UsbKvmInstance = append(d.UsbKvmInstance[:i], d.UsbKvmInstance[i+1:]...)
			return nil
		}
	}
	return errors.New("target USB KVM device not found")
}

func (d *DezkVM) StartAllUsbKvmDevices() error {
	for _, instance := range d.UsbKvmInstance {
		err := instance.Start()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DezkVM) StopAllUsbKvmDevices() error {
	for _, instance := range d.UsbKvmInstance {
		err := instance.Stop()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DezkVM) GetInstanceByUUID(uuid string) (*UsbKvmDeviceInstance, error) {
	for _, instance := range d.UsbKvmInstance {
		if instance.UUID() == uuid {
			return instance, nil
		}
	}
	return nil, errors.New("instance with specified UUID not found")
}

func (d *DezkVM) Close() error {
	return d.StopAllUsbKvmDevices()
}

// preferencesFilePath returns the path to the preferences JSON file for a given UUID.
func (d *DezkVM) preferencesFilePath(uuid string) string {
	return filepath.Join(d.ConfigFolderPath, uuid+".json")
}

// SavePreferences writes the instance preferences to disk as pretty-printed JSON.
func (d *DezkVM) SavePreferences(instance *UsbKvmDeviceInstance) error {
	if instance.Preferences == nil {
		return nil
	}
	data, err := json.MarshalIndent(instance.Preferences, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.preferencesFilePath(instance.UUID()), data, 0644)
}

// LoadPreferences reads preferences from disk for a given UUID.
// Returns nil (no error) if the file does not exist.
func (d *DezkVM) LoadPreferences(uuid string) (*UsbKvmPreferences, error) {
	path := d.preferencesFilePath(uuid)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var prefs UsbKvmPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, err
	}
	return &prefs, nil
}
