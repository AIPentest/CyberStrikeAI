/* Human evidence mapping for an existing assessment. */
(function () {
    'use strict';
    const labels = {
        title:'验收证据', evidencePath:'查看局部证据图', has_evidence:'已有证据', missing:'缺证据', needs_review:'待人工复核',
        all:'全部验收项', hint:'选择证据节点可回看来源。状态反映证据关联情况，验收结论需人工核对。',
        criterion:'验收项', fact:'事实', receipt:'原始回执', direct:'直接关联',
        edit:'编辑证据关联', noRows:'没有符合当前筛选的验收项', noEvidence:'尚无可用的关联证据',
        noReceipts:'暂无可读回执', observed:'直接观察', inferred:'推断', unavailable:'来源已不可用',
        note:'复核说明', flag:'标记为待人工复核', find:'搜索证据', placeholder:'按名称、来源步骤或 ID 搜索',
        noSources:'没有匹配的证据记录', selected:'已选证据', saved:'已保存验收证据',
        save:'保存关联', close:'取消', next:'下一页', previous:'上一页', loading:'正在读取证据',
        criteriaHint:'建议每行填写一条验收条件。留空时沿用任务目标。',
        maxSources:'每种证据最多选择 32 条', stale:'该验收项已更新。关闭并重新打开可查看最新关联。',
        completed:'已完成', failed:'失败', uncertain:'结果待核对', running:'处理中',
        cancelled:'已取消', reconciled:'已核对', interrupted:'已中断', error:'执行失败'
    };
    const text = key => {
        const path = 'deepAssessments.acceptance.' + key;
        const translated = window.t?.(path);
        return translated && translated !== path ? translated : labels[key] || key;
    };
    const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'})[c]);
    const button = (action, label, attrs='') => `<button type="button" class="btn-secondary" data-da-action="${action}" ${attrs}>${escape(label)}</button>`;
    let taskID = '', filter = '';
    function reset(id) {
        if (taskID === id) return;
        taskID = id;
        filter = '';
    }
    function setFilter(value) { filter = value; }

    function sourceNode(source, kind, criterion) {
        const action = kind === 'receipt' ? 'acceptance-receipt' : 'graph-open';
        return `<button type="button" class="da-acceptance-node da-acceptance-${kind}" data-da-action="${action}" data-criterion="${criterion}" data-kind="${kind}" data-node="${escape(source.id)}"><small>${escape(text(kind))} · ${escape(text(source.state))}</small><strong>${escape(source.title)}</strong></button>`;
    }
    const arrow = '<span class="da-acceptance-arrow" aria-hidden="true"></span>';

    function paths(row) {
        const receipts = new Map((row.receipts || []).map(r => [r.id, r]));
        const lines = (row.facts || []).map(fact => {
            const support = (fact.receipt_ids || []).map(id => receipts.get(id)).filter(Boolean);
            const targets = support.length ? support.map(r => sourceNode(r, 'receipt', row.number)).join('') : `<span class="da-acceptance-blank">${escape(text('noReceipts'))}</span>`;
            return `<div class="da-acceptance-path">${sourceNode(fact, 'fact', row.number)}${arrow}<div class="da-acceptance-branch">${targets}</div></div>`;
        });
        const direct = (row.receipt_ids || []).map(id => receipts.get(id)).filter(Boolean);
        if (direct.length) {
            lines.push(`<div class="da-acceptance-path"><span class="da-acceptance-blank">${escape(text('direct'))}</span>${arrow}<div class="da-acceptance-branch">${direct.map(r => sourceNode(r, 'receipt', row.number)).join('')}</div></div>`);
        }
        return lines.join('') || `<p class="da-acceptance-blank">${escape(text('noEvidence'))}</p>`;
    }

    function render(body, rows, writable, focus) {
        if (!rows) {
            body.innerHTML = `<p class="da-muted">${escape(text('loading'))}</p>`;
            return;
        }
        const counts = {has_evidence:0, missing:0, needs_review:0};
        rows.forEach(row => counts[row.status]++);
        const cards = rows.filter(row => !filter || row.status === filter).map(row => `<article class="da-acceptance-row" data-acceptance-criterion="${row.number}" tabindex="-1" ${row.number===focus?'data-focused':''}>
            <div class="da-acceptance-condition">
                <div class="da-acceptance-heading"><h4>${escape(text('criterion'))} ${row.number}</h4><span class="da-acceptance-status da-acceptance-tone-${escape(row.status)}">${escape(text(row.status))}</span></div>
                <p class="da-acceptance-text">${escape(row.text)}</p>
                ${row.review_note?`<p class="da-acceptance-note"><strong>${escape(text('note'))}</strong> ${escape(row.review_note)}</p>`:''}
                <div class="da-controls">${button('provenance-path',text('evidencePath'),`data-criterion="${row.number}"`)}
                ${writable?button('acceptance-edit',text('edit'),`data-number="${row.number}"`):''}</div>
            </div>
            ${arrow}<div class="da-acceptance-evidence">${paths(row)}</div>
        </article>`).join('');
        body.innerHTML = `<div class="da-acceptance-toolbar"><div class="da-controls" role="group" aria-label="${escape(text('title'))}">
            ${['', 'has_evidence', 'missing', 'needs_review'].map(status => button('acceptance-filter',text(status || 'all')+' · '+(status?counts[status]:rows.length),`data-status="${status}" aria-pressed="${String(filter===status)}"`)).join('')}
        </div><p class="da-muted">${escape(text('hint'))}</p></div>
        <div class="da-acceptance-canvas">${cards || `<p class="da-acceptance-empty">${escape(text('noRows'))}</p>`}</div>`;
    }

    function editor(row, id, json, show, close, saved) {
        show(text('edit') + ' · ' + text('criterion') + ' ' + row.number, `<form id="da-acceptance-form" class="da-drawer-form">
            <fieldset class="da-form-body">
                <p class="da-acceptance-text">${escape(row.text)}</p>
                <div class="da-controls" role="group" aria-label="${escape(text('find'))}">
                    <button type="button" class="btn-secondary" data-source-kind="fact">${escape(text('fact'))}</button>
                    <button type="button" class="btn-secondary" data-source-kind="receipt">${escape(text('receipt'))}</button>
                </div>
                <div class="da-acceptance-search"><input class="form-control" type="search" maxlength="300" aria-label="${escape(text('find'))}" placeholder="${escape(text('placeholder'))}"><button type="button" class="btn-secondary" data-source-search>${escape(text('find'))}</button></div>
                <div data-source-results></div>
                <div class="da-acceptance-selection"><strong>${escape(text('selected'))}</strong><div data-source-selected></div></div>
                <label class="da-acceptance-check"><input type="checkbox" name="review" ${row.needs_review?'checked':''}> ${escape(text('flag'))}</label>
                <label>${escape(text('note'))}<textarea name="note" class="form-control" rows="3" maxlength="2000">${escape(row.review_note)}</textarea></label>
                <p class="da-acceptance-error" data-source-error role="alert" hidden></p>
            </fieldset>
            <footer class="da-drawer-footer"><button type="submit" class="btn-primary">${escape(text('save'))}</button>${button('close',text('close'))}</footer>
        </form>`);
        const form = document.getElementById('da-acceptance-form');
        const notice = form.querySelector('[data-source-error]');
        const input = form.querySelector('input[type=search]');
        const results = form.querySelector('[data-source-results]');
        const selected = {
            fact:new Map((row.fact_ids || []).map(id => [id, (row.facts || []).find(item => item.id === id)?.title || text('unavailable') + ' · ' + id])),
            receipt:new Map((row.receipt_ids || []).map(id => [id, (row.receipts || []).find(item => item.id === id)?.title || text('unavailable') + ' · ' + id]))
        };
        const base = '/' + encodeURIComponent(id) + '/acceptance';
        const open = () => form.isConnected && form.closest('dialog').open;
        let kind = 'fact', search = '', offset = 0, request = 0, sources = [], more = false, saving = false;
        function message(value) {
            notice.textContent = value;
            notice.hidden = !value;
        }
        function selection() {
            form.querySelector('[data-source-selected]').innerHTML = Object.entries(selected).flatMap(([type, items]) =>
                [...items].map(([id, title]) => `<button type="button" class="btn-secondary" data-remove-kind="${type}" data-remove-id="${escape(id)}">${escape(text(type) + ' · ' + title)} ×</button>`)
            ).join('') || '<span class="da-muted">—</span>';
        }
        function list() {
            results.innerHTML = `<div class="da-acceptance-options">${sources.map(item => `<label class="da-acceptance-option">
                <input type="checkbox" data-source-id="${escape(item.id)}" ${selected[kind].has(item.id)?'checked':''}>
                <span><strong>${escape(item.title)}</strong><small>${escape(text(item.state)+' · '+item.step_id+' · '+item.id)}</small></span>
            </label>`).join('') || `<p class="da-muted">${escape(text('noSources'))}</p>`}</div>
            <div class="da-controls">${offset?`<button type="button" class="btn-secondary" data-source-page="-20">${escape(text('previous'))}</button>`:''}${more?`<button type="button" class="btn-secondary" data-source-page="20">${escape(text('next'))}</button>`:''}</div>`;
        }
        async function load() {
            const current = ++request;
            form.querySelectorAll('[data-source-kind]').forEach(el => el.setAttribute('aria-pressed',String(el.dataset.sourceKind===kind)));
            results.textContent = text('loading');
            try {
                const page = await json(base+'/sources?'+new URLSearchParams({kind, q:search, offset}));
                if (!open() || request !== current) return;
                sources = page.sources;
                more = page.has_more;
                list();
            } catch (error) {
                if (open() && request===current) { results.textContent=''; message(error.message); }
            }
        }
        form.addEventListener('click', event => {
            const target=event.target.closest('button');
            if (!target || saving) return;
            const data=target.dataset;
            if (data.removeKind) {
                selected[data.removeKind].delete(data.removeId);
                const option = [...results.querySelectorAll('[data-source-id]')].find(el => kind===data.removeKind && el.dataset.sourceId===data.removeId);
                if (option) option.checked=false;
                selection();
            } else if (data.sourceKind) {
                kind=data.sourceKind;
                offset=0;
                message('');
                void load();
            } else if (data.sourcePage) {
                offset=Math.max(0,offset+Number(data.sourcePage));
                void load();
            } else if ('sourceSearch' in data) {
                search=input.value.trim();
                offset=0;
                void load();
            }
        });
        input.addEventListener('keydown', event => {
            if (event.key==='Enter') {
                event.preventDefault();
                form.querySelector('[data-source-search]').click();
            }
        });
        form.addEventListener('change', event => {
            if (!event.target.dataset.sourceId) return;
            const id=event.target.dataset.sourceId;
            if (event.target.checked && selected[kind].size>=32) {
                event.target.checked=false;
                message(text('maxSources'));
                return;
            }
            if (event.target.checked) selected[kind].set(id, sources.find(item => item.id===id)?.title || id);
            else selected[kind].delete(id);
            message('');
            selection();
        });
        form.addEventListener('submit', async event => {
            event.preventDefault();
            if (saving) return;
            saving=true;
            const data = {
                revision:row.revision, fact_ids:[...selected.fact.keys()], receipt_ids:[...selected.receipt.keys()],
                needs_review:form.elements.review.checked, review_note:form.elements.note.value
            };
            const submit=form.querySelector('[type=submit]');
            submit.disabled=true;
            form.querySelector('fieldset').disabled=true;
            message('');
            try {
                await json(base+'/'+row.number,{method:'PUT',body:JSON.stringify(data)});
                if (open()) close();
                saved(id);
            } catch (error) {
                if (open()) message(error.status===409?text('stale'):error.message);
            } finally {
                saving=false;
                submit.disabled=false;
                form.querySelector('fieldset').disabled=false;
            }
        });
        selection();
        void load();
    }

    window.AssessmentAcceptance = {reset,setFilter,render,editor,text};
})();
