// 角色管理相关功能
function _t(key, opts) {
    if (typeof window.t === 'function') {
        try {
            var translated = window.t(key, opts);
            if (typeof translated === 'string' && translated && translated !== key) {
                return translated;
            }
        } catch (e) { /* ignore */ }
    }
    // i18n 未就绪或词条缺失时避免把 key 暴露给用户（与 zh-CN 默认一致）
    if (key === 'roles.noDescription') return '暂无描述';
    if (key === 'roles.noDescriptionShort') return '无描述';
    if (key === 'roles.defaultRoleDescription') {
        return '默认角色，不额外携带用户提示词，使用默认MCP';
    }
    return key;
}

const ROLE_MODAL_SELECT_IDS = ['role-workflow-id', 'role-workflow-policy'];
const roleModalSelectMap = {};
let roleModalSelectDocBound = false;
const ROLE_FORM_SELECT_CARET = '<svg class="role-form-select-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function closeAllRoleModalSelects() {
    Object.keys(roleModalSelectMap).forEach(function (id) {
        const reg = roleModalSelectMap[id];
        if (!reg || !reg.wrapper) return;
        reg.wrapper.classList.remove('open');
        if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    });
}

function syncRoleModalSelect(selectId) {
    const reg = roleModalSelectMap[selectId];
    if (!reg) return;
    const select = reg.select;
    const dropdown = reg.dropdown;
    const trigger = reg.trigger;
    const valueSpan = trigger.querySelector('.role-form-select-value');

    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'role-form-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        } else {
            item.setAttribute('aria-selected', 'false');
        }
        const check = document.createElement('span');
        check.className = 'role-form-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'role-form-select-label';
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

function syncAllRoleModalSelects() {
    ROLE_MODAL_SELECT_IDS.forEach(syncRoleModalSelect);
}

function enhanceRoleModalSelect(selectId) {
    const select = document.getElementById(selectId);
    if (!select) return;
    const existing = roleModalSelectMap[selectId];
    if (existing && existing.select !== select) {
        delete roleModalSelectMap[selectId];
    }
    if (select.dataset.roleFormCustom === '1') {
        syncRoleModalSelect(selectId);
        return;
    }
    select.dataset.roleFormCustom = '1';
    select.classList.add('role-form-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'role-form-select-ui';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'role-form-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'role-form-select-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', ROLE_FORM_SELECT_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'role-form-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    roleModalSelectMap[selectId] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        const open = wrapper.classList.contains('open');
        closeAllRoleModalSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
        }
    });

    dropdown.addEventListener('click', function (e) {
        const opt = e.target.closest('.role-form-select-option');
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
        syncRoleModalSelect(selectId);
    });

    select.addEventListener('change', function () {
        syncRoleModalSelect(selectId);
    });

    syncRoleModalSelect(selectId);
}

function refreshRoleModalSelects() {
    const modal = document.getElementById('role-modal');
    if (!modal) return;
    Object.keys(roleModalSelectMap).forEach(function (id) {
        if (!document.getElementById(id)) delete roleModalSelectMap[id];
    });
    ROLE_MODAL_SELECT_IDS.forEach(enhanceRoleModalSelect);
    if (!roleModalSelectDocBound) {
        roleModalSelectDocBound = true;
        document.addEventListener('click', closeAllRoleModalSelects);
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllRoleModalSelects();
        });
    }
}

/** 角色配置中的描述：trim，并把误存为 i18n key 的字面量视为空 */
// roles/ 目录种子角色：种子名/描述随语言翻译；用户自改过的角色名/描述按原文保留
const SEED_ROLE_NAME_KEYS = {
    'Default': 'chat.defaultRole',
    '渗透测试': 'roles.seedPentest.name',
    'API安全测试': 'roles.seedApiSecurity.name',
    'Web应用扫描': 'roles.seedWebAppScan.name',
    'Web框架测试': 'roles.seedWebFramework.name',
    '信息收集': 'roles.seedInfoCollect.name',
    '后渗透测试': 'roles.seedPostExploit.name',
    '容器安全': 'roles.seedContainerSec.name',
    '云安全审计': 'roles.seedCloudAudit.name',
    '二进制分析': 'roles.seedBinary.name',
    '数字取证': 'roles.seedForensics.name',
    '综合漏洞扫描': 'roles.seedVulnScan.name',
    'CTF': 'roles.seedCtf.name'
};
const SEED_ROLE_DESC_KEYS = {
    '默认角色，不额外携带用户提示词，使用默认MCP': 'roles.defaultRoleDescription',
    '专业渗透测试专家，全面深入的漏洞检测': 'roles.seedPentest.description',
    'API安全测试专家，专注于API接口安全检测': 'roles.seedApiSecurity.description',
    'Web应用漏洞扫描专家，全面的Web安全检测': 'roles.seedWebAppScan.description',
    'Web框架安全测试专家，专注于Web应用框架漏洞检测': 'roles.seedWebFramework.description',
    '资产发现与信息搜集专家': 'roles.seedInfoCollect.description',
    '后渗透测试专家，权限维持与横向移动': 'roles.seedPostExploit.description',
    '容器与Kubernetes安全专家，容器环境安全检测': 'roles.seedContainerSec.description',
    '云安全审计专家，多云环境安全检测': 'roles.seedCloudAudit.description',
    '二进制分析与利用专家，擅长逆向工程和密码破解': 'roles.seedBinary.description',
    '数字取证与隐写分析专家，文件与内存取证': 'roles.seedForensics.description',
    '综合漏洞扫描专家，多类型漏洞检测': 'roles.seedVulnScan.description',
    'CTF竞赛专家，擅长解题和漏洞利用': 'roles.seedCtf.description'
};

function rolePlainDescription(role) {
    const raw = typeof role.description === 'string' ? role.description.trim() : '';
    if (!raw) return '';
    if (raw === 'roles.noDescription' || raw === 'roles.noDescriptionShort') return '';
    const seedKey = SEED_ROLE_DESC_KEYS[raw];
    if (seedKey) return _t(seedKey);
    return raw;
}

/** 显示用的角色名：种子名随语言翻译，用户自建的角色名按原文保留 */
function roleDisplayName(role) {
    if (role && (role.name === 'Default' || !role.name)) {
        return _t('chat.defaultRole');
    }
    if (role && role.name) {
        const seedNameKey = SEED_ROLE_NAME_KEYS[role.name];
        if (seedNameKey) return _t(seedNameKey);
    }
    return (role && role.name) || '';
}

function initSelectionDetailTooltip() {
    if (window.__selectionDetailTooltipReady) return;
    window.__selectionDetailTooltipReady = true;

    const tooltip = document.createElement('div');
    tooltip.className = 'selection-detail-tooltip';
    tooltip.setAttribute('role', 'tooltip');
    document.body.appendChild(tooltip);

    let activeEl = null;

    function hideTooltip() {
        activeEl = null;
        tooltip.classList.remove('visible', 'placement-left');
    }

    function positionTooltip(el) {
        const text = el && el.getAttribute('data-selection-detail');
        if (!text) {
            hideTooltip();
            return;
        }
        activeEl = el;
        tooltip.textContent = text;
        tooltip.classList.add('visible');

        const rect = el.getBoundingClientRect();
        const tipRect = tooltip.getBoundingClientRect();
        const gap = 12;
        const margin = 12;
        const rightSpace = window.innerWidth - rect.right - gap - margin;
        const placeLeft = rightSpace < tipRect.width && rect.left > tipRect.width + gap + margin;
        const left = placeLeft ? rect.left - tipRect.width - gap : rect.right + gap;
        const top = Math.max(margin, Math.min(rect.top + rect.height / 2 - tipRect.height / 2, window.innerHeight - tipRect.height - margin));

        tooltip.classList.toggle('placement-left', placeLeft);
        tooltip.style.left = left + 'px';
        tooltip.style.top = top + 'px';
    }

    document.addEventListener('mouseover', function (event) {
        const el = event.target && event.target.closest && event.target.closest('[data-selection-detail]');
        if (!el || (event.relatedTarget && el.contains(event.relatedTarget))) return;
        positionTooltip(el);
    });
    document.addEventListener('mouseout', function (event) {
        const el = event.target && event.target.closest && event.target.closest('[data-selection-detail]');
        if (!el || (event.relatedTarget && el.contains(event.relatedTarget))) return;
        hideTooltip();
    });
    document.addEventListener('focusin', function (event) {
        const el = event.target && event.target.closest && event.target.closest('[data-selection-detail]');
        if (el) positionTooltip(el);
    });
    document.addEventListener('focusout', function (event) {
        const el = event.target && event.target.closest && event.target.closest('[data-selection-detail]');
        if (el) hideTooltip();
    });
    window.addEventListener('scroll', hideTooltip, true);
    window.addEventListener('resize', function () {
        if (activeEl) positionTooltip(activeEl);
    });
}

