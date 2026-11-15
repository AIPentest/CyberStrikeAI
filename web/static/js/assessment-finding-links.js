/* Navigation uses stable assessment/step IDs; names and tags are display only. */
(function () {
    'use strict';
    const labels = { reviews: '复现与独立复核', reported: '已登记，待独立复核', confirmed: '独立复核确认', refuted: '独立复核否定', inconclusive: '独立复核未定', reviewEvidence: '查看复核意图与回执', source: '来源深度测评', step: '来源步骤', openStep: '返回测评步骤', evidence: '查看步骤与复现证据', deleted: '测评已删除，漏洞记录仍保留', unavailable: '当前账号不能打开关联测评' };
    const t = key => { const path = 'assessmentFindingLinks.' + key; const text = typeof window.t === 'function' ? window.t(path) : ''; return text && text !== path ? text : labels[key]; };
    const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
    function href(v, tab = 'plan') {
        return '#deep-assessments?' + new URLSearchParams({ assessment_id: v.assessment_id, step_id: v.assessment_step_id || '', tab, vulnerability_id: v.id });
    }
    function reviews(v) {
        if (!v.assessment_reviews?.length) return '';
        return '<section class="da-review-records"><h4>' + escape(t('reviews')) + '</h4>' + v.assessment_reviews.map(r => {
            const target = '#deep-assessments?' + new URLSearchParams({assessment_id:v.assessment_id,step_id:r.step_id,tab:'evidence'});
            return '<details data-da-detail="review:' + escape(r.id || [r.step_id,r.verdict,r.created_at].join(':')) + '"><summary>' + escape(t(r.verdict)) + ' · ' + escape(r.step_id) + '</summary><pre>' + escape(r.reason) + '</pre><code>' + escape((r.evidence || []).join(', ')) + '</code>' + (v.assessment_can_open && !v.assessment_deleted ? '<p><a data-assessment-jump href="' + escape(target) + '">' + escape(t('reviewEvidence')) + '</a></p>' : '') + '</details>';
        }).join('') + '</section>';
    }
    function render(v) {
        if (!v.assessment_id) return '';
        const links = v.assessment_can_open && !v.assessment_deleted
            ? `<div class="vulnerability-inspector-actions"><a class="btn-secondary" data-assessment-jump href="${escape(href(v))}">${escape(t('openStep'))}</a><a class="btn-secondary" data-assessment-jump href="${escape(href(v, 'evidence'))}">${escape(t('evidence'))}</a></div>`
            : `<p class="da-muted">${escape(t(v.assessment_deleted ? 'deleted' : 'unavailable'))}</p>`;
        return `<section class="vulnerability-assessment-source"><strong>${escape(t('source'))}：${escape(v.assessment_name || v.assessment_id)}</strong><p>${escape(t('step'))}：${escape(v.assessment_step_title || v.assessment_step_id)}</p>${links}${reviews(v)}</section>`;
    }
    function openFinding(id, assessmentID = '') {
        const params = new URLSearchParams({ view: 'task' });
        if (id) params.set('id', id);
        if (assessmentID) params.set('assessment_id', assessmentID);
        const alreadyOpen = document.getElementById('page-vulnerabilities')?.classList.contains('active');
        history.replaceState(null, '', '#vulnerabilities?' + params);
        if (typeof window.switchPage === 'function') window.switchPage('vulnerabilities');
        // New-page initialization loads once and opens the exact finding. When
        // already on this page, update its filters and let that same path run.
        if (alreadyOpen) {
            if (typeof window.syncVulnerabilityFiltersFromLocationHash === 'function') window.syncVulnerabilityFiltersFromLocationHash();
            if (typeof window.refreshVulnerabilities === 'function') window.refreshVulnerabilities();
        }
    }
    document.addEventListener('click', event => {
        const link = event.target.closest?.('a[data-assessment-jump]');
        if (!link || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        if (typeof window.closeVulnerabilityInspectorForNavigation === 'function') window.closeVulnerabilityInspectorForNavigation();
        else if (typeof window.closeAppModal === 'function') window.closeAppModal('vulnerability-inspector');
        history.replaceState(null, '', link.hash);
        const alreadyOpen = document.getElementById('page-deep-assessments')?.classList.contains('active');
        if (typeof window.switchPage === 'function') window.switchPage('deep-assessments');
        if (alreadyOpen) window.DeepAssessments?.fromLocation();
    });
    window.AssessmentFindingLinks = { href, render, reviews, openFinding };
})();
