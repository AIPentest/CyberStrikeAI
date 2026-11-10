// Role-management functions
function _t(key, opts) {
    if (typeof window.t === 'function') {
        try {
            var translated = window.t(key, opts);
            if (typeof translated === 'string' && translated && translated !== key) {
                return translated;
            }
        } catch (e) { /* ignore */ }
    }
    // i18n When not ready or an entry is missing, avoid exposing the key to the user (consistent with the former zh-CN default)
    if (key === 'roles.noDescription') return 'No description';
    if (key === 'roles.noDescriptionShort') return 'No description';
    if (key === 'roles.defaultRoleDescription') {
        return 'Default role; carries no extra user prompt, uses the defaultMCP';
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

/** The description in the role config: trimmed, treating a literal mistakenly stored as an i18n key as empty */
function rolePlainDescription(role) {
    const raw = typeof role.description === 'string' ? role.description.trim() : '';
    if (!raw) return '';
    if (raw === 'roles.noDescription' || raw === 'roles.noDescriptionShort') return '';
    return raw;
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
let allRoleTools = []; // Store the full tool list (used for role tool selection)
// Shares localStorage with the MCP tool config, for consistent operations habits
function getRoleToolsPageSize() {
    const saved = localStorage.getItem('toolsPageSize');
    const n = saved ? parseInt(saved, 10) : 20;
    return isNaN(n) || n < 1 ? 20 : n;
}
// Filter by this role association: '' = All, 'role_on' = This role is checked as associated, 'role_off' = This role is not associated
let roleToolsStatusFilter = '';
/** When filtering by role association, cache the full list (matching the current search) to avoid losing state when paging */
let roleToolsListCacheFull = [];
let roleToolsListCacheSearch = '';
/** Whether to use client-side pagination (true in role-association filter mode true) */
let roleToolsClientMode = false;
let roleToolsPagination = {
    page: 1,
    pageSize: getRoleToolsPageSize(),
    total: 0,
    totalPages: 1
};
let roleToolsSearchKeyword = ''; // Tool search keyword
let roleToolStateMap = new Map(); // Tool state map: toolKey -> { enabled: boolean, ... }
let roleUsesAllTools = false; // Mark whether the role uses all tools (when tools is not configured)
let totalEnabledToolsInMCP = 0; // Total number of enabled tools (obtained from MCP management, from the API response)
// Only update on request results with no status filter and no search, used as the denominator of the stats bar (to avoid total shrinking after filtering and causing errors like 25/9)
let roleToolsStatsGrandTotal = 0; // Total number of tools (consistent with All in the MCP list)
let roleToolsStatsMcpEnabledTotal = 0; // MCP Globally enabled tool count
let roleConfiguredTools = new Set(); // The role config tool list (used to decide which tools should be selected)

// Sort the role list: the default role comes first, the rest sorted by name
function sortRoles(rolesArray) {
    const sortedRoles = [...rolesArray];
    // will"Default"separate the role out
    const defaultRole = sortedRoles.find(r => r.name === 'Default');
    const otherRoles = sortedRoles.filter(r => r.name !== 'Default');
    
    // Other roles are sorted by name, keeping a fixed order
    otherRoles.sort((a, b) => {
        const nameA = a.name || '';
        const nameB = b.name || '';
        return nameA.localeCompare(nameB, 'en-US');
    });
    
    // will"Default"Put this role first; the other roles follow in sorted order
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
            throw new Error('Failed to load roles');
        }
        const data = await response.json();
        roles = data.roles || [];
        updateRoleSelectorDisplay();
        renderRoleSelectionSidebar(); // Render the sidebar role list
        return roles;
    } catch (error) {
        console.error('Failed to load roles:', error);
        // The hint text uses i18n; if i18n is not yet initialized, fall back to readable text rather than exposing key (roles.loadFailed)
        var loadFailedLabel = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('roles.loadFailed')
            : 'Failed to load roles';
        showNotification(loadFailedLabel + ': ' + error.message, 'error');
        return [];
    }
}

// Handle role changes
function handleRoleChange(roleName) {
    const oldRole = currentRole;
    currentRole = roleName || '';
    localStorage.setItem('currentRole', currentRole);
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar(); // Update the sidebar selected state
    
    // When the role switches, if the tool list is loaded, mark it as needing a reload
    // so the next trigger@Tool suggestions will reload the tool list using the new role
    if (oldRole !== currentRole && typeof window !== 'undefined') {
        // Notify chat.js that the tool list needs reloading by setting a flag
        window._mentionToolsRoleChanged = true;
    }
}

// Update the role-selector display
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
        // Use the icon from the config; if absent, use the default icon
        let icon = selectedRole.icon || '🔵';
        // If the icon is a Unicode-escape format (\U0001F3C6), needs to be converted to emoji
        if (icon && typeof icon === 'string') {
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If the conversion fails, use the default icon
                    console.warn('Failed to convert the icon Unicode escape:', icon, e);
                    icon = '🔵';
                }
            }
        }
        roleSelectorIcon.textContent = icon;
        const isDefaultRole = selectedRole.name === 'Default' || !selectedRole.name;
        const displayName = isDefaultRole && typeof window.t === 'function'
            ? window.t('chat.defaultRole') : (selectedRole.name || (typeof window.t === 'function' ? window.t('chat.defaultRole') : 'Default'));
        // For a non-default role, avoid being overwritten by i18n data-i18n into'Default'
        roleSelectorText.setAttribute('data-i18n-skip-text', isDefaultRole ? 'false' : 'true');
        roleSelectorText.textContent = displayName;
    } else {
        // Default role
        roleSelectorText.setAttribute('data-i18n-skip-text', 'false');
        roleSelectorIcon.textContent = '🔵';
        roleSelectorText.textContent = typeof window.t === 'function' ? window.t('chat.defaultRole') : 'Default';
    }
}

