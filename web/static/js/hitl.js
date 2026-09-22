function hitlReviewerNormalize(v) {
    const x = String(v || '').trim().toLowerCase();
    if (x === 'audit_agent' || x === 'agent' || x === 'ai') return 'audit_agent';
    return 'human';
}

function hitlParsePayloadObject(raw) {
    if (!raw) return {};
    if (typeof raw === 'object') return raw;
    try {
        const o = JSON.parse(String(raw));
        return o && typeof o === 'object' ? o : {};
    } catch (e) {
        return {};
    }
}

function hitlRenderContextBlocks(payloadObj) {
    if (!payloadObj || typeof payloadObj !== 'object') return '';
    const blocks = [];
    function addBlock(label, value) {
        const s = String(value || '').trim();
        if (!s) return;
        blocks.push(
            '<div class="hitl-context-block">' +
            '<div class="hitl-context-label">' + escapeHtml(label) + '</div>' +
            '<pre class="hitl-context-text">' + escapeHtml(s) + '</pre>' +
            '</div>'
        );
    }
    addBlock(hitlT('fieldUserMessage', 'User message'), payloadObj.userMessage);
    addBlock(hitlT('fieldThinking', 'Thinking'), payloadObj.thinking);
    addBlock(hitlT('fieldReasoning', 'Reasoning'), payloadObj.reasoningChain);
    addBlock(hitlT('fieldPlanning', 'Planning'), payloadObj.planning);
    return blocks.join('');
}

function hitlRenderExecutionResultBlock(payloadObj) {
    if (!payloadObj || typeof payloadObj !== 'object') return '';
    const exec = payloadObj.executionResult;
    if (!exec || typeof exec !== 'object') return '';
    const ok = exec.success === true;
    const label = hitlT('fieldExecutionResult', 'Execution result') + (ok ? ' (' + hitlT('executionSuccess', 'success') + ')' : ' (' + hitlT('executionFailed', 'failed') + ')');
    const text = String(exec.result || '').trim();
    if (!text) return '';
    return (
        '<div class="hitl-context-block hitl-context-block--execution">' +
        '<div class="hitl-context-label">' + escapeHtml(label) + '</div>' +
        '<pre class="hitl-context-text">' + escapeHtml(text) + '</pre>' +
        '</div>'
    );
}

function hitlFillLogModalReadonlySections(payloadObj) {
    const ctxEl = document.getElementById('hitl-log-context-readonly');
    const execEl = document.getElementById('hitl-log-execution-readonly');
    const ctxHtml = hitlRenderContextBlocks(payloadObj);
    const execHtml = hitlRenderExecutionResultBlock(payloadObj);
    if (ctxEl) {
        ctxEl.innerHTML = ctxHtml;
        ctxEl.hidden = !ctxHtml;
    }
    if (execEl) {
        execEl.innerHTML = execHtml;
        execEl.hidden = !execHtml;
    }
}

function hitlPayloadSummary(payloadObj) {
    const parts = [];
    if (payloadObj && payloadObj.userMessage) parts.push(hitlT('fieldUserMessage', 'User'));
    if (payloadObj && payloadObj.thinking) parts.push(hitlT('fieldThinking', 'Thinking'));
    if (payloadObj && payloadObj.executionResult) parts.push(hitlT('fieldExecutionResult', 'Result'));
    return parts.length ? parts.join(' · ') : '—';
}

function hitlModeNormalize(m) {
    let v = String(m || '').trim().toLowerCase().replace(/-/g, '_');
    if (v === 'feedback' || v === 'followup') {
        v = 'approval';
    }
    const allowed = ['off', 'approval', 'review_edit'];
    return allowed.indexOf(v) >= 0 ? v : 'off';
}

function hitlT(key, fallback, params) {
    const fullKey = 'hitl.' + key;
    try {
        if (typeof window.t === 'function') {
            const translated = window.t(fullKey, params || {});
            if (typeof translated === 'string' && translated && translated !== fullKey) {
                return translated;
            }
        }
    } catch (e) {}
    return fallback;
}

const HITL_LOGS_PAGE_SIZE_KEY = 'cyberstrike_hitl_logs_page_size';
const HITL_PENDING_PAGE_SIZE_KEY = 'cyberstrike_hitl_pending_page_size';
const HITL_TIMEOUT_DEFAULT_MIGRATION_PREFIX = 'cyberstrike-hitl-timeout-default-v1:';
const HITL_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];
const hitlConversationConfigSaveQueues = new Map();

function hitlPaginationT(key, opts, fallback) {
    if (typeof window.t === 'function') {
        const keys = (key === 'paginationInfo' || key === 'perPageLabel')
            ? ['mcpMonitor.' + key, 'mcp.' + key]
            : ['mcp.' + key];
        for (let i = 0; i < keys.length; i++) {
            const v = window.t(keys[i], opts || {});
            if (typeof v === 'string' && v && v !== keys[i]) return v;
        }
    }
    return fallback != null ? fallback : key;
}

function hitlLocale() {
    if (typeof window.__locale === 'string' && window.__locale.length) {
        return window.__locale.startsWith('zh') ? 'zh-CN' : 'en-US';
    }
    return (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en-US';
}

function initHitlPageSizeFromStorage(storageKey, fallbackSize, assignFn) {
    try {
        const saved = parseInt(localStorage.getItem(storageKey), 10);
        if (HITL_PAGE_SIZE_OPTIONS.indexOf(saved) >= 0) {
            assignFn(saved);
            return;
        }
    } catch (e) { /* ignore */ }
    assignFn(fallbackSize);
}

function renderHitlPagination(containerId, state, goPageFnName, pageSizeChangeFnName, pageSizeSelectId) {
    const container = document.getElementById(containerId);
    if (!container) return;
    const esc = typeof escapeHtml === 'function' ? escapeHtml : function (s) { return String(s || ''); };
    const total = state.total || 0;
    const currentPage = state.page || 1;
    const pageSize = state.pageSize || 20;
    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(currentPage * pageSize, total);
    const infoText = hitlPaginationT('paginationInfo', { start: start, end: end, total: total },
        '显示 ' + start + '-' + end + ' / 共 ' + total + ' 条记录');
    const perPageLabel = hitlPaginationT('perPageLabel', null, '每页显示');
    const firstPageLabel = hitlPaginationT('firstPage', null, '首页');
    const prevPageLabel = hitlPaginationT('prevPage', null, '上一页');
    const pageInfoText = hitlPaginationT('pageInfo', { page: currentPage, total: totalPages },
        '第 ' + currentPage + ' / ' + totalPages + ' 页');
    const nextPageLabel = hitlPaginationT('nextPage', null, '下一页');
    const lastPageLabel = hitlPaginationT('lastPage', null, '末页');
    const disabledFirst = currentPage === 1 || total === 0;
    const disabledLast = currentPage >= totalPages || total === 0;
    let html = '<div class="monitor-pagination">';
    html += '<div class="pagination-info">';
    html += '<span>' + esc(infoText) + '</span>';
    html += '<label class="pagination-page-size">' + esc(perPageLabel);
    html += '<select id="' + esc(pageSizeSelectId) + '" onchange="' + esc(pageSizeChangeFnName) + '()">';
    HITL_PAGE_SIZE_OPTIONS.forEach(function (n) {
        html += '<option value="' + n + '"' + (pageSize === n ? ' selected' : '') + '>' + n + '</option>';
    });
    html += '</select></label></div>';
    html += '<div class="pagination-controls">';
    html += '<button type="button" class="btn-secondary" onclick="' + esc(goPageFnName) + '(1)"' + (disabledFirst ? ' disabled' : '') + '>' + esc(firstPageLabel) + '</button>';
    html += '<button type="button" class="btn-secondary" onclick="' + esc(goPageFnName) + '(' + (currentPage - 1) + ')"' + (disabledFirst ? ' disabled' : '') + '>' + esc(prevPageLabel) + '</button>';
    html += '<span class="pagination-page">' + esc(pageInfoText) + '</span>';
    html += '<button type="button" class="btn-secondary" onclick="' + esc(goPageFnName) + '(' + (currentPage + 1) + ')"' + (disabledLast ? ' disabled' : '') + '>' + esc(nextPageLabel) + '</button>';
    html += '<button type="button" class="btn-secondary" onclick="' + esc(goPageFnName) + '(' + totalPages + ')"' + (disabledLast ? ' disabled' : '') + '>' + esc(lastPageLabel) + '</button>';
    html += '</div></div>';
    container.innerHTML = html;
}

function hitlEffectiveEnabled(cfg) {
    if (!cfg) return false;
    if (cfg.enabled === true) return true;
    return hitlModeNormalize(cfg.mode) !== 'off';
}

function readHitlLocalStorageConv(conversationId) {
    if (!conversationId) return null;
    try {
        const key = 'cyberstrike-chat-hitl:' + String(conversationId).trim();
        const raw = localStorage.getItem(key);
        if (!raw) return null;
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object') return null;
        return parsed;
    } catch (e) {
        return null;
    }
}

function hitlSensitiveToolsToArray(config) {
    if (Array.isArray(config && config.sensitiveTools)) return config.sensitiveTools;
    const s = config && config.sensitiveTools;
    if (typeof s === 'string') {
        return s.split(/[,\n\r]+/).map(function (x) { return x.trim(); }).filter(Boolean);
    }
    return [];
}

function normalizeHitlTimeoutSeconds(v, fallback) {
    const n = Number(v);
    if (Number.isFinite(n)) {
        return n > 0 ? Math.floor(n) : 0;
    }
    const f = Number(fallback);
    if (Number.isFinite(f)) {
        return f > 0 ? Math.floor(f) : 0;
    }
    return 0;
}

function shouldMigrateLegacyHitlTimeout(conversationId, timeoutSeconds) {
    if (!conversationId || normalizeHitlTimeoutSeconds(timeoutSeconds, 0) > 0) return false;
    try {
        return localStorage.getItem(HITL_TIMEOUT_DEFAULT_MIGRATION_PREFIX + conversationId) !== '1';
    } catch (e) {
        return false;
    }
}

function markLegacyHitlTimeoutMigrated(conversationId) {
    try {
        localStorage.setItem(HITL_TIMEOUT_DEFAULT_MIGRATION_PREFIX + conversationId, '1');
    } catch (e) { /* ignore */ }
}

function getCurrentConversationIdForHitl() {
    if (typeof window.currentConversationId === 'string' && window.currentConversationId) {
        return window.currentConversationId;
    }
    const active = document.querySelector('.conversation-item.active');
    if (active && active.dataset && active.dataset.conversationId) {
        return active.dataset.conversationId;
    }
    return '';
}

async function fetchHitlConversationConfig(conversationId) {
    if (!conversationId) return null;
    const resp = await hitlApiFetch('/api/hitl/config/' + encodeURIComponent(conversationId), { credentials: 'same-origin' });
    if (!resp.ok) return null;
    const data = await resp.json();
    if (!data || !data.hitl) return null;
    return {
        hitl: data.hitl,
        defaultMode: hitlModeNormalize(data.defaultMode || 'off'),
        defaultReviewer: hitlReviewerNormalize(data.defaultReviewer || 'human'),
        defaultTimeoutSeconds: normalizeHitlTimeoutSeconds(data.defaultTimeoutSeconds, 300),
        hitlGlobalToolWhitelist: Array.isArray(data.hitlGlobalToolWhitelist) ? data.hitlGlobalToolWhitelist : []
    };
}

function applyHitlDefaultReviewerFromServer(reviewer) {
    return applyHitlDefaultConfigFromServer({ defaultReviewer: reviewer });
}

function applyHitlDefaultConfigFromServer(data) {
    const src = data && typeof data === 'object' ? data : {};
    const mode = hitlModeNormalize(src.defaultMode || src.mode || 'off');
    const reviewer = hitlReviewerNormalize(src.defaultReviewer || src.reviewer || 'human');
    const timeoutSeconds = normalizeHitlTimeoutSeconds(
        src.defaultTimeoutSeconds != null ? src.defaultTimeoutSeconds : src.timeoutSeconds,
        300
    );
    const out = {
        mode: mode,
        reviewer: reviewer,
        timeoutSeconds: timeoutSeconds
    };
    const backend = hitlNormalizeAuditBackend(src.auditBackend || src.audit_backend);
    const model = String(src.auditModel || src.audit_model || '').trim();
    if (backend) out.auditBackend = backend;
    if (model) out.auditModel = model;
    if (typeof window !== 'undefined') {
        window.csaiHitlDefaultConfig = out;
        window.csaiHitlDefaultReviewer = reviewer;
        if (backend) window.csaiHitlAuditBackend = backend;
        if (model || backend) window.csaiHitlAuditModel = model;
        if (Array.isArray(src.hitlGlobalToolWhitelist)) {
            window.csaiHitlGlobalToolWhitelist = src.hitlGlobalToolWhitelist;
        }
    }
    return out;
}

async function fetchHitlDefaultConfig() {
    const resp = await hitlApiFetch('/api/hitl/default-config', { credentials: 'same-origin' });
    if (!resp.ok) {
        return applyHitlDefaultConfigFromServer({ defaultMode: 'off', defaultReviewer: 'human', defaultTimeoutSeconds: 300 });
    }
    const data = await resp.json();
    return applyHitlDefaultConfigFromServer(data);
}

async function fetchHitlDefaultReviewer() {
    const cfg = await fetchHitlDefaultConfig();
    return hitlReviewerNormalize(cfg && cfg.reviewer);
}

async function putHitlDefaultConfig(config) {
    const current = (typeof window !== 'undefined' && window.csaiHitlDefaultConfig && typeof window.csaiHitlDefaultConfig === 'object')
        ? window.csaiHitlDefaultConfig
        : { mode: 'off', reviewer: 'human', timeoutSeconds: 300 };
    const cfg = config && typeof config === 'object' ? config : {};
    const payload = {
        mode: hitlModeNormalize(cfg.mode != null ? cfg.mode : current.mode),
        reviewer: hitlReviewerNormalize(cfg.reviewer != null ? cfg.reviewer : current.reviewer),
        timeoutSeconds: normalizeHitlTimeoutSeconds(
            cfg.timeoutSeconds != null ? cfg.timeoutSeconds : current.timeoutSeconds,
            300
        )
    };
    const resp = await hitlApiFetch('/api/hitl/default-config', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    });
    if (!resp.ok) {
        const msg = await readHitlApiError(resp);
        throw new Error(msg || ('HTTP ' + resp.status));
    }
    const data = await resp.json();
    return applyHitlDefaultConfigFromServer(data);
}