if (typeof document !== 'undefined') {
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initSelectionDetailTooltip);
    } else {
        initSelectionDetailTooltip();
    }
}

let currentRole = localStorage.getItem('currentRole') || '';
let roles = [];
let rolesSearchKeyword = ''; // 角色搜索关键词
let rolesSearchTimeout = null; // 搜索防抖定时器
let allRoleTools = []; // 存储所有工具列表（用于角色工具选择）
// 与 MCP 工具配置共用 localStorage，便于统一运维习惯
function getRoleToolsPageSize() {
    const saved = localStorage.getItem('toolsPageSize');
    const n = saved ? parseInt(saved, 10) : 20;
    return isNaN(n) || n < 1 ? 20 : n;
}
// 本角色关联筛选: '' = 全部, 'role_on' = 本角色已勾选关联, 'role_off' = 本角色未关联
let roleToolsStatusFilter = '';
/** 按角色关联筛选时缓存全量列表（匹配当前搜索），避免翻页丢状态 */
let roleToolsListCacheFull = [];
let roleToolsListCacheSearch = '';
/** 是否使用客户端分页（角色关联筛选模式下为 true） */
let roleToolsClientMode = false;
let roleToolsPagination = {
    page: 1,
    pageSize: getRoleToolsPageSize(),
    total: 0,
    totalPages: 1
};
let roleToolsSearchKeyword = ''; // 工具搜索关键词
let roleToolStateMap = new Map(); // 工具状态映射：toolKey -> { enabled: boolean, ... }
let roleUsesAllTools = false; // 标记角色是否使用所有工具（当没有配置tools时）
let totalEnabledToolsInMCP = 0; // 已启用的工具总数（从MCP管理中获取，从API响应中获取）
// 仅在「无状态筛选、无搜索」的请求结果上更新，供统计条分母使用（避免筛选后 total 变小导致 25/9 这类错误）
let roleToolsStatsGrandTotal = 0; // 工具总条数（与 MCP 列表「全部」一致）
let roleToolsStatsMcpEnabledTotal = 0; // MCP 全局已启用工具数
let roleConfiguredTools = new Set(); // 角色配置的工具列表（用于确定哪些工具应该被选中）

// 对角色列表进行排序：默认角色排在第一个，其他按名称排序
function sortRoles(rolesArray) {
    const sortedRoles = [...rolesArray];
    // 将"默认"角色分离出来
    const defaultRole = sortedRoles.find(r => r.name === 'Default');
    const otherRoles = sortedRoles.filter(r => r.name !== 'Default');
    
    // 其他角色按名称排序，保持固定顺序
    otherRoles.sort((a, b) => {
        const nameA = a.name || '';
        const nameB = b.name || '';
        return nameA.localeCompare(nameB, 'zh-CN');
    });
    
    // 将"默认"角色放在第一个，其他角色按排序后的顺序跟在后面
    const result = defaultRole ? [defaultRole, ...otherRoles] : otherRoles;
    return result;
}

// 加载所有角色
async function loadRoles() {
    if (window.i18nReady && typeof window.i18nReady.then === 'function') {
        try {
            await window.i18nReady;
        } catch (e) { /* ignore */ }
    }
    try {
        const response = await apiFetch('/api/roles');
        if (!response.ok) {
            throw new Error(typeof window.t === 'function' ? window.t('roles.loadFailed') : '加载角色失败');
        }
        const data = await response.json();
        roles = data.roles || [];
        updateRoleSelectorDisplay();
        renderRoleSelectionSidebar(); // 渲染侧边栏角色列表
        return roles;
    } catch (error) {
        console.error('加载角色失败:', error);
        // 提示文案使用 i18n；若此时 i18n 尚未初始化，则回退为可读中文，而不是暴露 key（roles.loadFailed）
        var loadFailedLabel = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('roles.loadFailed')
            : '加载角色失败';
        showNotification(loadFailedLabel + ': ' + error.message, 'error');
        return [];
    }
}

// 处理角色变更
function handleRoleChange(roleName) {
    const oldRole = currentRole;
    currentRole = roleName || '';
    localStorage.setItem('currentRole', currentRole);
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar(); // 更新侧边栏选中状态
    
    // 当角色切换时，如果工具列表已加载，标记为需要重新加载
    // 这样下次触发@工具建议时会使用新的角色重新加载工具列表
    if (oldRole !== currentRole && typeof window !== 'undefined') {
        // 通过设置一个标记来通知chat.js需要重新加载工具列表
        window._mentionToolsRoleChanged = true;
    }
}

// 更新角色选择器显示
function updateRoleSelectorDisplay() {
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    const roleSelectorIcon = document.getElementById('role-selector-icon');
    const roleSelectorText = document.getElementById('role-selector-text');
    
    if (!roleSelectorBtn || !roleSelectorIcon || !roleSelectorText) return;

    let selectedRole;
    if (currentRole && currentRole !== 'Default') {
        selectedRole = roles.find(r => r.name === currentRole);
    } else {
        selectedRole = roles.find(r => r.name === 'Default');
    }

    if (selectedRole) {
        // 使用配置中的图标，如果没有则使用默认图标
        let icon = selectedRole.icon || '🔵';
        // 如果 icon 是 Unicode 转义格式（\U0001F3C6），需要转换为 emoji
        if (icon && typeof icon === 'string') {
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // 如果转换失败，使用默认图标
                    console.warn('转换 icon Unicode 转义失败:', icon, e);
                    icon = '🔵';
                }
            }
        }
        roleSelectorIcon.textContent = icon;
        const isDefaultRole = selectedRole.name === 'Default' || !selectedRole.name;
        const displayName = roleDisplayName(selectedRole);
        // 非默认角色时避免被 i18n 的 data-i18n 覆盖成“默认”
        roleSelectorText.setAttribute('data-i18n-skip-text', isDefaultRole ? 'false' : 'true');
        roleSelectorText.textContent = displayName;
    } else {
        // 默认角色
        roleSelectorText.setAttribute('data-i18n-skip-text', 'false');
        roleSelectorIcon.textContent = '🔵';
        roleSelectorText.textContent = typeof window.t === 'function' ? window.t('chat.defaultRole') : 'Default';
    }
}

// 渲染主内容区域角色选择列表
function renderRoleSelectionSidebar() {
    const roleList = document.getElementById('role-selection-list');
    if (!roleList) return;

    // 清空列表
    roleList.innerHTML = '';

    // 根据角色配置获取图标，如果没有配置则使用默认图标
    function getRoleIcon(role) {
        if (role.icon) {
            // 如果 icon 是 Unicode 转义格式（\U0001F3C6），需要转换为 emoji
            let icon = role.icon;
            // 检查是否是 Unicode 转义格式（可能包含引号）
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // 如果转换失败，使用原值
                    console.warn('转换 icon Unicode 转义失败:', icon, e);
                }
            }
            return icon;
        }
        // 如果没有配置图标，根据角色名称的首字符生成默认图标
        // 使用一些通用的默认图标
        return '👤';
    }
    
    // 对角色进行排序：默认角色第一个，其他按名称排序
    const sortedRoles = sortRoles(roles);
    
    // 只显示已启用的角色
    const enabledSortedRoles = sortedRoles.filter(r => r.enabled !== false);
    
    enabledSortedRoles.forEach(role => {
        const isDefaultRole = role.name === 'Default';
        const isSelected = isDefaultRole ? (currentRole === '' || currentRole === 'Default') : (currentRole === role.name);
        const roleItem = document.createElement('div');
        roleItem.className = 'role-selection-item-main' + (isSelected ? ' selected' : '');
        roleItem.setAttribute('role', 'option');
        roleItem.tabIndex = 0;
        roleItem.onclick = () => {
            selectRole(role.name);
            closeRoleSelectionPanel(); // 选择后自动关闭面板
        };
        roleItem.onkeydown = (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                roleItem.click();
            }
        };
        const icon = getRoleIcon(role);
        
        // 处理默认角色的描述
        const plainDesc = rolePlainDescription(role);
        let description = plainDesc || _t('roles.noDescription');
        if (isDefaultRole && !plainDesc) {
            description = _t('roles.defaultRoleDescription');
        }
        roleItem.setAttribute('data-selection-detail', description);
        
        roleItem.innerHTML = `
            <div class="role-selection-item-icon-main">${icon}</div>
            <div class="role-selection-item-content-main">
                <div class="role-selection-item-name-main">${escapeHtml(roleDisplayName(role))}</div>
                <div class="role-selection-item-description-main">${escapeHtml(description)}</div>
            </div>
            ${isSelected ? '<div class="role-selection-checkmark-main">✓</div>' : ''}
        `;
        roleList.appendChild(roleItem);
    });
}

