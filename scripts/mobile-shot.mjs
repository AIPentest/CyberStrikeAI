/*
 * CyberStrikeAI — mobile shell screenshots.
 *
 * Captures the injected mobile chrome (appbar / tab bar / drawer / bottom sheet)
 * at a phone viewport without needing a login: the chrome is injected by
 * mobile-ui.js boot() purely from the viewport width, so hiding the login
 * overlay is enough to make syncAuthChrome re-show the tab bar. Data-driven page
 * bodies stay empty — this is for judging the *shell*, not the content.
 *
 *   node scripts/mobile-shot.mjs --state=plain --out=/tmp/shot.png
 *   node scripts/mobile-shot.mjs --state=drawer --base=http://127.0.0.1:8088
 *   states: login | plain | drawer | sheet | chat
 */
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';

const argv = Object.fromEntries(process.argv.slice(2).map(a => {
  const i = a.indexOf('=');
  return i === -1 ? [a.replace(/^--/, ''), '1'] : [a.slice(2, i), a.slice(i + 1)];
}));
const W = Number(argv.width || 390), H = Number(argv.height || 844);
const PORT = argv.port || '9713';
const BASE = (argv.base || 'http://127.0.0.1:8088').replace(/\/$/, '');
const STATE = argv.state || 'plain';
const OUT = argv.out || `/tmp/cs-shot/${STATE}-${W}x${H}.png`;

fs.mkdirSync(dirnameOf(OUT), { recursive: true });
function dirnameOf(p) { const i = p.lastIndexOf('/'); return p.slice(0, i) || '.'; }

const CH = process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const chrome = await import('node:child_process').then(m =>
  m.spawn(CH, ['--headless=new', `--remote-debugging-port=${PORT}`,
    `--user-data-dir=/tmp/cs-shot-${PORT}`, '--no-sandbox', 'about:blank'], { stdio: 'ignore' }));
process.on('exit', () => { try { chrome.kill('SIGKILL'); } catch { /* gone */ } });

let ws;
for (let i = 0; i < 80; i++) {
  try {
    const r = await fetch(`http://127.0.0.1:${PORT}/json/version`);
    ws = new WebSocket((await r.json()).webSocketDebuggerUrl);
    await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; setTimeout(rej, 4000); });
    break;
  } catch { await sleep(300); }
}
if (!ws) { console.error('chrome devtools unreachable'); process.exit(1); }

let idc = 0; const pending = new Map();
ws.onmessage = e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const { res, rej } = pending.get(m.id); pending.delete(m.id);
    m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result);
  }
};
const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
  const id = ++idc; pending.set(id, { res, rej });
  ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
});

const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
for (const m of ['Page.enable', 'Runtime.enable']) await send(m, {}, sessionId);
await send('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 3, mobile: true }, sessionId);
await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 }, sessionId);
await send('Network.setUserAgentOverride', {
  userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1'
}, sessionId);
const ev = async expr => {
  const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true }, sessionId);
  if (r.exceptionDetails) return { __err: (r.exceptionDetails.exception?.description || r.exceptionDetails.text).slice(0, 200) };
  return r.result?.value;
};

/* 主题要先落进本站点的 localStorage，about:blank 上写是写给另一个 origin。
   换主题用 Page.navigate 重新进，不要在 eval 里 location.reload()——awaitPromise
   会等一个永远不回来的执行上下文。 */
await send('Page.navigate', { url: BASE + '/' }, sessionId);
await sleep(1500);
const theme = argv.theme || (argv.dark === '1' ? 'dark' : '');
if (theme) {
  await ev(`localStorage.setItem('cyberstrike-theme', ${JSON.stringify(theme)})`);
  await send('Page.navigate', { url: BASE + '/' }, sessionId);
}
await sleep(3000);

if (STATE !== 'login') {
  /* 藏掉登录遮罩会触发 mobile-ui.js 的 MutationObserver，把底部栏显示回来 */
  await ev(`document.getElementById('login-overlay').style.display='none'`);
  await sleep(600);
  if (STATE === 'drawer') await ev(`window.CSAMobile && CSAMobile.openDrawer()`);
  if (STATE === 'sheet') await ev(`window.CSAMobile && CSAMobile.openSheet()`);
  if (STATE === 'chat') { await ev(`window.switchPage && switchPage('chat')`); await sleep(1500); }
  await sleep(1200);
}

const state = await ev(`(()=>({
  booted: !!window.CSAMobile,
  tabbar: !!document.querySelector('#m-tabbar') && getComputedStyle(document.querySelector('#m-tabbar')).display,
  sheet: !!document.querySelector('#m-sheet'),
  drawerOpen: !!document.querySelector('#main-sidebar.m-open'),
  sheetOpen: !!document.querySelector('#m-sheet.m-show'),
  lang: document.documentElement.getAttribute('lang')
}))()`);
console.log(JSON.stringify(state));

/* --eval='...' 用来在同一个已登录/已注入的页面上量一把真实计算样式 */
if (argv.eval) {
  const probe = await ev(`(function(){${argv.eval}})()`);
  console.log(JSON.stringify(probe, null, 2));
}

/* --why='.notification-btn' --prop=border-radius
   规则写了却不生效时，用 CDP 的匹配样式表分清：没命中，还是被更高优先级压住。 */
if (argv.why) {
  await send('DOM.enable', {}, sessionId);
  await send('CSS.enable', {}, sessionId);
  const { root } = await send('DOM.getDocument', { depth: -1 }, sessionId);
  const { nodeId } = await send('DOM.querySelector', { nodeId: root.nodeId, selector: argv.why }, sessionId);
  if (!nodeId) { console.log('selector 未命中任何节点:', argv.why); }
  else {
    const matched = await send('CSS.getMatchedStylesForNode', { nodeId }, sessionId);
    const want = (argv.prop || 'border-radius,border,background').split(',');
    const rows = [];
    for (const e of matched.matchedCSSRules || []) {
      const m = e.rule;
      const decl = (m.style.cssProperties || []).filter(p => want.includes(p.name) && !p.disabled && p.value);
      if (!decl.length) continue;
      const sel = (m.selectorList && m.selectorList.text) || '?';
      rows.push({
        origin: m.origin,
        sheet: (m.style.styleSheetId || '').split('@').pop(),
        selector: sel.length > 78 ? sel.slice(0, 78) + '…' : sel,
        decls: decl.map(d => `${d.name}:${d.value}${d.important ? ' !important' : ''}`),
      });
    }
    console.log(`== ${argv.why} 上命中且声明了 [${want.join(', ')}] 的规则，按级联顺序（后=更优先）==`);
    rows.forEach((r, i) => console.log(`${String(i).padStart(2)} ${r.origin.padEnd(10)} ${r.decls.join(' | ')}\n     ${r.selector}`));
    const cs = await ev(`(()=>{const e=document.querySelector(${JSON.stringify(argv.why)});const c=getComputedStyle(e);return JSON.stringify(${JSON.stringify(want)}.reduce((o,k)=>(o[k]=c[k],o),{}),null,1)})()`);
    console.log('最终计算值:', cs);
  }
}

const shot = await send('Page.captureScreenshot', { format: 'png' }, sessionId);
fs.writeFileSync(OUT, Buffer.from(shot.data, 'base64'));
console.log('shot -> ' + OUT);
process.exit(0);
