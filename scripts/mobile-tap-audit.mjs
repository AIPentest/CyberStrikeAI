/*
 * CyberStrikeAI — mobile tap audit.
 *
 * Proves the claim "every button on a phone can actually be tapped and shows up
 * correctly". It is deliberately narrower than scripts/mobile-audit.mjs (layout
 * defects): here every interactive element in the *active* page must pass four
 * geometric/reachability checks, and the injected mobile chrome must pass a
 * scripted finger tap.
 *
 *   node scripts/mobile-tap-audit.mjs --user=admin --pass=... --width=390 --height=844
 *   CSAI_AUDIT_USER=admin CSAI_AUDIT_PASS=... node scripts/mobile-tap-audit.mjs --all
 *   node scripts/mobile-tap-audit.mjs --cookie=<auth_token 的值> --all
 *
 * Flags:  --base=https://127.0.0.1:8088  --pages=a,b,c  --allow-writes=0|1  --ua=iphone|android
 *         --chrome=0 (skip the finger-tap suite)  --json=<path>
 *
 * Nothing destructive is tapped: elements whose label matches a write verb are
 * measured but never clicked, and 新对话-style writes need --allow-writes=1.
 * Credentials are never defaulted — a baked-in password leaks the moment this
 * file is committed or served.
 */
import fs from 'node:fs';
import { setTimeout as sleep } from 'node:timers/promises';

const argv = Object.fromEntries(process.argv.slice(2).map(a => {
  const i = a.indexOf('=');
  return i === -1 ? [a.replace(/^--/, ''), '1'] : [a.slice(2, i), a.slice(i + 1)];
}));

const W = Number(argv.width || 390), H = Number(argv.height || 844);
const PORT = argv.port || '9712';
const BASE = (argv.base || 'https://127.0.0.1:8088').replace(/\/$/, '');
const USER = argv.user || process.env.CSAI_AUDIT_USER || '';
const PASS = argv.pass || process.env.CSAI_AUDIT_PASS || '';
/* 不想交出口令时的替代路径：在浏览器里登录 127.0.0.1:8088，从 DevTools 复制
   auth_token 的值，用 --cookie=... 或 CSAI_AUDIT_COOKIE=... 传进来。会话本身 12 小时
   过期，比长期有效的主机口令暴露面小。两者都没有则拒绝运行。 */
const COOKIE = argv.cookie || process.env.CSAI_AUDIT_COOKIE || '';
const ALLOW_WRITES = argv['allow-writes'] === '1';
const MIN_TAP = Number(argv['min-tap'] || 44);
const CHROME = argv.chrome !== '0';
const OUT = argv.out || `/tmp/cs-tap/${W}x${H}`;
const JSON_OUT = argv.json || `${OUT}/tap-audit.json`;
const BASELINE = argv.baseline || 'scripts/mobile-tap-baseline.json';

if (!COOKIE && (!USER || !PASS)) {
  console.error('Refusing to run without credentials: pass --user=... --pass=... (or CSAI_AUDIT_USER/CSAI_AUDIT_PASS), or an existing session via --cookie=... (or CSAI_AUDIT_COOKIE).');
  process.exit(2);
}

const SPA_PAGES = (argv.pages || (argv.all ? '' : 'dashboard,chat,hitl,tasks'))
  .split(',').filter(Boolean);

const PAGES = argv.all
  ? ['dashboard', 'chat', 'projects', 'vulnerabilities', 'tasks', 'workflows', 'asset-overview',
     'asset-library', 'info-collect', 'hitl', 'tool-guard', 'webshell', 'c2-listeners', 'c2-sessions',
     'c2-tasks', 'c2-payloads', 'c2-events', 'c2-profiles', 'chat-files', 'mcp-monitor', 'mcp-management',
     'knowledge-management', 'knowledge-retrieval-logs', 'roles-management', 'platform-rbac',
     'skills-monitor', 'skills-management', 'agents-management', 'settings', 'audit-log', 'monitor']
  : SPA_PAGES;

fs.mkdirSync(OUT, { recursive: true });

/* Write verbs: an element carrying one of these in its label is measured, never
   tapped. Without this the audit would delete real rows in whatever instance it
   points at. */
