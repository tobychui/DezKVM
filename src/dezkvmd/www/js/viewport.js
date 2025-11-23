/*
    viewport.js
*/
let massStorageSwitchURL = "/api/v1/mass_storage/switch"; //side accept kvm or remote
let resolutionsAPIURL = "/api/v1/resolutions/{uuid}";
let currentResolutionAPIURL = "/api/v1/resolution/{uuid}";
let changeResolutionAPIURL = "/api/v1/resolution/change";

// Audio quality setting (low, standard, high)
// Check if localStorage has audio quality, set default to 'standard' if not
if (!localStorage.getItem('audioQuality')) {
    currentAudioQuality = 'standard';
    localStorage.setItem('audioQuality', 'standard');
}
let currentAudioQuality = localStorage.getItem('audioQuality');

// CSRF-protected AJAX function
$.cjax = function(payload){
    let requireTokenMethod = ["POST", "PUT", "DELETE"];
    if (requireTokenMethod.includes(payload.method) || requireTokenMethod.includes(payload.type)){
        //csrf token is required
        let csrfToken = document.getElementsByTagName("meta")["dezkvm.csrf.token"].getAttribute("content");
        payload.headers = {
            "dezkvm_csrf_token": csrfToken,
        }
    }

    $.ajax(payload);
}

$(document).ready(function() {
    // Check if the user has opted out of seeing the audio tips
    if (localStorage.getItem('dontShowAudioTipsAgain') === 'true') {
        $('#audioTips').remove();
    }

    // Hide advanced menu by default
    const advMenu = document.getElementById('advance-menu');
    if (advMenu && !advMenu.classList.contains('hide')) {
        closeAdvanceMenu();
    }

    // Load supported resolutions
    loadSupportedResolutions();
    
    // Set audio quality dropdown to saved preference
    $('#audioQualitySelect').val(currentAudioQuality);
});



/* Mass Storage Switch */
function switchMassStorageToRemote(){
    $.cjax({
        url: massStorageSwitchURL,
        type: 'POST',
        data: {
            side: 'remote',
            uuid: kvmDeviceUUID
        },
        success: function(response) {
            if (response.error) {
                alert('Error switching Mass Storage to Remote: ' + response.error);
            }
        },
        error: function(xhr, status, error) {
            alert('Error switching Mass Storage to Remote: ' + error);
        }
    });
}

function switchMassStorageToKvm(){
    $.cjax({
        url: massStorageSwitchURL,
        type: 'POST',
        data: {
            side: 'kvm',
            uuid: kvmDeviceUUID
        },
        success: function(response) {
            if (response.error) {
                alert('Error switching Mass Storage to KVM: ' + response.error);
            }
        },
        error: function(xhr, status, error) {
            alert('Error switching Mass Storage to KVM: ' + error);
        }
    });
}


/*
    UI elements and events
*/

function closeAdvanceMenu() {
    const advMenu = document.getElementById('advance-menu');
    const menu = document.getElementById('menu');
    advMenu.classList.add('hide');
    const btn = document.getElementById('btnToggleAdvanceMenu');
    const icon = btn.querySelector('i');
    icon.classList.remove('angle', 'up');
    icon.classList.add('angle', 'down');
    menu.style.width = '6.2em';
}

function showAdvanceMenu() {
    const advMenu = document.getElementById('advance-menu');
    const menu = document.getElementById('menu');
    advMenu.classList.remove('hide');
    const btn = document.getElementById('btnToggleAdvanceMenu');
    const icon = btn.querySelector('i');
    icon.classList.remove('angle', 'down');
    icon.classList.add('angle', 'up');
    menu.style.width = 'auto';
}

function toggleAdvanceMenu() {
    const advMenu = document.getElementById('advance-menu');
    const menu = document.getElementById('menu');
    advMenu.classList.toggle('hide');
    const btn = document.getElementById('btnToggleAdvanceMenu');
    const icon = btn.querySelector('i');
    if (advMenu.classList.contains('hide')) {
        closeAdvanceMenu();
    } else {
        showAdvanceMenu();
    }
}


function toggleFullScreen(){
    let elem = document.documentElement;
    if (!document.fullscreenElement) {
        if (elem.requestFullscreen) {
            elem.requestFullscreen();
        } else if (elem.mozRequestFullScreen) { // Firefox
            elem.mozRequestFullScreen();
        } else if (elem.webkitRequestFullscreen) { // Chrome, Safari, Opera
            elem.webkitRequestFullscreen();
        } else if (elem.msRequestFullscreen) { // IE/Edge
            elem.msRequestFullscreen();
        }
    } else {
        if (document.exitFullscreen) {
            document.exitFullscreen();
        } else if (document.mozCancelFullScreen) {
            document.mozCancelFullScreen();
        } else if (document.webkitExitFullscreen) {
            document.webkitExitFullscreen();
        } else if (document.msExitFullscreen) {
            document.msExitFullscreen();
        }
    }
}

