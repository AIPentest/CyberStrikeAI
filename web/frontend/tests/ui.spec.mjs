import { readFileSync } from 'node:fs';
import {
    test, expect, visit, ready, screenshot, label,
    READ_PERMISSIONS, UI_PERMISSIONS, PROJECT, FACT, VULNERABILITY, ASSESSMENT,
    CONVERSATION, STREAM_REPLY
} from './fixtures.mjs';

// Read the real registry without importing lazy JSX into Node. New pages join the sweep automatically.
const registry = readFileSync(new URL('../src/pages/registry.js', import.meta.url), 'utf8');
const pageIds = [...registry.matchAll(/^\s*(?:'([^']+)'|([\w-]+)):\s*lazy\(/gm)].map(match => match[1] || match[2]);
if (!pageIds.length || new Set(pageIds).size !== pageIds.length) throw new Error('Cannot enumerate the React page registry');

const PAGE_READS = {
    dashboard: ['/api/vulnerabilities/stats', '/api/monitor/stats', '/api/usage/tokens', '/api/projects/dashboard-summary'],
    chat: ['/api/conversations', '/api/projects'],
    'deep-assessments': ['/api/deep-assessments', `/api/deep-assessments/${ASSESSMENT.id}`],
    hitl: ['/api/hitl/pending'],
    'tool-guard': ['/api/tool-guard'],
    projects: ['/api/projects', `/api/projects/${PROJECT.id}`, `/api/projects/${PROJECT.id}/facts`],
    'asset-overview': ['/api/assets/stats', '/api/assets'],
    'asset-library': ['/api/assets'],
    'info-collect': [], // No search is triggered just by opening the page.
    vulnerabilities: ['/api/vulnerabilities', '/api/vulnerabilities/filter-options'],
    tasks: ['/api/batch-tasks'],
    workflows: ['/api/workflows', '/api/workflows/runs/pending'],
    webshell: ['/api/webshell/connections'],
    'c2-listeners': ['/api/c2/listeners'],
    'c2-sessions': ['/api/c2/sessions'],
    'c2-tasks': ['/api/c2/tasks'],
    'c2-payloads': ['/api/c2/listeners'],
    'c2-events': ['/api/c2/events'],
    'c2-profiles': ['/api/c2/profiles'],
    'chat-files': ['/api/chat-uploads'],
    'mcp-monitor': ['/api/monitor', '/api/monitor/calls-timeline'],
    'mcp-management': ['/api/external-mcp', '/api/config/tools'],
    'knowledge-retrieval-logs': ['/api/knowledge/retrieval-logs'],
    'knowledge-management': ['/api/knowledge/items', '/api/knowledge/index-status'],
    'skills-monitor': ['/api/skills/stats'],
    'skills-management': ['/api/skills'],
    'agents-management': ['/api/multi-agent/markdown-agents'],
    'roles-management': ['/api/roles'],
    'platform-rbac': ['/api/rbac/metadata', '/api/rbac/roles', '/api/rbac/users', '/api/rbac/resource-assignments'],
    settings: ['/api/config'],
    'api-docs': ['/api/openapi/spec']
};

async function readsCompleted(mockApi, paths) {
    for (const path of paths) {
        await expect.poll(() => mockApi.calls('GET', path).filter(call => call.fulfilled).length, { message: `Expected fixture read: ${path}` }).toBeGreaterThan(0);
    }
}

function button(scope, name) {
    const text = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return scope.getByRole('button', { name: new RegExp(`(?:^|\\s)${text}$`) });
}

async function assertNoOverflow(page) {
    const sizes = await page.evaluate(() => {
        const elements = [document.documentElement, document.body, document.querySelector('#main-content')];
        return elements.filter(Boolean).map(element => ({
            element: element.id || element.tagName,
            width: element.clientWidth, scroll: element.scrollWidth,
            left: element.getBoundingClientRect().left,
            right: element.getBoundingClientRect().right,
            viewport: window.innerWidth
        }));
    });
    for (const size of sizes) {
        expect(size.scroll, `${size.element} horizontal overflow`).toBeLessThanOrEqual(size.width + 1);
        expect(size.left, `${size.element} left edge`).toBeGreaterThanOrEqual(-1);
        expect(size.right, `${size.element} right edge`).toBeLessThanOrEqual(size.viewport + 1);
    }
}

async function assertDrawerFits(page, drawer) {
    await expect.poll(async () => {
        const box = await drawer.boundingBox();
        return Boolean(box && box.x >= -1 && box.x + box.width <= page.viewportSize().width + 1);
    }, { message: 'The open drawer must settle inside the viewport' }).toBe(true);
}

test.describe('registry: every lazy page', () => {
    for (const id of pageIds) {
        test(`registry: ${id}`, async ({ page, mockApi }) => {
            await visit(page, id);
            await readsCompleted(mockApi, PAGE_READS[id] || []);
            const main = await ready(page);
            await expect(page).toHaveURL(new RegExp(`#/${id}(?:\\?|$)`));
            await expect(main.locator('.cs-error-banner')).toHaveCount(0);
            await expect(main.locator('.cs-page-title, .chat-header-title')).not.toHaveText('');
            expect(mockApi.writes(), 'Opening a page must not launch a task or mutate data').toEqual([]);
            expect(mockApi.pageErrors).toEqual([]);
            expect(mockApi.unexpected).toEqual([]);
        });
    }
});

test.describe('login', () => {
    test.use({ authenticated: false });
    test('empty fields validate locally; only explicitly fake credentials may be submitted', async ({ page, mockApi }) => {
        await page.goto('/static/app/');
        const form = page.locator('.cs-login-form');
        const submit = button(form, label('login.submit', '登录'));
        await submit.click();
        await expect(form.getByText(label('login.usernameRequired', '请输入用户名'), { exact: true })).toBeVisible();
        await expect(form.getByText(label('login.passwordRequired', '请输入密码'), { exact: true })).toBeVisible();
        expect(mockApi.requests).toEqual([]);

        await form.getByLabel(label('login.username', '用户名'), { exact: true }).fill('FAKE_TEST_USER');
        await submit.click();
        await expect(form.getByText(label('login.passwordRequired', '请输入密码'), { exact: true })).toBeVisible();
        expect(mockApi.writes()).toEqual([]);

        mockApi.respond('POST', '/api/auth/login', { status: 401, json: { error: 'FAKE 测试登录被拒绝' } });
        await form.getByLabel(label('login.password', '密码'), { exact: true }).fill('FAKE_PASSWORD_NOT_A_CREDENTIAL');
        await submit.click();
        await expect(form.getByText('FAKE 测试登录被拒绝', { exact: true })).toBeVisible();
        expect(mockApi.calls('POST', '/api/auth/login').map(call => call.body)).toEqual([
            { username: 'FAKE_TEST_USER', password: 'FAKE_PASSWORD_NOT_A_CREDENTIAL' }
        ]);
        await expect(submit).toBeEnabled();
        await expect(page.locator('#main-content')).toHaveCount(0);
    });
});

test('dashboard: exact populated metrics and anonymous records', async ({ page }) => {
    const main = await visit(page, 'dashboard');
    const metrics = [
        [label('dashboard.runningConversations', '运行中任务'), '2'],
        [label('dashboard.vulnTotal', '漏洞总数'), '3'],
        [label('dashboard.toolCalls', '工具调用'), '10'],
        [label('dashboard.successRate', '执行成功率'), '80.0%'],
        [label('dashboard.tokenUsage', 'Token 用量（7 天）'), '1,200']
    ];
    for (const [name, value] of metrics) {
        await expect(button(main, name).locator('.cs-stat-value')).toHaveText(value);
    }
    await expect(button(main, PROJECT.name)).toBeVisible();
    await expect(button(main, VULNERABILITY.title)).toBeVisible();
    await expect(main.getByText(FACT.summary, { exact: true })).toBeVisible();
    await expect(main.getByText(/^(部分数据不可用|数据暂不可用)$/)).toHaveCount(0);
});

test('dashboard: severity segment and legend carry the active status into the vulnerability list', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/vulnerabilities/stats', (_, url) => ({
        json: url.searchParams.get('status') === 'fixed'
            ? { total: 4, by_severity: { high: 1, low: 3 }, by_status: { fixed: 4 } }
            : { total: 12, by_severity: { high: 3, low: 9 }, by_status: { open: 3, confirmed: 1, fixed: 4, ignored: 2, false_positive: 2 } }
    }));
    const main = await visit(page, 'dashboard');
    await expect(main.locator('.dashboard-risk-value')).toHaveText('50.0%');
    const card = main.locator('.dashboard-vulnerability-card');
    await card.locator('.semi-select').click();
    await page.getByRole('option', { name: /已修复$/ }).click();
    await page.mouse.move(1, 1);
    await expect(card.locator('.dashboard-ring-center strong')).toHaveText('4');
    const segment = card.locator('.dashboard-ring-link[data-severity="high"] path');
    // Click the visible SVG fill rather than the center of a path bounding box.
    const point = await segment.evaluate(path => {
        const bounds = path.getBBox();
        for (let x = 1; x < 10; x++) {
            for (let y = 1; y < 10; y++) {
                const inside = new DOMPoint(bounds.x + bounds.width * x / 10, bounds.y + bounds.height * y / 10);
                if (path.isPointInFill(inside)) {
                    const screen = inside.matrixTransform(path.getScreenCTM());
                    return { x: screen.x, y: screen.y };
                }
            }
        }
        return null;
    });
    expect(point).not.toBeNull();
    await page.mouse.move(point.x, point.y);
    await expect(card.locator('.dashboard-ring-center strong')).toHaveText('1');
    await page.mouse.click(point.x, point.y);
    await expect(page).toHaveURL(/#\/vulnerabilities\?severity=high&status=fixed$/);
    await expect.poll(() => mockApi.calls('GET', '/api/vulnerabilities').some(call => {
        const query = new URLSearchParams(call.query);
        return query.get('severity') === 'high' && query.get('status') === 'fixed';
    })).toBe(true);

    await page.goBack();
    await page.mouse.move(1, 1);
    await expect(card.locator('.dashboard-ring-center strong')).toHaveText('4');
    await card.locator('.dashboard-severity-item[data-severity="low"]').press('Enter');
    await expect(page).toHaveURL(/#\/vulnerabilities\?severity=low&status=fixed$/);
    expect(mockApi.writes()).toEqual([]);
});

