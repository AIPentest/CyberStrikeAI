/**
 * Unify popup: show the mask first, then fill in the large content in the next frame, to avoid competing with backdrop rendering for the main thread.
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
        el.style.display = opts.display || defaultDisplay(el);
        syncBodyLock();
        if (opts.focus === false) return el;
        const sel =
            opts.focusSelector ||
            'input.form-input, textarea.form-input, select.form-input, input:not([type="hidden"]):not([disabled]), textarea:not([disabled]), select:not([disabled])';
        const focusTarget = opts.focusEl || el.querySelector(sel);
        if (focusTarget) {
            requestAnimationFrame(function () {
                focusTarget.focus();
            });
        }
        return el;
    }

    function closeAppModal(idOrEl) {
        const el = resolveEl(idOrEl);
        if (el) el.style.display = 'none';
        syncBodyLock();
        return el;
    }

    function isAppModalOpen(idOrEl) {
        return isElVisible(resolveEl(idOrEl));
    }

    /** Double rAF: wait for the mask to finish rendering before writing large DOM chunks / forms */
    function deferModalContent(fn) {
        requestAnimationFrame(function () {
            requestAnimationFrame(fn);
        });
    }

    window.openAppModal = openAppModal;
    window.closeAppModal = closeAppModal;
    window.isAppModalOpen = isAppModalOpen;
    window.deferModalContent = deferModalContent;
    window.syncAppModalBodyLock = syncBodyLock;
})();
