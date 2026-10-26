const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');
const source = fs.readFileSync('web/static/js/c2.js', 'utf8');
function harness(overrides = {}, failure) {
    const fields = Object.fromEntries(Object.entries({ name: 'listener', type: 'http', host: '127.0.0.1', port: '8080', ...overrides }).map(([key, value]) => ['c2-listener-' + key, { value, setAttribute(k, v) { this[k] = v; }, focus() { this.focused = true; }, setCustomValidity(v) { this.message = v; }, reportValidity() {} }]));
    const requests = [], messages = [];
    let closed = false;
    const c = vm.createContext({ document: { getElementById: id => fields[id] }, C2: { listeners: [], closeModal() { closed = true; }, loadListeners() {} }, API_BASE: '/api/c2', c2t: key => key, showToast: (message, type) => messages.push({ message, type }), apiRequest: async (...args) => { requests.push(args); if (failure) throw Error(failure); return {}; } });
    vm.runInContext(source.slice(source.indexOf('    function validateListenerForm('), source.indexOf('    C2.startListener =')), c);
    vm.runInContext(source.slice(source.indexOf('    C2.saveListener ='), source.search(/    \/\/ [=]+\r?\n\s*\/\/ 会话管理/)), c);
    return { c, fields, requests, messages, closed: () => closed };
}
test('creation and editing reject invalid fields and identify the field', async () => {
    for (const method of ['createListener', 'saveListener']) for (const values of [{ name: '' }, { port: '0' }, { port: '65536' }, { port: '1.5' }, { port: '80abc' }]) {
        const h = harness(values);
        await h.c.C2[method]('id');
        assert.equal(h.requests.length, 0);
        assert.equal(h.messages.length, 1);
        assert.ok(Object.values(h.fields).some(field => field.focused && field['aria-invalid'] === 'true'));
    }
});
test('valid edit sends PUT and closes only after success', async () => {
    const h = harness();
    await h.c.C2.saveListener('id');
    assert.equal(h.requests[0][0], 'PUT');
    assert.equal(h.requests[0][1], '/api/c2/listeners/id');
    assert.equal(h.requests[0][2].bind_port, 8080);
    assert.equal(h.closed(), true);
});
test('transport failure is visible and keeps editor open', async () => {
    for (const method of ['createListener', 'saveListener']) {
        const h = harness({}, 'offline');
        await h.c.C2[method]('id');
        assert.equal(h.messages.at(-1).message, 'offline');
        assert.equal(h.messages.at(-1).type, 'error');
        assert.equal(h.closed(), false);
    }
});
test('modal allows click delegation while background close is target guarded', () => {
    const html = fs.readFileSync('web/templates/index.html', 'utf8');
    const modal = html.slice(html.indexOf('<div id="c2-modal"'), html.indexOf('<div id="c2-modal"') + 360);
    assert.match(modal, /if\(event.target===this\)C2.closeModal\(\)/);
    assert.doesNotMatch(modal, /stopPropagation/);
    assert.match(source, /case 'listener-save': C2.saveListener\(id\)/);
});
test('delegated edit-save button reaches the actual save function', async () => {
    const h = harness();
    const handlers = {};
    h.c.document.documentElement = { dataset: {} };
    h.c.document.addEventListener = (type, callback) => { handlers[type] = callback; };
    const start = source.indexOf('    function bindC2SafeActionDelegation(');
    const end = source.indexOf('    function runC2SafeAction(', start);
    vm.runInContext(source.slice(start, end), h.c);
    // Use the original action dispatcher, trimming at its next function boundary.
    const dispatchStart = source.indexOf('    function runC2SafeAction(');
    const dispatchEnd = source.indexOf('\n    function ', dispatchStart + 10);
    vm.runInContext(source.slice(dispatchStart, dispatchEnd), h.c);
    const button = { getAttribute: key => ({ 'data-c2-action': 'listener-save', 'data-c2-id': 'listener-id' })[key] };
    handlers.click({ target: { closest: selector => selector === '[data-c2-action]' ? button : null }, preventDefault() {}, stopPropagation() {} });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(h.requests.length, 1);
    assert.equal(h.requests[0][1], '/api/c2/listeners/listener-id');
    assert.equal(h.closed(), true);
});
