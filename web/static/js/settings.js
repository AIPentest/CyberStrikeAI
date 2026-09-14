// 设置相关功能
let currentConfig = null;
let selectedAIChannelId = '';
const AI_CHANNEL_PROBE_CONCURRENCY = 2;
const selectedAIChannelBulkIds = new Set();
const aiChannelProbeResults = {};
let allTools = [];
let alwaysVisibleToolNames = new Set();
let alwaysVisibleBuiltinToolNames = new Set();
// 全局工具状态映射，用于保存用户在所有页面的修改
// key: 唯一工具标识符（toolKey），value: { enabled: boolean, is_external: boolean, external_mcp: string }
let toolStateMap = new Map();
let activeRobotEditor = '';
let robotAuthDrafts = {};

function settingsT(key, fallback) {
    if (typeof window.t === 'function') {
        const translated = window.t(key);
        if (translated && translated !== key) return translated;
    }
    return fallback;
}

function settingsEscapeJsString(text) {
    return JSON.stringify(String(text == null ? '' : text));
}

function settingsEscapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function settingsEscapeJsStringAttr(text) {
    return settingsEscapeAttr(settingsEscapeJsString(text));
}

const settingsCustomSelects = new Map();
let settingsCustomSelectsDocBound = false;

function shouldEnhanceSettingsSelect(select) {
    if (!select || select.dataset.settingsCustomSelect === '1') return false;
    if (select.classList.contains('model-pick-native')) return false;
    if (select.id && select.id.indexOf('audit-filter-') === 0) return false;
    if (select.getAttribute('aria-hidden') === 'true') return false;
    if (select.style && select.style.display === 'none') return false;
    return true;
}

function closeSettingsCustomSelect(select) {
    const reg = settingsCustomSelects.get(select);
    if (reg) {
        reg.wrapper.classList.remove('open');
        reg.trigger.setAttribute('aria-expanded', 'false');
        if (reg.menu.parentNode !== reg.wrapper) {
            reg.wrapper.appendChild(reg.menu);
        }
        reg.menu.classList.remove('settings-custom-select-menu--floating');
        reg.menu.style.left = '';
        reg.menu.style.right = '';
        reg.menu.style.top = '';
        reg.menu.style.bottom = '';
        reg.menu.style.width = '';
        reg.menu.style.maxHeight = '';
    }
}

function closeAllSettingsCustomSelects() {
    settingsCustomSelects.forEach((reg) => {
        reg.wrapper.classList.remove('open');
        reg.trigger.setAttribute('aria-expanded', 'false');
        if (reg.menu.parentNode !== reg.wrapper) {
            reg.wrapper.appendChild(reg.menu);
        }
        reg.menu.classList.remove('settings-custom-select-menu--floating');
        reg.menu.style.left = '';
        reg.menu.style.right = '';
        reg.menu.style.top = '';
        reg.menu.style.bottom = '';
        reg.menu.style.width = '';
        reg.menu.style.maxHeight = '';
    });
}

function positionSettingsCustomSelectMenu(reg) {
    if (!reg || !reg.wrapper.classList.contains('open')) return;
    const rect = reg.trigger.getBoundingClientRect();
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 0;
    const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
    const gap = 6;
    const edgePadding = 12;
    const desiredWidth = Math.max(rect.width, 150);
    const width = Math.min(desiredWidth, Math.max(160, viewportWidth - edgePadding * 2));
    const left = Math.min(Math.max(edgePadding, rect.left), Math.max(edgePadding, viewportWidth - width - edgePadding));
    const spaceBelow = viewportHeight - rect.bottom - gap - edgePadding;
    const spaceAbove = rect.top - gap - edgePadding;
    const openAbove = spaceBelow < 180 && spaceAbove > spaceBelow;
    const maxHeight = Math.max(120, Math.floor((openAbove ? spaceAbove : spaceBelow) || 180));

    reg.menu.style.left = `${Math.round(left)}px`;
    reg.menu.style.right = 'auto';
    reg.menu.style.width = `${Math.round(width)}px`;
    reg.menu.style.maxHeight = `${maxHeight}px`;
    if (openAbove) {
        reg.menu.style.top = 'auto';
        reg.menu.style.bottom = `${Math.round(viewportHeight - rect.top + gap)}px`;
    } else {
        reg.menu.style.top = `${Math.round(rect.bottom + gap)}px`;
        reg.menu.style.bottom = 'auto';
    }
}

function openSettingsCustomSelect(select) {
    const reg = settingsCustomSelects.get(select);
    if (!reg || select.disabled) return;
    closeAllSettingsCustomSelects();
    reg.wrapper.classList.add('open');
    reg.trigger.setAttribute('aria-expanded', 'true');
    reg.menu.classList.add('settings-custom-select-menu--floating');
    document.body.appendChild(reg.menu);
    positionSettingsCustomSelectMenu(reg);
}

function repositionOpenSettingsCustomSelects() {
    settingsCustomSelects.forEach((reg) => positionSettingsCustomSelectMenu(reg));
}

function syncSettingsCustomSelect(select) {
    const reg = settingsCustomSelects.get(select);
    if (!reg) return;
    const selected = select.options[select.selectedIndex];
    reg.value.textContent = selected ? selected.textContent : '';
    reg.trigger.disabled = !!select.disabled;
    reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
    reg.menu.innerHTML = '';

    Array.prototype.forEach.call(select.options, (option, index) => {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'settings-custom-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-index', String(index));
        item.setAttribute('aria-selected', option.selected ? 'true' : 'false');
        item.disabled = !!option.disabled;
        item.classList.toggle('is-selected', option.selected);
        item.classList.toggle('is-disabled', !!option.disabled);

        const check = document.createElement('span');
        check.className = 'settings-custom-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';

        const label = document.createElement('span');
        label.className = 'settings-custom-select-label';
        label.textContent = option.textContent;

        item.appendChild(check);
        item.appendChild(label);

        if (select.id === 'ai-channel-select') {
            const probeStatus = option.dataset.probeStatus || '';
            const probeMessage = option.dataset.probeMessage || '';
            if (probeStatus) {
                item.classList.add('settings-custom-select-option--probe', `probe-${probeStatus}`);
                const status = document.createElement('span');
                status.className = `settings-custom-select-status ${probeStatus}`;
                status.innerHTML = `<span class="settings-custom-select-status-dot" aria-hidden="true"></span><span class="settings-custom-select-status-text"></span>`;
                status.querySelector('.settings-custom-select-status-text').textContent = probeMessage || probeStatus;
                item.appendChild(status);
            }
        }
        reg.menu.appendChild(item);
    });
}

function refreshSettingsCustomSelects() {
    settingsCustomSelects.forEach((_reg, select) => syncSettingsCustomSelect(select));
}

