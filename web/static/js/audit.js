/**
 * 系统设置 - 平台操作审计日志
 */
let auditLogsPage = 1;
let auditLogsPageSize = 20;
let auditLogsTotal = 0;
let auditLogsCache = [];

const AUDIT_PAGE_SIZE_KEY = 'cyberstrike_audit_page_size';

/** 按类别列出的操作（用于 datalist 提示，避免超长下拉） */
const AUDIT_ACTIONS_BY_CATEGORY = {
    auth: ['login', 'logout', 'change_password'],
    config: ['apply', 'update'],
    c2: ['listener_create', 'listener_delete', 'listener_start', 'listener_stop',
        'session_delete', 'task_create', 'task_cancel', 'task_delete'],
    webshell: ['connection_create', 'connection_delete'],
    knowledge: ['item_delete', 'index_build', 'index_rebuild_full'],
    conversation: ['create', 'delete', 'delete_turn'],
    vulnerability: ['create', 'update', 'delete'],
    external_mcp: ['upsert', 'delete'],
    task: ['create_queue', 'start_queue', 'delete_queue', 'pause_queue', 'rerun_queue', 'delete_batch_task'],
    tool: ['execution_delete', 'execution_delete_batch'],
    file: ['upload', 'delete'],
    hitl: ['decision', 'audit_strategy_update'],
    role: ['create', 'update', 'delete'],
    skill: ['create', 'update', 'delete'],
    agent: ['markdown_create', 'markdown_update', 'markdown_delete']
};

function auditT(key, opts, fallback) {
    if (typeof t === 'function') {
        const v = t(key, opts);
        if (v && v !== key) return v;
    }
    return fallback != null ? fallback : key;
}

function auditCategoryI18nKey(category) {
    if (!category) return '';
    if (category === 'external_mcp') return 'externalMcp';
    return category;
}

function auditCategoryLabel(category) {
    if (!category) return '';
    const key = 'settingsAudit.cat.' + auditCategoryI18nKey(category);
    return auditT(key, null, category);
}

function auditActionLabel(action) {
    if (!action) return '';
    return auditT('settingsAudit.act.' + action, null, action);
}

/** Stored DB messages that share category+action but need distinct i18n keys. */
const AUDIT_MSG_BY_STORED_TEXT = {
    '登录失败：密码错误': 'settingsAudit.msg.auth.login_failed',
    '修改密码失败：当前密码不正确': 'settingsAudit.msg.auth.change_password_failed',
    '应用配置失败：初始化知识库': 'settingsAudit.msg.config.apply_fail_kb_init',
    '应用配置失败：重新初始化知识库': 'settingsAudit.msg.config.apply_fail_kb_reinit',
    '应用配置失败：C2': 'settingsAudit.msg.config.apply_fail_c2'
};

function auditMessageLabel(log) {
    if (!log) return '';
    const raw = (log.message || '').trim();
    if (raw && AUDIT_MSG_BY_STORED_TEXT[raw]) {
        return auditT(AUDIT_MSG_BY_STORED_TEXT[raw], null, raw);
    }
    const cat = (log.category || '').trim();
    const act = (log.action || '').trim();
    const res = (log.result || '').trim();
    if (cat && act) {
        if (cat === 'auth' && act === 'login' && res === 'failure') {
            return auditT('settingsAudit.msg.auth.login_failed', null, raw);
        }
        if (cat === 'auth' && act === 'change_password' && res === 'failure') {
            return auditT('settingsAudit.msg.auth.change_password_failed', null, raw);
        }
        const key = 'settingsAudit.msg.' + cat + '.' + act;
        const translated = auditT(key, null, null);
        if (translated && translated !== key) return translated;
    }
    return raw;
}

function auditResultLabel(result) {
    if (!result) return '';
    return auditT('settingsAudit.result.' + result, null, result);
}

