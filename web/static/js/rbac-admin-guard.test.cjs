const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function setup() {
    const elements = new Map([
        'rbac-user-id', 'rbac-username', 'rbac-display-name', 'rbac-password',
        'rbac-user-enabled', 'rbac-user-roles-create-section', 'rbac-user-roles-edit-section',
        'rbac-user-roles-readonly', 'rbac-user-role-checkboxes', 'rbac-roles-list',
    ].map(id => [id, { value: '', checked: false, disabled: false, dataset: {}, innerHTML: '' }]));
    const context = vm.createContext({
        window: {}, console,
        document: { getElementById: id => elements.get(id) || null, querySelector: () => null, addEventListener() {} },
        openAppModal() {},
        escapeHtml: text => String(text),
    });
    vm.runInContext(fs.readFileSync(path.join(__dirname, 'rbac.js'), 'utf8') + '\nglobalThis.state = rbacState;', context);
    context.state.users = [
        { id: 'admin', username: 'admin', enabled: true, is_builtin: true, roles: ['admin'] },
        { id: 'member', username: 'member', enabled: true, roles: ['admin', 'operator'] },
    ];
    context.state.roles = [
        { id: 'admin', name: 'Administrator', scope: 'all', is_system: true },
        { id: 'operator', name: 'Operator', scope: 'assigned', is_system: true },
    ];
    return { context, elements };
}

function adminCheckbox(html) {
    return html.match(/<input[^>]+onchange="toggleSelectedUserRole\('admin'[^>]+>/)[0];
}

test('built-in enabled control is locked, then resets for member editing and creation', () => {
    const { context, elements } = setup();
    const input = elements.get('rbac-user-enabled');
    context.openRbacUserModal('admin');
    assert.equal(input.checked, true);
    assert.equal(input.disabled, true);
    assert.equal(input.dataset.stateDisabled, 'true');
    assert.ok(input.title);
    context.openRbacUserModal('member');
    assert.equal(input.disabled, false);
    assert.equal(input.dataset.stateDisabled, 'false');
    assert.equal(input.title, '');
    context.openRbacUserModal('admin');
    context.openRbacUserModal();
    assert.equal(input.disabled, false);
    assert.equal(input.checked, true);
    assert.equal(elements.get('rbac-user-id').value, '');
});

test('built-in admin role cannot be unchecked; another admin account can be demoted', () => {
    const { context, elements } = setup();
    context.state.selectedUserId = 'admin';
    context.renderRbacRoles();
    let checkbox = adminCheckbox(elements.get('rbac-roles-list').innerHTML);
    assert.match(checkbox, /\bchecked\b/);
    assert.match(checkbox, /\bdisabled\b/);
    context.toggleSelectedUserRole('admin', false);
    assert.equal(context.state.pendingUserRoles.has('admin'), true);
    context.state.selectedUserId = 'member';
    context.renderRbacRoles();
    checkbox = adminCheckbox(elements.get('rbac-roles-list').innerHTML);
    assert.doesNotMatch(checkbox, /\bdisabled\b/);
    context.toggleSelectedUserRole('admin', false);
    assert.equal(context.state.pendingUserRoles.has('admin'), false);
    assert.equal(context.state.pendingUserRoles.has('operator'), true);
});

test('protection still permits explicitly restoring a historically damaged built-in account', () => {
    const { context, elements } = setup();
    const admin = context.state.users[0];
    admin.enabled = false;
    admin.roles = [];
    context.openRbacUserModal('admin');
    assert.equal(elements.get('rbac-user-enabled').disabled, false);
    context.state.selectedUserId = 'admin';
    context.renderRbacRoles();
    assert.doesNotMatch(adminCheckbox(elements.get('rbac-roles-list').innerHTML), /\bdisabled\b/);
    context.toggleSelectedUserRole('admin', true);
    assert.match(adminCheckbox(elements.get('rbac-roles-list').innerHTML), /\bdisabled\b/);
});
