const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const { chromium } = require('playwright');
const root = path.join(__dirname, '..');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');
const template = read('web/templates/index.html');
const region = template.match(/<div id="page-deep-assessments"[^>]*><div id="deep-assessments-root"><\/div><\/div>/)?.[0];
assert.ok(region, 'independent page missing');
const navOrder = [...template.slice(template.indexOf('<nav class="main-sidebar-nav">'), template.indexOf('</nav>', template.indexOf('<nav class="main-sidebar-nav">'))).matchAll(/class="nav-item(?: [^"]*)?" data-page="([^"]+)"/g)].map(m => m[1]);
assert.equal(navOrder[navOrder.indexOf('chat') + 1], 'deep-assessments');
const sample = id => ({ id, name: '隔离样例 ' + id, status: 'draft', revision: 0, objective: '检查本地夹具', scope: '仅本地', criteria: '引用真实证据', strategy: '', summary: '', question: '', concurrency: 2, remaining_seconds: 172800, token_budget: 2000000, tokens_used: 0, max_rounds: 512, rounds: 0, updated_at: new Date().toISOString(), steps: [], metrics: {}, auto_resume: true });
async function fixture(t) {
 const state = { tasks: new Map(Array.from({ length: 24 }, (_, i) => ['t' + i, sample('t' + i)])), requests: [], errors: [], sharedAssets: [], linkedAssets: new Map() };
 const locale = JSON.stringify(JSON.parse(read('web/static/i18n/zh-CN.json'))).replaceAll('<', '\\u003c');
 const scripts = ['router', 'auth', 'assessment-node-panel','assessment-exploration-views','deep-assessment-graph', 'deep-assessments'].map(x => `<script src="/static/js/${x}.js"></script>`).join('');
 const html = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">${['style', 'mobile', 'deep-assessments', 'design-system'].map(x=>`<link rel="stylesheet" href="/static/css/${x}.css">`).join('')}</head><body><div class="container"><main class="main-layout"><section class="content-area">${region}<div class="page" id="page-chat"></div><div class="page" id="page-tasks"></div></section></main></div><script>window.t=k=>k.split('.').reduce((o,p)=>o&&o[p],${locale})||k;window.loadConversation=async id=>{window.visitedConversation=id};window.initializeChatUI=()=>{};window.refreshAppData=async()=>{};</script><script src="/static/vendor/cytoscape.min.js"></script><script>const originalGraph=window.cytoscape;window.cytoscape=(...args)=>{const chart=originalGraph(...args);window.fixtureGraph=chart;return chart};</script>${scripts}<script>document.removeEventListener('DOMContentLoaded',initializeApp);saveAuth('fixture-token',new Date(Date.now()+3600000),{user:{id:'fixture',username:'fixture'},permissions:['assessments:read','assessments:write','assessments:delete','agent:execute','chat:read','chat:write','tasks:read','asset:read'],scope:'own'});</script></body></html>`;
 const server = http.createServer(async (req,res)=>{
  const url = new URL(req.url,'http://fixture');
  const send=(code,body)=>{res.statusCode=code;res.setHeader('Content-Type','application/json; charset=utf-8');res.end(JSON.stringify(body));};
  if(url.pathname.startsWith('/static/')){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');return res.end(read('web'+url.pathname));}
  if(url.pathname==='/'){res.setHeader('Content-Type','text/html; charset=utf-8');return res.end(html);}
  if(url.pathname==='/favicon.ico'){res.statusCode=204;return res.end();}
  let raw='';for await(const chunk of req)raw+=chunk;const body=raw?JSON.parse(raw):null;
  state.requests.push({path:url.pathname,method:req.method,body,query:Object.fromEntries(url.searchParams),authorization:req.headers.authorization});
  if(req.headers.authorization!=='Bearer fixture-token')return send(401,{error:'authentication missing'});
  if(url.pathname==='/api/deep-assessments/options')return send(200,{models:[{id:'fixture',name:'Fixture'}],roles:['review'],projects:[]});
  if(url.pathname==='/api/deep-assessments'){
   if(req.method==='POST'){const task={...sample('new'),...body};state.tasks.set(task.id,task);return send(201,{task});}
   const filtered=[...state.tasks.values()].filter(t=>!!t.archived===(url.searchParams.get('archived')==='true')).filter(t=>(!url.searchParams.get('q')||t.name.includes(url.searchParams.get('q')))&&(!url.searchParams.get('status')||t.status===url.searchParams.get('status')));
   const offset=Number(url.searchParams.get('offset')||0),limit=Number(url.searchParams.get('limit')||20);return send(200,{tasks:filtered.slice(offset,offset+limit),has_more:filtered.length>offset+limit});
  }
  const match=url.pathname.match(/^\/api\/deep-assessments\/([^/]+)(?:\/(.*))?$/);
  if(!match)return send(404,{error:'unhandled route'});
  const task=state.tasks.get(match[1]);if(!task)return send(404,{error:'missing task'});
  const graph=task.graph||{nodes:[{id:'goal',kind:'goal',state:task.status,title:task.name,body:task.objective,source:{}}],edges:[],total:0,folded:0,next_offset:0,has_more:false};
  if(match[2]==='assets'){
   if(req.method==='POST'){state.linkedAssets.set(task.id,body.asset_ids);return send(200,{saved:true});}
   if(url.searchParams.get('asset_id')){const offset=Number(url.searchParams.get('offset')||0),rows=Array.from({length:51},(_,i)=>({id:'ep-'+i,method:'GET',path:'/endpoint/'+i,title:'fixture'}));return send(200,{endpoints:rows.slice(offset,offset+50),has_more:offset+50<rows.length,next_offset:offset+50});}
   const assets=state.sharedAssets.filter(a=>url.searchParams.get('scope')==='all'||(state.linkedAssets.get(task.id)||[]).includes(a.id));
   return send(200,{assets,nodes:assets.flatMap(a=>[{id:'domain:example.test',kind:'domain',title:'example.test',body:'',asset_ids:[a.id]},{id:'asset:'+a.id,kind:'service',title:a.host,body:JSON.stringify(a),asset_ids:[a.id]},{id:'endpoint:'+a.id,kind:'endpoint',title:'GET /fixture',body:'original endpoint',asset_ids:[a.id]}]),edges:assets.flatMap(a=>[{source:'domain:example.test',target:'asset:'+a.id,relation:'exposes'},{source:'asset:'+a.id,target:'endpoint:'+a.id,relation:'contains'}]),total:assets.length,next_offset:assets.length,has_more:false});
  }
  if(match[2]==='facts'){const facts=(task.facts||[]).filter(f=>!url.searchParams.get('confidence')||f.confidence===url.searchParams.get('confidence'));return send(200,{facts,next_offset:facts.length,has_more:false});}
  if(match[2]?.startsWith('steps/'))return send(200,{step:task.steps.find(s=>s.id===match[2].slice(6))});
  if(match[2]==='graph')return send(200,graph);
  if(match[2]==='graph/node'){const node=graph.nodes.find(n=>n.id===url.searchParams.get('node_id'));if(!node)return send(404,{error:'missing node'});return send(200,{node,next_offset:node.body.length,has_more:false});}
  if(match[2]==='events')return send(200,{events:[]});
  if(match[2]==='receipts')return send(200,{receipts:[],next_offset:0,has_more:false});
  if(match[2]==='report'){res.setHeader('Content-Type','text/markdown');return res.end('# fixture report');}
  if(match[2]?.startsWith('control/')){
   if(body.revision!==task.revision)return send(409,{error:'stale revision'});
   const action=match[2].split('/')[1];if(action==='start')task.status='running';if(action==='pause')task.status='paused';if(action==='archive'){task.archived=true;if(!['draft','completed','cancelled'].includes(task.status))task.status='paused';}if(action==='restore')task.archived=false;if(action==='guidance')task.summary=body.message;if(action==='budget')Object.assign(task,body);task.revision++;
  } else if(req.method==='DELETE') state.tasks.delete(task.id);
  return send(200,{task});
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const executablePath=process.env.CHROME_PATH||(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
 const browser=await chromium.launch(executablePath?{executablePath}:{});
 t.after(async()=>{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));});
 const page=await browser.newPage({viewport:{width:1440,height:1000},acceptDownloads:true});page.setDefaultTimeout(5000);
 page.on('pageerror',error=>state.errors.push(error.message));page.on('dialog',d=>d.accept());
 await page.goto(`http://127.0.0.1:${server.address().port}/#deep-assessments`);
 await page.locator('[data-da-action="select"]').first().waitFor();
 return {page,state};
}

test('deep assessment is independent, paginates and only creates drafts',async t=>{
 const {page,state}=await fixture(t);
 assert.equal(await page.locator('.da-list-item').count(),20);
 await page.locator('[data-da-action="next"]').click();await page.waitForFunction(()=>document.querySelectorAll('.da-list-item').length===4);
 await page.locator('[data-da-action="create"]').click();
 await page.locator('[name="name"]').fill('Two day fixture');await page.locator('[name="objective"]').fill('Read fixture evidence');
 const scope=page.getByRole('textbox',{name:'补充限制与排除项（选填）',exact:true});
 const criteria=page.getByRole('textbox',{name:'验收条件（选填）',exact:true});
 assert.equal(await scope.getAttribute('required'),null);assert.equal(await criteria.getAttribute('required'),null);
 assert.match(await scope.getAttribute('placeholder'),/例如：排除支付接口/);
 assert.match(await criteria.getAttribute('placeholder'),/例如：报告列明已测范围/);
 assert.match(await page.locator('#da-scope-hint').innerText(),/留空沿用目标/);
 assert.match(await page.locator('#da-criteria-hint').innerText(),/留空沿用目标/);
 assert.equal(await scope.inputValue(),'');assert.equal(await criteria.inputValue(),'');
 assert.equal(await page.locator('#da-create-form').evaluate(form=>form.checkValidity()),true);
 assert.equal(await page.locator('[name="hours"],[name="token_budget"],[name="max_rounds"]').count(),0);
 await page.locator('#da-create-form button[type="submit"]').click();await page.waitForFunction(()=>document.querySelector('#da-title').textContent.includes('Two day fixture'));
 const create=state.requests.find(r=>r.method==='POST'&&r.path==='/api/deep-assessments');assert.equal(create.body.remaining_seconds,undefined);assert.equal(create.body.token_budget,undefined);assert.equal(create.body.max_rounds,undefined);assert.equal(create.body.auto_resume,true);assert.equal(create.body.scope,'');assert.equal(create.body.criteria,'');
 assert.equal(state.requests.filter(r=>r.path.endsWith('/control/start')).length,0);
 await page.locator('[data-da-action="start"]').click();await page.locator('[data-da-action="pause"]').waitFor();
 await page.locator('#da-guidance-panel > summary').click(); await page.locator('#da-guidance').fill('Keep this unsent text');await page.evaluate(()=>DeepAssessments.refresh());assert.equal(await page.locator('#da-guidance').inputValue(),'Keep this unsent text');
 await page.locator('[data-da-action="pause"]').click();await page.locator('[data-da-action="start"]').waitFor();
 const download=page.waitForEvent('download');await page.locator('[data-da-action="report"]').click();assert.match((await download).suggestedFilename(),/^assessment-/);
 assert.ok(state.requests.every(r=>!r.path.startsWith('/api/batch-tasks')&&r.path!=='/api/tasks'));
 assert.deepEqual(state.errors,[]);
});

test('assessment escapes evidence and fits mobile and dark layouts',async t=>{
 const {page,state}=await fixture(t);const task=state.tasks.get('t0');task.name='<img src=x onerror="window.injected=1">';task.steps=[{id:'s1',title:'Evidence review',instruction:'Read fixture',summary:'<script>window.injected=1</script>',status:'completed',depends_on:[],attempts:3,conversation_id:'fixture-conversation',evidence:['receipt']}];
 await page.evaluate(()=>DeepAssessments.refresh());await page.locator('[data-da-action="select"][data-id="t0"]').click();
 await page.locator('[data-da-action="tab"][data-tab="plan"]').click();await page.locator('.da-step summary').click();
 assert.equal(await page.evaluate(()=>window.injected),undefined);assert.equal(await page.locator('#da-title img').count(),0);
 await page.locator('#da-guidance-panel > summary').click(); await page.locator('#da-guidance').fill('Unsent direction');await page.evaluate(()=>DeepAssessments.refresh());assert.equal(await page.locator('.da-step[open]').count(),1);
 const screenshotRoot=process.env.CSAI_SCREENSHOT_DIR;
 if(screenshotRoot){fs.mkdirSync(screenshotRoot,{recursive:true});await page.screenshot({path:path.join(screenshotRoot,'deep-assessments-desktop.png'),fullPage:true});}
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.setAttribute('data-theme','dark'));
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 await page.locator('[data-da-action="back-list"]').click();
 await page.locator('[data-da-action="create"]').click();
 await page.locator('#da-form-drawer').waitFor({state:'visible'});
 await page.waitForFunction(()=>Math.abs(document.getElementById('da-form-drawer').getBoundingClientRect().right-innerWidth)<1);
 const box=await page.locator('#da-form-drawer').boundingBox();assert.ok(box.x>=0&&box.x+box.width<=391);assert.ok(Math.abs(box.y)<1&&Math.abs(box.height-844)<1);
 if(screenshotRoot)await page.screenshot({path:path.join(screenshotRoot,'deep-assessments-mobile.png'),fullPage:true});
 assert.deepEqual(state.errors,[]);
});


test('assessment creation uses a right drawer with backdrop, Escape and button dismissal',async t=>{
 const {page,state}=await fixture(t);
 await page.locator('[data-da-action="create"]').click();
 const drawer=page.locator('#da-form-drawer');await drawer.waitFor({state:'visible'});
 // Wait for the short slide transition before checking final viewport geometry.
 await page.waitForFunction(()=>Math.abs(document.getElementById('da-form-drawer').getBoundingClientRect().right-innerWidth)<1);
 const box=await drawer.boundingBox();assert.ok(box.x>0);assert.equal(box.y,0);assert.equal(box.height,1000);assert.ok(Math.abs(box.x+box.width-1440)<1);
 assert.equal(await page.locator('[name="name"]').evaluate(el=>el===document.activeElement),true);
 await page.locator('[name="name"]').fill('Unsubmitted draft');
 await page.locator('[name="objective"]').click();assert.equal(await drawer.isVisible(),true);
 // A blank area inside the panel must not count as its backdrop.
 await page.mouse.click(box.x+8,110);assert.equal(await drawer.isVisible(),true);
 // Dragging out of a field must not accidentally dismiss the drawer.
 const input=await page.locator('[name="name"]').boundingBox();
 await page.mouse.move(input.x+10,input.y+10);await page.mouse.down();await page.mouse.move(20,110);await page.mouse.up();assert.equal(await drawer.isVisible(),true);
 await page.mouse.click(20,110);await drawer.waitFor({state:'hidden'});
 assert.equal(await page.locator('body').evaluate(el=>el.classList.contains('da-drawer-open')),false);
 await page.locator('[data-da-action="create"]').click();await drawer.waitFor({state:'visible'});await page.keyboard.press('Escape');await drawer.waitFor({state:'hidden'});
 await page.locator('[data-da-action="create"]').click();await drawer.waitFor({state:'visible'});await drawer.locator('.da-drawer-close').click();await drawer.waitFor({state:'hidden'});
 assert.equal(state.requests.filter(r=>r.method==='POST').length,0);
 assert.deepEqual(state.errors,[]);
});

test('continuous assessment workspace shows graph memories and returns to the list',async t=>{
 const {page,state}=await fixture(t);
 const task=state.tasks.get('t0');task.graph={nodes:[
  {id:'goal',kind:'goal',state:'running',title:'目标',body:'fixture goal',source:{}},
  {id:'step:s1',kind:'step',step_id:'s1',state:'completed',title:'历史探索',body:'observed evidence',source:{evidence:['receipt-one']}},
  {id:'memory:pi1',kind:'memory',step_id:'s1',state:'summary',title:'Pi 摘要',body:'Persisted original summary <script>window.injected=1</script>',source:{session_entry_id:'pi-entry',provenance:'pi_compaction'}},
  {id:'digest:old',kind:'digest',state:'folded',title:'历史归档',body:'eight earlier directions',source:{members:['step:s1']}}
 ],edges:[{source:'goal',target:'digest:old',relation:'archives'},{source:'goal',target:'step:s1',relation:'plans'},{source:'step:s1',target:'memory:pi1',relation:'summarizes'}],total:3,folded:8,next_offset:3,has_more:false};
 assert.equal(await page.locator('#da-workspace').isVisible(),false);
 const list=await page.locator('.da-list-panel').boundingBox();assert.ok(list.width>1300);
 const pager=await page.locator('.da-pagination').boundingBox();assert.ok(pager.y+pager.height<=1000);
 await page.locator('[data-da-action="select"][data-id="t0"]').click();
 const inspector=page.locator('#da-workspace');await inspector.waitFor({state:'visible'});
 await page.locator('#da-graph-select option[value="memory:pi1"]').waitFor({state:'attached'});
 assert.ok(await page.locator('#da-graph-canvas canvas').count()>0);
 await page.locator('#da-graph-select').selectOption('memory:pi1');
 await page.waitForFunction(()=>document.getElementById('da-graph-node').textContent.includes('Persisted original summary'));
 await page.locator('#da-graph-node details summary').click();
 await page.evaluate(()=>DeepAssessments.refresh());
 assert.equal(await page.locator('#da-graph-node details').getAttribute('open'),'');
 assert.equal(await page.evaluate(()=>window.injected),undefined);
 await page.waitForFunction(()=>document.getElementById('da-workspace').getAnimations().every(a=>a.playState==='finished'));
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'deep-assessment-graph.png'),fullPage:true});
 await page.locator('#da-graph-select').selectOption('digest:old');await page.locator('[data-da-action="graph-expand"]').click();
 assert.ok(state.requests.some(r=>r.path.endsWith('/graph/node')));
 assert.equal(await page.locator('[data-da-action="budget"]').count(),0);
 if(!await page.locator('#da-guidance').isVisible())await page.locator('#da-guidance-panel > summary').click(); await page.locator('#da-guidance').fill('Keep this draft direction');
 await page.locator('[data-da-action="back-list"]').click();await inspector.waitFor({state:'hidden'});
 await page.evaluate(()=>DeepAssessments.refresh());assert.equal(await inspector.isVisible(),false);
 await page.locator('[data-da-action="select"][data-id="t0"]').click();await inspector.waitFor({state:'visible'});assert.equal(await page.locator('#da-guidance').inputValue(),'Keep this draft direction');
 await page.keyboard.press('Escape');assert.equal(await inspector.isVisible(),true);
 await page.locator('[data-da-action="back-list"]').click();await inspector.waitFor({state:'hidden'});
 assert.equal(state.requests.filter(r=>r.method==='POST').length,0);
 assert.deepEqual(state.errors,[]);
});