function auditLocale() {
    if (typeof window.uiLocale === 'function') return window.uiLocale();
    if (typeof window.__locale === 'string' && window.__locale.length) {
        if (window.__locale.startsWith('zh')) return 'zh-CN';
        if (window.__locale.startsWith('ru')) return 'ru-RU';
        return 'en-US';
    }
    return (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en-US';
}

function auditTimezoneShortLabel() {
    try {
        const parts = new Intl.DateTimeFormat(auditLocale(), { timeZoneName: 'short' }).formatToParts(new Date());
        const tz = parts.find(function (p) { return p.type === 'timeZoneName'; });
        return tz ? tz.value : '';
    } catch (_) {
        return '';
    }
}

function formatAuditTime(iso) {
    if (!iso) return '';
    try {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return iso;
        return d.toLocaleString(auditLocale(), {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false,
            timeZoneName: 'short'
        });
    } catch (_) {
        return iso;
    }
}

/** Read stored local datetime (YYYY-MM-DDTHH:mm) from custom picker or raw input. */
function getAuditFilterDatetimeValue(inputId) {
    if (typeof window.AuditDatetimePicker !== 'undefined' && typeof window.AuditDatetimePicker.getValue === 'function') {
        return window.AuditDatetimePicker.getValue(inputId) || '';
    }
    var el = document.getElementById(inputId);
    return el ? (el.value || '') : '';
}

/** datetime-local / picker storage -> UTC RFC3339 for API. */
function auditDatetimeLocalToRFC3339(value) {
    if (!value || !value.trim()) return '';
    const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value.trim());
    if (!m) return '';
    const d = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], 0, 0);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString();
}

function updateAuditTimezoneHint() {
    const el = document.getElementById('audit-filter-timezone-hint');
    if (!el) return;
    const tz = auditTimezoneShortLabel();
    if (!tz) {
        el.hidden = true;
        el.textContent = '';
        return;
    }
    el.hidden = false;
    el.textContent = auditT('settingsAudit.filterTimeZone', { tz: tz },
        '时区：' + tz + '（筛选按浏览器本地时间，API 使用 UTC）');
}

function initAuditPageSizeFromStorage() {
    try {
        const saved = parseInt(localStorage.getItem(AUDIT_PAGE_SIZE_KEY), 10);
        if ([10, 20, 50, 100].indexOf(saved) >= 0) {
            auditLogsPageSize = saved;
        }
    } catch (_) { /* ignore */ }
    const sel = document.getElementById('audit-page-size');
    if (sel) sel.value = String(auditLogsPageSize);
}

function onAuditPageSizeChange() {
    const sel = document.getElementById('audit-page-size');
    if (!sel) return;
    const n = parseInt(sel.value, 10);
    if ([10, 20, 50, 100].indexOf(n) < 0) return;
    auditLogsPageSize = n;
    try {
        localStorage.setItem(AUDIT_PAGE_SIZE_KEY, String(n));
    } catch (_) { /* ignore */ }
    auditLogsPage = 1;
    loadAuditLogs(1);
}

function rebuildAuditActionSelect() {
    const catEl = document.getElementById('audit-filter-category');
    const actEl = document.getElementById('audit-filter-action');
    if (!actEl) return;

    const category = catEl ? catEl.value : '';
    const prev = actEl.value;
    const allLabel = auditT('settingsAudit.filterAllActions', null, '全部操作');
    const hint = auditT('settingsAudit.filterCascadeHint', null, '选择类别后可筛选具体操作');
    actEl.innerHTML = '';
    const allOpt = document.createElement('option');
    allOpt.value = '';
    allOpt.textContent = allLabel;
    actEl.appendChild(allOpt);

    if (!category) {
        actEl.disabled = true;
        actEl.value = '';
        actEl.title = hint;
        syncAuditCustomSelect('audit-filter-action');
        return;
    }

    actEl.disabled = false;
    actEl.title = '';

    const actions = AUDIT_ACTIONS_BY_CATEGORY[category] || [];
    actions.forEach(function (action) {
        const opt = document.createElement('option');
        opt.value = action;
        opt.textContent = auditActionLabel(action);
        actEl.appendChild(opt);
    });
    if (prev && Array.prototype.some.call(actEl.options, function (o) { return o.value === prev; })) {
        actEl.value = prev;
    }
    syncAuditCustomSelect('audit-filter-action');
}

function onAuditCategoryFilterChange() {
    rebuildAuditActionSelect();
}

function buildAuditQueryParams(forExport) {
    const params = new URLSearchParams();
    if (!forExport) {
        params.set('page', String(auditLogsPage));
        params.set('page_size', String(auditLogsPageSize));
    }
    const cat = document.getElementById('audit-filter-category');
    const act = document.getElementById('audit-filter-action');
    const res = document.getElementById('audit-filter-result');
    const q = document.getElementById('audit-filter-q');
    if (cat && cat.value) params.set('category', cat.value);
    if (act && !act.disabled && act.value) params.set('action', act.value);
    if (res && res.value) params.set('result', res.value);
    if (q && q.value.trim()) params.set('q', q.value.trim());
    const sinceISO = auditDatetimeLocalToRFC3339(getAuditFilterDatetimeValue('audit-filter-since'));
    const untilISO = auditDatetimeLocalToRFC3339(getAuditFilterDatetimeValue('audit-filter-until'));
    if (sinceISO) params.set('since', sinceISO);
    if (untilISO) params.set('until', untilISO);
    return params.toString();
}

