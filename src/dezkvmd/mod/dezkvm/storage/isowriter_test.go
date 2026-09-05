package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

// writeTestImage runs a full Begin + WriteImage cycle against a regular file
// standing in for the block device and returns the resulting device content.
func writeTestImage(t *testing.T, imageSize int, deviceSize int) ([]byte, []byte) {
	t.Helper()
	dir := t.TempDir()

	image := make([]byte, imageSize)
	if _, err := rand.Read(image); err != nil {
		t.Fatal(err)
	}

	devicePath := filepath.Join(dir, "device.bin")
	if err := os.WriteFile(devicePath, bytes.Repeat([]byte{0xEE}, deviceSize), 0644); err != nil {
		t.Fatal(err)
	}

	job := NewISOWriteJob()
	if err := job.Begin("test.iso", int64(imageSize)); err != nil {
		t.Fatal(err)
	}
	if err := job.WriteImage(devicePath, bytes.NewReader(image)); err != nil {
		t.Fatalf("WriteImage failed: %v", err)
	}

	status := job.Status()
	if status.State != ISOWriteStateDone {
		t.Fatalf("expected state done, got %s (%s)", status.State, status.Message)
	}
	if status.WrittenB != int64(imageSize) {
		t.Fatalf("expected %d bytes written, got %d", imageSize, status.WrittenB)
	}
	if status.VerifiedB != int64(imageSize) {
		t.Fatalf("expected %d bytes verified, got %d", imageSize, status.VerifiedB)
	}

	device, err := os.ReadFile(devicePath)
	if err != nil {
		t.Fatal(err)
	}
	return image, device
}

// TestWriteImageAligned writes an image whose size is an exact multiple of the
// sector alignment (like a real ISO) and checks the device content matches.
func TestWriteImageAligned(t *testing.T) {
	imageSize := 6 * 1024 * 1024 // spans two copy buffers, aligned
	image, device := writeTestImage(t, imageSize, imageSize+64*1024)
	if !bytes.Equal(device[:imageSize], image) {
		t.Fatal("device content does not match the image")
	}
	// Bytes past the image must be untouched.
	for i := imageSize; i < len(device); i++ {
		if device[i] != 0xEE {
			t.Fatalf("byte %d past the image was modified", i)
		}
	}
}

// TestWriteImageUnalignedTail writes an image with a tail that is not sector
// aligned; the tail must arrive intact and padding must stay within one sector.
func TestWriteImageUnalignedTail(t *testing.T) {
	imageSize := 5*1024*1024 + 1234
	image, device := writeTestImage(t, imageSize, imageSize+64*1024)
	if !bytes.Equal(device[:imageSize], image) {
		t.Fatal("device content does not match the image")
	}
	// With O_DIRECT the remainder of the final sector is zero-padded; with the
	// buffered fallback (filesystems without O_DIRECT support, e.g. tmpfs) the
	// tail is written exactly and the old content stays. Both are valid.
	padEnd := roundUpToAlign(imageSize)
	for i := imageSize; i < padEnd; i++ {
		if device[i] != 0 && device[i] != 0xEE {
			t.Fatalf("byte %d in the padding region has unexpected value 0x%02X", i, device[i])
		}
	}
	for i := padEnd; i < len(device); i++ {
		if device[i] != 0xEE {
			t.Fatalf("byte %d past the padded image was modified", i)
		}
	}
}

// TestWriteImageSmall writes an image smaller than one copy buffer.
func TestWriteImageSmall(t *testing.T) {
	imageSize := 4096 + 42
	image, device := writeTestImage(t, imageSize, 1024*1024)
	if !bytes.Equal(device[:imageSize], image) {
		t.Fatal("device content does not match the image")
	}
}

// TestWriteImageRejectsConcurrent ensures a second Begin fails while a job runs.
func TestWriteImageRejectsConcurrent(t *testing.T) {
	job := NewISOWriteJob()
	if err := job.Begin("a.iso", 10); err != nil {
		t.Fatal(err)
	}
	if err := job.Begin("b.iso", 10); err == nil {
		t.Fatal("expected second Begin to fail while job is running")
	}
	job.Abort(os.ErrClosed)
	if err := job.Begin("c.iso", 10); err != nil {
		t.Fatalf("Begin after Abort should succeed, got: %v", err)
	}
}

// TestVerifyDetectsCorruption runs the verify phase against a device whose
// content differs from the source hash by one byte and expects it to fail.
func TestVerifyDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	imageSize := 2 * 1024 * 1024
	image := make([]byte, imageSize)
	if _, err := rand.Read(image); err != nil {
		t.Fatal(err)
	}

	// The device carries a corrupted copy of the image (one byte flipped).
	corrupted := append([]byte(nil), image...)
	corrupted[imageSize/2] ^= 0xFF
	devicePath := filepath.Join(dir, "device.bin")
	if err := os.WriteFile(devicePath, corrupted, 0644); err != nil {
		t.Fatal(err)
	}

	job := NewISOWriteJob()
	if err := job.Begin("test.iso", int64(imageSize)); err != nil {
		t.Fatal(err)
	}
	job.addWritten(int64(imageSize))

	srcHash := sha256.New()
	srcHash.Write(image)
	if err := job.verifyPhase(devicePath, srcHash); err == nil {
		t.Fatal("expected verification to detect the corrupted byte")
	}
}
