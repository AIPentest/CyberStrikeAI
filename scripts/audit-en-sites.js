#!/usr/bin/env node
// scripts/audit-en-sites.js — list English-only c.JSON error sites (untranslated zh missing).
const fs = require('fs');
const path = require('path');
const dir = path.join('internal', 'handler');
const files = fs.readdirSync(dir).filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
const cjk = /[一-鿿]/;
const sites = [];
for (const f of files) {
  const lines = fs.readFileSync(path.join(dir, f), 'utf8').split('\n');
  lines.forEach((line, i) => {
    const m = line.match(/c\.JSON\((http\.Status\w+),\s*gin\.H\{"error":\s*"((?:[^"\\]|\\.)*)"\}\)/);
    if (!m) return;
    if (cjk.test(m[2])) return;
    sites.push([f, i + 1, m[1], m[2]]);
  });
}
for (const [f, ln, st, en] of sites) console.log(`${f}:${ln} ${st} ${JSON.stringify(en)}`);
console.log('total', sites.length);
