package serial

// serial.go — cross-platform serial port wrapper over go.bug.st/serial.
// Exposes the same Port / Config / OpenPort surface used by kvmswitch.go.

import (
	"fmt"
	"io"
	"time"

	goserial "go.bug.st/serial"
)

// Port wraps a go.bug.st/serial port.
type Port struct {
	inner       goserial.Port
	readTimeout time.Duration
}

// Config holds the configuration for opening a serial port.
type Config struct {
	Name        string        // e.g. "COM3" or "/dev/ttyACM0"
	Baud        int           // Baud rate, e.g. 115200
	ReadTimeout time.Duration // Per-Read timeout; 0 means blocking
}

// OpenPort opens and configures a serial port with 8N1 settings.
func OpenPort(cfg *Config) (*Port, error) {
	mode := &goserial.Mode{
		BaudRate: cfg.Baud,
		DataBits: 8,
		Parity:   goserial.NoParity,
		StopBits: goserial.OneStopBit,
	}

	p, err := goserial.Open(cfg.Name, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Name, err)
	}

	if cfg.ReadTimeout > 0 {
		if err := p.SetReadTimeout(cfg.ReadTimeout); err != nil {
			p.Close()
			return nil, fmt.Errorf("set read timeout: %w", err)
		}
	}

	// Discard any stale bytes sitting in the driver buffer.
	_ = p.ResetInputBuffer()
	_ = p.ResetOutputBuffer()

	return &Port{inner: p, readTimeout: cfg.ReadTimeout}, nil
}

// Read reads up to len(b) bytes.  Returns a timeout error when the deadline
// expires with no data, consistent with the rest of the codebase.
func (p *Port) Read(b []byte) (int, error) {
	n, err := p.inner.Read(b)
	if err != nil {
		return n, err
	}
	if n == 0 && p.readTimeout > 0 {
		return 0, fmt.Errorf("serial read timeout")
	}
	return n, nil
}

// ReadFull reads exactly len(b) bytes from the port.
func (p *Port) ReadFull(b []byte) (int, error) {
	return io.ReadFull(p, b)
}

// Write writes b to the port.
func (p *Port) Write(b []byte) (int, error) {
	return p.inner.Write(b)
}

// Flush discards unread input and unsent output.
func (p *Port) Flush() error {
	_ = p.inner.ResetInputBuffer()
	_ = p.inner.ResetOutputBuffer()
	return nil
}

// Close closes the port.
func (p *Port) Close() error {
	return p.inner.Close()
}
