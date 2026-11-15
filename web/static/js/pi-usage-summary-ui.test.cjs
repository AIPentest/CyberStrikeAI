const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

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
