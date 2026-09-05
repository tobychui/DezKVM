/*
    ocr-copy.js
    Provides OCR-based copy functionality in the remote desktop viewer.

    Based on tesseract.js for OCR processing.
    https://github.com/naptha/tesseract.js
*/

// Supported languages from Tesseract.js
// Reference: https://tesseract-ocr.github.io/tessdoc/Data-Files-in-different-versions.html
const SUPPORTED_LANGUAGES = [
    { code: 'eng', name: 'English' },
    { code: 'chi_sim', name: 'Chinese - Simplified' },
    { code: 'chi_tra', name: 'Chinese - Traditional' },
    { code: 'jpn', name: 'Japanese' },
    { code: 'kor', name: 'Korean' },
    { code: 'ara', name: 'Arabic' },
    { code: 'rus', name: 'Russian' },
    { code: 'fra', name: 'French' },
    { code: 'deu', name: 'German' },
    { code: 'spa', name: 'Spanish' },
    { code: 'por', name: 'Portuguese' },
    { code: 'ita', name: 'Italian' },
    { code: 'nld', name: 'Dutch' },
    { code: 'pol', name: 'Polish' },
    { code: 'tur', name: 'Turkish' },
    { code: 'vie', name: 'Vietnamese' },
    { code: 'tha', name: 'Thai' },
    { code: 'hin', name: 'Hindi' },
];

let ocrState = {
    isActive: false,
    startX: 0,
    startY: 0,
    isDragging: false,
    selectedLanguage: localStorage.getItem('dezkvm.ocr.lang') || 'eng'
};

/* Display name of the currently selected OCR language */
function ocrLanguageName(code) {
    const lang = SUPPORTED_LANGUAGES.find(l => l.code === code);
    return lang ? lang.name : code;
}

/**
 * Initialize and show the OCR region selector
 */
function showScreenshotSelector() {
    if (ocrState.isActive) {
        return; // Already active
    }

    const remoteCaptureEle = document.getElementById('remoteCapture');
    if (!remoteCaptureEle) {
        console.error('Remote capture element not found');
        return;
    }

    ocrState.isActive = true;
    pauseAllKeyEvents = true;

    // Create overlay
    const overlay = document.createElement('div');
    overlay.id = 'ocr-overlay';
    overlay.style.cssText = `
        position: fixed;
        top: 0;
        left: 0;
        width: 100vw;
        height: 100vh;
        background-color: rgba(0, 0, 0, 0.5);
        z-index: 9998;
        cursor: crosshair;
    `;

    // Create selection box
    const selectionBox = document.createElement('div');
    selectionBox.id = 'ocr-selection-box';
    selectionBox.style.cssText = `
        position: absolute;
        border: 2px solid #00b5ad;
        background-color: rgba(0, 181, 173, 0.1);
        display: none;
        pointer-events: none;
        z-index: 9999;
    `;

    // Create the confirm card (hidden until an area is selected). The
    // language is NOT selected here again — it comes from the OCR tool
    // panel and is remembered across sessions (dezkvm.ocr.lang).
    const controls = document.createElement('div');
    controls.id = 'ocr-controls';
    controls.style.cssText = `
        position: absolute;
        display: none;
        background: rgba(250, 250, 252, 0.97);
        backdrop-filter: blur(14px);
        padding: 14px 16px;
        border: 1px solid #e3e5e8;
        border-radius: 12px;
        box-shadow: 0 16px 50px rgba(0, 0, 0, 0.25);
        z-index: 10000;
        min-width: 250px;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
    `;

    const langInfo = document.createElement('div');
    langInfo.style.cssText = 'font-size: 0.85em; color: #86868b; margin-bottom: 10px;';
    langInfo.innerHTML = 'Recognize as <b style="color:#1c1c1e;">' +
        ocrLanguageName(ocrState.selectedLanguage) + '</b>' +
        '<br><span style="font-size:0.9em;">Change the language in the OCR Copy tool</span>';

    // Buttons container
    const buttonsDiv = document.createElement('div');
    buttonsDiv.style.cssText = 'display: flex; gap: 8px; justify-content: flex-end;';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'ui small basic button';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.onclick = cancelOCRSelection;

    const confirmBtn = document.createElement('button');
    confirmBtn.className = 'ui small primary button';
    confirmBtn.innerHTML = '<i class="check icon"></i> Confirm';
    confirmBtn.onclick = confirmOCRSelection;

    buttonsDiv.appendChild(cancelBtn);
    buttonsDiv.appendChild(confirmBtn);

    controls.appendChild(langInfo);
    controls.appendChild(buttonsDiv);

    // Append elements
    document.body.appendChild(overlay);
    document.body.appendChild(selectionBox);
    document.body.appendChild(controls);

    // Mouse event handlers
    overlay.addEventListener('mousedown', startSelection);
    overlay.addEventListener('mousemove', updateSelection);
    overlay.addEventListener('mouseup', endSelection);

    // ESC key to cancel
    document.addEventListener('keydown', handleEscapeKey);
}

