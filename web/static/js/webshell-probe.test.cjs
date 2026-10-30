const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');
const source = fs.readFileSync('web/static/js/webshell.js', 'utf8');
function harness(reply) {
    const requests = [];
    const button = {};
    const ctx = {
        webshellConnections: Array.from({length: 6}, (_, i) => ({id: `conn-${i}`, url: 'http://example.invalid', password: '********'})),
        webshellBatchProbeRunning: false, webshellProbeStatusById: {},
        wsT: key => key, renderWebshellList() {},
        document: {getElementById: id => id === 'webshell-batch-probe-btn' ? button : {value: 'unrelated-edit-id'}},
        webshellConnEncoding: () => 'utf-8', webshellConnOS: () => 'linux',
        apiFetch: (url, options) => {
            const body = JSON.parse(options.body);
            requests.push(body);
            return reply(body);
        }
    };
    vm.createContext(ctx);
    for (const name of ['buildWebshellProbeToken', 'buildWebshellProbeCommand', 'isWebshellProbeOutputMatched', 'probeWebshellConnection', 'updateWebshellBatchProbeButton', 'runBatchProbeWebshellConnections']) {
        const start = source.indexOf(`function ${name}(`);
        const end = source.indexOf('\nfunction ', start + 1);
        assert.ok(start >= 0 && end > start);
        vm.runInContext(source.slice(start, end), ctx);
    }
    return {ctx, requests, button};
}
function success(body) {
    return Promise.resolve({json: async () => ({ok: true, output: body.command.replace(/^echo /, '')})});
}
test('saved masked credentials use each connection ID and complete a batch, then allow retry', async () => {
    const {ctx, requests, button} = harness(success);
    await ctx.runBatchProbeWebshellConnections();
    assert.deepEqual(requests.map(r => r.connection_id).sort(), Array.from({length: 6}, (_, i) => `conn-${i}`));
    assert.ok(Object.values(ctx.webshellProbeStatusById).every(s => s.state === 'ok'));
    assert.equal(ctx.webshellBatchProbeRunning, false);
    assert.equal(button.disabled, false);
    await ctx.runBatchProbeWebshellConnections();
    assert.equal(requests.length, 12);
});
for (const failure of ['sync', 'network', 'json', 'server', 'mismatch']) {
    test(`${failure} failure settles offline, completes other connections and restores button`, async () => {
        const {ctx, button, requests} = harness(body => {
            if (body.connection_id !== 'conn-0') return success(body);
            if (failure === 'sync') throw new Error('sync failure');
            if (failure === 'network') return Promise.reject(new Error('network failure'));
            if (failure === 'json') return Promise.resolve({json: () => Promise.reject(new Error('invalid JSON'))});
            return Promise.resolve({json: async () => ({ok: failure === 'mismatch', output: '', error: 'probe failed'})});
        });
        await ctx.runBatchProbeWebshellConnections();
        assert.equal(requests.length, 6);
        assert.equal(ctx.webshellProbeStatusById['conn-0'].state, 'fail');
        assert.ok(ctx.webshellProbeStatusById['conn-0'].message);
        assert.equal(Object.values(ctx.webshellProbeStatusById).filter(s => s.state === 'ok').length, 5);
        assert.equal(ctx.webshellBatchProbeRunning, false);
        assert.equal(button.disabled, false);
    });
}
