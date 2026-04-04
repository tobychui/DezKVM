/*
    viewport.js

    This script handles various functionalities for the KVM viewport,
    including mass storage switching, audio quality management, resolution
    management, and UI interactions.
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

// Scale to fit setting
if (!localStorage.getItem('scaleToFit')) {
    localStorage.setItem('scaleToFit', 'false');
}
let isScaleToFit = localStorage.getItem('scaleToFit') === 'true';

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
    
    // Apply scale to fit setting if enabled
    if (isScaleToFit) {
        applyScaleToFit();
    }
    
    // Apply show local cursor preference
    if (typeof applyShowLocalCursorPreference === 'function') {
        applyShowLocalCursorPreference();
    }
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

    // Disable dropdowns and show loading state
    setResolutionDropdownLoading(true);

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
            // Automatically resume session
            autoResumeSession(width, height, fps);
        },
        error: function(xhr, status, error) {
            setResolutionDropdownLoading(false);
            $.toast({
                class: 'error',
                message: 'Error changing resolution: ' + (xhr.responseText || error),
                showProgress: 'bottom'
            });
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
        $("#" + streamingContainerId).attr('src', $("#" + streamingContainerId).attr('src') + "?t=" + Date.now());
        
        // Audio will be restarted when user clicks on the video (with current quality setting)
        console.log('Streams reconnected');
    }, 100);
}

/* Resolution Change Modal Functions */
function setResolutionDropdownLoading(loading) {
    var resSelect = document.getElementById('resolutionSelect');
    var settingsResSelect = document.getElementById('settingsResolutionSelect');
    var loadingIndicator = $('#resolutionLoadingIndicator');
    
    if (loading) {
        // Disable dropdowns
        if (resSelect) resSelect.disabled = true;
        if (settingsResSelect) settingsResSelect.disabled = true;
        
        // Show and animate loading indicator in settings panel
        if (loadingIndicator.length) {
            loadingIndicator.show();
            loadingIndicator.progress({
                percent: 100,
                indicating: true,
                autoSuccess: false
            });
            // Animate progress from 0 to ~90%
            loadingIndicator.progress('set percent', 0);
            var percent = 0;
            window.resolutionLoadingInterval = setInterval(function() {
                percent += Math.random() * 15;
                if (percent > 90) percent = 90;
                loadingIndicator.progress('set percent', percent);
            }, 200);
        }
    } else {
        // Enable dropdowns
        if (resSelect) resSelect.disabled = false;
        if (settingsResSelect) settingsResSelect.disabled = false;
        
        // Complete and hide loading indicator
        if (loadingIndicator.length) {
            if (window.resolutionLoadingInterval) {
                clearInterval(window.resolutionLoadingInterval);
                window.resolutionLoadingInterval = null;
            }
            loadingIndicator.progress('set percent', 100);
            setTimeout(function() {
                loadingIndicator.hide();
            }, 300);
        }
    }
}

function autoResumeSession(width, height, fps) {
    console.log('Auto-resuming session...');
    
    // Wait a moment for cleanup, then restart everything
    setTimeout(function() {
        // Reload the video stream
        setStreamingSource(kvmDeviceUUID);
        
        // Re-attach event listeners before starting HID WebSocket
        attachHidEventListeners();
        
        // Restart HID WebSocket
        startHidWebSocket();

        // Change the img src to force reload
        $("#remoteCapture").attr('src', $("#remoteCapture").attr('src') + "?t=" + Date.now());
        
        // Restart audio WebSocket with current quality (if not disabled)
        if (!audioFrontendStarted && currentAudioQuality !== 'disabled') {
            startAudioWebSocket(currentAudioQuality);
            audioFrontendStarted = true;
        }
        
        // Re-enable dropdown and hide loading
        setResolutionDropdownLoading(false);
        
        // Show success toast
        $.toast({
            message: `<i class="green circle check icon"></i> Resolution changed to ${width}x${height} @ ${fps}fps`,
            showProgress: 'bottom',
            classProgress: 'green'
        });
        
        console.log('Session auto-resumed - all streams reconnected');
    }, 100);
}

function prepareStreamsReconnection() {
    console.log('Preparing streams for reconnection...');
    
    // Stop audio and HID websockets
    if (audioFrontendStarted) {
        stopAudioWebSocket();
        audioFrontendStarted = false;
    }
    disconnectRemote();
    
    // Detach event listeners before reconnection
    detachHidEventListeners();
}

function disconnect() {
    disconnectRemote();
    window.location.href = "no_session.html";
}