/**
 * Handle ESC key press to cancel OCR selection
 */
function handleEscapeKey(e) {
    if (e.key === 'Escape' && ocrState.isActive) {
        cancelOCRSelection();
    }
}

/**
 * Start selection on mouse down
 */
function startSelection(e) {
    const remoteCaptureEle = document.getElementById('remoteCapture');
    const rect = remoteCaptureEle.getBoundingClientRect();

    // Check if click is within the remote capture element
    if (e.clientX < rect.left || e.clientX > rect.right ||
        e.clientY < rect.top || e.clientY > rect.bottom) {
        return;
    }

    ocrState.isDragging = true;
    ocrState.startX = e.clientX;
    ocrState.startY = e.clientY;

    const selectionBox = document.getElementById('ocr-selection-box');
    selectionBox.style.left = e.clientX + 'px';
    selectionBox.style.top = e.clientY + 'px';
    selectionBox.style.width = '0px';
    selectionBox.style.height = '0px';
    selectionBox.style.display = 'block';

    // Hide controls while dragging
    const controls = document.getElementById('ocr-controls');
    controls.style.display = 'none';
}

/**
 * Update selection box while dragging
 */
function updateSelection(e) {
    if (!ocrState.isDragging) return;

    const remoteCaptureEle = document.getElementById('remoteCapture');
    const rect = remoteCaptureEle.getBoundingClientRect();
    const selectionBox = document.getElementById('ocr-selection-box');

    // Constrain to remote capture element bounds
    let currentX = Math.max(rect.left, Math.min(e.clientX, rect.right));
    let currentY = Math.max(rect.top, Math.min(e.clientY, rect.bottom));

    const width = Math.abs(currentX - ocrState.startX);
    const height = Math.abs(currentY - ocrState.startY);
    const left = Math.min(ocrState.startX, currentX);
    const top = Math.min(ocrState.startY, currentY);

    selectionBox.style.width = width + 'px';
    selectionBox.style.height = height + 'px';
    selectionBox.style.left = left + 'px';
    selectionBox.style.top = top + 'px';
}

/**
 * End selection on mouse up
 */
function endSelection(e) {
    if (!ocrState.isDragging) return;

    ocrState.isDragging = false;

    const selectionBox = document.getElementById('ocr-selection-box');
    const width = parseInt(selectionBox.style.width);
    const height = parseInt(selectionBox.style.height);

    // Minimum selection size (10x10 pixels)
    if (width < 10 || height < 10) {
        selectionBox.style.display = 'none';
        return;
    }

    // Show controls
    showOCRControls();
}

/**
 * Position and show the OCR controls
 */
