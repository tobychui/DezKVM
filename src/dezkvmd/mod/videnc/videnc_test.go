package videnc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeTestMJPEGFrame renders one synthetic JPEG frame.
func makeTestMJPEGFrame(t *testing.T, w, h, seed int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x + seed*7) % 256),
				G: uint8((y + seed*13) % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestSoftwareEncoderPipeline feeds synthetic MJPEG frames through the
// software backend and verifies that an H.264 Annex-B stream comes out.
func TestSoftwareEncoderPipeline(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if ok, detail := softwareAvailable(); !ok {
		t.Skip("software backend unavailable: " + detail)
	}
	runEncoderPipeline(t, BackendSoftware)
}

// TestRKMPPEncoderPipeline runs the same check on the Rockchip hardware
// encoder when ffmpeg-rockchip is installed.
func TestRKMPPEncoderPipeline(t *testing.T) {
	if ok, detail, _ := rkmppAvailable(); !ok {
		t.Skip("rkmpp backend unavailable: " + detail)
	}
	runEncoderPipeline(t, BackendRockchipMPP)
}

// runEncoderPipeline feeds synthetic MJPEG frames through backend and checks
// that an H.264 Annex-B stream comes out.
func runEncoderPipeline(t *testing.T, backend Backend) {
	t.Helper()
	const width, height, fps = 320, 240, 10
	// The encoder may hold back its last frame until more input arrives
	const fedFrames, minFrames = 40, 35
	enc, err := New(Config{
		Backend:     backend,
		Width:       width,
		Height:      height,
		FPS:         fps,
		BitrateKbps: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.Start(); err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	// Feed frames from a goroutine (ffmpeg needs several frames before the
	// first encoded output appears).
	go func() {
		for i := 0; i < fedFrames; i++ {
			frame := makeTestMJPEGFrame(t, width, height, i)
			if err := enc.WriteFrame(frame); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Collect encoder output for up to 10 seconds.
	type result struct {
		data []byte
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		buf := make([]byte, 64*1024)
		collected := []byte{}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && countH264Frames(collected) < minFrames {
			n, err := enc.Output().Read(buf)
			if n > 0 {
				collected = append(collected, buf[:n]...)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				resCh <- result{collected, err}
				return
			}
		}
		resCh <- result{collected, nil}
	}()

	var out []byte
	select {
	case res := <-resCh:
		if res.err != nil && len(res.data) == 0 {
			t.Fatalf("failed to read encoder output: %v", res.err)
		}
		out = res.data
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for encoder output")
	}

	if len(out) == 0 {
		t.Fatal("encoder produced no output")
	}
	// The stream must contain Annex-B start codes.
	if !bytes.Contains(out, []byte{0x00, 0x00, 0x01}) {
		t.Fatalf("output does not look like an H.264 Annex-B stream (%d bytes)", len(out))
	}
	// Every fed frame must come out: a broken timestamp setup once made
	// ffmpeg drop all but 2 frames of a session while still producing bytes
	if n := countH264Frames(out); n < minFrames {
		t.Fatalf("only %d of %d frames were encoded", n, fedFrames)
	}
	t.Logf("received %d frames, %d bytes of H.264 output", countH264Frames(out), len(out))
	assertLimitedRange(t, out)
}

// assertLimitedRange checks the stream is flagged limited ("tv") range:
// Chrome and Edge render full range WebRTC H.264 with crushed shadows and
// blown out highlights.
func assertLimitedRange(t *testing.T, stream []byte) {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Log("ffprobe not installed, colour range not checked")
		return
	}
	cmd := exec.Command("ffprobe", "-v", "error", "-f", "h264", "-i", "pipe:0",
		"-show_entries", "stream=color_range,pix_fmt", "-of", "compact=p=0")
	cmd.Stdin = bytes.NewReader(stream)
	info, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe failed: %v", err)
	}
	if !strings.Contains(string(info), "color_range=tv") {
		t.Fatalf("stream is not limited range: %s", strings.TrimSpace(string(info)))
	}
}

// countH264Frames counts the pictures in an Annex-B stream: coded slices
// (NAL type 1 or 5) whose first_mb_in_slice is 0, i.e. ue(v) bit '1'.
func countH264Frames(b []byte) int {
	frames := 0
	for i := 0; i+4 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 || b[i+2] != 1 {
			continue
		}
		nalType := b[i+3] & 0x1F
		if (nalType == 1 || nalType == 5) && b[i+4]&0x80 != 0 {
			frames++
		}
	}
	return frames
}

// TestParseBackendString covers the UI value parsing.
func TestParseBackendString(t *testing.T) {
	cases := []struct {
		in      string
		backend Backend
		variant Variant
	}{
		{"auto", BackendAuto, VariantNone},
		{"intel-vaapi", BackendIntelVAAPI, VariantNone},
		{"v4l2m2m:raspberrypi", BackendV4L2M2M, VariantRaspberryPi},
		{"v4l2m2m:orangepi", BackendV4L2M2M, VariantOrangePi},
		{"software", BackendSoftware, VariantNone},
		{"nonsense", BackendAuto, VariantNone},
		{"", BackendAuto, VariantNone},
	}
	for _, c := range cases {
		b, v := ParseBackendString(c.in)
		if b != c.backend || v != c.variant {
			t.Errorf("ParseBackendString(%q) = (%s, %s), want (%s, %s)", c.in, b, v, c.backend, c.variant)
		}
	}
}

// A render node that does not belong to an Intel GPU (e.g. rockchip-drm on an
// SBC) must never be reported as a usable VA-API device.
func TestIntelVAAPIRejectsNonIntelRenderNode(t *testing.T) {
	if _, err := os.Stat(defaultRenderDevice); err != nil {
		t.Skip("no render node on this host")
	}
	driver := drmDriverName(defaultRenderDevice)
	if intelVAAPIDrivers[driver] {
		t.Skipf("host has an Intel GPU (%s)", driver)
	}
	if ok, detail := intelVAAPIAvailable(defaultRenderDevice); ok {
		t.Fatalf("VA-API reported available on %s driver: %s", driver, detail)
	}
	if b := pickAutoBackend(VariantNone); b == BackendIntelVAAPI {
		t.Fatalf("auto picked %s on a %s render node", b, driver)
	}
}

// On a Rockchip board whose ffmpeg lacks h264_rkmpp the backend must be
// disabled, and the hint must point the user at the installer script.
func TestRKMPPProbeExplainsMissingFFmpeg(t *testing.T) {
	if rockchipSoC() == "" {
		if ok, _, _ := rkmppAvailable(); ok {
			t.Fatal("rkmpp reported available on a non-Rockchip host")
		}
		t.Skip("not a Rockchip board")
	}
	if _, err := os.Stat(mppServiceDevice); err != nil {
		t.Skip("no " + mppServiceDevice)
	}
	if findFFmpegWithEncoder(rkmppFFmpegCandidates, "h264_rkmpp") != "" {
		t.Skip("an ffmpeg with h264_rkmpp is installed")
	}
	ok, detail, hint := rkmppAvailable()
	if ok {
		t.Fatalf("rkmpp available without h264_rkmpp: %s", detail)
	}
	if !strings.Contains(hint, rkmppInstallScript) {
		t.Fatalf("hint does not mention %s: %q", rkmppInstallScript, hint)
	}
	t.Logf("detail: %s", detail)
	t.Logf("hint: %s", hint)
}

func TestBuildRKMPPArgsForcesBrowserFriendlyH264(t *testing.T) {
	cfg := Config{FPS: 30, BitrateKbps: 4000}
	common := []string{"-c:v h264_rkmpp", "-profile:v baseline", "-bf 0", "-g 30", "-b:v 4000k", "-f h264 pipe:1"}

	sw := strings.Join(buildRKMPPArgs(cfg, false), " ")
	for _, want := range append(common, "-vf "+limitedRange("nv12")) {
		if !strings.Contains(sw, want) {
			t.Errorf("software decode: missing %q in %s", want, sw)
		}
	}
	if strings.Contains(sw, "mjpeg_rkmpp") {
		t.Errorf("software decode must not use mjpeg_rkmpp: %s", sw)
	}

	hw := strings.Join(buildRKMPPArgs(cfg, true), " ")
	for _, want := range append(common, "-c:v mjpeg_rkmpp -i pipe:0", "-vf "+limitedRange("nv12")) {
		if !strings.Contains(hw, want) {
			t.Errorf("hardware decode: missing %q in %s", want, hw)
		}
	}
}

func TestRKMPPMinQPFollowsBitrate(t *testing.T) {
	want := map[int]int{1000: 26, 2000: 24, 4000: 22, 6000: 21, 8000: 20, 0: 22, 100: 28, 100000: 16}
	for kbps, qp := range want {
		if got := rkmppMinQP(kbps); got != qp {
			t.Errorf("rkmppMinQP(%d) = %d, want %d", kbps, got, qp)
		}
	}
	args := strings.Join(buildRKMPPArgs(Config{FPS: 25, BitrateKbps: 4000}, true), " ")
	for _, want := range []string{"-qp_min 22", "-qp_min_i 22"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %s", want, args)
		}
	}
}

func TestParseBackendStringRKMPP(t *testing.T) {
	if b, _ := ParseBackendString("rkmpp"); b != BackendRockchipMPP {
		t.Fatalf("got %q", b)
	}
}

// corruptJPEGs returns frames modelled on capture glitches that hang the
// Rockchip hardware JPEG decoder, keyed by description.
func corruptJPEGs(t *testing.T, good []byte) map[string][]byte {
	t.Helper()
	garbage := append([]byte{}, good[:len(good)/3]...)
	for i := 0; i < len(good)/2; i++ {
		garbage = append(garbage, byte(i*7919>>3)|0x80) // dense 0xFFxx pairs
	}
	garbage = append(garbage, 0xFF, 0xD9)
	junk := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0xFF, 0x42}, 2000)...)
	junk = append(junk, 0xFF, 0xD9)
	return map[string][]byte{
		"garbage scan data":      garbage,
		"junk with JPEG header":  junk,
		"no EOI":                 good[:len(good)-2],
		"not a JPEG":             []byte("hello"),
		"cut inside the headers": good[:40],
	}
}