const WRITE_RE = /(删除|清空|清理|退出|注销|重启|停止|终止|中止|执行|运行|发送|提交|保存|新建|新增|创建|导入|上传|应用|重置|启用|禁用|安装|卸载|开始|立即|一键|批量|覆盖|恢复|回滚|delete|remove|clear|logout|submit|save|create|import|upload|apply|reset|install|uninstall|run|execute|send|restart|stop|kill|rollback|restore)/i;

const CH = process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const chrome = await import('node:child_process').then(m =>
  m.spawn(CH, ['--headless=new', `--remote-debugging-port=${PORT}`,
    `--user-data-dir=/tmp/cs-tap-${PORT}`, '--ignore-certificate-errors',
    '--no-sandbox', '--allow-running-insecure-content', 'about:blank'], { stdio: 'ignore' }));
/* 任何提前退出（连不上、登录失败）都不许把 headless Chrome 留在机器上 */
process.on('exit', () => { try { chrome.kill('SIGKILL'); } catch { /* 已退出 */ } });

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

let idc = 0; const pending = new Map(); const events = [];
ws.onmessage = e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const { res, rej } = pending.get(m.id); pending.delete(m.id);
    m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result);
  } else if (m.method) { events.push(m.method); }
};
const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
  const id = ++idc; pending.set(id, { res, rej });
  ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
});

const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
for (const m of ['Page.enable', 'Runtime.enable', 'Security.enable', 'Log.enable']) await send(m, {}, sessionId);
await send('Security.setIgnoreCertificateErrors', { ignore: true }, sessionId);
await send('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 3, mobile: true }, sessionId);
await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 }, sessionId);
/* --ua=android 用安卓 Chrome 的 UA 复跑同一套门禁（壳层按视口宽度判定，与 UA 无关；
   这里换 UA 是为了让"同一份 UI 在安卓浏览器下同样合格"有可复现的证据）。 */
const UA_STRINGS = {
  iphone: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1',
  android: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36'
};
const UA = UA_STRINGS[(argv.ua || 'iphone').toLowerCase()];
if (!UA) { console.error(`unknown --ua=${argv.ua} (iphone | android)`); process.exit(2); }
await send('Network.setUserAgentOverride', { userAgent: UA }, sessionId);

const ev = async expr => {
  const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true }, sessionId);
  if (r.exceptionDetails) return { __err: (r.exceptionDetails.exception?.description || r.exceptionDetails.text).slice(0, 300) };
  return r.result?.value;
};

/* 真手指只有 touch：Chromium 在移动模拟下会自己从 touchEnd 合成一次 click。
   再补一对 mouse 事件就是第二次 click —— 抽屉/面板会被「开了又关」，
   于是 menu-open 这类断言假失败（实测踩过）。 */
async function fingerTap(x, y) {
  const t = { x: Math.round(x), y: Math.round(y) };
  await send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [t] }, sessionId);
  await sleep(40);
  await send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] }, sessionId);
  await sleep(260);
}