async function putHitlDefaultReviewer(reviewer) {
    const cfg = await putHitlDefaultConfig({ reviewer: reviewer });
    return hitlReviewerNormalize(cfg && cfg.reviewer);
}

async function initHitlDefaultReviewerFromServer() {
    try {
        await fetchHitlDefaultConfig();
        if (!getCurrentConversationIdForHitl() && typeof window.refreshHitlConfigByCurrentConversation === 'function') {
            window.refreshHitlConfigByCurrentConversation();
        }
        refreshHitlPageReviewerBar();
    } catch (e) {
        console.warn('initHitlDefaultReviewerFromServer', e);
    }
}

/** 无会话时：将免审批工具合并进服务端 config.yaml，返回更新后的全局白名单数组 */
async function mergeHitlGlobalToolWhitelist(sensitiveTools) {
    const list = Array.isArray(sensitiveTools) ? sensitiveTools : [];
    const resp = await hitlApiFetch('/api/hitl/tool-whitelist', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sensitiveTools: list })
    });
    if (!resp.ok) {
        const msg = await readHitlApiError(resp);
        throw new Error(msg || ('HTTP ' + resp.status));
    }
    const data = await resp.json();
    if (data && Array.isArray(data.hitlGlobalToolWhitelist)) {
        return data.hitlGlobalToolWhitelist;
    }
    return [];
}

function hitlPageToolsSplit(s) {
    if (typeof window.hitlToolsSplitToArray === 'function') {
        return window.hitlToolsSplitToArray(s);
    }
    return String(s || '').split(/[,\n\r]+/).map(function (x) { return x.trim(); }).filter(Boolean);
}

function hitlPageToolsMergeDisplay(globalArr, sessionToolsArr) {
    if (typeof window.hitlMergeToolsForDisplay === 'function') {
        return window.hitlMergeToolsForDisplay(globalArr, sessionToolsArr);
    }
    const out = [];
    const seen = Object.create(null);
    function addOne(t) {
        const n = String(t || '').trim();
        if (!n) return;
        const k = n.toLowerCase();
        if (seen[k]) return;
        seen[k] = true;
        out.push(n);
    }
    if (Array.isArray(globalArr)) globalArr.forEach(addOne);
    if (Array.isArray(sessionToolsArr)) sessionToolsArr.forEach(addOne);
    return out.join(', ');
}

function showHitlPageWhitelistFeedback(text, isError) {
    const el = document.getElementById('hitl-page-whitelist-feedback');
    if (!el) return;
    const msg = String(text || '').trim();
    if (!msg) {
        el.hidden = true;
        el.textContent = '';
        el.className = 'hitl-apply-feedback';
        return;
    }
    el.hidden = false;
    el.textContent = msg;
    el.className = 'hitl-apply-feedback' + (isError ? ' hitl-apply-feedback--error' : '');
}

function syncHitlSidebarWhitelistDisplay(_toolsStr) {
    // The chat field is conversation-scoped. Updating the global allowlist page
    // must not replace it with a merged global + conversation display value.
}

async function fetchHitlGlobalToolWhitelist() {
    const resp = await hitlApiFetch('/api/hitl/tool-whitelist', { credentials: 'same-origin' });
    if (!resp.ok) {
        throw new Error(await readHitlApiError(resp));
    }
    const data = await resp.json();
    const list = Array.isArray(data.toolWhitelist) ? data.toolWhitelist : (
        Array.isArray(data.hitlGlobalToolWhitelist) ? data.hitlGlobalToolWhitelist : []
    );
    if (typeof window !== 'undefined') {
        window.csaiHitlGlobalToolWhitelist = list;
    }
    return list;
}

async function resolveHitlGlobalToolWhitelist() {
    try {
        return await fetchHitlGlobalToolWhitelist();
    } catch (e) {
        if (typeof window !== 'undefined' && Array.isArray(window.csaiHitlGlobalToolWhitelist)) {
            return window.csaiHitlGlobalToolWhitelist.slice();
        }
        try {
            const resp = await hitlApiFetch('/api/config', { credentials: 'same-origin' });
            if (resp.ok) {
                const cfg = await resp.json();
                const tw = cfg.hitl && cfg.hitl.tool_whitelist;
                if (Array.isArray(tw)) {
                    if (typeof window !== 'undefined') {
                        window.csaiHitlGlobalToolWhitelist = tw.slice();
                    }
                    return tw.slice();
                }
            }
        } catch (e2) {
            console.warn('resolveHitlGlobalToolWhitelist fallback', e2);
        }
        throw e;
    }
}

function hitlPageWhitelistDisplayValue(globalArr, sessionArr) {
    return hitlPageToolsMergeDisplay(globalArr, sessionArr);
}

async function refreshHitlPageWhitelist() {
    const ta = document.getElementById('hitl-page-sensitive-tools');
    if (!ta) return;
    const cached = typeof window !== 'undefined' && Array.isArray(window.csaiHitlGlobalToolWhitelist)
        ? window.csaiHitlGlobalToolWhitelist
        : [];
    if (cached.length > 0) {
        ta.value = hitlPageWhitelistDisplayValue(cached, []);
    }
    try {
        const globalArr = await resolveHitlGlobalToolWhitelist();
        const cid = getCurrentConversationIdForHitl();
        let sessionArr = [];
        if (cid) {
            const cfg = typeof window.getHitlConfigForConversation === 'function'
                ? window.getHitlConfigForConversation(cid)
                : null;
            sessionArr = hitlSensitiveToolsToArray(cfg || {});
        }
        ta.value = hitlPageWhitelistDisplayValue(globalArr, sessionArr);
        syncHitlSidebarWhitelistDisplay(ta.value);
    } catch (e) {
        console.warn('refreshHitlPageWhitelist', e);
        if (!ta.value.trim() && cached.length > 0) {
            ta.value = hitlPageWhitelistDisplayValue(cached, []);
        }
    }
}

async function putHitlGlobalToolWhitelist(toolWhitelist) {
    const list = Array.isArray(toolWhitelist) ? toolWhitelist : [];
    const resp = await hitlApiFetch('/api/hitl/tool-whitelist', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ toolWhitelist: list })
    });
    if (!resp.ok) {
        throw new Error(await readHitlApiError(resp));
    }
    const data = await resp.json();
    const out = Array.isArray(data.toolWhitelist) ? data.toolWhitelist : (
        Array.isArray(data.hitlGlobalToolWhitelist) ? data.hitlGlobalToolWhitelist : list
    );
    if (typeof window !== 'undefined') {
        window.csaiHitlGlobalToolWhitelist = out;
    }
    return out;
}

