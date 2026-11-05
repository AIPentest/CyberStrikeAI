let rbacState = {
    users: [],
    roles: [],
    permissions: {},
    assignments: [],
    selectedUserId: '',
    mainView: 'users',
    activeTab: 'roles',
    userSearch: '',
    roleSearch: '',
    permissionSearch: '',
    rolePermissionSelection: new Set(),
    resourceOptions: [],
    selectedResourceIds: new Set(),
    selectedResourceMeta: new Map(),
    resourceSearchTimer: null,
    resourceRequestSeq: 0,
    resourcePage: 0,
    resourcePageSize: 8,
    resourceHasMore: false,
    resourceTotal: 0,
    assignmentSearch: '',
    assignmentType: 'all',
    assignmentPage: 0,
    assignmentPageSize: 8,
    assignmentResourceType: 'conversation',
    showEffectivePermissions: false,
    auditLogs: [],
    auditLoading: false,
    auditPage: 0,
    auditPageSize: 20,
    auditTotal: 0,
    auditAction: '',
    auditResourceType: '',
    pendingRoleUserId: '',
    pendingUserRoles: new Set(),
    editingRoleIsSystem: false,
    savingRoleId: '',
};

const rbacScopeMeta = {
    all: { tone: 'is-warn' },
    assigned: { tone: 'is-info' },
    own: { tone: '' },
};

const rbacResourceLabels = {
    user: 'Platform user',
    project: 'project', conversation: 'dialogue', vulnerability: 'loophole', webshell: 'WebShell connection',
    batch_task: 'Batch task', c2_listener: 'C2 listener', asset: 'asset',
};

function rbacText(value, fallback = '') {
    return value == null || value === '' ? fallback : String(value);
}

function rbacEscape(value) {
    if (typeof escapeHtml === 'function') return escapeHtml(rbacText(value));
    const div = document.createElement('div');
    div.textContent = rbacText(value);
    return div.innerHTML;
}

function rbacT(key, fallback) {
    const options = arguments.length > 2 ? arguments[2] : undefined;
    const translator = typeof window !== 'undefined' && typeof window.t === 'function' ? window.t : null;
    if (translator) {
        const translated = translator(key, options);
        if (translated && translated !== key) return translated;
    }
    return fallback;
}

function rbacPermissionLabel(key) {
    const parts = String(key || '').split(':');
    return rbacT(`rbac.permissionDescriptions.${parts[0] || 'other'}.${parts[1] || 'unknown'}`, rbacState.permissions[key] || key);
}

function rbacPermissionModuleLabel(module) {
    return rbacT(`rbac.permissionModules.${module}`, module);
}

function rbacResourceLabel(type) {
    return rbacT(`rbac.resourceTypes.${type}`, rbacResourceLabels[type] || type);
}

function rbacRoleName(role) {
    if (!role) return '';
    return rbacRoleIsSystem(role) ? rbacT(`rbac.systemRoles.${role.id}.name`, role.name) : role.name;
}

function rbacRoleDescription(role) {
    if (!role) return '';
    return rbacRoleIsSystem(role) ? rbacT(`rbac.systemRoles.${role.id}.description`, role.description || '') : (role.description || '');
}

function rbacShortId(value) {
    const id = rbacText(value);
    if (id.length <= 16) return id;
    return `${id.slice(0, 8)}…${id.slice(-4)}`;
}

function rbacFormatResourceDetail(type, detail) {
    const text = rbacText(detail);
    if (!text) return '';
    if (type === 'conversation' && text) {
        return rbacT('rbac.resourceDetailProject', 'Parent project {{id}}', { id: rbacShortId(text) });
    }
    return text;
}

async function rbacCopyResourceId(id) {
    const value = rbacText(id);
    if (!value) return;
    try {
        if (navigator.clipboard && navigator.clipboard.writeText) {
            await navigator.clipboard.writeText(value);
        } else {
            const input = document.createElement('textarea');
            input.value = value;
            document.body.appendChild(input);
            input.select();
            document.execCommand('copy');
            document.body.removeChild(input);
        }
        rbacNotify(rbacT('rbac.copiedId', 'Copied'), 'success');
    } catch (error) {
        rbacNotify(`${rbacT('rbac.copyId', 'Copy ID')}: ${error.message || error}`, 'error');
    }
}

function rbacPendingSelectionCount() {
    const manualIds = document.getElementById('rbac-assignment-id')?.value.split(/[\s,，;；]+/).filter(Boolean) || [];
    return new Set([...rbacState.selectedResourceIds, ...manualIds]).size;
}

function rbacRememberSelectedResource(resource) {
    if (!resource || !resource.id) return;
    rbacState.selectedResourceMeta.set(resource.id, {
        label: resource.label || resource.id,
        detail: resource.detail || '',
        type: document.getElementById('rbac-assignment-type')?.value || '',
    });
}

function rbacSelectedResourceMeta(resourceId) {
    if (rbacState.selectedResourceMeta.has(resourceId)) {
        return rbacState.selectedResourceMeta.get(resourceId);
    }
    const cached = rbacState.resourceOptions.find(item => item.id === resourceId);
    if (cached) {
        return {
            label: cached.label || cached.id,
            detail: cached.detail || '',
            type: document.getElementById('rbac-assignment-type')?.value || '',
        };
    }
    return { label: resourceId, detail: '', type: document.getElementById('rbac-assignment-type')?.value || '' };
}

function clearRbacResourceSelection() {
    rbacState.selectedResourceIds.clear();
    rbacState.selectedResourceMeta.clear();
    const manual = document.getElementById('rbac-assignment-id');
    if (manual) manual.value = '';
    renderRbacResourcePicker();
    syncRbacAssignmentSubmit();
}

function renderRbacNoRoleNotice(user) {
    const box = document.getElementById('rbac-no-role-notice');
    if (!box) return;
    const hasRoles = user && rbacUserRoles(user).length > 0;
    if (!user || hasRoles) {
        box.hidden = true;
        box.innerHTML = '';
        return;
    }
    box.hidden = false;
    box.innerHTML = `
        <div class="rbac-no-role-notice-copy">
            <strong>${rbacEscape(rbacT('rbac.noRoleNoticeTitle', 'Role not yet assigned'))}</strong>
            <span>${rbacEscape(rbacT('rbac.noRoleNoticeHint', 'Please assign roles first, then configure resource authorization. Without a role, you can only access data you created yourself, and most functions are unavailable.'))}</span>
        </div>
        <button type="button" class="btn-secondary btn-small" onclick="focusRbacRoleAssignment()">${rbacEscape(rbacT('rbac.assignRolesNow', 'Go assign roles'))}</button>`;
}

function renderRbacPendingSelection() {
    const bar = document.getElementById('rbac-pending-bar');
    const list = document.getElementById('rbac-pending-list');
    const count = document.getElementById('rbac-pending-count');
    const manualIds = document.getElementById('rbac-assignment-id')?.value.split(/[\s,，;；]+/).map(id => id.trim()).filter(Boolean) || [];
    const selectedIds = Array.from(rbacState.selectedResourceIds);
    const total = new Set([...selectedIds, ...manualIds]).size;
    if (count) count.textContent = String(total);
    if (!bar || !list) return;
    if (total === 0) {
        bar.hidden = true;
        list.innerHTML = '';
        return;
    }
    bar.hidden = false;
    const resourceType = document.getElementById('rbac-assignment-type')?.value || '';
    const items = [
        ...selectedIds.map(id => ({ id, meta: rbacSelectedResourceMeta(id), manual: false })),
        ...manualIds.filter(id => !rbacState.selectedResourceIds.has(id)).map(id => ({ id, meta: { label: id, detail: rbacT('rbac.advancedAssignment', 'Advanced: directly input resource ID'), type: resourceType }, manual: true })),
    ];
    list.innerHTML = items.map(item => `
        <div class="rbac-pending-item">
            <div class="rbac-pending-item-main">
                <strong>${rbacEscape(item.meta.label || item.id)}</strong>
                ${(item.meta.label || item.id) === item.id ? '' : `<small title="${rbacEscape(item.id)}">${rbacEscape(rbacShortId(item.id))}</small>`}
            </div>
            <button type="button" class="rbac-pending-remove" data-resource-id="${rbacEscape(item.id)}" data-manual="${item.manual ? 'true' : 'false'}" onclick="removeRbacPendingItem(this)" title="${rbacEscape(rbacT('rbac.removePending', 'Remove'))}" aria-label="${rbacEscape(rbacT('rbac.removePending', 'Remove'))}">×</button>
        </div>`).join('');
}

function removeRbacPendingItem(button) {
    const resourceId = button?.dataset?.resourceId || '';
    if (!resourceId) return;
    if (button.dataset.manual === 'true') removeRbacManualPending(resourceId);
    else toggleRbacResourceSelection(resourceId, false);
}

function removeRbacManualPending(resourceId) {
    const input = document.getElementById('rbac-assignment-id');
    if (!input) return;
    const ids = input.value.split(/[\s,，;；]+/).map(id => id.trim()).filter(Boolean);
    input.value = ids.filter(id => id !== resourceId).join(', ');
    syncRbacAssignmentSubmit();
}

function rbacNotify(message, type = 'info') {
    if (typeof notifyApiError === 'function') {
        notifyApiError(message, type);
        return;
    }
    if (typeof showNotification === 'function') showNotification(message, type);
    else if (type === 'error') alert(message);
}

function rbacUserDisplayName(user) {
    // Seed the display_name for the built-in admin account (rbac.go 190); translate according to the UI language only when it exactly matches the seed value.
    if (user && user.id === 'admin') {
        const raw = String(user.displayName || user.display_name || '').trim();
        if (raw === 'administrator') {
            const translated = rbacT('rbac.builtinAdminDisplayName', '');
            if (translated) return translated;
        }
    }
    return (user && (user.displayName || user.display_name || user.username)) || '';
}