for (const [key, fallback, target] of [
    ['dashboard.runningConversations', '运行中任务', 'tasks'],
    ['dashboard.vulnTotal', '漏洞总数', 'vulnerabilities'],
    ['dashboard.toolCalls', '工具调用', 'mcp-monitor'],
    ['dashboard.successRate', '执行成功率', 'mcp-monitor'],
    ['dashboard.tokenUsage', 'Token 用量（7 天）', 'settings?section=token-usage']
]) {
    test(`dashboard: metric navigates to ${target} (${key})`, async ({ page }) => {
        const main = await visit(page, 'dashboard');
        await button(main, label(key, fallback)).click();
        await expect(page).toHaveURL(new URL(`/static/app/#/${target}`, page.url()).href);
        await ready(page);
    });
}

test('navigation: four Semi groups, parent expansion, child selection and collapse persistence', async ({ page }) => {
    await visit(page, 'asset-library');
    const sidebar = page.locator('.cs-sider');
    await expect(sidebar.locator('.cs-nav-section')).toHaveCount(4);
    await expect(sidebar.getByRole('menu')).toHaveCount(4);
    for (const [key, fallback] of [
        ['groupWorkspace', '工作台'], ['groupOperations', '安全作业'],
        ['groupCapabilities', '能力中心'], ['groupPlatform', '平台管理']
    ]) {
        const section = sidebar.getByRole('region', { name: label(`nav.${key}`, fallback), exact: true });
        await expect(section).toBeVisible();
        await expect(section.getByRole('menu')).toHaveCount(1);
    }
    const parent = sidebar.getByRole('menuitem', { name: label('nav.assets', '资产管理'), exact: true });
    const selected = sidebar.getByRole('menuitem', { name: label('nav.assetLibrary', '资产库'), exact: true });
    await expect(parent).toHaveAttribute('aria-expanded', 'true');
    await expect(selected).toHaveClass(/semi-navigation-item-selected/);
    await expect(sidebar.locator('.semi-navigation-item-selected')).toHaveCount(1);
    const overview = sidebar.getByRole('menuitem', { name: label('nav.assetOverview', '资产概览'), exact: true });
    await overview.click();
    await expect(page).toHaveURL(/#\/asset-overview$/);
    await expect(overview).toHaveClass(/semi-navigation-item-selected/);
    await expect(selected).not.toHaveClass(/semi-navigation-item-selected/);

    const wide = await sidebar.boundingBox();
    await button(sidebar, label('nav.collapse', '收起菜单')).click();
    await expect(page.locator('.cs-shell')).toHaveClass(/is-collapsed/);
    await expect.poll(async () => (await sidebar.boundingBox()).width).toBeLessThan(wide.width);
    await page.reload();
    await ready(page);
    await expect(page.locator('.cs-shell')).toHaveClass(/is-collapsed/);
    await button(sidebar, label('nav.expand', '展开菜单')).click();
    await expect(page.locator('.cs-shell')).not.toHaveClass(/is-collapsed/);
    await expect(parent).toHaveAttribute('aria-expanded', 'true');
    await expect(overview).toHaveClass(/semi-navigation-item-selected/);
    await expect.poll(() => page.evaluate(() => localStorage.getItem('csai-nav-collapsed'))).toBe('0');
});

test('navigation: collapsed submenus fit their labels and center beside the parent', async ({ page }, testInfo) => {
    await visit(page, 'chat');
    const sidebar = page.locator('.cs-sider');
    await button(sidebar, label('nav.collapse', '收起菜单')).click();
    await expect(page.locator('.cs-shell')).toHaveClass(/is-collapsed/);

    const mcp = sidebar.getByRole('menuitem', { name: 'MCP', exact: true });
    const monitor = page.getByRole('menuitem', { name: label('nav.mcpMonitor', 'MCP状态监控'), exact: true });
    const mcpMenu = page.locator('.semi-navigation-popover').filter({ has: monitor });
    await mcp.hover();
    await expect(monitor).toBeVisible();
    const mcpBox = await mcp.boundingBox();
    await expect.poll(async () => {
        const menu = await mcpMenu.boundingBox();
        return Math.abs(menu.y + menu.height / 2 - (mcpBox.y + mcpBox.height / 2));
    }).toBeLessThanOrEqual(1);
    const menuBox = await mcpMenu.boundingBox();
    const monitorText = await monitor.locator('.semi-navigation-item-text').boundingBox();
    const leftPadding = monitorText.x - menuBox.x;
    const rightPadding = menuBox.x + menuBox.width - monitorText.x - monitorText.width;
    expect(Math.abs(leftPadding - rightPadding)).toBeLessThanOrEqual(1);
    await screenshot(page, testInfo, 'collapsed-mcp-menu');

    const c2 = sidebar.getByRole('menuitem', { name: 'C2', exact: true });
    const profiles = page.getByRole('menuitem', { name: label('nav.c2Profiles', '流量伪装'), exact: true });
    const c2Menu = page.locator('.semi-navigation-popover').filter({ has: profiles });
    await c2.hover();
    await expect(c2Menu.getByRole('menuitem')).toHaveCount(6);
    const c2Box = await c2.boundingBox();
    const c2Center = c2Box.y + c2Box.height / 2;
    await expect.poll(async () => {
        const menu = await c2Menu.boundingBox();
        return Math.abs(menu.y + menu.height / 2 - c2Center);
    }).toBeLessThanOrEqual(1);
    const tallMenu = await c2Menu.boundingBox();
    expect(tallMenu.width).toBeLessThan(menuBox.width);

    // Put the trigger near the lower edge so the menu no longer fits at its centered position.
    const height = Math.ceil(c2Box.y + c2Box.height + 60);
    expect(c2Center + tallMenu.height / 2).toBeGreaterThan(height);
    await page.setViewportSize({ width: 1440, height });
    await expect.poll(async () => {
        const menu = await c2Menu.boundingBox();
        return menu.y >= 0 && menu.y + menu.height <= height && menu.y <= c2Center;
    }).toBe(true);
    await profiles.click();
    await expect(page).toHaveURL(/#\/c2-profiles$/);
});

test('theme: menu switches dark/light, actual primary colors, reload preserves preference', async ({ page }, testInfo) => {
    await visit(page, 'dashboard');
    let current = label('theme.system', '跟随系统');
    for (const [theme, name, hex, rgb] of [
        ['dark', label('theme.dark', '暗色'), '#81a7ff', 'rgb(129, 167, 255)'],
        ['light', label('theme.light', '浅色'), '#3266e3', 'rgb(50, 102, 227)']
    ]) {
        await button(page.locator('.cs-header'), current).click();
        await page.getByRole('menuitem', { name, exact: true }).click();
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(page.locator('body')).toHaveAttribute('theme-mode', theme);
        await expect.poll(() => page.evaluate(() => localStorage.getItem('cyberstrike-theme'))).toBe(theme);
        await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).getPropertyValue('--semi-color-primary').trim().toLowerCase())).toBe(hex);
        await expect(button(page.locator('#main-content'), '进入对话')).toHaveCSS('background-color', rgb);
        await page.reload();
        await ready(page);
        await expect(page.locator('html')).toHaveAttribute('data-theme-preference', theme);
        await expect(page.locator('body')).toHaveAttribute('theme-mode', theme);
        await expect(button(page.locator('.cs-header'), name)).toBeVisible();
        await screenshot(page, testInfo, `dashboard-${theme}`);
        current = name;
    }
});

