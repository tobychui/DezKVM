/*
    webrtc-stream.js — WebRTC H.264 streaming for the DezKVM viewport.

    When the "streaming_mode" preference is set to "webrtc", the MJPEG <img>
    capture element is swapped for a <video> element (same id, so the HID
    mouse mapping, OCR and CSS keep working) and a recv-only WebRTC peer
    connection is negotiated with the backend, which hardware-encodes the
    HDMI capture to H.264 (see mod/videnc).

    Loaded by viewport.html before kvmevt.js; kvmevt.js decides per the
    stored preferences whether to call startWebRTCStream() or to use the
    classic MJPEG stream.
*/

let _webrtcPC = null;
let _webrtcActive = false;

/*
    Ensure the capture element (#remoteCapture) is the given tag ('img' or
    'video'), swapping the node when needed. HID event listeners are
    re-attached after a swap since they bind to the concrete element.
*/
function ensureCaptureElement(tag) {
    let el = document.getElementById('remoteCapture');
    if (el && el.tagName.toLowerCase() === tag) {
        return el;
    }
    const fresh = document.createElement(tag);
    fresh.id = 'remoteCapture';
    fresh.className = el ? el.className : '';
    fresh.setAttribute('oncontextmenu', 'return false;');
    if (tag === 'video') {
        fresh.autoplay = true;
        fresh.muted = true;       // audio runs over the separate PCM websocket
        fresh.playsInline = true;
    }

    if (typeof detachHidEventListeners === 'function') {
        try { detachHidEventListeners(); } catch (e) { /* not attached yet */ }
    }
    if (el) {
        el.replaceWith(fresh);
    } else {
        document.getElementById('streamWrapper').appendChild(fresh);
    }
    if (typeof attachHidEventListeners === 'function') {
        try { attachHidEventListeners(); } catch (e) { console.error(e); }
    }
    return fresh;
}

/* Wait for ICE gathering to complete, bounded by timeoutMs. */
function _webrtcWaitIce(pc, timeoutMs) {
    if (pc.iceGatheringState === 'complete') return Promise.resolve();
    return new Promise(function (resolve) {
        const timer = setTimeout(resolve, timeoutMs);
        pc.addEventListener('icegatheringstatechange', function () {
            if (pc.iceGatheringState === 'complete') {
                clearTimeout(timer);
                resolve();
            }
        });
    });
}

/*
    Start the WebRTC stream for a device. prefs is the preferences object
    from /api/v1/preferences/{uuid} (may be null).
*/
async function startWebRTCStream(deviceUUID, prefs) {
    stopWebRTCStream(false);

    const videoEl = ensureCaptureElement('video');
    _webrtcActive = true;

    const pc = new RTCPeerConnection({
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
    });
    _webrtcPC = pc;

    pc.addTransceiver('video', { direction: 'recvonly' });
    pc.ontrack = function (evt) {
        videoEl.srcObject = evt.streams[0];
        videoEl.play().catch(function () { /* autoplay is muted, should not block */ });
    };
    pc.oniceconnectionstatechange = function () {
        if (!_webrtcActive) return;
        if (pc.iceConnectionState === 'failed') {
            $.toast({ class: 'error', message: '<i class="red times icon"></i> WebRTC connection failed', duration: 5000 });
        }
    };

    try {
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        await _webrtcWaitIce(pc, 2000);
    } catch (e) {
        console.error('WebRTC offer failed:', e);
        webrtcFallbackToMJPEG(deviceUUID, 'Browser WebRTC setup failed');
        return;
    }

    // Encoder settings from preferences (backend fills any blanks itself)
    const body = {
        sdp: pc.localDescription.sdp,
        encoder: (prefs && prefs.video_encoder) || 'auto',
        variant: (prefs && prefs.video_encoder_variant) || '',
        bitrate_kbps: (prefs && prefs.video_bitrate_kbps) || 0
    };

    const csrf = document.querySelector('meta[name="dezkvm.csrf.token"]');
    $.ajax({
        url: '/api/v1/webrtc/' + deviceUUID + '/offer',
        method: 'POST',
        contentType: 'application/json',
        headers: { 'dezkvm_csrf_token': csrf ? csrf.getAttribute('content') : '' },
        data: JSON.stringify(body),
        success: function (resp) {
            if (!_webrtcActive || _webrtcPC !== pc) return; // superseded meanwhile
            pc.setRemoteDescription({ type: 'answer', sdp: resp.sdp }).then(function () {
                console.log('WebRTC stream established via', resp.encoder_name);
                $.toast({
                    message: '<i class="green video icon"></i> WebRTC stream: ' + (resp.encoder_name || 'connected'),
                    duration: 4000
                });
            }).catch(function (e) {
                console.error('WebRTC answer rejected:', e);
                webrtcFallbackToMJPEG(deviceUUID, 'SDP negotiation failed');
            });
        },
        error: function (xhr) {
            webrtcFallbackToMJPEG(deviceUUID, (xhr.responseText || 'Server rejected WebRTC session').trim());
        }
    });
}

/*
    Stop the WebRTC stream. When notifyServer is true the backend session
    (and its hardware encoder) is released immediately instead of waiting
    for the ICE disconnect timeout.
*/
function stopWebRTCStream(notifyServer) {
    _webrtcActive = false;
    if (_webrtcPC) {
        try { _webrtcPC.close(); } catch (e) { /* already closed */ }
        _webrtcPC = null;
    }
    const el = document.getElementById('remoteCapture');
    if (el && el.tagName.toLowerCase() === 'video') {
        el.srcObject = null;
    }
    if (notifyServer && typeof kvmDeviceUUID !== 'undefined' && kvmDeviceUUID) {
        const csrf = document.querySelector('meta[name="dezkvm.csrf.token"]');
        $.ajax({
            url: '/api/v1/webrtc/' + kvmDeviceUUID + '/stop',
            method: 'POST',
            headers: { 'dezkvm_csrf_token': csrf ? csrf.getAttribute('content') : '' }
        });
    }
}

/* Fall back to the MJPEG stream after a WebRTC failure. */
function webrtcFallbackToMJPEG(deviceUUID, reason) {
    console.warn('Falling back to MJPEG:', reason);
    $.toast({
        message: '<i class="yellow warning icon"></i> WebRTC unavailable (' + reason + '), using MJPEG stream',
        duration: 6000
    });
    stopWebRTCStream(true);
    const img = ensureCaptureElement('img');
    img.src = '/api/v1/stream/' + deviceUUID + '/video?t=' + Date.now();
}
