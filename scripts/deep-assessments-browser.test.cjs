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
 const scripts = ['router', 'auth', 'assessment-exploration-views','deep-assessment-graph', 'deep-assessments'].map(x => `<script src="/static/js/${x}.js"></script>`).join('');
 const html = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">${['style', 'mobile', 'deep-assessments'].map(x=>`<link rel="stylesheet" href="/static/css/${x}.css">`).join('')}</head><body><div class="container"><main class="main-layout"><section class="content-area">${region}<div class="page" id="page-chat"></div><div class="page" id="page-tasks"></div></section></main></div><script>window.t=k=>k.split('.').reduce((o,p)=>o&&o[p],${locale})||k;window.loadConversation=async id=>{window.visitedConversation=id};window.initializeChatUI=()=>{};window.refreshAppData=async()=>{};</script><script src="/static/vendor/cytoscape.min.js"></script>${scripts}<script>document.removeEventListener('DOMContentLoaded',initializeApp);saveAuth('fixture-token',new Date(Date.now()+3600000),{user:{id:'fixture',username:'fixture'},permissions:['assessments:read','assessments:write','assessments:delete','agent:execute','chat:read','chat:write','tasks:read','asset:read'],scope:'own'});</script></body></html>`;
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
   const filtered=[...state.tasks.values()].filter(t=>(!url.searchParams.get('q')||t.name.includes(url.searchParams.get('q')))&&(!url.searchParams.get('status')||t.status===url.searchParams.get('status')));
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
   const action=match[2].split('/')[1];if(action==='start')task.status='running';if(action==='pause')task.status='paused';if(action==='guidance')task.summary=body.message;if(action==='budget')Object.assign(task,body);task.revision++;
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
 await page.locator('#da-guidance').fill('Keep this unsent text');await page.evaluate(()=>DeepAssessments.refresh());assert.equal(await page.locator('#da-guidance').inputValue(),'Keep this unsent text');
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
 await page.locator('#da-guidance').fill('Unsent direction');await page.evaluate(()=>DeepAssessments.refresh());assert.equal(await page.locator('.da-step[open]').count(),1);
 const screenshotRoot=process.env.CSAI_SCREENSHOT_DIR;
 if(screenshotRoot){fs.mkdirSync(screenshotRoot,{recursive:true});await page.screenshot({path:path.join(screenshotRoot,'deep-assessments-desktop.png'),fullPage:true});}
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.setAttribute('data-theme','dark'));
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 await page.locator('[data-da-action="close-detail"]').click();
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

test('continuous assessment graph shows durable memories and keeps drawer controls',async t=>{
 const {page,state}=await fixture(t);
 const task=state.tasks.get('t0');task.graph={nodes:[
  {id:'goal',kind:'goal',state:'running',title:'目标',body:'fixture goal',source:{}},
  {id:'step:s1',kind:'step',step_id:'s1',state:'completed',title:'历史探索',body:'observed evidence',source:{evidence:['receipt-one']}},
  {id:'memory:pi1',kind:'memory',step_id:'s1',state:'summary',title:'Pi 摘要',body:'Persisted original summary <script>window.injected=1</script>',source:{session_entry_id:'pi-entry',provenance:'pi_compaction'}},
  {id:'digest:old',kind:'digest',state:'folded',title:'历史归档',body:'eight earlier directions',source:{members:['step:s1']}}
 ],edges:[{source:'goal',target:'digest:old',relation:'archives'},{source:'goal',target:'step:s1',relation:'plans'},{source:'step:s1',target:'memory:pi1',relation:'summarizes'}],total:3,folded:8,next_offset:3,has_more:false};
 assert.equal(await page.locator('#da-inspector').isVisible(),false);
 const list=await page.locator('.da-list-panel').boundingBox();assert.ok(list.width>1300);
 const pager=await page.locator('.da-pagination').boundingBox();assert.ok(pager.y+pager.height<=1000);
 await page.locator('[data-da-action="select"][data-id="t0"]').click();
 const inspector=page.locator('#da-inspector');await inspector.waitFor({state:'visible'});
 await page.locator('#da-graph-select option[value="memory:pi1"]').waitFor({state:'attached'});
 assert.ok(await page.locator('#da-graph-canvas canvas').count()>0);
 await page.locator('#da-graph-select').selectOption('memory:pi1');
 await page.waitForFunction(()=>document.getElementById('da-graph-node').textContent.includes('Persisted original summary'));
 await page.locator('#da-graph-node details summary').click();
 await page.evaluate(()=>DeepAssessments.refresh());
 assert.equal(await page.locator('#da-graph-node details').getAttribute('open'),'');
 assert.equal(await page.evaluate(()=>window.injected),undefined);
 await page.waitForFunction(()=>document.getElementById('da-inspector').getAnimations().every(a=>a.playState==='finished'));
 if(process.env.CSAI_SCREENSHOT_DIR)await page.screenshot({path:path.join(process.env.CSAI_SCREENSHOT_DIR,'deep-assessment-graph.png'),fullPage:true});
 await page.locator('#da-graph-select').selectOption('digest:old');await page.locator('[data-da-action="graph-expand"]').click();
 assert.ok(state.requests.some(r=>r.path.endsWith('/graph/node')));
 assert.equal(await page.locator('[data-da-action="budget"]').count(),0);
 await page.locator('#da-guidance').fill('Keep this draft direction');
 await page.mouse.click(20,160);await inspector.waitFor({state:'hidden'});
 await page.evaluate(()=>DeepAssessments.refresh());assert.equal(await inspector.isVisible(),false);
 await page.locator('[data-da-action="select"][data-id="t0"]').click();await inspector.waitFor({state:'visible'});assert.equal(await page.locator('#da-guidance').inputValue(),'Keep this draft direction');
 await page.keyboard.press('Escape');await inspector.waitFor({state:'hidden'});
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
