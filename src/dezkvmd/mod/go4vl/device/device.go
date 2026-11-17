package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	sys "syscall"
	"time"

	"github.com/vladimirvivien/go4vl/v4l2"
)

type Device struct {
	path         string
	file         *os.File
	fd           uintptr
	config       config
	bufType      v4l2.BufType
	cap          v4l2.Capability
	cropCap      v4l2.CropCapability
	buffers      [][]byte
	requestedBuf v4l2.RequestBuffers
	streaming    bool
	output       chan []byte

	// DezKVM patch: lifecycle of the capture loop
	stopMu    sync.Mutex         // serialises Stop/Close
	cancel    context.CancelFunc // cancels the capture loop
	loopDone  chan struct{}      // closed when the capture loop has exited
	errMu     sync.Mutex
	streamErr error // why the capture loop ended on its own, if it did
}

// Open creates opens the underlying device at specified path for streaming.
// It returns a *Device or an error if unable to open device.
func Open(path string, options ...Option) (*Device, error) {
	fd, err := v4l2.OpenDevice(path, sys.O_RDWR|sys.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("device open: %w", err)
	}

	dev := &Device{path: path, config: config{}, fd: fd}
	// apply options
	if len(options) > 0 {
		for _, o := range options {
			o(&dev.config)
		}
	}

	// get capability
	cap, err := v4l2.GetCapability(dev.fd)
	if err != nil {
		if err := v4l2.CloseDevice(dev.fd); err != nil {
			return nil, fmt.Errorf("device %s: closing after failure: %s", path, err)
		}
		return nil, fmt.Errorf("device open: %s: %w", path, err)
	}
	dev.cap = cap

	// set preferred device buffer size
	if reflect.ValueOf(dev.config.bufSize).IsZero() {
		dev.config.bufSize = 2
	}

	// only supports streaming IO model right now
	if !dev.cap.IsStreamingSupported() {
		return nil, fmt.Errorf("device open: device does not support streamingIO")
	}

	switch {
	case cap.IsVideoCaptureSupported():
		// setup capture parameters and chan for captured data
		dev.bufType = v4l2.BufTypeVideoCapture
		dev.output = make(chan []byte, dev.config.bufSize)
	case cap.IsVideoOutputSupported():
		dev.bufType = v4l2.BufTypeVideoOutput
	default:
		if err := v4l2.CloseDevice(dev.fd); err != nil {
			return nil, fmt.Errorf("device open: %s: closing after failure: %s", path, err)
		}
		return nil, fmt.Errorf("device open: %s: %w", path, v4l2.ErrorUnsupportedFeature)
	}

	if !reflect.ValueOf(dev.config.bufType).IsZero() && dev.config.bufType != dev.bufType {
		return nil, fmt.Errorf("device open: does not support buffer stream type")
	}

	// ensures IOType is set, only MemMap supported now
	dev.config.ioType = v4l2.IOTypeMMAP

	// reset crop, only if cropping supported
	if cropcap, err := v4l2.GetCropCapability(dev.fd, dev.bufType); err == nil {
		if err := v4l2.SetCropRect(dev.fd, cropcap.DefaultRect); err != nil {
			// ignore errors
		}
	}

	// set pix format
	if !reflect.ValueOf(dev.config.pixFormat).IsZero() {
		if err := dev.SetPixFormat(dev.config.pixFormat); err != nil {
			return nil, fmt.Errorf("device open: %s: set format: %w", path, err)
		}
	} else {
		dev.config.pixFormat, err = v4l2.GetPixFormat(dev.fd)
		if err != nil {
			return nil, fmt.Errorf("device open: %s: get default format: %w", path, err)
		}
	}

	// set fps
	if !reflect.ValueOf(dev.config.fps).IsZero() {
		if err := dev.SetFrameRate(dev.config.fps); err != nil {
			return nil, fmt.Errorf("device open: %s: set fps: %w", path, err)
		}
	} else {
		if dev.config.fps, err = dev.GetFrameRate(); err != nil {
			return nil, fmt.Errorf("device open: %s: get fps: %w", path, err)
		}
	}

	return dev, nil
}

// Close closes the underlying device associated with `d` .
func (d *Device) Close() error {
	// DezKVM patch: always release the fd. When the device has been
	// unplugged Stop() reports errors (stream off / unmap on a dead device)
	// that must not stop us from closing the file descriptor.
	_ = d.Stop()
	return v4l2.CloseDevice(d.fd)
}

