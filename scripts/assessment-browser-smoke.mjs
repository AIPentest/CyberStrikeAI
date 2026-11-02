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
const task = { id: 'fixture-assessment', name: '本地测评 · 浏览器验证', goal: '核验本地隔离测试数据', scope: '仅使用浏览器测试夹具，无外部目标', material: '已提供两个测试端点', model: 'fixture', tools: ['inspect'], approval_mode: 'manual', concurrency: 2, token_budget: 200000, tokens: 1250, reserved_tokens: 0, remaining_seconds: 7200, deadline: 0, rounds: 2, max_rounds: 12, status: 'paused', reason: 'user_paused', summary: '已记录响应证据，下一步检查访问控制。', project_id: 'fixture-project', created_at: now(), updated_at: now(), steps: [
    { id: 's1', key: 'inspect', title: '检查响应和服务信息', instruction: '读取测试夹具并记录响应。', depends_on: [], status: 'completed', result: '响应证据已记录。', evidence: [{ kind: 'fact', title: '已记录测试响应', detail: '本地测试数据返回固定响应。', execution_ids: ['fixture-execution-1'] }] },
    { id: 's2', key: 'access', title: '核验访问控制边界', instruction: '对照测试夹具中的两个角色。', depends_on: ['inspect'], status: 'blocked', result: '等待补充测试角色。', evidence: [{ kind: 'finding', title: '待复核访问控制线索', detail: '仅为合成数据，需在授权环境中复核。', execution_ids: ['fixture-execution-2'] }] }
] };
const tasks = [task];
const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const send = data => { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(data)); };
    try {
        if (url.pathname === '/') {
            res.setHeader('Content-Type', 'text/html; charset=utf-8');
            res.end(`<!DOCTYPE html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/static/css/style.css"><link rel="stylesheet" href="/static/css/assessments.css"><style>body{display:block}#page-assessments{display:block;min-height:100vh}</style><main id="page-assessments" class="page active"><div id="assessment-workbench"></div></main><script>const labels=${JSON.stringify(labels)};window.t=k=>k.split('.').slice(1).reduce((v,p)=>v&&v[p],labels)||k;window.hasPermission=()=>true;</script><script src="/static/vendor/cytoscape.min.js"></script><script src="/static/js/assessments.js"></script><script>initAssessmentsPage()</script>`); return;
        }
        if (url.pathname.startsWith('/static/')) {
            const p = path.resolve(repo, 'web', '.' + url.pathname); if (!p.startsWith(path.join(repo, 'web', 'static') + path.sep)) throw Error('invalid path');
            res.setHeader('Content-Type', p.endsWith('.css') ? 'text/css' : 'application/javascript'); res.end(await fs.readFile(p)); return;
        }
        let body = ''; for await (const chunk of req) body += chunk;
        const input = body ? JSON.parse(body) : {};
        if (url.pathname === '/api/assessments/options') { send({ models: [{ id: 'fixture', name: 'Fixture', model: 'local-test' }], tools: [{ name: 'inspect', description: 'Inspect local fixture' }], projects: [{id:'fixture-project',name:'本地测试项目'}] }); return; }
        if (url.pathname === '/api/assessments' && req.method === 'POST') {
            const created = { ...task, ...input, id: 'created-' + (++sequence), status: 'draft', steps: [], tokens: 0, rounds: 0, created_at: now(), updated_at: now() }; tasks.unshift(created); send({ assessment: created }); return;
        }
        if (url.pathname === '/api/assessments') { send({ assessments: tasks }); return; }
        const id = url.pathname.split('/')[3]; const current = tasks.find(t => t.id === id);
        if (!current) { res.statusCode = 404; send({ error: 'not found' }); return; }
        if (url.pathname.endsWith('/events')) { send({ events: url.searchParams.get('after') === '0' ? [{ id: 1, kind: 'fixture', message: '<img src=x onerror="window.XSS=1">', data: {}, created_at: now() }] : [] }); return; }
        if (url.pathname.includes('/control/')) {
            const action = url.pathname.split('/').at(-1);
            if (action === 'steer') current.material += '\n' + input.message;
            else if (action === 'start') { current.status = 'awaiting_approval'; current.steps = [{ ...task.steps[0], id: 'review-step', status: 'awaiting_approval', pending: { id: 'call1', function: { name: 'inspect', arguments: { target: 'local-fixture' } } } }]; }
            else current.status = action === 'pause' ? 'paused' : 'stopped';
            current.updated_at = now(); send({ ok: true }); return;
        }
        if (url.pathname.endsWith('/decision')) { current.status = input.approved ? 'completed' : 'blocked'; current.steps[0].status = current.status; current.steps[0].pending = null; current.updated_at = now(); send({ ok: true }); return; }
        if (url.pathname.endsWith('/publish')) { send({ result: { id: 'published' } }); return; }
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
    await cdp('Page.navigate', { url: 'http://127.0.0.1:' + port }); await waitFor("!!document.querySelector('[data-id=fixture-assessment]')");
    await click('[data-id=fixture-assessment]'); await waitFor("!!document.getElementById('assessment-guidance')");
    await evaluate("document.getElementById('assessment-guidance').value='preserve unsaved guidance'"); await delay(3000);
    assert.equal(await evaluate("document.getElementById('assessment-guidance').value"), 'preserve unsaved guidance', 'poll erased typed guidance');
    await screenshot('desktop-overview');
    await click('[data-tab=chain]'); await waitFor("!!document.querySelector('#assessment-graph canvas')"); await screenshot('desktop-chain');
    await evaluate("document.documentElement.dataset.theme='dark'"); await click('[data-tab=chain]'); await screenshot('desktop-dark-chain'); await evaluate("document.documentElement.dataset.theme='light'");
    await click('[data-tab=evidence]'); assert.equal(await evaluate("document.querySelectorAll('[data-assessment-action=vulnerability]').length"), 1);
    await click('[data-tab=logs]'); assert.equal(await evaluate('window.XSS || 0'), 0, 'untrusted log rendered as HTML');
    await click('[data-assessment-action=new]'); await waitFor("document.getElementById('assessment-create')?.open"); assert.equal(await evaluate("document.querySelector('[name=project_id] option:last-child').textContent"), '本地测试项目');
    await evaluate(`document.querySelector('[name=name]').value='Created in browser';document.querySelector('[name=goal]').value='Inspect fixture';document.querySelector('[name=scope]').value='Local fixture';document.querySelector('[name=tools]').checked=true;document.getElementById('assessment-create-form').requestSubmit()`);
    await waitFor("document.querySelector('#assessment-detail h2')?.textContent==='Created in browser'");
    assert.ok((await evaluate('location.hash')).startsWith('#assessments?task=created-'), 'created task deep link missing');
    await click('[data-assessment-action=start]'); await waitFor("document.querySelector('#assessment-detail .assessment-status')?.dataset.status==='awaiting_approval'");
    await click('[data-tab=plan]'); await waitFor("!!document.querySelector('[data-assessment-action=approve]')"); await click('[data-assessment-action=approve]'); await waitFor("document.querySelector('#assessment-detail .assessment-status')?.dataset.status==='completed'");
    await click('[data-id=fixture-assessment]'); await waitFor("document.querySelector('#assessment-detail h2')?.textContent==='本地测评 · 浏览器验证'");
    await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
    await click('[data-tab=plan]'); await evaluate("document.getElementById('assessment-tab').scrollIntoView()"); await screenshot('mobile-plan');
    assert.ok(await evaluate('document.documentElement.scrollWidth <= window.innerWidth + 1'), 'mobile horizontal overflow');
    await evaluate('cleanupAssessmentsPage()');
    console.log(JSON.stringify({ ok: true, screenshots: out, checks: ['task selection', 'poll preserves draft', 'graph', 'candidate publishing controls', 'XSS escaping', 'create', 'start', 'approval', 'completion', 'mobile overflow'] }, null, 2));
} finally {
    if (socket) socket.close(); chrome.kill(); server.close();
}