test('shared assets, intent priorities and observed facts stay linked across navigation',async t=>{
 const {page,state}=await fixture(t);
 state.sharedAssets=[{id:'shared-asset',host:'https://app.example.test',title:'<img src=x onerror="window.injected=1">'}];
 const task=state.tasks.get('t0');task.steps=[{id:'inspect',title:'Inspect fixture intent',instruction:'Read fixture',priority:1,asset_ids:['shared-asset'],source_ids:['fact:inferred'],status:'completed',evidence:['receipt-own']}];
 task.facts=[{id:'fact:observed',step_id:'inspect',confidence:'observed',summary:'Observed fixture',body:'Original <script>window.injected=1</script>',evidence:['receipt-own']},{id:'fact:inferred',step_id:'inspect',confidence:'inferred',summary:'Hypothesis',body:'Needs independent evidence',evidence:[]}];
 await page.locator('[data-da-action="select"][data-id="t0"]').click();
 await page.locator('[data-da-action="tab"][data-tab="assets"]').click();
 await page.locator('[data-da-action="asset-all"]').click();
 await page.locator('[data-da-action="asset-attach"]').click();
 await page.waitForFunction(()=>document.querySelector('[data-da-action="asset-linked"]').getAttribute('aria-pressed')==='true');
 assert.deepEqual(state.linkedAssets.get('t0'),['shared-asset']);
 assert.ok(await page.locator('#da-asset-canvas canvas').count()>0);
 await page.locator('#da-asset-select').selectOption('endpoint:shared-asset');
 assert.match(await page.locator('#da-asset-detail').innerText(),/GET \/fixture/);
 await page.reload();await page.locator('#da-asset-canvas canvas').first().waitFor();
 await page.locator('#da-asset-select').selectOption('asset:shared-asset');
 await page.locator('[data-da-action="asset-endpoints"]').click();
 await page.waitForFunction(()=>document.querySelectorAll('.da-endpoint-list p').length===50);
 await page.locator('[data-da-action="asset-endpoints-next"]').click();
 await page.waitForFunction(()=>document.querySelectorAll('.da-endpoint-list p').length===1);
 assert.match(await page.locator('.da-endpoint-list').innerText(),/endpoint\/50/);
 await page.locator('[data-da-action="tab"][data-tab="plan"]').click();
 await page.locator('.da-step summary').click();
 assert.match(await page.locator('.da-step').innerText(),/优先级: 1/);
 await page.locator('[data-da-action="intent-asset"]').click();
 await page.waitForFunction(()=>location.hash.includes('tab=assets')&&location.hash.includes('asset_id=shared-asset'));
 await page.locator('[data-da-action="asset-intents"]').click();
 await page.waitForFunction(()=>location.hash.includes('tab=plan'));
 await page.locator('#da-intent-status').waitFor();
 assert.ok(state.requests.some(r=>r.query.step_asset==='shared-asset'));

 await page.locator('#da-intent-status').selectOption('pending');
 await page.waitForFunction(()=>location.hash.includes('tab=plan'));
 assert.ok(state.requests.some(r=>r.query.step_order==='queue'&&r.query.step_status==='pending'));
 await page.locator('[data-da-action="tab"][data-tab="facts"]').click();
 await page.waitForFunction(()=>document.querySelectorAll('.da-finding').length===2);
 assert.equal(await page.locator('.da-finding').count(),2);
 await page.locator('[data-da-action="fact-filter"][data-confidence="inferred"]').click();
 await page.waitForFunction(()=>document.querySelectorAll('.da-finding').length===1);
 assert.match(await page.locator('.da-finding').innerText(),/推断待证/);
 await page.locator('[data-da-action="fact-proof"]').click();
 await page.waitForFunction(()=>location.hash.includes('tab=evidence')&&location.hash.includes('step_id=inspect'));
 assert.match(await page.locator('.da-source-banner').innerText(),/Inspect fixture intent/);
 await page.locator('[data-da-action="tab"][data-tab="assets"]').click();
 await page.setViewportSize({width:390,height:844});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 const box=await page.locator('#da-asset-canvas').boundingBox();assert.ok(box.height>=300);
 const output='/tmp/csai-intents-ui';fs.mkdirSync(output,{recursive:true});await page.screenshot({path:output+'/shared-assets-mobile.png'});
 assert.equal(await page.evaluate(()=>window.injected),undefined);
 assert.deepEqual(state.errors,[]);
 assert.equal(state.requests.filter(r=>r.method==='POST'&&!r.path.endsWith('/assets')).length,0);
});


