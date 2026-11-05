import fs from 'fs';
import path from 'path';

const dir = process.argv[2] || '.';
const MAP = JSON.parse(fs.readFileSync('scripts/concat-prefix-map.json', 'utf8'));
const PREFIXES = Object.keys(MAP).sort((a, b) => b.length - a.length);

const out = { transformed: 0, sites: [] };
for (const f of fs.readdirSync(path.join(dir, 'internal/handler')).filter(f2 => f2.endsWith('.go') && !f2.includes('test') && f2 !== 'i18n_error.go')) {
  const p = path.join(dir, 'internal/handler', f);
  let s = fs.readFileSync(p, 'utf8');
  const nl = s.includes('\r\n') ? '\r\n' : '\n';

  // Shape 1: c.JSON(http.StatusX, gin.H{"error": "<zh prefix>" + errVar.Error()})
  // Rewrite to: respondErrorf(c, http.StatusX, "<zh prefix>%s", "<en prefix>%s", errVar.Error())
  const re1 = /(c\.JSON\((http\.Status\w+),\s*gin\.H\{"error":\s*")((?:[^"\\]|\\.)*)("\s*\+\s*([a-zA-Z_][\w]*(?:\.[a-zA-Z_]\w*)?)\.Error\(\)\}\))/g;
  s = re1[Symbol.replace] ? s : s; // no-op guard
  const before1 = s;
  s = s.replace(re1, (m, pre1, status, litOpen, zh, litClose2, errVar) => {
    if (!MAP[zh]) return m;
    out.transformed++;
    out.sites.push(`${f}: ${zh}+${errVar}.Error()`);
    return `respondErrorf(c, ${status}, "${zh}%s", "${MAP[zh]}%s", ${errVar}.Error())`;
  });

  // Shape 2: "error": "<zh prefix>" + err.Error() inside multi-key gin.H or multiline blocks:
  // "error":   "连接失败: " + err.Error(),
  // Rewrite value expression in place using i18nText(requestWantsEnglish(c), "zh prefix%v", "en prefix%v") + err... is not a single expr;
  // instead wrap whole value: i18nText(requestWantsEnglish(c), "<zh>%s", "<en>%s") needs args. So do:
  // "error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "<zh prefix>%s", "<en prefix>%s"), err.Error())
  const re2 = /("error":\s*)"((?:[^"\\]|\\.)*)("\s*\+\s*([a-zA-Z_][\w]*(?:\.[a-zA-Z_]\w*)?)\.Error\(\))/g;
  s = s.replace(re2, (m, kw, zh, close, errVar) => {
    if (!MAP[zh]) return m;
    out.transformed++;
    out.sites.push(`${f}: value-site ${zh}`);
    return `${kw}fmt.Sprintf(i18nText(requestWantsEnglish(c), "${zh}%s", "${MAP[zh]}%s"), ${errVar}.Error())`;
  });
  fs.writeFileSync(p, s);
}
console.log('transformed', out.transformed);
out.sites.forEach(x => console.log('  ', x));
