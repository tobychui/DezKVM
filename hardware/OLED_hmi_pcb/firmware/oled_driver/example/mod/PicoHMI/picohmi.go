package picohmi

import (
	"bufio"
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	"go.bug.st/serial"
)

const ENABLE_DEBUG = false

const (
	// Display geometry (8x8 font on 128x64 OLED)
	OLEDCols         = 16
	OLEDRows         = 8
	OLEDMaxCharCount = OLEDCols * OLEDRows // 128

	// LTV command bytes
	cmdClearScreen      byte = 0x01
	cmdSetScreenContent byte = 0x03
	cmdWriteRawFontBuf  byte = 0x04
	cmdWriteCharAtIdx   byte = 0x06
	cmdGetUUID          byte = 'u' // 0x75
	cmdUUIDResponse     byte = 0x62

	// Firmware response bytes
	respOK  byte = 0xF0
	respErr byte = 0xF1

	// Touch event bytes
	CMD_EVENT_TOUCH_A_DOWN       = 0xB0
	CMD_EVENT_TOUCH_A_UP         = 0xB1
	CMD_EVENT_TOUCH_B_DOWN       = 0xB2
	CMD_EVENT_TOUCH_B_UP         = 0xB3
	CMD_EVENT_TOUCH_A_LONG_PRESS = 0xB4
	CMD_EVENT_TOUCH_B_LONG_PRESS = 0xB5
)

// Config holds the connection and callback configuration for the display.
type Config struct {
	SerialDevice       string
	BaudRate           int
	ButtonEventHandler func(button int, event int)
}

// Display represents a connected PicoHMI OLED display.
type Display struct {
	Connected bool
	Config    *Config

	/* internal */
	serialPort   serial.Port
	serialReader *bufio.Reader
}

// NewDisplay creates a new Display instance with the given config.
func NewDisplay(config *Config) (*Display, error) {
	if config == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if config.BaudRate == 0 {
		config.BaudRate = 115200
	}
	display := &Display{
		Config: config,
	}
	return display, nil
}

