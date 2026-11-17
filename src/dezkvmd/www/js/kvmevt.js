/*
    kvmevt.js

    Keyboard, Video, Mouse (KVM) over WebSocket client-side event handling.
    Handles mouse and keyboard events, sending them to the server via WebSocket.
    Also manages audio streaming from the server: over the WebRTC peer
    connection when the WebRTC stream carries it, otherwise as raw PCM over
    the audio websocket (see startRemoteAudio).
*/
const enableKvmEventDebugPrintout = false; //Set to true to enable debug printout
const cursorCaptureElementId = "remoteCapture";
const streamingContainerId = "remoteCapture";

// HID event type constants (must match backend EventType values in mod/kvmhid/typedef.go)
const HIDEvent = Object.freeze({
    KEY_DOWN:         0,
    KEY_UP:           1,
    MOUSE_MOVE:       2,
    MOUSE_BTN_DOWN:   3,
    MOUSE_BTN_UP:     4,
    MOUSE_SCROLL:     5,
    RESET:            0xFF,
});

/* Random ID for this page load. Sent with the video stream request and the
   HID websocket so the backend knows which page owns the session: starting
   a stream from another page disconnects this one (see handleSessionTakenOver). */
var dezClientId = (window.crypto && crypto.randomUUID)
    ? crypto.randomUUID()
    : Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
let sessionTakenOver = false;

/* URL of the MJPEG stream for this page (cache-busted) */
function mjpegStreamURL(deviceUUID) {
    return '/api/v1/stream/' + deviceUUID + '/video?client=' + encodeURIComponent(dezClientId) + '&t=' + Date.now();
}

let hidsocket;
let hidWebSocketReady = false;
let protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
let port = window.location.port ? window.location.port : (protocol === 'wss' ? 443 : 80);
let hidSocketURL = `${protocol}://${window.location.hostname}:${port}/api/v1/hid/{uuid}/events`;
let audioSocketURL = `${protocol}://${window.location.hostname}:${port}/api/v1/stream/{uuid}/audio`;

let mouseMoveAbsolute = true; // Set to true for absolute mouse coordinates, false for relative
let relativeMouseSensitivity = 5; // Sensitivity multiplier for relative mouse mode (1-10)
let mouseIsOutside = false; //Mouse is outside capture element
let audioFrontendStarted = false; //Audio frontend has been started
// Declared up here: setStreamingSource() runs during this script's load
let remoteAudioWanted = false; // audio was started for this page (see startRemoteAudio)
let _streamModeKnown = false;  // the stream mode (and so the audio transport) is decided
let kvmDeviceUUID = ""; //UUID of the device being controlled
let swapCtrlCmd = false; // Swap CTRL and CMD (Meta) keys
let askOnPaste = true; // Prompt user when pasting
let directOcrToClipboard = false; // Copy OCR results straight to the clipboard, skipping the result window
let pauseAllKeyEvents = false; // Used to temporarily disable all key event handling when modals are open
let keyStackingEnabled = false; // Whether key stacking mode feature is enabled
let stackToggleKey = 'ShiftRight'; // event.code of the key that activates/deactivates key stacking (default: Right Shift)
let keyStackingActive = false;  // Whether key stacking is currently active (keys are being stacked)
let keyStack = [];              // Array of { keycode, isRightModKey } to be sent as a combo
let _suppressKeyUpCodes = new Set(); // event.code values whose keyUp should be swallowed (keys captured during stacking)

// ACK tracking for reliable HID sends (used by paste)
let _hidAckCounter = 0;
let _hidPendingAcks = {}; // { rid: { resolve, timer } }


if (window.location.hash.length > 1){
    kvmDeviceUUID = window.location.hash.substring(1);
    hidSocketURL = hidSocketURL.replace("{uuid}", kvmDeviceUUID) + '?client=' + encodeURIComponent(dezClientId);
    audioSocketURL = audioSocketURL.replace("{uuid}", kvmDeviceUUID);
    massStorageSwitchURL = massStorageSwitchURL.replace("{uuid}", kvmDeviceUUID);

    //Start HID WebSocket
    startHidWebSocket();
    $.toast({
        message: 'Connecting remote HID device',
        showProgress: 'bottom',
        classProgress: 'blue'
    });
    setStreamingSource(kvmDeviceUUID);

    // Load preferences from backend to apply on page load
    $.get('/api/v1/preferences/' + kvmDeviceUUID, function(prefs){
        if(prefs){
            if(typeof prefs.enable_relative_mouse_mode !== 'undefined'){
                mouseMoveAbsolute = !prefs.enable_relative_mouse_mode;
            }
            if(typeof prefs.relative_mouse_sensitivity !== 'undefined' && prefs.relative_mouse_sensitivity > 0){
                relativeMouseSensitivity = prefs.relative_mouse_sensitivity;
            }
            if(typeof prefs.swap_ctrl_cmd !== 'undefined'){
                swapCtrlCmd = prefs.swap_ctrl_cmd;
            }
            if(typeof prefs.ask_on_paste !== 'undefined'){
                askOnPaste = prefs.ask_on_paste;
            }
            if(typeof prefs.direct_ocr_to_clipboard !== 'undefined'){
                directOcrToClipboard = prefs.direct_ocr_to_clipboard;
            }

            // Self-start the audio stream. The parent shell also triggers
            // startRemoteAudio after connecting; the double start is
            // guarded, this covers direct loads / reloads of the viewport.
            let storedAudioQuality = getAudioQuality();
            if (storedAudioQuality !== 'disabled') {
                setTimeout(function () {
                    startRemoteAudio(storedAudioQuality);
                }, 500);
            }
            if(typeof prefs.key_stacking_enabled !== 'undefined'){
                keyStackingEnabled = prefs.key_stacking_enabled;
            }
            if(prefs.stack_toggle_key){
                stackToggleKey = prefs.stack_toggle_key;
            }
            // If relative mouse mode is saved, prompt user to click viewport to acquire pointer lock
            if(!mouseMoveAbsolute){
                $.toast({
                    message: '<i class="mouse pointer icon"></i> Click the viewport to restart cursor.',
                    duration: 5000
                });
            }
        }
    });
}


