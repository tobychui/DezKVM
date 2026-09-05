/*
    file-transfer.js

    Behaviour for the File Transfer tool (tools/file-transfer.html).

    The tool browses the KVM unit's USB mass-storage drive while it is switched
    to the KVM side. When the drive is handed to the remote computer the KVM
    can no longer read it, so the whole browser goes into a "disconnected"
    state instead of showing a stale listing -- see applyStorageState().

    Endpoints (all under /api/v1/storage/{uuid}):
        GET    /files?path=          list a directory
        POST   /files                create   {path, type:"file"|"dir"}
        DELETE /files?path=          delete
        POST   /upload               multipart {path, files[]}
        POST   /rename               {path, new_name}
        POST   /move                 {src, dst}
        POST   /copy                 {src, dst}
        GET    /download?path=       download a file
        GET    /volume               capacity + filesystem info
        GET    /thumb?path=          cached preview image (see THUMB_URL)
        POST   /unmount              flush and unmount the drive
*/
(function () {
    'use strict';

    /* ------------------------------------------------------------ config */

    const params = new URLSearchParams(window.location.search);
    const UUID = params.get('uuid') || '';
    const API = '/api/v1/storage/' + UUID;
    const CSRF = (document.querySelector('meta[name="dezkvm.csrf.token"]') || {}).content || '';

    // Directory entries created by the host OS. They are hidden from the
    // sidebar shortcuts (the listing itself still shows them) so the
    // shortcuts read as the user's own folders.
    const OS_FOLDERS = [
        'system volume information', '$recycle.bin', 'recycler', 'recycled',
        '.trashes', '.trash', '.trash-1000', '.spotlight-v100', '.fseventsd',
        '.temporaryitems', '.documentrevisions-v100', '.apdisk', 'found.000',
        'lost+found', '.metadata', 'msocache', 'system~1'
    ];
    function isOsFolder(name) {
        const n = String(name).toLowerCase();
        return OS_FOLDERS.indexOf(n) !== -1 || /^\.trash-\d+$/.test(n) || /^found\.\d+$/.test(n);
    }

    /* ------------------------------------------------------------- state */

    const state = {
        path: '/',
        entries: [],          // raw listing of the current directory
        selection: {},        // name -> true
        view: localStorage.getItem('dezkvm.ft.view') === 'grid' ? 'grid' : 'list',
        sort: { key: localStorage.getItem('dezkvm.ft.sortkey') || 'name',
                dir: localStorage.getItem('dezkvm.ft.sortdir') === 'desc' ? 'desc' : 'asc' },
        filter: '',           // active search term
        history: [],          // visited paths
        histIndex: -1,
        storageUuid: '',      // mass-storage UUID, shown in the path bar
        side: 'unknown',      // 'kvm' | 'remote' | 'unknown'
        browsable: false,
        devicePath: ''
    };

    /* ------------------------------------------------------------ icons */
    // Small inline SVG set for the chrome. Keeping them here avoids ~15 extra
    // requests on a device that is already streaming video.
    const ICON = {
        back:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 12H5M11 18l-6-6 6-6"/></svg>',
        forward: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6"/></svg>',
        up:      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M6 11l6-6 6 6"/></svg>',
        refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6"/></svg>',
        upload:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16V4M7 9l5-5 5 5M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg>',
        download:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v12M7 11l5 5 5-5M4 19h16"/></svg>',
        newfolder:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/><path d="M12 11v5M9.5 13.5h5"/></svg>',
        newfile: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z"/><path d="M14 3v5h5M12 12v5M9.5 14.5h5"/></svg>',
        more:    '<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="19" cy="12" r="1.7"/></svg>',
        list:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></svg>',
        grid:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/></svg>',
        trash:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13"/></svg>',
        move:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/><path d="M9 13h6M13 10.5l2.5 2.5-2.5 2.5"/></svg>',
        copy:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></svg>',
        rename:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4L19 9a2.1 2.1 0 0 0-3-3L5 17v3Z"/><path d="M14.5 7.5 16.5 9.5"/></svg>',
        search:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="6.5"/><path d="m16 16 4.5 4.5"/></svg>',
        eject:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M12 5 5 14h14L12 5Z"/><path d="M5 18h14" stroke-linecap="round"/></svg>',
        info:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 7.8h.01"/></svg>',
        selectall:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="3.5" width="17" height="17" rx="3"/><path d="m8 12 3 3 5-6"/></svg>',
        home:    '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m4 10 8-6 8 6v9a1 1 0 0 1-1 1h-4v-6H9v6H5a1 1 0 0 1-1-1v-9Z"/></svg>',
        empty:   '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/></svg>',
        unplug:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 3v5M11 3v5M6 8h6v3a3 3 0 0 1-3 3 3 3 0 0 1-3-3V8ZM9 14v3"/><path d="m15 12 6 6M21 12l-6 6"/></svg>',
        warn:    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4 2.5 20h19L12 4Z"/><path d="M12 10v4M12 17h.01"/></svg>'
    };
    /* ----------------------------------------------------------- helpers */

    function esc(s) {
        return String(s == null ? '' : s)
            .replace(/&/g, '&amp;').replace(/</g, '&lt;')
            .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    function csrfHeader() { return { 'dezkvm_csrf_token': CSRF }; }

    function fmtSize(bytes) {
        if (bytes == null) return '';
        if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB';
        if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB';
        if (bytes >= 1024) return (bytes / 1024).toFixed(0) + ' KB';
        return bytes + ' B';
    }

    function fmtDate(iso) {
        if (!iso) return '';
        const d = new Date(iso);
        if (isNaN(d.getTime())) return '';
        const pad = function (n) { return n < 10 ? '0' + n : String(n); };
        return d.getFullYear() + '/' + pad(d.getMonth() + 1) + '/' + pad(d.getDate()) +
            ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
    }

    function joinPath(dir, name) {
        return (dir === '/' ? '' : dir.replace(/\/+$/, '')) + '/' + name;
    }

    function parentPath(p) {
        const parts = p.replace(/\/+$/, '').split('/').filter(Boolean);
        parts.pop();
        return parts.length ? '/' + parts.join('/') : '/';
    }

    function normPath(p) {
        if (!p) return '/';
        p = String(p).replace(/\\/g, '/').replace(/\/+/g, '/');
        if (p[0] !== '/') p = '/' + p;
        if (p.length > 1) p = p.replace(/\/+$/, '');
        return p || '/';
    }

    function toast(msg, cls) {
        if (window.$ && $.toast) $.toast({ message: msg, class: cls || '', duration: 3200 });
    }

    function shortDeviceId() {
        // The path bar shows the mass-storage UUID in place of a drive letter.
        // Full UUIDs are long, so only the leading group is displayed and the
        // full value stays in the tooltip.
        const u = state.storageUuid || UUID;
        if (!u) return 'USB';
        return u.length > 12 ? u.slice(0, 8) : u;
    }

    // Server-side thumbnail. The backend caches renders under
    // thumb/{storage_uuid}/{relative_path}.jpg; entries with no renderable
    // preview answer 404 and the tile falls back to the extension icon.
    //
    // The response is cacheable for an hour, so the file's mtime rides along
    // as a cache key -- a file replaced in place then gets a fresh URL rather
    // than the previous file's preview.
    function thumbUrl(entryPath, modTime) {
        return API + '/thumb?path=' + encodeURIComponent(entryPath) +
            (modTime ? '&v=' + encodeURIComponent(modTime) : '');
    }

    const $ = window.jQuery;
    const el = function (id) { return document.getElementById(id); };

    /* --------------------------------------------------------- dropdowns */

    let openMenu = null;

    function closeMenu() {
        if (openMenu) { openMenu.remove(); openMenu = null; }
    }

    // items: [{ label, icon, danger, disabled, onClick }] or { sep: true }
    function showMenu(anchorEl, items, align) {
        closeMenu();
        const menu = document.createElement('div');
        menu.className = 'ft-menu';
        items.forEach(function (it) {
            if (it.sep) {
                const sep = document.createElement('div');
                sep.className = 'ft-menu-sep';
                menu.appendChild(sep);
                return;
            }
            const btn = document.createElement('button');
            btn.className = 'ft-menu-item' + (it.danger ? ' danger' : '');
            btn.disabled = !!it.disabled;
            btn.innerHTML = (it.icon ? ICON[it.icon] : '<span style="width:15px"></span>') +
                '<span>' + esc(it.label) + '</span>';
            btn.addEventListener('click', function () {
                closeMenu();
                if (it.onClick) it.onClick();
            });
            menu.appendChild(btn);
        });
        document.body.appendChild(menu);

        const r = anchorEl.getBoundingClientRect();
        const w = menu.offsetWidth;
        let left = align === 'right' ? r.right - w : r.left;
        left = Math.max(6, Math.min(left, window.innerWidth - w - 6));
        let top = r.bottom + 5;
        if (top + menu.offsetHeight > window.innerHeight - 6) {
            top = Math.max(6, r.top - menu.offsetHeight - 5);
        }
        menu.style.left = left + 'px';
        menu.style.top = top + 'px';
        openMenu = menu;
    }

    document.addEventListener('mousedown', function (e) {
        if (openMenu && !e.target.closest('.ft-menu')) closeMenu();
    });
    window.addEventListener('blur', closeMenu);

    /* ---------------------------------------------------- storage state */

    // Reads the instance list to learn which side owns the USB drive. The
    // File Transfer window can be left open across a side switch, so this is
    // re-run on a timer and whenever the shell notifies us.
    function refreshStorageState(then) {
        $.ajax({
            url: '/api/v1/instances',
            method: 'GET',
            success: function (list) {
                if (!Array.isArray(list)) list = [];
                const inst = list.find(function (i) { return i.uuid === UUID; });
                if (inst) {
                    state.storageUuid = inst.storage_uuid || '';
                    state.devicePath = inst.storage_device || '';
                    state.side = inst.usb_mass_storage_side === 2 ? 'remote' : 'kvm';
                    state.browsable = !!inst.storage_browsable;
                } else {
                    state.side = 'unknown';
                    state.browsable = false;
                }
                applyStorageState();
                if (then) then();
            },
            error: function () {
                state.side = 'unknown';
                state.browsable = false;
                applyStorageState();
                if (then) then();
            }
        });
    }

    function storageAvailable() {
        return state.side === 'kvm' && state.browsable;
    }

    // Mirrors the storage side into every part of the UI: the side chip, the
    // banner, and whether the file operations are usable at all.
    function applyStorageState() {
        const chip = el('ft-sidechip');
        chip.classList.remove('kvm', 'remote');
        if (state.side === 'kvm') {
            chip.classList.add('kvm');
            el('ft-sidelabel').textContent = 'KVM Side';
        } else if (state.side === 'remote') {
            chip.classList.add('remote');
            el('ft-sidelabel').textContent = 'Remote Side';
        } else {
            el('ft-sidelabel').textContent = 'Unknown';
        }

        const banner = el('ft-banner');
        const available = storageAvailable();

        if (state.side === 'remote') {
            banner.className = 'ft-banner warn shown';
            banner.querySelector('.ft-banner-icon').innerHTML = ICON.unplug;
            banner.querySelector('.ft-banner-text').innerHTML =
                '<b>Storage disconnected — the USB drive is attached to the remote computer.</b>' +
                '<span>The files are not readable from the KVM side. Switch the USB back to the KVM side to browse them here.</span>';
            el('ft-banner-action').textContent = 'Switch to KVM Side';
            el('ft-banner-action').style.display = '';
        } else if (state.side === 'kvm' && !state.browsable) {
            banner.className = 'ft-banner warn shown';
            banner.querySelector('.ft-banner-icon').innerHTML = ICON.warn;
            banner.querySelector('.ft-banner-text').innerHTML =
                '<b>Storage not browsable.</b>' +
                '<span>The drive is on the KVM side but could not be mounted — it may be unformatted, unplugged, or using an unsupported filesystem.</span>';
            el('ft-banner-action').style.display = 'none';
        } else {
            banner.className = 'ft-banner';
        }

        // Everything that writes to (or reads from) the drive is pointless
        // while it belongs to the remote side.
        document.querySelectorAll('[data-needs-storage]').forEach(function (b) {
            b.disabled = !available;
        });
        el('ft-pathinput').disabled = !available;

        if (!available) {
            state.entries = [];
            state.selection = {};
            renderSidebar();
            renderEntries();
            setStatus(state.side === 'remote' ? 'Storage disconnected' : 'Storage unavailable');
        }
    }

    /* ------------------------------------------------------- navigation */

    function navigate(path, fromHistory) {
        path = normPath(path);
        if (!fromHistory) {
            // Drop the forward tail when navigating from a mid-history point
            state.history = state.history.slice(0, state.histIndex + 1);
            if (state.history[state.histIndex] !== path) {
                state.history.push(path);
                state.histIndex = state.history.length - 1;
            }
        }
        state.path = path;
        state.selection = {};
        state.filter = '';
        el('ft-search-wrap').style.display = 'none';
        updatePathBar();
        updateNavButtons();
        loadDir();
    }

    function goBack() {
        if (state.histIndex <= 0) return;
        state.histIndex -= 1;
        navigate(state.history[state.histIndex], true);
    }

    function goForward() {
        if (state.histIndex >= state.history.length - 1) return;
        state.histIndex += 1;
        navigate(state.history[state.histIndex], true);
    }

    function goUp() {
        if (state.path === '/') return;
        navigate(parentPath(state.path));
    }

    function updateNavButtons() {
        el('ft-back').disabled = state.histIndex <= 0;
        el('ft-forward').disabled = state.histIndex >= state.history.length - 1;
        el('ft-up').disabled = state.path === '/';
    }

    function updatePathBar() {
        const input = el('ft-pathinput');
        if (document.activeElement === input) return; // don't fight the user
        input.value = shortDeviceId() + ':' + state.path;
        input.title = (state.storageUuid || UUID) + ':' + state.path;
    }

    function commitPathBar() {
        const raw = el('ft-pathinput').value.trim();
        // Accept "UUID:/path", "/path" and "path" alike
        const colon = raw.indexOf(':');
        const path = colon >= 0 ? raw.slice(colon + 1) : raw;
        navigate(path || '/');
    }

    /* --------------------------------------------------------- listing */

    function setStatus(text) { el('ft-status').textContent = text; }

    function loadDir() {
        if (!UUID) { setStatus('No device UUID.'); return; }
        if (!storageAvailable()) return;
        setStatus('Loading…');
        $.ajax({
            url: API + '/files',
            method: 'GET',
            data: { path: state.path },
            success: function (entries) {
                state.entries = Array.isArray(entries) ? entries : [];
                state.selection = {};
                renderEntries();
                renderSidebar();
            },
            error: function (xhr) {
                state.entries = [];
                renderEntries();
                // A directory read failing right after a side switch usually
                // means the drive went away; re-check rather than guess.
                if (xhr.status === 503) { refreshStorageState(); return; }
                setStatus('Failed to list directory');
                toast('Failed to list directory: ' + (xhr.responseText || xhr.status), 'error');
            }
        });
    }

    window.ftRefresh = function () { refreshStorageState(function () { loadDir(); }); };

    /* --------------------------------------------------------- sorting */

    function sortedEntries() {
        const key = state.sort.key;
        const mul = state.sort.dir === 'desc' ? -1 : 1;
        let list = state.entries.slice();

        if (state.filter) {
            const f = state.filter.toLowerCase();
            list = list.filter(function (e) { return e.name.toLowerCase().indexOf(f) !== -1; });
        }

        list.sort(function (a, b) {
            // Folders always lead, in both sort directions -- a listing where
            // the folders sink to the bottom is much harder to scan.
            if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
            let r = 0;
            if (key === 'size') r = (a.is_dir ? -1 : a.size) - (b.is_dir ? -1 : b.size);
            else if (key === 'date') r = new Date(a.mod_time) - new Date(b.mod_time);
            else if (key === 'type') {
                r = DezFileIcons.typeOf(a).label.localeCompare(DezFileIcons.typeOf(b).label);
                if (r === 0) r = a.name.localeCompare(b.name, undefined, { numeric: true });
            } else r = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
            return r * mul;
        });
        return list;
    }

    function setSort(key) {
        if (state.sort.key === key) {
            state.sort.dir = state.sort.dir === 'asc' ? 'desc' : 'asc';
        } else {
            state.sort.key = key;
            // Dates and sizes are most useful largest/newest-first on the
            // first click; names read better A-Z.
            state.sort.dir = (key === 'date' || key === 'size') ? 'desc' : 'asc';
        }
        localStorage.setItem('dezkvm.ft.sortkey', state.sort.key);
        localStorage.setItem('dezkvm.ft.sortdir', state.sort.dir);
        renderEntries();
    }

    function renderSortHeaders() {
        document.querySelectorAll('#ft-table thead th.sortable').forEach(function (th) {
            const active = th.dataset.sort === state.sort.key;
            th.classList.toggle('sorted', active);
            th.querySelector('.ft-sort-arrow').textContent = active
                ? (state.sort.dir === 'asc' ? '▲' : '▼') : '';
        });
    }

    /* --------------------------------------------------------- sidebar */

    function renderSidebar() {
        const wrap = el('ft-sidebar');
        wrap.innerHTML = '';

        const root = document.createElement('button');
        root.className = 'ft-shortcut';
        root.dataset.path = '/';
        root.innerHTML = ICON.home + '<span class="ft-sc-label">Root</span>';
        root.addEventListener('click', function () { navigate('/'); });
        wrap.appendChild(root);

        if (!storageAvailable()) { markActiveShortcut(); return; }

        // Shortcuts are the user's own folders at the root of the mount point.
        const source = state.path === '/' ? Promise.resolve(state.entries) : fetchRoot();
        Promise.resolve(source).then(function (entries) {
            (entries || [])
                .filter(function (e) { return e.is_dir && !isOsFolder(e.name) && e.name[0] !== '.'; })
                .sort(function (a, b) { return a.name.localeCompare(b.name, undefined, { numeric: true }); })
                .forEach(function (e) {
                    const b = document.createElement('button');
                    b.className = 'ft-shortcut';
                    b.dataset.path = '/' + e.name;
                    b.title = e.name;
                    b.innerHTML = DezFileIcons.svg({ is_dir: true }, 16) +
                        '<span class="ft-sc-label">' + esc(e.name) + '</span>';
                    b.addEventListener('click', function () { navigate('/' + e.name); });
                    wrap.appendChild(b);
                });
            markActiveShortcut();
        });
    }

    let rootCache = null;
    function fetchRoot() {
        if (rootCache) return rootCache;
        rootCache = $.ajax({ url: API + '/files', method: 'GET', data: { path: '/' } })
            .then(function (r) { return Array.isArray(r) ? r : []; }, function () { return []; });
        return rootCache;
    }

    function markActiveShortcut() {
        document.querySelectorAll('.ft-shortcut').forEach(function (b) {
            const p = b.dataset.path;
            b.classList.toggle('active', p === '/' ? state.path === '/' :
                (state.path === p || state.path.indexOf(p + '/') === 0));
        });
    }

    /* -------------------------------------------------------- rendering */

    function renderEntries() {
        const list = sortedEntries();
        renderSortHeaders();

        el('ft-table-wrap').style.display = state.view === 'list' ? '' : 'none';
        el('ft-grid').style.display = state.view === 'grid' ? '' : 'none';

        if (state.view === 'list') renderList(list); else renderGrid(list);

        const empty = el('ft-empty');
        const disconnected = !storageAvailable();
        empty.classList.toggle('shown', list.length === 0);
        if (list.length === 0) {
            empty.querySelector('.ft-empty-icon').innerHTML = disconnected ? ICON.unplug : ICON.empty;
            empty.querySelector('.ft-empty-text').textContent = disconnected
                ? 'No storage connected.'
                : (state.filter ? 'No files match “' + state.filter + '”.' : 'This folder is empty.');
        }

        updateSelectionUi();
        if (storageAvailable()) {
            const total = state.entries.length;
            setStatus(state.filter
                ? list.length + ' of ' + total + ' item' + (total !== 1 ? 's' : '')
                : total + ' item' + (total !== 1 ? 's' : ''));
        }
    }

    function renderList(list) {
        const tbody = el('ft-tbody');
        tbody.innerHTML = '';
        const frag = document.createDocumentFragment();

        list.forEach(function (e) {
            const t = DezFileIcons.typeOf(e);
            const entryPath = joinPath(state.path, e.name);
            const tr = document.createElement('tr');
            tr.dataset.name = e.name;
            if (state.selection[e.name]) tr.classList.add('selected');

            const tdName = document.createElement('td');
            tdName.innerHTML = '<span class="ft-cell-name">' + DezFileIcons.svg(e, 20) +
                '<span class="ft-name-text">' + esc(e.name) + '</span></span>';
            tr.appendChild(tdName);

            const tdSize = document.createElement('td');
            tdSize.className = 'ft-col-size';
            tdSize.textContent = e.is_dir ? '—' : fmtSize(e.size);
            tr.appendChild(tdSize);

            const tdDate = document.createElement('td');
            tdDate.className = 'ft-col-date';
            tdDate.textContent = fmtDate(e.mod_time);
            tr.appendChild(tdDate);

            const tdType = document.createElement('td');
            tdType.className = 'ft-col-type';
            tdType.textContent = t.label;
            tr.appendChild(tdType);

            const tdAct = document.createElement('td');
            tdAct.className = 'ft-col-act';
            if (!e.is_dir) {
                const dl = document.createElement('a');
                dl.className = 'ft-rowbtn';
                dl.title = 'Download';
                dl.innerHTML = ICON.download;
                dl.href = API + '/download?path=' + encodeURIComponent(entryPath);
                dl.setAttribute('download', e.name);
                tdAct.appendChild(dl);
            }
            const mb = document.createElement('button');
            mb.className = 'ft-rowbtn';
            mb.title = 'More';
            mb.innerHTML = ICON.more;
            mb.addEventListener('click', function (ev) {
                ev.stopPropagation();
                select(e.name, false, false);
                showMenu(mb, entryMenu(e), 'right');
            });
            tdAct.appendChild(mb);
            tr.appendChild(tdAct);

            wireEntryEvents(tr, e);
            frag.appendChild(tr);
        });
        tbody.appendChild(frag);
    }

    /* Thumbnail loading.

       A folder of photos would otherwise fire one request per tile the moment
       the grid renders, and every miss costs the KVM a full decode+resize. So
       previews are only requested once a tile scrolls into view, and at most
       MAX_INFLIGHT are decoding at any time. */
    const thumbQueue = (function () {
        const MAX_INFLIGHT = 3;
        const pending = [];      // [{ node, path }] visible, not yet started
        let inflight = 0;
        let observer = null;

        function ensureObserver() {
            if (observer || typeof IntersectionObserver === 'undefined') return;
            observer = new IntersectionObserver(function (records) {
                records.forEach(function (rec) {
                    if (!rec.isIntersecting) return;
                    observer.unobserve(rec.target);
                    pending.push({ node: rec.target, url: rec.target.dataset.thumbUrl });
                    pump();
                });
            }, { root: el('ft-scroll'), rootMargin: '150px' });
        }

        function pump() {
            while (inflight < MAX_INFLIGHT && pending.length) {
                const job = pending.shift();
                // The grid may have been re-rendered since the tile was queued
                if (!job.node.isConnected) continue;
                inflight++;
                const img = new Image();
                img.decoding = 'async';
                img.alt = '';
                img.onload = function () {
                    inflight--;
                    // Swap the icon for the real preview only once it has
                    // decoded, so a slow render never leaves an empty tile.
                    if (job.node.isConnected) {
                        job.node.innerHTML = '';
                        job.node.appendChild(img);
                    }
                    pump();
                };
                // A miss (no renderer for this type, unreadable file, drive
                // switched away) just leaves the extension icon in place.
                img.onerror = function () { inflight--; pump(); };
                img.src = job.url;
            }
        }

        return {
            observe: function (node, url) {
                node.dataset.thumbUrl = url;
                ensureObserver();
                if (observer) observer.observe(node);
                else { pending.push({ node: node, url: url }); pump(); }
            },
            reset: function () {
                pending.length = 0;
                if (observer) { observer.disconnect(); observer = null; }
            }
        };
    })();

    function renderGrid(list) {
        const grid = el('ft-grid');
        thumbQueue.reset();
        grid.innerHTML = '';
        const frag = document.createDocumentFragment();

        list.forEach(function (e) {
            const t = DezFileIcons.typeOf(e);
            const entryPath = joinPath(state.path, e.name);
            const tile = document.createElement('div');
            tile.className = 'ft-tile' + (state.selection[e.name] ? ' selected' : '');
            tile.dataset.name = e.name;
            tile.title = e.name;

            const thumb = document.createElement('div');
            thumb.className = 'ft-tile-thumb';
            thumb.innerHTML = DezFileIcons.svg(e, 46);

            if (t.thumbable) thumbQueue.observe(thumb, thumbUrl(entryPath, e.mod_time));
            tile.appendChild(thumb);

            const name = document.createElement('div');
            name.className = 'ft-tile-name';
            name.textContent = e.name;
            tile.appendChild(name);

            const sub = document.createElement('div');
            sub.className = 'ft-tile-sub';
            sub.textContent = e.is_dir ? 'Folder' : fmtSize(e.size);
            tile.appendChild(sub);

            wireEntryEvents(tile, e);
            frag.appendChild(tile);
        });
        grid.appendChild(frag);
    }

    function wireEntryEvents(node, e) {
        node.addEventListener('click', function (ev) {
            if (ev.target.closest('.ft-rowbtn')) return;
            select(e.name, ev.ctrlKey || ev.metaKey, ev.shiftKey);
        });
        node.addEventListener('dblclick', function (ev) {
            if (ev.target.closest('.ft-rowbtn')) return;
            open(e);
        });
        node.addEventListener('contextmenu', function (ev) {
            ev.preventDefault();
            if (!state.selection[e.name]) select(e.name, false, false);
            showMenu({ getBoundingClientRect: function () {
                return { left: ev.clientX, right: ev.clientX, top: ev.clientY, bottom: ev.clientY };
            } }, entryMenu(e));
        });
    }

    function open(e) {
        const entryPath = joinPath(state.path, e.name);
        if (e.is_dir) navigate(entryPath);
        else window.open(API + '/download?path=' + encodeURIComponent(entryPath), '_blank');
    }

    /* -------------------------------------------------------- selection */

    let lastClicked = null;

    function select(name, additive, range) {
        if (range && lastClicked) {
            const names = sortedEntries().map(function (e) { return e.name; });
            const a = names.indexOf(lastClicked), b = names.indexOf(name);
            if (a !== -1 && b !== -1) {
                if (!additive) state.selection = {};
                names.slice(Math.min(a, b), Math.max(a, b) + 1).forEach(function (n) {
                    state.selection[n] = true;
                });
            }
        } else if (additive) {
            if (state.selection[name]) delete state.selection[name];
            else state.selection[name] = true;
            lastClicked = name;
        } else {
            state.selection = {};
            state.selection[name] = true;
            lastClicked = name;
        }
        refreshSelectionClasses();
        updateSelectionUi();
    }

    function selectedNames() { return Object.keys(state.selection); }

    function selectedEntries() {
        const sel = state.selection;
        return state.entries.filter(function (e) { return sel[e.name]; });
    }

    function refreshSelectionClasses() {
        document.querySelectorAll('#ft-tbody tr, #ft-grid .ft-tile').forEach(function (n) {
            n.classList.toggle('selected', !!state.selection[n.dataset.name]);
        });
    }

    function updateSelectionUi() {
        const sel = selectedEntries();
        const n = sel.length;
        el('ft-selcount').textContent = n === 0
            ? ''
            : n + ' item' + (n !== 1 ? 's' : '') + ' selected';

        const off = n === 0 || !storageAvailable();
        ['ft-op-delete', 'ft-op-move', 'ft-op-copy', 'ft-op-more'].forEach(function (id) {
            el(id).disabled = off;
        });
        // There is no archive endpoint, so a folder on its own has nothing to
        // download; the button needs at least one plain file.
        el('ft-op-download').disabled = off || !sel.some(function (e) { return !e.is_dir; });
    }

    /* -------------------------------------------------- file operations */

    function entryMenu(e) {
        const entryPath = joinPath(state.path, e.name);
        return [
            { label: e.is_dir ? 'Open' : 'Download', icon: e.is_dir ? 'forward' : 'download',
              onClick: function () { open(e); } },
            { sep: true },
            { label: 'Rename…', icon: 'rename', onClick: function () { promptRename(e); } },
            { label: 'Move to…', icon: 'move', onClick: function () { promptTransfer('move'); } },
            { label: 'Copy to…', icon: 'copy', onClick: function () { promptTransfer('copy'); } },
            { sep: true },
            { label: 'Delete', icon: 'trash', danger: true,
              onClick: function () { confirmDelete([{ name: e.name, path: entryPath, is_dir: e.is_dir }]); } }
        ];
    }

    function downloadSelected() {
        // Each file is fetched through its own hidden anchor; the backend has
        // no archive endpoint, so a multi-select becomes several downloads.
        const files = selectedEntries().filter(function (e) { return !e.is_dir; });
        if (files.length === 0) { toast('Select at least one file to download'); return; }
        files.forEach(function (e, i) {
            setTimeout(function () {
                const a = document.createElement('a');
                a.href = API + '/download?path=' + encodeURIComponent(joinPath(state.path, e.name));
                a.download = e.name;
                document.body.appendChild(a);
                a.click();
                a.remove();
            }, i * 350);
        });
    }

    function confirmDelete(items) {
        if (!items.length) return;
        const label = items.length === 1
            ? '“' + items[0].name + '”'
            : items.length + ' items';
        el('ft-delete-msg').textContent = 'Delete ' + label + ' from the USB drive?';
        $('#ft-modal-delete').modal({
            onApprove: function () { runDelete(items.slice()); }
        }).modal('show');
    }

    function runDelete(items) {
        let done = 0, failed = 0;
        const next = function () {
            if (!items.length) {
                if (failed) toast(failed + ' item(s) could not be deleted', 'error');
                else toast('Deleted ' + done + ' item' + (done !== 1 ? 's' : ''));
                invalidateRoot();
                loadDir();
                return;
            }
            const it = items.shift();
            $.ajax({
                url: API + '/files?path=' + encodeURIComponent(it.path),
                method: 'DELETE',
                headers: csrfHeader(),
                complete: function (xhr) {
                    if (xhr.status >= 200 && xhr.status < 300) done++; else failed++;
                    next();
                }
            });
        };
        next();
    }

    function promptRename(e) {
        el('ft-rename-input').value = e.name;
        $('#ft-modal-rename').modal({
            onApprove: function () {
                const v = el('ft-rename-input').value.trim();
                if (!v || v === e.name) return true;
                $.ajax({
                    url: API + '/rename',
                    method: 'POST',
                    contentType: 'application/json',
                    headers: csrfHeader(),
                    data: JSON.stringify({ path: joinPath(state.path, e.name), new_name: v }),
                    success: function () { toast('Renamed to ' + esc(v)); invalidateRoot(); loadDir(); },
                    error: function (xhr) { toast('Rename failed: ' + (xhr.responseText || xhr.status), 'error'); }
                });
            }
        }).modal('show');
        setTimeout(function () { el('ft-rename-input').focus(); el('ft-rename-input').select(); }, 150);
    }

    // Destination picker shared by Move and Copy.
    let pickerPath = '/';
    let pickerMode = 'move';

    function promptTransfer(mode) {
        if (!selectedNames().length) return;
        pickerMode = mode;
        el('ft-picker-title').textContent = mode === 'move' ? 'Move to folder' : 'Copy to folder';
        el('ft-picker-confirm').textContent = mode === 'move' ? 'Move here' : 'Copy here';
        loadPicker('/');
        $('#ft-modal-picker').modal({
            onApprove: function () { runTransfer(pickerMode, pickerPath); }
        }).modal('show');
    }

    function loadPicker(path) {
        pickerPath = normPath(path);
        el('ft-picker-path').textContent = shortDeviceId() + ':' + pickerPath;
        const list = el('ft-picker-list');
        list.innerHTML = '<div style="padding:10px;color:var(--dez-text-sub)">Loading…</div>';
        $.ajax({
            url: API + '/files', method: 'GET', data: { path: pickerPath },
            success: function (entries) {
                list.innerHTML = '';
                if (pickerPath !== '/') {
                    const up = document.createElement('button');
                    up.className = 'ft-picker-row';
                    up.innerHTML = ICON.up + '<span>..</span>';
                    up.addEventListener('click', function () { loadPicker(parentPath(pickerPath)); });
                    list.appendChild(up);
                }
                const dirs = (entries || []).filter(function (e) { return e.is_dir; });
                dirs.sort(function (a, b) { return a.name.localeCompare(b.name, undefined, { numeric: true }); });
                dirs.forEach(function (e) {
                    const row = document.createElement('button');
                    row.className = 'ft-picker-row';
                    row.innerHTML = DezFileIcons.svg({ is_dir: true }, 17) + '<span>' + esc(e.name) + '</span>';
                    row.addEventListener('click', function () { loadPicker(joinPath(pickerPath, e.name)); });
                    list.appendChild(row);
                });
                if (!dirs.length && pickerPath === '/') {
                    list.innerHTML = '<div style="padding:10px;color:var(--dez-text-sub)">No sub-folders.</div>';
                }
            },
            error: function () {
                list.innerHTML = '<div style="padding:10px;color:var(--dez-red)">Failed to read folder.</div>';
            }
        });
    }

    function runTransfer(mode, dstDir) {
        const items = selectedEntries().map(function (e) {
            return { name: e.name, src: joinPath(state.path, e.name) };
        });
        if (!items.length) return;
        if (normPath(dstDir) === normPath(state.path) && mode === 'move') {
            toast('Source and destination are the same folder');
            return;
        }
        let failed = 0;
        const next = function () {
            if (!items.length) {
                if (failed) toast(failed + ' item(s) failed', 'error');
                else toast(mode === 'move' ? 'Moved' : 'Copied');
                invalidateRoot();
                loadDir();
                return;
            }
            const it = items.shift();
            $.ajax({
                url: API + '/' + mode,
                method: 'POST',
                contentType: 'application/json',
                headers: csrfHeader(),
                data: JSON.stringify({ src: it.src, dst: joinPath(dstDir, it.name) }),
                complete: function (xhr) {
                    if (!(xhr.status >= 200 && xhr.status < 300)) failed++;
                    next();
                }
            });
        };
        next();
    }

    function promptCreate(type) {
        const isDir = type === 'dir';
        el('ft-create-title').textContent = isDir ? 'New Folder' : 'New File';
        el('ft-create-input').value = '';
        el('ft-create-input').placeholder = isDir ? 'Folder name' : 'File name';
        $('#ft-modal-create').modal({
            onApprove: function () {
                const v = el('ft-create-input').value.trim();
                if (!v) return false;
                $.ajax({
                    url: API + '/files',
                    method: 'POST',
                    contentType: 'application/json',
                    headers: csrfHeader(),
                    data: JSON.stringify({ path: joinPath(state.path, v), type: type }),
                    success: function () { toast('Created ' + esc(v)); invalidateRoot(); loadDir(); },
                    error: function (xhr) { toast('Create failed: ' + (xhr.responseText || xhr.status), 'error'); }
                });
            }
        }).modal('show');
        setTimeout(function () { el('ft-create-input').focus(); }, 150);
    }

    // The sidebar shortcuts are built from the root listing, so any operation
    // that can add or remove a root folder has to drop the cache.
    function invalidateRoot() { rootCache = null; }

    /* ---------------------------------------------------------- upload */

    function uploadFiles(fileList) {
        if (!fileList || !fileList.length) return;
        if (!storageAvailable()) { toast('Storage is not connected to the KVM side', 'error'); return; }

        const fd = new FormData();
        fd.append('path', state.path);
        for (let i = 0; i < fileList.length; i++) fd.append('files', fileList[i]);

        const prog = el('ft-progress');
        prog.classList.add('shown');
        el('ft-progress-label').textContent = 'Uploading ' + fileList.length + ' file' + (fileList.length !== 1 ? 's' : '') + '…';
        el('ft-progress-bar').style.width = '0%';

        $.ajax({
            url: API + '/upload',
            method: 'POST',
            headers: csrfHeader(),
            data: fd,
            processData: false,
            contentType: false,
            xhr: function () {
                const x = $.ajaxSettings.xhr();
                if (x.upload) {
                    x.upload.addEventListener('progress', function (ev) {
                        if (!ev.lengthComputable) return;
                        el('ft-progress-bar').style.width = ((ev.loaded / ev.total) * 100).toFixed(1) + '%';
                    });
                }
                return x;
            },
            success: function (res) {
                prog.classList.remove('shown');
                const n = (res && res.uploaded) ? res.uploaded.length : fileList.length;
                toast('Uploaded ' + n + ' file' + (n !== 1 ? 's' : ''));
                invalidateRoot();
                loadDir();
            },
            error: function (xhr) {
                prog.classList.remove('shown');
                toast('Upload failed: ' + (xhr.responseText || xhr.status), 'error');
            }
        });
        el('ft-fileinput').value = '';
    }

    function initDropTarget() {
        const scroll = el('ft-scroll');
        const overlay = el('ft-dropoverlay');
        let depth = 0;

        // Only file drags from the OS should arm the overlay; dragging text or
        // a link around inside the page must not look like an upload.
        function isFileDrag(ev) {
            const dt = ev.dataTransfer;
            if (!dt) return false;
            if (dt.types) return Array.prototype.indexOf.call(dt.types, 'Files') !== -1;
            return false;
        }

        scroll.addEventListener('dragenter', function (ev) {
            if (!isFileDrag(ev) || !storageAvailable()) return;
            ev.preventDefault();
            depth++;
            overlay.classList.add('shown');
        });
        scroll.addEventListener('dragover', function (ev) {
            if (!isFileDrag(ev) || !storageAvailable()) return;
            ev.preventDefault();
            ev.dataTransfer.dropEffect = 'copy';
        });
        scroll.addEventListener('dragleave', function () {
            depth = Math.max(0, depth - 1);
            if (depth === 0) overlay.classList.remove('shown');
        });
        scroll.addEventListener('drop', function (ev) {
            if (!isFileDrag(ev)) return;
            ev.preventDefault();
            depth = 0;
            overlay.classList.remove('shown');
            if (!storageAvailable()) { toast('Storage is not connected to the KVM side', 'error'); return; }
            uploadFiles(ev.dataTransfer.files);
        });
    }

    /* ------------------------------------------------ device info / eject */

    function showDeviceInfo() {
        const body = el('ft-info-body');
        body.innerHTML = '<div style="padding:10px;color:var(--dez-text-sub)">Reading volume information…</div>';
        $('#ft-modal-info').modal({}).modal('show');

        $.ajax({
            url: API + '/volume',
            method: 'GET',
            success: function (v) { renderDeviceInfo(v); },
            error: function (xhr) {
                // /volume needs a mount, so it is expected to fail while the
                // drive is on the remote side -- still show what we do know.
                renderDeviceInfo(null, xhr.status === 503
                    ? 'The drive is not mounted on the KVM side.'
                    : ('Could not read volume information (' + xhr.status + ').'));
            }
        });
    }

    function renderDeviceInfo(v, note) {
        const rows = [];
        rows.push(['Storage UUID', state.storageUuid || '—']);
        rows.push(['Device node', state.devicePath || '—']);
        rows.push(['Attached to', state.side === 'remote' ? 'Remote computer' : (state.side === 'kvm' ? 'KVM host' : 'Unknown')]);
        rows.push(['Browsable', state.browsable ? 'Yes' : 'No']);

        let meter = '';
        if (v) {
            if (v.disk_model) rows.push(['Disk model', v.disk_model]);
            rows.push(['Filesystem', v.fs_type ? v.fs_type.toUpperCase() : 'Unknown']);
            rows.push(['Capacity', fmtSize(v.total_bytes)]);
            rows.push(['Used', fmtSize(v.used_bytes)]);
            rows.push(['Free', fmtSize(v.free_bytes)]);

            const pct = v.total_bytes ? (v.used_bytes / v.total_bytes) * 100 : 0;
            const cls = pct >= 95 ? 'full' : (pct >= 80 ? 'high' : '');
            meter = '<div style="margin-bottom:12px">' +
                '<div class="ft-meter ' + cls + '"><i style="width:' + pct.toFixed(1) + '%"></i></div>' +
                '<div style="font-size:12px;color:var(--dez-text-sub)">' +
                fmtSize(v.free_bytes) + ' free of ' + fmtSize(v.total_bytes) +
                ' (' + pct.toFixed(0) + '% used)</div></div>';

            if (v.categories) {
                const c = v.categories;
                rows.push(['Media files', fmtSize(c.media_bytes)]);
                rows.push(['Binaries', fmtSize(c.binary_bytes)]);
                rows.push(['Text files', fmtSize(c.text_bytes)]);
                rows.push(['Other', fmtSize(c.other_bytes)]);
            }
        }

        el('ft-info-body').innerHTML =
            (note ? '<div style="margin-bottom:10px;color:var(--dez-orange);font-size:12.5px">' + esc(note) + '</div>' : '') +
            meter +
            '<table class="ft-kv">' + rows.map(function (r) {
                return '<tr><td>' + esc(r[0]) + '</td><td>' + esc(r[1]) + '</td></tr>';
            }).join('') + '</table>';
    }

    function confirmUnmount() {
        $('#ft-modal-unmount').modal({
            onApprove: function () {
                $.ajax({
                    url: API + '/unmount',
                    method: 'POST',
                    headers: csrfHeader(),
                    success: function () {
                        toast('Drive unmounted — safe to switch or unplug');
                        refreshStorageState();
                    },
                    error: function (xhr) {
                        toast('Unmount failed: ' + (xhr.responseText || xhr.status), 'error');
                    }
                });
            }
        }).modal('show');
    }

    /* ------------------------------------------------------------ search */

    function toggleSearch() {
        const wrap = el('ft-search-wrap');
        const shown = wrap.style.display !== 'none';
        if (shown) {
            wrap.style.display = 'none';
            state.filter = '';
            renderEntries();
        } else {
            wrap.style.display = '';
            el('ft-search-input').value = state.filter;
            el('ft-search-input').focus();
        }
    }

    /* -------------------------------------------------------------- view */

    function setView(v) {
        state.view = v;
        localStorage.setItem('dezkvm.ft.view', v);
        el('ft-view-list').classList.toggle('active', v === 'list');
        el('ft-view-grid').classList.toggle('active', v === 'grid');
        renderEntries();
    }

    /* -------------------------------------------------------------- wire */

    function wire() {
        el('ft-back').innerHTML = ICON.back;
        el('ft-forward').innerHTML = ICON.forward;
        el('ft-up').innerHTML = ICON.up;
        el('ft-refresh').innerHTML = ICON.refresh;
        el('ft-view-list').innerHTML = ICON.list;
        el('ft-view-grid').innerHTML = ICON.grid;
        el('ft-btn-upload').innerHTML = ICON.upload + '<span>Upload</span>';
        el('ft-btn-newfolder').innerHTML = ICON.newfolder + '<span>New Folder</span>';
        el('ft-btn-more').innerHTML = ICON.more + '<span>More</span>';
        el('ft-op-download').innerHTML = ICON.download + '<span>Download</span>';
        el('ft-op-delete').innerHTML = ICON.trash + '<span>Delete</span>';
        el('ft-op-move').innerHTML = ICON.move + '<span>Move</span>';
        el('ft-op-copy').innerHTML = ICON.copy + '<span>Copy</span>';
        el('ft-op-more').innerHTML = ICON.more + '<span>More</span>';
        el('ft-dropoverlay').innerHTML = ICON.upload + '<div>Drop files to upload here</div>';

        el('ft-back').addEventListener('click', goBack);
        el('ft-forward').addEventListener('click', goForward);
        el('ft-up').addEventListener('click', goUp);
        el('ft-refresh').addEventListener('click', window.ftRefresh);

        el('ft-pathbar').addEventListener('click', function () { el('ft-pathinput').focus(); });
        el('ft-pathinput').addEventListener('focus', function () { this.select(); });
        el('ft-pathinput').addEventListener('blur', updatePathBar);
        el('ft-pathinput').addEventListener('keydown', function (ev) {
            if (ev.key === 'Enter') { ev.preventDefault(); this.blur(); commitPathBar(); }
            if (ev.key === 'Escape') { this.blur(); updatePathBar(); }
        });

        el('ft-sidechip').addEventListener('click', function () {
            showMenu(el('ft-sidechip'), [
                { label: 'Attach USB to KVM side', icon: 'refresh',
                  disabled: state.side === 'kvm', onClick: function () { requestSideSwitch('kvm'); } },
                { label: 'Attach USB to remote side', icon: 'refresh',
                  disabled: state.side === 'remote', onClick: function () { requestSideSwitch('remote'); } },
                { sep: true },
                { label: 'Device information…', icon: 'info', onClick: showDeviceInfo }
            ]);
        });
        el('ft-banner-action').addEventListener('click', function () { requestSideSwitch('kvm'); });

        el('ft-btn-upload').addEventListener('click', function () { el('ft-fileinput').click(); });
        el('ft-fileinput').addEventListener('change', function () { uploadFiles(this.files); });
        el('ft-btn-newfolder').addEventListener('click', function () { promptCreate('dir'); });

        el('ft-btn-more').addEventListener('click', function () {
            showMenu(el('ft-btn-more'), [
                { label: 'Search in this folder', icon: 'search', onClick: toggleSearch },
                { label: 'Select all', icon: 'selectall', disabled: !storageAvailable(),
                  onClick: function () {
                      state.selection = {};
                      sortedEntries().forEach(function (e) { state.selection[e.name] = true; });
                      refreshSelectionClasses();
                      updateSelectionUi();
                  } },
                { label: 'New file…', icon: 'newfile', disabled: !storageAvailable(),
                  onClick: function () { promptCreate('file'); } },
                { sep: true },
                { label: 'Device information…', icon: 'info', onClick: showDeviceInfo },
                { label: 'Unmount drive', icon: 'eject', disabled: !storageAvailable(),
                  onClick: confirmUnmount }
            ], 'right');
        });

        el('ft-view-list').addEventListener('click', function () { setView('list'); });
        el('ft-view-grid').addEventListener('click', function () { setView('grid'); });

        document.querySelectorAll('#ft-table thead th.sortable').forEach(function (th) {
            th.addEventListener('click', function () { setSort(th.dataset.sort); });
        });

        el('ft-op-download').addEventListener('click', downloadSelected);
        el('ft-op-delete').addEventListener('click', function () {
            confirmDelete(selectedEntries().map(function (e) {
                return { name: e.name, path: joinPath(state.path, e.name), is_dir: e.is_dir };
            }));
        });
        el('ft-op-move').addEventListener('click', function () { promptTransfer('move'); });
        el('ft-op-copy').addEventListener('click', function () { promptTransfer('copy'); });
        el('ft-op-more').addEventListener('click', function () {
            const sel = selectedEntries();
            showMenu(el('ft-op-more'), [
                { label: 'Rename…', icon: 'rename', disabled: sel.length !== 1,
                  onClick: function () { promptRename(sel[0]); } },
                { label: 'Open', icon: 'forward', disabled: sel.length !== 1,
                  onClick: function () { open(sel[0]); } },
                { sep: true },
                { label: 'Clear selection', icon: 'selectall',
                  onClick: function () {
                      state.selection = {};
                      refreshSelectionClasses();
                      updateSelectionUi();
                  } }
            ], 'right');
        });

        el('ft-search-input').addEventListener('input', function () {
            state.filter = this.value.trim();
            renderEntries();
        });
        el('ft-search-input').addEventListener('keydown', function (ev) {
            if (ev.key === 'Escape') toggleSearch();
        });
        el('ft-search-close').innerHTML = '&times;';
        el('ft-search-close').addEventListener('click', toggleSearch);

        // Clicking the empty area below the entries clears the selection
        el('ft-scroll').addEventListener('mousedown', function (ev) {
            if (ev.target.closest('tr') || ev.target.closest('.ft-tile') ||
                ev.target.closest('thead') || ev.target.closest('.ft-menu')) return;
            state.selection = {};
            refreshSelectionClasses();
            updateSelectionUi();
        });

        document.addEventListener('keydown', function (ev) {
            if (ev.target.matches('input, textarea')) return;
            if (ev.key === 'Delete' && selectedNames().length) {
                el('ft-op-delete').click();
            } else if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'a') {
                ev.preventDefault();
                state.selection = {};
                sortedEntries().forEach(function (e) { state.selection[e.name] = true; });
                refreshSelectionClasses();
                updateSelectionUi();
            } else if (ev.key === 'F5') {
                ev.preventDefault();
                window.ftRefresh();
            } else if (ev.key === 'Backspace') {
                goUp();
            }
        });

        initDropTarget();
        setView(state.view);
    }

    /* --------------------------------------------- shell / parent bridge */

    // The Virtual USB tool and the settings overlay live in the parent window.
    // They tell us when the drive changes hands or the disk is reformatted so
    // the browser can drop a listing that is no longer valid.
    window.addEventListener('message', function (evt) {
        const d = evt.data;
        if (!d || !d.type) return;
        if (d.type === 'dezkvm-storage-side-changed') {
            rootCache = null;
            refreshStorageState(function () {
                if (storageAvailable()) navigate('/');
            });
        } else if (d.type === 'dezkvm-format-complete') {
            rootCache = null;
            refreshStorageState(function () { navigate('/'); });
        }
    });

    function requestSideSwitch(side) {
        // The switch itself is driven by the viewport window (it owns the
        // aux-MCU helpers); ask it to do the work and wait for its reply.
        try {
            if (window.parent && window.parent !== window) {
                window.parent.postMessage({ type: 'dezkvm-request-storage-side', side: side }, '*');
                toast('Switching the USB drive to the ' + (side === 'kvm' ? 'KVM' : 'remote') + ' side…');
                return;
            }
        } catch (e) { /* fall through */ }
        toast('Use the Virtual USB tool to switch sides', 'error');
    }

    /* -------------------------------------------------------------- init */

    if (!UUID) {
        wire();
        setStatus('No device UUID provided.');
        applyStorageState();
        return;
    }

    wire();
    updateNavButtons();
    refreshStorageState(function () {
        navigate('/');
    });

    // The drive can be switched away by the settings overlay, the viewport
    // toolbar or the physical button, none of which necessarily notify us.
    setInterval(function () {
        const before = state.side + '|' + state.browsable;
        refreshStorageState(function () {
            if (before !== state.side + '|' + state.browsable && storageAvailable()) {
                rootCache = null;
                navigate('/');
            }
        });
    }, 8000);
})();
