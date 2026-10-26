// C2 模块前端逻辑 - 完整实现
// 支持: xterm 终端、文件管理、监听器/会话/任务/事件/Payload/Profile 管理

(function() {
    'use strict';

    // C2 模块命名空间
    const C2 = {
        currentPage: '',
        listeners: [],
        sessions: [],
        tasks: [],
        tasksPage: 1,
        tasksPageSize: 10,
        tasksTotal: 0,
        tasksPendingQueuedCount: null,
        tasksStatusCounts: { success: 0, failed: 0, pending: 0, cancelled: 0 },
        tasksFilter: { status: '', task_type: '', session_id: '', since: '', timePreset: 'all' },
        events: [],
        eventsPage: 1,
        eventsPageSize: 10,
        eventsTotal: 0,
        eventsLevelCounts: { info: 0, warn: 0, critical: 0 },
        eventsFilter: { level: '', category: '', session_id: '', since: '', timePreset: 'all' },
        profiles: [],
        selectedSessionId: null,
        selectedListenerId: null,
        sessionFilter: { status: '', listener_id: '', search: '', suspicious: false },
        eventSource: null,
        // xterm 相关
        terminalInstance: null,
        terminalFitAddon: null,
        terminalResizeObserver: null,
        terminalContainer: null,
        terminalSessionId: null,
        terminalHistory: {},
        terminalLogs: {},
        terminalBusy: false,
        terminalQueue: [],
        // 文件管理
        currentPath: '.',
        implantPwd: null,
        fileList: [],
        fileUploadBusy: false,
        // 任务轮询
        taskPollInterval: null,
    };

    // API 基础路径
    const API_BASE = '/api/c2';

    function activeC2ProjectId() {
        try {
            return (typeof getActiveProjectId === 'function' ? getActiveProjectId() : '') || '';
        } catch (e) {
            return '';
        }
    }

    function c2ResourceProjectId(item) {
        return String((item && (item.project_id || item.projectId)) || '').trim();
    }

    function c2LooksLikeUuid(value) {
        return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(String(value || '').trim());
    }

    function c2ProjectNameMap() {
        try {
            return (window.projectNameById && typeof window.projectNameById === 'object')
                ? window.projectNameById
                : {};
        } catch (e) {
            return {};
        }
    }

    /** 下拉展示用项目名：优先可读名称，绝不回退成 UUID */
    function c2ProjectDisplayName(id, nameHint) {
        const idStr = String(id || '').trim();
        if (!idStr) return '';
        let name = String(nameHint || '').trim();
        if (!name || name === idStr || c2LooksLikeUuid(name)) {
            const mapped = String(c2ProjectNameMap()[idStr] || '').trim();
            if (mapped && mapped !== idStr && !c2LooksLikeUuid(mapped)) name = mapped;
        }
        if (!name || name === idStr || c2LooksLikeUuid(name)) {
            if (typeof window.getProjectName === 'function') {
                const viaFn = String(window.getProjectName(idStr) || '').trim();
                if (viaFn && viaFn !== idStr && !c2LooksLikeUuid(viaFn)) name = viaFn;
            }
        }
        if (!name || name === idStr || c2LooksLikeUuid(name)) {
            return c2t('batchManageModal.unknownProject') || '未知项目';
        }
        return name;
    }

    function c2ProjectOptionsHtml(selectedId, projectList) {
        const selected = String(selectedId || '').trim();
        let html = `<option value="">${escapeHtml(c2t('assets.unboundProject') || '暂不绑定')}</option>`;
        let entries = [];
        if (Array.isArray(projectList) && projectList.length) {
            entries = projectList
                .filter((p) => p && p.id)
                .map((p) => [String(p.id), String(p.name || '').trim()]);
        } else {
            entries = Object.entries(c2ProjectNameMap());
        }
        const seen = new Set();
        entries.sort((a, b) => c2ProjectDisplayName(a[0], a[1]).localeCompare(c2ProjectDisplayName(b[0], b[1]), undefined, { sensitivity: 'base' }));
        entries.forEach(([id, name]) => {
            if (!id || seen.has(id)) return;
            seen.add(id);
            const label = c2ProjectDisplayName(id, name);
            html += `<option value="${escapeAttr(id)}"${id === selected ? ' selected' : ''}>${escapeHtml(label)}</option>`;
        });
        if (selected && !seen.has(selected)) {
            html += `<option value="${escapeAttr(selected)}" selected>${escapeHtml(c2ProjectDisplayName(selected))}</option>`;
        }
        return html;
    }

    function ensureC2ProjectsLoaded() {
        if (typeof window.ensureProjectsLoaded === 'function') return window.ensureProjectsLoaded();
        if (typeof window.fetchAllProjects === 'function') {
            return window.fetchAllProjects(false).then((list) => {
                if (typeof window.rebuildProjectNameMap === 'function') window.rebuildProjectNameMap(list || []);
                return list || [];
            });
        }
        return Promise.resolve([]);
    }

    /** 确保当前选中项目有可读名称（孤儿 / 未进缓存的 ID） */
    function ensureC2SelectedProjectNamed(projectId) {
        const id = String(projectId || '').trim();
        if (!id) return Promise.resolve();
        const mapped = String(c2ProjectNameMap()[id] || '').trim();
        if (mapped && mapped !== id && !c2LooksLikeUuid(mapped)) return Promise.resolve();
        if (typeof window.fetchProjectSummary !== 'function') return Promise.resolve();
        return window.fetchProjectSummary(id).then((p) => {
            if (p && p.id && typeof window.rememberProjectsInNameMap === 'function') {
                window.rememberProjectsInNameMap([p]);
            }
        }).catch(function () { /* ignore */ });
    }

    function c2ProjectBindSelectHtml(listener) {
        return `<select class="c2-project-bind-select" data-id="${escapeAttr(listener.id || '')}" title="${escapeAttr(c2t('assets.project') || '所属项目')}" onclick="event.stopPropagation()" onchange="C2.bindListenerProject(this.dataset.id, this.value)">${c2ProjectOptionsHtml(c2ResourceProjectId(listener))}</select>`;
    }

    function withC2ProjectQuery(url) {
        return url;
    }

    window.__c2DownloadPayload = function(filename) {
        const url = `${API_BASE}/payloads/${filename}/download`;
        const fetchFn = (typeof apiFetch === 'function') ? apiFetch : fetch;
        fetchFn(url).then(resp => {
            if (!resp.ok) throw new Error('download failed: ' + resp.status);
            return resp.blob();
        }).then(blob => {
            const a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            URL.revokeObjectURL(a.href);
        }).catch(err => {
            if (window.showToast) window.showToast(err.message, 'error');
        });
    };

    function c2t(key, opts) {
        try {
            if (typeof window.t === 'function') return window.t(key, opts || {});
        } catch (e) {}
        return key;
    }

    const c2FormSelectMap = {};
    let c2FormSelectDocBound = false;
    const C2_FORM_SELECT_CARET = '<svg class="c2-form-select-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    const C2_FORM_SELECT_HANDLERS = {
        'c2-listener-type': function () {
            C2.syncListenerProfileRowForType();
        }
    };

    function closeAllC2FormSelects() {
        Object.keys(c2FormSelectMap).forEach(function (id) {
            const reg = c2FormSelectMap[id];
            if (!reg || !reg.wrapper) return;
            reg.wrapper.classList.remove('open');
            if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
        });
    }

    function syncC2FormSelect(selectId) {
        const reg = c2FormSelectMap[selectId];
        if (!reg) return;
        const select = reg.select;
        const dropdown = reg.dropdown;
        const trigger = reg.trigger;
        const valueSpan = trigger.querySelector('.c2-form-select-value');

        dropdown.innerHTML = '';
        Array.prototype.forEach.call(select.options, function (opt) {
            const item = document.createElement('button');
            item.type = 'button';
            item.className = 'c2-form-select-option';
            item.setAttribute('role', 'option');
            item.setAttribute('data-value', opt.value);
            if (opt.value === select.value) {
                item.classList.add('is-selected');
                item.setAttribute('aria-selected', 'true');
            } else {
                item.setAttribute('aria-selected', 'false');
            }
            const check = document.createElement('span');
            check.className = 'c2-form-select-check';
            check.setAttribute('aria-hidden', 'true');
            check.textContent = '✓';
            const label = document.createElement('span');
            label.className = 'c2-form-select-label';
            label.textContent = opt.textContent;
            item.appendChild(check);
            item.appendChild(label);
            dropdown.appendChild(item);
        });

        const selectedOpt = select.options[select.selectedIndex];
        if (valueSpan) {
            valueSpan.textContent = selectedOpt ? selectedOpt.textContent : '';
        }
        trigger.disabled = !!select.disabled;
        reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
    }

    function enhanceC2FormSelect(select) {
        if (!select || !select.id) return;
        const existing = c2FormSelectMap[select.id];
        if (existing && existing.select !== select) {
            delete c2FormSelectMap[select.id];
        }
        if (select.dataset.c2FormCustom === '1') {
            syncC2FormSelect(select.id);
            return;
        }
        select.dataset.c2FormCustom = '1';
        select.classList.add('c2-form-native-select');
        select.tabIndex = -1;
        select.setAttribute('aria-hidden', 'true');

        const wrapper = document.createElement('div');
        wrapper.className = 'c2-form-select-ui';

        const trigger = document.createElement('button');
        trigger.type = 'button';
        trigger.className = 'c2-form-select-trigger';
        trigger.setAttribute('aria-haspopup', 'listbox');
        trigger.setAttribute('aria-expanded', 'false');
        const valueSpan = document.createElement('span');
        valueSpan.className = 'c2-form-select-value';
        trigger.appendChild(valueSpan);
        trigger.insertAdjacentHTML('beforeend', C2_FORM_SELECT_CARET);

        const dropdown = document.createElement('div');
        dropdown.className = 'c2-form-select-dropdown';
        dropdown.setAttribute('role', 'listbox');

        const parent = select.parentNode;
        parent.insertBefore(wrapper, select);
        wrapper.appendChild(trigger);
        wrapper.appendChild(dropdown);
        wrapper.appendChild(select);

        c2FormSelectMap[select.id] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

        trigger.addEventListener('click', function (e) {
            e.stopPropagation();
            if (select.disabled) return;
            const open = wrapper.classList.contains('open');
            closeAllC2FormSelects();
            if (!open) {
                wrapper.classList.add('open');
                trigger.setAttribute('aria-expanded', 'true');
            }
        });

        dropdown.addEventListener('click', function (e) {
            const opt = e.target.closest('.c2-form-select-option');
            if (!opt) return;
            e.stopPropagation();
            const val = opt.getAttribute('data-value');
            if (val === null) return;
            if (select.value !== val) {
                select.value = val;
                select.dispatchEvent(new Event('change', { bubbles: true }));
            }
            wrapper.classList.remove('open');
            trigger.setAttribute('aria-expanded', 'false');
            syncC2FormSelect(select.id);
        });

        select.addEventListener('change', function () {
            syncC2FormSelect(select.id);
        });

        if (!select.dataset.c2FormFilterBound) {
            select.dataset.c2FormFilterBound = '1';
            const handler = C2_FORM_SELECT_HANDLERS[select.id];
            if (typeof handler === 'function') {
                select.addEventListener('change', handler);
            }
        }

        syncC2FormSelect(select.id);
    }

    C2.refreshFormSelects = function (root) {
        const container = root || document.getElementById('c2-modal-content');
        if (!container) return;
        Object.keys(c2FormSelectMap).forEach(function (id) {
            if (!document.getElementById(id)) delete c2FormSelectMap[id];
        });
        container.querySelectorAll('select.c2-form-select-native').forEach(enhanceC2FormSelect);
        if (!c2FormSelectDocBound) {
            c2FormSelectDocBound = true;
            document.addEventListener('click', closeAllC2FormSelects);
            document.addEventListener('keydown', function (e) {
                if (e.key === 'Escape') closeAllC2FormSelects();
            });
        }
    };

    function listenerTypeLabel(type) {
        if (!type) return '';
        const k = 'c2.listeners.typeLabels.' + String(type).toLowerCase();
        const tr = c2t(k);
        if (tr !== k) return tr;
        return String(type).replace(/_/g, ' ');
    }

    function sessionStatusLabel(status) {
        const s = String(status || '').toLowerCase();
        if (!s) return '';
        const k = 'c2.sessions.' + s;
        const tr = c2t(k);
        if (tr !== k) return tr;
        return status;
    }

    function taskStatusLabel(status) {
        const s = String(status || '').toLowerCase();
        if (!s) return '';
        const k = 'c2.tasks.' + s;
        const tr = c2t(k);
        if (tr !== k) return tr;
        return status;
    }

    function formatTaskCommand(task) {
        if (!task) return '';
        const type = String(task.taskType || '').toLowerCase();
        const p = task.payload;
        if (!p || typeof p !== 'object' || Object.keys(p).length === 0) {
            if (type === 'pwd' || type === 'ps' || type === 'screenshot') return type;
            return '';
        }
        switch (type) {
            case 'shell':
            case 'exec':
                return p.command != null ? String(p.command) : '';
            case 'ls':
            case 'cd':
                return p.path != null ? String(p.path) : '';
            case 'download':
                return p.remote_path != null ? String(p.remote_path) : '';
            case 'upload':
                if (p.remote_path) return String(p.remote_path);
                if (p.file_id) return 'file:' + String(p.file_id);
                return '';
            case 'kill_proc':
                return p.pid != null ? 'pid:' + String(p.pid) : '';
            case 'sleep':
                let sleepStr = p.seconds != null ? 'sleep ' + p.seconds + 's' : '';
                if (p.jitter != null) sleepStr += (sleepStr ? ', ' : '') + 'jitter ' + p.jitter + '%';
                return sleepStr;
            case 'port_fwd':
                return [p.action, p.remote_host, p.remote_port, p.local_port].filter(v => v != null && v !== '').join(':');
            case 'socks_start':
            case 'socks_stop':
                return p.port != null ? 'port:' + String(p.port) : type;
            case 'load_assembly':
                if (p.args) return String(p.args);
                if (p.file_id) return 'file:' + String(p.file_id);
                return '';
            case 'persist':
                return p.method != null ? String(p.method) : '';
            default:
                try { return JSON.stringify(p); } catch (e) { return ''; }
        }
    }

    function truncateCommand(cmd, maxLen) {
        if (!cmd) return '';
        const s = String(cmd);
        if (!maxLen || s.length <= maxLen) return s;
        return s.substring(0, maxLen - 1) + '\u2026';
    }

    function taskTypeCategory(type) {
        const t = String(type || '').toLowerCase();
        if (t === 'shell' || t === 'exec') return 'shell';
        if (t === 'ls' || t === 'cd' || t === 'pwd' || t === 'download' || t === 'upload') return 'fs';
        if (t === 'sleep' || t === 'exit' || t === 'kill_proc') return 'control';
        return 'default';
    }

    function formatRelativeTime(value) {
        const ms = value ? new Date(value).getTime() : 0;
        if (!Number.isFinite(ms) || ms <= 0) return '';
        const diff = Date.now() - ms;
        if (diff < 60000) return c2t('c2.common.justNow');
        if (diff < 3600000) return c2t('c2.common.minutesAgo', { n: Math.floor(diff / 60000) });
        if (diff < 86400000) return c2t('c2.common.hoursAgo', { n: Math.floor(diff / 3600000) });
        return formatTime(value);
    }

    function sessionOsKey(os) {
        const o = String(os || '').toLowerCase();
        if (o.includes('darwin') || o === 'macos' || o === 'mac') return 'darwin';
        if (o.includes('win')) return 'windows';
        if (o.includes('linux') || o.includes('unix')) return 'linux';
        return 'default';
    }

    function sessionOsAvatarLabel(os) {
        const k = sessionOsKey(os);
        if (k === 'darwin') return 'mac';
        if (k === 'windows') return 'win';
        if (k === 'linux') return 'nix';
        return String(os || '?').substring(0, 3).toLowerCase();
    }

    function isEmptyInfoValue(value) {
        if (value == null) return true;
        const s = String(value).trim();
        if (!s || s === '-') return true;
        const lower = s.toLowerCase();
        return lower === 'unknown' || lower === 'n/a' || lower === 'null' || lower === 'undefined';
    }

    function displayInfoValue(value, fallback) {
        if (isEmptyInfoValue(value)) return fallback != null ? fallback : '—';
        return String(value);
    }

    function sessionMetaLine(s) {
        const user = displayInfoValue(s.username, '');
        const os = displayInfoValue(s.os, '');
        const arch = displayInfoValue(s.arch, '');
        const right = [os, arch].filter(Boolean).join('/') || '—';
        if (!user) return right;
        return user + '@' + right;
    }

    function sessionInfoRow(label, value, opts) {
        const empty = isEmptyInfoValue(value);
        const display = empty ? '—' : String(value);
        const valueClasses = ['c2-session-info-dl__value'];
        if (opts && opts.mono) valueClasses.push('is-mono');
        if (opts && opts.accent && !empty) valueClasses.push('is-accent');
        if (opts && opts.warn && !empty) valueClasses.push('is-warn');
        if (empty) valueClasses.push('is-empty');
        const rowCls = opts && opts.full ? ' c2-session-info-dl__row--full' : '';
        const copyBtn = (opts && opts.copy && !empty)
            ? `<button type="button" class="c2-session-info-copy" title="${escapeAttr(c2t('c2.sessions.infoCopy'))}" aria-label="${escapeAttr(c2t('c2.sessions.infoCopy'))}" data-c2-copy-value="${escapeAttr(String(value))}">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" stroke="currentColor" stroke-width="1.8"/><path d="M6 15H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
               </button>`
            : '';
        return `
            <div class="c2-session-info-dl__row${rowCls}">
                <dt class="c2-session-info-dl__label">${escapeHtml(label)}</dt>
                <dd class="${valueClasses.join(' ')}">
                    <span class="c2-session-info-dl__text" title="${escapeAttr(empty ? '' : display)}">${escapeHtml(display)}</span>
                    ${copyBtn}
                </dd>
            </div>`;
    }

    function renderSessionInfoPanel(s, adminVal, sleepLine) {
        const lastCheckin = formatTime(s.lastCheckIn);
        const lastRel = formatRelativeTime(s.lastCheckIn);
        const checkinDisplay = lastRel ? `${lastCheckin} · ${lastRel}` : lastCheckin;
        const noteText = s.note && String(s.note).trim() ? String(s.note) : '';
        const noteEmpty = !noteText;
        return `
            <div class="c2-session-info-panel">
                <div class="c2-session-info-grid">
                    <section class="c2-session-info-block">
                        <div class="c2-session-info-block__head">
                            <span class="c2-session-info-block__icon c2-session-info-block__icon--id" aria-hidden="true"></span>
                            <span>${escapeHtml(c2t('c2.sessions.infoSectionIdentity'))}</span>
                        </div>
                        <dl class="c2-session-info-dl">
                            ${sessionInfoRow(c2t('c2.sessions.infoSessionId'), s.id, { mono: true, accent: true, copy: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoImplantUuid'), s.implantUuid, { mono: true, copy: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoHostname'), s.hostname)}
                            ${sessionInfoRow(c2t('c2.sessions.infoUsername'), s.username)}
                        </dl>
                    </section>
                    <section class="c2-session-info-block">
                        <div class="c2-session-info-block__head">
                            <span class="c2-session-info-block__icon c2-session-info-block__icon--sys" aria-hidden="true"></span>
                            <span>${escapeHtml(c2t('c2.sessions.infoSectionSystem'))}</span>
                        </div>
                        <dl class="c2-session-info-dl">
                            ${sessionInfoRow(c2t('c2.sessions.infoOs'), s.os)}
                            ${sessionInfoRow(c2t('c2.sessions.infoArch'), s.arch, { mono: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoPid'), s.pid, { mono: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoProcess'), s.processName || '-', { mono: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoAdmin'), adminVal, { warn: !!s.isAdmin })}
                        </dl>
                    </section>
                    <section class="c2-session-info-block">
                        <div class="c2-session-info-block__head">
                            <span class="c2-session-info-block__icon c2-session-info-block__icon--net" aria-hidden="true"></span>
                            <span>${escapeHtml(c2t('c2.sessions.infoSectionNetwork'))}</span>
                        </div>
                        <dl class="c2-session-info-dl">
                            ${sessionInfoRow(c2t('c2.sessions.infoInternalIp'), s.internalIp || '-', { mono: true, accent: true, copy: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoSleep'), sleepLine)}
                            ${sessionInfoRow(c2t('c2.sessions.infoFirstSeen'), formatTime(s.firstSeenAt), { mono: true })}
                            ${sessionInfoRow(c2t('c2.sessions.infoLastCheckin'), checkinDisplay, { mono: true, accent: true })}
                        </dl>
                    </section>
                </div>
                <section class="c2-session-info-block c2-session-info-block--note${noteEmpty ? ' is-empty' : ''}" id="c2-session-note-block">
                    <div class="c2-session-info-block__head">
                        <span class="c2-session-info-block__icon c2-session-info-block__icon--note" aria-hidden="true"></span>
                        <span>${escapeHtml(c2t('c2.sessions.infoSectionNote'))}</span>
                        <button type="button" class="c2-session-note-edit-btn" data-require-permission="c2:write" data-c2-action="session-note-edit" data-c2-id="${escapeAttr(s.id)}">${escapeHtml(c2t('c2.sessions.noteEdit'))}</button>
                    </div>
                    <div class="c2-session-info-note${noteEmpty ? ' is-empty' : ''}" id="c2-session-note-view">${escapeHtml(noteText || c2t('c2.sessions.infoNoteEmpty'))}</div>
                    <div class="c2-session-note-editor" id="c2-session-note-editor" hidden>
                        <textarea id="c2-session-note-input" class="c2-session-note-textarea" maxlength="2000" rows="4" placeholder="${escapeAttr(c2t('c2.sessions.notePlaceholder'))}">${escapeHtml(noteText)}</textarea>
                        <div class="c2-session-note-editor__footer">
                            <span class="c2-session-note-counter" id="c2-session-note-counter">${noteText.length}/2000</span>
                            <div class="c2-session-note-editor__actions">
                                <button type="button" class="btn-secondary btn-sm" onclick="C2.cancelEditSessionNote()">${escapeHtml(c2t('common.cancel'))}</button>
                                <button type="button" class="btn-primary btn-sm" id="c2-session-note-save" data-c2-action="session-note-save" data-c2-id="${escapeAttr(s.id)}">${escapeHtml(c2t('common.save'))}</button>
                            </div>
                        </div>
                    </div>
                </section>
            </div>`;
    }

    // ============================================================================
    // 工具函数
    // ============================================================================

    function apiRequest(method, url, data) {
        if (method === 'GET') url = withC2ProjectQuery(url);
        const options = {
            method: method,
            headers: { 'Content-Type': 'application/json' }
        };
        if (data && (method === 'POST' || method === 'PUT' || method === 'PATCH' || method === 'DELETE')) {
            options.body = JSON.stringify(data);
        }
        if (typeof apiFetch === 'function') {
            return apiFetch(url, options).then(r => r.json());
        }
        return fetch(url, options).then(r => r.json());
    }

    function showToast(message, type = 'info') {
        if (window.showToast) {
            window.showToast(message, type);
            return;
        }
        const container = document.getElementById('c2-toast-container') || (() => {
            const div = document.createElement('div');
            div.id = 'c2-toast-container';
            div.style.cssText = 'position:fixed;top:20px;right:20px;z-index:10100;display:flex;flex-direction:column;gap:8px;';
            document.body.appendChild(div);
            return div;
        })();
        container.style.zIndex = '10100';
        const toast = document.createElement('div');
        const colors = { error: '#e53e3e', success: '#38a169', info: '#3182ce', warn: '#d69e2e' };
        toast.style.cssText = `background:${colors[type] || colors.info};color:#fff;padding:10px 18px;border-radius:6px;font-size:0.875rem;box-shadow:0 4px 12px rgba(0,0,0,0.2);opacity:0;transition:opacity .3s;max-width:400px;word-break:break-word;`;
        toast.textContent = message;
        container.appendChild(toast);
        requestAnimationFrame(() => { toast.style.opacity = '1'; });
        setTimeout(() => {
            toast.style.opacity = '0';
            setTimeout(() => toast.remove(), 300);
        }, 3500);
    }

    function formatTime(dateStr) {
        if (!dateStr) return '-';
        return new Date(dateStr).toLocaleString();
    }

    function formatDuration(ms) {
        if (!ms || ms <= 0) return '-';
        if (ms < 1000) return c2t('c2.fmt.durationMs', { n: ms });
        if (ms < 60000) return c2t('c2.fmt.durationSec', { n: (ms / 1000).toFixed(1) });
        return c2t('c2.fmt.durationMin', { n: (ms / 60000).toFixed(1) });
    }

    function formatBytes(n) {
        const num = Number(n);
        if (!Number.isFinite(num) || num < 0) return String(n == null ? '-' : n);
        if (num < 1024) return String(Math.round(num)) + ' B';
        const units = ['KB', 'MB', 'GB', 'TB'];
        let v = num / 1024;
        let i = 0;
        while (v >= 1024 && i < units.length - 1) {
            v /= 1024;
            i += 1;
        }
        const digits = v >= 100 ? 0 : (v >= 10 ? 1 : 2);
        return v.toFixed(digits) + ' ' + units[i];
    }

    function escapeHtml(text) {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    function escapeAttr(text) {
        return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
    }

    /** 任务列表操作按钮（查看/取消/删除）— 事件委托 */
    function bindC2TaskActionDelegation() {
        if (document.documentElement.dataset.c2TaskActionsBound === '1') return;
        document.documentElement.dataset.c2TaskActionsBound = '1';
        document.addEventListener('click', function(e) {
	            const btn = e.target.closest('[data-c2-task-action]');
	            if (!btn) return;
	            e.preventDefault();
	            e.stopImmediatePropagation();
            const action = btn.getAttribute('data-c2-task-action');
            const id = btn.getAttribute('data-task-id');
            if (!id) return;
            if (action === 'view') C2.viewTask(id);
            else if (action === 'cancel') C2.cancelTask(id);
            else if (action === 'delete') C2.deleteTaskById(id);
        });
    }
    bindC2TaskActionDelegation();

    /** C2 动态内容操作按钮 — 避免把用户可控值拼入 inline onclick */
    function bindC2SafeActionDelegation() {
        if (document.documentElement.dataset.c2SafeActionsBound === '1') return;
        document.documentElement.dataset.c2SafeActionsBound = '1';
        document.addEventListener('click', function(e) {
            const copyBtn = e.target.closest('[data-c2-copy-value]');
            if (copyBtn) {
                e.preventDefault();
                e.stopPropagation();
                C2.copyText(copyBtn.getAttribute('data-c2-copy-value') || '');
                return;
            }

            const fileBtn = e.target.closest('[data-c2-file-action]');
            if (fileBtn) {
                e.preventDefault();
                e.stopPropagation();
                const name = fileBtn.getAttribute('data-c2-file-name') || '';
                const action = fileBtn.getAttribute('data-c2-file-action');
                if (action === 'open') C2.openDirectory(name);
                else if (action === 'download') C2.downloadFile(name);
                return;
            }

            const stopEl = e.target.closest('[data-c2-stop-action]');
            const actionEl = e.target.closest('[data-c2-action]');
            if (stopEl && (!actionEl || !stopEl.contains(actionEl))) return;
            if (!actionEl) return;
            e.preventDefault();
            e.stopPropagation();
            runC2SafeAction(actionEl);
        });
        document.addEventListener('keydown', function(e) {
            if (e.key !== 'Enter' && e.key !== ' ') return;
            const actionEl = e.target.closest('[data-c2-action][role="button"]');
            if (!actionEl) return;
            e.preventDefault();
            runC2SafeAction(actionEl);
        });
    }
    bindC2SafeActionDelegation();

    function runC2SafeAction(el) {
        const action = el.getAttribute('data-c2-action');
        const id = el.getAttribute('data-c2-id') || '';
        switch (action) {
            case 'session-note-edit': C2.beginEditSessionNote(id); break;
            case 'session-note-save': C2.saveSessionNote(id); break;
            case 'listener-start': C2.startListener(id); break;
            case 'listener-stop': C2.stopListener(id); break;
            case 'listener-edit': C2.editListener(id); break;
            case 'listener-delete': C2.deleteListener(id); break;
            case 'listener-save': C2.saveListener(id); break;
            case 'session-select': C2.selectSession(id); break;
            case 'session-delete': C2.deleteSessionRecord(id); break;
            case 'session-sleep': C2.setSessionSleep(id); break;
            case 'session-kill': C2.killSession(id); break;
            case 'session-tasks-refresh': C2.loadSessionTasks(id); break;
            case 'task-view': C2.viewTask(id); break;
            case 'event-view': C2.viewEvent(id); break;
            case 'event-delete': C2.deleteEventById(id); break;
            case 'profile-delete': C2.deleteProfile(id); break;
            case 'payload-download': {
                if (typeof window.__c2DownloadPayload === 'function') window.__c2DownloadPayload(id);
                break;
            }
        }
    }

    /** 监听器表单：Malleable Profile 下拉选项 HTML（value / 文本已转义） */
    function listenerProfileSelectHtml(selectedProfileId) {
        const sel = selectedProfileId ? String(selectedProfileId) : '';
        let opts = `<option value="">${escapeHtml(c2t('c2.listeners.malleableProfileNone'))}</option>`;
        for (const p of (C2.profiles || [])) {
            if (!p) continue;
            const pid = p.id || p.ID;
            if (!pid) continue;
            const idEsc = escapeAttr(String(pid));
            const nameEsc = escapeHtml(p.name || pid);
            const selected = sel && String(pid) === sel ? ' selected' : '';
            opts += `<option value="${idEsc}"${selected}>${nameEsc}</option>`;
        }
        return opts;
    }

    function listenerResolvedProfileId(l) {
        if (!l) return '';
        const v = l.profileId != null && l.profileId !== '' ? l.profileId : l.profile_id;
        return v != null ? String(v).trim() : '';
    }

    /** 监听器卡片展示用 Profile 名称（依赖 C2.profiles，由 loadListeners 一并拉取） */
    function listenerProfileDisplayName(l) {
        const pid = listenerResolvedProfileId(l);
        if (!pid) return '';
        const list = C2.profiles || [];
        for (let i = 0; i < list.length; i++) {
            const p = list[i];
            if (p && (p.id === pid || p.ID === pid)) return String(p.name || p.id || pid).trim() || pid;
        }
        return pid.length > 18 ? pid.substring(0, 16) + '…' : pid;
    }

    function listenerTypeVisualClass(type) {
        const t = String(type || '').toLowerCase();
        if (t === 'https_beacon') return 'c2-ltype-mark--https';
        if (t === 'http_beacon') return 'c2-ltype-mark--http';
        if (t === 'tcp_reverse') return 'c2-ltype-mark--tcp';
        if (t === 'websocket') return 'c2-ltype-mark--ws';
        return 'c2-ltype-mark--def';
    }

    function listenerTypeShortLabel(type) {
        const t = String(type || '').toLowerCase();
        if (t === 'https_beacon') return 'HTTPS';
        if (t === 'http_beacon') return 'HTTP';
        if (t === 'tcp_reverse') return 'TCP';
        if (t === 'websocket') return 'WS';
        return '?';
    }

    function listenerCardStatusPillLabel(status) {
        const s = String(status || '').toLowerCase();
        if (s === 'running') return c2t('c2.listeners.running');
        if (s === 'stopped') return c2t('c2.listeners.stopped');
        if (s === 'error') return c2t('c2.listeners.statusError');
        return c2t('c2.listeners.stopped');
    }

    /** 避免 i18n 插值把日期里的「/」转成 &#x2F;，与 formatTime 拼接后整体转义 */
    function formatListenerStartedHtml(dateStr) {
        if (!dateStr) return '';
        const prefix = c2t('c2.listeners.startedAtPrefix');
        const time = formatTime(dateStr);
        return '<div class="c2-listener-meta-row"><span class="c2-listener-meta-label">' + escapeHtml(prefix) + '</span> <span class="c2-listener-meta-time">' + escapeHtml(time) + '</span></div>';
    }

    function copyToClipboard(text) {
        if (navigator.clipboard) {
            navigator.clipboard.writeText(text).then(() => showToast(c2t('c2.clipboardCopied'), 'success'));
        } else {
            const ta = document.createElement('textarea');
            ta.value = text;
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
            showToast(c2t('c2.clipboardCopied'), 'success');
        }
    }

    C2.copyText = function(text) {
        if (text == null || text === '') return;
        copyToClipboard(String(text));
    };

    // ============================================================================
    // 页面初始化
    // ============================================================================

    C2.init = function() {
        const pageId = window.currentPageId || '';
        
        if (pageId.startsWith('c2')) {
            C2.connectEventStream();
        }

        switch(pageId) {
            case 'c2-listeners':
                C2.loadListeners();
                break;
            case 'c2-sessions':
                C2.renderSessionToolbar();
                C2.ensureListenersLoaded().then(function() {
                    C2.renderSessionToolbar();
                    C2.loadSessions();
                });
                break;
            case 'c2-tasks':
                C2.loadTasks();
                break;
            case 'c2-payloads':
                C2.refreshFormSelects(document.getElementById('page-c2-payloads'));
                C2.loadListenersForPayload();
                break;
            case 'c2-events':
                C2.loadEvents();
                break;
            case 'c2-profiles':
                C2.loadProfiles();
                break;
        }
    };

    // ============================================================================
    // 监听器管理
    // ============================================================================

    C2.ensureListenersLoaded = function() {
        if (C2.listeners && C2.listeners.length > 0) {
            return Promise.resolve(C2.listeners);
        }
        return apiRequest('GET', `${API_BASE}/listeners`).then(function(data) {
            C2.listeners = (data && data.listeners) || [];
            return C2.listeners;
        });
    };

    C2.loadListeners = function() {
        Promise.all([
            apiRequest('GET', `${API_BASE}/listeners`),
            apiRequest('GET', `${API_BASE}/profiles`).catch(function() { return {}; }),
            ensureC2ProjectsLoaded().catch(function() { return []; })
        ]).then(function(results) {
            var ldata = results[0];
            var pdata = results[1];
            C2.listeners = (ldata && ldata.listeners) || [];
            if (pdata && pdata.profiles && !pdata.error) {
                C2.profiles = pdata.profiles;
            }
            C2.renderListeners();
        });
    };

    /** 拉取 Profile 列表（监听器表单用）；失败时置空列表不阻断弹窗 */
    C2.ensureProfilesLoaded = function() {
        return apiRequest('GET', `${API_BASE}/profiles`).then(data => {
            if (data && data.error) {
                C2.profiles = [];
                return C2.profiles;
            }
            C2.profiles = (data && data.profiles) || [];
            return C2.profiles;
        });
    };

    C2.renderListeners = function() {
        const container = document.getElementById('c2-listener-grid');
        if (!container) return;

        if (C2.listeners.length === 0) {
            container.innerHTML = `
                <div class="c2-empty">
                    <svg width="56" height="56" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" stroke-width="1.2" style="margin-bottom:16px;opacity:0.6;">
                        <path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z"></path>
                        <path d="M12 15l-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z"></path>
                        <path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0"></path>
                        <path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"></path>
                    </svg>
                    <h3 style="margin-bottom:8px;font-size:18px;font-weight:700;">${escapeHtml(c2t('c2.listeners.emptyTitle'))}</h3>
                    <p style="font-size:14px;">${escapeHtml(c2t('c2.listeners.emptyHint'))}</p>
                    <button class="btn-primary" data-require-permission="c2:write" onclick="C2.showCreateListenerModal()" style="margin-top:20px;">
                        ${escapeHtml(c2t('c2.listeners.headerCreateBtn'))}
                    </button>
                </div>`;
            return;
        }

        container.innerHTML = C2.listeners.map(function(l) {
            const st = String(l.status || 'stopped').toLowerCase();
            const stUi = st === 'running' || st === 'stopped' || st === 'error' ? st : 'stopped';
            const profilePid = listenerResolvedProfileId(l);
            const profileName = listenerProfileDisplayName(l);
            const profileBadge = profilePid
                ? '<div class="c2-listener-profile-badge" title="' + escapeAttr(c2t('c2.listeners.profileBadgeTitle')) + '"><span class="c2-listener-profile-dot" aria-hidden="true"></span><span>' + escapeHtml(profileName) + '</span></div>'
                : '';
            const cb = C2.getListenerCallbackHost(l);
            const cbRow = cb
                ? '<div class="c2-listener-kv"><span class="c2-listener-kv-label">' + escapeHtml(c2t('c2.listeners.callbackShort')) + '</span><span class="c2-listener-kv-val c2-listener-mono">' + escapeHtml(cb) + '</span></div>'
                : '';
            const remarkRow = l.remark ? '<div class="c2-listener-remark">' + escapeHtml(l.remark) + '</div>' : '';
            const projectRow = '<div class="c2-listener-kv"><span class="c2-listener-kv-label">' + escapeHtml(c2t('assets.project') || '所属项目') + '</span><span class="c2-listener-kv-val">' + c2ProjectBindSelectHtml(l) + '</span></div>';
            const startedHtml = formatListenerStartedHtml(l.startedAt);
            const pillLabel = escapeHtml(listenerCardStatusPillLabel(st));
            const typeMark = escapeHtml(listenerTypeShortLabel(l.type));
            const typeVis = listenerTypeVisualClass(l.type);
            const fullType = escapeHtml(listenerTypeLabel(l.type));
            const bindVal = escapeHtml(String(l.bindHost)) + ':' + escapeHtml(String(l.bindPort));

            return `
            <article class="c2-listener-card c2-listener-card--${stUi}" data-listener-id="${escapeAttr(l.id)}">
                <div class="c2-listener-card-head">
                    <div class="c2-ltype-mark ${typeVis}" title="${fullType}"><span>${typeMark}</span></div>
                    <div class="c2-listener-card-head-main">
                        <div class="c2-listener-card-title-row">
                            <h3 class="c2-listener-name">${escapeHtml(l.name)}</h3>
                            <span class="c2-listener-pill c2-listener-pill--${stUi}">${pillLabel}</span>
                        </div>
                        <div class="c2-listener-id-row">
                            <code class="c2-listener-id-full" title="${escapeAttr(l.id)}">${escapeHtml(l.id)}</code>
                        </div>
                    </div>
                </div>
                <div class="c2-listener-card-body">
                    <div class="c2-listener-kv">
                        <span class="c2-listener-kv-label">${escapeHtml(c2t('c2.listeners.bindEndpoint'))}</span>
                        <span class="c2-listener-kv-val c2-listener-mono"><span class="c2-status-dot ${escapeHtml(st)}"></span>${bindVal}</span>
                    </div>
                    ${cbRow}
                    ${projectRow}
                    ${profileBadge}
                    ${remarkRow}
                    ${startedHtml}
                </div>
                <div class="c2-listener-card-actions">
                    ${l.status === 'stopped'
                        ? `<button type="button" class="btn-primary btn-sm" data-require-permission="c2:write" data-c2-action="listener-start" data-c2-id="${escapeAttr(l.id)}">▶ ${escapeHtml(c2t('c2.listeners.start'))}</button>`
                        : `<button type="button" class="btn-secondary btn-sm" data-require-permission="c2:write" data-c2-action="listener-stop" data-c2-id="${escapeAttr(l.id)}">⏹ ${escapeHtml(c2t('c2.listeners.stop'))}</button>`
                    }
                    <button type="button" class="btn-secondary btn-sm" data-require-permission="c2:write" data-c2-action="listener-edit" data-c2-id="${escapeAttr(l.id)}">${escapeHtml(c2t('c2.listeners.edit'))}</button>
                    <button type="button" class="btn-danger btn-sm" data-require-permission="c2:delete" data-c2-action="listener-delete" data-c2-id="${escapeAttr(l.id)}">${escapeHtml(c2t('c2.listeners.delete'))}</button>
                </div>
            </article>`;
        }).join('');
        if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
    };

    C2.getListenerCallbackHost = function(l) {
        if (!l) return '';
        try {
            var raw = l.configJson != null ? l.configJson : '{}';
            var j = typeof raw === 'string' ? JSON.parse(raw || '{}') : (raw || {});
            return String(j.callback_host || '').trim();
        } catch (e) {
            return '';
        }
    };

    C2.getListenerConfig = function(l) {
        if (!l) return {};
        try {
            var raw = l.configJson != null ? l.configJson : '{}';
            return typeof raw === 'string' ? JSON.parse(raw || '{}') : (raw || {});
        } catch (e) {
            return {};
        }
    };

    C2.showCreateListenerModal = function() {
        const modal = document.getElementById('c2-modal');
        const content = document.getElementById('c2-modal-content');
        if (!content || !modal) return;

        openAppModal(modal);
        content.innerHTML = `
            <div class="c2-modal-header">
                <h3>${escapeHtml(c2t('c2.listeners.modalCreateTitle'))}</h3>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <p class="form-hint" style="margin-top:0;">${escapeHtml(c2t('c2.listeners.loadingProfiles'))}</p>
            </div>
        `;

        const createSelectedProjectId = activeC2ProjectId();
        Promise.all([
            C2.ensureProfilesLoaded(),
            ensureC2ProjectsLoaded().catch(() => []),
            ensureC2SelectedProjectNamed(createSelectedProjectId)
        ]).then(function (results) {
            const projects = results[1] || [];
            const profileOpts = listenerProfileSelectHtml('');
            const projectOpts = c2ProjectOptionsHtml(createSelectedProjectId, projects);
            const emptyProfHintCreate = (C2.profiles && C2.profiles.length > 0)
                ? ''
                : `<div class="form-hint form-hint--warning" style="margin-bottom:6px;">${escapeHtml(c2t('c2.listeners.malleableProfileEmptyListHint'))}</div>`;
            content.innerHTML = `
            <div class="c2-modal-header">
                <h3>${escapeHtml(c2t('c2.listeners.modalCreateTitle'))}</h3>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <div class="c2-form-row">
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.listeners.name'))} <span aria-hidden="true">*</span></label>
                        <input type="text" id="c2-listener-name" class="form-control" placeholder="${escapeAttr(c2t('c2.listeners.placeholderNameExample'))}">
                    </div>
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.listeners.type'))} <span aria-hidden="true">*</span></label>
                        <select id="c2-listener-type" class="form-control c2-form-select-native">
                            <option value="http_beacon">HTTP Beacon</option>
                            <option value="https_beacon">HTTPS Beacon</option>
                            <option value="tcp_reverse">TCP Reverse</option>
                            <option value="websocket">WebSocket</option>
                        </select>
                    </div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('assets.project') || '所属项目')}</label>
                    <select id="c2-listener-project-id" class="form-control c2-form-select-native">${projectOpts}</select>
                </div>
                <div class="c2-form-row">
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.listeners.bindHost'))}</label>
                        <input type="text" id="c2-listener-host" class="form-control" value="127.0.0.1">
                        <div class="form-hint">${escapeHtml(c2t('c2.listeners.bindHintExternal'))}</div>
                    </div>
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.listeners.bindPort'))} <span aria-hidden="true">*</span></label>
                        <input type="number" id="c2-listener-port" class="form-control" placeholder="8443">
                    </div>
                </div>
                <div class="c2-form-group" id="c2-listener-profile-group">
                    <label>${escapeHtml(c2t('c2.listeners.malleableProfile'))}</label>
                    ${emptyProfHintCreate}
                    <select id="c2-listener-profile-id" class="form-control c2-form-select-native">${profileOpts}</select>
                    <div class="form-hint">${escapeHtml(c2t('c2.listeners.malleableProfileHint'))}</div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.listeners.callbackHost'))}</label>
                    <input type="text" id="c2-listener-callback-host" class="form-control" placeholder="">
                    <div class="form-hint">${escapeHtml(c2t('c2.listeners.callbackHostHint'))}</div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.listeners.remark'))}</label>
                    <input type="text" id="c2-listener-remark" class="form-control" placeholder="${escapeAttr(c2t('c2.listeners.placeholderRemarkLong'))}">
                </div>
                <div class="c2-form-group" id="c2-listener-legacy-shell-group" style="display:none;">
                    <label class="c2-checkbox-label">
                        <input type="checkbox" id="c2-listener-legacy-shell">
                        ${escapeHtml(c2t('c2.listeners.allowLegacyShell'))}
                    </label>
                    <div class="form-hint form-hint--warning">${escapeHtml(c2t('c2.listeners.allowLegacyShellHint'))}</div>
                </div>
            </div>
            <div class="c2-modal-footer">
                <button class="btn-secondary" onclick="C2.closeModal()">${escapeHtml(c2t('common.cancel'))}</button>
                <button class="btn-primary" onclick="C2.createListener()">${escapeHtml(c2t('c2.listeners.submitCreate'))}</button>
            </div>
        `;
            C2.syncListenerProfileRowForType();
            C2.refreshFormSelects(content);
        }).catch(() => {
            showToast(c2t('c2.listeners.toastProfilesLoadFailed'), 'error');
            C2.closeModal();
        });
    };

    /** 非 HTTP/HTTPS Beacon 时隐藏 Profile 行；tcp_reverse 时显示经典 shell 开关 */
    C2.syncListenerProfileRowForType = function() {
        const typeEl = document.getElementById('c2-listener-type');
        const row = document.getElementById('c2-listener-profile-group');
        const legacyRow = document.getElementById('c2-listener-legacy-shell-group');
        if (!typeEl) return;
        const t = String(typeEl.value || '').toLowerCase();
        if (row) {
            const show = t === 'http_beacon' || t === 'https_beacon';
            row.style.display = show ? '' : 'none';
            if (!show) {
                const sel = document.getElementById('c2-listener-profile-id');
                if (sel) sel.value = '';
            }
        }
        if (legacyRow) {
            legacyRow.style.display = t === 'tcp_reverse' ? '' : 'none';
        }
        syncC2FormSelect('c2-listener-type');
        syncC2FormSelect('c2-listener-profile-id');
    };

    function validateListenerForm(creating) {
        const checks = [
            ['c2-listener-name', 'c2.listeners.nameRequired', value => !!value.trim()],
            ...(creating ? [['c2-listener-type', 'c2.listeners.typeRequired', value => !!value]] : []),
            ['c2-listener-port', 'c2.listeners.portInvalid', value => /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535]
        ];
        let firstInvalid;
        checks.forEach(([id, key, valid]) => {
            const input = document.getElementById(id);
            if (!input) return;
            const invalid = !valid(String(input.value || ''));
            input.setAttribute('aria-invalid', String(invalid));
            input.setCustomValidity?.(invalid ? c2t(key) : '');
            if (invalid && !firstInvalid) firstInvalid = { input, key };
        });
        if (firstInvalid) {
            showToast(c2t(firstInvalid.key), 'error');
            firstInvalid.input.focus();
            firstInvalid.input.reportValidity?.();
            return false;
        }
        return true;
    }

    C2.createListener = function() {
        if (!validateListenerForm(true)) return;
        const name = document.getElementById('c2-listener-name')?.value.trim();
        const type = document.getElementById('c2-listener-type')?.value;
        const bindHost = document.getElementById('c2-listener-host')?.value || '127.0.0.1';
        const bindPort = parseInt(document.getElementById('c2-listener-port')?.value);
        const callbackHost = document.getElementById('c2-listener-callback-host')?.value?.trim() || '';
        const remark = document.getElementById('c2-listener-remark')?.value;

        const profileId = (document.getElementById('c2-listener-profile-id')?.value || '').trim();
        const legacyShell = document.getElementById('c2-listener-legacy-shell')?.checked === true;
        const body = {
            name, type, bind_host: bindHost, bind_port: bindPort, remark,
            callback_host: callbackHost,
            project_id: (document.getElementById('c2-listener-project-id')?.value || '').trim(),
            profile_id: profileId
        };
        if (type === 'tcp_reverse' && legacyShell) {
            body.config = { allow_legacy_shell: true };
        }

        return apiRequest('POST', `${API_BASE}/listeners`, body).then(data => {
            if (data.error) {
                showToast(data.error, 'error');
            } else {
                showToast(c2t('c2.listeners.toastCreated'), 'success');
                C2.closeModal();
                C2.loadListeners();
            }
        }).catch(error => showToast(error.message || c2t('c2.listeners.saveFailed'), 'error'));
    };

    C2.startListener = function(id) {
        apiRequest('POST', `${API_BASE}/listeners/${id}/start`, {}).then(data => {
            if (data.error) showToast(data.error, 'error');
            else {
                showToast(c2t('c2.listeners.toastStarted'), 'success');
                C2.loadListeners();
            }
        });
    };

    C2.stopListener = function(id) {
        apiRequest('POST', `${API_BASE}/listeners/${id}/stop`, {}).then(data => {
            if (data.error) showToast(data.error, 'error');
            else {
                showToast(c2t('c2.listeners.toastStopped'), 'success');
                C2.loadListeners();
            }
        });
    };

    C2.deleteListener = function(id) {
        if (!confirm(c2t('c2.listeners.confirmDelete'))) return;
        apiRequest('DELETE', `${API_BASE}/listeners/${id}`, {}).then(data => {
            showToast(c2t('c2.listeners.toastDeleted'), 'success');
            C2.loadListeners();
        });
    };

    C2.bindListenerProject = function(id, projectId) {
        if (typeof requirePermission === 'function' && !requirePermission('c2:write')) {
            C2.renderListeners();
            return;
        }
        const listener = C2.listeners.find(x => x.id === id);
        if (!listener) return;
        const cfg = C2.getListenerConfig(listener);
        const body = {
            name: listener.name || '',
            bind_host: listener.bindHost || '127.0.0.1',
            bind_port: parseInt(listener.bindPort, 10) || 0,
            remark: listener.remark || '',
            callback_host: C2.getListenerCallbackHost(listener),
            project_id: String(projectId || '').trim(),
            profile_id: listenerResolvedProfileId(listener),
            config: cfg
        };
        apiRequest('PUT', `${API_BASE}/listeners/${id}`, body).then(data => {
            if (data && data.error) {
                showToast(data.error, 'error');
                C2.renderListeners();
                return;
            }
            showToast(c2t('projects.projectBound') || '已绑定项目', 'success');
            C2.loadListeners();
        }).catch(err => {
            showToast(err && err.message ? err.message : (c2t('c2.listeners.toastBindProjectFailed') || '绑定项目失败'), 'error');
            C2.renderListeners();
        });
    };

    C2.editListener = function(id) {
        const l = C2.listeners.find(x => x.id === id);
        if (!l) return;

        const cbHost = C2.getListenerCallbackHost(l);
        const cfg = C2.getListenerConfig(l);
        const legacyShell = !!cfg.allow_legacy_shell;
        const modal = document.getElementById('c2-modal');
        const content = document.getElementById('c2-modal-content');
        if (!content || !modal) return;

        openAppModal(modal);
        content.innerHTML = `
            <div class="c2-modal-header">
                <h3>${escapeHtml(c2t('c2.listeners.editTitle'))}</h3>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <p class="form-hint" style="margin-top:0;">${escapeHtml(c2t('c2.listeners.loadingProfiles'))}</p>
            </div>
        `;

        const editSelectedProjectId = c2ResourceProjectId(l);
        Promise.all([
            C2.ensureProfilesLoaded(),
            ensureC2ProjectsLoaded().catch(() => []),
            ensureC2SelectedProjectNamed(editSelectedProjectId)
        ]).then(function (results) {
            const projects = results[1] || [];
            const resolvedPid = listenerResolvedProfileId(l);
            const profileOpts = listenerProfileSelectHtml(resolvedPid);
            const projectOpts = c2ProjectOptionsHtml(editSelectedProjectId, projects);
            const lt = String(l.type || '').toLowerCase();
            const httpHint = (lt === 'http_beacon' || lt === 'https_beacon')
                ? ''
                : `<div class="form-hint" style="margin-bottom:6px;">${escapeHtml(c2t('c2.listeners.malleableProfileNonHttpHint'))}</div>`;
            const emptyProfHint = (C2.profiles && C2.profiles.length > 0)
                ? ''
                : `<div class="form-hint form-hint--warning" style="margin-bottom:6px;">${escapeHtml(c2t('c2.listeners.malleableProfileEmptyListHint'))}</div>`;
            content.innerHTML = `
            <div class="c2-modal-header">
                <h3>${escapeHtml(c2t('c2.listeners.editTitle'))}</h3>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.listeners.name'))} <span aria-hidden="true">*</span></label>
                    <input type="text" id="c2-listener-name" class="form-control" value="${escapeAttr(l.name)}">
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('assets.project') || '所属项目')}</label>
                    <select id="c2-listener-project-id" class="form-control c2-form-select-native">${projectOpts}</select>
                </div>
                <div class="c2-form-row">
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.listeners.bindHost'))}</label>
                        <input type="text" id="c2-listener-host" class="form-control" value="${escapeAttr(String(l.bindHost))}">
                    </div>
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.listeners.bindPort'))} <span aria-hidden="true">*</span></label>
                        <input type="number" id="c2-listener-port" class="form-control" value="${l.bindPort}">
                    </div>
                </div>
                <div class="c2-form-group" id="c2-listener-profile-group">
                    <label>${escapeHtml(c2t('c2.listeners.malleableProfile'))}</label>
                    ${httpHint}${emptyProfHint}
                    <select id="c2-listener-profile-id" class="form-control c2-form-select-native">${profileOpts}</select>
                    <div class="form-hint">${escapeHtml(c2t('c2.listeners.malleableProfileHint'))}</div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.listeners.callbackHost'))}</label>
                    <input type="text" id="c2-listener-callback-host" class="form-control" value="${escapeAttr(cbHost)}">
                    <div class="form-hint">${escapeHtml(c2t('c2.listeners.callbackHostHint'))}</div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.listeners.remark'))}</label>
                    <input type="text" id="c2-listener-remark" class="form-control" value="${escapeAttr(l.remark || '')}">
                </div>
                ${lt === 'tcp_reverse' ? `
                <div class="c2-form-group" id="c2-listener-legacy-shell-group">
                    <label class="c2-checkbox-label">
                        <input type="checkbox" id="c2-listener-legacy-shell"${legacyShell ? ' checked' : ''}>
                        ${escapeHtml(c2t('c2.listeners.allowLegacyShell'))}
                    </label>
                    <div class="form-hint form-hint--warning">${escapeHtml(c2t('c2.listeners.allowLegacyShellHint'))}</div>
                </div>` : ''}
            </div>
            <div class="c2-modal-footer">
                <button class="btn-secondary" onclick="C2.closeModal()">${escapeHtml(c2t('common.cancel'))}</button>
                <button class="btn-primary" data-c2-action="listener-save" data-c2-id="${escapeAttr(l.id)}">${escapeHtml(c2t('common.save'))}</button>
            </div>
        `;
            C2.refreshFormSelects(content);
        }).catch(() => {
            showToast(c2t('c2.listeners.toastProfilesLoadFailed'), 'error');
            C2.closeModal();
        });
    };

    C2.saveListener = function(id) {
        if (!validateListenerForm(false)) return;
        const name = document.getElementById('c2-listener-name')?.value.trim();
        const bindHost = document.getElementById('c2-listener-host')?.value;
        const bindPort = parseInt(document.getElementById('c2-listener-port')?.value);
        const callbackHost = document.getElementById('c2-listener-callback-host')?.value?.trim() ?? '';
        const remark = document.getElementById('c2-listener-remark')?.value;
        const profileEl = document.getElementById('c2-listener-profile-id');
        const profileId = profileEl ? String(profileEl.value || '').trim() : '';
        const legacyEl = document.getElementById('c2-listener-legacy-shell');
        const body = {
            name, bind_host: bindHost, bind_port: bindPort, remark,
            callback_host: callbackHost,
            project_id: (document.getElementById('c2-listener-project-id')?.value || '').trim(),
            profile_id: profileId
        };
        if (legacyEl) {
            const existing = C2.listeners.find(x => x.id === id);
            const merged = Object.assign({}, C2.getListenerConfig(existing), {
                allow_legacy_shell: legacyEl.checked === true
            });
            body.config = merged;
        }

        return apiRequest('PUT', `${API_BASE}/listeners/${id}`, body).then(data => {
            if (data.error) showToast(data.error, 'error');
            else {
                showToast(c2t('c2.listeners.toastUpdated'), 'success');
                C2.closeModal();
                C2.loadListeners();
            }
        }).catch(error => showToast(error.message || c2t('c2.listeners.saveFailed'), 'error'));
    };

    // ============================================================================
    // 会话管理
    // ============================================================================

    C2.loadSessions = function() {
        const f = C2.sessionFilter || {};
        const params = new URLSearchParams();
        if (f.status) params.set('status', f.status);
        if (f.listener_id) params.set('listener_id', f.listener_id);
        if (f.search) params.set('search', f.search);
        if (f.suspicious) params.set('suspicious', '1');
        const qs = params.toString();
        const url = `${API_BASE}/sessions` + (qs ? `?${qs}` : '');
        return apiRequest('GET', url).then(data => {
            C2.sessions = data.sessions || [];
            C2.renderSessionToolbar();
            C2.renderSessions();
        });
    };

    C2.renderSessionToolbar = function() {
        const toolbar = document.getElementById('c2-session-toolbar');
        if (!toolbar) return;
        const f = C2.sessionFilter || {};
        const listeners = C2.listeners || [];
        const listenerOpts = ['<option value="">' + escapeHtml(c2t('c2.sessions.filterAllListeners')) + '</option>']
            .concat(listeners.map(l => {
                const sel = f.listener_id === l.id ? ' selected' : '';
                return `<option value="${escapeAttr(l.id)}"${sel}>${escapeHtml(l.name)}</option>`;
            })).join('');
        toolbar.innerHTML = `
            <div class="c2-sessions-filter-row">
                <select id="c2-session-filter-status" class="form-control c2-native-select" title="${escapeAttr(c2t('c2.sessions.status'))}" onchange="C2.applySessionFilter()">
                    <option value="">${escapeHtml(c2t('c2.sessions.filterAllStatus'))}</option>
                    <option value="active"${f.status === 'active' ? ' selected' : ''}>${escapeHtml(c2t('c2.sessions.active'))}</option>
                    <option value="sleeping"${f.status === 'sleeping' ? ' selected' : ''}>${escapeHtml(c2t('c2.sessions.sleeping'))}</option>
                    <option value="dead"${f.status === 'dead' ? ' selected' : ''}>${escapeHtml(c2t('c2.sessions.dead'))}</option>
                </select>
                <select id="c2-session-filter-listener" class="form-control c2-native-select" onchange="C2.applySessionFilter()">${listenerOpts}</select>
            </div>
            <input type="text" id="c2-session-filter-search" class="form-control" placeholder="${escapeAttr(c2t('c2.sessions.filterSearchPlaceholder'))}" value="${escapeAttr(f.search || '')}" onkeydown="if(event.key==='Enter'){C2.applySessionFilter();}">
            <div class="c2-sessions-toolbar-meta">
                <label class="c2-sessions-select-all-label">
                    <input type="checkbox" id="c2-sessions-select-all" onchange="C2.onSessionsSelectAll(this.checked)">
                    <span>${escapeHtml(c2t('c2.sessions.selectAll'))}</span>
                </label>
                <div class="c2-sessions-quick-links">
                    <button type="button" onclick="C2.resetSessionFilter()">${escapeHtml(c2t('c2.sessions.filterReset'))}</button>
                    <button type="button" onclick="C2.applySuspiciousFilter()">${escapeHtml(c2t('c2.sessions.filterSuspicious'))}</button>
                </div>
                <span class="c2-sessions-count" id="c2-sessions-count"></span>
            </div>
        `;
        C2.syncSessionsToolbar();
    };

    C2.readSessionFilterFromDom = function() {
        return {
            status: document.getElementById('c2-session-filter-status')?.value || '',
            listener_id: document.getElementById('c2-session-filter-listener')?.value || '',
            search: (document.getElementById('c2-session-filter-search')?.value || '').trim(),
            suspicious: !!(C2.sessionFilter && C2.sessionFilter.suspicious),
        };
    };

    C2.applySessionFilter = function() {
        const next = C2.readSessionFilterFromDom();
        next.suspicious = false;
        C2.sessionFilter = next;
        C2.loadSessions();
    };

    C2.resetSessionFilter = function() {
        C2.sessionFilter = { status: '', listener_id: '', search: '', suspicious: false };
        C2.loadSessions();
    };

    C2.applySuspiciousFilter = function() {
        C2.sessionFilter = { status: 'dead', listener_id: '', search: '', suspicious: true };
        C2.loadSessions();
    };

    C2.collectCheckedSessionIds = function() {
        return Array.from(document.querySelectorAll('.c2-session-row-check:checked')).map(cb => cb.getAttribute('data-id')).filter(Boolean);
    };

    C2.syncSessionsToolbar = function() {
        const batchBtn = document.getElementById('c2-sessions-batch-delete');
        const filteredBtn = document.getElementById('c2-sessions-delete-filtered');
        const ids = C2.collectCheckedSessionIds();
        if (batchBtn) {
            batchBtn.disabled = ids.length === 0;
            batchBtn.classList.toggle('btn-danger', ids.length > 0);
            batchBtn.classList.toggle('btn-secondary', ids.length === 0);
        }
        const total = (C2.sessions || []).length;
        if (filteredBtn) {
            filteredBtn.disabled = total === 0;
            filteredBtn.classList.toggle('is-danger-soft', total > 0);
        }
        const countEl = document.getElementById('c2-sessions-count');
        if (countEl) {
            countEl.textContent = c2t('c2.sessions.filterCount', { n: total, selected: ids.length });
        }
        const all = document.querySelectorAll('.c2-session-row-check');
        const selAll = document.getElementById('c2-sessions-select-all');
        if (selAll && all.length) {
            const nChecked = document.querySelectorAll('.c2-session-row-check:checked').length;
            selAll.checked = nChecked === all.length;
            selAll.indeterminate = nChecked > 0 && nChecked < all.length;
        } else if (selAll) {
            selAll.checked = false;
            selAll.indeterminate = false;
        }
    };

    C2.onSessionsSelectAll = function(checked) {
        document.querySelectorAll('.c2-session-row-check').forEach(cb => { cb.checked = checked; });
        C2.syncSessionsToolbar();
    };

    C2.deleteSessionsByIds = function(ids, confirmKey, confirmOpts) {
        if (!ids || !ids.length) {
            showToast(c2t('c2.sessions.toastSelectFirst'), 'warn');
            return Promise.resolve();
        }
        if (!confirm(c2t(confirmKey, confirmOpts || { n: ids.length }))) return Promise.resolve();
        return apiRequest('DELETE', `${API_BASE}/sessions`, { ids }).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            const deleted = data.deleted != null ? data.deleted : ids.length;
            showToast(c2t('c2.sessions.toastBatchDeleted', { n: deleted }), 'success');
            if (C2.selectedSessionId && ids.indexOf(C2.selectedSessionId) >= 0) {
                C2.selectedSessionId = null;
            }
            C2.loadSessions();
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    C2.deleteSelectedSessions = function() {
        C2.deleteSessionsByIds(C2.collectCheckedSessionIds(), 'c2.sessions.confirmBatchDelete');
    };

    C2.deleteFilteredSessions = function() {
        const ids = (C2.sessions || []).map(s => s.id).filter(Boolean);
        C2.deleteSessionsByIds(ids, 'c2.sessions.confirmDeleteFiltered', { n: ids.length });
    };

    C2.renderSessions = function() {
        const list = document.getElementById('c2-session-list');
        const main = document.getElementById('c2-session-main');
        if (!list) return;

        if (C2.sessions.length === 0) {
            list.innerHTML = `<div class="c2-session-list-empty">${escapeHtml(c2t(C2.sessionFilter && (C2.sessionFilter.status || C2.sessionFilter.search || C2.sessionFilter.listener_id || C2.sessionFilter.suspicious) ? 'c2.sessions.emptyFilter' : 'c2.sessions.listEmpty'))}</div>`;
            if (main) {
                main.innerHTML = `
                    <div class="c2-session-main-empty">
                        <div class="c2-session-main-empty__icon" aria-hidden="true"></div>
                        <h3>${escapeHtml(c2t('c2.sessions.emptyTitle'))}</h3>
                        <p>${escapeHtml(c2t('c2.sessions.emptyHint'))}</p>
                    </div>`;
            }
            C2.syncSessionsToolbar();
            return;
        }

        list.innerHTML = C2.sessions.map(s => {
            const userLabel = displayInfoValue(s.username, '—');
            const osArch = [displayInfoValue(s.os, ''), displayInfoValue(s.arch, '')].filter(Boolean).join('/') || '—';
            const userEmpty = isEmptyInfoValue(s.username);
            const osEmpty = isEmptyInfoValue(s.os) && isEmptyInfoValue(s.arch);
            return `
            <div class="c2-session-item ${s.id === C2.selectedSessionId ? 'active' : ''}" 
                 data-status="${escapeAttr(s.status || '')}"
                 data-c2-action="session-select"
                 data-c2-id="${escapeAttr(s.id)}"
                 role="button"
                 tabindex="0">
                <input type="checkbox" class="c2-session-item-check c2-session-row-check" data-id="${escapeAttr(s.id)}"
                    data-c2-stop-action="1" onchange="C2.syncSessionsToolbar()">
                <div class="c2-session-item-body">
                    <div class="c2-session-header">
                        <div class="c2-session-host-row">
                            <span class="c2-session-live-dot ${escapeHtml(s.status || '')}" aria-hidden="true"></span>
                            <span class="c2-session-host">${escapeHtml(s.hostname || c2t('c2.sessions.unknownHost'))}</span>
                        </div>
                        <span class="c2-session-status ${s.status}">${escapeHtml(sessionStatusLabel(s.status))}</span>
                    </div>
                    <div class="c2-session-chips">
                        <span class="c2-session-chip${userEmpty ? ' c2-session-chip--empty' : ''}">${escapeHtml(userLabel)}</span>
                        <span class="c2-session-chip c2-session-chip--mono${osEmpty ? ' c2-session-chip--empty' : ''}">${escapeHtml(osArch)}</span>
                        ${s.isAdmin ? '<span class="c2-session-chip c2-session-chip--warn">' + escapeHtml(c2t('c2.sessions.rootBadge')) + '</span>' : ''}
                    </div>
                    <div class="c2-session-chips c2-session-chips--sub">
                        <span class="c2-session-chip c2-session-chip--dim">${escapeHtml(s.internalIp || '—')}</span>
                        <span class="c2-session-chip c2-session-chip--dim">PID ${escapeHtml(String(s.pid != null ? s.pid : '—'))}</span>
                    </div>
                    <div class="c2-session-item-footer">
                        <span class="c2-session-meta c2-session-item-time" title="${escapeAttr(formatTime(s.lastCheckIn))}">${escapeHtml(formatRelativeTime(s.lastCheckIn) || formatTime(s.lastCheckIn))}</span>
                        <button type="button" class="c2-session-card-delete" data-require-permission="c2:delete" data-c2-action="session-delete" data-c2-id="${escapeAttr(s.id)}">${escapeHtml(c2t('c2.sessions.cardDeleteSession'))}</button>
                    </div>
                </div>
            </div>`;
        }).join('');

        if (C2.selectedSessionId && !C2.sessions.find(s => s.id === C2.selectedSessionId)) {
            C2.selectedSessionId = null;
        }
        if (!C2.selectedSessionId && C2.sessions.length > 0) {
            C2.selectSession(C2.sessions[0].id);
        }
        C2.syncSessionsToolbar();
        if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(list);
    };

    C2.selectSession = function(id) {
        C2.selectedSessionId = id;
        C2.implantPwd = null;
        C2.currentPath = '.';
        C2.renderSessions();
        C2.renderSessionDetail(id);
    };

    C2.renderSessionDetail = function(id) {
        const container = document.getElementById('c2-session-main');
        if (!container) return;

        const s = C2.sessions.find(x => x.id === id);
        if (!s) return;

        const adminVal = s.isAdmin ? c2t('c2.sessions.adminYes') : c2t('c2.sessions.adminNo');
        const sleepLine = c2t('c2.sessions.infoSleepLine', { sec: s.sleepSeconds, jitter: s.jitterPercent });
        const osKey = sessionOsKey(s.os);
        const activeTab = C2.activeSessionTab || 'terminal';
        const tabCls = function (tab) { return activeTab === tab ? ' active' : ''; };
        const panelCls = function (tab) { return activeTab === tab ? ' active' : ''; };
        const panelDisplay = function (tab) {
            if (activeTab !== tab) return 'display:none';
            return tab === 'terminal' ? 'display:flex' : (tab === 'tasks' ? 'display:flex' : 'display:block');
        };
        const heartbeatRel = formatRelativeTime(s.lastCheckIn) || formatTime(s.lastCheckIn);

        container.innerHTML = `
            <div class="c2-session-detail">
                <div class="c2-session-hero">
                    <div class="c2-session-hero__main">
                        <div class="c2-session-avatar c2-session-avatar--${escapeHtml(osKey)}">${escapeHtml(sessionOsAvatarLabel(s.os))}</div>
                        <div class="c2-session-hero__text">
                            <div class="c2-session-hero__title-row">
                                <h3 class="c2-session-hero__title">${escapeHtml(s.hostname)}</h3>
                                <span class="c2-session-badge ${escapeHtml(s.status || '')}">${escapeHtml(sessionStatusLabel(s.status))}</span>
                                ${s.isAdmin ? '<span class="c2-session-hero-root">' + escapeHtml(c2t('c2.sessions.rootBadge')) + '</span>' : ''}
                            </div>
                            <div class="c2-session-hero__sub${isEmptyInfoValue(s.username) && isEmptyInfoValue(s.os) ? ' is-muted' : ''}">${escapeHtml(sessionMetaLine(s))}</div>
                            <div class="c2-session-hero__chips">
                                <span class="c2-session-hero-chip is-mono" title="${escapeAttr(c2t('c2.sessions.infoSessionId'))}">${escapeHtml(s.id)}</span>
                                <span class="c2-session-hero-chip">${escapeHtml(s.internalIp || '—')}</span>
                                <span class="c2-session-hero-chip">PID ${escapeHtml(String(s.pid != null ? s.pid : '—'))}</span>
                            </div>
                        </div>
                    </div>
                    <div class="c2-session-hero__side">
                        <div class="c2-session-hero__heartbeat">
                            <span class="c2-session-live-dot ${escapeHtml(s.status || '')}" aria-hidden="true"></span>
                            <span class="c2-session-hero__heartbeat-label">${escapeHtml(c2t('c2.sessions.infoLastCheckin'))}</span>
                            <span class="c2-session-hero__heartbeat-value">${escapeHtml(heartbeatRel)}</span>
                        </div>
                        <div class="c2-session-actions">
                            <button class="btn-secondary btn-sm" data-require-permission="c2:write" data-c2-action="session-sleep" data-c2-id="${escapeAttr(s.id)}">${escapeHtml(c2t('c2.sessions.btnSleep'))}</button>
                            <button class="btn-danger btn-sm" data-require-permission="c2:write" data-c2-action="session-kill" data-c2-id="${escapeAttr(s.id)}">${escapeHtml(c2t('c2.sessions.kill'))}</button>
                        </div>
                    </div>
                </div>
                
                <div class="c2-session-tabs c2-session-tabs--pills">
                    <button type="button" class="c2-session-tab${tabCls('terminal')}" data-tab="terminal" onclick="C2.switchTab('terminal')">${escapeHtml(c2t('c2.sessions.terminal'))}</button>
                    <button type="button" class="c2-session-tab${tabCls('files')}" data-tab="files" onclick="C2.switchTab('files')">${escapeHtml(c2t('c2.sessions.files'))}</button>
                    <button type="button" class="c2-session-tab${tabCls('tasks')}" data-tab="tasks" onclick="C2.switchTab('tasks')">${escapeHtml(c2t('c2.sessions.tasks'))}</button>
                    <button type="button" class="c2-session-tab${tabCls('info')}" data-tab="info" onclick="C2.switchTab('info')">${escapeHtml(c2t('c2.sessions.info'))}</button>
                </div>
                
                <div class="c2-session-tab-content">
                    <div id="c2-tab-terminal" class="c2-tab-panel${panelCls('terminal')}" style="${panelDisplay('terminal')}">
                        <div id="c2-terminal-container" class="c2-terminal-container"></div>
                        <div class="c2-terminal-toolbar">
                            <button class="btn-ghost btn-sm" onclick="C2.clearTerminal()">${escapeHtml(c2t('c2.sessions.clearTerminal'))}</button>
                            <button class="btn-ghost btn-sm" onclick="C2.copyTerminal()">${escapeHtml(c2t('common.copy'))}</button>
                            <span class="c2-terminal-status" id="c2-terminal-status">${escapeHtml(c2t('c2.sessions.termStatusReady'))}</span>
                        </div>
                    </div>
                    <div id="c2-tab-files" class="c2-tab-panel c2-tab-panel--card${panelCls('files')}" style="${panelDisplay('files')}">
                        <div class="c2-file-panel">
                            <div class="c2-file-toolbar">
                                <button class="btn-ghost btn-sm" onclick="C2.goToParentDirectory()">${escapeHtml(c2t('c2.files.parent'))}</button>
                                <button class="btn-ghost btn-sm" onclick="C2.refreshFiles()">${escapeHtml(c2t('c2.files.refresh'))}</button>
                                <button type="button" class="btn-ghost btn-sm" id="c2-file-upload-btn" data-require-permission="c2:write" onclick="C2.openFileUploadPicker()" title="${escapeAttr(c2t('c2.files.upload'))}">${escapeHtml(c2t('c2.files.upload'))}</button>
                                <input type="file" id="c2-file-upload-input" style="display:none" onchange="C2.onC2FileUploadPick(event)" />
                                <span id="c2-current-path" class="c2-path-breadcrumb">/</span>
                            </div>
                            <div id="c2-file-upload-hint" class="c2-file-upload-hint" hidden role="status"></div>
                            <div id="c2-file-upload-progress" class="c2-file-upload-progress" hidden role="status" aria-live="polite">
                                <div class="c2-file-upload-progress-track" aria-hidden="true"><div class="c2-file-upload-progress-fill" id="c2-file-upload-progress-fill"></div></div>
                                <span class="c2-file-upload-progress-label" id="c2-file-upload-progress-label"></span>
                            </div>
                            <div id="c2-file-list" class="c2-file-list"></div>
                        </div>
                    </div>
                    <div id="c2-tab-tasks" class="c2-tab-panel${panelCls('tasks')}" style="${panelDisplay('tasks')}">
                        <div id="c2-session-tasks-list"></div>
                    </div>
                    <div id="c2-tab-info" class="c2-tab-panel c2-tab-panel--card${panelCls('info')}" style="${panelDisplay('info')}">
                        ${renderSessionInfoPanel(s, adminVal, sleepLine)}
                    </div>
                </div>
            </div>
        `;

        var isCurlBeacon = s.implantUuid && s.implantUuid.startsWith('curl_');
        if (isCurlBeacon) {
            var termContainer = container.querySelector('#c2-terminal-container');
            if (termContainer) {
                termContainer.innerHTML =
                    '<div style="padding:24px;color:#94a3b8;text-align:center;line-height:1.8;">' +
                    '<div style="font-size:32px;margin-bottom:12px;">📡</div>' +
                    '<div style="font-size:14px;font-weight:600;color:#e2e8f0;margin-bottom:8px;">' + escapeHtml(c2t('c2.sessions.curlBeaconTitle')) + '</div>' +
                    '<div style="font-size:12px;">' + c2t('c2.sessions.curlBeaconBody').split('\n').map(function (ln) { return escapeHtml(ln); }).join('<br>') + '</div>' +
                    '</div>';
            }
        }
        setTimeout(() => {
            if (!isCurlBeacon) C2.initTerminal();
            C2.loadFileList(s.id, '.');
            C2.loadSessionTasks(s.id);
            C2.updateFileUploadButton(s);
            C2.ensureListenersLoaded().then(function() {
                C2.updateFileUploadButton(s);
            });
            requestAnimationFrame(function () { C2.fitTerminal(); });
        }, 0);
        if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
    };

    C2.switchTab = function(tab) {
        C2.activeSessionTab = tab;
        document.querySelectorAll('.c2-session-tab').forEach(el => el.classList.remove('active'));
        document.querySelectorAll('.c2-tab-panel').forEach(el => {
            el.classList.remove('active');
            el.style.display = 'none';
        });

        const tabEl = document.querySelector(`.c2-session-tab[data-tab="${tab}"]`);
        if (tabEl) tabEl.classList.add('active');

        const panel = document.getElementById(`c2-tab-${tab}`);
        if (panel) {
            panel.classList.add('active');
            if (tab === 'terminal' || tab === 'tasks') {
                panel.style.display = 'flex';
            } else {
                panel.style.display = 'block';
            }
        }

        if (tab === 'terminal') {
            requestAnimationFrame(function () {
                C2.fitTerminal();
                if (C2.terminalInstance) C2.terminalInstance.focus();
            });
        } else if (tab === 'tasks' && C2.selectedSessionId) {
            C2.loadSessionTasks(C2.selectedSessionId);
        }

        if (tabEl && typeof tabEl.blur === 'function') {
            tabEl.blur();
        }
    };

    C2.setSessionSleep = function(id) {
        if (!id) return;
        const modal = document.getElementById('c2-modal');
        const content = document.getElementById('c2-modal-content');
        const modalBox = modal && modal.querySelector('.c2-modal');
        if (!content || !modal) return;

        if (typeof isAppModalOpen === 'function' && isAppModalOpen(modal) && C2._sleepModalSessionId === id) {
            return;
        }

        const s = (C2.sessions || []).find(x => x.id === id);
        const defaultSleep = s && s.sleepSeconds != null ? s.sleepSeconds : 5;
        const defaultJitter = s && s.jitterPercent != null ? s.jitterPercent : 0;
        const hostLabel = s ? (s.hostname || s.id) : id;
        const currentLine = c2t('c2.sessions.sleepModalCurrent', { sec: defaultSleep, jitter: defaultJitter });
        const presets = [5, 10, 30, 60, 120, 300];
        const presetHtml = presets.map(function (p) {
            const active = p === defaultSleep ? ' is-active' : '';
            return `<button type="button" class="c2-sleep-preset${active}" data-c2-sleep-preset="${p}">${p}s</button>`;
        }).join('');

        C2._sleepModalSessionId = id;
        if (modalBox) {
            modalBox.classList.add('c2-modal--sleep');
            modalBox.classList.remove('c2-modal--wide');
        }

        openAppModal(modal, { focus: false });
        const render = function () {
            content.innerHTML = `
                <div class="c2-sleep-modal">
                    <div class="c2-sleep-modal__header">
                        <div class="c2-sleep-modal__title-wrap">
                            <div class="c2-sleep-modal__icon" aria-hidden="true">
                                <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                    <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6"/>
                                    <path d="M12 7.5V12l3 2.2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>
                                </svg>
                            </div>
                            <div>
                                <h3 class="c2-sleep-modal__title">${escapeHtml(c2t('c2.sessions.sleepModalTitle'))}</h3>
                                <p class="c2-sleep-modal__host">${escapeHtml(hostLabel)}</p>
                            </div>
                        </div>
                        <button type="button" class="c2-modal-close" onclick="C2.closeModal()" aria-label="${escapeAttr(c2t('common.close'))}">&times;</button>
                    </div>
                    <div class="c2-sleep-modal__current">${escapeHtml(currentLine)}</div>
                    <div class="c2-sleep-modal__body">
                        <div class="c2-sleep-field">
                            <div class="c2-sleep-field__head">
                                <label for="c2-sleep-seconds">${escapeHtml(c2t('c2.sessions.promptSleepSeconds'))}</label>
                                <span class="c2-sleep-field__value"><span id="c2-sleep-seconds-display">${defaultSleep}</span>s</span>
                            </div>
                            <div class="c2-sleep-field__control">
                                <input type="range" id="c2-sleep-seconds-range" class="c2-sleep-range" min="1" max="300" step="1" value="${defaultSleep}">
                                <input type="number" id="c2-sleep-seconds" class="c2-sleep-number" min="1" max="3600" value="${defaultSleep}">
                            </div>
                            <div class="c2-sleep-presets">
                                <span class="c2-sleep-presets__label">${escapeHtml(c2t('c2.sessions.sleepModalPresets'))}</span>
                                ${presetHtml}
                            </div>
                        </div>
                        <div class="c2-sleep-field">
                            <div class="c2-sleep-field__head">
                                <label for="c2-sleep-jitter">${escapeHtml(c2t('c2.sessions.promptJitterPercent'))}</label>
                                <span class="c2-sleep-field__value"><span id="c2-sleep-jitter-display">${defaultJitter}</span>%</span>
                            </div>
                            <div class="c2-sleep-field__control">
                                <input type="range" id="c2-sleep-jitter-range" class="c2-sleep-range" min="0" max="100" step="1" value="${defaultJitter}">
                                <input type="number" id="c2-sleep-jitter" class="c2-sleep-number" min="0" max="100" value="${defaultJitter}">
                            </div>
                        </div>
                        <div class="c2-sleep-modal__preview" id="c2-sleep-preview" role="status"></div>
                        <p class="c2-sleep-modal__hint">${escapeHtml(c2t('c2.sessions.sleepModalHint'))}</p>
                    </div>
                    <div class="c2-sleep-modal__footer">
                        <button type="button" class="btn-secondary" onclick="C2.closeModal()">${escapeHtml(c2t('common.cancel'))}</button>
                        <button type="button" class="btn-primary" id="c2-sleep-submit-btn">${escapeHtml(c2t('common.save'))}</button>
                    </div>
                </div>`;
            C2.bindSleepModalControls();
            const submitBtn = document.getElementById('c2-sleep-submit-btn');
            if (submitBtn) {
                submitBtn.addEventListener('click', function () { C2.submitSessionSleep(); });
            }
            const secInput = document.getElementById('c2-sleep-seconds');
            if (secInput) secInput.focus();
        };
        if (typeof deferModalContent === 'function') {
            deferModalContent(render);
        } else {
            render();
        }
    };

    C2.bindSleepModalControls = function() {
        const secRange = document.getElementById('c2-sleep-seconds-range');
        const secInput = document.getElementById('c2-sleep-seconds');
        const jitRange = document.getElementById('c2-sleep-jitter-range');
        const jitInput = document.getElementById('c2-sleep-jitter');
        if (!secRange || !secInput || !jitRange || !jitInput) return;

        const clamp = function (v, min, max) {
            if (!Number.isFinite(v)) return min;
            return Math.max(min, Math.min(max, v));
        };

        const syncPair = function (range, input, min, max, displayId) {
            const display = displayId ? document.getElementById(displayId) : null;
            const apply = function (value) {
                const v = clamp(value, min, max);
                range.value = String(Math.min(v, parseInt(range.max, 10) || max));
                input.value = String(v);
                if (display) display.textContent = String(v);
                C2.updateSleepModalPreview();
                document.querySelectorAll('[data-c2-sleep-preset]').forEach(function (btn) {
                    const p = parseInt(btn.getAttribute('data-c2-sleep-preset'), 10);
                    btn.classList.toggle('is-active', p === v && input === secInput);
                });
            };
            range.addEventListener('input', function () { apply(parseInt(range.value, 10)); });
            input.addEventListener('input', function () { apply(parseInt(input.value, 10)); });
            input.addEventListener('change', function () { apply(parseInt(input.value, 10)); });
        };

        syncPair(secRange, secInput, 1, 3600, 'c2-sleep-seconds-display');
        syncPair(jitRange, jitInput, 0, 100, 'c2-sleep-jitter-display');

        document.querySelectorAll('[data-c2-sleep-preset]').forEach(function (btn) {
            btn.addEventListener('click', function () {
                const v = parseInt(btn.getAttribute('data-c2-sleep-preset'), 10);
                if (!Number.isFinite(v)) return;
                secInput.value = String(v);
                secRange.value = String(Math.min(v, 300));
                const display = document.getElementById('c2-sleep-seconds-display');
                if (display) display.textContent = String(v);
                document.querySelectorAll('[data-c2-sleep-preset]').forEach(function (b) {
                    b.classList.toggle('is-active', b === btn);
                });
                C2.updateSleepModalPreview();
            });
        });

        C2.updateSleepModalPreview();
    };

    C2.updateSleepModalPreview = function() {
        const preview = document.getElementById('c2-sleep-preview');
        const secInput = document.getElementById('c2-sleep-seconds');
        const jitInput = document.getElementById('c2-sleep-jitter');
        if (!preview || !secInput || !jitInput) return;
        const sleep = parseInt(secInput.value, 10);
        const jitter = parseInt(jitInput.value, 10);
        if (!Number.isFinite(sleep) || sleep < 1) {
            preview.textContent = '';
            return;
        }
        const j = Number.isFinite(jitter) ? Math.max(0, Math.min(100, jitter)) : 0;
        const minSec = Math.max(1, Math.round(sleep * (1 - j / 100)));
        const maxSec = Math.max(minSec, Math.round(sleep * (1 + j / 100)));
        preview.textContent = c2t('c2.sessions.sleepModalPreview', { min: minSec, max: maxSec });
    };

    C2.submitSessionSleep = function() {
        const id = C2._sleepModalSessionId;
        if (!id) return;
        const sleepEl = document.getElementById('c2-sleep-seconds');
        const jitterEl = document.getElementById('c2-sleep-jitter');
        const submitBtn = document.getElementById('c2-sleep-submit-btn');
        const sleep = parseInt(sleepEl && sleepEl.value, 10);
        const jitter = parseInt(jitterEl && jitterEl.value, 10);
        if (!Number.isFinite(sleep) || sleep < 1) {
            showToast(c2t('c2.sessions.toastSleepInvalid'), 'warn');
            return;
        }
        const jitterVal = Number.isFinite(jitter) ? Math.max(0, Math.min(100, jitter)) : 0;
        if (submitBtn) submitBtn.disabled = true;

        apiRequest('PUT', `${API_BASE}/sessions/${id}/sleep`, {
            sleep_seconds: sleep,
            jitter_percent: jitterVal
        }).then(data => {
            if (submitBtn) submitBtn.disabled = false;
            if (data.error) {
                showToast(data.error, 'error');
                return;
            }
            C2._sleepModalSessionId = null;
            C2.closeModal();
            showToast(c2t('c2.sessions.toastSleepUpdated'), 'success');
            const refresh = C2.loadSessions();
            const after = function () {
                if (C2.selectedSessionId === id) {
                    C2.renderSessionDetail(id);
                }
            };
            if (refresh && typeof refresh.then === 'function') {
                refresh.then(after).catch(after);
            } else {
                after();
            }
        }).catch(function () {
            if (submitBtn) submitBtn.disabled = false;
        });
    };

    C2.beginEditSessionNote = function(id) {
        const block = document.getElementById('c2-session-note-block');
        const view = document.getElementById('c2-session-note-view');
        const editor = document.getElementById('c2-session-note-editor');
        const input = document.getElementById('c2-session-note-input');
        const editBtn = block && block.querySelector('.c2-session-note-edit-btn');
        if (!view || !editor || !input) return;
        const s = (C2.sessions || []).find(x => x.id === id);
        const note = s && s.note ? String(s.note) : '';
        input.value = note;
        view.hidden = true;
        editor.hidden = false;
        if (block) block.classList.remove('is-empty');
        if (editBtn) editBtn.hidden = true;
        C2.syncSessionNoteCounter();
        if (!input.dataset.boundCounter) {
            input.dataset.boundCounter = '1';
            input.addEventListener('input', function () { C2.syncSessionNoteCounter(); });
        }
        input.focus();
        input.setSelectionRange(input.value.length, input.value.length);
    };

    C2.syncSessionNoteCounter = function() {
        const input = document.getElementById('c2-session-note-input');
        const counter = document.getElementById('c2-session-note-counter');
        if (!input || !counter) return;
        counter.textContent = String(input.value.length) + '/2000';
    };

    C2.cancelEditSessionNote = function() {
        const block = document.getElementById('c2-session-note-block');
        const view = document.getElementById('c2-session-note-view');
        const editor = document.getElementById('c2-session-note-editor');
        const editBtn = block && block.querySelector('.c2-session-note-edit-btn');
        if (!view || !editor) return;
        editor.hidden = true;
        view.hidden = false;
        if (editBtn) editBtn.hidden = false;
        if (block) {
            const s = (C2.sessions || []).find(x => x.id === C2.selectedSessionId);
            const empty = !(s && s.note && String(s.note).trim());
            block.classList.toggle('is-empty', empty);
        }
    };

    C2.saveSessionNote = function(id) {
        if (!id) return;
        const input = document.getElementById('c2-session-note-input');
        const saveBtn = document.getElementById('c2-session-note-save');
        if (!input) return;
        const note = String(input.value || '').trim();
        if (note.length > 2000) {
            showToast(c2t('c2.sessions.toastNoteTooLong'), 'warn');
            return;
        }
        if (saveBtn) saveBtn.disabled = true;
        apiRequest('PUT', `${API_BASE}/sessions/${encodeURIComponent(id)}/note`, { note: note }).then(data => {
            if (saveBtn) saveBtn.disabled = false;
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            const saved = data.note != null ? String(data.note) : note;
            const s = (C2.sessions || []).find(x => x.id === id);
            if (s) s.note = saved;
            showToast(c2t('c2.sessions.toastNoteSaved'), 'success');
            const block = document.getElementById('c2-session-note-block');
            const view = document.getElementById('c2-session-note-view');
            const empty = !saved.trim();
            if (view) {
                view.textContent = empty ? c2t('c2.sessions.infoNoteEmpty') : saved;
                view.classList.toggle('is-empty', empty);
            }
            if (block) block.classList.toggle('is-empty', empty);
            C2.cancelEditSessionNote();
        }).catch(err => {
            if (saveBtn) saveBtn.disabled = false;
            showToast(err.message || String(err), 'error');
        });
    };

    C2.killSession = function(id) {
        if (!confirm(c2t('c2.sessions.confirmExitSession'))) return;
        apiRequest('POST', `${API_BASE}/tasks`, {
            session_id: id,
            task_type: 'exit',
            payload: {}
        }).then(data => {
            showToast(c2t('c2.sessions.toastExitSent'), 'success');
        });
    };

    C2.deleteSessionRecord = function(id) {
        if (!confirm(c2t('c2.sessions.confirmDeleteSession'))) return;
        apiRequest('DELETE', `${API_BASE}/sessions/${id}`, {}).then(data => {
            if (data.error) {
                showToast(data.error, 'error');
                return;
            }
            showToast(c2t('c2.sessions.toastSessionDeleted'), 'success');
            if (C2.selectedSessionId === id) C2.selectedSessionId = null;
            C2.loadSessions();
        });
    };

    // ============================================================================
    // xterm 终端
    // ============================================================================

    C2.serializeTerminalBuffer = function(term) {
        if (!term || !term.buffer || !term.buffer.active) return '';
        const buf = term.buffer.active;
        const lines = [];
        const viewportEnd = buf.baseY + term.rows;
        const start = Math.max(0, viewportEnd - 300);
        for (let i = start; i < viewportEnd; i++) {
            const line = buf.getLine(i);
            if (line) lines.push(line.translateToString(true));
        }
        while (lines.length && lines[lines.length - 1].trim() === '') lines.pop();
        return lines.join('\n');
    };

    C2.pushTerminalHistory = function(cmd) {
        const sid = C2.selectedSessionId;
        if (!sid || !cmd) return;
        if (!C2.terminalHistory[sid]) C2.terminalHistory[sid] = [];
        const hist = C2.terminalHistory[sid];
        if (hist.length === 0 || hist[hist.length - 1] !== cmd) {
            hist.push(cmd);
            if (hist.length > 200) hist.shift();
        }
    };

    C2.finishTerminalCommand = function(term, status) {
        C2.terminalBusy = false;
        const statusEl = document.getElementById('c2-terminal-status');
        if (status === 'err' && statusEl) {
            statusEl.textContent = c2t('c2.sessions.termStatusErr');
        } else if (status === 'timeout' && statusEl) {
            statusEl.textContent = c2t('c2.sessions.termStatusTimeout');
        } else if (statusEl && C2.terminalQueue.length === 0) {
            statusEl.textContent = c2t('c2.sessions.termStatusReady');
        }
        if (C2.terminalQueue.length > 0) {
            const next = C2.terminalQueue.shift();
            C2.runTerminalCommand(next, term);
            return;
        }
        term.write('$ ');
        if (statusEl && status !== 'err' && status !== 'timeout') {
            statusEl.textContent = c2t('c2.sessions.termStatusReady');
        }
    };

    C2.runTerminalCommand = function(cmd, term) {
        if (!C2.selectedSessionId) {
            term.writeln('\x1b[31m' + c2t('c2.sessions.termNoSession') + '\x1b[0m');
            term.write('$ ');
            return;
        }
        C2.terminalBusy = true;
        C2.pushTerminalHistory(cmd);
        const statusEl = document.getElementById('c2-terminal-status');
        if (statusEl) statusEl.textContent = c2t('c2.sessions.termStatusExec');

        apiRequest('POST', `${API_BASE}/tasks`, {
            session_id: C2.selectedSessionId,
            task_type: 'shell',
            payload: { command: cmd, timeout_seconds: 60 }
        }).then(data => {
            if (data.error) {
                term.writeln(`\x1b[31mError: ${data.error}\x1b[0m`);
                C2.finishTerminalCommand(term, 'err');
            } else {
                C2.waitForTaskResult(data.task?.id || data.task_id, term);
            }
        }).catch(function () {
            term.writeln('\x1b[31mError: request failed\x1b[0m');
            C2.finishTerminalCommand(term, 'err');
        });
    };

    C2.executeInTerminal = function(cmd, term) {
        if (!cmd) {
            term.write('$ ');
            return;
        }
        if (C2.terminalBusy) {
            C2.terminalQueue.push(cmd);
            term.writeln('\x1b[33m' + c2t('c2.sessions.termQueued') + '\x1b[0m');
            return;
        }
        C2.runTerminalCommand(cmd, term);
    };

    C2.waitForTaskResult = function(taskId, term) {
        let attempts = 0;
        const maxAttempts = 60;
        let delay = 500;
        const maxDelay = 5000;
        const check = () => {
            if (++attempts > maxAttempts) {
                term.writeln('\x1b[33m' + c2t('c2.sessions.termWaitTimeout') + '\x1b[0m');
                C2.finishTerminalCommand(term, 'timeout');
                return;
            }
            apiRequest('GET', `${API_BASE}/tasks/${taskId}`).then(data => {
                const task = data.task;
                if (task && (task.status === 'success' || task.status === 'failed')) {
                    if (task.resultText) {
                        const lines = task.resultText.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n');
                        lines.forEach(line => term.writeln(line));
                    }
                    if (task.error) {
                        term.writeln(`\x1b[31m${task.error}\x1b[0m`);
                    }
                    C2.finishTerminalCommand(term, task.status === 'failed' ? 'err' : 'ready');
                } else {
                    delay = Math.min(delay * 1.5, maxDelay);
                    setTimeout(check, delay);
                }
            }).catch(function () {
                C2.finishTerminalCommand(term, 'err');
            });
        };
        check();
    };

    C2.initTerminal = function() {
        const container = document.getElementById('c2-terminal-container');
        if (!container || typeof Terminal === 'undefined') return;

        if (C2.terminalInstance && C2.terminalSessionId) {
            C2.terminalLogs[C2.terminalSessionId] = C2.serializeTerminalBuffer(C2.terminalInstance);
        }
        if (C2.terminalInstance) {
            C2.terminalInstance.dispose();
        }

        const sessionId = C2.selectedSessionId || '_none';
        C2.terminalSessionId = sessionId;
        C2.terminalQueue = [];
        C2.terminalBusy = false;

        const term = new Terminal({
            cursorBlink: true,
            cursorStyle: 'block',
            fontSize: 14,
            fontFamily: 'Menlo, Monaco, "Courier New", "PingFang SC", "Microsoft YaHei", monospace',
            lineHeight: 1.3,
            scrollback: 5000,
            theme: {
                background: '#0d1117',
                foreground: '#e6edf3',
                cursor: '#58a6ff',
                cursorAccent: '#0d1117',
                selection: 'rgba(88, 166, 255, 0.3)'
            }
        });

        if (typeof FitAddon !== 'undefined') {
            const FitCtor = FitAddon.FitAddon || FitAddon;
            C2.terminalFitAddon = new FitCtor();
            term.loadAddon(C2.terminalFitAddon);
        }

        container.innerHTML = '';
        term.open(container);
        C2.fitTerminal();

        let lineBuffer = '';
        let cursorIndex = 0;
        let historyIndex = -1;
        let lastPasteAt = 0;
        let lastPasteText = '';
        const prompt = '$ ';

        function redrawInputLine() {
            term.write('\x1b[2K\r' + prompt + lineBuffer);
            const tail = lineBuffer.length - cursorIndex;
            if (tail > 0) term.write('\x1b[' + tail + 'D');
        }

        function resetInputLine() {
            lineBuffer = '';
            cursorIndex = 0;
            historyIndex = -1;
            term.write('\x1b[2K\r' + prompt);
        }

        function insertPlainText(text) {
            const safe = String(text).replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, '');
            if (!safe) return;
            lineBuffer = lineBuffer.slice(0, cursorIndex) + safe + lineBuffer.slice(cursorIndex);
            cursorIndex += safe.length;
            redrawInputLine();
        }

        function deleteWordBeforeCursor() {
            if (cursorIndex === 0) return;
            let start = cursorIndex;
            while (start > 0 && /\s/.test(lineBuffer[start - 1])) start--;
            while (start > 0 && !/\s/.test(lineBuffer[start - 1])) start--;
            lineBuffer = lineBuffer.slice(0, start) + lineBuffer.slice(cursorIndex);
            cursorIndex = start;
            redrawInputLine();
        }

        function moveWordLeft() {
            if (cursorIndex === 0) return;
            let pos = cursorIndex;
            while (pos > 0 && /\s/.test(lineBuffer[pos - 1])) pos--;
            while (pos > 0 && !/\s/.test(lineBuffer[pos - 1])) pos--;
            const delta = cursorIndex - pos;
            if (delta > 0) {
                term.write('\x1b[' + delta + 'D');
                cursorIndex = pos;
            }
        }

        function moveWordRight() {
            if (cursorIndex >= lineBuffer.length) return;
            let pos = cursorIndex;
            while (pos < lineBuffer.length && /\s/.test(lineBuffer[pos])) pos++;
            while (pos < lineBuffer.length && !/\s/.test(lineBuffer[pos])) pos++;
            const delta = pos - cursorIndex;
            if (delta > 0) {
                term.write('\x1b[' + delta + 'C');
                cursorIndex = pos;
            }
        }

        function showHistoryEntry(entry) {
            lineBuffer = entry || '';
            cursorIndex = lineBuffer.length;
            term.write('\x1b[2K\r' + prompt + lineBuffer);
        }

        function submitCurrentLine() {
            if (C2.terminalBusy) {
                term.writeln('');
                term.writeln('\x1b[33m' + c2t('c2.sessions.termWaitFinish') + '\x1b[0m');
                term.write(prompt + lineBuffer);
                const tail = lineBuffer.length - cursorIndex;
                if (tail > 0) term.write('\x1b[' + tail + 'D');
                return;
            }
            term.writeln('');
            const cmd = lineBuffer.trim();
            lineBuffer = '';
            cursorIndex = 0;
            historyIndex = -1;
            if (cmd) {
                C2.executeInTerminal(cmd, term);
            } else {
                term.write(prompt);
            }
        }

        function handlePasteText(text) {
            const now = Date.now();
            if (text === lastPasteText && now - lastPasteAt < 80) return;
            lastPasteAt = now;
            lastPasteText = text;

            const normalized = String(text).replace(/\r\n/g, '\n').replace(/\r/g, '\n');
            if (normalized.indexOf('\n') === -1) {
                insertPlainText(normalized);
                return;
            }
            const endsWithNewline = normalized.endsWith('\n');
            const parts = normalized.split('\n');
            const tail = parts.pop() || '';
            parts.forEach(function (part) {
                insertPlainText(part);
                submitCurrentLine();
            });
            if (tail) insertPlainText(tail);
            else if (endsWithNewline && parts.length === 0) submitCurrentLine();
        }

        const savedLog = C2.terminalLogs[sessionId];
        if (savedLog) {
            const normalized = String(savedLog).replace(/\r\n/g, '\n').replace(/\r/g, '\n').trimEnd();
            if (normalized) {
                term.write(normalized.replace(/\n/g, '\r\n') + '\r\n');
            }
        } else {
            term.writeln('\x1b[36m' + c2t('c2.sessions.terminalWelcome') + '\x1b[0m');
        }
        term.write(prompt);
        term.scrollToBottom();

        term.onData(function (e) {
            if (e === '\x0c') {
                term.clear();
                resetInputLine();
                C2.terminalLogs[sessionId] = '';
                return;
            }
            if (e === '\x03') {
                if (C2.terminalBusy) {
                    term.writeln('');
                    term.writeln('\x1b[33m^C (' + c2t('c2.sessions.termCtrlC') + ')\x1b[0m');
                }
                resetInputLine();
                return;
            }
            if (e === '\x16') {
                if (navigator.clipboard && navigator.clipboard.readText) {
                    navigator.clipboard.readText().then(handlePasteText).catch(function () {});
                }
                return;
            }
            if (e.length > 1 && e.indexOf('\x1b') !== 0) {
                handlePasteText(e);
                return;
            }
            if (e === '\x1b[D' || e === '\x1bOD') {
                if (cursorIndex > 0) {
                    cursorIndex--;
                    term.write('\x1b[D');
                }
                return;
            }
            if (e === '\x1b[C' || e === '\x1bOC') {
                if (cursorIndex < lineBuffer.length) {
                    cursorIndex++;
                    term.write('\x1b[C');
                }
                return;
            }
            if (e === '\x1b[1;3D' || e === '\x1bb') {
                moveWordLeft();
                return;
            }
            if (e === '\x1b[1;3C' || e === '\x1bf') {
                moveWordRight();
                return;
            }
            if (e === '\x1b[A' || e === '\x1bOA') {
                const hist = C2.terminalHistory[sessionId] || [];
                if (hist.length === 0) return;
                historyIndex = historyIndex < 0 ? hist.length - 1 : Math.max(0, historyIndex - 1);
                showHistoryEntry(hist[historyIndex]);
                return;
            }
            if (e === '\x1b[B' || e === '\x1bOB') {
                const hist = C2.terminalHistory[sessionId] || [];
                if (hist.length === 0) return;
                historyIndex = historyIndex < 0 ? -1 : Math.min(hist.length - 1, historyIndex + 1);
                if (historyIndex < 0) showHistoryEntry('');
                else showHistoryEntry(hist[historyIndex]);
                return;
            }
            if (e === '\x1b[H' || e === '\x1bOH' || e === '\x01') {
                if (cursorIndex > 0) {
                    term.write('\x1b[' + cursorIndex + 'D');
                    cursorIndex = 0;
                }
                return;
            }
            if (e === '\x1b[F' || e === '\x1bOF' || e === '\x05') {
                const move = lineBuffer.length - cursorIndex;
                if (move > 0) {
                    term.write('\x1b[' + move + 'C');
                    cursorIndex = lineBuffer.length;
                }
                return;
            }
            if (e === '\x1b[3~') {
                if (cursorIndex < lineBuffer.length) {
                    lineBuffer = lineBuffer.slice(0, cursorIndex) + lineBuffer.slice(cursorIndex + 1);
                    redrawInputLine();
                }
                return;
            }
            if (e === '\x15') {
                resetInputLine();
                return;
            }
            if (e === '\x0b') {
                lineBuffer = lineBuffer.slice(0, cursorIndex);
                redrawInputLine();
                return;
            }
            if (e === '\x17') {
                deleteWordBeforeCursor();
                return;
            }
            if (e === '\x1b\x7f') {
                deleteWordBeforeCursor();
                return;
            }

            const code = e.charCodeAt(0);
            if (code === 13 || code === 10) {
                submitCurrentLine();
            } else if (code === 127 || code === 8) {
                if (cursorIndex > 0) {
                    lineBuffer = lineBuffer.slice(0, cursorIndex - 1) + lineBuffer.slice(cursorIndex);
                    cursorIndex--;
                    redrawInputLine();
                }
            } else if (e.length === 1 && code >= 32) {
                historyIndex = -1;
                lineBuffer = lineBuffer.slice(0, cursorIndex) + e + lineBuffer.slice(cursorIndex);
                cursorIndex++;
                if (cursorIndex === lineBuffer.length) {
                    term.write(e);
                } else {
                    redrawInputLine();
                }
            }
        });

        const onTerminalPaste = function (ev) {
            const text = ev.clipboardData && ev.clipboardData.getData('text');
            if (!text) return;
            ev.preventDefault();
            handlePasteText(text);
        };
        if (term.element) {
            term.element.addEventListener('paste', onTerminalPaste);
        }

        term.attachCustomKeyEventHandler(function (ev) {
            if (ev.type !== 'keydown') return true;
            if ((ev.ctrlKey || ev.metaKey) && !ev.shiftKey && (ev.key === 'c' || ev.key === 'C')) {
                if (term.getSelection()) return true;
            }
            const isPaste = (ev.ctrlKey || ev.metaKey) && !ev.shiftKey && !ev.altKey
                && (ev.key === 'v' || ev.key === 'V');
            if (isPaste && navigator.clipboard && navigator.clipboard.readText) {
                ev.preventDefault();
                navigator.clipboard.readText().then(handlePasteText).catch(function () {});
                return false;
            }
            if (ev.shiftKey && ev.key === 'Insert' && navigator.clipboard && navigator.clipboard.readText) {
                ev.preventDefault();
                navigator.clipboard.readText().then(handlePasteText).catch(function () {});
                return false;
            }
            return true;
        });

        container.addEventListener('click', function () {
            term.focus();
        });
        container.setAttribute('tabindex', '0');

        C2.bindTerminalScrollbarAutoHide(container);

        C2.terminalInstance = term;

        if (C2.terminalResizeObserver) {
            C2.terminalResizeObserver.disconnect();
        }
        C2.terminalResizeObserver = new ResizeObserver(function () {
            C2.fitTerminal();
        });
        C2.terminalResizeObserver.observe(container);

        requestAnimationFrame(function () {
            requestAnimationFrame(function () {
                C2.fitTerminal();
                term.focus();
            });
        });
    };

    C2.bindTerminalScrollbarAutoHide = function(container) {
        if (!container) return;
        if (C2._terminalScrollbarHideTimer) {
            clearTimeout(C2._terminalScrollbarHideTimer);
            C2._terminalScrollbarHideTimer = null;
        }
        const show = function () {
            container.classList.add('is-scrollbar-visible');
            if (C2._terminalScrollbarHideTimer) clearTimeout(C2._terminalScrollbarHideTimer);
            C2._terminalScrollbarHideTimer = setTimeout(function () {
                container.classList.remove('is-scrollbar-visible');
                C2._terminalScrollbarHideTimer = null;
            }, 900);
        };
        const bindViewport = function () {
            const vp = container.querySelector('.xterm-viewport');
            if (!vp) return false;
            if (vp.dataset.scrollbarBound === '1') return true;
            vp.dataset.scrollbarBound = '1';
            vp.addEventListener('scroll', show, { passive: true });
            return true;
        };
        if (!container.dataset.scrollbarUiBound) {
            container.dataset.scrollbarUiBound = '1';
            container.addEventListener('wheel', show, { passive: true });
            container.addEventListener('touchmove', show, { passive: true });
        }
        if (!bindViewport()) {
            const mo = new MutationObserver(function () {
                if (bindViewport()) mo.disconnect();
            });
            mo.observe(container, { childList: true, subtree: true });
            setTimeout(function () { mo.disconnect(); }, 3000);
        }
    };

    C2.fitTerminal = function() {
        const container = document.getElementById('c2-terminal-container');
        if (!container || !C2.terminalFitAddon || !C2.terminalInstance) return;
        const rect = container.getBoundingClientRect();
        if (rect.width < 20 || rect.height < 20) return;
        try {
            C2.terminalFitAddon.fit();
            C2.terminalInstance.scrollToBottom();
        } catch (e) {}
    };

    C2.clearTerminal = function() {
        if (C2.terminalInstance) {
            C2.terminalInstance.clear();
            C2.terminalInstance.writeln('\x1b[36m' + c2t('c2.sessions.termCleared') + '\x1b[0m');
            C2.terminalInstance.write('$ ');
            C2.terminalInstance.scrollToBottom();
            if (C2.terminalSessionId) {
                C2.terminalLogs[C2.terminalSessionId] = C2.serializeTerminalBuffer(C2.terminalInstance);
            }
        }
        C2.terminalQueue = [];
    };

    C2.copyTerminal = function() {
        if (!C2.terminalInstance) return;
        const text = C2.terminalInstance.getSelection();
        if (text) copyToClipboard(text);
        else showToast(c2t('c2.sessions.termNoSelection'), 'warning');
    };

    // ============================================================================
    // 文件管理
    // ============================================================================

    C2.normalizeFilePath = function(path) {
        var p = path == null ? '.' : String(path).trim();
        if (!p || p === '/') return '.';
        p = p.replace(/\\/g, '/').replace(/\/+/g, '/').replace(/\/+$/, '');
        return p || '.';
    };

    C2.joinFilePath = function(base, name) {
        var b = C2.normalizeFilePath(base);
        var n = String(name || '').trim().replace(/\\/g, '/').replace(/^\/+/, '');
        if (!n) return b;
        if (b === '.' || b === '/') return n;
        return b + '/' + n;
    };

    /** 将相对浏览路径解析为 implant 工作目录下的绝对路径 */
    C2.resolvePathAgainstPwd = function(pwd, rel) {
        var base = String(pwd || '').trim().replace(/\\/g, '/').replace(/\/+$/, '');
        if (!base) base = '/';
        if (!base.startsWith('/')) base = '/' + base;
        var parts = String(rel || '.').replace(/\\/g, '/').split('/');
        var stack = base === '/' ? [] : base.split('/').filter(Boolean);
        for (var i = 0; i < parts.length; i++) {
            var p = parts[i];
            if (!p || p === '.') continue;
            if (p === '..') {
                if (stack.length) stack.pop();
            } else {
                stack.push(p);
            }
        }
        return '/' + stack.join('/');
    };

    /** 将 /d:/path/file 转为 Windows 远程路径 d:\path\file */
    C2.toWindowsRemotePath = function(path) {
        var p = String(path || '').trim().replace(/\\/g, '/');
        if (/^\/[a-zA-Z]:\//.test(p)) {
            p = p.slice(1);
        }
        return p.replace(/\//g, '\\');
    };

    C2.sessionIsWindows = function(session) {
        if (!session) return false;
        return String(session.os || '').toLowerCase().indexOf('windows') >= 0;
    };

    C2.resolveRemotePath = function(browsePath, filename) {
        var joined = C2.joinFilePath(browsePath || '.', filename);
        if (!C2.implantPwd) return joined;
        var resolved = C2.resolvePathAgainstPwd(C2.implantPwd, joined);
        var session = null;
        if (C2.selectedSessionId && C2.sessions) {
            session = C2.sessions.find(function(s) { return s.id === C2.selectedSessionId; });
        }
        if (C2.sessionIsWindows(session)) {
            return C2.toWindowsRemotePath(resolved);
        }
        return resolved;
    };

    C2.updateFileBreadcrumb = function(browsePath) {
        var breadcrumb = document.getElementById('c2-current-path');
        if (!breadcrumb) return;
        var rel = C2.normalizeFilePath(browsePath || '.');
        if (C2.implantPwd) {
            breadcrumb.textContent = C2.resolvePathAgainstPwd(C2.implantPwd, rel);
            breadcrumb.title = rel;
        } else {
            breadcrumb.textContent = rel;
            breadcrumb.title = '';
        }
    };

    C2.parseLsLine = function(line) {
        var trimmed = String(line || '').trim();
        if (!trimmed || /^total\s+\d+/i.test(trimmed)) return null;

        // Beacon 结构化输出：type\tmode\tsize\tname
        var beaconParts = trimmed.split('\t');
        if (beaconParts.length >= 4) {
            var bName = beaconParts.slice(3).join('\t').trim();
            var bMode = beaconParts[1].trim();
            var bType = beaconParts[0].trim();
            if (bName && bName !== '.' && bName !== '..') {
                return {
                    mode: bMode || bType,
                    size: beaconParts[2].trim(),
                    name: bName,
                    isDir: bType.charAt(0) === 'd' || bMode.charAt(0) === 'd'
                };
            }
            return null;
        }

        // 原生 ls -l 输出
        var m = trimmed.match(/^(\S+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.+)$/);
        if (!m) return null;
        var name = m[9].trim();
        var arrow = name.indexOf(' -> ');
        if (arrow > 0) name = name.slice(0, arrow).trim();
        if (!name || name === '.' || name === '..') return null;
        return {
            mode: m[1],
            size: m[5],
            name: name,
            isDir: m[1].charAt(0) === 'd'
        };
    };

    C2.isDownloadShellError = function(text) {
        var lower = String(text || '').toLowerCase();
        return lower.indexOf('c2_download_err:') >= 0 ||
            lower.indexOf('no such file') >= 0 ||
            lower.indexOf('permission denied') >= 0 ||
            lower.indexOf('is a directory') >= 0 ||
            lower.indexOf('cannot open') >= 0 ||
            lower.indexOf('not a regular file') >= 0;
    };

    C2.refreshImplantPwd = function(sessionId, callback) {
        if (!sessionId) {
            if (callback) callback();
            return;
        }
        apiRequest('POST', `${API_BASE}/tasks`, {
            session_id: sessionId,
            task_type: 'pwd',
            payload: {}
        }).then(function(data) {
            if (data.error) {
                if (callback) callback();
                return;
            }
            var taskId = data.task && data.task.id ? data.task.id : data.task_id;
            if (!taskId) {
                if (callback) callback();
                return;
            }
            C2.waitForImplantPwd(taskId, callback);
        }).catch(function() {
            if (callback) callback();
        });
    };

    C2.waitForImplantPwd = function(taskId, callback) {
        var attempts = 0;
        var poll = function() {
            if (++attempts > 30) {
                if (callback) callback();
                return;
            }
            apiRequest('GET', `${API_BASE}/tasks/${taskId}`).then(function(data) {
                var task = data.task;
                if (task && task.status === 'success' && task.resultText) {
                    C2.implantPwd = String(task.resultText).trim().split('\n').pop().trim();
                    C2.updateFileBreadcrumb(C2.currentPath);
                    if (callback) callback();
                } else if (task && task.status === 'failed') {
                    if (callback) callback();
                } else {
                    setTimeout(poll, 300);
                }
            });
        };
        poll();
    };

    C2.getParentFilePath = function(path) {
        var p = C2.normalizeFilePath(path);
        if (p === '.' || p === '/') return '.';
        var idx = p.lastIndexOf('/');
        if (idx < 0) return '.';
        var parent = p.slice(0, idx);
        return parent || '.';
    };

    C2.goToParentDirectory = function() {
        var parent = C2.getParentFilePath(C2.currentPath || '.');
        C2.loadFileList(null, parent);
    };

    C2.openDirectory = function(name) {
        var next = C2.joinFilePath(C2.currentPath || '.', name);
        C2.loadFileList(null, next);
    };

    C2.loadFileList = function(sessionId, path) {
        // 兼容误传：仅传路径时（如旧版 loadFileList('..')）自动纠正
        if (sessionId && path == null && typeof sessionId === 'string' &&
            (sessionId === '..' || sessionId === '.' || sessionId.indexOf('/') >= 0)) {
            path = sessionId;
            sessionId = null;
        }
        if (!sessionId) sessionId = C2.selectedSessionId;
        if (!sessionId) return;
        if (!path) path = C2.currentPath || '.';
        path = C2.normalizeFilePath(path);

        const container = document.getElementById('c2-file-list');
        const breadcrumb = document.getElementById('c2-current-path');
        
        if (container) container.innerHTML = '<div class="c2-loading">' + escapeHtml(c2t('c2.files.loading')) + '</div>';

        apiRequest('POST', `${API_BASE}/tasks`, {
            session_id: sessionId,
            task_type: 'ls',
            payload: { path: path }
        }).then(data => {
            if (data.error) {
                if (container) container.innerHTML = `<div class="c2-error">${data.error}</div>`;
                return;
            }
            C2.waitForFileList(data.task?.id || data.task_id, sessionId, path);
        });
    };

    C2.waitForFileList = function(taskId, sessionId, path) {
        let attempts = 0;
        const container = document.getElementById('c2-file-list');
        const check = () => {
            if (++attempts > 60) {
                if (container) container.innerHTML = '<div class="c2-error">' + escapeHtml(c2t('c2.files.timeout')) + '</div>';
                return;
            }
            apiRequest('GET', `${API_BASE}/tasks/${taskId}`).then(data => {
                const task = data.task;
                if (task && task.status === 'success') {
                    C2.currentPath = path;
                    C2.updateFileBreadcrumb(path);
                    C2.renderFileList(task.resultText || '');
                    C2.refreshImplantPwd(sessionId);
                } else if (task && task.status === 'failed') {
                    if (container) container.innerHTML = `<div class="c2-error">${escapeHtml(task.error || c2t('c2.files.failed'))}</div>`;
                } else {
                    setTimeout(check, 500);
                }
            });
        };
        check();
    };

    C2.renderFileList = function(output) {
        const container = document.getElementById('c2-file-list');
        if (!container) return;

        const entries = output.split('\n')
            .map(C2.parseLsLine)
            .filter(function(entry) { return entry != null; });
        if (entries.length === 0) {
            container.innerHTML = '<div class="c2-empty">' + escapeHtml(c2t('c2.files.emptyDir')) + '</div>';
            return;
        }

        container.innerHTML = `
            <table class="c2-file-table">
                <thead>
                    <tr>
                        <th>${escapeHtml(c2t('c2.files.colName'))}</th>
                        <th>${escapeHtml(c2t('c2.files.colSize'))}</th>
                        <th>${escapeHtml(c2t('c2.files.colMode'))}</th>
                        <th>${escapeHtml(c2t('c2.files.colActions'))}</th>
                    </tr>
                </thead>
                <tbody>
                    ${entries.map(function(entry) {
                        const sizeLabel = entry.isDir ? '—' : formatBytes(entry.size);
                        const iconCls = entry.isDir ? 'c2-file-icon c2-file-icon--dir' : 'c2-file-icon c2-file-icon--file';
                        return `
                            <tr>
                                <td>
                                    <div class="c2-file-name">
                                        <span class="${iconCls}" aria-hidden="true"></span>
                                        <span class="c2-file-name-text" title="${escapeAttr(entry.name)}">${escapeHtml(entry.name)}</span>
                                    </div>
                                </td>
                                <td title="${escapeAttr(String(entry.size || ''))}">${escapeHtml(sizeLabel)}</td>
                                <td>${escapeHtml(entry.mode)}</td>
                                <td>
                                    <button type="button"
                                        class="btn-ghost btn-sm c2-file-action-btn"
                                        data-c2-file-action="${entry.isDir ? 'open' : 'download'}"
                                        data-c2-file-name="${escapeAttr(entry.name)}">${escapeHtml(c2t(entry.isDir ? 'c2.files.open' : 'c2.files.download'))}</button>
                                </td>
                            </tr>
                        `;
                    }).join('')}
                </tbody>
            </table>
        `;
    };

    C2.refreshFiles = function() {
        C2.loadFileList(null, C2.currentPath);
    };

    C2.sessionTransport = function(session) {
        if (!session || !session.metadata) return '';
        return String(session.metadata.transport || '').toLowerCase();
    };

    C2.sessionSupportsUpload = function(session) {
        if (!session) {
            return { supported: false, reasonKey: 'c2.files.uploadUnsupported' };
        }
        if (session.implantUuid && String(session.implantUuid).indexOf('curl_') === 0) {
            return { supported: false, reasonKey: 'c2.files.uploadCurlBeacon' };
        }
        var transport = C2.sessionTransport(session);
        // 编译 Beacon：HTTP/HTTPS/TCP(CSB1) 均走二进制/结构化协议，支持 upload
        if (transport === 'tcp_beacon' || transport === 'http_beacon' || transport === 'https_beacon') {
            return { supported: true, reasonKey: '' };
        }
        // 经典 TCP 反弹 Shell（bash/nc，metadata.transport=tcp_reverse）
        if (transport === 'tcp_reverse' || (session.hostname && String(session.hostname).indexOf('tcp_') === 0)) {
            return { supported: false, reasonKey: 'c2.files.uploadTcpShell' };
        }
        return { supported: true, reasonKey: '' };
    };

    C2.updateFileUploadButton = function(session) {
        if (!session && C2.selectedSessionId) {
            session = C2.sessions.find(function(s) { return s.id === C2.selectedSessionId; });
        }
        var btn = document.getElementById('c2-file-upload-btn');
        if (!btn) return;
        var cap = C2.sessionSupportsUpload(session);
        btn.disabled = !cap.supported || !!C2.fileUploadBusy;
        btn.title = cap.supported ? c2t('c2.files.upload') : c2t(cap.reasonKey);
        if (!cap.supported) {
            btn.classList.add('is-disabled');
        } else {
            btn.classList.remove('is-disabled');
        }
        var hint = document.getElementById('c2-file-upload-hint');
        if (hint) {
            if (!cap.supported) {
                hint.hidden = false;
                hint.textContent = c2t(cap.reasonKey);
            } else {
                hint.hidden = true;
                hint.textContent = '';
            }
        }
    };

    C2.setFileUploadProgress = function(visible, percent, filename) {
        var row = document.getElementById('c2-file-upload-progress');
        if (!row) return;
        if (!visible) {
            row.hidden = true;
            return;
        }
        row.hidden = false;
        var fill = document.getElementById('c2-file-upload-progress-fill');
        var label = document.getElementById('c2-file-upload-progress-label');
        if (fill) fill.style.width = Math.max(0, Math.min(100, percent || 0)) + '%';
        if (label) {
            label.textContent = c2t('c2.files.uploading', { name: filename || '', percent: percent || 0 });
        }
    };

    C2.openFileUploadPicker = function() {
        if (!C2.selectedSessionId || C2.fileUploadBusy) return;
        var session = C2.sessions.find(function(s) { return s.id === C2.selectedSessionId; });
        var cap = C2.sessionSupportsUpload(session);
        if (!cap.supported) {
            showToast(c2t(cap.reasonKey), 'warn');
            return;
        }
        var inp = document.getElementById('c2-file-upload-input');
        if (inp) inp.click();
    };

    C2.onC2FileUploadPick = function(ev) {
        var input = ev && ev.target;
        var file = input && input.files && input.files[0];
        if (!file) return;
        if (input) input.value = '';
        C2.uploadFileToImplant(file);
    };

    C2.uploadFileToImplant = function(file) {
        if (!C2.selectedSessionId || C2.fileUploadBusy || !file) return;
        var sessionId = C2.selectedSessionId;
        var remotePath = C2.resolveRemotePath(C2.currentPath || '.', file.name);
        var uploadUrl = API_BASE + '/files/upload';

        C2.fileUploadBusy = true;
        C2.updateFileUploadButton();
        C2.setFileUploadProgress(true, 0, file.name);

        var form = new FormData();
        form.append('session_id', sessionId);
        form.append('remote_path', remotePath);
        form.append('file', file);

        var uploadPromise;
        if (typeof apiUploadWithProgress === 'function') {
            uploadPromise = apiUploadWithProgress(uploadUrl, form, {
                onProgress: function(p) {
                    C2.setFileUploadProgress(true, Math.min(p.percent || 0, 50), file.name);
                }
            });
        } else if (typeof apiFetch === 'function') {
            uploadPromise = apiFetch(uploadUrl, { method: 'POST', body: form });
        } else {
            uploadPromise = fetch(uploadUrl, { method: 'POST', body: form });
        }

        uploadPromise.then(function(res) {
            if (!res.ok) {
                return res.text().then(function(text) {
                    throw new Error(text || c2t('c2.files.failed'));
                });
            }
            return res.json();
        }).then(function(uploadData) {
            var fileId = uploadData && uploadData.file_id;
            if (!fileId) throw new Error(c2t('c2.files.failed'));
            C2.setFileUploadProgress(true, 55, file.name);
            return apiRequest('POST', API_BASE + '/tasks', {
                session_id: sessionId,
                task_type: 'upload',
                payload: { remote_path: remotePath, file_id: fileId }
            });
        }).then(function(taskData) {
            if (taskData && taskData.error) throw new Error(taskData.error);
            var taskId = taskData.task && taskData.task.id ? taskData.task.id : taskData.task_id;
            if (!taskId) {
                showToast(c2t('c2.files.uploadQueued'), 'success');
                C2.fileUploadBusy = false;
                C2.setFileUploadProgress(false);
                C2.updateFileUploadButton();
                return;
            }
            if (taskData.task && taskData.task.approvalStatus === 'pending') {
                showToast(c2t('c2.files.uploadPendingApproval'), 'info');
            }
            C2.waitForFileUpload(taskId, file.name);
        }).catch(function(err) {
            showToast((err && err.message) || c2t('c2.files.failed'), 'error');
            C2.fileUploadBusy = false;
            C2.setFileUploadProgress(false);
            C2.updateFileUploadButton();
        });
    };

    C2.waitForFileUpload = function(taskId, filename) {
        var attempts = 0;
        var check = function() {
            if (++attempts > 120) {
                showToast(c2t('c2.files.timeout'), 'error');
                C2.fileUploadBusy = false;
                C2.setFileUploadProgress(false);
                C2.updateFileUploadButton();
                return;
            }
            apiRequest('GET', API_BASE + '/tasks/' + taskId).then(function(data) {
                var task = data.task;
                if (task && task.approvalStatus === 'pending' && task.status === 'queued') {
                    C2.setFileUploadProgress(true, 60, filename);
                    setTimeout(check, 1000);
                    return;
                }
                if (task && task.status === 'success') {
                    C2.setFileUploadProgress(true, 100, filename);
                    showToast(c2t('c2.files.uploadOk'), 'success');
                    C2.fileUploadBusy = false;
                    setTimeout(function() { C2.setFileUploadProgress(false); }, 400);
                    C2.updateFileUploadButton();
                    C2.refreshFiles();
                } else if (task && task.status === 'failed') {
                    showToast(task.error || task.resultText || c2t('c2.files.failed'), 'error');
                    C2.fileUploadBusy = false;
                    C2.setFileUploadProgress(false);
                    C2.updateFileUploadButton();
                } else {
                    var pct = 60 + Math.min(35, Math.floor(attempts / 3));
                    C2.setFileUploadProgress(true, pct, filename);
                    setTimeout(check, 500);
                }
            }).catch(function() {
                C2.fileUploadBusy = false;
                C2.setFileUploadProgress(false);
                C2.updateFileUploadButton();
            });
        };
        check();
    };

    C2.saveDownloadBlob = function(blob, filename) {
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(a.href);
    };

    C2.saveDownloadContent = function(content, filename) {
        const text = String(content || '');
        if (C2.isDownloadShellError(text)) {
            throw new Error(text.trim() || c2t('c2.files.failed'));
        }
        const b64 = text.replace(/\s/g, '');
        let bytes;
        try {
            if (/^[A-Za-z0-9+/=]+$/.test(b64) && b64.length > 0) {
                const binary = atob(b64);
                bytes = new Uint8Array(binary.length);
                for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
            } else {
                bytes = new TextEncoder().encode(text);
            }
        } catch (e) {
            bytes = new TextEncoder().encode(text);
        }
        C2.saveDownloadBlob(new Blob([bytes], { type: 'application/octet-stream' }), filename);
    };

    C2.fetchTaskResultFile = function(taskId, filename) {
        const url = `${API_BASE}/tasks/${taskId}/result-file`;
        const fetchFn = (typeof apiFetch === 'function') ? apiFetch : fetch;
        fetchFn(url).then(resp => {
            if (!resp.ok) throw new Error('download failed: ' + resp.status);
            return resp.blob();
        }).then(blob => {
            C2.saveDownloadBlob(blob, filename);
        }).catch(err => {
            showToast((err && err.message) || c2t('c2.files.failed'), 'error');
        });
    };

    C2.waitForFileDownload = function(taskId, filename) {
        let attempts = 0;
        const check = () => {
            if (++attempts > 120) {
                showToast(c2t('c2.files.timeout'), 'error');
                return;
            }
            apiRequest('GET', `${API_BASE}/tasks/${taskId}`).then(data => {
                const task = data.task;
                if (task && task.status === 'success') {
                    if (task.resultBlobPath) {
                        C2.fetchTaskResultFile(taskId, filename);
                    } else if (task.resultText != null) {
                        try {
                            C2.saveDownloadContent(task.resultText, filename);
                            showToast(c2t('c2.files.downloadOk'), 'success');
                        } catch (err) {
                            showToast((err && err.message) || c2t('c2.files.failed'), 'error');
                        }
                    } else {
                        C2.saveDownloadBlob(new Blob([], { type: 'application/octet-stream' }), filename);
                        showToast(c2t('c2.files.downloadOk'), 'success');
                    }
                } else if (task && task.status === 'failed') {
                    showToast(task.error || task.resultText || c2t('c2.files.failed'), 'error');
                } else {
                    setTimeout(check, 500);
                }
            });
        };
        check();
    };

    C2.downloadFile = function(filename) {
        if (!C2.selectedSessionId) return;
        const remotePath = C2.resolveRemotePath(C2.currentPath || '.', filename);

        apiRequest('POST', `${API_BASE}/tasks`, {
            session_id: C2.selectedSessionId,
            task_type: 'download',
            payload: { remote_path: remotePath }
        }).then(data => {
            if (data.error) {
                showToast(data.error, 'error');
                return;
            }
            const taskId = data.task?.id || data.task_id;
            if (!taskId) {
                showToast(c2t('c2.payloads.toastDownloadQueued'), 'success');
                return;
            }
            C2.waitForFileDownload(taskId, filename);
        });
    };

    // ============================================================================
    // 任务管理
    // ============================================================================

    C2.loadTasks = function(page) {
        bindTasksFilterUiOnce();
        readTasksFilterFromDom();
        const p = page != null ? page : (C2.tasksPage || 1);
        C2.tasksPage = p;
        const ps = C2.tasksPageSize || 10;
        const qs = buildTasksQueryParams(p, ps);
        apiRequest('GET', `${API_BASE}/tasks?${qs}`).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            C2.tasks = data.tasks || [];
            C2.tasksTotal = typeof data.total === 'number' ? data.total : (C2.tasks.length || 0);
            C2.tasksStatusCounts = data.status_counts || { success: 0, failed: 0, pending: 0, cancelled: 0 };
            if (typeof data.pending_queued_count === 'number') {
                C2.tasksPendingQueuedCount = data.pending_queued_count;
            }
            const maxPage = Math.max(1, Math.ceil(C2.tasksTotal / ps));
            if (p > maxPage) {
                C2.loadTasks(maxPage);
                return;
            }
            C2.renderTasks();
            C2.renderTasksSummary();
            C2.renderTasksPagination();
            C2.syncTasksToolbar();
            syncTasksTimePresetButtons();
        }).catch(err => {
            showToast(err.message || String(err), 'error');
        });
    };

    C2.goTasksPage = function(targetPage) {
        const totalPages = Math.max(1, Math.ceil((C2.tasksTotal || 0) / (C2.tasksPageSize || 10)));
        if (targetPage < 1 || targetPage > totalPages) return;
        C2.loadTasks(targetPage);
        const list = document.getElementById('c2-task-list');
        if (list) list.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };

    C2.changeTasksPageSize = function() {
        const sel = document.getElementById('c2-tasks-page-size-pagination');
        if (!sel) return;
        const n = parseInt(sel.value, 10);
        if (n > 0) {
            C2.tasksPageSize = n;
            C2.loadTasks(1);
        }
    };

    C2.renderTasksPagination = function() {
        const paginationContainer = document.getElementById('c2-tasks-pagination');
        if (!paginationContainer) return;
        const total = C2.tasksTotal || 0;
        const currentPage = C2.tasksPage || 1;
        const pageSize = C2.tasksPageSize || 10;
        const totalPages = Math.max(1, Math.ceil(total / pageSize));
        if (total === 0) {
            paginationContainer.innerHTML = '';
            if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
            return;
        }
        const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
        const end = Math.min(currentPage * pageSize, total);
        let html = '<div class="monitor-pagination">';
        html += `
            <div class="pagination-info">
                <span>${escapeHtml(c2t('c2.tasks.paginationShow', { start, end, total }))}</span>
                <label class="pagination-page-size">
                    ${escapeHtml(c2t('c2.tasks.paginationPerPage'))}
                    <select id="c2-tasks-page-size-pagination" onchange="C2.changeTasksPageSize()">
                        <option value="10" ${pageSize === 10 ? 'selected' : ''}>10</option>
                        <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                        <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                        <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                    </select>
                </label>
            </div>
            <div class="pagination-controls">
                <button type="button" class="btn-secondary" onclick="C2.goTasksPage(1)" ${currentPage === 1 ? 'disabled' : ''}>${escapeHtml(c2t('c2.tasks.paginationFirst'))}</button>
                <button type="button" class="btn-secondary" onclick="C2.goTasksPage(${currentPage - 1})" ${currentPage === 1 ? 'disabled' : ''}>${escapeHtml(c2t('c2.tasks.paginationPrev'))}</button>
                <span class="pagination-page">${escapeHtml(c2t('c2.tasks.paginationPage', { current: currentPage, total: totalPages }))}</span>
                <button type="button" class="btn-secondary" onclick="C2.goTasksPage(${currentPage + 1})" ${currentPage >= totalPages ? 'disabled' : ''}>${escapeHtml(c2t('c2.tasks.paginationNext'))}</button>
                <button type="button" class="btn-secondary" onclick="C2.goTasksPage(${totalPages})" ${currentPage >= totalPages ? 'disabled' : ''}>${escapeHtml(c2t('c2.tasks.paginationLast'))}</button>
            </div>
        `;
        html += '</div>';
        paginationContainer.innerHTML = html;
        if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
        if (typeof applyTranslations === 'function') applyTranslations(paginationContainer);
    };

    C2.collectCheckedTaskIds = function() {
        return Array.from(document.querySelectorAll('.c2-task-row-check:checked')).map(cb => cb.getAttribute('data-id')).filter(Boolean);
    };

    C2.syncTasksToolbar = function() {
        const batchBtn = document.getElementById('c2-tasks-batch-delete');
        const ids = C2.collectCheckedTaskIds();
        if (batchBtn) batchBtn.disabled = ids.length === 0;
        const all = document.querySelectorAll('.c2-task-row-check');
        const selAll = document.getElementById('c2-tasks-select-all');
        if (selAll && all.length) {
            const nChecked = document.querySelectorAll('.c2-task-row-check:checked').length;
            selAll.checked = nChecked === all.length;
            selAll.indeterminate = nChecked > 0 && nChecked < all.length;
        } else if (selAll) {
            selAll.checked = false;
            selAll.indeterminate = false;
        }
    };

    C2.onTasksSelectAll = function(checked) {
        document.querySelectorAll('.c2-task-row-check').forEach(cb => { cb.checked = checked; });
        C2.syncTasksToolbar();
    };

    C2.deleteTaskById = function(id) {
        if (!id) return;
        if (!confirm(c2t('c2.tasks.confirmDeleteOne'))) return;
        apiRequest('DELETE', `${API_BASE}/tasks`, { ids: [id] }).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            showToast(c2t('c2.tasks.toastDeleted', { n: data.deleted != null ? data.deleted : 1 }), 'success');
            C2.loadTasks(C2.tasksPage || 1);
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    C2.deleteSelectedTasks = function() {
        const ids = C2.collectCheckedTaskIds();
        if (!ids.length) {
            showToast(c2t('c2.tasks.toastSelectFirst'), 'warn');
            return;
        }
        if (!confirm(c2t('c2.tasks.confirmBatchDelete', { n: ids.length }))) return;
        apiRequest('DELETE', `${API_BASE}/tasks`, { ids }).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            const deleted = data.deleted != null ? data.deleted : ids.length;
            showToast(c2t('c2.tasks.toastDeleted', { n: deleted }), 'success');
            C2.loadTasks(C2.tasksPage || 1);
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    function readTasksFilterFromDom() {
        const statusEl = document.getElementById('c2-tasks-filter-status');
        const typeEl = document.getElementById('c2-tasks-filter-type');
        const sessionEl = document.getElementById('c2-tasks-filter-session');
        C2.tasksFilter = C2.tasksFilter || { status: '', task_type: '', session_id: '', since: '', timePreset: 'all' };
        C2.tasksFilter.status = statusEl ? statusEl.value.trim() : '';
        C2.tasksFilter.task_type = typeEl ? typeEl.value.trim() : '';
        C2.tasksFilter.session_id = sessionEl ? sessionEl.value.trim() : '';
    }

    function buildTasksQueryParams(page, pageSize) {
        const params = new URLSearchParams();
        params.set('page', String(page));
        params.set('page_size', String(pageSize));
        const f = C2.tasksFilter || {};
        if (f.status) params.set('status', f.status);
        if (f.task_type) params.set('task_type', f.task_type);
        if (f.session_id) params.set('session_id', f.session_id);
        if (f.since) params.set('since', f.since);
        return params.toString();
    }

    function tasksTimePresetToSince(preset) {
        if (!preset || preset === 'all') return '';
        const now = Date.now();
        const map = { '15m': 15 * 60 * 1000, '1h': 60 * 60 * 1000, '24h': 24 * 60 * 60 * 1000, '7d': 7 * 24 * 60 * 60 * 1000 };
        const ms = map[preset];
        if (!ms) return '';
        return new Date(now - ms).toISOString();
    }

    function syncTasksTimePresetButtons() {
        const wrap = document.getElementById('c2-tasks-time-presets');
        if (!wrap) return;
        const active = (C2.tasksFilter && C2.tasksFilter.timePreset) || 'all';
        wrap.querySelectorAll('.c2-tasks-time-preset-btn').forEach(btn => {
            btn.classList.toggle('is-active', btn.getAttribute('data-preset') === active);
        });
    }

    function bindTasksFilterUiOnce() {
        if (document.documentElement.dataset.c2TasksFilterBound === '1') return;
        document.documentElement.dataset.c2TasksFilterBound = '1';

        const presets = document.getElementById('c2-tasks-time-presets');
        if (presets) {
            presets.addEventListener('click', (e) => {
                const btn = e.target.closest('.c2-tasks-time-preset-btn');
                if (!btn) return;
                const preset = btn.getAttribute('data-preset') || 'all';
                C2.tasksFilter = C2.tasksFilter || { status: '', task_type: '', session_id: '', since: '', timePreset: 'all' };
                C2.tasksFilter.timePreset = preset;
                C2.tasksFilter.since = tasksTimePresetToSince(preset);
                syncTasksTimePresetButtons();
                C2.loadTasks(1);
            });
        }

        const sessionInput = document.getElementById('c2-tasks-filter-session');
        if (sessionInput) {
            sessionInput.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') C2.applyTasksFilters();
            });
        }
    }

    C2.applyTasksFilters = function() {
        readTasksFilterFromDom();
        C2.loadTasks(1);
    };

    C2.resetTasksFilters = function() {
        C2.tasksFilter = { status: '', task_type: '', session_id: '', since: '', timePreset: 'all' };
        const statusEl = document.getElementById('c2-tasks-filter-status');
        const typeEl = document.getElementById('c2-tasks-filter-type');
        const sessionEl = document.getElementById('c2-tasks-filter-session');
        if (statusEl) statusEl.value = '';
        if (typeEl) typeEl.value = '';
        if (sessionEl) sessionEl.value = '';
        syncTasksTimePresetButtons();
        C2.loadTasks(1);
    };

    C2.renderTasksSummary = function() {
        const summary = document.getElementById('c2-tasks-summary');
        if (!summary) return;
        const total = C2.tasksTotal || 0;
        const counts = C2.tasksStatusCounts || { success: 0, failed: 0, pending: 0 };
        const set = (id, val) => {
            const el = document.getElementById(id);
            if (el) el.textContent = String(val);
        };
        set('c2-tasks-stat-total', total);
        set('c2-tasks-stat-success', counts.success || 0);
        set('c2-tasks-stat-failed', counts.failed || 0);
        set('c2-tasks-stat-pending', counts.pending || 0);
        summary.hidden = total === 0;
    };

    C2.loadSessionTasks = function(sessionId) {
        apiRequest('GET', `${API_BASE}/tasks?session_id=${encodeURIComponent(sessionId)}&limit=50`).then(data => {
            const container = document.getElementById('c2-session-tasks-list');
            const tasks = data.tasks || [];
            if (typeof data.pending_queued_count === 'number') {
                C2.tasksPendingQueuedCount = data.pending_queued_count;
            }
            
            if (!container) return;

            const refreshBtn = escapeHtml(c2t('c2.tasks.refresh'));
            const countLabel = escapeHtml(c2t('c2.tasks.sessionTaskCount', { n: tasks.length }));

            if (tasks.length === 0) {
                container.innerHTML = `
                    <div class="c2-session-tasks-panel">
                        <div class="c2-session-tasks-toolbar">
                            <div class="c2-session-tasks-toolbar-title">
                                <span class="c2-session-tasks-heading">${escapeHtml(c2t('c2.tasks.sessionTaskHistory'))}</span>
                                <span class="c2-session-tasks-count">0</span>
                            </div>
                            <button type="button" class="btn-ghost btn-sm c2-session-tasks-refresh" data-c2-action="session-tasks-refresh" data-c2-id="${escapeAttr(sessionId)}">${refreshBtn}</button>
                        </div>
                        <div class="c2-empty-inline">
                            <div class="c2-empty-inline__icon" aria-hidden="true"></div>
                            <div class="c2-empty-inline__text">${escapeHtml(c2t('c2.tasks.emptySession'))}</div>
                        </div>
                    </div>`;
                return;
            }
            
            container.innerHTML = `
                <div class="c2-session-tasks-panel">
                    <div class="c2-session-tasks-toolbar">
                        <div class="c2-session-tasks-toolbar-title">
                            <span class="c2-session-tasks-heading">${escapeHtml(c2t('c2.tasks.sessionTaskHistory'))}</span>
                            <span class="c2-session-tasks-count">${countLabel}</span>
                        </div>
                        <button type="button" class="btn-ghost btn-sm c2-session-tasks-refresh" data-c2-action="session-tasks-refresh" data-c2-id="${escapeAttr(sessionId)}">${refreshBtn}</button>
                    </div>
                    <div class="c2-session-tasks-rows">
                        ${tasks.map(t => {
                            const rawId = t.id || '';
                            const cmd = formatTaskCommand(t);
                            const cmdShort = truncateCommand(cmd, 64);
                            const typeCat = taskTypeCategory(t.taskType);
                            const status = String(t.status || '');
                            const isPending = status === 'queued' || status === 'sent' || status === 'running';
                            const timeStr = formatTime(t.completedAt || t.createdAt);
                            return `
                            <div class="c2-session-task-row ${isPending ? 'is-pending' : ''}" data-status="${escapeAttr(status)}">
                                <div class="c2-session-task-row__main">
                                    <span class="c2-task-status-dot ${escapeAttr(status)}" title="${escapeAttr(taskStatusLabel(status))}"></span>
                                    <span class="c2-task-type-badge c2-task-type-badge--${typeCat}">${escapeHtml(t.taskType || '-')}</span>
                                    <div class="c2-session-task-row__cmd" title="${escapeAttr(cmd || '')}">
                                        ${cmdShort
                                            ? `<code class="c2-session-task-command">${escapeHtml(cmdShort)}</code>`
                                            : `<span class="c2-session-task-command c2-session-task-command--muted">—</span>`}
                                    </div>
                                </div>
                                <div class="c2-session-task-row__meta">
                                    <span class="c2-status-badge ${escapeHtml(status)}">${escapeHtml(taskStatusLabel(status))}</span>
                                    <span class="c2-session-task-duration">${formatDuration(t.durationMs)}</span>
                                    <span class="c2-session-task-time" title="${escapeAttr(timeStr)}">${escapeHtml(formatRelativeTime(t.completedAt || t.createdAt) || timeStr)}</span>
                                    <button type="button" class="btn-secondary btn-small c2-session-task-view" data-c2-task-action="view" data-task-id="${escapeAttr(rawId)}">${escapeHtml(c2t('c2.tasks.view'))}</button>
                                </div>
                            </div>`;
                        }).join('')}
                    </div>
                </div>`;
        });
    };

    C2.renderTasks = function() {
        const container = document.getElementById('c2-task-list');
        if (!container) return;

        const selAll = document.getElementById('c2-tasks-select-all');
        if (selAll) {
            selAll.checked = false;
            selAll.indeterminate = false;
        }

        if (C2.tasks.length === 0) {
            container.innerHTML = '<div class="c2-tasks-empty">' + escapeHtml(c2t('c2.tasks.emptyAll')) + '</div>';
            if (selAll) selAll.disabled = true;
            C2.syncTasksToolbar();
            return;
        }
        if (selAll) selAll.disabled = false;

        const delTitle = escapeHtml(c2t('c2.tasks.deleteOne'));
        const cancelTitle = escapeHtml(c2t('c2.tasks.cancelBtn'));
        const dash = '<span class="c2-tasks-cell-muted">—</span>';
        const deleteIcon = '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/></svg>';

        container.innerHTML = `
            <table class="c2-tasks-table">
                <thead>
                    <tr>
                        <th class="c2-tasks-table-col-check">
                            <label class="c2-task-check-label" title="${escapeAttr(c2t('c2.tasks.selectAll'))}">
                                <input type="checkbox" id="c2-tasks-select-all" onchange="C2.onTasksSelectAll(this.checked)">
                            </label>
                        </th>
                        <th>${escapeHtml(c2t('c2.tasks.colCreated'))}</th>
                        <th>${escapeHtml(c2t('c2.tasks.colStatus'))}</th>
                        <th>${escapeHtml(c2t('c2.tasks.colType'))}</th>
                        <th>${escapeHtml(c2t('c2.tasks.colCommand'))}</th>
                        <th>${escapeHtml(c2t('c2.tasks.colSession'))}</th>
                        <th>${escapeHtml(c2t('c2.tasks.colTask'))}</th>
                        <th>${escapeHtml(c2t('c2.tasks.colDuration'))}</th>
                        <th class="c2-tasks-table-col-actions">${escapeHtml(c2t('c2.tasks.colActions'))}</th>
                    </tr>
                </thead>
                <tbody>
	                    ${C2.tasks.map(t => {
	                        const rawId = t.id || '';
	                        const tid = escapeHtml(rawId);
	                        const tidAttr = escapeAttr(rawId);
	                        const status = String(t.status || '');
                        const rowStatus = escapeHtml(status || 'queued');
                        const typeCat = taskTypeCategory(t.taskType);
                        const sessionFull = t.sessionId ? String(t.sessionId) : '';
                        const sessionShort = sessionFull
                            ? escapeHtml(sessionFull.substring(0, 10)) + (sessionFull.length > 10 ? '\u2026' : '')
                            : '';
                        const taskShort = rawId
                            ? escapeHtml(rawId.substring(0, 10)) + (rawId.length > 10 ? '\u2026' : '')
                            : '';
                        const cmd = formatTaskCommand(t);
                        const cmdEsc = escapeHtml(cmd);
                        const canCancel = status === 'queued' || status === 'sent';
                        return `
	                        <tr class="c2-tasks-row c2-tasks-row--${rowStatus}" data-task-id="${tidAttr}" data-c2-action="task-view" data-c2-id="${tidAttr}" role="button" tabindex="0">
                            <td class="c2-tasks-table-col-check" data-c2-stop-action="1">
                                <label class="c2-task-check-label">
	                                    <input type="checkbox" class="c2-task-row-check" data-id="${tidAttr}" onchange="C2.syncTasksToolbar()">
                                </label>
                            </td>
                            <td class="c2-tasks-col-time">${escapeHtml(formatTime(t.createdAt))}</td>
                            <td><span class="c2-status-badge ${escapeHtml(status)}">${escapeHtml(taskStatusLabel(status))}</span></td>
                            <td><span class="c2-task-type-badge c2-task-type-badge--${typeCat}">${escapeHtml(t.taskType || '-')}</span></td>
                            <td class="c2-tasks-col-command" title="${escapeAttr(cmd)}">${cmdEsc || dash}</td>
	                            <td class="c2-tasks-col-mono" title="${escapeAttr(sessionFull)}">${sessionShort || dash}</td>
	                            <td class="c2-tasks-col-mono" title="${tidAttr}">${taskShort || dash}</td>
                            <td class="c2-tasks-col-duration">${formatDuration(t.durationMs)}</td>
                            <td class="c2-tasks-table-col-actions" data-c2-stop-action="1">
                                <div class="c2-tasks-row-actions">
                                    ${canCancel
	                                        ? `<button type="button" class="c2-tasks-cancel-btn" data-c2-task-action="cancel" data-task-id="${tidAttr}" title="${cancelTitle}" aria-label="${cancelTitle}">${escapeHtml(c2t('c2.tasks.cancelBtn'))}</button>`
	                                        : ''}
	                                    <button type="button" class="c2-tasks-delete-btn" data-require-permission="c2:delete" data-c2-task-action="delete" data-task-id="${tidAttr}" title="${delTitle}" aria-label="${delTitle}">${deleteIcon}</button>
                                </div>
                            </td>
                        </tr>`;
                    }).join('')}
                </tbody>
            </table>
        `;
        C2.syncTasksToolbar();
        if (typeof applyTranslations === 'function') applyTranslations(container);
        if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
    };

    C2.viewTask = function(id) {
        const modal = document.getElementById('c2-modal');
        const content = document.getElementById('c2-modal-content');
        if (!content) return;

        const renderTaskModal = function(t) {
            if (!t || !modal) return;
            const cmd = formatTaskCommand(t);
            const hasPayload = t.payload && typeof t.payload === 'object' && Object.keys(t.payload).length > 0;
            const modalBox = modal.querySelector('.c2-modal');
            if (modalBox) modalBox.classList.add('c2-modal--wide');
            content.innerHTML = `
            <div class="c2-modal-header c2-task-modal-header">
                <div class="c2-task-modal-heading">
                    <h3>${escapeHtml(c2t('c2.tasks.modalTitle'))}</h3>
                    <span class="c2-status-badge ${escapeHtml(t.status || '')}">${escapeHtml(taskStatusLabel(t.status))}</span>
                </div>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <div class="c2-task-detail">
                    <div class="c2-task-detail-grid">
                        <div class="c2-task-kv">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelId'))}</span>
                            <span class="c2-task-kv__value c2-task-kv__value--mono">${escapeHtml(t.id || '-')}</span>
                        </div>
                        <div class="c2-task-kv">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelSession'))}</span>
                            <span class="c2-task-kv__value c2-task-kv__value--mono">${escapeHtml(t.sessionId || '-')}</span>
                        </div>
                        <div class="c2-task-kv">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelType'))}</span>
                            <span class="c2-task-kv__value">${escapeHtml(t.taskType || '-')}</span>
                        </div>
                        <div class="c2-task-kv">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelDuration'))}</span>
                            <span class="c2-task-kv__value c2-task-kv__value--accent">${formatDuration(t.durationMs)}</span>
                        </div>
                    </div>

                    <div class="c2-task-timeline">
                        <div class="c2-task-time-card">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelCreated'))}</span>
                            <span class="c2-task-kv__value">${formatTime(t.createdAt) || '-'}</span>
                        </div>
                        <div class="c2-task-time-card">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelSent'))}</span>
                            <span class="c2-task-kv__value">${formatTime(t.sentAt) || '-'}</span>
                        </div>
                        <div class="c2-task-time-card">
                            <span class="c2-task-kv__label">${escapeHtml(c2t('c2.tasks.labelCompleted'))}</span>
                            <span class="c2-task-kv__value">${formatTime(t.completedAt) || '-'}</span>
                        </div>
                    </div>

                    ${cmd ? `
                        <div class="c2-task-code-section">
                            <div class="c2-task-code-header">
                                <span class="c2-task-code-title">${escapeHtml(c2t('c2.tasks.labelCommand'))}</span>
                                <button type="button" class="btn-ghost btn-sm" onclick="C2.copyTaskBlock('c2-task-cmd-pre')">${escapeHtml(c2t('common.copy'))}</button>
                            </div>
                            <pre class="c2-task-command-pre" id="c2-task-cmd-pre">${escapeHtml(cmd)}</pre>
                        </div>
                    ` : ''}
                    ${hasPayload && !cmd ? `
                        <div class="c2-task-code-section">
                            <div class="c2-task-code-header">
                                <span class="c2-task-code-title">${escapeHtml(c2t('c2.tasks.labelPayload'))}</span>
                                <button type="button" class="btn-ghost btn-sm" onclick="C2.copyTaskBlock('c2-task-payload-pre')">${escapeHtml(c2t('common.copy'))}</button>
                            </div>
                            <pre class="c2-task-command-pre" id="c2-task-payload-pre">${escapeHtml(JSON.stringify(t.payload, null, 2))}</pre>
                        </div>
                    ` : ''}
                    ${t.error ? `
                        <div class="c2-task-error-section">
                            <div class="c2-task-code-header">
                                <span class="c2-task-code-title">${escapeHtml(c2t('c2.tasks.labelError'))}</span>
                            </div>
                            <div class="c2-task-error">${escapeHtml(t.error)}</div>
                        </div>
                    ` : ''}
                    ${t.resultText ? `
                        <div class="c2-task-code-section">
                            <div class="c2-task-code-header">
                                <span class="c2-task-code-title">${escapeHtml(c2t('c2.tasks.labelResult'))}</span>
                                <button type="button" class="btn-ghost btn-sm" onclick="C2.copyTaskBlock('c2-task-result-pre')">${escapeHtml(c2t('common.copy'))}</button>
                            </div>
                            <pre class="c2-task-result-pre" id="c2-task-result-pre">${escapeHtml(t.resultText)}</pre>
                        </div>
                    ` : ''}
                </div>
            </div>
            <div class="c2-modal-footer">
                <button class="btn-secondary" onclick="C2.closeModal()">${escapeHtml(c2t('common.close'))}</button>
            </div>
        `;
            openAppModal(modal);
        };

        const local = C2.tasks.find(x => x.id === id);
        if (local) {
            renderTaskModal(local);
            return;
        }
        apiRequest('GET', `${API_BASE}/tasks/${encodeURIComponent(id)}`).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            if (data.task) renderTaskModal(data.task);
            else showToast(c2t('c2.tasks.emptyAll'), 'warn');
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    C2.cancelTask = function(id) {
        apiRequest('POST', `${API_BASE}/tasks/${encodeURIComponent(id)}/cancel`, {}).then(data => {
            if (data.error) showToast(String(data.error), 'error');
            else {
                showToast(c2t('c2.tasks.toastCancelled'), 'success');
                C2.loadTasks(C2.tasksPage || 1);
            }
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    // ============================================================================
    // Payload 生成
    // ============================================================================

    C2.loadListenersForPayload = function() {
        apiRequest('GET', `${API_BASE}/listeners`).then(data => {
            if (data.error) {
                showToast(data.error, 'error');
                return;
            }
            C2.listeners = data.listeners || [];
            C2.renderPayloadPage();
        }).catch(err => {
            showToast(c2t('c2.payloads.toastLoadListenersFail', { msg: err.message || '' }), 'error');
        });
    };

    var onelinerKindsByListenerType = {
        'tcp_reverse': [
            { value: 'bash',       label: 'Bash (/dev/tcp)' },
            { value: 'nc',         label: 'Netcat (-e)' },
            { value: 'nc_mkfifo',  label: 'Netcat (mkfifo)' },
            { value: 'python',     label: 'Python' },
            { value: 'perl',       label: 'Perl' },
            { value: 'powershell', label: 'PowerShell' }
        ],
        'http_beacon': [
            { value: 'curl_beacon', label: 'Curl Beacon (HTTP)' }
        ],
        'https_beacon': [
            { value: 'curl_beacon', label: 'Curl Beacon (HTTP)' }
        ],
        'websocket': [
            { value: 'curl_beacon', label: 'Curl Beacon (HTTP)' }
        ]
    };

    C2.updateOnelinerKinds = function() {
        var listenerSelect = document.getElementById('c2-payload-listener');
        var kindSelect = document.getElementById('c2-payload-kind');
        if (!listenerSelect || !kindSelect) return;

        var listenerId = listenerSelect.value;
        var listener = (C2.listeners || []).find(function(l) { return l.id === listenerId; });
        var ltype = listener ? listener.type : '';
        var kinds = onelinerKindsByListenerType[ltype] || [];

        if (kinds.length === 0) {
            kindSelect.innerHTML = '<option value="">' + escapeHtml(c2t('c2.payloads.noKindOption')) + '</option>';
        } else {
            kindSelect.innerHTML = kinds.map(function(k) {
                return '<option value="' + k.value + '">' + k.label + '</option>';
            }).join('');
        }
        C2.refreshFormSelects(document.getElementById('page-c2-payloads'));
    };

    C2.updateLoopbackBuildHint = function() {
        const sel = document.getElementById('c2-build-listener');
        const hint = document.getElementById('c2-build-loopback-hint');
        if (!hint) return;
        const override = document.getElementById('c2-build-host') && String(document.getElementById('c2-build-host').value || '').trim();
        if (override) {
            hint.style.display = 'none';
            return;
        }
        const id = sel && sel.value;
        if (!id) {
            hint.style.display = 'none';
            return;
        }
        const l = (C2.listeners || []).find(function(x) { return x.id === id; });
        const h = (l && l.bindHost ? String(l.bindHost) : '').toLowerCase().trim();
        if (h === '127.0.0.1' || h === 'localhost' || h === '::1') {
            hint.textContent = c2t('c2.payloads.loopbackBeaconWarning');
            hint.style.display = 'block';
        } else {
            hint.style.display = 'none';
        }
    };

    C2.renderPayloadPage = function() {
        const optionsHtml = C2.listeners.length > 0
            ? C2.listeners.map(l =>
                `<option value="${escapeAttr(l.id)}">${escapeHtml(l.name)} (${escapeHtml(l.type)} ${escapeHtml(l.bindHost)}:${escapeHtml(l.bindPort)})</option>`
              ).join('')
            : '<option value="">' + escapeHtml(c2t('c2.payloads.noListenersOption')) + '</option>';

        const listenerSelect = document.getElementById('c2-payload-listener');
        if (listenerSelect) {
            listenerSelect.innerHTML = optionsHtml;
            listenerSelect.removeEventListener('change', C2.updateOnelinerKinds);
            listenerSelect.addEventListener('change', C2.updateOnelinerKinds);
        }

        const buildSelect = document.getElementById('c2-build-listener');
        if (buildSelect) {
            const listeners = C2.listeners || [];
            let buildOptionsHtml;
            if (listeners.length > 0) {
                buildOptionsHtml = listeners.map(l =>
                    `<option value="${escapeAttr(l.id)}">${escapeHtml(l.name)} (${escapeHtml(l.type)} ${escapeHtml(l.bindHost)}:${escapeHtml(l.bindPort)})</option>`
                ).join('');
            } else {
                buildOptionsHtml = '<option value="">' + escapeHtml(c2t('c2.payloads.noListenersOption')) + '</option>';
            }
            buildSelect.innerHTML = buildOptionsHtml;
            buildSelect.removeEventListener('change', C2.updateLoopbackBuildHint);
            buildSelect.addEventListener('change', C2.updateLoopbackBuildHint);
            C2.updateLoopbackBuildHint();
        }

        const buildHostInput = document.getElementById('c2-build-host');
        if (buildHostInput) {
            buildHostInput.removeEventListener('input', C2.updateLoopbackBuildHint);
            buildHostInput.addEventListener('input', C2.updateLoopbackBuildHint);
        }

        C2.updateOnelinerKinds();
        const buildBtn = document.getElementById('c2-build-btn');
        if (buildBtn && !buildBtn.disabled) buildBtn.textContent = c2t('c2.payloads.buildBeaconBtn');
        const genBtn = document.getElementById('c2-generate-oneliner-btn');
        if (genBtn) genBtn.textContent = c2t('c2.payloads.generateOnelinerBtn');
        C2.refreshFormSelects(document.getElementById('page-c2-payloads'));
    };

    C2.generateOneliner = function() {
        const listenerId = document.getElementById('c2-payload-listener')?.value;
        const kind = document.getElementById('c2-payload-kind')?.value || 'bash';
        const host = document.getElementById('c2-payload-host')?.value;

        if (!listenerId) {
            showToast(c2t('c2.payloads.toastPickListener'), 'error');
            return;
        }

        apiRequest('POST', `${API_BASE}/payloads/oneliner`, {
            listener_id: listenerId,
            kind: kind,
            host: host
        }).then(data => {
            if (data.error) {
                showToast(data.error, 'error');
            } else {
                const output = document.getElementById('c2-oneliner-output');
                if (output) {
                    output.textContent = data.oneliner;
                    output.style.display = 'block';
                }
            }
        }).catch(err => {
            showToast(c2t('c2.payloads.toastOnelinerFail', { msg: err.message || '' }), 'error');
        });
    };

    C2.copyOneliner = function() {
        const el = document.getElementById('c2-oneliner-output');
        if (el && el.textContent) copyToClipboard(el.textContent);
    };

    C2.buildBeacon = function() {
        const listenerId = document.getElementById('c2-build-listener')?.value;
        const os = document.getElementById('c2-build-os')?.value || 'linux';
        const arch = document.getElementById('c2-build-arch')?.value || 'amd64';
        const host = document.getElementById('c2-build-host')?.value;

        if (!listenerId) {
            showToast(c2t('c2.payloads.toastPickListener'), 'error');
            return;
        }

        const btn = document.getElementById('c2-build-btn');
        if (btn) {
            btn.disabled = true;
            btn.textContent = c2t('c2.payloads.building');
        }

        apiRequest('POST', `${API_BASE}/payloads/build`, {
            listener_id: listenerId,
            os: os,
            arch: arch,
            host: host
        }).then(data => {
            if (btn) {
                btn.disabled = false;
                btn.textContent = c2t('c2.payloads.buildBeaconBtn');
            }
            if (data.error) {
                showToast(data.error, 'error');
            } else {
                showToast(c2t('c2.payloads.toastBuildSuccess', { bytes: data.payload?.size_bytes }), 'success');
                const result = document.getElementById('c2-build-result');
                if (result) {
                    result.innerHTML = `
                        <div class="c2-build-success">
                            <div>✓ ${escapeHtml(c2t('c2.payloads.buildSuccessTitle'))}</div>
                            <div>${escapeHtml(c2t('c2.payloads.buildMetaOsArch', { os: data.payload?.os, arch: data.payload?.arch }))}</div>
                            <div>${escapeHtml(c2t('c2.payloads.buildSize', { bytes: data.payload?.size_bytes }))}</div>
                            <button type="button" data-c2-action="payload-download" data-c2-id="${escapeAttr(data.payload?.download_path?.split('/').pop() || '')}"
                               class="btn-primary" style="margin-top:8px;display:inline-block;cursor:pointer;">${escapeHtml(c2t('c2.payloads.download'))}</button>
                        </div>
                    `;
                }
            }
        }).catch(err => {
            if (btn) {
                btn.disabled = false;
                btn.textContent = c2t('c2.payloads.buildBeaconBtn');
            }
            showToast(c2t('c2.payloads.toastBuildFail', { msg: err.message || '' }), 'error');
        });
    };

    // ============================================================================
    // 事件审计
    // ============================================================================

    function eventLevelLabel(level) {
        const key = 'c2.events.' + (level || 'info');
        const translated = c2t(key);
        return translated !== key ? translated : (level || 'info');
    }

    function eventCategoryLabel(category) {
        const key = 'c2.events.' + (category || '');
        const translated = c2t(key);
        return translated !== key ? translated : (category || '-');
    }

    function eventLevelBadgeClass(level) {
        if (level === 'warn') return 'c2-event-level-badge--warn';
        if (level === 'critical') return 'c2-event-level-badge--critical';
        return 'c2-event-level-badge--info';
    }

    function eventCategoryBadgeClass(category) {
        return 'c2-event-category-badge c2-event-category-badge--' + (category || 'default');
    }

    function readEventsFilterFromDom() {
        const levelEl = document.getElementById('c2-events-filter-level');
        const catEl = document.getElementById('c2-events-filter-category');
        const sessionEl = document.getElementById('c2-events-filter-session');
        C2.eventsFilter = C2.eventsFilter || { level: '', category: '', session_id: '', since: '', timePreset: 'all' };
        C2.eventsFilter.level = levelEl ? levelEl.value.trim() : '';
        C2.eventsFilter.category = catEl ? catEl.value.trim() : '';
        C2.eventsFilter.session_id = sessionEl ? sessionEl.value.trim() : '';
    }

    function buildEventsQueryParams(page, pageSize) {
        const params = new URLSearchParams();
        params.set('page', String(page));
        params.set('page_size', String(pageSize));
        const f = C2.eventsFilter || {};
        if (f.level) params.set('level', f.level);
        if (f.category) params.set('category', f.category);
        if (f.session_id) params.set('session_id', f.session_id);
        if (f.since) params.set('since', f.since);
        return params.toString();
    }

    function eventsTimePresetToSince(preset) {
        if (!preset || preset === 'all') return '';
        const now = Date.now();
        const map = { '15m': 15 * 60 * 1000, '1h': 60 * 60 * 1000, '24h': 24 * 60 * 60 * 1000, '7d': 7 * 24 * 60 * 60 * 1000 };
        const ms = map[preset];
        if (!ms) return '';
        return new Date(now - ms).toISOString();
    }

    function syncEventsTimePresetButtons() {
        const wrap = document.getElementById('c2-events-time-presets');
        if (!wrap) return;
        const active = (C2.eventsFilter && C2.eventsFilter.timePreset) || 'all';
        wrap.querySelectorAll('.c2-events-time-preset-btn').forEach(btn => {
            btn.classList.toggle('is-active', btn.getAttribute('data-preset') === active);
        });
    }

    function bindEventsFilterUiOnce() {
        if (document.documentElement.dataset.c2EventsFilterBound === '1') return;
        document.documentElement.dataset.c2EventsFilterBound = '1';

        const presets = document.getElementById('c2-events-time-presets');
        if (presets) {
            presets.addEventListener('click', (e) => {
                const btn = e.target.closest('.c2-events-time-preset-btn');
                if (!btn) return;
                const preset = btn.getAttribute('data-preset') || 'all';
                C2.eventsFilter = C2.eventsFilter || { level: '', category: '', session_id: '', since: '', timePreset: 'all' };
                C2.eventsFilter.timePreset = preset;
                C2.eventsFilter.since = eventsTimePresetToSince(preset);
                syncEventsTimePresetButtons();
                C2.loadEvents(1);
            });
        }

        const sessionInput = document.getElementById('c2-events-filter-session');
        if (sessionInput) {
            sessionInput.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') C2.applyEventsFilters();
            });
        }
    }

    C2.applyEventsFilters = function() {
        readEventsFilterFromDom();
        C2.loadEvents(1);
    };

    C2.resetEventsFilters = function() {
        C2.eventsFilter = { level: '', category: '', session_id: '', since: '', timePreset: 'all' };
        const levelEl = document.getElementById('c2-events-filter-level');
        const catEl = document.getElementById('c2-events-filter-category');
        const sessionEl = document.getElementById('c2-events-filter-session');
        if (levelEl) levelEl.value = '';
        if (catEl) catEl.value = '';
        if (sessionEl) sessionEl.value = '';
        syncEventsTimePresetButtons();
        C2.loadEvents(1);
    };

    C2.renderEventsSummary = function() {
        const summary = document.getElementById('c2-events-summary');
        if (!summary) return;
        const total = C2.eventsTotal || 0;
        const counts = C2.eventsLevelCounts || { info: 0, warn: 0, critical: 0 };
        const set = (id, val) => {
            const el = document.getElementById(id);
            if (el) el.textContent = String(val);
        };
        set('c2-events-stat-total', total);
        set('c2-events-stat-info', counts.info || 0);
        set('c2-events-stat-warn', counts.warn || 0);
        set('c2-events-stat-critical', counts.critical || 0);
        summary.hidden = total === 0;
    };

    C2.viewEvent = function(id) {
        const event = (C2.events || []).find(e => e.id === id);
        if (!event) return;
        const modal = document.getElementById('c2-modal');
        const content = document.getElementById('c2-modal-content');
        if (!content || !modal) return;

        const modalBox = modal.querySelector('.c2-modal');
        if (modalBox) modalBox.classList.add('c2-modal--wide');

        const hasData = event.data && typeof event.data === 'object' && Object.keys(event.data).length > 0;
        const levelCls = eventLevelBadgeClass(event.level);
        const catCls = eventCategoryBadgeClass(event.category);

        content.innerHTML = `
            <div class="c2-modal-header c2-event-modal-header">
                <div class="c2-event-modal-heading">
                    <h3>${escapeHtml(c2t('c2.events.detailTitle'))}</h3>
                    <span class="c2-event-level-badge ${levelCls}">${escapeHtml(eventLevelLabel(event.level))}</span>
                    <span class="${catCls}">${escapeHtml(eventCategoryLabel(event.category))}</span>
                </div>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <div class="c2-event-detail">
                    <div class="c2-event-detail-grid">
                        <div class="c2-event-kv">
                            <span class="c2-event-kv__label">${escapeHtml(c2t('c2.events.time'))}</span>
                            <span class="c2-event-kv__value">${escapeHtml(formatTime(event.createdAt))}</span>
                        </div>
                        <div class="c2-event-kv">
                            <span class="c2-event-kv__label">${escapeHtml(c2t('c2.events.colSession'))}</span>
                            <span class="c2-event-kv__value c2-event-kv__value--mono">${escapeHtml(event.sessionId || '-')}</span>
                        </div>
                        <div class="c2-event-kv">
                            <span class="c2-event-kv__label">${escapeHtml(c2t('c2.events.colTask'))}</span>
                            <span class="c2-event-kv__value c2-event-kv__value--mono">${escapeHtml(event.taskId || '-')}</span>
                        </div>
                        <div class="c2-event-kv">
                            <span class="c2-event-kv__label">${escapeHtml(c2t('c2.events.colId'))}</span>
                            <span class="c2-event-kv__value c2-event-kv__value--mono">${escapeHtml(event.id || '-')}</span>
                        </div>
                    </div>
                    <div class="c2-event-message-block">
                        <div class="c2-event-message-block__label">${escapeHtml(c2t('c2.events.message'))}</div>
                        <div class="c2-event-message-block__body">${escapeHtml(event.message || '-')}</div>
                    </div>
                    ${hasData ? `
                        <div class="c2-event-data-block">
                            <div class="c2-event-data-block__header">
                                <span>${escapeHtml(c2t('c2.events.detailData'))}</span>
                                <button type="button" class="btn-ghost btn-sm" onclick="C2.copyTaskBlock('c2-event-data-pre')">${escapeHtml(c2t('common.copy'))}</button>
                            </div>
                            <pre class="c2-event-data-pre" id="c2-event-data-pre">${escapeHtml(JSON.stringify(event.data, null, 2))}</pre>
                        </div>
                    ` : ''}
                </div>
            </div>
            <div class="c2-modal-footer">
                <button class="btn-secondary" onclick="C2.closeModal()">${escapeHtml(c2t('common.close'))}</button>
            </div>
        `;
        openAppModal('c2-modal');
        if (typeof applyTranslations === 'function') applyTranslations(content);
    };

    C2.loadEvents = function(page) {
        bindEventsFilterUiOnce();
        readEventsFilterFromDom();
        const p = page != null ? page : (C2.eventsPage || 1);
        C2.eventsPage = p;
        const ps = C2.eventsPageSize || 10;
        const qs = buildEventsQueryParams(p, ps);
        apiRequest('GET', `${API_BASE}/events?${qs}`).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            C2.events = data.events || [];
            C2.eventsTotal = typeof data.total === 'number' ? data.total : (C2.events.length || 0);
            C2.eventsLevelCounts = data.level_counts || { info: 0, warn: 0, critical: 0 };
            const maxPage = Math.max(1, Math.ceil(C2.eventsTotal / ps));
            if (p > maxPage) {
                C2.loadEvents(maxPage);
                return;
            }
            C2.renderEvents();
            C2.renderEventsSummary();
            C2.renderEventsPagination();
            C2.syncEventsToolbar();
            syncEventsTimePresetButtons();
        }).catch(err => {
            showToast(err.message || String(err), 'error');
        });
    };

    C2.goEventsPage = function(targetPage) {
        const totalPages = Math.max(1, Math.ceil((C2.eventsTotal || 0) / (C2.eventsPageSize || 10)));
        if (targetPage < 1 || targetPage > totalPages) return;
        C2.loadEvents(targetPage);
        const list = document.getElementById('c2-event-list');
        if (list) list.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };

    C2.changeEventsPageSize = function() {
        const sel = document.getElementById('c2-events-page-size-pagination');
        if (!sel) return;
        const n = parseInt(sel.value, 10);
        if (n > 0) {
            C2.eventsPageSize = n;
            C2.loadEvents(1);
        }
    };

    C2.renderEventsPagination = function() {
        const paginationContainer = document.getElementById('c2-events-pagination');
        if (!paginationContainer) return;

        const total = C2.eventsTotal || 0;
        const currentPage = C2.eventsPage || 1;
        const pageSize = C2.eventsPageSize || 10;
        const totalPages = Math.max(1, Math.ceil(total / pageSize));

        if (total === 0) {
            paginationContainer.innerHTML = '';
            if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
            return;
        }

        const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
        const end = Math.min(currentPage * pageSize, total);

        let html = '<div class="monitor-pagination">';
        html += `
            <div class="pagination-info">
                <span>${escapeHtml(c2t('c2.events.paginationShow', { start, end, total }))}</span>
                <label class="pagination-page-size">
                    ${escapeHtml(c2t('c2.events.paginationPerPage'))}
                    <select id="c2-events-page-size-pagination" onchange="C2.changeEventsPageSize()">
                        <option value="10" ${pageSize === 10 ? 'selected' : ''}>10</option>
                        <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                        <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                        <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                    </select>
                </label>
            </div>
            <div class="pagination-controls">
                <button type="button" class="btn-secondary" onclick="C2.goEventsPage(1)" ${currentPage === 1 ? 'disabled' : ''}>${escapeHtml(c2t('c2.events.paginationFirst'))}</button>
                <button type="button" class="btn-secondary" onclick="C2.goEventsPage(${currentPage - 1})" ${currentPage === 1 ? 'disabled' : ''}>${escapeHtml(c2t('c2.events.paginationPrev'))}</button>
                <span class="pagination-page">${escapeHtml(c2t('c2.events.paginationPage', { current: currentPage, total: totalPages }))}</span>
                <button type="button" class="btn-secondary" onclick="C2.goEventsPage(${currentPage + 1})" ${currentPage >= totalPages ? 'disabled' : ''}>${escapeHtml(c2t('c2.events.paginationNext'))}</button>
                <button type="button" class="btn-secondary" onclick="C2.goEventsPage(${totalPages})" ${currentPage >= totalPages ? 'disabled' : ''}>${escapeHtml(c2t('c2.events.paginationLast'))}</button>
            </div>
        `;
        html += '</div>';
        paginationContainer.innerHTML = html;
        if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
        if (typeof applyTranslations === 'function') applyTranslations(paginationContainer);
    };

    C2.collectCheckedEventIds = function() {
        return Array.from(document.querySelectorAll('.c2-event-check:checked')).map(cb => cb.getAttribute('data-id')).filter(Boolean);
    };

    C2.syncEventsToolbar = function() {
        const batchBtn = document.getElementById('c2-events-batch-delete');
        const ids = C2.collectCheckedEventIds();
        if (batchBtn) batchBtn.disabled = ids.length === 0;

        const all = document.querySelectorAll('.c2-event-check');
        const selAll = document.getElementById('c2-events-select-all');
        if (selAll && all.length) {
            const nChecked = document.querySelectorAll('.c2-event-check:checked').length;
            selAll.checked = nChecked === all.length;
            selAll.indeterminate = nChecked > 0 && nChecked < all.length;
        } else if (selAll) {
            selAll.checked = false;
            selAll.indeterminate = false;
        }
    };

    C2.onEventsSelectAll = function(checked) {
        document.querySelectorAll('.c2-event-check').forEach(cb => { cb.checked = checked; });
        C2.syncEventsToolbar();
    };

    C2.deleteEventById = function(id) {
        if (!id) return;
        if (!confirm(c2t('c2.events.confirmDeleteOne'))) return;
        apiRequest('DELETE', `${API_BASE}/events`, { ids: [id] }).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            showToast(c2t('c2.events.toastDeleted', { n: data.deleted != null ? data.deleted : 1 }), 'success');
            C2.loadEvents(C2.eventsPage || 1);
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    C2.deleteSelectedEvents = function() {
        const ids = C2.collectCheckedEventIds();
        if (!ids.length) {
            showToast(c2t('c2.events.toastSelectFirst'), 'warn');
            return;
        }
        if (!confirm(c2t('c2.events.confirmBatchDelete', { n: ids.length }))) return;
        apiRequest('DELETE', `${API_BASE}/events`, { ids }).then(data => {
            if (data.error) {
                showToast(String(data.error), 'error');
                return;
            }
            const deleted = data.deleted != null ? data.deleted : ids.length;
            showToast(c2t('c2.events.toastDeleted', { n: deleted }), 'success');
            C2.loadEvents(C2.eventsPage || 1);
        }).catch(err => showToast(err.message || String(err), 'error'));
    };

    C2.renderEvents = function() {
        const container = document.getElementById('c2-event-list');
        if (!container) return;

        const selAll = document.getElementById('c2-events-select-all');
        if (selAll) {
            selAll.checked = false;
            selAll.indeterminate = false;
        }

        if (C2.events.length === 0) {
            container.innerHTML = '<div class="c2-events-empty">' + escapeHtml(c2t('c2.events.empty')) + '</div>';
            if (selAll) selAll.disabled = true;
            C2.syncEventsToolbar();
            return;
        }
        if (selAll) selAll.disabled = false;

        const delTitle = escapeHtml(c2t('c2.events.deleteOne'));
        const dash = '<span class="c2-events-cell-muted">—</span>';
        const deleteIcon = '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/></svg>';

        container.innerHTML = `
            <table class="c2-events-table">
                <thead>
                    <tr>
                        <th class="c2-events-table-col-check">
                            <label class="c2-event-check-label" title="${escapeAttr(c2t('c2.events.selectAll'))}">
                                <input type="checkbox" id="c2-events-select-all" onchange="C2.onEventsSelectAll(this.checked)">
                            </label>
                        </th>
                        <th>${escapeHtml(c2t('c2.events.time'))}</th>
                        <th>${escapeHtml(c2t('c2.events.level'))}</th>
                        <th>${escapeHtml(c2t('c2.events.category'))}</th>
                        <th>${escapeHtml(c2t('c2.events.message'))}</th>
                        <th>${escapeHtml(c2t('c2.events.colSession'))}</th>
                        <th>${escapeHtml(c2t('c2.events.colTask'))}</th>
                        <th class="c2-events-table-col-actions">${escapeHtml(c2t('common.actions'))}</th>
                    </tr>
                </thead>
                <tbody>
	                    ${C2.events.map(e => {
	                        const rawId = e.id || '';
	                        const eid = escapeHtml(rawId);
	                        const eidAttr = escapeAttr(rawId);
                        const levelCls = eventLevelBadgeClass(e.level);
                        const catCls = eventCategoryBadgeClass(e.category);
                        const sessionShort = e.sessionId ? escapeHtml(String(e.sessionId).substring(0, 10)) + (String(e.sessionId).length > 10 ? '\u2026' : '') : '';
                        const taskShort = e.taskId ? escapeHtml(String(e.taskId).substring(0, 10)) + (String(e.taskId).length > 10 ? '\u2026' : '') : '';
                        const msg = escapeHtml(e.message || '');
                        const rowLevel = escapeHtml(e.level || 'info');
                        return `
	                        <tr class="c2-events-row c2-events-row--${rowLevel}" data-event-id="${eidAttr}" data-c2-action="event-view" data-c2-id="${eidAttr}" role="button" tabindex="0">
                            <td class="c2-events-table-col-check" data-c2-stop-action="1">
                                <label class="c2-event-check-label">
	                                    <input type="checkbox" class="c2-event-check" data-id="${eidAttr}" onchange="C2.syncEventsToolbar()">
                                </label>
                            </td>
                            <td class="c2-events-col-time">${escapeHtml(formatTime(e.createdAt))}</td>
                            <td><span class="c2-event-level-badge ${levelCls}">${escapeHtml(eventLevelLabel(e.level))}</span></td>
                            <td><span class="${catCls}">${escapeHtml(eventCategoryLabel(e.category))}</span></td>
	                            <td class="c2-events-col-message" title="${escapeAttr(e.message || '')}">${msg || dash}</td>
	                            <td class="c2-events-col-mono" title="${escapeAttr(e.sessionId || '')}">${sessionShort || dash}</td>
	                            <td class="c2-events-col-mono" title="${escapeAttr(e.taskId || '')}">${taskShort || dash}</td>
                            <td class="c2-events-table-col-actions" data-c2-stop-action="1">
	                                <button type="button" class="c2-events-delete-btn" data-require-permission="c2:delete" data-c2-action="event-delete" data-c2-id="${eidAttr}" title="${delTitle}" aria-label="${delTitle}">${deleteIcon}</button>
                            </td>
                        </tr>`;
                    }).join('')}
                </tbody>
            </table>
        `;

        C2.syncEventsToolbar();
        if (typeof applyTranslations === 'function') applyTranslations(container);
        if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
    };

    C2.connectEventStream = function() {
        if (C2.eventSource) C2.eventSource.close();

        let streamUrl = `${API_BASE}/events/stream`;
        if (typeof authToken !== 'undefined' && authToken) {
            streamUrl += `?token=${encodeURIComponent(authToken)}`;
        }
        C2.eventSource = new EventSource(streamUrl);
        C2.eventSource.onmessage = (e) => {
            try {
                const event = JSON.parse(e.data);
                C2.onEvent(event);
            } catch (err) {}
        };
        C2.eventSource.onerror = () => {
            setTimeout(() => C2.connectEventStream(), 5000);
        };
    };

    C2.onEvent = function(event) {
        if (!event) return;

        if (window.currentPageId === 'c2-events' && (C2.eventsPage || 1) === 1) {
            C2.loadEvents(1);
        }

        const msg = String(event.message || '');
        const sid = event.sessionId || (event.data && event.data.session_id) || '';
        const sessionOnline = event.category === 'session' && event.level === 'critical' && (
            msg.includes('上线') || msg.includes('新会话') || /new session/i.test(msg)
        );
        const sessionOffline = event.category === 'session' && (
            msg.includes('离线') || /offline/i.test(msg)
        );

        if (sessionOnline || sessionOffline) {
            const prevCount = (C2.sessions || []).length;
            const prevSelected = C2.selectedSessionId;
            const refresh = C2.loadSessions();
            const afterRefresh = function () {
                if (sessionOnline && sid && window.currentPageId === 'c2-sessions') {
                    if (prevCount === 0 || !prevSelected) {
                        C2.selectSession(sid);
                    }
                }
            };
            if (refresh && typeof refresh.then === 'function') {
                refresh.then(afterRefresh).catch(function () {});
            } else {
                afterRefresh();
            }
            if (typeof refreshDashboard === 'function') {
                try { refreshDashboard(); } catch (e) {}
            }
        }

        if (sessionOnline) {
            showToast(`[${event.category}] ${msg}`, 'info');
        } else if (event.level === 'critical') {
            showToast(`[${event.category}] ${msg}`, 'error');
        }

        if (event.category === 'task') {
            const taskSid = sid;
            if (window.currentPageId === 'c2-tasks') {
                const page = msg.includes('入队') ? 1 : (C2.tasksPage || 1);
                C2.loadTasks(page);
            }
            if (window.currentPageId === 'c2-sessions' && taskSid && C2.selectedSessionId === taskSid) {
                C2.loadSessionTasks(taskSid);
            }
            if (typeof refreshDashboard === 'function') {
                try { refreshDashboard(); } catch (e) {}
            }
        }
    };

    // ============================================================================
    // Profile 管理
    // ============================================================================

    C2.loadProfiles = function() {
        apiRequest('GET', `${API_BASE}/profiles`).then(data => {
            C2.profiles = data.profiles || [];
            C2.renderProfiles();
        });
    };

    C2.renderProfiles = function() {
        const container = document.getElementById('c2-profile-list');
        if (!container) return;

        if (C2.profiles.length === 0) {
            container.innerHTML = '<div class="c2-empty">' + escapeHtml(c2t('c2.profiles.empty')) + '</div>';
            return;
        }

        const defVal = c2t('c2.profiles.defaultValue');
        container.innerHTML = C2.profiles.map(p => `
            <div class="c2-profile-card">
                <div class="c2-profile-header">
                    <h4>${escapeHtml(p.name)}</h4>
                    <button class="btn-danger btn-sm" data-require-permission="c2:delete" data-c2-action="profile-delete" data-c2-id="${escapeAttr(p.id)}">${escapeHtml(c2t('common.delete'))}</button>
                </div>
                <div class="c2-profile-info">
                    <div><strong>UA:</strong> ${escapeHtml(p.userAgent || defVal)}</div>
                    <div><strong>URIs:</strong> ${escapeHtml((p.uris || []).join(', ') || defVal)}</div>
                    <div><strong>Jitter:</strong> ${p.jitterMinMs || 0}ms – ${p.jitterMaxMs || 0}ms</div>
                </div>
            </div>
        `).join('');
        if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
    };

    C2.showCreateProfileModal = function() {
        const modal = document.getElementById('c2-modal');
        const content = document.getElementById('c2-modal-content');
        if (!content) return;

        content.innerHTML = `
            <div class="c2-modal-header">
                <h3>${escapeHtml(c2t('c2.profiles.modalCreateTitle'))}</h3>
                <button class="c2-modal-close" onclick="C2.closeModal()">&times;</button>
            </div>
            <div class="c2-modal-body">
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.profiles.profileNameLabel'))}</label>
                    <input type="text" id="c2-profile-name" class="form-control" placeholder="${escapeAttr(c2t('c2.profiles.placeholderProfileName'))}">
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.profiles.userAgent'))}</label>
                    <input type="text" id="c2-profile-ua" class="form-control" placeholder="Mozilla/5.0 (Windows NT 10.0; Win64; x64) ...">
                    <div class="form-hint">${escapeHtml(c2t('c2.profiles.hintUa'))}</div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.profiles.labelBeaconUris'))}</label>
                    <textarea id="c2-profile-uris" class="form-control" rows="3" placeholder="/api/v1/status&#10;/cdn/health&#10;/assets/check">/api/v1/status</textarea>
                    <div class="form-hint">${escapeHtml(c2t('c2.profiles.hintUris'))}</div>
                </div>
                <div class="c2-form-row">
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.profiles.labelJitterMin'))}</label>
                        <input type="number" id="c2-profile-jmin" class="form-control" value="100" min="0">
                    </div>
                    <div class="c2-form-group">
                        <label>${escapeHtml(c2t('c2.profiles.labelJitterMax'))}</label>
                        <input type="number" id="c2-profile-jmax" class="form-control" value="500" min="0">
                    </div>
                </div>
                <div class="c2-form-group">
                    <label>${escapeHtml(c2t('c2.profiles.labelRespHeaders'))}</label>
                    <textarea id="c2-profile-headers" class="form-control" rows="3" placeholder='{"Server":"nginx","X-Powered-By":"ASP.NET"}'>{"Server":"nginx"}</textarea>
                    <div class="form-hint">${escapeHtml(c2t('c2.profiles.hintHeaders'))}</div>
                </div>
            </div>
            <div class="c2-modal-footer">
                <button class="btn-secondary" onclick="C2.closeModal()">${escapeHtml(c2t('common.cancel'))}</button>
                <button class="btn-primary" onclick="C2.createProfile()">${escapeHtml(c2t('c2.profiles.submitCreate'))}</button>
            </div>
        `;
        openAppModal(modal);
    };

    C2.createProfile = function() {
        const name = document.getElementById('c2-profile-name')?.value.trim();
        if (!name) {
            showToast(c2t('c2.profiles.toastNameRequired'), 'error');
            return;
        }

        const userAgent = document.getElementById('c2-profile-ua')?.value.trim() || '';
        const urisRaw = document.getElementById('c2-profile-uris')?.value.trim() || '';
        const uris = urisRaw.split('\n').map(u => u.trim()).filter(u => u);
        const jitterMinMs = Number(document.getElementById('c2-profile-jmin')?.value);
        const jitterMaxMs = Number(document.getElementById('c2-profile-jmax')?.value);
        if (!Number.isSafeInteger(jitterMinMs) || !Number.isSafeInteger(jitterMaxMs) || jitterMinMs < 0 || jitterMaxMs < jitterMinMs) {
            showToast(c2t('c2.profiles.toastInvalidJitter'), 'error');
            return;
        }

        let responseHeaders = {};
        const headersRaw = document.getElementById('c2-profile-headers')?.value.trim();
        if (headersRaw) {
            try { responseHeaders = JSON.parse(headersRaw); }
            catch (e) { showToast(c2t('c2.profiles.toastInvalidHeadersJson'), 'error'); return; }
        }

        apiRequest('POST', `${API_BASE}/profiles`, {
            name,
            userAgent,
            uris,
            jitterMinMs,
            jitterMaxMs,
            responseHeaders
        }).then(data => {
            if (data.error) {
                showToast(data.error, 'error');
            } else {
                showToast(c2t('c2.profiles.toastCreated'), 'success');
                C2.closeModal();
                C2.loadProfiles();
            }
        });
    };

    C2.deleteProfile = function(id) {
        if (!confirm(c2t('c2.profiles.confirmDelete'))) return;
        apiRequest('DELETE', `${API_BASE}/profiles/${id}`, {}).then(data => {
            showToast(c2t('c2.profiles.toastDeleted'), 'success');
            C2.loadProfiles();
        });
    };

    // ============================================================================
    // 模态框
    // ============================================================================

    C2.copyTaskBlock = function(elementId) {
        const el = document.getElementById(elementId);
        if (el && el.textContent) copyToClipboard(el.textContent);
    };

    C2.closeModal = function() {
        closeAllC2FormSelects();
        const modal = document.getElementById('c2-modal');
        if (modal) {
            const modalBox = modal.querySelector('.c2-modal');
            if (modalBox) {
                modalBox.classList.remove('c2-modal--wide');
                modalBox.classList.remove('c2-modal--sleep');
            }
        }
        C2._sleepModalSessionId = null;
        closeAppModal('c2-modal');
    };

    // ============================================================================
    // 暴露到全局
    // ============================================================================

    window.C2 = C2;

    // 页面切换监听
    window.addEventListener('pageChanged', function(e) {
        if (e.detail?.pageId?.startsWith('c2')) {
            C2.init();
        }
    });

    // DOM 加载完成后初始化
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', () => {
            if (window.currentPageId?.startsWith('c2')) C2.init();
        });
    } else {
        if (window.currentPageId?.startsWith('c2')) C2.init();
    }

    document.addEventListener('languagechange', function () {
        try {
            if (!window.currentPageId || !String(window.currentPageId).startsWith('c2')) return;
            if (typeof applyTranslations === 'function') applyTranslations(document);
            C2.init();
            if (isAppModalOpen('c2-modal')) {
                C2.refreshFormSelects();
            }
            if (C2.selectedSessionId && (window.currentPageId === 'c2-sessions')) {
                C2.renderSessions();
                C2.renderSessionDetail(C2.selectedSessionId);
            }
        } catch (e) {
            console.warn('languagechange C2 refresh failed', e);
        }
    });

})();