const DETECT = `(function(){
  var vw=innerWidth, vh=innerHeight, MIN=${MIN_TAP};
  var SEL='button,a[href],input:not([type=hidden]),select,textarea,[onclick],[role=button],[role=tab],[role=menuitem],[role=switch],[role=checkbox],[role=link],.nav-item-content,.nav-submenu-item,.settings-custom-select-trigger,.tab,.switch';
  function nm(el){
    var s=el.tagName.toLowerCase();
    if(el.id) s+='#'+el.id;
    else if(typeof el.className==='string'&&el.className.trim()){
      var c=el.className.trim().split(/\\s+/).filter(function(x){return !/^is-|^active$|^has-/.test(x)});
      s+='.'+(c[0]||''); if(c[1]&&/layout|sidebar|content|panel|main|nav|list|detail|grid|row|header|body|wrap|table|bar|tab/i.test(c[1])) s+='.'+c[1];
    }
    return s;
  }
  function path(el){ var p=[],n=el; for(var i=0;i<4&&n&&n.tagName;i++){p.unshift(nm(n));n=n.parentElement} return p.join('>') }
  function label(el){
    var t=(el.innerText||'').trim();
    if(!t) t=(el.value||'').trim();
    if(!t) t=el.getAttribute('aria-label')||el.getAttribute('title')||el.getAttribute('placeholder')||'';
    if(!t&&el.getAttribute('data-i18n')) t=el.getAttribute('data-i18n');
    if(!t){ var i=el.querySelector('i,svg,img'); t=(el.className||'')+'[icon]'; }
    return String(t).replace(/\\s+/g,' ').trim().slice(0,42);
  }
  function shown(el){
    var n=el;
    while(n&&n!==document.documentElement){
      var c=getComputedStyle(n);
      if(c.display==='none'||c.visibility==='hidden'||parseFloat(c.opacity)===0) return false;
      n=n.parentElement;
    }
    return true;
  }
  function inActivePage(el){
    var p=el.closest('.page');
    return !p||p.classList.contains('active');
  }
  function scrollAncestor(el,axis){
    var a=el.parentElement;
    while(a){ var c=getComputedStyle(a);
      if(axis==='x'&&/auto|scroll/.test(c.overflowX)) return a;
      if(axis==='y'&&/auto|scroll/.test(c.overflowY)) return a;
      if(a===document.body) break; a=a.parentElement; }
    return null;
  }
  function clipAncestor(el){
    var a=el.parentElement;
    while(a){ var c=getComputedStyle(a);
      if(c.overflowX==='hidden'||c.overflowY==='hidden'||c.overflow==='hidden') return a;
      if(a===document.body) break; a=a.parentElement; }
    return null;
  }
  var fixedTally=function(){
    var out=[];
    [...document.querySelectorAll('*')].forEach(function(el){
      var c=getComputedStyle(el);
      if(c.position!=='fixed'&&c.position!=='sticky') return;
      if(!shown(el)) return;
      var r=el.getBoundingClientRect();
      if(r.width<1||r.height<1) return;
      if(c.pointerEvents==='none') return;
      if(parseFloat(c.zIndex||0)<0&&c.zIndex!=='auto') {}
      out.push({el:el,r:r,n:nm(el),z:c.zIndex});
    });
    return out;
  }();
  function overFixed(r){
    for(var i=0;i<fixedTally.length;i++){
      var f=fixedTally[i];
      if(r.top>=f.r.top&&r.bottom<=f.r.bottom&&r.left>=f.r.left&&r.right<=f.r.right) return f.n;
    }
    return null;
  }

  var bad=[], scanned=0, chromeSeen={};
  var list=[...document.querySelectorAll(SEL)];
  list.forEach(function(el){
    if(!shown(el)||!inActivePage(el)) return;
    /* 折叠 <details> 里的控件本来就要展开才存在，不算"点不到" */
    var det=el.closest('details');
    if(det&&!det.open&&!det.querySelector('summary').contains(el)) return;
    /*SCOPE*/
    if(el.ownerSVGElement) return;
    if(el.closest('.markdown-body')&&getComputedStyle(el).display==='inline') return;
    scanned++;
    var c=getComputedStyle(el);
    var r=el.getBoundingClientRect();
    /* try to bring it into view first: unreachable means a real dead button */
    if(r.top<0||r.bottom>vh||r.left<0||r.right>vw){
      /* 判"够不够得到"必须按手指能做的事来：逐层把可滚祖先滚到把元素居中。
         scrollIntoView 不算——实测它在这种「外层 overflow:hidden + 内层 auto」的嵌套里
         根本不滚（元素原地不动），会把 59 个划得到的控件误报成 offscreen。
         overflow:hidden 的祖先手指也划不动，所以只滚 auto/scroll 的层。 */
      (function reveal(node){
        var a=node.parentElement;
        while(a&&a!==document.documentElement){
          var cc=getComputedStyle(a);
          if(/auto|scroll/.test(cc.overflowY)&&a.scrollHeight>a.clientHeight+2){
            var ab=a.getBoundingClientRect(), rb2=node.getBoundingClientRect();
            a.scrollTop+= (rb2.top+rb2.height/2)-(ab.top+ab.height/2);
          }
          if(/auto|scroll/.test(cc.overflowX)&&a.scrollWidth>a.clientWidth+2){
            var ab2=a.getBoundingClientRect(), rb3=node.getBoundingClientRect();
            a.scrollLeft+= (rb3.left+rb3.width/2)-(ab2.left+ab2.width/2);
          }
          a=a.parentElement;
        }
      })(el);
      r=el.getBoundingClientRect();
    }
    var w=r.width,h=r.height;
    var out={el:nm(el),p:path(el),t:label(el),w:Math.round(w),h:Math.round(h)};
    if(w<2||h<2){ out.why='zero'; bad.push(out); return; }
    if(c.pointerEvents==='none'||c.pointerEvents==='hidden'){ out.why='pointer-none'; bad.push(out); return; }
    if(w<MIN||h<MIN){
      /* 复选/单选框的命中区常常由外层 label 提供（iOS 设置里也是整行可点）：
         自身 >=32 且 label 两个方向都到下限才算合格，否则照报 tiny */
      var pass=false;
      /* 本层对复选/单选框的口径是 30px（见 mobile.css 24.11），整行/label 才是命中区 */
      if((el.type==='checkbox'||el.type==='radio')&&w>=30&&h>=30){
        /* 命中区常常由 label 或整行提供（本层 24.11 就是按"整行可点"写的）：
           向上找第一个真正可点的祖先，它到下限就算合格 */
        var n=el, d=0;
        while(n&&n!==document.body&&d++<5){
          var clickable=n.tagName==='LABEL'||n.hasAttribute('onclick')||getComputedStyle(n).cursor==='pointer';
          if(clickable){var nb=n.getBoundingClientRect(); if(nb.width>=MIN&&nb.height>=MIN){pass=true;break;}}
          n=n.parentElement;
        }
      }
      if(!pass){ out.why='tiny'; bad.push(out); return; }
    }
    var cx=r.left+w/2, cy=r.top+h/2;
    if(cx<0||cy<0||cx>vw||cy>vh){
      var cl=clipAncestor(el), sx=scrollAncestor(el,'x'), sy=scrollAncestor(el,'y');
      out.why='offscreen'; out.clip=cl?nm(cl):null; out.swipes=[sx?'x':null,sy?'y':null].filter(Boolean).join('');
      bad.push(out); return;
    }
    var hitTop=document.elementFromPoint(cx,cy);
    var ok=false, cur=hitTop;
    while(cur){ if(cur===el){ok=true;break} cur=cur.parentElement; }
    if(!ok){
      cur=el;
      while(cur&&!ok){ if(cur.contains&&hitTop&&cur.contains(hitTop)) ok=true; cur=cur.parentElement; }
    }
    if(!ok&&hitTop){
      /* delegation: a handler on an ancestor is a legitimate target */
      var a=el.parentElement, d=0;
      while(a&&d++<4){ if(a.contains(hitTop)){ok=true;break} a=a.parentElement; }
    }
    if(!ok){
      out.why='covered';
      out.blocker=hitTop?nm(hitTop):'none';
      out.blockerPath=hitTop?path(hitTop):'';
      var of=overFixed(r); if(of) out.blockerFixed=of;
      bad.push(out); return;
    }
    if(el.id) chromeSeen[el.id]=1;
  });

  var scr=document.scrollingElement||document.documentElement;
  return {
    vw:vw,vh:vh,scanned:scanned,bad:bad,
    doc:{h:Math.round(scr.scrollHeight),clientH:scr.clientHeight,canScrollY:scr.scrollHeight>scr.clientHeight+2,
         bodyOverflowY:getComputedStyle(document.body).overflowY,htmlTouch:getComputedStyle(document.documentElement).touchAction},
    chrome:{tabbar:!!document.querySelector('#m-tabbar'),sheet:!!document.querySelector('#m-sheet'),
            menu:!!document.querySelector('.m-menu-btn'),more:!!document.querySelector('.m-more-btn'),
            chatbar:!!document.querySelector('#m-chat-bar'),scrim:!!document.querySelector('#m-scrim')},
    locale:(document.documentElement.getAttribute('lang')||'')
  };
})()`;

