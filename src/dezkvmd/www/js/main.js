/*
    DezKVM main.js — shell logic

    Top bar (device selector, session controls), landing instance grid,
    right side panel, and the floating Terminal / ISO-library windows.
    The active KVM session itself runs inside the #sessionContext iframe
    (viewport.html); vpCall() bridges shell buttons to functions in it.

    Shared UI kit: js/dez-ui.js + css/dez-ui.css (DezWindow, $.toast, ...).
*/

let activeSessionUuid = null;
let sessionStartTime = null;
let sessionTimerHandle = null;
let knownInstances = [];
let terminalCounter = 0;

/*
    Tool registry (shell side).

    Viewport tools are opened inside the session iframe via dezOpenTool();
    shell tools open floating windows at the shell level. The same ids are
    used by the viewport toolbox (js/toolbox.js) and the visibility
    preference (localStorage key dezkvm.tools.visible, managed from the
    settings overlay "Tools" tab).
*/
const SHELL_TOOL_REGISTRY = [
    { id: 'screenshot',    name: 'Screenshot',    sub: 'Capture and download frame', icon: 'img/icons/screenshot.svg',    scope: 'viewport' },
    { id: 'ocr-copy',      name: 'OCR Copy',      sub: 'Copy text from the screen',  icon: 'img/icons/ocr.svg',           scope: 'viewport' },
    { id: 'record',        name: 'Record',        sub: 'Record the remote display',  icon: 'img/icons/record.svg',        scope: 'viewport' },
    { id: 'clipboard',     name: 'Clipboard',     sub: 'Paste text to remote',       icon: 'img/icons/clipboard.svg',     scope: 'viewport' },
    { id: 'file-transfer', name: 'File Transfer', sub: 'Browse the USB drive',       icon: 'img/icons/file-transfer.svg', scope: 'viewport' },
    { id: 'virtual-usb',   name: 'Virtual USB',   sub: 'Switch mass storage side',   icon: 'img/icons/usb.svg',           scope: 'viewport' },
    { id: 'power',         name: 'Power Control', sub: 'Control remote power',       icon: 'img/icons/power.svg',         scope: 'viewport' },
    { id: 'system-info',   name: 'System Info',   sub: 'View device details',        icon: 'img/icons/info.svg',          scope: 'viewport' },
    { id: 'terminal',      name: 'Terminal',      sub: 'Open an SSH session',        icon: 'img/icons/terminal.svg',      scope: 'shell' },
    { id: 'iso-library',   name: 'ISO Library',   sub: 'Manage bootable images',     icon: 'img/icons/disc.svg',          scope: 'shell' }
];


function visibleToolIds() {
    try {
        const stored = JSON.parse(localStorage.getItem('dezkvm.tools.visible'));
        if (Array.isArray(stored)) return stored;
    } catch (e) { /* fall through */ }
    return SHELL_TOOL_REGISTRY.map(t => t.id);
}

/* ------------------------------------------------------------ helpers */

function csrfToken() {
    const meta = document.querySelector('meta[name="dezkvm.csrf.token"]');
    return meta ? meta.getAttribute('content') : '';
}

// Call a function inside the session viewport iframe, when available
function vpCall(fnName, ...args) {
    const frame = document.getElementById('sessionContext');
    if (!activeSessionUuid || !frame.contentWindow) {
        $.toast({ message: '<img src="img/icons/display.svg" class="dez-icon"> Connect to a device first', duration: 3000 });
        return;
    }
    const fn = frame.contentWindow[fnName];
    if (typeof fn === 'function') {
        try { return fn(...args); } catch (e) { console.error('vpCall ' + fnName + ' failed:', e); }
    } else {
        console.warn('viewport function not available:', fnName);
    }
}

function shortUuid(uuid) {
    return uuid ? uuid.substring(0, 8) : '';
}

function instanceDisplayName(inst) {
    return 'KVM Port ' + shortUuid(inst.uuid);
}

/* ------------------------------------------------------- initial load */