function showOCRControls() {
    const selectionBox = document.getElementById('ocr-selection-box');
    const controls = document.getElementById('ocr-controls');

    const boxRect = selectionBox.getBoundingClientRect();
    const controlsHeight = 100; // Approximate height

    // Determine if controls should be above or below the selection
    const spaceBelow = window.innerHeight - boxRect.bottom;
    const spaceAbove = boxRect.top;

    let controlsTop, controlsLeft;

    if (spaceBelow >= controlsHeight) {
        // Show below
        controlsTop = boxRect.bottom + 10;
    } else if (spaceAbove >= controlsHeight) {
        // Show above
        controlsTop = boxRect.top - controlsHeight - 10;
    } else {
        // Not enough space above or below, show inside at bottom
        controlsTop = boxRect.bottom - controlsHeight - 10;
    }

    // Center horizontally relative to selection box
    controlsLeft = boxRect.left + (boxRect.width / 2) - 125; // 125 is half of min-width

    // Ensure controls don't go off-screen
    controlsLeft = Math.max(10, Math.min(controlsLeft, window.innerWidth - 260));
    controlsTop = Math.max(10, Math.min(controlsTop, window.innerHeight - controlsHeight - 10));

    controls.style.left = controlsLeft + 'px';
    controls.style.top = controlsTop + 'px';
    controls.style.display = 'block';
}

// Languages whose scripts do not use spaces between characters. Tesseract
// tends to insert spurious spaces when recognizing them, so the result window
// offers (and defaults to) space removal for these.
const CJK_LANGUAGES = ['chi_sim', 'chi_tra', 'jpn', 'kor'];

function isCJKLanguage(langCode) {
    return CJK_LANGUAGES.includes(langCode);
}

/**
 * Remove the spurious spaces Tesseract inserts between CJK characters.
 * Line breaks are preserved; regular / no-break / full-width spaces and tabs
 * are stripped.
 */
function removeOcrSpaces(text) {
    return text.replace(/[ \t\u00A0\u3000]+/g, '');
}

/**
 * Confirm OCR selection and process
 */
async function confirmOCRSelection() {
    const selectionBox = document.getElementById('ocr-selection-box');
    const remoteCaptureEle = document.getElementById('remoteCapture');

    // Get selection coordinates relative to the remote capture element
    const captureRect = remoteCaptureEle.getBoundingClientRect();
    const selectionRect = selectionBox.getBoundingClientRect();

    // Calculate relative coordinates
    const relX = selectionRect.left - captureRect.left;
    const relY = selectionRect.top - captureRect.top;
    const relWidth = selectionRect.width;
    const relHeight = selectionRect.height;

    // Show loading indicator
    showOCRLoading();

    try {
        // Create a canvas to capture the selected region
        const canvas = document.createElement('canvas');
        const ctx = canvas.getContext('2d');

        // Calculate scale factor between displayed size and the intrinsic
        // stream size (naturalWidth for the MJPEG <img>, videoWidth for the
        // WebRTC <video> element)
        const intrinsicW = remoteCaptureEle.naturalWidth || remoteCaptureEle.videoWidth || captureRect.width;
        const intrinsicH = remoteCaptureEle.naturalHeight || remoteCaptureEle.videoHeight || captureRect.height;
        const scaleX = intrinsicW / captureRect.width;
        const scaleY = intrinsicH / captureRect.height;

        // Set canvas size to match the selected region in natural dimensions
        canvas.width = relWidth * scaleX;
        canvas.height = relHeight * scaleY;

        // Draw the selected portion of the image
        ctx.drawImage(
            remoteCaptureEle,
            relX * scaleX,
            relY * scaleY,
            canvas.width,
            canvas.height,
            0,
            0,
            canvas.width,
            canvas.height
        );

        // Perform OCR
        console.log('Starting OCR with language:', ocrState.selectedLanguage);
        const worker = await Tesseract.createWorker(ocrState.selectedLanguage);
        const { data: { text } } = await worker.recognize(canvas);
        await worker.terminate();

        ocrResultText = text;

    } catch (error) {
        console.error('OCR Error:', error);
        if (typeof $ !== 'undefined' && $.toast) {
            $.toast({ class: 'error', message: '<i class="red times icon"></i> OCR processing failed: ' + error.message, duration: 5000 });
        }
    } finally {
        // Clean up the selection overlay before presenting the result
        cancelOCRSelection();
    }

    if (ocrResultText !== null) {
        handleOCRResult(ocrResultText, ocrState.selectedLanguage);
    }
}

// Holds the raw text of the last OCR run while it is handed from
// confirmOCRSelection to the result presenter.
let ocrResultText = null;

/**
 * Present an OCR result: either copy it straight to the clipboard (when the
 * "Direct OCR to clipboard" preference is enabled) or open the floating
 * result window.
 */
