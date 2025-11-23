/*
    kvmevt.js

    Keyboard, Video, Mouse (KVM) over WebSocket client-side event handling.
    Handles mouse and keyboard events, sending them to the server via WebSocket.
    Also manages audio streaming from the server.
*/
const enableKvmEventDebugPrintout = false; //Set to true to enable debug printout
const cursorCaptureElementId = "remoteCapture";
let hidsocket;
let hidWebSocketReady = false;
let protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
let port = window.location.port ? window.location.port : (protocol === 'wss' ? 443 : 80);
let hidSocketURL = `${protocol}://${window.location.hostname}:${port}/api/v1/hid/{uuid}/events`;
let audioSocketURL = `${protocol}://${window.location.hostname}:${port}/api/v1/stream/{uuid}/audio`;

let mouseMoveAbsolute = true; // Set to true for absolute mouse coordinates, false for relative
let mouseIsOutside = false; //Mouse is outside capture element
let audioFrontendStarted = false; //Audio frontend has been started
let kvmDeviceUUID = ""; //UUID of the device being controlled


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
}


/* Initiate API endpoint */
function setStreamingSource(deviceUUID) {
    let videoStreamURL = `/api/v1/stream/${deviceUUID}/video`
    let videoElement = document.getElementById("remoteCapture");
    videoElement.src = videoStreamURL;
}

/* Mouse events */
function handleMouseMove(event) {
    const hidCommand = {
        event: 2,
        mouse_x: event.clientX,
        mouse_y: event.clientY,
    };

    const rect = event.target.getBoundingClientRect();
    const relativeX = event.clientX - rect.left;
    const relativeY = event.clientY - rect.top;
    
    if (relativeX < 0 || relativeY < 0 || relativeX > rect.width || relativeY > rect.height) {
        mouseIsOutside = true;
        return; // Mouse is outside the client rect
    }
    mouseIsOutside = false;
    const percentageX = (relativeX / rect.width) * 4096;
    const percentageY = (relativeY / rect.height) * 4096;

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
        event: 3,
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

    if (!audioFrontendStarted && currentAudioQuality !== 'disabled'){
        startAudioWebSocket(currentAudioQuality);
        audioFrontendStarted = true;
    }
}

function handleMouseRelease(event) {
    event.preventDefault();
    event.stopImmediatePropagation();
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
        event: 4,
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
    const hidCommand = {
        event: 5,
        mouse_scroll: event.deltaY
    };
    if (mouseIsOutside) {
        console.warn("Mouse is outside the capture area, ignoring mouse press.");
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

function handleKeyDown(event) {
    event.preventDefault();
    event.stopImmediatePropagation();
    const key = event.key;
    let hidCommand = {
        event: 0,
        keycode: event.keyCode
    };

    if (enableKvmEventDebugPrintout) {
        console.log(`Key down: ${key} (code: ${event.keyCode})`);
    }

    // Check if the key is a modkey on the right side of the keyboard
    const rightModKeys = ['Control', 'Alt', 'Shift', 'Meta'];
    if (rightModKeys.includes(key) && event.location === 2) {
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
    event.preventDefault();
    event.stopImmediatePropagation();
    const key = event.key;
    
    let hidCommand = {
        event: 1,
        keycode: event.keyCode
    };

    if (enableKvmEventDebugPrintout) {
        console.log(`Key up: ${key} (code: ${event.keyCode})`);
    }

    // Check if the key is a modkey on the right side of the keyboard
    const rightModKeys = ['Control', 'Alt', 'Shift', 'Meta'];
    if (rightModKeys.includes(key) && event.location === 2) {
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

/* Start and Stop HID events */
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
            event: 0xFF
        };
        hidsocket.send(JSON.stringify(hidResetCommand));
    });

    hidsocket.addEventListener('message', function(event) {
        //Todo: handle control signals from server if needed
        //console.log('Message from server ', event.data);
    });

  
}

// Attach keyboard event listeners
const remoteCaptureEle = document.getElementById(cursorCaptureElementId);
document.addEventListener('keydown', handleKeyDown);
document.addEventListener('keyup', handleKeyUp);

// Attach mouse event listeners
remoteCaptureEle.addEventListener('click', function(event){
    console.log('Capture element clicked, focusing for keyboard input.');
    remoteCaptureEle.focus();
    event.preventDefault();
});
remoteCaptureEle.addEventListener('mousemove', handleMouseMove);
remoteCaptureEle.addEventListener('mousedown', handleMousePress);
remoteCaptureEle.addEventListener('mouseup', handleMouseRelease);
remoteCaptureEle.addEventListener('wheel', handleMouseScroll);

function stopHidWebSocket(){
    if (!hidsocket){
        alert("No ws connection to stop");
        return;
    }

    hidsocket.close();
    hidsocket = null;
    console.log('HID Transport WebSocket disconnected.');
    document.removeEventListener('keydown', handleKeyDown);
    document.removeEventListener('keyup', handleKeyUp);
}

/* Reset remote HID state */
function resetRemoteHID() {
    if (hidsocket && hidsocket.readyState === WebSocket.OPEN) {
        const hidResetCommand = {
            event: 0xFF
        };
        hidsocket.send(JSON.stringify(hidResetCommand));
        console.log('HID reset command sent');
    } else {
        alert('HID WebSocket is not connected');
    }
}



/* Audio Streaming Frontend */
let audioSocket;
let audioContext;
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
