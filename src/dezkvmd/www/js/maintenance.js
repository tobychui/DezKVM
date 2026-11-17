/*
    maintenance.js — Maintenance mode (shell, shown when no KVM session is open)

    Manage the machine running dezkvmd: live CPU / memory / storage /
    network / temperature tiles, CPU+memory and network history charts
    (1 h / 24 h), the process list, and restart / shutdown of the host.
    Power actions ask for the admin password again.

    Backend: mod/hostmgr, /api/v1/host/{summary,cpuinfo,history,processes,power}.
    Uses main.js globals: activeSessionUuid, csrfToken().
*/
(function () {
    'use strict';

    const SUMMARY_MS = 2000;
    const PROCS_MS = 3000;
    const HISTORY_MS = 10000;
    const RANGE_SECONDS = { '1h': 3600, '24h': 86400 };

    let active = false;
    let timers = [];
    let historyRange = '1h';
    let cpuInfo = null;
    let lastSummary = null;
    let lastHistory = null;
    let lastProcs = [];
    let procCpus = 1;
    let procSort = { key: 'cpu', dir: -1 };
    const busy = {};
    const tableView = {}; // chart id -> true when showing the table

    /* ------------------------------------------------------ formatting */

    function fmtBytes(b) {
        if (!(b >= 0)) return '–';
        const units = ['B', 'KB', 'MB', 'GB', 'TB'];
        let i = 0;
        while (b >= 1024 && i < units.length - 1) { b /= 1024; i++; }
        return (i === 0 || b >= 100 ? Math.round(b) : b.toFixed(1)) + ' ' + units[i];
    }

    function fmtBits(bps) {
        if (!(bps >= 0)) return '–';
        const units = ['bps', 'Kbps', 'Mbps', 'Gbps'];
        let i = 0;
        while (bps >= 1000 && i < units.length - 1) { bps /= 1000; i++; }
        return (i === 0 || bps >= 100 ? Math.round(bps) : bps.toFixed(1)) + ' ' + units[i];
    }

    function fmtDuration(sec) {
        sec = Math.max(0, Math.floor(sec));
        const d = Math.floor(sec / 86400);
        const h = Math.floor((sec % 86400) / 3600);
        const m = Math.floor((sec % 3600) / 60);
        if (d > 0) return d + 'd ' + h + 'h';
        if (h > 0) return h + 'h ' + m + 'm';
        if (m > 0) return m + 'm';
        return sec + 's';
    }

    function fmtClock(unix) {
        const d = new Date(unix * 1000);
        return String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    }

    function pct(used, total) {
        return total > 0 ? used / total * 100 : 0;
    }

    function setText(id, text) {
        const el = document.getElementById(id);
        if (el) el.textContent = text;
    }

    // Meter severity: under 70% normal, 70-90% warning, 90%+ critical
    function setMeter(id, percent) {
        const el = document.getElementById(id);
        if (!el) return;
        const p = Math.max(0, Math.min(100, percent || 0));
        el.firstElementChild.style.width = p + '%';
        el.classList.toggle('warning', p >= 70 && p < 90);
        el.classList.toggle('critical', p >= 90);
        el.setAttribute('role', 'meter');
        el.setAttribute('aria-valuemin', '0');
        el.setAttribute('aria-valuemax', '100');
        el.setAttribute('aria-valuenow', String(Math.round(p)));
    }

    /* ----------------------------------------------------------- fetch */

    function getJSON(url) {
        return fetch(url, { cache: 'no-store', credentials: 'same-origin' }).then(function (resp) {
            if (resp.status === 401) {
                window.location.href = '/login.html';
                throw new Error('unauthorized');
            }
            if (!resp.ok) return resp.text().then(function (t) { throw new Error(t || resp.statusText); });
            return resp.json();
        });
    }

    // Run fn unless the previous call of the same name is still pending or
    // the page is in the background
    function guarded(name, fn) {
        return function () {
            if (!active || busy[name] || document.hidden) return;
            busy[name] = true;
            Promise.resolve(fn()).catch(function (e) {
                console.warn('Maintenance ' + name + ':', e.message || e);
            }).finally(function () { busy[name] = false; });
        };
    }

    /* ------------------------------------------------------ enter/exit */

    function setButtonState() {
        const btn = document.getElementById('tbMaintenanceBtn');
        if (btn) btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    }

    function enter() {
        if (active) return;
        if (typeof activeSessionUuid !== 'undefined' && activeSessionUuid) return; // only without a session
        active = true;
        document.body.classList.add('maintenance-active');
        $('#landing').hide();
        $('#maintenance').show();
        setButtonState();

        refreshSummary();
        refreshProcs();
        refreshHistory();
        if (!cpuInfo) {
            getJSON('/api/v1/host/cpuinfo').then(function (info) {
                cpuInfo = info;
                if (lastSummary) renderSummary(lastSummary);
            }).catch(function () { /* optional */ });
        }
        timers = [
            setInterval(refreshSummary, SUMMARY_MS),
            setInterval(refreshProcs, PROCS_MS),
            setInterval(refreshHistory, HISTORY_MS)
        ];
    }

    function exit() {
        if (!active) return;
        active = false;
        timers.forEach(clearInterval);
        timers = [];
        document.body.classList.remove('maintenance-active');
        $('#maintenance').hide();
        if (typeof activeSessionUuid === 'undefined' || !activeSessionUuid) {
            $('#landing').show();
        }
        setButtonState();
    }

    window.toggleMaintenanceMode = function () { active ? exit() : enter(); };
    window.exitMaintenanceMode = exit;
    window.maintenanceModeActive = function () { return active; };

    /* --------------------------------------------------------- summary */

    const refreshSummary = guarded('summary', function () {
        return getJSON('/api/v1/host/summary').then(renderSummary);
    });

    function renderSummary(s) {
        lastSummary = s;

        const hostParts = [s.hostname, s.model, s.os, s.kernel ? 'Linux ' + s.kernel : '', s.arch];
        setText('mtHostLine', hostParts.filter(Boolean).join(' · '));

        // CPU
        setText('mtCpuValue', s.cpu_ready ? s.cpu.toFixed(1) + '%' : '…');
        setMeter('mtCpuMeter', s.cpu);
        // hardwareinfo reports unknown fields as "???"
        const known = function (v) { return v && v.indexOf('???') < 0 ? v.trim() : ''; };
        const cpuName = cpuInfo ? (known(cpuInfo.Model) || known(cpuInfo.Hardware)) : '';
        const cores = s.cores ? s.cores + ' cores' : (s.threads + ' threads');
        setText('mtCpuSub', [cpuName, cores].filter(Boolean).join(' · ') +
            '\nLoad ' + s.load.map(function (l) { return l.toFixed(2); }).join(' / '));

        // Memory
        const ramPct = pct(s.ram_used, s.ram_total);
        setText('mtRamValue', ramPct.toFixed(0) + '%');
        setMeter('mtRamMeter', ramPct);
        let ramSub = fmtBytes(s.ram_used) + ' of ' + fmtBytes(s.ram_total);
        if (s.swap_total > 0) ramSub += '\nSwap ' + fmtBytes(s.swap_used) + ' of ' + fmtBytes(s.swap_total);
        setText('mtRamSub', ramSub);

        // Storage
        if (s.disk) {
            const diskPct = pct(s.disk.used, s.disk.total);
            setText('mtDiskValue', diskPct.toFixed(0) + '%');
            setMeter('mtDiskMeter', diskPct);
            setText('mtDiskSub', fmtBytes(s.disk.used) + ' of ' + fmtBytes(s.disk.total) + ' (' + s.disk.path + ')');
        }

        // Network: sum of the interfaces, details of the main one
        let rx = 0, tx = 0;
        (s.net || []).forEach(function (n) { rx += n.rx_bps; tx += n.tx_bps; });
        setText('mtRxValue', fmtBits(rx));
        setText('mtTxValue', fmtBits(tx));
        const up = (s.net || []).filter(function (n) { return n.up; });
        setText('mtNetSub', up.map(function (n) {
            const v4 = (n.addrs || []).find(function (a) { return a.indexOf(':') < 0; });
            return n.name + (v4 ? ' ' + v4.split('/')[0] : '');
        }).join('\n') || 'No interface up');

        // Temperature: hottest sensor, status as icon + label
        const tempEl = document.getElementById('mtTempValue');
        const tempSub = document.getElementById('mtTempSub');
        if (s.temps && s.temps.length) {
            const hottest = s.temps.reduce(function (a, b) { return b.celsius > a.celsius ? b : a; });
            tempEl.textContent = hottest.celsius.toFixed(1) + ' °C';
            const level = hottest.celsius >= 80 ? 'critical' : (hottest.celsius >= 70 ? 'warning' : '');
            tempSub.textContent = '';
            const badge = document.createElement('span');
            badge.className = 'mt-status ' + level;
            badge.textContent = level === 'critical' ? 'Hot' : (level === 'warning' ? 'Warm' : 'Normal');
            tempSub.appendChild(badge);
            tempSub.appendChild(document.createTextNode(' · ' + hottest.name));
        } else {
            tempEl.textContent = '–';
            tempSub.textContent = 'No sensor found';
        }

        // Uptime
        setText('mtUptimeValue', fmtDuration(s.uptime));
        setText('mtUptimeSub', s.processes + ' processes');
    }

    /* --------------------------------------------------------- history */

    const refreshHistory = guarded('history', function () {
        const range = historyRange;
        ['mtChartUsage', 'mtChartNet'].forEach(function (id) {
            document.getElementById(id).classList.add('refreshing');
        });
        return getJSON('/api/v1/host/history?range=' + range).then(function (h) {
            if (range !== historyRange) return; // range switched meanwhile
            lastHistory = h;
            renderCharts();
        }).finally(function () {
            ['mtChartUsage', 'mtChartNet'].forEach(function (id) {
                document.getElementById(id).classList.remove('refreshing');
            });
        });
    });

    function renderCharts() {
        if (!lastHistory) return;
        const points = lastHistory.points || [];
        const step = lastHistory.step || 10;
        const span = RANGE_SECONDS[lastHistory.range] || 3600;
        const end = points.length ? points[points.length - 1].T : Math.floor(Date.now() / 1000);

        renderChart('mtChartUsage', {
            points: points, step: step, start: end - span, end: end,
            yMax: 100,
            yTicks: [0, 25, 50, 75, 100],
            fmt: function (v) { return v.toFixed(v >= 10 ? 0 : 1) + '%'; },
            axisFmt: function (v) { return v + '%'; },
            label: 'CPU and memory usage in percent',
            series: [
                { name: 'CPU', color: 'var(--mt-series-1)', get: function (p) { return p.CPU; } },
                { name: 'Memory', color: 'var(--mt-series-2)', get: function (p) { return p.RAM; } }
            ]
        });

        let maxBits = 0;
        points.forEach(function (p) { maxBits = Math.max(maxBits, p.RX, p.TX); });
        const scale = niceScale(maxBits);
        renderChart('mtChartNet', {
            points: points, step: step, start: end - span, end: end,
            yMax: scale.max,
            yTicks: scale.ticks,
            fmt: fmtBits,
            axisFmt: fmtBitsShort,
            label: 'Network throughput in bits per second',
            series: [
                { name: 'Download', color: 'var(--mt-series-1)', get: function (p) { return p.RX; } },
                { name: 'Upload', color: 'var(--mt-series-2)', get: function (p) { return p.TX; } }
            ]
        });
    }

    // Compact axis labels: 0 / 250K / 500K / 1M
    function fmtBitsShort(v) {
        if (v === 0) return '0';
        const units = ['', 'K', 'M', 'G'];
        let i = 0;
        while (v >= 1000 && i < units.length - 1) { v /= 1000; i++; }
        return (Math.round(v * 10) / 10) + units[i];
    }

    // Round axis maximum with 4 even steps
    function niceScale(max) {
        if (!(max > 0)) return { max: 1000, ticks: [0, 250, 500, 750, 1000] };
        const rough = max / 4;
        const mag = Math.pow(10, Math.floor(Math.log10(rough)));
        const norm = rough / mag;
        const stepNorm = norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 2.5 ? 2.5 : norm <= 5 ? 5 : 10;
        const step = stepNorm * mag;
        return { max: step * 4, ticks: [0, step, step * 2, step * 3, step * 4] };
    }

    function svgEl(tag, attrs) {
        const el = document.createElementNS('http://www.w3.org/2000/svg', tag);
        Object.keys(attrs || {}).forEach(function (k) { el.setAttribute(k, attrs[k]); });
        return el;
    }

    /*
        Line chart: one hairline grid, 2px series lines, legend above, value
        at the end of each line when the two labels do not collide, and a
        crosshair tooltip listing every series (mouse, touch, arrow keys).
        Points more than 1.5 steps apart, or with an unknown (-1) value,
        break the line instead of bridging the gap.
    */
    function renderChart(id, cfg) {
        const host = document.getElementById(id);
        host.textContent = '';

        const legend = document.createElement('div');
        legend.className = 'mt-legend';
        cfg.series.forEach(function (s) {
            const item = document.createElement('span');
            const key = document.createElement('i');
            key.className = 'mt-key';
            key.style.background = s.color;
            item.appendChild(key);
            item.appendChild(document.createTextNode(s.name));
            legend.appendChild(item);
        });
        host.appendChild(legend);

        const usable = cfg.points.filter(function (p) {
            return cfg.series.some(function (s) { return s.get(p) >= 0; });
        });
        if (usable.length < 2) {
            const empty = document.createElement('div');
            empty.className = 'mt-chart-empty';
            empty.textContent = 'Collecting data… a new point is added every ' + cfg.step + ' seconds.';
            host.appendChild(empty);
            return;
        }

        if (tableView[id]) {
            host.appendChild(buildTable(cfg, usable));
            return;
        }

        const width = Math.max(280, host.clientWidth);
        const height = 210;
        const m = { top: 10, right: 58, bottom: 22, left: 46 };
        const plotW = width - m.left - m.right;
        const plotH = height - m.top - m.bottom;
        const x = function (t) { return m.left + (t - cfg.start) / (cfg.end - cfg.start) * plotW; };
        const y = function (v) { return m.top + plotH - Math.min(v, cfg.yMax) / cfg.yMax * plotH; };

        const svg = svgEl('svg', {
            viewBox: '0 0 ' + width + ' ' + height,
            height: height,
            tabindex: '0',
            role: 'img',
            'aria-label': cfg.label + '. Use the arrow keys to read values.'
        });

        // Grid + y labels
        cfg.yTicks.forEach(function (v, i) {
            svg.appendChild(svgEl('line', {
                x1: m.left, x2: m.left + plotW, y1: y(v), y2: y(v),
                class: i === 0 ? 'mt-baseline' : 'mt-grid'
            }));
            const label = svgEl('text', { x: m.left - 8, y: y(v) + 4, 'text-anchor': 'end', class: 'mt-axis-text' });
            label.textContent = cfg.axisFmt(v);
            svg.appendChild(label);
        });

        // X labels: every 10 min (1 h) or 4 h (24 h), on round clock times
        const span = cfg.end - cfg.start;
        const every = span <= 3600 ? 600 : 14400;
        const tzOffset = new Date().getTimezoneOffset() * 60;
        let t = Math.ceil((cfg.start - tzOffset) / every) * every + tzOffset;
        for (; t <= cfg.end; t += every) {
            const lx = x(t);
            if (lx < m.left + 14 || lx > m.left + plotW - 14) continue;
            const label = svgEl('text', { x: lx, y: height - 4, 'text-anchor': 'middle', class: 'mt-axis-text' });
            label.textContent = fmtClock(t);
            svg.appendChild(label);
        }

        // Lines
        const gap = cfg.step * 1.5;
        cfg.series.forEach(function (s) {
            let d = '';
            let prevT = null;
            cfg.points.forEach(function (p) {
                const v = s.get(p);
                if (!(v >= 0)) { prevT = null; return; }
                const move = prevT === null || p.T - prevT > gap;
                d += (move ? 'M' : 'L') + x(p.T).toFixed(1) + ' ' + y(v).toFixed(1);
                prevT = p.T;
            });
            svg.appendChild(svgEl('path', { d: d, class: 'mt-line', style: 'stroke:' + s.color }));
        });

        // End values, only when they do not collide
        const last = usable[usable.length - 1];
        const ends = cfg.series.map(function (s) { return { s: s, v: s.get(last) }; })
            .filter(function (e) { return e.v >= 0; });
        const collide = ends.length === 2 && Math.abs(y(ends[0].v) - y(ends[1].v)) < 14;
        ends.forEach(function (e) {
            svg.appendChild(svgEl('circle', { cx: x(last.T), cy: y(e.v), r: 4, class: 'mt-dot', style: 'fill:' + e.s.color }));
            if (collide) return;
            const label = svgEl('text', { x: x(last.T) + 8, y: y(e.v) + 4, class: 'mt-end-text' });
            label.textContent = cfg.fmt(e.v);
            svg.appendChild(label);
        });

        // Hover layer
        const cross = svgEl('line', { y1: m.top, y2: m.top + plotH, class: 'mt-crosshair', visibility: 'hidden' });
        svg.appendChild(cross);
        const hoverDots = cfg.series.map(function (s) {
            const dot = svgEl('circle', { r: 4, class: 'mt-dot', style: 'fill:' + s.color, visibility: 'hidden' });
            svg.appendChild(dot);
            return dot;
        });
        const hit = svgEl('rect', { x: m.left, y: m.top, width: plotW, height: plotH, fill: 'transparent' });
        svg.appendChild(hit);
        host.appendChild(svg);

        const tip = document.createElement('div');
        tip.className = 'mt-tooltip';
        tip.style.display = 'none';
        host.appendChild(tip);

        let index = -1;
        function show(i) {
            index = Math.max(0, Math.min(usable.length - 1, i));
            const p = usable[index];
            const px = x(p.T);
            cross.setAttribute('x1', px);
            cross.setAttribute('x2', px);
            cross.setAttribute('visibility', 'visible');

            tip.textContent = '';
            const time = document.createElement('div');
            time.className = 'mt-tt-time';
            time.textContent = new Date(p.T * 1000).toLocaleString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', month: 'short', day: 'numeric' });
            tip.appendChild(time);
            cfg.series.forEach(function (s, si) {
                const v = s.get(p);
                const dot = hoverDots[si];
                if (v >= 0) {
                    dot.setAttribute('cx', px);
                    dot.setAttribute('cy', y(v));
                    dot.setAttribute('visibility', 'visible');
                } else {
                    dot.setAttribute('visibility', 'hidden');
                }
                const row = document.createElement('div');
                row.className = 'mt-tt-row';
                const key = document.createElement('i');
                key.className = 'mt-key';
                key.style.background = s.color;
                const val = document.createElement('b');
                val.textContent = v >= 0 ? cfg.fmt(v) : 'no data';
                const name = document.createElement('span');
                name.textContent = s.name;
                row.appendChild(key);
                row.appendChild(val);
                row.appendChild(name);
                tip.appendChild(row);
            });

            // Place the tooltip beside the crosshair, inside the card
            tip.style.display = 'block';
            const scaleX = svg.getBoundingClientRect().width / width;
            const left = px * scaleX;
            const tipW = tip.offsetWidth;
            tip.style.left = (left + 12 + tipW > host.clientWidth ? left - tipW - 12 : left + 12) + 'px';
            tip.style.top = (legend.offsetHeight + 6) + 'px';
        }
        function hide() {
            index = -1;
            tip.style.display = 'none';
            cross.setAttribute('visibility', 'hidden');
            hoverDots.forEach(function (d) { d.setAttribute('visibility', 'hidden'); });
        }
        function nearest(clientX) {
            const rect = svg.getBoundingClientRect();
            const vx = (clientX - rect.left) / rect.width * width;
            const t = cfg.start + (vx - m.left) / plotW * (cfg.end - cfg.start);
            let best = 0;
            for (let i = 1; i < usable.length; i++) {
                if (Math.abs(usable[i].T - t) < Math.abs(usable[best].T - t)) best = i;
            }
            return best;
        }
        hit.addEventListener('pointermove', function (e) { show(nearest(e.clientX)); });
        hit.addEventListener('pointerleave', hide);
        svg.addEventListener('keydown', function (e) {
            if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                e.preventDefault();
                show(index < 0 ? usable.length - 1 : index + (e.key === 'ArrowLeft' ? -1 : 1));
            } else if (e.key === 'Escape') {
                hide();
            }
        });
        svg.addEventListener('blur', hide);
    }

    // Table view of a chart, newest first
    function buildTable(cfg, usable) {
        const wrap = document.createElement('div');
        wrap.className = 'mt-chart-table-wrap';
        const table = document.createElement('table');
        table.className = 'mt-chart-table';
        const head = table.createTHead().insertRow();
        ['Time'].concat(cfg.series.map(function (s) { return s.name; })).forEach(function (h) {
            const th = document.createElement('th');
            th.textContent = h;
            head.appendChild(th);
        });
        const body = table.createTBody();
        usable.slice().reverse().forEach(function (p) {
            const row = body.insertRow();
            row.insertCell().textContent = new Date(p.T * 1000).toLocaleString([], { hour: '2-digit', minute: '2-digit', month: 'short', day: 'numeric' });
            cfg.series.forEach(function (s) {
                const v = s.get(p);
                row.insertCell().textContent = v >= 0 ? cfg.fmt(v) : '–';
            });
        });
        wrap.appendChild(table);
        return wrap;
    }

    /* ------------------------------------------------------- processes */

    const refreshProcs = guarded('procs', function () {
        return getJSON('/api/v1/host/processes').then(function (data) {
            lastProcs = data.processes || [];
            procCpus = data.cpus || 1;
            renderProcs();
        });
    });

    function renderProcs() {
        const filter = (document.getElementById('mtProcFilter').value || '').trim().toLowerCase();
        const showKernel = document.getElementById('mtShowKernel').checked;
        const now = lastSummary ? lastSummary.time : Math.floor(Date.now() / 1000);

        let list = lastProcs.filter(function (p) {
            if (!showKernel && p.kernel) return false;
            if (!filter) return true;
            return String(p.pid) === filter ||
                p.name.toLowerCase().indexOf(filter) >= 0 ||
                p.user.toLowerCase().indexOf(filter) >= 0 ||
                p.cmd.toLowerCase().indexOf(filter) >= 0;
        });
        const key = procSort.key;
        const dir = procSort.dir;
        list.sort(function (a, b) {
            let av = a[key], bv = b[key];
            if (key === 'started') { av = -av; bv = -bv; } // "running for": older first when descending
            if (typeof av === 'string') return av.localeCompare(bv) * dir;
            return ((av > bv) - (av < bv)) * dir || (a.pid - b.pid);
        });

        const shown = lastProcs.filter(function (p) { return showKernel || !p.kernel; }).length;
        setText('mtProcCount', list.length === shown ? shown + ' running' : list.length + ' of ' + shown);

        document.querySelectorAll('.mt-proc-table th[data-sort]').forEach(function (th) {
            if (th.dataset.sort === key) th.setAttribute('aria-sort', dir > 0 ? 'ascending' : 'descending');
            else th.removeAttribute('aria-sort');
        });

        const body = document.getElementById('mtProcBody');
        const frag = document.createDocumentFragment();
        if (list.length === 0) {
            const row = document.createElement('tr');
            const cell = row.insertCell();
            cell.colSpan = 8;
            cell.className = 'mt-empty';
            cell.textContent = filter ? 'No process matches the filter' : 'No processes';
            frag.appendChild(row);
        }
        list.forEach(function (p) {
            const row = document.createElement('tr');
            if (p.kernel) row.className = 'mt-kernel';
            const cells = [
                [String(p.pid), 'num'],
                [p.name, ''],
                [p.user, ''],
                [p.cpu.toFixed(1) + '%', 'num'],
                [fmtBytes(p.rss) + ' · ' + p.mem.toFixed(1) + '%', 'num'],
                [String(p.threads), 'num'],
                [p.started ? fmtDuration(now - p.started) : '–', 'num'],
                [p.cmd, 'mt-cmd']
            ];
            cells.forEach(function (c) {
                const td = row.insertCell();
                td.textContent = c[0];
                if (c[1]) td.className = c[1];
            });
            row.lastChild.title = p.cmd;
            frag.appendChild(row);
        });
        body.textContent = '';
        body.appendChild(frag);
    }

    /* ----------------------------------------------------------- power */

    let powerAction = null;

    const POWER_TEXT = {
        reboot: {
            title: 'Restart the KVM host?',
            body: 'The host restarts now. All KVM sessions, terminals and file transfers are disconnected; the page reconnects when the host is back.',
            button: 'Restart host',
            progress: 'Restarting…'
        },
        shutdown: {
            title: 'Shut down the KVM host?',
            body: 'The host powers off now. All KVM sessions end, and DezKVM stays offline until the host is powered on again by hand.',
            button: 'Shut down host',
            progress: 'Shutting down…'
        }
    };

    window.openHostPowerDialog = function (action) {
        const text = POWER_TEXT[action];
        if (!text) return;
        powerAction = action;
        setText('mtPowerTitle', text.title);
        setText('mtPowerText', text.body);
        setText('mtPowerConfirm', text.button);
        setText('mtPowerError', '');
        const input = document.getElementById('mtPowerPassword');
        input.value = '';
        $('#mtPowerModal').modal({
            onHidden: function () { document.getElementById('mtPowerPassword').value = ''; }
        }).modal('show');
        setTimeout(function () { input.focus(); }, 50);
    };

    function submitPower() {
        const action = powerAction;
        const input = document.getElementById('mtPowerPassword');
        const button = document.getElementById('mtPowerConfirm');
        if (!action) return;
        if (!input.value) {
            setText('mtPowerError', 'Enter the admin password.');
            input.focus();
            return;
        }
        button.classList.add('loading');
        setText('mtPowerError', '');
        fetch('/api/v1/host/power', {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
                'Content-Type': 'application/json',
                'dezkvm_csrf_token': (typeof csrfToken === 'function') ? csrfToken() : ''
            },
            body: JSON.stringify({ action: action, password: input.value })
        }).then(function (resp) {
            if (resp.ok) {
                input.value = '';
                $('#mtPowerModal').modal('hide');
                watchPowerAction(action);
                return;
            }
            return resp.text().then(function (msg) {
                setText('mtPowerError', resp.status === 403 && /password/i.test(msg)
                    ? 'Incorrect password.'
                    : (msg.trim() || ('Request failed (' + resp.status + ')')));
                input.select();
            });
        }).catch(function () {
            setText('mtPowerError', 'Could not reach the host.');
        }).finally(function () {
            button.classList.remove('loading');
        });
    }

    /* Follow the host going down (and, for a restart, coming back) */
    function watchPowerAction(action) {
        exit();
        const text = POWER_TEXT[action];
        const overlay = document.getElementById('mtPowerOverlay');
        const spinner = document.getElementById('mtOverlaySpinner');
        const closeBtn = document.getElementById('mtOverlayClose');
        const hostname = lastSummary && lastSummary.hostname ? lastSummary.hostname : 'the host';
        setText('mtOverlayTitle', text.progress);
        setText('mtOverlayText', 'Waiting for ' + hostname + ' to go offline…');
        spinner.style.display = '';
        closeBtn.style.display = 'none';
        overlay.style.display = '';

        const started = Date.now();
        let wentDown = false;

        function ping() {
            const ctrl = new AbortController();
            const timer = setTimeout(function () { ctrl.abort(); }, 3000);
            return fetch('/api/v1/host/summary', { cache: 'no-store', credentials: 'same-origin', signal: ctrl.signal })
                .then(function (resp) {
                    clearTimeout(timer);
                    return resp.ok ? resp.json().catch(function () { return {}; }) : {};
                }, function () {
                    clearTimeout(timer);
                    return null; // unreachable
                });
        }

        function fail(message) {
            spinner.style.display = 'none';
            setText('mtOverlayTitle', action === 'reboot' ? 'The host did not restart' : 'The host did not shut down');
            setText('mtOverlayText', message);
            closeBtn.style.display = '';
        }

        function tick() {
            ping().then(function (state) {
                const elapsed = (Date.now() - started) / 1000;
                if (!wentDown) {
                    if (state === null) {
                        wentDown = true;
                        if (action === 'shutdown') {
                            spinner.style.display = 'none';
                            setText('mtOverlayTitle', 'The host is shutting down');
                            setText('mtOverlayText', 'dezkvmd has stopped and ' + hostname + ' is powering off. Power it on again to use DezKVM. You can close this page.');
                            return;
                        }
                        setText('mtOverlayText', hostname + ' is restarting. This page reconnects when it is back online.');
                    } else if (state.power_error) {
                        fail(state.power_error);
                        return;
                    } else if (elapsed > 90) {
                        fail(hostname + ' is still running 90 seconds after the request. Check the dezkvmd log on the host.');
                        return;
                    }
                } else if (state !== null) {
                    setText('mtOverlayTitle', 'Back online');
                    setText('mtOverlayText', 'Reloading…');
                    setTimeout(function () { window.location.reload(); }, 800);
                    return;
                } else if (elapsed > 300) {
                    setText('mtOverlayText', hostname + ' is taking longer than usual to come back. Still waiting…');
                }
                setTimeout(tick, 2000);
            });
        }
        setTimeout(tick, 1500);
    }

    window.closeHostPowerOverlay = function () {
        document.getElementById('mtPowerOverlay').style.display = 'none';
    };

    /* ----------------------------------------------------------- wiring */

    $(function () {
        $('#mtPowerConfirm').on('click', submitPower);
        $('#mtPowerPassword').on('keydown', function (e) {
            if (e.key === 'Enter') { e.preventDefault(); submitPower(); }
        });

        $('.mt-range .button').on('click', function () {
            const range = this.dataset.range;
            if (range === historyRange) return;
            historyRange = range;
            $('.mt-range .button').each(function () {
                const on = this.dataset.range === range;
                this.classList.toggle('active', on);
                this.setAttribute('aria-pressed', on ? 'true' : 'false');
            });
            busy.history = false;
            refreshHistory();
        });

        $('.mt-link[data-table-for]').on('click', function () {
            const id = this.dataset.tableFor;
            tableView[id] = !tableView[id];
            this.textContent = tableView[id] ? 'Show chart' : 'Show table';
            renderCharts();
        });

        $('#mtProcFilter').on('input', renderProcs);
        $('#mtShowKernel').on('change', renderProcs);
        $('.mt-proc-table th[data-sort]').on('click', function () {
            const key = this.dataset.sort;
            if (procSort.key === key) {
                procSort.dir = -procSort.dir;
            } else {
                // Numbers start with the largest, text alphabetically
                procSort = { key: key, dir: ['name', 'user', 'cmd'].indexOf(key) >= 0 ? 1 : -1 };
            }
            renderProcs();
        });

        // Redraw the charts at the new width
        let resizeTimer = null;
        window.addEventListener('resize', function () {
            if (!active) return;
            clearTimeout(resizeTimer);
            resizeTimer = setTimeout(renderCharts, 150);
        });

        // Catch up immediately when the tab becomes visible again
        document.addEventListener('visibilitychange', function () {
            if (active && !document.hidden) {
                refreshSummary();
                refreshProcs();
                refreshHistory();
            }
        });
    });
})();