/* Initiate the video stream: MJPEG (default) or WebRTC per the stored
   streaming_mode preference. */
function setStreamingSource(deviceUUID) {
    // Until the mode is known, audio starts wait: the WebRTC stream may
    // carry the audio itself (see startRemoteAudio).
    _streamModeKnown = false;
    $.get('/api/v1/preferences/' + deviceUUID, function(prefs){
        applyStreamingSource(deviceUUID, prefs);
    }).fail(function(){
        applyStreamingSource(deviceUUID, null);
    });
}

function applyStreamingSource(deviceUUID, prefs){
    const useWebRTC = prefs && prefs.streaming_mode === 'webrtc'
        && typeof startWebRTCStream === 'function'
        && typeof RTCPeerConnection !== 'undefined';

    if (useWebRTC) {
        // Decides synchronously whether the session will carry the audio
        startWebRTCStream(deviceUUID, prefs);
    } else {
        // Classic MJPEG pipeline
        if (typeof stopWebRTCStream === 'function') {
            stopWebRTCStream(true);
        }
        const el = (typeof ensureCaptureElement === 'function')
            ? ensureCaptureElement('img')
            : document.getElementById('remoteCapture');
        el.src = mjpegStreamURL(deviceUUID);
    }

    _streamModeKnown = true;
    syncRemoteAudioTransport();
}

/* Get current streaming resolution, return [width, height].
   Works for both the MJPEG <img> and the WebRTC <video> element. */
function getCurrentStreamingResolution(){
    const el = document.getElementById(streamingContainerId);
    const width = el.naturalWidth || el.videoWidth || el.clientWidth;
    const height = el.naturalHeight || el.videoHeight || el.clientHeight;
    return [width, height];
}


/* Get current mouse button state bitmask from event.buttons */
function getMouseButtonState(event) {
    let state = 0;
    if (event.buttons & 1) state |= 0x01; // left
    if (event.buttons & 4) state |= 0x02; // middle
    if (event.buttons & 2) state |= 0x04; // right
    return state;
}

/* Mouse events */
function handleMouseMove(event) {
    // In relative mode, only send data when pointer lock is active
    if (!mouseMoveAbsolute && !document.pointerLockElement) return;
    if (!mouseMoveAbsolute) {
        // Relative mouse mode: use movementX/Y deltas
        let dx = Math.round(event.movementX * (relativeMouseSensitivity / 5));
        let dy = Math.round(event.movementY * (relativeMouseSensitivity / 5));
        // Clamp to int8 range (-127 to 127)
        dx = Math.max(-127, Math.min(127, dx));
        dy = Math.max(-127, Math.min(127, dy));
        if (dx === 0 && dy === 0) return;
        const hidCommand = {
            event: HIDEvent.MOUSE_MOVE,
            mouse_rel_x: dx,
            mouse_rel_y: dy,
            mouse_move_button_state: getMouseButtonState(event),
        };
        if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
            hidsocket.send(JSON.stringify(hidCommand));
        }
        return;
    }

    const hidCommand = {
        event: HIDEvent.MOUSE_MOVE,
        mouse_x: event.clientX,
        mouse_y: event.clientY,
    };

    let rect = event.target.getBoundingClientRect();
    let offsetX = 0;
    let offsetY = 0;
    if (!!isScaleToFit && isScaleToFit){
        //Calculate relative coordinates in scale to fit mode
        let boundingEdge = getScaleToFitBoundEdge();
        let streamingResolution = getCurrentStreamingResolution();
        if (boundingEdge === 'width'){
            //Width is already 100%, calculate the actual height
            let scaleRatio = rect.width / streamingResolution[0];
            let adjustedRectHeight = streamingResolution[1] * scaleRatio;
            let verticalOffset = (rect.height - adjustedRectHeight) / 2;
            console.log(`Adjusted height: ${adjustedRectHeight}, vertical offset: ${verticalOffset} ${scaleRatio}`);
            offsetY -= verticalOffset;
            rect.height = adjustedRectHeight;
        }else{
            //Height is already 100%, calculate the actual width
            let scaleRatio = rect.height / streamingResolution[1];
            let adjustedRectWidth = streamingResolution[0] * scaleRatio;
            let horizontalOffset = (rect.width - adjustedRectWidth) / 2;
            offsetX -= horizontalOffset;
            rect.width = adjustedRectWidth;
        }
    }

    let relativeX = event.clientX - rect.left;
    let relativeY = event.clientY - rect.top;
    
    const percentageX = Math.max(0, Math.min(4095, ((relativeX + offsetX) / rect.width) * 4096));
    const percentageY = Math.max(0, Math.min(4095, ((relativeY + offsetY) / rect.height) * 4096));

    hidCommand.mouse_x = Math.round(percentageX);
    hidCommand.mouse_y = Math.round(percentageY);

    if (enableKvmEventDebugPrintout) {
        console.log(`Mouse move: (${event.clientX}, ${event.clientY})`);
        console.log(`Mouse move relative: (${relativeX}, ${relativeY})`);
        console.log(`Mouse move percentage: (${hidCommand.mouse_x}, ${hidCommand.mouse_y})`);
    }

    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        hidsocket.send(JSON.stringify(hidCommand));
    } else {
        console.error("WebSocket is not open.");
    }
}


