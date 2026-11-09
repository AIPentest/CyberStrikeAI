#!/usr/bin/env node
// 把 internal/handler/*.go 中的 c.JSON(..., gin.H{"error": "中文"}) 转为
// respondError(c, 状态, "中文", "English")，en 翻译取自 i18n-error-map.json。
// 仅处理「纯字符串字量」的位（Sprintf / err.Error() 不在此轮）。
import fs from 'fs';
import path from 'path';

const ROOT = path.resolve(process.argv[2] || '.');
const MAP = JSON.parse(fs.readFileSync(path.join(ROOT, 'scripts/i18n-error-map.json'), 'utf8'));

const files = fs.readdirSync(path.join(ROOT, 'internal/handler')).filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
let total = 0, byFile = {};
for (const f of files) {
    const p = path.join(ROOT, 'internal/handler', f);
    const src = fs.readFileSync(p, 'utf8');
    const re = /c\.JSON\((http\.Status\w+),\s*gin\.H\{"error":\s*("(?:[^"\\]|\\.)*")\}\)/g;
    let out = src, count = 0;
    // 逐位收集再替换：replace 使用回调，需在第一次匹配就替换
    out = src.replace(re, (match, status, zhLit) => {
        const zh = JSON.parse(zhLit);
        if (!/[一-鿿]/.test(zh)) return match; // 非中文，保留原样
        const en = MAP[zh];
        if (!en) { console.error('NO TRANSLATION for', zh, 'in', f); return match; }
        count++;
        return `respondError(c, ${status}, ${zhLit}, ${JSON.stringify(en)})`;
    });
    if (count) {
        fs.writeFileSync(p, out);
        byFile[f] = count; total += count;
    }
}
console.log('transformed sites:', total);
console.log(JSON.stringify(byFile));
