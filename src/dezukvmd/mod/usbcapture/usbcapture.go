package usbcapture

import (
	"fmt"
	"os"
)

// NewInstance creates a new video capture instance
func NewInstance(config *Config) (*Instance, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	//Check if the video device exists
	if _, err := os.Stat(config.VideoDeviceName); os.IsNotExist(err) {
		return nil, fmt.Errorf("video device %s does not exist", config.VideoDeviceName)
	} else if err != nil {
		return nil, fmt.Errorf("failed to check video device: %w", err)
	}

	//Check if the device file actualy points to a video device
	isValidDevice, err := CheckVideoCaptureDevice(config.VideoDeviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to check video device: %w", err)
	}

	if !isValidDevice {
		return nil, fmt.Errorf("device %s is not a video capture device", config.VideoDeviceName)
	}

	//Get the supported resolutions of the video device
	formatInfo, err := GetV4L2FormatInfo(config.VideoDeviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get video device format info: %w", err)
	}

	if len(formatInfo) == 0 {
		return nil, fmt.Errorf("no supported formats found for device %s", config.VideoDeviceName)
	}

	return &Instance{
		Config:               config,
		Capturing:            false,
		SupportedResolutions: formatInfo,

		// Videos
		camera:     nil,
		pixfmt:     0,
		width:      0,
		height:     0,
		streamInfo: "",

		//Audio
		audiostopchan: make(chan bool, 1),

		// Access control
		videoTakeoverChan: make(chan bool, 1),
		accessCount:       0,
	}, nil
}

// GetStreamInfo returns the stream information string
func (i *Instance) GetStreamInfo() string {
	return i.streamInfo
}

// IsCapturing checks if the camera is currently capturing video
func (i *Instance) IsCapturing() bool {
	return i.Capturing
}

// IsAudioStreaming checks if the audio is currently being captured
func (i *Instance) IsAudioStreaming() bool {
	return i.isAudioStreaming
}

// Close closes the camera device and releases resources
func (i *Instance) Close() error {
	if i.camera != nil {
		i.StopCapture()
	}
	return nil
}