function enhanceSettingsSelect(select) {
    if (!shouldEnhanceSettingsSelect(select)) {
        if (select && select.dataset.settingsCustomSelect === '1') {
            syncSettingsCustomSelect(select);
        }
        return;
    }

    select.dataset.settingsCustomSelect = '1';
    select.classList.add('settings-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'settings-custom-select';
    if (select.id && select.id.indexOf('openai-reasoning-') === 0) {
        wrapper.classList.add('settings-custom-select--compact');
    }
    if (select.style.width) wrapper.style.width = select.style.width;
    if (select.style.minWidth) wrapper.style.minWidth = select.style.minWidth;

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'settings-custom-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');

    const value = document.createElement('span');
    value.className = 'settings-custom-select-value';
    const caret = document.createElement('span');
    caret.className = 'settings-custom-select-caret';
    caret.setAttribute('aria-hidden', 'true');
    caret.textContent = '▾';
    trigger.appendChild(value);
    trigger.appendChild(caret);

    const menu = document.createElement('div');
    menu.className = 'settings-custom-select-menu';
    menu.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(menu);
    wrapper.appendChild(select);

    settingsCustomSelects.set(select, { wrapper, trigger, value, menu });

    trigger.addEventListener('click', (event) => {
        event.stopPropagation();
        if (select.disabled) return;
        const willOpen = !wrapper.classList.contains('open');
        if (willOpen) openSettingsCustomSelect(select);
        else closeSettingsCustomSelect(select);
    });

    trigger.addEventListener('keydown', (event) => {
        if (select.disabled) return;
        const enabledOptions = Array.prototype.filter.call(select.options, (option) => !option.disabled);
        if (!enabledOptions.length) return;
        const current = Math.max(0, enabledOptions.indexOf(select.options[select.selectedIndex]));
        let next = current;
        if (event.key === 'ArrowDown') next = Math.min(enabledOptions.length - 1, current + 1);
        else if (event.key === 'ArrowUp') next = Math.max(0, current - 1);
        else if (event.key === 'Home') next = 0;
        else if (event.key === 'End') next = enabledOptions.length - 1;
        else if (event.key === 'Escape') {
            closeSettingsCustomSelect(select);
            return;
        } else if (event.key === 'Enter' || event.key === ' ') {
            openSettingsCustomSelect(select);
            event.preventDefault();
            return;
        } else {
            return;
        }
        event.preventDefault();
        const nextOption = enabledOptions[next];
        if (nextOption && select.value !== nextOption.value) {
            select.value = nextOption.value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        syncSettingsCustomSelect(select);
    });

    menu.addEventListener('click', (event) => {
        const item = event.target.closest('.settings-custom-select-option');
        if (!item || item.disabled) return;
        event.stopPropagation();
        const option = select.options[Number(item.dataset.index)];
        if (option && !option.disabled && select.value !== option.value) {
            select.value = option.value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        syncSettingsCustomSelect(select);
        closeSettingsCustomSelect(select);
    });

    select.addEventListener('change', () => syncSettingsCustomSelect(select));
    syncSettingsCustomSelect(select);
}

function initSettingsCustomSelects(root) {
    const scope = root || document.getElementById('page-settings');
    if (!scope) return;
    scope.querySelectorAll('select').forEach(enhanceSettingsSelect);
    if (!settingsCustomSelectsDocBound) {
        document.addEventListener('click', closeAllSettingsCustomSelects);
        document.addEventListener('keydown', (event) => {
            if (event.key === 'Escape') closeAllSettingsCustomSelects();
        });
        document.addEventListener('scroll', repositionOpenSettingsCustomSelects, true);
        window.addEventListener('resize', repositionOpenSettingsCustomSelects);
        settingsCustomSelectsDocBound = true;
    }
    refreshSettingsCustomSelects();
}

function getRobotStatus(type) {
    const value = (id) => document.getElementById(id)?.value?.trim() || '';
    const checked = (id) => document.getElementById(id)?.checked === true;
    let configured = false;
    let enabled = false;

    if (type === 'wechat') {
        configured = !!value('robot-wechat-ilink-bot-id');
        enabled = checked('robot-wechat-enabled');
    } else if (type === 'wecom') {
        const agentId = parseInt(value('robot-wecom-agent-id'), 10);
        configured = !!(value('robot-wecom-token') && value('robot-wecom-corp-id') && value('robot-wecom-secret') && agentId > 0);
        enabled = checked('robot-wecom-enabled');
    } else if (type === 'dingtalk') {
        configured = !!(value('robot-dingtalk-client-id') && value('robot-dingtalk-client-secret'));
        enabled = checked('robot-dingtalk-enabled');
    } else if (type === 'lark') {
        configured = !!(value('robot-lark-app-id') && value('robot-lark-app-secret'));
        enabled = checked('robot-lark-enabled');
    } else if (type === 'telegram') {
        configured = !!value('robot-telegram-bot-token');
        enabled = checked('robot-telegram-enabled');
    } else if (type === 'slack') {
        configured = !!(value('robot-slack-bot-token') && value('robot-slack-app-token'));
        enabled = checked('robot-slack-enabled');
    } else if (type === 'discord') {
        configured = !!value('robot-discord-bot-token');
        enabled = checked('robot-discord-enabled');
    } else if (type === 'qq') {
        configured = !!(value('robot-qq-app-id') && value('robot-qq-client-secret'));
        enabled = checked('robot-qq-enabled');
    }

    if (enabled) {
        return { state: 'enabled', text: settingsT('settings.robots.statusEnabled', '已启用') };
    }
    if (configured) {
        return { state: 'ready', text: settingsT('settings.robots.statusConfigured', '已配置') };
    }
    return { state: 'idle', text: settingsT('settings.robots.statusNotConfigured', '未配置') };
}

function refreshRobotManager() {
    ['wechat', 'wecom', 'dingtalk', 'lark', 'telegram', 'slack', 'discord', 'qq'].forEach((type) => {
        const status = getRobotStatus(type);
        const pill = document.getElementById(`robot-card-${type}-status`);
        if (pill) {
            pill.className = `robot-status-pill robot-status-pill--${status.state}`;
            pill.textContent = status.text;
        }
        const card = document.querySelector(`[data-robot-card="${type}"]`);
        if (card) {
            card.classList.toggle('is-active', activeRobotEditor === type);
        }
    });
}

function openRobotEditor(type) {
	if (activeRobotEditor && activeRobotEditor !== type) {
		robotAuthDrafts[activeRobotEditor] = readRobotAuthPolicyEditor();
	}
    activeRobotEditor = type;
    const empty = document.getElementById('robot-editor-empty');
    if (empty) empty.hidden = true;
    document.querySelectorAll('[data-robot-editor]').forEach((panel) => {
        panel.hidden = panel.dataset.robotEditor !== type;
    });
    refreshRobotManager();
    const panel = document.querySelector(`[data-robot-editor="${type}"]`);
    if (panel) {
        panel.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
    loadRobotAuthPolicyEditor(type);
}

function loadRobotAuthPolicyEditor(type) {
    const panel = document.getElementById('robot-auth-policy-panel');
    if (!panel) return;
    panel.hidden = !type;
    const auth = robotAuthDrafts[type] || (currentConfig?.robots?.[type]?.auth) || {};
    const mode = auth.mode === 'service_account' ? 'service_account' : 'user_binding';
    const modeInput = document.getElementById('robot-auth-mode');
    const serviceUserInput = document.getElementById('robot-service-user-id');
    const allowlistInput = document.getElementById('robot-allowed-external-users');
    if (modeInput) modeInput.value = mode;
    if (serviceUserInput) serviceUserInput.value = auth.service_user_id || '';
    if (allowlistInput) allowlistInput.value = Array.isArray(auth.allowed_external_users) ? auth.allowed_external_users.join('\n') : '';
    onRobotAuthModeChange();
}

function onRobotAuthModeChange() {
    const serviceFields = document.getElementById('robot-service-account-fields');
    if (serviceFields) serviceFields.hidden = document.getElementById('robot-auth-mode')?.value !== 'service_account';
    updateRobotServiceAccountWarning();
}

function updateRobotServiceAccountWarning() {
    const warning = document.getElementById('robot-service-admin-warning');
    if (!warning) return;
    const isServiceMode = document.getElementById('robot-auth-mode')?.value === 'service_account';
    const userID = document.getElementById('robot-service-user-id')?.value.trim().toLowerCase() || '';
    warning.hidden = !(isServiceMode && userID === 'admin');
}

function readRobotAuthPolicyEditor() {
    const mode = document.getElementById('robot-auth-mode')?.value === 'service_account' ? 'service_account' : 'user_binding';
    if (mode === 'user_binding') return { mode };
    const allowed = (document.getElementById('robot-allowed-external-users')?.value || '')
        .split(/[\n,，]/).map(value => value.trim()).filter(Boolean);
    return {
        mode,
        service_user_id: document.getElementById('robot-service-user-id')?.value.trim() || '',
        allowed_external_users: Array.from(new Set(allowed))
    };
}

function robotAuthPayload(type, prevRobots) {
    if (type === activeRobotEditor) return readRobotAuthPolicyEditor();
    return robotAuthDrafts[type] || (prevRobots[type] && prevRobots[type].auth) || { mode: 'user_binding' };
}

function openRobotCreateModal() {
    const modal = document.getElementById('robot-create-modal');
    if (modal) modal.style.display = 'block';
}

function closeRobotCreateModal() {
    const modal = document.getElementById('robot-create-modal');
    if (modal) modal.style.display = 'none';
}

function openRobotCommandsModal() {
    if (typeof openAppModal === 'function') {
        openAppModal('robot-commands-modal', { focus: false });
        return;
    }
    const modal = document.getElementById('robot-commands-modal');
    if (modal) modal.style.display = 'block';
}

function closeRobotCommandsModal() {
    if (typeof closeAppModal === 'function') {
        closeAppModal('robot-commands-modal');
        return;
    }
    const modal = document.getElementById('robot-commands-modal');
    if (modal) modal.style.display = 'none';
}

function selectRobotType(type) {
    closeRobotCreateModal();
    openRobotEditor(type);
}

function bindRobotManagerEvents() {
    const robotInputIds = [
        'robot-wechat-enabled', 'robot-wechat-ilink-bot-id',
        'robot-wecom-enabled', 'robot-wecom-token', 'robot-wecom-corp-id', 'robot-wecom-secret', 'robot-wecom-agent-id',
        'robot-dingtalk-enabled', 'robot-dingtalk-client-id', 'robot-dingtalk-client-secret',
        'robot-lark-enabled', 'robot-lark-app-id', 'robot-lark-app-secret',
        'robot-telegram-enabled', 'robot-telegram-bot-token', 'robot-telegram-bot-username', 'robot-telegram-allow-group',
        'robot-slack-enabled', 'robot-slack-bot-token', 'robot-slack-app-token',
        'robot-discord-enabled', 'robot-discord-bot-token', 'robot-discord-allow-guild',
        'robot-qq-enabled', 'robot-qq-app-id', 'robot-qq-client-secret', 'robot-qq-sandbox'
    ];
    robotInputIds.forEach((id) => {
        const el = document.getElementById(id);
        if (el && !el.dataset.robotManagerBound) {
            el.addEventListener('input', refreshRobotManager);
            el.addEventListener('change', refreshRobotManager);
            el.dataset.robotManagerBound = 'true';
        }
    });

    const modal = document.getElementById('robot-create-modal');
    if (modal && !modal.dataset.robotManagerBound) {
        modal.addEventListener('click', (event) => {
            if (event.target === modal) closeRobotCreateModal();
        });
        modal.dataset.robotManagerBound = 'true';
    }

    const commandsModal = document.getElementById('robot-commands-modal');
    if (commandsModal && !commandsModal.dataset.robotManagerBound) {
        commandsModal.addEventListener('click', (event) => {
            if (event.target === commandsModal) closeRobotCommandsModal();
        });
        commandsModal.dataset.robotManagerBound = 'true';
    }
}

// 生成工具的唯一标识符，用于区分同名但来源不同的工具
function getToolKey(tool) {
    // 如果是外部工具，使用 external_mcp::tool.name 作为唯一标识
    // 如果是内部工具，使用 tool.name 作为标识
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    return tool.name;
}

// 常驻工具配置存储键（外部工具用 mcp::tool，与后端 tool_search 白名单一致）
function getAlwaysVisibleStorageKey(tool) {
    return getToolKey(tool);
}

function addAlwaysVisibleAliases(name) {
    const n = (name || '').trim();
    if (!n) return;
    alwaysVisibleToolNames.add(n);
    if (n.includes('::')) {
        const sep = n.indexOf('::');
        const mcp = n.slice(0, sep);
        const tool = n.slice(sep + 2);
        if (mcp && tool) {
            alwaysVisibleToolNames.add(`${mcp}__${tool}`);
        }
        return;
    }
    if (n.includes('__')) {
        const sep = n.lastIndexOf('__');
        const mcp = n.slice(0, sep);
        const tool = n.slice(sep + 2);
        if (mcp && tool) {
            alwaysVisibleToolNames.add(`${mcp}::${tool}`);
        }
    }
}

function removeAlwaysVisibleAliases(name) {
    const n = (name || '').trim();
    if (!n) return;
    alwaysVisibleToolNames.delete(n);
    if (n.includes('::')) {
        const sep = n.indexOf('::');
        const mcp = n.slice(0, sep);
        const tool = n.slice(sep + 2);
        if (mcp && tool) {
            alwaysVisibleToolNames.delete(`${mcp}__${tool}`);
        }
        return;
    }
    if (n.includes('__')) {
        const sep = n.lastIndexOf('__');
        const mcp = n.slice(0, sep);
        const tool = n.slice(sep + 2);
        if (mcp && tool) {
            alwaysVisibleToolNames.delete(`${mcp}::${tool}`);
        }
    }
}

function isToolAlwaysVisible(tool) {
    const key = getAlwaysVisibleStorageKey(tool);
    if (alwaysVisibleToolNames.has(key)) return true;
    if (alwaysVisibleToolNames.has(tool.name)) return true;
    if (tool.is_external && tool.external_mcp) {
        if (alwaysVisibleToolNames.has(`${tool.external_mcp}__${tool.name}`)) return true;
    }
    return false;
}

function isToolAlwaysVisibleBuiltin(tool) {
    if (alwaysVisibleBuiltinToolNames.has(tool.name)) return true;
    return alwaysVisibleBuiltinToolNames.has(getAlwaysVisibleStorageKey(tool));
}

function getAlwaysVisibleForSave() {
    const out = new Set();
    for (const name of alwaysVisibleToolNames) {
        if (alwaysVisibleBuiltinToolNames.has(name)) continue;
        if (name.includes('::')) {
            out.add(name);
            continue;
        }
        if (name.includes('__')) {
            const sep = name.lastIndexOf('__');
            const mcp = name.slice(0, sep);
            const tool = name.slice(sep + 2);
            if (mcp && tool) out.add(`${mcp}::${tool}`);
            continue;
        }
        out.add(name);
    }
    return Array.from(out);
}

function countUserAlwaysVisibleTools() {
    return getAlwaysVisibleForSave().length;
}
// 从localStorage读取每页显示数量，默认为20
const getToolsPageSize = () => {
    const saved = localStorage.getItem('toolsPageSize');
    return saved ? parseInt(saved, 10) : 20;
};

let toolsPagination = {
    page: 1,
    pageSize: getToolsPageSize(),
    total: 0,
    totalPages: 0
};
let toolsLoadController = null;
let toolsLoadSequence = 0;

let c2NavSyncedOnce = false;

/** 根据是否启用多代理，禁用/启用机器人模式中的 Eino 编排选项 */
function syncRobotAgentModeSelectOptions(multiEnabled) {
    const sel = document.getElementById('multi-agent-robot-mode');
    if (!sel) return;
    ['deep', 'plan_execute', 'supervisor'].forEach(function (v) {
        const opt = sel.querySelector('option[value="' + v + '"]');
        if (opt) opt.disabled = !multiEnabled;
    });
    if (!multiEnabled && ['deep', 'plan_execute', 'supervisor'].indexOf(sel.value) >= 0) {
        sel.value = 'eino_single';
    }
    syncSettingsCustomSelect(sel);
}

/** 首次进入仪表盘等页面前拉一次配置，隐藏侧栏 C2（避免禁用后仍显示） */
window.syncC2NavOnceFromServer = async function syncC2NavOnceFromServer() {
    if (c2NavSyncedOnce || typeof apiFetch === 'undefined') {
        return;
    }
    c2NavSyncedOnce = true;
    try {
        const r = await apiFetch('/api/config');
        if (r.ok) {
            const cfg = await r.json();
            syncC2NavFromConfig(cfg);
        }
    } catch (_) {
        /* ignore */
    }
};

// 根据 C2 是否启用显示主导航 C2 入口与仪表盘接入概览中的 C2 子块（与 /api/config 的 c2.enabled 一致）
function syncC2NavFromConfig(cfg) {
    const on = cfg && cfg.c2 && cfg.c2.enabled !== false;
    const nav = document.getElementById('nav-c2');
    if (nav) {
        nav.style.display = on ? '' : 'none';
    }
    const c2Tab = document.getElementById('dashboard-access-tab-c2');
    if (c2Tab) {
        if (!on) {
            c2Tab.hidden = true;
        } else {
            c2Tab.removeAttribute('hidden');
        }
    }
    window.__c2Enabled = on;
    if (typeof syncDashboardAccessTabs === 'function') {
        syncDashboardAccessTabs();
    }
}

// 切换设置分类
function switchSettingsSection(section) {
    if (section === 'rbac') {
        if (typeof switchPage === 'function') {
            switchPage('platform-rbac');
        }
        return;
    }

    // 更新导航项状态
    document.querySelectorAll('.settings-nav-item').forEach(item => {
        item.classList.remove('active');
    });
    const activeNavItem = document.querySelector(`.settings-nav-item[data-section="${section}"]`);
    if (activeNavItem) {
        activeNavItem.classList.add('active');
    }
    
    // 更新内容区域显示
    document.querySelectorAll('.settings-section-content').forEach(content => {
        content.classList.remove('active');
    });
    const activeContent = document.getElementById(`settings-section-${section}`);
    if (activeContent) {
        activeContent.classList.add('active');
        initSettingsCustomSelects(activeContent);
    }
	if (section === 'robots' && typeof window.loadVulnerabilityAlertSubscription === 'function') {
		window.loadVulnerabilityAlertSubscription();
	}
    if (section === 'terminal' && typeof initTerminal === 'function') {
        setTimeout(initTerminal, 0);
    }
    if (section === 'audit' && typeof initAuditLogsSection === 'function') {
        setTimeout(initAuditLogsSection, 0);
    }
}

// 打开设置
async function openSettings() {
    // 切换到设置页面
    if (typeof switchPage === 'function') {
        switchPage('settings');
    }
    
    // 每次打开时清空全局状态映射，重新加载最新配置
    toolStateMap.clear();
    
    // 每次打开时重新加载最新配置（系统设置页面不需要加载工具列表）
    await loadConfig(false);
    initSettingsCustomSelects();
    
    // 清除之前的验证错误状态
    document.querySelectorAll('.form-group input').forEach(input => {
        input.classList.remove('error');
    });
    
    // 默认显示基本设置
    switchSettingsSection('basic');
}

// 关闭设置（保留函数以兼容旧代码，但现在不需要关闭功能）
function closeSettings() {
    // 不再需要关闭功能，因为现在是页面而不是模态框
    // 如果需要，可以切换回对话页面
    if (typeof switchPage === 'function') {
        switchPage('chat');
    }
}

// 点击模态框外部关闭（只保留MCP详情模态框）
window.onclick = function(event) {
    const mcpModal = document.getElementById('mcp-detail-modal');
    
    if (event.target === mcpModal) {
        closeMCPDetail();
    }
}

// 加载配置
async function loadConfig(loadTools = true, options = {}) {
    const silent = options && options.silent === true;
    try {
        const response = await apiFetch('/api/config');
        if (!response.ok) {
            if (typeof readApiError === 'function') {
                throw new Error(await readApiError(response, '获取配置失败'));
            }
            throw new Error('获取配置失败');
        }
        
        currentConfig = await response.json();
        const alwaysVisibleConfigured = currentConfig?.multi_agent?.tool_search_always_visible_tools;
        const alwaysVisibleEffective = currentConfig?.multi_agent?.tool_search_always_visible_effective_tools;
        alwaysVisibleToolNames = new Set();
        if (Array.isArray(alwaysVisibleConfigured)) {
            alwaysVisibleConfigured.filter(Boolean).forEach(addAlwaysVisibleAliases);
        }
        alwaysVisibleBuiltinToolNames = new Set();
        if (Array.isArray(alwaysVisibleEffective)) {
            const configuredSet = new Set(Array.isArray(alwaysVisibleConfigured) ? alwaysVisibleConfigured : []);
            alwaysVisibleEffective.filter(Boolean).forEach(name => {
                if (!configuredSet.has(name)) {
                    alwaysVisibleBuiltinToolNames.add(name);
                }
            });
        }
        
        currentConfig.ai = ensureAIConfigShape(currentConfig);
        selectedAIChannelId = currentConfig.ai.default_channel;
        renderAIChannelSelect();
        writeAIChannelToMainForm(selectedAIChannelId);

        fillVisionConfigFromCurrent(currentConfig.vision || {});
        initModelListControls();

        // 填充FOFA配置
        const fofa = currentConfig.fofa || {};
        const fofaKeyEl = document.getElementById('fofa-api-key');
        const fofaBaseUrlEl = document.getElementById('fofa-base-url');
        if (fofaKeyEl) fofaKeyEl.value = fofa.api_key || '';
        if (fofaBaseUrlEl) fofaBaseUrlEl.value = fofa.base_url || '';
        ['zoomeye', 'quake', 'shodan'].forEach((name) => {
            const cfg = currentConfig[name] || {};
            const keyEl = document.getElementById(`${name}-api-key`);
            const baseUrlEl = document.getElementById(`${name}-base-url`);
            if (keyEl) keyEl.value = cfg.api_key || '';
            if (baseUrlEl) baseUrlEl.value = cfg.base_url || '';
        });

        // 填充人机协同配置
        const hitl = currentConfig.hitl || {};
        const hitlReviewerEl = document.getElementById('hitl-default-reviewer');
        if (hitlReviewerEl) {
            const reviewer = String(hitl.default_reviewer || 'human').trim().toLowerCase();
            hitlReviewerEl.value = reviewer === 'audit_agent' ? 'audit_agent' : 'human';
        }
        const hitlAuditModel = hitl.audit_model || {};
        const hitlAuditProviderEl = document.getElementById('hitl-audit-model-provider');
        if (hitlAuditProviderEl) {
            const provider = String(hitlAuditModel.provider || '').trim().toLowerCase();
            hitlAuditProviderEl.value = ['openai', 'claude', 'orcarouter'].includes(provider) ? provider : '';
        }
        const hitlAuditBaseUrlEl = document.getElementById('hitl-audit-model-base-url');
        if (hitlAuditBaseUrlEl) hitlAuditBaseUrlEl.value = hitlAuditModel.base_url || '';
        const hitlAuditApiKeyEl = document.getElementById('hitl-audit-model-api-key');
        if (hitlAuditApiKeyEl) hitlAuditApiKeyEl.value = hitlAuditModel.api_key || '';
        const hitlAuditModelNameEl = document.getElementById('hitl-audit-model-name');
        if (hitlAuditModelNameEl) hitlAuditModelNameEl.value = hitlAuditModel.model || '';
        const hitlRetentionEl = document.getElementById('hitl-retention-days');
        if (hitlRetentionEl) {
            hitlRetentionEl.value = (hitl.retention_days === undefined || hitl.retention_days === null) ? '90' : String(hitl.retention_days);
        }
        const hitlWhitelistEl = document.getElementById('hitl-tool-whitelist');
        if (hitlWhitelistEl) {
            hitlWhitelistEl.value = Array.isArray(hitl.tool_whitelist) ? hitl.tool_whitelist.join('\n') : '';
        }
        const hitlApprovalPromptEl = document.getElementById('hitl-audit-agent-prompt-settings');
        if (hitlApprovalPromptEl) {
            hitlApprovalPromptEl.value = hitl.audit_agent_prompt || '';
        }
        const hitlReviewEditPromptEl = document.getElementById('hitl-audit-agent-prompt-review-edit-settings');
        if (hitlReviewEditPromptEl) {
            hitlReviewEditPromptEl.value = hitl.audit_agent_prompt_review_edit || '';
        }
        
        // 填充Agent配置
        document.getElementById('agent-max-iterations').value = currentConfig.agent.max_iterations || 30;
        const toolWaitTimeoutEl = document.getElementById('agent-tool-wait-timeout-seconds');
        if (toolWaitTimeoutEl) {
            const v = currentConfig.agent.tool_wait_timeout_seconds;
            toolWaitTimeoutEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '60';
        }
        [
            ['agent-external-mcp-concurrency-server', 'external_mcp_max_concurrent_per_server', '2'],
            ['agent-external-mcp-concurrency-total', 'external_mcp_max_concurrent_total', '16'],
            ['agent-external-mcp-circuit-threshold', 'external_mcp_circuit_failure_threshold', '3'],
            ['agent-external-mcp-circuit-cooldown', 'external_mcp_circuit_cooldown_seconds', '60']
        ].forEach(([id, key, fallback]) => {
            const el = document.getElementById(id);
            if (!el) return;
            const v = currentConfig.agent[key];
            el.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : fallback;
        });

        const ma = currentConfig.multi_agent || {};
        const maEn = document.getElementById('multi-agent-enabled');
        if (maEn) {
            maEn.checked = ma.enabled === true;
            if (!maEn.dataset.robotModeBound) {
                maEn.dataset.robotModeBound = '1';
                maEn.addEventListener('change', function () {
                    syncRobotAgentModeSelectOptions(maEn.checked);
                });
            }
        }
        const maPeLoop = document.getElementById('multi-agent-pe-loop');
        if (maPeLoop) {
            const v = ma.plan_execute_loop_max_iterations;
            maPeLoop.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '0';
        }
        const maRobotMode = document.getElementById('multi-agent-robot-mode');
        if (maRobotMode) {
            let mode = (ma.robot_default_agent_mode || 'eino_single').trim().toLowerCase();
            maRobotMode.value = mode;
            syncRobotAgentModeSelectOptions(ma.enabled === true);
        }
        const modelRetryMaxEl = document.getElementById('eino-model-retry-max-retries');
        if (modelRetryMaxEl) {
            const v = ma.model_retry_max_retries;
            modelRetryMaxEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '0';
        }
        const modelRetryBackoffEl = document.getElementById('eino-model-retry-max-backoff-sec');
        if (modelRetryBackoffEl) {
            const v = ma.model_retry_max_backoff_sec;
            modelRetryBackoffEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '0';
        }
        const modelFailoverChannelsEl = document.getElementById('eino-model-failover-channels');
        if (modelFailoverChannelsEl) {
            const channels = ma.model_failover_channels;
            modelFailoverChannelsEl.value = Array.isArray(channels) ? channels.join('\n') : '';
        }
        const modelFailoverMaxEl = document.getElementById('eino-model-failover-max-retries');
        if (modelFailoverMaxEl) {
            const v = ma.model_failover_max_retries;
            modelFailoverMaxEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '0';
        }
        const userLedgerMaxEl = document.getElementById('summarization-user-ledger-max-runes');
        if (userLedgerMaxEl) {
            const v = ma.summarization_user_intent_ledger_max_runes;
            userLedgerMaxEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '96000';
        }
        const userLedgerEntryMaxEl = document.getElementById('summarization-user-ledger-entry-max-runes');
        if (userLedgerEntryMaxEl) {
            const v = ma.summarization_user_intent_ledger_entry_max_runes;
            userLedgerEntryMaxEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '16000';
        }
        const latestUserMaxEl = document.getElementById('latest-user-message-max-runes');
        if (latestUserMaxEl) {
            const v = ma.latest_user_message_max_runes;
            latestUserMaxEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '48000';
        }
        const latestUserHeadEl = document.getElementById('latest-user-message-head-runes');
        if (latestUserHeadEl) {
            const v = ma.latest_user_message_head_runes;
            latestUserHeadEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '24000';
        }
        const latestUserTailEl = document.getElementById('latest-user-message-tail-runes');
        if (latestUserTailEl) {
            const v = ma.latest_user_message_tail_runes;
            latestUserTailEl.value = (v !== undefined && v !== null && !Number.isNaN(Number(v))) ? String(Number(v)) : '24000';
        }
        
        // 填充知识库配置
        const knowledgeEnabledCheckbox = document.getElementById('knowledge-enabled');
        if (knowledgeEnabledCheckbox) {
            knowledgeEnabledCheckbox.checked = currentConfig.knowledge?.enabled !== false;
        }
        
        // 填充知识库详细配置
        if (currentConfig.knowledge) {
            const knowledge = currentConfig.knowledge;
            
            // 基本配置
            const basePathInput = document.getElementById('knowledge-base-path');
            if (basePathInput) {
                basePathInput.value = knowledge.base_path || 'knowledge_base';
            }
            
            // 嵌入模型配置
            const embeddingProviderSelect = document.getElementById('knowledge-embedding-provider');
            if (embeddingProviderSelect) {
                embeddingProviderSelect.value = knowledge.embedding?.provider || 'openai';
            }
            
            const embeddingModelInput = document.getElementById('knowledge-embedding-model');
            if (embeddingModelInput) {
                embeddingModelInput.value = knowledge.embedding?.model || '';
            }
            
            const embeddingBaseUrlInput = document.getElementById('knowledge-embedding-base-url');
            if (embeddingBaseUrlInput) {
                embeddingBaseUrlInput.value = knowledge.embedding?.base_url || '';
            }
            
            const embeddingApiKeyInput = document.getElementById('knowledge-embedding-api-key');
            if (embeddingApiKeyInput) {
                embeddingApiKeyInput.value = knowledge.embedding?.api_key || '';
            }
            
            // 检索配置
            const retrievalTopKInput = document.getElementById('knowledge-retrieval-top-k');
            if (retrievalTopKInput) {
                retrievalTopKInput.value = knowledge.retrieval?.top_k || 5;
            }
            
            const retrievalThresholdInput = document.getElementById('knowledge-retrieval-similarity-threshold');
            if (retrievalThresholdInput) {
                retrievalThresholdInput.value = knowledge.retrieval?.similarity_threshold || 0.7;
            }
            
            const subIdxFilterInput = document.getElementById('knowledge-retrieval-sub-index-filter');
            if (subIdxFilterInput) {
                subIdxFilterInput.value = knowledge.retrieval?.sub_index_filter || '';
            }

            const mq = knowledge.retrieval?.multi_query || {};
            const mqMaxInput = document.getElementById('knowledge-multi-query-max-queries');
            if (mqMaxInput) {
                const mqVal = parseInt(mq.max_queries, 10);
                mqMaxInput.value = (!isNaN(mqVal) && mqVal > 0) ? mqVal : 4;
            }
            const rr = knowledge.retrieval?.rerank || {};
            const rerankProviderSelect = document.getElementById('knowledge-rerank-provider');
            if (rerankProviderSelect) {
                const p = (rr.provider || '').toLowerCase();
                rerankProviderSelect.value = (p === 'dashscope' || p === 'cohere') ? p : '';
            }
            const rerankModelInput = document.getElementById('knowledge-rerank-model');
            if (rerankModelInput) {
                rerankModelInput.value = rr.model || '';
            }
            const rerankBaseUrlInput = document.getElementById('knowledge-rerank-base-url');
            if (rerankBaseUrlInput) {
                rerankBaseUrlInput.value = rr.base_url || '';
            }
            const rerankApiKeyInput = document.getElementById('knowledge-rerank-api-key');
            if (rerankApiKeyInput) {
                rerankApiKeyInput.value = rr.api_key || '';
            }

            const post = knowledge.retrieval?.post_retrieve || {};
            const prefetchInput = document.getElementById('knowledge-post-retrieve-prefetch-top-k');
            if (prefetchInput) {
                prefetchInput.value = post.prefetch_top_k ?? 20;
            }
            const maxCharsInput = document.getElementById('knowledge-post-retrieve-max-chars');
            if (maxCharsInput) {
                maxCharsInput.value = post.max_context_chars ?? 0;
            }
            const maxTokInput = document.getElementById('knowledge-post-retrieve-max-tokens');
            if (maxTokInput) {
                maxTokInput.value = post.max_context_tokens ?? 0;
            }

            // 索引配置
            const indexing = knowledge.indexing || {};
            const chunkStrategySelect = document.getElementById('knowledge-indexing-chunk-strategy');
            if (chunkStrategySelect) {
                const v = (indexing.chunk_strategy || 'markdown_then_recursive').toLowerCase();
                chunkStrategySelect.value = v === 'recursive' ? 'recursive' : 'markdown_then_recursive';
            }
            const reqTimeoutInput = document.getElementById('knowledge-indexing-request-timeout');
            if (reqTimeoutInput) {
                reqTimeoutInput.value = indexing.request_timeout_seconds ?? 120;
            }
            const batchSizeInput = document.getElementById('knowledge-indexing-batch-size');
            if (batchSizeInput) {
                batchSizeInput.value = indexing.batch_size ?? 64;
            }
            const preferFileCb = document.getElementById('knowledge-indexing-prefer-source-file');
            if (preferFileCb) {
                preferFileCb.checked = indexing.prefer_source_file === true;
            }
            const subIdxInput = document.getElementById('knowledge-indexing-sub-indexes');
            if (subIdxInput) {
                const arr = indexing.sub_indexes;
                subIdxInput.value = Array.isArray(arr) ? arr.join(', ') : (typeof arr === 'string' ? arr : '');
            }
            const chunkSizeInput = document.getElementById('knowledge-indexing-chunk-size');
            if (chunkSizeInput) {
                chunkSizeInput.value = indexing.chunk_size || 512;
            }

            const chunkOverlapInput = document.getElementById('knowledge-indexing-chunk-overlap');
            if (chunkOverlapInput) {
                chunkOverlapInput.value = indexing.chunk_overlap ?? 50;
            }

            const maxChunksPerItemInput = document.getElementById('knowledge-indexing-max-chunks-per-item');
            if (maxChunksPerItemInput) {
                maxChunksPerItemInput.value = indexing.max_chunks_per_item ?? 0;
            }

            const maxRpmInput = document.getElementById('knowledge-indexing-max-rpm');
            if (maxRpmInput) {
                maxRpmInput.value = indexing.max_rpm ?? 0;
            }

            const rateLimitDelayInput = document.getElementById('knowledge-indexing-rate-limit-delay-ms');
            if (rateLimitDelayInput) {
                rateLimitDelayInput.value = indexing.rate_limit_delay_ms ?? 300;
            }

            const maxRetriesInput = document.getElementById('knowledge-indexing-max-retries');
            if (maxRetriesInput) {
                maxRetriesInput.value = indexing.max_retries ?? 3;
            }

            const retryDelayInput = document.getElementById('knowledge-indexing-retry-delay-ms');
            if (retryDelayInput) {
                retryDelayInput.value = indexing.retry_delay_ms ?? 1000;
            }
        }

        const c2EnabledCb = document.getElementById('c2-enabled');
        if (c2EnabledCb) {
            c2EnabledCb.checked = currentConfig.c2?.enabled !== false;
        }
        syncC2NavFromConfig(currentConfig);

        // 填充机器人配置
        robotAuthDrafts = {};
        const robots = currentConfig.robots || {};
        const wechat = robots.wechat || {};
        const wecom = robots.wecom || {};
        const dingtalk = robots.dingtalk || {};
        const lark = robots.lark || {};
        const telegram = robots.telegram || {};
        const slack = robots.slack || {};
        const discord = robots.discord || {};
        const qq = robots.qq || {};
        const wechatEnabled = document.getElementById('robot-wechat-enabled');
        if (wechatEnabled) wechatEnabled.checked = wechat.enabled === true;
        const wechatBase = document.getElementById('robot-wechat-base-url');
        if (wechatBase) wechatBase.value = wechat.base_url || 'https://ilinkai.weixin.qq.com';
        const wechatBotType = document.getElementById('robot-wechat-bot-type');
        if (wechatBotType) wechatBotType.value = wechat.bot_type || '3';
        const wechatBotAgent = document.getElementById('robot-wechat-bot-agent');
        if (wechatBotAgent) wechatBotAgent.value = wechat.bot_agent || 'CyberStrikeAI/1.0';
        const wechatBotId = document.getElementById('robot-wechat-ilink-bot-id');
        if (wechatBotId) wechatBotId.value = wechat.ilink_bot_id || '';
        if (typeof refreshWechatRobotBoundUI === 'function') {
            refreshWechatRobotBoundUI({ ...wechat, bound: !!(wechat.bot_token && wechat.ilink_bot_id) });
        }
        const wecomEnabled = document.getElementById('robot-wecom-enabled');
        if (wecomEnabled) wecomEnabled.checked = wecom.enabled === true;
        const wecomToken = document.getElementById('robot-wecom-token');
        if (wecomToken) wecomToken.value = wecom.token || '';
        const wecomAes = document.getElementById('robot-wecom-encoding-aes-key');
        if (wecomAes) wecomAes.value = wecom.encoding_aes_key || '';
        const wecomCorp = document.getElementById('robot-wecom-corp-id');
        if (wecomCorp) wecomCorp.value = wecom.corp_id || '';
        const wecomSecret = document.getElementById('robot-wecom-secret');
        if (wecomSecret) wecomSecret.value = wecom.secret || '';
        const wecomAgentId = document.getElementById('robot-wecom-agent-id');
        if (wecomAgentId) wecomAgentId.value = wecom.agent_id || '0';
        const dingtalkEnabled = document.getElementById('robot-dingtalk-enabled');
        if (dingtalkEnabled) dingtalkEnabled.checked = dingtalk.enabled === true;
        const dingtalkClientId = document.getElementById('robot-dingtalk-client-id');
        if (dingtalkClientId) dingtalkClientId.value = dingtalk.client_id || '';
        const dingtalkClientSecret = document.getElementById('robot-dingtalk-client-secret');
        if (dingtalkClientSecret) dingtalkClientSecret.value = dingtalk.client_secret || '';
        const larkEnabled = document.getElementById('robot-lark-enabled');
        if (larkEnabled) larkEnabled.checked = lark.enabled === true;
        const larkAppId = document.getElementById('robot-lark-app-id');
        if (larkAppId) larkAppId.value = lark.app_id || '';
        const larkAppSecret = document.getElementById('robot-lark-app-secret');
        if (larkAppSecret) larkAppSecret.value = lark.app_secret || '';
        const larkVerify = document.getElementById('robot-lark-verify-token');
        if (larkVerify) larkVerify.value = lark.verify_token || '';
        const telegramEnabled = document.getElementById('robot-telegram-enabled');
        if (telegramEnabled) telegramEnabled.checked = telegram.enabled === true;
        const telegramToken = document.getElementById('robot-telegram-bot-token');
        if (telegramToken) telegramToken.value = telegram.bot_token || '';
        const telegramUsername = document.getElementById('robot-telegram-bot-username');
        if (telegramUsername) telegramUsername.value = telegram.bot_username || '';
        const telegramAllowGroup = document.getElementById('robot-telegram-allow-group');
        if (telegramAllowGroup) telegramAllowGroup.checked = telegram.allow_group_messages === true;
        const slackEnabled = document.getElementById('robot-slack-enabled');
        if (slackEnabled) slackEnabled.checked = slack.enabled === true;
        const slackBotToken = document.getElementById('robot-slack-bot-token');
        if (slackBotToken) slackBotToken.value = slack.bot_token || '';
        const slackAppToken = document.getElementById('robot-slack-app-token');
        if (slackAppToken) slackAppToken.value = slack.app_token || '';
        const discordEnabled = document.getElementById('robot-discord-enabled');
        if (discordEnabled) discordEnabled.checked = discord.enabled === true;
        const discordToken = document.getElementById('robot-discord-bot-token');
        if (discordToken) discordToken.value = discord.bot_token || '';
        const discordAllowGuild = document.getElementById('robot-discord-allow-guild');
        if (discordAllowGuild) discordAllowGuild.checked = discord.allow_guild_messages === true;
        const qqEnabled = document.getElementById('robot-qq-enabled');
        if (qqEnabled) qqEnabled.checked = qq.enabled === true;
        const qqAppId = document.getElementById('robot-qq-app-id');
        if (qqAppId) qqAppId.value = qq.app_id || '';
        const qqSecret = document.getElementById('robot-qq-client-secret');
        if (qqSecret) qqSecret.value = qq.client_secret || '';
        const qqSandbox = document.getElementById('robot-qq-sandbox');
        if (qqSandbox) qqSandbox.checked = qq.sandbox === true;
        bindRobotManagerEvents();
        refreshRobotManager();
        initSettingsCustomSelects();
        refreshSettingsCustomSelects();
        
        // 只有在需要时才加载工具列表（MCP管理页面需要，系统设置页面不需要）
        if (loadTools) {
            // 设置每页显示数量（会在分页控件渲染时设置）
            const savedPageSize = getToolsPageSize();
            toolsPagination.pageSize = savedPageSize;
            
            // 加载工具列表（使用分页）
            toolsSearchKeyword = '';
            await loadToolsList(1, '');
        }
    } catch (error) {
        console.error('加载配置失败:', error);
        if (!silent) {
            const baseMsg = (typeof window !== 'undefined' && typeof window.t === 'function')
                ? window.t('settings.apply.loadFailed')
                : '加载配置失败';
            if (typeof notifyApiError === 'function') {
                notifyApiError(baseMsg + ': ' + error.message);
            } else {
                alert(baseMsg + ': ' + error.message);
            }
        }
        throw error;
    }
}

// 工具搜索关键词
let toolsSearchKeyword = '';

// 工具状态筛选: '' = 全部, 'true' = 已启用, 'false' = 已停用
let toolsStatusFilter = '';

// 按外部 MCP 来源筛选（点击左侧卡片时设置）
let toolsExternalMcpFilter = '';

// 加载工具列表（分页）
async function loadToolsList(page = 1, searchKeyword = '', options = {}) {
    // 等待 i18n 就绪，避免快速刷新时翻译函数未初始化导致显示占位符
    if (window.i18nReady) await window.i18nReady;
    const toolsList = document.getElementById('tools-list');
    const requestSequence = ++toolsLoadSequence;

    // 新请求接管列表，取消仍在进行的旧请求，避免连续筛选/切页时旧响应覆盖新结果。
    if (toolsLoadController) {
        toolsLoadController.abort();
    }
    const controller = new AbortController();
    toolsLoadController = controller;

    // 清理 DOM 之前先保留用户尚未保存的勾选状态。
    saveCurrentPageToolStates();

    // 首次加载才显示占位；后续刷新保留旧列表，避免整块内容闪烁和布局跳动。
    if (toolsList) {
        toolsList.setAttribute('aria-busy', 'true');
        if (!toolsList.querySelector('.tool-item')) {
            toolsList.innerHTML = '<div class="tools-list-items"><div class="loading" style="padding: 20px; text-align: center; color: var(--text-muted);">⏳ ' + (typeof window.t === 'function' ? window.t('mcp.loadingTools') : '正在加载工具列表...') + '</div></div>';
        }
    }
    
    let timeoutId = null;
    try {
        const pageSize = toolsPagination.pageSize;
        let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
        if (searchKeyword) {
            url += `&search=${encodeURIComponent(searchKeyword)}`;
        }
        if (toolsStatusFilter !== '') {
            url += `&enabled=${toolsStatusFilter}`;
        }
        if (options.refreshExternal) {
            url += '&refresh_external=true';
        }
        if (toolsExternalMcpFilter) {
            url += `&external_mcp=${encodeURIComponent(toolsExternalMcpFilter)}`;
        }
        
        // 使用较短的超时时间（10秒），避免长时间等待
        timeoutId = setTimeout(() => controller.abort(), 10000);
        
        const response = await apiFetch(url, {
            signal: controller.signal
        });
        
        if (!response.ok) {
            if (typeof readApiError === 'function') {
                throw new Error(await readApiError(response, '获取工具列表失败'));
            }
            throw new Error('获取工具列表失败');
        }
        
        const result = await response.json();
        if (requestSequence !== toolsLoadSequence) return;

        allTools = result.tools || [];
        toolsPagination = {
            page: result.page || page,
            pageSize: result.page_size || pageSize,
            total: result.total || 0,
            totalEnabled: result.total_enabled ?? 0,
            totalPages: result.total_pages || 1
        };
        
        // 初始化工具状态映射（如果工具不在映射中，使用服务器返回的状态）
        allTools.forEach(tool => {
            const toolKey = getToolKey(tool);
            if (!toolStateMap.has(toolKey)) {
                toolStateMap.set(toolKey, {
                    enabled: tool.enabled,
                    is_external: tool.is_external || false,
                    external_mcp: tool.external_mcp || '',
                    name: tool.name // 保存原始工具名称
                });
            }
        });
        
        renderToolsList();
        renderToolsPagination();
        renderExternalMcpFilterChip();
        updateExternalMcpCardSelection();
    } catch (error) {
        // 被后续请求替代属于正常控制流，不显示错误，也不覆盖新请求的界面。
        if (controller.signal.aborted && requestSequence !== toolsLoadSequence) return;

        console.error('加载工具列表失败:', error);
        if (toolsList) {
            const isTimeout = error.name === 'AbortError' || error.message.includes('timeout');
            const errorMsg = isTimeout 
                ? (typeof window.t === 'function' ? window.t('mcp.loadToolsTimeout') : '加载工具列表超时，可能是外部MCP连接较慢。请点击"刷新"按钮重试，或检查外部MCP连接状态。')
                : (typeof window.t === 'function' ? window.t('mcp.loadToolsFailed') : '加载工具列表失败') + ': ' + escapeHtml(error.message);
            toolsList.innerHTML = `<div class="error" style="padding: 20px; text-align: center;">${errorMsg}</div>`;
        }
    } finally {
        if (timeoutId !== null) clearTimeout(timeoutId);
        if (requestSequence === toolsLoadSequence) {
            if (toolsList) toolsList.removeAttribute('aria-busy');
            if (toolsLoadController === controller) toolsLoadController = null;
        }
    }
}

// 每行有两类复选框：行首「启用工具」与名称旁「常驻」；统计/全选只应针对行首启用复选框
const TOOL_ENABLE_CHECKBOX_SELECTOR = '#tools-list .tool-item > input[type="checkbox"]';

// 保存当前页的工具状态到全局映射
function saveCurrentPageToolStates() {
    document.querySelectorAll('#tools-list .tool-item').forEach(item => {
        const checkbox = item.querySelector(':scope > input[type="checkbox"]');
        const toolKey = item.dataset.toolKey; // 使用唯一标识符
        const toolName = item.dataset.toolName;
        const isExternal = item.dataset.isExternal === 'true';
        const externalMcp = item.dataset.externalMcp || '';
        if (toolKey && checkbox) {
            toolStateMap.set(toolKey, {
                enabled: checkbox.checked,
                is_external: isExternal,
                external_mcp: externalMcp,
                name: toolName // 保存原始工具名称
            });
        }
    });
}

// 搜索工具
function searchTools() {
    const searchInput = document.getElementById('tools-search');
    const keyword = searchInput ? searchInput.value.trim() : '';
    toolsSearchKeyword = keyword;
    // 搜索时重置到第一页
    loadToolsList(1, keyword);
}

// 清除搜索
function clearSearch() {
    const searchInput = document.getElementById('tools-search');
    if (searchInput) {
        searchInput.value = '';
    }
    toolsSearchKeyword = '';
    loadToolsList(1, '');
}

// 处理搜索框回车事件
function handleSearchKeyPress(event) {
    if (event.key === 'Enter') {
        searchTools();
    }
}

// 按状态筛选工具
function filterToolsByStatus(status) {
    toolsStatusFilter = status;
    // 更新按钮激活状态
    document.querySelectorAll('.tools-status-filter .btn-filter').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.filter === status);
    });
    // 重置到第一页并重新加载
    loadToolsList(1, toolsSearchKeyword);
}

