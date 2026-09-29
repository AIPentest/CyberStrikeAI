/**
 * Mobile adaptation behaviour: off-canvas navigation drawer + chat conversation drawer.
 * Purely additive — desktop behaviour is untouched (all logic is gated on a media query).
 */
(function () {
    'use strict';

    var MQ = '(max-width: 768px)';
    function isMobile() {
        return window.matchMedia && window.matchMedia(MQ).matches;
    }

    function el(id) { return document.getElementById(id); }

    /* ---------------- main navigation drawer ---------------- */

    function setNav(open) {
        var sidebar = el('main-sidebar');
        var btn = el('mobile-menu-btn');
        if (!sidebar) return;
        sidebar.classList.toggle('mobile-open', open);
        document.body.classList.toggle('cs-nav-open', open);
        if (btn) {
            btn.setAttribute('aria-expanded', open ? 'true' : 'false');
            if (open) {
                var close = sidebar.querySelector('.mobile-drawer-close');
                if (close) close.focus();
            } else {
                try { btn.focus(); } catch (e) { /* not focusable yet */ }
            }
        }
    }

    function closeNav() {
        if (document.body.classList.contains('cs-nav-open')) setNav(false);
    }

    function toggleNav() {
        var sidebar = el('main-sidebar');
        if (!sidebar) return;
        setNav(!sidebar.classList.contains('mobile-open'));
    }

    // The desktop 64px icon rail depends on hover tooltips; on a phone the drawer is always expanded.
    function expandSidebarForMobile() {
        var sidebar = el('main-sidebar');
        if (sidebar && isMobile()) sidebar.classList.remove('collapsed');
    }

    /* ---------------- chat conversation drawer ---------------- */

    function convSidebar() {
        return el('conversation-sidebar');
    }

    function setConv(open) {
        var sb = convSidebar();
        if (!sb) return;
        sb.classList.toggle('mobile-open', open);
        document.body.classList.toggle('cs-conv-open', open);
        var t = el('mobile-conv-btn');
        if (t) t.setAttribute('aria-expanded', open ? 'true' : 'false');
    }

    function toggleConv() {
        var sb = convSidebar();
        if (!sb) return;
        setConv(!sb.classList.contains('mobile-open'));
    }

    function closeConv() {
        if (document.body.classList.contains('cs-conv-open')) setConv(false);
    }

    function ensureConvButton() {
        if (el('mobile-conv-btn')) return;
        var page = el('page-chat');
        if (!page) return;
        var host = page.querySelector('.chat-page-layout') || page.querySelector('.chat-container') || page;
        if (!host) return;
        if (getComputedStyle(host).position === 'static') host.style.position = 'relative';
        var b = document.createElement('button');
        b.type = 'button';
        b.id = 'mobile-conv-btn';
        b.className = 'mobile-conv-btn';
        b.setAttribute('aria-label', '会话列表');
        b.setAttribute('aria-expanded', 'false');
        b.innerHTML = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
            '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>' +
            '<span>会话</span>';
        b.addEventListener('click', toggleConv);
        host.appendChild(b);
    }

    /* ---------------- global wiring ---------------- */

    function onRouteChange() {
        closeNav();
        closeConv();
        expandSidebarForMobile();
    }

    function bindSwipe(node, onOpen, onClose) {
        var x0 = null, y0 = null, tracking = false;
        node.addEventListener('touchstart', function (e) {
            if (e.touches.length !== 1) return;
            x0 = e.touches[0].clientX;
            y0 = e.touches[0].clientY;
            tracking = true;
        }, { passive: true });

        node.addEventListener('touchend', function (e) {
            if (!tracking || x0 === null) return;
            tracking = false;
            var dx = e.changedTouches[0].clientX - x0;
            var dy = e.changedTouches[0].clientY - y0;
            if (Math.abs(dx) < 62 || Math.abs(dy) > Math.abs(dx) * 0.8) return;
            if (dx > 0 && x0 < 34) onOpen();
            else if (dx < 0) onClose();
        }, { passive: true });
    }

    function init() {
        expandSidebarForMobile();
        ensureConvButton();

        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            closeNav();
            closeConv();
        });

        // Tapping a nav entry should dismiss the drawer so the result is visible.
        var sidebar = el('main-sidebar');
        if (sidebar) {
            sidebar.addEventListener('click', function (e) {
                if (!isMobile()) return;
                if (e.target.closest('.nav-item-content, .nav-submenu-item')) {
                    // let submenus expand in place; only leaf pages dismiss the drawer
                    if (!e.target.closest('.nav-item-has-submenu') || e.target.closest('.nav-submenu-item')) {
                        window.setTimeout(closeNav, 90);
                    }
                }
            });
            bindSwipe(sidebar, function () {}, closeNav);
        }

        var sb = convSidebar();
        if (sb) bindSwipe(sb, function () {}, closeConv);

        var backdrop = el('cs-drawer-backdrop');
        if (backdrop) bindSwipe(backdrop, function () {}, closeNav);

        window.addEventListener('hashchange', onRouteChange);
        if (window.matchMedia) {
            var mq = window.matchMedia(MQ);
            var handler = function () {
                if (!isMobile()) {
                    closeNav();
                    closeConv();
                } else {
                    expandSidebarForMobile();
                }
            };
            if (mq.addEventListener) mq.addEventListener('change', handler);
            else if (mq.addListener) mq.addListener(handler);
        }

        // Keep the drawer dismissed when a full-screen sheet opens over it.
        document.addEventListener('click', function (e) {
            if (e.target.closest && e.target.closest('.page-content, .content-area') && document.body.classList.contains('cs-nav-open')) {
                closeNav();
            }
        });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

    window.CyberStrikeMobile = {
        isMobile: isMobile,
        toggleNav: toggleNav,
        openNav: function () { setNav(true); },
        closeNav: closeNav,
        toggleConv: toggleConv,
        closeConv: closeConv,
        refresh: onRouteChange
    };
})();
