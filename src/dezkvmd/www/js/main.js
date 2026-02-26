/*
    DezKVM main.js

    This file contains the main JavaScript logic for the DezKVM web interface
    For viewport logics (KVM video and audio streaming), see viewport.js
*/
let currentTab = 'instances';
let currentList = null;
let terminals = {}; // Store active terminal sessions
let terminalCounter = 0; // Counter for unique terminal IDs
let activeTerminalId = null; // Currently active terminal

$(document).ready(function() {
    listInstances();
});

function hideAllViewports() {
    $(".viewport").hide();   
}

function hideAllLists(){
    $(".viewlist").hide();
}

/*
    View List Management
*/

function hideTerminalList() {
    $('#terminalsTab').hide();
    $('.sidebar .menu-options .item[menu="terminal"]').removeClass('active');
    if (currentTab === 'terminal' && activeTerminalId) {
        $(`#${activeTerminalId}`).focus();
    }
}

function toggleTerminalList() {
    if ($('#terminalsTab').is(':visible')) {
        hideTerminalList();
    } else {
        hideInstanceList();
        listTerminals();
        $('#terminalsTab').show();
    }
}

function showInstanceList(){
    listInstances();
    $('#instancesTab').show();
}

function hideInstanceList() {
    $('#instancesTab').hide();
    $('.sidebar .menu-options .item[menu="instances"]').removeClass('active');
    if (currentTab === 'session') {
        $('#sessionContext').focus();
    }
}

function toggleInstanceList(){
    if ($('#instancesTab').is(':visible')) {
        hideInstanceList();
    } else {
        showInstanceList();
    }
}

function toggleLists(listType) {
    if (listType != currentList) {
        hideAllLists();
    }
    if (listType === 'instances') {
        toggleInstanceList();
    } else if (listType === 'terminal') {
        toggleTerminalList();
    } else{
        return;
    }

    currentList = listType;

    $('.sidebar .menu-options .item').removeClass('active');
    $(`.sidebar .menu-options .item[menu="${listType}"]`).addClass('active');
}

/*
    Viewport Management
*/
function switchViewport(viewport) {
    hideAllViewports();
    if (viewport === 'session') {
        hideAllLists();
        $('#session').show();
        $('#sessionContext').focus();
        currentTab = viewport;
    } else if (viewport == "terminal"){
        $('#terminal').show();
        currentTab = viewport;
    } else if (viewport === 'files') {
        // Handle file management tab activation here
    } else if (viewport === 'power') {
        // Handle power control tab activation here
    }
    
    $('.sidebar .menu-options .item').removeClass('active');
    $(`.sidebar .menu-options .item[menu="${viewport}"]`).addClass('active');
}

/*
    Session & Viewport Management
*/

// Make sure session iframe regains focus when the window is focused or when the iframe content is loaded
$(window).on('focus', function() {
    if (currentTab === 'session') {
        $('#sessionContext').focus();
    }
});

$('#sessionContext').on('load', function() {
    this.contentWindow.focus();
});

function connectToSession(sessionId, callback=undefined) {
    $('#sessionContext').attr('src', `/viewport.html?ts=${Date.now()}#${sessionId}`);
    if (callback) callback();
}

function startSession(sessionId) {
    connectToSession(sessionId, function() {
        // Switch to session viewport
        switchViewport('session');

        // Hide instance list if it's open
        hideInstanceList();

        // Start audio streaming automatically with retries
        let audioRetryCount = 0;
        const maxAudioRetries = 5;
        const audioRetryInterval = 200;

        function tryStartAudio() {
            if ($('#sessionContext')[0].contentWindow.startAudioWebSocket) {
                let currentAudioQuality = localStorage.getItem('audioQuality');
                if (!currentAudioQuality) {
                    currentAudioQuality = 'standard'; // Default quality
                    localStorage.setItem('audioQuality', currentAudioQuality);
                }else if (currentAudioQuality == 'disabled') {
                    return; // Audio is disabled, do not start
                }
                
                $('#sessionContext')[0].contentWindow.startAudioWebSocket(currentAudioQuality);
                console.log('Audio streaming started successfully');
            } else if (audioRetryCount < maxAudioRetries) {
                audioRetryCount++;
                console.log(`Retrying audio start (attempt ${audioRetryCount}/${maxAudioRetries})`);
                setTimeout(tryStartAudio, audioRetryInterval);
            } else {
                console.log('Failed to start audio streaming after maximum retries');
                $.toast({
                    class: 'error',
                    message: 'Failed to connect to remote Audio Device.',
                });
            }
        }

        setTimeout(function(){
            $('#sessionContext')[0].contentWindow.focus();
            tryStartAudio();
        }, 300);
    });
}

