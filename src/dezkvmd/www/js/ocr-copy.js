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
    selectedLanguage: 'eng'
};

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

    // Create controls container (hidden initially)
    const controls = document.createElement('div');
    controls.id = 'ocr-controls';
    controls.style.cssText = `
        position: absolute;
        display: none;
        background-color: white;
        padding: 12px;
        border-radius: 4px;
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
        z-index: 10000;
        min-width: 250px;
    `;

    // Language selector
    const langLabel = document.createElement('label');
    langLabel.textContent = 'Language: ';
    langLabel.style.cssText = 'margin-right: 8px; font-weight: bold;';

    const langSelect = document.createElement('select');
    langSelect.id = 'ocr-language-select';
    langSelect.className = 'ui compact dropdown';
    langSelect.style.cssText = 'margin-bottom: 10px;';
    
    SUPPORTED_LANGUAGES.forEach(lang => {
        const option = document.createElement('option');
        option.value = lang.code;
        option.textContent = lang.name;
        if (lang.code === ocrState.selectedLanguage) {
            option.selected = true;
        }
        langSelect.appendChild(option);
    });

    langSelect.addEventListener('change', function() {
        ocrState.selectedLanguage = this.value;
    });

    // Buttons container
    const buttonsDiv = document.createElement('div');
    buttonsDiv.style.cssText = 'display: flex; gap: 8px; margin-top: 10px;';

    const confirmBtn = document.createElement('button');
    confirmBtn.className = 'ui small green button';
    confirmBtn.innerHTML = '<i class="check icon"></i>Confirm';
    confirmBtn.onclick = confirmOCRSelection;

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'ui small red button';
    cancelBtn.innerHTML = '<i class="times icon"></i>Cancel';
    cancelBtn.onclick = cancelOCRSelection;

    buttonsDiv.appendChild(confirmBtn);
    buttonsDiv.appendChild(cancelBtn);

    controls.appendChild(langLabel);
    controls.appendChild(langSelect);
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

        // Calculate scale factor between displayed image and natural image size
        const scaleX = remoteCaptureEle.naturalWidth / captureRect.width;
        const scaleY = remoteCaptureEle.naturalHeight / captureRect.height;

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

        console.log('OCR Result:');
        console.log(text);

        // TODO: Copy to clipboard or show in UI
        // For now, just log to console as requested

    } catch (error) {
        console.error('OCR Error:', error);
        alert('OCR processing failed: ' + error.message);
    } finally {
        // Clean up
        cancelOCRSelection();
    }
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