/* 
    Audio Quality Management 
*/
function changeAudioQuality(quality) {
    if (!quality || !['low', 'standard', 'high', 'disabled'].includes(quality)) {
        console.error('Invalid audio quality:', quality);
        return;
    }
    
    console.log('Changing audio quality to:', quality);
    
    // Save the quality preference
    currentAudioQuality = quality;
    localStorage.setItem('audioQuality', quality);
    
    // Handle disabled audio
    if (quality === 'disabled') {
        if (audioFrontendStarted && audioSocket) {
            console.log('Disabling audio...');
            stopAudioWebSocket();
            audioFrontendStarted = false;
        }
        console.log('Audio disabled');
        return;
    }
    
    // Restart audio WebSocket with new quality if it's currently running
    if (audioFrontendStarted && audioSocket) {
        console.log('Restarting audio with new quality...');
        stopAudioWebSocket();
        audioFrontendStarted = false;
    }

    // Wait a moment for cleanup, then restart with new quality
    setTimeout(function() {
        startAudioWebSocket(currentAudioQuality);
        audioFrontendStarted = true;
        console.log('Audio restarted with quality:', currentAudioQuality);
        $.toast({
            message: "<b>Audio Quality Updated</b><br>Click on the video area if you do not hear audio shortly.",
        });
    }, 500);
}

/* 
    Resolution Management 
*/
function loadSupportedResolutions() {
    if (!kvmDeviceUUID) {
        console.warn("KVM device UUID not set, cannot load resolutions");
        return;
    }

    const url = resolutionsAPIURL.replace("{uuid}", kvmDeviceUUID);
    $.cjax({
        url: url,
        type: 'GET',
        success: function(resolutions) {
            populateResolutionDropdown(resolutions);
            loadCurrentResolution();
        },
        error: function(xhr, status, error) {
            console.error('Error loading supported resolutions:', error);
        }
    });
}

function populateResolutionDropdown(resolutions) {
    const dropdown = document.getElementById('resolutionSelect');
    if (!dropdown) {
        console.error('Resolution dropdown not found');
        return;
    }

    // Clear existing options
    dropdown.innerHTML = '';

    // Build resolution options from the format info
    const isChrome = navigator.userAgent.includes('Chrome');
    for (const formatInfo of resolutions) {
        // We're interested in MJPEG format primarily
        if (formatInfo.Format.toLowerCase() !== 'mjpg') {
            continue;
        }

        for (const sizeInfo of formatInfo.Sizes) {
            for (const fps of sizeInfo.FPS) {
                const resolutionStr = `${sizeInfo.Width}x${sizeInfo.Height}`;
                const optionText = `${resolutionStr} @ ${fps}fps`;
                const optionValue = `${sizeInfo.Width}x${sizeInfo.Height}x${fps}`;
                
                const option = document.createElement('option');
                option.value = optionValue;
                option.text = optionText;
                dropdown.appendChild(option);
            }
        }
    }

    console.log('Resolution dropdown populated with', dropdown.options.length, 'options');
}

function loadCurrentResolution() {
    if (!kvmDeviceUUID) {
        console.warn("KVM device UUID not set, cannot load current resolution");
        return;
    }

    const url = currentResolutionAPIURL.replace("{uuid}", kvmDeviceUUID);
    $.cjax({
        url: url,
        type: 'GET',
        success: function(currentResolution) {
            setCurrentResolutionAsDefault(currentResolution);
        },
        error: function(xhr, status, error) {
            console.error('Error loading current resolution:', error);
        }
    });
}

function setCurrentResolutionAsDefault(currentResolution) {
    const dropdown = document.getElementById('resolutionSelect');
    if (!dropdown) {
        console.error('Resolution dropdown not found');
        return;
    }

    const currentValue = `${currentResolution.Width}x${currentResolution.Height}x${currentResolution.FPS}`;
    console.log('Setting current resolution as default:', currentValue);

    // Find and select the matching option
    for (let i = 0; i < dropdown.options.length; i++) {
        if (dropdown.options[i].value === currentValue) {
            dropdown.selectedIndex = i;
            console.log('Current resolution set as default');
            break;
        }
    }
}