// 选择角色
function selectRole(roleName) {
    // 将"默认"映射为空字符串（表示默认角色）
    if (roleName === 'Default') {
        roleName = '';
    }
    handleRoleChange(roleName);
    renderRoleSelectionSidebar(); // 重新渲染以更新选中状态
}

function getChatRoleSelectorWrapper() {
    return document.getElementById('role-selector-wrapper')
        || document.getElementById('role-selector-btn')?.closest('.role-selector-wrapper:not(.project-selector-wrapper)');
}

function isRoleSelectionPanelOpen() {
    const panel = document.getElementById('role-selection-panel');
    if (!panel) return false;
    return panel.style.display !== 'none' && panel.style.display !== '';
}

// 切换角色选择面板显示/隐藏
function toggleRoleSelectionPanel() {
    const panel = document.getElementById('role-selection-panel');
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    if (!panel) return;
    
    const isHidden = !isRoleSelectionPanelOpen();
    
    if (isHidden) {
        if (typeof closeAgentModePanel === 'function') {
            closeAgentModePanel();
        }
        if (typeof closeChatProjectPanel === 'function') {
            closeChatProjectPanel();
        }
        if (typeof closeChatReasoningPanel === 'function') {
            closeChatReasoningPanel();
        }
        renderRoleSelectionSidebar();
        panel.style.display = 'flex'; // 使用flex布局
        // 添加打开状态的视觉反馈
        if (roleSelectorBtn) {
            roleSelectorBtn.classList.add('active');
            roleSelectorBtn.setAttribute('aria-expanded', 'true');
        }
        
        // 确保面板渲染后再检查位置
        setTimeout(() => {
            const wrapper = getChatRoleSelectorWrapper();
            if (wrapper) {
                const rect = wrapper.getBoundingClientRect();
                const panelHeight = panel.offsetHeight || 400;
                const viewportHeight = window.innerHeight;
                
                // 如果面板顶部超出视窗，滚动到合适位置
                if (rect.top - panelHeight < 0) {
                    const scrollY = window.scrollY + rect.top - panelHeight - 20;
                    window.scrollTo({ top: Math.max(0, scrollY), behavior: 'smooth' });
                }
            }
        }, 10);
    } else {
        closeRoleSelectionPanel();
    }
}

// 关闭角色选择面板（选择角色后自动调用）
function closeRoleSelectionPanel() {
    const panel = document.getElementById('role-selection-panel');
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    if (panel) {
        panel.style.display = 'none';
    }
    if (roleSelectorBtn) {
        roleSelectorBtn.classList.remove('active');
        roleSelectorBtn.setAttribute('aria-expanded', 'false');
    }
}

// 转义HTML
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

// 刷新角色列表
async function refreshRoles() {
    await loadRoles();
    // 检查当前页面是否为角色管理页面
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    // 始终更新侧边栏角色选择列表
    renderRoleSelectionSidebar();
    showNotification(typeof window.t === 'function' ? window.t('roles.refreshed') : '已刷新', 'success');
}

// 渲染角色列表
function renderRolesList() {
    const rolesList = document.getElementById('roles-list');
    if (!rolesList) return;

    // 过滤角色（根据搜索关键词）：种子角色也按翻译后的名/描述匹配
    let filteredRoles = roles;
    if (rolesSearchKeyword) {
        const keyword = rolesSearchKeyword.toLowerCase();
        filteredRoles = roles.filter(role =>
            role.name.toLowerCase().includes(keyword) ||
            rolePlainDescription(role).toLowerCase().includes(keyword) ||
            roleDisplayName(role).toLowerCase().includes(keyword)
        );
    }

    if (filteredRoles.length === 0) {
        rolesList.innerHTML = '<div class="empty-state">' + 
            (rolesSearchKeyword ? _t('roles.noMatchingRoles') : _t('roles.noRoles')) + 
            '</div>';
        return;
    }

    // 对角色进行排序：默认角色第一个，其他按名称排序
    const sortedRoles = sortRoles(filteredRoles);
    
    rolesList.innerHTML = sortedRoles.map(role => {
        const plainDesc = rolePlainDescription(role);
        // 获取角色图标，如果是Unicode转义格式则转换为emoji
        let roleIcon = role.icon || '👤';
        if (roleIcon && typeof roleIcon === 'string') {
            // 检查是否是 Unicode 转义格式（可能包含引号）
            const unicodeMatch = roleIcon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    roleIcon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // 如果转换失败，使用默认图标
                    console.warn('转换 icon Unicode 转义失败:', roleIcon, e);
                    roleIcon = '👤';
                }
            }
        }

        // 获取工具列表显示
        let toolsDisplay = '';
        let toolsCount = 0;
        if (role.name === 'Default') {
            toolsDisplay = _t('roleModal.usingAllTools');
        } else if (role.tools && role.tools.length > 0) {
            toolsCount = role.tools.length;
            // 显示前5个工具名称
            const toolNames = role.tools.slice(0, 5).map(tool => {
                // 如果是外部工具，格式为 external_mcp::tool_name，只显示工具名
                const toolName = tool.includes('::') ? tool.split('::')[1] : tool;
                return escapeHtml(toolName);
            });
            if (toolsCount <= 5) {
                toolsDisplay = toolNames.join(', ');
            } else {
                toolsDisplay = toolNames.join(', ') + _t('roleModal.andNMore', { count: toolsCount });
            }
        } else if (role.mcps && role.mcps.length > 0) {
            toolsCount = role.mcps.length;
            toolsDisplay = _t('roleModal.andNMore', { count: toolsCount });
        } else {
            toolsDisplay = _t('roleModal.usingAllTools');
        }

        return `
        <div class="role-card">
            <div class="role-card-header">
                <h3 class="role-card-title">
                    <span class="role-card-icon">${roleIcon}</span>
                    ${escapeHtml(roleDisplayName(role))}
                </h3>
                <span class="role-card-badge ${role.enabled !== false ? 'enabled' : 'disabled'}">
                    ${role.enabled !== false ? _t('roles.enabled') : _t('roles.disabled')}
                </span>
            </div>
            <div class="role-card-description">${escapeHtml(plainDesc || _t('roles.noDescriptionShort'))}</div>
            <div class="role-card-tools">
                <span class="role-card-tools-label">${_t('roleModal.toolsLabel')}</span>
                <span class="role-card-tools-value">${toolsDisplay}</span>
            </div>
            <div class="role-card-actions">
                <button class="btn-secondary btn-small" onclick="editRole(${escapeJsStringAttr(role.name)})">${_t('common.edit')}</button>
                ${role.name !== 'Default' ? `<button class="btn-secondary btn-small btn-danger" onclick="deleteRole(${escapeJsStringAttr(role.name)})">${_t('common.delete')}</button>` : ''}
            </div>
        </div>
    `;
    }).join('');
}

// 处理角色搜索输入
function handleRolesSearchInput() {
    clearTimeout(rolesSearchTimeout);
    rolesSearchTimeout = setTimeout(() => {
        searchRoles();
    }, 300);
}

// 搜索角色
function searchRoles() {
    const searchInput = document.getElementById('roles-search');
    if (!searchInput) return;
    
    rolesSearchKeyword = searchInput.value.trim();
    const clearBtn = document.getElementById('roles-search-clear');
    if (clearBtn) {
        clearBtn.style.display = rolesSearchKeyword ? 'block' : 'none';
    }
    
    renderRolesList();
}