func TestCheckBaselineJPEG(t *testing.T) {
	good := makeTestMJPEGFrame(t, 320, 240, 1)
	if err := checkBaselineJPEG(good); err != nil {
		t.Fatalf("valid frame rejected: %v", err)
	}
	// Capture buffers may carry padding after EOI
	if err := checkBaselineJPEG(append(append([]byte{}, good...), 0, 0, 0, 0)); err != nil {
		t.Fatalf("valid frame with trailing padding rejected: %v", err)
	}
	for name, frame := range corruptJPEGs(t, good) {
		if err := checkBaselineJPEG(frame); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestRKMPPSurvivesCorruptFrames feeds corrupt frames between good ones
// through the hardware pipeline and checks the stream keeps going.
func TestRKMPPSurvivesCorruptFrames(t *testing.T) {
	if ok, detail, _ := rkmppAvailable(); !ok {
		t.Skip("rkmpp backend unavailable: " + detail)
	}
	if !rkmppHWDecode {
		t.Skip("no hardware JPEG decode")
	}
	enc, err := New(Config{Backend: BackendRockchipMPP, Width: 320, Height: 240, FPS: 10, BitrateKbps: 500})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.Start(); err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	good := makeTestMJPEGFrame(t, 320, 240, 0)
	var bad [][]byte
	for _, f := range corruptJPEGs(t, good) {
		bad = append(bad, f)
	}
	const fed = 40
	go func() {
		for i := 0; i < fed; i++ {
			if i >= 10 && i < 10+len(bad) {
				_ = enc.WriteFrame(bad[i-10])
			}
			if enc.WriteFrame(makeTestMJPEGFrame(t, 320, 240, i)) != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	out := readUntilFrames(t, enc.Output(), 35, 10*time.Second)
	if n := countH264Frames(out); n < 35 {
		t.Fatalf("only %d of %d good frames were encoded around the corrupt ones", n, fed)
	}
}

// TestEncoderWatchdogRestartsStalledProcess simulates an ffmpeg that stops
// producing output: the first process swallows its input, the restarted one
// echoes it. The output stream must resume without the caller noticing.
func TestEncoderWatchdogRestartsStalledProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started-once")
	script := "if [ -e " + marker + " ]; then exec cat; else touch " + marker + "; cat >/dev/null; fi" // keeps stdout open, like a hung ffmpeg
	enc := newFFmpegEncoderBin("stall test", "sh", []string{"-c", script})
	if err := enc.Start(); err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := enc.WriteFrame([]byte("frame\n")); err != nil {
				t.Errorf("WriteFrame failed across the restart: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	buf := make([]byte, 64)
	got := make(chan int, 1)
	go func() {
		n, _ := enc.Output().Read(buf)
		got <- n
	}()
	select {
	case n := <-got:
		if n == 0 || !strings.HasPrefix(string(buf[:n]), "frame") {
			t.Fatalf("unexpected output after restart: %q", buf[:n])
		}
	case <-time.After(stallTimeout + 3*time.Second):
		t.Fatal("watchdog did not restart the stalled process")
	}
}

// readUntilFrames reads r until it holds want H.264 frames or timeout.
func readUntilFrames(t *testing.T, r io.Reader, want int, timeout time.Duration) []byte {
	t.Helper()
	type result struct{ data []byte }
	ch := make(chan []byte, 1)
	go func() {
		var collected []byte
		buf := make([]byte, 64*1024)
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) && countH264Frames(collected) < want {
			n, err := r.Read(buf)
			collected = append(collected, buf[:n]...)
			if err != nil {
				break
			}
		}
		ch <- collected
	}()
	select {
	case out := <-ch:
		return out
	case <-time.After(timeout + 5*time.Second):
		t.Fatal("timed out reading encoder output")
		return nil
	}
}

// makeDesktopJPEG renders a 1280x720 frame with dense high contrast
// "text" on a gradient. invert flips the colours, like a theme or
// background change on the remote screen.
func makeDesktopJPEG(t *testing.T, invert bool) []byte {
	t.Helper()
	const w, h = 1280, 720
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	rnd := func() uint32 { seed = seed*1664525 + 1013904223; return seed >> 16 }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[(y*w+x)*4+0] = uint8(30 + x*60/w)
			img.Pix[(y*w+x)*4+1] = uint8(30 + y*60/h)
			img.Pix[(y*w+x)*4+2] = 90
			img.Pix[(y*w+x)*4+3] = 255
		}
	}
	// Text in a window: 5x7 glyphs from a tiny "font" in 8x14 cells, like
	// real text the same shapes repeat, which is what makes it compressible
	glyphs := [][7]uint8{
		{0x0E, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11}, {0x1E, 0x11, 0x1E, 0x11, 0x11, 0x11, 0x1E},
		{0x0E, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0E}, {0x1F, 0x10, 0x1E, 0x10, 0x10, 0x10, 0x1F},
		{0x11, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11}, {0x0E, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0E},
		{0x11, 0x1B, 0x15, 0x11, 0x11, 0x11, 0x11}, {0x0E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // space
	}
	for row := 120; row+14 < 420; row += 14 { // a text window over a gradient background
		for col := 200; col+8 < 900; col += 8 {
			g := glyphs[rnd()%uint32(len(glyphs))]
			for gy := 0; gy < 7; gy++ {
				for gx := 0; gx < 5; gx++ {
					if g[gy]&(0x10>>gx) != 0 {
						i := ((row+gy)*w + col + gx) * 4
						img.Pix[i], img.Pix[i+1], img.Pix[i+2] = 230, 230, 230
					}
				}
			}
		}
	}
	if invert {
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = 255-img.Pix[i+2], 255-img.Pix[i], 255-img.Pix[i+1]
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestRKMPPNoBurstOnScreenChange guards against the encoder sending
// keyframes and scene changes far over the bitrate budget, which makes the
// browser lag while it drains them.
func TestRKMPPNoBurstOnScreenChange(t *testing.T) {
	if ok, detail, _ := rkmppAvailable(); !ok {
		t.Skip("rkmpp backend unavailable: " + detail)
	}
	const fps, kbps = 25, 4000
	enc, err := New(Config{Backend: BackendRockchipMPP, Width: 1280, Height: 720, FPS: fps, BitrateKbps: kbps})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.Start(); err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	normal, inverted := makeDesktopJPEG(t, false), makeDesktopJPEG(t, true)
	const fed = 100
	go func() {
		for i := 0; i < fed; i++ {
			frame := normal // 2 s of static desktop, then a colour flip every 0.2 s
			if i >= 50 && (i/5)%2 == 1 {
				frame = inverted
			}
			if enc.WriteFrame(frame) != nil {
				return
			}
			time.Sleep(time.Second / fps)
		}
	}()
	out := readUntilFrames(t, enc.Output(), fed-5, 15*time.Second)

	// Size of each frame: distance between consecutive frame starts
	var starts []int
	for j := 0; j+4 < len(out); j++ {
		if out[j] == 0 && out[j+1] == 0 && out[j+2] == 1 {
			if ty := out[j+3] & 0x1F; (ty == 1 || ty == 5) && out[j+4]&0x80 != 0 {
				starts = append(starts, j)
			}
		}
	}
	largest := 0
	for k := 1; k < len(starts); k++ {
		largest = max(largest, starts[k]-starts[k-1])
	}
	limit := kbps * 1000 / 8 * 3 / 10 // 0.3 s worth of the bitrate in one frame
	t.Logf("%d frames, largest %d KB (limit %d KB), %d KB total", len(starts), largest/1024, limit/1024, len(out)/1024)
	if largest > limit {
		t.Fatalf("largest frame %d KB exceeds 0.3 s of the %d kbps budget (%d KB)", largest/1024, kbps, limit/1024)
	}
}