// Done returns a channel that is closed when the capture loop has exited,
// either because it was stopped or because the device failed. It returns nil
// before Start has been called. Use StreamError to find out why it ended.
//
// DezKVM patch.
func (d *Device) Done() <-chan struct{} {
	return d.loopDone
}

// StreamError returns the error that made the capture loop stop on its own
// (for example the device was unplugged), or nil if it is still running or
// was stopped normally.
//
// DezKVM patch.
func (d *Device) StreamError() error {
	d.errMu.Lock()
	defer d.errMu.Unlock()
	return d.streamErr
}

func (d *Device) setStreamError(err error) {
	d.errMu.Lock()
	d.streamErr = err
	d.errMu.Unlock()
}

// Name returns the device name (or path)
func (d *Device) Name() string {
	return d.path
}

// Fd returns the file descriptor value for the device
func (d *Device) Fd() uintptr {
	return d.fd
}

// Buffers returns the internal mapped buffers. This method should be
// called after streaming has been started otherwise it may return nil.
func (d *Device) Buffers() [][]byte {
	return d.buffers
}

// Capability returns device capability info.
func (d *Device) Capability() v4l2.Capability {
	return d.cap
}

// BufferType this is a convenience method that returns the device mode (i.e. Capture, Output, etc)
// Use method Capability for detail about the device.
func (d *Device) BufferType() v4l2.BufType {
	return d.bufType
}

// BufferCount returns configured number of buffers to be used during streaming.
// If called after streaming start, this value could be updated by the driver.
func (d *Device) BufferCount() v4l2.BufType {
	return d.config.bufSize
}

// MemIOType returns the device memory input/output type (i.e. Memory mapped, DMA, user pointer, etc)
func (d *Device) MemIOType() v4l2.IOType {
	return d.config.ioType
}

// GetOutput returns the channel that outputs streamed data that is
// captured from the underlying device driver.
func (d *Device) GetOutput() <-chan []byte {
	return d.output
}

// SetInput sets up an input channel for data this sent for output to the
// underlying device driver.
func (d *Device) SetInput(in <-chan []byte) {

}

// GetCropCapability returns cropping info for device
func (d *Device) GetCropCapability() (v4l2.CropCapability, error) {
	if !d.cap.IsVideoCaptureSupported() {
		return v4l2.CropCapability{}, v4l2.ErrorUnsupportedFeature
	}
	return d.cropCap, nil
}

// SetCropRect crops the video dimension for the device
func (d *Device) SetCropRect(r v4l2.Rect) error {
	if !d.cap.IsVideoCaptureSupported() {
		return v4l2.ErrorUnsupportedFeature
	}
	if err := v4l2.SetCropRect(d.fd, r); err != nil {
		return fmt.Errorf("device: %w", err)
	}
	return nil
}

// GetPixFormat retrieves pixel format info for device
func (d *Device) GetPixFormat() (v4l2.PixFormat, error) {
	if !d.cap.IsVideoCaptureSupported() {
		return v4l2.PixFormat{}, v4l2.ErrorUnsupportedFeature
	}

	if reflect.ValueOf(d.config.pixFormat).IsZero() {
		pixFmt, err := v4l2.GetPixFormat(d.fd)
		if err != nil {
			return v4l2.PixFormat{}, fmt.Errorf("device: %w", err)
		}
		d.config.pixFormat = pixFmt
	}

	return d.config.pixFormat, nil
}

// SetPixFormat sets the pixel format for the associated device.
func (d *Device) SetPixFormat(pixFmt v4l2.PixFormat) error {
	if !d.cap.IsVideoCaptureSupported() {
		return v4l2.ErrorUnsupportedFeature
	}

	if err := v4l2.SetPixFormat(d.fd, pixFmt); err != nil {
		return fmt.Errorf("device: %w", err)
	}
	d.config.pixFormat = pixFmt
	return nil
}

// GetFormatDescription returns a format description for the device at specified format index
func (d *Device) GetFormatDescription(idx uint32) (v4l2.FormatDescription, error) {
	if !d.cap.IsVideoCaptureSupported() {
		return v4l2.FormatDescription{}, v4l2.ErrorUnsupportedFeature
	}

	return v4l2.GetFormatDescription(d.fd, idx)
}

// GetFormatDescriptions returns all possible format descriptions for device
func (d *Device) GetFormatDescriptions() ([]v4l2.FormatDescription, error) {
	if !d.cap.IsVideoCaptureSupported() {
		return nil, v4l2.ErrorUnsupportedFeature
	}

	return v4l2.GetAllFormatDescriptions(d.fd)
}

