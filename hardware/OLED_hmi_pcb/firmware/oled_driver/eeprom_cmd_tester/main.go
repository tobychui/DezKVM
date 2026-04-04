package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"go.bug.st/serial"
)

var (
	port     = flag.String("port", "COM5", "Serial port to use")
	baud     = flag.Int("baud", 115200, "Baud rate for serial communication")
	ascii    = flag.Int("ascii", 0x20, "ASCII code to write (default: 0x20 ' ', converted to address 0 in EEPROM)")
	writeHex = flag.String("write", "", "32-byte hex string to write to EEPROM (64 hex chars, e.g., 'FFFF...')")
)

const (
	CMD_WRITE_FONT_DATA = 0xC3
	CMD_LOAD_FONT_DATA  = 0xC4
	FONT_CHAR_SIZE      = 32
)

func main() {
	flag.Parse()

	if *writeHex == "" {
		log.Fatal("Please provide hex data with -write flag (64 hex characters for 32 bytes)")
	}

	// Parse hex string
	hexStr := strings.ReplaceAll(*writeHex, " ", "")
	hexStr = strings.ReplaceAll(hexStr, "0x", "")
	hexStr = strings.ReplaceAll(hexStr, ",", "")

	data, err := hex.DecodeString(hexStr)
	if err != nil {
		log.Fatalf("Failed to parse hex string: %v", err)
	}

	// Pad with 0x00 if less than 32 bytes, or truncate/error if more
	if len(data) > FONT_CHAR_SIZE {
		log.Fatalf("Data too large: got %d bytes, maximum %d bytes", len(data), FONT_CHAR_SIZE)
	}

	if len(data) < FONT_CHAR_SIZE {
		log.Printf("Data size is %d bytes, padding with 0x00 to reach %d bytes", len(data), FONT_CHAR_SIZE)
		padded := make([]byte, FONT_CHAR_SIZE)
		copy(padded, data)
		data = padded
	}

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

	// Write font data to EEPROM
	if err := writeFontData(serialPort, uint8(*ascii), data); err != nil {
		log.Fatalf("Failed to write font data: %v", err)
	}

	log.Printf("✓ Successfully wrote 32 bytes to EEPROM for ASCII 0x%02X ('%c')", *ascii, rune(*ascii))

	// Read back and verify
	log.Println("Reading back data to verify...")
	readData, err := readFontData(serialPort, uint8(*ascii))
	if err != nil {
		log.Fatalf("Failed to read font data: %v", err)
	}

	// Compare
	if !compareData(data, readData) {
		log.Fatal("❌ Data mismatch! Write verification failed.")
	}

	log.Println("✓ Data verified successfully!")
}

// writeFontData sends CMD_WRITE_FONT_DATA command using LTV format
func writeFontData(port serial.Port, ascii uint8, data []byte) error {
	if len(data) != FONT_CHAR_SIZE {
		return fmt.Errorf("invalid data size: %d", len(data))
	}

	// Build LTV packet: [length] [type] [ascii] [32 bytes data]
	// Length = 0x22 (34 bytes: 1 type + 1 ascii + 32 data, not counting length byte itself)
	packet := make([]byte, 35)
	packet[0] = 0x22 // Length
	packet[1] = CMD_WRITE_FONT_DATA
	packet[2] = ascii
	copy(packet[3:], data)

	log.Printf("Sending %d bytes to EEPROM for ASCII 0x%02X...", len(packet), ascii)
	log.Printf("Packet: % X", packet)

	// Send entire packet
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

	log.Println("Device responded: 0xF0 (success)")
	return nil
}

// readFontData sends CMD_LOAD_FONT_DATA command and reads back the data
func readFontData(port serial.Port, ascii uint8) ([]byte, error) {
	// Build LTV packet: [length] [type] [ascii]
	// Length = 0x02 (2 bytes: 1 type + 1 ascii)
	packet := []byte{0x02, CMD_LOAD_FONT_DATA, ascii}

	log.Printf("Sending read command for ASCII 0x%02X...", ascii)

	// Send packet
	n, err := port.Write(packet)
	if err != nil {
		return nil, fmt.Errorf("write failed: %v", err)
	}
	if n != len(packet) {
		return nil, fmt.Errorf("incomplete write: %d/%d", n, len(packet))
	}

	// Read response (32 hex bytes + newline)
	time.Sleep(300 * time.Millisecond)
	reader := bufio.NewReader(port)
	response := make([]byte, 32)
	for i := 0; i < 32; i++ {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("failed to read byte %d: %v", i, err)
		}
		response[i] = b
	}

	if len(response) != FONT_CHAR_SIZE {
		return nil, fmt.Errorf("invalid response size: got %d bytes, expected %d", len(response), FONT_CHAR_SIZE)
	}

	log.Printf("Read back: % X", response)
	return response, nil
}

// compareData compares two byte slices
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
