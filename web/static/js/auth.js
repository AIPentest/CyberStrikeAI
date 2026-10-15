const AUTH_STORAGE_KEY = 'cyberstrike-auth';
let authToken = null;
let authTokenExpiry = null;
let authUser = null;
let authRoles = [];
let authPermissions = new Set();
let authScope = '';
let authPromise = null;
let authPromiseResolvers = [];
let isAppInitialized = false;
let robotBindingCountdownTimer = null;
let robotBindingExpiresAt = 0;
let robotBindingLifetimeMs = 5 * 60 * 1000;
let activeRobotBindingCode = '';

function isTokenValid() {
    return !!authToken && authTokenExpiry instanceof Date && authTokenExpiry.getTime() > Date.now();
}

function saveAuth(token, expiresAt, meta = {}) {
    const expiry = expiresAt instanceof Date ? expiresAt : new Date(expiresAt);
    authToken = token;
    authTokenExpiry = expiry;
    authUser = meta.user || null;
    authRoles = Array.isArray(meta.roles) ? meta.roles : [];
    authPermissions = new Set(Array.isArray(meta.permissions) ? meta.permissions : []);
    authScope = meta.scope || '';
    try {
        localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify({
            token,
            expiresAt: expiry.toISOString(),
            user: authUser,
            roles: authRoles,
            permissions: Array.from(authPermissions),
            scope: authScope,
        }));
    } catch (error) {
        console.warn('无法持久化认证信息:', error);
    }
    renderUserMenuProfile();
}

function clearAuthStorage() {
    authToken = null;
    authTokenExpiry = null;
    authUser = null;
    authRoles = [];
    authPermissions = new Set();
    authScope = '';
    applyRBACToUI();
    try {
        localStorage.removeItem(AUTH_STORAGE_KEY);
    } catch (error) {
        console.warn('无法清除认证信息:', error);
    }
    renderUserMenuProfile();
}

function loadAuthFromStorage() {
    try {
        const raw = localStorage.getItem(AUTH_STORAGE_KEY);
        if (!raw) {
            return false;
        }
        const stored = JSON.parse(raw);
        if (!stored.token || !stored.expiresAt) {
            clearAuthStorage();
            return false;
        }
        const expiry = new Date(stored.expiresAt);
        if (Number.isNaN(expiry.getTime())) {
            clearAuthStorage();
            return false;
        }
        authToken = stored.token;
        authTokenExpiry = expiry;
        authUser = stored.user || null;
        authRoles = Array.isArray(stored.roles) ? stored.roles : [];
        authPermissions = new Set(Array.isArray(stored.permissions) ? stored.permissions : []);
        authScope = stored.scope || '';
        return isTokenValid();
    } catch (error) {
        console.error('读取认证信息失败:', error);
        clearAuthStorage();
        return false;
    }
}

function resolveAuthPromises(success) {
    authPromiseResolvers.forEach(resolve => resolve(success));
    authPromiseResolvers = [];
    authPromise = null;
}

function setLoginPasswordVisible(visible) {
    const input = document.getElementById('login-password');
    const button = document.getElementById('login-password-toggle');
    if (input) input.type = visible ? 'text' : 'password';
    if (button) {
        button.setAttribute('aria-pressed', String(visible));
        const key = visible ? 'login.hidePassword' : 'login.showPassword';
        button.setAttribute('data-i18n', key);
        const label = typeof window.t === 'function' ? window.t(key) : (visible ? '隐藏密码' : '显示密码');
        button.setAttribute('aria-label', label);
        button.setAttribute('title', label);
    }
}

function toggleLoginPasswordVisibility() {
    const input = document.getElementById('login-password');
    setLoginPasswordVisible(input && input.type === 'password');
}

function showLoginOverlay(message = '') {
    setLoginPasswordVisible(false);
    const overlay = document.getElementById('login-overlay');
    const errorBox = document.getElementById('login-error');
    const usernameInput = document.getElementById('login-username');
    const passwordInput = document.getElementById('login-password');
    if (!overlay) {
        return;
    }
    openAppModal('login-overlay', { focus: false });
    if (errorBox) {
        if (message) {
            errorBox.textContent = message;
            errorBox.style.display = 'block';
        } else {
            errorBox.textContent = '';
            errorBox.style.display = 'none';
        }
    }
    setTimeout(function () {
        if (usernameInput && !usernameInput.value) {
            usernameInput.focus();
        } else if (passwordInput) {
            passwordInput.focus();
        }
    }, 100);
}

