// WebShell 管理（类似冰蝎/蚁剑：虚拟终端、文件管理、命令执行）

const WEBSHELL_SIDEBAR_WIDTH_KEY = 'webshell_sidebar_width';
const WEBSHELL_DEFAULT_SIDEBAR_WIDTH = 360;
/** 右侧主区域（终端/文件管理）最小宽度，避免拖到中间时右边变形 */
const WEBSHELL_MAIN_MIN_WIDTH = 380;
const WEBSHELL_PROMPT = 'shell> ';
let webshellConnections = [];
let currentWebshellId = null;
let webshellTerminalInstance = null;
let webshellTerminalFitAddon = null;
let webshellTerminalResizeObserver = null;
let webshellTerminalResizeContainer = null;
let webshellCurrentConn = null;
let webshellLineBuffer = '';
let webshellRunning = false;
let webshellTerminalRunning = false;
let webshellTerminalLogsByConn = {};
let webshellTerminalSessionsByConn = {};
let webshellPersistLoadedByConn = {};
let webshellPersistSaveTimersByConn = {};
// 按连接保存命令历史，用于上下键
let webshellHistoryByConn = {};
let webshellHistoryIndex = -1;
const WEBSHELL_HISTORY_MAX = 100;
// 清屏防重入：一次点击只执行一次（避免多次绑定或重复触发导致多个 shell>）
let webshellClearInProgress = false;
// AI 助手：按连接 ID 保存对话 ID，便于多轮对话
let webshellAiConvMap = {};
// AI 助手：项目绑定（已有对话按 convId，新对话按 connId 草稿）
let webshellAiProjectByConvId = {};
let webshellAiDraftProjectByConn = {};
let webshellAiSending = false;
let webshellAiAbortController = null; // AbortController for current AI stream
let webshellAiStreamReader = null;    // Current ReadableStreamDefaultReader
let webshellDbConfigByConn = {};
let webshellDirTreeByConn = {};
let webshellDirExpandedByConn = {};
let webshellDirLoadedByConn = {};
let webshellSelectedFileByConn = {};
// 流式打字机效果：当前会话的 response 序号，用于中止过期的打字
let webshellStreamingTypingId = 0;
let webshellProbeStatusById = {};
let webshellBatchProbeRunning = false;

/** 允许的响应编码，与后端 normalizeWebshellEncoding 对齐 */
const WEBSHELL_ALLOWED_ENCODINGS = ['auto', 'utf-8', 'gbk', 'gb18030'];

/** 归一化连接的 encoding 字段，返回 'auto' | 'utf-8' | 'gbk' | 'gb18030'（空/未知 → auto） */
function normalizeWebshellEncoding(v) {
    var s = (v == null ? '' : String(v)).trim().toLowerCase();
    if (s === 'utf8') s = 'utf-8';
    if (!s) return 'auto';
    return WEBSHELL_ALLOWED_ENCODINGS.indexOf(s) >= 0 ? s : 'auto';
}

/** 从连接对象取编码，便于透传到 /api/webshell/exec 与 /api/webshell/file */
function webshellConnEncoding(conn) {
    return normalizeWebshellEncoding(conn && conn.encoding);
}

/** 允许的目标 OS，与后端 normalizeWebshellOS 对齐 */
const WEBSHELL_ALLOWED_OS = ['auto', 'linux', 'windows'];

/** 归一化连接的 os 字段，返回 'auto' | 'linux' | 'windows'（空/未知 → auto） */
function normalizeWebshellOS(v) {
    var s = (v == null ? '' : String(v)).trim().toLowerCase();
    if (!s) return 'auto';
    return WEBSHELL_ALLOWED_OS.indexOf(s) >= 0 ? s : 'auto';
}

/** 从连接对象取目标 OS，便于透传到 /api/webshell/exec 与 /api/webshell/file */
function webshellConnOS(conn) {
    return normalizeWebshellOS(conn && conn.os);
}

/** 生成一次性探活 token，避免固定回显值被包装时误判 */
function buildWebshellProbeToken() {
    return '__CSAI_PROBE_' + Math.random().toString(36).slice(2, 10) + '_' + Date.now().toString(36) + '__';
}

/** 构造跨 Windows/Linux 都可执行的探活命令 */
function buildWebshellProbeCommand(token) {
    return 'echo ' + token;
}

/** 探活成功判定：HTTP 成功且输出中包含本次 token */
function isWebshellProbeOutputMatched(output, token) {
    if (!token) return false;
    var text = (output == null) ? '' : String(output);
    return text.indexOf(token) !== -1;
}

/**
 * 组装 /api/webshell/file 的公共请求体。
 * 所有文件管理调用点都应走此函数，避免遗漏字段（如 connection_id）。
 * @param {Object} conn 连接对象
 * @param {Object} extra 额外字段（action / path / content / target_path / chunk_index ...）
 * @returns {string} JSON 字符串
 */
function webshellFileRequestBody(conn, extra) {
    const base = {
        url: conn.url,
        password: conn.password || '',
        type: conn.type || 'php',
        method: (conn.method || 'post').toLowerCase(),
        cmd_param: conn.cmdParam || '',
        encoding: webshellConnEncoding(conn),
        os: webshellConnOS(conn),
        connection_id: conn.id || ''
    };
    const merged = Object.assign(base, extra || {});
    return JSON.stringify(merged);
}

/**
 * 当服务端探活命中目标系统（仅 auto 连接首次列目录时出现）时，
 * 把结果同步到本地 webshellConnections 缓存 + 持久化到数据库。
 * 后续刷新不再探活，AI 也能直接看到正确的 OS 上下文。
 */
function applyWebshellDetectedOS(conn, data) {
    if (!conn || !data || !data.detected_os) return;
    const detected = normalizeWebshellOS(data.detected_os);
    if (detected !== 'linux' && detected !== 'windows') return;
    if (webshellConnOS(conn) !== 'auto') return; // 用户已显式配置，尊重之
    conn.os = detected;
    if (Array.isArray(webshellConnections)) {
        for (var i = 0; i < webshellConnections.length; i++) {
            if (webshellConnections[i] && webshellConnections[i].id === conn.id) {
                webshellConnections[i].os = detected;
                break;
            }
        }
    }
    if (typeof renderWebshellList === 'function') {
        try { renderWebshellList(); } catch (e) {}
    }
    // 服务端已经回写了 DB；但极少数情况下调用方未带 connection_id，这里再兜底 PUT 一次
    if (conn.id && typeof apiFetch === 'function') {
        apiFetch('/api/webshell/connections/' + encodeURIComponent(conn.id), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                url: conn.url,
                password: conn.password || '',
                type: conn.type || 'php',
                method: conn.method || 'post',
                cmd_param: conn.cmdParam || '',
                remark: conn.remark || '',
                encoding: conn.encoding || 'auto',
                os: detected
            })
        }).catch(function () {});
    }
}

/** 与主对话页一致：Eino 模式走 /api/multi-agent/stream，body 带 orchestration */
function resolveWebshellAiStreamRequest() {
    if (typeof apiFetch === 'undefined') {
        return Promise.resolve({ path: '/api/eino-agent/stream', orchestration: null });
    }
    return apiFetch('/api/config').then(function (r) {
        if (!r.ok) return null;
        return r.json();
    }).then(function (cfg) {
        var norm = 'eino_single';
        if (typeof window.csaiChatAgentMode === 'object' && typeof window.csaiChatAgentMode.normalizeStored === 'function') {
            norm = window.csaiChatAgentMode.normalizeStored(localStorage.getItem('cyberstrike-chat-agent-mode'), cfg);
        } else {
            var mode = localStorage.getItem('cyberstrike-chat-agent-mode');
            norm = (mode && (mode === 'eino_single' || mode === 'deep' || mode === 'plan_execute' || mode === 'supervisor')) ? mode : 'eino_single';
        }
        if (cfg && cfg.multi_agent && cfg.multi_agent.enabled &&
            typeof window.csaiChatAgentMode === 'object' && typeof window.csaiChatAgentMode.isEino === 'function' && window.csaiChatAgentMode.isEino(norm)) {
            return { path: '/api/multi-agent/stream', orchestration: norm };
        }
        return { path: '/api/eino-agent/stream', orchestration: null };
    }).catch(function () {
        return { path: '/api/eino-agent/stream', orchestration: null };
    });
}

// ─── WebShell AI 助手：角色 + 对话模式选择器（与主「对话」页对齐） ───

let wsRolesCache = null; // 缓存 /api/roles 结果

function wsLoadRoles() {
    if (typeof apiFetch === 'undefined') return;
    apiFetch('/api/roles').then(function (r) { return r.json(); }).then(function (data) {
        wsRolesCache = (data && Array.isArray(data.roles)) ? data.roles : [];
        wsRenderRoleList();
        wsUpdateRoleSelectorDisplay();
    }).catch(function () { /* ignore */ });
}

function wsUpdateRoleSelectorDisplay() {
    var iconEl = document.getElementById('ws-role-selector-icon');
    var textEl = document.getElementById('ws-role-selector-text');
    if (!iconEl || !textEl) return;
    var cur = (typeof getCurrentRole === 'function') ? getCurrentRole() : (localStorage.getItem('currentRole') || '');
    if (!cur) {
        iconEl.textContent = '\ud83d\udd35';
        textEl.textContent = (typeof window.t === 'function' ? window.t('chat.defaultRole') : '') || 'Default';
        return;
    }
    if (wsRolesCache) {
        for (var i = 0; i < wsRolesCache.length; i++) {
            if (wsRolesCache[i].name === cur) {
                iconEl.textContent = wsRolesCache[i].icon || '\ud83d\udd35';
                textEl.textContent = cur;
                return;
            }
        }
    }
    iconEl.textContent = '\ud83d\udd35';
    textEl.textContent = cur;
}

function wsRenderRoleList() {
    var listEl = document.getElementById('ws-role-selection-list');
    if (!listEl) return;
    var cur = (typeof getCurrentRole === 'function') ? getCurrentRole() : (localStorage.getItem('currentRole') || '');
    var html = '';
    // 默认角色
    var defSelected = !cur ? ' selected' : '';
    var defDesc = wsTOr('roles.defaultRoleDescription', '默认角色，不额外携带用户提示词，使用所有工具');
    html += '<button type="button" class="role-selection-item-main' + defSelected + '" data-selection-detail="' + escapeHtmlAttr(defDesc) + '" onclick="wsSelectRole(\'\')">' +
        '<div class="role-selection-item-icon-main">\ud83d\udd35</div>' +
        '<div class="role-selection-item-content-main"><div class="role-selection-item-name-main">' +
        (wsTOr('chat.defaultRole', 'Default')) +
        '</div><div class="role-selection-item-description-main">' +
        escapeHtml(defDesc) +
        '</div></div>' +
        (defSelected ? '<div class="role-selection-checkmark-main">\u2713</div>' : '') +
        '</button>';
    if (wsRolesCache) {
        for (var i = 0; i < wsRolesCache.length; i++) {
            var r = wsRolesCache[i];
            if (!r.enabled) continue;
            if (r.name === 'Default') continue; // 已在上方硬编码默认角色，跳过 API 返回的默认项
            var sel = (r.name === cur) ? ' selected' : '';
            var desc = r.description || '';
            html += '<button type="button" class="role-selection-item-main' + sel + '" data-selection-detail="' + escapeHtmlAttr(desc) + '" onclick="wsSelectRole(\'' + r.name.replace(/'/g, "\\'") + '\')">' +
                '<div class="role-selection-item-icon-main">' + escapeHtml(r.icon || '\ud83d\udd35') + '</div>' +
                '<div class="role-selection-item-content-main"><div class="role-selection-item-name-main">' + escapeHtml(r.name) + '</div>' +
                '<div class="role-selection-item-description-main">' + escapeHtml(desc.substring(0, 60)) + '</div></div>' +
                (sel ? '<div class="role-selection-checkmark-main">\u2713</div>' : '') +
                '</button>';
        }
    }
    listEl.innerHTML = html;
}

function wsSelectRole(name) {
    var roleName = name || '';
    // 使用主页的 handleRoleChange 来同步 roles.js 内部状态和 localStorage
    if (typeof handleRoleChange === 'function') {
        try { handleRoleChange(roleName); } catch (e) { /* */ }
    } else {
        try { localStorage.setItem('currentRole', roleName); } catch (e) { /* */ }
    }
    if (typeof window.currentSelectedRole !== 'undefined') window.currentSelectedRole = roleName;
    wsUpdateRoleSelectorDisplay();
    wsRenderRoleList();
    wsCloseRolePanel();
}

function wsToggleRolePanel() {
    var panel = document.getElementById('ws-role-selection-panel');
    if (!panel) return;
    var isOpen = panel.style.display === 'flex';
    if (isOpen) { wsCloseRolePanel(); return; }
    wsCloseAgentModePanel();
    wsCloseProjectPanel();
    panel.style.display = 'flex';
}
function wsCloseRolePanel() {
    var panel = document.getElementById('ws-role-selection-panel');
    if (panel) panel.style.display = 'none';
}

// ─── 对话模式选择器 ───

function wsInitAgentMode() {
    if (typeof apiFetch === 'undefined') return;
    apiFetch('/api/config').then(function (r) { return r.ok ? r.json() : null; }).then(function (cfg) {
        var wrapper = document.getElementById('ws-agent-mode-wrapper');
        if (!wrapper) return;
        wrapper.style.display = '';
        // 是否启用多代理
        var multiOn = cfg && cfg.multi_agent && cfg.multi_agent.enabled;
        // 隐藏/显示多代理选项
        var opts = wrapper.querySelectorAll('.ws-agent-mode-option');
        opts.forEach(function (el) {
            var v = el.getAttribute('data-value');
            if (v === 'deep' || v === 'plan_execute' || v === 'supervisor') {
                el.style.display = multiOn ? '' : 'none';
            }
        });
        // 标准化当前值
        var stored = localStorage.getItem('cyberstrike-chat-agent-mode');
        var norm;
        if (typeof window.csaiChatAgentMode === 'object' && typeof window.csaiChatAgentMode.normalizeStored === 'function') {
            norm = window.csaiChatAgentMode.normalizeStored(stored, cfg);
        } else {
            norm = stored || 'eino_single';
            if (norm !== 'eino_single' && norm !== 'deep' && norm !== 'plan_execute' && norm !== 'supervisor') {
                norm = 'eino_single';
            }
            if (norm === 'multi') norm = 'deep';
        }
        wsSyncAgentMode(norm);
    }).catch(function () {
        var wrapper = document.getElementById('ws-agent-mode-wrapper');
        if (wrapper) wrapper.style.display = '';
        wsSyncAgentMode('eino_single');
    });
}

function wsSyncAgentMode(value) {
    var hid = document.getElementById('ws-agent-mode-select');
    var label = document.getElementById('ws-agent-mode-text');
    var icon = document.getElementById('ws-agent-mode-icon');
    if (hid) hid.value = value;
    if (label) label.textContent = (typeof getAgentModeLabelForValue === 'function') ? getAgentModeLabelForValue(value) : value;
    if (icon) icon.textContent = (typeof getAgentModeIconForValue === 'function') ? getAgentModeIconForValue(value) : '\ud83e\udd16';
    var wrapper = document.getElementById('ws-agent-mode-wrapper');
    if (wrapper) {
        wrapper.querySelectorAll('.ws-agent-mode-option').forEach(function (el) {
            el.classList.toggle('selected', el.getAttribute('data-value') === value);
        });
    }
}

function wsSelectAgentMode(mode) {
    try { localStorage.setItem('cyberstrike-chat-agent-mode', mode); } catch (e) { /* */ }
    wsSyncAgentMode(mode);
    wsCloseAgentModePanel();
    // 同步主页模式选择器
    if (typeof syncAgentModeFromValue === 'function') try { syncAgentModeFromValue(mode); } catch (e) { /* */ }
}

function wsToggleAgentModePanel() {
    var panel = document.getElementById('ws-agent-mode-panel');
    if (!panel) return;
    var isOpen = panel.style.display === 'flex';
    if (isOpen) { wsCloseAgentModePanel(); return; }
    wsCloseRolePanel();
    wsCloseProjectPanel();
    panel.style.display = 'flex';
}
function wsCloseAgentModePanel() {
    var panel = document.getElementById('ws-agent-mode-panel');
    if (panel) panel.style.display = 'none';
}

// ─── WebShell AI 项目选择器（与主「对话」页对齐） ───

function wsProjectT(key, fallback) {
    if (typeof window.t === 'function') {
        var v = window.t(key);
        if (v && v !== key) return v;
    }
    return fallback;
}

function wsProjectPickerT(key) {
    var fallbacks = {
        'projects.noProject': '无项目',
        'projects.noProjectDescription': '不绑定项目黑板',
        'projects.sharedFactBoard': '共享事实黑板',
        'common.untitled': '未命名',
        'common.loading': '加载中…',
        'chat.filterProjectSearchEmpty': '没有匹配的项目',
        'chat.filterProjectSearchMore': '更多项目请输入关键字搜索',
        'chat.filterProjectSearchFailed': '加载项目失败，请重试',
    };
    return wsProjectT(key, fallbacks[key]);
}

function getWebshellAiConvId(conn) {
    if (!conn || !conn.id) return '';
    return webshellAiConvMap[conn.id] || '';
}

function getWebshellAiProjectSelection(conn) {
    if (!conn || !conn.id) return '';
    var convId = getWebshellAiConvId(conn);
    if (convId) return webshellAiProjectByConvId[convId] || '';
    return webshellAiDraftProjectByConn[conn.id] || '';
}

function wsSetWebshellAiProject(conn, projectId) {
    if (!conn || !conn.id) return;
    var pid = projectId || '';
    var convId = getWebshellAiConvId(conn);
    if (convId) {
        if (pid) webshellAiProjectByConvId[convId] = pid;
        else delete webshellAiProjectByConvId[convId];
    } else if (pid) {
        webshellAiDraftProjectByConn[conn.id] = pid;
    } else {
        delete webshellAiDraftProjectByConn[conn.id];
    }
    wsUpdateProjectButtonLabel();
}

function wsIsActiveProjectId(id) {
    if (!id) return false;
    var map = window.projectNameById || {};
    return !!map[id];
}

function wsResolveWebshellAiProjectSelection(conn) {
    var raw = getWebshellAiProjectSelection(conn);
    if (!raw) return '';
    return wsIsActiveProjectId(raw) ? raw : '';
}

function wsUpdateProjectButtonLabel() {
    var textEl = document.getElementById('ws-project-text');
    if (!textEl || !webshellCurrentConn) return;
    var id = wsResolveWebshellAiProjectSelection(webshellCurrentConn);
    var nameMap = window.projectNameById || {};
    textEl.textContent = id && nameMap[id] ? nameMap[id] : wsProjectT('projects.noProject', '无项目');
}

async function wsLoadProjectPanelList() {
    if (typeof window.renderProjectPickerPanel !== 'function') return;
    await window.renderProjectPickerPanel('webshell', {
        listId: 'ws-project-list',
        searchInputId: 'ws-project-search',
        getSelectedId: function () {
            return webshellCurrentConn ? wsResolveWebshellAiProjectSelection(webshellCurrentConn) : '';
        },
        onSelect: function (projectId) { wsSelectProject(projectId); },
        t: wsProjectPickerT,
    });
}

async function wsRenderProjectPanel() {
    if (typeof window.initProjectPickerPanelSearch === 'function') {
        window.initProjectPickerPanelSearch('webshell', 'ws-project-search', function () {
            if (typeof window.scheduleProjectPickerPanelSearch === 'function') {
                window.scheduleProjectPickerPanelSearch('webshell', function () { wsLoadProjectPanelList(); });
            }
        });
    }
    if (typeof window.clearProjectPickerPanelSearch === 'function') {
        window.clearProjectPickerPanelSearch('webshell', 'ws-project-search');
    }
    await wsLoadProjectPanelList();
    requestAnimationFrame(function () { document.getElementById('ws-project-search')?.focus(); });
}

function wsCloseProjectPanel() {
    var panel = document.getElementById('ws-project-panel');
    var btn = document.getElementById('ws-project-btn');
    if (panel) panel.style.display = 'none';
    if (btn) {
        btn.classList.remove('active');
        btn.setAttribute('aria-expanded', 'false');
    }
    if (typeof window.clearProjectPickerPanelSearch === 'function') {
        window.clearProjectPickerPanelSearch('webshell', 'ws-project-search');
    }
}

async function wsToggleProjectPanel() {
    var panel = document.getElementById('ws-project-panel');
    var btn = document.getElementById('ws-project-btn');
    if (!panel) return;
    var isHidden = panel.style.display === 'none' || !panel.style.display;
    if (!isHidden) {
        wsCloseProjectPanel();
        return;
    }
    wsCloseRolePanel();
    wsCloseAgentModePanel();
    panel.style.display = 'flex';
    if (btn) {
        btn.classList.add('active');
        btn.setAttribute('aria-expanded', 'true');
    }
    await wsRenderProjectPanel();
}

async function wsSelectProject(projectId) {
    wsCloseProjectPanel();
    await applyWebshellAiProjectSelection(projectId || '');
}

async function applyWebshellAiProjectSelection(projectId) {
    var conn = webshellCurrentConn;
    if (!conn || !conn.id) return;
    var prev = getWebshellAiProjectSelection(conn);
    if (projectId === prev) {
        wsUpdateProjectButtonLabel();
        return;
    }
    var convId = getWebshellAiConvId(conn);
    if (convId) {
        try {
            var res = await apiFetch('/api/conversations/' + encodeURIComponent(convId) + '/project', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ projectId: projectId }),
            });
            if (!res.ok) {
                var err = await res.json().catch(function () { return {}; });
                throw new Error(err.error || res.statusText);
            }
            wsSetWebshellAiProject(conn, projectId);
            if (typeof showNotification === 'function') {
                showNotification(
                    projectId ? wsProjectT('projects.projectBound', '已绑定项目') : wsProjectT('projects.projectUnbound', '已解除项目绑定'),
                    'success'
                );
            }
        } catch (e) {
            console.error(e);
            alert(wsProjectT('projects.updateProjectBindingFailed', '更新项目绑定失败') + ': ' + (e.message || e));
            wsUpdateProjectButtonLabel();
            return;
        }
    } else {
        wsSetWebshellAiProject(conn, projectId);
    }
    wsUpdateProjectButtonLabel();
}

function showNewProjectModalFromWebshellAi() {
    wsCloseProjectPanel();
    if (webshellCurrentConn && webshellCurrentConn.id) {
        window._projectModalFromWebshellConnId = webshellCurrentConn.id;
    }
    window._projectModalFromChat = false;
    if (typeof showNewProjectModal === 'function') showNewProjectModal();
}

window.applyWebshellAiProjectSelection = applyWebshellAiProjectSelection;
window.showNewProjectModalFromWebshellAi = showNewProjectModalFromWebshellAi;
window.wsToggleProjectPanel = wsToggleProjectPanel;
window.wsCloseProjectPanel = wsCloseProjectPanel;

// ─── end WebShell AI 项目选择器 ───

/** 当 WebShell AI Tab 可见时刷新选择器显示（同步主页可能的更改） */
function wsRefreshSelectors() {
    wsUpdateRoleSelectorDisplay();
    wsRenderRoleList();
    wsUpdateProjectButtonLabel();
    var stored = localStorage.getItem('cyberstrike-chat-agent-mode') || 'eino_single';
    if (stored !== 'eino_single' && stored !== 'deep' && stored !== 'plan_execute' && stored !== 'supervisor') {
        stored = 'eino_single';
    }
    wsSyncAgentMode(stored);
}

// 点击面板外部关闭
document.addEventListener('click', function (e) {
    var rolePanel = document.getElementById('ws-role-selection-panel');
    var roleBtn = document.getElementById('ws-role-selector-btn');
    if (rolePanel && rolePanel.style.display !== 'none' && roleBtn && !rolePanel.contains(e.target) && !roleBtn.contains(e.target)) {
        wsCloseRolePanel();
    }
    var modePanel = document.getElementById('ws-agent-mode-panel');
    var modeBtn = document.getElementById('ws-agent-mode-btn');
    if (modePanel && modePanel.style.display !== 'none' && modeBtn && !modePanel.contains(e.target) && !modeBtn.contains(e.target)) {
        wsCloseAgentModePanel();
    }
    var projectPanel = document.getElementById('ws-project-panel');
    var projectBtn = document.getElementById('ws-project-btn');
    if (projectPanel && projectPanel.style.display !== 'none' && projectBtn && !projectPanel.contains(e.target) && !projectBtn.contains(e.target)) {
        wsCloseProjectPanel();
    }
});

// ─── end WebShell AI 选择器 ───

/** 停止当前 WebShell AI 流式请求 */
function wsStopAiStream(conn) {
    // 1. Abort the fetch
    if (webshellAiAbortController) {
        try { webshellAiAbortController.abort(); } catch (e) { /* */ }
        webshellAiAbortController = null;
    }
    // 2. Cancel the reader
    if (webshellAiStreamReader) {
        try { webshellAiStreamReader.cancel(); } catch (e) { /* */ }
        webshellAiStreamReader = null;
    }
    // 3. Call backend cancel API if we have a conversation
    var convId = conn && conn.id ? (webshellAiConvMap[conn.id] || '') : '';
    if (convId && typeof apiFetch === 'function') {
        apiFetch('/api/agent-loop/cancel', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ conversationId: convId })
        }).catch(function () { /* ignore */ });
    }
    // 4. Reset UI state
    wsSetAiSendingState(false);
}

/** 切换发送/停止按钮状态 */
function wsSetAiSendingState(sending) {
    webshellAiSending = sending;
    var sendBtn = document.getElementById('webshell-ai-send');
    var stopBtn = document.getElementById('webshell-ai-stop');
    if (sendBtn) {
        sendBtn.disabled = sending;
        sendBtn.style.display = sending ? 'none' : '';
    }
    if (stopBtn) {
        stopBtn.style.display = sending ? '' : 'none';
    }
}

// 从服务端（SQLite）拉取连接列表
function getWebshellConnections() {
    if (typeof apiFetch === 'undefined') {
        return Promise.resolve([]);
    }
    var url = '/api/webshell/connections';
    return apiFetch(url, { method: 'GET' })
        .then(function (r) { return r.json(); })
        .then(function (list) { return Array.isArray(list) ? list : []; })
        .catch(function (e) {
            console.warn('读取 WebShell 连接列表失败', e);
            return [];
        });
}

function webshellConnectionProjectId(conn) {
    return (conn && (conn.project_id || conn.projectId) || '').trim();
}

function webshellProjectOptionsHtml(selectedId) {
    var selected = String(selectedId || '').trim();
    var html = '<option value="">' + escapeHtml(wsT('assets.unboundProject') || '暂不绑定') + '</option>';
    var entries = [];
    try {
        if (typeof projectNameById !== 'undefined') entries = Object.entries(projectNameById);
    } catch (e) {}
    entries.sort(function (a, b) {
        return String(a[1] || '').localeCompare(String(b[1] || ''), undefined, { sensitivity: 'base' });
    });
    entries.forEach(function (entry) {
        var id = entry[0];
        var name = entry[1] || id;
        if (!id) return;
        html += '<option value="' + escapeHtml(id) + '"' + (id === selected ? ' selected' : '') + '>' + escapeHtml(name) + '</option>';
    });
    if (selected && !entries.some(function (entry) { return entry[0] === selected; })) {
        html += '<option value="' + escapeHtml(selected) + '" selected>' + escapeHtml(selected) + '</option>';
    }
    return html;
}

var webshellFormSelectMap = {};
var webshellFormSelectDocBound = false;
var WEBSHELL_FORM_SELECT_CARET = '<span class="webshell-form-select-caret" aria-hidden="true"></span>';

function closeAllWebshellFormSelects() {
    Object.keys(webshellFormSelectMap).forEach(function (id) {
        var reg = webshellFormSelectMap[id];
        if (!reg || !reg.wrapper) return;
        reg.wrapper.classList.remove('open');
        if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    });
}

function syncWebshellFormSelect(select) {
    if (!select || !select.id) return;
    var reg = webshellFormSelectMap[select.id];
    if (!reg) return;
    var dropdown = reg.dropdown;
    var trigger = reg.trigger;
    var valueSpan = trigger.querySelector('.webshell-form-select-value');
    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        var item = document.createElement('button');
        item.type = 'button';
        item.className = 'webshell-form-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        item.setAttribute('aria-selected', opt.value === select.value ? 'true' : 'false');
        if (opt.value === select.value) item.classList.add('is-selected');

        var check = document.createElement('span');
        check.className = 'webshell-form-select-check';
        check.textContent = '✓';
        check.setAttribute('aria-hidden', 'true');

        var label = document.createElement('span');
        label.className = 'webshell-form-select-label';
        label.textContent = opt.textContent;

        item.appendChild(check);
        item.appendChild(label);
        dropdown.appendChild(item);
    });
    var selectedOpt = select.options[select.selectedIndex];
    if (valueSpan) valueSpan.textContent = selectedOpt ? selectedOpt.textContent : '';
    trigger.disabled = !!select.disabled;
    reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
}

function enhanceWebshellFormSelect(select) {
    if (!select || !select.id) return;
    var existing = webshellFormSelectMap[select.id];
    if (existing && existing.select !== select) delete webshellFormSelectMap[select.id];
    if (select.dataset.webshellFormCustom === '1') {
        syncWebshellFormSelect(select);
        return;
    }

    select.dataset.webshellFormCustom = '1';
    select.classList.add('webshell-form-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    var wrapper = document.createElement('div');
    wrapper.className = 'webshell-form-select-ui';

    var trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'webshell-form-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');

    var valueSpan = document.createElement('span');
    valueSpan.className = 'webshell-form-select-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', WEBSHELL_FORM_SELECT_CARET);

    var dropdown = document.createElement('div');
    dropdown.className = 'webshell-form-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    var parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    webshellFormSelectMap[select.id] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        var open = wrapper.classList.contains('open');
        closeAllWebshellFormSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
        }
    });

    dropdown.addEventListener('click', function (e) {
        var item = e.target.closest('.webshell-form-select-option');
        if (!item) return;
        e.stopPropagation();
        var value = item.getAttribute('data-value');
        if (value === null) return;
        if (select.value !== value) {
            select.value = value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        wrapper.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');
        syncWebshellFormSelect(select);
    });

    select.addEventListener('change', function () {
        syncWebshellFormSelect(select);
    });

    syncWebshellFormSelect(select);
}

function refreshWebshellFormSelects(root) {
    var container = root || document.getElementById('webshell-modal');
    if (!container) return;
    Object.keys(webshellFormSelectMap).forEach(function (id) {
        if (!document.getElementById(id)) delete webshellFormSelectMap[id];
    });
    container.querySelectorAll('select').forEach(enhanceWebshellFormSelect);
    if (!webshellFormSelectDocBound) {
        webshellFormSelectDocBound = true;
        document.addEventListener('click', closeAllWebshellFormSelects);
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllWebshellFormSelects();
        });
    }
}

function populateWebshellProjectSelect(selectedId) {
    var sel = document.getElementById('webshell-project-id');
    if (!sel) return Promise.resolve();
    var selected = String(selectedId || '').trim();
    sel.innerHTML = webshellProjectOptionsHtml(selected);
    sel.value = selected;
    syncWebshellFormSelect(sel);
    var loadPromise = Promise.resolve([]);
    if (typeof ensureProjectsLoaded === 'function') {
        loadPromise = ensureProjectsLoaded();
    } else if (typeof fetchAllProjects === 'function') {
        loadPromise = fetchAllProjects(false).then(function (list) {
            if (typeof rebuildProjectNameMap === 'function') rebuildProjectNameMap(list || []);
            return list || [];
        });
    }
    return loadPromise.then(function () {
        sel.innerHTML = webshellProjectOptionsHtml(selected);
        sel.value = selected;
        syncWebshellFormSelect(sel);
    }).catch(function (e) {
        console.warn('加载 WebShell 项目选项失败', e);
    });
}

// 从服务端刷新连接列表并重绘侧栏
function refreshWebshellConnectionsFromServer() {
    return getWebshellConnections().then(function (list) {
        webshellConnections = list;
        renderWebshellList();
        if (typeof ensureProjectsLoaded === 'function') {
            ensureProjectsLoaded().then(function () {
                renderWebshellList();
            }).catch(function () {});
        }
        return list;
    });
}

