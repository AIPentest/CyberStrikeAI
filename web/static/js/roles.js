// Role management related functions
function _t(key, opts) {
    if (typeof window.t === 'function') {
        try {
            var translated = window.t(key, opts);
            if (typeof translated === 'string' && translated && translated !== key) {
                return translated;
            }
        } catch (e) { /* ignore */ }
    }
    // Avoid exposing the key to the user when i18n is not ready or the entry is missing (consistent with the zh-CN default)
    if (key === 'roles.noDescription') return 'No description available';
    if (key === 'roles.noDescriptionShort') return 'No description';
    if (key === 'roles.defaultRoleDescription') {
        return 'Default role, does not carry additional user prompts, uses the default MCP';
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

/** Trim the description in role config, and treat literals mistakenly stored as i18n keys as empty */
// roles/ directory seed roles: seed names/descriptions translated per language; role names/descriptions modified by the user kept as the original text
const SEED_ROLE_NAME_KEYS = {
    'Default': 'chat.defaultRole',
    'penetration testing': 'roles.seedPentest.name',
    'API Security Testing': 'roles.seedApiSecurity.name',
    'Web application scanning': 'roles.seedWebAppScan.name',
    'Web Framework Test': 'roles.seedWebFramework.name',
    'Information collection': 'roles.seedInfoCollect.name',
    'post-penetration testing': 'roles.seedPostExploit.name',
    'Container security': 'roles.seedContainerSec.name',
    'Cloud Security Audit': 'roles.seedCloudAudit.name',
    'binary analysis': 'roles.seedBinary.name',
    'digital forensics': 'roles.seedForensics.name',
    'Comprehensive vulnerability scanning': 'roles.seedVulnScan.name',
    'CTF': 'roles.seedCtf.name'
};
const SEED_ROLE_DESC_KEYS = {
    'Default role, does not carry additional user prompts, uses the default MCP': 'roles.defaultRoleDescription',
    'Professional penetration testing experts, comprehensive in-depth vulnerability detection': 'roles.seedPentest.description',
    'API security testing expert, specializing in API interface security detection': 'roles.seedApiSecurity.description',
    'Web application vulnerability scanning expert, comprehensive web security detection': 'roles.seedWebAppScan.description',
    'Web framework security testing expert, specializing in Web application framework vulnerability detection': 'roles.seedWebFramework.description',
    'Asset Discovery and Information Gathering Expert': 'roles.seedInfoCollect.description',
    'Post-penetration testing expert, privilege maintenance and lateral movement': 'roles.seedPostExploit.description',
    'Container and Kubernetes security expert, container environment security detection': 'roles.seedContainerSec.description',
    'Cloud security audit expert, multi-cloud environment security detection': 'roles.seedCloudAudit.description',
    'Binary analysis and exploitation expert, skilled in reverse engineering and password cracking': 'roles.seedBinary.description',
    'Digital forensics and steganography analysis expert, file and memory forensics': 'roles.seedForensics.description',
    'Comprehensive vulnerability scanning expert, multiple types of vulnerability detection': 'roles.seedVulnScan.description',
    'CTF competition expert, skilled in problem solving and vulnerability exploitation': 'roles.seedCtf.description'
};

function rolePlainDescription(role) {
    const raw = typeof role.description === 'string' ? role.description.trim() : '';
    if (!raw) return '';
    if (raw === 'roles.noDescription' || raw === 'roles.noDescriptionShort') return '';
    const seedKey = SEED_ROLE_DESC_KEYS[raw];
    if (seedKey) return _t(seedKey);
    return raw;
}

/** Role name for display: seed names are translated according to the language, user-created role names are preserved as the original text */
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
let rolesSearchKeyword = ''; // Role search keyword
let rolesSearchTimeout = null; // Search debounce timer
let allRoleTools = []; // Stores all tools list (for role tool selection)
// Shares localStorage with the MCP tool configuration, making it easier to standardize ops habits
function getRoleToolsPageSize() {
    const saved = localStorage.getItem('toolsPageSize');
    const n = saved ? parseInt(saved, 10) : 20;
    return isNaN(n) || n < 1 ? 20 : n;
}
// This role association filter: '' = all, 'role_on' = association checked for this role, 'role_off' = not associated with this role
let roleToolsStatusFilter = '';
/** Cache the full list when filtering by role association (matching the current search) to avoid losing state during pagination */
let roleToolsListCacheFull = [];
let roleToolsListCacheSearch = '';
/** Whether to use client-side pagination (true in role relation filter mode) */
let roleToolsClientMode = false;
let roleToolsPagination = {
    page: 1,
    pageSize: getRoleToolsPageSize(),
    total: 0,
    totalPages: 1
};
let roleToolsSearchKeyword = ''; // Tool search keywords
let roleToolStateMap = new Map(); // Tool state mapping: toolKey -> { enabled: boolean, ... }
let roleUsesAllTools = false; // Flag whether the role uses all tools (when tools is not configured)
let totalEnabledToolsInMCP = 0; // Total number of enabled tools (fetched from MCP management, fetched from API response)
// Only update on the results of "no status filter, no search" requests, for use as the stats bar denominator (to avoid errors like 25/9 caused by total shrinking after filtering)
let roleToolsStatsGrandTotal = 0; // Total number of tools (matches "All" in the MCP list)
let roleToolsStatsMcpEnabledTotal = 0; // Number of tools enabled globally in MCP
let roleConfiguredTools = new Set(); // The role configuration's tool list (used to determine which tools should be selected)

// Sort the role list: the default role comes first, the rest are sorted by name
function sortRoles(rolesArray) {
    const sortedRoles = [...rolesArray];
    // Separate out the "default" role
    const defaultRole = sortedRoles.find(r => r.name === 'Default');
    const otherRoles = sortedRoles.filter(r => r.name !== 'Default');
    
    // Other roles are sorted by name, maintaining a fixed order
    otherRoles.sort((a, b) => {
        const nameA = a.name || '';
        const nameB = b.name || '';
        return nameA.localeCompare(nameB, 'zh-CN');
    });
    
    // Put the "default" role first, followed by the other roles in sorted order
    const result = defaultRole ? [defaultRole, ...otherRoles] : otherRoles;
    return result;
}

// Load all roles
async function loadRoles() {
    if (window.i18nReady && typeof window.i18nReady.then === 'function') {
        try {
            await window.i18nReady;
        } catch (e) { /* ignore */ }
    }
    try {
        const response = await apiFetch('/api/roles');
        if (!response.ok) {
            throw new Error(typeof window.t === 'function' ? window.t('roles.loadFailed') : 'Failed to load role');
        }
        const data = await response.json();
        roles = data.roles || [];
        updateRoleSelectorDisplay();
        renderRoleSelectionSidebar(); // Render sidebar role list
        return roles;
    } catch (error) {
        console.error('Failed to load role:', error);
        // Prompt text uses i18n; if i18n is not yet initialized at this point, fall back to readable Chinese rather than exposing the key (roles.loadFailed)
        var loadFailedLabel = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('roles.loadFailed')
            : 'Failed to load role';
        showNotification(loadFailedLabel + ': ' + error.message, 'error');
        return [];
    }
}

// Handle role change
function handleRoleChange(roleName) {
    const oldRole = currentRole;
    currentRole = roleName || '';
    localStorage.setItem('currentRole', currentRole);
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar(); // Update sidebar selected state
    
    // When the role is switched, if the tools list has been loaded, mark it as needing to be reloaded
    // This way, the next time the @tool suggestion is triggered, it will use the new role to reload the tool list
    if (oldRole !== currentRole && typeof window !== 'undefined') {
        // By setting a flag to notify chat.js that it needs to reload the tool list
        window._mentionToolsRoleChanged = true;
    }
}

// Update role selector display
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
        // Use the icon from the configuration, if there is none then use the default icon
        let icon = selectedRole.icon || '🔵';
        // If icon is in Unicode escape format (\U0001F3C6), it needs to be converted to an emoji
        if (icon && typeof icon === 'string') {
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If conversion fails, use default icon
                    console.warn('Failed to convert icon Unicode escape:', icon, e);
                    icon = '🔵';
                }
            }
        }
        roleSelectorIcon.textContent = icon;
        const isDefaultRole = selectedRole.name === 'Default' || !selectedRole.name;
        const displayName = roleDisplayName(selectedRole);
        // When the role is not the default one, avoid being overwritten into "default" by i18n's data-i18n
        roleSelectorText.setAttribute('data-i18n-skip-text', isDefaultRole ? 'false' : 'true');
        roleSelectorText.textContent = displayName;
    } else {
        // Default role
        roleSelectorText.setAttribute('data-i18n-skip-text', 'false');
        roleSelectorIcon.textContent = '🔵';
        roleSelectorText.textContent = typeof window.t === 'function' ? window.t('chat.defaultRole') : 'Default';
    }
}

