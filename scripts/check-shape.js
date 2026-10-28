const fs = require('fs');
const files = fs.readdirSync('internal/handler').filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
let n = 0, bad = 0;
for (const f of files) {
  const s = fs.readFileSync('internal/handler/' + f, 'utf8');
  const reG = /respondError\(c, http\.Status\w+, "(?:[^"\\]|\\.)*", "(?:[^"\\]|\\.)*"\)/g;
  const count = (s.match(reG) || []).length; n += count;
  const total = (s.match(/respondError\(/g) || []).length;
  if (total !== count) { bad += total - count; console.log(f, 'well-shaped', count, 'total', total); }
}
console.log('well-shaped respondError:', n, 'anomalous:', bad);
