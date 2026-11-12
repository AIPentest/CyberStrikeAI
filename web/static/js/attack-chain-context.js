// Compressed context remains separate from attack nodes and original user authorization.
(function () {
    'use strict';

    let labels = [];

    function translate(key, values) {
        if (typeof window.t === 'function') return window.t('attackChainModal.' + key, values);
        const fallback = {
            contextTitle: '压缩摘要与证据线索 ({{count}})',
            contextNotice: '当前会话轨迹中的系统生成摘要，可能晚于图的生成时间。仅供回溯与检索，不代表用户的新授权或已核实工具证据。可按摘要内的快照路径读取原始证据。',
            contextSummary: '压缩摘要 {{number}}',
            contextCopy: '复制完整摘要',
            contextCopied: '已复制',
            contextCopyFailed: '复制失败，可选中文本手动复制。'
        };
        let text = fallback[key] || key;
        Object.entries(values || {}).forEach(([name, value]) => {
            text = text.replace('{{' + name + '}}', String(value));
        });
        return text;
    }

    function label(element, key, values) {
        const refresh = () => { element.textContent = translate(key, values); };
        labels.push(refresh);
        refresh();
    }

    function clear() {
        labels = [];
        const card = document.getElementById('attack-chain-context');
        const contents = document.getElementById('attack-chain-context-content');
        if (contents) contents.replaceChildren();
        if (card) {
            card.hidden = true;
            card.open = false;
        }
    }

    async function copyText(text) {
        if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
            try {
                await navigator.clipboard.writeText(text);
                return;
            } catch (_) {
                // Plain HTTP deployments may deny clipboard access; retain manual-copy fallback.
            }
        }
        const previousFocus = document.activeElement;
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.readOnly = true;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        try {
            textarea.focus();
            textarea.select();
            if (!document.execCommand('copy')) throw new Error('copy failed');
        } finally {
            textarea.remove();
            if (previousFocus && typeof previousFocus.focus === 'function') {
                previousFocus.focus({ preventScroll: true });
            }
        }
    }

    function render(chainData) {
        const card = document.getElementById('attack-chain-context');
        // A 409 refresh re-renders the same conversation; keep the card's open state.
        const wasOpen = !!card && card.open;
        clear();
        const contents = document.getElementById('attack-chain-context-content');
        if (!card || !contents) return;

        // Only explicit server provenance qualifies; summary-looking user text stays user text.
        const summaries = Array.isArray(chainData && chainData.context_summaries)
            ? chainData.context_summaries.filter(item => item &&
                item.provenance === 'system_generated_summary' &&
                typeof item.content === 'string' && item.content.trim())
            : [];
        if (!summaries.length) return;

        label(document.getElementById('attack-chain-context-title'), 'contextTitle', { count: summaries.length });
        label(document.getElementById('attack-chain-context-notice'), 'contextNotice');
        summaries.forEach((item, index) => {
            const entry = document.createElement('details');
            entry.className = 'attack-chain-context-entry';
            const heading = document.createElement('summary');
            label(heading, 'contextSummary', { number: index + 1 });
            const body = document.createElement('pre');
            body.className = 'attack-chain-context-body';
            // Do not render Markdown/HTML or turn evidence paths into clickable URLs.
            body.textContent = item.content;
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'btn-secondary attack-chain-context-copy';
            label(button, 'contextCopy');
            const status = document.createElement('span');
            status.className = 'attack-chain-context-copy-status';
            status.setAttribute('role', 'status');
            let statusKey = '';
            labels.push(() => { status.textContent = statusKey ? translate(statusKey) : ''; });
            button.addEventListener('click', async () => {
                button.disabled = true;
                status.textContent = '';
                try {
                    await copyText(item.content);
                    statusKey = 'contextCopied';
                } catch (_) {
                    statusKey = 'contextCopyFailed';
                } finally {
                    status.textContent = translate(statusKey);
                    button.disabled = false;
                }
            });
            entry.append(heading, body, button, status);
            contents.appendChild(entry);
        });
        card.hidden = false;
        card.open = wasOpen;
    }

    document.addEventListener('languagechange', () => labels.forEach(refresh => refresh()));
    window.CyberStrikeAttackChainContext = { render, clear };
})();
