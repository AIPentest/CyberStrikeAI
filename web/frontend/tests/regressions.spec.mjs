import { test, expect, visit, ready, label, UI_PERMISSIONS, STAMP } from './fixtures.mjs';

test('read notification does not add to unread count and its timestamp is shown', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/notifications/summary', { json: {
        unreadCount: 0, items: [{ id: 'read-fixture', type: 'fixture', title: '示例已读通知', ts: STAMP, read: true, count: 7 }]
    } });
    await visit(page, 'platform-rbac');
    await page.getByRole('button', { name: label('notifications.title', '通知'), exact: true }).click();
    const item = page.getByRole('button', { name: /示例已读通知/ });
    await expect(item.locator('.cs-notification-meta')).toContainText('2026');
    await expect(page.locator('.semi-badge-count')).toHaveCount(0);
    expect(mockApi.writes()).toEqual([]);
});

test.describe('sign-in integration', () => {
    test.use({ authenticated: false });
    test('successful sign-in opens the app with a synthetic scoped session', async ({ page, mockApi }) => {
        // The fixture validates its own synthetic token on all subsequent requests.
        mockApi.respond('POST', '/api/auth/login', { json: {
            token: 'FAKE-PLAYWRIGHT-NOT-A-REAL-TOKEN', expires_at: new Date(Date.now() + 3600000).toISOString(),
            permissions: UI_PERMISSIONS, roles: [], user: { id: 'fixture-user', username: 'fixture' }
        } });
        await page.goto('/static/app/');
        const form = page.locator('.cs-login-form');
        await form.getByLabel(label('login.username', '用户名'), { exact: true }).fill('fixture');
        await form.getByLabel(label('login.password', '密码'), { exact: true }).fill('fixture-password');
        await form.getByRole('button', { name: /登录$/ }).click();
        await ready(page);
        await expect(form).toHaveCount(0);
    });
});

test('preview links keep the classic route and the current session', async ({ page, mockApi }) => {
    await page.route(url => url.pathname === '/', route => route.fulfill({
        contentType: 'text/html',
        body: '<title>Classic workspace fixture</title>'
    }));

    await visit(page, 'dashboard');
    await page.getByRole('button', { name: label('dashboard.runningConversations', '运行中任务'), exact: true }).click();
    await expect(page).toHaveURL(/\/#tasks$/);

    await visit(page, 'dashboard');
    await page.getByRole('button', { name: label('dashboard.tokenUsage', 'Token 用量（7 天）'), exact: true }).click();
    await expect(page).toHaveURL(/\/#settings\?section=token-usage$/);

    await visit(page, 'dashboard');
    await page.getByRole('button', { name: label('auth.userMenu', '用户菜单'), exact: true }).click();
    await page.getByText(label('header.classicWorkspace', '返回经典界面'), { exact: true }).click();
    await expect(page).toHaveTitle('Classic workspace fixture');
    await expect(page).toHaveURL(/:5199\/$/);
    expect(await page.evaluate(() => Boolean(localStorage.getItem('cyberstrike-auth')))).toBe(true);
    expect(mockApi.writes()).toEqual([]);
});

test('permission and feature availability control the administrative buttons', async ({ page, mockApi }) => {
    await visit(page, 'platform-rbac');
    await expect(page.getByRole('button', { name: '新建角色' })).toHaveCount(0);

    mockApi.respond('GET', '/api/auth/validate', { json: {
        expires_at: new Date(Date.now() + 3600000).toISOString(),
        user: { id: 'fixture-user', username: 'fixture' },
        permissions: [...UI_PERMISSIONS, 'rbac:write', 'knowledge:write']
    } });
    await page.reload();
    await ready(page);
    await page.getByRole('button', { name: '新建角色' }).click();
    const role = page.getByRole('dialog');
    await role.getByRole('button', { name: /确定$/ }).click();
    await expect(role.getByText('请输入角色名称')).toBeVisible();
    await role.getByRole('button', { name: /取消$/ }).click();
    await expect(role).toHaveCount(0);

    mockApi.respond('GET', '/api/knowledge/items', { json: {
        enabled: false, items: [], message: '知识库未启用（fixture）'
    } });
    await visit(page, 'knowledge-management');
    await expect(page.getByText('知识库未启用（fixture）')).toBeVisible();
    await expect(page.getByRole('button', { name: '新增知识' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: '检索测试' })).toBeDisabled();
    expect(mockApi.writes()).toEqual([]);
});

test('saving an asset directly from the target field preserves its address and title', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/auth/validate', { json: {
        expires_at: new Date(Date.now() + 3600000).toISOString(),
        user: { id: 'fixture-user', username: 'fixture' },
        permissions: [...UI_PERMISSIONS, 'asset:write']
    } });
    mockApi.respond('POST', '/api/assets/import', { json: { created: 1 } });
    await visit(page, 'asset-library');
    await page.getByRole('button', { name: /新增资产$/ }).click();
    const editor = page.getByRole('dialog');
    await editor.getByRole('textbox', { name: /页面标题/ }).fill('匿名资产');
    await editor.getByRole('textbox', { name: /资产地址/ }).fill('semi.example');
    await editor.getByRole('button', { name: /添加资产$/ }).click();
    await expect(editor).toHaveCount(0);
    const [save] = mockApi.calls('POST', '/api/assets/import');
    expect(save.body.assets[0]).toMatchObject({ domain: 'semi.example', title: '匿名资产' });
});