function handleOCRResult(rawText, langCode) {
    ocrResultText = null;

    if (!rawText || rawText.trim() === '') {
        if (typeof $ !== 'undefined' && $.toast) {
            $.toast({ message: '<i class="yellow search icon"></i> No text was recognized in the selected area', duration: 4000 });
        }
        return;
    }

    const cjk = isCJKLanguage(langCode);

    // Direct mode: skip the window, apply CJK space removal automatically
    if (typeof directOcrToClipboard !== 'undefined' && directOcrToClipboard) {
        const processed = (cjk ? removeOcrSpaces(rawText) : rawText).trim();
        copyTextToClipboard(processed).then(function() {
            if (typeof $ !== 'undefined' && $.toast) {
                $.toast({ message: '<i class="green copy icon"></i> OCR text copied to clipboard', duration: 3500 });
            }
        }).catch(function() {
            // Clipboard access failed (e.g. document lost focus) — fall back
            // to the result window where the copy button is a user gesture.
            showOCRResultWindow(rawText, langCode);
        });
        return;
    }

    showOCRResultWindow(rawText, langCode);
}

/**
 * Copy text to the user clipboard. Uses the async Clipboard API with a
 * hidden-textarea execCommand fallback for older browsers.
 * Returns a promise that rejects when both methods fail.
 */
function copyTextToClipboard(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
        return navigator.clipboard.writeText(text).catch(function() {
            return _copyViaExecCommand(text);
        });
    }
    return _copyViaExecCommand(text);
}

function _copyViaExecCommand(text) {
    return new Promise(function(resolve, reject) {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        let ok = false;
        try {
            ok = document.execCommand('copy');
        } catch (e) {
            ok = false;
        }
        document.body.removeChild(ta);
        if (ok) { resolve(); } else { reject(new Error('execCommand copy failed')); }
    });
}

/* ---- OCR result floating window ---- */

/**
 * Show the floating OCR result window (draggable, like the file manager
 * popup). Lets the user review / edit the recognized text, toggle CJK space
 * removal, and copy the result to the clipboard.
 */