$(document).ready(function () {
    listInstances();
    buildSidePanelLists();
    updateThemeIcon();
    updateFitIcon();

    // Refresh the landing grid periodically while it is visible
    setInterval(function () {
        if (!activeSessionUuid) listInstances();
    }, 30000);

    // Session resume via URL hash
    if (window.location.hash) {
        try {
            const hashData = JSON.parse(decodeURIComponent(window.location.hash.substring(1)));
            if (hashData.type === 'instance' && hashData.sessionId) {
                $.toast({
                    message: 'Resume previous session?',
                    duration: 5000,
                    actions: [
                        { text: 'Yes', class: 'mini green', click: function () { startSession(hashData.sessionId); } },
                        { text: 'No', class: 'mini basic', click: function () { window.location.hash = ''; } }
                    ]
                });
            }
        } catch (e) {
            console.error('Failed to parse URL hash:', e);
        }
    }
});

/* -------------------------------------------------- instances / landing */

function renderInstanceCard(inst) {
    const card = $(`
        <div class="kvm-instance">
            <div class="screenshot">
                <img src="/api/v1/screenshot/${inst.uuid}#${Date.now()}" alt="Screenshot">
            </div>
            <div class="instance-info">
                <span class="status-dot on"></span>
                <span class="inst-text">
                    <b>${instanceDisplayName(inst)}</b>
                    <small>${inst.video_capture_dev || ''} · ${inst.video_resolution_width}×${inst.video_resolution_height}@${inst.video_framerate}</small>
                </span>
                <button class="ui small primary button">Connect</button>
            </div>
        </div>
    `);
    card.find('button').on('click', function (e) {
        e.stopPropagation();
        startSession(inst.uuid);
    });
    card.on('click', function () { startSession(inst.uuid); });
    return card;
}

function listInstances(callback) {
    $.get('/api/v1/instances', function (data) {
        let instances = [];
        try {
            instances = typeof data === 'string' ? JSON.parse(data) : data;
        } catch (e) { instances = []; }
        instances.sort((a, b) => a.uuid.localeCompare(b.uuid));
        knownInstances = instances;

        const $list = $('#instanceList');
        $list.empty();
        if (instances.length === 0) {
            $list.append('<div class="ui message">No KVM ports detected. Check the USB connections and restart dezkvmd.</div>');
        } else {
            instances.forEach(function (inst) { $list.append(renderInstanceCard(inst)); });
        }

        renderDevicePanel();
        if (callback) callback();
    });
}

/* --------------------------------------------------- device dropdown */

function renderDevicePanel() {
    const $list = $('#devicePanelList');
    $list.empty();
    if (knownInstances.length === 0) {
        $list.append('<div class="ui message" style="margin:4px;">No devices found.</div>');
        return;
    }
    knownInstances.forEach(function (inst) {
        const entry = $(`
            <button class="device-entry ${inst.uuid === activeSessionUuid ? 'active' : ''}">
                <span class="dev-thumb">
                    <img src="/api/v1/screenshot/${inst.uuid}#${Date.now()}" alt="">
                    <img src="img/icons/display.svg" class="dez-icon thumb-fallback" alt="">
                </span>
                <span class="dev-text">
                    <b>${instanceDisplayName(inst)}</b>
                    <small>${inst.video_capture_dev || ''}</small>
                </span>
                <span class="status-dot on"></span>
            </button>
        `);
        // Fall back to the generic display icon when the preview is
        // unavailable (instance just started, capture device busy, ...)
        entry.find('.dev-thumb > img').first().on('error', function () {
            $(this).closest('.dev-thumb').addClass('no-preview');
        });
        entry.on('click', function () {
            hideDevicePanel();
            if (inst.uuid !== activeSessionUuid) startSession(inst.uuid);
        });
        $list.append(entry);
    });
}

function toggleDevicePanel() {
    const panel = document.getElementById('devicePanel');
    if (panel.style.display === 'none') {
        listInstances(function () { panel.style.display = ''; });
    } else {
        panel.style.display = 'none';
    }
}

function hideDevicePanel() {
    document.getElementById('devicePanel').style.display = 'none';
}

// Close popovers when clicking elsewhere
document.addEventListener('mousedown', function (e) {
    if (!e.target.closest('.tb-device')) hideDevicePanel();
    if (!e.target.closest('#sidePanel') && !e.target.closest('.tb-btn')) hideSidePanel();
});

/* ---------------------------------------------------- session control */