test('assessment rows control and archive work without opening detail or touching batch tasks',async t=>{
 const {page,state}=await fixture(t);const row=page.locator('.da-list-item[data-id="t0"]');
 assert.equal(await row.locator('[data-da-action="row-start"]').isEnabled(),true);
 assert.equal(await row.locator('[data-da-action="row-pause"]').isEnabled(),false);
 await page.locator('section.content-area').evaluate(el=>el.style.maxWidth='780px');
 const area=await page.locator('section.content-area').boundingBox(),actions=await row.locator('.da-row-actions').boundingBox();assert.ok(actions.x+actions.width<=area.x+area.width+1);
 await page.locator('section.content-area').evaluate(el=>el.style.maxWidth='');
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'assessment-row-actions.png'),fullPage:true});
 await row.locator('[data-da-action="row-start"]').click();
 await page.waitForFunction(()=>document.querySelector('.da-list-item[data-id="t0"] [data-da-action="row-pause"]').disabled===false);
 assert.equal(await page.locator('#da-workspace').isVisible(),false);
 assert.equal(await row.locator('[data-da-action="row-archive"]').isEnabled(),false);
 await row.locator('[data-da-action="row-pause"]').click();
 await page.waitForFunction(()=>document.querySelector('.da-list-item[data-id="t0"] [data-da-action="row-archive"]').disabled===false);
 await row.locator('[data-da-action="row-archive"]').click();await row.waitFor({state:'detached'});
 await page.locator('#da-archive-filter').selectOption('archived');await row.waitFor();
 assert.equal(await row.locator('[data-da-action="row-start"]').isEnabled(),false);
 await row.locator('[data-da-action="details"]').click();await page.locator('#da-workspace').waitFor();
 assert.equal(await page.locator('dialog[open]').count(),0);
 await page.locator('#da-title .da-archived').waitFor();
 assert.match(await page.locator('#da-title').innerText(),/已归档/);
 await page.locator('[data-da-action="back-list"]').click();await row.waitFor();
 await row.locator('[data-da-action="row-restore"]').click();await row.waitFor({state:'detached'});
 await page.locator('#da-archive-filter').selectOption('current');await row.waitFor();
 assert.equal(state.tasks.get('t0').status,'paused');
 assert.equal(state.requests.filter(r=>r.path.endsWith('/control/start')).length,1);
 assert.ok(state.requests.every(r=>!r.path.startsWith('/api/batch-tasks')));
 assert.deepEqual(state.errors,[]);
});

