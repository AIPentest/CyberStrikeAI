const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function harness(responseStatus = 200) {
    const requests = [];
    const context = {
        Headers, Response, Blob,
        URL: { createObjectURL: () => 'blob:fixture', revokeObjectURL() {} },
        console, Element: class Element {},
        setTimeout() {}, clearTimeout() {}, setInterval() {}, clearInterval() {},
        localStorage: { setItem() {}, removeItem() {}, getItem() { return null; } },
        document: {
            getElementById() { return null; }, querySelector() { return null; },
            querySelectorAll() { return []; }, addEventListener() {},
            createElement() { return { click() {}, style: {} }; },
        },
        async fetch(url, options = {}) {
            requests.push({ url, ...options, headers: new Headers(options.headers) });
            return new Response(JSON.stringify(responseStatus === 401 ? { error: 'unauthorized' } : { ok: true }), { status: responseStatus });
        },
    };
    context.window = context;
    vm.createContext(context);
    // Load the actual application authentication implementation, not a mock apiFetch.
    vm.runInContext(fs.readFileSync('web/static/js/auth.js', 'utf8'), context);
    let source = fs.readFileSync('web/static/js/assessments.js', 'utf8');
    const end = source.lastIndexOf('})();');
    assert.ok(end > 0);
    source = source.slice(0, end) + '\nwindow.assessmentTest = {api, onClick, select(id) {selected=id;task={id};}};\n' + source.slice(end);
    vm.runInContext(source, context);
    vm.runInContext("saveAuth('fixture-session', new Date(Date.now()+60000), {user:{username:'fixture'},permissions:['assessment:read','assessment:write']})", context);
    return { context, requests, workbench: context.assessmentTest };
}

test('assessment listing, options and creation use the real login Bearer transport', async () => {
    const { workbench, requests } = harness();
    await workbench.api('');
    await workbench.api('/options');
    await workbench.api('', 'POST', { name: 'fixture' });
    assert.equal(requests.length, 3);
    for (const r of requests) assert.equal(r.headers.get('Authorization'), 'Bearer fixture-session');
    assert.equal(requests[2].method, 'POST');
    assert.equal(JSON.parse(requests[2].body).name, 'fixture');
});

test('assessment report and metrics downloads carry the login token', async () => {
    const { workbench, requests } = harness();
    workbench.select('fixture-task');
    for (const action of ['report', 'metrics']) {
        const button = { dataset: { assessmentAction: action }, disabled: false };
        await workbench.onClick({ target: { closest: () => button } });
    }
    assert.deepEqual(requests.map(r => r.url), ['/api/assessments/fixture-task/report', '/api/assessments/fixture-task/metrics']);
    for (const r of requests) assert.equal(r.headers.get('Authorization'), 'Bearer fixture-session');
});

test('assessment requests wait for login and use the renewed token', async () => {
    const { context, workbench, requests } = harness();
    vm.runInContext('authTokenExpiry = new Date(0)', context);
    const pending = workbench.api('/options');
    await Promise.resolve();
    assert.equal(requests.length, 0, 'anonymous request sent while waiting for login');
    vm.runInContext("saveAuth('renewed-fixture', new Date(Date.now()+60000)); resolveAuthPromises(true)", context);
    await pending;
    assert.equal(requests[0].headers.get('Authorization'), 'Bearer renewed-fixture');
});

test('assessment unauthorized response invokes normal session invalidation', async () => {
    const { context, workbench } = harness(401);
    await assert.rejects(workbench.api(''));
    assert.equal(vm.runInContext('isTokenValid()', context), false);
});
