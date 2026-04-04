package usbcapture

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"syscall"

	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
)

type Config struct {
	DeviceName string
	Resolution *SizeInfo //The prefered resolution to start the video stream
}

type Instance struct {
	Config               *Config
	SupportedResolutions []FormatInfo //The supported resolutions of the video device
	Capturing            bool
	camera               *device.Device
	frames_buff          <-chan []byte
	pixfmt               v4l2.FourCCType
	width                int
	height               int
	streamInfo           string
}

// NewInstance creates a new video capture instance
func NewInstance(config *Config) (*Instance, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	//Check if the video device exists
	if _, err := os.Stat(config.DeviceName); os.IsNotExist(err) {
		return nil, fmt.Errorf("video device %s does not exist", config.DeviceName)
	} else if err != nil {
		return nil, fmt.Errorf("failed to check video device: %w", err)
	}

	//Check if the device file actualy points to a video device
	isValidDevice, err := checkVideoCaptureDevice(config.DeviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to check video device: %w", err)
	}

	if !isValidDevice {
		return nil, fmt.Errorf("device %s is not a video capture device", config.DeviceName)
	}

	//Get the supported resolutions of the video device
	formatInfo, err := GetV4L2FormatInfo(config.DeviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get video device format info: %w", err)
	}

	if len(formatInfo) == 0 {
		return nil, fmt.Errorf("no supported formats found for device %s", config.DeviceName)
	}

	return &Instance{
		Config:               config,
		Capturing:            false,
		SupportedResolutions: formatInfo,
	}, nil
}

// start http service
func (i *Instance) ServeVideoStream(w http.ResponseWriter, req *http.Request) {
	mimeWriter := multipart.NewWriter(w)
	w.Header().Set("Content-Type", fmt.Sprintf("multipart/x-mixed-replace; boundary=%s", mimeWriter.Boundary()))
	partHeader := make(textproto.MIMEHeader)
	partHeader.Add("Content-Type", "image/jpeg")

	var frame []byte
	for frame = range i.frames_buff {
		if len(frame) == 0 {
			log.Print("skipping empty frame")
			continue
		}

		partWriter, err := mimeWriter.CreatePart(partHeader)
		if err != nil {
			log.Printf("failed to create multi-part writer: %s", err)
			return
		}

		if _, err := partWriter.Write(frame); err != nil {
			if errors.Is(err, syscall.EPIPE) {
				//broken pipe, the client browser has exited
				return
			}
			log.Printf("failed to write image: %s", err)
		}

	}
}

// start video capture
func (i *Instance) StartVideoCapture(selectedFPS int) error {
	if i.Capturing {
		return fmt.Errorf("video capture already started")
	}

	devName := i.Config.DeviceName
	frameRate := 25
	buffSize := 8
	format := "mjpeg"

	if i.Config.Resolution == nil {
		return fmt.Errorf("resolution not provided")
	}

	//Check if the selected FPS is valid in the provided Resolutions
	if i.Config.Resolution != nil {
		for _, size := range i.Config.Resolution.FPS {
			if size == selectedFPS {
				frameRate = size
				break
			}
		}

		if frameRate != selectedFPS {
			log.Printf("selected FPS %d is not supported, using default %d", selectedFPS, frameRate)
		}
	} else {
		log.Printf("no resolution provided, using default %d", frameRate)
	}

	//Check if the video device is a capture device
	isCaptureDev, err := checkVideoCaptureDevice(devName)
	if err != nil {
		return fmt.Errorf("failed to check video device: %w", err)
	}
	if !isCaptureDev {
		return fmt.Errorf("device %s is not a video capture device", devName)
	}

	//Open the video device
	camera, err := device.Open(devName,
		device.WithIOType(v4l2.IOTypeMMAP),
		device.WithPixFormat(v4l2.PixFormat{
			PixelFormat: getFormatType(format),
			Width:       uint32(i.Config.Resolution.Width),
			Height:      uint32(i.Config.Resolution.Height),
			Field:       v4l2.FieldAny,
		}),
		device.WithFPS(uint32(frameRate)),
		device.WithBufferSize(uint32(buffSize)),
	)

	if err != nil {
		return fmt.Errorf("failed to open video device: %w", err)
	}

	i.camera = camera

	caps := camera.Capability()
	log.Printf("device [%s] opened\n", devName)
	log.Printf("device info: %s", caps.String())
	//2025/03/16 15:45:25 device info: driver: uvcvideo; card: USB Video: USB Video; bus info: usb-0000:00:14.0-2

	// set device format
	currFmt, err := camera.GetPixFormat()
	if err != nil {
		log.Fatalf("unable to get format: %s", err)
	}
	log.Printf("Current format: %s", currFmt)
	//2025/03/16 15:45:25 Current format: Motion-JPEG [1920x1080]; field=any; bytes per line=0; size image=0; colorspace=Default; YCbCr=Default; Quant=Default; XferFunc=Default
	i.pixfmt = currFmt.PixelFormat
	i.width = int(currFmt.Width)
	i.height = int(currFmt.Height)

	i.streamInfo = fmt.Sprintf("%s - %s [%dx%d] %d fps",
		caps.Card,
		v4l2.PixelFormats[currFmt.PixelFormat],
		currFmt.Width, currFmt.Height, frameRate,
	)

	// start capture
	ctx, cancel := context.WithCancel(context.TODO())
	if err := camera.Start(ctx); err != nil {
		log.Fatalf("stream capture: %s", err)
	}
	defer func() {
		cancel()
		camera.Close()
	}()

	// video stream
	i.frames_buff = camera.GetOutput()

	log.Printf("device capture started (buffer size set %d)", camera.BufferCount())
	i.Capturing = true
	return nil
}

// GetStreamInfo returns the stream information string
func (i *Instance) GetStreamInfo() string {
	return i.streamInfo
}

// IsCapturing checks if the camera is currently capturing video
func (i *Instance) IsCapturing() bool {
	return i.Capturing
}

// StopCapture stops the video capture and closes the camera device
func (i *Instance) StopCapture() error {
	if i.camera != nil {
		i.camera.Stop()
		i.camera.Close()
		i.camera = nil
	}
	i.Capturing = false
	return nil
}

// Close closes the camera device and releases resources
func (i *Instance) Close() error {
	if i.camera != nil {
		i.StopCapture()
	}
	return nil
}

func getFormatType(fmtStr string) v4l2.FourCCType {
	switch strings.ToLower(fmtStr) {
	case "jpeg":
		return v4l2.PixelFmtJPEG
	case "mpeg":
		return v4l2.PixelFmtMPEG
	case "mjpeg":
		return v4l2.PixelFmtMJPEG
	case "h264", "h.264":
		return v4l2.PixelFmtH264
	case "yuyv":
		return v4l2.PixelFmtYUYV
	case "rgb":
		return v4l2.PixelFmtRGB24
	}
	return v4l2.PixelFmtMPEG
}
