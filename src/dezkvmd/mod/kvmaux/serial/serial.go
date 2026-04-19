package serial

/*
	serial.go

	In linux kernel 6.1, there are some issue with USB CDC device close
	which may cause the device to be stuck in a bad state until unplugged.
	To work around this, we use direct serial I/O with termios configuration instead
	of relying on higher level libraries which may have hidden buffering or
	other behaviors that interfere with proper closing.

	Not sure if this is a Linux kernel bug or just a quirk of how CDC ACM devices work, but
	this implementation here works on my machine
*/

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Port represents an open serial port on Linux.
type Port struct {
	file        *os.File
	readTimeout time.Duration
}

// Config holds the configuration for opening a serial port.
type Config struct {
	Name        string        // Device path, e.g. /dev/ttyACM0
	Baud        int           // Baud rate, e.g. 115200
	ReadTimeout time.Duration // Read timeout; 0 means blocking
}

// baudRateMap maps integer baud rates to termios constants.
var baudRateMap = map[int]uint32{
	1200:    unix.B1200,
	2400:    unix.B2400,
	4800:    unix.B4800,
	9600:    unix.B9600,
	19200:   unix.B19200,
	38400:   unix.B38400,
	57600:   unix.B57600,
	115200:  unix.B115200,
	230400:  unix.B230400,
	460800:  unix.B460800,
	500000:  unix.B500000,
	576000:  unix.B576000,
	921600:  unix.B921600,
	1000000: unix.B1000000,
}

// OpenPort opens and configures a serial port with raw 8N1 settings.
func OpenPort(cfg *Config) (*Port, error) {
	baudConst, ok := baudRateMap[cfg.Baud]
	if !ok {
		return nil, fmt.Errorf("unsupported baud rate: %d", cfg.Baud)
	}

	// Open with O_NOCTTY to avoid becoming controlling terminal,
	// O_NONBLOCK to prevent open() itself from blocking on DCD
	f, err := os.OpenFile(cfg.Name, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", cfg.Name, err)
	}

	fd := int(f.Fd())

	// Clear O_NONBLOCK now that open succeeded — we want reads to
	// respect VTIME/VMIN or use SetDeadline via the poller
	if err := unix.SetNonblock(fd, false); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to clear O_NONBLOCK: %w", err)
	}

	// Get current termios
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("TCGETS failed: %w", err)
	}

	// Configure raw mode (equivalent to cfmakeraw)
	termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	termios.Oflag &^= unix.OPOST
	termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	termios.Cflag &^= unix.CSIZE | unix.PARENB
	termios.Cflag |= unix.CS8 | unix.CLOCAL | unix.CREAD

	// Set baud rate
	termios.Ispeed = baudConst
	termios.Ospeed = baudConst

	// Set read timeout via VMIN/VTIME
	// VMIN=0, VTIME=T means: return as soon as any data arrives,
	// or after T deciseconds (100ms units) if no data
	if cfg.ReadTimeout > 0 {
		deciseconds := int(cfg.ReadTimeout / (100 * time.Millisecond))
		if deciseconds < 1 {
			deciseconds = 1
		}
		if deciseconds > 255 {
			deciseconds = 255
		}
		termios.Cc[unix.VMIN] = 0
		termios.Cc[unix.VTIME] = uint8(deciseconds)
	} else {
		// Blocking read: wait for at least 1 byte
		termios.Cc[unix.VMIN] = 1
		termios.Cc[unix.VTIME] = 0
	}

	// Apply termios
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, termios); err != nil {
		f.Close()
		return nil, fmt.Errorf("TCSETS failed: %w", err)
	}

	// Flush any stale data in kernel buffers
	if err := unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH); err != nil {
		// Non-fatal — some drivers don't support TCFLSH
	}

	return &Port{
		file:        f,
		readTimeout: cfg.ReadTimeout,
	}, nil
}

// Read reads up to len(b) bytes from the serial port.
func (p *Port) Read(b []byte) (int, error) {
	n, err := p.file.Read(b)
	// On Linux, VTIME expiry with no data returns n=0, err=nil.
	// Convert to a timeout error so io.ReadFull works correctly.
	if n == 0 && err == nil {
		return 0, fmt.Errorf("serial read timeout")
	}
	return n, err
}

// ReadFull reads exactly len(b) bytes from the serial port.
func (p *Port) ReadFull(b []byte) (int, error) {
	return io.ReadFull(p, b)
}

// Write writes data to the serial port.
func (p *Port) Write(b []byte) (int, error) {
	return p.file.Write(b)
}

// Flush discards data in the kernel serial buffers.
func (p *Port) Flush() error {
	fd := int(p.file.Fd())
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH)
}

// Close closes the serial port.
func (p *Port) Close() error {
	if p.file != nil {
		err := p.file.Close()
		p.file = nil
		return err
	}
	return nil
}