// 渲染工具列表
function renderToolsList() {
    const toolsList = document.getElementById('tools-list');
    if (!toolsList) return;
    
    // 移除可能存在的分页控件（会在 renderToolsPagination 中重新添加）
    const oldPagination = toolsList.querySelector('.tools-pagination');
    if (oldPagination) {
        oldPagination.remove();
    }
    
    // 获取或创建列表容器
    let listContainer = toolsList.querySelector('.tools-list-items');
    if (!listContainer) {
        listContainer = document.createElement('div');
        listContainer.className = 'tools-list-items';
        toolsList.appendChild(listContainer);
    }
    
    // 清空列表容器内容（移除加载提示）
    listContainer.innerHTML = '';
    
    if (allTools.length === 0) {
        listContainer.innerHTML = '<div class="empty">' + (typeof window.t === 'function' ? window.t('mcp.noTools') : '暂无工具') + '</div>';
        if (!toolsList.contains(listContainer)) {
            toolsList.appendChild(listContainer);
        }
        // 更新统计
        updateToolsStats();
        return;
    }
    
    allTools.forEach(tool => {
        const toolKey = getToolKey(tool); // 生成唯一标识符
        const toolItem = document.createElement('div');
        toolItem.className = 'tool-item';
        toolItem.dataset.toolKey = toolKey; // 保存唯一标识符
        toolItem.dataset.toolName = tool.name; // 保存原始工具名称
        toolItem.dataset.isExternal = tool.is_external ? 'true' : 'false';
        toolItem.dataset.externalMcp = tool.external_mcp || '';
        
        // 从全局状态映射获取工具状态，如果不存在则使用服务器返回的状态
        const toolState = toolStateMap.get(toolKey) || {
            enabled: tool.enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || ''
        };
        const alwaysVisibleChecked = isToolAlwaysVisible(tool);
        const alwaysVisibleLocked = isToolAlwaysVisibleBuiltin(tool);
        
        // 外部工具标签，显示来源信息（可点击跳转到对应 MCP 卡片）
        let externalBadge = '';
        if (toolState.is_external || tool.is_external) {
            const externalMcpName = toolState.external_mcp || tool.external_mcp || '';
            const badgeText = externalMcpName ? (typeof window.t === 'function' ? window.t('mcp.externalFrom', { name: escapeHtml(externalMcpName) }) : `外部 (${escapeHtml(externalMcpName)})`) : (typeof window.t === 'function' ? window.t('mcp.externalBadge') : '外部');
            const badgeTitle = externalMcpName ? (typeof window.t === 'function' ? window.t('mcp.externalToolFrom', { name: escapeHtml(externalMcpName) }) + ' — 点击跳转' : `外部MCP工具 - 来源：${escapeHtml(externalMcpName)} — 点击跳转`) : (typeof window.t === 'function' ? window.t('mcp.externalBadge') : '外部MCP工具');
            if (externalMcpName) {
                externalBadge = `<span class="external-tool-badge clickable" onclick="scrollToExternalMCP(${settingsEscapeJsStringAttr(externalMcpName)}, event)" title="${settingsEscapeAttr(badgeTitle)}">${badgeText}</span>`;
            } else {
                externalBadge = `<span class="external-tool-badge" title="${settingsEscapeAttr(badgeTitle)}">${badgeText}</span>`;
            }
        }

        // 生成唯一的checkbox id，使用工具唯一标识符
        const checkboxId = `tool-${settingsEscapeAttr(toolKey).replace(/::/g, '--')}`;

        toolItem.innerHTML = `
            <input type="checkbox" class="theme-checkbox" id="${checkboxId}" ${toolState.enabled ? 'checked' : ''} ${toolState.is_external || tool.is_external ? 'data-external="true"' : ''} onchange="handleToolCheckboxChange(${settingsEscapeJsStringAttr(toolKey)}, this.checked)" />
            <div class="tool-item-info">
                <div class="tool-item-name">
                    ${escapeHtml(tool.name)}
                    ${externalBadge}
                    <label class="tool-resident-toggle" title="${typeof window.t === 'function' ? window.t('mcp.alwaysVisibleHint') : '始终常驻在 Tool Search 可见列表'}" onclick="event.stopPropagation()">
                        <input type="checkbox" class="theme-checkbox" ${alwaysVisibleChecked ? 'checked' : ''} ${alwaysVisibleLocked ? 'disabled' : ''} onchange="handleToolAlwaysVisibleChange(${settingsEscapeJsStringAttr(toolKey)}, this.checked)" />
                        <span>${typeof window.t === 'function' ? window.t('mcp.alwaysVisibleLabel') : '常驻'}</span>
                    </label>
                    ${alwaysVisibleLocked ? `<span class="external-tool-badge" title="${typeof window.t === 'function' ? window.t('mcp.alwaysVisibleBuiltinHint') : '后端内置工具默认常驻，不可关闭'}">${typeof window.t === 'function' ? window.t('mcp.alwaysVisibleBuiltinLabel') : '内置默认'}</span>` : ''}
                    <span class="tool-expand-icon">▶</span>
                </div>
                <div class="tool-item-desc">${escapeHtml(tool.description || (typeof window.t === 'function' ? window.t('mcp.noDescription') : '无描述'))}</div>
                <div class="tool-item-detail" style="display:none"></div>
            </div>
        `;
        toolItem.addEventListener('click', function (event) {
            const infoEl = toolItem.querySelector('.tool-item-info');
            if (!infoEl) return;
            toggleToolDetail(infoEl, toolKey, !!tool.is_external, tool.external_mcp || '', event);
        });
        listContainer.appendChild(toolItem);
    });
    
    if (!toolsList.contains(listContainer)) {
        toolsList.appendChild(listContainer);
    }
    
    // 更新统计
    updateToolsStats();
}

// 展开/折叠工具详情面板（按需从后端加载 schema）
function toggleToolDetail(infoEl, toolKey, isExternal, externalMcp, event) {
    // 点击 checkbox 或外部工具徽章时不展开
    if (event && (event.target.tagName === 'INPUT' || event.target.closest('.external-tool-badge'))) return;

    const detail = infoEl.querySelector('.tool-item-detail');
    const icon = infoEl.querySelector('.tool-expand-icon');
    if (!detail) return;

    // 使用 data-open 作为主状态，避免仅依赖 style.display 带来的首击偶发判定不一致
    const isOpen = detail.dataset.open === '1';
    detail.style.display = isOpen ? 'none' : 'block';
    detail.dataset.open = isOpen ? '0' : '1';
    if (icon) icon.textContent = isOpen ? '▶' : '▼';

    // 首次展开时从后端按需加载
    if (!isOpen && !detail.dataset.rendered) {
        detail.dataset.rendered = '1';
        const descEl = infoEl.querySelector('.tool-item-desc');
        const fullDesc = descEl ? descEl.textContent : '';

        // 先显示加载状态
        detail.innerHTML = `
            <div class="tool-detail-desc">${escapeHtml(fullDesc)}</div>
            <div class="tool-detail-section-title">参数定义</div>
            <div style="color:var(--text-tertiary);font-size:0.8125rem;padding:4px 0;">加载中...</div>
        `;

        // 解析工具名（外部工具 toolKey 格式为 mcpName::toolName）
        let apiToolName = toolKey;
        let query = '';
        if (isExternal && externalMcp) {
            const parts = toolKey.split('::');
            apiToolName = parts.length > 1 ? parts[1] : toolKey;
            query = '?external_mcp=' + encodeURIComponent(externalMcp);
        }

        apiFetch(`/api/config/tools/${encodeURIComponent(apiToolName)}/schema${query}`)
            .then(r => r.json())
            .then(data => {
                const schema = data.input_schema;
                let schemaHTML = '';
                if (schema) {
                    const props = schema.properties || {};
                    const required = schema.required || [];
                    const paramKeys = Object.keys(props);
                    if (paramKeys.length > 0) {
                        schemaHTML = `<table class="tool-schema-table">
                            <thead><tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr></thead>
                            <tbody>`;
                        paramKeys.forEach(key => {
                            const p = props[key] || {};
                            const type = p.type || (p.enum ? 'enum' : '—');
                            const isReq = required.includes(key);
                            const desc = p.description || '';
                            schemaHTML += `<tr>
                                <td><code>${escapeHtml(key)}</code></td>
                                <td>${escapeHtml(String(type))}</td>
                                <td>${isReq ? '<span style="color:#28a745">✔</span>' : ''}</td>
                                <td>${escapeHtml(desc)}</td>
                            </tr>`;
                        });
                        schemaHTML += '</tbody></table>';
                    }
                }
                if (!schemaHTML) {
                    schemaHTML = '<div style="color:var(--text-tertiary);font-size:0.8125rem;padding:4px 0;">无参数定义</div>';
                }
                detail.innerHTML = `
                    <div class="tool-detail-desc">${escapeHtml(fullDesc)}</div>
                    <div class="tool-detail-section-title">参数定义</div>
                    ${schemaHTML}
                `;
            })
            .catch(() => {
                detail.innerHTML = `
                    <div class="tool-detail-desc">${escapeHtml(fullDesc)}</div>
                    <div class="tool-detail-section-title">参数定义</div>
                    <div style="color:var(--text-tertiary);font-size:0.8125rem;padding:4px 0;">加载失败</div>
                `;
            });
    }
}

// 点击外部工具徽章跳转到对应的外部 MCP 卡片
function scrollToExternalMCP(mcpName, event) {
    event.stopPropagation();
    const items = document.querySelectorAll('.external-mcp-item');
    for (const item of items) {
        if (item.dataset.mcpName === mcpName) {
            item.scrollIntoView({ behavior: 'smooth', block: 'center' });
            item.classList.add('highlight');
            setTimeout(() => item.classList.remove('highlight'), 2000);
            return;
        }
    }
}

// 点击左侧外部 MCP 卡片，筛选并定位右侧工具列表
async function scrollToExternalMCPTools(mcpName, event) {
    if (event) {
        if (event.target.closest('.external-mcp-item-actions, button, a, input, label')) {
            return;
        }
        event.stopPropagation();
    }

    if (toolsExternalMcpFilter === mcpName) {
        await clearExternalMcpFilter();
        return;
    }

    toolsExternalMcpFilter = mcpName;
    updateExternalMcpCardSelection();
    renderExternalMcpFilterChip();
    await loadToolsList(1, toolsSearchKeyword);

    requestAnimationFrame(() => {
        highlightExternalMcpTools(mcpName);
    });
}

function highlightExternalMcpTools(mcpName) {
    const toolsList = document.querySelector('.mcp-tools-panel .tools-list');
    if (toolsList) {
        toolsList.scrollTop = 0;
    }

    document.querySelectorAll('#tools-list .tool-item.highlight').forEach(el => {
        el.classList.remove('highlight');
    });

    const selector = `#tools-list .tool-item[data-external-mcp="${CSS.escape(mcpName)}"]`;
    const matchingTools = document.querySelectorAll(selector);
    if (matchingTools.length === 0) {
        return;
    }

    matchingTools[0].scrollIntoView({ behavior: 'smooth', block: 'start' });
    matchingTools.forEach(el => {
        el.classList.add('highlight');
        setTimeout(() => el.classList.remove('highlight'), 2000);
    });
}

async function clearExternalMcpFilter() {
    toolsExternalMcpFilter = '';
    updateExternalMcpCardSelection();
    renderExternalMcpFilterChip();
    await loadToolsList(1, toolsSearchKeyword);
}

function updateExternalMcpCardSelection() {
    document.querySelectorAll('.external-mcp-item').forEach(item => {
        item.classList.toggle('selected', item.dataset.mcpName === toolsExternalMcpFilter);
    });
}

function renderExternalMcpFilterChip() {
    let chip = document.getElementById('tools-source-filter-chip');
    const toolsActions = document.querySelector('.mcp-tools-panel .tools-actions');
    if (!toolsActions) {
        return;
    }

    if (!chip) {
        chip = document.createElement('div');
        chip.id = 'tools-source-filter-chip';
        chip.className = 'tools-source-filter-chip';
        toolsActions.appendChild(chip);
    }

    if (!toolsExternalMcpFilter) {
        chip.style.display = 'none';
        chip.innerHTML = '';
        return;
    }

    const t = typeof window.t === 'function' ? window.t : (k) => k;
    chip.style.display = 'inline-flex';
    chip.innerHTML = `
        <span>${t('mcp.filterBySource', { name: escapeHtml(toolsExternalMcpFilter) })}</span>
        <button type="button" class="tools-source-filter-clear" onclick="clearExternalMcpFilter()" title="${escapeHtml(t('mcp.clearSourceFilter'))}">×</button>
    `;
}

// 渲染工具列表分页控件
function renderToolsPagination() {
    const toolsList = document.getElementById('tools-list');
    if (!toolsList) return;
    
    // 移除旧的分页控件
    const oldPagination = toolsList.querySelector('.tools-pagination');
    if (oldPagination) {
        oldPagination.remove();
    }
    
    // 如果只有一页或没有数据，不显示分页
    if (toolsPagination.totalPages <= 1) {
        return;
    }
    
    const pagination = document.createElement('div');
    pagination.className = 'tools-pagination';
    
    const { page, totalPages, total } = toolsPagination;
    const startItem = (page - 1) * toolsPagination.pageSize + 1;
    const endItem = Math.min(page * toolsPagination.pageSize, total);
    
    const savedPageSize = getToolsPageSize();
    const t = typeof window.t === 'function' ? window.t : (k) => k;
    const paginationT = (key, opts) => {
        if (typeof window.t === 'function') return window.t(key, opts);
        if (key === 'mcp.paginationInfo' && opts) return `显示 ${opts.start}-${opts.end} / 共 ${opts.total} 个工具`;
        if (key === 'mcp.pageInfo' && opts) return `第 ${opts.page} / ${opts.total} 页`;
        return key;
    };
    pagination.innerHTML = `
        <div class="pagination-info">
            ${paginationT('mcp.paginationInfo', { start: startItem, end: endItem, total: total })}${toolsSearchKeyword ? ` (${t('common.search')}: "${escapeHtml(toolsSearchKeyword)}")` : ''}
        </div>
        <div class="pagination-page-size">
            <label for="tools-page-size-pagination">${t('mcp.perPage')}</label>
            <select id="tools-page-size-pagination" onchange="changeToolsPageSize()">
                <option value="10" ${savedPageSize === 10 ? 'selected' : ''}>10</option>
                <option value="20" ${savedPageSize === 20 ? 'selected' : ''}>20</option>
                <option value="50" ${savedPageSize === 50 ? 'selected' : ''}>50</option>
                <option value="100" ${savedPageSize === 100 ? 'selected' : ''}>100</option>
            </select>
        </div>
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="loadToolsList(1, ${settingsEscapeJsStringAttr(toolsSearchKeyword)})" ${page === 1 ? 'disabled' : ''}>${t('mcp.firstPage')}</button>
            <button class="btn-secondary" onclick="loadToolsList(${page - 1}, ${settingsEscapeJsStringAttr(toolsSearchKeyword)})" ${page === 1 ? 'disabled' : ''}>${t('mcp.prevPage')}</button>
            <span class="pagination-page">${paginationT('mcp.pageInfo', { page: page, total: totalPages })}</span>
            <button class="btn-secondary" onclick="loadToolsList(${page + 1}, ${settingsEscapeJsStringAttr(toolsSearchKeyword)})" ${page === totalPages ? 'disabled' : ''}>${t('mcp.nextPage')}</button>
            <button class="btn-secondary" onclick="loadToolsList(${totalPages}, ${settingsEscapeJsStringAttr(toolsSearchKeyword)})" ${page === totalPages ? 'disabled' : ''}>${t('mcp.lastPage')}</button>
        </div>
    `;
    
    toolsList.appendChild(pagination);
}

// 处理工具checkbox状态变化
function handleToolCheckboxChange(toolKey, enabled) {
    // 更新全局状态映射
    const toolItem = document.querySelector(`.tool-item[data-tool-key="${toolKey}"]`);
    if (toolItem) {
        const toolName = toolItem.dataset.toolName;
        const isExternal = toolItem.dataset.isExternal === 'true';
        const externalMcp = toolItem.dataset.externalMcp || '';
        toolStateMap.set(toolKey, {
            enabled: enabled,
            is_external: isExternal,
            external_mcp: externalMcp,
            name: toolName // 保存原始工具名称
        });
    }
    updateToolsStats();
}

function handleToolAlwaysVisibleChange(toolKey, alwaysVisible) {
    const key = (toolKey || '').trim();
    if (!key) return;
    if (alwaysVisible) {
        addAlwaysVisibleAliases(key);
    } else {
        removeAlwaysVisibleAliases(key);
    }
    updateToolsStats();
}

// 全选工具
function selectAllTools() {
    document.querySelectorAll(TOOL_ENABLE_CHECKBOX_SELECTOR).forEach(checkbox => {
        checkbox.checked = true;
        // 更新全局状态映射
        const toolItem = checkbox.closest('.tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                toolStateMap.set(toolKey, {
                    enabled: true,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName // 保存原始工具名称
                });
            }
        }
    });
    updateToolsStats();
}

// 全不选工具
function deselectAllTools() {
    document.querySelectorAll(TOOL_ENABLE_CHECKBOX_SELECTOR).forEach(checkbox => {
        checkbox.checked = false;
        // 更新全局状态映射
        const toolItem = checkbox.closest('.tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                toolStateMap.set(toolKey, {
                    enabled: false,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName // 保存原始工具名称
                });
            }
        }
    });
    updateToolsStats();
}

// 改变每页显示数量
async function changeToolsPageSize() {
    // 尝试从两个位置获取选择器（顶部或分页区域）
    const pageSizeSelect = document.getElementById('tools-page-size') || document.getElementById('tools-page-size-pagination');
    if (!pageSizeSelect) return;
    
    const newPageSize = parseInt(pageSizeSelect.value, 10);
    if (isNaN(newPageSize) || newPageSize < 1) {
        return;
    }
    
    // 保存到localStorage
    localStorage.setItem('toolsPageSize', newPageSize.toString());
    
    // 更新分页配置
    toolsPagination.pageSize = newPageSize;
    
    // 同步更新另一个选择器（如果存在）
    const otherSelect = document.getElementById('tools-page-size') || document.getElementById('tools-page-size-pagination');
    if (otherSelect && otherSelect !== pageSizeSelect) {
        otherSelect.value = newPageSize;
    }
    
    // 重新加载第一页
    await loadToolsList(1, toolsSearchKeyword);
}

