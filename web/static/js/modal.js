/**
 * Shared dialog/drawer lifecycle: presentation, stacking, keyboard focus and deferred rendering.
 */
(function () {
    const BODY_LOCK = 'app-modal-open';
    const LEGACY_BODY_LOCK = 'projects-modal-open';
    const OVERLAY_SELECTOR =
        '.projects-modal-overlay, .c2-modal-overlay, .modal, .info-collect-cell-modal, #login-overlay';

    const FLEX_MODAL_IDS = new Set([
        'role-modal',
        'skill-modal',
        'agent-md-modal',
        'batch-manage-modal',
        'workflow-meta-modal',
        'workflow-dry-run-modal',
        'login-overlay',
    ]);

    // Explicitly opt long forms and inspectable records into the shared surface.
    // Short confirmations/selectors retain their existing centered-dialog behavior.
    const DRAWER_IDS = new Set([
        'asset-detail-modal', 'asset-editor-modal', 'asset-import-modal', 'asset-bulk-edit-modal',
        'vulnerability-inspector', 'vulnerability-modal', 'vulnerability-retest-modal',
        'batch-queue-detail-modal', 'batch-manage-modal', 'batch-import-modal',
        'mcp-detail-modal', 'external-mcp-modal', 'attack-chain-modal',
        'fact-modal', 'fact-detail-modal', 'knowledge-item-modal', 'retrieval-log-details-modal',
        'audit-detail-modal', 'hitl-log-modal', 'skill-modal', 'skill-view-modal', 'agent-md-modal', 'role-modal',
        'rbac-user-modal', 'rbac-role-modal', 'chat-files-edit-modal', 'webshell-modal',
        'workflow-package-import-modal', 'workflow-ai-modal', 'robot-commands-modal',
        'robot-account-binding-modal', 'c2-modal',
        'info-collect-cell-modal', 'fofa-parse-modal', 'webshell-db-profile-modal',
    ]);
    const WIDE_IDS = new Set([
        'skill-modal', 'skill-view-modal', 'agent-md-modal', 'role-modal', 'fact-modal', 'vulnerability-modal',
        'chat-files-edit-modal', 'batch-queue-detail-modal', 'mcp-detail-modal', 'attack-chain-modal',
    ]);
    const returnFocus = new WeakMap();
    const surfaceObservers = new WeakMap();
    const openOrder = [];

    function prepareSurface(el) {
        if (!el || el.id === 'login-overlay') return;
        const drawer = DRAWER_IDS.has(el.id) && !el.querySelector('.c2-modal--sleep');
        el.dataset.uiSurface = drawer ? 'drawer' : 'dialog';
        el.classList.toggle('app-drawer-wide', drawer && (WIDE_IDS.has(el.id) || !!el.querySelector('.c2-modal--wide')));
        if (!drawer) return;
        el.setAttribute('role', 'dialog');
        el.setAttribute('aria-modal', 'true');
        const header = el.querySelector('.modal-header, .projects-modal-header, .c2-modal-header, .vulnerability-inspector-header');
        if (!header) return; // Asynchronously rendered details are prepared again after rendering.
        const title = header.querySelector('h2, h3');
        if (title) {
            if (!title.id) title.id = el.id + '-heading';
            el.setAttribute('aria-labelledby', title.id);
        }
        const close = header.querySelector('.modal-close, .projects-modal-close, .c2-modal-close');
        if (close && close.tagName !== 'BUTTON') {
            close.setAttribute('role', 'button');
            close.tabIndex = 0;
            close.onkeydown = function (event) {
                if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); close.click(); }
            };
        }
        if (close && !close.getAttribute('aria-label')) {
            close.setAttribute('aria-label', typeof window.t === 'function' ? window.t('common.close') : 'Close');
        }
        if (!header.querySelector('.app-drawer-expand')) {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'app-drawer-expand';
            button.textContent = '⤢';
            button.setAttribute('aria-pressed', 'false');
            button.setAttribute('data-i18n', 'common.expandPanel');
            button.setAttribute('data-i18n-attr', 'aria-label,title');
            button.setAttribute('data-i18n-skip-text', 'true');
            const label = typeof window.t === 'function' ? window.t('common.expandPanel') : 'Expand panel';
            button.setAttribute('aria-label', label);
            button.title = label;
            button.onclick = function () {
                const expanded = el.classList.toggle('app-drawer-expanded');
                button.setAttribute('aria-pressed', String(expanded));
            };
            header.insertBefore(button, close && close.parentNode === header ? close : null);
        }
    }

    function resolveEl(idOrEl) {
        if (!idOrEl) return null;
        return typeof idOrEl === 'string' ? document.getElementById(idOrEl) : idOrEl;
    }

    function isElVisible(el) {
        if (!el) return false;
        const s = window.getComputedStyle(el);
        return s.display !== 'none' && s.visibility !== 'hidden';
    }

    function defaultDisplay(el) {
        if (el.classList.contains('projects-modal-overlay') || el.classList.contains('c2-modal-overlay')) {
            return 'flex';
        }
        if (el.classList.contains('info-collect-cell-modal')) {
            return 'flex';
        }
        if (el.classList.contains('chat-files-form-modal')) {
            return 'flex';
        }
        if (FLEX_MODAL_IDS.has(el.id)) {
            return 'flex';
        }
        return 'block';
    }

    function syncBodyLock() {
        const anyOpen = Array.from(document.querySelectorAll(OVERLAY_SELECTOR)).some(isElVisible);
        document.body.classList.toggle(BODY_LOCK, anyOpen);
        const projectsOpen = Array.from(document.querySelectorAll('.projects-modal-overlay')).some(isElVisible);
        document.body.classList.toggle(LEGACY_BODY_LOCK, projectsOpen);
    }

    function openAppModal(idOrEl, opts) {
        opts = opts || {};
        const el = resolveEl(idOrEl);
        if (!el) return null;
        prepareSurface(el);
        if (!surfaceObservers.has(el)) {
            const observer = new MutationObserver(function () { prepareSurface(el); });
            observer.observe(el, { childList: true, subtree: true });
            surfaceObservers.set(el, observer);
        }
        if (!isElVisible(el)) returnFocus.set(el, document.activeElement);
        const previous = openOrder.indexOf(el);
        if (previous !== -1) openOrder.splice(previous, 1);
        openOrder.push(el);
        // Nested editors must stack above the detail panel they were opened from.
        el.style.setProperty('--app-overlay-order', String(10060 + openOrder.length * 2));
        el.style.display = opts.display || defaultDisplay(el);
        syncBodyLock();
        requestAnimationFrame(function () { if (isElVisible(el)) prepareSurface(el); });
        if (opts.focus === false) return el;
        const sel =
            opts.focusSelector ||
            'input.form-input, textarea.form-input, select.form-input, input:not([type="hidden"]):not([disabled]), textarea:not([disabled]), select:not([disabled])';
        const focusTarget = opts.focusEl || Array.from(el.querySelectorAll(sel)).find(item => item.getClientRects().length)
            || el.querySelector('.modal-close, .projects-modal-close, .c2-modal-close');
        if (focusTarget) {
            requestAnimationFrame(function () {
                focusTarget.focus();
            });
        }
        return el;
    }

    function closeAppModal(idOrEl) {
        const el = resolveEl(idOrEl);
        if (el) {
            el.style.display = 'none';
            surfaceObservers.get(el)?.disconnect();
            surfaceObservers.delete(el);
            el.classList.remove('app-drawer-expanded');
            const button = el.querySelector('.app-drawer-expand');
            if (button) button.setAttribute('aria-pressed', 'false');
            const index = openOrder.indexOf(el);
            if (index !== -1) openOrder.splice(index, 1);
            const target = returnFocus.get(el);
            if (target && target.isConnected) target.focus({ preventScroll: true });
            returnFocus.delete(el);
        }
        syncBodyLock();
        return el;
    }

    function isAppModalOpen(idOrEl) {
        return isElVisible(resolveEl(idOrEl));
    }

    /** 双 rAF：等遮罩绘制完成后再写入大段 DOM / 表单 */
    function deferModalContent(fn) {
        requestAnimationFrame(function () {
            requestAnimationFrame(fn);
        });
    }

    // Keep keyboard focus within the topmost drawer. Existing close handlers own
    // cleanup and dirty-form semantics, so Escape activates their close control.
    document.addEventListener('keydown', function (event) {
        const el = openOrder.filter(isElVisible).at(-1);
        if (!el || el.dataset.uiSurface !== 'drawer' || event.defaultPrevented) return;
        if (event.key === 'Escape') {
            const close = el.querySelector('.modal-close, .projects-modal-close, .c2-modal-close');
            if (close) { event.preventDefault(); event.stopImmediatePropagation(); close.click(); }
        } else if (event.key === 'Tab') {
            const items = Array.from(el.querySelectorAll('button, [href], input, select, textarea, [tabindex]'))
                .filter(item => !item.disabled && item.tabIndex >= 0 && item.getClientRects().length);
            if (!items.length) return;
            const first = items[0], last = items[items.length - 1];
            if (event.shiftKey && (document.activeElement === first || !el.contains(document.activeElement))) {
                event.preventDefault(); last.focus();
            } else if (!event.shiftKey && (document.activeElement === last || !el.contains(document.activeElement))) {
                event.preventDefault(); first.focus();
            }
        }
    });
    document.querySelectorAll(OVERLAY_SELECTOR).forEach(prepareSurface);

    window.openAppModal = openAppModal;
    window.closeAppModal = closeAppModal;
    window.isAppModalOpen = isAppModalOpen;
    window.deferModalContent = deferModalContent;
    window.syncAppModalBodyLock = syncBodyLock;
})();