// 使用 wsT 避免与全局 window.t 冲突导致无限递归
function wsT(key) {
    var globalT = typeof window !== 'undefined' ? window.t : null;
    if (typeof globalT === 'function' && globalT !== wsT) return globalT(key);
    var fallback = {
        'webshell.title': 'WebShell 管理',
        'webshell.addConnection': '添加连接',
        'webshell.cmdParam': '命令参数名',
        'webshell.cmdParamPlaceholder': '不填默认为 cmd，如填 xxx 则请求为 xxx=命令',
        'webshell.encoding': '响应编码',
        'webshell.encodingAuto': '自动检测',
        'webshell.encodingUtf8': 'UTF-8',
        'webshell.encodingGbk': 'GBK（中文 Windows）',
        'webshell.encodingGb18030': 'GB18030',
        'webshell.encodingHint': '中文 Windows 目标若出现乱码，请切换为 GBK 或 GB18030',
        'webshell.os': '目标系统',
        'webshell.osAuto': '自动（按 Shell 类型推断）',
        'webshell.osLinux': 'Linux / Unix',
        'webshell.osWindows': 'Windows',
        'webshell.osHint': '决定文件管理/上传使用 Linux 还是 Windows 命令；PHP/JSP 跑在 Windows 上请选 Windows',
        'webshell.connections': '连接列表',
        'webshell.noConnections': '暂无连接，请点击「添加连接」',
        'webshell.selectOrAdd': '请从左侧选择连接，或添加新的 WebShell 连接',
        'webshell.deleteConfirm': '确定要删除该连接吗？',
        'webshell.editConnection': '编辑',
        'webshell.editConnectionTitle': '编辑连接',
        'webshell.tabTerminal': '虚拟终端',
        'webshell.tabFileManager': '文件管理',
        'webshell.tabAiAssistant': 'AI 助手',
        'webshell.tabDbManager': '数据库管理',
        'webshell.tabMemo': '备忘录',
        'webshell.dbType': '数据库类型',
        'webshell.dbHost': '主机',
        'webshell.dbPort': '端口',
        'webshell.dbUsername': '用户名',
        'webshell.dbPassword': '密码',
        'webshell.dbName': '数据库名',
        'webshell.dbSqlitePath': 'SQLite 文件路径',
        'webshell.dbSqlPlaceholder': '输入 SQL，例如：SELECT version();',
        'webshell.dbRunSql': '执行 SQL',
        'webshell.dbTest': '测试连接',
        'webshell.dbOutput': '执行输出',
        'webshell.dbNoConn': '请先选择 WebShell 连接',
        'webshell.dbSqlRequired': '请输入 SQL',
        'webshell.dbRunning': '数据库命令执行中，请稍候',
        'webshell.dbCliHint': '如果提示命令不存在，请先在目标主机安装对应客户端（mysql/psql/sqlite3/sqlcmd）',
        'webshell.dbExecFailed': '数据库执行失败',
        'webshell.dbSchema': '数据库结构',
        'webshell.dbLoadSchema': '加载结构',
        'webshell.dbNoSchema': '暂无数据库结构，请先加载',
        'webshell.dbSelectTableHint': '点击表名可展开列信息并生成查询 SQL',
        'webshell.dbNoColumns': '暂无列信息',
        'webshell.dbResultTable': '结果表格',
        'webshell.dbClearSql': '清空 SQL',
        'webshell.dbTemplateSql': '示例 SQL',
        'webshell.dbRows': '行',
        'webshell.dbColumns': '列',
        'webshell.dbSchemaFailed': '加载数据库结构失败',
        'webshell.dbSchemaLoaded': '结构加载完成',
        'webshell.dbAddProfile': '新增连接',
        'webshell.dbExecSuccess': 'SQL 执行成功',
        'webshell.dbNoOutput': '执行完成（无输出）',
        'webshell.dbRenameProfile': '重命名',
        'webshell.dbDeleteProfile': '删除连接',
        'webshell.dbDeleteProfileConfirm': '确定删除该数据库连接配置吗？',
        'webshell.dbProfileNamePrompt': '请输入连接名称',
        'webshell.dbProfileName': '连接名称',
        'webshell.dbProfiles': '数据库连接',
        'webshell.aiSystemReadyMessage': '系统已就绪。请输入您的测试需求，系统将自动执行相应的安全测试。',
        'webshell.aiPlaceholder': '例如：列出当前目录下的文件',
        'webshell.aiSend': '发送',
        'webshell.aiMemo': '备忘录',
        'webshell.aiMemoPlaceholder': '记录关键命令、测试思路、复现步骤...',
        'webshell.aiMemoClear': '清空',
        'webshell.aiMemoSaving': '保存中...',
        'webshell.aiMemoSaved': '已保存到本地',
        'webshell.terminalWelcome': 'WebShell 虚拟终端 — 输入命令后按回车执行（Ctrl+L 清屏）',
        'webshell.quickCommands': '快捷命令',
        'webshell.downloadFile': '下载',
        'webshell.filePath': '当前路径',
        'webshell.listDir': '列出目录',
        'webshell.readFile': '读取',
        'webshell.editFile': '编辑',
        'webshell.deleteFile': '删除',
        'webshell.saveFile': '保存',
        'webshell.cancelEdit': '取消',
        'webshell.parentDir': '上级目录',
        'webshell.execError': '执行失败',
        'webshell.testConnectivity': '测试连通性',
        'webshell.testSuccess': '连通性正常，Shell 可访问',
        'webshell.testFailed': '连通性测试失败',
        'webshell.testNoExpectedOutput': 'Shell 返回了响应但未得到预期输出，请检查连接密码与命令参数名',
        'webshell.clearScreen': '清屏',
        'webshell.copyTerminalLog': '复制日志',
        'webshell.terminalIdle': '空闲',
        'webshell.terminalRunning': '执行中',
        'webshell.terminalCopyOk': '日志已复制',
        'webshell.terminalCopyFail': '复制失败',
        'webshell.terminalNewWindow': '新终端',
        'webshell.terminalWindowPrefix': '终端',
        'webshell.running': '执行中…',
        'webshell.waitFinish': '请等待当前命令执行完成',
        'webshell.newDir': '新建目录',
        'webshell.rename': '重命名',
        'webshell.upload': '上传',
        'webshell.newFile': '新建文件',
        'webshell.filterPlaceholder': '过滤文件名',
        'webshell.batchDelete': '批量删除',
        'webshell.batchDownload': '批量下载',
        'webshell.moreActions': '更多操作',
        'webshell.refresh': '刷新',
        'webshell.selectAll': '全选',
        'webshell.breadcrumbHome': '根',
        'webshell.dirTree': '目录列表',
        'webshell.searchPlaceholder': '搜索连接...',
        'webshell.noMatchConnections': '暂无匹配连接',
        'webshell.batchProbe': '一键批量探活',
        'webshell.probeRunning': '探活中',
        'webshell.probeOnline': '在线',
        'webshell.probeOffline': '离线',
        'webshell.probeNoConnections': '暂无可探活连接',
        'webshell.back': '返回',
        'webshell.colModifiedAt': '修改时间',
        'webshell.colPerms': '权限',
        'webshell.colOwner': '所有者',
        'webshell.colGroup': '用户组',
        'webshell.colType': '类型',
        'common.delete': '删除',
        'common.refresh': '刷新',
        'common.actions': '操作'
    };
    return fallback[key] || key;
}

function wsTOr(key, fallbackText) {
    var text = wsT(key);
    if (!text || text === key) return fallbackText;
    return text;
}

// 全局只绑定一次：清屏 = 销毁终端并重新创建，保证只出现一个 shell>（不依赖 xterm.clear()，避免某些环境下 clear 不生效或重复写入）
function bindWebshellClearOnce() {
    if (window._webshellClearBound) return;
    window._webshellClearBound = true;
    document.body.addEventListener('click', function (e) {
        var btn = e.target && (e.target.id === 'webshell-terminal-clear' ? e.target : e.target.closest ? e.target.closest('#webshell-terminal-clear') : null);
        if (!btn || !webshellCurrentConn) return;
        e.preventDefault();
        e.stopPropagation();
        if (webshellClearInProgress) return;
        webshellClearInProgress = true;
        try {
            destroyWebshellTerminal();
            webshellLineBuffer = '';
            webshellHistoryIndex = -1;
            if (webshellCurrentConn && webshellCurrentConn.id) {
                var sid = getActiveWebshellTerminalSessionId(webshellCurrentConn.id);
                clearWebshellTerminalLog(getWebshellTerminalSessionKey(webshellCurrentConn.id, sid));
            }
            initWebshellTerminal(webshellCurrentConn);
        } finally {
            setTimeout(function () { webshellClearInProgress = false; }, 100);
        }
    }, true);
}

// WebShell 行内/工具栏“操作”下拉：点击菜单外自动收起
function bindWebshellActionMenusAutoCloseOnce() {
    if (window._webshellActionMenusAutoCloseBound) return;
    window._webshellActionMenusAutoCloseBound = true;
    document.addEventListener('click', function (e) {
        // 只要点在 details 内部，就让浏览器自行切换（open/close）
        var clickedInMenu = e.target && e.target.closest && (
            e.target.closest('details.webshell-conn-actions') ||
            e.target.closest('details.webshell-row-actions') ||
            e.target.closest('details.webshell-toolbar-actions')
        );
        if (clickedInMenu) return;

        var openDetails = document.querySelectorAll(
            'details.webshell-conn-actions[open],details.webshell-row-actions[open],details.webshell-toolbar-actions[open]'
        );
        openDetails.forEach(function (d) { d.open = false; });
    }, true);
}

// 初始化 WebShell 管理页面（从 SQLite 拉取连接列表）
function initWebshellPage() {
    bindWebshellClearOnce();
    bindWebshellActionMenusAutoCloseOnce();
    destroyWebshellTerminal();
    webshellCurrentConn = null;
    currentWebshellId = null;
    webshellConnections = [];
    renderWebshellList();
    applyWebshellSidebarWidth();
    initWebshellSidebarResize();

    // 连接搜索：实时过滤连接列表
    var searchEl = document.getElementById('webshell-conn-search');
    if (searchEl && searchEl.dataset.bound !== '1') {
        searchEl.dataset.bound = '1';
        searchEl.addEventListener('input', function () {
            renderWebshellList();
        });
    }

    const workspace = document.getElementById('webshell-workspace');
    if (workspace) {
        workspace.innerHTML = '<div class="webshell-workspace-placeholder" data-i18n="webshell.selectOrAdd">' + (wsT('webshell.selectOrAdd')) + '</div>';
    }
    getWebshellConnections().then(function (list) {
        webshellConnections = list;
        renderWebshellList();
    });

    var batchProbeBtn = document.getElementById('webshell-batch-probe-btn');
    if (batchProbeBtn && batchProbeBtn.dataset.bound !== '1') {
        batchProbeBtn.dataset.bound = '1';
        batchProbeBtn.addEventListener('click', function () {
            runBatchProbeWebshellConnections();
        });
    }
    updateWebshellBatchProbeButton();
}

function getWebshellSidebarWidth() {
    try {
        const w = parseInt(localStorage.getItem(WEBSHELL_SIDEBAR_WIDTH_KEY), 10);
        if (!isNaN(w) && w >= 260 && w <= 800) return w;
    } catch (e) {}
    return WEBSHELL_DEFAULT_SIDEBAR_WIDTH;
}

function setWebshellSidebarWidth(px) {
    localStorage.setItem(WEBSHELL_SIDEBAR_WIDTH_KEY, String(px));
}

function applyWebshellSidebarWidth() {
    const sidebar = document.getElementById('webshell-sidebar');
    if (!sidebar) return;
    const parentW = sidebar.parentElement ? sidebar.parentElement.offsetWidth : 0;
    let w = getWebshellSidebarWidth();
    if (parentW > 0) w = Math.min(w, Math.max(260, parentW - WEBSHELL_MAIN_MIN_WIDTH));
    sidebar.style.width = w + 'px';
}

function initWebshellSidebarResize() {
    const handle = document.getElementById('webshell-resize-handle');
    const sidebar = document.getElementById('webshell-sidebar');
    if (!handle || !sidebar || handle.dataset.resizeBound === '1') return;
    handle.dataset.resizeBound = '1';
    let startX = 0, startW = 0;
    function onMove(e) {
        const dx = e.clientX - startX;
        let w = Math.round(startW + dx);
        const parentW = sidebar.parentElement ? sidebar.parentElement.offsetWidth : 800;
        const min = 260;
        const max = Math.min(800, parentW - WEBSHELL_MAIN_MIN_WIDTH);
        w = Math.max(min, Math.min(max, w));
        sidebar.style.width = w + 'px';
    }
    function onUp() {
        handle.classList.remove('active');
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        setWebshellSidebarWidth(parseInt(sidebar.style.width, 10) || WEBSHELL_DEFAULT_SIDEBAR_WIDTH);
    }
    handle.addEventListener('mousedown', function (e) {
        if (e.button !== 0) return;
        e.preventDefault();
        startX = e.clientX;
        startW = sidebar.offsetWidth;
        handle.classList.add('active');
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
    });
}

// 销毁当前终端实例（切换连接或离开页面时）
function destroyWebshellTerminal() {
    if (webshellTerminalResizeObserver && webshellTerminalResizeContainer) {
        try { webshellTerminalResizeObserver.unobserve(webshellTerminalResizeContainer); } catch (e) {}
        webshellTerminalResizeObserver = null;
        webshellTerminalResizeContainer = null;
    }
    if (webshellTerminalInstance) {
        try {
            webshellTerminalInstance.dispose();
        } catch (e) {}
        webshellTerminalInstance = null;
    }
    webshellTerminalFitAddon = null;
    webshellLineBuffer = '';
    webshellRunning = false;
    webshellTerminalRunning = false;
    setWebshellTerminalStatus(false);
}

// 渲染连接列表
function renderWebshellList() {
    const listEl = document.getElementById('webshell-list');
    if (!listEl) return;

    const searchEl = document.getElementById('webshell-conn-search');
    const searchTerm = (searchEl && typeof searchEl.value === 'string' ? searchEl.value : '').trim().toLowerCase();

    if (!webshellConnections.length) {
        listEl.innerHTML = '<div class="webshell-empty" data-i18n="webshell.noConnections">' + (wsT('webshell.noConnections')) + '</div>';
        return;
    }

    const filtered = searchTerm
        ? webshellConnections.filter(conn => {
            const id = String(conn.id || '').toLowerCase();
            const url = String(conn.url || '').toLowerCase();
            const remark = String(conn.remark || '').toLowerCase();
            return id.includes(searchTerm) || url.includes(searchTerm) || remark.includes(searchTerm);
        })
        : webshellConnections;

    if (filtered.length === 0) {
        listEl.innerHTML = '<div class="webshell-empty">' + (wsT('webshell.noMatchConnections') || '暂无匹配连接') + '</div>';
        return;
    }

    listEl.innerHTML = filtered.map(conn => {
        const remark = (conn.remark || conn.url || '').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        const url = (conn.url || '').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        const urlTitle = (conn.url || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
        const active = currentWebshellId === conn.id ? ' active' : '';
        const safeId = escapeHtml(conn.id);
        const actionsLabel = wsT('common.actions') || '操作';
        const probe = webshellProbeStatusById[conn.id] || null;
        var probeHtml = '';
        if (probe && probe.state === 'probing') {
            probeHtml = '<span class="webshell-probe-badge probing">' + (wsT('webshell.probeRunning') || '探活中') + '</span>';
        } else if (probe && probe.state === 'ok') {
            probeHtml = '<span class="webshell-probe-badge ok">' + (wsT('webshell.probeOnline') || '在线') + '</span>';
        } else if (probe && probe.state === 'fail') {
            probeHtml = '<span class="webshell-probe-badge fail" title="' + escapeHtml(probe.message || '') + '">' + (wsT('webshell.probeOffline') || '离线') + '</span>';
        }
        var encNorm = normalizeWebshellEncoding(conn.encoding);
        var encHtml = '';
        if (encNorm && encNorm !== 'auto') {
            encHtml = '<span class="webshell-probe-badge" title="' + escapeHtml(wsT('webshell.encoding') || '响应编码') + '">' + escapeHtml(encNorm.toUpperCase()) + '</span>';
        }
        var osNorm = normalizeWebshellOS(conn.os);
        var osHtml = '';
        if (osNorm && osNorm !== 'auto') {
            var osLabel = osNorm === 'windows' ? 'WIN' : 'LINUX';
            osHtml = '<span class="webshell-probe-badge" title="' + escapeHtml(wsT('webshell.os') || '目标系统') + '">' + osLabel + '</span>';
        }
        return (
            '<div class="webshell-item' + active + '" data-id="' + safeId + '">' +
            '<div class="webshell-item-remark-row"><div class="webshell-item-remark" title="' + urlTitle + '">' + remark + '</div>' + probeHtml + osHtml + encHtml + '</div>' +
            '<div class="webshell-item-url" title="' + urlTitle + '">' + url + '</div>' +
            '<div class="webshell-item-actions">' +
            '<details class="webshell-conn-actions"><summary class="btn-ghost btn-sm webshell-conn-actions-btn" title="' + actionsLabel + '">' + actionsLabel + '</summary>' +
            '<div class="webshell-row-actions-menu">' +
            '<button type="button" class="btn-ghost btn-sm webshell-edit-conn-btn" data-id="' + safeId + '" title="' + wsT('webshell.editConnection') + '">' + wsT('webshell.editConnection') + '</button>' +
            '<button type="button" class="btn-ghost btn-sm webshell-delete-btn" data-id="' + safeId + '" title="' + wsT('common.delete') + '">' + wsT('common.delete') + '</button>' +
            '</div></details>' +
            '</div>' +
            '</div>'
        );
    }).join('');

    listEl.querySelectorAll('.webshell-item').forEach(el => {
        el.addEventListener('click', function (e) {
            if (e.target.closest('.webshell-delete-btn') || e.target.closest('.webshell-edit-conn-btn') || e.target.closest('.webshell-conn-actions-btn')) return;
            selectWebshell(el.getAttribute('data-id'));
        });
    });
    listEl.querySelectorAll('.webshell-edit-conn-btn').forEach(btn => {
        btn.addEventListener('click', function (e) {
            e.stopPropagation();
            showEditWebshellModal(btn.getAttribute('data-id'));
        });
    });
    listEl.querySelectorAll('.webshell-delete-btn').forEach(btn => {
        btn.addEventListener('click', function (e) {
            e.stopPropagation();
            deleteWebshell(btn.getAttribute('data-id'));
        });
    });
}

function probeWebshellConnection(conn) {
    if (!conn || typeof apiFetch === 'undefined') {
        return Promise.resolve({ ok: false, message: wsT('webshell.testFailed') || '连通性测试失败' });
    }
    var probeToken = buildWebshellProbeToken();
    return apiFetch('/api/webshell/exec', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            url: conn.url,
            password: conn.password || '',
            type: conn.type || 'php',
            method: ((conn.method || 'post').toLowerCase() === 'get') ? 'get' : 'post',
            cmd_param: conn.cmdParam || '',
            encoding: webshellConnEncoding(conn),
            os: webshellConnOS(conn),
            connection_id: conn.id || '',
            command: buildWebshellProbeCommand(probeToken),
            connection_id: password === '********' ? (document.getElementById('webshell-edit-id')?.value || '') : ''
        })
    })
        .then(function (r) { return r.json(); })
        .then(function (data) {
            var output = (data && data.output != null) ? String(data.output) : '';
            var ok = !!(data && data.ok && isWebshellProbeOutputMatched(output, probeToken));
            if (ok) return { ok: true, message: wsT('webshell.testSuccess') || '连通性正常，Shell 可访问' };
            var msg = (data && data.error) ? data.error : (wsT('webshell.testFailed') || '连通性测试失败');
            return { ok: false, message: msg };
        })
        .catch(function (e) {
            return { ok: false, message: (e && e.message) ? e.message : String(e) };
        });
}

function updateWebshellBatchProbeButton(done, total, okCount) {
    var btn = document.getElementById('webshell-batch-probe-btn');
    if (!btn) return;
    if (webshellBatchProbeRunning) {
        var d = typeof done === 'number' ? done : 0;
        var t = typeof total === 'number' ? total : webshellConnections.length;
        btn.disabled = true;
        btn.textContent = (wsT('webshell.probeRunning') || '探活中') + ' ' + d + '/' + t;
        return;
    }
    btn.disabled = false;
    if (typeof done === 'number' && typeof total === 'number' && total > 0 && typeof okCount === 'number') {
        btn.textContent = (wsT('webshell.batchProbe') || '一键批量探活') + ' (' + okCount + '/' + total + ')';
    } else {
        btn.textContent = wsT('webshell.batchProbe') || '一键批量探活';
    }
}

function runBatchProbeWebshellConnections() {
    if (webshellBatchProbeRunning) return;
    if (!Array.isArray(webshellConnections) || webshellConnections.length === 0) {
        alert(wsT('webshell.probeNoConnections') || '暂无可探活连接');
        return;
    }
    webshellBatchProbeRunning = true;
    var total = webshellConnections.length;
    var done = 0;
    var okCount = 0;

    webshellConnections.forEach(function (conn) {
        if (!conn || !conn.id) return;
        webshellProbeStatusById[conn.id] = { state: 'probing', message: '' };
    });
    renderWebshellList();
    updateWebshellBatchProbeButton(done, total, okCount);

    var idx = 0;
    var concurrency = Math.min(4, total);

    function runOne() {
        if (idx >= total) return Promise.resolve();
        var conn = webshellConnections[idx++];
        if (!conn || !conn.id) {
            done++;
            updateWebshellBatchProbeButton(done, total, okCount);
            return runOne();
        }
        return probeWebshellConnection(conn).then(function (res) {
            if (res.ok) okCount++;
            webshellProbeStatusById[conn.id] = {
                state: res.ok ? 'ok' : 'fail',
                message: res.message || ''
            };
            done++;
            renderWebshellList();
            updateWebshellBatchProbeButton(done, total, okCount);
        }).then(runOne);
    }

    var workers = [];
    for (var i = 0; i < concurrency; i++) workers.push(runOne());
    Promise.all(workers).finally(function () {
        webshellBatchProbeRunning = false;
        updateWebshellBatchProbeButton(done, total, okCount);
    });
}

function escapeHtml(s) {
    if (!s) return '';
    const div = document.createElement('div');
    div.textContent = s;
    return div.innerHTML;
}

function escapeHtmlAttr(s) {
    return escapeHtml(s).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function webshellFinalizationReasonLabel(reason, status) {
    var key = String(reason || status || '').trim();
    var labels = {
        pending_tool_executions: wsTOr('webshell.finalizationPendingToolExecutions', '等待工具执行完成'),
        missing_execution_evidence: wsTOr('webshell.finalizationMissingEvidence', '缺少完成态证据'),
        awaiting_hitl: wsTOr('webshell.finalizationAwaitingHitl', '等待人工确认'),
        empty_response: wsTOr('webshell.finalizationEmptyResponse', '未捕获到有效回复'),
        missing_finalization_contract: wsTOr('webshell.finalizationMissingContract', '缺少最终化证明'),
        in_progress: wsTOr('webshell.finalizationInProgress', '仍在验证'),
        blocked: wsTOr('webshell.finalizationBlocked', '检查未通过'),
        failed: wsTOr('webshell.finalizationFailed', '任务失败'),
        cancelled: wsTOr('chat.taskCancelled', '任务已取消'),
        verified: wsTOr('webshell.finalizationVerified', '已验证')
    };
    return labels[key] || key || wsTOr('webshell.finalizationBlocked', '检查未通过');
}

function webshellFinalizationMissingCheckLabel(check) {
    var s = String(check || '').trim();
    if (!s) return '';
    if (s.indexOf('tool execution still queued or running') !== -1) return wsTOr('webshell.finalizationToolStillRunning', '仍有工具执行未结束');
    if (s.indexOf('execution evidence is required but no completed tool execution was recorded') !== -1) return wsTOr('webshell.finalizationEvidenceRequired', '本轮要求执行证据，但没有 completed 工具记录');
    if (s.indexOf('workflow is awaiting HITL approval') !== -1) return wsTOr('webshell.finalizationAwaitingHitlApproval', '工作流正在等待人工确认');
    if (s.indexOf('assistant final text is empty') !== -1) return wsTOr('webshell.finalizationEmptyFinalText', '未捕获到有效最终文本');
    if (s.indexOf('agent run status is ') === 0) {
        var statusText = s.replace('agent run status is ', '');
        return (typeof window.t === 'function' ? window.t('webshell.finalizationStatusStill', { status: statusText }) : ('任务状态仍为 ' + statusText));
    }
    return s;
}

function webshellFinalizationNotice(data, eventMessage, hasContract) {
    var reason = hasContract
        ? webshellFinalizationReasonLabel(data && data.completionReason, data && data.status)
        : webshellFinalizationReasonLabel('missing_finalization_contract');
    var lines = [wsTOr('webshell.finalizationNoticeHeader', '仍在验证，暂不生成最终结论'), (typeof window.t === 'function' ? window.t('webshell.finalizationNoticeStatus', { reason: reason }) : ('状态：' + reason))];
    var pending = Array.isArray(data && data.pendingExecutionIds) ? data.pendingExecutionIds.filter(Boolean).map(String) : [];
    if (pending.length) {
        lines.push((typeof window.t === 'function' ? window.t('webshell.finalizationPendingTools', { tools: pending.slice(0, 3).join(', ') + (pending.length > 3 ? ' 等' : '') }) : ('待完成工具：' + pending.slice(0, 3).join(', ') + (pending.length > 3 ? ' 等' : ''))));
    }
    var missing = Array.isArray(data && data.missingChecks)
        ? data.missingChecks.map(webshellFinalizationMissingCheckLabel).filter(Boolean)
        : [];
    if (missing.length) {
        lines.push((typeof window.t === 'function' ? window.t('webshell.finalizationPendingChecks', { checks: missing.slice(0, 2).join('；') + (missing.length > 2 ? ' 等' : '') }) : ('待完成检查：' + missing.slice(0, 2).join('；') + (missing.length > 2 ? ' 等' : ''))));
    }
    if (!hasContract && eventMessage) {
        lines.push(wsTOr('webshell.finalizationCandidateMoved', '候选输出已移入过程详情。'));
    }
    return lines.join('\n');
}

function escapeSingleQuotedShellArg(value) {
    var s = value == null ? '' : String(value);
    return "'" + s.replace(/'/g, "'\\''") + "'";
}

function safeConnIdForStorage(conn) {
    if (!conn || !conn.id) return '';
    return String(conn.id).replace(/[^\w.-]/g, '_');
}

function normalizeWebshellPath(path) {
    var p = path == null ? '.' : String(path).trim();
    if (!p) return '.';
    p = p.replace(/\\/g, '/').replace(/\/+/g, '/');
    if (p === '/') return '/';
    // Windows 盘符根目录保持为 "C:/"，避免被裁成 "C:" 后父级计算异常
    if (/^[A-Za-z]:\/?$/.test(p)) {
        return p.slice(0, 2) + '/';
    }
    if (!p || p === '.') return '.';
    if (p.endsWith('/')) p = p.slice(0, -1);
    return p || '.';
}

function getWebshellSelectedFile(conn) {
    if (!conn || !conn.id) return '';
    var p = webshellSelectedFileByConn[conn.id];
    if (!p) return '';
    return normalizeWebshellPath(p);
}

function setWebshellSelectedFile(conn, path) {
    if (!conn || !conn.id) return;
    if (!path) {
        delete webshellSelectedFileByConn[conn.id];
        return;
    }
    webshellSelectedFileByConn[conn.id] = normalizeWebshellPath(path);
}

function getWebshellParentPath(path) {
    var p = normalizeWebshellPath(path);
    if (p === '/') return '/';
    // Windows 盘符根目录不可再上探
    if (/^[A-Za-z]:\/$/.test(p)) return p;
    // 允许从当前目录持续上探：. -> .. -> ../.. -> ../../..
    if (p === '.') return '..';
    if (/^(?:\.\.\/)*\.\.$/.test(p)) return p + '/..';
    // 已经是相对上探时，先维持链路；后续 list 成功后会用远端真实路径回填
    var idx = p.lastIndexOf('/');
    if (idx < 0) return '.';
    var parent = p.slice(0, idx) || (p.startsWith('/') ? '/' : '.');
    if (/^[A-Za-z]:$/.test(parent)) return parent + '/';
    return parent;
}

function inferPathFromWindowsDirOutput(rawOutput) {
    var text = String(rawOutput || '').replace(/\r/g, '');
    var lines = text.split('\n');
    for (var i = 0; i < lines.length; i++) {
        var line = String(lines[i] || '').trim();
        // 中文: C:\xxx 的目录
        var zh = line.match(/^([A-Za-z]:\\.*)\s+的目录$/);
        if (zh && zh[1]) return normalizeWebshellPath(zh[1]);
        // 英文: Directory of C:\xxx
        var en = line.match(/^Directory of\s+([A-Za-z]:\\.*)$/i);
        if (en && en[1]) return normalizeWebshellPath(en[1]);
    }
    return '';
}

function getWebshellTerminalSessionKey(connId, sessionId) {
    if (!connId || !sessionId) return '';
    return String(connId) + '::' + String(sessionId);
}

function normalizeWebshellTerminalSessions(raw) {
    var state = raw && typeof raw === 'object' ? raw : {};
    var list = Array.isArray(state.sessions) ? state.sessions.slice() : [];
    if (!list.length) {
        list = [{ id: 't1', name: (wsT('webshell.terminalWindowPrefix') || '终端') + '1' }];
    }
    list = list.map(function (s, i) {
        var id = (s && s.id ? String(s.id) : ('t' + (i + 1)));
        var name = (s && s.name ? String(s.name) : ((wsT('webshell.terminalWindowPrefix') || '终端') + (i + 1)));
        return { id: id, name: name };
    });
    var activeId = state.activeId;
    if (!activeId || !list.some(function (s) { return s.id === activeId; })) activeId = list[0].id;
    return { sessions: list, activeId: activeId };
}

function getWebshellTerminalSessions(connId) {
    if (!connId) return normalizeWebshellTerminalSessions(null);
    if (webshellTerminalSessionsByConn[connId]) return webshellTerminalSessionsByConn[connId];
    var state = normalizeWebshellTerminalSessions(null);
    webshellTerminalSessionsByConn[connId] = state;
    return state;
}

function saveWebshellTerminalSessions(connId, state) {
    if (!connId || !state) return;
    var normalized = normalizeWebshellTerminalSessions(state);
    webshellTerminalSessionsByConn[connId] = normalized;
    queueWebshellPersistStateSave(connId);
}

function getActiveWebshellTerminalSessionId(connId) {
    return getWebshellTerminalSessions(connId).activeId;
}

function getWebshellTerminalLog(connId) {
    if (!connId) return '';
    if (typeof webshellTerminalLogsByConn[connId] === 'string') return webshellTerminalLogsByConn[connId];
    webshellTerminalLogsByConn[connId] = '';
    return '';
}

function saveWebshellTerminalLog(connId, content) {
    if (!connId) return;
    var text = String(content || '');
    var maxLen = 50000; // keep recent terminal output only
    if (text.length > maxLen) text = text.slice(text.length - maxLen);
    webshellTerminalLogsByConn[connId] = text;
}

function appendWebshellTerminalLog(connId, chunk) {
    if (!connId || !chunk) return;
    var current = getWebshellTerminalLog(connId);
    saveWebshellTerminalLog(connId, current + String(chunk));
}

function clearWebshellTerminalLog(connId) {
    if (!connId) return;
    webshellTerminalLogsByConn[connId] = '';
}

function buildWebshellPersistState(connId) {
    var dbState = getWebshellDbState({ id: connId });
    var terminalSessions = getWebshellTerminalSessions(connId);
    return {
        dbState: dbState || null,
        terminalSessions: terminalSessions || null
    };
}

function applyWebshellPersistState(connId, state) {
    if (!connId || !state || typeof state !== 'object') return;
    if (state.dbState && typeof state.dbState === 'object') {
        var key = getWebshellDbStateStorageKey({ id: connId });
        webshellDbConfigByConn[key] = normalizeWebshellDbState(state.dbState);
    }
    if (state.terminalSessions && typeof state.terminalSessions === 'object') {
        webshellTerminalSessionsByConn[connId] = normalizeWebshellTerminalSessions(state.terminalSessions);
    }
}

function queueWebshellPersistStateSave(connId) {
    if (!connId || typeof apiFetch !== 'function') return;
    if (webshellPersistSaveTimersByConn[connId]) clearTimeout(webshellPersistSaveTimersByConn[connId]);
    webshellPersistSaveTimersByConn[connId] = setTimeout(function () {
        delete webshellPersistSaveTimersByConn[connId];
        var payload = buildWebshellPersistState(connId);
        apiFetch('/api/webshell/connections/' + encodeURIComponent(connId) + '/state', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ state: payload })
        }).catch(function () {});
    }, 500);
}

function ensureWebshellPersistStateLoaded(conn) {
    if (!conn || !conn.id || typeof apiFetch !== 'function') return Promise.resolve();
    if (webshellPersistLoadedByConn[conn.id]) return Promise.resolve();
    return apiFetch('/api/webshell/connections/' + encodeURIComponent(conn.id) + '/state', { method: 'GET' })
        .then(function (r) { return r.ok ? r.json() : Promise.reject(new Error('load state failed')); })
        .then(function (data) {
            applyWebshellPersistState(conn.id, data && data.state ? data.state : {});
            webshellPersistLoadedByConn[conn.id] = true;
        })
        .catch(function () {
            webshellPersistLoadedByConn[conn.id] = true;
        });
}

function setWebshellTerminalStatus(running) {
    webshellTerminalRunning = !!running;
    var el = document.getElementById('webshell-terminal-status');
    if (!el) return;
    el.classList.toggle('running', !!running);
    el.classList.toggle('idle', !running);
    el.textContent = running ? (wsT('webshell.terminalRunning') || '执行中') : (wsT('webshell.terminalIdle') || '空闲');
}

function renderWebshellTerminalSessions(conn) {
    if (!conn || !conn.id) return;
    var tabsEl = document.getElementById('webshell-terminal-sessions');
    if (!tabsEl) return;
    var connId = conn.id;
    var state = getWebshellTerminalSessions(connId);
    var html = '';
    state.sessions.forEach(function (s) {
        var active = s.id === state.activeId;
        html += '<div class="webshell-terminal-session' + (active ? ' active' : '') + '">' +
            '<button type="button" class="webshell-terminal-session-main" data-action="switch" data-terminal-id="' + escapeHtml(s.id) + '">' + escapeHtml(s.name) + '</button>' +
            '<button type="button" class="webshell-terminal-session-close" data-action="close" data-terminal-id="' + escapeHtml(s.id) + '" title="' + escapeHtml(wsT('common.close') || '关闭') + '">×</button>' +
            '</div>';
    });
    html += '<button type="button" class="webshell-terminal-session-add" data-action="add" title="' + escapeHtml(wsT('webshell.terminalNewWindow') || '新终端') + '">+</button>';
    tabsEl.innerHTML = html;
    tabsEl.querySelectorAll('[data-action]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            var action = btn.getAttribute('data-action') || '';
            var targetId = btn.getAttribute('data-terminal-id') || '';
            if (webshellRunning || webshellTerminalRunning) return;
            if (action === 'add') {
                var nextState = getWebshellTerminalSessions(connId);
                var seq = nextState.sessions.length + 1;
                var nextId = 't' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
                var prefix = wsT('webshell.terminalWindowPrefix') || '终端';
                nextState.sessions.push({ id: nextId, name: prefix + seq });
                nextState.activeId = nextId;
                saveWebshellTerminalSessions(connId, nextState);
                destroyWebshellTerminal();
                initWebshellTerminal(conn);
                renderWebshellTerminalSessions(conn);
                return;
            }
            if (!targetId) return;
            if (action === 'close') {
                var curr2 = getWebshellTerminalSessions(connId);
                if (curr2.sessions.length <= 1) return;
                var idx2 = curr2.sessions.findIndex(function (s) { return s.id === targetId; });
                if (idx2 < 0) return;
                curr2.sessions.splice(idx2, 1);
                if (curr2.activeId === targetId) {
                    var fallback = curr2.sessions[Math.max(0, idx2 - 1)] || curr2.sessions[0];
                    curr2.activeId = fallback.id;
                }
                saveWebshellTerminalSessions(connId, curr2);
                // 清理该终端的日志与历史
                var terminalKey = getWebshellTerminalSessionKey(connId, targetId);
                clearWebshellTerminalLog(terminalKey);
                delete webshellHistoryByConn[terminalKey];
                destroyWebshellTerminal();
                initWebshellTerminal(conn);
                renderWebshellTerminalSessions(conn);
                return;
            }
            if (targetId === getActiveWebshellTerminalSessionId(connId)) return;
            var curr = getWebshellTerminalSessions(connId);
            curr.activeId = targetId;
            saveWebshellTerminalSessions(connId, curr);
            destroyWebshellTerminal();
            initWebshellTerminal(conn);
            renderWebshellTerminalSessions(conn);
        });
    });
}

