/*
    DezuKVM - Offline USB KVM Client

    Author: tobychui

    Note: This require HTTPS and user interaction to request serial port access.

    This file is part of DezuKVM.
    DezuKVM is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.
*/

/*
    USB Serial Communication
*/
let serialPort = null;
let serialReader = null;
let serialWriter = null;
let serialReadBuffer = [];

// Update selected port display
function updateSelectedPortDisplay(port) {
    const selectedPortElem = document.getElementById('selectedPort');
    if (port && port.getInfo) {
        const info = port.getInfo();
        selectedPortElem.textContent = `VID: ${info.usbVendorId || '-'}, PID: ${info.usbProductId || '-'}`;
    } else if (port) {
        selectedPortElem.textContent = 'KVM Connected';
    } else {
        selectedPortElem.textContent = 'KVM Not Connected';
    }
}

// Request a new serial port
async function requestSerialPort() {
    try {
        // Disconnect previous port if connected
        if (serialPort) {
            await disconnectSerialPort();
        }
        serialPort = await navigator.serial.requestPort();
        await serialPort.open({ baudRate: 115200 });
        serialReader = serialPort.readable.getReader();
        serialWriter = serialPort.writable.getWriter();
        updateSelectedPortDisplay(serialPort);

        // Change button to indicate connected state
        document.getElementById('selectSerialPort').classList.add('is-negative');
        document.querySelector('#selectSerialPort span').className = 'ts-icon is-link-slash-icon';

        // Start reading loop for incoming data
        readSerialLoop();
    } catch (e) {
        updateSelectedPortDisplay(null);
        alert('Failed to open serial port');
    }
}

// Disconnect serial port
async function disconnectSerialPort() {
    try {
        if (serialReader) {
            await serialReader.cancel();
            serialReader.releaseLock();
            serialReader = null;
        }
        if (serialWriter) {
            serialWriter.releaseLock();
            serialWriter = null;
        }
        if (serialPort) {
            await serialPort.close();
            serialPort = null;
        }
    } catch (e) {}
    updateSelectedPortDisplay(null);
}

// Read loop for incoming serial data, dispatches 'data' events on parent
async function readSerialLoop() {
    while (serialPort && serialReader) {
        try {
            const { value, done } = await serialReader.read();
            if (done) break;
            if (value) {
                // Append to buffer
                //serialReadBuffer.push(...value);
                //console.log('Received data:', Array.from(value).map(b => b.toString(16).padStart(2, '0')).join(' '));
            }
        } catch (e) {
            break;
        }
    }
}

// Send data over serial
async function sendSerial(data) {
    if (!serialWriter) throw new Error('Serial port not open');
    await serialWriter.write(data);
}

// Button event to select serial port
document.getElementById('selectSerialPort').addEventListener('click', function(){
    if (serialPort) {
        disconnectSerialPort();
        document.getElementById('selectSerialPort').classList.remove('is-negative');
        document.querySelector('#selectSerialPort span').className = 'ts-icon is-keyboard-icon';
    } else {
        requestSerialPort();
    }
});

/*
    CH9329 HID bytecode converter
*/
function resizeTouchscreenToVideo() {
    const video = document.getElementById('video');
    const touchscreen = document.getElementById('touchscreen');
    if (video && touchscreen) {
        const rect = video.getBoundingClientRect();
        // Assume video stream is always 16:9 (1920x1080)
        const aspectRatio = 16 / 9;
        let displayWidth = rect.width;
        let displayHeight = rect.height;
        let offsetX = 0;
        let offsetY = 0;

        // Calculate the actual displayed video area (may be letterboxed/pillarboxed)
        if (rect.width / rect.height > aspectRatio) {
            // Pillarbox: black bars left/right
            displayHeight = rect.height;
            displayWidth = rect.height * aspectRatio;
            offsetX = rect.left + (rect.width - displayWidth) / 2;
            offsetY = rect.top;
        } else {
            // Letterbox: black bars top/bottom
            displayWidth = rect.width;
            displayHeight = rect.width / aspectRatio;
            offsetX = rect.left;
            offsetY = rect.top + (rect.height - displayHeight) / 2;
        }

        touchscreen.style.position = 'absolute';
        touchscreen.style.left = offsetX + 'px';
        touchscreen.style.top = offsetY + 'px';
        touchscreen.style.width = displayWidth + 'px';
        touchscreen.style.height = displayHeight + 'px';
        touchscreen.width = displayWidth;
        touchscreen.height = displayHeight;
    }
}

