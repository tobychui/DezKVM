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
	"os/exec"
	"strconv"
	"sync"
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

// commonOutputArgs are the raw H.264 output options shared by all backends.
func commonOutputArgs() []string {
	return []string{
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
		"-vf", "format=nv12,hwupload",
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
		"-pix_fmt", "yuv420p",
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
		"-pix_fmt", "yuv420p",
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

// ffmpegEncoder implements Encoder on top of an ffmpeg child process.
type ffmpegEncoder struct {
	name string
	args []string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  bytes.Buffer
	started bool
	closed  bool
}

func newFFmpegEncoder(name string, args []string) *ffmpegEncoder {
	return &ffmpegEncoder{name: name, args: args}
}

func (e *ffmpegEncoder) Name() string { return e.name }

func (e *ffmpegEncoder) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return fmt.Errorf("encoder already started")
	}

	cmd := exec.Command("ffmpeg", e.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = &e.stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	e.cmd = cmd
	e.stdin = stdin
	e.stdout = stdout
	e.started = true

	// Reap the process when it exits on its own so it never zombifies.
	go func() { _ = cmd.Wait() }()
	return nil
}

func (e *ffmpegEncoder) WriteFrame(frame []byte) error {
	e.mu.Lock()
	stdin := e.stdin
	closed := e.closed
	e.mu.Unlock()
	if closed || stdin == nil {
		return fmt.Errorf("encoder not running")
	}
	if _, err := stdin.Write(frame); err != nil {
		return fmt.Errorf("encoder rejected frame (%s): %w — %s",
			e.name, err, e.lastStderr())
	}
	return nil
}

func (e *ffmpegEncoder) Output() io.Reader {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stdout
}

func (e *ffmpegEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if e.stdin != nil {
		e.stdin.Close()
	}
	if e.stdout != nil {
		e.stdout.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	return nil
}

// lastStderr returns the tail of the ffmpeg stderr output for error messages.
func (e *ffmpegEncoder) lastStderr() string {
	s := e.stderr.String()
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	if s == "" {
		return "(no ffmpeg output)"
	}
	return s
}
