const fs=require('node:fs'),vm=require('node:vm'),test=require('node:test'),assert=require('node:assert/strict');
const source=fs.readFileSync('web/static/js/knowledge.js','utf8');
const c=vm.createContext({});vm.runInContext(source.slice(0,source.indexOf('\n\n',source.indexOf('return \'Please check'))+2),c);
test('embedding failures give the matching configuration or service advice',()=>{
 for(const [error,expected] of [['status code: 401 Incorrect API key provided','API key'],['403 Forbidden','access rights'],['429 rate limit','request rate'],['context deadline exceeded','timed out'],['dial: no such host','DNS'],['unknown error','error above']]) assert.ok(c.knowledgeIndexErrorAdvice(error).includes(expected));
});

test('actual status renderer shows specific advice even with zero items',async()=>{
 const container={style:{},innerHTML:''};
 const ctx=vm.createContext({apiFetch:async()=>({ok:true,json:async()=>({total_items:0,last_error:'401 Unauthorized test-only failure'})}),document:{getElementById:()=>container},escapeHtml:s=>s,indexProgressInterval:null,clearInterval(){},showNotification(){},console});
 vm.runInContext(source.slice(0,source.indexOf('\n\n',source.indexOf('return \'Please check'))+2),ctx);
 vm.runInContext(source.slice(source.indexOf('async function updateIndexProgress('),source.indexOf('function stopIndexProgressPolling(')),ctx);
 await ctx.updateIndexProgress();
 assert.equal(container.style.display,'block');assert.match(container.innerHTML,/API key/);assert.match(container.innerHTML,/401 Unauthorized/);
});
