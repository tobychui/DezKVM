package videnc

/*
	probe.go

	Runtime detection of the encoder backends available on this host. The
	results are cached (hardware does not change while the daemon runs) and
	exposed to the web UI so the encoder dropdown can grey out unusable
	options.
*/

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// BackendInfo describes one probed backend for the settings UI.
type BackendInfo struct {
	Backend   Backend `json:"backend"`
	Variant   Variant `json:"variant,omitempty"`
	Label     string  `json:"label"`
	Available bool    `json:"available"`
	Detail    string  `json:"detail"` // human readable availability note
}

var (
	probeOnce   sync.Once
	probeResult []BackendInfo

	ffmpegEncodersOnce sync.Once
	ffmpegEncodersList string
)

// DetectBackends probes every backend once and returns the cached result.
func DetectBackends() []BackendInfo {
	probeOnce.Do(func() {
		intelOK, intelDetail := intelVAAPIAvailable(defaultRenderDevice)
		rpiOK, rpiDetail := v4l2m2mAvailable(VariantRaspberryPi)
		opiOK, opiDetail := v4l2m2mAvailable(VariantOrangePi)
		swOK, swDetail := softwareAvailable()

		probeResult = []BackendInfo{
			{Backend: BackendIntelVAAPI, Label: "Intel iGPU (VA-API)", Available: intelOK, Detail: intelDetail},
			{Backend: BackendV4L2M2M, Variant: VariantRaspberryPi, Label: "V4L2 M2M — Raspberry Pi", Available: rpiOK, Detail: rpiDetail},
			{Backend: BackendV4L2M2M, Variant: VariantOrangePi, Label: "V4L2 M2M — Orange Pi", Available: opiOK, Detail: opiDetail},
			{Backend: BackendSoftware, Label: "Software (ffmpeg libx264)", Available: swOK, Detail: swDetail},
		}
	})
	return probeResult
}

// ffmpegEncoders returns the (cached) output of `ffmpeg -encoders`.
func ffmpegEncoders() string {
	ffmpegEncodersOnce.Do(func() {
		out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
		if err != nil {
			ffmpegEncodersList = ""
			return
		}
		ffmpegEncodersList = string(out)
	})
	return ffmpegEncodersList
}

func ffmpegHasEncoder(name string) bool {
	list := ffmpegEncoders()
	if list == "" {
		return false
	}
	return strings.Contains(list, " "+name+" ")
}

func ffmpegInstalled() (bool, string) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return false, "ffmpeg not found in PATH"
	}
	return true, ""
}

// intelVAAPIAvailable checks for a DRM render node plus VA-API encoder
// support in ffmpeg.
func intelVAAPIAvailable(renderDevice string) (bool, string) {
	if ok, detail := ffmpegInstalled(); !ok {
		return false, detail
	}
	if renderDevice == "" {
		renderDevice = defaultRenderDevice
	}
	if _, err := os.Stat(renderDevice); err != nil {
		return false, fmt.Sprintf("no DRM render node at %s", renderDevice)
	}
	if !ffmpegHasEncoder("h264_vaapi") {
		return false, "ffmpeg was built without h264_vaapi support"
	}
	return true, "ready (" + renderDevice + ")"
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
