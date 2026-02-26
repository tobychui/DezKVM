/*
    paste-box.js

    This script implements the Paste Box functionality, allowing users to
    input text and send it as simulated keyboard input to the remote system
    via HID over WebSocket.
*/

const PASTE_BOX_MAX_CHARS = 1000; // Maximum characters allowed in paste box

let pasteBoxActive = false;
let pasteCancelled = false;

// Set the UI pastebox max chars
document.addEventListener('DOMContentLoaded', () => {
    document.getElementById('pasteTextarea').attributes.maxlength = PASTE_BOX_MAX_CHARS;
    document.getElementById('pasteCharCounter').textContent = `0 / ${PASTE_BOX_MAX_CHARS}`;
});

// Character to keycode mapping for HID-supported characters
const charToKeyCode = {
    // Numbers
    '0': 48, '1': 49, '2': 50, '3': 51, '4': 52,
    '5': 53, '6': 54, '7': 55, '8': 56, '9': 57,
    // Lowercase letters
    'a': 65, 'b': 66, 'c': 67, 'd': 68, 'e': 69,
    'f': 70, 'g': 71, 'h': 72, 'i': 73, 'j': 74,
    'k': 75, 'l': 76, 'm': 77, 'n': 78, 'o': 79,
    'p': 80, 'q': 81, 'r': 82, 's': 83, 't': 84,
    'u': 85, 'v': 86, 'w': 87, 'x': 88, 'y': 89, 'z': 90,
    // Special characters that need shift
    '!': {keycode: 49, shift: true},  // Shift + 1
    '@': {keycode: 50, shift: true},  // Shift + 2
    '#': {keycode: 51, shift: true},  // Shift + 3
    '$': {keycode: 52, shift: true},  // Shift + 4
    '%': {keycode: 53, shift: true},  // Shift + 5
    '^': {keycode: 54, shift: true},  // Shift + 6
    '&': {keycode: 55, shift: true},  // Shift + 7
    '*': {keycode: 56, shift: true},  // Shift + 8
    '(': {keycode: 57, shift: true},  // Shift + 9
    ')': {keycode: 48, shift: true},  // Shift + 0
    // Other common symbols
    ' ': 32,  // Space
    '-': 189, '_': {keycode: 189, shift: true},
    '=': 187, '+': {keycode: 187, shift: true},
    '[': 219, '{': {keycode: 219, shift: true},
    ']': 221, '}': {keycode: 221, shift: true},
    '\\': 220, '|': {keycode: 220, shift: true},
    ';': 186, ':': {keycode: 186, shift: true},
    "'": 222, '"': {keycode: 222, shift: true},
    ',': 188, '<': {keycode: 188, shift: true},
    '.': 190, '>': {keycode: 190, shift: true},
    '/': 191, '?': {keycode: 191, shift: true},
    '`': 192, '~': {keycode: 192, shift: true},
    '\n': 13, // Enter
    '\t': 9,  // Tab
};

function updatePasteBoxCharCounter() {
    const textarea = document.getElementById('pasteTextarea');
    const counter = document.getElementById('pasteCharCounter');
    const currentLength = textarea.value.length;
    counter.textContent = `${currentLength} / ${PASTE_BOX_MAX_CHARS}`;
    
    // Change color if approaching or at limit
    if (currentLength >= PASTE_BOX_MAX_CHARS) {
        counter.style.color = '#db2828'; // Red
    } else if (currentLength >= PASTE_BOX_MAX_CHARS * 0.9) {
        counter.style.color = '#f2711c'; // Orange
    } else {
        counter.style.color = '#767676'; // Gray
    }
}

function showPasteBox() {
    const pasteBox = document.getElementById('pasteBox');
    const textarea = document.getElementById('pasteTextarea');
    pasteBox.style.display = 'flex';
    pasteBoxActive = true;
    textarea.focus();
    updatePasteBoxCharCounter();
    
    // Prevent document key events when paste box is active
    document.removeEventListener('keydown', handleKeyDown);
    document.removeEventListener('keyup', handleKeyUp);
}

function closePasteBox() {
    const pasteBox = document.getElementById('pasteBox');
    pasteBox.style.display = 'none';
    pasteBoxActive = false;
    
    // Re-attach document key events
    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('keyup', handleKeyUp);
}

function clearPasteBox() {
    document.getElementById('pasteTextarea').value = '';
    updatePasteBoxCharCounter();
}