function hideLoginOverlay() {
    setLoginPasswordVisible(false);
    const overlay = document.getElementById('login-overlay');
    const errorBox = document.getElementById('login-error');
    const usernameInput = document.getElementById('login-username');
    const passwordInput = document.getElementById('login-password');
    closeAppModal('login-overlay');
    if (errorBox) {
        errorBox.textContent = '';
        errorBox.style.display = 'none';
    }
    if (passwordInput) {
        passwordInput.value = '';
        passwordInput.setAttribute('aria-invalid', 'false');
    }
    if (usernameInput) usernameInput.setAttribute('aria-invalid', 'false');
    if (usernameInput && !authUser) {
        usernameInput.value = '';
    }
}

function ensureAuthPromise() {
    if (!authPromise) {
        authPromise = new Promise(resolve => {
            authPromiseResolvers.push(resolve);
        });
    }
    return authPromise;
}

async function ensureAuthenticated() {
    if (isTokenValid()) {
        return true;
    }
    showLoginOverlay();
    await ensureAuthPromise();
    return true;
}

function handleUnauthorized({ message = null, silent = false } = {}) {
    clearAuthStorage();
    authPromise = null;
    authPromiseResolvers = [];
    let finalMessage = message;
    if (!finalMessage) {
        if (typeof window !== 'undefined' && typeof window.t === 'function') {
            finalMessage = window.t('auth.sessionExpired');
        } else {
            finalMessage = '认证已过期，请重新登录';
        }
    }
    if (!silent) {
        showLoginOverlay(finalMessage);
    } else {
        showLoginOverlay();
    }
    return false;
}

async function apiFetch(url, options = {}) {
    await ensureAuthenticated();
    const opts = { ...options };
    const headers = new Headers(options && options.headers ? options.headers : undefined);
    if (authToken && !headers.has('Authorization')) {
        headers.set('Authorization', `Bearer ${authToken}`);
    }
    opts.headers = headers;

    const response = await fetch(url, opts);
    if (response.status === 401) {
        handleUnauthorized();
        const msg = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('auth.unauthorized')
            : '未授权访问';
        throw new Error(msg);
    }
    // 403 属于可预期的 RBAC 拒绝，返回 Response 供调用方通过 res.ok / ensureApiOk 处理。
    return response;
}

/**
 * multipart POST with XMLHttpRequest so upload progress is available (fetch 无法可靠上报进度).
 * 返回与 fetch 类似的对象：ok、status、json()、text()
 */
async function apiUploadWithProgress(url, formData, options = {}) {
    await ensureAuthenticated();
    const onProgress = typeof options.onProgress === 'function' ? options.onProgress : null;
    return new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open('POST', url);
        if (authToken) {
            xhr.setRequestHeader('Authorization', `Bearer ${authToken}`);
        }
        xhr.upload.onprogress = (e) => {
            if (!onProgress || !e.lengthComputable) return;
            const percent = e.total > 0 ? Math.round((e.loaded / e.total) * 100) : 0;
            onProgress({ loaded: e.loaded, total: e.total, percent });
        };
        xhr.onerror = () => {
            reject(new Error('Network error'));
        };
        xhr.onload = () => {
            if (xhr.status === 401) {
                handleUnauthorized();
                const msg = (typeof window !== 'undefined' && typeof window.t === 'function')
                    ? window.t('auth.unauthorized')
                    : '未授权访问';
                reject(new Error(msg));
                return;
            }
            const responseText = xhr.responseText || '';
            resolve({
                ok: xhr.status >= 200 && xhr.status < 300,
                status: xhr.status,
                text: async () => responseText,
                json: async () => {
                    try {
                        return responseText ? JSON.parse(responseText) : {};
                    } catch (err) {
                        throw err;
                    }
                },
            });
        };
        xhr.send(formData);
    });
}

function clearLoginFormError(event) {
    const input = event.target;
    if (!input || (input.id !== 'login-username' && input.id !== 'login-password')) return;
    input.setAttribute('aria-invalid', 'false');
    const error = document.getElementById('login-error');
    if (error) { error.textContent = ''; error.style.display = 'none'; }
}

