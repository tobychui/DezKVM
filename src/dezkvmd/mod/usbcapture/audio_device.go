package usbcapture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// upgrader is used to upgrade HTTP connections to WebSocket connections
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ListCaptureDevices lists all available audio capture devices in the /dev/snd directory.
func ListCaptureDevices() ([]string, error) {
	files, err := os.ReadDir("/dev/snd")
	if err != nil {
		return nil, fmt.Errorf("failed to read /dev/snd: %w", err)
	}

	var captureDevs []string
	for _, file := range files {
		name := file.Name()
		if strings.HasPrefix(name, "pcm") && strings.HasSuffix(name, "c") {
			fullPath := "/dev/snd/" + name
			captureDevs = append(captureDevs, fullPath)
		}
	}

	return captureDevs, nil
}

// FindHDMICaptureCard searches for an HDMI capture card using the `arecord -l` command.
func FindHDMICapturePCMPath() (string, error) {
	out, err := exec.Command("arecord", "-l").Output()
	if err != nil {
		return "", fmt.Errorf("arecord -l failed: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "ms2109") || strings.Contains(lower, "ms2130") {
			// Example line:
			// card 1: MS2109 [MS2109], device 0: USB Audio [USB Audio]
			parts := strings.Fields(line)
			var cardNum, devNum string
			for i := range parts {
				if parts[i] == "card" && i+1 < len(parts) {
					cardNum = parts[i+1][:1] // "1"
				}
				if parts[i] == "device" && i+1 < len(parts) {
					devNum = strings.TrimSuffix(parts[i+1], ":") // "0"
				}
			}

			if cardNum != "" && devNum != "" {
				return fmt.Sprintf("/dev/snd/pcmC%vD%vc", cardNum, devNum), nil
			}
		}
	}

	return "", fmt.Errorf("no HDMI capture card found")
}

// Convert a PCM device name to a hardware device name.
// Example: "pcmC1D0c" -> "hw:1,0"
func pcmDeviceToHW(dev string) (string, error) {
	// Regex to extract card and device numbers
	re := regexp.MustCompile(`pcmC(\d+)D(\d+)[cp]`)
	matches := re.FindStringSubmatch(dev)
	if len(matches) < 3 {
		return "", fmt.Errorf("invalid device format")
	}
	card := matches[1]
	device := matches[2]
	return fmt.Sprintf("hw:%s,%s", card, device), nil
}

func GetDefaultAudioConfig() *AudioConfig {
	return &AudioConfig{
		SampleRate:     48000,
		Channels:       2,
		BytesPerSample: 2,    // 16-bit
		FrameSize:      1920, // 1920 samples per frame = 40ms @ 48kHz
	}
}

func GetDefaultAudioDevice() string {
	//Check if the default ALSA device exists
	if _, err := os.Stat("/dev/snd/pcmC0D0c"); err == nil {
		return "/dev/snd/pcmC0D0c"
	}

	//If not, list all capture devices and return the first one
	devs, err := ListCaptureDevices()
	if err != nil || len(devs) == 0 {
		return ""
	}

	return devs[0]
}

// AudioFormat returns the PCM format delivered by StreamAudioTo: signed
// 16-bit little-endian, interleaved, at this sample rate and channel count.
func (i *Instance) AudioFormat() (sampleRate int, channels int) {
	cfg := i.Config.AudioConfig
	if cfg == nil {
		cfg = GetDefaultAudioConfig()
	}
	return cfg.SampleRate, cfg.Channels
}

// resolveAudioDevice returns the ALSA PCM device file and its hw:X,Y name.
func (i *Instance) resolveAudioDevice() (pcmdev string, hwdev string, err error) {
	pcmdev = i.Config.AudioDeviceName
	if pcmdev == "" {
		// Try finding the HDMI capture card automatically
		pcmdev, err = FindHDMICapturePCMPath()
		if err != nil {
			return "", "", fmt.Errorf("failed to find HDMI capture PCM path: %w", err)
		}
	}
	hwdev, err = pcmDeviceToHW(pcmdev)
	if err != nil {
		return "", "", fmt.Errorf("failed to convert PCM device %s to hardware device: %w", pcmdev, err)
	}
	return pcmdev, hwdev, nil
}