async function saveHitlPageWhitelist() {
    const ta = document.getElementById('hitl-page-sensitive-tools');
    const btn = document.getElementById('hitl-page-whitelist-save-btn');
    if (!ta) return;
    showHitlPageWhitelistFeedback('', false);
    if (btn) btn.disabled = true;
    try {
        const desired = hitlPageToolsSplit(ta.value);
        const globalArr = await putHitlGlobalToolWhitelist(desired);
        const displayStr = hitlPageToolsMergeDisplay(globalArr, []);
        ta.value = displayStr;
        syncHitlSidebarWhitelistDisplay(displayStr);

        const cid = getCurrentConversationIdForHitl();
        if (cid) {
            const cfg = typeof window.getHitlConfigForConversation === 'function'
                ? window.getHitlConfigForConversation(cid)
                : { mode: 'off', reviewer: 'human', sensitiveTools: '', timeoutSeconds: 0 };
            const nextCfg = Object.assign({}, cfg, { sensitiveTools: '' });
            if (typeof window.saveHitlConfigForConversation === 'function') {
                window.saveHitlConfigForConversation(cid, nextCfg);
            }
            if (typeof saveHitlConversationConfig === 'function') {
                await saveHitlConversationConfig(cid, nextCfg);
            }
        }

        showHitlPageWhitelistFeedback(hitlT('whitelistSaved', 'Whitelist saved.'), false);
    } catch (e) {
        showHitlPageWhitelistFeedback(hitlT('whitelistSaveFailed', 'Failed to save whitelist') + ': ' + (e.message || e), true);
    } finally {
        if (btn) btn.disabled = false;
    }
}

async function saveHitlConversationConfig(conversationId, config) {
    if (!conversationId || !config) return false;
    const normalizedConversationId = String(conversationId).trim();
    const mode = hitlModeNormalize(config.mode || 'off');
    const enabled = typeof config.enabled === 'boolean' ? config.enabled : (mode !== 'off');
    const sensitiveTools = hitlSensitiveToolsToArray(config);
    const timeoutSeconds = normalizeHitlTimeoutSeconds(config.timeoutSeconds, 0);
    const reviewer = hitlReviewerNormalize(config.reviewer || 'human');
    const previous = hitlConversationConfigSaveQueues.get(normalizedConversationId) || Promise.resolve();
    const queued = previous.catch(function () {}).then(async function () {
        const resp = await hitlApiFetch('/api/hitl/config', {
            method: 'PUT',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                conversationId: normalizedConversationId,
                enabled: enabled,
                mode: mode,
                reviewer: reviewer,
                sensitiveTools: sensitiveTools,
                timeoutSeconds: timeoutSeconds
            })
        });
        if (!resp.ok) {
            const msg = await readHitlApiError(resp);
            throw new Error(msg || ('HTTP ' + resp.status));
        }
        return true;
    });
    hitlConversationConfigSaveQueues.set(normalizedConversationId, queued);
    return queued.finally(function () {
        if (hitlConversationConfigSaveQueues.get(normalizedConversationId) === queued) {
            hitlConversationConfigSaveQueues.delete(normalizedConversationId);
        }
    });
}

async function syncHitlConfigFromServer(conversationId) {
    const pack = await fetchHitlConversationConfig(conversationId);
    if (!pack || !pack.hitl) return;
    const cfg = pack.hitl;
    const globalWL = pack.hitlGlobalToolWhitelist || [];
    applyHitlDefaultConfigFromServer({
        defaultMode: pack.defaultMode,
        defaultReviewer: pack.defaultReviewer,
        defaultTimeoutSeconds: pack.defaultTimeoutSeconds,
        hitlGlobalToolWhitelist: globalWL
    });
    if (typeof window !== 'undefined') {
        window.csaiHitlGlobalToolWhitelist = globalWL;
    }
    const strip = typeof window.hitlStripGlobalToolsFromFormString === 'function'
        ? window.hitlStripGlobalToolsFromFormString
        : function (_g, s) { return typeof s === 'string' ? s.trim() : ''; };

    let merged = cfg;
    if (!hitlEffectiveEnabled(cfg)) {
        const local = readHitlLocalStorageConv(conversationId);
        const localMode = local && local.mode ? hitlModeNormalize(local.mode) : 'off';
        if (localMode !== 'off') {
            const localReviewer = hitlReviewerNormalize(local && local.reviewer);
            let localToolsStr = typeof local.sensitiveTools === 'string' ? local.sensitiveTools : '';
            localToolsStr = strip(globalWL, localToolsStr);
            merged = {
                enabled: true,
                mode: localMode,
                reviewer: localReviewer,
                sensitiveTools: localToolsStr.split(/[,\n\r]+/).map(function (s) { return s.trim(); }).filter(Boolean),
                timeoutSeconds: normalizeHitlTimeoutSeconds(
                    local && local.timeoutSeconds,
                    normalizeHitlTimeoutSeconds(cfg.timeoutSeconds, 0)
                )
            };
            saveHitlConversationConfig(conversationId, {
                mode: localMode,
                reviewer: localReviewer,
                sensitiveTools: localToolsStr,
                enabled: true,
                timeoutSeconds: merged.timeoutSeconds
            }).catch(function (err) {
                console.warn('HITL 会话配置同步到服务器失败（将仅保留本地 UI）:', err);
            });
        }
    }
    if (shouldMigrateLegacyHitlTimeout(conversationId, merged.timeoutSeconds)) {
        merged = Object.assign({}, merged, { timeoutSeconds: 300 });
        try {
            await saveHitlConversationConfig(conversationId, merged);
            markLegacyHitlTimeoutMigrated(conversationId);
        } catch (err) {
            console.warn('HITL 旧会话等待时限迁移失败，将在下次加载时重试:', err);
        }
    } else if (normalizeHitlTimeoutSeconds(merged.timeoutSeconds, 0) > 0) {
        markLegacyHitlTimeoutMigrated(conversationId);
    }
    const uiMode = hitlEffectiveEnabled(merged) ? hitlModeNormalize(merged.mode) : 'off';
    const rawArr = Array.isArray(merged.sensitiveTools)
        ? merged.sensitiveTools
        : hitlSensitiveToolsToArray({ sensitiveTools: merged.sensitiveTools });
    const sessionOnlyStr = strip(globalWL, rawArr.join(', '));
    const normalizedCfg = Object.assign({}, merged, {
        mode: uiMode,
        reviewer: hitlReviewerNormalize(merged.reviewer || cfg.reviewer || 'human'),
        sensitiveTools: sessionOnlyStr
    });
    if (typeof window.saveHitlConfigForConversation === 'function') {
        window.saveHitlConfigForConversation(conversationId, normalizedCfg);
    } else {
        try {
            localStorage.setItem('chat_hitl_config_' + conversationId, JSON.stringify(normalizedCfg));
        } catch (e) {}
    }
    if (
        getCurrentConversationIdForHitl() === conversationId &&
        typeof window.applyHitlConfigToUI === 'function'
    ) {
        window.applyHitlConfigToUI(normalizedCfg);
    }
    reconcileHitlUiState();
}

async function syncHitlConfigToServerByCurrentConversation() {
    const conversationId = getCurrentConversationIdForHitl();
    if (!conversationId) return;
    if (typeof window.readHitlConfigFromForm !== 'function') return;
    const cfg = window.readHitlConfigFromForm();
    await saveHitlConversationConfig(conversationId, cfg);
}

function reconcileHitlUiState() {
    if (typeof window.readHitlConfigFromForm === 'function' && typeof window.updateHitlStatusUI === 'function') {
        try {
            const cfg = window.readHitlConfigFromForm();
            window.updateHitlStatusUI(cfg);
        } catch (e) {}
    }
}

let hitlFollowRunSeq = 0;

function hitlAutoResizeTextarea(textarea) {
    if (!textarea) return;
    textarea.style.height = 'auto';
    textarea.style.height = Math.max(textarea.scrollHeight, textarea.offsetHeight || 0) + 'px';
}

function bindHitlAutoResizeTextareas(root) {
    const scope = root || document;
    if (!scope || !scope.querySelectorAll) return;
    scope.querySelectorAll('.hitl-edit-args').forEach(function (textarea) {
        if (textarea.__hitlAutoResizeBound) {
            hitlAutoResizeTextarea(textarea);
            return;
        }
        textarea.__hitlAutoResizeBound = true;
        hitlAutoResizeTextarea(textarea);
        textarea.addEventListener('input', function () {
            hitlAutoResizeTextarea(textarea);
        });
    });
}

/**
 * 审批提交后原 SSE 已断开：轮询任务列表，运行中则拉取过程详情；任务结束后再整页加载会话以对齐终态。
 */
async function followAgentRunAfterHitlDecision(conversationId) {
    if (!conversationId || typeof apiFetch !== 'function') return;
    if (typeof window.attachRunningTaskEventStream === 'function') {
        try {
            const attached = await window.attachRunningTaskEventStream(conversationId);
            if (attached) return;
        } catch (e) {
            console.warn('attachRunningTaskEventStream', e);
        }
    }
    var mySeq = ++hitlFollowRunSeq;
    var intervalMs = 2000;
    var firstDelayMs = 500;
    var maxMs = 30 * 60 * 1000;
    var deadline = Date.now() + maxMs;

    function taskStillActive(cid) {
        return apiFetch('/api/agent-loop/tasks').then(function (r) {
            if (!r.ok) return false;
            return r.json().then(function (j) {
                var tasks = (j && j.tasks) ? j.tasks : [];
                return tasks.some(function (t) {
                    return t && t.conversationId === cid && (t.status === 'running' || t.status === 'cancelling');
                });
            });
        }).catch(function () { return false; });
    }

    await new Promise(function (r) { setTimeout(r, firstDelayMs); });

    while (mySeq === hitlFollowRunSeq) {
        if (Date.now() > deadline) {
            if (typeof window.loadConversation === 'function' && window.currentConversationId === conversationId) {
                await window.loadConversation(conversationId);
            }
            if (typeof loadActiveTasks === 'function') loadActiveTasks();
            return;
        }
        try {
            var active = await taskStillActive(conversationId);
            var onThisConv = (typeof window.currentConversationId === 'string' && window.currentConversationId === conversationId);
            if (onThisConv && typeof window.refreshLastAssistantProcessDetails === 'function') {
                await window.refreshLastAssistantProcessDetails(conversationId);
            }
            if (!active) {
                await new Promise(function (r) { setTimeout(r, 450); });
                if (typeof window.loadConversation === 'function' && window.currentConversationId === conversationId) {
                    await window.loadConversation(conversationId);
                }
                if (typeof loadActiveTasks === 'function') loadActiveTasks();
                return;
            }
        } catch (e) {
            console.warn('followAgentRunAfterHitlDecision', e);
        }
        await new Promise(function (r) { setTimeout(r, intervalMs); });
    }
}