// Render the role-selection list in the main content area
function renderRoleSelectionSidebar() {
    const roleList = document.getElementById('role-selection-list');
    if (!roleList) return;

    // Clear the list
    roleList.innerHTML = '';

    // Get the icon from the role config; if not configured, use the default icon
    function getRoleIcon(role) {
        if (role.icon) {
            // If the icon is a Unicode-escape format (\U0001F3C6), needs to be converted to emoji
            let icon = role.icon;
            // Check whether it is a Unicode-escape format (may contain quotes)
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If the conversion fails, use the original value
                    console.warn('Failed to convert the icon Unicode escape:', icon, e);
                }
            }
            return icon;
        }
        // If no icon is configured, generate a default icon from the first character of the role name
        // Use some generic default icons
        return '👤';
    }
    
    // Sort the roles: the default role first, the rest sorted by name
    const sortedRoles = sortRoles(roles);
    
    // Show only enabled roles
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
            closeRoleSelectionPanel(); // Close the panel automatically after selecting
        };
        roleItem.onkeydown = (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                roleItem.click();
            }
        };
        const icon = getRoleIcon(role);
        
        // Handle the default role description
        const plainDesc = rolePlainDescription(role);
        let description = plainDesc || _t('roles.noDescription');
        if (isDefaultRole && !plainDesc) {
            description = _t('roles.defaultRoleDescription');
        }
        roleItem.setAttribute('data-selection-detail', description);
        
        roleItem.innerHTML = `
            <div class="role-selection-item-icon-main">${icon}</div>
            <div class="role-selection-item-content-main">
                <div class="role-selection-item-name-main">${escapeHtml(role.name)}</div>
                <div class="role-selection-item-description-main">${escapeHtml(description)}</div>
            </div>
            ${isSelected ? '<div class="role-selection-checkmark-main">✓</div>' : ''}
        `;
        roleList.appendChild(roleItem);
    });
}

