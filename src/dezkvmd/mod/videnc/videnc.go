package videnc

/*
	videnc - Hardware video encoder abstraction layer for DezKVM

	Converts the MJPEG stream captured from the HDMI capture card into a
	low-latency H.264 elementary stream suitable for WebRTC. The MJPEG input
	is decoded to raw YUV and re-encoded by one of several interchangeable
	backends:

	  BackendIntelVAAPI  - Intel iGPU via VA-API (h264_vaapi). AMD iGPU
	                       support can later reuse this backend since it is
	                       also VA-API based.
	  BackendV4L2M2M     - Kernel V4L2 memory-to-memory hardware encoders
	                       (h264_v4l2m2m). Variant selects the board family:
	                       VariantRaspberryPi (bcm2835-codec, /dev/video11)
	                       or VariantOrangePi (Allwinner H-series; note that
	                       some AllWinner SoCs currently has no
	                       mainline H.264 *encoder* driver - the Cedrus
	                       driver is decode-only - so this variant probes for
	                       an M2M encoder device and reports unavailable when
	                       none exists).
	  BackendSoftware    - Portable fallback using libx264 (CPU).

	All backends drive the encoder through an ffmpeg child process (started
	with exec.Command; no CGO). ffmpeg performs the MJPEG software decode to
	YUV internally and hands the frames to the selected encoder. The Go side
	pipes MJPEG frames into stdin and reads the Annex-B H.264 stream from
	stdout.
*/

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Backend identifies an encoder implementation.
type Backend string

const (
	BackendAuto       Backend = "auto"        // pick the best available backend
	BackendIntelVAAPI Backend = "intel-vaapi" // Intel iGPU via VA-API
	BackendV4L2M2M    Backend = "v4l2m2m"     // V4L2 memory-to-memory (SBC hardware encoders)
	BackendSoftware   Backend = "software"    // libx264 software fallback
)

// Variant selects a board family for backends that need device-specific
// behaviour (currently only BackendV4L2M2M).
type Variant string

const (
	VariantNone        Variant = ""
	VariantRaspberryPi Variant = "raspberrypi"
	VariantOrangePi    Variant = "orangepi"
)

// Config describes one encoding session.
type Config struct {
	Backend Backend
	Variant Variant

	Width  int
	Height int
	FPS    int

	// BitrateKbps is the target video bitrate ("compression rate" from the
	// user's point of view). 0 selects the default (4000 kbps).
	BitrateKbps int

	// RenderDevice overrides the DRM render node for VA-API
	// (default /dev/dri/renderD128).
	RenderDevice string
}

// Encoder consumes MJPEG frames and produces an H.264 Annex-B byte stream.
type Encoder interface {
	// Name returns a human readable descriptor of the backend in use.
	Name() string
	// Start launches the encoding pipeline.
	Start() error
	// WriteFrame feeds one complete MJPEG frame into the encoder.
	WriteFrame(frame []byte) error
	// Output returns the reader for the encoded H.264 Annex-B stream.
	// Valid after Start.
	Output() io.Reader
	// Close terminates the pipeline and releases the hardware encoder.
	Close() error
}

// ErrBackendUnavailable is returned when the requested backend cannot run on
// this host (missing device node, missing ffmpeg support, ...).
var ErrBackendUnavailable = errors.New("video encoder backend not available on this system")

func (c *Config) withDefaults() Config {
	out := *c
	if out.BitrateKbps <= 0 {
		out.BitrateKbps = 4000
	}
	if out.FPS <= 0 {
		out.FPS = 25
	}
	if out.RenderDevice == "" {
		out.RenderDevice = defaultRenderDevice
	}
	return out
}

// New creates an encoder for the requested backend. BackendAuto picks the
// first available hardware backend and falls back to software. The chosen
// backend's availability is verified before the encoder is returned.
func New(cfg Config) (Encoder, error) {
	resolved := cfg.withDefaults()

	backend := resolved.Backend
	if backend == "" || backend == BackendAuto {
		backend = pickAutoBackend(resolved.Variant)
	}

	switch backend {
	case BackendIntelVAAPI:
		if avail, detail := intelVAAPIAvailable(resolved.RenderDevice); !avail {
			return nil, fmt.Errorf("%w: intel-vaapi: %s", ErrBackendUnavailable, detail)
		}
		return newFFmpegEncoder("Intel iGPU (VA-API h264_vaapi)", buildIntelVAAPIArgs(resolved)), nil

	case BackendV4L2M2M:
		if avail, detail := v4l2m2mAvailable(resolved.Variant); !avail {
			return nil, fmt.Errorf("%w: v4l2m2m(%s): %s", ErrBackendUnavailable, variantLabel(resolved.Variant), detail)
		}
		return newFFmpegEncoder(
			fmt.Sprintf("V4L2 M2M hardware encoder (%s, h264_v4l2m2m)", variantLabel(resolved.Variant)),
			buildV4L2M2MArgs(resolved)), nil

	case BackendSoftware:
		if avail, detail := softwareAvailable(); !avail {
			return nil, fmt.Errorf("%w: software: %s", ErrBackendUnavailable, detail)
		}
		return newFFmpegEncoder("Software (ffmpeg libx264)", buildSoftwareArgs(resolved)), nil
	}

	return nil, fmt.Errorf("unknown video encoder backend %q", cfg.Backend)
}

// pickAutoBackend returns the preferred backend for this host:
// Intel VA-API > V4L2 M2M > software.
func pickAutoBackend(variant Variant) Backend {
	if avail, _ := intelVAAPIAvailable(defaultRenderDevice); avail {
		return BackendIntelVAAPI
	}
	if avail, _ := v4l2m2mAvailable(variant); avail {
		return BackendV4L2M2M
	}
	return BackendSoftware
}

func variantLabel(v Variant) string {
	switch v {
	case VariantRaspberryPi:
		return "Raspberry Pi"
	case VariantOrangePi:
		return "Orange Pi"
	default:
		return "generic"
	}
}

// ParseBackendString parses a UI value like "v4l2m2m:raspberrypi" or
// "intel-vaapi" into backend + variant.
func ParseBackendString(s string) (Backend, Variant) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	backend := Backend(parts[0])
	variant := VariantNone
	if len(parts) == 2 {
		variant = Variant(parts[1])
	}
	switch backend {
	case BackendAuto, BackendIntelVAAPI, BackendV4L2M2M, BackendSoftware:
		return backend, variant
	}
	return BackendAuto, VariantNone
}
