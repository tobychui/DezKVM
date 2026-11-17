package videnc

/*
	jpegcheck.go

	Fast structural check of a baseline JPEG frame. The Rockchip hardware
	JPEG decoder (mjpeg_rkmpp) stops producing frames for the rest of the
	session after it is fed a frame whose headers are missing or whose
	entropy-coded data is garbage, which a USB capture glitch can produce.
	The software decoder just skips such frames. Frames that fail this check
	are dropped before they reach the hardware decoder.

	The check is a single pass over the bytes (~60 KB per 1080p frame), no
	decoding is done.
*/

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// checkBaselineJPEG returns an error when b is not a well-formed baseline
// (or extended sequential) JPEG with a layout the Rockchip decoder supports.
func checkBaselineJPEG(b []byte) error {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return errors.New("no SOI marker")
	}

	sawSOF := false
	i := 2
	// Header segments up to and including SOS
	for {
		// Markers may be preceded by any number of 0xFF fill bytes
		if i >= len(b) || b[i] != 0xFF {
			return fmt.Errorf("expected a marker at offset %d", i)
		}
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			return errors.New("truncated before SOS")
		}
		marker := b[i]
		i++

		switch {
		case marker == 0xD9:
			return errors.New("EOI before SOS")
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			continue // standalone markers carry no length
		}

		if i+2 > len(b) {
			return errors.New("truncated segment length")
		}
		segLen := int(binary.BigEndian.Uint16(b[i:]))
		if segLen < 2 || i+segLen > len(b) {
			return fmt.Errorf("bad length %d for marker 0x%02X", segLen, marker)
		}
		seg := b[i+2 : i+segLen]
		i += segLen

		switch marker {
		case 0xC0, 0xC1: // baseline / extended sequential DCT
			if err := checkSOF(seg); err != nil {
				return err
			}
			sawSOF = true
		case 0xC2, 0xC3, 0xC5, 0xC6, 0xC7, 0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF:
			return fmt.Errorf("unsupported JPEG process (SOF 0x%02X)", marker)
		case 0xDA: // SOS
			if !sawSOF {
				return errors.New("SOS before SOF")
			}
			return checkEntropyData(b[i:])
		}
	}
}

// checkSOF validates the frame header: 8 bit, non-empty, and either
// grayscale or YCbCr 4:2:0 / 4:2:2 / 4:4:4.
func checkSOF(seg []byte) error {
	if len(seg) < 6 {
		return errors.New("short SOF")
	}
	if seg[0] != 8 {
		return fmt.Errorf("unsupported sample precision %d", seg[0])
	}
	height := binary.BigEndian.Uint16(seg[1:])
	width := binary.BigEndian.Uint16(seg[3:])
	comps := int(seg[5])
	if width == 0 || height == 0 {
		return errors.New("zero image size")
	}
	if len(seg) < 6+3*comps {
		return errors.New("short SOF component list")
	}
	switch comps {
	case 1:
		return nil
	case 3:
		y := seg[7]
		cb, cr := seg[10], seg[13]
		if cb != 0x11 || cr != 0x11 {
			return fmt.Errorf("unsupported chroma sampling 0x%02X/0x%02X", cb, cr)
		}
		if y != 0x11 && y != 0x21 && y != 0x22 {
			return fmt.Errorf("unsupported luma sampling 0x%02X", y)
		}
		return nil
	}
	return fmt.Errorf("unsupported component count %d", comps)
}

// checkEntropyData scans the entropy-coded data after SOS. Inside it, 0xFF
// may only be followed by 0x00 (stuffing), a restart marker, or EOI. Random
// data almost never satisfies this, so it catches garbage scans cheaply.
func checkEntropyData(d []byte) error {
	for j := 0; j < len(d); j++ {
		if d[j] != 0xFF {
			continue
		}
		k := j + 1
		for k < len(d) && d[k] == 0xFF {
			k++ // fill bytes
		}
		if k >= len(d) {
			return errors.New("truncated at a marker")
		}
		switch m := d[k]; {
		case m == 0x00 || (m >= 0xD0 && m <= 0xD7):
			j = k
		case m == 0xD9:
			return nil // EOI; capture buffers may carry padding after it
		default:
			return fmt.Errorf("invalid marker 0xFF%02X in scan data", m)
		}
	}
	return errors.New("no EOI marker")
}