test('chat: empty state, blank Enter and Shift+Enter never send', async ({ page, mockApi }) => {
    const main = await visit(page, 'chat');
    await expect(main.locator('.chat-welcome')).toBeVisible();
    const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容'), exact: true });
    await input.press('Enter');
    await expect(page.getByText(label('chat.emptyMessage', '请输入内容或添加附件'), { exact: true })).toBeVisible();
    await input.fill('FAKE：仅整理 localhost 示例范围');
    await input.press('End');
    await input.press('Shift+Enter');
    await input.pressSequentially('FAKE second line');
    await expect(input).toHaveValue('FAKE：仅整理 localhost 示例范围\nFAKE second line');
    expect(mockApi.writes()).toEqual([]);
});

test.describe('chat: read-only permissions', () => {
    test.use({ appPermissions: READ_PERMISSIONS });
    test('no chat:write disables input, attachment and send', async ({ page, mockApi }) => {
        const main = await visit(page, 'chat');
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容'), exact: true });
        await expect(input).toBeDisabled();
        await expect(button(main, label('chat.send', '发送'))).toBeDisabled();
        await expect(button(main, label('chat.attachmentsLabel', '附件'))).toBeDisabled();
        await expect(main.getByText(label('chat.noWritePermission', '当前账号没有对话写入权限（chat:write），发送已禁用。'), { exact: true })).toBeVisible();
        // Send real keyboard events, without force-enabling a disabled application control.
        await main.locator('.chat-header-title').click();
        await page.keyboard.press('Enter');
        await page.keyboard.press('Shift+Enter');
        await expect(input).toHaveValue('');
        expect(mockApi.writes()).toEqual([]);
    });
});