// Call on load and on resize
window.addEventListener('resize', resizeTouchscreenToVideo);
window.addEventListener('DOMContentLoaded', resizeTouchscreenToVideo);
setTimeout(resizeTouchscreenToVideo, 1000); // Also after 1s to ensure video is loaded

class HIDController {
    constructor() {
        this.hidState = {
            MouseButtons: 0x00,
            Modkey: 0x00,
            KeyboardButtons: [0, 0, 0, 0, 0, 0]
        };
        this.Config = {
            ScrollSensitivity: 1
        };
    }

    // Calculates checksum for a given array of bytes
    calcChecksum(arr) {
        return arr.reduce((sum, b) => (sum + b) & 0xFF, 0);
    }

    // Soft reset the CH9329 chip
    async softReset() {
        if (!serialPort || !serialPort.readable || !serialPort.writable) {
            throw new Error('Serial port not open');
        }
        const packet = [
            0x57, 0xAB, 0x00, 0x0F, 0x00 // checksum placeholder
        ];
        packet[4] = this.calcChecksum(packet.slice(0, 4));
        await this.sendPacketAndWait(packet, 0x0F);
    }

    // Sends a packet over serial and waits for a reply with a specific command code
    async sendPacketAndWait(packet, replyCmd) {
        const timeout = 300; // 300ms timeout
        const succReplyByte = replyCmd | 0x80;
	    const errorReplyByte = replyCmd | 0xC0;
        // Succ example for cmd 0x04: 57 AB 00 84 01 00 87
        // Header is 57 AB 00, we can skip that
        // then the 0x84 is the replyCmd | 0x80 (or if error, 0xC4)
        // 0x01 is the data length (1 byte)
        // 0x00 is the data (success)
        // 0x87 is the checksum
        serialReadBuffer = [];
        await sendSerial(new Uint8Array(packet));
        const startTime = Date.now();
        /*
        while (true) {
            // Check for timeout
            if (Date.now() - startTime > timeout) {
                //Timeout, ignore this reply
                return Promise.reject(new Error('timeout waiting for reply'));
            }  
            // Check if we have enough data for a reply
            if (serialReadBuffer.length >= 5) {
                // Look for the start of a packet
                for (let i = 0; i <= serialReadBuffer.length - 5; i++) {
                    if (serialReadBuffer[i] === 0x57 && serialReadBuffer[i + 1] === 0xAB) {
                       //Discard bytes before the packet
                       if (i > 0) {
                           serialReadBuffer.splice(0, i);
                       }

                       // Now we have 57 AB at the start, check if we have the full packet
                       const len = serialReadBuffer[3];
                       const fullPacketLength = 4 + len + 1;
                       if (serialReadBuffer.length >= fullPacketLength) {
                           const packet = serialReadBuffer.slice(0, fullPacketLength);
                           serialReadBuffer = serialReadBuffer.slice(fullPacketLength);
                           const checksum = this.calcChecksum(packet.slice(0, fullPacketLength - 1));
                           if (checksum !== packet[fullPacketLength - 1]) {
                               // Invalid checksum, discard packet
                               continue;
                           }
                           if (packet[4] === replyCmd) {
                               return Promise.resolve();
                           }
                       }
                    }
                }
            }
        }*/

        // Seems the speed required to get a reply is too high for the browser
        // so reply check is not implemented for now
        await new Promise(resolve => setTimeout(resolve, 30));
        return Promise.resolve();
    }

    // Mouse move absolute
    async MouseMoveAbsolute(xLSB, xMSB, yLSB, yMSB) {
        if (!serialPort || !serialPort.readable || !serialPort.writable) {
            return;
        }
        const packet = [
            0x57, 0xAB, 0x00, 0x04, 0x07, 0x02,
            this.hidState.MouseButtons,
            xLSB,
            xMSB,
            yLSB,
            yMSB,
            0x00, // Scroll
            0x00  // Checksum placeholder
        ];
        packet[12] = this.calcChecksum(packet.slice(0, 12));
        await this.sendPacketAndWait(packet, 0x04);
    }