async function loadAuditSummary() {
    if (typeof apiFetch !== 'function') return;
    const wrap = document.getElementById('audit-summary-stats');
    try {
        const r = await apiFetch('/api/audit/summary?' + buildAuditQueryParams(true));
        if (!r.ok) return;
        const data = await r.json();
        if (wrap) wrap.hidden = false;
        const elTotal = document.getElementById('audit-stat-total');
        const elSuccess = document.getElementById('audit-stat-success');
        const elFail = document.getElementById('audit-stat-failures');
        const elRecent = document.getElementById('audit-stat-recent');
        const total = data.total != null ? data.total : 0;
        const failures = data.failures != null ? data.failures : 0;
        if (elTotal) elTotal.textContent = String(total);
        if (elSuccess) elSuccess.textContent = String(Math.max(0, total - failures));
        if (elFail) elFail.textContent = String(failures);
        if (elRecent) elRecent.textContent = String(data.recent_7d != null ? data.recent_7d : 0);
    } catch (_) { /* ignore */ }
}

async function loadAuditLogs(page) {
    if (typeof apiFetch !== 'function') return;
    auditLogsPage = page != null ? page : auditLogsPage;
    const listEl = document.getElementById('audit-log-list');
    if (listEl) {
        listEl.innerHTML = '<div class="loading-spinner">' + (typeof escapeHtml === 'function' ? escapeHtml(auditT('settingsAudit.loading', null, '加载中...')) : '加载中...') + '</div>';
    }
    try {
        const qs = buildAuditQueryParams(false);
        const r = await apiFetch('/api/audit/logs?' + qs);
        if (!r.ok) {
            const err = await r.json().catch(function () { return {}; });
            throw new Error(err.error || r.statusText);
        }
        const data = await r.json();
        auditLogsCache = data.logs || [];
        renderAuditLogs(auditLogsCache);
        auditLogsTotal = typeof data.total === 'number' ? data.total : 0;
        const maxPage = Math.max(1, Math.ceil(auditLogsTotal / auditLogsPageSize));
        if (auditLogsPage > maxPage) {
            loadAuditLogs(maxPage);
            return;
        }
        renderAuditLogsPagination();
        loadAuditSummary();
    } catch (e) {
        if (listEl) {
            const msg = typeof escapeHtml === 'function' ? escapeHtml(e.message || String(e)) : (e.message || String(e));
            listEl.innerHTML = '<div class="monitor-empty">' + msg + '</div>';
        }
        if (typeof showToast === 'function') {
            showToast(e.message || String(e), 'error');
        }
    }
}

function auditResultTagClass(result) {
    return result === 'failure' ? 'audit-tag--fail' : 'audit-tag--ok';
}

