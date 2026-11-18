/* Read-only evidence navigation and explicit reviewer endpoint annotations. */
(function () {
    'use strict';
    const words = {
        path:'证据路径', history:'历次测评', endpoints:'端点证据', current:'本次测评',
        criterion:'验收项', service:'资产', endpoint:'端点', fact:'事实', step:'来源意图', receipt:'原始回执',
        has_evidence:'已有证据', observed:'已有观察证据', inferred:'推断', missing:'缺证据', needs_review:'待人工复核',
        linked:'已关联', unlinked:'关联事实', remove:'解除关联', all:'全部端点', noRows:'当前没有匹配的记录',
        backed_by:'引用原始回执', review_link:'人工验收关联', observed_on:'观察锚点', inferred_on:'推断锚点',
        endpoint_annotation:'人工端点关联', targeted_by:'关联意图', produces:'产出', contains:'包含',
        graph:'打开探索图', viewFact:'查看事实原文', fit:'适应画布', asset:'打开资产图', evidence:'查看证据路径',
        unanchored:'条直接引用的回执在该验收项下暂无可见资产归属。',
        scope:'范围或项目口径不同，保留历次记录供人工对照。',
        comparison:'差异基于两次已保存的证据记录。端点须由人工明确关联。',
        endpointHint:'端点状态来自明确关联的事实。服务级观察不会计入端点。',
        edit:'管理事实关联', endpointList:'返回端点清单', intentList:'查看全部相关意图',
        noAsset:'此视图需要资产读取权限。', previous:'上一页', next:'下一页', loading:'正在读取记录',
        newObservations:'新增观察文本', newEndpoints:'新增证据端点', unseenNow:'前次有证据、本次暂无记录的端点',
        observations:'有效观察', pending:'待复核验收项', last:'最近观察', saved:'已更新端点证据',
        selection:'选择节点查看关系', earlier:'暂无可读的较早测评记录',
        completed:'已完成', running:'处理中', failed:'失败', uncertain:'结果待核对'
    };
    const text = key => {
        const path = 'deepAssessments.provenance.' + key;
        const value = window.t?.(path);
        return value && value !== path ? value : words[key] || key;
    };
    const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'})[c]);
    const attr = (name,value) => `data-${name}="${esc(value)}"`;
    const button = (action,title,data={}) => `<button type="button" class="btn-secondary" data-da-action="${action}" ${Object.entries(data).map(([key,value])=>attr(key,value)).join(' ')}>${esc(title)}</button>`;
    const date = value => {
        const timestamp = value ? new Date(value) : null;
        return timestamp && !Number.isNaN(timestamp.valueOf()) ? timestamp.toLocaleString() : value || '—';
    };
    const status = value => `<span class="da-acceptance-status da-acceptance-tone-${esc(value==='observed'?'has_evidence':value)}">${esc(text(value))}</span>`;
    function route(task, tab, query={}) {
        return '#deep-assessments?' + new URLSearchParams({assessment_id:task, tab, ...query});
    }
    function link(task, tab, query, title) {
        return `<a class="btn-secondary" href="${esc(route(task,tab,query))}">${esc(title)}</a>`;
    }
    function pageControls(current, hasMore) {
        return `<div class="da-controls">${current?button('provenance-page',text('previous'),{direction:-1}):''}${hasMore?button('provenance-page',text('next'),{direction:1}):''}</div>`;
    }
    function assetBar(asset, active='') {
        if (!asset) return '';
        const views=[['intent-asset','asset'],['provenance-path','path'],['provenance-endpoints','endpoints'],['provenance-history','history']];
        return `<div class="da-controls">${views.filter(([,key])=>key!==active).map(([action,key])=>button(action,text(key),{asset})).join('')}</div>`;
    }

    function renderPath(body, data, context, task, onSelect) {
        const remembered = context.node || data.root;
        const visible = data.nodes.some(n=>n.id===remembered) ? remembered : data.root;
        if (!body.querySelector('#da-provenance-canvas')) {
            body.innerHTML = `<div class="da-provenance-heading" id="da-provenance-header"></div>
                <div class="da-graph-workspace da-provenance-workspace">
                    <div id="da-provenance-canvas" role="img" aria-label="${esc(text('path'))}"></div>
                    ${window.DeepAssessments.separator()}
                    <section id="da-provenance-node" aria-live="polite"></section>
                </div><div id="da-provenance-pages"></div>`;
        }
        const header = body.querySelector('#da-provenance-header');
        header.innerHTML = `<h3>${esc(data.title)}</h3>
            ${assetBar(context.asset,context.endpoint?'':'path')}
            ${data.unattributed_receipts?`<p class="da-muted">${data.unattributed_receipts} ${esc(text('unanchored'))}</p>`:''}
            <div class="da-controls">${button('graph-fit',text('fit'))}
            ${data.related_intent_count?button('asset-intents',text('intentList')+' · '+data.related_intent_count,{asset:context.asset}):''}
            ${button('graph-details','')}<select class="form-control" id="da-provenance-select" aria-label="${esc(text('selection'))}">${data.nodes.map(n=>`<option value="${esc(n.id)}">${esc(text(n.kind)+' · '+n.title)}</option>`).join('')}</select></div>`;
        const select = body.querySelector('#da-provenance-select');
        const panel = body.querySelector('#da-provenance-node');
        function show(id, initiated) {
            const n=data.nodes.find(item=>item.id===id);
            if(!n)return;
            select.value=id;
            if(initiated){onSelect(id);window.DeepAssessments.showNodeDetails();}
            window.DeepAssessmentGraph?.select(id);
            const source=n.source||{};
            const actions = {
                service:()=>source.asset_id===context.asset?'':assetBar(source.asset_id),
                endpoint:()=>button('provenance-endpoints',text('edit'),{asset:source.asset_id,endpoint:source.endpoint_id}),
                criterion:()=>link(task,'acceptance',{criterion:source.criterion},text('criterion')+' '+source.criterion),
                fact:()=>button('graph-open',text('viewFact'),{node:n.id,kind:'fact'}),
                receipt:()=>link(task,'evidence',{receipt_id:source.receipt_id},text('receipt')),
                step:()=>button('show-step',text('step'),{step:n.step_id})
            };
            const relations=data.edges.filter(e=>e.source===id||e.target===id).map(e=>{
                const neighbor=data.nodes.find(item=>item.id===(e.source===id?e.target:e.source));
                return neighbor?button('provenance-select',text(e.relation)+' · '+neighbor.title,{node:neighbor.id}):'';
            }).join('');
            const criteria=id===data.root&&n.kind==='service'?data.nodes.filter(node=>node.kind==='criterion')
                .map(node=>button('provenance-select',text('criterion')+' · '+node.title,{node:node.id})).join(''):'';
            window.AssessmentNodePanel?.render(panel,{id:n.id,key:task+'/evidence-path/'+n.id,title:n.title,kind:text(n.kind),state:text(n.state||''),
                sections:n.body?[{label:text(n.kind),body:n.body}]:[],
                contentHTML:relations||criteria?'<div class="da-controls da-provenance-neighbors">'+criteria+relations+'</div>':'',
                actionsHTML:actions[n.kind]?.()||''});
        }
        select.onchange=()=>show(select.value,true);
        body.querySelector('#da-provenance-pages').innerHTML=pageControls(context.offset,data.has_more);
        const canvas=body.querySelector('#da-provenance-canvas');
        const key=[task,'evidence-path',context.asset,context.endpoint,context.criterion,context.fact,context.offset].join('/');
        window.DeepAssessmentGraph?.render(canvas,data,key,id=>show(id,true),text,text);
        show(visible,false);
        panel.onclick=event=>{
            const target=event.target.closest('[data-da-action="provenance-select"]');
            if(target){event.stopPropagation();show(target.dataset.node,true);}
        };
    }

    function renderEndpoints(body, data, context, task, writable) {
        let html=`<div class="da-provenance-heading"><h3>${esc(data.title)} · ${esc(text('endpoints'))}</h3>${assetBar(context.asset,'endpoints')}<p class="da-muted">${esc(text('endpointHint'))}</p></div>`;
        if (context.endpoint) {
            const e=data.endpoint;
            html+=`<div class="da-controls">${button('provenance-endpoints',text('endpointList'),{asset:context.asset})}${button('provenance-path',text('evidence'),{asset:context.asset,endpoint:e.id})}</div>
                <h4>${esc(e.method+' '+e.path)}</h4><div class="da-provenance-records">`;
            html+=(data.items||[]).map(item=>{
                const f=item.fact;
                const toggle=writable?button('provenance-toggle',text(item.linked?'remove':'unlinked'),{fact:f.id,linked:String(!item.linked)}):'';
                return `<article class="da-provenance-card"><strong>${esc(f.title)}</strong><p class="da-muted">${esc(text(f.state)+(item.linked?' · '+text('linked'):''))}</p><div class="da-controls">${button('provenance-path',text('evidence'),{fact:f.id})}${toggle}</div></article>`;
            }).join('');
        } else {
            html+=`<div class="da-controls da-provenance-filters" role="group" aria-label="${esc(text('endpoints'))}">
                ${['','observed','needs_review','missing'].map(key=>{
                    const count=key?data.counts[key]:Object.values(data.counts).reduce((a,b)=>a+b,0);
                    return `<button type="button" class="btn-secondary" data-da-action="provenance-filter" data-status="${key}" aria-pressed="${String(context.status===key)}">${esc(text(key||'all')+' · '+count)}</button>`;
                }).join('')}</div><div class="da-provenance-records">`;
            html+=data.endpoints.map(e=>`<article class="da-provenance-card"><div class="da-provenance-card-title"><strong>${esc(e.method+' '+e.path)}</strong>${status(e.status)}</div>
                ${e.title?`<p class="da-muted">${esc(e.title)}</p>`:''}
                <div class="da-controls">${button('provenance-path',text('evidence'),{asset:context.asset,endpoint:e.id})}
                ${writable?button('provenance-endpoints',text('edit'),{asset:context.asset,endpoint:e.id}):''}</div></article>`).join('');
        }
        const empty=context.endpoint?!(data.items||[]).length:!data.endpoints.length;
        if(empty)html+=`<p class="da-muted">${esc(text('noRows'))}</p>`;
        body.innerHTML=html+'</div>'+pageControls(context.offset,data.has_more);
    }

    function renderHistory(body, data, context) {
        const record = (r, current) => {
            const facts=(r.recent||[]).map(f=>link(r.task_id,'evidence-path',{fact_id:f.id},f.title)).join('');
            const criteria=(r.pending_criteria||[]).map(number=>link(r.task_id,'acceptance',{criterion:number},text('criterion')+' '+number)).join('');
            const diff=current?'':r.comparable?`<p>${esc(text('newObservations'))} ${r.new_observations} · ${esc(text('newEndpoints'))} ${r.new_endpoints} · ${esc(text('unseenNow'))} ${r.unseen_now}</p>`:`<p class="da-muted">${esc(text('scope'))}</p>`;
            return `<article class="da-provenance-card"><div class="da-provenance-card-title"><h4>${esc(current?text('current')+' · '+r.name:r.name)}</h4><time class="da-muted">${esc(date(r.created_at))}</time></div>
                <p>${esc(text('observations'))} ${r.observed_facts} · ${esc(text('endpoints'))} ${r.observed_endpoints} · ${esc(text('pending'))} ${r.pending_criteria.length}</p>
                <p class="da-muted">${esc(text('last')+' '+date(r.last_observation))}</p>${diff}
                <div class="da-controls">${link(r.task_id,'evidence-path',{asset_id:data.asset_id},text('path'))}${facts}${criteria}</div></article>`;
        };
        body.innerHTML=`<div class="da-provenance-heading"><h3>${esc(data.title)} · ${esc(text('history'))}</h3>${assetBar(context.asset,'history')}<p class="da-muted">${esc(text('comparison'))}</p></div>
            <div class="da-provenance-records">${record(data.current,true)}${data.prior.map(r=>record(r,false)).join('')||`<p class="da-muted">${esc(text('earlier'))}</p>`}</div>${pageControls(context.offset,data.has_more)}`;
    }

    function render(body, tab, data, context, task, writable, onSelect) {
        if (!data) {
            body.textContent=text('loading');
            return;
        }
        if(tab==='evidence-path')renderPath(body,data,context,task,onSelect);
        else if(tab==='endpoint-review')renderEndpoints(body,data,context,task,writable);
        else renderHistory(body,data,context);
    }
    function load(task, tab, context, json) {
        const paths={'evidence-path':'evidence-path','asset-history':'evidence-history','endpoint-review':'endpoint-evidence'};
        const params=new URLSearchParams({offset:context.offset||0});
        if(context.asset)params.set('asset_id',context.asset);
        if(context.endpoint)params.set('endpoint_id',context.endpoint);
        if(context.fact)params.set('fact_id',context.fact);
        if(context.criterion)params.set('criterion',context.criterion);
        if(context.status)params.set('status',context.status);
        return json('/'+encodeURIComponent(task)+'/'+paths[tab]+'?'+params);
    }
    window.AssessmentProvenance={text,render,load};
})();