// StreamAudioTo captures PCM audio from the capture card (see AudioFormat)
// and delivers it to fn, in whole sample frames, until ctx is cancelled, fn
// returns an error or another client takes over the audio. fn must not keep
// the slice after it returns.
//
// The ALSA capture device can only be opened once, so like the video stream
// there is a single audio consumer: starting a new one (audio websocket or
// a WebRTC session carrying audio) stops the previous one.
func (i *Instance) StreamAudioTo(ctx context.Context, fn func(pcm []byte) error) error {
	if ctx.Err() != nil {
		return nil // already cancelled: do not take the audio from anyone
	}
	consumer, tookOver := i.beginAudioConsumer()
	defer i.endAudioConsumer(consumer)
	if tookOver {
		log.Println("Another client is already receiving audio, taking over the audio stream...")
	}

	pcmdev, hwdev, err := i.resolveAudioDevice()
	if err != nil {
		return err
	}

	// The previous consumer's arecord was killed and reaped above, but the
	// device may still be held for a moment (or by another program).
	for retry := 0; alsa_device_occupied(pcmdev) && retry < 10; retry++ {
		select {
		case <-ctx.Done():
			return nil
		case <-consumer.takeover:
			return ErrAudioTakenOver
		case <-time.After(250 * time.Millisecond):
		}
	}

	sampleRate, channels := i.AudioFormat()
	bytesPerSample := 2 // S16_LE
	log.Printf("Starting audio capture on %s (%s), %d Hz, %d channel(s)\n", pcmdev, hwdev, sampleRate, channels)

	cmd := exec.Command("arecord",
		"-q",
		"-t", "raw", // headerless PCM (the default WAV header would be played as noise)
		"-f", "S16_LE", // Format: 16-bit little-endian
		"-r", fmt.Sprint(sampleRate),
		"-c", fmt.Sprint(channels),
		"-D", hwdev, // Use the hardware device
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get arecord stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start arecord: %w", err)
	}

	// Kill arecord as soon as the consumer is told to stop, which also
	// unblocks the read below.
	readDone := make(chan struct{})
	stopReason := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			stopReason <- nil
		case <-consumer.takeover:
			stopReason <- ErrAudioTakenOver
		case <-readDone:
			return
		}
		cmd.Process.Kill()
	}()

	i.audioStreaming.Store(true)
	defer i.audioStreaming.Store(false)

	frameBytes := channels * bytesPerSample
	// Up to 2 x FrameSize samples per read (80 ms at the default 48 kHz)
	frameSize := 0
	if i.Config.AudioConfig != nil {
		frameSize = i.Config.AudioConfig.FrameSize
	}
	if frameSize < 480 {
		frameSize = 1920
	}
	buf := make([]byte, frameSize*frameBytes*2)
	pending := 0 // bytes of an incomplete sample frame carried to the next read

	var result error
	for {
		n, readErr := stdout.Read(buf[pending:])
		total := pending + n
		aligned := total - total%frameBytes
		if aligned > 0 {
			if err := fn(buf[:aligned]); err != nil {
				result = err
				break
			}
		}
		pending = copy(buf, buf[aligned:total])

		if readErr != nil {
			select {
			case reason := <-stopReason:
				result = reason // stopped on purpose
			default:
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = readErr.Error()
				}
				result = fmt.Errorf("audio capture ended: %s", msg)
			}
			break
		}
	}

	close(readDone)
	cmd.Process.Kill()
	cmd.Wait() // reap arecord so the ALSA device is released
	log.Println("Audio capture stopped")
	return result
}

