/*
    onscreen-keyboard.js
    
    Virtual on-screen keyboard with draggable and resizable features
*/

const MAX_HOLD_KEYS = 6; // Maximum number of non-modifier keys that can be held simultaneously

// Keyboard state
let keyboardVisible = false;
let keyboardFullWidth = false;
let keyboardDragging = false;
let keyboardOffset = { x: 0, y: 0 };

// Hold mode toggle (for non-modifier keys)
let holdModeEnabled = false;
let heldKeys = new Map(); // Stores held keys: keyCode -> {$key, isRightKey}

// Modifier key states (hold mode)
let shiftHeld = false;
let ctrlHeld = false;
let altHeld = false;
let capsLockHeld = false;

// Keyboard position
let keyboardPos = { x: 0, y: 0 };

$(document).ready(function() {
    initializeOnscreenKeyboard();
});

function initializeOnscreenKeyboard() {
    const $keyboard = $('#onscreenKeyboard');
    const $dragBar = $('.keyboard-drag-bar');
    
    // Set initial position (centered at bottom)
    centerKeyboard();
    
    // Make drag bar draggable
    $dragBar.on('mousedown', function(e) {
        // Don't start dragging if clicking on buttons
        if ($(e.target).closest('button').length > 0) {
            return;
        }
        
        // Don't drag in fullwidth mode
        if (keyboardFullWidth) {
            return;
        }
        
        keyboardDragging = true;
        keyboardOffset.x = e.clientX - keyboardPos.x;
        keyboardOffset.y = e.clientY - keyboardPos.y;
        
        $keyboard.addClass('dragging');
        e.preventDefault();
    });
    
    $(document).on('mousemove', function(e) {
        if (keyboardDragging) {
            keyboardPos.x = e.clientX - keyboardOffset.x;
            keyboardPos.y = e.clientY - keyboardOffset.y;
            
            // Keep keyboard within viewport
            const maxX = window.innerWidth - $keyboard.outerWidth();
            const maxY = window.innerHeight - $keyboard.outerHeight();
            
            keyboardPos.x = Math.max(0, Math.min(keyboardPos.x, maxX));
            keyboardPos.y = Math.max(0, Math.min(keyboardPos.y, maxY));
            
            $keyboard.css({
                left: keyboardPos.x + 'px',
                top: keyboardPos.y + 'px'
            });
        }
    });
    
    $(document).on('mouseup', function() {
        if (keyboardDragging) {
            keyboardDragging = false;
            $keyboard.removeClass('dragging');
        }
    });
    
    // Attach key press handlers
    $('.key').on('mousedown', function(e) {
        e.preventDefault();
        const $key = $(this);
        handleKeyPress($key);
    });
    
    $('.key').on('mouseup', function(e) {
        e.preventDefault();
        const $key = $(this);
        handleKeyRelease($key);
    });
    
    // Prevent context menu on keys
    $('.key').on('contextmenu', function(e) {
        e.preventDefault();
        return false;
    });
}

function centerKeyboard() {
    const $keyboard = $('#onscreenKeyboard');
    const keyboardWidth = $keyboard.outerWidth();
    const keyboardHeight = $keyboard.outerHeight();
    
    keyboardPos.x = (window.innerWidth - keyboardWidth) / 2;
    keyboardPos.y = window.innerHeight - keyboardHeight - 20;
    
    $keyboard.css({
        left: keyboardPos.x + 'px',
        top: keyboardPos.y + 'px'
    });
}

function showOnscreenKeyboard() {
    const $keyboard = $('#onscreenKeyboard');
    $keyboard.show();
    keyboardVisible = true;
    
    if (!keyboardFullWidth) {
        centerKeyboard();
    }
}

function closeOnscreenKeyboard() {
    const $keyboard = $('#onscreenKeyboard');
    $keyboard.hide();
    keyboardVisible = false;
    
    // Release all held modifier keys
    releaseAllModifiers();
}

function toggleOnscreenKeyboard() {
    if (keyboardVisible) {
        closeOnscreenKeyboard();
    } else {
        showOnscreenKeyboard();
    }
}

