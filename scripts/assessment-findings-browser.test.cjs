const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const http=require('node:http');
const {chromium}=require('playwright');
const root=path.join(__dirname,'..');
const read=file=>fs.readFileSync(path.join(root,file),'utf8');
const template=read('web/templates/index.html');
function div(id){
 const start=template.indexOf(`<div id="${id}"`);assert.ok(start>=0,id);
 const tags=/<\/?div\b[^>]*>/g;tags.lastIndex=start;let depth=0,m;
 while((m=tags.exec(template))){depth+=m[0].startsWith('</')?-1:1;if(!depth)return template.slice(start,tags.lastIndex);}
 throw new Error('unclosed '+id);
}
async function fixture(t){
 const now=new Date().toISOString();
 const sourceStep={id:'early-step',title:'Origin step outside current page',status:'completed',instruction:'Read local fixture',summary:'source observation',evidence:['receipt-1'],depends_on:[],attempts:1,conversation_id:'source-conversation'};
 const task={id:'same-id',name:'域名深度测评',status:'completed',revision:1,objective:'Inspect fixture',scope:'local',criteria:'real evidence',steps:[],metrics:{},rounds:3,tokens_used:100,updated_at:now};
 const finding={id:'finding-1',title:'权限校验问题',description:'Fixture finding',severity:'high',status:'open',type:'authorization',target:'https://example.test/account',conversation_id:'source-conversation',project_id:'',assessment_id:'same-id',assessment_name:task.name,assessment_step_id:sourceStep.id,assessment_step_title:sourceStep.title,assessment_can_open:true,reproduction_steps:'1. Read the supplied fixture\n2. Verify the recorded result',evidence:'original-proof <img src=x onerror="window.injected=1">',impact:'fixture impact',recommendation:'fixture fix',created_at:now,updated_at:now,targets:[{host:'example.test',raw:'https://example.test/account',domain:'example.test'}]};
 const batch={id:'batch-finding',title:'批量任务原有漏洞',severity:'medium',status:'open',description:'batch fixture',target:'https://batch.example.test',task_id:'same-id',task_queue_id:'batch-queue',task_queue_title:'原批量队列',created_at:now,updated_at:now};
 const state={task,sourceStep,finding,batch,requests:[],errors:[],canOpen:true};
 const locale=JSON.stringify(JSON.parse(read('web/static/i18n/zh-CN.json'))).replaceAll('<','\\u003c');
 const scripts=['router','auth','modal','vulnerability','assessment-finding-links','vulnerability-workspace','assessment-exploration-views','deep-assessment-graph','deep-assessments'].map(x=>`<script src="/static/js/${x}.js"></script>`).join('');
 const styles=['style','mobile','vulnerability-workspace','vulnerability-list','deep-assessments'].map(x=>`<link rel="stylesheet" href="/static/css/${x}.css">`).join('');
 const html=`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">${styles}</head><body><div class="container"><main class="main-layout"><section class="content-area">${div('page-deep-assessments')}${div('page-vulnerabilities')}<div class="page" id="page-chat"></div></section></main></div>${div('vulnerability-inspector')}<script>window.t=(key,opts={})=>{const v=key.split('.').reduce((o,k)=>o&&o[k],${locale});return String(v||key).replace(/{{(\\w+)}}/g,(_,k)=>String(opts[k]??''))};window.loadConversation=async id=>{window.visitedConversation=id};window.initializeChatUI=()=>{};window.refreshAppData=async()=>{};</script><script src="/static/vendor/cytoscape.min.js"></script>${scripts}<script>document.removeEventListener('DOMContentLoaded',initializeApp);saveAuth('fixture-token',new Date(Date.now()+3600000),{user:{id:'fixture',username:'fixture'},permissions:['assessments:read','assessments:write','vulnerability:read','vulnerability:write','chat:read','chat:write','project:read'],scope:'own'});</script></body></html>`;
 const server=http.createServer(async(req,res)=>{
  const url=new URL(req.url,'http://fixture');
  const send=(code,data)=>{res.statusCode=code;res.setHeader('Content-Type','application/json; charset=utf-8');res.end(JSON.stringify(data));};
  try{
   if(url.pathname.startsWith('/static/')){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');return res.end(read('web'+url.pathname));}
   if(url.pathname==='/'){res.setHeader('Content-Type','text/html; charset=utf-8');return res.end(html);}
   if(url.pathname==='/favicon.ico'){res.statusCode=204;return res.end();}
   state.requests.push({path:url.pathname,query:Object.fromEntries(url.searchParams),method:req.method});
   if(req.headers.authorization!=='Bearer fixture-token')return send(401,{error:'authentication missing'});
   if(url.pathname==='/api/projects')return send(200,{projects:[]});
   if(url.pathname==='/api/deep-assessments')return send(200,{tasks:[task],has_more:false});
   if(url.pathname==='/api/deep-assessments/same-id')return send(200,{task,step_total:101,step_next_offset:50,step_has_more:true});
   if(url.pathname==='/api/deep-assessments/same-id/graph')return send(200,{nodes:[{id:'goal',kind:'goal',state:'completed',title:task.name,body:'',source:{}}],edges:[],total:0,folded:0,has_more:false});
   if(url.pathname==='/api/deep-assessments/same-id/steps/review-step')return send(200,{step:{...sourceStep,id:'review-step',title:'Independent review step'}});
   if(url.pathname==='/api/deep-assessments/same-id/receipts'&&url.searchParams.get('step_id')==='review-step')return send(200,{receipts:[{id:'review-receipt',execution_id:'review-execution',step_id:'review-step',tool:'fixture_recheck',arguments:'{}',result:'independent-review-evidence',status:'completed'}],next_offset:1,has_more:false});
   if(url.pathname==='/api/deep-assessments/same-id/steps/early-step')return send(200,{step:sourceStep});
   if(url.pathname==='/api/deep-assessments/same-id/vulnerabilities')return send(200,{vulnerabilities:[finding],total:1,next_offset:1,has_more:false});
   if(url.pathname==='/api/deep-assessments/same-id/vulnerabilities/finding-1')return send(200,{vulnerability:finding,step:sourceStep});
   if(url.pathname==='/api/deep-assessments/same-id/receipts')return send(200,{receipts:[{id:'receipt-1',execution_id:'execution-1',step_id:'early-step',tool:'fixture_read',arguments:'{}',result:'original-execution-evidence',status:'completed'}],next_offset:1,has_more:false});
   if(url.pathname==='/api/deep-assessments/same-id/events')return send(200,{events:[]});
   if(url.pathname==='/api/vulnerabilities/filter-options')return send(200,{vulnerability_ids:['finding-1','batch-finding'],task_ids:['same-id'],queue_ids:['batch-queue'],assessment_ids:['same-id'],conversation_ids:[],conversation_tags:[],task_tags:[]});
   let items=[finding,batch];if(url.searchParams.get('assessment_id'))items=items.filter(v=>v.assessment_id===url.searchParams.get('assessment_id'));if(url.searchParams.get('id'))items=items.filter(v=>v.id===url.searchParams.get('id'));
   if(url.pathname==='/api/vulnerabilities/stats')return send(200,{total:items.length,by_severity:{high:items.filter(v=>v.severity==='high').length,medium:items.filter(v=>v.severity==='medium').length},by_status:{open:items.length}});
   if(url.pathname==='/api/vulnerabilities')return send(200,{vulnerabilities:items,total:items.length,page:1,page_size:20,total_pages:1});
   if(url.pathname==='/api/vulnerabilities/finding-1/context')return send(200,{vulnerability:{...finding,assessment_can_open:state.canOpen},history:[],members:[],assets:[]});
   if(url.pathname==='/api/vulnerabilities/finding-1')return send(200,{...finding,assessment_can_open:state.canOpen});
   return send(404,{error:'unhandled fixture route '+url.pathname});
  }catch(error){send(500,{error:error.message});}
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const executablePath=process.env.CHROME_PATH||(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':undefined);
 const browser=await chromium.launch(executablePath?{executablePath}:{});
 t.after(async()=>{await browser.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));});
 const page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(6000);page.on('pageerror',e=>state.errors.push(e.message));page.on('dialog',d=>d.accept());
 await page.goto(`http://127.0.0.1:${server.address().port}/#deep-assessments`);await page.locator('[data-da-action="select"]').waitFor();
 return {page,state};
}

test('assessment findings round-trip through vulnerability management to exact evidence',async t=>{
 const {page,state}=await fixture(t);
 await page.locator('[data-da-action="select"]').click();await page.locator('[data-da-action="tab"][data-tab="findings"]').click();
 await page.locator('.da-finding').waitFor();assert.match(await page.locator('.da-finding').innerText(),/权限校验问题/);
 await page.locator('.da-finding [data-da-action="open-finding"]').click();
 await page.locator('#vulnerability-inspector a[data-assessment-jump]').first().waitFor();
 assert.match(await page.locator('.vulnerability-assessment-source').innerText(),/域名深度测评/);
 await page.locator('#vulnerability-inspector a[data-assessment-jump]').first().click();
 await page.locator('#da-inspector .da-step[data-step="early-step"][open]').waitFor();
 assert.match(page.url(),/assessment_id=same-id/);assert.match(page.url(),/step_id=early-step/);
 assert.match(await page.locator('.da-source-banner').innerText(),/权限校验问题/);
 await page.reload();await page.locator('#da-inspector .da-step[data-step="early-step"][open]').waitFor();
 await page.locator('.da-source-banner [data-da-action="open-finding"]').click();
 await page.locator('#vulnerability-inspector a[data-assessment-jump]').last().waitFor();await page.locator('#vulnerability-inspector a[data-assessment-jump]').last().click();
 await page.waitForFunction(()=>document.querySelector('.da-source-banner')?.textContent.includes('original-proof'));
 assert.match(page.url(),/tab=evidence/);assert.equal(await page.locator('.da-source-banner img').count(),0);assert.equal(await page.evaluate(()=>window.injected),undefined);
 assert.ok(state.requests.some(r=>r.path.endsWith('/receipts')&&r.query.step_id==='early-step'));
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 await page.waitForFunction(()=>document.getElementById('da-inspector').getAnimations().every(a=>a.playState==='finished'));
 assert.equal(await page.locator('#vulnerability-inspector').isVisible(),false);assert.equal(await page.locator('dialog[open]').count(),1);
 const box=await page.locator('#da-inspector').boundingBox();assert.ok(Math.abs(box.x)<1&&Math.abs(box.y)<1&&Math.abs(box.width-390)<1);
 assert.ok(state.requests.every(r=>r.method==='GET'));
 assert.deepEqual(state.errors,[]);
 const dir=process.env.CSAI_SCREENSHOT_DIR;if(dir){fs.mkdirSync(dir,{recursive:true});await page.screenshot({path:path.join(dir,'assessment-finding-evidence.png'),fullPage:true});}
});

test('task grouping separates assessment IDs from batch IDs and disabled source links',async t=>{
 const {page,state}=await fixture(t);
 await page.evaluate(()=>{location.hash='vulnerabilities?view=task';});
 await page.locator('#vulnerabilities-list [data-host-key="assessment:same-id"]').waitFor();
 assert.match(await page.locator('[data-host-key="assessment:same-id"]').innerText(),/域名深度测评/);
 assert.match(await page.locator('[data-host-key="queue:batch-queue"]').innerText(),/原批量队列/);
 await page.locator('[data-host-key="assessment:same-id"]').click();await page.waitForFunction(()=>document.querySelectorAll('#vulnerability-host-table [data-vuln-id]').length>0);
 assert.equal(await page.locator('#vulnerability-host-table #vulnerability-card-batch-finding').count(),0);
 state.canOpen=false;
 await page.locator('#vulnerability-host-table #vulnerability-card-finding-1 .vulnerability-record-main').click();
 await page.locator('.vulnerability-assessment-source').waitFor();assert.equal(await page.locator('.vulnerability-assessment-source a').count(),0);
 assert.deepEqual(state.errors,[]);
});


test('independent review links remain usable after assessment polling refreshes',async t=>{
 const {page,state}=await fixture(t);
 state.finding.assessment_reviews=[{id:'review-1',step_id:'review-step',verdict:'confirmed',reason:'Independent fixture review',evidence:['review-receipt']}];
 await page.locator('[data-da-action="select"]').click();
 await page.locator('[data-da-action="tab"][data-tab="findings"]').click();
 await page.locator('.da-finding [data-da-action="open-finding"]').click();
 await page.getByRole('link',{name:'查看步骤与复现证据',exact:true}).click();
 const review=page.locator('#da-inspector .da-review-records details');
 await review.locator('summary').click();
 await page.evaluate(()=>window.DeepAssessments.refresh());
 await page.evaluate(()=>window.DeepAssessments.refresh());
 assert.equal(await review.getAttribute('open'),'');
 await review.getByRole('link',{name:'查看复核意图与回执',exact:true}).click();
 await page.locator('#da-inspector .da-step[data-step="review-receipt"]').waitFor();
 assert.match(page.url(),/step_id=review-step/);
 assert.match(await page.locator('.da-source-banner').innerText(),/Independent review step/);
 await page.locator('#da-inspector .da-step[data-step="review-receipt"] summary').click();
 assert.match(await page.locator('#da-body').innerText(),/independent-review-evidence/);
 assert.doesNotMatch(await page.locator('#da-body').innerText(),/original-execution-evidence/);
 assert.ok(state.requests.every(r=>r.method==='GET'));
 assert.deepEqual(state.errors,[]);
});


test('authenticated refresh restores the requested assessment and write controls',async t=>{
 const {page,state}=await fixture(t);
 await page.evaluate(async()=>{
  document.querySelector('[data-da-action="create"]').remove();
  history.replaceState(null,'','#deep-assessments?assessment_id=same-id&step_id=review-step&tab=evidence');
  window.loadConversations=async()=>{};
  window.loadActiveTasks=async()=>{};
  await refreshAppData();
 });
 await page.locator('#da-inspector .da-step[data-step="review-receipt"]').waitFor();
 assert.match(await page.locator('.da-source-banner').innerText(),/Independent review step/);
 assert.equal(await page.locator('[data-da-action="create"]').count(),1);
 assert.ok(state.requests.every(r=>r.method==='GET'));
 assert.deepEqual(state.errors,[]);
});