// AudioStreamingHandler streams raw PCM audio over a WebSocket. The
// ?quality= parameter selects the sample rate sent to the browser: low
// (16 kHz), standard (24 kHz, default) or high (48 kHz).
func (i *Instance) AudioStreamingHandler(w http.ResponseWriter, r *http.Request) {
	quality := r.URL.Query().Get("quality")
	selectedQuality := "standard"
	for _, q := range []string{"low", "standard", "high"} {
		if quality == q {
			selectedQuality = q
			break
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade to websocket:", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// The client sends "exit" when it stops listening; a read error means
	// it went away. Either way stop capturing.
	go func() {
		defer cancel()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(msg) == "exit" {
				log.Println("Received exit command from audio client")
				return
			}
		}
	}()

	err = i.StreamAudioTo(ctx, func(pcm []byte) error {
		switch selectedQuality {
		case "standard":
			pcm = downsample48kTo24kStereo(pcm) // Downsample to 24kHz stereo
		case "low":
			pcm = downsample48kTo16kStereo(pcm) // Downsample to 16kHz stereo
		}
		return conn.WriteMessage(websocket.BinaryMessage, pcm)
	})
	if err != nil {
		log.Println("Audio websocket stream ended:", err)
	}
}

// StopAudioStreaming stops the active audio consumer (websocket or WebRTC)
// and waits for the capture to be released.
func (i *Instance) StopAudioStreaming() {
	i.audioMu.Lock()
	consumer := i.activeAudioConsumer
	i.audioMu.Unlock()
	if consumer == nil {
		return // not streaming
	}

	consumer.stop()
	select {
	case <-consumer.done:
	case <-time.After(3 * time.Second):
		log.Println("Warning: Audio streaming did not stop in time")
	}
}

// ErrAudioTakenOver is returned by StreamAudioTo when another client takes
// over the audio stream (or StopAudioStreaming is called).
var ErrAudioTakenOver = errors.New("audio stream taken over by another client")

type audioConsumer struct {
	takeover chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

func (c *audioConsumer) stop() {
	c.stopOnce.Do(func() { close(c.takeover) })
}

func (i *Instance) beginAudioConsumer() (*audioConsumer, bool) {
	consumer := &audioConsumer{
		takeover: make(chan struct{}),
		done:     make(chan struct{}),
	}

	i.audioMu.Lock()
	previous := i.activeAudioConsumer
	i.activeAudioConsumer = consumer
	i.audioMu.Unlock()

	if previous != nil {
		previous.stop()
		select {
		case <-previous.done:
		case <-time.After(3 * time.Second):
			log.Println("Previous audio consumer did not exit before timeout, continuing takeover...")
		}
	}
	return consumer, previous != nil
}

func (i *Instance) endAudioConsumer(consumer *audioConsumer) {
	i.audioMu.Lock()
	if i.activeAudioConsumer == consumer {
		i.activeAudioConsumer = nil
	}
	i.audioMu.Unlock()
	close(consumer.done)
}

// Downsample48kTo24kStereo downsamples a 48kHz stereo audio buffer to 24kHz.
// It assumes the input buffer is in 16-bit stereo format (2 bytes per channel).
// The output buffer will also be in 16-bit stereo format.
func downsample48kTo24kStereo(buf []byte) []byte {
	const frameSize = 4 // 2 bytes per channel × 2 channels
	if len(buf)%frameSize != 0 {
		// Trim incomplete frame (rare case)
		buf = buf[:len(buf)-len(buf)%frameSize]
	}

	out := make([]byte, 0, len(buf)/2)

	for i := 0; i < len(buf); i += frameSize * 2 {
		// Copy every other frame (drop 1 in 2)
		if i+frameSize <= len(buf) {
			out = append(out, buf[i:i+frameSize]...)
		}
	}

	return out
}

// Downsample48kTo16kStereo downsamples a 48kHz stereo audio buffer to 16kHz.
// It assumes the input buffer is in 16-bit stereo format (2 bytes per channel).
// The output buffer will also be in 16-bit stereo format.
func downsample48kTo16kStereo(buf []byte) []byte {
	const frameSize = 4 // 2 bytes per channel × 2 channels
	if len(buf)%frameSize != 0 {
		// Trim incomplete frame (rare case)
		buf = buf[:len(buf)-len(buf)%frameSize]
	}

	out := make([]byte, 0, len(buf)/3)

	for i := 0; i < len(buf); i += frameSize * 3 {
		// Copy every third frame (drop 2 in 3)
		if i+frameSize <= len(buf) {
			out = append(out, buf[i:i+frameSize]...)
		}
	}

	return out
}

func alsa_device_occupied(dev string) bool {
	f, err := os.OpenFile(dev, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		//result <- true // Occupied or cannot open
		return true
	}
	f.Close()
	return false
}