// GetVideoInputIndex returns current video input index for device
func (d *Device) GetVideoInputIndex() (int32, error) {
	if !d.cap.IsVideoCaptureSupported() {
		return 0, v4l2.ErrorUnsupportedFeature
	}

	return v4l2.GetCurrentVideoInputIndex(d.fd)
}

// GetVideoInputInfo returns video input info for device
func (d *Device) GetVideoInputInfo(index uint32) (v4l2.InputInfo, error) {
	if !d.cap.IsVideoCaptureSupported() {
		return v4l2.InputInfo{}, v4l2.ErrorUnsupportedFeature
	}

	return v4l2.GetVideoInputInfo(d.fd, index)
}

// GetStreamParam returns streaming parameter information for device
func (d *Device) GetStreamParam() (v4l2.StreamParam, error) {
	if !d.cap.IsVideoCaptureSupported() && d.cap.IsVideoOutputSupported() {
		return v4l2.StreamParam{}, v4l2.ErrorUnsupportedFeature
	}
	return v4l2.GetStreamParam(d.fd, d.bufType)
}

// SetStreamParam saves stream parameters for device
func (d *Device) SetStreamParam(param v4l2.StreamParam) error {
	if !d.cap.IsVideoCaptureSupported() && d.cap.IsVideoOutputSupported() {
		return v4l2.ErrorUnsupportedFeature
	}
	return v4l2.SetStreamParam(d.fd, d.bufType, param)
}

// SetFrameRate sets the FPS rate value of the device
func (d *Device) SetFrameRate(fps uint32) error {
	if !d.cap.IsStreamingSupported() {
		return fmt.Errorf("set frame rate: %w", v4l2.ErrorUnsupportedFeature)
	}

	var param v4l2.StreamParam
	switch {
	case d.cap.IsVideoCaptureSupported():
		param.Capture = v4l2.CaptureParam{TimePerFrame: v4l2.Fract{Numerator: 1, Denominator: fps}}
	case d.cap.IsVideoOutputSupported():
		param.Output = v4l2.OutputParam{TimePerFrame: v4l2.Fract{Numerator: 1, Denominator: fps}}
	default:
		return v4l2.ErrorUnsupportedFeature
	}
	if err := d.SetStreamParam(param); err != nil {
		return fmt.Errorf("device: set fps: %w", err)
	}
	d.config.fps = fps
	return nil
}

// GetFrameRate returns the FPS value for the device
func (d *Device) GetFrameRate() (uint32, error) {
	if reflect.ValueOf(d.config.fps).IsZero() {
		param, err := d.GetStreamParam()
		if err != nil {
			return 0, fmt.Errorf("device: frame rate: %w", err)
		}
		switch {
		case d.cap.IsVideoCaptureSupported():
			d.config.fps = param.Capture.TimePerFrame.Denominator
		case d.cap.IsVideoOutputSupported():
			d.config.fps = param.Output.TimePerFrame.Denominator
		default:
			return 0, v4l2.ErrorUnsupportedFeature
		}
	}

	return d.config.fps, nil
}

// GetMediaInfo returns info for a device that supports the Media API
func (d *Device) GetMediaInfo() (v4l2.MediaDeviceInfo, error) {
	return v4l2.GetMediaDeviceInfo(d.fd)
}

func (d *Device) Start(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if !d.cap.IsStreamingSupported() {
		return fmt.Errorf("device: start stream: %s", v4l2.ErrorUnsupportedFeature)
	}

	if d.streaming {
		return fmt.Errorf("device: stream already started")
	}

	// allocate device buffers
	bufReq, err := v4l2.InitBuffers(d)
	if err != nil {
		return fmt.Errorf("device: requested buffer type not be supported: %w", err)
	}

	d.config.bufSize = bufReq.Count
	d.requestedBuf = bufReq

	// for each allocated device buf, map into local space
	if d.buffers, err = v4l2.MapMemoryBuffers(d); err != nil {
		return fmt.Errorf("device: make mapped buffers: %s", err)
	}

	if err := d.startStreamLoop(ctx); err != nil {
		return fmt.Errorf("device: start stream loop: %s", err)
	}

	d.streaming = true

	return nil
}

