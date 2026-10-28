const fs = require('fs');
const path = require('path');
const dir = 'internal/handler';
const files = fs.readdirSync(dir).filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
const cjk = /[一-鿿]/;
for (const f of files) {
  const lines = fs.readFileSync(path.join(dir, f), 'utf8').split('\n');
  lines.forEach((line, i) => {
    const clean = line.replace(/\r$/, '');
    const m = clean.match(/"error":\s*(.+)$/);
    if (!m) return;
    const expr = m[1].replace(/,\s*$/, '').replace(/\}\s*\)?\s*$/, '').trim();
    if (cjk.test(expr) || /^reservedContextCheckpointError/.test(expr)) {
      console.log(`${f}:${i + 1}: ${expr}`);
    }
  });
}