test.describe('chat: execution permission boundary', () => {
    test.use({ appPermissions: UI_PERMISSIONS.filter(permission => permission !== 'agent:execute') });
    test('chat:write without agent:execute must not send an agent request', async ({ page, mockApi }) => {
        const main = await visit(page, 'chat');
        // Strict regression, not test.fail/skip: backend requires agent:execute for /agent/stream.
        await expect(button(main, label('chat.send', '发送'))).toBeDisabled();
        expect(mockApi.writes()).toEqual([]);
    });
});

test('chat: select stored conversation and restore it after reload', async ({ page, mockApi }) => {
    const main = await visit(page, 'chat');
    await main.getByRole('button', { name: new RegExp(`^${CONVERSATION.title}`) }).click();
    await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${CONVERSATION.id}$`));
    await expect(main.getByRole('heading', { name: CONVERSATION.title, exact: true })).toBeVisible();
    for (const message of CONVERSATION.messages) await expect(main.getByText(message.content, { exact: true })).toBeVisible();
    const row = main.locator('.chat-conv-main').filter({ hasText: CONVERSATION.title });
    await expect(row).toHaveAttribute('aria-current', 'true');
    await page.reload();
    await ready(page);
    await expect(main.getByText(CONVERSATION.messages[1].content, { exact: true })).toBeVisible();
    await readsCompleted(mockApi, [`/api/conversations/${CONVERSATION.id}`]);
    expect(mockApi.writes()).toEqual([]);
});

test('chat: floating composer stays aligned and shows focus', async ({ page, mockApi }, testInfo) => {
    mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
    await page.goto(`/static/app/#/chat?conversation=${CONVERSATION.id}`);
    const main = await ready(page);
    const panel = main.locator('.chat-panel');
    const messages = panel.locator('.chat-messages');
    const assistant = messages.locator('.chat-msg-assistant').first();
    const composer = panel.locator('.chat-composer');
    const composerBox = composer.locator('.chat-composer-box');
    const input = composer.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
    await expect(assistant).toBeVisible();

    const [timeline, footer, box] = await Promise.all(
        [messages, composer, composerBox].map(element => element.boundingBox())
    );
    expect(footer.x).toBeCloseTo(timeline.x, 0);
    expect(footer.width).toBeCloseTo(timeline.width, 0);
    expect(footer.y).toBeCloseTo(timeline.y + timeline.height, 0);
    expect(box.x).toBeGreaterThanOrEqual(timeline.x);
    expect(box.width).toBeLessThanOrEqual(footer.width);
    await expect(composer).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await expect(composerBox).toHaveCSS('border-top-width', '1px');
    await expect(composerBox).toHaveCSS('border-radius', '16px');

    const borderColor = await composerBox.evaluate(node => getComputedStyle(node).borderTopColor);
    await input.click();
    await expect(input).toBeFocused();
    await expect(input).toHaveCSS('outline-style', 'none');
    await expect(composerBox).not.toHaveCSS('border-top-color', borderColor);
    expect(mockApi.writes()).toEqual([]);
    await panel.screenshot({ path: testInfo.outputPath('focused-chat-panel.png') });
});