function getWebshellTreeState(conn) {
    var key = safeConnIdForStorage(conn);
    if (!key) return null;
    if (!webshellDirTreeByConn[key]) webshellDirTreeByConn[key] = { '.': [] };
    if (!webshellDirExpandedByConn[key]) webshellDirExpandedByConn[key] = { '.': true };
    if (!webshellDirLoadedByConn[key]) webshellDirLoadedByConn[key] = { '.': false };
    return {
        key: key,
        tree: webshellDirTreeByConn[key],
        expanded: webshellDirExpandedByConn[key],
        loaded: webshellDirLoadedByConn[key]
    };
}

function newWebshellDbProfile(name) {
    var now = Date.now().toString(36);
    var rand = Math.random().toString(36).slice(2, 8);
    return {
        id: 'dbp_' + now + rand,
        name: name || 'DB-1',
        type: 'mysql',
        host: '127.0.0.1',
        port: '3306',
        username: 'root',
        password: '',
        database: '',
        selectedDatabase: '',
        sqlitePath: '/tmp/test.db',
        sql: 'SELECT 1;',
        output: '',
        outputIsError: false,
        schema: {}
    };
}

function getWebshellDbStateStorageKey(conn) {
    return 'webshell_db_state_' + safeConnIdForStorage(conn);
}

function normalizeWebshellDbState(rawState) {
    var state = rawState && typeof rawState === 'object' ? rawState : {};
    var profiles = Array.isArray(state.profiles) ? state.profiles.slice() : [];
    if (!profiles.length) profiles = [newWebshellDbProfile('DB-1')];
    profiles = profiles.map(function (p, idx) {
        var base = newWebshellDbProfile('DB-' + (idx + 1));
        return Object.assign(base, p || {});
    });
    var activeProfileId = state.activeProfileId || '';
    if (!profiles.some(function (p) { return p.id === activeProfileId; })) {
        activeProfileId = profiles[0].id;
    }
    var aiMemo = typeof state.aiMemo === 'string' ? state.aiMemo : '';
    if (aiMemo.length > 100000) aiMemo = aiMemo.slice(0, 100000);
    return { profiles: profiles, activeProfileId: activeProfileId, aiMemo: aiMemo };
}

function getWebshellDbState(conn) {
    var key = getWebshellDbStateStorageKey(conn);
    if (!key) return normalizeWebshellDbState(null);
    if (webshellDbConfigByConn[key]) return webshellDbConfigByConn[key];
    var state = normalizeWebshellDbState(null);
    webshellDbConfigByConn[key] = state;
    return state;
}

function saveWebshellDbState(conn, state) {
    var key = getWebshellDbStateStorageKey(conn);
    if (!key || !state) return;
    var normalized = normalizeWebshellDbState(state);
    webshellDbConfigByConn[key] = normalized;
    if (conn && conn.id) queueWebshellPersistStateSave(conn.id);
}

function getWebshellDbConfig(conn) {
    var state = getWebshellDbState(conn);
    var active = state.profiles.find(function (p) { return p.id === state.activeProfileId; });
    return active || state.profiles[0];
}

function saveWebshellDbConfig(conn, cfg) {
    if (!cfg) return;
    var state = getWebshellDbState(conn);
    var idx = state.profiles.findIndex(function (p) { return p.id === state.activeProfileId; });
    if (idx < 0) idx = 0;
    state.profiles[idx] = Object.assign({}, state.profiles[idx], cfg);
    state.activeProfileId = state.profiles[idx].id;
    saveWebshellDbState(conn, state);
}

function getWebshellAiMemo(conn) {
    var state = getWebshellDbState(conn);
    return typeof state.aiMemo === 'string' ? state.aiMemo : '';
}

function saveWebshellAiMemo(conn, text) {
    var state = getWebshellDbState(conn);
    state.aiMemo = String(text || '');
    if (state.aiMemo.length > 100000) state.aiMemo = state.aiMemo.slice(0, 100000);
    saveWebshellDbState(conn, state);
}

function webshellDbGetFieldValue(id) {
    var el = document.getElementById(id);
    return el && typeof el.value === 'string' ? el.value.trim() : '';
}

function webshellDbCollectConfig(conn) {
    var curr = getWebshellDbConfig(conn) || {};
    var nameVal = webshellDbGetFieldValue('webshell-db-profile-name');
    var cfg = {
        name: nameVal || curr.name || 'DB-1',
        type: webshellDbGetFieldValue('webshell-db-type') || 'mysql',
        host: webshellDbGetFieldValue('webshell-db-host') || '127.0.0.1',
        port: webshellDbGetFieldValue('webshell-db-port') || '',
        username: webshellDbGetFieldValue('webshell-db-user') || '',
        password: (document.getElementById('webshell-db-pass') || {}).value || '',
        database: webshellDbGetFieldValue('webshell-db-name') || '',
        selectedDatabase: curr.selectedDatabase || '',
        sqlitePath: webshellDbGetFieldValue('webshell-db-sqlite-path') || '/tmp/test.db',
        sql: (document.getElementById('webshell-db-sql') || {}).value || ''
    };
    saveWebshellDbConfig(conn, cfg);
    return cfg;
}

function webshellDbUpdateFieldVisibility() {
    var type = webshellDbGetFieldValue('webshell-db-type') || 'mysql';
    var isSqlite = type === 'sqlite';
    var blocks = document.querySelectorAll('.webshell-db-common-field');
    blocks.forEach(function (el) { el.style.display = isSqlite ? 'none' : ''; });
    var sqliteBlock = document.getElementById('webshell-db-sqlite-row');
    if (sqliteBlock) sqliteBlock.style.display = isSqlite ? '' : 'none';
    var portEl = document.getElementById('webshell-db-port');
    if (portEl && !String(portEl.value || '').trim()) {
        if (type === 'mysql') portEl.value = '3306';
        else if (type === 'pgsql') portEl.value = '5432';
        else if (type === 'mssql') portEl.value = '1433';
    }
}

function webshellDbSetOutput(text, isError) {
    var outputEl = document.getElementById('webshell-db-output');
    if (!outputEl) return;
    outputEl.textContent = text || '';
    outputEl.classList.toggle('error', !!isError);
}

function webshellDbRenderTable(rawOutput) {
    var wrap = document.getElementById('webshell-db-result-table');
    if (!wrap) return false;
    var raw = String(rawOutput || '').trim();
    if (!raw) {
        wrap.innerHTML = '';
        return false;
    }
    var lines = raw.split(/\r?\n/).filter(function (line) {
        var t = String(line || '').trim();
        if (!t) return false;
        if (/^\(\d+\s+rows?\)$/i.test(t)) return false;
        if (/^-{3,}$/.test(t)) return false;
        return true;
    });
    if (lines.length < 2) {
        wrap.innerHTML = '';
        return false;
    }
    var delimiter = lines[0].indexOf('\t') >= 0 ? '\t' : (lines[0].indexOf('|') >= 0 ? '|' : '');
    if (!delimiter) {
        wrap.innerHTML = '';
        return false;
    }
    var header = lines[0].split(delimiter).map(function (s) { return String(s || '').trim(); });
    if (!header.length || (header.length === 1 && !header[0])) {
        wrap.innerHTML = '';
        return false;
    }
    var rows = [];
    for (var i = 1; i < lines.length; i++) {
        var line = lines[i];
        if (/^[-+\s|]+$/.test(line)) continue;
        var cols = line.split(delimiter).map(function (s) { return String(s || '').trim(); });
        if (cols.length !== header.length) continue;
        rows.push(cols);
    }
    if (!rows.length) {
        wrap.innerHTML = '';
        return false;
    }
    var maxRows = Math.min(rows.length, 200);
    var html = '<table class="webshell-db-table"><thead><tr>';
    header.forEach(function (h) { html += '<th>' + escapeHtml(h || '-') + '</th>'; });
    html += '</tr></thead><tbody>';
    for (var r = 0; r < maxRows; r++) {
        html += '<tr>';
        rows[r].forEach(function (c) { html += '<td>' + escapeHtml(c || '') + '</td>'; });
        html += '</tr>';
    }
    html += '</tbody></table>';
    if (rows.length > maxRows) {
        html += '<div class="webshell-db-table-meta">' + escapeHtml(typeof window.t === 'function' ? window.t('webshell.dbTableMetaTruncated', { shown: maxRows, total: rows.length }) : ('仅展示前 ' + maxRows + ' 行，共 ' + rows.length + ' 行')) + '</div>';
    } else {
        html += '<div class="webshell-db-table-meta">' + escapeHtml(typeof window.t === 'function' ? window.t('webshell.dbTableMetaTotal', { rows: rows.length, cols: header.length }) : ('共 ' + rows.length + ' 行，' + header.length + ' 列')) + '</div>';
    }
    wrap.innerHTML = html;
    return true;
}