// Select Role
function selectRole(roleName) {
    // will"Default"Maps to an empty string (meaning the default role)
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

// Toggle the role-selection panel visibility
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
        // Add visual feedback for the open state
        if (roleSelectorBtn) {
            roleSelectorBtn.classList.add('active');
            roleSelectorBtn.setAttribute('aria-expanded', 'true');
        }
        
        // Ensure the panel is rendered before checking its position
        setTimeout(() => {
            const wrapper = getChatRoleSelectorWrapper();
            if (wrapper) {
                const rect = wrapper.getBoundingClientRect();
                const panelHeight = panel.offsetHeight || 400;
                const viewportHeight = window.innerHeight;
                
                // If the top of the panel is off-screen, scroll to a suitable position
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

// Close the role-selection panel (called automatically after selecting a role)
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

// escapeHTML
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

// Refresh the role list
async function refreshRoles() {
    await loadRoles();
    // Check whether the current page is the role management page
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    // Always update the sidebar role-selection list
    renderRoleSelectionSidebar();
    showNotification('Refreshed', 'success');
}

// Render the role list
function renderRolesList() {
    const rolesList = document.getElementById('roles-list');
    if (!rolesList) return;

    // Filter roles (by search keyword)
    let filteredRoles = roles;
    if (rolesSearchKeyword) {
        const keyword = rolesSearchKeyword.toLowerCase();
        filteredRoles = roles.filter(role => 
            role.name.toLowerCase().includes(keyword) ||
            (role.description && role.description.toLowerCase().includes(keyword))
        );
    }

    if (filteredRoles.length === 0) {
        rolesList.innerHTML = '<div class="empty-state">' + 
            (rolesSearchKeyword ? _t('roles.noMatchingRoles') : _t('roles.noRoles')) + 
            '</div>';
        return;
    }

    // Sort the roles: the default role first, the rest sorted by name
    const sortedRoles = sortRoles(filteredRoles);
    
    rolesList.innerHTML = sortedRoles.map(role => {
        const plainDesc = rolePlainDescription(role);
        // Get the role icon; if it is a Unicode-escape format, convert it toemoji
        let roleIcon = role.icon || '👤';
        if (roleIcon && typeof roleIcon === 'string') {
            // Check whether it is a Unicode-escape format (may contain quotes)
            const unicodeMatch = roleIcon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    roleIcon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If the conversion fails, use the default icon
                    console.warn('Failed to convert the icon Unicode escape:', roleIcon, e);
                    roleIcon = '👤';
                }
            }
        }

        // Get the tool list for display
        let toolsDisplay = '';
        let toolsCount = 0;
        if (role.name === 'Default') {
            toolsDisplay = _t('roleModal.usingAllTools');
        } else if (role.tools && role.tools.length > 0) {
            toolsCount = role.tools.length;
            // Show the first 5 tool names
            const toolNames = role.tools.slice(0, 5).map(tool => {
                // If it is an external tool, the format is external_mcp::tool_name, and only the tool name is shown
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
                    ${escapeHtml(role.name)}
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

// Clear the role search
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

// Generate the tool unique identifier (consistent with getToolKey in settings.js)
function getToolKey(tool) {
    // If it is an external tool, use external_mcp::tool.name as the unique identifier
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    // Built-in tools use the tool name directly
    return tool.name;
}

// Merge a single tool into roleToolStateMap (consistent with the single-item logic in loadRoleTools)
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
            throw new Error('Failed to get the tool list');
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

// Save the current page tool state to the global map
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
                mcpEnabled: existingState ? existingState.mcpEnabled : true // Keep the MCP enabled state
            });
        }
    });
}

// Load the full tool list (used for role tool selection)
async function loadRoleTools(page = 1, searchKeyword = '') {
    try {
        // Before loading a new page, save the current page state to the global map
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
                throw new Error('Failed to get the tool list');
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
        console.error('Failed to load the tool list:', error);
        const toolsList = document.getElementById('role-tools-list');
        if (toolsList) {
            toolsList.innerHTML = `<div class="tools-error">${_t('roleModal.loadToolsFailed')}: ${escapeHtml(error.message)}</div>`;
        }
    }
}

// Render the role tool-selection list
function renderRoleToolsList() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Clear the loading hint and old content
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
        
        // Get the tool state from the state map
        const toolState = roleToolStateMap.get(toolKey) || {
            enabled: tool.enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || ''
        };
        
        // External tool tag
        let externalBadge = '';
        if (toolState.is_external || tool.is_external) {
            const externalMcpName = toolState.external_mcp || tool.external_mcp || '';
            const badgeText = externalMcpName ? `External (${escapeHtml(externalMcpName)})` : 'External';
            const badgeTitle = externalMcpName ? `External MCP tool - source: ${escapeHtml(externalMcpName)}` : 'External MCP tool';
            externalBadge = `<span class="external-tool-badge" title="${escapeAttr(badgeTitle)}">${badgeText}</span>`;
        }
        let mcpDisabledBadge = '';
        if (tool.enabled === false) {
            mcpDisabledBadge = `<span class="role-tool-mcp-disabled-badge" title="${escapeHtml(_t('roleModal.mcpDisabledBadgeTitle'))}">${escapeHtml(_t('roleModal.mcpDisabledBadge'))}</span>`;
        }
        // Generate a uniquecheckbox id
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
                <div class="role-tool-item-desc">${escapeHtml(tool.description || 'No description')}</div>
            </div>
        `;
        listContainer.appendChild(toolItem);
    });
    
    toolsList.appendChild(listContainer);
}

// Render the tool-list pagination control (always shows the range and per-page count, so it can still be adjusted even with a single page page size)
function renderRoleToolsPagination() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Remove the old pagination control
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

// Handle tool checkbox state changes
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
            mcpEnabled: existingState ? existingState.mcpEnabled : true // Keep the MCP enabled state
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

// Select all tools
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
                // Only select tools enabled in MCP management
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

// Deselect all tools
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
                    mcpEnabled: existingState ? existingState.mcpEnabled : true // Keep the MCP enabled state
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

// Update the tool statistics (denominator: the associable upper limit'= The number of MCP-enabled tools across the whole store, consistent with the count filtered by MCP-enabled on the MCP management page; checking=Associate this role)
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

// Get the selected tool list (returns an array of toolKeys)
async function getSelectedRoleTools() {
    // Save the current page state first
    saveCurrentRolePageToolStates();
    
    // If there is no search keyword, all pages of tools must be loaded to ensure the state map is complete
    // But for performance, we can get only the selected tools from the state map
    // The problem: if the user selected tools only on some pages, the tool state of other pages may not be in the map
    
    // If the total number of tools exceeds the loaded count, we must ensure tools on all unloaded pages are also considered
    // But for role tool selection, we only need the tools the user has explicitly selected
    // So we can get the selected tools directly from the state map
    
    // Get all selected tools from the state map (returns only tools enabled in MCP management)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        // Return only tools enabled in MCP management and selected by the role
        if (state.enabled && state.mcpEnabled !== false) {
            selectedTools.push(toolKey);
        }
    });
    
    // If the user may have selected tools on other pages, we must ensure the current page state is also saved
    // But the state map should already contain the state of all visited pages
    
    return selectedTools;
}

// Set the selected tools (used when editing a role)
function setSelectedRoleTools(selectedToolKeys) {
    const selectedSet = new Set(selectedToolKeys || []);
    
    // Update the state map
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

// Show the add-role modal
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

    // When adding a role: show the tool-selection UI and hide the default-role hint
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

    // Reset the tool state
    roleToolStateMap.clear();
    roleConfiguredTools.clear(); // Clear the role config tool list
    roleUsesAllTools = false; // When adding a role, do not use all tools by default
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
    
    // Clear the tool-list DOM, to avoid saveCurrentRolePageToolStates in loadRoleTools reading stale state
    if (toolsList) {
        toolsList.innerHTML = '';
    }

    // Load and render the tool list
    await loadRoleTools(1, '');
    
    // Ensure the tool list is shown
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    
    // Ensure the statistics update correctly (showing0/108)
    updateRoleToolsStats();

    refreshRoleModalSelects();
    openAppModal('role-modal');
}

// Edit role
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
    document.getElementById('role-name').disabled = true; // The name cannot be changed when editing
    document.getElementById('role-description').value = role.description || '';
    // Handle the icon field: if it is a Unicode-escape format, convert it to an emoji; otherwise use it directly
    let iconValue = role.icon || '';
    if (iconValue && iconValue.startsWith('\\U')) {
        // Convert the Unicode-escape format (e.g. \U0001F3C6)isemoji
        try {
            const codePoint = parseInt(iconValue.substring(2), 16);
            iconValue = String.fromCodePoint(codePoint);
        } catch (e) {
            // If the conversion fails, use the original value
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
        // Default role: hide the tool-selection UI and show the hint
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
        // Non-default role: show the tool-selection UI and hide the hint
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

        // Reset the tool state
        roleToolStateMap.clear();
        roleConfiguredTools.clear(); // Clear the role config tool list
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

        // Prefer the tools field; if absent use the mcps field (backward compatible)
        const selectedTools = role.tools || (role.mcps && role.mcps.length > 0 ? role.mcps : []);
        
        // Determine whether all tools are used: if tools is not configured (or is an empty array), it means all tools are used
        roleUsesAllTools = !role.tools || role.tools.length === 0;
        
        // Save the role config tool list
        if (selectedTools.length > 0) {
            selectedTools.forEach(toolKey => {
                roleConfiguredTools.add(toolKey);
            });
        }
        
        // If there are selected tools, initialize the state map first
        if (selectedTools.length > 0) {
            roleUsesAllTools = false; // Has configured tools; does not use all tools
            // Add the selected tools to the state map (marked as selected)
            selectedTools.forEach(toolKey => {
                // If the map does not yet have this tool, create a default state first (enabled istrue)
                if (!roleToolStateMap.has(toolKey)) {
                    roleToolStateMap.set(toolKey, {
                        enabled: true,
                        is_external: false,
                        external_mcp: '',
                        name: toolKey.split('::').pop() || toolKey // Extract the tool name from the toolKey
                    });
                } else {
                    // If it already exists, update it to selected
                    const state = roleToolStateMap.get(toolKey);
                    state.enabled = true;
                }
            });
        }

        // Load the tool list (first page)
        await loadRoleTools(1, '');
        
        // If using all tools, mark all enabled tools on the current page as selected
        if (roleUsesAllTools) {
            // Mark all tools on the current page enabled in MCP management as selected
            document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
                const toolItem = checkbox.closest('.role-tool-item');
                if (toolItem) {
                    const toolKey = toolItem.dataset.toolKey;
                    const toolName = toolItem.dataset.toolName;
                    const isExternal = toolItem.dataset.isExternal === 'true';
                    const externalMcp = toolItem.dataset.externalMcp || '';
                    if (toolKey) {
                        const state = roleToolStateMap.get(toolKey);
                        // Only select tools enabled in MCP management
                        // If the state exists, use its mcpEnabled; otherwise assume enabled (since loadRoleTools should already have initialized all tools)
                        const shouldEnable = state ? (state.mcpEnabled !== false) : true;
                        checkbox.checked = shouldEnable;
                        if (state) {
                            state.enabled = shouldEnable;
                        } else {
                            // If the state does not exist, create a new one (this should not happen, since loadRoleTools should have already initialized it)
                            roleToolStateMap.set(toolKey, {
                                enabled: shouldEnable,
                                is_external: isExternal,
                                external_mcp: externalMcp,
                                name: toolName,
                                mcpEnabled: true // Assume enabled; the actual value is updated in loadRoleTools
                            });
                        }
                    }
                }
            });
            // Update the statistics to ensure the correct selected count is shown
            updateRoleToolsStats();
        } else if (selectedTools.length > 0) {
            // After loading, set the selected state again (to ensure the current page tools are also set correctly)
            setSelectedRoleTools(selectedTools);
        }
    }

    refreshRoleModalSelects();
    openAppModal('role-modal');
}

// Close the role modal
function closeRoleModal() {
    closeAllRoleModalSelects();
    closeAppModal('role-modal');
}

function closeRoleSelectModal() {
    closeAppModal('role-select-modal');
}

// Get all selected tools (including those not enabled in MCP management)
function getAllSelectedRoleTools() {
    // Save the current page state first
    saveCurrentRolePageToolStates();
    
    // Get all selected tools from the state map (regardless of whether they are enabled in MCP management)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        if (state.enabled) {
            selectedTools.push({
                key: toolKey,
                name: state.name || toolKey.split('::').pop() || toolKey,
                mcpEnabled: state.mcpEnabled !== false // mcpEnabled false means disabled; otherwise treated as enabled
            });
        }
    });
    
    return selectedTools;
}

// Check for and get tools not enabled in MCP management
function getDisabledTools(selectedTools) {
    return selectedTools.filter(tool => {
        const state = roleToolStateMap.get(tool.key);
        // If mcpEnabled is explicitly false, treat it as disabled
        return state && state.mcpEnabled === false;
    });
}

// Load all tools into the state map (used when switching from all tools to a subset)
async function loadAllToolsToStateMap() {
    try {
        const pageSize = 100; // Use a larger page size to reduce the number of requests
        let page = 1;
        let hasMore = true;
        
        // Iterate over all pages to get all tools
        while (hasMore) {
            const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error('Failed to get the tool list');
            }
            
            const result = await response.json();
            
            // Add all tools to the state map
            result.tools.forEach(tool => {
                const toolKey = getToolKey(tool);
                if (!roleToolStateMap.has(toolKey)) {
                    // The tool is not in the map; initialize it based on the current mode
                    let enabled = false;
                    if (roleUsesAllTools) {
                        // If using all tools and the tool is enabled in MCP management, mark it as selected
                        enabled = tool.enabled ? true : false;
                    } else {
                        // If not using all tools, only tools in the role config tool list are marked as selected
                        enabled = roleConfiguredTools.has(toolKey);
                    }
                    roleToolStateMap.set(toolKey, {
                        enabled: enabled,
                        is_external: tool.is_external || false,
                        external_mcp: tool.external_mcp || '',
                        name: tool.name,
                        mcpEnabled: tool.enabled // Save the original enabled state from MCP management
                    });
                } else {
                    // The tool is already in the map; update other properties but keep the enabled state
                    const state = roleToolStateMap.get(toolKey);
                    state.is_external = tool.is_external || false;
                    state.external_mcp = tool.external_mcp || '';
                    state.mcpEnabled = tool.enabled; // Update the original enabled state from MCP management
                    if (!state.name || state.name === toolKey.split('::').pop()) {
                        state.name = tool.name; // Update the tool name
                    }
                }
            });
            
            // Check whether there are more pages
            if (page >= result.total_pages) {
                hasMore = false;
            } else {
                page++;
            }
        }
    } catch (error) {
        console.error('Failed to load all tools into the state map:', error);
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
    // Convert the emoji to a Unicode-escape format to match the YAML format (e.g. \U0001F3C6)
    if (icon) {
        // Get the Unicode code point of the first character (handling the case where an emoji may be multiple characters)
        const codePoint = icon.codePointAt(0);
        if (codePoint && codePoint > 0x7F) {
            // Convert to an 8-digit hexadecimal format (\U0001F3C6)
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
    
    // Check whether this is the first role being added (after excluding the default role, there are no user-created roles)
    const isFirstUserRole = !isEdit && !isDefaultRole && roles.filter(r => r.name !== 'Default').length === 0;
    
    // The default role does not save the tools field (uses all tools)
    // Non-default role: if it uses all tools (roleUsesAllTools is true), the tools field is also not saved
    let tools = [];
    let disabledTools = []; // Store tools not enabled in MCP management
    
    if (!isDefaultRole) {
        // Save the current page state
        saveCurrentRolePageToolStates();
        
        // Collect all selected tools (including those not enabled in MCP management)
        let allSelectedTools = getAllSelectedRoleTools();
        
        // If adding the first role and no tools are selected, use all tools by default
        if (isFirstUserRole && allSelectedTools.length === 0) {
            roleUsesAllTools = true;
            showNotification(_t('roleModal.firstRoleNoToolsHint'), 'info');
        } else if (roleUsesAllTools) {
            // If currently using all tools, check whether the user has deselected some tools
            // Check whether the state map has any unselected enabled tools
            let hasUnselectedTools = false;
            roleToolStateMap.forEach((state) => {
                // If the tool is enabled in MCP management but not selected, the user has deselected it
                if (state.mcpEnabled !== false && !state.enabled) {
                    hasUnselectedTools = true;
                }
            });
            
            // If the user deselected some enabled tools, switch to the subset-of-tools mode
            if (hasUnselectedTools) {
                // Before switching, all tools must be loaded into the state map
                // This way we can correctly save the state of all tools (except those the user deselected)
                await loadAllToolsToStateMap();
                
                // Mark all enabled tools as selected (except those the user already deselected)
                // Tools the user has deselected have enabled=false in the state map and stay unchanged
                roleToolStateMap.forEach((state, toolKey) => {
                    // If the tool is enabled in MCP management and is not explicitly marked as unselected in the state map (i.e. enabled is notfalse)
                    // then mark as selected
                    if (state.mcpEnabled !== false && state.enabled !== false) {
                        state.enabled = true;
                    }
                });
                
                roleUsesAllTools = false;
            } else {
                // Even when using all tools, all tools must be loaded into the state map so we can check whether any disabled tool is selected
                // This lets us detect whether the user manually selected some disabled tools
                await loadAllToolsToStateMap();
                
                // Check whether any disabled tool was manually selected (enabled is true but mcpEnabled isfalse)
                let hasDisabledToolsSelected = false;
                roleToolStateMap.forEach((state) => {
                    if (state.enabled && state.mcpEnabled === false) {
                        hasDisabledToolsSelected = true;
                    }
                });
                
                // If no disabled tool is selected, mark all enabled tools as selected (this is the default behavior of using all tools)
                if (!hasDisabledToolsSelected) {
                    roleToolStateMap.forEach((state) => {
                        if (state.mcpEnabled !== false) {
                            state.enabled = true;
                        }
                    });
                }
                
                // Update allSelectedTools, since the state map now contains all tools
                allSelectedTools = getAllSelectedRoleTools();
            }
        }
        
        // Check which tools are not enabled in MCP management (checked regardless of whether all tools are used)
        disabledTools = getDisabledTools(allSelectedTools);
        
        // If there are disabled tools, notify the user
        if (disabledTools.length > 0) {
            const toolNames = disabledTools.map(t => t.name).join(', ');
            const message = `the following ${disabledTools.length} tools are not enabled in MCP management and cannot be configured in the role: \n\n${toolNames}\n\n First, in"MCP Management"enable these tools there, then configure them in the role.\n\n Continue saving? (only enabled tools will be saved)`;
            
            if (!confirm(message)) {
                return; // The user cancelled saving
            }
        }
        
        // If using all tools, there is no need to fetch the tool list
        if (!roleUsesAllTools) {
            // Get the selected tool list (only tools enabled in MCP management)
            tools = await getSelectedRoleTools();
        }
    }

    const roleData = {
        name: name,
        description: description,
        icon: icon || undefined, // If it is an empty string, do not send the field
        user_prompt: userPrompt,
        tools: tools, // The default role is an empty array, meaning all tools are used
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
            throw new Error(error.error || 'Failed to save the role');
        }

        // If any disabled tools were filtered out, notify the user
        if (disabledTools.length > 0) {
            let toolNames = disabledTools.map(t => t.name).join(', ');
            // If the tool-name list is too long, truncate the display
            if (toolNames.length > 100) {
                toolNames = toolNames.substring(0, 100) + '...';
            }
            showNotification(
                `${isEdit ? 'Role updated' : 'Role created'}, but filtered ${disabledTools.length} tools not enabled in MCP management: ${toolNames}.First, in"MCP Management"enable these tools there, then configure them in the role.`,
                'warning'
            );
        } else {
            showNotification(isEdit ? 'Role updated' : 'Role created', 'success');
        }
        
        closeRoleModal();
        await refreshRoles();
    } catch (error) {
        console.error('Failed to save the role:', error);
        showNotification('Failed to save the role: ' + error.message, 'error');
    }
}

// Delete role
async function deleteRole(roleName) {
    if (roleName === 'Default') {
        showNotification(_t('roleModal.cannotDeleteDefaultRole'), 'error');
        return;
    }

    if (!confirm(`Are you sure you want to delete the role"${roleName}"? This action cannot be undone.`)) {
        return;
    }

    try {
        const response = await apiFetch(`/api/roles/${encodeURIComponent(roleName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete the role');
        }

        showNotification('Role deleted', 'success');
        
        // If the deleted role is the currently selected one, switch to the default role
        if (currentRole === roleName) {
            handleRoleChange('');
        }

        await refreshRoles();
    } catch (error) {
        console.error('Failed to delete the role:', error);
        showNotification('Failed to delete the role: ' + error.message, 'error');
    }
}

// Initialize the role list on page switch
if (typeof window.switchPage === 'function') {
    const originalSwitchPage = window.switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        if (page === 'roles-management') {
            loadRoles().then(() => renderRolesList());
        }
    };
}

