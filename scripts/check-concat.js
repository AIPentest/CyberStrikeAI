const fs = require('fs');
const files = fs.readdirSync('internal/handler').filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
let found = [];
for (const f of files) {
  const s = fs.readFileSync('internal/handler/' + f, 'utf8');
  // Chinese string followed by + concatenation inside an error value or vice versa
  const re = /"error":\s*(?:"[^"]*"[^,}]*\+|[^,}]*\+"[^"]*[一-龕][^"]*")/g;
  let m; const reG = new RegExp(re.source.replace('(?=', '(?:'), 'g');
  try { while ((m = reG.exec(s)) !== null) found.push(f + ': ' + m[0].slice(0, 90)); } catch (e) { console.error(e.message); }
}
console.log('concat-error sites:', found.length);
found.slice(0, 40).forEach(x => console.log(x));