function webshellDbQuoteIdentifier(type, name) {
    var v = String(name || '');
    if (!v) return '';
    if (type === 'mysql') return '`' + v.replace(/`/g, '``') + '`';
    if (type === 'mssql') return '[' + v.replace(/]/g, ']]') + ']';
    return '"' + v.replace(/"/g, '""') + '"';
}

function webshellDbQuoteLiteral(value) {
    return "'" + String(value == null ? '' : value).replace(/'/g, "''") + "'";
}

function buildWebshellDbCommand(cfg, isTestOnly, options) {
    options = options || {};
    var type = cfg.type || 'mysql';
    var sql = String(isTestOnly ? 'SELECT 1;' : (options.sql || cfg.sql || '')).trim();
    if (!sql) return { error: wsT('webshell.dbSqlRequired') || '请输入 SQL' };

    var sqlB64 = btoa(unescape(encodeURIComponent(sql)));
    var sqlB64Arg = escapeSingleQuotedShellArg(sqlB64);
    var tmpFile = '/tmp/.csai_sql_$$.sql';
    var decodeToFile = 'printf %s ' + sqlB64Arg + " | base64 -d > " + tmpFile;
    var cleanup = '; rc=$?; rm -f ' + tmpFile + '; echo "__CSAI_DB_RC__:$rc"; exit $rc';
    var command = '';

    if (type === 'mysql') {
        var host = escapeSingleQuotedShellArg(cfg.host || '127.0.0.1');
        var port = escapeSingleQuotedShellArg(cfg.port || '3306');
        var user = escapeSingleQuotedShellArg(cfg.username || 'root');
        var pass = escapeSingleQuotedShellArg(cfg.password || '');
        var dbName = cfg.selectedDatabase || cfg.database || '';
        var db = dbName ? (' -D ' + escapeSingleQuotedShellArg(dbName)) : '';
        command = decodeToFile + '; MYSQL_PWD=' + pass + ' mysql -h ' + host + ' -P ' + port + ' -u ' + user + db + ' --batch --raw < ' + tmpFile + cleanup;
    } else if (type === 'pgsql') {
        var pHost = escapeSingleQuotedShellArg(cfg.host || '127.0.0.1');
        var pPort = escapeSingleQuotedShellArg(cfg.port || '5432');
        var pUser = escapeSingleQuotedShellArg(cfg.username || 'postgres');
        var pPass = escapeSingleQuotedShellArg(cfg.password || '');
        var pDb = escapeSingleQuotedShellArg(cfg.selectedDatabase || cfg.database || 'postgres');
        command = decodeToFile + '; PGPASSWORD=' + pPass + ' psql -h ' + pHost + ' -p ' + pPort + ' -U ' + pUser + ' -d ' + pDb + ' -v ON_ERROR_STOP=1 -A -F "|" -P footer=off -f ' + tmpFile + cleanup;
    } else if (type === 'sqlite') {
        var sqlitePath = escapeSingleQuotedShellArg(cfg.sqlitePath || '/tmp/test.db');
        command = decodeToFile + '; sqlite3 -header -separator "|" ' + sqlitePath + ' < ' + tmpFile + cleanup;
    } else if (type === 'mssql') {
        var sHost = cfg.host || '127.0.0.1';
        var sPort = cfg.port || '1433';
        var sUser = escapeSingleQuotedShellArg(cfg.username || 'sa');
        var sPass = escapeSingleQuotedShellArg(cfg.password || '');
        var sDb = escapeSingleQuotedShellArg(cfg.selectedDatabase || cfg.database || 'master');
        var server = escapeSingleQuotedShellArg(sHost + ',' + sPort);
        command = decodeToFile + '; sqlcmd -S ' + server + ' -U ' + sUser + ' -P ' + sPass + ' -W -s "|" -d ' + sDb + ' -i ' + tmpFile + cleanup;
    } else {
        return { error: (wsT('webshell.dbExecFailed') || '数据库执行失败') + ': unsupported type ' + type };
    }

    return { command: command };
}

function buildWebshellDbSchemaCommand(cfg) {
    var type = cfg.type || 'mysql';
    var schemaSQL = '';
    if (type === 'mysql') {
        schemaSQL = "SELECT SCHEMA_NAME AS db_name, '' AS table_name, '' AS column_name FROM INFORMATION_SCHEMA.SCHEMATA UNION ALL SELECT TABLE_SCHEMA AS db_name, TABLE_NAME AS table_name, '' AS column_name FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE='BASE TABLE' UNION ALL SELECT TABLE_SCHEMA AS db_name, TABLE_NAME AS table_name, COLUMN_NAME AS column_name FROM INFORMATION_SCHEMA.COLUMNS ORDER BY db_name, table_name, column_name;";
    } else if (type === 'pgsql') {
        schemaSQL = "SELECT table_schema AS db_name, table_name, '' AS column_name FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema NOT IN ('pg_catalog','information_schema') UNION ALL SELECT table_schema AS db_name, table_name, column_name FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') ORDER BY db_name, table_name, column_name;";
    } else if (type === 'sqlite') {
        schemaSQL = "SELECT 'main' AS db_name, name AS table_name, '' AS column_name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' UNION ALL SELECT 'main' AS db_name, m.name AS table_name, p.name AS column_name FROM sqlite_master m JOIN pragma_table_info(m.name) p ON 1=1 WHERE m.type='table' AND m.name NOT LIKE 'sqlite_%' ORDER BY db_name, table_name, column_name;";
    } else if (type === 'mssql') {
        schemaSQL = "SELECT TABLE_SCHEMA AS db_name, TABLE_NAME AS table_name, '' AS column_name FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE='BASE TABLE' UNION ALL SELECT TABLE_SCHEMA AS db_name, TABLE_NAME AS table_name, COLUMN_NAME AS column_name FROM INFORMATION_SCHEMA.COLUMNS ORDER BY db_name, table_name, column_name;";
    } else {
        return { error: (wsT('webshell.dbExecFailed') || '数据库执行失败') + ': unsupported type ' + type };
    }
    return buildWebshellDbCommand(cfg, false, { sql: schemaSQL });
}

function buildWebshellDbColumnsCommand(cfg, dbName, tableName) {
    var type = cfg.type || 'mysql';
    var sql = '';
    if (type === 'mysql') {
        sql = "SELECT COLUMN_NAME AS column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=" + webshellDbQuoteLiteral(dbName) + " AND TABLE_NAME=" + webshellDbQuoteLiteral(tableName) + " ORDER BY ORDINAL_POSITION;";
    } else if (type === 'pgsql') {
        sql = "SELECT column_name FROM information_schema.columns WHERE table_schema=" + webshellDbQuoteLiteral(dbName) + " AND table_name=" + webshellDbQuoteLiteral(tableName) + " ORDER BY ordinal_position;";
    } else if (type === 'sqlite') {
        sql = "SELECT name AS column_name FROM pragma_table_info(" + webshellDbQuoteLiteral(tableName) + ") ORDER BY cid;";
    } else if (type === 'mssql') {
        sql = "SELECT COLUMN_NAME AS column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=" + webshellDbQuoteLiteral(dbName) + " AND TABLE_NAME=" + webshellDbQuoteLiteral(tableName) + " ORDER BY ORDINAL_POSITION;";
    } else {
        return { error: (wsT('webshell.dbExecFailed') || '数据库执行失败') + ': unsupported type ' + type };
    }
    return buildWebshellDbCommand(cfg, false, { sql: sql });
}

function buildWebshellDbColumnsByDatabaseCommand(cfg, dbName) {
    var type = cfg.type || 'mysql';
    var sql = '';
    if (type === 'mysql') {
        sql = "SELECT TABLE_NAME AS table_name, COLUMN_NAME AS column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=" + webshellDbQuoteLiteral(dbName) + " ORDER BY TABLE_NAME, ORDINAL_POSITION;";
    } else if (type === 'pgsql') {
        sql = "SELECT table_name, column_name FROM information_schema.columns WHERE table_schema=" + webshellDbQuoteLiteral(dbName) + " ORDER BY table_name, ordinal_position;";
    } else if (type === 'sqlite') {
        sql = "SELECT m.name AS table_name, p.name AS column_name FROM sqlite_master m JOIN pragma_table_info(m.name) p ON 1=1 WHERE m.type='table' AND m.name NOT LIKE 'sqlite_%' ORDER BY m.name, p.cid;";
    } else if (type === 'mssql') {
        sql = "SELECT TABLE_NAME AS table_name, COLUMN_NAME AS column_name FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=" + webshellDbQuoteLiteral(dbName) + " ORDER BY TABLE_NAME, ORDINAL_POSITION;";
    } else {
        return { error: (wsT('webshell.dbExecFailed') || '数据库执行失败') + ': unsupported type ' + type };
    }
    return buildWebshellDbCommand(cfg, false, { sql: sql });
}

function parseWebshellDbExecOutput(rawOutput) {
    var raw = String(rawOutput || '');
    var rc = null;
    var cleaned = raw.replace(/__CSAI_DB_RC__:(\d+)\s*$/m, function (_, code) {
        rc = parseInt(code, 10);
        return '';
    }).trim();
    return { rc: rc, output: cleaned };
}

function parseWebshellDbSchema(rawOutput) {
    var text = String(rawOutput || '').trim();
    if (!text) return {};
    var lines = text.split(/\r?\n/).filter(function (line) {
        return line && line.trim() && !/^\(\d+\s+rows?\)$/i.test(line.trim()) && !/^[-+\s|]+$/.test(line.trim());
    });
    if (lines.length < 2) return {};
    var delimiter = lines[0].indexOf('\t') >= 0 ? '\t' : (lines[0].indexOf('|') >= 0 ? '|' : '');
    if (!delimiter) return {};
    var headers = lines[0].split(delimiter).map(function (s) { return String(s || '').trim().toLowerCase(); });
    var dbIdx = headers.indexOf('db_name');
    var tableIdx = headers.indexOf('table_name');
    var columnIdx = headers.indexOf('column_name');
    if (dbIdx < 0 || tableIdx < 0) return {};
    var schema = {};
    for (var i = 1; i < lines.length; i++) {
        var cols = lines[i].split(delimiter).map(function (s) { return String(s || '').trim(); });
        if (cols.length !== headers.length) continue;
        var db = cols[dbIdx] || 'default';
        var table = cols[tableIdx] || '';
        var column = columnIdx >= 0 ? (cols[columnIdx] || '') : '';
        if (!schema[db]) schema[db] = { tables: {} };
        if (!table) continue;
        if (!schema[db].tables[table]) schema[db].tables[table] = [];
        if (column && schema[db].tables[table].indexOf(column) < 0) {
            schema[db].tables[table].push(column);
        }
    }
    return normalizeWebshellDbSchema(schema);
}

function parseWebshellDbColumns(rawOutput) {
    var text = String(rawOutput || '').trim();
    if (!text) return [];
    var lines = text.split(/\r?\n/).filter(function (line) {
        return line && line.trim() && !/^\(\d+\s+rows?\)$/i.test(line.trim()) && !/^[-+\s|]+$/.test(line.trim());
    });
    if (lines.length < 2) return [];
    var delimiter = lines[0].indexOf('\t') >= 0 ? '\t' : (lines[0].indexOf('|') >= 0 ? '|' : '');
    if (!delimiter) {
        if (String(lines[0] || '').trim().toLowerCase() !== 'column_name') return [];
        var plainColumns = [];
        for (var p = 1; p < lines.length; p++) {
            var plainName = String(lines[p] || '').trim();
            if (!plainName || plainColumns.indexOf(plainName) >= 0) continue;
            plainColumns.push(plainName);
        }
        return plainColumns;
    }
    var headers = lines[0].split(delimiter).map(function (s) { return String(s || '').trim().toLowerCase(); });
    var colIdx = headers.indexOf('column_name');
    if (colIdx < 0) return [];
    var columns = [];
    for (var i = 1; i < lines.length; i++) {
        var cols = lines[i].split(delimiter).map(function (s) { return String(s || '').trim(); });
        if (cols.length !== headers.length) continue;
        var name = cols[colIdx] || '';
        if (!name || columns.indexOf(name) >= 0) continue;
        columns.push(name);
    }
    return columns;
}

function parseWebshellDbTableColumns(rawOutput) {
    var text = String(rawOutput || '').trim();
    if (!text) return {};
    var lines = text.split(/\r?\n/).filter(function (line) {
        return line && line.trim() && !/^\(\d+\s+rows?\)$/i.test(line.trim()) && !/^[-+\s|]+$/.test(line.trim());
    });
    if (lines.length < 2) return {};
    var delimiter = lines[0].indexOf('\t') >= 0 ? '\t' : (lines[0].indexOf('|') >= 0 ? '|' : '');
    if (!delimiter) return {};
    var headers = lines[0].split(delimiter).map(function (s) { return String(s || '').trim().toLowerCase(); });
    var tableIdx = headers.indexOf('table_name');
    var colIdx = headers.indexOf('column_name');
    if (tableIdx < 0 || colIdx < 0) return {};
    var tableColumns = {};
    for (var i = 1; i < lines.length; i++) {
        var cols = lines[i].split(delimiter).map(function (s) { return String(s || '').trim(); });
        if (cols.length !== headers.length) continue;
        var tableName = cols[tableIdx] || '';
        var colName = cols[colIdx] || '';
        if (!tableName || !colName) continue;
        if (!tableColumns[tableName]) tableColumns[tableName] = [];
        if (tableColumns[tableName].indexOf(colName) >= 0) continue;
        tableColumns[tableName].push(colName);
    }
    return tableColumns;
}

function normalizeWebshellDbSchema(rawSchema) {
    if (!rawSchema || typeof rawSchema !== 'object') return {};
    var normalized = {};
    Object.keys(rawSchema).forEach(function (dbName) {
        var dbEntry = rawSchema[dbName];
        var tableMap = {};

        if (Array.isArray(dbEntry)) {
            dbEntry.forEach(function (tableName) {
                var t = String(tableName || '').trim();
                if (!t) return;
                if (!tableMap[t]) tableMap[t] = [];
            });
        } else if (dbEntry && typeof dbEntry === 'object') {
            var tablesSource = (dbEntry.tables && typeof dbEntry.tables === 'object') ? dbEntry.tables : dbEntry;
            Object.keys(tablesSource).forEach(function (tableName) {
                if (tableName === 'tables') return;
                var t = String(tableName || '').trim();
                if (!t) return;
                var rawColumns = tablesSource[tableName];
                var columns = Array.isArray(rawColumns) ? rawColumns : [];
                var uniqColumns = [];
                columns.forEach(function (colName) {
                    var c = String(colName || '').trim();
                    if (!c || uniqColumns.indexOf(c) >= 0) return;
                    uniqColumns.push(c);
                });
                uniqColumns.sort(function (a, b) { return a.localeCompare(b); });
                tableMap[t] = uniqColumns;
            });
        }

        var sortedTables = {};
        Object.keys(tableMap).sort(function (a, b) { return a.localeCompare(b); }).forEach(function (tableName) {
            sortedTables[tableName] = tableMap[tableName];
        });
        normalized[dbName] = { tables: sortedTables };
    });
    return normalized;
}

function simplifyWebshellAiError(rawMessage) {
    var msg = String(rawMessage || '').trim();
    var lower = msg.toLowerCase();
    if ((lower.indexOf('401') !== -1 || lower.indexOf('unauthorized') !== -1) &&
        (lower.indexOf('api key') !== -1 || lower.indexOf('apikey') !== -1)) {
        return wsTOr('webshell.aiAuthFailed', '鉴权失败：API Key 未配置或无效（401）');
    }
    if (lower.indexOf('timeout') !== -1 || lower.indexOf('timed out') !== -1) {
        return wsTOr('webshell.aiRequestTimeout', '请求超时，请稍后重试');
    }
    if (lower.indexOf('network') !== -1 || lower.indexOf('failed to fetch') !== -1) {
        return wsTOr('webshell.aiNetworkError', '网络异常，请检查服务连通性');
    }
    return msg || wsTOr('webshell.aiRequestFailed', '请求失败');
}

function renderWebshellAiErrorMessage(targetEl, rawMessage) {
    if (!targetEl) return;
    var full = String(rawMessage || '').trim();
    var shortMsg = simplifyWebshellAiError(full);
    targetEl.classList.add('webshell-ai-msg-error');
    targetEl.innerHTML = '';
    var head = document.createElement('div');
    head.className = 'webshell-ai-error-head';
    head.textContent = shortMsg;
    targetEl.appendChild(head);
    if (full && full !== shortMsg) {
        var detail = document.createElement('details');
        detail.className = 'webshell-ai-error-detail';
        var summary = document.createElement('summary');
        summary.textContent = wsTOr('webshell.aiViewErrorDetail', '查看详细错误');
        var pre = document.createElement('pre');
        pre.textContent = full;
        detail.appendChild(summary);
        detail.appendChild(pre);
        targetEl.appendChild(detail);
    }
}

function isLikelyWebshellAiErrorMessage(content, msg) {
    var text = String(content || '').trim();
    if (!text) return false;
    var lower = text.toLowerCase();
    if (/^(执行失败|请求失败|请求异常|error)\s*[:：]/i.test(text)) return true;
    if (/(status code\s*:\s*4\d{2}|unauthorized|forbidden|apikey|api key|invalid api key)/i.test(lower)) return true;
    if (/(noderunerror|tool[-_ ]?error|agent[-_ ]?error|执行失败)/i.test(lower)) return true;
    var details = msg && Array.isArray(msg.processDetails) ? msg.processDetails : [];
    return details.some(function (d) { return String((d && d.eventType) || '').toLowerCase() === 'error'; });
}

function formatWebshellAiConvDate(updatedAt) {
    if (!updatedAt) return '';
    var d = typeof updatedAt === 'string' ? new Date(updatedAt) : updatedAt;
    if (isNaN(d.getTime())) return '';
    var now = new Date();
    var sameDay = d.getDate() === now.getDate() && d.getMonth() === now.getMonth() && d.getFullYear() === now.getFullYear();
    if (sameDay) return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0');
    return (d.getMonth() + 1) + '/' + d.getDate();
}

function webshellAgentPx(data) {
    if (!data || data.einoAgent == null) return '';
    var s = String(data.einoAgent).trim();
    return s ? ('[' + s + '] ') : '';
}

function formatWebshellCompactInteger(value) {
    var n = Number(value || 0);
    if (!Number.isFinite(n)) return '0';
    try {
        var locale = (typeof getCurrentTimeLocale === 'function') ? getCurrentTimeLocale() : undefined;
        return Math.max(0, Math.trunc(n)).toLocaleString(locale);
    } catch (e) {
        return String(Math.max(0, Math.trunc(n)));
    }
}

function formatWebshellEinoUsageSummaryTitle(data) {
    var d = data && typeof data === 'object' ? data : {};
    var base = wsTOr('chat.einoUsageSummaryTitle', 'Token 用量汇总');
    var total = Number(d.totalTokens || 0);
    if (Number.isFinite(total) && total > 0) {
        return base + ' · ' + formatWebshellCompactInteger(total);
    }
    return base;
}

function formatWebshellEinoUsageSummaryMessage(data) {
    var d = data && typeof data === 'object' ? data : {};
    var rows = [
        [wsTOr('chat.einoUsageModelCalls', '模型调用'), d.modelCalls],
        [wsTOr('chat.einoUsagePromptTokens', '输入 tokens'), d.promptTokens],
        [wsTOr('chat.einoUsageCompletionTokens', '输出 tokens'), d.completionTokens],
        [wsTOr('chat.einoUsageTotalTokens', '总 tokens'), d.totalTokens]
    ];
    if (Number(d.cachedTokens || 0) > 0) {
        rows.push([wsTOr('chat.einoUsageCachedTokens', '缓存 tokens'), d.cachedTokens]);
    }
    if (Number(d.cacheWriteTokens || 0) > 0) {
        rows.push([wsTOr('chat.einoUsageCacheWriteTokens', '缓存写入 tokens'), d.cacheWriteTokens]);
    }
    if (Number(d.reasoningTokens || 0) > 0) {
        rows.push([wsTOr('chat.einoUsageReasoningTokens', '推理 tokens'), d.reasoningTokens]);
    }
    return rows.map(function (row) {
        return row[0] + ': ' + formatWebshellCompactInteger(row[1]);
    }).join('\n');
}

// 根据后端保存的 processDetail 构建一条时间线项的 HTML（与 appendTimelineItem 展示一致）
function buildWebshellTimelineItemFromDetail(detail) {
    var eventType = detail.eventType || '';
    var title = detail.message || '';
    var data = detail.data || {};
    var ap = webshellAgentPx(data);
    if (eventType === 'iteration') {
        title = ap + ((typeof window.t === 'function') ? window.t('chat.iterationRound', { n: data.iteration || 1 }) : ('第 ' + (data.iteration || 1) + ' 轮迭代'));
    } else if (eventType === 'thinking') {
        title = ap + '🤔 ' + ((typeof window.t === 'function') ? window.t('chat.aiThinking') : 'AI 思考');
    } else if (eventType === 'reasoning_chain') {
        title = ap + '🔗 ' + ((typeof window.t === 'function') ? window.t('chat.reasoningChain') : '推理过程');
    } else if (eventType === 'tool_calls_detected') {
        title = ap + '🔧 ' + ((typeof window.t === 'function') ? window.t('chat.toolCallsDetected', { count: data.count || 0 }) : ('检测到 ' + (data.count || 0) + ' 个工具调用'));
    } else if (eventType === 'tool_call') {
        var tn = data.toolName || ((typeof window.t === 'function') ? window.t('chat.unknownTool') : '未知工具');
        var idx = data.index || 0;
        var total = data.total || 0;
        var wsCallTitle = typeof window.formatToolCallTimelineTitle === 'function'
            ? window.formatToolCallTimelineTitle(tn, idx, total)
            : ((typeof window.t === 'function') ? window.t('chat.callTool', { name: tn, index: idx, total: total }) : ('调用: ' + tn + (total ? ' (' + idx + '/' + total + ')' : '')));
        title = ap + '🔧 ' + wsCallTitle;
    } else if (eventType === 'tool_result') {
        var tname = data.toolName || wsTOr('webshell.defaultToolName', '工具');
        var wsNoResultText = (typeof window.t === 'function') ? window.t('timeline.noResult') : '无结果';
        var wsResult = data.result != null ? data.result : (data.error != null ? data.error : (data.resultPreview != null ? data.resultPreview : wsNoResultText));
        var wsResultStr = (typeof wsResult === 'string') ? wsResult : JSON.stringify(wsResult);
        var wsDisplayState = (typeof window.getToolResultDisplayState === 'function')
            ? window.getToolResultDisplayState(data, { rawText: wsResultStr })
            : { kind: ((data.isError || data.success === false) ? 'error' : 'success'), isError: (data.isError || data.success === false) };
        var wsBackgroundRunning = wsDisplayState.kind === 'background_running';
        var success = !wsDisplayState.isError && !wsBackgroundRunning;
        var wsIcon = wsDisplayState.kind === 'blocked' ? '🛡 ' : (wsBackgroundRunning ? '⏳ ' : (success ? '✅ ' : '❌ '));
        var wsLabel = wsDisplayState.kind === 'blocked'
            ? ((typeof window.t === 'function') ? window.t('chat.toolExecBlocked', { name: tname }) : tname + ' 已拦截')
            : wsBackgroundRunning
            ? (((typeof window.getBackgroundRunningToolLabel === 'function') ? window.getBackgroundRunningToolLabel() : '后台执行中') + ': ' + tname)
            : ((typeof window.t === 'function') ? (success ? window.t('chat.toolExecComplete', { name: tname }) : window.t('chat.toolExecFailed', { name: tname })) : (tname + (success ? ' 执行完成' : ' 执行失败')));
        title = ap + wsIcon + wsLabel;
    } else if (eventType === 'eino_agent_reply') {
        title = ap + '💬 ' + ((typeof window.t === 'function') ? window.t('chat.einoAgentReplyTitle') : '子代理回复');
    } else if (eventType === 'eino_usage_summary') {
        title = ap + '📊 ' + formatWebshellEinoUsageSummaryTitle(data);
    } else if (eventType === 'progress') {
        title = (typeof window.translateProgressMessage === 'function') ? window.translateProgressMessage(detail.message || '') : (detail.message || '');
    }
    var html = '<span class="webshell-ai-timeline-title">' + escapeHtml(title || '') + '</span>';
    if (eventType === 'eino_agent_reply' && detail.message) {
        html += '<div class="webshell-ai-timeline-msg"><pre style="white-space:pre-wrap;">' + escapeHtml(detail.message) + '</pre></div>';
    }
    if (eventType === 'eino_usage_summary') {
        html += '<div class="webshell-ai-timeline-msg"><pre style="white-space:pre-wrap;">' + escapeHtml(formatWebshellEinoUsageSummaryMessage(data)) + '</pre></div>';
    }
    if (eventType === 'tool_call' && data && (data.argumentsObj || data.arguments)) {
        try {
            var args = data.argumentsObj;
            if (args == null && data.arguments != null && String(data.arguments).trim() !== '') {
                try {
                    args = JSON.parse(String(data.arguments));
                } catch (e2) {
                    args = { _raw: String(data.arguments) };
                }
            }
            if (args && typeof args === 'object') {
                var paramsLabel = (typeof window.t === 'function') ? window.t('timeline.params') : '参数:';
                html += '<div class="webshell-ai-timeline-msg"><div class="tool-arg-section"><strong>' + escapeHtml(paramsLabel) + '</strong><pre class="tool-args">' + escapeHtml(JSON.stringify(args, null, 2)) + '</pre></div></div>';
            }
        } catch (e) {}
    }
    if (eventType === 'tool_call' && data && data._mergedResult && typeof window.buildToolResultSectionHtml === 'function') {
        html += '<div class="webshell-ai-timeline-msg tool-result-slot">' + window.buildToolResultSectionHtml(data._mergedResult) + '</div>';
    } else if (eventType === 'tool_result' && data) {
        var noResultText = (typeof window.t === 'function') ? window.t('timeline.noResult') : '无结果';
        var result = data.result != null ? data.result : (data.error != null ? data.error : (data.resultPreview != null ? data.resultPreview : noResultText));
        var resultStr = (typeof result === 'string') ? result : JSON.stringify(result);
        var displayState = (typeof window.getToolResultDisplayState === 'function')
            ? window.getToolResultDisplayState(data, { rawText: resultStr })
            : { kind: ((data.isError || data.success === false) ? 'error' : 'success'), isError: (data.isError || data.success === false) };
        var execResultLabel = (typeof window.t === 'function') ? window.t('timeline.executionResult') : '执行结果:';
        var execIdLabel = (typeof window.t === 'function') ? window.t('timeline.executionId') : '执行ID:';
        var sectionClass = displayState.kind === 'blocked' ? 'blocked' : (displayState.kind === 'background_running' ? 'pending' : (displayState.isError ? 'error' : 'success'));
        html += '<div class="webshell-ai-timeline-msg"><div class="tool-result-section ' + sectionClass + '"><strong>' + escapeHtml(execResultLabel) + '</strong><pre class="tool-result">' + escapeHtml(resultStr) + '</pre>' + (data.executionId ? '<div class="tool-execution-id"><span>' + escapeHtml(execIdLabel) + '</span> <code>' + escapeHtml(String(data.executionId)) + '</code></div>' : '') + '</div></div>';
    } else if (eventType !== 'eino_usage_summary' && detail.message && detail.message !== title) {
        html += '<div class="webshell-ai-timeline-msg">' + escapeHtml(detail.message) + '</div>';
    }
    return html;
}

// 渲染「执行过程及调用工具」折叠块（默认折叠，刷新后加载历史时保留并可展开）
function renderWebshellProcessDetailsBlock(processDetails, defaultCollapsed) {
    if (!processDetails || processDetails.length === 0) return null;
    if (typeof window.filterNoiseProcessDetails === 'function') {
        processDetails = window.filterNoiseProcessDetails(processDetails);
    }
    if (!processDetails.length) return null;
    if (typeof window.coalesceProcessDetailsToolPairs === 'function') {
        processDetails = window.coalesceProcessDetailsToolPairs(processDetails);
    }
    var expandLabel = (typeof window.t === 'function') ? window.t('chat.expandDetail') : '展开详情';
    var collapseLabel = (typeof window.t === 'function') ? window.t('tasks.collapseDetail') : '收起详情';
    var headerLabel = (typeof window.t === 'function') ? (window.t('chat.penetrationTestDetail') || '任务执行详情') : '任务执行详情';
    var wrapper = document.createElement('div');
    wrapper.className = 'process-details-container webshell-ai-process-block';
    var collapsed = defaultCollapsed !== false;
    wrapper.innerHTML = '<button type="button" class="webshell-ai-process-toggle" aria-expanded="' + (!collapsed) + '">' + escapeHtml(headerLabel) + ' <span class="ws-toggle-icon">' + (collapsed ? '▶' : '▼') + '</span></button><div class="process-details-content"><div class="progress-timeline webshell-ai-timeline has-items' + (collapsed ? '' : ' expanded') + '"></div></div>';
    var timeline = wrapper.querySelector('.progress-timeline');
    processDetails.forEach(function (d) {
        var item = document.createElement('div');
        item.className = 'webshell-ai-timeline-item webshell-ai-timeline-' + (d.eventType || '');
        item.innerHTML = buildWebshellTimelineItemFromDetail(d);
        timeline.appendChild(item);
    });
    var toggleBtn = wrapper.querySelector('.webshell-ai-process-toggle');
    var toggleIcon = wrapper.querySelector('.ws-toggle-icon');
    toggleBtn.addEventListener('click', function () {
        var isExpanded = timeline.classList.contains('expanded');
        timeline.classList.toggle('expanded');
        toggleBtn.setAttribute('aria-expanded', !isExpanded);
        if (toggleIcon) toggleIcon.textContent = isExpanded ? '▶' : '▼';
    });
    return wrapper;
}

function fetchAndRenderWebshellAiConvList(conn, listEl) {
    if (!conn || !conn.id || !listEl || typeof apiFetch !== 'function') return Promise.resolve();
    return apiFetch('/api/webshell/connections/' + encodeURIComponent(conn.id) + '/ai-conversations', { method: 'GET' })
        .then(function (r) { return r.json(); })
        .then(function (list) {
            if (!Array.isArray(list)) list = [];
            listEl.innerHTML = '';
            list.forEach(function (item) {
                var row = document.createElement('div');
                row.className = 'webshell-ai-conv-item';
                row.dataset.convId = item.id;
                var title = (item.title || '').trim() || item.id.slice(0, 8);
                var dateStr = item.updatedAt ? formatWebshellAiConvDate(item.updatedAt) : '';
                row.innerHTML = '<span class="webshell-ai-conv-item-title">' + escapeHtml(title) + '</span><span class="webshell-ai-conv-item-date">' + escapeHtml(dateStr) + '</span>';
                if (webshellAiConvMap[conn.id] === item.id) row.classList.add('active');
                row.addEventListener('click', function () {
                    webshellAiConvListSelect(conn, item.id, document.getElementById('webshell-ai-messages'), listEl);
                });
                var delBtn = document.createElement('button');
                delBtn.type = 'button';
                delBtn.className = 'btn-ghost btn-sm webshell-ai-conv-del';
                delBtn.textContent = '×';
                delBtn.title = wsT('webshell.aiDeleteConversation') || '删除对话';
                delBtn.addEventListener('click', function (e) {
                    e.stopPropagation();
                    if (!confirm(wsT('webshell.aiDeleteConversationConfirm') || '确定删除该对话？')) return;
                    var deletedId = item.id;
                    apiFetch('/api/conversations/' + encodeURIComponent(deletedId), { method: 'DELETE' })
                        .then(function (r) {
                            if (r.ok) {
                                if (webshellAiConvMap[conn.id] === deletedId) {
                                    delete webshellAiConvMap[conn.id];
                                    var msgs = document.getElementById('webshell-ai-messages');
                                    if (msgs) msgs.innerHTML = '';
                                }
                                fetchAndRenderWebshellAiConvList(conn, listEl);
                                try {
                                    document.dispatchEvent(new CustomEvent('conversation-deleted', { detail: { conversationId: deletedId } }));
                                } catch (err) { /* ignore */ }
                            }
                        })
                        .catch(function (e) { console.warn('删除对话失败', e); });
                });
                row.appendChild(delBtn);
                listEl.appendChild(row);
            });
        })
        .catch(function (e) { console.warn('加载对话列表失败', e); });
}

function webshellAiConvListSelect(conn, convId, messagesContainer, listEl) {
    if (!conn || !convId || !messagesContainer) return;
    webshellAiConvMap[conn.id] = convId;
    if (listEl) listEl.querySelectorAll('.webshell-ai-conv-item').forEach(function (el) {
        el.classList.toggle('active', el.dataset.convId === convId);
    });
    if (typeof apiFetch !== 'function') return;
    apiFetch('/api/conversations/' + encodeURIComponent(convId) + '?include_process_details=1', { method: 'GET' })
        .then(function (r) { return r.json(); })
        .then(function (data) {
            wsSetWebshellAiProject(conn, data.projectId || data.project_id || '');
            messagesContainer.innerHTML = '';
            var list = data.messages || [];
            list.forEach(function (msg) {
                var role = (msg.role || '').toLowerCase();
                var content = (msg.content || '').trim();
                if (!content && role !== 'assistant') return;
                var div = document.createElement('div');
                div.className = 'webshell-ai-msg ' + (role === 'user' ? 'user' : 'assistant');
                if (role === 'user') {
                    div.textContent = content;
                } else {
                    if (isLikelyWebshellAiErrorMessage(content, msg)) {
                        renderWebshellAiErrorMessage(div, content);
                    } else if (typeof formatMarkdown === 'function') {
                        div.innerHTML = formatMarkdown(content);
                    } else {
                        div.textContent = content;
                    }
                }
                messagesContainer.appendChild(div);
                if (role === 'assistant') {
                    var wsMergedDetails = (typeof window.mergeMessageReasoningContentIntoProcessDetails === 'function')
                        ? window.mergeMessageReasoningContentIntoProcessDetails(msg.processDetails || [], msg.reasoningContent)
                        : (msg.processDetails || []);
                    if (wsMergedDetails.length > 0) {
                        var block = renderWebshellProcessDetailsBlock(wsMergedDetails, true);
                        if (block) messagesContainer.appendChild(block);
                    }
                }
            });
            if (list.length === 0) {
                var readyMsg = wsT('webshell.aiSystemReadyMessage') || '系统已就绪。请输入您的测试需求，系统将自动执行相应的安全测试。';
                var readyDiv = document.createElement('div');
                readyDiv.className = 'webshell-ai-msg assistant';
                readyDiv.textContent = readyMsg;
                messagesContainer.appendChild(readyDiv);
            }
            messagesContainer.scrollTop = messagesContainer.scrollHeight;
        })
        .catch(function (e) { console.warn('加载对话失败', e); });
}

// 选择连接：渲染终端 + 文件管理 Tab，并初始化终端
function selectWebshell(id, stateReady) {
    currentWebshellId = id;
    renderWebshellList();
    const conn = webshellConnections.find(c => c.id === id);
    const workspace = document.getElementById('webshell-workspace');
    if (!workspace) return;
    if (!conn) {
        workspace.innerHTML = '<div class="webshell-workspace-placeholder">' + wsT('webshell.selectOrAdd') + '</div>';
        return;
    }
    if (!stateReady) {
        ensureWebshellPersistStateLoaded(conn).then(function () {
            if (currentWebshellId === id) selectWebshell(id, true);
        });
        return;
    }

    destroyWebshellTerminal();
    webshellCurrentConn = conn;

    workspace.innerHTML =
        '<div class="webshell-tabs">' +
        '<button type="button" class="webshell-tab active" data-tab="terminal">' + wsT('webshell.tabTerminal') + '</button>' +
        '<button type="button" class="webshell-tab" data-tab="file">' + wsT('webshell.tabFileManager') + '</button>' +
        '<button type="button" class="webshell-tab" data-tab="db">' + (wsT('webshell.tabDbManager') || '数据库管理') + '</button>' +
        '<button type="button" class="webshell-tab" data-tab="ai">' + (wsT('webshell.tabAiAssistant') || 'AI 助手') + '</button>' +
        '<button type="button" class="webshell-tab" data-tab="memo">' + (wsT('webshell.tabMemo') || '备忘录') + '</button>' +
        '</div>' +
        '<div id="webshell-pane-terminal" class="webshell-pane active">' +
        '<div class="webshell-terminal-toolbar">' +
        '<button type="button" class="btn-ghost btn-sm" id="webshell-terminal-clear" title="' + (wsT('webshell.clearScreen') || '清屏') + '">' + (wsT('webshell.clearScreen') || '清屏') + '</button> ' +
        '<button type="button" class="btn-ghost btn-sm" id="webshell-terminal-copy-log" title="' + (wsT('webshell.copyTerminalLog') || '复制日志') + '">' + (wsT('webshell.copyTerminalLog') || '复制日志') + '</button> ' +
        '<span id="webshell-terminal-status" class="webshell-terminal-status idle">' + (wsT('webshell.terminalIdle') || '空闲') + '</span> ' +
        '<span class="webshell-quick-label">' + (wsT('webshell.quickCommands') || '快捷命令') + ':</span> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="whoami">whoami</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="id">id</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="pwd">pwd</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="ls -la">ls -la</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="uname -a">uname -a</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="ifconfig">ifconfig</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="ip a">ip a</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="env">env</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="hostname">hostname</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="ps aux">ps aux</button> ' +
        '<button type="button" class="btn-ghost btn-sm webshell-quick-cmd" data-cmd="netstat -tulnp">netstat</button>' +
        '</div>' +
        '<div class="webshell-terminal-shell">' +
        '<div id="webshell-terminal-sessions" class="webshell-terminal-sessions"></div>' +
        '<div id="webshell-terminal-container" class="webshell-terminal-container"></div>' +
        '</div>' +
        '</div>' +
        '<div id="webshell-pane-file" class="webshell-pane">' +
        '<div class="webshell-file-layout">' +
        '<aside class="webshell-file-sidebar">' +
        '<div class="webshell-file-sidebar-title">' + wsTOr('webshell.dirTree', '目录列表') + '</div>' +
        '<div id="webshell-dir-tree" class="webshell-dir-tree"></div>' +
        '</aside>' +
        '<section class="webshell-file-main">' +
        '<div class="webshell-file-toolbar">' +
        '<div class="webshell-file-breadcrumb" id="webshell-file-breadcrumb"></div>' +
        '<div class="webshell-file-toolbar-main">' +
        '<label class="webshell-file-path-field"><span>' + wsT('webshell.filePath') + '</span> <input type="text" id="webshell-file-path" class="form-control" value="." /></label>' +
        '<input type="text" id="webshell-file-filter" class="form-control webshell-file-filter" placeholder="' + (wsT('webshell.filterPlaceholder') || '过滤文件名') + '" />' +
        '<button type="button" class="btn-secondary" id="webshell-list-dir">' + wsT('webshell.listDir') + '</button>' +
        '<button type="button" class="btn-ghost" id="webshell-parent-dir">' + wsT('webshell.parentDir') + '</button>' +
        '</div>' +
        '<div class="webshell-file-toolbar-actions">' +
        '<button type="button" class="btn-ghost" id="webshell-file-refresh" title="' + (wsT('webshell.refresh') || '刷新') + '">' + (wsT('webshell.refresh') || '刷新') + '</button>' +
        '<details class="webshell-toolbar-actions">' +
        '<summary class="btn-ghost webshell-toolbar-actions-btn">' + (wsT('webshell.moreActions') || '更多操作') + '</summary>' +
        '<div class="webshell-row-actions-menu">' +
        '<button type="button" class="btn-ghost" id="webshell-mkdir-btn">' + (wsT('webshell.newDir') || '新建目录') + '</button>' +
        '<button type="button" class="btn-ghost" id="webshell-newfile-btn">' + (wsT('webshell.newFile') || '新建文件') + '</button>' +
        '<button type="button" class="btn-ghost" id="webshell-upload-btn">' + (wsT('webshell.upload') || '上传') + '</button>' +
        '<button type="button" class="btn-ghost" id="webshell-batch-delete-btn">' + (wsT('webshell.batchDelete') || '批量删除') + '</button>' +
        '<button type="button" class="btn-ghost" id="webshell-batch-download-btn">' + (wsT('webshell.batchDownload') || '批量下载') + '</button>' +
        '</div></details>' +
        '</div>' +
        '</div>' +
        '<div id="webshell-file-list" class="webshell-file-list"></div>' +
        '</section>' +
        '</div>' +
        '</div>' +
        '<div id="webshell-pane-ai" class="webshell-pane webshell-pane-ai-with-sidebar">' +
        '<div class="webshell-ai-sidebar">' +
        '<button type="button" class="btn-primary btn-sm webshell-ai-new-btn" id="webshell-ai-new-conv">' + (wsT('webshell.aiNewConversation') || '新对话') + '</button>' +
        '<div class="webshell-ai-conv-list" id="webshell-ai-conv-list"></div>' +
        '</div>' +
        '<div class="webshell-ai-main">' +
        '<div id="webshell-ai-messages" class="webshell-ai-messages"></div>' +
        '<div class="webshell-ai-input-area">' +
        '<div class="webshell-ai-selectors-row">' +
        '<div class="ws-project-selector-wrapper project-selector-wrapper">' +
        '<button type="button" id="ws-project-btn" class="role-selector-btn" onclick="wsToggleProjectPanel()" aria-label="' + escapeHtml(wsProjectT('projects.chatSelectorButton', '选择项目')) + '" aria-haspopup="listbox" aria-expanded="false" title="' + escapeHtml(wsProjectT('projects.chatSelectorButton', '绑定项目后共享事实黑板（跨对话）')) + '">' +
        '<span class="role-selector-icon" aria-hidden="true">📁</span>' +
        '<span id="ws-project-text" class="role-selector-text">' + escapeHtml(wsProjectT('projects.noProject', '无项目')) + '</span>' +
        '<svg class="role-selector-arrow" width="10" height="10" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>' +
        '</button>' +
        '<div id="ws-project-panel" class="role-selection-panel chat-project-panel" style="display:none;" role="listbox">' +
        '<div class="role-selection-panel-header">' +
        '<h3 class="role-selection-panel-title">' + escapeHtml(wsProjectT('projects.selectProject', '选择项目')) + '</h3>' +
        '<button type="button" class="role-selection-panel-close" onclick="wsCloseProjectPanel()" title="' + escapeHtml(wsProjectT('common.close', '关闭')) + '" aria-label="' + escapeHtml(wsProjectT('common.close', '关闭')) + '">' +
        '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></button>' +
        '</div>' +
        '<div class="chat-project-panel-body">' +
        '<div class="chat-project-panel-search">' +
        '<input type="search" id="ws-project-search" class="chat-project-panel-search-input" autocomplete="off" placeholder="' + escapeHtml(wsProjectT('projects.searchProjectsPlaceholder', '搜索项目…')) + '">' +
        '</div>' +
        '<div id="ws-project-list" class="role-selection-list-main"></div>' +
        '<div class="chat-project-panel-footer">' +
        ((typeof hasPermission === 'function' && hasPermission('project:write'))
            ? ('<button type="button" class="role-selection-item-main chat-project-panel-create-btn" onclick="showNewProjectModalFromWebshellAi()">' +
                '<span class="chat-project-panel-create-icon" aria-hidden="true">+</span>' +
                '<span class="chat-project-panel-create-label">' + escapeHtml(wsProjectT('projects.newProject', '新建项目')) + '</span>' +
                '</button>')
            : '') +
        '</div></div></div></div>' +
        '<div class="ws-role-selector-wrapper">' +
        '<button type="button" class="role-selector-btn ws-role-selector-btn" id="ws-role-selector-btn" onclick="wsToggleRolePanel()">' +
        '<span id="ws-role-selector-icon" class="role-selector-icon">\ud83d\udd35</span>' +
        '<span id="ws-role-selector-text" class="role-selector-text">' + (wsT('chat.defaultRole') || 'Default') + '</span>' +
        '<svg class="role-selector-arrow" width="10" height="10" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>' +
        '</button>' +
        '<div id="ws-role-selection-panel" class="role-selection-panel" style="display:none;">' +
        '<div class="role-selection-panel-header"><h3 class="role-selection-panel-title">' + (wsT('chat.rolePanelTitle') || '选择角色') + '</h3>' +
        '<button type="button" class="role-selection-panel-close" onclick="wsCloseRolePanel()"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></button>' +
        '</div><div id="ws-role-selection-list" class="role-selection-list-main"></div></div>' +
        '</div>' +
        '<div class="ws-agent-mode-wrapper" id="ws-agent-mode-wrapper" style="display:none;">' +
        '<div class="agent-mode-inner">' +
        '<button type="button" class="role-selector-btn agent-mode-btn" id="ws-agent-mode-btn" onclick="wsToggleAgentModePanel()">' +
        '<span id="ws-agent-mode-icon" class="role-selector-icon">\ud83e\udd16</span>' +
        '<span id="ws-agent-mode-text" class="role-selector-text">' + (wsT('chat.agentModeEinoSingle') || 'Eino 单代理') + '</span>' +
        '<svg class="role-selector-arrow" width="10" height="10" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>' +
        '</button>' +
        '<div id="ws-agent-mode-panel" class="agent-mode-panel" style="display:none;" role="listbox">' +
        '<div class="role-selection-panel-header agent-mode-panel-header"><h3 class="role-selection-panel-title">' + (wsT('chat.agentModePanelTitle') || '对话模式') + '</h3>' +
        '<button type="button" class="role-selection-panel-close" onclick="wsCloseAgentModePanel()"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></button>' +
        '</div>' +
        '<div class="agent-mode-options">' +
        '<button type="button" class="role-selection-item-main agent-mode-option ws-agent-mode-option" data-value="eino_single" role="option" onclick="wsSelectAgentMode(\'eino_single\')" data-agent-mode-detail="' + escapeHtmlAttr(wsT('chat.agentModeEinoSingleHint') || 'Eino ChatModelAgent + Runner') + '"><div class="role-selection-item-icon-main">\u26a1</div><div class="role-selection-item-content-main"><div class="role-selection-item-name-main">' + (wsT('chat.agentModeEinoSingle') || 'Eino 单代理（ADK）') + '</div><div class="role-selection-item-description-main">' + (wsT('chat.agentModeEinoSingleHint') || 'Eino ChatModelAgent + Runner') + '</div></div><div class="role-selection-checkmark-main agent-mode-check" data-agent-mode-check="eino_single">\u2713</div></button>' +
        '<button type="button" class="role-selection-item-main agent-mode-option ws-agent-mode-option" data-value="deep" role="option" onclick="wsSelectAgentMode(\'deep\')" data-agent-mode-detail="' + escapeHtmlAttr(wsT('chat.agentModeDeepHint') || 'Eino DeepAgent，适合复杂安全测试、多阶段 task 子代理委派与汇总') + '"><div class="role-selection-item-icon-main">\ud83e\udde9</div><div class="role-selection-item-content-main"><div class="role-selection-item-name-main">' + (wsT('chat.agentModeDeep') || 'Deep（DeepAgent）') + '</div><div class="role-selection-item-description-main">' + (wsT('chat.agentModeDeepHint') || 'Eino DeepAgent，适合复杂安全测试、多阶段 task 子代理委派与汇总') + '</div></div><div class="role-selection-checkmark-main agent-mode-check" data-agent-mode-check="deep">\u2713</div></button>' +
        '<button type="button" class="role-selection-item-main agent-mode-option ws-agent-mode-option" data-value="plan_execute" role="option" onclick="wsSelectAgentMode(\'plan_execute\')" data-agent-mode-detail="' + escapeHtmlAttr(wsT('chat.agentModePlanExecuteHint') || '规划 → 执行 → 重规划') + '"><div class="role-selection-item-icon-main">\ud83d\udccb</div><div class="role-selection-item-content-main"><div class="role-selection-item-name-main">' + (wsT('chat.agentModePlanExecuteLabel') || 'Plan-Execute') + '</div><div class="role-selection-item-description-main">' + (wsT('chat.agentModePlanExecuteHint') || '规划 → 执行 → 重规划') + '</div></div><div class="role-selection-checkmark-main agent-mode-check" data-agent-mode-check="plan_execute">\u2713</div></button>' +
        '<button type="button" class="role-selection-item-main agent-mode-option ws-agent-mode-option" data-value="supervisor" role="option" onclick="wsSelectAgentMode(\'supervisor\')" data-agent-mode-detail="' + escapeHtmlAttr(wsT('chat.agentModeSupervisorHint') || '专家路由场景：监督者通过 transfer 动态分派多个专业子代理') + '"><div class="role-selection-item-icon-main">\ud83c\udfaf</div><div class="role-selection-item-content-main"><div class="role-selection-item-name-main">' + (wsT('chat.agentModeSupervisorLabel') || 'Supervisor（专家路由）') + '</div><div class="role-selection-item-description-main">' + (wsT('chat.agentModeSupervisorHint') || '专家路由场景：监督者通过 transfer 动态分派多个专业子代理') + '</div></div><div class="role-selection-checkmark-main agent-mode-check" data-agent-mode-check="supervisor">\u2713</div></button>' +
        '</div></div></div>' +
        '<input type="hidden" id="ws-agent-mode-select" value="eino_single" autocomplete="off" />' +
        '</div>' +
        '</div>' +
        '<div class="webshell-ai-input-row">' +
        '<textarea id="webshell-ai-input" class="webshell-ai-input form-control" rows="2" placeholder="' + (wsT('webshell.aiPlaceholder') || '例如：列出当前目录下的文件') + '"></textarea>' +
        '<button type="button" class="btn-primary" id="webshell-ai-send">' + (wsT('webshell.aiSend') || '发送') + '</button>' +
        '<button type="button" class="btn-danger webshell-ai-stop-btn" id="webshell-ai-stop" style="display:none;">' + wsTOr('webshell.aiStop', '停止') + '</button>' +
        '</div>' +
        '</div>' +
        '</div>' +
        '</div>' +
        '<div id="webshell-pane-memo" class="webshell-pane webshell-pane-memo">' +
        '<div class="webshell-memo-layout">' +
        '<div class="webshell-memo-head"><span>' + (wsT('webshell.aiMemo') || '备忘录') + '</span><button type="button" class="btn-ghost btn-sm" id="webshell-ai-memo-clear">' + (wsT('webshell.aiMemoClear') || '清空') + '</button></div>' +
        '<textarea id="webshell-ai-memo-input" class="webshell-memo-input form-control" rows="18" placeholder="' + (wsT('webshell.aiMemoPlaceholder') || '记录关键命令、测试思路、复现步骤...') + '"></textarea>' +
        '<div id="webshell-ai-memo-status" class="webshell-memo-status">' + (wsT('webshell.aiMemoSaved') || '已保存到本地') + '</div>' +
        '</div>' +
        '</div>' +
        '<div id="webshell-pane-db" class="webshell-pane webshell-pane-db">' +
        '<div class="webshell-db-profiles-bar"><div id="webshell-db-profiles" class="webshell-db-profiles"></div><div class="webshell-db-profile-actions"><button type="button" class="btn-ghost btn-sm" id="webshell-db-add-profile-btn">+ ' + (wsT('webshell.dbAddProfile') || '新增连接') + '</button></div></div>' +
        '<div class="webshell-db-layout">' +
        '<aside class="webshell-db-sidebar">' +
        '<div class="webshell-db-sidebar-head"><span>' + (wsT('webshell.dbSchema') || '数据库结构') + '</span><button type="button" class="btn-ghost btn-sm" id="webshell-db-load-schema-btn">' + (wsT('webshell.dbLoadSchema') || '加载结构') + '</button></div>' +
        '<div id="webshell-db-schema-tree" class="webshell-db-schema-tree"><div class="webshell-empty">' + (wsT('webshell.dbNoSchema') || '暂无数据库结构，请先加载') + '</div></div>' +
        '<div class="webshell-db-sidebar-hint">' + (wsT('webshell.dbSelectTableHint') || '点击表名可生成查询 SQL') + '</div>' +
        '</aside>' +
        '<section class="webshell-db-main">' +
        '<div class="webshell-db-sql-tools"><button type="button" class="btn-ghost btn-sm" id="webshell-db-template-btn">' + (wsT('webshell.dbTemplateSql') || '示例 SQL') + '</button><button type="button" class="btn-ghost btn-sm" id="webshell-db-clear-btn">' + (wsT('webshell.dbClearSql') || '清空 SQL') + '</button></div>' +
        '<textarea id="webshell-db-sql" class="webshell-db-sql form-control" rows="8" placeholder="' + (wsT('webshell.dbSqlPlaceholder') || '输入 SQL，例如：SELECT version();') + '"></textarea>' +
        '<div class="webshell-db-actions">' +
        '<button type="button" class="btn-ghost" id="webshell-db-test-btn">' + (wsT('webshell.dbTest') || '测试连接') + '</button>' +
        '<button type="button" class="btn-primary" id="webshell-db-run-btn">' + (wsT('webshell.dbRunSql') || '执行 SQL') + '</button>' +
        '</div>' +
        '<div class="webshell-db-output-wrap"><div class="webshell-db-output-title">' + (wsT('webshell.dbOutput') || '执行输出') + '</div><div id="webshell-db-result-table" class="webshell-db-result-table"></div><pre id="webshell-db-output" class="webshell-db-output"></pre><div class="webshell-db-hint">' + (wsT('webshell.dbCliHint') || '如果提示命令不存在，请先在目标主机安装对应客户端（mysql/psql/sqlite3/sqlcmd）') + '</div></div>' +
        '<div id="webshell-db-profile-modal" class="modal">' +
        '<div class="modal-content webshell-db-profile-modal-content">' +
        '<div class="modal-header"><h2 id="webshell-db-profile-modal-title">' + (wsT('webshell.editConnectionTitle') || '编辑连接') + '</h2><span class="modal-close" id="webshell-db-profile-modal-close">&times;</span></div>' +
        '<div class="modal-body">' +
        '<div class="webshell-db-toolbar">' +
        '<label><span>' + (wsT('webshell.dbProfileName') || '连接名称') + '</span><input id="webshell-db-profile-name" class="form-control" type="text" maxlength="30" /></label>' +
        '<label><span>' + (wsT('webshell.dbType') || '数据库类型') + '</span><select id="webshell-db-type" class="form-control"><option value="mysql">MySQL</option><option value="pgsql">PostgreSQL</option><option value="sqlite">SQLite</option><option value="mssql">SQL Server</option></select></label>' +
        '<label class="webshell-db-common-field"><span>' + (wsT('webshell.dbHost') || '主机') + '</span><input id="webshell-db-host" class="form-control" type="text" value="127.0.0.1" /></label>' +
        '<label class="webshell-db-common-field"><span>' + (wsT('webshell.dbPort') || '端口') + '</span><input id="webshell-db-port" class="form-control" type="text" /></label>' +
        '<label class="webshell-db-common-field"><span>' + (wsT('webshell.dbUsername') || '用户名') + '</span><input id="webshell-db-user" class="form-control" type="text" /></label>' +
        '<label class="webshell-db-common-field"><span>' + (wsT('webshell.dbPassword') || '密码') + '</span><input id="webshell-db-pass" class="form-control" type="password" /></label>' +
        '<label class="webshell-db-common-field"><span>' + (wsT('webshell.dbName') || '数据库名') + '</span><input id="webshell-db-name" class="form-control" type="text" /></label>' +
        '<label id="webshell-db-sqlite-row"><span>' + (wsT('webshell.dbSqlitePath') || 'SQLite 文件路径') + '</span><input id="webshell-db-sqlite-path" class="form-control" type="text" value="/tmp/test.db" /></label>' +
        '</div>' +
        '</div>' +
        '<div class="modal-footer"><button type="button" class="btn-secondary" id="webshell-db-profile-cancel-btn">' + (wsT('common.cancel') || '取消') + '</button><button type="button" class="btn-primary" id="webshell-db-profile-save-btn">' + (wsT('common.save') || '保存') + '</button></div>' +
        '</div>' +
        '</div>' +
        '</section>' +
        '</div>' +
        '</div>';

    // Tab 切换
    workspace.querySelectorAll('.webshell-tab').forEach(btn => {
        btn.addEventListener('click', function () {
            const tab = btn.getAttribute('data-tab');
            workspace.querySelectorAll('.webshell-tab').forEach(b => b.classList.remove('active'));
            workspace.querySelectorAll('.webshell-pane').forEach(p => p.classList.remove('active'));
            btn.classList.add('active');
            const pane = document.getElementById('webshell-pane-' + tab);
            if (pane) pane.classList.add('active');
            if (tab === 'terminal' && webshellTerminalInstance && webshellTerminalFitAddon) {
                try { webshellTerminalFitAddon.fit(); } catch (e) {}
            }
            if (tab === 'ai') {
                try { wsRefreshSelectors(); } catch (e) {}
            }
        });
    });

    // 文件管理：列出目录、上级目录
    const pathInput = document.getElementById('webshell-file-path');
    document.getElementById('webshell-list-dir').addEventListener('click', function () {
        // 点击时用当前连接，编辑保存后立即生效
        webshellFileListDir(webshellCurrentConn, pathInput ? pathInput.value.trim() || '.' : '.');
    });
    document.getElementById('webshell-parent-dir').addEventListener('click', function () {
        const p = (pathInput && pathInput.value.trim()) || '.';
        pathInput.value = getWebshellParentPath(p);
        webshellFileListDir(webshellCurrentConn, pathInput.value || '.');
    });

    // 清屏由 bindWebshellClearOnce 统一事件委托处理，此处不再绑定，避免重复绑定导致一次点击出现多个 shell>
    var terminalCopyLogBtn = document.getElementById('webshell-terminal-copy-log');
    if (terminalCopyLogBtn) {
        terminalCopyLogBtn.addEventListener('click', function () {
            if (!webshellCurrentConn || !webshellCurrentConn.id) return;
            var activeId = getActiveWebshellTerminalSessionId(webshellCurrentConn.id);
            var log = getWebshellTerminalLog(getWebshellTerminalSessionKey(webshellCurrentConn.id, activeId)) || '';
            if (navigator && navigator.clipboard && navigator.clipboard.writeText) {
                navigator.clipboard.writeText(log).then(function () {
                    terminalCopyLogBtn.title = wsT('webshell.terminalCopyOk') || '日志已复制';
                    setTimeout(function () {
                        terminalCopyLogBtn.title = wsT('webshell.copyTerminalLog') || '复制日志';
                    }, 1200);
                }).catch(function () {
                    terminalCopyLogBtn.title = wsT('webshell.terminalCopyFail') || '复制失败';
                });
                return;
            }
            try {
                var ta = document.createElement('textarea');
                ta.value = log;
                document.body.appendChild(ta);
                ta.select();
                document.execCommand('copy');
                document.body.removeChild(ta);
            } catch (e) {}
        });
    }
    renderWebshellTerminalSessions(conn);
    // 快捷命令：点击后执行并输出到终端
    workspace.querySelectorAll('.webshell-quick-cmd').forEach(function (btn) {
        btn.addEventListener('click', function () {
            var cmd = btn.getAttribute('data-cmd');
            if (cmd) runQuickCommand(cmd);
        });
    });
    // 文件：刷新、新建目录、新建文件、上传、批量操作
    var filterInput = document.getElementById('webshell-file-filter');
    document.getElementById('webshell-file-refresh').addEventListener('click', function () {
        webshellFileListDir(webshellCurrentConn, pathInput ? pathInput.value.trim() || '.' : '.');
    });
    if (filterInput) filterInput.addEventListener('input', function () {
        webshellFileListApplyFilter();
    });
    document.getElementById('webshell-mkdir-btn').addEventListener('click', function () { webshellFileMkdir(webshellCurrentConn, pathInput); });
    document.getElementById('webshell-newfile-btn').addEventListener('click', function () { webshellFileNewFile(webshellCurrentConn, pathInput); });
    document.getElementById('webshell-upload-btn').addEventListener('click', function () { webshellFileUpload(webshellCurrentConn, pathInput); });
    document.getElementById('webshell-batch-delete-btn').addEventListener('click', function () { webshellBatchDelete(webshellCurrentConn, pathInput); });
    document.getElementById('webshell-batch-download-btn').addEventListener('click', function () { webshellBatchDownload(webshellCurrentConn, pathInput); });

    // AI 助手：侧边栏对话列表 + 主区消息
    var aiInput = document.getElementById('webshell-ai-input');
    var aiSendBtn = document.getElementById('webshell-ai-send');
    var aiMessages = document.getElementById('webshell-ai-messages');
    var aiNewConvBtn = document.getElementById('webshell-ai-new-conv');
    var aiConvListEl = document.getElementById('webshell-ai-conv-list');

    // 初始化角色 + 模式 + 项目选择器
    wsLoadRoles();
    wsInitAgentMode();
    if (typeof prefetchProjectsForChat === 'function') prefetchProjectsForChat();
    wsUpdateProjectButtonLabel();
    var aiMemoInput = document.getElementById('webshell-ai-memo-input');
    var aiMemoStatus = document.getElementById('webshell-ai-memo-status');
    var aiMemoClearBtn = document.getElementById('webshell-ai-memo-clear');
    var aiMemoSaveTimer = null;

    function setWebshellAiMemoStatus(text, isError) {
        if (!aiMemoStatus) return;
        aiMemoStatus.textContent = text || '';
        aiMemoStatus.classList.toggle('error', !!isError);
    }

    function flushWebshellAiMemo() {
        if (!aiMemoInput) return;
        saveWebshellAiMemo(conn, aiMemoInput.value || '');
        setWebshellAiMemoStatus(wsT('webshell.aiMemoSaved') || '已保存到本地', false);
    }

    if (aiMemoInput) {
        aiMemoInput.value = getWebshellAiMemo(conn);
        setWebshellAiMemoStatus(wsT('webshell.aiMemoSaved') || '已保存到本地', false);
        aiMemoInput.addEventListener('input', function () {
            setWebshellAiMemoStatus(wsT('webshell.aiMemoSaving') || '保存中...', false);
            if (aiMemoSaveTimer) clearTimeout(aiMemoSaveTimer);
            aiMemoSaveTimer = setTimeout(function () {
                aiMemoSaveTimer = null;
                flushWebshellAiMemo();
            }, 500);
        });
        aiMemoInput.addEventListener('blur', function () {
            if (aiMemoSaveTimer) {
                clearTimeout(aiMemoSaveTimer);
                aiMemoSaveTimer = null;
            }
            flushWebshellAiMemo();
        });
    }

    if (aiMemoClearBtn && aiMemoInput) {
        aiMemoClearBtn.addEventListener('click', function () {
            aiMemoInput.value = '';
            flushWebshellAiMemo();
            aiMemoInput.focus();
        });
    }

    if (aiNewConvBtn) {
        aiNewConvBtn.addEventListener('click', function () {
            delete webshellAiConvMap[conn.id];
            delete webshellAiDraftProjectByConn[conn.id];
            wsUpdateProjectButtonLabel();
            if (aiMessages) {
                aiMessages.innerHTML = '';
                var readyMsg = wsT('webshell.aiSystemReadyMessage') || '系统已就绪。请输入您的测试需求，系统将自动执行相应的安全测试。';
                var div = document.createElement('div');
                div.className = 'webshell-ai-msg assistant';
                div.textContent = readyMsg;
                aiMessages.appendChild(div);
            }
            if (aiConvListEl) aiConvListEl.querySelectorAll('.webshell-ai-conv-item').forEach(function (el) { el.classList.remove('active'); });
        });
    }
    if (aiSendBtn && aiInput && aiMessages) {
        var aiStopBtn = document.getElementById('webshell-ai-stop');
        aiSendBtn.addEventListener('click', function () { runWebshellAiSend(conn, aiInput, aiSendBtn, aiMessages); });
        if (aiStopBtn) {
            aiStopBtn.addEventListener('click', function () { wsStopAiStream(conn); });
        }
        aiInput.addEventListener('keydown', function (e) {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                runWebshellAiSend(conn, aiInput, aiSendBtn, aiMessages);
            }
        });
        fetchAndRenderWebshellAiConvList(conn, aiConvListEl).then(function () {
            loadWebshellAiHistory(conn, aiMessages).then(function () {
                if (webshellAiConvMap[conn.id] && aiConvListEl) {
                    aiConvListEl.querySelectorAll('.webshell-ai-conv-item').forEach(function (el) {
                        el.classList.toggle('active', el.dataset.convId === webshellAiConvMap[conn.id]);
                    });
                }
            });
        });
    }

    // 数据库管理：支持多连接工作区（不同数据库类型并存）+ 刷新持久化
    var dbTypeEl = document.getElementById('webshell-db-type');
    var dbRunBtn = document.getElementById('webshell-db-run-btn');
    var dbTestBtn = document.getElementById('webshell-db-test-btn');
    var dbSqlEl = document.getElementById('webshell-db-sql');
    var dbLoadSchemaBtn = document.getElementById('webshell-db-load-schema-btn');
    var dbTemplateBtn = document.getElementById('webshell-db-template-btn');
    var dbClearBtn = document.getElementById('webshell-db-clear-btn');
    var dbSchemaTreeEl = document.getElementById('webshell-db-schema-tree');
    var dbProfilesEl = document.getElementById('webshell-db-profiles');
    var dbAddProfileBtn = document.getElementById('webshell-db-add-profile-btn');
    var dbProfileModalEl = document.getElementById('webshell-db-profile-modal');
    var dbProfileModalTitleEl = document.getElementById('webshell-db-profile-modal-title');
    var dbProfileModalCloseBtn = document.getElementById('webshell-db-profile-modal-close');
    var dbProfileModalCancelBtn = document.getElementById('webshell-db-profile-cancel-btn');
    var dbProfileModalSaveBtn = document.getElementById('webshell-db-profile-save-btn');
    var dbProfileNameEl = document.getElementById('webshell-db-profile-name');
    var dbHostEl = document.getElementById('webshell-db-host');
    var dbPortEl = document.getElementById('webshell-db-port');
    var dbUserEl = document.getElementById('webshell-db-user');
    var dbPassEl = document.getElementById('webshell-db-pass');
    var dbNameEl = document.getElementById('webshell-db-name');
    var dbSqliteEl = document.getElementById('webshell-db-sqlite-path');
    var dbColumnsLoading = {};
    var dbColumnsBatchLoading = {};
    var dbColumnsBatchLoaded = {};

    function resetDbColumnLoadCache() {
        dbColumnsLoading = {};
        dbColumnsBatchLoading = {};
        dbColumnsBatchLoaded = {};
    }

    function setDbActionButtonsDisabled(disabled) {
        if (dbRunBtn) dbRunBtn.disabled = disabled;
        if (dbTestBtn) dbTestBtn.disabled = disabled;
        if (dbLoadSchemaBtn) dbLoadSchemaBtn.disabled = disabled;
        if (dbAddProfileBtn) dbAddProfileBtn.disabled = disabled;
    }

    function setDbProfileModalVisible(visible, mode) {
        if (!dbProfileModalEl) return;
        if (visible) {
            if (dbProfileModalTitleEl) {
                if (mode === 'add') dbProfileModalTitleEl.textContent = wsT('webshell.dbAddProfile') || '新增连接';
                else dbProfileModalTitleEl.textContent = wsT('webshell.editConnectionTitle') || '编辑连接';
            }
            openAppModal(dbProfileModalEl);
        } else {
            closeAppModal(dbProfileModalEl);
        }
    }

    function applyActiveDbProfileToForm() {
        var dbCfg = getWebshellDbConfig(conn);
        if (!dbCfg) return;
        if (dbProfileNameEl) dbProfileNameEl.value = dbCfg.name || 'DB-1';
        if (dbTypeEl) dbTypeEl.value = dbCfg.type || 'mysql';
        if (dbHostEl) dbHostEl.value = dbCfg.host || '127.0.0.1';
        if (dbPortEl) dbPortEl.value = dbCfg.port || '';
        if (dbUserEl) dbUserEl.value = dbCfg.username || '';
        if (dbPassEl) dbPassEl.value = dbCfg.password || '';
        if (dbNameEl) dbNameEl.value = dbCfg.database || dbCfg.selectedDatabase || '';
        if (dbSqliteEl) dbSqliteEl.value = dbCfg.sqlitePath || '/tmp/test.db';
        if (dbSqlEl) dbSqlEl.value = dbCfg.sql || 'SELECT 1;';
        webshellDbUpdateFieldVisibility();
        webshellDbSetOutput(dbCfg.output || '', !!dbCfg.outputIsError);
        webshellDbRenderTable(dbCfg.output || '');
    }

    function renderDbProfileTabs() {
        if (!dbProfilesEl) return;
        var state = getWebshellDbState(conn);
        var html = '';
        state.profiles.forEach(function (p) {
            var active = p.id === state.activeProfileId;
            html += '<div class="webshell-db-profile-tab' + (active ? ' active' : '') + '" data-id="' + escapeHtml(p.id) + '">' +
                '<button type="button" class="webshell-db-profile-main" data-action="switch" data-id="' + escapeHtml(p.id) + '">' + escapeHtml(p.name || 'DB') + '</button>' +
                '<button type="button" class="webshell-db-profile-menu" data-action="edit" data-id="' + escapeHtml(p.id) + '" title="' + escapeHtml(wsT('webshell.editConnection') || '编辑') + '">⚙</button>' +
                '<button type="button" class="webshell-db-profile-menu" data-action="delete" data-id="' + escapeHtml(p.id) + '" title="' + escapeHtml(wsT('webshell.dbDeleteProfile') || '删除连接') + '">×</button>' +
                '</div>';
        });
        dbProfilesEl.innerHTML = html;
        dbProfilesEl.querySelectorAll('[data-action]').forEach(function (btn) {
            btn.addEventListener('click', function () {
                var action = btn.getAttribute('data-action');
                var id = btn.getAttribute('data-id') || '';
                if (!id) return;
                var state = getWebshellDbState(conn);
                var idx = state.profiles.findIndex(function (p) { return p.id === id; });
                if (idx < 0) return;
                if (action === 'switch') {
                    state.activeProfileId = id;
                    saveWebshellDbState(conn, state);
                    applyActiveDbProfileToForm();
                    renderDbProfileTabs();
                    resetDbColumnLoadCache();
                    renderDbSchemaTree();
                    return;
                }
                if (action === 'edit') {
                    state.activeProfileId = id;
                    saveWebshellDbState(conn, state);
                    applyActiveDbProfileToForm();
                    renderDbProfileTabs();
                    setDbProfileModalVisible(true, 'edit');
                    return;
                }
                if (action === 'delete') {
                    if (state.profiles.length <= 1) return;
                    if (!confirm(wsT('webshell.dbDeleteProfileConfirm') || '确定删除该数据库连接配置吗？')) return;
                    state.profiles.splice(idx, 1);
                    if (!state.profiles.some(function (p) { return p.id === state.activeProfileId; })) {
                        state.activeProfileId = state.profiles[0].id;
                    }
                    saveWebshellDbState(conn, state);
                    applyActiveDbProfileToForm();
                    renderDbProfileTabs();
                    resetDbColumnLoadCache();
                    renderDbSchemaTree();
                }
            });
        });
    }

    function renderDbSchemaTree() {
        if (!dbSchemaTreeEl) return;
        var cfg = getWebshellDbConfig(conn);
        var schema = normalizeWebshellDbSchema((cfg && cfg.schema && typeof cfg.schema === 'object') ? cfg.schema : {});
        var dbs = Object.keys(schema).sort(function (a, b) { return a.localeCompare(b); });
        var openTableKeys = {};
        dbSchemaTreeEl.querySelectorAll('.webshell-db-table-node[open]').forEach(function (node) {
            var openDb = node.getAttribute('data-db') || '';
            var openTable = node.getAttribute('data-table') || '';
            if (!openDb || !openTable) return;
            openTableKeys[openDb + '::' + openTable] = true;
        });
        if (!dbs.length) {
            dbSchemaTreeEl.innerHTML = '<div class="webshell-empty">' + escapeHtml(wsT('webshell.dbNoSchema') || '暂无数据库结构，请先加载') + '</div>';
            return;
        }
        var selectedDb = (cfg.selectedDatabase || '').trim();
        var html = '';
        dbs.forEach(function (dbName) {
            var tables = (schema[dbName] && schema[dbName].tables) ? schema[dbName].tables : {};
            var tableNames = Object.keys(tables).sort(function (a, b) { return a.localeCompare(b); });
            var isActive = selectedDb && selectedDb === dbName;
            html += '<details class="webshell-db-group"' + (isActive ? ' open' : '') + '>';
            html += '<summary class="webshell-db-group-title" data-db="' + escapeHtml(dbName) + '" title="' + escapeHtml(dbName) + '"><span class="webshell-db-icon">🗄</span><span class="webshell-db-label">' + escapeHtml(dbName) + '</span><span class="webshell-db-count">' + tableNames.length + '</span></summary>';
            html += '<div class="webshell-db-group-items">';
            tableNames.forEach(function (tableName) {
                var columns = Array.isArray(tables[tableName]) ? tables[tableName] : [];
                var columnCountText = columns.length > 0 ? String(columns.length) : '-';
                var tableKey = dbName + '::' + tableName;
                var tableOpen = !!openTableKeys[tableKey];
                html += '<details class="webshell-db-table-node" data-db="' + escapeHtml(dbName) + '" data-table="' + escapeHtml(tableName) + '" data-columns-loaded="' + (columns.length ? '1' : '0') + '"' + (tableOpen ? ' open' : '') + '>';
                html += '<summary class="webshell-db-table-item" data-db="' + escapeHtml(dbName) + '" data-table="' + escapeHtml(tableName) + '" title="' + escapeHtml(tableName) + '"><span class="webshell-db-icon">📄</span><span class="webshell-db-label">' + escapeHtml(tableName) + '</span><span class="webshell-db-count">' + escapeHtml(columnCountText) + '</span></summary>';
                if (columns.length) {
                    html += '<div class="webshell-db-column-list">';
                    columns.forEach(function (columnName) {
                        html += '<button type="button" class="webshell-db-column-item" data-db="' + escapeHtml(dbName) + '" data-table="' + escapeHtml(tableName) + '" data-column="' + escapeHtml(columnName) + '" title="' + escapeHtml(columnName) + '"><span class="webshell-db-icon">🧱</span><span class="webshell-db-label">' + escapeHtml(columnName) + '</span></button>';
                    });
                    html += '</div>';
                } else {
                    html += '<div class="webshell-db-column-empty">' + escapeHtml(wsT('webshell.dbNoColumns') || '暂无列信息') + '</div>';
                }
                html += '</details>';
            });
            html += '</div></details>';
        });
        dbSchemaTreeEl.innerHTML = html;

        dbSchemaTreeEl.querySelectorAll('.webshell-db-group-title').forEach(function (el) {
            el.addEventListener('click', function () {
                var cfg = webshellDbCollectConfig(conn);
                cfg.selectedDatabase = el.getAttribute('data-db') || '';
                saveWebshellDbConfig(conn, cfg);
                if (dbNameEl && cfg.type !== 'sqlite') dbNameEl.value = cfg.selectedDatabase;
                ensureDbDatabaseColumns(cfg.selectedDatabase);
            });
        });
        dbSchemaTreeEl.querySelectorAll('.webshell-db-table-item').forEach(function (el) {
            el.addEventListener('click', function () {
                var table = el.getAttribute('data-table') || '';
                var dbName = el.getAttribute('data-db') || '';
                if (!table) return;
                var cfg = webshellDbCollectConfig(conn);
                cfg.selectedDatabase = dbName;
                if (cfg.type !== 'sqlite') cfg.database = dbName;
                saveWebshellDbConfig(conn, cfg);
                if (dbNameEl && cfg.type !== 'sqlite') dbNameEl.value = dbName;
                var tableRef = cfg.type === 'sqlite'
                    ? webshellDbQuoteIdentifier(cfg.type, table)
                    : webshellDbQuoteIdentifier(cfg.type, dbName) + '.' + webshellDbQuoteIdentifier(cfg.type, table);
                if (dbSqlEl) {
                    dbSqlEl.value = 'SELECT * FROM ' + tableRef + ' ORDER BY 1 DESC LIMIT 20;';
                    webshellDbCollectConfig(conn);
                }
                ensureDbTableColumns(dbName, table);
            });
        });
        dbSchemaTreeEl.querySelectorAll('.webshell-db-table-node').forEach(function (node) {
            node.addEventListener('toggle', function () {
                if (!node.open) return;
                var dbName = node.getAttribute('data-db') || '';
                var table = node.getAttribute('data-table') || '';
                if (!dbName || !table) return;
                ensureDbTableColumns(dbName, table);
            });
        });
        dbSchemaTreeEl.querySelectorAll('.webshell-db-column-item').forEach(function (el) {
            el.addEventListener('click', function (evt) {
                evt.preventDefault();
                evt.stopPropagation();
                var table = el.getAttribute('data-table') || '';
                var column = el.getAttribute('data-column') || '';
                var dbName = el.getAttribute('data-db') || '';
                if (!table || !column) return;
                var cfg = webshellDbCollectConfig(conn);
                cfg.selectedDatabase = dbName;
                if (cfg.type !== 'sqlite') cfg.database = dbName;
                saveWebshellDbConfig(conn, cfg);
                if (dbNameEl && cfg.type !== 'sqlite') dbNameEl.value = dbName;
                var tableRef = cfg.type === 'sqlite'
                    ? webshellDbQuoteIdentifier(cfg.type, table)
                    : webshellDbQuoteIdentifier(cfg.type, dbName) + '.' + webshellDbQuoteIdentifier(cfg.type, table);
                if (dbSqlEl) {
                    dbSqlEl.value = 'SELECT ' + webshellDbQuoteIdentifier(cfg.type, column) + ' FROM ' + tableRef + ' LIMIT 20;';
                    webshellDbCollectConfig(conn);
                }
            });
        });
        var autoDb = selectedDb || dbs[0] || '';
        if (autoDb) ensureDbDatabaseColumns(autoDb);
    }

    function ensureDbTableColumns(dbName, tableName) {
        if (!dbName || !tableName || webshellRunning) return;
        var loadKey = (conn && conn.id ? conn.id : 'local') + '::' + dbName + '::' + tableName;
        if (dbColumnsLoading[loadKey]) return;
        webshellDbCollectConfig(conn);
        var cfg = getWebshellDbConfig(conn);
        var schema = normalizeWebshellDbSchema((cfg && cfg.schema && typeof cfg.schema === 'object') ? cfg.schema : {});
        if (!schema[dbName]) return;
        if (!schema[dbName].tables[tableName]) return;
        if (Array.isArray(schema[dbName].tables[tableName]) && schema[dbName].tables[tableName].length > 0) return;

        var built = buildWebshellDbColumnsCommand(cfg, dbName, tableName);
        if (!built.command) return;
        dbColumnsLoading[loadKey] = true;
        webshellRunning = true;
        setDbActionButtonsDisabled(true);
        execWebshellCommand(conn, built.command).then(function (out) {
            var parsed = parseWebshellDbExecOutput(out);
            var success = parsed.rc === 0 || (parsed.rc == null && parsed.output && !/error|failed|denied|unknown|not found|access/i.test(parsed.output));
            if (!success) return;
            var columns = parseWebshellDbColumns(parsed.output);
            if (!columns.length) return;
            var nextCfg = getWebshellDbConfig(conn);
            var nextSchema = normalizeWebshellDbSchema((nextCfg && nextCfg.schema && typeof nextCfg.schema === 'object') ? nextCfg.schema : {});
            if (!nextSchema[dbName]) nextSchema[dbName] = { tables: {} };
            if (!nextSchema[dbName].tables[tableName]) nextSchema[dbName].tables[tableName] = [];
            nextSchema[dbName].tables[tableName] = columns;
            nextCfg.schema = nextSchema;
            saveWebshellDbConfig(conn, nextCfg);
            renderDbSchemaTree();
        }).catch(function () {
            // ignore single-table column load errors to avoid interrupting main flow
        }).finally(function () {
            delete dbColumnsLoading[loadKey];
            webshellRunning = false;
            setDbActionButtonsDisabled(false);
        });
    }

    function ensureDbDatabaseColumns(dbName) {
        if (!dbName || webshellRunning) return;
        webshellDbCollectConfig(conn);
        var cfg = getWebshellDbConfig(conn);
        var schema = normalizeWebshellDbSchema((cfg && cfg.schema && typeof cfg.schema === 'object') ? cfg.schema : {});
        if (!schema[dbName] || !schema[dbName].tables) return;
        var hasUnknown = Object.keys(schema[dbName].tables).some(function (tableName) {
            var cols = schema[dbName].tables[tableName];
            return !Array.isArray(cols) || cols.length === 0;
        });
        if (!hasUnknown) return;

        var batchKey = (conn && conn.id ? conn.id : 'local') + '::' + (cfg.type || 'mysql') + '::' + dbName;
        if (dbColumnsBatchLoading[batchKey] || dbColumnsBatchLoaded[batchKey]) return;
        var built = buildWebshellDbColumnsByDatabaseCommand(cfg, dbName);
        if (!built.command) return;

        dbColumnsBatchLoading[batchKey] = true;
        webshellRunning = true;
        setDbActionButtonsDisabled(true);
        execWebshellCommand(conn, built.command).then(function (out) {
            var parsed = parseWebshellDbExecOutput(out);
            var success = parsed.rc === 0 || (parsed.rc == null && parsed.output && !/error|failed|denied|unknown|not found|access/i.test(parsed.output));
            if (!success) return;
            var tableColumns = parseWebshellDbTableColumns(parsed.output);
            if (!Object.keys(tableColumns).length) return;

            var nextCfg = getWebshellDbConfig(conn);
            var nextSchema = normalizeWebshellDbSchema((nextCfg && nextCfg.schema && typeof nextCfg.schema === 'object') ? nextCfg.schema : {});
            if (!nextSchema[dbName]) nextSchema[dbName] = { tables: {} };
            Object.keys(tableColumns).forEach(function (tableName) {
                nextSchema[dbName].tables[tableName] = tableColumns[tableName];
            });
            nextCfg.schema = nextSchema;
            saveWebshellDbConfig(conn, nextCfg);
            renderDbSchemaTree();
        }).catch(function () {
            // ignore batch column load errors to avoid interrupting main flow
        }).finally(function () {
            delete dbColumnsBatchLoading[batchKey];
            dbColumnsBatchLoaded[batchKey] = true;
            webshellRunning = false;
            setDbActionButtonsDisabled(false);
        });
    }

    function loadDbSchema() {
        if (!conn || !conn.id) {
            webshellDbSetOutput(wsT('webshell.dbNoConn') || '请先选择 WebShell 连接', true);
            return;
        }
        if (webshellRunning) {
            webshellDbSetOutput(wsT('webshell.dbRunning') || '数据库命令执行中，请稍候', true);
            return;
        }
        var cfg = webshellDbCollectConfig(conn);
        resetDbColumnLoadCache();
        var built = buildWebshellDbSchemaCommand(cfg);
        if (!built.command) {
            webshellDbSetOutput(built.error || (wsT('webshell.dbSchemaFailed') || '加载数据库结构失败'), true);
            return;
        }
        webshellDbSetOutput(wsT('webshell.running') || '执行中…', false);
        webshellRunning = true;
        setDbActionButtonsDisabled(true);
        execWebshellCommand(conn, built.command).then(function (out) {
            var parsed = parseWebshellDbExecOutput(out);
            var success = parsed.rc === 0 || (parsed.rc == null && parsed.output && !/error|failed|denied|unknown|not found|access/i.test(parsed.output));
            if (!success) {
                webshellDbSetOutput((wsT('webshell.dbSchemaFailed') || '加载数据库结构失败') + ':\n' + (parsed.output || ''), true);
                return;
            }
            cfg.schema = parseWebshellDbSchema(parsed.output);
            cfg.output = wsT('webshell.dbSchemaLoaded') || '结构加载完成';
            cfg.outputIsError = false;
            saveWebshellDbConfig(conn, cfg);
            renderDbSchemaTree();
            webshellDbSetOutput(wsT('webshell.dbSchemaLoaded') || '结构加载完成', false);
        }).catch(function (err) {
            webshellDbSetOutput((wsT('webshell.dbSchemaFailed') || '加载数据库结构失败') + ': ' + (err && err.message ? err.message : String(err)), true);
        }).finally(function () {
            webshellRunning = false;
            setDbActionButtonsDisabled(false);
        });
    }

    function runDbQuery(isTestOnly) {
        if (!conn || !conn.id) {
            webshellDbSetOutput(wsT('webshell.dbNoConn') || '请先选择 WebShell 连接', true);
            return;
        }
        if (webshellRunning) {
            webshellDbSetOutput(wsT('webshell.dbRunning') || '数据库命令执行中，请稍候', true);
            return;
        }
        var cfg = webshellDbCollectConfig(conn);
        var built = buildWebshellDbCommand(cfg, !!isTestOnly);
        if (!built.command) {
            webshellDbSetOutput(built.error || (wsT('webshell.dbExecFailed') || '数据库执行失败'), true);
            return;
        }
        webshellDbSetOutput(wsT('webshell.running') || '执行中…', false);
        webshellRunning = true;
        setDbActionButtonsDisabled(true);
        execWebshellCommand(conn, built.command).then(function (out) {
            var parsed = parseWebshellDbExecOutput(out);
            var code = parsed.rc;
            var content = parsed.output || '';
            var success = (code === 0) || (code == null && content && !/error|failed|denied|unknown|not found|access/i.test(content));
            if (isTestOnly) {
                if (success) {
                    cfg.output = wsTOr('webshell.dbTestPassed', '连接测试通过');
                    cfg.outputIsError = false;
                    saveWebshellDbConfig(conn, cfg);
                    webshellDbSetOutput(cfg.output, false);
                } else {
                    cfg.output = wsTOr('webshell.dbTestFailed', '连接测试失败') + (content ? (':\n' + content) : '');
                    cfg.outputIsError = true;
                    saveWebshellDbConfig(conn, cfg);
                    webshellDbSetOutput(cfg.output, true);
                }
                return;
            }
            if (!success) {
                cfg.output = (wsT('webshell.dbExecFailed') || '数据库执行失败') + (content ? (':\n' + content) : '');
                cfg.outputIsError = true;
                saveWebshellDbConfig(conn, cfg);
                webshellDbSetOutput(cfg.output, true);
                return;
            }
            var hasTable = webshellDbRenderTable(content);
            if (hasTable) {
                cfg.output = wsT('webshell.dbExecSuccess') || 'SQL 执行成功';
                cfg.outputIsError = false;
                saveWebshellDbConfig(conn, cfg);
                webshellDbSetOutput(cfg.output, false);
            } else {
                cfg.output = content || (wsT('webshell.dbNoOutput') || '执行完成（无输出）');
                cfg.outputIsError = false;
                saveWebshellDbConfig(conn, cfg);
                webshellDbSetOutput(cfg.output, false);
            }
        }).catch(function (err) {
            cfg.output = (wsT('webshell.dbExecFailed') || '数据库执行失败') + ': ' + (err && err.message ? err.message : String(err));
            cfg.outputIsError = true;
            saveWebshellDbConfig(conn, cfg);
            webshellDbSetOutput(cfg.output, true);
        }).finally(function () {
            webshellRunning = false;
            setDbActionButtonsDisabled(false);
        });
    }

    if (dbTypeEl) dbTypeEl.addEventListener('change', function () {
        webshellDbUpdateFieldVisibility();
        var cfg = webshellDbCollectConfig(conn);
        cfg.selectedDatabase = '';
        cfg.schema = {};
        saveWebshellDbConfig(conn, cfg);
        resetDbColumnLoadCache();
        renderDbSchemaTree();
    });
    ['webshell-db-profile-name', 'webshell-db-host', 'webshell-db-port', 'webshell-db-user', 'webshell-db-pass', 'webshell-db-name', 'webshell-db-sqlite-path'].forEach(function (id) {
        var el = document.getElementById(id);
        if (el) el.addEventListener('change', function () {
            webshellDbCollectConfig(conn);
            resetDbColumnLoadCache();
            renderDbProfileTabs();
        });
    });
    if (dbSqlEl) dbSqlEl.addEventListener('change', function () { webshellDbCollectConfig(conn); });
    if (dbRunBtn) dbRunBtn.addEventListener('click', function () { runDbQuery(false); });
    if (dbTestBtn) dbTestBtn.addEventListener('click', function () { runDbQuery(true); });
    if (dbLoadSchemaBtn) dbLoadSchemaBtn.addEventListener('click', function () { loadDbSchema(); });
    if (dbTemplateBtn) dbTemplateBtn.addEventListener('click', function () {
        if (!dbSqlEl) return;
        var cfg = webshellDbCollectConfig(conn);
        if (cfg.type === 'mysql') dbSqlEl.value = 'SHOW DATABASES;\nSELECT DATABASE() AS current_db;';
        else if (cfg.type === 'pgsql') dbSqlEl.value = 'SELECT current_database();\nSELECT schema_name FROM information_schema.schemata ORDER BY schema_name;';
        else if (cfg.type === 'sqlite') dbSqlEl.value = "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name;";
        else dbSqlEl.value = "SELECT name FROM sys.databases ORDER BY name;\nSELECT DB_NAME() AS current_db;";
        webshellDbCollectConfig(conn);
    });
    if (dbClearBtn) dbClearBtn.addEventListener('click', function () {
        if (dbSqlEl) dbSqlEl.value = '';
        webshellDbCollectConfig(conn);
    });
    if (dbProfileModalCloseBtn) dbProfileModalCloseBtn.addEventListener('click', function () {
        setDbProfileModalVisible(false);
    });
    if (dbProfileModalCancelBtn) dbProfileModalCancelBtn.addEventListener('click', function () {
        applyActiveDbProfileToForm();
        setDbProfileModalVisible(false);
    });
    if (dbProfileModalSaveBtn) dbProfileModalSaveBtn.addEventListener('click', function () {
        webshellDbCollectConfig(conn);
        renderDbProfileTabs();
        resetDbColumnLoadCache();
        setDbProfileModalVisible(false);
    });
    if (dbProfileModalEl) dbProfileModalEl.addEventListener('click', function (evt) {
        if (evt.target === dbProfileModalEl) {
            applyActiveDbProfileToForm();
            setDbProfileModalVisible(false);
        }
    });
    if (dbAddProfileBtn) dbAddProfileBtn.addEventListener('click', function () {
        var state = getWebshellDbState(conn);
        var name = 'DB-' + (state.profiles.length + 1);
        var p = newWebshellDbProfile(name);
        state.profiles.push(p);
        state.activeProfileId = p.id;
        saveWebshellDbState(conn, state);
        applyActiveDbProfileToForm();
        renderDbProfileTabs();
        renderDbSchemaTree();
        setDbProfileModalVisible(true, 'add');
    });
    renderDbProfileTabs();
    applyActiveDbProfileToForm();
    renderDbSchemaTree();
    setDbProfileModalVisible(false);

    initWebshellTerminal(conn);
}

// 加载 WebShell 连接的 AI 助手对话历史（持久化展示），返回 Promise 供 .then 更新工具栏等；含 processDetails 时渲染折叠的「执行过程及调用工具」
function loadWebshellAiHistory(conn, messagesContainer) {
    if (!conn || !conn.id || !messagesContainer) return Promise.resolve();
    if (typeof apiFetch !== 'function') return Promise.resolve();
    return apiFetch('/api/webshell/connections/' + encodeURIComponent(conn.id) + '/ai-history', { method: 'GET' })
        .then(function (r) { return r.json(); })
        .then(function (data) {
            if (data.conversationId) {
                webshellAiConvMap[conn.id] = data.conversationId;
                apiFetch('/api/conversations/' + encodeURIComponent(data.conversationId), { method: 'GET' })
                    .then(function (r) { return r.ok ? r.json() : null; })
                    .then(function (conv) {
                        if (conv) wsSetWebshellAiProject(conn, conv.projectId || conv.project_id || '');
                    })
                    .catch(function () { /* ignore */ });
            }
            var list = Array.isArray(data.messages) ? data.messages : [];
            list.forEach(function (msg) {
                var role = (msg.role || '').toLowerCase();
                var content = (msg.content || '').trim();
                if (!content && role !== 'assistant') return;
                var div = document.createElement('div');
                div.className = 'webshell-ai-msg ' + (role === 'user' ? 'user' : 'assistant');
                if (role === 'user') {
                    div.textContent = content;
                } else {
                    if (isLikelyWebshellAiErrorMessage(content, msg)) {
                        renderWebshellAiErrorMessage(div, content);
                    } else if (typeof formatMarkdown === 'function') {
                        div.innerHTML = formatMarkdown(content);
                    } else {
                        div.textContent = content;
                    }
                }
                messagesContainer.appendChild(div);
                if (role === 'assistant') {
                    var wsHistMerged = (typeof window.mergeMessageReasoningContentIntoProcessDetails === 'function')
                        ? window.mergeMessageReasoningContentIntoProcessDetails(msg.processDetails || [], msg.reasoningContent)
                        : (msg.processDetails || []);
                    if (wsHistMerged.length > 0) {
                        var block = renderWebshellProcessDetailsBlock(wsHistMerged, true);
                        if (block) messagesContainer.appendChild(block);
                    }
                }
            });
            if (list.length === 0) {
                var readyMsg = wsT('webshell.aiSystemReadyMessage') || '系统已就绪。请输入您的测试需求，系统将自动执行相应的安全测试。';
                var readyDiv = document.createElement('div');
                readyDiv.className = 'webshell-ai-msg assistant';
                readyDiv.textContent = readyMsg;
                messagesContainer.appendChild(readyDiv);
            }
            messagesContainer.scrollTop = messagesContainer.scrollHeight;
        })
        .catch(function (e) {
            console.warn('加载 WebShell AI 历史失败', conn.id, e);
        });
}

function runWebshellAiSend(conn, inputEl, sendBtn, messagesContainer) {
    if (!conn || !conn.id) return;
    var message = (inputEl && inputEl.value || '').trim();
    if (!message) return;
    if (webshellAiSending) return;
    if (typeof apiFetch !== 'function') {
        if (messagesContainer) {
            var errDiv = document.createElement('div');
            errDiv.className = 'webshell-ai-msg assistant';
            errDiv.textContent = wsTOr('webshell.aiCannotSend', '无法发送：未登录或 apiFetch 不可用');
            messagesContainer.appendChild(errDiv);
            messagesContainer.scrollTop = messagesContainer.scrollHeight;
        }
        return;
    }

    webshellAiAbortController = new AbortController();
    wsSetAiSendingState(true);

    var userDiv = document.createElement('div');
    userDiv.className = 'webshell-ai-msg user';
    userDiv.textContent = message;
    messagesContainer.appendChild(userDiv);

    var timelineContainer = document.createElement('div');
    timelineContainer.className = 'webshell-ai-timeline';
    timelineContainer.setAttribute('aria-live', 'polite');

    var assistantDiv = document.createElement('div');
    assistantDiv.className = 'webshell-ai-msg assistant';
    assistantDiv.textContent = '…';
    messagesContainer.appendChild(timelineContainer);
    messagesContainer.appendChild(assistantDiv);
    messagesContainer.scrollTop = messagesContainer.scrollHeight;

    function appendTimelineItem(type, title, message, data) {
        var item = document.createElement('div');
        item.className = 'webshell-ai-timeline-item webshell-ai-timeline-' + type;

        var html = '<span class="webshell-ai-timeline-title">' + escapeHtml(title || message || '') + '</span>';

        // 工具调用入参 + 结果同卡
        if (type === 'tool_call' && data) {
            try {
                var args = data.argumentsObj;
                if (args == null && data.arguments != null && String(data.arguments).trim() !== '') {
                    try {
                        args = JSON.parse(String(data.arguments));
                    } catch (e1) {
                        args = { _raw: String(data.arguments) };
                    }
                }
                if (args == null || typeof args !== 'object') {
                    args = {};
                }
                var paramsLabel = (typeof window.t === 'function') ? window.t('timeline.params') : '参数:';
                var pendingResult = (typeof window.buildToolResultSectionHtml === 'function')
                    ? window.buildToolResultSectionHtml({}, { pending: true })
                    : '';
                html += '<div class="webshell-ai-timeline-msg"><div class="tool-arg-section"><strong>' +
                    escapeHtml(paramsLabel) +
                    '</strong><pre class="tool-args">' +
                    escapeHtml(JSON.stringify(args, null, 2)) +
                    '</pre></div>' +
                    (pendingResult ? '<div class="tool-result-slot">' + pendingResult + '</div>' : '') +
                    '</div>';
            } catch (e) {
                // JSON 解析失败时忽略参数详情，避免打断主流程
            }
        } else if (type === 'eino_agent_reply' && message) {
            html += '<div class="webshell-ai-timeline-msg"><pre style="white-space:pre-wrap;">' + escapeHtml(message) + '</pre></div>';
        } else if (type === 'eino_usage_summary') {
            var usageText = message || formatWebshellEinoUsageSummaryMessage(data);
            html += '<div class="webshell-ai-timeline-msg"><pre style="white-space:pre-wrap;">' + escapeHtml(usageText) + '</pre></div>';
        } else if (type === 'tool_result' && data) {
            // 工具调用出参
            var noResultText = (typeof window.t === 'function') ? window.t('timeline.noResult') : '无结果';
            var result = data.result != null ? data.result : (data.error != null ? data.error : (data.resultPreview != null ? data.resultPreview : noResultText));
            var resultStr = (typeof result === 'string') ? result : JSON.stringify(result);
            var displayState = (typeof window.getToolResultDisplayState === 'function')
                ? window.getToolResultDisplayState(data, { rawText: resultStr })
                : { kind: ((data.isError || data.success === false) ? 'error' : 'success'), isError: (data.isError || data.success === false) };
            var execResultLabel = (typeof window.t === 'function') ? window.t('timeline.executionResult') : '执行结果:';
            var execIdLabel = (typeof window.t === 'function') ? window.t('timeline.executionId') : '执行ID:';
            var sectionClass = displayState.kind === 'blocked' ? 'blocked' : (displayState.kind === 'background_running' ? 'pending' : (displayState.isError ? 'error' : 'success'));
            html += '<div class="webshell-ai-timeline-msg"><div class="tool-result-section ' +
                sectionClass +
                '"><strong>' + escapeHtml(execResultLabel) + '</strong><pre class="tool-result">' +
                escapeHtml(resultStr) +
                '</pre>' +
                (data.executionId ? '<div class="tool-execution-id"><span>' +
                    escapeHtml(execIdLabel) +
                    '</span> <code>' +
                    escapeHtml(String(data.executionId)) +
                    '</code></div>' : '') +
                '</div></div>';
        } else if (message && message !== title) {
            html += '<div class="webshell-ai-timeline-msg">' + escapeHtml(message) + '</div>';
        }

        item.innerHTML = html;
        timelineContainer.appendChild(item);
        timelineContainer.classList.add('has-items');
        messagesContainer.scrollTop = messagesContainer.scrollHeight;
        return item;
    }

    var einoSubReplyStreams = new Map();
    var wsThinkingStreams = new Map();        // streamId → { el, buf }
    var wsToolResultStreams = new Map();      // toolCallId → { el, buf }
    var wsToolCallItems = new Map();          // toolCallId → DOM item（参数+结果同卡）

    if (inputEl) inputEl.value = '';

    var convId = webshellAiConvMap[conn.id] || '';
    var wsRole = (typeof getCurrentRole === 'function') ? getCurrentRole() : (localStorage.getItem('currentRole') || '');
    var body = {
        message: message,
        webshellConnectionId: conn.id,
        conversationId: convId,
        role: wsRole,
        finalization: {
            requireExecutionEvidence: true
        }
    };
    if (!convId) {
        var wsPid = getWebshellAiProjectSelection(conn);
        if (wsPid) body.projectId = wsPid;
    }

    // 流式输出：支持 progress 实时更新、response 打字机效果；若后端发送多段 response 则追加
    var streamingTarget = '';  // 当前要打字显示的目标全文（用于打字机效果）
    var streamingTypingId = 0;  // 防重入，每次新 response 自增

    resolveWebshellAiStreamRequest().then(function (info) {
        if (info && info.orchestration) {
            body.orchestration = info.orchestration;
        }
        if (typeof window.buildReasoningRequestPayload === 'function') {
            var rp = window.buildReasoningRequestPayload();
            if (rp) {
                body.reasoning = rp;
            }
        }
        return apiFetch(info.path, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
            signal: webshellAiAbortController ? webshellAiAbortController.signal : undefined
        });
    }).then(function (response) {
        if (!response.ok) {
            renderWebshellAiErrorMessage(assistantDiv, wsTOr('webshell.aiRequestFailed', '请求失败') + ': HTTP ' + response.status);
            return;
        }
        return response.body.getReader();
    }).then(function (reader) {
        if (!reader) return;
        webshellAiStreamReader = reader;
        var decoder = new TextDecoder();
        var buffer = '';
        return reader.read().then(function processChunk(result) {
            if (result.done) return;
            buffer += decoder.decode(result.value, { stream: true });
            var lines = buffer.split('\n');
            buffer = lines.pop() || '';
            for (var i = 0; i < lines.length; i++) {
                var line = lines[i];
                if (line.indexOf('data: ') !== 0) continue;
                try {
                    var eventData = JSON.parse(line.slice(6));
                    var _et = eventData.type;
                    var _ed = eventData.data || {};
                    var _em = eventData.message || '';

                    if (_et === 'conversation' && _ed.conversationId) {
                        var convId = _ed.conversationId;
                        var prevDraft = webshellAiDraftProjectByConn[conn.id];
                        if (prevDraft) {
                            webshellAiProjectByConvId[convId] = prevDraft;
                            delete webshellAiDraftProjectByConn[conn.id];
                        }
                        webshellAiConvMap[conn.id] = convId;
                        var listEl = document.getElementById('webshell-ai-conv-list');
                        if (listEl) fetchAndRenderWebshellAiConvList(conn, listEl).then(function () {
                            listEl.querySelectorAll('.webshell-ai-conv-item').forEach(function (el) {
                                el.classList.toggle('active', el.dataset.convId === convId);
                            });
                        });

                    // ─── Response streaming ───
                    } else if (_et === 'response_start') {
                        streamingTarget = '';
                        webshellStreamingTypingId += 1;
                        streamingTypingId = webshellStreamingTypingId;
                        assistantDiv.dataset.finalized = 'false';
                        assistantDiv.classList.add('webshell-ai-candidate-output');
                        assistantDiv.textContent = '…';
                        messagesContainer.scrollTop = messagesContainer.scrollHeight;
                    } else if (_et === 'response_delta') {
                        var deltaText = (_em != null && _em !== '') ? String(_em) : '';
                        var mergeBuf = (typeof window.mergeStreamBuffer === 'function')
                            ? window.mergeStreamBuffer
                            : function (cur, dlt) {
                                var normR = (typeof window.normalizeStreamingDeltaJs === 'function')
                                    ? window.normalizeStreamingDeltaJs(cur, dlt)
                                    : [cur + dlt, dlt];
                                return normR[0];
                            };
                        if (deltaText || (_ed && _ed.accumulated != null)) {
                            streamingTarget = mergeBuf(streamingTarget, deltaText, _ed);
                            webshellStreamingTypingId += 1;
                            streamingTypingId = webshellStreamingTypingId;
                            runWebshellAiStreamingTyping(assistantDiv, streamingTarget, streamingTypingId, messagesContainer);
                        }
                    } else if (_et === 'response') {
                        var text = (_em != null && _em !== '') ? _em : (typeof _ed === 'string' ? _ed : '');
                        var finalized = !!(_ed && _ed.finalized === true);
                        var hasFinalizationContract = !!(_ed && (
                            Object.prototype.hasOwnProperty.call(_ed, 'finalized') ||
                            Object.prototype.hasOwnProperty.call(_ed, 'finalizable') ||
                            Object.prototype.hasOwnProperty.call(_ed, 'completionReason') ||
                            Object.prototype.hasOwnProperty.call(_ed, 'evidenceVerified') ||
                            Object.prototype.hasOwnProperty.call(_ed, 'missingChecks')
                        ));
                        assistantDiv.dataset.finalized = finalized ? 'true' : 'false';
                        assistantDiv.classList.toggle('webshell-ai-candidate-output', !finalized);
                        assistantDiv.classList.toggle('webshell-ai-finalized-output', finalized);
                        if (!finalized && !hasFinalizationContract && text) {
                            appendTimelineItem('finalization_check', wsTOr('webshell.aiCandidateMissingContract', '候选输出缺少最终化证明'), text, Object.assign({}, _ed || {}, { missingFinalizationContract: true }));
                            text = webshellFinalizationNotice(_ed || {}, text, false);
                        } else if (!finalized && hasFinalizationContract) {
                            text = webshellFinalizationNotice(_ed || {}, text, true);
                        }
                        if (text) {
                            streamingTarget = String(text);
                            webshellStreamingTypingId += 1;
                            streamingTypingId = webshellStreamingTypingId;
                            runWebshellAiStreamingTyping(assistantDiv, streamingTarget, streamingTypingId, messagesContainer);
                        }

                    // ─── Terminal events ───
                    } else if (_et === 'finalization_check') {
                        var finalizationOk = !!(_ed && _ed.finalized === true);
                        appendTimelineItem('finalization_check', finalizationOk ? wsTOr('webshell.aiFinalCheckPass', '最终回复检查通过') : wsTOr('webshell.aiFinalCheckFail', '最终回复检查未通过'), finalizationOk ? (_em || wsTOr('webshell.aiFinalCheckPassMsg', '最终回复检查通过。')) : webshellFinalizationNotice(_ed || {}, _em, true), _ed);
                    } else if (_et === 'finalization_auto_continue') {
                        appendTimelineItem('progress', wsTOr('webshell.aiContinueVerification', '继续验证'), _em, _ed);
                    } else if (_et === 'eino_model_retry') {
                        var retryAttempt = _ed && _ed.attempt ? ('（' + _ed.attempt + '）') : '';
                        var retryMsg = _em || wsTOr('webshell.aiModelRetryMsg', '模型调用遇到临时问题，Eino 正在原生重试…');
                        if (_ed && _ed.reason) retryMsg += '\n' + wsTOr('webshell.aiRetryReasonLabel', '原因：') + _ed.reason;
                        if (_ed && _ed.error) retryMsg += '\n' + wsTOr('webshell.aiRetryErrorLabel', '错误详情：') + _ed.error;
                        appendTimelineItem('warning', wsTOr('chat.einoModelRetryTitle', '🔁 模型调用重试') + retryAttempt, retryMsg, _ed);
                    } else if (_et === 'eino_model_failover') {
                        var failoverAttempt = _ed && _ed.attempt ? ('（' + _ed.attempt + '）') : '';
                        var failoverMsg = _em || wsTOr('webshell.aiModelFailoverMsg', '主模型重试耗尽，正在切换备用模型。');
                        if (_ed && _ed.channel) failoverMsg += '\n' + wsTOr('webshell.aiFailoverChannelLabel', '通道：') + _ed.channel;
                        if (_ed && _ed.model) failoverMsg += '\n' + wsTOr('webshell.aiFailoverModelLabel', '模型：') + _ed.model;
                        appendTimelineItem('warning', wsTOr('chat.einoModelFailoverTitle', '🔀 切换备用模型') + failoverAttempt, failoverMsg, _ed);
                    } else if (_et === 'eino_usage_summary') {
                        appendTimelineItem('eino_usage_summary', '📊 ' + formatWebshellEinoUsageSummaryTitle(_ed), formatWebshellEinoUsageSummaryMessage(_ed), _ed);
                    } else if (_et === 'error' && _em) {
                        streamingTypingId += 1;
                        var errLabel = wsTOr('chat.error', '错误');
                        appendTimelineItem('error', '❌ ' + errLabel, _em, _ed);
                        renderWebshellAiErrorMessage(assistantDiv, errLabel + ': ' + _em);
                    } else if (_et === 'cancelled') {
                        streamingTypingId += 1;
                        var cancelLabel = wsTOr('chat.taskCancelled', '任务已取消');
                        appendTimelineItem('cancelled', '⛔ ' + cancelLabel, _em, _ed);
                        if (!streamingTarget && !assistantDiv.dataset.hasContent) {
                            assistantDiv.textContent = cancelLabel;
                        }
                    } else if (_et === 'done') {
                        // 清理流式状态
                        wsThinkingStreams.clear();
                        wsToolResultStreams.clear();
                        einoSubReplyStreams.clear();

                    // ─── Iteration / Progress ───
                    } else if (_et === 'progress' && _em) {
                        var progressMsg = (typeof window.translateProgressMessage === 'function')
                            ? window.translateProgressMessage(_em) : _em;
                        appendTimelineItem('progress', '🔍 ' + progressMsg, '', _ed);
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if (_et === 'iteration') {
                        var iterN = _ed.iteration || 0;
                        var iterTitle = wsTOr('chat.iterationRound', '') || (iterN ? ('第 ' + iterN + ' 轮迭代') : (_em || wsTOr('webshell.aiIterationDefault', '迭代')));
                        if (typeof window.t === 'function' && iterN) {
                            iterTitle = window.t('chat.iterationRound', { n: iterN });
                        }
                        var iterMessage = _em || '';
                        if (iterMessage && typeof window.translateProgressMessage === 'function') {
                            iterMessage = window.translateProgressMessage(iterMessage);
                        }
                        appendTimelineItem('iteration', '🔍 ' + iterTitle, iterMessage, _ed);
                        if (!streamingTarget) assistantDiv.textContent = '…';

                    // ─── Thinking / reasoning_chain（推理过程，reasoning_content） ───
                    } else if ((_et === 'thinking_stream_start' || _et === 'reasoning_chain_stream_start') && _ed.streamId) {
                        var isRcStart = _et === 'reasoning_chain_stream_start';
                        if (wsThinkingStreams.has(_ed.streamId)) {
                            var tsExist = wsThinkingStreams.get(_ed.streamId);
                            tsExist.buf = '';
                            if (tsExist.body) tsExist.body.textContent = '';
                        } else {
                        var thinkSLabel = wsTOr(isRcStart ? 'chat.reasoningChain' : 'chat.aiThinking', isRcStart ? '推理过程' : 'AI 思考');
                        var thinkEmoji = isRcStart ? '🔗' : '🤔';
                        var thinkSItem = document.createElement('div');
                        thinkSItem.className = 'webshell-ai-timeline-item webshell-ai-timeline-' + (isRcStart ? 'reasoning_chain' : 'thinking');
                        thinkSItem.innerHTML = '<span class="webshell-ai-timeline-title">' + escapeHtml(webshellAgentPx(_ed) + thinkEmoji + ' ' + thinkSLabel) + '</span>';
                        var thinkSPre = document.createElement('div');
                        thinkSPre.className = 'webshell-ai-timeline-msg webshell-thinking-stream-body';
                        thinkSItem.appendChild(thinkSPre);
                        timelineContainer.appendChild(thinkSItem);
                        timelineContainer.classList.add('has-items');
                        wsThinkingStreams.set(_ed.streamId, { el: thinkSItem, body: thinkSPre, buf: '' });
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if ((_et === 'thinking_stream_delta' || _et === 'reasoning_chain_stream_delta') && _ed && _ed.streamId) {
                        var tsD = wsThinkingStreams.get(_ed.streamId);
                        if (tsD) {
                            var mergeThink = (typeof window.mergeStreamBuffer === 'function')
                                ? window.mergeStreamBuffer
                                : function (cur, dlt) {
                                    var normT = (typeof window.normalizeStreamingDeltaJs === 'function')
                                        ? window.normalizeStreamingDeltaJs(cur, dlt) : [cur + dlt, dlt];
                                    return normT[0];
                                };
                            tsD.buf = mergeThink(tsD.buf, _em || '', _ed);
                            if (typeof formatMarkdown === 'function') {
                                tsD.body.innerHTML = formatMarkdown(tsD.buf);
                            } else {
                                tsD.body.textContent = tsD.buf;
                            }
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if ((_et === 'thinking_stream_end' || _et === 'reasoning_chain_stream_end') && _ed.streamId) {
                        var tsE = wsThinkingStreams.get(_ed.streamId);
                        if (tsE) {
                            var fullThink = (_em != null && _em !== '') ? String(_em) : tsE.buf;
                            if (typeof formatMarkdown === 'function') {
                                tsE.body.innerHTML = formatMarkdown(fullThink);
                            } else {
                                tsE.body.textContent = fullThink;
                            }
                            wsThinkingStreams.delete(_ed.streamId);
                        }
                    } else if ((_et === 'thinking' || _et === 'reasoning_chain') && _em) {
                        // 如果有 streamId 且已存在流式条目，跳过避免重复
                        if (_ed.streamId && wsThinkingStreams.has(_ed.streamId)) {
                            // 已由 *_stream_* 处理
                        } else {
                            var isRc = _et === 'reasoning_chain';
                            var thinkLabel = wsTOr(isRc ? 'chat.reasoningChain' : 'chat.aiThinking', isRc ? '推理过程' : 'AI 思考');
                            var thinkEm = isRc ? '🔗' : '🤔';
                            appendTimelineItem(isRc ? 'reasoning_chain' : 'thinking', webshellAgentPx(_ed) + thinkEm + ' ' + thinkLabel, _em, _ed);
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';

                    // ─── Warning ───
                    } else if (_et === 'warning') {
                        appendTimelineItem('warning', '⚠️ ' + (_em || ''), '', _ed);

                    // ─── Tool calls ───
                    } else if (_et === 'tool_calls_detected' && _ed) {
                        var count = _ed.count || 0;
                        var detectedLabel = wsTOr('chat.toolCallsDetected', '') || ('检测到 ' + count + ' 个工具调用');
                        if (typeof window.t === 'function') {
                            try { detectedLabel = window.t('chat.toolCallsDetected', { count: count }); } catch (e) { /* */ }
                        }
                        appendTimelineItem('tool_calls_detected', webshellAgentPx(_ed) + '🔧 ' + detectedLabel, _em || '', _ed);
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if (_et === 'tool_call' && _ed) {
                        var tn = _ed.toolName || wsTOr('chat.unknownTool', '未知工具');
                        var idx = _ed.index || 0;
                        var total = _ed.total || 0;
                        var callTitle = typeof window.formatToolCallTimelineTitle === 'function'
                            ? window.formatToolCallTimelineTitle(tn, idx, total)
                            : (wsTOr('chat.callTool', '') || ('调用工具: ' + tn + (total ? ' (' + idx + '/' + total + ')' : '')));
                        var callItem = appendTimelineItem('tool_call', webshellAgentPx(_ed) + '🔧 ' + callTitle, _em || '', _ed);
                        if (_ed.toolCallId && callItem) {
                            wsToolCallItems.set(_ed.toolCallId, callItem);
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';

                    // ─── Tool result (final) ───
                    } else if (_et === 'tool_result' && _ed) {
                        var wsLiveState = typeof window.getToolResultDisplayState === 'function' ? window.getToolResultDisplayState(_ed) : { success: _ed.success !== false };
                        var blocked = wsLiveState.kind === 'blocked';
                        var success = wsLiveState.success;
                        var tname = _ed.toolName || wsTOr('webshell.defaultToolName', '工具');
                        var merged = false;
                        if (_ed.toolCallId) {
                            var streamSt = wsToolResultStreams.get(_ed.toolCallId);
                            var callElRes = wsToolCallItems.get(_ed.toolCallId) || (streamSt && streamSt.el);
                            if (callElRes && typeof window.mergeToolResultIntoCallItem === 'function') {
                                window.mergeToolResultIntoCallItem(callElRes, _ed);
                                merged = true;
                                wsToolResultStreams.delete(_ed.toolCallId);
                                wsToolCallItems.delete(_ed.toolCallId);
                            }
                        }
                        if (!merged) {
                            var titleText = wsTOr(blocked ? 'chat.toolExecBlocked' : (success ? 'chat.toolExecComplete' : 'chat.toolExecFailed'), '') ||
                                (tname + (blocked ? ' 已拦截' : (success ? ' 执行完成' : ' 执行失败')));
                            if (typeof window.t === 'function') {
                                try { titleText = window.t(blocked ? 'chat.toolExecBlocked' : (success ? 'chat.toolExecComplete' : 'chat.toolExecFailed'), { name: tname }); } catch (e) { /* */ }
                            }
                            var title = webshellAgentPx(_ed) + (blocked ? '🛡 ' : (success ? '✅ ' : '❌ ')) + titleText;
                            var sub = _em || (_ed.result ? String(_ed.result).slice(0, 300) : '');
                            appendTimelineItem('tool_result', title, sub, _ed);
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';

                    // ─── Eino sub-agent reply streaming ───
                    } else if (_et === 'eino_agent_reply_stream_start' && _ed.streamId) {
                        if (einoSubReplyStreams.has(_ed.streamId)) {
                            var stExist = einoSubReplyStreams.get(_ed.streamId);
                            stExist.buf = '';
                            var preExist = stExist.el && stExist.el.querySelector('.webshell-eino-reply-stream-body');
                            if (preExist) preExist.textContent = '';
                        } else {
                        var repTS = wsTOr('chat.einoAgentReplyTitle', '子代理回复');
                        var runTS = wsTOr('timeline.running', '执行中...');
                        var itemS = document.createElement('div');
                        itemS.className = 'webshell-ai-timeline-item webshell-ai-timeline-eino_agent_reply';
                        itemS.innerHTML = '<span class="webshell-ai-timeline-title">' + escapeHtml(webshellAgentPx(_ed) + '💬 ' + repTS + ' · ' + runTS) + '</span>';
                        timelineContainer.appendChild(itemS);
                        timelineContainer.classList.add('has-items');
                        einoSubReplyStreams.set(_ed.streamId, { el: itemS, buf: '' });
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if (_et === 'eino_agent_reply_stream_delta' && _ed.streamId) {
                        var stD = einoSubReplyStreams.get(_ed.streamId);
                        if (stD) {
                            var mergeSub = (typeof window.mergeStreamBuffer === 'function')
                                ? window.mergeStreamBuffer
                                : function (cur, dlt) {
                                    var normS = (typeof window.normalizeStreamingDeltaJs === 'function')
                                        ? window.normalizeStreamingDeltaJs(cur, dlt) : [cur + dlt, dlt];
                                    return normS[0];
                                };
                            stD.buf = mergeSub(stD.buf, _em || '', _ed);
                            var preD = stD.el.querySelector('.webshell-eino-reply-stream-body');
                            if (!preD) {
                                preD = document.createElement('pre');
                                preD.className = 'webshell-ai-timeline-msg webshell-eino-reply-stream-body';
                                preD.style.whiteSpace = 'pre-wrap';
                                stD.el.appendChild(preD);
                            }
                            if (typeof formatMarkdown === 'function') {
                                preD.innerHTML = formatMarkdown(stD.buf);
                            } else {
                                preD.textContent = stD.buf;
                            }
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if (_et === 'eino_agent_reply_stream_end' && _ed.streamId) {
                        var stE = einoSubReplyStreams.get(_ed.streamId);
                        if (stE) {
                            var fullE = (_em != null && _em !== '') ? String(_em) : stE.buf;
                            var repTE = wsTOr('chat.einoAgentReplyTitle', '子代理回复');
                            var titE = stE.el.querySelector('.webshell-ai-timeline-title');
                            if (titE) titE.textContent = webshellAgentPx(_ed) + '💬 ' + repTE;
                            var preE = stE.el.querySelector('.webshell-eino-reply-stream-body');
                            if (!preE) {
                                preE = document.createElement('pre');
                                preE.className = 'webshell-ai-timeline-msg webshell-eino-reply-stream-body';
                                preE.style.whiteSpace = 'pre-wrap';
                                stE.el.appendChild(preE);
                            }
                            if (typeof formatMarkdown === 'function') {
                                preE.innerHTML = formatMarkdown(fullE);
                            } else {
                                preE.textContent = fullE;
                            }
                            einoSubReplyStreams.delete(_ed.streamId);
                        }
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    } else if (_et === 'eino_agent_reply' && _em) {
                        var replyT = wsTOr('chat.einoAgentReplyTitle', '子代理回复');
                        appendTimelineItem('eino_agent_reply', webshellAgentPx(_ed) + '💬 ' + replyT, _em, _ed);
                        if (!streamingTarget) assistantDiv.textContent = '…';
                    }
                } catch (e) { /* ignore parse error */ }
            }
            messagesContainer.scrollTop = messagesContainer.scrollHeight;
            return reader.read().then(processChunk);
        });
    }).catch(function (err) {
        var msg = err && err.message ? err.message : String(err);
        var isAbort = /abort/i.test(msg);
        if (!isAbort) {
            renderWebshellAiErrorMessage(assistantDiv, wsTOr('webshell.aiRequestException', '请求异常') + ': ' + msg);
        }
    }).then(function () {
        webshellAiAbortController = null;
        webshellAiStreamReader = null;
        wsSetAiSendingState(false);
        if (assistantDiv.textContent === '…' && !streamingTarget) {
            // 没有任何 response 内容，保持纯文本提示
            assistantDiv.textContent = wsTOr('webshell.aiNoReplyContent', '无回复内容');
        } else if (streamingTarget) {
            // 流式结束：先终止当前打字机循环，避免后续 tick 把 HTML 覆盖回纯文本
            webshellStreamingTypingId += 1;
            // 再使用 Markdown 渲染完整内容
            if (typeof formatMarkdown === 'function') {
                assistantDiv.innerHTML = formatMarkdown(streamingTarget);
            } else {
                assistantDiv.textContent = streamingTarget;
            }
        }
        // 生成结果后：将执行过程折叠并保留，供后续查看；统一放在「助手回复下方」（与刷新后加载历史一致，最佳实践）
        if (timelineContainer && timelineContainer.classList.contains('has-items') && !timelineContainer.closest('.webshell-ai-process-block')) {
            var headerLabel = (typeof window.t === 'function') ? (window.t('chat.penetrationTestDetail') || '任务执行详情') : '任务执行详情';
            var wrap = document.createElement('div');
            wrap.className = 'process-details-container webshell-ai-process-block';
            wrap.innerHTML = '<button type="button" class="webshell-ai-process-toggle" aria-expanded="false">' + escapeHtml(headerLabel) + ' <span class="ws-toggle-icon">▶</span></button><div class="process-details-content"></div>';
            var contentDiv = wrap.querySelector('.process-details-content');
            contentDiv.appendChild(timelineContainer);
            timelineContainer.classList.add('progress-timeline');
            messagesContainer.insertBefore(wrap, assistantDiv.nextSibling);
            var toggleBtn = wrap.querySelector('.webshell-ai-process-toggle');
            var toggleIcon = wrap.querySelector('.ws-toggle-icon');
            toggleBtn.addEventListener('click', function () {
                var isExpanded = timelineContainer.classList.contains('expanded');
                timelineContainer.classList.toggle('expanded');
                toggleBtn.setAttribute('aria-expanded', !isExpanded);
                if (toggleIcon) toggleIcon.textContent = isExpanded ? '▶' : '▼';
            });
        }
        messagesContainer.scrollTop = messagesContainer.scrollHeight;
    });
}

// 打字机效果：将 target 逐字/逐段写入 el，保证只生效于当前 id 的调用
function runWebshellAiStreamingTyping(el, target, id, scrollContainer) {
    if (!el || id === undefined) return;
    var chunkSize = 3;
    var delayMs = 24;
    function tick() {
        if (id !== webshellStreamingTypingId) return;
        var cur = el.textContent || '';
        if (cur.length >= target.length) {
            el.textContent = target;
            if (scrollContainer) scrollContainer.scrollTop = scrollContainer.scrollHeight;
            return;
        }
        var next = target.slice(0, cur.length + chunkSize);
        el.textContent = next;
        if (scrollContainer) scrollContainer.scrollTop = scrollContainer.scrollHeight;
        setTimeout(tick, delayMs);
    }
    if (el.textContent.length < target.length) setTimeout(tick, delayMs);
}

function getWebshellHistory(connId) {
    if (!connId) return [];
    if (!webshellHistoryByConn[connId]) webshellHistoryByConn[connId] = [];
    return webshellHistoryByConn[connId];
}
function pushWebshellHistory(connId, cmd) {
    if (!connId || !cmd) return;
    if (!webshellHistoryByConn[connId]) webshellHistoryByConn[connId] = [];
    var h = webshellHistoryByConn[connId];
    if (h[h.length - 1] === cmd) return;
    h.push(cmd);
    if (h.length > WEBSHELL_HISTORY_MAX) h.shift();
}

// 执行快捷命令并将输出写入当前终端
function runQuickCommand(cmd) {
    if (!webshellCurrentConn || !webshellTerminalInstance) return;
    if (webshellRunning || webshellTerminalRunning) return;
    var term = webshellTerminalInstance;
    var connId = webshellCurrentConn.id;
    var sessionId = getActiveWebshellTerminalSessionId(connId);
    var terminalKey = getWebshellTerminalSessionKey(connId, sessionId);
    term.writeln('');
    pushWebshellHistory(terminalKey, cmd);
    appendWebshellTerminalLog(terminalKey, '\n$ ' + cmd + '\n');
    webshellRunning = true;
    setWebshellTerminalStatus(true);
    execWebshellCommand(webshellCurrentConn, cmd).then(function (out) {
        var s = String(out || '').replace(/\r\n/g, '\n').replace(/\r/g, '\n');
        s.split('\n').forEach(function (line) { term.writeln(line.replace(/\r/g, '')); });
        appendWebshellTerminalLog(terminalKey, s + '\n');
        term.write(WEBSHELL_PROMPT);
    }).catch(function (err) {
        var em = (err && err.message ? err.message : wsT('webshell.execError'));
        term.writeln('\x1b[31m' + em + '\x1b[0m');
        appendWebshellTerminalLog(terminalKey, em + '\n');
        term.write(WEBSHELL_PROMPT);
    }).finally(function () {
        webshellRunning = false;
        setWebshellTerminalStatus(false);
        renderWebshellTerminalSessions(webshellCurrentConn);
    });
}

// ---------- 虚拟终端（xterm + 按行执行） ----------
function initWebshellTerminal(conn) {
    const container = document.getElementById('webshell-terminal-container');
    if (!container || typeof Terminal === 'undefined') {
        if (container) {
            container.innerHTML = '<p class="terminal-error">' + escapeHtml(wsTOr('webshell.xtermNotLoaded', '未加载 xterm.js，请刷新页面')) + '</p>';
        }
        return;
    }

    const term = new Terminal({
        cursorBlink: true,
        cursorStyle: 'underline',
        fontSize: 13,
        fontFamily: 'Menlo, Monaco, "Courier New", monospace',
        lineHeight: 1.2,
        scrollback: 2000,
        theme: {
            background: '#0d1117',
            foreground: '#e6edf3',
            cursor: '#58a6ff',
            cursorAccent: '#0d1117',
            selection: 'rgba(88, 166, 255, 0.3)'
        }
    });

    let fitAddon = null;
    if (typeof FitAddon !== 'undefined') {
        const FitCtor = FitAddon.FitAddon || FitAddon;
        fitAddon = new FitCtor();
        term.loadAddon(fitAddon);
    }

    term.open(container);
    // 先 fit 再写内容，避免未计算尺寸时光标/画布错位挡住文字
    try {
        if (fitAddon) fitAddon.fit();
    } catch (e) {}
    setWebshellTerminalStatus(false);
    var connId = conn && conn.id ? conn.id : '';
    var sessionId = getActiveWebshellTerminalSessionId(connId);
    var terminalKey = getWebshellTerminalSessionKey(connId, sessionId);
    var cachedLog = getWebshellTerminalLog(terminalKey);
    if (cachedLog) {
        // xterm 恢复内容时统一使用 CRLF，避免切换窗口后出现“斜排”错位
        term.write(String(cachedLog).replace(/\r\n/g, '\n').replace(/\r/g, '\n').replace(/\n/g, '\r\n'));
    }
    term.write(WEBSHELL_PROMPT);

    // 按行写入输出，与系统设置终端 writeOutput 一致，避免 ls 等输出错位
    function writeWebshellOutput(term, text, isError) {
        if (!term || !text) return;
        var s = String(text).replace(/\r\n/g, '\n').replace(/\r/g, '\n');
        var lines = s.split('\n');
        var prefix = isError ? '\x1b[31m' : '';
        var suffix = isError ? '\x1b[0m' : '';
        term.write(prefix);
        for (var i = 0; i < lines.length; i++) {
            term.writeln(lines[i].replace(/\r/g, ''));
        }
        term.write(suffix);
    }

    term.onData(function (data) {
        // Ctrl+L 清屏
        if (data === '\x0c') {
            term.clear();
            webshellLineBuffer = '';
            webshellHistoryIndex = -1;
            term.write(WEBSHELL_PROMPT);
            clearWebshellTerminalLog(terminalKey);
            return;
        }
        // Ctrl+C：当前实现不支持远程中断，给出提示并回到提示符
        if (data === '\x03') {
            if (webshellTerminalRunning) {
                writeWebshellOutput(term, wsTOr('webshell.ctrlCNotSupported', '^C (当前版本暂不支持中断远程命令)'), true);
                appendWebshellTerminalLog(terminalKey, wsTOr('webshell.ctrlCNotSupported', '^C (当前版本暂不支持中断远程命令)') + '\n');
            }
            webshellLineBuffer = '';
            webshellHistoryIndex = -1;
            term.write(WEBSHELL_PROMPT);
            return;
        }
        // Ctrl+U：清空当前输入行
        if (data === '\x15') {
            webshellLineBuffer = '';
            term.write('\x1b[2K\r' + WEBSHELL_PROMPT);
            return;
        }
        // 上/下键：命令历史
        if (data === '\x1b[A' || data === '\x1bOA') {
            var hist = getWebshellHistory(terminalKey);
            if (hist.length === 0) return;
            webshellHistoryIndex = webshellHistoryIndex < 0 ? hist.length : Math.max(0, webshellHistoryIndex - 1);
            webshellLineBuffer = hist[webshellHistoryIndex] || '';
            term.write('\x1b[2K\r' + WEBSHELL_PROMPT + webshellLineBuffer);
            return;
        }
        if (data === '\x1b[B' || data === '\x1bOB') {
            var hist2 = getWebshellHistory(terminalKey);
            if (hist2.length === 0) return;
            webshellHistoryIndex = webshellHistoryIndex < 0 ? -1 : Math.min(hist2.length - 1, webshellHistoryIndex + 1);
            if (webshellHistoryIndex < 0) webshellLineBuffer = '';
            else webshellLineBuffer = hist2[webshellHistoryIndex] || '';
            term.write('\x1b[2K\r' + WEBSHELL_PROMPT + webshellLineBuffer);
            return;
        }
        // 回车：发送当前行到后端执行
        if (data === '\r' || data === '\n') {
            term.writeln('');
            var cmd = webshellLineBuffer.trim();
            webshellLineBuffer = '';
            webshellHistoryIndex = -1;
            if (cmd) {
                if (webshellRunning) {
                    writeWebshellOutput(term, wsT('webshell.waitFinish'), true);
                    appendWebshellTerminalLog(terminalKey, (wsT('webshell.waitFinish') || '请等待当前命令执行完成') + '\n');
                    term.write(WEBSHELL_PROMPT);
                    return;
                }
                pushWebshellHistory(terminalKey, cmd);
                appendWebshellTerminalLog(terminalKey, '$ ' + cmd + '\n');
                webshellRunning = true;
                setWebshellTerminalStatus(true);
                renderWebshellTerminalSessions(conn);
                execWebshellCommand(webshellCurrentConn, cmd).then(function (out) {
                    webshellRunning = false;
                    setWebshellTerminalStatus(false);
                    renderWebshellTerminalSessions(conn);
                    if (out && out.length) {
                        writeWebshellOutput(term, out, false);
                        appendWebshellTerminalLog(terminalKey, String(out).replace(/\r\n/g, '\n').replace(/\r/g, '\n') + '\n');
                    }
                    term.write(WEBSHELL_PROMPT);
                }).catch(function (err) {
                    webshellRunning = false;
                    setWebshellTerminalStatus(false);
                    renderWebshellTerminalSessions(conn);
                    var errMsg = err && err.message ? err.message : wsT('webshell.execError');
                    writeWebshellOutput(term, errMsg, true);
                    appendWebshellTerminalLog(terminalKey, String(errMsg || '') + '\n');
                    term.write(WEBSHELL_PROMPT);
                });
            } else {
                term.write(WEBSHELL_PROMPT);
            }
            return;
        }
        // 多行粘贴：按行依次执行
        if (data.indexOf('\n') !== -1 || data.indexOf('\r') !== -1) {
            var full = (webshellLineBuffer + data).replace(/\r\n/g, '\n').replace(/\r/g, '\n');
            var lines = full.split('\n');
            webshellLineBuffer = lines.pop() || '';
            if (lines.length > 0 && !webshellRunning && webshellCurrentConn) {
                var runNext = function (idx) {
                    if (idx >= lines.length) {
                        term.write(WEBSHELL_PROMPT + webshellLineBuffer);
                        return;
                    }
                    var line = lines[idx].trim();
                    if (!line) { runNext(idx + 1); return; }
                    pushWebshellHistory(terminalKey, line);
                    appendWebshellTerminalLog(terminalKey, '$ ' + line + '\n');
                    webshellRunning = true;
                    setWebshellTerminalStatus(true);
                    renderWebshellTerminalSessions(conn);
                    execWebshellCommand(webshellCurrentConn, line).then(function (out) {
                        if (out && out.length) {
                            writeWebshellOutput(term, out, false);
                            appendWebshellTerminalLog(terminalKey, String(out).replace(/\r\n/g, '\n').replace(/\r/g, '\n') + '\n');
                        }
                        webshellRunning = false;
                        setWebshellTerminalStatus(false);
                        renderWebshellTerminalSessions(conn);
                        runNext(idx + 1);
                    }).catch(function (err) {
                        var em = err && err.message ? err.message : wsT('webshell.execError');
                        writeWebshellOutput(term, em, true);
                        appendWebshellTerminalLog(terminalKey, String(em || '') + '\n');
                        webshellRunning = false;
                        setWebshellTerminalStatus(false);
                        renderWebshellTerminalSessions(conn);
                        runNext(idx + 1);
                    });
                };
                runNext(0);
            } else {
                term.write(data);
            }
            return;
        }
        // 退格
        if (data === '\x7f' || data === '\b') {
            if (webshellLineBuffer.length > 0) {
                webshellLineBuffer = webshellLineBuffer.slice(0, -1);
                term.write('\b \b');
            }
            return;
        }
        webshellLineBuffer += data;
        term.write(data);
    });

    webshellTerminalInstance = term;
    webshellTerminalFitAddon = fitAddon;
    // 延迟再次 fit，确保容器尺寸稳定后光标与文字不错位
    setTimeout(function () {
        try { if (fitAddon) fitAddon.fit(); } catch (e) {}
    }, 100);
    // 容器尺寸变化时重新 fit，避免光标/文字被遮挡
    if (fitAddon && typeof ResizeObserver !== 'undefined' && container) {
        webshellTerminalResizeContainer = container;
        webshellTerminalResizeObserver = new ResizeObserver(function () {
            try { fitAddon.fit(); } catch (e) {}
        });
        webshellTerminalResizeObserver.observe(container);
    }
    renderWebshellTerminalSessions(conn);
}

// 调用后端执行命令
function execWebshellCommand(conn, command) {
    return new Promise(function (resolve, reject) {
        if (typeof apiFetch === 'undefined') {
            reject(new Error(wsTOr('webshell.apiFetchMissing', 'apiFetch 未定义')));
            return;
        }
        apiFetch('/api/webshell/exec', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                url: conn.url,
                password: conn.password || '',
                type: conn.type || 'php',
                method: (conn.method || 'post').toLowerCase(),
                cmd_param: conn.cmdParam || '',
                encoding: webshellConnEncoding(conn),
                os: webshellConnOS(conn),
                connection_id: conn.id || '',
                command: command
            })
        }).then(function (r) { return r.json(); })
            .then(function (data) {
                if (data && data.output !== undefined) resolve(data.output || '');
                else if (data && data.error) reject(new Error(data.error));
                else resolve('');
            })
            .catch(reject);
    });
}

// ---------- 文件管理 ----------
function webshellFileListDir(conn, path) {
    const listEl = document.getElementById('webshell-file-list');
    if (!listEl) return;
    listEl.innerHTML = '<div class="webshell-loading">' + wsT('common.refresh') + '...</div>';

    if (typeof apiFetch === 'undefined') {
        listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(wsTOr('webshell.apiFetchMissing', 'apiFetch 未定义')) + '</div>';
        return;
    }

    apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'list', path: path })
    }).then(function (r) { return r.json(); })
        .then(function (data) {
            applyWebshellDetectedOS(conn, data);
            if (!data.ok && data.error) {
                listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(data.error) + '</div><pre class="webshell-file-raw">' + escapeHtml(data.output || '') + '</pre>';
                return;
            }
            var normalizedPath = normalizeWebshellPath(path);
            var inferredPath = inferPathFromWindowsDirOutput(data.output || '');
            var displayPath = inferredPath || normalizedPath;
            listEl.dataset.currentPath = displayPath;
            listEl.dataset.rawOutput = data.output || '';
            var pathInput = document.getElementById('webshell-file-path');
            if (pathInput) pathInput.value = displayPath;
            renderFileList(listEl, displayPath, data.output || '', conn);
        })
        .catch(function (err) {
            listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(err && err.message ? err.message : wsT('webshell.execError')) + '</div>';
        });
}

function normalizeLsMtime(month, day, timeOrYear) {
    if (!month || !day || !timeOrYear) return '';
    var token = String(timeOrYear).trim();
    if (/^\d{4}$/.test(token)) return token + ' ' + month + ' ' + day;
    var now = new Date();
    var year = now.getFullYear();
    if (/^\d{1,2}:\d{2}$/.test(token)) {
        var monthMap = { Jan: 0, Feb: 1, Mar: 2, Apr: 3, May: 4, Jun: 5, Jul: 6, Aug: 7, Sep: 8, Oct: 9, Nov: 10, Dec: 11 };
        var m = monthMap[month];
        var d = parseInt(day, 10);
        if (m != null && !isNaN(d)) {
            var inferred = new Date(year, m, d);
            if (inferred.getTime() > now.getTime()) year = year - 1;
        }
        return year + ' ' + month + ' ' + day + ' ' + token;
    }
    return month + ' ' + day + ' ' + token;
}

function modeToType(mode) {
    if (!mode || !mode.length) return '';
    var c = mode.charAt(0);
    if (c === 'd') return 'dir';
    if (c === '-') return 'file';
    if (c === 'l') return 'link';
    if (c === 'c') return 'char';
    if (c === 'b') return 'block';
    if (c === 's') return 'socket';
    if (c === 'p') return 'pipe';
    return c;
}

function parseWindowsDirEntry(line) {
    var m = String(line || '').match(/^(\d{4}[\/-]\d{1,2}[\/-]\d{1,2})\s+(\d{1,2}:\d{2})(?:\s*(AM|PM))?\s+(<[^>]+>|[\d,]+)\s+(.+?)\s*$/i);
    if (!m) return null;
    var kind = (m[4] || '').trim();
    var name = (m[5] || '').trim();
    if (!name || name === '.' || name === '..') return null;
    var isDir = /^<(dir|junction|symlinkd)>$/i.test(kind);
    var size = isDir ? '' : kind.replace(/,/g, '');
    var mtime = (m[1] + ' ' + m[2] + (m[3] ? (' ' + m[3].toUpperCase()) : '')).trim();
    return {
        name: name,
        isDir: isDir,
        size: size,
        mtime: mtime,
        mode: isDir ? 'd' : '-',
        owner: '',
        group: '',
        type: isDir ? 'dir' : 'file'
    };
}

function parseWebshellListItems(rawOutput) {
    var lines = (rawOutput || '').split(/\n/).filter(function (l) { return l.trim(); });
    var items = [];
    for (var i = 0; i < lines.length; i++) {
        var line = lines[i];
        var trimmedLine = String(line || '').trim();
        // `ls -la` 首行常见 "total 12"（中文环境为 "总计 12"），不是文件项。
        if (/^(total|总计)\s+\d+$/i.test(trimmedLine)) continue;
        // `dir` 头尾信息（中英文）与 shell 提示符，不是目录项。
        if (/^(驱动器|卷的序列号是|volume in drive|volume serial number is|directory of)/i.test(trimmedLine)) continue;
        if (/^[A-Za-z]:\\.*\s+的目录$/i.test(trimmedLine)) continue;
        if (/^\d+\s+(个文件|file\(s\))\s+[\d,]+\s+(字节|bytes?)$/i.test(trimmedLine)) continue;
        if (/^\d+\s+(个目录|dir\(s\))\s+[\d,]+\s+(可用字节|bytes free)$/i.test(trimmedLine)) continue;
        if (/^[^>\n]*>\s*dir(?:\s|$)/i.test(trimmedLine)) continue;
        var name = '';
        var isDir = false;
        var size = '';
        var mode = '';
        var mtime = '';
        var owner = '';
        var group = '';
        var type = '';
        var mLs = line.match(/^(\S+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\d+)\s+([A-Za-z]{3})\s+(\d{1,2})\s+(\S+)\s+(.+)$/);
        if (mLs) {
            mode = mLs[1];
            owner = mLs[3];
            group = mLs[4];
            size = mLs[5];
            mtime = normalizeLsMtime(mLs[6], mLs[7], mLs[8]);
            name = (mLs[9] || '').trim();
            isDir = mode && mode.startsWith('d');
            type = modeToType(mode);
        } else {
            var winItem = parseWindowsDirEntry(line);
            if (winItem) {
                items.push({
                    name: winItem.name,
                    isDir: winItem.isDir,
                    line: line,
                    size: winItem.size,
                    mode: winItem.mode,
                    mtime: winItem.mtime,
                    owner: winItem.owner,
                    group: winItem.group,
                    type: winItem.type
                });
                continue;
            }
            // 仅兜底解析 Unix 权限格式，避免把 `dir` 统计行误识别为文件。
            if (/^[bcdlps-][rwxStTs-]{9}[+.@]?\s/.test(line)) {
                var parts = line.trim().split(/\s+/);
                if (parts.length >= 9) {
                    name = parts.slice(8).join(' ').trim();
                } else {
                    name = parts.length ? parts[parts.length - 1].trim() : line.trim();
                }
                if (name === '.' || name === '..') continue;
                isDir = line.startsWith('d');
                parts = line.split(/\s+/);
                if (parts.length >= 5) { mode = parts[0]; size = parts[4]; }
                if (parts.length >= 4) { owner = parts[2] || ''; group = parts[3] || ''; }
                if (parts.length >= 8 && /^[A-Za-z]{3}$/.test(parts[5])) mtime = normalizeLsMtime(parts[5], parts[6], parts[7]);
                type = modeToType(mode);
            } else {
                continue;
            }
        }
        if (name === '.' || name === '..') continue;
        items.push({ name: name, isDir: isDir, line: line, size: size, mode: mode, mtime: mtime, owner: owner, group: group, type: type });
    }
    return items;
}

function fetchWebshellDirectoryItems(conn, path) {
    if (!conn || typeof apiFetch === 'undefined') return Promise.resolve([]);
    return apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'list', path: path })
    }).then(function (r) { return r.json(); }).then(function (data) {
        applyWebshellDetectedOS(conn, data);
        if (!data || data.error || !data.ok) return [];
        return parseWebshellListItems(data.output || '');
    }).catch(function () {
        return [];
    });
}

function renderFileList(listEl, currentPath, rawOutput, conn, nameFilter) {
    currentPath = normalizeWebshellPath(currentPath);
    var items = parseWebshellListItems(rawOutput);
    var selectedPath = getWebshellSelectedFile(conn || webshellCurrentConn);
    if (nameFilter && nameFilter.trim()) {
        var f = nameFilter.trim().toLowerCase();
        items = items.filter(function (item) { return item.name.toLowerCase().indexOf(f) !== -1; });
    }
    // 面包屑
    var breadcrumbEl = document.getElementById('webshell-file-breadcrumb');
    if (breadcrumbEl) {
        var absolute = currentPath.startsWith('/');
        var parts = (currentPath === '.' || currentPath === '/' || currentPath === '') ? [] : currentPath.replace(/^\//, '').split('/');
        breadcrumbEl.innerHTML = '<a href="#" class="webshell-breadcrumb-item" data-path="' + (absolute ? '/' : '.') + '">' + (wsT('webshell.breadcrumbHome') || '根') + '</a>' +
            parts.map(function (p, idx) {
                var path = (absolute ? '/' : '') + parts.slice(0, idx + 1).join('/');
                return ' / <a href="#" class="webshell-breadcrumb-item" data-path="' + escapeHtml(path) + '">' + escapeHtml(p) + '</a>';
            }).join('');
    }
    renderDirectoryTree(currentPath, items, conn);
    var html = '';
    if (items.length === 0) {
        // 目录为空/过滤后为空时，给出明确空状态，避免 tbody 留白导致“整块抽象大白屏”
        if (rawOutput.trim() && !nameFilter) {
            html = '<pre class="webshell-file-raw">' + escapeHtml(rawOutput) + '</pre>';
        } else {
            html = '<table class="webshell-file-table"><thead><tr><th class="webshell-col-check"><input type="checkbox" id="webshell-file-select-all" title="' + (wsT('webshell.selectAll') || '全选') + '" /></th><th>' + wsT('webshell.filePath') + '</th><th class="webshell-col-size">' + (wsT('webshell.colSize') || '大小') + '</th><th class="webshell-col-mtime">' + (wsT('webshell.colModifiedAt') || '修改时间') + '</th><th class="webshell-col-owner">' + (wsT('webshell.colOwner') || '所有者') + '</th><th class="webshell-col-perms">' + (wsT('webshell.colPerms') || '权限') + '</th><th class="webshell-col-actions"></th></tr></thead><tbody>' +
                '<tr><td colspan="7" class="webshell-file-empty-state">' + (wsT('common.noData') || '暂无文件') + '</td></tr>' +
                '</tbody></table>';
        }
    } else {
        html = '<table class="webshell-file-table"><thead><tr><th class="webshell-col-check"><input type="checkbox" id="webshell-file-select-all" title="' + (wsT('webshell.selectAll') || '全选') + '" /></th><th>' + wsT('webshell.filePath') + '</th><th class="webshell-col-size">' + (wsT('webshell.colSize') || '大小') + '</th><th class="webshell-col-mtime">' + (wsT('webshell.colModifiedAt') || '修改时间') + '</th><th class="webshell-col-owner">' + (wsT('webshell.colOwner') || '所有者') + '</th><th class="webshell-col-perms">' + (wsT('webshell.colPerms') || '权限') + '</th><th class="webshell-col-actions"></th></tr></thead><tbody>';
        if (currentPath !== '.' && currentPath !== '') {
            html += '<tr><td></td><td><a href="#" class="webshell-file-link" data-path="' + escapeHtml(getWebshellParentPath(currentPath)) + '" data-isdir="1">..</a></td><td></td><td></td><td></td><td></td><td></td></tr>';
        }
        items.forEach(function (item) {
            var pathNext = currentPath === '.' ? item.name : currentPath + '/' + item.name;
            var pathNextNorm = normalizeWebshellPath(pathNext);
            var nameClass = item.isDir ? 'is-dir' : 'is-file';
            html += '<tr class="' + (!item.isDir && selectedPath === pathNextNorm ? 'webshell-file-row-selected' : '') + '"><td class="webshell-col-check">';
            if (!item.isDir) html += '<input type="checkbox" class="webshell-file-cb" data-path="' + escapeHtml(pathNext) + '" />';
            html += '</td><td class="webshell-col-name"><a href="#" class="webshell-file-link ' + nameClass + '" title="' + escapeHtml(item.name) + '" data-path="' + escapeHtml(pathNext) + '" data-isdir="' + (item.isDir ? '1' : '0') + '">' + escapeHtml(item.name) + (item.isDir ? '/' : '') + '</a></td>';
            html += '<td class="webshell-col-size">' + escapeHtml(item.size) + '</td>';
            html += '<td class="webshell-col-mtime">' + escapeHtml(item.mtime || '') + '</td>';
            html += '<td class="webshell-col-owner">' + escapeHtml(item.owner || '') + '</td>';
            html += '<td class="webshell-col-perms">' + escapeHtml(item.mode || '') + '</td>';
            html += '<td class="webshell-col-actions">';
            if (item.isDir) {
                html += '<button type="button" class="btn-ghost btn-sm webshell-file-rename" data-path="' + escapeHtml(pathNext) + '" data-name="' + escapeHtml(item.name) + '">' + (wsT('webshell.rename') || '重命名') + '</button>';
            } else {
                var actionsLabel = wsT('common.actions') || '操作';
                html += '<details class="webshell-row-actions"><summary class="btn-ghost btn-sm webshell-row-actions-btn" title="' + actionsLabel + '">' + actionsLabel + '</summary>' +
                    '<div class="webshell-row-actions-menu">' +
                    '<button type="button" class="btn-ghost btn-sm webshell-file-read" data-path="' + escapeHtml(pathNext) + '">' + wsT('webshell.readFile') + '</button>' +
                    '<button type="button" class="btn-ghost btn-sm webshell-file-download" data-path="' + escapeHtml(pathNext) + '">' + wsT('webshell.downloadFile') + '</button>' +
                    '<button type="button" class="btn-ghost btn-sm webshell-file-edit" data-path="' + escapeHtml(pathNext) + '">' + wsT('webshell.editFile') + '</button>' +
                    '<button type="button" class="btn-ghost btn-sm webshell-file-rename" data-path="' + escapeHtml(pathNext) + '" data-name="' + escapeHtml(item.name) + '">' + (wsT('webshell.rename') || '重命名') + '</button>' +
                    '<button type="button" class="btn-ghost btn-sm webshell-file-del" data-path="' + escapeHtml(pathNext) + '">' + wsT('webshell.deleteFile') + '</button>' +
                    '</div></details>';
            }
            html += '</td></tr>';
        });
        html += '</tbody></table>';
    }
    listEl.innerHTML = html;

    listEl.querySelectorAll('.webshell-file-link').forEach(function (a) {
        a.addEventListener('click', function (e) {
            e.preventDefault();
            const path = a.getAttribute('data-path');
            const isDir = a.getAttribute('data-isdir') === '1';
            const pathInput = document.getElementById('webshell-file-path');
            if (isDir) {
                setWebshellSelectedFile(webshellCurrentConn, '');
                if (pathInput) pathInput.value = path;
                webshellFileListDir(webshellCurrentConn, path);
            } else {
                // 打开文件时保留当前“浏览目录”上下文，避免返回时落到单文件视图
                setWebshellSelectedFile(webshellCurrentConn, path);
                renderDirectoryTree(currentPath, items, conn || webshellCurrentConn);
                webshellFileRead(webshellCurrentConn, path, listEl, currentPath);
            }
        });
    });
    listEl.querySelectorAll('.webshell-file-read').forEach(function (btn) {
        btn.addEventListener('click', function (e) {
            e.preventDefault();
            var filePath = btn.getAttribute('data-path');
            setWebshellSelectedFile(webshellCurrentConn, filePath);
            renderDirectoryTree(currentPath, items, conn || webshellCurrentConn);
            webshellFileRead(webshellCurrentConn, filePath, listEl, currentPath);
        });
    });
    listEl.querySelectorAll('.webshell-file-download').forEach(function (btn) {
        btn.addEventListener('click', function (e) {
            e.preventDefault();
            webshellFileDownload(webshellCurrentConn, btn.getAttribute('data-path'));
        });
    });
    listEl.querySelectorAll('.webshell-file-edit').forEach(function (btn) {
        btn.addEventListener('click', function (e) {
            e.preventDefault();
            webshellFileEdit(webshellCurrentConn, btn.getAttribute('data-path'), listEl);
        });
    });
    listEl.querySelectorAll('.webshell-file-del').forEach(function (btn) {
        btn.addEventListener('click', function (e) {
            e.preventDefault();
            if (!confirm(wsT('webshell.deleteConfirm'))) return;
            webshellFileDelete(webshellCurrentConn, btn.getAttribute('data-path'), function () {
                webshellFileListDir(webshellCurrentConn, document.getElementById('webshell-file-path').value.trim() || '.');
            });
        });
    });
    listEl.querySelectorAll('.webshell-file-rename').forEach(function (btn) {
        btn.addEventListener('click', function (e) {
            e.preventDefault();
            webshellFileRename(webshellCurrentConn, btn.getAttribute('data-path'), btn.getAttribute('data-name'), listEl);
        });
    });
    var selectAll = document.getElementById('webshell-file-select-all');
    if (selectAll) {
        selectAll.addEventListener('change', function () {
            listEl.querySelectorAll('.webshell-file-cb').forEach(function (cb) { cb.checked = selectAll.checked; });
        });
    }
    if (breadcrumbEl) {
        breadcrumbEl.querySelectorAll('.webshell-breadcrumb-item').forEach(function (a) {
            a.addEventListener('click', function (e) {
                e.preventDefault();
                var p = a.getAttribute('data-path');
                var pathInput = document.getElementById('webshell-file-path');
                if (pathInput) pathInput.value = p;
                webshellFileListDir(webshellCurrentConn, p);
            });
        });
    }
}

function renderDirectoryTree(currentPath, items, conn) {
    var treeEl = document.getElementById('webshell-dir-tree');
    if (!treeEl) return;
    var state = getWebshellTreeState(conn || webshellCurrentConn);
    var curr = normalizeWebshellPath(currentPath);
    var dirs = (items || []).filter(function (item) { return item && item.isDir; });
    if (!state) {
        treeEl.innerHTML = '<div class="webshell-empty">' + escapeHtml(wsTOr('webshell.noDirectories', '暂无目录')) + '</div>';
        return;
    }
    var tree = state.tree;
    var expanded = state.expanded;
    var loaded = state.loaded;
    var selectedPath = getWebshellSelectedFile(conn || webshellCurrentConn);
    var rootPath = curr.startsWith('/') ? '/' : '.';
    if (!tree[rootPath]) tree[rootPath] = [];
    if (expanded[rootPath] !== false) expanded[rootPath] = true;

    // 把当前目录的子项（目录+文件）同步到树缓存
    var childNodes = (items || []).map(function (item) {
        var childPath = curr === '.' ? normalizeWebshellPath(item.name) : normalizeWebshellPath(curr + '/' + item.name);
        return {
            path: childPath,
            name: item.name,
            isDir: !!item.isDir
        };
    }).filter(function (n) { return !!n.path; });
    childNodes.sort(function (a, b) {
        // 目录优先，再按名称排序
        if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
        return (a.name || '').localeCompare(b.name || '');
    });
    tree[curr] = childNodes;
    loaded[curr] = true;
    childNodes.forEach(function (node) {
        if (node.isDir && !tree[node.path]) tree[node.path] = [];
    });

    // 仅对“真实路径”补祖先链；相对上探链（../..）不构建，避免出现假层级。
    var isRelativeUpChain = /^(?:\.\.\/)*\.\.$/.test(curr);
    var parts = (curr === '.' || curr === '/') ? [] : curr.replace(/^\//, '').split('/');
    var parentPath = rootPath;
    if (!isRelativeUpChain) {
        for (var i = 0; i < parts.length; i++) {
            var nextPath = parentPath === '.' ? parts[i] : normalizeWebshellPath(parentPath + '/' + parts[i]);
            if (!tree[parentPath]) tree[parentPath] = [];
            var parentChildren = tree[parentPath];
            var hasAncestorNode = parentChildren.some(function (n) { return n && n.path === nextPath; });
            if (!hasAncestorNode) {
                parentChildren.push({ path: nextPath, name: parts[i], isDir: true });
                parentChildren.sort(function (a, b) {
                    if (!!a.isDir !== !!b.isDir) return a.isDir ? -1 : 1;
                    return (a.name || '').localeCompare(b.name || '');
                });
            }
            if (!tree[nextPath]) tree[nextPath] = [];
            expanded[parentPath] = true;
            parentPath = nextPath;
        }
    }
    if (expanded[curr] == null) expanded[curr] = true;

    function renderNode(node, depth) {
        var path = node.path;
        var isDir = !!node.isDir;
        var children = isDir ? (tree[path] || []).slice() : [];
        var hasLoadedChildren = isDir ? (loaded[path] === true) : true;
        var canExpand = isDir && (path === rootPath || !hasLoadedChildren || children.length > 0);
        var hasChildren = children.length > 0;
        var isExpanded = isDir ? (expanded[path] === true) : false;
        var isActive = path === curr;
        var isSelectedFile = !isDir && path === selectedPath;
        var name = node.name;
        var icon = isDir ? (path === rootPath ? '🗂' : '📁') : '📄';
        var nodeHtml =
            '<div class="webshell-tree-node" data-depth="' + depth + '">' +
            '<div class="webshell-tree-row' + (isActive ? ' active' : '') + (isSelectedFile ? ' selected-file' : '') + '">' +
            '<button type="button" class="webshell-tree-toggle' + (canExpand ? '' : ' empty') + '" data-path="' + escapeHtml(path) + '">' + (canExpand ? (isExpanded ? '▾' : '▸') : '·') + '</button>' +
            '<button type="button" class="webshell-dir-item' + (isDir ? ' is-dir' : ' is-file') + '" title="' + escapeHtml(name) + '" data-path="' + escapeHtml(path) + '" data-isdir="' + (isDir ? '1' : '0') + '"><span class="webshell-tree-icon">' + icon + '</span><span class="webshell-tree-name">' + escapeHtml(name) + '</span></button>' +
            '</div>';
        if (isDir && hasChildren && isExpanded) {
            nodeHtml += '<div class="webshell-tree-children">';
            for (var j = 0; j < children.length; j++) {
                nodeHtml += renderNode(children[j], depth + 1);
            }
            nodeHtml += '</div>';
        }
        nodeHtml += '</div>';
        return nodeHtml;
    }

    treeEl.innerHTML = '<div class="webshell-tree-root">' + renderNode({ path: rootPath, name: '/', isDir: true }, 0) + '</div>';
    treeEl.querySelectorAll('.webshell-tree-toggle').forEach(function (btn) {
        btn.addEventListener('click', function (e) {
            e.preventDefault();
            e.stopPropagation();
            var p = normalizeWebshellPath(btn.getAttribute('data-path') || '.');
            if (expanded[p] === true) {
                expanded[p] = false;
                renderDirectoryTree(curr, items, conn || webshellCurrentConn);
                return;
            }
            if (loaded[p] === true) {
                expanded[p] = true;
                renderDirectoryTree(curr, items, conn || webshellCurrentConn);
                return;
            }
            fetchWebshellDirectoryItems(conn || webshellCurrentConn, p).then(function (subItems) {
                var nextChildren = (subItems || []).map(function (it) {
                    return {
                        path: p === '.' ? normalizeWebshellPath(it.name) : normalizeWebshellPath(p + '/' + it.name),
                        name: it.name,
                        isDir: !!it.isDir
                    };
                }).filter(function (n) { return !!n.path; }).sort(function (a, b) {
                    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
                    return (a.name || '').localeCompare(b.name || '');
                });
                tree[p] = nextChildren;
                nextChildren.forEach(function (childNode) {
                    if (childNode.isDir) {
                        if (!tree[childNode.path]) tree[childNode.path] = [];
                        if (loaded[childNode.path] == null) loaded[childNode.path] = false;
                    }
                });
                loaded[p] = true;
                expanded[p] = true;
                renderDirectoryTree(curr, items, conn || webshellCurrentConn);
            });
        });
    });
    treeEl.querySelectorAll('.webshell-dir-item').forEach(function (btn) {
        btn.addEventListener('click', function () {
            var p = normalizeWebshellPath(btn.getAttribute('data-path') || '.');
            var isDir = btn.getAttribute('data-isdir') === '1';
            var pathInput = document.getElementById('webshell-file-path');
            if (isDir) {
                setWebshellSelectedFile(webshellCurrentConn, '');
                if (pathInput) pathInput.value = p;
                webshellFileListDir(webshellCurrentConn, p);
                return;
            }
            var listEl = document.getElementById('webshell-file-list');
            var browsePath = p.replace(/\/[^/]+$/, '') || '.';
            setWebshellSelectedFile(webshellCurrentConn, p);
            renderDirectoryTree(curr, items, conn || webshellCurrentConn);
            if (listEl) webshellFileRead(webshellCurrentConn, p, listEl, browsePath);
        });
    });
}

function webshellFileListApplyFilter() {
    var listEl = document.getElementById('webshell-file-list');
    var path = listEl && listEl.dataset.currentPath ? listEl.dataset.currentPath : (document.getElementById('webshell-file-path') && document.getElementById('webshell-file-path').value.trim()) || '.';
    var raw = listEl && listEl.dataset.rawOutput ? listEl.dataset.rawOutput : '';
    var filterInput = document.getElementById('webshell-file-filter');
    var filter = filterInput ? filterInput.value : '';
    if (!listEl || !raw) return;
    renderFileList(listEl, path, raw, webshellCurrentConn, filter);
}

function webshellFileMkdir(conn, pathInput) {
    if (!conn || typeof apiFetch === 'undefined') return;
    var base = (pathInput && pathInput.value.trim()) || '.';
    var name = prompt(wsT('webshell.newDir') || '新建目录', 'newdir');
    if (name == null || !name.trim()) return;
    var path = base === '.' ? name.trim() : base + '/' + name.trim();
    apiFetch('/api/webshell/file', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: webshellFileRequestBody(conn, { action: 'mkdir', path: path }) })
        .then(function (r) { return r.json(); })
        .then(function () { webshellFileListDir(conn, base); })
        .catch(function () { webshellFileListDir(conn, base); });
}

function webshellFileNewFile(conn, pathInput) {
    if (!conn || typeof apiFetch === 'undefined') return;
    var base = (pathInput && pathInput.value.trim()) || '.';
    var name = prompt(wsT('webshell.newFile') || '新建文件', 'newfile.txt');
    if (name == null || !name.trim()) return;
    var path = base === '.' ? name.trim() : base + '/' + name.trim();
    var content = prompt(wsTOr('webshell.newFileContentPrompt', '初始内容（可选）'), '');
    if (content === null) return;
    var listEl = document.getElementById('webshell-file-list');
    webshellFileWrite(conn, path, content || '', function () { webshellFileListDir(conn, base); }, listEl);
}

function webshellFileUpload(conn, pathInput) {
    if (!conn || typeof apiFetch === 'undefined') return;
    var base = (pathInput && pathInput.value.trim()) || '.';
    var input = document.createElement('input');
    input.type = 'file';
    input.multiple = false;
    input.onchange = function () {
        var file = input.files && input.files[0];
        if (!file) return;
        var reader = new FileReader();
        reader.onload = function () {
            var buf = reader.result;
            var bin = new Uint8Array(buf);
            var CHUNK = 32000;
            var base64Chunks = [];
            for (var i = 0; i < bin.length; i += CHUNK) {
                var slice = bin.subarray(i, Math.min(i + CHUNK, bin.length));
                var b64 = btoa(String.fromCharCode.apply(null, slice));
                base64Chunks.push(b64);
            }
            var path = base === '.' ? file.name : base + '/' + file.name;
            var listEl = document.getElementById('webshell-file-list');
            if (listEl) listEl.innerHTML = '<div class="webshell-loading">' + (wsT('webshell.upload') || '上传') + '...</div>';
            var idx = 0;
            function sendNext() {
                if (idx >= base64Chunks.length) {
                    webshellFileListDir(conn, base);
                    return;
                }
                apiFetch('/api/webshell/file', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: webshellFileRequestBody(conn, { action: 'upload_chunk', path: path, content: base64Chunks[idx], chunk_index: idx }) })
                    .then(function (r) { return r.json(); })
                    .then(function () { idx++; sendNext(); })
                    .catch(function () { idx++; sendNext(); });
            }
            sendNext();
        };
        reader.readAsArrayBuffer(file);
    };
    input.click();
}

function webshellFileRename(conn, oldPath, oldName, listEl) {
    if (!conn || typeof apiFetch === 'undefined') return;
    var newName = prompt((wsT('webshell.rename') || '重命名') + ': ' + oldName, oldName);
    if (newName == null || newName.trim() === '') return;
    var parts = oldPath.split('/');
    var dir = parts.length > 1 ? parts.slice(0, -1).join('/') + '/' : '';
    var newPath = dir + newName.trim();
    apiFetch('/api/webshell/file', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: webshellFileRequestBody(conn, { action: 'rename', path: oldPath, target_path: newPath }) })
        .then(function (r) { return r.json(); })
        .then(function () { webshellFileListDir(conn, document.getElementById('webshell-file-path').value.trim() || '.'); })
        .catch(function () { webshellFileListDir(conn, document.getElementById('webshell-file-path').value.trim() || '.'); });
}

function webshellBatchDelete(conn, pathInput) {
    if (!conn) return;
    var listEl = document.getElementById('webshell-file-list');
    var checked = listEl ? listEl.querySelectorAll('.webshell-file-cb:checked') : [];
    var paths = [];
    checked.forEach(function (cb) { paths.push(cb.getAttribute('data-path')); });
    if (paths.length === 0) { alert(wsTOr('webshell.batchDeleteSelectFirst', '批量删除：请先勾选文件')); return; }
    if (!confirm(typeof window.t === 'function' ? window.t('webshell.batchDeleteConfirm', { count: paths.length }) : (wsT('webshell.batchDelete') + '：确定删除 ' + paths.length + ' 个文件？'))) return;
    var base = (pathInput && pathInput.value.trim()) || '.';
    var i = 0;
    function delNext() {
        if (i >= paths.length) { webshellFileListDir(conn, base); return; }
        webshellFileDelete(conn, paths[i], function () { i++; delNext(); });
    }
    delNext();
}

function webshellBatchDownload(conn, pathInput) {
    if (!conn) return;
    var listEl = document.getElementById('webshell-file-list');
    var checked = listEl ? listEl.querySelectorAll('.webshell-file-cb:checked') : [];
    var paths = [];
    checked.forEach(function (cb) { paths.push(cb.getAttribute('data-path')); });
    if (paths.length === 0) { alert(wsTOr('webshell.batchDownloadSelectFirst', '批量下载：请先勾选文件')); return; }
    paths.forEach(function (path) { webshellFileDownload(conn, path); });
}

// 下载文件到本地（读取内容后触发浏览器下载）
function webshellFileDownload(conn, path) {
    if (typeof apiFetch === 'undefined') return;
    apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'read', path: path })
    }).then(function (r) { return r.json(); })
        .then(function (data) {
            var content = (data && data.output) != null ? data.output : (data.error || '');
            var name = path.replace(/^.*[/\\]/, '') || 'download.txt';
            var blob = new Blob([content], { type: 'application/octet-stream' });
            var a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = name;
            a.click();
            URL.revokeObjectURL(a.href);
        })
        .catch(function (err) { alert(wsT('webshell.execError') + ': ' + (err && err.message ? err.message : '')); });
}