// Click outside the modal to close
document.addEventListener('click', (e) => {
    const roleSelectModal = document.getElementById('role-select-modal');
    if (roleSelectModal && e.target === roleSelectModal) {
        closeRoleSelectModal();
    }

    const roleModal = document.getElementById('role-modal');
    if (roleModal && e.target === roleModal) {
        closeRoleModal();
    }

    // Click outside the role-selection panel to close (must use #role-selector-wrapper, Do not use .role-selector-wrapper: the project selector also carries that class)
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

// After a language switch, refresh the role selector and the Select Role list text
document.addEventListener('languagechange', () => {
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar();
    syncAllRoleModalSelects();
});

// Get the currently selected role (for chat.js to use)
function getCurrentRole() {
    return currentRole || '';
}

// Expose the function to the global scope
if (typeof window !== 'undefined') {
    window.getCurrentRole = getCurrentRole;
    window.setCurrentRole = handleRoleChange;
    window.toggleRoleSelectionPanel = toggleRoleSelectionPanel;
    window.closeRoleSelectionPanel = closeRoleSelectionPanel;
    window.closeRoleSelectModal = closeRoleSelectModal;
    window.filterRoleToolsByStatus = filterRoleToolsByStatus;
    window.refreshRoleModalSelects = refreshRoleModalSelects;
    window.currentSelectedRole = getCurrentRole();
    
    // Listen for role changes and update the global variable
    const originalHandleRoleChange = handleRoleChange;
    handleRoleChange = function(roleName) {
        originalHandleRoleChange(roleName);
        if (typeof window !== 'undefined') {
            window.currentSelectedRole = getCurrentRole();
            window.setCurrentRole = handleRoleChange;
        }
    };
}