function startSession(uuid) {
    const frame = document.getElementById('sessionContext');
    frame.src = `/viewport.html?ts=${Date.now()}#${uuid}`;
    frame.style.display = '';
    $('#landing').hide();

    activeSessionUuid = uuid;
    document.body.classList.add('session-active');
    window.location.hash = encodeURIComponent(JSON.stringify({ type: 'instance', sessionId: uuid }));

    // Top bar device selector
    const inst = knownInstances.find(i => i.uuid === uuid);
    $('#devSelName').text(inst ? instanceDisplayName(inst) : ('KVM Port ' + shortUuid(uuid)));
    $('#devSelSub').text(inst ? (inst.video_capture_dev || '') : '');
    $('#devSelDot').addClass('on');

    // Side panel connection card
    $('#spConnection').show();
    $('#spSessionUuid').text(uuid);
    sessionStartTime = Date.now();
    if (sessionTimerHandle) clearInterval(sessionTimerHandle);
    sessionTimerHandle = setInterval(updateSessionTimer, 1000);
    updateSessionTimer();
    buildSidePanelLists();

    // Start audio streaming automatically once the viewport is ready.
    // (The viewport also self-starts audio; the double start is guarded.
    // Triggering from here keeps the start inside the user-activation
    // window of the click that opened the session.)
    let audioRetryCount = 0;
    const maxAudioRetries = 12;
    function tryStartAudio() {
        const cw = frame.contentWindow;
        if (cw && typeof cw.startAudioWebSocket === 'function') {
            let quality = localStorage.getItem('audioQuality');
            if (!quality) {
                quality = 'standard';
                localStorage.setItem('audioQuality', quality);
            }
            if (quality !== 'disabled') cw.startAudioWebSocket(quality);
        } else if (audioRetryCount < maxAudioRetries) {
            audioRetryCount++;
            setTimeout(tryStartAudio, 300);
        } else {
            console.warn('Viewport did not expose startAudioWebSocket in time');
            $.toast({
                message: '<i class="yellow volume off icon"></i> Remote audio did not start — click inside the session to retry',
                duration: 5000
            });
        }
    }
    setTimeout(function () {
        frame.contentWindow.focus();
        tryStartAudio();
        updateFitIcon();
    }, 400);
}

function disconnectSession() {
    const frame = document.getElementById('sessionContext');
    try {
        if (frame.contentWindow && typeof frame.contentWindow.disconnectRemote === 'function') {
            frame.contentWindow.disconnectRemote();
        }
    } catch (e) { /* iframe may already be gone */ }
    frame.src = 'no_session.html';
    frame.style.display = 'none';

    activeSessionUuid = null;
    document.body.classList.remove('session-active');
    window.location.hash = '';
    if (sessionTimerHandle) { clearInterval(sessionTimerHandle); sessionTimerHandle = null; }

    $('#devSelName').text('No device connected');
    $('#devSelSub').text('Select a KVM port to connect');
    $('#devSelDot').removeClass('on');
    $('#spConnection').hide();
    hideSidePanel();

    $('#landing').show();
    listInstances();
    buildSidePanelLists();
}

function updateSessionTimer() {
    if (!sessionStartTime) return;
    const s = Math.floor((Date.now() - sessionStartTime) / 1000);
    const hh = String(Math.floor(s / 3600)).padStart(2, '0');
    const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0');
    const ss = String(s % 60).padStart(2, '0');
    $('#spSessionTime').text(`${hh}:${mm}:${ss}`);
}

/* -------------------------------------------------------- side panel */

function toggleSidePanel() {
    const panel = document.getElementById('sidePanel');
    if (panel.style.display === 'none') {
        buildSidePanelLists();
        panel.style.display = '';
    } else {
        panel.style.display = 'none';
    }
}

function hideSidePanel() {
    document.getElementById('sidePanel').style.display = 'none';
}