function rbacUserIsBuiltin(user) {
    return !!(user && (user.isBuiltin || user.is_builtin || user.id === 'admin'));
}

function rbacUserRoles(user) {
    return Array.isArray(user && user.roles) ? user.roles : [];
}

function resolveUserEffectiveAccess(user) {
    const userRoles = user ? rbacUserRoles(user) : [];
    const effectivePermissions = new Set();
    const permissionsByModule = {};
    let effectiveScope = 'own';
    userRoles.forEach(roleId => {
        const role = rbacState.roles.find(r => r.id === roleId);
        (role && role.permissions || []).forEach(permission => {
            effectivePermissions.add(permission);
            const module = permission.split(':')[0] || 'other';
            if (!permissionsByModule[module]) permissionsByModule[module] = [];
            permissionsByModule[module].push(permission);
        });
        if (role && role.scope === 'all') effectiveScope = 'all';
        else if (role && role.scope === 'assigned' && effectiveScope !== 'all') effectiveScope = 'assigned';
    });
    const assignmentCount = user
        ? rbacState.assignments.filter(a => (a.userId || a.user_id) === user.id).length
        : 0;
    return { userRoles, effectivePermissions, permissionsByModule, effectiveScope, assignmentCount };
}

let rbacPermissionsPopoverDocBound = false;

function positionRbacPermissionsPopover() {
    const popover = document.getElementById('rbac-permissions-popover');
    const btn = document.querySelector('.rbac-summary-count--interactive');
    if (!popover || !btn || popover.hidden) return;
    const rect = btn.getBoundingClientRect();
    const width = Math.min(380, window.innerWidth - 32);
    let left = rect.left;
    if (left + width > window.innerWidth - 16) left = window.innerWidth - width - 16;
    left = Math.max(16, left);
    const top = Math.min(rect.bottom + 8, window.innerHeight - 16);
    popover.style.width = `${width}px`;
    popover.style.left = `${left}px`;
    popover.style.top = `${top}px`;
    const arrowLeft = Math.min(Math.max(rect.left + rect.width / 2 - left - 5, 12), width - 22);
    popover.style.setProperty('--rbac-popover-arrow-left', `${arrowLeft}px`);
}

function onRbacPermissionsPopoverReposition() {
    if (rbacState.showEffectivePermissions) positionRbacPermissionsPopover();
}

function setRbacPermissionsPopoverOpen(open) {
    rbacState.showEffectivePermissions = open;
    const popover = document.getElementById('rbac-permissions-popover');
    const btn = document.querySelector('.rbac-summary-count--interactive');
    if (popover) popover.hidden = !open;
    if (btn) btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    if (open) {
        requestAnimationFrame(() => positionRbacPermissionsPopover());
    }
}

function closeRbacPermissionsPopover() {
    setRbacPermissionsPopoverOpen(false);
}

function toggleRbacEffectivePermissions(ev) {
    if (ev) {
        ev.preventDefault();
        ev.stopPropagation();
    }
    const opening = !rbacState.showEffectivePermissions;
    if (opening) renderRbacEffectivePermissionsContent(selectedRbacUser());
    setRbacPermissionsPopoverOpen(opening);
}

function onRbacPermissionsPopoverPointerDown(ev) {
    if (!rbacState.showEffectivePermissions) return;
    const popover = document.getElementById('rbac-permissions-popover');
    const btn = document.querySelector('.rbac-summary-count--interactive');
    if (popover && popover.contains(ev.target)) return;
    if (btn && btn.contains(ev.target)) return;
    closeRbacPermissionsPopover();
}

function bindRbacPermissionsPopoverDismiss() {
    if (rbacPermissionsPopoverDocBound) return;
    rbacPermissionsPopoverDocBound = true;
    document.addEventListener('mousedown', onRbacPermissionsPopoverPointerDown);
    document.addEventListener('keydown', ev => {
        if (ev.key === 'Escape' && rbacState.showEffectivePermissions) closeRbacPermissionsPopover();
    });
    window.addEventListener('resize', onRbacPermissionsPopoverReposition);
    window.addEventListener('scroll', onRbacPermissionsPopoverReposition, true);
}

function renderRbacEffectivePermissionsContent(user) {
    const popover = document.getElementById('rbac-permissions-popover');
    if (!popover || !user) return;
    const access = resolveUserEffectiveAccess(user);
    const modules = Object.keys(access.permissionsByModule).sort();
    if (!modules.length) {
        popover.innerHTML = `<div class="rbac-permissions-popover-inner"><div class="rbac-empty"><strong>${rbacT('rbac.empty.noEffectivePermissions', 'No valid permissions available')}</strong><span>${rbacEscape(rbacT('rbac.empty.assignRoleFirst', 'Once a role is assigned, you can view permission details'))}</span></div></div>`;
        return;
    }
    popover.innerHTML = `
        <div class="rbac-permissions-popover-inner">
            <div class="rbac-effective-permissions-head">
                <strong>${rbacEscape(rbacT('rbac.effectivePermissionsDetail', 'Effective Permission Details'))}</strong>
                <span class="rbac-permissions-popover-count">${access.effectivePermissions.size}</span>
            </div>
            <div class="rbac-effective-permissions-body">
                ${modules.map(module => `
                    <section class="rbac-effective-module">
                        <h4>${rbacEscape(rbacPermissionModuleLabel(module))}<span>${access.permissionsByModule[module].length}</span></h4>
                        <div class="rbac-effective-permission-tags">
                            ${access.permissionsByModule[module].sort().map(key => `<span title="${rbacEscape(rbacPermissionLabel(key))}">${rbacEscape(rbacPermissionLabel(key))}</span>`).join('')}
                        </div>
                    </section>`).join('')}
            </div>
        </div>`;
}

function renderRbacEffectivePermissions(user) {
    const popover = document.getElementById('rbac-permissions-popover');
    const btn = document.querySelector('.rbac-summary-count--interactive');
    if (!popover) return;
    if (!user) {
        popover.hidden = true;
        popover.innerHTML = '';
        rbacState.showEffectivePermissions = false;
        if (btn) btn.setAttribute('aria-expanded', 'false');
        return;
    }
    if (btn) btn.setAttribute('aria-expanded', rbacState.showEffectivePermissions ? 'true' : 'false');
    popover.hidden = !rbacState.showEffectivePermissions;
    if (rbacState.showEffectivePermissions) {
        renderRbacEffectivePermissionsContent(user);
        requestAnimationFrame(() => positionRbacPermissionsPopover());
    }
}

function rbacRoleIsSystem(role) {
    return !!(role && (role.isSystem || role.is_system));
}

async function rbacRun(action, fallbackMessage) {
    try {
        await action();
        return true;
    } catch (error) {
        rbacNotify(`${fallbackMessage}: ${error.message || error}`, 'error');
        return false;
    }
}

async function initPlatformRbacPage() {
    if (typeof window !== 'undefined' && window.i18nReady) await window.i18nReady;
    const typeInput = document.getElementById('rbac-assignment-type');
    if (typeInput) rbacState.assignmentResourceType = typeInput.value || 'conversation';
    bindRbacPermissionsPopoverDismiss();
    await rbacRun(loadPlatformRbac, rbacT('rbac.errors.loadFailed', 'Load failed'));
    initRbacSelects();
}

function initRbacSelects() {
    if (typeof window.initSettingsCustomSelects !== 'function') return;
    ['page-platform-rbac', 'rbac-user-modal', 'rbac-role-modal'].forEach(id => {
        const root = document.getElementById(id);
        if (root) window.initSettingsCustomSelects(root);
    });
    if (typeof window.refreshSettingsCustomSelects === 'function') {
        window.refreshSettingsCustomSelects();
    }
}

async function refreshRbacAssignments() {
    const res = await apiFetch('/api/rbac/resource-assignments');
    const result = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(result.error || rbacT('rbac.errors.loadDataFailed', 'Unable to load permission data'));
    rbacState.assignments = result.assignments || [];
    renderRbacOverview();
    renderRbacUsers();
    renderRbacAssignments();
}

async function refreshRbacUserRoles(userId, roles) {
    const user = rbacState.users.find(item => item.id === userId);
    if (user) user.roles = roles;
    rbacState.pendingRoleUserId = '';
    ensurePendingUserRoles(selectedRbacUser());
    renderRbacOverview();
    renderRbacUsers();
    renderRbacRoles();
}

async function loadPlatformRbac() {
    const responses = await Promise.all([
        apiFetch('/api/rbac/metadata'),
        apiFetch('/api/rbac/users'),
        apiFetch('/api/rbac/roles'),
        apiFetch('/api/rbac/resource-assignments'),
    ]);
    const payloads = await Promise.all(responses.map(response => response.json().catch(() => ({}))));
    const failedIndex = responses.findIndex(response => !response.ok);
    if (failedIndex >= 0) {
        throw new Error(payloads[failedIndex].error || rbacT('rbac.errors.loadDataFailed', 'Unable to load permission data'));
    }
    const [meta, users, roles, assignments] = payloads;
    rbacState.permissions = meta.permissions || {};
    rbacState.users = users.users || [];
    rbacState.roles = roles.roles || [];
    rbacState.assignments = assignments.assignments || [];
    if (!rbacState.selectedUserId && rbacState.users.length) {
        rbacState.selectedUserId = rbacState.users[0].id;
    }
    if (rbacState.selectedUserId && !rbacState.users.some(u => u.id === rbacState.selectedUserId)) {
        rbacState.selectedUserId = rbacState.users[0] ? rbacState.users[0].id : '';
    }
    renderRbac();
    if (rbacState.mainView === 'users' && rbacState.activeTab === 'assignments') {
        rbacState.resourcePage = 0;
        await loadRbacResourceOptions();
    }
}