test('chat: fake Enter send consumes finite SSE and reconciles stored history', async ({ page, mockApi }) => {
    const message = 'FAKE：只总结 localhost 匿名范围，不调用工具。';
    const id = mockApi.enableChatStream(message);
    const main = await visit(page, 'chat');
    const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容'), exact: true });
    await input.fill(message);
    await input.press('Enter');
    await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
    await expect(main.locator('.chat-msg-assistant').getByText(STREAM_REPLY, { exact: true })).toBeVisible();
    await expect(main.locator('.chat-msg-user').getByText(message, { exact: true })).toHaveCount(1);
    await expect(input).toHaveValue('');
    await expect(button(main, label('chat.send', '发送'))).toBeEnabled();
    await readsCompleted(mockApi, [`/api/conversations/${id}`]);
    expect(mockApi.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([
        { method: 'POST', path: '/api/agent/stream', body: { message, conversationId: '' } }
    ]);
    await main.locator('.chat-events-strip .semi-collapse-header').click();
    await expect(main.locator('.chat-events-strip pre')).toContainText('"type":"response"');
    await expect(main.locator('.chat-events-strip pre')).not.toContainText('response_delta');
    await expect(main.locator('.chat-events-strip pre')).toContainText('"type":"done"');
    await page.reload();
    await ready(page);
    await expect(main.locator('.chat-msg-assistant').getByText(STREAM_REPLY, { exact: true })).toBeVisible();
    expect(mockApi.calls('POST', '/api/agent/stream')).toHaveLength(1);
});

for (const [id, path, record] of [
    ['deep-assessments', '/api/deep-assessments', ASSESSMENT.name],
    ['vulnerabilities', '/api/vulnerabilities', VULNERABILITY.title],
    ['projects', '/api/projects', PROJECT.name]
]) {
    test(`retry: ${id} displays API failure then recovers through its retry button`, async ({ page, mockApi }) => {
        mockApi.fail('GET', path, '示例列表加载失败');
        const main = await visit(page, id);
        const banner = main.locator('.cs-error-banner');
        await expect(banner).toContainText('示例列表加载失败');
        const count = mockApi.calls('GET', path).length;
        mockApi.restore('GET', path);
        await banner.getByRole('button').click();
        await expect.poll(() => mockApi.calls('GET', path).length).toBeGreaterThan(count);
        await expect(banner).toHaveCount(0);
        await expect(main.locator('.cs-table-wrap').getByText(record, { exact: true })).toBeVisible();
        expect(mockApi.writes()).toEqual([]);
    });
}

test('deep assessment: archive filter stays horizontal and switches the list without writes', async ({ page, mockApi }) => {
    mockApi.respond('GET', '/api/deep-assessments', (_, url) => ({
        json: { tasks: url.searchParams.get('archived') === 'true' ? [] : [ASSESSMENT], has_more: false }
    }));
    const main = await visit(page, 'deep-assessments');
    const filter = main.getByRole('radiogroup', { name: '归档状态' });
    const unarchived = filter.getByRole('radio', { name: '未归档' });
    const archived = filter.getByRole('radio', { name: '已归档' });
    const unarchivedLabel = filter.getByText('未归档', { exact: true });
    const archivedLabel = filter.getByText('已归档', { exact: true });
    const draft = button(main, ASSESSMENT.name);

    await expect(unarchived).toBeChecked();
    await expect(draft).toBeVisible();
    await expect(filter).toBeInViewport({ ratio: 1 });
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(filter).toBeInViewport({ ratio: 1 });
    expect((await unarchivedLabel.boundingBox()).y).toBe((await archivedLabel.boundingBox()).y);

    await archivedLabel.click();
    await expect(archived).toBeChecked();
    await expect(draft).toHaveCount(0);
    await unarchivedLabel.click();
    await expect(unarchived).toBeChecked();
    await expect(draft).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: options retry, required objective, close new-draft modal without execution', async ({ page, mockApi }, testInfo) => {
    mockApi.fail('GET', '/api/deep-assessments/options', '示例选项加载失败');
    const main = await visit(page, 'deep-assessments');
    await button(main, label('deepAssessments.create', '新建测评')).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('示例选项加载失败');
    mockApi.restore('GET', '/api/deep-assessments/options');
    await button(dialog, '重试').click();
    await expect(dialog.locator('.cs-error-banner')).toHaveCount(0);
    await button(dialog, label('deepAssessments.save', '保存草稿')).click();
    await expect(dialog.getByText('请填写测评目标', { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await screenshot(page, testInfo, 'assessment-validation');
    await button(dialog.locator('.assessment-form-actions'), label('deepAssessments.close', '关闭')).click();
    await expect(dialog).toHaveCount(0);
    await expect(button(main, ASSESSMENT.name)).toBeVisible();
});

test('vulnerability: dashboard deep link, detail retry, evidence and close; new form validation', async ({ page, mockApi }, testInfo) => {
    mockApi.fail('GET', `/api/vulnerabilities/${VULNERABILITY.id}/context`, '示例详情加载失败');
    await visit(page, 'dashboard');
    await button(page.locator('#main-content'), VULNERABILITY.title).click();
    await expect(page).toHaveURL(new RegExp(`#/vulnerabilities\\?id=${VULNERABILITY.id}$`));
    const drawer = page.getByRole('dialog');
    await expect(drawer.locator('.cs-error-banner')).toContainText('示例详情加载失败');
    mockApi.restore('GET', `/api/vulnerabilities/${VULNERABILITY.id}/context`);
    await drawer.locator('.cs-error-banner').getByRole('button').click();
    await expect(drawer).toContainText(VULNERABILITY.description);
    await drawer.getByRole('tab', { name: label('vulnerabilityPage.inspector_evidence', '复现与证据'), exact: true }).click();
    await expect(drawer.getByText(VULNERABILITY.evidence, { exact: true })).toBeVisible();
    await screenshot(page, testInfo, 'vulnerability-detail');
    await button(drawer, label('common.close', '关闭')).click();
    await expect(drawer).toHaveCount(0);
    await expect(page).toHaveURL(/#\/vulnerabilities$/);
    await button(page.locator('#main-content'), label('vulnerability.addVuln', '添加漏洞')).click();
    const editor = page.getByRole('dialog');
    await button(editor, label('common.save', '保存')).click();
    await expect(editor.getByText(label('vulnerabilityModal.titleRequired', '请填写漏洞标题'), { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await button(editor, label('common.cancel', '取消')).click();
    await expect(editor).toHaveCount(0);
});

test('project: dashboard jump, fact detail opens/closes and create validates then retries', async ({ page, mockApi }) => {
    await visit(page, 'dashboard');
    await button(page.locator('#main-content'), PROJECT.name).click();
    await expect(page).toHaveURL(new RegExp(`#/projects\\?id=${PROJECT.id}$`));
    const main = await ready(page);
    await button(main, FACT.summary).click();
    const drawer = page.getByRole('dialog');
    await expect(drawer.getByText(FACT.body, { exact: true })).toBeVisible();
    await button(drawer, label('common.close', '关闭')).click();
    await expect(drawer).toHaveCount(0);

    await button(main, label('projects.newProjectCta', '+ 新建项目')).click();
    const dialog = page.getByRole('dialog');
    const create = button(dialog, label('projects.createProject', '创建项目'));
    await create.click();
    await expect(page.getByText(label('projects.enterProjectName', '请输入项目名称'), { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await dialog.getByLabel(label('projects.projectName', '项目名称'), { exact: true }).fill(PROJECT.name);
    await dialog.getByLabel(label('projects.projectDescription', '项目描述'), { exact: true }).fill(PROJECT.description);
    mockApi.fail('POST', '/api/projects', '示例保存失败，可重试');
    await create.click();
    await expect(page.getByText('示例保存失败，可重试', { exact: true })).toBeVisible();
    await expect(dialog).toBeVisible();
    await expect(dialog.locator('input')).toHaveValue(PROJECT.name);
    mockApi.respond('POST', '/api/projects', { json: PROJECT });
    await create.click();
    await expect(dialog).toHaveCount(0);
    expect(mockApi.calls('POST', '/api/projects').map(call => call.body)).toEqual([
        { name: PROJECT.name, description: PROJECT.description },
        { name: PROJECT.name, description: PROJECT.description }
    ]);
});

test('shared JsonViewer: real settings response expands and collapses', async ({ page, mockApi }) => {
    const main = await visit(page, 'settings');
    await main.getByText(label('settings.jsonPanel', 'GET /config 原始响应（JSON，便于核对字段名）'), { exact: true }).click();
    const viewer = main.locator('.cs-json-viewer');
    const expand = viewer.getByRole('button', { expanded: false });
    await expect(expand).toBeVisible();
    await expect(viewer.locator('pre')).not.toBeVisible();
    await expand.click();
    await expect(viewer.locator('pre')).toBeVisible();
    await expect(viewer.locator('pre')).toContainText('"tools": []');
    await viewer.getByRole('button', { expanded: true }).click();
    await expect(viewer.locator('pre')).not.toBeVisible();
    expect(mockApi.writes()).toEqual([]);
});

test.describe('mobile @mobile', () => {
    test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

    test('navigation opens, closes and selects a nested page without horizontal overflow', async ({ page }) => {
        await visit(page, 'dashboard');
        await expect(page.locator('.cs-sider')).toHaveCount(0);
        const open = button(page.locator('.cs-header'), label('common.expandPanel', '展开面板'));
        await open.click();
        const nav = page.getByRole('dialog').filter({ has: page.locator('.cs-sidebar-inner') });
        await expect(nav).toBeVisible();
        await assertDrawerFits(page, nav);
        // Semi v2's built-in close icon has no accessible name; scope it to this dialog.
        await nav.locator('button.semi-sidesheet-close').click();
        await expect(nav).toHaveCount(0);
        await open.click();
        await nav.getByRole('menuitem', { name: label('nav.assets', '资产管理'), exact: true }).click();
        await nav.getByRole('menuitem', { name: label('nav.assetLibrary', '资产库'), exact: true }).click();
        await expect(page).toHaveURL(/#\/asset-library$/);
        await expect(nav).toHaveCount(0);
        await ready(page);
        await assertNoOverflow(page);
        await open.click();
        await expect(nav.getByRole('menuitem', { name: label('nav.assetLibrary', '资产库'), exact: true })).toHaveClass(/semi-navigation-item-selected/);
        await nav.locator('button.semi-sidesheet-close').click();
    });

    for (const id of ['dashboard', 'deep-assessments', 'projects', 'vulnerabilities']) {
        test(`${id}: workspace stays within 390px`, async ({ page, mockApi }) => {
            await visit(page, id);
            await readsCompleted(mockApi, PAGE_READS[id]);
            await ready(page);
            await assertNoOverflow(page);
        });
    }

    test('chat: composer and send remain in viewport, multiline input does not send', async ({ page, mockApi }, testInfo) => {
        const main = await visit(page, 'chat');
        const input = main.getByRole('textbox', { name: label('chat.composerLabel', '对话内容'), exact: true });
        const send = button(main, label('chat.send', '发送'));
        await expect(main.locator('.chat-welcome')).toBeVisible();
        await expect(input).toBeInViewport({ ratio: 1 });
        await expect(send).toBeInViewport({ ratio: 1 });
        await input.fill('FAKE localhost '.repeat(8));
        await input.press('End');
        await input.press('Shift+Enter');
        await expect(input).toHaveValue(/\n$/);
        await expect(input).toHaveCSS('outline-style', 'none');
        await expect(send).toBeInViewport({ ratio: 1 });
        await assertNoOverflow(page);
        expect(mockApi.writes()).toEqual([]);
        await screenshot(page, testInfo, 'mobile-chat-composer');
    });

    test('vulnerability detail never exceeds 100vw and closes back to the list', async ({ page }, testInfo) => {
        const main = await visit(page, 'vulnerabilities');
        await main.getByText(VULNERABILITY.title, { exact: true }).click();
        const drawer = page.getByRole('dialog');
        await expect(drawer).toContainText(VULNERABILITY.description);
        await assertDrawerFits(page, drawer);
        await screenshot(page, testInfo, 'mobile-vulnerability-detail');
        await button(drawer, label('common.close', '关闭')).click();
        await expect(drawer).toHaveCount(0);
        await assertNoOverflow(page);
    });
});
