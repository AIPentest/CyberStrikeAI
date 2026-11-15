/* Shared assets and immutable facts; navigation never starts an execution. */
(function () {
 'use strict';
 let endpointAsset='',endpointOffset=0,focusAsset='';
 let task='', mode='linked', query='', offset=0, factOffset=0, factConfidence='', selected='';
 const labels={coverage:'已有观察证据（不表示测完）',assets:'资产图',facts:'事实',linked:'本次测评资产',all:'共享资产库',search:'搜索资产',attach:'关联到本次测评',empty:'暂无记录',previous:'上一页',next:'下一页',observed:'直接观察',inferred:'推断待证',allFacts:'全部事实',proof:'查看来源意图与回执',fit:'适应画布',hint:'共享资产保留统一身份；关联到测评后，可作为探索意图的目标。IP 关系仅表示资产记录中的关联，不等同于已验证 DNS。',endpoints:'选择服务节点，可分页查看全部端点。',endpointList:'查看全部端点',assetIntents:'查看目标意图',source:'来源意图',evidence:'原始回执',select:'选择节点查看资产详情',domain:'域名',subdomain:'子域',ip:'IP',service:'服务',application:'应用',endpoint:'端点'};
 const text=k=>{const path='deepAssessments.exploration.'+k,v=window.t?.(path);return v&&v!==path?v:labels[k]||k;};
 const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c]);
 const button=(action,label,extra='')=>'<button type="button" class="btn-secondary" data-da-action="'+action+'" '+extra+'>'+esc(label)+'</button>';
 const canAssets=()=>typeof window.hasPermission!=='function'||window.hasPermission('asset:read');
 function reset(id){if(task===id)return;task=id;focusAsset='';endpointAsset='';endpointOffset=0;mode='linked';query='';offset=0;factOffset=0;factConfidence='';selected='';}
 async function load(id,tab,json){
  reset(id);
  if(tab==='assets'){
   const graph=await json('/'+encodeURIComponent(id)+'/assets?'+new URLSearchParams({scope:mode==='all'?'all':'linked',q:query,offset,asset_id_graph:focusAsset}));
   if(endpointAsset)graph.endpoint_page=await json('/'+encodeURIComponent(id)+'/assets?'+new URLSearchParams({asset_id:endpointAsset,offset:endpointOffset}));
   return graph;
  }
  if(tab==='facts')return json('/'+encodeURIComponent(id)+'/facts?'+new URLSearchParams({confidence:factConfidence,offset:factOffset}));
 }
 function render(body,tab,data,canWrite){
  if(tab==='assets'){
   const graph=data||{nodes:[],edges:[],assets:[],total:0};
   if(!body.querySelector('#da-asset-canvas')){
    window.DeepAssessmentGraph?.destroy();
    body.innerHTML='<div class="da-controls">'+button('asset-linked',text('linked'))+button('asset-all',text('all'))+button('graph-fit',text('fit'))+button('graph-details','', 'aria-expanded="false"')+'</div><form id="da-asset-search" class="da-controls"><input id="da-asset-query" class="form-control" aria-label="'+esc(text('search'))+'" placeholder="'+esc(text('search'))+'"><button class="btn-secondary" type="submit">'+esc(text('search'))+'</button></form><p class="da-muted">'+esc(text('hint'))+'</p><select id="da-asset-select" class="form-control" aria-label="'+esc(text('select'))+'"></select><div class="da-graph-workspace"><div id="da-asset-canvas" role="img" aria-label="'+esc(text('assets'))+'"></div><section id="da-asset-detail"></section></div><div id="da-asset-rows"></div><div id="da-asset-pages" class="da-controls"></div>';
    body.querySelector('#da-asset-query').value=query;
   }
   if(document.activeElement!==body.querySelector('#da-asset-query'))body.querySelector('#da-asset-query').value=query;
   body.querySelectorAll('[data-da-action="asset-linked"],[data-da-action="asset-all"]').forEach(el=>el.setAttribute('aria-pressed',String(el.dataset.daAction==='asset-'+mode)));
   const pick=(id,userInitiated=false)=>{
    selected=id;const n=graph.nodes.find(n=>n.id===id),el=document.getElementById('da-asset-detail');if(!el)return;
    if(n&&userInitiated)window.DeepAssessments?.showNodeDetails();
    el.innerHTML=n?'<small>'+esc(text(n.kind))+'</small><h4>'+esc(n.title)+'</h4><pre>'+esc(n.body||'')+'</pre><code>'+esc(n.id)+'</code>':'<p>'+esc(text('select'))+'</p>';
    if(n?.kind==='service' && n.asset_ids?.length===1){
     el.innerHTML+=button('asset-intents',text('assetIntents'),'data-asset="'+esc(n.asset_ids[0])+'"');
     el.innerHTML+=button('asset-endpoints',text('endpointList'),'data-asset="'+esc(n.asset_ids[0])+'"');
     if(endpointAsset===n.asset_ids[0] && graph.endpoint_page){
      const ep=graph.endpoint_page;
      el.innerHTML+='<div class="da-endpoint-list">'+ep.endpoints.map(e=>'<p><strong>'+esc(e.method)+' '+esc(e.path)+'</strong><br>'+esc(e.title||'')+'</p>').join('')+'</div><div class="da-controls">'+(endpointOffset?button('asset-endpoints-previous',text('previous')):'')+(ep.has_more?button('asset-endpoints-next',text('next')):'')+'</div>';
     }
    }
    const select=document.getElementById('da-asset-select');if(select)select.value=id;
    window.DeepAssessmentGraph?.select(id);
   };
   const select=body.querySelector('#da-asset-select');select.innerHTML='<option value="">'+esc(text('select'))+'</option>'+graph.nodes.map(n=>'<option value="'+esc(n.id)+'">'+esc(text(n.kind)+' · '+n.title)+'</option>').join('');
   select.onchange=()=>pick(select.value,true);
   window.DeepAssessmentGraph?.render(body.querySelector('#da-asset-canvas'),graph,task+'/assets/'+mode+'/'+offset+'/'+focusAsset,id=>pick(id,true),text);
   pick(selected);
   body.querySelector('#da-asset-rows').innerHTML='<p class="da-muted">'+esc(text('endpoints'))+'</p>'+graph.assets.map(a=>'<article class="da-finding"><strong>'+esc(a.host||a.domain||a.ip)+'</strong><p>'+esc(a.title||a.business_system||'')+'</p><code>'+esc(a.id)+'</code>'+(canWrite&&mode==='all'?button('asset-attach',text('attach'),'data-asset="'+esc(a.id)+'"'):'')+'</article>').join('');
   body.querySelector('#da-asset-pages').innerHTML=(graph.coverage?'<span>'+esc(text('coverage'))+' '+graph.coverage.with_observed_evidence+'/'+graph.coverage.linked+'</span>':'')+'<span>'+esc(text(mode==='all'?'all':'linked'))+' · '+graph.total+'</span>'+(offset?button('asset-previous',text('previous')):'')+(graph.has_more?button('asset-next',text('next')):'');
  }else if(tab==='facts'){
   body.innerHTML='<div class="da-controls">'+['','observed','inferred'].map(k=>button('fact-filter',text(k||'allFacts'),'data-confidence="'+k+'" aria-pressed="'+String(k===factConfidence)+'"')).join('')+'</div>'+((data?.facts||[]).map(f=>'<article class="da-finding"><span class="da-badge da-'+esc(f.confidence)+'">'+esc(text(f.confidence))+'</span><h4>'+esc(f.summary)+'</h4><pre>'+esc(f.body)+'</pre><p>'+esc(text('source'))+'：'+esc(f.step_id)+'</p><p>'+esc(text('evidence'))+'：'+esc((f.evidence||[]).join(', '))+'</p>'+button('fact-proof',text('proof'),'data-step="'+esc(f.step_id)+'"')+'</article>').join('')||'<p>'+esc(text('empty'))+'</p>')+'<div class="da-controls">'+(factOffset?button('fact-previous',text('previous')):'')+(data?.has_more?button('fact-next',text('next')):'')+'</div>';
  }
 }
 async function action(name,data,json){
  if(name==='asset-endpoints'){endpointAsset=data.asset;endpointOffset=0;return true;}
  if(name==='asset-endpoints-next'||name==='asset-endpoints-previous'){endpointOffset=Math.max(0,endpointOffset+(name==='asset-endpoints-next'?50:-50));return true;}
  if(name==='asset-linked'||name==='asset-all'){mode=name==='asset-all'?'all':'linked';focusAsset='';offset=0;selected='';return true;}
  if(name==='asset-search'){query=document.getElementById('da-asset-query')?.value||'';focusAsset='';offset=0;selected='';return true;}
  if(name==='asset-next'||name==='asset-previous'){offset=Math.max(0,offset+(name==='asset-next'?20:-20));selected='';return true;}
  if(name==='asset-attach'){await json('/'+encodeURIComponent(task)+'/assets',{method:'POST',body:JSON.stringify({asset_ids:[data.asset]})});mode='linked';query='';offset=0;selected='';return true;}
  if(name==='fact-filter'){factConfidence=data.confidence||'';factOffset=0;return true;}
  if(name==='fact-next'||name==='fact-previous'){factOffset=Math.max(0,factOffset+(name==='fact-next'?20:-20));return true;}
  return false;
 }
 function focusAssetNode(id){focusAsset=id;query='';offset=0;mode='linked';selected='asset:'+id;endpointAsset='';}
 window.AssessmentExplorationViews={focusAsset:focusAssetNode,load,render,action,reset,canAssets};
})();
