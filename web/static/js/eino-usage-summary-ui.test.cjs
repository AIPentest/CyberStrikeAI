const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

const monitor = fs.readFileSync('web/static/js/monitor.js', 'utf8');
const webshell = fs.readFileSync('web/static/js/webshell.js', 'utf8');
const styles = fs.readFileSync('web/static/css/style.css', 'utf8');
const en = JSON.parse(fs.readFileSync('web/static/i18n/en-US.json', 'utf8'));

test('Eino token usage summary is rendered as a first-class timeline event', () => {
    assert.match(monitor, /case 'eino_usage_summary'/);
    assert.match(monitor, /formatEinoUsageSummaryTitle\(d\)/);
    assert.match(monitor, /formatEinoUsageSummaryMessage\(d\)/);
    assert.match(monitor, /addTimelineItem\(timeline, 'eino_usage_summary'/);
    assert.match(styles, /\.timeline-item-eino_usage_summary/);
    assert.match(styles, /html\[data-theme="dark"\] \.timeline-item-eino_usage_summary/);
});

test('Eino token usage summary is visible in WebShell live and restored timelines', () => {
    assert.match(webshell, /_et === 'eino_usage_summary'/);
    assert.match(webshell, /appendTimelineItem\('eino_usage_summary'/);
    assert.match(webshell, /function formatWebshellEinoUsageSummaryTitle/);
    assert.match(webshell, /function formatWebshellEinoUsageSummaryMessage/);
    assert.match(webshell, /eventType === 'eino_usage_summary'/);
    assert.match(styles, /\.webshell-ai-timeline-eino_usage_summary/);
    assert.match(styles, /html\[data-theme="dark"\] \.webshell-ai-timeline-eino_usage_summary/);
});

test('Eino token usage summary supports language refresh', () => {
    assert.match(monitor, /type === 'eino_usage_summary'/);
    assert.match(monitor, /item\.dataset\.modelCalls/);
    assert.match(monitor, /setTimelineItemContentStreamPlain\(contentEl, formatEinoUsageSummaryMessage\(usageData\)\)/);
});

test('Eino resilience and usage timeline labels have en translations', () => {
    [
        'einoModelRetryTitle',
        'einoModelFailoverTitle',
        'einoUsageSummaryTitle',
        'einoUsageModelCalls',
        'einoUsagePromptTokens',
        'einoUsageCompletionTokens',
        'einoUsageTotalTokens',
        'einoUsageCachedTokens',
        'einoUsageReasoningTokens'
    ].forEach((key) => {
        assert.equal(typeof en.chat[key], 'string', `en chat.${key}`);
        assert.ok(en.chat[key].length > 0, `en chat.${key} is empty`);
    });
});
