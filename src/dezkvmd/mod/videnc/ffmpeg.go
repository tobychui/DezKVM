package videnc

/*
	ffmpeg.go

	Shared ffmpeg child-process runner used by every backend, plus the
	per-backend argument builders. The process reads MJPEG from stdin,
	decodes it to YUV (software decode inside ffmpeg) and emits a raw
	H.264 Annex-B stream on stdout tuned for low latency:

	  - no B-frames (WebRTC/live decode friendly)
	  - 1 second GOP so a joining/recovering client resyncs quickly
	  - repeated SPS/PPS headers on every keyframe
*/

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const defaultRenderDevice = "/dev/dri/renderD128"

// commonInputArgs are the low latency input options shared by all backends.
func commonInputArgs(cfg Config) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-f", "mjpeg",
		"-framerate", strconv.Itoa(cfg.FPS),
		"-i", "pipe:0",
	}
}

// withInputDecoder inserts "-c:v decoder" before the "-i" of input args.
func withInputDecoder(args []string, decoder string) []string {
	out := make([]string, 0, len(args)+2)
	for _, a := range args {
		if a == "-i" {
			out = append(out, "-c:v", decoder)
		}
		out = append(out, a)
	}
	return out
}

// limitedRange returns a filter that converts the decoded capture frames to
// limited ("TV", 16-235) range in pixFmt.
//
// Capture cards send full range (0-255) JPEG, and ffmpeg keeps that range
// and flags the H.264 stream as full range. Firefox honours the flag, but
// Chrome and Edge show WebRTC H.264 as limited range regardless: shadows
// crush to black and highlights blow out to flat white (6% of a typical
// video frame clipped to black). Limited range is what every player
// expects. Point sampling keeps the conversion at ~25 ms CPU per 1080p
// frame on a Cortex-A53, it only affects how 4:2:2 chroma is halved.
func limitedRange(pixFmt string) string {
	return "scale=out_range=tv:flags=neighbor,format=" + pixFmt
}

// commonOutputArgs are the raw H.264 output options shared by all backends.
func commonOutputArgs() []string {
	return []string{
		// Encode every frame as it arrives. With nobuffer + a 32 byte probe
		// the mjpeg demuxer stamps frames 1 tick apart in a 1/1200000 time
		// base, so the default constant frame rate mode treats them as
		// duplicates and drops all but ~2 frames of the whole session.
		"-fps_mode", "passthrough",
		"-an",
		"-f", "h264",
		"pipe:1",
	}
}

// buildIntelVAAPIArgs builds the ffmpeg arguments for the Intel iGPU backend.
// The MJPEG frames are software-decoded, uploaded to the GPU as NV12 and
// encoded by the fixed-function encoder (Quick Sync).
func buildIntelVAAPIArgs(cfg Config) []string {
	bitrate := fmt.Sprintf("%dk", cfg.BitrateKbps)
	args := commonInputArgs(cfg)
	args = append(args,
		"-vaapi_device", cfg.RenderDevice,
		"-vf", limitedRange("nv12")+",hwupload",
		"-c:v", "h264_vaapi",
		"-profile:v", "constrained_baseline",
		"-bf", "0",
		"-g", strconv.Itoa(cfg.FPS),
		"-b:v", bitrate,
		"-maxrate", bitrate,
	)
	return append(args, commonOutputArgs()...)
}

// buildV4L2M2MArgs builds the ffmpeg arguments for kernel V4L2 M2M hardware
// encoders (Raspberry Pi bcm2835-codec and compatible SBC encoders).
func buildV4L2M2MArgs(cfg Config) []string {
	bitrate := fmt.Sprintf("%dk", cfg.BitrateKbps)
	args := commonInputArgs(cfg)
	args = append(args,
		"-vf", limitedRange("yuv420p"),
		"-c:v", "h264_v4l2m2m",
		"-bf", "0",
		"-g", strconv.Itoa(cfg.FPS),
		"-b:v", bitrate,
	)
	return append(args, commonOutputArgs()...)
}

