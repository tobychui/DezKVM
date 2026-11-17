package videnc

/*
	rkmpp.go

	Rockchip hardware H.264 encoder (VEPU) through the Rockchip Media
	Process Platform (MPP). Mainline ffmpeg has no MPP encoder, it needs the
	ffmpeg-rockchip fork (github.com/nyanmisaka/ffmpeg-rockchip) which adds
	h264_rkmpp. platforms/rockchip/install_ffmpeg_rkmpp.sh builds that fork
	into rkmppFFmpegPrefix, next to (not over) the distro ffmpeg, so every
	other ffmpeg user keeps the stock build.

	When the SoC also has a hardware JPEG decoder (mjpeg_rkmpp), the capture
	card's MJPEG frames are decoded by it instead of the CPU, which cuts the
	pipeline's CPU use from ~3 cores to under one at 1080p30 and shaves
	~14 ms of latency. Frames are checked by checkBaselineJPEG first, see
	jpegcheck.go for why.
*/

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// rkmppFFmpegPrefix is where install_ffmpeg_rkmpp.sh installs ffmpeg-rockchip
	rkmppFFmpegPrefix = "/opt/ffmpeg-rkmpp"
	// rkmppInstallScript is the installer path relative to the dezkvmd folder
	rkmppInstallScript = "platforms/rockchip/install_ffmpeg_rkmpp.sh"
	// mppServiceDevice is created by the Rockchip vendor kernel MPP driver
	mppServiceDevice = "/dev/mpp_service"
)

// rkmppFFmpegCandidates are the ffmpeg binaries checked for h264_rkmpp, in
// order. "ffmpeg" (from PATH) covers distros that already ship
// ffmpeg-rockchip (e.g. some Armbian / Ubuntu Rockchip images).
var rkmppFFmpegCandidates = []string{rkmppFFmpegPrefix + "/bin/ffmpeg", "ffmpeg"}

var (
	rkmppProbeOnce   sync.Once
	rkmppProbeStatus probeStatus
	rkmppProbeHint   string
	rkmppFFmpegBin   string
	// rkmppHWDecode is true when mjpeg_rkmpp decodes the capture frames
	rkmppHWDecode bool
)

// rkmppAvailable reports whether the Rockchip MPP H.264 encoder can be used.
// hint tells the user how to fix it when it is a fixable software problem.
func rkmppAvailable() (ok bool, detail string, hint string) {
	rkmppProbeOnce.Do(func() {
		ok, detail, hint, bin := probeRKMPP()
		rkmppProbeStatus = probeStatus{ok, detail}
		rkmppProbeHint = hint
		rkmppFFmpegBin = bin
		if ok {
			if err := probeRKMPPJPEGDecode(bin); err != nil {
				rkmppProbeStatus.detail += ", JPEG decode in software (" + err.Error() + ")"
			} else {
				rkmppHWDecode = true
				rkmppProbeStatus.detail = strings.Replace(rkmppProbeStatus.detail, "ready (", "ready (hardware JPEG decode, ", 1)
			}
		}
	})
	return rkmppProbeStatus.ok, rkmppProbeStatus.detail, rkmppProbeHint
}

func probeRKMPP() (ok bool, detail string, hint string, bin string) {
	soc := rockchipSoC()
	if soc == "" {
		return false, "not a Rockchip SoC", "", ""
	}
	if _, err := os.Stat(mppServiceDevice); err != nil {
		return false, fmt.Sprintf("%s detected, but the MPP kernel driver is missing (no %s)", soc, mppServiceDevice),
			"The hardware encoder needs a Rockchip vendor (BSP) kernel that provides " + mppServiceDevice +
				". Use the board vendor's OS image (e.g. FriendlyElec, Radxa, Armbian vendor kernel).", ""
	}

	bin = findFFmpegWithEncoder(rkmppFFmpegCandidates, "h264_rkmpp")
	if bin == "" {
		return false, fmt.Sprintf("%s hardware encoder present, but the installed ffmpeg has no h264_rkmpp encoder", soc),
			"Stock ffmpeg does not support the Rockchip encoder. Build ffmpeg-rockchip with " +
				"\"sudo ./" + rkmppInstallScript + "\" in the dezkvmd folder (installs to " + rkmppFFmpegPrefix +
				", the system ffmpeg is left untouched), then restart dezkvmd.", ""
	}

	// Encode one tiny frame to make sure MPP really opens (permissions on
	// /dev/mpp_service, mismatched librockchip_mpp...)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "nullsrc=s=320x240",
		"-frames:v", "1",
		"-vf", "format=nv12",
		"-c:v", "h264_rkmpp",
		"-f", "null", "-").CombinedOutput()
	if err != nil {
		return false, fmt.Sprintf("h264_rkmpp test encode with %s failed: %s", bin, firstLine(string(out))),
			"Make sure dezkvmd can open " + mppServiceDevice + " (run it as root or add the user to the \"video\" group).", ""
	}
	return true, fmt.Sprintf("ready (%s, %s)", soc, bin), "", bin
}