function webshellFileRead(conn, path, listEl, browsePath) {
    if (typeof apiFetch === 'undefined') return;
    listEl.innerHTML = '<div class="webshell-loading">' + wsT('webshell.readFile') + '...</div>';
    apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'read', path: path })
    }).then(function (r) { return r.json(); })
        .then(function (data) {
            const out = (data && data.output) ? data.output : (data.error || '');
            var backPath = (browsePath && String(browsePath).trim()) ? String(browsePath).trim() : ((document.getElementById('webshell-file-path') && document.getElementById('webshell-file-path').value.trim()) || '.');
            if (backPath === path) {
                // 兜底：若路径被污染成文件路径，回退到父目录
                backPath = path.replace(/\/[^/]+$/, '') || '.';
            }
            listEl.innerHTML = '<div class="webshell-file-content"><div class="webshell-file-content-path">' + escapeHtml(path) + '</div><pre>' + escapeHtml(out) + '</pre><button type="button" class="btn-ghost" id="webshell-file-back-btn" data-back-path="' + escapeHtml(backPath) + '">' + wsT('webshell.back') + '</button></div>';
            var backBtn = document.getElementById('webshell-file-back-btn');
            if (backBtn) {
                backBtn.addEventListener('click', function () {
                    var p = backBtn.getAttribute('data-back-path') || '.';
                    webshellFileListDir(webshellCurrentConn, p);
                });
            }
        })
        .catch(function (err) {
            listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(err && err.message ? err.message : '') + '</div>';
        });
}