// 更新工具统计信息
async function updateToolsStats() {
    const statsEl = document.getElementById('tools-stats');
    if (!statsEl) return;
    
    // 先保存当前页的状态到全局映射
    saveCurrentPageToolStates();
    
    // 计算当前页的启用工具数（仅行首「启用」复选框，不含「常驻」）
    const currentPageEnabled = Array.from(document.querySelectorAll(`${TOOL_ENABLE_CHECKBOX_SELECTOR}:checked`)).length;
    const currentPageTotal = document.querySelectorAll(TOOL_ENABLE_CHECKBOX_SELECTOR).length;
    
    // 计算所有工具的启用数
    let totalEnabled = 0;
    let totalTools = toolsPagination.total || 0;
    
    try {
        // 如果有搜索关键词，只统计搜索结果
        if (toolsSearchKeyword) {
            totalTools = allTools.length;
            totalEnabled = allTools.filter(tool => {
                // 优先使用全局状态映射，否则使用checkbox状态，最后使用服务器返回的状态
                const toolKey = getToolKey(tool);
                const savedState = toolStateMap.get(toolKey);
                if (savedState !== undefined) {
                    return savedState.enabled;
                }
                const checkboxId = `tool-${toolKey.replace(/::/g, '--')}`;
                const checkbox = document.getElementById(checkboxId);
                return checkbox ? checkbox.checked : tool.enabled;
            }).length;
        } else {
            // 使用服务端统计，避免为统计翻页触发多次外部 MCP ListTools
            totalEnabled = toolsPagination.totalEnabled ?? 0;
            if (toolStateMap.size > 0) {
                let delta = 0;
                allTools.forEach(tool => {
                    const toolKey = getToolKey(tool);
                    const savedState = toolStateMap.get(toolKey);
                    if (savedState === undefined) {
                        return;
                    }
                    if (savedState.enabled !== tool.enabled) {
                        delta += savedState.enabled ? 1 : -1;
                    }
                });
                totalEnabled = Math.max(0, totalEnabled + delta);
            }
        }
    } catch (error) {
        console.warn('获取工具统计失败，使用当前页数据', error);
        // 如果获取失败，使用当前页的数据
        totalTools = totalTools || currentPageTotal;
        totalEnabled = currentPageEnabled;
    }
    
    const tStats = typeof window.t === 'function' ? window.t : (k) => k;
    const pinnedCount = countUserAlwaysVisibleTools();
    statsEl.innerHTML = `
        <span title="${tStats('mcp.currentPageEnabled')}">✅ ${tStats('mcp.currentPageEnabled')}: <strong>${currentPageEnabled}</strong> / ${currentPageTotal}</span>
        <span title="${tStats('mcp.totalEnabled')}">📊 ${tStats('mcp.totalEnabled')}: <strong>${totalEnabled}</strong> / ${totalTools}</span>
        <span title="${tStats('mcp.alwaysVisibleHint')}">📌 ${tStats('mcp.alwaysVisibleLabel')}: <strong>${pinnedCount}</strong></span>
    `;
}

// 过滤工具（已废弃，现在使用服务端搜索）
// 保留此函数以防其他地方调用，但实际功能已由searchTools()替代
function filterTools() {
    // 不再使用客户端过滤，改为触发服务端搜索
    // 可以保留为空函数或移除oninput事件
}

// 应用设置
async function applySettings() {
    try {
        // 清除之前的验证错误状态
        document.querySelectorAll('.form-group input').forEach(input => {
            input.classList.remove('error');
        });
        
        // 验证必填字段
        const provider = document.getElementById('openai-provider')?.value || 'openai';
        const apiKey = document.getElementById('openai-api-key').value.trim();
        const baseUrl = document.getElementById('openai-base-url').value.trim();
        const model = document.getElementById('openai-model').value.trim();
        
        let hasError = false;
        
        if (!apiKey) {
            document.getElementById('openai-api-key').classList.add('error');
            hasError = true;
        }
        
        if (!baseUrl) {
            document.getElementById('openai-base-url').classList.add('error');
            hasError = true;
        }
        
        if (!model) {
            document.getElementById('openai-model').classList.add('error');
            hasError = true;
        }
        
        if (hasError) {
            const msg = (typeof window !== 'undefined' && typeof window.t === 'function')
                ? window.t('settings.apply.fillRequired')
                : '请填写所有必填字段（标记为 * 的字段）';
            alert(msg);
            return;
        }

        const visionPayload = collectVisionConfigFromForm();
        if (visionPayload.enabled && !visionPayload.model) {
            const vm = document.getElementById('vision-model');
            if (vm) vm.classList.add('error');
            alert((typeof window.t === 'function') ? window.t('settingsBasic.visionModelRequired') : '启用视觉分析时请填写视觉模型名称');
            return;
        }
        
        // 收集配置
        const knowledgeEnabledCheckbox = document.getElementById('knowledge-enabled');
        const knowledgeEnabled = knowledgeEnabledCheckbox ? knowledgeEnabledCheckbox.checked : true;
        
        // 收集知识库配置
        const c2EnabledCheckbox = document.getElementById('c2-enabled');
        const c2Enabled = c2EnabledCheckbox ? c2EnabledCheckbox.checked : true;

        const knowledgeConfig = {
            enabled: knowledgeEnabled,
            base_path: document.getElementById('knowledge-base-path')?.value.trim() || 'knowledge_base',
            embedding: {
                provider: document.getElementById('knowledge-embedding-provider')?.value || 'openai',
                model: document.getElementById('knowledge-embedding-model')?.value.trim() || '',
                base_url: document.getElementById('knowledge-embedding-base-url')?.value.trim() || '',
                api_key: document.getElementById('knowledge-embedding-api-key')?.value.trim() || ''
            },
            retrieval: {
                top_k: parseInt(document.getElementById('knowledge-retrieval-top-k')?.value) || 5,
                similarity_threshold: (() => {
                    const val = parseFloat(document.getElementById('knowledge-retrieval-similarity-threshold')?.value);
                    return isNaN(val) ? 0.7 : val;
                })(),
                sub_index_filter: document.getElementById('knowledge-retrieval-sub-index-filter')?.value?.trim() || '',
                multi_query: {
                    max_queries: (() => {
                        const v = parseInt(document.getElementById('knowledge-multi-query-max-queries')?.value, 10);
                        if (isNaN(v) || v <= 0) return 4;
                        return Math.min(8, v);
                    })()
                },
                rerank: {
                    provider: document.getElementById('knowledge-rerank-provider')?.value?.trim() || '',
                    model: document.getElementById('knowledge-rerank-model')?.value?.trim() || '',
                    base_url: document.getElementById('knowledge-rerank-base-url')?.value?.trim() || '',
                    api_key: document.getElementById('knowledge-rerank-api-key')?.value?.trim() || ''
                },
                post_retrieve: {
                    prefetch_top_k: (() => {
                        const raw = document.getElementById('knowledge-post-retrieve-prefetch-top-k')?.value;
                        const v = parseInt(raw, 10);
                        return isNaN(v) ? 20 : Math.max(0, v);
                    })(),
                    max_context_chars: parseInt(document.getElementById('knowledge-post-retrieve-max-chars')?.value, 10) || 0,
                    max_context_tokens: parseInt(document.getElementById('knowledge-post-retrieve-max-tokens')?.value, 10) || 0
                }
            },
            indexing: (() => {
                const subRaw = document.getElementById("knowledge-indexing-sub-indexes")?.value?.trim() || "";
                const sub_indexes = subRaw
                    ? subRaw.split(/[,，]/).map(s => s.trim()).filter(Boolean)
                    : [];
                return {
                    chunk_strategy: document.getElementById("knowledge-indexing-chunk-strategy")?.value || "markdown_then_recursive",
                    request_timeout_seconds: parseInt(document.getElementById("knowledge-indexing-request-timeout")?.value, 10) || 0,
                    batch_size: parseInt(document.getElementById("knowledge-indexing-batch-size")?.value, 10) || 0,
                    prefer_source_file: document.getElementById("knowledge-indexing-prefer-source-file")?.checked === true,
                    sub_indexes,
                    chunk_size: parseInt(document.getElementById("knowledge-indexing-chunk-size")?.value) || 512,
                    chunk_overlap: parseInt(document.getElementById("knowledge-indexing-chunk-overlap")?.value) ?? 50,
                    max_chunks_per_item: parseInt(document.getElementById("knowledge-indexing-max-chunks-per-item")?.value) ?? 0,
                    max_rpm: parseInt(document.getElementById("knowledge-indexing-max-rpm")?.value) ?? 0,
                    rate_limit_delay_ms: parseInt(document.getElementById("knowledge-indexing-rate-limit-delay-ms")?.value) ?? 300,
                    max_retries: parseInt(document.getElementById("knowledge-indexing-max-retries")?.value) ?? 3,
                    retry_delay_ms: parseInt(document.getElementById("knowledge-indexing-retry-delay-ms")?.value) ?? 1000
                };
            })()
        };
        
        const wecomAgentIdVal = document.getElementById('robot-wecom-agent-id')?.value.trim();
        if (!currentConfig) currentConfig = {};
        currentConfig.ai = ensureAIConfigShape(currentConfig);
        const activeChannelId = normalizeAIChannelId(selectedAIChannelId || currentConfig.ai.default_channel || 'default');
        currentConfig.ai.channels[activeChannelId] = readAIChannelFromMainForm(activeChannelId);
        currentConfig.ai.default_channel = activeChannelId;
        currentConfig.ai = normalizeAIConfigProviderProfiles(currentConfig.ai);
        renderAIChannelSelect();
        const activeChannel = currentConfig.ai.channels[activeChannelId] || {};
        const prevOpenai = activeChannel;
        const prevRobots = (currentConfig && currentConfig.robots) ? currentConfig.robots : {};
        const prevHitl = (currentConfig && currentConfig.hitl) ? currentConfig.hitl : {};
        const hitlRetentionRaw = document.getElementById('hitl-retention-days')?.value;
        const hitlRetention = parseInt(hitlRetentionRaw, 10);
        const hitlWhitelistRaw = document.getElementById('hitl-tool-whitelist')?.value || '';
        const hitlToolsSplit = (typeof window.hitlToolsSplitToArray === 'function')
            ? window.hitlToolsSplitToArray
            : function (s) {
                return String(s || '').split(/[\n,，]/).map(v => v.trim()).filter(Boolean);
            };
        const config = {
            ai: normalizeAIConfigProviderProfiles(currentConfig.ai),
            vision: visionPayload,
            fofa: {
                api_key: document.getElementById('fofa-api-key')?.value.trim() || '',
                base_url: document.getElementById('fofa-base-url')?.value.trim() || ''
            },
            zoomeye: {
                api_key: document.getElementById('zoomeye-api-key')?.value.trim() || '',
                base_url: document.getElementById('zoomeye-base-url')?.value.trim() || ''
            },
            quake: {
                api_key: document.getElementById('quake-api-key')?.value.trim() || '',
                base_url: document.getElementById('quake-base-url')?.value.trim() || ''
            },
            shodan: {
                api_key: document.getElementById('shodan-api-key')?.value.trim() || '',
                base_url: document.getElementById('shodan-base-url')?.value.trim() || ''
            },
            hitl: {
                ...prevHitl,
                audit_model: {
                    ...(prevHitl.audit_model || {}),
                    provider: document.getElementById('hitl-audit-model-provider')?.value || '',
                    base_url: document.getElementById('hitl-audit-model-base-url')?.value.trim() || '',
                    api_key: document.getElementById('hitl-audit-model-api-key')?.value.trim() || '',
                    model: document.getElementById('hitl-audit-model-name')?.value.trim() || ''
                },
                default_reviewer: document.getElementById('hitl-default-reviewer')?.value === 'audit_agent' ? 'audit_agent' : 'human',
                retention_days: Number.isNaN(hitlRetention) ? 90 : Math.max(0, hitlRetention),
                tool_whitelist: hitlToolsSplit(hitlWhitelistRaw),
                audit_agent_prompt: document.getElementById('hitl-audit-agent-prompt-settings')?.value.trim() || '',
                audit_agent_prompt_review_edit: document.getElementById('hitl-audit-agent-prompt-review-edit-settings')?.value.trim() || ''
            },
            agent: {
                max_iterations: parseInt(document.getElementById('agent-max-iterations').value) || 30,
                tool_wait_timeout_seconds: Math.max(0, parseInt(document.getElementById('agent-tool-wait-timeout-seconds')?.value || '60', 10) || 0),
                external_mcp_max_concurrent_per_server: parseInt(document.getElementById('agent-external-mcp-concurrency-server')?.value || '2', 10) || 0,
                external_mcp_max_concurrent_total: parseInt(document.getElementById('agent-external-mcp-concurrency-total')?.value || '16', 10) || 0,
                external_mcp_circuit_failure_threshold: parseInt(document.getElementById('agent-external-mcp-circuit-threshold')?.value || '3', 10) || 0,
                external_mcp_circuit_cooldown_seconds: Math.max(0, parseInt(document.getElementById('agent-external-mcp-circuit-cooldown')?.value || '60', 10) || 0)
            },
            multi_agent: (function () {
                const peRaw = document.getElementById('multi-agent-pe-loop')?.value;
                const peParsed = parseInt(peRaw, 10);
                const peLoop = Number.isNaN(peParsed) ? 0 : Math.max(0, peParsed);
                const ledgerRaw = document.getElementById('summarization-user-ledger-max-runes')?.value;
                const ledgerParsed = parseInt(ledgerRaw, 10);
                const ledgerMax = Number.isNaN(ledgerParsed) ? 0 : Math.max(0, ledgerParsed);
                const ledgerEntryRaw = document.getElementById('summarization-user-ledger-entry-max-runes')?.value;
                const ledgerEntryParsed = parseInt(ledgerEntryRaw, 10);
                const ledgerEntryMax = Number.isNaN(ledgerEntryParsed) ? 0 : Math.max(0, ledgerEntryParsed);
                const latestRaw = document.getElementById('latest-user-message-max-runes')?.value;
                const latestParsed = parseInt(latestRaw, 10);
                const latestMax = Number.isNaN(latestParsed) ? 0 : Math.max(0, latestParsed);
                const latestHeadRaw = document.getElementById('latest-user-message-head-runes')?.value;
                const latestHeadParsed = parseInt(latestHeadRaw, 10);
                const latestHead = Number.isNaN(latestHeadParsed) ? 0 : Math.max(0, latestHeadParsed);
                const latestTailRaw = document.getElementById('latest-user-message-tail-runes')?.value;
                const latestTailParsed = parseInt(latestTailRaw, 10);
                const latestTail = Number.isNaN(latestTailParsed) ? 0 : Math.max(0, latestTailParsed);
                const maEnabled = document.getElementById('multi-agent-enabled')?.checked === true;
                let robotMode = document.getElementById('multi-agent-robot-mode')?.value || 'eino_single';
                if (!maEnabled && ['deep', 'plan_execute', 'supervisor'].indexOf(robotMode) >= 0) {
                    robotMode = 'eino_single';
                }
                const parseNonNegativeInt = function (id) {
                    const raw = document.getElementById(id)?.value;
                    const parsed = parseInt(raw, 10);
                    return Number.isNaN(parsed) ? 0 : Math.max(0, parsed);
                };
                const failoverChannelsRaw = document.getElementById('eino-model-failover-channels')?.value || '';
                const failoverChannels = Array.from(new Set(
                    failoverChannelsRaw.split(/[\n,，]/).map(s => s.trim()).filter(Boolean)
                ));
                return {
                    enabled: maEnabled,
                    robot_default_agent_mode: robotMode,
                    batch_use_multi_agent: currentConfig?.multi_agent?.batch_use_multi_agent === true,
                    plan_execute_loop_max_iterations: peLoop,
                    model_retry_max_retries: parseNonNegativeInt('eino-model-retry-max-retries'),
                    model_retry_max_backoff_sec: parseNonNegativeInt('eino-model-retry-max-backoff-sec'),
                    model_failover_channels: failoverChannels,
                    model_failover_max_retries: parseNonNegativeInt('eino-model-failover-max-retries'),
                    summarization_user_intent_ledger_max_runes: ledgerMax,
                    summarization_user_intent_ledger_entry_max_runes: ledgerEntryMax,
                    latest_user_message_max_runes: latestMax,
                    latest_user_message_head_runes: latestHead,
                    latest_user_message_tail_runes: latestTail
                };
            })(),
            knowledge: knowledgeConfig,
            c2: {
                enabled: c2Enabled
            },
            robots: {
                ...(prevRobots.session && typeof prevRobots.session === 'object' ? { session: prevRobots.session } : {}),
                wechat: {
                    enabled: document.getElementById('robot-wechat-enabled')?.checked === true,
                    auth: robotAuthPayload('wechat', prevRobots),
                    base_url: document.getElementById('robot-wechat-base-url')?.value.trim() || 'https://ilinkai.weixin.qq.com',
                    bot_type: document.getElementById('robot-wechat-bot-type')?.value.trim() || '3',
                    bot_agent: document.getElementById('robot-wechat-bot-agent')?.value.trim() || 'CyberStrikeAI/1.0',
                    ilink_bot_id: document.getElementById('robot-wechat-ilink-bot-id')?.value.trim() || (prevRobots.wechat && prevRobots.wechat.ilink_bot_id) || '',
                    ...(prevRobots.wechat && typeof prevRobots.wechat === 'object' ? {
                        bot_token: prevRobots.wechat.bot_token || '',
                        ilink_user_id: prevRobots.wechat.ilink_user_id || '',
                        get_updates_buf: prevRobots.wechat.get_updates_buf || ''
                    } : {})
                },
                wecom: {
                    enabled: document.getElementById('robot-wecom-enabled')?.checked === true,
                    auth: robotAuthPayload('wecom', prevRobots),
                    token: document.getElementById('robot-wecom-token')?.value.trim() || '',
                    encoding_aes_key: document.getElementById('robot-wecom-encoding-aes-key')?.value.trim() || '',
                    corp_id: document.getElementById('robot-wecom-corp-id')?.value.trim() || '',
                    secret: document.getElementById('robot-wecom-secret')?.value.trim() || '',
                    agent_id: parseInt(wecomAgentIdVal, 10) || 0
                },
                dingtalk: {
                    enabled: document.getElementById('robot-dingtalk-enabled')?.checked === true,
                    auth: robotAuthPayload('dingtalk', prevRobots),
                    client_id: document.getElementById('robot-dingtalk-client-id')?.value.trim() || '',
                    client_secret: document.getElementById('robot-dingtalk-client-secret')?.value.trim() || '',
                    allow_conversation_id_fallback: !!(prevRobots.dingtalk && prevRobots.dingtalk.allow_conversation_id_fallback)
                },
                lark: {
                    enabled: document.getElementById('robot-lark-enabled')?.checked === true,
                    auth: robotAuthPayload('lark', prevRobots),
                    app_id: document.getElementById('robot-lark-app-id')?.value.trim() || '',
                    app_secret: document.getElementById('robot-lark-app-secret')?.value.trim() || '',
                    verify_token: document.getElementById('robot-lark-verify-token')?.value.trim() || '',
                    allow_chat_id_fallback: !!(prevRobots.lark && prevRobots.lark.allow_chat_id_fallback)
                },
                telegram: {
                    enabled: document.getElementById('robot-telegram-enabled')?.checked === true,
                    auth: robotAuthPayload('telegram', prevRobots),
                    bot_token: document.getElementById('robot-telegram-bot-token')?.value.trim() || '',
                    bot_username: document.getElementById('robot-telegram-bot-username')?.value.trim() || '',
                    allow_group_messages: document.getElementById('robot-telegram-allow-group')?.checked === true,
                    ...(prevRobots.telegram && typeof prevRobots.telegram === 'object' ? {
                        update_offset: prevRobots.telegram.update_offset || 0
                    } : {})
                },
                slack: {
                    enabled: document.getElementById('robot-slack-enabled')?.checked === true,
                    auth: robotAuthPayload('slack', prevRobots),
                    bot_token: document.getElementById('robot-slack-bot-token')?.value.trim() || '',
                    app_token: document.getElementById('robot-slack-app-token')?.value.trim() || ''
                },
                discord: {
                    enabled: document.getElementById('robot-discord-enabled')?.checked === true,
                    auth: robotAuthPayload('discord', prevRobots),
                    bot_token: document.getElementById('robot-discord-bot-token')?.value.trim() || '',
                    allow_guild_messages: document.getElementById('robot-discord-allow-guild')?.checked === true
                },
                qq: {
                    enabled: document.getElementById('robot-qq-enabled')?.checked === true,
                    auth: robotAuthPayload('qq', prevRobots),
                    app_id: document.getElementById('robot-qq-app-id')?.value.trim() || '',
                    client_secret: document.getElementById('robot-qq-client-secret')?.value.trim() || '',
                    sandbox: document.getElementById('robot-qq-sandbox')?.checked === true
                }
            },
            tools: []
        };
        
        // 收集工具启用状态
        // 先保存当前页的状态到全局映射
        saveCurrentPageToolStates();
        
        // 获取所有工具列表以获取完整状态（遍历所有页面）
        // 注意：无论是否在搜索状态下，都要获取所有工具的状态，以确保完整保存
        try {
            const allToolsMap = new Map();
            let page = 1;
            let hasMore = true;
            const pageSize = 100; // 使用合理的页面大小
            
            // 遍历所有页面获取所有工具（不使用搜索关键词，获取全部工具）
            while (hasMore) {
                const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
                
                const pageResponse = await apiFetch(url);
                if (!pageResponse.ok) {
                    throw new Error('获取工具列表失败');
                }
                
                const pageResult = await pageResponse.json();
                
                // 将工具添加到映射中
                // 优先使用全局状态映射中的状态（用户修改过的），否则使用服务器返回的状态
                pageResult.tools.forEach(tool => {
                    const toolKey = getToolKey(tool);
                    const savedState = toolStateMap.get(toolKey);
                    allToolsMap.set(toolKey, {
                        name: tool.name,
                        enabled: savedState ? savedState.enabled : tool.enabled,
                        is_external: savedState ? savedState.is_external : (tool.is_external || false),
                        external_mcp: savedState ? savedState.external_mcp : (tool.external_mcp || '')
                    });
                });
                
                // 检查是否还有更多页面
                if (page >= pageResult.total_pages) {
                    hasMore = false;
                } else {
                    page++;
                }
            }
            
            // 将所有工具添加到配置中
            allToolsMap.forEach((tool, toolKey) => {
                config.tools.push({
                    name: tool.name,
                    enabled: tool.enabled,
                    is_external: tool.is_external,
                    external_mcp: tool.external_mcp
                });
            });
        } catch (error) {
            console.warn('获取所有工具列表失败，仅使用全局状态映射', error);
            // 如果获取失败，使用全局状态映射
            toolStateMap.forEach((toolData, toolKey) => {
                // toolData.name 保存了原始工具名称
                const toolName = toolData.name || toolKey.split('::').pop();
                config.tools.push({
                    name: toolName,
                    enabled: toolData.enabled,
                    is_external: toolData.is_external,
                    external_mcp: toolData.external_mcp
                });
            });
        }
        
        // 更新配置
        const updateResponse = await apiFetch('/api/config', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(config)
        });
        
        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            const fallback = (typeof window !== 'undefined' && typeof window.t === 'function')
                ? window.t('settings.apply.applyFailed')
                : '应用配置失败';
            throw new Error(error.error || fallback);
        }
        
        // 应用配置
        const applyResponse = await apiFetch('/api/config/apply', {
            method: 'POST'
        });
        
        if (!applyResponse.ok) {
            const error = await applyResponse.json();
            const fallback = (typeof window !== 'undefined' && typeof window.t === 'function')
                ? window.t('settings.apply.applyFailed')
                : '应用配置失败';
            throw new Error(error.error || fallback);
        }
        
        const successMsg = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('settings.apply.applySuccess')
            : '配置已成功应用！';
        alert(successMsg);
        try {
            const cfgResp = await apiFetch('/api/config');
            if (cfgResp.ok) {
                const fresh = await cfgResp.json();
                syncC2NavFromConfig(fresh);
            }
        } catch (e) {
            console.warn('refresh C2 nav after apply', e);
        }
        try {
            if (typeof initChatAgentModeFromConfig === 'function') {
                await initChatAgentModeFromConfig();
            }
        } catch (e) {
            console.warn('initChatAgentModeFromConfig after settings', e);
        }
        closeSettings();
    } catch (error) {
        console.error('应用配置失败:', error);
        const baseMsg = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('settings.apply.applyFailed')
            : '应用配置失败';
        alert(baseMsg + ': ' + error.message);
    }
}