function toggleKeyboardSize() {
    const $keyboard = $('#onscreenKeyboard');
    const $toggleBtn = $('#btnToggleKeyboardSize i');
    
    keyboardFullWidth = !keyboardFullWidth;
    
    if (keyboardFullWidth) {
        // Switch to full width mode
        $keyboard.addClass('fullwidth');
        $toggleBtn.removeClass('expand arrows alternate').addClass('compress');
        
        // Position at bottom
        $keyboard.css({
            left: '0',
            top: 'auto',
            bottom: '0'
        });
    } else {
        // Switch to floating mode
        $keyboard.removeClass('fullwidth');
        $toggleBtn.removeClass('compress').addClass('expand arrows alternate');
        
        // Reset to centered position
        $keyboard.css({
            bottom: 'auto'
        });
        centerKeyboard();
    }
}

function handleKeyPress($key) {
    const keyCode = parseInt($key.attr('data-key'));
    const isModifier = $key.hasClass('modifier-key');
    
    // Check if it's a right-side key (numpad enter, right shift/ctrl/alt)
    const isRightKey = $key.hasClass('key-shift-right') || 
                       $key.hasClass('key-ctrl-right') || 
                       $key.hasClass('key-alt-right') ||
                       $key.hasClass('key-numpad-enter');
    
    if (isModifier) {
        // Handle modifier keys (Shift, Ctrl, Alt, Caps Lock) - always in hold mode
        toggleModifierKey($key, keyCode, isRightKey);
    } else {
        // Handle regular keys
        if (holdModeEnabled) {
            // In hold mode: toggle key state
            const keyIdentifier = keyCode + (isRightKey ? '_right' : '_left');
            
            if (heldKeys.has(keyIdentifier)) {
                // Key is already held - release it
                const heldData = heldKeys.get(keyIdentifier);
                heldData.$key.removeClass('active held');
                sendVirtualKeyRelease(keyCode, isRightKey);
                heldKeys.delete(keyIdentifier);
            } else {
                // Check if we've reached the 6-key limit
                if (heldKeys.size >= MAX_HOLD_KEYS) {
                    // Show toast notification
                    $('body').toast({
                        message: `Maximum ${MAX_HOLD_KEYS} keys can be held simultaneously`,
                        class: 'warning',
                        displayTime: 3000,
                        //position: 'top center',
                        showIcon: 'exclamation triangle'
                    });
                    console.warn(`Max held keys reached (${MAX_HOLD_KEYS})`);
                    return; // Don't hold the key or send events
                }
                
                // Key is not held - hold it down
                $key.addClass('active held');
                sendVirtualKeyPress(keyCode, isRightKey);
                heldKeys.set(keyIdentifier, { $key: $key, isRightKey: isRightKey });
            }
        } else {
            // Normal mode: just add visual feedback and send press
            $key.addClass('active');
            sendVirtualKeyPress(keyCode, isRightKey);
        }
    }
}

function handleKeyRelease($key) {
    const keyCode = parseInt($key.attr('data-key'));
    const isModifier = $key.hasClass('modifier-key');
    
    // Check if it's a right-side key
    const isRightKey = $key.hasClass('key-shift-right') || 
                       $key.hasClass('key-ctrl-right') || 
                       $key.hasClass('key-alt-right') ||
                       $key.hasClass('key-numpad-enter');
    
    if (!isModifier && !holdModeEnabled) {
        // Only send release in normal mode (not hold mode)
        $key.removeClass('active');
        sendVirtualKeyRelease(keyCode, isRightKey);
    }
    // In hold mode, release is handled by clicking again (in handleKeyPress)
    // Modifier keys are handled separately, so no release here
}

