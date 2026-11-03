/* Durable assessment workbench. This module never calls the chat API. */
(function () {
    'use strict';
    let selected = '', tab = 'overview', timer = null, offset = 0, epoch = 0, loading = false;
    let task = null, rows = [], events = [], after = 0, options = { models: [], tools: [] }, graph = null;
    let dagView = null, graphResize = null, graphTheme = null;
    const root = () => document.getElementById('assessment-workbench');
    const tr = (key) => typeof window.t === 'function' ? window.t('assessments.' + key) : key;
    const esc = (v) => String(v == null ? '' : v).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    const can = (p) => typeof window.hasPermission === 'function' && window.hasPermission(p);
    const badge = (s) => '<span class="assessment-status" data-status="' + esc(s) + '">' + esc(tr('status.' + s)) + '</span>';
    const button = (action, text, permission, attrs) => '<button type="button" class="btn-secondary btn-small" data-assessment-action="' + action + '" ' + (attrs || '') + (permission && !can(permission) ? ' disabled' : '') + '>' + esc(text) + '</button>';
    async function api(path, method, body) {
        const response = await apiFetch('/api/assessments' + path, { method: method || 'GET', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || response.statusText);
        return data;
    }
    function message(text, error) {
        const el = document.getElementById('assessment-message');
        if (el) { el.textContent = text; el.classList.toggle('is-error', !!error); }
    }
    function destroyGraph() {
        if (graphResize) { graphResize.disconnect(); graphResize = null; }
        if (graphTheme) { graphTheme.disconnect(); graphTheme = null; }
        if (!graph) return;
        const context = graph.scratch('assessmentDag');
        if (context) dagView = { ...context, zoom: graph.zoom(), pan: { ...graph.pan() },
            selected: graph.$('node:selected').id(), positions: new Map(graph.nodes().map(n => [n.id(), { ...n.position() }])),
            scroll: document.getElementById('assessment-dag-inspector')?.scrollTop || 0 };
        graph.destroy(); graph = null;
    }
    function shell() {
        root().innerHTML = '<div class="assessment-header"><div><h1>' + esc(tr('title')) + '</h1><p>' + esc(tr('subtitle')) + '</p></div><div class="assessment-actions">' + button('refresh', tr('refresh')) + button('new', tr('new'), 'assessment:write') + '</div></div>' +
            '<p id="assessment-message" role="status" aria-live="polite"></p><div class="assessment-layout"><aside class="assessment-list"><h2>' + esc(tr('tasks')) + '</h2><div id="assessment-task-list"></div><div class="assessment-actions">' + button('previous', tr('previous')) + button('next', tr('next')) + '</div></aside><section id="assessment-detail" class="assessment-detail"></section></div>';
    }
    function list() {
        const el = document.getElementById('assessment-task-list');
        if (!el) return;
        el.innerHTML = rows.length ? rows.map(r => '<button class="assessment-list-item' + (r.id === selected ? ' selected' : '') + '" data-assessment-action="select" data-id="' + esc(r.id) + '"><strong>' + esc(r.name) + '</strong>' + badge(r.status) + '<small>' + esc(new Date(r.updated_at).toLocaleString()) + '</small></button>').join('') : '<p class="assessment-empty">' + esc(tr('empty')) + '</p>';
    }
    function detail() {
        const pickerFocused = document.activeElement?.id === 'assessment-dag-picker';
        destroyGraph();
        const el = document.getElementById('assessment-detail'); if (!el) return;
        if (!task) { el.innerHTML = '<div class="assessment-empty"><h2>' + esc(tr('welcome')) + '</h2><p>' + esc(tr('welcomeBody')) + '</p></div>'; return; }
        const active = ['running', 'awaiting_approval', 'pausing', 'stopping'].includes(task.status);
        const resumable = ['draft', 'paused', 'blocked', 'partial', 'failed'].includes(task.status);
        const tools = task.tools.join(', ');
        let controls = '';
        if (resumable && can('agent:execute')) controls += button('start', tr(task.status === 'draft' ? 'start' : 'resume'), 'assessment:write');
        if (['running', 'awaiting_approval'].includes(task.status)) controls += button('pause', tr('pause'), 'assessment:write');
        if (!['completed', 'stopped', 'exhausted', 'failed'].includes(task.status)) controls += button('stop', tr('stop'), 'assessment:write');
        controls += button('report', tr('report'), 'assessment:read') + button('metrics', tr('metrics'), 'assessment:read');
        if (['draft','paused','blocked','partial','failed','exhausted'].includes(task.status)) controls += button('budget', tr('adjustBudget'), 'assessment:write');
        if (!active) controls += button('delete', tr('delete'), 'assessment:delete');
        el.innerHTML = '<div class="assessment-detail-heading"><div><h2>' + esc(task.name) + '</h2>' + badge(task.status) + '</div><div class="assessment-actions">' + controls + '</div></div>' +
            '<div class="assessment-metrics"><div><strong>' + task.steps.filter(s => s.status === 'completed').length + ' / ' + task.steps.filter(s => s.status !== 'skipped').length + '</strong><span>' + esc(tr('steps')) + (task.steps.some(s => s.status === 'skipped') ? ' · ' + esc(tr('status.skipped')) + ' ' + task.steps.filter(s => s.status === 'skipped').length : '') + '</span></div><div><strong>' + task.tokens.toLocaleString() + ' / ' + task.token_budget.toLocaleString() + '</strong><span>' + esc(tr('tokens')) + '</span></div><div><strong>' + task.rounds + ' / ' + task.max_rounds + '</strong><span>' + esc(tr('rounds')) + '</span></div><div><strong>' + Math.max(0, Math.ceil((task.deadline ? task.deadline - Date.now() / 1000 : task.remaining_seconds) / 60)) + '</strong><span>' + esc(tr('minutes')) + '</span></div></div>' +
            '<div class="assessment-tabs" role="tablist">' + ['overview', 'dag', 'plan', 'review', 'chain', 'evidence', 'logs'].map(id => '<button role="tab" aria-selected="' + (tab === id) + '" class="' + (tab === id ? 'active' : '') + '" data-assessment-action="tab" data-tab="' + id + '">' + esc(tr(id)) + '</button>').join('') + '</div><div id="assessment-tab" role="tabpanel"></div>';
        const body = document.getElementById('assessment-tab');
        if (tab === 'overview') {
            body.innerHTML = ['goal', 'scope', 'material', 'strategy', 'summary'].map(key => '<section class="assessment-card"><h3>' + esc(tr(key)) + '</h3><div class="assessment-text">' + esc(task[key] || tr('none')) + '</div></section>').join('') +
                '<p class="assessment-text">' + esc(tr('reason')) + ': ' + esc(task.reason || '—') + '\n' + esc(tr('model')) + ': ' + esc(task.model || tr('default')) + '\n' + esc(tr('tools')) + ': ' + esc(tools) + '\n' + esc(tr('project')) + ': ' + esc(task.project_id || '—') + '</p>' +
                (resumable ? '<label class="assessment-field">' + esc(tr('guidance')) + '<textarea id="assessment-guidance" maxlength="8000" rows="3"></textarea></label>' + button('steer', tr('saveGuidance'), 'assessment:write') : '');
        } else if (tab === 'dag') {
            renderDag(body);
            if (pickerFocused) document.getElementById('assessment-dag-picker')?.focus({ preventScroll: true });
        } else if (tab === 'review') {
            body.innerHTML = '<h3>' + esc(tr('criteria')) + '</h3>' + (task.criteria || [task.goal]).map((c,i) => {const v=task.review && task.review.criteria.find(x=>x.index===i);return '<article class="assessment-card"><h3>'+esc(c)+'</h3><strong>'+esc(tr('verdict.'+(v?v.status:'insufficient')))+'</strong><p class="assessment-text">'+esc(v?v.reason:tr('reviewPending'))+'</p></article>';}).join('') + '<h3>'+esc(tr('findingReview'))+'</h3>' + (task.review && task.review.findings || []).map(v=>'<article class="assessment-card"><strong>'+esc(tr('verdict.'+v.status))+'</strong><p class="assessment-text">'+esc(v.reason)+'</p><small>'+esc((v.execution_ids||[]).join(', '))+'</small></article>').join('') + '<h3>'+esc(tr('phaseUsage'))+'</h3><pre class="assessment-text">'+esc(JSON.stringify(task.usage || {},null,2))+'</pre>';
        } else if (tab === 'plan') {
            body.innerHTML = task.steps.map(s => '<article class="assessment-card"><div class="assessment-step-title"><h3>' + esc(s.title) + '</h3>' + badge(s.status) + '</div><small>' + esc(s.key) + (s.depends_on && s.depends_on.length ? ' ← ' + esc(s.depends_on.join(', ')) : '') + '</small><p class="assessment-text">' + esc(s.instruction) + '</p><p class="assessment-text">' + esc(s.result) + '</p>' + (s.checkpoint ? '<p class="assessment-text">' + esc(tr('checkpoint')) + ': ' + esc(s.checkpoint) + '</p>' : '') + (s.retired_reason ? '<p class="assessment-text">' + esc(tr('retiredReason')) + ': ' + esc(s.retired_reason) + '</p>' : '') +
                (s.pending ? '<div class="assessment-approval"><h4>' + esc(tr('approval')) + '</h4><p>' + esc(tr('approvalHint')) + '</p><strong>' + esc(s.pending.function.name) + '</strong><pre>' + esc(JSON.stringify(s.pending.function.arguments, null, 2)) + '</pre>' +
                    (s.status === 'awaiting_approval' && !['completed', 'stopped', 'exhausted', 'failed'].includes(task.status) ? button('approve', tr('approve'), 'hitl:write', 'data-step="' + esc(s.id) + '" data-approval="' + esc(s.approval_id) + '"' + (!can('assessment:write') ? ' disabled' : '')) + button('deny', tr('deny'), 'hitl:write', 'data-step="' + esc(s.id) + '" data-approval="' + esc(s.approval_id) + '"' + (!can('assessment:write') ? ' disabled' : '')) : '<p>' + esc(tr('approved')) + '</p>') + '</div>' : '') +
                (s.status === 'blocked' && resumable ? button('retry', tr('retry'), 'assessment:write', 'data-step="' + esc(s.id) + '"') : '') + '</article>').join('') || '<p class="assessment-empty">' + esc(tr('noSteps')) + '</p>';
        } else if (tab === 'evidence') {
            body.innerHTML = '<p>' + esc(tr('candidateHint')) + '</p>' + task.steps.map(s => (s.evidence || []).map((v, i) => '<article class="assessment-card"><small>' + esc(s.title) + ' · ' + esc(v.kind) + '</small><h3>' + esc(v.title) + '</h3><p class="assessment-text">' + esc(v.detail) + '</p><p class="assessment-text">' + esc((v.execution_ids || []).join('\n')) + '</p>' +
                (v.kind !== 'gap' && v.execution_ids.length ? (task.project_id ? button('fact', tr('publishFact'), 'project:write', 'data-step="' + esc(s.id) + '" data-index="' + i + '" data-revision="' + esc(v.revision) + '"') : '') + (v.kind === 'asset' && v.asset ? button('asset', tr('publishAsset'), 'asset:write', 'data-step="' + esc(s.id) + '" data-index="' + i + '" data-revision="' + esc(v.revision) + '"') : '') + (v.kind === 'finding' ? button('vulnerability', tr('publishFinding'), 'vulnerability:write', 'data-step="' + esc(s.id) + '" data-index="' + i + '" data-revision="' + esc(v.revision) + '"') : '') : '') + '</article>').join('')).join('');
        } else if (tab === 'logs') {
            body.innerHTML = '<div class="assessment-log">' + events.map(ev => '<details><summary><time>' + esc(ev.created_at) + '</time> · ' + esc(ev.kind) + ' · ' + esc(ev.message.slice(0, 100)) + '</summary><pre>' + esc(ev.message) + '\n' + esc(ev.data ? JSON.stringify(ev.data, null, 2) : '') + '</pre></details>').join('') + '</div>';
        } else if (tab === 'chain') {
            body.innerHTML = '<p>' + esc(tr('chainHint')) + '</p><div id="assessment-graph" aria-label="' + esc(tr('chain')) + '"></div><p id="assessment-graph-selection" class="assessment-text"></p>';
            renderGraph();
        }
    }
    // Longest-path ranks keep every dependency left of its successor, including
    // multi-parent joins. No external layout plugin or CDN is required.
    function buildDag(assessment) {
        const steps = assessment.steps || [], byKey = new Map(steps.map(s => [s.key, s]));
        const nodeID = s => 'step:' + s.id;
        const nodes = [{ data: { id: 'goal', label: tr('goal'), kind: 'goal' } }];
        const edges = [], parents = new Map([['goal', []]]), children = new Map([['goal', []]]);
        let invalid = false;
        for (const s of steps) {
            const id = nodeID(s);
            nodes.push({ data: { id, stepID: s.id, kind: s.status, label: s.title.slice(0, 30) + (s.title.length > 30 ? '…' : '') + '\n' + tr('status.' + s.status) } });
            parents.set(id, []); children.set(id, []);
        }
        for (const s of steps) {
            const deps = [...new Set(s.depends_on || [])], target = nodeID(s);
            const sources = deps.length ? deps.flatMap(key => {
                const dep = byKey.get(key);
                if (!dep) { invalid = true; return []; }
                return [nodeID(dep)];
            }) : ['goal'];
            for (const source of sources) {
                parents.get(target).push(source); children.get(source).push(target);
                edges.push({ data: { id: 'edge:' + edges.length, source, target } });
            }
        }
        const degrees = new Map(nodes.map(n => [n.data.id, parents.get(n.data.id).length]));
        const ranks = new Map(nodes.map(n => [n.data.id, n.data.id === 'goal' ? 0 : 1]));
        const queue = nodes.map(n => n.data.id).filter(id => !degrees.get(id));
        for (let i = 0; i < queue.length; i++) {
            const id = queue[i];
            for (const next of children.get(id)) {
                ranks.set(next, Math.max(ranks.get(next), ranks.get(id) + 1));
                degrees.set(next, degrees.get(next) - 1);
                if (!degrees.get(next)) queue.push(next);
            }
        }
        if (queue.length !== nodes.length) {
            invalid = true;
            // Keep damaged legacy data inspectable; never loop on a cycle or
            // silently drop steps and present an incomplete graph as a valid DAG.
            const tail = Math.max(...ranks.values()) + 1;
            nodes.filter(n => degrees.get(n.data.id)).forEach(n => ranks.set(n.data.id, tail));
        }
        const layers = [];
        for (const n of nodes) (layers[ranks.get(n.data.id)] ||= []).push(n.data.id);
        const order = new Map();
        const record = () => layers.forEach(ids => ids.forEach((id, i) => order.set(id, i)));
        record();
        // Barycentric sweeps reduce crossings while keeping a deterministic order.
        for (let pass = 0; pass < 4; pass++) {
            const forward = pass % 2 === 0, adjacent = forward ? parents : children;
            const indices = layers.map((_, i) => i).filter(i => i > 0);
            if (!forward) indices.reverse();
            for (const i of indices) {
                const score = id => { const peers = adjacent.get(id); return peers.length ? peers.reduce((v, p) => v + order.get(p), 0) / peers.length : order.get(id); };
                layers[i].sort((a, b) => score(a) - score(b)); record();
            }
        }
        for (const n of nodes) {
            const rank = ranks.get(n.data.id), layer = layers[rank];
            n.position = { x: rank * 266, y: (layer.indexOf(n.data.id) - (layer.length - 1) / 2) * 112 };
        }
        return { elements: [...nodes, ...edges], invalid, signature: JSON.stringify([nodes.map(n => n.data.id), edges.map(e => [e.data.source, e.data.target])]) };
    }
    function dagStyles(container) {
        const css = getComputedStyle(container), color = (name, fallback) => css.getPropertyValue(name).trim() || fallback;
        return [
            { selector: 'node', style: { label: 'data(label)', shape: 'round-rectangle', width: 204, height: 80,
                'background-color': color('--bg-primary', '#fff'), 'border-color': color('--border-color', '#cbd5e1'), 'border-width': 1.5,
                color: color('--text-primary', '#334155'), 'text-valign': 'center', 'text-wrap': 'wrap', 'text-max-width': '178px', 'font-size': 13, 'line-height': 1.3 } },
            { selector: 'node[kind="goal"]', style: { 'background-color': '#4f46e5', color: '#fff', 'border-color': '#4f46e5', width: 140, height: 52 } },
            { selector: 'node[kind="running"]', style: { 'border-color': '#6366f1', 'border-width': 3 } },
            { selector: 'node[kind="completed"]', style: { 'border-color': '#0d9488', 'border-width': 2 } },
            { selector: 'node[kind="awaiting_approval"], node[kind="blocked"]', style: { 'border-color': '#d97706', 'border-width': 2 } },
            { selector: 'node[kind="failed"]', style: { 'border-color': '#dc2626', 'border-width': 2 } },
            { selector: 'node[kind="skipped"]', style: { 'border-style': 'dashed', 'background-opacity': .6 } },
            { selector: 'edge', style: { width: 1.5, 'line-color': color('--text-muted', '#94a3b8'), 'target-arrow-color': color('--text-muted', '#94a3b8'), 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', 'arrow-scale': .85 } },
            { selector: 'edge.dag-path', style: { width: 2.5, 'line-color': '#818cf8', 'target-arrow-color': '#818cf8' } },
            { selector: 'node:selected', style: { 'overlay-color': '#6366f1', 'overlay-opacity': .13, 'overlay-padding': 6 } }
        ];
    }
    function renderDagInspector(step) {
        const panel = document.getElementById('assessment-dag-inspector'); if (!panel) return;
        const section = (title, value) => value ? '<section><h4>' + esc(tr(title)) + '</h4><p class="assessment-text">' + esc(value) + '</p></section>' : '';
        if (!step) { panel.innerHTML = '<small>' + esc(tr('dagDetails')) + '</small><h3>' + esc(tr('goal')) + '</h3>' + section('goal', task.goal) + section('scope', task.scope); return; }
        const byKey = new Map(task.steps.map(s => [s.key, s]));
        panel.innerHTML = '<small>' + esc(tr('dagDetails')) + '</small><h3>' + esc(step.title) + '</h3>' + badge(step.status) + '<p class="assessment-dag-key">' + esc(step.key) + '</p>' +
            section('dagInstruction', step.instruction) + section('checkpoint', step.checkpoint) + section('dagResult', step.result) + section('retiredReason', step.retired_reason) +
            '<section><h4>' + esc(tr('dagDependencies')) + '</h4>' + ((step.depends_on || []).length ? step.depends_on.map(key => {
                const dep = byKey.get(key); return dep ? '<button type="button" class="assessment-dag-dependency" data-dag-step="' + esc(dep.id) + '">' + esc(dep.title) + badge(dep.status) + '</button>' : '<p class="assessment-text">' + esc(key) + '</p>';
            }).join('') : '<p>' + esc(tr('dagNoDependencies')) + '</p>') + '</section>' +
            ((step.evidence || []).length ? '<section><h4>' + esc(tr('evidence')) + ' · ' + step.evidence.length + '</h4>' + step.evidence.map(e => '<p class="assessment-text">' + esc(e.title) + '</p>').join('') + '</section>' : '') +
            '<div class="assessment-actions">' + button('tab', tr(step.pending ? 'dagReviewCall' : 'plan'), null, 'data-tab="plan"') + '</div>';
        panel.querySelectorAll('[data-dag-step]').forEach(el => el.addEventListener('click', () => selectDagStep(el.dataset.dagStep, true)));
    }
    function selectDagStep(id, center) {
        const step = task.steps.find(s => s.id === id);
        const picker = document.getElementById('assessment-dag-picker'); if (picker) picker.value = step ? step.id : '';
        renderDagInspector(step);
        if (!graph || !graph.scratch('assessmentDag')) return;
        const node = graph.getElementById(step ? 'step:' + step.id : 'goal');
        graph.elements().removeClass('dag-path'); graph.nodes().unselect(); node.select();
        if (step) {
            const path = node.predecessors().union(node.successors()).union(node);
            path.edges().addClass('dag-path');
        }
        if (center) { graph.resize(); graph.zoom(Math.max(graph.zoom(), .8)); graph.center(node); }
    }
    function renderDag(body) {
        if (!task.steps.length) {
            body.innerHTML = '<div class="assessment-dag-empty"><span aria-hidden="true">◇ → ◇</span><h3>' + esc(tr('dagEmpty')) + '</h3><p>' + esc(tr('noSteps')) + '</p></div>'; return;
        }
        const data = buildDag(task), states = [...new Set(task.steps.map(s => s.status))];
        const controls = [['out', '−', 'dagZoomOut'], ['in', '+', 'dagZoomIn'], ['fit', tr('dagFit'), 'dagFit'], ['layout', tr('dagLayout'), 'dagLayout']];
        body.innerHTML = '<div class="assessment-dag-heading"><div><h3>' + esc(tr('dag')) + '</h3><p>' + esc(tr('dagHint')) + '</p></div><span class="assessment-dag-count">' + task.steps.length + ' ' + esc(tr('dagNodes')) + ' · ' + (data.elements.filter(e => e.data.source && e.data.source !== 'goal').length) + ' ' + esc(tr('dagEdges')) + '</span></div>' +
            '<div class="assessment-dag-toolbar"><label for="assessment-dag-picker">' + esc(tr('dagLocate')) + '</label><select id="assessment-dag-picker"><option value="">' + esc(tr('goal')) + '</option>' + task.steps.map(s => '<option value="' + esc(s.id) + '">' + esc(s.title + ' · ' + tr('status.' + s.status)) + '</option>').join('') + '</select><div class="assessment-actions">' + controls.map(([action, label, title]) => '<button type="button" class="btn-secondary btn-small" data-dag-action="' + action + '" aria-label="' + esc(tr(title)) + '" title="' + esc(tr(title)) + '">' + esc(label) + '</button>').join('') + '</div></div>' +
            (data.invalid ? '<p class="assessment-dag-warning" role="alert">' + esc(tr('dagInvalid')) + '</p>' : '') +
            '<div class="assessment-dag-layout"><div class="assessment-dag-canvas-wrap"><div id="assessment-dag-canvas" role="img" aria-label="' + esc(tr('dagAccessible')) + '"></div><div class="assessment-dag-legend">' + states.map(s => badge(s)).join('') + '</div></div><aside id="assessment-dag-inspector" class="assessment-dag-inspector" aria-label="' + esc(tr('dagDetails')) + '"></aside></div>';
        const picker = document.getElementById('assessment-dag-picker');
        picker.addEventListener('change', () => selectDagStep(picker.value, true));
        const preferred = task.steps.find(s => s.status === 'running') || task.steps.find(s => s.status === 'awaiting_approval') || task.steps.find(s => s.status === 'blocked') || task.steps[0];
        const container = document.getElementById('assessment-dag-canvas');
        if (!window.cytoscape) {
            container.classList.add('assessment-dag-unavailable'); container.textContent = tr('dagUnavailable');
            body.querySelectorAll('[data-dag-action]').forEach(b => { b.disabled = true; });
            selectDagStep(preferred.id, false); return;
        }
        const saved = dagView?.taskID === task.id ? dagView : null;
        if (saved?.signature === data.signature) data.elements.forEach(e => { if (saved.positions.has(e.data.id)) e.position = saved.positions.get(e.data.id); });
        graph = window.cytoscape({ container, elements: data.elements, layout: { name: 'preset', fit: false }, style: dagStyles(container),
            minZoom: .08, maxZoom: 2, wheelSensitivity: .2, selectionType: 'single', boxSelectionEnabled: false });
        graph.scratch('assessmentDag', { taskID: task.id, signature: data.signature });
        const fit = () => { graph.resize(); graph.fit(graph.elements(), 36); if (graph.zoom() > 1) { graph.zoom(1); graph.center(); } };
        if (saved) { graph.zoom(saved.zoom); graph.pan(saved.pan); } else fit();
        const selectedNode = saved?.selected ? graph.getElementById(saved.selected) : null;
        // A phone-sized fit of a long DAG becomes unreadable. Start at the active
        // step with readable labels; Fit still provides the complete overview.
        selectDagStep(selectedNode?.length ? selectedNode.data('stepID') : preferred.id, !saved && container.clientWidth < 500);
        document.getElementById('assessment-dag-inspector').scrollTop = saved?.scroll || 0;
        graph.on('tap', 'node', ev => selectDagStep(ev.target.data('stepID'), false));
        graph.on('tap', ev => { if (ev.target === graph) selectDagStep('', false); });
        body.querySelectorAll('[data-dag-action]').forEach(el => el.addEventListener('click', () => {
            const action = el.dataset.dagAction;
            if (action === 'layout') { const fresh = buildDag(task); fresh.elements.filter(e => e.position).forEach(e => graph.getElementById(e.data.id).position(e.position)); fit(); }
            else if (action === 'fit') fit();
            else graph.zoom({ level: Math.max(graph.minZoom(), Math.min(graph.maxZoom(), graph.zoom() * (action === 'in' ? 1.25 : .8))), renderedPosition: { x: graph.width() / 2, y: graph.height() / 2 } });
        }));
        if (window.ResizeObserver) { graphResize = new ResizeObserver(() => { if (graph) graph.resize(); }); graphResize.observe(container); }
        if (window.MutationObserver) { graphTheme = new MutationObserver(() => { if (graph) graph.style(dagStyles(container)); }); graphTheme.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] }); }
    }
    function renderGraph() {
        const container = document.getElementById('assessment-graph');
        if (!window.cytoscape || !container) return;
        const elements = [{ data: { id: 'goal', label: task.goal.slice(0, 80), kind: 'goal' } }];
        const byKey = Object.fromEntries(task.steps.map(s => [s.key, s.id]));
        task.steps.forEach(s => {
            elements.push({ data: { id: s.id, label: s.title + '\n' + tr('status.' + s.status), kind: s.status, detail: [s.instruction, s.result, s.checkpoint, s.retired_reason].filter(Boolean).join('\n\n') } });
            (s.depends_on && s.depends_on.length ? s.depends_on.map(k => byKey[k]).filter(Boolean) : ['goal']).forEach(parent => elements.push({ data: { source: parent, target: s.id } }));
            (s.evidence || []).forEach((v, i) => { const id = s.id + '-e' + i; elements.push({ data: { id, label: v.title, kind: v.kind, detail: v.detail } }, { data: { source: s.id, target: id } }); });
        });
        graph = window.cytoscape({ container, elements, layout: { name: 'breadthfirst', directed: true, padding: 25, spacingFactor: 1.5 }, style: [
            { selector: 'node', style: { label: 'data(label)', 'background-color': '#64748b', color: getComputedStyle(container).getPropertyValue('--text-primary').trim() || '#334155', 'text-valign': 'bottom', 'text-wrap': 'wrap', 'text-max-width': '150px', 'font-size': 11, 'text-margin-y': 8, width: 28, height: 28 } },
            { selector: 'node[kind="completed"], node[kind="fact"]', style: { 'background-color': '#0d9488' } },
            { selector: 'node[kind="finding"]', style: { 'background-color': '#dc2626' } },
            { selector: 'node[kind="awaiting_approval"]', style: { 'background-color': '#d97706' } },
            { selector: 'node[kind="goal"]', style: { 'background-color': '#4f46e5', width: 40, height: 40 } },
            { selector: 'edge', style: { width: 1.5, 'line-color': '#94a3b8', 'target-arrow-color': '#94a3b8', 'target-arrow-shape': 'triangle', 'curve-style': 'bezier' } }
        ] });
        graph.on('tap', 'node', ev => { document.getElementById('assessment-graph-selection').textContent = ev.target.data('detail') || ev.target.data('label'); });
    }
    async function refresh(initial) {
        if (loading || !root()) return; loading = true; const version = epoch;
        try {
            const listData = await api('?limit=50&offset=' + offset);
            if (version !== epoch) return;
            rows = listData.assessments; list();
            if (selected) {
                const id = selected;
                const data = await api('/' + encodeURIComponent(id));
                const log = await api('/' + encodeURIComponent(id) + '/events?after=' + after);
                if (version !== epoch || selected !== id) return;
                task = data.assessment; events.push(...log.events);
                if (log.events.length) after = log.events[log.events.length - 1].id;
                // Preserve typed guidance, expanded logs, selection and graph on quiet polls.
                const changed = !root().dataset.updated || root().dataset.updated !== task.updated_at;
                if (changed || initial || tab === 'logs' && log.events.length) {
                    const draft = document.getElementById('assessment-guidance'); const value = draft && draft.value;
                    detail(); root().dataset.updated = task.updated_at;
                    const field = document.getElementById('assessment-guidance'); if (field && value) field.value = value;
                }
            } else detail();
            if (document.getElementById('assessment-message')?.classList.contains('is-error')) message('', false);
        } catch (err) { if (version === epoch) message(err.message, true); }
        finally { loading = false; }
    }
    let createDialogLoading = false;
    async function createDialog() {
        if (createDialogLoading || document.getElementById('assessment-create')?.open) return;
        createDialogLoading = true;
        const version = epoch;
        try {
            options = await api('/options');
            if (version !== epoch || !root()) return;
            const prior = document.getElementById('assessment-create');
            if (prior) prior.remove();
            const dialog = document.createElement('dialog');
            dialog.id = 'assessment-create';
            dialog.className = 'assessment-dialog assessment-create-dialog';
            dialog.setAttribute('aria-labelledby', 'assessment-create-title');
            dialog.setAttribute('aria-describedby', 'assessment-create-intro');
            const required = '<span class="assessment-required" aria-hidden="true">*</span>';
            const field = (name, type, value, extra) => '<label class="assessment-field"><span>' + esc(tr(name === 'concurrency' ? 'concurrencyShort' : name)) + '</span><input name="' + name + '" type="' + type + '" value="' + esc(value ?? '') + '" ' + (extra || '') + '></label>';
            const textarea = (name, rows, mandatory, placeholder) => '<label class="assessment-field"><span>' + esc(tr(name)) + (mandatory ? required : '') + '</span><textarea name="' + name + '" rows="' + rows + '" maxlength="' + (name === 'material' ? '24000' : '12000') + '" placeholder="' + esc(tr(placeholder)) + '"' + (mandatory ? ' required' : '') + '></textarea></label>';
            const modelOptions = (inherit) => (inherit ? '<option value="">' + esc(tr('inheritModel')) + '</option>' : '') + options.models.map(m => '<option value="' + esc(m.id) + '"' + (!inherit && m.default ? ' selected' : '') + '>' + esc((m.name || m.id || tr('default')) + (m.model ? ' · ' + m.model : '')) + '</option>').join('');
            const closeIcon = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18"/></svg>';
            dialog.innerHTML = '<form id="assessment-create-form">' +
                '<header class="assessment-dialog-header"><div><h2 id="assessment-create-title">' + esc(tr('new')) + '</h2><p id="assessment-create-intro">' + esc(tr('createIntro')) + '</p></div><button type="button" class="assessment-icon-button" data-assessment-action="cancel-create" aria-label="' + esc(tr('cancel')) + '">' + closeIcon + '</button></header>' +
                '<div class="assessment-create-scroll"><div class="assessment-create-grid"><div class="assessment-create-main">' +
                '<section class="assessment-create-section"><h3>' + esc(tr('taskBasics')) + '</h3><div class="assessment-form-grid">' +
                '<label class="assessment-field"><span>' + esc(tr('name')) + required + '</span><input name="name" type="text" required maxlength="200" placeholder="' + esc(tr('namePlaceholder')) + '" autofocus></label>' +
                '<label class="assessment-field"><span>' + esc(tr('project')) + '<small>' + esc(tr('optional')) + '</small></span><select name="project_id"><option value="">' + esc(tr('noProject')) + '</option>' + (options.projects || []).map(p => '<option value="' + esc(p.id) + '">' + esc(p.name) + '</option>').join('') + '</select></label></div>' +
                textarea('goal', 3, true, 'goalPlaceholder') + textarea('scope', 3, true, 'scopePlaceholder') +
                '<details class="assessment-disclosure"><summary>' + esc(tr('materialsAndCriteria')) + '<span>' + esc(tr('optional')) + '</span></summary><div class="assessment-disclosure-body">' + textarea('material', 2, false, 'materialPlaceholder') + textarea('criteria', 2, false, 'criteriaPlaceholder') + '</div></details></section>' +
                '<section class="assessment-create-section"><div class="assessment-section-heading"><h3>' + esc(tr('tools')) + required + '</h3><span id="assessment-tools-count" class="assessment-selection-count" role="status"></span></div>' +
                '<div class="assessment-tool-toolbar"><input type="search" id="assessment-tools-search" placeholder="' + esc(tr('searchTools')) + '" aria-label="' + esc(tr('searchTools')) + '"><button type="button" class="btn-secondary btn-small" data-tool-selection="visible">' + esc(tr('selectVisible')) + '</button><button type="button" class="assessment-text-button" data-tool-selection="clear">' + esc(tr('clearSelection')) + '</button></div>' +
                '<div class="assessment-tool-picker">' + options.tools.map(tool => '<label class="assessment-tool-option" data-tool-search="' + esc((tool.name + ' ' + tool.description).toLowerCase()) + '"><input type="checkbox" name="tools" value="' + esc(tool.name) + '"><span><strong>' + esc(tool.name) + '</strong><small title="' + esc(tool.description) + '">' + esc(tool.description) + '</small></span></label>').join('') + '<p id="assessment-tools-empty" class="assessment-inline-hint" hidden>' + esc(tr('noMatchingTools')) + '</p></div><p class="assessment-inline-hint">' + esc(tr('selectionHint')) + '</p></section></div>' +
                '<aside class="assessment-runtime-panel"><h3>' + esc(tr('runSettings')) + '</h3><label class="assessment-field"><span>' + esc(tr('model')) + '</span><select name="model">' + modelOptions(false) + '</select></label>' +
                '<label class="assessment-field"><span>' + esc(tr('approvalMode')) + '</span><select name="approval_mode"><option value="policy">' + esc(tr('approvalPolicyShort')) + '</option><option value="manual">' + esc(tr('approvalManualShort')) + '</option></select></label>' +
                '<p class="assessment-inline-hint">' + esc(tr('approvalPolicyHint')) + '</p><div class="assessment-form-grid">' + field('concurrency', 'number', 2, 'min="1" max="4" required') + field('runMinutes', 'number', 120, 'min="1" max="1440" required') + field('token_budget', 'number', 200000, 'min="4000" max="10000000" required') + field('max_rounds', 'number', 12, 'min="1" max="50" required') + '</div><p class="assessment-inline-hint">' + esc(tr('runtimeBudgetHint')) + '</p>' +
                '<details class="assessment-disclosure"><summary>' + esc(tr('advancedSettings')) + '</summary><div class="assessment-disclosure-body"><label class="assessment-field"><span>' + esc(tr('reviewModel')) + '</span><select name="reviewer_model">' + modelOptions(true) + '</select></label>' +
                ['general', 'recon', 'auth', 'validation', 'verification'].map(p => '<label class="assessment-field"><span>' + esc(tr('profiles.' + p)) + '</span><select name="worker_model_' + p + '">' + modelOptions(true) + '</select></label>').join('') +
                '<label class="assessment-field"><span>' + esc(tr('selectedSkills')) + '</span><select name="skill_ids" multiple size="4">' + (options.skills || []).map(id => '<option value="' + esc(id) + '">' + esc(id) + '</option>').join('') + '</select></label>' + (!(options.skills || []).length ? '<p class="assessment-inline-hint">' + esc(tr('noSkillsAvailable')) + '</p>' : '') + '</div></details></aside></div></div>' +
                '<footer class="assessment-dialog-footer"><div class="assessment-footer-copy"><p id="assessment-create-error" role="alert"></p><p>' + esc(tr('draftHint')) + '</p></div><div class="assessment-actions"><button type="button" class="btn-secondary" data-assessment-action="cancel-create">' + esc(tr('cancel')) + '</button><button class="btn-primary" type="submit">' + esc(tr('createDraft')) + '</button></div></footer></form>';
            document.body.appendChild(dialog);
            dialog.addEventListener('close', () => dialog.remove());
            dialog.addEventListener('click', onClick);
            const checkboxes = () => [...dialog.querySelectorAll('input[name=tools]')];
            const updateSelection = () => {
                const boxes = checkboxes(), count = boxes.filter(b => b.checked).length;
                dialog.querySelector('#assessment-tools-count').textContent = tr('selectedTools') + ' ' + count + ' / 100';
                boxes.forEach(b => b.closest('label').classList.toggle('selected', b.checked));
                const visible = boxes.filter(b => !b.closest('label').hidden);
                dialog.querySelector('[data-tool-selection=visible]').disabled = !visible.length || count + visible.filter(b => !b.checked).length > 100;
                dialog.querySelector('#assessment-tools-empty').hidden = visible.length > 0;
            };
            dialog.querySelector('#assessment-tools-search').addEventListener('input', ev => {
                const query = ev.target.value.trim().toLowerCase();
                dialog.querySelectorAll('.assessment-tool-option').forEach(row => { row.hidden = !row.dataset.toolSearch.includes(query); });
                updateSelection();
            });
            dialog.addEventListener('change', ev => {
                if (ev.target.name !== 'tools') return;
                if (checkboxes().filter(b => b.checked).length > 100) { ev.target.checked = false; dialog.querySelector('#assessment-create-error').textContent = tr('toolLimit'); }
                updateSelection();
            });
            dialog.querySelectorAll('[data-tool-selection]').forEach(btn => btn.addEventListener('click', () => {
                checkboxes().forEach(b => { if (btn.dataset.toolSelection === 'clear') b.checked = false; else if (!b.closest('label').hidden) b.checked = true; });
                updateSelection();
            }));
            dialog.querySelector('form').addEventListener('submit', async ev => {
                ev.preventDefault();
                const submit = dialog.querySelector('[type=submit]'), error = dialog.querySelector('#assessment-create-error');
                error.textContent = '';
                const data = new FormData(ev.target), body = Object.fromEntries(data.entries());
                body.tools = data.getAll('tools');
                if (!body.tools.length || body.tools.length > 100) { error.textContent = tr('chooseTools'); dialog.querySelector('#assessment-tools-search').focus(); return; }
                body.criteria = String(data.get('criteria') || '').split(/\n/).map(s => s.trim()).filter(Boolean);
                body.skill_ids = data.getAll('skill_ids');
                body.worker_models = {};
                ['general', 'recon', 'auth', 'validation', 'verification'].forEach(p => { const key = 'worker_model_' + p; if (data.get(key)) body.worker_models[p] = data.get(key); delete body[key]; });
                ['concurrency', 'token_budget', 'max_rounds'].forEach(key => body[key] = Number(body[key]));
                body.remaining_seconds = Number(body.runMinutes) * 60;
                delete body.runMinutes;
                submit.disabled = true;
                try {
                    const created = await api('', 'POST', body);
                    selected = created.assessment.id;
                    history.replaceState(null, '', '#assessments?task=' + encodeURIComponent(selected));
                    events = []; after = 0; task = created.assessment; tab = 'overview';
                    dialog.close();
                    await refresh(true);
                } catch (err) { error.textContent = err.message; submit.disabled = false; }
            });
            updateSelection();
            dialog.showModal();
        } finally { createDialogLoading = false; }
    }
    function budgetDialog() {
        if (!task || !can('assessment:write')) return;
        const id=task.id, dialog=document.createElement('dialog'); dialog.id='assessment-budget'; dialog.className='assessment-dialog';
        dialog.innerHTML='<form><h2>'+esc(tr('adjustBudget'))+'</h2><p>'+esc(tr('budgetHint'))+'</p>' + [['token_budget',task.token_budget,task.tokens+1000,10000000],['remaining_seconds',Math.max(task.remaining_seconds,3600),60,86400],['max_rounds',Math.min(50,Math.max(task.max_rounds,task.rounds+1)),task.rounds+1,50]].map(([name,value,min,max])=>'<label class="assessment-field">'+esc(tr(name))+'<input name="'+name+'" type="number" min="'+min+'" max="'+max+'" value="'+value+'" required></label>').join('')+'<p role="alert"></p><div class="assessment-actions">'+button('cancel-budget',tr('cancel'))+'<button class="btn-primary" type="submit">'+esc(tr('saveBudget'))+'</button></div></form>';
        dialog.addEventListener('click',onClick);dialog.querySelector('form').addEventListener('submit',async ev=>{ev.preventDefault();const btn=dialog.querySelector('[type=submit]');btn.disabled=true;try{const data=Object.fromEntries(new FormData(ev.target));Object.keys(data).forEach(k=>data[k]=Number(data[k]));await api('/'+id+'/control/budget','POST',data);dialog.remove();await refresh(true);}catch(err){dialog.querySelector('[role=alert]').textContent=err.message;btn.disabled=false;}});document.body.appendChild(dialog);dialog.showModal();
    }
    async function onClick(ev) {
        const target = ev.target.closest('[data-assessment-action]'); if (!target || target.disabled) return;
        const action = target.dataset.assessmentAction; const id = task && task.id === selected ? task.id : null;
        try {
            if (action === 'select') { epoch++; selected = target.dataset.id; history.replaceState(null, '', '#assessments?task=' + encodeURIComponent(selected)); task = null; events = []; after = 0; delete root().dataset.updated; detail(); await refresh(true); return; }
            if (action === 'tab') { tab = target.dataset.tab; detail(); return; }
            if (action === 'refresh') { await refresh(true); return; }
            if (action === 'new') { const label=target.textContent; target.disabled=true;target.textContent=tr('loadingOptions');try{await createDialog();}finally{target.textContent=label;target.disabled=false;}return; }
            if (action === 'cancel-budget') { document.getElementById('assessment-budget').remove(); return; }
            if (action === 'budget') { budgetDialog(); return; }
            if (action === 'cancel-create') { document.getElementById('assessment-create').close(); return; }
            if (action === 'next' || action === 'previous') { offset = Math.max(0, offset + (action === 'next' ? 50 : -50)); await refresh(true); return; }
            if (!id) return;
            target.disabled = true;
            if (['start', 'pause', 'stop'].includes(action)) await api('/' + id + '/control/' + action, 'POST', {});
            else if (action === 'steer') await api('/' + id + '/control/steer', 'POST', { message: document.getElementById('assessment-guidance').value });
            else if (action === 'approve' || action === 'deny') await api('/' + id + '/steps/' + target.dataset.step + '/decision', 'POST', { approved: action === 'approve', approval_id: target.dataset.approval });
            else if (action === 'retry') { if (!window.confirm(tr('retryConfirm'))) return; await api('/' + id + '/steps/' + target.dataset.step + '/retry', 'POST', {}); }
            else if (action === 'delete') { if (!window.confirm(tr('deleteConfirm'))) return; await api('/' + id, 'DELETE'); selected = ''; history.replaceState(null, '', '#assessments'); task = null; events = []; after = 0; }
            else if (action === 'fact' || action === 'vulnerability' || action === 'asset') { await api('/' + id + '/publish', 'POST', { step_id: target.dataset.step, index: Number(target.dataset.index), kind: action, revision: target.dataset.revision }); message(tr('published')); }
            else if (action === 'report' || action === 'metrics') {
                const response = await apiFetch('/api/assessments/' + id + '/' + action, { credentials: 'same-origin' }); if (!response.ok) throw new Error(response.statusText);
                const url = URL.createObjectURL(await response.blob()); const link = document.createElement('a'); link.href = url; link.download = 'assessment-' + id + (action === 'metrics' ? '-metrics.json' : '.md'); link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
            }
            await refresh(true);
        } catch (err) { message(err.message, true); }
        finally { target.disabled = false; }
    }
    window.initAssessmentsPage = function () {
        window.cleanupAssessmentsPage(); epoch++; const hash=window.location.hash.split('?'); if(hash[0]==='#assessments'){selected=new URLSearchParams(hash[1]||'').get('task')||selected;} if (!root()) return; shell(); root().addEventListener('click', onClick); refresh(true);
        timer = setInterval(() => { if (!document.hidden) refresh(false); }, 2500);
    };
    window.cleanupAssessmentsPage = function () { if (timer) clearInterval(timer); timer = null; epoch++; destroyGraph(); const dialog=document.getElementById('assessment-create'); if(dialog) dialog.remove(); const budget=document.getElementById('assessment-budget');if(budget)budget.remove(); if (root()) root().removeEventListener('click', onClick); };
})();
