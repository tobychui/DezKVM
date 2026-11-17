package usbcapture

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
)

/*
	Bandwidth estimation for MJPEG video stream

	1920 x 1080 60fps = 55Mbps //Chrome not support
	1920 x 1080 30fps = 50Mbps
	1920 x 1080 25fps = 40Mbps
	1920 x 1080 20fps = 30Mbps
	1920 x 1080 10fps = 15Mbps

	1360 x 768 60fps = 28Mbps //Chrome not support
	1360 x 768 30fps = 25Mbps
	1360 x 768 25fps = 20Mbps
	1360 x 768 20fps = 18Mbps
	1360 x 768 10fps = 10Mbps
*/

// Struct to store the size and fps info
type FormatInfo struct {
	Format string
	Sizes  []SizeInfo
}

type SizeInfo struct {
	Width  int
	Height int
	FPS    []int
}

//go:embed stream_takeover.jpg
var endOfStreamJPG []byte

// StartVideoCapture starts the video capture. Once started, the capture is
// supervised: if the capture device is lost (for example a USB glitch drops
// the capture card) it is restarted automatically with the same settings
// until StopVideoCapture or Close is called.
func (i *Instance) StartVideoCapture(openWithResolution *CaptureResolution) error {
	i.capMu.Lock()
	defer i.capMu.Unlock()

	if err := i.startVideoCaptureLocked(openWithResolution); err != nil {
		return err
	}
	i.wantCapture = true
	return nil
}

// startVideoCaptureLocked opens the device and starts the capture. The caller must hold capMu.
func (i *Instance) startVideoCaptureLocked(openWithResolution *CaptureResolution) error {
	if i.Capturing {
		return fmt.Errorf("video capture already started")
	}

	if openWithResolution == nil {
		return fmt.Errorf("resolution not provided")
	}
	if openWithResolution.FPS == 0 {
		openWithResolution.FPS = 25 //Default to 25 FPS
	}

	devName := i.Config.VideoDeviceName
	frameRate := openWithResolution.FPS
	buffSize := 4 //No. of frames to buffer, 2 is too few for the capture card to always have a free buffer
	//Default to MJPEG
	//Other formats that are commonly supported are YUYV, H264, MJPEG
	format := "mjpeg"

	//Check if the video device is a capture device
	isCaptureDev, err := CheckVideoCaptureDevice(devName)
	if err != nil {
		return fmt.Errorf("failed to check video device: %w", err)
	}
	if !isCaptureDev {
		return fmt.Errorf("device %s is not a video capture device", devName)
	}

	//Check if the selected FPS is valid in the provided Resolutions
	resolutionIsSupported, err := deviceSupportResolution(i.Config.VideoDeviceName, openWithResolution)
	if err != nil {
		return err
	}
	if !resolutionIsSupported {
		return errors.New("this device do not support the required resolution settings")
	}

	// Retry opening the device up to 5 times with increasing delay
	var camera *device.Device
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		//Open the video device
		camera, err = device.Open(devName,
			device.WithIOType(v4l2.IOTypeMMAP),
			device.WithPixFormat(v4l2.PixFormat{
				PixelFormat: getFormatType(format),
				Width:       uint32(openWithResolution.Width),
				Height:      uint32(openWithResolution.Height),
				Field:       v4l2.FieldAny,
			}),
			device.WithFPS(uint32(frameRate)),
			device.WithBufferSize(uint32(buffSize)),
		)
		if err == nil {
			break // Success
		}

		// Check if it's a "device or resource busy" error
		if strings.Contains(err.Error(), "device or resource busy") && attempt < maxRetries-1 {
			log.Printf("Device busy, retrying in %d seconds (attempt %d/%d)", attempt+1, attempt+1, maxRetries)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		// Other error or last attempt failed
		log.Println("Error opening video device:", err)
		return fmt.Errorf("failed to open video device: %w", err)
	}

	i.camera = camera
	caps := camera.Capability()
	log.Printf("device [%s] opened\n", devName)
	log.Printf("device info: %s", caps.String())
	// Should get something like this:
	//2025/03/16 15:45:25 device info: driver: uvcvideo; card: USB Video: USB Video; bus info: usb-0000:00:14.0-2

	// set device format
	currFmt, err := camera.GetPixFormat()
	if err != nil {
		camera.Close()
		i.camera = nil
		return fmt.Errorf("failed to get current pixel format: %w", err)
	}

	// Should get something like this:
	// 2025/03/16 15:45:25 Current format: Motion-JPEG [1920x1080]; field=any; bytes per line=0; size image=0; colorspace=Default; YCbCr=Default; Quant=Default; XferFunc=Default
	log.Printf("Current format: %s", currFmt)

	// store format info
	i.pixfmt = currFmt.PixelFormat
	i.width = int(currFmt.Width)
	i.height = int(currFmt.Height)
	i.fps = frameRate

	i.streamInfo = fmt.Sprintf("%s - %s [%dx%d] %d fps",
		caps.Card,
		v4l2.PixelFormats[currFmt.PixelFormat],
		currFmt.Width, currFmt.Height, frameRate,
	)

	// start capture
	ctx, cancel := context.WithCancel(context.TODO())
	if err := camera.Start(ctx); err != nil {
		cancel()
		camera.Close()
		i.camera = nil
		return fmt.Errorf("stream capture: %w", err)
	}
	i.cameraStartContext = cancel

	// Consumers read from one channel that outlives any single camera, so a
	// stream that is already open resumes by itself after a restart.
	if i.frameCh == nil {
		i.frameCh = make(chan []byte, buffSize)
	}
	i.frames_buff = i.frameCh
	for drained := false; !drained; { // drop frames left over from a previous camera
		select {
		case <-i.frameCh:
		default:
			drained = true
		}
	}
	go i.pumpFrames(camera, ctx)

	log.Printf("device capture started (buffer size set %d)", camera.BufferCount())
	i.lastRes = *openWithResolution
	i.Capturing = true
	return nil
}

