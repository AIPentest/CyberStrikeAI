const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const monitor = fs.readFileSync('web/static/js/monitor.js', 'utf8');
const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');
const reason = '工具调用已被安全规则拦截：识别到 example.gov，禁止访问。\n规则: 政府网站保护';
const englishReason = 'The tool call was blocked by a security rule: Identified example.gov, access forbidden.\nRule: Government sites protection (government-domains)\nMatched content: "example.gov"';

function sourceFunction(source, name) {
    const start = source.indexOf(`function ${name}(`);
    assert.notEqual(start, -1, name);
    const rest = source.slice(start);
    const next = rest.slice(1).search(/\n(?:async )?function /);
    return (next === -1 ? rest : rest.slice(0, next + 1)).split(/\nwindow\.|\nconst toolCallDetailStateByItemId/)[0];
}

class Element {
    constructor() {
        this.dataset = {};
        this.children = [];
        this.className = '';
        this.classList = {
            contains: (value) => this.className.split(' ').includes(value),
            add: (...values) => { this.className = [...new Set([...this.className.split(' ').filter(Boolean), ...values])].join(' '); },
            remove: (...values) => { this.className = this.className.split(' ').filter((value) => !values.includes(value)).join(' '); }
        };
    }
    set innerHTML(value) {
        this.html = value;
        this.title = new Element();
        this.title.className = 'timeline-item-title';
    }
    get innerHTML() { return this.html; }
    appendChild(child) { child.parent = this; this.children.push(child); }
    remove() { if (this.parent) this.parent.children = this.parent.children.filter((child) => child !== this); }
    querySelector(selector) {
        if (selector === '.timeline-item-title') return this.title || null;
        if (selector === '.tool-status-badge') return this.children.find((child) => child.classList.contains('tool-status-badge')) || null;
        return null;
    }
}

function runtime() {
    const ctx = {
        window: {}, document: { createElement: () => new Element() },
        toolCallDetailStateByItemId: new Map(),
        updateToolDetailToggleLabel() {}, applyEinoTimelineRole() {}, pruneLiveTimelineIfNeeded() {},
        getCurrentTimeLocale: () => 'en-US', getTimeFormatOptions: () => ({}),
        escapeHtml: (value) => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;'),
    };
    const funcs = ['collectToolResultTextParts', 'isToolGuardBlockedResult', 'getToolExecutionDisplayStatus',
        'getToolResultDisplayState', 'toolDisplayStatusFromState', 'getBackgroundRunningToolLabel',
        'getToolCallStatusPresentation', 'applyToolCallStatus', 'parseToolCallArgsFromData', 'toolCallArgsEmpty',
        'setToolCallDetailState', 'mergeToolResultIntoCallItem', 'coalesceProcessDetailsToolPairs',
        'buildToolResultSectionHtml', 'addTimelineItem'];
    vm.createContext(ctx);
    vm.runInContext(funcs.map((name) => sourceFunction(monitor, name)).join('\n'), ctx);
    ctx.window.getToolExecutionDisplayStatus = ctx.getToolExecutionDisplayStatus;
    vm.runInContext(['normalizeToolExecutionSummary', 'getToolExecutionStatusLabel', 'formatMCPResultJsonForDisplay'].map((name) => sourceFunction(chat, name)).join('\n'), ctx);
    return ctx;
}

test('structured block markers have priority over generic failure and running states', () => {
    const ctx = runtime();
    for (const payload of [
        { blocked: true, success: false, isError: true },
        { status: 'blocked', isError: true },
        { success: false, result: { blocked: true, isError: true, content: [] } },
        { success: false, result: JSON.stringify({ _meta: { 'cyberstrike.ai/blocked': true }, isError: true, content: [] }) },
        { blocked: true, displayStatus: 'background_running', success: true },
    ]) {
        const state = ctx.getToolResultDisplayState(payload);
        assert.equal(state.kind, 'blocked');
        assert.equal(state.success, false);
        assert.equal(ctx.toolDisplayStatusFromState(state), 'blocked');
    }
});

