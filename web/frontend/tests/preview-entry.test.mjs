import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import test from 'node:test';

const web = new URL('../../', import.meta.url);

test('classic header links to the built Semi workspace', () => {
    const classic = readFileSync(new URL('templates/index.html', web), 'utf8');
    assert.match(classic, /<a [^>]*href="\/static\/app\/"[^>]*data-i18n="header.semiPreview"/);

    const preview = readFileSync(new URL('static/app/index.html', web), 'utf8');
    assert.match(preview, /<div id="root">/);
    const assets = [...preview.matchAll(/(?:src|href)="\/static\/(app\/assets\/[^"]+)"/g)];
    assert.ok(assets.some(([, path]) => path.endsWith('.js')), 'preview index references a JavaScript bundle');
    assert.ok(assets.some(([, path]) => path.endsWith('.css')), 'preview index references bundled styles');
    for (const [, path] of assets) {
        assert.ok(statSync(new URL(`static/${path}`, web)).size > 0, `Built asset is present: ${path}`);
    }
});
