const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync('web/static/js/settings.js','utf8');
const code=source.slice(source.indexOf('function collectHitlAuditModelEffectiveConfig()'),source.indexOf('async function testHitlAuditModelConnection()'));
function collect(overrides={},savedHeaders){
 const fields={'openai-provider':'openai','openai-api-key':'********','openai-base-url':'https://main.invalid','openai-model':'main',...overrides};
 const c={document:{getElementById:id=>({value:fields[id]||''})},selectedAIChannelId:'main',normalizeAIChannelId:x=>x,currentConfig:{ai:{channels:{main:{headers:{Authorization:'********','X-Label':'label'}}}},hitl:{audit_model:{headers:savedHeaders}}}};
 vm.createContext(c);vm.runInContext(code,c);return JSON.parse(JSON.stringify(c.collectHitlAuditModelEffectiveConfig()));
}
test('audit shares headers only when it inherits model identity',()=>{
 assert.equal(collect().headers.Authorization,'********');
 assert.equal(collect({'hitl-audit-model-name':'small'}).headers.Authorization,'********');
 for(const [id,value] of [['hitl-audit-model-provider','openai'],['hitl-audit-model-base-url','https://audit.invalid'],['hitl-audit-model-api-key','********']])assert.deepEqual(collect({[id]:value}).headers,{});
});
test('explicit audit headers belong to audit configuration',()=>{
 assert.deepEqual(collect({'hitl-audit-model-base-url':'https://audit.invalid'},{'X-Api-Key':'********'}).headers,{'X-Api-Key':'********'});
});
test('empty audit headers follow the same identity policy as omitted headers',()=>{
 assert.deepEqual(collect({},{}).headers,collect().headers);
 assert.deepEqual(collect({'hitl-audit-model-api-key':'audit-key'},{}).headers,{});
});