async function refreshPlatformRbac(button) {
    const originalText = button ? button.textContent : '';
    if (button) {
        button.disabled = true;
        button.textContent = rbacT('common.loading', 'Refreshing…');
    }
    try {
        await loadPlatformRbac();
        initRbacSelects();
    } catch (error) {
        rbacNotify(`${rbacT('rbac.errors.loadFailed', 'Refresh failed')}: ${error.message || error}`, 'error');
    } finally {
        if (button) {
            button.disabled = false;
            button.textContent = originalText || rbacT('common.refresh', 'refresh');
        }
    }
}

function selectedRbacUser() {
    return rbacState.users.find(u => u.id === rbacState.selectedUserId) || null;
}

function renderRbac() {
    renderRbacOverview();
    renderRbacUsers();
    renderRbacRoles();
    renderRbacAssignments();
    renderRbacRoleCatalog();
    switchRbacView(rbacState.mainView);
}

function switchRbacView(view) {
    rbacState.mainView = view === 'roles' ? 'roles' : 'users';
    document.querySelectorAll('.rbac-workspace-tab').forEach(button => {
        const active = button.dataset.rbacView === rbacState.mainView;
        button.classList.toggle('is-active', active);
        button.setAttribute('aria-selected', String(active));
    });
    const usersView = document.getElementById('rbac-view-users');
    const rolesView = document.getElementById('rbac-view-roles');
    if (usersView) usersView.hidden = rbacState.mainView !== 'users';
    if (rolesView) rolesView.hidden = rbacState.mainView !== 'roles';
}

function renderRbacOverview() {
    const enabled = rbacState.users.filter(user => user.enabled).length;
    const setText = (id, value) => {
        const el = document.getElementById(id);
        if (el) el.textContent = String(value);
    };
    setText('rbac-metric-users', rbacState.users.length);
    setText('rbac-metric-enabled', enabled);
    setText('rbac-metric-roles', rbacState.roles.length);
    setText('rbac-metric-assignments', rbacState.assignments.length);
    const enabledMetric = document.querySelector('.rbac-metric-enabled');
    if (enabledMetric) {
        const allEnabled = rbacState.users.length > 0 && enabled === rbacState.users.length;
        enabledMetric.hidden = allEnabled;
    }
    const user = selectedRbacUser();
    const context = document.getElementById('rbac-metric-context');
    if (context) {
        if (!user) {
            context.hidden = true;
            context.innerHTML = '';
        } else {
            const access = resolveUserEffectiveAccess(user);
            context.hidden = false;
            context.innerHTML = `
                <span class="rbac-context-label">${rbacEscape(rbacT('rbac.currentMemberLabel', 'Current member'))}</span>
                <strong class="rbac-context-member" title="${rbacEscape(rbacUserDisplayName(user))}">${rbacEscape(rbacUserDisplayName(user))}</strong>
                <span class="rbac-context-stat"><b>${access.userRoles.length}</b>${rbacEscape(rbacT('rbac.metricRolesPill', 'character'))}</span>
                <span class="rbac-context-stat"><b>${access.assignmentCount}</b>${rbacEscape(rbacT('rbac.metricAssignmentsPill', 'item authorization'))}</span>`;
        }
    }
}

function renderRbacUsers() {
    const list = document.getElementById('rbac-users-list');
    const count = document.getElementById('rbac-users-count');
    if (count) count.textContent = String(rbacState.users.length);
    if (!list) return;
    const keyword = rbacState.userSearch.trim().toLowerCase();
    const users = keyword
        ? rbacState.users.filter(user => `${user.username || ''} ${rbacUserDisplayName(user)}`.toLowerCase().includes(keyword))
        : rbacState.users;
    if (!users.length) {
        list.innerHTML = `<div class="rbac-empty"><strong>${keyword ? rbacT('rbac.empty.noMatchingUsers', 'No matching members') : rbacT('rbac.empty.noUsers', 'No platform members yet')}</strong><span>${keyword ? rbacT('rbac.empty.tryAnotherKeyword', 'Try other keywords') : rbacT('rbac.empty.addFirstUser', 'Click "Add Member" in the top right corner to start configuring')}</span></div>`;
        return;
    }
    list.innerHTML = users.map(user => {
        const active = user.id === rbacState.selectedUserId ? ' is-active' : '';
        const roleNames = rbacUserRoles(user).map(roleId => {
            const role = rbacState.roles.find(r => r.id === roleId);
            return role ? rbacRoleName(role) : roleId;
        }).join(' / ');
        const assignmentCount = rbacState.assignments.filter(a => (a.userId || a.user_id) === user.id).length;
        const summary = `${roleNames || rbacT('rbac.noRoleAssigned', 'Unassigned roles')} · ${assignmentCount ? rbacT('rbac.resourceCount', '{{count}} resources', { count: assignmentCount }) : rbacT('rbac.noExplicitGrant', 'No separate authorization')}`;
        const handle = `@${user.username}${rbacUserIsBuiltin(user) ? ` · ${rbacT('rbac.builtinAccount', 'built-in account')}` : ''}`;
        return `
            <button type="button" class="rbac-user-row${active}" onclick="selectRbacUser('${rbacEscape(user.id)}')" title="${rbacEscape(`${rbacUserDisplayName(user)} (${handle}) — ${summary}`)}">
                <strong class="rbac-user-name">${rbacEscape(rbacUserDisplayName(user))}</strong>
                <span class="rbac-pill ${user.enabled ? 'is-ok' : 'is-muted'}">${user.enabled ? rbacT('rbac.statusEnabled', 'Enable') : rbacT('rbac.statusDisabled', 'Deactivate')}</span>
                <small class="rbac-user-handle">${rbacEscape(handle)}</small>
                <span class="rbac-user-summary">${rbacEscape(summary)}</span>
            </button>`;
    }).join('');
}

function setRbacUserSearch(value) {
    rbacState.userSearch = value || '';
    renderRbacUsers();
}

function selectRbacUser(userId) {
    if (userId !== rbacState.selectedUserId && selectedUserRolesAreDirty() &&
        !confirm(rbacT('rbac.confirmDiscardSwitchUser', 'The current role changes have not been saved yet. Are you sure you want to discard them and switch to another member?'))) return;
    rbacState.selectedUserId = userId;
    rbacState.pendingRoleUserId = '';
    rbacState.pendingUserRoles.clear();
    rbacState.selectedResourceIds.clear();
    rbacState.selectedResourceMeta.clear();
    rbacState.resourcePage = 0;
    rbacState.assignmentPage = 0;
    rbacState.auditPage = 0;
    rbacState.showEffectivePermissions = false;
    renderRbac();
    if (rbacState.activeTab === 'assignments') loadRbacResourceOptions();
    if (rbacState.activeTab === 'audit') loadRbacUserAuditLogs();
}

function setRbacRoleSearch(value) {
    rbacState.roleSearch = value || '';
    renderRbacRoleCatalog();
}

function rbacScopeInfo(scope) {
    const meta = rbacScopeMeta[scope] || { tone: '' };
    return {
        tone: meta.tone,
        label: rbacT(`rbac.scopes.${scope}.label`, scope || rbacT('rbac.notConfigured', 'Not set')),
        hint: rbacT(`rbac.scopes.${scope}.hint`, ''),
    };
}

function renderRbacRoleCatalog() {
    const box = document.getElementById('rbac-role-catalog');
    if (!box) return;
    const query = rbacState.roleSearch.trim().toLowerCase();
    const roles = rbacState.roles.filter(role => {
        const permissionText = (role.permissions || []).join(' ');
        return !query || `${rbacRoleName(role)} ${rbacRoleDescription(role)} ${permissionText}`.toLowerCase().includes(query);
    });
    if (!roles.length) {
        box.innerHTML = `<div class="rbac-empty"><strong>${rbacT('rbac.empty.noMatchingRoles', 'No matching roles')}</strong><span>${rbacT('rbac.empty.adjustRoleSearch', 'Adjust search conditions or create a custom role')}</span></div>`;
        return;
    }
    box.innerHTML = roles.map(role => {
        const permissions = role.permissions || [];
        const modules = Array.from(new Set(permissions.map(key => key.split(':')[0]))).filter(Boolean);
        const members = rbacState.users.filter(user => rbacUserRoles(user).includes(role.id)).length;
        const scope = rbacScopeInfo(role.scope);
        return `<article class="rbac-catalog-card">
            <div class="rbac-catalog-card-head">
                <div><h4>${rbacEscape(rbacRoleName(role))}</h4><p>${rbacEscape(rbacRoleDescription(role) || rbacT('rbac.noRoleDescription', 'No role description available'))}</p></div>
                ${rbacRoleIsSystem(role) ? `<span class="rbac-pill">${rbacT('rbac.systemBuiltin', 'Built-in system')}</span>` : `<span class="rbac-pill is-custom">${rbacT('rbac.customRole', 'Custom')}</span>`}
            </div>
            <div class="rbac-catalog-stats">
                <div><strong>${permissions.length}</strong><span>${rbacT('rbac.operationPermissions', 'Operation permissions')}</span></div>
                <div><strong>${members}</strong><span>${rbacT('rbac.assignedMembers', 'Assigned members')}</span></div>
                <div><strong>${rbacEscape(scope.label)}</strong><span>${rbacT('rbac.scope', 'resource scope')}</span></div>
            </div>
            <div class="rbac-module-tags">${modules.slice(0, 6).map(module => `<span title="${rbacEscape(module)}">${rbacEscape(rbacPermissionModuleLabel(module))}</span>`).join('')}${modules.length > 6 ? `<span>+${modules.length - 6}</span>` : ''}</div>
            <div class="rbac-catalog-card-foot">
                <span>${rbacEscape(scope.hint)}</span>
                <div>
                    <button type="button" class="btn-secondary btn-small" onclick="openRbacRoleModal('${rbacEscape(role.id)}')">${rbacRoleIsSystem(role) ? rbacT('rbac.viewPermissions', 'View permission') : rbacT('common.edit', 'edit')}</button>
                    ${rbacRoleIsSystem(role) ? '' : `<button type="button" class="btn-secondary btn-small btn-delete" onclick="deleteRbacRole('${rbacEscape(role.id)}')">${rbacT('common.delete', 'Delete')}</button>`}
                </div>
            </div>
        </article>`;
    }).join('');
}

