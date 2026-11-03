// Browser smoke test against a fixture API; no model credentials or targets.
// CHROME_PATH=/path/to/chrome node scripts/assessment-browser-smoke.mjs
import http from 'node:http';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';

const repo = process.cwd();
const out = await fs.mkdtemp(path.join(os.tmpdir(), 'csai-assessment-browser-'));
const labels = JSON.parse(await fs.readFile('web/static/i18n/zh-CN.json', 'utf8')).assessments;
const now = () => new Date().toISOString();
let sequence = 0;
const authenticatedPaths = new Set();
let unauthorizedRequests = 0;
const fixtureTools = [{ name: 'inspect', description: 'Inspect local fixture' }, ...Array.from({ length: 104 }, (_, i) => ({ name: 'fixture_tool_' + String(i + 1).padStart(3, '0'), description: 'Local tool fixture' }))];
const fixtureSession = { token: 'assessment-fixture-session', expires_at: new Date(Date.now()+3600000).toISOString(), user: {id:'fixture-user',username:'fixture'}, roles:['admin'], scope:'all', permissions:['assessment:read','assessment:write','assessment:delete','agent:execute','hitl:write','project:read','project:write','asset:write','vulnerability:write','skills:read'] };
const task = { id: 'fixture-assessment', name: '本地测评 · 浏览器验证', goal: '核验本地隔离测试数据', scope: '仅使用浏览器测试夹具，无外部目标', material: '已提供两个测试端点', strategy: '先核验响应，再检查访问控制边界', model: 'fixture', tools: ['inspect'], approval_mode: 'manual', concurrency: 2, token_budget: 200000, tokens: 1250, reserved_tokens: 0, remaining_seconds: 7200, deadline: 0, rounds: 2, max_rounds: 12, status: 'paused', reason: 'user_paused', summary: '已记录响应证据，下一步检查访问控制。', project_id: 'fixture-project', created_at: now(), updated_at: now(), steps: [
    { id: 's1', key: 'inspect', title: '检查响应和服务信息', instruction: '读取测试夹具并记录响应。', depends_on: [], status: 'completed', result: '响应证据已记录。', checkpoint: '服务响应已保存，后续可复用。', evidence: [{ kind: 'fact', title: '已记录测试响应', detail: '本地测试数据返回固定响应。', execution_ids: ['fixture-execution-1'] }] },
    { id: 's2', key: 'access', title: '核验访问控制边界', instruction: '对照测试夹具中的两个角色。', depends_on: ['inspect'], status: 'blocked', result: '等待补充测试角色。', evidence: [{ kind: 'finding', title: '待复核访问控制线索', detail: '仅为合成数据，需在授权环境中复核。', execution_ids: ['fixture-execution-2'] }] }
] };
for (const step of task.steps) for (const evidence of step.evidence) evidence.revision="fixture-version";
const dagTask = { ...task, id: 'dag-fixture', name: '执行 DAG · 并行分支验证', status: 'running', steps: [
    { id:'d1', key:'inventory', title:'整理授权资产与入口', instruction:'汇总本地夹具的入口信息。', status:'completed', depends_on:[], result:'已整理两个测试入口。', evidence:[] },
    { id:'d2', key:'auth', title:'检查身份与访问边界', instruction:'使用两个测试角色核对权限。', status:'running', depends_on:['inventory'], checkpoint:'已记录角色对照表。', evidence:[] },
    { id:'d3', key:'input', title:'核验输入处理行为', instruction:'核对本地夹具的输入与响应。', status:'awaiting_approval', depends_on:['inventory'], evidence:[] },
    { id:'d4', key:'verify', title:'独立复核候选发现', instruction:'复查两条分支提供的证据。', status:'pending', depends_on:['auth','input'], evidence:[] },
    { id:'d5', key:'retired', title:'已关闭的探索方向', instruction:'保留历史方向。', status:'skipped', depends_on:['inventory'], retired_reason:'已有证据排除该假设。', evidence:[] },
    { id:'d6', key:'review', title:'汇总证据与验收结论', instruction:'对照验收条件整理报告。', status:'pending', depends_on:['verify'], evidence:[] }
] };
const tasks = [task, dagTask];
let holdDetail=false;const detailWaiters=[];
const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const send = data => { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(data)); };
    try {
        if (url.pathname === '/') {
            res.setHeader('Content-Type', 'text/html; charset=utf-8');
            res.end(`<!DOCTYPE html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/static/css/style.css"><link rel="stylesheet" href="/static/css/assessments.css"><style>body{display:block}#page-assessments{display:block;min-height:100vh}</style><div id="login-overlay"><form id="login-form"><input id="login-username" value="fixture"><input id="login-password" type="password"><button id="login-submit" type="submit">Login</button><p id="login-error"></p></form></div><main id="page-assessments" class="page active"><div id="assessment-workbench"></div></main><script>const labels=${JSON.stringify(labels)};window.t=k=>k.split('.').slice(1).reduce((v,p)=>v&&v[p],labels)||k;</script><script src="/static/vendor/cytoscape.min.js"></script><script src="/static/js/modal.js"></script><script src="/static/js/auth.js"></script><script src="/static/js/assessments.js"></script><script>bootstrapApp=async function(){isAppInitialized=true;initAssessmentsPage();};</script>`); return;
        }
        if (url.pathname.startsWith('/static/')) {
            const p = path.resolve(repo, 'web', '.' + url.pathname); if (!p.startsWith(path.join(repo, 'web', 'static') + path.sep)) throw Error('invalid path');
            res.setHeader('Content-Type', p.endsWith('.css') ? 'text/css' : 'application/javascript'); res.end(await fs.readFile(p)); return;
        }
        let body = ''; for await (const chunk of req) body += chunk;
        const input = body ? JSON.parse(body) : {};
        if (url.pathname === '/api/auth/login') { send(fixtureSession); return; }
        if (url.pathname.startsWith('/api/')) {
            if (req.headers.authorization !== 'Bearer ' + fixtureSession.token) { unauthorizedRequests++; res.statusCode=401; send({error:'未授权访问，请先登录'}); return; }
            authenticatedPaths.add(url.pathname);
        }
        if (url.pathname === '/api/auth/validate') {send(fixtureSession);return;}
        if (url.pathname.endsWith('/report')) {res.setHeader('Content-Type','text/markdown');res.end('# Fixture assessment report');return;}
        if (url.pathname === '/api/assessments/options') { send({ models: [{ id: 'fixture', name: 'Fixture', model: 'local-test' }], tools: fixtureTools, projects: [{id:'fixture-project',name:'本地测试项目'}] }); return; }
        if (url.pathname === '/api/assessments' && req.method === 'POST') {
            const created = { ...task, ...input, id: 'created-' + (++sequence), status: 'draft', steps: [], tokens: 0, rounds: 0, created_at: now(), updated_at: now() }; tasks.unshift(created); send({ assessment: created }); return;
        }
        if (url.pathname === '/api/assessments') { send({ assessments: tasks }); return; }
        const id = url.pathname.split('/')[3]; const current = tasks.find(t => t.id === id);
        if (!current) { res.statusCode = 404; send({ error: 'not found' }); return; }
        if (url.pathname.endsWith('/events')) { send({ events: url.searchParams.get('after') === '0' ? [{ id: 1, kind: 'fixture', message: '<img src=x onerror="window.XSS=1">', data: {}, created_at: now() }] : [] }); return; }
        if (url.pathname.includes('/control/')) {
            const action = url.pathname.split('/').at(-1);
            if (action === 'budget') { current.token_budget=input.token_budget;current.remaining_seconds=input.remaining_seconds;current.max_rounds=input.max_rounds;current.status='paused'; }
            else if (action === 'steer') current.material += '\n' + input.message;
            else if (action === 'start') { current.status = 'awaiting_approval'; current.steps = [{ ...task.steps[0], id: 'review-step', status: 'awaiting_approval', approval_id: 'fixture-request', pending: { id: 'call1', function: { name: 'inspect', arguments: { target: 'local-fixture' } } } }]; }
            else current.status = action === 'pause' ? 'paused' : 'stopped';
            current.updated_at = now(); send({ ok: true }); return;
        }
        if (url.pathname.endsWith('/decision')) { if(input.approval_id !== current.steps[0].approval_id) {res.statusCode=409;send({error:'stale approval'});return;} current.status = input.approved ? 'completed' : 'blocked'; current.steps[0].status = current.status; current.steps[0].pending = null; current.updated_at = now(); send({ ok: true }); return; }
        if (url.pathname.endsWith('/metrics')) {send({task_id:current.id,status:current.status,tokens:current.tokens});return;}
        if (url.pathname.endsWith('/publish')) { if(input.revision !== 'fixture-version'){res.statusCode=409;send({error:'stale evidence'});return;} send({ result: { id: 'published' } }); return; }
        if(holdDetail && req.method === "GET") await new Promise(resolve => detailWaiters.push(resolve));
        send({ assessment: current });
    } catch (error) { res.statusCode = 500; send({ error: error.message }); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const port = server.address().port;
const chrome = spawn(process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', ['--headless=new', '--no-proxy-server', '--remote-debugging-port=0', '--user-data-dir=' + path.join(out, 'profile'), 'about:blank'], { stdio: 'ignore' });
let socket; const pending = new Map(); let serial = 0;
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
try {
    let ws;
    for (let i = 0; i < 100; i++) { try { const active = await fs.readFile(path.join(out, 'profile/DevToolsActivePort'), 'utf8'); const devPort = active.split('\n')[0]; const targets = await (await fetch('http://127.0.0.1:' + devPort + '/json/list')).json(); ws = targets.find(target => target.type === 'page').webSocketDebuggerUrl; break; } catch { await delay(100); } }
    assert.ok(ws, 'Chrome did not start');
    socket = new WebSocket(ws); await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
    socket.onmessage = ev => { const data = JSON.parse(ev.data); if (data.method==='Runtime.exceptionThrown') console.error(JSON.stringify(data.params)); if (data.id && pending.has(data.id)) { const p = pending.get(data.id); pending.delete(data.id); data.error ? p.reject(Error(data.error.message)) : p.resolve(data.result); } };
    const cdp = (method, params = {}) => new Promise((resolve, reject) => { const id = ++serial; pending.set(id, { resolve, reject }); socket.send(JSON.stringify({ id, method, params })); });
    const evaluate = async expression => { const data = await cdp('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (data.exceptionDetails) throw Error(data.exceptionDetails.text); return data.result.value; };
    const waitFor = async expression => { for (let i = 0; i < 100; i++) { if (await evaluate(expression)) return; await delay(100); } console.error(await evaluate('JSON.stringify({url:location.href,html:document.documentElement.outerHTML.slice(0,4000)})')); throw Error('Timed out: ' + expression); };
    const click = async selector => evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
    const screenshot = async name => { const shot = await cdp('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true }); await fs.writeFile(path.join(out, name + '.png'), Buffer.from(shot.data, 'base64')); };
    await cdp('Runtime.enable');
    await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await cdp('Page.setDownloadBehavior', {behavior:'allow',downloadPath:out});
    await cdp('Page.navigate', { url: 'http://127.0.0.1:' + port });
    await waitFor("!!document.getElementById('login-form') && typeof submitLogin === 'function'");
    await evaluate("document.getElementById('login-password').value='fixture-password';document.getElementById('login-form').requestSubmit()");
    await waitFor("!!document.querySelector('[data-id=fixture-assessment]')");
    await click('[data-id=fixture-assessment]'); await waitFor("!!document.getElementById('assessment-guidance')");
    await evaluate("document.getElementById('assessment-guidance').value='preserve unsaved guidance'"); await delay(3000);
    assert.equal(await evaluate("document.getElementById('assessment-guidance').value"), 'preserve unsaved guidance', 'poll erased typed guidance');
    assert.ok(await evaluate("document.getElementById('assessment-tab').textContent.includes('先核验响应，再检查访问控制边界')"));
    await screenshot('desktop-overview');
    await click('[data-tab=review]'); await waitFor("document.getElementById('assessment-tab').textContent.includes('验收条件')");
    await click('[data-assessment-action=budget]'); await waitFor("document.getElementById('assessment-budget')?.open");
    await evaluate("document.querySelector('#assessment-budget [name=token_budget]').value='250000';document.querySelector('#assessment-budget form').requestSubmit()");await waitFor("!document.getElementById('assessment-budget')");
    await click('[data-tab=chain]'); await waitFor("!!document.querySelector('#assessment-graph canvas')"); await screenshot('desktop-chain');
    await evaluate("document.documentElement.dataset.theme='dark'"); await click('[data-tab=chain]'); await screenshot('desktop-dark-chain'); await evaluate("document.documentElement.dataset.theme='light'");
    await evaluate("window.fixtureCytoscape=window.cytoscape;window.cytoscape=(...args)=>(window.fixtureGraph=window.fixtureCytoscape(...args))");
    await click('[data-id=dag-fixture]'); await waitFor("document.querySelector('#assessment-detail h2')?.textContent==='执行 DAG · 并行分支验证'");
    await click('[data-tab=dag]'); await waitFor("!!document.querySelector('#assessment-dag-canvas canvas')");
    assert.equal(await evaluate('fixtureGraph.nodes().length'), 7);
    assert.equal(await evaluate('fixtureGraph.edges().length'), 7);
    assert.ok(await evaluate("fixtureGraph.edges().every(e=>e.source().position('x')<e.target().position('x'))"), 'DAG dependencies point backwards');
    await evaluate("document.getElementById('assessment-tab').scrollIntoView()");
    await screenshot('desktop-dag');
    await evaluate("document.documentElement.dataset.theme='dark'"); await delay(300); await screenshot('desktop-dark-dag'); await evaluate("document.documentElement.dataset.theme='light'");
    const chooseStep = async id => evaluate(`document.getElementById('assessment-dag-picker').value=${JSON.stringify(id)};document.getElementById('assessment-dag-picker').dispatchEvent(new Event('change'))`);
    await chooseStep('d4');
    assert.equal(await evaluate("document.querySelectorAll('#assessment-dag-inspector [data-dag-step]').length"),2, 'join inspector lost a prerequisite');
    await click('[data-dag-step=d2]');
    assert.equal(await evaluate("document.getElementById('assessment-dag-picker').value"), 'd2');
    await evaluate("fixtureGraph.getElementById('step:d3').emit('tap');void 0");
    assert.equal(await evaluate("document.getElementById('assessment-dag-picker').value"), 'd3', 'canvas click did not select step');
    const zoom = await evaluate('fixtureGraph.zoom()'); await click('[data-dag-action=in]');
    assert.ok(await evaluate('fixtureGraph.zoom()') > zoom, 'zoom control had no effect');
    await evaluate("fixtureGraph.zoom(.7);fixtureGraph.pan({x:51,y:83});fixtureGraph.getElementById('step:d3').position({x:560,y:150});void 0");
    dagTask.steps[2].status='completed'; dagTask.updated_at=now();
    await waitFor("document.querySelector('#assessment-dag-inspector .assessment-status')?.dataset.status==='completed'");
    assert.equal(await evaluate('fixtureGraph.zoom()'),.7,'poll reset zoom');
    assert.deepEqual(await evaluate('fixtureGraph.pan()'),{x:51,y:83},'poll reset pan');
    assert.deepEqual(await evaluate("fixtureGraph.getElementById('step:d3').position()"),{x:560,y:150},'poll reset dragged node');
    assert.equal(await evaluate("document.getElementById('assessment-dag-picker').value"),'d3','poll reset selection');
    dagTask.steps.push({id:'d7',key:'new',title:'补充复核步骤',status:'pending',depends_on:['verify'],evidence:[]});dagTask.updated_at=now();
    await waitFor("fixtureGraph.getElementById('step:d7').length===1");
    assert.equal(await evaluate("document.getElementById('assessment-dag-picker').value"),'d3','replanning reset selection');
    await click('[data-dag-action=layout]');
    assert.ok(await evaluate("fixtureGraph.edges().every(e=>e.source().position('x')<e.target().position('x'))"),'new DAG step not laid out');
    dagTask.steps[2].result='<img src=x onerror="window.XSS=1">';dagTask.updated_at=now();
    await waitFor("document.getElementById('assessment-dag-inspector').textContent.includes('<img src=x')");
    assert.equal(await evaluate('window.XSS || 0'),0,'DAG inspector rendered untrusted HTML');
    await cdp('Emulation.setDeviceMetricsOverride', {width:390,height:844,deviceScaleFactor:1,mobile:true});
    await chooseStep('d3');
    assert.ok(await evaluate("Math.abs(fixtureGraph.getElementById('step:d3').renderedPosition('x')-fixtureGraph.width()/2)<1"),'node location uses stale canvas dimensions after resize');
    await evaluate("document.querySelector('.assessment-dag-heading').scrollIntoView()"); await screenshot('mobile-dag');
    assert.ok(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1'),'mobile DAG overflows');
    await cdp('Emulation.setDeviceMetricsOverride', {width:1440,height:1000,deviceScaleFactor:1,mobile:false});
    await evaluate('window.cytoscape=null'); await click('[data-tab=dag]');
    assert.ok(await evaluate("document.querySelector('.assessment-dag-unavailable') !== null"),'missing-library fallback absent');
    await chooseStep('d4');
    assert.equal(await evaluate("document.querySelectorAll('#assessment-dag-inspector [data-dag-step]').length"),2,'fallback hides dependencies');
    await evaluate('window.cytoscape=window.fixtureCytoscape');
    await click('[data-id=fixture-assessment]'); await waitFor("document.querySelector('#assessment-detail h2')?.textContent==='本地测评 · 浏览器验证'");
    await click('[data-tab=evidence]'); assert.equal(await evaluate("document.querySelectorAll('[data-assessment-action=vulnerability]').length"), 1);
    await click('[data-assessment-action=fact]'); await waitFor("document.getElementById('assessment-message').textContent.includes('已保存')");
    await click('[data-tab=logs]'); assert.equal(await evaluate('window.XSS || 0'), 0, 'untrusted log rendered as HTML');
    await click('[data-assessment-action=new]'); await waitFor("document.getElementById('assessment-create')?.open"); assert.equal(await evaluate("document.querySelector('[name=project_id] option:last-child').textContent"), '本地测试项目');
    assert.equal(await evaluate("document.querySelector('[data-tool-selection=all]').textContent"),'全选');
    assert.equal(await evaluate("document.querySelector('[data-tool-selection=all]').disabled"),false,'more than 100 tools disabled bulk selection');
    assert.equal(await evaluate("getComputedStyle(document.querySelector('[data-tool-selection=visible]')).display"),'none','search-specific action shown without search');
    const dialogLayout = await evaluate("(()=>{const d=document.getElementById('assessment-create').getBoundingClientRect(), f=document.querySelector('#assessment-create .assessment-dialog-footer').getBoundingClientRect();return {left:d.left,right:d.right,top:d.top,bottom:d.bottom,footer:f.bottom,width:innerWidth,height:innerHeight}})()");
    assert.ok(Math.abs(dialogLayout.left - (dialogLayout.width - dialogLayout.right)) < 3, 'create dialog is not horizontally centered');
    assert.ok(dialogLayout.top >= 12 && dialogLayout.bottom <= dialogLayout.height - 12 && dialogLayout.footer <= dialogLayout.bottom, 'create dialog/footer escapes viewport');
    await screenshot('desktop-create');
    await evaluate("document.documentElement.dataset.theme='dark'"); await delay(300); await screenshot('desktop-dark-create'); await evaluate("document.documentElement.dataset.theme='light'");
    await evaluate("document.getElementById('assessment-tools-search').value='no-match-fixture';document.getElementById('assessment-tools-search').dispatchEvent(new Event('input'))");
    assert.equal(await evaluate("document.getElementById('assessment-tools-empty').hidden"), false);
    await evaluate("document.getElementById('assessment-tools-search').value='inspect';document.getElementById('assessment-tools-search').dispatchEvent(new Event('input'))");
    await click('[data-tool-selection=visible]');
    assert.ok(await evaluate("document.getElementById('assessment-tools-count').textContent.includes('1 / 100')"));
    await evaluate("document.getElementById('assessment-tools-search').value='fixture_tool_104';document.getElementById('assessment-tools-search').dispatchEvent(new Event('input'))");
    await click('[data-tool-selection=visible]');
    assert.equal(await evaluate("document.querySelectorAll('input[name=tools]:checked').length"),2,'filtered selection discarded an existing selection');
    await click('[data-tool-selection=all]');
    assert.equal(await evaluate("document.querySelectorAll('input[name=tools]:checked').length"),100,'select all must fill the limit even while filtering');
    assert.equal(await evaluate("document.querySelector('[name=tools][value=fixture_tool_104]').checked"),true,'select all discarded a prior selection');
    assert.ok(await evaluate("document.getElementById('assessment-tools-selection-note').textContent.includes('仍有部分工具未选中')"),'partial bulk selection was not disclosed');
    await evaluate("document.getElementById('assessment-tools-search').value='';document.getElementById('assessment-tools-search').dispatchEvent(new Event('input'));document.querySelector('.assessment-tool-toolbar').scrollIntoView()");
    await screenshot('desktop-tools-select-all');
    await click('[data-tool-selection=clear]');
    assert.equal(await evaluate("document.querySelectorAll('input[name=tools]:checked').length"),0,'clear did not clear hidden selections');
    assert.equal(await evaluate("document.getElementById('assessment-tools-selection-note').textContent"),'','clear left a stale limit notice');
    await evaluate("document.getElementById('assessment-tools-search').value='inspect';document.getElementById('assessment-tools-search').dispatchEvent(new Event('input'))");
    await click('[data-tool-selection=visible]');
    await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await screenshot('mobile-create');
    assert.ok(await evaluate("(()=>{const d=document.getElementById('assessment-create'),r=d.getBoundingClientRect(),f=d.querySelector('.assessment-dialog-footer').getBoundingClientRect();return r.left>=8&&r.right<=innerWidth-8&&f.bottom<=innerHeight-8&&d.scrollWidth<=d.clientWidth+1})()"), 'mobile create dialog overflows or hides footer');
    await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await evaluate(`document.querySelector('[name=name]').value='Created in browser';document.querySelector('[name=goal]').value='Inspect fixture';document.querySelector('[name=scope]').value='Local fixture';document.querySelector('[name=criteria]').value='Record a fixture response';document.querySelector('[name=tools]').checked=true;document.getElementById('assessment-create-form').requestSubmit()`);
    await waitFor("document.querySelector('#assessment-detail h2')?.textContent==='Created in browser'");
    assert.ok((await evaluate('location.hash')).startsWith('#assessments?task=created-'), 'created task deep link missing');
    await click('[data-tab=dag]'); assert.ok(await evaluate("!!document.querySelector('.assessment-dag-empty')"),'draft DAG empty state missing');
    await click('[data-assessment-action=start]'); await waitFor("document.querySelector('#assessment-detail .assessment-status')?.dataset.status==='awaiting_approval'");
    await click('[data-tab=plan]'); await waitFor("!!document.querySelector('[data-assessment-action=approve]')"); await click('[data-assessment-action=approve]'); await waitFor("document.querySelector('#assessment-detail .assessment-status')?.dataset.status==='completed'");
    holdDetail=true; await click('[data-id=fixture-assessment]'); assert.equal(await evaluate("document.querySelector('#assessment-detail [data-assessment-action=start]') === null"), true, 'stale task controls remained during switch'); holdDetail=false; detailWaiters.splice(0).forEach(resolve=>resolve()); await waitFor("document.querySelector('#assessment-detail h2')?.textContent==='本地测评 · 浏览器验证'");
    await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await click('[data-tab=plan]'); await evaluate("document.getElementById('assessment-tab').scrollIntoView()"); await screenshot('mobile-plan');
    assert.ok(await evaluate('document.documentElement.scrollWidth <= window.innerWidth + 1'), 'mobile horizontal overflow');
    await click('[data-assessment-action=report]');
    for(let i=0;i<100&&!authenticatedPaths.has('/api/assessments/fixture-assessment/report');i++) await delay(50);
    await click('[data-assessment-action=metrics]');
    for(let i=0;i<100&&!authenticatedPaths.has('/api/assessments/fixture-assessment/metrics');i++) await delay(50);
    assert.equal(unauthorizedRequests,0,'a logged-in workbench request omitted its Bearer token');
    for(const route of ['/api/assessments','/api/assessments/options','/api/assessments/fixture-assessment/report','/api/assessments/fixture-assessment/metrics']) assert.ok(authenticatedPaths.has(route),'missing authenticated request: '+route);
    await evaluate('cleanupAssessmentsPage()');
    console.log(JSON.stringify({ ok: true, screenshots: out, checks: ['real auth.js login and Bearer propagation', 'authenticated create/options/control/downloads', 'task selection', 'poll preserves draft', 'graph', 'DAG parallel branches and joins', 'DAG click/keyboard selection and zoom', 'DAG polling preserves viewport, selection and dragged nodes', 'DAG replanning, empty state and library fallback', 'desktop/dark/mobile DAG layout', 'candidate publishing controls', 'XSS escaping', 'create', 'start', 'approval', 'completion', 'mobile overflow', 'centered create modal and visible footer', 'tool search and bulk selection with 105 tools, selection preservation and 100-tool cap', 'desktop/dark/mobile create layout'] }, null, 2));
} finally {
    if (socket) socket.close(); chrome.kill(); server.close();
}