// 清除角色搜索
function clearRolesSearch() {
    const searchInput = document.getElementById('roles-search');
    if (searchInput) {
        searchInput.value = '';
    }
    rolesSearchKeyword = '';
    const clearBtn = document.getElementById('roles-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    renderRolesList();
}

// 生成工具唯一标识符（与settings.js中的getToolKey保持一致）
function getToolKey(tool) {
    // 如果是外部工具，使用 external_mcp::tool.name 作为唯一标识符
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    // 内置工具直接使用工具名称
    return tool.name;
}

// 将单个工具合并进 roleToolStateMap（与 loadRoleTools 中单条逻辑一致）
function mergeToolIntoRoleStateMap(tool) {
    const toolKey = getToolKey(tool);
    if (!roleToolStateMap.has(toolKey)) {
        let enabled = false;
        if (roleUsesAllTools) {
            enabled = tool.enabled ? true : false;
        } else {
            enabled = roleConfiguredTools.has(toolKey);
        }
        roleToolStateMap.set(toolKey, {
            enabled: enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || '',
            name: tool.name,
            mcpEnabled: tool.enabled
        });
    } else {
        const state = roleToolStateMap.get(toolKey);
        if (roleUsesAllTools && tool.enabled) {
            state.enabled = true;
        }
        state.is_external = tool.is_external || false;
        state.external_mcp = tool.external_mcp || '';
        state.mcpEnabled = tool.enabled;
        if (!state.name || state.name === toolKey.split('::').pop()) {
            state.name = tool.name;
        }
    }
}

function getRoleLinkedForTool(toolKey, tool) {
    if (roleToolStateMap.has(toolKey)) {
        return !!roleToolStateMap.get(toolKey).enabled;
    }
    if (roleUsesAllTools) {
        return tool.enabled !== false;
    }
    return roleConfiguredTools.has(toolKey);
}

function computeRoleLinkFilteredTools() {
    if (!roleToolsListCacheFull.length) {
        return [];
    }
    return roleToolsListCacheFull.filter(tool => {
        const key = getToolKey(tool);
        const linked = getRoleLinkedForTool(key, tool);
        if (roleToolsStatusFilter === 'role_on') {
            return linked;
        }
        if (roleToolsStatusFilter === 'role_off') {
            return !linked;
        }
        return true;
    });
}

async function fetchAllRoleToolsIntoCache(searchKeyword) {
    const pageSize = 100;
    let page = 1;
    const all = [];
    let totalPages = 1;
    do {
        let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
        if (searchKeyword) {
            url += `&search=${encodeURIComponent(searchKeyword)}`;
        }
        const response = await apiFetch(url);
        if (!response.ok) {
            throw new Error(typeof window.t === 'function' ? window.t('roles.fetchToolsFailed') : '获取工具列表失败');
        }
        const result = await response.json();
        const tools = result.tools || [];
        tools.forEach(tool => mergeToolIntoRoleStateMap(tool));
        all.push(...tools);
        totalPages = Math.max(1, result.total_pages || 1);
        page++;
    } while (page <= totalPages);
    roleToolsListCacheFull = all;
    roleToolsStatsGrandTotal = all.length;
    roleToolsStatsMcpEnabledTotal = all.filter(t => t.enabled !== false).length;
    totalEnabledToolsInMCP = roleToolsStatsMcpEnabledTotal;
}

// 保存当前页的工具状态到全局映射
function saveCurrentRolePageToolStates() {
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            const toolName = item.dataset.toolName;
            const isExternal = item.dataset.isExternal === 'true';
            const externalMcp = item.dataset.externalMcp || '';
            const existingState = roleToolStateMap.get(toolKey);
            roleToolStateMap.set(toolKey, {
                enabled: checkbox.checked,
                is_external: isExternal,
                external_mcp: externalMcp,
                name: toolName,
                mcpEnabled: existingState ? existingState.mcpEnabled : true // 保留MCP启用状态
            });
        }
    });
}

// 加载所有工具列表（用于角色工具选择）
async function loadRoleTools(page = 1, searchKeyword = '') {
    try {
        // 在加载新页面之前，先保存当前页的状态到全局映射
        saveCurrentRolePageToolStates();

        const pageSize = roleToolsPagination.pageSize;
        const needRoleLinkFilter =
            roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off';

        if (needRoleLinkFilter) {
            roleToolsClientMode = true;
            const searchChanged = searchKeyword !== roleToolsListCacheSearch;
            if (searchChanged || roleToolsListCacheFull.length === 0) {
                await fetchAllRoleToolsIntoCache(searchKeyword);
                roleToolsListCacheSearch = searchKeyword;
            }
            const filtered = computeRoleLinkFilteredTools();
            const total = filtered.length;
            let totalPages = Math.max(1, Math.ceil(total / pageSize) || 1);
            let p = page;
            if (p > totalPages) {
                p = totalPages;
            }
            if (p < 1) {
                p = 1;
            }
            roleToolsPagination = {
                page: p,
                pageSize,
                total,
                totalPages
            };
            allRoleTools = filtered.slice((p - 1) * pageSize, p * pageSize);
        } else {
            roleToolsClientMode = false;
            roleToolsListCacheFull = [];
            roleToolsListCacheSearch = '';

            let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            if (searchKeyword) {
                url += `&search=${encodeURIComponent(searchKeyword)}`;
            }

            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error(typeof window.t === 'function' ? window.t('roles.fetchToolsFailed') : '获取工具列表失败');
            }

            const result = await response.json();
            allRoleTools = result.tools || [];
            roleToolsPagination = {
                page: result.page || page,
                pageSize: result.page_size || pageSize,
                total: result.total || 0,
                totalPages: result.total_pages || 1
            };

            if (roleToolsStatusFilter === '' && !searchKeyword) {
                roleToolsStatsGrandTotal = result.total || 0;
                if (result.total_enabled !== undefined) {
                    roleToolsStatsMcpEnabledTotal = result.total_enabled;
                    totalEnabledToolsInMCP = result.total_enabled;
                }
            }

            allRoleTools.forEach(tool => mergeToolIntoRoleStateMap(tool));
        }

        renderRoleToolsList();
        renderRoleToolsPagination();
        updateRoleToolsStats();
    } catch (error) {
        console.error('加载工具列表失败:', error);
        const toolsList = document.getElementById('role-tools-list');
        if (toolsList) {
            toolsList.innerHTML = `<div class="tools-error">${_t('roleModal.loadToolsFailed')}: ${escapeHtml(error.message)}</div>`;
        }
    }
}

// 渲染角色工具选择列表
function renderRoleToolsList() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // 清除加载提示和旧内容
    toolsList.innerHTML = '';

    if (roleToolsStatusFilter === 'role_on') {
        const banner = document.createElement('div');
        banner.className = 'role-tools-filter-banner role-tools-filter-banner-on';
        banner.setAttribute('role', 'status');
        banner.textContent = _t('roleModal.roleFilterOnBanner');
        toolsList.appendChild(banner);
    } else if (roleToolsStatusFilter === 'role_off') {
        const banner = document.createElement('div');
        banner.className = 'role-tools-filter-banner role-tools-filter-banner-off';
        banner.setAttribute('role', 'status');
        banner.textContent = _t('roleModal.roleFilterOffBanner');
        toolsList.appendChild(banner);
    }
    
    const listContainer = document.createElement('div');
    listContainer.className = 'role-tools-list-items';
    listContainer.innerHTML = '';
    
    if (allRoleTools.length === 0) {
        listContainer.innerHTML = '<div class="tools-empty">' + _t('roleModal.noTools') + '</div>';
        toolsList.appendChild(listContainer);
        return;
    }

    const chkTitle = escapeAttr(_t('roleModal.checkboxLinkTitle'));
    
    allRoleTools.forEach(tool => {
        const toolKey = getToolKey(tool);
        const toolItem = document.createElement('div');
        toolItem.className = 'role-tool-item';
        toolItem.dataset.toolKey = toolKey;
        toolItem.dataset.toolName = tool.name;
        toolItem.dataset.isExternal = tool.is_external ? 'true' : 'false';
        toolItem.dataset.externalMcp = tool.external_mcp || '';
        
        // 从状态映射获取工具状态
        const toolState = roleToolStateMap.get(toolKey) || {
            enabled: tool.enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || ''
        };
        
        // 外部工具标签
        let externalBadge = '';
        if (toolState.is_external || tool.is_external) {
            const externalMcpName = toolState.external_mcp || tool.external_mcp || '';
            const badgeText = externalMcpName
                ? (typeof window.t === 'function' ? window.t('roles.externalBadgeWithName', { name: escapeHtml(externalMcpName) }) : `外部 (${escapeHtml(externalMcpName)})`)
                : (typeof window.t === 'function' ? window.t('roles.externalBadge') : '外部');
            const badgeTitle = externalMcpName
                ? (typeof window.t === 'function' ? window.t('roles.externalToolBadgeTitleSource', { name: escapeHtml(externalMcpName) }) : `外部MCP工具 - 来源：${escapeHtml(externalMcpName)}`)
                : (typeof window.t === 'function' ? window.t('roles.externalToolBadgeTitle') : '外部MCP工具');
            externalBadge = `<span class="external-tool-badge" title="${escapeAttr(badgeTitle)}">${badgeText}</span>`;
        }
        let mcpDisabledBadge = '';
        if (tool.enabled === false) {
            mcpDisabledBadge = `<span class="role-tool-mcp-disabled-badge" title="${escapeHtml(_t('roleModal.mcpDisabledBadgeTitle'))}">${escapeHtml(_t('roleModal.mcpDisabledBadge'))}</span>`;
        }
        // 生成唯一的checkbox id
        const checkboxId = `role-tool-${escapeAttr(toolKey).replace(/::/g, '--')}`;
        
        toolItem.innerHTML = `
            <input type="checkbox" id="${checkboxId}" ${toolState.enabled ? 'checked' : ''} 
                   title="${chkTitle}" aria-label="${chkTitle}"
                   onchange="handleRoleToolCheckboxChange(${escapeJsStringAttr(toolKey)}, this.checked)" />
            <div class="role-tool-item-info">
                <div class="role-tool-item-name">
                    ${escapeHtml(tool.name)}
                    ${externalBadge}
                    ${mcpDisabledBadge}
                </div>
                <div class="role-tool-item-desc">${escapeHtml(tool.description || (typeof window.t === 'function' ? window.t('roles.noDescriptionShort') : '无描述'))}</div>
            </div>
        `;
        listContainer.appendChild(toolItem);
    });
    
    toolsList.appendChild(listContainer);
}