    // Mouse move relative
    async MouseMoveRelative(dx, dy, wheel) {
         if (!serialPort || !serialPort.readable || !serialPort.writable) {
            return;
        }
        // Ensure 0x80 is not used
        if (dx === 0x80) dx = 0x81;
        if (dy === 0x80) dy = 0x81;
        const packet = [
            0x57, 0xAB, 0x00, 0x05, 0x05, 0x01,
            this.hidState.MouseButtons,
            dx,
            dy,
            wheel,
            0x00 // Checksum placeholder
        ];
        packet[10] = this.calcChecksum(packet.slice(0, 10));
        await this.sendPacketAndWait(packet, 0x05);
    }

    // Mouse button press
    async MouseButtonPress(button) {
        switch (button) {
            case 0x01: // Left
                this.hidState.MouseButtons |= 0x01;
                break;
            case 0x02: // Right
                this.hidState.MouseButtons |= 0x02;
                break;
            case 0x03: // Middle
                this.hidState.MouseButtons |= 0x04;
                break;
            default:
                throw new Error("invalid opcode for mouse button press");
        }
        await this.MouseMoveRelative(0, 0, 0);
    }

    // Mouse button release
    async MouseButtonRelease(button) {
        switch (button) {
            case 0x00: // Release all
                this.hidState.MouseButtons = 0x00;
                break;
            case 0x01: // Left
                this.hidState.MouseButtons &= ~0x01;
                break;
            case 0x02: // Right
                this.hidState.MouseButtons &= ~0x02;
                break;
            case 0x03: // Middle
                this.hidState.MouseButtons &= ~0x04;
                break;
            default:
                throw new Error("invalid opcode for mouse button release");
        }
        await this.MouseMoveRelative(0, 0, 0);
    }

    // Mouse scroll
    async MouseScroll(tilt) {
        if (tilt === 0) return;
        let wheel;
        if (tilt < 0) {
            wheel = this.Config.ScrollSensitivity;
        } else {
            wheel = 0xFF - this.Config.ScrollSensitivity;
        }
        await this.MouseMoveRelative(0, 0, wheel);
    }

    // --- Keyboard Emulation ---

    // Set modifier key (Ctrl, Shift, Alt, GUI)
    async SetModifierKey(keycode, isRight) {
        const MOD_LCTRL = 0x01, MOD_LSHIFT = 0x02, MOD_LALT = 0x04, MOD_LGUI = 0x08;
        const MOD_RCTRL = 0x10, MOD_RSHIFT = 0x20, MOD_RALT = 0x40, MOD_RGUI = 0x80;
        let modifierBit = 0;
        switch (keycode) {
            case 17: modifierBit = isRight ? MOD_RCTRL : MOD_LCTRL; break;
            case 16: modifierBit = isRight ? MOD_RSHIFT : MOD_LSHIFT; break;
            case 18: modifierBit = isRight ? MOD_RALT : MOD_LALT; break;
            case 91: modifierBit = isRight ? MOD_RGUI : MOD_LGUI; break;
            default: throw new Error("Not a modifier key");
        }
        this.hidState.Modkey |= modifierBit;
        await this.keyboardSendKeyCombinations();
    }

    // Unset modifier key (Ctrl, Shift, Alt, GUI)
    async UnsetModifierKey(keycode, isRight) {
        const MOD_LCTRL = 0x01, MOD_LSHIFT = 0x02, MOD_LALT = 0x04, MOD_LGUI = 0x08;
        const MOD_RCTRL = 0x10, MOD_RSHIFT = 0x20, MOD_RALT = 0x40, MOD_RGUI = 0x80;
        let modifierBit = 0;
        switch (keycode) {
            case 17: modifierBit = isRight ? MOD_RCTRL : MOD_LCTRL; break;
            case 16: modifierBit = isRight ? MOD_RSHIFT : MOD_LSHIFT; break;
            case 18: modifierBit = isRight ? MOD_RALT : MOD_LALT; break;
            case 91: modifierBit = isRight ? MOD_RGUI : MOD_LGUI; break;
            default: throw new Error("Not a modifier key");
        }
        this.hidState.Modkey &= ~modifierBit;
        await this.keyboardSendKeyCombinations();
    }

