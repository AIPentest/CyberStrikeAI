const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('web/static/js/monitor.js', 'utf8');
const statsSource = source.slice(source.indexOf('const MCP_STATS_TOP_N'), source.indexOf('function renderMonitorExecutions('));

function harness() {
    const container = { innerHTML: '' };
    const escapeHtml = (value) => String(value).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    const context = vm.createContext({
        window: { __locale: 'zh-CN' },
        document: { getElementById: (id) => id === 'monitor-stats' ? container : null },
        localStorage: { getItem: () => null },
        monitorState: {},
        escapeHtml,
        escapeAttrLocal: escapeHtml,
        formatMonitorToolName: (name) => name,
        monitorToolNamesEqual: (a, b) => a === b,
    });
    vm.runInContext(statsSource, context);
    Object.assign(context, {
        bindMonitorStatsPanelEvents() {},
        bindMcpStatsTimelineEvents() {},
        updateMonitorStatsSubtitle() {},
    });
    return { context, container };
}

test('security blocks count toward total calls but not failures or terminations', () => {
    const { context } = harness();
    const totals = context.buildMonitorTotals({ totalCalls: 10, successCalls: 4, failedCalls: 1, blockedCalls: 3 });
    assert.deepEqual({ ...totals }, { total: 10, success: 4, failed: 1, blocked: 3, neutral: 2, lastCallTime: null });
    assert.equal(context.buildMonitorTotals({ totalCalls: 3, blockedCalls: 3 }).neutral, 0);
    assert.equal(context.buildMonitorTotals({ totalCalls: 2, successCalls: 1, failedCalls: 1 }).blocked, 0);
});

test('the overview success rate excludes security blocks and hides failure/termination labels when only blocked', () => {
    const { context, container } = harness();
    context.renderMonitorStats({ totalCalls: 5, successCalls: 1, failedCalls: 1, blockedCalls: 3 });
    assert.match(container.innerHTML, />50\.0%<\/span>/);
    assert.match(container.innerHTML, /is-blocked">Security block 3<\/span>/);
    assert.doesNotMatch(container.innerHTML, /is-neutral/);

    context.renderMonitorStats({ totalCalls: 3, blockedCalls: 3 });
    assert.match(container.innerHTML, /value--rate is-muted">-<\/span>/);
    assert.match(container.innerHTML, /is-fail">Failed 0<\/span>/);
    assert.doesNotMatch(container.innerHTML, /is-danger|is-neutral|0\.0%/);
});

test('tool statistics show security blocks separately and do not lower the tool success rate', () => {
    const { context } = harness();
    for (const render of [context.renderMcpStatsToolTable, context.renderMcpStatsToolsPanel]) {
        const blockedOnly = render([{ toolName: 'safe-tool', totalCalls: 4, blockedCalls: 4 }], { total: 4 });
        assert.match(blockedOnly, /Security block 4/);
        assert.match(blockedOnly, /is-muted">-<\/span>/);
        assert.doesNotMatch(blockedOnly, /is-danger|>0\.0%<\/span>/);

        const mixed = render([{ toolName: 'safe-tool', totalCalls: 10, successCalls: 3, failedCalls: 1, blockedCalls: 6 }], { total: 10 });
        assert.match(mixed, /75\.0%/);
        assert.match(mixed, /Security block 6/);
        assert.match(mixed, /Failed 1/);
    }
});

test('the trend chart distinguishes security blocks from failures and keeps separate hover counts', () => {
    const { context } = harness();
    const points = [{ t: '2026-09-07T00:00:00Z', total: 3, failed: 0, blocked: 3 }];
    const html = context.renderMcpStatsTimelineBody({ range: '24h', points, summary: { totalCalls: 3, peak: 3 } });
    assert.match(html, /legend-item--blocked">Security block/);
    assert.match(html, /mcp-stats-timeline-bar-blocked/);
    assert.match(html, /mcp-stats-timeline-line--blocked/);
    assert.match(html, /data-total="3" data-failed="0" data-blocked="3"/);
    assert.doesNotMatch(html, /legend-item--fail|mcp-stats-timeline-bar-fail/);

    const mixed = context.buildMcpTimelineSvg([{ ...points[0], total: 4, failed: 1, blocked: 2 }], '24h');
    assert.match(mixed, /data-total="4" data-failed="1" data-blocked="2"/);
    assert.match(mixed, /mcp-stats-timeline-bar-fail/);
    assert.match(mixed, /mcp-stats-timeline-bar-blocked/);
});
