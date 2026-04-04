package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"go.bug.st/serial"
)

var (
	port          = flag.String("port", "COM5", "Serial port to connect to the microcontroller")
	baudrate      = flag.Int("baudrate", 115200, "Baud rate for serial communication")
	fontPath      = flag.String("font", "./font.ttf", "Path to TrueType font file")
	tmpDir        = flag.String("tmpdir", "./tmp", "Directory to save generated bitmaps")
	noWrite       = flag.Bool("nowrite", false, "If set, will not write to EEPROM, only generate bitmaps and read back for verification")
	font_offset_x = flag.Int("offsetx", 0, "Horizontal offset for character rendering (default: 0)")
	font_offset_y = flag.Int("offsety", 0, "Vertical offset for character rendering (default: 0)")
	fontWidth     = flag.Int("fontwidth", 16, "Font glyph width in pixels (must match firmware FONT_WIDTH)")
	fontHeight    = flag.Int("fontheight", 16, "Font glyph height in pixels (must match firmware FONT_HEIGHT)")
	fontSize      = flag.Int("fontsize", 14, "Font size to render in points (adjust to fit within fontWidth/fontHeight)")
	writeMode     = flag.String("mode", "font", "Write mode: 'font' to write font data, 'frame' to write boot animation frames from ./bootanimation/*.png")
)

const (
	CMD_WRITE_FONT_DATA   = 0xC3
	CMD_LOAD_FONT_DATA    = 0xC4
	CMD_LOAD_FONT_TO_OLED = 0xC5
	CMD_WRITE_EEPROM_RAW  = 0xC6
	CMD_READ_EEPROM_RAW   = 0xC7

	BOOT_ANIMATION_START_ADDR = 0x0300
	BOOT_ANIMATION_FRAME_SIZE = 1024
	EEPROM_PAGE_SIZE          = 32
	MAX_BOOT_FRAMES           = 3
)

// Derived values computed from flags in main()
var (
	fontBytesPerRow int
	fontCharSize    int
)

