import fs from 'fs';
import path from 'path';

const dir = process.argv[2] || '.';
const MAP = JSON.parse(fs.readFileSync('scripts/message-map.json', 'utf8'));
const keys = Object.keys(MAP).sort((a, b) => b.length - a.length);

let transformed = 0;
for (const f of fs.readdirSync(path.join(dir, 'internal/handler')).filter(x => x.endsWith('.go') && !x.includes('test') && x !== 'i18n_error.go')) {
  const p = path.join(dir, 'internal/handler', f);
  let s = fs.readFileSync(p, 'utf8');
  // "message": "<zh>"  ->  "message": i18nText(requestWantsEnglish(c), "<zh>", "<en>")
  for (const zh of keys) {
    const o = `"message": "${zh}"`;
    const n = `"message": i18nText(requestWantsEnglish(c), "${zh}", "${MAP[zh]}")`;
    const cnt = s.split(o).length - 1;
    if (cnt > 0) {
      transformed += cnt;
      console.log(`${f}: ${cnt}x ${zh.slice(0, 30)}`);
      s = s.split(o).join(n);
    }
  }
  fs.writeFileSync(p, s);
}
console.log('transformed', transformed);