async function submitLogin(event) {
    event.preventDefault();
    const usernameInput = document.getElementById('login-username');
    const passwordInput = document.getElementById('login-password');
    const errorBox = document.getElementById('login-error');
    const submitBtn = document.querySelector('.login-submit');

    [usernameInput, passwordInput].forEach(input => input && input.setAttribute('aria-invalid', 'false'));

    if (!passwordInput) {
        return;
    }

    const username = usernameInput ? usernameInput.value.trim() : '';
    const password = passwordInput.value.trim();
    if (!username || Array.from(username).length > 64) {
        if (usernameInput) usernameInput.setAttribute('aria-invalid', 'true');
        if (errorBox) {
            errorBox.textContent = typeof window.t === 'function' ? window.t('auth.usernameRequired') : '用户名不能为空，且不能超过 64 个字符';
            errorBox.style.display = 'block';
        }
        if (usernameInput) usernameInput.focus();
        return;
    }
    if (!password) {
        passwordInput.setAttribute('aria-invalid', 'true');
        if (errorBox) {
            const msgEmpty = (typeof window !== 'undefined' && typeof window.t === 'function')
                ? window.t('auth.enterPassword')
                : '请输入密码';
            errorBox.textContent = msgEmpty;
            errorBox.style.display = 'block';
        }
        passwordInput.focus();
        return;
    }

    if (submitBtn) {
        submitBtn.disabled = true;
    }

    try {
        const response = await fetch('/api/auth/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ username, password }),
        });
        const result = await response.json().catch(() => ({}));
        if (!response.ok || !result.token) {
            if (errorBox) {
                const fallback = (typeof window !== 'undefined' && typeof window.t === 'function')
                    ? window.t('auth.loginFailedCheck')
                    : '登录失败，请检查密码';
                errorBox.textContent = result.error || fallback;
                errorBox.style.display = 'block';
            }
            return;
        }

        saveAuth(result.token, result.expires_at, {
            user: result.user,
            roles: result.roles,
            permissions: result.permissions,
            scope: result.scope,
        });
        hideLoginOverlay();
        applyRBACToUI();
        resolveAuthPromises(true);
        if (!isAppInitialized) {
            await bootstrapApp();
        } else {
            await refreshAppData();
        }
    } catch (error) {
        console.error('登录失败:', error);
        if (errorBox) {
            const fallback = (typeof window !== 'undefined' && typeof window.t === 'function')
                ? window.t('auth.loginFailedRetry')
                : '登录失败，请稍后重试';
            errorBox.textContent = fallback;
            errorBox.style.display = 'block';
        }
    } finally {
        if (submitBtn) {
            submitBtn.disabled = false;
        }
    }
}

async function refreshAppData(showTaskErrors = false) {
    if (typeof initChatAgentModeFromConfig === 'function') {
        try {
            await initChatAgentModeFromConfig();
        } catch (error) {
            console.warn('刷新对话模式配置失败:', error);
        }
    }
    await Promise.allSettled([
        loadConversations(),
        loadActiveTasks(showTaskErrors),
    ]);
    // 未登录首屏的项目侧栏可能先收到 401 并显示失败；认证完成后必须主动重试。
    // 放在对话/任务刷新之后，确保最终渲染一定使用有效登录态且不会被早期失败覆盖。
    if (typeof window.refreshChatProjectSelector === 'function') {
        try {
            await window.refreshChatProjectSelector({ reloadFolders: true });
        } catch (error) {
            console.warn('刷新项目侧栏失败:', error);
        }
    }
}

async function bootstrapApp() {
    if (!isAppInitialized) {
        // 等待 i18n 首包加载完成后再插系统就绪消息，避免清除缓存后语言显示 English 气泡仍是中文
        try {
            if (window.i18nReady && typeof window.i18nReady.then === 'function') {
                await window.i18nReady;
            }
        } catch (e) {
            console.warn('等待 i18n 就绪失败，继续初始化聊天', e);
        }
        initializeChatUI();
        isAppInitialized = true;
    }
    applyRBACToUI();
    if (typeof installWriteHandlerGuards === 'function') {
        installWriteHandlerGuards();
    }
    await refreshAppData();
}

const PAGE_PERMISSION_MAP = {
    dashboard: 'dashboard:read',
    chat: 'chat:read',
    hitl: 'hitl:read',
    'tool-guard': 'config:read',
    'info-collect': 'fofa:execute',
    assets: 'asset:read',
    'asset-overview': 'asset:read',
    'asset-library': 'asset:read',
    tasks: 'tasks:read',
    workflows: 'workflow:read',
    projects: 'project:read',
    vulnerabilities: 'vulnerability:read',
    'chat-files': 'files:read',
    webshell: 'webshell:read',
    c2: 'c2:read',
    'c2-listeners': 'c2:read',
    'c2-sessions': 'c2:read',
    'c2-tasks': 'c2:read',
    'c2-payloads': 'c2:read',
    'c2-events': 'c2:read',
    'c2-profiles': 'c2:read',
    mcp: 'mcp:read',
    'mcp-monitor': 'monitor:read',
    'mcp-management': 'mcp:read',
    knowledge: 'knowledge:read',
    'knowledge-retrieval-logs': 'knowledge:read',
    'knowledge-management': 'knowledge:read',
    skills: 'skills:read',
    'skills-monitor': 'skills:read',
    'skills-management': 'skills:read',
    agents: 'agents:read',
    'agents-management': 'agents:read',
    roles: 'roles:read',
    'roles-management': 'roles:read',
    'platform-rbac': 'rbac:read',
    settings: 'config:read',
};

function hasPermission(permission) {
    return !permission || authPermissions.has(permission);
}

