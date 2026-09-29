/*
 * CyberStrikeAI — unified mobile audit.
 *
 * Every mobile bug found so far, as a repeatable gate. It logs into a running
 * instance, so it needs credentials — nothing is hardcoded. Run against any page:
 *   node scripts/mobile-audit.mjs --user=admin --pass=... --route=dashboard --width=390
 *   node scripts/mobile-audit.mjs --user=admin --pass=... --url=/api-docs --width=390
 *   CSAI_AUDIT_USER=admin CSAI_AUDIT_PASS=... node scripts/mobile-audit.mjs --all
 *
 * Checks:
 *   1 overflow      content past the right edge with no scrollable ancestor
 *   2 squeezed      row flex/grid child under 250px that holds real content
 *   3 unreachable   content pushed off-screen and clipped by an overflow:hidden ancestor
 *   4 touchscroll   a real scroll container without touch-action, or root overscroll:none
 *   5 truncated     text cut off with no swipe to reveal it
 *   6 tapsize       interactive element under 44px
 *   7 overlap       statically positioned siblings whose boxes collide
 *   8 viewport      page height not matching the visual viewport
 *   9 sliver        inner scroll region squeezed to a few px by a locked parent
 *  10 clipped       a hidden dropdown that opens into a region a scroll box hides
 *  11 nibbled       a small abs-positioned badge/label clipped by its scrollable strip
 *  12 crushed        a panel squeezed to a sliver by a height-locked parent
 */
import { setTimeout as sleep } from 'node:timers/promises';
import fs from 'node:fs';

const args = Object.fromEntries(process.argv.slice(2).map(a => {
  const i = a.indexOf('=');
  return i === -1 ? [a.replace(/^--/, ''), '1'] : [a.slice(2, i), a.slice(i + 1)];
}));
const W = Number(args.width || 390), H = Number(args.height || 844);
const PORT = args.port || '9711';
const BASE = args.base || 'https://127.0.0.1:8080';
/* This harness logs in as a real admin, so credentials are never defaulted —
   a baked-in password leaks the moment the file is committed or served.
   Pass --user/--pass, or set CSAI_AUDIT_USER / CSAI_AUDIT_PASS. */
const USER = args.user || process.env.CSAI_AUDIT_USER || '';
const PASS = args.pass || process.env.CSAI_AUDIT_PASS || '';
if (!USER || !PASS) {
  console.error('Refusing to run without credentials: pass --user=... --pass=... or set CSAI_AUDIT_USER / CSAI_AUDIT_PASS.');
  process.exit(2);
}
const OUT = args.out || `/tmp/cs-audit/${W}x${H}`;
const SPA_PAGES = (args.pages || 'dashboard,chat,projects,vulnerabilities,tasks,workflows,asset-overview,asset-library,info-collect,hitl,tool-guard,webshell,c2-listeners,c2-sessions,c2-tasks,c2-payloads,c2-events,c2-profiles,chat-files,mcp-monitor,mcp-management,knowledge-management,knowledge-retrieval-logs,roles-management,platform-rbac,skills-monitor,skills-management,agents-management,settings,audit-log,monitor').split(',');
const pages = args.all ? SPA_PAGES.map(p => ({ hash: p }))
  : args.pages ? args.pages.split(',').map(p => ({ hash: p }))
  : (args.url ? [{ url: args.url }] : [{ hash: args.route || 'dashboard' }]);

