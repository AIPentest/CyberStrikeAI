(function () {
    const STORAGE_LAST_SEEN_KEY = 'cyberstrike-notification-last-seen-at';
    const POLL_INTERVAL_ACTIVE_MS = 15000;
    const POLL_INTERVAL_HIDDEN_MS = 60000;
    const MAX_RENDER_ITEMS = 20;

    const state = {
        inFlight: false,
        timerId: null,
        dropdownOpen: false,
        lastSeenAt: readLastSeenAt(),
        items: [],
        unreadCount: 0,
    };

    function readLastSeenAt() {
        try {
            const raw = localStorage.getItem(STORAGE_LAST_SEEN_KEY);
            const n = Number(raw);
            if (Number.isFinite(n) && n > 0) return n;
        } catch (e) {
            console.warn('Failed to read the notification read-time:', e);
        }
        return 0;
    }

    function persistLastSeenAt(ts) {
        try {
            localStorage.setItem(STORAGE_LAST_SEEN_KEY, String(ts));
        } catch (e) {
            console.warn('Failed to save the notification read-time:', e);
        }
    }

    function getTimeMs(value) {
        if (!value) return 0;
        const d = new Date(value);
        const ms = d.getTime();
        return Number.isFinite(ms) ? ms : 0;
    }

    function getLocale() {
        if (typeof window !== 'undefined') {
            if (typeof window.__locale === 'string' && window.__locale) {
                return window.__locale;
            }
            if (typeof window.currentLang === 'string' && window.currentLang) {
                return window.currentLang;
            }
        }
        return 'en-US';
    }

    function formatTime(value) {
        const ms = getTimeMs(value);
        if (!ms) return '-';
        return new Date(ms).toLocaleString(getLocale());
    }

    function htmlEscape(value) {
        if (typeof window.escapeHtml === 'function') {
            return window.escapeHtml(value == null ? '' : String(value));
        }
        const div = document.createElement('div');
        div.textContent = value == null ? '' : String(value);
        return div.innerHTML;
    }

    function t(key, fallback, params) {
        if (typeof window !== 'undefined' && typeof window.t === 'function') {
            try {
                const translated = window.t(key, params || {});
                if (translated && translated !== key) return translated;
            } catch (_ignored) {}
        }
        return fallback;
    }

    async function apiJson(url, options) {
        if (typeof window.apiFetch !== 'function') return null;
        const res = await window.apiFetch(url, options || {});
        if (!res.ok) return null;
        return res.json();
    }

    async function fetchNotificationSummary() {
        const url = '/api/notifications/summary?since='
            + encodeURIComponent(String(state.lastSeenAt || 0))
            + '&limit=80&lang=' + encodeURIComponent(getLocale());
        try {
            const summary = await apiJson(url);
            if (summary && typeof summary === 'object') {
                return summary;
            }
        } catch (_ignored) {}
        return null;
    }

    function renderBadge(count) {
        const badge = document.getElementById('notification-badge');
        const btn = document.getElementById('notification-bell-btn');
        if (!badge || !btn) return;
        if (count <= 0) {
            badge.style.display = 'none';
            btn.classList.remove('has-alert');
            return;
        }
        const text = count > 99 ? '99+' : String(count);
        badge.innerHTML = '<span class="notification-badge-text">' + htmlEscape(text) + '</span>';
        badge.style.display = 'inline-block';
        btn.classList.add('has-alert');
    }

    function countP0(items) {
        return (Array.isArray(items) ? items : []).reduce((acc, item) => {
            if (!item || item.level !== 'p0') return acc;
            if (typeof item.count === 'number' && item.count > 0) return acc + item.count;
            return acc + 1;
        }, 0);
    }

    function markableItems(items) {
        return (Array.isArray(items) ? items : []).filter(item => item && item.actionable !== true && item.id);
    }

    function hasAction(item) {
        if (!item || !item.type) return false;
        if (item.type === 'vulnerability_created' && item.vulnerabilityId) return true;
        if ((item.type === 'task_completed' || item.type === 'long_running_tasks') && item.conversationId) return true;
        if (item.type === 'task_failed' && item.executionId) return true;
        if (item.type === 'hitl_pending') return true;
        if (item.type === 'c2_session_online' && item.sessionId) return true;
        return false;
    }

    function openNotificationTarget(item) {
        if (!item || !item.type) return;
        if (item.type === 'vulnerability_created' && item.vulnerabilityId) {
            window.location.hash = 'vulnerabilities?id=' + encodeURIComponent(item.vulnerabilityId);
            return;
        }
        if ((item.type === 'task_completed' || item.type === 'long_running_tasks') && item.conversationId) {
            // 统一走路由提供的会话导航入口.直接修改 hash 会先触发 hashchange
            // 切pages, 再由路由延迟加载会话, 视觉上会多闪一次.
            if (typeof window.navigateToConversation === 'function') {
                window.navigateToConversation(item.conversationId);
            } else {
                window.location.hash = 'chat?conversation=' + encodeURIComponent(item.conversationId);
            }
            return;
        }
        if (item.type === 'task_failed' && item.executionId) {
            window.location.hash = 'mcp-monitor';
            setTimeout(function () {
                if (typeof showMCPDetail === 'function') {
                    showMCPDetail(item.executionId);
                }
            }, 450);
            return;
        }
        if (item.type === 'hitl_pending') {
            window.location.hash = 'hitl';
            return;
        }
        if (item.type === 'c2_session_online' && item.sessionId) {
            if (typeof window.switchPage === 'function') {
                window.switchPage('c2-sessions');
            } else {
                window.location.hash = 'c2-sessions';
            }
            const sid = item.sessionId;
            window.setTimeout(function () {
                if (typeof C2 === 'undefined' || !C2.loadSessions || !C2.selectSession) return;
                var p = C2.loadSessions();
                if (p && typeof p.then === 'function') {
                    p.then(function () { C2.selectSession(sid); }).catch(function () {});
                } else {
                    window.setTimeout(function () { try { C2.selectSession(sid); } catch (e) {} }, 500);
                }
            }, 120);
        }
    }

    async function markItemsRead(eventIds) {
        if (!Array.isArray(eventIds) || !eventIds.length) return true;
        const payload = { eventIds: eventIds };
        try {
            const result = await apiJson('/api/notifications/read', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });
            return !!result;
        } catch (_ignored) {
            return false;
        }
    }

    function renderNotificationList(items) {
        const list = document.getElementById('notification-list');
        if (!list) return;
        const renderItems = Array.isArray(items) ? items.slice(0, MAX_RENDER_ITEMS) : [];
        if (!renderItems.length) {
            list.innerHTML = '<div class="notification-empty">' + htmlEscape(t('notifications.empty', 'No new events')) + '</div>';
            return;
        }
        const html = renderItems.map(item => {
            const canMarkRead = item.actionable !== true && !!item.id;
            const canView = hasAction(item);
            return `
                <div class="notification-item notification-level-${htmlEscape(item.level || 'p2')}">
                    <div class="notification-item-header">
                        <div class="notification-item-title">${htmlEscape(item.title || t('notifications.itemDefaultTitle', 'Notifications'))}</div>
                        <div class="notification-item-actions">
                            ${canView ? `<button class="notification-item-action-btn notification-item-view-btn" type="button" data-action-id="${htmlEscape(item.id || '')}">${htmlEscape(t('common.view', 'View'))}</button>` : ''}
                            ${canMarkRead ? `<button class="notification-item-action-btn notification-item-read-btn" type="button" data-notification-id="${htmlEscape(item.id)}">${htmlEscape(t('notifications.markSingleRead', 'Read'))}</button>` : ''}
                        </div>
                    </div>
                    <div class="notification-item-desc">${htmlEscape(item.desc || '')}</div>
                    <div class="notification-item-time">${htmlEscape(formatTime(item.ts))}</div>
                </div>
            `;
        }).join('');
        list.innerHTML = html;
        const viewButtons = list.querySelectorAll('.notification-item-view-btn');
        viewButtons.forEach(btn => {
            btn.addEventListener('click', async function (event) {
                event.preventDefault();
                event.stopPropagation();
                const eventID = btn.getAttribute('data-action-id') || '';
                if (!eventID) return;
                const item = state.items.find(it => it && it.id === eventID);
                if (!item) return;
                openNotificationTarget(item);
                closeDropdown();
                if (item.actionable !== true && await markItemsRead([eventID])) {
                    await refreshNotifications();
                }
            });
        });
        const readButtons = list.querySelectorAll('.notification-item-read-btn');
        readButtons.forEach(btn => {
            btn.addEventListener('click', async function (event) {
                event.preventDefault();
                event.stopPropagation();
                const eventID = btn.getAttribute('data-notification-id') || '';
                if (!eventID) return;
                const ok = await markItemsRead([eventID]);
                if (ok) {
                    await refreshNotifications();
                }
            });
        });
    }

    function closeDropdown() {
        const dropdown = document.getElementById('notification-dropdown');
        const bellBtn = document.getElementById('notification-bell-btn');
        if (dropdown) dropdown.style.display = 'none';
        if (bellBtn) bellBtn.classList.remove('active');
        state.dropdownOpen = false;
    }

    function markSeenNow() {
        state.lastSeenAt = Date.now();
        persistLastSeenAt(state.lastSeenAt);
    }

    async function refreshNotifications() {
        if (state.inFlight) return;
        state.inFlight = true;
        try {
            const summary = await fetchNotificationSummary();
            const items = summary && Array.isArray(summary.items) ? summary.items : [];
            state.items = items;
            const unreadCount = summary && Number.isFinite(Number(summary.unreadCount))
                ? Number(summary.unreadCount)
                : countP0(items);
            state.unreadCount = Math.max(0, unreadCount);
            renderBadge(state.unreadCount);
            renderNotificationList(items);
        } catch (e) {
            console.warn('Failed to refresh notifications:', e);
        } finally {
            state.inFlight = false;
        }
    }

    function scheduleNextPoll() {
        if (state.timerId) {
            window.clearTimeout(state.timerId);
            state.timerId = null;
        }
        const interval = document.hidden ? POLL_INTERVAL_HIDDEN_MS : POLL_INTERVAL_ACTIVE_MS;
        state.timerId = window.setTimeout(async function () {
            await refreshNotifications();
            scheduleNextPoll();
        }, interval);
    }

    function handleDocumentClick(event) {
        const container = document.querySelector('.notification-menu-container');
        if (!container) return;
        if (!container.contains(event.target)) {
            closeDropdown();
        }
    }

    async function toggleDropdown() {
        const dropdown = document.getElementById('notification-dropdown');
        const bellBtn = document.getElementById('notification-bell-btn');
        if (!dropdown || !bellBtn) return;
        const isOpen = dropdown.style.display !== 'none';
        if (isOpen) {
            closeDropdown();
            return;
        }
        // 从仪表盘'View全部'等容器外入口打开时, 同一 click 会冒泡到 document, 
        // handleDocumentClick 会误判为'点在外面'并立刻关掉.推迟到宏任务再展开即可.
        const runOpen = async function () {
            if (dropdown.style.display !== 'none') return;
            dropdown.style.display = 'block';
            bellBtn.classList.add('active');
            state.dropdownOpen = true;
            await refreshNotifications();
        };
        window.setTimeout(function () {
            void runOpen();
        }, 0);
    }

    async function markAllSeen() {
        const ids = markableItems(state.items).map(item => item.id);
        const ok = await markItemsRead(ids);
        if (ok) {
            markSeenNow();
            await refreshNotifications();
        }
    }

    function initNotifications() {
        const bellBtn = document.getElementById('notification-bell-btn');
        if (!bellBtn) return;
        document.addEventListener('click', handleDocumentClick);
        document.addEventListener('visibilitychange', scheduleNextPoll);
        document.addEventListener('languagechange', function () {
            refreshNotifications();
        });
        refreshNotifications();
        scheduleNextPoll();
    }

    window.toggleNotificationDropdown = toggleDropdown;
    window.markAllNotificationsSeen = markAllSeen;

    document.addEventListener('DOMContentLoaded', initNotifications);
})();