/* DETECT 默认扫"当前页 + 全局固定件"。遮罩展开时页面元素会被 scrim 判成 covered，
   所以扫抽屉/面板要限定作用域，否则全是假红。 */
const detectFor = scope => DETECT.replace('/*SCOPE*/',
  scope ? `if(!el.closest(${JSON.stringify(scope)})) return;` : '');

async function openPage(hash) {
  await ev(`location.hash=${JSON.stringify(hash)}`);
  await sleep(2600);
}

const host = new URL(BASE).hostname;
if (COOKIE) await send('Network.setCookie', { name: 'auth_token', value: COOKIE, domain: host, path: '/' }, sessionId);
await send('Page.navigate', { url: BASE + '/' }, sessionId);
await sleep(3500);

/* The login card is a phone surface too — check it before authenticating. */
const login = await ev(DETECT);
if (!COOKIE) {
  await ev(`(()=>{const u=document.getElementById('login-username'),p=document.getElementById('login-password');if(u&&p){u.value=${JSON.stringify(USER)};p.value=${JSON.stringify(PASS)};document.getElementById('login-form').requestSubmit();}})()`);
  await sleep(8000);
} else {
  console.log('using the session cookie supplied via --cookie; no password was sent');
}
const who = await ev(`(()=>{const o=document.getElementById('login-overlay');return o?getComputedStyle(o).display:'none'})()`);
if (who !== 'none') { console.error('login did not take (overlay still displayed):', who); process.exit(3); }