function showOCRResultWindow(rawText, langCode) {
    closeOCRResultWindow();

    // Keep keystrokes inside the window from being sent to the remote host
    pauseAllKeyEvents = true;

    const cjk = isCJKLanguage(langCode);

    const win = document.createElement('div');
    win.id = 'ocr-result-window';
    win.style.cssText = `
        position: fixed;
        top: 50%;
        left: 50%;
        transform: translate(-50%, -50%);
        width: min(92vw, 520px);
        background: #fff;
        border-radius: 8px;
        box-shadow: 0 12px 40px rgba(0, 0, 0, 0.4);
        border: 1px solid #d8d8d8;
        z-index: 2100;
        display: flex;
        flex-direction: column;
        overflow: hidden;
    `;

    win.innerHTML = `
        <div id="ocr-result-header" style="display:flex;align-items:center;justify-content:space-between;
                padding:0.6em 1em;background:#f5f5f5;border-bottom:1px solid #e0e0e0;cursor:move;user-select:none;">
            <span style="font-weight:600;"><i class="i cursor icon"></i> OCR Result</span>
            <button class="ui mini icon button" id="ocr-result-close" title="Close" style="margin:0;">
                <i class="times icon"></i>
            </button>
        </div>
        <div style="padding:1em;display:flex;flex-direction:column;gap:0.8em;">
            <textarea id="ocr-result-text" spellcheck="false"
                style="width:100%;height:180px;resize:vertical;padding:0.6em;border:1px solid #d4d4d5;
                       border-radius:4px;font-size:0.95em;font-family:inherit;box-sizing:border-box;"></textarea>
            <label style="display:flex;align-items:center;gap:0.5em;cursor:pointer;font-size:0.92em;color:#555;">
                <input type="checkbox" id="ocr-remove-spaces" style="cursor:pointer;">
                Remove all spaces (recommended for CJK text)
            </label>
            <div style="display:flex;gap:0.5em;justify-content:flex-end;">
                <button class="ui basic button" id="ocr-result-cancel">Close</button>
                <button class="ui teal button" id="ocr-result-copy">
                    <i class="copy icon"></i> Copy to Clipboard
                </button>
            </div>
        </div>
    `;

    document.body.appendChild(win);

    const textarea = document.getElementById('ocr-result-text');
    const chkRemoveSpaces = document.getElementById('ocr-remove-spaces');

    // Auto-check space removal for CJK languages
    chkRemoveSpaces.checked = cjk;

    function refreshText() {
        textarea.value = (chkRemoveSpaces.checked ? removeOcrSpaces(rawText) : rawText).trim();
    }
    refreshText();
    chkRemoveSpaces.addEventListener('change', refreshText);

    // Copy button (copies the current textarea content, including user edits)
    document.getElementById('ocr-result-copy').addEventListener('click', function() {
        const btn = this;
        copyTextToClipboard(textarea.value).then(function() {
            btn.innerHTML = '<i class="check icon"></i> Copied!';
            if (typeof $ !== 'undefined' && $.toast) {
                $.toast({ message: '<i class="green copy icon"></i> OCR text copied to clipboard', duration: 3000 });
            }
            setTimeout(closeOCRResultWindow, 600);
        }).catch(function() {
            if (typeof $ !== 'undefined' && $.toast) {
                $.toast({ class: 'error', message: '<i class="red times icon"></i> Failed to access the clipboard', duration: 4000 });
            }
        });
    });

    document.getElementById('ocr-result-close').addEventListener('click', closeOCRResultWindow);
    document.getElementById('ocr-result-cancel').addEventListener('click', closeOCRResultWindow);
    document.addEventListener('keydown', _ocrResultEscHandler);

    // Drag the window by its header (mirrors the file manager popup behavior)
    const header = document.getElementById('ocr-result-header');
    let dragging = false, offX = 0, offY = 0;
    header.addEventListener('mousedown', function(e) {
        if (e.target.closest('button')) return;
        dragging = true;
        const rect = win.getBoundingClientRect();
        offX = e.clientX - rect.left;
        offY = e.clientY - rect.top;
        win.style.transform = 'none';
        win.style.top = rect.top + 'px';
        win.style.left = rect.left + 'px';
        e.preventDefault();
    });
    document.addEventListener('mousemove', _ocrResultDragMove);
    document.addEventListener('mouseup', _ocrResultDragEnd);
    function _ocrResultDragMove(e) {
        if (!dragging) return;
        win.style.top = (e.clientY - offY) + 'px';
        win.style.left = (e.clientX - offX) + 'px';
    }
    function _ocrResultDragEnd() { dragging = false; }
    win._dragCleanup = function() {
        document.removeEventListener('mousemove', _ocrResultDragMove);
        document.removeEventListener('mouseup', _ocrResultDragEnd);
    };

    textarea.focus();
}

function _ocrResultEscHandler(e) {
    if (e.key === 'Escape') {
        closeOCRResultWindow();
    }
}

/**
 * Close the OCR result window and resume remote key event handling.
 */
function closeOCRResultWindow() {
    const win = document.getElementById('ocr-result-window');
    if (win) {
        if (win._dragCleanup) win._dragCleanup();
        win.remove();
    }
    document.removeEventListener('keydown', _ocrResultEscHandler);
    pauseAllKeyEvents = false;
}

/**
 * Show loading indicator during OCR processing
 */
function showOCRLoading() {
    const controls = document.getElementById('ocr-controls');
    controls.innerHTML = `
        <div style="text-align: center; padding: 10px;">
            <div class="ui active inline loader"></div>
            <p style="margin-top: 10px;">Processing OCR...</p>
        </div>
    `;
}

/**
 * Cancel OCR selection and clean up
 */
function cancelOCRSelection() {
    ocrState.isActive = false;
    ocrState.isDragging = false;
    pauseAllKeyEvents = false;

    // Remove elements
    const overlay = document.getElementById('ocr-overlay');
    const selectionBox = document.getElementById('ocr-selection-box');
    const controls = document.getElementById('ocr-controls');

    if (overlay) overlay.remove();
    if (selectionBox) selectionBox.remove();
    if (controls) controls.remove();

    // Remove event listener
    document.removeEventListener('keydown', handleEscapeKey);
}

