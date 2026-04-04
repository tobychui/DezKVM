# Hardware Guide

This guide walks you through building, flashing, and cabling the DezKVM IP-KVM system from scratch. It is written with first-time hardware builders in mind — do not skip sections, as each step builds on the previous one.

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Components](#components)
3. [Building the DezKVM PoC USB-KVM Module](#building-the-dezkvm-poc-usb-kvm-module)
4. [Flashing the Onboard CH552G MCU](#flashing-the-onboard-ch552g-mcu)
5. [Configuring the CH9329 Baud Rate](#configuring-the-ch9329-baud-rate)
6. [Connecting to a Host SBC (Multiple KVM Ports, No HDMI Switch)](#connecting-to-a-host-sbc-multiple-kvm-ports-no-hdmi-switch)
7. [Building and Connecting the ATX Board](#building-and-connecting-the-atx-board)
8. [Optional: OLED HMI Display](#optional-oled-hmi-display)
9. [Troubleshooting](#troubleshooting)

---

## System Overview

DezKVM is a fully **USB-protocol-based** IP-KVM system. Unlike PiKVM and similar projects, it does not require a dedicated HDMI switch, HDMI-to-CSI adapter, custom kernel drivers, or an FPGA. Everything on the DezKVM port module enumerates as standard USB devices — the host SBC sees a USB hub containing a video capture device, a USB-to-UART adapter, and a serial ACM device. This means the Linux kernel requires no patches or custom modules.

A complete system looks like this:

```
[ Host SBC (Orange Pi / Raspberry Pi / etc.) ]
         |
    USB 2.0 cable (one per KVM port)
         |
[ DezKVM Port Module ] ── HDMI ──► [ Remote Computer being controlled ]
         |                ─── USB-C (HID) ──────────────────────────►
         |                ─── RJ45 ATX cable ──► [ ATX Board ]
         |                                               |
         |                                   Dupont wires to front-panel header
```

You can connect **multiple DezKVM Port Modules** to a single host SBC — one per computer you want to control. The `dezkvmd` daemon on the SBC identifies each module by its unique UUID and routes keyboard/mouse/video to the correct session. There is no need for any external HDMI matrix switch or KVM switch box.

---

## Components

Before you start building, gather everything listed below.

### Per DezKVM Port Module

| Item | Notes |
|------|-------|
| DezKVM PoC PCB (v8 recommended) | Order from your PCB fab using the Gerber files in `DezKVM PoC/pcb/v8/` |
| MS2109 HDMI capture card module | A small plug-in module; search for "MS2109 HDMI capture card" — they are very inexpensive |
| All SMD components per BOM | BOM CSV is provided in `DezKVM PoC/pcb/v8/` |
| 3D-printed enclosure | STL files in `DezKVM PoC/3d_models/` (front, back panels + HDMI capture holder) |

### For ATX Power Control (optional, one per controlled computer)

| Item | Notes |
|------|-------|
| ATX Board PCB | Gerber files in `ATX board/PCB/` |
| RJ45 connector (18.3 mm pitch) | Connects the ATX board to the Port Module |
| 4× 2×2p 2.54 mm male pin headers | Front-panel connector blocks |
| 5× M3×5 mm screws | For mounting |
| Dupont wires | 8× Male-to-Female (front-panel to ATX board) + 8× Female-to-Female (ATX board to motherboard header) |
| 3D-printed parts | `ATX board/3d_models/` — PCIe bracket, mount, and optional cable press |

### Host SBC

Any Linux-capable single-board computer with USB 2.0 ports works. More USB ports means more KVM ports you can run simultaneously:

| SBC | Ethernet | USB Ports | Recommended Port Modules | Max Concurrent Sessions |
|-----|----------|-----------|--------------------------|-------------------------|
| Orange Pi Zero LTS | 100 Mbps | 3 | 1–2 | 1 |
| Orange Pi Zero 2 | 1 Gbps | 3 | 2–4 | 4 |
| NanoPi Zero 2 | 1 Gbps | 1 | 1–4 | 4 |
| Raspberry Pi 4 | 1 Gbps | 4 | 4 | 4 |

> You can always add more Port Modules. The limits above are practical bandwidth guidelines — each module uses ~80–90 Mbps at 1080p 25 fps MJPEG.

---

## Building the DezKVM PoC USB-KVM Module

### Step 1 — Order the PCB

Open `DezKVM PoC/pcb/v8/` and upload the Gerber zip file to a PCB fabrication service (JLCPCB, PCBWay, etc.). Standard 2-layer, 1.6 mm FR4 settings work fine. If the fab offers SMT assembly, the BOM CSV and Pick & Place CSV are also provided in that folder for JLCPCB assembly service.

### Step 2 — Solder the components

If you are assembling by hand, use the schematic PDF in `DezKVM PoC/pcb/v8/` as your reference. Key chips and their placement:

| Chip | Role | Notes |
|------|------|-------|
| SL2.1A | USB 2.0 hub | Groups all downstream devices under one USB tree |
| CH552G | Onboard MCU | Handles ATX control, USB mass-storage switching, UUID service, HDMI power control |
| CH340C | USB-to-UART bridge | Bridges the SBC's USB to the CH9329 |
| CH9329 | UART-to-USB-HID | Emulates keyboard and mouse on the remote side — no driver needed on the controlled PC |
| CH440G | USB signal switch | Routes the internal USB Type-C port between SBC and remote |
| MS2109 module | HDMI capture | Plug-in module; press it into the slot on the PCB |

> **First-time soldering tip:** Start with the smallest SMD components first (resistors and capacitors), then the ICs, and finish with connectors. For the QFN/SOP packages, use plenty of flux and a fine-tipped iron. Review the PCB silkscreen for all component orientations before powering on.

### Step 3 — Print and assemble the enclosure

Print the STL files from `DezKVM PoC/3d_models/`. The assembly consists of:
- **front.ipt / front.stl** — front plate with HDMI and USB-C cutouts
- **back.ipt / back.stl** — back plate with host-side USB-C and PROG header access
- **capture_holder_top / capture_holder_bottom** — holds the MS2109 module in the correct position

PLA at 0.2 mm layer height works well. Orient the flat face of each part down when printing to avoid supports.

### Step 4 — Ports at a glance

Once assembled, you will see:

**Remote side (faces the computer you are controlling):**
- HDMI input — connect to the remote computer's HDMI output
- RJ45 ATX control port — connect to the ATX board (optional)
- USB-C (upper) — USB mass-storage passthrough
- USB-C (lower, closer to HDMI) — USB HID (keyboard + mouse to remote PC)

**Host side (faces your SBC):**
- USB-C (USB 2.0) — single cable to your SBC; everything goes through this
- XH2.54 1×4p white header — alternative connection if you prefer a JST header over USB-C
- PROG header (unpopulated by default) — only needed for certain reprogramming scenarios

---

## Flashing the Onboard CH552G MCU

The CH552G is an 8051-based microcontroller that runs the ATX control, UUID service, and HDMI power management firmware. It is programmed using the Arduino IDE with the **ch55xduino** board support package.

### Prerequisites

1. Download and install [Arduino IDE](https://www.arduino.cc/en/software) (version 2.x recommended).
2. In Arduino IDE, go to **File → Preferences** and add this URL to the "Additional boards manager URLs" field:
   ```
   https://raw.githubusercontent.com/DeqingSun/ch55xduino/unreal/package_ch55xduino_mcs51_index.json
   ```
3. Open **Tools → Board → Boards Manager**, search for `CH55x`, and install the ch55xduino package.
4. Open the firmware sketch that matches your PCB version from `DezKVM PoC/firmware/`. For v7 or v8 PCBs, use `For v7 PCB/usbkvm_fw/usbkvm_fw.ino`.

### First-Time Flash (bootloader mode entry)

The CH552G ships without firmware. To enter its USB bootloader, you must hold down the FLASH button on the PCB **before** the chip sees USB power. Follow these steps exactly in order:

1. Open the firmware sketch in Arduino IDE and click **Compile** (the tick/check button). Wait for it to finish — this caches the binary and makes the next step faster.
2. Set the upload settings under **Tools**:
   - **Clock**: `24MHz (Internal)`
   - **Upload method**: `USB CODE w/ 148B USB RAM`
3. **Hold down the FLASH button** on the PCB. Do not release it yet.
4. While still holding FLASH, plug the **host-side USB-C cable** from the PCB into your PC.
5. Check your device manager (Windows) or `lsusb` (Linux). You should see:
   - A USB hub (SL2.1A)
   - A USB video/audio device (MS2109 capture card, if installed)
   - A USB-to-UART adapter (CH340C)
   - An **Unknown Device** — this is the CH552G in bootloader mode
6. In Arduino IDE, click **Upload** (the right-arrow button). You do not need to select a serial port for the first flash.
7. Watch the IDE output. **The moment you see the compilation phase finish and the upload phase start, release the FLASH button.** This is the critical timing window:
   - Release too late → upload times out
   - Release too early → CH552G resets out of bootloader before the upload completes
   - If it fails, unplug the cable and repeat from step 3
8. When successful, the Unknown Device disappears from your device manager and a new **COM port / ttyACM device** appears. This is the CH552G running its firmware.

> The first flash is the hardest part of building this device. It may take 2–3 attempts to get the button timing right. This is normal — do not be discouraged.

### Subsequent Firmware Updates

After the first flash is done, the ch55xduino bootloader is triggered automatically via the serial port. Future updates work exactly like any Arduino board:

1. Select the correct COM port / ttyACM device under **Tools → Port**.
2. Click **Upload**.

No button holding is required.

---

## Configuring the CH9329 Baud Rate

The CH9329 (keyboard/mouse emulator chip) ships with a default baud rate of **9600 bps**, which is too slow for smooth KVM control. You need to change it to **115200 bps** once after first assembly.

> This step requires the `dezkvmd` software to be built. See the main `src/dezkvmd/` directory for build instructions.

1. Plug the **host-side USB-C** port into your SBC (or any Linux PC).
2. Take a separate USB power source (a phone charger is fine) and plug it into the **lower USB-C port** on the remote side of the KVM module — the one closer to the HDMI connector. This powers up the CH9329 independently so the configuration can reach it.
3. On the SBC / Linux machine, run:
   ```bash
   src/dezkvmd/configure_chip.sh
   ```
   If the CH340C UART adapter enumerated on a different path than `/dev/ttyUSB0`, edit `src/dezkvmd/config/usbkvm.json` to match before running the script.
4. Wait for the script to complete without errors.
5. Unplug the power from the lower USB-C port.
6. Connect the **remote computer's USB port** to that lower USB-C port — keyboard and mouse should now feel immediate and responsive.

---

## Connecting to a Host SBC (Multiple KVM Ports, No HDMI Switch)

This is the feature that sets DezKVM apart: **you do not need a dedicated HDMI switch** (like the PiKVM HDMI switch) because each port module is an independent USB device. Adding another KVM port just means plugging in another USB cable.

### How it works

Each DezKVM Port Module contains a **SL2.1A USB 2.0 hub** that groups all of its downstream USB devices under a single USB tree. When plugged into the SBC, the Linux kernel sees:

| Linux Device | What it is |
|---|---|
| USB hub (SL2.1A) | The "identity container" of one KVM port |
| `/dev/video*` + ALSA sound card | MS2109 HDMI capture — video and audio from the remote machine |
| `/dev/ttyUSB*` (ch341 driver) | CH340C UART bridge — the pipe that carries keyboard/mouse commands to CH9329 |
| `/dev/ttyACM*` (CDC ACM) | CH552G MCU — ATX control, USB mass-storage switching, UUID queries |

The `dezkvmd` daemon queries `u` (UUID command) on each `ttyACM` device at startup to build a map of **UUID → USB tree → video device + ttyUSB + ttyACM**. This is how it knows which keyboard/mouse pipe belongs to which video stream, even after rebooting or re-plugging modules.

### Single-port setup

```
[ SBC USB port 1 ]
      |
[ DezKVM Port Module 1 ]
      |── HDMI ──► Computer A
```

Connect the host-side USB-C of the Port Module to any USB 2.0 or USB 3.0 port on the SBC. USB 3.0 ports are backwards compatible with USB 2.0.

### Multi-port setup

```
[ SBC USB port 1 ] ──► [ DezKVM Port Module 1 ] ──► Computer A
[ SBC USB port 2 ] ──► [ DezKVM Port Module 2 ] ──► Computer B
[ SBC USB port 3 ] ──► [ DezKVM Port Module 3 ] ──► Computer C
```

Each module plugs directly into its own USB port on the SBC. If your SBC has fewer physical USB ports than you need, you can use a **USB 2.0 hub** to expand — just be aware that all modules behind the same hub share the bandwidth of that hub's upstream port.

```
[ SBC USB port 1 ] ──► [ USB 2.0 Hub ]
                               |──► [ DezKVM Port Module 1 ] ──► Computer A
                               |──► [ DezKVM Port Module 2 ] ──► Computer B
```

> **Bandwidth note:** Each module uses ~80–90 Mbps at 1080p 25 fps. USB 2.0 practical throughput is around 400 Mbps, so two modules per hub port is comfortable; four is the realistic maximum when all sessions are active simultaneously.

### Why no HDMI switch is needed

With PiKVM and similar designs, the SBC has only one HDMI input (via CSI adapter), forcing you to use an external HDMI matrix switch to route signals. DezKVM avoids this completely because:

- Each Port Module has its **own dedicated HDMI capture chip** (MS2109).
- Each capture chip is an independent USB UVC device on the SBC.
- The SBC sees as many independent video streams as there are Port Modules plugged in.

Each stream can be viewed, recorded, or controlled independently and simultaneously — no switching required.

---

## Building and Connecting the ATX Board

The ATX board gives the SBC the ability to press the power button, press the reset button, and read the power LED and hard-drive LED of the controlled computer — exactly like physically sitting in front of it.

### Build the ATX Board PCB

Order the PCB using the Gerber files in `ATX board/PCB/` and populate it using the parts list in `ATX board/README.md`. The board is simple: it contains just a few jumper controlled by four **EL817 optocouplers in the KVM port unit**, providing electrical isolation between the KVM and the controlled PC's front-panel header. There is no MCU on the ATX board itself — all logic runs on the CH552G inside the Port Module.

3D-print the parts from `ATX board/3d_models/`:

- **pcie-bracket.stl** — mounts into any empty PCIe slot bracket hole on the back of the PC case, giving you a clean exterior mounting point
- **atx-adapter_mount.stl** — holds the ATX PCB in place
- **atx-adapter_cable-press.stl** — optional cable management clip instead of zip ties

### Wire the ATX Board to the Motherboard

The motherboard's front-panel header is usually a 2×5 or 2×9 pin block near the bottom-right of the board, labelled something like `F_PANEL` or `JFP1`. Consult your motherboard manual for the exact pinout. Using **Female-to-Female Dupont wires**, connect:

| ATX Board header | Motherboard front-panel pin |
|------------------|----------------------------|
| PWR_SW + | Power switch signal |
| PWR_SW − | Power switch ground |
| RST_SW + | Reset switch signal |
| RST_SW − | Reset switch ground |
| PWR_LED + | Power LED anode |
| PWR_LED − | Power LED cathode |
| HDD_LED + | HDD LED anode |
| HDD_LED − | HDD LED cathode |

The ATX board's four 2×2p headers are labelled on the PCB silkscreen. If your case already has front-panel wires plugged into the motherboard, use **Male-to-Female Dupont wires** to daisy-chain from the existing wires to the ATX board, then from the ATX board to the motherboard header.

### Connect the ATX Board to the Port Module

Run an **RJ45 Ethernet cable** (standard Cat5e or Cat6 patch cable) between:
- The RJ45 port on the ATX board
- The **RJ45 ATX control port** on the remote side of the DezKVM Port Module

The cable carries the four isolated signals (power switch, reset switch, power LED, HDD LED) between the ATX board and the CH552G on the Port Module. No special pinout or crossover cable is required — a straight-through patch cable is correct.

> **Safety note:** The EL817 optocouplers on the ATX board electrically isolate the KVM circuitry from the motherboard's front-panel signals. Do not bypass or remove them.

---

## Optional: OLED HMI Display

The OLED HMI PCB (`OLED_hmi_pcb/`) is an optional status display that connects to the SBC via USB UART. It uses the same CH552G MCU and a 0.96-inch SSD1306 OLED. Firmware and PCB files are in `OLED_hmi_pcb/`. The `dezkvmd` daemon communicates with it using a length-type-value UART protocol to render text and respond to the two capacitive touch buttons on the board.

This is a cosmetic and convenience addition — the KVM system is fully functional without it.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Arduino IDE upload times out during first flash | Released FLASH button too late | Unplug USB, hold FLASH again, replug, and release button faster after upload starts |
| CH552G keeps resetting during first flash | Released FLASH button too early | Unplug USB, hold FLASH again, replug, keep holding until IDE shows "uploading" |
| No video device appears on SBC | MS2109 module not seated properly, or HDMI source not active | Re-seat the capture module; verify the remote PC has a display output on that HDMI port |
| Keyboard/mouse is sluggish or drops inputs | CH9329 still running at 9600 bps | Re-run `configure_chip.sh` as described in the CH9329 section above |
| `dezkvmd` cannot find a module | Module is on a different ttyACM/ttyUSB path | Edit `config/usbkvm.json` to point to the correct device paths |
| ATX power/reset commands have no effect | RJ45 cable not connected, or wrong motherboard header wiring | Check the patch cable and verify Dupont wire connections against the motherboard manual |
| Two modules have the same UUID | UUID was regenerated accidentally | Send command `u` to each ttyACM device individually to read UUIDs; use `z` (debug) to regenerate one if duplicated |