// pumpFrames forwards the camera's frames to the stable frame channel until the
// camera's capture loop ends. If that was not requested (the device failed), it
// starts the recovery.
func (i *Instance) pumpFrames(camera *device.Device, ctx context.Context) {
	for frame := range camera.GetOutput() {
		select {
		case i.frameCh <- frame:
		default:
			// Nobody is keeping up (or nobody is watching). Drop the oldest
			// frame instead of stalling the camera, a stale frame is worthless.
			select {
			case <-i.frameCh:
			default:
			}
			select {
			case i.frameCh <- frame:
			default:
			}
		}
	}

	if ctx.Err() != nil {
		return // stopped on purpose
	}
	log.Printf("video capture device lost: %v", camera.StreamError())
	i.recoverCapture(camera)
}

// recoverCapture tears down a failed camera and keeps trying to restart the
// capture with the last used settings until it works or capture is stopped.
func (i *Instance) recoverCapture(failed *device.Device) {
	i.capMu.Lock()
	if i.camera != failed || !i.wantCapture {
		i.capMu.Unlock() // already stopped or replaced by someone else
		return
	}
	i.Capturing = false
	i.cameraStartContext()
	if err := failed.Close(); err != nil {
		log.Printf("closing the lost capture device: %v", err)
	}
	i.camera = nil
	res := i.lastRes
	i.capMu.Unlock()

	backoff := time.Second
	for attempt := 1; ; attempt++ {
		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff += time.Second
		}

		i.capMu.Lock()
		if !i.wantCapture || i.camera != nil {
			i.capMu.Unlock() // capture was stopped, or restarted by someone else
			return
		}
		if _, err := os.Stat(i.Config.VideoDeviceName); err != nil {
			i.capMu.Unlock()
			if attempt == 1 || attempt%10 == 0 {
				log.Printf("waiting for %s to come back (attempt %d)", i.Config.VideoDeviceName, attempt)
			}
			continue
		}
		retry := res
		err := i.startVideoCaptureLocked(&retry)
		i.capMu.Unlock()
		if err == nil {
			log.Printf("video capture recovered (attempt %d)", attempt)
			return
		}
		log.Printf("video capture restart attempt %d failed: %v", attempt, err)
	}
}