function fillVisionConfigFromCurrent(v) {
    const en = document.getElementById('vision-enabled');
    if (en) en.checked = v.enabled === true;
    const prov = document.getElementById('vision-provider');
    if (prov) prov.value = (v.provider || '').trim();
    const setVal = (id, val) => {
        const el = document.getElementById(id);
        if (el) el.value = val != null && val !== '' ? String(val) : '';
    };
    setVal('vision-api-key', v.api_key || '');
    setVal('vision-base-url', v.base_url || '');
    setVal('vision-model', v.model || '');
    setVal('vision-max-image-bytes', v.max_image_bytes || 5242880);
    setVal('vision-max-dimension', v.max_dimension || 2048);
    setVal('vision-jpeg-quality', v.jpeg_quality || 82);
    setVal('vision-max-payload-bytes', v.max_payload_bytes || 524288);
    setVal('vision-skip-preprocess-bytes', v.skip_preprocess_below_bytes != null ? v.skip_preprocess_below_bytes : 2097152);
    setVal('vision-timeout-seconds', v.timeout_seconds || 60);
    const det = document.getElementById('vision-detail');
    if (det) {
        const d = (v.detail || 'low').toString().toLowerCase();
        det.value = ['low', 'auto', 'high'].includes(d) ? d : 'low';
    }
    syncVisionFormEnabled();
}

function collectVisionConfigFromForm() {
    const parseIntOr = (id, fallback) => {
        const n = parseInt(document.getElementById(id)?.value, 10);
        return Number.isNaN(n) ? fallback : n;
    };
    const provider = document.getElementById('vision-provider')?.value.trim() || '';
    return {
        enabled: document.getElementById('vision-enabled')?.checked === true,
        api_key: document.getElementById('vision-api-key')?.value.trim() || '',
        base_url: document.getElementById('vision-base-url')?.value.trim() || '',
        model: document.getElementById('vision-model')?.value.trim() || '',
        provider: provider,
        timeout_seconds: parseIntOr('vision-timeout-seconds', 60),
        max_image_bytes: parseIntOr('vision-max-image-bytes', 5242880),
        max_dimension: parseIntOr('vision-max-dimension', 2048),
        jpeg_quality: parseIntOr('vision-jpeg-quality', 82),
        max_payload_bytes: parseIntOr('vision-max-payload-bytes', 524288),
        skip_preprocess_below_bytes: parseIntOr('vision-skip-preprocess-bytes', 2097152),
        detail: document.getElementById('vision-detail')?.value || 'low'
    };
}

function syncVisionFormEnabled() {
    const enabled = document.getElementById('vision-enabled')?.checked === true;
    const panel = document.getElementById('vision-fields-panel');
    if (panel) {
        panel.style.opacity = enabled ? '1' : '0.55';
        panel.querySelectorAll('input, select, textarea, a').forEach(el => {
            if (el.id === 'test-vision-btn' || el.id === 'fetch-vision-models-btn' || el.id === 'vision-model-select') return;
            el.disabled = !enabled;
        });
        syncModelListFetchButtons();
    }
}

const modelPickSelectMap = {};
let modelPickSelectDocListener = false;

function modelPickT(key) {
    return typeof window.t === 'function' ? window.t(key) : key;
}

function closeAllModelPickDropdowns() {
    Object.keys(modelPickSelectMap).forEach(function (id) {
        modelPickSelectMap[id].wrapper.classList.remove('open');
    });
}

function syncModelPickDropdown(selectId) {
    const reg = modelPickSelectMap[selectId];
    if (!reg) return;
    const { select, dropdown, trigger, wrapper, menuList, countBadge } = reg;
    const placeholder = modelPickT('settingsBasic.modelsListSelectPlaceholder');

    menuList.innerHTML = '';
    let optionCount = 0;
    Array.prototype.forEach.call(select.options, function (opt) {
        if (!opt.value) return;
        optionCount += 1;
        const item = document.createElement('div');
        item.className = 'model-pick-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        }
        const check = document.createElement('span');
        check.className = 'model-pick-option-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'model-pick-option-label';
        label.textContent = opt.textContent;
        item.appendChild(check);
        item.appendChild(label);
        menuList.appendChild(item);
    });

    const selectedOpt = select.selectedIndex >= 0 ? select.options[select.selectedIndex] : null;
    const labelEl = trigger.querySelector('.model-pick-trigger-label');
    if (labelEl) {
        labelEl.textContent = (selectedOpt && selectedOpt.value) ? selectedOpt.textContent : placeholder;
    }
    if (countBadge) {
        countBadge.textContent = String(optionCount);
        countBadge.style.display = optionCount > 0 ? '' : 'none';
    }
    const header = wrapper.querySelector('.model-pick-menu-header');
    if (header) {
        header.textContent = optionCount > 0
            ? placeholder + ' · ' + optionCount
            : placeholder;
    }

    trigger.disabled = !!select.disabled;
    wrapper.classList.toggle('is-disabled', !!select.disabled);
    wrapper.style.display = optionCount > 0 ? '' : 'none';
    select.style.display = 'none';
}

function enhanceModelPickSelect(selectId) {
    const select = document.getElementById(selectId);
    if (!select) return;
    if (select.dataset.modelPickEnhanced === '1') {
        syncModelPickDropdown(selectId);
        return;
    }
    select.dataset.modelPickEnhanced = '1';
    select.classList.add('model-pick-native');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'model-pick-dropdown';
    wrapper.style.display = 'none';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'model-pick-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');

    const labelSpan = document.createElement('span');
    labelSpan.className = 'model-pick-trigger-label';
    labelSpan.textContent = modelPickT('settingsBasic.modelsListSelectPlaceholder');

    const meta = document.createElement('span');
    meta.className = 'model-pick-trigger-meta';

    const countBadge = document.createElement('span');
    countBadge.className = 'model-pick-count';
    countBadge.style.display = 'none';

    const caret = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    caret.setAttribute('class', 'model-pick-caret');
    caret.setAttribute('viewBox', '0 0 16 16');
    caret.setAttribute('aria-hidden', 'true');
    caret.innerHTML = '<path fill="currentColor" d="M4.47 6.47a.75.75 0 0 1 1.06 0L8 8.94l2.47-2.47a.75.75 0 1 1 1.06 1.06l-3 3a.75.75 0 0 1-1.06 0l-3-3a.75.75 0 0 1 0-1.06z"/>';

    meta.appendChild(countBadge);
    meta.appendChild(caret);
    trigger.appendChild(labelSpan);
    trigger.appendChild(meta);

    const menu = document.createElement('div');
    menu.className = 'model-pick-menu';

    const header = document.createElement('div');
    header.className = 'model-pick-menu-header';
    menu.appendChild(header);

    const menuList = document.createElement('div');
    menuList.className = 'model-pick-menu-list';
    menuList.setAttribute('role', 'listbox');
    menu.appendChild(menuList);

    const parent = select.parentNode;
    const fetchLink = parent.querySelector('.model-pick-fetch-link');
    if (fetchLink) {
        parent.insertBefore(wrapper, fetchLink);
    } else {
        parent.appendChild(wrapper);
    }
    wrapper.appendChild(trigger);
    wrapper.appendChild(menu);
    wrapper.appendChild(select);

    modelPickSelectMap[selectId] = {
        wrapper,
        trigger,
        menu,
        menuList,
        countBadge,
        select
    };

    if (!modelPickSelectDocListener) {
        document.addEventListener('click', closeAllModelPickDropdowns);
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllModelPickDropdowns();
        });
        modelPickSelectDocListener = true;
    }

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        const open = wrapper.classList.contains('open');
        closeAllModelPickDropdowns();
        if (!open) wrapper.classList.add('open');
    });

    menuList.addEventListener('click', function (e) {
        const opt = e.target.closest('.model-pick-option');
        if (!opt) return;
        const val = opt.getAttribute('data-value');
        if (val === null || val === '') return;
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        wrapper.classList.remove('open');
        syncModelPickDropdown(selectId);
    });

    syncModelPickDropdown(selectId);
}

function normalizeAIChannelId(name) {
    const raw = String(name || '').trim().toLowerCase().replace(/_/g, '-');
    const id = raw.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    return id || 'default';
}

function aiChannelBaseURLHost(baseUrl) {
    const raw = String(baseUrl || '').trim();
    if (!raw) return '';
    try {
        return new URL(raw).hostname.toLowerCase().replace(/^www\./, '');
    } catch (e) {
        try {
            return new URL(`https://${raw.replace(/^\/+/, '')}`).hostname.toLowerCase().replace(/^www\./, '');
        } catch (_) {
            return '';
        }
    }
}

function isOfficialDeepSeekBaseURL(baseUrl) {
    return aiChannelBaseURLHost(baseUrl) === 'api.deepseek.com';
}

function normalizeAIChannelProviderProfile(channel) {
    if (!channel || typeof channel !== 'object') return channel;
    if (isOfficialDeepSeekBaseURL(channel.base_url)) {
        channel.reasoning = {
            ...(channel.reasoning || {}),
            profile: 'deepseek'
        };
    }
    return channel;
}

function normalizeAIConfigProviderProfiles(ai) {
    if (!ai || typeof ai !== 'object' || !ai.channels || typeof ai.channels !== 'object') return ai;
    Object.keys(ai.channels).forEach((id) => {
        ai.channels[id] = normalizeAIChannelProviderProfile(ai.channels[id] || {});
    });
    return ai;
}

function escapeAIChannelHtml(value) {
    return String(value == null ? '' : value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function ensureAIConfigShape(cfg) {
    const ai = cfg && cfg.ai && typeof cfg.ai === 'object' ? cfg.ai : {};
    const channels = ai.channels && typeof ai.channels === 'object' ? { ...ai.channels } : {};
    let def = normalizeAIChannelId(ai.default_channel || '');
    if (!channels[def]) {
        const oa = (cfg && cfg.openai) ? cfg.openai : {};
        channels[def] = {
            name: def === 'default' ? 'Default' : def,
            provider: oa.provider || 'openai',
            api_key: oa.api_key || '',
            base_url: oa.base_url || '',
            model: oa.model || '',
            max_total_tokens: oa.max_total_tokens || 120000,
            max_completion_tokens: oa.max_completion_tokens || 0,
            reasoning: oa.reasoning || {}
        };
    }
    return normalizeAIConfigProviderProfiles({ default_channel: def, channels });
}

function readAIChannelFromMainForm(id) {
    const prev = currentConfig?.ai?.channels?.[id] || {};
    const maxCompletionTokens = parseInt(document.getElementById('openai-max-completion-tokens')?.value, 10) || 32768;
    return normalizeAIChannelProviderProfile({
        ...prev,
        name: (document.getElementById('ai-channel-name')?.value || '').trim() || prev.name || id,
        provider: document.getElementById('openai-provider')?.value || 'openai',
        api_key: document.getElementById('openai-api-key')?.value.trim() || '',
        base_url: document.getElementById('openai-base-url')?.value.trim() || '',
        model: document.getElementById('openai-model')?.value.trim() || '',
        max_total_tokens: parseInt(document.getElementById('openai-max-total-tokens')?.value, 10) || 120000,
        max_completion_tokens: maxCompletionTokens,
        reasoning: {
            ...(prev.reasoning || {}),
            mode: document.getElementById('openai-reasoning-mode')?.value || 'auto',
            effort: (document.getElementById('openai-reasoning-effort')?.value || '').trim(),
            profile: document.getElementById('openai-reasoning-profile')?.value || 'auto',
            allow_client_reasoning: document.getElementById('openai-reasoning-allow-client')?.checked !== false
        }
    });
}

function writeAIChannelToMainForm(id) {
    const ai = ensureAIConfigShape(currentConfig || {});
    const ch = ai.channels[id] || ai.channels[ai.default_channel] || {};
    selectedAIChannelId = id || ai.default_channel;
    const nameEl = document.getElementById('ai-channel-name');
    if (nameEl) nameEl.value = ch.name || selectedAIChannelId;
    const providerEl = document.getElementById('openai-provider');
    if (providerEl) {
        const provider = (ch.provider === 'openai' || !ch.provider) ? 'openai_compatible' : ch.provider;
        providerEl.value = provider;
        syncSettingsCustomSelect(providerEl);
    }
    const keyEl = document.getElementById('openai-api-key');
    if (keyEl) keyEl.value = ch.api_key || '';
    const baseEl = document.getElementById('openai-base-url');
    if (baseEl) baseEl.value = ch.base_url || '';
    const modelEl = document.getElementById('openai-model');
    if (modelEl) modelEl.value = ch.model || '';
    const maxTokensEl = document.getElementById('openai-max-total-tokens');
    if (maxTokensEl) maxTokensEl.value = ch.max_total_tokens || 120000;
    const maxCompletionTokensEl = document.getElementById('openai-max-completion-tokens');
    if (maxCompletionTokensEl) maxCompletionTokensEl.value = ch.max_completion_tokens || 32768;
    const r = ch.reasoning || {};
    const modeEl = document.getElementById('openai-reasoning-mode');
    if (modeEl) {
        modeEl.value = ['auto', 'on', 'off'].includes(String(r.mode || '').toLowerCase()) ? String(r.mode).toLowerCase() : 'auto';
        syncSettingsCustomSelect(modeEl);
    }
    const effEl = document.getElementById('openai-reasoning-effort');
    if (effEl) {
        effEl.value = ['', 'low', 'medium', 'high', 'max', 'xhigh'].includes(String(r.effort || '').toLowerCase()) ? String(r.effort || '').toLowerCase() : '';
        syncSettingsCustomSelect(effEl);
    }
    const profileEl = document.getElementById('openai-reasoning-profile');
    if (profileEl) {
        profileEl.value = ['auto', 'deepseek', 'deepseek_compat', 'openai_compat', 'output_config_effort'].includes(String(r.profile || '').toLowerCase()) ? String(r.profile || '').toLowerCase() : 'auto';
        syncSettingsCustomSelect(profileEl);
    }
    const allowEl = document.getElementById('openai-reasoning-allow-client');
    if (allowEl) allowEl.checked = r.allow_client_reasoning !== false;
    syncModelListFetchButtons();
    syncAIChannelEditorPreview();
    syncConnectionTestResultForSelectedAIChannel();
}

function displayAIChannelName(id, ch) {
    const name = String(ch?.name || '').trim();
    if ((name === '新通道' || name === 'New Channel') && !String(ch?.model || '').trim()) {
        return settingsT('settingsBasic.aiChannelUntitled', name || id);
    }
    return name || id;
}

function aiChannelSelectLabel(id, ch) {
    const marker = id === currentConfig?.ai?.default_channel ? ' *' : '';
    return `${displayAIChannelName(id, ch)}${marker} · ${ch?.model || '-'}`;
}

function aiChannelOptionProbeMeta(id) {
    const probe = aiChannelProbeResults[id];
    if (!probe) return null;
    const status = probe.status || '';
    if (!['testing', 'ready', 'failed'].includes(status)) return null;
    return {
        status,
        message: probe.message || (status === 'ready'
            ? settingsT('settingsBasic.aiChannelReady', '可用')
            : status === 'testing'
                ? settingsT('settingsBasic.testing', '测试中...')
                : settingsT('settingsBasic.testFailed', '连接失败'))
    };
}

function updateAIChannelSelectOption(id) {
    const select = document.getElementById('ai-channel-select');
    if (!select || !currentConfig?.ai?.channels) return;
    const channelId = normalizeAIChannelId(id || selectedAIChannelId || currentConfig.ai.default_channel || 'default');
    const ch = currentConfig.ai.channels[channelId];
    if (!ch) return;
    const opt = Array.from(select.options).find((option) => option.value === channelId);
    if (opt) {
        opt.textContent = aiChannelSelectLabel(channelId, ch);
        const probeMeta = aiChannelOptionProbeMeta(channelId);
        if (probeMeta) {
            opt.dataset.probeStatus = probeMeta.status;
            opt.dataset.probeMessage = probeMeta.message;
        } else {
            delete opt.dataset.probeStatus;
            delete opt.dataset.probeMessage;
        }
        if (channelId === selectedAIChannelId) {
            select.value = channelId;
            select.selectedIndex = opt.index;
        }
    }
    if (typeof syncSettingsCustomSelect === 'function') {
        syncSettingsCustomSelect(select);
    }
}

function syncSelectedAIChannelUI() {
    updateAIChannelSelectOption(selectedAIChannelId);
    updateAIChannelEditorChrome(selectedAIChannelId);
    renderAIChannelList();
    syncConnectionTestResultForSelectedAIChannel();
}

function syncAIChannelEditorPreview() {
    if (!currentConfig?.ai?.channels || !selectedAIChannelId || !currentConfig.ai.channels[selectedAIChannelId]) return;
    const id = normalizeAIChannelId(selectedAIChannelId);
    const prev = currentConfig.ai.channels[id] || {};
    const next = readAIChannelFromMainForm(id);
    const connectionChanged = ['provider', 'base_url', 'api_key', 'model'].some((key) => String(prev[key] || '') !== String(next[key] || ''));
    if (connectionChanged) {
        delete aiChannelProbeResults[id];
    }
    currentConfig.ai.channels[id] = next;
    syncSelectedAIChannelUI();
}

function bindAIChannelEditorPreviewSync() {
    const ids = [
        'ai-channel-name',
        'openai-provider',
        'openai-api-key',
        'openai-base-url',
        'openai-model'
    ];
    ids.forEach((fieldId) => {
        const el = document.getElementById(fieldId);
        if (!el || el.dataset.aiChannelPreviewBound === '1') return;
        el.dataset.aiChannelPreviewBound = '1';
        const eventName = el.tagName === 'SELECT' ? 'change' : 'input';
        el.addEventListener(eventName, syncAIChannelEditorPreview);
    });
}

function renderAIChannelSelect() {
    if (!currentConfig) return;
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    const select = document.getElementById('ai-channel-select');
    if (!select) return;
    select.innerHTML = '';
    const ids = Object.keys(currentConfig.ai.channels || {}).sort();
    ids.forEach((id) => {
        const ch = currentConfig.ai.channels[id] || {};
        const opt = document.createElement('option');
        opt.value = id;
        opt.textContent = aiChannelSelectLabel(id, ch);
        const probeMeta = aiChannelOptionProbeMeta(id);
        if (probeMeta) {
            opt.dataset.probeStatus = probeMeta.status;
            opt.dataset.probeMessage = probeMeta.message;
        }
        select.appendChild(opt);
    });
    selectedAIChannelId = selectedAIChannelId && currentConfig.ai.channels[selectedAIChannelId]
        ? selectedAIChannelId
        : currentConfig.ai.default_channel;
    select.value = selectedAIChannelId;
    updateAIChannelSelectOption(selectedAIChannelId);
    if (typeof syncSettingsCustomSelect === 'function') {
        syncSettingsCustomSelect(select);
    }
    updateAIChannelEditorChrome(selectedAIChannelId);
    renderAIChannelList(ids);
    const countLabel = typeof window.t === 'function'
        ? window.t('settingsBasic.aiChannelCount').replace('{count}', String(ids.length))
        : `已保存 ${ids.length} 个通道`;
    showAIChannelSaveHint(countLabel, true);
}

function channelHostLabel(baseUrl) {
    const raw = String(baseUrl || '').trim();
    if (!raw) return '-';
    try {
        return new URL(raw).host || raw;
    } catch (e) {
        return raw.replace(/^https?:\/\//, '').split('/')[0] || raw;
    }
}

function renderAIChannelList(ids) {
    const list = document.getElementById('ai-channel-list');
    if (!list || !currentConfig?.ai?.channels) return;
    list.innerHTML = '';
    (ids || Object.keys(currentConfig.ai.channels).sort()).forEach((id) => {
        const ch = currentConfig.ai.channels[id] || {};
        const isDefault = id === currentConfig.ai.default_channel;
        const isComplete = !validateSelectedAIChannelPayload(ch);
        const probe = aiChannelProbeResults[id] || null;
        const item = document.createElement('div');
        item.className = 'ai-channel-list-item' + (id === selectedAIChannelId ? ' active' : '') + (selectedAIChannelBulkIds.has(id) ? ' checked' : '');
        item.setAttribute('role', 'button');
        item.setAttribute('tabindex', '0');
        item.setAttribute('aria-current', id === selectedAIChannelId ? 'true' : 'false');
        item.onclick = () => selectAIChannelForEditing(id);
        item.onkeydown = (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                selectAIChannelForEditing(id);
            }
        };
        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'ai-channel-bulk-check';
        checkbox.checked = selectedAIChannelBulkIds.has(id);
        checkbox.setAttribute('aria-label', settingsT('settingsBasic.aiChannelSelectAria', '选择 {name}').replace('{name}', displayAIChannelName(id, ch)));
        checkbox.onclick = (event) => {
            event.stopPropagation();
            if (checkbox.checked) {
                selectedAIChannelBulkIds.add(id);
            } else {
                selectedAIChannelBulkIds.delete(id);
            }
            item.classList.toggle('checked', checkbox.checked);
        };
        const displayName = displayAIChannelName(id, ch);
        const defaultBadge = isDefault ? `<span class="ai-channel-badge">${escapeAIChannelHtml(settingsT('settingsBasic.aiChannelDefaultBadge', '默认'))}</span>` : '';
        let statusText = isComplete
            ? settingsT('settingsBasic.aiChannelComplete', '配置完整')
            : settingsT('settingsBasic.aiChannelDraft', '待完善');
        let statusClass = isComplete ? 'complete' : 'draft';
        if (probe) {
            statusText = probe.message || statusText;
            statusClass = probe.status || statusClass;
        }
        const body = document.createElement('div');
        body.className = 'ai-channel-card-body';
        body.innerHTML = `
            <div class="ai-channel-list-main">
                <span class="ai-channel-status-dot ${statusClass}" aria-hidden="true"></span>
                <strong title="${escapeAIChannelHtml(displayName)}">${escapeAIChannelHtml(displayName)}</strong>
                ${defaultBadge}
            </div>
            <div class="ai-channel-list-meta" title="${escapeAIChannelHtml(ch.model || '-')} · ${escapeAIChannelHtml(channelHostLabel(ch.base_url))}">${escapeAIChannelHtml(ch.model || '-')} · ${escapeAIChannelHtml(channelHostLabel(ch.base_url))}</div>
            <div class="ai-channel-list-foot">
                <span class="ai-channel-status-label ${statusClass}" title="${escapeAIChannelHtml(statusText)}">${escapeAIChannelHtml(statusText)}</span>
                <span title="${escapeAIChannelHtml(id)}">${escapeAIChannelHtml(id)}</span>
            </div>
        `;
        item.appendChild(checkbox);
        item.appendChild(body);
        list.appendChild(item);
    });
}

function showAIChannelSaveHint(message, ok) {
    const el = document.getElementById('ai-channel-save-hint');
    if (!el) return;
    el.textContent = message;
    el.classList.toggle('is-error', ok === false);
    el.classList.toggle('is-success', ok !== false);
}

function updateAIChannelEditorChrome(id) {
    const ai = ensureAIConfigShape(currentConfig || {});
    const channelId = normalizeAIChannelId(id || ai.default_channel || 'default');
    const ch = ai.channels[channelId] || {};
    const isDefault = channelId === ai.default_channel;
    const isComplete = !validateSelectedAIChannelPayload(ch);
    const probe = aiChannelProbeResults[channelId] || null;
    const title = document.getElementById('ai-channel-editor-title');
    const meta = document.getElementById('ai-channel-editor-meta');
    if (title) {
        title.textContent = settingsT('settingsBasic.aiChannelFormContextHint', '表单保存后会更新该通道配置。');
    }
    if (meta) {
        const provider = ch.provider === 'claude' ? 'Claude' : (ch.provider === 'orcarouter' ? 'OrcaRouter' : settingsT('settingsBasic.aiChannelOpenAICompat', 'OpenAI 兼容'));
        const statusText = probe?.message || (isComplete
            ? settingsT('settingsBasic.aiChannelComplete', '配置完整')
            : settingsT('settingsBasic.aiChannelDraft', '待完善'));
        const statusClass = probe?.status || (isComplete ? 'complete' : 'draft');
        const chips = [
            {
                label: isDefault
                    ? settingsT('settingsBasic.aiChannelDefaultMeta', '默认通道')
                    : settingsT('settingsBasic.aiChannelCustomMeta', '自定义通道'),
                className: isDefault ? 'default' : ''
            },
            { label: provider },
            { label: ch.model || settingsT('settingsBasic.aiChannelModelMissing', '未填写模型') },
            { label: channelHostLabel(ch.base_url) },
            { label: statusText, className: statusClass }
        ].filter((chip) => chip.label);
        meta.innerHTML = chips.map((chip) => {
            const className = chip.className ? ` ${escapeAIChannelHtml(chip.className)}` : '';
            return `<span class="ai-channel-editor-chip${className}" title="${escapeAIChannelHtml(chip.label)}">${escapeAIChannelHtml(chip.label)}</span>`;
        }).join('');
    }
}

function validateSelectedAIChannelPayload(ch) {
    const missing = [];
    if (!String(ch.base_url || '').trim()) missing.push('Base URL');
    if (!String(ch.api_key || '').trim()) missing.push('API Key');
    if (!String(ch.model || '').trim()) missing.push('模型');
    if (missing.length) {
        return missing.join(', ');
    }
    return '';
}

function resolveSavedAIChannelId(ai, preferredId, preferredPayload) {
    const channels = ai?.channels || {};
    const normalizedPreferred = normalizeAIChannelId(preferredId || '');
    if (normalizedPreferred && channels[normalizedPreferred]) return normalizedPreferred;

    const payload = preferredPayload || {};
    const targetName = String(payload.name || '').trim();
    const targetModel = String(payload.model || '').trim();
    const targetBaseUrl = String(payload.base_url || '').trim();
    const targetProvider = String(payload.provider || '').trim();
    const ids = Object.keys(channels).sort();
    const matched = ids.find((id) => {
        const ch = channels[id] || {};
        return String(ch.name || '').trim() === targetName
            && String(ch.model || '').trim() === targetModel
            && String(ch.base_url || '').trim() === targetBaseUrl
            && String(ch.provider || '').trim() === targetProvider;
    });
    return matched || ai?.default_channel || ids[0] || normalizedPreferred || 'default';
}

async function refreshAIChannelsFromServer(preferredId, preferredPayload) {
    const response = await apiFetch('/api/config');
    if (!response.ok) return false;
    currentConfig = await response.json();
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    selectedAIChannelId = resolveSavedAIChannelId(currentConfig.ai, preferredId, preferredPayload);
    renderAIChannelSelect();
    writeAIChannelToMainForm(selectedAIChannelId);
    if (typeof populateChatAIChannelSelect === 'function') {
        populateChatAIChannelSelect(currentConfig.ai);
    }
    return true;
}

async function persistAIChannelsToServer(successMessage, options = {}) {
    if (typeof requirePermission === 'function' && !requirePermission('config:write')) return false;
    if (!currentConfig) currentConfig = {};
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    const id = normalizeAIChannelId(selectedAIChannelId || currentConfig.ai.default_channel || 'default');
    const channelPayload = readAIChannelFromMainForm(id);
    currentConfig.ai.channels[id] = channelPayload;
    selectedAIChannelId = id;
    const missing = validateSelectedAIChannelPayload(channelPayload);
    if (missing) {
        showAIChannelSaveHint(`请填写：${missing}`, false);
        alert(`请填写：${missing}`);
        return false;
    }
    renderAIChannelSelect();
    showAIChannelSaveHint(settingsT('settingsBasic.aiChannelSaving', '正在保存通道...'), true);
    try {
        const shouldMergeLatest = options.mergeLatest !== false;
        const latestResponse = shouldMergeLatest ? await apiFetch('/api/config') : null;
        if (latestResponse && latestResponse.ok) {
            const latest = await latestResponse.json();
            const latestAI = ensureAIConfigShape(latest || {});
            currentConfig.ai.channels = {
                ...(latestAI.channels || {}),
                ...(currentConfig.ai.channels || {}),
                [id]: channelPayload
            };
            if (!currentConfig.ai.default_channel) {
                currentConfig.ai.default_channel = latestAI.default_channel || id;
            }
        }
        currentConfig.ai = normalizeAIConfigProviderProfiles(currentConfig.ai);
        const updateResponse = await apiFetch('/api/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ai: currentConfig.ai })
        });
        if (!updateResponse.ok) {
            const error = await updateResponse.json().catch(() => ({}));
            throw new Error(error.error || '保存通道失败');
        }
        const applyResponse = await apiFetch('/api/config/apply', { method: 'POST' });
        if (!applyResponse.ok) {
            const error = await applyResponse.json().catch(() => ({}));
            throw new Error(error.error || '应用通道失败');
        }
        await refreshAIChannelsFromServer(id, channelPayload);
        showAIChannelSaveHint(successMessage || '通道已保存', true);
        return true;
    } catch (error) {
        showAIChannelSaveHint(error.message || '保存通道失败', false);
        alert(error.message || '保存通道失败');
        return false;
    }
}

async function persistAIConfigOnlyToServer(successMessage) {
    if (typeof requirePermission === 'function' && !requirePermission('config:write')) return false;
    if (!currentConfig) return false;
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    currentConfig.ai = normalizeAIConfigProviderProfiles(currentConfig.ai);
    showAIChannelSaveHint(settingsT('settingsBasic.aiChannelSaving', '正在保存通道...'), true);
    try {
        const updateResponse = await apiFetch('/api/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ai: currentConfig.ai })
        });
        if (!updateResponse.ok) {
            const error = await updateResponse.json().catch(() => ({}));
            throw new Error(error.error || '保存通道失败');
        }
        const applyResponse = await apiFetch('/api/config/apply', { method: 'POST' });
        if (!applyResponse.ok) {
            const error = await applyResponse.json().catch(() => ({}));
            throw new Error(error.error || '应用通道失败');
        }
        await refreshAIChannelsFromServer(selectedAIChannelId);
        showAIChannelSaveHint(successMessage || '通道已保存', true);
        return true;
    } catch (error) {
        showAIChannelSaveHint(error.message || '保存通道失败', false);
        alert(error.message || '保存通道失败');
        return false;
    }
}

