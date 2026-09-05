package webrtcstream

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/h264reader"

	"imuslab.com/dezkvm/dezkvmd/mod/videnc"
)

// TestIsFirstSliceOfFrame checks the Exp-Golomb first_mb_in_slice heuristic.
func TestIsFirstSliceOfFrame(t *testing.T) {
	// first_mb_in_slice == 0 → ue(v) coded as '1' → top bit of byte 1 set
	if !isFirstSliceOfFrame([]byte{0x65, 0x88, 0x84}) {
		t.Error("expected first slice detection for 0x88 slice header byte")
	}
	// first_mb_in_slice > 0 → top bit clear
	if isFirstSliceOfFrame([]byte{0x41, 0x3a, 0x00}) {
		t.Error("expected non-first slice for 0x3a slice header byte")
	}
	if isFirstSliceOfFrame([]byte{0x65}) {
		t.Error("short NAL must not be treated as a frame start")
	}
}

// TestAccessUnitAggregation runs real libx264 zerolatency output (which emits
// multiple slice NALs per frame — the exact cause of the frozen partial
// picture bug) through the aggregation rules and verifies that the stream
// groups back into one access unit per input frame.
func TestAccessUnitAggregation(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	const width, height, fps, frameCount = 640, 480, 10, 25

	enc, err := videnc.New(videnc.Config{
		Backend:     videnc.BackendSoftware,
		Width:       width,
		Height:      height,
		FPS:         fps,
		BitrateKbps: 800,
	})
	if err != nil {
		t.Skip("software encoder unavailable: " + err.Error())
	}
	if err := enc.Start(); err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	// Feed synthetic MJPEG frames, then close stdin via Close after a delay
	// so ffmpeg flushes its encoder.
	go func() {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for f := 0; f < frameCount; f++ {
			for y := 0; y < height; y += 4 {
				for x := 0; x < width; x += 4 {
					img.Set(x, y, color.RGBA{uint8((x + f*11) % 256), uint8((y + f*3) % 256), 128, 255})
				}
			}
			var buf bytes.Buffer
			if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}) != nil {
				return
			}
			if enc.WriteFrame(buf.Bytes()) != nil {
				return
			}
			time.Sleep(15 * time.Millisecond)
		}
		time.Sleep(500 * time.Millisecond)
		enc.Close() // EOF on stdout → reader loop below terminates
	}()

	reader, err := h264reader.NewReader(enc.Output())
	if err != nil {
		t.Fatal(err)
	}

	// Replicate the pumpEncodedStream aggregation rules.
	totalNALs := 0
	totalVCL := 0
	accessUnits := 0
	multiSliceAUs := 0
	auHasSlice := false
	auSliceCount := 0

	flush := func() {
		if auHasSlice {
			accessUnits++
			if auSliceCount > 1 {
				multiSliceAUs++
			}
		}
		auHasSlice = false
		auSliceCount = 0
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		nal, err := reader.NextNAL()
		if err == io.EOF || err != nil {
			flush()
			break
		}
		totalNALs++
		vcl := isVCLNal(nal.UnitType)
		if vcl {
			totalVCL++
		}
		if auHasSlice && (!vcl || isFirstSliceOfFrame(nal.Data)) {
			flush()
		}
		if vcl {
			auHasSlice = true
			auSliceCount++
		}
	}

	t.Logf("NALs=%d VCL=%d accessUnits=%d multiSliceAUs=%d (fed %d frames)",
		totalNALs, totalVCL, accessUnits, multiSliceAUs, frameCount)

	if accessUnits == 0 {
		t.Fatal("no access units decoded from the encoder output")
	}
	// Every input frame must map to at most one access unit: if the AU count
	// tracked the NAL count instead, the old one-sample-per-NAL bug is back.
	if accessUnits > frameCount {
		t.Fatalf("aggregated %d access units for %d input frames — slices are not being grouped", accessUnits, frameCount)
	}
	if totalVCL > accessUnits && multiSliceAUs == 0 {
		t.Fatalf("encoder emitted %d slice NALs across %d AUs but no multi-slice AU was aggregated", totalVCL, accessUnits)
	}
}
