# EEPROM Command Tester

A simple command-line tool for writing hex data to the OLED Driver EEPROM via serial port.

## Usage

### Write hex data to EEPROM

```bash
go run main.go -port COM5 -ascii 0x41 -write "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
```

### Parameters

- `-port`: Serial port to connect to (default: `COM5`)
- `-baud`: Baud rate for serial communication (default: `115200`)
- `-ascii`: ASCII code for the character to write (default: `0x41` for 'A')
- `-write`: Hex string to write to EEPROM (up to 64 hex characters for 32 bytes, shorter strings will be padded with 0x00)

### Example

Write a full 32-byte test pattern for ASCII 'B' (0x42):

```bash
go run main.go -port COM5 -ascii 0x42 -write "00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF00FF"
```

Write a shorter pattern (will be padded with 0x00):

```bash
go run main.go -ascii 0x43 -write "FFFF"
# This writes: FFFF000000000000000000000000000000000000000000000000000000000000
```

The tool will:
1. Connect to the serial port
2. Send the hex data using the LTV (Length-Type-Value) protocol
3. Wait for "OK" response from the device
4. Read back the data to verify the write was successful
5. Compare written vs. read data

## Build

```bash
go build -o eeprom_tester.exe
```

## Notes

- The hex string can be up to 64 characters (32 bytes)
- Shorter hex strings will be automatically padded with 0x00 bytes
- Longer hex strings will cause an error
- Data format: 16 rows × 2 bytes = 32 bytes per character
- LTV packet format: `[0x22] [0xC3] [ASCII] [32 bytes data]`
