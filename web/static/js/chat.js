let currentConversationId = null;

/** Persist the visible chat in the URL so a reload can restore and reconnect it. */
function syncChatConversationHash(conversationId) {
    const normalizedConversationId = String(conversationId || '').trim();
    if (!normalizedConversationId || window.location.hash.split('?')[0] !== '#chat') return;
    const targetHash = '#chat?conversation=' + encodeURIComponent(normalizedConversationId);
    if (window.location.hash !== targetHash) {
        window.history.replaceState(null, '', targetHash);
    }
}
window.syncChatConversationHash = syncChatConversationHash;

function clearChatConversationHash() {
    if (window.location.hash.split('?')[0] !== '#chat' || window.location.hash === '#chat') return;
    window.history.replaceState(null, '', '#chat');
}
window.clearChatConversationHash = clearChatConversationHash;
let loadConversationRequestSeq = 0;
let loadConversationAbortController = null;
let loadConversationPendingId = '';
let chatConversationNavigationSeq = 0;

function isChatConversationLoadPending(conversationId) {
    const id = String(conversationId || '').trim();
    return !!id && loadConversationPendingId === id;
}
window.isChatConversationLoadPending = isChatConversationLoadPending;

function markChatConversationNavigation(nextConversationId, force = false) {
    const nextId = String(nextConversationId || '').trim();
    const visibleId = String(currentConversationId || '').trim();
    if (force || nextId !== visibleId) {
        chatConversationNavigationSeq++;
    }
    return chatConversationNavigationSeq;
}

/**
 * 离开聊天页时立即让尚在初始化的发送请求失去页面所有权。
 * 后端任务仍会继续执行；这里只中止浏览器前台流，避免首个 conversation
 * 事件在用户已经切到其他页面后再次抢占当前会话。
 */
function abandonChatConversationForPageNavigation() {
    markChatConversationNavigation('', true);
    if (typeof window.cancelScheduledChatConversationFromHash === 'function') {
        window.cancelScheduledChatConversationFromHash();
    }
    cancelPendingConversationLoad();
    detachLiveChatStreamForNavigation('', true);
}
window.abandonChatConversationForPageNavigation = abandonChatConversationForPageNavigation;

/**
 * 轻量会话 LRU 缓存。
 *
 * 缓存只用作请求失败时的降级数据，不能先于服务端响应直接渲染：
 * 运行中会话的 process details 会持续写入，直接渲染旧快照会让
 * UI 暂时回退到旧轮次，等 task-events 接管后又突然跳到最新轮次。
 */
const CONVERSATION_LITE_CACHE_MAX = 12;
const conversationLiteCache = new Map();

function getConversationLiteFromCache(conversationId) {
    if (!conversationId) return null;
    const hit = conversationLiteCache.get(conversationId);
    if (!hit) return null;
    conversationLiteCache.delete(conversationId);
    conversationLiteCache.set(conversationId, hit);
    return hit;
}

function putConversationLiteCache(conversationId, data) {
    if (!conversationId || !data) return;
    conversationLiteCache.delete(conversationId);
    conversationLiteCache.set(conversationId, data);
    while (conversationLiteCache.size > CONVERSATION_LITE_CACHE_MAX) {
        const oldest = conversationLiteCache.keys().next().value;
        conversationLiteCache.delete(oldest);
    }
}

function invalidateConversationLiteCache(conversationId) {
    if (conversationId) {
        conversationLiteCache.delete(conversationId);
    } else {
        conversationLiteCache.clear();
    }
}

window.invalidateConversationLiteCache = invalidateConversationLiteCache;

// @ 提及相关状态
let mentionTools = [];
let mentionToolsLoaded = false;
let mentionToolsLoadingPromise = null;
let mentionSuggestionsEl = null;
let mentionFilteredTools = [];
let externalMcpNames = []; // 外部MCP名称列表
const mentionState = {
    active: false,
    startIndex: -1,
    query: '',
    selectedIndex: 0,
};

// IME输入法状态跟踪
let isComposing = false;
let compositionEndTimer = null;

// 输入框草稿保存相关
const DRAFT_STORAGE_KEY = 'cyberstrike-chat-draft';
const RECENT_CONVERSATIONS_EXPANDED_KEY = 'cyberstrike-chat-recent-conversations-expanded';
let draftSaveTimer = null;
const DRAFT_SAVE_DELAY = 500; // 500ms防抖延迟

// 对话文件上传相关（后端会拼接路径与内容发给大模型，前端不再重复发文件列表）
const MAX_CHAT_FILES = 10;
const CHAT_FILE_DEFAULT_PROMPT = '请根据上传的文件内容进行分析。';
/** 与 handler.formatInterruptContinueUserMessage 首段一致；主对话不展示，仅迭代详情（user_interrupt_continue） */
const CHAT_INTERRUPT_CONTINUE_USER_PREFIX = '【用户补充 / 中断后继续】';
function isInterruptContinueInjectChatMessage(content) {
    return typeof content === 'string' && content.trimStart().startsWith(CHAT_INTERRUPT_CONTINUE_USER_PREFIX);
}
/**
 * 对话附件：选文件后异步 POST /api/chat-uploads，发送时只传 serverPath（绝对路径），请求体不再内联大文件内容。
 * @type {{ id: number, fileName: string, mimeType: string, serverPath: string|null, uploading: boolean, uploadPercent: number, uploadPromise: Promise<void>|null, uploadError: string|null }[]}
 */
let chatAttachments = [];
let chatAttachmentSeq = 0;

// 对话模式：eino_single = Eino ADK 单代理（/api/eino-agent/stream）；deep / plan_execute / supervisor = Eino 多代理（/api/multi-agent/stream，请求体 orchestration）
const AGENT_MODE_STORAGE_KEY = 'cyberstrike-chat-agent-mode';
const AGENT_MODE_CONVERSATION_STORAGE_PREFIX = 'cyberstrike-chat-agent-mode:conversation';
const AI_CHANNEL_STORAGE_KEY = 'cyberstrike-chat-ai-channel';
const REASONING_MODE_LS = 'cyberstrike-chat-reasoning-mode';
const REASONING_EFFORT_LS = 'cyberstrike-chat-reasoning-effort';
const CHAT_AI_CHANNEL_SUMMARY_NAME_MAX = 10;
const CHAT_AGENT_MODE_EINO_SINGLE = 'eino_single';
const CHAT_AGENT_EINO_MODES = ['deep', 'plan_execute', 'supervisor'];
let multiAgentAPIEnabled = false;
let chatAIChannels = {};
let chatDefaultAIChannel = '';
let chatAIChannelIdByNormalizedId = {};
let chatHitlAuditModelName = '';
let chatHitlAuditBackend = '';
let chatSystemModelRequestSeq = 0;
let chatSystemModelSaving = false;
let chatSystemModelCloseTimer = null;
let chatSystemModelOptions = [];
let chatSystemModelCurrent = '';
let chatSystemModelLoadError = '';
const CHAT_SYSTEM_MODEL_CACHE_TTL_MS = 5 * 60 * 1000;
const chatSystemModelCache = new Map();

// 人机协同（HITL）会话级配置
const HITL_STORAGE_PREFIX = 'cyberstrike-chat-hitl';
const HITL_MODE_OFF = 'off';
const HITL_MODE_APPROVAL = 'approval';
const HITL_MODE_REVIEW_EDIT = 'review_edit';
const HITL_MODE_OPTIONS = [HITL_MODE_OFF, HITL_MODE_APPROVAL, HITL_MODE_REVIEW_EDIT];
const DEFAULT_HITL_TIMEOUT_SECONDS = 300;
// Agent orchestration/control tools are safe baseline exemptions for every
// conversation. Keep this separate from config.tool_whitelist: the latter is
// enforced globally by the backend and must not be copied into this field.
const DEFAULT_HITL_SESSION_TOOL_WHITELIST = 'tool_search, skill, task, write_todos, transfer_to_agent, exit, TaskCreate, TaskGet, TaskUpdate, TaskList, upsert_project_fact, get_project_fact';
let hitlApplyFeedbackTimer = null;
let hitlAutoSaveTimer = null;
let hitlConfigSyncConversationId = '';
let hitlConfigSyncPromise = Promise.resolve();
const sessionSettingsSelects = new Map();
let sessionSettingsSelectDocBound = false;

function sessionSettingsSelectLabel(option) {
    return option ? (option.textContent || option.label || option.value || '') : '';
}

function syncSessionSettingsSelect(select) {
    const reg = sessionSettingsSelects.get(select);
    if (!reg) return;
    const selected = select.options[select.selectedIndex];
    reg.value.textContent = sessionSettingsSelectLabel(selected);
    reg.trigger.disabled = !!select.disabled;
    reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
    reg.menu.innerHTML = '';

    Array.prototype.forEach.call(select.options, function (option, index) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'session-settings-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-index', String(index));
        item.setAttribute('aria-selected', option.selected ? 'true' : 'false');
        item.disabled = !!option.disabled;
        item.classList.toggle('is-selected', !!option.selected);

        const label = document.createElement('span');
        label.className = 'session-settings-select-option-label';
        label.textContent = sessionSettingsSelectLabel(option);
        item.appendChild(label);
        reg.menu.appendChild(item);
    });
}

function closeSessionSettingsSelect(select) {
    const reg = sessionSettingsSelects.get(select);
    if (!reg) return;
    reg.wrapper.classList.remove('open');
    reg.trigger.setAttribute('aria-expanded', 'false');
}

function closeAllSessionSettingsSelects() {
    sessionSettingsSelects.forEach(function (_reg, select) {
        closeSessionSettingsSelect(select);
    });
}

function enhanceSessionSettingsSelect(select) {
    if (!select || select.dataset.sessionSettingsSelect === '1') {
        if (select) syncSessionSettingsSelect(select);
        return;
    }
    const panel = select.closest && select.closest('.conversation-reasoning-card');
    if (!panel) return;

    select.dataset.sessionSettingsSelect = '1';
    select.classList.add('session-settings-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'session-settings-select';
    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'session-settings-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const value = document.createElement('span');
    value.className = 'session-settings-select-value';
    const caret = document.createElement('span');
    caret.className = 'session-settings-select-caret';
    caret.setAttribute('aria-hidden', 'true');
    caret.textContent = '⌄';
    trigger.appendChild(value);
    trigger.appendChild(caret);

    const menu = document.createElement('div');
    menu.className = 'session-settings-select-menu';
    menu.setAttribute('role', 'listbox');

    select.parentNode.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(menu);
    wrapper.appendChild(select);
    sessionSettingsSelects.set(select, { wrapper: wrapper, trigger: trigger, value: value, menu: menu });

    trigger.addEventListener('click', function (event) {
        event.stopPropagation();
        if (select.disabled) return;
        const willOpen = !wrapper.classList.contains('open');
        closeAllSessionSettingsSelects();
        wrapper.classList.toggle('open', willOpen);
        trigger.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
    });

    trigger.addEventListener('keydown', function (event) {
        if (select.disabled) return;
        const enabled = Array.prototype.filter.call(select.options, function (option) { return !option.disabled; });
        if (!enabled.length) return;
        const currentOption = select.options[select.selectedIndex];
        const current = Math.max(0, enabled.indexOf(currentOption));
        let next = current;
        if (event.key === 'ArrowDown') next = Math.min(enabled.length - 1, current + 1);
        else if (event.key === 'ArrowUp') next = Math.max(0, current - 1);
        else if (event.key === 'Home') next = 0;
        else if (event.key === 'End') next = enabled.length - 1;
        else if (event.key === 'Escape') {
            closeSessionSettingsSelect(select);
            return;
        } else if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
            return;
        } else {
            return;
        }
        event.preventDefault();
        const nextOption = enabled[next];
        if (nextOption && select.value !== nextOption.value) {
            select.value = nextOption.value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        syncSessionSettingsSelect(select);
    });

    menu.addEventListener('click', function (event) {
        const item = event.target.closest('.session-settings-select-option');
        if (!item || item.disabled) return;
        event.stopPropagation();
        const option = select.options[Number(item.dataset.index)];
        if (option && !option.disabled && select.value !== option.value) {
            select.value = option.value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        syncSessionSettingsSelect(select);
        closeSessionSettingsSelect(select);
    });

    select.addEventListener('change', function () {
        syncSessionSettingsSelect(select);
    });
    syncSessionSettingsSelect(select);
}

function initSessionSettingsSelects() {
    const panel = document.getElementById('conversation-reasoning-body');
    if (!panel) return;
    panel.querySelectorAll('select').forEach(enhanceSessionSettingsSelect);
    if (!sessionSettingsSelectDocBound) {
        document.addEventListener('click', closeAllSessionSettingsSelects);
        document.addEventListener('keydown', function (event) {
            if (event.key === 'Escape') closeAllSessionSettingsSelects();
        });
        sessionSettingsSelectDocBound = true;
    }
}

function refreshSessionSettingsSelects() {
    sessionSettingsSelects.forEach(function (_reg, select) {
        syncSessionSettingsSelect(select);
    });
}

function syncChatReasoningBarHeight() {
    const reasoning = document.getElementById('chat-reasoning-wrapper');
    const inputBar = document.getElementById('chat-input-container');
    if (!reasoning || !inputBar) return;
    // The composer is now a two-layer surface and is intentionally taller than
    // the sidebar trigger. Do not mirror its height into the settings card.
    reasoning.style.removeProperty('--chat-input-bar-height');
    const chatContainer = inputBar.closest('.chat-container');
    const height = Math.ceil(inputBar.getBoundingClientRect().height || 0);
    if (chatContainer && height > 0) {
        chatContainer.style.setProperty('--chat-composer-total-height', height + 'px');
    }
}

function mountChatSessionSettingsPopover() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const composerSurface = document.querySelector('.chat-composer-surface');
    if (!wrap || !composerSurface) return;
    if (wrap.parentElement !== composerSurface) {
        composerSurface.appendChild(wrap);
    }
    wrap.classList.add('chat-session-settings-popover');
    syncChatSessionSettingsLayerState();
}

function syncChatSessionSettingsLayerState() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const inputBar = document.getElementById('chat-input-container');
    if (!inputBar) return;
    const open = !!wrap && wrap.style.display !== 'none' &&
        !wrap.classList.contains('conversation-reasoning-collapsed');
    inputBar.classList.toggle('is-session-settings-open', open);
}

function initChatReasoningBarHeightSync() {
    mountChatSessionSettingsPopover();
    syncChatReasoningBarHeight();
    window.addEventListener('resize', syncChatReasoningBarHeight);
    const inputBar = document.getElementById('chat-input-container');
    if (inputBar && typeof ResizeObserver !== 'undefined') {
        const ro = new ResizeObserver(syncChatReasoningBarHeight);
        ro.observe(inputBar);
    }
}

/** 非阻塞提示（与 chat-files-toast 样式共用） */
function showChatToast(message, type) {
    const text = message == null ? '' : String(message);
    if (!text) return;
    const el = document.createElement('div');
    el.className = 'chat-files-toast' + (type === 'error' ? ' chat-toast--error' : '');
    el.setAttribute('role', 'status');
    el.textContent = text;
    document.body.appendChild(el);
    requestAnimationFrame(function () {
        el.classList.add('chat-files-toast-visible');
    });
    const hideMs = type === 'error' ? 4500 : 2600;
    setTimeout(function () {
        el.classList.remove('chat-files-toast-visible');
        setTimeout(function () { el.remove(); }, 300);
    }, hideMs);
}
if (typeof window !== 'undefined') {
    window.showChatToast = showChatToast;
}

function normalizeOrchestrationClient(s) {
    const v = String(s || '').trim().toLowerCase().replace(/-/g, '_');
    if (v === 'plan_execute' || v === 'planexecute' || v === 'pe') return 'plan_execute';
    if (v === 'supervisor' || v === 'super' || v === 'sv') return 'supervisor';
    return 'deep';
}

function chatAgentModeIsEino(mode) {
    return CHAT_AGENT_EINO_MODES.indexOf(mode) >= 0;
}

function chatAgentModeIsEinoSingle(mode) {
    return mode === CHAT_AGENT_MODE_EINO_SINGLE;
}

function normalizeHitlMode(mode) {
    let v = String(mode || '').trim().toLowerCase().replace(/-/g, '_');
    if (v === 'feedback' || v === 'followup') {
        v = HITL_MODE_APPROVAL;
    }
    if (HITL_MODE_OPTIONS.includes(v)) return v;
    return HITL_MODE_OFF;
}

function normalizeHitlTimeoutForChat(value, fallback) {
    const n = Number(value);
    if (!Number.isFinite(n)) return fallback;
    return Math.max(0, Math.min(86400, Math.round(n)));
}

function defaultHitlConfig() {
    const serverDefault = (typeof window !== 'undefined' && window.csaiHitlDefaultConfig && typeof window.csaiHitlDefaultConfig === 'object')
        ? window.csaiHitlDefaultConfig
        : {};
    const serverReviewer = serverDefault.reviewer || ((typeof window !== 'undefined' && window.csaiHitlDefaultReviewer)
        ? window.csaiHitlDefaultReviewer
        : 'human');
    return {
        mode: normalizeHitlMode(serverDefault.mode || HITL_MODE_OFF),
        reviewer: normalizeHitlReviewer(serverReviewer),
        sensitiveTools: DEFAULT_HITL_SESSION_TOOL_WHITELIST,
        timeoutSeconds: normalizeHitlTimeoutForChat(serverDefault.timeoutSeconds, DEFAULT_HITL_TIMEOUT_SECONDS),
        updatedAt: ''
    };
}

function normalizeHitlReviewer(v) {
    const x = String(v || '').trim().toLowerCase();
    if (x === 'audit_agent' || x === 'agent' || x === 'ai') return 'audit_agent';
    return 'human';
}

/** 白名单字符串拆成数组（逗号或换行分隔，与 textarea 一致） */
function hitlToolsSplitToArray(s) {
    return String(s || '')
        .split(/[,\n\r]+/)
        .map(function (x) { return x.trim(); })
        .filter(Boolean);
}

/** 与 config.yaml hitl.tool_whitelist 合并为输入框展示（全局项在前，去重不区分大小写） */
function hitlMergeToolsForDisplay(globalArr, sessionToolsArr) {
    const seen = Object.create(null);
    const out = [];
    function addOne(t) {
        const n = String(t || '').trim();
        if (!n) return;
        const k = n.toLowerCase();
        if (seen[k]) return;
        seen[k] = true;
        out.push(n);
    }
    if (Array.isArray(globalArr)) {
        globalArr.forEach(addOne);
    }
    if (Array.isArray(sessionToolsArr)) {
        sessionToolsArr.forEach(addOne);
    }
    return out.join(', ');
}

/** 保存/发请求前去掉全局白名单工具，避免会话里重复存 config 已有项 */
function hitlStripGlobalToolsFromFormString(globalArr, commaStr) {
    if (!Array.isArray(globalArr) || globalArr.length === 0) {
        return typeof commaStr === 'string' ? commaStr.trim() : '';
    }
    const g = Object.create(null);
    globalArr.forEach(function (t) {
        const k = String(t || '').trim().toLowerCase();
        if (k) g[k] = true;
    });
    return hitlToolsSplitToArray(commaStr)
        .filter(function (p) {
            return p && !g[p.toLowerCase()];
        })
        .join(', ');
}

function getHitlStorageKeyByConversation(conversationId) {
    return `${HITL_STORAGE_PREFIX}:${String(conversationId || '').trim()}`;
}

function chatTranslate(key, fallback) {
    if (typeof window.t === 'function') {
        const translated = window.t(key);
        if (translated && translated !== key) return translated;
    }
    return fallback;
}

function getHitlModeLabel(mode) {
    const safeMode = normalizeHitlMode(mode);
    switch (safeMode) {
        case HITL_MODE_APPROVAL:
            return chatTranslate('chat.hitlModeApproval', '审批模式');
        case HITL_MODE_REVIEW_EDIT:
            return chatTranslate('chat.hitlModeReviewEdit', '审查编辑');
        default:
            return chatTranslate('chat.hitlModeOff', '关闭');
    }
}

function getHitlConfigForConversation(conversationId) {
    const fallback = defaultHitlConfig();
    const cid = conversationId ? String(conversationId).trim() : '';
    if (!cid) {
        return fallback;
    }
    const key = getHitlStorageKeyByConversation(cid);
    try {
        const raw = localStorage.getItem(key);
        if (!raw) {
            return fallback;
        }
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object') {
            return fallback;
        }
        return {
            mode: normalizeHitlMode(parsed.mode),
            reviewer: normalizeHitlReviewer(parsed.reviewer),
            sensitiveTools: typeof parsed.sensitiveTools === 'string' ? parsed.sensitiveTools : fallback.sensitiveTools,
            timeoutSeconds: normalizeHitlTimeoutForChat(parsed.timeoutSeconds, fallback.timeoutSeconds),
            updatedAt: typeof parsed.updatedAt === 'string' ? parsed.updatedAt : ''
        };
    } catch (e) {
        return fallback;
    }
}

function setHitlReviewerUI(reviewer) {
    const v = normalizeHitlReviewer(reviewer);
    const hidden = document.getElementById('hitl-reviewer-select');
    if (hidden) hidden.value = v;
    document.querySelectorAll('.hitl-reviewer-toggle-btn').forEach(function (btn) {
        const active = btn.getAttribute('data-reviewer') === v;
        btn.classList.toggle('is-active', active);
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
}

async function onHitlReviewerChanged(reviewer) {
    setHitlReviewerUI(reviewer);
    updateChatReasoningSummary();
    const cfg = readHitlConfigFromForm();
    const cid = typeof currentConversationId === 'string' ? currentConversationId.trim() : '';
    saveHitlConfigForConversation(cid, cfg, { syncGlobalLast: true });
    try {
        if (cid && typeof window.saveHitlConversationConfig === 'function') {
            await window.saveHitlConversationConfig(cid, cfg);
        } else if (typeof window.putHitlDefaultConfig === 'function') {
            await window.putHitlDefaultConfig(cfg);
        } else if (typeof window.putHitlDefaultReviewer === 'function') {
            await window.putHitlDefaultReviewer(cfg.reviewer);
        }
        const ok = typeof window.t === 'function' ? window.t('hitl.pageReviewerSaved') : '审批方已保存。';
        showChatToast(ok, 'success');
    } catch (e) {
        console.warn('onHitlReviewerChanged', e);
        const prefix = typeof window.t === 'function' ? window.t('chat.hitlApplyFail') : '同步到服务器失败';
        showChatToast(prefix, 'error');
    }
}

function bindHitlReviewerToggleListeners() {
    document.querySelectorAll('.hitl-reviewer-toggle-btn').forEach(function (btn) {
        if (btn.dataset.hitlReviewerBound === '1') return;
        btn.dataset.hitlReviewerBound = '1';
        btn.addEventListener('click', function () {
            const v = btn.getAttribute('data-reviewer');
            if (!v) return;
            onHitlReviewerChanged(v);
        });
    });
}

function saveHitlConfigForConversation(conversationId, cfg, opts) {
    void opts;
    if (!conversationId) {
        return;
    }
    const payload = {
        mode: normalizeHitlMode(cfg && cfg.mode),
        reviewer: normalizeHitlReviewer(cfg && cfg.reviewer),
        sensitiveTools: typeof (cfg && cfg.sensitiveTools) === 'string' ? cfg.sensitiveTools : '',
        timeoutSeconds: normalizeHitlTimeoutForChat(cfg && cfg.timeoutSeconds, DEFAULT_HITL_TIMEOUT_SECONDS),
        updatedAt: typeof (cfg && cfg.updatedAt) === 'string' ? cfg.updatedAt : ''
    };
    const key = getHitlStorageKeyByConversation(conversationId);
    try {
        localStorage.setItem(key, JSON.stringify(payload));
    } catch (e) {
        console.warn('saveHitlConfigForConversation failed', e);
    }
}

function readHitlConfigFromForm() {
    const modeEl = document.getElementById('hitl-mode-select');
    const reviewerEl = document.getElementById('hitl-reviewer-select');
    const toolsEl = document.getElementById('hitl-sensitive-tools');
    const timeoutEl = document.getElementById('hitl-timeout-select');
    const mode = normalizeHitlMode(modeEl ? modeEl.value : HITL_MODE_OFF);
    const reviewer = normalizeHitlReviewer(reviewerEl ? reviewerEl.value : 'human');
    let sensitiveTools = toolsEl ? String(toolsEl.value || '').trim() : '';
    const g = typeof window !== 'undefined' ? window.csaiHitlGlobalToolWhitelist : null;
    if (Array.isArray(g) && g.length > 0) {
        sensitiveTools = hitlStripGlobalToolsFromFormString(g, sensitiveTools);
    }
    return {
        mode,
        reviewer,
        sensitiveTools,
        timeoutSeconds: normalizeHitlTimeoutForChat(timeoutEl ? timeoutEl.value : DEFAULT_HITL_TIMEOUT_SECONDS, DEFAULT_HITL_TIMEOUT_SECONDS),
        updatedAt: new Date().toISOString()
    };
}

function updateHitlStatusUI(_cfg) {
    /* 侧栏已改为自动保存；同步更新输入框快捷摘要。 */
    updateChatReasoningSummary();
}

function applyHitlConfigToUI(cfg) {
    const conf = cfg || defaultHitlConfig();
    const modeEl = document.getElementById('hitl-mode-select');
    const toolsEl = document.getElementById('hitl-sensitive-tools');
    const timeoutEl = document.getElementById('hitl-timeout-select');
    const uiMode = normalizeHitlMode(conf.mode);
    if (modeEl) modeEl.value = uiMode;
    setHitlReviewerUI(conf.reviewer);
    // Keep this field scoped to the current conversation. The config-level
    // allowlist is applied by the backend and must not be copied into the
    // editable session value. Empty/legacy sessions receive only the stable
    // Agent control-tool baseline shown by the original UI.
    const toolsVal = typeof conf.sensitiveTools === 'string' && conf.sensitiveTools.trim()
        ? conf.sensitiveTools.trim()
        : DEFAULT_HITL_SESSION_TOOL_WHITELIST;
    if (toolsEl) {
        toolsEl.value = toolsVal;
    }
    if (timeoutEl) {
        const timeoutSeconds = normalizeHitlTimeoutForChat(conf.timeoutSeconds, DEFAULT_HITL_TIMEOUT_SECONDS);
        const supported = Array.from(timeoutEl.options || []).some(function (option) {
            return Number(option.value) === timeoutSeconds;
        });
        timeoutEl.value = String(supported ? timeoutSeconds : DEFAULT_HITL_TIMEOUT_SECONDS);
    }
    updateHitlStatusUI(conf);
    refreshSessionSettingsSelects();
}

function bindHitlSidebarModeListener() {
    const modeEl = document.getElementById('hitl-mode-select');
    const timeoutEl = document.getElementById('hitl-timeout-select');
    [modeEl, timeoutEl].forEach(function (el) {
        if (!el || el.dataset.hitlModeBound === '1') return;
        el.dataset.hitlModeBound = '1';
        el.addEventListener('change', function () {
            applyHitlConfigToUI(readHitlConfigFromForm());
            refreshSessionSettingsSelects();
            scheduleHitlSidebarAutosave(0);
            updateChatReasoningSummary();
        });
    });
}

function refreshHitlConfigByCurrentConversation() {
    const cfg = getHitlConfigForConversation(currentConversationId || '');
    applyHitlConfigToUI(cfg);
}

async function waitForHitlConfigReady(conversationId) {
    const cid = String(conversationId || '').trim();
    if (cid && hitlConfigSyncConversationId === cid) {
        await hitlConfigSyncPromise;
        return;
    }
    const defaultReady = window.csaiHitlDefaultConfigReady || window.csaiHitlDefaultReviewerReady;
    if (!cid && defaultReady && typeof defaultReady.then === 'function') {
        await defaultReady.catch(function () {});
        if (!currentConversationId) refreshHitlConfigByCurrentConversation();
    }
}

function showHitlApplyFeedback(text, isError, partial) {
    const el = document.getElementById('hitl-apply-feedback');
    if (hitlApplyFeedbackTimer) {
        clearTimeout(hitlApplyFeedbackTimer);
        hitlApplyFeedbackTimer = null;
    }
    if (!el) {
        if (text && isError) {
            showChatToast(text, 'error');
        }
        return;
    }
    el.classList.toggle('hitl-apply-feedback--error', !!isError);
    el.classList.toggle('hitl-apply-feedback--partial', !!partial && !isError);
    if (!text) {
        el.textContent = '';
        el.style.display = 'none';
        el.classList.remove('hitl-apply-feedback--error', 'hitl-apply-feedback--partial');
        return;
    }
    el.textContent = text;
    el.style.display = 'block';
    if (!isError) {
        hitlApplyFeedbackTimer = setTimeout(function () {
            el.textContent = '';
            el.style.display = 'none';
            el.classList.remove('hitl-apply-feedback--error');
            el.classList.remove('hitl-apply-feedback--partial');
            hitlApplyFeedbackTimer = null;
        }, 3200);
    }
}

/** 侧栏人机协同：自动写入本地、合并展示并尽量同步服务端 */
async function applyHitlSidebarConfig() {
    const btn = document.getElementById('hitl-apply-btn');
    showHitlApplyFeedback('', false);
    if (btn) btn.disabled = true;
    try {
        const cfg = readHitlConfigFromForm();
        const cid = typeof currentConversationId === 'string' ? currentConversationId.trim() : '';
        saveHitlConfigForConversation(cid, cfg, { syncGlobalLast: true });

        const toolsArr = hitlToolsSplitToArray(cfg.sensitiveTools || '');

        let yamlMerged = false;
        if (!cid && toolsArr.length > 0 && typeof window.mergeHitlGlobalToolWhitelist === 'function') {
            const newGlobal = await window.mergeHitlGlobalToolWhitelist(toolsArr);
            if (Array.isArray(newGlobal)) {
                window.csaiHitlGlobalToolWhitelist = newGlobal;
            }
            yamlMerged = true;
        }

        applyHitlConfigToUI(cfg);

        if (cid && typeof window.saveHitlConversationConfig === 'function') {
            await window.saveHitlConversationConfig(cid, cfg);
            const ok = typeof window.t === 'function' ? window.t('chat.hitlApplyOkSync') : '人机协同配置已保存并同步到服务器。';
            showHitlApplyFeedback(ok, false);
        } else if (typeof window.putHitlDefaultConfig === 'function') {
            await window.putHitlDefaultConfig(cfg);
            const okDefault = typeof window.t === 'function' ? window.t('chat.hitlApplyOkDefaultConfig') : '人机协同默认配置已写入 config.yaml 并生效。';
            showHitlApplyFeedback(okDefault, false);
        } else if (yamlMerged) {
            const okYaml = typeof window.t === 'function' ? window.t('chat.hitlApplyOkWhitelistYaml') : '免审批工具已合并进 config.yaml 并生效。会话配置会自动保存。';
            showHitlApplyFeedback(okYaml, false);
        } else {
            const localOnly = typeof window.t === 'function' ? window.t('chat.hitlApplyOkLocal') : '已保存到本浏览器。';
            showHitlApplyFeedback(localOnly, false);
        }
        if (typeof window.refreshHitlPageWhitelist === 'function') {
            window.refreshHitlPageWhitelist();
        }
    } catch (e) {
        console.warn('applyHitlSidebarConfig', e);
        const prefix = typeof window.t === 'function' ? window.t('chat.hitlApplyFail') : '同步到服务器失败';
        const detail = (e && e.message) ? e.message : String(e);
        showHitlApplyFeedback(prefix + (detail ? '：' + detail : ''), true);
    } finally {
        if (btn) btn.disabled = false;
    }
}

function scheduleHitlSidebarAutosave(delayMs) {
    if (hitlAutoSaveTimer) {
        clearTimeout(hitlAutoSaveTimer);
        hitlAutoSaveTimer = null;
    }
    hitlAutoSaveTimer = setTimeout(function () {
        hitlAutoSaveTimer = null;
        applyHitlSidebarConfig();
    }, typeof delayMs === 'number' ? delayMs : 500);
}

function bindHitlSensitiveToolsAutosaveListener() {
    const toolsEl = document.getElementById('hitl-sensitive-tools');
    if (!toolsEl || toolsEl.dataset.hitlAutosaveBound === '1') return;
    toolsEl.dataset.hitlAutosaveBound = '1';
    toolsEl.addEventListener('input', function () {
        scheduleHitlSidebarAutosave(700);
    });
    toolsEl.addEventListener('blur', function () {
        scheduleHitlSidebarAutosave(0);
    });
}

/** 将 localStorage 规范为 eino_single | deep | plan_execute | supervisor */
function chatAgentModeNormalizeStored(stored, cfg) {
    const pub = cfg && cfg.multi_agent ? cfg.multi_agent : null;
    const multiOn = !!(pub && pub.enabled);
    const s = stored;
    if (chatAgentModeIsEinoSingle(s)) return s;
    if (chatAgentModeIsEino(s)) {
        return multiOn ? s : CHAT_AGENT_MODE_EINO_SINGLE;
    }
    return CHAT_AGENT_MODE_EINO_SINGLE;
}

function normalizeConversationAgentModeForUI(mode) {
    const v = String(mode || '').trim().toLowerCase().replace(/-/g, '_');
    if (chatAgentModeIsEinoSingle(v)) return v;
    if (chatAgentModeIsEino(v)) {
        return multiAgentAPIEnabled ? v : CHAT_AGENT_MODE_EINO_SINGLE;
    }
    return '';
}

function conversationAgentModeStorageKey(conversationId) {
    return `${AGENT_MODE_CONVERSATION_STORAGE_PREFIX}:${String(conversationId || '').trim()}`;
}

function readConversationAgentModePreference(conversationId) {
    if (!conversationId) return '';
    try {
        return normalizeConversationAgentModeForUI(localStorage.getItem(conversationAgentModeStorageKey(conversationId)) || '');
    } catch (e) {
        return '';
    }
}

function saveConversationAgentModePreference(conversationId, mode) {
    const normalized = normalizeConversationAgentModeForUI(mode);
    if (!conversationId || !normalized) return;
    try {
        localStorage.setItem(conversationAgentModeStorageKey(conversationId), normalized);
    } catch (e) { /* ignore */ }
}

function applyConversationAgentMode(conversationId, conversation) {
    const saved = readConversationAgentModePreference(conversationId);
    const fromServer = normalizeConversationAgentModeForUI(conversation && (conversation.agentMode || conversation.agent_mode));
    const mode = saved || fromServer;
    if (!mode) return;
    syncAgentModeFromValue(mode);
}

if (typeof window !== 'undefined') {
    window.csaiHitlGlobalToolWhitelist = window.csaiHitlGlobalToolWhitelist || [];
    window.csaiHitlDefaultConfig = window.csaiHitlDefaultConfig || {
        mode: HITL_MODE_OFF,
        reviewer: 'human',
        timeoutSeconds: DEFAULT_HITL_TIMEOUT_SECONDS
    };
    window.csaiHitlDefaultReviewer = window.csaiHitlDefaultReviewer || 'human';
    window.csaiChatAgentMode = {
        EINO_MODES: CHAT_AGENT_EINO_MODES,
        EINO_SINGLE: CHAT_AGENT_MODE_EINO_SINGLE,
        isEino: chatAgentModeIsEino,
        isEinoSingle: chatAgentModeIsEinoSingle,
        normalizeStored: chatAgentModeNormalizeStored,
        normalizeOrchestration: normalizeOrchestrationClient
    };
    window.applyHitlSidebarConfig = applyHitlSidebarConfig;
    window.readHitlConfigFromForm = readHitlConfigFromForm;
    window.applyHitlConfigToUI = applyHitlConfigToUI;
    window.refreshHitlConfigByCurrentConversation = refreshHitlConfigByCurrentConversation;
    window.saveHitlConfigForConversation = saveHitlConfigForConversation;
    window.getHitlConfigForConversation = getHitlConfigForConversation;
    bindHitlSidebarModeListener();
    bindHitlReviewerToggleListeners();
    bindHitlSensitiveToolsAutosaveListener();
    window.setHitlReviewerUI = setHitlReviewerUI;
    window.onHitlReviewerChanged = onHitlReviewerChanged;
    window.bindHitlReviewerToggleListeners = bindHitlReviewerToggleListeners;
    window.hitlMergeToolsForDisplay = hitlMergeToolsForDisplay;
    window.hitlStripGlobalToolsFromFormString = hitlStripGlobalToolsFromFormString;
    window.hitlToolsSplitToArray = hitlToolsSplitToArray;
    window.updateHitlStatusUI = updateHitlStatusUI;
}

function syncHitlSidebarAriaExpanded() {
    var card = document.getElementById('hitl-sidebar-card');
    var toggle = document.getElementById('hitl-sidebar-toggle');
    if (!card || !toggle) return;
    toggle.setAttribute('aria-expanded', card.classList.contains('hitl-sidebar-collapsed') ? 'false' : 'true');
}

function closeHitlSidebarCard() {
    var card = document.getElementById('hitl-sidebar-card');
    if (!card || card.classList.contains('hitl-sidebar-collapsed')) return;
    card.classList.add('hitl-sidebar-collapsed');
    syncHitlSidebarAriaExpanded();
    try {
        localStorage.setItem('hitl-sidebar-collapsed', '1');
    } catch (e) {}
}

function toggleHitlSidebarCard() {
    var card = document.getElementById('hitl-sidebar-card');
    if (!card) return;
    card.classList.toggle('hitl-sidebar-collapsed');
    syncHitlSidebarAriaExpanded();
    try {
        localStorage.setItem('hitl-sidebar-collapsed', card.classList.contains('hitl-sidebar-collapsed') ? '1' : '0');
    } catch (e) {}
}
window.toggleHitlSidebarCard = toggleHitlSidebarCard;

document.addEventListener('DOMContentLoaded', function () {
    var card = document.getElementById('hitl-sidebar-card');
    if (card && localStorage.getItem('hitl-sidebar-collapsed') === '0') {
        card.classList.remove('hitl-sidebar-collapsed');
    }
    syncHitlSidebarAriaExpanded();
});

function getAgentModeLabelForValue(mode) {
    if (typeof window.t === 'function') {
        switch (mode) {
            case 'deep':
                return window.t('chat.agentModeDeep');
            case 'plan_execute':
                return window.t('chat.agentModePlanExecuteLabel');
            case 'supervisor':
                return window.t('chat.agentModeSupervisorLabel');
            case CHAT_AGENT_MODE_EINO_SINGLE:
                return window.t('chat.agentModeEinoSingle');
            default:
                return mode;
        }
    }
    switch (mode) {
        case CHAT_AGENT_MODE_EINO_SINGLE: return 'Eino 单代理';
        case 'deep': return 'Deep';
        case 'plan_execute': return 'Plan-Execute';
        case 'supervisor': return 'Supervisor';
        default: return mode;
    }
}

function getAgentModeIconClassForValue(mode) {
    switch (mode) {
        case CHAT_AGENT_MODE_EINO_SINGLE: return 'eino';
        case 'deep': return 'deep';
        case 'plan_execute': return 'plan';
        case 'supervisor': return 'supervisor';
        default: return 'default';
    }
}

function renderAgentModeLogoMarkup() {
    return '<svg class="agent-mode-logo__svg" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="3" y="11" width="18" height="10" rx="2"/><circle cx="12" cy="5" r="2"/><path d="M12 7v4"/><path d="M8 16h.01"/><path d="M16 16h.01"/></svg>';
}

function syncAgentModeFromValue(value) {
    const hid = document.getElementById('agent-mode-select');
    const label = document.getElementById('agent-mode-text');
    const icon = document.getElementById('agent-mode-icon');
    if (hid) hid.value = value;
    if (label) label.textContent = getAgentModeLabelForValue(value);
    if (icon) {
        icon.className = 'role-selector-icon agent-mode-logo agent-mode-logo--' + getAgentModeIconClassForValue(value);
        icon.innerHTML = renderAgentModeLogoMarkup();
    }
    document.querySelectorAll('.agent-mode-option').forEach(function (el) {
        const v = el.getAttribute('data-value');
        el.classList.toggle('selected', v === value);
    });
    syncReasoningRowVisibility(value);
}

function syncReasoningRowVisibility(modeVal) {
    mountChatSessionSettingsPopover();
    const wrap = document.getElementById('chat-reasoning-wrapper');
    if (!wrap) return;
    const show = modeVal === CHAT_AGENT_MODE_EINO_SINGLE || (multiAgentAPIEnabled && chatAgentModeIsEino(modeVal));
    wrap.style.display = show ? '' : 'none';
    if (!show) {
        closeChatReasoningPanel();
    } else {
        syncChatReasoningBarHeight();
        updateChatReasoningSummary();
    }
}

function normalizeChatAIChannelId(s) {
    return String(s || '').trim().toLowerCase().replace(/_/g, '-').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
}

function resolveChatAIChannelId(id) {
    const raw = String(id || '').trim();
    if (!raw) return '';
    if (chatAIChannels[raw]) return raw;
    const normalized = normalizeChatAIChannelId(raw);
    return normalized && chatAIChannelIdByNormalizedId[normalized] ? chatAIChannelIdByNormalizedId[normalized] : '';
}

function populateChatAIChannelSelect(ai) {
    const select = document.getElementById('chat-ai-channel-select');
    if (!select) return;
    const cfg = ai && typeof ai === 'object' ? ai : {};
    chatAIChannels = cfg.channels && typeof cfg.channels === 'object' ? cfg.channels : {};
    chatAIChannelIdByNormalizedId = {};
    Object.keys(chatAIChannels).forEach(function (id) {
        const normalized = normalizeChatAIChannelId(id);
        if (normalized && !chatAIChannelIdByNormalizedId[normalized]) {
            chatAIChannelIdByNormalizedId[normalized] = id;
        }
    });
    chatDefaultAIChannel = resolveChatAIChannelId(cfg.default_channel || '');
    select.innerHTML = '';
    const fallbackOpt = document.createElement('option');
    fallbackOpt.value = '';
    fallbackOpt.textContent = typeof window.t === 'function' ? window.t('chat.aiChannelDefault') : '跟随默认通道';
    select.appendChild(fallbackOpt);
    Object.keys(chatAIChannels).sort().forEach(function (id) {
        const ch = chatAIChannels[id] || {};
        const opt = document.createElement('option');
        opt.value = id;
        opt.textContent = (ch.name || id) + (ch.model ? ' · ' + ch.model : '');
        select.appendChild(opt);
    });
    let stored = '';
    try { stored = localStorage.getItem(AI_CHANNEL_STORAGE_KEY) || ''; } catch (e) {}
    stored = resolveChatAIChannelId(stored);
    select.value = stored || '';
    refreshSessionSettingsSelects();
    updateChatReasoningSummary();
}

function selectedChatAIChannelId() {
    const select = document.getElementById('chat-ai-channel-select');
    return resolveChatAIChannelId(select ? select.value : '');
}

function currentChatAIChannelLabel() {
    const id = selectedChatAIChannelId() || chatDefaultAIChannel;
    const ch = id ? chatAIChannels[id] : null;
    if (!ch) {
        return chatTranslate('chat.aiChannelDefaultShort', '默认通道');
    }
    return ch.name || id;
}

function currentChatModelLabel() {
    const id = selectedChatAIChannelId() || chatDefaultAIChannel;
    const ch = id ? chatAIChannels[id] : null;
    const model = ch && typeof ch.model === 'string' ? ch.model.trim() : '';
    return model || currentChatAIChannelLabel();
}

function currentSystemModelLabel() {
    const ch = chatDefaultAIChannel ? chatAIChannels[chatDefaultAIChannel] : null;
    const model = ch && typeof ch.model === 'string' ? ch.model.trim() : '';
    return model || (ch && (ch.name || chatDefaultAIChannel)) || currentChatModelLabel();
}

function currentHitlAuditBackend() {
    const b = String(chatHitlAuditBackend || (typeof window !== 'undefined' && window.csaiHitlAuditBackend) || '').trim().toLowerCase();
    return (b === 'typesafe' || b === 'jev' || b === 'type-safe') ? 'typesafe' : 'openai';
}

function currentHitlAuditModelLabel() {
    if (currentHitlAuditBackend() === 'typesafe') {
        return chatHitlAuditModelName || 'jev-latest';
    }
    return chatHitlAuditModelName || currentSystemModelLabel();
}

function currentHitlAuditEngineLabel() {
    const engine = currentHitlAuditBackend() === 'typesafe'
        ? chatTranslate('settings.hitl.auditBackendTypeSafe', 'TypeSafe Jev')
        : chatTranslate('settings.hitl.auditBackendOpenAI', 'OpenAI 协议模型');
    const model = currentHitlAuditModelLabel();
    return engine + (model ? ' · ' + model : '');
}

function resolveChatPickerChannelId() {
    return selectedChatAIChannelId() || chatDefaultAIChannel;
}

function chatSystemModelConfigState(cfg, preferredChannelId) {
    const source = cfg && typeof cfg === 'object' ? cfg : {};
    const sourceAI = source.ai && typeof source.ai === 'object' ? source.ai : {};
    const channels = sourceAI.channels && typeof sourceAI.channels === 'object'
        ? { ...sourceAI.channels }
        : {};
    const resolveFromChannels = function (value) {
        const raw = String(value || '').trim();
        if (raw && channels[raw]) return raw;
        const normalized = normalizeChatAIChannelId(raw);
        return normalized
            ? Object.keys(channels).find(function (id) {
                return normalizeChatAIChannelId(id) === normalized;
            }) || ''
            : '';
    };
    let defaultChannelId = resolveFromChannels(sourceAI.default_channel);
    let channelId = resolveFromChannels(preferredChannelId) || defaultChannelId;
    if (!channels[channelId]) {
        channelId = Object.keys(channels)[0] || 'default';
    }
    if (!channels[channelId]) {
        const legacy = source.openai && typeof source.openai === 'object' ? source.openai : {};
        channels[channelId] = {
            name: channelId === 'default' ? 'Default' : channelId,
            provider: legacy.provider || 'openai',
            api_key: legacy.api_key || '',
            base_url: legacy.base_url || '',
            model: legacy.model || ''
        };
    }
    if (!defaultChannelId) defaultChannelId = channelId;
    return {
        ai: { ...sourceAI, default_channel: defaultChannelId, channels: channels },
        channelId: channelId,
        channel: channels[channelId]
    };
}

function chatSystemModelElements() {
    return {
        wrap: document.getElementById('chat-model-shortcut-wrap'),
        button: document.getElementById('chat-model-shortcut'),
        menu: document.getElementById('chat-system-model-menu'),
        main: document.getElementById('chat-system-model-main'),
        subview: document.getElementById('chat-system-model-subview'),
        subviewTitle: document.getElementById('chat-system-model-subview-title'),
        list: document.getElementById('chat-system-model-list'),
        status: document.getElementById('chat-system-model-status'),
        subviewStatus: document.getElementById('chat-system-model-subview-status'),
        channelValue: document.getElementById('chat-system-model-channel-value'),
        currentValue: document.getElementById('chat-system-model-current-value'),
        modeValue: document.getElementById('chat-system-model-mode-value'),
        effortValue: document.getElementById('chat-system-model-effort-value')
    };
}

function setChatSystemModelStatus(message, tone) {
    const ui = chatSystemModelElements();
    [ui.status, ui.subviewStatus].forEach(function (status) {
        if (!status) return;
        status.textContent = message || '';
        status.dataset.tone = tone || '';
    });
}

function chatReasoningEffortLabel(value) {
    switch (String(value || '').trim()) {
        case 'low': return 'low';
        case 'medium': return 'medium';
        case 'high': return 'high';
        case 'xhigh': return 'xhigh';
        case 'max': return 'max';
        default: return chatTranslate('chat.reasoningEffortUnset', '不指定');
    }
}

function currentChatReasoningEffort() {
    const effort = document.getElementById('chat-reasoning-effort');
    return effort ? String(effort.value || '').trim() : '';
}

function currentChatReasoningMode() {
    const mode = document.getElementById('chat-reasoning-mode');
    const value = mode ? String(mode.value || 'default').trim() : 'default';
    return ['default', 'off', 'on', 'auto'].includes(value) ? value : 'default';
}

function currentChatReasoningMenuLabel() {
    const modeValue = currentChatReasoningMode();
    if (modeValue === 'off') return chatTranslate('chat.reasoningModeOff', '关闭');
    const effort = currentChatReasoningEffort();
    return effort ? chatReasoningEffortLabel(effort) : reasoningSummaryModeLabel(modeValue);
}

function updateChatSystemModelPickerValues() {
    const ui = chatSystemModelElements();
    const channel = currentChatAIChannelLabel();
    const model = currentChatModelLabel();
    const mode = reasoningSummaryModeLabel(currentChatReasoningMode());
    const effort = currentChatReasoningMenuLabel();
    if (ui.channelValue) ui.channelValue.textContent = channel;
    if (ui.currentValue) ui.currentValue.textContent = model;
    if (ui.modeValue) ui.modeValue.textContent = mode;
    if (ui.effortValue) ui.effortValue.textContent = effort;
    const composerEffort = document.getElementById('chat-model-shortcut-effort');
    if (composerEffort) composerEffort.textContent = effort;
}

function closeChatSystemModelPicker(force) {
    if (chatSystemModelSaving && !force) return;
    const ui = chatSystemModelElements();
    if (ui.menu) ui.menu.hidden = true;
    if (ui.button) {
        ui.button.classList.remove('active');
        ui.button.setAttribute('aria-expanded', 'false');
    }
    if (ui.main) ui.main.hidden = false;
    if (ui.subview) ui.subview.hidden = true;
    chatSystemModelRequestSeq += 1;
}

async function readChatSystemModelError(response, fallback) {
    try {
        const body = await response.json();
        return body.error || body.message || fallback;
    } catch (_) {
        return fallback;
    }
}

function renderChatSystemModelOptions(models, currentModel) {
    const ui = chatSystemModelElements();
    if (!ui.list) return 0;
    ui.list.innerHTML = '';
    const unique = [];
    const seen = new Set();
    [currentModel].concat(Array.isArray(models) ? models : []).forEach(function (value) {
        const model = String(value || '').trim();
        if (!model || seen.has(model)) return;
        seen.add(model);
        unique.push(model);
    });
    unique.forEach(function (model) {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = 'chat-system-model-option';
        option.setAttribute('role', 'option');
        option.setAttribute('aria-selected', model === currentModel ? 'true' : 'false');
        option.dataset.model = model;

        const label = document.createElement('span');
        label.className = 'chat-system-model-option-label';
        label.textContent = model;
        option.appendChild(label);

        if (model === currentModel) {
            option.classList.add('is-selected');
            const current = document.createElement('span');
            current.className = 'chat-system-model-current';
            current.textContent = chatTranslate('chat.systemModelCurrent', '当前');
            option.appendChild(current);
        }
        option.addEventListener('click', function (event) {
            event.preventDefault();
            event.stopPropagation();
            selectChatSystemModel(model);
        });
        ui.list.appendChild(option);
    });
    return unique.length;
}

function renderChatReasoningEffortOptions() {
    const ui = chatSystemModelElements();
    if (!ui.list) return;
    ui.list.innerHTML = '';
    const currentEffort = currentChatReasoningEffort();
    ['', 'low', 'medium', 'high', 'xhigh', 'max'].forEach(function (effort) {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = 'chat-system-model-option';
        option.setAttribute('role', 'option');
        option.setAttribute('aria-selected', effort === currentEffort ? 'true' : 'false');
        if (effort === currentEffort) option.classList.add('is-selected');

        const label = document.createElement('span');
        label.className = 'chat-system-model-option-label chat-system-effort-option-label';
        label.textContent = chatReasoningEffortLabel(effort);
        option.appendChild(label);

        if (effort === currentEffort) {
            const current = document.createElement('span');
            current.className = 'chat-system-model-current';
            current.textContent = chatTranslate('chat.systemModelCurrent', '当前');
            option.appendChild(current);
        }
        option.addEventListener('click', function (event) {
            event.preventDefault();
            event.stopPropagation();
            selectChatReasoningEffort(effort);
        });
        ui.list.appendChild(option);
    });
}

function renderChatReasoningModeOptions() {
    const ui = chatSystemModelElements();
    if (!ui.list) return;
    ui.list.innerHTML = '';
    const currentMode = currentChatReasoningMode();
    ['default', 'off', 'on', 'auto'].forEach(function (mode) {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = 'chat-system-model-option';
        option.setAttribute('role', 'option');
        option.setAttribute('aria-selected', mode === currentMode ? 'true' : 'false');
        if (mode === currentMode) option.classList.add('is-selected');
        const label = document.createElement('span');
        label.className = 'chat-system-model-option-label chat-system-effort-option-label';
        label.textContent = reasoningSummaryModeLabel(mode);
        option.appendChild(label);
        if (mode === currentMode) {
            const current = document.createElement('span');
            current.className = 'chat-system-model-current';
            current.textContent = chatTranslate('chat.systemModelCurrent', '当前');
            option.appendChild(current);
        }
        option.addEventListener('click', function (event) {
            event.preventDefault();
            event.stopPropagation();
            selectChatReasoningMode(mode);
        });
        ui.list.appendChild(option);
    });
}

function finishChatReasoningPickerUpdate() {
    persistChatReasoningPrefs();
    setChatSystemModelStatus(chatTranslate('chat.reasoningSessionUpdated', '会话推理设置已更新'), 'success');
    if (chatSystemModelCloseTimer) window.clearTimeout(chatSystemModelCloseTimer);
    chatSystemModelCloseTimer = window.setTimeout(function () {
        chatSystemModelSaving = false;
        closeChatSystemModelPicker(true);
    }, 450);
}

function selectChatReasoningMode(mode) {
    if (chatSystemModelSaving) return;
    const chosen = ['default', 'off', 'on', 'auto'].includes(String(mode || '').trim())
        ? String(mode || '').trim()
        : 'default';
    const modeControl = document.getElementById('chat-reasoning-mode');
    if (!modeControl) return;
    chatSystemModelSaving = true;
    modeControl.value = chosen;
    finishChatReasoningPickerUpdate();
}

function selectChatReasoningEffort(effort) {
    if (chatSystemModelSaving) return;
    const raw = String(effort || '').trim();
    const chosen = ['', 'low', 'medium', 'high', 'xhigh', 'max'].includes(raw) ? raw : '';
    const effortControl = document.getElementById('chat-reasoning-effort');
    if (!effortControl) return;
    chatSystemModelSaving = true;
    effortControl.value = chosen;
    finishChatReasoningPickerUpdate();
}

function renderChatSystemModelRetry() {
    const ui = chatSystemModelElements();
    if (!ui.list) return;
    ui.list.innerHTML = '';
    const retry = document.createElement('button');
    retry.type = 'button';
    retry.className = 'chat-system-model-retry';
    retry.textContent = chatTranslate('chat.systemModelRetry', '重新获取');
    retry.addEventListener('click', function (retryEvent) {
        retryEvent.preventDefault();
        retryEvent.stopPropagation();
        fetchChatSystemModelsForChannel(resolveChatPickerChannelId(), { force: true });
    });
    ui.list.appendChild(retry);
}

function renderChatAIChannelOptions() {
    const ui = chatSystemModelElements();
    if (!ui.list) return;
    ui.list.innerHTML = '';
    const selected = selectedChatAIChannelId();
    const choices = [{ id: '', label: chatTranslate('chat.aiChannelDefault', '跟随默认通道') }]
        .concat(Object.keys(chatAIChannels).sort().map(function (id) {
            const channel = chatAIChannels[id] || {};
            return { id: id, label: channel.name || id };
        }));
    choices.forEach(function (choice) {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = 'chat-system-model-option';
        option.setAttribute('role', 'option');
        option.setAttribute('aria-selected', choice.id === selected ? 'true' : 'false');
        if (choice.id === selected) option.classList.add('is-selected');
        const label = document.createElement('span');
        label.className = 'chat-system-model-option-label chat-system-channel-option-label';
        label.textContent = choice.label;
        option.appendChild(label);
        if (choice.id === selected) {
            const current = document.createElement('span');
            current.className = 'chat-system-model-current';
            current.textContent = chatTranslate('chat.systemModelCurrent', '当前');
            option.appendChild(current);
        }
        option.addEventListener('click', function (event) {
            event.preventDefault();
            event.stopPropagation();
            selectChatAIChannel(choice.id);
        });
        ui.list.appendChild(option);
    });
}

async function selectChatAIChannel(channelId) {
    const select = document.getElementById('chat-ai-channel-select');
    if (!select) return;
    const resolved = resolveChatAIChannelId(channelId);
    select.value = resolved || '';
    persistChatAIChannelPref();
    refreshSessionSettingsSelects();
    updateChatSystemModelPickerValues();
    openChatSystemModelView('main');
    setChatSystemModelStatus(chatTranslate('chat.systemModelLoading', '正在获取模型列表…'), 'loading');
    await fetchChatSystemModelsForChannel(resolveChatPickerChannelId(), { force: true });
}

function openChatSystemModelView(view, event) {
    if (event) {
        event.preventDefault();
        event.stopPropagation();
    }
    const ui = chatSystemModelElements();
    if (!ui.main || !ui.subview || !ui.list) return;
    if (view === 'main') {
        ui.main.hidden = false;
        ui.subview.hidden = true;
        updateChatSystemModelPickerValues();
        return;
    }
    ui.main.hidden = true;
    ui.subview.hidden = false;
    ui.subview.dataset.view = view;
    if (view === 'channel') {
        if (ui.subviewTitle) ui.subviewTitle.textContent = chatTranslate('chat.aiChannelLabel', 'AI 通道');
        setChatSystemModelStatus('', '');
        renderChatAIChannelOptions();
        return;
    }
    if (view === 'mode') {
        if (ui.subviewTitle) ui.subviewTitle.textContent = chatTranslate('chat.reasoningModeLabel', '推理模式');
        setChatSystemModelStatus('', '');
        renderChatReasoningModeOptions();
        return;
    }
    if (view === 'effort') {
        if (ui.subviewTitle) ui.subviewTitle.textContent = chatTranslate('chat.reasoningEffortLabel', '推理强度');
        setChatSystemModelStatus('', '');
        renderChatReasoningEffortOptions();
        return;
    }
    if (ui.subviewTitle) ui.subviewTitle.textContent = chatTranslate('chat.systemModelField', '模型');
    if (chatSystemModelOptions.length) {
        const count = renderChatSystemModelOptions(chatSystemModelOptions, chatSystemModelCurrent);
        setChatSystemModelStatus(
            chatTranslate('chat.systemModelLoaded', '已获取 {count} 个模型').replace('{count}', String(count)),
            'success'
        );
    } else if (chatSystemModelLoadError) {
        renderChatSystemModelRetry();
        setChatSystemModelStatus(chatSystemModelLoadError, 'error');
    } else {
        ui.list.innerHTML = '';
        setChatSystemModelStatus(chatTranslate('chat.systemModelLoading', '正在获取模型列表…'), 'loading');
    }
}

async function selectChatSystemModel(model) {
    if (chatSystemModelSaving) return;
    if (typeof requirePermission === 'function' && !requirePermission('config:write')) return;
    const chosen = String(model || '').trim();
    if (!chosen) return;
    const ui = chatSystemModelElements();
    chatSystemModelSaving = true;
    if (ui.list) {
        ui.list.querySelectorAll('button').forEach(function (button) { button.disabled = true; });
    }
    setChatSystemModelStatus(chatTranslate('chat.systemModelSaving', '正在保存…'), 'loading');
    try {
        const latestResponse = await apiFetch('/api/config');
        if (!latestResponse.ok) {
            throw new Error(await readChatSystemModelError(latestResponse, chatTranslate('chat.systemModelSaveFailed', '保存失败')));
        }
        const latest = await latestResponse.json();
        const state = chatSystemModelConfigState(latest, resolveChatPickerChannelId());
        state.ai.channels[state.channelId] = { ...state.channel, model: chosen };
        const updateResponse = await apiFetch('/api/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ai: state.ai })
        });
        if (!updateResponse.ok) {
            throw new Error(await readChatSystemModelError(updateResponse, chatTranslate('chat.systemModelSaveFailed', '保存失败')));
        }
        const applyResponse = await apiFetch('/api/config/apply', { method: 'POST' });
        if (!applyResponse.ok) {
            throw new Error(await readChatSystemModelError(applyResponse, chatTranslate('chat.systemModelApplyFailed', '应用模型失败')));
        }
        chatAIChannels = state.ai.channels;
        chatDefaultAIChannel = resolveChatAIChannelId(state.ai.default_channel) || state.channelId;
        updateChatComposerSessionShortcuts();
        await initChatAgentModeFromConfig();
        chatSystemModelCurrent = chosen;
        chatSystemModelOptions = [chosen].concat(chatSystemModelOptions);
        updateChatSystemModelPickerValues();
        renderChatSystemModelOptions(chatSystemModelOptions, chosen);
        setChatSystemModelStatus(chatTranslate('chat.systemModelSaved', '已自动保存'), 'success');
        if (chatSystemModelCloseTimer) window.clearTimeout(chatSystemModelCloseTimer);
        chatSystemModelCloseTimer = window.setTimeout(function () {
            chatSystemModelSaving = false;
            closeChatSystemModelPicker(true);
        }, 650);
        return;
    } catch (error) {
        console.error('selectChatSystemModel', error);
        setChatSystemModelStatus(error.message || chatTranslate('chat.systemModelSaveFailed', '保存失败'), 'error');
    }
    chatSystemModelSaving = false;
    if (ui.list) {
        ui.list.querySelectorAll('button').forEach(function (button) { button.disabled = false; });
    }
}

function chatSystemModelCacheKey(channelId, channel) {
    return [
        String(channelId || ''),
        String(channel && channel.provider || 'openai'),
        String(channel && channel.base_url || '').trim()
    ].join('|');
}

async function fetchChatSystemModelsForChannel(channelId, options) {
    const opts = options || {};
    const ui = chatSystemModelElements();
    if (!ui.menu || !ui.list || ui.menu.hidden) return;
    const resolvedChannelId = resolveChatAIChannelId(channelId) || chatDefaultAIChannel;
    const channel = resolvedChannelId ? chatAIChannels[resolvedChannelId] || {} : {};
    const cacheKey = chatSystemModelCacheKey(resolvedChannelId, channel);
    const cached = chatSystemModelCache.get(cacheKey);
    const requestId = ++chatSystemModelRequestSeq;
    chatSystemModelCurrent = String(channel.model || '').trim();
    chatSystemModelOptions = [];
    chatSystemModelLoadError = '';
    if (!opts.force && cached && Date.now() - cached.fetchedAt < CHAT_SYSTEM_MODEL_CACHE_TTL_MS) {
        chatSystemModelOptions = cached.models.slice();
        if (ui.subview && !ui.subview.hidden && ui.subview.dataset.view === 'model') {
            renderChatSystemModelOptions(chatSystemModelOptions, chatSystemModelCurrent);
        }
        const cachedCount = [chatSystemModelCurrent].concat(chatSystemModelOptions)
            .map(function (model) { return String(model || '').trim(); })
            .filter(function (model, index, all) { return model && all.indexOf(model) === index; })
            .length;
        setChatSystemModelStatus(
            chatTranslate('chat.systemModelLoaded', '已获取 {count} 个模型').replace('{count}', String(cachedCount)),
            'success'
        );
        return;
    }
    if (ui.subview && !ui.subview.hidden && ui.subview.dataset.view === 'model') {
        ui.list.innerHTML = '';
    }
    setChatSystemModelStatus(chatTranslate('chat.systemModelLoading', '正在获取模型列表…'), 'loading');
    try {
        if (!String(channel.api_key || '').trim()) {
            throw new Error(chatTranslate('chat.systemModelNeedApiKey', '请先在系统设置中配置 API Key'));
        }
        const listResponse = await apiFetch('/api/config/list-models', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                provider: channel.provider || 'openai',
                base_url: String(channel.base_url || '').trim(),
                api_key: String(channel.api_key || '').trim()
            })
        });
        const result = await listResponse.json().catch(function () { return {}; });
        if (!listResponse.ok || !result.success) {
            throw new Error(result.error || chatTranslate('chat.systemModelLoadFailed', '获取模型失败'));
        }
        if (requestId !== chatSystemModelRequestSeq || ui.menu.hidden) return;
        chatSystemModelOptions = Array.isArray(result.models) ? result.models.slice() : [];
        chatSystemModelCache.set(cacheKey, {
            models: chatSystemModelOptions.slice(),
            fetchedAt: Date.now()
        });
        const count = [chatSystemModelCurrent].concat(chatSystemModelOptions)
            .map(function (model) { return String(model || '').trim(); })
            .filter(function (model, index, all) { return model && all.indexOf(model) === index; })
            .length;
        if (ui.subview && !ui.subview.hidden && ui.subview.dataset.view === 'model') {
            renderChatSystemModelOptions(chatSystemModelOptions, chatSystemModelCurrent);
        }
        setChatSystemModelStatus(
            chatTranslate('chat.systemModelLoaded', '已获取 {count} 个模型').replace('{count}', String(count)),
            'success'
        );
    } catch (error) {
        if (requestId !== chatSystemModelRequestSeq || ui.menu.hidden) return;
        chatSystemModelLoadError = error.message || chatTranslate('chat.systemModelLoadFailed', '获取模型失败');
        if (ui.subview && !ui.subview.hidden && ui.subview.dataset.view === 'model') {
            renderChatSystemModelRetry();
        }
        setChatSystemModelStatus(chatSystemModelLoadError, 'error');
    }
}

async function openChatSystemModelPicker(event) {
    if (event) {
        event.preventDefault();
        event.stopPropagation();
    }
    const ui = chatSystemModelElements();
    if (!ui.menu || !ui.button || !ui.list) return;
    if (!ui.menu.hidden) {
        closeChatSystemModelPicker();
        return;
    }
    if (chatSystemModelCloseTimer) {
        window.clearTimeout(chatSystemModelCloseTimer);
        chatSystemModelCloseTimer = null;
    }
    if (typeof closeChatReasoningPanel === 'function') closeChatReasoningPanel();
    ui.menu.hidden = false;
    ui.button.classList.add('active');
    ui.button.setAttribute('aria-expanded', 'true');
    if (ui.main) ui.main.hidden = false;
    if (ui.subview) ui.subview.hidden = true;
    ui.list.innerHTML = '';
    updateChatSystemModelPickerValues();
    await fetchChatSystemModelsForChannel(resolveChatPickerChannelId());
}

function truncateChatAIChannelSummaryLabel(label) {
    const chars = Array.from(String(label || ''));
    if (chars.length <= CHAT_AI_CHANNEL_SUMMARY_NAME_MAX) return chars.join('');
    return chars.slice(0, CHAT_AI_CHANNEL_SUMMARY_NAME_MAX).join('') + '...';
}

function persistChatAIChannelPref() {
    const id = selectedChatAIChannelId();
    try {
        if (id) localStorage.setItem(AI_CHANNEL_STORAGE_KEY, id);
        else localStorage.removeItem(AI_CHANNEL_STORAGE_KEY);
    } catch (e) {}
    updateChatReasoningSummary();
    updateChatSystemModelPickerValues();
}

function reasoningSummaryModeLabel(mode) {
    const m = (mode || 'default').trim();
    switch (m) {
        case 'off': return chatTranslate('chat.reasoningModeOff', '关闭');
        case 'on': return chatTranslate('chat.reasoningModeOn', '开启');
        case 'auto': return chatTranslate('chat.reasoningModeAuto', '自动');
        default: return chatTranslate('chat.reasoningSummaryFollow', '系统');
    }
}

function updateChatReasoningSummary() {
    const el = document.getElementById('chat-reasoning-summary');
    const modeEl = document.getElementById('chat-reasoning-mode');
    const effEl = document.getElementById('chat-reasoning-effort');
    if (!el || !modeEl) return;
    const mode = (modeEl.value || 'default').trim();
    const effort = effEl && effEl.value ? String(effEl.value).trim() : '';
    const t = (typeof window.t === 'function') ? window.t : function (k) { return k; };
    const modePart = reasoningSummaryModeLabel(mode);
    const reasoningPart = effort || modePart || t('chat.reasoningSummaryDash');
    let hitlPart = '';
    try {
        const hitlCfg = readHitlConfigFromForm();
        hitlPart = getHitlModeLabel(hitlCfg.mode);
    } catch (e) {
        hitlPart = '';
    }
    const channelPart = currentChatAIChannelLabel();
    const modelPart = currentChatModelLabel();
    el.textContent = hitlPart;
    el.title = hitlPart;
    updateChatComposerSessionShortcuts({
        channel: channelPart,
        model: modelPart,
        reasoning: reasoningPart,
        hitl: hitlPart
    });
}

function updateChatComposerSessionShortcuts(summary) {
    const data = summary || {};
    const modelEl = document.getElementById('chat-model-shortcut-text');
    const hitlEl = document.getElementById('chat-hitl-shortcut-text');
    if (modelEl) {
        // 输入框右侧展示当前会话通道的模型；审批模型只出现在 HITL 入口。
        const label = currentChatModelLabel();
        modelEl.textContent = truncateChatAIChannelSummaryLabel(label);
        modelEl.title = label;
        const shortcut = document.getElementById('chat-model-shortcut');
        if (shortcut) {
            const channel = currentChatAIChannelLabel();
            const effort = currentChatReasoningMenuLabel();
            const action = chatTranslate('chat.modelSettingsAria', '选择 AI 通道、模型与推理设置');
            shortcut.setAttribute('aria-label', action + '：' + channel + ' · ' + label + ' · ' + effort);
            shortcut.title = action + '：' + channel + ' · ' + label + ' · ' + effort;
        }
        updateChatSystemModelPickerValues();
    }
    if (hitlEl) {
        const cfg = readHitlConfigFromForm();
        const auditAgent = normalizeHitlReviewer(cfg.reviewer) === 'audit_agent';
        const prefix = auditAgent
            ? chatTranslate('chat.sessionShortcutAuditAgent', 'Agent 审查')
            : chatTranslate('chat.sessionShortcutHuman', '人工审批');
        const modeLabel = data.hitl || getHitlModeLabel(cfg.mode);
        const approvalModel = auditAgent ? currentHitlAuditEngineLabel() : '';
        const label = prefix + '：' + modeLabel + (approvalModel ? ' · ' + approvalModel : '');
        hitlEl.textContent = label;
        hitlEl.title = label;
    }
}

function openChatSessionSettings(section, event) {
    if (event && typeof event.stopPropagation === 'function') event.stopPropagation();
    mountChatSessionSettingsPopover();
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const toggle = document.getElementById('conversation-reasoning-toggle');
    if (!wrap || !toggle || wrap.style.display === 'none') return;
    syncChatReasoningBarHeight();
    wrap.classList.remove('conversation-reasoning-collapsed');
    syncChatSessionSettingsLayerState();
    toggle.setAttribute('aria-expanded', 'true');
    if (typeof closeAgentModePanel === 'function') closeAgentModePanel();
    if (typeof closeRoleSelectionPanel === 'function') closeRoleSelectionPanel();
    if (typeof closeChatProjectPanel === 'function') closeChatProjectPanel();
    updateChatReasoningSummary();

    let target = null;
    if (section === 'hitl') target = document.getElementById('hitl-mode-select');
    else if (section === 'reasoning') target = document.getElementById('chat-reasoning-mode');
    else target = document.getElementById('chat-ai-channel-select');
    const group = target && target.closest('.session-settings-group');
    if (group && typeof group.scrollIntoView === 'function') {
        group.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
    const customTrigger = target && target.closest('.session-settings-select')
        ? target.closest('.session-settings-select').querySelector('.session-settings-select-trigger')
        : null;
    window.setTimeout(function () {
        if (customTrigger) customTrigger.focus({ preventScroll: true });
        else if (target) target.focus({ preventScroll: true });
    }, 180);
}

function getVisibleChatConversationId() {
    return typeof currentConversationId === 'string' && currentConversationId.trim()
        ? currentConversationId.trim()
        : '';
}

function shouldTreatLiveChatTaskAsCurrent(liveConversationId, visibleConversationId, hasVisibleProgress) {
    const liveId = String(liveConversationId || '').trim();
    const visibleId = String(visibleConversationId || '').trim();
    if (liveId) return !!visibleId && liveId === visibleId;
    return hasVisibleProgress === true;
}

function ownsLiveChatStream(liveStream) {
    return !!liveStream && window.__csAgentLiveStream === liveStream;
}

function shouldIgnoreLiveChatStreamEvent(
    liveStream,
    activeLiveStream = window.__csAgentLiveStream,
    navigationSeq = chatConversationNavigationSeq
) {
    return !liveStream ||
        activeLiveStream !== liveStream ||
        liveStream.active !== true ||
        liveStream.detached === true ||
        liveStream.navigationSeq !== navigationSeq;
}

function clearLiveChatStreamIfOwned(liveStream) {
    if (!ownsLiveChatStream(liveStream)) return false;
    liveStream.active = false;
    window.__csAgentLiveStream = { active: false, conversationId: null, progressId: null };
    updateChatPrimaryActionState();
    return true;
}

/**
 * 离开正在读取主 POST 流的对话时，只断开浏览器侧响应流，不停止后端任务。
 * 后端任务使用 detachedAgentContext，仍会继续运行；重新进入该对话时由
 * task-events 镜像流接管。这样同时运行多个对话也只占用一个前台长连接，
 * 不会耗尽浏览器对同一主机的连接槽位而卡住普通 GET/POST 请求。
 */
function detachLiveChatStreamForNavigation(nextConversationId, force = false) {
    const liveStream = window.__csAgentLiveStream;
    if (!liveStream || !liveStream.active) return false;
    const liveConversationId = String(liveStream.conversationId || '').trim();
    const nextId = String(nextConversationId || '').trim();
    if (!force && liveConversationId && liveConversationId === nextId) return false;
    if (!force && !liveConversationId && !nextId) return false;

    liveStream.detached = true;
    liveStream.active = false;
    const controller = liveStream.abortController;
    if (controller && !controller.signal.aborted) {
        controller.abort();
    }
    if (ownsLiveChatStream(liveStream)) {
        updateChatPrimaryActionState();
    }
    return true;
}

function cancelPendingConversationLoad() {
    if (!loadConversationAbortController) return false;
    if (!loadConversationAbortController.signal.aborted) {
        loadConversationAbortController.abort();
    }
    loadConversationAbortController = null;
    return true;
}

function isLiveChatTaskVisible(live, visibleConversationId) {
    if (!live || !live.active) return false;
    const progress = live.progressId ? document.getElementById(live.progressId) : null;
    const hasVisibleProgress = !!(progress && progress.closest('#chat-messages'));
    return shouldTreatLiveChatTaskAsCurrent(
        live.conversationId,
        visibleConversationId,
        hasVisibleProgress
    );
}

function getCurrentChatTaskConversationId() {
    const visibleConversationId = getVisibleChatConversationId();
    if (visibleConversationId) return visibleConversationId;
    return '';
}

function isCurrentChatTaskActive() {
    const live = window.__csAgentLiveStream;
    const visibleConversationId = getVisibleChatConversationId();
    if (isLiveChatTaskVisible(live, visibleConversationId)) return true;
    return !!visibleConversationId &&
        typeof isConversationTaskRunning === 'function' &&
        isConversationTaskRunning(visibleConversationId);
}

function updateChatPrimaryActionState() {
    const button = document.getElementById('chat-send-btn');
    if (!button) return;
    const running = isCurrentChatTaskActive();
    const label = running
        ? chatTranslate('tasks.stopTask', '停止任务')
        : chatTranslate('chat.send', '发送');
    button.classList.toggle('is-task-running', running);
    button.setAttribute('aria-label', label);
    button.setAttribute('title', label);
    const labelElement = button.querySelector('.send-btn-label');
    if (labelElement) labelElement.textContent = label;
}

function handleChatPrimaryAction(event) {
    if (event) event.preventDefault();
    if (!isCurrentChatTaskActive()) {
        sendMessage();
        return;
    }

    const live = window.__csAgentLiveStream;
    const conversationId = getCurrentChatTaskConversationId();
    if (conversationId && typeof cancelActiveTask === 'function') {
        cancelActiveTask(conversationId);
        return;
    }
    if (live && live.progressId && typeof cancelProgressTask === 'function') {
        cancelProgressTask(live.progressId);
    }
}

function initChatPrimaryActionButton() {
    const button = document.getElementById('chat-send-btn');
    if (!button) return;
    if (!button.querySelector('.send-btn-stop-icon')) {
        const stopIcon = document.createElement('span');
        stopIcon.className = 'send-btn-stop-icon';
        stopIcon.setAttribute('aria-hidden', 'true');
        button.appendChild(stopIcon);
    }
    button.onclick = handleChatPrimaryAction;
    updateChatPrimaryActionState();
}

document.addEventListener('DOMContentLoaded', initChatPrimaryActionButton);

function closeChatReasoningPanel() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const toggle = document.getElementById('conversation-reasoning-toggle');
    if (wrap) wrap.classList.add('conversation-reasoning-collapsed');
    syncChatSessionSettingsLayerState();
    if (toggle) toggle.setAttribute('aria-expanded', 'false');
}

function toggleConversationReasoningCard() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const toggle = document.getElementById('conversation-reasoning-toggle');
    if (!wrap || !toggle) return;
    syncChatReasoningBarHeight();
    wrap.classList.toggle('conversation-reasoning-collapsed');
    syncChatSessionSettingsLayerState();
    const collapsed = wrap.classList.contains('conversation-reasoning-collapsed');
    toggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
    if (!collapsed) {
        if (typeof closeAgentModePanel === 'function') {
            closeAgentModePanel();
        }
        if (typeof closeRoleSelectionPanel === 'function') {
            closeRoleSelectionPanel();
        }
        updateChatReasoningSummary();
    }
}

function toggleChatReasoningPanel() {
    toggleConversationReasoningCard();
}

function restoreChatReasoningControlsFromStorage() {
    try {
        const m = document.getElementById('chat-reasoning-mode');
        const e = document.getElementById('chat-reasoning-effort');
        if (m) {
            const v = localStorage.getItem(REASONING_MODE_LS);
            if (v && ['default', 'off', 'on', 'auto'].indexOf(v) !== -1) {
                m.value = v;
            }
        }
        if (e) {
            const v = localStorage.getItem(REASONING_EFFORT_LS);
            if (v !== null && ['', 'low', 'medium', 'high', 'max', 'xhigh'].indexOf(v) !== -1) {
                e.value = v;
            }
        }
        refreshSessionSettingsSelects();
        updateChatReasoningSummary();
    } catch (err) { /* ignore */ }
}

function persistChatReasoningPrefs() {
    try {
        const m = document.getElementById('chat-reasoning-mode');
        const elEff = document.getElementById('chat-reasoning-effort');
        if (m) localStorage.setItem(REASONING_MODE_LS, m.value || 'default');
        if (elEff) localStorage.setItem(REASONING_EFFORT_LS, elEff.value || '');
        refreshSessionSettingsSelects();
        updateChatReasoningSummary();
    } catch (err) { /* ignore */ }
}

/** 供 WebShell 等复用：在 Eino 路径下返回 reasoning 请求片段或 undefined */
function buildReasoningRequestPayload() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    if (!wrap || wrap.style.display === 'none') {
        return undefined;
    }
    const modeEl = document.getElementById('chat-reasoning-mode');
    const effEl = document.getElementById('chat-reasoning-effort');
    if (!modeEl) return undefined;
    const mode = (modeEl.value || 'default').trim();
    const effort = effEl && effEl.value ? String(effEl.value).trim() : '';
    if (mode === 'default' && !effort) {
        return undefined;
    }
    const o = {};
    if (mode !== 'default') o.mode = mode;
    if (effort) o.effort = effort;
    return Object.keys(o).length ? o : undefined;
}

if (typeof window !== 'undefined') {
    window.persistChatAIChannelPref = persistChatAIChannelPref;
    window.populateChatAIChannelSelect = populateChatAIChannelSelect;
    window.persistChatReasoningPrefs = persistChatReasoningPrefs;
    window.buildReasoningRequestPayload = buildReasoningRequestPayload;
    window.closeChatReasoningPanel = closeChatReasoningPanel;
    window.toggleChatReasoningPanel = toggleChatReasoningPanel;
    window.toggleConversationReasoningCard = toggleConversationReasoningCard;
    window.updateChatReasoningSummary = updateChatReasoningSummary;
    window.updateChatComposerSessionShortcuts = updateChatComposerSessionShortcuts;
    window.openChatSessionSettings = openChatSessionSettings;
    window.openChatSystemModelPicker = openChatSystemModelPicker;
    window.openChatSystemModelView = openChatSystemModelView;
    window.closeChatSystemModelPicker = closeChatSystemModelPicker;
    window.refreshSessionSettingsSelects = refreshSessionSettingsSelects;
    window.updateChatPrimaryActionState = updateChatPrimaryActionState;
}

function closeAgentModePanel() {
    const panel = document.getElementById('agent-mode-panel');
    const btn = document.getElementById('agent-mode-btn');
    if (panel) panel.style.display = 'none';
    if (btn) {
        btn.classList.remove('active');
        btn.setAttribute('aria-expanded', 'false');
    }
}

function toggleAgentModePanel() {
    const panel = document.getElementById('agent-mode-panel');
    const btn = document.getElementById('agent-mode-btn');
    if (!panel || !btn) return;
    const isOpen = panel.style.display === 'flex';
    if (isOpen) {
        closeAgentModePanel();
        return;
    }
    if (typeof closeChatReasoningPanel === 'function') {
        closeChatReasoningPanel();
    }
    if (typeof closeRoleSelectionPanel === 'function') {
        closeRoleSelectionPanel();
    }
    if (typeof closeChatProjectPanel === 'function') {
        closeChatProjectPanel();
    }
    panel.style.display = 'flex';
    btn.classList.add('active');
    btn.setAttribute('aria-expanded', 'true');
}

function selectAgentMode(mode) {
    const ok = chatAgentModeIsEinoSingle(mode) || chatAgentModeIsEino(mode);
    if (!ok) return;
    saveConversationAgentModePreference(currentConversationId, mode);
    try {
        localStorage.setItem(AGENT_MODE_STORAGE_KEY, mode);
    } catch (e) { /* ignore */ }
    syncAgentModeFromValue(mode);
    closeAgentModePanel();
}

async function initChatAgentModeFromConfig() {
    const wrap = document.getElementById('agent-mode-wrapper');
    const sel = document.getElementById('agent-mode-select');
    if (!wrap || !sel) return;

    // 先展示基础模式，避免首次登录时配置接口短暂失败导致入口被隐藏。
    wrap.style.display = '';
    let stored = localStorage.getItem(AGENT_MODE_STORAGE_KEY);
    if (!(chatAgentModeIsEinoSingle(stored) || chatAgentModeIsEino(stored))) {
        stored = CHAT_AGENT_MODE_EINO_SINGLE;
    }
    sel.value = stored;
    syncAgentModeFromValue(stored);
    document.querySelectorAll('.agent-mode-option').forEach(function (el) {
        const v = el.getAttribute('data-value');
        if (v === 'deep' || v === 'plan_execute' || v === 'supervisor') {
            el.style.display = 'none';
        } else {
            el.style.display = '';
        }
    });
    restoreChatReasoningControlsFromStorage();
    syncReasoningRowVisibility(stored);

    try {
        const r = await apiFetch('/api/config');
        if (!r.ok) return;
        const cfg = await r.json();
        multiAgentAPIEnabled = !!(cfg.multi_agent && cfg.multi_agent.enabled);
        populateChatAIChannelSelect(cfg.ai || {});
        const hitlAuditModel = cfg.hitl && cfg.hitl.audit_model;
        chatHitlAuditBackend = cfg.hitl && typeof cfg.hitl.audit_backend === 'string'
            ? cfg.hitl.audit_backend.trim().toLowerCase()
            : '';
        chatHitlAuditModelName = hitlAuditModel && typeof hitlAuditModel.model === 'string'
            ? hitlAuditModel.model.trim()
            : '';
        if (typeof window !== 'undefined') {
            window.csaiHitlAuditBackend = chatHitlAuditBackend;
            window.csaiHitlAuditModel = chatHitlAuditModelName;
            if (typeof window.renderHitlPageAuditEngine === 'function') {
                window.renderHitlPageAuditEngine();
            }
            if (typeof window.renderHitlStrategyJevHint === 'function') {
                window.renderHitlStrategyJevHint();
            }
        }
        updateChatReasoningSummary();
        if (typeof window !== 'undefined') {
            window.__csaiMultiAgentPublic = cfg.multi_agent || null;
            const tw = cfg.hitl && cfg.hitl.tool_whitelist;
            if (Array.isArray(tw)) {
                window.csaiHitlGlobalToolWhitelist = tw.slice();
            }
        }
        if (typeof window.refreshHitlPageWhitelist === 'function') {
            window.refreshHitlPageWhitelist();
        }
        document.querySelectorAll('.agent-mode-option').forEach(function (el) {
            const v = el.getAttribute('data-value');
            if (v === 'deep' || v === 'plan_execute' || v === 'supervisor') {
                el.style.display = multiAgentAPIEnabled ? '' : 'none';
            } else {
                el.style.display = '';
            }
        });
        stored = chatAgentModeNormalizeStored(stored, cfg);
        try {
            localStorage.setItem(AGENT_MODE_STORAGE_KEY, stored);
        } catch (e) { /* ignore */ }
        sel.value = stored;
        syncAgentModeFromValue(stored);
        restoreChatReasoningControlsFromStorage();
        syncReasoningRowVisibility(stored);
    } catch (e) {
        console.warn('initChatAgentModeFromConfig', e);
    }
}

document.addEventListener('languagechange', function () {
    const hid = document.getElementById('agent-mode-select');
    if (!hid) return;
    const v = hid.value;
    if (chatAgentModeIsEinoSingle(v) || chatAgentModeIsEino(v)) {
        syncAgentModeFromValue(v);
    }
    if (typeof updateChatReasoningSummary === 'function') {
        updateChatReasoningSummary();
    }
});

// 保存输入框草稿到localStorage（防抖版本）
function saveChatDraftDebounced(content) {
    // 清除之前的定时器
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
    }

    // 设置新的定时器
    draftSaveTimer = setTimeout(() => {
        saveChatDraft(content);
    }, DRAFT_SAVE_DELAY);
}

// 保存输入框草稿到localStorage
function saveChatDraft(content) {
    try {
        const chatInput = document.getElementById('chat-input');
        const placeholderText = chatInput ? (chatInput.getAttribute('placeholder') || '').trim() : '';
        const trimmed = (content || '').trim();

        // 不要把占位提示本身当作草稿保存
        if (trimmed && (!placeholderText || trimmed !== placeholderText)) {
            localStorage.setItem(DRAFT_STORAGE_KEY, content);
        } else {
            // 如果内容为空或等于占位提示，清除保存的草稿
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        }
    } catch (error) {
        // localStorage可能已满或不可用，静默失败
        console.warn('保存草稿失败:', error);
    }
}

// 从localStorage恢复输入框草稿
function restoreChatDraft() {
    try {
        const chatInput = document.getElementById('chat-input');
        if (!chatInput) {
            return;
        }
        const placeholderText = (chatInput.getAttribute('placeholder') || '').trim();
        // 若当前 value 与 placeholder 相同，说明提示被误当作内容，清空以便正确显示占位符
        if (placeholderText && chatInput.value.trim() === placeholderText) {
            chatInput.value = '';
        }
        // 如果输入框已有内容，不恢复草稿（避免覆盖用户输入）
        if (chatInput.value && chatInput.value.trim().length > 0) {
            return;
        }

        const draft = localStorage.getItem(DRAFT_STORAGE_KEY);
        const trimmedDraft = draft ? draft.trim() : '';

        // 如果草稿内容和占位提示一样，则认为是无效草稿，不恢复
        if (trimmedDraft && (!placeholderText || trimmedDraft !== placeholderText)) {
            chatInput.value = draft;
            // 调整输入框高度以适应内容
            adjustTextareaHeight(chatInput);
        } else if (trimmedDraft && placeholderText && trimmedDraft === placeholderText) {
            // 清理掉无效草稿，避免之后继续干扰
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        }
    } catch (error) {
        console.warn('恢复草稿失败:', error);
    }
}

// 清除保存的草稿
function clearChatDraft() {
    try {
        // 同步清除，确保立即生效
        localStorage.removeItem(DRAFT_STORAGE_KEY);
    } catch (error) {
        console.warn('清除草稿失败:', error);
    }
}

// 调整textarea高度以适应内容
function adjustTextareaHeight(textarea) {
    if (!textarea) return;

    // 先重置高度为auto，然后立即设置为固定值，确保能准确获取scrollHeight
    textarea.style.height = 'auto';
    // 强制浏览器重新计算布局
    void textarea.offsetHeight;

    // 计算新高度（最小40px，最大不超过300px）
    const scrollHeight = textarea.scrollHeight;
    const newHeight = Math.min(Math.max(scrollHeight, 40), 300);
    textarea.style.height = newHeight + 'px';

    // 如果内容为空或只有很少内容，立即重置到最小高度
    if (!textarea.value || textarea.value.trim().length === 0) {
        textarea.style.height = '40px';
    }
}

// 发送消息
async function sendMessage() {
    const input = document.getElementById('chat-input');
    let message = input.value.trim();
    const hasAttachments = chatAttachments && chatAttachments.length > 0;
    const requestConversationId = currentConversationId;
    const requestNavigationSeq = chatConversationNavigationSeq;

    if (!message && !hasAttachments) {
        return;
    }

    // A restored conversation renders from the local cache first, while its
    // authoritative HITL config is fetched separately. Do not let a fast send
    // reuse the temporary/default reviewer (historically "human") before that
    // fetch completes, otherwise refreshing could turn Audit Agent review into
    // a human approval for the next tool call.
    const hitlConversationAtSendStart = String(currentConversationId || '').trim();
    await waitForHitlConfigReady(hitlConversationAtSendStart);
    if (String(currentConversationId || '').trim() !== hitlConversationAtSendStart) return;

    // Enter 会直接调用 sendMessage；同一会话在其他标签页已启动任务时，
    // 必须在渲染用户气泡和发起 POST 前做一次权威状态同步，避免生成一轮“已有任务执行中”伪对话。
    if (currentConversationId && typeof loadActiveTasks === 'function') {
        await loadActiveTasks();
    }
    if (isCurrentChatTaskActive()) {
        updateChatPrimaryActionState();
        showChatToast(chatTranslate('chat.taskAlreadyRunning', '当前会话已有任务正在执行，请先等待完成或停止任务。'), 'info');
        return;
    }

    if (hasAttachments) {
        const needWait = chatAttachments.some((a) => a.uploading);
        if (needWait) {
            const waitLabel = (typeof window.t === 'function')
                ? window.t('chat.waitingAttachmentsUpload')
                : '正在等待附件上传完成…';
            chatAttachmentProgressSet(true, 0, waitLabel);
        }
        try {
            await Promise.all(chatAttachments.map((a) => (a.uploadPromise ? a.uploadPromise : Promise.resolve())));
        } finally {
            refreshChatAttachmentUploadProgress();
        }
        const bad = chatAttachments.filter((a) => !a.serverPath);
        if (bad.length) {
            const hint = (typeof window.t === 'function')
                ? window.t('chat.attachmentsUploadIncomplete')
                : '部分附件未上传成功，请移除失败项或重新选择文件后再发送。';
            alert(hint);
            return;
        }
    }

    // 有附件且用户未输入时，发一句简短默认提示即可（后端会拼接路径和文件内容给大模型）
    if (hasAttachments && !message) {
        message = CHAT_FILE_DEFAULT_PROMPT;
    }

    // 发送前的任务状态/附件检查可能包含异步等待。若用户已主动切换会话，
    // 保留当前页面，不再把这次尚未发出的请求写入新的可见对话。
    if (requestNavigationSeq !== chatConversationNavigationSeq) {
        return;
    }

    // 显示用户消息（含附件名，便于用户确认）
    const displayMessage = hasAttachments
        ? message + '\n' + chatAttachments.map(a => '📎 ' + a.fileName).join('\n')
        : message;
    if (window.CyberStrikeChatScroll) {
        window.CyberStrikeChatScroll.onUserSendMessage();
    }
    addMessage('user', displayMessage, null, null, null, { scroll: 'none' });
    if (currentConversationId) {
        invalidateConversationLiteCache(currentConversationId);
    }

    // 清除防抖定时器，防止在清空输入框后重新保存草稿
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
        draftSaveTimer = null;
    }

    // 立即清除草稿，防止页面刷新时恢复
    clearChatDraft();
    // 使用同步方式确保草稿被清除
    try {
        localStorage.removeItem(DRAFT_STORAGE_KEY);
    } catch (e) {
        // 忽略错误
    }

    // 立即清空输入框并清除草稿（在发送请求之前）
    input.value = '';
    // 强制重置输入框高度为初始高度（40px）
    input.style.height = '40px';

    // 构建请求体（含附件）
    const body = {
        message: message,
        conversationId: requestConversationId,
        role: typeof getCurrentRole === 'function' ? getCurrentRole() : ''
    };
    if (window.__csNextChatFinalizationPolicy && typeof window.__csNextChatFinalizationPolicy === 'object') {
        body.finalization = window.__csNextChatFinalizationPolicy;
        window.__csNextChatFinalizationPolicy = null;
    }
    let streamConversationId = body.conversationId ? String(body.conversationId) : null;
    const isStreamStillVisibleForRequest = function () {
        if (!document.getElementById(progressId)) return false;
        if (!streamConversationId) return currentConversationId === body.conversationId;
        return currentConversationId === streamConversationId;
    };
    if (!currentConversationId && typeof getActiveProjectId === 'function') {
        const pid = getActiveProjectId();
        if (pid) body.projectId = pid;
    }
    const aiChannelId = selectedChatAIChannelId();
    if (aiChannelId) {
        body.aiChannelId = aiChannelId;
    }
    const hitlCfg = readHitlConfigFromForm();
    if (normalizeHitlMode(hitlCfg.mode) !== HITL_MODE_OFF) {
        const sensitiveTools = hitlToolsSplitToArray(hitlCfg.sensitiveTools || '');
        body.hitl = {
            enabled: true,
            mode: normalizeHitlMode(hitlCfg.mode),
            reviewer: normalizeHitlReviewer(hitlCfg.reviewer),
            sensitiveTools: sensitiveTools,
            timeoutSeconds: normalizeHitlTimeoutForChat(hitlCfg.timeoutSeconds, DEFAULT_HITL_TIMEOUT_SECONDS)
        };
    }
    if (hasAttachments) {
        body.attachments = chatAttachments.map((a) => ({
            fileName: a.fileName,
            mimeType: a.mimeType || '',
            serverPath: a.serverPath
        }));
    }
    const reasoningPayload = buildReasoningRequestPayload();
    if (reasoningPayload) {
        body.reasoning = reasoningPayload;
    }
    // 发送后清空附件列表
    chatAttachments = [];
    renderChatFileChips();

    // 创建进度消息容器（使用详细的进度展示）
    const progressId = addProgressMessage();
    if (window.CyberStrikeChatScroll) {
        window.CyberStrikeChatScroll.markProgressStreaming(true, progressId);
        window.CyberStrikeChatScroll.onUserSendMessage();
    }
    const progressElement = document.getElementById(progressId);
    registerProgressTask(progressId, streamConversationId);
    const requestAbortController = new AbortController();
    const liveStreamState = {
        active: true,
        conversationId: streamConversationId || null,
        progressId: progressId,
        abortController: requestAbortController,
        detached: false,
        navigationSeq: requestNavigationSeq
    };
    window.__csAgentLiveStream = liveStreamState;
    if (streamConversationId && typeof window.notifyConversationTaskStarted === 'function') {
        window.notifyConversationTaskStarted(streamConversationId);
    }
    updateChatPrimaryActionState();
    loadActiveTasks();
    let assistantMessageId = null;
    let mcpExecutionIds = [];

    try {
        const modeSel = document.getElementById('agent-mode-select');
        let modeVal = modeSel ? modeSel.value : CHAT_AGENT_MODE_EINO_SINGLE;
        saveConversationAgentModePreference(streamConversationId || currentConversationId, modeVal);
        const useMulti = multiAgentAPIEnabled && chatAgentModeIsEino(modeVal);
        const streamPath = useMulti ? '/api/multi-agent/stream' : '/api/eino-agent/stream';
        if (useMulti && modeVal) {
            body.orchestration = modeVal;
        }
        const response = await apiFetch(streamPath, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(body),
            signal: requestAbortController.signal,
        });

        if (!response.ok) {
            throw new Error('请求失败: ' + response.status);
        }

        liveStreamState.conversationId = streamConversationId || null;
        try {
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            let streamSawDone = false;
            const dispatchStreamEvent = function (eventData) {
                if (eventData && eventData.type === 'done') {
                    streamSawDone = true;
                }
                const eventConvId = eventData && eventData.data && eventData.data.conversationId
                    ? String(eventData.data.conversationId)
                    : '';
                let justBoundConversation = false;
                if (eventConvId) {
                    if (streamConversationId && streamConversationId !== eventConvId) {
                        return;
                    }
                    if (!streamConversationId) {
                        streamConversationId = eventConvId;
                        liveStreamState.conversationId = eventConvId;
                        justBoundConversation = true;
                    }
                }
                // 切换对话后仍可能收到旧响应流中已缓冲的 conversation、response_start
                // 或 response 事件。它们只能补齐后台任务归属，不能重新抢占当前对话。
                if (shouldIgnoreLiveChatStreamEvent(liveStreamState)) {
                    if (eventConvId) updateProgressConversation(progressId, eventConvId);
                    return;
                }
                if (!justBoundConversation && !isStreamStillVisibleForRequest()) {
                    return;
                }
                handleStreamEvent(eventData, progressElement, progressId,
                    () => assistantMessageId, (id) => { assistantMessageId = id; },
                    () => mcpExecutionIds, (ids) => { mcpExecutionIds = ids; },
                    { conversationId: streamConversationId });
            };
            const processSseLines = typeof processSseDataLinesYielding === 'function'
                ? processSseDataLinesYielding
                : async function (lines, onEvent) {
                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            try {
                                onEvent(JSON.parse(line.slice(6)));
                            } catch (e) {
                                console.error('解析事件数据失败:', e, line);
                            }
                        }
                    }
                };

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop(); // 保留最后一个不完整的行

                await processSseLines(lines, dispatchStreamEvent);
            }
            // Flush decoder internal buffer to avoid losing the final partial UTF-8 code point.
            buffer += decoder.decode();

            // 处理剩余的buffer
            if (buffer.trim()) {
                const lines = buffer.split('\n');
                await processSseLines(lines, dispatchStreamEvent);
            }
            if (!streamSawDone) {
                if (typeof loadActiveTasks === 'function') {
                    loadActiveTasks();
                }
                const convId = streamConversationId || (body && body.conversationId) || null;
                let attached = false;
                if (
                    convId &&
                    ownsLiveChatStream(liveStreamState) &&
                    !liveStreamState.detached &&
                    isStreamStillVisibleForRequest() &&
                    typeof window.attachRunningTaskEventStream === 'function'
                ) {
                    clearLiveChatStreamIfOwned(liveStreamState);
                    attached = await window.attachRunningTaskEventStream(convId).catch(() => false);
                }
                if (!attached && isStreamStillVisibleForRequest()) {
                    const hint = typeof window.t === 'function'
                        ? window.t('chat.streamEndedWithoutDone')
                        : '连接提前结束，未收到任务完成信号。任务可能仍在后端执行，请查看顶部运行中任务或刷新当前对话。';
                    addMessage('system', hint);
                }
            }
        } finally {
            const clearedOwnedStream = clearLiveChatStreamIfOwned(liveStreamState);
            if (clearedOwnedStream && !liveStreamState.detached && window.CyberStrikeChatScroll) {
                window.CyberStrikeChatScroll.onStreamEnd();
            }
        }

        // 消息发送成功后，再次确保草稿被清除
        clearChatDraft();
        try {
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        } catch (e) {
            // 忽略错误
        }

    } catch (error) {
        clearLiveChatStreamIfOwned(liveStreamState);
        if (liveStreamState.detached || !isStreamStillVisibleForRequest()) {
            if (typeof loadActiveTasks === 'function') {
                loadActiveTasks();
            }
            return;
        }
        removeMessage(progressId);
        const msg = error && error.message != null ? String(error.message) : String(error);
        const isNetwork = /network|fetch|Failed to fetch|aborted|AbortError|load failed|NetworkError/i.test(msg);
        if (isNetwork && typeof window.t === 'function') {
            addMessage('system', window.t('chat.streamNetworkErrorHint', { detail: msg }));
        } else if (isNetwork) {
            addMessage('system', '连接已中断（' + msg + '）。长时间任务可能仍在后端执行，请查看顶部运行中任务或稍后刷新对话。');
        } else {
            addMessage('system', '错误: ' + msg);
        }
        if (typeof loadActiveTasks === 'function') {
            loadActiveTasks();
        }
        // 发送失败时，不恢复草稿，因为消息已经显示在对话框中了
    }
}

// ---------- 对话文件上传 ----------
function renderChatFileChips() {
    const list = document.getElementById('chat-file-list');
    if (!list) return;
    list.innerHTML = '';
    if (!chatAttachments.length) return;
    chatAttachments.forEach((a, i) => {
        const chip = document.createElement('div');
        chip.className = 'chat-file-chip';
        if (a.uploading) chip.classList.add('chat-file-chip--uploading');
        if (a.uploadError) chip.classList.add('chat-file-chip--error');
        chip.setAttribute('role', 'listitem');
        const name = document.createElement('span');
        name.className = 'chat-file-chip-name';
        name.title = a.fileName;
        let label = a.fileName;
        if (a.uploading) {
            label += ' · ' + ((typeof window.t === 'function') ? window.t('chat.attachmentUploading') : '上传中…');
        } else if (a.uploadError) {
            label += ' · ' + ((typeof window.t === 'function') ? window.t('chat.attachmentUploadFailed') : '失败');
        }
        name.textContent = label;
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'chat-file-chip-remove';
        remove.title = typeof window.t === 'function' ? window.t('common.remove') : '移除';
        remove.innerHTML = '×';
        remove.setAttribute('aria-label', '移除 ' + a.fileName);
        remove.addEventListener('click', () => removeChatAttachment(i));
        chip.appendChild(name);
        chip.appendChild(remove);
        list.appendChild(chip);
    });
}

function removeChatAttachment(index) {
    chatAttachments.splice(index, 1);
    renderChatFileChips();
    refreshChatAttachmentUploadProgress();
}

// 有附件且输入框为空时，填入一句默认提示（可编辑）；后端会单独拼接路径与内容给大模型
function appendChatFilePrompt() {
    const input = document.getElementById('chat-input');
    if (!input || !chatAttachments.length) return;
    if (!input.value.trim()) {
        input.value = CHAT_FILE_DEFAULT_PROMPT;
        adjustTextareaHeight(input);
    }
}

function chatAttachmentProgressSet(visible, percent, detailText) {
    const wrap = document.getElementById('chat-attachment-progress');
    const fill = document.getElementById('chat-attachment-progress-fill');
    const label = document.getElementById('chat-attachment-progress-label');
    if (!wrap || !fill || !label) return;
    if (!visible) {
        wrap.hidden = true;
        fill.style.width = '0%';
        label.textContent = '';
        return;
    }
    wrap.hidden = false;
    const p = Math.min(100, Math.max(0, Math.round(percent)));
    fill.style.width = p + '%';
    label.textContent = detailText || '';
}

function refreshChatAttachmentUploadProgress() {
    if (!chatAttachments.length) {
        chatAttachmentProgressSet(false);
        return;
    }
    const uploading = chatAttachments.filter((a) => a.uploading);
    if (!uploading.length) {
        chatAttachmentProgressSet(false);
        return;
    }
    let sum = 0;
    chatAttachments.forEach((a) => {
        sum += a.uploading ? (a.uploadPercent || 0) : 100;
    });
    const overall = Math.round(sum / chatAttachments.length);
    const line = (typeof window.t === 'function')
        ? window.t('chat.uploadingAttachmentsDetail', {
            done: chatAttachments.length - uploading.length,
            total: chatAttachments.length,
            percent: overall
        })
        : ('上传附件 ' + (chatAttachments.length - uploading.length) + '/' + chatAttachments.length + ' · ' + overall + '%');
    chatAttachmentProgressSet(true, overall, line);
}

async function uploadOneChatAttachment(entry, file) {
    const form = new FormData();
    form.append('file', file);
    const conv = currentConversationId;
    if (conv && String(conv).trim()) {
        form.append('conversationId', String(conv).trim());
    }
    const entryId = entry.id;
    try {
        const res = typeof apiUploadWithProgress === 'function'
            ? await apiUploadWithProgress('/api/chat-uploads', form, {
                onProgress: function (p) {
                    const cur = chatAttachments.find((x) => x.id === entryId);
                    if (cur) {
                        cur.uploadPercent = p.percent;
                        refreshChatAttachmentUploadProgress();
                    }
                }
            })
            : await apiFetch('/api/chat-uploads', { method: 'POST', body: form });
        if (!res.ok) {
            throw new Error(await res.text());
        }
        const data = await res.json().catch(() => ({}));
        const abs = data.absolutePath ? String(data.absolutePath).trim() : '';
        if (!abs) {
            throw new Error('no absolutePath in response');
        }
        const cur = chatAttachments.find((x) => x.id === entryId);
        if (cur) {
            cur.serverPath = abs;
            cur.uploading = false;
            cur.uploadPercent = 100;
            cur.uploadError = null;
        }
    } catch (e) {
        const msg = (e && e.message) ? e.message : String(e);
        const cur = chatAttachments.find((x) => x.id === entryId);
        if (cur) {
            cur.uploading = false;
            cur.uploadError = msg;
            cur.serverPath = null;
        }
        alert(((typeof window.t === 'function') ? window.t('chat.attachmentUploadAlert', { name: file.name }) : ('上传失败：' + file.name)) + '\n' + msg);
    }
    renderChatFileChips();
    refreshChatAttachmentUploadProgress();
}

async function addFilesToChat(files) {
    if (!files || !files.length) return;
    const next = Array.from(files);
    if (chatAttachments.length + next.length > MAX_CHAT_FILES) {
        alert('最多同时上传 ' + MAX_CHAT_FILES + ' 个文件，当前已选 ' + chatAttachments.length + ' 个。');
        return;
    }
    next.forEach((file) => {
        const id = ++chatAttachmentSeq;
        const entry = {
            id: id,
            fileName: file.name,
            mimeType: file.type || '',
            serverPath: null,
            uploading: true,
            uploadPercent: 0,
            uploadPromise: null,
            uploadError: null
        };
        entry.uploadPromise = uploadOneChatAttachment(entry, file);
        chatAttachments.push(entry);
    });
    renderChatFileChips();
    refreshChatAttachmentUploadProgress();
    appendChatFilePrompt();
}

function setupChatFileUpload() {
    const inputEl = document.getElementById('chat-file-input');
    const container = document.getElementById('chat-input-container') || document.querySelector('.chat-input-container');
    if (!inputEl || !container) return;

    inputEl.addEventListener('change', function () {
        const files = this.files;
        if (files && files.length) {
            addFilesToChat(files).catch(function () { /* addFilesToChat 已提示 */ });
        }
        this.value = '';
    });

    container.addEventListener('dragover', function (e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.add('drag-over');
    });
    container.addEventListener('dragleave', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (!this.contains(e.relatedTarget)) {
            this.classList.remove('drag-over');
        }
    });
    container.addEventListener('drop', function (e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.remove('drag-over');
        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length) addFilesToChat(files).catch(function () { /* addFilesToChat 已提示 */ });
    });
}

// 确保 chat-input-container 有 id（若模板未写）
function ensureChatInputContainerId() {
    const c = document.querySelector('.chat-input-container');
    if (c && !c.id) c.id = 'chat-input-container';
}

function setupMentionSupport() {
    mentionSuggestionsEl = document.getElementById('mention-suggestions');
    if (mentionSuggestionsEl) {
        mentionSuggestionsEl.style.display = 'none';
        mentionSuggestionsEl.addEventListener('mousedown', (event) => {
            // 防止点击候选项时输入框失焦
            event.preventDefault();
        });
    }
    ensureMentionToolsLoaded().catch(() => {
        // 忽略加载错误，稍后可重试
    });
}

// 刷新工具列表（重置已加载状态，强制重新加载）
function refreshMentionTools() {
    mentionToolsLoaded = false;
    mentionTools = [];
    externalMcpNames = [];
    mentionToolsLoadingPromise = null;
    // 如果当前正在使用@功能，立即触发重新加载
    if (mentionState.active) {
        ensureMentionToolsLoaded().catch(() => {
            // 忽略加载错误
        });
    }
}

// 将刷新函数暴露到window对象，供其他模块调用
if (typeof window !== 'undefined') {
    window.refreshMentionTools = refreshMentionTools;
}

function ensureMentionToolsLoaded() {
    // 检查角色是否改变，如果改变则强制重新加载
    if (typeof window !== 'undefined' && window._mentionToolsRoleChanged) {
        mentionToolsLoaded = false;
        mentionTools = [];
        delete window._mentionToolsRoleChanged;
    }

    if (mentionToolsLoaded) {
        return Promise.resolve(mentionTools);
    }
    if (mentionToolsLoadingPromise) {
        return mentionToolsLoadingPromise;
    }
    mentionToolsLoadingPromise = fetchMentionTools().finally(() => {
        mentionToolsLoadingPromise = null;
    });
    return mentionToolsLoadingPromise;
}

// 生成工具的唯一标识符，用于区分同名但来源不同的工具
function getToolKeyForMention(tool) {
    // 如果是外部工具，使用 external_mcp::tool.name 作为唯一标识
    // 如果是内部工具，使用 tool.name 作为标识
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    return tool.name;
}

async function fetchMentionTools() {
    const pageSize = 100;
    let page = 1;
    let totalPages = 1;
    const seen = new Set();
    const collected = [];

    try {
        // 获取当前选中的角色（从 roles.js 的函数获取）
        const roleName = typeof getCurrentRole === 'function' ? getCurrentRole() : '';

        // 同时获取外部MCP列表
        try {
            const mcpResponse = await apiFetch('/api/external-mcp');
            if (mcpResponse.ok) {
                const mcpData = await mcpResponse.json();
                externalMcpNames = Object.keys(mcpData.servers || {}).filter(name => {
                    const server = mcpData.servers[name];
                    // 只包含已连接且已启用的MCP
                    return server.status === 'connected' &&
                           (server.config.external_mcp_enable || (server.config.enabled && !server.config.disabled));
                });
            }
        } catch (mcpError) {
            console.warn('加载外部MCP列表失败:', mcpError);
            externalMcpNames = [];
        }

        while (page <= totalPages && page <= 20) {
            // 构建API URL，如果指定了角色，添加role查询参数
            let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            if (roleName && roleName !== '默认') {
                url += `&role=${encodeURIComponent(roleName)}`;
            }

            const response = await apiFetch(url);
            if (!response.ok) {
                break;
            }
            const result = await response.json();
            const tools = Array.isArray(result.tools) ? result.tools : [];
            tools.forEach(tool => {
                if (!tool || !tool.name) {
                    return;
                }
                // 使用唯一标识符来去重，而不是只使用工具名称
                const toolKey = getToolKeyForMention(tool);
                if (seen.has(toolKey)) {
                    return;
                }
                seen.add(toolKey);

                // 确定工具在当前角色中的启用状态
                // 如果有 role_enabled 字段，使用它（表示指定了角色）
                // 否则使用 enabled 字段（表示未指定角色或使用所有工具）
                let roleEnabled = tool.enabled !== false;
                if (tool.role_enabled !== undefined && tool.role_enabled !== null) {
                    roleEnabled = tool.role_enabled;
                }

                collected.push({
                    name: tool.name,
                    description: tool.description || '',
                    enabled: tool.enabled !== false, // 工具本身的启用状态
                    roleEnabled: roleEnabled, // 在当前角色中的启用状态
                    isExternal: !!tool.is_external,
                    externalMcp: tool.external_mcp || '',
                    toolKey: toolKey, // 保存唯一标识符
                });
            });
            totalPages = result.total_pages || 1;
            page += 1;
            if (page > totalPages) {
                break;
            }
        }
        mentionTools = collected;
        mentionToolsLoaded = true;
    } catch (error) {
        console.warn('加载工具列表失败，@提及功能可能不可用:', error);
    }
    return mentionTools;
}

function handleChatInputInput(event) {
    const textarea = event.target;
    updateMentionStateFromInput(textarea);
    // 自动调整输入框高度
    // 使用requestAnimationFrame确保在DOM更新后立即调整，特别是在删除内容时
    requestAnimationFrame(() => {
        adjustTextareaHeight(textarea);
    });
    // 保存输入内容到localStorage（防抖）
    saveChatDraftDebounced(textarea.value);
}

function handleChatInputClick(event) {
    updateMentionStateFromInput(event.target);
}

function handleChatInputKeydown(event) {
    // 如果正在使用输入法输入（IME），回车键应该用于确认候选词，而不是发送消息
    // Safari 可能在确认候选词时先触发 compositionend，再触发 Enter keydown，
    // 因此这里同时使用全局状态和 keyCode 229 兜底。
    if (event.isComposing || isComposing || event.keyCode === 229) {
        return;
    }

    if (mentionState.active && mentionSuggestionsEl && mentionSuggestionsEl.style.display !== 'none') {
        if (event.key === 'ArrowDown') {
            event.preventDefault();
            moveMentionSelection(1);
            return;
        }
        if (event.key === 'ArrowUp') {
            event.preventDefault();
            moveMentionSelection(-1);
            return;
        }
        if (event.key === 'Enter' || event.key === 'Tab') {
            event.preventDefault();
            applyMentionSelection();
            return;
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            deactivateMentionState();
            return;
        }
    }

    // Enter 直接发送；Shift+Enter 保留 textarea 原生换行行为。
    if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        void sendMessage();
    }
}

function updateMentionStateFromInput(textarea) {
    if (!textarea) {
        deactivateMentionState();
        return;
    }
    const caret = textarea.selectionStart || 0;
    const textBefore = textarea.value.slice(0, caret);
    const atIndex = textBefore.lastIndexOf('@');

    if (atIndex === -1) {
        deactivateMentionState();
        return;
    }

    // 限制触发字符之前必须是空白或起始位置
    if (atIndex > 0) {
        const boundaryChar = textBefore[atIndex - 1];
        if (boundaryChar && !/\s/.test(boundaryChar) && !'([{，。,.;:!?'.includes(boundaryChar)) {
            deactivateMentionState();
            return;
        }
    }

    const querySegment = textBefore.slice(atIndex + 1);

    if (querySegment.includes(' ') || querySegment.includes('\n') || querySegment.includes('\t') || querySegment.includes('@')) {
        deactivateMentionState();
        return;
    }

    if (querySegment.length > 60) {
        deactivateMentionState();
        return;
    }

    mentionState.active = true;
    mentionState.startIndex = atIndex;
    mentionState.query = querySegment.toLowerCase();
    mentionState.selectedIndex = 0;

    if (!mentionToolsLoaded) {
        renderMentionSuggestions({ showLoading: true });
    } else {
        updateMentionCandidates();
        renderMentionSuggestions();
    }

    ensureMentionToolsLoaded().then(() => {
        if (mentionState.active) {
            updateMentionCandidates();
            renderMentionSuggestions();
        }
    });
}

function updateMentionCandidates() {
    if (!mentionState.active) {
        mentionFilteredTools = [];
        return;
    }
    const normalizedQuery = (mentionState.query || '').trim().toLowerCase();
    let filtered = mentionTools;

    if (normalizedQuery) {
        // 检查是否精确匹配外部MCP名称
        const exactMatchedMcp = externalMcpNames.find(mcpName =>
            mcpName.toLowerCase() === normalizedQuery
        );

        if (exactMatchedMcp) {
            // 如果完全匹配MCP名称，只显示该MCP下的所有工具
            filtered = mentionTools.filter(tool => {
                return tool.externalMcp && tool.externalMcp.toLowerCase() === exactMatchedMcp.toLowerCase();
            });
        } else {
            // 检查是否部分匹配MCP名称
            const partialMatchedMcps = externalMcpNames.filter(mcpName =>
                mcpName.toLowerCase().includes(normalizedQuery)
            );

            // 正常匹配：按工具名称和描述过滤，同时也匹配MCP名称
            filtered = mentionTools.filter(tool => {
                const nameMatch = tool.name.toLowerCase().includes(normalizedQuery);
                const descMatch = tool.description && tool.description.toLowerCase().includes(normalizedQuery);
                const mcpMatch = tool.externalMcp && tool.externalMcp.toLowerCase().includes(normalizedQuery);

                // 如果部分匹配到MCP名称，也包含该MCP下的所有工具
                const mcpPartialMatch = partialMatchedMcps.some(mcpName =>
                    tool.externalMcp && tool.externalMcp.toLowerCase() === mcpName.toLowerCase()
                );

                return nameMatch || descMatch || mcpMatch || mcpPartialMatch;
            });
        }
    }

    filtered = filtered.slice().sort((a, b) => {
        // 如果指定了角色，优先显示在当前角色中启用的工具
        if (a.roleEnabled !== undefined || b.roleEnabled !== undefined) {
            const aRoleEnabled = a.roleEnabled !== undefined ? a.roleEnabled : a.enabled;
            const bRoleEnabled = b.roleEnabled !== undefined ? b.roleEnabled : b.enabled;
            if (aRoleEnabled !== bRoleEnabled) {
                return aRoleEnabled ? -1 : 1; // 启用的工具排在前面
            }
        }

        if (normalizedQuery) {
            // 精确匹配MCP名称的工具优先显示
            const aMcpExact = a.externalMcp && a.externalMcp.toLowerCase() === normalizedQuery;
            const bMcpExact = b.externalMcp && b.externalMcp.toLowerCase() === normalizedQuery;
            if (aMcpExact !== bMcpExact) {
                return aMcpExact ? -1 : 1;
            }

            const aStarts = a.name.toLowerCase().startsWith(normalizedQuery);
            const bStarts = b.name.toLowerCase().startsWith(normalizedQuery);
            if (aStarts !== bStarts) {
                return aStarts ? -1 : 1;
            }
        }
        // 如果指定了角色，使用 roleEnabled；否则使用 enabled
        const aEnabled = a.roleEnabled !== undefined ? a.roleEnabled : a.enabled;
        const bEnabled = b.roleEnabled !== undefined ? b.roleEnabled : b.enabled;
        if (aEnabled !== bEnabled) {
            return aEnabled ? -1 : 1;
        }
        return a.name.localeCompare(b.name, 'zh-CN');
    });

    mentionFilteredTools = filtered;
    if (mentionFilteredTools.length === 0) {
        mentionState.selectedIndex = 0;
    } else if (mentionState.selectedIndex >= mentionFilteredTools.length) {
        mentionState.selectedIndex = 0;
    }
}

function renderMentionSuggestions({ showLoading = false } = {}) {
    if (!mentionSuggestionsEl || !mentionState.active) {
        hideMentionSuggestions();
        return;
    }

    const currentQuery = mentionState.query || '';
    const existingList = mentionSuggestionsEl.querySelector('.mention-suggestions-list');
    const canPreserveScroll = !showLoading &&
        existingList &&
        mentionSuggestionsEl.dataset.lastMentionQuery === currentQuery;
    const previousScrollTop = canPreserveScroll ? existingList.scrollTop : 0;

    if (showLoading) {
        mentionSuggestionsEl.innerHTML = '<div class="mention-empty">' + (typeof window.t === 'function' ? window.t('chat.loadingTools') : '正在加载工具...') + '</div>';
        mentionSuggestionsEl.style.display = 'block';
        delete mentionSuggestionsEl.dataset.lastMentionQuery;
        return;
    }

    if (!mentionFilteredTools.length) {
        mentionSuggestionsEl.innerHTML = '<div class="mention-empty">' + (typeof window.t === 'function' ? window.t('chat.noMatchTools') : '没有匹配的工具') + '</div>';
        mentionSuggestionsEl.style.display = 'block';
        mentionSuggestionsEl.dataset.lastMentionQuery = currentQuery;
        return;
    }

    const itemsHtml = mentionFilteredTools.map((tool, index) => {
        const activeClass = index === mentionState.selectedIndex ? 'active' : '';
        // 如果工具有 roleEnabled 字段（指定了角色），使用它；否则使用 enabled
        const toolEnabled = tool.roleEnabled !== undefined ? tool.roleEnabled : tool.enabled;
        const disabledClass = toolEnabled ? '' : 'disabled';
        const badge = tool.isExternal ? '<span class="mention-item-badge">外部</span>' : '<span class="mention-item-badge internal">内置</span>';
        const nameHtml = escapeHtml(tool.name);
        const description = tool.description && tool.description.length > 0 ? escapeHtml(tool.description) : (typeof window.t === 'function' ? window.t('chat.noDescription') : '暂无描述');
        const descHtml = `<div class="mention-item-desc">${description}</div>`;
        // 根据工具在当前角色中的启用状态显示状态标签
        const statusLabel = toolEnabled ? '可用' : (tool.roleEnabled !== undefined ? '已禁用（当前角色）' : '已禁用');
        const statusClass = toolEnabled ? 'enabled' : 'disabled';
        const originLabel = tool.isExternal
            ? (tool.externalMcp ? `来源：${escapeHtml(tool.externalMcp)}` : '来源：外部MCP')
            : '来源：内置工具';

        return `
            <button type="button" class="mention-item ${activeClass} ${disabledClass}" data-index="${index}">
                <div class="mention-item-name">
                    <span class="mention-item-icon">🔧</span>
                    <span class="mention-item-text">@${nameHtml}</span>
                    ${badge}
                </div>
                ${descHtml}
                <div class="mention-item-meta">
                    <span class="mention-status ${statusClass}">${statusLabel}</span>
                    <span class="mention-origin">${originLabel}</span>
                </div>
            </button>
        `;
    }).join('');

    const listWrapper = document.createElement('div');
    listWrapper.className = 'mention-suggestions-list';
    listWrapper.innerHTML = itemsHtml;

    mentionSuggestionsEl.innerHTML = '';
    mentionSuggestionsEl.appendChild(listWrapper);
    mentionSuggestionsEl.style.display = 'block';
    mentionSuggestionsEl.dataset.lastMentionQuery = currentQuery;

    if (canPreserveScroll) {
        listWrapper.scrollTop = previousScrollTop;
    }

    listWrapper.querySelectorAll('.mention-item').forEach(item => {
        item.addEventListener('mousedown', (event) => {
            event.preventDefault();
            const idx = parseInt(item.dataset.index, 10);
            if (!Number.isNaN(idx)) {
                mentionState.selectedIndex = idx;
            }
            applyMentionSelection();
        });
    });

    scrollMentionSelectionIntoView();
}

function hideMentionSuggestions() {
    if (mentionSuggestionsEl) {
        mentionSuggestionsEl.style.display = 'none';
        mentionSuggestionsEl.innerHTML = '';
        delete mentionSuggestionsEl.dataset.lastMentionQuery;
    }
}

function deactivateMentionState() {
    mentionState.active = false;
    mentionState.startIndex = -1;
    mentionState.query = '';
    mentionState.selectedIndex = 0;
    mentionFilteredTools = [];
    hideMentionSuggestions();
}

function moveMentionSelection(direction) {
    if (!mentionFilteredTools.length) {
        return;
    }
    const max = mentionFilteredTools.length - 1;
    let nextIndex = mentionState.selectedIndex + direction;
    if (nextIndex < 0) {
        nextIndex = max;
    } else if (nextIndex > max) {
        nextIndex = 0;
    }
    mentionState.selectedIndex = nextIndex;
    updateMentionActiveHighlight();
}

function updateMentionActiveHighlight() {
    if (!mentionSuggestionsEl) {
        return;
    }
    const items = mentionSuggestionsEl.querySelectorAll('.mention-item');
    if (!items.length) {
        return;
    }
    items.forEach(item => item.classList.remove('active'));

    let targetIndex = mentionState.selectedIndex;
    if (targetIndex < 0) {
        targetIndex = 0;
    }
    if (targetIndex >= items.length) {
        targetIndex = items.length - 1;
        mentionState.selectedIndex = targetIndex;
    }

    const activeItem = items[targetIndex];
    if (activeItem) {
        activeItem.classList.add('active');
        scrollMentionSelectionIntoView(activeItem);
    }
}

function scrollMentionSelectionIntoView(targetItem = null) {
    if (!mentionSuggestionsEl) {
        return;
    }
    const activeItem = targetItem || mentionSuggestionsEl.querySelector('.mention-item.active');
    if (activeItem && typeof activeItem.scrollIntoView === 'function') {
        activeItem.scrollIntoView({
            block: 'nearest',
            inline: 'nearest',
            behavior: 'auto'
        });
    }
}

function applyMentionSelection() {
    const textarea = document.getElementById('chat-input');
    if (!textarea || mentionState.startIndex === -1 || !mentionFilteredTools.length) {
        deactivateMentionState();
        return;
    }

    const selectedTool = mentionFilteredTools[mentionState.selectedIndex] || mentionFilteredTools[0];
    if (!selectedTool) {
        deactivateMentionState();
        return;
    }

    const caret = textarea.selectionStart || 0;
    const before = textarea.value.slice(0, mentionState.startIndex);
    const after = textarea.value.slice(caret);
    const mentionText = `@${selectedTool.name}`;
    const needsSpace = after.length === 0 || !/^\s/.test(after);
    const insertText = mentionText + (needsSpace ? ' ' : '');

    textarea.value = before + insertText + after;
    const newCaret = before.length + insertText.length;
    textarea.focus();
    textarea.setSelectionRange(newCaret, newCaret);

    // 调整输入框高度并保存草稿
    adjustTextareaHeight(textarea);
    saveChatDraftDebounced(textarea.value);

    deactivateMentionState();
}

function initializeChatUI() {
    const chatInputEl = document.getElementById('chat-input');
    if (chatInputEl) {
        // 初始化时设置正确的高度
        adjustTextareaHeight(chatInputEl);
        // 恢复保存的草稿（仅在输入框为空时恢复，避免覆盖用户输入）
        if (!chatInputEl.value || chatInputEl.value.trim() === '') {
            // 检查对话中是否有最近的消息（30秒内），如果有，说明可能是刚刚发送的消息，不恢复草稿
            const messagesDiv = document.getElementById('chat-messages');
            let shouldRestoreDraft = true;
            if (messagesDiv && messagesDiv.children.length > 0) {
                // 检查最后一条消息的时间
                const lastMessage = messagesDiv.lastElementChild;
                if (lastMessage) {
                    const timeDiv = lastMessage.querySelector('.message-time');
                    if (timeDiv && timeDiv.textContent) {
                        // 如果最后一条消息是用户消息，且时间很近，不恢复草稿
                        const isUserMessage = lastMessage.classList.contains('user');
                        if (isUserMessage) {
                            // 检查消息时间，如果是最近30秒内的，不恢复草稿
                            const now = new Date();
                            const messageTimeText = timeDiv.textContent;
                            // 简单检查：如果消息时间显示的是当前时间（格式：HH:MM），且是用户消息，不恢复草稿
                            // 更精确的方法是检查消息的创建时间，但需要从消息元素中获取
                            // 这里采用简单策略：如果最后一条是用户消息，且输入框为空，可能是刚发送的，不恢复草稿
                            shouldRestoreDraft = false;
                        }
                    }
                }
            }
            if (shouldRestoreDraft) {
                restoreChatDraft();
            } else {
                // 即使不恢复草稿，也要清除localStorage中的草稿，避免下次误恢复
                clearChatDraft();
            }
        }
    }

    const messagesDiv = document.getElementById('chat-messages');
    if (messagesDiv && messagesDiv.childElementCount === 0) {
        renderChatWelcomeEmptyState();
    }

    addAttackChainButton(currentConversationId);
    loadActiveTasks(true);
    if (activeTaskInterval) {
        clearInterval(activeTaskInterval);
    }
    activeTaskInterval = setInterval(() => loadActiveTasks(), ACTIVE_TASK_REFRESH_INTERVAL);
    setupMentionSupport();
    ensureChatInputContainerId();
    setupChatFileUpload();
}

// 消息计数器，确保ID唯一
let messageCounter = 0;

// 为消息气泡中的表格添加独立的滚动容器
function wrapTablesInBubble(bubble) {
    const tables = bubble.querySelectorAll('table');
    tables.forEach(table => {
        // 检查表格是否已经有包装容器
        if (table.parentElement && table.parentElement.classList.contains('table-wrapper')) {
            return;
        }

        // 创建表格包装容器
        const wrapper = document.createElement('div');
        wrapper.className = 'table-wrapper';

        // 将表格移动到包装容器中
        table.parentNode.insertBefore(wrapper, table);
        wrapper.appendChild(table);
    });
}

const PROJECT_NAME_DISPLAY_MAX_CHARACTERS = 12;

/** 仅限制项目名的界面展示，不修改实际保存的名称。 */
function formatProjectNameForDisplay(value) {
    const fullName = String(value == null ? '' : value);
    const characters = Array.from(fullName);
    if (characters.length <= PROJECT_NAME_DISPLAY_MAX_CHARACTERS) return fullName;
    return `${characters.slice(0, PROJECT_NAME_DISPLAY_MAX_CHARACTERS).join('')}…`;
}

function applyProjectNameDisplay(element, value, fallback = '') {
    if (!element) return '';
    const fullName = String(value || fallback || '');
    element.textContent = formatProjectNameForDisplay(fullName);
    element.dataset.fullName = fullName;
    element.title = fullName;
    return fullName;
}

window.formatProjectNameForDisplay = formatProjectNameForDisplay;
window.applyProjectNameDisplay = applyProjectNameDisplay;

function getChatWelcomeProjectName() {
    const projectElement = document.getElementById('chat-project-text');
    const projectText = (projectElement?.dataset?.fullName || projectElement?.textContent || '').trim();
    return projectText || (typeof window.t === 'function' ? window.t('projects.noProject') : '无项目');
}

function getChatWelcomeText() {
    const project = getChatWelcomeProjectName();
    const noProject = typeof window.t === 'function' ? window.t('projects.noProject') : '无项目';
    if (!project || project === noProject) {
        return typeof window.t === 'function'
            ? window.t('chat.noProjectWelcomeMessage')
            : '当前无项目，请输入您的测试需求，系统将自动执行相应的安全测试。';
    }
    return typeof window.t === 'function'
        ? window.t('chat.projectWelcomeMessage', { project })
        : `当前${project}项目，请输入您的测试需求，系统将自动执行相应的安全测试。`;
}

function updateChatWelcomeTitle(title) {
    if (!title) return;
    const project = getChatWelcomeProjectName();
    const noProject = typeof window.t === 'function' ? window.t('projects.noProject') : '无项目';
    const subtitle = title.parentElement?.querySelector('.chat-welcome-empty-state-subtitle');

    if (project === noProject) {
        title.textContent = typeof window.t === 'function'
            ? window.t('chat.noProjectWelcomeTitle')
            : '要测试什么？';
    } else {
        const prefix = typeof window.t === 'function'
            ? window.t('chat.projectWelcomeTitlePrefix')
            : '要在 ';
        const suffix = typeof window.t === 'function'
            ? window.t('chat.projectWelcomeTitleSuffix')
            : ' 项目中测试什么？';
        const projectName = document.createElement('span');
        projectName.className = 'chat-welcome-project-name';
        applyProjectNameDisplay(projectName, project);
        title.replaceChildren(document.createTextNode(prefix), projectName, document.createTextNode(suffix));
    }

    if (subtitle) {
        subtitle.textContent = typeof window.t === 'function'
            ? window.t('chat.welcomeSubtitle')
            : '请输入您的测试需求，系统将自动执行相应的安全测试。';
    }
}

function renderChatWelcomeEmptyState() {
    const messagesDiv = document.getElementById('chat-messages');
    if (!messagesDiv) return null;
    messagesDiv.querySelectorAll('.chat-welcome-empty-state').forEach((node) => node.remove());
    const state = document.createElement('div');
    state.className = 'chat-welcome-empty-state';
    state.setAttribute('role', 'status');
    state.setAttribute('aria-live', 'polite');
    state.innerHTML = '<p class="chat-welcome-empty-state-title"></p><p class="chat-welcome-empty-state-subtitle"></p>';
    updateChatWelcomeTitle(state.querySelector('.chat-welcome-empty-state-title'));
    messagesDiv.appendChild(state);
    return state;
}

/** 更新新对话欢迎空状态，并兼容刷新旧版本遗留的系统就绪消息。 */
function refreshSystemReadyMessageBubbles() {
    const text = getChatWelcomeText();
    const welcome = document.querySelector('.chat-welcome-empty-state-title');
    if (welcome) updateChatWelcomeTitle(welcome);
    const escapeHtmlLocal = (s) => {
        if (!s) return '';
        const div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    };
    let formattedContent;
    if (typeof window.csMarkdownSanitize !== 'undefined') {
        formattedContent = window.csMarkdownSanitize.formatMarkdownToHtml(text, { profile: 'chat' });
    } else {
        formattedContent = escapeHtmlLocal(text).replace(/\n/g, '<br>');
    }

    document.querySelectorAll('.message.assistant[data-system-ready-message]').forEach(function (messageDiv) {
        const bubble = messageDiv.querySelector('.message-bubble');
        if (!bubble) return;
        const copyBtn = bubble.querySelector('.message-copy-btn');
        if (copyBtn) copyBtn.remove();
        bubble.innerHTML = formattedContent;
        if (typeof wrapTablesInBubble === 'function') wrapTablesInBubble(bubble);
        messageDiv.dataset.originalContent = text;
        appendMessageCopyButton(messageDiv);
    });
}

function ensureMessageMetaFooter(content) {
    if (!content) return null;
    let footer = content.querySelector('.message-meta-footer');
    if (footer) return footer;
    const timeDiv = content.querySelector('.message-time');
    footer = document.createElement('div');
    footer.className = 'message-meta-footer';
    if (timeDiv && timeDiv.parentNode === content) {
        timeDiv.parentNode.insertBefore(footer, timeDiv);
        footer.appendChild(timeDiv);
    } else {
        content.appendChild(footer);
    }
    return footer;
}

function appendMessageCopyButton(messageDiv) {
    if (!messageDiv) return null;
    if (!messageDiv.classList || (!messageDiv.classList.contains('assistant') && !messageDiv.classList.contains('user'))) {
        return null;
    }
    const content = messageDiv.querySelector('.message-content');
    const footer = ensureMessageMetaFooter(content);
    if (!footer) return null;

    messageDiv.querySelectorAll('.message-bubble .message-copy-btn').forEach((btn) => btn.remove());
    let copyBtn = footer.querySelector('.message-copy-btn');
    if (copyBtn) return copyBtn;

    copyBtn = document.createElement('button');
    copyBtn.type = 'button';
    copyBtn.className = 'message-copy-btn';
    copyBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>' + (typeof window.t === 'function' ? window.t('common.copy') : '复制') + '</span>';
    copyBtn.title = typeof window.t === 'function' ? window.t('chat.copyMessageTitle') : '复制消息内容';
    copyBtn.setAttribute('aria-label', copyBtn.title);
    copyBtn.onclick = function(e) {
        e.stopPropagation();
        copyMessageToClipboard(messageDiv, this);
    };
    const deleteBtn = footer.querySelector('.message-delete-turn-btn');
    if (deleteBtn) {
        footer.insertBefore(copyBtn, deleteBtn);
    } else {
        footer.appendChild(copyBtn);
    }
    return copyBtn;
}
window.appendMessageCopyButton = appendMessageCopyButton;
window.ensureMessageMetaFooter = ensureMessageMetaFooter;

// 添加消息（options.systemReadyMessage 为 true 时，语言切换会刷新该条文案）
function addMessage(role, content, mcpExecutionIds = null, progressId = null, createdAt = null, options = null) {
    const messagesDiv = document.getElementById('chat-messages');
    const messageDiv = document.createElement('div');
    messageCounter++;
    const id = 'msg-' + Date.now() + '-' + messageCounter + '-' + Math.random().toString(36).substr(2, 9);
    messageDiv.id = id;
    messageDiv.className = 'message ' + role;

    messagesDiv.querySelector('.chat-welcome-empty-state')?.remove();

    // 创建消息内容容器
    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'message-content';

    // 创建消息气泡
    const bubble = document.createElement('div');
    bubble.className = 'message-bubble';

    // 解析 Markdown 或 HTML 格式
    let formattedContent;
    const escapeHtml = (text) => {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    };

    // 助手消息中的已知中文错误前缀做国际化替换（后端固定返回中文）
    let displayContent = content;
    if (role === 'assistant' && typeof displayContent === 'string' && typeof window.t === 'function') {
        if (displayContent.indexOf('执行失败: ') === 0) {
            displayContent = window.t('chat.executeFailed') + ': ' + displayContent.slice('执行失败: '.length);
        }
        if (displayContent.indexOf('调用OpenAI失败:') !== -1) {
            displayContent = displayContent.replace(/调用OpenAI失败:/g, window.t('chat.callOpenAIFailed') + ':');
        }
    }

    // 对于用户消息，直接转义HTML，不进行Markdown解析，以保留所有特殊字符
    if (role === 'user') {
        formattedContent = escapeHtml(content).replace(/\n/g, '<br>');
    } else if (typeof window.csMarkdownSanitize !== 'undefined') {
        formattedContent = window.csMarkdownSanitize.formatMarkdownToHtml(
            role === 'assistant' ? displayContent : content,
            { profile: 'chat' }
        );
    } else {
        const rawForEscape = role === 'assistant' ? displayContent : content;
        formattedContent = escapeHtml(rawForEscape).replace(/\n/g, '<br>');
    }

    bubble.innerHTML = formattedContent;

    // 刷新恢复运行中会话时，后端正文可能仍是持久化占位值“处理中...”。
    // 保留消息节点供迭代详情和最终回复复用，但不要把占位值显示成助手正文。
    if (role === 'assistant' && options && options.hideAssistantPlaceholder) {
        messageDiv.classList.add('assistant-placeholder-content');
        bubble.hidden = true;
    }

    if (typeof window.csMarkdownSanitize !== 'undefined') {
        window.csMarkdownSanitize.stripSuspiciousImages(bubble);
    }

    // 为每个表格添加独立的滚动容器
    wrapTablesInBubble(bubble);

    contentWrapper.appendChild(bubble);

    // 保存原始内容到消息元素，用于复制功能
    if (role === 'assistant' || role === 'user') {
        messageDiv.dataset.originalContent = content;
    }

    // 添加时间戳
    const timeDiv = document.createElement('div');
    timeDiv.className = 'message-time';
    // 如果有传入的创建时间，使用它；否则使用当前时间
    let messageTime;
    if (createdAt) {
        // 处理字符串或Date对象
        if (typeof createdAt === 'string') {
            messageTime = new Date(createdAt);
        } else if (createdAt instanceof Date) {
            messageTime = createdAt;
        } else {
            messageTime = new Date(createdAt);
        }
        // 如果解析失败，使用当前时间
        if (isNaN(messageTime.getTime())) {
            messageTime = new Date();
        }
    } else {
        messageTime = new Date();
    }
    const msgTimeLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const msgTimeOpts = { hour: '2-digit', minute: '2-digit' };
    if (msgTimeLocale === 'zh-CN') msgTimeOpts.hour12 = false;
    timeDiv.textContent = messageTime.toLocaleTimeString(msgTimeLocale, msgTimeOpts);
    try {
        timeDiv.dataset.messageTime = messageTime.toISOString();
    } catch (e) { /* ignore */ }
    const metaFooter = document.createElement('div');
    metaFooter.className = 'message-meta-footer';
    metaFooter.appendChild(timeDiv);
    contentWrapper.appendChild(metaFooter);
    messageDiv.appendChild(contentWrapper);

    // 为用户和助手消息添加复制按钮（复制整条消息内容）
    if (role === 'assistant' || role === 'user') {
        appendMessageCopyButton(messageDiv);
    }

    // 有 MCP 执行记录且非流式占位消息时展示调用按钮；带 progressId 的流式占位不挂此条（与进度卡片一致，结束时 integrate 再创建）
    if (role === 'assistant' && (mcpExecutionIds && Array.isArray(mcpExecutionIds) && mcpExecutionIds.length > 0) && !progressId) {
        if (options && options.deferMcpButtons) {
            try {
                const ids = cacheMcpExecutionIds(messageDiv, mcpExecutionIds);
                messageDiv.dataset.pendingMcpExecutionIds = JSON.stringify(ids);
            } catch (e) { /* ignore */ }
        } else {
            setMcpCallExecutionIds(messageDiv, mcpExecutionIds);
        }
    }

    // 标记「系统就绪」占位消息，便于切换语言后刷新文案
    if (options && options.systemReadyMessage) {
        messageDiv.setAttribute('data-system-ready-message', '1');
    }
    messagesDiv.appendChild(messageDiv);
    if (window.CyberStrikeChatScroll) {
        window.CyberStrikeChatScroll.applyMessageScroll(options);
    } else {
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
    }
    return id;
}

// 复制消息内容到剪贴板（使用原始Markdown格式）
function copyMessageToClipboard(messageDiv, button) {
    try {
        // 获取保存的原始Markdown内容
        const originalContent = messageDiv.dataset.originalContent;

        // 统一的复制处理函数
        const doCopy = (text) => {
            // 优先使用现代 Clipboard API（需要 HTTPS 或 localhost）
            if (navigator.clipboard && navigator.clipboard.writeText) {
                return navigator.clipboard.writeText(text).then(() => {
                    showCopySuccess(button);
                }).catch(err => {
                    console.error('Clipboard API 复制失败:', err);
                    fallbackCopy(text);
                });
            } else {
                // 降级方案：使用传统的 execCommand 方法（适用于 HTTP 环境）
                return fallbackCopy(text);
            }
        };

        // 降级复制函数（使用 document.execCommand）
        const fallbackCopy = (text) => {
            try {
                const textArea = document.createElement('textarea');
                textArea.value = text;
                textArea.style.position = 'fixed';
                textArea.style.left = '-999999px';
                textArea.style.top = '-999999px';
                textArea.style.opacity = '0';
                document.body.appendChild(textArea);
                textArea.focus();
                textArea.select();

                const successful = document.execCommand('copy');
                document.body.removeChild(textArea);

                if (successful) {
                    showCopySuccess(button);
                } else {
                    throw new Error('execCommand copy failed');
                }
            } catch (execErr) {
                console.error('降级复制失败:', execErr);
                alert(typeof window.t === 'function' ? window.t('chat.copyFailedManual') : '复制失败，请手动选择内容复制');
            }
        };

        if (!originalContent) {
            // 如果没有保存原始内容，尝试从渲染后的HTML提取（降级方案）
            const bubble = messageDiv.querySelector('.message-bubble');
            if (bubble) {
                const tempDiv = document.createElement('div');
                tempDiv.innerHTML = bubble.innerHTML;

                // 移除复制按钮本身（避免复制按钮文本）
                const copyBtnInTemp = tempDiv.querySelector('.message-copy-btn');
                if (copyBtnInTemp) {
                    copyBtnInTemp.remove();
                }

                // 提取纯文本内容
                let textContent = tempDiv.textContent || tempDiv.innerText || '';
                textContent = textContent.replace(/\n{3,}/g, '\n\n').trim();

                doCopy(textContent);
            }
            return;
        }

        // 使用原始Markdown内容
        doCopy(originalContent);
    } catch (error) {
        console.error('复制消息时出错:', error);
        alert(typeof window.t === 'function' ? window.t('chat.copyFailedManual') : '复制失败，请手动选择内容复制');
    }
}

// 显示复制成功提示
function showCopySuccess(button) {
    if (button) {
        const originalText = button.innerHTML;
        button.dataset.copySuccessActive = '1';
        button.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M20 6L9 17l-5-5" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>' + (typeof window.t === 'function' ? window.t('common.copied') : '已复制') + '</span>';
        button.style.color = '#10b981';
        button.style.background = 'rgba(16, 185, 129, 0.1)';
        button.style.borderColor = 'rgba(16, 185, 129, 0.3)';
        setTimeout(() => {
            delete button.dataset.copySuccessActive;
            button.innerHTML = originalText;
            button.style.color = '';
            button.style.background = '';
            button.style.borderColor = '';
        }, 2000);
    }
}

/** Claude extended thinking 内部尾缀（与后端 DisplayReasoningContent 一致，UI 不展示） */
const CLAUDE_REASONING_UI_SUFFIX = '\n---CSAI_CLAUDE_THINKING_BLOCKS---\n';

function normalizeReasoningContentForDisplay(text) {
    if (text == null) return '';
    let s = String(text).trim();
    if (!s) return '';
    const idx = s.lastIndexOf(CLAUDE_REASONING_UI_SUFFIX);
    if (idx >= 0) {
        s = s.slice(0, idx).trim();
    }
    return s;
}

function setMessageReasoningContent(messageIdOrEl, reasoningContent) {
    const el = typeof messageIdOrEl === 'string' ? document.getElementById(messageIdOrEl) : messageIdOrEl;
    if (!el || !el.dataset) return;
    const rc = normalizeReasoningContentForDisplay(reasoningContent);
    if (rc) {
        el.dataset.reasoningContent = rc;
    } else {
        delete el.dataset.reasoningContent;
    }
}

function getMessageReasoningContent(messageIdOrEl) {
    const el = typeof messageIdOrEl === 'string' ? document.getElementById(messageIdOrEl) : messageIdOrEl;
    if (!el || !el.dataset) return '';
    return normalizeReasoningContentForDisplay(el.dataset.reasoningContent || '');
}

function reasoningTextAlreadyInProcessDetails(processDetails, rc) {
    if (!rc) return true;
    const list = Array.isArray(processDetails) ? processDetails : [];
    for (let i = 0; i < list.length; i++) {
        const d = list[i];
        if (!d) continue;
        const et = d.eventType || '';
        if (et !== 'reasoning_chain' && et !== 'thinking') continue;
        const msg = normalizeReasoningContentForDisplay(d.message || '');
        if (!msg) continue;
        if (msg === rc || msg.includes(rc) || rc.includes(msg)) {
            return true;
        }
    }
    return false;
}

/** 合并 messages.reasoningContent 与 process_details 中的 reasoning_chain，两者都读、都展示（去重后） */
function mergeMessageReasoningContentIntoProcessDetails(processDetails, reasoningContent) {
    const rc = normalizeReasoningContentForDisplay(reasoningContent);
    const details = Array.isArray(processDetails) ? processDetails.slice() : [];
    if (!rc || reasoningTextAlreadyInProcessDetails(details, rc)) {
        return details;
    }
    details.push({
        eventType: 'reasoning_chain',
        message: rc,
        data: { source: 'message.reasoningContent' }
    });
    return details;
}

async function syncAssistantReasoningContentFromServer(backendMessageId, domAssistantId) {
    if (!backendMessageId || !domAssistantId || !currentConversationId || typeof apiFetch !== 'function') {
        return;
    }
    try {
        const convRes = await apiFetch(`/api/conversations/${encodeURIComponent(currentConversationId)}?include_process_details=0`);
        const conv = await convRes.json().catch(() => ({}));
        if (!convRes.ok || !Array.isArray(conv.messages)) return;
        const msg = conv.messages.find((m) => m && String(m.id) === String(backendMessageId));
        if (!msg || !msg.reasoningContent) return;
        setMessageReasoningContent(domAssistantId, msg.reasoningContent);
        // 最终回复到达后同样必须完整恢复过程详情；无参数接口默认仅返回前 50 条，
        // 否则这里会把 task-events 恢复出的完整时间线再次覆盖成第一页。
        if (typeof window.loadProcessDetailsPaginated === 'function') {
            await window.loadProcessDetailsPaginated(domAssistantId, String(backendMessageId));
        } else {
            const pdRes = await apiFetch(
                `/api/messages/${encodeURIComponent(String(backendMessageId))}/process-details?full=1`
            );
            const pdJson = await pdRes.json().catch(() => ({}));
            const details = pdRes.ok && Array.isArray(pdJson.processDetails) ? pdJson.processDetails : [];
            if (typeof renderProcessDetails === 'function') {
                renderProcessDetails(domAssistantId, details);
            }
        }
    } catch (e) {
        console.warn('syncAssistantReasoningContentFromServer failed', e);
    }
}

window.normalizeReasoningContentForDisplay = normalizeReasoningContentForDisplay;
window.setMessageReasoningContent = setMessageReasoningContent;
window.getMessageReasoningContent = getMessageReasoningContent;
window.filterNoiseProcessDetails = filterNoiseProcessDetails;
window.mergeMessageReasoningContentIntoProcessDetails = mergeMessageReasoningContentIntoProcessDetails;
window.syncAssistantReasoningContentFromServer = syncAssistantReasoningContentFromServer;

/** 相邻且类型/正文/data 完全一致的过程详情只保留一条（与后端去重一致，避免时间线叠多条相同块） */
function isEinoAgentHeartbeatProgress(detail) {
    if (!detail || detail.eventType !== 'progress') return false;
    const msg = String(detail.message != null ? detail.message : '').trim();
    return /^\[Eino\]\s+\S/.test(msg);
}

function hasModelOutputRecoveryMarker(value) {
    if (!value) return false;
    let obj = value;
    if (typeof obj === 'string') {
        const text = obj.trim();
        if (!text || text.indexOf('_cyberstrike_model_output_recovery') === -1) return false;
        try {
            obj = JSON.parse(text);
        } catch (e) {
            return false;
        }
    }
    return !!(obj && typeof obj === 'object' && obj._cyberstrike_model_output_recovery);
}

function isModelOutputRecoveryToolCallDetail(detail) {
    if (!detail || detail.eventType !== 'tool_call') return false;
    const data = detail.data && typeof detail.data === 'object' ? detail.data : {};
    return hasModelOutputRecoveryMarker(data.argumentsObj) || hasModelOutputRecoveryMarker(data.arguments);
}

function isAnonymousTaskFragmentToolCallDetail(detail) {
    if (!detail || detail.eventType !== 'tool_call') return false;
    const data = detail.data && typeof detail.data === 'object' ? detail.data : {};
    const toolName = String(data.toolName || '').trim().toLowerCase();
    if (toolName && toolName !== 'task' && toolName !== 'unknown') return false;

    let raw = null;
    const argsObj = data.argumentsObj && typeof data.argumentsObj === 'object' ? data.argumentsObj : null;
    if (argsObj) {
        const keys = Object.keys(argsObj);
        if (keys.length === 1 && keys[0] === '_raw') {
            raw = String(argsObj._raw != null ? argsObj._raw : '').trim();
        }
    }
    if (raw == null && typeof data.arguments === 'string') {
        raw = data.arguments.trim();
    }
    if (raw == null) return false;
    if (!raw) return true;
    return !(raw.startsWith('{') && raw.endsWith('}'));
}

function isInternalEinoDiagnosticDetail(detail) {
    if (!detail) return false;
    if (detail.eventType === 'model_output_rejected') return true;
    if (detail.eventType !== 'progress') return false;
    const msg = String(detail.message != null ? detail.message : '').trim();
    if (msg === 'Eino TurnLoop 常驻多轮 runtime 已接管本轮会话。' ||
        msg === 'Eino TurnLoop 已在安全点切换到用户补充后的下一轮。' ||
        msg === '已将用户补充推入 Eino TurnLoop，正在等待安全点切换…') {
        return true;
    }
    const data = detail.data && typeof detail.data === 'object' ? detail.data : {};
    const kind = String(data.kind || '').trim();
    return kind === 'turn_loop_takeover' || kind === 'turn_loop_preempted';
}

function filterNoiseProcessDetails(details) {
    if (!Array.isArray(details)) return details;
    return details.filter(function (d) {
        if (isEinoAgentHeartbeatProgress(d)) return false;
        if (isModelOutputRecoveryToolCallDetail(d)) return false;
        if (isAnonymousTaskFragmentToolCallDetail(d)) return false;
        if (isInternalEinoDiagnosticDetail(d)) return false;
        return !(d && d.eventType === 'tool_calls_detected');
    });
}

function dedupeConsecutiveProcessDetailRows(details) {
    if (!Array.isArray(details) || details.length < 2) {
        return details;
    }
    const out = [details[0]];
    for (let i = 1; i < details.length; i++) {
        const cur = details[i];
        if (processDetailRowFingerprint(out[out.length - 1]) === processDetailRowFingerprint(cur)) {
            continue;
        }
        out.push(cur);
    }
    return out;
}

function processDetailRowFingerprint(d) {
    if (!d || typeof d !== 'object') {
        return '';
    }
    const et = String(d.eventType || '');
    const msg = String(d.message != null ? d.message : '').trim();
    let dataKey = '';
    try {
        if (d.data != null) {
            dataKey = JSON.stringify(d.data);
        }
    } catch (e) {
        dataKey = String(d.data);
    }
    return et + '\0' + msg + '\0' + dataKey;
}

function compactWorkflowProcessDetails(details) {
    if (!Array.isArray(details) || details.length === 0) return details || [];
    return details.filter((detail) => {
        const eventType = detail && detail.eventType ? String(detail.eventType) : '';
        // workflow_node_start 已经表达了节点进入；这些事件只用于实时状态，落到详情里会让 Agent 节点看起来重复启动。
        return eventType !== 'workflow_agent_start';
    });
}

function isProcessDetailsUserExpanded(messageId) {
    const container = document.getElementById('process-details-' + messageId);
    return !!(container && container.dataset && container.dataset.userExpanded === '1');
}

function messageHasConversationContent(messageElement) {
    if (!messageElement || !messageElement.classList || !messageElement.classList.contains('assistant')) return false;
    if (messageElement.hasAttribute('data-system-ready-message')) return false;
    if (messageElement.dataset && String(messageElement.dataset.backendMessageId || '').trim()) return true;
    const raw = messageElement.dataset ? String(messageElement.dataset.originalContent || '').trim() : '';
    if (raw) return true;
    const bubble = messageElement.querySelector('.message-bubble');
    if (!bubble) return false;
    const clone = bubble.cloneNode(true);
    clone.querySelectorAll('.message-copy-btn').forEach((btn) => btn.remove());
    return String(clone.textContent || '').trim().length > 0;
}

function syncProcessDetailButtonLabels(messageId, expanded) {
    const expandT = typeof window.t === 'function' ? window.t('chat.expandDetail') : '展开详情';
    const collapseT = typeof window.t === 'function' ? window.t('tasks.collapseDetail') : '收起详情';
    const label = expanded ? collapseT : expandT;
    document.querySelectorAll('#' + messageId + ' .process-detail-btn').forEach((btn) => {
        btn.innerHTML = '<span>' + label + '</span>';
    });
    if (typeof window.syncAssistantTurnSummary === 'function') {
        window.syncAssistantTurnSummary(document.getElementById(messageId));
    }
}

/** 懒加载占位提示可点击，与工具栏「展开详情」行为一致 */
function bindProcessDetailsLazyHint(hostEl, messageId) {
    if (!hostEl || !messageId) return;
    const emptyEl = hostEl.classList && hostEl.classList.contains('progress-timeline-empty')
        ? hostEl
        : hostEl.querySelector('.progress-timeline-empty');
    if (!emptyEl || emptyEl.dataset.lazyHintBound === '1') return;
    emptyEl.dataset.lazyHintBound = '1';
    emptyEl.classList.add('progress-timeline-lazy-clickable');
    emptyEl.setAttribute('role', 'button');
    emptyEl.setAttribute('tabindex', '0');
    const activate = () => {
        if (typeof toggleProcessDetails === 'function') {
            toggleProcessDetails(null, messageId);
        }
    };
    emptyEl.addEventListener('click', activate);
    emptyEl.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            activate();
        }
    });
}
window.bindProcessDetailsLazyHint = bindProcessDetailsLazyHint;

// 渲染过程详情
// options.append=true 时分页追加；options.markLoaded=false 时保留 lazy 标记（分页加载中）
function renderProcessDetails(messageId, processDetails, options) {
    const renderOpts = options || {};
    const appendMode = !!renderOpts.append;
    const prependMode = !!renderOpts.prepend;
    const markLoaded = renderOpts.markLoaded !== false;
    const toolStatusByProcessDetailId = new Map();
    if (Array.isArray(renderOpts.toolExecutions)) {
        renderOpts.toolExecutions.forEach((execution) => {
            if (!execution || !execution.processDetailId) return;
            toolStatusByProcessDetailId.set(String(execution.processDetailId), String(execution.status || '').toLowerCase());
        });
    }
    const messageElement = document.getElementById(messageId);
    if (!messageElement) {
        return;
    }
    const isLazyRequest = (processDetails === null);
    const reasoningFromMessage = getMessageReasoningContent(messageElement);
    const backendId = messageElement.dataset ? String(messageElement.dataset.backendMessageId || '').trim() : '';
    const hasConversationContent = !!renderOpts.force || messageHasConversationContent(messageElement);
    if (isLazyRequest && !reasoningFromMessage && !backendId && getMcpExecutionCount(messageElement) <= 0 && !hasConversationContent) {
        pruneEmptyMcpCallSection(messageElement);
        return;
    }

    // 查找或创建 MCP 区域（工具栏 + 工具列表 + 迭代时间线 分区）
    const chrome = ensureMcpCallSectionChrome(messageElement, messageId);
    if (!chrome) return;
    const { mcpSection, toolbar: buttonsContainer } = chrome;

    // 添加过程详情按钮（如果还没有）
    let processDetailBtn = buttonsContainer.querySelector('.process-detail-btn');
    if (!processDetailBtn) {
        processDetailBtn = document.createElement('button');
        processDetailBtn.className = 'mcp-detail-btn process-detail-btn';
        processDetailBtn.innerHTML = '<span>' + (typeof window.t === 'function' ? window.t('chat.expandDetail') : '展开详情') + '</span>';
        processDetailBtn.onclick = () => toggleProcessDetails(null, messageId);
        buttonsContainer.appendChild(processDetailBtn);
    }
    syncMcpToolsToggleButton(messageElement);

    // 创建过程详情容器（放在工具列表之后）
    const detailsId = 'process-details-' + messageId;
    let detailsContainer = document.getElementById(detailsId);
    const toolListEl = chrome.toolList;

    if (!detailsContainer) {
        detailsContainer = document.createElement('div');
        detailsContainer.id = detailsId;
        detailsContainer.className = 'process-details-container';
        if (toolListEl) {
            toolListEl.after(detailsContainer);
        } else if (buttonsContainer.nextSibling) {
            mcpSection.insertBefore(detailsContainer, buttonsContainer.nextSibling);
        } else {
            mcpSection.appendChild(detailsContainer);
        }
    }

    // 创建时间线（即使没有processDetails也要创建，以便展开详情按钮能正常工作）
    const timelineId = detailsId + '-timeline';
    let timeline = document.getElementById(timelineId);

    if (!timeline) {
        const contentDiv = document.createElement('div');
        contentDiv.className = 'process-details-content';

        timeline = document.createElement('div');
        timeline.id = timelineId;
        timeline.className = 'progress-timeline';

        contentDiv.appendChild(timeline);
        detailsContainer.appendChild(contentDiv);
    }
    if (typeof window.ensureProcessDetailsReturnLatestControl === 'function') {
        window.ensureProcessDetailsReturnLatestControl(timeline);
    }

    // processDetails === null 表示“尚未加载（懒加载）”；messages.reasoningContent 可先展示
    const isLazyNotLoaded = isLazyRequest;
    if (isLazyNotLoaded && !reasoningFromMessage) {
        detailsContainer.dataset.lazyNotLoaded = '1';
        detailsContainer.dataset.loaded = '0';
        const expandLabel = typeof window.t === 'function' ? window.t('chat.expandDetail') : '展开详情';
        let lazyHint = expandLabel + '（点击后加载迭代详情）';
        timeline.innerHTML = '<div class="progress-timeline-empty">' + lazyHint + '</div>';
        bindProcessDetailsLazyHint(timeline, messageId);
        timeline.classList.remove('expanded');
        if (typeof window.updateProcessDetailsReturnLatestControl === 'function') {
            window.updateProcessDetailsReturnLatestControl(timeline);
        }
        prefetchProcessDetailsSummaryHint(messageId, messageElement);
        return;
    }
    if (isLazyNotLoaded) {
        detailsContainer.dataset.lazyNotLoaded = '1';
        detailsContainer.dataset.loaded = '0';
        processDetails = [];
        if (!appendMode) {
            prefetchProcessDetailsSummaryHint(messageId, messageElement);
        }
    } else if (markLoaded) {
        detailsContainer.dataset.lazyNotLoaded = '0';
        detailsContainer.dataset.loaded = '1';
    }
    const turnUsageFromDetails = extractAssistantTurnTokenUsage(processDetails);
    if (turnUsageFromDetails) {
        setAssistantTurnTokenUsage(messageElement, turnUsageFromDetails);
    }
    processDetails = mergeMessageReasoningContentIntoProcessDetails(processDetails, reasoningFromMessage);
    processDetails = filterNoiseProcessDetails(processDetails);
    processDetails = dedupeConsecutiveProcessDetailRows(processDetails);
    const renderedMcpIds = collectMcpExecutionIdsFromProcessDetails(processDetails);
    if (renderedMcpIds.length > 0) {
        setPendingMcpExecutionIds(messageElement, renderedMcpIds);
        setMcpExecutionSummaryCount(messageElement, renderedMcpIds.length);
    }
    if (typeof window.coalesceProcessDetailsToolPairs === 'function') {
        processDetails = window.coalesceProcessDetailsToolPairs(processDetails);
    }
    processDetails = compactWorkflowProcessDetails(processDetails);
    // 如果没有processDetails或为空，显示空状态
    if (!processDetails || processDetails.length === 0) {
        if (!appendMode && !prependMode) {
            timeline.innerHTML = '<div class="progress-timeline-empty">' + (typeof window.t === 'function' ? window.t('chat.noProcessDetail') : '暂无过程详情（可能执行过快或未触发详细事件）') + '</div>';
            if (!isProcessDetailsUserExpanded(messageId)) {
                timeline.classList.remove('expanded');
            }
            if (typeof window.updateProcessDetailsReturnLatestControl === 'function') {
                window.updateProcessDetailsReturnLatestControl(timeline);
            }
        }
        return;
    }

    const prependAnchor = prependMode ? timeline.firstChild : null;
    const prependScrollBox = prependMode ? document.getElementById('chat-messages') : null;
    const prependScrollHeight = prependScrollBox ? prependScrollBox.scrollHeight : 0;
    const prependScrollTop = prependScrollBox ? prependScrollBox.scrollTop : 0;
    const prependedIds = [];

    if (!appendMode && !prependMode) {
        timeline.innerHTML = '';
    }


    function processDetailAgentPrefix(d) {
        if (!d || d.einoAgent == null) return '';
        const s = String(d.einoAgent).trim();
        return s ? ('[' + s + '] ') : '';
    }

    function formatProcessDetailEinoRunRetryKind(kind) {
        if (typeof window.formatEinoRunRetryKind === 'function') {
            return window.formatEinoRunRetryKind(kind);
        }
        const key = String(kind || '').trim();
        if (!key) return '';
        const labels = {
            rate_limit: '限流 / 请求过多',
            retryable_http: '可重试 HTTP 错误',
            upstream_server: '上游服务错误',
            http_error: 'HTTP 错误',
            upstream_busy: '上游繁忙',
            network: '网络连接异常',
            stream: '流式读取异常',
            transient: '临时异常'
        };
        if (typeof window.t === 'function') {
            const translated = window.t('chat.einoRunRetryKind_' + key);
            if (translated && translated !== 'chat.einoRunRetryKind_' + key) return translated;
        }
        return labels[key] || key;
    }

    function formatProcessDetailEinoRunRetryTitle(data) {
        if (typeof window.formatEinoRunRetryTitle === 'function') {
            return window.formatEinoRunRetryTitle(data);
        }
        const d = data && typeof data === 'object' ? data : {};
        const base = typeof window.t === 'function'
            ? window.t('chat.einoRunRetryTitle')
            : '🔁 临时错误重试';
        const attempt = Number(d.attempt || 0);
        const maxAttempts = Number(d.maxAttempts || 0);
        if (Number.isFinite(attempt) && attempt > 0 && Number.isFinite(maxAttempts) && maxAttempts > 0) {
            return base + '（' + attempt + '/' + maxAttempts + '）';
        }
        return base;
    }

    function formatProcessDetailEinoRunRetryMessage(message, data) {
        if (typeof window.formatEinoRunRetryMessage === 'function') {
            return window.formatEinoRunRetryMessage(message, data);
        }
        const d = data && typeof data === 'object' ? data : {};
        const base = String(message || '').trim();
        const errRaw = d.errorSummary != null && String(d.errorSummary).trim() !== ''
            ? String(d.errorSummary).trim()
            : (d.error != null ? String(d.error).trim() : '');
        const lines = [];
        if (base) lines.push(base);
        const attempt = Number(d.attempt || 0);
        const maxAttempts = Number(d.maxAttempts || 0);
        const backoffSec = Number(d.backoffSec || 0);
        const kind = formatProcessDetailEinoRunRetryKind(d.errorKind);
        if (Number.isFinite(attempt) && attempt > 0 && Number.isFinite(maxAttempts) && maxAttempts > 0) {
            const retryPlan = typeof window.t === 'function'
                ? window.t('chat.einoRunRetryPlan', { attempt: attempt, maxAttempts: maxAttempts, backoffSec: Number.isFinite(backoffSec) && backoffSec > 0 ? backoffSec : '-' })
                : ('重试进度：第 ' + attempt + '/' + maxAttempts + ' 次，等待 ' + (Number.isFinite(backoffSec) && backoffSec > 0 ? backoffSec : '-') + ' 秒');
            if (!base || base.indexOf(String(attempt) + '/' + String(maxAttempts)) === -1) {
                lines.push(retryPlan);
            }
        }
        if (kind) {
            const kindLabel = typeof window.t === 'function'
                ? window.t('chat.einoRunRetryReasonKind')
                : '原因类型';
            lines.push(kindLabel + '：' + kind);
        }
        if (errRaw && (!base || base.indexOf(errRaw) === -1)) {
            const detailLabel = typeof window.t === 'function'
                ? window.t('chat.einoRunRetryErrorDetail')
                : '错误详情';
            lines.push(detailLabel + '：' + errRaw);
        }
        return lines.join('\n');
    }

    function renderOneProcessDetail(detail) {
        const eventType = detail.eventType || '';
        const title = detail.message || '';
        const data = detail.data || {};
        const agPx = processDetailAgentPrefix(data);

        let itemTitle = title;
        if (eventType === 'workflow_start') {
            const name = data.workflowName || data.workflowId || '';
            itemTitle = '🧭 工作流开始' + (name ? (' · ' + name) : '');
        } else if (eventType === 'workflow_done') {
            const name = data.workflowName || data.workflowId || '';
            itemTitle = '✅ 工作流完成' + (name ? (' · ' + name) : '');
        } else if (eventType === 'workflow_node_start') {
            const label = data.label || title || data.nodeId || '';
            itemTitle = '▶ 节点开始' + (label ? (' · ' + label) : '');
        } else if (eventType === 'workflow_node_result') {
            const label = data.label || data.nodeId || '';
            const status = data.status || '';
            const nodeType = data.nodeType != null ? String(data.nodeType).toLowerCase() : '';
            if (nodeType === 'condition') {
                const matched = data.matched === true || data.matched === 'true' || (data.output && (data.output.matched === true || data.output.matched === 'true'));
                itemTitle = (matched ? '✅' : '🔀') + ' 条件判断' + (label ? (' · ' + label) : '') + ' → ' + (matched ? '是' : '否');
            } else {
                const icon = status === 'failed' ? '❌' : (status === 'skipped' ? '⏭️' : '✅');
                itemTitle = icon + ' 节点完成' + (label ? (' · ' + label) : '') + (status ? ('（' + status + '）') : '');
            }
        } else if (eventType === 'workflow_branch_taken' || eventType === 'workflow_branch_skipped') {
            const branch = data.branchLabel || '';
            const target = data.targetLabel || data.targetId || '';
            const taken = eventType === 'workflow_branch_taken';
            itemTitle = (taken ? '➡️' : '⏭️') + (taken ? ' 执行分支' : ' 跳过分支') + (branch ? (' · ' + branch) : '') + (target ? (' → ' + target) : '');
        } else if (eventType === 'workflow_tool_start') {
            const tool = data.tool || data.toolName || '';
            itemTitle = '🔧 工具节点' + (tool ? (' · ' + tool) : '');
        } else if (eventType === 'workflow_agent_output') {
            const label = data.label || data.nodeId || '';
            itemTitle = '🤖 Agent 输出' + (label ? (' · ' + label) : '');
        } else if (eventType === 'workflow_hitl_checkpoint') {
            itemTitle = '🧑‍⚖️ 人工确认检查点';
        } else if (eventType === 'workflow_hitl_waiting') {
            itemTitle = '🧑‍⚖️ 工作流等待审批';
        } else if (eventType === 'workflow_paused') {
            itemTitle = '⏸️ 工作流已暂停';
        } else if (eventType === 'iteration') {
            const n = data.iteration || 1;
            if (data.orchestration === 'plan_execute' && data.einoScope === 'main') {
                const phase = typeof window.translatePlanExecuteAgentName === 'function'
                    ? window.translatePlanExecuteAgentName(data.einoAgent) : (data.einoAgent || '');
                itemTitle = (typeof window.t === 'function'
                    ? window.t('chat.einoPlanExecuteRound', { n: n, phase: phase })
                    : ('Plan-Execute · 第 ' + n + ' 轮 · ' + phase));
            } else if (data.einoScope === 'main') {
                itemTitle = agPx + (typeof window.t === 'function'
                    ? window.t('chat.einoOrchestratorRound', { n: n })
                    : ('主代理 · 第 ' + n + ' 轮'));
            } else if (data.einoScope === 'sub') {
                const agent = data.einoAgent != null ? String(data.einoAgent).trim() : '';
                itemTitle = agPx + (typeof window.t === 'function'
                    ? window.t('chat.einoSubAgentStep', { n: n, agent: agent })
                    : ('子代理 · ' + agent + ' · 第 ' + n + ' 步'));
            } else {
                itemTitle = agPx + (typeof window.t === 'function' ? window.t('chat.iterationRound', { n: n }) : '第 ' + n + ' 轮迭代');
            }
        } else if (eventType === 'thinking') {
            itemTitle = agPx + '🤔 ' + (typeof window.t === 'function' ? window.t('chat.aiThinking') : 'AI思考');
        } else if (eventType === 'reasoning_chain') {
            itemTitle = agPx + '🔗 ' + (typeof window.t === 'function' ? window.t('chat.reasoningChain') : '推理过程');
        } else if (eventType === 'planning') {
            if (typeof window.einoMainStreamPlanningTitle === 'function') {
                itemTitle = window.einoMainStreamPlanningTitle(data);
            } else {
                itemTitle = agPx + '📝 ' + (typeof window.t === 'function' ? window.t('chat.planning') : '规划中');
            }
        } else if (eventType === 'tool_calls_detected') {
            itemTitle = agPx + '🔧 ' + (typeof window.t === 'function' ? window.t('chat.toolCallsDetected', { count: data.count || 0 }) : '检测到 ' + (data.count || 0) + ' 个工具调用');
        } else if (eventType === 'tool_call') {
            const toolName = data.toolName || (typeof window.t === 'function' ? window.t('chat.unknownTool') : '未知工具');
            const index = data.index || 0;
            const total = data.total || 0;
            const callTitle = typeof window.formatToolCallTimelineTitle === 'function'
                ? window.formatToolCallTimelineTitle(toolName, index, total)
                : (typeof window.t === 'function' ? window.t('chat.callTool', { name: escapeHtml(toolName), index: index, total: total }) : '调用工具: ' + escapeHtml(toolName) + ' (' + index + '/' + total + ')');
            itemTitle = agPx + '🔧 ' + callTitle;
        } else if (eventType === 'tool_result') {
            const toolName = data.toolName || (typeof window.t === 'function' ? window.t('chat.unknownTool') : '未知工具');
            const noResultText = typeof window.t === 'function' ? window.t('timeline.noResult') : '无结果';
            const result = data.result != null ? data.result : (data.error != null ? data.error : (data.resultPreview != null ? data.resultPreview : noResultText));
            const resultStr = typeof result === 'string' ? result : JSON.stringify(result);
            const displayState = typeof window.getToolResultDisplayState === 'function'
                ? window.getToolResultDisplayState(data, { rawText: resultStr })
                : { kind: (data.success !== false ? 'success' : 'error'), isError: data.success === false };
            const backgroundRunning = displayState.kind === 'background_running';
            const success = !displayState.isError && !backgroundRunning;
            const statusIcon = displayState.kind === 'blocked' ? '🛡' : (backgroundRunning ? '⏳' : (success ? '✅' : '❌'));
            const execText = displayState.kind === 'blocked'
                ? (typeof window.t === 'function' ? window.t('chat.toolExecBlocked', { name: escapeHtml(toolName) }) : '工具 ' + escapeHtml(toolName) + ' 已拦截')
                : backgroundRunning
                ? ((typeof window.getBackgroundRunningToolLabel === 'function' ? window.getBackgroundRunningToolLabel() : '后台执行中') + ': ' + escapeHtml(toolName))
                : (success ? (typeof window.t === 'function' ? window.t('chat.toolExecComplete', { name: escapeHtml(toolName) }) : '工具 ' + escapeHtml(toolName) + ' 执行完成') : (typeof window.t === 'function' ? window.t('chat.toolExecFailed', { name: escapeHtml(toolName) }) : '工具 ' + escapeHtml(toolName) + ' 执行失败'));
            let execLine = statusIcon + ' ' + execText;
            if (toolName === BuiltinTools.SEARCH_KNOWLEDGE_BASE && success) {
                execLine = '📚 ' + execLine + ' - ' + (typeof window.t === 'function' ? window.t('chat.knowledgeRetrievalTag') : '知识检索');
            }
            itemTitle = agPx + execLine;
        } else if (eventType === 'eino_agent_reply') {
            itemTitle = agPx + '💬 ' + (typeof window.t === 'function' ? window.t('chat.einoAgentReplyTitle') : '子代理回复');
        } else if (eventType === 'eino_empty_response_continue') {
            itemTitle = typeof window.t === 'function'
                ? window.t('chat.einoEmptyResponseContinueTitle')
                : '🔁 自动续跑（无助手正文）';
        } else if (eventType === 'eino_run_retry') {
            itemTitle = formatProcessDetailEinoRunRetryTitle(data);
            detail.message = formatProcessDetailEinoRunRetryMessage(title, data);
        } else if (eventType === 'knowledge_retrieval') {
            itemTitle = '📚 ' + (typeof window.t === 'function' ? window.t('chat.knowledgeRetrieval') : '知识检索');
        } else if (eventType === 'error') {
            itemTitle = '❌ ' + (typeof window.t === 'function' ? window.t('chat.error') : '错误');
        } else if (eventType === 'cancelled') {
            itemTitle = '⛔ ' + (typeof window.t === 'function' ? window.t('chat.taskCancelled') : '任务已取消');
        } else if (eventType === 'hitl_interrupt') {
            const hitlMsg = (detail.message && String(detail.message).trim()) ? String(detail.message).trim() : (typeof window.t === 'function' ? window.t('hitl.pendingTitle') : '待审批');
            itemTitle = agPx + '🧑‍⚖️ HITL · ' + hitlMsg;
        } else if (eventType === 'hitl_audit_agent_started') {
            itemTitle = agPx + '审计 Agent 正在审查';
        } else if (eventType === 'hitl_audit_agent') {
            itemTitle = agPx + '审计 Agent 已完成审查';
        } else if (eventType === 'hitl_resumed') {
            itemTitle = agPx + '审批已通过';
        } else if (eventType === 'hitl_rejected') {
            itemTitle = agPx + '审批已拒绝';
        } else if (eventType === 'progress') {
            itemTitle = typeof window.translateProgressMessage === 'function' ? window.translateProgressMessage(detail.message || '') : (detail.message || '');
        } else if (eventType === 'user_interrupt_continue') {
            itemTitle = typeof window.t === 'function'
                ? window.t('chat.userInterruptContinueTitle')
                : '⏸️ 用户中断并继续';
        }

        if (eventType === 'hitl_interrupt' || eventType === 'hitl_audit_agent_started' ||
            eventType === 'hitl_audit_agent' || eventType === 'hitl_resumed' || eventType === 'hitl_rejected') {
            const hitlTarget = typeof findToolCallItemForHitl === 'function'
                ? findToolCallItemForHitl(timeline, data)
                : null;
            if (hitlTarget && hitlTarget.id) {
                if (eventType === 'hitl_interrupt' || eventType === 'hitl_audit_agent_started') {
                    renderInlineHitlApproval(hitlTarget.id, Object.assign({}, data, {
                        reviewer: eventType === 'hitl_audit_agent_started' ? 'audit_agent' : (data.reviewer || 'human'),
                        status: eventType === 'hitl_audit_agent_started' ? 'audit_running' : (data.status || 'pending')
                    }));
                } else {
                    const decision = eventType === 'hitl_rejected' || data.decision === 'reject' ? 'reject' : 'approve';
                    resolveInlineHitlDecision(timeline, Object.assign({}, data, {
                        reviewer: data.reviewer || data.decidedBy || (eventType === 'hitl_audit_agent' ? 'audit_agent' : 'human'),
                        status: 'decided'
                    }), decision, detail.message || '');
                }
                return;
            }
        }

        const processDetailId = detail.id || data.processDetailId || '';
        const timelineOpts = {
            title: itemTitle,
            message: detail.message || '',
            data: data,
            processDetailId: processDetailId,
            createdAt: detail.createdAt
        };
        if (eventType === 'tool_call' && data._mergedResult) {
            timelineOpts.mergedResult = data._mergedResult;
            if (typeof window.getToolResultDisplayState === 'function') {
                const displayState = window.getToolResultDisplayState(data._mergedResult);
                if (displayState && displayState.kind === 'background_running') {
                    timelineOpts.toolStatus = 'background_running';
                }
            }
        }
        if (eventType === 'tool_call' && detail.id && toolStatusByProcessDetailId.has(String(detail.id))) {
            timelineOpts.toolStatus = toolStatusByProcessDetailId.get(String(detail.id));
        }
        const itemId = addTimelineItem(timeline, eventType, timelineOpts);
        if (itemId && (eventType === 'hitl_interrupt' || eventType === 'hitl_audit_agent_started')) {
            renderInlineHitlApproval(itemId, Object.assign({}, data, {
                reviewer: eventType === 'hitl_audit_agent_started' ? 'audit_agent' : (data.reviewer || 'human'),
                status: eventType === 'hitl_audit_agent_started' ? 'audit_running' : (data.status || 'pending')
            }));
        } else if (itemId && (eventType === 'hitl_audit_agent' || eventType === 'hitl_resumed' || eventType === 'hitl_rejected')) {
            renderInlineHitlApproval(itemId, Object.assign({}, data, {
                resolved: true,
                decision: eventType === 'hitl_rejected' || data.decision === 'reject' ? 'reject' : 'approve',
                reviewer: data.reviewer || data.decidedBy || (eventType === 'hitl_audit_agent' ? 'audit_agent' : 'human'),
                status: 'decided'
            }));
        }
        if (prependMode && itemId) {
            prependedIds.push(itemId);
        }
    }

    function finishPrependRender() {
        if (!prependMode || prependedIds.length === 0) return;
        const fragment = document.createDocumentFragment();
        prependedIds.forEach((id) => {
            const node = document.getElementById(id);
            if (node) fragment.appendChild(node);
        });
        timeline.insertBefore(fragment, prependAnchor || timeline.firstChild);
        if (prependScrollBox) {
            const delta = prependScrollBox.scrollHeight - prependScrollHeight;
            prependScrollBox.scrollTop = prependScrollTop + delta;
        }
    }

    const TIMELINE_RENDER_BATCH = 40;
    const renderTimelineBatch = (startIdx) => {
        const endIdx = Math.min(startIdx + TIMELINE_RENDER_BATCH, processDetails.length);
        for (let i = startIdx; i < endIdx; i++) {
            renderOneProcessDetail(processDetails[i]);
        }
        if (endIdx < processDetails.length) {
            requestAnimationFrame(() => renderTimelineBatch(endIdx));
        } else if (markLoaded) {
            finishPrependRender();
            finishProcessDetailsRender(messageElement, processDetails, isLazyNotLoaded, timeline);
        } else {
            finishPrependRender();
        }
    };
    if (processDetails.length > TIMELINE_RENDER_BATCH) {
        renderTimelineBatch(0);
    } else {
        processDetails.forEach(renderOneProcessDetail);
        finishPrependRender();
        if (markLoaded) {
            finishProcessDetailsRender(messageElement, processDetails, isLazyNotLoaded, timeline);
        }
    }
}

function finishProcessDetailsRender(messageElement, processDetails, isLazyNotLoaded, timeline) {
    if (isLazyNotLoaded && getMessageReasoningContent(messageElement)) {
        const lazyHint = document.createElement('div');
        lazyHint.className = 'progress-timeline-empty progress-timeline-lazy-hint';
        lazyHint.textContent = (typeof window.t === 'function' ? window.t('chat.expandDetail') : '展开详情') +
            '（点击后加载完整过程详情）';
        timeline.appendChild(lazyHint);
        bindProcessDetailsLazyHint(lazyHint, messageElement.id);
    }

    const hasPendingHitlInDetails = processDetails.some(d => d && d.eventType === 'hitl_interrupt');
    const hasPendingWorkflowHitl = processDetails.some(d => d && d.eventType === 'workflow_hitl_waiting');
    const hasErrorOrCancelled = processDetails.some(d =>
        d.eventType === 'error' || d.eventType === 'cancelled'
    );
    const userExpanded = isProcessDetailsUserExpanded(messageElement.id);
    if (userExpanded) {
        timeline.classList.add('expanded');
        syncProcessDetailButtonLabels(messageElement.id, true);
    } else if (hasErrorOrCancelled && !hasPendingHitlInDetails && !hasPendingWorkflowHitl) {
        timeline.classList.remove('expanded');
        syncProcessDetailButtonLabels(messageElement.id, false);
    }
    if (hasPendingWorkflowHitl && messageElement && messageElement.id) {
        const convId = typeof window.currentConversationId === 'string' ? window.currentConversationId : '';
        if (convId && typeof window.restoreWorkflowHitlInlineForConversation === 'function') {
            window.restoreWorkflowHitlInlineForConversation(convId);
        }
    }
    if (typeof window.ensureProcessDetailsReturnLatestControl === 'function') {
        window.ensureProcessDetailsReturnLatestControl(timeline);
    }
    if (typeof window.updateProcessDetailsReturnLatestControl === 'function') {
        window.updateProcessDetailsReturnLatestControl(timeline);
    }
}

/** 懒加载折叠态：后台拉摘要，提示迭代规模而不加载全量详情 */
function prefetchProcessDetailsSummaryHint(messageId, messageElement) {
    if (!messageElement || !messageElement.dataset || !messageElement.dataset.backendMessageId) return;
    const backendId = String(messageElement.dataset.backendMessageId).trim();
    if (!backendId || typeof apiFetch !== 'function') return;
    const detailsContainer = document.getElementById('process-details-' + messageId);
    if (!detailsContainer || detailsContainer.dataset.summaryFetched === '1') return;
    detailsContainer.dataset.summaryFetched = '1';
    apiFetch('/api/messages/' + encodeURIComponent(backendId) + '/process-details?summary=1')
        .then(async (res) => {
            const j = await res.json().catch(() => ({}));
            if (!res.ok || !j.summary) return;
            const s = j.summary;
            if (typeof window.setAssistantTurnTiming === 'function') {
                window.setAssistantTurnTiming(messageElement, {
                    startedAt: s.startedAt,
                    completedAt: s.completedAt,
                    durationMs: s.durationMs,
                    status: s.status || 'completed'
                });
            }
            const summaryMcpIds = Array.isArray(s.mcpExecutionIds) ? s.mcpExecutionIds : [];
            const summaryTools = Array.isArray(s.toolExecutions) ? s.toolExecutions : [];
            if (summaryTools.length > 0) {
                setPendingToolExecutionSummaries(messageElement, summaryTools);
            }
            if (summaryMcpIds.length > 0) {
                setPendingMcpExecutionIds(messageElement, summaryMcpIds);
            }
            const summaryToolExecutionCount = summaryTools
                .map(normalizeToolExecutionSummaryForButton)
                .filter((item) => item.executionId)
                .length;
            const buttonToolCount = summaryToolExecutionCount > 0 ? summaryToolExecutionCount : summaryMcpIds.length;
            if (buttonToolCount > 0) {
                setMcpExecutionSummaryCount(messageElement, buttonToolCount);
            }
            const timeline = detailsContainer.querySelector('.progress-timeline');
            if (!timeline || detailsContainer.dataset.loaded === '1') return;
            const expandLabel = typeof window.t === 'function' ? window.t('chat.expandDetail') : '展开详情';
            let hint = expandLabel + '（点击后加载迭代详情）';
            if (s.maxIteration > 0) {
                hint = expandLabel + '（共 ' + s.maxIteration + ' 轮迭代，' + (s.total || 0) + ' 条详情）';
            } else if (s.total > 0) {
                hint = expandLabel + '（共 ' + (s.total || 0) + ' 条详情）';
            }
            const empty = timeline.querySelector('.progress-timeline-empty');
            if (empty) {
                empty.textContent = hint;
                bindProcessDetailsLazyHint(timeline, messageId);
            }
        })
        .catch(() => {});
}

// 移除消息
function removeMessage(id) {
    const messageDiv = document.getElementById(id);
    if (messageDiv) {
        messageDiv.remove();
    }
}

// 输入框事件绑定（Enter 发送、Shift+Enter 换行 / @提及）
const chatInput = document.getElementById('chat-input');
if (chatInput) {
    chatInput.addEventListener('keydown', handleChatInputKeydown);
    chatInput.addEventListener('input', handleChatInputInput);
    chatInput.addEventListener('click', handleChatInputClick);
    chatInput.addEventListener('focus', handleChatInputClick);
    // IME输入法事件监听，用于跟踪输入法状态
    chatInput.addEventListener('compositionstart', () => {
        if (compositionEndTimer) {
            clearTimeout(compositionEndTimer);
            compositionEndTimer = null;
        }
        isComposing = true;
    });
    chatInput.addEventListener('compositionend', () => {
        if (compositionEndTimer) {
            clearTimeout(compositionEndTimer);
        }
        compositionEndTimer = setTimeout(() => {
            isComposing = false;
            compositionEndTimer = null;
        }, 0);
    });
    chatInput.addEventListener('blur', () => {
        setTimeout(() => {
            if (!chatInput.matches(':focus')) {
                deactivateMentionState();
            }
        }, 120);
        // 失焦时立即保存草稿（不等待防抖）
        if (chatInput.value) {
            saveChatDraft(chatInput.value);
        }
    });
}

// 页面卸载时立即保存草稿
window.addEventListener('beforeunload', () => {
    const chatInput = document.getElementById('chat-input');
    if (chatInput && chatInput.value) {
        // 立即保存，不使用防抖
        saveChatDraft(chatInput.value);
    }
});

function getPendingMcpExecutionCount(messageElement) {
    if (!messageElement || !messageElement.dataset || !messageElement.dataset.pendingMcpExecutionIds) {
        return 0;
    }
    try {
        const ids = JSON.parse(messageElement.dataset.pendingMcpExecutionIds);
        return Array.isArray(ids) ? ids.length : 0;
    } catch (e) {
        return 0;
    }
}

function getPendingToolExecutionSummaryCount(messageElement) {
    if (!messageElement || !messageElement.dataset || !messageElement.dataset.pendingToolExecutionSummaries) {
        return 0;
    }
    try {
        const tools = JSON.parse(messageElement.dataset.pendingToolExecutionSummaries);
        return Array.isArray(tools)
            ? tools.map(normalizeToolExecutionSummaryForButton).filter((item) => item.executionId).length
            : 0;
    } catch (e) {
        return 0;
    }
}

function getMcpExecutionCount(messageElement) {
    const pendingSummaries = getPendingToolExecutionSummaryCount(messageElement);
    if (pendingSummaries > 0) return pendingSummaries;
    const toolList = messageElement && messageElement.querySelector('.mcp-tool-list');
    if (toolList) {
        const rendered = toolList.querySelectorAll('.mcp-detail-btn').length;
        if (rendered > 0) return rendered;
    }
    if (messageElement && messageElement.dataset && messageElement.dataset.mcpExecutionCount) {
        const summaryCount = parseInt(messageElement.dataset.mcpExecutionCount, 10) || 0;
        if (summaryCount > 0) return summaryCount;
    }
    const pending = getPendingMcpExecutionCount(messageElement);
    if (pending > 0) return pending;
    return 0;
}

function getExistingMcpCallSectionChrome(messageElement) {
    if (!messageElement) return null;
    const mcpSection = messageElement.querySelector('.mcp-call-section');
    if (!mcpSection) return null;
    return {
        mcpSection: mcpSection,
        toolbar: mcpSection.querySelector('.mcp-call-toolbar'),
        toolList: mcpSection.querySelector('.mcp-tool-list')
    };
}

function pruneEmptyMcpCallSection(messageElement) {
    const chrome = getExistingMcpCallSectionChrome(messageElement);
    if (!chrome || !chrome.mcpSection) return;
    const hasDetails = !!chrome.mcpSection.querySelector('.process-details-container');
    const hasProcessDetailButton = !!(chrome.toolbar && chrome.toolbar.querySelector('.process-detail-btn'));
    const hasToolButtons = !!(chrome.toolList && chrome.toolList.querySelector('.mcp-detail-btn'));
    const hasPendingTools = getPendingMcpExecutionCount(messageElement) > 0 ||
        getPendingToolExecutionSummaryCount(messageElement) > 0 ||
        getMcpExecutionCount(messageElement) > 0;
    if (!hasDetails && !hasProcessDetailButton && !hasToolButtons && !hasPendingTools) {
        chrome.mcpSection.remove();
    }
}

function collectMcpExecutionIdsFromProcessDetails(processDetails) {
    if (!Array.isArray(processDetails)) return [];
    const seen = new Set();
    const ids = [];
    const add = (value) => {
        const id = value == null ? '' : String(value).trim();
        if (!id || seen.has(id)) return;
        seen.add(id);
        ids.push(id);
    };
    processDetails.forEach((detail) => {
        const data = detail && detail.data && typeof detail.data === 'object' ? detail.data : null;
        if (!data) return;
        add(data.executionId);
        const merged = data._mergedResult && typeof data._mergedResult === 'object' ? data._mergedResult : null;
        if (merged) add(merged.executionId);
    });
    return ids;
}

function normalizeMcpExecutionIds(executionIds) {
    if (!Array.isArray(executionIds)) return [];
    const seen = new Set();
    return executionIds.reduce((ids, value) => {
        const id = value == null ? '' : String(value).trim();
        if (id && !seen.has(id)) {
            seen.add(id);
            ids.push(id);
        }
        return ids;
    }, []);
}

function cacheMcpExecutionIds(messageElement, executionIds) {
    if (!messageElement || !messageElement.dataset) return [];
    const ids = normalizeMcpExecutionIds(executionIds);
    if (ids.length > 0) {
        messageElement.dataset.mcpExecutionIds = JSON.stringify(ids);
    } else {
        delete messageElement.dataset.mcpExecutionIds;
    }
    return ids;
}

function getCachedMcpExecutionIds(messageElement) {
    if (!messageElement || !messageElement.dataset || !messageElement.dataset.mcpExecutionIds) return [];
    try {
        return normalizeMcpExecutionIds(JSON.parse(messageElement.dataset.mcpExecutionIds));
    } catch (e) {
        delete messageElement.dataset.mcpExecutionIds;
        return [];
    }
}

function setPendingMcpExecutionIds(messageElement, executionIds) {
    if (!messageElement || !messageElement.dataset || !Array.isArray(executionIds)) return;
    const ids = cacheMcpExecutionIds(messageElement, executionIds);
    if (ids.length > 0) {
        messageElement.dataset.pendingMcpExecutionIds = JSON.stringify(ids);
    } else {
        delete messageElement.dataset.pendingMcpExecutionIds;
    }
    const renderedToolList = messageElement.querySelector('.mcp-tool-list');
    if (ids.length > 0 && renderedToolList && renderedToolList.querySelector('.mcp-detail-btn')) {
        renderMcpCallButtons(messageElement);
    }
    if (typeof syncMcpToolsToggleButton === 'function') {
        syncMcpToolsToggleButton(messageElement);
    }
}

function normalizeToolExecutionSummaryForButton(raw) {
    const data = raw && typeof raw === 'object' ? raw : {};
	return {
	    toolName: data.toolName || data.name || '',
	    status: data.status || '',
	    executionId: data.executionId || '',
	    toolCallId: data.toolCallId || '',
	    processDetailId: data.processDetailId || '',
	    resultDetailId: data.resultDetailId || ''
	};
}

function cacheToolExecutionSummaries(messageElement, summaries) {
    if (!messageElement || !messageElement.dataset || !Array.isArray(summaries)) return [];
    const normalized = summaries
        .map(normalizeToolExecutionSummaryForButton)
        .filter((item) => item.executionId);
    if (normalized.length > 0) {
        messageElement.dataset.toolExecutionSummaries = JSON.stringify(normalized);
    } else {
        delete messageElement.dataset.toolExecutionSummaries;
    }
    return normalized;
}

function getCachedToolExecutionSummaries(messageElement) {
    if (!messageElement || !messageElement.dataset || !messageElement.dataset.toolExecutionSummaries) return [];
    try {
        const parsed = JSON.parse(messageElement.dataset.toolExecutionSummaries);
        return Array.isArray(parsed) ? parsed.map(normalizeToolExecutionSummaryForButton) : [];
    } catch (e) {
        delete messageElement.dataset.toolExecutionSummaries;
        return [];
    }
}

function selectToolExecutionSummariesForButtons(summaries, executionIds) {
    const normalizedSummaries = Array.isArray(summaries)
        ? summaries.map(normalizeToolExecutionSummaryForButton)
        : [];
    const normalizedIds = normalizeMcpExecutionIds(executionIds);
    if (normalizedSummaries.length > 0) return normalizedSummaries;
    if (normalizedIds.length === 0) return normalizedSummaries;
    return normalizedIds.map((executionId) => normalizeToolExecutionSummaryForButton({ executionId }));
}

function setPendingToolExecutionSummaries(messageElement, summaries) {
    if (!messageElement || !messageElement.dataset || !Array.isArray(summaries)) return;
    const normalized = cacheToolExecutionSummaries(messageElement, summaries);
    if (normalized.length > 0) {
        messageElement.dataset.pendingToolExecutionSummaries = JSON.stringify(normalized);
    } else {
        delete messageElement.dataset.pendingToolExecutionSummaries;
    }
    const renderedToolList = messageElement.querySelector('.mcp-tool-list');
    if (normalized.length > 0 && renderedToolList && renderedToolList.querySelector('.mcp-detail-btn')) {
        setMcpCallSummaries(messageElement, normalized);
        delete messageElement.dataset.pendingToolExecutionSummaries;
    }
    if (typeof syncMcpToolsToggleButton === 'function') {
        syncMcpToolsToggleButton(messageElement);
    }
}

function setMcpExecutionSummaryCount(messageElement, count) {
    if (!messageElement || !messageElement.dataset) return;
    const n = parseInt(count, 10) || 0;
    if (n > 0) {
        messageElement.dataset.mcpExecutionCount = String(n);
    } else {
        delete messageElement.dataset.mcpExecutionCount;
    }
    if (typeof syncMcpToolsToggleButton === 'function') {
        syncMcpToolsToggleButton(messageElement);
    }
}

function formatMcpToolsToggleLabel(count, expanded) {
    if (expanded) {
        if (typeof window.t === 'function') {
            const s = window.t('chat.collapseToolExecutions');
            if (s && s !== 'chat.collapseToolExecutions') return s;
        }
        return '收起工具执行';
    }
    if (typeof window.t === 'function') {
        const s = window.t('chat.toolExecutionsCount', { n: count });
        if (s && s !== 'chat.toolExecutionsCount') return s;
    }
    return count + '次工具执行';
}

function formatAssistantTurnDuration(durationMs) {
    const totalSeconds = Math.max(0, Math.floor((Number(durationMs) || 0) / 1000));
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (hours > 0) {
        return typeof window.t === 'function'
            ? window.t('chat.turnDurationHours', { hours: hours, minutes: minutes })
            : hours + ' 小时 ' + minutes + ' 分钟';
    }
    if (minutes > 0) {
        return typeof window.t === 'function'
            ? window.t('chat.turnDurationMinutes', { minutes: minutes, seconds: seconds })
            : minutes + ' 分钟 ' + seconds + ' 秒';
    }
    return typeof window.t === 'function'
        ? window.t('chat.turnDurationSeconds', { seconds: seconds })
        : seconds + ' 秒';
}

function assistantTurnUsageNumber(value) {
    const n = Number(value);
    return Number.isFinite(n) && n > 0 ? Math.round(n) : 0;
}

function normalizeAssistantTurnTokenUsage(data) {
    const source = data && typeof data === 'object' ? data : {};
    const usage = {
        modelCalls: assistantTurnUsageNumber(source.modelCalls),
        promptTokens: assistantTurnUsageNumber(source.promptTokens),
        completionTokens: assistantTurnUsageNumber(source.completionTokens),
        totalTokens: assistantTurnUsageNumber(source.totalTokens),
        cachedTokens: assistantTurnUsageNumber(source.cachedTokens),
        reasoningTokens: assistantTurnUsageNumber(source.reasoningTokens),
        model: source.model != null ? String(source.model).trim() : ''
    };
    if (usage.totalTokens <= 0 && (usage.promptTokens > 0 || usage.completionTokens > 0)) {
        usage.totalTokens = usage.promptTokens + usage.completionTokens;
    }
    return usage.totalTokens > 0 ? usage : null;
}

function mergeAssistantTurnTokenUsage(target, usage) {
    if (!usage) return target || null;
    const out = target || {
        modelCalls: 0,
        promptTokens: 0,
        completionTokens: 0,
        totalTokens: 0,
        cachedTokens: 0,
        reasoningTokens: 0,
        model: ''
    };
    out.modelCalls += usage.modelCalls || 0;
    out.promptTokens += usage.promptTokens || 0;
    out.completionTokens += usage.completionTokens || 0;
    out.totalTokens += usage.totalTokens || 0;
    out.cachedTokens += usage.cachedTokens || 0;
    out.reasoningTokens += usage.reasoningTokens || 0;
    if (!out.model && usage.model) out.model = usage.model;
    return out.totalTokens > 0 ? out : null;
}

function extractAssistantTurnTokenUsage(processDetails) {
    if (!Array.isArray(processDetails)) return null;
    let total = null;
    processDetails.forEach((detail) => {
        if (!detail || String(detail.eventType || '').trim() !== 'eino_usage_summary') return;
        const usage = normalizeAssistantTurnTokenUsage(detail.data);
        total = mergeAssistantTurnTokenUsage(total, usage);
    });
    return total;
}

function setAssistantTurnTokenUsage(messageElementOrId, usage) {
    const messageElement = typeof messageElementOrId === 'string'
        ? document.getElementById(messageElementOrId)
        : messageElementOrId;
    if (!messageElement || !messageElement.dataset) return;
    const normalized = normalizeAssistantTurnTokenUsage(usage);
    if (!normalized) {
        delete messageElement.dataset.turnModelCalls;
        delete messageElement.dataset.turnPromptTokens;
        delete messageElement.dataset.turnCompletionTokens;
        delete messageElement.dataset.turnTotalTokens;
        delete messageElement.dataset.turnCachedTokens;
        delete messageElement.dataset.turnReasoningTokens;
        delete messageElement.dataset.turnModel;
        syncAssistantTurnSummary(messageElement);
        return;
    }
    messageElement.dataset.turnModelCalls = String(normalized.modelCalls || 0);
    messageElement.dataset.turnPromptTokens = String(normalized.promptTokens || 0);
    messageElement.dataset.turnCompletionTokens = String(normalized.completionTokens || 0);
    messageElement.dataset.turnTotalTokens = String(normalized.totalTokens || 0);
    messageElement.dataset.turnCachedTokens = String(normalized.cachedTokens || 0);
    messageElement.dataset.turnReasoningTokens = String(normalized.reasoningTokens || 0);
    if (normalized.model) {
        messageElement.dataset.turnModel = normalized.model;
    } else {
        delete messageElement.dataset.turnModel;
    }
    syncAssistantTurnSummary(messageElement);
}

function getAssistantTurnTokenUsage(messageElement) {
    if (!messageElement || !messageElement.dataset) return null;
    return normalizeAssistantTurnTokenUsage({
        modelCalls: messageElement.dataset.turnModelCalls,
        promptTokens: messageElement.dataset.turnPromptTokens,
        completionTokens: messageElement.dataset.turnCompletionTokens,
        totalTokens: messageElement.dataset.turnTotalTokens,
        cachedTokens: messageElement.dataset.turnCachedTokens,
        reasoningTokens: messageElement.dataset.turnReasoningTokens,
        model: messageElement.dataset.turnModel
    });
}

function formatAssistantTurnTokenCount(value) {
    const n = assistantTurnUsageNumber(value);
    if (n >= 1000000) return (n / 1000000).toFixed(n >= 10000000 ? 0 : 1).replace(/\.0$/, '') + 'M';
    if (n >= 1000) return (n / 1000).toFixed(n >= 100000 ? 0 : 1).replace(/\.0$/, '') + 'K';
    return String(n);
}

function formatAssistantTurnTokenUsageLabel(usage) {
    const tokens = formatAssistantTurnTokenCount(usage && usage.totalTokens);
    return typeof window.t === 'function'
        ? window.t('chat.turnTokenUsageLabel', { tokens: tokens })
        : tokens + ' tokens';
}

function formatAssistantTurnTokenUsageTitle(usage) {
    const safeUsage = usage || {};
    const values = {
        total: formatAssistantTurnTokenCount(safeUsage.totalTokens),
        prompt: formatAssistantTurnTokenCount(safeUsage.promptTokens),
        completion: formatAssistantTurnTokenCount(safeUsage.completionTokens),
        cached: formatAssistantTurnTokenCount(safeUsage.cachedTokens),
        reasoning: formatAssistantTurnTokenCount(safeUsage.reasoningTokens),
        calls: formatAssistantTurnTokenCount(safeUsage.modelCalls),
        model: safeUsage.model || ''
    };
    return typeof window.t === 'function'
        ? window.t('chat.turnTokenUsageTitle', values)
        : 'Token usage: ' + values.total + ' (input ' + values.prompt + ', output ' + values.completion + ')';
}

function assistantTurnTimestamp(value) {
    if (value == null || value === '') return NaN;
    const n = new Date(value).getTime();
    return Number.isFinite(n) ? n : NaN;
}

function assistantTurnTerminalState(processDetails) {
    if (!Array.isArray(processDetails)) return null;
    for (let i = processDetails.length - 1; i >= 0; i--) {
        const detail = processDetails[i] || {};
        const eventType = String(detail.eventType || '').trim().toLowerCase();
        if (eventType === 'cancelled') {
            return { status: 'cancelled', completedAt: detail.createdAt || null, detail: detail };
        }
        if (eventType === 'timeout') {
            return { status: 'timeout', completedAt: detail.createdAt || null, detail: detail };
        }
        if (eventType === 'error') {
            return { status: 'failed', completedAt: detail.createdAt || null, detail: detail };
        }
    }
    return null;
}

let assistantTurnElapsedTimer = null;

function syncRunningAssistantTurnSummaries() {
    const runningTurns = document.querySelectorAll('#chat-messages .message.assistant[data-turn-status="running"]');
    runningTurns.forEach((messageElement) => syncAssistantTurnSummary(messageElement));
    if (runningTurns.length === 0 && assistantTurnElapsedTimer) {
        clearInterval(assistantTurnElapsedTimer);
        assistantTurnElapsedTimer = null;
    }
}

function syncAssistantTurnElapsedClock() {
    const hasRunningTurn = !!document.querySelector('#chat-messages .message.assistant[data-turn-status="running"]');
    if (hasRunningTurn && !assistantTurnElapsedTimer) {
        assistantTurnElapsedTimer = setInterval(syncRunningAssistantTurnSummaries, 1000);
    } else if (!hasRunningTurn && assistantTurnElapsedTimer) {
        clearInterval(assistantTurnElapsedTimer);
        assistantTurnElapsedTimer = null;
    }
}

function setAssistantTurnTiming(messageElementOrId, timing) {
    const messageElement = typeof messageElementOrId === 'string'
        ? document.getElementById(messageElementOrId)
        : messageElementOrId;
    if (!messageElement || !messageElement.dataset) return;
    const value = timing || {};
    if (value.startedAt) messageElement.dataset.turnStartedAt = String(value.startedAt);
    if (value.completedAt) messageElement.dataset.turnCompletedAt = String(value.completedAt);
    if (value.status) messageElement.dataset.turnStatus = String(value.status);
    const status = String(messageElement.dataset.turnStatus || 'completed');
    if (status === 'running') {
        // 摘要接口对运行中任务返回 durationMs=0。刷新页面时不能把这个快照
        // 当作固定耗时保存，否则后续渲染会一直显示“已处理 0 秒”。
        delete messageElement.dataset.turnDurationMs;
        delete messageElement.dataset.turnCompletedAt;
    } else {
        const explicitDuration = Number(value.durationMs);
        if (Number.isFinite(explicitDuration) && explicitDuration >= 0) {
            messageElement.dataset.turnDurationMs = String(Math.round(explicitDuration));
        }
        const startedAt = assistantTurnTimestamp(messageElement.dataset.turnStartedAt);
        const completedAt = assistantTurnTimestamp(messageElement.dataset.turnCompletedAt);
        if ((!Number.isFinite(explicitDuration) || explicitDuration < 0) &&
            Number.isFinite(startedAt) && Number.isFinite(completedAt) && completedAt >= startedAt) {
            messageElement.dataset.turnDurationMs = String(completedAt - startedAt);
        }
    }
    syncAssistantTurnSummary(messageElement);
    syncAssistantTurnElapsedClock();
}

function syncAssistantTurnSummary(messageElementOrId) {
    const messageElement = typeof messageElementOrId === 'string'
        ? document.getElementById(messageElementOrId)
        : messageElementOrId;
    if (!messageElement) return;
    const label = messageElement.querySelector('.mcp-call-label.turn-process-summary');
    if (!label) return;
    const details = messageElement.querySelector('.process-details-container');
    const timeline = details && details.querySelector('.progress-timeline');
    const expanded = !!(timeline && timeline.classList.contains('expanded'));
    const status = String(messageElement.dataset.turnStatus || 'completed');
    let durationMs = Number(messageElement.dataset.turnDurationMs);
    if (!Number.isFinite(durationMs) || durationMs < 0) {
        const startedAt = assistantTurnTimestamp(messageElement.dataset.turnStartedAt);
        const completedAt = status === 'running'
            ? Date.now()
            : assistantTurnTimestamp(messageElement.dataset.turnCompletedAt);
        durationMs = Number.isFinite(startedAt) && Number.isFinite(completedAt)
            ? Math.max(0, completedAt - startedAt)
            : 0;
    }
    const duration = formatAssistantTurnDuration(durationMs);
    const tokenUsage = getAssistantTurnTokenUsage(messageElement);
    const tokenUsageHtml = tokenUsage
        ? '<span class="turn-process-token-chip" title="' + escapeHtml(formatAssistantTurnTokenUsageTitle(tokenUsage)) + '">' +
            escapeHtml(formatAssistantTurnTokenUsageLabel(tokenUsage)) +
          '</span>'
        : '';
    let text;
    if (status === 'running') {
        text = typeof window.t === 'function' ? window.t('chat.turnElapsedRunning', { duration: duration }) : '已处理 ' + duration;
    } else if (status === 'cancelled') {
        text = typeof window.t === 'function' ? window.t('chat.turnElapsedCancelled', { duration: duration }) : '已中断 · 耗时 ' + duration;
    } else if (status === 'timeout') {
        text = typeof window.t === 'function' ? window.t('chat.turnElapsedTimeout', { duration: duration }) : '已超时 · 耗时 ' + duration;
    } else if (status === 'failed') {
        text = typeof window.t === 'function' ? window.t('chat.turnElapsedFailed', { duration: duration }) : '执行失败 · 耗时 ' + duration;
    } else {
        text = typeof window.t === 'function' ? window.t('chat.turnElapsedComplete', { duration: duration }) : '耗时 ' + duration;
    }
    label.innerHTML = `
        <span class="turn-process-leading">
            <span class="turn-process-status-dot${status === 'running' ? ' is-running' : ''}" aria-hidden="true"></span>
            <span class="turn-process-summary-text">${escapeHtml(text)}</span>
            ${tokenUsageHtml}
        </span>
        <svg class="turn-process-chevron" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M7.5 5.5L12 10l-4.5 4.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
    `;
    label.classList.toggle('is-expanded', expanded);
    label.setAttribute('aria-expanded', expanded ? 'true' : 'false');
    label.setAttribute('aria-label', typeof window.t === 'function'
        ? window.t('chat.turnProcessAria', { state: text })
        : text + '，展开或收起执行过程');
}

window.setAssistantTurnTiming = setAssistantTurnTiming;
window.setAssistantTurnTokenUsage = setAssistantTurnTokenUsage;
window.syncAssistantTurnSummary = syncAssistantTurnSummary;
window.formatAssistantTurnDuration = formatAssistantTurnDuration;
window.extractAssistantTurnTokenUsage = extractAssistantTurnTokenUsage;

/** 渗透测试区：工具栏（展开详情 | N次工具执行）+ 独立工具列表 + 迭代时间线 */
function ensureMcpCallSectionChrome(messageElement, messageId) {
    const contentWrapper = messageElement && messageElement.querySelector('.message-content');
    if (!contentWrapper) return null;

    let mcpSection = messageElement.querySelector('.mcp-call-section');
    if (!mcpSection) {
        mcpSection = document.createElement('div');
        mcpSection.className = 'mcp-call-section';
        const mcpLabel = document.createElement('button');
        mcpLabel.type = 'button';
        mcpLabel.className = 'mcp-call-label turn-process-summary';
        mcpLabel.onclick = function (event) {
            event.stopPropagation();
            toggleProcessDetails(null, messageId || messageElement.id);
        };
        mcpSection.appendChild(mcpLabel);
        const resultBubble = contentWrapper.querySelector(':scope > .message-bubble');
        contentWrapper.insertBefore(mcpSection, resultBubble || contentWrapper.firstChild);
    } else if (mcpSection.parentNode === contentWrapper) {
        const resultBubble = contentWrapper.querySelector(':scope > .message-bubble');
        if (resultBubble && mcpSection.nextSibling !== resultBubble) {
            contentWrapper.insertBefore(mcpSection, resultBubble);
        }
    }

    messageElement.classList.add('assistant-turn-with-process');
    const resultBubble = contentWrapper.querySelector(':scope > .message-bubble');
    if (resultBubble) resultBubble.classList.add('assistant-final-result');

    let toolbar = mcpSection.querySelector('.mcp-call-toolbar');
    if (!toolbar) {
        toolbar = document.createElement('div');
        toolbar.className = 'mcp-call-toolbar';
        mcpSection.appendChild(toolbar);
    }

    let toolList = mcpSection.querySelector('.mcp-tool-list');
    if (!toolList) {
        toolList = document.createElement('div');
        toolList.className = 'mcp-tool-list';
        const detailsContainer = mcpSection.querySelector('.process-details-container');
        if (detailsContainer) {
            mcpSection.insertBefore(toolList, detailsContainer);
        } else {
            toolbar.after(toolList);
        }
    }

    const clientId = messageId || messageElement.id;
    if (clientId && !toolbar.querySelector('.process-detail-btn')) {
        const processDetailBtn = document.createElement('button');
        processDetailBtn.className = 'mcp-detail-btn process-detail-btn';
        processDetailBtn.innerHTML = '<span>' + (typeof window.t === 'function' ? window.t('chat.expandDetail') : '展开详情') + '</span>';
        processDetailBtn.onclick = () => toggleProcessDetails(null, clientId);
        toolbar.appendChild(processDetailBtn);
    }

    syncAssistantTurnSummary(messageElement);
    return { mcpSection, toolbar, toolList };
}

function syncMcpToolsToggleButton(messageElement) {
    if (!messageElement) return;
    const count = getMcpExecutionCount(messageElement);
    let chrome = getExistingMcpCallSectionChrome(messageElement);
    if (!chrome || (count > 0 && (!chrome.toolbar || !chrome.toolList))) {
        if (count <= 0) return;
        chrome = ensureMcpCallSectionChrome(messageElement, messageElement.id);
    }
    if (!chrome) return;
    const { toolbar, toolList } = chrome;
    if (!toolbar || !toolList) return;
    let toolsToggle = toolbar.querySelector('.mcp-tools-toggle-btn');
    if (count <= 0) {
        if (toolsToggle) toolsToggle.remove();
        pruneEmptyMcpCallSection(messageElement);
        return;
    }
    if (!toolsToggle) {
        toolsToggle = document.createElement('button');
        toolsToggle.type = 'button';
        toolsToggle.className = 'mcp-detail-btn mcp-tools-toggle-btn';
        toolsToggle.onclick = function (e) {
            e.stopPropagation();
            toggleMcpToolList(messageElement.id);
        };
        toolbar.appendChild(toolsToggle);
    }
    const expanded = toolList.classList.contains('expanded');
    toolsToggle.innerHTML = '<span>' + formatMcpToolsToggleLabel(count, expanded) + '</span>';
}

function toggleMcpToolList(assistantMessageId) {
    const messageEl = document.getElementById(assistantMessageId);
    if (!messageEl) return;
    const chrome = ensureMcpCallSectionChrome(messageEl, assistantMessageId);
    if (!chrome) return;
    const { toolList } = chrome;
    if (
        !getPendingMcpExecutionCount(messageEl) &&
        !getPendingToolExecutionSummaryCount(messageEl) &&
        !toolList.querySelector('.mcp-detail-btn')
    ) {
        syncMcpToolsToggleButton(messageEl);
        return;
    }
    const willExpand = !toolList.classList.contains('expanded');
    if (willExpand) {
        renderPendingMcpCallButtons(messageEl);
        toolList.classList.add('expanded');
    } else {
        toolList.classList.remove('expanded');
    }
    syncMcpToolsToggleButton(messageEl);
}

window.toggleMcpToolList = toggleMcpToolList;
window.syncMcpToolsToggleButton = syncMcpToolsToggleButton;
window.isProcessDetailsUserExpanded = isProcessDetailsUserExpanded;
window.syncProcessDetailButtonLabels = syncProcessDetailButtonLabels;
window.ensureMcpCallSectionChrome = ensureMcpCallSectionChrome;
window.setMcpExecutionSummaryCount = setMcpExecutionSummaryCount;
window.setPendingMcpExecutionIds = setPendingMcpExecutionIds;
window.setPendingToolExecutionSummaries = setPendingToolExecutionSummaries;

async function openTaskToolExecutionDetail(messageElement, item, index) {
    const detailItem = normalizeToolExecutionSummaryForButton(item);
    if (!detailItem.executionId) return;
    await showMCPDetail(detailItem.executionId);
}

/**
 * 声明式渲染工具调用列表。
 * 工具详情只以 executionId 作为稳定入口；缺少 executionId 的历史摘要不渲染为工具详情入口。
 * 每次更新整体替换列表，避免增量追加产生双重状态。
 */
function renderMcpCallButtons(messageElement) {
    if (!messageElement) return;
    const chrome = ensureMcpCallSectionChrome(messageElement, messageElement.id);
    if (!chrome) return;
    const toolList = chrome.toolList;
    const executionIds = getCachedMcpExecutionIds(messageElement);
    const summaries = getCachedToolExecutionSummaries(messageElement);
    const items = selectToolExecutionSummariesForButtons(summaries, executionIds)
        .filter((item) => item && item.executionId);

    const renderVersion = String((parseInt(toolList.dataset.renderVersion, 10) || 0) + 1);
    toolList.dataset.renderVersion = renderVersion;
    const fragment = document.createDocumentFragment();
    items.forEach((item, index) => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'mcp-detail-btn';
        btn.dataset.execIndex = String(index + 1);
        if (item.executionId) {
            btn.dataset.execId = item.executionId;
        }
        if (item.toolCallId) {
            btn.dataset.toolCallId = item.toolCallId;
        }
        btn.onclick = () => openTaskToolExecutionDetail(messageElement, item, index);
        if (item.toolName) {
            renderToolExecutionButtonContent(btn, item.toolName, String(index + 1), item.status);
        } else {
            btn.innerHTML = '<span>' + (typeof window.t === 'function'
                ? window.t('chat.callNumber', { n: index + 1 })
                : '调用 #' + (index + 1)) + '</span>';
        }
        fragment.appendChild(btn);
    });
    toolList.replaceChildren(fragment);

    if (summaries.length === 0 && executionIds.length > 0) {
        batchUpdateButtonToolNames(toolList, executionIds, renderVersion);
    }
    syncMcpToolsToggleButton(messageElement);
}

function setMcpCallExecutionIds(messageElement, executionIds) {
    if (!messageElement || !Array.isArray(executionIds)) return;
    cacheMcpExecutionIds(messageElement, executionIds);
    renderMcpCallButtons(messageElement);
}

function setMcpCallSummaries(messageElement, summaries) {
    if (!messageElement || !Array.isArray(summaries)) return;
    cacheToolExecutionSummaries(messageElement, summaries);
    renderMcpCallButtons(messageElement);
}

/** 懒加载：用户展开工具列表时提交待渲染的数据模型。 */
function renderPendingMcpCallButtons(messageElement) {
    if (!messageElement || !messageElement.dataset) {
        return;
    }
    let renderedSummaryExecutions = false;
    if (messageElement.dataset.pendingToolExecutionSummaries) {
        let summaries;
        try {
            summaries = JSON.parse(messageElement.dataset.pendingToolExecutionSummaries);
        } catch (e) {
            delete messageElement.dataset.pendingToolExecutionSummaries;
            summaries = [];
        }
        if (Array.isArray(summaries) && summaries.length > 0) {
            setMcpCallSummaries(messageElement, summaries);
            renderedSummaryExecutions = true;
        }
        delete messageElement.dataset.pendingToolExecutionSummaries;
    }
    if (renderedSummaryExecutions) {
        return;
    }
    if (messageElement.dataset.pendingMcpExecutionIds) {
        let executionIds;
        try {
            executionIds = JSON.parse(messageElement.dataset.pendingMcpExecutionIds);
        } catch (e) {
            delete messageElement.dataset.pendingMcpExecutionIds;
            executionIds = [];
        }
        if (Array.isArray(executionIds) && executionIds.length > 0) {
            setMcpCallExecutionIds(messageElement, executionIds);
        }
        delete messageElement.dataset.pendingMcpExecutionIds;
    }
}

window.setMcpCallExecutionIds = setMcpCallExecutionIds;

function normalizeToolExecutionSummary(raw) {
    if (typeof raw === 'string') {
        return { toolName: raw, status: '' };
    }
    if (raw && typeof raw === 'object') {
        return {
            toolName: raw.toolName || raw.name || '',
            status: typeof window.getToolExecutionDisplayStatus === 'function' ? window.getToolExecutionDisplayStatus(raw) : (raw.status || '')
        };
    }
    return { toolName: '', status: '' };
}

function getToolExecutionStatusLabel(status) {
    const normalized = String(status || '').toLowerCase();
    if (typeof window.t === 'function') {
        const keyMap = {
            completed: 'mcpMonitor.statusSuccess',
            failed: 'mcpMonitor.statusFailed',
            blocked: 'mcpMonitor.statusBlocked',
            running: 'mcpMonitor.statusRunning',
            cancelled: 'mcpMonitor.statusCancelled',
            pending: 'mcpMonitor.statusPending',
            result_missing: 'timeline.resultMissing'
        };
        const key = keyMap[normalized];
        if (key) {
            const translated = window.t(key);
            if (translated && translated !== key) return translated;
        }
    }
    const fallback = {
        completed: '成功',
        failed: '失败',
        blocked: '已拦截',
        running: '运行中',
        cancelled: '已取消',
        pending: '等待中',
        result_missing: '结果记录缺失'
    };
    return fallback[normalized] || '';
}

function renderToolExecutionButtonContent(btn, displayToolName, index, status) {
    const safeToolName = escapeHtml(displayToolName || (typeof window.t === 'function' ? window.t('chat.unknownTool') : '未知工具'));
    const safeIndex = escapeHtml(index || '');
    const statusText = getToolExecutionStatusLabel(status);
    const normalizedStatus = String(status || '').toLowerCase();
    const label = safeIndex ? `${safeToolName} #${safeIndex}` : safeToolName;
    btn.innerHTML = '<span class="mcp-tool-name">' + label + '</span>';
    if (!statusText) {
        btn.removeAttribute('data-status');
        btn.removeAttribute('title');
        return;
    }
    btn.dataset.status = normalizedStatus;
    btn.title = statusText;
}

// 批量获取工具摘要并更新按钮（消除 N 次单独 API 请求，合并为 1 次）
async function batchUpdateButtonToolNames(buttonsContainer, executionIds, renderVersion) {
    if (!executionIds || executionIds.length === 0) return;
    try {
        const response = await apiFetch('/api/monitor/executions/names', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids: executionIds }),
        });
        if (!response.ok) return;
        const nameMap = await response.json(); // { execId: toolName } 或 { execId: { toolName, status } }
        // 等待请求期间如果摘要触发了新一轮渲染，旧响应不得覆盖新状态。
        if (renderVersion && buttonsContainer.dataset.renderVersion !== renderVersion) return;
        // 更新对应按钮的文本
        const buttons = buttonsContainer.querySelectorAll('.mcp-detail-btn[data-exec-id]');
        buttons.forEach(btn => {
            const execId = btn.dataset.execId;
            const index = btn.dataset.execIndex;
            const summary = normalizeToolExecutionSummary(nameMap[execId]);
            const toolName = summary.toolName;
            if (toolName) {
                const displayToolName = toolName.includes('::') ? toolName.split('::')[1] : toolName;
                renderToolExecutionButtonContent(btn, displayToolName, index, summary.status);
            }
        });
    } catch (error) {
        console.error('批量获取工具名称失败:', error);
    }
}

function extractMCPResultText(result) {
    if (!result) return '';
    const content = result.content;
    if (typeof content === 'string') return content;
    if (Array.isArray(content)) {
        return content
            .map(item => (item && typeof item === 'object' && typeof item.text === 'string') ? item.text : '')
            .filter(Boolean)
            .join('\n\n');
    }
    if (content && typeof content === 'object' && typeof content.text === 'string') {
        return content.text;
    }
    return '';
}

function formatMCPDetailText(text) {
    if (text == null) return '';
    return String(text);
}

function formatMCPResultJsonForDisplay(result) {
    if (!result) return '{}';
    const payload = {
        content: result.content,
        isError: !!result.isError,
        ...(result.blocked === true ? { blocked: true } : {})
    };
    return JSON.stringify(payload, null, 2);
}

function switchMCPResultDetailTab(tabName) {
    const normalized = tabName === 'raw' ? 'raw' : 'success';
    const tabs = {
        success: document.getElementById('detail-result-tab-success'),
        raw: document.getElementById('detail-result-tab-raw')
    };
    const panels = {
        success: document.getElementById('detail-result-panel-success'),
        raw: document.getElementById('detail-result-panel-raw')
    };
    Object.keys(tabs).forEach(function (key) {
        const isActive = key === normalized;
        if (tabs[key]) {
            tabs[key].classList.toggle('active', isActive);
            tabs[key].setAttribute('aria-selected', isActive ? 'true' : 'false');
        }
        if (panels[key]) {
            panels[key].classList.toggle('active', isActive);
            panels[key].hidden = !isActive;
        }
    });
}

function setMCPResultDetailTabs(defaultTab, hasSuccessContent) {
    const successTab = document.getElementById('detail-result-tab-success');
    if (successTab) {
        successTab.disabled = !hasSuccessContent;
        successTab.classList.toggle('disabled', !hasSuccessContent);
    }
    switchMCPResultDetailTab(hasSuccessContent && defaultTab !== 'raw' ? 'success' : 'raw');
}

function copyActiveMCPResultDetail(triggerBtn = null) {
    const activePanel = document.querySelector('#mcp-detail-modal .detail-result-panel.active');
    const activeBlock = activePanel ? activePanel.querySelector('.code-block') : null;
    copyDetailBlock(activeBlock ? activeBlock.id : 'detail-response', triggerBtn);
}

function renderMCPDetailModal(exec) {
    exec = exec || {};
    document.getElementById('detail-tool-name').textContent = exec.toolName || (typeof window.t === 'function' ? window.t('mcpDetailModal.unknown') : 'Unknown');
    document.getElementById('detail-execution-id').textContent = exec.id || 'N/A';
    const statusEl = document.getElementById('detail-status');
    const normalizedStatus = typeof window.getToolExecutionDisplayStatus === 'function' ? window.getToolExecutionDisplayStatus(exec) : (exec.status || 'unknown').toLowerCase();
    const blocked = normalizedStatus === 'blocked';
    statusEl.textContent = getStatusText(normalizedStatus);
    const statusClass = normalizedStatus === 'background_running' ? 'running' : normalizedStatus;
    statusEl.className = `status-chip status-${statusClass}`;
    try {
        statusEl.dataset.detailStatus = normalizedStatus;
    } catch (e) { /* ignore */ }
    const detailTimeLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const detailTimeEl = document.getElementById('detail-time');
    if (detailTimeEl) {
        detailTimeEl.textContent = exec.startTime
            ? new Date(exec.startTime).toLocaleString(detailTimeLocale)
            : '—';
        try {
            detailTimeEl.dataset.detailTimeIso = exec.startTime ? new Date(exec.startTime).toISOString() : '';
        } catch (e) { /* ignore */ }
    }

    const requestData = {
        tool: exec.toolName,
        arguments: exec.arguments
    };
    document.getElementById('detail-request').textContent = JSON.stringify(requestData, null, 2);

    const responseElement = document.getElementById('detail-response');
    const successElement = document.getElementById('detail-success');
    const errorSection = document.getElementById('detail-error-section');
    const errorElement = document.getElementById('detail-error');

    responseElement.className = 'code-block';
    responseElement.textContent = '';
    if (successElement) {
        successElement.className = 'code-block';
        successElement.textContent = '';
    }
    if (errorSection && errorElement) {
        errorSection.style.display = 'none';
        errorElement.textContent = '';
    }
    setMCPResultDetailTabs('raw', false);
    const resultTabLabel = document.querySelector('#detail-result-tab-success [data-i18n]');
    if (resultTabLabel) {
        const key = blocked ? 'mcpDetailModal.blockReason' : 'mcpDetailModal.correctInfo';
        resultTabLabel.dataset.i18n = key;
        resultTabLabel.textContent = typeof window.t === 'function' ? window.t(key) : (blocked ? '拦截原因' : '正确信息');
    }

    if (exec.result) {
        const agentVisibleText = formatMCPDetailText(extractMCPResultText(exec.result));
        const emptyText = typeof window.t === 'function' ? window.t('mcpDetailModal.execSuccessNoContent') : '执行成功，未返回可展示的文本内容。';

        if (blocked) {
            responseElement.className = 'code-block blocked';
            responseElement.textContent = formatMCPResultJsonForDisplay(exec.result);
            if (successElement) {
                successElement.className = 'code-block blocked';
                successElement.textContent = agentVisibleText || exec.error || getStatusText('blocked');
            }
            setMCPResultDetailTabs('success', true);
        } else if (exec.result.isError) {
            responseElement.className = 'code-block error';
            responseElement.textContent = formatMCPResultJsonForDisplay(exec.result);
            if (successElement) {
                successElement.textContent = '';
            }
            setMCPResultDetailTabs('raw', false);
            if (exec.error && errorSection && errorElement) {
                errorSection.style.display = 'block';
                errorElement.textContent = exec.error;
            }
        } else {
            responseElement.className = 'code-block';
            responseElement.textContent = formatMCPResultJsonForDisplay(exec.result);
            if (successElement) {
                successElement.textContent = agentVisibleText || emptyText;
            }
            setMCPResultDetailTabs('success', true);
        }
    } else {
        if (normalizedStatus === 'running' || normalizedStatus === 'background_running') {
            responseElement.textContent = typeof window.t === 'function' ? window.t('mcpDetailModal.runningNoResponseYet') : '尚无返回，工具可能仍在执行。若长时间无响应，可在下方终止本次调用。';
        } else {
            responseElement.textContent = typeof window.t === 'function' ? window.t('chat.noResponseData') : '暂无响应数据';
        }
        setMCPResultDetailTabs('raw', false);
    }

    const abortSection = document.getElementById('detail-abort-section');
    const abortBtn = document.getElementById('detail-abort-btn');
    if (abortSection && abortBtn) {
        if ((normalizedStatus === 'running' || normalizedStatus === 'background_running') && exec.id) {
            abortSection.style.display = 'block';
            abortBtn.dataset.execId = exec.id || '';
            abortBtn.textContent = typeof window.t === 'function' ? window.t('mcpDetailModal.abortBtn') : '终止工具';
        } else {
            abortSection.style.display = 'none';
            delete abortBtn.dataset.execId;
        }
    }
}

async function showMCPDetail(executionId) {
    try {
        openAppModal('mcp-detail-modal', { focus: false });
        const response = await apiFetch(`/api/monitor/execution/${executionId}`);
        const exec = await response.json();

        if (!response.ok) {
            closeMCPDetail();
            alert((typeof window.t === 'function' ? window.t('mcpDetailModal.getDetailFailed') : '获取详情失败') + ': ' + (exec.error || (typeof window.t === 'function' ? window.t('mcpDetailModal.unknown') : '未知错误')));
            return;
        }

        deferModalContent(function () {
            renderMCPDetailModal(exec);
        });
    } catch (error) {
        closeMCPDetail();
        alert((typeof window.t === 'function' ? window.t('mcpDetailModal.getDetailFailed') : '获取详情失败') + ': ' + error.message);
    }
}

// 关闭MCP详情模态框
function closeMCPDetail() {
    closeAppModal('mcp-detail-modal');
}

/** 从详情模态框触发：取消当前进行中的 MCP 工具调用 */
async function abortMCPToolExecutionFromDetail() {
    const btn = document.getElementById('detail-abort-btn');
    const id = btn && btn.dataset.execId;
    if (!id) {
        return;
    }
    await cancelMCPToolExecution(id, { refreshDetail: true });
}

/**
 * 打开 MCP 工具终止弹窗（说明会经服务端加上「用户终止说明」标题块后与工具输出合并给模型）
 * @param {string} executionId
 * @param {{ refreshDetail?: boolean }} [options]
 */
function openMcpToolAbortModal(executionId, options = {}) {
    window.__mcpToolAbortContext = { executionId: executionId, options: options || {} };
    const ta = document.getElementById('mcp-tool-abort-note');
    if (ta) {
        ta.value = '';
    }
    openAppModal('mcp-tool-abort-modal');
}

function closeMcpToolAbortModal() {
    window.__mcpToolAbortContext = null;
    closeAppModal('mcp-tool-abort-modal');
}

async function submitMcpToolAbortModal() {
    const ctx = window.__mcpToolAbortContext;
    if (!ctx || !ctx.executionId) {
        closeMcpToolAbortModal();
        return;
    }
    const note = (document.getElementById('mcp-tool-abort-note') && document.getElementById('mcp-tool-abort-note').value || '').trim();
    const executionId = ctx.executionId;
    const options = ctx.options || {};
    closeMcpToolAbortModal();
    await cancelMCPToolExecutionSubmit(executionId, note, options);
}

/**
 * 提交终止请求（body: { note }）
 * @param {string} executionId
 * @param {string} userNote
 * @param {{ refreshDetail?: boolean }} [options]
 */
async function cancelMCPToolExecutionSubmit(executionId, userNote, options = {}) {
    if (!executionId) {
        return;
    }
    let conversationId = '';
    if (typeof monitorState !== 'undefined' && Array.isArray(monitorState.executions)) {
        const exec = monitorState.executions.find(e => e && e.id === executionId);
        if (exec) {
            conversationId = (exec.conversationId || '').trim();
        }
    }
    try {
        if (conversationId && typeof requestCancelWithContinue === 'function') {
            await requestCancelWithContinue(conversationId, userNote || '', { executionId });
        } else {
            const res = await apiFetch(`/api/monitor/execution/${encodeURIComponent(executionId)}/cancel`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ note: userNote || '' }),
            });
            const body = await res.json().catch(() => ({}));
            if (!res.ok) {
                throw new Error(body.error || body.message || res.statusText);
            }
        }
        const okMsg = typeof window.t === 'function' ? window.t('mcpDetailModal.abortSuccess') : '已发送终止请求';
        alert(okMsg);
        if (options.refreshDetail && typeof showMCPDetail === 'function') {
            await showMCPDetail(executionId);
        }
        if (typeof refreshMonitorPanel === 'function') {
            const page = (typeof monitorState !== 'undefined' && monitorState.pagination && monitorState.pagination.page) ? monitorState.pagination.page : 1;
            await refreshMonitorPanel(page);
        }
    } catch (e) {
        const failMsg = typeof window.t === 'function' ? window.t('mcpDetailModal.abortFailed') : '终止失败';
        alert(failMsg + ': ' + (e && e.message ? e.message : String(e)));
    }
}

/**
 * 取消单次 MCP 工具执行（监控页「终止」）。有 conversationId 时复用对话页「中断并继续」弹窗与 API。
 * @param {string} executionId
 * @param {{ refreshDetail?: boolean }} [options]
 */
async function cancelMCPToolExecution(executionId, options = {}) {
    if (!executionId) {
        return;
    }
    let conversationId = '';
    if (typeof monitorState !== 'undefined' && Array.isArray(monitorState.executions)) {
        const exec = monitorState.executions.find(e => e && e.id === executionId);
        if (exec) {
            conversationId = (exec.conversationId || '').trim();
        }
    }
    if (conversationId && typeof openUserInterruptModal === 'function') {
        openUserInterruptModal(null, conversationId);
        window.__monitorInterruptContext = { executionId: executionId, options: options || {} };
        return;
    }
    openMcpToolAbortModal(executionId, options);
}

// 复制详情面板中的内容
function copyDetailBlock(elementId, triggerBtn = null) {
    const target = document.getElementById(elementId);
    if (!target) {
        return;
    }
    const text = target.textContent || '';
    if (!text.trim()) {
        return;
    }

    const originalLabel = triggerBtn ? (triggerBtn.dataset.originalLabel || triggerBtn.textContent.trim()) : '';
    if (triggerBtn && !triggerBtn.dataset.originalLabel) {
        triggerBtn.dataset.originalLabel = originalLabel;
    }

    const showCopiedState = () => {
        if (!triggerBtn) {
            return;
        }
        triggerBtn.textContent = '已复制';
        triggerBtn.disabled = true;
        setTimeout(() => {
            triggerBtn.disabled = false;
            triggerBtn.textContent = triggerBtn.dataset.originalLabel || originalLabel || '复制';
        }, 1200);
    };

    const fallbackCopy = (value) => {
        return new Promise((resolve, reject) => {
            const textarea = document.createElement('textarea');
            textarea.value = value;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();
            try {
                const successful = document.execCommand('copy');
                document.body.removeChild(textarea);
                if (successful) {
                    resolve();
                } else {
                    reject(new Error('execCommand failed'));
                }
            } catch (err) {
                document.body.removeChild(textarea);
                reject(err);
            }
        });
    };

    const copyPromise = (navigator.clipboard && typeof navigator.clipboard.writeText === 'function')
        ? navigator.clipboard.writeText(text)
        : fallbackCopy(text);

    copyPromise
        .then(() => {
            showCopiedState();
        })
        .catch(() => {
            if (triggerBtn) {
                triggerBtn.disabled = false;
                triggerBtn.textContent = triggerBtn.dataset.originalLabel || originalLabel || '复制';
            }
            alert('复制失败，请手动选择文本复制。');
        });
}


// 开始新对话
async function startNewConversation(options = {}) {
    const hasExplicitProjectId = !!options
        && Object.prototype.hasOwnProperty.call(options, 'projectId');
    const inheritedProjectId = typeof resolveChatProjectSelection === 'function'
        ? resolveChatProjectSelection()
        : (window._loadedConversationProjectId || '');
    const requestedProjectId = hasExplicitProjectId
        ? String(options.projectId || '').trim()
        : String(inheritedProjectId || '').trim();
    markChatConversationNavigation('', true);
    if (typeof window.cancelScheduledChatConversationFromHash === 'function') {
        window.cancelScheduledChatConversationFromHash();
    }
    clearChatConversationHash();
    cancelPendingConversationLoad();
    detachLiveChatStreamForNavigation('', true);
    if (typeof window.cancelRunningTaskEventStream === 'function') {
        window.cancelRunningTaskEventStream('');
    }
    if (typeof window.clearChatHitlApprovalDock === 'function') {
        window.clearChatHitlApprovalDock();
    }
    currentConversationId = null;
    window._loadedConversationProjectId = '';
    try {
        window.currentConversationId = '';
    } catch (e) { /* ignore */ }
    window.dispatchEvent(new CustomEvent('conversation-changed', { detail: { conversationId: '' } }));
    updateChatPrimaryActionState();
    // 顶部“新任务”继承当前文件夹；文件夹内的“+”仍可显式指定（包括无项目）。
    if (typeof setActiveProjectId === 'function') setActiveProjectId(requestedProjectId);
    if (typeof refreshChatProjectSelector === 'function') {
        await refreshChatProjectSelector();
    }
    document.getElementById('chat-messages').innerHTML = '';
    updateChatPrimaryActionState();
    renderChatWelcomeEmptyState();
    addAttackChainButton(null);
    updateActiveConversation();
    // 刷新对话列表，确保显示最新的历史对话
    loadConversations();
    // 清除防抖定时器，防止恢复草稿时触发保存
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
        draftSaveTimer = null;
    }
    // 清除草稿，新对话不应该恢复之前的草稿
    clearChatDraft();
    // 清空输入框
    const chatInput = document.getElementById('chat-input');
    if (chatInput) {
        chatInput.value = '';
        adjustTextareaHeight(chatInput);
    }
    refreshHitlConfigByCurrentConversation();
}

function createConversationListItem(conversation) {
    const item = document.createElement('div');
    item.className = 'conversation-item';
    item.dataset.conversationId = conversation.id;
    if (conversation.id === currentConversationId) {
        item.classList.add('active');
    }

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'conversation-content';

    const title = document.createElement('div');
    title.className = 'conversation-title';
    const titleText = conversation.title || '未命名对话';
    title.textContent = safeTruncateText(titleText, 60);
    title.title = titleText; // 设置完整标题以便悬停查看
    contentWrapper.appendChild(title);

    if (!getConversationProjectFilter()) {
        const pid = conversation.projectId || conversation.project_id || '';
        const projectName = pid && window.projectNameById ? window.projectNameById[pid] : '';
        if (projectName) {
            const badge = document.createElement('div');
            badge.className = 'conversation-item-project-badge';
            badge.textContent = projectName;
            badge.title = projectName;
            contentWrapper.appendChild(badge);
        }
    }

    const time = document.createElement('div');
    time.className = 'conversation-time';
    time.textContent = conversation._timeText || formatConversationTimestamp(conversation._time || new Date());
    contentWrapper.appendChild(time);

    item.appendChild(contentWrapper);

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'conversation-delete-btn';
    deleteBtn.innerHTML = `
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6h14zM10 11v6M14 11v6"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
    `;
    deleteBtn.title = '删除对话';
    deleteBtn.onclick = (e) => {
        e.stopPropagation();
        deleteConversation(conversation.id);
    };
    item.appendChild(deleteBtn);

    item.onclick = (e) => {
        e.preventDefault();
        e.stopPropagation();
        const targetConversationId = String(item.dataset.conversationId || '').trim();
        if (targetConversationId) loadConversation(targetConversationId);
    };
    return item;
}

// 处理历史记录搜索
let conversationSearchTimer = null;
function handleConversationSearch(query) {
    commitConversationsPage(1, { bumpNavigateGen: true });
    conversationsSearchQuery = query || '';
    // 防抖处理，避免频繁请求
    if (conversationSearchTimer) {
        clearTimeout(conversationSearchTimer);
    }

    const searchInput = document.getElementById('conversation-search-input');
    const clearBtn = document.getElementById('conversation-search-clear');

    if (clearBtn) {
        if (query && query.trim()) {
            clearBtn.style.display = 'block';
        } else {
            clearBtn.style.display = 'none';
        }
    }

    conversationSearchTimer = setTimeout(() => {
        loadConversations(query);
    }, 300); // 300ms防抖延迟
}

// 清除搜索
function clearConversationSearch() {
    const searchInput = document.getElementById('conversation-search-input');
    const clearBtn = document.getElementById('conversation-search-clear');

    if (searchInput) {
        searchInput.value = '';
    }
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }

    commitConversationsPage(1, { bumpNavigateGen: true });
    conversationsSearchQuery = '';
    loadConversations('');
}

function conversationSidebarText(key, fallback) {
    if (typeof window.t === 'function') {
        const translated = window.t(key);
        if (translated && translated !== key) return translated;
    }
    return fallback;
}

/**
 * Go 进程会嵌入 index.html。开发时即使进程尚未重启，也通过新版静态 JS
 * 将旧侧栏升级为项目文件夹结构；新模板已包含结构时该函数保持幂等。
 */
function ensureProjectSidebarStructure() {
    const sidebar = document.getElementById('conversation-sidebar');
    const sidebarContent = sidebar && sidebar.querySelector('.sidebar-content');
    if (!sidebar || !sidebarContent) return;

    const newTaskLabel = sidebar.querySelector('.new-chat-btn span:last-child');
    if (newTaskLabel) {
        newTaskLabel.setAttribute('data-i18n', 'chat.newTask');
        newTaskLabel.textContent = conversationSidebarText('chat.newTask', '新任务');
    }

    const searchInput = document.getElementById('conversation-search-input');
    if (searchInput) {
        searchInput.setAttribute('data-i18n', 'projects.searchProjectsPlaceholder');
        searchInput.setAttribute('data-i18n-attr', 'placeholder');
        searchInput.setAttribute('oninput', 'handleProjectFolderSearch(this.value)');
        searchInput.setAttribute('onkeypress', "if(event.key === 'Enter') handleProjectFolderSearch(this.value)");
        searchInput.placeholder = conversationSidebarText('projects.searchProjectsPlaceholder', '搜索项目…');
    }
    const searchClear = document.getElementById('conversation-search-clear');
    if (searchClear) searchClear.setAttribute('onclick', 'clearProjectFolderSearch()');

    const projectFilter = sidebarContent.querySelector('.conversation-project-filter');
    if (projectFilter) projectFilter.hidden = true;

    let projectSection = sidebarContent.querySelector('.project-folders-section');
    const legacyTaskSection = sidebarContent.querySelector('.task-folders-section');
    if (!projectSection && legacyTaskSection) {
        projectSection = legacyTaskSection;
        projectSection.className = 'project-folders-section';
        projectSection.setAttribute('aria-labelledby', 'project-folders-title');
        const legacyHeader = projectSection.querySelector('.task-folders-header');
        if (legacyHeader) legacyHeader.className = 'section-header project-folders-header';
        const legacyTitle = projectSection.querySelector('#task-folders-title');
        if (legacyTitle) {
            legacyTitle.id = 'project-folders-title';
            legacyTitle.setAttribute('data-i18n', 'chat.projectFolders');
            legacyTitle.textContent = conversationSidebarText('chat.projectFolders', '项目');
        }
        const legacyList = projectSection.querySelector('#task-folders-list');
        if (legacyList) {
            legacyList.id = 'project-folders-list';
            legacyList.className = 'project-folders-list';
            legacyList.removeAttribute('role');
            legacyList.innerHTML = '';
        }
    }
    if (!projectSection) {
        projectSection = document.createElement('section');
        projectSection.className = 'project-folders-section';
        projectSection.setAttribute('aria-labelledby', 'project-folders-title');
        projectSection.innerHTML =
            '<div class="section-header project-folders-header">' +
                '<span id="project-folders-title" class="section-title" data-i18n="chat.projectFolders">项目</span>' +
                '<button type="button" class="add-group-btn project-folders-add-btn" data-require-permission="project:write" onclick="showNewProjectModalFromChatSidebar()" data-i18n="projects.newProject" data-i18n-attr="title,aria-label" data-i18n-skip-text="true" title="新建项目" aria-label="新建项目">' +
                    '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>' +
                '</button>' +
            '</div>' +
            '<div id="project-folders-list" class="project-folders-list"></div>';
        const searchBox = sidebarContent.querySelector('.conversation-search-box');
        if (searchBox) searchBox.insertAdjacentElement('afterend', projectSection);
        else sidebarContent.insertBefore(projectSection, sidebarContent.firstChild);
    }

    const projectHeader = projectSection.querySelector('.project-folders-header');
    if (projectHeader && !projectHeader.querySelector('.project-folders-add-btn')) {
        const addProjectButton = document.createElement('button');
        addProjectButton.type = 'button';
        addProjectButton.className = 'add-group-btn project-folders-add-btn';
        addProjectButton.dataset.requirePermission = 'project:write';
        addProjectButton.setAttribute('onclick', 'showNewProjectModalFromChatSidebar()');
        addProjectButton.setAttribute('data-i18n', 'projects.newProject');
        addProjectButton.setAttribute('data-i18n-attr', 'title,aria-label');
        addProjectButton.setAttribute('data-i18n-skip-text', 'true');
        addProjectButton.title = conversationSidebarText('projects.newProject', '新建项目');
        addProjectButton.setAttribute('aria-label', addProjectButton.title);
        addProjectButton.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
        projectHeader.appendChild(addProjectButton);
    }

    const recentSection = sidebarContent.querySelector('.recent-conversations-section');
    if (recentSection) {
        recentSection.id = 'recent-conversations-section';
        recentSection.classList.add('is-collapsed');
        let toggle = document.getElementById('recent-conversations-toggle');
        let body = document.getElementById('recent-conversations-body');
        if (!toggle) {
            const oldHeader = recentSection.querySelector(':scope > .section-header');
            const title = oldHeader && oldHeader.querySelector('.section-title');
            const actions = oldHeader && oldHeader.querySelector('.section-header-actions');
            const list = recentSection.querySelector('#conversations-list');

            toggle = document.createElement('button');
            toggle.type = 'button';
            toggle.id = 'recent-conversations-toggle';
            toggle.className = 'section-header recent-conversations-toggle';
            toggle.setAttribute('aria-expanded', 'false');
            toggle.setAttribute('aria-controls', 'recent-conversations-body');
            toggle.setAttribute('data-i18n', 'chat.toggleRecentConversations');
            toggle.setAttribute('data-i18n-attr', 'title,aria-label');
            toggle.setAttribute('data-i18n-skip-text', 'true');
            toggle.title = conversationSidebarText('chat.toggleRecentConversations', '展开/折叠最近对话');
            toggle.setAttribute('aria-label', toggle.title);
            toggle.addEventListener('click', toggleRecentConversations);
            if (title) toggle.appendChild(title);
            else toggle.innerHTML = '<span class="section-title" data-i18n="chat.recentConversations">最近对话</span>';

            const meta = document.createElement('span');
            meta.className = 'recent-conversations-toggle-meta';
            meta.innerHTML =
                '<span id="recent-conversations-count" class="recent-conversations-count">0</span>' +
                '<svg class="recent-conversations-chevron" width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M9 18l6-6-6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
            toggle.appendChild(meta);

            body = document.createElement('div');
            body.id = 'recent-conversations-body';
            body.className = 'recent-conversations-body';
            body.hidden = true;
            if (actions) {
                actions.classList.add('recent-conversations-actions');
                body.appendChild(actions);
            }
            if (list) body.appendChild(list);
            if (oldHeader) oldHeader.remove();
            recentSection.prepend(toggle);
            recentSection.appendChild(body);
        }
    }

    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(sidebar);
    }
}

function setRecentConversationsExpanded(expanded, options = {}) {
    const section = document.getElementById('recent-conversations-section');
    const toggle = document.getElementById('recent-conversations-toggle');
    const body = document.getElementById('recent-conversations-body');
    const pagination = document.getElementById('conversations-pagination');
    const open = !!expanded;
    if (section) section.classList.toggle('is-collapsed', !open);
    if (toggle) toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    if (body) body.hidden = !open;
    if (pagination) pagination.hidden = !open;
    if (options.persist !== false) {
        try {
            localStorage.setItem(RECENT_CONVERSATIONS_EXPANDED_KEY, open ? '1' : '0');
        } catch (e) { /* ignore */ }
    }
}

function restoreRecentConversationsState() {
    let expanded = false;
    try {
        expanded = localStorage.getItem(RECENT_CONVERSATIONS_EXPANDED_KEY) === '1';
    } catch (e) { /* ignore */ }
    setRecentConversationsExpanded(expanded, { persist: false });
}

function toggleRecentConversations() {
    const toggle = document.getElementById('recent-conversations-toggle');
    const expanded = toggle && toggle.getAttribute('aria-expanded') === 'true';
    setRecentConversationsExpanded(!expanded);
}

function updateRecentConversationsCount(total) {
    const count = document.getElementById('recent-conversations-count');
    if (count) count.textContent = String(Math.max(0, Number(total) || 0));
}

if (typeof window !== 'undefined') {
    window.toggleRecentConversations = toggleRecentConversations;
    window.setRecentConversationsExpanded = setRecentConversationsExpanded;
}

function formatConversationTimestamp(dateObj, todayStart, yesterdayStart) {
    if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) {
        return '';
    }
    // 如果没有传入 todayStart，使用当前日期作为参考
    const now = new Date();
    const referenceToday = todayStart || new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const referenceYesterday = yesterdayStart || new Date(referenceToday.getTime() - 24 * 60 * 60 * 1000);
    const messageDate = new Date(dateObj.getFullYear(), dateObj.getMonth(), dateObj.getDate());
    const fmtLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const yesterdayLabel = typeof window.t === 'function' ? window.t('chat.yesterday') : '昨天';

    const timeOnlyOpts = { hour: '2-digit', minute: '2-digit' };
    const dateTimeOpts = { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' };
    const fullDateOpts = { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' };
    if (fmtLocale === 'zh-CN') {
        timeOnlyOpts.hour12 = false;
        dateTimeOpts.hour12 = false;
        fullDateOpts.hour12 = false;
    }
    if (messageDate.getTime() === referenceToday.getTime()) {
        return dateObj.toLocaleTimeString(fmtLocale, timeOnlyOpts);
    }
    if (messageDate.getTime() === referenceYesterday.getTime()) {
        return yesterdayLabel + ' ' + dateObj.toLocaleTimeString(fmtLocale, timeOnlyOpts);
    }
    if (dateObj.getFullYear() === referenceToday.getFullYear()) {
        return dateObj.toLocaleString(fmtLocale, dateTimeOpts);
    }
    return dateObj.toLocaleString(fmtLocale, fullDateOpts);
}

function getConversationGroup(dateObj, todayStart, sevenDaysCutoff, yesterdayStart) {
    if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) {
        return 'earlier';
    }
    const today = new Date(todayStart.getFullYear(), todayStart.getMonth(), todayStart.getDate());
    const yesterday = new Date(yesterdayStart.getFullYear(), yesterdayStart.getMonth(), yesterdayStart.getDate());
    const messageDay = new Date(dateObj.getFullYear(), dateObj.getMonth(), dateObj.getDate());

    if (messageDay.getTime() === today.getTime() || messageDay > today) {
        return 'today';
    }
    if (messageDay.getTime() === yesterday.getTime()) {
        return 'yesterday';
    }
    const cutoff = new Date(sevenDaysCutoff.getFullYear(), sevenDaysCutoff.getMonth(), sevenDaysCutoff.getDate());
    if (messageDay >= cutoff && messageDay < yesterday) {
        return 'last7Days';
    }
    return 'earlier';
}

// 加载对话
/** 轻量加载会话后，仅对「处理中…」占位回复拉取过程详情（机器人等非 SSE 场景）；已完成会话不预取全量 */
async function prefetchLastAssistantProcessDetails() {
    const nodes = document.querySelectorAll('#chat-messages .message.assistant');
    if (!nodes.length) return;
    const last = nodes[nodes.length - 1];
    if (!last || !last.id) return;
    const bubble = last.querySelector('.message-bubble');
    const visibleText = bubble ? String(bubble.textContent || '').trim() : '';
    const isPlaceholder = visibleText === '处理中...' || visibleText === 'Processing...';
    if (!isPlaceholder) return;
    const container = document.getElementById('process-details-' + last.id);
    if (!container || container.dataset.lazyNotLoaded !== '1') return;
    const backendId = last.dataset && last.dataset.backendMessageId;
    if (!backendId || typeof apiFetch !== 'function') return;
    if (typeof window.loadProcessDetailsPaginated === 'function') {
        await window.loadProcessDetailsPaginated(last.id, backendId);
        return;
    }
    const res = await apiFetch('/api/messages/' + encodeURIComponent(String(backendId)) + '/process-details?full=1');
    const j = await res.json().catch(() => ({}));
    if (!res.ok || !Array.isArray(j.processDetails) || j.processDetails.length === 0) return;
    if (typeof renderProcessDetails === 'function') {
        renderProcessDetails(last.id, j.processDetails);
    }
}

async function hydrateConversationTokenUsage(conversationId, expectedSeq, signal) {
    const id = String(conversationId || '').trim();
    if (!id || typeof apiFetch !== 'function' || typeof window.setAssistantTurnTokenUsage !== 'function') return;
    if (signal && signal.aborted) return;
    const params = new URLSearchParams();
    params.set('since', '1970-01-01');
    params.set('limit', '500');
    const res = await apiFetch(
        '/api/conversations/' + encodeURIComponent(id) + '/token-usage?' + params.toString(),
        signal ? { signal: signal } : undefined
    );
    const payload = await res.json().catch(() => ({}));
    if (!res.ok || (signal && signal.aborted)) return;
    if (expectedSeq != null && expectedSeq !== loadConversationRequestSeq) return;
    if (currentConversationId !== id) return;
    const rows = Array.isArray(payload && payload.recent) ? payload.recent : [];
    if (rows.length === 0) return;
    const byMessage = new Map();
    rows.forEach((row) => {
        const messageId = row && row.messageId != null ? String(row.messageId).trim() : '';
        if (!messageId) return;
        const usage = normalizeAssistantTurnTokenUsage(row);
        if (!usage) return;
        byMessage.set(messageId, mergeAssistantTurnTokenUsage(byMessage.get(messageId) || null, usage));
    });
    if (byMessage.size === 0) return;
    document.querySelectorAll('#chat-messages .message.assistant[data-backend-message-id]').forEach((messageElement) => {
        const backendMessageId = messageElement && messageElement.dataset
            ? String(messageElement.dataset.backendMessageId || '').trim()
            : '';
        const usage = backendMessageId ? byMessage.get(backendMessageId) : null;
        if (usage) {
            window.setAssistantTurnTokenUsage(messageElement, usage);
        }
    });
}

async function loadConversation(conversationId) {
    conversationId = String(conversationId || '').trim();
    if (!conversationId) return;
    // Keep the visible conversation addressable across a full page refresh.
    // Sidebar/project entries call loadConversation directly (rather than the
    // router helper), so without this synchronization #chat loses the active
    // conversation and reload falls back to the welcome screen instead of
    // reconnecting the running task event stream.
    markChatConversationNavigation(conversationId);
    if (typeof window.cancelScheduledChatConversationFromHash === 'function') {
        window.cancelScheduledChatConversationFromHash();
    }
    syncChatConversationHash(conversationId);
    const seq = ++loadConversationRequestSeq;
    const previousConversationId = currentConversationId;
    cancelPendingConversationLoad();
    detachLiveChatStreamForNavigation(conversationId);
    // 用户单击即代表新的可见会话。必须在任何网络等待之前提交该选择，
    // 否则每 2 秒的活跃任务刷新仍会把旧会话识别为可见，并排队重载旧补流，
    // 反过来取消这次切换。
    currentConversationId = conversationId;
    try {
        window.currentConversationId = conversationId;
    } catch (e) { /* ignore */ }
    loadConversationPendingId = conversationId;
    const conversationLoadController = new AbortController();
    loadConversationAbortController = conversationLoadController;
    if (typeof window.selectChatProjectConversationItem === 'function') {
        window.selectChatProjectConversationItem(conversationId);
    }
    if (typeof window.cancelRunningTaskEventStream === 'function') {
        window.cancelRunningTaskEventStream(conversationId);
    }
    if (typeof window.clearChatHitlApprovalDock === 'function') {
        window.clearChatHitlApprovalDock();
    }
    try {
        const cachedConversation = getConversationLiteFromCache(conversationId);
        let conversation = null;
        let response = null;
        try {
            response = await apiFetch(`/api/conversations/${conversationId}?include_process_details=0`, {
                signal: conversationLoadController.signal
            });
            conversation = await response.json();
        } catch (fetchError) {
            if (fetchError && fetchError.name === 'AbortError') return;
            if (!cachedConversation) throw fetchError;
            console.warn('加载最新对话失败，使用本地缓存:', fetchError);
            conversation = cachedConversation;
        }
        if (seq !== loadConversationRequestSeq) {
            return;
        }
        if (response && !response.ok) {
            if (seq === loadConversationRequestSeq) {
                currentConversationId = previousConversationId;
                try {
                    window.currentConversationId = previousConversationId || '';
                } catch (e) { /* ignore */ }
                if (previousConversationId) syncChatConversationHash(previousConversationId);
                else clearChatConversationHash();
            }
            showChatToast('加载对话失败: ' + (conversation.error || '未知错误'), 'error');
            return;
        }
        if (response && response.ok) {
            putConversationLiteCache(conversationId, conversation);
        }
        if (seq !== loadConversationRequestSeq) {
            return;
        }

        // 更新当前对话ID
        currentConversationId = conversationId;
        window._loadedConversationProjectId = conversation.projectId || conversation.project_id || '';
        const conversationRoleName = conversation.roleName || conversation.role_name || '';
        if (typeof window.setCurrentRole === 'function') {
            window.setCurrentRole(conversationRoleName || '默认');
        }
        applyConversationAgentMode(conversationId, conversation);
        try {
            window.currentConversationId = conversationId;
        } catch (e) { /* ignore */ }
        window.dispatchEvent(new CustomEvent('conversation-changed', { detail: { conversationId: conversationId } }));
        updateChatPrimaryActionState();
        if (typeof refreshChatProjectSelector === 'function') {
            refreshChatProjectSelector({ reloadFolders: false, renderFolders: false });
        }
        refreshHitlConfigByCurrentConversation();
        const hitlSyncPromise = (typeof window.syncHitlConfigFromServer === 'function')
            ? window.syncHitlConfigFromServer(conversationId).then(() => {
                if (seq === loadConversationRequestSeq && currentConversationId === conversationId) {
                    refreshHitlConfigByCurrentConversation();
                }
            }).catch(() => {})
            : Promise.resolve();
        hitlConfigSyncConversationId = conversationId;
        hitlConfigSyncPromise = Promise.resolve(hitlSyncPromise);
        await hitlConfigSyncPromise;
        if (seq !== loadConversationRequestSeq || currentConversationId !== conversationId) {
            return;
        }
        updateActiveConversation();

        // 如果攻击链模态框打开且显示的不是当前对话，关闭它
        const attackChainModal = document.getElementById('attack-chain-modal');
        if (attackChainModal && isAppModalOpen('attack-chain-modal')) {
            if (currentAttackChainConversationId !== conversationId) {
                closeAttackChainModal();
            }
        }

        // 清空消息区域
        const messagesDiv = document.getElementById('chat-messages');
        if (seq !== loadConversationRequestSeq) {
            return;
        }
        messagesDiv.innerHTML = '';

        // 检查对话中是否有最近的消息，如果有，清除草稿（避免恢复已发送的消息）
        let hasRecentUserMessage = false;
        if (conversation.messages && conversation.messages.length > 0) {
            const lastMessage = conversation.messages[conversation.messages.length - 1];
            if (lastMessage && lastMessage.role === 'user') {
                // 检查消息时间，如果是最近30秒内的，清除草稿
                const messageTime = new Date(lastMessage.createdAt);
                const now = new Date();
                const timeDiff = now.getTime() - messageTime.getTime();
                if (timeDiff < 30000) { // 30秒内
                    hasRecentUserMessage = true;
                }
            }
        }
        if (hasRecentUserMessage) {
            // 如果有最近发送的用户消息，清除草稿
            clearChatDraft();
            const chatInput = document.getElementById('chat-input');
            if (chatInput) {
                chatInput.value = '';
                adjustTextareaHeight(chatInput);
            }
        }

        // 加载消息 — 分批渲染避免长时间阻塞主线程
        if (conversation.messages && conversation.messages.length > 0) {
            const FIRST_BATCH = 20;  // 首批同步渲染（用户可见区域）
            const BATCH_SIZE = 10;   // 后续每批条数

            // 渲染单条消息的辅助函数
            const renderOneMessage = (msg) => {
                if (msg.role === 'user' && isInterruptContinueInjectChatMessage(msg.content)) {
                    return;
                }
                const assistantContent = String(msg && msg.content != null ? msg.content : '').trim();
                const terminalState = msg && msg.role === 'assistant'
                    ? assistantTurnTerminalState(msg.processDetails)
                    : null;
                let displayContent = msg.content;
                if (msg.role === 'assistant' &&
                    (assistantContent === '处理中...' || assistantContent === 'Processing...') && terminalState) {
                    displayContent = terminalState.detail.message || msg.content;
                }

                // 消息时间口径：
                // - user: createdAt 即可（发送后不会再更新）
                // - assistant: 如果后端提供 updatedAt（任务完成时写回），优先用它，避免占位消息“任务开始时间”误导
                const msgTime = (msg && msg.role === 'assistant' && msg.updatedAt) ? msg.updatedAt : (msg ? msg.createdAt : null);
                const mcpIds = (msg.mcpExecutionIds && Array.isArray(msg.mcpExecutionIds)) ? msg.mcpExecutionIds : [];
                const isAssistantPlaceholder = msg.role === 'assistant' && (
                    assistantContent === '处理中...' || assistantContent === 'Processing...'
                );
                const addOpts = (msg.role === 'assistant' && (mcpIds.length > 0 || isAssistantPlaceholder))
                    ? {
                        deferMcpButtons: mcpIds.length > 0,
                        hideAssistantPlaceholder: isAssistantPlaceholder
                    }
                    : null;
                const messageId = addMessage(msg.role, displayContent, mcpIds, null, msgTime, addOpts);
                const messageEl = document.getElementById(messageId);
                if (messageEl && msg && msg.id) {
                    messageEl.dataset.backendMessageId = String(msg.id);
                    attachDeleteTurnButton(messageEl);
                }
                if (msg.role === 'assistant') {
                    if (messageEl && typeof window.setAssistantTurnTiming === 'function') {
                        const startedAt = msg && msg.createdAt ? msg.createdAt : null;
                        const completedAt = terminalState && terminalState.completedAt
                            ? terminalState.completedAt
                            : (msg && msg.updatedAt ? msg.updatedAt : startedAt);
                        const startedMs = assistantTurnTimestamp(startedAt);
                        const completedMs = assistantTurnTimestamp(completedAt);
                        const isRunning = isAssistantPlaceholder && !terminalState;
                        const status = terminalState ? terminalState.status : (isRunning ? 'running' : 'completed');
                        window.setAssistantTurnTiming(messageEl, {
                            startedAt: startedAt,
                            completedAt: isRunning ? null : completedAt,
                            durationMs: (!isRunning && Number.isFinite(startedMs) && Number.isFinite(completedMs))
                                ? Math.max(0, completedMs - startedMs)
                                : undefined,
                            status: status
                        });
                    }
                    if (messageEl && msg.reasoningContent) {
                        setMessageReasoningContent(messageEl, msg.reasoningContent);
                    }
                    const hasField = msg && Object.prototype.hasOwnProperty.call(msg, 'processDetails');
                    renderProcessDetails(messageId, hasField ? (msg.processDetails || []) : null);
                    if (msg.processDetails && msg.processDetails.length > 0) {
                        const hasErrorOrCancelled = msg.processDetails.some(d =>
                            d.eventType === 'error' || d.eventType === 'cancelled'
                        );
                        if (hasErrorOrCancelled) {
                            collapseAllProgressDetails(messageId, null);
                        }
                    }
                }
            };

            const msgs = conversation.messages;
            const firstBatch = msgs.slice(0, FIRST_BATCH);
            const rest = msgs.slice(FIRST_BATCH);

            let pendingMessageBatches = Promise.resolve();

            // 首批同步渲染
            firstBatch.forEach(renderOneMessage);

            // 剩余消息通过 requestAnimationFrame 分批渲染，避免阻塞 UI
            if (rest.length > 0) {
                const savedConvId = conversationId;
                const savedSeq = seq;
                pendingMessageBatches = new Promise((resolve) => {
                    let offset = 0;
                    const renderNextBatch = () => {
                        if (savedSeq !== loadConversationRequestSeq || currentConversationId !== savedConvId) {
                            resolve();
                            return;
                        }
                        const batch = rest.slice(offset, offset + BATCH_SIZE);
                        batch.forEach(renderOneMessage);
                        offset += BATCH_SIZE;
                        if (offset < rest.length) {
                            requestAnimationFrame(renderNextBatch);
                        } else {
                            if (window.CyberStrikeChatScroll) {
                                window.CyberStrikeChatScroll.forceScrollToBottom(false);
                            } else {
                                messagesDiv.scrollTop = messagesDiv.scrollHeight;
                            }
                            resolve();
                        }
                    };
                    requestAnimationFrame(renderNextBatch);
                });
            }

            if (window.CyberStrikeChatScroll) {
                window.CyberStrikeChatScroll.forceScrollToBottom(false);
            } else {
                messagesDiv.scrollTop = messagesDiv.scrollHeight;
            }
            addAttackChainButton(conversationId);
            await pendingMessageBatches;
            if (seq !== loadConversationRequestSeq) {
                return;
            }
            hydrateConversationTokenUsage(conversationId, seq, conversationLoadController.signal).catch((e) => {
                if (!e || e.name !== 'AbortError') {
                    console.warn('hydrateConversationTokenUsage failed', e);
                }
            });
            if (currentConversationId === conversationId && typeof window.restoreHitlInlineForConversation === 'function') {
                await window.restoreHitlInlineForConversation(conversationId);
            }
            if (
                window.CyberStrikeChatScroll &&
                typeof window.CyberStrikeChatScroll.settleConversationRestoreToBottom === 'function'
            ) {
                window.CyberStrikeChatScroll.settleConversationRestoreToBottom(30);
            }
        } else {
            renderChatWelcomeEmptyState();
            if (window.CyberStrikeChatScroll) {
                window.CyberStrikeChatScroll.forceScrollToBottom(false);
            } else {
                messagesDiv.scrollTop = messagesDiv.scrollHeight;
            }
            addAttackChainButton(conversationId);
            if (seq !== loadConversationRequestSeq) {
                return;
            }
            if (currentConversationId === conversationId && typeof window.restoreHitlInlineForConversation === 'function') {
                await window.restoreHitlInlineForConversation(conversationId);
            }
        }

        // 页面刷新后主流式连接会中断；若该会话仍在后端运行，自动挂载 task-events 补流继续更新前端迭代进度。
        const skipReplay = typeof window.shouldSkipTaskEventReplayAttach === 'function'
            && window.shouldSkipTaskEventReplayAttach(conversationId);
        if (
            seq === loadConversationRequestSeq &&
            currentConversationId === conversationId &&
            typeof window.attachRunningTaskEventStream === 'function' &&
            !skipReplay
        ) {
            Promise.resolve()
                .then(() => window.attachRunningTaskEventStream(conversationId))
                .catch((e) => {
                    console.warn('attachRunningTaskEventStream on loadConversation failed', e);
                });
        } else if (seq === loadConversationRequestSeq && currentConversationId === conversationId) {
            // 机器人等非 Web 流式来源：会话已结束或未注册任务时，按需拉取最后一条助手消息的过程详情
            prefetchLastAssistantProcessDetails().catch((e) => {
                console.warn('prefetchLastAssistantProcessDetails failed', e);
            });
        }
    } catch (error) {
        if (error && error.name === 'AbortError') return;
        if (seq === loadConversationRequestSeq) {
            currentConversationId = previousConversationId;
            try {
                window.currentConversationId = previousConversationId || '';
            } catch (e) { /* ignore */ }
            if (previousConversationId) syncChatConversationHash(previousConversationId);
            else clearChatConversationHash();
            if (typeof window.selectChatProjectConversationItem === 'function') {
                window.selectChatProjectConversationItem(previousConversationId);
            }
        }
        console.error('加载对话失败:', error);
        showChatToast('加载对话失败: ' + (error && error.message ? error.message : String(error)), 'error');
    } finally {
        if (seq === loadConversationRequestSeq && typeof window.finishChatConversationRestore === 'function') {
            window.finishChatConversationRestore(conversationId);
        }
        if (loadConversationAbortController === conversationLoadController) {
            loadConversationAbortController = null;
        }
        if (seq === loadConversationRequestSeq && loadConversationPendingId === conversationId) {
            loadConversationPendingId = '';
        }
    }
}

/** 「删除本轮」：与时间戳同一行（message-meta-footer），风格与复制按钮区区分 */
function attachDeleteTurnButton(messageEl) {
    if (!messageEl || !messageEl.dataset.backendMessageId) return;
    if (messageEl.querySelector('.message-delete-turn-btn')) return;
    const content = messageEl.querySelector('.message-content');
    if (!content) return;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'message-delete-turn-btn';
    const title = typeof window.t === 'function' ? window.t('chat.deleteTurnTitle') : '删除本轮对话';
    btn.title = title;
    btn.setAttribute('aria-label', title);
    btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2m3 0v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6h14zM10 11v6M14 11v6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    btn.onclick = function (e) {
        e.stopPropagation();
        e.preventDefault();
        deleteConversationTurnFromUI(messageEl.dataset.backendMessageId);
    };
    const timeDiv = content.querySelector('.message-time');
    let footer = content.querySelector('.message-meta-footer');
    if (!footer && timeDiv && timeDiv.parentNode === content) {
        footer = document.createElement('div');
        footer.className = 'message-meta-footer';
        timeDiv.parentNode.insertBefore(footer, timeDiv);
        footer.appendChild(timeDiv);
    }
    if (footer) {
        footer.appendChild(btn);
    } else {
        content.appendChild(btn);
    }
}

/** 删除锚点所在整轮（后端：该轮 user 至下一轮 user 之前），并清空 ReAct 快照 */
async function deleteConversationTurnFromUI(anchorBackendMessageId) {
    if (!currentConversationId || !anchorBackendMessageId) return;
    const confirmMsg = typeof window.t === 'function' ? window.t('chat.deleteTurnConfirm') : '确定删除本轮对话？';
    if (!confirm(confirmMsg)) return;
    try {
        const response = await apiFetch(`/api/conversations/${currentConversationId}/delete-turn`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messageId: anchorBackendMessageId })
        });
        let data = {};
        try {
            data = await response.json();
        } catch (e) { /* ignore */ }
        if (!response.ok) {
            throw new Error(data.error || data.message || 'delete failed');
        }
        invalidateConversationLiteCache(currentConversationId);
        await loadConversation(currentConversationId);
        if (typeof loadConversations === 'function') {
            loadConversations();
        }
    } catch (error) {
        console.error('delete turn failed:', error);
        const failed = typeof window.t === 'function' ? window.t('chat.deleteTurnFailed') : '删除本轮失败';
        alert(failed + ': ' + (error && error.message ? error.message : error));
    }
}

// 删除对话
async function deleteConversation(conversationId, skipConfirm = false) {
    // 确认删除（如果调用者没有跳过确认）
    if (!skipConfirm) {
        if (!confirm('确定要删除这个对话吗？对话消息将不可恢复，但已记录的漏洞会保留在漏洞库中。')) {
            return;
        }
    }

    try {
        const response = await apiFetch(`/api/conversations/${conversationId}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || '删除失败');
        }

        // 如果删除的是当前对话，清空对话界面
        if (conversationId === currentConversationId) {
            currentConversationId = null;
            try {
                window.currentConversationId = '';
            } catch (e) { /* ignore */ }
            document.getElementById('chat-messages').innerHTML = '';
            renderChatWelcomeEmptyState();
            addAttackChainButton(null);
        }

        invalidateConversationLiteCache(conversationId);

        // 先同步所有侧栏的本地状态，再执行网络刷新。项目文件夹使用独立的
        // conversation cache；如果只刷新“最近对话”，删除项会一直残留到整页刷新。
        try {
            document.dispatchEvent(new CustomEvent('conversation-deleted', { detail: { conversationId } }));
        } catch (e) { /* ignore */ }

        // 刷新对话列表
        if (typeof loadConversations === 'function') {
            loadConversations();
        }

        // 批量管理弹窗打开时，同步刷新弹窗内列表
        const batchModal = document.getElementById('batch-manage-modal');
        if (batchModal && isAppModalOpen('batch-manage-modal')) {
            allConversationsForBatch = allConversationsForBatch.filter(c => c.id !== conversationId);
            applyBatchConversationFilters();
        }

    } catch (error) {
        console.error('删除对话失败:', error);
        alert('删除对话失败: ' + error.message);
    }
}

// 更新活动对话样式
function updateActiveConversation() {
    document.querySelectorAll('.conversation-item').forEach(item => {
        item.classList.remove('active');
        if (currentConversationId && item.dataset.conversationId === currentConversationId) {
            item.classList.add('active');
        }
    });
}

// ==================== 攻击链可视化功能 ====================

// 生成节点图标的 data URL（用于 Cytoscape background-image）
// 返回一个精美的渐变色方块 + 白色矢量图标
function _acBuildNodeIconDataUrl(iconType, color, colorDark) {
    let iconPath = '';
    if (iconType === 'target') {
        iconPath = 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm0-14c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6zm0 10c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4z';
    } else if (iconType === 'action') {
        iconPath = 'M7 2v11h3v9l7-12h-4l4-8z';
    } else if (iconType === 'vulnerability') {
        iconPath = 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-1 6h2v6h-2V7zm0 8h2v2h-2v-2z';
    } else {
        iconPath = 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z';
    }
    // 64x64 的图标方块（渐变 + 圆角 + 白色矢量图标）
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
<defs>
<linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
<stop offset="0%" stop-color="${color}"/>
<stop offset="100%" stop-color="${colorDark}"/>
</linearGradient>
</defs>
<rect x="0" y="0" width="64" height="64" rx="14" fill="url(#g)"/>
<g transform="translate(14 14) scale(1.5)"><path d="${iconPath}" fill="#FFFFFF"/></g>
</svg>`;
    // 使用 base64 编码（btoa 在浏览器中原生支持）
    try {
        return 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)));
    } catch (e) {
        // 兜底：URL 编码
        return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
    }
}

let attackChainCytoscape = null;
let currentAttackChainConversationId = null;
// 按对话ID管理加载状态，实现不同对话之间的解耦
const attackChainLoadingMap = new Map(); // Map<conversationId, boolean>

// 检查指定对话是否正在加载
function isAttackChainLoading(conversationId) {
    return attackChainLoadingMap.get(conversationId) === true;
}

// 设置指定对话的加载状态
function setAttackChainLoading(conversationId, loading) {
    if (loading) {
        attackChainLoadingMap.set(conversationId, true);
    } else {
        attackChainLoadingMap.delete(conversationId);
    }
}

// 添加攻击链按钮（已移至菜单，此函数保留以保持兼容性，但不再显示顶部按钮）
function addAttackChainButton(conversationId) {
    // 攻击链按钮已移至三点菜单，不再需要显示顶部按钮
    // 此函数保留以保持代码兼容性，但不再执行任何操作
    const conversationHeader = document.getElementById('conversation-header');
    if (conversationHeader) {
        conversationHeader.style.display = 'none';
    }
}

function updateAttackChainAvailability() {
    addAttackChainButton(currentConversationId);
}

// 显示攻击链模态框
async function showAttackChain(conversationId) {
    // 如果当前显示的对话ID不同，或者没有在加载，允许打开
    // 如果正在加载同一个对话，也允许打开（显示加载状态）
    if (isAttackChainLoading(conversationId) && currentAttackChainConversationId === conversationId) {
        // 如果模态框已经打开且显示的是同一个对话，不重复打开
        const modal = document.getElementById('attack-chain-modal');
        if (modal && isAppModalOpen('attack-chain-modal')) {
            console.log('攻击链正在加载中，模态框已打开');
            return;
        }
    }

    currentAttackChainConversationId = conversationId;
    const modal = document.getElementById('attack-chain-modal');
    if (!modal) {
        console.error('攻击链模态框未找到');
        return;
    }

    openAppModal('attack-chain-modal', { focus: false });
    updateAttackChainStats({ nodes: [], edges: [] });

    // 清空容器
    const container = document.getElementById('attack-chain-container');
    if (container) {
        container.innerHTML = '<div class="loading-spinner">' + (typeof window.t === 'function' ? window.t('chat.loading') : '加载中...') + '</div>';
    }

    // 隐藏详情面板
    const detailsPanel = document.getElementById('attack-chain-details');
    if (detailsPanel) {
        detailsPanel.style.display = 'none';
    }

    // 禁用重新生成按钮
    const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
    if (regenerateBtn) {
        regenerateBtn.disabled = true;
        regenerateBtn.style.opacity = '0.5';
        regenerateBtn.style.cursor = 'not-allowed';
    }

    // 加载攻击链数据
    await loadAttackChain(conversationId);
}

// 加载攻击链数据
async function loadAttackChain(conversationId) {
    if (isAttackChainLoading(conversationId)) {
        return; // 防止重复调用
    }

    setAttackChainLoading(conversationId, true);

    try {
        const response = await apiFetch(`/api/attack-chain/${conversationId}`);

        if (!response.ok) {
            // 处理 409 Conflict（正在生成中）
            if (response.status === 409) {
                const error = await response.json();
                const container = document.getElementById('attack-chain-container');
                if (container) {
                    container.innerHTML = `
                        <div style="text-align: center; padding: 28px 24px; color: var(--text-secondary);">
                            <div style="display: inline-flex; align-items: center; gap: 8px; font-size: 0.95rem; color: var(--text-primary);">
                                <span role="presentation" aria-hidden="true">⏳</span>
                                <span>攻击链生成中，请稍候</span>
                            </div>
                            <button class="btn-secondary" onclick="refreshAttackChain()" style="margin-top: 12px; font-size: 0.78rem; padding: 4px 12px;">
                                刷新
                            </button>
                        </div>
                    `;
                }
                // 5秒后自动刷新（允许刷新，但保持加载状态防止重复点击）
                // 使用闭包保存 conversationId，防止串台
                setTimeout(() => {
                    // 检查当前显示的对话ID是否匹配
                    if (currentAttackChainConversationId === conversationId) {
                        refreshAttackChain();
                    }
                }, 5000);
                // 在 409 情况下，保持加载状态，防止重复点击
                // 但允许 refreshAttackChain 调用 loadAttackChain 来检查状态
                // 注意：不重置加载状态，保持加载状态
                // 恢复按钮状态（虽然保持加载状态，但允许用户手动刷新）
                const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
                if (regenerateBtn) {
                    regenerateBtn.disabled = false;
                    regenerateBtn.style.opacity = '1';
                    regenerateBtn.style.cursor = 'pointer';
                }
                return; // 提前返回，不执行 finally 块中的 setAttackChainLoading(conversationId, false)
            }

            const error = await response.json();
            throw new Error(error.error || '加载攻击链失败');
        }

        const chainData = await response.json();

        // 检查当前显示的对话ID是否匹配，防止串台
        if (currentAttackChainConversationId !== conversationId) {
            console.log('攻击链数据已返回，但当前显示的对话已切换，忽略此次渲染', {
                returned: conversationId,
                current: currentAttackChainConversationId
            });
            setAttackChainLoading(conversationId, false);
            return;
        }

        // 渲染攻击链
        renderAttackChain(chainData);

        // 更新统计信息
        updateAttackChainStats(chainData);

        // 成功加载后，重置加载状态
        setAttackChainLoading(conversationId, false);

    } catch (error) {
        console.error('加载攻击链失败:', error);
        const container = document.getElementById('attack-chain-container');
        if (container) {
            container.innerHTML = '<div class="error-message">' + (typeof window.t === 'function' ? window.t('chat.loadFailed', { message: error.message }) : '加载失败: ' + error.message) + '</div>';
        }
        // 错误时也重置加载状态
        setAttackChainLoading(conversationId, false);
    } finally {
        // 恢复重新生成按钮
        const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
        if (regenerateBtn) {
            regenerateBtn.disabled = false;
            regenerateBtn.style.opacity = '1';
            regenerateBtn.style.cursor = 'pointer';
        }
    }
}

// 渲染攻击链
function renderAttackChain(chainData) {
    const container = document.getElementById('attack-chain-container');
    if (!container) {
        return;
    }

    // 清空容器
    container.innerHTML = '';

    if (!chainData.nodes || chainData.nodes.length === 0) {
        container.innerHTML = '<div class="empty-message">' + (typeof window.t === 'function' ? window.t('chat.noAttackChainData') : '暂无攻击链数据') + '</div>';
        return;
    }

    // 计算图的复杂度（用于动态调整布局和样式）
    const nodeCount = chainData.nodes.length;
    const edgeCount = chainData.edges.length;
    const isComplexGraph = nodeCount > 15 || edgeCount > 25;
    const isDarkTheme = document.documentElement.getAttribute('data-theme') === 'dark';

    // 优化节点标签：智能截断和换行
    chainData.nodes.forEach(node => {
        if (node.label) {
            // 智能截断：优先在标点符号、空格处截断
            const maxLength = isComplexGraph ? 18 : 22;
            if (node.label.length > maxLength) {
                let truncated = node.label.substring(0, maxLength);
                // 尝试在最后一个标点符号或空格处截断
                const lastPunct = Math.max(
                    truncated.lastIndexOf('，'),
                    truncated.lastIndexOf('。'),
                    truncated.lastIndexOf('、'),
                    truncated.lastIndexOf(' '),
                    truncated.lastIndexOf('/')
                );
                if (lastPunct > maxLength * 0.6) { // 如果标点符号位置合理
                    truncated = truncated.substring(0, lastPunct + 1);
                }
                node.label = truncated + '...';
            }
        }
    });

    // 准备Cytoscape数据
    const elements = [];

    // 添加节点，并预计算样式信息（与导出保持一致的主题色）
    chainData.nodes.forEach(node => {
        const riskScore = node.risk_score || 0;
        const nodeType = node.type || '';
        const metadata = node.metadata || {};

        // 统一的主题系统（与导出一致）
        let typeLabel = '节点';
        let typeEn = 'NODE';
        let typeColor = '#334155';      // 主文字色
        let accentColor = '#94a3b8';    // 强调色（图标/边框）
        let accentDark = '#475569';     // 深色版本
        let bgGradientStart = '#FFFFFF';
        let bgGradientEnd = '#F8FAFC';
        let iconType = 'default';       // 图标类型

        if (nodeType === 'target') {
            typeLabel = '目标';
            typeEn = 'TARGET';
            typeColor = '#312E81';
            accentColor = '#4F46E5';
            accentDark = '#3730A3';
            bgGradientStart = '#FFFFFF';
            bgGradientEnd = '#F5F3FF';
            iconType = 'target';
        } else if (nodeType === 'action') {
            typeLabel = '行动';
            typeEn = 'ACTION';
            const findings = metadata.findings || [];
            const hasFindings = Array.isArray(findings) && findings.length > 0;
            const isFailedInsight = (metadata.status || '') === 'failed_insight';
            if (hasFindings && !isFailedInsight) {
                typeColor = '#064E3B';
                accentColor = '#10B981';
                accentDark = '#047857';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#ECFDF5';
            } else {
                typeColor = '#334155';
                accentColor = '#64748B';
                accentDark = '#475569';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#F8FAFC';
            }
            iconType = 'action';
        } else if (nodeType === 'vulnerability') {
            typeLabel = '漏洞';
            typeEn = 'VULNERABILITY';
            if (riskScore >= 80) {
                typeColor = '#881337';
                accentColor = '#E11D48';
                accentDark = '#BE123C';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#FFF1F2';
            } else if (riskScore >= 60) {
                typeColor = '#7C2D12';
                accentColor = '#EA580C';
                accentDark = '#C2410C';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#FFF7ED';
            } else if (riskScore >= 40) {
                typeColor = '#713F12';
                accentColor = '#CA8A04';
                accentDark = '#A16207';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#FEFCE8';
            } else {
                typeColor = '#134E4A';
                accentColor = '#0D9488';
                accentDark = '#0F766E';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#F0FDFA';
            }
            iconType = 'vulnerability';
        }

        const labelTextColor = isDarkTheme ? '#E5E7EB' : '#0F172A';
        if (isDarkTheme) {
            typeColor = '#E5E7EB';
            bgGradientStart = '#111827';
            if (nodeType === 'target') {
                bgGradientEnd = '#1E1B4B';
            } else if (nodeType === 'action') {
                bgGradientEnd = accentColor === '#10B981' ? '#052E2B' : '#172033';
            } else if (nodeType === 'vulnerability') {
                if (riskScore >= 80) {
                    bgGradientEnd = '#3F101C';
                } else if (riskScore >= 60) {
                    bgGradientEnd = '#3B1D0D';
                } else if (riskScore >= 40) {
                    bgGradientEnd = '#3A2A0A';
                } else {
                    bgGradientEnd = '#063A36';
                }
            } else {
                bgGradientEnd = '#172033';
            }
        }

        // 为每个节点生成图标 background-image（data URL）
        const iconSvg = _acBuildNodeIconDataUrl(iconType, accentColor, accentDark);

        // 计算徽章文本（右上角）
        let badgeText = '';
        if (nodeType === 'vulnerability' && riskScore > 0) {
            const rl = riskScore >= 80 ? '严重' : riskScore >= 60 ? '高' : riskScore >= 40 ? '中' : '低';
            badgeText = rl + ' · ' + riskScore;
        } else if (nodeType === 'action') {
            const findings = metadata.findings || [];
            if (Array.isArray(findings) && findings.length > 0 && metadata.status !== 'failed_insight') {
                badgeText = '发现 ' + findings.length;
            } else if (metadata.status === 'failed_insight') {
                badgeText = '有线索';
            }
        } else if (nodeType === 'target') {
            badgeText = '主目标';
        }

        elements.push({
            data: {
                id: node.id,
                label: node.label,
                originalLabel: node.label,
                type: nodeType,
                typeLabel: typeLabel,
                typeEn: typeEn,
                typeColor: typeColor,
                accentColor: accentColor,
                accentDark: accentDark,
                bgGradientStart: bgGradientStart,
                bgGradientEnd: bgGradientEnd,
                labelTextColor: labelTextColor,
                iconDataUrl: iconSvg,
                badgeText: badgeText,
                riskScore: riskScore,
                toolExecutionId: node.tool_execution_id || '',
                metadata: metadata
            }
        });
    });

    // 添加边（只添加源节点和目标节点都存在的边）
    const nodeIds = new Set(chainData.nodes.map(node => node.id));

    // 保存有效的边用于ELK布局
    const validEdges = [];
    chainData.edges.forEach(edge => {
        // 验证源节点和目标节点是否存在
        if (nodeIds.has(edge.source) && nodeIds.has(edge.target)) {
            validEdges.push(edge);
            elements.push({
                data: {
                    id: edge.id,
                    source: edge.source,
                    target: edge.target,
                    type: edge.type || 'leads_to',
                    weight: edge.weight || 1
                }
            });
        } else {
            console.warn('跳过无效的边：源节点或目标节点不存在', {
                edgeId: edge.id,
                source: edge.source,
                target: edge.target,
                sourceExists: nodeIds.has(edge.source),
                targetExists: nodeIds.has(edge.target)
            });
        }
    });

    // 初始化Cytoscape - 现代卡片式节点设计（图标 + 文字 + 徽章）
    attackChainCytoscape = cytoscape({
        container: container,
        elements: elements,
        style: [
            {
                selector: 'node',
                style: {
                    // 节点 label：两行文字（类型英文 | 主标题）
                    'label': function(ele) {
                        const typeEn = ele.data('typeEn') || '';
                        const typeLabel = ele.data('typeLabel') || '';
                        const label = ele.data('label') || '';
                        const badgeText = ele.data('badgeText') || '';
                        // 第一行：TYPE_EN · 类型（小字）
                        // 第二行：主标题（大字）
                        // 第三行：徽章文字（彩色提示）
                        let line1 = typeEn + '  ·  ' + typeLabel;
                        if (badgeText) line1 += '  [' + badgeText + ']';
                        return line1 + '\n' + label;
                    },
                    'width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'target') return isComplexGraph ? 300 : 360;
                        if (type === 'vulnerability') return isComplexGraph ? 280 : 340;
                        return isComplexGraph ? 260 : 320;
                    },
                    'height': function(ele) {
                        return isComplexGraph ? 84 : 100;
                    },
                    'shape': 'round-rectangle',
                    // 浅色渐变背景（白色到主题色极淡）
                    'background-fill': 'linear-gradient',
                    'background-gradient-direction': 'to-bottom-right',
                    'background-gradient-stop-colors': function(ele) {
                        return (ele.data('bgGradientStart') || '#FFFFFF') + ' ' +
                               (ele.data('bgGradientEnd') || '#F8FAFC');
                    },
                    'background-gradient-stop-positions': '0 100',
                    'background-opacity': 1,
                    // 左侧类型图标（SVG dataURL 作为背景图）
                    'background-image': function(ele) {
                        return ele.data('iconDataUrl') || 'none';
                    },
                    'background-image-containment': 'inside',
                    'background-fit': 'none',
                    'background-image-opacity': 1,
                    'background-width': '36px',
                    'background-height': '36px',
                    'background-position-x': '18px',
                    'background-position-y': '50%',
                    'background-offset-y': '0',
                    'background-clip': 'node',
                    'bounds-expansion': 0,
                    // 边框：主题色柔和
                    'border-width': 1.5,
                    'border-color': function(ele) {
                        return ele.data('accentColor') || '#94a3b8';
                    },
                    'border-opacity': 0.5,
                    // 文字样式
                    'color': function(ele) {
                        return ele.data('labelTextColor') || '#0f172a';
                    },
                    'font-size': function(ele) {
                        return isComplexGraph ? '13px' : '14px';
                    },
                    'font-weight': 700,
                    'font-family': '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", "PingFang SC", "Microsoft YaHei", sans-serif',
                    // 文字左对齐、预留左侧图标空间
                    'text-valign': 'center',
                    'text-halign': 'center',
                    'text-justification': 'left',
                    'text-wrap': 'wrap',
                    'text-max-width': function(ele) {
                        const type = ele.data('type');
                        const w = (type === 'target') ? (isComplexGraph ? 300 : 360)
                                : (type === 'vulnerability') ? (isComplexGraph ? 280 : 340)
                                : (isComplexGraph ? 260 : 320);
                        return (w - 80) + 'px';
                    },
                    'text-overflow-wrap': 'anywhere',
                    'text-margin-x': 28,
                    'text-margin-y': 0,
                    'padding': '12px',
                    'line-height': 1.4,
                    'text-outline-width': 0,
                    // 柔和阴影（overlay 模拟）
                    'overlay-color': '#0f172a',
                    'overlay-opacity': 0,
                    'overlay-padding': 0,
                    'transition-property': 'overlay-opacity, border-width, border-color',
                    'transition-duration': '160ms'
                }
            },
            {
                // 目标节点：边框略粗
                selector: 'node[type = "target"]',
                style: {
                    'border-width': 2
                }
            },
            {
                // 漏洞节点：边框略粗
                selector: 'node[type = "vulnerability"]',
                style: {
                    'border-width': 2
                }
            },
            {
                selector: 'edge',
                style: {
                    'width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers') return 2.6;
                        if (type === 'enables') return 2.8;
                        return 2;
                    },
                    'line-color': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers' || type === 'targets') return '#4F46E5';
                        if (type === 'enables') return '#E11D48';
                        if (type === 'leads_to') return '#64748B';
                        return '#cbd5e1';
                    },
                    'target-arrow-color': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers' || type === 'targets') return '#4F46E5';
                        if (type === 'enables') return '#E11D48';
                        if (type === 'leads_to') return '#64748B';
                        return '#cbd5e1';
                    },
                    'target-arrow-shape': 'triangle-backcurve',
                    'arrow-scale': 1.35,
                    'curve-style': 'bezier',
                    'control-point-step-size': 60,
                    'opacity': 0.88,
                    'line-style': function(ele) {
                        const type = ele.data('type');
                        if (type === 'targets') return 'dashed';
                        return 'solid';
                    },
                    'line-dash-pattern': function(ele) {
                        const type = ele.data('type');
                        if (type === 'targets') return [10, 5];
                        return [];
                    },
                    'transition-property': 'opacity, width, line-color',
                    'transition-duration': '160ms'
                }
            },
            {
                selector: 'node:selected',
                style: {
                    'border-width': 3.5,
                    'border-color': '#4F46E5',
                    'z-index': 999,
                    'opacity': 1,
                    'overlay-opacity': 0.06,
                    'overlay-color': '#4F46E5',
                    'overlay-padding': 8
                }
            }
        ],
        userPanningEnabled: true,
        userZoomingEnabled: true,
        boxSelectionEnabled: true,
        minZoom: 0.2,
        maxZoom: 3
    });

    // 使用ELK布局（高质量DAG布局，减少边交叉）
    let layoutOptions = {
        name: 'breadthfirst',
        directed: true,
        spacingFactor: isComplexGraph ? 3.0 : 2.5,
        padding: 40
    };

    // 使用ELK.js进行布局计算
    // elk.bundled.js会暴露ELK对象，可以直接使用new ELK()
    let elkInstance = null;
    if (typeof ELK !== 'undefined') {
        try {
            elkInstance = new ELK();
        } catch (e) {
            console.warn('ELK初始化失败:', e);
        }
    }

    if (elkInstance) {
        try {

            // === 布局参数（始终使用 DOWN 纵向布局）===
            const isSmallGraph = chainData.nodes.length <= 8 && validEdges.length <= 12;
            // 同层节点间距（横向分散）
            const nodeGap = isComplexGraph ? 45 : isSmallGraph ? 80 : 60;
            // 层间距（纵向：给连线足够发挥空间，同时避免图太高）
            const layerGap = isComplexGraph ? 70 : isSmallGraph ? 130 : 95;

            // 构建 ELK 图结构 - 节点尺寸与 Cytoscape 样式保持一致
            const elkGraph = {
                id: 'root',
                layoutOptions: {
                    'elk.algorithm': 'layered',
                    'elk.direction': 'DOWN',
                    'elk.padding': '[top=30,left=50,bottom=30,right=50]',
                    'elk.spacing.nodeNode': String(nodeGap),
                    'elk.spacing.edgeNode': '20',
                    'elk.spacing.edgeEdge': '12',
                    'elk.spacing.componentComponent': '50',
                    'elk.layered.spacing.nodeNodeBetweenLayers': String(layerGap),
                    'elk.layered.spacing.edgeNodeBetweenLayers': '20',
                    'elk.layered.spacing.edgeEdgeBetweenLayers': '12',
                    'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
                    'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED',
                    'elk.layered.nodePlacement.bk.edgeStraightening': 'IMPROVE_STRAIGHTNESS',
                    'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
                    'elk.layered.crossingMinimization.semiInteractive': 'false',
                    'elk.layered.thoroughness': String(isComplexGraph ? 10 : 15),
                    'elk.layered.cycleBreaking.strategy': 'GREEDY',
                    'elk.layered.compaction.connectedComponents': 'true',
                    'elk.layered.compaction.postCompaction.strategy': 'LEFT_RIGHT_CONSTRAINT_LOCKING',
                    'elk.layered.unnecessaryBendpoints': 'true',
                    'elk.layered.mergeEdges': 'false'
                },
                children: chainData.nodes.map(node => {
                    const type = node.type || '';
                    return {
                        id: node.id,
                        width: type === 'target' ? (isComplexGraph ? 300 : 360) :
                               type === 'vulnerability' ? (isComplexGraph ? 280 : 340) :
                               (isComplexGraph ? 260 : 320),
                        height: isComplexGraph ? 84 : 100
                    };
                }),
                edges: validEdges.map(edge => ({
                    id: edge.id,
                    sources: [edge.source],
                    targets: [edge.target]
                }))
            };

            // 使用ELK计算布局
            elkInstance.layout(elkGraph).then(laidOutGraph => {
                // 应用ELK计算的布局到Cytoscape节点
                if (laidOutGraph && laidOutGraph.children) {
                    laidOutGraph.children.forEach(elkNode => {
                        const cyNode = attackChainCytoscape.getElementById(elkNode.id);
                        if (cyNode && elkNode.x !== undefined && elkNode.y !== undefined) {
                            cyNode.position({
                                x: elkNode.x + (elkNode.width || 0) / 2,
                                y: elkNode.y + (elkNode.height || 0) / 2
                            });
                        }
                    });

                    // 布局完成后，居中显示图
                    setTimeout(() => {
                        centerAttackChain();
                    }, 150);
                } else {
                    throw new Error('ELK布局返回无效结果');
                }
            }).catch(err => {
                console.warn('ELK布局计算失败，使用默认布局:', err);
                // 回退到默认布局
                const layout = attackChainCytoscape.layout(layoutOptions);
                layout.one('layoutstop', () => {
                    setTimeout(() => {
                        centerAttackChain();
                    }, 100);
                });
                layout.run();
            });
        } catch (e) {
            console.warn('ELK布局初始化失败，使用默认布局:', e);
            // 回退到默认布局
            const layout = attackChainCytoscape.layout(layoutOptions);
            layout.one('layoutstop', () => {
                setTimeout(() => {
                    centerAttackChain();
                }, 100);
            });
            layout.run();
        }
    } else {
        console.warn('ELK.js未加载，使用默认布局。请检查elkjs库是否正确加载。');
        // 使用默认布局
        const layout = attackChainCytoscape.layout(layoutOptions);
        layout.one('layoutstop', () => {
            setTimeout(() => {
                centerAttackChain();
            }, 100);
        });
        layout.run();
    }

    // 居中攻击链的函数：始终让所有节点完整可见
    function centerAttackChain() {
        try {
            if (!attackChainCytoscape) {
                return;
            }
            const container = attackChainCytoscape.container();
            if (!container) return;
            const containerWidth = container.offsetWidth;
            const containerHeight = container.offsetHeight;
            if (containerWidth === 0 || containerHeight === 0) {
                setTimeout(centerAttackChain, 100);
                return;
            }

            // 使用较大 padding 让节点不贴边，视觉上更舒适
            // 核心原则：完全依赖 fit 的结果来保证全局可见，不强制最小缩放
            const padding = 60;
            attackChainCytoscape.fit(undefined, padding);

            // 只在极端情况下微调：小图（2-3 节点）fit 后缩放过大时适当降低
            setTimeout(() => {
                if (!attackChainCytoscape) return;
                const currentZoom = attackChainCytoscape.zoom();
                // 上限：避免节点占满屏幕看起来过大
                const MAX_INITIAL_ZOOM = 1.25;
                // 下限：避免极小图看不清（极小图通常节点很少）
                const MIN_READABLE_ZOOM = 0.25;

                let targetZoom = currentZoom;
                if (currentZoom > MAX_INITIAL_ZOOM) {
                    targetZoom = MAX_INITIAL_ZOOM;
                } else if (currentZoom < MIN_READABLE_ZOOM) {
                    // 如果 fit 后缩放低于 0.25，说明图超大；保持当前结果，让用户可拖动查看
                    targetZoom = MIN_READABLE_ZOOM;
                }

                if (Math.abs(targetZoom - currentZoom) > 0.01) {
                    const extent = attackChainCytoscape.extent();
                    const cx = (extent.x1 + extent.x2) / 2;
                    const cy = (extent.y1 + extent.y2) / 2;
                    attackChainCytoscape.zoom({
                        level: targetZoom,
                        position: { x: cx, y: cy }
                    });
                }
                attackChainCytoscape.center();
            }, 60);
        } catch (error) {
            console.warn('居中图表时出错:', error);
        }
    }

    // 添加点击事件
    attackChainCytoscape.on('tap', 'node', function(evt) {
        const node = evt.target;
        showNodeDetails(node.data());
    });

    // 点击空白处关闭详情
    attackChainCytoscape.on('tap', function(evt) {
        if (evt.target === attackChainCytoscape) {
            attackChainCytoscape.elements().unselect();
        }
    });

    // 添加悬停效果：增强边框 + 柔光叠加 + 淡化不相关连线
    attackChainCytoscape.on('mouseover', 'node', function(evt) {
        const node = evt.target;
        const accent = node.data('accentColor') || '#4F46E5';
        node.style({
            'border-width': 3,
            'border-color': accent,
            'border-opacity': 1,
            'overlay-color': accent,
            'overlay-opacity': 0.08,
            'overlay-padding': 10,
            'z-index': 998
        });
        const connected = node.connectedEdges();
        attackChainCytoscape.edges().not(connected).style('opacity', 0.2);
        connected.style({ 'opacity': 1, 'width': 3.5 });
    });

    attackChainCytoscape.on('mouseout', 'node', function(evt) {
        const node = evt.target;
        const type = node.data('type');
        const defaultBorderWidth = (type === 'target' || type === 'vulnerability') ? 2 : 1.5;
        node.style({
            'border-width': defaultBorderWidth,
            'border-color': node.data('accentColor') || '#94a3b8',
            'border-opacity': 0.5,
            'overlay-opacity': 0,
            'overlay-padding': 0,
            'z-index': 0
        });
        attackChainCytoscape.edges().style({ 'opacity': 0.88, 'width': '' });
    });

    // 保存原始数据用于过滤
    window.attackChainOriginalData = chainData;
}

// 安全地获取边的源节点和目标节点
function getEdgeNodes(edge) {
    try {
        const source = edge.source();
        const target = edge.target();

        // 检查源节点和目标节点是否存在
        if (!source || !target || source.length === 0 || target.length === 0) {
            return { source: null, target: null, valid: false };
        }

        return { source: source, target: target, valid: true };
    } catch (error) {
        console.warn('获取边的节点时出错:', error, edge.id());
        return { source: null, target: null, valid: false };
    }
}

// 过滤攻击链节点（按搜索关键词）
function filterAttackChainNodes(searchText) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }

    const searchLower = searchText.toLowerCase().trim();
    if (searchLower === '') {
        // 重置所有节点可见性
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        // 恢复默认边框
        attackChainCytoscape.nodes().style('border-width', 2);
        return;
    }

    // 过滤节点
    attackChainCytoscape.nodes().forEach(node => {
        // 使用原始标签进行搜索，不包含类型标签
        const originalLabel = node.data('originalLabel') || node.data('label') || '';
        const label = originalLabel.toLowerCase();
        const type = (node.data('type') || '').toLowerCase();
        const matches = label.includes(searchLower) || type.includes(searchLower);

        if (matches) {
            node.style('display', 'element');
            // 高亮匹配的节点
            node.style('border-width', 4);
            node.style('border-color', '#0066ff');
        } else {
            node.style('display', 'none');
        }
    });

    // 隐藏没有可见源节点或目标节点的边
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }

        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });

    // 重新调整视图
    attackChainCytoscape.fit(undefined, 60);
}

// 按类型过滤攻击链节点
function filterAttackChainByType(type) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }

    if (type === 'all') {
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.nodes().style('border-width', 2);
        attackChainCytoscape.fit(undefined, 60);
        return;
    }

    // 过滤节点
    attackChainCytoscape.nodes().forEach(node => {
        const nodeType = node.data('type') || '';
        if (nodeType === type) {
            node.style('display', 'element');
        } else {
            node.style('display', 'none');
        }
    });

    // 隐藏没有可见源节点或目标节点的边
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }

        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });

    // 重新调整视图
    attackChainCytoscape.fit(undefined, 60);
}

// 按风险等级过滤攻击链节点
function filterAttackChainByRisk(riskLevel) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }

    if (riskLevel === 'all') {
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.nodes().style('border-width', 2);
        attackChainCytoscape.fit(undefined, 60);
        return;
    }

    // 定义风险范围
    const riskRanges = {
        'high': [80, 100],
        'medium-high': [60, 79],
        'medium': [40, 59],
        'low': [0, 39]
    };

    const [minRisk, maxRisk] = riskRanges[riskLevel] || [0, 100];

    // 过滤节点
    attackChainCytoscape.nodes().forEach(node => {
        const riskScore = node.data('riskScore') || 0;
        if (riskScore >= minRisk && riskScore <= maxRisk) {
            node.style('display', 'element');
        } else {
            node.style('display', 'none');
        }
    });

    // 隐藏没有可见源节点或目标节点的边
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }

        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });

    // 重新调整视图
    attackChainCytoscape.fit(undefined, 60);
}

// 重置攻击链筛选
function resetAttackChainFilters() {
    // 重置搜索框
    const searchInput = document.getElementById('attack-chain-search');
    if (searchInput) {
        searchInput.value = '';
    }

    // 重置类型筛选
    const typeFilter = document.getElementById('attack-chain-type-filter');
    if (typeFilter) {
        typeFilter.value = 'all';
    }

    // 重置风险筛选
    const riskFilter = document.getElementById('attack-chain-risk-filter');
    if (riskFilter) {
        riskFilter.value = 'all';
    }

    // 重置所有节点可见性
    if (attackChainCytoscape) {
        attackChainCytoscape.nodes().forEach(node => {
            node.style('display', 'element');
            node.style('border-width', 2); // 恢复默认边框
        });
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.fit(undefined, 60);
    }
}

// 显示节点详情
function showNodeDetails(nodeData) {
    const detailsPanel = document.getElementById('attack-chain-details');
    const detailsContent = document.getElementById('attack-chain-details-content');

    if (!detailsPanel || !detailsContent) {
        return;
    }

    // 给 sidebar 标记详情激活态，CSS 会隐藏图例让详情独占空间
    const sidebar = document.querySelector('.attack-chain-sidebar');
    if (sidebar) sidebar.classList.add('details-active');

    // 使用 requestAnimationFrame 优化显示动画
    requestAnimationFrame(() => {
        detailsPanel.style.display = 'flex';
        requestAnimationFrame(() => {
            detailsPanel.style.opacity = '1';
        });
    });

    let html = `
        <div class="node-detail-item">
            <strong>节点ID:</strong> <code>${nodeData.id}</code>
        </div>
        <div class="node-detail-item">
            <strong>类型:</strong> ${getNodeTypeLabel(nodeData.type)}
        </div>
        <div class="node-detail-item">
            <strong>标签:</strong> ${escapeHtml(nodeData.originalLabel || nodeData.label)}
        </div>
        <div class="node-detail-item">
            <strong>风险评分:</strong> ${nodeData.riskScore}/100
        </div>
    `;

    // 显示action节点信息（工具执行 + AI分析）
    if (nodeData.type === 'action' && nodeData.metadata) {
        if (nodeData.metadata.tool_name) {
            html += `
                <div class="node-detail-item">
                    <strong>工具名称:</strong> <code>${escapeHtml(nodeData.metadata.tool_name)}</code>
                </div>
            `;
        }
        if (nodeData.metadata.tool_intent) {
            html += `
                <div class="node-detail-item">
                    <strong>工具意图:</strong> <span style="color: #0066ff; font-weight: bold;">${escapeHtml(nodeData.metadata.tool_intent)}</span>
                </div>
            `;
        }
        if (nodeData.metadata.status === 'failed_insight') {
            html += `
                <div class="node-detail-item">
                    <strong>执行状态:</strong> <span style="color: #ff9800; font-weight: bold;">失败但有线索</span>
                </div>
            `;
        }
        if (nodeData.metadata.ai_analysis) {
            html += `
                <div class="node-detail-item">
                    <strong>AI分析:</strong> <div class="node-detail-ai-analysis">${escapeHtml(nodeData.metadata.ai_analysis)}</div>
                </div>
            `;
        }
        if (nodeData.metadata.findings && Array.isArray(nodeData.metadata.findings) && nodeData.metadata.findings.length > 0) {
            html += `
                <div class="node-detail-item">
                    <strong>关键发现:</strong>
                    <ul style="margin: 5px 0; padding-left: 20px;">
                        ${nodeData.metadata.findings.map(f => `<li>${escapeHtml(f)}</li>`).join('')}
                    </ul>
                </div>
            `;
        }
    }

    // 显示目标信息（如果是目标节点）
    if (nodeData.type === 'target' && nodeData.metadata && nodeData.metadata.target) {
        html += `
            <div class="node-detail-item">
                <strong>测试目标:</strong> <code>${escapeHtml(nodeData.metadata.target)}</code>
            </div>
        `;
    }

    // 显示漏洞信息（如果是漏洞节点）
    if (nodeData.type === 'vulnerability' && nodeData.metadata) {
        if (nodeData.metadata.vulnerability_type) {
            html += `
                <div class="node-detail-item">
                    <strong>漏洞类型:</strong> ${escapeHtml(nodeData.metadata.vulnerability_type)}
                </div>
            `;
        }
        if (nodeData.metadata.description) {
            html += `
                <div class="node-detail-item">
                    <strong>描述:</strong> ${escapeHtml(nodeData.metadata.description)}
                </div>
            `;
        }
        if (nodeData.metadata.severity) {
            html += `
                <div class="node-detail-item">
                    <strong>严重程度:</strong> <span style="color: ${getSeverityColor(nodeData.metadata.severity)}; font-weight: bold;">${escapeHtml(nodeData.metadata.severity)}</span>
                </div>
            `;
        }
        if (nodeData.metadata.location) {
            html += `
                <div class="node-detail-item">
                    <strong>位置:</strong> <code>${escapeHtml(nodeData.metadata.location)}</code>
                </div>
            `;
        }
    }

    if (nodeData.toolExecutionId) {
        html += `
            <div class="node-detail-item">
                <strong>工具执行ID:</strong> <code>${nodeData.toolExecutionId}</code>
            </div>
        `;
    }

    // 详情占满 sidebar 后，内容区滚动由自身处理，重置到顶部
    if (detailsContent) {
        detailsContent.scrollTop = 0;
    }

    requestAnimationFrame(() => {
        detailsContent.innerHTML = html;
        requestAnimationFrame(() => {
            if (detailsContent) {
                detailsContent.scrollTop = 0;
            }
        });
    });
}

// 获取严重程度颜色
function getSeverityColor(severity) {
    const colors = {
        'critical': '#ff0000',
        'high': '#ff4444',
        'medium': '#ff8800',
        'low': '#ffbb00'
    };
    return colors[severity.toLowerCase()] || '#666';
}

// 获取节点类型标签
function getNodeTypeLabel(type) {
    const labels = {
        'action': '行动',
        'vulnerability': '漏洞',
        'target': '目标'
    };
    return labels[type] || type;
}

// 更新统计信息（使用 i18n，与 attackChainModal.nodesEdges 一致）
function updateAttackChainStats(chainData) {
    const statsElement = document.getElementById('attack-chain-stats');
    if (statsElement) {
        const nodeCount = chainData.nodes ? chainData.nodes.length : 0;
        const edgeCount = chainData.edges ? chainData.edges.length : 0;
        if (typeof window.t === 'function') {
            statsElement.textContent = window.t('attackChainModal.nodesEdges', {
                nodes: nodeCount,
                edges: edgeCount
            });
        } else {
            statsElement.textContent = `Nodes: ${nodeCount} | Edges: ${edgeCount}`;
        }
    }
}

// 语言切换时刷新攻击链统计文案（动态 textContent 不会随 applyTranslations 更新）
document.addEventListener('languagechange', function () {
    if (window.attackChainOriginalData && typeof updateAttackChainStats === 'function') {
        updateAttackChainStats(window.attackChainOriginalData);
    } else {
        const statsEl = document.getElementById('attack-chain-stats');
        if (statsEl && typeof window.t === 'function') {
            statsEl.textContent = window.t('attackChainModal.nodesEdges', { nodes: 0, edges: 0 });
        }
    }
});

// 关闭节点详情
function closeNodeDetails() {
    const detailsPanel = document.getElementById('attack-chain-details');
    const sidebar = document.querySelector('.attack-chain-sidebar');

    if (detailsPanel) {
        detailsPanel.style.opacity = '0';
        setTimeout(() => {
            detailsPanel.style.display = 'none';
            detailsPanel.style.opacity = '';
            // 移除详情激活态，图例恢复显示
            if (sidebar) sidebar.classList.remove('details-active');
        }, 220);
    } else if (sidebar) {
        sidebar.classList.remove('details-active');
    }

    if (attackChainCytoscape) {
        attackChainCytoscape.elements().unselect();
    }
}

// 关闭攻击链模态框
function closeAttackChainModal() {
    closeAppModal('attack-chain-modal');

    // 关闭节点详情
    closeNodeDetails();

    // 清理Cytoscape实例
    if (attackChainCytoscape) {
        attackChainCytoscape.destroy();
        attackChainCytoscape = null;
    }

    currentAttackChainConversationId = null;
}

// 刷新攻击链（重新加载）
// 注意：此函数允许在加载过程中调用，用于检查生成状态
function refreshAttackChain() {
    if (currentAttackChainConversationId) {
        // 临时允许刷新，即使正在加载中（用于检查生成状态）
        const wasLoading = isAttackChainLoading(currentAttackChainConversationId);
        setAttackChainLoading(currentAttackChainConversationId, false); // 临时重置，允许刷新
        loadAttackChain(currentAttackChainConversationId).finally(() => {
            // 如果之前正在加载（409 情况），恢复加载状态
            // 否则保持 false（正常完成）
            if (wasLoading) {
                // 检查是否仍然需要保持加载状态（如果还是 409，会在 loadAttackChain 中处理）
                // 这里我们假设如果成功加载，则重置状态
                // 如果还是 409，loadAttackChain 会保持加载状态
            }
        });
    }
}

// 重新生成攻击链
async function regenerateAttackChain() {
    if (!currentAttackChainConversationId) {
        return;
    }

    // 防止重复点击（只检查当前对话的加载状态）
    if (isAttackChainLoading(currentAttackChainConversationId)) {
        console.log('攻击链正在生成中，请稍候...');
        return;
    }

    // 保存请求时的对话ID，防止串台
    const savedConversationId = currentAttackChainConversationId;
    setAttackChainLoading(savedConversationId, true);

    const container = document.getElementById('attack-chain-container');
    if (container) {
        container.innerHTML = '<div class="loading-spinner">重新生成中...</div>';
    }

    // 禁用重新生成按钮
    const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
    if (regenerateBtn) {
        regenerateBtn.disabled = true;
        regenerateBtn.style.opacity = '0.5';
        regenerateBtn.style.cursor = 'not-allowed';
    }

    try {
        // 调用重新生成接口
        const response = await apiFetch(`/api/attack-chain/${savedConversationId}/regenerate`, {
            method: 'POST'
        });

        if (!response.ok) {
            // 处理 409 Conflict（正在生成中）
            if (response.status === 409) {
                const error = await response.json();
                if (container) {
                    container.innerHTML = `
                        <div class="loading-spinner" style="text-align: center; padding: 40px;">
                            <div style="margin-bottom: 16px;">⏳ 攻击链正在生成中...</div>
                            <div style="color: var(--text-secondary); font-size: 0.875rem;">
                                请稍候，生成完成后将自动显示
                            </div>
                            <button class="btn-secondary" onclick="refreshAttackChain()" style="margin-top: 16px;">
                                刷新查看进度
                            </button>
                        </div>
                    `;
                }
                // 5秒后自动刷新
                // savedConversationId 已在函数开始处定义
                setTimeout(() => {
                    // 检查当前显示的对话ID是否匹配，且仍在加载中
                    if (currentAttackChainConversationId === savedConversationId &&
                        isAttackChainLoading(savedConversationId)) {
                        refreshAttackChain();
                    }
                }, 5000);
                return;
            }

            const error = await response.json();
            throw new Error(error.error || '重新生成攻击链失败');
        }

        const chainData = await response.json();

        // 检查当前显示的对话ID是否匹配，防止串台
        if (currentAttackChainConversationId !== savedConversationId) {
            console.log('攻击链数据已返回，但当前显示的对话已切换，忽略此次渲染', {
                returned: savedConversationId,
                current: currentAttackChainConversationId
            });
            setAttackChainLoading(savedConversationId, false);
            return;
        }

        // 渲染攻击链
        renderAttackChain(chainData);

        // 更新统计信息
        updateAttackChainStats(chainData);

    } catch (error) {
        console.error('重新生成攻击链失败:', error);
        if (container) {
            container.innerHTML = `<div class="error-message">重新生成失败: ${error.message}</div>`;
        }
    } finally {
        setAttackChainLoading(savedConversationId, false);

        // 恢复重新生成按钮
        if (regenerateBtn) {
            regenerateBtn.disabled = false;
            regenerateBtn.style.opacity = '1';
            regenerateBtn.style.cursor = 'pointer';
        }
    }
}

// ==================== 攻击链导出（精美版） ====================

// XML/HTML 转义
function _acEscapeXml(str) {
    if (str === null || str === undefined) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&apos;');
}

// 按字符宽度做软换行（支持中英文混排），返回字符串数组
function _acWrapLabel(label, maxChars, maxLines) {
    if (!label) return [''];
    const text = String(label).replace(/\s+/g, ' ').trim();
    if (!text) return [''];
    // 以"字符宽度"估算：中文算2，其他算1
    const width = (ch) => (/[\u4e00-\u9fa5\uff00-\uffef]/.test(ch) ? 2 : 1);
    const maxW = maxChars * 1.8; // 以单位宽度衡量

    const lines = [];
    let buf = '';
    let bufW = 0;
    let lastSpaceIdx = -1;
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        const w = width(ch);
        if (ch === ' ') lastSpaceIdx = buf.length;
        if (bufW + w > maxW) {
            // 在空格处换行（英文更自然）
            let cut = buf;
            let rest = '';
            if (lastSpaceIdx > 0 && lastSpaceIdx >= buf.length - 10) {
                cut = buf.substring(0, lastSpaceIdx);
                rest = buf.substring(lastSpaceIdx + 1);
            }
            lines.push(cut);
            if (lines.length >= maxLines) {
                // 在最后加省略号
                const last = lines[lines.length - 1];
                lines[lines.length - 1] = _acTruncateToWidth(last, maxW - 2) + '…';
                return lines;
            }
            buf = rest + ch;
            bufW = 0;
            for (let j = 0; j < buf.length; j++) bufW += width(buf[j]);
            lastSpaceIdx = -1;
        } else {
            buf += ch;
            bufW += w;
        }
    }
    if (buf) lines.push(buf);
    if (lines.length > maxLines) {
        const kept = lines.slice(0, maxLines);
        kept[kept.length - 1] = _acTruncateToWidth(kept[kept.length - 1], maxW - 2) + '…';
        return kept;
    }
    return lines;
}

function _acTruncateToWidth(str, maxW) {
    const width = (ch) => (/[\u4e00-\u9fa5\uff00-\uffef]/.test(ch) ? 2 : 1);
    let w = 0;
    let out = '';
    for (let i = 0; i < str.length; i++) {
        w += width(str[i]);
        if (w > maxW) break;
        out += str[i];
    }
    return out;
}

// 计算颜色对应的深色主色（辅助 accent 深色）
function _acDarken(hex, amount) {
    try {
        const h = hex.replace('#', '');
        const r = parseInt(h.substring(0, 2), 16);
        const g = parseInt(h.substring(2, 4), 16);
        const b = parseInt(h.substring(4, 6), 16);
        const f = (c) => Math.max(0, Math.min(255, Math.round(c * (1 - amount))));
        return '#' + [f(r), f(g), f(b)].map(x => x.toString(16).padStart(2, '0')).join('');
    } catch (e) {
        return hex;
    }
}

// 从当前 Cytoscape 实例收集导出所需的节点/边信息
function _acCollectExportData() {
    if (!attackChainCytoscape) return null;
    const nodes = [];
    attackChainCytoscape.nodes().forEach(n => {
        // 过滤隐藏节点
        if (n.style('display') === 'none') return;
        const pos = n.position();
        // 读取 Cytoscape 中实际渲染的节点尺寸，保证导出与看板一致
        let w = n.outerWidth ? n.outerWidth() : n.width();
        let h = n.outerHeight ? n.outerHeight() : n.height();
        // 兜底
        if (!w || !isFinite(w) || w < 40) w = 280;
        if (!h || !isFinite(h) || h < 30) h = 96;
        nodes.push({
            id: n.id(),
            x: pos.x,
            y: pos.y,
            w: w,
            h: h,
            type: n.data('type') || '',
            typeLabel: n.data('typeLabel') || '',
            typeBadge: n.data('typeBadge') || '•',
            typeColor: n.data('typeColor') || '#334155',
            accentColor: n.data('accentColor') || '#94a3b8',
            bgGradientStart: n.data('bgGradientStart') || '#FFFFFF',
            bgGradientEnd: n.data('bgGradientEnd') || '#F8FAFC',
            riskScore: n.data('riskScore') || 0,
            label: n.data('originalLabel') || n.data('label') || n.id(),
            metadata: n.data('metadata') || {}
        });
    });

    const edges = [];
    attackChainCytoscape.edges().forEach(e => {
        if (e.style('display') === 'none') return;
        const info = getEdgeNodes(e);
        if (!info.valid) return;
        const s = info.source.position();
        const t = info.target.position();
        edges.push({
            id: e.id(),
            source: info.source.id(),
            target: info.target.id(),
            sx: s.x, sy: s.y,
            tx: t.x, ty: t.y,
            type: e.data('type') || 'leads_to'
        });
    });

    return { nodes, edges };
}

// 节点类型图标（SVG path）—— 真正的矢量图标
function _acGetNodeIconPath(type) {
    // 24×24 视图下的 path（会被缩放到 iconSize）
    if (type === 'target') {
        // 靶子（同心圆 + 十字准星）
        return 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm0-14c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6zm0 10c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4z';
    }
    if (type === 'action') {
        // 闪电（行动）
        return 'M7 2v11h3v9l7-12h-4l4-8z';
    }
    if (type === 'vulnerability') {
        // 盾牌警告
        return 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-1 6h2v6h-2V7zm0 8h2v2h-2v-2z';
    }
    // 默认点
    return 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z';
}

// 获取节点风险等级标签（漏洞节点用）
function _acGetRiskLabel(score) {
    if (score >= 80) return '严重';
    if (score >= 60) return '高';
    if (score >= 40) return '中';
    if (score > 0) return '低';
    return '';
}

// 生成精美 SVG 字符串（高端商业报告风格）
function _acBuildSvgString() {
    const data = _acCollectExportData();
    if (!data || data.nodes.length === 0) throw new Error('没有可导出的数据');

    const { nodes, edges } = data;

    // --- 关键：重新统一节点尺寸为大卡片设计（SVG 中使用自己的规格） ---
    // SVG 导出时采用更大的卡片，让信息层次清晰
    nodes.forEach(n => {
        // 根据当前在 Cytoscape 中的位置，重新分配 SVG 版本的宽高
        // 统一使用大卡片以便展示完整信息
        n.w = 380;
        n.h = 140;
    });

    // 计算图的包围盒
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    nodes.forEach(n => {
        minX = Math.min(minX, n.x - n.w / 2);
        minY = Math.min(minY, n.y - n.h / 2);
        maxX = Math.max(maxX, n.x + n.w / 2);
        maxY = Math.max(maxY, n.y + n.h / 2);
    });

    // ==================== 版面布局参数 ====================
    const GRAPH_PAD = 100;                   // 图区域内部留白
    const HEADER_H = 128;                    // 顶部标题栏（加大）
    const FOOTER_H = 56;                     // 底部信息栏
    const LEGEND_W = 320;                    // 右侧图例面板
    const OUTER_PAD = 32;                    // 最外层留白

    const rawGraphW = (maxX - minX) + GRAPH_PAD * 2;
    const rawGraphH = (maxY - minY) + GRAPH_PAD * 2;

    const minGraphW = 900;
    const minGraphH = 620;
    const graphW = Math.max(rawGraphW, minGraphW);
    const graphH = Math.max(rawGraphH, minGraphH);

    const contentW = graphW + LEGEND_W + 36;
    const contentH = graphH + HEADER_H + FOOTER_H + 24;

    const totalW = contentW + OUTER_PAD * 2;
    const totalH = contentH + OUTER_PAD * 2;

    // 图区域在卡片坐标系的位置
    const graphAreaX = OUTER_PAD + 20;
    const graphAreaY = OUTER_PAD + HEADER_H;
    const graphAreaW = graphW - 4;
    const graphAreaH = contentH - HEADER_H - FOOTER_H;

    // 节点坐标映射
    const graphCenterOffsetX = (graphAreaW - rawGraphW) / 2;
    const graphCenterOffsetY = (graphAreaH - rawGraphH) / 2;
    const graphOriginX = graphAreaX + GRAPH_PAD + graphCenterOffsetX - minX;
    const graphOriginY = graphAreaY + GRAPH_PAD + graphCenterOffsetY - minY;

    // 图例区坐标
    const legendX = graphAreaX + graphW + 16;

    // 统计信息
    const nodeCount = nodes.length;
    const edgeCount = edges.length;
    const vulnNodes = nodes.filter(n => n.type === 'vulnerability');
    const actionNodes = nodes.filter(n => n.type === 'action');
    const targetNodes = nodes.filter(n => n.type === 'target');
    const criticalCount = vulnNodes.filter(n => n.riskScore >= 80).length;
    const highCount = vulnNodes.filter(n => n.riskScore >= 60 && n.riskScore < 80).length;
    const medCount = vulnNodes.filter(n => n.riskScore >= 40 && n.riskScore < 60).length;
    const lowCount = vulnNodes.filter(n => n.riskScore > 0 && n.riskScore < 40).length;

    const timestamp = new Date();
    const ts = timestamp.getFullYear() + '-' +
        String(timestamp.getMonth() + 1).padStart(2, '0') + '-' +
        String(timestamp.getDate()).padStart(2, '0') + ' ' +
        String(timestamp.getHours()).padStart(2, '0') + ':' +
        String(timestamp.getMinutes()).padStart(2, '0');

    // 类型主题色（高级感配色）
    const typeTheme = {
        'target': { primary: '#4F46E5', light: '#EEF2FF', dark: '#3730A3', text: '#312E81', label: '目标' },
        'action-success': { primary: '#10B981', light: '#ECFDF5', dark: '#047857', text: '#064E3B', label: '行动' },
        'action-neutral': { primary: '#64748B', light: '#F8FAFC', dark: '#475569', text: '#334155', label: '行动' },
        'vuln-critical': { primary: '#E11D48', light: '#FFF1F2', dark: '#BE123C', text: '#881337', label: '漏洞' },
        'vuln-high': { primary: '#EA580C', light: '#FFF7ED', dark: '#C2410C', text: '#7C2D12', label: '漏洞' },
        'vuln-med': { primary: '#CA8A04', light: '#FEFCE8', dark: '#A16207', text: '#713F12', label: '漏洞' },
        'vuln-low': { primary: '#0D9488', light: '#F0FDFA', dark: '#0F766E', text: '#134E4A', label: '漏洞' }
    };

    function themeFor(n) {
        if (n.type === 'target') return typeTheme['target'];
        if (n.type === 'action') {
            const m = n.metadata || {};
            const findings = m.findings || [];
            const hasFindings = Array.isArray(findings) && findings.length > 0;
            const isFailed = m.status === 'failed_insight';
            return (hasFindings && !isFailed) ? typeTheme['action-success'] : typeTheme['action-neutral'];
        }
        if (n.type === 'vulnerability') {
            const s = n.riskScore || 0;
            if (s >= 80) return typeTheme['vuln-critical'];
            if (s >= 60) return typeTheme['vuln-high'];
            if (s >= 40) return typeTheme['vuln-med'];
            return typeTheme['vuln-low'];
        }
        return typeTheme['action-neutral'];
    }

    // 边的起止主题
    const nodesMap = new Map(nodes.map(n => [n.id, n]));

    // ==================== 开始组装 SVG ====================
    const parts = [];
    parts.push(`<?xml version="1.0" encoding="UTF-8"?>`);
    parts.push(`<svg xmlns="http://www.w3.org/2000/svg" width="${totalW}" height="${totalH}" viewBox="0 0 ${totalW} ${totalH}" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', 'Hiragino Sans GB', Roboto, Helvetica, Arial, sans-serif">`);

    // ==================== defs ====================
    parts.push(`<defs>`);

    // 根背景渐变：极淡暖灰
    parts.push(`<linearGradient id="ac-bg" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#FAFBFC"/>
        <stop offset="100%" stop-color="#F1F5F9"/>
    </linearGradient>`);

    // 角落光晕
    parts.push(`<radialGradient id="ac-glow-1" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#6366F1" stop-opacity="0.12"/>
        <stop offset="100%" stop-color="#6366F1" stop-opacity="0"/>
    </radialGradient>`);
    parts.push(`<radialGradient id="ac-glow-2" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#EC4899" stop-opacity="0.08"/>
        <stop offset="100%" stop-color="#EC4899" stop-opacity="0"/>
    </radialGradient>`);
    parts.push(`<radialGradient id="ac-glow-3" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#06B6D4" stop-opacity="0.08"/>
        <stop offset="100%" stop-color="#06B6D4" stop-opacity="0"/>
    </radialGradient>`);

    // 品牌渐变（标题用）
    parts.push(`<linearGradient id="ac-brand" x1="0%" y1="0%" x2="100%" y2="0%">
        <stop offset="0%" stop-color="#4F46E5"/>
        <stop offset="50%" stop-color="#7C3AED"/>
        <stop offset="100%" stop-color="#EC4899"/>
    </linearGradient>`);

    // 网格点阵（非常淡）
    parts.push(`<pattern id="ac-dot" x="0" y="0" width="24" height="24" patternUnits="userSpaceOnUse">
        <circle cx="12" cy="12" r="1" fill="#0F172A" fill-opacity="0.06"/>
    </pattern>`);

    // 节点卡片阴影（多层阴影，更有层次）
    parts.push(`<filter id="ac-shadow-card" x="-20%" y="-20%" width="140%" height="140%">
        <feDropShadow dx="0" dy="1" stdDeviation="1.5" flood-color="#0F172A" flood-opacity="0.06"/>
        <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#0F172A" flood-opacity="0.08"/>
    </filter>`);

    // 图标徽章阴影
    parts.push(`<filter id="ac-shadow-icon" x="-30%" y="-30%" width="160%" height="160%">
        <feDropShadow dx="0" dy="2" stdDeviation="3" flood-color="#0F172A" flood-opacity="0.15"/>
    </filter>`);

    // 风险徽章阴影
    parts.push(`<filter id="ac-shadow-badge" x="-30%" y="-30%" width="160%" height="160%">
        <feDropShadow dx="0" dy="1.5" stdDeviation="2.5" flood-color="#0F172A" flood-opacity="0.18"/>
    </filter>`);

    // 为每个节点定义图标渐变（大图标用）
    Object.keys(typeTheme).forEach(key => {
        const t = typeTheme[key];
        parts.push(`<linearGradient id="ac-icon-grad-${key}" x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stop-color="${t.primary}"/>
            <stop offset="100%" stop-color="${t.dark}"/>
        </linearGradient>`);
    });

    // 边使用渐变（源 -> 目标）
    edges.forEach((e, idx) => {
        const sNode = nodesMap.get(e.source);
        const tNode = nodesMap.get(e.target);
        if (!sNode || !tNode) return;
        const sTheme = themeFor(sNode);
        const tTheme = themeFor(tNode);
        parts.push(`<linearGradient id="ac-edge-grad-${idx}" gradientUnits="userSpaceOnUse" x1="${e.sx}" y1="${e.sy}" x2="${e.tx}" y2="${e.ty}">
            <stop offset="0%" stop-color="${sTheme.primary}" stop-opacity="0.7"/>
            <stop offset="100%" stop-color="${tTheme.primary}" stop-opacity="0.9"/>
        </linearGradient>`);
    });

    // 为每种主题色定义箭头标记
    Object.keys(typeTheme).forEach(key => {
        const t = typeTheme[key];
        parts.push(`<marker id="ac-arrow-${key}" viewBox="0 0 12 12" refX="10" refY="6" markerWidth="8" markerHeight="8" orient="auto-start-reverse" markerUnits="strokeWidth">
            <path d="M 0 0 L 12 6 L 0 12 L 3 6 Z" fill="${t.primary}"/>
        </marker>`);
    });

    parts.push(`</defs>`);

    // ==================== 背景 ====================
    parts.push(`<rect x="0" y="0" width="${totalW}" height="${totalH}" fill="url(#ac-bg)"/>`);
    // 角落光晕
    parts.push(`<ellipse cx="${totalW * 0.1}" cy="${totalH * 0.15}" rx="${totalW * 0.4}" ry="${totalH * 0.4}" fill="url(#ac-glow-1)"/>`);
    parts.push(`<ellipse cx="${totalW * 0.9}" cy="${totalH * 0.85}" rx="${totalW * 0.35}" ry="${totalH * 0.35}" fill="url(#ac-glow-2)"/>`);
    parts.push(`<ellipse cx="${totalW * 0.5}" cy="${totalH * 0.1}" rx="${totalW * 0.3}" ry="${totalH * 0.3}" fill="url(#ac-glow-3)"/>`);

    // ==================== 主卡片 ====================
    parts.push(`<rect x="${OUTER_PAD}" y="${OUTER_PAD}" width="${contentW}" height="${contentH}" rx="24" ry="24" fill="#FFFFFF" stroke="rgba(15,23,42,0.06)" stroke-width="1" filter="url(#ac-shadow-card)"/>`);

    // ==================== 顶部标题栏 ====================
    const tX = OUTER_PAD + 40;
    const tY = OUTER_PAD + 28;

    // 左侧 Logo 色块（大，渐变，有设计感）
    parts.push(`<g filter="url(#ac-shadow-icon)">`);
    parts.push(`<rect x="${tX - 4}" y="${tY}" width="48" height="48" rx="12" fill="url(#ac-brand)"/>`);
    // Logo 图标（六边形 + 闪电）
    parts.push(`<g transform="translate(${tX - 4 + 12}, ${tY + 12}) scale(0.9)">
        <path d="M12 2L3 7v10l9 5 9-5V7z" fill="none" stroke="#FFFFFF" stroke-width="1.8" stroke-linejoin="round"/>
        <path d="M10 7l-2 5h3l-1 4 4-5h-3l1-4z" fill="#FFFFFF"/>
    </g>`);
    parts.push(`</g>`);

    // 主标题（超大、粗体）
    parts.push(`<text x="${tX + 56}" y="${tY + 26}" font-size="26" font-weight="800" fill="#0F172A" letter-spacing="-0.6px">攻击链可视化报告</text>`);

    // 副标题（小字、次要色）
    parts.push(`<text x="${tX + 56}" y="${tY + 50}" font-size="13" font-weight="500" fill="#64748B" letter-spacing="0.1px">Attack Chain Analysis · ${_acEscapeXml(ts)}</text>`);

    // 右上角：关键统计胶囊（3 个）
    const kpiY = OUTER_PAD + 28;
    const kpiH = 48;
    const kpiGap = 12;
    const kpiW = 110;
    const kpiItems = [
        { label: '节点', value: nodeCount, color: '#4F46E5' },
        { label: '连线', value: edgeCount, color: '#06B6D4' },
        { label: '严重漏洞', value: criticalCount, color: criticalCount > 0 ? '#E11D48' : '#94A3B8' }
    ];
    let kpiXStart = OUTER_PAD + contentW - 40 - (kpiW * kpiItems.length + kpiGap * (kpiItems.length - 1));
    kpiItems.forEach((kpi, i) => {
        const kx = kpiXStart + i * (kpiW + kpiGap);
        // 卡片背景
        parts.push(`<rect x="${kx}" y="${kpiY}" width="${kpiW}" height="${kpiH}" rx="12" fill="#FFFFFF" stroke="${kpi.color}" stroke-opacity="0.15" stroke-width="1"/>`);
        // 左侧细条
        parts.push(`<rect x="${kx}" y="${kpiY + 10}" width="3" height="${kpiH - 20}" rx="1.5" fill="${kpi.color}"/>`);
        // 数值（大字）
        parts.push(`<text x="${kx + 16}" y="${kpiY + 26}" font-size="20" font-weight="800" fill="#0F172A" letter-spacing="-0.4px">${kpi.value}</text>`);
        // 标签（小字）
        parts.push(`<text x="${kx + 16}" y="${kpiY + 40}" font-size="10.5" font-weight="600" fill="#64748B" letter-spacing="0.4px">${_acEscapeXml(kpi.label)}</text>`);
    });

    // 标题分隔线（渐变淡化）
    parts.push(`<line x1="${OUTER_PAD + 40}" y1="${OUTER_PAD + HEADER_H - 10}" x2="${OUTER_PAD + contentW - 40}" y2="${OUTER_PAD + HEADER_H - 10}" stroke="rgba(15,23,42,0.08)" stroke-width="1"/>`);

    // ==================== 图区域 ====================
    parts.push(`<rect x="${graphAreaX}" y="${graphAreaY}" width="${graphAreaW}" height="${graphAreaH}" rx="18" fill="#FCFCFD" stroke="rgba(15,23,42,0.05)" stroke-width="1"/>`);
    parts.push(`<rect x="${graphAreaX}" y="${graphAreaY}" width="${graphAreaW}" height="${graphAreaH}" rx="18" fill="url(#ac-dot)" opacity="0.7"/>`);

    // ==================== 开始绘制图形 ====================
    parts.push(`<g transform="translate(${graphOriginX}, ${graphOriginY})">`);

    // ---- 边（渐变、柔和曲线） ----
    edges.forEach((e, idx) => {
        const sNode = nodesMap.get(e.source);
        const tNode = nodesMap.get(e.target);
        if (!sNode || !tNode) return;
        const tTheme = themeFor(tNode);

        const dx = e.tx - e.sx;
        const dy = e.ty - e.sy;
        const mag = Math.sqrt(dx * dx + dy * dy) || 1;
        const offset = Math.min(80, mag * 0.25);
        const nx = -dy / mag;
        const ny = dx / mag;
        const cx = (e.sx + e.tx) / 2 + nx * offset;
        const cy = (e.sy + e.ty) / 2 + ny * offset;
        const shrink = 22;
        const ex = e.tx - (dx / mag) * shrink;
        const ey = e.ty - (dy / mag) * shrink;

        const strokeWidth = (e.type === 'discovers' || e.type === 'enables') ? 2.4 : 2;
        const strokeDash = e.type === 'targets' ? 'stroke-dasharray="10,5"' : '';
        // 目标箭头 key（根据目标节点的主题）
        const targetThemeKey = Object.keys(typeTheme).find(k => typeTheme[k] === tTheme) || 'action-neutral';

        // 先画一个轻微的 halo（背景光晕）
        parts.push(`<path d="M ${e.sx.toFixed(1)} ${e.sy.toFixed(1)} Q ${cx.toFixed(1)} ${cy.toFixed(1)} ${ex.toFixed(1)} ${ey.toFixed(1)}" fill="none" stroke="${tTheme.primary}" stroke-width="${strokeWidth + 4}" stroke-linecap="round" stroke-opacity="0.08" ${strokeDash}/>`);
        // 主线
        parts.push(`<path d="M ${e.sx.toFixed(1)} ${e.sy.toFixed(1)} Q ${cx.toFixed(1)} ${cy.toFixed(1)} ${ex.toFixed(1)} ${ey.toFixed(1)}" fill="none" stroke="url(#ac-edge-grad-${idx})" stroke-width="${strokeWidth}" stroke-linecap="round" ${strokeDash} marker-end="url(#ac-arrow-${targetThemeKey})"/>`);
    });

    // ---- 节点（大卡片设计） ----
    nodes.forEach((n, i) => {
        const theme = themeFor(n);
        const themeKey = Object.keys(typeTheme).find(k => typeTheme[k] === theme) || 'action-neutral';

        const x = n.x - n.w / 2;
        const y = n.y - n.h / 2;
        const r = 18;  // 圆角

        // ========== 卡片主体 ==========
        // 柔和阴影 + 纯白背景
        parts.push(`<g filter="url(#ac-shadow-card)">`);
        parts.push(`<rect x="${x}" y="${y}" width="${n.w}" height="${n.h}" rx="${r}" fill="#FFFFFF"/>`);
        parts.push(`</g>`);
        // 顶部主题色条（很淡的渐变）
        parts.push(`<rect x="${x}" y="${y}" width="${n.w}" height="${n.h}" rx="${r}" fill="${theme.primary}" fill-opacity="0.02"/>`);
        // 细边框
        parts.push(`<rect x="${x}" y="${y}" width="${n.w}" height="${n.h}" rx="${r}" fill="none" stroke="${theme.primary}" stroke-opacity="0.18" stroke-width="1"/>`);
        // 顶部彩色装饰条（小圆点序列或渐变条）
        parts.push(`<rect x="${x + 20}" y="${y}" width="${n.w - 40}" height="3" rx="1.5" fill="${theme.primary}" fill-opacity="0.5"/>`);

        const padX = 24;
        const padY = 22;

        // ========== 顶部：大图标 + 类型标签 + 右侧徽章 ==========
        const iconSize = 44;
        const iconX = x + padX;
        const iconY = y + padY;

        // 图标背景（渐变方形）
        parts.push(`<g filter="url(#ac-shadow-icon)">`);
        parts.push(`<rect x="${iconX}" y="${iconY}" width="${iconSize}" height="${iconSize}" rx="12" fill="url(#ac-icon-grad-${themeKey})"/>`);
        parts.push(`</g>`);
        // 图标 path（白色，缩放）
        const iconPath = _acGetNodeIconPath(n.type);
        const iconScale = (iconSize * 0.55) / 24;
        const iconInnerOffset = (iconSize - 24 * iconScale) / 2;
        parts.push(`<g transform="translate(${iconX + iconInnerOffset}, ${iconY + iconInnerOffset}) scale(${iconScale.toFixed(3)})">
            <path d="${iconPath}" fill="#FFFFFF"/>
        </g>`);

        // 类型标签（在图标右侧）
        const typeTextX = iconX + iconSize + 14;
        // 类型英文（TYPE LABEL，淡色小字）
        const typeEn = n.type === 'target' ? 'TARGET' : n.type === 'action' ? 'ACTION' : n.type === 'vulnerability' ? 'VULNERABILITY' : (n.type || '').toUpperCase();
        parts.push(`<text x="${typeTextX}" y="${iconY + 14}" font-size="10" font-weight="700" fill="${theme.dark}" fill-opacity="0.75" letter-spacing="1.2px">${_acEscapeXml(typeEn)}</text>`);
        // 类型中文（大字，主要色）
        parts.push(`<text x="${typeTextX}" y="${iconY + 34}" font-size="16" font-weight="700" fill="${theme.text}" letter-spacing="-0.2px">${_acEscapeXml(theme.label)}</text>`);

        // ========== 右上角徽章 ==========
        const badgeY = iconY + 2;
        const badgeH = 26;
        if (n.type === 'vulnerability' && n.riskScore > 0) {
            // 风险分数徽章（大号，渐变背景）
            const riskLabel = _acGetRiskLabel(n.riskScore);
            const badgeText = `${riskLabel} · ${n.riskScore}`;
            const badgeW = 90;
            const bx = x + n.w - badgeW - padX;
            parts.push(`<g filter="url(#ac-shadow-badge)">`);
            parts.push(`<rect x="${bx}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeH / 2}" fill="url(#ac-icon-grad-${themeKey})"/>`);
            parts.push(`<text x="${bx + badgeW / 2}" y="${badgeY + badgeH / 2 + 4.5}" text-anchor="middle" font-size="12" font-weight="700" fill="#FFFFFF" letter-spacing="0.2px">${_acEscapeXml(badgeText)}</text>`);
            parts.push(`</g>`);
        } else if (n.type === 'action') {
            const m = n.metadata || {};
            const findings = m.findings || [];
            const hasFindings = Array.isArray(findings) && findings.length > 0;
            const isFailed = m.status === 'failed_insight';
            if (hasFindings || isFailed) {
                const text = isFailed ? '有线索' : `发现 ${findings.length}`;
                const badgeW = 70;
                const bx = x + n.w - badgeW - padX;
                parts.push(`<rect x="${bx}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeH / 2}" fill="${theme.primary}" fill-opacity="0.12" stroke="${theme.primary}" stroke-opacity="0.4" stroke-width="1"/>`);
                // 小圆点（状态指示）
                parts.push(`<circle cx="${bx + 12}" cy="${badgeY + badgeH / 2}" r="3" fill="${theme.primary}"/>`);
                parts.push(`<text x="${bx + 20}" y="${badgeY + badgeH / 2 + 4.5}" font-size="11.5" font-weight="700" fill="${theme.dark}">${_acEscapeXml(text)}</text>`);
            }
        } else if (n.type === 'target') {
            // 目标节点显示"目标"标识
            const badgeW = 60;
            const bx = x + n.w - badgeW - padX;
            parts.push(`<rect x="${bx}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeH / 2}" fill="${theme.primary}" fill-opacity="0.12" stroke="${theme.primary}" stroke-opacity="0.4" stroke-width="1"/>`);
            parts.push(`<text x="${bx + badgeW / 2}" y="${badgeY + badgeH / 2 + 4.5}" text-anchor="middle" font-size="11.5" font-weight="700" fill="${theme.dark}" letter-spacing="0.3px">主目标</text>`);
        }

        // ========== 主标题 ==========
        const contentTopY = iconY + iconSize + 18;
        const titleFontSize = 16;
        const titleLineH = titleFontSize + 6;
        const contentAvailW = n.w - padX * 2;
        const charsPerLine = Math.max(10, Math.floor(contentAvailW / (titleFontSize * 0.58)));
        const titleLines = _acWrapLabel(n.label, charsPerLine, 2);
        titleLines.forEach((ln, idx) => {
            parts.push(`<text x="${x + padX}" y="${contentTopY + idx * titleLineH}" font-size="${titleFontSize}" font-weight="700" fill="#0F172A" letter-spacing="-0.2px">${_acEscapeXml(ln)}</text>`);
        });

        // ========== 底部元信息栏 ==========
        const metaY = y + n.h - 22;
        // 分隔线
        parts.push(`<line x1="${x + padX}" y1="${metaY - 10}" x2="${x + n.w - padX}" y2="${metaY - 10}" stroke="rgba(15,23,42,0.06)" stroke-width="1"/>`);

        // 生成元信息文本
        const metaItems = [];
        if (n.type === 'target') {
            const tgt = (n.metadata && n.metadata.target) ? n.metadata.target : null;
            if (tgt) metaItems.push({ icon: 'loc', text: _acTruncateToWidth(tgt, 26) });
        } else if (n.type === 'action') {
            const toolName = n.metadata && n.metadata.tool_name;
            if (toolName) metaItems.push({ icon: 'tool', text: _acTruncateToWidth(toolName, 20) });
            const intent = n.metadata && n.metadata.tool_intent;
            if (intent) metaItems.push({ icon: 'aim', text: _acTruncateToWidth(intent, 22) });
        } else if (n.type === 'vulnerability') {
            const vt = n.metadata && n.metadata.vulnerability_type;
            if (vt) metaItems.push({ icon: 'shield', text: _acTruncateToWidth(vt, 22) });
            const sev = n.metadata && n.metadata.severity;
            if (sev) metaItems.push({ icon: 'alert', text: _acTruncateToWidth(sev, 12) });
        }
        if (metaItems.length === 0) {
            // 没有元信息时显示节点ID简短版
            metaItems.push({ icon: 'hash', text: _acTruncateToWidth(n.id || '', 20) });
        }

        // 元信息图标 path（24x24）
        const metaIconPaths = {
            'loc': 'M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5a2.5 2.5 0 1 1 0-5 2.5 2.5 0 0 1 0 5z',
            'tool': 'M22.7 19l-9.1-9.1c.9-2.3.4-5-1.5-6.9-2-2-5-2.4-7.4-1.3L9 6 6 9 1.6 4.7C.4 7.1.9 10.1 2.9 12.1c1.9 1.9 4.6 2.4 6.9 1.5l9.1 9.1c.4.4 1 .4 1.4 0l2.3-2.3c.5-.4.5-1.1.1-1.4z',
            'aim': 'M12 2L4 5v6c0 5.5 3.8 10.7 8 12 4.2-1.3 8-6.5 8-12V5l-8-3zm4 10H8V9h3V7l3 3-3 3v-1z',
            'shield': 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-2 16l-4-4 1.4-1.4 2.6 2.6 6.6-6.6L18 9l-8 8z',
            'alert': 'M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z',
            'hash': 'M20 9h-4.5l.9-4h-2l-.9 4H9l.9-4H8l-.9 4H3v2h3.7l-1 4H2v2h3.3l-.9 4h2l.9-4H12l-.9 4h2l.9-4H19v-2h-4.7l1-4H20V9zm-6.3 6H9l1-4h4.7l-1 4z'
        };

        let metaX = x + padX;
        metaItems.forEach((mi, idx) => {
            if (idx > 0) {
                // 分隔符
                parts.push(`<circle cx="${metaX + 6}" cy="${metaY}" r="1.2" fill="#CBD5E1"/>`);
                metaX += 14;
            }
            // 图标
            const path = metaIconPaths[mi.icon] || metaIconPaths.hash;
            parts.push(`<g transform="translate(${metaX}, ${metaY - 7}) scale(${(13 / 24).toFixed(3)})">
                <path d="${path}" fill="${theme.primary}" fill-opacity="0.8"/>
            </g>`);
            metaX += 18;
            // 文本
            parts.push(`<text x="${metaX}" y="${metaY + 3}" font-size="11.5" font-weight="500" fill="#64748B">${_acEscapeXml(mi.text)}</text>`);
            metaX += mi.text.length * 6.5;  // 粗略估算
        });
    });

    parts.push(`</g>`);

    // ==================== 右侧图例 ====================
    const lx = legendX;
    const ly = graphAreaY;
    const lw = LEGEND_W - 16;
    const lh = graphAreaH;

    // 图例主卡片
    parts.push(`<rect x="${lx}" y="${ly}" width="${lw}" height="${lh}" rx="18" fill="#FFFFFF" stroke="rgba(15,23,42,0.06)" stroke-width="1"/>`);
    // 顶部彩色装饰
    parts.push(`<rect x="${lx + 16}" y="${ly}" width="${lw - 32}" height="3" rx="1.5" fill="url(#ac-brand)"/>`);

    let curY = ly + 26;

    // --- 节点类型 ---
    parts.push(`<text x="${lx + 24}" y="${curY}" font-size="10.5" font-weight="800" fill="#64748B" letter-spacing="1.5px">NODE TYPES · 节点类型</text>`);
    curY += 22;
    const typeSummary = [
        { key: 'target', count: targetNodes.length, text: '目标' },
        { key: 'action-success', count: actionNodes.filter(a => { const m = a.metadata || {}; return Array.isArray(m.findings) && m.findings.length > 0 && m.status !== 'failed_insight'; }).length, text: '行动（有发现）' },
        { key: 'action-neutral', count: actionNodes.filter(a => { const m = a.metadata || {}; const f = Array.isArray(m.findings) ? m.findings : []; return f.length === 0 || m.status === 'failed_insight'; }).length, text: '行动（其他）' },
        { key: 'vuln-critical', count: criticalCount, text: '严重漏洞' },
        { key: 'vuln-high', count: highCount, text: '高风险漏洞' },
        { key: 'vuln-med', count: medCount, text: '中风险漏洞' },
        { key: 'vuln-low', count: lowCount, text: '低风险漏洞' }
    ];
    typeSummary.forEach(item => {
        const t = typeTheme[item.key];
        if (item.count === 0) return;  // 不显示零计数项
        // 图标方块
        parts.push(`<rect x="${lx + 24}" y="${curY - 10}" width="14" height="14" rx="4" fill="${t.primary}"/>`);
        // 标签文本
        parts.push(`<text x="${lx + 46}" y="${curY + 1}" font-size="12.5" font-weight="500" fill="#334155">${_acEscapeXml(item.text)}</text>`);
        // 计数
        parts.push(`<text x="${lx + lw - 24}" y="${curY + 1}" font-size="12.5" font-weight="700" fill="#0F172A" text-anchor="end">${item.count}</text>`);
        curY += 22;
    });
    curY += 10;

    // --- 连线含义 ---
    parts.push(`<text x="${lx + 24}" y="${curY}" font-size="10.5" font-weight="800" fill="#64748B" letter-spacing="1.5px">CONNECTIONS · 连线含义</text>`);
    curY += 22;
    const lineItems = [
        { label: '行动发现漏洞', color: '#4F46E5', dash: '' },
        { label: '使能 / 促成关系', color: '#E11D48', dash: '' },
        { label: '逻辑顺序', color: '#64748B', dash: '' },
        { label: '目标定位', color: '#4F46E5', dash: '6,3' }
    ];
    lineItems.forEach(l => {
        const dashAttr = l.dash ? `stroke-dasharray="${l.dash}"` : '';
        parts.push(`<line x1="${lx + 24}" y1="${curY - 3}" x2="${lx + 62}" y2="${curY - 3}" stroke="${l.color}" stroke-width="2.4" stroke-linecap="round" ${dashAttr}/>`);
        parts.push(`<polygon points="${lx + 62},${curY - 6} ${lx + 68},${curY - 3} ${lx + 62},${curY}" fill="${l.color}"/>`);
        parts.push(`<text x="${lx + 78}" y="${curY + 1}" font-size="12.5" font-weight="500" fill="#334155">${_acEscapeXml(l.label)}</text>`);
        curY += 24;
    });
    curY += 10;

    // --- 风险等级条 ---
    parts.push(`<text x="${lx + 24}" y="${curY}" font-size="10.5" font-weight="800" fill="#64748B" letter-spacing="1.5px">RISK LEVELS · 风险等级</text>`);
    curY += 22;
    const riskBar = [
        { label: '严重', range: '80-100', color: '#E11D48' },
        { label: '高', range: '60-79', color: '#EA580C' },
        { label: '中', range: '40-59', color: '#CA8A04' },
        { label: '低', range: '0-39', color: '#0D9488' }
    ];
    riskBar.forEach(r => {
        // 风险等级胶囊
        parts.push(`<rect x="${lx + 24}" y="${curY - 10}" width="46" height="18" rx="9" fill="${r.color}"/>`);
        parts.push(`<text x="${lx + 47}" y="${curY + 2}" text-anchor="middle" font-size="10.5" font-weight="700" fill="#FFFFFF" letter-spacing="0.3px">${_acEscapeXml(r.label)}</text>`);
        // 分数范围
        parts.push(`<text x="${lx + 80}" y="${curY + 1}" font-size="12" font-weight="500" fill="#64748B">分数 ${_acEscapeXml(r.range)}</text>`);
        curY += 26;
    });

    // ==================== 底部信息栏 ====================
    const fY = OUTER_PAD + contentH - FOOTER_H;
    // 分隔线
    parts.push(`<line x1="${OUTER_PAD + 40}" y1="${fY + 16}" x2="${OUTER_PAD + contentW - 40}" y2="${fY + 16}" stroke="rgba(15,23,42,0.06)" stroke-width="1"/>`);
    // 左侧品牌
    parts.push(`<circle cx="${OUTER_PAD + 44}" cy="${fY + 34}" r="5" fill="url(#ac-brand)"/>`);
    parts.push(`<text x="${OUTER_PAD + 56}" y="${fY + 38}" font-size="11.5" font-weight="600" fill="#64748B">CyberStrikeAI <tspan fill="#94A3B8" font-weight="500">· Attack Chain Visualization Report</tspan></text>`);
    // 右侧时间戳
    parts.push(`<text x="${OUTER_PAD + contentW - 40}" y="${fY + 38}" font-size="11.5" font-weight="500" fill="#94A3B8" text-anchor="end">${_acEscapeXml(ts)}</text>`);

    parts.push(`</svg>`);
    return parts.join('\n');
}

// 下载文本文件
function _acDownloadBlob(blob, filename) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(() => URL.revokeObjectURL(url), 150);
}

// 基于 SVG 字符串生成高清 PNG
function _acSvgToPng(svgString, scale) {
    return new Promise((resolve, reject) => {
        try {
            // 读取 SVG 尺寸
            const m = svgString.match(/<svg[^>]*width="(\d+(?:\.\d+)?)"[^>]*height="(\d+(?:\.\d+)?)"/i);
            const w = m ? parseFloat(m[1]) : 1600;
            const h = m ? parseFloat(m[2]) : 900;
            const s = scale || Math.min(2.5, Math.max(1.5, 2000 / Math.max(w, h)));

            const blob = new Blob([svgString], { type: 'image/svg+xml;charset=utf-8' });
            const url = URL.createObjectURL(blob);
            const img = new Image();
            img.onload = function () {
                try {
                    const canvas = document.createElement('canvas');
                    canvas.width = Math.round(w * s);
                    canvas.height = Math.round(h * s);
                    const ctx = canvas.getContext('2d');
                    ctx.imageSmoothingEnabled = true;
                    ctx.imageSmoothingQuality = 'high';
                    ctx.fillStyle = '#ffffff';
                    ctx.fillRect(0, 0, canvas.width, canvas.height);
                    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
                    URL.revokeObjectURL(url);
                    canvas.toBlob(pngBlob => {
                        if (!pngBlob) reject(new Error('PNG 生成失败'));
                        else resolve(pngBlob);
                    }, 'image/png', 0.95);
                } catch (err) {
                    URL.revokeObjectURL(url);
                    reject(err);
                }
            };
            img.onerror = function (e) {
                URL.revokeObjectURL(url);
                reject(new Error('SVG 加载失败'));
            };
            img.src = url;
        } catch (e) {
            reject(e);
        }
    });
}

// 导出攻击链（美化版）
function exportAttackChain(format) {
    if (!attackChainCytoscape) {
        alert(typeof window.t === 'function' ? window.t('chat.pleaseLoadAttackChainFirst', {}, '请先加载攻击链') : '请先加载攻击链');
        return;
    }

    // 延时确保渲染完成
    setTimeout(() => {
        try {
            const svgString = _acBuildSvgString();
            const convId = currentAttackChainConversationId || 'export';
            const tsName = Date.now();

            if (format === 'svg') {
                const blob = new Blob([svgString], { type: 'image/svg+xml;charset=utf-8' });
                _acDownloadBlob(blob, `attack-chain-${convId}-${tsName}.svg`);
            } else if (format === 'png') {
                _acSvgToPng(svgString, 2)
                    .then(pngBlob => _acDownloadBlob(pngBlob, `attack-chain-${convId}-${tsName}.png`))
                    .catch(err => {
                        console.error('导出 PNG 失败，回退到 Cytoscape 原生导出:', err);
                        // 回退方案：使用 Cytoscape 自带导出
                        try {
                            const p = attackChainCytoscape.png({ output: 'blob', bg: '#ffffff', full: true, scale: 2 });
                            if (p && typeof p.then === 'function') {
                                p.then(b => _acDownloadBlob(b, `attack-chain-${convId}-${tsName}.png`))
                                    .catch(e => alert('导出 PNG 失败: ' + (e && e.message || e)));
                            } else if (p) {
                                _acDownloadBlob(p, `attack-chain-${convId}-${tsName}.png`);
                            } else {
                                alert('导出 PNG 失败');
                            }
                        } catch (e2) {
                            alert('导出 PNG 失败: ' + (e2 && e2.message || e2));
                        }
                    });
            } else {
                alert('不支持的导出格式: ' + format);
            }
        } catch (error) {
            console.error('导出失败:', error);
            alert('导出失败: ' + (error && error.message || '未知错误'));
        }
    }, 80);
}

// ============================================
// 对话批量管理功能
// ============================================

let contextMenuConversationId = null;
let contextMenuConversationTitle = '';
let conversationsListLoadSeq = 0; // 对话列表加载序号，避免并发请求导致重复渲染
let conversationsListNavigateGen = 0; // 用户主动翻页代数，防止后台刷新覆盖翻页结果
const CONVERSATIONS_PAGE_SIZE_KEY = 'cyberstrike.conversations_page_size';
const CONVERSATIONS_SORT_KEY = 'cyberstrike.conversations_sort_by';
const CONVERSATIONS_PROJECT_FILTER_KEY = 'cyberstrike.conversations_project_filter';
const CONVERSATION_PROJECT_FILTER_NONE = '__none__';
const CONVERSATION_PROJECT_FILTER_SELECT_ID = 'conversation-project-filter';
const CONVERSATION_PROJECT_FILTER_CARET = '<svg class="conversation-project-filter-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const BATCH_PROJECT_FILTER_SELECT_ID = 'batch-project-filter';
const projectFilterCustomSelectRegistry = {};
let projectFilterCustomSelectDocBound = false;

function projectFilterT(key, fallback) {
    if (typeof window.t === 'function') {
        const value = window.t(key);
        if (value && value !== key) return value;
    }
    return fallback;
}

function closeProjectFilterCustomSelect(selectId) {
    const reg = projectFilterCustomSelectRegistry[selectId];
    if (!reg || !reg.wrapper) return;
    reg.wrapper.classList.remove('open');
    if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    if (reg.filterSearchTimer) {
        clearTimeout(reg.filterSearchTimer);
        reg.filterSearchTimer = null;
    }
    reg.filterSearchSeq = (reg.filterSearchSeq || 0) + 1;
    if (reg.searchInput) reg.searchInput.value = '';
}

function closeAllProjectFilterCustomSelects() {
    Object.keys(projectFilterCustomSelectRegistry).forEach(closeProjectFilterCustomSelect);
}

function ensureProjectFilterSearchUi(reg) {
    if (reg.searchInput && reg.optionsList) return;
    const { dropdown } = reg;
    dropdown.innerHTML = '';

    const searchWrap = document.createElement('div');
    searchWrap.className = 'conversation-project-filter-search';
    const searchInput = document.createElement('input');
    searchInput.type = 'search';
    searchInput.className = 'conversation-project-filter-search-input';
    searchInput.setAttribute('autocomplete', 'off');
    searchInput.setAttribute('data-i18n', 'chat.filterProjectSearch');
    searchInput.setAttribute('data-i18n-attr', 'placeholder');
    searchInput.placeholder = projectFilterT('chat.filterProjectSearch', '搜索项目…');
    searchWrap.appendChild(searchInput);
    dropdown.appendChild(searchWrap);
    reg.searchInput = searchInput;

    const optionsList = document.createElement('div');
    optionsList.className = 'conversation-project-filter-options';
    dropdown.appendChild(optionsList);
    reg.optionsList = optionsList;
    reg.filterSearchSeq = 0;
    reg.filterSearchTimer = null;

    searchInput.addEventListener('input', () => loadProjectFilterLocalOptions(reg.select.id));
    searchInput.addEventListener('click', (e) => e.stopPropagation());
    searchInput.addEventListener('keydown', (e) => {
        e.stopPropagation();
        if (e.key === 'Escape') closeProjectFilterCustomSelect(reg.select.id);
    });
}

function createProjectFilterOptionButton(value, label, selectedValue) {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'conversation-project-filter-option';
    item.setAttribute('role', 'option');
    item.setAttribute('data-value', value);
    item.title = label;
    if (value === selectedValue) {
        item.classList.add('is-selected');
        item.setAttribute('aria-selected', 'true');
    } else {
        item.setAttribute('aria-selected', 'false');
    }
    const check = document.createElement('span');
    check.className = 'conversation-project-filter-check';
    check.setAttribute('aria-hidden', 'true');
    check.textContent = '✓';
    const labelEl = document.createElement('span');
    labelEl.className = 'conversation-project-filter-option-label';
    labelEl.textContent = label;
    labelEl.title = label;
    item.appendChild(check);
    item.appendChild(labelEl);
    return item;
}

function appendProjectFilterStatusMessage(optionsList, className, text) {
    const el = document.createElement('div');
    el.className = className;
    el.textContent = text;
    optionsList.appendChild(el);
    return el;
}

function renderProjectFilterPinnedOptions(reg) {
    const { select, optionsList } = reg;
    optionsList.innerHTML = '';
    Array.prototype.forEach.call(select.options, (opt) => {
        if (opt.value === '' || opt.value === CONVERSATION_PROJECT_FILTER_NONE) {
            optionsList.appendChild(createProjectFilterOptionButton(opt.value, opt.textContent || '', select.value));
        }
    });
}

function ensureNativeProjectFilterOption(select, projectId, label) {
    if (!projectId || projectId === CONVERSATION_PROJECT_FILTER_NONE) return;
    if (Array.prototype.some.call(select.options, (opt) => opt.value === projectId)) return;
    const opt = document.createElement('option');
    opt.value = projectId;
    opt.textContent = label || projectId;
    select.appendChild(opt);
}

async function loadProjectFilterLocalOptions(selectId) {
    const reg = projectFilterCustomSelectRegistry[selectId];
    if (!reg || !reg.optionsList) return;
    const query = (reg.searchInput?.value || '').trim();
    const seq = ++reg.filterSearchSeq;

    const needsFetch = typeof window.isProjectsCacheReady === 'function' && !window.isProjectsCacheReady();
    let loadingEl = null;
    if (needsFetch) {
        renderProjectFilterPinnedOptions(reg);
        loadingEl = appendProjectFilterStatusMessage(
            reg.optionsList,
            'conversation-project-filter-status',
            projectFilterT('common.loading', '加载中…')
        );
    }

    try {
        const ensureLoaded = typeof window.ensureProjectsLoaded === 'function'
            ? window.ensureProjectsLoaded
            : null;
        const filterLocal = typeof window.filterActiveProjectsLocal === 'function'
            ? window.filterActiveProjectsLocal
            : null;
        if (!ensureLoaded || !filterLocal) throw new Error('projects cache unavailable');

        const all = await ensureLoaded();
        if (seq !== reg.filterSearchSeq) return;

        renderProjectFilterPinnedOptions(reg);
        const selected = reg.select.value;
        const pinnedValues = new Set(['', CONVERSATION_PROJECT_FILTER_NONE]);
        const projects = filterLocal(all, query);
        projects.forEach((p) => {
            if (pinnedValues.has(p.id)) return;
            reg.optionsList.appendChild(
                createProjectFilterOptionButton(p.id, p.name || p.id, selected)
            );
        });

        if (query && projects.length === 0) {
            appendProjectFilterStatusMessage(
                reg.optionsList,
                'conversation-project-filter-empty',
                projectFilterT('chat.filterProjectSearchEmpty', '没有匹配的项目')
            );
        }
    } catch (e) {
        if (seq !== reg.filterSearchSeq) return;
        renderProjectFilterPinnedOptions(reg);
        appendProjectFilterStatusMessage(
            reg.optionsList,
            'conversation-project-filter-empty',
            projectFilterT('chat.filterProjectSearchFailed', '加载项目失败，请重试')
        );
    } finally {
        if (loadingEl && loadingEl.parentNode) loadingEl.remove();
    }
}

function syncProjectFilterCustomSelect(selectId) {
    const reg = projectFilterCustomSelectRegistry[selectId];
    if (!reg) return;
    ensureProjectFilterSearchUi(reg);
    const { select, trigger } = reg;
    const valueSpan = trigger.querySelector('.conversation-project-filter-value');
    const selectedOpt = select.options[select.selectedIndex];
    const selectedText = selectedOpt ? (selectedOpt.textContent || '') : '';
    if (valueSpan) {
        valueSpan.textContent = selectedText;
        valueSpan.title = selectedText;
    }
}

function ensureSimpleCustomSelectOptionsUi(reg) {
    if (reg.optionsList) return;
    reg.dropdown.innerHTML = '';
    const optionsList = document.createElement('div');
    optionsList.className = 'conversation-project-filter-options';
    reg.dropdown.appendChild(optionsList);
    reg.optionsList = optionsList;
}

function renderSimpleCustomSelectOptions(reg) {
    ensureSimpleCustomSelectOptionsUi(reg);
    const { select, optionsList } = reg;
    optionsList.innerHTML = '';
    Array.prototype.forEach.call(select.options, (opt) => {
        optionsList.appendChild(createProjectFilterOptionButton(opt.value, opt.textContent || '', select.value));
    });
}

function syncSimpleCustomSelect(selectId) {
    const reg = projectFilterCustomSelectRegistry[selectId];
    if (!reg) return;
    const { select, trigger } = reg;
    const valueSpan = trigger.querySelector('.conversation-project-filter-value');
    const selectedOpt = select.options[select.selectedIndex];
    const selectedText = selectedOpt ? (selectedOpt.textContent || '') : '';
    if (valueSpan) {
        valueSpan.textContent = selectedText;
        valueSpan.title = selectedText;
    }
}

function initSimpleCustomSelect(selectId) {
    const select = document.getElementById(selectId);
    if (!select) return;
    if (select.dataset.projectCustomSelect === '1') {
        syncSimpleCustomSelect(selectId);
        return;
    }
    select.dataset.projectCustomSelect = '1';
    select.classList.add('conversation-project-filter-native');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'conversation-project-filter-ui';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'conversation-project-filter-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'conversation-project-filter-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', CONVERSATION_PROJECT_FILTER_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'conversation-project-filter-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    projectFilterCustomSelectRegistry[selectId] = { wrapper, trigger, dropdown, select };

    trigger.addEventListener('click', (e) => {
        e.stopPropagation();
        const open = wrapper.classList.contains('open');
        closeAllProjectFilterCustomSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
            renderSimpleCustomSelectOptions(projectFilterCustomSelectRegistry[selectId]);
        }
    });

    dropdown.addEventListener('click', (e) => {
        const opt = e.target.closest('.conversation-project-filter-option');
        if (!opt) return;
        e.stopPropagation();
        const val = opt.getAttribute('data-value');
        if (val === null) return;
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        closeProjectFilterCustomSelect(selectId);
        syncSimpleCustomSelect(selectId);
    });

    if (!projectFilterCustomSelectDocBound) {
        projectFilterCustomSelectDocBound = true;
        document.addEventListener('click', closeAllProjectFilterCustomSelects);
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') closeAllProjectFilterCustomSelects();
        });
    }
    syncSimpleCustomSelect(selectId);
}

function initProjectFilterCustomSelect(selectId) {
    const select = document.getElementById(selectId);
    if (!select) return;
    if (select.dataset.projectCustomSelect === '1') {
        syncProjectFilterCustomSelect(selectId);
        return;
    }
    select.dataset.projectCustomSelect = '1';
    select.classList.add('conversation-project-filter-native');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'conversation-project-filter-ui';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'conversation-project-filter-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'conversation-project-filter-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', CONVERSATION_PROJECT_FILTER_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'conversation-project-filter-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    projectFilterCustomSelectRegistry[selectId] = { wrapper, trigger, dropdown, select };

    trigger.addEventListener('click', (e) => {
        e.stopPropagation();
        const open = wrapper.classList.contains('open');
        closeAllProjectFilterCustomSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
            ensureProjectFilterSearchUi(projectFilterCustomSelectRegistry[selectId]);
            const reg = projectFilterCustomSelectRegistry[selectId];
            if (reg?.searchInput) {
                reg.searchInput.value = '';
                loadProjectFilterLocalOptions(selectId);
                requestAnimationFrame(() => reg.searchInput.focus());
            }
        }
    });

    dropdown.addEventListener('click', (e) => {
        const opt = e.target.closest('.conversation-project-filter-option');
        if (!opt) return;
        e.stopPropagation();
        const val = opt.getAttribute('data-value');
        if (val === null) return;
        const label = opt.querySelector('.conversation-project-filter-option-label')?.textContent || val;
        ensureNativeProjectFilterOption(select, val, label);
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        closeProjectFilterCustomSelect(selectId);
        syncProjectFilterCustomSelect(selectId);
    });

    if (!projectFilterCustomSelectDocBound) {
        projectFilterCustomSelectDocBound = true;
        document.addEventListener('click', closeAllProjectFilterCustomSelects);
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') closeAllProjectFilterCustomSelects();
        });
    }
    syncProjectFilterCustomSelect(selectId);
}

function syncConversationProjectCustomSelect() {
    syncProjectFilterCustomSelect(CONVERSATION_PROJECT_FILTER_SELECT_ID);
}

function initConversationProjectCustomSelect() {
    initProjectFilterCustomSelect(CONVERSATION_PROJECT_FILTER_SELECT_ID);
}

function getConversationProjectFilter() {
    try {
        return localStorage.getItem(CONVERSATIONS_PROJECT_FILTER_KEY) || '';
    } catch (e) {
        return '';
    }
}

function setConversationProjectFilter(projectId) {
    const value = (projectId || '').trim();
    try {
        if (value) localStorage.setItem(CONVERSATIONS_PROJECT_FILTER_KEY, value);
        else localStorage.removeItem(CONVERSATIONS_PROJECT_FILTER_KEY);
    } catch (e) { /* ignore */ }
    const sel = document.getElementById('conversation-project-filter');
    if (sel && sel.value !== value) sel.value = value;
    syncConversationProjectCustomSelect();
    updateConversationSidebarFilterUI();
}

function appendProjectFilterPinnedNativeOptions(sel) {
    const tFn = typeof window.t === 'function' ? window.t.bind(window) : null;
    const allLabel = tFn ? tFn('chat.filterAllProjects') : '全部项目';
    const unboundLabel = tFn ? tFn('chat.filterUnboundProjects') : '未绑定项目';
    sel.innerHTML = '';
    const allOpt = document.createElement('option');
    allOpt.value = '';
    allOpt.textContent = allLabel;
    allOpt.setAttribute('data-i18n', 'chat.filterAllProjects');
    sel.appendChild(allOpt);
    const unboundOpt = document.createElement('option');
    unboundOpt.value = CONVERSATION_PROJECT_FILTER_NONE;
    unboundOpt.textContent = unboundLabel;
    unboundOpt.setAttribute('data-i18n', 'chat.filterUnboundProjects');
    sel.appendChild(unboundOpt);
}

async function resolveProjectFilterSelection(projectId) {
    const saved = (projectId || '').trim();
    if (!saved || saved === CONVERSATION_PROJECT_FILTER_NONE) return saved;
    const fetchSummary = typeof window.fetchProjectSummary === 'function'
        ? window.fetchProjectSummary
        : null;
    if (!fetchSummary) return saved;
    const project = await fetchSummary(saved);
    if (!project || !project.id || project.status === 'archived') return '';
    return project.id;
}

async function appendSelectedProjectFilterOption(sel, projectId) {
    const id = (projectId || '').trim();
    if (!id || id === CONVERSATION_PROJECT_FILTER_NONE) return;
    if (Array.prototype.some.call(sel.options, (opt) => opt.value === id)) return;
    const fetchSummary = typeof window.fetchProjectSummary === 'function'
        ? window.fetchProjectSummary
        : null;
    const project = fetchSummary ? await fetchSummary(id) : null;
    const label = (project && (project.name || project.id)) || (window.projectNameById && window.projectNameById[id]) || id;
    const opt = document.createElement('option');
    opt.value = id;
    opt.textContent = label;
    sel.appendChild(opt);
}

async function refreshConversationProjectFilter() {
    const sel = document.getElementById('conversation-project-filter');
    if (!sel) return;
    const saved = getConversationProjectFilter();
    appendProjectFilterPinnedNativeOptions(sel);
    const normalized = await resolveProjectFilterSelection(saved);
    if (normalized && normalized !== CONVERSATION_PROJECT_FILTER_NONE) {
        await appendSelectedProjectFilterOption(sel, normalized);
    }
    if (normalized !== saved) setConversationProjectFilter(normalized);
    sel.value = normalized;
    syncConversationProjectCustomSelect();
    updateConversationSidebarFilterUI();
}

function onConversationProjectFilterChange(projectId) {
    setConversationProjectFilter(projectId || '');
    commitConversationsPage(1, { bumpNavigateGen: true });
    loadConversations(conversationsSearchQuery);
}

function updateConversationSidebarFilterUI() {
    const titleEl = document.querySelector('.recent-conversations-section .section-title');
    const filter = getConversationProjectFilter();
    const hasSearch = !!(conversationsSearchQuery && conversationsSearchQuery.trim());
    if (!titleEl) return;
    const tFn = typeof window.t === 'function' ? window.t.bind(window) : null;
    if (filter && filter !== CONVERSATION_PROJECT_FILTER_NONE) {
        const name = (window.projectNameById && window.projectNameById[filter]) || filter;
        const fullTitle = tFn ? tFn('chat.projectConversationsTitle', { name }) : `${name} · 对话`;
        titleEl.textContent = fullTitle;
        titleEl.title = fullTitle;
        titleEl.classList.add('section-title--filtered');
        titleEl.removeAttribute('data-i18n');
    } else if (filter === CONVERSATION_PROJECT_FILTER_NONE) {
        const fullTitle = tFn ? tFn('chat.unboundConversationsTitle') : '未绑定项目';
        titleEl.textContent = fullTitle;
        titleEl.title = fullTitle;
        titleEl.classList.add('section-title--filtered');
        titleEl.setAttribute('data-i18n', 'chat.unboundConversationsTitle');
    } else {
        titleEl.classList.remove('section-title--filtered');
        titleEl.removeAttribute('title');
        titleEl.setAttribute('data-i18n', 'chat.recentConversations');
        if (tFn) titleEl.textContent = tFn('chat.recentConversations');
    }
}

window.onConversationProjectBindingChanged = function onConversationProjectBindingChanged() {
    loadConversations(conversationsSearchQuery);
};

function getConversationSortBy() {
    try {
        const saved = localStorage.getItem(CONVERSATIONS_SORT_KEY);
        if (saved === 'created_at' || saved === 'updated_at') return saved;
    } catch (e) { /* ignore */ }
    return 'updated_at';
}

let conversationSortBy = getConversationSortBy();

function getConversationSortTime(conv) {
    const field = conversationSortBy === 'created_at' ? 'createdAt' : 'updatedAt';
    const raw = conv && conv[field];
    if (!raw) return new Date(0);
    const date = new Date(raw);
    return isNaN(date.getTime()) ? new Date(0) : date;
}

function updateConversationSortMenuUI() {
    const menu = document.getElementById('conversation-sort-menu');
    const btn = document.getElementById('conversation-sort-btn');
    if (!menu) return;
    menu.querySelectorAll('.conversation-sort-option').forEach((option) => {
        const selected = option.dataset.sort === conversationSortBy;
        option.classList.toggle('is-selected', selected);
        option.setAttribute('aria-checked', selected ? 'true' : 'false');
    });
    if (btn) {
        btn.setAttribute('aria-expanded', menu.hidden ? 'false' : 'true');
    }
}

function closeConversationSortMenu() {
    const menu = document.getElementById('conversation-sort-menu');
    const btn = document.getElementById('conversation-sort-btn');
    if (menu) menu.hidden = true;
    if (btn) btn.setAttribute('aria-expanded', 'false');
}

function toggleConversationSortMenu(event) {
    if (event) {
        event.preventDefault();
        event.stopPropagation();
    }
    const menu = document.getElementById('conversation-sort-menu');
    const btn = document.getElementById('conversation-sort-btn');
    if (!menu || !btn) return;
    const willOpen = menu.hidden;
    closeConversationSortMenu();
    if (willOpen) {
        menu.hidden = false;
        btn.setAttribute('aria-expanded', 'true');
        updateConversationSortMenuUI();
    }
}

function setConversationSortBy(sortBy) {
    const next = sortBy === 'created_at' ? 'created_at' : 'updated_at';
    if (next === conversationSortBy) {
        closeConversationSortMenu();
        return;
    }
    conversationSortBy = next;
    try {
        localStorage.setItem(CONVERSATIONS_SORT_KEY, next);
    } catch (e) { /* ignore */ }
    updateConversationSortMenuUI();
    closeConversationSortMenu();
    commitConversationsPage(1, { bumpNavigateGen: true });
    loadConversations(conversationsSearchQuery);
}

if (!window.__conversationSortMenuBound) {
    window.__conversationSortMenuBound = true;
    document.addEventListener('click', (event) => {
        const dropdown = document.getElementById('conversation-sort-dropdown');
        if (!dropdown || dropdown.contains(event.target)) return;
        closeConversationSortMenu();
    });
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') closeConversationSortMenu();
    });
}

window.toggleConversationSortMenu = toggleConversationSortMenu;
window.setConversationSortBy = setConversationSortBy;
window.closeConversationSortMenu = closeConversationSortMenu;

function getConversationsPageSize() {
    try {
        const saved = parseInt(localStorage.getItem(CONVERSATIONS_PAGE_SIZE_KEY), 10);
        if ([20, 50, 100].includes(saved)) return saved;
    } catch (e) { /* ignore */ }
    return 50;
}

let conversationsPagination = {
    page: 1,
    pageSize: getConversationsPageSize(),
    total: 0,
    visibleCount: 0,
};
let conversationsSearchQuery = '';
let conversationsPaginationEventsBound = false;

function getConversationsTotalPages() {
    const { total, pageSize } = conversationsPagination;
    return Math.max(1, Math.ceil((total || 0) / pageSize) || 1);
}

/**
 * 分页状态约定：
 * - conversationsPagination.page 仅在此处（用户操作 / reconcile 钳制 / clamp）写入
 * - loadConversations 只读页码，用 intentPage 或当前 page 计算 offset
 * - isStaleConversationListLoad 丢弃页码或 navigateGen 已变的在途请求
 */
function commitConversationsPage(page, { bumpNavigateGen = false } = {}) {
    const next = Math.max(1, parseInt(page, 10) || 1);
    if (bumpNavigateGen) {
        conversationsListNavigateGen += 1;
    }
    conversationsPagination.page = next;
    return next;
}

function isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage) {
    if (loadSeq !== conversationsListLoadSeq) return true;
    // 后台刷新期间用户已翻页（含 2→1、1→2），丢弃过期结果
    if (intentPage == null && navigateGenAtStart !== conversationsListNavigateGen) return true;
    // 用户主动翻页后，丢弃目标页已变化的请求
    if (intentPage != null && intentPage !== conversationsPagination.page) return true;
    // 后台刷新完成时页码已变（如 reconcile 钳制），丢弃过期结果
    if (intentPage == null && activePage != null && activePage !== conversationsPagination.page) return true;
    return false;
}

function reconcileConversationsPageAfterTotal(activePage, intentPage, parsed, pageSize, offset, resolvedTotal) {
    let total = resolvedTotal;
    const totalPages = () => Math.max(1, Math.ceil((total || 0) / pageSize) || 1);

    if (activePage <= totalPages()) {
        return { ok: true, total };
    }

    const serverTotal = parseListTotalValue(parsed.total, parsed.items.length);
    const hasPageData = parsed.items.length > 0;
    const knownTotal = conversationsPagination.total || 0;
    // 用户主动翻页且服务端确有该页数据时，不信过期/偏低的 total（避免 2>1 被钳回第 1 页）
    if (intentPage != null && (hasPageData || serverTotal > offset || total > offset || knownTotal > offset)) {
        total = Math.max(total, serverTotal, knownTotal, offset + parsed.items.length);
        if (activePage <= totalPages()) {
            return { ok: true, total };
        }
    }

    const clampedPage = totalPages();
    commitConversationsPage(clampedPage);
    return { ok: false, total, clampedPage };
}

function clampConversationsPageToTotal() {
    const totalPages = getConversationsTotalPages();
    if (conversationsPagination.page > totalPages) {
        commitConversationsPage(totalPages);
        return true;
    }
    if (conversationsPagination.page < 1) {
        commitConversationsPage(1);
        return true;
    }
    return false;
}

let conversationsPaginationRenderLock = false;

function initConversationsPaginationEvents() {
    if (conversationsPaginationEventsBound) return;
    const el = document.getElementById('conversations-pagination');
    if (!el) return;
    conversationsPaginationEventsBound = true;
    el.addEventListener('click', (e) => {
        const btn = e.target.closest('[data-conv-page]');
        if (!btn || btn.disabled) return;
        e.preventDefault();
        const page = parseInt(btn.getAttribute('data-conv-page'), 10);
        if (Number.isFinite(page)) {
            goConversationsPage(page);
        }
    });
    el.addEventListener('change', (e) => {
        if (conversationsPaginationRenderLock) return;
        if (e.target && e.target.id === 'conversations-page-size-pagination') {
            changeConversationsPageSize();
        }
    });
}

function parseListTotalValue(raw, itemsLength) {
    if (typeof raw === 'number' && Number.isFinite(raw) && raw >= 0) return raw;
    if (raw != null && raw !== '') {
        const n = parseInt(String(raw), 10);
        if (Number.isFinite(n) && n >= 0) return n;
    }
    return itemsLength;
}

function parseListOffsetValue(raw) {
    if (typeof raw === 'number' && Number.isFinite(raw) && raw >= 0) return raw;
    if (raw != null && raw !== '') {
        const n = parseInt(String(raw), 10);
        if (Number.isFinite(n) && n >= 0) return n;
    }
    return 0;
}

function parseConversationsListResponse(data) {
    if (Array.isArray(data)) {
        return { items: data, total: data.length, limit: data.length, offset: 0, isLegacyArray: true };
    }
    const items = data.conversations || data.items || [];
    const arr = Array.isArray(items) ? items : [];
    return {
        items: arr,
        total: parseListTotalValue(data.total, arr.length),
        limit: parseListTotalValue(data.limit, arr.length) || arr.length,
        offset: parseListOffsetValue(data.offset),
        isLegacyArray: false,
    };
}

async function resolveConversationsListTotal(params, parsed, pageSize, offset) {
    const serverTotal = parsed.total;
    if (!parsed.isLegacyArray && typeof serverTotal === 'number' && Number.isFinite(serverTotal) && serverTotal >= 0) {
        return serverTotal;
    }
    if (!parsed.isLegacyArray && serverTotal > offset + parsed.items.length) {
        return serverTotal;
    }
    if (parsed.items.length < pageSize) {
        return Math.max(serverTotal, offset + parsed.items.length);
    }
    const probe = new URLSearchParams(params);
    probe.set('offset', String(offset + pageSize));
    probe.set('limit', '1');
    try {
        const res = await apiFetch(`/api/conversations?${probe}`);
        if (!res.ok) return Math.max(serverTotal, offset + parsed.items.length);
        const probeParsed = parseConversationsListResponse(await res.json());
        if (probeParsed.total > serverTotal) return probeParsed.total;
        if (probeParsed.items.length > 0) {
            return Math.max(serverTotal, offset + pageSize + 1);
        }
    } catch (e) { /* ignore */ }
    return Math.max(serverTotal, offset + parsed.items.length);
}

async function fetchAllConversations(searchQuery) {
    let all = [];
    const pageSize = 200;
    let offset = 0;
    let total = Infinity;
    const search = (searchQuery || '').trim();
    while (all.length < total) {
        const params = new URLSearchParams({ limit: String(pageSize), offset: String(offset) });
        if (search) params.set('search', search);
        const res = await apiFetch(`/api/conversations?${params}`);
        if (!res.ok) throw new Error('load conversations failed');
        const parsed = parseConversationsListResponse(await res.json());
        all = all.concat(parsed.items);
        total = parsed.total;
        if (!parsed.items.length) break;
        offset += parsed.items.length;
    }
    return all;
}

function getConversationListEmptyHtml() {
    const filter = getConversationProjectFilter();
    if (filter && filter !== CONVERSATION_PROJECT_FILTER_NONE) {
        return '<div class="conversations-list-empty" data-i18n="chat.noProjectConversations"></div>';
    }
    if (filter === CONVERSATION_PROJECT_FILTER_NONE) {
        return '<div class="conversations-list-empty" data-i18n="chat.noUnboundConversations"></div>';
    }
    return '<div class="conversations-list-empty" data-i18n="chat.noHistoryConversations"></div>';
}

function renderConversationsPagination(visibleCount) {
    const el = document.getElementById('conversations-pagination');
    if (!el) return;
    const { page, pageSize, total } = conversationsPagination;
    if (typeof visibleCount === 'number') {
        conversationsPagination.visibleCount = visibleCount;
    }

    if (!total) {
        el.innerHTML = '';
        el.hidden = true;
        return;
    }

    const totalPages = getConversationsTotalPages();
    const navDisabled = totalPages <= 1;
    const recentToggle = document.getElementById('recent-conversations-toggle');
    el.hidden = !recentToggle || recentToggle.getAttribute('aria-expanded') !== 'true';
    const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const end = Math.min(page * pageSize, total);
    const tFn = typeof window.t === 'function' ? window.t.bind(window) : null;
    const infoText = tFn
        ? tFn('chat.paginationRange', { start, end, total })
        : `${start}-${end}/${total}`;
    const pageText = tFn
        ? tFn('chat.paginationPage', { page, total: totalPages })
        : `${page}/${totalPages}`;
    const perPageLabel = tFn ? tFn('chat.paginationPerPage') : 'Per page';
    const prevLabel = tFn ? tFn('chat.paginationPrev') : 'Prev';
    const nextLabel = tFn ? tFn('chat.paginationNext') : 'Next';
    const prevPage = page - 1;
    const nextPage = page + 1;
    conversationsPaginationRenderLock = true;
    try {
        el.innerHTML = `
        <div class="sidebar-list-pagination-inner sidebar-list-pagination-inner--compact">
            <span class="pagination-info">${escapeHtml(infoText)}</span>
            <div class="pagination-controls">
                <button type="button" class="btn-icon-pagination" data-conv-page="${prevPage}" ${page <= 1 || navDisabled ? 'disabled' : ''} title="${escapeHtml(prevLabel)}" aria-label="${escapeHtml(prevLabel)}">‹</button>
                <span class="pagination-page">${escapeHtml(pageText)}</span>
                <button type="button" class="btn-icon-pagination" data-conv-page="${nextPage}" ${page >= totalPages || navDisabled ? 'disabled' : ''} title="${escapeHtml(nextLabel)}" aria-label="${escapeHtml(nextLabel)}">›</button>
            </div>
            <label class="pagination-page-size">
                ${escapeHtml(perPageLabel)}
                <select id="conversations-page-size-pagination">
                    <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                    <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                    <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                </select>
            </label>
        </div>`;
    } finally {
        conversationsPaginationRenderLock = false;
    }
}

function goConversationsPage(page) {
    const requestedPage = Math.max(1, parseInt(page, 10) || 1);
    const scrollToTop = requestedPage !== conversationsPagination.page;
    commitConversationsPage(requestedPage, { bumpNavigateGen: true });
    loadConversations(conversationsSearchQuery, {
        refreshMeta: false,
        scrollToTop,
        intentPage: requestedPage,
    });
}

function changeConversationsPageSize() {
    const sel = document.getElementById('conversations-page-size-pagination');
    const newSize = sel ? parseInt(sel.value, 10) : 50;
    if (![20, 50, 100].includes(newSize)) return;
    // 重建 DOM 后浏览器可能异步触发 change，值未变时不应重置页码
    if (newSize === conversationsPagination.pageSize) return;
    try {
        localStorage.setItem(CONVERSATIONS_PAGE_SIZE_KEY, String(newSize));
    } catch (e) { /* ignore */ }
    conversationsPagination.pageSize = newSize;
    commitConversationsPage(1, { bumpNavigateGen: true });
    loadConversations(conversationsSearchQuery);
}

window.goConversationsPage = goConversationsPage;
window.changeConversationsPageSize = changeConversationsPageSize;

// 加载对话列表（支持置顶）
async function loadConversations(searchQuery = '', options = {}) {
    const refreshMeta = options.refreshMeta !== false;
    const scrollToTop = options.scrollToTop === true;
    const intentPage = Number.isFinite(options.intentPage) ? options.intentPage : null;
    const navigateGenAtStart = conversationsListNavigateGen;
    const loadSeq = ++conversationsListLoadSeq;
    try {
        conversationsSearchQuery = searchQuery || '';
        const pageSize = getConversationsPageSize();
        conversationsPagination.pageSize = pageSize;
        const activePage = intentPage != null ? intentPage : conversationsPagination.page;
        const offset = (activePage - 1) * pageSize;
        const convParams = new URLSearchParams({ limit: String(pageSize), offset: String(offset) });
        if (conversationSortBy === 'created_at') {
            convParams.set('sort_by', 'created_at');
        }
        const projectFilter = getConversationProjectFilter();
        if (projectFilter) {
            convParams.set('project_id', projectFilter);
        }
        if (searchQuery && searchQuery.trim()) {
            convParams.set('search', searchQuery.trim());
        }
        updateConversationSidebarFilterUI();
        const url = `/api/conversations?${convParams}`;
        const response = await apiFetch(url);
        if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;

        const listContainer = document.getElementById('conversations-list');
        if (!listContainer) {
            return;
        }

        // 保存滚动位置
        const sidebarContent = listContainer.closest('.sidebar-content');
        const savedScrollTop = sidebarContent ? sidebarContent.scrollTop : 0;

        const emptyStateHtml = getConversationListEmptyHtml();
        listContainer.innerHTML = '';

        // 如果响应不是200，显示空状态（友好处理，不显示错误）
        if (!response.ok) {
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            updateRecentConversationsCount(0);
            renderConversationsPagination(0);
            return;
        }

        const data = await response.json();
        if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;
        const parsed = parseConversationsListResponse(data);
        const resolvedTotal = await resolveConversationsListTotal(convParams, parsed, pageSize, offset);
        if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;
        conversationsPagination.total = resolvedTotal;
        updateRecentConversationsCount(resolvedTotal);

        const pageCheck = reconcileConversationsPageAfterTotal(
            activePage, intentPage, parsed, pageSize, offset, resolvedTotal
        );
        conversationsPagination.total = pageCheck.total;
        if (!pageCheck.ok) {
            if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;
            // 用户主动翻页被钳制时仍保留 intent，并 bump navigateGen 使在途后台刷新失效
            if (intentPage != null) {
                commitConversationsPage(pageCheck.clampedPage, { bumpNavigateGen: true });
            }
            loadConversations(searchQuery, {
                ...options,
                intentPage: pageCheck.clampedPage,
                scrollToTop: options.scrollToTop === true || activePage !== pageCheck.clampedPage,
            });
            return;
        }
        if (intentPage == null && clampConversationsPageToTotal()) {
            if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;
            loadConversations(searchQuery, options);
            return;
        }

        // 双重保险：后端或并发情况下若出现重复ID，前端按ID去重
        const uniqueConversations = [];
        const seenConversationIds = new Set();
        parsed.items.forEach(conv => {
            if (!conv || !conv.id || seenConversationIds.has(conv.id)) {
                return;
            }
            seenConversationIds.add(conv.id);
            uniqueConversations.push(conv);
        });

        if (uniqueConversations.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            renderConversationsPagination(0);
            return;
        }

        // 分离置顶和普通对话
        const pinnedConvs = [];
        const normalConvs = [];

        uniqueConversations.forEach(conv => {
            if (conv.pinned) {
                pinnedConvs.push(conv);
            } else {
                normalConvs.push(conv);
            }
        });

        // 按时间排序
        const sortByTime = (a, b) => getConversationSortTime(b) - getConversationSortTime(a);

        pinnedConvs.sort(sortByTime);
        normalConvs.sort(sortByTime);

        const now = new Date();
        const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
        const yesterdayStart = new Date(todayStart);
        yesterdayStart.setDate(todayStart.getDate() - 1);
        const sevenDaysCutoff = new Date(todayStart);
        sevenDaysCutoff.setDate(todayStart.getDate() - 7);

        const tFn = typeof window.t === 'function' ? window.t.bind(window) : null;
        const groupOrder = [
            { key: 'today', label: tFn ? tFn('chat.historyGroupToday') : '今天' },
            { key: 'yesterday', label: tFn ? tFn('chat.yesterday') : '昨天' },
            { key: 'last7Days', label: tFn ? tFn('chat.historyGroupLast7Days') : '过去七天' },
            { key: 'earlier', label: tFn ? tFn('chat.historyGroupEarlier') : '更早' },
        ];

        const groups = {
            today: [],
            yesterday: [],
            last7Days: [],
            earlier: [],
        };

        normalConvs.forEach(conv => {
            const dateObj = getConversationSortTime(conv);
            const validDate = dateObj.getTime() === 0 ? new Date() : dateObj;
            const groupKey = getConversationGroup(validDate, todayStart, sevenDaysCutoff, yesterdayStart);
            groups[groupKey].push({
                ...conv,
                _timeText: formatConversationTimestamp(validDate, todayStart, yesterdayStart),
            });
        });

        const fragment = document.createDocumentFragment();

        if (pinnedConvs.length > 0) {
            pinnedConvs.forEach(conv => {
                const dateObj = getConversationSortTime(conv);
                const validDate = dateObj.getTime() === 0 ? new Date() : dateObj;
                fragment.appendChild(createConversationListItemWithMenu({
                    ...conv,
                    _timeText: formatConversationTimestamp(validDate, todayStart, yesterdayStart),
                }, true));
            });
        }

        groupOrder.forEach(({ key, label }) => {
            const items = groups[key];
            if (!items || items.length === 0) {
                return;
            }
            const section = document.createElement('div');
            section.className = 'conversation-group';

            const title = document.createElement('div');
            title.className = 'conversation-group-title';
            title.textContent = label;
            section.appendChild(title);

            items.forEach(itemData => {
                section.appendChild(createConversationListItemWithMenu(itemData, false));
            });

            fragment.appendChild(section);
        });

        const visibleCount = pinnedConvs.length + Object.values(groups).reduce((n, arr) => n + (arr ? arr.length : 0), 0);
        conversationsPagination.visibleCount = visibleCount;

        if (fragment.children.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            renderConversationsPagination(0);
            return;
        }

        if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;
        listContainer.appendChild(fragment);
        updateActiveConversation();
        renderConversationsPagination(visibleCount);

        // 翻页时回到列表顶部；后台刷新保留滚动位置
        if (sidebarContent) {
            requestAnimationFrame(() => {
                if (!isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) {
                    sidebarContent.scrollTop = scrollToTop ? 0 : savedScrollTop;
                }
            });
        }
    } catch (error) {
        if (isStaleConversationListLoad(loadSeq, intentPage, navigateGenAtStart, activePage)) return;
        console.error('加载对话列表失败:', error);
        // 错误时显示空状态，而不是错误提示（更友好的用户体验）
        const listContainer = document.getElementById('conversations-list');
        if (listContainer) {
            listContainer.innerHTML = getConversationListEmptyHtml();
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            updateRecentConversationsCount(0);
            renderConversationsPagination(0);
        }
    }
}

// 创建带菜单的对话项
function createConversationListItemWithMenu(conversation, isPinned) {
    const item = document.createElement('div');
    item.className = 'conversation-item';
    item.dataset.conversationId = conversation.id;
    if (conversation.id === currentConversationId) {
        item.classList.add('active');
    }

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'conversation-content';

    const titleWrapper = document.createElement('div');
    titleWrapper.style.display = 'flex';
    titleWrapper.style.alignItems = 'center';
    titleWrapper.style.gap = '4px';

    const title = document.createElement('div');
    title.className = 'conversation-title';
    const titleText = conversation.title || '未命名对话';
    title.textContent = safeTruncateText(titleText, 60);
    title.title = titleText; // 设置完整标题以便悬停查看
    titleWrapper.appendChild(title);

    if (isPinned) {
        const pinIcon = document.createElement('span');
        pinIcon.className = 'conversation-item-pinned';
        pinIcon.innerHTML = '📌';
        pinIcon.title = '已置顶';
        titleWrapper.appendChild(pinIcon);
    }

    contentWrapper.appendChild(titleWrapper);

    const time = document.createElement('div');
    time.className = 'conversation-time';
    const dateObj = conversation.updatedAt ? new Date(conversation.updatedAt) : new Date();
    time.textContent = conversation._timeText || formatConversationTimestamp(dateObj);
    contentWrapper.appendChild(time);

    item.appendChild(contentWrapper);

    const menuBtn = document.createElement('button');
    menuBtn.className = 'conversation-item-menu';
    menuBtn.innerHTML = '⋯';
    menuBtn.onclick = (e) => openConversationContextMenuForId(e, conversation.id, conversation.title || '');
    item.appendChild(menuBtn);

    item.onclick = (e) => {
        e.preventDefault();
        e.stopPropagation();
        const targetConversationId = String(item.dataset.conversationId || '').trim();
        if (targetConversationId) loadConversation(targetConversationId);
    };

    return item;
}

function openConversationContextMenuForId(event, conversationId, conversationTitle = '') {
    event.stopPropagation();
    event.preventDefault();
    contextMenuConversationId = conversationId;
    contextMenuConversationTitle = conversationTitle;
    return showConversationContextMenu(event);
}

let downloadMarkdownSubmenuHideTimer = null;

function clearDownloadMarkdownSubmenuHideTimeout() {
    if (!downloadMarkdownSubmenuHideTimer) return;
    clearTimeout(downloadMarkdownSubmenuHideTimer);
    downloadMarkdownSubmenuHideTimer = null;
}

function hideDownloadMarkdownSubmenu() {
    clearDownloadMarkdownSubmenuHideTimeout();
    downloadMarkdownSubmenuHideTimer = setTimeout(() => {
        const submenu = document.getElementById('download-markdown-submenu');
        if (submenu) submenu.style.display = 'none';
        downloadMarkdownSubmenuHideTimer = null;
    }, 120);
}

function handleDownloadMarkdownSubmenuEnter() {
    clearDownloadMarkdownSubmenuHideTimeout();
    const submenu = document.getElementById('download-markdown-submenu');
    if (submenu) submenu.style.display = 'block';
}

function handleDownloadMarkdownSubmenuLeave(event) {
    const submenu = document.getElementById('download-markdown-submenu');
    if (submenu && event?.relatedTarget && submenu.contains(event.relatedTarget)) return;
    hideDownloadMarkdownSubmenu();
}

function updateConversationContextPinText(isPinned) {
    const pinMenuText = document.getElementById('pin-conversation-menu-text');
    if (!pinMenuText) return;
    if (typeof window.t === 'function') {
        pinMenuText.textContent = isPinned ? window.t('contextMenu.unpinConversation') : window.t('contextMenu.pinConversation');
    } else {
        pinMenuText.textContent = isPinned ? '取消置顶' : '置顶此对话';
    }
}

async function refreshConversationContextPinText(convId) {
    if (!convId) {
        updateConversationContextPinText(false);
        return;
    }
    try {
        const response = await apiFetch(`/api/conversations/${convId}`);
        if (!response.ok) return;
        const conv = await response.json();
        updateConversationContextPinText(!!conv.pinned);
    } catch (error) {
        console.error('获取对话置顶状态失败:', error);
    }
}

// 显示对话上下文菜单
async function showConversationContextMenu(event) {
    const menu = document.getElementById('conversation-context-menu');
    if (!menu) return;

    const downloadSubmenu = document.getElementById('download-markdown-submenu');
    if (downloadSubmenu) {
        downloadSubmenu.style.display = 'none';
    }
    // 清除所有定时器
    clearDownloadMarkdownSubmenuHideTimeout();

    const convId = contextMenuConversationId;
    updateConversationContextPinText(false);

    // 更新攻击链菜单项的启用状态
    const attackChainMenuItem = document.getElementById('attack-chain-menu-item');
    if (attackChainMenuItem) {
        if (convId) {
            const isRunning = typeof isConversationTaskRunning === 'function'
                ? isConversationTaskRunning(convId)
                : false;
            if (isRunning) {
                attackChainMenuItem.style.opacity = '0.5';
                attackChainMenuItem.style.cursor = 'not-allowed';
                attackChainMenuItem.onclick = null;
                attackChainMenuItem.title = '当前对话正在执行，请稍后再生成攻击链';
            } else {
                attackChainMenuItem.style.opacity = '1';
                attackChainMenuItem.style.cursor = 'pointer';
                attackChainMenuItem.onclick = showAttackChainFromContext;
                attackChainMenuItem.title = (typeof window.t === 'function' ? window.t('chat.viewAttackChainCurrentConv') : '查看当前对话的攻击链');
            }
        } else {
            attackChainMenuItem.style.opacity = '0.5';
            attackChainMenuItem.style.cursor = 'not-allowed';
            attackChainMenuItem.onclick = null;
            attackChainMenuItem.title = (typeof window.t === 'function' ? window.t('chat.viewAttackChainSelectConv') : '请选择一个对话以查看攻击链');
        }
    }

    // 先显示菜单，置顶状态随后异步刷新，避免接口慢时点击没有任何反馈。
    menu.style.display = 'block';
    menu.style.visibility = 'visible';
    menu.style.opacity = '1';

    // 强制重排以获取正确尺寸
    void menu.offsetHeight;

    // 计算菜单位置，确保不超出屏幕
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    const submenuWidth = 0;

    let left = event.clientX;
    let top = event.clientY;

    // 如果菜单会超出右边界，调整到左侧
    if (left + menuRect.width + submenuWidth > viewportWidth) {
        left = event.clientX - menuRect.width;
        // 如果调整后仍然超出，则放在按钮左侧
        if (left < 0) {
            left = Math.max(8, event.clientX - menuRect.width - submenuWidth);
        }
    }

    // 如果菜单会超出下边界，调整到上方
    if (top + menuRect.height > viewportHeight) {
        top = Math.max(8, event.clientY - menuRect.height);
    }

    // 确保不超出左边界
    if (left < 0) {
        left = 8;
    }

    // 确保不超出上边界
    if (top < 0) {
        top = 8;
    }

    menu.style.left = left + 'px';
    menu.style.top = top + 'px';

    // 如果菜单在右侧，子菜单应该在左侧显示
    if (left < event.clientX) {
        if (downloadSubmenu) {
            downloadSubmenu.style.left = 'auto';
            downloadSubmenu.style.right = '100%';
            downloadSubmenu.style.marginLeft = '0';
            downloadSubmenu.style.marginRight = '4px';
        }
    } else {
        if (downloadSubmenu) {
            downloadSubmenu.style.left = '100%';
            downloadSubmenu.style.right = 'auto';
            downloadSubmenu.style.marginLeft = '4px';
            downloadSubmenu.style.marginRight = '0';
        }
    }

    // 点击外部关闭菜单
    const closeMenu = (e) => {
        // 检查点击是否在主菜单或子菜单内
        const downloadMarkdownSubmenuEl = document.getElementById('download-markdown-submenu');
        const clickedInMenu = menu.contains(e.target);
        const clickedInDownloadSubmenu = downloadMarkdownSubmenuEl && downloadMarkdownSubmenuEl.contains(e.target);

        if (!clickedInMenu && !clickedInDownloadSubmenu) {
            closeContextMenu();
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        document.addEventListener('click', closeMenu);
    }, 0);

    refreshConversationContextPinText(convId);
}

let renameConversationTargetId = null;

function ensureConversationRenameModal() {
    let modal = document.getElementById('conversation-rename-modal');
    if (modal) return modal;

    modal = document.createElement('div');
    modal.id = 'conversation-rename-modal';
    modal.className = 'modal-overlay projects-modal-overlay';
    modal.style.display = 'none';
    modal.setAttribute('role', 'dialog');
    modal.setAttribute('aria-modal', 'true');
    modal.setAttribute('aria-labelledby', 'conversation-rename-title');
    modal.innerHTML = `
        <div class="projects-modal-dialog">
            <div class="projects-modal-header">
                <div class="projects-modal-header-text">
                    <div>
                        <h3 id="conversation-rename-title" data-i18n="chat.renameConversationTitle">重命名对话</h3>
                        <p class="projects-modal-subtitle" data-i18n="chat.renameConversationSubtitle">修改后会同步更新项目文件夹和最近对话中的名称</p>
                    </div>
                </div>
                <button type="button" class="projects-modal-close" data-conversation-rename-close aria-label="关闭" data-i18n="common.close" data-i18n-attr="aria-label" data-i18n-skip-text="true">&times;</button>
            </div>
            <div class="projects-modal-body">
                <div class="projects-form-field">
                    <label for="conversation-rename-input" data-i18n="chat.conversationTitleLabel">对话名称</label>
                    <input type="text" id="conversation-rename-input" class="form-input" maxlength="200" autocomplete="off" data-i18n="chat.conversationTitlePlaceholder" data-i18n-attr="placeholder" placeholder="请输入对话名称">
                </div>
            </div>
            <div class="projects-modal-footer">
                <button class="btn-secondary" type="button" data-conversation-rename-close data-i18n="common.cancel">取消</button>
                <button class="btn-primary" type="button" id="conversation-rename-submit" data-i18n="contextMenu.rename">重命名</button>
            </div>
        </div>`;
    modal.addEventListener('click', (event) => {
        if (event.target === modal) closeConversationRenameModal();
    });
    modal.querySelectorAll('[data-conversation-rename-close]').forEach((button) => {
        button.addEventListener('click', closeConversationRenameModal);
    });
    modal.querySelector('#conversation-rename-submit')?.addEventListener('click', saveConversationRename);
    modal.querySelector('#conversation-rename-input')?.addEventListener('keydown', (event) => {
        if (event.key === 'Enter') {
            event.preventDefault();
            saveConversationRename();
        } else if (event.key === 'Escape') {
            closeConversationRenameModal();
        }
    });
    document.body.appendChild(modal);
    if (typeof window.applyTranslations === 'function') window.applyTranslations(modal);
    return modal;
}

// 打开应用内重命名弹窗，避免内置浏览器拦截 window.prompt。
function renameConversation() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    renameConversationTargetId = convId;
    const currentTitle = contextMenuConversationTitle || '';
    ensureConversationRenameModal();
    const input = document.getElementById('conversation-rename-input');
    if (input) input.value = currentTitle;
    closeContextMenu();
    openAppModal('conversation-rename-modal', { focusEl: input });
    if (input) input.select();
}

function closeConversationRenameModal() {
    renameConversationTargetId = null;
    closeAppModal('conversation-rename-modal');
}

async function saveConversationRename() {
    const convId = renameConversationTargetId;
    const input = document.getElementById('conversation-rename-input');
    const newTitle = (input?.value || '').trim();
    if (!convId || !newTitle) {
        input?.focus();
        return;
    }

    const submitButton = document.getElementById('conversation-rename-submit');
    if (submitButton) submitButton.disabled = true;

    try {
        const response = await apiFetch(`/api/conversations/${convId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ title: newTitle.trim() }),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || '更新失败');
        }

        // 更新前端显示
        document.querySelectorAll('[data-conversation-id]').forEach((item) => {
            if (item.dataset.conversationId !== convId) return;
            item.querySelectorAll('.conversation-title, .project-conversation-title')
                .forEach((titleEl) => {
                    titleEl.textContent = newTitle.trim();
                    titleEl.title = newTitle.trim();
                });
        });

        // 同步更新顶栏正在运行的任务名称
        if (typeof updateActiveTaskConversationTitle === 'function') {
            updateActiveTaskConversationTitle(convId, newTitle.trim());
        }

        // 重新加载对话列表
        await loadConversations();
        if (typeof window.refreshChatProjectFolders === 'function') {
            await window.refreshChatProjectFolders();
        }
        closeConversationRenameModal();
    } catch (error) {
        console.error('重命名对话失败:', error);
        const failedLabel = typeof window.t === 'function' ? window.t('chat.renameFailed') : '重命名失败';
        const unknownErr = '未知错误';
        alert(failedLabel + ': ' + (error.message || unknownErr));
    } finally {
        if (submitButton) submitButton.disabled = false;
    }
}

async function assertConversationActionResponse(response, fallbackMessage) {
    if (response && response.ok) return response;
    let payload = {};
    try {
        payload = response ? await response.json() : {};
    } catch (e) { /* ignore */ }
    throw new Error(payload.error || payload.message || fallbackMessage);
}

function notifyConversationPinnedChanged(conversationId, pinned) {
    try {
        document.dispatchEvent(new CustomEvent('conversation-pinned-changed', {
            detail: { conversationId, pinned: !!pinned }
        }));
    } catch (e) { /* ignore */ }
}

// 置顶对话
async function pinConversation() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    // 点击后立即收起菜单，避免网络请求期间看起来“没有反应”。
    closeContextMenu();

    try {
        const response = await apiFetch(`/api/conversations/${convId}`);
        await assertConversationActionResponse(response, '获取对话失败');
        const conv = await response.json();
        const newPinned = !conv.pinned;

        const updateResponse = await apiFetch(`/api/conversations/${convId}/pinned`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ pinned: newPinned }),
        });
        await assertConversationActionResponse(updateResponse, '更新置顶状态失败');

        notifyConversationPinnedChanged(convId, newPinned);
        loadConversations();
    } catch (error) {
        console.error('置顶对话失败:', error);
        alert('置顶失败: ' + (error.message || '未知错误'));
    }

}

// 从上下文菜单查看攻击链
function showAttackChainFromContext() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    closeContextMenu();
    showAttackChain(convId);
}

function formatConversationDateForMarkdown(value) {
    if (!value) return '';
    const d = new Date(value);
    if (isNaN(d.getTime())) return '';
    const locale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    return d.toLocaleString(locale, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
    });
}

function getConversationRoleLabel(role) {
    switch (role) {
        case 'assistant':
            return 'Assistant';
        case 'user':
            return 'User';
        case 'system':
            return 'System';
        default:
            return role || 'Unknown';
    }
}

function formatConversationAsMarkdown(conversation, options = {}) {
    const includeToolDetails = !!options.includeToolDetails;
    const title = (conversation && conversation.title ? String(conversation.title) : '').trim() || 'Untitled Conversation';
    const createdAt = formatConversationDateForMarkdown(conversation && conversation.createdAt);
    const updatedAt = formatConversationDateForMarkdown(conversation && conversation.updatedAt);
    const messages = Array.isArray(conversation && conversation.messages) ? conversation.messages : [];

    let markdown = `# ${title}\n\n`;
    markdown += `- Conversation ID: \`${conversation && conversation.id ? conversation.id : ''}\`\n`;
    if (createdAt) markdown += `- Created At: ${createdAt}\n`;
    if (updatedAt) markdown += `- Updated At: ${updatedAt}\n`;
    markdown += `- Message Count: ${messages.length}\n\n`;
    markdown += '---\n\n';

    if (messages.length === 0) {
        markdown += '_No messages in this conversation._\n';
        return markdown;
    }

    messages.forEach((msg, index) => {
        if (msg && msg.role === 'user' && isInterruptContinueInjectChatMessage(msg.content)) {
            return;
        }
        const role = getConversationRoleLabel(msg && msg.role);
        const timestamp = formatConversationDateForMarkdown(msg && msg.createdAt);
        const content = msg && typeof msg.content === 'string' ? msg.content : '';

        markdown += `## ${index + 1}. ${role}`;
        if (timestamp) markdown += ` (${timestamp})`;
        markdown += '\n\n';
        markdown += content ? `${content}\n\n` : '_[Empty message]_\n\n';

        if (Array.isArray(msg && msg.processDetails) && msg.processDetails.length > 0) {
            markdown += '### Process Details\n\n';
            msg.processDetails.forEach((detail) => {
                const detailTime = formatConversationDateForMarkdown(detail && detail.timestamp);
                const eventType = detail && detail.eventType ? detail.eventType : 'event';
                const detailMsg = detail && detail.message ? detail.message : '';
                // Avoid "[label]:" pattern because some Markdown parsers treat it as link reference definition.
                markdown += `- \`${eventType}\``;
                if (detailTime) markdown += ` ${detailTime}`;
                if (detailMsg) markdown += `: ${detailMsg}`;
                markdown += '\n';

                if (includeToolDetails && detail && detail.data && (eventType === 'tool_call' || eventType === 'tool_result')) {
                    const pretty = JSON.stringify(detail.data, null, 2);
                    markdown += '\n```json\n';
                    markdown += pretty || '{}';
                    markdown += '\n```\n';
                }
            });
            markdown += '\n';
        }

        if (Array.isArray(msg && msg.mcpExecutionIds) && msg.mcpExecutionIds.length > 0) {
            markdown += `- MCP Execution IDs: ${msg.mcpExecutionIds.join(', ')}\n\n`;
        }

        markdown += '---\n\n';
    });

    return markdown;
}

function buildConversationMarkdownFileName(conversation, options = {}) {
    const includeToolDetails = !!options.includeToolDetails;
    const title = (conversation && conversation.title ? String(conversation.title) : '').trim() || 'conversation';
    const safeTitle = title
        .replace(/[\\/:*?"<>|]/g, '_')
        .replace(/\s+/g, '_')
        .slice(0, 60) || 'conversation';
    const idPart = (conversation && conversation.id ? String(conversation.id) : '').slice(0, 8) || 'export';
    const modePart = includeToolDetails ? 'full' : 'summary';
    return `${safeTitle}_${idPart}_${modePart}.md`;
}

// 从上下文菜单下载对话 Markdown
async function downloadConversationMarkdownFromContext(includeToolDetails = false) {
    const convId = contextMenuConversationId;
    if (!convId) return;

    try {
        // 下载不影响页面性能：直接从后端一次性拉取全量过程详情
        const response = await apiFetch(`/api/conversations/${convId}?include_process_details=1`);
        let conversation = null;
        try {
            conversation = await response.json();
        } catch (e) {
            conversation = null;
        }
        if (!response.ok) {
            const errorMsg = conversation && conversation.error ? conversation.error : 'unknown error';
            throw new Error(errorMsg);
        }

        const markdown = formatConversationAsMarkdown(conversation || {}, { includeToolDetails });
        const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = buildConversationMarkdownFileName(conversation || {}, { includeToolDetails });
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
    } catch (error) {
        console.error('下载对话 Markdown 失败:', error);
        const failedLabel = typeof window.t === 'function' ? window.t('chat.downloadConversationFailed') : '下载失败';
        const errMsg = error && error.message ? error.message : 'unknown error';
        alert(failedLabel + ': ' + errMsg);
    }

    closeContextMenu();
}

// 从上下文菜单跳转到漏洞管理，并按当前对话 ID 筛选
function navigateToVulnerabilitiesForContextConversation() {
    const convId = contextMenuConversationId;
    if (!convId) {
        closeContextMenu();
        return;
    }
    closeContextMenu();
    window.location.hash = 'vulnerabilities?conversation_id=' + encodeURIComponent(convId);
}

// 从上下文菜单删除对话
function deleteConversationFromContext() {
    if (typeof requirePermission === 'function' && !requirePermission('chat:delete')) return;
    const convId = contextMenuConversationId;
    if (!convId) return;

    const confirmMsg = typeof window.t === 'function' ? window.t('chat.deleteConversationConfirm') : '确定要删除此对话吗？';
    if (confirm(confirmMsg)) {
        deleteConversation(convId, true); // 跳过内部确认，因为这里已经确认过了
    }
    closeContextMenu();
}

// 关闭上下文菜单
function closeContextMenu() {
    const menu = document.getElementById('conversation-context-menu');
    if (menu) {
        menu.style.display = 'none';
    }
    const downloadSubmenu = document.getElementById('download-markdown-submenu');
    if (downloadSubmenu) {
        downloadSubmenu.style.display = 'none';
    }
    // 清除所有定时器
    clearDownloadMarkdownSubmenuHideTimeout();
    contextMenuConversationId = null;
    contextMenuConversationTitle = '';
}

// 显示批量管理模态框
let allConversationsForBatch = [];

function getConversationProjectId(conv) {
    return (conv?.projectId || conv?.project_id || '').trim();
}

function getConversationProjectLabel(conv) {
    const pid = getConversationProjectId(conv);
    if (!pid) {
        return typeof window.t === 'function' ? window.t('batchManageModal.noProject') : '无项目';
    }
    const name = window.projectNameById && window.projectNameById[pid];
    if (name) return name;
    return typeof window.t === 'function' ? window.t('batchManageModal.unknownProject') : '未知项目';
}

async function prefetchProjectNamesForConversations(conversations) {
    const missing = new Set();
    for (const conv of conversations || []) {
        const pid = getConversationProjectId(conv);
        if (pid && !(window.projectNameById && window.projectNameById[pid])) {
            missing.add(pid);
        }
    }
    if (!missing.size) return;
    const fetchSummary = typeof window.fetchProjectSummary === 'function'
        ? window.fetchProjectSummary
        : null;
    if (!fetchSummary) return;
    await Promise.all([...missing].map((id) => fetchSummary(id).catch(() => null)));
}

async function refreshBatchProjectFilter() {
    const sel = document.getElementById('batch-project-filter');
    if (!sel) return;
    const saved = sel.value || '';
    appendProjectFilterPinnedNativeOptions(sel);
    const normalized = await resolveProjectFilterSelection(saved);
    if (normalized && normalized !== CONVERSATION_PROJECT_FILTER_NONE) {
        await appendSelectedProjectFilterOption(sel, normalized);
    }
    sel.value = normalized;
    syncProjectFilterCustomSelect(BATCH_PROJECT_FILTER_SELECT_ID);
}

function getBatchFilteredConversations() {
    const query = (document.getElementById('batch-search-input')?.value || '').trim().toLowerCase();
    const projectFilter = (document.getElementById('batch-project-filter')?.value || '').trim();
    return allConversationsForBatch.filter((conv) => {
        const pid = getConversationProjectId(conv);
        if (projectFilter) {
            if (projectFilter === CONVERSATION_PROJECT_FILTER_NONE) {
                if (pid) return false;
            } else if (pid !== projectFilter) {
                return false;
            }
        }
        if (!query) return true;
        const title = (conv.title || '').toLowerCase();
        const projectName = getConversationProjectLabel(conv).toLowerCase();
        return title.includes(query) || projectName.includes(query);
    });
}

function applyBatchConversationFilters() {
    const filtered = getBatchFilteredConversations();
    updateBatchManageTitle(filtered.length);
    renderBatchConversations(filtered);
}

// 更新批量管理模态框标题（含条数），支持 i18n；count 为当前条数
function updateBatchManageTitle(count) {
    const titleEl = document.getElementById('batch-manage-title');
    if (!titleEl || typeof window.t !== 'function') return;
    const template = window.t('batchManageModal.title', { count: '__C__' });
    const parts = template.split('__C__');
    titleEl.innerHTML = (parts[0] || '') + '<span id="batch-manage-count">' + (count || 0) + '</span>' + (parts[1] || '');
}

async function showBatchManageModal() {
    try {
        initProjectFilterCustomSelect(BATCH_PROJECT_FILTER_SELECT_ID);
        allConversationsForBatch = await fetchAllConversations('');
        await prefetchProjectNamesForConversations(allConversationsForBatch);
        await refreshBatchProjectFilter();
        const sidebarFilter = getConversationProjectFilter();
        const batchSel = document.getElementById('batch-project-filter');
        if (batchSel && sidebarFilter && (
            sidebarFilter === CONVERSATION_PROJECT_FILTER_NONE ||
            (window.projectNameById && window.projectNameById[sidebarFilter])
        )) {
            batchSel.value = sidebarFilter;
        }
        const searchInput = document.getElementById('batch-search-input');
        if (searchInput) searchInput.value = '';
        applyBatchConversationFilters();
        openAppModal('batch-manage-modal', { focus: false });
    } catch (error) {
        console.error('加载对话列表失败:', error);
        initProjectFilterCustomSelect(BATCH_PROJECT_FILTER_SELECT_ID);
        allConversationsForBatch = [];
        await refreshBatchProjectFilter();
        applyBatchConversationFilters();
        openAppModal('batch-manage-modal', { focus: false });
    }
}

// 安全截断中文字符串，避免在汉字中间截断
function safeTruncateText(text, maxLength = 50) {
    if (!text || typeof text !== 'string') {
        return text || '';
    }

    // 使用 Array.from 将字符串转换为字符数组（正确处理 Unicode 代理对）
    const chars = Array.from(text);

    // 如果文本长度未超过限制，直接返回
    if (chars.length <= maxLength) {
        return text;
    }

    // 截断到最大长度（基于字符数，而不是代码单元）
    let truncatedChars = chars.slice(0, maxLength);

    // 尝试在标点符号或空格处截断，使截断更自然
    // 在截断点往前查找合适的断点（不超过20%的长度）
    const searchRange = Math.floor(maxLength * 0.2);
    const breakChars = ['，', '。', '、', ' ', ',', '.', ';', ':', '!', '?', '！', '？', '/', '\\', '-', '_'];
    let bestBreakPos = truncatedChars.length;

    for (let i = truncatedChars.length - 1; i >= truncatedChars.length - searchRange && i >= 0; i--) {
        if (breakChars.includes(truncatedChars[i])) {
            bestBreakPos = i + 1; // 在标点符号后断开
            break;
        }
    }

    // 如果找到合适的断点，使用它；否则使用原截断位置
    if (bestBreakPos < truncatedChars.length) {
        truncatedChars = truncatedChars.slice(0, bestBreakPos);
    }

    // 将字符数组转换回字符串，并添加省略号
    return truncatedChars.join('') + '...';
}

// 渲染批量管理对话列表
function renderBatchConversations(filtered = null) {
    const list = document.getElementById('batch-conversations-list');
    if (!list) return;

    const conversations = filtered || allConversationsForBatch;
    list.innerHTML = '';

    conversations.forEach(conv => {
        const row = document.createElement('div');
        row.className = 'batch-conversation-row';
        row.dataset.conversationId = conv.id;

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'batch-conversation-checkbox theme-checkbox';
        checkbox.dataset.conversationId = conv.id;
        checkbox.addEventListener('change', syncSelectAllBatchCheckbox);

        const checkboxCol = document.createElement('div');
        checkboxCol.className = 'batch-table-col-checkbox';
        checkboxCol.appendChild(checkbox);

        const name = document.createElement('div');
        name.className = 'batch-table-col-name';
        const originalTitle = conv.title || (typeof window.t === 'function' ? window.t('batchManageModal.unnamedConversation') : '未命名对话');
        const truncatedTitle = safeTruncateText(originalTitle, 36);
        name.textContent = truncatedTitle;
        name.title = originalTitle;

        const project = document.createElement('div');
        project.className = 'batch-table-col-project';
        const projectLabel = getConversationProjectLabel(conv);
        const truncatedProject = safeTruncateText(projectLabel, 28);
        project.textContent = truncatedProject;
        project.title = projectLabel;
        if (!getConversationProjectId(conv)) {
            project.classList.add('is-unbound');
        }

        const time = document.createElement('div');
        time.className = 'batch-table-col-time';
        const dateObj = conv.updatedAt ? new Date(conv.updatedAt) : new Date();
        const locale = (typeof i18next !== 'undefined' && i18next.language) ? i18next.language : 'zh-CN';
        time.textContent = dateObj.toLocaleString(locale, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit'
        });

        const action = document.createElement('div');
        action.className = 'batch-table-col-action';
        const deleteBtn = document.createElement('button');
        deleteBtn.type = 'button';
        deleteBtn.className = 'batch-delete-btn';
        deleteBtn.innerHTML = `
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6h14zM10 11v6M14 11v6"
                      stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
        `;
        const deleteLabel = typeof window.t === 'function' ? window.t('contextMenu.deleteConversation') : '删除此对话';
        deleteBtn.title = deleteLabel;
        deleteBtn.setAttribute('aria-label', deleteLabel);
        deleteBtn.onclick = (e) => {
            e.stopPropagation();
            deleteConversation(conv.id);
        };
        action.appendChild(deleteBtn);

        row.appendChild(checkboxCol);
        row.appendChild(name);
        row.appendChild(project);
        row.appendChild(time);
        row.appendChild(action);

        list.appendChild(row);
    });

    syncSelectAllBatchCheckbox();
}

// 筛选批量管理对话
function filterBatchConversations() {
    applyBatchConversationFilters();
}

// 全选/取消全选
function toggleSelectAllBatch() {
    const selectAll = document.getElementById('batch-select-all');
    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox');

    if (selectAll) {
        selectAll.indeterminate = false;
    }
    checkboxes.forEach(cb => {
        cb.checked = selectAll.checked;
    });
}

function syncSelectAllBatchCheckbox() {
    const selectAll = document.getElementById('batch-select-all');
    if (!selectAll) return;

    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox');
    const total = checkboxes.length;
    const checked = document.querySelectorAll('.batch-conversation-checkbox:checked').length;

    if (total === 0 || checked === 0) {
        selectAll.checked = false;
        selectAll.indeterminate = false;
    } else if (checked === total) {
        selectAll.checked = true;
        selectAll.indeterminate = false;
    } else {
        selectAll.checked = false;
        selectAll.indeterminate = true;
    }
}

// 删除选中的对话
async function deleteSelectedConversations() {
    if (typeof requirePermission === 'function' && !requirePermission('chat:delete')) return;
    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox:checked');
    if (checkboxes.length === 0) {
        alert(typeof window.t === 'function' ? window.t('batchManageModal.confirmDeleteNone') : '请先选择要删除的对话');
        return;
    }

    const confirmMsg = typeof window.t === 'function' ? window.t('batchManageModal.confirmDeleteN', { count: checkboxes.length }) : '确定要删除选中的 ' + checkboxes.length + ' 条对话吗？';
    if (!confirm(confirmMsg)) {
        return;
    }

    const ids = Array.from(checkboxes).map(cb => cb.dataset.conversationId);

    try {
        for (const id of ids) {
            await deleteConversation(id, true); // 跳过内部确认，因为批量删除时已经确认过了
        }
        // 删除后保持弹窗打开，便于继续管理剩余对话
        const selectAll = document.getElementById('batch-select-all');
        if (selectAll) {
            selectAll.checked = false;
            selectAll.indeterminate = false;
        }
    } catch (error) {
        console.error('删除失败:', error);
        const failedMsg = typeof window.t === 'function' ? window.t('batchManageModal.deleteFailed') : '删除失败';
        const unknownErr = '未知错误';
        alert(failedMsg + ': ' + (error.message || unknownErr));
    }
}

// 关闭批量管理模态框
function closeBatchManageModal() {
    closeAllProjectFilterCustomSelects();
    closeAppModal('batch-manage-modal');
    const selectAll = document.getElementById('batch-select-all');
    if (selectAll) {
        selectAll.checked = false;
        selectAll.indeterminate = false;
    }
    const searchInput = document.getElementById('batch-search-input');
    if (searchInput) searchInput.value = '';
    const batchProj = document.getElementById('batch-project-filter');
    if (batchProj) batchProj.value = '';
    allConversationsForBatch = [];
}

// 语言切换时刷新当前聊天页内的时间与动态文案（消息时间、执行流程时间由 monitor 的 refreshProgressAndTimelineI18n 处理）
function refreshChatPanelI18n() {
    const locale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const timeOpts = { hour: '2-digit', minute: '2-digit' };
    if (locale === 'zh-CN') timeOpts.hour12 = false;
    const t = typeof window.t === 'function' ? window.t : function (k) { return k; };

    const messagesEl = document.getElementById('chat-messages');
    if (messagesEl) {
        messagesEl.querySelectorAll('.message-time[data-message-time]').forEach(function (el) {
            try {
                const d = new Date(el.dataset.messageTime);
                if (!isNaN(d.getTime())) {
                    el.textContent = d.toLocaleTimeString(locale, timeOpts);
                }
            } catch (e) { /* ignore */ }
        });
        messagesEl.querySelectorAll('.process-detail-btn').forEach(function (btn) {
            const span = btn.querySelector('span');
            if (!span) return;
            const assistantEl = btn.closest('.message.assistant');
            const messageId = assistantEl && assistantEl.id;
            const detailsId = messageId ? 'process-details-' + messageId : '';
            const timeline = detailsId ? document.getElementById(detailsId) && document.getElementById(detailsId).querySelector('.progress-timeline') : null;
            const expanded = timeline && timeline.classList.contains('expanded');
            span.textContent = expanded ? t('tasks.collapseDetail') : t('chat.expandDetail');
        });
        const copyLabel = t('common.copy');
        const copyTitle = t('chat.copyMessageTitle');
        messagesEl.querySelectorAll('.message-copy-btn').forEach(function (btn) {
            if (btn.dataset.copySuccessActive === '1') return;
            const span = btn.querySelector('span');
            if (span) span.textContent = copyLabel;
            btn.title = copyTitle;
            btn.setAttribute('aria-label', copyTitle);
        });
        messagesEl.querySelectorAll('.message.assistant').forEach(function (msgEl) {
            if (typeof window.syncAssistantTurnSummary === 'function') {
                window.syncAssistantTurnSummary(msgEl);
            }
            if (typeof window.syncMcpToolsToggleButton === 'function') {
                window.syncMcpToolsToggleButton(msgEl);
            }
        });
        if (window.CyberStrikeChatScroll && typeof window.CyberStrikeChatScroll.refreshTurnRail === 'function') {
            window.CyberStrikeChatScroll.refreshTurnRail();
        }
    }

    if (isAppModalOpen('mcp-detail-modal')) {
        const detailTimeEl = document.getElementById('detail-time');
        if (detailTimeEl && detailTimeEl.dataset.detailTimeIso) {
            try {
                const d = new Date(detailTimeEl.dataset.detailTimeIso);
                if (!isNaN(d.getTime())) {
                    detailTimeEl.textContent = d.toLocaleString(locale);
                }
            } catch (e) { /* ignore */ }
        }
        const statusEl = document.getElementById('detail-status');
        if (statusEl && statusEl.dataset.detailStatus !== undefined && typeof getStatusText === 'function') {
            statusEl.textContent = getStatusText(statusEl.dataset.detailStatus);
        }
    }
}

// 语言切换时刷新批量管理模态框标题（若当前正在显示）；并刷新对话列表时间格式与系统就绪提示；刷新当前页消息时间与动态文案
document.addEventListener('languagechange', function () {
    refreshSystemReadyMessageBubbles();
    refreshChatPanelI18n();
    if (typeof refreshConversationProjectFilter === 'function') {
        refreshConversationProjectFilter();
    }
    if (typeof refreshBatchProjectFilter === 'function') {
        refreshBatchProjectFilter().then(() => {
            const modal = document.getElementById('batch-manage-modal');
            if (modal && isAppModalOpen('batch-manage-modal') && typeof applyBatchConversationFilters === 'function') {
                applyBatchConversationFilters();
            }
        });
    }
    // 侧边栏最近对话等列表的时间戳会随语言变化（24h/12h 等），重新拉列表以统一格式
    if (typeof loadConversations === 'function') {
        loadConversations();
    }
});

// 初始化时加载对话列表
document.addEventListener('DOMContentLoaded', async () => {
    ensureProjectSidebarStructure();
    if (window.i18nReady) await window.i18nReady;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(document.getElementById('conversation-sidebar'));
    }
    // 任务栏不再暴露项目筛选，清除旧选择以免隐藏部分任务。
    setConversationProjectFilter('');
    restoreRecentConversationsState();
    updateConversationSortMenuUI();
    initConversationProjectCustomSelect();
    initConversationsPaginationEvents();
    await refreshConversationProjectFilter();
    await loadConversations();

    // 添加页面焦点时自动刷新对话列表的功能
    // 这样当通过OpenAPI创建对话后，切换回页面时能自动看到新对话
    let lastFocusTime = Date.now();
    const CONVERSATION_REFRESH_INTERVAL = 30000; // 30秒内最多刷新一次，避免过于频繁

    window.addEventListener('focus', () => {
        const now = Date.now();
        // 如果距离上次刷新超过30秒，才刷新对话列表
        if (now - lastFocusTime > CONVERSATION_REFRESH_INTERVAL) {
            lastFocusTime = now;
            if (typeof loadConversations === 'function') {
                loadConversations();
            }
        }
    });

    // 监听页面可见性变化（当用户切换标签页回来时）
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden) {
            // 页面变为可见时，检查是否需要刷新
            const now = Date.now();
            if (now - lastFocusTime > CONVERSATION_REFRESH_INTERVAL) {
                lastFocusTime = now;
                if (typeof loadConversations === 'function') {
                    loadConversations();
                }
            }
        }
    });

    // 任意入口删除对话后同步：若删除的是当前对话则清空主区，并刷新侧边栏列表（如从 WebShell AI 助手删除）
    document.addEventListener('conversation-deleted', (e) => {
        const id = e.detail && e.detail.conversationId;
        if (!id) return;
        // API 已确认删除后立即移除可见列表项，网络刷新只负责校准分页和计数。
        document.querySelectorAll('.conversation-item[data-conversation-id]')
            .forEach((item) => {
                if (item.dataset.conversationId === id) item.remove();
            });
        if (id === currentConversationId) {
            currentConversationId = null;
            try {
                window.currentConversationId = '';
            } catch (e) { /* ignore */ }
            const messagesDiv = document.getElementById('chat-messages');
            if (messagesDiv) messagesDiv.innerHTML = '';
            renderChatWelcomeEmptyState();
            addAttackChainButton(null);
        }
        if (typeof loadConversations === 'function') {
            loadConversations();
        }
    });
});

async function refreshAllProjectFilterSelects() {
    await refreshConversationProjectFilter();
    await refreshBatchProjectFilter();
}

// 顶层 async function 不会自动挂到 window，hitl 等脚本依赖 window.loadConversation
if (typeof window !== 'undefined') {
    window.loadConversation = loadConversation;
    window.startNewConversation = startNewConversation;
    window.refreshChatWelcomeEmptyState = refreshSystemReadyMessageBubbles;
    window.openConversationContextMenuForId = openConversationContextMenuForId;
    window.renameConversation = renameConversation;
    window.closeConversationRenameModal = closeConversationRenameModal;
    window.saveConversationRename = saveConversationRename;
    window.refreshConversationProjectFilter = refreshConversationProjectFilter;
    window.refreshAllProjectFilterSelects = refreshAllProjectFilterSelects;
    window.onConversationProjectFilterChange = onConversationProjectFilterChange;
}