function renderRbacRoles() {
    const title = document.getElementById('rbac-selected-title');
    const subtitle = document.getElementById('rbac-selected-subtitle');
    const summary = document.getElementById('rbac-selected-summary');
    const editBtn = document.getElementById('rbac-edit-user-btn');
    const deleteBtn = document.getElementById('rbac-delete-user-btn');
    const user = selectedRbacUser();
    ensurePendingUserRoles(user);
    if (title) {
        if (user) {
            const status = user.enabled ? rbacT('rbac.statusEnabled', 'Enable') : rbacT('rbac.statusDisabled', 'Deactivate');
            const displayName = rbacUserDisplayName(user);
            const handle = user.username === displayName ? '' : `@${user.username} · `;
            title.innerHTML = `<span class="rbac-detail-name" title="${rbacEscape(displayName)}">${rbacEscape(displayName)}</span><span class="rbac-detail-meta" title="${rbacEscape(`${handle}${status}`)}">${rbacEscape(handle)}${rbacEscape(status)}</span>`;
        } else {
            title.textContent = rbacT('rbac.selectUser', 'Select user');
        }
    }
    if (subtitle) {
        subtitle.textContent = user ? '' : rbacT('rbac.selectUserHint', 'Select a platform user, then edit the role, status, and resource permissions.');
        subtitle.hidden = !!user;
    }
    if (editBtn) editBtn.disabled = !user;
    if (deleteBtn) deleteBtn.disabled = !user || rbacUserIsBuiltin(user);
    renderRbacNoRoleNotice(user);
    if (summary) {
        if (!user) {
            summary.innerHTML = '';
        } else {
            const access = resolveUserEffectiveAccess(user);
            const scope = rbacScopeInfo(access.effectiveScope);
            const permissionCount = access.effectivePermissions.size;
            const permissionCountHtml = permissionCount
                ? `<button type="button" class="rbac-summary-count rbac-summary-count--interactive" aria-expanded="${rbacState.showEffectivePermissions ? 'true' : 'false'}" aria-haspopup="dialog" aria-controls="rbac-permissions-popover" title="${rbacEscape(rbacT('rbac.viewPermissionsDetail', 'View Details'))}" onclick="toggleRbacEffectivePermissions(event)">${permissionCount}</button>`
                : `<strong class="rbac-summary-count">${permissionCount}</strong>`;
            summary.innerHTML = `
                <div><span>${rbacT('rbac.roles', 'role')}</span><strong>${access.userRoles.length}</strong></div>
                <div class="rbac-summary-permissions">
                    <span>${rbacT('rbac.effectivePermissions', 'effective permissions')}</span>
                    ${permissionCountHtml}
                    <div id="rbac-permissions-popover" class="rbac-permissions-popover" role="dialog" aria-label="${rbacEscape(rbacT('rbac.effectivePermissionsDetail', 'Effective Permission Details'))}" hidden></div>
                </div>
                <div><span>${rbacT('rbac.effectiveScope', 'valid resource range')}</span><strong title="${rbacEscape(scope.hint)}">${rbacEscape(scope.label)}</strong></div>
                <div><span>${rbacT('rbac.metricAssignments', 'Resource Authorization')}</span><strong>${access.assignmentCount}</strong></div>
                `;
        }
    }
    renderRbacEffectivePermissions(user);

    const box = document.getElementById('rbac-roles-list');
    if (!box) return;
    if (!rbacState.roles.length) {
        box.innerHTML = `<div class="empty-state">${rbacT('rbac.empty.noRoles', 'No roles yet')}</div>`;
        return;
    }
    const userRoles = user ? rbacState.pendingUserRoles : new Set();
    box.innerHTML = rbacState.roles.map(role => {
        const checked = userRoles.has(role.id);
        const protectedRole = rbacUserIsBuiltin(user) && role.id === 'admin' && checked;
        const permissionCount = (role.permissions || []).length;
        const assignedUsers = rbacState.users.filter(u => rbacUserRoles(u).includes(role.id)).length;
        const scope = rbacScopeInfo(role.scope);
        return `
            <article class="rbac-role-card${checked ? ' is-selected' : ''}">
                <div class="rbac-role-card-head">
                    <label class="checkbox-label">
                        <input type="checkbox" class="modern-checkbox" ${checked ? 'checked' : ''} ${!user || protectedRole ? 'disabled' : ''} title="${protectedRole ? rbacEscape(rbacT('rbac.protectBuiltinAdminRole', 'Built-in administrator must retain the administrator role')) : ''}" onchange="toggleSelectedUserRole('${rbacEscape(role.id)}', this.checked)">
                        <span class="checkbox-custom"></span>
                        <span class="checkbox-text"><strong>${rbacEscape(rbacRoleName(role))}</strong></span>
                    </label>
                    <span class="rbac-pill ${scope.tone}">${rbacEscape(scope.label)}</span>
                </div>
                <p>${rbacEscape(rbacRoleDescription(role) || rbacT('rbac.noDescription', 'No description'))}</p>
                <div class="rbac-role-card-foot">
                    <span>${rbacT('rbac.permissionCount', '{{count}} permissions', { count: permissionCount })}</span>
                    <span>${rbacT('rbac.userCount', '{{count}} users', { count: assignedUsers })}</span>
                    <span>${rbacEscape(scope.hint)}</span>
                </div>
            </article>`;
    }).join('');
    syncRbacRoleSaveActions();
}

function ensurePendingUserRoles(user) {
    if (!user) {
        rbacState.pendingRoleUserId = '';
        rbacState.pendingUserRoles.clear();
        return;
    }
    if (rbacState.pendingRoleUserId !== user.id) {
        rbacState.pendingRoleUserId = user.id;
        rbacState.pendingUserRoles = new Set(rbacUserRoles(user));
    }
}

function toggleSelectedUserRole(roleId, enabled) {
    const user = selectedRbacUser();
    if (!user || (rbacUserIsBuiltin(user) && roleId === 'admin' && !enabled)) return;
    if (enabled) rbacState.pendingUserRoles.add(roleId);
    else rbacState.pendingUserRoles.delete(roleId);
    renderRbacRoles();
}

function selectedUserRolesAreDirty() {
    const user = selectedRbacUser();
    if (!user) return false;
    const current = new Set(rbacUserRoles(user));
    return current.size !== rbacState.pendingUserRoles.size ||
        Array.from(current).some(roleId => !rbacState.pendingUserRoles.has(roleId));
}

function syncRbacRoleSaveActions() {
    const dirty = selectedUserRolesAreDirty();
    const indicator = document.getElementById('rbac-role-unsaved');
    const reset = document.getElementById('rbac-role-reset-btn');
    const apply = document.getElementById('rbac-role-apply-btn');
    if (indicator) indicator.hidden = !dirty;
    if (reset) reset.disabled = !dirty;
    if (apply) apply.disabled = !dirty;
}

function resetSelectedUserRoles() {
    const user = selectedRbacUser();
    rbacState.pendingUserRoles = new Set(rbacUserRoles(user));
    renderRbacRoles();
}