function renderAuditLogs(logs) {
    const listEl = document.getElementById('audit-log-list');
    if (!listEl) return;
    const esc = typeof escapeHtml === 'function' ? escapeHtml : function (s) { return String(s || ''); };
    if (!logs.length) {
        listEl.innerHTML = '<div class="audit-log-empty">' + esc(auditT('settingsAudit.empty', null, '暂无审计记录')) + '</div>';
        return;
    }
    const dash = '<span class="audit-log-cell-muted">—</span>';
    const head = (
        '<div class="audit-log-table-wrap">' +
        '<table class="audit-log-table">' +
        '<thead><tr>' +
        '<th data-i18n="settingsAudit.colTime">时间</th>' +
        '<th data-i18n="settingsAudit.colMessage">说明</th>' +
        '<th data-i18n="settingsAudit.colCategory">类别</th>' +
        '<th data-i18n="settingsAudit.colAction">操作</th>' +
        '<th data-i18n="settingsAudit.colResult">结果</th>' +
        '<th data-i18n="settingsAudit.colIp">IP</th>' +
        '<th data-i18n="settingsAudit.colResource">资源 ID</th>' +
        '</tr></thead><tbody>'
    );
    const rows = logs.map(function (log) {
        const catLabel = esc(auditCategoryLabel(log.category || ''));
        const actionLabel = esc(auditActionLabel(log.action || ''));
        const msg = esc(auditMessageLabel(log));
        const ip = esc(log.clientIp || '');
        const when = esc(formatAuditTime(log.createdAt));
        const res = esc(auditResultLabel(log.result || ''));
        const rid = log.resourceId ? esc(log.resourceId) : '';
        const eid = esc(log.id || '');
        const resultCls = auditResultTagClass(log.result || '');
        const rowClick = 'onclick="showAuditLogDetail(\'' + eid + '\')" ' +
            'onkeydown="if(event.key===\'Enter\'||event.key===\' \'){event.preventDefault();showAuditLogDetail(\'' + eid + '\')}"';
        return (
            '<tr class="audit-log-row" role="button" tabindex="0" ' + rowClick + '>' +
            '<td class="audit-log-col-time">' + when + '</td>' +
            '<td class="audit-log-col-msg" title="' + msg + '">' + (msg || dash) + '</td>' +
            '<td>' + (catLabel ? '<span class="audit-tag audit-tag--cat">' + catLabel + '</span>' : dash) + '</td>' +
            '<td>' + (actionLabel ? '<span class="audit-tag audit-tag--act">' + actionLabel + '</span>' : dash) + '</td>' +
            '<td>' + (res ? '<span class="audit-tag ' + resultCls + '">' + res + '</span>' : dash) + '</td>' +
            '<td class="audit-log-col-ip">' + (ip || dash) + '</td>' +
            '<td class="audit-log-col-resource" title="' + rid + '">' + (rid || dash) + '</td>' +
            '</tr>'
        );
    }).join('');
    listEl.innerHTML = head + rows + '</tbody></table></div>';
    if (typeof applyTranslations === 'function') {
        applyTranslations(listEl);
    }
}

function renderAuditLogsPagination() {
    const container = document.getElementById('audit-logs-pagination');
    if (!container) return;
    const esc = typeof escapeHtml === 'function' ? escapeHtml : function (s) { return String(s || ''); };
    const total = auditLogsTotal || 0;
    const currentPage = auditLogsPage || 1;
    const pageSize = auditLogsPageSize || 20;
    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(currentPage * pageSize, total);
    const infoText = auditT('mcpMonitor.paginationInfo', { start: start, end: end, total: total },
        '显示 ' + start + '-' + end + ' / 共 ' + total + ' 条记录');
    const perPageLabel = auditT('mcpMonitor.perPageLabel', null, '每页显示');
    const firstPageLabel = auditT('mcp.firstPage', null, '首页');
    const prevPageLabel = auditT('mcp.prevPage', null, '上一页');
    const pageInfoText = auditT('mcp.pageInfo', { page: currentPage, total: totalPages },
        '第 ' + currentPage + ' / ' + totalPages + ' 页');
    const nextPageLabel = auditT('mcp.nextPage', null, '下一页');
    const lastPageLabel = auditT('mcp.lastPage', null, '末页');
    const disabledFirst = currentPage === 1 || total === 0;
    const disabledLast = currentPage >= totalPages || total === 0;
    let html = '<div class="monitor-pagination">';
    html += '<div class="pagination-info">';
    html += '<span>' + esc(infoText) + '</span>';
    html += '<label class="pagination-page-size">' + esc(perPageLabel);
    html += '<select id="audit-page-size" onchange="onAuditPageSizeChange()">';
    [10, 20, 50, 100].forEach(function (n) {
        html += '<option value="' + n + '"' + (pageSize === n ? ' selected' : '') + '>' + n + '</option>';
    });
    html += '</select></label></div>';
    html += '<div class="pagination-controls">';
    html += '<button type="button" class="btn-secondary" onclick="goAuditLogsPage(1)"' + (disabledFirst ? ' disabled' : '') + '>' + esc(firstPageLabel) + '</button>';
    html += '<button type="button" class="btn-secondary" onclick="goAuditLogsPage(' + (currentPage - 1) + ')"' + (disabledFirst ? ' disabled' : '') + '>' + esc(prevPageLabel) + '</button>';
    html += '<span class="pagination-page">' + esc(pageInfoText) + '</span>';
    html += '<button type="button" class="btn-secondary" onclick="goAuditLogsPage(' + (currentPage + 1) + ')"' + (disabledLast ? ' disabled' : '') + '>' + esc(nextPageLabel) + '</button>';
    html += '<button type="button" class="btn-secondary" onclick="goAuditLogsPage(' + totalPages + ')"' + (disabledLast ? ' disabled' : '') + '>' + esc(lastPageLabel) + '</button>';
    html += '</div></div>';
    container.innerHTML = html;
}

