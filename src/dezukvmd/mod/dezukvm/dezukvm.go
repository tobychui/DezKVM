package dezukvm

import (
	"errors"

	"imuslab.com/dezukvm/dezukvmd/mod/usbcapture"
)

// NewKvmHostInstance creates a new instance of DezukVM, which can manage multiple USB KVM devices.
func NewKvmHostInstance(option *RuntimeOptions) *DezukVM {
	return &DezukVM{
		UsbKvmInstance: []*UsbKvmDeviceInstance{},
		occupiedUUIDs:  make(map[string]bool),
		option:         option,
	}
}

// AddUsbKvmDevice adds a new USB KVM device instance to the DezukVM manager.
func (d *DezukVM) AddUsbKvmDevice(config *UsbKvmDeviceOption) error {
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
func (d *DezukVM) RemoveUsbKvmDevice(uuid string) error {
	for i, dev := range d.UsbKvmInstance {
		if dev.UUID() == uuid {
			d.UsbKvmInstance = append(d.UsbKvmInstance[:i], d.UsbKvmInstance[i+1:]...)
			return nil
		}
	}
	return errors.New("target USB KVM device not found")
}

func (d *DezukVM) StartAllUsbKvmDevices() error {
	for _, instance := range d.UsbKvmInstance {
		err := instance.Start()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DezukVM) StopAllUsbKvmDevices() error {
	for _, instance := range d.UsbKvmInstance {
		err := instance.Stop()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DezukVM) GetInstanceByUUID(uuid string) (*UsbKvmDeviceInstance, error) {
	for _, instance := range d.UsbKvmInstance {
		if instance.UUID() == uuid {
			return instance, nil
		}
	}
	return nil, errors.New("instance with specified UUID not found")
}

func (d *DezukVM) Close() error {
	return d.StopAllUsbKvmDevices()
}
