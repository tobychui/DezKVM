package videnc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os/exec"
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

	const width, height, fps = 320, 240, 10
	enc, err := New(Config{
		Backend:     BackendSoftware,
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
		for i := 0; i < 40; i++ {
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
		for time.Now().Before(deadline) && len(collected) < 2048 {
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
	t.Logf("received %d bytes of H.264 output", len(out))
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