test('legacy guard failures are recognized in raw, nested, serialized and deferred history results', () => {
    const ctx = runtime();
    for (const result of [reason, englishReason, { isError: true, content: [{ type: 'text', text: reason }] }, JSON.stringify({ isError: true, content: [{ type: 'text', text: englishReason }] })]) {
        assert.equal(ctx.getToolResultDisplayState({ result, success: false }).kind, 'blocked');
    }
    assert.equal(ctx.getToolResultDisplayState({ resultPreview: reason, success: false, _payloadDeferred: true }).kind, 'blocked');
    assert.equal(ctx.getToolResultDisplayState({ success: false }, { rawText: reason }).kind, 'blocked');
});

test('quoted mentions, prefix lookalikes and explicitly successful output stay ordinary results', () => {
    const ctx = runtime();
    for (const result of ['示例：' + reason, '"' + reason + '"', '工具调用已被安全规则拦截说明文档', 'The tool call was blocked by a security ruleset is a doc phrase']) {
        assert.equal(ctx.getToolResultDisplayState({ result, success: false }).kind, 'error');
    }
    for (const data of [{ success: true, result: reason }, { isError: false, content: [{ text: reason }] }, { status: 'completed', result: reason }]) {
        assert.equal(ctx.getToolResultDisplayState(data, { rawText: reason }).kind, 'success');
    }
    assert.equal(ctx.getToolResultDisplayState({ success: false, result: 'connection refused' }).kind, 'error');
});

test('live merge replaces a red failure badge with the distinct block badge and keeps the reason', () => {
    const ctx = runtime();
    const timeline = new Element();
    ctx.addTimelineItem(timeline, 'tool_call', { title: 'http_request', data: { toolName: 'http_request' }, toolStatus: 'failed' });
    const item = timeline.children[0];
    assert.equal(item.classList.contains('tool-call-failed'), true);
    ctx.mergeToolResultIntoCallItem(item, { blocked: true, success: false, result: reason });
    assert.equal(item.dataset.toolDisplayStatus, 'blocked');
    assert.equal(item.dataset.toolSuccess, '0');
    assert.equal(item.classList.contains('tool-call-blocked'), true);
    assert.equal(item.classList.contains('tool-call-failed'), false);
    assert.equal(item.title.children.length, 1);
    assert.match(item.title.children[0].textContent, /已拦截/);
    assert.equal(ctx.toolCallDetailStateByItemId.get(item.id).rawText, reason);
});

test('refresh coalescing preserves blocks even when the old execution summary says failed', () => {
    const ctx = runtime();
    const details = ctx.coalesceProcessDetailsToolPairs([
        { id: 'call', eventType: 'tool_call', data: { toolCallId: 'id', toolName: 'http_request' } },
        { id: 'result', eventType: 'tool_result', data: { toolCallId: 'id', success: false, result: reason } }
    ]);
    assert.equal(details.length, 1);
    const timeline = new Element();
    ctx.addTimelineItem(timeline, 'tool_call', { data: details[0].data, toolStatus: 'failed' });
    const item = timeline.children[0];
    assert.equal(item.dataset.toolDisplayStatus, 'blocked');
    assert.match(item.title.children[0].className, /tool-status-blocked/);
    assert.equal(item.classList.contains('tool-call-failed'), false);
    const resultData = ctx.toolCallDetailStateByItemId.get(item.id).resultData;
    assert.match(ctx.buildToolResultSectionHtml(resultData), /tool-result-section blocked/);
    assert.doesNotMatch(ctx.buildToolResultSectionHtml(resultData), /tool-result-section error/);
});

test('execution summary buttons and raw detail preserve the separate blocked status', () => {
    const ctx = runtime();
    assert.equal(ctx.normalizeToolExecutionSummary({ toolName: 'http_request', status: 'blocked' }).status, 'blocked');
    assert.equal(ctx.normalizeToolExecutionSummary({ toolName: 'http_request', status: 'failed', error: reason }).status, 'blocked');
    assert.equal(ctx.getToolExecutionStatusLabel('blocked'), '已拦截');
    assert.equal(JSON.parse(ctx.formatMCPResultJsonForDisplay({ blocked: true, isError: true, content: [] })).blocked, true);
});
