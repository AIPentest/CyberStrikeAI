const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');
const source = fs.readFileSync('web/static/js/token-usage.js', 'utf8');
const harness = () => {
    const context = { document: { addEventListener() {} }, window: {}, AbortController };
    vm.createContext(context);
    vm.runInContext(source, context);
    return context;
};

test('cache and reasoning details do not inflate total or double-count input', () => {
    const ctx = harness();
    const metrics = ctx.tokenUsageMetrics({ totalTokens: 120, promptTokens: 100, completionTokens: 20, cachedTokens: 40, cacheWriteTokens: 30, reasoningTokens: 10, modelCalls: 1 });
    const values = Object.fromEntries(metrics);
    assert.equal(values.usageTotal, 120);
    assert.equal(values.usageCacheWrite, 30);
    assert.equal(values.usageCacheRate, '40.0%');
    assert.equal(Object.fromEntries(ctx.tokenUsageMetrics({})).usageCacheRate, '—');
    assert.equal(ctx.tokenUsageCount(Infinity), 0);
    assert.equal(ctx.tokenUsageCount(-12), 0);
});

test('period selection refreshes once and rejects arbitrary periods', () => {
    const ctx = harness();
    let calls = 0;
    ctx.refreshTokenUsage = () => { calls++; };
    ctx.document.getElementById = () => ({ textContent: '' });
    ctx.changeTokenUsagePeriod('30');
    ctx.changeTokenUsagePeriod('999');
    assert.equal(ctx.tokenUsageState.days, 30);
    assert.equal(calls, 1);
});

test('cache hit ratio uses input tokens, clamps invalid detail and handles missing input', () => {
    const ctx = harness();
    assert.equal(ctx.tokenUsageCacheRate({ promptTokens: 1000, cachedTokens: 930, totalTokens: 1100 }), '93.0%');
    assert.equal(ctx.tokenUsageCacheRate({ promptTokens: 1000, cachedTokens: 0 }), '0.0%');
    assert.equal(ctx.tokenUsageCacheRate({ promptTokens: 10, cachedTokens: 30 }), '100.0%');
    assert.equal(ctx.tokenUsageCacheRate({ promptTokens: 0, cachedTokens: 30 }), '—');
    assert.equal(ctx.tokenUsageCacheRate(null), '—');
});

test('daily trends retain server day keys and fill only gaps within returned history', () => {
    const ctx = harness();
    const rows = ctx.tokenUsageTimeline([{ key: '2026-10-12', totalTokens: 12 }, { key: '2026-10-10', totalTokens: 10 }, { key: 'invalid', totalTokens: 20 }]);
    assert.deepEqual(Array.from(rows, row => row.key), ['2026-10-10', '2026-10-11', '2026-10-12']);
    assert.equal(rows[1].totalTokens, 0);
    assert.equal(rows[1].events, 0);
    assert.equal(rows[2].totalTokens, 12);
    assert.equal(ctx.tokenUsageTimeline([]).length, 0);
    assert.equal(ctx.tokenUsageTimeline([{ key: '2026-10-10', totalTokens: 10 }]).length, 1);
});

test('detail requests stay project scoped and discard late results after a period change', async () => {
    const ctx = harness();
    const host = { setAttribute() {}, removeAttribute() {}, textContent: '' };
    ctx.document.getElementById = id => id === 'settings-token-analysis' ? host : null;
    ctx.getActiveProjectId = () => 'project / sample';
    const requests = [];
    ctx.apiFetch = (url, options) => new Promise(resolve => requests.push({ url, options, resolve }));
    const rendered = [];
    ctx.renderTokenUsageAnalysis = value => rendered.push(value);
    const first = ctx.refreshTokenUsage();
    ctx.tokenUsageState.days = 30;
    const second = ctx.refreshTokenUsage();
    assert.ok(requests[0].options.signal.aborted);
    assert.match(requests[1].url, /days=30.*project_id=project%20%2F%20sample/);
    requests[1].resolve({ ok: true, json: async () => ({ summary: { totalTokens: 30 } }) });
    await second;
    requests[0].resolve({ ok: true, json: async () => ({ summary: { totalTokens: 7 } }) });
    await first;
    assert.equal(rendered.length, 1);
    assert.equal(rendered[0].summary.totalTokens, 30);
    const pending = ctx.refreshTokenUsage();
    ctx.stopTokenUsageRequests();
    requests[2].resolve({ ok: true, json: async () => ({ summary: { totalTokens: 90 } }) });
    await pending;
    assert.equal(rendered.length, 1, 'leaving the section must invalidate outstanding results');
});

test('all supported languages expose usage labels and interpolation placeholders', () => {
    for (const locale of ['zh-CN', 'en-US', 'ru-RU']) {
        const data = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        for (const key of ['usageView', 'usageScope', 'usageCacheRead', 'usageCacheWrite', 'usageTrend', 'usageCacheHitRate']) assert.ok(data.dashboard[key]);
        assert.ok(data.settings.nav.tokenUsage);
        for (const key of ['subtitle', 'scopeNote', 'trendMetric', 'cacheComposition', 'cacheHit', 'uncachedInput', 'cacheNote', 'scopeProject', 'scopeAccessible', 'noDailyData']) assert.ok(data.tokenUsageUI[key]);
        assert.match(data.dashboard.usageCacheHitRate, /\{\{rate\}\}/);
        assert.match(data.dashboard.tokenUsageSub, /\{\{rate\}\}/);
        assert.match(data.chat.turnTokenUsageTitle, /\{\{cacheWrite\}\}/);
    }
});
