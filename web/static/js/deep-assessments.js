/* Independent assessment workspace. Batch task routes and controllers are untouched. */
(function () {
    'use strict';
    const S = { extra: null, intentAsset: '', intentStatus: '', intentCounts: {}, routeKey: '', focusStepId: '', focusedStep: null, originFindingId: '', originFinding: null, findings: [], findingOffset: 0, findingMore: false, findingTotal: 0, id: '', task: null, tab: 'graph', graph: null, graphScope: '', graphOffset: 0, graphNode: null, nodeOffset: 0, nodeMore: false, nodeRequest: 0, stepOffset: 0, stepTotal: 0, stepMore: false, page: 0, more: false, busy: false, epoch: 0, timer: null, mounted: false, drawerEpoch: 0, events: [], receipts: [], receiptOffset: 0, receiptMore: false };
    const labels = { assets: '资产图', facts: '事实', priority: '优先级', linkedAssets: '目标资产', intentSources: '来源节点', findings: '关联漏洞', noFindings: '暂未登记漏洞。', openFinding: '在漏洞管理查看', allFindings: '在漏洞管理查看本次测评', reproduction: '复现步骤', findingEvidence: '复现证据', sourceStep: '来源步骤', allSteps: '显示全部步骤与回执', sourceFinding: '关联漏洞', graph: '探索图', continuous: '持续执行', mode: '运行模式', elapsed: '已运行', graphHint: '摘要用于续接；原始观察和执行回执可随时展开。', graphVisible: '当前页节点', graphFolded: '已折叠节点', graphHome: '全图概览', graphExpand: '展开关联节点', graphFit: '适应画布', graphSelect: '点击节点查看原始内容与来源。', graphMore: '继续读取原文', status: '状态', updatedAt: '更新时间', loading: '正在读取测评…', contextUsage:'上下文 / 窗口', toolDefinitions:'工具定义', title: '深度测评', subtitle: '围绕一个目标持续探索，保存证据与进度，跨会话继续执行。', create: '新建测评', refresh: '刷新', search: '搜索测评名称', all: '全部状态', empty: '暂无测评，先创建一个目标。', select: '选择测评，查看探索进度与证据。', previous: '上一页', next: '下一页', name: '测评名称', objective: '目标', scope: '允许范围与排除项', criteria: '验收条件', model: '模型渠道', project: '关联项目', role: '角色', default: '系统默认', none: '不关联', tokens: '累计 Token', rounds: '规划轮次', concurrency: '并行步骤', autoResume: '服务重启后自动恢复结果明确的步骤', save: '保存草稿', close: '关闭', start: '开始 / 继续', pause: '暂停', cancel: '停止', delete: '删除', report: '下载报告', overview: '概览', plan: '意图队列', evidence: '证据回执', activity: '运行记录', quality: '上下文与用量', guidance: '补充线索或调整方向', send: '提交方向', answer: '答复并继续', retry: '恢复此步骤', conversation: '查看执行会话', unknown: '请先核对回执，确认上次操作结果及执行资源状态。', reconcile: '我已核对上次调用结果，允许从原会话继续', confirmStop: '停止后将保留已有结果，结束本次测评。', confirmDelete: '删除本条测评？关联执行会话仍保留。', strategy: '当前路线', summary: '当前结论 / 缺口', question: '待补充信息', dependencies: '前置步骤', segments: '执行分段', evidenceIDs: '证据引用', noSteps: '开始后由规划器逐步生成探索方向。', noRecords: '暂无记录', calls: '工具调用', compactions: '上下文压缩', activeTools: '当前可见工具', session: '会话', tokenNote: '累计用量包含 Pi 压缩，不作为任务自动结束条件。', recoveryNote: '结果不明的调用会暂停等待核对，已完成步骤不会自动重做。', saved: '已保存', updated: '已更新', retryNote: '恢复会保留原 Pi 会话及历史证据。' };
    const states = { draft: '草稿', running: '执行中', pending: '待执行', completed: '已完成', blocked: '受阻', failed: '失败', paused: '已暂停', pausing: '暂停中', cancelling: '停止中', cancelled: '已停止', recovering: '待恢复', waiting_retry: '等待模型恢复', waiting_input: '待答复', needs_review: '待核对', uncertain: '结果不明', waiting_progress: '等待新进展', interrupted: '已中断', cleanup_unconfirmed: '待确认资源停止', retired: '已撤下', reconciled: '已核对', yielded: '分段完成' };
    const tr = (key, fallback) => { const v = typeof window.t === 'function' ? window.t('deepAssessments.' + key) : ''; return v && v !== 'deepAssessments.' + key ? v : fallback; };
    const L = key => tr(key, labels[key] || key);
    const status = key => tr('states.' + key, states[key] || key);
    const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
    const $ = id => document.getElementById(id);
    const active = t => ['running', 'pausing', 'cancelling'].includes(t?.status);
    const terminal = t => ['completed', 'cancelled'].includes(t?.status);
    const canFindings = () => typeof window.hasPermission !== 'function' || window.hasPermission('vulnerability:read');
    const can = action => typeof window.hasPermission !== 'function' || window.hasPermission('assessments:' + action);
    function notice(message, error = false) {
        ['da-notice', 'da-detail-notice', 'da-form-notice'].forEach(id => {
            const el = $(id);
            if (el) { el.textContent = message; el.classList.toggle('is-error', error); el.hidden = !message; }
        });
    }
    function syncDrawerLock() {
        document.body.classList.toggle('da-drawer-open', !!$('da-inspector')?.open || !!$('da-form-drawer')?.open);
    }
    function openDrawer(id) {
        const drawer = $(id);
        if (!drawer.open) {
            drawer.returnFocus = document.activeElement;
            drawer.showModal();
        }
        syncDrawerLock();
        const target = id === 'da-form-drawer' ? (drawer.querySelector('input:not([type="checkbox"]), textarea') || drawer.querySelector('button')) : drawer.querySelector('[data-da-action="close-detail"]');
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
            syncDrawerLock();
            requestAnimationFrame(() => {
                if (!$('page-deep-assessments')?.classList.contains('active')) return;
                const top = $('da-form-drawer').open ? $('da-form-drawer') : $('da-inspector').open ? $('da-inspector') : null;
                const prior = drawer.returnFocus;
                if (prior?.isConnected && !prior.disabled && (!top || top.contains(prior))) prior.focus({ preventScroll: true });
                else if (top) top.querySelector('button')?.focus({ preventScroll: true });
                else ([...document.querySelectorAll('.da-list-item')].find(row => row.dataset.id === S.id) || document.querySelector('[data-da-action="create"]'))?.focus({ preventScroll: true });
            });
        });
    }
    async function api(path, options = {}) {
        const fetcher = typeof window.apiFetch === 'function' ? window.apiFetch : fetch;
        const response = await fetcher('/api/deep-assessments' + path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } });
        if (!response.ok) { let value; try { value = await response.json(); } catch {} throw new Error(value?.error || 'HTTP ' + response.status); }
        return response;
    }
    async function json(path, options) { return (await api(path, options)).json(); }
    const button = (action, label, extra = '') => `<button type="button" class="btn-secondary" data-da-action="${action}" ${extra}>${esc(label)}</button>`;
    const badge = value => `<span class="da-badge da-${esc(value)}">${esc(status(value))}</span>`;
    function writeRoute() {
        if (!S.id) return;
        const params = new URLSearchParams({ assessment_id: S.id, tab: S.tab });
        if (S.intentAsset) params.set('asset_id', S.intentAsset);
        if (S.focusStepId) params.set('step_id', S.focusStepId);
        if (S.originFindingId) params.set('vulnerability_id', S.originFindingId);
        history.replaceState(null, '', '#deep-assessments?' + params);
        S.routeKey = window.location.hash;
    }
    function applyRoute() {
        const [page, ...query] = window.location.hash.slice(1).split('?');
        if (page !== 'deep-assessments' || !$('page-deep-assessments')?.classList.contains('active') || (typeof window.hasPermission === 'function' && !window.hasPermission('assessments:read'))) return false;
        const params = new URLSearchParams(query.join('?')), id = params.get('assessment_id');
        if (!id) return false;
        if (S.routeKey === window.location.hash && $('da-inspector').open) return false;
        const changed = S.id !== id;
        S.id = id; window.AssessmentExplorationViews?.reset(id); if (changed) { S.task = null; $('da-guidance').value = ''; }
        S.intentAsset = params.get('asset_id') || ''; S.focusStepId = params.get('step_id') || ''; S.originFindingId = params.get('vulnerability_id') || '';
        S.focusedStep = null; S.originFinding = null; S.findings = []; S.findingOffset = 0; S.receiptOffset = 0; S.stepOffset = 0;
        S.tab = ['overview','graph','plan','evidence','activity','quality','findings','assets','facts'].includes(params.get('tab')) ? params.get('tab') : (S.focusStepId ? 'plan' : 'graph');
        if(S.tab==='assets' && S.intentAsset)window.AssessmentExplorationViews?.focusAsset(S.intentAsset);
        S.graph = null; S.graphScope = ''; S.graphNode = null; S.graphOffset = 0; S.events = []; S.receipts = [];
        S.routeKey = window.location.hash; ++S.epoch;
        renderTask(); openDrawer('da-inspector'); return true;
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
            <div class="page-header da-header"><div><h2>${esc(L('title'))}</h2><p>${esc(L('subtitle'))}</p></div><div>${button('refresh', L('refresh'))}${can('write') ? button('create', L('create')) : ''}</div></div>
            <div id="da-notice" class="da-notice" role="status" hidden></div>
            <section class="da-list-panel">
                <div class="da-filters"><input id="da-search" class="form-control" aria-label="${esc(L('search'))}" placeholder="${esc(L('search'))}"><select id="da-filter" class="form-control" aria-label="${esc(L('all'))}"><option value="">${esc(L('all'))}</option>${Object.keys(states).filter(s => !['pending', 'retired', 'reconciled', 'yielded'].includes(s)).map(s => `<option value="${s}">${esc(status(s))}</option>`).join('')}</select></div>
                <div class="da-list-head" aria-hidden="true"><span>${esc(L('name'))}</span><span>${esc(L('status'))}</span><span>${esc(L('rounds'))}</span><span>${esc(L('tokens'))}</span><span>${esc(L('updatedAt'))}</span></div>
                <div id="da-list"></div>
                <div class="da-pagination">${button('previous', L('previous'))}<span id="da-page"></span>${button('next', L('next'))}</div>
            </section>
            <dialog id="da-inspector" class="da-drawer da-inspector" aria-labelledby="da-title">
                <header class="da-drawer-header"><div id="da-title"></div><button type="button" class="da-drawer-close" data-da-action="close-detail" aria-label="${esc(L('close'))}">&times;</button></header>
                <div class="da-detail-scroll"><div id="da-detail-notice" class="da-notice" role="status" hidden></div><div id="da-controls" class="da-controls"></div><div id="da-tabs" class="da-tabs" role="tablist"></div><div id="da-body"></div><form id="da-guidance-form" hidden><label for="da-guidance">${esc(L('guidance'))}</label><textarea id="da-guidance" class="form-control" rows="3" maxlength="8000"></textarea><button class="btn-primary" id="da-guidance-submit" type="submit">${esc(L('send'))}</button></form></div>
            </dialog>
            <dialog id="da-form-drawer" class="da-drawer da-form-drawer" aria-labelledby="da-form-title">
                <header class="da-drawer-header"><h3 id="da-form-title"></h3><button type="button" class="da-drawer-close" data-da-action="close" aria-label="${esc(L('close'))}">&times;</button></header>
                <div id="da-form-notice" class="da-notice" role="alert" hidden></div><div id="da-form-content"></div>
            </dialog>`;
        bindDrawer('da-inspector');
        bindDrawer('da-form-drawer');
        window.addEventListener('hashchange', () => {
            if (window.location.hash.slice(1).split('?')[0] !== 'deep-assessments') { closeDrawer('da-form-drawer'); closeDrawer('da-inspector'); } else if (applyRoute()) void refresh();
        });
        root.addEventListener('click', event => { const target = event.target.closest('[data-da-action]'); if (target) void action(target.dataset.daAction, target.dataset); });
        root.addEventListener('submit', event => { event.preventDefault(); if (event.target.id === 'da-asset-search') void action('asset-search'); if (event.target.id === 'da-create-form') void saveCreate(event.target); if (event.target.id === 'da-guidance-form') void guidance();  });
        root.addEventListener('change', event => { if (event.target.id === 'da-intent-status') { S.intentStatus=event.target.value; S.stepOffset=0; void refresh(); } if (event.target.id === 'da-graph-select' && event.target.value) void selectGraphNode(event.target.value); });
        let searchTimer;
        $('da-search').addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { S.page = 0; void refresh(); }, 250); });
        $('da-filter').addEventListener('change', () => { S.page = 0; void refresh(); });
    }
    function renderTask() {
        const task = S.task;
        if (!task) { $('da-title').textContent = L('title'); $('da-controls').innerHTML = ''; $('da-tabs').innerHTML = ''; $('da-body').textContent = L(S.id ? 'loading' : 'select'); $('da-guidance-form').hidden = true; return; }
        $('da-title').innerHTML = `<h3>${esc(task.name)} ${badge(task.status)}</h3>`;
        $('da-controls').innerHTML = `${can('write') && !terminal(task) ? (!active(task) ? button('start', L('start'), task.question || task.status === 'needs_review' ? 'disabled' : '') : button('pause', L('pause'), task.status !== 'running' ? 'disabled' : '')) + button('cancel', L('cancel')) : ''}${button('report', L('report'))}${can('delete') && !active(task) ? button('delete', L('delete')) : ''}`;
        $('da-tabs').innerHTML = ['overview', ...(window.AssessmentExplorationViews?.canAssets() ? ['assets'] : []), 'graph', 'plan', 'facts', ...(canFindings() ? ['findings'] : []), 'evidence', 'activity', 'quality'].map(tab => `<button type="button" role="tab" aria-selected="${S.tab === tab}" data-da-action="tab" data-tab="${tab}" class="${S.tab === tab ? 'active' : ''}">${esc(L(tab))}</button>`).join('');
        const body = $('da-body'), scroller = $('da-inspector').querySelector('.da-detail-scroll'), scroll = scroller.scrollTop;
        const opened = new Set([...body.querySelectorAll('details[open]')].map(el => el.dataset.step));
        if (!['graph','assets'].includes(S.tab)) window.DeepAssessmentGraph?.destroy();
        if (S.tab === 'overview') {
            const seconds = (task.active_seconds || 0) + (active(task) && task.run_started_at ? Math.max(0, (Date.now() - Date.parse(task.run_started_at)) / 1000) : 0);
            body.innerHTML = `<div class="da-stats"><div><small>${esc(L('mode'))}</small><strong>${esc(L('continuous'))}</strong><small>${esc(L('elapsed'))} ${(seconds / 3600).toFixed(1)} h</small></div><div><small>${esc(L('tokens'))}</small><strong>${Number(task.tokens_used || 0).toLocaleString()}</strong></div><div><small>${esc(L('rounds'))}</small><strong>${task.rounds || 0}</strong></div></div>${['objective', 'scope', 'criteria', 'strategy', 'summary', 'question'].filter(k => task[k]).map(k => `<section class="da-section"><h4>${esc(L(k))}</h4><pre>${esc(task[k])}</pre></section>`).join('')}<p class="da-muted">${esc(L('recoveryNote'))}</p>`;
        } else if (S.tab === 'graph') {
            renderGraph(body);
        } else if (S.tab === 'assets' || S.tab === 'facts') {
            window.AssessmentExplorationViews?.render(body, S.tab, S.extra, can('write'));
        } else if (S.tab === 'plan') {
            body.innerHTML = sourceBanner() + (S.focusedStep ? [S.focusedStep] : task.steps || []).map(step => `<details class="da-step" data-step="${esc(step.id)}" ${opened.has(step.id) || step.id === S.focusStepId ? 'open' : ''}><summary>${badge(step.status)} <strong>${esc(step.title)}</strong><small>${esc(step.id)}</small></summary><pre>${esc(step.instruction)}</pre><p>${esc(L('priority'))}: ${step.priority || 100} · ${esc(L('linkedAssets'))}: ${(step.asset_ids || []).map(id=>window.AssessmentExplorationViews?.canAssets()?button('intent-asset',id,`data-asset="${esc(id)}"`):esc(id)).join(' ') || '—'}</p><p>${esc(L('intentSources'))}: ${esc((step.source_ids || []).join(', ') || '—')}</p><p>${esc(L('dependencies'))}: ${esc((step.depends_on || []).join(', ') || '—')} · ${esc(L('segments'))}: ${step.attempts || 0}</p><pre>${esc(step.summary)}</pre><p>${esc(L('evidenceIDs'))}: ${esc((step.evidence || []).join(', ') || '—')}</p><div class="da-controls">${step.conversation_id ? button('conversation', L('conversation'), `data-conversation="${esc(step.conversation_id)}"`) : ''}${!active(task) && can('write') && ['failed', 'blocked', 'interrupted', 'uncertain', 'cleanup_unconfirmed'].includes(step.status) ? button('retry', L('retry'), `data-step="${esc(step.id)}"`) : ''}</div></details>`).join('') || `<p>${esc(L('noSteps'))}</p>`;
            if (!S.focusStepId) body.insertAdjacentHTML('afterbegin', `<div class="da-controls">${S.intentAsset?button('clear-intent-asset',L('allSteps')):''}<select id="da-intent-status" class="form-control" aria-label="${esc(L('status'))}"><option value="">${esc(L('all'))}</option>${['pending','running','completed','blocked','failed','retired'].map(k=>`<option value="${k}" ${S.intentStatus===k?'selected':''}>${esc(status(k))} (${S.intentCounts[k]||0})</option>`).join('')}</select></div>`);
            if (!S.focusStepId) body.innerHTML += `<div class="da-controls">${S.stepOffset ? button('step-previous', L('previous')) : ''}<span>${(task.steps || []).length} / ${S.stepTotal}</span>${S.stepMore ? button('step-next', L('next')) : ''}</div>`;
        } else if (S.tab === 'findings') {
            body.innerHTML = `<div class="da-controls">${button('all-findings', L('allFindings'))}<span>${S.findingTotal}</span></div>` + (S.findings.map(v => `<article class="da-finding"><h4>${esc(v.title)}</h4><p>${esc(v.severity)} · ${esc(v.status)}</p><p>${esc(L('sourceStep'))}：${esc(v.assessment_step_title || v.assessment_step_id)}</p><div class="da-controls">${button('open-finding', L('openFinding'), `data-vulnerability="${esc(v.id)}"`)}${button('finding-evidence', L('findingEvidence'), `data-vulnerability="${esc(v.id)}" data-step="${esc(v.assessment_step_id)}"`)}</div></article>`).join('') || `<p>${esc(L('noFindings'))}</p>`) + `<div class="da-controls">${S.findingOffset ? button('previous-findings', L('previous')) : ''}${S.findingMore ? button('next-findings', L('next')) : ''}</div>`;
        } else if (S.tab === 'evidence') {
            body.innerHTML = sourceBanner(true) + S.receipts.map(row => `<details class="da-step" data-step="${esc(row.id)}" ${opened.has(row.id) ? 'open' : ''}><summary>${badge(row.status)} ${esc(row.tool)} · ${esc(row.step_id)}</summary><code>${esc(row.execution_id || row.id)}</code><pre>${esc(row.arguments)}</pre><pre>${esc(row.result)}</pre></details>`).join('') || `<p>${esc(L('noRecords'))}</p>`;
            body.innerHTML += `<div class="da-controls">${S.receiptOffset > 0 ? button('previous-receipts', L('previous')) : ''}${S.receiptMore ? button('more-receipts', L('next')) : ''}</div>`;
        } else if (S.tab === 'activity') {
            body.innerHTML = S.events.slice().reverse().map(row => `<div class="da-event"><small>${esc(new Date(row.created_at).toLocaleString())} · ${esc(row.step_id)} · ${esc(row.kind)}</small><pre>${esc(row.message)}</pre></div>`).join('') || `<p>${esc(L('noRecords'))}</p>`;
        } else {
            body.innerHTML = `<p class="da-muted">${esc(L('tokenNote'))}</p><div class="da-table-wrap"><table><thead><tr><th>${esc(L('session'))}</th><th>Token</th><th>${esc(L('compactions'))}</th><th>${esc(L('activeTools'))}</th><th>${esc(L('contextUsage'))}</th><th>${esc(L('toolDefinitions'))}</th></tr></thead><tbody>${Object.entries(task.metrics || {}).map(([id, m]) => `<tr><td>${esc(m.step_id)}<br><small>${esc(id.slice(0, 12))}</small></td><td>${Number(m.tokens || 0).toLocaleString()}</td><td>${m.compactions || 0}</td><td>${m.tool_count || 0}</td><td>${m.context_tokens == null ? '—' : Number(m.context_tokens).toLocaleString()} / ${Number(m.context_window || 0).toLocaleString()}</td><td>${(Number(m.tool_definition_bytes || 0) / 1024).toFixed(1)} KB</td></tr>`).join('')}</tbody></table></div>`;
        }
        scroller.scrollTop = scroll;
        $('da-guidance-form').hidden = !can('write') || terminal(task);
        $('da-guidance-submit').textContent = L(task.question ? 'answer' : 'send');
        if (S.busy) $('da-controls').querySelectorAll('button').forEach(b => b.disabled = true);
    }
    async function refresh() {
        if (S.busy || !S.mounted) return;
        const epoch = ++S.epoch, id = $('da-inspector').open ? S.id : '';
        try {
            const query = new URLSearchParams({ q: $('da-search').value, status: $('da-filter').value, limit: '20', offset: String(S.page * 20) });
            const [list, detail] = await Promise.all([json('?' + query), id ? json('/' + encodeURIComponent(id) + '?step_limit=50&step_offset=' + S.stepOffset + (S.tab === 'plan' && !S.focusStepId ? '&step_order=queue&step_status=' + encodeURIComponent(S.intentStatus) + '&step_asset=' + encodeURIComponent(S.intentAsset) : '')) : null]);
            if (epoch !== S.epoch || S.busy) return;
            S.more = list.has_more;
            const listScroll = $('da-list').scrollTop;
            $('da-list').innerHTML = (list.tasks || []).map(task => `<button type="button" class="da-list-item ${task.id === S.id ? 'selected' : ''}" data-da-action="select" data-id="${esc(task.id)}" aria-haspopup="dialog"><strong>${esc(task.name)}</strong><span>${badge(task.status)}</span><span class="da-row-rounds"><small>${esc(L('rounds'))}</small>${task.rounds || 0}</span><span class="da-row-tokens"><small>${esc(L('tokens'))}</small>${Number(task.tokens_used || 0).toLocaleString()}</span><time>${esc(new Date(task.updated_at).toLocaleString())}</time></button>`).join('') || `<p class="da-muted da-empty">${esc(L('empty'))}</p>`;
            $('da-list').scrollTop = listScroll;
            $('da-page').textContent = String(S.page + 1);
            $('deep-assessments-root').querySelector('[data-da-action="previous"]').disabled = S.page === 0;
            $('deep-assessments-root').querySelector('[data-da-action="next"]').disabled = !S.more;
            if (detail && (!S.task || detail.task.revision >= S.task.revision)) S.task = detail.task;
            if (detail) { S.intentCounts = detail.intent_counts || {}; S.stepTotal = detail.step_total ?? detail.task.steps?.length ?? 0; S.stepMore = !!detail.step_has_more; }
            if (id && S.originFindingId && canFindings()) {
                const data = await json('/' + encodeURIComponent(id) + '/vulnerabilities/' + encodeURIComponent(S.originFindingId)); if (epoch !== S.epoch) return;
                S.originFinding = data.vulnerability; S.focusedStep = data.step; S.focusStepId = data.vulnerability.assessment_step_id;
            } else if (id && S.focusStepId) {
                const data = await json('/' + encodeURIComponent(id) + '/steps/' + encodeURIComponent(S.focusStepId)); if (epoch !== S.epoch) return; S.focusedStep = data.step;
            }
            if (id && ['assets','facts'].includes(S.tab)) { const data = await window.AssessmentExplorationViews.load(id,S.tab,json); if (epoch !== S.epoch) return; S.extra = data; }
            if (id && S.tab === 'findings' && canFindings()) {
                const data = await json('/' + encodeURIComponent(id) + '/vulnerabilities?limit=20&offset=' + S.findingOffset); if (epoch !== S.epoch) return;
                S.findings = data.vulnerabilities || []; S.findingTotal = data.total; S.findingMore = data.has_more;
            }
            if (id && S.tab === 'graph') { const data = await json('/' + encodeURIComponent(id) + '/graph?' + new URLSearchParams({ scope: S.graphScope, offset: S.graphOffset, limit: 80 })); if (epoch !== S.epoch) return; S.graph = data; }
            if (id && S.tab === 'activity') { const data = await json('/' + encodeURIComponent(id) + '/events?after=' + (S.events.at(-1)?.id || 0)); if (epoch !== S.epoch) return; S.events.push(...data.events); S.events = S.events.slice(-500); }
            if (id && S.tab === 'evidence') { const data = await json('/' + encodeURIComponent(id) + '/receipts?' + new URLSearchParams({ offset: S.receiptOffset, step_id: S.focusStepId })); if (epoch !== S.epoch) return; S.receipts = data.receipts; S.receiptMore = data.has_more; }
            renderTask();
        } catch (error) { if (epoch === S.epoch) notice(error.message, true); }
    }
    const graphKind = kind => tr('graphKinds.' + kind, { fact: '事实', finding: '漏洞', goal: '目标', step: '探索意图', observation: '观察', receipt: '执行回执', memory: 'Pi 摘要', digest: '历史归档', guidance: '人工线索' }[kind] || kind);
    function renderGraph(body) {
        if (!body.querySelector('#da-graph-canvas')) {
            window.DeepAssessmentGraph?.destroy();
            body.innerHTML = `<div class="da-graph-toolbar">${button('graph-home', L('graphHome'))}${button('graph-fit', L('graphFit'))}${button('graph-in', '+', 'aria-label="Zoom in"')}${button('graph-out', '−', 'aria-label="Zoom out"')}<span id="da-graph-meta"></span></div><p class="da-muted">${esc(L('graphHint'))}</p><div class="da-graph-legend">${['goal','step','fact','finding','observation','receipt','memory','digest','guidance'].map(k => `<span class="da-kind-${k}">${esc(graphKind(k))}</span>`).join('')}</div><select id="da-graph-select" class="form-control" aria-label="${esc(L('graphSelect'))}"></select><div class="da-graph-workspace"><div id="da-graph-canvas" role="img" aria-label="${esc(L('graph'))}"></div><section id="da-graph-node" aria-live="polite"></section></div><div id="da-graph-pages" class="da-controls"></div>`;
        }
        if (!S.graph) { $('da-graph-meta').textContent = L('loading'); return; }
        $('da-graph-meta').textContent = `${L('graphVisible')} ${Math.max(0, S.graph.nodes.length - 1)} / ${S.graph.total} · ${L('graphFolded')} ${S.graph.folded}`;
        $('da-graph-select').innerHTML = `<option value="">${esc(L('graphSelect'))}</option>` + S.graph.nodes.map(n => `<option value="${esc(n.id)}" ${S.graphNode?.id === n.id ? 'selected' : ''}>${esc(graphKind(n.kind) + ' · ' + n.title)}</option>`).join('');
        const task = S.id;
        window.DeepAssessmentGraph?.render($('da-graph-canvas'), S.graph, task + '/' + S.graphScope + '/' + S.graphOffset, node => { if (S.id === task) void selectGraphNode(node); }, graphKind);
        $('da-graph-pages').innerHTML = `${S.graphOffset ? button('graph-previous', L('previous')) : ''}${S.graph.has_more ? button('graph-next', L('next')) : ''}`;
        renderGraphNode();
    }
    const graphState = state => tr('graphStates.' + state, { observed: '直接观察', inferred: '推断待证', reported: '待独立复核', confirmed: '复核确认', refuted: '复核否定', inconclusive: '复核未定', unreviewed: '历史记录待核对', summary: '压缩摘要', note: '观察记录', user: '用户提供', folded: '已折叠' }[state] || status(state));
    function renderGraphNode() {
        const panel = $('da-graph-node'); if (!panel) return;
        const n = S.graphNode;
        panel.innerHTML = n ? `<small>${esc(graphKind(n.kind))} · ${esc(graphState(n.state))}</small><h4>${esc(n.title)}</h4><code>${esc(n.id)}</code><pre>${esc(n.body)}</pre><details><summary>${esc(L('evidenceIDs'))}</summary><pre>${esc(JSON.stringify(n.source || {}, null, 2))}</pre></details><div class="da-controls">${window.AssessmentExplorationViews?.canAssets()?(n.source?.asset_ids||[]).map(id=>button('intent-asset',L('linkedAssets')+' '+id,`data-asset="${esc(id)}"`)).join(''):''}${n.kind === 'finding' ? button('open-finding',L('openFinding'),`data-vulnerability="${esc(n.id.slice(8))}"`) : ''}${n.step_id ? button('fact-proof',L('sourceStep'),`data-step="${esc(n.step_id)}"`) : ''}${['step','digest'].includes(n.kind) ? button('graph-expand', L('graphExpand'), `data-node="${esc(n.id)}"`) : ''}${S.nodeMore ? button('graph-more', L('graphMore')) : ''}</div>` : `<p class="da-muted">${esc(L('graphSelect'))}</p>`;
    }
    async function selectGraphNode(node, append = false) {
        const task = S.id, request = ++S.nodeRequest;
        try {
            const data = await json('/' + encodeURIComponent(task) + '/graph/node?' + new URLSearchParams({ node_id: node, offset: append ? S.nodeOffset : 0 }));
            if (request !== S.nodeRequest || S.id !== task || S.tab !== 'graph') return;
            if (append && S.graphNode?.id === node) data.node.body = S.graphNode.body + data.node.body;
            S.graphNode = data.node; window.DeepAssessmentGraph?.select(node); if ($('da-graph-select')) $('da-graph-select').value = node; S.nodeOffset = data.next_offset; S.nodeMore = data.has_more;
            renderGraphNode();
        } catch (error) { if (request === S.nodeRequest && S.id === task) notice(error.message, true); }
    }
    async function mutate(action, values = {}) {
        if (S.busy || !S.task || !can('write')) return false;
        S.busy = true; ++S.epoch; renderTask();
        try {
            const data = await json('/' + encodeURIComponent(S.id) + '/control/' + action + '?step_limit=50&step_offset=' + S.stepOffset, { method: 'POST', body: JSON.stringify({ revision: S.task.revision, ...values }) });
            S.task = data.task; S.stepTotal = data.step_total ?? data.task.steps?.length ?? 0; S.stepMore = !!data.step_has_more; notice(data.start_error || L('updated'), !!data.start_error); return !data.start_error;
        } catch (error) { notice(error.message, true); return false; }
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
        showForm(L('create'), `<form id="da-create-form" class="da-drawer-form"><div class="da-form-body">${field('name', L('name'), '', 'text', 'maxlength="200" required')}<label>${esc(L('objective'))}<textarea class="form-control" name="objective" rows="3" maxlength="16000" required></textarea></label><label>${esc(L('scope'))}<textarea class="form-control" name="scope" rows="3" maxlength="16000" required></textarea></label><label>${esc(L('criteria'))}<textarea class="form-control" name="criteria" rows="2" maxlength="8000"></textarea></label><div class="da-form-grid">${choices('model', L('model'), opts.models || [], L('default'))}${choices('role', L('role'), opts.roles || [], L('default'))}${choices('project_id', L('project'), opts.projects || [], L('none'))}${field('concurrency', L('concurrency'), 2, 'number', 'min="1" max="4" required')}</div><label class="da-check"><input type="checkbox" name="auto_resume" checked>${esc(L('autoResume'))}</label><p class="da-muted">${esc(L('recoveryNote'))}</p></div><footer class="da-drawer-footer"><button type="submit" class="btn-primary">${esc(L('save'))}</button>${button('close', L('close'))}</footer></form>`);
    }
    async function saveCreate(form) {
        if (S.busy || !can('write')) return;
        const values = Object.fromEntries(new FormData(form));
        values.concurrency = Number(values.concurrency); values.auto_resume = form.elements.auto_resume.checked;
        S.busy = true; ++S.epoch; form.querySelector('button[type="submit"]').disabled = true;
        try { const data = await json('', { method: 'POST', body: JSON.stringify(values) }); S.id = data.task.id; S.intentAsset=''; S.extra=null; window.AssessmentExplorationViews?.reset(S.id); S.task = data.task; S.focusStepId = ''; S.focusedStep = null; S.originFindingId = ''; S.originFinding = null; S.findingOffset = 0; S.tab = 'graph'; S.stepOffset = 0; S.stepMore = false; S.stepTotal = 0; S.graph = null; S.graphScope = ''; S.graphOffset = 0; S.graphNode = null; S.events = []; S.receipts = []; S.receiptOffset = 0; closeDrawer('da-form-drawer'); writeRoute(); renderTask(); if ($('page-deep-assessments')?.classList.contains('active')) openDrawer('da-inspector'); notice(L('saved')); }
        catch (error) { notice(error.message, true); }
        finally { S.busy = false; form.querySelector('button[type="submit"]').disabled = false; await refresh(); }
    }
    async function guidance() { const value = $('da-guidance').value.trim(); if (!value) return; if (await mutate(S.task.question ? 'answer' : 'guidance', { message: value })) { if ($('da-guidance').value.trim() === value) $('da-guidance').value = ''; } }
    async function action(name, data = {}) {
        if (S.busy) return;
        try {
            if (await window.AssessmentExplorationViews?.action(name,data,json)) { if(['asset-linked','asset-all','asset-search'].includes(name)){S.intentAsset='';writeRoute();} return await refresh(); }
            if (name === 'intent-asset') { S.intentAsset=data.asset; window.AssessmentExplorationViews.focusAsset(data.asset); S.tab='assets'; writeRoute(); return await refresh(); }
            if (name === 'asset-intents') { S.focusStepId='';S.focusedStep=null;S.originFindingId='';S.originFinding=null;S.intentAsset=data.asset;S.intentStatus='';S.stepOffset=0;S.tab='plan';writeRoute();return await refresh(); }
            if (name === 'clear-intent-asset') { S.intentAsset='';S.stepOffset=0;return await refresh(); }
            if (name === 'fact-proof') { S.focusStepId=data.step; S.originFindingId=''; S.originFinding=null; S.focusedStep=null; S.tab='evidence'; S.receiptOffset=0; writeRoute(); return await refresh(); }
            if (name === 'close') return closeDrawer('da-form-drawer');
            if (name === 'close-detail') return closeDrawer('da-inspector');
            if (name === 'create') return await create();
            if (name === 'refresh') return await refresh();
            if (name === 'previous' || name === 'next') { S.page += name === 'previous' ? -1 : 1; S.page = Math.max(0, S.page); return await refresh(); }
            if (name === 'select') { ++S.drawerEpoch; S.focusStepId = ''; S.focusedStep = null; S.originFindingId = ''; S.originFinding = null; S.findingOffset = 0; if (S.id !== data.id) { S.id = data.id; S.extra=null; S.intentAsset=''; S.intentStatus=''; window.AssessmentExplorationViews?.reset(S.id); S.task = null; S.tab = 'graph'; S.stepOffset = 0; S.stepMore = false; S.stepTotal = 0; S.graph = null; S.graphScope = ''; S.graphOffset = 0; S.graphNode = null; ++S.nodeRequest; S.events = []; S.receipts = []; S.receiptOffset = 0; $('da-guidance').value = ''; $('da-inspector').querySelector('.da-detail-scroll').scrollTop = 0; } writeRoute(); renderTask(); openDrawer('da-inspector'); return await refresh(); }
            if (name === 'tab') { S.tab = data.tab; writeRoute(); $('da-inspector').querySelector('.da-detail-scroll').scrollTop = 0; return await refresh(); }
            if (name === 'open-finding' || name === 'all-findings') { closeDrawer('da-form-drawer'); closeDrawer('da-inspector'); window.AssessmentFindingLinks?.openFinding(name === 'open-finding' ? data.vulnerability : '', S.id); return; }
            if (name === 'finding-evidence') { S.originFindingId = data.vulnerability; S.focusStepId = data.step; S.focusedStep = null; S.originFinding = null; S.tab = 'evidence'; S.receiptOffset = 0; writeRoute(); return await refresh(); }
            if (name === 'clear-source') { S.focusStepId = ''; S.focusedStep = null; S.originFindingId = ''; S.originFinding = null; S.receiptOffset = 0; writeRoute(); return await refresh(); }
            if (name === 'next-findings' || name === 'previous-findings') { S.findingOffset = Math.max(0, S.findingOffset + (name === 'next-findings' ? 20 : -20)); return await refresh(); }
            if (name === 'step-next' || name === 'step-previous') { S.stepOffset = Math.max(0, S.stepOffset + (name === 'step-next' ? 50 : -50)); return await refresh(); }
            if (name === 'graph-fit') return window.DeepAssessmentGraph?.fit();
            if (name === 'graph-in') return window.DeepAssessmentGraph?.zoomIn();
            if (name === 'graph-out') return window.DeepAssessmentGraph?.zoomOut();
            if (name === 'graph-home' || name === 'graph-expand') { S.graphScope = name === 'graph-home' ? '' : data.node; S.graphOffset = 0; S.graphNode = null; ++S.nodeRequest; return await refresh(); }
            if (name === 'graph-next' || name === 'graph-previous') { S.graphOffset = Math.max(0, S.graphOffset + (name === 'graph-next' ? 80 : -80)); return await refresh(); }
            if (name === 'graph-more' && S.graphNode) return await selectGraphNode(S.graphNode.id, true);
            if (name === 'start' || name === 'pause') return await mutate(name);
            if (name === 'cancel') { if (window.confirm(L('confirmStop'))) return await mutate('cancel'); return; }
            if (name === 'conversation') { closeDrawer('da-form-drawer'); closeDrawer('da-inspector'); if (typeof window.switchPage === 'function') window.switchPage('chat'); if (typeof window.loadConversation === 'function') await window.loadConversation(data.conversation); return; }

            if (name === 'retry') { const step = (S.focusedStep?.id === data.step ? S.focusedStep : S.task.steps.find(s => s.id === data.step)); const uncertain = ['uncertain', 'cleanup_unconfirmed'].includes(step?.status); if (uncertain && !window.confirm(L('unknown') + '\n\n' + L('reconcile'))) return; return await mutate('retry', { step_id: data.step, reconciled: uncertain, cleanup_confirmed: uncertain }); }
            if (name === 'report') { const response = await api('/' + encodeURIComponent(S.id) + '/report'); const url = URL.createObjectURL(await response.blob()); const a = document.createElement('a'); a.href = url; a.download = 'assessment-' + S.id + '.md'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); return; }
            if (name === 'delete' && can('delete') && window.confirm(L('confirmDelete'))) { S.busy = true; ++S.epoch; try { await api('/' + encodeURIComponent(S.id), { method: 'DELETE' }); S.id = ''; S.task = null; closeDrawer('da-inspector'); } finally { S.busy = false; } return await refresh(); }
            if (name === 'more-receipts' || name === 'previous-receipts') { S.receiptOffset = Math.max(0, S.receiptOffset + (name === 'more-receipts' ? 50 : -50)); return await refresh(); }
        } catch (error) { notice(error.message, true); }
    }
    window.DeepAssessments = { init() { mount(); applyRoute(); void refresh(); if (!S.timer) S.timer = setInterval(() => { if ($('page-deep-assessments')?.classList.contains('active') && !document.hidden) void refresh(); }, 4000); }, fromLocation() { mount(); if (applyRoute()) void refresh(); }, refresh };
})();