function webshellFileEdit(conn, path, listEl) {
    if (typeof apiFetch === 'undefined') return;
    listEl.innerHTML = '<div class="webshell-loading">' + wsT('webshell.editFile') + '...</div>';
    apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'read', path: path })
    }).then(function (r) { return r.json(); })
        .then(function (data) {
            const content = (data && data.output) ? data.output : (data.error || '');
            const pathInput = document.getElementById('webshell-file-path');
            const currentPath = pathInput ? pathInput.value.trim() || '.' : '.';
            listEl.innerHTML =
                '<div class="webshell-file-edit-wrap">' +
                '<div class="webshell-file-edit-path">' + escapeHtml(path) + '</div>' +
                '<textarea id="webshell-edit-textarea" class="webshell-file-edit-textarea" rows="18">' + escapeHtml(content) + '</textarea>' +
                '<div class="webshell-file-edit-actions">' +
                '<button type="button" class="btn-primary btn-sm" id="webshell-edit-save">' + wsT('webshell.saveFile') + '</button> ' +
                '<button type="button" class="btn-ghost btn-sm" id="webshell-edit-cancel">' + wsT('webshell.cancelEdit') + '</button>' +
                '</div></div>';
            document.getElementById('webshell-edit-save').addEventListener('click', function () {
                const textarea = document.getElementById('webshell-edit-textarea');
                const newContent = textarea ? textarea.value : '';
                webshellFileWrite(webshellCurrentConn, path, newContent, function () {
                    webshellFileListDir(webshellCurrentConn, currentPath);
                }, listEl);
            });
            document.getElementById('webshell-edit-cancel').addEventListener('click', function () {
                webshellFileListDir(webshellCurrentConn, currentPath);
            });
        })
        .catch(function (err) {
            listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(err && err.message ? err.message : '') + '</div>';
        });
}