// start http service
func (i *Instance) ServeVideoStream(w http.ResponseWriter, req *http.Request) {
	consumer, tookOver := i.beginVideoConsumer()
	defer i.endVideoConsumer(consumer)

	if tookOver {
		log.Println("Another client is already connected, kicking out the previous client...")
		log.Println("Previous client kicked out, taking over the stream...")
	}

	err := i.streamMJPEG(w, req, consumer)
	if err != nil {
		log.Printf("video stream error: %v", err)
	}
}

// badFrameCounter counts empty or corrupted frames and logs a summary at
// most every 5 seconds instead of one line per frame
type badFrameCounter struct {
	count   int
	lastLog time.Time
}

func newBadFrameCounter() *badFrameCounter {
	return &badFrameCounter{lastLog: time.Now()}
}

func (b *badFrameCounter) add() {
	b.count++
	if time.Since(b.lastLog) >= 5*time.Second {
		log.Printf("skipped %d empty or corrupted frames in the last %v (the capture card returned incomplete buffers)", b.count, time.Since(b.lastLog).Round(time.Second))
		b.count = 0
		b.lastLog = time.Now()
	}
}

// latestFrame drains the frames already waiting in ch and returns the newest
// valid one, or frame if none of the waiting frames is valid. Frames that are
// already queued are stale, sending them only adds latency.
func latestFrame(frame []byte, ch <-chan []byte, badFrames *badFrameCounter) []byte {
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return frame
			}
			if isJPEG(f) {
				frame = f
			} else if badFrames != nil {
				badFrames.add()
			}
		default:
			return frame
		}
	}
}

func isJPEG(frame []byte) bool {
	if len(frame) < 4 {
		return false
	}
	// Quick check for SOI/EOI markers
	if bytes.HasPrefix(frame, []byte{0xFF, 0xD8}) && bytes.HasSuffix(frame, []byte{0xFF, 0xD9}) {
		return true
	}
	// Fallback: look for SOI anywhere and EOI after it
	start := bytes.Index(frame, []byte{0xFF, 0xD8})
	end := bytes.LastIndex(frame, []byte{0xFF, 0xD9})
	return start >= 0 && end > start
}

func (i *Instance) streamMJPEG(w http.ResponseWriter, req *http.Request, consumer *videoConsumer) error {
	// Set up the multipart response
	mimeWriter := multipart.NewWriter(w)
	w.Header().Set("Content-Type", fmt.Sprintf("multipart/x-mixed-replace; boundary=%s", mimeWriter.Boundary()))
	partHeader := make(textproto.MIMEHeader)
	partHeader.Add("Content-Type", "image/jpeg")

	//Chrome MJPEG decoder cannot decode the first frame from MS2109 capture card for unknown reason
	//Thus we are discarding the first frame here
	if i.frames_buff != nil {
		select {
		case <-i.frames_buff:
			// Discard the first frame
		default:
			// No frame to discard
		}
	}

	// Streaming loop
	var frame []byte
	var partWriter io.Writer
	var err error
	badFrames := newBadFrameCounter()
	for {
		// Wait for the next frame, but stay responsive to the client leaving or a takeover.
		// While the capture device is being recovered no frames arrive for a few seconds.
		select {
		case <-req.Context().Done():
			return nil
		case <-consumer.takeover:
			return writeTakeoverFrame(mimeWriter)
		case frame = <-i.frames_buff:
		}

		// Skip to the newest frame to keep latency low when the network is congested
		frame = latestFrame(frame, i.frames_buff, badFrames)

		if !isJPEG(frame) {
			// Empty (incomplete buffer from the capture card) or corrupted frame
			badFrames.add()
			continue
		}

		partWriter, err = mimeWriter.CreatePart(partHeader)
		if err != nil {
			log.Printf("failed to create multi-part writer: %s", err)
			return err
		}

		// Software Frame Compression (experimental)
		if i.Config.VideoConfig.UseJPEGCompression {
			img, err := jpeg.Decode(bytes.NewReader(frame))
			if err != nil {
				log.Printf("failed to decode JPEG frame: %s", err)
				continue
			}

			var buf bytes.Buffer
			// Tested on 1080p 25fps streaming playing youtube
			// Not re-encoded -> 56Mbps
			// Quality: 100 -> 81 Mbps
			// Quality: 75 -> 30Mbps
			// Quality: 50 -> 24Mbps
			// Quality: 30 -> 20Mbps
			// Quality: 10 -> 12Mbps
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: i.Config.VideoConfig.JPEGCompressionQuality})
			if err != nil {
				log.Printf("failed to re-encode JPEG frame: %s", err)
				continue
			}
			frame = buf.Bytes()

		}

		if _, err = partWriter.Write(frame); err != nil {
			if errors.Is(err, syscall.EPIPE) {
				//broken pipe, the client browser has exited
				return nil
			}
			if errors.Is(err, http.ErrAbortHandler) {
				//TODO: properly handle client exit
				return nil
			}
			log.Printf("failed to write image: %s", err)
		}

		// Flush the response so the frame is sent immediately to the client
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case <-req.Context().Done():
			// Client disconnected, exit the loop
			return nil
		case <-consumer.takeover:
			// Another client is taking over, exit the loop
			return writeTakeoverFrame(mimeWriter)
		default:
			// Continue streaming
		}

	}
}

