const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

/* 移动端注入层的文案键契约：mobile-ui.js 引用的每个 i18n 键必须在三种语言里都存在，
   否则手机上会出现「点开关却看到 mobile.openNav」这类内部键名裸露。
   遍历域 = 传播域：凡是能写出 data-i18n 的三种入口都要抽，缺一个入口就会漏检。 */

const LOCALES = ['zh-CN', 'en-US', 'ru-RU'];
const js = fs.readFileSync('web/static/js/mobile-ui.js', 'utf8');

function lookup(dict, key) {
    return key.split('.').reduce((acc, part) => (acc && typeof acc === 'object' ? acc[part] : undefined), dict);
}

function referencedKeys(source) {
    const patterns = [
        /label\(\s*[A-Za-z_$][\w$]*\s*,\s*'([^']+)'/g,
        /ariaLabel\(\s*[A-Za-z_$][\w$]*\s*,\s*'([^']+)'/g,
        /setAttrI18n\(\s*[A-Za-z_$][\w$]*\s*,\s*'[^']+'\s*,\s*'([^']+)'/g,
        /translate\(\s*'([^']+)'/g,
        /buildSheetItem\(\s*'([^']+)'/g,
        /labelKey:\s*'([^']+)'/g,
    ];
    const keys = new Set();
    for (const re of patterns) {
        let m;
        while ((m = re.exec(source))) keys.add(m[1]);
    }
    return keys;
}

const keys = referencedKeys(js);

test('至少抽到一批键，防止正则整体失配后空集通过', () => {
    assert.ok(keys.size >= 16, `expected >=20 referenced keys, got ${keys.size}`);
    assert.ok(keys.has('mobile.conversations'), 'tab labels must go through i18n');
});

for (const locale of LOCALES) {
    test(`${locale} 覆盖移动端引用的每一个键`, () => {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        const missing = [...keys].filter(k => typeof lookup(dict, k) !== 'string' || !lookup(dict, k).trim());
        assert.deepEqual(missing, [], `${locale} 缺少可用译文: ${missing.join(', ')}`);
    });
}

test('三种语言的 mobile 命名空间键集完全一致', () => {
    const sets = LOCALES.map(l => Object.keys(JSON.parse(fs.readFileSync(`web/static/i18n/${l}.json`, 'utf8')).mobile).sort());
    assert.deepEqual(sets[1], sets[0], 'en-US 与 zh-CN 键集不同');
    assert.deepEqual(sets[2], sets[0], 'ru-RU 与 zh-CN 键集不同');
});

test('带插值的文案在三种语言里都保留占位符', () => {
    for (const locale of LOCALES) {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        assert.match(lookup(dict, 'mobile.themeNow'), /\{\{\s*name\s*\}\}/, `${locale} 的 themeNow 丢了 {{name}}`);
    }
});

test('语言与主题动作复用桌面端既有键，不另立一套', () => {
    assert.ok(keys.has('header.apiDocs'), 'API 文档应复用 header.apiDocs');
    assert.ok(keys.has('header.github'), 'GitHub 应复用 header.github 而不是另立键');
    assert.ok(keys.has('nav.dashboard'), '底部标签应复用 nav.* 分组名');
});

test('注入层不再残留会直接显示给用户的硬编码中文', () => {
    /* 判据与函数名无关：helper 区之外不许出现任何"裸写用户可见文本"的 API。
       helper 区 = 从文案注释块起到 toast 定义前，四件套 label/translate/setAttrI18n/ariaLabel 都在里面。 */
    const from = js.indexOf('/* 注入节点的文案');
    const to = js.indexOf('function toast(msg, ms)');
    assert.ok(from !== -1 && to > from, '找不到文案 helper 区，说明 helper 被搬散或改名');
    const outside = js.slice(0, from) + js.slice(to);

    const bypasses = [
        /toast\(\s*'[^']*[一-鿿][^']*'/,
        /setAttribute\(\s*'aria-label'\s*,/,
        /setAttribute\(\s*'placeholder'\s*,/,
        /\.placeholder\s*=\s*'[^']*[一-鿿]/,
        /\.textContent\s*=\s*'[^']*[一-鿿]/,
        /el\(\s*'[a-zA-Z]+'\s*,\s*'[^']*'\s*,\s*'[^']*[一-鿿]/,
    ];
    for (const re of bypasses) {
        const hit = outside.match(re);
        assert.equal(hit, null, `绕过 i18n 通道的写法: ${hit && hit[0]}`);
    }
});