// buildSoftwareArgs builds the ffmpeg arguments for the libx264 CPU fallback.
func buildSoftwareArgs(cfg Config) []string {
	bitrate := fmt.Sprintf("%dk", cfg.BitrateKbps)
	args := commonInputArgs(cfg)
	args = append(args,
		"-vf", limitedRange("yuv420p"),
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-profile:v", "baseline",
		"-bf", "0",
		"-g", strconv.Itoa(cfg.FPS),
		"-b:v", bitrate,
		"-maxrate", bitrate,
		"-bufsize", bitrate,
	)
	return append(args, commonOutputArgs()...)
}

/* ------------------------------------------------------------ runner */

const (
	// stallTimeout is how long frames may go in without any encoded output
	// before the ffmpeg process is considered hung and restarted.
	stallTimeout = 3 * time.Second
	// maxStallRestarts bounds back-to-back restarts that never produce
	// output, so a dead encoder ends the session instead of looping.
	maxStallRestarts = 3
)

// ffmpegEncoder implements Encoder on top of an ffmpeg child process.
//
// The process is supervised: if frames keep going in but nothing comes out
// for stallTimeout (e.g. the Rockchip hardware JPEG decoder hangs on a
// corrupt frame), it is replaced by a fresh process. Output() stays the same
// continuous stream across restarts; the new process starts with SPS/PPS and
// an IDR frame, so the browser's decoder resyncs on its own.
type ffmpegEncoder struct {
	name string
	bin  string // ffmpeg executable (path or name looked up in PATH)
	args []string

	// validateJPEG drops frames that are not well-formed baseline JPEGs
	// before they reach a decoder that cannot cope with them.
	validateJPEG bool

	mu       sync.Mutex
	proc     *ffmpegProc
	outR     *io.PipeReader
	outW     *io.PipeWriter
	started  bool
	closed   bool
	restarts int // consecutive stall restarts without output in between

	lastIn  atomic.Int64 // unix nanos of the last frame written
	lastOut atomic.Int64 // unix nanos of the last encoded output

	droppedFrames atomic.Int64
	lastDropLog   atomic.Int64
	done          chan struct{}
}

// ffmpegProc is one ffmpeg child process.
type ffmpegProc struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    bytes.Buffer
	startedAt time.Time
}

func newFFmpegEncoder(name string, args []string) *ffmpegEncoder {
	return newFFmpegEncoderBin(name, "ffmpeg", args)
}

// newFFmpegEncoderBin is newFFmpegEncoder with a specific ffmpeg binary, for
// backends that need a non-default ffmpeg build.
func newFFmpegEncoderBin(name, bin string, args []string) *ffmpegEncoder {
	return &ffmpegEncoder{name: name, bin: bin, args: args, done: make(chan struct{})}
}

func (e *ffmpegEncoder) Name() string { return e.name }

func (e *ffmpegEncoder) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return fmt.Errorf("encoder already started")
	}
	e.outR, e.outW = io.Pipe()
	if err := e.startProcLocked(); err != nil {
		return err
	}
	e.started = true
	go e.watchdog()
	return nil
}

// startProcLocked launches a new ffmpeg process and makes it current.
// Caller holds e.mu.
func (e *ffmpegEncoder) startProcLocked() error {
	p := &ffmpegProc{cmd: exec.Command(e.bin, e.args...)}
	var err error
	if p.stdin, err = p.cmd.StdinPipe(); err != nil {
		return err
	}
	if p.stdout, err = p.cmd.StdoutPipe(); err != nil {
		return err
	}
	p.cmd.Stderr = &p.stderr
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}
	p.startedAt = time.Now()
	e.proc = p

	// Reap the process when it exits on its own so it never zombifies.
	go func() { _ = p.cmd.Wait() }()
	go e.copyOutput(p)
	return nil
}

// copyOutput forwards one process' stdout into the continuous output pipe.
func (e *ffmpegEncoder) copyOutput(p *ffmpegProc) {
	buf := make([]byte, 64*1024)
	for {
		n, err := p.stdout.Read(buf)
		if n > 0 {
			e.lastOut.Store(time.Now().UnixNano())
			e.mu.Lock()
			e.restarts = 0
			e.mu.Unlock()
			if _, werr := e.outW.Write(buf[:n]); werr != nil {
				return // consumer is gone
			}
		}
		if err != nil {
			e.mu.Lock()
			current := e.proc == p && !e.closed
			e.mu.Unlock()
			if current {
				// ffmpeg exited on its own: end the stream like before
				e.outW.CloseWithError(fmt.Errorf("ffmpeg exited (%s): %s", e.name, e.lastStderr()))
			}
			return
		}
	}
}

