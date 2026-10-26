const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const assert = require('node:assert/strict');

/* 全 JS 文键契约：web/static/js/*.js 中所有走 translate-helper 的静态键
   必须在三种语言里都存在——缺失会让英文/俄文 UI 裸看中文回退值。

   Helper 前缀形（函数体里 fullKey = '<ns>.' + key）会把短键加到自层
   命名空间；这里把每个 JS 文件的 helper→prefix 从源件抽出来，再把短键
   前加上后查。动态拼接前缀（键以 . 或 _ 结尾）属各文件自层，不在契约。 */

const LOCALES = ['zh-CN', 'en-US', 'ru-RU'];
const JS_DIR = path.join('web', 'static', 'js');

function lookup(dict, key) {
    return key.split('.').reduce((acc, part) => (acc && typeof acc === 'object' ? acc[part] : undefined), dict);
}

// 英文复数形：t('dashboard.x', {count}) 在 en-US 萋 _one/_other 后缀，
// zh/ru 单一复数语言用无后缀键；两形都视为满足该键。
function lookupPlural(dict, key) {
    const plain = lookup(dict, key);
    if (typeof plain === 'string' && plain.trim()) return plain;
    const one = lookup(dict, key + '_one');
    const other = lookup(dict, key + '_other');
    if (typeof one === 'string' && one.trim() && typeof other === 'string' && other.trim()) return one;
    return undefined;
}

// 从一个文件源抽 helper→prefix：function xxxT(…) { … '<ns>.' + key … }
function helperPrefixes(source) {
    const prefixes = {};
    const re = /function\s+([a-zA-Z]+T)\(([^)]*)\)\s*\{([\s\S]*?)(?:\n\}|\n(?=\s*function))/g;
    let m;
    while ((m = re.exec(source))) {
        const name = m[1], body = m[3];
        const pm = body.match(/'([[\w.]+\.)'\s*\+\s*(?:opts\.)?key/);
        if (pm) prefixes[name] = pm[1];
    }
    // 不止函数声明：const xxxT = … 也会前缀（无此形，暂补）
    return prefixes;
}

function referencedKeys(source) {
    const prefixes = helperPrefixes(source);
    const patterns = [
        /window\.t\(\s*'([^']+)'/g,
        /\bdt\(\s*'([^']+)'/g,
        /\b([a-zA-Z]+T)\(\s*'([^']+)'/g,
        /conversationSidebarText\(\s*'([^']+)'/g,
    ];
    const keys = new Set();
    for (const re of patterns) {
        let m;
        while ((m = re.exec(source))) {
            let k = m[1];
            // xxxT('key') 情况：m[2] 是键；window.t('key') 情况，m[1] 即键
            if (m[2] !== undefined) {
                const helper = m[1];
                k = prefixes[helper] ? prefixes[helper] + m[2] : m[2];
            }
            if (k.endsWith('.') || /[_$]$/.test(k)) continue; // 动态前缀/动态拼接
            keys.add(k);
        }
    }
    return keys;
}

const files = fs.readdirSync(JS_DIR).filter(f => f.endsWith('.js') && !f.endsWith('.test.js'));

test('至少抽到一批键，防止正性整体失配后空集通过', () => {
    let total = 0;
    for (const f of files) total += referencedKeys(fs.readFileSync(path.join(JS_DIR, f), 'utf8')).size;
    assert.ok(total >= 300, `expected >=300 referenced keys, got ${total}`);
});

for (const locale of LOCALES) {
    test(`${locale} 覆盖 JS 引用的每一个静态键`, () => {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        const missing = [];
        for (const f of files) {
            const keys = referencedKeys(fs.readFileSync(path.join(JS_DIR, f), 'utf8'));
            for (const k of keys) {
                if (typeof lookupPlural(dict, k) !== 'string' || !lookupPlural(dict, k).trim()) missing.push(`${f}: ${k}`);
            }
        }
        assert.deepEqual(missing, [], `${locale} 缺缺少可用译文: ${missing.slice(0, 20).join(', ')}`);
    });
}