// Render main content area role selection list
function renderRoleSelectionSidebar() {
    const roleList = document.getElementById('role-selection-list');
    if (!roleList) return;

    // Clear the list
    roleList.innerHTML = '';

    // Get the icon based on the role configuration; if not configured, use the default icon
    function getRoleIcon(role) {
        if (role.icon) {
            // If icon is in Unicode escape format (\U0001F3C6), it needs to be converted to an emoji
            let icon = role.icon;
            // Check if it is a Unicode escape format (may contain quotes)
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If conversion fails, use the original value
                    console.warn('Failed to convert icon Unicode escape:', icon, e);
                }
            }
            return icon;
        }
        // If no icon is configured, generate a default icon based on the first character of the role name
        // Use some generic default icons
        return '👤';
    }
    
    // Sort the roles: default role first, the rest sorted by name
    const sortedRoles = sortRoles(roles);
    
    // Only show enabled roles
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
            closeRoleSelectionPanel(); // Automatically close the panel after selection
        };
        roleItem.onkeydown = (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                roleItem.click();
            }
        };
        const icon = getRoleIcon(role);
        
        // Handle default role description
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

// Select Role
function selectRole(roleName) {
    // Map "default" to an empty string (representing the default role)
    if (roleName === 'Default') {
        roleName = '';
    }
    handleRoleChange(roleName);
    renderRoleSelectionSidebar(); // Re-render to update the selected state
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

// Toggle role selection panel show/hide
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
        panel.style.display = 'flex'; // Use flex layout
        // Add visual feedback for open state
        if (roleSelectorBtn) {
            roleSelectorBtn.classList.add('active');
            roleSelectorBtn.setAttribute('aria-expanded', 'true');
        }
        
        // Ensure the panel is rendered before checking the position
        setTimeout(() => {
            const wrapper = getChatRoleSelectorWrapper();
            if (wrapper) {
                const rect = wrapper.getBoundingClientRect();
                const panelHeight = panel.offsetHeight || 400;
                const viewportHeight = window.innerHeight;
                
                // If the panel top is above the viewport, scroll to the appropriate position
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

// Close the role selection panel (automatically called after a role is selected)
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

// Escape HTML
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

// Refresh role list
async function refreshRoles() {
    await loadRoles();
    // Check whether the current page is the role management page
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    // Always update the sidebar role selection list
    renderRoleSelectionSidebar();
    showNotification(typeof window.t === 'function' ? window.t('roles.refreshed') : 'Refreshed', 'success');
}

// Render role list
function renderRolesList() {
    const rolesList = document.getElementById('roles-list');
    if (!rolesList) return;

    // Filter roles (by search keyword): seed roles also match by translated name/description
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

    // Sort the roles: default role first, the rest sorted by name
    const sortedRoles = sortRoles(filteredRoles);
    
    rolesList.innerHTML = sortedRoles.map(role => {
        const plainDesc = rolePlainDescription(role);
        // Get the role icon, converting it to emoji if it is in Unicode escape format
        let roleIcon = role.icon || '👤';
        if (roleIcon && typeof roleIcon === 'string') {
            // Check if it is a Unicode escape format (may contain quotes)
            const unicodeMatch = roleIcon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    roleIcon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If conversion fails, use default icon
                    console.warn('Failed to convert icon Unicode escape:', roleIcon, e);
                    roleIcon = '👤';
                }
            }
        }

        // Get tool list display
        let toolsDisplay = '';
        let toolsCount = 0;
        if (role.name === 'Default') {
            toolsDisplay = _t('roleModal.usingAllTools');
        } else if (role.tools && role.tools.length > 0) {
            toolsCount = role.tools.length;
            // Display the first 5 tool names
            const toolNames = role.tools.slice(0, 5).map(tool => {
                // If it is an external tool, the format is external_mcp__tool_name, only display the tool name
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

// Handle role search input
function handleRolesSearchInput() {
    clearTimeout(rolesSearchTimeout);
    rolesSearchTimeout = setTimeout(() => {
        searchRoles();
    }, 300);
}

// Search roles
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

// Clear role search
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

// Generate the tool's unique identifier (keep consistent with getToolKey in settings.js)
function getToolKey(tool) {
    // If it is an external tool, use external_mcp::tool.name as the unique identifier
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    // Built-in tools directly use the tool name
    return tool.name;
}

// Merge a single tool into roleToolStateMap (consistent with the single-entry logic in loadRoleTools)
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
            throw new Error(typeof window.t === 'function' ? window.t('roles.fetchToolsFailed') : 'Failed to get tool list');
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

// Save the current page's tool state to the global map
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
                mcpEnabled: existingState ? existingState.mcpEnabled : true // Preserve MCP enabled state
            });
        }
    });
}

// Load all tools list (for role tool selection)
async function loadRoleTools(page = 1, searchKeyword = '') {
    try {
        // Before loading a new page, first save the current page's state to the global map
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
                throw new Error(typeof window.t === 'function' ? window.t('roles.fetchToolsFailed') : 'Failed to get tool list');
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
        console.error('Failed to load tool list:', error);
        const toolsList = document.getElementById('role-tools-list');
        if (toolsList) {
            toolsList.innerHTML = `<div class="tools-error">${_t('roleModal.loadToolsFailed')}: ${escapeHtml(error.message)}</div>`;
        }
    }
}

// Render character tool selection list
function renderRoleToolsList() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Clear loading prompt and old content
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
        
        // Get tool state from state mapping
        const toolState = roleToolStateMap.get(toolKey) || {
            enabled: tool.enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || ''
        };
        
        // External tool tag
        let externalBadge = '';
        if (toolState.is_external || tool.is_external) {
            const externalMcpName = toolState.external_mcp || tool.external_mcp || '';
            const badgeText = externalMcpName
                ? (typeof window.t === 'function' ? window.t('roles.externalBadgeWithName', { name: escapeHtml(externalMcpName) }) : `External (\${escapeHtml(externalMcpName)})`)
                : (typeof window.t === 'function' ? window.t('roles.externalBadge') : 'external');
            const badgeTitle = externalMcpName
                ? (typeof window.t === 'function' ? window.t('roles.externalToolBadgeTitleSource', { name: escapeHtml(externalMcpName) }) : `External MCP tool - Source: \${escapeHtml(externalMcpName)}`)
                : (typeof window.t === 'function' ? window.t('roles.externalToolBadgeTitle') : 'External MCP tools');
            externalBadge = `<span class="external-tool-badge" title="${escapeAttr(badgeTitle)}">${badgeText}</span>`;
        }
        let mcpDisabledBadge = '';
        if (tool.enabled === false) {
            mcpDisabledBadge = `<span class="role-tool-mcp-disabled-badge" title="${escapeHtml(_t('roleModal.mcpDisabledBadgeTitle'))}">${escapeHtml(_t('roleModal.mcpDisabledBadge'))}</span>`;
        }
        // Generate unique checkbox id
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
                <div class="role-tool-item-desc">${escapeHtml(tool.description || (typeof window.t === 'function' ? window.t('roles.noDescriptionShort') : 'No description'))}</div>
            </div>
        `;
        listContainer.appendChild(toolItem);
    });
    
    toolsList.appendChild(listContainer);
}

// Render the tool list pagination control (always display the range and items per page, so that the page size can still be adjusted even when there is only one page)
function renderRoleToolsPagination() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Remove old pagination control
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
    const perPageLabel = typeof window.t === 'function' ? window.t('mcp.perPage') : 'per page';
    
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

// Handle tool checkbox state change
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
            mcpEnabled: existingState ? existingState.mcpEnabled : true // Preserve MCP enabled state
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

// Select All Tool
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
                // Only select tools that are enabled in MCP management
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

// Deselect all tool
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
                    mcpEnabled: existingState ? existingState.mcpEnabled : true // Preserve MCP enabled state
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

// Search tools
function searchRoleTools(keyword) {
    roleToolsSearchKeyword = keyword;
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = keyword ? 'block' : 'none';
    }
    loadRoleTools(1, keyword);
}

// Clear search
function clearRoleToolsSearch() {
    document.getElementById('role-tools-search').value = '';
    searchRoleTools('');
}

// Update tool statistics info (definition: denominator "linkable upper limit" = number of enabled tools across all MCPs in the library, consistent with the count of the "MCP enabled" filter on the MCP management page; checked = linked to this role)
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

// Get the selected tool list (returns a toolKey array)
async function getSelectedRoleTools() {
    // First save the current page's state
    saveCurrentRolePageToolStates();
    
    // If there is no search keyword, we need to load tools from all pages to ensure the status mapping is complete.
    // But for performance, we can just get the selected tools from the state map
    // The problem is: if the user only selected tools on certain pages, the tool state of other pages may not be in the mapping
    
    // If the total number of tools is greater than the number of loaded tools, we need to ensure that tools from all unloaded pages are also considered
    // But for role tool selection, we only need to retrieve the tools the user has explicitly selected
    // So we can just get the selected tool directly from the state map
    
    // Get all selected tools from the state mapping (only returns tools that are enabled in MCP management)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        // Only return tools that are enabled in MCP management and selected by the role
        if (state.enabled && state.mcpEnabled !== false) {
            selectedTools.push(toolKey);
        }
    });
    
    // If the user may have selected a tool on another page, we need to ensure that the current page's state is also saved
    // But the state map should already contain the states of all visited pages
    
    return selectedTools;
}

// Set the selected tool (used when editing a character)
function setSelectedRoleTools(selectedToolKeys) {
    const selectedSet = new Set(selectedToolKeys || []);
    
    // Update status mapping
    roleToolStateMap.forEach((state, toolKey) => {
        state.enabled = selectedSet.has(toolKey);
    });
    
    // Update the checkbox state of the current page
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            checkbox.checked = selectedSet.has(toolKey);
        }
    });
    
    updateRoleToolsStats();
}

// Show the add role modal dialog
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

    // When adding a role: show the tool selection interface, hide the default role hint
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

    // Reset tool state
    roleToolStateMap.clear();
    roleConfiguredTools.clear(); // Clear the tool list of role configuration
    roleUsesAllTools = false; // By default, do not use all tools when adding a role
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
    
    // Clear the tool list DOM to prevent saveCurrentRolePageToolStates in loadRoleTools from reading stale state
    if (toolsList) {
        toolsList.innerHTML = '';
    }

    // Load and render the tool list
    await loadRoleTools(1, '');
    
    // Ensure the tool list is displayed
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    
    // Ensure statistics info is updated correctly (display 0/108)
    updateRoleToolsStats();

    refreshRoleModalSelects();
    openAppModal('role-modal');
}

// Edit Role
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
    document.getElementById('role-name').disabled = true; // Cannot modify name when editing
    document.getElementById('role-description').value = role.description || '';
    // Process the icon field: if it is in Unicode escape format, convert to emoji; otherwise use it directly
    let iconValue = role.icon || '';
    if (iconValue && iconValue.startsWith('\\U')) {
        // Convert Unicode escape format (e.g. \U0001F3C6) to emoji
        try {
            const codePoint = parseInt(iconValue.substring(2), 16);
            iconValue = String.fromCodePoint(codePoint);
        } catch (e) {
            // If conversion fails, use the original value
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

    // Check whether it is the default role
    const isDefaultRole = roleName === 'Default';
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (isDefaultRole) {
        // Default role: hide tool selection interface, display prompt message
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
        // Non-default role: display tool selection interface, hide hint message
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

        // Reset tool state
        roleToolStateMap.clear();
        roleConfiguredTools.clear(); // Clear the tool list of role configuration
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

        // Prefer the tools field; if absent, use the mcps field (backward compatibility)
        const selectedTools = role.tools || (role.mcps && role.mcps.length > 0 ? role.mcps : []);
        
        // Determine whether to use all tools: if tools is not configured (or tools is an empty array), it means all tools are used
        roleUsesAllTools = !role.tools || role.tools.length === 0;
        
        // Tool list for saving role configuration
        if (selectedTools.length > 0) {
            selectedTools.forEach(toolKey => {
                roleConfiguredTools.add(toolKey);
            });
        }
        
        // If there are selected tools, first initialize the state mapping
        if (selectedTools.length > 0) {
            roleUsesAllTools = false; // There is a configuration tool, don't use all tools
            // Add the selected tool to the state mapping (marked as selected)
            selectedTools.forEach(toolKey => {
                // If the map doesn't have this tool yet, first create a default state (enabled is true)
                if (!roleToolStateMap.has(toolKey)) {
                    roleToolStateMap.set(toolKey, {
                        enabled: true,
                        is_external: false,
                        external_mcp: '',
                        name: toolKey.split('::').pop() || toolKey // Extract tool name from toolKey
                    });
                } else {
                    // If it already exists, update to selected state
                    const state = roleToolStateMap.get(toolKey);
                    state.enabled = true;
                }
            });
        }

        // Load tool list (first page)
        await loadRoleTools(1, '');
        
        // If using all tools, mark all enabled tools on the current page as selected
        if (roleUsesAllTools) {
            // Mark all tools enabled in MCP management on the current page as selected
            document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
                const toolItem = checkbox.closest('.role-tool-item');
                if (toolItem) {
                    const toolKey = toolItem.dataset.toolKey;
                    const toolName = toolItem.dataset.toolName;
                    const isExternal = toolItem.dataset.isExternal === 'true';
                    const externalMcp = toolItem.dataset.externalMcp || '';
                    if (toolKey) {
                        const state = roleToolStateMap.get(toolKey);
                        // Only select tools that are enabled in MCP management
                        // If the state exists, use mcpEnabled from the state; otherwise assume it's enabled (since loadRoleTools should have already initialized all tools)
                        const shouldEnable = state ? (state.mcpEnabled !== false) : true;
                        checkbox.checked = shouldEnable;
                        if (state) {
                            state.enabled = shouldEnable;
                        } else {
                            // If the state doesn't exist, create a new state (this case shouldn't happen, because loadRoleTools should have already initialized it)
                            roleToolStateMap.set(toolKey, {
                                enabled: shouldEnable,
                                is_external: isExternal,
                                external_mcp: externalMcp,
                                name: toolName,
                                mcpEnabled: true // Assume it is enabled, the actual value will be updated in loadRoleTools
                            });
                        }
                    }
                }
            });
            // Update statistics info, ensure the correct selected count is displayed
            updateRoleToolsStats();
        } else if (selectedTools.length > 0) {
            // After loading completes, set the selected state again (ensure the current page's tools are also set correctly)
            setSelectedRoleTools(selectedTools);
        }
    }

    refreshRoleModalSelects();
    openAppModal('role-modal');
}

// Close role modal
function closeRoleModal() {
    closeAllRoleModalSelects();
    closeAppModal('role-modal');
}

function closeRoleSelectModal() {
    closeAppModal('role-select-modal');
}

// Get all selected tools (including tools not enabled in MCP management)
function getAllSelectedRoleTools() {
    // First save the current page's state
    saveCurrentRolePageToolStates();
    
    // Get all selected tools from the state map (regardless of whether they are enabled in MCP management)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        if (state.enabled) {
            selectedTools.push({
                key: toolKey,
                name: state.name || toolKey.split('::').pop() || toolKey,
                mcpEnabled: state.mcpEnabled !== false // When mcpEnabled is false, it is not enabled; in all other cases, it is treated as enabled
            });
        }
    });
    
    return selectedTools;
}

// Check and get tools not enabled in MCP management
function getDisabledTools(selectedTools) {
    return selectedTools.filter(tool => {
        const state = roleToolStateMap.get(tool.key);
        // If mcpEnabled is explicitly false, treat it as not enabled
        return state && state.mcpEnabled === false;
    });
}

// Load all tools into the state map (for when switching from using all tools to a subset of tools)
async function loadAllToolsToStateMap() {
    try {
        const pageSize = 100; // Use a larger page size to reduce the number of requests
        let page = 1;
        let hasMore = true;
        
        // Iterate through all pages to get all tools
        while (hasMore) {
            const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error(typeof window.t === 'function' ? window.t('roles.fetchToolsFailed') : 'Failed to get tool list');
            }

            const result = await response.json();

            // Add all tools to the state map
            result.tools.forEach(tool => {
                const toolKey = getToolKey(tool);
                if (!roleToolStateMap.has(toolKey)) {
                    // Tool not in map, initialize based on current mode
                    let enabled = false;
                    if (roleUsesAllTools) {
                        // If all tools are used, and the tools are enabled in MCP management, mark as checked
                        enabled = tool.enabled ? true : false;
                    } else {
                        // If not using all tools, only tools in the role configuration's tool list are marked as selected
                        enabled = roleConfiguredTools.has(toolKey);
                    }
                    roleToolStateMap.set(toolKey, {
                        enabled: enabled,
                        is_external: tool.is_external || false,
                        external_mcp: tool.external_mcp || '',
                        name: tool.name,
                        mcpEnabled: tool.enabled // Save the original enabled state in MCP management
                    });
                } else {
                    // Tool is already in the map, update other properties but preserve the enabled state
                    const state = roleToolStateMap.get(toolKey);
                    state.is_external = tool.is_external || false;
                    state.external_mcp = tool.external_mcp || '';
                    state.mcpEnabled = tool.enabled; // Update the original enabled state in MCP management
                    if (!state.name || state.name === toolKey.split('::').pop()) {
                        state.name = tool.name; // Update tool name
                    }
                }
            });
            
            // Check if there are more pages
            if (page >= result.total_pages) {
                hasMore = false;
            } else {
                page++;
            }
        }
    } catch (error) {
        console.error('Failed to load all tools to state mapping:', error);
        throw error;
    }
}

// Save role
async function saveRole() {
    if (typeof requirePermission === 'function' && !requirePermission('roles:write')) return;
    const name = document.getElementById('role-name').value.trim();
    if (!name) {
        showNotification(_t('roleModal.roleNameRequired'), 'error');
        return;
    }

    const description = document.getElementById('role-description').value.trim();
    let icon = document.getElementById('role-icon').value.trim();
    // Convert emoji to Unicode escape format to match YAML format (e.g. \U0001F3C6)
    if (icon) {
        // Get the Unicode code point of the first character (handle the case where emoji may be multiple characters)
        const codePoint = icon.codePointAt(0);
        if (codePoint && codePoint > 0x7F) {
            // Convert to 8-digit hexadecimal format (\U0001F3C6)
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
    
    // Check whether it is the default role
    const isDefaultRole = name === 'Default';
    
    // Check whether this is the first time adding a role (after excluding default roles, there are no roles created by users)
    const isFirstUserRole = !isEdit && !isDefaultRole && roles.filter(r => r.name !== 'Default').length === 0;
    
    // Default role does not save the tools field (use all tools)
    // Non-default role: if all tools are used (roleUsesAllTools is true), also don't save the tools field
    let tools = [];
    let disabledTools = []; // Store tools that are not enabled in MCP management
    
    if (!isDefaultRole) {
        // Save the state of the current page
        saveCurrentRolePageToolStates();
        
        // Collect all selected tools (including those not enabled in MCP management)
        let allSelectedTools = getAllSelectedRoleTools();
        
        // If this is the first role being added and no tools are selected, default to using all tools
        if (isFirstUserRole && allSelectedTools.length === 0) {
            roleUsesAllTools = true;
            showNotification(_t('roleModal.firstRoleNoToolsHint'), 'info');
        } else if (roleUsesAllTools) {
            // If currently using all tools, need to check whether the user has deselected some tools
            // Check whether there are any unselected enabled tools in the state mapping
            let hasUnselectedTools = false;
            roleToolStateMap.forEach((state) => {
                // If the tool is enabled in MCP management but not selected, it means the user has deselected the tool
                if (state.mcpEnabled !== false && !state.enabled) {
                    hasUnselectedTools = true;
                }
            });
            
            // If the user deselects some of the enabled tools, switch to partial tool mode
            if (hasUnselectedTools) {
                // Before switching, all tools need to be loaded into the state mapping
                // This way we can correctly save the state of all tools (except those the user cancelled)
                await loadAllToolsToStateMap();
                
                // Mark all enabled tools as selected (except those the user has cancelled)
                // Tools already cancelled by the user have enabled set to false in the status mapping, keep unchanged
                roleToolStateMap.forEach((state, toolKey) => {
                    // If the tool is enabled in MCP management and is not explicitly marked as unselected in the status mapping (i.e., enabled is not false)
                    // then mark as selected
                    if (state.mcpEnabled !== false && state.enabled !== false) {
                        state.enabled = true;
                    }
                });
                
                roleUsesAllTools = false;
            } else {
                // Even if all tools are used, we still need to load all tools into the state mapping to check whether any disabled tools are selected
                // This allows us to detect whether the user has manually selected some tools that are not enabled
                await loadAllToolsToStateMap();
                
                // Check if any unenabled tools have been manually selected (enabled is true but mcpEnabled is false)
                let hasDisabledToolsSelected = false;
                roleToolStateMap.forEach((state) => {
                    if (state.enabled && state.mcpEnabled === false) {
                        hasDisabledToolsSelected = true;
                    }
                });
                
                // If no unenabled tools are selected, mark all enabled tools as selected (this is the default behavior of using all tools)
                if (!hasDisabledToolsSelected) {
                    roleToolStateMap.forEach((state) => {
                        if (state.mcpEnabled !== false) {
                            state.enabled = true;
                        }
                    });
                }
                
                // Update allSelectedTools, because the state mapping now contains all tools
                allSelectedTools = getAllSelectedRoleTools();
            }
        }
        
        // Check which tools are not enabled in MCP management (check regardless of whether all tools are used)
        disabledTools = getDisabledTools(allSelectedTools);
        
        // If there are unenabled tools, prompt the user
        if (disabledTools.length > 0) {
            const toolNames = disabledTools.map(t => t.name).join('、');
            const message = typeof window.t === 'function'
                ? window.t('roles.disabledToolsConfirm', { count: disabledTools.length, tools: toolNames })
                : `The following ${disabledTools.length} tools are not enabled in MCP Management and cannot be configured in the role;\n\n${toolNames}\n\nPlease enable these tools in "MCP Management" first, then configure them in the role.\n\nContinue saving? (Only enabled tools will be saved)`;
            
            if (!confirm(message)) {
                return; // User cancelled save
            }
        }
        
        // If using all tools, no need to fetch the tool list
        if (!roleUsesAllTools) {
            // Get the selected tool list (only includes tools that are enabled in MCP management)
            tools = await getSelectedRoleTools();
        }
    }

    const roleData = {
        name: name,
        description: description,
        icon: icon || undefined, // If it is an empty string, this field is not sent
        user_prompt: userPrompt,
        tools: tools, // Default roles is an empty array, meaning all tools are used
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
            throw new Error(error.error || (typeof window.t === 'function' ? window.t('roles.saveFailed') : 'Failed to save role'));
        }

        // If there are unenabled tools that were filtered out, prompt the user
        if (disabledTools.length > 0) {
            let toolNames = disabledTools.map(t => t.name).join('、');
            // If the tool name list is too long, truncate the display
            if (toolNames.length > 100) {
                toolNames = toolNames.substring(0, 100) + '...';
            }
            const filteredKey = isEdit ? 'roles.roleUpdatedWithFilteredTools' : 'roles.roleCreatedWithFilteredTools';
            showNotification(
                typeof window.t === 'function'
                    ? window.t(filteredKey, { count: disabledTools.length, tools: toolNames })
                    : `${isEdit ? 'Character has been updated' : 'Character created'}, but ${disabledTools.length} tools not enabled in MCP Management have been filtered out: ${toolNames}. Please enable these tools in "MCP Management" first, then configure them in the character.`,
                'warning'
            );
        } else {
            showNotification(
                typeof window.t === 'function'
                    ? window.t(isEdit ? 'roles.roleUpdated' : 'roles.roleCreated')
                    : (isEdit ? 'Character has been updated' : 'Character created'),
                'success'
            );
        }
        
        closeRoleModal();
        await refreshRoles();
    } catch (error) {
        console.error('Failed to save role:', error);
        showNotification((typeof window.t === 'function' ? window.t('roles.saveFailed') : 'Failed to save role') + ': ' + error.message, 'error');
    }
}

// Delete Role
async function deleteRole(roleName) {
    if (roleName === 'Default') {
        showNotification(_t('roleModal.cannotDeleteDefaultRole'), 'error');
        return;
    }

    if (!confirm(typeof window.t === 'function' ? window.t('roles.deleteRoleConfirm', { name: roleName }) : `Are you sure you want to delete the role "\${roleName}"? This action cannot be undone.`)) {
        return;
    }

    try {
        const response = await apiFetch(`/api/roles/${encodeURIComponent(roleName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || (typeof window.t === 'function' ? window.t('roles.deleteFailed') : 'Failed to delete role'));
        }

        showNotification(typeof window.t === 'function' ? window.t('roles.deleted') : 'Role deleted', 'success');
        
        // If the deleted role is the currently selected one, switch to the default role
        if (currentRole === roleName) {
            handleRoleChange('');
        }

        await refreshRoles();
    } catch (error) {
        console.error('Failed to delete role:', error);
        showNotification((typeof window.t === 'function' ? window.t('roles.deleteFailed') : 'Failed to delete role') + ': ' + error.message, 'error');
    }
}