function webshellFileWrite(conn, path, content, onDone, listEl) {
    if (typeof apiFetch === 'undefined') return;
    if (listEl) listEl.innerHTML = '<div class="webshell-loading">' + wsT('webshell.saveFile') + '...</div>';
    apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'write', path: path, content: content })
    }).then(function (r) { return r.json(); })
        .then(function (data) {
            if (data && !data.ok && data.error && listEl) {
                listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(data.error) + '</div><pre class="webshell-file-raw">' + escapeHtml(data.output || '') + '</pre>';
                return;
            }
            if (onDone) onDone();
        })
        .catch(function (err) {
            if (listEl) listEl.innerHTML = '<div class="webshell-file-error">' + escapeHtml(err && err.message ? err.message : wsT('webshell.execError')) + '</div>';
        });
}

function webshellFileDelete(conn, path, onDone) {
    if (typeof apiFetch === 'undefined') return;
    apiFetch('/api/webshell/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: webshellFileRequestBody(conn, { action: 'delete', path: path })
    }).then(function (r) { return r.json(); })
        .then(function () { if (onDone) onDone(); })
        .catch(function () { if (onDone) onDone(); });
}

// 删除连接（请求服务端删除后刷新列表）
function deleteWebshell(id) {
    if (!confirm(wsT('webshell.deleteConfirm'))) return;
    if (currentWebshellId === id) destroyWebshellTerminal();
    if (currentWebshellId === id) currentWebshellId = null;
    // 清理本地缓存（服务端会级联删除 SQLite 里的状态）
    delete webshellPersistLoadedByConn[id];
    if (webshellPersistSaveTimersByConn[id]) {
        clearTimeout(webshellPersistSaveTimersByConn[id]);
        delete webshellPersistSaveTimersByConn[id];
    }
    delete webshellTerminalSessionsByConn[id];
    var dbStateKey = getWebshellDbStateStorageKey({ id: id });
    if (dbStateKey) delete webshellDbConfigByConn[dbStateKey];
    Object.keys(webshellTerminalLogsByConn).forEach(function (k) {
        if (k === id || k.indexOf(id + '::') === 0) delete webshellTerminalLogsByConn[k];
    });
    Object.keys(webshellHistoryByConn).forEach(function (k) {
        if (k === id || k.indexOf(id + '::') === 0) delete webshellHistoryByConn[k];
    });
    if (typeof apiFetch === 'undefined') return;
    apiFetch('/api/webshell/connections/' + encodeURIComponent(id), { method: 'DELETE' })
        .then(function () {
            return refreshWebshellConnectionsFromServer();
        })
        .then(function () {
            const workspace = document.getElementById('webshell-workspace');
            if (workspace) {
                workspace.innerHTML = '<div class="webshell-workspace-placeholder">' + wsT('webshell.selectOrAdd') + '</div>';
            }
        })
        .catch(function (e) {
            console.warn('删除 WebShell 连接失败', e);
            refreshWebshellConnectionsFromServer();
        });
}