function handleMousePress(event) {
    event.preventDefault();
    event.stopImmediatePropagation();
    if (!mouseMoveAbsolute && !document.pointerLockElement) return;
    if (mouseIsOutside) {
        console.warn("Mouse is outside the capture area, ignoring mouse press.");
        return;
    }
    /* Mouse buttons: 1=left, 2=right, 3=middle */
    const buttonMap = {
        0: 1, 
        1: 3,
        2: 2
    }; //Map javascript mouse buttons to HID buttons

    const hidCommand = {
        event: HIDEvent.MOUSE_BTN_DOWN,
        mouse_button: buttonMap[event.button] || 0
    };

    // Log the mouse button state
    if (enableKvmEventDebugPrintout) {
        console.log(`Mouse down: ${hidCommand.mouse_button}`);
    }

    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        hidsocket.send(JSON.stringify(hidCommand));
    } else {
        console.error("WebSocket is not open.");
    }

    if (!audioFrontendStarted && !!currentAudioQuality && currentAudioQuality !== 'disabled'){
        startRemoteAudio(currentAudioQuality);
        audioFrontendStarted = true;
    }
}

function handleMouseRelease(event) {
    event.preventDefault();
    event.stopImmediatePropagation();
    if (!mouseMoveAbsolute && !document.pointerLockElement) return;
    if (mouseIsOutside) {
        console.warn("Mouse is outside the capture area, ignoring mouse release.");
        return;
    }
    /* Mouse buttons: 1=left, 2=right, 3=middle */
    const buttonMap = {
        0: 1, 
        1: 3,
        2: 2
    }; //Map javascript mouse buttons to HID buttons
    
    const hidCommand = {
        event: HIDEvent.MOUSE_BTN_UP,
        mouse_button: buttonMap[event.button] || 0
    };

    if (enableKvmEventDebugPrintout) {
        console.log(`Mouse release: ${hidCommand.mouse_button}`);
    }

    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        hidsocket.send(JSON.stringify(hidCommand));
    } else {
        console.error("WebSocket is not open.");
    }
}

function handleMouseScroll(event) {
    if (!mouseMoveAbsolute && !document.pointerLockElement) return;
    const hidCommand = {
        event: HIDEvent.MOUSE_SCROLL,
        mouse_scroll: event.deltaY
    };
    if (mouseIsOutside) {
        console.warn("Mouse is outside the capture area, ignoring mouse scroll.");
        return;
    }

    if (enableKvmEventDebugPrintout) {
        console.log(`Mouse scroll: mouse_scroll=${event.deltaY}`);
    }

    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        hidsocket.send(JSON.stringify(hidCommand));
    } else {
        console.error("WebSocket is not open.");
    }
}



/* Keyboard */
function isNumpadEvent(event) {
    return event.location === 3;
}

// Swap CTRL (keyCode 17) and Meta/CMD (keyCode 91/93) keycodes when swap is enabled
function applyCtrlCmdSwap(keyCode) {
    if (!swapCtrlCmd) return keyCode;
    // Left/Right Ctrl = 17, Left Meta = 91, Right Meta = 93
    if (keyCode === 17) return 91;  // Ctrl -> Meta
    if (keyCode === 91 || keyCode === 93) return 17; // Meta -> Ctrl
    return keyCode;
}

// Swap the key name for modifier detection when swap is enabled
function applyCtrlCmdSwapKey(key) {
    if (!swapCtrlCmd) return key;
    if (key === 'Control') return 'Meta';
    if (key === 'Meta') return 'Control';
    return key;
}

// handleStackToggleKeyDown manages entering/exiting key stacking mode when the toggle key is pressed
function handleStackToggleKeyDown(event){
    if (!keyStackingActive) {
        // Enter stacking mode
        keyStack = [];
        _suppressKeyUpCodes.clear();
        $("#keystackDisplay").show();
         document.getElementById('keystackContent').innerHTML = "";
        keyStackingActive = true;
        console.log('[KeyStack] Stacking mode ON');
        if (typeof $ !== 'undefined' && $.toast) {
            $.toast({ message: `<i class="ui green circle check icon"></i> Key stacking ON`, duration: 2500 });
        }
    } else {
        // Exit stacking mode and fire the combo
        keyStackingActive = false;
        $("#keystackDisplay").hide();
        console.log('[KeyStack] Stacking mode OFF, sending combo:', keyStack.map(k => `${k.key}(${k.keycode})`));
        if (typeof $ !== 'undefined' && $.toast) {
            if (keyStack.length > 0) {
                var label = keyStack.map(k => k.key).join(' + ') || '(empty)';
                $.toast({ message: `Sending: ${label}`, duration: 2500 });
            }else{
                $.toast({ message: `Key stack cleared`, duration: 2500 });
            }
        }
        if (keyStack.length > 0) {
            sendKeyStackWithAck(keyStack.slice());
            keyStack = [];
        }
    }
}

function clearKeyStack(){
    keyStackingActive = false;
    $("#keystackDisplay").hide();
    keyStack = [];
    _suppressKeyUpCodes.clear();
    console.log('[KeyStack] Stacking mode OFF, combo cleared');
}

// In stacking mode, capture keys into the stack instead of sending immediately. 
// Also track which keys to suppress keyUp for.
function handleStackModeKeyDown(event){
    let stackKeyCode = applyCtrlCmdSwap(event.keyCode);
    let stackKey = applyCtrlCmdSwapKey(event.key);
    const rightModKeys = ['Control', 'Alt', 'Shift', 'Meta'];
    const isRight = (rightModKeys.includes(stackKey) && event.location === 2) ||
                    (event.key === 'Enter' && isNumpadEvent(event));
    if (keyStack.length >=6){
        // Reached the max number of USB HID keys that can be sent in a combo, ignore additional keys
        $.toast({ message: '<i class="red circle times icon"></i> Max 6 keys in combo, ignoring: ' + event.key, duration: 3000 });
        return;
    }
    // Ignore repeated keydown events (key held)
    if (!keyStack.some(k => k.code === event.code)) {
        keyStack.push({ key: event.key, keycode: stackKeyCode, isRightModKey: isRight, code: event.code });
        _suppressKeyUpCodes.add(event.code);
        console.log(`[KeyStack] + ${event.key} (keyCode=${stackKeyCode})  stack: [${keyStack.map(k => k.key).join(', ')}]`);

        // Render the keystack to the display
        let html = keyStack.map(k => `<div class="ui green label">${k.code}</div>`).join('');
        document.getElementById('keystackContent').innerHTML = html;
    }
    return;
}