function renderHitlPendingList(items) {
    const list = Array.isArray(items) ? items : [];
    if (!list.length) return '';
    return list.map(function (item) {
            const payloadObj = hitlParsePayloadObject(item.payload || '');
            const payload = String(item.payload || '');
            const contextHtml = hitlRenderContextBlocks(payloadObj);
            const mode = String(item.mode || '').trim().toLowerCase();
            const allowEdit = mode === 'review_edit';
            var escId = escapeHtml(String(item.id || ''));
            var qId = JSON.stringify(String(item.id || '')).replace(/"/g, '&quot;');
            var qConv = JSON.stringify(String(item.conversationId || '')).replace(/"/g, '&quot;');
            return (
                '<div class="hitl-pending-item">' +
                '<div class="hitl-pending-item-header">' +
                '<div class="hitl-pending-item-title">' +
                '<span class="hitl-tool-badge">' + escapeHtml(item.toolName || '-') + '</span>' +
                '<span class="hitl-mode-tag hitl-mode-tag--' + escapeHtml(mode) + '">' + escapeHtml(item.mode || '-') + '</span>' +
                '</div>' +
                '<button class="hitl-dismiss-btn" title="' + escapeHtml(hitlT('dismiss', 'Dismiss')) + '" onclick="dismissHitlItem(' + qId + ')">&times;</button>' +
                '</div>' +
                '<div class="hitl-pending-meta">' + escapeHtml(hitlT('conversationLabel', 'Conversation:')) + ' ' + escapeHtml(item.conversationId || '-') + '</div>' +
                contextHtml +
                hitlRenderExecutionResultBlock(payloadObj) +
                '<pre class="hitl-pending-payload">' + escapeHtml(payload) + '</pre>' +
                (allowEdit
                    ? ('<div class="hitl-input-help">' + escapeHtml(hitlT('reviewEditHelp', 'Review & edit mode: provide a JSON object to override tool arguments. Example: {"command":"ls -la"}')) + '</div>' +
                       '<textarea id="hitl-edit-' + escId + '" class="hitl-edit-args" placeholder=\'{"command":"ls -la"}\'></textarea>')
                    : '<div class="hitl-input-help">' + escapeHtml(hitlT('approvalHelp', 'Approval mode: only approve/reject, argument editing is disabled.')) + '</div>') +
                '<div class="hitl-input-help">' + escapeHtml(hitlT('commentHelp', 'Comment (optional): briefly note the approval reason.')) + '</div>' +
                '<input id="hitl-comment-' + escId + '" class="hitl-config-input hitl-inline-comment" type="text" placeholder="' + escapeHtml(hitlT('commentPlaceholder', 'e.g. allow read-only command')) + '">' +
                '<div class="hitl-pending-actions">' +
                '<button class="btn-secondary" onclick="submitHitlDecision(' + qId + ',&quot;reject&quot;,' + qConv + ')">' + escapeHtml(hitlT('reject', 'Reject')) + '</button>' +
                '<button class="btn-primary" onclick="submitHitlDecision(' + qId + ',&quot;approve&quot;,' + qConv + ')">' + escapeHtml(hitlT('approve', 'Approve')) + '</button>' +
                '</div>' +
                '</div>'
            );
        }).join('');
}

function hitlWorkflowPendingLabel(run) {
    const pending = hitlParsePayloadObject(run.pending_hitl_json || run.pendingHitlJson || '');
    const pendingHitl = pending.pendingHitl && typeof pending.pendingHitl === 'object' ? pending.pendingHitl : pending;
    return pendingHitl.label || pendingHitl.nodeId || run.pending_hitl_node_id || run.pendingHitlNodeId || run.workflow_id || run.workflowId || run.id || '-';
}

function renderWorkflowHitlPendingList(runs) {
    const list = Array.isArray(runs) ? runs : [];
    if (!list.length) return '';
    return list.map(function (run) {
        const runId = String(run.id || '').trim();
        const pending = hitlParsePayloadObject(run.pending_hitl_json || run.pendingHitlJson || '');
        const pendingHitl = pending.pendingHitl && typeof pending.pendingHitl === 'object' ? pending.pendingHitl : pending;
        const label = hitlWorkflowPendingLabel(run);
        const prompt = String(pendingHitl.prompt || '').trim();
        const convId = String(run.conversation_id || run.conversationId || '').trim();
        const qRun = JSON.stringify(runId).replace(/"/g, '&quot;');
        const qConv = JSON.stringify(convId).replace(/"/g, '&quot;');
        const workflowLabel = hitlT('workflowPendingTitle', 'Workflow approval');
        const openChatLabel = hitlT('openConversation', 'Open conversation');
        return (
            '<div class="hitl-pending-item hitl-pending-item--workflow">' +
            '<div class="hitl-pending-item-header">' +
            '<div class="hitl-pending-item-title">' +
            '<span class="hitl-tool-badge">' + escapeHtml(workflowLabel) + '</span>' +
            '<span class="hitl-mode-tag hitl-mode-tag--approval">' + escapeHtml(label) + '</span>' +
            '</div>' +
            '</div>' +
            '<div class="hitl-pending-meta">' + escapeHtml(hitlT('conversationLabel', 'Conversation:')) + ' ' + escapeHtml(convId || '-') + '</div>' +
            (prompt ? ('<div class="hitl-input-help">' + escapeHtml(prompt) + '</div>') : '') +
            '<div class="hitl-input-help">' + escapeHtml(hitlT('commentHelp', 'Comment (optional): briefly note the approval reason.')) + '</div>' +
            '<input id="workflow-hitl-comment-' + escapeHtml(runId) + '" class="hitl-config-input hitl-inline-comment" type="text" placeholder="' + escapeHtml(hitlT('commentPlaceholder', 'e.g. allow read-only command')) + '">' +
            '<div class="hitl-pending-actions">' +
            (convId ? ('<button class="btn-secondary" onclick="openHitlConversation(' + qConv + ')">' + escapeHtml(openChatLabel) + '</button>') : '') +
            '<button class="btn-secondary" onclick="submitWorkflowHitlDecisionFromPage(' + qRun + ', false, ' + qConv + ')">' + escapeHtml(hitlT('reject', 'Reject')) + '</button>' +
            '<button class="btn-primary" onclick="submitWorkflowHitlDecisionFromPage(' + qRun + ', true, ' + qConv + ')">' + escapeHtml(hitlT('approve', 'Approve')) + '</button>' +
            '</div>' +
            '</div>'
        );
    }).join('');
}

async function submitWorkflowHitlDecisionFromPage(runId, approved, conversationId) {
    const rid = String(runId || '').trim();
    if (!rid) return;
    const commentEl = document.getElementById('workflow-hitl-comment-' + rid);
    const comment = commentEl ? String(commentEl.value || '').trim() : '';
    try {
        if (typeof window.submitWorkflowHitlDecision === 'function') {
            await window.submitWorkflowHitlDecision(rid, approved, comment);
        } else {
            const resp = await hitlApiFetch('/api/workflows/runs/' + encodeURIComponent(rid) + '/resume', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'same-origin',
                body: JSON.stringify({ approved: !!approved, comment: comment })
            });
            const body = await resp.json().catch(function () { return {}; });
            if (!resp.ok) throw new Error((body && body.error) ? body.error : 'submit failed');
        }
        if (conversationId && typeof followAgentRunAfterHitlDecision === 'function') {
            await followAgentRunAfterHitlDecision(conversationId);
        }
        await refreshHitlPending();
    } catch (e) {
        alert((e && e.message) ? e.message : hitlT('submitFailed', 'Submit failed'));
    }
}

function openHitlConversation(conversationId) {
    const cid = String(conversationId || '').trim();
    if (!cid) return;
    if (typeof switchPage === 'function') {
        switchPage('chat');
    }
    if (typeof loadConversation === 'function') {
        loadConversation(cid);
    }
}

async function refreshHitlPending() {
    const container = document.getElementById('hitl-pending-list');
    if (!container) return;
    container.innerHTML = '<div class="loading-spinner">' + escapeHtml(hitlT('loading', 'Loading...')) + '</div>';
    try {
        const q = document.getElementById('hitl-pending-search');
        const params = new URLSearchParams({
            page: String(hitlPendingPage),
            pageSize: String(hitlPendingPageSize)
        });
        if (q && q.value.trim()) params.set('q', q.value.trim());
        const resp = await hitlApiFetch('/api/hitl/pending?' + params.toString(), { credentials: 'same-origin' });
        if (!resp.ok) {
            throw new Error('request failed');
        }
        const data = await resp.json();
        const rawItems = Array.isArray(data.items) ? data.items : [];
        const items = rawItems.filter(function (item) {
            return hitlReviewerNormalize(item && (item.reviewer || item.decidedBy || item.decided_by)) !== 'audit_agent' &&
                String(item && item.status || '').trim().toLowerCase() !== 'audit_running';
        });
        let workflowRuns = [];
        try {
            const wfResp = await hitlApiFetch('/api/workflows/runs/pending', { credentials: 'same-origin' });
            if (wfResp.ok) {
                const wfData = await wfResp.json().catch(function () { return {}; });
                workflowRuns = Array.isArray(wfData.runs) ? wfData.runs : [];
            }
        } catch (wfErr) {
            console.warn('fetch workflow pending runs failed', wfErr);
        }
        const searchQ = q && q.value.trim() ? q.value.trim().toLowerCase() : '';
        if (searchQ) {
            workflowRuns = workflowRuns.filter(function (run) {
                const conv = String(run.conversation_id || run.conversationId || '').toLowerCase();
                const wfId = String(run.workflow_id || run.workflowId || '').toLowerCase();
                const runId = String(run.id || '').toLowerCase();
                const label = hitlWorkflowPendingLabel(run).toLowerCase();
                return conv.indexOf(searchQ) >= 0 || wfId.indexOf(searchQ) >= 0 || runId.indexOf(searchQ) >= 0 || label.indexOf(searchQ) >= 0;
            });
        }
        const hiddenAgentItems = rawItems.length - items.length;
        hitlPendingTotal = Math.max(0, (typeof data.total === 'number' ? data.total : rawItems.length) - hiddenAgentItems) + workflowRuns.length;
        const maxPage = Math.max(1, Math.ceil(hitlPendingTotal / hitlPendingPageSize));
        if (hitlPendingPage > maxPage) {
            hitlPendingPage = maxPage;
            await refreshHitlPending();
            return;
        }
        const badge = document.getElementById('hitl-pending-count');
        if (badge) {
            badge.textContent = String(hitlPendingTotal);
            badge.hidden = hitlPendingTotal <= 0;
        }
        hitlPendingCache = items;
        hitlPendingLoaded = true;
        const workflowHtml = renderWorkflowHitlPendingList(workflowRuns);
        const toolHtml = items.length ? renderHitlPendingList(items) : '';
        if (!workflowHtml && !toolHtml) {
            container.innerHTML = '<div class="empty-state">' + escapeHtml(hitlT('emptyState', 'No pending approvals')) + '</div>';
        } else {
            container.innerHTML = workflowHtml + (workflowHtml && toolHtml ? '<div class="hitl-pending-section-divider"></div>' : '') + (toolHtml || '');
        }
        bindHitlAutoResizeTextareas(container);
        renderHitlPendingPagination();
    } catch (e) {
        hitlPendingLoaded = false;
        container.innerHTML = '<div class="empty-state">' + escapeHtml(hitlT('loadFailed', 'Failed to load')) + '</div>';
        renderHitlPendingPagination();
    }
}

function filterHitlPending() {
    hitlPendingPage = 1;
    refreshHitlPending();
}

async function submitHitlDecision(interruptId, decision, conversationIdOpt) {
    const commentBox = document.getElementById('hitl-comment-' + interruptId);
    const comment = (commentBox && commentBox.value) ? commentBox.value.trim() : '';
    let editedArguments = null;
    const editBox = document.getElementById('hitl-edit-' + interruptId);
    if (editBox && editBox.value && editBox.value.trim()) {
        try {
            editedArguments = JSON.parse(editBox.value.trim());
        } catch (e) {
            alert(hitlT('invalidJson', 'Invalid JSON arguments'));
            return;
        }
    }
    const convFollow = conversationIdOpt || getCurrentConversationIdForHitl();
    return submitHitlDecisionWithPayload(interruptId, decision, comment, editedArguments, convFollow);
}

async function submitHitlDecisionWithPayload(interruptId, decision, comment, editedArguments, conversationIdForFollow) {
    const resp = await hitlApiFetch('/api/hitl/decision', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ interruptId: interruptId, decision: decision, comment: comment, editedArguments: editedArguments })
    });
    if (!resp.ok) {
        const errText = await readHitlApiError(resp);
        if (resp.status === 409 && (errText.indexOf('already resolved') >= 0 || errText.indexOf('not found') >= 0)) {
            await dismissHitlItem(interruptId, true);
            return true;
        }
        alert(hitlT('submitFailedPrefix', 'Submit failed:') + ' ' + errText);
        return false;
    }
    refreshHitlPending();
    const cid = conversationIdForFollow || getCurrentConversationIdForHitl();
    if (cid) {
        followAgentRunAfterHitlDecision(cid);
    }
    return true;
}