function toggleModifierKey($key, keyCode, isRightKey = false) {
    if (keyCode === 16) { // Shift
        shiftHeld = !shiftHeld;
        updateModifierState($('.key-shift-left, .key-shift-right'), shiftHeld);
        
        if (shiftHeld) {
            sendVirtualKeyPress(16, isRightKey);
        } else {
            sendVirtualKeyRelease(16, isRightKey);
        }
    } else if (keyCode === 17) { // Ctrl
        ctrlHeld = !ctrlHeld;
        updateModifierState($('.key-ctrl-left, .key-ctrl-right'), ctrlHeld);
        
        if (ctrlHeld) {
            sendVirtualKeyPress(17, isRightKey);
        } else {
            sendVirtualKeyRelease(17, isRightKey);
        }
    } else if (keyCode === 18) { // Alt
        altHeld = !altHeld;
        updateModifierState($('.key-alt-left, .key-alt-right'), altHeld);
        
        if (altHeld) {
            sendVirtualKeyPress(18, isRightKey);
        } else {
            sendVirtualKeyRelease(18, isRightKey);
        }
    } else if (keyCode === 20) { // Caps Lock
        capsLockHeld = !capsLockHeld;
        updateModifierState($('.key-caps'), capsLockHeld);
        
        // Caps lock is a toggle, send press and release
        sendVirtualKeyPress(20, false);
        setTimeout(() => sendVirtualKeyRelease(20, false), 50);
    } else if (keyCode === 91) { // Win key
        // Win key - just send press and release
        sendVirtualKeyPress(91, false);
        setTimeout(() => sendVirtualKeyRelease(91, false), 50);
    }
}

function updateModifierState($keys, isHeld) {
    if (isHeld) {
        $keys.addClass('held');
    } else {
        $keys.removeClass('held');
    }
}

function releaseAllModifiers() {
    if (shiftHeld) {
        sendVirtualKeyRelease(16, false);
        shiftHeld = false;
        updateModifierState($('.key-shift-left, .key-shift-right'), false);
    }
    if (ctrlHeld) {
        sendVirtualKeyRelease(17, false);
        ctrlHeld = false;
        updateModifierState($('.key-ctrl-left, .key-ctrl-right'), false);
    }
    if (altHeld) {
        sendVirtualKeyRelease(18, false);
        altHeld = false;
        updateModifierState($('.key-alt-left, .key-alt-right'), false);
    }
    if (capsLockHeld) {
        capsLockHeld = false;
        updateModifierState($('.key-caps'), false);
    }
}

function releaseAllHeldKeys() {
    // Release all non-modifier keys held in hold mode
    heldKeys.forEach((heldData, keyIdentifier) => {
        const keyCode = parseInt(keyIdentifier.split('_')[0]);
        heldData.$key.removeClass('active held');
        sendVirtualKeyRelease(keyCode, heldData.isRightKey);
    });
    heldKeys.clear();
}

function toggleHoldMode() {
    holdModeEnabled = !holdModeEnabled;
    const $toggleBtn = $('#btnToggleHoldMode i');
    const $btnElement = $('#btnToggleHoldMode');
    
    if (holdModeEnabled) {
        // Hold mode enabled
        //$toggleBtn.removeClass('long arrow alternate down').addClass('lock');
        $btnElement.attr('title', 'Hold Mode: ON (Click keys to hold/release)');
        $btnElement.addClass("lock");
        console.log('Hold mode enabled');
    } else {
        // Hold mode disabled - release all held keys
        //$toggleBtn.removeClass('lock').addClass('long arrow alternate down');
        $btnElement.attr('title', 'Hold Mode: OFF');
        $btnElement.removeClass("lock");

        releaseAllHeldKeys();
        console.log('Hold mode disabled');
    }
}

function sendVirtualKeyPress(keyCode, isRightModifier = false) {
    if (!hidsocket || hidsocket.readyState !== WebSocket.OPEN) {
        console.error('HID WebSocket not connected');
        return;
    }
    
    const hidCommand = {
        event: 0, // Key down
        keycode: keyCode,
        is_right_modifier_key: isRightModifier
    };
    
    hidsocket.send(JSON.stringify(hidCommand));
    console.log('Virtual key press:', keyCode, 'isRight:', isRightModifier);
}

function sendVirtualKeyRelease(keyCode, isRightModifier = false) {
    if (!hidsocket || hidsocket.readyState !== WebSocket.OPEN) {
        console.error('HID WebSocket not connected');
        return;
    }
    
    const hidCommand = {
        event: 1, // Key up
        keycode: keyCode,
        is_right_modifier_key: isRightModifier
    };
    
    hidsocket.send(JSON.stringify(hidCommand));
    console.log('Virtual key release:', keyCode, 'isRight:', isRightModifier);
}