// handleKeyDown is the main keyboard event handler. 
// It manages normal key sending as well as key stacking and paste prompt interception.
function handleKeyDown(event) {
    //Check if there are any settings / modal open, if yes don't capture paste events
    if (pauseAllKeyEvents){
        return;
    }

    // Intercept paste (Ctrl+V / Cmd+V) when askOnPaste is enabled
    if (askOnPaste && (event.key === 'v' || event.key === 'V') && (event.ctrlKey || event.metaKey)) {
        // Not handled here, will be handled by paste event listener to show prompt
        return;
    }

    event.preventDefault();
    event.stopImmediatePropagation();

    // Key stacking toggle and capture
    if (keyStackingEnabled && event.code === stackToggleKey) {
         // Intercept the key stacking toggle key (matched by event.code string since we want to allow ShiftLeft vs ShiftRight distinction)
        handleStackToggleKeyDown(event);
        return;
    }else if (keyStackingActive) {
          // While stacking is active, capture the key into the stack instead of sending it
        handleStackModeKeyDown(event);
        return;
    }

    // Ordinary key press, send to server as usual
    const key = event.key;
    let keyCode = applyCtrlCmdSwap(event.keyCode);
    let swappedKey = applyCtrlCmdSwapKey(key);
    let hidCommand = {
        event: HIDEvent.KEY_DOWN,
        keycode: keyCode
    };

    if (enableKvmEventDebugPrintout) {
        console.log(`Key down: ${key} (code: ${event.keyCode}) -> sent code: ${keyCode}`);
    }

    // Check if the key is a modkey on the right side of the keyboard
    const rightModKeys = ['Control', 'Alt', 'Shift', 'Meta'];
    if (rightModKeys.includes(swappedKey) && event.location === 2) {
        hidCommand.is_right_modifier_key = true;
    }else if (key === 'Enter' && isNumpadEvent(event)) {
        //Special case for Numpad Enter
        hidCommand.is_right_modifier_key = true;
    }else{
        hidCommand.is_right_modifier_key = false;
    }

    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        hidsocket.send(JSON.stringify(hidCommand));
    } else {
        console.error("WebSocket is not open.");
    }
}

function handleKeyUp(event) {
    //Check if there are any settings / modal open, if yes don't capture paste events
    if (pauseAllKeyEvents){
        return;
    }

    // Always swallow keyUp for the toggle key itself
    event.preventDefault();
    event.stopImmediatePropagation();

    // Key Stacking events
    if (keyStackingEnabled && event.code === stackToggleKey) {
        //Toggle key released
        return;
    }else if (_suppressKeyUpCodes.has(event.code)) {
        // Swallow keyUp for keys that were captured into the stack
        _suppressKeyUpCodes.delete(event.code);
        return;
    }else if (keyStackingActive) {
        // Also suppress keyUp for any key pressed while stacking is still active
        handleStackModeKeyUp(event);
        return;
    }

    // Ordinary key release, send to server as usual
    const key = event.key;
    let keyCode = applyCtrlCmdSwap(event.keyCode);
    let swappedKey = applyCtrlCmdSwapKey(key);

    let hidCommand = {
        event: HIDEvent.KEY_UP,
        keycode: keyCode
    };

    if (enableKvmEventDebugPrintout) {
        console.log(`Key up: ${key} (code: ${event.keyCode}) -> sent code: ${keyCode}`);
    }

    // Check if the key is a modkey on the right side of the keyboard
    const rightModKeys = ['Control', 'Alt', 'Shift', 'Meta'];
    if (rightModKeys.includes(swappedKey) && event.location === 2) {
        hidCommand.is_right_modifier_key = true;
    } else if (key === 'Enter' && isNumpadEvent(event)) {
        //Special case for Numpad Enter
        hidCommand.is_right_modifier_key = true;
    }else{
        hidCommand.is_right_modifier_key = false;
    }


    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        hidsocket.send(JSON.stringify(hidCommand));
    } else {
        console.error("WebSocket is not open.");
    }
}

/*
    Paste capture event handler
*/

function handlePasteEvent(event) {
    //Check if there are any settings / modal open, if yes don't capture paste events
    if (pauseAllKeyEvents){
        return;
    }

    event.preventDefault();
    event.stopImmediatePropagation();

    let paste = (event.clipboardData || window.clipboardData).getData("text");
    console.log("paste event: ", paste);
    openPasteModal(paste);
}

function openPasteModal(clipText) {
    var preview = clipText ? clipText.substring(0, 120) : '';
    if (clipText && clipText.length > 120) preview += '…';
    var escapedPreview = $('<span>').text(preview).html();
    var modKey = swapCtrlCmd ? 'Cmd' : 'Ctrl';

    //Check if clipboard is empty, if yes send Ctrl+V directly
    if (!clipText) {
        pastePromptSendKey();
        return;
    }else{
        // Since Ctrl is already held by remote side
        // we need to release Ctrl before proceeding
        var modKeyCode = swapCtrlCmd ? 91 : 17; // Meta or Ctrl
        if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
            // Modifier up
            hidsocket.send(JSON.stringify({ event: HIDEvent.KEY_UP, keycode: modKeyCode, is_right_modifier_key: false }));
        }
    }

    var modal = $('#pastePromptModal');
    modal.find('.paste-prompt-preview').html(escapedPreview || '<i>(empty clipboard)</i>');
    modal.find('.paste-prompt-sendkey-label').text('Send ' + modKey + '+V to remote');
    modal.data('clipText', clipText);
    pauseAllKeyEvents = true;

    modal.modal({
        closable: true,
        onHidden: function() {
            pauseAllKeyEvents = false;
        }
    }).modal('show');
}