// 渲染工具列表分页控件（始终展示范围与每页条数，便于在仅一页时仍可调整 page size）
function renderRoleToolsPagination() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // 移除旧的分页控件
    const oldPagination = toolsList.querySelector('.role-tools-pagination');
    if (oldPagination) {
        oldPagination.remove();
    }
    
    const pagination = document.createElement('div');
    pagination.className = 'role-tools-pagination';
    
    const { page, totalPages, total, pageSize } = roleToolsPagination;
    const startItem = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const endItem = total === 0 ? 0 : Math.min(page * pageSize, total);
    const savedPageSize = getRoleToolsPageSize();
    const perPageLabel = typeof window.t === 'function' ? window.t('mcp.perPage') : '每页';
    
    const paginationShowText = _t('roleModal.paginationShow', { start: startItem, end: endItem, total: total }) +
        (roleToolsSearchKeyword ? _t('roleModal.paginationSearch', { keyword: roleToolsSearchKeyword }) : '');
    const navDisabled = total === 0 || totalPages <= 1;
    pagination.innerHTML = `
        <div class="pagination-info">${paginationShowText}</div>
        <div class="pagination-page-size">
            <label for="role-tools-page-size-pagination">${escapeHtml(perPageLabel)}</label>
            <select id="role-tools-page-size-pagination" onchange="changeRoleToolsPageSize()">
                <option value="10" ${savedPageSize === 10 ? 'selected' : ''}>10</option>
                <option value="20" ${savedPageSize === 20 ? 'selected' : ''}>20</option>
                <option value="50" ${savedPageSize === 50 ? 'selected' : ''}>50</option>
                <option value="100" ${savedPageSize === 100 ? 'selected' : ''}>100</option>
            </select>
        </div>
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="loadRoleTools(1, ${escapeJsStringAttr(roleToolsSearchKeyword)})" ${page === 1 || navDisabled ? 'disabled' : ''}>${_t('roleModal.firstPage')}</button>
            <button class="btn-secondary" onclick="loadRoleTools(${page - 1}, ${escapeJsStringAttr(roleToolsSearchKeyword)})" ${page === 1 || navDisabled ? 'disabled' : ''}>${_t('roleModal.prevPage')}</button>
            <span class="pagination-page">${_t('roleModal.pageOf', { page: page, total: totalPages })}</span>
            <button class="btn-secondary" onclick="loadRoleTools(${page + 1}, ${escapeJsStringAttr(roleToolsSearchKeyword)})" ${page === totalPages || navDisabled ? 'disabled' : ''}>${_t('roleModal.nextPage')}</button>
            <button class="btn-secondary" onclick="loadRoleTools(${totalPages}, ${escapeJsStringAttr(roleToolsSearchKeyword)})" ${page === totalPages || navDisabled ? 'disabled' : ''}>${_t('roleModal.lastPage')}</button>
        </div>
    `;
    
    toolsList.appendChild(pagination);
    if (typeof initPaginationSelects === 'function') initPaginationSelects(pagination);
}

function syncRoleToolsFilterButtons() {
    const wrap = document.getElementById('role-tools-status-filter');
    if (!wrap) return;
    wrap.querySelectorAll('.btn-filter').forEach(btn => {
        const v = btn.getAttribute('data-filter');
        const filterVal = v === null || v === undefined ? '' : String(v);
        btn.classList.toggle('active', filterVal === roleToolsStatusFilter);
    });
}

function roleToolsListScopeLine() {
    const n = roleToolsPagination.total || 0;
    if (roleToolsStatusFilter === 'role_on') {
        return _t('roleModal.statsListScopeRoleOn', { n: n });
    }
    if (roleToolsStatusFilter === 'role_off') {
        return _t('roleModal.statsListScopeRoleOff', { n: n });
    }
    return _t('roleModal.statsListScopeAll', { n: n });
}

function filterRoleToolsByStatus(status) {
    roleToolsStatusFilter = status;
    syncRoleToolsFilterButtons();
    loadRoleTools(1, roleToolsSearchKeyword);
}

async function changeRoleToolsPageSize() {
    const sel = document.getElementById('role-tools-page-size-pagination');
    if (!sel) return;
    const newPageSize = parseInt(sel.value, 10);
    if (isNaN(newPageSize) || newPageSize < 1) return;
    localStorage.setItem('toolsPageSize', String(newPageSize));
    roleToolsPagination.pageSize = newPageSize;
    await loadRoleTools(1, roleToolsSearchKeyword);
}

// 处理工具checkbox状态变化
function handleRoleToolCheckboxChange(toolKey, enabled) {
    const toolItem = document.querySelector(`.role-tool-item[data-tool-key="${toolKey}"]`);
    if (toolItem) {
        const toolName = toolItem.dataset.toolName;
        const isExternal = toolItem.dataset.isExternal === 'true';
        const externalMcp = toolItem.dataset.externalMcp || '';
        const existingState = roleToolStateMap.get(toolKey);
        roleToolStateMap.set(toolKey, {
            enabled: enabled,
            is_external: isExternal,
            external_mcp: externalMcp,
            name: toolName,
            mcpEnabled: existingState ? existingState.mcpEnabled : true // 保留MCP启用状态
        });
    }
    if (
        roleToolsClientMode &&
        (roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off')
    ) {
        loadRoleTools(roleToolsPagination.page, roleToolsSearchKeyword);
    } else {
        updateRoleToolsStats();
    }
}

// 全选工具
function selectAllRoleTools() {
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                const existingState = roleToolStateMap.get(toolKey);
                // 只选中在MCP管理中已启用的工具
                const shouldEnable = existingState && existingState.mcpEnabled !== false;
                checkbox.checked = shouldEnable;
                roleToolStateMap.set(toolKey, {
                    enabled: shouldEnable,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName,
                    mcpEnabled: existingState ? existingState.mcpEnabled : true
                });
            }
        }
    });
    if (
        roleToolsClientMode &&
        (roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off')
    ) {
        loadRoleTools(roleToolsPagination.page, roleToolsSearchKeyword);
    } else {
        updateRoleToolsStats();
    }
}

// 全不选工具
function deselectAllRoleTools() {
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        checkbox.checked = false;
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                const existingState = roleToolStateMap.get(toolKey);
                roleToolStateMap.set(toolKey, {
                    enabled: false,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName,
                    mcpEnabled: existingState ? existingState.mcpEnabled : true // 保留MCP启用状态
                });
            }
        }
    });
    if (
        roleToolsClientMode &&
        (roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off')
    ) {
        loadRoleTools(roleToolsPagination.page, roleToolsSearchKeyword);
    } else {
        updateRoleToolsStats();
    }
}

// 搜索工具
function searchRoleTools(keyword) {
    roleToolsSearchKeyword = keyword;
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = keyword ? 'block' : 'none';
    }
    loadRoleTools(1, keyword);
}

// 清除搜索
function clearRoleToolsSearch() {
    document.getElementById('role-tools-search').value = '';
    searchRoleTools('');
}