    // Send a keyboard press by JavaScript keycode
    async SendKeyboardPress(keycode) {
        const hid = this.javaScriptKeycodeToHIDOpcode(keycode);
        if (hid === 0x00) throw new Error("Unsupported keycode: " + keycode);
        // Already pressed?
        for (let i = 0; i < 6; i++) {
            if (this.hidState.KeyboardButtons[i] === hid) return;
        }
        // Find empty slot
        for (let i = 0; i < 6; i++) {
            if (this.hidState.KeyboardButtons[i] === 0x00) {
                this.hidState.KeyboardButtons[i] = hid;
                await this.keyboardSendKeyCombinations();
                return;
            }
        }
        throw new Error("No space left in keyboard state to press key: " + keycode);
    }

    // Send a keyboard release by JavaScript keycode
    async SendKeyboardRelease(keycode) {
        const hid = this.javaScriptKeycodeToHIDOpcode(keycode);
        if (hid === 0x00) throw new Error("Unsupported keycode: " + keycode);
        for (let i = 0; i < 6; i++) {
            if (this.hidState.KeyboardButtons[i] === hid) {
                this.hidState.KeyboardButtons[i] = 0x00;
                await this.keyboardSendKeyCombinations();
                return;
            }
        }
        // Not pressed, do nothing
    }

    // Send the current key combinations (modifiers + up to 6 keys)
    async keyboardSendKeyCombinations() {
        const packet = [
            0x57, 0xAB, 0x00, 0x02, 0x08,
            this.hidState.Modkey, 0x00,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
            0x00
        ];
        for (let i = 0; i < 6; i++) {
            packet[7 + i] = this.hidState.KeyboardButtons[i] || 0x00;
        }
        packet[13] = this.calcChecksum(packet.slice(0, 13));
        await this.sendPacketAndWait(packet, 0x02);
    }

    // Convert JavaScript keycode to HID keycode
    javaScriptKeycodeToHIDOpcode(keycode) {
        // Letters A-Z
        if (keycode >= 65 && keycode <= 90) return (keycode - 65) + 0x04;
        // Numbers 1-9 (top row, not numpad)
        if (keycode >= 49 && keycode <= 57) return (keycode - 49) + 0x1E;
        // F1 to F12
        if (keycode >= 112 && keycode <= 123) return (keycode - 112) + 0x3A;
        switch (keycode) {
            case 8: return 0x2A; // Backspace
            case 9: return 0x2B; // Tab
            case 13: return 0x28; // Enter
            case 16: return 0xE1; // Left shift
            case 17: return 0xE0; // Left Ctrl
            case 18: return 0xE6; // Left Alt
            case 19: return 0x48; // Pause
            case 20: return 0x39; // Caps Lock
            case 27: return 0x29; // Escape
            case 32: return 0x2C; // Spacebar
            case 33: return 0x4B; // Page Up
            case 34: return 0x4E; // Page Down
            case 35: return 0x4D; // End
            case 36: return 0x4A; // Home
            case 37: return 0x50; // Left Arrow
            case 38: return 0x52; // Up Arrow
            case 39: return 0x4F; // Right Arrow
            case 40: return 0x51; // Down Arrow
            case 44: return 0x46; // Print Screen or F13 (Firefox)
            case 45: return 0x49; // Insert
            case 46: return 0x4C; // Delete
            case 48: return 0x27; // 0 (not Numpads)
            case 59: return 0x33; // ';'
            case 61: return 0x2E; // '='
            case 91: return 0xE3; // Left GUI (Windows)
            case 92: return 0xE7; // Right GUI
            case 93: return 0x65; // Menu key
            case 96: return 0x62; // 0 (Numpads)
            case 97: return 0x59; // 1 (Numpads)
            case 98: return 0x5A; // 2 (Numpads)
            case 99: return 0x5B; // 3 (Numpads)
            case 100: return 0x5C; // 4 (Numpads)
            case 101: return 0x5D; // 5 (Numpads)
            case 102: return 0x5E; // 6 (Numpads)
            case 103: return 0x5F; // 7 (Numpads)
            case 104: return 0x60; // 8 (Numpads)
            case 105: return 0x61; // 9 (Numpads)
            case 106: return 0x55; // * (Numpads)
            case 107: return 0x57; // + (Numpads)
            case 109: return 0x56; // - (Numpads)
            case 110: return 0x63; // dot (Numpads)
            case 111: return 0x54; // divide (Numpads)
            case 144: return 0x53; // Num Lock
            case 145: return 0x47; // Scroll Lock
            case 146: return 0x58; // Numpad enter
            case 173: return 0x2D; // -
            case 186: return 0x33; // ';'
            case 187: return 0x2E; // '='
            case 188: return 0x36; // ','
            case 189: return 0x2D; // '-'
            case 190: return 0x37; // '.'
            case 191: return 0x38; // '/'
            case 192: return 0x35; // '`'
            case 219: return 0x2F; // '['
            case 220: return 0x31; // backslash
            case 221: return 0x30; // ']'
            case 222: return 0x34; // '\''
            default: return 0x00;
        }
    }
}

