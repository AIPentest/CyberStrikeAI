const fs=require('node:fs'),vm=require('node:vm'),test=require('node:test'),assert=require('node:assert/strict');
const source=fs.readFileSync('web/static/js/monitor.js','utf8');const c=vm.createContext({});vm.runInContext(source.slice(source.indexOf('function progressDoneOutcome('),source.indexOf('function finalizeOutstandingToolCallsForProgress(')),c);
test('done preserves cancelled and failed outcomes instead of showing success',()=>{
 for(const status of ['cancelled','canceled','failed','timeout','cleanup_failed','cleanup_unconfirmed']){
  const result=c.progressDoneOutcome({status});assert.notEqual(result.icon,'✅');assert.equal(result.toolStatus,status.startsWith('cancel')?'cancelled':'failed');
 }
 assert.equal(c.progressDoneOutcome({status:'completed'}).icon,'✅');assert.equal(c.progressDoneOutcome({workflowStatus:'cancelled'}).icon,'⛔');
});
test('actual done branch cancels its progress control and outstanding tools',()=>{
 const start=source.indexOf("        case 'done':"),end=source.indexOf('\n    }',start);
 for(const status of ['cancelled','failed','completed']){
  const title={};let label,toolStatus;
  const ctx=vm.createContext({event:{type:'done',data:{status}},progressId:'p',window:{},document:{querySelector:()=>title},timeline:null,progressTaskState:new Map([['p',{}]]),stopProgressElapsedClock(){},responseStreamStateByProgressId:new Map(),mainIterationStateByProgressId:new Map(),thinkingStreamStateByProgressId:new Map(),einoAgentReplyStreamStateByProgressId:new Map(),toolResultStreamStateByKey:new Map(),loadActiveTasks(){},setTimeout(){},finalizeProgressTask:(_,value)=>{label=value},finalizeOutstandingToolCallsForProgress:(_,value)=>{toolStatus=value}});
  vm.runInContext(source.slice(source.indexOf('function progressDoneOutcome('),source.indexOf('function finalizeOutstandingToolCallsForProgress(')),ctx);
  vm.runInContext('function run(){switch(event.type){'+source.slice(start,end)+'}}',ctx);ctx.run();
  assert.equal(title.textContent.startsWith('✅'),status==='completed');assert.equal(label,status==='cancelled'?'Task cancelled':status==='failed'?'Task execution failed':'Penetration test complete');assert.equal(toolStatus,status==='cancelled'?'cancelled':'failed');
 }
});