function hasAnyPermission(permissions) {
    if (!Array.isArray(permissions) || !permissions.length) return true;
    return permissions.some((permission) => hasPermission(permission));
}

async function readApiError(response, fallback) {
    if (!response) {
        return fallback || authT('auth.requestFailed', '请求失败');
    }
    try {
        const body = await response.clone().json();
        return body.error || body.message || fallback || authT('auth.requestFailed', '请求失败');
    } catch (error) {
        return fallback || authT('auth.requestFailed', '请求失败');
    }
}

function notifyApiError(message, type = 'error') {
    const text = (message || '').trim() || authT('auth.requestFailed', '请求失败');
    if (typeof showNotification === 'function') {
        showNotification(text, type);
        return;
    }
    if (typeof showToast === 'function') {
        showToast(text, type);
        return;
    }
    alert(text);
}

async function notifyApiResponseError(response, fallback) {
    notifyApiError(await readApiError(response, fallback));
}

async function ensureApiOk(response, fallback) {
    if (response && response.ok) return true;
    await notifyApiResponseError(response, fallback);
    return false;
}

function requirePermission(permission, customMessage) {
    const allowed = Array.isArray(permission)
        ? hasAnyPermission(permission)
        : hasPermission(permission);
    if (allowed) return true;
    notifyApiError(customMessage || authT('auth.forbidden', '权限不足'));
    return false;
}

function permissionAllowedForElement(el) {
    if (!el) return true;
    const anyOf = el.getAttribute('data-require-permission-any');
    const permission = el.getAttribute('data-require-permission');
    if (anyOf) {
        return hasAnyPermission(anyOf.split(/[\s,|]+/).map((item) => item.trim()).filter(Boolean));
    }
    if (permission) {
        return hasPermission(permission);
    }
    return true;
}

function applyPermissionElement(el) {
    const anyOf = el.getAttribute('data-require-permission-any');
    const permission = el.getAttribute('data-require-permission');
    if (!anyOf && !permission) return;
    const allowed = permissionAllowedForElement(el);
    el.hidden = !allowed;
    el.classList.toggle('rbac-permission-denied', !allowed);
    // Permission decides whether this user may ever use the control; it does not decide
    // whether the control is ready. A page that disables one for a reason of its own marks
    // it with data-state-disabled, because writing el.disabled = !allowed here otherwise
    // re-enables it for exactly the users who are allowed - and a button whose preconditions
    // are unmet is then pressed, and fails.
    const blockedByState = el.dataset && el.dataset.stateDisabled === 'true';
    const disabled = !allowed || blockedByState;
    if ('disabled' in el) {
        el.disabled = disabled;
    }
    el.setAttribute('aria-hidden', allowed ? 'false' : 'true');
    el.setAttribute('aria-disabled', disabled ? 'true' : 'false');
}

let permissionClickGuardInstalled = false;

function installPermissionClickGuard() {
    if (permissionClickGuardInstalled) return;
    permissionClickGuardInstalled = true;
    document.addEventListener('click', (event) => {
        const target = event.target instanceof Element
            ? event.target.closest('[data-require-permission], [data-require-permission-any]')
            : null;
        if (!target || permissionAllowedForElement(target)) return;
        event.preventDefault();
        event.stopPropagation();
        if (typeof event.stopImmediatePropagation === 'function') {
            event.stopImmediatePropagation();
        }
        notifyApiError(authT('auth.forbidden', '权限不足'));
    }, true);
}

function applyRBACToUI(root) {
    installPermissionClickGuard();
    document.querySelectorAll('[data-page]').forEach((el) => {
        // Navigation permissions must also be refreshed during scoped renders.
        // Explicit rules take precedence over the fallback page permission map.
        if (el.hasAttribute('data-require-permission') || el.hasAttribute('data-require-permission-any')) {
            applyPermissionElement(el);
            return;
        }
        const page = el.getAttribute('data-page');
        const permission = PAGE_PERMISSION_MAP[page];
        if (!permission) return;
        const allowed = hasPermission(permission);
        el.hidden = !allowed;
        el.setAttribute('aria-hidden', allowed ? 'false' : 'true');
    });
    const permissionRoot = root instanceof Element ? root : document;
    permissionRoot.querySelectorAll('[data-require-permission], [data-require-permission-any]').forEach(applyPermissionElement);
    if (permissionRoot instanceof Element && permissionRoot.matches('[data-require-permission], [data-require-permission-any]')) {
        applyPermissionElement(permissionRoot);
    }
    const userAvatar = document.querySelector('.user-avatar-btn');
    if (userAvatar && authUser && authUser.username) {
        const displayName = getAuthDisplayName();
        userAvatar.setAttribute('title', displayName);
        userAvatar.setAttribute('aria-label', authT('header.userMenuFor', '用户菜单：{{name}}', { name: displayName }));
    }
    renderUserMenuProfile();
}