async function hitlApiFetch(url, options) {
    if (typeof apiFetch === 'function') {
        return apiFetch(url, options || {});
    }
    return fetch(url, options || {});
}

async function readHitlApiError(resp) {
    try {
        const data = await resp.json();
        if (data && typeof data.error === 'string' && data.error.trim()) return data.error.trim();
        return 'HTTP ' + resp.status;
    } catch (e) {
        return 'HTTP ' + resp.status;
    }
}

async function dismissHitlItem(interruptId, silent) {
    try {
        await hitlApiFetch('/api/hitl/dismiss', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ interruptId: interruptId })
        });
    } catch (e) {
        if (!silent) { console.warn('dismissHitlItem', e); }
    }
    refreshHitlPending();
}

let hitlActiveTab = 'pending';
let hitlLogsPage = 1;
let hitlLogsPageSize = 20;
let hitlLogsTotal = 0;
let hitlLogsCache = [];
let hitlLogsLoaded = false;
let hitlLogsRetentionDays = 0;
const hitlSelectedLogs = new Set();
let hitlPendingPage = 1;
let hitlPendingPageSize = 20;
let hitlPendingTotal = 0;
let hitlPendingCache = [];
let hitlPendingLoaded = false;

function switchHitlPageTab(tab) {
    const tabs = ['pending', 'logs', 'strategy', 'whitelist'];
    hitlActiveTab = tabs.indexOf(tab) >= 0 ? tab : 'pending';
    const pendingTab = document.getElementById('hitl-tab-pending');
    const logsTab = document.getElementById('hitl-tab-logs');
    const strategyTab = document.getElementById('hitl-tab-strategy');
    const whitelistTab = document.getElementById('hitl-tab-whitelist');
    const pendingPanel = document.getElementById('hitl-panel-pending');
    const logsPanel = document.getElementById('hitl-panel-logs');
    const strategyPanel = document.getElementById('hitl-panel-strategy');
    const whitelistPanel = document.getElementById('hitl-panel-whitelist');
    if (pendingTab) {
        pendingTab.classList.toggle('hitl-page-tab--active', hitlActiveTab === 'pending');
        pendingTab.setAttribute('aria-selected', hitlActiveTab === 'pending' ? 'true' : 'false');
    }
    if (logsTab) {
        logsTab.classList.toggle('hitl-page-tab--active', hitlActiveTab === 'logs');
        logsTab.setAttribute('aria-selected', hitlActiveTab === 'logs' ? 'true' : 'false');
    }
    if (strategyTab) {
        strategyTab.classList.toggle('hitl-page-tab--active', hitlActiveTab === 'strategy');
        strategyTab.setAttribute('aria-selected', hitlActiveTab === 'strategy' ? 'true' : 'false');
    }
    if (whitelistTab) {
        whitelistTab.classList.toggle('hitl-page-tab--active', hitlActiveTab === 'whitelist');
        whitelistTab.setAttribute('aria-selected', hitlActiveTab === 'whitelist' ? 'true' : 'false');
    }
    if (pendingPanel) pendingPanel.hidden = hitlActiveTab !== 'pending';
    if (logsPanel) logsPanel.hidden = hitlActiveTab !== 'logs';
    if (strategyPanel) strategyPanel.hidden = hitlActiveTab !== 'strategy';
    if (whitelistPanel) whitelistPanel.hidden = hitlActiveTab !== 'whitelist';
    refreshHitlActivePanel();
}

function refreshHitlPageReviewerBar() {
    const cid = getCurrentConversationIdForHitl();
    const cfg = typeof window.getHitlConfigForConversation === 'function'
        ? window.getHitlConfigForConversation(cid)
        : null;
    if (cfg && typeof window.setHitlReviewerUI === 'function') {
        window.setHitlReviewerUI(cfg.reviewer);
    }
    if (typeof window.bindHitlReviewerToggleListeners === 'function') {
        window.bindHitlReviewerToggleListeners();
    }
    renderHitlPageAuditEngine();
    renderHitlStrategyJevHint();
}

let hitlDefaultAuditPrompt = '';
let hitlDefaultAuditPromptReviewEdit = '';
let hitlStrategyMode = 'approval';

function switchHitlStrategyMode(mode) {
    hitlStrategyMode = mode === 'review_edit' ? 'review_edit' : 'approval';
    const approvalTab = document.getElementById('hitl-strategy-tab-approval');
    const reviewTab = document.getElementById('hitl-strategy-tab-review-edit');
    const approvalTa = document.getElementById('hitl-audit-agent-prompt');
    const reviewTa = document.getElementById('hitl-audit-agent-prompt-review-edit');
    const hintApproval = document.getElementById('hitl-strategy-hint-approval');
    const hintReview = document.getElementById('hitl-strategy-hint-review-edit');
    if (approvalTab) {
        approvalTab.classList.toggle('hitl-strategy-subtab--active', hitlStrategyMode === 'approval');
        approvalTab.setAttribute('aria-selected', hitlStrategyMode === 'approval' ? 'true' : 'false');
    }
    if (reviewTab) {
        reviewTab.classList.toggle('hitl-strategy-subtab--active', hitlStrategyMode === 'review_edit');
        reviewTab.setAttribute('aria-selected', hitlStrategyMode === 'review_edit' ? 'true' : 'false');
    }
    if (approvalTa) approvalTa.hidden = hitlStrategyMode !== 'approval';
    if (reviewTa) reviewTa.hidden = hitlStrategyMode !== 'review_edit';
    if (hintApproval) hintApproval.hidden = hitlStrategyMode !== 'approval';
    if (hintReview) hintReview.hidden = hitlStrategyMode !== 'review_edit';
    renderHitlStrategyJevHint();
}

function showHitlStrategyFeedback(text, isError) {
    const el = document.getElementById('hitl-strategy-feedback');
    if (!el) return;
    const msg = String(text || '').trim();
    if (!msg) {
        el.hidden = true;
        el.textContent = '';
        el.className = 'hitl-apply-feedback';
        return;
    }
    el.hidden = false;
    el.textContent = msg;
    el.className = 'hitl-apply-feedback' + (isError ? ' hitl-apply-feedback--error' : '');
}

async function refreshHitlAuditStrategy() {
    const approvalTa = document.getElementById('hitl-audit-agent-prompt');
    const reviewTa = document.getElementById('hitl-audit-agent-prompt-review-edit');
    if (!approvalTa) return;
    try {
        const resp = await hitlApiFetch('/api/hitl/audit-strategy', { credentials: 'same-origin' });
        if (!resp.ok) return;
        const data = await resp.json();
        hitlDefaultAuditPrompt = typeof data.defaultAuditAgentPrompt === 'string' ? data.defaultAuditAgentPrompt : '';
        hitlDefaultAuditPromptReviewEdit = typeof data.defaultAuditAgentPromptReviewEdit === 'string' ? data.defaultAuditAgentPromptReviewEdit : '';
        approvalTa.value = typeof data.auditAgentPrompt === 'string' ? data.auditAgentPrompt : hitlDefaultAuditPrompt;
        if (reviewTa) {
            reviewTa.value = typeof data.auditAgentPromptReviewEdit === 'string' ? data.auditAgentPromptReviewEdit : hitlDefaultAuditPromptReviewEdit;
        }
        switchHitlStrategyMode(hitlStrategyMode);
    } catch (e) {
        console.warn('refreshHitlAuditStrategy', e);
    }
}

function renderHitlStrategyJevHint() {
    let el = document.getElementById('hitl-strategy-hint-jev');
    const bar = document.querySelector('.hitl-page-strategy-bar') || document.getElementById('hitl-page-strategy-bar');
    if (!el && bar) {
        el = document.createElement('p');
        el.className = 'hitl-page-strategy-hint';
        el.id = 'hitl-strategy-hint-jev';
        const reviewHint = document.getElementById('hitl-strategy-hint-review-edit');
        if (reviewHint && reviewHint.parentNode) reviewHint.parentNode.insertBefore(el, reviewHint.nextSibling);
        else bar.appendChild(el);
    }
    if (!el) return;
    const ts = hitlCurrentAuditEngine().backend === 'typesafe';
    el.hidden = !ts;
    if (ts) el.textContent = hitlT('strategyHintJev', 'TypeSafe Jev evaluates the custom strategy as structured questions. Built-in destructive rules remain a hard floor.');
}

async function saveHitlAuditStrategy() {
    const approvalTa = document.getElementById('hitl-audit-agent-prompt');
    const reviewTa = document.getElementById('hitl-audit-agent-prompt-review-edit');
    const btn = document.getElementById('hitl-strategy-save-btn');
    if (!approvalTa) return;
    showHitlStrategyFeedback('', false);
    if (btn) btn.disabled = true;
    try {
        const resp = await hitlApiFetch('/api/hitl/audit-strategy', {
            method: 'PUT',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                auditAgentPrompt: String(approvalTa.value || ''),
                auditAgentPromptReviewEdit: reviewTa ? String(reviewTa.value || '') : ''
            })
        });
        if (!resp.ok) throw new Error(await readHitlApiError(resp));
        const data = await resp.json();
        if (typeof data.auditAgentPrompt === 'string') approvalTa.value = data.auditAgentPrompt;
        if (reviewTa && typeof data.auditAgentPromptReviewEdit === 'string') reviewTa.value = data.auditAgentPromptReviewEdit;
        showHitlStrategyFeedback(hitlT('strategySaved', 'Audit strategy saved.'), false);
    } catch (e) {
        showHitlStrategyFeedback(hitlT('strategySaveFailed', 'Failed to save') + ': ' + (e.message || e), true);
    } finally {
        if (btn) btn.disabled = false;
    }
}