function sendKeyPress(keycode, needsShift = false) {
    if (!hidsocket || hidsocket.readyState !== WebSocket.OPEN) {
        console.error('HID WebSocket not connected');
        return;
    }

    // Press shift if needed
    if (needsShift) {
        hidsocket.send(JSON.stringify({
            event: 0,
            keycode: 16, // Shift key
            is_right_modifier_key: false
        }));
    }

    // Press the key
    hidsocket.send(JSON.stringify({
        event: 0,
        keycode: keycode,
        is_right_modifier_key: false
    }));

    // Release the key
    hidsocket.send(JSON.stringify({
        event: 1,
        keycode: keycode,
        is_right_modifier_key: false
    }));

    // Release shift if needed
    if (needsShift) {
        hidsocket.send(JSON.stringify({
            event: 1,
            keycode: 16, // Shift key
            is_right_modifier_key: false
        }));
    }
}

function cancelPasteText() {
    pasteCancelled = true;
    $.toast({
        message: '<i class="orange exclamation icon"></i> Paste operation cancelled',
    });
}

async function sendPasteText() {
    const textarea = document.getElementById('pasteTextarea');
    const text = textarea.value;
    const progressBar = document.getElementById('pasteProgressBar');
    const sendButton = document.getElementById('btnSendPaste');
    const clearButton = document.getElementById('btnClearPaste');
    const cancelButton = document.getElementById('btnCancelPaste');
    
    if (!text) {
        $.toast({
            message: '<i class="yellow exclamation triangle icon"></i> No text to send',
            //class: 'warning'
        });
        return;
    }

    if (!hidsocket || hidsocket.readyState !== WebSocket.OPEN) {
        $.toast({
            message: '<i class="red circle times icon"></i> HID WebSocket not connected',
            //class: 'error'
        });
        return;
    }

    // Estimate the total time required
    const estimatedTimeMs = text.length * 30; // 30ms per character
    if (estimatedTimeMs > 10000) { // More than 10 seconds
        const proceed = confirm(`Sending this text may take approximately ${(estimatedTimeMs / 1000).toFixed(1)} seconds. Do you want to proceed?`);
        if (!proceed) {
            return;
        }
    }

    // Reset cancel flag
    pasteCancelled = false;

    // Disable buttons, show cancel button and progress bar
    sendButton.style.display = 'none';
    clearButton.style.display = 'none';
    cancelButton.style.display = 'inline-block';
    textarea.disabled = true;
    progressBar.style.display = 'block';
    $('#pasteProgressBar').progress({
        percent: 0
    });

    let sentCount = 0;
    let skippedCount = 0;

    // Process each character with delay
    for (let i = 0; i < text.length; i++) {
        // Check if cancelled
        if (pasteCancelled) {
            console.log('Paste operation cancelled by user');
            break;
        }

        const char = text[i];
        
        // Check if uppercase letter
        if (char >= 'A' && char <= 'Z') {
            const keycode = char.charCodeAt(0);
            sendKeyPress(keycode, true);
            sentCount++;
        }
        // Check if lowercase letter
        else if (char >= 'a' && char <= 'z') {
            const keycode = char.toUpperCase().charCodeAt(0);
            sendKeyPress(keycode, false);
            sentCount++;
        }
        // Check if number or mapped character
        else if (charToKeyCode[char] !== undefined) {
            const mapping = charToKeyCode[char];
            if (typeof mapping === 'object') {
                sendKeyPress(mapping.keycode, mapping.shift);
            } else {
                sendKeyPress(mapping, false);
            }
            sentCount++;
        }
        else {
            skippedCount++;
            console.warn(`Unsupported character: '${char}' (code: ${char.charCodeAt(0)})`);
        }

        // Update progress bar
        const progress = ((i + 1) / text.length) * 100;
        $('#pasteProgressBar').progress('set percent', progress);

        // Add delay between keystrokes (30ms delay)
        await new Promise(resolve => setTimeout(resolve, 30));
    }

    // Show completion message only if not cancelled
    if (!pasteCancelled) {
        let message = `<i class="green check circle icon"></i> Sent ${sentCount} characters`;
        if (skippedCount > 0) {
            message += `, skipped ${skippedCount} unsupported characters`;
        }

        $.toast({
            message: message,
            //class: 'success'
        });
    }

    // Re-enable buttons and hide progress bar
    sendButton.style.display = 'inline-block';
    clearButton.style.display = 'inline-block';
    cancelButton.style.display = 'none';
    textarea.disabled = false;
    progressBar.style.display = 'none';
    $('#pasteProgressBar').progress('set percent', 0);

    // Clear and close only if not cancelled
    if (!pasteCancelled) {
        clearPasteBox();
        closePasteBox();
    }
}