test('detail is a full page and browser back restores the filtered task list',async t=>{
 const {page,state}=await fixture(t);
 await page.locator('#da-search').fill('隔离样例 t0');
 await page.waitForFunction(()=>document.querySelectorAll('.da-list-item').length===1);
 await page.locator('[data-da-action="details"]').click();await page.locator('#da-workspace').waitFor();
 const root=await page.locator('#deep-assessments-root').boundingBox(),detail=await page.locator('#da-workspace').boundingBox();
 assert.ok(Math.abs(detail.width-root.width)<2);assert.equal(await page.locator('dialog[open]').count(),0);
 assert.equal(await page.locator('#da-list-view').isVisible(),false);
 assert.equal(await page.locator('#da-guidance').isVisible(),false);
 const canvas=await page.locator('#da-graph-canvas').boundingBox();assert.ok(canvas.width>1000&&canvas.height>=400);
 await page.goBack();await page.locator('#da-list-view').waitFor();
 assert.equal(await page.locator('#da-search').inputValue(),'隔离样例 t0');
 await page.goForward();await page.locator('#da-workspace').waitFor();
 await page.reload();await page.locator('#da-workspace').waitFor();
 assert.match(page.url(),/assessment_id=t0/);assert.equal(state.requests.filter(r=>r.method==='POST').length,0);
 assert.deepEqual(state.errors,[]);
});