function authT(key, fallback, opts = {}) {
    if (typeof window !== 'undefined' && typeof window.t === 'function') {
        const translated = window.t(key, opts);
        if (translated && translated !== key) {
            return translated;
        }
    }
    return fallback.replace(/\{\{\s*(\w+)\s*\}\}/g, function (_, name) {
        return Object.prototype.hasOwnProperty.call(opts, name) ? String(opts[name]) : '';
    });
}

function getAuthDisplayName() {
    if (!authUser) {
        return authT('header.unknownUser', '未知用户');
    }
    return String(authUser.display_name || authUser.displayName || authUser.username || '').trim()
        || authT('header.unknownUser', '未知用户');
}

function getAuthUsername() {
    if (!authUser) {
        return '-';
    }
    const username = String(authUser.username || '').trim();
    return username ? `@${username}` : '-';
}

function getScopeLabel(scope) {
    const normalized = String(scope || '').trim().toLowerCase();
    const keyMap = {
        all: 'header.scopeAll',
        assigned: 'header.scopeAssigned',
        own: 'header.scopeOwn',
    };
    const fallbackMap = {
        all: '全部资源',
        assigned: '指定资源',
        own: '自己的资源',
    };
    const key = keyMap[normalized] || 'header.scopeUnknown';
    const fallback = fallbackMap[normalized] || '资源范围未知';
    return authT(key, fallback);
}

function renderUserMenuProfile() {
    const displayNameEl = document.getElementById('user-menu-display-name');
    const usernameEl = document.getElementById('user-menu-username');
    const scopeEl = document.getElementById('user-menu-scope');
    const rolesEl = document.getElementById('user-menu-roles');
    const permissionsEl = document.getElementById('user-menu-permissions');
    const avatarBtn = document.getElementById('user-avatar-btn') || document.querySelector('.user-avatar-btn');

    const displayName = getAuthDisplayName();
    const roleCount = Array.isArray(authRoles) ? authRoles.length : 0;
    const permissionCount = authPermissions instanceof Set ? authPermissions.size : 0;

    if (displayNameEl) displayNameEl.textContent = displayName;
    if (usernameEl) usernameEl.textContent = getAuthUsername();
    if (scopeEl) scopeEl.textContent = getScopeLabel(authScope);
    if (rolesEl) rolesEl.textContent = authT('header.rolesCount', '{{count}} 个角色', { count: roleCount });
    if (permissionsEl) permissionsEl.textContent = authT('header.permissionsCount', '{{count}} 项权限', { count: permissionCount });
    if (avatarBtn && authUser) {
        avatarBtn.setAttribute('title', displayName);
        avatarBtn.setAttribute('aria-label', authT('header.userMenuFor', '用户菜单：{{name}}', { name: displayName }));
    } else if (avatarBtn) {
        avatarBtn.setAttribute('aria-label', authT('header.userMenu', '用户菜单'));
    }
}

function setUserMenuOpen(open) {
    const dropdown = document.getElementById('user-menu-dropdown');
    const avatarBtn = document.getElementById('user-avatar-btn') || document.querySelector('.user-avatar-btn');
    if (!dropdown) return;
    dropdown.style.display = open ? 'block' : 'none';
    if (avatarBtn) {
        avatarBtn.classList.toggle('active', open);
        avatarBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
    }
    if (open) {
        renderUserMenuProfile();
    }
}

// 通用工具函数
function getStatusText(status) {
    const s = (status && String(status).toLowerCase()) || '';
    if (typeof window.t !== 'function') {
        const fallback = { pending: '等待中', queued: '排队中', running: '执行中', background_running: '后台执行中', completed: '已完成', failed: '失败', blocked: '已拦截', cancelled: '已终止', hard_timeout: '硬超时', orphaned: '孤儿记录' };
        return fallback[s] || status;
    }
    const keyMap = { pending: 'mcpDetailModal.statusPending', queued: 'mcpDetailModal.statusQueued', running: 'mcpDetailModal.statusRunning', background_running: 'timeline.backgroundRunning', completed: 'mcpDetailModal.statusCompleted', failed: 'mcpDetailModal.statusFailed', blocked: 'mcpMonitor.statusBlocked', cancelled: 'mcpDetailModal.statusCancelled', hard_timeout: 'mcpMonitor.statusHardTimeout', orphaned: 'mcpMonitor.statusOrphaned' };
    const key = keyMap[s];
    return key ? window.t(key) : status;
}

