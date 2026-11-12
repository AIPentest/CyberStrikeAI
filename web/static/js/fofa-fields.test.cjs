const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const info = fs.readFileSync(path.join(__dirname, 'info-collect.js'), 'utf8');
const assets = fs.readFileSync(path.join(__dirname, 'assets.js'), 'utf8');
const c = vm.createContext({ window: {}, URL });
vm.runInContext(info.slice(0, info.indexOf('const infoCollectState')), c);
const providers = vm.runInContext('INFO_COLLECT_PROVIDERS', c);

test('FOFA default, presets, and HTML use supported return fields', () => {
    const supported = new Set('ip,port,protocol,country,country_name,region,city,longitude,latitude,asn,org,host,domain,os,server,icp,title,jarm,header,banner,cert,base_protocol,link,body'.split(','));
    const html = fs.readFileSync(path.join(__dirname, '../../templates/index.html'), 'utf8');
    const initial = html.match(/id="fofa-fields" value="([^"]+)"/)[1];
    assert.equal(initial, providers.fofa.fields);
    for (const fields of [initial, ...providers.fofa.fieldPresets.map(p => p[1])]) {
        for (const field of fields.split(',')) assert.ok(supported.has(field), 'unsupported return field: ' + field);
    }
    assert.ok(providers.quake.fields.includes('location.province_cn'));
});

// Execute the real conversion and its validation helpers, including array rows.
vm.runInContext(assets.slice(assets.indexOf('function assetEditorIsIPv4('), assets.indexOf('function parseAssetEditorTarget(')), c);
vm.runInContext(assets.slice(assets.indexOf('function fofaResultToAsset('), assets.indexOf('async function importFofaAssetsByIndexes(')), c);
for (const [field, provider] of [['region', 'fofa'], ['province', 'fofa'], ['location.province_cn', 'quake']]) {
    for (const array of [false, true]) {
        test(`${provider} ${field} ${array ? 'array' : 'object'} row keeps province on import`, () => {
            c.window.infoCollectState = { currentPayload: { provider } };
            const fields = ['host', 'ip', 'port', field, 'city'];
            const values = ['https://example.com', '192.0.2.1', 443, 'Beijing', 'Beijing'];
            const row = array ? values : Object.fromEntries(fields.map((f, i) => [f, values[i]]));
            const asset = c.fofaResultToAsset(row, fields);
            assert.equal(asset.province, 'Beijing');
            assert.equal(asset.ip, '192.0.2.1');
            assert.equal(asset.port, 443);
            assert.equal(asset.city, 'Beijing');
            assert.equal(asset.source, provider);
        });
    }
}