test('graph preserves the viewport through polling, side panel resize and tab changes',async t=>{
 const {page,state}=await fixture(t),task=state.tasks.get('t0');
 task.graph={nodes:[{id:'goal',kind:'goal',title:task.name,state:'running',body:'',source:{}},...Array.from({length:12},(_,i)=>({id:'step:s'+i,step_id:'s'+i,kind:'step',title:'持续探索方向与很长的中文节点说明，完整内容在选中后展示 '+i,state:'pending',body:'Fixture detail',source:{}}))],edges:Array.from({length:12},(_,i)=>({source:i>=3?'step:s'+(i-3):'goal',target:'step:s'+i,relation:'depends_on'})),total:12,folded:0,has_more:false};
 // A review/proof cycle must not flatten the entire graph into one column.
 task.graph.edges.push({source:'step:s6',target:'step:s3',relation:'supports'});
 await page.locator('[data-da-action="details"][data-id="t0"]').click();
 await page.locator('#da-graph-select option[value="step:s3"]').waitFor({state:'attached'});
 const before=await page.locator('#da-graph-canvas').boundingBox();
 await page.evaluate(()=>{fixtureGraph.zoom(.8);fixtureGraph.pan({x:70,y:40});});
 const positions=await page.evaluate(()=>Object.fromEntries(fixtureGraph.nodes().map(n=>[n.id(),n.position()])));
 assert.ok(new Set(Object.values(positions).map(p=>p.x)).size>2);
 await page.locator('#da-graph-select').selectOption('step:s3');
 await page.locator('#da-graph-node h4').waitFor();
 await page.waitForFunction(width=>document.getElementById('da-graph-canvas').clientWidth<width-200,before.width);
 assert.equal(await page.evaluate(()=>fixtureGraph.zoom()),.8);
 await page.locator('[data-da-action="graph-details"]').click();
 await page.waitForFunction(()=>document.getElementById('da-graph-node').getClientRects().length===0);
 const expanded=await page.locator('#da-graph-canvas').boundingBox();assert.ok(expanded.width>=before.width-2);
 task.graph.nodes.push({id:'fact:new',step_id:'s3',kind:'fact',state:'observed',title:'New evidence',body:'fixture',source:{}});task.graph.edges.push({source:'step:s3',target:'fact:new',relation:'produces'});task.graph.total++;
 await page.evaluate(()=>DeepAssessments.refresh());
 assert.equal(await page.evaluate(()=>fixtureGraph.zoom()),.8);
 assert.deepEqual(await page.evaluate(()=>Object.fromEntries(fixtureGraph.nodes().filter(n=>n.id()!=='fact:new').map(n=>[n.id(),n.position()]))),positions);
 const newPosition=await page.evaluate(()=>fixtureGraph.getElementById('fact:new').position());assert.ok(!Object.values(positions).some(p=>p.x===newPosition.x&&p.y===newPosition.y));
 await page.locator('[data-da-action="tab"][data-tab="overview"]').click();await page.locator('.da-stats').waitFor();
 await page.locator('[data-da-action="tab"][data-tab="graph"]').click();await page.locator('#da-graph-select').waitFor();
 assert.equal(await page.evaluate(()=>fixtureGraph.zoom()),.8);
 assert.equal(await page.locator('#da-graph-node').isVisible(),false);
 await page.locator('[data-da-action="graph-fit"]').click();
 assert.deepEqual(state.errors,[]);assert.equal(state.requests.filter(r=>r.method==='POST').length,0);
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'assessment-workspace-full.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'assessment-workspace-mobile.png'),fullPage:true});
});

