// i18n-migrate.mjs — inventory hardcoded Chinese frontend text per docs/en-US/frontend-i18n.md
// Usage:
//   node scripts/i18n-migrate.mjs stats                — per-file uncovered counts
//   node scripts/i18n-migrate.mjs inventory             — write scripts/i18n-inventory.json (classified)
//   node scripts/i18n-migrate.mjs extract <file.js>     — dump categorized Chinese strings for one file
// Categories:
//   ui       — visible text candidates (innerHTML/textContent/placeholder/title/alert/notify/labels)
//   compare  — string equality/case values (must NOT be keyed — backend/JS semantics)
//   prompt   — LLM prompt text (not UI; out of scope per guidelines)
//   comment  — source comments (out of scope)
//   other    — needs manual review

import fs from 'node:fs';
import path from 'node:path';

// Script lives in <repo>/scripts — repo root is one level up.
const ROOT = path.resolve(path.dirname(process.argv[1] ?? '.'), '..');
const JS_DIR = path.join(ROOT, 'web', 'static', 'js');
const TPL_DIR = path.join(ROOT, 'web', 'templates');

const HAN = /[一-鿿]/;
// A line is "covered" when its Chinese text feeds an i18n call: the global
// window.t helper, per-module translate helpers (*T / *Translate / *T(key,...)),
// or data-i18n attributes in templates.
const COVERED = /(?:window\.t\(|\w*[Tt]ranslate\(|\w+T\(|\bdt\(|\bst\(|\btr\(|\b_tf\(|\b_t\(|\bt\(|data-i18n(?:-[\w-]+)?=|applyTranslations|__locale)/;
const UI_HINT = /(?:innerHTML|textContent|innerText|placeholder|aria-|title|alert\(|notif|toast|label|caption|tooltip|summary|render|html|msg|message|error|text)/i;
const COMPARE_HINT = /(?:===|!==|case\s+\S?['"]|\?\.|switch\s*\()/;

function listFiles(dir, ext) {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir, { recursive: true })
    .filter((f) => f.endsWith(ext) && !f.includes('vendor'))
    .map((f) => path.join(dir, f));
}

function categorize(line) {
  const trimmed = line.trim();
  if (trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*')) return 'comment';
  const isStringCmp = /(===|!==|case\s+|switch\s*\()\s*['"][^\n]*[一-鿿]/.test(line);
  if (isStringCmp) return 'compare';
  if (/PROMPT|prompt|请根据|系统提示|systemPrompt/i.test(line)) return 'prompt';
  if (UI_HINT.test(line)) return 'ui';
  return 'other';
}

function scanFile(file, kind) {
  const lines = fs.readFileSync(file, 'utf8').split('\n');
  const hits = [];
  lines.forEach((line, i) => {
    if (!HAN.test(line)) return;
    const covered = COVERED.test(line);
    const category = categorize(line);
    const strings = [...line.matchAll(/(['"`])([^'"`\n]*一[^一-鿿]*|[^一-鿿]*一[^\n]*?)\1/g)].map((m) => m[2]);
    hits.push({ line: i + 1, covered, category, strings, text: line.trim().slice(0, 200) });
  });
  return { file: path.relative(ROOT, file).replaceAll('\\', '/'), kind, hits };
}

function scan() {
  const results = [];
  for (const f of listFiles(JS_DIR, '.js')) {
    const r = scanFile(f, 'js');
    if (r.hits.length) results.push(r);
  }
  for (const f of listFiles(TPL_DIR, '.html')) {
    const r = scanFile(f, 'html');
    if (r.hits.length) results.push(r);
  }
  return results;
}

const cmd = process.argv[2] ?? 'stats';

if (cmd === 'extract') {
  const target = process.argv[3];
  if (!target) { console.error('usage: node scripts/i18n-migrate.mjs extract <relative/file.js>'); process.exit(1); }
  const results = scan();
  const r = results.find((x) => x.file.endsWith(target) || x.file === target);
  if (!r) { console.error('no Chinese hits in ' + target); process.exit(0); }
  const grouped = { ui: [], compare: [], prompt: [], other: [] };
  for (const h of r.hits) {
    if (h.covered || h.category === 'comment') continue;
    grouped[h.category].push({ line: h.line, strings: h.strings, text: h.text });
  }
  console.log(JSON.stringify({ file: r.file, ui: grouped.ui, compare: grouped.compare, prompt: grouped.prompt, other: grouped.other }, null, 1));
} else if (cmd === 'inventory') {
  const results = scan();
  fs.writeFileSync(path.join(ROOT, 'scripts', 'i18n-inventory.json'), JSON.stringify(results, null, 1));
  const total = results.reduce((a, r) => a + r.hits.length, 0);
  const uncovered = results.reduce((a, r) => a + r.hits.filter((h) => !h.covered).length, 0);
  const ui = results.reduce((a, r) => a + r.hits.filter((h) => !h.covered && h.category === 'ui').length, 0);
  console.log(`wrote scripts/i18n-inventory.json — ${results.length} files, ${total} Chinese lines, ${uncovered} uncovered, ${ui} ui-candidates`);
} else {
  const results = scan();
  const rows = results.map((r) => {
    const total = r.hits.length;
    const uncovered = r.hits.filter((h) => !h.covered).length;
    const ui = r.hits.filter((h) => !h.covered && h.category === 'ui').length;
    return { file: r.file, total, uncovered, ui };
  }).sort((a, b) => b.uncovered - a.uncovered);
  for (const r of rows) if (r.uncovered > 0) console.log(`${String(r.uncovered).padStart(5)} unc ${String(r.ui).padStart(5)} ui  ${String(r.total).padStart(5)} tot  ${r.file}`);
  const grand = rows.reduce((a, r) => a + r.uncovered, 0);
  console.log(`\ntotal uncovered: ${grand}`);
}