function pastePromptSendText() {
    var modal = $('#pastePromptModal');
    var clipText = modal.data('clipText') || '';
    modal.modal('hide');
    if (!clipText) {
        $.toast({ message: '<i class="yellow exclamation triangle icon"></i> Clipboard is empty', duration: 3000 });
        return;
    }
    // Set .value (NOT .innerHTML): innerHTML parses the clipboard text as
    // HTML (mangling entities) and stops tracking .value once the textarea
    // is dirty, which made this path send stale/garbled characters.
    const ta = document.getElementById('pasteTextarea');
    ta.value = clipText.substring(0, typeof PASTE_BOX_MAX_CHARS !== 'undefined' ? PASTE_BOX_MAX_CHARS : 1000);
    if (typeof updatePasteBoxCharCounter === 'function') {
        updatePasteBoxCharCounter();
    }
    setTimeout(function() {
        //Reusing paste-box.js, sendPasteText() will do the textarea cleanup after sending the text,
        // so no need to clear it here
        sendPasteText();
    }, 500);
}

function pastePromptSendKey() {
    $('#pastePromptModal').modal('hide');
    // Send Ctrl+V (or Meta+V if swapped) as key down/up
    var modKeyCode = swapCtrlCmd ? 91 : 17; // Meta or Ctrl
    var vKeyCode = 86; // V
    if (!hidsocket || hidsocket.readyState !== WebSocket.OPEN) return;
    // Modifier down
    hidsocket.send(JSON.stringify({ event: HIDEvent.KEY_DOWN, keycode: modKeyCode, is_right_modifier_key: false }));
    // V down
    hidsocket.send(JSON.stringify({ event: HIDEvent.KEY_DOWN, keycode: vKeyCode, is_right_modifier_key: false }));
    // V up
    hidsocket.send(JSON.stringify({ event: HIDEvent.KEY_UP, keycode: vKeyCode, is_right_modifier_key: false }));
    // Modifier up
    hidsocket.send(JSON.stringify({ event: HIDEvent.KEY_UP, keycode: modKeyCode, is_right_modifier_key: false }));
}

/*
    Send a stacked key combo with ACK: all keydowns first, then all keyups.
    E.g. stack=[Ctrl, Alt, Del] → keydown Ctrl, keydown Alt, keydown Del,
                                   keyup  Ctrl, keyup  Alt, keyup  Del.
*/
async function sendKeyStackWithAck(stack) {
    // All key-downs in order
    for (const entry of stack) {
        await sendHidWithAck({ event: HIDEvent.KEY_DOWN, keycode: entry.keycode, is_right_modifier_key: entry.isRightModKey });
    }
    // All key-ups in the same order
    for (const entry of stack) {
        await sendHidWithAck({ event: HIDEvent.KEY_UP, keycode: entry.keycode, is_right_modifier_key: entry.isRightModKey });
    }
    console.log('[KeyStack] Combo sent.');
}

/*
    ACK-based HID send: sends a HID command and waits for the backend to
    confirm it was written to the serial port before resolving.
    Timeout at 500ms as a safety net.
*/
function sendHidWithAck(hidCommand) {
    return new Promise(function(resolve) {
        if (!hidsocket || hidsocket.readyState !== WebSocket.OPEN) {
            resolve('error');
            return;
        }
        var rid = 'r' + (++_hidAckCounter);
        hidCommand.rid = rid;

        var timer = setTimeout(function() {
            if (_hidPendingAcks[rid]) {
                delete _hidPendingAcks[rid];
                resolve('timeout');
            }
        }, 500);

        _hidPendingAcks[rid] = { resolve: resolve, timer: timer };
        hidsocket.send(JSON.stringify(hidCommand));

        if (_hidAckCounter > 1e6) {
            // Prevent _hidAckCounter overflow in long sessions
            _hidAckCounter = 0;
        }
    });
}

/*
    Send a single key press+release with ACK. Waits for the MCU to confirm
    the final release before resolving, so the next keystroke is safe to send.
*/
async function sendKeyPressWithAck(keycode, needsShift) {
    if (needsShift) {
        await sendHidWithAck({ event: HIDEvent.KEY_DOWN, keycode: 16, is_right_modifier_key: false });
    }
    await sendHidWithAck({ event: HIDEvent.KEY_DOWN, keycode: keycode, is_right_modifier_key: false });
    await sendHidWithAck({ event: HIDEvent.KEY_UP, keycode: keycode, is_right_modifier_key: false });
    if (needsShift) {
        await sendHidWithAck({ event: HIDEvent.KEY_UP, keycode: 16, is_right_modifier_key: false });
    }
}


/* Start and Stop HID events */
function attachHidEventListeners() {
    const remoteCaptureEle = document.getElementById(cursorCaptureElementId);
    if (!remoteCaptureEle) {
        console.error('Remote capture element not found');
        return;
    }

    // Attach keyboard event listeners to document (so they work globally)
    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('keyup', handleKeyUp);
    document.addEventListener('paste', handlePasteEvent);
    

    remoteCaptureEle.addEventListener('mousemove', handleMouseMove);
    remoteCaptureEle.addEventListener('mousedown', handleMousePress);
    remoteCaptureEle.addEventListener('mouseup', handleMouseRelease);
    remoteCaptureEle.addEventListener('wheel', handleMouseScroll);
    
    console.log('HID event listeners attached');
}

function detachHidEventListeners() {
    const remoteCaptureEle = document.getElementById(cursorCaptureElementId);
    if (!remoteCaptureEle) {
        console.error('Remote capture element not found');
        return;
    }

    // Remove keyboard event listeners from document
    document.removeEventListener('keydown', handleKeyDown);
    document.removeEventListener('keyup', handleKeyUp);
    document.removeEventListener('paste', handlePasteEvent);

    // Remove mouse event listeners from capture element
    remoteCaptureEle.removeEventListener('mousemove', handleMouseMove);
    remoteCaptureEle.removeEventListener('mousedown', handleMousePress);
    remoteCaptureEle.removeEventListener('mouseup', handleMouseRelease);
    remoteCaptureEle.removeEventListener('wheel', handleMouseScroll);
    
    console.log('HID event listeners detached');
}

