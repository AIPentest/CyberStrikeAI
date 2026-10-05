/* ==========================================================================
   CyberStrikeAI — Mobile UI Runtime
   --------------------------------------------------------------------------
   与 mobile.css 配套的移动端交互层。设计语言参考 QuantumNous/new-api：
     - 768px 断点（matchMedia('(max-width: 767.98px)')）
     - 左侧抽屉导航（rounded + backdrop-blur 遮罩 + spring 过渡）
     - 底部滑出式面板（更多 / 快捷操作）
     - 底部标签栏 + 待审批角标
     - visualViewport 键盘避让

   本文件是"纯增量"层：不修改任何既有 JS 的行为，只在其上挂钩。
   桌面端（≥768px）不注入任何 DOM，也不改变既有逻辑。
   ========================================================================== */
(function () {
    'use strict';

    /* ---------------------------------------------------------------------
       0. 常量与状态
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
       1. 图标
       --------------------------------------------------------------------- */
    var ICON = {
        menu: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M3 12h18M3 18h18"/></svg>',
        close: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>',
        more: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/></svg>',
        dashboard: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7.5" height="8.5" rx="1.6"/><rect x="13.5" y="3" width="7.5" height="5.5" rx="1.6"/><rect x="13.5" y="11" width="7.5" height="10" rx="1.6"/><rect x="3" y="14" width="7.5" height="7" rx="1.6"/></svg>',
        chat: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.4 8.4 0 0 1-9 8.4 9.2 9.2 0 0 1-2.9-.4L3 21l1.5-5.1A8.3 8.3 0 0 1 3 11.5 8.4 8.4 0 0 1 12 3a8.4 8.4 0 0 1 9 8.5z"/></svg>',
        hitl: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2 4 5.5v6c0 5 3.4 9.3 8 10.5 4.6-1.2 8-5.5 8-10.5v-6L12 2z"/><path d="m9 12 2 2 4-4"/></svg>',
        tasks: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="3" width="16" height="18" rx="2.2"/><path d="M8.5 8.5h7M8.5 12.5h7M8.5 16.5h4"/></svg>',
        more2: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/></svg>',
        doc: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M9 13h6M9 17h6"/></svg>',
        github: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.58 2 12.23c0 4.52 2.87 8.35 6.84 9.7.5.1.68-.22.68-.49v-1.7c-2.78.62-3.37-1.37-3.37-1.37-.45-1.18-1.11-1.5-1.11-1.5-.91-.63.07-.62.07-.62 1 .07 1.53 1.05 1.53 1.05.9 1.57 2.36 1.12 2.94.86.09-.67.35-1.12.63-1.38-2.22-.26-4.56-1.14-4.56-5.06 0-1.12.39-2.03 1.03-2.75-.1-.26-.45-1.3.1-2.71 0 0 .84-.28 2.75 1.05a9.3 9.3 0 0 1 5 0c1.91-1.33 2.75-1.05 2.75-1.05.55 1.41.2 2.45.1 2.71.64.72 1.03 1.63 1.03 2.75 0 3.93-2.35 4.8-4.58 5.05.36.32.68.94.68 1.9v2.82c0 .27.18.6.69.49A10.06 10.06 0 0 0 22 12.23C22 6.58 17.52 2 12 2z"/></svg>',
        theme: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19"/></svg>',
        lang: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 2.5 15.4 0 18M12 3c-2.5 2.6-2.5 15.4 0 18"/></svg>',
        top: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M5 12l7-7 7 7"/></svg>',
        refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-3.1-6.8"/><path d="M21 4v5h-5"/></svg>',
        full: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M21 16v3a2 2 0 0 1-2 2h-3M3 16v3a2 2 0 0 0 2 2h3"/></svg>',
        logout: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5M21 12H9"/></svg>',
        list: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></svg>',
        plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14"/></svg>'
    };

    /* 底部标签栏定义 */
    var TABS = [
        { id: 'dashboard', label: '仪表盘', icon: 'dashboard' },
        { id: 'chat', label: '对话', icon: 'chat' },
        { id: 'hitl', label: '协同', icon: 'hitl', badge: 'hitl' },
        { id: 'tasks', label: '任务', icon: 'tasks' },
        { id: '__more', label: '更多', icon: 'more2' }
    ];

    /* 每个标签对应的"归属页"集合，用于点亮高亮 */
    var TAB_GROUP = {
        dashboard: ['dashboard'],
        chat: ['chat'],
        hitl: ['hitl'],
        tasks: ['tasks']
    };

    /* ---------------------------------------------------------------------
       2. 工具函数
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
       3. 视口高度与键盘避让
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
            /* scroll 在页面滚动时逐帧触发，而 syncKeyboard 要读 vv.height 触发强制布局，
               必须用 rAF 合帧，否则滚动明显掉帧 */
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
       4. 遮罩 / 抽屉 / 面板
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

    /* 对话页三个选择面板（项目 / 角色 / 对话模式）都是贴底的白色抽屉，正下方就是
       同为白色的输入框。没有遮罩时两层白盒子直接叠在一起，用户读到的是「布局重叠」。
       补一层遮罩把抽屉与输入框分开，点遮罩即关闭，也让「点空白退出」有明确落点。 */
    var SELECTION_PANELS = ['#chat-project-panel', '#role-selection-panel', '#agent-mode-panel'];

    function selectionPanelOpen() {
        for (var i = 0; i < SELECTION_PANELS.length; i++) {
            var p = $(SELECTION_PANELS[i]);
            if (p && getComputedStyle(p).display !== 'none') return true;
        }
        return false;
    }

    function closeSelectionPanels() {
        /* 下拉关闭过程中面板带着 translateY，关闭时一并清掉，下次打开不会残留位移 */
        SELECTION_PANELS.forEach(function (sel) {
            var p = $(sel);
            if (p) { p.style.transform = ''; p.style.transition = ''; }
        });
        ['closeChatProjectPanel', 'closeRoleSelectionPanel', 'closeAgentModePanel'].forEach(function (fn) {
            if (typeof window[fn] === 'function') {
                try { window[fn](); } catch (e) { /* 面板可能未初始化，忽略 */ }
            }
        });
    }

    var lastPanelOpen = null;

    /* 面板打开前按输入条实际位置校准两件事：
       --m-panel-offset = 输入条顶部到视口底 + 8px 间距 → 面板 bottom 停在输入条上方；
       --m-panel-maxh   = 面板最高 45vh（且不超过输入条顶-16px）→ 面板顶部留出遮罩区，
       点面板外（聊天区/遮罩）仍能关闭，小屏上不会顶到视口顶让遮罩无处可点。
       这样点开项目/角色/模式面板时输入框全程可见（微信输入面板行为），不再重叠。 */
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
        /* 下拉关闭时会逐帧改 transform，MutationObserver 每帧回调一次；
           开合状态没变就直接返回，避免每帧重算遮罩 */
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

    /* 三个选择面板是贴底抽屉，按移动端习惯支持向下拖动关闭。
       面板内部可能是滚动列表，只有滚到顶才接管下拉手势。 */
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
                /* 部分环境下 touchend 的 changedTouches 为空，回退到最后一帧的坐标 */
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
        /* 已打开则关闭：工具条「会话」按钮必须能反悔，否则只能点空白 */
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
       5. 顶部应用栏注入
       --------------------------------------------------------------------- */
    function buildMenuButton() {
        var btn = el('button', 'm-menu-btn m-mobile-only', ICON.menu);
        btn.type = 'button';
        btn.setAttribute('aria-label', '打开导航菜单');
        btn.addEventListener('click', function () {
            if (state.drawerOpen) closeDrawer();
            else openDrawer();
        });
        return btn;
    }

    function buildMoreButton() {
        var btn = el('button', 'm-more-btn m-mobile-only', ICON.more);
        btn.type = 'button';
        btn.setAttribute('aria-label', '更多操作');
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
       6. 抽屉头部 / 搜索 / 页脚
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
        close.setAttribute('aria-label', '关闭导航菜单');
        close.addEventListener('click', closeDrawer);
        head.appendChild(brand);
        head.appendChild(close);

        var search = el('div', 'm-drawer-search m-mobile-only');
        var input = el('input');
        input.type = 'search';
        input.placeholder = '搜索菜单…';
        input.setAttribute('autocomplete', 'off');
        input.setAttribute('aria-label', '搜索菜单');
        input.addEventListener('input', function () {
            filterNav(input.value);
        });
        var empty = el('div', 'm-nav-empty m-mobile-only', '没有匹配的菜单');
        empty.hidden = true;
        search.appendChild(input);
        search.appendChild(empty);

        var foot = el('div', 'm-drawer-foot m-mobile-only');
        foot.appendChild(el('span', null, '移动端导航'));
        var topBtn = el('button', 'btn-secondary btn-small', '返回顶部');
        topBtn.type = 'button';
        topBtn.style.minHeight = '32px';
        topBtn.addEventListener('click', function () {
            scrollActiveToTop();
            closeDrawer();
        });
        foot.appendChild(topBtn);

        sidebar.insertBefore(foot, sidebar.firstChild);
        sidebar.insertBefore(search, sidebar.firstChild);
        sidebar.insertBefore(head, sidebar.firstChild);
    }

    /* 会话抽屉（对话页左侧列表）：移动端关闭按钮 + 选中后自动收起 */
    function injectChatDrawerChrome() {
        var sidebar = $('#conversation-sidebar');
        if (!sidebar) return;
        if ($('.m-chat-drawer-close', sidebar)) return;

        var close = el('button', 'm-close-btn m-chat-drawer-close', ICON.close);
        close.type = 'button';
        close.setAttribute('aria-label', '关闭会话列表');
        close.addEventListener('click', closeChatDrawer);

        /* 顶掉桌面端「折叠」按钮的位置，避免额外占一行 */
        var header = $('.conversation-sidebar-header', sidebar);
        if (header) {
            header.appendChild(close);
        } else {
            var head = el('div', 'm-chat-drawer-head m-mobile-only');
            head.appendChild(el('span', 'm-chat-drawer-title', '会话列表'));
            head.appendChild(close);
            sidebar.insertBefore(head, sidebar.firstChild);
        }

        /* 点会话项 / 新建对话后自动收起，否则列表一直盖在对话上 */
        sidebar.addEventListener('click', function (e) {
            if (!state.chatDrawerOpen) return;
            var target = e.target;
            if (!target || !target.closest) return;
            /* 项目分组只做筛选、分区标题只做展开，都不关闭抽屉 */
            if (target.closest('.project-folder-item, .section-header, .recent-conversations-toggle, .m-close-btn')) return;
            var hit = target.closest('.conversation-item, .project-conversation-item, .new-chat-btn, .project-folder-new-conversation');
            if (!hit) return;
            setTimeout(closeChatDrawer, 90);
        });
    }

    /* 菜单搜索：按文本过滤 nav-item / nav-submenu-item */
    function filterNav(keyword) {
        var sidebar = $('#main-sidebar');
        if (!sidebar) return;
        var q = String(keyword || '').trim().toLowerCase();
        var items = $$('.nav-item', sidebar);

        items.forEach(function (item) {
            item.classList.remove('m-nav-hidden');
            var subs = $$('.nav-submenu-item', item);
            /* 父项只按自身标题匹配，避免子项文本把父项误判为命中 */
            var content = item.querySelector('.nav-item-content');
            var selfText = ((content ? content.textContent : item.textContent) || '').toLowerCase();
            var selfHit = !q || selfText.indexOf(q) >= 0;
            var subHit = false;

            subs.forEach(function (sub) {
                var hit = !q || (sub.textContent || '').toLowerCase().indexOf(q) >= 0;
                sub.classList.toggle('m-nav-hit', !!q && hit);
                /* 父项自身命中时整组保留，避免展开后是空组 */
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
                /* 命中子项时自动展开分组 */
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

        /* 分区标题：整组都被隐藏时一并隐藏 */
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
       7. 底部标签栏
       --------------------------------------------------------------------- */
    function buildTabbar() {
        var bar = el('nav');
        bar.id = 'm-tabbar';
        bar.setAttribute('aria-label', '主导航');

        TABS.forEach(function (tab) {
            var btn = el('button', 'm-tab');
            btn.type = 'button';
            btn.dataset.tab = tab.id;
            btn.innerHTML = ICON[tab.icon] + '<span>' + tab.label + '</span>';
            if (tab.badge) {
                var badge = el('span', 'm-tab-badge');
                badge.dataset.badgeFor = tab.badge;
                badge.style.display = 'none';
                btn.appendChild(badge);
            }
            btn.addEventListener('click', function () {
                if (tab.id === '__more') {
                    if (state.sheetOpen) closeSheet();
                    else openSheet();
                    return;
                }
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
        /* C2 / 资产 / 漏洞 / 设置 等页面归入"更多" */
        $$('.m-tab', bar).forEach(function (btn) {
            var id = btn.dataset.tab;
            var active = id === target;
            btn.classList.toggle('m-active', active);
            if (active) btn.setAttribute('aria-current', 'page');
            else btn.removeAttribute('aria-current');
        });
    }

    /* ---------------------------------------------------------------------
       8. 底部滑出面板
       --------------------------------------------------------------------- */
    function buildSheetItem(label, icon, onClick, opts) {
        opts = opts || {};
        var btn = el('button', 'm-sheet-item' + (opts.danger ? ' m-danger' : ''), ICON[icon] + '<span>' + label + '</span>');
        btn.type = 'button';
        btn.addEventListener('click', function () {
            onClick();
            if (!opts.keepOpen) closeSheet();
        });
        if (opts.id) btn.id = opts.id;
        return btn;
    }

    function injectSheet() {
        if ($('#m-sheet')) return;
        var sheet = el('div');
        sheet.id = 'm-sheet';
        sheet.setAttribute('role', 'dialog');
        sheet.setAttribute('aria-modal', 'true');
        sheet.setAttribute('aria-label', '更多操作');
        sheet.classList.add('m-mobile-only');

        var grip = el('div', 'm-sheet-grip');
        var head = el('div', 'm-sheet-head');
        head.appendChild(el('span', 'm-sheet-title', '更多操作'));
        var close = el('button', 'm-close-btn', ICON.close);
        close.type = 'button';
        close.setAttribute('aria-label', '关闭');
        close.addEventListener('click', closeSheet);
        head.appendChild(close);

        var body = el('div', 'm-sheet-body');

        body.appendChild(buildSheetItem('API 文档', 'doc', function () {
            window.open('/api-docs', '_blank');
        }));
        body.appendChild(buildSheetItem('GitHub', 'github', function () {
            window.open('https://github.com/AIPentest/CyberStrikeAI', '_blank');
        }));
        body.appendChild(buildSheetItem('切换主题', 'theme', function () {
            if (typeof window.cycleThemePreference === 'function') {
                window.cycleThemePreference();
            } else {
                var t = document.getElementById('theme-toggle-btn');
                if (t) t.click();
            }
            toast('主题：' + (document.documentElement.getAttribute('data-theme') === 'dark' ? '深色' : '浅色'));
        }, { keepOpen: true }));
        body.appendChild(buildSheetItem('切换语言', 'lang', function () {
            var cur = (document.documentElement.getAttribute('lang') || 'zh-CN').toLowerCase();
            var next = cur.indexOf('zh') === 0 ? 'en-US' : 'zh-CN';
            if (typeof window.onLanguageSelect === 'function') window.onLanguageSelect(next);
            toast(next.indexOf('zh') === 0 ? '已切换为中文' : 'Switched to English');
        }, { keepOpen: true }));

        body.appendChild(el('div', 'm-sheet-sep'));

        body.appendChild(buildSheetItem('返回顶部', 'top', scrollActiveToTop, { keepOpen: true }));
        body.appendChild(buildSheetItem('刷新页面', 'refresh', function () {
            location.reload();
        }));
        body.appendChild(buildSheetItem('全屏', 'full', function () {
            try {
                if (!document.fullscreenElement) {
                    (document.documentElement.requestFullscreen || function () {}).call(document.documentElement);
                } else {
                    document.exitFullscreen();
                }
            } catch (e) {
                toast('当前浏览器不支持全屏');
            }
        }, { keepOpen: true }));
        body.appendChild(buildSheetItem('退出登录', 'logout', function () {
            if (typeof window.logout === 'function') window.logout();
        }, { danger: true }));

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
       9. 对话页移动端工具条
       --------------------------------------------------------------------- */
    function injectChatBar() {
        var container = $('.chat-container');
        if (!container) return;
        if ($('#m-chat-bar', container)) return;

        var bar = el('div', 'm-chat-bar m-mobile-only');
        bar.id = 'm-chat-bar';

        var listBtn = el('button', 'm-chat-bar-btn', ICON.list + '<span>会话</span>');
        listBtn.type = 'button';
        listBtn.addEventListener('click', openChatDrawer);

        var spacer = el('div', 'm-chat-bar-spacer');

        var newBtn = el('button', 'm-chat-bar-btn m-primary', ICON.plus + '<span>新对话</span>');
        newBtn.type = 'button';
        newBtn.setAttribute('data-require-permission', 'chat:write');
        newBtn.addEventListener('click', function () {
            if (typeof window.startNewConversation === 'function') {
                window.startNewConversation();
            } else {
                toast('暂未加载对话模块');
            }
        });

        bar.appendChild(listBtn);
        bar.appendChild(spacer);
        bar.appendChild(newBtn);
        container.insertBefore(bar, container.firstChild);
    }

    /* ---------------------------------------------------------------------
       10. 表格横向滚动包装
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
       11. 路由挂钩
       --------------------------------------------------------------------- */
    function patchRouter() {
        if (typeof window.switchPage !== 'function' || window.switchPage.__mPatched) return;
        var original = window.switchPage;
        var patched = function (pageId) {
            var result = original.apply(this, arguments);
            try {
                onPageChanged(pageId);
            } catch (e) {
                /* 移动层异常不影响主流程 */
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
        } catch (e) { /* 隐私模式下忽略 */ }

        /* 页面渲染是异步的，稍后再做表格包装 */
        [80, 400, 1200].forEach(function (delay) {
            setTimeout(function () {
                var page = document.getElementById('page-' + pageId);
                if (page) wrapTables(page);
            }, delay);
        });
    }

    /* ---------------------------------------------------------------------
       12. 待审批角标（HITL）
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
       13. 登录态与底部栏可见性
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
       14. 手势
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
            /* body 已滚动时优先滚列表；滚到顶或按在把手/头部时允许下拉关闭 */
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
            /* 部分环境下 touchend 的 changedTouches 为空，回退到最后一帧的坐标 */
            var endY = (e.changedTouches && e.changedTouches[0]) ? e.changedTouches[0].clientY : lastY;
            var dy = endY - startY;
            dragging = false;
            if (dy > 70) closeSheet();
            else sheet.style.transform = '';
        }, { passive: true });
    }

    /* 从屏幕左缘右滑打开抽屉 */
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

    /* 双击缩放由 CSS touch-action: manipulation 抑制（见 mobile.css），
       原先的全局非 passive touchend 监听会阻塞滚动，已移除 */

    /* ---------------------------------------------------------------------
       15. 导航点击后自动收起抽屉
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
       16. 表格 MutationObserver
       --------------------------------------------------------------------- */
    function watchDom() {
        var area = $('.content-area');
        if (!area) return;
        var timer = null;
        var observer = new MutationObserver(function (records) {
            /* 只关心可能引入表格的变更：聊天流式输出会持续改动 DOM，
               若每次变动都全页扫表，长会话下会明显掉帧 */
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
       17. 断点切换
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
       18. 启动
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

        /* 恢复上次所在页面高亮 */
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

            /* 待审批角标：首次 + 每 45s + 回到前台时刷新 */
            refreshHitlBadge();
            setInterval(refreshHitlBadge, 45000);
            document.addEventListener('visibilitychange', function () {
                if (!document.hidden) refreshHitlBadge();
            });
        }

        wrapTables($('.page.active') || document);
    }

    /* 手机端有遮罩兜底，平板竖屏没有移动端外壳（boot 在非移动端直接返回），
       三个选择面板同样是贴底抽屉，这里用 document 级委托补上「点面板外关闭」，
       让 768–1023.98px 也有一条不依赖 × 按钮的退路。 */
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
            /* 平板竖屏：面板仍贴底（mobile.css 区块 22），但 watchSelectionPanels 只在 boot 内注册 */
            watchSelectionPanels();
        }
        bindSelectionOutsideClose();
    }

    /* 对外暴露，便于调试与后续扩展 */
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
