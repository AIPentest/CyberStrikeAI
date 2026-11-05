import fs from 'fs';

// merge audit-keys-all.json into each locale's settingsAudit.msg
// en -> [0], zh -> [1], ru -> [0] (no Russian translator; English fallback)
const all = JSON.parse(fs.readFileSync('scripts/audit-keys-all.json', 'utf8'));
const targets = {
  'web/static/i18n/en-US.json': 0,
  'web/static/i18n/zh-CN.json': 1,
  'web/static/i18n/ru-RU.json': 0,
};
for (const [p, idx] of Object.entries(targets)) {
  const j = JSON.parse(fs.readFileSync(p, 'utf8'));
  j.settingsAudit = j.settingsAudit || {};
  j.settingsAudit.msg = j.settingsAudit.msg || {};
  let n = 0;
  for (const [cat, acts] of Object.entries(all)) {
    j.settingsAudit.msg[cat] = j.settingsAudit.msg[cat] || {};
    for (const [act, pair] of Object.entries(acts)) {
      j.settingsAudit.msg[cat][act] = pair[idx];
      n++;
    }
  }
  fs.writeFileSync(p, JSON.stringify(j, null, 2));
  console.log(p, 'added', n);
}