// 更新工具统计信息（口径：分母「可关联上限」= 全库 MCP 已开工具数，与 MCP 管理页筛选「MCP已开」条数一致；勾选=关联本角色）
function updateRoleToolsStats() {
    const statsEl = document.getElementById('role-tools-stats');
    if (!statsEl) return;

    const pageChecked = Array.from(document.querySelectorAll('#role-tools-list input[type="checkbox"]:checked')).length;
    const pageTotal = document.querySelectorAll('#role-tools-list input[type="checkbox"]').length;
    const mcpOnMax =
        (roleToolsStatsMcpEnabledTotal > 0 ? roleToolsStatsMcpEnabledTotal : totalEnabledToolsInMCP) || 0;
    const grandAll =
        (roleToolsStatsGrandTotal > 0 ? roleToolsStatsGrandTotal : roleToolsPagination.total) || 0;
    const scopeLine = roleToolsListScopeLine();

    if (roleUsesAllTools) {
        statsEl.innerHTML = `
            <div class="role-tools-stats-row">
                <span title="${escapeHtml(_t('roleModal.statsPageLinkedTitle'))}">✅ ${_t('roleModal.statsPageLinked', { current: pageChecked, total: pageTotal })}</span>
            </div>
            <div class="role-tools-stats-row">
                <span title="${escapeHtml(_t('roleModal.statsRoleUsesAllTitle'))}">📊 ${_t('roleModal.statsRoleUsesAll', { mcpOn: mcpOnMax, all: grandAll })}</span>
            </div>
            <div class="role-tools-stats-hint">📋 ${escapeHtml(scopeLine)}</div>
        `;
        return;
    }

    let roleLinked = 0;
    roleToolStateMap.forEach(state => {
        if (state.enabled && state.mcpEnabled !== false) {
            roleLinked++;
        }
    });
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const savedState = roleToolStateMap.get(toolKey);
            if (savedState && savedState.enabled !== checkbox.checked && savedState.mcpEnabled !== false) {
                if (checkbox.checked && !savedState.enabled) {
                    roleLinked++;
                } else if (!checkbox.checked && savedState.enabled) {
                    roleLinked--;
                }
            }
        }
    });

    const roleRow =
        mcpOnMax > 0
            ? `<span title="${escapeHtml(_t('roleModal.statsRoleLinkedTitle'))}">📊 ${_t('roleModal.statsRoleLinked', { current: roleLinked, max: mcpOnMax })}</span>`
            : `<span title="${escapeHtml(_t('roleModal.statsRoleLinkedNoMaxTitle'))}">📊 ${_t('roleModal.statsRoleLinkedNoMax', { current: roleLinked })}</span>`;

    statsEl.innerHTML = `
        <div class="role-tools-stats-row">
            <span title="${escapeHtml(_t('roleModal.statsPageLinkedTitle'))}">✅ ${_t('roleModal.statsPageLinked', { current: pageChecked, total: pageTotal })}</span>
        </div>
        <div class="role-tools-stats-row">${roleRow}</div>
        <div class="role-tools-stats-hint">📋 ${escapeHtml(scopeLine)}</div>
    `;
}

// 获取选中的工具列表（返回toolKey数组）
async function getSelectedRoleTools() {
    // 先保存当前页的状态
    saveCurrentRolePageToolStates();
    
    // 如果没有搜索关键词，需要加载所有页面的工具来确保状态映射完整
    // 但为了性能，我们可以只从状态映射中获取已选中的工具
    // 问题是：如果用户只在某些页面选择了工具，其他页面的工具状态可能不在映射中
    
    // 如果总工具数大于已加载的工具数，我们需要确保所有未加载页面的工具也被考虑
    // 但对于角色工具选择，我们只需要获取用户明确选择过的工具
    // 所以直接从状态映射获取已选中的工具即可
    
    // 从状态映射获取所有选中的工具（只返回在MCP管理中已启用的工具）
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        // 只返回在MCP管理中已启用且被角色选中的工具
        if (state.enabled && state.mcpEnabled !== false) {
            selectedTools.push(toolKey);
        }
    });
    
    // 如果用户可能在其他页面选择了工具，我们需要确保当前页的状态也被保存
    // 但状态映射应该已经包含了所有访问过的页面的状态
    
    return selectedTools;
}

// 设置选中的工具（用于编辑角色时）
function setSelectedRoleTools(selectedToolKeys) {
    const selectedSet = new Set(selectedToolKeys || []);
    
    // 更新状态映射
    roleToolStateMap.forEach((state, toolKey) => {
        state.enabled = selectedSet.has(toolKey);
    });
    
    // 更新当前页的checkbox状态
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            checkbox.checked = selectedSet.has(toolKey);
        }
    });
    
    updateRoleToolsStats();
}

// 显示添加角色模态框
async function showAddRoleModal() {
    if (typeof requirePermission === 'function' && !requirePermission('roles:write')) return;
    const modal = document.getElementById('role-modal');
    if (!modal) return;

    document.getElementById('role-modal-title').textContent = _t('roleModal.addRole');
    document.getElementById('role-name').value = '';
    document.getElementById('role-name').disabled = false;
    document.getElementById('role-description').value = '';
    document.getElementById('role-icon').value = '';
    document.getElementById('role-user-prompt').value = '';
    document.getElementById('role-enabled').checked = true;
    if (typeof loadWorkflowOptionsForRoleModal === 'function') {
        await loadWorkflowOptionsForRoleModal('');
    }
    const workflowPolicy = document.getElementById('role-workflow-policy');
    if (workflowPolicy) {
        workflowPolicy.value = 'auto';
    }

    // 添加角色时：显示工具选择界面，隐藏默认角色提示
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (defaultHint) {
        defaultHint.style.display = 'none';
    }
    if (toolsControls) {
        toolsControls.style.display = 'block';
    }
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    if (formHint) {
        formHint.style.display = 'block';
    }

    // 重置工具状态
    roleToolStateMap.clear();
    roleConfiguredTools.clear(); // 清空角色配置的工具列表
    roleUsesAllTools = false; // 添加角色时默认不使用所有工具
    roleToolsSearchKeyword = '';
    const searchInput = document.getElementById('role-tools-search');
    if (searchInput) {
        searchInput.value = '';
    }
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    roleToolsStatusFilter = '';
    syncRoleToolsFilterButtons();
    roleToolsPagination.pageSize = getRoleToolsPageSize();
    
    // 清空工具列表 DOM，避免 loadRoleTools 中的 saveCurrentRolePageToolStates 读取旧状态
    if (toolsList) {
        toolsList.innerHTML = '';
    }

    // 加载并渲染工具列表
    await loadRoleTools(1, '');
    
    // 确保工具列表显示
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    
    // 确保统计信息正确更新（显示0/108）
    updateRoleToolsStats();

    refreshRoleModalSelects();
    openAppModal('role-modal');
}

// 编辑角色
async function editRole(roleName) {
    const role = roles.find(r => r.name === roleName);
    if (!role) {
        showNotification(_t('roleModal.roleNotFound'), 'error');
        return;
    }

    const modal = document.getElementById('role-modal');
    if (!modal) return;

    document.getElementById('role-modal-title').textContent = _t('roleModal.editRole');
    document.getElementById('role-name').value = role.name;
    document.getElementById('role-name').disabled = true; // 编辑时不允许修改名称
    document.getElementById('role-description').value = role.description || '';
    // 处理icon字段：如果是Unicode转义格式，转换为emoji；否则直接使用
    let iconValue = role.icon || '';
    if (iconValue && iconValue.startsWith('\\U')) {
        // 转换Unicode转义格式（如 \U0001F3C6）为emoji
        try {
            const codePoint = parseInt(iconValue.substring(2), 16);
            iconValue = String.fromCodePoint(codePoint);
        } catch (e) {
            // 如果转换失败，使用原值
        }
    }
    document.getElementById('role-icon').value = iconValue;
    document.getElementById('role-user-prompt').value = role.user_prompt || '';
    document.getElementById('role-enabled').checked = role.enabled !== false;
    if (typeof loadWorkflowOptionsForRoleModal === 'function') {
        await loadWorkflowOptionsForRoleModal(role.workflow_id || '');
    }
    const workflowPolicy = document.getElementById('role-workflow-policy');
    if (workflowPolicy) {
        workflowPolicy.value = role.workflow_policy || 'auto';
    }

    // 检查是否为默认角色
    const isDefaultRole = roleName === 'Default';
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (isDefaultRole) {
        // 默认角色：隐藏工具选择界面，显示提示信息
        if (defaultHint) {
            defaultHint.style.display = 'block';
        }
        if (toolsControls) {
            toolsControls.style.display = 'none';
        }
        if (toolsList) {
            toolsList.style.display = 'none';
        }
        if (formHint) {
            formHint.style.display = 'none';
        }
    } else {
        // 非默认角色：显示工具选择界面，隐藏提示信息
        if (defaultHint) {
            defaultHint.style.display = 'none';
        }
        if (toolsControls) {
            toolsControls.style.display = 'block';
        }
        if (toolsList) {
            toolsList.style.display = 'block';
        }
        if (formHint) {
            formHint.style.display = 'block';
        }

        // 重置工具状态
        roleToolStateMap.clear();
        roleConfiguredTools.clear(); // 清空角色配置的工具列表
        roleToolsSearchKeyword = '';
        const searchInput = document.getElementById('role-tools-search');
        if (searchInput) {
            searchInput.value = '';
        }
        const clearBtn = document.getElementById('role-tools-search-clear');
        if (clearBtn) {
            clearBtn.style.display = 'none';
        }
        roleToolsStatusFilter = '';
        syncRoleToolsFilterButtons();
        roleToolsPagination.pageSize = getRoleToolsPageSize();

        // 优先使用tools字段，如果没有则使用mcps字段（向后兼容）
        const selectedTools = role.tools || (role.mcps && role.mcps.length > 0 ? role.mcps : []);
        
        // 判断是否使用所有工具：如果没有配置tools（或tools为空数组），表示使用所有工具
        roleUsesAllTools = !role.tools || role.tools.length === 0;
        
        // 保存角色配置的工具列表
        if (selectedTools.length > 0) {
            selectedTools.forEach(toolKey => {
                roleConfiguredTools.add(toolKey);
            });
        }
        
        // 如果有选中的工具，先初始化状态映射
        if (selectedTools.length > 0) {
            roleUsesAllTools = false; // 有配置工具，不使用所有工具
            // 将选中的工具添加到状态映射（标记为选中）
            selectedTools.forEach(toolKey => {
                // 如果映射中还没有这个工具，先创建一个默认状态（enabled为true）
                if (!roleToolStateMap.has(toolKey)) {
                    roleToolStateMap.set(toolKey, {
                        enabled: true,
                        is_external: false,
                        external_mcp: '',
                        name: toolKey.split('::').pop() || toolKey // 从toolKey中提取工具名称
                    });
                } else {
                    // 如果已存在，更新为选中状态
                    const state = roleToolStateMap.get(toolKey);
                    state.enabled = true;
                }
            });
        }

        // 加载工具列表（第一页）
        await loadRoleTools(1, '');
        
        // 如果使用所有工具，标记当前页所有已启用的工具为选中
        if (roleUsesAllTools) {
            // 标记当前页所有在MCP管理中已启用的工具为选中
            document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
                const toolItem = checkbox.closest('.role-tool-item');
                if (toolItem) {
                    const toolKey = toolItem.dataset.toolKey;
                    const toolName = toolItem.dataset.toolName;
                    const isExternal = toolItem.dataset.isExternal === 'true';
                    const externalMcp = toolItem.dataset.externalMcp || '';
                    if (toolKey) {
                        const state = roleToolStateMap.get(toolKey);
                        // 只选中在MCP管理中已启用的工具
                        // 如果状态存在，使用状态中的 mcpEnabled；否则假设已启用（因为 loadRoleTools 应该已经初始化了所有工具）
                        const shouldEnable = state ? (state.mcpEnabled !== false) : true;
                        checkbox.checked = shouldEnable;
                        if (state) {
                            state.enabled = shouldEnable;
                        } else {
                            // 如果状态不存在，创建新状态（这种情况不应该发生，因为 loadRoleTools 应该已经初始化了）
                            roleToolStateMap.set(toolKey, {
                                enabled: shouldEnable,
                                is_external: isExternal,
                                external_mcp: externalMcp,
                                name: toolName,
                                mcpEnabled: true // 假设已启用，实际值会在loadRoleTools中更新
                            });
                        }
                    }
                }
            });
            // 更新统计信息，确保显示正确的选中数量
            updateRoleToolsStats();
        } else if (selectedTools.length > 0) {
            // 加载完成后，再次设置选中状态（确保当前页的工具也被正确设置）
            setSelectedRoleTools(selectedTools);
        }
    }

    refreshRoleModalSelects();
    openAppModal('role-modal');
}

