// Conversation-scoped, read-only experience drafts. All model text uses textContent.
(function () {
    let dialog = null;
    let requestVersion = 0;
    let returnFocus = null;
    function tr(key) { return typeof window.t === 'function' ? window.t('learning.' + key) : key; }
    function element(tag, text, className) {
        const node = document.createElement(tag);
        if (text !== undefined) node.textContent = text;
        if (className) node.className = className;
        return node;
    }
    function close() {
        requestVersion++;
        if (dialog) { dialog.close(); dialog.remove(); dialog = null; }
        if (returnFocus && returnFocus.isConnected) returnFocus.focus();
    }
    window.openLearningDrafts = async function () {
        const cid = typeof currentConversationId === 'string' ? currentConversationId : '';
        if (!cid) return;
        close();
        returnFocus = document.activeElement;
        const version = ++requestVersion;
        dialog = element('dialog', undefined, 'learning-dialog');
        const header = element('div', undefined, 'learning-dialog-header');
        const title = element('h2', tr('title'));
        title.id = 'learning-dialog-title';
        dialog.setAttribute('aria-labelledby', title.id);
        const closeButton = element('button', tr('close'), 'btn-secondary');
        closeButton.type = 'button'; closeButton.onclick = close;
        header.append(title, closeButton);
        const content = element('div', tr('loading'), 'learning-dialog-content');
        content.setAttribute('aria-live', 'polite');
        dialog.append(header, element('p', tr('draftHint'), 'learning-hint'), content);
        dialog.addEventListener('cancel', event => { event.preventDefault(); close(); });
        document.body.append(dialog);
        dialog.showModal();
        try {
            const response = await apiFetch('/api/conversations/' + encodeURIComponent(cid) + '/learning');
            if (!response.ok) throw new Error('request failed');
            const data = await response.json();
            if (version !== requestVersion || !dialog) return;
            if (currentConversationId !== cid) { close(); return; }
            content.replaceChildren();
            if (!data.enabled) content.append(element('p', tr('disabled'), 'learning-hint'));
            if (!Array.isArray(data.jobs) || !data.jobs.length) content.append(element('p', tr('empty')));
            for (const job of data.jobs || []) {
                const card = element('article', undefined, 'learning-card');
                card.append(element('h3', tr('status.' + job.status)));
                card.append(element('p', tr('unknownOutcome'), 'learning-hint'));
                card.append(element('p', new Date(job.createdAt * 1000).toLocaleString()));
                if (job.reason) card.append(element('p', job.reason));
                if (job.candidateJson) {
                    let candidate;
                    try { candidate = JSON.parse(job.candidateJson); } catch (_) { candidate = null; }
                    if (candidate) {
                        card.append(element('h4', candidate.name));
                        card.append(element('p', candidate.description));
                        card.append(element('pre', candidate.skillMarkdown, 'learning-skill'));
                        card.append(element('p', tr('evidence') + ': ' + (candidate.evidenceIds || []).join(', '), 'learning-evidence'));
                    }
                }
                content.append(card);
            }
            const refresh = element('button', tr('refresh'), 'btn-secondary');
            refresh.type = 'button';
            refresh.onclick = () => { if (currentConversationId === cid) window.openLearningDrafts(); else close(); };
            content.append(refresh);
        } catch (_) {
            if (version === requestVersion && dialog) content.textContent = tr('loadFailed');
        }
    };
}());
