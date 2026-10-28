const fs = require('fs');
const path = require('path');
const dir = process.argv[2] || 'internal/handler';
const files = fs.readdirSync(dir).filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
// find "error": <expr> where expr contains string literal + '+' concat
const out = [];
for (const f of files) {
  const lines = fs.readFileSync(path.join(dir, f), 'utf8').split('\n');
  lines.forEach((line, i) => {
    const m = line.match(/"error":\s*(.+)/);
    if (m && /\+/.test(m[1])) {
      out.push({ file: f, line: i + 1, expr: m[1].trim().replace(/,\s*$/, '').replace(/\}\s*$/, '').trim() });
    }
  });
}
out.forEach(o => console.log(`${o.file}:${o.line}: ${o.expr}`));
console.log('TOTAL', out.length);
