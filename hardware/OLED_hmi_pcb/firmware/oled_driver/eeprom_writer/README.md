# EEPROM Writer

This tool generates font bitmaps from a TrueType font and writes them — along with boot animation frames — to the OLED driver board's 24LC32 EEPROM (4096 bytes) via serial communication.

## Prerequisites

- Go 1.24.1 or later
- OLED driver board flashed with the latest firmware, connected via USB serial
- A TrueType font file (`.ttf`)

## Build

```bat
go build -o eeprom_write.exe
```

## Flashing the EEPROM

Use `write_eeprom.bat` to write both the font data and boot animation frames in one step.

**Before running**, open `write_eeprom.bat` and set the correct COM port and baud rate at the top:

```bat
set COM_PORT=COM5
set BAUDRATE=115200
```

Then run:

```bat
.\write_eeprom.bat
```

This will:
1. Write all 95 printable ASCII characters (0x20–0x7E) as 8×8 bitmaps using `protracker.ttf`
2. Write up to 3 boot animation frames from the `./bootanimation/` folder

## Write Modes

### Font mode (default)

Generates bitmaps for all printable ASCII characters and writes them to EEPROM.

```bat
.\eeprom_write.exe -port=COM5 -baudrate=115200 -font="protracker.ttf" -fontwidth=8 -fontheight=8 -fontsize=8 -offsetx=0 -offsety=1
```

Add `-nowrite` to generate and preview bitmaps in `./tmp/` without writing to EEPROM.

### Frame mode

Reads PNG images from `./bootanimation/`, converts them to SSD1306 page format, and writes them to EEPROM.

```bat
.\eeprom_write.exe -port=COM5 -baudrate=115200 -mode=frame
```

## Command Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `COM5` | Serial port |
| `-baudrate` | `115200` | Baud rate |
| `-mode` | `font` | Write mode: `font` or `frame` |
| `-font` | `./font.ttf` | Path to TrueType font file |
| `-fontwidth` | `16` | Glyph width in pixels |
| `-fontheight` | `16` | Glyph height in pixels |
| `-fontsize` | `14` | Font render size in points |
| `-offsetx` | `0` | Horizontal render offset |
| `-offsety` | `0` | Vertical render offset |
| `-tmpdir` | `./tmp` | Directory to save bitmap previews |
| `-nowrite` | `false` | Generate bitmaps only, skip EEPROM write |

## Test Scripts (`tests/`)

These scripts use the `eeprom_write.exe` from the parent directory. They are useful for previewing fonts or writing alternative fonts during development.

| Script | Description |
|--------|-------------|
| `generate_default.bat` | Preview 16×16 font glyphs (`font.ttf`) without writing |
| `generate_dot_gothic.bat` | Preview 16×16 DotGothic16 glyphs without writing |
| `generate_protracker_8x8.bat` | Preview 8×8 Protracker glyphs without writing |
| `write_default.bat` | Write 16×16 `font.ttf` to EEPROM |
| `write_dot_gothic.bat` | Write 16×16 DotGothic16 to EEPROM |
| `write_protracker_8x8.bat` | Write 8×8 Protracker font to EEPROM |
| `write_bootanimation_frames.bat` | Write boot animation frames to EEPROM |

`generate_*` scripts add `-nowrite` so they produce bitmap previews in `./tmp/` only.

## EEPROM Layout

The 24LC32 has 4096 bytes total. Layout with 8×8 font (`FONT_CHAR_SIZE` = 8 bytes):

| Region | Address Range | Size |
|--------|--------------|------|
| ASCII 0x20–0x7E font data (95 chars × 8 bytes) | 0x0000–0x02F7 | 760 bytes |
| Reserved / padding | 0x02F8–0x02FF | 8 bytes |
| Boot animation frames | 0x0300–0x0EFF | 3072 bytes |
| Unused | 0x0F00–0x0FFF | 256 bytes |

## Boot Animation Specifications

- **Image format:** PNG, monochrome (1-bit), converted via threshold (pixels darker than 50% grey = on)
- **Resolution:** exactly **128 × 64 pixels**
- **Storage format:** SSD1306 page format — 8 pages × 128 columns, 1 byte per column encodes 8 vertical pixels (LSB = topmost pixel of page)
- **Bytes per frame:** 1024
- **Maximum frames:** **3**
- **Frame addresses:**
  - Frame 0: `0x0300`–`0x06FF`
  - Frame 1: `0x0700`–`0x0AFF`
  - Frame 2: `0x0B00`–`0x0EFF`
- **Frame files:** place PNGs named in alphabetical order inside `./bootanimation/`. Files beyond the first 3 are ignored with a warning.

## LTV Protocol Commands

All serial packets use Length-Type-Value format. The length byte does not count itself.

| Command | Code | Packet Format |
|---------|------|---------------|
| Write font to EEPROM | `0xC3` | `[len] [0xC3] [ascii] [N bytes font data]` |
| Read font from EEPROM | `0xC4` | `[0x02] [0xC4] [ascii]` → returns N raw bytes |
| Display font on OLED | `0xC5` | `[0x02] [0xC5] [ascii]` |
| Write raw EEPROM | `0xC6` | `[len] [0xC6] [addr_h] [addr_l] [data, max 32 bytes]` |
| Read raw EEPROM | `0xC7` | `[0x04] [0xC7] [addr_h] [addr_l] [count]` → returns count raw bytes |

On success the firmware responds with `0xF0`; on error `0xF1`.

## Troubleshooting

**Port not found:** Check Device Manager for the correct COM port and ensure CH552G drivers are installed.

**Freezes / no response:** The firmware's `CMD_MAX_LENGTH` buffer must be large enough for a full raw-write packet (36 bytes). Check that `oled_driver.h` defines `CMD_MAX_LENGTH` correctly.

**Characters look wrong:** Adjust `-fontsize`, `-offsetx`, `-offsety` flags, or use `-nowrite` to preview in `./tmp/` first.

**Animation colours inverted:** The tool bitwise-inverts (`^b`) all frame bytes before writing so that black PNG pixels illuminate OLED pixels. If the display is still inverted, check `loadAndConvertFrame()` in `main.go`.