function resetHitlAuditStrategy() {
    const approvalTa = document.getElementById('hitl-audit-agent-prompt');
    const reviewTa = document.getElementById('hitl-audit-agent-prompt-review-edit');
    if (hitlStrategyMode === 'review_edit' && reviewTa) {
        reviewTa.value = hitlDefaultAuditPromptReviewEdit || reviewTa.value;
    } else if (approvalTa) {
        approvalTa.value = hitlDefaultAuditPrompt || approvalTa.value;
    }
    showHitlStrategyFeedback('', false);
}

function refreshHitlActivePanel() {
    refreshHitlPageReviewerBar();
    if (hitlActiveTab === 'logs') {
        refreshHitlLogs();
    } else if (hitlActiveTab === 'strategy') {
        refreshHitlAuditStrategy();
    } else if (hitlActiveTab === 'whitelist') {
        refreshHitlPageWhitelist();
    } else {
        refreshHitlPending();
    }
}

function hitlDecidedByLabel(v) {
    const map = {
        human: hitlT('reviewerHuman', 'Human'),
        audit_agent: hitlT('reviewerAgent', 'Audit Agent'),
        system: hitlT('reviewerSystem', 'System'),
        manual: hitlT('reviewerManual', 'Manual')
    };
    return map[v] || v || '-';
}

function hitlNormalizeAuditBackend(v) {
    const s = String(v || '').trim().toLowerCase();
    if (s === 'typesafe' || s === 'jev' || s === 'type-safe' || s === 'typesafe-ai') return 'typesafe';
    if (s === 'openai' || s === 'openai_compatible' || s === 'llm') return 'openai';
    return '';
}

function hitlCurrentAuditEngine() {
    const cfg = (typeof window !== 'undefined' && window.csaiHitlDefaultConfig) || {};
    const backend = hitlNormalizeAuditBackend(cfg.auditBackend || (typeof window !== 'undefined' && window.csaiHitlAuditBackend));
    let model = String(cfg.auditModel || (typeof window !== 'undefined' && window.csaiHitlAuditModel) || '').trim();
    if (backend === 'typesafe' && !model) model = 'jev-latest';
    return { backend: backend || 'openai', model: model };
}

function hitlAuditEngineLabel(backend, model) {
    const b = hitlNormalizeAuditBackend(backend);
    if (!b) return '';
    const name = b === 'typesafe'
        ? hitlT('auditEngineJev', 'TypeSafe Jev')
        : hitlT('auditEngineOpenAI', 'OpenAI protocol');
    const m = String(model || '').trim();
    return m ? (name + ' · ' + m) : name;
}

function hitlAuditEngineFromItem(item) {
    const data = item && typeof item === 'object' ? item : {};
    let backend = hitlNormalizeAuditBackend(data.auditBackend || data.audit_backend);
    let model = String(data.auditModel || data.audit_model || '').trim();
    if (!backend) {
        const payload = typeof window.hitlParsePayloadObject === 'function'
            ? hitlParsePayloadObject(data.payload || '')
            : {};
        const approval = payload && payload.hitlApproval && typeof payload.hitlApproval === 'object'
            ? payload.hitlApproval
            : {};
        backend = hitlNormalizeAuditBackend(approval.auditBackend || approval.audit_backend);
        if (!model) model = String(approval.auditModel || approval.audit_model || '').trim();
    }
    if (!backend) {
        const comment = String(data.comment || '');
        if (/TypeSafe|破坏分|choice=|Jev/i.test(comment)) backend = 'typesafe';
        else if (hitlReviewerNormalize(data.decidedBy || data.decided_by) === 'audit_agent') backend = 'openai';
    }
    if (backend === 'typesafe' && !model) model = 'jev-latest';
    return { backend: backend, model: model };
}

function ensureHitlPageAuditEngineEl() {
    let el = document.getElementById('hitl-page-audit-engine');
    if (el) return el;
    const bar = document.getElementById('hitl-page-reviewer-bar');
    if (!bar) return null;
    el = document.createElement('p');
    el.className = 'hitl-page-audit-engine';
    el.id = 'hitl-page-audit-engine';
    el.hidden = true;
    const hint = bar.querySelector('.hitl-page-reviewer-hint');
    if (hint) bar.insertBefore(el, hint);
    else bar.appendChild(el);
    return el;
}

function renderHitlPageAuditEngine() {
    const el = ensureHitlPageAuditEngineEl();
    if (!el) return;
    const info = hitlCurrentAuditEngine();
    const engine = hitlAuditEngineLabel(info.backend, info.model);
    if (!engine) {
        el.hidden = true;
        el.textContent = '';
        return;
    }
    el.hidden = false;
    el.textContent = hitlT('auditEngineLabel', 'Approval engine') + '：' + engine;
}

function hitlFormatTime(v) {
    if (!v) return '-';
    try {
        const d = new Date(v);
        if (Number.isNaN(d.getTime())) return String(v);
        return d.toLocaleString(hitlLocale(), {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false
        });
    } catch (e) {
        return String(v);
    }
}

const HITL_LOG_FILTER_SELECT_IDS = ['hitl-logs-decision-filter', 'hitl-logs-decidedby-filter'];
const hitlLogFilterSelectMap = {};
let hitlLogFilterSelectDocBound = false;

const HITL_FILTER_SELECT_CARET = '<svg class="hitl-filter-select-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function closeAllHitlLogFilterSelects() {
    Object.keys(hitlLogFilterSelectMap).forEach(function (id) {
        const reg = hitlLogFilterSelectMap[id];
        if (!reg || !reg.wrapper) return;
        reg.wrapper.classList.remove('open');
        if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    });
}

function syncHitlLogFilterSelect(selectId) {
    const reg = hitlLogFilterSelectMap[selectId];
    if (!reg) return;
    const select = reg.select;
    const dropdown = reg.dropdown;
    const trigger = reg.trigger;
    const valueSpan = trigger.querySelector('.hitl-filter-select-value');

    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'hitl-filter-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        } else {
            item.setAttribute('aria-selected', 'false');
        }
        const check = document.createElement('span');
        check.className = 'hitl-filter-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'hitl-filter-select-label';
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

function syncAllHitlLogFilterSelects() {
    HITL_LOG_FILTER_SELECT_IDS.forEach(syncHitlLogFilterSelect);
}

function enhanceHitlLogFilterSelect(selectId) {
    const select = document.getElementById(selectId);
    if (!select) return;
    if (select.dataset.hitlCustomSelect === '1') {
        syncHitlLogFilterSelect(selectId);
        return;
    }
    select.dataset.hitlCustomSelect = '1';
    select.classList.add('hitl-filter-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'hitl-filter-select-ui';
    if (selectId === 'hitl-logs-decision-filter') {
        wrapper.classList.add('hitl-filter-select-ui--decision');
    } else if (selectId === 'hitl-logs-decidedby-filter') {
        wrapper.classList.add('hitl-filter-select-ui--decidedby');
    }

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'hitl-filter-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'hitl-filter-select-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', HITL_FILTER_SELECT_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'hitl-filter-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    hitlLogFilterSelectMap[selectId] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        const open = wrapper.classList.contains('open');
        closeAllHitlLogFilterSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
        }
    });

    dropdown.addEventListener('click', function (e) {
        const opt = e.target.closest('.hitl-filter-select-option');
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
        syncHitlLogFilterSelect(selectId);
    });

    select.addEventListener('change', function () {
        syncHitlLogFilterSelect(selectId);
    });
}

function initHitlLogFilterSelects() {
    if (!hitlLogFilterSelectDocBound) {
        document.addEventListener('click', closeAllHitlLogFilterSelects);
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllHitlLogFilterSelects();
        });
        hitlLogFilterSelectDocBound = true;
    }
    HITL_LOG_FILTER_SELECT_IDS.forEach(function (id) {
        enhanceHitlLogFilterSelect(id);
        const select = document.getElementById(id);
        if (select && !select.dataset.hitlFilterBound) {
            select.dataset.hitlFilterBound = '1';
            select.addEventListener('change', filterHitlLogs);
        }
    });
    syncAllHitlLogFilterSelects();
}

function hitlLogsHasActiveFilters() {
    const qEl = document.getElementById('hitl-logs-search');
    const decEl = document.getElementById('hitl-logs-decision-filter');
    const byEl = document.getElementById('hitl-logs-decidedby-filter');
    return Boolean(
        (qEl && qEl.value.trim()) ||
        (decEl && decEl.value && decEl.value !== 'all') ||
        (byEl && byEl.value && byEl.value !== 'all')
    );
}

function hitlLogsFilterParams() {
    const params = new URLSearchParams();
    const qEl = document.getElementById('hitl-logs-search');
    const decEl = document.getElementById('hitl-logs-decision-filter');
    const byEl = document.getElementById('hitl-logs-decidedby-filter');
    if (qEl && qEl.value.trim()) params.set('q', qEl.value.trim());
    if (decEl && decEl.value && decEl.value !== 'all') params.set('decision', decEl.value);
    if (byEl && byEl.value && byEl.value !== 'all') params.set('decidedBy', byEl.value);
    return params;
}

function updateHitlLogsRetentionHint() {
    const el = document.getElementById('hitl-logs-retention-hint');
    if (!el) return;
    if (typeof hitlLogsRetentionDays === 'number' && hitlLogsRetentionDays > 0) {
        el.textContent = hitlT('retentionHint', 'Audit logs are kept for {{days}} days, then purged automatically.', { days: hitlLogsRetentionDays });
        el.hidden = false;
    } else {
        el.textContent = '';
        el.hidden = true;
    }
}

function updateHitlLogsBatchActionsState() {
    const selectedCount = hitlSelectedLogs.size;
    const batchActions = document.getElementById('hitl-logs-batch-actions');
    const selectedCountSpan = document.getElementById('hitl-logs-selected-count');
    if (batchActions) {
        batchActions.style.display = selectedCount > 0 ? 'flex' : 'none';
    }
    if (selectedCountSpan) {
        selectedCountSpan.textContent = hitlT('selectedCount', '{{count}} selected', { count: selectedCount });
    }
    const selectAllCheckbox = document.getElementById('hitl-logs-select-all');
    if (selectAllCheckbox) {
        const allCheckboxes = document.querySelectorAll('.hitl-log-checkbox');
        if (allCheckboxes.length === 0) {
            selectAllCheckbox.checked = false;
            selectAllCheckbox.indeterminate = false;
        } else {
            const checkedOnPage = Array.from(allCheckboxes).filter(function (cb) {
                return hitlSelectedLogs.has(cb.value);
            }).length;
            selectAllCheckbox.checked = checkedOnPage === allCheckboxes.length;
            selectAllCheckbox.indeterminate = checkedOnPage > 0 && checkedOnPage < allCheckboxes.length;
        }
    }
}

