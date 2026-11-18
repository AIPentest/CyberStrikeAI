/* Shared assets and immutable facts; navigation never starts an execution. */
(function () {
 'use strict';
 let endpointAsset='',endpointOffset=0,focusAsset='';
 let task='', mode='linked', query='', offset=0, factOffset=0, factConfidence='', selected='';
 let observation='';
 const labels={browse:'查看共享资产库',help:'资产图说明',records:'资产记录',emptyLinked:'本次测评尚未关联资产',emptyShared:'当前范围内没有匹配的资产',emptyHint:'可查看共享资产库；测评发现并登记资产后，也会显示在这里。',clearSearch:'清除搜索',assetInfo:'资产信息',host:'地址',port:'端口',protocol:'协议',title:'名称',server:'服务指纹',system:'业务系统',status:'状态',sourceName:'来源',lastSeen:'最近发现',method:'方法',path:'路径',active:'活跃',inactive:'停用',coverage:'已有观察证据（不表示测完）',assets:'资产图',facts:'事实',linked:'本次测评资产',all:'共享资产库',search:'搜索资产',attach:'关联到本次测评',empty:'暂无记录',previous:'上一页',next:'下一页',observed:'直接观察',inferred:'推断待证',allFacts:'全部事实',proof:'查看来源意图与回执',fit:'适应画布',hint:'共享资产保留统一身份；关联到测评后，可作为探索意图的目标。IP 关系仅表示资产记录中的关联，不等同于已验证 DNS。',endpoints:'选择服务节点，可分页查看全部端点。',endpointList:'查看全部端点',assetIntents:'查看目标意图',source:'来源意图',evidence:'原始回执',select:'选择节点查看资产详情',domain:'域名',subdomain:'子域',ip:'IP',service:'服务',application:'应用',endpoint:'端点',evidencePath:'查看证据路径',history:'历次测评',endpointEvidence:'端点证据'};
 Object.assign(labels,{coverageFilter:'按观察证据筛选',coverageAll:'全部关联资产',coverageObserved:'已有观察证据',coverageMissing:'暂无观察证据',coverageEmpty:'没有符合当前观察证据筛选的资产',coverageHint:'只统计当前账号可读的关联资产。覆盖情况按已有观察事实计算，验收需核对具体证据。',assetFacts:'查看此资产的事实',assetFactFilter:'当前仅查看此资产的事实'});
 const text=k=>{const path='deepAssessments.exploration.'+k,v=window.t?.(path);return v&&v!==path?v:labels[k]||k;};
 const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c]);
 const button=(action,label,extra='')=>'<button type="button" class="btn-secondary" data-da-action="'+action+'" '+extra+'>'+esc(label)+'</button>';
 const canAssets=()=>typeof window.hasPermission!=='function'||window.hasPermission('asset:read');
 function reset(id){if(task===id)return;task=id;focusAsset='';endpointAsset='';endpointOffset=0;mode='linked';query='';offset=0;factOffset=0;factConfidence='';selected='';observation='';}
 function setObservation(value){
  observation=['observed','missing'].includes(value)?value:'';
  if(observation)mode='linked';
  focusAsset='';endpointAsset='';selected='';offset=0;
 }
 function restoreObservation(value){
  mode='linked';query='';
  setObservation(value);
 }
 async function load(id,tab,json,asset=''){
  reset(id);
  if(tab==='assets'){
   const graph=await json('/'+encodeURIComponent(id)+'/assets?'+new URLSearchParams({scope:mode==='all'?'all':'linked',q:query,offset,asset_id_graph:focusAsset,observation}));
   if(endpointAsset&&graph.assets.some(a=>a.id===endpointAsset))graph.endpoint_page=await json('/'+encodeURIComponent(id)+'/assets?'+new URLSearchParams({asset_id:endpointAsset,offset:endpointOffset}));
   return graph;
  }
  if(tab==='facts'){
   const data=await json('/'+encodeURIComponent(id)+'/facts?'+new URLSearchParams({confidence:factConfidence,offset:factOffset,asset_id:asset}));
   return {...data,asset_id:asset};
  }
 }
 function render(body,tab,data,canWrite){
  if(tab==='assets'){
   const graph=data||{nodes:[],edges:[],assets:[],total:0};
   if(!body.querySelector('#da-asset-canvas')){
    window.DeepAssessmentGraph?.destroy();
    body.innerHTML='<div class="da-asset-toolbar"><div class="da-asset-scopes" role="group" aria-label="'+esc(text('assets'))+'">'+button('asset-linked',text('linked'))+button('asset-all',text('all'))+'</div><form id="da-asset-search" class="da-asset-search" role="search"><input id="da-asset-query" type="search" class="form-control" aria-label="'+esc(text('search'))+'" placeholder="'+esc(text('search'))+'"><button class="btn-secondary" type="submit">'+esc(text('search'))+'</button></form><div class="da-controls">'+button('graph-fit',text('fit'))+button('graph-details','', 'aria-expanded="false"')+'</div></div><div class="da-graph-navigation"><select id="da-asset-select" class="form-control" aria-label="'+esc(text('select'))+'"></select><span id="da-asset-meta" class="da-muted"></span><details class="da-view-help" data-da-detail="asset-help"><summary>'+esc(text('help'))+'</summary><p class="da-muted">'+esc(text('hint'))+'</p></details></div><div class="da-graph-workspace"><div id="da-asset-canvas" role="img" aria-label="'+esc(text('assets'))+'"></div>'+window.DeepAssessments.separator()+'<section id="da-asset-detail" aria-live="polite"></section></div><details class="da-asset-records"><summary id="da-asset-records-title"></summary><div id="da-asset-rows"></div></details><div id="da-asset-pages" class="da-controls"></div>';
    body.querySelector('#da-asset-query').value=query;
    const controls=document.createElement('div');
    controls.className='da-controls da-coverage-controls';
    controls.id='da-asset-coverage';
    controls.setAttribute('role','group');
    controls.setAttribute('aria-label',text('coverageFilter'));
    body.querySelector('.da-asset-toolbar').after(controls);
   }
   if(document.activeElement!==body.querySelector('#da-asset-query'))body.querySelector('#da-asset-query').value=query;
   body.querySelectorAll('[data-da-action="asset-linked"],[data-da-action="asset-all"]').forEach(el=>el.setAttribute('aria-pressed',String(el.dataset.daAction==='asset-'+mode)));
   const coverage=body.querySelector('#da-asset-coverage');
   coverage.hidden=mode!=='linked'||!graph.coverage;
   if(!coverage.hidden){
    const {linked,with_observed_evidence:observed}=graph.coverage;
    const filters=[['','coverageAll',linked],['observed','coverageObserved',observed],['missing','coverageMissing',linked-observed]];
    if(!coverage.firstChild){
     coverage.insertAdjacentHTML('beforeend',filters.map(([status,label])=>button('asset-observation',text(label),'data-observation="'+status+'"')).join(''));
     coverage.insertAdjacentHTML('beforeend','<p class="da-muted da-coverage-hint">'+esc(text('coverageHint'))+'</p>');
    }
    coverage.querySelectorAll('button').forEach((el,index)=>{
     const [status,label,count]=filters[index];
     el.textContent=text(label)+' · '+count;
     el.setAttribute('aria-pressed',String(observation===status));
    });
   }
   const pick=(id,userInitiated=false)=>{
    selected=id;const n=graph.nodes.find(n=>n.id===id),panel=body.querySelector('#da-asset-detail');
    if(n&&userInitiated)window.DeepAssessments?.showNodeDetails();
    let record=null;try{record=JSON.parse(n?.body||'null');}catch{}
    const properties=record?['host','domain','ip','port','protocol','title','server','business_system','status','source','last_seen_at','method','path'].filter(k=>record[k]!==''&&record[k]!=null&&record[k]!==0).map(k=>({label:text(({business_system:'system',source:'sourceName',last_seen_at:'lastSeen',domain:'domain',ip:'ip'})[k]||k),value:k==='status'?text(record[k]):record[k]})):[];
    let actions='',content='';const ep=n?.kind==='service'&&endpointAsset===n.asset_ids?.[0]?graph.endpoint_page:null;
    if(n?.kind==='service' && n.asset_ids?.length===1){
     actions+=button('asset-intents',text('assetIntents'),'data-asset="'+esc(n.asset_ids[0])+'"')+button('asset-endpoints',text('endpointList'),'data-asset="'+esc(n.asset_ids[0])+'"');
     if(mode==='linked'){
      const asset='data-asset="'+esc(n.asset_ids[0])+'"';
      actions+=button('asset-facts',text('assetFacts'),asset)+button('provenance-path',text('evidencePath'),asset)+button('provenance-history',text('history'),asset)+button('provenance-endpoints',text('endpointEvidence'),asset);
     }
     if(canWrite&&mode==='all')actions+=button('asset-attach',text('attach'),'data-asset="'+esc(n.asset_ids[0])+'"');
     if(ep)content='<section class="da-node-section"><h4>'+esc(text('endpointList'))+'</h4><div class="da-endpoint-list">'+ep.endpoints.map(e=>'<p><strong>'+esc(e.method)+' '+esc(e.path)+'</strong><br>'+esc(e.title||'')+'</p>').join('')+'</div><div class="da-controls">'+(endpointOffset?button('asset-endpoints-previous',text('previous')):'')+(ep.has_more?button('asset-endpoints-next',text('next')):'')+'</div></section>';
    }
    if(n?.kind==='endpoint'&&mode==='linked'&&n.asset_ids?.length===1){
     const attrs='data-asset="'+esc(n.asset_ids[0])+'" data-endpoint="'+esc(n.id)+'"';
     actions+=button('provenance-path',text('evidencePath'),attrs)+button('provenance-endpoints',text('endpointEvidence'),attrs);
    }
    if(window.AssessmentNodePanel)window.AssessmentNodePanel.render(panel,n?{id:n.id,key:task+'/'+n.id+(ep?'/endpoints/'+endpointOffset:''),title:n.title,kind:text(n.kind),sections:properties.length?[{label:text('assetInfo'),properties}]:n.body?[{label:text('assetInfo'),body:n.body}]:[],source:record||{asset_ids:n.asset_ids},contentHTML:content,actionsHTML:actions}:{emptyText:text('select')});
    else panel.textContent=n?n.title+'\n'+(n.body||''):text('select');
    const select=body.querySelector('#da-asset-select');select.value=id;
    window.DeepAssessmentGraph?.select(id);
   };
   const select=body.querySelector('#da-asset-select');select.innerHTML='<option value="">'+esc(text('select'))+'</option>'+graph.nodes.map(n=>'<option value="'+esc(n.id)+'">'+esc(text(n.kind)+' · '+n.title)+'</option>').join('');select.disabled=!graph.nodes.length;
   select.onchange=()=>pick(select.value,true);
   const canvas=body.querySelector('#da-asset-canvas');canvas.classList.toggle('da-empty-canvas',!graph.nodes.length);canvas.closest('.da-graph-workspace').classList.toggle('da-graph-empty',!graph.nodes.length);
   if(graph.nodes.length){canvas.setAttribute('role','img');window.DeepAssessmentGraph?.render(canvas,graph,task+'/assets/'+mode+'/'+observation+'/'+offset+'/'+focusAsset+'/'+query,id=>pick(id,true),text);}
   else {window.DeepAssessmentGraph?.destroy();window.DeepAssessments?.hideNodeDetails();canvas.setAttribute('role','region');canvas.innerHTML='<div class="da-empty-state"><h4>'+esc(text(observation?'coverageEmpty':mode==='linked'&&!query?'emptyLinked':'emptyShared'))+'</h4>'+(observation?'':'<p>'+esc(text('emptyHint'))+'</p>')+(query?button('asset-clear',text('clearSearch')):observation?button('asset-observation',text('coverageAll'),'data-observation=""'):mode==='linked'?button('asset-browse',text('browse')):'')+'</div>';}
   if(selected&&!graph.nodes.some(n=>n.id===selected)){selected='';window.DeepAssessments?.hideNodeDetails();}
   pick(selected);
   const records=body.querySelector('.da-asset-records');if(records.dataset.scope!==mode){records.open=mode==='all';records.dataset.scope=mode;}
   records.hidden=!graph.assets.length;body.querySelector('#da-asset-records-title').textContent=text('records')+' · '+graph.assets.length;
   body.querySelector('#da-asset-rows').innerHTML='<p class="da-muted">'+esc(text('endpoints'))+'</p>'+graph.assets.map(a=>'<article class="da-finding"><strong>'+esc(a.host||a.domain||a.ip)+'</strong><p>'+esc(a.title||a.business_system||'')+'</p><code>'+esc(a.id)+'</code>'+(canWrite&&mode==='all'?button('asset-attach',text('attach'),'data-asset="'+esc(a.id)+'"'):'')+'</article>').join('');
   body.querySelector('#da-asset-meta').textContent=text(mode==='all'?'all':'linked')+' · '+graph.total;
   body.querySelector('#da-asset-pages').innerHTML=(offset?button('asset-previous',text('previous')):'')+(graph.has_more?button('asset-next',text('next')):'');
  }else if(tab==='facts'){
   const assetFilter=data?.asset_id?'<div class="da-controls"><span>'+esc(text('assetFactFilter'))+'</span>'+button('intent-asset',text('assetInfo'),'data-asset="'+esc(data.asset_id)+'"')+button('clear-fact-asset',text('allFacts'))+'</div>':'';
   const cards=(data?.facts||[]).map(f=>{
    const receipts=(f.evidence||[]).map(id=>button('graph-open',id,'data-node="'+esc('receipt:'+id)+'" data-kind="receipt"')).join('');
    return '<article class="da-finding"><span class="da-badge da-'+esc(f.confidence)+'">'+esc(text(f.confidence))+'</span><h4>'+esc(f.summary)+'</h4><pre>'+esc(f.body)+'</pre><p>'+esc(text('source'))+'：'+esc(f.step_id)+'</p>'+(receipts?'<div class="da-controls"><span>'+esc(text('evidence'))+'</span>'+receipts+'</div>':'')+button('fact-proof',text('proof'),'data-step="'+esc(f.step_id)+'"')+button('provenance-path',text('evidencePath'),'data-fact="'+esc(f.id)+'"')+'</article>';
   }).join('');
   body.innerHTML=assetFilter+'<div class="da-controls">'+['','observed','inferred'].map(k=>button('fact-filter',text(k||'allFacts'),'data-confidence="'+k+'" aria-pressed="'+String(k===factConfidence)+'"')).join('')+'</div>'+(cards||'<p>'+esc(text('empty'))+'</p>')+'<div class="da-controls">'+(factOffset?button('fact-previous',text('previous')):'')+(data?.has_more?button('fact-next',text('next')):'')+'</div>';
  }
 }
 async function action(name,data,json){
  if(name==='asset-endpoints'){endpointAsset=data.asset;endpointOffset=0;return true;}
  if(name==='asset-endpoints-next'||name==='asset-endpoints-previous'){endpointOffset=Math.max(0,endpointOffset+(name==='asset-endpoints-next'?50:-50));return true;}
  if(name==='asset-observation'){setObservation(data.observation);return true;}
  if(name==='asset-linked'||name==='asset-all'||name==='asset-browse'){mode=name==='asset-linked'?'linked':'all';setObservation('');return true;}
  if(name==='asset-clear'){query='';focusAsset='';offset=0;selected='';return true;}
  if(name==='asset-search'){query=(document.getElementById('da-asset-query')?.value||'').trim();focusAsset='';offset=0;selected='';return true;}
  if(name==='asset-next'||name==='asset-previous'){offset=Math.max(0,offset+(name==='asset-next'?20:-20));selected='';return true;}
  if(name==='asset-attach'){await json('/'+encodeURIComponent(task)+'/assets',{method:'POST',body:JSON.stringify({asset_ids:[data.asset]})});focusAssetNode(data.asset);return true;}
  if(name==='fact-filter'){factConfidence=data.confidence||'';factOffset=0;return true;}
  if(name==='fact-next'||name==='fact-previous'){factOffset=Math.max(0,factOffset+(name==='fact-next'?20:-20));return true;}
  return false;
 }
 function focusAssetNode(id){restoreObservation('');focusAsset=id;selected='asset:'+id;}
 function routeState(){
  return {asset_node:selected,asset_scope:mode==='all'?'all':'',asset_q:query,asset_focus:focusAsset,asset_offset:offset,asset_endpoint: endpointAsset,asset_endpoint_offset:endpointOffset};
 }
 function restoreRouteState(params){
  mode=params.get('asset_scope')==='all'?'all':'linked';
  observation=mode==='linked'&&['observed','missing'].includes(params.get('observation'))?params.get('observation'):'';
  query=params.get('asset_q')||'';focusAsset=params.get('asset_focus')||'';offset=Math.max(0,Number(params.get('asset_offset'))||0);
  selected=params.get('asset_node')||'';endpointAsset=params.get('asset_endpoint')||'';endpointOffset=Math.max(0,Number(params.get('asset_endpoint_offset'))||0);
 }
 window.AssessmentExplorationViews={routeState,restoreRouteState,resetFacts(){factOffset=0;factConfidence='';},focusAsset:focusAssetNode,restoreObservation,observation:()=>observation,load,render,action,reset,canAssets};
})();