/* 
    Instances List
*/

function renderInstance(instance) {
    let metadata = encodeURIComponent(JSON.stringify(instance));
    return `
        <div class="kvm-instance" data-metadata="${metadata}">
            <div class="instance-body">
                <div class="screenshot">
                    <img src="/api/v1/screenshot/${instance.uuid}#${Date.now()}" alt="Screenshot for ${instance.uuid}">
                </div>
            </div>
            <div class="instance-overlay">
                <div class="ui small circular basic label">
                    ${instance.uuid}
                </div>
                <h3 class="ui header">
                    <span>${instance.video_capture_dev}</span>
                    <div class="sub header">${instance.stream_info}</div>
                </h3>
                <div class="instance-actions">
                    <button class="ui small circular secondary button launch-btn" onclick="startSession('${instance.uuid}')">Connect</button>
                    <button class="ui small circular button details-btn" onclick="showInstanceDetails('${instance.uuid}')">Details</button>
                </div>
                
            </div>
        </div>
    `;
}

function listInstances(callback=undefined) {
    $.get('/api/v1/instances', function(data) {
        let instances = [];
        try {
            instances = typeof data === 'string' ? JSON.parse(data) : data;
        } catch (e) {
            instances = [];
        }
        instances.sort((a, b) => a.uuid.localeCompare(b.uuid));
        const $list = $('#instanceList');
        $list.empty();
        if (instances.length === 0) {
            $list.append('<div class="ui message">No instances found.</div>');
            return;
        }
        instances.forEach(function(instance) {
            $list.append(renderInstance(instance));
        });
        if (callback) callback();
    });
}

$(document).on('keydown', function(e) {
    if (currentTab === 'session') {
        $('#sessionContext')[0].contentWindow.focus();
    } else if (currentTab === 'terminal' && activeTerminalId) {
        $(`#${activeTerminalId}`)[0].contentWindow.focus();
    }
});

/*
    Terminal Management
*/

function renderTerminal(terminalData) {
    let metadata = encodeURIComponent(JSON.stringify(terminalData));
    return `
        <div class="terminal-entry" data-terminal-id="${terminalData.id}" data-metadata="${metadata}">
            <div class="terminal-info">
                <div class="ui small circular basic label">
                    <i class="terminal icon"></i> Terminal ${terminalData.displayId}
                </div>
                <div class="terminal-details">
                    <strong>${terminalData.username}@${terminalData.server}:${terminalData.port}</strong>
                </div>
            </div>
            <div class="terminal-actions">
                <button class="ui small inverted basic circular icon button" onclick="closeTerminal('${terminalData.id}'); event.stopPropagation();" title="Close Terminal">
                    <i class="times icon"></i>
                </button>
            </div>
        </div>
    `;
}

function listTerminals() {
    const $list = $('#terminalList');
    $list.empty();
    
    const terminalArray = Object.values(terminals);
    if (terminalArray.length === 0) {
        $('#noTerminalsMessage').show();
        return;
    } else {
        $('#noTerminalsMessage').hide();
    }
    
    terminalArray.forEach(function(terminal) {
        const $terminalEntry = $(renderTerminal(terminal));
        $terminalEntry.on('click', function() {
            switchTerminal(terminal.id);
        });
        $list.append($terminalEntry);
    });
}