function changeResolution(resolutionValue) {
    if (!resolutionValue) {
        console.error('No resolution value provided');
        return;
    }

    // Parse the resolution value (format: WIDTHxHEIGHTxFPS)
    const parts = resolutionValue.split('x');
    if (parts.length !== 3) {
        console.error('Invalid resolution format:', resolutionValue);
        return;
    }

    const width = parseInt(parts[0]);
    const height = parseInt(parts[1]);
    const fps = parseInt(parts[2]);

    if (isNaN(width) || isNaN(height) || isNaN(fps)) {
        console.error('Invalid resolution values:', width, height, fps);
        return;
    }

    console.log(`Changing resolution to ${width}x${height} @ ${fps}fps`);

    // Show the resolution change modal
    showResolutionChangeModal();

    // Call the API to change resolution
    $.cjax({
        url: changeResolutionAPIURL,
        type: 'POST',
        data: {
            uuid: kvmDeviceUUID,
            width: width,
            height: height,
            fps: fps
        },
        success: function(response) {
            console.log('Resolution changed successfully:', response);
            // Prepare streams for reconnection
            prepareStreamsReconnection();
            // Show resume button
            showResumeButton();
        },
        error: function(xhr, status, error) {
            hideResolutionChangeModal();
            alert('Error changing resolution: ' + (xhr.responseText || error));
            console.error('Error changing resolution:', error);
        }
    });
}

function reconnectStreams() {
    console.log('Reconnecting streams...');
    
    // Stop audio and HID websockets
    if (audioFrontendStarted) {
        stopAudioWebSocket();
        audioFrontendStarted = false;
    }
    disconnectRemote();

    // Wait a moment for cleanup, then restart
    setTimeout(function() {
        // Reload the video stream
        setStreamingSource(kvmDeviceUUID);
        
        // Restart HID WebSocket
        startHidWebSocket();

        // Change the img src to force reload
        $("#remoteCapture").attr('src', $("#remoteCapture").attr('src') + "?t=" + Date.now());
        
        // Audio will be restarted when user clicks on the video (with current quality setting)
        console.log('Streams reconnected');
    }, 1000);
}

/* Resolution Change Modal Functions */
function showResolutionChangeModal() {
    // Reset modal to original state
    $('#resolutionChangeModal .icon.header').html('<i class="spinner loading icon"></i> Changing Resolution');
    $('#resolutionChangeModal p').text('Changing resolution of HDMI capture, please wait...');
    $('#resumeSessionBtn').hide();
    
    $('#resolutionChangeModal').modal({
        closable: false,
        autofocus: false
    })
    .modal('show');
}

function hideResolutionChangeModal() {
    $('#resolutionChangeModal').modal('hide');
}

function showResumeButton() {
    // Hide the loading spinner and message
    $('#resolutionChangeModal .icon.header i').removeClass('spinner loading').addClass('checkmark');
    $('#resolutionChangeModal .icon.header').html('<i class="green circle check icon"></i> Resolution Changed');
    $('#resolutionChangeModal p').text('Resolution has been changed successfully!');
    
    // Show the resume button
    $('#resumeSessionBtn').show();
}

function prepareStreamsReconnection() {
    console.log('Preparing streams for reconnection...');
    
    // Stop audio and HID websockets
    if (audioFrontendStarted) {
        stopAudioWebSocket();
        audioFrontendStarted = false;
    }
    disconnectRemote();
}

function resumeSession(event) {
    console.log('Resuming session...');
    
    // Hide the modal
    hideResolutionChangeModal();
    
    // Wait a moment for cleanup, then restart everything
    setTimeout(function() {
        // Reload the video stream
        setStreamingSource(kvmDeviceUUID);
        
        // Restart HID WebSocket
        startHidWebSocket();
        $.toast({
            message: 'Reconnecting HID device<br>This will only take a moment',
            showProgress: 'bottom',
            classProgress: 'blue'
        });

        // Change the img src to force reload
        $("#remoteCapture").attr('src', $("#remoteCapture").attr('src') + "?t=" + Date.now());
        
        // Restart audio WebSocket with current quality (if not disabled)
        if (!audioFrontendStarted && currentAudioQuality !== 'disabled') {
            startAudioWebSocket(currentAudioQuality);
            audioFrontendStarted = true;
        }
        
        console.log('Session resumed - all streams reconnected');
    }, 500);
}

function disconnect() {
    disconnectRemote();
    window.location.href = "no_session.html";
}

