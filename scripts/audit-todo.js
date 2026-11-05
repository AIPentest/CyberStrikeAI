#!/usr/bin/env node
// scripts/audit-todo.js — list unconverted static "error" literals (zh & en)
// in internal/handler/*.go, skipping respondError/i18nText/err.Error() sites.
const fs = require('fs');
const path = require('path');
const dir = path.join('internal', 'handler');
const files = fs.readdirSync(dir).filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
const cjk = /[一-鿿]/;
const lit = {};
for (const f of files) {
  const lines = fs.readFileSync(path.join(dir, f), 'utf8').split('\n');
  lines.forEach((line, i) => {
    const m = line.match(/"error":\s*(.+)/);
    if (!m) return;
    const expr = m[1].trim();
    if (/respondError|i18nText/.test(expr)) return;
    if (/\.Error\(\)/.test(expr)) return;
    const mm = expr.match(/^"((?:[^"\\]|\\.)*)"/);
    if (mm) {
      const k = mm[1];
      (lit[k] = lit[k] || []).push(f + ':' + (i + 1));
    }
  });
}
const zhKeys = Object.keys(lit).filter(k => cjk.test(k));
const enKeys = Object.keys(lit).filter(k => !cjk.test(k));
console.log('unconverted zh literals:', zhKeys.length);
zhKeys.forEach(k => console.log(' ZH', JSON.stringify(k), lit[k].length + 'x', lit[k][0]));
console.log('unconverted en literals:', enKeys.length);
enKeys.forEach(k => console.log(' EN', JSON.stringify(k), lit[k].length + 'x', lit[k][0]));