function createNewTerminal() {
    terminalCounter++;
    const terminalId = `terminal_${terminalCounter}`;
    
    // Create terminal data object
    const terminalData = {
        id: terminalId,
        displayId: terminalCounter,
        server: '',
        port: 22,
        username: '',
        sessionUrl: null,
        created: Date.now()
    };
    
    // Store terminal data
    terminals[terminalId] = terminalData;
    
    // Create iframe for the terminal
    const $iframe = $('<iframe>', {
        id: terminalId,
        class: 'terminal-iframe',
        src: 'terminal.html',
        style: 'display: none; border: none; width: 100%; height: 100%;'
    });
    
    // Add iframe to terminal viewport
    $('#terminal').append($iframe);
    
    // Set up message listener for terminal connection details
    window.addEventListener('message', function terminalMessageHandler(event) {
        if (event.data && event.data.type === 'terminalConnected' && event.data.terminalId === terminalId) {
            // Update terminal data with connection details
            if (terminals[terminalId]) {
                terminals[terminalId].server = event.data.server;
                terminals[terminalId].port = event.data.port;
                terminals[terminalId].username = event.data.username;
                terminals[terminalId].sessionUrl = event.data.sessionUrl;
                
                // Refresh terminal list if it's visible
                if ($('#terminalsTab').is(':visible')) {
                    listTerminals();
                }
            }
        }
    });
    
    // Switch to the new terminal
    switchTerminal(terminalId);
    
    // Hide terminal list
    hideTerminalList();
}

function switchTerminal(terminalId) {
    if (!terminals[terminalId]) {
        console.error('Terminal not found:', terminalId);
        return;
    }
    
    // Show the terminal UI viewport
    switchViewport('terminal');

    // Hide all terminal iframes
    $('.terminal-iframe').hide();
    
    // Show the selected terminal
    $(`#${terminalId}`).show();
    activeTerminalId = terminalId;
    
    // Switch to terminal viewport if not already there
    if (currentTab !== 'terminal') {
        switchViewport('terminal');
    } else {
        hideTerminalList();
        $('#terminal').show();
    }
    
    // Focus the terminal iframe
    setTimeout(function() {
        $(`#${terminalId}`).focus();
    }, 100);
}

function closeTerminal(terminalId) {
    if (!terminals[terminalId]) {
        console.error('Terminal not found:', terminalId);
        return;
    }
    
    // Remove the iframe
    $(`#${terminalId}`).remove();
    
    // Remove from terminals object
    delete terminals[terminalId];
    
    // If this was the active terminal, switch to another or show terminal list
    if (activeTerminalId === terminalId) {
        activeTerminalId = null;
        
        // Try to switch to another terminal
        const remainingTerminals = Object.keys(terminals);
        if (remainingTerminals.length > 0) {
            switchTerminal(remainingTerminals[0]);
        } else {
            // No terminals left, show terminal list
            toggleTerminalList();
        }
    }
    
    // Refresh terminal list
    listTerminals();
}

function logout() {
    $.ajax({
        url: '/api/v1/logout',
        method: 'POST',
        success: function() {
            window.location.href = '/login.html';
        },
        error: function() {
            $.toast({
                class: 'error',
                message: 'Logout failed. Please try again.'
            });
        }
    });
}

/*
    Instance Details Modal
*/

function showInstanceDetails(instanceUuid) {
    // Find the instance data from the DOM
    let instanceData = null;
    $('.kvm-instance').each(function() {
        let metadata = $(this).attr('data-metadata');
        metadata = metadata ? JSON.parse(decodeURIComponent(metadata)) : null;
        if (metadata && metadata.uuid === instanceUuid) {
            instanceData = metadata;
            return false; // break the loop
        }
    });

    console.log(instanceData);

    if (!instanceData) {
        $.toast({
            class: 'error',
            message: 'Instance not found.'
        });
        return;
    }

    // Populate modal with instance data
    $('#detail-uuid').text(instanceData.uuid);
    $('#detail-video-dev').text(instanceData.video_capture_dev || 'N/A');
    $('#detail-resolution').text(
        `${instanceData.video_resolution_width}×${instanceData.video_resolution_height} pixels`
    );
    $('#detail-framerate').text(`${instanceData.video_framerate} fps`);
    $('#detail-audio-dev').text(instanceData.audio_capture_dev || 'N/A');
    $('#detail-audio-config').text(
        `${instanceData.audio_channels} channels @ ${instanceData.audio_sample_rate} Hz`
    );
    $('#detail-aux-mcu').text(instanceData.aux_mcu_device || 'N/A');
    $('#detail-usb-kvm').text(instanceData.usb_kvm_device || 'N/A');
    $('#detail-mass-storage').text(instanceData.usb_mass_storage_side || 'N/A');
    $('#detail-stream-info').text(instanceData.stream_info || 'N/A');

    // Set up the connect button
    $('#connectFromModal').off('click').on('click', function() {
        $('#instanceDetailsModal').modal('hide');
        startSession(instanceUuid);
    });

    // Show the modal
    $('#instanceDetailsModal').modal('show');
}