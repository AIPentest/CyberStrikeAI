const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');
const contextSource = fs.readFileSync('web/static/js/attack-chain-context.js', 'utf8');
const chatSource = fs.readFileSync('web/static/js/chat.js', 'utf8');

class Element {
    constructor(tagName, sinks) {
        Object.assign(this, { tagName, children: [], listeners: new Map(), attributes: {}, style: {}, textContent: '', hidden: false, open: false, sinks });
        this.classList = { remove() {}, add() {} };
    }
    append(...children) { children.forEach(child => this.appendChild(child)); }
    appendChild(child) { this.children.push(child); child.parent = this; return child; }
    replaceChildren(...children) { this.children.forEach(child => { child.parent = null; }); this.children = []; this.append(...children); }
    remove() { if (this.parent) this.parent.children = this.parent.children.filter(child => child !== this); this.parent = null; }
    addEventListener(type, listener) { this.listeners.set(type, listener); }
    setAttribute(name, value) { this.attributes[name] = value; }
    focus() {}
    select() {}
    set innerHTML(value) { this.sinks.push(String(value)); this.replaceChildren(); this._html = value; }
    get innerHTML() { return this._html || ''; }
}
function deferred() {
    let resolve, reject;
    const promise = new Promise((r, j) => { resolve = r; reject = j; });
    return { promise, resolve, reject };
}
function harness(options = {}) {
    const sinks = [], copied = [], timers = [], listeners = new Map(), ids = new Map();
    for (const id of ['attack-chain-context', 'attack-chain-context-content', 'attack-chain-context-title', 'attack-chain-context-notice', 'attack-chain-container', 'attack-chain-stats', 'attack-chain-modal', 'attack-chain-details']) ids.set(id, new Element('div', sinks));
    ids.get('attack-chain-context').hidden = true;
    const button = new Element('button', sinks), sidebar = new Element('div', sinks), body = new Element('body', sinks);
    const document = {
        body, getElementById: id => ids.get(id), createElement: tag => new Element(tag, sinks),
        addEventListener(type, callback) { const callbacks = listeners.get(type) || []; callbacks.push(callback); listeners.set(type, callbacks); },
        querySelector: selector => selector === '.attack-chain-sidebar' ? sidebar : button,
        execCommand: () => { copied.push(body.children.find(child => child.tagName === 'textarea').value); return options.copyWorks !== false; }
    };
    let language = 'zh-CN';
    const dictionaries = Object.fromEntries(['zh-CN', 'en-US', 'ru-RU'].map(lang => [lang, JSON.parse(fs.readFileSync(`web/static/i18n/${lang}.json`, 'utf8'))]));
    const window = {
        t(key, values) {
            let translated = key.split('.').reduce((value, part) => value && value[part], dictionaries[language]) || key;
            // Mirror real i18next: interpolated values are escaped unless the caller opts out.
            const escape = !(values && values.interpolation && values.interpolation.escapeValue === false);
            Object.entries(values || {}).forEach(([name, value]) => {
                if (name === 'interpolation') return;
                const text = escape
                    ? String(value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
                        .replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/\//g, '&#x2F;')
                    : String(value);
                translated = translated.replace('{{' + name + '}}', text);
            });
            return translated;
        }, attackChainOriginalData: null
    };
    const runtime = vm.createContext({
        document, window, navigator: { clipboard: options.clipboard === false ? undefined : { writeText: async text => { if (options.clipboardRejects) throw Error('denied'); copied.push(text); } } },
        setTimeout: callback => { timers.push(callback); }, console: { error() {}, log() {} },
        closeAppModal() {}, openAppModal() {}, isAppModalOpen: () => false, closeNodeDetails() {},
        alert() {},
        currentAttackChainConversationId: 'A', attackChainCytoscape: null,
        apiFetch: options.apiFetch || (() => { throw Error('unexpected API request'); })
    });
    vm.runInContext(contextSource, runtime);
    vm.runInContext('let attackChainRequestSequence = 0; const attackChainRequestOwners = new Map(); const attackChainLoadingMap = new Map();', runtime);
    for (const [name, nextName] of [
        ['beginAttackChainRequest', '_acBuildNodeIconDataUrl'], ['isAttackChainLoading', 'addAttackChainButton'],
        ['resetAttackChainContext', 'showAttackChain'], ['showAttackChain', 'loadAttackChain'],
        ['loadAttackChain', 'renderAttackChain'], ['renderAttackChain', 'getEdgeNodes'],
        ['updateAttackChainStats', 'closeNodeDetails'], ['closeAttackChainModal', 'refreshAttackChain'],
        ['refreshAttackChain', 'regenerateAttackChain'], ['regenerateAttackChain', '_acEscapeXml'],
        ['exportAttackChain', 'projectFilterT']
    ]) {
        const pattern = name => new RegExp(`(?:async )?function ${name}\\(`);
        const start = chatSource.search(pattern(name)), tail = chatSource.slice(start), end = tail.search(pattern(nextName));
        assert.ok(start >= 0 && end > 0, `actual ${name} function is present`);
        vm.runInContext(tail.slice(0, end), runtime);
    }
    return { runtime, window, ids, button, sinks, copied, timers, body, api: window.CyberStrikeAttackChainContext,
        changeLanguage(next) { language = next; (listeners.get('languagechange') || []).forEach(listener => listener()); },
        entries() { return ids.get('attack-chain-context-content').children; } };
}
const summary = content => ({ id: 'summary-123', provenance: 'system_generated_summary', content });
const chain = content => ({ nodes: [], edges: [], context_summaries: [summary(content)] });
const response = data => ({ ok: true, json: async () => data });

test('summary text and snapshot paths remain inert and copying preserves the exact original', async () => {
    const h = harness();
    const content = '关键线索\n<img src=x onerror=alert(1)>\n[证据](javascript:alert(1))\n/快照/transcript-ab123-时间.txt\nCookie: sample=fixture\n';
    h.api.render(chain(content));
    assert.equal(h.ids.get('attack-chain-context').hidden, false);
    assert.equal(h.ids.get('attack-chain-context').open, false);
    const entry = h.entries()[0];
    assert.equal(entry.children[1].textContent, content);
    assert.deepEqual(entry.children.map(element => element.tagName), ['summary', 'pre', 'button', 'span']);
    assert.deepEqual(h.sinks, [], 'no raw summary content ever reaches an HTML sink');
    await entry.children[2].listeners.get('click')();
    assert.deepEqual(h.copied, [content]);
    assert.equal(entry.children[3].textContent, '已复制');
});

test('only explicit provenance qualifies and empty/new context clears old bodies', () => {
    const h = harness();
    h.api.render({ context_summaries: [summary('trusted'), { content: 'summary-looking user text' }, { provenance: 'skills', content: 'not a summary' }, summary('  ')] });
    assert.equal(h.entries().length, 1);
    h.ids.get('attack-chain-context').open = true;
    h.api.render({ context_summaries: [] });
    assert.equal(h.entries().length, 0);
    assert.equal(h.ids.get('attack-chain-context').hidden, true);
    assert.equal(h.ids.get('attack-chain-context').open, false);
    h.api.render(chain('next conversation'));
    assert.equal(h.entries()[0].children[1].textContent, 'next conversation');
    h.api.clear();
    assert.equal(h.entries().length, 0);
});

test('language changes retain expansion and original content while translating generated labels', () => {
    const h = harness();
    h.api.render(chain('原始正文'));
    const entry = h.entries()[0];
    entry.open = true;
    h.ids.get('attack-chain-context').open = true;
    h.changeLanguage('en-US');
    assert.equal(entry.children[0].textContent, 'Context summary 1');
    assert.equal(entry.children[2].textContent, 'Copy full summary');
    assert.equal(entry.children[1].textContent, '原始正文');
    assert.equal(entry.open, true);
    assert.equal(h.ids.get('attack-chain-context').open, true);
});

test('re-rendering the same conversation keeps the card expanded, clear still collapses', () => {
    const h = harness();
    h.api.render(chain('first pass'));
    h.ids.get('attack-chain-context').open = true;
    h.api.render(chain('second pass'));
    assert.equal(h.ids.get('attack-chain-context').open, true);
    assert.equal(h.entries()[0].children[1].textContent, 'second pass');
    h.api.clear();
    assert.equal(h.ids.get('attack-chain-context').open, false);
});

test('clipboard restrictions use the text fallback and failures retain selectable content', async () => {
    for (const options of [{ clipboard: false }, { clipboardRejects: true }, { clipboard: false, copyWorks: false }]) {
        const h = harness(options);
        h.api.render(chain('full evidence path'));
        const entry = h.entries()[0];
        await entry.children[2].listeners.get('click')();
        assert.deepEqual(h.copied, ['full evidence path']);
        assert.equal(h.body.children.length, 0);
        assert.equal(entry.children[2].disabled, false);
        assert.equal(entry.children[1].textContent, 'full evidence path');
        assert.equal(entry.children[3].textContent, options.copyWorks === false ? '复制失败，可选中文本手动复制。' : '已复制');
    }
});

test('an empty graph still displays its context without changing nodes or statistics', () => {
    const h = harness(), data = chain('summary without graph nodes');
    let destroyed = 0;
    h.runtime.attackChainCytoscape = { destroy: () => { destroyed += 1; } };
    h.runtime.renderAttackChain(data);
    h.runtime.updateAttackChainStats(data);
    assert.equal(h.entries()[0].children[1].textContent, 'summary without graph nodes');
    assert.equal(h.ids.get('attack-chain-stats').textContent, '节点: 0 | 边: 0');
    assert.equal(data.nodes.length, 0);
    assert.equal(h.window.attackChainOriginalData, data);
    assert.equal(destroyed, 1);
    assert.equal(h.runtime.attackChainCytoscape, null, 'old graph is unavailable to filters and exports');
    const alerts = [];
    h.runtime.alert = message => alerts.push(message);
    h.runtime.exportAttackChain('svg');
    assert.equal(alerts.length, 1);
    assert.equal(h.timers.length, 0, 'export cannot schedule an old graph download');
});

test('late 409/errors cannot replace another conversation or unlock its request', async () => {
    for (const operation of ['loadAttackChain', 'regenerateAttackChain']) for (const result of ['409', 'error', 'json-error']) {
        const request = deferred(), h = harness({ apiFetch: () => request.promise });
        const pending = operation === 'loadAttackChain' ? h.runtime[operation]('A') : h.runtime[operation]();
        h.runtime.currentAttackChainConversationId = 'B';
        h.api.render(chain('B evidence'));
        h.button.disabled = true;
        const marker = h.ids.get('attack-chain-container').innerHTML;
        if (result === 'error') request.reject(Error('<img onerror=alert(1)>'));
        else if (result === 'json-error') request.resolve({ ok: false, status: 500, json: async () => ({ error: 'old error' }) });
        else request.resolve({ ok: false, status: 409 });
        await pending;
        assert.equal(h.entries()[0].children[1].textContent, 'B evidence', `${operation}/${result}`);
        assert.equal(h.ids.get('attack-chain-container').innerHTML, marker);
        assert.equal(h.button.disabled, true);
        assert.equal(h.timers.length, 0);
    }
});

test('same-conversation requests keep the latest response and old completion cannot clear loading', async () => {
    const requests = [deferred(), deferred()];
    let index = 0;
    const h = harness({ apiFetch: () => requests[index++].promise });
    const older = h.runtime.loadAttackChain('A');
    h.runtime.setAttackChainLoading('A', false);
    const newer = h.runtime.loadAttackChain('A');
    requests[0].resolve(response(chain('old evidence')));
    await older;
    assert.equal(h.runtime.isAttackChainLoading('A'), true);
    requests[1].resolve(response(chain('new evidence')));
    await newer;
    assert.equal(h.entries()[0].children[1].textContent, 'new evidence');
    assert.equal(h.runtime.isAttackChainLoading('A'), false);
    const reverse = [deferred(), deferred()];
    index = 0;
    h.runtime.apiFetch = () => reverse[index++].promise;
    const slow = h.runtime.loadAttackChain('A');
    h.runtime.setAttackChainLoading('A', false);
    const fast = h.runtime.loadAttackChain('A');
    reverse[1].resolve(response(chain('newest evidence')));
    await fast;
    reverse[0].resolve(response(chain('stale evidence')));
    await slow;
    assert.equal(h.entries()[0].children[1].textContent, 'newest evidence');
});

test('current errors clear context and display server text without HTML execution', async () => {
    const h = harness({ apiFetch: async () => ({ ok: false, status: 500, json: async () => ({ error: '<img src=x onerror=alert(1)>' }) }) });
    h.api.render(chain('old evidence'));
    let destroyed = 0;
    h.runtime.attackChainCytoscape = { destroy: () => { destroyed += 1; } };
    await h.runtime.loadAttackChain('A');
    assert.equal(h.entries().length, 0);
    assert.equal(h.ids.get('attack-chain-container').children[0].textContent.includes('<img src=x onerror=alert(1)>'), true);
    assert.equal(h.sinks.some(html => html.includes('onerror')), false);
    assert.equal(destroyed, 1);
    assert.equal(h.runtime.attackChainCytoscape, null);
    const alerts = [];
    h.runtime.alert = message => alerts.push(message);
    h.runtime.exportAttackChain('svg');
    assert.equal(alerts.length, 1);
    assert.equal(h.timers.length, 0);
});

test('failure text stays readable instead of showing i18next escape entities', async () => {
    const upstream = '生成攻击链失败: Post "https://api.example.com/v1/chat/completions": context deadline exceeded';
    const h = harness({ apiFetch: async () => ({ ok: false, status: 500, json: async () => ({ error: upstream }) }) });
    await h.runtime.loadAttackChain('A');
    const shown = h.ids.get('attack-chain-container').children[0].textContent;
    assert.equal(shown.includes(upstream), true);
    assert.equal(/&#x2F;|&quot;|&amp;|&#39;/.test(shown), false);
    assert.equal(h.sinks.some(html => html.includes('api.example.com')), false);
});

test('regenerate failure reports its own message key unescaped', async () => {
    const upstream = 'AI生成失败: Post "https://api.example.com/v1/chat/completions": EOF';
    const h = harness({ apiFetch: async () => ({ ok: false, status: 500, json: async () => ({ error: upstream }) }) });
    await h.runtime.regenerateAttackChain();
    const shown = h.ids.get('attack-chain-container').children[0].textContent;
    assert.equal(shown.startsWith('重新生成失败: '), true);
    assert.equal(shown.includes(upstream), true);
    assert.equal(h.sinks.some(html => html.includes('api.example.com')), false);
});

test('regenerate 409 refreshes after completion while superseded timers stay inert', async () => {
    const h = harness({ apiFetch: async () => ({ ok: false, status: 409 }) });
    let refreshes = 0;
    h.runtime.refreshAttackChain = () => { refreshes += 1; };
    await h.runtime.regenerateAttackChain();
    assert.equal(h.runtime.isAttackChainLoading('A'), false);
    h.timers[0]();
    assert.equal(refreshes, 1);
    h.runtime.beginAttackChainRequest('A');
    h.timers[0]();
    assert.equal(refreshes, 1);
});

test('closing/reopening pending context clears old bodies and invalidates old responses', async () => {
    const requests = [deferred(), deferred()];
    let index = 0;
    const h = harness({ apiFetch: () => requests[index++].promise });
    const oldRequest = h.runtime.loadAttackChain('A');
    h.api.render(chain('old context'));
    h.runtime.closeAttackChainModal();
    assert.equal(h.entries().length, 0);
    assert.equal(h.window.attackChainOriginalData, null);
    const reopened = h.runtime.showAttackChain('A');
    assert.equal(index, 2);
    requests[0].resolve(response(chain('stale closed context')));
    await oldRequest;
    assert.equal(h.entries().length, 0);
    assert.equal(h.runtime.isAttackChainLoading('A'), true);
    requests[1].resolve(response(chain('reopened context')));
    await reopened;
    assert.equal(h.entries()[0].children[1].textContent, 'reopened context');
});
