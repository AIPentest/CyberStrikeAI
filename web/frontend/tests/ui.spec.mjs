import { readFileSync } from 'node:fs';
import {
    test, expect, visit, ready, screenshot, label,
    PROJECT, FACT, VULNERABILITY
} from './fixtures.mjs';

// Read the real registry without importing lazy JSX into Node. New pages join the sweep automatically.
const registry = readFileSync(new URL('../src/pages/registry.js', import.meta.url), 'utf8');
const pageIds = [...registry.matchAll(/^\s*(?:'([^']+)'|([\w-]+)):\s*lazy\(/gm)].map(match => match[1] || match[2]);
if (!pageIds.length || new Set(pageIds).size !== pageIds.length) throw new Error('Cannot enumerate the React page registry');

const PAGE_READS = {
    dashboard: ['/api/vulnerabilities/stats', '/api/monitor/stats', '/api/usage/tokens', '/api/projects/dashboard-summary'],
    projects: ['/api/projects', `/api/projects/${PROJECT.id}`, `/api/projects/${PROJECT.id}/facts`],
    'asset-overview': ['/api/assets/stats', '/api/assets'],
    'asset-library': ['/api/assets'],
    vulnerabilities: ['/api/vulnerabilities', '/api/vulnerabilities/filter-options'],
    'chat-files': ['/api/chat-uploads'],
    'mcp-monitor': ['/api/monitor', '/api/monitor/calls-timeline'],
    'knowledge-retrieval-logs': ['/api/knowledge/retrieval-logs'],
    'knowledge-management': ['/api/knowledge/items', '/api/knowledge/index-status'],
    'skills-monitor': ['/api/skills/stats'],
    'skills-management': ['/api/skills'],
    'agents-management': ['/api/multi-agent/markdown-agents'],
    'roles-management': ['/api/roles'],
    'platform-rbac': ['/api/rbac/metadata', '/api/rbac/roles', '/api/rbac/users', '/api/rbac/resource-assignments'],
    settings: ['/api/config'],
    'api-docs': ['/api/openapi/spec'],
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
    ['dashboard.vulnTotal', '漏洞总数', 'vulnerabilities'],
    ['dashboard.toolCalls', '工具调用', 'mcp-monitor'],
    ['dashboard.successRate', '执行成功率', 'mcp-monitor'],
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

for (const [id, path, record] of [
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

test('vulnerability: dashboard deep link, detail retry, evidence and close; create validates and saves', async ({ page, mockApi }, testInfo) => {
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
    const title = '新增匿名记录';
    mockApi.respond('POST', '/api/vulnerabilities', { json: { ...VULNERABILITY, title, severity: 'low' } });
    await editor.getByRole('textbox', { name: /^标题/ }).fill(title);
    await editor.getByRole('combobox', { name: /严重程度/ }).click();
    await page.getByRole('option', { name: /低危$/ }).click();
    await button(editor, label('common.save', '保存')).click();
    await expect(editor).toHaveCount(0);
    expect(mockApi.calls('POST', '/api/vulnerabilities').map(call => call.body)).toMatchObject([{ title, severity: 'low' }]);
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

    for (const id of ['dashboard', 'asset-overview', 'projects', 'vulnerabilities']) {
        test(`${id}: workspace stays within 390px`, async ({ page, mockApi }) => {
            await visit(page, id);
            await readsCompleted(mockApi, PAGE_READS[id]);
            await ready(page);
            await assertNoOverflow(page);
        });
    }

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