function goAuditLogsPage(p) {
    const totalPages = Math.max(1, Math.ceil((auditLogsTotal || 0) / (auditLogsPageSize || 20)));
    if (p < 1 || p > totalPages) return;
    loadAuditLogs(p);
}

function filterAuditLogs() {
    auditLogsPage = 1;
    loadAuditLogs(1);
}

function resetAuditLogFilters() {
    const cat = document.getElementById('audit-filter-category');
    const act = document.getElementById('audit-filter-action');
    const res = document.getElementById('audit-filter-result');
    const q = document.getElementById('audit-filter-q');
    if (cat) cat.value = '';
    if (res) res.value = '';
    if (q) q.value = '';
    if (typeof window.AuditDatetimePicker !== 'undefined' && typeof window.AuditDatetimePicker.clearAll === 'function') {
        window.AuditDatetimePicker.clearAll();
    }
    rebuildAuditActionSelect();
    syncAuditCustomSelect('audit-filter-category');
    syncAuditCustomSelect('audit-filter-result');
    filterAuditLogs();
}

function applyAuditTimePreset(preset) {
    if (typeof window.AuditDatetimePicker === 'undefined') return;
    const now = new Date();
    let since = new Date(now.getTime());
    let until = new Date(now.getTime());
    switch (preset) {
        case '15m':
            since = new Date(now.getTime() - 15 * 60 * 1000);
            break;
        case '1h':
            since = new Date(now.getTime() - 60 * 60 * 1000);
            break;
        case '24h':
            since = new Date(now.getTime() - 24 * 60 * 60 * 1000);
            break;
        case '7d':
            since = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            break;
        case 'today':
            since = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0, 0);
            break;
        default:
            return;
    }
    window.AuditDatetimePicker.setValue('audit-filter-since', since);
    window.AuditDatetimePicker.setValue('audit-filter-until', until);
    filterAuditLogs();
}

function initAuditTimePresets() {
    const wrap = document.getElementById('audit-time-presets');
    if (!wrap || wrap.dataset.bound === '1') return;
    wrap.dataset.bound = '1';
    wrap.addEventListener('click', function (ev) {
        const btn = ev.target.closest('[data-preset]');
        if (!btn) return;
        applyAuditTimePreset(btn.getAttribute('data-preset'));
    });
}

/** 资源已被删除/移除的审计操作，不再提供「打开关联资源」 */
const AUDIT_ACTIONS_RESOURCE_REMOVED = {
    delete: true,
    item_delete: true,
    connection_delete: true,
    listener_delete: true,
    session_delete: true,
    task_delete: true,
    execution_delete: true,
    execution_delete_batch: true,
    delete_queue: true,
    delete_batch_task: true,
    markdown_delete: true
};

function auditResourceWasRemoved(log) {
    if (!log || !log.action) return false;
    return !!AUDIT_ACTIONS_RESOURCE_REMOVED[log.action];
}

/** 删除类操作，或关联资源已不存在（由详情 API resourceAvailable 判定） */
function auditResourceUnavailable(log) {
    if (!log) return false;
    if (auditResourceWasRemoved(log)) return true;
    return log.resourceAvailable === false;
}

function auditResourceMeta(log) {
    if (!log || !log.resourceId) return '';
    const esc = typeof escapeHtml === 'function' ? escapeHtml : function (s) { return String(s || ''); };
    const id = esc(log.resourceId);
    if (auditResourceUnavailable(log)) {
        const idLabel = esc(auditT('settingsAudit.resourceIdLabel', null, '资源 ID'));
        const removed = esc(auditT('settingsAudit.resourceRemoved', null, '（关联对象已删除）'));
        return '<p class="audit-resource-meta"><strong>' + idLabel + ':</strong> <code>' + id +
            '</code> <span class="audit-resource-removed">' + removed + '</span></p>';
    }
    const link = auditResourceLink(log);
    return link || ('<p><strong>ID:</strong> ' + id + '</p>');
}

async function auditOpenConversationChat(conversationId) {
    const id = String(conversationId || '').trim();
    if (!id) return;
    if (typeof apiFetch === 'function') {
        try {
            const r = await apiFetch('/api/conversations/' + encodeURIComponent(id));
            if (!r.ok) {
                if (typeof showToast === 'function') {
                    showToast(auditT('settingsAudit.resourceRemoved', null, '（关联对象已删除）'), 'warning');
                }
                return;
            }
        } catch (_) {
            return;
        }
    }
    closeAuditDetailModal();
    if (typeof switchPage === 'function') {
        switchPage('chat');
    }
    if (typeof loadConversation === 'function') {
        void loadConversation(id);
    }
}
window.auditOpenConversationChat = auditOpenConversationChat;

