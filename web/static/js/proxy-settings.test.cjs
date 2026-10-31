const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
function fixture(saved = {}) {
 const elements = {};
 for (const key of ['mode','proxy_url','proxy_pool','proxy_source','strategy','max_attempts','cooldown','min_switch_interval']) elements[`proxy-setting-${key}`] = {value:'',checkValidity:()=>true};
 elements['proxy-settings-save'] = {disabled:false};
 const calls = [], alerts = [], panels = ['fixed','pool'].map(mode=>({dataset:{proxyMode:mode},hidden:true}));
 const ctx = {window:{t:x=>x}, alert:x=>alerts.push(x),document:{getElementById:id=>elements[id],querySelectorAll:()=>panels},apiFetch:async(url,options)=>{calls.push({url,options});return {ok:true,json:async()=>({proxy:saved})};}};
 vm.createContext(ctx);vm.runInContext(fs.readFileSync(__dirname+'/proxy-settings.js','utf8'),ctx);
 return {ctx,elements,calls,alerts,panels};
}
test('initial defaults and mode panels are usable', async()=>{
 const f=fixture({mode:'',max_attempts:0});await f.ctx.loadProxySettings();
 assert.equal(f.elements['proxy-setting-max_attempts'].value,3);
 assert.equal(f.elements['proxy-setting-min_switch_interval'].value,60);
 f.elements['proxy-setting-mode'].value='fixed';f.ctx.syncProxySettingsMode();assert.equal(f.panels[0].hidden,false);assert.equal(f.panels[1].hidden,true);
});
test('masked credentials survive save without restarting other services',async()=>{
 const f=fixture({mode:'fixed',proxy_url:'********',max_attempts:3,cooldown:60,min_switch_interval:60});await f.ctx.loadProxySettings();await f.ctx.saveProxySettings();
 const write=f.calls.find(x=>x.options?.method==='PUT');assert.equal(JSON.parse(write.options.body).proxy.proxy_url,'********');
 assert.equal(f.calls.some(x=>x.url==='/api/config/apply'),false);
 assert.equal(f.elements['proxy-settings-save'].disabled,false);
});
