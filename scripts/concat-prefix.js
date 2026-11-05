const fs = require('fs');
const files = fs.readdirSync('internal/handler').filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
const set = {};
for (const f of files) {
  const s = fs.readFileSync('internal/handler/' + f, 'utf8');
  const reG = /"error":\s*"([^"]*[一-龕][^"]*)"\s*\+/g;
  let m; while ((m = reG.exec(s)) !== null) { set[m[1]] = (set[m[1]] || 0) + 1; }
}
console.log(JSON.stringify(set, null, 1));