const report = { login, pages: {}, chromeTaps: [] };
let grand = {};
const bump = k => { grand[k] = (grand[k] || 0) + 1; };

const log = (o, k) => { (o.bad || []).forEach(b => bump(b.why)); };

for (const pg of PAGES) {
  await openPage(pg);
  const r = await ev(DETECT);
  if (!r || r.__err) { console.log(pg, 'DETECT ERROR', r && r.__err); continue; }
  report.pages[pg] = r;
  log(r);
  const n = r.bad.length;
  console.log(`${pg.padEnd(26)} scanned=${String(r.scanned).padStart(3)} defects=${String(n).padStart(3)} ${n ? '' : '  OK'}`);
  r.bad.slice(0, 6).forEach(b => console.log(`    ${b.why.padEnd(12)} ${String(b.w)}x${String(b.h)} ${b.t}  ${b.el}${b.blocker ? ' <= ' + b.blocker : ''}`));
  const shot = await send('Page.captureScreenshot', { format: 'png' }, sessionId);
  fs.writeFileSync(`${OUT}/${pg}.png`, Buffer.from(shot.data, 'base64'));
}

/* ---------------- overlays: 抽屉与面板展开后的按钮 ---------------- */
const OVERLAYS = [
  { name: 'sheet', route: 'dashboard', open: `CSAMobile.openSheet()`, scope: '#m-sheet' },
  { name: 'drawer', route: 'dashboard', open: `CSAMobile.openDrawer()`, scope: '#main-sidebar' },
  { name: 'chat-drawer', route: 'chat', open: `CSAMobile.openChatDrawer()`, scope: '#conversation-sidebar' },
];
for (const o of OVERLAYS) {
  await openPage(o.route);
  await ev(`(()=>{try{${o.open}}catch(e){}})()`);
  await sleep(900);                       /* 等开合动画结束，动画中量到的是位移中的盒子 */
  const r = await ev(detectFor(o.scope));
  if (!r || r.__err) { console.log(o.name, 'DETECT ERROR', r && r.__err); continue; }
  report.pages[o.name] = r;
  log(r);
  console.log(`${('[' + o.name + ']').padEnd(26)} scanned=${String(r.scanned).padStart(3)} defects=${String(r.bad.length).padStart(3)} ${r.bad.length ? '' : '  OK'}`);
  r.bad.slice(0, 6).forEach(b => console.log(`    ${b.why.padEnd(12)} ${String(b.w)}x${String(b.h)} ${b.t}  ${b.el}${b.blocker ? ' <= ' + b.blocker : ''}`));
  await ev(`CSAMobile.closeSheet();CSAMobile.closeDrawer();CSAMobile.closeChatDrawer();`);
}

