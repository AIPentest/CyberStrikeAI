const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

/* 桌面端 chat.js 文键契约：所有走 chatTranslate/window.t 的键
   必须在三种语言里都存在，且英文文本必须给出（用户在英文 UI 下
   不许裸看中文回退值）。 */

const LOCALES = ['zh-CN', 'en-US', 'ru-RU'];
const js = fs.readFileSync('web/static/js/chat.js', 'utf8');

function lookup(dict, key) {
    return key.split('.').reduce((acc, part) => (acc && typeof acc === 'object' ? acc[part] : undefined), dict);
}

// Key reference shapes actually used in chat.js:
//   chatTranslate('chat.x', ...) / window.t('chat.x') / window.t('chat.x', opts)
//   conversationSidebarText('chat.x', ...) — returns t() when available.
function referencedKeys(source) {
    const patterns = [
        /chatTranslate\(\s*'([^']+)'/g,
        /conversationSidebarText\(\s*'([^']+)'/g,
        /window\.t\(\s*'([^']+)'/g,
    ];
    const keys = new Set();
    for (const re of patterns) {
        let m;
        while ((m = re.exec(source))) keys.add(m[1]);
    }
    // dynamic-prefix keys (e.g. 'chat.einoRunRetryKind_' + key) are built at
    // runtime; the prefix itself is not a key, drop it.
    return new Set([...keys].filter(k => !/[_$]$/.test(k)));
}

const keys = referencedKeys(js);

test('至少抽到一批键，防止正性整体失配后空集通过', () => {
    assert.ok(keys.size >= 30, `expected >=30 referenced keys, got ${keys.size}`);
});

for (const locale of LOCALES) {
    test(`${locale} 覆盖 chat.js 引用的每一个键`, () => {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        const missing = [...keys].filter(k => typeof lookup(dict, k) !== 'string' || !lookup(dict, k).trim());
        assert.deepEqual(missing, [], `${locale} 缺缺少可用译文: ${missing.join(', ')}`);
    });
}

test('chat 命名空间键集在三种语言里完全一致', () => {
    const sets = LOCALES.map(l => {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${l}.json`, 'utf8'));
        return dict.chat && typeof dict.chat === 'object'
            ? Object.keys(dict.chat).sort()
            : [];
    });
    assert.deepEqual(sets[1], sets[0], 'en-US 与 zh-CN 的 chat.* 键集不同');
    assert.deepEqual(sets[2], sets[0], 'ru-RU 与 zh-CN 的 chat.* 键集不同');
});