function auditResourceLink(log) {
    if (!log || auditResourceUnavailable(log)) return '';
    const type = log.resourceType || '';
    const id = log.resourceId || '';
    if (!id) return '';
    const esc = typeof escapeHtml === 'function' ? escapeHtml : function (s) { return String(s || ''); };
    const label = esc(auditT('settingsAudit.openResource', null, '打开关联资源'));
    if (type === 'conversation' || (type === '' && id.length > 8 && !id.startsWith('c2_'))) {
        const chatLabel = esc(auditT('settingsAudit.openResourceChat', null, '打开关联资源（chat）'));
        return '<p><button type="button" class="btn-secondary btn-small audit-open-chat-btn" data-conversation-id="' +
            esc(id) + '">' + chatLabel + '</button></p>';
    }
    if (type === 'vulnerability' || type === 'batch_queue') {
        const page = type === 'batch_queue' ? 'tasks' : 'vulnerabilities';
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchPage===\'function\'){switchPage(\'' + page + '\');}">' + label + '</button></p>';
    }
    if (type === 'c2_listener' || type === 'c2_session' || type === 'c2_task') {
        const page = type === 'c2_listener' ? 'c2-listeners' : (type === 'c2_session' ? 'c2-sessions' : 'c2-tasks');
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchPage===\'function\'){switchPage(\'' + page + '\');}">' + label + '</button></p>';
    }
    if (type === 'webshell_connection') {
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchPage===\'function\'){switchPage(\'webshell\');}">' + label + '</button></p>';
    }
    if (type === 'knowledge_item') {
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchPage===\'function\'){switchPage(\'knowledge-management\');}">' + label + '</button></p>';
    }
    if (type === 'chat_upload') {
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchPage===\'function\'){switchPage(\'chat-files\');}">' + label + '</button></p>';
    }
    if (type === 'tool_execution') {
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchPage===\'function\'){switchPage(\'mcp-monitor\');}">' + label + '</button></p>';
    }
    if (type === 'role' || type === 'skill' || type === 'markdown_agent') {
        return '<p><button type="button" class="btn-secondary btn-small" onclick="closeAuditDetailModal();if(typeof switchSettingsSection===\'function\'){switchPage(\'settings\');switchSettingsSection(\'roles\');}">' + label + '</button></p>';
    }
    return '';
}

function refreshAuditLogs() {
    loadAuditLogs(auditLogsPage);
}

async function downloadAuditExport(url, filename) {
    const r = await apiFetch(url);
    if (!r.ok) {
        const err = await r.json().catch(function () { return {}; });
        throw new Error(err.error || r.statusText);
    }
    const blob = await r.blob();
    const objectUrl = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = objectUrl;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(objectUrl);
}

function closeAuditExportMenu() {
    const menu = document.getElementById('audit-export-menu');
    const trigger = document.getElementById('audit-export-trigger');
    if (menu) menu.hidden = true;
    if (trigger) trigger.setAttribute('aria-expanded', 'false');
}

function toggleAuditExportMenu(ev) {
    if (ev && ev.stopPropagation) ev.stopPropagation();
    const menu = document.getElementById('audit-export-menu');
    const trigger = document.getElementById('audit-export-trigger');
    if (!menu) return;
    const willOpen = menu.hidden;
    if (willOpen) {
        menu.hidden = false;
        if (trigger) trigger.setAttribute('aria-expanded', 'true');
        if (!window._auditExportMenuDocBound) {
            window._auditExportMenuDocBound = true;
            document.addEventListener('click', function () {
                closeAuditExportMenu();
            });
        }
    } else {
        closeAuditExportMenu();
    }
}

async function runAuditExport(format) {
    closeAuditExportMenu();
    if (format === 'csv') {
        await exportAuditLogsCsv();
    } else {
        await exportAuditLogs();
    }
}

async function exportAuditLogs() {
    if (typeof apiFetch !== 'function') return;
    try {
        await downloadAuditExport(
            '/api/audit/logs/export?' + buildAuditQueryParams(true),
            'audit-logs-' + new Date().toISOString().slice(0, 10) + '.json'
        );
        if (typeof showToast === 'function') {
            showToast(auditT('settingsAudit.exportDone', null, '导出完成'), 'success');
        }
    } catch (e) {
        if (typeof showToast === 'function') {
            showToast(e.message || String(e), 'error');
        }
    }
}