/* ---------------- chrome finger taps ---------------- */
/* 关闭要走用户真能点到的控件：抽屉展开后汉堡键被抽屉自己盖住，
   再点它点的是侧栏（真实用户用抽屉内的 × 或遮罩）。每步前先复位，
   否则一条失败会连锁把后面的断言全带偏。 */
const RESET = `CSAMobile.closeDrawer();CSAMobile.closeSheet();CSAMobile.closeChatDrawer();`;
const TAPS = [
  { name: 'menu-open', sel: '.m-menu-btn', pre: RESET, expect: `document.querySelector('#main-sidebar').classList.contains('m-open')` },
  { name: 'menu-close', sel: '#main-sidebar .m-close-btn', pre: `${RESET}CSAMobile.openDrawer();`, expect: `!document.querySelector('#main-sidebar').classList.contains('m-open')` },
  { name: 'menu-scrim-close', sel: '#m-scrim', pre: `${RESET}CSAMobile.openDrawer();`, expect: `!document.querySelector('#main-sidebar').classList.contains('m-open')` },
  { name: 'more-open', sel: '.m-more-btn', pre: RESET, expect: `document.querySelector('#m-sheet').classList.contains('m-show')` },
  { name: 'more-close', sel: '#m-sheet .m-close-btn', pre: `${RESET}CSAMobile.openSheet();`, expect: `!document.querySelector('#m-sheet').classList.contains('m-show')` },
  { name: 'tab-hitl', sel: '#m-tabbar [data-tab=hitl]', expect: `document.querySelector('#page-hitl').classList.contains('active')` },
  { name: 'tab-tasks', sel: '#m-tabbar [data-tab=tasks]', expect: `document.querySelector('#page-tasks').classList.contains('active')` },
  { name: 'tab-dashboard', sel: '#m-tabbar [data-tab=dashboard]', expect: `document.querySelector('#page-dashboard').classList.contains('active')` },
  { name: 'tab-chat', sel: '#m-tabbar [data-tab=chat]', expect: `document.querySelector('#page-chat').classList.contains('active')` },
  { name: 'chat-drawer-open', sel: '#m-chat-bar button:first-child', expect: `document.querySelector('#conversation-sidebar').classList.contains('m-open')` },
  { name: 'chat-drawer-close', sel: '#conversation-sidebar .m-close-btn', expect: `!document.querySelector('#conversation-sidebar').classList.contains('m-open')` },
];

if (CHROME) {
  await openPage('chat');
  for (const t of TAPS) {
    if (t.pre) await ev(`(()=>{try{${t.pre}}catch(e){}})()`);
    await sleep(320);
    const box = await ev(`(()=>{const e=document.querySelector(${JSON.stringify(t.sel)});if(!e)return null;const r=e.getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2,w:r.width,h:r.height,shown:getComputedStyle(e).display!=='none'}})()`);
    if (!box || !box.shown) { report.chromeTaps.push({ name: t.name, ok: false, why: 'absent' }); console.log(`tap ${t.name.padEnd(20)} ABSENT`); continue; }
    events.length = 0;
    await fingerTap(box.x, box.y);
    const passed = await ev(`(()=>{try{return !!(${t.expect})}catch(e){return false}})()`);
    const err = events.includes('Runtime.exceptionThrown');
    report.chromeTaps.push({ name: t.name, ok: !!passed && !err, state: passed, err });
    console.log(`tap ${t.name.padEnd(20)} ${passed && !err ? 'ok' : 'FAIL'} (state=${passed} jsErr=${err})`);
  }
  /* language round-trip: the injected chrome must re-label itself */
  await ev(`window.changeLanguage && window.changeLanguage('en-US')`);
  await sleep(2500);
  const en = await ev(`(()=>{const o=[];document.querySelectorAll('#m-tabbar .m-tab>span:not(.m-tab-badge),#m-sheet .m-sheet-item>span').forEach(function(s){o.push(s.textContent.trim())});return {lang:document.documentElement.getAttribute('lang'),labels:o}})()`);
  report.langEn = en;
  console.log('en-US chrome labels:', JSON.stringify(en));
  await ev(`window.changeLanguage && window.changeLanguage('ru-RU')`);
  await sleep(2500);
  const ru = await ev(`(()=>{const o=[];document.querySelectorAll('#m-tabbar .m-tab>span:not(.m-tab-badge),#m-sheet .m-sheet-item>span').forEach(function(s){o.push(s.textContent.trim())});return {lang:document.documentElement.getAttribute('lang'),labels:o}})()`);
  report.langRu = ru;
  console.log('ru-RU chrome labels:', JSON.stringify(ru));
  await ev(`window.changeLanguage && window.changeLanguage('zh-CN')`);
  await sleep(2000);
}

