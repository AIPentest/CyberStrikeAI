// audit-uncovered-context.mjs — 行级 covered 标记漏掉跨行的 t(...) ?: 回退表达式；
// 这里取每个含中文的代码行前后 ±3 行作 covered 判定，剩下的才是真未覆盖站点。
// 用法: node scripts/audit-uncovered-context.mjs [fileFilter]
import fs from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(path.dirname(process.argv[1] ?? '.'), '..');
const JS_DIR = path.join(ROOT, 'web', 'static', 'js');
const TPL_DIR = path.join(ROOT, 'web', 'templates');

const HAN = /[一-鿿]/;
const COVERED = /(?:window\.t\(|\w*[Tt]ranslate\(|\w+T\(|\bdt\(|\bst\(|\btr\(|\b_tf\(|\b_t\(|\bt\(|data-i18n(?:-[\w-]+)?=|applyTranslations|__locale)/;
const T_CALL = /(?:window\.t\(|\w*[Tt]ranslate\w*\(|\w+T\w*\(|\bdt\(|\bst\(|\btr\(|\b_tf\(|\b_t\(|\bt\(|data-i18n(?:-[\w-]+)?=|__locale)/;
// 回退表条目形：'ns.key': '中文' —— 本行即已带键，非裸硬文字。
const KEYED_MAP_ENTRY = /^\s*['"][\w.\-]+['"]\s*:\s*['"][^'"]*[一-鿿]/;
const COMMENT_ONLY = /^\s*(?:\/\/|\*|\/\*)/;
const CONSOLE = /console\.(?:log|warn|error|info|debug)/;

function listFiles(dir, ext) {
    if (!fs.existsSync(dir)) return [];
    return fs.readdirSync(dir, { recursive: true })
        .filter((f) => f.endsWith(ext) && !f.includes('vendor'))
        .map((f) => path.join(dir, f));
}

function scan(file) {
    const lines = fs.readFileSync(file, 'utf8').split('\n');
    const unc = [];
    lines.forEach((line, i) => {
        if (!HAN.test(line)) return;
        if (COMMENT_ONLY.test(line)) return;
        if (CONSOLE.test(line)) return;
        if (KEYED_MAP_ENTRY.test(line)) return; // 已键的回退表条目
        const code = line.split('//')[0];
        if (!HAN.test(code)) return; // 行尾中文注释
        // 走 i18n 呼吗：本行或 ±3 行内
        const hit = lines.slice(Math.max(0, i - 3), Math.min(lines.length, i + 4)).some((l) => T_CALL.test(l));
        if (hit) return;
        // 中文只在字符串字量里吗（不在 key 位置）
        if (!/[['"`]/.test(code)) return;
        unc.push({ line: i + 1, text: line.trim().slice(0, 150) });
    });
    return unc;
}

const filter = process.argv[2];
const rows = [];
for (const f of [...listFiles(JS_DIR, '.js'), ...listFiles(TPL_DIR, '.html')]) {
    if (filter && !f.replace(/\\/g, '/').includes(filter)) continue;
    const unc = scan(f);
    if (unc.length) rows.push({ file: path.relative(ROOT, f).replaceAll('\\', '/'), unc });
}
rows.sort((a, b) => b.unc.length - a.unc.length);
for (const r of rows) {
    console.log(`\n## ${r.file} — ${r.unc.length}`);
    for (const e of r.unc.slice(0, 40)) console.log(`  ${String(e.line).padStart(6)} ${e.text}`);
    if (r.unc.length > 40) console.log(`  … +${r.unc.length - 40} more`);
}
console.log(`\ntotal truly-uncovered: ${rows.reduce((a, r) => a + r.unc.length, 0)}`);
