package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

var (
	asciiStr    = flag.String("i", "A", "ASCII chars for fill/single mode; PNG file path or base64-encoded PNG for image mode")
	fontsize    = flag.Int("s", 8, "Font/cell size in pixels: 8 (8×8, 16×8 grid) or 16 (16×16, 8×4 grid)")
	displayMode = flag.String("m", "fill", "Mode: 'single', 'fill', 'image', or 'clear'")
	writepos    = flag.String("idx", "0,0", "For 'single' mode: grid position to write char, as 'col,row' (0-based)")
	baud        = flag.Int("baud", 115200, "Baud rate for serial communication")
	port        = flag.String("port", "COM5", "Serial port to use")
)

const (
	CMD_CLEAR_SCREEN       = 0x01
	CMD_SET_SCREEN_CONTENT = 0x03
	CMD_WRITE_RAW_FONT_BUF = 0x04
	CMD_WRITE_CHAR_AT_IDX  = 0x06
	CMD_LOAD_FONT_TO_OLED  = 0xC5
)

func main() {
	flag.Parse()

	// Open serial port
	serialPort, err := serial.Open(*port, &serial.Mode{
		BaudRate: *baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		log.Fatalf("Failed to open serial port %s: %v", *port, err)
	}
	defer serialPort.Close()

	log.Printf("Connected to %s at %d baud", *port, *baud)
	time.Sleep(2 * time.Second) // Wait for device to be ready

	switch *displayMode {
	case "single":
		chars := []rune(*asciiStr)
		if len(chars) == 0 {
			log.Fatal("No characters provided")
		}

		// Determine grid dimensions from font size.
		cols := 16
		if *fontsize == 16 {
			cols = 8
		}

		// Parse -idx "col,row" (0-based).
		parts := strings.SplitN(*writepos, ",", 2)
		if len(parts) != 2 {
			log.Fatalf("Invalid -idx value %q: expected 'col,row'", *writepos)
		}
		startCol, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		startRow, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || startCol < 0 || startRow < 0 || startCol >= cols {
			log.Fatalf("Invalid -idx value %q", *writepos)
		}

		// Cap output to the remaining cells on the same row.
		maxChars := cols - startCol
		if len(chars) > maxChars {
			log.Printf("Warning: input truncated from %d to %d chars (end of row %d)",
				len(chars), maxChars, startRow)
			chars = chars[:maxChars]
		}

		for i, ch := range chars {
			idx := uint8(startRow*cols + startCol + i)
			asciiCode := uint8(ch)
			log.Printf("Writing '%c' (0x%02X) at cell idx=%d (col=%d row=%d)",
				ch, asciiCode, idx, startCol+i, startRow)
			if err := sendCharAtIdx(serialPort, idx, asciiCode); err != nil {
				log.Fatalf("Failed to write char '%c' at idx %d: %v", ch, idx, err)
			}
			log.Printf("✓ '%c' written at idx=%d", ch, idx)
		}
	case "fill":
		chars := []rune(*asciiStr)
		if len(chars) == 0 {
			log.Fatal("No characters provided")
		}
		screenbuffer := make([]byte, len(chars))
		for i := 0; i < len(chars); i++ {
			screenbuffer[i] = byte(chars[i])
		}
		log.Printf("Sending screen content: %s", string(screenbuffer))
		if err := sendScreenContent(serialPort, screenbuffer); err != nil {
			log.Fatalf("Failed to send screen content: %v", err)
		}
		log.Println("✓ Screen content sent")
	case "image":
		img, err := loadImage(*asciiStr)
		if err != nil {
			log.Fatalf("Failed to load image: %v", err)
		}
		if err := sendImage(serialPort, img, *fontsize); err != nil {
			log.Fatalf("Failed to send image: %v", err)
		}
		log.Println("✓ Image sent")
	case "clear":
		log.Println("Clearing OLED screen...")
		if err := clearScreen(serialPort); err != nil {
			log.Fatalf("Failed to clear screen: %v", err)
		}
		log.Println("✓ Screen cleared")
	default:
		log.Println("Invalid mode. Use 'single', 'fill', 'image' or 'clear'.")
		return
	}

	log.Println("Done!")
}

// clearScreen sends CMD_CLEAR_SCREEN and waits for 0xF0 response
func clearScreen(port serial.Port) error {
	// LTV packet: [0x01] [0x01] (length=1, type=CMD_CLEAR_SCREEN)
	packet := []byte{0x01, CMD_CLEAR_SCREEN}
	log.Printf("Packet: % X", packet)
	n, err := port.Write(packet)
	if err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}
	time.Sleep(20 * time.Millisecond)
	reader := bufio.NewReader(port)
	response, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}
	if response != 0xF0 {
		return fmt.Errorf("unexpected response: 0x%02X (expected 0xF0)", response)
	}
	return nil
}

// sendScreenContent sends CMD_SET_SCREEN_CONTENT with 32 bytes and waits for 0xF0 response
func sendScreenContent(port serial.Port, content []byte) error {
	// LTV packet: [0x21] [0x03] [32 bytes] — length=0x21(33)=type(1)+data(32)
	packet := make([]byte, 2+len(content))
	packet[0] = byte(1 + len(content)) // length = type(1) + data(len(content))
	packet[1] = CMD_SET_SCREEN_CONTENT
	copy(packet[2:], content)
	log.Printf("Packet: % X", packet)
	n, err := port.Write(packet)
	if err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}
	time.Sleep(50 * time.Millisecond)
	reader := bufio.NewReader(port)
	response, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}
	if response != 0xF0 {
		return fmt.Errorf("unexpected response: 0x%02X (expected 0xF0)", response)
	}
	return nil
}

