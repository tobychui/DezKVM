# USB KVM Firmware - UART Operation Guide

This document describes how to operate the USB KVM device through UART serial communication.

## Connection Settings

- **Baud Rate**: 115200
- **Data Bits**: 8
- **Parity**: None
- **Stop Bits**: 1
- **Flow Control**: None

## Command Protocol

All commands are single ASCII characters sent over UART. The device responds with a binary protocol following this format:

```
<Length> <Command Identifier> <Payload>
```

**Note**: The Length byte includes only the Command Identifier and Payload bytes, not the Length byte itself.

## USB Mass Storage Commands

### Switch to KVM Host (`m`)
Switches the USB mass storage device to the KVM host side.

**Command**: `m` (0x6D)

**Usage**:
```
Send: m
```

### Switch to Remote PC (`n`)
Switches the USB mass storage device to the remote computer side.

**Command**: `n` (0x6E)

**Usage**:
```
Send: n
```

### Query USB Mass Storage Side (`y`)
Returns which side the USB mass storage is currently connected to.

**Command**: `y` (0x79)

**Usage**:
```
Send: y
Response: <Length> <Command ID> <Side>
```

## Device Identification

### Get Device UUID (`u`)
Returns the unique UUID (UUIDv4) of this KVM device.

**Command**: `u` (0x75)

**Response Format**:
```
<Length> 0x62 <UUID String>
```

**Example**:
```
Send: u
Response: 0x25 0x62 "ac1636c2-af4b-4241-8e2c-43e385c85602"
```

## ATX Power Control Commands

These commands are only available when `ENABLE_ATX_CTRL` is defined in the firmware.

### Report ATX Status (`a`)
Reports the current status of the ATX power and HDD LEDs.

**Command**: `a` (0x61)

**Response Format**:
```
0x02 0x61 <Status Byte>
```

The status byte contains the state of the power and HDD LEDs.

**Status Byte Format**:
- Bit 0: Power LED state (0 = OFF, 1 = ON)
- Bit 1: HDD LED state (0 = OFF, 1 = ON)
- Bits 2-7: Reserved

**Example Responses**:
```
Send: a
Response: 0x02 0x61 0x00  // Both LEDs off (PC is off)
Response: 0x02 0x61 0x01  // Power LED on, HDD LED off (PC is on, idle)
Response: 0x02 0x61 0x03  // Both LEDs on (PC is on, HDD active)
Response: 0x02 0x61 0x02  // Power LED off, HDD LED on (unusual state)
```

### Enable Automatic ATX Reporting (`i`)
Enables automatic reporting of ATX status changes.

**Command**: `i` (0x69)

**Usage**:
```
Send: i
```

After enabling, the device will automatically send ATX status updates when the power or HDD LED states change.

### Disable Automatic ATX Reporting (`o`)
Disables automatic reporting of ATX status changes.

**Command**: `o` (0x6F)

**Usage**:
```
Send: o
```

### Press Power Button (`p`)
Presses down the ATX power button (sets HIGH).

**Command**: `p` (0x70)

**Usage**:
```
Send: p
```

**Note**: You must release the button with the `s` command.

### Release Power Button (`s`)
Releases the ATX power button (sets LOW).

**Command**: `s` (0x73)

**Usage**:
```
Send: s
```

### Press Reset Button (`r`)
Presses down the ATX reset button (sets HIGH).

**Command**: `r` (0x72)

**Usage**:
```
Send: r
```

**Note**: You must release the button with the `d` command.

### Release Reset Button (`d`)
Releases the ATX reset button (sets LOW).

**Command**: `d` (0x64)

**Usage**:
```
Send: d
```

## LED Control Commands

Control the programmable LED (LED_PROG) on the device.

### LED Off (`0`)
Turns the programmable LED off.

**Command**: `0` (0x30)

**Usage**:
```
Send: 0
```

### LED On (`1`)
Turns the programmable LED on (solid).

**Command**: `1` (0x31)

**Usage**:
```
Send: 1
```

### LED Fast Blink (`2`)
Sets the LED to blink rapidly (200ms intervals).

**Command**: `2` (0x32)

**Usage**:
```
Send: 2
```

### LED Slow Blink (`3`)
Sets the LED to blink slowly (1000ms intervals).

**Command**: `3` (0x33)

**Usage**:
```
Send: 3
```

## System Commands

### Reset Echo (`f`)
Resets the communication and returns an acknowledgment.

**Command**: `f` (0x66)

**Response Format**:
```
0x01 0xFF
```

**Usage**:
```
Send: f
Response: 0x01 0xFF
```

## Error Responses

### Unknown Command (0xFE)
When an invalid command is sent, the device responds with:

```
0x01 0xFE
```

## Response Codes Summary

| Code | Description |
|------|-------------|
| 0xFE | Unknown command error |
| 0xFF | Reset echo acknowledgment |
| 0x60 | Reserved for future use |
| 0x61 | ATX status report |
| 0x62 | Device UUID |

## LED Behavior Notes

- **Default Mode**: By default (LED_MODE_AUTO), the LED toggles state each time a command is received
- **Manual Modes**: When using LED commands (`0`, `1`, `2`, `3`), the LED switches to manual control and stops auto-toggling
- **Startup Sequence**: On boot, the LED blinks 10 times rapidly to indicate initialization

## USB Mass Storage Switching

When switching the USB mass storage between KVM host and remote PC:

1. Power to the USB device is briefly cut
2. The data lines are switched
3. Power is restored with appropriate delays

**Delays**:
- Power switch delay: 100ms
- Data switch delay: 10ms

## ATX Control Notes

- The ATX control pins directly interface with the motherboard's front panel headers
- Button press commands (`p`, `r`) set the pins HIGH - you must remember to release them with (`s`, `d`)
- Typical power button press duration: 100-200ms for normal boot, 4-5 seconds for force shutdown
- Reset button press duration: 100-500ms

## Troubleshooting

**No response from device:**
- Check baud rate (should be 115200)
- Verify correct COM port / device path
- Ensure proper USB connection

**Commands not working:**
- Send reset echo command (`f`) and verify response (0x01 0xFF)
- Check if ENABLE_DEBUG was defined during compilation (not recommended for production)
- Verify ATX commands are only used when ENABLE_ATX_CTRL is defined

**LED not responding:**
- LED will not auto-toggle when in manual mode (`0`, `1`, `2`, `3` commands)
- Send LED command again to verify mode change

## Firmware Information

- **Target MCU**: CH552G
- **Clock Speed**: 24MHz (Internal)
- **Build Flags**:
  - `ENABLE_DEBUG`: Enable debug output (do not use in production)
  - `ENABLE_ATX_CTRL`: Enable ATX power control features
