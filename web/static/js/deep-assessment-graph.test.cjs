const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const context={window:{}};
vm.runInNewContext(fs.readFileSync(path.join(__dirname,'deep-assessment-graph.js'),'utf8'),context);
const layout=context.window.DeepAssessmentGraph.computeLayout;

test('evidence feedback cycles retain distinct causal columns and every node',()=>{
 const nodes=[{id:'goal',kind:'goal'},{id:'step:a',kind:'step'},{id:'step:b',kind:'step'},{id:'receipt:a',kind:'receipt'},{id:'fact:a',kind:'fact'}];
 const edges=[
  {source:'goal',target:'step:a',relation:'plans'},
  {source:'step:a',target:'receipt:a',relation:'executes'},
  {source:'receipt:a',target:'fact:a',relation:'supports'},
  {source:'fact:a',target:'step:a',relation:'informs'},
  {source:'step:a',target:'step:b',relation:'depends_on'},
  {source:'step:b',target:'fact:a',relation:'reviews'},
 ];
 const original=JSON.stringify({nodes,edges}),positions=layout(nodes,edges);
 assert.equal(positions.size,nodes.length);
 assert.ok(positions.get('receipt:a').x>positions.get('step:a').x);
 assert.ok(positions.get('step:b').x>positions.get('step:a').x);
 assert.ok(new Set([...positions.values()].map(p=>p.x)).size>=4);
 assert.equal(JSON.stringify({nodes,edges}),original,'layout must not delete evidence or relations');
});

test('large sibling groups wrap into compact columns without overlapping',()=>{
 const nodes=[{id:'goal',kind:'goal'},...Array.from({length:80},(_,i)=>({id:'receipt:'+i,kind:'receipt'}))];
 const edges=nodes.slice(1).map(n=>({source:'goal',target:n.id,relation:'executes'}));
 const positions=layout(nodes,edges),points=[...positions.values()];
 assert.equal(positions.size,81);
 assert.ok(Math.max(...points.map(p=>p.y))-Math.min(...points.map(p=>p.y))<1500);
 assert.ok(new Set(points.map(p=>p.x)).size>3);
 for(let i=0;i<points.length;i++)for(let j=i+1;j<points.length;j++){
  assert.ok(Math.abs(points[i].x-points[j].x)>=250||Math.abs(points[i].y-points[j].y)>=125);
 }
 assert.deepEqual([...layout(nodes,edges)], [...positions]);
});

test('polling keeps existing positions while placing new evidence safely',()=>{
 const nodes=[{id:'goal',kind:'goal'},{id:'step:a',kind:'step'}];
 const edges=[{source:'goal',target:'step:a',relation:'plans'}];
 const existing=layout(nodes,edges);
 nodes.push({id:'fact:new',kind:'fact'});edges.push({source:'step:a',target:'fact:new',relation:'produces'});
 const next=layout(nodes,edges,existing);
 for(const [id,p] of existing)assert.deepEqual(next.get(id),p);
 assert.equal(next.size,3);
});