// Instantiate HID controller
const controller = new HIDController();
const videoOverlayElement = document.getElementById('touchscreen');

let isMouseDown = false;
let lastX = 0;
let lastY = 0;

// Mouse down
videoOverlayElement.addEventListener('mousedown', async (e) => {
    isMouseDown = true;
    lastX = e.clientX;
    lastY = e.clientY;
    if (e.button === 0) {
        await controller.MouseButtonPress(0x01); // Left
    } else if (e.button === 2) {
        await controller.MouseButtonPress(0x02); // Right
    } else if (e.button === 1) {
        await controller.MouseButtonPress(0x03); // Middle
    }
});

// Mouse up
videoOverlayElement.addEventListener('mouseup', async (e) => {
    isMouseDown = false;
    if (e.button === 0) {
        await controller.MouseButtonRelease(0x01); // Left
    } else if (e.button === 2) {
        await controller.MouseButtonRelease(0x02); // Right
    } else if (e.button === 1) {
        await controller.MouseButtonRelease(0x03); // Middle
    }
});

// Mouse move (absolute positioning)
videoOverlayElement.addEventListener('mousemove', async (e) => {
    const rect = videoOverlayElement.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const width = rect.width;
    const height = rect.height;
    const offsetX = x / width;
    const offsetY = y / height;
    //console.log('Offset ratio:', { offsetX, offsetY });

    const absX = Math.round(offsetX * 4095);
    const absY = Math.round(offsetY * 4095);
    await controller.MouseMoveAbsolute(absX & 0xFF, (absX >> 8) & 0xFF, absY & 0xFF, (absY >> 8) & 0xFF);
});

// Context menu disable (for right click)
videoOverlayElement.addEventListener('contextmenu', (e) => {
    e.preventDefault();
});

// Mouse wheel (scroll)
videoOverlayElement.addEventListener('wheel', async (e) => {
    e.preventDefault();
    let tilt = e.deltaY > 0 ? 1 : -1;
    await controller.MouseScroll(tilt);
});

// Keyboard events for HID emulation
window.addEventListener('keydown', async (e) => {
    // Ignore repeated events
    //if (e.repeat) return;
    try {
        // Modifier keys
        if (e.key === 'Control' || e.key === 'Shift' || e.key === 'Alt' || e.key === 'Meta') {
            await controller.SetModifierKey(e.keyCode, e.location === KeyboardEvent.DOM_KEY_LOCATION_RIGHT);
        } else {
            await controller.SendKeyboardPress(e.keyCode);
        }
        e.preventDefault();
    } catch (err) {
        // Ignore unsupported keys
    }
});

window.addEventListener('keyup', async (e) => {
    try {
        if (e.key === 'Control' || e.key === 'Shift' || e.key === 'Alt' || e.key === 'Meta') {
            await controller.UnsetModifierKey(e.keyCode, e.location === KeyboardEvent.DOM_KEY_LOCATION_RIGHT);
        } else {
            await controller.SendKeyboardRelease(e.keyCode);
        }
        e.preventDefault();
    } catch (err) {
        // Ignore unsupported keys
    }
});

document.getElementById('resetHIDBtn').addEventListener('click', async () => {
    try {
        await controller.softReset();
        alert('HID soft reset sent.');
    } catch (e) {
        alert('Failed to reset HID: ' + e.message);
    }
});

/* 
    Audio Capture
*/
const audioSelect = document.getElementById('audioSource');
const refreshAudioBtn = document.getElementById('refreshAudioSources');
let currentAudioStream = null;

// List audio input devices
async function listAudioSources() {
    const devices = await navigator.mediaDevices.enumerateDevices();
    audioSelect.innerHTML = '';
    devices
        .filter(device => device.kind === 'audioinput')
        .forEach(device => {
            const option = document.createElement('option');
            option.value = device.deviceId;
            option.text = device.label || `Microphone ${audioSelect.length + 1}`;
            audioSelect.appendChild(option);
        });
}

