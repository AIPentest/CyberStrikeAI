const fs=require('node:fs'),vm=require('node:vm'),test=require('node:test'),assert=require('node:assert/strict');
const source=fs.readFileSync('web/static/js/knowledge.js','utf8');
const c=vm.createContext({window:undefined});vm.runInContext(source.slice(0,source.indexOf('function getKnowledgeNotEnabledHTML')),c);
test('embedding failures give the matching configuration or service advice',()=>{
 for(const [error,expected] of [['status code: 401 Incorrect API key provided','API 密钥'],['403 Forbidden','访问权限'],['429 rate limit','请求频率'],['context deadline exceeded','超时'],['dial: no such host','DNS'],['unknown error','上方错误']]) assert.ok(c.knowledgeIndexErrorAdvice(error).includes(expected));
});

test('actual status renderer shows specific advice even with zero items',async()=>{
 const container={style:{},innerHTML:''};
 const ctx=vm.createContext({apiFetch:async()=>({ok:true,json:async()=>({total_items:0,last_error:'401 Unauthorized test-only failure'})}),document:{getElementById:()=>container},escapeHtml:s=>s,indexProgressInterval:null,clearInterval(){},showNotification(){},console});
 vm.runInContext(source.slice(0,source.indexOf('function getKnowledgeNotEnabledHTML')),ctx);
 vm.runInContext(source.slice(source.indexOf('async function updateIndexProgress('),source.indexOf('function stopIndexProgressPolling(')),ctx);
 await ctx.updateIndexProgress();
 assert.equal(container.style.display,'block');assert.match(container.innerHTML,/API 密钥/);assert.match(container.innerHTML,/401 Unauthorized/);
});