// 关闭角色模态框
function closeRoleModal() {
    closeAllRoleModalSelects();
    closeAppModal('role-modal');
}

function closeRoleSelectModal() {
    closeAppModal('role-select-modal');
}

// 获取所有选中的工具（包括未在MCP管理中启用的工具）
function getAllSelectedRoleTools() {
    // 先保存当前页的状态
    saveCurrentRolePageToolStates();
    
    // 从状态映射获取所有选中的工具（不管是否在MCP管理中启用）
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        if (state.enabled) {
            selectedTools.push({
                key: toolKey,
                name: state.name || toolKey.split('::').pop() || toolKey,
                mcpEnabled: state.mcpEnabled !== false // mcpEnabled 为 false 时是未启用，其他情况视为已启用
            });
        }
    });
    
    return selectedTools;
}

// 检查并获取未在MCP管理中启用的工具
function getDisabledTools(selectedTools) {
    return selectedTools.filter(tool => {
        const state = roleToolStateMap.get(tool.key);
        // 如果 mcpEnabled 明确为 false，则认为是未启用
        return state && state.mcpEnabled === false;
    });
}

// 加载所有工具到状态映射中（用于从使用全部工具切换到部分工具时）
async function loadAllToolsToStateMap() {
    try {
        const pageSize = 100; // 使用较大的页面大小以减少请求次数
        let page = 1;
        let hasMore = true;
        
        // 遍历所有页面获取所有工具
        while (hasMore) {
            const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error(typeof window.t === 'function' ? window.t('roles.fetchToolsFailed') : '获取工具列表失败');
            }

            const result = await response.json();

            // 将所有工具添加到状态映射中
            result.tools.forEach(tool => {
                const toolKey = getToolKey(tool);
                if (!roleToolStateMap.has(toolKey)) {
                    // 工具不在映射中，根据当前模式初始化
                    let enabled = false;
                    if (roleUsesAllTools) {
                        // 如果使用所有工具，且工具在MCP管理中已启用，则标记为选中
                        enabled = tool.enabled ? true : false;
                    } else {
                        // 如果不使用所有工具，只有工具在角色配置的工具列表中才标记为选中
                        enabled = roleConfiguredTools.has(toolKey);
                    }
                    roleToolStateMap.set(toolKey, {
                        enabled: enabled,
                        is_external: tool.is_external || false,
                        external_mcp: tool.external_mcp || '',
                        name: tool.name,
                        mcpEnabled: tool.enabled // 保存MCP管理中的原始启用状态
                    });
                } else {
                    // 工具已在映射中，更新其他属性但保留enabled状态
                    const state = roleToolStateMap.get(toolKey);
                    state.is_external = tool.is_external || false;
                    state.external_mcp = tool.external_mcp || '';
                    state.mcpEnabled = tool.enabled; // 更新MCP管理中的原始启用状态
                    if (!state.name || state.name === toolKey.split('::').pop()) {
                        state.name = tool.name; // 更新工具名称
                    }
                }
            });
            
            // 检查是否还有更多页面
            if (page >= result.total_pages) {
                hasMore = false;
            } else {
                page++;
            }
        }
    } catch (error) {
        console.error('加载所有工具到状态映射失败:', error);
        throw error;
    }
}

