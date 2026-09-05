/*
    toolbox.js — floating Tools box for the session viewport.

    Renders the draggable Tools shortcut grid (DezWindow) and opens the
    individual tool panels, which are ajax-loaded from /tools/<id>.html
    into their own floating windows. Which tools are shown is a user
    preference stored in localStorage (dezkvm.tools.visible), editable from
    the settings overlay "Tools" tab.

    Runs inside viewport.html; requires js/dez-ui.js and the viewport globals
    (kvmDeviceUUID, showPasteBox, showScreenshotSelector, ...).
*/

/*
    Tool registry. kind:
      'ajax'   – load /tools/<id>.html into a DezWindow
      'custom' – call open() (existing floating UIs / actions)
*/
const DEZ_TOOLS = [
    { id: 'screenshot',    name: 'Screenshot',    icon: '/img/icons/screenshot.svg',    kind: 'ajax', width: 480 },
    { id: 'clipboard',     name: 'Clipboard',     icon: '/img/icons/clipboard.svg',     kind: 'custom', open: function () { if (typeof showPasteBox === 'function') showPasteBox(); } },
    { id: 'file-transfer', name: 'File Transfer', icon: '/img/icons/file-transfer.svg', kind: 'custom', open: openFileTransferWindow },
    { id: 'virtual-usb',   name: 'Virtual USB',   icon: '/img/icons/usb.svg',           kind: 'ajax', width: 430 },
    { id: 'power',         name: 'Power',         icon: '/img/icons/power.svg',         kind: 'ajax', width: 440 },
    { id: 'system-info',   name: 'System Info',   icon: '/img/icons/info.svg',          kind: 'ajax', width: 480 },
    { id: 'ocr-copy',      name: 'OCR Copy',      icon: '/img/icons/ocr.svg',           kind: 'ajax', width: 430 },
    { id: 'record',        name: 'Record',        icon: '/img/icons/record.svg',        kind: 'ajax', width: 440,
      onClose: function () { if (typeof recOnWindowClosed === 'function') recOnWindowClosed(); } },
    { id: 'settings',      name: 'Settings',      icon: '/img/icons/settings.svg',      kind: 'custom',
      open: function () {
          const toolbox = DezWindow.get('dez-toolbox');
          if (toolbox) toolbox.close();
          if (typeof openSettingsOverlay === 'function') openSettingsOverlay();
      } }
];

function dezVisibleToolIds() {
    try {
        const stored = JSON.parse(localStorage.getItem('dezkvm.tools.visible'));
        if (Array.isArray(stored)) return stored;
    } catch (e) { /* default below */ }
    // Default: everything (shell-level tools are handled by the shell)
    return DEZ_TOOLS.map(t => t.id).concat(['terminal', 'iso-library']);
}

/* --------------------------------------------------------- toolbox box */

function dezToolboxToggle() {
    const existing = DezWindow.get('dez-toolbox');
    if (existing) { existing.close(); return; }

    const visible = dezVisibleToolIds();
    const grid = document.createElement('div');
    grid.className = 'dez-toolbox-grid';

    let shown = 0;
    DEZ_TOOLS.forEach(function (tool) {
        if (!visible.includes(tool.id)) return;
        shown++;
        const item = document.createElement('button');
        item.className = 'dez-toolbox-item';
        item.innerHTML = '<img src="' + tool.icon + '" alt=""><span>' + tool.name + '</span>';
        item.addEventListener('click', function () { dezOpenTool(tool.id); });
        grid.appendChild(item);
    });
    if (shown === 0) {
        grid.innerHTML = '<div class="dez-toolbox-empty">All tools are hidden.<br>Enable them in Settings &rarr; Tools.</div>';
    }

    DezWindow({
        id: 'dez-toolbox',
        title: 'Tools',
        icon: '/img/icons/toolbox.svg',
        width: 420,
        content: grid,
        x: window.innerWidth - 460,
        y: Math.max(12, window.innerHeight - 340)
    });
}

/* ----------------------------------------------------------- open tool */

function dezOpenTool(toolId) {
    const tool = DEZ_TOOLS.find(t => t.id === toolId);
    if (!tool) {
        console.warn('Unknown tool:', toolId);
        return;
    }
    if (tool.kind === 'custom') {
        tool.open();
        return;
    }
    DezWindow({
        id: 'tool-' + tool.id,
        title: tool.name,
        icon: tool.icon,
        width: tool.width || 440,
        padded: true,
        ajax: '/tools/' + tool.id + '.html',
        onClose: tool.onClose
    });
}

/* --------------------------------------------------- file transfer tool */

// The file manager stays a standalone page (it manages its own big table
// and upload logic) and is hosted in a floating window iframe. The iframe
// keeps the id "fileManagerFrame" so the settings overlay format tool can
// keep notifying it via postMessage after a disk format.
function openFileTransferWindow() {
    if (typeof kvmDeviceUUID === 'undefined' || !kvmDeviceUUID) {
        $.toast({ message: 'Device UUID not available', duration: 3000 });
        return;
    }
    DezWindow({
        id: 'tool-file-transfer',
        title: 'File Transfer',
        icon: '/img/icons/file-transfer.svg',
        width: 980,
        height: 640,
        iframe: '/tools/file-transfer.html?uuid=' + encodeURIComponent(kvmDeviceUUID) + '#' + Date.now(),
        frameId: 'fileManagerFrame',
        resizable: true
    });
}
