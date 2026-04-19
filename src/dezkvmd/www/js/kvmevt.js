/*
    kvmevt.js

    Keyboard, Video, Mouse (KVM) over WebSocket client-side event handling.
    Handles mouse and keyboard events, sending them to the server via WebSocket.
    Also manages audio streaming from the server.
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
let kvmDeviceUUID = ""; //UUID of the device being controlled
let swapCtrlCmd = false; // Swap CTRL and CMD (Meta) keys
let askOnPaste = true; // Prompt user when pasting
let pausePasteCapture = false; // Used to temporarily disable paste event handling when modals are open
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
    hidSocketURL = hidSocketURL.replace("{uuid}", kvmDeviceUUID);
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


/* Initiate API endpoint */
function setStreamingSource(deviceUUID) {
    let videoStreamURL = `/api/v1/stream/${deviceUUID}/video`
    let videoElement = document.getElementById("remoteCapture");
    videoElement.src = videoStreamURL;
}

/* Get current streaming resolution, return [width, height] */
function getCurrentStreamingResolution(){
    const img = document.getElementById(streamingContainerId);
    const width = img.naturalWidth || parseInt(parts[0]);
    const height = img.naturalHeight || parseInt(parts[1]);
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
        startAudioWebSocket(currentAudioQuality);
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
            var label = keyStack.map(k => k.key).join(' + ') || '(empty)';
            $.toast({ message: `Sending: ${label}`, duration: 2500 });
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
        let html = keyStack.map(k => `<div class="ui basic green label">${k.code}</div>`).join('');
        document.getElementById('keystackContent').innerHTML = html;
    }
    return;
}

// handleKeyDown is the main keyboard event handler. 
// It manages normal key sending as well as key stacking and paste prompt interception.
function handleKeyDown(event) {
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
    if (pausePasteCapture){
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
    }

    var modal = $('#pastePromptModal');
    modal.find('.paste-prompt-preview').html(escapedPreview || '<i>(empty clipboard)</i>');
    modal.find('.paste-prompt-sendkey-label').text('Send ' + modKey + '+V to remote');
    modal.data('clipText', clipText);
    pausePasteCapture = true;

    modal.modal({
        closable: true,
        onHidden: function() {
            pausePasteCapture = false;
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
    document.getElementById('pasteTextarea').innerHTML = (clipText);
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

    hidsocket.addEventListener('message', function(event) {
        // Handle ACK replies from backend
        try {
            var msg = JSON.parse(event.data);
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
let audioQueue = [];
let audioPlaying = false;

//accept low, standard, high quality audio mode
function startAudioWebSocket(quality="standard") {
    if (audioSocket) {
        console.warn("Audio WebSocket already started");
        return;
    }

    audioSocket = new WebSocket(`${audioSocketURL}?quality=${quality}`);
    audioSocket.binaryType = 'arraybuffer';

    audioSocket.onopen = function() {
        console.log("Audio WebSocket connected");
        if (!audioContext) {
            audioContext = new (window.AudioContext || window.webkitAudioContext)({sampleRate: 24000});
        }
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

    audioSocket.onclose = function() {
        console.log("Audio WebSocket closed");
        audioSocket = null;
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
            source.connect(audioContext.destination);
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
}

window.addEventListener('beforeunload', function() {
    stopAudioWebSocket();
});

// Disconnect both HID and Audio WebSockets
function disconnectRemote(){
    stopAudioWebSocket();
    stopHidWebSocket();
}