// Initialize role list on page switch
if (typeof window.switchPage === 'function') {
    const originalSwitchPage = window.switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        if (page === 'roles-management') {
            loadRoles().then(() => renderRolesList());
        }
    };
}

// Click outside the modal to close it
document.addEventListener('click', (e) => {
    const roleSelectModal = document.getElementById('role-select-modal');
    if (roleSelectModal && e.target === roleSelectModal) {
        closeRoleSelectModal();
    }

    const roleModal = document.getElementById('role-modal');
    if (roleModal && e.target === roleModal) {
        closeRoleModal();
    }

    // Clicking outside the role selection panel closes it (must use #role-selector-wrapper, not .role-selector-wrapper: the project selector also carries that class)
    if (isRoleSelectionPanelOpen()) {
        const roleSelectorWrapper = getChatRoleSelectorWrapper();
        if (!roleSelectorWrapper?.contains(e.target)) {
            closeRoleSelectionPanel();
        }
    }
});

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    loadRoles();
    updateRoleSelectorDisplay();
    refreshRoleModalSelects();
});

// After switching the language, refresh the role selector and the 'Select Role' list text
document.addEventListener('languagechange', () => {
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar();
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    syncAllRoleModalSelects();
});

// Get the currently selected role (for use by chat.js)
function getCurrentRole() {
    return currentRole || '';
}

// Expose function to global scope
if (typeof window !== 'undefined') {
    window.getCurrentRole = getCurrentRole;
    window.setCurrentRole = handleRoleChange;
    window.toggleRoleSelectionPanel = toggleRoleSelectionPanel;
    window.closeRoleSelectionPanel = closeRoleSelectionPanel;
    window.closeRoleSelectModal = closeRoleSelectModal;
    window.filterRoleToolsByStatus = filterRoleToolsByStatus;
    window.refreshRoleModalSelects = refreshRoleModalSelects;
    window.currentSelectedRole = getCurrentRole();
    
    // Listen for role changes, update global variables
    const originalHandleRoleChange = handleRoleChange;
    handleRoleChange = function(roleName) {
        originalHandleRoleChange(roleName);
        if (typeof window !== 'undefined') {
            window.currentSelectedRole = getCurrentRole();
            window.setCurrentRole = handleRoleChange;
        }
    };
}