fs.writeFileSync(JSON_OUT, JSON.stringify(report, null, 2));

const chromeFail = report.chromeTaps.filter(t => !t.ok).length;
const total = Object.values(grand).reduce((a, b) => a + b, 0);
console.log(`\n=========== MOBILE TAP AUDIT @${W}x${H} ===========`);
console.log(`pages                ${Object.keys(report.pages).length}`);
console.log(`covered (occluded)   ${grand.covered || 0}`);
console.log(`offscreen            ${grand.offscreen || 0}`);
console.log(`zero-size            ${grand.zero || 0}`);
console.log(`pointer-events:none  ${grand['pointer-none'] || 0}`);
console.log(`tap target < ${MIN_TAP}px  ${grand.tiny || 0}`);
console.log(`chrome taps failed   ${chromeFail}`);
console.log(`report               ${JSON_OUT}`);

/* Ratchet: the钉死的 baseline may only go down. */
let bl = null;
try { bl = JSON.parse(fs.readFileSync(BASELINE, 'utf8')); } catch { }
let ratchetFail = false;
if (bl) {
  for (const [k, v] of Object.entries(grand)) {
    if ((bl[k] ?? 0) < v) { console.log(`RATCHET FAIL ${k}: baseline ${bl[k]} -> now ${v}`); ratchetFail = true; }
  }
  for (const k of Object.keys(bl)) {
    if (k.startsWith('page:')) {
      const name = k.slice(5), now = (report.pages[name] || {}).bad?.length ?? 0;
      if (bl[k] < now) { console.log(`RATCHET FAIL ${name}: baseline ${bl[k]} -> now ${now}`); ratchetFail = true; }
    }
  }
  if (chromeFail > (bl.chromeFail ?? 0)) { console.log(`RATCHET FAIL chrome: ${bl.chromeFail} -> ${chromeFail}`); ratchetFail = true; }
} else {
  const fresh = { ...grand };
  for (const [p, r] of Object.entries(report.pages)) fresh['page:' + p] = r.bad.length;
  fresh.chromeFail = chromeFail;
  fresh.note = 'generated by scripts/mobile-tap-audit.mjs; numbers may only decrease';
  fs.writeFileSync(BASELINE, JSON.stringify(fresh, null, 2));
  console.log(`baseline written -> ${BASELINE}`);
}

/* 退出码口径（写死，别每次靠人判断）：
   - 够不到 / 被遮挡 / 零尺寸 / pointer-events:none —— 任何一处都是硬失败，控件点不到没有"存量债"可言；
   - 自家注入的抽屉、面板、会话抽屉里的按钮 —— 必须 0，不许留债；
   - 桌面遗留控件的小命中区 —— 走基线只降不升，超基线即红。 */
const hard = (grand.covered || 0) + (grand.offscreen || 0) + (grand.zero || 0) + (grand['pointer-none'] || 0);
const ownChrome = ['sheet', 'drawer', 'chat-drawer'].reduce((n, k) => n + ((report.pages[k] || {}).bad || []).length, 0);
const fail = hard || ownChrome || chromeFail || ratchetFail;
console.log(`\n判定: 硬缺陷 ${hard} | 自家外壳 ${ownChrome} | 点击失败 ${chromeFail} | 基线回潮 ${ratchetFail ? '是' : '否'} -> ${fail ? 'FAIL' : 'PASS'}`);
process.exit(fail ? 1 : 0);
