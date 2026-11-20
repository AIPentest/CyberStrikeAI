import { readFileSync } from 'node:fs';
import {
    test, expect, visit, ready, screenshot, label, conversationListFixture,
    READ_PERMISSIONS, UI_PERMISSIONS, PROJECT, FACT, VULNERABILITY, ASSESSMENT,
    CONVERSATION, STREAM_REPLY, APP_VERSION
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
    test('keyboard focus outlines the entire login input, including its icon', async ({ page }) => {
        await page.goto('/static/app/');
        const form = page.locator('.cs-login-form');
        const username = form.getByLabel(label('login.username', '用户名'), { exact: true });
        const password = form.getByLabel(label('login.password', '密码'), { exact: true });

        await expect(username).toBeVisible();
        for (const input of [username, password]) {
            await page.keyboard.press('Tab');
            await expect(input).toBeFocused();
            const wrapper = form.locator('.semi-input-wrapper:focus-within');
            await expect(input).toHaveCSS('outline-style', 'none');
            await expect(wrapper).toHaveCSS('outline-style', 'solid');
            await expect(wrapper).toHaveCSS('outline-width', '2px');
            await expect(wrapper).toHaveCSS('outline-offset', '-2px');
        }
    });

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

test('dashboard: feed cards align above full-width facts that stack on small screens', async ({ page, mockApi }, testInfo) => {
    const vulnerabilities = Array.from({ length: 8 }, (_, index) => ({
        ...VULNERABILITY, id: `fixture-vulnerability-${index}`
    }));
    const facts = [FACT, { ...FACT, id: 'fixture-long-fact', summary: `示例事实 ${'localhost/'.repeat(45)}` }];
    mockApi.respond('GET', '/api/vulnerabilities', () => ({ json: { vulnerabilities } }));
    mockApi.respond('GET', '/api/projects/dashboard-summary', () => ({
        json: { totals: { active_projects: 1, total_facts: facts.length }, projects: [PROJECT], recent_facts: facts }
    }));

    const main = await visit(page, 'dashboard');
    const card = name => main.locator('.cs-section-card').filter({ has: page.getByText(name, { exact: true }) });
    const recentVulnerabilities = await card('最近漏洞').boundingBox();
    const capabilities = await card('能力与运行态').boundingBox();
    const recentFacts = card('近期事实');
    const factItems = recentFacts.locator('.semi-list-item');
    await expect(factItems).toHaveCount(2);
    const factsBox = await recentFacts.boundingBox();
    const [left, right] = await Promise.all([factItems.nth(0).boundingBox(), factItems.nth(1).boundingBox()]);

    expect(recentVulnerabilities.y + recentVulnerabilities.height).toBeCloseTo(capabilities.y + capabilities.height, 0);
    expect(factsBox.y).toBeGreaterThan(capabilities.y + capabilities.height);
    expect(factsBox.x).toBeCloseTo(recentVulnerabilities.x, 0);
    expect(factsBox.x + factsBox.width).toBeCloseTo(capabilities.x + capabilities.width, 0);
    expect(left.y).toBeCloseTo(right.y, 0);
    expect(right.x).toBeGreaterThan(left.x + left.width);
    await recentFacts.scrollIntoViewIfNeeded();
    await screenshot(page, testInfo, 'dashboard-facts-desktop');

    await page.setViewportSize({ width: 390, height: 844 });
    const [first, second] = await Promise.all([factItems.nth(0).boundingBox(), factItems.nth(1).boundingBox()]);
    expect(first.x).toBeCloseTo(second.x, 0);
    expect(second.y).toBeGreaterThan(first.y + first.height);
    await assertNoOverflow(page);
    await factItems.first().getByRole('button', { name: '查看所属项目' }).click();
    await expect(page).toHaveURL(new RegExp(`#/projects\\?id=${PROJECT.id}$`));
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
    const version = sidebar.getByText(APP_VERSION, { exact: true });
    await expect(version).toBeVisible();
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
    await expect(version).toBeHidden();
    await expect.poll(async () => (await sidebar.boundingBox()).width).toBeLessThan(wide.width);
    await page.reload();
    await ready(page);
    await expect(page.locator('.cs-shell')).toHaveClass(/is-collapsed/);
    await button(sidebar, label('nav.expand', '展开菜单')).click();
    await expect(page.locator('.cs-shell')).not.toHaveClass(/is-collapsed/);
    await expect(version).toBeVisible();
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

for (const uploadFails of [false, true]) {
    test(`chat: attachment plus ${uploadFails ? 'preserves failed uploads until removed' : 'uploads and sends the selected file'}`, async ({ page, mockApi }) => {
        const message = 'FAKE：整理附件内容。';
        const id = mockApi.enableChatStream(message);
        let releaseUpload;
        const uploadReady = new Promise(resolve => { releaseUpload = resolve; });
        mockApi.respond('POST', '/api/chat-uploads', async () => {
            await uploadReady;
            return uploadFails
                ? { status: 503, json: { error: 'fixture 附件上传失败' } }
                : { json: { absolutePath: '/tmp/fixture-attachment.txt' } };
        });
        const main = await visit(page, 'chat');
        const composer = main.locator('.chat-composer');
        const input = composer.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
        await input.fill(message);
        const chooserPending = page.waitForEvent('filechooser');
        await button(composer, label('chat.attachmentsLabel', '附件')).click();
        const chooser = await chooserPending;
        await chooser.setFiles({ name: 'fixture-attachment.txt', mimeType: 'text/plain', buffer: Buffer.from('FAKE fixture attachment') });
        try {
            await expect(composer.locator('.chat-file-chip')).toContainText('上传中');
            await expect(button(composer, label('chat.send', '发送'))).toBeDisabled();
            await input.press('Enter');
            expect(mockApi.calls('POST', '/api/agent/stream')).toEqual([]);
        } finally {
            releaseUpload();
        }
        if (uploadFails) {
            await expect(composer.locator('.chat-file-chip')).toContainText('上传失败');
            await input.press('Enter');
            await expect(input).toHaveValue(message);
            await expect(page.getByText(label('chat.attachmentsUploadIncomplete', '存在上传失败的附件，请移除后再发送'), { exact: true })).toBeVisible();
            expect(mockApi.calls('POST', '/api/agent/stream')).toEqual([]);
            await composer.locator('.chat-file-chip .semi-tag-close').click();
            await expect(composer.locator('.chat-file-chip')).toHaveCount(0);
        } else {
            await expect(composer.locator('.chat-file-chip')).toContainText('已上传');
        }
        await input.press('Enter');
        await expect(page).toHaveURL(new RegExp(`#/chat\\?conversation=${id}$`));
        expect(mockApi.calls('POST', '/api/agent/stream')[0].body).toEqual({
            message,
            conversationId: '',
            ...(uploadFails ? {} : { attachments: [{ fileName: 'fixture-attachment.txt', mimeType: 'text/plain', serverPath: '/tmp/fixture-attachment.txt' }] })
        });
    });
}

for (const width of [1440, 1024, 390]) {
    test(`chat: header spacing and composer settings stay usable at ${width}px`, async ({ page, mockApi }, testInfo) => {
        await page.setViewportSize({ width, height: 900 });
        const longTitle = '用于验证对话页标题换行与操作区布局的长会话名称'.repeat(4);
        mockApi.respond('GET', `/api/conversations/${CONVERSATION.id}`, { json: { ...CONVERSATION, title: longTitle, projectId: PROJECT.id } });
        mockApi.respond('GET', '/api/config', { json: { ai: {
            default_channel: 'fixture-long-model',
            channels: { 'fixture-long-model': { name: '用于验证输入区长名称的模型通道'.repeat(3), model: 'fixture-model' } }
        } } });
        mockApi.respond('GET', '/api/agent-loop/tasks', { json: { tasks: [] } });
        mockApi.respond('GET', '/api/hitl/default-config', { json: { defaultMode: 'off' } });
        mockApi.respond('GET', `/api/hitl/config/${CONVERSATION.id}`, { json: { enabled: false, mode: 'off' } });

        for (const stored of [false, true]) {
            const main = await visit(page, stored ? `chat?conversation=${CONVERSATION.id}` : 'chat');
            const header = main.locator('.chat-header');
            const title = header.locator('.chat-header-title');
            const composer = main.locator('.chat-composer');
            const input = composer.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
            await expect(title).toHaveText(stored ? longTitle : label('chat.newChat', '新对话'));
            await expect(button(header, label('chat.sessionSettingsTitle'))).toHaveCount(0);
            await expect(button(header, label('chat.tokenUsage', 'Token 用量'))).toHaveCount(0);
            await expect(button(header, label('chat.refreshConversation'))).toHaveCount(stored ? 1 : 0);
            await expect(button(composer, label('chat.sessionSettingsTitle', '会话设置'))).toHaveCount(0);
            const [workspaceBox, titleBox, textBox, actionsBox] = await Promise.all(
                [main.locator('.chat-main'), title, header.locator('.chat-header-main'), header.locator('.chat-header-actions')]
                    .map(element => element.boundingBox())
            );
            expect(titleBox.x - workspaceBox.x).toBeGreaterThanOrEqual(workspaceBox.width <= 560 ? 12 : 24);
            expect(actionsBox.x >= textBox.x + textBox.width || actionsBox.y >= textBox.y + textBox.height).toBe(true);
            for (const action of await header.getByRole('button').all()) {
                await expect(action).toBeInViewport({ ratio: 1 });
            }
            await expect(button(composer, label('chat.send', '发送'))).toBeInViewport({ ratio: 1 });
            await assertNoOverflow(page);
            const card = composer.locator('.chat-composer-box');
            const context = card.locator('.chat-composer-context');
            await expect(context.getByRole('combobox')).toHaveCount(2);
            const project = context.locator('.chat-composer-project');
            await expect(project).toHaveText(stored ? PROJECT.name : label('chat.defaultWorkspace', '默认工作区'));
            const contextBox = await context.boundingBox();
            const projectBounds = await project.boundingBox();
            expect(projectBounds.x + projectBounds.width).toBeLessThan(contextBox.x + contextBox.width - 16);
            const box = await card.boundingBox();
            const inputBox = await input.boundingBox();
            expect(contextBox.y).toBeCloseTo(box.y + 1, 0);
            expect(contextBox.y + contextBox.height).toBeLessThan(inputBox.y);
            for (const control of await context.getByRole('combobox').all()) {
                await expect(control).toBeInViewport({ ratio: 1 });
                const bounds = await control.boundingBox();
                expect(bounds.y + bounds.height).toBeLessThanOrEqual(inputBox.y);
            }
            const attachment = button(composer, label('chat.attachmentsLabel', '附件'));
            const hitl = button(composer, label('chat.hitlTitle'));
            const model = button(composer, label('chat.modelSettingsAria'));
            const send = button(composer, label('chat.send', '发送'));
            const toolbarBoxes = [];
            for (const control of [attachment, hitl, model, send]) {
                await expect(control).toBeInViewport({ ratio: 1 });
                toolbarBoxes.push(await control.boundingBox());
            }
            for (let index = 1; index < toolbarBoxes.length; index++) {
                expect(toolbarBoxes[index].x).toBeGreaterThanOrEqual(toolbarBoxes[index - 1].x + toolbarBoxes[index - 1].width);
            }
            await screenshot(page, testInfo, `chat-header-${stored ? 'long-title' : 'new'}-${width}`);
            await model.click();
            const modelPopover = page.locator('.chat-model-popover');
            await expect(modelPopover.getByRole('slider', { name: label('chat.reasoningEffortLabel') })).toBeVisible();
            await expect(modelPopover).toBeInViewport({ ratio: 1 });
            const popoverBox = await modelPopover.boundingBox();
            expect(popoverBox.x).toBeGreaterThanOrEqual(0);
            expect(popoverBox.x + popoverBox.width).toBeLessThanOrEqual(width);
            await title.click();
            await expect(modelPopover).not.toBeVisible();

            await input.fill('保留会话草稿');
            await hitl.click();
            const settings = page.getByRole('dialog').filter({ hasText: label('chat.hitlTitle') });
            await expect(settings.getByRole('combobox', { name: label('chat.hitlModeLabel') })).toBeVisible();
            await settings.locator('button.semi-sidesheet-close').click();
            await expect(settings).not.toBeVisible();
            await expect(input).toHaveValue('保留会话草稿');
        }
        expect(mockApi.writes()).toEqual([]);
    });
}

for (const width of [1440, 390]) {
    for (const colorScheme of ['light', 'dark']) {
        test(`chat: compact reasoning picker at ${width}px in ${colorScheme} mode`, async ({ page, mockApi }, testInfo) => {
            await page.setViewportSize({ width, height: 900 });
            await page.emulateMedia({ colorScheme });
            mockApi.respond('GET', '/api/config', { json: { ai: {
                default_channel: 'fixture-model',
                channels: { 'fixture-model': { name: '示例模型', model: 'fixture-model' } }
            } } });
            const main = await visit(page, 'chat');
            const composer = main.locator('.chat-composer');
            const openModel = button(composer, label('chat.modelSettingsAria'));
            await expect(openModel).toContainText('示例模型');
            await openModel.click();
            const popover = page.locator('.chat-model-popover');
            const slider = popover.getByRole('slider', { name: label('chat.reasoningEffortLabel') });
            await expect(slider).toBeVisible();
            await expect(popover.getByRole('combobox')).toHaveCount(1);
            await slider.focus();
            await slider.press('Home');
            for (let index = 0; index < 3; index++) await slider.press('ArrowRight');
            await expect(slider).toHaveAttribute('aria-valuenow', '3');
            await expect(slider).toHaveAttribute('aria-valuetext', label('chat.reasoningEffortLabels.high'));
            await expect(popover.getByRole('status')).toHaveText(label('chat.reasoningEffortLabels.high'));
            await expect(composer.locator('.chat-composer-effort')).toHaveText(label('chat.reasoningEffortLabels.high'));
            await expect(popover).toBeInViewport({ ratio: 1 });
            const bounds = await popover.boundingBox();
            expect(bounds.height).toBeLessThanOrEqual(110);
            expect(bounds.width).toBeLessThanOrEqual(234);
            expect(bounds.x).toBeGreaterThanOrEqual(0);
            expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
            const overflow = await popover.evaluate(element => element.scrollWidth - element.clientWidth);
            expect(overflow).toBeLessThanOrEqual(1);
            await expect(button(popover, label('chat.reasoningReset'))).toBeInViewport({ ratio: 1 });
            await expect(popover.getByRole('combobox')).toBeInViewport({ ratio: 1 });
            await expect(popover.locator('.chat-reasoning-channel .semi-select-arrow')).toHaveCSS('opacity', '1');
            const picker = popover.locator('.chat-reasoning-picker');
            const track = picker.locator('.semi-slider-track');
            const rail = picker.locator('.semi-slider-rail');
            const marks = picker.locator('.semi-slider-dot');
            const strength = popover.getByRole('status');
            const normalColor = await strength.evaluate(element => getComputedStyle(element).color);
            await expect(picker).not.toHaveClass(/is-max/);
            await expect(track).toHaveCSS('background-image', 'none');
            await expect(track).toHaveCSS('animation-name', 'none');
            const [firstMark, lastMark, railBounds] = await Promise.all(
                [marks.first(), marks.last(), rail].map(element => element.boundingBox())
            );
            expect(firstMark.x - railBounds.x).toBeGreaterThanOrEqual(6);
            expect(railBounds.x + railBounds.width - lastMark.x - lastMark.width).toBeGreaterThanOrEqual(6);
            await strength.click();
            await screenshot(page, testInfo, `chat-reasoning-${width}-${colorScheme}`);
            await popover.screenshot({ path: testInfo.outputPath(`reasoning-panel-${width}-${colorScheme}.png`) });

            await marks.last().click();
            await expect(picker).toHaveClass(/is-max/);
            await expect(strength).toHaveText('ultra');
            await expect(slider).toHaveAttribute('aria-valuetext', 'ultra');
            await expect(composer.locator('.chat-composer-effort')).toHaveText('ultra');
            await expect(strength).not.toHaveCSS('color', normalColor);
            await expect(track).toHaveCSS('background-image', /linear-gradient/);
            await expect(track).toHaveCSS('animation-name', 'chat-reasoning-flow');
            await expect.poll(() => track.evaluate(element => element.getAnimations().some(animation =>
                animation.animationName === 'chat-reasoning-flow' && animation.playState === 'running' && animation.currentTime > 0
            ))).toBe(true);
            await expect.poll(() => track.evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('chat-reasoning-sparkle');
            expect(await track.evaluate(element => getComputedStyle(element, '::after').pointerEvents)).toBe('none');
            await strength.click();
            await screenshot(page, testInfo, `chat-reasoning-max-${width}-${colorScheme}`);
            await popover.screenshot({ path: testInfo.outputPath(`reasoning-max-panel-${width}-${colorScheme}.png`) });

            await page.emulateMedia({ reducedMotion: 'reduce' });
            await expect(track).toHaveCSS('animation-name', 'none');
            await expect.poll(() => track.evaluate(element => getComputedStyle(element, '::after').animationName)).toBe('none');
            await expect(track).toHaveCSS('background-image', /linear-gradient/);
            await page.emulateMedia({ reducedMotion: 'no-preference' });
            await expect(track).toHaveCSS('animation-name', 'chat-reasoning-flow');

            const mode = button(popover, label('chat.reasoningModeLabel'));
            await mode.click();
            await page.locator('.chat-reasoning-mode-menu').getByRole('menuitem', { name: label('chat.reasoningModeOff') }).click();
            await expect(picker).not.toHaveClass(/is-max/);
            await expect(slider).toHaveAttribute('aria-disabled', 'true');
            await expect(track).toHaveCSS('background-image', 'none');
            await expect(track).toHaveCSS('animation-name', 'none');
            await mode.click();
            await page.locator('.chat-reasoning-mode-menu').getByRole('menuitem', { name: label('chat.reasoningModeOn') }).click();
            await expect(picker).toHaveClass(/is-max/);
            await button(popover, label('chat.reasoningReset')).click();
            await expect(picker).not.toHaveClass(/is-max/);
            await expect(track).toHaveCSS('background-image', 'none');
            await slider.press('End');
            await slider.press('ArrowLeft');
            await expect(picker).not.toHaveClass(/is-max/);
            await expect(strength).toHaveText(label('chat.reasoningEffortLabels.xhigh'));
            await expect(track).toHaveCSS('animation-name', 'none');
            await slider.press('Escape');
            await expect(popover).not.toBeVisible();
            await expect(openModel).toBeFocused();
            expect(mockApi.writes()).toEqual([]);
        });
    }
}

test.describe('chat: composer settings permission boundary', () => {
    test.use({ appPermissions: READ_PERMISSIONS.filter(permission => !['config:read', 'hitl:read'].includes(permission)) });
    test('disables the approval shortcut without approval access', async ({ page, mockApi }) => {
        const main = await visit(page, 'chat');
        await expect(button(main, label('chat.hitlTitle'))).toBeDisabled();
        expect(mockApi.writes()).toEqual([]);
    });
});

for (const width of [1440, 390]) {
    test(`chat: mode descriptions are readable at ${width}px and modes stay selectable`, async ({ page, mockApi }, testInfo) => {
        await page.setViewportSize({ width, height: 700 });
        const main = await visit(page, 'chat');
        const select = main.locator('.chat-composer-mode');
        const list = page.getByRole('listbox');
        const modes = [
            ['agentModePiSingle', 'agentModePiSingleHint'],
            ['agentModeDeep', 'agentModeDeepHint'],
            ['agentModePlanExecuteLabel', 'agentModePlanExecuteHint'],
            ['agentModeSupervisorLabel', 'agentModeSupervisorHint']
        ];
        await expect(select).toHaveText(label('chat.agentModePiSingle'));
        await select.press('Enter');
        await expect(list.getByRole('option')).toHaveCount(modes.length);
        for (const [titleKey, hintKey] of modes) {
            const option = list.getByRole('option').filter({ hasText: label(`chat.${titleKey}`) });
            await expect(option.getByText(label(`chat.${hintKey}`), { exact: true })).toBeInViewport({ ratio: 1 });
        }
        await expect(list).toBeInViewport({ ratio: 1 });
        await screenshot(page, testInfo, `chat-mode-descriptions-${width}`);
        await select.press('ArrowDown');
        await select.press('Enter');
        await expect(select).toHaveText(label('chat.agentModeDeep'));
        await expect(list).toBeHidden();

        await select.click();
        const supervisor = list.getByText(label('chat.agentModeSupervisorHint'), { exact: true });
        await supervisor.click();
        await expect(select).toHaveText(label('chat.agentModeSupervisorLabel'));
        await expect(list).toBeHidden();
        expect(mockApi.writes()).toEqual([]);
    });
}

test('chat: create a project from the composer and select it for the current draft', async ({ page, mockApi }) => {
    const created = { ...PROJECT, id: 'fixture-new-chat-project', name: '对话中新建的项目' };
    mockApi.respond('POST', '/api/projects', { json: created });
    const main = await visit(page, 'chat');
    const composer = main.locator('.chat-composer');
    const input = composer.getByRole('textbox', { name: label('chat.composerLabel', '对话内容') });
    const projectSelect = composer.getByRole('combobox').filter({ hasText: label('chat.defaultWorkspace', '默认工作区') });
    const createOption = page.getByRole('option').filter({ hasText: `+ ${label('projects.newProject', '新建项目')}` });
    const dialog = page.getByRole('dialog', { name: label('projects.modalNewTitle', '新建项目') });

    await input.fill('项目工作记录草稿');
    await readsCompleted(mockApi, ['/api/projects']);
    await projectSelect.click();
    await expect(page.getByRole('option', { name: PROJECT.name })).toBeVisible();
    await createOption.click();
    await expect(dialog).toHaveClass(/semi-sidesheet-inner/);
    await button(dialog, label('common.cancel', '取消')).click();
    await expect(projectSelect).toBeVisible();
    expect(mockApi.writes()).toEqual([]);

    await projectSelect.click();
    await createOption.click();
    await dialog.getByLabel(label('projects.projectName', '项目名称'), { exact: true }).fill(created.name);
    await button(dialog, label('projects.createProject', '创建项目')).click();
    await expect(dialog).not.toBeVisible();
    await expect(composer.getByRole('combobox').filter({ hasText: created.name })).toBeVisible();
    await expect(input).toHaveValue('项目工作记录草稿');
    await expect(page).toHaveURL(/#\/chat$/);
    expect(mockApi.calls('POST', '/api/projects').map(call => call.body)).toEqual([
        { name: created.name, description: '' }
    ]);
    expect(mockApi.writes().map(call => call.path)).toEqual(['/api/projects']);
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
        await readsCompleted(mockApi, ['/api/projects', '/api/conversations']);
        await expect(button(main, label('chat.batchManage', '批量管理'))).toHaveCount(0);
        await expect(main.locator('.chat-composer-context').getByRole('combobox').filter({ hasText: label('chat.defaultWorkspace', '默认工作区') })).toBeDisabled();
        await expect(main.locator('.chat-composer-mode')).toBeDisabled();
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

test('chat: restore token counts and the error from a turn without a reply', async ({ page, mockApi }) => {
    const error = 'Pi worker exited without a result';
    const [user, assistant] = CONVERSATION.messages;
    mockApi.respond('GET', `/api/conversations/${CONVERSATION.id}`, {
        json: {
            ...CONVERSATION,
            messages: [user, {
                ...assistant,
                content: '',
                processDetails: [
                    {
                        eventType: 'agent_usage_summary',
                        message: 'Pi token usage summary',
                        data: { modelCalls: 1, promptTokens: 1000, completionTokens: 24, totalTokens: 1024 }
                    },
                    { eventType: 'error', message: error }
                ]
            }]
        }
    });
    const main = await visit(page, `chat?conversation=${CONVERSATION.id}`);
    const title = `${label('chat.piUsageSummaryTitle')} · 1,024`;
    const usage = main.locator('.chat-note').filter({ hasText: title });
    await expect(usage.getByText(title, { exact: true })).toBeVisible();
    for (const [key, count] of [
        ['piUsageModelCalls', '1'],
        ['piUsagePromptTokens', '1,000'],
        ['piUsageCompletionTokens', '24'],
        ['piUsageTotalTokens', '1,024']
    ]) {
        await expect(usage).toContainText(`${label(`chat.${key}`)}: ${count}`);
    }
    await expect(main.getByText(error, { exact: true })).toBeVisible();
});

test.describe('chat: batch selection', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'chat:delete'] });
    const conversations = Array.from({ length: 23 }, (_, index) => ({
        ...CONVERSATION,
        id: `fixture-batch-${index + 1}`,
        title: `批量示例 ${String(index + 1).padStart(2, '0')}`,
        projectId: ''
    }));
    const projectConversations = conversations.slice(0, 21).map((item, index) => ({
        ...item,
        id: 'fixture-project-chat-' + (index + 1),
        title: '项目示例 ' + String(index + 1).padStart(2, '0'),
        projectId: PROJECT.id
    }));

    test.beforeEach(async ({ mockApi }) => {
        let remaining = [...conversations, ...projectConversations];
        mockApi.respond('GET', '/api/conversations', (_, url) => ({ json: conversationListFixture(remaining, url) }));
        for (const item of conversations) {
            mockApi.respond('DELETE', `/api/conversations/${item.id}`, () => {
                remaining = remaining.filter(row => row.id !== item.id);
                return { json: { success: true } };
            });
        }
    });

    for (const width of [1440, 390]) {
        test(`select, clear and delete only the current page at ${width}px`, async ({ page, mockApi }, testInfo) => {
            await page.setViewportSize({ width, height: 1000 });
            const main = await visit(page, 'chat');
            if (width < 900) await button(main, label('chat.conversations', '会话')).click();
            const sidebar = page.locator('.chat-sidebar-inner:visible');
            await expect(sidebar.locator('.chat-conv-item')).toHaveCount(20);
            await button(sidebar, label('chat.batchManage', '批量管理')).click();
            const bar = sidebar.locator('.chat-batch-bar');
            const selectPage = bar.getByRole('checkbox', { name: label('chat.selectPage') });
            const rows = sidebar.locator('.chat-conv-check');
            const deleteButton = button(bar, label('chat.batchDelete', '批量删除'));
            await expect(selectPage).not.toBeChecked();
            await expect(deleteButton).toBeDisabled();

            await bar.getByText(label('chat.selectPage'), { exact: true }).click();
            await expect(sidebar.locator('.chat-conv-check input:checked')).toHaveCount(20);
            await expect(bar).toContainText('已选 20');
            await bar.getByText(label('chat.selectPage'), { exact: true }).click();
            await expect(sidebar.locator('.chat-conv-check input:checked')).toHaveCount(0);
            await expect(deleteButton).toBeDisabled();

            await rows.first().click();
            await expect(bar.locator('.chat-batch-select-all')).toHaveClass(/semi-checkbox-indeterminate/);
            await bar.getByText(label('chat.selectPage'), { exact: true }).click();
            await rows.last().click();
            await expect(bar).toContainText('已选 19');
            await expect(bar.locator('.chat-batch-select-all')).toHaveClass(/semi-checkbox-indeterminate/);
            await rows.last().click();
            await expect(selectPage).toBeChecked();
            await expect(bar.locator('.chat-batch-select-all')).not.toHaveClass(/semi-checkbox-indeterminate/);
            await selectPage.focus();
            await page.keyboard.press('Space');
            await expect(sidebar.locator('.chat-conv-check input:checked')).toHaveCount(0);
            await bar.getByText(label('chat.selectPage'), { exact: true }).click();
            await expect(deleteButton).toBeInViewport({ ratio: 1 });
            await expect(selectPage).toBeInViewport({ ratio: 1 });
            const sizes = await bar.evaluate(element => ({ width: element.clientWidth, scroll: element.scrollWidth }));
            expect(sizes.scroll).toBeLessThanOrEqual(sizes.width);
            await screenshot(page, testInfo, `chat-batch-selection-${width}`);
            expect(mockApi.writes()).toEqual([]);

            await deleteButton.click();
            await expect(bar).toHaveCount(0);
            await expect(sidebar.locator('.chat-conv-item')).toHaveCount(3);
            expect(mockApi.writes().map(call => `${call.method} ${call.path}`).sort()).toEqual(
                conversations.slice(0, 20).map(item => `DELETE /api/conversations/${item.id}`).sort()
            );
        });
    }

    test('keeps default workspace and project conversations in their own collapsible pages', async ({ page }) => {
        const main = await visit(page, 'chat');
        const sidebar = main.locator('.chat-sidebar-inner');
        const workspace = button(sidebar, label('chat.defaultWorkspace') + ' 23');
        const project = button(sidebar, PROJECT.name + ' 21');
        await expect(workspace).toHaveAttribute('aria-expanded', 'true');
        await expect(project).toHaveAttribute('aria-expanded', 'false');
        await expect(sidebar.getByText('批量示例 01', { exact: true })).toBeVisible();
        await project.click();
        await expect(workspace).toHaveAttribute('aria-expanded', 'false');
        await expect(project).toHaveAttribute('aria-expanded', 'true');
        await expect(sidebar.getByText('批量示例 01', { exact: true })).toHaveCount(0);
        await expect(sidebar.getByText('项目示例 01', { exact: true })).toBeVisible();
        await sidebar.getByRole('button', { name: 'Next', exact: true }).click();
        await expect(sidebar.getByText('项目示例 21', { exact: true })).toBeVisible();
        await expect(sidebar.getByText(PROJECT.id.slice(0, 8), { exact: true })).toHaveCount(0);
        await project.click();
        await expect(sidebar.locator('.chat-conv-item')).toHaveCount(0);
        await workspace.click();
        await expect(sidebar.getByText('批量示例 01', { exact: true })).toBeVisible();
    });

    test('resets selections on pagination, search, project filtering and cancel', async ({ page, mockApi }) => {
        await visit(page, 'chat');
        const sidebar = page.locator('.chat-sidebar-inner:visible');
        const manage = button(sidebar, label('chat.batchManage', '批量管理'));
        const bar = sidebar.locator('.chat-batch-bar');
        const selectPage = bar.getByRole('checkbox', { name: label('chat.selectPage') });
        await manage.click();
        await bar.getByText(label('chat.selectPage'), { exact: true }).click();
        await sidebar.getByRole('button', { name: 'Next', exact: true }).click();
        await expect(bar).toHaveCount(0);
        await expect(sidebar.locator('.chat-conv-item')).toHaveCount(3);
        await manage.click();
        await expect(bar).toContainText('已选 0');
        await bar.getByText(label('chat.selectPage'), { exact: true }).click();
        await expect(bar).toContainText('已选 3');

        await sidebar.getByRole('textbox', { name: label('chat.searchHistory', '搜索历史对话') }).fill('批量示例 2');
        await expect(bar).toHaveCount(0);
        await expect(sidebar.locator('.chat-conv-item')).toHaveCount(4);
        await manage.click();
        await expect(bar).toContainText('已选 0');
        await bar.getByText(label('chat.selectPage'), { exact: true }).click();
        await expect(bar).toContainText('已选 4');

        await sidebar.getByRole('textbox', { name: label('chat.searchHistory') }).fill('项目示例 2');
        await expect(button(sidebar, PROJECT.name + ' 2')).toBeVisible();
        await sidebar.getByRole('combobox').filter({ hasText: label('chat.filterAllProjects', '全部项目') }).click();
        await page.getByRole('option').filter({ hasText: PROJECT.name }).click();
        await expect(bar).toHaveCount(0);
        await expect(sidebar.locator('.chat-conv-item')).toHaveCount(2);
        await manage.click();
        await expect(bar).toContainText('已选 0');
        await bar.getByText(label('chat.selectPage'), { exact: true }).click();
        await expect(bar).toContainText('已选 2');
        await button(sidebar, label('common.cancel', '取消')).click();
        await manage.click();
        await expect(selectPage).not.toBeChecked();
        await expect(button(bar, label('chat.batchDelete', '批量删除'))).toBeDisabled();

        await sidebar.getByRole('textbox', { name: label('chat.searchHistory', '搜索历史对话') }).fill('没有匹配的会话');
        await expect(sidebar.locator('.chat-conv-item')).toHaveCount(0);
        await expect(manage).toHaveCount(0);
        await expect(bar).toHaveCount(0);
        expect(mockApi.writes()).toEqual([]);
    });
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
    await expect(composerBox).toHaveCSS('border-radius', '20px');

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

test('deep assessment: detail shows the channel name and counts active time until paused', async ({ page, mockApi }) => {
    const now = Date.now();
    await page.clock.setFixedTime(now);
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    const task = {
        ...ASSESSMENT, status: 'running', model: 'qwen-max', active_seconds: 12,
        run_started_at: new Date(now - 5000).toISOString()
    };
    const model_channel_name = '自定义模型渠道';
    mockApi.respond('GET', path, { json: { task, model_channel_name } });
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });

    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}`);
    const main = await ready(page);
    const elapsed = main.locator('.cs-stat-card').filter({ hasText: '执行中' }).locator('.cs-stat-detail');
    await expect(main.getByText(`模型 ${model_channel_name}`, { exact: true })).toBeVisible();
    await expect(elapsed).toHaveText('17 秒');
    const reads = mockApi.calls('GET', path).length;
    await page.clock.setFixedTime(now + 3000);
    await expect(elapsed).toHaveText('20 秒');
    expect(mockApi.calls('GET', path)).toHaveLength(reads);

    mockApi.respond('GET', path, { json: {
        task: { ...task, status: 'paused', active_seconds: 27, run_started_at: null },
        model_channel_name
    } });
    await button(main, '刷新').click();
    const pausedElapsed = main.locator('.cs-stat-card').filter({ hasText: '已暂停' }).locator('.cs-stat-detail');
    await expect(pausedElapsed).toHaveText('27 秒');
    await page.clock.setFixedTime(now + 6000);
    await button(main, '刷新').click();
    await expect(pausedElapsed).toHaveText('27 秒');
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: auto-generated names do not repeat the objective in the header', async ({ page, mockApi }) => {
    for (const objective of [ASSESSMENT.objective, '示例目标与预期结果。'.repeat(12)]) {
        const id = `fixture-title-${objective.length}`;
        const path = `/api/deep-assessments/${id}`;
        const name = objective.length > 80 ? `${objective.slice(0, 80)}…` : objective;
        mockApi.respond('GET', path, { json: { task: { ...ASSESSMENT, id, name, objective } } });
        mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
        await page.goto(`/static/app/#/deep-assessments?id=${id}`);
        const main = await ready(page);
        await expect(main.locator('.cs-page-title')).toHaveText(name);
        await expect(main.locator('.cs-page-header .cs-page-desc')).toHaveCount(0);
        await expect(main.locator('.assessment-pre').filter({ hasText: objective })).toBeVisible();
    }
});

test('deep assessment: graph label color follows the theme with the graph open', async ({ page, mockApi }, testInfo) => {
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    const title = '图谱示例';
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
    mockApi.respond('GET', `${path}/graph`, { json: {
        nodes: [{ id: 'goal', kind: 'goal', title }, { id: 'note', kind: 'observation', title: '示例观察记录' }],
        edges: [{ source: 'goal', target: 'note' }]
    } });
    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}&tab=graph`);
    const main = await ready(page);
    const nodeLabel = main.getByRole('group', { name: '探索图' }).getByRole('button', { name: title });
    await expect(nodeLabel).toHaveCSS('color', 'rgb(25, 36, 56)');
    const reads = mockApi.calls('GET', `${path}/graph`).length;

    await button(page.locator('.cs-header'), label('theme.system', '跟随系统')).click();
    await page.getByRole('menuitem', { name: label('theme.dark', '暗色'), exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(nodeLabel).toHaveCSS('color', 'rgb(232, 237, 246)');
    expect(mockApi.calls('GET', `${path}/graph`)).toHaveLength(reads);
    await screenshot(page, testInfo, 'assessment-graph-dark');
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: dense graph keeps labels readable and locates a cross-page node', async ({ page, mockApi }, testInfo) => {
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    const goal = { id: 'goal', kind: 'goal', title: '示例测评目标' };
    const steps = Array.from({ length: 20 }, (_, i) => ({
        id: `step:${i}`, step_id: String(i), kind: 'step', title: `探索意图 ${i + 1}`
    }));
    const facts = Array.from({ length: 60 }, (_, i) => ({
        id: `fact:${i}`, step_id: String(i % steps.length), kind: 'fact',
        title: `已观察到的第 ${i + 1} 条事实记录及其关联说明`
    }));
    const receipts = Array.from({ length: 80 }, (_, i) => ({
        id: `receipt:${i}`, step_id: String(i % steps.length), kind: 'receipt', boundary: true,
        title: `跨页引用中的回执说明和完整执行记录${'示例'.repeat(18)} 唯一编号 ${i + 1}`, body: '合成回执原文'
    }));
    const edges = [
        ...steps.map(step => ({ source: goal.id, target: step.id, relation: 'plans' })),
        ...facts.map(fact => ({ source: `step:${fact.step_id}`, target: fact.id, relation: 'produces' })),
        ...receipts.map(receipt => ({ source: `step:${receipt.step_id}`, target: receipt.id, relation: 'executes' })),
        ...facts.map((fact, i) => ({ source: receipts[i].id, target: fact.id, relation: 'supports' })),
        ...facts.map((fact, i) => ({ source: fact.id, target: steps[(i + 1) % steps.length].id, relation: 'informs' }))
    ];
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
    mockApi.respond('GET', `${path}/graph`, { json: {
        nodes: [goal, ...steps, ...facts, ...receipts], edges,
        primary_count: 80, total: 160, boundary_total: 80, has_more: true
    } });
    const receipt = receipts.at(-1);
    mockApi.respond('GET', `${path}/graph/node`, { json: {
        node: receipt,
        relations: [{ node: steps.at(-1), relation: 'executes', direction: 'upstream' }]
    } });
    await page.addInitScript(() => localStorage.setItem('cyberstrike-theme', 'dark'));
    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}&tab=graph`);
    const main = await ready(page);
    const graph = main.getByRole('group', { name: '探索图' });
    const labels = graph.getByRole('button');
    await expect(graph.getByRole('button', { name: goal.title, exact: true })).toBeVisible();
    expect(await labels.count()).toBeGreaterThan(5);
    expect(await labels.count()).toBeLessThan(25);
    const bounds = await labels.evaluateAll(items => items.map(item => {
        const { left, top, right, bottom } = item.getBoundingClientRect();
        return { left, top, right, bottom, size: getComputedStyle(item).fontSize };
    }));
    for (const [i, box] of bounds.entries()) {
        expect(box.size).toBe('14px');
        for (const other of bounds.slice(i + 1)) {
            expect(box.left < other.right && box.right > other.left && box.top < other.bottom && box.bottom > other.top).toBe(false);
        }
    }
    await expect(main.locator('.assessment-node-panel')).toHaveCount(0);
    await expect(main.getByText('主干 80 / 280 条连线 · 收起 80 个明细节点')).toBeVisible();
    const goalLabel = graph.getByRole('button', { name: goal.title, exact: true });
    const firstIntent = graph.getByRole('button', { name: /^探索意图/ }).first();
    expect((await goalLabel.boundingBox()).x).toBeLessThan((await firstIntent.boundingBox()).x);
    const legend = main.getByRole('group', { name: '连线图例' });
    await expect(legend.getByText('规划与依赖')).toBeVisible();
    await expect(legend.getByText('产出与证据')).toBeVisible();
    await graph.screenshot({ path: testInfo.outputPath('assessment-graph-dense-overview.png') });
    const reads = mockApi.calls('GET', `${path}/graph`).length;
    await button(main, '显示全部节点与连线').click();
    await expect(main.getByText('全部 280 条连线')).toBeVisible();
    await button(main, '仅看主干').click();
    expect(mockApi.calls('GET', `${path}/graph`)).toHaveLength(reads);

    await main.getByText('搜索并定位节点', { exact: true }).click();
    await main.getByRole('combobox').getByRole('textbox').fill('唯一编号 80');
    await page.getByRole('option', { name: /跨页引用/ }).click();
    await expect(main.getByText('合成回执原文')).toBeVisible();
    const selected = graph.getByRole('button', { name: receipt.title, pressed: true });
    await expect(selected).toBeVisible();
    await expect(graph.getByRole('button', { name: goal.title, exact: true })).toHaveCount(0);
    expect(new URLSearchParams(mockApi.calls('GET', `${path}/graph/node`).at(-1).query).get('node_id')).toBe(receipt.id);
    const positionInCanvas = async () => {
        const [card, canvas] = await Promise.all([selected.boundingBox(), graph.boundingBox()]);
        return card && canvas ? { x: card.x - canvas.x, y: card.y - canvas.y } : null;
    };
    const position = await positionInCanvas();
    await main.locator('.cs-section-card').getByRole('button', { name: '刷新', exact: true }).click();
    await expect.poll(() => mockApi.calls('GET', `${path}/graph`).length).toBe(reads + 1);
    await expect.poll(async () => {
        const restored = await positionInCanvas();
        return restored ? Math.hypot(restored.x - position.x, restored.y - position.y) : Infinity;
    }).toBeLessThan(1);
    await screenshot(page, testInfo, 'assessment-graph-dense-selection');

    await button(main, '关闭详情').click();
    await expect(main.locator('.assessment-node-panel')).toHaveCount(0);
    await page.setViewportSize({ width: 390, height: 844 });
    await button(main, '适应画布').click();
    await expect(graph.getByRole('button', { name: goal.title, exact: true })).toBeVisible();
    await assertNoOverflow(page);
    await graph.screenshot({ path: testInfo.outputPath('assessment-graph-dense-mobile.png') });
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: graph brief reflects saved intents and the current task summary', async ({ page, mockApi }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    const first = { id: 'notes', title: '整理已有记录', status: 'completed' };
    const second = { id: 'summary', title: '归并阶段结果', status: 'pending', depends_on: [first.id] };
    const task = { ...ASSESSMENT, steps: [first, second], summary: '已整理首批记录，阶段结果待归并。' };
    mockApi.respond('GET', path, { json: { task } });
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
    const node = { id: `step:${second.id}`, kind: 'step', title: second.title, body: '已保存的意图详情' };
    mockApi.respond('GET', `${path}/graph`, { json: { nodes: [node], edges: [] } });
    mockApi.respond('GET', `${path}/graph/node`, { json: { node } });

    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}&tab=graph`);
    const main = await ready(page);
    const brief = main.locator('.assessment-graph-brief');
    await expect(brief.getByText('2 个意图 · 1 个已完成')).toBeVisible();
    await brief.locator('summary').click();
    await expect(brief.getByText(task.objective)).toBeVisible();
    await expect(brief.getByText(`上游依赖 · ${first.title}`)).toBeVisible();
    await expect(brief.getByText(task.summary)).toBeVisible();
    await button(brief, second.title).click();
    await expect(main.getByText(node.body)).toBeVisible();

    const updated = { ...task, steps: [first, { ...second, status: 'completed' }], summary: '已归并本阶段结果。' };
    mockApi.respond('GET', path, { json: { task: updated } });
    await main.getByRole('button', { name: 'refresh 刷新', exact: true }).click();
    await expect(brief.getByText('2 个意图 · 2 个已完成')).toBeVisible();
    await expect(brief.getByText(updated.summary)).toBeVisible();
    await assertNoOverflow(page);
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: graph polls saved progress without moving the selection or viewport', async ({ page, mockApi }) => {
    await page.clock.install();
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    const first = { id: 'existing', title: '已有意图', status: 'completed' };
    const added = { id: 'new', title: '新保存的意图', status: 'pending' };
    const goal = { id: 'goal', kind: 'goal', title: '图谱目标' };
    const stepNode = step => ({ id: `step:${step.id}`, kind: 'step', title: step.title, state: step.status });
    const node = { ...stepNode(first), body: '当前阅读中的意图详情' };
    const connect = step => ({ source: goal.id, target: stepNode(step).id, relation: 'plans' });
    const task = { ...ASSESSMENT, steps: [first], summary: '已有阶段总结' };

    mockApi.respond('GET', path, { json: { task } });
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
    mockApi.respond('GET', `${path}/graph`, { json: { nodes: [goal, node], edges: [connect(first)], primary_count: 1, total: 1 } });
    mockApi.respond('GET', `${path}/graph/node`, { json: { node } });
    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}&tab=graph`);
    const main = await ready(page);
    const graph = main.getByRole('group', { name: '探索图' });
    const brief = main.locator('.assessment-graph-brief');
    await brief.locator('summary').click();
    await button(brief, first.title).click();
    await expect(main.getByText(node.body)).toBeVisible();
    const selected = graph.getByRole('button', { name: first.title, pressed: true });
    const root = graph.getByRole('button', { name: goal.title });
    await expect(selected).toBeVisible();
    await main.getByRole('button', { name: '放大图谱' }).click();
    await graph.scrollIntoViewIfNeeded();
    const bounds = await graph.boundingBox();
    await page.mouse.move(bounds.x + bounds.width * 0.65, bounds.y + 100);
    await page.mouse.down();
    await page.mouse.move(bounds.x + bounds.width * 0.65 + 48, bounds.y + 132, { steps: 4 });
    await page.mouse.up();
    await page.clock.runFor(50);
    const canvas = graph.locator('canvas').first();
    await canvas.evaluate(element => { element.dataset.viewportCheck = 'kept'; });
    const coordinates = async () => {
        const box = await graph.boundingBox();
        const markers = await Promise.all([selected.boundingBox(), root.boundingBox()]);
        return markers.flatMap(marker => marker ? [marker.x - box.x, marker.y - box.y] : []);
    };
    const before = await coordinates();
    expect(before).toHaveLength(4);

    const updated = { ...task, steps: [first, added], summary: '自动读到的新阶段总结' };
    mockApi.respond('GET', path, { json: { task: updated } });
    mockApi.respond('GET', `${path}/graph`, { json: {
        nodes: [goal, stepNode(added), node],
        edges: [connect(added), connect(first)],
        primary_count: 2, total: 2
    } });

    await page.clock.fastForward(10000);
    await expect(brief.getByText(updated.summary)).toBeVisible();
    await expect(button(brief, added.title)).toBeVisible();
    await expect(main.getByText(/当前页节点 2 \/ 2/)).toBeVisible();
    await expect(canvas).toHaveAttribute('data-viewport-check', 'kept');
    await expect(main.getByText(node.body)).toBeVisible();
    await expect(selected).toBeVisible();
    const after = await coordinates();
    expect(after).toHaveLength(before.length);
    after.forEach((value, i) => expect(value).toBeCloseTo(before[i], 0));
    await button(main, '关闭详情').click();
    await expect(graph.getByRole('button', { name: added.title })).toBeVisible();

    await main.getByRole('switch', { name: '自动刷新探索图' }).click();
    const graphReads = mockApi.calls('GET', `${path}/graph`).length;
    const taskReads = mockApi.calls('GET', path).length;
    await page.clock.fastForward(20000);
    expect(mockApi.calls('GET', `${path}/graph`)).toHaveLength(graphReads);
    expect(mockApi.calls('GET', path)).toHaveLength(taskReads);
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: graph switches views and resets pagination on a narrow screen', async ({ page, mockApi }) => {
    const apiPath = `/api/deep-assessments/${ASSESSMENT.id}`;
    mockApi.respond('GET', `${apiPath}/events`, { json: { events: [] } });
    mockApi.respond('GET', `${apiPath}/graph`, { json: {
        nodes: [{ id: 'goal', kind: 'goal', title: '示例目标' }], edges: [], has_more: true
    } });
    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}&tab=graph`);
    const main = await ready(page);
    const filter = main.getByRole('radiogroup', { name: '图谱视图' });
    const summary = filter.getByRole('radio', { name: '主链路' });
    const full = filter.getByRole('radio', { name: '执行记录' });
    const lastGraphQuery = () => new URLSearchParams(mockApi.calls('GET', `${apiPath}/graph`).at(-1)?.query);

    await expect(summary).toBeChecked();
    await expect(filter).toBeInViewport({ ratio: 1 });
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(main.getByText('探索图', { exact: true }).and(main.locator('.cs-section-title'))).toBeInViewport({ ratio: 1 });
    await expect(filter).toBeInViewport({ ratio: 1 });
    await button(main, '下一页').click();
    await expect.poll(() => lastGraphQuery().get('offset')).toBe('80');

    await filter.getByText('执行记录', { exact: true }).click();
    await expect(full).toBeChecked();
    await expect.poll(() => [lastGraphQuery().get('view'), lastGraphQuery().get('offset')]).toEqual(['full', '0']);
    await filter.getByText('主链路', { exact: true }).click();
    await expect(summary).toBeChecked();
    await expect.poll(() => lastGraphQuery().get('view')).toBe('summary');
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: focused text fields use a complete border without a second outline', async ({ page }) => {
    const main = await visit(page, 'deep-assessments');
    await button(main, label('deepAssessments.create', '新建测评')).click();
    const drawer = page.getByRole('dialog', { name: label('deepAssessments.create', '新建测评') });
    const name = drawer.getByRole('textbox', { name: label('deepAssessments.name', '测评名称'), exact: true });
    const objective = drawer.getByRole('textbox', { name: /目标/ });
    const concurrency = drawer.getByRole('spinbutton', { name: label('deepAssessments.concurrency', '并行步骤') });
    const focusBorder = 'rgb(50, 102, 227)';

    await name.click();
    await expect(name).toBeFocused();
    await expect(name).toHaveCSS('outline-style', 'none');
    await expect(drawer.locator('.semi-input-wrapper:focus-within')).toHaveCSS('border-color', focusBorder);

    await name.press('Tab');
    await expect(objective).toBeFocused();
    await expect(objective).toHaveCSS('outline-style', 'none');
    await expect(drawer.locator('.semi-input-textarea-wrapper:focus-within')).toHaveCSS('border-color', focusBorder);

    await concurrency.click();
    await expect(concurrency).toBeFocused();
    await expect(concurrency).toHaveCSS('outline-style', 'none');
    await expect(drawer.locator('.semi-input-wrapper:focus-within')).toHaveCSS('border-color', focusBorder);
});

test('deep assessment: new-draft drawer retries options, validates and saves without execution', async ({ page, mockApi }, testInfo) => {
    mockApi.fail('GET', '/api/deep-assessments/options', '示例选项加载失败');
    const main = await visit(page, 'deep-assessments');
    const create = button(main, label('deepAssessments.create', '新建测评'));
    await create.click();
    const drawer = page.getByRole('dialog', { name: label('deepAssessments.create', '新建测评') });
    const save = button(drawer, label('deepAssessments.save', '保存草稿'));
    await expect(drawer).toHaveClass(/semi-sidesheet-inner/);
    await expect.poll(async () => {
        const { x, width } = await drawer.boundingBox();
        return Math.round(x + width);
    }).toBe(page.viewportSize().width);
    expect((await drawer.boundingBox()).x).toBeGreaterThan(0);
    await expect(drawer).toContainText('示例选项加载失败');
    mockApi.respond('GET', '/api/deep-assessments/options', { json: { models: [], roles: ['示例角色', '默认'], projects: [PROJECT] } });
    await button(drawer, '重试').click();
    await expect(drawer.locator('.cs-error-banner')).toHaveCount(0);
    await save.click();
    await expect(drawer.getByText('请填写测评目标', { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await screenshot(page, testInfo, 'assessment-validation');
    await button(drawer.locator('.semi-sidesheet-footer'), label('deepAssessments.close', '关闭')).click();
    await expect(drawer).toHaveCount(0);
    await expect(button(main, ASSESSMENT.name)).toBeVisible();

    await page.setViewportSize({ width: 390, height: 650 });
    await create.click();
    await expect(save).toBeInViewport({ ratio: 1 });
    await drawer.locator('.assessment-form-note').scrollIntoViewIfNeeded();
    await expect(save).toBeInViewport({ ratio: 1 });

    mockApi.respond('POST', '/api/deep-assessments', { json: { task: ASSESSMENT } });
    mockApi.respond('GET', `/api/deep-assessments/${ASSESSMENT.id}/events`, { json: { events: [] } });
    await drawer.getByRole('textbox', { name: label('deepAssessments.name', '测评名称'), exact: true }).fill(ASSESSMENT.name);
    await drawer.getByRole('textbox', { name: /目标/ }).fill(ASSESSMENT.objective);
    const role = drawer.getByRole('combobox', { name: label('deepAssessments.role', '角色') });
    await expect(role).toHaveText('默认');
    await role.click();
    const roleOptions = page.getByRole('listbox').getByRole('option');
    await expect(roleOptions).toHaveText(['默认', '示例角色']);
    await roleOptions.getByText('示例角色', { exact: true }).click();
    await expect(role).toHaveText('示例角色');
    await save.click();
    await expect(page).toHaveURL(/id=fixture-assessment&tab=overview$/);
    await expect(drawer).toHaveCount(0);
    await expect(main.locator('.cs-page-title')).toHaveText(ASSESSMENT.name);
    await expect(main.locator('.cs-page-desc')).toHaveText(ASSESSMENT.objective);
    const writes = mockApi.writes();
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({
        method: 'POST', path: '/api/deep-assessments',
        body: { name: ASSESSMENT.name, objective: ASSESSMENT.objective, role: '示例角色' }
    });
});

test('deep assessment: create and select a new project without losing the draft', async ({ page, mockApi }) => {
    const created = { ...PROJECT, id: 'fixture-new-assessment-project', name: '测评中新建的项目' };
    const main = await visit(page, 'deep-assessments');
    const drawer = page.getByRole('dialog', { name: label('deepAssessments.create', '新建测评') });
    const projectDrawer = page.getByRole('dialog', { name: label('projects.modalNewTitle', '新建项目') });
    const createProject = button(drawer, label('projects.newProject', '新建项目'));
    const saveAssessment = button(drawer, label('deepAssessments.save', '保存草稿'));
    const objective = drawer.getByRole('textbox', { name: /目标/ });
    const select = drawer.getByRole('combobox', { name: label('deepAssessments.project', '关联项目') });

    await button(main, label('deepAssessments.create', '新建测评')).click();
    await objective.fill(ASSESSMENT.objective);
    await select.click();
    await page.getByRole('option').filter({ hasText: PROJECT.name }).click();
    await createProject.click();
    await expect(projectDrawer).toBeVisible();
    await expect(saveAssessment).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(projectDrawer).not.toBeVisible();
    await expect(createProject).toBeFocused();

    await createProject.click();
    await expect(projectDrawer).toBeVisible();
    await page.locator('.semi-sidesheet-mask').last().click({ position: { x: 32, y: 300 } });
    await expect(projectDrawer).not.toBeVisible();
    await expect(createProject).toBeFocused();
    await expect(saveAssessment).toBeEnabled();
    await expect(select).toContainText(PROJECT.name);
    await expect(objective).toHaveValue(ASSESSMENT.objective);
    expect(mockApi.writes()).toEqual([]);

    await page.setViewportSize({ width: 390, height: 650 });
    await createProject.click();
    await assertDrawerFits(page, projectDrawer);
    const projectName = projectDrawer.getByLabel(label('projects.projectName', '项目名称'), { exact: true });
    const submitProject = button(projectDrawer, label('projects.createProject', '创建项目'));
    mockApi.fail('POST', '/api/projects', '示例项目创建失败');
    await projectName.fill(created.name);
    await submitProject.click();
    await expect(projectDrawer).toContainText('示例项目创建失败');
    await expect(projectName).toHaveValue(created.name);
    mockApi.respond('POST', '/api/projects', { json: created });
    await submitProject.click();
    await expect(projectDrawer).not.toBeVisible();
    await expect(select).toContainText(created.name);
    await expect(objective).toHaveValue(ASSESSMENT.objective);
    expect(mockApi.calls('POST', '/api/deep-assessments')).toEqual([]);

    mockApi.respond('POST', '/api/deep-assessments', { json: { task: ASSESSMENT } });
    mockApi.respond('GET', `/api/deep-assessments/${ASSESSMENT.id}/events`, { json: { events: [] } });
    await saveAssessment.click();
    await expect(page).toHaveURL(/id=fixture-assessment&tab=overview$/);
    const writes = mockApi.writes();
    expect(writes.map(call => call.path)).toEqual(['/api/projects', '/api/projects', '/api/deep-assessments']);
    expect(writes[1].body).toEqual({ name: created.name, description: '' });
    expect(writes[2].body).toMatchObject({ project_id: created.id, objective: ASSESSMENT.objective, role: '' });
});

test.describe('deep assessment: project read permission only', () => {
    test.use({ appPermissions: UI_PERMISSIONS.filter(permission => permission !== 'project:write') });
    test('can select an existing project without a create action', async ({ page, mockApi }) => {
        const main = await visit(page, 'deep-assessments');
        await button(main, label('deepAssessments.create', '新建测评')).click();
        const drawer = page.getByRole('dialog', { name: label('deepAssessments.create', '新建测评') });
        const select = drawer.getByRole('combobox', { name: label('deepAssessments.project', '关联项目') });
        await select.click();
        await page.getByRole('option').filter({ hasText: PROJECT.name }).click();
        await expect(select).toContainText(PROJECT.name);
        await expect(button(drawer, label('projects.newProject', '新建项目'))).toHaveCount(0);
        expect(mockApi.writes()).toEqual([]);
    });
});

test('deep assessment: guidance and asset selection open as drawers', async ({ page, mockApi }) => {
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
    mockApi.respond('GET', `${path}/assets`, { json: { assets: [] } });
    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}`);
    const main = await ready(page);

    await button(main, '补充方向').click();
    const guidance = page.getByRole('dialog', { name: '补充线索或调整方向' });
    await expect(guidance).toHaveClass(/semi-sidesheet-inner/);
    await button(guidance, '取消').click();
    await expect(guidance).toHaveCount(0);

    await main.getByRole('tab', { name: '攻击面图', exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`#/deep-assessments\\?id=${ASSESSMENT.id}&tab=assets$`));
    await expect(main.locator('.cs-section-card').getByText('攻击面图', { exact: true })).toBeVisible();
    await expect(main.getByText('攻击面图节点', { exact: true })).toBeVisible();
    await button(main, '添加资产').click();
    const assets = page.getByRole('dialog', { name: '关联共享资产' });
    await expect(assets).toHaveClass(/semi-sidesheet-inner/);
    await expect(button(assets, '关联')).toBeDisabled();
    await button(assets, '取消').click();
    await expect(assets).toHaveCount(0);
    expect(mockApi.writes()).toEqual([]);
});

