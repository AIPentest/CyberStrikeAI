/**
 * System settings - Terminal: multiple tabs, streaming output, command history, Ctrl+L to clear the screen, long-running commands can be cancelled
 */
(function () {
    var getContext = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (type, attrs) {
        if (type === '2d') {
            attrs = (attrs && typeof attrs === 'object') ? Object.assign({ willReadFrequently: true }, attrs) : { willReadFrequently: true };
            return getContext.call(this, type, attrs);
        }
        return getContext.apply(this, arguments);
    };

    var terminals = [];
    var currentTabId = 1;
    var inited = false;
    var tabIdCounter = 1;
    var PROMPT = ''; // The real Shell outputs its own prompt, no longer customized here
    var HISTORY_MAX = 100;
    var CANCEL_AFTER_MS = 125000;

    function getCurrent() {
        for (var i = 0; i < terminals.length; i++) {
            if (terminals[i].id === currentTabId) return terminals[i];
        }
        return terminals[0] || null;
    }

    function tr(key, opts) {
        if (typeof window !== 'undefined' && typeof window.t === 'function') {
            return window.t(key, opts);
        }
        // Fallback when i18n is not ready (same as zh-CN)
        var fallbacks = {
            'settingsTerminal.welcomeLine': 'CyberStrikeAI Terminal - Real Shell session, enter commands directly; Ctrl+L clears the screen',
            'settingsTerminal.sessionClosed': '[Session closed]',
            'settingsTerminal.connectionError': '[Terminal connection error]',
            'settingsTerminal.connectFailed': '[Unable to connect to terminal service: {{msg}}]',
            'settingsTerminal.closeTabTitle': 'Close',
            'settingsTerminal.containerClickTitle': 'Click here and then enter a command',
            'settingsTerminal.xtermNotLoaded': 'Failed to load xterm.js, please refresh the page or check your network.',
            'settingsTerminal.terminalTab': 'Terminal {{n}}'
        };
        var s = fallbacks[key] || key;
        if (opts && typeof opts === 'object') {
            Object.keys(opts).forEach(function (k) {
                s = s.split('{{' + k + '}}').join(String(opts[k]));
            });
        }
        return s;
    }

    function getWelcomeLine() {
        return tr('settingsTerminal.welcomeLine') + '\r\n';
    }

    function writePrompt(tab) {
        // The prompt is output by the backend Shell itself; here we only keep a placeholder function to avoid errors in old code.
    }

    function redrawTabDisplay(t) {
        if (!t || !t.term) return;
        t.term.clear();
        t.term.write(getWelcomeLine());
    }

    function writeln(tabOrS, s) {
        var t, text;
        if (arguments.length === 1) { text = tabOrS; t = getCurrent(); } else { t = tabOrS; text = s; }
        if (!t || !t.term) return;
        if (text) t.term.writeln(text);
        else t.term.writeln('');
    }

    function writeTermData(tab, data) {
        if (!tab || !tab.term || data === undefined || data === null) return;
        try {
            tab.term.write(data, function () {
                try { tab.term.scrollToBottom(); } catch (e) {}
            });
        } catch (e) {
            tab.term.write(data);
        }
    }

    function writeOutput(tab, text, isError) {
        var t = tab || getCurrent();
        if (!t || !t.term || !text) return;
        var s = String(text).replace(/\r\n/g, '\n').replace(/\r/g, '\n');
        var lines = s.split('\n');
        var prefix = isError ? '\x1b[31m' : '';
        var suffix = isError ? '\x1b[0m' : '';
        t.term.write(prefix);
        for (var i = 0; i < lines.length; i++) {
            var line = lines[i].replace(/\r/g, '');
            t.term.writeln(line);
        }
        t.term.write(suffix);
    }

    // Get the current login token from local storage (keep consistent with the structure used in auth.js)
    function getStoredAuthToken() {
        try {
            var raw = localStorage.getItem('cyberstrike-auth');
            if (!raw) return null;
            var o = JSON.parse(raw);
            if (o && o.token) return o.token;
        } catch (e) {}
        return null;
    }

    // WebSocket URL construction (compatible with http/https, and passes the token via the query string to satisfy backend authentication)
    function buildTerminalWSURL() {
        var proto = (window.location.protocol === 'https:') ? 'wss://' : 'ws://';
        var url = proto + window.location.host + '/api/terminal/ws';
        var token = getStoredAuthToken();
        if (token) {
            url += '?token=' + encodeURIComponent(token);
        }
        return url;
    }

    function ensureTerminalWS(tab) {
        if (tab.ws && (tab.ws.readyState === WebSocket.OPEN || tab.ws.readyState === WebSocket.CONNECTING)) {
            return;
        }
        try {
            var ws = new WebSocket(buildTerminalWSURL());
            tab.ws = ws;
            tab.running = true;

            ws.onopen = function () {
                if (tab.term) {
                    tab.term.focus();
                    // Send the actual terminal dimensions to the backend immediately
                    // so the PTY size matches what xterm.js is displaying.
                    if (tab.term.cols && tab.term.rows) {
                        try {
                            ws.send(JSON.stringify({ type: 'resize', cols: tab.term.cols, rows: tab.term.rows }));
                        } catch (e) {}
                    }
                }
            };

            ws.onmessage = function (ev) {
                if (!tab.term) return;
                // Handle binary messages and text messages
                if (ev.data instanceof ArrayBuffer) {
                    var decoder = new TextDecoder('utf-8');
                    writeTermData(tab, decoder.decode(ev.data));
                } else if (ev.data instanceof Blob) {
                    // Blob type, needs to be read asynchronously
                    var reader = new FileReader();
                    reader.onload = function () {
                        var decoder = new TextDecoder('utf-8');
                        writeTermData(tab, decoder.decode(reader.result));
                    };
                    reader.readAsArrayBuffer(ev.data);
                } else {
                    // string type
                    writeTermData(tab, ev.data);
                }
            };

            ws.onclose = function () {
                tab.running = false;
                if (tab.term) {
                    tab.term.writeln('\r\n\x1b[2m' + tr('settingsTerminal.sessionClosed') + '\x1b[0m');
                }
            };

            ws.onerror = function () {
                tab.running = false;
                if (tab.term) {
                    tab.term.writeln('\r\n\x1b[31m' + tr('settingsTerminal.connectionError') + '\x1b[0m');
                }
            };
        } catch (e) {
            if (tab.term) {
                tab.term.writeln('\r\n\x1b[31m' + tr('settingsTerminal.connectFailed', { msg: String(e) }) + '\x1b[0m');
            }
        }
    }

    function createTerminalInContainer(container, tab) {
        if (typeof Terminal === 'undefined') return null;
        if (!tab.history) tab.history = [];
        if (tab.historyIndex === undefined) tab.historyIndex = -1;
        if (tab.cursorIndex === undefined) tab.cursorIndex = 0;

        var term = new Terminal({
            cursorBlink: true,
            cursorStyle: 'bar',
            fontSize: 13,
            fontFamily: 'Menlo, Monaco, "Courier New", monospace',
            lineHeight: 1.2,
            smoothScrollDuration: 0,
            scrollSensitivity: 1,
            fastScrollSensitivity: 5,
            scrollback: 1000,
            theme: {
                background: '#0d1117',
                foreground: '#e6edf3',
                cursor: '#58a6ff',
                cursorAccent: '#0d1117',
                selection: 'rgba(88, 166, 255, 0.3)',
                black: '#484f58',
                red: '#ff7b72',
                green: '#3fb950',
                yellow: '#d29922',
                blue: '#58a6ff',
                magenta: '#bc8cff',
                cyan: '#39c5cf',
                white: '#e6edf3',
                brightBlack: '#6e7681',
                brightRed: '#ffa198',
                brightGreen: '#56d364',
                brightYellow: '#e3b341',
                brightBlue: '#79c0ff',
                brightMagenta: '#d2a8ff',
                brightCyan: '#56d4dd',
                brightWhite: '#f0f6fc'
            }
        });
        var fitAddon = null;
        if (typeof FitAddon !== 'undefined') {
            var FitCtor = (FitAddon.FitAddon || FitAddon);
            fitAddon = new FitCtor();
            term.loadAddon(fitAddon);
        }
        term.open(container);
        term.write(getWelcomeLine());
        container.addEventListener('click', function () {
            switchTerminalTab(tab.id);
            if (term) term.focus();
        });
        container.setAttribute('tabindex', '0');
        container.title = tr('settingsTerminal.containerClickTitle');

        function sendToWS(data) {
            ensureTerminalWS(tab);
            if (tab.ws && tab.ws.readyState === WebSocket.OPEN) {
                try {
                    tab.ws.send(data);
                } catch (e) {}
            }
        }

        function sendResize() {
            if (tab.ws && tab.ws.readyState === WebSocket.OPEN && term.cols && term.rows) {
                try {
                    tab.ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
                } catch (e) {}
            }
        }

        term.onData(function (data) {
            // Ctrl+L: clears the screen locally, and also sends ^L to the backend
            if (data === '\x0c') {
                term.clear();
                sendToWS(data);
                return;
            }
            sendToWS(data);
        });

        // Notify backend when the terminal is resized so the PTY dimensions stay in sync.
        // This is critical for full-screen programs like vi/vim/less to render correctly.
        term.onResize(function (size) {
            sendResize();
        });

        tab.term = term;
        tab.fitAddon = fitAddon;
        if (typeof ResizeObserver !== 'undefined' && fitAddon) {
            var resizeTimer;
            tab.resizeObserver = new ResizeObserver(function () {
                clearTimeout(resizeTimer);
                resizeTimer = setTimeout(function () {
                    try {
                        fitAddon.fit();
                        term.scrollToBottom();
                    } catch (e) {}
                }, 50);
            });
            tab.resizeObserver.observe(container);
        }
        // Immediately establish the WebSocket so the backend PTY/Shell starts right away and outputs the prompt；
        // If we wait until the first keypress to connect, the user will feel they must press Enter first before they can type (actually, the connection has not yet been established).
        ensureTerminalWS(tab);
        return term;
    }

    function switchTerminalTab(id) {
        var prevId = currentTabId;
        currentTabId = id;
        document.querySelectorAll('.terminal-tab').forEach(function (el) {
            el.classList.toggle('active', parseInt(el.getAttribute('data-tab-id'), 10) === id);
        });
        document.querySelectorAll('.terminal-pane').forEach(function (el) {
            var paneId = el.getAttribute('id');
            var match = paneId && paneId.match(/terminal-pane-(\d+)/);
            var paneTabId = match ? parseInt(match[1], 10) : 0;
            el.classList.toggle('active', paneTabId === id);
        });
        var t = getCurrent();
        if (t && t.term) {
            try {
                if (t.fitAddon) t.fitAddon.fit();
                t.term.scrollToBottom();
            } catch (e) {}
            if (prevId !== id) {
                requestAnimationFrame(function () {
                    if (currentTabId === id && t.term) {
                        try {
                            if (t.fitAddon) t.fitAddon.fit();
                            t.term.scrollToBottom();
                        } catch (e) {}
                        t.term.focus();
                    }
                });
            } else {
                t.term.focus();
            }
        }
    }

    function addTerminalTab() {
        if (typeof Terminal === 'undefined') return;
        tabIdCounter += 1;
        var id = tabIdCounter;
        var paneId = 'terminal-pane-' + id;
        var containerId = 'terminal-container-' + id;
        var tabsEl = document.querySelector('.terminal-tabs');
        var panesEl = document.querySelector('.terminal-panes');
        if (!tabsEl || !panesEl) return;

        var tabDiv = document.createElement('div');
        tabDiv.className = 'terminal-tab';
        tabDiv.setAttribute('data-tab-id', String(id));
        var label = document.createElement('span');
        label.className = 'terminal-tab-label';
        label.textContent = tr('settingsTerminal.terminalTab', { n: id });
        label.onclick = function () { switchTerminalTab(id); };
        var closeBtn = document.createElement('button');
        closeBtn.type = 'button';
        closeBtn.className = 'terminal-tab-close';
        closeBtn.title = tr('settingsTerminal.closeTabTitle');
        closeBtn.textContent = '×';
        closeBtn.onclick = function (e) { e.stopPropagation(); removeTerminalTab(id); };
        tabDiv.appendChild(label);
        tabDiv.appendChild(closeBtn);
        var plusBtn = tabsEl.querySelector('.terminal-tab-new');
        tabsEl.insertBefore(tabDiv, plusBtn);

        var paneDiv = document.createElement('div');
        paneDiv.id = paneId;
        paneDiv.className = 'terminal-pane';
        var containerDiv = document.createElement('div');
        containerDiv.id = containerId;
        containerDiv.className = 'terminal-container';
        paneDiv.appendChild(containerDiv);
        panesEl.appendChild(paneDiv);

        var tab = { id: id, paneId: paneId, containerId: containerId, lineBuffer: '', cursorIndex: 0, running: false, term: null, fitAddon: null, history: [], historyIndex: -1 };
        terminals.push(tab);
        createTerminalInContainer(containerDiv, tab);
        switchTerminalTab(id);
        updateTerminalTabCloseVisibility();
        setTimeout(function () {
            try { if (tab.fitAddon) tab.fitAddon.fit(); if (tab.term) tab.term.focus(); } catch (e) {}
        }, 50);
    }

    function updateTerminalTabCloseVisibility() {
        var tabsEl = document.querySelector('.terminal-tabs');
        if (!tabsEl) return;
        var tabDivs = tabsEl.querySelectorAll('.terminal-tab');
        var showClose = terminals.length > 1;
        for (var i = 0; i < tabDivs.length; i++) {
            var btn = tabDivs[i].querySelector('.terminal-tab-close');
            if (btn) btn.style.display = showClose ? '' : 'none';
        }
    }

    function removeTerminalTab(id) {
        if (terminals.length <= 1) return;
        var idx = -1;
        for (var i = 0; i < terminals.length; i++) { if (terminals[i].id === id) { idx = i; break; } }
        if (idx < 0) return;

        var deletingCurrent = (currentTabId === id);
        var switchToIndex = deletingCurrent ? (idx > 0 ? idx - 1 : 0) : -1;

        var tab = terminals[idx];
        if (tab.resizeObserver && tab.resizeObserver.disconnect) tab.resizeObserver.disconnect();
        if (tab.term && tab.term.dispose) tab.term.dispose();
        tab.term = null;
        tab.fitAddon = null;
        tab.resizeObserver = null;
        terminals.splice(idx, 1);

        var tabDiv = document.querySelector('.terminal-tab[data-tab-id="' + id + '"]');
        var paneDiv = document.getElementById('terminal-pane-' + id);
        if (tabDiv && tabDiv.parentNode) tabDiv.parentNode.removeChild(tabDiv);
        if (paneDiv && paneDiv.parentNode) paneDiv.parentNode.removeChild(paneDiv);

        var curIdxBeforeRenumber = -1;
        if (!deletingCurrent) {
            for (var i = 0; i < terminals.length; i++) {
                if (terminals[i].id === currentTabId) { curIdxBeforeRenumber = i; break; }
            }
        }

        for (var i = 0; i < terminals.length; i++) {
            var t = terminals[i];
            t.id = i + 1;
            t.paneId = 'terminal-pane-' + (i + 1);
            t.containerId = 'terminal-container-' + (i + 1);
        }
        tabIdCounter = terminals.length;
        if (curIdxBeforeRenumber >= 0) currentTabId = terminals[curIdxBeforeRenumber].id;

        var tabsEl = document.querySelector('.terminal-tabs');
        var panesEl = document.querySelector('.terminal-panes');
        if (tabsEl) {
            var tabDivs = tabsEl.querySelectorAll('.terminal-tab');
            for (var i = 0; i < tabDivs.length; i++) {
                var t = terminals[i];
                tabDivs[i].setAttribute('data-tab-id', String(t.id));
                var lbl = tabDivs[i].querySelector('.terminal-tab-label');
                if (lbl) lbl.textContent = tr('settingsTerminal.terminalTab', { n: t.id });
                if (lbl) lbl.onclick = (function (tid) { return function () { switchTerminalTab(tid); }; })(t.id);
                var cb = tabDivs[i].querySelector('.terminal-tab-close');
                if (cb) cb.onclick = (function (tid) { return function (e) { e.stopPropagation(); removeTerminalTab(tid); }; })(t.id);
            }
        }
        if (panesEl) {
            var paneDivs = panesEl.querySelectorAll('.terminal-pane');
            for (var i = 0; i < paneDivs.length; i++) {
                var t = terminals[i];
                paneDivs[i].id = t.paneId;
                var cont = paneDivs[i].querySelector('.terminal-container');
                if (cont) cont.id = t.containerId;
            }
        }

        updateTerminalTabCloseVisibility();

        if (deletingCurrent && terminals.length > 0) {
            currentTabId = terminals[switchToIndex].id;
            switchTerminalTab(currentTabId);
        }
    }

    function escapeHtml(s) {
        return String(s)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;');
    }

    function refreshTerminalI18n() {
        // Update tab and container title after language switch; don't force-clear the content of already-opened terminals, to avoid losing session output
        try {
            var tabsEl = document.querySelector('.terminal-tabs');
            if (tabsEl) {
                var tabDivs = tabsEl.querySelectorAll('.terminal-tab');
                for (var i = 0; i < tabDivs.length && i < terminals.length; i++) {
                    var tid = terminals[i].id;
                    var lbl = tabDivs[i].querySelector('.terminal-tab-label');
                    if (lbl) lbl.textContent = tr('settingsTerminal.terminalTab', { n: tid });
                    var cb = tabDivs[i].querySelector('.terminal-tab-close');
                    if (cb) cb.title = tr('settingsTerminal.closeTabTitle');
                }
            }
            terminals.forEach(function (tab) {
                if (!tab || !tab.term) return;
                var cont = document.getElementById(tab.containerId);
                if (cont) cont.title = tr('settingsTerminal.containerClickTitle');
            });
        } catch (e) { /* ignore */ }
    }

    document.addEventListener('languagechange', function () {
        refreshTerminalI18n();
    });

    function initTerminal() {
        var pane1 = document.getElementById('terminal-pane-1');
        var container1 = document.getElementById('terminal-container-1');
        if (!pane1 || !container1) return;
        if (inited) {
            var t = getCurrent();
            if (t && t.term) t.term.focus();
            terminals.forEach(function (tab) { try { if (tab.fitAddon) tab.fitAddon.fit(); } catch (e) {} });
            return;
        }
        inited = true;

        if (typeof Terminal === 'undefined') {
            container1.innerHTML = '<p class="terminal-error">' + escapeHtml(tr('settingsTerminal.xtermNotLoaded')) + '</p>';
            return;
        }

        currentTabId = 1;
        var tab = { id: 1, paneId: 'terminal-pane-1', containerId: 'terminal-container-1', lineBuffer: '', cursorIndex: 0, running: false, term: null, fitAddon: null, history: [], historyIndex: -1 };
        terminals.push(tab);
        createTerminalInContainer(container1, tab);

        updateTerminalTabCloseVisibility();

        refreshTerminalI18n();

        setTimeout(function () {
            try { if (tab.fitAddon) tab.fitAddon.fit(); if (tab.term) tab.term.focus(); } catch (e) {}
        }, 100);

        var resizeTimer;
        window.addEventListener('resize', function () {
            clearTimeout(resizeTimer);
            resizeTimer = setTimeout(function () {
                terminals.forEach(function (t) { try { if (t.fitAddon) t.fitAddon.fit(); } catch (e) {} });
            }, 150);
        });
    }

    function terminalClear() {
        var t = getCurrent();
        if (!t || !t.term) return;
        t.term.clear();
        t.lineBuffer = '';
        if (t.cursorIndex !== undefined) t.cursorIndex = 0;
        writePrompt(t);
        t.term.focus();
    }

    window.initTerminal = initTerminal;
    window.terminalClear = terminalClear;
    window.switchTerminalTab = switchTerminalTab;
    window.addTerminalTab = addTerminalTab;
    window.removeTerminalTab = removeTerminalTab;
})();
