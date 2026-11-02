/* Durable assessment workbench. This module never calls the chat API. */
(function () {
    'use strict';
    let selected = '', tab = 'overview', timer = null, offset = 0, epoch = 0, loading = false;
    let task = null, rows = [], events = [], after = 0, options = { models: [], tools: [] }, graph = null;
    const root = () => document.getElementById('assessment-workbench');
    const tr = (key) => typeof window.t === 'function' ? window.t('assessments.' + key) : key;
    const esc = (v) => String(v == null ? '' : v).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    const can = (p) => typeof window.hasPermission === 'function' && window.hasPermission(p);
    const badge = (s) => '<span class="assessment-status" data-status="' + esc(s) + '">' + esc(tr('status.' + s)) + '</span>';
    const button = (action, text, permission, attrs) => '<button type="button" class="btn-secondary btn-small" data-assessment-action="' + action + '" ' + (attrs || '') + (permission && !can(permission) ? ' disabled' : '') + '>' + esc(text) + '</button>';
    async function api(path, method, body) {
        const response = await fetch('/api/assessments' + path, { method: method || 'GET', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || response.statusText);
        return data;
    }
    function message(text, error) {
        const el = document.getElementById('assessment-message');
        if (el) { el.textContent = text; el.classList.toggle('is-error', !!error); }
    }
    function destroyGraph() { if (graph) { graph.destroy(); graph = null; } }
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
        destroyGraph();
        const el = document.getElementById('assessment-detail'); if (!el) return;
        if (!task) { el.innerHTML = '<div class="assessment-empty"><h2>' + esc(tr('welcome')) + '</h2><p>' + esc(tr('welcomeBody')) + '</p></div>'; return; }
        const active = ['running', 'awaiting_approval', 'pausing', 'stopping'].includes(task.status);
        const resumable = ['draft', 'paused', 'blocked'].includes(task.status);
        const tools = task.tools.join(', ');
        let controls = '';
        if (resumable && can('agent:execute')) controls += button('start', tr(task.status === 'draft' ? 'start' : 'resume'), 'assessment:write');
        if (['running', 'awaiting_approval'].includes(task.status)) controls += button('pause', tr('pause'), 'assessment:write');
        if (!['completed', 'stopped', 'exhausted', 'failed'].includes(task.status)) controls += button('stop', tr('stop'), 'assessment:write');
        controls += button('report', tr('report'), 'assessment:read');
        if (!active) controls += button('delete', tr('delete'), 'assessment:delete');
        el.innerHTML = '<div class="assessment-detail-heading"><div><h2>' + esc(task.name) + '</h2>' + badge(task.status) + '</div><div class="assessment-actions">' + controls + '</div></div>' +
            '<div class="assessment-metrics"><div><strong>' + task.steps.filter(s => s.status === 'completed').length + ' / ' + task.steps.length + '</strong><span>' + esc(tr('steps')) + '</span></div><div><strong>' + task.tokens.toLocaleString() + ' / ' + task.token_budget.toLocaleString() + '</strong><span>' + esc(tr('tokens')) + '</span></div><div><strong>' + task.rounds + ' / ' + task.max_rounds + '</strong><span>' + esc(tr('rounds')) + '</span></div><div><strong>' + Math.max(0, Math.ceil((task.deadline ? task.deadline - Date.now() / 1000 : task.remaining_seconds) / 60)) + '</strong><span>' + esc(tr('minutes')) + '</span></div></div>' +
            '<div class="assessment-tabs" role="tablist">' + ['overview', 'plan', 'chain', 'evidence', 'logs'].map(id => '<button role="tab" aria-selected="' + (tab === id) + '" class="' + (tab === id ? 'active' : '') + '" data-assessment-action="tab" data-tab="' + id + '">' + esc(tr(id)) + '</button>').join('') + '</div><div id="assessment-tab" role="tabpanel"></div>';
        const body = document.getElementById('assessment-tab');
        if (tab === 'overview') {
            body.innerHTML = ['goal', 'scope', 'material', 'summary'].map(key => '<section class="assessment-card"><h3>' + esc(tr(key)) + '</h3><div class="assessment-text">' + esc(task[key] || tr('none')) + '</div></section>').join('') +
                '<p class="assessment-text">' + esc(tr('reason')) + ': ' + esc(task.reason || '—') + '\n' + esc(tr('model')) + ': ' + esc(task.model || tr('default')) + '\n' + esc(tr('tools')) + ': ' + esc(tools) + '\n' + esc(tr('project')) + ': ' + esc(task.project_id || '—') + '</p>' +
                (resumable ? '<label class="assessment-field">' + esc(tr('guidance')) + '<textarea id="assessment-guidance" maxlength="8000" rows="3"></textarea></label>' + button('steer', tr('saveGuidance'), 'assessment:write') : '');
        } else if (tab === 'plan') {
            body.innerHTML = task.steps.map(s => '<article class="assessment-card"><div class="assessment-step-title"><h3>' + esc(s.title) + '</h3>' + badge(s.status) + '</div><small>' + esc(s.key) + (s.depends_on && s.depends_on.length ? ' ← ' + esc(s.depends_on.join(', ')) : '') + '</small><p class="assessment-text">' + esc(s.instruction) + '</p><p class="assessment-text">' + esc(s.result) + '</p>' +
                (s.pending ? '<div class="assessment-approval"><h4>' + esc(tr('approval')) + '</h4><p>' + esc(tr('approvalHint')) + '</p><strong>' + esc(s.pending.function.name) + '</strong><pre>' + esc(JSON.stringify(s.pending.function.arguments, null, 2)) + '</pre>' +
                    (s.status === 'awaiting_approval' && !['completed', 'stopped', 'exhausted', 'failed'].includes(task.status) ? button('approve', tr('approve'), 'hitl:write', 'data-step="' + esc(s.id) + '"' + (!can('assessment:write') ? ' disabled' : '')) + button('deny', tr('deny'), 'hitl:write', 'data-step="' + esc(s.id) + '"' + (!can('assessment:write') ? ' disabled' : '')) : '<p>' + esc(tr('approved')) + '</p>') + '</div>' : '') +
                (s.status === 'blocked' && resumable ? button('retry', tr('retry'), 'assessment:write', 'data-step="' + esc(s.id) + '"') : '') + '</article>').join('') || '<p class="assessment-empty">' + esc(tr('noSteps')) + '</p>';
        } else if (tab === 'evidence') {
            body.innerHTML = '<p>' + esc(tr('candidateHint')) + '</p>' + task.steps.map(s => (s.evidence || []).map((v, i) => '<article class="assessment-card"><small>' + esc(s.title) + ' · ' + esc(v.kind) + '</small><h3>' + esc(v.title) + '</h3><p class="assessment-text">' + esc(v.detail) + '</p><p class="assessment-text">' + esc((v.execution_ids || []).join('\n')) + '</p>' +
                (v.kind !== 'gap' && v.execution_ids.length ? (task.project_id ? button('fact', tr('publishFact'), 'project:write', 'data-step="' + esc(s.id) + '" data-index="' + i + '"') : '') + (v.kind === 'asset' && v.asset ? button('asset', tr('publishAsset'), 'asset:write', 'data-step="' + esc(s.id) + '" data-index="' + i + '"') : '') + (v.kind === 'finding' ? button('vulnerability', tr('publishFinding'), 'vulnerability:write', 'data-step="' + esc(s.id) + '" data-index="' + i + '"') : '') : '') + '</article>').join('')).join('');
        } else if (tab === 'logs') {
            body.innerHTML = '<div class="assessment-log">' + events.map(ev => '<details><summary><time>' + esc(ev.created_at) + '</time> · ' + esc(ev.kind) + ' · ' + esc(ev.message.slice(0, 100)) + '</summary><pre>' + esc(ev.message) + '\n' + esc(ev.data ? JSON.stringify(ev.data, null, 2) : '') + '</pre></details>').join('') + '</div>';
        } else if (tab === 'chain') {
            body.innerHTML = '<p>' + esc(tr('chainHint')) + '</p><div id="assessment-graph" aria-label="' + esc(tr('chain')) + '"></div><p id="assessment-graph-selection" class="assessment-text"></p>';
            renderGraph();
        }
    }
    function renderGraph() {
        const container = document.getElementById('assessment-graph');
        if (!window.cytoscape || !container) return;
        const elements = [{ data: { id: 'goal', label: task.goal.slice(0, 80), kind: 'goal' } }];
        const byKey = Object.fromEntries(task.steps.map(s => [s.key, s.id]));
        task.steps.forEach(s => {
            elements.push({ data: { id: s.id, label: s.title + '\n' + tr('status.' + s.status), kind: s.status, detail: s.instruction + '\n\n' + s.result } });
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
        } catch (err) { if (version === epoch) message(err.message, true); }
        finally { loading = false; }
    }
    async function createDialog() {
        options = await api('/options');
        const prior = document.getElementById('assessment-create'); if (prior) prior.remove();
        const dialog = document.createElement('dialog'); dialog.id = 'assessment-create'; dialog.className = 'assessment-dialog';
        const field = (name, type, value, extra) => '<label class="assessment-field">' + esc(tr(name)) + '<input name="' + name + '" type="' + type + '" value="' + esc(value || '') + '" ' + (extra || '') + '></label>';
        dialog.innerHTML = '<form id="assessment-create-form"><h2>' + esc(tr('new')) + '</h2>' + field('name', 'text', '', 'required maxlength="200"') +
            ['goal', 'scope', 'material'].map(name => '<label class="assessment-field">' + esc(tr(name)) + '<textarea name="' + name + '" rows="3" maxlength="' + (name === 'material' ? '24000' : '12000') + '"' + (name !== 'material' ? ' required' : '') + '></textarea></label>').join('') +
            '<div class="assessment-form-grid">' + field('project_id', 'text', '', 'maxlength="64"') + '<label class="assessment-field">' + esc(tr('model')) + '<select name="model">' + options.models.map(m => '<option value="' + esc(m.id) + '"' + (m.default ? ' selected' : '') + '>' + esc((m.name || m.id || tr('default')) + ' · ' + m.model) + '</option>').join('') + '</select></label>' +
            '<label class="assessment-field">' + esc(tr('approvalMode')) + '<select name="approval_mode"><option value="policy">' + esc(tr('approvalPolicy')) + '</option><option value="manual">' + esc(tr('approvalManual')) + '</option></select></label>' + field('concurrency', 'number', 2, 'min="1" max="4" required') + field('token_budget', 'number', 200000, 'min="4000" max="10000000" required') + field('duration', 'number', 120, 'min="1" max="1440" required') + field('max_rounds', 'number', 12, 'min="1" max="50" required') + '</div>' +
            '<fieldset><legend>' + esc(tr('tools')) + '</legend><p>' + esc(tr('toolsHint')) + '</p><div class="assessment-tool-picker">' + options.tools.map(tool => '<label><input type="checkbox" name="tools" value="' + esc(tool.name) + '"><span><strong>' + esc(tool.name) + '</strong><small>' + esc(tool.description) + '</small></span></label>').join('') + '</div></fieldset><p id="assessment-create-error" role="alert"></p><div class="assessment-actions"><button type="button" class="btn-secondary" data-assessment-action="cancel-create">' + esc(tr('cancel')) + '</button><button class="btn-primary" type="submit">' + esc(tr('createDraft')) + '</button></div></form>';
        document.body.appendChild(dialog); dialog.addEventListener('click', onClick);
        dialog.querySelector('form').addEventListener('submit', async ev => {
            ev.preventDefault(); const submit = dialog.querySelector('[type=submit]'); submit.disabled = true;
            const data = new FormData(ev.target), body = Object.fromEntries(data.entries());
            body.tools = data.getAll('tools'); ['concurrency', 'token_budget', 'max_rounds'].forEach(key => body[key] = Number(body[key])); body.remaining_seconds = Number(body.duration) * 60; delete body.duration;
            try { const created = await api('', 'POST', body); selected = created.assessment.id; events = []; after = 0; task = created.assessment; dialog.close(); dialog.remove(); tab = 'overview'; await refresh(true); }
            catch (err) { dialog.querySelector('#assessment-create-error').textContent = err.message; submit.disabled = false; }
        });
        dialog.showModal();
    }
    async function onClick(ev) {
        const target = ev.target.closest('[data-assessment-action]'); if (!target || target.disabled) return;
        const action = target.dataset.assessmentAction; const id = selected;
        try {
            if (action === 'select') { epoch++; selected = target.dataset.id; history.replaceState(null, '', '#assessments?task=' + encodeURIComponent(selected)); task = null; events = []; after = 0; delete root().dataset.updated; await refresh(true); return; }
            if (action === 'tab') { tab = target.dataset.tab; detail(); return; }
            if (action === 'refresh') { await refresh(true); return; }
            if (action === 'new') { await createDialog(); return; }
            if (action === 'cancel-create') { document.getElementById('assessment-create').close(); return; }
            if (action === 'next' || action === 'previous') { offset = Math.max(0, offset + (action === 'next' ? 50 : -50)); await refresh(true); return; }
            if (!id) return;
            target.disabled = true;
            if (['start', 'pause', 'stop'].includes(action)) await api('/' + id + '/control/' + action, 'POST', {});
            else if (action === 'steer') await api('/' + id + '/control/steer', 'POST', { message: document.getElementById('assessment-guidance').value });
            else if (action === 'approve' || action === 'deny') await api('/' + id + '/steps/' + target.dataset.step + '/decision', 'POST', { approved: action === 'approve' });
            else if (action === 'retry') { if (!window.confirm(tr('retryConfirm'))) return; await api('/' + id + '/steps/' + target.dataset.step + '/retry', 'POST', {}); }
            else if (action === 'delete') { if (!window.confirm(tr('deleteConfirm'))) return; await api('/' + id, 'DELETE'); selected = ''; task = null; events = []; after = 0; }
            else if (action === 'fact' || action === 'vulnerability' || action === 'asset') { await api('/' + id + '/publish', 'POST', { step_id: target.dataset.step, index: Number(target.dataset.index), kind: action }); message(tr('published')); }
            else if (action === 'report') {
                const response = await fetch('/api/assessments/' + id + '/report', { credentials: 'same-origin' }); if (!response.ok) throw new Error(response.statusText);
                const url = URL.createObjectURL(await response.blob()); const link = document.createElement('a'); link.href = url; link.download = 'assessment-' + id + '.md'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
            }
            await refresh(true);
        } catch (err) { message(err.message, true); }
        finally { target.disabled = false; }
    }
    window.initAssessmentsPage = function () {
        window.cleanupAssessmentsPage(); epoch++; const hash=window.location.hash.split('?'); if(hash[0]==='#assessments'){selected=new URLSearchParams(hash[1]||'').get('task')||selected;} if (!root()) return; shell(); root().addEventListener('click', onClick); refresh(true);
        timer = setInterval(() => { if (!document.hidden) refresh(false); }, 2500);
    };
    window.cleanupAssessmentsPage = function () { if (timer) clearInterval(timer); timer = null; epoch++; destroyGraph(); const dialog=document.getElementById('assessment-create'); if(dialog) dialog.remove(); if (root()) root().removeEventListener('click', onClick); };
})();
