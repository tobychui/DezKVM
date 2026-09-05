package storage

/*
	isowriter.go

	Writes ISO / raw disk images to a block device using only the Go standard
	library (no dd, no CGO).  Progress is tracked inside an ISOWriteJob so the
	web frontend can poll the current state while a write is in progress.

	The device is written with O_DIRECT so the data bypasses the kernel page
	cache and physically reaches the USB stick (the same technique used by
	`dd oflag=direct` and balenaEtcher).  This avoids page-cache aliasing
	between the whole-disk node and partition nodes.  After writing, the
	written range is read back (also with O_DIRECT) and its SHA-256 checksum
	is compared against the source data, so a flash is only reported as
	successful when the bytes on the stick provably match the image.
*/

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ISO write job states.
const (
	ISOWriteStateIdle      = "idle"
	ISOWriteStateWriting   = "writing"
	ISOWriteStateSyncing   = "syncing"
	ISOWriteStateVerifying = "verifying"
	ISOWriteStateDone      = "done"
	ISOWriteStateError     = "error"
)

// isoWriteBufferSize is the copy buffer size (4 MiB).
const isoWriteBufferSize = 4 * 1024 * 1024

// diskSectorAlign is the alignment used for O_DIRECT buffers and I/O lengths.
// 4096 satisfies both 512-byte and 4K-sector devices.
const diskSectorAlign = 4096

// ISOWriteJob tracks the state of one image-write operation on a disk.
// A single job instance is reused for consecutive writes on the same device;
// only one write can run at a time.
type ISOWriteJob struct {
	mu         sync.Mutex
	state      string
	isoName    string
	written    int64
	verified   int64
	total      int64 // 0 = unknown (e.g. streamed upload without a size field)
	message    string
	startedAt  time.Time
	finishedAt time.Time
}

// ISOWriteStatus is the JSON-serialisable snapshot of an ISOWriteJob.
type ISOWriteStatus struct {
	State     string  `json:"state"`
	ISOName   string  `json:"iso_name"`
	WrittenB  int64   `json:"written_b"`
	VerifiedB int64   `json:"verified_b"`
	TotalB    int64   `json:"total_b"` // 0 when unknown
	Percent   float64 `json:"percent"` // 0–100 of the current phase; -1 when total is unknown
	SpeedBps  int64   `json:"speed_bps"`
	Message   string  `json:"message"`
}

// NewISOWriteJob returns an idle job.
func NewISOWriteJob() *ISOWriteJob {
	return &ISOWriteJob{state: ISOWriteStateIdle}
}

// Status returns a snapshot of the current job state.
func (j *ISOWriteJob) Status() ISOWriteStatus {
	j.mu.Lock()
	defer j.mu.Unlock()

	status := ISOWriteStatus{
		State:     j.state,
		ISOName:   j.isoName,
		WrittenB:  j.written,
		VerifiedB: j.verified,
		TotalB:    j.total,
		Percent:   -1,
		Message:   j.message,
	}

	// Percent tracks the current phase: written bytes while writing,
	// verified bytes while verifying.
	phaseProgress := j.written
	phaseTotal := j.total
	if j.state == ISOWriteStateVerifying {
		phaseProgress = j.verified
		phaseTotal = j.written // verify target is what was written
	}
	if phaseTotal > 0 {
		status.Percent = float64(phaseProgress) / float64(phaseTotal) * 100
		if status.Percent > 100 {
			status.Percent = 100
		}
	}

	// Average throughput over the elapsed time of the active/last run.
	end := j.finishedAt
	if j.state == ISOWriteStateWriting || j.state == ISOWriteStateSyncing || j.state == ISOWriteStateVerifying {
		end = time.Now()
	}
	if !j.startedAt.IsZero() && end.After(j.startedAt) {
		elapsed := end.Sub(j.startedAt).Seconds()
		if elapsed > 0 {
			status.SpeedBps = int64(float64(j.written+j.verified) / elapsed)
		}
	}
	return status
}

// IsRunning reports whether a write is currently in progress.
func (j *ISOWriteJob) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == ISOWriteStateWriting || j.state == ISOWriteStateSyncing || j.state == ISOWriteStateVerifying
}

// Begin transitions the job to the writing state, or errors when a write is
// already in progress.  Callers must invoke Begin before WriteImage so that
// status polls never observe the stale state of a previous run; if a later
// preparation step fails before WriteImage runs, release the job with Abort.
func (j *ISOWriteJob) Begin(isoName string, total int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == ISOWriteStateWriting || j.state == ISOWriteStateSyncing || j.state == ISOWriteStateVerifying {
		return errors.New("another image write is already in progress")
	}
	j.state = ISOWriteStateWriting
	j.isoName = isoName
	j.written = 0
	j.verified = 0
	j.total = total
	j.message = ""
	j.startedAt = time.Now()
	j.finishedAt = time.Time{}
	return nil
}

// Abort marks a begun-but-never-started job as failed (e.g. the device could
// not be prepared after Begin succeeded).
func (j *ISOWriteJob) Abort(err error) {
	j.fail(err)
}

func (j *ISOWriteJob) setState(state string) {
	j.mu.Lock()
	j.state = state
	j.mu.Unlock()
}

func (j *ISOWriteJob) addWritten(n int64) {
	j.mu.Lock()
	j.written += n
	j.mu.Unlock()
}

func (j *ISOWriteJob) addVerified(n int64) {
	j.mu.Lock()
	j.verified += n
	j.mu.Unlock()
}

func (j *ISOWriteJob) finish() {
	j.mu.Lock()
	j.state = ISOWriteStateDone
	j.finishedAt = time.Now()
	j.mu.Unlock()
}