func main() {
	flag.Parse()

	// Derive font storage sizes from flag values
	fontBytesPerRow = (*fontWidth + 7) / 8
	fontCharSize = *fontHeight * fontBytesPerRow
	log.Printf("Font config: %dx%d, %d bytes/row, %d bytes/char",
		*fontWidth, *fontHeight, fontBytesPerRow, fontCharSize)

	// Open serial port
	serialPort, err := serial.Open(*port, &serial.Mode{
		BaudRate: *baudrate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		log.Fatalf("Failed to open serial port: %v", err)
	}
	defer serialPort.Close()

	if *writeMode == "frame" {
		writeBootAnimationFrames(serialPort)
		return
	}

	// Font mode: generate bitmaps and write to EEPROM

	// Create tmp directory
	os.RemoveAll(*tmpDir)
	if err := os.MkdirAll(*tmpDir, 0755); err != nil {
		log.Fatalf("Failed to create tmp directory: %v", err)
	}

	// Load font
	fontBytes, err := ioutil.ReadFile(*fontPath)
	if err != nil {
		log.Fatalf("Failed to read font file: %v", err)
	}

	ttfFont, err := truetype.Parse(fontBytes)
	if err != nil {
		log.Fatalf("Failed to parse font: %v", err)
	}

	// Step 1: Generate bitmaps for all characters
	log.Println("=== Step 1: Generating bitmaps ===")
	for ascii := 0x20; ascii <= 0x7E; ascii++ {
		char := rune(ascii)
		bitmapPath := filepath.Join(*tmpDir, fmt.Sprintf("char_0x%02X.png", ascii))

		// Check if bitmap already exists
		if _, err := os.Stat(bitmapPath); err == nil {
			log.Printf("Skipping '%c' (0x%02X) - bitmap already exists", char, ascii)
			continue
		}

		log.Printf("Generating bitmap for character: '%c' (0x%02X)", char, ascii)

		// Generate bitmap
		bitmap := generateCharBitmap(ttfFont, char)

		// Save bitmap to tmp folder
		if err := saveBitmap(bitmap, bitmapPath); err != nil {
			log.Printf("Warning: Failed to save bitmap: %v", err)
		} else {
			log.Printf("✓ Saved bitmap: %s", bitmapPath)
		}
	}

	log.Println("\n=== Step 2: Writing and verifying to EEPROM ===")
	log.Printf("Connected to %s at %d baud", *port, *baudrate)
	time.Sleep(2 * time.Second) // Wait for device to be ready

	if *noWrite {
		log.Println("NO WRITE MODE: Skipping EEPROM write")
		os.Exit(0)
	}
	// Step 2: Write and test all characters
	for ascii := 0x20; ascii <= 0x7E; ascii++ {
		char := rune(ascii)
		bitmapPath := filepath.Join(*tmpDir, fmt.Sprintf("char_0x%02X.png", ascii))

		log.Printf("Writing character: '%c' (0x%02X)", char, ascii)

		// Load bitmap from file
		bitmap, err := loadBitmap(bitmapPath)
		if err != nil {
			log.Fatalf("Failed to load bitmap for 0x%02X: %v", ascii, err)
		}

		// Convert to hex data format
		hexData := bitmapToHexData(bitmap)

		// Write to EEPROM via serial
		if err := writeFontData(serialPort, uint8(ascii), hexData); err != nil {
			log.Fatalf("Failed to write font data for 0x%02X: %v", ascii, err)
		}

		time.Sleep(100 * time.Millisecond) // Small delay before reading back

		// Verify by reading back
		readData, err := readFontData(serialPort, uint8(ascii))
		if err != nil {
			log.Fatalf("Failed to read font data for 0x%02X: %v (TERMINATING)", ascii, err)
		}

		// Compare
		if !compareData(hexData, readData) {
			log.Fatalf("Data mismatch for character 0x%02X (TERMINATING)", ascii)
		}

		log.Printf("✓ Character '%c' (0x%02X) written and verified", char, ascii)
		time.Sleep(100 * time.Millisecond) // Small delay between characters
	}

	log.Println("\n=== All characters written and verified successfully! ===")
}

// writeBootAnimationFrames reads PNGs from ./bootanimation/, converts them to
// SSD1306 page format, and writes them to EEPROM using CMD_WRITE_EEPROM_RAW.
func writeBootAnimationFrames(serialPort serial.Port) {
	files, err := filepath.Glob("./bootanimation/*.png")
	if err != nil {
		log.Fatalf("Failed to list boot animation frames: %v", err)
	}
	sort.Strings(files)

	if len(files) == 0 {
		log.Fatal("No PNG files found in ./bootanimation/")
	}
	if len(files) > MAX_BOOT_FRAMES {
		log.Printf("Warning: Only %d frames fit in EEPROM, ignoring %d extra files",
			MAX_BOOT_FRAMES, len(files)-MAX_BOOT_FRAMES)
		files = files[:MAX_BOOT_FRAMES]
	}

	_, err = serialPort.Write([]byte{0xCF, 0xCF, 0xCF}) // Send in a force reset
	if err != nil {
		panic(err)
	}

	log.Printf("=== Writing %d boot animation frames ===", len(files))
	log.Printf("Connected to %s at %d baud", *port, *baudrate)
	time.Sleep(2 * time.Second)

	for i, path := range files {
		log.Printf("\n--- Frame %d: %s ---", i, path)

		frameData, err := loadAndConvertFrame(path)
		if err != nil {
			log.Fatalf("Failed to load frame %d (%s): %v", i, path, err)
		}
		log.Println("✓ Frame loaded and converted to SSD1306 format")
		baseAddr := BOOT_ANIMATION_START_ADDR + i*BOOT_ANIMATION_FRAME_SIZE

		// Write in 32-byte page chunks
		log.Println("Writing to EEPROM in 32-byte chunks...")
		for offset := 0; offset < BOOT_ANIMATION_FRAME_SIZE; offset += EEPROM_PAGE_SIZE {
			addr := uint16(baseAddr + offset)
			chunk := frameData[offset : offset+EEPROM_PAGE_SIZE]
			if err := writeEepromRaw(serialPort, addr, chunk); err != nil {
				log.Fatalf("Failed to write frame %d at 0x%04X: %v", i, addr, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		log.Printf("Write complete, verifying...")

		// Verify by reading back in 32-byte chunks
		for offset := 0; offset < BOOT_ANIMATION_FRAME_SIZE; offset += EEPROM_PAGE_SIZE {
			addr := uint16(baseAddr + offset)
			readBack, err := readEepromRaw(serialPort, addr, EEPROM_PAGE_SIZE)
			if err != nil {
				log.Fatalf("Failed to read back frame %d at 0x%04X: %v", i, addr, err)
			}
			expected := frameData[offset : offset+EEPROM_PAGE_SIZE]
			if !compareData(expected, readBack) {
				log.Fatalf("Verification failed for frame %d at 0x%04X\n  expected: % X\n  got:      % X",
					i, addr, expected, readBack)
			}
			log.Printf("✓ Chunk at 0x%04X verified", addr)
			time.Sleep(50 * time.Millisecond)
		}

		log.Printf("✓ Frame %d written and verified (0x%04X–0x%04X)",
			i, baseAddr, baseAddr+BOOT_ANIMATION_FRAME_SIZE-1)
	}

	log.Printf("\n=== All %d boot animation frames written successfully! ===", len(files))
}

// loadAndConvertFrame loads a 128x64 PNG and converts it to SSD1306 page format (1024 bytes).
func loadAndConvertFrame(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	if bounds.Dx() != 128 || bounds.Dy() != 64 {
		return nil, fmt.Errorf("frame image must be 128x64, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Convert to SSD1306 page format: 8 pages x 128 columns
	// Each byte = 8 vertical pixels, LSB = top pixel of the page
	data := make([]byte, BOOT_ANIMATION_FRAME_SIZE)
	for page := 0; page < 8; page++ {
		for col := 0; col < 128; col++ {
			var b byte
			for bit := 0; bit < 8; bit++ {
				y := page*8 + bit
				x := col
				grayColor := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
				if grayColor.Y < 128 { // dark pixel = on
					b |= 1 << bit
				}
			}
			data[page*128+col] = ^b
		}
	}
	return data, nil
}

// writeEepromRaw sends CMD_WRITE_EEPROM_RAW to write up to 32 bytes at a 16-bit EEPROM address.
func writeEepromRaw(port serial.Port, addr uint16, data []byte) error {
	if len(data) > EEPROM_PAGE_SIZE {
		return fmt.Errorf("data too large: %d (max %d)", len(data), EEPROM_PAGE_SIZE)
	}

	// Packet: [length] [0xC6] [addr_h] [addr_l] [data...]
	payloadLen := 1 + 2 + len(data) // type + addr(2) + data
	packet := make([]byte, 1+payloadLen)
	packet[0] = uint8(payloadLen)
	packet[1] = CMD_WRITE_EEPROM_RAW
	packet[2] = uint8(addr >> 8)
	packet[3] = uint8(addr & 0xFF)
	copy(packet[4:], data)

	log.Printf("Writing to EEPROM at address 0x%04X: % X", addr, data)
	n, err := port.Write(packet)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d", n, len(packet))
	}

	// Wait for 0xF0 response
	time.Sleep(100 * time.Millisecond)
	log.Println("Waiting for EEPROM write response...")
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

// readEepromRaw sends CMD_READ_EEPROM_RAW to read count bytes from a 16-bit EEPROM address.
func readEepromRaw(port serial.Port, addr uint16, count int) ([]byte, error) {
	// Packet: [0x04] [0xC7] [addr_h] [addr_l] [count]
	packet := []byte{0x04, CMD_READ_EEPROM_RAW, uint8(addr >> 8), uint8(addr & 0xFF), uint8(count)}

	n, err := port.Write(packet)
	if err != nil {
		return nil, err
	}
	if n != len(packet) {
		return nil, fmt.Errorf("incomplete write: %d/%d", n, len(packet))
	}

	// Read response (count raw bytes)
	time.Sleep(300 * time.Millisecond)
	response := make([]byte, count)
	reader := bufio.NewReader(port)
	for i := 0; i < count; i++ {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("failed to read byte %d: %v", i, err)
		}
		response[i] = b
	}
	return response, nil
}

func sendResetPacket(serialPort serial.Port) {
	// Send 3 consecutive 0xCF bytes to trigger emergency reset
	for i := 0; i < 3; i++ {
		log.Printf("Reset byte %d: 0xCF", i+1)
		n, err := serialPort.Write([]byte{0xCF})
		if err != nil {
			log.Fatalf("Failed to send reset command: %v", err)
		}
		if n != 1 {
			log.Fatalf("Incomplete write for reset command: %d/%d", n, 1)
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Println("Reset command sent")
	time.Sleep(200 * time.Millisecond)
}

// generateCharBitmap renders a character to a fontWidth x fontHeight bitmap
func generateCharBitmap(ttfFont *truetype.Font, char rune) [][]bool {
	img := image.NewGray(image.Rect(0, 0, *fontWidth, *fontHeight))

	// Fill with white (background)
	for y := 0; y < *fontHeight; y++ {
		for x := 0; x < *fontWidth; x++ {
			img.SetGray(x, y, color.Gray{255})
		}
	}

	// Create freetype context
	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(ttfFont)
	c.SetFontSize(float64(*fontSize)) // Adjust size to fit glyph height
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.Black)

	// Draw the character
	pt := freetype.Pt(*font_offset_x, *fontHeight-*font_offset_y)
	_, err := c.DrawString(string(char), pt)
	if err != nil {
		log.Printf("Warning: Failed to draw character: %v", err)
	}

	// Convert to boolean bitmap (true = black pixel)
	bitmap := make([][]bool, *fontHeight)
	for y := 0; y < *fontHeight; y++ {
		bitmap[y] = make([]bool, *fontWidth)
		for x := 0; x < *fontWidth; x++ {
			gray := img.GrayAt(x, y).Y
			bitmap[y][x] = gray < 128 // Threshold
		}
	}

	return bitmap
}

// bitmapToHexData converts bitmap to byte array (fontHeight rows x fontBytesPerRow bytes, MSB-aligned)
func bitmapToHexData(bitmap [][]bool) []byte {
	data := make([]byte, fontCharSize)
	for row := 0; row < *fontHeight; row++ {
		for col := 0; col < *fontWidth; col++ {
			if bitmap[row][col] {
				byteIdx := col / 8
				bitIdx := 7 - (col % 8)
				data[row*fontBytesPerRow+byteIdx] |= 1 << bitIdx
			}
		}
	}
	return data
}

// saveBitmap saves the bitmap as a PNG file
func saveBitmap(bitmap [][]bool, path string) error {
	img := image.NewGray(image.Rect(0, 0, *fontWidth, *fontHeight))
	for y := 0; y < *fontHeight; y++ {
		for x := 0; x < *fontWidth; x++ {
			if bitmap[y][x] {
				img.SetGray(x, y, color.Gray{0}) // Black
			} else {
				img.SetGray(x, y, color.Gray{255}) // White
			}
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, img)
}

// loadBitmap loads a PNG file and converts it to boolean bitmap
func loadBitmap(path string) ([][]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}

	// Convert to boolean bitmap (true = black pixel)
	bitmap := make([][]bool, *fontHeight)
	for y := 0; y < *fontHeight; y++ {
		bitmap[y] = make([]bool, *fontWidth)
		for x := 0; x < *fontWidth; x++ {
			grayColor := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			bitmap[y][x] = grayColor.Y < 128 // Threshold
		}
	}

	return bitmap, nil
}

// writeFontData sends CMD_WRITE_FONT_DATA command
func writeFontData(port serial.Port, ascii uint8, data []byte) error {
	if len(data) != fontCharSize {
		return fmt.Errorf("invalid data size: %d (expected %d)", len(data), fontCharSize)
	}

	// Build LTV packet: [length] [type] [ascii] [fontCharSize bytes data]
	payloadLen := 1 + 1 + fontCharSize // type + ascii + data
	packet := make([]byte, 1+payloadLen)
	packet[0] = uint8(payloadLen)
	packet[1] = CMD_WRITE_FONT_DATA
	packet[2] = ascii
	copy(packet[3:], data)

	// Send packet
	log.Printf("Writing font data for ASCII 0x%02X to EEPROM: % X", ascii, data)
	log.Printf("Packet: % X", packet)
	n, err := port.Write(packet)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("incomplete write: %d/%d", n, len(packet))
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
	log.Printf("Response received: 0x%02X\n", response)
	log.Println("✓ Write successful")

	return nil
}

// readFontData sends CMD_LOAD_FONT_DATA command and reads back the data
func readFontData(port serial.Port, ascii uint8) ([]byte, error) {
	// Build LTV packet: [length] [type] [ascii]
	packet := []byte{0x02, CMD_LOAD_FONT_DATA, ascii}

	// Send packet
	log.Printf("Requesting font data for ASCII 0x%02X...", ascii)
	n, err := port.Write(packet)
	if err != nil {
		return nil, err
	}
	if n != len(packet) {
		return nil, fmt.Errorf("incomplete write: %d/%d", n, len(packet))
	}

	// Read response (fontCharSize raw bytes)
	time.Sleep(300 * time.Millisecond)
	response := make([]byte, fontCharSize)
	reader := bufio.NewReader(port)
	for i := 0; i < fontCharSize; i++ {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("failed to read byte %d: %v", i, err)
		}
		response[i] = b
	}

	log.Printf("✓ Read successful: % X", response)
	return response, nil
}

// compareData compares two byte arrays
func compareData(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