function formatDuration(ms) {
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    
    if (hours > 0) {
        return `${hours}小时${minutes % 60}分钟`;
    } else if (minutes > 0) {
        return `${minutes}分钟${seconds % 60}秒`;
    } else {
        return `${seconds}秒`;
    }
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function escapeJsString(text) {
    return JSON.stringify(String(text == null ? '' : text));
}

function escapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function escapeJsStringAttr(text) {
    return escapeAttr(escapeJsString(text));
}

/** @param {string} text @param {{ profile?: 'chat'|'timeline' }} [options] */
function formatMarkdown(text, options) {
    if (typeof window.csMarkdownSanitize !== 'undefined') {
        return window.csMarkdownSanitize.formatMarkdownToHtml(text, options);
    }
    const raw = text == null ? '' : String(text);
    return escapeHtml(raw).replace(/\n/g, '<br>');
}

async function setupOIDCLoginUI() {
    try {
        const response = await fetch('/api/auth/oidc/config', {credentials: 'same-origin'});
        if (!response.ok) return;
        const oidcConfig = await response.json();
        if (!oidcConfig.enabled) return;
        const form = document.getElementById('login-form');
        if (!form || document.getElementById('oidc-login-button')) return;
        const button = document.createElement('button');
        button.id = 'oidc-login-button';
        button.type = 'button';
        button.textContent = (typeof oidcConfig.login_label === 'string' && oidcConfig.login_label.trim())
            ? oidcConfig.login_label.trim() : 'Sign in with SSO';
        button.className = 'login-oidc-button';
        button.addEventListener('click', () => { window.location.href = '/api/auth/oidc/login'; });
        form.appendChild(button);
    } catch (error) { console.warn('OIDC config unavailable', error); }
}
function setupLoginUI() {
    setupOIDCLoginUI();
    installPermissionClickGuard();
    const loginForm = document.getElementById('login-form');
    if (loginForm) {
        loginForm.addEventListener('submit', submitLogin);
        loginForm.addEventListener('input', clearLoginFormError);
    }
}

async function initializeApp() {
    setupLoginUI();
    const hasStoredAuth = loadAuthFromStorage();
    if (hasStoredAuth && isTokenValid()) {
        try {
            const response = await apiFetch('/api/auth/validate', {
                method: 'GET',
            });
            if (response.ok) {
                const result = await response.json().catch(() => ({}));
                saveAuth(result.token || authToken, result.expires_at || authTokenExpiry, {
                    user: result.user || authUser,
                    roles: result.roles || authRoles,
                    permissions: result.permissions || Array.from(authPermissions),
                    scope: result.scope || authScope,
                });
                hideLoginOverlay();
                applyRBACToUI();
                resolveAuthPromises(true);
                await bootstrapApp();
                return;
            }
        } catch (error) {
            console.warn('本地会话已失效，需重新登录');
        }
    }

    // OIDC callback supplies a secure HttpOnly cookie, validated by the server.
    try {
        const response = await fetch('/api/auth/validate', {credentials: 'same-origin'});
        if (response.ok) {
            const result = await response.json();
            if (result.token) {
                saveAuth(result.token, result.expires_at, result);
                hideLoginOverlay();
                applyRBACToUI();
                resolveAuthPromises(true);
                await bootstrapApp();
                return;
            }
        }
    } catch (error) { console.warn('OIDC session validation failed', error); }
    clearAuthStorage();
    showLoginOverlay();
}

// 用户菜单控制
function toggleUserMenu() {
    const dropdown = document.getElementById('user-menu-dropdown');
    if (!dropdown) return;
    
    const isVisible = dropdown.style.display !== 'none';
    setUserMenuOpen(!isVisible);
}

// 点击页面其他地方时关闭下拉菜单
document.addEventListener('click', function(event) {
    const dropdown = document.getElementById('user-menu-dropdown');
    const avatarBtn = document.querySelector('.user-avatar-btn');
    
    if (dropdown && avatarBtn && 
        !dropdown.contains(event.target) && 
        !avatarBtn.contains(event.target)) {
        setUserMenuOpen(false);
    }
});

document.addEventListener('languagechange', function () {
    renderUserMenuProfile();
});

async function openRobotAccountBinding() {
    setUserMenuOpen(false);
    openAppModal('robot-account-binding-modal');
    if (robotBindingExpiresAt) updateRobotBindingCountdown();
    await loadRobotAccountBindings();
}

function closeRobotAccountBinding() {
    closeAppModal('robot-account-binding-modal');
	if (typeof window.loadVulnerabilityAlertSubscription === 'function') {
		window.loadVulnerabilityAlertSubscription();
	}
}

async function generateRobotBindingCode() {
    const generateBtn = document.getElementById('robot-binding-generate-btn');
    if (generateBtn) {
        generateBtn.disabled = true;
        generateBtn.textContent = '正在生成…';
    }
    let response;
    let data;
    try {
        response = await apiFetch('/api/auth/robot-binding-code', { method: 'POST' });
        data = await response.json().catch(() => ({}));
    } catch (error) {
        if (typeof showNotification === 'function') showNotification('生成绑定码失败，请检查网络连接', 'error');
        resetRobotBindingGenerateButton();
        return;
    }
    if (!response.ok || !data.code) {
        if (typeof showNotification === 'function') showNotification(data.error || '生成绑定码失败', 'error');
        resetRobotBindingGenerateButton();
        return;
    }
    const codeEl = document.getElementById('robot-binding-code');
    const copyBtn = document.getElementById('robot-binding-copy-btn');
    const card = document.getElementById('robot-binding-code-card');
    const timer = document.getElementById('robot-binding-timer');
    const state = document.getElementById('robot-binding-code-state');
    activeRobotBindingCode = data.code;
    robotBindingLifetimeMs = Math.max(1000, Number(data.expires_in_seconds || 300) * 1000);
    // Use the server-provided duration instead of comparing wall clocks, so a
    // client machine with clock skew still gets an accurate countdown.
    robotBindingExpiresAt = Date.now() + robotBindingLifetimeMs;
    if (codeEl) codeEl.textContent = data.code;
    if (copyBtn) copyBtn.disabled = false;
    if (card) card.className = 'robot-binding-code-card is-active';
    if (timer) timer.hidden = false;
    if (state) state.textContent = '等待绑定';
    if (generateBtn) {
        generateBtn.disabled = false;
        generateBtn.textContent = '重新生成';
    }
    startRobotBindingCountdown();
}

function resetRobotBindingGenerateButton() {
    const generateBtn = document.getElementById('robot-binding-generate-btn');
    if (generateBtn) {
        generateBtn.disabled = false;
        generateBtn.textContent = activeRobotBindingCode ? '重新生成' : '生成绑定码';
    }
}

function startRobotBindingCountdown() {
    if (robotBindingCountdownTimer) clearInterval(robotBindingCountdownTimer);
    updateRobotBindingCountdown();
    robotBindingCountdownTimer = setInterval(updateRobotBindingCountdown, 250);
}

function updateRobotBindingCountdown() {
    if (!robotBindingExpiresAt) return;
    const remaining = Math.max(0, robotBindingExpiresAt - Date.now());
    const seconds = Math.ceil(remaining / 1000);
    const minutesPart = String(Math.floor(seconds / 60)).padStart(2, '0');
    const secondsPart = String(seconds % 60).padStart(2, '0');
    const countdown = document.getElementById('robot-binding-countdown');
    const progress = document.getElementById('robot-binding-progress-bar');
    const expiry = document.getElementById('robot-binding-expiry');
    if (countdown) countdown.textContent = `${minutesPart}:${secondsPart}`;
    if (progress) progress.style.width = `${Math.max(0, Math.min(100, remaining / robotBindingLifetimeMs * 100))}%`;
    if (expiry && activeRobotBindingCode) expiry.textContent = `请发送：绑定 ${activeRobotBindingCode}`;
    if (remaining <= 0) expireRobotBindingCode();
}

function expireRobotBindingCode() {
    if (robotBindingCountdownTimer) clearInterval(robotBindingCountdownTimer);
    robotBindingCountdownTimer = null;
    robotBindingExpiresAt = 0;
    activeRobotBindingCode = '';
    const card = document.getElementById('robot-binding-code-card');
    const codeEl = document.getElementById('robot-binding-code');
    const state = document.getElementById('robot-binding-code-state');
    const expiry = document.getElementById('robot-binding-expiry');
    const countdown = document.getElementById('robot-binding-countdown');
    const progress = document.getElementById('robot-binding-progress-bar');
    const copyBtn = document.getElementById('robot-binding-copy-btn');
    if (card) card.className = 'robot-binding-code-card is-expired';
    if (codeEl) codeEl.textContent = '已失效';
    if (state) state.textContent = '已过期';
    if (expiry) expiry.textContent = '绑定码已过期，请重新生成';
    if (countdown) countdown.textContent = '00:00';
    if (progress) progress.style.width = '0%';
    if (copyBtn) copyBtn.disabled = true;
    resetRobotBindingGenerateButton();
    const generateBtn = document.getElementById('robot-binding-generate-btn');
    if (generateBtn) generateBtn.textContent = '重新生成';
    loadRobotAccountBindings();
}

async function copyRobotBindingCode() {
    if (!activeRobotBindingCode || !robotBindingExpiresAt || robotBindingExpiresAt <= Date.now()) return;
    const command = `绑定 ${activeRobotBindingCode}`;
    try {
        await navigator.clipboard.writeText(command);
    } catch (_) {
        const textarea = document.createElement('textarea');
        textarea.value = command;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        textarea.remove();
    }
    const copyBtn = document.getElementById('robot-binding-copy-btn');
    const label = copyBtn?.querySelector('span');
    if (label) label.textContent = '已复制';
    setTimeout(() => { if (label) label.textContent = '复制命令'; }, 1400);
    if (typeof showNotification === 'function') showNotification('绑定命令已复制', 'success');
}

async function loadRobotAccountBindings() {
    const list = document.getElementById('robot-account-binding-list');
    if (!list) return;
    const response = await apiFetch('/api/auth/robot-bindings');
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
        list.textContent = data.error || '获取绑定失败';
        return;
    }
    const bindings = Array.isArray(data.bindings) ? data.bindings : [];
    if (!bindings.length) {
        list.innerHTML = `<div class="robot-binding-empty-state">
            <span class="robot-binding-empty-icon"><svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" stroke="currentColor" stroke-width="2"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" stroke="currentColor" stroke-width="2"/></svg></span>
            <div><strong>暂无绑定账号</strong><p>生成绑定码并在机器人中发送，即可完成首次绑定。</p></div>
        </div>`;
        return;
    }
    const platformLabels = { wechat: '微信', wecom: '企业微信', dingtalk: '钉钉', lark: '飞书', telegram: 'Telegram', slack: 'Slack', discord: 'Discord', qq: 'QQ' };
    list.innerHTML = bindings.map(binding => `
        <div class="robot-binding-account-card">
            <span class="robot-binding-platform-icon">${escapeHtml((platformLabels[binding.platform] || binding.platform || '?').slice(0, 1).toUpperCase())}</span>
            <div class="robot-binding-account-main">
                <div class="robot-binding-account-name"><strong>${escapeHtml(platformLabels[binding.platform] || binding.platform || '-')}</strong><span>已连接</span></div>
                <small>账号标识 ${escapeHtml(binding.external_user_hint || '-')} · 更新于 ${escapeHtml(formatRobotBindingTime(binding.updated_at))}</small>
            </div>
            <button type="button" class="btn-secondary btn-small robot-binding-unbind-btn" onclick="deleteRobotAccountBinding(${escapeJsStringAttr(binding.id || '')})">解除绑定</button>
        </div>`).join('');
	if (typeof window.loadVulnerabilityAlertSubscription === 'function') {
		window.loadVulnerabilityAlertSubscription();
	}
}