function startHidWebSocket(){
    if (sessionTakenOver) {
        return; // another page owns the session; "Take back control" reloads
    }
    if (hidsocket){
        //Already started
        console.warn("Invalid usage: HID Transport Websocket already started!");
        return;
    }
    const socketUrl = hidSocketURL;
    hidsocket = new WebSocket(socketUrl);

    hidsocket.addEventListener('open', function(event) {
        console.log('HID Transport WebSocket is connected.');

        // Send a soft reset command to the server to reset the HID state
        // that possibly got out of sync from previous session
        const hidResetCommand = {
            event: HIDEvent.RESET
        };
        hidsocket.send(JSON.stringify(hidResetCommand));
    });

    // Closed by the backend because another page took over the session
    hidsocket.addEventListener('close', function(event) {
        if (event.code === 4001) {
            handleSessionTakenOver();
        }
    });

    hidsocket.addEventListener('message', function(event) {
        // Handle ACK replies from backend
        try {
            var msg = JSON.parse(event.data);
            if (msg.type === 'takeover') {
                handleSessionTakenOver(msg.reason);
                return;
            }
            if (msg.rid && _hidPendingAcks[msg.rid]) {
                clearTimeout(_hidPendingAcks[msg.rid].timer);
                _hidPendingAcks[msg.rid].resolve(msg.status);
                delete _hidPendingAcks[msg.rid];
            }
        } catch(e) {
            // Not JSON or not an ACK — ignore
        }
    });

  
}

// Attach event listeners on page load
attachHidEventListeners();

function stopHidWebSocket(){
    if (!hidsocket){
        if (sessionTakenOver) return; // already closed by the takeover
        alert("No ws connection to stop");
        return;
    }

    hidsocket.close();
    hidsocket = null;
    console.log('HID Transport WebSocket disconnected.');
}

/* Reset remote HID state */
function resetRemoteHID() {
    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        const hidResetCommand = {
            event: HIDEvent.RESET
        };
        hidsocket.send(JSON.stringify(hidResetCommand));
        console.log('HID reset command sent');

        $.toast({
            message: '<i class="ui green circle check icon"></i> Remote HID state reset completed',
        });
    } else {
        alert('HID WebSocket is not connected');
    }
}



/* Audio Streaming Frontend */
let audioSocket = null;
let audioContext = null;
let audioGainNode = null; // gain of the websocket PCM playback
let audioQueue = [];
let audioPlaying = false;
let _audioHintShown = false;

/* Remote audio settings, stored per browser (settings overlay, Audio tab) */
function getAudioQuality() {
    return localStorage.getItem('audioQuality') || 'standard';
}

/* "Always transfer audio via WebSocket": keep the PCM websocket even when
   the video runs over WebRTC */
function audioForcedWebSocket() {
    return localStorage.getItem('audioForceWebSocket') === 'true';
}

/* Playback gain in percent, 0-200 (100 = unchanged) */
function getAudioGainPercent() {
    const v = parseInt(localStorage.getItem('audioGain'), 10);
    return isNaN(v) ? 100 : Math.min(200, Math.max(0, v));
}
let remoteAudioGain = getAudioGainPercent() / 100;

/* Set the playback gain (percent, 0-200) of whichever audio path is
   playing, and remember it for this browser. */
function setRemoteAudioGain(percent) {
    percent = Math.min(200, Math.max(0, Math.round(Number(percent) || 0)));
    remoteAudioGain = percent / 100;
    localStorage.setItem('audioGain', String(percent));
    [[audioContext, audioGainNode], [_rtcAudioCtx, _rtcAudioGain]].forEach(function (pair) {
        const ctx = pair[0], node = pair[1];
        if (ctx && node) {
            // short ramp, avoids clicks while dragging the slider
            node.gain.setTargetAtTime(remoteAudioGain, ctx.currentTime, 0.02);
        }
    });
    if (_rtcAudioEl && !_rtcAudioGain) {
        _rtcAudioEl.volume = Math.min(1, remoteAudioGain); // no Web Audio: cannot boost
    }
}

/* Audio can come over two transports:
   - WebRTC: when the video runs over WebRTC, the session also carries the
     audio as Opus (unless "Always transfer audio via WebSocket" is set, or
     the server cannot encode Opus). The track is played by playWebRTCAudio.
   - WebSocket: raw PCM over /api/v1/stream/{uuid}/audio, for MJPEG mode.
   startRemoteAudio is the single entry point used by the shell, clicks and
   settings; it opens the websocket only when the WebRTC session does not
   carry the audio. The server has one audio capture, so only one transport
   may run at a time. */
function startRemoteAudio(quality) {
    if (sessionTakenOver) return;
    quality = quality || getAudioQuality();
    if (quality === 'disabled') return;
    remoteAudioWanted = true;
    if (!_streamModeKnown) return; // applyStreamingSource picks the transport
    syncRemoteAudioTransport();
}

/* Apply a changed audio setting (quality or transport). A WebRTC session
   negotiates its audio track up front, so when the change affects it the
   stream is restarted; otherwise only the websocket is. Returns true when
   the stream is being restarted. */
function restartRemoteAudio() {
    const quality = getAudioQuality();
    const wantWebRTCAudio = quality !== 'disabled' && !audioForcedWebSocket();
    const webrtc = typeof webrtcStreamActive === 'function' && webrtcStreamActive();
    if (audioSocket) stopAudioWebSocket();
    audioFrontendStarted = false;
    remoteAudioWanted = quality !== 'disabled';
    if (webrtc && (webrtcCarriesAudio() || wantWebRTCAudio)) {
        setStreamingSource(kvmDeviceUUID); // renegotiates, then syncs the websocket
        return true;
    }
    if (remoteAudioWanted) {
        // Give the server a moment to release the capture
        setTimeout(syncRemoteAudioTransport, 500);
    }
    return false;
}