function toggleHitlLogSelection(id, checked) {
    if (!id) return;
    if (checked) {
        hitlSelectedLogs.add(id);
    } else {
        hitlSelectedLogs.delete(id);
    }
    updateHitlLogsBatchActionsState();
}

function toggleHitlLogsSelectAll(checkbox) {
    const checkboxes = document.querySelectorAll('.hitl-log-checkbox');
    checkboxes.forEach(function (cb) {
        cb.checked = checkbox.checked;
        if (checkbox.checked) {
            hitlSelectedLogs.add(cb.value);
        } else {
            hitlSelectedLogs.delete(cb.value);
        }
    });
    updateHitlLogsBatchActionsState();
}

function selectAllHitlLogs() {
    const checkboxes = document.querySelectorAll('.hitl-log-checkbox');
    checkboxes.forEach(function (cb) {
        cb.checked = true;
        hitlSelectedLogs.add(cb.value);
    });
    const selectAllCheckbox = document.getElementById('hitl-logs-select-all');
    if (selectAllCheckbox) {
        selectAllCheckbox.checked = true;
        selectAllCheckbox.indeterminate = false;
    }
    updateHitlLogsBatchActionsState();
}

function deselectAllHitlLogs() {
    const checkboxes = document.querySelectorAll('.hitl-log-checkbox');
    checkboxes.forEach(function (cb) {
        cb.checked = false;
    });
    hitlSelectedLogs.clear();
    const selectAllCheckbox = document.getElementById('hitl-logs-select-all');
    if (selectAllCheckbox) {
        selectAllCheckbox.checked = false;
        selectAllCheckbox.indeterminate = false;
    }
    updateHitlLogsBatchActionsState();
}

async function batchDeleteHitlLogs() {
    const ids = Array.from(hitlSelectedLogs);
    if (!ids.length) {
        alert(hitlT('selectLogsFirst', 'Select audit logs to delete first'));
        return;
    }
    const count = ids.length;
    if (!confirm(hitlT('batchDeleteConfirm', 'Delete the selected {{count}} audit log(s)? This cannot be undone.', { count: count }))) {
        return;
    }
    try {
        const resp = await hitlApiFetch('/api/hitl/logs', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin',
            body: JSON.stringify({ ids: ids })
        });
        if (!resp.ok) {
            const err = await resp.json().catch(function () { return {}; });
            throw new Error(err.error || hitlT('batchDeleteFailed', 'Batch delete failed'));
        }
        const result = await resp.json().catch(function () { return {}; });
        const deletedCount = typeof result.deleted === 'number' ? result.deleted : count;
        ids.forEach(function (id) { hitlSelectedLogs.delete(id); });
        await refreshHitlLogs();
        alert(hitlT('batchDeleteSuccess', 'Successfully deleted {{count}} audit log(s)', { count: deletedCount }));
    } catch (e) {
        console.error('batchDeleteHitlLogs', e);
        alert(hitlT('batchDeleteFailed', 'Batch delete failed') + ': ' + (e && e.message ? e.message : String(e)));
    }
}

async function clearHitlLogs() {
    const count = hitlLogsTotal || 0;
    if (count <= 0) {
        return;
    }
    const confirmKey = hitlLogsHasActiveFilters() ? 'clearAllConfirm' : 'clearAllConfirmNoFilter';
    if (!confirm(hitlT(confirmKey, 'Clear all {{count}} audit log(s)? This cannot be undone.', { count: count }))) {
        return;
    }
    try {
        const params = hitlLogsFilterParams();
        const resp = await hitlApiFetch('/api/hitl/logs' + (params.toString() ? '?' + params.toString() : ''), {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin',
            body: JSON.stringify({ all: true })
        });
        if (!resp.ok) {
            const err = await resp.json().catch(function () { return {}; });
            throw new Error(err.error || hitlT('clearAllFailed', 'Clear failed'));
        }
        const result = await resp.json().catch(function () { return {}; });
        const deletedCount = typeof result.deleted === 'number' ? result.deleted : count;
        hitlSelectedLogs.clear();
        hitlLogsPage = 1;
        await refreshHitlLogs();
        alert(hitlT('clearAllSuccess', 'Cleared {{count}} audit log(s)', { count: deletedCount }));
    } catch (e) {
        console.error('clearHitlLogs', e);
        alert(hitlT('clearAllFailed', 'Clear failed') + ': ' + (e && e.message ? e.message : String(e)));
    }
}

function renderHitlLogsTable(items) {
    const wrap = document.getElementById('hitl-logs-table-wrap');
    if (!wrap) return;
    const list = Array.isArray(items) ? items : [];
    if (!list.length) {
        wrap.innerHTML =
            '<div class="empty-state">' +
            '<p>' + escapeHtml(hitlT('logsEmpty', 'No audit logs')) + '</p>' +
            '<p class="hitl-logs-empty-hint">' + escapeHtml(hitlT('logsEmptyHint', 'Records appear here after HITL decisions.')) + '</p>' +
            '</div>';
        const batchActions = document.getElementById('hitl-logs-batch-actions');
        if (batchActions) batchActions.style.display = 'none';
        renderHitlLogsPagination();
        return;
    }
    const rows = list.map(function (item) {
            const rawId = String(item.id || '');
            const id = escapeHtml(rawId);
            const jsId = rawId.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
            const qId = JSON.stringify(rawId).replace(/"/g, '&quot;');
            const isSelected = hitlSelectedLogs.has(rawId);
            const payloadObj = hitlParsePayloadObject(item.payload || '');
            const decision = String(item.decision || '-');
            const decisionCls = decision === 'approve' ? 'hitl-decision--approve' : (decision === 'reject' ? 'hitl-decision--reject' : '');
            const summary = hitlPayloadSummary(payloadObj);
            return (
                '<tr>' +
                '<td><input type="checkbox" class="hitl-log-checkbox" value="' + id + '" ' + (isSelected ? 'checked' : '') + ' onchange="toggleHitlLogSelection(\'' + jsId + '\', this.checked)" /></td>' +
                '<td class="hitl-logs-cell-mono">' + id + '</td>' +
                '<td>' + escapeHtml(String(item.toolName || '-')) + '</td>' +
                '<td class="hitl-logs-cell-mono">' + escapeHtml(String(item.conversationId || '-')) + '</td>' +
                '<td><span class="hitl-decision-tag ' + decisionCls + '">' + escapeHtml(hitlDecisionLabel(decision)) + '</span></td>' +
                '<td>' + escapeHtml(hitlDecidedByLabel(item.decidedBy)) + (function () {
                    const engine = hitlAuditEngineFromItem(item);
                    const label = hitlAuditEngineLabel(engine.backend, engine.model);
                    return label ? '<div class="hitl-log-engine">' + escapeHtml(label) + '</div>' : '';
                }()) + '</td>' +
                '<td class="hitl-logs-summary">' + escapeHtml(summary) + '</td>' +
                '<td>' + escapeHtml(hitlFormatTime(item.decidedAt || item.createdAt)) + '</td>' +
                '<td class="hitl-logs-actions">' +
                '<button type="button" class="btn-link" onclick="openHitlLogModal(' + qId + ')">' + escapeHtml(hitlT('viewDetail', 'Detail')) + '</button>' +
                '</td>' +
                '</tr>'
            );
        }).join('');
    wrap.innerHTML =
        '<table class="hitl-logs-table">' +
        '<thead><tr>' +
        '<th><input type="checkbox" id="hitl-logs-select-all" onchange="toggleHitlLogsSelectAll(this)" aria-label="select all" /></th>' +
        '<th>' + escapeHtml(hitlT('colId', 'ID')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colTool', 'Tool')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colConversation', 'Conversation')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colDecision', 'Decision')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colDecidedBy', 'Reviewer')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colContext', 'Context')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colTime', 'Time')) + '</th>' +
        '<th>' + escapeHtml(hitlT('colActions', 'Actions')) + '</th>' +
        '</tr></thead><tbody>' + rows + '</tbody></table>';
    updateHitlLogsBatchActionsState();
    renderHitlLogsPagination();
}

async function refreshHitlLogs() {
    const wrap = document.getElementById('hitl-logs-table-wrap');
    if (!wrap) return;
    wrap.innerHTML = '<div class="loading-spinner">' + escapeHtml(hitlT('loading', 'Loading...')) + '</div>';
    try {
        const params = new URLSearchParams({
            page: String(hitlLogsPage),
            pageSize: String(hitlLogsPageSize)
        });
        const filterParams = hitlLogsFilterParams();
        filterParams.forEach(function (value, key) { params.set(key, value); });
        const resp = await hitlApiFetch('/api/hitl/logs?' + params.toString(), { credentials: 'same-origin' });
        if (!resp.ok) throw new Error('request failed');
        const data = await resp.json();
        const items = Array.isArray(data.items) ? data.items : [];
        hitlLogsTotal = typeof data.total === 'number' ? data.total : items.length;
        hitlLogsRetentionDays = typeof data.retentionDays === 'number' ? data.retentionDays : 0;
        updateHitlLogsRetentionHint();
        const maxPage = Math.max(1, Math.ceil(hitlLogsTotal / hitlLogsPageSize));
        if (hitlLogsPage > maxPage) {
            hitlLogsPage = maxPage;
            await refreshHitlLogs();
            return;
        }
        hitlLogsCache = items;
        hitlLogsLoaded = true;
        renderHitlLogsTable(items);
    } catch (e) {
        hitlLogsLoaded = false;
        wrap.innerHTML = '<div class="empty-state">' + escapeHtml(hitlT('loadFailed', 'Failed to load')) + '</div>';
        renderHitlLogsPagination();
    }
}

function filterHitlLogs() {
    hitlLogsPage = 1;
    refreshHitlLogs();
}

function refreshHitlLogsI18n() {
    if (!document.getElementById('hitl-logs-table-wrap') || !hitlLogsLoaded) return;
    updateHitlLogsRetentionHint();
    renderHitlLogsTable(hitlLogsCache);
}

function refreshHitlPendingI18n() {
    if (!document.getElementById('hitl-pending-list') || !hitlPendingLoaded) return;
    refreshHitlPending();
}

function refreshHitlI18n() {
    refreshHitlLogsI18n();
    refreshHitlPendingI18n();
    syncAllHitlLogFilterSelects();
    renderHitlLogsPagination();
    renderHitlPendingPagination();
    renderHitlPageAuditEngine();
    renderHitlStrategyJevHint();
}

function renderHitlLogsPagination() {
    renderHitlPagination('hitl-logs-pagination', {
        total: hitlLogsTotal,
        page: hitlLogsPage,
        pageSize: hitlLogsPageSize
    }, 'hitlLogsGoPage', 'onHitlLogsPageSizeChange', 'hitl-logs-page-size');
}