test('deep assessment: register and edit an attack surface clue with a fact reference', async ({ page, mockApi }) => {
    const path = `/api/deep-assessments/${ASSESSMENT.id}`;
    const endpoint = `${path}/surface-items`;
    const factId = 'fact:fixture-surface';
    const itemId = 'fixture-surface-item';
    let item = null;
    mockApi.respond('GET', `${path}/assets`, { json: { assets: [{ id: 'fixture-asset', host: 'fixture.example', port: 443, protocol: 'https' }] } });
    mockApi.respond('GET', `${path}/events`, { json: { events: [] } });
    mockApi.respond('GET', endpoint, () => ({ json: { items: item ? [item] : [], has_more: false } }));
    mockApi.respond('POST', endpoint, ({ body }) => {
        item = { ...body, id: itemId, revision: 1 };
        return { json: item };
    });
    mockApi.respond('PUT', `${endpoint}/${itemId}`, ({ body }) => {
        item = { ...body, id: itemId, revision: 2 };
        return { json: item };
    });
    mockApi.respond('GET', `${path}/evidence-path`, { json: { title: '示例配置事实', nodes: [], edges: [] } });

    await page.goto(`/static/app/#/deep-assessments?id=${ASSESSMENT.id}&tab=assets`);
    const main = await ready(page);
    await expect(main.getByRole('tab', { name: '探索图', exact: true })).toBeVisible();
    await expect(main.getByRole('tab', { name: '攻击面图', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(main.getByText('fixture.example', { exact: true })).toBeVisible();
    await expect(main.getByText('暂无攻击面线索。')).toBeVisible();
    await button(main, '新增线索').click();
    const create = page.getByRole('dialog', { name: '新增线索' });
    const saveLabel = label('common.save', '保存');
    await button(create, saveLabel).click();
    await expect(create.getByText('请填写脱敏摘要', { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await create.getByRole('textbox', { name: '脱敏摘要' }).fill('示例配置发现云访问密钥');
    await create.getByRole('textbox', { name: '发现位置' }).fill('示例配置文件');
    await create.getByRole('textbox', { name: '关联事实 ID（选填）' }).fill(factId);
    await button(create, saveLabel).click();
    await expect(create).toHaveCount(0);
    const row = main.getByRole('row').filter({ hasText: '示例配置发现云访问密钥' });
    await expect(row).toContainText('AK/SK');
    expect(mockApi.calls('POST', endpoint)[0].body).toEqual({
        kind: 'cloud_access_key', summary: '示例配置发现云访问密钥', location: '示例配置文件', fact_id: factId
    });

    await button(row, '编辑线索').click();
    const edit = page.getByRole('dialog', { name: '编辑线索' });
    await edit.getByRole('textbox', { name: '脱敏摘要' }).fill('示例配置发现账号凭据');
    const credential = edit.getByRole('radio', { name: '账号凭据', exact: true });
    await credential.check();
    await expect(credential).toBeChecked();
    await button(edit, saveLabel).click();
    await expect(edit).toHaveCount(0);
    expect(mockApi.calls('PUT', `${endpoint}/${itemId}`)[0].body).toMatchObject({ kind: 'credential', revision: 1 });

    await main.getByRole('link', { name: '查看证据路径' }).click();
    await expect(main.getByText('示例配置事实', { exact: true })).toBeVisible();
    const evidenceRead = mockApi.calls('GET', `${path}/evidence-path`)[0];
    expect(new URLSearchParams(evidenceRead.query).get('fact_id')).toBe(factId);
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
    await expect(editor).toHaveClass(/semi-sidesheet-inner/);
    await button(editor.locator('.semi-sidesheet-footer'), label('common.save', '保存')).click();
    await expect(editor.getByText(label('vulnerabilityModal.titleRequired', '请填写漏洞标题'), { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await button(editor, label('common.cancel', '取消')).click();
    await expect(editor).toHaveCount(0);
});

for (const width of [1440, 1024, 390]) {
    test(`project: compact pagination and list alignment at ${width}px`, async ({ page, mockApi }, testInfo) => {
        await page.setViewportSize({ width, height: 1200 });
        const main = await visit(page, 'projects');
        await readsCompleted(mockApi, PAGE_READS.projects);
        const list = main.locator('.project-list-card');
        const pager = list.locator('.project-list-pagination');
        await expect(list.locator('.semi-table-tbody > tr')).toHaveCount(1);
        await expect(pager).toContainText('1/1');
        await expect(pager.getByRole('button', { name: 'Previous', exact: true })).toBeDisabled();
        await expect(pager.getByRole('button', { name: 'Next', exact: true })).toBeDisabled();
        await expect(list.locator('.semi-table-pagination-info, .semi-page-total, .semi-page-switch')).toHaveCount(0);
        await assertNoOverflow(page);

        const listBox = await list.boundingBox();
        const detailBox = await main.locator('.project-detail').boundingBox();
        const detailBottom = await main.locator('.project-detail .cs-section-card').last().boundingBox();
        const pagerBox = await pager.boundingBox();
        const controlsBox = await pager.locator('.semi-page').boundingBox();
        expect(controlsBox.height).toBeLessThanOrEqual(36);
        expect(controlsBox.x).toBeGreaterThanOrEqual(listBox.x);
        expect(controlsBox.x + controlsBox.width).toBeLessThanOrEqual(listBox.x + listBox.width);
        expect(listBox.y + listBox.height - pagerBox.y - pagerBox.height).toBeLessThanOrEqual(24);
        if (width > 900) {
            expect(Math.abs(listBox.y - detailBox.y)).toBeLessThanOrEqual(1);
            expect(Math.abs(listBox.y + listBox.height - detailBottom.y - detailBottom.height)).toBeLessThanOrEqual(1);
        } else {
            expect(detailBox.y).toBeGreaterThanOrEqual(listBox.y + listBox.height);
        }

        const tableBody = list.locator('.semi-table-body');
        const scrollLeft = await tableBody.evaluate(element => {
            element.scrollLeft = element.scrollWidth;
            return element.scrollLeft;
        });
        expect(scrollLeft).toBeGreaterThan(0);
        const scrolledPagerBox = await pager.boundingBox();
        expect(scrolledPagerBox.x).toBe(pagerBox.x);
        expect(scrolledPagerBox.width).toBe(pagerBox.width);
        await tableBody.evaluate(element => { element.scrollLeft = 0; });
        await screenshot(page, testInfo, `project-list-${width}`);
        expect(mockApi.writes()).toEqual([]);
    });
}

test('project: list height follows the active detail tab', async ({ page, mockApi }) => {
    mockApi.respond('GET', `/api/projects/${PROJECT.id}/conversations`, { json: { conversations: [] } });
    const main = await visit(page, 'projects');
    for (const name of [label('projects.tabSettings', '设置'), label('projects.tabConversations', '关联对话'), label('projects.tabFacts', '事实黑板')]) {
        await main.getByRole('tab', { name, exact: true }).click();
        await ready(page);
        const listBox = await main.locator('.project-list-card').boundingBox();
        const detailBottom = await main.locator('.project-detail .cs-section-card').last().boundingBox();
        expect(Math.abs(listBox.y + listBox.height - detailBottom.y - detailBottom.height)).toBeLessThanOrEqual(1);
    }
    expect(mockApi.writes()).toEqual([]);
});

test('project: compact pagination supports selection, filtering and a shrinking list', async ({ page, mockApi }) => {
    const projects = [PROJECT, ...Array.from({ length: 10 }, (_, index) => ({
        ...PROJECT,
        id: `fixture-project-${index + 2}`,
        name: `示例项目 ${index + 2} with a long project name`,
        status: index === 9 ? 'archived' : 'active'
    }))];
    const lastProject = projects.at(-1);
    mockApi.respond('GET', '/api/projects', { json: { projects, total: projects.length } });
    mockApi.respond('GET', `/api/projects/${lastProject.id}`, { json: lastProject });
    mockApi.respond('GET', `/api/projects/${lastProject.id}/stats`, { json: { fact_count: 0, vuln_count: 0, conversation_count: 0, sparse_fact_count: 0 } });
    mockApi.respond('GET', `/api/projects/${lastProject.id}/facts`, { json: [] });
    const main = await visit(page, 'projects');
    const list = main.locator('.project-list-card');
    const pager = list.locator('.project-list-pagination');
    const rows = list.locator('.semi-table-tbody > tr');
    const next = pager.getByRole('button', { name: 'Next', exact: true });
    const previous = pager.getByRole('button', { name: 'Previous', exact: true });
    await expect(rows).toHaveCount(10);
    await expect(pager).toContainText('1/2');
    const listBox = await list.boundingBox();
    const detailBottom = await main.locator('.project-detail .cs-section-card').last().boundingBox();
    expect(Math.abs(listBox.y + listBox.height - detailBottom.y - detailBottom.height)).toBeLessThanOrEqual(1);
    await next.click();
    await expect(rows).toHaveCount(1);
    await expect(pager).toContainText('2/2');
    await rows.first().click();
    await expect(page).toHaveURL(new RegExp(`id=${lastProject.id}(?:&|$)`));
    await expect(main.locator('.project-detail-title')).toHaveText(lastProject.name);
    await previous.click();
    await expect(rows).toHaveCount(10);
    await next.click();

    const search = list.getByPlaceholder(label('projects.searchProjectsPlaceholder', '搜索项目…'));
    await search.fill('no-matching-project');
    await expect(list.getByText(label('projects.noMatchingProjects', '无匹配项目'), { exact: true })).toBeVisible();
    await expect(pager).toHaveCount(0);
    await search.fill('');
    await expect(rows).toHaveCount(10);
    await expect(pager).toContainText('1/2');
    await next.click();
    const status = list.locator('.project-toolbar .semi-select');
    await status.click();
    await page.getByRole('option').filter({ hasText: label('projects.statusActive', '进行中') }).click();
    await expect(rows).toHaveCount(10);
    await expect(pager).toContainText('1/1');
    await expect(next).toBeDisabled();
    await status.click();
    await page.getByRole('option').filter({ hasText: label('projects.all', '全部') }).click();
    await next.click();
    mockApi.respond('GET', '/api/projects', { json: { projects: [PROJECT], total: 1 } });
    await button(main.locator('.cs-page-header'), label('common.refresh', '刷新')).click();
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toContainText(PROJECT.name);
    await expect(pager).toContainText('1/1');
    await expect(next).toBeDisabled();
    await expect(previous).toBeDisabled();

    mockApi.respond('GET', '/api/projects', { json: { projects: [], total: 0 } });
    await visit(page, 'projects');
    await page.reload();
    await ready(page);
    await expect(list.getByText(label('projects.noProjects', '暂无项目'), { exact: true })).toBeVisible();
    await expect(pager).toHaveCount(0);
    await assertNoOverflow(page);
    expect(mockApi.writes()).toEqual([]);
});

test('project: review a tentative fact and only link a readable source', async ({ page, mockApi }) => {
    const hidden = 'secret-conversation-id';
    const pending = {
        ...FACT, id: 'fixture-pending-fact', fact_key: 'fixture.pending', confidence: 'tentative',
        summary: '待审核示例事实', confirmation_note: '', source_conversation_id: '', source_message_id: '', execution_ids: []
    };
    const readable = {
        ...FACT, id: 'fixture-readable-fact', fact_key: 'fixture.readable', confidence: 'tentative',
        summary: '可回看来源', source_conversation_id: CONVERSATION.id, source_message_id: 'fixture-user-message',
        execution_ids: ['fixture-execution']
    };
    const facts = [pending, readable];
    mockApi.respond('GET', `/api/projects/${PROJECT.id}/facts`, (_entry, url) => {
        const key = url.searchParams.get('fact_key');
        return { json: key ? facts.find(item => item.fact_key === key) : facts };
    });
    mockApi.respond('PUT', `/api/projects/${PROJECT.id}/facts/${pending.id}`, (entry) => {
        Object.assign(pending, entry.body);
        return { json: { ...pending, incoming_links: [], outgoing_links: [] } };
    });
    const main = await visit(page, 'projects');
    await expect(main.getByRole('button', { name: '会话', exact: true })).toHaveCount(1);
    await expect(main.getByText(hidden)).toHaveCount(0);

    const row = main.locator('tr', { hasText: pending.summary });
    await button(row, label('common.edit', '编辑')).click();
    const dialog = page.getByRole('dialog', { name: label('projects.editFact', '编辑事实') });
    const confidence = dialog.getByRole('combobox').nth(1);
    await confidence.click();
    await page.locator('.semi-select-option').filter({ hasText: '已确认' }).click();
    await expect(confidence).toContainText('已确认');
    await button(dialog, label('projects.saveChanges', '保存修改')).click();
    await expect(page.getByText('升级为已确认需要审核说明', { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);

    const note = '人工核对来源回执后确认';
    await dialog.getByPlaceholder('说明为何把这条事实标为已确认').fill(note);
    await button(dialog, label('projects.saveChanges', '保存修改')).click();
    await expect(dialog).toHaveCount(0);
    expect(mockApi.calls('PUT', `/api/projects/${PROJECT.id}/facts/${pending.id}`).map(call => call.body)).toEqual([
        expect.objectContaining({ confidence: 'confirmed', confirmation_note: note, fact_key: pending.fact_key })
    ]);
    await expect(row.getByText('已确认', { exact: true })).toBeVisible();
    await button(row, label('common.edit', '编辑')).click();
    await expect(page.getByRole('dialog').getByPlaceholder('说明为何把这条事实标为已确认')).toHaveValue(note);
    await expect(main.getByText(hidden)).toHaveCount(0);
});

test('project: dashboard jump, fact detail opens/closes and create validates then retries', async ({ page, mockApi }) => {
    await visit(page, 'dashboard');
    await button(page.locator('#main-content'), PROJECT.name).click();
    await expect(page).toHaveURL(new RegExp(`#/projects\\?id=${PROJECT.id}$`));
    const main = await ready(page);
    await button(main, FACT.summary).click();
    const drawer = page.getByRole('dialog');
    await expect(drawer.getByText(FACT.body, { exact: true })).toBeVisible();
    const backdrop = page.locator('.semi-sidesheet-mask');
    const outside = { position: { x: 32, y: 300 } };
    await backdrop.click(outside);
    await expect(drawer).toHaveCount(0);

    const projectRow = main.getByRole('row').filter({ hasText: PROJECT.name });
    await button(projectRow, label('projects.archiveProject', '归档')).click();
    const confirmation = page.getByRole('dialog');
    await expect(confirmation).toContainText(label('projects.confirmArchiveProject'));
    await page.locator('.semi-modal-wrap').click(outside);
    await expect(confirmation).toHaveCount(0);
    expect(mockApi.writes()).toEqual([]);

    const openProject = button(main, label('projects.newProjectCta', '+ 新建项目'));
    await openProject.click();
    const dialog = page.getByRole('dialog');
    const create = button(dialog.locator('.semi-sidesheet-footer'), label('projects.createProject', '创建项目'));
    await expect(dialog).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(create).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(dialog.locator('button.semi-sidesheet-close')).toBeFocused();
    await backdrop.click(outside);
    await expect(dialog).toHaveCount(0);
    await expect(openProject).toBeFocused();
    await openProject.click();

    await create.click();
    await expect(page.getByText(label('projects.enterProjectName', '请输入项目名称'), { exact: true })).toBeVisible();
    expect(mockApi.writes()).toEqual([]);
    await dialog.getByLabel(label('projects.projectName', '项目名称'), { exact: true }).fill(PROJECT.name);
    await dialog.getByLabel(label('projects.projectDescription', '项目描述'), { exact: true }).fill(PROJECT.description);
    let finishSave;
    const saving = new Promise(resolve => { finishSave = resolve; });
    mockApi.respond('POST', '/api/projects', () => saving);
    await create.click();
    try {
        await expect(create).toBeDisabled();
        await backdrop.click(outside);
        await expect(dialog).toBeVisible();
    } finally {
        finishSave({ status: 503, json: { error: '示例保存失败，可重试' } });
    }
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

test.describe('import drawers on a narrow screen', () => {
    test.use({ appPermissions: [...UI_PERMISSIONS, 'asset:write', 'workflow:write'], viewport: { width: 390, height: 650 } });

    for (const [id, trigger, title, action] of [
        ['asset-library', label('assets.bulkImport', '批量导入'), label('assets.bulkImportTitle', '批量导入资产'), /导入.*有效数据/],
        ['workflows', label('workflows.package.importLocal', '导入本地包'), label('workflows.package.title', '导入本地包'), label('workflows.package.confirmImport', '确认导入')]
    ]) {
        test(`${id}: import fits the viewport and keeps its action visible`, async ({ page, mockApi }) => {
            const main = await visit(page, id);
            const open = button(main, trigger);
            await open.click();
            const drawer = page.getByRole('dialog', { name: title });
            await expect(drawer).toHaveClass(/semi-sidesheet-inner/);
            await assertDrawerFits(page, drawer);
            const submit = drawer.locator('.semi-sidesheet-footer').getByRole('button', { name: action });
            await expect(submit).toBeDisabled();
            await expect(submit).toBeInViewport({ ratio: 1 });
            await page.keyboard.press('Escape');
            await expect(drawer).toHaveCount(0);
            await expect(open).toBeFocused();

            await page.setViewportSize({ width: 1440, height: 1000 });
            await open.click();
            await expect(drawer).toBeVisible();
            await page.locator('.semi-sidesheet-mask').click({ position: { x: 32, y: 300 } });
            await expect(drawer).toHaveCount(0);
            expect(mockApi.writes()).toEqual([]);
        });
    }
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
        await expect(nav.getByText(APP_VERSION, { exact: true })).toBeVisible();
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

    test('info collection: the results title and actions fit in the card header', async ({ page }) => {
        const main = await visit(page, 'info-collect');
        const titleText = label('infoCollectPage.queryResults', '查询结果');
        const header = main.locator('.semi-card-header-wrapper').filter({ hasText: titleText });
        const title = header.getByText(titleText, { exact: true });
        const importButton = button(header, label('assets.importSelected', '入库所选'));

        await header.scrollIntoViewIfNeeded();
        await expect(title).toBeInViewport({ ratio: 1 });
        await expect(importButton).toBeInViewport({ ratio: 1 });
        const titleBox = await title.boundingBox();
        const actionBox = await importButton.boundingBox();
        expect(actionBox.y).toBeGreaterThanOrEqual(titleBox.y + titleBox.height);
    });

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