// 保存角色
async function saveRole() {
    if (typeof requirePermission === 'function' && !requirePermission('roles:write')) return;
    const name = document.getElementById('role-name').value.trim();
    if (!name) {
        showNotification(_t('roleModal.roleNameRequired'), 'error');
        return;
    }

    const description = document.getElementById('role-description').value.trim();
    let icon = document.getElementById('role-icon').value.trim();
    // 将emoji转换为Unicode转义格式以匹配YAML格式（如 \U0001F3C6）
    if (icon) {
        // 获取第一个字符的Unicode代码点（处理emoji可能是多个字符的情况）
        const codePoint = icon.codePointAt(0);
        if (codePoint && codePoint > 0x7F) {
            // 转换为8位十六进制格式（\U0001F3C6）
            icon = '\\U' + codePoint.toString(16).toUpperCase().padStart(8, '0');
        }
    }
    const userPrompt = document.getElementById('role-user-prompt').value.trim();
    const enabled = document.getElementById('role-enabled').checked;
    const workflowIdEl = document.getElementById('role-workflow-id');
    const workflowPolicyEl = document.getElementById('role-workflow-policy');
    const workflowId = workflowIdEl ? workflowIdEl.value.trim() : '';
    const workflowPolicy = workflowPolicyEl ? workflowPolicyEl.value.trim() : 'auto';

    const isEdit = document.getElementById('role-name').disabled;
    
    // 检查是否为默认角色
    const isDefaultRole = name === 'Default';
    
    // 检查是否是首次添加角色（排除默认角色后，没有任何用户创建的角色）
    const isFirstUserRole = !isEdit && !isDefaultRole && roles.filter(r => r.name !== 'Default').length === 0;
    
    // 默认角色不保存tools字段（使用所有工具）
    // 非默认角色：如果使用所有工具（roleUsesAllTools为true），也不保存tools字段
    let tools = [];
    let disabledTools = []; // 存储未在MCP管理中启用的工具
    
    if (!isDefaultRole) {
        // 保存当前页的状态
        saveCurrentRolePageToolStates();
        
        // 收集所有选中的工具（包括未在MCP管理中启用的）
        let allSelectedTools = getAllSelectedRoleTools();
        
        // 如果是首次添加角色且没有选择工具，默认使用全部工具
        if (isFirstUserRole && allSelectedTools.length === 0) {
            roleUsesAllTools = true;
            showNotification(_t('roleModal.firstRoleNoToolsHint'), 'info');
        } else if (roleUsesAllTools) {
            // 如果当前使用所有工具，需要检查用户是否取消了一些工具
            // 检查状态映射中是否有未选中的已启用工具
            let hasUnselectedTools = false;
            roleToolStateMap.forEach((state) => {
                // 如果工具在MCP管理中已启用但未选中，说明用户取消了该工具
                if (state.mcpEnabled !== false && !state.enabled) {
                    hasUnselectedTools = true;
                }
            });
            
            // 如果用户取消了一些已启用的工具，切换到部分工具模式
            if (hasUnselectedTools) {
                // 在切换之前，需要加载所有工具到状态映射中
                // 这样我们可以正确保存所有工具的状态（除了用户取消的那些）
                await loadAllToolsToStateMap();
                
                // 将所有已启用的工具标记为选中（除了用户已取消的那些）
                // 用户已取消的工具在状态映射中enabled为false，保持不变
                roleToolStateMap.forEach((state, toolKey) => {
                    // 如果工具在MCP管理中已启用，且状态映射中没有明确标记为未选中（即enabled不是false）
                    // 则标记为选中
                    if (state.mcpEnabled !== false && state.enabled !== false) {
                        state.enabled = true;
                    }
                });
                
                roleUsesAllTools = false;
            } else {
                // 即使使用所有工具，也需要加载所有工具到状态映射中，以便检查是否有未启用的工具被选中
                // 这样可以检测用户是否手动选择了一些未启用的工具
                await loadAllToolsToStateMap();
                
                // 检查是否有未启用的工具被手动选中（enabled为true但mcpEnabled为false）
                let hasDisabledToolsSelected = false;
                roleToolStateMap.forEach((state) => {
                    if (state.enabled && state.mcpEnabled === false) {
                        hasDisabledToolsSelected = true;
                    }
                });
                
                // 如果没有未启用的工具被选中，将所有已启用的工具标记为选中（这是使用所有工具的默认行为）
                if (!hasDisabledToolsSelected) {
                    roleToolStateMap.forEach((state) => {
                        if (state.mcpEnabled !== false) {
                            state.enabled = true;
                        }
                    });
                }
                
                // 更新 allSelectedTools，因为现在状态映射中包含了所有工具
                allSelectedTools = getAllSelectedRoleTools();
            }
        }
        
        // 检查哪些工具未在MCP管理中启用（无论是否使用所有工具都要检查）
        disabledTools = getDisabledTools(allSelectedTools);
        
        // 如果有未启用的工具，提示用户
        if (disabledTools.length > 0) {
            const toolNames = disabledTools.map(t => t.name).join('、');
            const message = typeof window.t === 'function'
                ? window.t('roles.disabledToolsConfirm', { count: disabledTools.length, tools: toolNames })
                : `以下 ${disabledTools.length} 个工具未在MCP管理中启用，无法在角色中配置：\n\n${toolNames}\n\n请先在"MCP管理"中启用这些工具，然后再在角色中配置。\n\n是否继续保存？（将只保存已启用的工具）`;
            
            if (!confirm(message)) {
                return; // 用户取消保存
            }
        }
        
        // 如果使用所有工具，不需要获取工具列表
        if (!roleUsesAllTools) {
            // 获取选中的工具列表（只包含在MCP管理中已启用的工具）
            tools = await getSelectedRoleTools();
        }
    }

    const roleData = {
        name: name,
        description: description,
        icon: icon || undefined, // 如果为空字符串，则不发送该字段
        user_prompt: userPrompt,
        tools: tools, // 默认角色为空数组，表示使用所有工具
        enabled: enabled,
        workflow_id: workflowId || undefined,
        workflow_version: workflowId ? 'latest' : undefined,
        workflow_policy: workflowId ? (workflowPolicy || 'auto') : undefined
    };
    const url = isEdit ? `/api/roles/${encodeURIComponent(name)}` : '/api/roles';
    const method = isEdit ? 'PUT' : 'POST';

    try {
        const response = await apiFetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(roleData)
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || (typeof window.t === 'function' ? window.t('roles.saveFailed') : '保存角色失败'));
        }

        // 如果有未启用的工具被过滤掉了，提示用户
        if (disabledTools.length > 0) {
            let toolNames = disabledTools.map(t => t.name).join('、');
            // 如果工具名称列表太长，截断显示
            if (toolNames.length > 100) {
                toolNames = toolNames.substring(0, 100) + '...';
            }
            const filteredKey = isEdit ? 'roles.roleUpdatedWithFilteredTools' : 'roles.roleCreatedWithFilteredTools';
            showNotification(
                typeof window.t === 'function'
                    ? window.t(filteredKey, { count: disabledTools.length, tools: toolNames })
                    : `${isEdit ? '角色已更新' : '角色已创建'}，但已过滤 ${disabledTools.length} 个未在MCP管理中启用的工具：${toolNames}。请先在"MCP管理"中启用这些工具，然后再在角色中配置。`,
                'warning'
            );
        } else {
            showNotification(
                typeof window.t === 'function'
                    ? window.t(isEdit ? 'roles.roleUpdated' : 'roles.roleCreated')
                    : (isEdit ? '角色已更新' : '角色已创建'),
                'success'
            );
        }
        
        closeRoleModal();
        await refreshRoles();
    } catch (error) {
        console.error('保存角色失败:', error);
        showNotification((typeof window.t === 'function' ? window.t('roles.saveFailed') : '保存角色失败') + ': ' + error.message, 'error');
    }
}

// 删除角色
async function deleteRole(roleName) {
    if (roleName === 'Default') {
        showNotification(_t('roleModal.cannotDeleteDefaultRole'), 'error');
        return;
    }

    if (!confirm(typeof window.t === 'function' ? window.t('roles.deleteRoleConfirm', { name: roleName }) : `确定要删除角色"${roleName}"吗？此操作不可撤销。`)) {
        return;
    }

    try {
        const response = await apiFetch(`/api/roles/${encodeURIComponent(roleName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || (typeof window.t === 'function' ? window.t('roles.deleteFailed') : '删除角色失败'));
        }

        showNotification(typeof window.t === 'function' ? window.t('roles.deleted') : '角色已删除', 'success');
        
        // 如果删除的是当前选中的角色,切换到默认角色
        if (currentRole === roleName) {
            handleRoleChange('');
        }

        await refreshRoles();
    } catch (error) {
        console.error('删除角色失败:', error);
        showNotification((typeof window.t === 'function' ? window.t('roles.deleteFailed') : '删除角色失败') + ': ' + error.message, 'error');
    }
}

// 在页面切换时初始化角色列表
if (typeof window.switchPage === 'function') {
    const originalSwitchPage = window.switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        if (page === 'roles-management') {
            loadRoles().then(() => renderRolesList());
        }
    };
}

// 点击模态框外部关闭
document.addEventListener('click', (e) => {
    const roleSelectModal = document.getElementById('role-select-modal');
    if (roleSelectModal && e.target === roleSelectModal) {
        closeRoleSelectModal();
    }

    const roleModal = document.getElementById('role-modal');
    if (roleModal && e.target === roleModal) {
        closeRoleModal();
    }

    // 点击角色选择面板外部关闭（须用 #role-selector-wrapper，勿用 .role-selector-wrapper：项目选择器也带该类）
    if (isRoleSelectionPanelOpen()) {
        const roleSelectorWrapper = getChatRoleSelectorWrapper();
        if (!roleSelectorWrapper?.contains(e.target)) {
            closeRoleSelectionPanel();
        }
    }
});

// 页面加载时初始化
document.addEventListener('DOMContentLoaded', () => {
    loadRoles();
    updateRoleSelectorDisplay();
    refreshRoleModalSelects();
});

// 语言切换后刷新角色选择器与「选择角色」列表文案
document.addEventListener('languagechange', () => {
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar();
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    syncAllRoleModalSelects();
});

// 获取当前选中的角色（供chat.js使用）
function getCurrentRole() {
    return currentRole || '';
}

// 暴露函数到全局作用域
if (typeof window !== 'undefined') {
    window.getCurrentRole = getCurrentRole;
    window.setCurrentRole = handleRoleChange;
    window.toggleRoleSelectionPanel = toggleRoleSelectionPanel;
    window.closeRoleSelectionPanel = closeRoleSelectionPanel;
    window.closeRoleSelectModal = closeRoleSelectModal;
    window.filterRoleToolsByStatus = filterRoleToolsByStatus;
    window.refreshRoleModalSelects = refreshRoleModalSelects;
    window.currentSelectedRole = getCurrentRole();
    
    // 监听角色变化，更新全局变量
    const originalHandleRoleChange = handleRoleChange;
    handleRoleChange = function(roleName) {
        originalHandleRoleChange(roleName);
        if (typeof window !== 'undefined') {
            window.currentSelectedRole = getCurrentRole();
            window.setCurrentRole = handleRoleChange;
        }
    };
}