// 打开添加连接弹窗
function showAddWebshellModal() {
    if (typeof requirePermission === 'function' && !requirePermission('webshell:write')) return;
    var editIdEl = document.getElementById('webshell-edit-id');
    if (editIdEl) editIdEl.value = '';
    document.getElementById('webshell-url').value = '';
    document.getElementById('webshell-password').value = '';
    document.getElementById('webshell-type').value = 'php';
    document.getElementById('webshell-method').value = 'post';
    document.getElementById('webshell-cmd-param').value = '';
    var osSelEl = document.getElementById('webshell-os');
    if (osSelEl) osSelEl.value = 'auto';
    var encSelEl = document.getElementById('webshell-encoding');
    if (encSelEl) encSelEl.value = 'auto';
    populateWebshellProjectSelect('');
    document.getElementById('webshell-remark').value = '';
    var titleEl = document.getElementById('webshell-modal-title');
    if (titleEl) titleEl.textContent = wsT('webshell.addConnection');
    var modal = document.getElementById('webshell-modal');
    if (modal) {
        openAppModal(modal);
        refreshWebshellFormSelects(modal);
    }
}

// 打开编辑连接弹窗（预填当前连接信息）
function showEditWebshellModal(connId) {
    var conn = webshellConnections.find(function (c) { return c.id === connId; });
    if (!conn) return;
    var titleEl = document.getElementById('webshell-modal-title');
    if (titleEl) titleEl.textContent = wsT('webshell.editConnectionTitle');
    openAppModal('webshell-modal', { focus: false });
    deferModalContent(function () {
        var editIdEl = document.getElementById('webshell-edit-id');
        if (editIdEl) editIdEl.value = conn.id;
        document.getElementById('webshell-url').value = conn.url || '';
        document.getElementById('webshell-password').value = conn.password || '';
        document.getElementById('webshell-type').value = conn.type || 'php';
        document.getElementById('webshell-method').value = (conn.method || 'post').toLowerCase();
        document.getElementById('webshell-cmd-param').value = conn.cmdParam || '';
        var osEditEl = document.getElementById('webshell-os');
        if (osEditEl) osEditEl.value = normalizeWebshellOS(conn.os);
        var encEditEl = document.getElementById('webshell-encoding');
        if (encEditEl) encEditEl.value = normalizeWebshellEncoding(conn.encoding);
        populateWebshellProjectSelect(webshellConnectionProjectId(conn));
        document.getElementById('webshell-remark').value = conn.remark || '';
        refreshWebshellFormSelects(document.getElementById('webshell-modal'));
        document.getElementById('webshell-url')?.focus();
    });
}

// 关闭弹窗
function closeWebshellModal() {
    var editIdEl = document.getElementById('webshell-edit-id');
    if (editIdEl) editIdEl.value = '';
    closeAppModal('webshell-modal');
}

// 语言切换时刷新 WebShell 页面内所有由 JS 生成的文案（不重建终端）
function refreshWebshellUIOnLanguageChange() {
    var page = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || '');
    if (page !== 'webshell') return;

    renderWebshellList();
    var workspace = document.getElementById('webshell-workspace');
    if (workspace) {
        if (!currentWebshellId || !webshellCurrentConn) {
            workspace.innerHTML = '<div class="webshell-workspace-placeholder" data-i18n="webshell.selectOrAdd">' + wsT('webshell.selectOrAdd') + '</div>';
        } else {
            // 只更新标签文案，不重建终端
            var tabTerminal = workspace.querySelector('.webshell-tab[data-tab="terminal"]');
            var tabFile = workspace.querySelector('.webshell-tab[data-tab="file"]');
            var tabAi = workspace.querySelector('.webshell-tab[data-tab="ai"]');
            var tabDb = workspace.querySelector('.webshell-tab[data-tab="db"]');
            var tabMemo = workspace.querySelector('.webshell-tab[data-tab="memo"]');
            if (tabTerminal) tabTerminal.textContent = wsT('webshell.tabTerminal');
            if (tabFile) tabFile.textContent = wsT('webshell.tabFileManager');
            if (tabAi) tabAi.textContent = wsT('webshell.tabAiAssistant') || 'AI 助手';
            if (tabDb) tabDb.textContent = wsT('webshell.tabDbManager') || '数据库管理';
            if (tabMemo) tabMemo.textContent = wsT('webshell.tabMemo') || '备忘录';

            var quickLabel = workspace.querySelector('.webshell-quick-label');
            if (quickLabel) quickLabel.textContent = (wsT('webshell.quickCommands') || '快捷命令') + ':';
            var terminalClearBtn = document.getElementById('webshell-terminal-clear');
            if (terminalClearBtn) {
                terminalClearBtn.title = wsT('webshell.clearScreen') || '清屏';
                terminalClearBtn.textContent = wsT('webshell.clearScreen') || '清屏';
            }
            var terminalCopyBtn = document.getElementById('webshell-terminal-copy-log');
            if (terminalCopyBtn) {
                terminalCopyBtn.title = wsT('webshell.copyTerminalLog') || '复制日志';
                terminalCopyBtn.textContent = wsT('webshell.copyTerminalLog') || '复制日志';
            }
            setWebshellTerminalStatus(webshellTerminalRunning);
            if (webshellCurrentConn) renderWebshellTerminalSessions(webshellCurrentConn);
            var pathLabel = workspace.querySelector('.webshell-file-toolbar label span');
            var fileSidebarTitle = workspace.querySelector('.webshell-file-sidebar-title');
            var fileMoreActionsBtn = workspace.querySelector('.webshell-toolbar-actions-btn');
            var listDirBtn = document.getElementById('webshell-list-dir');
            var parentDirBtn = document.getElementById('webshell-parent-dir');
            if (pathLabel) pathLabel.textContent = wsT('webshell.filePath');
            if (fileSidebarTitle) fileSidebarTitle.textContent = wsT('webshell.dirTree') || '目录列表';
            if (fileMoreActionsBtn) fileMoreActionsBtn.textContent = wsT('webshell.moreActions') || '更多操作';
            if (listDirBtn) listDirBtn.textContent = wsT('webshell.listDir');
            if (parentDirBtn) parentDirBtn.textContent = wsT('webshell.parentDir');
            // 文件管理工具栏按钮（红框区域）：切换语言时立即更新
            var refreshBtn = document.getElementById('webshell-file-refresh');
            var mkdirBtn = document.getElementById('webshell-mkdir-btn');
            var newFileBtn = document.getElementById('webshell-newfile-btn');
            var uploadBtn = document.getElementById('webshell-upload-btn');
            var batchDeleteBtn = document.getElementById('webshell-batch-delete-btn');
            var batchDownloadBtn = document.getElementById('webshell-batch-download-btn');
            var filterInput = document.getElementById('webshell-file-filter');
            if (refreshBtn) { refreshBtn.title = wsT('webshell.refresh') || '刷新'; refreshBtn.textContent = wsT('webshell.refresh') || '刷新'; }
            if (mkdirBtn) mkdirBtn.textContent = wsT('webshell.newDir') || '新建目录';
            if (newFileBtn) newFileBtn.textContent = wsT('webshell.newFile') || '新建文件';
            if (uploadBtn) uploadBtn.textContent = wsT('webshell.upload') || '上传';
            if (batchDeleteBtn) batchDeleteBtn.textContent = wsT('webshell.batchDelete') || '批量删除';
            if (batchDownloadBtn) batchDownloadBtn.textContent = wsT('webshell.batchDownload') || '批量下载';
            if (filterInput) filterInput.placeholder = wsT('webshell.filterPlaceholder') || '过滤文件名';

            // AI 助手区域文案：Tab 内按钮、占位符、系统就绪提示
            var aiNewConvBtn = document.getElementById('webshell-ai-new-conv');
            if (aiNewConvBtn) aiNewConvBtn.textContent = wsT('webshell.aiNewConversation') || '新对话';
            var aiInput = document.getElementById('webshell-ai-input');
            if (aiInput) aiInput.placeholder = wsT('webshell.aiPlaceholder') || '例如：列出当前目录下的文件';
            var aiSendBtn = document.getElementById('webshell-ai-send');
            if (aiSendBtn) aiSendBtn.textContent = wsT('webshell.aiSend') || '发送';
            var aiMemoTitle = document.querySelector('.webshell-memo-head span');
            if (aiMemoTitle) aiMemoTitle.textContent = wsT('webshell.aiMemo') || '备忘录';
            var aiMemoClearBtn = document.getElementById('webshell-ai-memo-clear');
            if (aiMemoClearBtn) aiMemoClearBtn.textContent = wsT('webshell.aiMemoClear') || '清空';
            var aiMemoInput = document.getElementById('webshell-ai-memo-input');
            if (aiMemoInput) aiMemoInput.placeholder = wsT('webshell.aiMemoPlaceholder') || '记录关键命令、测试思路、复现步骤...';
            var aiMemoStatus = document.getElementById('webshell-ai-memo-status');
            if (aiMemoStatus && !aiMemoStatus.classList.contains('error')) {
                var savingText = wsT('webshell.aiMemoSaving') || '保存中...';
                var savedText = wsT('webshell.aiMemoSaved') || '已保存到本地';
                aiMemoStatus.textContent = aiMemoStatus.textContent === savingText ? savingText : savedText;
            }
            var dbTypeLabel = document.querySelector('#webshell-db-type') ? document.querySelector('#webshell-db-type').closest('label') : null;
            if (dbTypeLabel && dbTypeLabel.querySelector('span')) dbTypeLabel.querySelector('span').textContent = wsT('webshell.dbType') || '数据库类型';
            var dbProfileNameLabel = document.querySelector('#webshell-db-profile-name') ? document.querySelector('#webshell-db-profile-name').closest('label') : null;
            if (dbProfileNameLabel && dbProfileNameLabel.querySelector('span')) dbProfileNameLabel.querySelector('span').textContent = wsT('webshell.dbProfileName') || '连接名称';
            var dbHostLabel = document.querySelector('#webshell-db-host') ? document.querySelector('#webshell-db-host').closest('label') : null;
            if (dbHostLabel && dbHostLabel.querySelector('span')) dbHostLabel.querySelector('span').textContent = wsT('webshell.dbHost') || '主机';
            var dbPortLabel = document.querySelector('#webshell-db-port') ? document.querySelector('#webshell-db-port').closest('label') : null;
            if (dbPortLabel && dbPortLabel.querySelector('span')) dbPortLabel.querySelector('span').textContent = wsT('webshell.dbPort') || '端口';
            var dbUserLabel = document.querySelector('#webshell-db-user') ? document.querySelector('#webshell-db-user').closest('label') : null;
            if (dbUserLabel && dbUserLabel.querySelector('span')) dbUserLabel.querySelector('span').textContent = wsT('webshell.dbUsername') || '用户名';
            var dbPassLabel = document.querySelector('#webshell-db-pass') ? document.querySelector('#webshell-db-pass').closest('label') : null;
            if (dbPassLabel && dbPassLabel.querySelector('span')) dbPassLabel.querySelector('span').textContent = wsT('webshell.dbPassword') || '密码';
            var dbNameLabel = document.querySelector('#webshell-db-name') ? document.querySelector('#webshell-db-name').closest('label') : null;
            if (dbNameLabel && dbNameLabel.querySelector('span')) dbNameLabel.querySelector('span').textContent = wsT('webshell.dbName') || '数据库名';
            var dbSqliteLabel = document.querySelector('#webshell-db-sqlite-path') ? document.querySelector('#webshell-db-sqlite-path').closest('label') : null;
            if (dbSqliteLabel && dbSqliteLabel.querySelector('span')) dbSqliteLabel.querySelector('span').textContent = wsT('webshell.dbSqlitePath') || 'SQLite 文件路径';
            var dbSchemaTitle = document.querySelector('.webshell-db-sidebar-head span');
            if (dbSchemaTitle) dbSchemaTitle.textContent = wsT('webshell.dbSchema') || '数据库结构';
            var dbLoadSchemaBtn = document.getElementById('webshell-db-load-schema-btn');
            if (dbLoadSchemaBtn) dbLoadSchemaBtn.textContent = wsT('webshell.dbLoadSchema') || '加载结构';
            var dbTemplateBtn = document.getElementById('webshell-db-template-btn');
            if (dbTemplateBtn) dbTemplateBtn.textContent = wsT('webshell.dbTemplateSql') || '示例 SQL';
            var dbClearBtn = document.getElementById('webshell-db-clear-btn');
            if (dbClearBtn) dbClearBtn.textContent = wsT('webshell.dbClearSql') || '清空 SQL';
            var dbRunBtn = document.getElementById('webshell-db-run-btn');
            if (dbRunBtn) dbRunBtn.textContent = wsT('webshell.dbRunSql') || '执行 SQL';
            var dbTestBtn = document.getElementById('webshell-db-test-btn');
            if (dbTestBtn) dbTestBtn.textContent = wsT('webshell.dbTest') || '测试连接';
            var dbSql = document.getElementById('webshell-db-sql');
            if (dbSql) dbSql.placeholder = wsT('webshell.dbSqlPlaceholder') || '输入 SQL，例如：SELECT version();';
            var dbTitle = document.querySelector('.webshell-db-output-title');
            if (dbTitle) dbTitle.textContent = wsT('webshell.dbOutput') || '执行输出';
            var dbHint = document.querySelector('.webshell-db-hint');
            if (dbHint) dbHint.textContent = wsT('webshell.dbCliHint') || '如果提示命令不存在，请先在目标主机安装对应客户端（mysql/psql/sqlite3/sqlcmd）';
            var dbTreeHint = document.querySelector('.webshell-db-sidebar-hint');
            if (dbTreeHint) dbTreeHint.textContent = wsT('webshell.dbSelectTableHint') || '点击表名可生成查询 SQL';
            var dbAddProfileBtn = document.getElementById('webshell-db-add-profile-btn');
            if (dbAddProfileBtn) dbAddProfileBtn.textContent = '+ ' + (wsT('webshell.dbAddProfile') || '新增连接');
            var dbProfileModalTitle = document.getElementById('webshell-db-profile-modal-title');
            if (dbProfileModalTitle) dbProfileModalTitle.textContent = wsT('webshell.editConnectionTitle') || '编辑连接';
            var dbProfileCancelBtn = document.getElementById('webshell-db-profile-cancel-btn');
            if (dbProfileCancelBtn) dbProfileCancelBtn.textContent = wsT('common.cancel') || '取消';
            var dbProfileSaveBtn = document.getElementById('webshell-db-profile-save-btn');
            if (dbProfileSaveBtn) dbProfileSaveBtn.textContent = wsT('common.save') || '保存';
            document.querySelectorAll('.webshell-db-profile-menu[data-action="edit"]').forEach(function (el) {
                el.title = wsT('webshell.editConnection') || '编辑';
            });
            document.querySelectorAll('.webshell-db-profile-menu[data-action="delete"]').forEach(function (el) {
                el.title = wsT('webshell.dbDeleteProfile') || '删除连接';
            });
            var dbTree = document.getElementById('webshell-db-schema-tree');
            if (dbTree && !dbTree.querySelector('.webshell-db-group')) {
                dbTree.innerHTML = '<div class="webshell-empty">' + escapeHtml(wsT('webshell.dbNoSchema') || '暂无数据库结构，请先加载') + '</div>';
            }

            // 如果当前 AI 对话区只有系统就绪提示（没有用户消息），用当前语言重置这条提示
            var aiMessages = document.getElementById('webshell-ai-messages');
            if (aiMessages) {
                var hasUserMsg = !!aiMessages.querySelector('.webshell-ai-msg.user');
                var msgNodes = aiMessages.querySelectorAll('.webshell-ai-msg');
                if (!hasUserMsg && msgNodes.length <= 1) {
                    var readyMsg = wsT('webshell.aiSystemReadyMessage') || '系统已就绪。请输入您的测试需求，系统将自动执行相应的安全测试。';
                    aiMessages.innerHTML = '';
                    var readyDiv = document.createElement('div');
                    readyDiv.className = 'webshell-ai-msg assistant';
                    readyDiv.textContent = readyMsg;
                    aiMessages.appendChild(readyDiv);
                }
            }

            var pathInput = document.getElementById('webshell-file-path');
            var fileListEl = document.getElementById('webshell-file-list');
            if (fileListEl && webshellCurrentConn && pathInput) {
                webshellFileListDir(webshellCurrentConn, pathInput.value.trim() || '.');
            }

            // 连接搜索占位符（动态属性：这里手动更新）
            var connSearchEl = document.getElementById('webshell-conn-search');
            if (connSearchEl) {
                var ph = wsT('webshell.searchPlaceholder') || '搜索连接...';
                connSearchEl.setAttribute('placeholder', ph);
                connSearchEl.placeholder = ph;
            }
        }
    }

    var modal = document.getElementById('webshell-modal');
    if (modal && isAppModalOpen('webshell-modal')) {
        var titleEl = document.getElementById('webshell-modal-title');
        var editIdEl = document.getElementById('webshell-edit-id');
        if (titleEl) {
            titleEl.textContent = (editIdEl && editIdEl.value) ? wsT('webshell.editConnectionTitle') : wsT('webshell.addConnection');
        }
        if (typeof window.applyTranslations === 'function') {
            window.applyTranslations(modal);
        }
    }
}

document.addEventListener('languagechange', function () {
    refreshWebshellUIOnLanguageChange();
});

// 任意入口删除对话后同步：若当前在 WebShell AI 助手且已选连接，则刷新对话列表（与 Chat 侧边栏删除保持一致）
document.addEventListener('conversation-deleted', function (e) {
    var id = e.detail && e.detail.conversationId;
    if (!id || !currentWebshellId || !webshellCurrentConn) return;
    var listEl = document.getElementById('webshell-ai-conv-list');
    if (listEl) fetchAndRenderWebshellAiConvList(webshellCurrentConn, listEl);
    if (webshellAiConvMap[webshellCurrentConn.id] === id) {
        delete webshellAiConvMap[webshellCurrentConn.id];
        var msgs = document.getElementById('webshell-ai-messages');
        if (msgs) msgs.innerHTML = '';
    }
});

// 测试连通性（不保存，仅用当前表单参数请求 Shell 执行一次性探活命令）
function testWebshellConnection() {
    var url = (document.getElementById('webshell-url') || {}).value;
    if (url && typeof url.trim === 'function') url = url.trim();
    if (!url) {
        alert(wsTOr('webshell.urlRequired', '请填写 Shell 地址'));
        return;
    }
    var password = (document.getElementById('webshell-password') || {}).value;
    if (password && typeof password.trim === 'function') password = password.trim(); else password = '';
    var type = (document.getElementById('webshell-type') || {}).value || 'php';
    var method = ((document.getElementById('webshell-method') || {}).value || 'post').toLowerCase();
    var cmdParam = (document.getElementById('webshell-cmd-param') || {}).value;
    if (cmdParam && typeof cmdParam.trim === 'function') cmdParam = cmdParam.trim(); else cmdParam = '';
    var osTag = normalizeWebshellOS((document.getElementById('webshell-os') || {}).value);
    var encoding = normalizeWebshellEncoding((document.getElementById('webshell-encoding') || {}).value);
    var btn = document.getElementById('webshell-test-btn');
    var probeToken = buildWebshellProbeToken();
    if (btn) { btn.disabled = true; btn.textContent = (typeof wsT === 'function' ? wsT('common.refresh') : '刷新') + '...'; }
    if (typeof apiFetch === 'undefined') {
        if (btn) { btn.disabled = false; btn.textContent = wsT('webshell.testConnectivity'); }
        alert(wsT('webshell.testFailed') || '连通性测试失败');
        return;
    }
    // 连通性使用 Windows/Linux 都识别的最小内建命令作为探测（echo token 在 cmd 和 sh 下行为等价）
    apiFetch('/api/webshell/exec', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            url: url,
            password: password || '',
            type: type,
            method: method === 'get' ? 'get' : 'post',
            cmd_param: cmdParam || '',
            encoding: encoding,
            os: osTag,
            command: buildWebshellProbeCommand(probeToken)
        })
    })
        .then(function (r) { return r.json(); })
        .then(function (data) {
            if (btn) { btn.disabled = false; btn.textContent = wsT('webshell.testConnectivity'); }
            if (!data) {
                alert(wsT('webshell.testFailed') || '连通性测试失败');
                return;
            }
            // 仅 HTTP 200 不算通过，需校验响应中是否包含本次一次性探活 token
            var output = (data.output != null) ? String(data.output) : '';
            var reallyOk = data.ok && isWebshellProbeOutputMatched(output, probeToken);
            if (reallyOk) {
                alert(wsT('webshell.testSuccess') || '连通性正常，Shell 可访问');
            } else {
                var msg;
                if (data.ok && !isWebshellProbeOutputMatched(output, probeToken))
                    msg = wsT('webshell.testNoExpectedOutput') || 'Shell 返回了响应但未得到预期输出，请检查连接密码与命令参数名';
                else
                    msg = (data.error) ? data.error : (wsT('webshell.testFailed') || '连通性测试失败');
                if (data.http_code) msg += ' (HTTP ' + data.http_code + ')';
                alert(msg);
            }
        })
        .catch(function (e) {
            if (btn) { btn.disabled = false; btn.textContent = wsT('webshell.testConnectivity'); }
            alert((wsT('webshell.testFailed') || '连通性测试失败') + ': ' + (e && e.message ? e.message : String(e)));
        });
}

// 保存连接（新建或更新，请求服务端写入 SQLite 后刷新列表）
function saveWebshellConnection() {
    if (typeof requirePermission === 'function' && !requirePermission('webshell:write')) return;
    var url = (document.getElementById('webshell-url') || {}).value;
    if (url && typeof url.trim === 'function') url = url.trim();
    if (!url) {
        alert(wsTOr('webshell.urlRequired', '请填写 Shell 地址'));
        return;
    }
    var password = (document.getElementById('webshell-password') || {}).value;
    if (password && typeof password.trim === 'function') password = password.trim(); else password = '';
    var type = (document.getElementById('webshell-type') || {}).value || 'php';
    var method = ((document.getElementById('webshell-method') || {}).value || 'post').toLowerCase();
    var cmdParam = (document.getElementById('webshell-cmd-param') || {}).value;
    if (cmdParam && typeof cmdParam.trim === 'function') cmdParam = cmdParam.trim(); else cmdParam = '';
    var osTag = normalizeWebshellOS((document.getElementById('webshell-os') || {}).value);
    var encoding = normalizeWebshellEncoding((document.getElementById('webshell-encoding') || {}).value);
    var remark = (document.getElementById('webshell-remark') || {}).value;
    if (remark && typeof remark.trim === 'function') remark = remark.trim(); else remark = '';

    var editIdEl = document.getElementById('webshell-edit-id');
    var editId = editIdEl ? editIdEl.value.trim() : '';
    var projectId = (document.getElementById('webshell-project-id') || {}).value || '';
    if (projectId && typeof projectId.trim === 'function') projectId = projectId.trim(); else projectId = '';
    var body = { url: url, password: password, type: type, method: method === 'get' ? 'get' : 'post', cmd_param: cmdParam, encoding: encoding, os: osTag, remark: remark || url, project_id: projectId };
    if (typeof apiFetch === 'undefined') return;

    var reqUrl = editId ? ('/api/webshell/connections/' + encodeURIComponent(editId)) : '/api/webshell/connections';
    var reqMethod = editId ? 'PUT' : 'POST';
    apiFetch(reqUrl, {
        method: reqMethod,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
    })
        .then(function (r) { return r.json(); })
        .then(function () {
            closeWebshellModal();
            return refreshWebshellConnectionsFromServer();
        })
        .then(function (list) {
            // 若编辑的是当前选中的连接，同步更新 webshellCurrentConn，使终端/文件管理立即使用新配置
            if (editId && currentWebshellId === editId && Array.isArray(list)) {
                var updated = list.find(function (c) { return c.id === editId; });
                if (updated) webshellCurrentConn = updated;
            }
        })
        .catch(function (e) {
            console.warn('保存 WebShell 连接失败', e);
            alert(e && e.message ? e.message : wsTOr('webshell.saveFailed', '保存失败'));
        });
}