async function saveSelectedAIChannel() {
    await persistAIChannelsToServer(typeof window.t === 'function' ? window.t('settingsBasic.aiChannelSaved') : '通道已保存');
}

async function setSelectedAIChannelDefault() {
    if (!currentConfig) return;
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    const id = normalizeAIChannelId(selectedAIChannelId || currentConfig.ai.default_channel || 'default');
    currentConfig.ai.channels[id] = readAIChannelFromMainForm(id);
    currentConfig.ai.default_channel = id;
    await persistAIChannelsToServer(typeof window.t === 'function' ? window.t('settingsBasic.aiChannelDefaultSaved') : '已设为默认通道');
}

function selectAIChannelForEditing(id) {
    if (!currentConfig) return;
    if (selectedAIChannelId && currentConfig.ai?.channels?.[selectedAIChannelId]) {
        currentConfig.ai.channels[selectedAIChannelId] = readAIChannelFromMainForm(selectedAIChannelId);
    }
    const next = normalizeAIChannelId(id || currentConfig.ai?.default_channel || 'default');
    selectedAIChannelId = next;
    writeAIChannelToMainForm(next);
    renderAIChannelSelect();
}

function uniqueAIChannelId(base) {
    const ai = ensureAIConfigShape(currentConfig || {});
    let id = normalizeAIChannelId(base);
    if (!ai.channels[id]) return id;
    let i = 2;
    while (ai.channels[`${id}-${i}`]) i++;
    return `${id}-${i}`;
}

function createAIChannelFromForm() {
    if (!currentConfig) currentConfig = {};
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    if (selectedAIChannelId && currentConfig.ai.channels[selectedAIChannelId]) {
        currentConfig.ai.channels[selectedAIChannelId] = readAIChannelFromMainForm(selectedAIChannelId);
    }
    const baseName = (typeof window.t === 'function' ? window.t('settingsBasic.aiChannelUntitled') : 'New Channel');
    const id = uniqueAIChannelId(baseName);
    currentConfig.ai.channels[id] = {
        name: baseName,
        provider: 'openai_compatible',
        api_key: '',
        base_url: '',
        model: '',
        max_total_tokens: 120000,
        max_completion_tokens: 32768,
        reasoning: { mode: 'auto', effort: '', profile: 'auto', allow_client_reasoning: true }
    };
    selectedAIChannelId = id;
    renderAIChannelSelect();
    writeAIChannelToMainForm(id);
    showAIChannelSaveHint(settingsT('settingsBasic.aiChannelNewUnsaved', '新通道尚未保存，填写后点击「保存更改」。'), true);
}

function copyAIChannelFromForm() {
    if (!currentConfig) return;
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    const source = readAIChannelFromMainForm(selectedAIChannelId || currentConfig.ai.default_channel);
    const id = uniqueAIChannelId((source.name || selectedAIChannelId || 'channel') + '-copy');
    currentConfig.ai.channels[id] = { ...source, name: (source.name || id) + ' Copy' };
    selectedAIChannelId = id;
    renderAIChannelSelect();
    writeAIChannelToMainForm(id);
    showAIChannelSaveHint(settingsT('settingsBasic.aiChannelCopyUnsaved', '复制的通道尚未保存，确认后点击「保存更改」。'), true);
}

async function deleteSelectedAIChannel() {
    if (!currentConfig) return;
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    const ids = Object.keys(currentConfig.ai.channels || {});
    if (ids.length <= 1) {
        alert('至少保留一个 AI 通道');
        return;
    }
    const id = selectedAIChannelId || currentConfig.ai.default_channel;
    const ch = currentConfig.ai.channels[id] || {};
    const name = ch.name || id;
    const msg = typeof window.t === 'function'
        ? window.t('settingsBasic.aiChannelDeleteConfirm').replace('{name}', name)
        : `确定删除 AI 通道「${name}」吗？`;
    if (!confirm(msg)) {
        return;
    }
    delete currentConfig.ai.channels[id];
    delete aiChannelProbeResults[id];
    selectedAIChannelBulkIds.delete(id);

    const remainingIds = Object.keys(currentConfig.ai.channels || {}).sort();
    if (!currentConfig.ai.channels[currentConfig.ai.default_channel]) {
        currentConfig.ai.default_channel = remainingIds[0];
    }
    selectedAIChannelId = currentConfig.ai.default_channel || remainingIds[0];
    renderAIChannelSelect();
    writeAIChannelToMainForm(selectedAIChannelId);
    const saved = await persistAIConfigOnlyToServer(settingsT('settingsBasic.aiChannelDeleted', '通道已删除'));
    if (saved) {
        renderAIChannelSelect();
        writeAIChannelToMainForm(selectedAIChannelId);
    }
}

function selectedOrAllAIChannelIdsForProbe() {
    if (!currentConfig) return [];
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    if (selectedAIChannelId && currentConfig.ai.channels[selectedAIChannelId]) {
        currentConfig.ai.channels[selectedAIChannelId] = readAIChannelFromMainForm(selectedAIChannelId);
    }
    const checked = Array.from(selectedAIChannelBulkIds).filter((id) => currentConfig.ai.channels[id]);
    const ids = checked.length ? checked : Object.keys(currentConfig.ai.channels || {}).sort();
    return ids.filter((id) => !validateSelectedAIChannelPayload(currentConfig.ai.channels[id] || {}));
}

async function probeSelectedAIChannels() {
    if (typeof requirePermission === 'function' && !requirePermission('config:write')) return;
    const ids = selectedOrAllAIChannelIdsForProbe();
    if (!ids.length) {
        alert(settingsT('settingsBasic.aiChannelProbeNoComplete', '没有可探活的完整通道，请先填写 Base URL、API Key 和模型'));
        return;
    }
    showAIChannelSaveHint(settingsT('settingsBasic.aiChannelProbing', '正在探活 {count} 个通道...').replace('{count}', String(ids.length)), true);
    ids.forEach((id) => {
        aiChannelProbeResults[id] = { status: 'testing', message: settingsT('settingsBasic.testing', '测试中...') };
        updateAIChannelSelectOption(id);
    });
    renderAIChannelList();
    let okCount = 0;
    let nextIndex = 0;
    async function probeNextAIChannel() {
        const id = ids[nextIndex++];
        if (!id) return;
        const ch = currentConfig.ai.channels[id] || {};
        try {
            const response = await apiFetch('/api/config/test-openai', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    provider: ch.provider || 'openai_compatible',
                    base_url: ch.base_url || '',
                    api_key: ch.api_key || '',
                    model: ch.model || ''
                })
            });
            const result = await response.json().catch(() => ({}));
            if (response.ok && result.success) {
                okCount += 1;
                const latency = result.latency_ms ? ` ${result.latency_ms}ms` : '';
                aiChannelProbeResults[id] = { status: 'ready', message: settingsT('settingsBasic.aiChannelReadyWithLatency', '可用{latency}').replace('{latency}', latency) };
            } else {
                aiChannelProbeResults[id] = { status: 'failed', message: formatConnectionTestError(result.error || settingsT('settingsBasic.testFailed', '连接失败')).message };
            }
        } catch (error) {
            aiChannelProbeResults[id] = { status: 'failed', message: formatConnectionTestError(error.message || settingsT('settingsBasic.testError', '测试出错')).message };
        }
        updateAIChannelSelectOption(id);
        renderAIChannelList();
    }
    const workers = Array.from({ length: Math.min(AI_CHANNEL_PROBE_CONCURRENCY, ids.length) }, async function () {
        while (nextIndex < ids.length) {
            await probeNextAIChannel();
        }
    });
    await Promise.all(workers);
    showAIChannelSaveHint(settingsT('settingsBasic.aiChannelProbeDone', '探活完成：{ok}/{total} 可用').replace('{ok}', String(okCount)).replace('{total}', String(ids.length)), okCount === ids.length);
}

async function deleteCheckedAIChannels() {
    if (typeof requirePermission === 'function' && !requirePermission('config:write')) return;
    if (!currentConfig) return;
    currentConfig.ai = ensureAIConfigShape(currentConfig);
    const ids = Array.from(selectedAIChannelBulkIds).filter((id) => currentConfig.ai.channels[id]);
    if (!ids.length) {
        alert('请先勾选要删除的通道');
        return;
    }
    const deletable = ids.filter((id) => id !== currentConfig.ai.default_channel);
    if (!deletable.length) {
        alert('默认通道不能批量删除，请先切换默认通道');
        return;
    }
    if (Object.keys(currentConfig.ai.channels || {}).length - deletable.length < 1) {
        alert('至少保留一个 AI 通道');
        return;
    }
    const names = deletable.map((id) => currentConfig.ai.channels[id]?.name || id).join('、');
    if (!confirm(`确定删除 ${deletable.length} 个 AI 通道吗？\n${names}`)) {
        return;
    }
    deletable.forEach((id) => {
        delete currentConfig.ai.channels[id];
        selectedAIChannelBulkIds.delete(id);
        delete aiChannelProbeResults[id];
    });
    if (!currentConfig.ai.channels[selectedAIChannelId]) {
        selectedAIChannelId = currentConfig.ai.default_channel;
    }
    renderAIChannelSelect();
    writeAIChannelToMainForm(selectedAIChannelId);
    await persistAIConfigOnlyToServer(`已删除 ${deletable.length} 个通道`);
}

if (typeof window !== 'undefined') {
    window.selectAIChannelForEditing = selectAIChannelForEditing;
    window.saveSelectedAIChannel = saveSelectedAIChannel;
    window.setSelectedAIChannelDefault = setSelectedAIChannelDefault;
    window.createAIChannelFromForm = createAIChannelFromForm;
    window.copyAIChannelFromForm = copyAIChannelFromForm;
    window.deleteSelectedAIChannel = deleteSelectedAIChannel;
    window.probeSelectedAIChannels = probeSelectedAIChannels;
    window.deleteCheckedAIChannels = deleteCheckedAIChannels;
}

if (typeof document !== 'undefined' && !document.__aiChannelI18nBound) {
    document.__aiChannelI18nBound = true;
    document.addEventListener('languagechange', function () {
        if (!currentConfig?.ai) return;
        renderAIChannelSelect();
        updateAIChannelEditorChrome(selectedAIChannelId || currentConfig.ai.default_channel);
    });
}

function initModelListControls() {
    bindAIChannelEditorPreviewSync();
    const providerEl = document.getElementById('openai-provider');
    if (providerEl && !providerEl.dataset.modelListBound) {
        providerEl.dataset.modelListBound = '1';
        providerEl.addEventListener('change', syncModelListFetchButtons);
    }
    const visionProv = document.getElementById('vision-provider');
    if (visionProv && !visionProv.dataset.modelListBound) {
        visionProv.dataset.modelListBound = '1';
        visionProv.addEventListener('change', syncModelListFetchButtons);
    }
    const hitlAuditProv = document.getElementById('hitl-audit-model-provider');
    if (hitlAuditProv && !hitlAuditProv.dataset.modelListBound) {
        hitlAuditProv.dataset.modelListBound = '1';
        hitlAuditProv.addEventListener('change', syncModelListFetchButtons);
    }
    const knowledgeEmbeddingProv = document.getElementById('knowledge-embedding-provider');
    if (knowledgeEmbeddingProv && !knowledgeEmbeddingProv.dataset.modelListBound) {
        knowledgeEmbeddingProv.dataset.modelListBound = '1';
        knowledgeEmbeddingProv.addEventListener('change', syncModelListFetchButtons);
    }
    bindModelSelect('openai');
    bindModelSelect('vision');
    bindModelSelect('hitlAudit');
    bindModelSelect('knowledgeEmbedding');
    syncModelListFetchButtons();
}

function modelSelectIds(scope) {
    if (scope === 'vision') {
        return { selectId: 'vision-model-select', inputId: 'vision-model' };
    }
    if (scope === 'hitlAudit') {
        return { selectId: 'hitl-audit-model-select', inputId: 'hitl-audit-model-name' };
    }
    if (scope === 'knowledgeEmbedding') {
        return { selectId: 'knowledge-embedding-model-select', inputId: 'knowledge-embedding-model' };
    }
    return { selectId: 'openai-model-select', inputId: 'openai-model' };
}

function bindModelSelect(scope) {
    const { selectId, inputId } = modelSelectIds(scope);
    const select = document.getElementById(selectId);
    if (!select || select.dataset.bound) return;
    select.dataset.bound = '1';
    enhanceModelPickSelect(selectId);
    select.addEventListener('change', function () {
        if (!select.value) return;
        const input = document.getElementById(inputId);
        if (input) input.value = select.value;
        if (scope === 'openai') {
            syncAIChannelEditorPreview();
        }
    });
}

function resolveModelListCredentials(scope) {
    if (scope === 'vision') {
        const vp = (document.getElementById('vision-provider')?.value || '').trim();
        const provider = vp || document.getElementById('openai-provider')?.value || 'openai';
        const baseUrl = (document.getElementById('vision-base-url')?.value || '').trim()
            || (document.getElementById('openai-base-url')?.value || '').trim();
        const apiKey = (document.getElementById('vision-api-key')?.value || '').trim()
            || (document.getElementById('openai-api-key')?.value || '').trim();
        return { provider, base_url: baseUrl, api_key: apiKey };
    }
    if (scope === 'hitlAudit') {
        const hp = (document.getElementById('hitl-audit-model-provider')?.value || '').trim();
        const provider = hp || document.getElementById('openai-provider')?.value || 'openai';
        const baseUrl = (document.getElementById('hitl-audit-model-base-url')?.value || '').trim()
            || (document.getElementById('openai-base-url')?.value || '').trim();
        const apiKey = (document.getElementById('hitl-audit-model-api-key')?.value || '').trim()
            || (document.getElementById('openai-api-key')?.value || '').trim();
        return { provider, base_url: baseUrl, api_key: apiKey };
    }
    if (scope === 'knowledgeEmbedding') {
        const kp = (document.getElementById('knowledge-embedding-provider')?.value || '').trim();
        const provider = kp || document.getElementById('openai-provider')?.value || 'openai';
        const baseUrl = (document.getElementById('knowledge-embedding-base-url')?.value || '').trim()
            || (document.getElementById('openai-base-url')?.value || '').trim();
        const apiKey = (document.getElementById('knowledge-embedding-api-key')?.value || '').trim()
            || (document.getElementById('openai-api-key')?.value || '').trim();
        return { provider, base_url: baseUrl, api_key: apiKey };
    }
    return {
        provider: document.getElementById('openai-provider')?.value || 'openai',
        base_url: (document.getElementById('openai-base-url')?.value || '').trim(),
        api_key: (document.getElementById('openai-api-key')?.value || '').trim()
    };
}

function syncModelListFetchButtons() {
    const tFn = typeof window.t === 'function' ? window.t : (k) => k;
    const openaiProv = document.getElementById('openai-provider')?.value || 'openai';
    const openaiBtn = document.getElementById('fetch-openai-models-btn');
    const openaiHint = document.getElementById('fetch-openai-models-hint');
    const openaiSelect = document.getElementById('openai-model-select');
    const isClaudeOpenai = openaiProv === 'claude';
    if (openaiBtn) {
        openaiBtn.style.display = isClaudeOpenai ? 'none' : '';
    }
    if (openaiSelect && isClaudeOpenai) {
        openaiSelect.style.display = 'none';
        const openaiWrap = modelPickSelectMap['openai-model-select'];
        if (openaiWrap) openaiWrap.wrapper.style.display = 'none';
    } else if (openaiSelect && !isClaudeOpenai) {
        syncModelPickDropdown('openai-model-select');
    }
    if (openaiHint) {
        if (isClaudeOpenai) {
            openaiHint.textContent = tFn('settingsBasic.modelsListClaudeHint');
            openaiHint.style.display = '';
        } else {
            openaiHint.textContent = '';
            openaiHint.style.display = 'none';
        }
    }

    const vp = (document.getElementById('vision-provider')?.value || '').trim();
    const visionEffectiveProv = vp || openaiProv;
    const visionBtn = document.getElementById('fetch-vision-models-btn');
    const visionHint = document.getElementById('fetch-vision-models-hint');
    const visionSelect = document.getElementById('vision-model-select');
    const isClaudeVision = visionEffectiveProv === 'claude';
    if (visionBtn) {
        visionBtn.style.display = isClaudeVision ? 'none' : '';
    }
    if (visionSelect && isClaudeVision) {
        visionSelect.style.display = 'none';
        const visionWrap = modelPickSelectMap['vision-model-select'];
        if (visionWrap) visionWrap.wrapper.style.display = 'none';
    } else if (visionSelect && !isClaudeVision) {
        syncModelPickDropdown('vision-model-select');
    }
    if (visionHint) {
        if (isClaudeVision) {
            visionHint.textContent = tFn('settingsBasic.modelsListClaudeHint');
            visionHint.style.display = '';
        } else {
            visionHint.textContent = '';
            visionHint.style.display = 'none';
        }
    }

    const hp = (document.getElementById('hitl-audit-model-provider')?.value || '').trim();
    const hitlAuditEffectiveProv = hp || openaiProv;
    const hitlAuditBtn = document.getElementById('fetch-hitl-audit-models-btn');
    const hitlAuditHint = document.getElementById('fetch-hitl-audit-models-hint');
    const hitlAuditSelect = document.getElementById('hitl-audit-model-select');
    const isClaudeHitlAudit = hitlAuditEffectiveProv === 'claude';
    if (hitlAuditBtn) {
        hitlAuditBtn.style.display = isClaudeHitlAudit ? 'none' : '';
    }
    if (hitlAuditSelect && isClaudeHitlAudit) {
        hitlAuditSelect.style.display = 'none';
        const hitlAuditWrap = modelPickSelectMap['hitl-audit-model-select'];
        if (hitlAuditWrap) hitlAuditWrap.wrapper.style.display = 'none';
    } else if (hitlAuditSelect && !isClaudeHitlAudit) {
        syncModelPickDropdown('hitl-audit-model-select');
    }
    if (hitlAuditHint) {
        if (isClaudeHitlAudit) {
            hitlAuditHint.textContent = tFn('settingsBasic.modelsListClaudeHint');
            hitlAuditHint.style.display = '';
        } else {
            hitlAuditHint.textContent = '';
            hitlAuditHint.style.display = 'none';
        }
    }

    const kp = (document.getElementById('knowledge-embedding-provider')?.value || '').trim();
    const knowledgeEmbeddingEffectiveProv = kp || openaiProv;
    const knowledgeEmbeddingBtn = document.getElementById('fetch-knowledge-embedding-models-btn');
    const knowledgeEmbeddingHint = document.getElementById('fetch-knowledge-embedding-models-hint');
    const knowledgeEmbeddingSelect = document.getElementById('knowledge-embedding-model-select');
    const isClaudeKnowledgeEmbedding = knowledgeEmbeddingEffectiveProv === 'claude';
    if (knowledgeEmbeddingBtn) {
        knowledgeEmbeddingBtn.style.display = isClaudeKnowledgeEmbedding ? 'none' : '';
    }
    if (knowledgeEmbeddingSelect && isClaudeKnowledgeEmbedding) {
        knowledgeEmbeddingSelect.style.display = 'none';
        const knowledgeEmbeddingWrap = modelPickSelectMap['knowledge-embedding-model-select'];
        if (knowledgeEmbeddingWrap) knowledgeEmbeddingWrap.wrapper.style.display = 'none';
    } else if (knowledgeEmbeddingSelect && !isClaudeKnowledgeEmbedding) {
        syncModelPickDropdown('knowledge-embedding-model-select');
    }
    if (knowledgeEmbeddingHint) {
        if (isClaudeKnowledgeEmbedding) {
            knowledgeEmbeddingHint.textContent = tFn('settingsBasic.modelsListClaudeHint');
            knowledgeEmbeddingHint.style.display = '';
        } else {
            knowledgeEmbeddingHint.textContent = '';
            knowledgeEmbeddingHint.style.display = 'none';
        }
    }
}