func writeTakeoverFrame(mimeWriter *multipart.Writer) error {
	endFrameHeader := make(textproto.MIMEHeader)
	endFrameHeader.Add("Content-Type", "image/jpeg")
	endFrameHeader.Add("Content-Length", fmt.Sprint(len(endOfStreamJPG)))
	partWriter, err := mimeWriter.CreatePart(endFrameHeader)
	if err == nil {
		if _, writeErr := partWriter.Write(endOfStreamJPG); writeErr != nil {
			return writeErr
		}
	}
	log.Println("Video stream taken over by another client, exiting...")
	return nil
}

// StopVideoCapture stops the video capture and closes the camera device.
// It also cancels any automatic recovery in progress.
func (i *Instance) StopVideoCapture() error {
	i.capMu.Lock()
	defer i.capMu.Unlock()
	i.wantCapture = false
	return i.stopVideoCaptureLocked()
}

func (i *Instance) stopVideoCaptureLocked() error {
	if i.camera != nil {
		i.cameraStartContext()
		err := i.camera.Close()
		if err != nil {
			return err
		}
		i.camera = nil
		time.Sleep(300 * time.Millisecond)
		log.Println("video capture stopped")
	} else {
		log.Println("video capture is not running")
	}
	i.Capturing = false

	return nil
}

// IsCaptureCardVideoInterface checks if the given video device is a capture card with multiple input interfaces
func IsCaptureCardVideoInterface(device string) bool {
	if ok, _ := CheckVideoCaptureDevice(device); !ok {
		return false
	}
	formats, err := GetV4L2FormatInfo(device)
	if err != nil {
		return false
	}
	count := 0
	for _, f := range formats {
		count += len(f.Sizes)
	}
	return count > 1
}

// CheckVideoCaptureDevice checks if the given video device is a video capture device
func CheckVideoCaptureDevice(device string) (bool, error) {
	// Run v4l2-ctl to get device capabilities
	cmd := exec.Command("v4l2-ctl", "--device", device, "--all")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to execute v4l2-ctl: %w", err)
	}

	// Convert output to string and check for the "Video Capture" capability
	outputStr := string(output)
	if strings.Contains(outputStr, "Video Capture") {
		return true, nil
	}
	return false, nil
}

