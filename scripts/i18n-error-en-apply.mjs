#!/usr/bin/env node
// 把 internal/handler/*.go 中残留的纯英文错误位转为 respondError/respondErrorf。
// 处理三种形：
//   1. 单行 c.JSON(status, gin.H{"error": "<en>"})          → respondError(c, status, "zh", "en")
//   2. 多行单键 c.JSON(status, gin.H{\n "error": "<en>",\n}) → respondError(c, status, "zh", "en")
//   3. 拼接 c.JSON(status, gin.H{"error": "<prefix>" + x.Error()}) → respondErrorf(c, status, "zh prefix%s", "en prefix%s", x.Error())
// 中文翻译取自 scripts/i18n-error-en-map.json（en→zh）。已含中文或不在 map 的位跳过并列名。
// 用法： node scripts/i18n-error-en-apply.mjs [rootDir]
import fs from 'fs';
import path from 'path';

const ROOT = path.resolve(process.argv[2] ?? '.');
const MAP = JSON.parse(fs.readFileSync(path.join(ROOT, 'scripts/i18n-error-en-map.json'), 'utf8'));
const cjk = /[一-鿿]/;

const files = fs.readdirSync(path.join(ROOT, 'internal/handler'))
  .filter(f => f.endsWith('.go') && !f.includes('test'));

const missing = new Set();
const byFile = {};
let total = 0;

for (const f of files) {
  const p = path.join(ROOT, 'internal/handler', f);
  const src = fs.readFileSync(p, 'utf8');
  let out = src;
  let count = 0;

  // 形 3：prefix + err.Error() 拼接（最特殊，先做）
  const re3 = /(c\.JSON\()((?:http\.Status\w+|\d{3}))(,\s*gin\.H\{"error":\s*)("(?:[^"\\]|\\.)*")(\s*\+\s*[a-zA-Z_][\w.]*(?:\.[a-zA-Z_]\w*)?\.Error\(\)\}\))/g;
  out = out.replace(re3, (m, o1, status, o2, lit, o3) => {
    const en = JSON.parse(lit);
    if (cjk.test(en)) return m;
    const zh = MAP[en];
    if (!zh) { missing.add(en); return m; }
    count++;
    return `respondErrorf(c, ${status}, "${zh}%s", "${en}%s", ${errVarOf(o3)}.Error())`;
  });

  // 形 2：多行单键
  const re2 = /(c\.JSON\()((?:http\.Status\w+|\d{3}))(,\s*gin\.H\{\s*\n\s*"error":\s*)("(?:[^"\\]|\\.)*")(,\s*\n\s*\}\))/g;
  out = out.replace(re2, (m, o1, status, o2, lit, o3) => {
    const en = JSON.parse(lit);
    if (cjk.test(en)) return m;
    const zh = MAP[en];
    if (!zh) { missing.add(en); return m; }
    count++;
    return `respondError(c, ${status}, ${JSON.stringify(zh)}, ${lit})`;
  });

  // 形 1：单行（status 可是 http.StatusX 或数字）
  const re1 = /(c\.JSON\()((?:http\.Status\w+|\d{3}))(,\s*gin\.H\{"error":\s*)("(?:[^"\\]|\\.)*")(\}\))/g;
  out = out.replace(re1, (m, o1, status, o2, lit, o3) => {
    const en = JSON.parse(lit);
    if (cjk.test(en)) return m;
    const zh = MAP[en];
    if (!zh) { missing.add(en); return m; }
    count++;
    return `respondError(c, ${status}, ${JSON.stringify(zh)}, ${lit})`;
  });

  if (count) {
    fs.writeFileSync(p, out);
    byFile[f] = count;
    total += count;
  }
}

// 从形 3 的第 5 捕捿组提取 err 变名（"…+ err.Error()})"）
function errVarOf(tail) {
  const m = tail.match(/\+\s*([a-zA-Z_][\w.]*)\.Error\(\)/);
  return m ? m[1] : 'err';
}

console.log('transformed sites:', total);
console.log(JSON.stringify(byFile, null, 2));
if (missing.size) {
  console.error('NO TRANSLATION for:');
  for (const en of [...missing].sort()) console.error(' ', JSON.stringify(en));
  process.exitCode = 1;
}