async function saveSelectedUserRoles() {
    const user = selectedRbacUser();
    if (!user || !selectedUserRolesAreDirty()) return;
    const roles = Array.from(rbacState.pendingUserRoles);
    const saved = await rbacRun(async () => {
        const res = await apiFetch(`/api/rbac/users/${encodeURIComponent(user.id)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                display_name: user.displayName || user.display_name || '',
                enabled: user.enabled,
                roles,
            }),
        });
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || rbacT('rbac.errors.saveFailed', 'Save failed'));
        await refreshRbacUserRoles(user.id, roles);
        rbacNotify(rbacT('rbac.messages.roleChangesSaved', 'Role changes have been saved, permissions take effect immediately'), 'success');
        if (rbacState.activeTab === 'audit') loadRbacUserAuditLogs();
    }, rbacT('rbac.errors.saveFailed', 'Save failed'));
    if (!saved) {
        renderRbacRoles();
    }
}

function switchRbacTab(tab) {
    if (tab !== rbacState.activeTab && selectedUserRolesAreDirty() &&
        !confirm(rbacT('rbac.confirmDiscardSwitchTab', 'The current role changes have not been saved yet. Are you sure you want to discard them and switch to another tab?'))) return;
    if (tab !== rbacState.activeTab && selectedUserRolesAreDirty()) resetSelectedUserRoles();
    rbacState.activeTab = tab;
    document.querySelectorAll('.rbac-tab').forEach(btn => btn.classList.toggle('is-active', btn.dataset.rbacTab === tab));
    const roles = document.getElementById('rbac-tab-roles');
    const assignments = document.getElementById('rbac-tab-assignments');
    const audit = document.getElementById('rbac-tab-audit');
    if (roles) roles.hidden = tab !== 'roles';
    if (assignments) assignments.hidden = tab !== 'assignments';
    if (audit) audit.hidden = tab !== 'audit';
    if (tab === 'assignments') loadRbacResourceOptions();
    if (tab === 'audit') {
        rbacState.auditPage = 0;
        loadRbacUserAuditLogs();
    }
    initRbacSelects();
}

function focusRbacRoleAssignment() {
    switchRbacTab('roles');
    requestAnimationFrame(() => {
        const panel = document.getElementById('rbac-tab-roles');
        const list = document.getElementById('rbac-roles-list');
        const section = panel?.querySelector('.rbac-section-toolbar') || list;
        if (panel) panel.scrollTop = 0;
        if (section) section.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        const highlight = list || panel;
        if (highlight) {
            highlight.classList.remove('rbac-focus-pulse');
            void highlight.offsetWidth;
            highlight.classList.add('rbac-focus-pulse');
            highlight.addEventListener('animationend', () => highlight.classList.remove('rbac-focus-pulse'), { once: true });
        }
        const firstCheckbox = list?.querySelector('input[type="checkbox"]:not(:disabled)');
        if (firstCheckbox) firstCheckbox.focus({ preventScroll: true });
    });
}

function renderRbacAssignments() {
    const box = document.getElementById('rbac-assignments-list');
    if (!box) return;
    const user = selectedRbacUser();
    const rows = user ? rbacState.assignments.filter(a => a.userId === user.id || a.user_id === user.id) : [];
    const count = document.getElementById('rbac-current-assignment-count');
    if (count) count.textContent = String(rows.length);
    if (!user) {
        box.innerHTML = `<div class="rbac-empty"><strong>${rbacT('rbac.empty.selectUserFirst', 'Please select a user first')}</strong></div>`;
        renderRbacAssignmentPagination(0, 1);
        renderRbacPendingSelection();
        return;
    }
    if (!rows.length) {
        box.innerHTML = `<div class="rbac-empty"><strong>${rbacT('rbac.empty.noAssignments', 'No resource authorization yet')}</strong><span>${rbacT('rbac.empty.addAssignmentHint', 'Check resources on the left, or use the "Confirm Authorization" button above')}</span></div>`;
        renderRbacAssignmentPagination(0, 1);
        renderRbacPendingSelection();
        return;
    }
    const query = rbacState.assignmentSearch.trim().toLowerCase();
    const filteredRows = rows.filter(row => {
        const type = row.resourceType || row.resource_type || '';
        const id = row.resourceId || row.resource_id || '';
        const label = row.resourceLabel || row.resource_label || '';
        const detail = row.resourceDetail || row.resource_detail || '';
        return (rbacState.assignmentType === 'all' || type === rbacState.assignmentType) &&
            (!query || `${rbacResourceLabel(type)} ${type} ${label} ${detail} ${id}`.toLowerCase().includes(query));
    });
    const totalPages = Math.max(1, Math.ceil(filteredRows.length / rbacState.assignmentPageSize));
    rbacState.assignmentPage = Math.min(rbacState.assignmentPage, totalPages - 1);
    const start = rbacState.assignmentPage * rbacState.assignmentPageSize;
    const visibleRows = filteredRows.slice(start, start + rbacState.assignmentPageSize);
    if (!filteredRows.length) {
        box.innerHTML = `<div class="rbac-empty"><strong>${rbacT('rbac.empty.noMatchingAssignments', 'No matching authorization')}</strong><span>${rbacT('rbac.empty.adjustAssignmentFilter', 'Adjust resource type or search criteria')}</span></div>`;
        renderRbacAssignmentPagination(0, 1);
        renderRbacPendingSelection();
        return;
    }
    box.innerHTML = visibleRows.map(row => {
        const type = row.resourceType || row.resource_type;
        const id = row.resourceId || row.resource_id;
        const label = row.resourceLabel || row.resource_label || id;
        const detail = rbacFormatResourceDetail(type, row.resourceDetail || row.resource_detail || '');
        return `
            <div class="rbac-assignment-row">
                <span class="rbac-assignment-identity">
                    <span class="rbac-pill is-info">${rbacEscape(rbacResourceLabel(type))}</span>
                    <strong title="${rbacEscape(label)}">${rbacEscape(label)}</strong>
                </span>
                <span class="rbac-assignment-id" title="${rbacEscape(id)}">${rbacEscape(rbacShortId(id))}</span>
                <span class="rbac-assignment-detail">${detail ? rbacEscape(detail) : '—'}</span>
                <span class="rbac-assignment-actions">
                    <button type="button" class="btn-link btn-small" onclick="rbacCopyResourceId('${rbacEscape(id)}')">${rbacEscape(rbacT('rbac.copyId', 'Copy ID'))}</button>
                    <button type="button" class="btn-link btn-small is-danger" onclick="deleteRbacAssignment('${rbacEscape(row.id)}')">${rbacT('rbac.revoke', 'revoke')}</button>
                </span>
            </div>`;
    }).join('');
    renderRbacAssignmentPagination(filteredRows.length, totalPages);
    renderRbacPendingSelection();
}

function renderRbacAssignmentPagination(total, totalPages) {
    const pagination = document.getElementById('rbac-assignment-pagination');
    if (!pagination) return;
    const pages = Math.max(1, totalPages || 1);
    pagination.hidden = pages <= 1;
    rbacState.assignmentPage = Math.min(rbacState.assignmentPage, pages - 1);
    const info = pagination.querySelector('[data-rbac-page-info]');
    const previous = pagination.querySelector('[data-rbac-page-previous]');
    const next = pagination.querySelector('[data-rbac-page-next]');
    if (info) {
        info.textContent = total
            ? rbacT('rbac.pagination.pageSummary', 'Page {{page}} / {{pages}} · {{total}} items total', {
                page: rbacState.assignmentPage + 1,
                pages,
                total,
            })
            : rbacT('rbac.pagination.emptySummary', 'Total: 0 items');
    }
    if (previous) previous.disabled = total === 0 || rbacState.assignmentPage === 0;
    if (next) next.disabled = total === 0 || rbacState.assignmentPage >= pages - 1;
}

function filterRbacAssignments(value) {
    rbacState.assignmentSearch = value || '';
    rbacState.assignmentPage = 0;
    renderRbacAssignments();
}

function changeRbacAssignmentFilter(value) {
    rbacState.assignmentType = value || 'all';
    rbacState.assignmentPage = 0;
    renderRbacAssignments();
}

function changeRbacAssignmentPage(delta) {
    rbacState.assignmentPage = Math.max(0, rbacState.assignmentPage + delta);
    renderRbacAssignments();
}

function changeRbacResourceType() {
    const typeInput = document.getElementById('rbac-assignment-type');
    if (!typeInput) return;
    const nextType = typeInput.value;
    const previousType = rbacState.assignmentResourceType || 'conversation';
    if (nextType === previousType) return;
    if (rbacState.selectedResourceIds.size > 0 &&
        !confirm(rbacT('rbac.confirmDiscardResourceType', 'Switching the resource type will clear the current selection. Are you sure you want to continue?'))) {
        typeInput.value = previousType;
        initRbacSelects();
        return;
    }
    rbacState.assignmentResourceType = nextType;
    rbacState.selectedResourceIds.clear();
    rbacState.selectedResourceMeta.clear();
    rbacState.resourcePage = 0;
    const search = document.getElementById('rbac-resource-search');
    if (search) search.value = '';
    syncRbacAssignmentSubmit();
    loadRbacResourceOptions();
}

function queueRbacResourceSearch() {
    clearTimeout(rbacState.resourceSearchTimer);
    rbacState.resourcePage = 0;
    rbacState.resourceSearchTimer = setTimeout(loadRbacResourceOptions, 250);
}

async function loadRbacResourceOptions() {
    const picker = document.getElementById('rbac-resource-picker');
    const typeInput = document.getElementById('rbac-assignment-type');
    if (!picker || !typeInput || !selectedRbacUser()) return;
    const requestSeq = ++rbacState.resourceRequestSeq;
    const query = document.getElementById('rbac-resource-search')?.value.trim() || '';
    picker.innerHTML = `<div class="rbac-picker-status">${rbacT('rbac.loadingResources', 'Loading real resources…')}</div>`;
    syncRbacResourcePagination(true);
    try {
        const offset = rbacState.resourcePage * rbacState.resourcePageSize;
        const res = await apiFetch(`/api/rbac/resources?type=${encodeURIComponent(typeInput.value)}&q=${encodeURIComponent(query)}&limit=${rbacState.resourcePageSize}&offset=${offset}`);
        const result = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(result.error || rbacT('rbac.errors.loadResourcesFailed', 'Failed to load resource'));
        if (requestSeq !== rbacState.resourceRequestSeq) return;
        rbacState.resourceOptions = result.resources || [];
        rbacState.resourceHasMore = !!result.has_more;
        rbacState.resourceTotal = Number(result.total || 0);
        renderRbacResourcePicker();
    } catch (error) {
        if (requestSeq !== rbacState.resourceRequestSeq) return;
        rbacState.resourceHasMore = false;
        rbacState.resourceTotal = 0;
        picker.innerHTML = `<div class="rbac-picker-status is-error">${rbacEscape(error.message || rbacT('rbac.errors.loadResourcesFailed', 'Failed to load resource'))} <button type="button" class="btn-link" onclick="loadRbacResourceOptions()">${rbacT('rbac.retry', 'Retry')}</button></div>`;
        syncRbacResourcePagination(false);
    }
}

function renderRbacResourcePicker() {
    const picker = document.getElementById('rbac-resource-picker');
    if (!picker) return;
    const user = selectedRbacUser();
    const resourceType = document.getElementById('rbac-assignment-type')?.value || '';
    const assigned = new Set(rbacState.assignments
        .filter(row => (row.userId === user?.id || row.user_id === user?.id) && (row.resourceType || row.resource_type) === resourceType)
        .map(row => row.resourceId || row.resource_id));
    if (!rbacState.resourceOptions.length) {
        picker.innerHTML = `<div class="rbac-picker-status">${rbacT('rbac.empty.noMatchingResources', 'No matching real resource')}</div>`;
        syncRbacResourcePagination(false);
        return;
    }
    picker.innerHTML = rbacState.resourceOptions.map(resource => {
        const alreadyAssigned = assigned.has(resource.id);
        const checked = rbacState.selectedResourceIds.has(resource.id);
        const detail = rbacFormatResourceDetail(resourceType, resource.detail || '');
        return `<label class="rbac-resource-option${alreadyAssigned ? ' is-assigned' : ''}">
            <input type="checkbox" class="modern-checkbox" value="${rbacEscape(resource.id)}" ${checked ? 'checked' : ''} ${alreadyAssigned ? 'disabled' : ''} onchange="toggleRbacResourceSelection(this.value, this.checked)">
            <span class="checkbox-custom"></span>
            <span class="rbac-resource-option-main" title="${rbacEscape(resource.label || resource.id)}">
                <strong>${rbacEscape(resource.label || resource.id)}</strong>
            </span>
            <span class="rbac-resource-option-id" title="${rbacEscape(resource.id)}">${rbacEscape(rbacShortId(resource.id))}</span>
            <span class="rbac-resource-option-detail">${alreadyAssigned ? rbacT('rbac.alreadyAssigned', 'Authorized') : (rbacEscape(detail) || '—')}</span>
            <button type="button" class="btn-link btn-small rbac-resource-copy" onclick="event.preventDefault(); event.stopPropagation(); rbacCopyResourceId('${rbacEscape(resource.id)}')">${rbacEscape(rbacT('rbac.copyId', 'Copy ID'))}</button>
        </label>`;
    }).join('');
    syncRbacResourcePagination(false);
}

function syncRbacResourcePagination(loading) {
    const pagination = document.getElementById('rbac-resource-pagination');
    if (!pagination) return;
    const previous = pagination.querySelector('[data-rbac-page-previous]');
    const next = pagination.querySelector('[data-rbac-page-next]');
    const info = pagination.querySelector('[data-rbac-page-info]');
    pagination.hidden = !loading && rbacState.resourcePage === 0 && !rbacState.resourceHasMore;
    if (previous) previous.disabled = loading || rbacState.resourcePage === 0;
    if (next) next.disabled = loading || !rbacState.resourceHasMore;
    if (info) {
        const pages = Math.max(1, Math.ceil(rbacState.resourceTotal / rbacState.resourcePageSize));
        info.textContent = rbacT('rbac.pagination.pageSummary', 'Page {{page}} / {{pages}} · {{total}} items total', {
            page: rbacState.resourcePage + 1,
            pages,
            total: rbacState.resourceTotal,
        });
    }
}

function changeRbacResourcePage(delta) {
    const nextPage = rbacState.resourcePage + delta;
    if (nextPage < 0 || (delta > 0 && !rbacState.resourceHasMore)) return;
    rbacState.resourcePage = nextPage;
    loadRbacResourceOptions();
}

function toggleRbacResourceSelection(resourceId, checked) {
    if (checked) {
        rbacState.selectedResourceIds.add(resourceId);
        const resource = rbacState.resourceOptions.find(item => item.id === resourceId);
        rbacRememberSelectedResource(resource || { id: resourceId, label: resourceId, detail: '' });
    } else {
        rbacState.selectedResourceIds.delete(resourceId);
        rbacState.selectedResourceMeta.delete(resourceId);
        if (rbacState.resourceOptions.length) renderRbacResourcePicker();
    }
    syncRbacAssignmentSubmit();
}

function syncRbacAssignmentSubmit() {
    renderRbacPendingSelection();
}

function rbacAuditActionLabel(log) {
    const action = log && log.action ? String(log.action) : '';
    const key = `rbac.auditActions.${action}`;
    const translated = rbacT(key, '');
    return translated || (log && log.message) || action;
}

async function loadRbacUserAuditLogs() {
    const box = document.getElementById('rbac-audit-list');
    const user = selectedRbacUser();
    if (!box) return;
    if (!user) {
        box.innerHTML = `<div class="rbac-empty"><strong>${rbacT('rbac.empty.selectUserFirst', 'Please select a user first')}</strong></div>`;
        rbacState.auditPage = 0;
        rbacState.auditTotal = 0;
        renderRbacAuditPagination();
        return;
    }
    rbacState.auditLoading = true;
    box.innerHTML = `<div class="rbac-picker-status">${rbacT('common.loading', 'Loading...')}</div>`;
    try {
        const params = new URLSearchParams({
            category: 'rbac',
            related_user_id: user.id,
            page: String(rbacState.auditPage + 1),
            page_size: String(rbacState.auditPageSize),
        });
        if (rbacState.auditAction) params.set('action', rbacState.auditAction);
        if (rbacState.auditResourceType) params.set('resource_type', rbacState.auditResourceType);
        const res = await apiFetch(`/api/audit/logs?${params.toString()}`);
        const result = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(result.error || rbacT('rbac.errors.loadAuditFailed', 'Failed to load audit records'));
        rbacState.auditLogs = result.logs || [];
        rbacState.auditTotal = Number(result.total || 0);
        renderRbacAuditLogs();
        renderRbacAuditPagination();
    } catch (error) {
        box.innerHTML = `<div class="rbac-picker-status is-error">${rbacEscape(error.message || rbacT('rbac.errors.loadAuditFailed', 'Failed to load audit records'))} <button type="button" class="btn-link" onclick="loadRbacUserAuditLogs()">${rbacT('rbac.retry', 'Retry')}</button></div>`;
        renderRbacAuditPagination();
    } finally {
        rbacState.auditLoading = false;
    }
}

function renderRbacAuditPagination() {
    const pagination = document.getElementById('rbac-audit-pagination');
    if (!pagination) return;
    const pages = Math.max(1, Math.ceil(rbacState.auditTotal / rbacState.auditPageSize));
    rbacState.auditPage = Math.min(rbacState.auditPage, pages - 1);
    pagination.hidden = false;
    const first = pagination.querySelector('[data-rbac-page-first]');
    const previous = pagination.querySelector('[data-rbac-page-previous]');
    const next = pagination.querySelector('[data-rbac-page-next]');
    const last = pagination.querySelector('[data-rbac-page-last]');
    const info = pagination.querySelector('[data-rbac-page-info]');
    const range = pagination.querySelector('[data-rbac-page-range]');
    const atFirst = rbacState.auditPage === 0;
    const atLast = rbacState.auditPage >= pages - 1;
    if (first) first.disabled = atFirst;
    if (previous) previous.disabled = atFirst;
    if (next) next.disabled = atLast;
    if (last) last.disabled = atLast;
    if (info) info.textContent = rbacT('rbac.pagination.pageIndicator', 'Page {{page}} / {{pages}}', {
        page: rbacState.auditPage + 1,
        pages,
    });
    const start = rbacState.auditTotal === 0 ? 0 : rbacState.auditPage * rbacState.auditPageSize + 1;
    const end = Math.min(rbacState.auditTotal, (rbacState.auditPage + 1) * rbacState.auditPageSize);
    if (range) range.textContent = rbacT('rbac.pagination.recordRange', 'Showing {{start}}-{{end}} / {{total}} records total', {
        start,
        end,
        total: rbacState.auditTotal,
    });
}

function changeRbacAuditPage(delta) {
    const pages = Math.max(1, Math.ceil(rbacState.auditTotal / rbacState.auditPageSize));
    const nextPage = rbacState.auditPage + delta;
    if (nextPage < 0 || nextPage >= pages || rbacState.auditLoading) return;
    rbacState.auditPage = nextPage;
    loadRbacUserAuditLogs();
}

function setRbacAuditPage(page) {
    if (rbacState.auditLoading) return;
    const pages = Math.max(1, Math.ceil(rbacState.auditTotal / rbacState.auditPageSize));
    const nextPage = page < 0 ? pages - 1 : Math.max(0, Math.min(Number(page) || 0, pages - 1));
    if (nextPage === rbacState.auditPage) return;
    rbacState.auditPage = nextPage;
    loadRbacUserAuditLogs();
}

function refreshRbacUserAuditLogs() {
    rbacState.auditPage = 0;
    loadRbacUserAuditLogs();
}

function changeRbacAuditPageSize(value) {
    const pageSize = Number(value);
    if (![20, 50, 100].includes(pageSize)) return;
    rbacState.auditPageSize = pageSize;
    rbacState.auditPage = 0;
    loadRbacUserAuditLogs();
}

function setRbacAuditFilter(filter, value) {
    if (filter === 'action') rbacState.auditAction = String(value || '').trim();
    else if (filter === 'resourceType') rbacState.auditResourceType = String(value || '').trim();
    else return;
    rbacState.auditPage = 0;
    loadRbacUserAuditLogs();
}

function renderRbacAuditLogs() {
    const box = document.getElementById('rbac-audit-list');
    if (!box) return;
    if (!rbacState.auditLogs.length) {
        box.innerHTML = `<div class="rbac-empty"><strong>${rbacT('rbac.empty.noAuditLogs', 'No change records yet')}</strong><span>${rbacT('rbac.empty.noAuditLogsHint', 'Changes to this member\'s roles and permissions will be displayed here')}</span></div>`;
        return;
    }
    const formatTime = typeof formatAuditTime === 'function'
        ? formatAuditTime
        : (iso) => rbacText(iso);
    const header = `
        <div class="rbac-audit-table-head" role="row">
            <span role="columnheader">${rbacEscape(rbacT('rbac.auditColumns.action', 'operation'))}</span>
            <span role="columnheader">${rbacEscape(rbacT('rbac.auditColumns.actor', 'Operator'))}</span>
            <span role="columnheader">${rbacEscape(rbacT('rbac.auditColumns.resourceType', 'Resource type'))}</span>
            <span role="columnheader">${rbacEscape(rbacT('rbac.auditColumns.resourceId', 'Resource ID'))}</span>
            <span role="columnheader">${rbacEscape(rbacT('rbac.auditColumns.time', 'Operation time'))}</span>
        </div>`;
    const rows = rbacState.auditLogs.map(log => {
        const resourceType = log.resourceType || log.resource_type;
        const resourceId = log.resourceId || log.resource_id;
        return `
        <article class="rbac-audit-row" role="row">
            <strong class="rbac-audit-action" role="cell" data-label="${rbacEscape(rbacT('rbac.auditColumns.action', 'operation'))}"><span class="rbac-audit-cell-value">${rbacEscape(rbacAuditActionLabel(log))}</span></strong>
            <span class="rbac-audit-actor" role="cell" data-label="${rbacEscape(rbacT('rbac.auditColumns.actor', 'Operator'))}"><span class="rbac-pill rbac-audit-cell-value">${rbacEscape(log.actor || rbacT('rbac.unknownActor', 'Unknown Operator'))}</span></span>
            <span class="rbac-audit-detail" role="cell" data-label="${rbacEscape(rbacT('rbac.auditColumns.resourceType', 'Resource type'))}"><span class="rbac-audit-cell-value">${resourceType ? rbacEscape(rbacResourceLabel(resourceType)) : '—'}</span></span>
            <span class="rbac-audit-detail rbac-audit-resource-id" role="cell" data-label="${rbacEscape(rbacT('rbac.auditColumns.resourceId', 'Resource ID'))}"><span class="rbac-audit-cell-value">${resourceId ? rbacEscape(resourceId) : '—'}</span></span>
            <span class="rbac-audit-time" role="cell" data-label="${rbacEscape(rbacT('rbac.auditColumns.time', 'Operation time'))}"><span class="rbac-audit-cell-value">${rbacEscape(formatTime(log.createdAt || log.created_at))}</span></span>
        </article>`;
    }).join('');
    box.removeAttribute('role');
    box.innerHTML = `
        <div class="rbac-audit-table" role="table">
            ${header}
            <div class="rbac-audit-table-body" role="rowgroup">${rows}</div>
        </div>`;
}

function openRbacUserModal(userId = '') {
    const user = userId ? rbacState.users.find(u => u.id === userId) : null;
    document.getElementById('rbac-user-id').value = user ? user.id : '';
    document.getElementById('rbac-username').value = user ? user.username : '';
    document.getElementById('rbac-username').disabled = !!user;
    document.getElementById('rbac-display-name').value = user ? (user.displayName || user.display_name || '') : '';
    document.getElementById('rbac-password').value = '';
    const enabledInput = document.getElementById('rbac-user-enabled');
    enabledInput.checked = user ? !!user.enabled : true;
    enabledInput.disabled = rbacUserIsBuiltin(user) && enabledInput.checked;
    enabledInput.dataset.stateDisabled = String(enabledInput.disabled);
    enabledInput.title = enabledInput.disabled ? rbacT('rbac.protectBuiltinAdminEnabled', 'Built-in administrator cannot be disabled') : '';
    const createRoles = document.getElementById('rbac-user-roles-create-section');
    const editRoles = document.getElementById('rbac-user-roles-edit-section');
    if (createRoles) createRoles.hidden = !!user;
    if (editRoles) editRoles.hidden = !user;
    if (user) renderRbacUserRolesReadonly(user);
    else renderRbacUserRoleCheckboxes(new Set());
    initRbacSelects();
    openAppModal('rbac-user-modal');
}

function renderRbacUserRolesReadonly(user) {
    const box = document.getElementById('rbac-user-roles-readonly');
    if (!box) return;
    const roles = rbacUserRoles(user).map(roleId => rbacState.roles.find(role => role.id === roleId)).filter(Boolean);
    if (!roles.length) {
        box.innerHTML = `<div class="rbac-empty is-compact"><strong>${rbacT('rbac.noRoleAssigned', 'Unassigned roles')}</strong></div>`;
        return;
    }
    box.innerHTML = roles.map(role => {
        const scope = rbacScopeInfo(role.scope);
        return `<div class="rbac-readonly-role"><strong>${rbacEscape(rbacRoleName(role))}</strong><span>${rbacEscape(scope.label)}</span></div>`;
    }).join('');
}

function openRbacRolesFromUserModal() {
    closeRbacUserModal();
    focusRbacRoleAssignment();
}

function openRbacUserModalForSelected() {
    const user = selectedRbacUser();
    if (!user) return;
    if (selectedUserRolesAreDirty() && !confirm(rbacT('rbac.confirmDiscardEditUser', 'The current role changes have not been saved yet. Are you sure you want to discard them and edit the member profile?'))) return;
    if (selectedUserRolesAreDirty()) resetSelectedUserRoles();
    openRbacUserModal(user.id);
}

function closeRbacUserModal() {
    closeAppModal('rbac-user-modal');
}

function renderRbacUserRoleCheckboxes(selected) {
    const box = document.getElementById('rbac-user-role-checkboxes');
    if (!box) return;
    box.innerHTML = rbacState.roles.map(role => {
        const scope = rbacScopeInfo(role.scope);
        return `
        <label class="checkbox-label rbac-checkbox-item">
            <input type="checkbox" class="modern-checkbox" value="${rbacEscape(role.id)}" ${selected.has(role.id) ? 'checked' : ''}>
            <span class="checkbox-custom"></span>
            <span class="checkbox-text rbac-role-option-text"><strong>${rbacEscape(rbacRoleName(role))}</strong><small>${rbacEscape(scope.label)} · ${rbacEscape(rbacRoleDescription(role) || rbacT('rbac.noDescription', 'No description'))}</small></span>
        </label>`;
    }).join('');
}

async function saveRbacUser() {
    await rbacRun(async () => {
        const id = document.getElementById('rbac-user-id').value;
        const isEdit = !!id;
        const payload = {
            display_name: document.getElementById('rbac-display-name').value.trim(),
            password: document.getElementById('rbac-password').value,
            enabled: document.getElementById('rbac-user-enabled').checked,
        };
        if (!isEdit) {
            payload.roles = Array.from(document.querySelectorAll('#rbac-user-role-checkboxes input:checked')).map(el => el.value);
            payload.username = document.getElementById('rbac-username').value.trim();
        }
        let url = '/api/rbac/users';
        let method = 'POST';
        if (isEdit) {
            url += '/' + encodeURIComponent(id);
            method = 'PUT';
            if (!payload.password) delete payload.password;
        }
        const res = await apiFetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || rbacT('rbac.errors.saveFailed', 'Save failed'));
        closeRbacUserModal();
        await loadPlatformRbac();
        if (isEdit && rbacState.activeTab === 'audit') loadRbacUserAuditLogs();
    }, rbacT('rbac.errors.saveUserFailed', 'Failed to save user'));
}

async function deleteSelectedRbacUser() {
    await rbacRun(async () => {
        const user = selectedRbacUser();
        if (!user) return;
        if (rbacUserIsBuiltin(user)) {
            rbacNotify(rbacT('rbac.deleteBuiltinUser', 'Built-in administrator cannot be deleted'), 'error');
            return;
        }
        const access = resolveUserEffectiveAccess(user);
        const confirmMessage = rbacT('rbac.deleteUserConfirmDetailed', 'Confirm deleting member {{name}} (@{{username}})?\n\nThis will immediately revoke their sessions and remove {{roles}} role bindings and {{grants}} resource grants.', {
            name: rbacUserDisplayName(user),
            username: user.username,
            roles: access.userRoles.length,
            grants: access.assignmentCount,
        });
        if (!confirm(confirmMessage)) return;
        const res = await apiFetch(`/api/rbac/users/${encodeURIComponent(user.id)}`, { method: 'DELETE' });
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || rbacT('rbac.errors.deleteFailed', 'Delete failed'));
        rbacState.selectedUserId = '';
        await loadPlatformRbac();
    }, rbacT('rbac.errors.deleteUserFailed', 'Failed to delete user'));
}

function openRbacRoleModal(roleId = '') {
    const role = roleId ? rbacState.roles.find(r => r.id === roleId) : null;
    document.getElementById('rbac-role-id').value = role ? role.id : '';
    document.getElementById('rbac-role-name').value = role ? rbacRoleName(role) : '';
    document.getElementById('rbac-role-description').value = role ? rbacRoleDescription(role) : '';
    document.getElementById('rbac-role-scope').value = role ? role.scope : 'assigned';
    rbacState.editingRoleIsSystem = rbacRoleIsSystem(role);
    ['rbac-role-name', 'rbac-role-description', 'rbac-role-scope'].forEach(id => {
        const field = document.getElementById(id);
        if (field) field.disabled = rbacState.editingRoleIsSystem;
    });
    const saveButton = document.getElementById('rbac-role-save-btn');
    if (saveButton) saveButton.hidden = rbacState.editingRoleIsSystem;
    document.querySelectorAll('.rbac-permission-tools button').forEach(button => {
        button.disabled = rbacState.editingRoleIsSystem;
    });
    const title = document.getElementById('rbac-role-modal-title');
    if (title) title.textContent = rbacState.editingRoleIsSystem
        ? rbacT('rbac.viewSystemRole', 'View system roles')
        : (role ? rbacT('rbac.editRole', 'Edit Role') : rbacT('rbac.createRole', 'Create Role'));
    const search = document.getElementById('rbac-permission-search');
    if (search) search.value = '';
    rbacState.permissionSearch = '';
    rbacState.rolePermissionSelection = new Set((role && role.permissions) || []);
    renderRbacPermissionCheckboxes();
    initRbacSelects();
    openAppModal('rbac-role-modal');
}

function closeRbacRoleModal() {
    closeAppModal('rbac-role-modal');
}

function renderRbacPermissionCheckboxes() {
    const box = document.getElementById('rbac-permission-checkboxes');
    if (!box) return;
    const grouped = {};
    const query = rbacState.permissionSearch.trim().toLowerCase();
    Object.keys(rbacState.permissions).sort().forEach(key => {
        const desc = rbacPermissionLabel(key);
        if (query && !`${key} ${desc}`.toLowerCase().includes(query)) return;
        const module = key.split(':')[0] || 'other';
        if (!grouped[module]) grouped[module] = [];
        grouped[module].push(key);
    });
    if (!Object.keys(grouped).length) {
        box.innerHTML = `<div class="empty-state">${rbacT('rbac.empty.noMatchingPermissions', 'No matching permissions')}</div>`;
        return;
    }
    box.innerHTML = Object.keys(grouped).sort().map(module => `
        <section class="rbac-permission-group">
            <div class="rbac-permission-group-head">
                <h4>${rbacEscape(rbacPermissionModuleLabel(module))}<small>${rbacEscape(module)}</small></h4>
                <span>${grouped[module].length}</span>
            </div>
            <div class="rbac-checkbox-grid">
                ${grouped[module].map(key => `
                    <label class="checkbox-label rbac-checkbox-item" title="${rbacEscape(rbacPermissionLabel(key))}">
                        <input type="checkbox" class="modern-checkbox" value="${rbacEscape(key)}" ${rbacState.rolePermissionSelection.has(key) ? 'checked' : ''} ${rbacState.editingRoleIsSystem ? 'disabled' : ''} onchange="setRbacPermissionSelected('${rbacEscape(key)}', this.checked)">
                        <span class="checkbox-custom"></span>
                        <span class="checkbox-text rbac-permission-option"><strong>${rbacEscape(rbacPermissionLabel(key))}</strong><code>${rbacEscape(key)}</code></span>
                    </label>`).join('')}
            </div>
        </section>`).join('');
}

function setRbacPermissionSelected(key, checked) {
    if (checked) rbacState.rolePermissionSelection.add(key);
    else rbacState.rolePermissionSelection.delete(key);
}

function filterRbacPermissions(value) {
    rbacState.permissionSearch = value || '';
    renderRbacPermissionCheckboxes();
}

function selectVisibleRbacPermissions(checked) {
    document.querySelectorAll('#rbac-permission-checkboxes input[type="checkbox"]').forEach(input => {
        input.checked = checked;
        setRbacPermissionSelected(input.value, checked);
    });
}

async function saveRbacRole() {
    await rbacRun(async () => {
        const id = document.getElementById('rbac-role-id').value;
        const payload = {
            name: document.getElementById('rbac-role-name').value.trim(),
            description: document.getElementById('rbac-role-description').value.trim(),
            scope: document.getElementById('rbac-role-scope').value,
            permissions: Array.from(rbacState.rolePermissionSelection),
        };
        const url = id ? `/api/rbac/roles/${encodeURIComponent(id)}` : '/api/rbac/roles';
        const res = await apiFetch(url, {
            method: id ? 'PUT' : 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || rbacT('rbac.errors.saveFailed', 'Save failed'));
        closeRbacRoleModal();
        await loadPlatformRbac();
    }, rbacT('rbac.errors.saveRoleFailed', 'Failed to save role'));
}

async function deleteRbacRole(roleId) {
    await rbacRun(async () => {
        if (!confirm(rbacT('rbac.deleteRoleConfirm', 'Confirm deletion of this platform role?'))) return;
        const res = await apiFetch(`/api/rbac/roles/${encodeURIComponent(roleId)}`, { method: 'DELETE' });
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || rbacT('rbac.errors.deleteFailed', 'Delete failed'));
        await loadPlatformRbac();
    }, rbacT('rbac.errors.deleteRoleFailed', 'Failed to delete role'));
}

async function createRbacAssignment() {
    await rbacRun(async () => {
        const user = selectedRbacUser();
        if (!user) return;
        const resourceType = document.getElementById('rbac-assignment-type').value;
        const manualIds = document.getElementById('rbac-assignment-id').value.split(/[\s,，;；]+/).map(id => id.trim()).filter(Boolean);
        const resourceIds = Array.from(new Set([...rbacState.selectedResourceIds, ...manualIds]));
        if (!resourceIds.length) {
            rbacNotify(rbacT('rbac.errors.enterResourceId', 'Please enter at least one resource ID'), 'error');
            return;
        }
        if (resourceIds.length > 100) {
            rbacNotify(rbacT('rbac.errors.tooManyResources', 'Authorize at most 100 resources at a time'), 'error');
            return;
        }
        const res = await apiFetch('/api/rbac/resource-assignments', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ user_id: user.id, resource_type: resourceType, resource_ids: resourceIds, auto_detect: true }),
        });
        const result = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(result.error || rbacT('rbac.errors.batchGrantFailed', 'Batch authorization failed'));
        document.getElementById('rbac-assignment-id').value = '';
        rbacState.selectedResourceIds.clear();
        rbacState.selectedResourceMeta.clear();
        await refreshRbacAssignments();
        await loadRbacResourceOptions();
        syncRbacAssignmentSubmit();
        if (rbacState.activeTab === 'audit') loadRbacUserAuditLogs();
        const created = Number(result.created || 0);
        const skipped = Number(result.skipped || 0);
        rbacNotify(skipped
            ? rbacT('rbac.messages.grantPartial', 'Added {{created', { created, skipped })
            : rbacT('rbac.messages.grantSuccess', 'Authorized {{count}} {{resource}} items', { count: created, resource: rbacResourceLabel(resourceType) }), 'success');
    }, rbacT('rbac.errors.grantFailed', 'Authorization failed'));
}

async function deleteRbacAssignment(id) {
    await rbacRun(async () => {
        const res = await apiFetch(`/api/rbac/resource-assignments/${encodeURIComponent(id)}`, { method: 'DELETE' });
        if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || rbacT('rbac.errors.revokeFailed', 'Revoke failed'));
        await refreshRbacAssignments();
        if (rbacState.activeTab === 'assignments') await loadRbacResourceOptions();
        if (rbacState.activeTab === 'audit') loadRbacUserAuditLogs();
    }, rbacT('rbac.errors.revokeFailed', 'Revoke failed'));
}

document.addEventListener('languagechange', () => {
    renderRbac();
    if (rbacState.resourceOptions.length) renderRbacResourcePicker();
    renderRbacPermissionCheckboxes();
    renderRbacEffectivePermissions(selectedRbacUser());
    if (rbacState.auditLogs.length) renderRbacAuditLogs();
    initRbacSelects();
    const roleId = document.getElementById('rbac-role-id')?.value;
    const role = roleId ? rbacState.roles.find(item => item.id === roleId) : null;
    if (role && rbacRoleIsSystem(role)) {
        const name = document.getElementById('rbac-role-name');
        const description = document.getElementById('rbac-role-description');
        const title = document.getElementById('rbac-role-modal-title');
        if (name) name.value = rbacRoleName(role);
        if (description) description.value = rbacRoleDescription(role);
        if (title) title.textContent = rbacT('rbac.viewSystemRole', 'View system roles');
    }
});

window.openRbacRolesFromUserModal = openRbacRolesFromUserModal;
window.focusRbacRoleAssignment = focusRbacRoleAssignment;
window.toggleRbacEffectivePermissions = toggleRbacEffectivePermissions;
window.loadRbacUserAuditLogs = loadRbacUserAuditLogs;
window.refreshRbacUserAuditLogs = refreshRbacUserAuditLogs;
window.changeRbacAuditPage = changeRbacAuditPage;
window.setRbacAuditPage = setRbacAuditPage;
window.changeRbacAuditPageSize = changeRbacAuditPageSize;
window.setRbacAuditFilter = setRbacAuditFilter;
window.clearRbacResourceSelection = clearRbacResourceSelection;
window.rbacCopyResourceId = rbacCopyResourceId;
window.initPlatformRbacPage = initPlatformRbacPage;
window.refreshPlatformRbac = refreshPlatformRbac;
window.initRbacSelects = initRbacSelects;
window.loadPlatformRbac = loadPlatformRbac;
window.selectRbacUser = selectRbacUser;
window.switchRbacView = switchRbacView;
window.setRbacRoleSearch = setRbacRoleSearch;
window.setRbacUserSearch = setRbacUserSearch;
window.switchRbacTab = switchRbacTab;
window.openRbacUserModal = openRbacUserModal;
window.openRbacUserModalForSelected = openRbacUserModalForSelected;
window.closeRbacUserModal = closeRbacUserModal;
window.saveRbacUser = saveRbacUser;
window.deleteSelectedRbacUser = deleteSelectedRbacUser;
window.openRbacRoleModal = openRbacRoleModal;
window.closeRbacRoleModal = closeRbacRoleModal;
window.saveRbacRole = saveRbacRole;
window.deleteRbacRole = deleteRbacRole;
window.filterRbacPermissions = filterRbacPermissions;
window.selectVisibleRbacPermissions = selectVisibleRbacPermissions;
window.setRbacPermissionSelected = setRbacPermissionSelected;
window.createRbacAssignment = createRbacAssignment;
window.deleteRbacAssignment = deleteRbacAssignment;
window.changeRbacResourceType = changeRbacResourceType;
window.queueRbacResourceSearch = queueRbacResourceSearch;
window.loadRbacResourceOptions = loadRbacResourceOptions;
window.toggleRbacResourceSelection = toggleRbacResourceSelection;
window.changeRbacResourcePage = changeRbacResourcePage;
window.filterRbacAssignments = filterRbacAssignments;
window.changeRbacAssignmentFilter = changeRbacAssignmentFilter;
window.changeRbacAssignmentPage = changeRbacAssignmentPage;
window.syncRbacAssignmentSubmit = syncRbacAssignmentSubmit;
window.resetSelectedUserRoles = resetSelectedUserRoles;
window.saveSelectedUserRoles = saveSelectedUserRoles;