async function exportAuditLogsCsv() {
    if (typeof apiFetch !== 'function') return;
    try {
        const qs = buildAuditQueryParams(true);
        await downloadAuditExport(
            '/api/audit/logs/export?' + (qs ? qs + '&' : '') + 'format=csv',
            'audit-logs-' + new Date().toISOString().slice(0, 10) + '.csv'
        );
        if (typeof showToast === 'function') {
            showToast(auditT('settingsAudit.exportDone', null, '导出完成'), 'success');
        }
    } catch (e) {
        if (typeof showToast === 'function') {
            showToast(e.message || String(e), 'error');
        }
    }
}

function closeAuditDetailModal() {
    closeAppModal('audit-detail-modal');
    const el = document.getElementById('audit-detail-modal');
    if (el) el.remove();
    syncAppModalBodyLock();
}

async function showAuditLogDetail(id) {
    if (!id || typeof apiFetch !== 'function') return;
    const esc = typeof escapeHtml === 'function' ? escapeHtml : function (s) { return String(s || ''); };
    try {
        closeAuditDetailModal();
        const overlay = document.createElement('div');
        overlay.id = 'audit-detail-modal';
        overlay.className = 'modal';
        document.body.appendChild(overlay);
        openAppModal(overlay, { focus: false });
        const r = await apiFetch('/api/audit/logs/' + encodeURIComponent(id));
        if (!r.ok) throw new Error('not found');
        const data = await r.json();
        const log = data.log || {};
        const detail = log.detail ? JSON.stringify(log.detail, null, 2) : '';
        const catAction = esc(auditCategoryLabel(log.category || '')) + ' / ' + esc(auditActionLabel(log.action || ''));
        deferModalContent(function () {
            overlay.innerHTML =
                '<div class="modal-content" style="max-width: 720px;">' +
                '<div class="modal-header">' +
                '<h2>' + esc(auditT('settingsAudit.detailTitle', null, '审计详情')) + '</h2>' +
                '<span class="modal-close" onclick="closeAuditDetailModal()">&times;</span>' +
                '</div>' +
                '<div class="modal-body audit-detail-body">' +
                '<p><strong>' + esc(auditT('settingsAudit.detailTime', null, '时间')) + ':</strong> ' + esc(formatAuditTime(log.createdAt)) + '</p>' +
                '<p><strong>' + esc(auditT('settingsAudit.detailCategory', null, '类别')) + ':</strong> ' + catAction + '</p>' +
                '<p><strong>' + esc(auditT('settingsAudit.detailResult', null, '结果')) + ':</strong> ' + esc(auditResultLabel(log.result || '')) + '</p>' +
                '<p><strong>' + esc(auditT('settingsAudit.detailMessage', null, '说明')) + ':</strong> ' + esc(auditMessageLabel(log)) + '</p>' +
                (log.clientIp ? '<p><strong>IP:</strong> ' + esc(log.clientIp) + '</p>' : '') +
                (log.sessionHint ? '<p><strong>' + esc(auditT('settingsAudit.detailSession', null, '会话')) + ':</strong> ' + esc(log.sessionHint) + '</p>' : '') +
                (log.userAgent ? '<p><strong>UA:</strong> ' + esc(log.userAgent) + '</p>' : '') +
                auditResourceMeta(log) +
                (detail ? '<pre class="audit-detail-pre">' + esc(detail) + '</pre>' : '') +
                '</div>' +
                '<div class="modal-footer"><button type="button" class="btn-secondary" onclick="closeAuditDetailModal()">' +
                esc(auditT('common.close', null, '关闭')) + '</button></div>' +
                '</div>';
            const chatBtn = overlay.querySelector('.audit-open-chat-btn');
            if (chatBtn) {
                chatBtn.addEventListener('click', function () {
                    auditOpenConversationChat(chatBtn.getAttribute('data-conversation-id'));
                });
            }
            overlay.addEventListener('click', function (ev) {
                if (ev.target === overlay) closeAuditDetailModal();
            });
        });
    } catch (e) {
        closeAuditDetailModal();
        if (typeof showToast === 'function') {
            showToast(e.message || String(e), 'error');
        }
    }
}

function initAuditLogsSection() {
    if (!document.getElementById('audit-log-list')) return;
    initAuditPageSizeFromStorage();
    initAuditFilterSelects();
    rebuildAuditActionSelect();
    if (typeof window.AuditDatetimePicker !== 'undefined' && typeof window.AuditDatetimePicker.init === 'function') {
        window.AuditDatetimePicker.init();
    }
    initAuditTimePresets();
    updateAuditTimezoneHint();
    loadAuditLogs(1);
}