// GetDefaultVideoDevice returns the first available video capture device, e.g., /dev/video0
func GetDefaultVideoDevice() (string, error) {
	// List all /dev/video* devices and return the first one that is a video capture device
	for i := 0; i < 10; i++ {
		device := fmt.Sprintf("/dev/video%d", i)
		isCapture, err := CheckVideoCaptureDevice(device)
		if err != nil {
			continue
		}
		if isCapture {
			return device, nil
		}
	}
	return "", fmt.Errorf("no video capture device found")
}

// deviceSupportResolution checks if the given video device supports the specified resolution and frame rate
func deviceSupportResolution(devicePath string, resolution *CaptureResolution) (bool, error) {
	formatInfo, err := GetV4L2FormatInfo(devicePath)
	if err != nil {
		return false, err
	}

	// Yes, this is an O(N^3) operation, but a video decices rarely have supported resolution
	// more than 20 combinations. The compute time should be fine
	for _, res := range formatInfo {
		for _, size := range res.Sizes {
			//Check if there is a matching resolution
			if size.Height == resolution.Height && size.Width == resolution.Width {
				//Matching resolution. Check if the required FPS is supported
				for _, fps := range size.FPS {
					if fps == resolution.FPS {
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
}

// PrintV4L2FormatInfo prints the supported formats, resolutions, and frame rates of the given video device
func PrintV4L2FormatInfo(devicePath string) {
	// Check if the device is a video capture device
	isCapture, err := CheckVideoCaptureDevice(devicePath)
	if err != nil {
		fmt.Printf("Error checking device: %v\n", err)
		return
	}
	if !isCapture {
		fmt.Printf("Device %s is not a video capture device\n", devicePath)
		return
	}

	// Get format info
	formats, err := GetV4L2FormatInfo(devicePath)
	if err != nil {
		fmt.Printf("Error getting format info: %v\n", err)
		return
	}

	// Print format info
	for _, format := range formats {
		fmt.Printf("Format: %s\n", format.Format)
		for _, size := range format.Sizes {
			fmt.Printf("  Size: %dx%d\n", size.Width, size.Height)
			fmt.Printf("    FPS: %v\n", size.FPS)
		}
	}
}

// Function to run the v4l2-ctl command and parse the output
func GetV4L2FormatInfo(devicePath string) ([]FormatInfo, error) {
	// Run the v4l2-ctl command to list formats
	cmd := exec.Command("v4l2-ctl", "--list-formats-ext", "-d", devicePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	// Parse the output
	var formats []FormatInfo
	var currentFormat *FormatInfo
	var currentSize *SizeInfo
	scanner := bufio.NewScanner(&out)

	formatRegex := regexp.MustCompile(`\[(\d+)\]: '(\S+)'`)
	sizeRegex := regexp.MustCompile(`Size: Discrete (\d+)x(\d+)`)
	intervalRegex := regexp.MustCompile(`Interval: Discrete (\d+\.\d+)s \((\d+\.\d+) fps\)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Match format line
		if matches := formatRegex.FindStringSubmatch(line); matches != nil {
			if currentFormat != nil {
				formats = append(formats, *currentFormat)
			}
			// Start a new format entry
			currentFormat = &FormatInfo{
				Format: matches[2],
			}
			currentSize = nil
			continue
		}

		// Match size line
		if matches := sizeRegex.FindStringSubmatch(line); matches != nil && currentFormat != nil {
			width, _ := strconv.Atoi(matches[1])
			height, _ := strconv.Atoi(matches[2])

			// Add a new size entry to the current format and track it
			currentFormat.Sizes = append(currentFormat.Sizes, SizeInfo{
				Width:  width,
				Height: height,
			})
			currentSize = &currentFormat.Sizes[len(currentFormat.Sizes)-1]
			continue
		}

		// Match FPS interval line for the current size
		if matches := intervalRegex.FindStringSubmatch(line); matches != nil && currentSize != nil {
			fps, _ := strconv.ParseFloat(matches[2], 32)
			currentSize.FPS = append(currentSize.FPS, int(fps))
		}
	}

	// Append the last format if present
	if currentFormat != nil {
		formats = append(formats, *currentFormat)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return formats, nil
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

// CaptureScreenshot captures a single frame from the video device and returns it as JPEG
// This function can be called even if the device is not currently streaming
// If the device is already capturing, it will use the current stream
// If not, it will temporarily open the device, capture one frame, and close it
func (i *Instance) CaptureScreenshot() ([]byte, error) {
	// If device is already capturing, grab a frame from the current stream
	if i.Capturing && i.frames_buff != nil {
		// Flush the first 5 frames as they may be blank/invalid
		for j := 0; j < 5; j++ {
			select {
			case <-i.frames_buff:
				// Discard this frame
			case <-time.After(1 * time.Second):
				// Timeout flushing, continue
			}
		}
		select {
		case frame := <-i.frames_buff:
			if len(frame) > 0 && isJPEG(frame) {
				return frame, nil
			}
			return nil, fmt.Errorf("invalid frame received from stream")
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("timeout waiting for frame from active stream")
		}
	}

	// Device is not capturing, we need to temporarily open it
	devName := i.Config.VideoDeviceName

	// Check if the video device is a capture device
	isCaptureDev, err := CheckVideoCaptureDevice(devName)
	if err != nil {
		return nil, fmt.Errorf("failed to check video device: %w", err)
	}
	if !isCaptureDev {
		return nil, fmt.Errorf("device %s is not a video capture device", devName)
	}

	// Get supported resolutions and pick the first available one
	formats, err := GetV4L2FormatInfo(devName)
	if err != nil || len(formats) == 0 {
		return nil, fmt.Errorf("failed to get device formats: %w", err)
	}

	// Find the first MJPEG format with a valid resolution
	var width, height, fps int
	found := false
	for _, format := range formats {
		if strings.ToLower(format.Format) == "mjpeg" && len(format.Sizes) > 0 {
			// Pick the first available size
			size := format.Sizes[0]
			width = size.Width
			height = size.Height
			if len(size.FPS) > 0 {
				fps = size.FPS[0]
			} else {
				fps = 15 // Default fallback
			}
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("no MJPEG format found for device")
	}

	// Open the device temporarily with buffer for multiple frames
	camera, err := device.Open(devName,
		device.WithIOType(v4l2.IOTypeMMAP),
		device.WithPixFormat(v4l2.PixFormat{
			PixelFormat: v4l2.PixelFmtMJPEG,
			Width:       uint32(width),
			Height:      uint32(height),
			Field:       v4l2.FieldAny,
		}),
		device.WithFPS(uint32(fps)),
		device.WithBufferSize(2), // Buffer size for frame capture
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open video device for screenshot: %w", err)
	}
	defer camera.Close()

	// Start capture
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := camera.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start capture: %w", err)
	}

	// Get frames channel
	frames := camera.GetOutput()

	// Flush the first 5 frames as they may be blank/invalid
	log.Printf("Flushing first 5 frames from newly opened device...")
	for i := 0; i < 5; i++ {
		select {
		case <-frames:
			// Discard this frame
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout flushing initial frames")
		}
	}

	// Now get the actual screenshot frame
	select {
	case frame := <-frames:
		if len(frame) > 0 && isJPEG(frame) {
			log.Printf("Screenshot captured successfully from device")
			return frame, nil
		}
		return nil, fmt.Errorf("invalid frame captured")
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout capturing screenshot")
	}
}

// ServeScreenshot serves a single JPEG screenshot via HTTP
// This is useful for preview thumbnails in the web UI
func (i *Instance) ServeScreenshot(w http.ResponseWriter, req *http.Request) {
	// Capture screenshot
	screenshot, err := i.CaptureScreenshot()
	if err != nil {
		log.Printf("Failed to capture screenshot: %v", err)

		// Return a placeholder error image
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusInternalServerError)

		// Create a simple error placeholder image
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		// Fill with dark gray
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				img.Set(x, y, image.Black)
			}
		}

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err == nil {
			w.Write(buf.Bytes())
		}
		return
	}

	// Serve the screenshot
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", fmt.Sprint(len(screenshot)))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	w.Write(screenshot)
}
