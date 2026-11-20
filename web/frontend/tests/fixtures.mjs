import { test as base, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

export { expect };
export const ORIGIN = 'http://127.0.0.1:5199';
export const FAKE_TOKEN = 'FAKE-PLAYWRIGHT-NOT-A-REAL-TOKEN';

// Explicit keys from internal/security/rbac.go. No wildcard or application auth bypass.
export const READ_PERMISSIONS = [
    'auth:self', 'dashboard:read', 'chat:read', 'hitl:read',
    'tasks:read', 'project:read', 'vulnerability:read', 'asset:read', 'webshell:read',
    'c2:read', 'mcp:read', 'knowledge:read', 'skills:read', 'agents:read', 'roles:read',
    'config:read', 'rbac:read',
    'notification:read', 'files:read', 'monitor:read', 'openapi:read', 'update:read'
];
export const UI_PERMISSIONS = [
    ...READ_PERMISSIONS, 'fofa:execute', 'project:write', 'vulnerability:write'
];

const translations = new Map(['zh-CN', 'en-US'].map(lang => [
    lang, JSON.parse(readFileSync(new URL(`../../static/i18n/${lang}.json`, import.meta.url), 'utf8'))
]));
// Use the real dictionaries, not stubbed translations that hide localization changes.
export function label(key, fallback) {
    for (const lang of ['zh-CN', 'en-US']) {
        const value = key.split('.').reduce((node, part) => node?.[part], translations.get(lang));
        if (typeof value === 'string') return value;
    }
    return fallback;
}

export const STAMP = '2026-11-01T08:00:00Z';
export const PROJECT = {
    id: 'fixture-project', name: '示例项目', description: '仅用于本地浏览器回归的匿名项目',
    status: 'active', pinned: false, created_at: STAMP, updated_at: STAMP
};
export const FACT = {
    id: 'fixture-fact', project_id: PROJECT.id, project_name: PROJECT.name,
    fact_key: 'fixture.localhost.scope', category: 'target', confidence: 'confirmed',
    summary: '示例目标仅限 localhost', body: '仅检查 http://localhost:8080/ 的匿名示例。',
    created_at: STAMP, updated_at: STAMP, incoming_links: [], outgoing_links: [],
    link_counts: { incoming: 0, outgoing: 0 }
};
export const VULNERABILITY = {
    id: 'fixture-vulnerability', title: '示例访问控制问题', severity: 'critical', status: 'open',
    type: '访问控制', project_id: PROJECT.id, target: 'http://localhost:8080/example',
    host: 'localhost', description: '匿名回归数据，不代表真实漏洞。',
    evidence: 'FAKE-EVIDENCE: local fixture only', recommendation: '核对示例访问权限。',
    created_at: STAMP, updated_at: STAMP
};
const CONVERSATION_ID = 'fixture-conversation';

function makeReads(session) {
    const vulnerabilities = [
        VULNERABILITY,
        { ...VULNERABILITY, id: 'fixture-vulnerability-high', title: '示例响应头问题', severity: 'high' },
        { ...VULNERABILITY, id: 'fixture-vulnerability-low', title: '示例信息提示', severity: 'low' }
    ];
    const monitor = {
        summary: { totalCalls: 10, successCalls: 8, failedCalls: 2, runningCalls: 0, toolCount: 2 },
        topTools: [{ name: 'fixture_read_only', count: 10, totalCalls: 10 }]
    };
    return new Map(Object.entries({
        '/api/auth/validate': {
            expires_at: session.expiresAt, user: session.user, roles: session.roles,
            permissions: session.permissions
        },
        '/api/notifications/summary': { items: [] },
        '/api/agent-loop/tasks': { tasks: [1, 2].map(id => ({
            runId: `fixture-run-${id}`, conversationId: CONVERSATION_ID,
            title: `示例任务 ${id}`, status: 'running', startedAt: STAMP
        })) },
        '/api/vulnerabilities/stats': { total: 3, distinct: 3, by_severity: { critical: 1, high: 1, medium: 0, low: 1, info: 0 }, by_status: { open: 3 } },
        '/api/vulnerabilities': { vulnerabilities, total: 3, page: 1, limit: 20 },
        '/api/vulnerabilities/filter-options': { project_ids: [PROJECT.id], conversation_tags: [], task_tags: [], assessment_ids: [] },
        [`/api/vulnerabilities/${VULNERABILITY.id}/context`]: { vulnerability: VULNERABILITY, history: [], members: [], assets: [] },
        [`/api/vulnerabilities/${VULNERABILITY.id}`]: VULNERABILITY,
        '/api/batch-tasks': { queues: [], total: 0, page: 1 },
        '/api/monitor/stats': monitor,
        '/api/monitor': { ...monitor, executions: [], total: 0, page: 1, retentionDays: 30 },
        '/api/monitor/calls-timeline': { points: [], summary: { totalCalls: 10, peak: 0 } },
        '/api/usage/tokens': { summary: { totalTokens: 1200, calls: 4, promptTokens: 800, completionTokens: 400 }, daily: [], records: [] },
        '/api/knowledge/stats': { total_items: 0, categories: {} },
        '/api/knowledge/items': { items: [], total: 0 },
        '/api/knowledge/categories': { categories: [] },
        '/api/knowledge/index-status': { indexed: 0, total: 0, running: false },
        '/api/knowledge/retrieval-logs': { logs: [], total: 0 },
        '/api/skills/stats': { total_skills: 0, total_calls: 0, total_success: 0, stats: [] },
        '/api/skills': { skills: [], total: 0 },
        '/api/config': { ai: { channels: {} }, tools: [] },
        '/api/config/tools': { tools: [], total: 0 },
        '/api/system/update': { status: { installed: true } },
        '/api/openapi/spec': { paths: {
            '/api/projects': { get: { summary: '浏览项目', tags: ['项目'], responses: {} } }
        } },
        '/api/roles': { roles: [{ name: '示例只读角色', description: '匿名角色', tools: [], enabled: true }] },
        '/api/multi-agent/markdown-agents': { agents: [] },
        '/api/hitl/pending': { items: [], total: 0, page: 1, pageSize: 20 },
        '/api/external-mcp/stats': { total: 0, enabled: 0, connected: 0, disabled: 0 },
        '/api/config/tools': { tools: [], total: 0, page: 1, page_size: 20 },
        '/api/webshell/connections': { connections: [], total: 0 },
        '/api/c2/listeners': { listeners: [] },
        '/api/c2/sessions': { sessions: [], total: 0 },
        '/api/chat-uploads': { files: [], folders: [], total: 0 },
        '/api/projects': { projects: [PROJECT], total: 1 },
        '/api/projects/dashboard-summary': {
            totals: { active_projects: 1, total_facts: 1 }, projects: [PROJECT], recent_facts: [FACT]
        },
        [`/api/projects/${PROJECT.id}`]: PROJECT,
        [`/api/projects/${PROJECT.id}/stats`]: { fact_count: 1, vuln_count: 3, conversation_count: 1, sparse_fact_count: 0 },
        [`/api/projects/${PROJECT.id}/facts`]: url => url.searchParams.has('fact_key') ? FACT : [FACT],
        '/api/assets': { assets: [], total: 0, page: 1, page_size: 20 },
        '/api/assets/stats': { total: 0, ips: 0, domains: 0, ports: 0, recent: 0, protocols: [], coverage: { scanned: 0, scanned_30d: 0, never_scanned: 0, stale: 0 } },
        '/api/rbac/metadata': { permissions: Object.fromEntries(UI_PERMISSIONS.map(key => [key, key])) },
        '/api/rbac/roles': { roles: [] },
        '/api/rbac/users': { users: [session.user] },
        '/api/rbac/resource-assignments': { assignments: [] }
    }));
}

class MockApi {
    constructor(session) {
        this.reads = makeReads(session);
        this.overrides = new Map();
        this.requests = [];
        this.unexpected = [];
        this.pageErrors = [];
    }

    // Overrides are persistent until restored: React StrictMode may issue a GET twice.
    respond(method, path, response) {
        if (!path.startsWith('/api/')) throw new Error('Only /api/ fixture overrides are permitted');
        this.overrides.set(`${method.toUpperCase()} ${path}`, response);
    }

    fail(method, path, message = '示例接口暂不可用，请重试') {
        this.respond(method, path, { status: 503, json: { error: message } });
    }

    restore(method, path) {
        this.overrides.delete(`${method.toUpperCase()} ${path}`);
    }

    calls(method, path) {
        return this.requests.filter(item => item.method === method && item.path === path);
    }

    writes() {
        return this.requests.filter(item => !['GET', 'HEAD', 'OPTIONS'].includes(item.method));
    }

    async route(route) {
        const request = route.request();
        const url = new URL(request.url());
        const method = request.method();
        if (url.origin !== ORIGIN) {
            this.unexpected.push(`Blocked external request: ${method} ${url.href}`);
            return route.abort('blockedbyclient');
        }
        if (url.pathname === '/api' || url.pathname.startsWith('/api/')) {
            let body = request.postData();
            try { body = body ? JSON.parse(body) : null; } catch { /* Keep non-JSON fixture requests visible. */ }
            const entry = { method, path: url.pathname, query: url.search, body };
            this.requests.push(entry);
            const key = `${method} ${url.pathname}`;
            let response;
            if (this.overrides.has(key)) {
                const override = this.overrides.get(key);
                response = typeof override === 'function' ? await override(entry, url) : override;
            } else if (method === 'GET' && this.reads.has(url.pathname)) {
                const value = this.reads.get(url.pathname);
                response = { json: typeof value === 'function' ? value(url) : value };
            } else {
                this.unexpected.push(`Unregistered API (not forwarded): ${key}${url.search}`);
                response = { status: 501, json: { error: `Missing Playwright fixture: ${key}` } };
            }
            if (url.pathname !== '/api/auth/login' && request.headers().authorization !== `Bearer ${FAKE_TOKEN}`) {
                this.unexpected.push(`Missing FAKE fixture bearer: ${key}`);
            }
            await route.fulfill({
                status: 200,
                headers: { 'cache-control': 'no-store' },
                ...response
            });
            entry.fulfilled = true;
            entry.status = response.status ?? 200;
            return;
        }
        const language = /^\/static\/i18n\/(zh-CN|en-US)\.json$/.exec(url.pathname)?.[1];
        if (method === 'GET' && language) return route.fulfill({ json: translations.get(language) });
        // Only local Vite modules/documents and static visual assets may reach the dev server.
        // fetch/XHR outside the explicitly mocked endpoints is never passed through.
        const staticPath = url.pathname.startsWith('/static/app/')
            || /^\/static\/(logo\.png|favicon\.ico|site\.webmanifest)$/.test(url.pathname);
        const assetType = ['document', 'script', 'stylesheet', 'image', 'font', 'manifest', 'other'].includes(request.resourceType());
        if (method === 'GET' && staticPath && assetType) return route.continue();
        this.unexpected.push(`Blocked non-fixture request: ${method} ${url.href}`);
        return route.abort('blockedbyclient');
    }
}

export const test = base.extend({
    authenticated: [true, { option: true }],
    appPermissions: [UI_PERMISSIONS, { option: true }],
    mockApi: [async ({ context, authenticated, appPermissions }, use, testInfo) => {
        const session = {
            token: FAKE_TOKEN, expiresAt: new Date(Date.now() + 86_400_000).toISOString(),
            user: { id: 'FAKE-PLAYWRIGHT-USER', username: 'FAKE_TEST_USER', display_name: '示例用户' },
            roles: ['FAKE_TEST_ROLE'], permissions: [...appPermissions]
        };
        const mock = new MockApi(session);
        context.on('weberror', event => mock.pageErrors.push(event.error().stack || event.error().message));
        await context.route('**/*', route => mock.route(route));
        await context.routeWebSocket('**/*', socket => {
            const url = new URL(socket.url());
            // Mock the Vite HMR socket without connecting to any server. Other sockets fail closed.
            if (url.origin === ORIGIN.replace('http:', 'ws:') && url.pathname === '/static/app/') return;
            mock.unexpected.push(`Blocked WebSocket: ${url.href}`);
            socket.close();
        });
        await context.addInitScript(({ session, authenticated, origin }) => {
            if (location.origin !== origin || localStorage.getItem('FAKE_PLAYWRIGHT_SEEDED')) return;
            localStorage.clear();
            localStorage.setItem('FAKE_PLAYWRIGHT_SEEDED', '1');
            localStorage.setItem('csai_lang', 'zh-CN');
            localStorage.setItem('cyberstrike-theme', 'system');
            localStorage.setItem('csai-nav-collapsed', '0');
            if (authenticated) localStorage.setItem('cyberstrike-auth', JSON.stringify(session));
        }, { session, authenticated, origin: ORIGIN });
        await use(mock);
        if (testInfo.status !== testInfo.expectedStatus) console.error('Fixture writes:', JSON.stringify(mock.writes()));
        await testInfo.attach('fixture-api-log', {
            body: Buffer.from(JSON.stringify({ requests: mock.requests, unexpected: mock.unexpected, pageErrors: mock.pageErrors }, null, 2)),
            contentType: 'application/json'
        });
        expect.soft(mock.unexpected, 'Every API must have an explicit local fixture; external traffic is forbidden').toEqual([]);
        expect.soft(mock.pageErrors, 'No uncaught browser / React errors').toEqual([]);
    }, { auto: true }]
});

export async function ready(page) {
    const main = page.locator('#main-content');
    await expect(main).toBeVisible();
    await expect(main.locator('.cs-page-loading')).toHaveCount(0);
    await expect(main.locator('.cs-page-title, .chat-header-title')).toBeVisible();
    // Wait for actual data loading indicators, not just the always-present layout main.
    await expect(main.locator('.semi-spin:not(.semi-spin-hidden):visible')).toHaveCount(0);
    await expect(main.locator('[aria-busy="true"]:visible')).toHaveCount(0);
    await expect(main.getByText(label('common.forbidden', '当前账号没有访问该页面的权限'), { exact: true })).toHaveCount(0);
    return main;
}

export async function visit(page, pageId) {
    await page.goto(`/static/app/#/${pageId}`);
    return ready(page);
}

export async function screenshot(page, testInfo, name) {
    const path = testInfo.outputPath(`${name}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    await testInfo.attach(name, { path, contentType: 'image/png' });
}