// watchdog restarts ffmpeg when frames go in but no output comes out.
func (e *ffmpegEncoder) watchdog() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
		}
		now := time.Now()
		lastIn := time.Unix(0, e.lastIn.Load())
		lastOut := time.Unix(0, e.lastOut.Load())

		e.mu.Lock()
		p := e.proc
		feeding := now.Sub(lastIn) < time.Second
		silentSince := lastOut
		if p.startedAt.After(silentSince) {
			silentSince = p.startedAt
		}
		if e.closed || !feeding || now.Sub(silentSince) < stallTimeout {
			e.mu.Unlock()
			continue
		}
		if e.restarts >= maxStallRestarts {
			e.mu.Unlock()
			log.Printf("Video encoder %s: no output after %d restarts, giving up\n", e.name, maxStallRestarts)
			e.outW.CloseWithError(fmt.Errorf("encoder %s stalled: %s", e.name, e.lastStderr()))
			return
		}
		e.restarts++
		log.Printf("Video encoder %s: no output for %s while receiving frames, restarting ffmpeg (attempt %d/%d). ffmpeg said: %s\n",
			e.name, now.Sub(silentSince).Round(100*time.Millisecond), e.restarts, maxStallRestarts, e.lastStderrLocked())
		_ = p.stdin.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		if err := e.startProcLocked(); err != nil {
			e.mu.Unlock()
			log.Printf("Video encoder %s: restart failed: %v\n", e.name, err)
			e.outW.CloseWithError(err)
			return
		}
		e.mu.Unlock()
	}
}

func (e *ffmpegEncoder) WriteFrame(frame []byte) error {
	if e.validateJPEG {
		if err := checkBaselineJPEG(frame); err != nil {
			e.logDroppedFrame(err)
			return nil
		}
	}

	e.mu.Lock()
	p := e.proc
	closed := e.closed
	e.mu.Unlock()
	if closed || p == nil {
		return fmt.Errorf("encoder not running")
	}

	// Write without holding the lock: a hung ffmpeg can block the pipe, and
	// the watchdog must still be able to replace it.
	e.lastIn.Store(time.Now().UnixNano())
	if _, err := p.stdin.Write(frame); err != nil {
		e.mu.Lock()
		replaced := e.proc != p && !e.closed
		e.mu.Unlock()
		if replaced {
			return nil // the watchdog restarted ffmpeg mid-write, drop this frame
		}
		return fmt.Errorf("encoder rejected frame (%s): %w — %s",
			e.name, err, e.lastStderr())
	}
	return nil
}

// logDroppedFrame reports rejected frames at most every 10 seconds.
func (e *ffmpegEncoder) logDroppedFrame(reason error) {
	n := e.droppedFrames.Add(1)
	now := time.Now().UnixNano()
	last := e.lastDropLog.Load()
	if now-last < int64(10*time.Second) || !e.lastDropLog.CompareAndSwap(last, now) {
		return
	}
	log.Printf("Video encoder %s: dropped corrupt capture frame (%v), %d dropped so far\n", e.name, reason, n)
}

func (e *ffmpegEncoder) Output() io.Reader {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.outR
}

func (e *ffmpegEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if e.started {
		close(e.done)
	}
	if p := e.proc; p != nil {
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
	if e.outW != nil {
		_ = e.outW.Close()
	}
	return nil
}

// lastStderr returns the tail of the current ffmpeg stderr output for error messages.
func (e *ffmpegEncoder) lastStderr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastStderrLocked()
}

func (e *ffmpegEncoder) lastStderrLocked() string {
	if e.proc == nil {
		return "(no ffmpeg output)"
	}
	s := e.proc.stderr.String()
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	if s == "" {
		return "(no ffmpeg output)"
	}
	return s
}