function populateModelSelect(scope, models, currentValue) {
    const { selectId, inputId } = modelSelectIds(scope);
    const select = document.getElementById(selectId);
    const input = document.getElementById(inputId);
    if (!select) return;
    const tFn = typeof window.t === 'function' ? window.t : (k) => k;
    select.innerHTML = '';
    const placeholder = document.createElement('option');
    placeholder.value = '';
    placeholder.disabled = true;
    placeholder.textContent = tFn('settingsBasic.modelsListSelectPlaceholder');
    select.appendChild(placeholder);

    const seen = new Set();
    const addOption = (id) => {
        const val = (id || '').trim();
        if (!val || seen.has(val)) return;
        seen.add(val);
        const opt = document.createElement('option');
        opt.value = val;
        opt.textContent = val;
        select.appendChild(opt);
    };
    (models || []).forEach(addOption);
    const cur = (currentValue || (input && input.value) || '').trim();
    if (cur && seen.has(cur)) {
        select.value = cur;
    } else {
        select.value = '';
    }
    enhanceModelPickSelect(selectId);
    syncModelPickDropdown(selectId);
}

async function fetchModelList(scope) {
    const tFn = typeof window.t === 'function' ? window.t : (k) => k;
    const creds = resolveModelListCredentials(scope);
    const modelListUiIds = {
        openai: {
            btnId: 'fetch-openai-models-btn',
            resultId: 'fetch-openai-models-result'
        },
        vision: {
            btnId: 'fetch-vision-models-btn',
            resultId: 'fetch-vision-models-result'
        },
        hitlAudit: {
            btnId: 'fetch-hitl-audit-models-btn',
            resultId: 'fetch-hitl-audit-models-result'
        },
        knowledgeEmbedding: {
            btnId: 'fetch-knowledge-embedding-models-btn',
            resultId: 'fetch-knowledge-embedding-models-result'
        }
    };
    const uiIds = modelListUiIds[scope] || modelListUiIds.openai;
    const btnId = uiIds.btnId;
    const resultId = uiIds.resultId;
    const inputId = modelSelectIds(scope).inputId;
    const btn = document.getElementById(btnId);
    const resultEl = document.getElementById(resultId);
    const inputEl = document.getElementById(inputId);

    if (creds.provider === 'claude') {
        if (resultEl) {
            resultEl.textContent = tFn('settingsBasic.modelsListClaudeHint');
            resultEl.style.color = 'var(--text-muted, #718096)';
        }
        return;
    }
    if (!creds.api_key) {
        if (resultEl) {
            resultEl.textContent = tFn('settingsBasic.modelsListNeedApiKey');
            resultEl.style.color = 'var(--error-color, #e53e3e)';
        }
        return;
    }

    if (btn) {
        btn.style.pointerEvents = 'none';
        btn.style.opacity = '0.5';
    }
    if (resultEl) {
        resultEl.textContent = tFn('settingsBasic.modelsListFetching');
        resultEl.style.color = 'var(--text-muted, #718096)';
    }

    try {
        const response = await apiFetch('/api/config/list-models', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(creds)
        });
        const result = await response.json();
        if (!response.ok) {
            throw new Error(result.error || '请求失败');
        }
        if (!result.success) {
            if (resultEl) {
                resultEl.textContent = (result.supported === false
                    ? tFn('settingsBasic.modelsListClaudeHint')
                    : tFn('settingsBasic.modelsListFailed')) + ': ' + (result.error || '');
                resultEl.style.color = 'var(--error-color, #e53e3e)';
            }
            return;
        }
        const currentValue = inputEl ? inputEl.value.trim() : '';
        populateModelSelect(scope, result.models || [], currentValue);
        if (resultEl) {
            const count = result.count != null ? result.count : (result.models || []).length;
            resultEl.textContent = tFn('settingsBasic.modelsListSuccess').replace('{count}', String(count));
            resultEl.style.color = 'var(--success-color, #38a169)';
        }
    } catch (error) {
        if (resultEl) {
            resultEl.textContent = tFn('settingsBasic.modelsListFailed') + ': ' + error.message;
            resultEl.style.color = 'var(--error-color, #e53e3e)';
        }
    } finally {
        if (btn) {
            btn.style.pointerEvents = '';
            btn.style.opacity = '';
        }
    }
}

async function testVisionConnection() {
    const resultEl = document.getElementById('test-vision-result');
    const vision = collectVisionConfigFromForm();
    const openai = {
        provider: document.getElementById('openai-provider')?.value || 'openai',
        api_key: document.getElementById('openai-api-key')?.value.trim() || '',
        base_url: document.getElementById('openai-base-url')?.value.trim() || '',
        model: document.getElementById('openai-model')?.value.trim() || ''
    };
    const apiKey = vision.api_key || openai.api_key;
    const model = vision.model;
    if (!apiKey || !model) {
        if (resultEl) {
            resultEl.textContent = typeof window.t === 'function' ? window.t('settingsBasic.visionTestFillRequired') : '请填写视觉模型，并确保 API Key 可用';
        }
        return;
    }
    if (resultEl) {
        resultEl.textContent = typeof window.t === 'function' ? window.t('settingsBasic.testing') : '测试中...';
        resultEl.style.color = '';
    }
    try {
        const response = await apiFetch('/api/config/test-vision', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ vision: vision, openai: openai })
        });
        const result = await response.json();
        if (result.success) {
            const latency = result.latency_ms != null ? ` (${result.latency_ms}ms)` : '';
            const modelInfo = result.model ? ` [${result.model}]` : '';
            resultEl.textContent = (typeof window.t === 'function' ? window.t('settingsBasic.testSuccess') : '连接成功') + modelInfo + latency;
            resultEl.style.color = 'var(--success-color, #38a169)';
        } else {
            resultEl.textContent = (typeof window.t === 'function' ? window.t('settingsBasic.testFailed') : '连接失败') + ': ' + (result.error || '未知错误');
            resultEl.style.color = 'var(--error-color, #e53e3e)';
        }
    } catch (error) {
        resultEl.textContent = (typeof window.t === 'function' ? window.t('settingsBasic.testError') : '测试出错') + ': ' + error.message;
        resultEl.style.color = 'var(--error-color, #e53e3e)';
    }
}

function collectHitlAuditModelEffectiveConfig() {
    const main = {
        provider: document.getElementById('openai-provider')?.value || 'openai',
        api_key: document.getElementById('openai-api-key')?.value.trim() || '',
        base_url: document.getElementById('openai-base-url')?.value.trim() || '',
        model: document.getElementById('openai-model')?.value.trim() || ''
    };
    return {
        provider: document.getElementById('hitl-audit-model-provider')?.value || main.provider,
        base_url: document.getElementById('hitl-audit-model-base-url')?.value.trim() || main.base_url,
        api_key: document.getElementById('hitl-audit-model-api-key')?.value.trim() || main.api_key,
        model: document.getElementById('hitl-audit-model-name')?.value.trim() || main.model
    };
}

async function testHitlAuditModelConnection() {
    const btn = document.getElementById('test-hitl-audit-model-btn');
    const resultEl = document.getElementById('test-hitl-audit-model-result');
    const cfg = collectHitlAuditModelEffectiveConfig();

    if (!cfg.base_url || !cfg.api_key || !cfg.model) {
        if (resultEl) {
            resultEl.style.color = 'var(--danger-color, #e53e3e)';
            resultEl.textContent = typeof window.t === 'function' ? window.t('settingsBasic.testFillRequired') : '请先填写 Base URL、API Key 和模型';
        }
        return;
    }

    if (btn) {
        btn.style.pointerEvents = 'none';
        btn.style.opacity = '0.5';
    }
    if (resultEl) {
        resultEl.style.color = 'var(--text-muted, #888)';
        resultEl.textContent = typeof window.t === 'function' ? window.t('settingsBasic.testing') : '测试中...';
    }

    try {
        const response = await apiFetch('/api/config/test-openai', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(cfg)
        });
        const result = await response.json();

        if (result.success) {
            if (resultEl) {
                resultEl.style.color = 'var(--success-color, #38a169)';
                const latency = result.latency_ms ? ` (${result.latency_ms}ms)` : '';
                const modelInfo = result.model ? ` [${result.model}]` : '';
                resultEl.textContent = (typeof window.t === 'function' ? window.t('settingsBasic.testSuccess') : '连接成功') + modelInfo + latency;
            }
        } else if (resultEl) {
            resultEl.style.color = 'var(--danger-color, #e53e3e)';
            resultEl.textContent = (typeof window.t === 'function' ? window.t('settingsBasic.testFailed') : '连接失败') + ': ' + (result.error || '未知错误');
        }
    } catch (error) {
        if (resultEl) {
            resultEl.style.color = 'var(--danger-color, #e53e3e)';
            resultEl.textContent = (typeof window.t === 'function' ? window.t('settingsBasic.testError') : '测试出错') + ': ' + error.message;
        }
    } finally {
        if (btn) {
            btn.style.pointerEvents = '';
            btn.style.opacity = '';
        }
    }
}

function formatConnectionTestError(errorText) {
    const raw = String(errorText || '').trim() || settingsT('settingsBasic.testError', '测试出错');
    return {
        message: raw,
        detail: raw
    };
}

function setConnectionTestResult(resultEl, state, message, title) {
    if (!resultEl) return;
    resultEl.classList.remove('is-error', 'is-success', 'is-muted', 'is-visible');
    resultEl.style.color = '';
    resultEl.textContent = message || '';
    resultEl.title = title || '';
    if (message) {
        resultEl.classList.add('is-visible', state || 'is-muted');
    }
}

function syncConnectionTestResultForSelectedAIChannel() {
    const resultEl = document.getElementById('test-openai-result');
    if (!resultEl) return;
    const channelId = normalizeAIChannelId(selectedAIChannelId || currentConfig?.ai?.default_channel || 'default');
    const probe = aiChannelProbeResults[channelId];
    if (!probe) {
        setConnectionTestResult(resultEl, '', '');
        return;
    }
    const state = probe.status === 'ready'
        ? 'is-success'
        : probe.status === 'failed'
            ? 'is-error'
            : 'is-muted';
    let message = probe.message || '';
    const fillRequiredMessage = settingsT('settingsBasic.testFillRequired', '请先填写 Base URL、API Key 和模型');
    const failedPrefix = (typeof window.t === 'function' ? window.t('settingsBasic.testFailed') : '连接失败') + ': ';
    if (probe.status === 'failed' && message && message !== fillRequiredMessage && !message.startsWith(failedPrefix)) {
        message = failedPrefix + message;
    }
    setConnectionTestResult(resultEl, state, message);
}

function showConnectionTestFailure(resultEl, errorText) {
    if (!resultEl) return;
    const formatted = formatConnectionTestError(errorText);
    setConnectionTestResult(
        resultEl,
        'is-error',
        (typeof window.t === 'function' ? window.t('settingsBasic.testFailed') : '连接失败') + ': ' + formatted.message,
        formatted.detail || formatted.message
    );
}

// 测试OpenAI连接
async function testOpenAIConnection() {
    const btn = document.getElementById('test-openai-btn');
    const resultEl = document.getElementById('test-openai-result');

    const provider = document.getElementById('openai-provider')?.value || 'openai';
    const baseUrl = document.getElementById('openai-base-url').value.trim();
    const apiKey = document.getElementById('openai-api-key').value.trim();
    const model = document.getElementById('openai-model').value.trim();
    const channelId = normalizeAIChannelId(selectedAIChannelId || currentConfig?.ai?.default_channel || 'default');
    const isTestingSameChannel = () => normalizeAIChannelId(selectedAIChannelId || currentConfig?.ai?.default_channel || 'default') === channelId;

    if (!baseUrl || !apiKey || !model) {
        const message = typeof window.t === 'function' ? window.t('settingsBasic.testFillRequired') : '请先填写 Base URL、API Key 和模型';
        aiChannelProbeResults[channelId] = { status: 'failed', message };
        if (isTestingSameChannel()) {
            setConnectionTestResult(resultEl, 'is-error', message);
        }
        syncSelectedAIChannelUI();
        return;
    }

    if (btn) {
        btn.style.pointerEvents = 'none';
        btn.style.opacity = '0.5';
    }
    const testingMessage = settingsT('settingsBasic.testing', '测试中...');
    aiChannelProbeResults[channelId] = { status: 'testing', message: testingMessage };
    if (isTestingSameChannel()) {
        setConnectionTestResult(resultEl, 'is-muted', testingMessage);
    }
    syncSelectedAIChannelUI();

    try {
        const response = await apiFetch('/api/config/test-openai', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                provider: provider,
                base_url: baseUrl,
                api_key: apiKey,
                model: model
            })
        });

        const result = await response.json();

        if (result.success) {
            const latency = result.latency_ms ? ` (${result.latency_ms}ms)` : '';
            const modelInfo = result.model ? ` [${result.model}]` : '';
            const message = (typeof window.t === 'function' ? window.t('settingsBasic.testSuccess') : '连接成功') + modelInfo + latency;
            aiChannelProbeResults[channelId] = { status: 'ready', message };
            if (isTestingSameChannel()) {
                setConnectionTestResult(resultEl, 'is-success', message);
            }
        } else {
            const message = formatConnectionTestError(result.error || '未知错误').message;
            aiChannelProbeResults[channelId] = { status: 'failed', message };
            if (isTestingSameChannel()) {
                showConnectionTestFailure(resultEl, message);
            }
        }
    } catch (error) {
        const message = formatConnectionTestError(error.message || '测试出错').message;
        aiChannelProbeResults[channelId] = { status: 'failed', message };
        if (isTestingSameChannel()) {
            showConnectionTestFailure(resultEl, message);
        }
    } finally {
        updateAIChannelSelectOption(channelId);
        syncSelectedAIChannelUI();
        if (btn) {
            btn.style.pointerEvents = '';
            btn.style.opacity = '';
        }
    }
}

// 保存工具配置（独立函数，用于MCP管理页面）
async function saveToolsConfig() {
    if (typeof requirePermission === 'function' && !requirePermission('config:write')) return;
    try {
        // 先保存当前页的状态到全局映射
        saveCurrentPageToolStates();
        
        // 获取当前配置（只获取工具部分）
        const response = await apiFetch('/api/config');
        if (!response.ok) {
            throw new Error('获取配置失败');
        }
        
        const currentConfig = await response.json();
        
        // 构建只包含工具配置的配置对象
        const config = {
            ai: ensureAIConfigShape(currentConfig || {}),
            agent: currentConfig.agent || {},
            multi_agent: {
                enabled: currentConfig?.multi_agent?.enabled === true,
                robot_default_agent_mode: currentConfig?.multi_agent?.robot_default_agent_mode || 'eino_single',
                batch_use_multi_agent: currentConfig?.multi_agent?.batch_use_multi_agent === true,
                plan_execute_loop_max_iterations: Number(currentConfig?.multi_agent?.plan_execute_loop_max_iterations || 0),
                tool_search_always_visible_tools: getAlwaysVisibleForSave()
            },
            tools: []
        };
        
        // 收集工具启用状态（与applySettings中的逻辑相同）
        try {
            const allToolsMap = new Map();
            let page = 1;
            let hasMore = true;
            const pageSize = 100;
            
            // 遍历所有页面获取所有工具
            while (hasMore) {
                const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
                
                const pageResponse = await apiFetch(url);
                if (!pageResponse.ok) {
                    throw new Error('获取工具列表失败');
                }
                
                const pageResult = await pageResponse.json();
                
                // 将工具添加到映射中
                pageResult.tools.forEach(tool => {
                    const toolKey = getToolKey(tool);
                    const savedState = toolStateMap.get(toolKey);
                    allToolsMap.set(toolKey, {
                        name: tool.name,
                        enabled: savedState ? savedState.enabled : tool.enabled,
                        is_external: savedState ? savedState.is_external : (tool.is_external || false),
                        external_mcp: savedState ? savedState.external_mcp : (tool.external_mcp || '')
                    });
                });
                
                // 检查是否还有更多页面
                if (page >= pageResult.total_pages) {
                    hasMore = false;
                } else {
                    page++;
                }
            }
            
            // 将所有工具添加到配置中
            allToolsMap.forEach((tool, toolKey) => {
                config.tools.push({
                    name: tool.name,
                    enabled: tool.enabled,
                    is_external: tool.is_external,
                    external_mcp: tool.external_mcp
                });
            });
        } catch (error) {
            console.warn('获取所有工具列表失败，仅使用全局状态映射', error);
            // 如果获取失败，使用全局状态映射
            toolStateMap.forEach((toolData, toolKey) => {
                // toolData.name 保存了原始工具名称
                const toolName = toolData.name || toolKey.split('::').pop();
                config.tools.push({
                    name: toolName,
                    enabled: toolData.enabled,
                    is_external: toolData.is_external,
                    external_mcp: toolData.external_mcp
                });
            });
        }
        
        // 更新配置
        const updateResponse = await apiFetch('/api/config', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(config)
        });
        
        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            throw new Error(error.error || '更新配置失败');
        }
        
        // 应用配置
        const applyResponse = await apiFetch('/api/config/apply', {
            method: 'POST'
        });
        
        if (!applyResponse.ok) {
            const error = await applyResponse.json();
            throw new Error(error.error || '应用配置失败');
        }
        
        alert(typeof window.t === 'function' ? window.t('mcp.toolsConfigSaved') : '工具配置已成功保存！');
        
        // 重新加载工具列表以反映最新状态
        if (typeof loadToolsList === 'function') {
            await loadToolsList(toolsPagination.page, toolsSearchKeyword);
        }
    } catch (error) {
        console.error('保存工具配置失败:', error);
        alert((typeof window.t === 'function' ? window.t('mcp.saveToolsConfigFailed') : '保存工具配置失败') + ': ' + error.message);
    }
}

function resetPasswordForm() {
    const currentInput = document.getElementById('auth-current-password');
    const newInput = document.getElementById('auth-new-password');
    const confirmInput = document.getElementById('auth-confirm-password');

    [currentInput, newInput, confirmInput].forEach(input => {
        if (input) {
            input.value = '';
            input.classList.remove('error');
        }
    });
}

async function changePassword() {
    const currentInput = document.getElementById('auth-current-password');
    const newInput = document.getElementById('auth-new-password');
    const confirmInput = document.getElementById('auth-confirm-password');
    const submitBtn = document.querySelector('.change-password-submit');

    [currentInput, newInput, confirmInput].forEach(input => input && input.classList.remove('error'));

    const currentPassword = currentInput?.value.trim() || '';
    const newPassword = newInput?.value.trim() || '';
    const confirmPassword = confirmInput?.value.trim() || '';

    let hasError = false;

    if (!currentPassword) {
        currentInput?.classList.add('error');
        hasError = true;
    }

    if (!newPassword || newPassword.length < 8) {
        newInput?.classList.add('error');
        hasError = true;
    }

    if (newPassword !== confirmPassword) {
        confirmInput?.classList.add('error');
        hasError = true;
    }

    if (hasError) {
        alert(typeof window.t === 'function' ? window.t('settings.security.fillPasswordHint') : '请正确填写当前密码和新密码，新密码至少 8 位且需要两次输入一致。');
        return;
    }

    if (submitBtn) {
        submitBtn.disabled = true;
    }

    try {
        const response = await apiFetch('/api/auth/change-password', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                oldPassword: currentPassword,
                newPassword: newPassword
            })
        });

        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
            throw new Error(result.error || '修改密码失败');
        }

        const pwdMsg = typeof window.t === 'function' ? window.t('settings.security.passwordUpdated') : '密码已更新，请使用新密码重新登录。';
        alert(pwdMsg);
        resetPasswordForm();
        handleUnauthorized({ message: pwdMsg, silent: false });
        closeSettings();
    } catch (error) {
        console.error('修改密码失败:', error);
        alert((typeof window.t === 'function' ? window.t('settings.security.changePasswordFailed') : '修改密码失败') + ': ' + error.message);
    } finally {
        if (submitBtn) {
            submitBtn.disabled = false;
        }
    }
}

// ==================== 外部MCP管理 ====================

let currentEditingMCPName = null;

// 拉取外部MCP列表数据（供轮询使用，返回 { servers, stats }）
async function fetchExternalMCPs() {
    const response = await apiFetch('/api/external-mcp');
    if (!response.ok) {
        if (typeof readApiError === 'function') {
            throw new Error(await readApiError(response, '获取外部MCP列表失败'));
        }
        throw new Error('获取外部MCP列表失败');
    }
    return response.json();
}

// MCP 管理页定时刷新外部 MCP 状态（感知后台断连/自动重连）
let externalMcpPollTimer = null;
const EXTERNAL_MCP_POLL_INTERVAL_MS = 8000;
let externalMcpRenderSignature = '';

function renderExternalMCPData(data, forceRender = false) {
    const servers = data.servers || {};
    const stats = data.stats || {};
    const signature = JSON.stringify({ servers, stats });

    if (!forceRender && signature === externalMcpRenderSignature) {
        updateExternalMcpCardSelection();
        return false;
    }

    externalMcpRenderSignature = signature;
    renderExternalMCPList(servers);
    renderExternalMCPStats(stats);
    return true;
}

function startExternalMcpPoll() {
    stopExternalMcpPoll();
    externalMcpPollTimer = setInterval(function () {
        const mcpPage = document.getElementById('page-mcp-management');
        if (!mcpPage || !mcpPage.classList.contains('active')) {
            stopExternalMcpPoll();
            return;
        }
        if (document.hidden) {
            return;
        }
        loadExternalMCPs().catch(function () { /* ignore */ });
    }, EXTERNAL_MCP_POLL_INTERVAL_MS);
}

function stopExternalMcpPoll() {
    if (externalMcpPollTimer) {
        clearInterval(externalMcpPollTimer);
        externalMcpPollTimer = null;
    }
}

// 加载外部MCP列表并渲染
async function loadExternalMCPs(options = {}) {
    try {
        // 等待 i18n 就绪，避免快速刷新时翻译函数未初始化导致显示占位符
        if (window.i18nReady) await window.i18nReady;
        const data = await fetchExternalMCPs();
        renderExternalMCPData(data, options.forceRender === true);
    } catch (error) {
        console.error('加载外部MCP列表失败:', error);
        const list = document.getElementById('external-mcp-list');
        if (list) {
            const errT = typeof window.t === 'function' ? window.t : (k) => k;
        list.innerHTML = `<div class="error">${escapeHtml(errT('mcp.loadExternalMCPFailed'))}: ${escapeHtml(error.message)}</div>`;
        }
    }
}