// sendCharAtIdx sends CMD_WRITE_CHAR_AT_IDX: writes one ASCII char at a specific
// cell index on the display without clearing the rest of the screen.
// Packet: [0x03] [0x06] [idx] [ascii]
func sendCharAtIdx(port serial.Port, idx uint8, ascii uint8) error {
	packet := []byte{0x03, CMD_WRITE_CHAR_AT_IDX, idx, ascii}
	log.Printf("  Packet: % X", packet)
	n, err := port.Write(packet)
	if err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}
	time.Sleep(50 * time.Millisecond)
	reader := bufio.NewReader(port)
	resp, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read ACK: %v", err)
	}
	if resp != 0xF0 {
		return fmt.Errorf("unexpected ACK: 0x%02X (expected 0xF0)", resp)
	}
	return nil
}

// displayCharOnOLED sends CMD_LOAD_FONT_TO_OLED and waits for 0xF0 response
func displayCharOnOLED(port serial.Port, ascii uint8) error {
	// LTV packet: [0x02] [0xC5] [ascii]
	packet := []byte{0x02, CMD_LOAD_FONT_TO_OLED, ascii}

	log.Printf("Packet: % X", packet)
	n, err := port.Write(packet)
	if err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}

	// Wait for response 0xF0 (success)
	time.Sleep(300 * time.Millisecond)
	reader := bufio.NewReader(port)
	response, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if response != 0xF0 {
		return fmt.Errorf("unexpected response: 0x%02X (expected 0xF0)", response)
	}

	return nil
}

// loadImage loads a PNG/JPEG from a file path, or decodes a base64-encoded PNG/JPEG.
func loadImage(input string) (image.Image, error) {
	// Try to open as a file first.
	if f, err := os.Open(input); err == nil {
		defer f.Close()
		img, _, err := image.Decode(f)
		return img, err
	}

	// Fall back to base64: try standard encoding, then URL-safe.
	imgData, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		imgData, err = base64.URLEncoding.DecodeString(input)
		if err != nil {
			imgData, err = base64.RawStdEncoding.DecodeString(input)
			if err != nil {
				return nil, fmt.Errorf("not a valid file path or base64 image: %v", err)
			}
		}
	}
	img, _, err := image.Decode(bytes.NewReader(imgData))
	return img, err
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
	r, g, b, a := img.At(x, y).RGBA() // 0–65535 range
	if a < 0x8000 {
		return false
	}
	// BT.601 luma, scaled back to 0–255
	lum := (19595*r + 38470*g + 7471*b) >> 24
	return lum > 127
}

// sendImage scales the source image to 128×64, slices it into character cells,
// and sends each cell via CMD_WRITE_RAW_FONT_BUF, waiting for ACK between cells.
//
// fontSizePx controls the cell dimension:
//
//	8  → 16 cols × 8 rows = 128 cells, 8 bytes/cell  (matches FONT_CHAR_SIZE=8)
//	16 → 8 cols  × 4 rows = 32 cells, 32 bytes/cell  (matches FONT_CHAR_SIZE=32)
func sendImage(port serial.Port, src image.Image, fontSizePx int) error {
	const displayW, displayH = 128, 64
	img := resampleNearest(src, displayW, displayH)

	var cols, rows, cellPx, bytesPerRow int
	switch fontSizePx {
	case 16:
		cols, rows, cellPx, bytesPerRow = 8, 4, 16, 2
	default: // 8
		cols, rows, cellPx, bytesPerRow = 16, 8, 8, 1
	}
	charSize := cellPx * bytesPerRow
	totalCells := cols * rows

	for idx := 0; idx < totalCells; idx++ {
		col := idx % cols
		row := idx / cols
		originX := col * cellPx
		originY := row * cellPx

		// Build font_buf bytes: row-major, MSB = leftmost pixel within each byte.
		fontData := make([]byte, charSize)
		for gy := 0; gy < cellPx; gy++ {
			for gx := 0; gx < cellPx; gx++ {
				if pixelOn(img, originX+gx, originY+gy) {
					byteIdx := gx / 8
					bitIdx := uint(7 - (gx % 8))
					fontData[gy*bytesPerRow+byteIdx] |= 1 << bitIdx
				}
			}
		}

		log.Printf("Sending cell %d/%d (col=%d row=%d)...", idx+1, totalCells, col, row)
		if err := sendRawFontBuf(port, uint8(idx), fontData); err != nil {
			return fmt.Errorf("cell %d: %v", idx, err)
		}
	}
	return nil
}

// sendRawFontBuf sends a CMD_WRITE_RAW_FONT_BUF packet and waits for the 0xF0 ACK.
// Packet layout: [len=2+len(fontData)] [0x04] [position] [fontData...]
func sendRawFontBuf(port serial.Port, position uint8, fontData []byte) error {
	pktLen := byte(2 + len(fontData)) // type(1) + position(1) + data
	packet := make([]byte, 1+int(pktLen))
	packet[0] = pktLen
	packet[1] = CMD_WRITE_RAW_FONT_BUF
	packet[2] = position
	copy(packet[3:], fontData)

	log.Printf("  Packet[%d]: % X", position, packet)
	n, err := port.Write(packet)
	if err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d bytes", n, len(packet))
	}
	reader := bufio.NewReader(port)
	resp, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read ACK: %v", err)
	}
	if resp != 0xF0 {
		return fmt.Errorf("unexpected ACK: 0x%02X (expected 0xF0)", resp)
	}
	return nil
}