function buildSidePanelLists() {
    const visible = visibleToolIds();
    const $tools = $('#spToolList');
    $tools.empty();
    SHELL_TOOL_REGISTRY.forEach(function (tool) {
        if (!visible.includes(tool.id)) return;
        const needsSession = tool.scope === 'viewport';
        const item = $(`
            <button class="sp-item ${needsSession && !activeSessionUuid ? 'disabled' : ''}">
                <img src="${tool.icon}" class="dez-icon" alt="">
                <span class="sp-item-text"><b>${tool.name}</b><small>${tool.sub}</small></span>
            </button>
        `);
        item.on('click', function () {
            hideSidePanel();
            openTool(tool.id);
        });
        $tools.append(item);
    });

    const $settings = $('#spSettingsList');
    $settings.empty();
    const settingsItem = $(`
        <button class="sp-item ${!activeSessionUuid ? 'disabled' : ''}">
            <img src="img/icons/settings.svg" class="dez-icon" alt="">
            <span class="sp-item-text"><b>Settings</b><small>Session preferences and device options</small></span>
        </button>
    `);
    settingsItem.on('click', function () {
        hideSidePanel();
        vpCall('openSettingsOverlay');
    });
    $settings.append(settingsItem);
}

function openTool(toolId) {
    if (toolId === 'terminal') { openTerminalWindow(); return; }
    if (toolId === 'iso-library') { toggleIsoLibraryWindow(); return; }
    vpCall('dezOpenTool', toolId);
}

/* ------------------------------------------------- dark theme toggle */

function updateThemeIcon() {
    const icon = document.getElementById('tbThemeIcon');
    if (!icon) return;
    const dark = localStorage.getItem('dezkvm.theme') === 'dark';
    icon.src = dark ? 'img/icons/sun.svg' : 'img/icons/moon.svg';
    icon.closest('button').title = dark ? 'Switch to light theme' : 'Switch to dark theme';
}

function toggleDezTheme() {
    const dark = localStorage.getItem('dezkvm.theme') === 'dark';
    localStorage.setItem('dezkvm.theme', dark ? 'light' : 'dark');
    // dezApplyTheme (dez-ui.js) applies locally and recurses into iframes
    if (typeof dezApplyTheme === 'function') dezApplyTheme();
    updateThemeIcon();
}

/* ------------------------------------------- fit-to-window toggle */

// The scale-to-fit state is owned by the viewport and persisted in
// localStorage('scaleToFit') (shared origin), so the shell can mirror it.
function updateFitIcon() {
    const icon = document.getElementById('tbFitIcon');
    if (!icon) return;
    const fitOn = localStorage.getItem('scaleToFit') === 'true';
    // Fit enabled → "-> <-" (compress); disabled → "<- ->" (expand)
    icon.src = fitOn ? 'img/icons/compress.svg' : 'img/icons/expand.svg';
    icon.closest('button').title = fitOn ? 'Show actual resolution' : 'Fit to window';
}

function toggleFitFromShell() {
    vpCall('externalToggleScaleToFit');
    // externalToggleScaleToFit applies the change after a 100ms delay
    setTimeout(updateFitIcon, 250);
}

/* ----------------------------------------------- ATX quick actions */

function quickAtx(action, btn) {
    if (!activeSessionUuid) return;

    // Two-step arm confirmation
    if (!btn.dataset.armed) {
        btn.dataset.armed = '1';
        btn.dataset.origHtml = btn.innerHTML;
        btn.classList.add('armed');
        btn.innerHTML = 'Confirm?';
        setTimeout(function () {
            delete btn.dataset.armed;
            btn.classList.remove('armed');
            if (btn.dataset.origHtml) btn.innerHTML = btn.dataset.origHtml;
        }, 4000);
        return;
    }
    delete btn.dataset.armed;
    btn.classList.remove('armed');
    btn.innerHTML = btn.dataset.origHtml;

    $.ajax({
        url: '/api/v1/atx/' + activeSessionUuid + '/trigger',
        method: 'POST',
        contentType: 'application/json',
        headers: { 'dezkvm_csrf_token': csrfToken() },
        data: JSON.stringify({ action: action }),
        success: function () {
            $.toast({ message: '<i class="green check icon"></i> ' + (action === 'reset_click' ? 'Reset button pressed' : 'Power button pressed'), duration: 3500 });
        },
        error: function (xhr) {
            $.toast({ class: 'error', message: '<i class="red times icon"></i> ATX action failed: ' + (xhr.responseText || ''), duration: 5000 });
        }
    });
}

/* --------------------------------------------------- terminal windows */