async function reloadMcpToolsAfterExternalChange(refreshExternal = false) {
    if (typeof loadToolsList === 'function') {
        const page = (toolsPagination && toolsPagination.page) ? toolsPagination.page : 1;
        await loadToolsList(page, toolsSearchKeyword, { refreshExternal });
    }
}

// 轮询列表直到指定 MCP 的工具数量已更新（每秒拉一次，拿到即停，无固定延迟）
// name 为 null 时仅按 maxAttempts 次数轮询，不判断 tool_count
async function pollExternalMCPToolCount(name, maxAttempts = 10) {
    const pollIntervalMs = 1000;
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
        await new Promise(r => setTimeout(r, pollIntervalMs));
        try {
            const data = await fetchExternalMCPs();
            renderExternalMCPData(data);
            if (name != null) {
                const server = data.servers && data.servers[name];
                if (server && server.tool_count > 0) break;
            }
        } catch (e) {
            console.warn('轮询工具数量失败:', e);
        }
    }
    await reloadMcpToolsAfterExternalChange(true);
    if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
        window.refreshMentionTools();
    }
}

// 渲染外部MCP列表
function renderExternalMCPList(servers) {
    const list = document.getElementById('external-mcp-list');
    if (!list) return;
    const layout = document.querySelector('.mcp-management-layout');
    
    if (Object.keys(servers).length === 0) {
        if (layout) layout.classList.add('external-empty');
        const emptyT = typeof window.t === 'function' ? window.t : (k) => k;
        list.innerHTML = '<div class="empty">📋 ' + emptyT('mcp.noExternalMCP') + '<br><span style="font-size: 0.875rem; margin-top: 8px; display: block;">' + emptyT('mcp.clickToAddExternal') + '</span></div>';
        return;
    }
    if (layout) layout.classList.remove('external-empty');
    
    let html = '<div class="external-mcp-items">';
    for (const [name, server] of Object.entries(servers)) {
        const status = server.status || 'disconnected';
        const statusClass = status === 'connected' ? 'status-connected' : 
                           status === 'connecting' ? 'status-connecting' :
                           status === 'error' ? 'status-error' :
                           status === 'disabled' ? 'status-disabled' : 'status-disconnected';
        const statusT = typeof window.t === 'function' ? window.t : (k) => k;
        const statusText = status === 'connected' ? statusT('mcp.connected') : 
                          status === 'connecting' ? statusT('mcp.connecting') :
                          status === 'error' ? statusT('mcp.connectionFailed') :
                          status === 'disabled' ? statusT('mcp.disabled') : statusT('mcp.disconnected');
        const transport = server.config.type || server.config.transport || (server.config.command ? 'stdio' : 'http');
        const transportIcon = transport === 'stdio' ? '⚙️' : '🌐';
        
        const hasTools = server.tool_count !== undefined && server.tool_count > 0;
        const cardClickTitle = hasTools
            ? escapeHtml(statusT('mcp.clickToViewTools', { name }))
            : '';
        const cardClass = hasTools ? 'external-mcp-item clickable' : 'external-mcp-item';
        const selectedClass = toolsExternalMcpFilter === name ? ' selected' : '';

        html += `
            <div class="${cardClass}${selectedClass}" data-mcp-name="${settingsEscapeAttr(name)}"${hasTools ? ` onclick="scrollToExternalMCPTools(${settingsEscapeJsStringAttr(name)}, event)" title="${settingsEscapeAttr(cardClickTitle)}"` : ''}>
                <div class="external-mcp-item-header">
                    <div class="external-mcp-item-info">
                        <h4>${transportIcon} ${escapeHtml(name)}${server.tool_count !== undefined && server.tool_count > 0 ? `<span class="tool-count-badge" title="${escapeHtml(statusT('mcp.toolCount'))}">🔧 ${server.tool_count}</span>` : ''}</h4>
                        <span class="external-mcp-status ${statusClass}">${statusText}</span>
                    </div>
                    <div class="external-mcp-item-actions">
                        ${status === 'connected' || status === 'disconnected' || status === 'error' || status === 'disabled' ?
                            `<button class="btn-small" id="btn-toggle-${settingsEscapeAttr(name)}" onclick="toggleExternalMCP(${settingsEscapeJsStringAttr(name)}, ${settingsEscapeJsStringAttr(status)})" title="${settingsEscapeAttr(status === 'connected' ? statusT('mcp.stopConnection') : statusT('mcp.startConnection'))}">
                                ${status === 'connected' ? '⏸ ' + statusT('mcp.stop') : '▶ ' + statusT('mcp.start')}
                            </button>` :
                            status === 'connecting' ?
                            `<button class="btn-small" id="btn-toggle-${settingsEscapeAttr(name)}" disabled style="opacity: 0.6; cursor: not-allowed;">
                                ⏳ ${statusT('mcp.connecting')}
                            </button>` : ''}
                        <button class="btn-small" onclick="editExternalMCP(${settingsEscapeJsStringAttr(name)})" title="${settingsEscapeAttr(statusT('mcp.editConfig'))}" ${status === 'connecting' ? 'disabled' : ''}>✏️ ${statusT('common.edit')}</button>
                        <button class="btn-small btn-danger" onclick="deleteExternalMCP(${settingsEscapeJsStringAttr(name)})" title="${settingsEscapeAttr(statusT('mcp.deleteConfig'))}" ${status === 'connecting' ? 'disabled' : ''}>🗑 ${statusT('common.delete')}</button>
                    </div>
                </div>
                ${(status === 'error' || status === 'disconnected') && server.error ? `
                <div class="external-mcp-error" style="margin: 12px 0; padding: 12px; background: ${status === 'error' ? '#fee' : '#fff8e6'}; border-left: 3px solid ${status === 'error' ? '#f44' : '#e6a700'}; border-radius: 4px; color: ${status === 'error' ? '#c33' : '#8a6d00'}; font-size: 0.875rem;">
                    <strong>${status === 'error' ? '❌' : '⚠️'} ${statusT('mcp.connectionErrorLabel')}</strong>${escapeHtml(server.error)}
                </div>` : ''}
                <div class="external-mcp-item-details">
                    <div>
                        <strong>${statusT('mcp.transportMode')}</strong>
                        <span>${transportIcon} ${escapeHtml(transport.toUpperCase())}</span>
                    </div>
                    ${server.tool_count !== undefined && server.tool_count > 0 ? `
                    <div>
                        <strong>${statusT('mcp.toolCount')}</strong>
                        <span style="font-weight: 600; color: var(--accent-color);">${statusT('mcp.toolsCountValue', { count: server.tool_count })}</span>
                    </div>` : server.tool_count === 0 && status === 'connected' ? `
                    <div>
                        <strong>${statusT('mcp.toolCount')}</strong>
                        <span style="color: var(--text-muted);">${statusT('mcp.noTools')}</span>
                    </div>` : ''}
                    ${server.config.description ? `
                    <div>
                        <strong>${statusT('mcp.description')}</strong>
                        <span>${escapeHtml(server.config.description)}</span>
                    </div>` : ''}
                    ${server.config.timeout ? `
                    <div>
                        <strong>${statusT('mcp.timeout')}</strong>
                        <span>${server.config.timeout} ${statusT('mcp.secondsUnit')}</span>
                    </div>` : ''}
                    ${transport === 'stdio' && server.config.command ? `
                    <div>
                        <strong>${statusT('mcp.command')}</strong>
                        <span style="font-family: monospace; font-size: 0.8125rem;">${escapeHtml(server.config.command)}</span>
                    </div>` : ''}
                    ${transport === 'http' && server.config.url ? `
                    <div>
                        <strong>${statusT('mcp.urlLabel')}</strong>
                        <span style="font-family: monospace; font-size: 0.8125rem; word-break: break-all;">${escapeHtml(server.config.url)}</span>
                    </div>` : ''}
                </div>
            </div>
        `;
    }
    html += '</div>';
    list.innerHTML = html;
    updateExternalMcpCardSelection();
}

// 渲染外部MCP统计信息
function renderExternalMCPStats(stats) {
    const statsEl = document.getElementById('external-mcp-stats');
    if (!statsEl) return;
    
    const total = stats.total || 0;
    const enabled = stats.enabled || 0;
    const disabled = stats.disabled || 0;
    const connected = stats.connected || 0;
    
    const statsT = typeof window.t === 'function' ? window.t : (k) => k;
    statsEl.innerHTML = `
        <span title="${statsT('mcp.totalCount')}">📊 ${statsT('mcp.totalCount')}: <strong>${total}</strong></span>
        <span title="${statsT('mcp.enabledCount')}">✅ ${statsT('mcp.enabledCount')}: <strong>${enabled}</strong></span>
        <span title="${statsT('mcp.disabledCount')}">⏸ ${statsT('mcp.disabledCount')}: <strong>${disabled}</strong></span>
        <span title="${statsT('mcp.connectedCount')}">🔗 ${statsT('mcp.connectedCount')}: <strong>${connected}</strong></span>
    `;
}

// 显示添加外部MCP模态框
function showAddExternalMCPModal() {
    if (typeof requirePermission === 'function' && !requirePermission('mcp:write')) return;
    currentEditingMCPName = null;
    document.getElementById('external-mcp-modal-title').textContent = (typeof window.t === 'function' ? window.t('mcp.addExternalMCP') : '添加外部MCP');
    document.getElementById('external-mcp-json').value = '';
    document.getElementById('external-mcp-json-error').style.display = 'none';
    document.getElementById('external-mcp-json-error').textContent = '';
    document.getElementById('external-mcp-json').classList.remove('error');
    openAppModal('external-mcp-modal');
}

// 关闭外部MCP模态框
function closeExternalMCPModal() {
    closeAppModal('external-mcp-modal');
    currentEditingMCPName = null;
}

// 编辑外部MCP
async function editExternalMCP(name) {
    try {
        currentEditingMCPName = name;
        document.getElementById('external-mcp-modal-title').textContent = (typeof window.t === 'function' ? window.t('mcp.editExternalMCP') : '编辑外部MCP');
        document.getElementById('external-mcp-json').value = '';
        document.getElementById('external-mcp-json-error').style.display = 'none';
        document.getElementById('external-mcp-json-error').textContent = '';
        document.getElementById('external-mcp-json').classList.remove('error');
        openAppModal('external-mcp-modal', { focus: false });
        const response = await apiFetch(`/api/external-mcp/${encodeURIComponent(name)}`);
        if (!response.ok) {
            throw new Error(typeof window.t === 'function' ? window.t('mcp.getConfigFailed') : '获取外部MCP配置失败');
        }
        const server = await response.json();
        const config = { ...server.config };
        delete config.tool_count;
        delete config.external_mcp_enable;
        const configObj = {};
        configObj[name] = config;
        const jsonStr = JSON.stringify(configObj, null, 2);
        deferModalContent(() => {
            document.getElementById('external-mcp-json').value = jsonStr;
            document.getElementById('external-mcp-json')?.focus();
        });
    } catch (error) {
        closeExternalMCPModal();
        console.error('编辑外部MCP失败:', error);
        alert((typeof window.t === 'function' ? window.t('mcp.operationFailed') : '编辑失败') + ': ' + error.message);
    }
}

// 格式化JSON
function formatExternalMCPJSON() {
    const jsonTextarea = document.getElementById('external-mcp-json');
    const errorDiv = document.getElementById('external-mcp-json-error');
    
    try {
        const jsonStr = jsonTextarea.value.trim();
        if (!jsonStr) {
            errorDiv.textContent = (typeof window.t === 'function' ? window.t('mcp.jsonEmpty') : 'JSON不能为空');
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
        
        const parsed = JSON.parse(jsonStr);
        const formatted = JSON.stringify(parsed, null, 2);
        jsonTextarea.value = formatted;
        errorDiv.style.display = 'none';
        jsonTextarea.classList.remove('error');
    } catch (error) {
        errorDiv.textContent = (typeof window.t === 'function' ? window.t('mcp.jsonError') : 'JSON格式错误') + ': ' + error.message;
        errorDiv.style.display = 'block';
        jsonTextarea.classList.add('error');
    }
}

// 加载示例
function loadExternalMCPExample() {
    const example = {
        "my-stdio-server": {
            command: "python3",
            args: [
                "${HOME}/mcp-servers/main.py",
                "--port",
                "${MCP_PORT:-3000}"
            ],
            env: {
                "API_KEY": "${API_KEY}",
                "LOG_LEVEL": "${LOG_LEVEL:-INFO}"
            },
            timeout: 300
        },
        "my-http-server": {
            type: "http",
            url: "https://mcp.example.com/mcp",
            headers: {
                "Authorization": "Bearer ${MCP_TOKEN}"
            }
        },
        "my-sse-server": {
            type: "sse",
            url: "http://127.0.0.1:8081/mcp/sse"
        }
    };
    
    document.getElementById('external-mcp-json').value = JSON.stringify(example, null, 2);
    document.getElementById('external-mcp-json-error').style.display = 'none';
    document.getElementById('external-mcp-json').classList.remove('error');
}

// 保存外部MCP
async function saveExternalMCP() {
    if (typeof requirePermission === 'function' && !requirePermission('mcp:write')) return;
    const jsonTextarea = document.getElementById('external-mcp-json');
    const jsonStr = jsonTextarea.value.trim();
    const errorDiv = document.getElementById('external-mcp-json-error');
    
    if (!jsonStr) {
        errorDiv.textContent = (typeof window.t === 'function' ? window.t('mcp.jsonEmpty') : 'JSON不能为空');
        errorDiv.style.display = 'block';
        jsonTextarea.classList.add('error');
        jsonTextarea.focus();
        return;
    }
    
    let configObj;
    try {
        configObj = JSON.parse(jsonStr);
    } catch (error) {
        errorDiv.textContent = (typeof window.t === 'function' ? window.t('mcp.jsonError') : 'JSON格式错误') + ': ' + error.message;
        errorDiv.style.display = 'block';
        jsonTextarea.classList.add('error');
        jsonTextarea.focus();
        return;
    }
    
    const t = (typeof window.t === 'function' ? window.t : function (k, opts) { return k; });
    // 验证必须是对象格式
    if (typeof configObj !== 'object' || Array.isArray(configObj) || configObj === null) {
        errorDiv.textContent = t('mcp.configMustBeObject');
        errorDiv.style.display = 'block';
        jsonTextarea.classList.add('error');
        return;
    }
    
    // 获取所有配置名称
    const names = Object.keys(configObj);
    if (names.length === 0) {
        errorDiv.textContent = t('mcp.configNeedOne');
        errorDiv.style.display = 'block';
        jsonTextarea.classList.add('error');
        return;
    }
    
    // 验证每个配置
    for (const name of names) {
        if (!name || name.trim() === '') {
            errorDiv.textContent = t('mcp.configNameEmpty');
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
        
        const config = configObj[name];
        if (typeof config !== 'object' || Array.isArray(config) || config === null) {
            errorDiv.textContent = t('mcp.configMustBeObj', { name: name });
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
        
        // 移除 external_mcp_enable 字段（由按钮控制，但保留 enabled/disabled 用于向后兼容）
        delete config.external_mcp_enable;
        
        // 验证配置内容（同时支持官方 type 字段和旧版 transport 字段）
        const transport = config.type || config.transport || (config.command ? 'stdio' : config.url ? 'http' : '');
        if (!transport) {
            errorDiv.textContent = t('mcp.configNeedCommand', { name: name });
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
        
        if (transport === 'stdio' && !config.command) {
            errorDiv.textContent = t('mcp.configStdioNeedCommand', { name: name });
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
        
        if (transport === 'http' && !config.url) {
            errorDiv.textContent = t('mcp.configHttpNeedUrl', { name: name });
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
        
        if (transport === 'sse' && !config.url) {
            errorDiv.textContent = t('mcp.configSseNeedUrl', { name: name });
            errorDiv.style.display = 'block';
            jsonTextarea.classList.add('error');
            return;
        }
    }
    
    // 清除错误提示
    errorDiv.style.display = 'none';
    jsonTextarea.classList.remove('error');
    
    try {
        // 如果是编辑模式，只更新当前编辑的配置
        if (currentEditingMCPName) {
            if (!configObj[currentEditingMCPName]) {
                errorDiv.textContent = (typeof window.t === 'function' ? window.t('mcp.configEditMustContainName', { name: currentEditingMCPName }) : '配置错误: 编辑模式下，JSON必须包含配置名称 "' + currentEditingMCPName + '"');
                errorDiv.style.display = 'block';
                jsonTextarea.classList.add('error');
                return;
            }
            
            const response = await apiFetch(`/api/external-mcp/${encodeURIComponent(currentEditingMCPName)}`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ config: configObj[currentEditingMCPName] }),
            });
            
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || '保存失败');
            }
        } else {
            // 添加模式：保存所有配置
            for (const name of names) {
                const config = configObj[name];
                const response = await apiFetch(`/api/external-mcp/${encodeURIComponent(name)}`, {
                    method: 'PUT',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({ config }),
                });
                
                if (!response.ok) {
                    const error = await response.json();
                    throw new Error(`保存 "${name}" 失败: ${error.error || '未知错误'}`);
                }
            }
        }
        
        closeExternalMCPModal();
        await loadExternalMCPs();
        if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
            window.refreshMentionTools();
        }
        // 轮询几次以拉取后端异步更新的工具数量（无固定延迟，拿到即停）
        pollExternalMCPToolCount(null, 5);
        alert(typeof window.t === 'function' ? window.t('mcp.saveSuccess') : '保存成功');
    } catch (error) {
        console.error('保存外部MCP失败:', error);
        errorDiv.textContent = (typeof window.t === 'function' ? window.t('mcp.operationFailed') : '保存失败') + ': ' + error.message;
        errorDiv.style.display = 'block';
        jsonTextarea.classList.add('error');
    }
}

// 删除外部MCP
async function deleteExternalMCP(name) {
    if (!confirm((typeof window.t === 'function' ? window.t('mcp.deleteExternalConfirm', { name: name }) : `确定要删除外部MCP "${name}" 吗？`))) {
        return;
    }
    
    try {
        const response = await apiFetch(`/api/external-mcp/${encodeURIComponent(name)}`, {
            method: 'DELETE',
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || '删除失败');
        }
        
        await loadExternalMCPs();
        // 刷新对话界面的工具列表，移除已删除的MCP工具
        if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
            window.refreshMentionTools();
        }
        alert(typeof window.t === 'function' ? window.t('mcp.deleteSuccess') : '删除成功');
    } catch (error) {
        console.error('删除外部MCP失败:', error);
        alert((typeof window.t === 'function' ? window.t('mcp.operationFailed') : '删除失败') + ': ' + error.message);
    }
}

// 切换外部MCP启停
async function toggleExternalMCP(name, currentStatus) {
    const action = currentStatus === 'connected' ? 'stop' : 'start';
    const buttonId = `btn-toggle-${name}`;
    const button = document.getElementById(buttonId);
    
    // 如果是启动操作，显示加载状态
    if (action === 'start' && button) {
        button.disabled = true;
        button.style.opacity = '0.6';
        button.style.cursor = 'not-allowed';
        button.innerHTML = '⏳ 连接中...';
    }
    
    try {
        const response = await apiFetch(`/api/external-mcp/${encodeURIComponent(name)}/${action}`, {
            method: 'POST',
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || '操作失败');
        }
        
        const result = await response.json();
        
        // 如果是启动操作，先立即检查一次状态
        if (action === 'start') {
            // 立即检查一次状态（可能已经连接）
            try {
                const statusResponse = await apiFetch(`/api/external-mcp/${encodeURIComponent(name)}`);
                if (statusResponse.ok) {
                    const statusData = await statusResponse.json();
                    const status = statusData.status || 'disconnected';
                    
                    if (status === 'connected') {
                        await loadExternalMCPs();
                        if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
                            window.refreshMentionTools();
                        }
                        // 轮询直到该 MCP 工具数量已更新（每秒拉一次，无固定延迟）
                        pollExternalMCPToolCount(name, 10);
                        await reloadMcpToolsAfterExternalChange(true);
                        return;
                    }
                }
            } catch (error) {
                console.error('检查状态失败:', error);
            }
            
            // 如果还未连接，开始轮询
            await pollExternalMCPStatus(name, 30); // 最多轮询30次（约30秒）
        } else {
            // 停止操作，直接刷新
            await loadExternalMCPs();
            await reloadMcpToolsAfterExternalChange(false);
            // 刷新对话界面的工具列表
            if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
                window.refreshMentionTools();
            }
        }
    } catch (error) {
        console.error('切换外部MCP状态失败:', error);
        alert((typeof window.t === 'function' ? window.t('mcp.operationFailed') : '操作失败') + ': ' + error.message);
        
        // 恢复按钮状态
        if (button) {
            button.disabled = false;
            button.style.opacity = '1';
            button.style.cursor = 'pointer';
            button.innerHTML = '▶ 启动';
        }
        
        // 刷新状态
        await loadExternalMCPs();
        // 刷新对话界面的工具列表
        if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
            window.refreshMentionTools();
        }
    }
}

// 轮询外部MCP状态
async function pollExternalMCPStatus(name, maxAttempts = 30) {
    let attempts = 0;
    const pollInterval = 1000; // 1秒轮询一次
    
    while (attempts < maxAttempts) {
        await new Promise(resolve => setTimeout(resolve, pollInterval));
        
        try {
            const response = await apiFetch(`/api/external-mcp/${encodeURIComponent(name)}`);
            if (response.ok) {
                const data = await response.json();
                const status = data.status || 'disconnected';
                
                // 更新按钮状态
                const buttonId = `btn-toggle-${name}`;
                const button = document.getElementById(buttonId);
                
                if (status === 'connected') {
                    await loadExternalMCPs();
                    if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
                        window.refreshMentionTools();
                    }
                    // 轮询直到该 MCP 工具数量已更新（每秒拉一次，无固定延迟）
                    pollExternalMCPToolCount(name, 10);
                    await reloadMcpToolsAfterExternalChange(true);
                    return;
                } else if (status === 'error' || status === 'disconnected') {
                    // 连接失败，刷新列表并显示错误
                    await loadExternalMCPs();
                    // 刷新对话界面的工具列表
                    if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
                        window.refreshMentionTools();
                    }
                    if (status === 'error') {
                        alert(typeof window.t === 'function' ? window.t('mcp.connectionFailedCheck') : '连接失败，请检查配置和网络连接');
                    }
                    return;
                } else if (status === 'connecting') {
                    // 仍在连接中，继续轮询
                    attempts++;
                    continue;
                }
            }
        } catch (error) {
            console.error('轮询状态失败:', error);
        }
        
        attempts++;
    }
    
    // 超时，刷新列表
    await loadExternalMCPs();
    // 刷新对话界面的工具列表
    if (typeof window !== 'undefined' && typeof window.refreshMentionTools === 'function') {
        window.refreshMentionTools();
    }
    alert(typeof window.t === 'function' ? window.t('mcp.connectionTimeout') : '连接超时，请检查配置和网络连接');
}

// 在打开设置时加载外部MCP列表
const originalOpenSettings = openSettings;
openSettings = async function() {
    await originalOpenSettings();
    await loadExternalMCPs();
};

// 语言切换后重新渲染 MCP 管理页中由 JS 写入的区块（innerHTML 不会随 data-i18n 自动更新）
document.addEventListener('languagechange', function () {
    try {
        const settingsPage = document.getElementById('page-settings');
        if (settingsPage) {
            initSettingsCustomSelects(settingsPage);
            refreshSettingsCustomSelects();
        }
        const mcpPage = document.getElementById('page-mcp-management');
        if (mcpPage && mcpPage.classList.contains('active')) {
            if (typeof loadExternalMCPs === 'function') {
                loadExternalMCPs({ forceRender: true }).catch(function () { /* ignore */ });
            }
            if (typeof updateToolsStats === 'function') {
                updateToolsStats().catch(function () { /* ignore */ });
            }
        }
    } catch (e) {
        console.warn('languagechange MCP refresh failed', e);
    }
});

window.initSettingsCustomSelects = initSettingsCustomSelects;
window.refreshSettingsCustomSelects = refreshSettingsCustomSelects;
window.closeAllSettingsCustomSelects = closeAllSettingsCustomSelects;
