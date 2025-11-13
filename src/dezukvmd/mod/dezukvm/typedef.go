package dezukvm

import (
	"imuslab.com/dezukvm/dezukvmd/mod/kvmaux"
	"imuslab.com/dezukvm/dezukvmd/mod/kvmhid"
	"imuslab.com/dezukvm/dezukvmd/mod/usbcapture"
)

type UsbKvmDeviceOption struct {
	/* Device Paths */
	USBKVMDevicePath       string `json:"usb_kvm_device_path"`       // Path to the USB KVM HID device (e.g., /dev/ttyUSB0)
	AuxMCUDevicePath       string `json:"aux_mcu_device_path"`       // Path to the auxiliary MCU device (e.g., /dev/ttyACM0)
	VideoCaptureDevicePath string `json:"video_capture_device_path"` // Path to the video capture device (e.g., /dev/video0)
	AudioCaptureDevicePath string `json:"audio_capture_device_path"` // Path to the audio capture device (e.g., /dev/snd/pcmC1D0c)

	/* Capture Settings */
	CaptureVideoResolutionWidth   int `json:"capture_video_resolution_width"`  // Video capture resolution width in pixels, e.g., 1920
	CaptureeVideoResolutionHeight int `json:"capture_video_resolution_height"` // Video capture resolution height in pixels, e.g., 1080
	CaptureeVideoFPS              int `json:"capture_video_resolution_fps"`    // Video capture frames per second, e.g., 25
	CaptureAudioSampleRate        int `json:"capture_audio_sample_rate"`       // Audio capture sample rate in Hz, e.g., 48000
	CaptureAudioChannels          int `json:"capture_audio_channels"`          // Number of audio channels, e.g., 2 for stereo
	CaptureAudioBytesPerSample    int `json:"capture_audio_bytes_per_sample"`  // Bytes per audio sample, e.g., 2 for 16-bit audio
	CaptureAudioFrameSize         int `json:"capture_audio_frame_size"`        // Size of each audio frame in bytes, e.g., 1920

	/* Communication Settings */
	USBKVMBaudrate int `json:"usb_kvm_baudrate"` // Baudrate for USB KVM HID communication, e.g., 115200
	AuxMCUBaudrate int `json:"aux_mcu_baudrate"` // Baudrate for auxiliary MCU communication, e.g., 115200
}

type UsbKvmDeviceInstance struct {
	Config *UsbKvmDeviceOption

	/* Processed Configs */
	captureConfig         *usbcapture.Config
	videoResoltuionConfig *usbcapture.CaptureResolution

	/* Internals */
	uuid             string // Session UUID obtained from AuxMCU
	usbKVMController *kvmhid.Controller
	auxMCUController *kvmaux.AuxMcu
	usbCaptureDevice *usbcapture.Instance
	parent           *DezukVM
}

type RuntimeOptions struct {
	EnableLog bool `json:"enable_log"` // Enable or disable logging
}
type DezukVM struct {
	UsbKvmInstance []*UsbKvmDeviceInstance

	/* Internals */
	occupiedUUIDs map[string]bool // Track occupied UUIDs to prevent duplicate connections
	option        *RuntimeOptions // Runtime options
}