test('long node details have an adjustable reader and keep their position through refreshes',async t=>{
 const {page,state}=await fixture(t),task=state.tasks.get('t0');
 const instruction=Array.from({length:45},(_,i)=>`${i+1}. 检查本地样例记录，保存观察结果并注明来源。这里是用于验证长文本阅读布局的内容，不会执行网络操作。`).join('\n\n');
 task.name='长文本阅读示例';task.graph={nodes:[
  {id:'goal',kind:'goal',title:'整理样例证据',state:'running',body:'整理本地样例证据并形成可核对记录。',source:{}},
  {id:'step:read',kind:'step',step_id:'read',title:'核对样例记录与证据来源',state:'running',body:'已整理记录，正在核对原始回执。\n\nInstruction: '+instruction,source:{evidence:['receipt:sample']}},
  {id:'receipt:sample',kind:'receipt',step_id:'read',title:'本地样例回执',state:'completed',body:Array.from({length:120},(_,i)=>`record ${i}: example.test /fixture — verified fixture`).join('\n'),source:{tool:'fixture'}}
 ],edges:[{source:'goal',target:'step:read',relation:'plans'},{source:'step:read',target:'receipt:sample',relation:'executes'}],total:2,folded:0,has_more:false};
 await page.locator('[data-da-action="details"][data-id="t0"]').click();
 await page.locator('#da-graph-select').selectOption('step:read');
 await page.locator('.da-node-heading h3').waitFor();
 const panel=page.locator('#da-graph-node'),content=panel.locator('.da-node-content'),bar=page.getByRole('separator');
 assert.match(await panel.locator('.da-node-heading').innerText(),/核对样例记录/);
 assert.deepEqual(await panel.locator('.da-node-section h4').allTextContents(),['结果摘要','执行说明']);
 assert.equal(await panel.locator('.da-node-prose').nth(1).textContent(),instruction);
 const bounds=await panel.boundingBox();assert.ok(bounds.width>=400,JSON.stringify(bounds));
 const geometry=await content.evaluate(el=>({height:el.clientHeight,total:el.scrollHeight,font:parseFloat(getComputedStyle(el.querySelector('.da-node-prose')).fontSize),family:getComputedStyle(el.querySelector('.da-node-prose')).fontFamily}));
 assert.ok(geometry.height>=200&&geometry.total>geometry.height,JSON.stringify(geometry));
 assert.ok(geometry.font>=14);assert.doesNotMatch(geometry.family,/monospace/i);
 await content.evaluate(el=>el.scrollTop=480);
 const scroll=await content.evaluate(el=>el.scrollTop);
 await page.evaluate(()=>DeepAssessments.refresh());
 assert.equal(await content.evaluate(el=>el.scrollTop),scroll);
 await page.evaluate(()=>fixtureGraph.zoom(.75));
 const separator=await bar.boundingBox();
 await page.mouse.move(separator.x+separator.width/2,separator.y+40);await page.mouse.down();await page.mouse.move(separator.x-100,separator.y+40);await page.mouse.up();
 assert.ok((await panel.boundingBox()).width>bounds.width+90);
 assert.equal(await page.evaluate(()=>fixtureGraph.zoom()),.75);
 const dragged=(await panel.boundingBox()).width;
 await bar.press('ArrowRight');assert.ok(Math.abs((await panel.boundingBox()).width-dragged+32)<2);
 await bar.press('Home');assert.ok(Math.abs((await panel.boundingBox()).width-bounds.width)<2);
 await content.evaluate(el=>el.scrollTop=0);
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'node-reader-split.png'),fullPage:true});
 await panel.getByRole('button',{name:'展开阅读',exact:true}).click();
 assert.equal(await page.locator('#da-graph-canvas').isVisible(),false);
 assert.ok((await panel.boundingBox()).width>1200);
 await content.evaluate(el=>el.scrollTop=300);
 await page.evaluate(()=>DeepAssessments.refresh());
 assert.equal(await page.locator('#da-graph-canvas').isVisible(),false);
 assert.equal(await content.evaluate(el=>el.scrollTop),300);
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'node-reader-expanded.png'),fullPage:true});
 await page.locator('#da-graph-select').selectOption('receipt:sample');
 await panel.locator('.da-node-code').first().waitFor();
 assert.equal(await content.evaluate(el=>el.scrollTop),0);
 assert.match(await panel.locator('.da-node-code').first().textContent(),/record 119/);
 await panel.getByRole('button',{name:'返回图',exact:true}).click();
 assert.equal(await page.locator('#da-graph-canvas').isVisible(),true);
 assert.equal(await page.evaluate(()=>fixtureGraph.zoom()),.75);
 await page.setViewportSize({width:390,height:844});
 await panel.getByRole('button',{name:'展开阅读',exact:true}).click();
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 assert.equal(await page.locator('.da-detail-scroll').evaluate(el=>el.scrollTop),0);
 assert.ok((await content.boundingBox()).height>200);
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'node-reader-mobile.png'),fullPage:true});
 await panel.getByRole('button',{name:'关闭节点详情',exact:true}).click();
 assert.equal(await panel.isVisible(),false);assert.equal(await page.locator('#da-graph-canvas').isVisible(),true);
 assert.deepEqual(state.errors,[]);assert.equal(state.requests.filter(r=>r.method==='POST').length,0);
});