function renderHitlPendingPagination() {
    renderHitlPagination('hitl-pending-pagination', {
        total: hitlPendingTotal,
        page: hitlPendingPage,
        pageSize: hitlPendingPageSize
    }, 'hitlPendingGoPage', 'onHitlPendingPageSizeChange', 'hitl-pending-page-size');
}

function onHitlLogsPageSizeChange() {
    const sel = document.getElementById('hitl-logs-page-size');
    if (!sel) return;
    const n = parseInt(sel.value, 10);
    if (HITL_PAGE_SIZE_OPTIONS.indexOf(n) < 0) return;
    hitlLogsPageSize = n;
    try {
        localStorage.setItem(HITL_LOGS_PAGE_SIZE_KEY, String(n));
    } catch (e) { /* ignore */ }
    hitlLogsPage = 1;
    refreshHitlLogs();
}

function onHitlPendingPageSizeChange() {
    const sel = document.getElementById('hitl-pending-page-size');
    if (!sel) return;
    const n = parseInt(sel.value, 10);
    if (HITL_PAGE_SIZE_OPTIONS.indexOf(n) < 0) return;
    hitlPendingPageSize = n;
    try {
        localStorage.setItem(HITL_PENDING_PAGE_SIZE_KEY, String(n));
    } catch (e) { /* ignore */ }
    hitlPendingPage = 1;
    refreshHitlPending();
}

function hitlLogsGoPage(page) {
    const totalPages = Math.max(1, Math.ceil((hitlLogsTotal || 0) / (hitlLogsPageSize || 20)));
    if (page < 1 || page > totalPages) return;
    hitlLogsPage = page;
    refreshHitlLogs();
}

function hitlPendingGoPage(page) {
    const totalPages = Math.max(1, Math.ceil((hitlPendingTotal || 0) / (hitlPendingPageSize || 20)));
    if (page < 1 || page > totalPages) return;
    hitlPendingPage = page;
    refreshHitlPending();
}

function hitlDecisionLabel(decision) {
    const d = String(decision || '').toLowerCase();
    if (d === 'approve') return hitlT('decisionApprove', 'Approve');
    if (d === 'reject') return hitlT('decisionReject', 'Reject');
    return decision || '—';
}

function hitlFormatPayloadForDisplay(raw) {
    if (!raw) return '';
    if (typeof raw === 'object') {
        try {
            return JSON.stringify(raw, null, 2);
        } catch (e) {
            return String(raw);
        }
    }
    const s = String(raw).trim();
    if (!s) return '';
    try {
        return JSON.stringify(JSON.parse(s), null, 2);
    } catch (e) {
        return s;
    }
}

async function openHitlLogModal(idOpt) {
    const modal = document.getElementById('hitl-log-modal');
    if (!modal || !idOpt) return;
    const resp = await hitlApiFetch('/api/hitl/logs/' + encodeURIComponent(idOpt), { credentials: 'same-origin' });
    if (!resp.ok) {
        alert(hitlT('loadFailed', 'Failed to load'));
        return;
    }
    const item = await resp.json();
    const payloadObj = hitlParsePayloadObject(item.payload || '');
    const idEl = document.getElementById('hitl-log-detail-id');
    const toolEl = document.getElementById('hitl-log-detail-tool');
    const convEl = document.getElementById('hitl-log-detail-conversation');
    const decisionEl = document.getElementById('hitl-log-detail-decision');
    const decidedByEl = document.getElementById('hitl-log-detail-decided-by');
    const timeEl = document.getElementById('hitl-log-detail-time');
    const commentRow = document.getElementById('hitl-log-detail-comment-row');
    const commentEl = document.getElementById('hitl-log-detail-comment');
    const payloadWrap = document.getElementById('hitl-log-detail-payload-wrap');
    const payloadEl = document.getElementById('hitl-log-detail-payload');
    if (idEl) idEl.textContent = item.id || '—';
    if (toolEl) toolEl.textContent = item.toolName || '—';
    if (convEl) convEl.textContent = item.conversationId || '—';
    if (decisionEl) {
        const decision = String(item.decision || '');
        const cls = decision === 'approve' ? 'hitl-decision--approve' : (decision === 'reject' ? 'hitl-decision--reject' : '');
        decisionEl.innerHTML = '<span class="hitl-decision-tag ' + cls + '">' + escapeHtml(hitlDecisionLabel(decision)) + '</span>';
    }
    if (decidedByEl) decidedByEl.textContent = hitlDecidedByLabel(item.decidedBy);
    let engineRow = document.getElementById('hitl-log-detail-engine-row');
    let engineEl = document.getElementById('hitl-log-detail-engine');
    if (!engineRow || !engineEl) {
        const decidedRow = decidedByEl && decidedByEl.closest('.hitl-log-detail-row');
        const dl = decidedRow && decidedRow.parentElement;
        if (dl && decidedRow) {
            engineRow = document.createElement('div');
            engineRow.className = 'hitl-log-detail-row';
            engineRow.id = 'hitl-log-detail-engine-row';
            engineRow.hidden = true;
            engineRow.innerHTML = '<dt>' + escapeHtml(hitlT('colAuditEngine', 'Approval engine')) + '</dt><dd id="hitl-log-detail-engine">—</dd>';
            if (decidedRow.nextSibling) dl.insertBefore(engineRow, decidedRow.nextSibling);
            else dl.appendChild(engineRow);
            engineEl = document.getElementById('hitl-log-detail-engine');
        }
    }
    if (engineRow && engineEl) {
        const engine = hitlAuditEngineFromItem(item);
        const label = hitlAuditEngineLabel(engine.backend, engine.model);
        if (label) {
            engineEl.textContent = label;
            engineRow.hidden = false;
        } else {
            engineEl.textContent = '';
            engineRow.hidden = true;
        }
    }
    if (timeEl) timeEl.textContent = hitlFormatTime(item.decidedAt || item.createdAt);
    const comment = String(item.comment || '').trim();
    if (commentRow && commentEl) {
        if (comment) {
            commentEl.textContent = comment;
            commentRow.hidden = false;
        } else {
            commentEl.textContent = '';
            commentRow.hidden = true;
        }
    }
    hitlFillLogModalReadonlySections(payloadObj);
    const payloadText = hitlFormatPayloadForDisplay(item.payload || '');
    if (payloadWrap && payloadEl) {
        if (payloadText) {
            payloadEl.textContent = payloadText;
            payloadWrap.hidden = false;
        } else {
            payloadEl.textContent = '';
            payloadWrap.hidden = true;
        }
    }
    modal.style.display = 'flex';
}

function closeHitlLogModal() {
    const modal = document.getElementById('hitl-log-modal');
    if (modal) modal.style.display = 'none';
}

window.saveHitlPageWhitelist = saveHitlPageWhitelist;
window.refreshHitlPageWhitelist = refreshHitlPageWhitelist;
window.refreshHitlPending = refreshHitlPending;
window.refreshHitlLogs = refreshHitlLogs;
window.refreshHitlActivePanel = refreshHitlActivePanel;
window.renderHitlPageAuditEngine = renderHitlPageAuditEngine;
window.renderHitlStrategyJevHint = renderHitlStrategyJevHint;
window.switchHitlPageTab = switchHitlPageTab;
window.switchHitlStrategyMode = switchHitlStrategyMode;
window.resetHitlAuditStrategy = resetHitlAuditStrategy;
window.saveHitlAuditStrategy = saveHitlAuditStrategy;
window.refreshHitlAuditStrategy = refreshHitlAuditStrategy;
window.openHitlLogModal = openHitlLogModal;
window.closeHitlLogModal = closeHitlLogModal;
window.hitlLogsGoPage = hitlLogsGoPage;
window.hitlPendingGoPage = hitlPendingGoPage;
window.filterHitlLogs = filterHitlLogs;
window.batchDeleteHitlLogs = batchDeleteHitlLogs;
window.clearHitlLogs = clearHitlLogs;
window.selectAllHitlLogs = selectAllHitlLogs;
window.deselectAllHitlLogs = deselectAllHitlLogs;
window.toggleHitlLogSelection = toggleHitlLogSelection;
window.toggleHitlLogsSelectAll = toggleHitlLogsSelectAll;
window.filterHitlPending = filterHitlPending;
window.onHitlLogsPageSizeChange = onHitlLogsPageSizeChange;
window.onHitlPendingPageSizeChange = onHitlPendingPageSizeChange;
window.submitHitlDecision = submitHitlDecision;
window.submitHitlDecisionWithPayload = submitHitlDecisionWithPayload;
window.submitWorkflowHitlDecisionFromPage = submitWorkflowHitlDecisionFromPage;
window.openHitlConversation = openHitlConversation;
window.dismissHitlItem = dismissHitlItem;
window.followAgentRunAfterHitlDecision = followAgentRunAfterHitlDecision;

window.addEventListener('hitl-interrupt', function () {
    if (typeof window.currentPage === 'function' && window.currentPage() === 'hitl') {
        refreshHitlActivePanel();
    }
});

window.addEventListener('pageshow', function () {
    setTimeout(reconcileHitlUiState, 0);
});
document.addEventListener('DOMContentLoaded', function () {
    initHitlPageSizeFromStorage(HITL_LOGS_PAGE_SIZE_KEY, 20, function (n) { hitlLogsPageSize = n; });
    initHitlPageSizeFromStorage(HITL_PENDING_PAGE_SIZE_KEY, 20, function (n) { hitlPendingPageSize = n; });
    initHitlLogFilterSelects();
    if (typeof window.bindHitlReviewerToggleListeners === 'function') {
        window.bindHitlReviewerToggleListeners();
    }
    window.csaiHitlDefaultConfigReady = initHitlDefaultReviewerFromServer();
    window.csaiHitlDefaultReviewerReady = window.csaiHitlDefaultConfigReady;
    setTimeout(reconcileHitlUiState, 0);
});

document.addEventListener('languagechange', function () {
    try {
        refreshHitlI18n();
    } catch (e) {
        console.warn('languagechange hitl refresh failed', e);
    }
});

// 由 applyHitlSidebarConfig 调用，将侧栏配置同步到后端
window.syncHitlConfigToServerByCurrentConversation = syncHitlConfigToServerByCurrentConversation;
window.saveHitlConversationConfig = saveHitlConversationConfig;
window.mergeHitlGlobalToolWhitelist = mergeHitlGlobalToolWhitelist;
window.fetchHitlDefaultConfig = fetchHitlDefaultConfig;
window.putHitlDefaultConfig = putHitlDefaultConfig;

// 由 chat.js 在 loadConversation 内 await 调用；挂到 window 供其它入口显式触发
window.syncHitlConfigFromServer = syncHitlConfigFromServer;
window.fetchHitlDefaultReviewer = fetchHitlDefaultReviewer;
window.putHitlDefaultReviewer = putHitlDefaultReviewer;