function refreshAuditFilterI18n() {
    const section = document.getElementById('settings-section-audit');
    if (section && typeof applyTranslations === 'function') {
        applyTranslations(section);
    }
    rebuildAuditActionSelect();
    syncAuditCustomSelect('audit-filter-category');
    syncAuditCustomSelect('audit-filter-action');
    syncAuditCustomSelect('audit-filter-result');
    updateAuditTimezoneHint();
}

function refreshAuditLogsI18n() {
    if (!document.getElementById('audit-log-list')) return;
    refreshAuditFilterI18n();
    if (auditLogsCache.length) {
        renderAuditLogs(auditLogsCache);
        renderAuditLogsPagination();
    }
}

document.addEventListener('languagechange', function () {
    try {
        refreshAuditLogsI18n();
    } catch (e) {
        console.warn('languagechange audit refresh failed', e);
    }
});

var auditCustomSelectMap = {};
var auditFilterSelectsDocListener = false;

function closeAuditCustomSelect(selectId) {
    var reg = auditCustomSelectMap[selectId];
    if (reg && reg.wrapper) {
        reg.wrapper.classList.remove('open');
    }
}

function closeAllAuditCustomSelects() {
    Object.keys(auditCustomSelectMap).forEach(function (id) {
        closeAuditCustomSelect(id);
    });
}

function syncAuditCustomSelect(selectId) {
    var reg = auditCustomSelectMap[selectId];
    if (!reg) return;
    var select = reg.select;
    var dropdown = reg.dropdown;
    var trigger = reg.trigger;
    var wrapper = reg.wrapper;
    var valueSpan = trigger.querySelector('.audit-custom-select-value');

    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        var item = document.createElement('div');
        item.className = 'audit-custom-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        }
        var check = document.createElement('span');
        check.className = 'audit-custom-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        var label = document.createElement('span');
        label.className = 'audit-custom-select-label';
        label.textContent = opt.textContent;
        item.appendChild(check);
        item.appendChild(label);
        dropdown.appendChild(item);
    });

    var selectedOpt = select.options[select.selectedIndex];
    if (valueSpan) {
        valueSpan.textContent = selectedOpt ? selectedOpt.textContent : '';
    }
    trigger.disabled = !!select.disabled;
    wrapper.classList.toggle('is-disabled', !!select.disabled);
}

function enhanceAuditFilterSelect(selectId) {
    var select = document.getElementById(selectId);
    if (!select) return;
    if (select.dataset.auditCustom === '1') {
        syncAuditCustomSelect(selectId);
        return;
    }
    select.dataset.auditCustom = '1';
    select.classList.add('audit-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    var wrapper = document.createElement('div');
    wrapper.className = 'audit-custom-select';

    var trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'audit-custom-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    var valueSpan = document.createElement('span');
    valueSpan.className = 'audit-custom-select-value';
    trigger.appendChild(valueSpan);
    var caret = document.createElement('span');
    caret.className = 'audit-custom-select-caret';
    caret.setAttribute('aria-hidden', 'true');
    caret.textContent = '▾';
    trigger.appendChild(caret);

    var dropdown = document.createElement('div');
    dropdown.className = 'audit-custom-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    var parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    auditCustomSelectMap[selectId] = {
        wrapper: wrapper,
        trigger: trigger,
        dropdown: dropdown,
        select: select
    };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        var open = wrapper.classList.contains('open');
        closeAllAuditCustomSelects();
        if (!open) wrapper.classList.add('open');
    });

    dropdown.addEventListener('click', function (e) {
        var opt = e.target.closest('.audit-custom-select-option');
        if (!opt) return;
        e.stopPropagation();
        var val = opt.getAttribute('data-value');
        if (val === null) val = '';
        closeAuditCustomSelect(selectId);
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        closeAuditCustomSelect(selectId);
        syncAuditCustomSelect(selectId);
    });

    syncAuditCustomSelect(selectId);
}

function initAuditFilterSelects() {
    if (!document.getElementById('audit-filter-category')) return;
    if (!auditFilterSelectsDocListener) {
        document.addEventListener('click', function () {
            closeAllAuditCustomSelects();
        });
        auditFilterSelectsDocListener = true;
    }
    enhanceAuditFilterSelect('audit-filter-category');
    enhanceAuditFilterSelect('audit-filter-action');
    enhanceAuditFilterSelect('audit-filter-result');
}
