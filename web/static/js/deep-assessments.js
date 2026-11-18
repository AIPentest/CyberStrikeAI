/* Independent assessment workspace. Batch task routes and controllers are untouched. */
(function () {
    'use strict';
    const S = { renderTab:'',renderStatus:'',renderWasBusy:false,renderSignature:'', guidanceDelivered:false,runtime:null,graphView:'summary',relations:[],linksOffset:0,linksMore:false,stepDrafts:Object.create(null), readerExpanded: false, noticeTimer: null, detailVisible: false, detailsOpen: false, listTasks: [], rowBusy: new Set(), extra: null, intentAsset: '', intentStatus: '', intentCounts: {}, routeKey: '', focusStepId: '', focusedStep: null, originFindingId: '', originFinding: null, findings: [], findingOffset: 0, findingMore: false, findingTotal: 0, id: '', task: null, tab: 'graph', graph: null, graphScope: '', graphOffset: 0, graphNode: null, nodeOffset: 0, nodeMore: false, nodeRequest: 0, stepOffset: 0, stepTotal: 0, stepMore: false, page: 0, more: false, busy: false, epoch: 0, timer: null, mounted: false, drawerEpoch: 0, events: [], receipts: [], receiptOffset: 0, receiptMore: false };
    const labels = { viewBlocker:'查看受阻原因', noBlockedIntents:'没有单条意图被标记为受阻；整条测评的原因见上方。', noMatchingSteps:'没有符合当前筛选条件的意图。', continueStopped:'继续测评',continueStoppedHint:'从原进度继续已停止的测评，保留已完成步骤和证据',closedReviewHint:'测评已结束。核对仅更新历史记录，不会启动测评；已停止的测评可通过“继续测评”恢复。',reviewSaved:'已记录核对，待核对列表已更新',conflict:'测评状态已更新，请刷新后重试。',stepChanged:'此步骤在核对期间发生了新的执行，请重新查看最新回执后确认。', liveUpdates:'有更新，查看最新', mainGraph:'主链路',fullGraph:'执行记录',graphRefs:'跨页引用',graphHidden:'已收起执行记录',graphOverflow:'还有跨页关联，可在节点详情中继续查看',upstream:'上游来源',downstream:'下游产出',moreRelations:'继续查看关联',openBranch:'定位关联分支',guidanceDelivered:'已发送给此意图',guidanceQueued:'已保存，将在此意图继续时生效', review:'处理待核对', reviewTitle:'未派发新步骤，有工具结果待确认', reviewHint:'点击“开始 / 继续”会先核对已保存的执行回执，已确认结束的步骤可直接续跑；仍有结果不明的调用时，保持停止并提示核对。', reviewStepHint:'确认只解除本步骤的待核对状态，不会自动执行；原有进度和证据保留。', reviewAcknowledge:'确认已核对', reviewDone:'没有待核对步骤。当前执行状态见页面顶部。', viewReceipts:'查看回执', notRunning:'当前未在执行，无需暂停', autoResumeState:'服务重启后自动续跑', enabled:'已开启', disabled:'已关闭', autoResumeHint:'打开页面不会启动任务；自动续跑只针对重启前的执行，草稿、手动暂停和待核对任务不会自动开始。', readerExpand:'展开阅读', readerBack:'返回图', resizeDetails:'调整节点详情宽度', graphHelp:'图例与说明', backToList:'返回测评列表', details:'详情', actions:'操作', archive:'归档', restore:'恢复到列表', currentTasks:'当前任务', archivedTasks:'已归档', archiveFilter:'归档状态', restoreHint:'恢复只回到列表，不会自动执行。', archiveHint:'归档保留进度与证据；执行中的任务请先暂停。', showDetails:'显示节点详情', hideDetails:'收起节点详情', scopeOptional: "补充限制与排除项（选填）", criteriaOptional: "验收条件（选填）", scopeHint: "仅补充目标中未说明的限制；留空沿用目标。", scopeExample: "例如：排除支付接口；不修改生产数据；不做压力测试。", criteriaHint: "说明什么结果算完成；留空沿用目标。", criteriaExample: "例如：报告列明已测范围、未测部分及原因；确认的漏洞附复现步骤和原始证据。", assets: '资产图', facts: '事实', priority: '优先级', linkedAssets: '目标资产', intentSources: '来源节点', findings: '关联漏洞', noFindings: '暂未登记漏洞。', openFinding: '在漏洞管理查看', allFindings: '在漏洞管理查看本次测评', reproduction: '复现步骤', findingEvidence: '复现证据', sourceStep: '来源步骤', allSteps: '显示全部步骤与回执', sourceFinding: '关联漏洞', graph: '探索图', continuous: '持续执行', mode: '运行模式', elapsed: '已运行', graphHint: '摘要用于续接；原始观察和执行回执可随时展开。', graphVisible: '当前页节点', graphFolded: '已折叠节点', graphHome: '全图概览', graphExpand: '展开关联节点', graphLayout: '整理布局', graphFit: '适应画布', graphSelect: '点击节点查看原始内容与来源。', graphMore: '继续读取原文', status: '状态', updatedAt: '更新时间', loading: '正在读取测评…', contextUsage:'上下文 / 窗口', toolDefinitions:'工具定义', title: '深度测评', subtitle: '围绕一个目标持续探索，保存证据与进度，跨会话继续执行。', create: '新建测评', refresh: '刷新', search: '搜索测评名称', all: '全部状态', empty: '暂无测评，先创建一个目标。', select: '选择测评，查看探索进度与证据。', previous: '上一页', next: '下一页', name: '测评名称', objective: '目标', scope: '补充限制与排除项', criteria: '验收条件', model: '模型渠道', project: '关联项目', role: '角色', default: '系统默认', none: '不关联', tokens: '累计 Token', rounds: '规划轮次', concurrency: '并行步骤', autoResume: '服务重启后自动恢复结果明确的步骤', save: '保存草稿', close: '关闭', start: '开始 / 继续', pause: '暂停', cancel: '停止', delete: '删除', report: '下载报告', overview: '概览', plan: '意图队列', evidence: '证据回执', activity: '运行记录', quality: '上下文与用量', guidance: '补充线索或调整方向', send: '提交方向', answer: '答复并继续', retry: '恢复此步骤', conversation: '查看执行会话', unknown: '请先核对回执，确认上次操作结果及执行资源状态。', reconcile: '我已核对调用结果并确认执行资源已停止，解除本步骤的待核对状态。任务仍需手动开始。', confirmStop: '停止后将保留已有结果，结束本次测评。', confirmDelete: '删除本条测评？关联执行会话仍保留。', strategy: '最近规划路线', summary: '当前结论 / 缺口', question: '待补充信息', dependencies: '前置步骤', segments: '执行分段', evidenceIDs: '证据引用', noSteps: '开始后由规划器逐步生成探索方向。', noRecords: '暂无记录', calls: '工具调用', compactions: '上下文压缩', activeTools: '当前可见工具', session: '会话', tokenNote: '累计用量包含 Pi 压缩，不作为任务自动结束条件。', recoveryNote: '结果不明的调用会暂停等待核对，已完成步骤不会自动重做。', saved: '已保存', updated: '已更新', retryNote: '恢复会保留原 Pi 会话及历史证据。' };
    const states = { archived: '已归档', draft: '草稿', running: '执行中', pending: '待执行', completed: '已完成', blocked: '受阻', failed: '失败', paused: '已暂停', pausing: '暂停中', cancelling: '停止中', cancelled: '已停止', recovering: '待恢复', waiting_retry: '等待模型恢复', waiting_input: '待答复', needs_review: '待核对', uncertain: '结果不明', waiting_progress: '等待新进展', interrupted: '已中断', cleanup_unconfirmed: '待确认资源停止', retired: '已撤下', reconciled: '已核对', yielded: '分段完成' };
    const tr = (key, fallback) => { const v = typeof window.t === 'function' ? window.t('deepAssessments.' + key) : ''; return v && v !== 'deepAssessments.' + key ? v : fallback; };
    const provenanceTabs = new Set(['evidence-path','asset-history','endpoint-review']);
    const provenanceText = tab => window.AssessmentProvenance?.text(({'evidence-path':'path','asset-history':'history','endpoint-review':'endpoints'})[tab]) || tab;
    const provenanceKey = () => JSON.stringify([S.id,S.tab,S.provenance?.asset,S.provenance?.endpoint,S.provenance?.fact,S.provenance?.criterion,S.provenance?.status,S.provenance?.offset]);
    const L = key => provenanceTabs.has(key) ? provenanceText(key) : key==='acceptance' ? window.AssessmentAcceptance?.text('title') || '验收证据' : tr(key, labels[key] || key);
    const status = key => tr('states.' + key, states[key] || key);
    const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
    const $ = id => document.getElementById(id);
    const active = t => ['running', 'pausing', 'cancelling'].includes(t?.status);
    const terminal = t => ['completed', 'cancelled'].includes(t?.status);
    const canFindings = () => typeof window.hasPermission !== 'function' || window.hasPermission('vulnerability:read');
    const can = action => typeof window.hasPermission !== 'function' || window.hasPermission('assessments:' + action);
    function notice(message, error = false) {
        clearTimeout(S.noticeTimer);
        ['da-notice', 'da-detail-notice', 'da-form-notice'].forEach(id => {
            const el = $(id);
            if (el) { el.textContent = message; el.classList.toggle('is-error', error); el.hidden = !message; }
        });
        if(message && !error) S.noticeTimer=setTimeout(()=>notice(''),4500);
    }
    function syncDrawerLock() {
        document.body.classList.toggle('da-drawer-open', !!$('da-form-drawer')?.open);
    }
    function openDrawer(id) {
        const drawer = $(id);
        window.prepareAppDrawer?.(drawer);
        if (!drawer.open) {
            drawer.returnFocus = document.activeElement;
            drawer.showModal();
        }
        syncDrawerLock();
        const target = drawer.querySelector('input:not([type="checkbox"]), textarea') || drawer.querySelector('button');
        target?.focus({ preventScroll: true });
    }
    function closeDrawer(id) {
        ++S.drawerEpoch;
        const drawer = $(id);
        if (drawer?.open) drawer.close();
        syncDrawerLock();
    }
    function bindDrawer(id) {
        const drawer = $(id);
        let outsideDown = false;
        const outside = event => {
            const rect = drawer.getBoundingClientRect();
            return event.target === drawer && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom);
        };
        drawer.addEventListener('pointerdown', event => { outsideDown = outside(event); });
        drawer.addEventListener('pointercancel', () => { outsideDown = false; });
        drawer.addEventListener('click', event => {
            const dismiss = outsideDown && outside(event); outsideDown = false;
            if (dismiss && !S.busy) closeDrawer(id);
        });
        drawer.addEventListener('cancel', event => {
            if (S.busy) event.preventDefault();
            else ++S.drawerEpoch;
        });
        drawer.addEventListener('close', () => {
            drawer.classList.remove('app-drawer-expanded');
            drawer.querySelector('.app-drawer-expand')?.setAttribute('aria-pressed', 'false');
            syncDrawerLock();
            requestAnimationFrame(() => {
                if (!$('page-deep-assessments')?.classList.contains('active')) return;
                const top = $('da-form-drawer').open ? $('da-form-drawer') : null;
                const prior = drawer.returnFocus;
                if (prior?.isConnected && !prior.disabled && prior.getClientRects().length && (!top || top.contains(prior))) prior.focus({ preventScroll: true });
                else if (top) top.querySelector('button')?.focus({ preventScroll: true });
                else if (S.detailVisible) $('da-title').focus({preventScroll:true});
                else ([...document.querySelectorAll('.da-task-name')].find(row => row.dataset.id === S.id) || document.querySelector('[data-da-action="create"]'))?.focus({ preventScroll: true });
            });
        });
    }
    function showWorkspace() {
        S.detailVisible = true;
        $('da-list-view').hidden = true;
        $('da-workspace').hidden = false;
        renderTask();
        $('da-title').focus({preventScroll:true});
    }
    function hideWorkspace() {
        ++S.epoch;
        S.detailVisible = false;
        $('da-workspace').hidden = true;
        $('da-list-view').hidden = false;
    }
    function syncDetailPanels() {
        $('da-workspace').classList.toggle('da-node-details-hidden', !S.detailsOpen);
        $('da-workspace').classList.toggle('da-node-reader', S.detailsOpen && S.readerExpanded);
        $('da-body').querySelectorAll('[data-da-action="graph-details"]').forEach(b => {
            if(b.classList.contains('da-node-close')) return;
            b.textContent = L(S.detailsOpen ? 'hideDetails' : 'showDetails');
            b.setAttribute('aria-expanded',String(S.detailsOpen));
        });
        $('da-body').querySelectorAll('[data-da-action="graph-reader"]').forEach(b => {
            b.textContent=L(S.readerExpanded?'readerBack':'readerExpand');
            b.setAttribute('aria-expanded',String(S.readerExpanded));
        });
        $('da-body').querySelectorAll('.da-panel-resize').forEach(bar=>{
            if(!bar.getClientRects().length)return;
            const width=Math.round(bar.nextElementSibling.getBoundingClientRect().width);
            bar.setAttribute('aria-controls',bar.nextElementSibling.id);
            bar.setAttribute('aria-valuemin','320');
            bar.setAttribute('aria-valuemax',String(Math.max(320,Math.min(760,bar.parentElement.clientWidth-360))));
            bar.setAttribute('aria-valuenow',String(width));bar.setAttribute('aria-valuetext',width+' px');
        });
    }

    function hideNodeDetails() { S.detailsOpen=false;S.readerExpanded=false;syncDetailPanels(); }
    function showNodeDetails() { S.detailsOpen=true; syncDetailPanels(); }
    function separator() { return '<div class="da-panel-resize" role="separator" aria-orientation="vertical" tabindex="0" aria-label="'+esc(L('resizeDetails'))+'"></div>'; }
    function installPanelResize(root) {
        const workspace=$('da-workspace');let drag=null;
        const persist=()=>{try{const width=workspace.style.getPropertyValue('--da-node-panel-width');if(width)sessionStorage.setItem('csai-assessment-node-width',width);else sessionStorage.removeItem('csai-assessment-node-width');}catch{}};
        const apply=(bar,width)=>{
            const frame=bar.closest('.da-graph-workspace');
            if(!frame || !bar.getClientRects().length)return;
            const max=Math.max(320,Math.min(760,frame.clientWidth-360));
            const value=Math.round(Math.max(320,Math.min(max,width)));
            workspace.style.setProperty('--da-node-panel-width',value+'px');
            bar.setAttribute('aria-valuemin','320');bar.setAttribute('aria-valuemax',String(max));bar.setAttribute('aria-valuenow',String(value));bar.setAttribute('aria-valuetext',value+' px');
        };
        try{const width=sessionStorage.getItem('csai-assessment-node-width');if(/^\d{3}px$/.test(width||'')&&parseInt(width)>=320&&parseInt(width)<=760)workspace.style.setProperty('--da-node-panel-width',width);}catch{}
        root.addEventListener('pointerdown',event=>{
            const bar=event.target.closest('.da-panel-resize');if(!bar || event.button!==0 || !bar.getClientRects().length)return;
            const panel=bar.nextElementSibling;drag={bar,id:event.pointerId,x:event.clientX,width:panel.getBoundingClientRect().width};
            bar.focus({preventScroll:true});bar.setPointerCapture(event.pointerId);workspace.classList.add('da-resizing');event.preventDefault();
        });
        root.addEventListener('pointermove',event=>{if(drag&&event.pointerId===drag.id)apply(drag.bar,drag.width-(event.clientX-drag.x));});
        const finish=event=>{if(!drag||event.pointerId!==drag.id)return;const previous=drag;drag=null;workspace.classList.remove('da-resizing');if(previous.bar.hasPointerCapture(previous.id))previous.bar.releasePointerCapture(previous.id);persist();};
        root.addEventListener('pointerup',finish);root.addEventListener('pointercancel',finish);root.addEventListener('lostpointercapture',finish);
        const barReset=bar=>requestAnimationFrame(()=>{if(bar?.isConnected){const width=Math.round(bar.nextElementSibling.getBoundingClientRect().width);bar.setAttribute('aria-valuenow',String(width));bar.setAttribute('aria-valuetext',width+' px');}});
        root.addEventListener('dblclick',event=>{if(event.target.closest('.da-panel-resize')){workspace.style.removeProperty('--da-node-panel-width');persist();barReset(event.target.closest('.da-panel-resize'));}});
        root.addEventListener('keydown',event=>{
            const bar=event.target.closest('.da-panel-resize');if(!bar||!bar.getClientRects().length)return;
            if(event.key==='Home'){workspace.style.removeProperty('--da-node-panel-width');persist();barReset(event.target.closest('.da-panel-resize'));event.preventDefault();}
            else if(['ArrowLeft','ArrowRight'].includes(event.key)){apply(bar,bar.nextElementSibling.getBoundingClientRect().width+(event.key==='ArrowLeft'?32:-32));persist();event.preventDefault();}
        });
    }
    function listHash() {
        const params = new URLSearchParams();
        if ($('da-search').value) params.set('q',$('da-search').value);
        if ($('da-filter').value) params.set('status',$('da-filter').value);
        if ($('da-archive-filter').value==='archived') params.set('archived','true');
        if(S.page) params.set('page',String(S.page));
        return '#deep-assessments' + (params.size ? '?' + params : '');
    }
    function writeListRoute(push=false) {
        const hash=listHash();
        if(hash!==location.hash) history[push?'pushState':'replaceState'](history.state,'',hash);
        S.routeKey=hash;
    }
    function returnToList() {
        writeListRoute(true);hideWorkspace();
        void refresh();
        requestAnimationFrame(()=>($('da-list').querySelector('.da-task-name[data-id="'+CSS.escape(S.id)+'"]') || $('da-search'))?.focus({preventScroll:true}));
    }
    async function api(path, options = {}) {
        const fetcher = typeof window.apiFetch === 'function' ? window.apiFetch : fetch;
        const response = await fetcher('/api/deep-assessments' + path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } });
        if (!response.ok) { let value; try { value = await response.json(); } catch {} const error=new Error(value?.code==='task_changed'||value?.error==='task changed; refresh and retry'?L('conflict'):value?.error || 'HTTP ' + response.status); error.status=response.status; throw error; }
        return response;
    }
    async function json(path, options) { return (await api(path, options)).json(); }
    const button = (action, label, extra = '') => `<button type="button" class="btn-secondary" data-da-action="${action}" ${extra}>${esc(label)}</button>`;
    const badge = value => `<span class="da-badge da-${esc(value)}">${esc(status(value))}</span>`;
    function writeRoute(push = false) {
        if (!S.id) return;
        const params = new URLSearchParams({ assessment_id: S.id, tab: S.tab });
        if($('da-search').value)params.set('list_q',$('da-search').value);
        if($('da-filter').value)params.set('list_status',$('da-filter').value);
        if($('da-archive-filter').value==='archived')params.set('archived','true');
        if(S.page)params.set('list_page',String(S.page));
        if (S.intentAsset) params.set('asset_id', S.intentAsset);
        const observation=S.tab==='assets'&&window.AssessmentExplorationViews?.observation();
        if(observation)params.set('observation',observation);
        if(S.tab==='assets'){
            for(const [key,value] of Object.entries(window.AssessmentExplorationViews?.routeState?.()||{})){
                if(value)params.set(key,value);
            }
        }
        if(provenanceTabs.has(S.tab)){
            const context=S.provenance||{};
            for(const [key,value] of Object.entries({asset_id:context.asset,endpoint_id:context.endpoint,fact_id:context.fact,criterion:context.criterion,endpoint_state:context.status,offset:context.offset,path_node:context.node})){
                if(value)params.set(key,value);
            }
        }
        if(S.tab==='acceptance'&&S.acceptanceFocus)params.set('criterion',String(S.acceptanceFocus));
        if(S.tab==='evidence'&&S.focusReceiptId)params.set('receipt_id',S.focusReceiptId);
        if(S.tab==='graph'&&S.routeGraphNode)params.set('node_id',S.routeGraphNode);
        if(S.intentStatus)params.set('step_status',S.intentStatus);
        if(S.graphScope)params.set('graph_scope',S.graphScope);if(S.graphView==='full')params.set('graph_view','full');
        if (S.focusStepId) params.set('step_id', S.focusStepId);
        if (S.originFindingId) params.set('vulnerability_id', S.originFindingId);
        const hash='#deep-assessments?' + params;
        if (hash!==location.hash) history[push?'pushState':'replaceState'](history.state, '', hash);
        S.routeKey = window.location.hash;
    }
    function applyRoute() {
        const [page, ...query] = window.location.hash.slice(1).split('?');
        if (page !== 'deep-assessments' || !$('page-deep-assessments')?.classList.contains('active') || (typeof window.hasPermission === 'function' && !window.hasPermission('assessments:read'))) return false;
        const params = new URLSearchParams(query.join('?')), id = params.get('assessment_id');
        if (!id) { $('da-search').value=params.get('q')||''; $('da-filter').value=params.get('status')||''; $('da-archive-filter').value=params.get('archived')==='true'?'archived':'current'; S.page=Math.max(0,Number(params.get('page'))||0); S.routeKey=location.hash; hideWorkspace(); return true; }
        if (S.routeKey === window.location.hash && S.detailVisible) return false;
        if(params.has('list_q'))$('da-search').value=params.get('list_q');
        if(params.has('list_status'))$('da-filter').value=params.get('list_status');
        if(params.has('archived'))$('da-archive-filter').value=params.get('archived')==='true'?'archived':'current';
        if(params.has('list_page'))S.page=Math.max(0,Number(params.get('list_page'))||0);
        const changed = S.id !== id;
        S.id = id; window.AssessmentExplorationViews?.reset(id); if (changed) { S.task = null; S.readerExpanded=false; S.detailsOpen=false; $('da-guidance').value = '';  }
        S.intentStatus=params.get('step_status')||''; S.intentAsset = params.get('asset_id') || ''; S.focusStepId = params.get('step_id') || ''; S.originFindingId = params.get('vulnerability_id') || '';
        S.focusedStep = null; S.originFinding = null; S.findings = []; S.findingOffset = 0; S.receiptOffset = 0; S.stepOffset = 0;
        S.tab = (['overview','graph','plan','evidence','activity','quality','findings','assets','facts','acceptance'].includes(params.get('tab'))||provenanceTabs.has(params.get('tab'))) ? params.get('tab') : (S.focusStepId ? 'plan' : 'graph');
        if(S.tab==='assets'){
            window.AssessmentExplorationViews?.restoreObservation(params.get('observation'));
            if(S.intentAsset){window.AssessmentExplorationViews?.focusAsset(S.intentAsset);S.detailsOpen=true;}
            if(params.has('asset_node')){
                window.AssessmentExplorationViews?.restoreRouteState?.(params);
                S.detailsOpen=true;
            }
        }
        if(provenanceTabs.has(S.tab)){
            S.provenance={asset:params.get('asset_id')||'',endpoint:params.get('endpoint_id')||'',fact:params.get('fact_id')||'',
                criterion:Math.max(0,Number(params.get('criterion'))||0),status:params.get('endpoint_state')||'',
                offset:Math.max(0,Number(params.get('offset'))||0),node:params.get('path_node')||''};
            S.intentAsset='';S.focusStepId='';S.originFindingId='';
            S.detailsOpen=S.tab==='evidence-path';S.readerExpanded=false;
        }
        if(S.tab==='facts')window.AssessmentExplorationViews?.resetFacts();
        if($('da-acceptance-form'))closeDrawer('da-form-drawer');
        window.AssessmentAcceptance?.reset(id);
        S.acceptanceFocus=S.tab==='acceptance' ? Number(params.get('criterion'))||0 : 0;
        S.revealAcceptance=!!S.acceptanceFocus;
        if(S.revealAcceptance)window.AssessmentAcceptance?.setFilter('');
        S.focusReceiptId=S.tab==='evidence' ? params.get('receipt_id')||'' : '';
        S.routeGraphNode=S.tab==='graph' ? params.get('node_id')||'' : '';
        S.graph = null; S.graphScope = S.routeGraphNode || params.get('graph_scope') || ''; S.graphView=params.get('graph_view')==='full'?'full':'summary'; S.graphNode = null; S.relations=[]; S.graphOffset = 0; S.events = []; S.receipts = [];
        S.routeKey = window.location.hash; S.renderSignature=''; ++S.epoch;
        showWorkspace(); return true;
    }
    function sourceBanner(includeEvidence = false) {
        if (!S.focusStepId && !S.originFinding) return '';
        const finding = S.originFinding;
        return `<section class="da-source-banner"><strong>${esc(L('sourceStep'))}：${esc(S.focusedStep?.title || finding?.assessment_step_title || S.focusStepId)}</strong>${finding ? `<p>${esc(L('sourceFinding'))}：${esc(finding.title)}</p>` : ''}${includeEvidence && finding ? `<h4>${esc(L('reproduction'))}</h4><pre>${esc(finding.reproduction_steps)}</pre><h4>${esc(L('findingEvidence'))}</h4><pre>${esc(finding.evidence)}</pre>${window.AssessmentFindingLinks?.reviews(finding) || ''}` : ''}<div class="da-controls">${button('clear-source', L('allSteps'))}${finding ? button('open-finding', L('openFinding'), `data-vulnerability="${esc(finding.id)}"`) : ''}</div></section>`;
    }
    function mount() {
        const root = $('deep-assessments-root'); if (!root || S.mounted) return;
        S.mounted = true;
        root.innerHTML = `
            <div id="da-list-view" class="da-list-view"><div class="page-header da-header"><div><h2>${esc(L('title'))}</h2><p>${esc(L('subtitle'))}</p></div><div>${button('refresh', L('refresh'))}${can('write') ? button('create', L('create')) : ''}</div></div>
            <div id="da-notice" class="da-notice" role="status" hidden></div>
            <section class="da-list-panel">
                <div class="da-filters"><input id="da-search" class="form-control" aria-label="${esc(L('search'))}" placeholder="${esc(L('search'))}"><select id="da-filter" class="form-control" aria-label="${esc(L('all'))}"><option value="">${esc(L('all'))}</option>${Object.keys(states).filter(s => !['archived', 'pending', 'retired', 'reconciled', 'yielded'].includes(s)).map(s => `<option value="${s}">${esc(status(s))}</option>`).join('')}</select><select id="da-archive-filter" class="form-control" aria-label="${esc(L('archiveFilter'))}"><option value="current">${esc(L('currentTasks'))}</option><option value="archived">${esc(L('archivedTasks'))}</option></select></div>
                <div class="da-list-head" aria-hidden="true"><span>${esc(L('name'))}</span><span>${esc(L('status'))}</span><span>${esc(L('rounds'))}</span><span>${esc(L('tokens'))}</span><span>${esc(L('updatedAt'))}</span><span>${esc(L('actions'))}</span></div>
                <div id="da-list"></div>
                <div class="da-pagination">${button('previous', L('previous'))}<span id="da-page"></span>${button('next', L('next'))}</div>
            </section>
            </div>
            <section id="da-workspace" class="da-workspace da-node-details-hidden" aria-labelledby="da-title" hidden>
                <header class="da-workspace-header"><div class="da-workspace-heading">${button('back-list','←','aria-label="'+esc(L('backToList'))+'" title="'+esc(L('backToList'))+'"')}<div class="da-title-stack"><div id="da-title" tabindex="-1"></div><div id="da-task-meta" class="da-muted"></div></div></div><div id="da-controls" class="da-controls"></div></header>
                <div id="da-detail-notice" class="da-notice" role="status" hidden></div>
                <div id="da-tabs" class="da-tabs" role="tablist"></div>
                <button type="button" id="da-overview-updates" class="btn-secondary" data-da-action="live-latest" hidden>${esc(L('liveUpdates'))}</button><div class="da-detail-scroll"><section id="da-task-blocker" class="da-task-blocker" role="status" tabindex="-1" hidden></section><div id="da-recovery-notice" class="da-recovery-notice" role="status" hidden></div><div id="da-body"></div></div>
                <details id="da-guidance-panel"><summary>${esc(L('guidance'))}</summary><form id="da-guidance-form" hidden><label for="da-guidance">${esc(L('guidance'))}</label><textarea id="da-guidance" class="form-control" rows="2" maxlength="8000"></textarea><button class="btn-primary" id="da-guidance-submit" type="submit">${esc(L('send'))}</button></form></details>
            </section>
            <dialog id="da-form-drawer" class="da-drawer da-form-drawer" aria-labelledby="da-form-title">
                <header class="da-drawer-header"><h3 id="da-form-title"></h3><button type="button" class="da-drawer-close" data-da-action="close" aria-label="${esc(L('close'))}">&times;</button></header>
                <div id="da-form-notice" class="da-notice" role="alert" hidden></div><div id="da-form-content"></div>
            </dialog>`;
        bindDrawer('da-form-drawer');
        installPanelResize(root);
        window.addEventListener('hashchange', () => {
            if (window.location.hash.slice(1).split('?')[0] !== 'deep-assessments') { closeDrawer('da-form-drawer'); hideWorkspace(); } else if (applyRoute()) void refresh();
        });
        root.addEventListener('click', event => { const target = event.target.closest('[data-da-action]'); if (target) void action(target.dataset.daAction, target.dataset); });
        root.addEventListener('submit', event => { event.preventDefault(); if (event.target.id === 'da-asset-search') void action('asset-search'); if (event.target.id === 'da-create-form') void saveCreate(event.target); if (event.target.id === 'da-guidance-form') void guidance();if(event.target.dataset.daIntentGuidance)void action('intent-guidance',{step:event.target.dataset.daIntentGuidance,message:event.target.querySelector('textarea').value});  });
        root.addEventListener('input',event=>{const form=event.target.closest('[data-da-intent-guidance]');if(form&&event.target.tagName==='TEXTAREA')S.stepDrafts[form.dataset.daIntentGuidance]=event.target.value;});
        root.addEventListener('change', event => { if(event.target.dataset.daIntentPriority)void action('intent-priority',{step:event.target.dataset.daIntentPriority,priority:Number(event.target.value)}); if (event.target.id === 'da-intent-status') { S.intentStatus=event.target.value; S.stepOffset=0;writeRoute(); void refresh(); } if (event.target.id === 'da-graph-select' && event.target.value) void selectGraphNode(event.target.value); });
        let searchTimer;
        $('da-search').addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { S.page = 0; writeListRoute(); void refresh(); }, 250); });
        $('da-archive-filter').addEventListener('change', () => { S.page=0;writeListRoute();void refresh(); });
        $('da-filter').addEventListener('change', () => { S.page = 0; writeListRoute(); void refresh(); });
    }
    const canExecute = () => typeof window.hasPermission !== 'function' || window.hasPermission('agent:execute');
    const needsReview = task => ['needs_review','uncertain','cleanup_unconfirmed'].includes(task.status)||Number(task.review_count)>0||(task.steps||[]).some(s=>['uncertain','cleanup_unconfirmed'].includes(s.status));
    const canStart = task => !task.archived && !terminal(task) && !active(task) && !task.question && task.status!=='waiting_input' && canExecute();
    const canContinueStopped=task=>task.status==='cancelled'&&!task.archived&&canExecute();
    const canPause = task => !task.archived && ['running','recovering','waiting_retry','waiting_progress'].includes(task.status);
    const canArchive = task => !needsReview(task) && !task.archived && !active(task) && !['recovering','needs_review','uncertain','cleanup_unconfirmed'].includes(task.status);
    function renderList() {
        const list=$('da-list'),scroll=list.scrollTop;
        list.innerHTML=S.listTasks.map(task=>{
            const busy=S.rowBusy.has(task.id),id='data-id="'+esc(task.id)+'"';
            const controls=(can('write')?(task.status==='cancelled'?button('row-continue-stopped',L('continueStopped'),id+(!canContinueStopped(task)||busy?' disabled':'')+' title="'+esc(L('continueStoppedHint'))+'"'):button('row-start',L('start'),id+(!canStart(task)||busy?' disabled':'')))+button('row-pause',L('pause'),id+(!canPause(task)||busy?' disabled title="'+esc(L('notRunning'))+'"':'')):'')
                +button('details',L('details'),id)
                +(can('write') ? button(task.archived?'row-restore':'row-archive',L(task.archived?'restore':'archive'),id+((!task.archived&&!canArchive(task))||busy?' disabled':'')+' title="'+esc(L(task.archived?'restoreHint':needsReview(task)?'reviewHint':'archiveHint'))+'"') : '');
            return `<article class="da-list-item ${task.id===S.id?'selected':''}" data-id="${esc(task.id)}" aria-busy="${busy}"><button type="button" class="da-task-name" data-da-action="select" ${id}>${esc(task.name)}</button><span>${needsReview(task)?`<button type="button" class="da-review-status" data-da-action="review" ${id} aria-label="${esc(L('review'))}" title="${esc(L('review'))}">${badge(task.status)}${terminal(task)&&task.review_count?`<small class="da-review-count">${esc(status('needs_review'))} ${task.review_count}</small>`:''}</button>`:badge(task.status)}${task.archived?badge('archived'):''}</span><span class="da-row-rounds"><small>${esc(L('rounds'))}</small>${task.rounds||0}</span><span class="da-row-tokens"><small>${esc(L('tokens'))}</small>${Number(task.tokens_used||0).toLocaleString()}</span><time>${esc(new Date(task.updated_at).toLocaleString())}</time><div class="da-row-actions" aria-label="${esc(task.name+' '+L('actions'))}">${controls}</div></article>`;
        }).join('')||`<p class="da-muted da-empty">${esc(L('empty'))}</p>`;
        list.scrollTop=scroll;$('da-page').textContent=String(S.page+1);
        $('da-list-view').querySelector('[data-da-action="previous"]').disabled=S.page===0;
        $('da-list-view').querySelector('[data-da-action="next"]').disabled=!S.more;
    }
    async function rowControl(id, action) {
        if(!can('write') || S.rowBusy.has(id))return;
        S.rowBusy.add(id);renderList();
        try {
            // The list can be stale; obtain the current revision before a control request.
            const current=await json('/'+encodeURIComponent(id));
            const data=await json('/'+encodeURIComponent(id)+'/control/'+action,{method:'POST',body:JSON.stringify({revision:current.task.revision})});
            if(S.task?.id===id)S.task=data.task;
            notice(data.start_error||L('updated'),!!data.start_error);
        } catch(error) { notice(error.message,true); }
        finally { S.rowBusy.delete(id);await refresh(); }
    }
    function renderTask() {
        if(!S.detailVisible)return;
        $('da-workspace').dataset.view=S.tab;
        const task = S.task;
        if (!task) { $('da-task-blocker').hidden=true; $('da-overview-updates').hidden=true;$('da-recovery-notice').hidden=true; $('da-task-meta').textContent=''; $('da-title').textContent = L('title'); $('da-controls').innerHTML = ''; $('da-tabs').innerHTML = ''; $('da-body').textContent = L(S.id ? 'loading' : 'select'); $('da-guidance-form').hidden = true; $('da-guidance-panel').hidden = true; return; }
        $('da-title').innerHTML = `<h2>${esc(task.name)} ${task.status==='blocked'?`<button type="button" class="da-review-status" data-da-action="blocked-reason">${badge(task.status)}</button>`:badge(task.status)} ${task.archived?badge('archived'):''}</h2>`;
        $('da-task-meta').textContent=[L('model')+' '+(task.model||L('default')),L('role')+' '+(task.role||L('default')),L('rounds')+' '+(task.rounds||0),L('tokens')+' '+Number(task.tokens_used||0).toLocaleString()].join(' · ');
        $('da-controls').innerHTML = `${can('write')&&task.status==='cancelled'?button('continue-stopped',L('continueStopped'),!canContinueStopped(task)?'disabled':''):''}${can('write') && !terminal(task) && !task.archived ? button('start',L('start'),!canStart(task)?'disabled':'')+button('pause',L('pause'),!canPause(task)?'disabled':'')+button('cancel', L('cancel')) : ''}${can('write') ? button(task.archived?'restore':'archive',L(task.archived?'restore':'archive'),!task.archived && !canArchive(task)?'disabled title="'+esc(L(needsReview(task)?'reviewHint':'archiveHint'))+'"':'') : ''}${button('report', L('report'))}${can('delete') && !active(task) ? button('delete', L('delete')) : ''}`;
        const blocker=$('da-task-blocker');blocker.hidden=task.status!=='blocked';const blockerHTML=window.AssessmentWorkbench?.blocker(task,S.runtime)||'';if(blocker.renderedHTML!==blockerHTML){blocker.innerHTML=blockerHTML;blocker.renderedHTML=blockerHTML;}
        const recovery=$('da-recovery-notice');recovery.hidden=!needsReview(task);
        recovery.innerHTML=needsReview(task)?`<div><strong>${esc(L('reviewTitle'))}</strong><p>${esc(L(terminal(task)?'closedReviewHint':'reviewHint'))}</p></div>${button('review',L('review'))}`:'';
        $('da-tabs').innerHTML = ['overview', 'acceptance', ...(window.AssessmentExplorationViews?.canAssets() ? ['assets'] : []), 'graph', ...(provenanceTabs.has(S.tab)?[S.tab]:[]), 'plan', 'facts', ...(canFindings() ? ['findings'] : []), 'evidence', 'activity', 'quality'].map(tab => `<button type="button" role="tab" aria-selected="${S.tab === tab}" data-da-action="tab" data-tab="${tab}" class="${S.tab === tab ? 'active' : ''}">${esc(L(tab))}</button>`).join('');
        const body = $('da-body'), scroller = $('da-workspace').querySelector('.da-detail-scroll'), scroll = scroller.scrollTop;
        const opened = new Set([...body.querySelectorAll('details[open]')].map(el => el.dataset.step));
        const expandedDetails = new Set([...body.querySelectorAll('details[data-da-detail][open]')].map(el => el.dataset.daDetail));
        const focused=document.activeElement,form=focused?.closest?.('[data-da-intent-guidance]');const editing=form&&body.contains(focused)?{step:form.dataset.daIntentGuidance,start:focused.selectionStart,end:focused.selectionEnd}:null;
        const signature=JSON.stringify([S.id,S.tab,S.busy,S.tab==='overview'?[task.status,task.tokens_used,task.rounds,task.summary,task.strategy,task.question,task.auto_resume,S.runtime]:S.tab==='plan'?[task.steps,S.focusedStep,S.intentStatus,S.intentCounts,task.status,task.archived]:S.tab==='graph'?[S.graph,S.graphScope,S.graphView]:S.tab==='assets'||S.tab==='facts'?S.extra:S.tab==='findings'?[S.findings,S.findingOffset]:provenanceTabs.has(S.tab)?[provenanceKey(),S.provenanceData]:S.tab==='acceptance'?[S.acceptance,S.acceptanceFocus]:S.tab==='evidence'?[S.receipts,S.focusStepId,S.originFinding,S.focusReceiptId]:S.tab==='activity'?S.events:task.metrics]);
        let unchanged=S.renderSignature===signature;const hold=S.tab==='overview'&&S.renderTab==='overview'&&S.renderStatus===task.status&&!S.busy&&!S.renderWasBusy&&!unchanged&&scroller.scrollTop>120;$('da-overview-updates').hidden=!hold;if(hold)unchanged=true;else{S.renderSignature=signature;S.renderTab=S.tab;S.renderStatus=task.status;S.renderWasBusy=S.busy;}
        if(!unchanged){
        if (!['graph','assets','evidence-path'].includes(S.tab)) window.DeepAssessmentGraph?.destroy();
        if (S.tab === 'overview') {
            const seconds = (task.active_seconds || 0) + (active(task) && task.run_started_at ? Math.max(0, (Date.now() - Date.parse(task.run_started_at)) / 1000) : 0);
            body.innerHTML = `<div class="da-stats"><button type="button" data-da-action="${task.status==='blocked'?'blocked-reason':'overview-tab'}" data-tab="activity"><small>${esc(L('status'))}</small><strong>${badge(task.status)}</strong><small>${esc(L('elapsed'))} ${(seconds / 3600).toFixed(1)} h</small><span class="da-stat-link">${esc(L(task.status==='blocked'?'viewBlocker':'activity'))} →</span></button><button type="button" data-da-action="overview-tab" data-tab="quality"><small>${esc(L('tokens'))}</small><strong>${Number(task.tokens_used || 0).toLocaleString()}</strong><span class="da-stat-link">${esc(L('quality'))} →</span></button><button type="button" data-da-action="overview-tab" data-tab="activity"><small>${esc(L('rounds'))}</small><strong>${task.rounds || 0}</strong><span class="da-stat-link">${esc(L('activity'))} →</span></button></div><p class="da-muted">${esc(L('autoResumeState'))}：${esc(L(task.auto_resume?'enabled':'disabled'))}。${esc(L('autoResumeHint'))}</p>${window.AssessmentWorkbench?.overview(task,S.runtime,status)||''}<details class="da-task-brief" data-da-detail="task-brief"><summary>${esc(window.AssessmentWorkbench?.text('brief')||L('objective'))}</summary>${['objective', 'scope', 'criteria', 'strategy', 'summary', 'question'].filter(k => task[k]).map(k => `<section class="da-section"><h4>${esc(L(k))}</h4><pre>${esc(task[k])}</pre></section>`).join('')}</details><p class="da-muted">${esc(L('recoveryNote'))}</p>`;
        } else if (provenanceTabs.has(S.tab)) {
            const data=S.provenanceData?.key===provenanceKey()?S.provenanceData.value:null;
            window.AssessmentProvenance?.render(body,S.tab,data,S.provenance,S.id,can('write'),node=>{
                S.provenance.node=node;writeRoute();
            });
        } else if (S.tab === 'acceptance') {
            window.AssessmentAcceptance?.reset(S.id);
            window.AssessmentAcceptance?.render(body, S.acceptance?.id===S.id ? S.acceptance.rows : null, can('write'), S.acceptanceFocus);
        } else if (S.tab === 'graph') {
            renderGraph(body);
        } else if (S.tab === 'assets' || S.tab === 'facts') {
            window.AssessmentExplorationViews?.render(body, S.tab, S.extra, can('write') && !task.archived);
        } else if (S.tab === 'plan') {
            body.innerHTML = sourceBanner() + (S.focusedStep ? [S.focusedStep] : task.steps || []).map(step => `<details class="da-step" data-step="${esc(step.id)}" ${opened.has(step.id) || step.id === S.focusStepId || S.intentStatus==='needs_review' ? 'open' : ''}><summary>${badge(step.status)} <strong>${esc(step.title)}</strong><small>${esc(step.id)}</small></summary>${window.AssessmentWorkbench?.controls(step,task,can('write'),S.stepDrafts[step.id])||''}<pre>${esc(step.instruction)}</pre><p>${esc(L('priority'))}: ${step.priority || 100} · ${esc(L('linkedAssets'))}: ${(step.asset_ids || []).map(id=>window.AssessmentExplorationViews?.canAssets()?button('intent-asset',id,`data-asset="${esc(id)}"`):esc(id)).join(' ') || '—'}</p><p>${esc(L('intentSources'))}: ${esc((step.source_ids || []).join(', ') || '—')}</p><p>${esc(L('dependencies'))}: ${esc((step.depends_on || []).join(', ') || '—')} · ${esc(L('segments'))}: ${step.attempts || 0}</p><pre>${esc(step.summary)}</pre><p>${esc(L('evidenceIDs'))}: ${esc((step.evidence || []).join(', ') || '—')}</p>${['uncertain','cleanup_unconfirmed'].includes(step.status)?`<p class="da-review-step-note">${esc(L('reviewStepHint'))}</p>`:''}<div class="da-controls">${button('fact-proof',L('viewReceipts'),`data-step="${esc(step.id)}"`)}${step.conversation_id ? button('conversation', L('conversation'), `data-conversation="${esc(step.conversation_id)}"`) : ''}${!task.archived && !active(task) && can('write') && (['uncertain','cleanup_unconfirmed'].includes(step.status)||!terminal(task)&&['failed','blocked','interrupted'].includes(step.status)) ? button(['uncertain','cleanup_unconfirmed'].includes(step.status)?'reconcile':'retry', L(['uncertain','cleanup_unconfirmed'].includes(step.status)?'reviewAcknowledge':'retry'), `data-step="${esc(step.id)}"`) : ''}</div></details>`).join('') || `<p>${esc(L(S.intentStatus==='needs_review'?'reviewDone':S.intentStatus==='blocked'&&task.status==='blocked'?'noBlockedIntents':S.intentStatus||S.intentAsset?'noMatchingSteps':'noSteps'))}</p>`;
            if (!S.focusStepId) body.insertAdjacentHTML('afterbegin', `<div class="da-controls">${S.intentAsset?button('clear-intent-asset',L('allSteps')):''}<select id="da-intent-status" class="form-control" aria-label="${esc(L('status'))}"><option value="">${esc(L('all'))}</option>${['needs_review','pending','running','pausing','paused','completed','blocked','failed','uncertain','cleanup_unconfirmed','interrupted','retired','reconciled'].map(k=>`<option value="${k}" ${S.intentStatus===k?'selected':''}>${esc(status(k))} (${S.intentCounts[k]||0})</option>`).join('')}</select></div>`);
            if (!S.focusStepId) body.innerHTML += `<div class="da-controls">${S.stepOffset ? button('step-previous', L('previous')) : ''}<span>${(task.steps || []).length} / ${S.stepTotal}</span>${S.stepMore ? button('step-next', L('next')) : ''}</div>`;
        } else if (S.tab === 'findings') {
            body.innerHTML = `<div class="da-controls">${button('all-findings', L('allFindings'))}<span>${S.findingTotal}</span></div>` + (S.findings.map(v => `<article class="da-finding"><h4>${esc(v.title)}</h4><p>${esc(v.severity)} · ${esc(v.status)}</p><p>${esc(L('sourceStep'))}：${esc(v.assessment_step_title || v.assessment_step_id)}</p><div class="da-controls">${button('open-finding', L('openFinding'), `data-vulnerability="${esc(v.id)}"`)}${button('finding-evidence', L('findingEvidence'), `data-vulnerability="${esc(v.id)}" data-step="${esc(v.assessment_step_id)}"`)}</div></article>`).join('') || `<p>${esc(L('noFindings'))}</p>`) + `<div class="da-controls">${S.findingOffset ? button('previous-findings', L('previous')) : ''}${S.findingMore ? button('next-findings', L('next')) : ''}</div>`;
        } else if (S.tab === 'evidence') {
            body.innerHTML = (S.focusReceiptId ? '<div class="da-controls">'+button('clear-receipt',L('evidence'))+'</div>' : '') + sourceBanner(true) + S.receipts.map(row => `<details class="da-step" data-step="${esc(row.id)}" ${opened.has(row.id)||row.id===S.focusReceiptId ? 'open' : ''}><summary>${badge(row.status)} ${esc(row.tool)} · ${esc(row.step_id)}</summary><code>${esc(row.execution_id || row.id)}</code><pre>${esc(row.arguments)}</pre><pre>${esc(row.result)}</pre></details>`).join('') || `<p>${esc(L('noRecords'))}</p>`;
            body.innerHTML += `<div class="da-controls">${S.receiptOffset > 0 ? button('previous-receipts', L('previous')) : ''}${S.receiptMore ? button('more-receipts', L('next')) : ''}</div>`;
        } else if (S.tab === 'activity') {
            body.innerHTML = S.events.slice().reverse().map(row => `<div class="da-event"><small>${esc(new Date(row.created_at).toLocaleString())} · ${esc(row.step_id)} · ${esc(row.kind)}</small><pre>${esc(row.message)}</pre></div>`).join('') || `<p>${esc(L('noRecords'))}</p>`;
        } else {
            body.innerHTML = `<p class="da-muted">${esc(L('tokenNote'))}</p><div class="da-table-wrap"><table><thead><tr><th>${esc(L('session'))}</th><th>Token</th><th>${esc(L('compactions'))}</th><th>${esc(L('activeTools'))}</th><th>${esc(L('contextUsage'))}</th><th>${esc(L('toolDefinitions'))}</th></tr></thead><tbody>${Object.entries(task.metrics || {}).map(([id, m]) => `<tr><td>${esc(m.step_id)}<br><small>${esc(id.slice(0, 12))}</small></td><td>${Number(m.tokens || 0).toLocaleString()}</td><td>${m.compactions || 0}</td><td>${m.tool_count || 0}</td><td>${m.context_tokens == null ? '—' : Number(m.context_tokens).toLocaleString()} / ${Number(m.context_window || 0).toLocaleString()}</td><td>${(Number(m.tool_definition_bytes || 0) / 1024).toFixed(1)} KB</td></tr>`).join('')}</tbody></table></div>`;
        }
        }
        body.querySelectorAll('details[data-da-detail]').forEach(el => { el.open = expandedDetails.has(el.dataset.daDetail); });
        if(!unchanged&&editing){const input=body.querySelector('[data-da-intent-guidance="'+CSS.escape(editing.step)+'"] textarea');if(input){input.focus({preventScroll:true});input.setSelectionRange(editing.start,editing.end);}}
        scroller.scrollTop = scroll;if(!hold)window.AssessmentWorkbench?.refreshClocks(body);
        $('da-guidance-form').hidden = !can('write') || terminal(task) || task.archived;
        $('da-guidance-panel').hidden = $('da-guidance-form').hidden;
        syncDetailPanels();
        $('da-guidance-submit').textContent = L(task.question ? 'answer' : 'send');
        if (S.busy) {$('da-controls').querySelectorAll('button').forEach(b => b.disabled = true);body.querySelectorAll('[data-da-intent-priority],[data-da-action="intent-pause"],[data-da-action="intent-resume"],[data-da-intent-guidance] button').forEach(b=>b.disabled=true);}
    }
    async function refresh() {
        if (S.busy || !S.mounted) return;
        const epoch = ++S.epoch, id = S.detailVisible ? S.id : '';
        try {
            const query = new URLSearchParams({ q: $('da-search').value, status: $('da-filter').value, limit: '20', offset: String(S.page * 20), archived: String($('da-archive-filter').value==='archived') });
            const [list, detail] = await Promise.all([id ? null : json('?' + query), id ? json('/' + encodeURIComponent(id) + '?runtime=true&step_limit=50&step_offset=' + S.stepOffset + (S.tab === 'plan' && !S.focusStepId ? '&step_order=queue&step_status=' + encodeURIComponent(S.intentStatus) + '&step_asset=' + encodeURIComponent(S.intentAsset) : '')) : null]);
            if (epoch !== S.epoch || S.busy) return;
            if (list) { S.more=list.has_more; S.listTasks=list.tasks||[]; renderList(); }
            if (detail && (!S.task || detail.task.revision >= S.task.revision)) S.task = detail.task;
            if (detail) S.stepOffset=detail.step_offset??S.stepOffset;
            if (detail) { S.runtime=detail.runtime||null; S.intentCounts = detail.intent_counts || {}; S.stepTotal = detail.step_total ?? detail.task.steps?.length ?? 0; S.stepMore = !!detail.step_has_more; }
            if (id && S.originFindingId && canFindings()) {
                const data = await json('/' + encodeURIComponent(id) + '/vulnerabilities/' + encodeURIComponent(S.originFindingId)); if (epoch !== S.epoch) return;
                S.originFinding = data.vulnerability; S.focusedStep = data.step; S.focusStepId = data.vulnerability.assessment_step_id;
            } else if (id && S.focusStepId) {
                const data = await json('/' + encodeURIComponent(id) + '/steps/' + encodeURIComponent(S.focusStepId)); if (epoch !== S.epoch) return; S.focusedStep = data.step;
            }
            if (id && ['assets','facts'].includes(S.tab)) { const data = await window.AssessmentExplorationViews.load(id,S.tab,json,S.intentAsset); if (epoch !== S.epoch) return; S.extra = data; }
            if (id && provenanceTabs.has(S.tab)) {
                const key=provenanceKey();
                const data=await window.AssessmentProvenance.load(id,S.tab,S.provenance,json);
                if(epoch!==S.epoch || key!==provenanceKey())return;
                S.provenanceData={key,value:data};
            }
            if (id && S.tab === 'acceptance') {
                const data=await json('/'+encodeURIComponent(id)+'/acceptance');
                if(epoch!==S.epoch)return;
                S.acceptance={id,rows:data.rows};
            }
            if (id && S.tab === 'findings' && canFindings()) {
                const data = await json('/' + encodeURIComponent(id) + '/vulnerabilities?limit=20&offset=' + S.findingOffset); if (epoch !== S.epoch) return;
                S.findings = data.vulnerabilities || []; S.findingTotal = data.total; S.findingMore = data.has_more;
            }
            if (id && S.tab === 'graph') { const data = await json('/' + encodeURIComponent(id) + '/graph?' + new URLSearchParams({ scope: S.graphScope, view:S.graphView, offset: S.graphOffset, limit: 80 })); if (epoch !== S.epoch) return; S.graph = data; }
            if (id && S.tab === 'activity') { const data = await json('/' + encodeURIComponent(id) + '/events?after=' + (S.events.at(-1)?.id || 0)+(S.events.length?'':'&latest=true')); if (epoch !== S.epoch) return; S.events.push(...data.events); S.events = S.events.slice(-500); }
            if (id && S.tab === 'evidence') { const data = await json('/' + encodeURIComponent(id) + '/receipts?' + new URLSearchParams({ offset: S.receiptOffset, step_id: S.focusStepId, receipt_id: S.focusReceiptId||'' })); if (epoch !== S.epoch) return; S.receipts = data.receipts; S.receiptMore = data.has_more; }
            renderTask();
            if(S.tab==='acceptance'&&S.revealAcceptance){
                S.revealAcceptance=false;
                const row=$('da-body').querySelector('[data-acceptance-criterion="'+CSS.escape(String(S.acceptanceFocus))+'"]');
                row?.focus({preventScroll:true});
                row?.scrollIntoView({block:'nearest'});
            }
            if(S.tab==='graph'&&S.routeGraphNode){
                const node=S.routeGraphNode;
                S.routeGraphNode='';
                await selectGraphNode(node);
            }
        } catch (error) { if (epoch === S.epoch) notice(error.message, true); }
    }
    const graphKind = kind => tr('graphKinds.' + kind, { fact: '事实', finding: '漏洞', goal: '目标', step: '探索意图', observation: '观察', receipt: '执行回执', memory: 'Pi 摘要', digest: '历史归档', guidance: '人工线索' }[kind] || kind);
    function renderGraph(body) {
        if (!body.querySelector('#da-graph-canvas')) {
            window.DeepAssessmentGraph?.destroy();
            body.innerHTML = `<div class="da-graph-toolbar">${button('graph-view',L('mainGraph'),'data-view="summary"')}${button('graph-view',L('fullGraph'),'data-view="full"')}${button('graph-home', L('graphHome'))}${button('graph-fit', L('graphFit'))}${button('graph-details',L(S.detailsOpen?'hideDetails':'showDetails'),'aria-expanded="'+S.detailsOpen+'"')}${button('graph-layout',L('graphLayout'))}${button('graph-in', '+', 'aria-label="Zoom in"')}${button('graph-out', '−', 'aria-label="Zoom out"')}<span id="da-graph-meta"></span></div><div class="da-graph-navigation"><select id="da-graph-select" class="form-control" aria-label="${esc(L('graphSelect'))}"></select><details class="da-view-help" data-da-detail="graph-help"><summary>${esc(L('graphHelp'))}</summary><p class="da-muted">${esc(L('graphHint'))}</p><div class="da-graph-legend">${['goal','step','fact','finding','observation','receipt','memory','digest','guidance'].map(k => `<span class="da-kind-${k}">${esc(graphKind(k))}</span>`).join('')}</div><div class="da-relation-legend">${['depends_on','informs','produces','supports'].map(k=>`<span class="da-relation-${k}">${esc(tr('graphRelations.'+k,({depends_on:'依赖',informs:'派生',produces:'产出',supports:'支持'})[k]))}</span>`).join('')}</div></details></div><div class="da-graph-workspace"><div id="da-graph-canvas" role="img" aria-label="${esc(L('graph'))}"></div>${separator()}<section id="da-graph-node" aria-live="polite"></section></div><div id="da-graph-pages" class="da-controls"></div>`;
        }
        if (!S.graph) { $('da-graph-meta').textContent = L('loading'); return; }
        $('da-graph-meta').textContent = `${L('graphVisible')} ${(S.graph.primary_count??Math.max(0,S.graph.nodes.length-1))} / ${S.graph.total} · ${L('graphFolded')} ${S.graph.folded} · ${L('graphRefs')} ${(S.graph.boundary_total||0)-(S.graph.boundary_more||0)}/${S.graph.boundary_total||0} · ${L('graphHidden')} ${S.graph.details_hidden||0}${S.graph.boundary_more?' · '+L('graphOverflow')+' '+S.graph.boundary_more:''}`;
        body.querySelectorAll('[data-da-action="graph-view"]').forEach(el=>el.setAttribute('aria-pressed',String(el.dataset.view===S.graphView)));
        $('da-graph-select').innerHTML = `<option value="">${esc(L('graphSelect'))}</option>` + S.graph.nodes.map(n => `<option value="${esc(n.id)}" ${S.graphNode?.id === n.id ? 'selected' : ''}>${esc((n.boundary?L('graphRefs')+' · ':'')+graphKind(n.kind) + ' · ' + n.title)}</option>`).join('');
        const task = S.id;
        window.DeepAssessmentGraph?.render($('da-graph-canvas'), S.graph, task + '/' + S.graphView + '/' + S.graphScope + '/' + S.graphOffset, node => { if (S.id === task) void selectGraphNode(node); }, graphKind, graphState);
        $('da-graph-pages').innerHTML = `${S.graphOffset ? button('graph-previous', L('previous')) : ''}${S.graph.has_more ? button('graph-next', L('next')) : ''}`;
        renderGraphNode();
    }
    const graphState = state => tr('graphStates.' + state, { observed: '直接观察', inferred: '推断待证', reported: '待独立复核', confirmed: '复核确认', refuted: '复核否定', inconclusive: '复核未定', unreviewed: '历史记录待核对', summary: '压缩摘要', note: '观察记录', user: '用户提供', folded: '已折叠', superseded:'已替代的归档' }[state] || status(state));
    function renderGraphNode() {
        const panel=$('da-graph-node');if(!panel)return;
        const n=S.graphNode;
        if(!window.AssessmentNodePanel){panel.textContent=n?n.title+'\n'+n.body:L('graphSelect');return;}
        const assets=window.AssessmentExplorationViews?.canAssets()?(n?.source?.asset_ids||[]):[];
        let links=assets.length?`<details class="da-node-links" data-da-detail="node-assets:${esc(n.id)}"><summary>${esc(L('linkedAssets'))} · ${assets.length}</summary><div class="da-node-asset-links">${assets.map(id=>button('intent-asset',id,`data-asset="${esc(id)}"`)).join('')}</div></details>`:'';
        if(n&&S.relations.length)links+=['upstream','downstream'].map(direction=>`<section class="da-node-relations"><h4>${esc(L(direction))}</h4>${S.relations.filter(r=>r.direction===direction).map(r=>button('graph-open',tr('graphRelations.'+r.relation,r.relation)+' · '+r.node.title,`data-node="${esc(r.node.id)}" data-kind="${esc(r.node.kind)}"`)).join('')}</section>`).join('')+(S.linksMore?button('graph-links-more',L('moreRelations')):'');
        const actions=n?`${n.kind==='fact'?button('provenance-path',window.AssessmentProvenance?.text('path')||'证据路径',`data-fact="${esc(n.id)}"`):''}${n.kind==='finding'?button('open-finding',L('openFinding'),`data-vulnerability="${esc(n.id.slice(8))}"`):''}${n.step_id?button('fact-proof',L('sourceStep'),`data-step="${esc(n.step_id)}"`):''}${(n.boundary||n.step_id||n.kind==='digest')?button('graph-expand',L('graphExpand'),`data-node="${esc(n.id)}"`):''}${S.nodeMore?button('graph-more',L('graphMore')):''}`:'';
        window.AssessmentNodePanel.render(panel,n?{id:n.id,key:S.id+'/'+n.id,title:n.title,kind:graphKind(n.kind),state:graphState(n.state),sections:window.AssessmentNodePanel.sections(n),source:n.source,contentHTML:links,actionsHTML:actions}:{emptyText:L('graphSelect')});
    }
    async function selectGraphNode(node, append = false, appendLinks=false) {
        const task = S.id, request = ++S.nodeRequest;
        try {
            const data = await json('/' + encodeURIComponent(task) + '/graph/node?' + new URLSearchParams({ node_id: node, offset: append ? S.nodeOffset : 0,links_offset:appendLinks?S.linksOffset:0 }));
            if (request !== S.nodeRequest || S.id !== task || S.tab !== 'graph') return;
            if (append && S.graphNode?.id === node) data.node.body = S.graphNode.body + data.node.body;
            data.node.boundary=S.graph?.nodes.find(n=>n.id===node)?.boundary||false;
            if(!appendLinks)S.graphNode = data.node; if(appendLinks)S.relations=S.relations.concat(data.relations||[]);else if(!append)S.relations=data.relations||[];if(!append){S.linksOffset=data.relations_next_offset||0;S.linksMore=!!data.relations_has_more;}showNodeDetails(); window.DeepAssessmentGraph?.select(node); if ($('da-graph-select')) $('da-graph-select').value = node; if(!appendLinks){S.nodeOffset = data.next_offset; S.nodeMore = data.has_more;}
            renderGraphNode();
        } catch (error) { if (request === S.nodeRequest && S.id === task) notice(error.message, true); }
    }
    async function mutate(action, values = {}, expectedStep=null) {
        if (S.busy || !S.task || !can('write')) return false;
        const taskId=S.id;
        S.busy = true; const epoch=++S.epoch; renderTask();
        const stillCurrent=()=>S.id===taskId&&S.epoch===epoch&&S.detailVisible;
        try {
            let revision=S.task.revision;
            if(expectedStep){
                const fresh=await json('/'+encodeURIComponent(taskId)+'/steps/'+encodeURIComponent(values.step_id));
                if(!stillCurrent())return false;
                if((fresh.step.attempts||0)!==expectedStep.attempts||(fresh.step.conversation_id||'')!==expectedStep.conversation)throw new Error(L('stepChanged'));
                revision=fresh.revision??revision;
                if(action==='reconcile'&&!['uncertain','cleanup_unconfirmed'].includes(fresh.step.status)){
                    if(['running','pausing'].includes(fresh.step.status))throw new Error(L('stepChanged'));
                    notice(L('reviewSaved'));return true;
                }
            }else if(action.startsWith('intent-')||action==='continue-stopped'){revision=(await json('/'+encodeURIComponent(taskId)+'?step_limit=1')).task.revision;}
            if(!stillCurrent())return false;
            const query=new URLSearchParams({step_limit:50,step_offset:S.stepOffset});
            if(S.tab==='plan'){query.set('step_order','queue');query.set('step_status',S.intentStatus);query.set('step_asset',S.intentAsset);}
            const data = await json('/' + encodeURIComponent(taskId) + '/control/' + action + '?'+query, { method: 'POST', body: JSON.stringify({ revision, ...values }) });
            if(!stillCurrent())return false;
            if(data.step&&S.focusStepId===data.step.id)S.focusedStep=data.step;
            S.intentCounts=data.intent_counts||S.intentCounts;
            S.stepOffset=data.step_offset??S.stepOffset;
            if(action==='reconcile'&&S.tab==='plan'&&S.intentStatus==='needs_review'){S.focusStepId='';S.focusedStep=null;writeRoute();}

            S.guidanceDelivered=!!data.guidance_delivered;S.task = data.task; S.stepTotal = data.step_total ?? data.task.steps?.length ?? 0; S.stepMore = !!data.step_has_more; notice(data.start_error || L(action==='reconcile'?'reviewSaved':'updated'), !!data.start_error);if(action==='continue-stopped'&&data.task.status==='waiting_input')$('da-guidance-panel').open=true; return !data.start_error;
        } catch (error) { if(stillCurrent())notice(error.message, true); return false; }
        finally { S.busy = false; renderTask(); void refresh(); }
    }
    function showForm(title, html) {
        $('da-form-title').textContent = title;
        $('da-form-content').innerHTML = html;
        $('da-form-notice').hidden = true;
        openDrawer('da-form-drawer');
    }
    const field = (name, label, value = '', type = 'text', extra = '') => `<label>${esc(label)}<input class="form-control" name="${name}" type="${type}" value="${esc(value)}" ${extra}></label>`;
    const choices = (name, label, items, empty) => `<label>${esc(label)}<select class="form-control" name="${name}"><option value="">${esc(empty)}</option>${items.map(item => `<option value="${esc(item.id ?? item)}">${esc(item.name ?? item)}</option>`).join('')}</select></label>`;
    async function create() {
        if (!can('write')) return;
        const epoch = ++S.drawerEpoch;
        const opts = await json('/options');
        if (epoch !== S.drawerEpoch || !$('page-deep-assessments')?.classList.contains('active')) return;
        showForm(L('create'), `<form id="da-create-form" class="da-drawer-form"><div class="da-form-body">${field('name', L('name'), '', 'text', 'maxlength="200" required')}<label>${esc(L('objective'))}<textarea class="form-control" name="objective" rows="3" maxlength="16000" required></textarea></label><label>${esc(L('scopeOptional'))}<textarea class="form-control" name="scope" rows="3" maxlength="16000" aria-describedby="da-scope-hint" placeholder="${esc(L('scopeExample'))}"></textarea></label><p id="da-scope-hint" class="da-muted da-field-hint">${esc(L('scopeHint'))}</p><label>${esc(L('criteriaOptional'))}<textarea class="form-control" name="criteria" rows="2" maxlength="8000" aria-describedby="da-criteria-hint" placeholder="${esc(L('criteriaExample'))}"></textarea></label><p id="da-criteria-hint" class="da-muted da-field-hint">${esc(window.AssessmentAcceptance?.text('criteriaHint')||L('criteriaHint'))}</p><div class="da-form-grid">${choices('model', L('model'), opts.models || [], L('default'))}${choices('role', L('role'), opts.roles || [], L('default'))}${choices('project_id', L('project'), opts.projects || [], L('none'))}${field('concurrency', L('concurrency'), 2, 'number', 'min="1" max="4" required')}</div><label class="da-check"><input type="checkbox" name="auto_resume" checked>${esc(L('autoResume'))}</label><p class="da-muted">${esc(L('recoveryNote'))}</p></div><footer class="da-drawer-footer"><button type="submit" class="btn-primary">${esc(L('save'))}</button>${button('close', L('close'))}</footer></form>`);
    }
    async function saveCreate(form) {
        if (S.busy || !can('write')) return;
        const values = Object.fromEntries(new FormData(form));
        values.concurrency = Number(values.concurrency); values.auto_resume = form.elements.auto_resume.checked;
        S.busy = true; ++S.epoch; form.querySelector('button[type="submit"]').disabled = true;
        try { const data = await json('', { method: 'POST', body: JSON.stringify(values) }); S.id = data.task.id; S.readerExpanded=false; S.detailsOpen=false; S.intentAsset=''; S.extra=null; window.AssessmentExplorationViews?.reset(S.id); S.task = data.task; S.focusStepId = ''; S.focusedStep = null; S.originFindingId = ''; S.originFinding = null; S.findingOffset = 0; S.runtime=null;S.stepDrafts=Object.create(null);S.graphView='summary';S.tab = 'overview'; S.stepOffset = 0; S.stepMore = false; S.stepTotal = 0; S.graph = null; S.graphScope = ''; S.graphOffset = 0; S.graphNode = null; S.events = []; S.receipts = []; S.receiptOffset = 0; closeDrawer('da-form-drawer');writeRoute(true);if ($('page-deep-assessments')?.classList.contains('active')) showWorkspace(); notice(L('saved')); }
        catch (error) { notice(error.message, true); }
        finally { S.busy = false; form.querySelector('button[type="submit"]').disabled = false; await refresh(); }
    }
    async function guidance() { const value = $('da-guidance').value.trim(); if (!value) return; if (await mutate(S.task.question ? 'answer' : 'guidance', { message: value })) { if ($('da-guidance').value.trim() === value) $('da-guidance').value = ''; } }
    async function action(name, data = {}) {
        if (S.busy) return;
        try {
            if(['provenance-path','provenance-endpoints','provenance-history'].includes(name)){
                if(S.tab==='acceptance'&&data.criterion)S.acceptanceFocus=Number(data.criterion);
                writeRoute();
                S.provenance={asset:data.asset||'',endpoint:data.endpoint||'',fact:data.fact||'',criterion:Number(data.criterion)||0,status:data.status||'',offset:0,node:''};
                S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset='';S.intentStatus='';
                S.tab=({'provenance-path':'evidence-path','provenance-endpoints':'endpoint-review','provenance-history':'asset-history'})[name];
                S.readerExpanded=false;S.detailsOpen=S.tab==='evidence-path';
                $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;
                writeRoute(true);return await refresh();
            }
            if(name==='provenance-page'||name==='provenance-filter'){
                const step=S.tab==='asset-history'?10:S.tab==='evidence-path'?12:20;
                const offset=name==='provenance-page'?Math.max(0,S.provenance.offset+step*Number(data.direction)):0;
                S.provenance={...S.provenance,offset,node:'',status:name==='provenance-filter'?data.status:S.provenance.status};
                writeRoute();return await refresh();
            }
            if(name==='provenance-toggle'){
                if(!can('write')||S.tab!=='endpoint-review')return;
                const id=S.id, key=provenanceKey();
                await json('/'+encodeURIComponent(id)+'/endpoint-evidence',{method:'PUT',body:JSON.stringify({
                    asset_id:S.provenance.asset,endpoint_id:S.provenance.endpoint,fact_id:data.fact,linked:data.linked==='true'
                })});
                if(key===provenanceKey()&&S.detailVisible){notice(window.AssessmentProvenance.text('saved'));return await refresh();}
                return;
            }
            if(name==='acceptance-filter'){
                window.AssessmentAcceptance.setFilter(data.status);
                S.acceptanceFocus=0;S.renderSignature='';writeRoute();renderTask();
                $('da-body').querySelector('[data-status="'+CSS.escape(data.status)+'"]')?.focus({preventScroll:true});
                return;
            }
            if(name==='acceptance-edit'){
                const row=S.acceptance?.id===S.id && S.acceptance.rows.find(item=>String(item.number)===data.number);
                if(!row||!can('write'))return;
                S.acceptanceFocus=row.number;writeRoute();
                window.AssessmentAcceptance.editor(row,S.id,json,showForm,()=>closeDrawer('da-form-drawer'),id=>{
                    if(S.id!==id || S.tab!=='acceptance' || !S.detailVisible)return;
                    notice(window.AssessmentAcceptance.text('saved'));
                    void refresh();
                });
                return;
            }
            if(name==='acceptance-receipt'){
                S.acceptanceFocus=Number(data.criterion);writeRoute();
                S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset='';S.intentStatus='';
                S.tab='evidence';S.focusReceiptId=data.node;S.receiptOffset=0;S.readerExpanded=false;S.detailsOpen=false;
                $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;
                writeRoute(true);return await refresh();
            }
            if(name==='clear-receipt'){S.focusReceiptId='';S.receiptOffset=0;writeRoute();return await refresh();}
            if (await window.AssessmentExplorationViews?.action(name,data,json)) { S.renderSignature=''; if(['asset-linked','asset-all','asset-browse','asset-search','asset-clear','asset-observation'].includes(name)){S.intentAsset='';S.detailsOpen=false;S.readerExpanded=false;writeRoute();} return await refresh(); }
            if (name === 'intent-asset') { S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.readerExpanded=false;S.intentAsset=data.asset; S.detailsOpen=true; window.AssessmentExplorationViews.focusAsset(data.asset); S.tab='assets'; $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0; writeRoute(); return await refresh(); }
            if (name === 'asset-facts' || name === 'clear-fact-asset') {
                S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;
                S.intentAsset=name==='asset-facts'?data.asset:'';
                S.readerExpanded=false;S.detailsOpen=false;S.tab='facts';
                window.AssessmentExplorationViews.resetFacts();
                $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;
                writeRoute(true);return await refresh();
            }
            if (name === 'asset-intents') { S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset=data.asset;S.intentStatus='';S.stepOffset=0;S.tab='plan';writeRoute();return await refresh(); }
            if (name === 'clear-intent-asset') { S.intentAsset='';S.stepOffset=0;return await refresh(); }
            if (name === 'fact-proof') { S.focusReceiptId=''; S.focusStepId=data.step; S.originFindingId=''; S.originFinding=null; S.focusedStep=null; S.tab='evidence'; S.receiptOffset=0; $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0; writeRoute(); return await refresh(); }
            if (name === 'close') return closeDrawer('da-form-drawer');
            if (name === 'back-list') return returnToList();
            if (name === 'create') return await create();
            if (name === 'refresh') return await refresh();
            if(name==='blocked-reason'){if(S.task?.status==='blocked'){$('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;$('da-task-blocker').focus({preventScroll:true});}return;}
            if(name==='live-latest'){$('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;S.renderSignature='';renderTask();return;}
            if(name==='show-step'){S.focusStepId=data.step;S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.tab='plan';S.stepOffset=0;$('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;writeRoute(true);return await refresh();}
            if(name==='overview-tab'||name==='queue-status'){
                const tab=name==='queue-status'?'plan':data.tab;
                if(!['plan','quality','activity','facts','findings'].includes(tab)||tab==='findings'&&!canFindings())return;
                S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset='';S.intentStatus=name==='queue-status'?data.status||'':'';S.stepOffset=0;S.receiptOffset=0;S.findingOffset=0;
                if(tab==='facts')window.AssessmentExplorationViews?.resetFacts();
                if(tab==='activity')S.events=[];
                S.tab=tab;S.readerExpanded=false;S.detailsOpen=false;
                $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;
                writeRoute(true);return await refresh();
            }
            if(['intent-pause','intent-resume','intent-priority','intent-guidance'].includes(name)){const values={step_id:data.step};if(name==='intent-priority')values.priority=Number(data.priority);if(name==='intent-guidance')values.message=data.message;const ok=await mutate(name,values);if(ok&&name==='intent-guidance'){if(S.stepDrafts[data.step]===data.message)delete S.stepDrafts[data.step];S.renderSignature='';renderTask();notice(L(S.guidanceDelivered?'guidanceDelivered':'guidanceQueued'));}return;}
            if(name==='graph-view'){S.routeGraphNode='';S.graphView=data.view==='full'?'full':'summary';S.graphOffset=0;S.graphNode=null;S.relations=[];hideNodeDetails();S.renderSignature='';writeRoute();return await refresh();}
            if(name==='graph-open'){
                if(S.tab!=='graph'){
                    if(S.tab==='acceptance'&&data.criterion){S.acceptanceFocus=Number(data.criterion);writeRoute();}
                    S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset='';S.intentStatus='';
                    S.tab='graph';
                    $('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;
                }
                S.graphScope=data.node;S.graphOffset=0;
                if(data.criterion||['receipt','observation'].includes(data.kind))S.graphView='full';
                S.routeGraphNode=data.node;
                S.graphNode=null;S.relations=[];hideNodeDetails();writeRoute(true);
                return await refresh();
            }
            if(name==='graph-links-more'&&S.graphNode)return await selectGraphNode(S.graphNode.id,false,true);
            if (name === 'previous' || name === 'next') { S.page += name === 'previous' ? -1 : 1; S.page = Math.max(0, S.page); writeListRoute();return await refresh(); }
            if (name === 'select' || name === 'details' || name === 'review') { data={...data,id:data.id||S.id}; ++S.drawerEpoch; S.focusStepId = ''; S.focusedStep = null; S.originFindingId = ''; S.originFinding = null; S.findingOffset = 0; if (S.id !== data.id) { S.id = data.id; S.readerExpanded=false; S.detailsOpen=false; S.extra=null; S.intentAsset=''; S.intentStatus=''; window.AssessmentExplorationViews?.reset(S.id); S.task = null; S.runtime=null;S.stepDrafts=Object.create(null);S.graphView='summary';S.tab = 'overview'; S.stepOffset = 0; S.stepMore = false; S.stepTotal = 0; S.graph = null; S.graphScope = ''; S.graphOffset = 0; S.graphNode = null; ++S.nodeRequest; S.events = []; S.receipts = []; S.receiptOffset = 0; $('da-guidance').value = ''; $('da-workspace').querySelector('.da-detail-scroll').scrollTop = 0; } if(name==='review'){S.tab='plan';S.intentStatus='needs_review';S.intentAsset='';S.stepOffset=0;S.readerExpanded=false;S.detailsOpen=false;$('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;} writeListRoute();writeRoute(true);showWorkspace();return await refresh(); }
            if (name === 'tab') { if(S.tab!==data.tab){S.readerExpanded=false;S.detailsOpen=false;S.focusReceiptId='';S.routeGraphNode='';if(data.tab==='acceptance'){S.acceptanceFocus=0;S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset='';S.intentStatus='';}if(data.tab==='facts'){S.intentAsset='';window.AssessmentExplorationViews?.resetFacts();}}S.tab = data.tab; writeRoute(); $('da-workspace').querySelector('.da-detail-scroll').scrollTop = 0; return await refresh(); }
            if (name === 'open-finding' || name === 'all-findings') { closeDrawer('da-form-drawer'); hideWorkspace(); window.AssessmentFindingLinks?.openFinding(name === 'open-finding' ? data.vulnerability : '', S.id); return; }
            if (name === 'finding-evidence') { S.focusReceiptId=''; S.originFindingId = data.vulnerability; S.focusStepId = data.step; S.focusedStep = null; S.originFinding = null; S.tab = 'evidence'; S.receiptOffset = 0; writeRoute(); return await refresh(); }
            if (name === 'clear-source') { S.focusReceiptId=''; S.focusStepId = ''; S.focusedStep = null; S.originFindingId = ''; S.originFinding = null; S.receiptOffset = 0; writeRoute(); return await refresh(); }
            if (name === 'next-findings' || name === 'previous-findings') { S.findingOffset = Math.max(0, S.findingOffset + (name === 'next-findings' ? 20 : -20)); return await refresh(); }
            if (name === 'step-next' || name === 'step-previous') { S.stepOffset = Math.max(0, S.stepOffset + (name === 'step-next' ? 50 : -50)); return await refresh(); }
            if (name === 'node-close') { hideNodeDetails();return; }
            if (name === 'graph-reader') { S.readerExpanded=!S.readerExpanded;S.detailsOpen=true;syncDetailPanels();$('da-workspace').querySelector('.da-detail-scroll').scrollTop=0;return; }
            if (name === 'graph-details') { S.detailsOpen=!S.detailsOpen;if(!S.detailsOpen)S.readerExpanded=false;syncDetailPanels();return; }
            if (name === 'graph-layout') return window.DeepAssessmentGraph?.relayout();
            if (name === 'graph-fit') return window.DeepAssessmentGraph?.fit();
            if (name === 'graph-in') return window.DeepAssessmentGraph?.zoomIn();
            if (name === 'graph-out') return window.DeepAssessmentGraph?.zoomOut();
            if (name === 'graph-home' || name === 'graph-expand') { S.routeGraphNode=''; S.graphScope = name === 'graph-home' ? '' : data.node;if(name==='graph-expand')S.graphView='full';writeRoute(true); S.graphOffset = 0; S.graphNode = null; S.detailsOpen=false;S.readerExpanded=false;++S.nodeRequest; return await refresh(); }
            if (name === 'graph-next' || name === 'graph-previous') { S.graphOffset = Math.max(0, S.graphOffset + (name === 'graph-next' ? 80 : -80)); return await refresh(); }
            if (name === 'graph-more' && S.graphNode) return await selectGraphNode(S.graphNode.id, true);
            if (name.startsWith('row-')) return await rowControl(data.id,name.slice(4));
            if (['start','pause','archive','restore','continue-stopped'].includes(name)) return await mutate(name);
            if (name === 'cancel') { if (window.confirm(L('confirmStop'))) return await mutate('cancel'); return; }
            if (name === 'conversation') { closeDrawer('da-form-drawer'); hideWorkspace(); if (typeof window.switchPage === 'function') window.switchPage('chat'); if (typeof window.loadConversation === 'function') await window.loadConversation(data.conversation); return; }

            if (name === 'retry'||name==='reconcile') { const step = (S.focusedStep?.id === data.step ? S.focusedStep : S.task.steps.find(s => s.id === data.step));if(!step)return; const uncertain = ['uncertain', 'cleanup_unconfirmed'].includes(step.status);const expected={attempts:step.attempts||0,conversation:step.conversation_id||''}; if (uncertain && !window.confirm(L('unknown') + '\n\n' + (terminal(S.task)?L('closedReviewHint'):L('reconcile')))) return; return await mutate(uncertain?'reconcile':'retry', { step_id: data.step, reconciled: uncertain, cleanup_confirmed: uncertain },expected); }
            if (name === 'report') { const response = await api('/' + encodeURIComponent(S.id) + '/report'); const url = URL.createObjectURL(await response.blob()); const a = document.createElement('a'); a.href = url; a.download = 'assessment-' + S.id + '.md'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); return; }
            if (name === 'delete' && can('delete') && window.confirm(L('confirmDelete'))) { S.busy = true; ++S.epoch; try { await api('/' + encodeURIComponent(S.id), { method: 'DELETE' }); S.id = ''; S.task = null;writeListRoute(true);hideWorkspace(); } finally { S.busy = false; } return await refresh(); }
            if (name === 'more-receipts' || name === 'previous-receipts') { S.receiptOffset = Math.max(0, S.receiptOffset + (name === 'more-receipts' ? 50 : -50)); return await refresh(); }
        } catch (error) { notice(error.message, true); }
    }
    function afterAuthentication() {
        mount();
        if (!S.mounted || !can('read') || !$('page-deep-assessments')?.classList.contains('active')) return;
        const header = $('deep-assessments-root').querySelector('.da-header > div:last-child');
        const create = header?.querySelector('[data-da-action="create"]');
        if (can('write') && !create) header?.insertAdjacentHTML('beforeend', button('create',L('create')));
        else if (!can('write')) create?.remove();
        applyRoute();
        void refresh();
    }
    window.DeepAssessments = { afterAuthentication, showNodeDetails, hideNodeDetails, syncDetailPanels, separator, init() { mount(); applyRoute(); void refresh(); if (!S.timer) S.timer = setInterval(() => { if ($('page-deep-assessments')?.classList.contains('active') && !document.hidden) void refresh(); }, 4000); }, fromLocation() { mount(); if (applyRoute()) void refresh(); }, refresh };
})();