// Opens the Terminal Manager window (connection form + saved sessions).
// Actual terminal sessions run in their own windows via launchTerminalSession.
function openTerminalWindow() {
    DezWindow({
        id: 'terminal-manager',
        title: 'Terminal',
        icon: 'img/icons/terminal.svg',
        width: 700,
        ajax: 'tools/terminal-manager.html'
    });
}

// Open a terminal session window auto-connecting to the given target,
// and remember the target in the saved-sessions list. Windows are keyed by
// target: re-opening the same saved session restores the existing (possibly
// hidden) window instead of spawning a second SSH connection.
function launchTerminalSession(server, port, username) {
    const target = username + '@' + server + ':' + port;
    const winId = 'terminal-' + target;

    if (DezWindow.get(winId)) {
        DezWindow({ id: winId }); // un-hides + focuses the existing session
        return;
    }

    const payload = encodeURIComponent(JSON.stringify({
        server: server,
        port: port,
        username: username
    }));
    DezWindow({
        id: winId,
        title: target,
        icon: 'img/icons/terminal.svg',
        width: 780,
        height: 500,
        iframe: 'terminal.html#' + payload,
        resizable: true,
        minimizable: true,
        confirmClose: 'Closing this window will terminate the SSH session. Use the hide (—) button to keep it running in the background.'
    });
    saveTerminalSession(server, port, username);
}

// Persist a session target (deduplicated, most recent first, capped at 20).
function saveTerminalSession(server, port, username) {
    let sessions = [];
    try {
        const stored = JSON.parse(localStorage.getItem('dezkvm.terminal.sessions'));
        if (Array.isArray(stored)) sessions = stored;
    } catch (e) { /* start fresh */ }
    sessions = sessions.filter(function (s) {
        return !(s.server === server && s.port === port && s.username === username);
    });
    sessions.unshift({ name: username + '@' + server, server: server, port: port, username: username });
    if (sessions.length > 20) sessions = sessions.slice(0, 20);
    localStorage.setItem('dezkvm.terminal.sessions', JSON.stringify(sessions));
}

// Terminal iframes post connection details once the SSH session is set up
window.addEventListener('message', function (event) {
    if (event.data && event.data.type === 'terminalConnected') {
        // Update the title of the window whose iframe sent this message
        document.querySelectorAll('.dez-window').forEach(function (winEl) {
            const frame = winEl.querySelector('iframe');
            if (frame && frame.contentWindow === event.source) {
                const titleEl = winEl.querySelector('.dez-window-title');
                if (titleEl) {
                    titleEl.textContent = event.data.username + '@' + event.data.server + ':' + event.data.port;
                }
            }
        });
    }
});

/* ------------------------------------------------- ISO library window */

function toggleIsoLibraryWindow() {
    const existing = DezWindow.get('iso-library');
    if (existing) { existing.close(); return; }
    DezWindow({
        id: 'iso-library',
        title: 'ISO / Image Library',
        icon: 'img/icons/disc.svg',
        width: 640,
        height: 520,
        iframe: 'iso-manager.html',
        resizable: true
    });
}

/* ------------------------------------------------------------ logout */

function logout() {
    $.ajax({
        url: '/api/v1/logout',
        method: 'POST',
        success: function () { window.location.href = '/login.html'; },
        error: function () {
            $.toast({ class: 'error', message: 'Logout failed. Please try again.' });
        }
    });
}

/* -------------------------------------------------- keyboard routing */

// Refocus the session viewport on stray keydowns, but never steal focus
// from floating windows (terminal / ISO library) or open panels.
$(document).on('keydown', function () {
    if (!activeSessionUuid) return;
    if (window.DezWindow && DezWindow.anyOpen()) return;
    if (document.getElementById('sidePanel').style.display !== 'none') return;
    if (document.getElementById('devicePanel').style.display !== 'none') return;
    const frame = document.getElementById('sessionContext');
    if (frame.contentWindow) frame.contentWindow.focus();
});

$(window).on('focus', function () {
    if (activeSessionUuid && !(window.DezWindow && DezWindow.anyOpen())) {
        const frame = document.getElementById('sessionContext');
        if (frame.contentWindow) frame.contentWindow.focus();
    }
});