// probeRKMPPJPEGDecode checks that the hardware JPEG decoder can decode a
// 4:2:0 JPEG and feed the hardware H.264 encoder.
func probeRKMPPJPEGDecode(bin string) error {
	if !ffmpegBinHasDecoder(bin, "mjpeg_rkmpp") {
		return fmt.Errorf("%s has no mjpeg_rkmpp decoder", bin)
	}
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var frame bytes.Buffer
	if err := jpeg.Encode(&frame, img, &jpeg.Options{Quality: 80}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error",
		"-c:v", "mjpeg_rkmpp", "-f", "mjpeg", "-i", "pipe:0",
		"-c:v", "h264_rkmpp", "-f", "h264", "pipe:1")
	cmd.Stdin = bytes.NewReader(frame.Bytes())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("mjpeg_rkmpp test decode failed: %s", firstLine(stderr.String()))
	}
	if len(out) == 0 {
		return fmt.Errorf("mjpeg_rkmpp test decode produced no frame")
	}
	return nil
}

// rockchipSoC returns the SoC name (e.g. "RK3528A") from the device tree, or
// "" when this is not a Rockchip board.
func rockchipSoC() string {
	data, err := os.ReadFile("/proc/device-tree/compatible")
	if err != nil {
		return ""
	}
	for _, c := range strings.Split(string(data), "\x00") {
		if name, ok := strings.CutPrefix(c, "rockchip,"); ok {
			return strings.ToUpper(name)
		}
	}
	return ""
}

// buildRKMPPArgs builds the ffmpeg arguments for the Rockchip MPP encoder.
//
// With hwDecode the frames are decoded by mjpeg_rkmpp, otherwise in
// software. Either way they are converted to limited range NV12: the
// capture card emits full range 4:2:2 JPEG, and browsers need limited range
// 4:2:0 (see limitedRange).
func buildRKMPPArgs(cfg Config, hwDecode bool) []string {
	bitrate := fmt.Sprintf("%dk", cfg.BitrateKbps)
	minQP := rkmppMinQP(cfg.BitrateKbps)
	var args []string
	if hwDecode {
		args = withInputDecoder(commonInputArgs(cfg), "mjpeg_rkmpp")
	} else {
		args = commonInputArgs(cfg)
	}
	// The hardware decoder outputs full range NV16 in system memory; the
	// encoder could take it as is, but browsers need limited range
	args = append(args, "-vf", limitedRange("nv12"))
	args = append(args,
		"-c:v", "h264_rkmpp",
		"-profile:v", "baseline",
		"-rc_mode", "CBR",
		"-bf", "0",
		"-g", strconv.Itoa(cfg.FPS),
		"-b:v", bitrate,
		"-maxrate", bitrate,
		// See rkmppMinQP: without a floor the encoder bursts far over the
		// bitrate on keyframes and scene changes, which shows up as lag
		"-qp_min", strconv.Itoa(minQP),
		"-qp_min_i", strconv.Itoa(minQP),
	)
	return append(args, commonOutputArgs()...)
}

// rkmppMinQP returns the lowest quantizer (finest quality) the encoder may
// use for a target bitrate.
//
// ffmpeg-rockchip defaults the floor to 0. On a static desktop frames are
// cheap, so rate control drifts towards near-lossless quality, and the next
// keyframe or the first frame after a big screen change is then encoded at
// that quality: frames of 300 KB to 1.3 MB at a 4 Mbps (~20 KB/frame)
// target, 5-18 Mbps bursts for seconds, and a browser that lags while it
// drains them. At 4 Mbps a floor of 22 caps those frames at ~120 KB and the
// rate at ~2 Mbps while text stays sharp (SSIM 0.985 vs 0.994 on a macOS
// desktop). Each doubling of the bitrate allows 2 steps finer, so the
// higher quality settings keep more detail with the same relative burst.
func rkmppMinQP(bitrateKbps int) int {
	if bitrateKbps <= 0 {
		bitrateKbps = 4000
	}
	qp := int(math.Round(22 - 2*math.Log2(float64(bitrateKbps)/4000)))
	return max(16, min(28, qp))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "(no ffmpeg output)"
	}
	return s
}