function formatRobotBindingTime(value) {
    const date = new Date(value || '');
    if (Number.isNaN(date.getTime())) return '未知时间';
    return date.toLocaleString([], { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

async function deleteRobotAccountBinding(id) {
    if (!id || !window.confirm('确定解除该机器人账号绑定吗？')) return;
    const response = await apiFetch(`/api/auth/robot-bindings/${encodeURIComponent(id)}`, { method: 'DELETE' });
    if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        if (typeof showNotification === 'function') showNotification(data.error || '解绑失败', 'error');
        return;
    }
    await loadRobotAccountBindings();
}

// 退出登录
async function logout() {
    // 关闭下拉菜单
    setUserMenuOpen(false);
    
    try {
        // 先尝试调用退出API（如果token有效）
        if (authToken) {
            const headers = new Headers();
            headers.set('Authorization', `Bearer ${authToken}`);
            await fetch('/api/auth/logout', {
                method: 'POST',
                headers: headers,
            }).catch(() => {
                // 忽略错误，继续清除本地认证信息
            });
        }
    } catch (error) {
        console.error('退出登录API调用失败:', error);
    } finally {
        // 无论如何都清除本地认证信息
        clearAuthStorage();
        hideLoginOverlay();
        showLoginOverlay(typeof window.t === 'function' ? window.t('auth.loggedOut') : '已退出登录');
    }
}

// 导出函数供HTML使用
window.toggleUserMenu = toggleUserMenu;
window.openRobotAccountBinding = openRobotAccountBinding;
window.closeRobotAccountBinding = closeRobotAccountBinding;
window.generateRobotBindingCode = generateRobotBindingCode;
window.copyRobotBindingCode = copyRobotBindingCode;
window.deleteRobotAccountBinding = deleteRobotAccountBinding;
window.logout = logout;
window.toggleLoginPasswordVisibility = toggleLoginPasswordVisibility;
window.hasPermission = hasPermission;
window.hasAnyPermission = hasAnyPermission;
window.readApiError = readApiError;
window.notifyApiError = notifyApiError;
window.notifyApiResponseError = notifyApiResponseError;
window.ensureApiOk = ensureApiOk;
window.requirePermission = requirePermission;
window.permissionAllowedForElement = permissionAllowedForElement;
window.applyRBACToUI = applyRBACToUI;

function rbacAfterDynamicRender(root) {
    applyRBACToUI(root);
}

window.rbacAfterDynamicRender = rbacAfterDynamicRender;

document.addEventListener('DOMContentLoaded', initializeApp);
