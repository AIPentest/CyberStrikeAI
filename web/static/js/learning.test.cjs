const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');
const source = fs.readFileSync('web/static/js/learning.js', 'utf8');

function harness(fetcher) {
    const nodes = [];
    function node(tag) {
        const n = { tag, children: [], isConnected: true, append(...items) { this.children.push(...items); },
            replaceChildren(...items) { this.children = items; }, setAttribute() {}, addEventListener() {},
            showModal() {}, close() {}, remove() { this.isConnected = false; }, focus() {} };
        Object.defineProperty(n, 'innerHTML', { set() { throw new Error('unsafe model HTML'); } });
        nodes.push(n); return n;
    }
    const ctx = { currentConversationId: 'synthetic/id', window: { t: x => x }, document: {
        activeElement: node('button'), body: node('body'), createElement: node
    }, apiFetch: fetcher, Date, encodeURIComponent };
    vm.createContext(ctx); vm.runInContext(source, ctx);
    return { ctx, nodes };
}
test('model content is text and conversation ID is encoded', async () => {
    let url;
    const h = harness(async path => {
        url = path;
        return { ok: true, json: async () => ({ enabled: true, jobs: [{ status: 'drafted', createdAt: 1,
            candidateJson: JSON.stringify({ name: '<img onerror=alert(1)>', skillMarkdown: '<script>bad()</script>', evidenceIds: ['tool'] }) }] }) };
    });
    await h.ctx.window.openLearningDrafts();
    assert.equal(url, '/api/conversations/synthetic%2Fid/learning');
    assert.ok(h.nodes.some(n => n.tag === 'pre' && n.textContent === '<script>bad()</script>'));
});
test('delayed response cannot expose the previous conversation after switching', async () => {
    let release;
    const h = harness(() => new Promise(resolve => { release = resolve; }));
    const pending = h.ctx.window.openLearningDrafts();
    h.ctx.currentConversationId = 'other';
    release({ ok: true, json: async () => ({ enabled: true, jobs: [{ status: 'drafted', reason: 'previous private draft' }] }) });
    await pending;
    assert.ok(!h.nodes.some(n => n.textContent === 'previous private draft'));
    assert.ok(h.nodes.find(n => n.tag === 'dialog').isConnected === false);
});
