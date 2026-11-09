/* ==========================================================================
   CyberStrikeAI — Mobile UI Runtime
   --------------------------------------------------------------------------
   Mobile interaction layer paired with mobile.css. Design language references QuantumNous/new-api:
     - 768px breakpoint (matchMedia('(max-width: 767.98px)'))
     - Left-side drawer navigation (rounded + backdrop-blur mask + spring transition)
     - Bottom slide-out panel (More / Quick actions)
     - Bottom tab bar + pending approval badge
     - visualViewport keyboard avoidance

   This file is a "pure increment" layer: it does not modify the behavior of any existing JS, it only hooks on top of it.
   Desktop (≥768px) injects no DOM and does not change existing logic.
   ========================================================================== */
(function () {
    'use strict';

    /* ---------------------------------------------------------------------
       0. Constants and state
       --------------------------------------------------------------------- */
    var MOBILE_QUERY = '(max-width: 767.98px)';
    var mq = window.matchMedia ? window.matchMedia(MOBILE_QUERY) : null;

    var STORAGE_TAB = 'csaiMobileTab';

    var state = {
        drawerOpen: false,
        sheetOpen: false,
        chatDrawerOpen: false,
        booted: false,
        hitlPending: 0
    };

    function isMobile() {
        return mq ? mq.matches : window.innerWidth < 768;
    }

    /* ---------------------------------------------------------------------
       1. Icon
       --------------------------------------------------------------------- */
    var ICON = {
        menu: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M3 12h18M3 18h18"/></svg>',
        close: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>',
        more: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/></svg>',
        dashboard: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7.5" height="8.5" rx="1.6"/><rect x="13.5" y="3" width="7.5" height="5.5" rx="1.6"/><rect x="13.5" y="11" width="7.5" height="10" rx="1.6"/><rect x="3" y="14" width="7.5" height="7" rx="1.6"/></svg>',
        chat: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.4 8.4 0 0 1-9 8.4 9.2 9.2 0 0 1-2.9-.4L3 21l1.5-5.1A8.3 8.3 0 0 1 3 11.5 8.4 8.4 0 0 1 12 3a8.4 8.4 0 0 1 9 8.5z"/></svg>',
        hitl: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2 4 5.5v6c0 5 3.4 9.3 8 10.5 4.6-1.2 8-5.5 8-10.5v-6L12 2z"/><path d="m9 12 2 2 4-4"/></svg>',
        tasks: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="3" width="16" height="18" rx="2.2"/><path d="M8.5 8.5h7M8.5 12.5h7M8.5 16.5h4"/></svg>',
        doc: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M9 13h6M9 17h6"/></svg>',
        github: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.58 2 12.23c0 4.52 2.87 8.35 6.84 9.7.5.1.68-.22.68-.49v-1.7c-2.78.62-3.37-1.37-3.37-1.37-.45-1.18-1.11-1.5-1.11-1.5-.91-.63.07-.62.07-.62 1 .07 1.53 1.05 1.53 1.05.9 1.57 2.36 1.12 2.94.86.09-.67.35-1.12.63-1.38-2.22-.26-4.56-1.14-4.56-5.06 0-1.12.39-2.03 1.03-2.75-.1-.26-.45-1.3.1-2.71 0 0 .84-.28 2.75 1.05a9.3 9.3 0 0 1 5 0c1.91-1.33 2.75-1.05 2.75-1.05.55 1.41.2 2.45.1 2.71.64.72 1.03 1.63 1.03 2.75 0 3.93-2.35 4.8-4.58 5.05.36.32.68.94.68 1.9v2.82c0 .27.18.6.69.49A10.06 10.06 0 0 0 22 12.23C22 6.58 17.52 2 12 2z"/></svg>',
        theme: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19"/></svg>',
        lang: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 2.5 15.4 0 18M12 3c-2.5 2.6-2.5 15.4 0 18"/></svg>',
        top: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M5 12l7-7 7 7"/></svg>',
        refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-3.1-6.8"/><path d="M21 4v5h-5"/></svg>',
        full: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M21 16v3a2 2 0 0 1-2 2h-3M3 16v3a2 2 0 0 0 2 2h3"/></svg>',        list: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></svg>',
        plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14"/></svg>'
    };

    /* Bottom tab bar definition. label is only a fallback for when i18next is not ready; the proper text comes from labelKey. */
    var TABS = [
        { id: 'dashboard', label: 'dashboard', labelKey: 'nav.dashboard', icon: 'dashboard' },
        { id: 'chat', label: 'dialogue', labelKey: 'nav.chat', icon: 'chat' },
        { id: 'hitl', label: 'collaborate', labelKey: 'nav.hitl', icon: 'hitl', badge: 'hitl' },
        { id: 'tasks', label: 'task', labelKey: 'nav.tasks', icon: 'tasks' }
    ];

    /* The set of "attribution pages" corresponding to each tag, used to turn on highlighting */
    var TAB_GROUP = {
        dashboard: ['dashboard'],
        chat: ['chat'],
        hitl: ['hitl'],
        tasks: ['tasks']
    };

    /* ---------------------------------------------------------------------
       2. Utility functions
       --------------------------------------------------------------------- */
    function $(sel, root) {
        return (root || document).querySelector(sel);
    }

    function $$(sel, root) {
        return Array.prototype.slice.call((root || document).querySelectorAll(sel));
    }

    function el(tag, cls, html) {
        var node = document.createElement(tag);
        if (cls) node.className = cls;
        if (html != null) node.innerHTML = html;
        return node;
    }

    /* Inject the node's text: attach data-i18n and let applyTranslations in i18n.js handle it,
       When the language is switched, re-translation happens automatically, so there's no need to store another copy of the dictionary in this layer.
       The node writes the Chinese fallback first — i18next may fail to load entirely (offline/vendor blocked).
       All writes of "Chinese fallback + key" must go through these three functions; nowhere else in this layer may a bare
       setAttribute('aria-label', 'Chinese') appear, otherwise the anti-regression test cannot tell a fallback from a bypass. */
    function translate(key, fallback, opts) {
        if (typeof window.t === 'function') {
            var s = window.t(key, opts);
            if (s && s !== key) return s;
        }
        return fallback.replace(/\{\{\s*(\w+)\s*\}\}/g, function (_, name) {
            return opts && Object.prototype.hasOwnProperty.call(opts, name) ? String(opts[name]) : '';
        });
    }

    function label(textNode, key, fallback) {
        textNode.setAttribute('data-i18n', key);
        textNode.textContent = translate(key, fallback);
        return textNode;
    }

    function setAttrI18n(node, attr, key, fallback) {
        var list = (node.getAttribute('data-i18n-attr') || '').split(',')
            .map(function (s) { return s.trim(); })
            .filter(function (s) { return s && s !== attr; });
        list.push(attr);
        node.setAttribute('data-i18n', key);
        node.setAttribute('data-i18n-attr', list.join(','));
        node.setAttribute(attr, translate(key, fallback));
        return node;
    }

    /* Elements with child nodes (icons) can only have their attributes translated, not textContent, otherwise the icon will be overwritten */
    function ariaLabel(node, key, fallback) {
        node.setAttribute('data-i18n-skip-text', 'true');
        return setAttrI18n(node, 'aria-label', key, fallback);
    }

    function relabelAll() {
        if (typeof window.applyTranslations === 'function') window.applyTranslations(document.body);
    }

    function toast(msg, ms) {
        var box = $('#m-toast');
        if (!box) {
            box = el('div');
            box.id = 'm-toast';
            document.body.appendChild(box);
        }
        box.textContent = msg;
        box.classList.add('m-show');
        clearTimeout(toast._t);
        toast._t = setTimeout(function () {
            box.classList.remove('m-show');
        }, ms || 1800);
    }

    /* ---------------------------------------------------------------------
       3. Viewport height and keyboard avoidance
       --------------------------------------------------------------------- */
    var baseViewportHeight = window.innerHeight || 0;

    function syncViewportHeight() {
        var h = window.innerHeight || document.documentElement.clientHeight;
        if (h > baseViewportHeight) baseViewportHeight = h;
        document.documentElement.style.setProperty('--m-app-height', h + 'px');
    }

    function syncKeyboard() {
        var vv = window.visualViewport;
        if (!vv || !isMobile()) {
            document.body.classList.remove('m-keyboard');
            return;
        }
        var overlap = baseViewportHeight - vv.height - vv.offsetTop;
        var open = overlap > 120;
        document.body.classList.toggle('m-keyboard', open);
    }

    function watchViewport() {
        syncViewportHeight();
        syncKeyboard();
        window.addEventListener('resize', function () {
            syncViewportHeight();
            syncKeyboard();
        });
        window.addEventListener('orientationchange', function () {
            setTimeout(syncViewportHeight, 220);
            setTimeout(syncKeyboard, 260);
        });
        if (window.visualViewport) {
            /* scroll is triggered frame by frame as the page scrolls, whereas syncKeyboard needs to read vv.height, triggering a forced layout,
               Must use rAF to coalesce frames, otherwise scrolling visibly drops frames */
            var raf = 0;
            var onViewportChange = function () {
                if (raf) return;
                raf = requestAnimationFrame(function () {
                    raf = 0;
                    syncKeyboard();
                });
            };
            window.visualViewport.addEventListener('resize', onViewportChange);
            window.visualViewport.addEventListener('scroll', onViewportChange);
        }
    }

    /* ---------------------------------------------------------------------
       4. Mask / Drawer / Panel
       --------------------------------------------------------------------- */
    function ensureScrim() {
        var s = $('#m-scrim');
        if (!s) {
            s = el('div', 'm-scrim');
            s.id = 'm-scrim';
            s.addEventListener('click', function () {
                closeDrawer();
                closeSheet();
                closeChatDrawer();
                closeSelectionPanels();
            });
            document.body.appendChild(s);
        }
        return s;
    }

    function showScrim() {
        ensureScrim().classList.add('m-show');
        document.body.classList.add('m-locked');
    }

    function hideScrimIfIdle() {
        if (state.drawerOpen || state.sheetOpen || state.chatDrawerOpen) return;
        if (selectionPanelOpen()) return;
        var s = $('#m-scrim');
        if (s) s.classList.remove('m-show');
        document.body.classList.remove('m-locked');
    }

    /* The three selection panels on the conversation page (project / role / conversation mode) are all white drawers attached to the bottom, and directly below is
       Also a white input box. Without a mask, the two layers of white boxes are stacked directly on top of each other, and the user reads this as "layout overlap".
       Add a mask layer to separate the drawer from the input box; clicking the mask closes it, and also gives "click blank space to exit" a clear landing point. */
    var SELECTION_PANELS = ['#chat-project-panel', '#role-selection-panel', '#agent-mode-panel'];

    function selectionPanelOpen() {
        for (var i = 0; i < SELECTION_PANELS.length; i++) {
            var p = $(SELECTION_PANELS[i]);
            if (p && getComputedStyle(p).display !== 'none') return true;
        }
        return false;
    }

    function closeSelectionPanels() {
        /* During the dropdown closing process, the panel carries a translateY; clear it upon closing as well, so the next open won't have residual displacement. */
        SELECTION_PANELS.forEach(function (sel) {
            var p = $(sel);
            if (p) { p.style.transform = ''; p.style.transition = ''; }
        });
        ['closeChatProjectPanel', 'closeRoleSelectionPanel', 'closeAgentModePanel'].forEach(function (fn) {
            if (typeof window[fn] === 'function') {
                try { window[fn](); } catch (e) { /* Panel may not be initialized, ignoring */ }
            }
        });
    }

    var lastPanelOpen = null;

    /* Before the panel opens, calibrate two things based on the actual position of the input bar:
       --m-panel-offset = distance from input bar top to viewport bottom + 8px gap → panel bottom stops above the input bar;
       --m-panel-maxh   = panel max 45vh (and not exceeding input bar top - 16px) → leave masking area at panel top,
       Clicking outside the panel (chat area/overlay) can still close it; on small screens it won't be pushed to the viewport top, leaving the overlay with nowhere to click.
       This way, when you click to open the project/role/mode panel, the input box is fully visible throughout (WeChat input panel behavior), no longer overlapping. */
    function updatePanelOffset() {
        var c = document.querySelector('.chat-input-container');
        if (!c) return;
        var top = c.getBoundingClientRect().top;
        var vh = window.innerHeight || document.documentElement.clientHeight;
        var root = document.documentElement.style;
        root.setProperty('--m-panel-offset', Math.round(Math.max(60, vh - top + 8)) + 'px');
        root.setProperty('--m-panel-maxh', Math.max(200, Math.min(Math.round(top - 16), Math.round(vh * 0.45))) + 'px');
    }

    function syncSelectionScrim() {
        var open = selectionPanelOpen();
        /* When the dropdown closes, transform is changed frame by frame, and MutationObserver fires a callback once per frame；
           If the open/closed state hasn't changed, return directly to avoid recomputing the mask every frame */
        if (open === lastPanelOpen) return;
        lastPanelOpen = open;
        if (open) {
            updatePanelOffset();
            showScrim();
            document.body.classList.add('m-panel-open');
        } else {
            document.body.classList.remove('m-panel-open');
            hideScrimIfIdle();
        }
    }

    /* The three selection panels are bottom-attached drawers; following mobile conventions, they support closing by dragging downward.
       The inside of the panel may be a scrolling list; it only takes over the pull-down gesture when scrolled to the top. */
    function enableSelectionPanelSwipe() {
        SELECTION_PANELS.forEach(function (sel) {
            var panel = $(sel);
            if (!panel || panel.__mPanelSwipe) return;
            panel.__mPanelSwipe = true;

            var startY = 0;
            var lastY = 0;
            var tracking = false;
            var dragging = false;

            panel.addEventListener('touchstart', function (e) {
                if (!isMobile() || e.touches.length !== 1) return;
                var n = e.target;
                while (n && n !== panel) {
                    var oy = getComputedStyle(n).overflowY;
                    if ((oy === 'auto' || oy === 'scroll') && n.scrollTop > 0) return;
                    n = n.parentElement;
                }
                startY = e.touches[0].clientY;
                lastY = startY;
                tracking = true;
                dragging = false;
            }, { passive: true });

            panel.addEventListener('touchmove', function (e) {
                if (!tracking || e.touches.length !== 1) return;
                lastY = e.touches[0].clientY;
                var dy = lastY - startY;
                if (!dragging && dy > 10) dragging = true;
                if (dragging && dy > 0) {
                    panel.style.transition = 'none';
                    panel.style.transform = 'translateY(' + dy + 'px)';
                }
            }, { passive: true });

            panel.addEventListener('touchend', function (e) {
                if (!tracking) return;
                tracking = false;
                panel.style.transition = '';
                if (!dragging) return;
                /* In some environments, touchend's changedTouches is empty, fall back to the last frame's coordinates */
                var endY = (e.changedTouches && e.changedTouches[0]) ? e.changedTouches[0].clientY : lastY;
                var dy = endY - startY;
                dragging = false;
                if (dy > 70) closeSelectionPanels();
                else panel.style.transform = '';
            }, { passive: true });
        });
    }

    function watchSelectionPanels() {
        var mo = new MutationObserver(function () { syncSelectionScrim(); });
        SELECTION_PANELS.forEach(function (sel) {
            var p = $(sel);
            if (p) mo.observe(p, { attributes: true, attributeFilter: ['style', 'class'] });
        });
        enableSelectionPanelSwipe();
    }

    function openDrawer() {
        if (!isMobile()) return;
        var sidebar = $('#main-sidebar');
        if (!sidebar) return;
        closeSheet();
        closeChatDrawer();
        sidebar.classList.add('m-open');
        state.drawerOpen = true;
        showScrim();
        var first = $('.m-drawer-search input', sidebar);
        if (first) setTimeout(function () { first.focus({ preventScroll: true }); }, 320);
    }

    function closeDrawer() {
        var sidebar = $('#main-sidebar');
        if (sidebar) {
            sidebar.classList.remove('m-open');
            sidebar.style.transform = '';
        }
        state.drawerOpen = false;
        hideScrimIfIdle();
    }

    function openChatDrawer() {
        if (!isMobile()) return;
        var sidebar = $('#conversation-sidebar');
        if (!sidebar) return;
        /* Close if already open: the toolbar "Session" button must be able to go back, otherwise you can only click on blank space. */
        if (state.chatDrawerOpen) {
            closeChatDrawer();
            return;
        }
        closeDrawer();
        closeSheet();
        sidebar.classList.add('m-open');
        state.chatDrawerOpen = true;
        document.body.classList.add('m-chat-drawer-open');
        showScrim();
    }

    function closeChatDrawer() {
        var sidebar = $('#conversation-sidebar');
        if (sidebar) {
            sidebar.classList.remove('m-open');
            sidebar.style.transform = '';
        }
        state.chatDrawerOpen = false;
        document.body.classList.remove('m-chat-drawer-open');
        hideScrimIfIdle();
    }

    function openSheet() {
        if (!isMobile()) return;
        closeDrawer();
        var sheet = $('#m-sheet');
        if (!sheet) return;
        sheet.classList.add('m-show');
        state.sheetOpen = true;
        showScrim();
    }

    function closeSheet() {
        var sheet = $('#m-sheet');
        if (sheet) {
            sheet.classList.remove('m-show');
            sheet.style.transform = '';
        }
        state.sheetOpen = false;
        hideScrimIfIdle();
    }

    /* ---------------------------------------------------------------------
       5. Top app bar injection
       --------------------------------------------------------------------- */
    function buildMenuButton() {
        var btn = el('button', 'm-menu-btn m-mobile-only', ICON.menu);
        btn.type = 'button';
        ariaLabel(btn, 'mobile.openNav', 'Open navigation menu');
        btn.addEventListener('click', function () {
            if (state.drawerOpen) closeDrawer();
            else openDrawer();
        });
        return btn;
    }

    function buildMoreButton() {
        var btn = el('button', 'm-more-btn m-mobile-only', ICON.more);
        btn.type = 'button';
        ariaLabel(btn, 'mobile.moreActions', 'More actions');
        btn.addEventListener('click', function () {
            if (state.sheetOpen) closeSheet();
            else openSheet();
        });
        return btn;
    }

    function injectHeaderChrome() {
        var content = $('header .header-content');
        if (!content) return;
        if (!$('.m-menu-btn', content)) {
            content.insertBefore(buildMenuButton(), content.firstChild);
        }
        var actions = $('.header-actions', content);
        if (actions && !$('.m-more-btn', actions)) {
            actions.appendChild(buildMoreButton());
        }
    }

    /* ---------------------------------------------------------------------
       6. Drawer header / Search / Footer
       --------------------------------------------------------------------- */
    function injectDrawerChrome() {
        var sidebar = $('#main-sidebar');
        if (!sidebar) return;
        if ($('.m-drawer-head', sidebar)) return;

        var head = el('div', 'm-drawer-head m-mobile-only');
        var brand = el('div', 'm-drawer-brand',
            '<img src="/static/logo.png" alt="CyberStrikeAI">' +
            '<span class="brand-wordmark"><span class="brand-wordmark__core">CyberStrike</span><span class="brand-wordmark__ai">AI</span></span>');
        var close = el('button', 'm-close-btn', ICON.close);
        close.type = 'button';
        ariaLabel(close, 'mobile.closeNav', 'Close navigation menu');
        close.addEventListener('click', closeDrawer);
        head.appendChild(brand);
        head.appendChild(close);

        var search = el('div', 'm-drawer-search m-mobile-only');
        var input = el('input');
        input.type = 'search';
        setAttrI18n(input, 'placeholder', 'mobile.searchMenu', 'Search menu…');
        setAttrI18n(input, 'aria-label', 'mobile.searchMenu', 'Search menu');
        input.setAttribute('autocomplete', 'off');
        input.addEventListener('input', function () {
            filterNav(input.value);
        });
        var empty = el('div', 'm-nav-empty m-mobile-only');
        label(empty, 'mobile.noMatch', 'No matching menu');
        empty.hidden = true;
        search.appendChild(input);
        search.appendChild(empty);

        sidebar.insertBefore(search, sidebar.firstChild);
        sidebar.insertBefore(head, sidebar.firstChild);
    }

    /* Conversation drawer (left-side list on conversation page): mobile close button + auto collapse when selected */
    function injectChatDrawerChrome() {
        var sidebar = $('#conversation-sidebar');
        if (!sidebar) return;
        if ($('.m-chat-drawer-close', sidebar)) return;

        var close = el('button', 'm-close-btn m-chat-drawer-close', ICON.close);
        close.type = 'button';
        ariaLabel(close, 'mobile.closeChatList', 'Close session list');
        close.addEventListener('click', closeChatDrawer);

        /* Takes the place of the desktop "Collapse" button, avoiding taking up an extra line */
        var header = $('.conversation-sidebar-header', sidebar);
        if (header) {
            header.appendChild(close);
        } else {
            var head = el('div', 'm-chat-drawer-head m-mobile-only');
            var title = el('span', 'm-chat-drawer-title');
            label(title, 'mobile.chatList', 'Conversation list');
            head.appendChild(title);
            head.appendChild(close);
            sidebar.insertBefore(head, sidebar.firstChild);
        }

        /* Click session item / auto-collapse after new conversation, otherwise the list keeps covering the conversation */
        sidebar.addEventListener('click', function (e) {
            if (!state.chatDrawerOpen) return;
            var target = e.target;
            if (!target || !target.closest) return;
            /* Project grouping only does filtering, section titles only do expanding, neither closes the drawer */
            if (target.closest('.project-folder-item, .section-header, .recent-conversations-toggle, .m-close-btn')) return;
            var hit = target.closest('.conversation-item, .project-conversation-item, .new-chat-btn, .project-folder-new-conversation');
            if (!hit) return;
            setTimeout(closeChatDrawer, 90);
        });
    }

    /* Menu search: filter nav-item / nav-submenu-item by text */
    function filterNav(keyword) {
        var sidebar = $('#main-sidebar');
        if (!sidebar) return;
        var q = String(keyword || '').trim().toLowerCase();
        var items = $$('.nav-item', sidebar);

        items.forEach(function (item) {
            item.classList.remove('m-nav-hidden');
            var subs = $$('.nav-submenu-item', item);
            /* Parent items match only by their own title, preventing child item text from falsely marking the parent as a hit */
            var content = item.querySelector('.nav-item-content');
            var selfText = ((content ? content.textContent : item.textContent) || '').toLowerCase();
            var selfHit = !q || selfText.indexOf(q) >= 0;
            var subHit = false;

            subs.forEach(function (sub) {
                var hit = !q || (sub.textContent || '').toLowerCase().indexOf(q) >= 0;
                sub.classList.toggle('m-nav-hit', !!q && hit);
                /* Keep the whole group when the parent item itself matches, avoiding an empty group after expansion */
                sub.classList.toggle('m-nav-hidden', !!q && !hit && !selfHit);
                if (hit && q) subHit = true;
            });

            if (!q) {
                item.classList.remove('m-nav-hit');
                return;
            }

            if (selfHit || subHit) {
                item.classList.remove('m-nav-hidden');
                item.classList.toggle('m-nav-hit', selfHit && !subHit);
                /* Automatically expand group when a sub-item matches */
                if (subHit) item.classList.add('expanded');
            } else {
                item.classList.add('m-nav-hidden');
            }
        });

        var empty = $('.m-nav-empty');
        if (empty) {
            var visibleCount = 0;
            items.forEach(function (item) {
                if (!item.classList.contains('m-nav-hidden')) visibleCount++;
            });
            empty.hidden = !q || visibleCount > 0;
        }

        /* Section title: also hide when the entire group is hidden */
        $$('.nav-section-label', sidebar).forEach(function (label) {
            var next = label.nextElementSibling;
            var anyVisible = false;
            while (next && !next.classList.contains('nav-section-label')) {
                if (next.classList.contains('nav-item') && !next.classList.contains('m-nav-hidden')) {
                    anyVisible = true;
                    break;
                }
                next = next.nextElementSibling;
            }
            label.classList.toggle('m-nav-hidden', !!q && !anyVisible);
        });
    }

    /* ---------------------------------------------------------------------
       7. Bottom tab bar
       --------------------------------------------------------------------- */
    function buildTabbar() {
        var bar = el('nav');
        bar.id = 'm-tabbar';
        ariaLabel(bar, 'mobile.mainNav', 'Main navigation');

        TABS.forEach(function (tab) {
            var btn = el('button', 'm-tab', ICON[tab.icon]);
            btn.type = 'button';
            btn.dataset.tab = tab.id;
            var span = el('span');
            label(span, tab.labelKey, tab.label);
            btn.appendChild(span);
            if (tab.badge) {
                var badge = el('span', 'm-tab-badge');
                badge.dataset.badgeFor = tab.badge;
                badge.style.display = 'none';
                btn.appendChild(badge);
            }
            btn.addEventListener('click', function () {
                if (typeof window.switchPage === 'function') {
                    window.switchPage(tab.id);
                }
                closeDrawer();
                closeSheet();
                closeChatDrawer();
            });
            bar.appendChild(btn);
        });
        return bar;
    }

    function injectTabbar() {
        if ($('#m-tabbar')) return;
        var bar = buildTabbar();
        bar.classList.add('m-mobile-only');
        document.body.appendChild(bar);
    }

    function setActiveTab(pageId) {
        var bar = $('#m-tabbar');
        if (!bar) return;
        var target = null;
        Object.keys(TAB_GROUP).forEach(function (key) {
            if (TAB_GROUP[key].indexOf(pageId) >= 0) target = key;
        });
        /* Pages that fall outside the four tabs (C2 / Assets / Settings…) do not highlight any tab:
           the "more" entry lives only in the top bar; no second duplicate button is placed in the tab bar */
        $$('.m-tab', bar).forEach(function (btn) {
            var id = btn.dataset.tab;
            var active = id === target;
            btn.classList.toggle('m-active', active);
            if (active) btn.setAttribute('aria-current', 'page');
            else btn.removeAttribute('aria-current');
        });
    }

    /* ---------------------------------------------------------------------
       8. Bottom slide-out panel
       --------------------------------------------------------------------- */
    function buildSheetItem(key, fallbackLabel, icon, onClick, opts) {
        opts = opts || {};
        var btn = el('button', 'm-sheet-item', ICON[icon]);
        var span = el('span');
        label(span, key, fallbackLabel);
        btn.appendChild(span);
        btn.type = 'button';
        btn.addEventListener('click', function () {
            onClick();
            if (!opts.keepOpen) closeSheet();
        });
        if (opts.id) btn.id = opts.id;
        return btn;
    }

    /* Secondary operations already have buttons on desktop; mobile just consolidates them into a panel.
       Directly trigger the desktop widget, avoiding copying yet another copy of the URL or behavior in this layer (the upstream repository address has changed before). */
    function clickDesktopControl(matcher) {
        var nodes = $$('.header-actions button, .header-actions a');
        for (var i = 0; i < nodes.length; i++) {
            var n = nodes[i];
            var where = (n.getAttribute('onclick') || n.getAttribute('href') || '');
            if (!matcher.test(where)) continue;
            n.click();
            return true;
        }
        return false;
    }

    function currentThemeLabel() {
        var resolved = document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
        return typeof window.t === 'function' ? (window.t('theme.' + resolved) || resolved) : resolved;
    }

    /* Languages selectable in the panel: go by the options that actually exist in the desktop dropdown; upstream adding a language doesn't require changes to this layer */
    function availableLocales() {
        var seen = [];
        $$('#lang-dropdown .lang-option').forEach(function (o) {
            var lang = o.getAttribute('data-lang');
            if (!lang) return;
            if (seen.some(function (x) { return x.lang === lang; })) return;
            seen.push({ lang: lang, name: (o.textContent || '').trim() });
        });
        if (!seen.length) seen = [{ lang: 'zh-CN', name: 'Chinese' }, { lang: 'en-US', name: 'English' }];
        return seen;
    }

    function cycleLanguage() {
        var opts = availableLocales();
        var cur = String(window.uiLocale ? window.uiLocale() : (document.documentElement.getAttribute('lang') || 'zh-CN'));
        var idx = 0;
        opts.forEach(function (o, i) { if (o.lang === cur) idx = i; });
        var next = opts[(idx + 1) % opts.length];
        if (typeof window.onLanguageSelect === 'function') {
            window.onLanguageSelect(next.lang);
            toast(next.name);
        }
    }

    function requestFullscreenSafe() {
        var root = document.documentElement;
        if (document.fullscreenElement || document.webkitFullscreenElement) {
            var exit = document.exitFullscreen || document.webkitExitFullscreen;
            if (exit) { try { exit.call(document); } catch (e) { /* Logged out */ } }
            return;
        }
        var req = root.requestFullscreen || root.webkitRequestFullscreen;
        if (!req) {
            /* iOS Safari has no fullscreen API: previously this went through an empty function branch, clicking the button gave no feedback */
            toast(translate('mobile.fullscreenUnsupported', 'The current browser does not support fullscreen'));
            return;
        }
        try {
            var r = req.call(root);
            if (r && typeof r.catch === 'function') {
                r.catch(function () { toast(translate('mobile.fullscreenUnsupported', 'The current browser does not support fullscreen')); });
            }
        } catch (e) {
            toast(translate('mobile.fullscreenUnsupported', 'The current browser does not support fullscreen'));
        }
    }

    function injectSheet() {
        if ($('#m-sheet')) return;
        var sheet = el('div');
        sheet.id = 'm-sheet';
        sheet.setAttribute('role', 'dialog');
        sheet.setAttribute('aria-modal', 'true');
        ariaLabel(sheet, 'mobile.moreActions', 'More actions');
        sheet.classList.add('m-mobile-only');

        var grip = el('div', 'm-sheet-grip');
        var head = el('div', 'm-sheet-head');
        var title = el('span', 'm-sheet-title');
        label(title, 'mobile.moreActions', 'More actions');
        head.appendChild(title);
        var close = el('button', 'm-close-btn', ICON.close);
        close.type = 'button';
        ariaLabel(close, 'common.close', 'Close');
        close.addEventListener('click', closeSheet);
        head.appendChild(close);

        var body = el('div', 'm-sheet-body');

        body.appendChild(buildSheetItem('header.apiDocs', 'API documentation', 'doc', function () {
            if (!clickDesktopControl(/\/api-docs/)) window.location.href = '/api-docs';
        }));
        body.appendChild(buildSheetItem('header.github', 'GitHub', 'github', function () {
            clickDesktopControl(/github\.com/i);
        }));
        body.appendChild(buildSheetItem('mobile.switchTheme', 'Switch Theme', 'theme', function () {
            if (typeof window.cycleThemePreference === 'function') {
                window.cycleThemePreference();
            } else {
                var t = document.getElementById('theme-toggle-btn');
                if (t) t.click();
            }
            toast(translate('mobile.themeNow', 'Current theme: {{name}}', { name: currentThemeLabel() }));
        }, { keepOpen: true }));
        body.appendChild(buildSheetItem('mobile.switchLanguage', 'Switch language', 'lang', cycleLanguage, { keepOpen: true }));

        body.appendChild(el('div', 'm-sheet-sep'));

        body.appendChild(buildSheetItem('mobile.backToTop', 'Return to top', 'top', scrollActiveToTop, { keepOpen: true }));
        body.appendChild(buildSheetItem('mobile.refreshPage', 'Refresh the page', 'refresh', function () {
            location.reload();
        }));
        body.appendChild(buildSheetItem('mobile.fullscreen', 'full screen', 'full', requestFullscreenSafe, { keepOpen: true }));
        sheet.appendChild(grip);
        sheet.appendChild(head);
        sheet.appendChild(body);
        document.body.appendChild(sheet);

        enableSheetSwipe(sheet);
    }

    function scrollActiveToTop() {
        var page = $('.page.active .page-content') || $('.page.active');
        if (page) page.scrollTo({ top: 0, behavior: 'smooth' });
        var msgs = $('#chat-messages');
        if (msgs && $('#page-chat') && $('#page-chat').classList.contains('active')) {
            msgs.scrollTo({ top: 0, behavior: 'smooth' });
        }
    }

    /* ---------------------------------------------------------------------
       9. Mobile toolbar for the conversation page
       --------------------------------------------------------------------- */
    function injectChatBar() {
        var container = $('.chat-container');
        if (!container) return;
        if ($('#m-chat-bar', container)) return;

        var bar = el('div', 'm-chat-bar m-mobile-only');
        bar.id = 'm-chat-bar';

        var listBtn = el('button', 'm-chat-bar-btn', ICON.list);
        var listSpan = el('span');
        label(listSpan, 'mobile.conversations', 'conversation');
        listBtn.appendChild(listSpan);
        listBtn.type = 'button';
        listBtn.addEventListener('click', openChatDrawer);

        var spacer = el('div', 'm-chat-bar-spacer');

        var newBtn = el('button', 'm-chat-bar-btn m-primary', ICON.plus);
        var newSpan = el('span');
        label(newSpan, 'mobile.newChat', 'New conversation');
        newBtn.appendChild(newSpan);
        newBtn.type = 'button';
        newBtn.setAttribute('data-require-permission', 'chat:write');
        newBtn.addEventListener('click', function () {
            if (typeof window.startNewConversation === 'function') {
                window.startNewConversation();
            } else {
                toast(translate('mobile.chatModuleMissing', 'The dialogue module has not yet been loaded'));
            }
        });

        bar.appendChild(listBtn);
        bar.appendChild(spacer);
        bar.appendChild(newBtn);
        container.insertBefore(bar, container.firstChild);
    }

    /* ---------------------------------------------------------------------
       10. Table horizontal scroll wrapping
       --------------------------------------------------------------------- */
    function wrapTables(root) {
        if (!isMobile()) return;
        var scope = root || document;
        var tables = $$('table', scope);
        tables.forEach(function (table) {
            var parent = table.parentElement;
            if (!parent) return;
            if (parent.classList.contains('m-table-scroll')) return;
            if (table.closest('.m-no-wrap')) return;

            var cols = table.querySelectorAll('thead th').length || table.querySelectorAll('tr:first-child td').length;
            var wrap = el('div', 'm-table-scroll' + (cols > 0 && cols <= 3 ? ' m-tight' : ''));
            parent.insertBefore(wrap, table);
            wrap.appendChild(table);
        });
    }

    /* ---------------------------------------------------------------------
       11. Routing hooks
       --------------------------------------------------------------------- */
    function patchRouter() {
        if (typeof window.switchPage !== 'function' || window.switchPage.__mPatched) return;
        var original = window.switchPage;
        var patched = function (pageId) {
            var result = original.apply(this, arguments);
            try {
                onPageChanged(pageId);
            } catch (e) {
                /* Mobile layer exceptions do not affect the main flow */
                console.warn('[mobile-ui] onPageChanged failed', e);
            }
            return result;
        };
        patched.__mPatched = true;
        patched.__mOriginal = original;
        window.switchPage = patched;
    }

    function onPageChanged(pageId) {
        setActiveTab(pageId);
        closeDrawer();
        closeSheet();
        if (pageId !== 'chat') closeChatDrawer();
        try {
            localStorage.setItem(STORAGE_TAB, pageId);
        } catch (e) { /* Ignored in incognito mode */ }

        /* Page rendering is asynchronous, do the table wrapping later */
        [80, 400, 1200].forEach(function (delay) {
            setTimeout(function () {
                var page = document.getElementById('page-' + pageId);
                if (page) wrapTables(page);
            }, delay);
        });
    }

    /* ---------------------------------------------------------------------
       12. Pending approval badge (HITL)
       --------------------------------------------------------------------- */
    function setHitlBadge(count) {
        state.hitlPending = count;
        var badge = $('[data-badge-for="hitl"]');
        if (!badge) return;
        if (!count || count <= 0) {
            badge.style.display = 'none';
            badge.textContent = '';
            return;
        }
        badge.textContent = count > 99 ? '99+' : String(count);
        badge.style.display = 'inline-block';
    }

    function refreshHitlBadge() {
        if (!isMobile() || !state.booted) return;
        if ($('#login-overlay') && $('#login-overlay').style.display !== 'none') {
            setHitlBadge(0);
            return;
        }
        var pending = fetch('/api/hitl/pending?page=1&pageSize=1', { credentials: 'same-origin' })
            .then(function (r) { return r.ok ? r.json() : null; })
            .then(function (d) {
                if (!d) return 0;
                if (typeof d.total === 'number') return d.total;
                if (Array.isArray(d.items)) return d.items.length;
                return 0;
            })
            .catch(function () { return 0; });

        var workflows = fetch('/api/workflows/runs/pending', { credentials: 'same-origin' })
            .then(function (r) { return r.ok ? r.json() : null; })
            .then(function (d) {
                if (!d || !Array.isArray(d.runs)) return 0;
                return d.runs.length;
            })
            .catch(function () { return 0; });

        Promise.all([pending, workflows]).then(function (res) {
            setHitlBadge((res[0] || 0) + (res[1] || 0));
        });
    }

    /* ---------------------------------------------------------------------
       13. Login state and bottom bar visibility
       --------------------------------------------------------------------- */
    function syncAuthChrome() {
        var overlay = $('#login-overlay');
        var loggedOut = overlay && overlay.style.display !== 'none';
        var bar = $('#m-tabbar');
        if (bar) bar.style.display = loggedOut ? 'none' : '';
        if (loggedOut) {
            closeDrawer();
            closeSheet();
            closeChatDrawer();
        }
    }

    function watchAuthOverlay() {
        var overlay = $('#login-overlay');
        if (!overlay) return;
        new MutationObserver(syncAuthChrome).observe(overlay, {
            attributes: true,
            attributeFilter: ['style', 'class']
        });
    }

    /* ---------------------------------------------------------------------
       14. Gesture
       --------------------------------------------------------------------- */
    function enableDrawerSwipe(sidebar, closer) {
        if (!sidebar) return;
        var startX = 0;
        var startY = 0;
        var tracking = false;
        var dragging = false;

        sidebar.addEventListener('touchstart', function (e) {
            if (!isMobile() || e.touches.length !== 1) return;
            startX = e.touches[0].clientX;
            startY = e.touches[0].clientY;
            tracking = true;
            dragging = false;
        }, { passive: true });

        sidebar.addEventListener('touchmove', function (e) {
            if (!tracking || e.touches.length !== 1) return;
            var dx = e.touches[0].clientX - startX;
            var dy = e.touches[0].clientY - startY;
            if (!dragging) {
                if (Math.abs(dx) > 10 && Math.abs(dx) > Math.abs(dy) * 1.4 && dx < 0) {
                    dragging = true;
                } else if (Math.abs(dy) > 12) {
                    tracking = false;
                    return;
                }
            }
            if (dragging && dx < 0) {
                sidebar.style.transition = 'none';
                sidebar.style.transform = 'translateX(' + dx + 'px)';
            }
        }, { passive: true });

        sidebar.addEventListener('touchend', function (e) {
            if (!tracking) return;
            tracking = false;
            sidebar.style.transition = '';
            if (!dragging) return;
            var dx = (e.changedTouches[0] ? e.changedTouches[0].clientX : startX) - startX;
            dragging = false;
            if (dx < -60) {
                closer();
            } else {
                sidebar.style.transform = '';
            }
        }, { passive: true });
    }

    function enableSheetSwipe(sheet) {
        var startY = 0;
        var lastY = 0;
        var tracking = false;
        var dragging = false;

        sheet.addEventListener('touchstart', function (e) {
            if (!isMobile() || e.touches.length !== 1) return;
            /* When the body has already been scrolled, prefer scrolling the list; when scrolled to the top or pressed on the handle/head, allow pull-down to close */
            var body = e.target.closest('.m-sheet-body');
            if (body && body.scrollTop > 0) return;
            startY = e.touches[0].clientY;
            lastY = startY;
            tracking = true;
            dragging = false;
        }, { passive: true });

        sheet.addEventListener('touchmove', function (e) {
            if (!tracking || e.touches.length !== 1) return;
            lastY = e.touches[0].clientY;
            var dy = lastY - startY;
            if (!dragging && dy > 10) dragging = true;
            if (dragging && dy > 0) {
                sheet.style.transition = 'none';
                sheet.style.transform = 'translateY(' + dy + 'px)';
            }
        }, { passive: true });

        sheet.addEventListener('touchend', function (e) {
            if (!tracking) return;
            tracking = false;
            sheet.style.transition = '';
            if (!dragging) return;
            /* In some environments, touchend's changedTouches is empty, fall back to the last frame's coordinates */
            var endY = (e.changedTouches && e.changedTouches[0]) ? e.changedTouches[0].clientY : lastY;
            var dy = endY - startY;
            dragging = false;
            if (dy > 70) closeSheet();
            else sheet.style.transform = '';
        }, { passive: true });
    }

    /* Swipe right from the left edge of the screen to open the drawer */
    function enableEdgeSwipe() {
        var startX = 0;
        var startY = 0;
        var armed = false;
        var dragging = false;

        document.addEventListener('touchstart', function (e) {
            if (!isMobile() || e.touches.length !== 1) return;
            if (state.drawerOpen || state.sheetOpen || state.chatDrawerOpen) return;
            var t = e.touches[0];
            if (t.clientX > 24) return;
            if ($('#login-overlay') && $('#login-overlay').style.display !== 'none') return;
            startX = t.clientX;
            startY = t.clientY;
            armed = true;
            dragging = false;
        }, { passive: true });

        document.addEventListener('touchmove', function (e) {
            if (!armed || e.touches.length !== 1) return;
            var dx = e.touches[0].clientX - startX;
            var dy = e.touches[0].clientY - startY;
            if (!dragging) {
                if (dx > 18 && Math.abs(dx) > Math.abs(dy) * 1.4) {
                    dragging = true;
                    openDrawer();
                } else if (Math.abs(dy) > 16) {
                    armed = false;
                }
            }
        }, { passive: true });

        document.addEventListener('touchend', function () {
            armed = false;
            dragging = false;
        }, { passive: true });
    }

    /* Double-tap zoom is suppressed by CSS touch-action: manipulation (see mobile.css),
       The original global non-passive touchend listener would block scrolling, has been removed */

    /* ---------------------------------------------------------------------
       15. Automatically collapse drawer after navigation click
       --------------------------------------------------------------------- */
    function bindNavAutoClose() {
        var sidebar = $('#main-sidebar');
        if (!sidebar) return;
        sidebar.addEventListener('click', function (e) {
            if (!isMobile()) return;
            var target = e.target.closest('.nav-submenu-item');
            if (target && target.dataset.page) {
                setTimeout(closeDrawer, 60);
                return;
            }
            var item = e.target.closest('.nav-item-content');
            if (item && item.closest('.nav-item') && !item.closest('.nav-item-has-submenu')) {
                setTimeout(closeDrawer, 60);
            }
        });
    }

    /* ---------------------------------------------------------------------
       16. Table MutationObserver
       --------------------------------------------------------------------- */
    function watchDom() {
        var area = $('.content-area');
        if (!area) return;
        var timer = null;
        var observer = new MutationObserver(function (records) {
            /* Only care about changes that might introduce tables: chat streaming output continuously modifies the DOM,
               If every change triggers a full-page table scan, long sessions will see noticeable frame drops */
            var relevant = records.some(function (r) {
                if (!r.addedNodes || !r.addedNodes.length) return false;
                for (var i = 0; i < r.addedNodes.length; i++) {
                    var n = r.addedNodes[i];
                    if (n.nodeType !== 1) continue;
                    if (n.tagName === 'TABLE') return true;
                    if (n.querySelector && n.querySelector('table')) return true;
                }
                return false;
            });
            if (!relevant) return;
            clearTimeout(timer);
            timer = setTimeout(function () {
                var page = $('.page.active');
                if (page) wrapTables(page);
            }, 260);
        });
        observer.observe(area, { childList: true, subtree: true });
    }

    /* ---------------------------------------------------------------------
       17. Breakpoint toggle
       --------------------------------------------------------------------- */
    function onBreakpointChange() {
        if (isMobile()) {
            boot();
        } else {
            closeDrawer();
            closeSheet();
            closeChatDrawer();
            closeSelectionPanels();
            var scrim = $('#m-scrim');
            if (scrim) scrim.classList.remove('m-show');
            document.body.classList.remove('m-locked', 'm-keyboard');
            document.documentElement.style.removeProperty('--m-app-height');
        }
    }

    /* ---------------------------------------------------------------------
       18. Start
       --------------------------------------------------------------------- */
    function boot() {
        if (!isMobile()) return;
        injectHeaderChrome();
        injectDrawerChrome();
        injectChatDrawerChrome();
        injectTabbar();
        injectSheet();
        injectChatBar();
        ensureScrim();
        watchAuthOverlay();
        syncAuthChrome();
        patchRouter();

        /* Restore the highlight of the last visited page */
        var hashPage = (window.location.hash || '').replace(/^#/, '').split('?')[0];
        setActiveTab(hashPage || 'dashboard');

        if (!state.booted) {
            state.booted = true;
            bindNavAutoClose();
            enableDrawerSwipe($('#main-sidebar'), closeDrawer);
            enableDrawerSwipe($('#conversation-sidebar'), closeChatDrawer);
            enableEdgeSwipe();
            watchDom();
            watchSelectionPanels();

            /* Pending approval badge: first fetch + every 45s + refresh when returning to foreground */
            refreshHitlBadge();
            setInterval(refreshHitlBadge, 45000);
            document.addEventListener('visibilitychange', function () {
                if (!document.hidden) refreshHitlBadge();
            });
        }

        wrapTables($('.page.active') || document);
        /* Nodes injected later than the i18n first frame are not automatically translated, so we patch it once here; language switching is handled by
           The applyTranslations(document) in i18n.js covers (these nodes carry data-i18n). */
        relabelAll();
        /* The new conversation button carries data-require-permission: the RBAC click guard is document-level delegation,
           but "hide when permission is missing" depends on one explicit refresh, otherwise a low-permission account sees an unclickable button */
        if (typeof window.applyRBACToUI === 'function') {
            try { window.applyRBACToUI(); } catch (e) { /* Ignore if not initialized */ }
        }
    }

    /* Mobile has a mask fallback, tablet portrait has no mobile shell (boot returns directly on non-mobile),
       The three selection panels are likewise bottom-docked drawers; here we use document-level delegation to fill in 'click outside the panel to close',
       Let 768–1023.98px also have an escape route that doesn't depend on the × button. */
    function bindSelectionOutsideClose() {
        document.addEventListener('click', function (e) {
            if (!selectionPanelOpen()) return;
            if (e.target.closest('#chat-project-panel, #role-selection-panel, #agent-mode-panel')) return;
            if (e.target.closest('#chat-project-btn, #role-selector-btn, #agent-mode-btn')) return;
            closeSelectionPanels();
        }, true);
    }

    function start() {
        watchViewport();
        if (mq) {
            if (mq.addEventListener) mq.addEventListener('change', onBreakpointChange);
            else if (mq.addListener) mq.addListener(onBreakpointChange);
        }
        if (isMobile()) {
            boot();
        } else {
            /* Tablet portrait mode: panel still sticks to the bottom (mobile.css block 22), but watchSelectionPanels is only registered inside boot */
            watchSelectionPanels();
        }
        bindSelectionOutsideClose();
    }

    /* Exposed externally, to facilitate debugging and future extension */
    window.CSAMobile = {
        isMobile: isMobile,
        openDrawer: openDrawer,
        closeDrawer: closeDrawer,
        openChatDrawer: openChatDrawer,
        closeChatDrawer: closeChatDrawer,
        openSheet: openSheet,
        closeSheet: closeSheet,
        closeSelectionPanels: closeSelectionPanels,
        refreshHitlBadge: refreshHitlBadge,
        wrapTables: wrapTables,
        toast: toast
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', start);
    } else {
        start();
    }
})();