fs.mkdirSync(OUT, { recursive: true });
const CH = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const chrome = await import('node:child_process').then(m => m.spawn(CH, ['--headless=new', `--remote-debugging-port=${PORT}`, `--user-data-dir=/tmp/cs-au-${PORT}`, '--ignore-certificate-errors', '--no-sandbox', 'about:blank'], { stdio: 'ignore' }));
let ws;
for (let i = 0; i < 80; i++) { try { const r = await fetch(`http://127.0.0.1:${PORT}/json/version`); ws = new WebSocket((await r.json()).webSocketDebuggerUrl); await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; setTimeout(rej, 4000); }); break; } catch { await sleep(300); } }
if (!ws) { console.error('chrome devtools unreachable'); process.exit(1); }
let idc = 0; const pending = new Map();
ws.onmessage = e => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { const { res, rej } = pending.get(m.id); pending.delete(m.id); m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result); } };
const send = (method, params = {}, sessionId) => new Promise((res, rej) => { const id = ++idc; pending.set(id, { res, rej }); ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) })); });
const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
for (const m of ['Page.enable', 'Runtime.enable', 'Security.enable']) await send(m, {}, sessionId);
await send('Security.setIgnoreCertificateErrors', { ignore: true }, sessionId);
await send('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 2, mobile: true }, sessionId);
await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 }, sessionId);
await send('Network.setUserAgentOverride', { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1' }, sessionId);
const ev = async e => { const r = await send('Runtime.evaluate', { expression: e, awaitPromise: true, returnByValue: true }, sessionId); if (r.exceptionDetails) return { __err: (r.exceptionDetails.exception?.description || r.exceptionDetails.text).slice(0, 200) }; return r.result?.value; };

// ---------------- in-page detector ----------------
const DETECT = `(function(){
  var vw=innerWidth, vh=innerHeight, res={};
  function nm(el){
    var s=el.tagName.toLowerCase();
    if(el.id) s+='#'+el.id;
    else if(typeof el.className==='string'&&el.className.trim()){
      var c=el.className.trim().split(/\\s+/).filter(function(x){return !/^is-|^active$|^has-/.test(x)});
      s+='.'+(c[0]||''); if(c[1]&&/layout|sidebar|content|panel|main|nav|list|detail|grid|row|header|body|wrap|table/i.test(c[1])) s+='.'+c[1];
    }
    return s;
  }
  function path(el){ var p=[],n=el; for(var i=0;i<3&&n&&n.tagName;i++){p.unshift(nm(n));n=n.parentElement} return p.join('>') }
  function txt(el){ var t=''; for(var i=0;i<el.childNodes.length;i++){ var c=el.childNodes[i]; if(c.nodeType===3) t+=c.nodeValue; } return (t||el.innerText||'').trim(); }
  function vis(el){ var c=getComputedStyle(el); if(c.display==='none'||c.visibility==='hidden') return false; var r=el.getBoundingClientRect(); return r.width>2&&r.height>2 }
  function scrollAncestor(el, axis){
    var a=el.parentElement;
    while(a){ var c=getComputedStyle(a);
      if(axis==='x' && /auto|scroll/.test(c.overflowX)) return a;
      if(axis==='y' && /auto|scroll/.test(c.overflowY)) return a;
      if(a===document.body) break; a=a.parentElement;
    } return null;
  }
  function clipAncestor(el){
    var a=el.parentElement;
    while(a){ var c=getComputedStyle(a); if(c.overflowX==='hidden'||c.overflow==='hidden') return a; if(a===document.body)break; a=a.parentElement }
    return null;
  }

  var all=[...document.querySelectorAll('body *')].filter(vis);
  // SVG internals stack by design; treating them as layout boxes is pure noise
  all=all.filter(function(el){ return !el.ownerSVGElement });

  /* 1 + 3 : past the right edge */
  var overflow=[], unreachable=[];
  all.forEach(function(el){
    var c=getComputedStyle(el); var r=el.getBoundingClientRect();
    if(c.position==='fixed') return;
    if(r.right<=vw+2 && r.left>=-2) return;
    if(el.children.length>2) return;
    if(scrollAncestor(el,'x')) return;                 // intentional swipe region
    var item={ el:nm(el), p:path(el), right:Math.round(r.right), left:Math.round(r.left), w:Math.round(r.width), over:Math.round(Math.max(r.right-vw, vw-r.left)) };
    if(clipAncestor(el)) unreachable.push(item); else overflow.push(item);
  });

  /* 2 : squeezed panes */
  var squeezed=[];
  all.forEach(function(el){
    var c=getComputedStyle(el);
    if(c.display!=='flex'&&c.display!=='grid') return;
    if(c.position==='fixed'||c.position==='absolute') return;
    var rowish = c.display==='grid' ? c.gridTemplateColumns.split(' ').filter(function(x){return x.trim()}).length>1 : c.flexDirection.indexOf('row')===0;
    if(!rowish) return;
    var kids=[...el.children].filter(vis); if(kids.length<2) return;
    var narrow=kids.filter(function(k){
      var kw=k.getBoundingClientRect().width;
      if(kw>=250) return false;
      // a small *control* (button, switch, badge) beside text is normal design;
      // a small *content container* is the squeeze bug we are looking for
      if(k.matches('button,a,input,select,textarea,svg,img,.btn,.btn-primary,.btn-secondary,.btn-small,.btn-sm,.btn-icon,.switch,.badge,.pill')) return false;
      if(k.children.length<2 && !k.querySelector('h1,h2,h3,h4,table,form,.form-group,p')) return false;
      return !!k.querySelector('h1,h2,h3,h4,label,button,input,select,textarea,table,.btn-primary,.form-group,[class*="title"],[class*="card"]');
    });
    if(!narrow.length) return;
    squeezed.push({ el:nm(el), p:path(el), dir:(c.display==='grid'?'grid':'flex'), minW:Math.round(Math.min.apply(null,narrow.map(function(k){return k.getBoundingClientRect().width}))), kids:kids.map(function(k){return nm(k)+':'+Math.round(k.getBoundingClientRect().width)}).slice(0,4).join(' ')});
  });

  /* 4 : touch scrolling */
  var scrollers=[], noTouch=[], rootBad=[];
  all.forEach(function(el){
    var c=getComputedStyle(el);
    if(!/auto|scroll/.test(c.overflowY)&&!/auto|scroll/.test(c.overflowX)) return;
    var sy=el.scrollHeight>el.clientHeight+2, sx=el.scrollWidth>el.clientWidth+2;
    if(!sy&&!sx) return;
    scrollers.push({el:nm(el)});
    var okY=!sy||/pan-y/.test(c.touchAction), okX=!sx||/pan-x/.test(c.touchAction);
    if(!(okY&&okX)) noTouch.push({ el:nm(el), p:path(el), touch:c.touchAction, needY:sy, needX:sx });
  });
  [[ 'html', document.documentElement ], ['body', document.body]].forEach(function(pair){
    var c=getComputedStyle(pair[1]);
    if(c.overscrollBehavior&&c.overscrollBehavior!=='auto') rootBad.push(pair[0]+' overscroll='+c.overscrollBehavior);
  });

  /* 5 : truncated text with no way to reveal it */
  var truncated=[];
  all.forEach(function(el){
    var c=getComputedStyle(el);
    if(el.children.length>1) return;
    var t=txt(el); if(t.length<3) return;
    var r=el.getBoundingClientRect(); if(r.width<20) return;
    var clippedX = el.scrollWidth>el.clientWidth+6 && /hidden|clip/.test(c.overflowX);
    var clippedY = el.scrollHeight>el.clientHeight+6 && /hidden|clip/.test(c.overflowY) && parseFloat(c.lineHeight||0)>0 && (el.scrollHeight-el.clientHeight) > parseFloat(c.lineHeight)*1.6;
    if(!clippedX&&!clippedY) return;
    if(clippedX && scrollAncestor(el,'x')) return;   // swipeable, acceptable
    truncated.push({ el:nm(el), p:path(el), lost:clippedX?(el.scrollWidth-el.clientWidth):(el.scrollHeight-el.clientHeight), axis:clippedX?'x':'y', t:t.slice(0,26) });
  });

  /* 6 : tap targets */
  var SEL='button,a[href],input:not([type=hidden]),select,textarea,[onclick],[role=button],[role=tab],.btn,.btn-primary,.btn-secondary,.btn-small,.btn-sm,.btn-icon,.modal-close,.nav-item-content,.nav-submenu-item,.settings-custom-select-trigger,.tab';
  var tiny=[];
  [...document.querySelectorAll(SEL)].forEach(function(el){
    if(!vis(el)) return;
    var r=el.getBoundingClientRect();
    if(r.width>=44||r.height>=44) if(r.width>=40&&r.height>=40) return;
    if(r.width>=44&&r.height>=40) return;
    if(el.type==='checkbox'||el.type==='radio') { if(r.width>=20&&r.height>=20) return; }
    tiny.push({ el:nm(el), p:path(el), w:Math.round(r.width), h:Math.round(r.height), t:(el.innerText||el.value||'').trim().slice(0,16) });
  });

  /* 7 : overlapping static siblings */
  var overlaps=[];
  var byParent=new Map();
  all.forEach(function(el){ var p=el.parentElement; if(!p) return; if(!byParent.has(p)) byParent.set(p,[]); byParent.get(p).push(el) });
  byParent.forEach(function(kids){
    for(var i=0;i<kids.length;i++) for(var k=i+1;k<kids.length;k++){
      var a=kids[i], b=kids[k];
      if(a.contains(b)||b.contains(a)) continue;            // parent/child share a box by definition
      var ca=getComputedStyle(a), cb=getComputedStyle(b);
      if(ca.position==='absolute'||cb.position==='absolute'||ca.position==='fixed'||cb.position==='fixed') continue;
      if(ca.position==='sticky'||cb.position==='sticky') continue;
      if(ca.display==='inline'||cb.display==='inline') continue;   // inline text runs report shared boxes, not layout bugs
      var ra=a.getBoundingClientRect(), rb=b.getBoundingClientRect();
      var ox=Math.min(ra.right,rb.right)-Math.max(ra.left,rb.left);
      var oy=Math.min(ra.bottom,rb.bottom)-Math.max(ra.top,rb.top);
      if(ox<=8||oy<=8) continue;
      var area=Math.min(ra.width*ra.height, rb.width*rb.height);
      if(!area) continue;
      var pct=Math.round(ox*oy/area*100);
      if(pct<22) continue;
      overlaps.push({ a:nm(a), b:nm(b), pct:pct, ox:Math.round(ox), oy:Math.round(oy), p:path(a) });
    }
  });

  /* 8 : viewport fit */
  var de=document.documentElement;
  var viewport={ innerH:vh, docClientH:de.clientHeight, bodyClientH:document.body.clientHeight, docScrollH:de.scrollHeight, bodyOverflowY:getComputedStyle(document.body).overflowY };

  /* 9 : collapsed scroller — an inner overflow:auto region squeezed to a sliver
     by a height-locked flex parent, while that parent itself cannot scroll.
     Reads to the user as "this page will not scroll at all". */
  var slivers=[];
  all.forEach(function(el){
    var c=getComputedStyle(el);
    if(!/auto|scroll/.test(c.overflowY)) return;
    var r=el.getBoundingClientRect();
    if(r.height>=140 || r.height<2) return;
    if(el.scrollHeight < Math.max(300, r.height*2)) return;
    var p=scrollAncestor(el,'y');
    // if a parent can scroll, the content is still reachable; only flag when it cannot
    var outerLocked = !p;
    slivers.push({ el:nm(el), p:path(el), boxH:Math.round(r.height), contentH:el.scrollHeight,
                   hidden: el.scrollHeight - Math.round(r.height), outerLocked: outerLocked,
                   parentOvfY: p?getComputedStyle(p).overflowY:'none', flex:c.flex, minH:c.minHeight });
  });

  /* 12 : crushed panel. A visible box squeezed to a sliver by a height-locked
     parent while holding real content. Check 9 misses this class because it only
     looks at overflow-y:auto regions that are at least 2px tall — a panel left
     0px tall inside its own overflow:hidden grid row never registers as a
     scroller, or as visible, in the first place. */
  var crushed=[];
  [...document.querySelectorAll('body *')].forEach(function(el){
    if(el.hasAttribute('hidden')) return;
    var c=getComputedStyle(el);
    if(c.display==='none'||c.visibility==='hidden') return;
    var r=el.getBoundingClientRect();
    if(r.width<40||r.height>=40||el.clientHeight>40) return;
    if(el.scrollHeight<300) return;
    var selfClips=(c.overflowY==='hidden'||c.overflowY==='clip');
    if(!selfClips&&scrollAncestor(el,'y')) return;      // still swipeable, awkward but reachable
    crushed.push({ el:nm(el), p:path(el), boxH:Math.round(r.height), clientH:el.clientHeight,
                   contentH:el.scrollHeight, lost:el.scrollHeight-Math.max(el.clientHeight,Math.round(r.height)),
                   ovfY:c.overflowY, parentOvfY:(function(){var a=el.parentElement;return a?getComputedStyle(a).overflowY:'-'})() });
  });

  function top(arr,n){ var m=new Map(); arr.forEach(function(o){ var k=o.el||o.a; if(!m.has(k)) m.set(k,o) }); return [...m.values()].slice(0,n) }

  /* 11 : nibbled overlay. A small absolutely-positioned element with text — a badge,
     a count bubble, a dot label — hanging past the padding edge of a scroll/clip box
     that cannot swipe to it. Check 10's area metric is deliberately lenient and lets
     these through; on a 18px badge losing 5px is the whole defect.
     Badges are display:none until something is unread, so the hidden ones are
     force-shown for one synchronous layout pass and restored, as in check 10. */
  var nibbled=[];
  [...document.querySelectorAll('body *')].forEach(function(el){
    var c=getComputedStyle(el);
    if(c.position!=='absolute') return;
    if(c.visibility==='hidden') return;
    var hidden=c.display==='none';
    if(hidden&&!/display\\s*:\\s*none/.test(el.getAttribute('style')||'')) return;
    var t=(el.innerText||el.textContent||'').trim();
    if(!t||t.length>12) return;                       // labels only, not decorative blocks
    var prev=el.style.display;
    if(hidden) el.style.display='inline-block';
    var r=el.getBoundingClientRect();
    var p=el.parentElement, verdict=null;
    while(p&&p!==document.body){
      var pc=getComputedStyle(p);
      if(pc.overflowX!=='visible'||pc.overflowY!=='visible'){
        var q=p.getBoundingClientRect();
        var canY=/auto|scroll/.test(pc.overflowY)&&p.scrollHeight>p.clientHeight+2;
        var canX=/auto|scroll/.test(pc.overflowX)&&p.scrollWidth>p.clientWidth+2;
        var out={ top:canY?0:Math.round(q.top-r.top), bottom:canY?0:Math.round(r.bottom-q.bottom),
                  left:canX?0:Math.round(q.left-r.left), right:canX?0:Math.round(r.right-q.right) };
        var worst=Math.max(out.top,out.bottom,out.left,out.right);
        if(worst>2) verdict={ el:nm(el), p:path(el), text:t.slice(0,10), clipped:out, worst:worst,
                              by:nm(p), ovf:pc.overflowX+'/'+pc.overflowY, wasHidden:hidden };
        break;                                        // nearest clip box decides it
      }
      if(pc.transform!=='none'||pc.perspective!=='none') break;
      p=p.parentElement;
    }
    el.style.display=prev;
    if(!verdict) return;
    if(verdict.worst>Math.round(r.height*0.9)) return;  // pushed out entirely — that is check 10's case
    if(r.width<4||r.height<4||r.width>90||r.height>44) return;
    nibbled.push(verdict);
  });

  /* 10 : clipped popover. A panel that is display:none at rest and opens into a
     region an ancestor scroll/clip box hides. Every other probe misses it
     because it is invisible until tapped — to the user the trigger "does nothing".
     Each candidate is forced open for one synchronous layout pass, then restored.
     Verdict is the share of the panel a finger could actually see. */
  var clipped=[];
  [...document.querySelectorAll('body *')].forEach(function(el){
    var c=getComputedStyle(el);
    if(c.position!=='absolute'&&c.position!=='fixed') return;
    if(c.display!=='none') return;
    if(!/display\\s*:\\s*none/.test(el.getAttribute('style')||'')) return;   // opened via inline style
    if(!(el.children.length||el.innerText)) return;

    var clip=null;
    if(c.position==='absolute'){
      var cb=el.parentElement;
      while(cb&&getComputedStyle(cb).position==='static') cb=cb.parentElement;
      if(!cb) return;                       // containing block is the viewport: nothing clips it
      for(var a=cb;a&&a!==document.body;a=a.parentElement){
        var ac=getComputedStyle(a);
        if(ac.overflowX!=='visible'||ac.overflowY!=='visible'){ clip=a; break }
        if(ac.transform!=='none'||ac.perspective!=='none') break;   // becomes the containing block
      }
      if(!clip) return;
    }else{
      // context menus parked in flow until the opener writes left/top at the cursor
      if(c.top==='auto'&&c.left==='auto') return;
    }

    var prev=el.style.display;
    el.style.display='block';
    var r=el.getBoundingClientRect();
    var canY=false,canX=false;
    var cL=0,cT=0,cR=vw,cB=vh;
    if(clip){
      var q=clip.getBoundingClientRect(), cc=getComputedStyle(clip);
      canY=/auto|scroll/.test(cc.overflowY)&&clip.scrollHeight>clip.clientHeight+2;
      canX=/auto|scroll/.test(cc.overflowX)&&clip.scrollWidth>clip.clientWidth+2;
      cL=q.left;cT=q.top;cR=q.right;cB=q.bottom;
      if(canY){ cT=0;cB=vh }   // swipeable within the clip, so not lost
      if(canX){ cL=0;cR=vw }
    }
    el.style.display=prev;

    if(r.width<8||r.height<8) return;
    var iw=Math.min(r.right,cR)-Math.max(r.left,cL);
    var ih=Math.min(r.bottom,cB)-Math.max(r.top,cT);
    var pct=iw>0&&ih>0? Math.round(iw*ih/(r.width*r.height)*100) : 0;
    if(pct>=25) return;
    clipped.push({ el:nm(el), p:path(el), pos:c.position, clip:clip?nm(clip):'viewport', visiblePct:pct,
                   panel:{y:Math.round(r.y),w:Math.round(r.width),h:Math.round(r.height)},
                   clipBox:clip?{y:Math.round(cT),h:Math.round(cB-cT)}:null,
                   swipesY:canY, swipesX:canX });
  });

  return {
    vw:vw, vh:vh,
    counts:{ overflow:overflow.length, unreachable:unreachable.length, squeezed:squeezed.length,
             scrollers:scrollers.length, noTouch:noTouch.length, rootBad:rootBad.length,
             truncated:truncated.length, tiny:tiny.length, overlaps:overlaps.length, slivers:slivers.length,
             clipped:clipped.length, nibbled:nibbled.length, crushed:crushed.length },
    overflow:top(overflow.sort(function(a,b){return b.over-a.over}),6),
    unreachable:top(unreachable.sort(function(a,b){return b.over-a.over}),6),
    squeezed:top(squeezed.sort(function(a,b){return a.minW-b.minW}),6),
    noTouch:top(noTouch,6),
    truncated:top(truncated.sort(function(a,b){return b.lost-a.lost}),8),
    tiny:top(tiny.sort(function(a,b){return (a.w*a.h)-(b.w*b.h)}),6),
    overlaps:top(overlaps.sort(function(a,b){return b.pct-a.pct}),6),
    slivers:top(slivers.sort(function(a,b){return b.hidden-a.hidden}),6),
    clipped:top(clipped.sort(function(a,b){return a.visiblePct-b.visiblePct}),6),
    nibbled:top(nibbled.sort(function(a,b){return b.worst-a.worst}),6),
    crushed:top(crushed.sort(function(a,b){return b.lost-a.lost}),6),
    rootBad:rootBad, viewport:viewport
  };
})()`;

// ---------------- drive it ----------------
async function openTarget(pg) {
  if (pg.url) { await send('Page.navigate', { url: BASE + pg.url }, sessionId); await sleep(5000); return pg.url; }
  await ev(`location.hash=${JSON.stringify(pg.hash)}`); await sleep(3000);
  return pg.hash;
}

await send('Page.navigate', { url: BASE + '/' }, sessionId); await sleep(3000);
await ev(`(()=>{const u=document.getElementById('login-username'),p=document.getElementById('login-password');if(u&&p){u.value=${JSON.stringify(USER)};p.value=${JSON.stringify(PASS)};document.getElementById('login-form').requestSubmit();}})()`);
await sleep(7000);

const report = {};
let grand = { overflow: 0, unreachable: 0, squeezed: 0, noTouch: 0, rootBad: 0, truncated: 0, tiny: 0, overlaps: 0, slivers: 0, clipped: 0, nibbled: 0, crushed: 0 };
const W1 = 22;
for (const pg of pages) {
  const label = await openTarget(pg);
  const r = await ev(DETECT);
  if (!r || r.__err) { console.log(label, 'DETECT ERROR', r && r.__err); continue; }
  report[label] = r;
  const c = r.counts;
  grand.overflow += c.overflow; grand.unreachable += c.unreachable; grand.squeezed += c.squeezed;
  grand.noTouch += c.noTouch; grand.rootBad += c.rootBad; grand.truncated += c.truncated;
  grand.tiny += c.tiny; grand.overlaps += c.overlaps; grand.slivers += (c.slivers||0); grand.clipped += (c.clipped||0); grand.nibbled += (c.nibbled||0); grand.crushed += (c.crushed||0);
  const bad = c.overflow + c.unreachable + c.noTouch + c.rootBad + c.truncated + c.overlaps + (c.slivers||0) + (c.clipped||0) + (c.nibbled||0) + (c.crushed||0);
  console.log(`${label.padEnd(W1)} ovf=${String(c.overflow).padStart(2)} unreach=${String(c.unreachable).padStart(2)} sliver=${String(c.slivers).padStart(2)} clip=${String(c.clipped).padStart(2)} nibble=${String(c.nibbled).padStart(2)} crush=${String(c.crushed).padStart(2)} noTouch=${String(c.noTouch).padStart(2)} trunc=${String(c.truncated).padStart(2)} overlap=${String(c.overlaps).padStart(2)} tap<44=${String(c.tiny).padStart(3)} ${bad ? '' : '  OK'}`);
  const shot = await send('Page.captureScreenshot', { format: 'png' }, sessionId);
  fs.writeFileSync(`${OUT}/${label.replace(/[^a-z0-9-]/gi, '_')}.png`, Buffer.from(shot.data, 'base64'));
}
fs.writeFileSync(`${OUT}/audit.json`, JSON.stringify(report, null, 2));

console.log(`\n================ AUDIT @${W}x${H} ================`);
console.log(`pages checked        ${Object.keys(report).length}`);
console.log(`1 overflow           ${grand.overflow}`);
console.log(`3 unreachable        ${grand.unreachable}   (controls you cannot reach at all)`);
console.log(`2 squeezed panes     ${grand.squeezed}`);
console.log(`4 scrollers w/o touch ${grand.noTouch}   rootOverscrollBad ${grand.rootBad}`);
console.log(`5 truncated text     ${grand.truncated}`);
console.log(`7 overlapping boxes  ${grand.overlaps}`);
console.log(`9 collapsed scroller ${grand.slivers}   (page cannot scroll at all)`);
console.log(`10 clipped popover   ${grand.clipped}   (dropdown opens into a hidden region)`);
console.log(`11 nibbled overlay   ${grand.nibbled}   (badge/label clipped by its strip)`);
console.log(`12 crushed panel     ${grand.crushed}   (sliver holding real content)`);
console.log(`6 small tap targets  ${grand.tiny}`);
console.log(`report -> ${OUT}/audit.json`);

// dump the worst instances of each class for triage
for (const [page, r] of Object.entries(report)) {
  for (const k of ['crushed', 'nibbled', 'clipped', 'slivers', 'unreachable', 'truncated', 'overlaps', 'squeezed', 'noTouch', 'overflow']) {
    if (!r[k]?.length) continue;
    console.log(`\n--- ${k} @ ${page}`);
    r[k].slice(0, 4).forEach(o => console.log('   ' + JSON.stringify(o).slice(0, 220)));
  }
}
chrome.kill(); process.exit(0);