// Connect establishes a connection to the PicoHMI device via the configured serial port.
func (d *Display) Connect() error {
	mode := &serial.Mode{
		BaudRate: d.Config.BaudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(d.Config.SerialDevice, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port %s: %w", d.Config.SerialDevice, err)
	}
	d.serialPort = port
	d.serialReader = bufio.NewReader(port)
	d.Connected = true

	// Set a reasonable default read timeout for normal operations
	d.serialPort.SetReadTimeout(300 * time.Millisecond)

	// Allow the firmware to finish booting before sending commands.
	time.Sleep(300 * time.Millisecond)
	d.Reset()                         // Clear any boot messages and ensure we're in a known state.
	time.Sleep(50 * time.Millisecond) // Short delay after reset before sending commands
	return nil
}

// Disconnect closes the serial connection and releases resources.
func (d *Display) Disconnect() error {
	if d.serialPort == nil {
		return nil
	}
	d.Connected = false
	return d.serialPort.Close()
}

// GetUUID requests and returns the device UUID.
// The device responds with [0x25][0x62][36-char UUID string].
func (d *Display) GetUUID() (string, error) {
	if !d.Connected {
		return "", fmt.Errorf("display not connected")
	}

	// Send UUID request command
	packet := []byte{cmdGetUUID}
	n, err := d.serialPort.Write(packet)
	if err != nil {
		return "", fmt.Errorf("serial write failed: %w", err)
	}
	if n != len(packet) {
		return "", fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}

	time.Sleep(50 * time.Millisecond)

	// Read length byte (should be 0x25 = 37)
	lengthByte, err := d.serialReader.ReadByte()
	if err != nil {
		return "", fmt.Errorf("failed to read length byte: %w", err)
	}
	if lengthByte != 0x25 {
		return "", fmt.Errorf("unexpected length byte: 0x%02X (expected 0x25)", lengthByte)
	}

	// Read type byte (should be 0x62)
	typeByte, err := d.serialReader.ReadByte()
	if err != nil {
		return "", fmt.Errorf("failed to read type byte: %w", err)
	}
	if typeByte != cmdUUIDResponse {
		return "", fmt.Errorf("unexpected type byte: 0x%02X (expected 0x62)", typeByte)
	}

	// Read 36-byte UUID string
	uuidBytes := make([]byte, 36)
	n, err = d.serialReader.Read(uuidBytes)
	if err != nil {
		return "", fmt.Errorf("failed to read UUID: %w", err)
	}
	if n != 36 {
		return "", fmt.Errorf("incomplete UUID read: %d/36 bytes", n)
	}

	//Check if the UUID is valid (first char should start with 2 = type DISPLAY)
	if len(uuidBytes) > 0 && uuidBytes[0] != '2' {
		return "", fmt.Errorf("invalid UUID: %s", string(uuidBytes))
	}
	return string(uuidBytes), nil
}

func (d *Display) Reset() error {
	if !d.Connected {
		return fmt.Errorf("display not connected")
	}

	packet := []byte{0xCF, 0xCF, 0xCF} // CMD_RESET
	n, err := d.serialPort.Write(packet)
	if err != nil {
		return fmt.Errorf("serial write failed: %w", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}

	// Flush receive buffer during reset wait period
	// The firmware may send error messages if it's in an unknown state when receiving the first 0xCF
	// Temporarily set a very short timeout for the flush operation
	d.serialPort.SetReadTimeout(5 * time.Millisecond)

	deadline := time.Now().Add(250 * time.Millisecond)
	buf := make([]byte, 128)
	for time.Now().Before(deadline) {
		// Try to read and discard any available bytes
		n, err := d.serialPort.Read(buf)
		if err != nil || n == 0 {
			// No data available, sleep briefly before next attempt
			time.Sleep(5 * time.Millisecond)
		}
		// If we got data, continue immediately to drain more
	}

	// Restore normal read timeout
	d.serialPort.SetReadTimeout(300 * time.Millisecond)

	// Reset the bufio reader to ensure buffered data is cleared
	d.serialReader.Reset(d.serialPort)

	return nil
}

// Clear sends CMD_CLEAR_SCREEN, blanking the OLED display.
func (d *Display) Clear() error {
	if !d.Connected {
		return fmt.Errorf("display not connected")
	}
	// LTV packet: [0x01] [0x01]
	packet := []byte{0x01, cmdClearScreen}
	return d.sendPacketAndAck(packet)
}

// DrawText renders text onto the OLED screen.
// The text may contain newlines; each line maps to one row of the 16-column grid.
// Lines longer than 16 characters are truncated. Up to 8 lines are shown;
// excess lines are discarded. Unused cells are padded with spaces.
//
// Uses CMD_SET_SCREEN_CONTENT to send all 128 characters in one packet.
// Requires firmware with 130-byte buffer (1 length + 1 type + 128 data).
func (d *Display) DrawText(text string) error {
	if !d.Connected {
		return fmt.Errorf("display not connected")
	}

	lines := strings.Split(text, "\n")
	if len(lines) > OLEDRows {
		lines = lines[:OLEDRows]
	}

	// Build a 128-byte buffer (16 cols × 8 rows), all spaces by default
	buf := make([]byte, OLEDMaxCharCount)
	for i := range buf {
		buf[i] = 0x20 // space
	}

	// Fill in the text line by line
	for row, line := range lines {
		runes := []rune(line)
		if len(runes) > OLEDCols {
			runes = runes[:OLEDCols]
		}
		for col, ch := range runes {
			// Only printable ASCII (0x20–0x7E) is stored in EEPROM.
			if ch < 0x20 || ch > 0x7E {
				ch = 0x20
			}
			buf[row*OLEDCols+col] = byte(ch)
		}
	}

	// LTV packet: [0x81] [0x03] [128 bytes]
	// Length = 1 (type) + 128 (data) = 129 = 0x81
	// Total packet size: 130 bytes
	packet := make([]byte, 2+OLEDMaxCharCount)
	packet[0] = byte(1 + OLEDMaxCharCount) // 0x81 = 129
	packet[1] = cmdSetScreenContent
	copy(packet[2:], buf)
	return d.sendPacketAndAck(packet)
}

// DrawTextAt writes a string starting at the given grid column and row (0-based).
// Characters that fall beyond column 15 are truncated.
func (d *Display) DrawTextAt(col int, row int, text string) error {
	if !d.Connected {
		return fmt.Errorf("display not connected")
	}
	if col < 0 || col >= OLEDCols || row < 0 || row >= OLEDRows {
		return fmt.Errorf("position (%d,%d) out of range", col, row)
	}

	runes := []rune(text)
	maxChars := OLEDCols - col
	if len(runes) > maxChars {
		runes = runes[:maxChars]
	}

	for i, ch := range runes {
		if ch < 0x20 || ch > 0x7E {
			ch = 0x20
		}
		idx := uint8(row*OLEDCols + col + i)
		// LTV packet: [0x03] [0x06] [idx] [ascii]
		packet := []byte{0x03, cmdWriteCharAtIdx, idx, byte(ch)}
		if err := d.sendPacketAndAck(packet); err != nil {
			return fmt.Errorf("DrawTextAt char %d: %w", i, err)
		}
	}
	return nil
}

// DrawImage renders a Go image.Image onto the OLED using 8x8 pixel cells.
// The image is scaled to 128x64 using nearest-neighbour resampling.
func (d *Display) DrawImage(img image.Image) error {
	if !d.Connected {
		return fmt.Errorf("display not connected")
	}
	const displayW, displayH = 128, 64
	scaled := resampleNearest(img, displayW, displayH)

	const cols, rows, cellPx = OLEDCols, OLEDRows, 8
	totalCells := cols * rows

	for idx := 0; idx < totalCells; idx++ {
		col := idx % cols
		row := idx / cols
		originX := col * cellPx
		originY := row * cellPx

		fontData := make([]byte, cellPx) // 8 bytes for 8x8 cell
		for gy := 0; gy < cellPx; gy++ {
			for gx := 0; gx < cellPx; gx++ {
				if pixelOn(scaled, originX+gx, originY+gy) {
					fontData[gy] |= 1 << uint(7-gx)
				}
			}
		}

		// LTV: [len=2+8] [0x04] [position] [fontData...]
		pktLen := byte(2 + len(fontData))
		packet := make([]byte, 1+int(pktLen))
		packet[0] = pktLen
		packet[1] = cmdWriteRawFontBuf
		packet[2] = byte(idx)
		copy(packet[3:], fontData)
		if err := d.sendPacketAndAck(packet); err != nil {
			return fmt.Errorf("DrawImage cell %d: %w", idx, err)
		}
	}
	return nil
}

// DrawImageFromFile loads an image from disk and calls DrawImage.
func (d *Display) DrawImageFromFile(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open image file: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}
	return d.DrawImage(img)
}

// sendPacketAndAck writes a raw LTV packet to the device and waits for the 0xF0 ACK.
// Polls for the ACK with a timeout to handle both fast commands (~20ms) and slow
// commands like full-screen renders (~1.5s).
func (d *Display) sendPacketAndAck(packet []byte) error {
	if ENABLE_DEBUG {
		fmt.Printf("[DEBUG] Sending packet: len=%d, type=0x%02X\n", len(packet), packet[1])
		fmt.Println("[DEBUG] Packet data (hex):", fmt.Sprintf("% X", packet))
	}

	// Now send the command
	n, err := d.serialPort.Write(packet)
	if err != nil {
		return fmt.Errorf("serial write failed: %w", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}

	// Poll for ACK. Full screen renders can take 2-3s on slower hardware.
	startTime := time.Now()
	deadline := time.Now().Add(3 * time.Second)
	attemptCount := 0
	respBuf := make([]byte, 1)
	var allReceived []byte

	time.Sleep(30 * time.Millisecond) // Initial delay before polling for ACK

	for time.Now().Before(deadline) {
		attemptCount++
		n, err := d.serialPort.Read(respBuf)
		if err != nil || n == 0 {
			continue
		}

		if ENABLE_DEBUG {
			fmt.Println("[DEBUG] Received response byte:", fmt.Sprintf("0x%02X", respBuf[0]))
		}

		elapsed := time.Since(startTime)
		allReceived = append(allReceived, respBuf[0])
		if ENABLE_DEBUG {
			fmt.Printf("[DEBUG] Attempt %d @ %v: received byte 0x%02X (total received so far: %X)\n",
				attemptCount, elapsed, respBuf[0], allReceived)
		}

		if respBuf[0] == respErr {
			return fmt.Errorf("device returned error (0xF1)")
		}
		if respBuf[0] != respOK {
			// Not the ACK we expected - could be extra data, keep polling
			if ENABLE_DEBUG {
				fmt.Printf("[DEBUG] Not an ACK (0xF0), continuing...\n")
			}
			continue
		}
		// Got the ACK!
		if ENABLE_DEBUG {
			fmt.Printf("[DEBUG] SUCCESS: Got ACK 0xF0 after %d attempts in %v\n", attemptCount, elapsed)
		}
		return nil
	}

	if ENABLE_DEBUG {
		fmt.Printf("[DEBUG] TIMEOUT after %d attempts, received bytes: %X\n", attemptCount, allReceived)
	}
	return fmt.Errorf("timeout waiting for ACK (5s)")
}

// resampleNearest scales src to dstW×dstH using nearest-neighbour interpolation.
func resampleNearest(src image.Image, dstW, dstH int) *image.NRGBA {
	srcB := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := 0; dy < dstH; dy++ {
		sy := srcB.Min.Y + dy*srcB.Dy()/dstH
		for dx := 0; dx < dstW; dx++ {
			sx := srcB.Min.X + dx*srcB.Dx()/dstW
			dst.Set(dx, dy, src.At(sx, sy))
		}
	}
	return dst
}

// pixelOn returns true when the pixel at (x, y) should be lit.
// Transparent pixels are treated as off; luminance threshold is 127.
func pixelOn(img image.Image, x, y int) bool {
	r, g, b, a := img.At(x, y).RGBA()
	if a < 0x8000 {
		return false
	}
	lum := (19595*r + 38470*g + 7471*b) >> 24
	return lum > 127
}
