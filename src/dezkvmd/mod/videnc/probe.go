package videnc

/*
	probe.go

	Runtime detection of the encoder backends available on this host. The
	results are cached (hardware does not change while the daemon runs) and
	exposed to the web UI so the encoder dropdown can grey out unusable
	options.
*/

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackendInfo describes one probed backend for the settings UI.
type BackendInfo struct {
	Backend   Backend `json:"backend"`
	Variant   Variant `json:"variant,omitempty"`
	Label     string  `json:"label"`
	Available bool    `json:"available"`
	Detail    string  `json:"detail"`         // human readable availability note
	Hint      string  `json:"hint,omitempty"` // how to make an unavailable backend work, if fixable
}

var (
	probeOnce   sync.Once
	probeResult []BackendInfo

	ffmpegEncodersMu    sync.Mutex
	ffmpegEncodersCache = map[string]string{}
)

// DetectBackends probes every backend once and returns the cached result.
func DetectBackends() []BackendInfo {
	probeOnce.Do(func() {
		intelOK, intelDetail := intelVAAPIAvailable(defaultRenderDevice)
		rkOK, rkDetail, rkHint := rkmppAvailable()
		rpiOK, rpiDetail := v4l2m2mAvailable(VariantRaspberryPi)
		opiOK, opiDetail := v4l2m2mAvailable(VariantOrangePi)
		swOK, swDetail := softwareAvailable()

		probeResult = []BackendInfo{
			{Backend: BackendIntelVAAPI, Label: "Intel iGPU (VA-API)", Available: intelOK, Detail: intelDetail},
			{Backend: BackendRockchipMPP, Label: "Rockchip MPP hardware encoder", Available: rkOK, Detail: rkDetail, Hint: rkHint},
			{Backend: BackendV4L2M2M, Variant: VariantRaspberryPi, Label: "V4L2 M2M — Raspberry Pi", Available: rpiOK, Detail: rpiDetail},
			{Backend: BackendV4L2M2M, Variant: VariantOrangePi, Label: "V4L2 M2M — Orange Pi", Available: opiOK, Detail: opiDetail},
			{Backend: BackendSoftware, Label: "Software (ffmpeg libx264)", Available: swOK, Detail: swDetail},
		}
	})
	return probeResult
}

// ffmpegEncodersOf returns the (cached) output of `<bin> -encoders`, or ""
// when bin cannot be run.
func ffmpegEncodersOf(bin string) string {
	ffmpegEncodersMu.Lock()
	defer ffmpegEncodersMu.Unlock()
	if list, ok := ffmpegEncodersCache[bin]; ok {
		return list
	}
	out, err := exec.Command(bin, "-hide_banner", "-encoders").Output()
	list := ""
	if err == nil {
		list = string(out)
	}
	ffmpegEncodersCache[bin] = list
	return list
}

func ffmpegBinHasEncoder(bin, name string) bool {
	return strings.Contains(ffmpegEncodersOf(bin), " "+name+" ")
}

var (
	ffmpegDecodersMu    sync.Mutex
	ffmpegDecodersCache = map[string]string{}
)

// ffmpegBinHasDecoder reports whether bin lists decoder name in -decoders.
func ffmpegBinHasDecoder(bin, name string) bool {
	ffmpegDecodersMu.Lock()
	list, ok := ffmpegDecodersCache[bin]
	if !ok {
		out, err := exec.Command(bin, "-hide_banner", "-decoders").Output()
		if err == nil {
			list = string(out)
		}
		ffmpegDecodersCache[bin] = list
	}
	ffmpegDecodersMu.Unlock()
	return strings.Contains(list, " "+name+" ")
}

// ffmpegHasEncoder checks the ffmpeg found in PATH
func ffmpegHasEncoder(name string) bool {
	return ffmpegBinHasEncoder("ffmpeg", name)
}

// FFmpegHasEncoder reports whether the ffmpeg found in PATH lists encoder
// name (cached). Used by other modules that run their own ffmpeg, e.g. the
// WebRTC Opus audio encoder.
func FFmpegHasEncoder(name string) bool {
	return ffmpegHasEncoder(name)
}

// findFFmpegWithEncoder returns the first candidate ffmpeg binary that
// exists and supports encoder name, or "".
func findFFmpegWithEncoder(candidates []string, name string) string {
	for _, bin := range candidates {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		if ffmpegBinHasEncoder(bin, name) {
			return bin
		}
	}
	return ""
}

func ffmpegInstalled() (bool, string) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return false, "ffmpeg not found in PATH"
	}
	return true, ""
}

// intelVAAPIDrivers are the kernel DRM drivers of Intel GPUs. Other render
// nodes (rockchip-drm, vc4, panfrost, sun4i-drm... on SBCs) exist on most ARM
// boards but have no VA-API encoder behind them.
var intelVAAPIDrivers = map[string]bool{"i915": true, "xe": true}