/* Open or close the audio websocket to match the current stream */
function syncRemoteAudioTransport() {
    if (!remoteAudioWanted || sessionTakenOver || !kvmDeviceUUID) return;
    const quality = getAudioQuality();
    if (quality === 'disabled') return;
    if (typeof webrtcCarriesAudio === 'function' && webrtcCarriesAudio()) {
        if (audioSocket) {
            console.log('Audio moves to the WebRTC connection, closing the audio websocket');
            stopAudioWebSocket();
        }
        resumeAudioContext(true);
    } else if (!audioSocket) {
        startAudioWebSocket(quality);
    }
}

/* Is any remote audio path running (websocket or WebRTC)? */
function remoteAudioTransport() {
    if (_rtcAudioEl) return 'webrtc';
    if (audioSocket) return 'websocket';
    return '';
}

/* ---- WebRTC audio playback ----
   The Opus track of the WebRTC session goes through Web Audio so the gain
   can go above 100%. */
let _rtcAudioEl = null;   // muted <audio> holding the stream (see below)
let _rtcAudioCtx = null;
let _rtcAudioGain = null;

function playWebRTCAudio(stream) {
    stopWebRTCAudio();
    // Chrome only feeds a remote WebRTC stream into Web Audio while the
    // stream is also attached to a media element, so keep a muted one.
    _rtcAudioEl = new Audio();
    _rtcAudioEl.muted = true;
    _rtcAudioEl.autoplay = true;
    _rtcAudioEl.srcObject = stream;
    try {
        _rtcAudioCtx = new (window.AudioContext || window.webkitAudioContext)({ latencyHint: 'interactive' });
        const src = _rtcAudioCtx.createMediaStreamSource(stream);
        _rtcAudioGain = _rtcAudioCtx.createGain();
        _rtcAudioGain.gain.value = remoteAudioGain;
        src.connect(_rtcAudioGain);
        _rtcAudioGain.connect(_rtcAudioCtx.destination);
    } catch (e) {
        // No usable Web Audio: play through the element, gain capped at 100%
        console.warn('Web Audio unavailable for WebRTC audio, playing without gain boost:', e);
        if (_rtcAudioCtx) { try { _rtcAudioCtx.close(); } catch (e2) { /* ignore */ } }
        _rtcAudioCtx = null;
        _rtcAudioGain = null;
        _rtcAudioEl.muted = false;
        _rtcAudioEl.volume = Math.min(1, remoteAudioGain);
    }
    _rtcAudioEl.play().catch(function () { /* resumed on the next click */ });
    audioFrontendStarted = true;
    resumeAudioContext(true);
    console.log('Playing remote audio from the WebRTC connection');
}

function stopWebRTCAudio() {
    if (_rtcAudioEl) {
        _rtcAudioEl.pause();
        _rtcAudioEl.srcObject = null;
        _rtcAudioEl = null;
    }
    if (_rtcAudioCtx) {
        try { _rtcAudioCtx.close(); } catch (e) { /* already closed */ }
        _rtcAudioCtx = null;
    }
    _rtcAudioGain = null;
    if (!audioSocket) audioFrontendStarted = false;
}

/* Resume suspended audio playback (browser autoplay policy). Called after
   the audio starts and again on every user interaction, since some
   browsers only allow the resume during a user gesture. */
function resumeAudioContext(showHint) {
    if (_rtcAudioEl && _rtcAudioEl.paused) {
        _rtcAudioEl.play().catch(function () { /* needs a gesture */ });
    }
    [audioContext, _rtcAudioCtx].forEach(function (ctx) {
        if (!ctx || ctx.state !== 'suspended') return;
        ctx.resume().then(function () {
            console.log('AudioContext resumed');
            _audioHintShown = false;
        }).catch(function () {
            if (showHint && !_audioHintShown && typeof $ !== 'undefined' && $.toast) {
                _audioHintShown = true;
                $.toast({
                    message: '<i class="volume up icon"></i> Click anywhere in the session to enable remote audio',
                    duration: 6000
                });
            }
        });
    });
}

// Any interaction inside the viewport may unlock audio playback; a click is
// also the retry path when the audio socket failed to start earlier.
function _audioGestureKick() {
    if (sessionTakenOver) return; // the takeover screen must not restart audio
    resumeAudioContext(false);
    if (kvmDeviceUUID) startRemoteAudio();
}
document.addEventListener('click', _audioGestureKick, true);
document.addEventListener('keydown', function () { resumeAudioContext(false); }, true);

