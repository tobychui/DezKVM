/*
    dez-ui.js

    DezKVM shared UI kit behaviours. Replaces the Fomantic-UI javascript with
    small custom implementations exposing the same jQuery plugin APIs that the
    existing code base calls ($.toast, .modal(), .progress(), .checkbox(),
    .slider(), .dropdown()), plus the DezWindow draggable floating-window
    factory used by the shell and the viewport toolbox.

    Requires jQuery. Styles live in css/dez-ui.css.
*/

(function ($) {
    'use strict';

    if (!$) {
        console.error('dez-ui.js requires jQuery');
        return;
    }

    /* =================================================== toast ($.toast) */

    function toastRegion() {
        let region = document.getElementById('dez-toast-region');
        if (!region) {
            region = document.createElement('div');
            region.id = 'dez-toast-region';
            document.body.appendChild(region);
        }
        return region;
    }

    $.toast = function (opts) {
        opts = opts || {};
        const box = document.createElement('div');
        box.className = 'toast-box ' + (opts.class || '');

        const msg = document.createElement('div');
        msg.innerHTML = opts.message || '';
        box.appendChild(msg);

        let sticky = false;
        if (Array.isArray(opts.actions) && opts.actions.length > 0) {
            sticky = true; // toasts with actions stay until a choice is made
            const actionsRow = document.createElement('div');
            actionsRow.className = 'toast-actions';
            opts.actions.forEach(function (action) {
                const btn = document.createElement('button');
                btn.className = 'ui ' + (action.class || 'basic') + ' button';
                btn.textContent = action.text || 'OK';
                btn.addEventListener('click', function () {
                    try { if (action.click) action.click.call(box); } catch (e) { console.error(e); }
                    box.remove();
                });
                actionsRow.appendChild(btn);
            });
            box.appendChild(actionsRow);
        }

        toastRegion().appendChild(box);

        if (typeof opts.onVisible === 'function') {
            try { opts.onVisible.call(box); } catch (e) { /* legacy hook, ignore */ }
        }

        if (!sticky) {
            const duration = typeof opts.duration === 'number' ? opts.duration : 3500;
            if (duration > 0) {
                setTimeout(function () {
                    box.style.opacity = '0';
                    box.style.transition = 'opacity 0.25s';
                    setTimeout(function () { box.remove(); }, 260);
                }, duration);
            }
        }else if (typeof opts.duration === 'number' && opts.duration > 0) {
            //Explicity decalre durations
            setTimeout(function () {
                box.style.opacity = '0';
                box.style.transition = 'opacity 0.25s';
                setTimeout(function () { box.remove(); }, 260);
            }, opts.duration); 
        }
        return $(box);
    };

    /* ==================================================== modal (.modal) */

    let dimmerEl = null;
    let activeModal = null;

    function getDimmer() {
        if (!dimmerEl) {
            dimmerEl = document.createElement('div');
            dimmerEl.className = 'dez-dimmer';
            dimmerEl.addEventListener('mousedown', function (e) {
                if (e.target !== dimmerEl || !activeModal) return;
                const cfg = $(activeModal).data('dez-modal-cfg') || {};
                if (cfg.closable === false) return;
                hideModal(activeModal);
            });
            document.body.appendChild(dimmerEl);
        }
        return dimmerEl;
    }

    function showModal(el) {
        const dimmer = getDimmer();
        if (activeModal && activeModal !== el) hideModal(activeModal);
        if (el.parentElement !== dimmer) dimmer.appendChild(el);
        dimmer.classList.add('active');
        el.classList.add('active');
        activeModal = el;
    }

    function hideModal(el) {
        el.classList.remove('active');
        if (dimmerEl) dimmerEl.classList.remove('active');
        if (activeModal === el) activeModal = null;
        const cfg = $(el).data('dez-modal-cfg') || {};
        if (typeof cfg.onHidden === 'function') {
            try { cfg.onHidden.call(el); } catch (e) { console.error(e); }
        }
    }

    function wireModalButtons(el) {
        if (el.dataset.dezModalWired) return;
        el.dataset.dezModalWired = '1';
        $(el).on('click', '.approve, .positive, .ok', function () {
            const cfg = $(el).data('dez-modal-cfg') || {};
            let result = true;
            if (typeof cfg.onApprove === 'function') {
                try { result = cfg.onApprove.call(el); } catch (e) { console.error(e); result = true; }
            }
            if (result !== false) hideModal(el);
        });
        $(el).on('click', '.deny, .cancel, .negative', function () {
            const cfg = $(el).data('dez-modal-cfg') || {};
            let result = true;
            if (typeof cfg.onDeny === 'function') {
                try { result = cfg.onDeny.call(el); } catch (e) { console.error(e); result = true; }
            }
            if (result !== false) hideModal(el);
        });
        // Modal header/content close icons
        $(el).on('click', '> .close.icon, > i.close', function () { hideModal(el); });
    }

    $.fn.modal = function (arg) {
        this.each(function () {
            const el = this;
            wireModalButtons(el);
            if (typeof arg === 'object' && arg !== null) {
                $(el).data('dez-modal-cfg', arg);
            } else if (arg === 'show') {
                showModal(el);
            } else if (arg === 'hide') {
                hideModal(el);
            } else if (arg === 'toggle') {
                el.classList.contains('active') ? hideModal(el) : showModal(el);
            }
        });
        return this;
    };

    /* ============================================== progress (.progress) */

    $.fn.progress = function (opts) {
        this.each(function () {
            const el = this;
            let bar = el.querySelector('.bar');
            if (!bar) {
                bar = document.createElement('div');
                bar.className = 'bar';
                el.insertBefore(bar, el.firstChild);
            }
            if (typeof opts === 'object' && opts !== null && typeof opts.percent !== 'undefined') {
                const pct = Math.max(0, Math.min(100, Number(opts.percent) || 0));
                bar.style.width = pct + '%';
                const txt = bar.querySelector('.progress');
                if (txt) txt.textContent = Math.round(pct) + '%';
            }
        });
        return this;
    };

    /* ============================================== checkbox (.checkbox) */

    $.fn.checkbox = function (behavior) {
        this.each(function () {
            const input = this.tagName === 'INPUT' ? this : this.querySelector('input[type="checkbox"]');
            if (!input) return;
            if (behavior === 'check') input.checked = true;
            else if (behavior === 'uncheck') input.checked = false;
            else if (behavior === 'toggle') input.checked = !input.checked;
            // object opts / init: nothing to do, CSS handles the visuals
        });
        return this;
    };

    /* ================================================== slider (.slider) */

    $.fn.slider = function (arg, value) {
        let ret = this;
        this.each(function () {
            const el = this;
            let api = $(el).data('dez-slider');

            if (typeof arg === 'object' && arg !== null) {
                // init: build range input + value bubble
                const cfg = arg;
                el.classList.add('dez-slider');
                el.innerHTML = '';
                const range = document.createElement('input');
                range.type = 'range';
                range.min = cfg.min != null ? cfg.min : 0;
                range.max = cfg.max != null ? cfg.max : 100;
                range.step = cfg.step != null ? cfg.step : 1;
                range.value = cfg.start != null ? cfg.start : range.min;
                const bubble = document.createElement('span');
                bubble.className = 'dez-slider-value';
                bubble.textContent = range.value;
                el.appendChild(range);
                el.appendChild(bubble);

                range.addEventListener('input', function () {
                    bubble.textContent = range.value;
                });
                range.addEventListener('change', function () {
                    bubble.textContent = range.value;
                    if (typeof cfg.onChange === 'function') {
                        cfg.onChange.call(el, Number(range.value));
                    }
                });

                api = {
                    set: function (v) { range.value = v; bubble.textContent = range.value; },
                    get: function () { return Number(range.value); }
                };
                $(el).data('dez-slider', api);
            } else if (arg === 'set value' && api) {
                api.set(value);
            } else if (arg === 'get value' && api) {
                ret = api.get();
            }
        });
        return ret;
    };

    /* ======================================== dropdown (.dropdown, no-op) */
    // Native <select> elements are styled by dez-ui.css; the Fomantic
    // enhancement is intentionally not reproduced.
    $.fn.dropdown = function () { return this; };

    /* =========================================== DezWindow (float window) */

    const dezWindows = {}; // id -> instance
    let windowZ = 2500;

    /**
     * Transparent full-viewport layer shown only while a floating window is
     * being dragged or resized. It sits above the page content (including
     * the KVM viewport iframe) but below the floating windows, so every
     * mousemove/mouseup during the gesture lands on our own document
     * instead of being swallowed by an iframe the cursor flies over.
     */
    let dragShieldEl = null;
    let dragShieldUsers = 0;

    function showDragShield(cursor) {
        dragShieldUsers += 1;
        if (!dragShieldEl) {
            dragShieldEl = document.createElement('div');
            dragShieldEl.className = 'dez-drag-shield';
            document.body.appendChild(dragShieldEl);
        }
        dragShieldEl.style.cursor = cursor || 'move';
        dragShieldEl.style.display = '';
    }

    function hideDragShield() {
        dragShieldUsers = Math.max(0, dragShieldUsers - 1);
        if (dragShieldUsers === 0 && dragShieldEl) dragShieldEl.style.display = 'none';
    }

    /**
     * Create (or focus) a draggable floating window.
     *
     * options: {
     *   id:       unique id; reuses/focuses (and un-hides) the existing
     *             window when already open
     *   title:    header text
     *   icon:     optional /img/icons/*.svg path shown in the header
     *   width, height: css sizes (numbers = px)
     *   x, y:     initial position; defaults to a centered cascade
     *   iframe:   url -> body becomes an iframe
     *   ajax:     url -> body content is jQuery .load()-ed from the url
     *   content:  html string or element for the body
     *   padded:   add padding to the body (for ajax/content windows)
     *   frameId:  optional id attribute for the iframe element
     *   resizable:    add edge/corner grips for resizing
     *   minimizable:  add a hide (—) button; the window keeps living in the
     *                 DOM (iframes keep their session) and reappears when the
     *                 same window id is opened again
     *   confirmClose: message string -> the X button shows an inline
     *                 confirmation before actually closing
     *   onClose:  callback fired after the window is removed
     * }
     */
    function DezWindow(options) {
        const opts = options || {};
        if (opts.id && dezWindows[opts.id]) {
            const existing = dezWindows[opts.id];
            existing.show();
            existing.focus();
            return existing;
        }

        const self = {};
        const win = document.createElement('div');
        win.className = 'dez-window';
        if (opts.id) win.dataset.dezWindowId = opts.id;

        const width = typeof opts.width === 'number' ? opts.width + 'px' : (opts.width || '520px');
        const height = typeof opts.height === 'number' ? opts.height + 'px' : (opts.height || 'auto');
        win.style.width = 'min(' + width + ', 94vw)';
        if (height !== 'auto') win.style.height = 'min(' + height + ', 88vh)';

        // Cascade placement so stacked windows don't fully overlap
        const openCount = Object.keys(dezWindows).length;
        const baseX = opts.x != null ? opts.x : Math.max(12, (window.innerWidth - parseInt(width)) / 2 + openCount * 28);
        const baseY = opts.y != null ? opts.y : Math.max(12, window.innerHeight * 0.14 + openCount * 28);
        win.style.left = baseX + 'px';
        win.style.top = baseY + 'px';

        // Header
        const header = document.createElement('div');
        header.className = 'dez-window-header';
        if (opts.icon) {
            const img = document.createElement('img');
            img.className = 'dez-icon';
            img.src = opts.icon;
            img.alt = '';
            header.appendChild(img);
        }
        const title = document.createElement('span');
        title.className = 'dez-window-title';
        title.textContent = opts.title || '';
        header.appendChild(title);

        if (opts.minimizable) {
            const minBtn = document.createElement('button');
            minBtn.className = 'dez-window-btn';
            minBtn.title = 'Hide (session keeps running)';
            minBtn.innerHTML = '<img src="/img/icons/minimize.svg" alt="Hide">';
            minBtn.addEventListener('click', function () { self.hide(); });
            header.appendChild(minBtn);
        }

        const closeBtn = document.createElement('button');
        closeBtn.className = 'dez-window-btn';
        closeBtn.title = 'Close';
        closeBtn.innerHTML = '<img src="/img/icons/close.svg" alt="Close">';
        closeBtn.addEventListener('click', function () {
            if (opts.confirmClose) {
                self.showCloseConfirm();
            } else {
                self.close();
            }
        });
        header.appendChild(closeBtn);
        win.appendChild(header);

        // Body
        const body = document.createElement('div');
        body.className = 'dez-window-body' + (opts.padded ? ' padded' : '');
        if (opts.iframe) {
            const frame = document.createElement('iframe');
            frame.src = opts.iframe;
            if (opts.frameId) frame.id = opts.frameId;
            body.appendChild(frame);
        } else if (opts.content) {
            if (typeof opts.content === 'string') body.innerHTML = opts.content;
            else body.appendChild(opts.content);
        }
        win.appendChild(body);
        document.body.appendChild(win);

        if (opts.ajax) {
            $(body).load(opts.ajax, function (res, status, xhr) {
                if (status === 'error') {
                    body.innerHTML = '<div class="ui error message">Failed to load tool (' + xhr.status + ')</div>';
                }
            });
        }

        // Dragging. While dragging/resizing, the dez-interacting class turns
        // off pointer events on window iframes, and a full-viewport
        // transparent shield is laid over the page (below the floating
        // windows) so that a fast pointer that outruns the window cannot
        // hand the mousemove/mouseup stream to whatever sits underneath —
        // most importantly the KVM viewport iframe, which would otherwise
        // forward the drag to the remote machine and strand the window in
        // a "still dragging" state.
        let dragging = false, offX = 0, offY = 0;
        header.addEventListener('mousedown', function (e) {
            if (e.target.closest('button')) return;
            dragging = true;
            showDragShield('move');
            document.body.classList.add('dez-interacting');
            const rect = win.getBoundingClientRect();
            offX = e.clientX - rect.left;
            offY = e.clientY - rect.top;
            e.preventDefault();
        });

        // Resizing (east / south / south-east grips)
        let resizing = null; // 'e' | 's' | 'se'
        let startW = 0, startH = 0, startX = 0, startY = 0;
        if (opts.resizable) {
            ['e', 's', 'se'].forEach(function (dir) {
                const grip = document.createElement('div');
                grip.className = 'dez-window-grip dez-window-grip-' + dir;
                grip.addEventListener('mousedown', function (e) {
                    resizing = dir;
                    showDragShield(dir === 'e' ? 'ew-resize' : (dir === 's' ? 'ns-resize' : 'nwse-resize'));
                    document.body.classList.add('dez-interacting');
                    const rect = win.getBoundingClientRect();
                    // Switch from the min(...) shorthand to explicit px so
                    // the resize is applied verbatim.
                    startW = rect.width;
                    startH = rect.height;
                    startX = e.clientX;
                    startY = e.clientY;
                    win.style.width = startW + 'px';
                    win.style.height = startH + 'px';
                    e.preventDefault();
                    e.stopPropagation();
                });
                win.appendChild(grip);
            });
        }

        function onMove(e) {
            if (resizing) {
                if (resizing === 'e' || resizing === 'se') {
                    win.style.width = Math.max(280, Math.min(startW + (e.clientX - startX), window.innerWidth - 20)) + 'px';
                }
                if (resizing === 's' || resizing === 'se') {
                    win.style.height = Math.max(160, Math.min(startH + (e.clientY - startY), window.innerHeight - 20)) + 'px';
                }
                return;
            }
            if (!dragging) return;
            const maxX = window.innerWidth - 60;
            const maxY = window.innerHeight - 40;
            win.style.left = Math.max(-win.offsetWidth + 80, Math.min(e.clientX - offX, maxX)) + 'px';
            win.style.top = Math.max(0, Math.min(e.clientY - offY, maxY)) + 'px';
        }
        function onUp() {
            if (!dragging && !resizing) return;
            dragging = false;
            resizing = null;
            hideDragShield();
            document.body.classList.remove('dez-interacting');
        }
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
        // A drag that ends outside the browser window (or is cancelled by
        // the OS / a context menu) never fires mouseup on the document.
        window.addEventListener('blur', onUp);

        // Focus (z-order) management
        win.addEventListener('mousedown', function () { self.focus(); });

        self.el = win;
        self.body = body;
        self.id = opts.id || null;
        self.setTitle = function (text) { title.textContent = text; };
        self.focus = function () {
            windowZ += 1;
            win.style.zIndex = windowZ;
            document.querySelectorAll('.dez-window').forEach(function (w) { w.classList.remove('focused'); });
            win.classList.add('focused');
        };
        self.hide = function () {
            win.style.display = 'none';
        };
        self.show = function () {
            win.style.display = '';
        };
        self.isHidden = function () {
            return win.style.display === 'none';
        };
        self.showCloseConfirm = function () {
            if (win.querySelector('.dez-window-confirm')) return;
            const pop = document.createElement('div');
            pop.className = 'dez-window-confirm';
            const msg = document.createElement('span');
            msg.textContent = opts.confirmClose;
            const row = document.createElement('div');
            row.className = 'dez-window-confirm-row';
            const cancel = document.createElement('button');
            cancel.className = 'ui mini basic button';
            cancel.textContent = 'Cancel';
            cancel.addEventListener('click', function () { pop.remove(); });
            const confirm = document.createElement('button');
            confirm.className = 'ui mini red button';
            confirm.textContent = 'Close';
            confirm.addEventListener('click', function () { self.close(); });
            row.appendChild(cancel);
            row.appendChild(confirm);
            pop.appendChild(msg);
            pop.appendChild(row);
            win.appendChild(pop);
        };
        self.close = function () {
            onUp();
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup', onUp);
            window.removeEventListener('blur', onUp);
            win.remove();
            if (self.id) delete dezWindows[self.id];
            if (typeof opts.onClose === 'function') {
                try { opts.onClose(); } catch (e) { console.error(e); }
            }
        };

        if (opts.id) dezWindows[opts.id] = self;
        self.focus();
        return self;
    }

    DezWindow.get = function (id) { return dezWindows[id] || null; };
    DezWindow.isOpen = function (id) { return !!dezWindows[id]; };
    // Hidden (minimized) windows don't count as open for focus routing
    DezWindow.anyOpen = function () {
        return Object.keys(dezWindows).some(function (id) { return !dezWindows[id].isHidden(); });
    };
    DezWindow.closeAll = function () {
        Object.keys(dezWindows).forEach(function (id) { dezWindows[id].close(); });
    };

    window.DezWindow = DezWindow;

    /* ================================================== dark theme */

    // Theme preference is shared across every DezKVM page via localStorage.
    // Each page applies it on load (dez-ui.js is included everywhere) and
    // the shell's toggle re-applies it recursively into open iframes.
    function dezApplyTheme() {
        const dark = localStorage.getItem('dezkvm.theme') === 'dark';
        if (document.body) {
            document.body.classList.toggle('dez-dark', dark);
        }
        document.querySelectorAll('iframe').forEach(function (frame) {
            try {
                if (frame.contentWindow && typeof frame.contentWindow.dezApplyTheme === 'function') {
                    frame.contentWindow.dezApplyTheme();
                }
            } catch (e) { /* cross-origin or not loaded yet */ }
        });
    }
    window.dezApplyTheme = dezApplyTheme;

    if (document.body) {
        dezApplyTheme();
    } else {
        document.addEventListener('DOMContentLoaded', dezApplyTheme);
    }

})(window.jQuery);