// Stop stops the capture loop and the stream, and unmaps the buffers. It is
// safe to call more than once and from several goroutines.
//
// DezKVM patch: the capture loop is stopped and waited for *before* the
// buffers are unmapped. The original code unmapped while the loop could still
// be copying from them, which crashes with SIGSEGV.
func (d *Device) Stop() error {
	d.stopMu.Lock()
	defer d.stopMu.Unlock()

	if !d.streaming {
		return nil
	}
	d.streaming = false

	if d.cancel != nil {
		d.cancel()
	}
	if d.loopDone != nil {
		<-d.loopDone // the loop only blocks in cancellable waits
	}

	// Try both steps even if the first fails (device may be unplugged).
	streamOffErr := v4l2.StreamOff(d)
	unmapErr := v4l2.UnmapMemoryBuffers(d)
	if streamOffErr != nil {
		return fmt.Errorf("device: stop: %w", streamOffErr)
	}
	if unmapErr != nil {
		return fmt.Errorf("device: stop: %w", unmapErr)
	}
	return nil
}

// Capture loop tuning (DezKVM patch)
const (
	// how long one wait for a frame may block before the loop re-checks for cancellation
	capturePollInterval = 500 * time.Millisecond
	// how many dequeue errors in a row are tolerated before the device is declared dead
	captureMaxConsecutiveErrors = 30
	// pause between retries after an error
	captureErrorBackoff = 20 * time.Millisecond
)

// startStreamLoop queues the buffers, turns the stream on and starts the capture
// loop in its own goroutine. It returns as soon as the loop is running.
//
// DezKVM patch: the upstream loop panicked on any dequeue error, which killed
// the whole process whenever a USB capture card glitched or was unplugged.
// This version never panics. Transient errors are retried, and a dead device
// ends the loop cleanly (output channel closed, Done() fires, StreamError()
// says why) so the caller can recover.
func (d *Device) startStreamLoop(ctx context.Context) error {
	// Initial enqueue of buffers for capture
	for i := 0; i < int(d.config.bufSize); i++ {
		_, err := v4l2.QueueBuffer(d.fd, d.config.ioType, d.bufType, uint32(i))
		if err != nil {
			return fmt.Errorf("device: buffer queueing: %w", err)
		}
	}

	if err := v4l2.StreamOn(d); err != nil {
		return fmt.Errorf("device: stream on: %w", err)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.loopDone = make(chan struct{})
	d.setStreamError(nil)
	go d.captureLoop(loopCtx)

	return nil
}

// isDeviceGone reports errors after which retrying is pointless.
func isDeviceGone(err error) bool {
	return errors.Is(err, sys.ENODEV) || errors.Is(err, sys.ENXIO) || errors.Is(err, sys.EBADF)
}

func (d *Device) captureLoop(ctx context.Context) {
	defer close(d.loopDone)
	defer close(d.output)

	fd := d.Fd()
	ioMemType := d.MemIOType()
	bufType := d.BufferType()

	consecutiveErrors := 0
	// fail records an error and reports whether the loop must give up.
	fail := func(what string, err error) bool {
		consecutiveErrors++
		if isDeviceGone(err) || consecutiveErrors >= captureMaxConsecutiveErrors {
			d.setStreamError(fmt.Errorf("device: capture loop: %s: %w (%d consecutive errors)", what, err, consecutiveErrors))
			return true
		}
		select {
		case <-ctx.Done():
			return true
		case <-time.After(captureErrorBackoff):
			return false
		}
	}

	for ctx.Err() == nil {
		ready, err := v4l2.WaitReadable(fd, capturePollInterval)
		if err != nil {
			if fail("wait for frame", err) {
				return
			}
			continue
		}
		if !ready {
			continue
		}

		buff, err := v4l2.DequeueBuffer(fd, ioMemType, bufType)
		if err != nil {
			if errors.Is(err, sys.EAGAIN) {
				continue
			}
			if fail("buffer dequeue", err) {
				return
			}
			continue
		}
		consecutiveErrors = 0

		// Copy the frame out of the mapped buffer, then hand the buffer straight back to the
		// driver so a slow consumer never starves the capture card of free buffers.
		frame := []byte{}
		if buff.Flags&v4l2.BufFlagMapped != 0 && buff.Flags&v4l2.BufFlagError == 0 && int(buff.Index) < len(d.buffers) {
			mapped := d.buffers[buff.Index]
			n := int(buff.BytesUsed)
			if n > len(mapped) {
				n = len(mapped)
			}
			frame = make([]byte, n)
			copy(frame, mapped[:n])
		}

		if _, err := v4l2.QueueBuffer(fd, ioMemType, bufType, buff.Index); err != nil {
			// The buffer is lost, so the stream can no longer run reliably.
			d.setStreamError(fmt.Errorf("device: capture loop: buffer queue (index %d, flags %#x, bytes %d, seq %d): %w",
				buff.Index, buff.Flags, buff.BytesUsed, buff.Sequence, err))
			return
		}

		select {
		case d.output <- frame:
		case <-ctx.Done():
			return
		}
	}
}
