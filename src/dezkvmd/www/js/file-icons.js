/*
    file-icons.js

    Extension -> file type mapping shared by the File Transfer tool's list and
    thumbnail views. Icons are inline SVG (a document sheet with a coloured
    band carrying the extension) so a grid can render a few hundred entries
    without firing a request per cell -- this runs on an SBC.

    Public API (window.DezFileIcons):
      typeOf(entry)      -> { key, ext, label, color, thumbable }
      svg(entry, size)   -> inline <svg> markup string
      isThumbable(entry) -> true when the backend may have a real preview
*/
(function (global) {
    'use strict';

    // key    internal id
    // label  shown in the "Type" column for extension-less files
    // color  accent colour of the icon
    const TYPES = {
        folder:  { label: 'Folder',      color: '#f5b544' },
        image:   { label: 'Image',       color: '#2b8ef2' },
        video:   { label: 'Video',       color: '#8b5cf6' },
        audio:   { label: 'Audio',       color: '#12a5a5' },
        pdf:     { label: 'PDF',         color: '#e03d3d' },
        doc:     { label: 'Document',    color: '#2b6ef2' },
        sheet:   { label: 'Spreadsheet', color: '#1a9c5b' },
        slide:   { label: 'Slides',      color: '#e8792b' },
        archive: { label: 'Archive',     color: '#9b8a5a' },
        code:    { label: 'Code',        color: '#5b6b7f' },
        text:    { label: 'Text',        color: '#6b7280' },
        font:    { label: 'Font',        color: '#a855a8' },
        model:   { label: '3D Model',    color: '#c9a227' },
        disk:    { label: 'Disk Image',  color: '#6d7a8c' },
        exec:    { label: 'Executable',  color: '#4b5563' },
        file:    { label: 'File',        color: '#9aa0a6' }
    };

    // Extensions the server can actually render a preview for. This mirrors
    // IsRenderable() in mod/thumbnails/renderer.go exactly -- a guess that is
    // too generous costs one wasted request per tile in the grid, and the KVM
    // is streaming video at the same time.
    const THUMBABLE = {};
    ['png', 'jpeg', 'jpg', 'webp',                              // image.go
     'mp3', 'ogg', 'flac',                                      // audio.go (cover art)
     'mkv', 'mp4', 'webm', 'ogv', 'avi', 'rmvb',                // video.go (needs ffmpeg)
     'gcode', 'gco',                                            // gcode.go (slicer preview)
     'arw', 'cr2', 'dng', 'nef', 'raf', 'orf',                  // raw.go (embedded JPEG)
     'psd', 'svg'
    ].forEach(function (e) { THUMBABLE[e] = true; });

    // Extension -> type key. One flat map so a lookup is a single hash hit per
    // entry rather than a walk through a list of regexes.
    const EXT = {};
    function map(key, exts) { exts.forEach(function (e) { EXT[e] = key; }); }

    map('image',   ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp', 'heic', 'heif', 'tif', 'tiff', 'ico', 'svg', 'psd',
                    'avif', 'arw', 'cr2', 'cr3', 'nef', 'dng', 'orf', 'raf', 'rw2', 'pef', 'srw']);
    map('video',   ['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'mpg', 'mpeg', '3gp', 'ts', 'm2ts']);
    map('audio',   ['mp3', 'mp2', 'wav', 'flac', 'aac', 'ogg', 'oga', 'm4a', 'wma', 'opus', 'aiff', 'mid', 'midi']);
    map('pdf',     ['pdf']);
    map('doc',     ['doc', 'docx', 'odt', 'rtf', 'pages', 'wpd']);
    map('sheet',   ['xls', 'xlsx', 'ods', 'csv', 'tsv', 'numbers']);
    map('slide',   ['ppt', 'pptx', 'odp', 'key']);
    map('archive', ['zip', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'zst', '7z', 'rar', 'cab', 'lz', 'lzma']);
    map('code',    ['js', 'mjs', 'ts', 'tsx', 'jsx', 'py', 'go', 'rs', 'c', 'h', 'cpp', 'hpp', 'cc', 'cs', 'java',
                    'kt', 'swift', 'rb', 'php', 'sh', 'bash', 'zsh', 'ps1', 'bat', 'cmd', 'html', 'htm', 'css',
                    'scss', 'less', 'json', 'xml', 'sql', 'lua', 'pl', 'r', 'vue', 'svelte']);
    map('text',    ['txt', 'md', 'markdown', 'log', 'ini', 'cfg', 'conf', 'yaml', 'yml', 'toml', 'nfo', 'lst']);
    map('font',    ['ttf', 'otf', 'woff', 'woff2', 'eot', 'fnt']);
    map('model',   ['stl', 'obj', '3mf', 'ply', 'gcode', 'gco', 'nc', 'step', 'stp', 'fbx', 'dae', 'glb', 'gltf']);
    map('disk',    ['iso', 'img', 'dmg', 'vhd', 'vhdx', 'vmdk', 'qcow2', 'bin', 'cue']);
    map('exec',    ['exe', 'msi', 'appimage', 'deb', 'rpm', 'apk', 'app', 'elf', 'dll', 'so']);

    function extOf(name) {
        const s = String(name || '');
        const i = s.lastIndexOf('.');
        if (i <= 0) return '';
        return s.slice(i + 1).toLowerCase();
    }

    function typeOf(entry) {
        if (entry && entry.is_dir) {
            return { key: 'folder', ext: '', label: TYPES.folder.label, color: TYPES.folder.color, thumbable: false };
        }
        const ext = extOf(entry && entry.name);
        const key = EXT[ext] || 'file';
        const t = TYPES[key];
        return {
            key: key,
            ext: ext,
            // The mockup shows the concrete extension ("MP3", "DOCX") and falls
            // back to the generic family name for extension-less files.
            label: ext ? ext.toUpperCase() : t.label,
            color: t.color,
            thumbable: !!THUMBABLE[ext]
        };
    }

    function isThumbable(entry) { return typeOf(entry).thumbable; }

    function esc(s) {
        return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }

    // Short badge burned into the icon. Long extensions are trimmed so they
    // stay legible at 20px.
    function badgeText(t) {
        if (!t.ext) return '';
        return (t.ext.length > 4 ? t.ext.slice(0, 4) : t.ext).toUpperCase();
    }

    function folderSvg(size, color) {
        return '<svg class="dez-file-icon" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" ' +
            'fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">' +
            '<path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h4.1c.4 0 .78.16 1.06.44L11 6.8h8.5A1.5 1.5 0 0 1 21 8.3v9.2a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5v-11Z" fill="' + color + '" fill-opacity="0.55"/>' +
            '<path d="M3 9.4h18v8.1a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5V9.4Z" fill="' + color + '"/>' +
            '</svg>';
    }

    function fileSvg(size, color, badge) {
        // 24x24 sheet with a folded top-right corner; the extension badge sits
        // in a coloured band across the lower half.
        let label = '';
        if (badge) {
            const fs = badge.length >= 4 ? 5 : (badge.length === 3 ? 6 : 7);
            label = '<text x="12" y="18.3" text-anchor="middle" font-size="' + fs + '" font-weight="700" ' +
                'font-family="-apple-system, system-ui, sans-serif" fill="#ffffff" letter-spacing="0.2">' +
                esc(badge) + '</text>';
        }
        return '<svg class="dez-file-icon" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" ' +
            'fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">' +
            '<path d="M5.5 3h8.2L20 9.2v11.3A1.5 1.5 0 0 1 18.5 22h-13A1.5 1.5 0 0 1 4 20.5v-16A1.5 1.5 0 0 1 5.5 3Z" fill="#ffffff" stroke="' + color + '" stroke-width="1.4" stroke-linejoin="round"/>' +
            '<path d="M13.5 3 20 9.4h-5a1.5 1.5 0 0 1-1.5-1.5V3Z" fill="' + color + '" fill-opacity="0.35"/>' +
            '<rect x="4" y="13.2" width="16" height="6.4" rx="1.2" fill="' + color + '"/>' +
            label +
            '</svg>';
    }

    function svg(entry, size) {
        size = size || 20;
        const t = typeOf(entry);
        if (t.key === 'folder') return folderSvg(size, t.color);
        return fileSvg(size, t.color, badgeText(t));
    }

    global.DezFileIcons = {
        typeOf: typeOf,
        isThumbable: isThumbable,
        svg: svg,
        extOf: extOf
    };
})(window);