//accept low, standard, high quality audio mode
function startAudioWebSocket(quality="standard") {
    if (sessionTakenOver) {
        return; // another page owns the session (the server refuses it too)
    }
    if (audioSocket) {
        console.warn("Audio WebSocket already started");
        return;
    }

    audioSocket = new WebSocket(`${audioSocketURL}?quality=${quality}&client=${encodeURIComponent(dezClientId)}`);
    audioSocket.binaryType = 'arraybuffer';

    audioSocket.onopen = function() {
        console.log("Audio WebSocket connected");
        audioFrontendStarted = true;
        if (!audioContext) {
            audioContext = new (window.AudioContext || window.webkitAudioContext)({sampleRate: 24000});
            audioGainNode = audioContext.createGain();
            audioGainNode.gain.value = remoteAudioGain;
            audioGainNode.connect(audioContext.destination);
        }
        // Browser autoplay policies start the context suspended unless we
        // are inside a user-activation window. Try to resume immediately
        // and tell the user to interact when the browser refuses.
        resumeAudioContext(true);
    };


    const MAX_AUDIO_QUEUE = 4;
    let PCM_SAMPLE_RATE;
    if (quality == "high"){
        PCM_SAMPLE_RATE = 48000; // Use 48kHz for high quality
    } else if (quality == "low") {
        PCM_SAMPLE_RATE = 16000; // Use 24kHz for low quality
    } else {
        PCM_SAMPLE_RATE = 24000; // Default to 24kHz for standard quality
    }
    let scheduledTime = 0;
    audioSocket.onmessage = function(event) {
        if (typeof event.data === 'string') {
            // Text frames are control messages, e.g. a takeover notice
            try {
                const msg = JSON.parse(event.data);
                if (msg.type === 'takeover') handleSessionTakenOver(msg.reason);
            } catch (e) { /* not JSON */ }
            return;
        }
        if (!audioContext) return;
        let pcm = new Int16Array(event.data);
        if (pcm.length === 0) {
            console.warn("Received empty PCM data");
            return;
        }
        if (pcm.length % 2 !== 0) {
            console.warn("Received PCM data with odd length, dropping last sample");
            pcm = pcm.slice(0, -1);
        }
        // Convert Int16 PCM to Float32 [-1, 1]
        let floatBuf = new Float32Array(pcm.length);
        for (let i = 0; i < pcm.length; i++) {
            floatBuf[i] = pcm[i] / 32768;
        }
        // Limit queue size to prevent memory overflow
        if (audioQueue.length >= MAX_AUDIO_QUEUE) {
            audioQueue.shift();
        }
        audioQueue.push(floatBuf);
        scheduleAudioPlayback();
    };

    audioSocket.onclose = function(event) {
        console.log("Audio WebSocket closed");
        if (event && event.code === 4001) {
            // Refused: this page's session was taken over by another page
            audioSocket = null;
            handleSessionTakenOver();
            return;
        }
        audioSocket = null;
        audioFrontendStarted = false;
        audioPlaying = false;
        audioQueue = [];
        scheduledTime = 0;
    };

    audioSocket.onerror = function(e) {
        console.error("Audio WebSocket error", e);
    };

    function scheduleAudioPlayback() {
        if (!audioContext || audioQueue.length === 0) return;

        // Use audioContext.currentTime to schedule buffers back-to-back
        if (scheduledTime < audioContext.currentTime) {
            scheduledTime = audioContext.currentTime;
        }

        while (audioQueue.length > 0) {
            let floatBuf = audioQueue.shift();
            let frameCount = floatBuf.length / 2;
            let buffer = audioContext.createBuffer(2, frameCount, PCM_SAMPLE_RATE);
            for (let ch = 0; ch < 2; ch++) {
                let channelData = buffer.getChannelData(ch);
                for (let i = 0; i < frameCount; i++) {
                    channelData[i] = floatBuf[i * 2 + ch];
                }
            }

            if (scheduledTime - audioContext.currentTime > 0.2) {
                console.warn("Audio buffer too far ahead, discarding frame");
                continue;
            }
            let source = audioContext.createBufferSource();
            source.buffer = buffer;
            source.connect(audioGainNode || audioContext.destination);
            source.start(scheduledTime);
            scheduledTime += buffer.duration;
        }
    }
}

function stopAudioWebSocket() {
    if (!audioSocket) {
        console.warn("No audio WebSocket to stop");
        return;
    }

    if (audioSocket.readyState === WebSocket.OPEN) {
        audioSocket.send("exit");
    }
    audioSocket.onclose = null; // Prevent onclose from being called again
    audioSocket.onerror = null; // Prevent onerror from being called again
    audioSocket.close();
    audioSocket = null;
    audioPlaying = false;
    audioQueue = [];
    if (audioContext) {
        audioContext.close();
        audioContext = null;
    }
    audioGainNode = null;
}

window.addEventListener('beforeunload', function() {
    if (audioSocket) stopAudioWebSocket();
    stopWebRTCAudio();
});

/*
    Another browser page started a video stream for this device and now owns
    the session: the backend already stopped this page's video and closed its
    HID websocket. Stop everything locally (without telling the server, that
    would end the new owner's WebRTC stream) and show the takeover screen.
*/
function handleSessionTakenOver(reason) {
    if (sessionTakenOver) return;
    sessionTakenOver = true;
    console.warn('Session taken over by another client:', reason || '');

    if (typeof stopWebRTCStream === 'function') {
        stopWebRTCStream(false);
    }
    const capEl = document.getElementById('remoteCapture');
    if (capEl && capEl.tagName === 'IMG') {
        capEl.removeAttribute('src'); // stop the MJPEG request
    }
    try { stopAudioWebSocket(); } catch (e) { /* audio was not running */ }
    audioFrontendStarted = false;
    try { detachHidEventListeners(); } catch (e) { /* not attached */ }
    if (document.pointerLockElement) {
        document.exitPointerLock();
    }
    if (hidsocket) {
        try { hidsocket.close(); } catch (e) { /* already closed */ }
        hidsocket = null;
    }
    showTakenOverScreen();
}

function showTakenOverScreen() {
    if (document.getElementById('sessionTakenOverOverlay')) return;
    const overlay = document.createElement('div');
    overlay.id = 'sessionTakenOverOverlay';
    overlay.setAttribute('role', 'alertdialog');
    overlay.setAttribute('aria-labelledby', 'sessionTakenOverTitle');
    overlay.style.cssText = 'position:fixed;inset:0;z-index:10000;display:flex;align-items:center;' +
        'justify-content:center;background:rgba(20,20,20,0.92);color:#eee;text-align:center;padding:16px;';
    overlay.innerHTML =
        '<div style="max-width:420px;">' +
        '  <i class="huge unlinkify icon" style="color:#e05a6a;"></i>' +
        '  <h2 id="sessionTakenOverTitle" style="margin:0.6em 0 0.4em;font-weight:600;">Session taken over</h2>' +
        '  <p style="opacity:0.85;line-height:1.5;margin:0 0 1.4em;">This device is now being viewed and controlled from another ' +
        '  browser window or device. Video, audio, keyboard and mouse have been disconnected from this page.</p>' +
        '  <button class="ui primary button" id="sessionTakeBackButton">Take back control</button>' +
        '</div>';
    document.body.appendChild(overlay);
    document.getElementById('sessionTakeBackButton').addEventListener('click', function() {
        // A fresh page load gets a new client ID and takes the session back
        window.location.reload();
    });
}

// Disconnect both HID and Audio WebSockets
function disconnectRemote(){
    if (audioSocket) stopAudioWebSocket();
    stopHidWebSocket();
}
