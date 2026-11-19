const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');
const monitor = fs.readFileSync('web/static/js/monitor.js', 'utf8');
const webshell = fs.readFileSync('web/static/js/webshell.js', 'utf8');
const styles = fs.readFileSync('web/static/css/style.css', 'utf8');
const zh = JSON.parse(fs.readFileSync('web/static/i18n/zh-CN.json', 'utf8'));
const en = JSON.parse(fs.readFileSync('web/static/i18n/en-US.json', 'utf8'));

test('Pi token usage summary is rendered as a first-class timeline event', () => {
    assert.match(monitor, /case 'agent_usage_summary'/);
    assert.match(monitor, /formatPiUsageSummaryTitle\(d\)/);
    assert.match(monitor, /formatPiUsageSummaryMessage\(d\)/);
    assert.match(monitor, /addTimelineItem\(timeline, 'agent_usage_summary'/);
    assert.match(styles, /\.timeline-item-agent_usage_summary/);
    assert.match(styles, /html\[data-theme="dark"\] \.timeline-item-agent_usage_summary/);
});

test('Pi token usage summary is visible in WebShell live and restored timelines', () => {
    assert.match(webshell, /_et === 'agent_usage_summary'/);
    assert.match(webshell, /appendTimelineItem\('agent_usage_summary'/);
    assert.match(webshell, /function formatWebshellPiUsageSummaryTitle/);
    assert.match(webshell, /function formatWebshellPiUsageSummaryMessage/);
    assert.match(webshell, /eventType === 'agent_usage_summary'/);
    assert.match(styles, /\.webshell-ai-timeline-agent_usage_summary/);
    assert.match(styles, /html\[data-theme="dark"\] \.webshell-ai-timeline-agent_usage_summary/);
});

test('chat history renders recorded token counts instead of repeating the raw event label', () => {
    const formatters = monitor.slice(monitor.indexOf('function formatCompactInteger('), monitor.indexOf('function dispatchAgentPlanTaskEvent('));
    const renderer = chat.slice(chat.indexOf('function renderOneProcessDetail('), chat.indexOf('function finishPrependRender('));
    const rows = [];
    const context = vm.createContext({
        window: { t: key => en.chat[key.slice('chat.'.length)] },
        timeline: {},
        prependMode: false,
        getCurrentTimeLocale: () => 'en-US',
        processDetailAgentPrefix: () => '',
        addTimelineItem: (_timeline, type, options) => rows.push({ type, ...options })
    });
    vm.runInContext(formatters + renderer, context);
    context.renderOneProcessDetail({
        id: 'usage-1',
        eventType: 'agent_usage_summary',
        message: 'Pi token usage summary',
        data: { modelCalls: 1, promptTokens: 1000, completionTokens: 24, totalTokens: 1024 }
    });
    const error = 'Pi worker exited without a result: invalid Pi protocol JSON: unexpected end of JSON input';
    context.renderOneProcessDetail({ id: 'error-1', eventType: 'error', message: error });

    assert.equal(rows.length, 2);
    assert.equal(rows[0].type, 'agent_usage_summary');
    assert.equal(rows[0].title, en.chat.piUsageSummaryTitle + ' · 1,024');
    assert.equal(rows[0].message, [
        en.chat.piUsageModelCalls + ': 1',
        en.chat.piUsagePromptTokens + ': 1,000',
        en.chat.piUsageCompletionTokens + ': 24',
        en.chat.piUsageTotalTokens + ': 1,024'
    ].join('\n'));
    assert.equal(rows[1].type, 'error');
    assert.equal(rows[1].message, error);
});

test('Pi token usage summary supports language refresh', () => {
    assert.match(monitor, /type === 'agent_usage_summary'/);
    assert.match(monitor, /item\.dataset\.modelCalls/);
    assert.match(monitor, /setTimelineItemContentStreamPlain\(contentEl, formatPiUsageSummaryMessage\(usageData\)\)/);
});

test('Pi resilience and usage timeline labels have zh/en translations', () => {
    [
        'piModelRetryTitle',
        'piModelFailoverTitle',
        'piUsageSummaryTitle',
        'piUsageModelCalls',
        'piUsagePromptTokens',
        'piUsageCompletionTokens',
        'piUsageTotalTokens',
        'piUsageCachedTokens',
        'piUsageReasoningTokens'
    ].forEach((key) => {
        assert.equal(typeof zh.chat[key], 'string', `zh chat.${key}`);
        assert.ok(zh.chat[key].length > 0, `zh chat.${key} is empty`);
        assert.equal(typeof en.chat[key], 'string', `en chat.${key}`);
        assert.ok(en.chat[key].length > 0, `en chat.${key} is empty`);
    });
});