test('asset search controls have room for focus and empty graphs explain the next step',async t=>{
 const {page,state}=await fixture(t);
 await page.locator('[data-da-action="details"][data-id="t0"]').click();
 await page.locator('[data-da-action="tab"][data-tab="assets"]').click();
 await page.locator('#da-asset-query').waitFor();
 assert.match(await page.locator('#da-asset-canvas').innerText(),/本次测评尚未关联资产/);
 assert.equal(await page.locator('#da-asset-select').isDisabled(),true);
 for(const width of [1440,1024,390]){
  await page.setViewportSize({width,height:width===390?844:1000});await page.locator('#da-asset-query').focus();
  const geometry=await page.evaluate(()=>{
   const rect=id=>document.querySelector(id).getBoundingClientRect();
   const search=rect('#da-asset-query'),toolbar=rect('.da-asset-toolbar'),navigation=rect('.da-graph-navigation'),scroll=rect('.da-detail-scroll');
   return {gap:navigation.top-toolbar.bottom,inset:search.left-scroll.left,right:scroll.right-search.right,height:search.height,overflow:document.documentElement.scrollWidth-innerWidth};
  });
  assert.ok(geometry.gap>=10,JSON.stringify(geometry));assert.ok(geometry.inset>=4&&geometry.right>=4,JSON.stringify(geometry));
  assert.ok(geometry.height>=32);assert.ok(geometry.overflow<=1);
  if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,`asset-search-${width}.png`),fullPage:true});
 }
 await page.setViewportSize({width:1440,height:1000});
 state.sharedAssets=[{id:'fixture-service',host:'https://app.example.test',port:443,protocol:'https',title:'样例应用',server:'Fixture server',status:'active'}];
 await page.locator('[data-da-action="asset-browse"]').click();
 await page.locator('#da-asset-select').selectOption('asset:fixture-service');
 await page.locator('.da-node-properties').waitFor();
 assert.match(await page.locator('.da-node-properties').innerText(),/地址\nhttps:\/\/app.example.test/);
 assert.match(await page.locator('.da-node-properties').innerText(),/端口\n443/);
 assert.equal(await page.locator('#da-asset-detail .da-node-source').getAttribute('open'),null);
 assert.ok((await page.locator('#da-asset-detail').boundingBox()).width>=400);
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'asset-reader.png'),fullPage:true});
 assert.deepEqual(state.errors,[]);assert.equal(state.requests.filter(r=>r.method==='POST').length,0);
});