// Start streaming selected audio source
async function startAudioStream() {
    if (currentAudioStream) {
        currentAudioStream.getTracks().forEach(track => track.stop());
    }
    const deviceId = audioSelect.value;
    try {
        const stream = await navigator.mediaDevices.getUserMedia({
            audio: { 
                deviceId: deviceId ? { exact: deviceId } : undefined,
                echoCancellation: false,
                noiseSuppression: false,
                autoGainControl: false,
                sampleRate: 48000,
                channelCount: 2
            }
        });
        currentAudioStream = stream;

        // Create audio element if not exists
        let audioElem = document.getElementById('audioStream');
        if (!audioElem) {
            audioElem = document.createElement('audio');
            audioElem.id = 'audioStream';
            audioElem.autoplay = true;
            audioElem.controls = true;
            audioElem.style.position = 'fixed';
            audioElem.style.bottom = '10px';
            audioElem.style.left = '10px';
            audioElem.style.zIndex = 1001;
            document.body.appendChild(audioElem);
            audioElem.style.display = 'none';
        }
        audioElem.srcObject = stream;
    } catch (err) {
        alert('Error accessing audio device: ' + err.message);
    }
}

// Event listeners
refreshAudioBtn.addEventListener('click', listAudioSources);
audioSelect.addEventListener('change', startAudioStream);

// Initial population
listAudioSources().then(startAudioStream);

/*
    Video Captures

    The following section handles HDMI capture via connected webcams.
*/
async function ensureCameraPermission() {
    try {
        // Request permission to access the camera to get device labels
        await navigator.mediaDevices.getUserMedia({ video: true });
    } catch (e) {
        alert('Unable to access camera');
    }
}

async function getCameras() {
    const devices = await navigator.mediaDevices.enumerateDevices();
    const videoSelect = document.getElementById('videoSource');
    videoSelect.innerHTML = '';
    devices.forEach(device => {
        if (device.kind === 'videoinput') {
            const option = document.createElement('option');
            option.value = device.deviceId;
            option.text = device.label || `Camera ${videoSelect.length + 1}`;
            videoSelect.appendChild(option);
        }
    });
}

async function startStream() {
    const videoSelect = document.getElementById('videoSource');
    const deviceId = videoSelect.value;
    if (window.currentStream) {
        window.currentStream.getTracks().forEach(track => track.stop());
    }
    const constraints = {
        video: {
            deviceId: { exact: deviceId },
            width: { ideal: 1920 },
            height: { ideal: 1080 }
        }
    };
    try {
        const stream = await navigator.mediaDevices.getUserMedia(constraints);
        document.getElementById('video').srcObject = stream;
        window.currentStream = stream;

        // Resize touchscreen overlay after a short delay to ensure video is loaded
        setTimeout(resizeTouchscreenToVideo, 500);
    } catch (e) {
        alert('Unable to access camera');
    }
}

document.getElementById('videoSource').addEventListener('change', startStream);

document.getElementById('fullscreenBtn').addEventListener('click', () => {
    if (
        document.fullscreenElement ||
        document.webkitFullscreenElement ||
        document.mozFullScreenElement ||
        document.msFullscreenElement
    ) {
        if (document.exitFullscreen) {
            document.exitFullscreen();
        } else if (document.webkitExitFullscreen) {
            document.webkitExitFullscreen();
        } else if (document.mozCancelFullScreen) {
            document.mozCancelFullScreen();
        } else if (document.msExitFullscreen) {
            document.msExitFullscreen();
        }
        document.querySelector('#fullscreenBtn span').className = 'ts-icon is-maximize-icon';
        
    } else {
        if (document.body.requestFullscreen) {
            document.body.requestFullscreen();
        } else if (document.body.webkitRequestFullscreen) {
            document.body.webkitRequestFullscreen();
        } else if (document.body.mozRequestFullScreen) {
            document.body.mozRequestFullScreen();
        } else if (document.body.msRequestFullscreen) {
            document.body.msRequestFullscreen();
        }
        document.querySelector('#fullscreenBtn span').className = 'ts-icon is-minimize-icon';
        
    }
});

document.getElementById('refreshCameras').addEventListener('click', async () => {
    await getCameras();
    await startStream();
});

// Ensure permission, then populate cameras and start stream
ensureCameraPermission().then(() => {
    getCameras().then(startStream);
});

navigator.mediaDevices.addEventListener('devicechange', () => {
    getCameras().then(startStream);
});