type probeStatus struct {
	ok     bool
	detail string
}

var (
	vaapiProbeMu    sync.Mutex
	vaapiProbeCache = map[string]probeStatus{}
)

// intelVAAPIAvailable checks that renderDevice belongs to an Intel GPU and
// that ffmpeg can actually open a VA-API H.264 encoder on it. The result is
// cached per render device.
func intelVAAPIAvailable(renderDevice string) (bool, string) {
	if renderDevice == "" {
		renderDevice = defaultRenderDevice
	}
	vaapiProbeMu.Lock()
	defer vaapiProbeMu.Unlock()
	if st, ok := vaapiProbeCache[renderDevice]; ok {
		return st.ok, st.detail
	}
	ok, detail := probeIntelVAAPI(renderDevice)
	vaapiProbeCache[renderDevice] = probeStatus{ok, detail}
	return ok, detail
}

func probeIntelVAAPI(renderDevice string) (bool, string) {
	if ok, detail := ffmpegInstalled(); !ok {
		return false, detail
	}
	if _, err := os.Stat(renderDevice); err != nil {
		return false, fmt.Sprintf("no DRM render node at %s", renderDevice)
	}
	driver := drmDriverName(renderDevice)
	if !intelVAAPIDrivers[driver] {
		if driver == "" {
			driver = "unknown"
		}
		return false, fmt.Sprintf("%s is not an Intel GPU (driver: %s)", renderDevice, driver)
	}
	if !ffmpegHasEncoder("h264_vaapi") {
		return false, "ffmpeg was built without h264_vaapi support"
	}
	// Encode one tiny frame to make sure libva and its driver really work
	// (e.g. intel-media-va-driver may be missing)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-vaapi_device", renderDevice,
		"-f", "lavfi", "-i", "nullsrc=s=256x144",
		"-frames:v", "1",
		"-vf", "format=nv12,hwupload",
		"-c:v", "h264_vaapi",
		"-f", "null", "-").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return false, fmt.Sprintf("VA-API H.264 encoder test on %s failed: %s", renderDevice, msg)
	}
	return true, "ready (" + renderDevice + ")"
}

// drmDriverName returns the kernel driver bound to a DRM node such as
// /dev/dri/renderD128, or "" if it cannot be determined.
func drmDriverName(node string) string {
	link, err := os.Readlink(filepath.Join("/sys/class/drm", filepath.Base(node), "device", "driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// v4l2m2mAvailable checks for a kernel V4L2 memory-to-memory H.264 encoder
// device plus ffmpeg support. The variant only affects the diagnostics: the
// actual device is discovered by scanning /dev/video*.
func v4l2m2mAvailable(variant Variant) (bool, string) {
	if ok, detail := ffmpegInstalled(); !ok {
		return false, detail
	}
	if !ffmpegHasEncoder("h264_v4l2m2m") {
		return false, "ffmpeg was built without h264_v4l2m2m support"
	}
	dev, err := findM2MEncoderDevice()
	if err != nil {
		if variant == VariantOrangePi {
			return false, "no V4L2 M2M H.264 encoder device found (some Allwinner SoCs has no mainline encoder driver; the Cedrus driver is decode-only)"
		}
		return false, "no V4L2 M2M H.264 encoder device found"
	}
	return true, "ready (" + dev + ")"
}

// findM2MEncoderDevice scans /dev/video* for a device that advertises H.264
// on its (M2M) output side via v4l2-ctl.
func findM2MEncoderDevice() (string, error) {
	devices, err := filepath.Glob("/dev/video*")
	if err != nil || len(devices) == 0 {
		return "", fmt.Errorf("no video devices present")
	}
	for _, dev := range devices {
		out, err := exec.Command("v4l2-ctl", "-d", dev, "--list-formats-out").Output()
		if err != nil {
			continue
		}
		text := string(out)
		// An M2M encoder produces H264 on its capture side and consumes raw
		// frames on its output side. ffmpeg's h264_v4l2m2m expects YUV in /
		// H264 out, so look for a device that accepts raw formats and lists
		// H264 among its capture formats.
		if !strings.Contains(text, "YU12") && !strings.Contains(text, "YUYV") && !strings.Contains(text, "NV12") {
			continue
		}
		capOut, err := exec.Command("v4l2-ctl", "-d", dev, "--list-formats").Output()
		if err != nil {
			continue
		}
		if strings.Contains(string(capOut), "H264") {
			return dev, nil
		}
	}
	return "", fmt.Errorf("no M2M H.264 encoder found")
}

// softwareAvailable checks the libx264 fallback.
func softwareAvailable() (bool, string) {
	if ok, detail := ffmpegInstalled(); !ok {
		return false, detail
	}
	if !ffmpegHasEncoder("libx264") {
		return false, "ffmpeg was built without libx264 support"
	}
	return true, "ready (CPU encoding)"
}
