import test from 'node:test';
import assert from 'node:assert/strict';
import { dumpYaml, parseYaml } from '../src/pages/settings/lib/yaml.js';

test('generated config preserves quoted list values, mapping keys and multiline text', () => {
    const data = {
        list: ['docs,archived', ' literal text ', '.5', 'alpha: beta', 'first\n\nsecond\n\n'],
        channels: { 'custom: channel': { selector: '1.', prompt: 'First\n\nSecond\n\n' } }
    };
    assert.deepEqual(parseYaml(dumpYaml(data)), data);
});