func (j *ISOWriteJob) fail(err error) {
	j.mu.Lock()
	j.state = ISOWriteStateError
	j.message = err.Error()
	j.finishedAt = time.Now()
	j.mu.Unlock()
}

// alignedBuffer allocates a byte slice whose backing array is aligned to
// diskSectorAlign, as required for O_DIRECT I/O.
func alignedBuffer(size int) []byte {
	raw := make([]byte, size+diskSectorAlign)
	misalign := int(uintptr(unsafe.Pointer(&raw[0])) % diskSectorAlign)
	off := 0
	if misalign != 0 {
		off = diskSectorAlign - misalign
	}
	return raw[off : off+size]
}

// roundUpToAlign rounds n up to the next multiple of diskSectorAlign.
func roundUpToAlign(n int) int {
	if n%diskSectorAlign == 0 {
		return n
	}
	return n + diskSectorAlign - n%diskSectorAlign
}

// openDirect opens path with O_DIRECT, falling back to buffered I/O when the
// kernel or device does not support direct I/O.  The returned bool reports
// whether direct I/O is in effect.
func openDirect(path string, flag int) (*os.File, bool, error) {
	f, err := os.OpenFile(path, flag|unix.O_DIRECT, 0)
	if err == nil {
		return f, true, nil
	}
	f, err = os.OpenFile(path, flag, 0)
	return f, false, err
}

// WriteImage streams src to the block device at diskPath.  Begin must have
// been called first (it records the image name and total size, and rejects
// concurrent writes).  The device must be unmounted before calling this
// function.  The call blocks until the write and read-back verification
// complete; poll Status from another goroutine for progress.
func (j *ISOWriteJob) WriteImage(diskPath string, src io.Reader) error {
	j.mu.Lock()
	started := j.state == ISOWriteStateWriting
	j.mu.Unlock()
	if !started {
		return errors.New("image write job was not started with Begin")
	}

	srcHash := sha256.New()
	if err := j.writePhase(diskPath, src, srcHash); err != nil {
		j.fail(err)
		return err
	}

	j.setState(ISOWriteStateVerifying)
	if err := j.verifyPhase(diskPath, srcHash); err != nil {
		j.fail(err)
		return err
	}

	j.finish()
	return nil
}

// writePhase copies src to the device, hashing the source data on the fly.
func (j *ISOWriteJob) writePhase(diskPath string, src io.Reader, srcHash hash.Hash) error {
	// Linux block devices always support O_DIRECT; the buffered fallback only
	// triggers on filesystems without direct I/O (e.g. tmpfs in unit tests).
	dev, directIO, err := openDirect(diskPath, os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("cannot open device %s for writing: %w", diskPath, err)
	}

	buf := alignedBuffer(isoWriteBufferSize)
	for {
		n, readErr := io.ReadFull(src, buf)
		if n > 0 {
			srcHash.Write(buf[:n])

			// O_DIRECT requires the I/O length to be sector aligned.  The
			// final chunk of an image may be unaligned: pad it with zeros up
			// to the next sector boundary.  The padding lands in the unused
			// region past the image (the image is validated to fit the disk,
			// and disks are always whole multiples of the sector size).
			writeLen := n
			if directIO && n%diskSectorAlign != 0 {
				padded := roundUpToAlign(n)
				for i := n; i < padded; i++ {
					buf[i] = 0
				}
				writeLen = padded
			}
			if _, writeErr := dev.Write(buf[:writeLen]); writeErr != nil {
				dev.Close()
				return fmt.Errorf("write to %s failed after %d bytes: %w", diskPath, j.Status().WrittenB, writeErr)
			}
			j.addWritten(int64(n))
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			dev.Close()
			return fmt.Errorf("read from image source failed: %w", readErr)
		}
	}

	// Flush the device write cache so all data is physically on the stick.
	j.setState(ISOWriteStateSyncing)
	if err := dev.Sync(); err != nil {
		dev.Close()
		return fmt.Errorf("final sync of %s failed: %w", diskPath, err)
	}
	if err := dev.Close(); err != nil {
		return fmt.Errorf("close of %s failed: %w", diskPath, err)
	}
	return nil
}

// verifyPhase reads the written range back from the device (bypassing the
// page cache) and compares its SHA-256 checksum against the source data.
func (j *ISOWriteJob) verifyPhase(diskPath string, srcHash hash.Hash) error {
	written := j.Status().WrittenB
	if written == 0 {
		return errors.New("nothing was written to the device")
	}

	dev, _, err := openDirect(diskPath, os.O_RDONLY)
	if err != nil {
		return fmt.Errorf("cannot re-open device %s for verification: %w", diskPath, err)
	}
	defer dev.Close()

	readHash := sha256.New()
	buf := alignedBuffer(isoWriteBufferSize)
	var remaining = written
	for remaining > 0 {
		// Read a full aligned chunk; only hash the bytes that belong to the image.
		want := int64(len(buf))
		if remaining < want {
			want = int64(roundUpToAlign(int(remaining)))
		}
		n, readErr := io.ReadFull(dev, buf[:want])
		if n > 0 {
			useful := int64(n)
			if useful > remaining {
				useful = remaining
			}
			readHash.Write(buf[:useful])
			remaining -= useful
			j.addVerified(useful)
		}
		if readErr != nil && remaining > 0 {
			return fmt.Errorf("read-back from %s failed with %d bytes left to verify: %w", diskPath, remaining, readErr)
		}
	}

	srcSum := srcHash.Sum(nil)
	devSum := readHash.Sum(nil)
	for i := range srcSum {
		if srcSum[i] != devSum[i] {
			return fmt.Errorf("verification failed: data on %s does not match the image (source sha256 %x, device sha256 %x)",
				diskPath, srcSum, devSum)
		}
	}
	return nil
}
