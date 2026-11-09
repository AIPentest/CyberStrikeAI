#!/usr/bin/env node
// scripts/audit-en-multiline.js — find English-only single-key multiline
// c.JSON(status, gin.H{\n "error": "...", \n}) sites needing i18n conversion.
const fs = require('fs');
const path = require('path');
const dir = path.join('internal', 'handler');
const files = fs.readdirSync(dir).filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
const cjk = /[一-鿿]/;
const re = /c\.JSON\(\s*(http\.Status\w+|\d{3})\s*,\s*gin\.H\{\s*\n\s*"error":\s*"((?:[^"\\]|\\.)*)",\s*\n\s*\}\)/g;
for (const f of files) {
  const s = fs.readFileSync(path.join(dir, f), 'utf8');
  let m;
  while ((m = re.exec(s))) {
    if (cjk.test(m[2])) continue;
    const line = s.slice(0, m.index).split('\n').length;
    console.log(`${f}:${line} ${m[1]} ${JSON.stringify(m[2])}`);
  }
}
