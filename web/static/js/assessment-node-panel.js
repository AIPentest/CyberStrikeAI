/* In-page node reader shared by the asset and exploration graphs. */
(function () {
    'use strict';
    const labels = { reader: '展开阅读', back: '返回图', close: '关闭节点详情', source: '证据与来源', summary: '结果摘要', instruction: '执行说明', content: '节点内容', output: '执行回执' };
    const text = key => { const path='deepAssessments.nodeReader.'+key, value=window.t?.(path); return value && value!==path ? value : labels[key] || key; };
    const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c]);
    const rendered=new WeakMap();
    function sections(node) {
        const body=String(node.body||''), delimiter='\n\nInstruction: ', split=node.kind==='step'?body.indexOf(delimiter):-1;
        if(split>=0) return [{label:text('summary'),body:body.slice(0,split)},{label:text('instruction'),body:body.slice(split+delimiter.length)}].filter(section=>section.body.trim());
        return [{label:text(node.kind==='receipt'?'output':'content'),body,code:node.kind==='receipt'}];
    }
    function render(panel, options) {
        const signature=JSON.stringify(options);
        if(rendered.get(panel)===signature){window.DeepAssessments?.syncDetailPanels();return;}
        rendered.set(panel,signature);
        const key=options.key||options.id||'';
        const same=panel.dataset.nodeKey===key;
        const scroll=same?(panel.querySelector('.da-node-content')?.scrollTop||0):0;
        const expanded=new Set(same?[...panel.querySelectorAll('details[open]')].map(el=>el.dataset.daDetail):[]);
        const focused=panel.contains(document.activeElement)?document.activeElement:null;
        const focusAction=same?focused?.dataset.daAction:'';
        const focusData=focused?JSON.stringify({...focused.dataset}):'';
        panel.dataset.nodeKey=key;panel.classList.add('da-node-panel');
        if(!options.id) {
            panel.innerHTML='<div class="da-node-empty"><p>'+escape(options.emptyText||'')+'</p></div>';
            return;
        }
        let content=(options.sections||[]).map(section=>{
            let body='';
            if(section.properties) body='<dl class="da-node-properties">'+section.properties.filter(item=>item.value!==''&&item.value!=null).map(item=>'<div><dt>'+escape(item.label)+'</dt><dd>'+escape(item.value)+'</dd></div>').join('')+'</dl>';
            else body=section.code?'<pre class="da-node-code">'+escape(section.body)+'</pre>':'<div class="da-node-prose">'+escape(section.body)+'</div>';
            return '<section class="da-node-section"><h4>'+escape(section.label)+'</h4>'+body+'</section>';
        }).join('');
        if(options.contentHTML) content+=options.contentHTML;
        if(options.source) content+='<details class="da-node-source" data-da-detail="node-source:'+escape(options.id)+'"><summary>'+escape(text('source'))+'</summary><pre class="da-node-code">'+escape(JSON.stringify(options.source,null,2))+'</pre></details>';
        panel.innerHTML='<header class="da-node-heading"><div class="da-node-heading-row"><span class="da-node-kind">'+escape(options.kind||'')+(options.state?' · '+escape(options.state):'')+'</span><div class="da-node-reading-controls"><button type="button" class="btn-secondary" data-da-action="graph-reader" aria-expanded="false">'+escape(text('reader'))+'</button><button type="button" class="da-node-close" data-da-action="node-close" aria-label="'+escape(text('close'))+'" title="'+escape(text('close'))+'">×</button></div></div><h3>'+escape(options.title)+'</h3><code class="da-node-id">'+escape(options.id)+'</code></header><div class="da-node-content">'+content+'</div>'+(options.actionsHTML?'<footer class="da-node-actions da-controls">'+options.actionsHTML+'</footer>':'');
        panel.querySelectorAll('details[data-da-detail]').forEach(el=>el.open=expanded.has(el.dataset.daDetail));
        panel.querySelector('.da-node-content').scrollTop=scroll;
        if(focusAction) [...panel.querySelectorAll('[data-da-action]')].find(el=>el.dataset.daAction===focusAction&&JSON.stringify({...el.dataset})===focusData)?.focus({preventScroll:true});
        window.DeepAssessments?.syncDetailPanels();
    }
    window.AssessmentNodePanel={render,sections,text};
})();
