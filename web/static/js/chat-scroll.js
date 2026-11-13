/**
 * 主对话区智能粘底滚动：流式输出时自动跟随，用户上滑阅读时不抢焦点。
 * 主 POST 流（sendMessage）与刷新后 task-events 补流共用同一策略。
 */
(function () {
    'use strict';

    /** 距底部在此范围内才继续自动跟随（宜小，避免“差一点也被拽回去”） */
    const CHAT_SCROLL_FOLLOW_THRESHOLD_PX = 48;
    /** 只有真正到达底部才恢复跟随；2px 用于兼容高分屏的亚像素滚动。 */
    const CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX = 2;
    /** 到达此范围视为位于最后一轮 */
    const CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX = 120;
    /** 用户上滑后的短暂锁，防止 SSE 与 scroll 事件竞态抢滚动 */
    const DETACH_LOCK_MS = 900;
    /** 刷新恢复会跨越历史消息、过程详情、字体与流订阅等多轮异步布局。 */
    const CONVERSATION_RESTORE_SETTLE_MIN_MS = 3000;
    const CONVERSATION_RESTORE_SETTLE_MAX_MS = 6000;
    const CONVERSATION_RESTORE_STABLE_FRAMES = 12;

    /** @type {'following' | 'detached'} */
    let scrollMode = 'following';
    let scrollFollowRaf = 0;
    let scrollSettleGeneration = 0;
    let conversationRestoreGeneration = 0;
    /** 用户脱离跟随后，下方是否有未读的新输出（不按 SSE 次数计） */
    let hasPendingNewBelow = false;
    let listenersBound = false;
    let lastScrollTop = 0;
    let lastScrollRange = 0;
    let detachLockUntil = 0;
    /** 最近一次由用户发起的滚动意图；布局变化或脚本滚动不得据此恢复粘底。 */
    let userScrollIntentUntil = 0;
    let turnRailRefreshRaf = 0;
    let turnRailSignature = '';
    let activeTurnIndex = -1;
    let turnRailObserver = null;
    let chatMessagesResizeObserver = null;
    let turnPreviewHideTimer = 0;

    function getChatMessagesEl() {
        return document.getElementById('chat-messages');
    }

    function getTurnRailEl() {
        return document.getElementById('chat-turn-rail');
    }

    function getTurnRailMarkersEl() {
        return document.getElementById('chat-turn-rail-markers');
    }

    function getReturnLatestButton() {
        return document.getElementById('chat-return-latest');
    }

    function normalizePreviewText(value) {
        return String(value || '').replace(/\s+/g, ' ').trim();
    }

    function trimPreviewText(value, maxLength) {
        const text = normalizePreviewText(value);
        if (text.length <= maxLength) return text;
        return text.slice(0, Math.max(1, maxLength - 1)).trimEnd() + '…';
    }

    function messagePreviewText(messageEl) {
        if (!messageEl) return '';
        const original = messageEl.dataset ? messageEl.dataset.originalContent : '';
        if (original) return normalizePreviewText(original);
        const bubble = messageEl.querySelector('.assistant-final-result, .message-bubble');
        if (!bubble) return '';
        const clone = bubble.cloneNode(true);
        clone.querySelectorAll('button, .message-copy-btn, .progress-actions, .progress-footer, .process-details-content').forEach(function (el) {
            el.remove();
        });
        return normalizePreviewText(clone.textContent);
    }

    /** 每条用户消息开始一轮，直到下一条用户消息前的助手消息都归入该轮。 */
    function collectConversationTurns() {
        const messagesEl = getChatMessagesEl();
        if (!messagesEl) return [];
        const turns = [];
        let currentTurn = null;
        Array.from(messagesEl.children).forEach(function (messageEl) {
            if (!messageEl.classList || !messageEl.classList.contains('message')) return;
            if (messageEl.classList.contains('user')) {
                currentTurn = { user: messageEl, assistants: [] };
                turns.push(currentTurn);
                return;
            }
            if (currentTurn && messageEl.classList.contains('assistant')) {
                currentTurn.assistants.push(messageEl);
            }
        });
        return turns;
    }

    function localizedTurnLabel(index, question) {
        const number = index + 1;
        const prefix = typeof window.t === 'function'
            ? window.t('chat.turnNumber', { number: number })
            : '第 ' + number + ' 轮';
        const safePrefix = prefix && prefix !== 'chat.turnNumber' ? prefix : ('第 ' + number + ' 轮');
        return question ? safePrefix + '：' + question : safePrefix;
    }

    function turnPreviewData(turn, index) {
        const question = trimPreviewText(messagePreviewText(turn && turn.user), 100)
            || localizedTurnLabel(index, '');
        const assistants = turn && turn.assistants ? turn.assistants : [];
        let assistant = null;
        for (let i = assistants.length - 1; i >= 0; i--) {
            if (!assistants[i].classList.contains('progress-message')) {
                assistant = assistants[i];
                break;
            }
        }
        if (!assistant && assistants.length) assistant = assistants[assistants.length - 1];
        let summary = trimPreviewText(messagePreviewText(assistant), 220);
        if (!summary) {
            summary = typeof window.t === 'function' ? window.t('chat.turnPending') : '正在处理…';
            if (!summary || summary === 'chat.turnPending') summary = '正在处理…';
        }
        return { question: question, summary: summary };
    }

    function hideTurnPreview() {
        if (turnPreviewHideTimer) {
            window.clearTimeout(turnPreviewHideTimer);
            turnPreviewHideTimer = 0;
        }
        const preview = document.getElementById('chat-turn-rail-preview');
        if (preview) preview.hidden = true;
    }

    function scheduleHideTurnPreview() {
        if (turnPreviewHideTimer) window.clearTimeout(turnPreviewHideTimer);
        turnPreviewHideTimer = window.setTimeout(hideTurnPreview, 160);
    }

    function showTurnPreview(marker, index) {
        if (turnPreviewHideTimer) {
            window.clearTimeout(turnPreviewHideTimer);
            turnPreviewHideTimer = 0;
        }
        const preview = document.getElementById('chat-turn-rail-preview');
        const title = document.getElementById('chat-turn-rail-preview-title');
        const summary = document.getElementById('chat-turn-rail-preview-summary');
        const turn = collectConversationTurns()[index];
        if (!preview || !title || !summary || !marker || !turn) return;

        const data = turnPreviewData(turn, index);
        title.textContent = data.question;
        summary.textContent = data.summary;
        preview.hidden = false;

        const markerRect = marker.getBoundingClientRect();
        const previewRect = preview.getBoundingClientRect();
        const left = Math.min(markerRect.right + 18, window.innerWidth - previewRect.width - 12);
        const desiredTop = markerRect.top + markerRect.height / 2 - previewRect.height / 2;
        const top = Math.max(12, Math.min(desiredTop, window.innerHeight - previewRect.height - 12));
        preview.style.left = Math.max(12, left) + 'px';
        preview.style.top = top + 'px';
    }

    function setActiveTurnMarker(index) {
        const markersEl = getTurnRailMarkersEl();
        if (!markersEl) return;
        const markers = Array.from(markersEl.querySelectorAll('.chat-turn-rail-marker'));
        if (!markers.length) return;
        const nextIndex = Math.max(0, Math.min(index, markers.length - 1));
        markers.forEach(function (marker, markerIndex) {
            const active = markerIndex === nextIndex;
            marker.classList.toggle('is-active', active);
            if (active) marker.setAttribute('aria-current', 'step');
            else marker.removeAttribute('aria-current');
        });
        markers[markers.length - 1].classList.toggle('has-pending-new', hasPendingNewBelow);

        if (activeTurnIndex !== nextIndex) {
            activeTurnIndex = nextIndex;
            const activeMarker = markers[nextIndex];
            const markerTop = activeMarker.offsetTop;
            const markerBottom = markerTop + activeMarker.offsetHeight;
            if (markerTop < markersEl.scrollTop) {
                markersEl.scrollTop = Math.max(0, markerTop - 8);
            } else if (markerBottom > markersEl.scrollTop + markersEl.clientHeight) {
                markersEl.scrollTop = markerBottom - markersEl.clientHeight + 8;
            }
        }
    }

    function updateTurnRailActive() {
        const messagesEl = getChatMessagesEl();
        const turns = collectConversationTurns();
        if (!messagesEl || !turns.length) return;
        if (isNearBottom(CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX)) {
            setActiveTurnMarker(turns.length - 1);
            return;
        }
        const readingLine = messagesEl.scrollTop + messagesEl.clientHeight * 0.34;
        let index = 0;
        for (let i = 0; i < turns.length; i++) {
            if (turns[i].user.offsetTop <= readingLine) index = i;
            else break;
        }
        setActiveTurnMarker(index);
    }

    function jumpToConversationTurn(index) {
        const messagesEl = getChatMessagesEl();
        const turn = collectConversationTurns()[index];
        if (!messagesEl || !turn || !turn.user) return;
        userScrollIntentUntil = 0;
        setScrollDetached();
        messagesEl.scrollTo({
            top: Math.max(0, turn.user.offsetTop - 20),
            behavior: 'instant'
        });
        rememberScrollPosition(messagesEl);
        setActiveTurnMarker(index);
        hideTurnPreview();
        updateReturnLatestButton();
    }

    function focusTurnMarker(index) {
        const markersEl = getTurnRailMarkersEl();
        const marker = markersEl && markersEl.querySelector('.chat-turn-rail-marker[data-turn-index="' + index + '"]');
        if (marker) marker.focus();
    }

    function rebuildTurnRail(force) {
        const rail = getTurnRailEl();
        const markersEl = getTurnRailMarkersEl();
        if (!rail || !markersEl) return;
        const turns = collectConversationTurns();
        rail.hidden = turns.length === 0;
        if (!turns.length) {
            markersEl.replaceChildren();
            turnRailSignature = '';
            activeTurnIndex = -1;
            hideTurnPreview();
            return;
        }

        const signature = turns.map(function (turn, index) {
            return (turn.user.id || ('turn-' + index)) + ':' + messagePreviewText(turn.user);
        }).join('|');
        if (!force && signature === turnRailSignature) {
            updateTurnRailActive();
            return;
        }

        const fragment = document.createDocumentFragment();
        turns.forEach(function (turn, index) {
            const marker = document.createElement('button');
            const question = trimPreviewText(messagePreviewText(turn.user), 88);
            marker.type = 'button';
            marker.className = 'chat-turn-rail-marker';
            marker.dataset.turnIndex = String(index);
            marker.setAttribute('aria-label', localizedTurnLabel(index, question));
            marker.addEventListener('click', function () {
                jumpToConversationTurn(index);
            });
            marker.addEventListener('mouseenter', function () {
                showTurnPreview(marker, index);
            });
            marker.addEventListener('mouseleave', scheduleHideTurnPreview);
            marker.addEventListener('keydown', function (event) {
                if (event.key === 'ArrowDown' || event.key === 'ArrowRight') {
                    event.preventDefault();
                    focusTurnMarker(Math.min(turns.length - 1, index + 1));
                } else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') {
                    event.preventDefault();
                    focusTurnMarker(Math.max(0, index - 1));
                } else if (event.key === 'Home') {
                    event.preventDefault();
                    focusTurnMarker(0);
                } else if (event.key === 'End') {
                    event.preventDefault();
                    focusTurnMarker(turns.length - 1);
                }
            });
            fragment.appendChild(marker);
        });
        markersEl.replaceChildren(fragment);
        turnRailSignature = signature;
        activeTurnIndex = -1;
        updateTurnRailActive();
    }

    function scheduleTurnRailRefresh(force) {
        cancelAnimationFrame(turnRailRefreshRaf);
        turnRailRefreshRaf = requestAnimationFrame(function () {
            rebuildTurnRail(force === true);
        });
    }

    function streamBelongsToVisibleConversation(stream) {
        if (!stream || !stream.active) return false;
        const visibleConversationId = typeof window.currentConversationId === 'string'
            ? window.currentConversationId.trim()
            : '';
        const streamConversationId = typeof stream.conversationId === 'string'
            ? stream.conversationId.trim()
            : '';

        // 新建对话在后端返回 conversationId 前，两边都为空，仍属于当前界面。
        if (!streamConversationId) return !visibleConversationId;
        return streamConversationId === visibleConversationId;
    }

    /** 只有当前可见对话的主 POST 流 / task-events 补流才视为「正在输出」 */
    function isStreamActive() {
        try {
            const live = window.__csAgentLiveStream;
            if (streamBelongsToVisibleConversation(live)) return true;
            const replay = window.__csTaskEventStream;
            return streamBelongsToVisibleConversation(replay);
        } catch (e) {
            return false;
        }
    }

    function isVisibleConversationTaskActive() {
        if (isStreamActive()) return true;
        try {
            const visibleConversationId = typeof window.currentConversationId === 'string'
                ? window.currentConversationId.trim()
                : '';
            if (!visibleConversationId) return false;
            const taskChecker = typeof window.isConversationTaskRunning === 'function'
                ? window.isConversationTaskRunning
                : (typeof isConversationTaskRunning === 'function' ? isConversationTaskRunning : null);
            return !!(taskChecker && taskChecker(visibleConversationId));
        } catch (e) {
            return false;
        }
    }

    function scrollRange(el) {
        return Math.max(0, el.scrollHeight - el.clientHeight);
    }

    function rememberScrollPosition(el) {
        lastScrollTop = el.scrollTop;
        lastScrollRange = scrollRange(el);
    }

    function distanceFromBottom(el) {
        if (!el) return 0;
        return scrollRange(el) - el.scrollTop;
    }

    function isNearBottom(thresholdPx) {
        const el = getChatMessagesEl();
        if (!el) return true;
        return distanceFromBottom(el) <= thresholdPx;
    }

    function isChatMessagesPinnedToBottom() {
        return isNearBottom(CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX);
    }

    /** 已在底部时恢复 following（解决：手动滚到底但 scrollMode 仍为 detached） */
    function resumeFollowingIfAtBottom(thresholdPx, userInitiated) {
        if (!userInitiated && Date.now() < detachLockUntil) return false;
        const threshold = Number.isFinite(Number(thresholdPx))
            ? Math.max(0, Number(thresholdPx))
            : CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX;
        if (!isNearBottom(threshold)) return false;
        // detached 是用户明确上滑后的阅读状态。布局变化、流式增高和模式切换
        // 即使让视口暂时接近底部，也不能自行恢复；只有用户明确向下滚到底才恢复。
        if (scrollMode === 'detached') {
            if (!userInitiated) return false;
            setScrollFollowing();
        }
        return true;
    }

    function captureScrollPinState() {
        if (Date.now() < detachLockUntil) return false;
        return scrollMode === 'following';
    }

    function setScrollFollowing() {
        scrollMode = 'following';
        getChatMessagesEl()?.classList.add('is-following-latest');
        detachLockUntil = 0;
        userScrollIntentUntil = 0;
        hasPendingNewBelow = false;
        updateTurnRailState();
    }

    function markPendingNewBelow() {
        if (scrollMode !== 'detached') return;
        hasPendingNewBelow = true;
        updateTurnRailState();
    }

    function setScrollDetached() {
        scrollMode = 'detached';
        getChatMessagesEl()?.classList.remove('is-following-latest');
        detachLockUntil = Date.now() + DETACH_LOCK_MS;
        cancelAnimationFrame(scrollFollowRaf);
        scrollFollowRaf = 0;
        scrollSettleGeneration += 1;
        conversationRestoreGeneration += 1;
        if (isStreamActive()) {
            hasPendingNewBelow = true;
        }
        updateTurnRailState();
    }

    function scrollChatToBottomInstant() {
        if (scrollMode !== 'following') return;
        const el = getChatMessagesEl();
        if (!el) return;
        const bottom = scrollRange(el);
        if (bottom - el.scrollTop > CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX) {
            el.scrollTo({ top: bottom, behavior: 'instant' });
            rememberScrollPosition(el);
        }
    }

    function updateTurnRailState() {
        updateTurnRailActive();
        updateReturnLatestButton();
    }

    function updateReturnLatestButton() {
        const button = getReturnLatestButton();
        const messagesEl = getChatMessagesEl();
        if (!button || !messagesEl) return;
        const scrollable = messagesEl.scrollHeight > messagesEl.clientHeight + 2;
        const shouldShow = scrollable && !isNearBottom(CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX);
        const streaming = shouldShow && isVisibleConversationTaskActive();
        button.hidden = !shouldShow;
        button.classList.toggle('is-streaming', streaming);
        button.classList.toggle('has-pending-new', shouldShow && hasPendingNewBelow);
    }

    function isolateReturnLatestPointerEvent(event) {
        if (!event) return;
        // 该按钮会在点击后立即隐藏。阻止指针事件继续冒泡，避免长历史对话中
        // 按钮隐藏与底部审批卡片重排发生在同一帧时产生点击穿透。
        event.stopPropagation();
    }

    function onReturnLatestClick(event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        forceScrollChatToBottom();
        const button = getReturnLatestButton();
        if (button) button.blur();
    }

    function canAutoScrollNow(wasPinnedBeforeDomUpdate) {
        if (Date.now() < detachLockUntil) return false;
        if (scrollMode === 'detached') return false;
        if (wasPinnedBeforeDomUpdate === true) return true;
        return isNearBottom(CHAT_SCROLL_FOLLOW_THRESHOLD_PX);
    }

    function scheduleChatScrollToBottomIfFollowing(wasPinnedBeforeDomUpdate) {
        if (!canAutoScrollNow(wasPinnedBeforeDomUpdate)) {
            markPendingNewBelow();
            return;
        }
        if (scrollFollowRaf) return;
        scrollFollowRaf = requestAnimationFrame(function () {
            scrollFollowRaf = 0;
            scrollChatToBottomInstant();
        });
    }

    /**
     * 长详情恢复/终态对账会跨多个 requestAnimationFrame 分批增高 DOM。
     * 单次滚底可能早于最后一批节点；在仍处于 following 时连续若干帧校准，
     * 用户一旦主动上滑进入 detached，后续帧立即停止，避免抢回阅读位置。
     */
    function settleChatToBottomIfFollowing(frameCount) {
        const frames = Number.isFinite(Number(frameCount))
            ? Math.max(1, Math.min(30, Math.floor(Number(frameCount))))
            : 12;
        const generation = ++scrollSettleGeneration;

        function settleFrame(remaining) {
            if (generation !== scrollSettleGeneration) return;
            if (scrollMode !== 'following' || Date.now() < detachLockUntil) return;
            scrollChatToBottomInstant();
            if (remaining > 1) {
                requestAnimationFrame(function () {
                    settleFrame(remaining - 1);
                });
            }
        }

        requestAnimationFrame(function () {
            settleFrame(frames);
        });
    }

    /**
     * 刷新恢复长会话时，消息、详情和审批卡会跨多帧继续增高。
     * 批次与审批恢复完成时沿用当前跟随状态。用户可以在此之前开始阅读历史。
     */
    function settleConversationRestoreToBottom(frameCount) {
        if (!captureScrollPinState()) return;
        const requestedFrames = Number.isFinite(Number(frameCount))
            ? Math.max(1, Math.floor(Number(frameCount)))
            : 30;
        const minimumDuration = Math.max(
            CONVERSATION_RESTORE_SETTLE_MIN_MS,
            Math.ceil(requestedFrames * (1000 / 60))
        );
        const generation = ++conversationRestoreGeneration;
        const startedAt = Date.now();
        let lastHeight = -1;
        let stableFrames = 0;

        function settleRestoreFrame() {
            if (generation !== conversationRestoreGeneration) return;
            // wheel / touch / keyboard / scrollbar drag 会进入 detached；立即尊重用户阅读位置。
            if (scrollMode !== 'following' || Date.now() < detachLockUntil) return;
            const el = getChatMessagesEl();
            if (!el) return;

            scrollChatToBottomInstant();
            const currentHeight = el.scrollHeight;
            if (currentHeight === lastHeight && isNearBottom(1)) {
                stableFrames += 1;
            } else {
                stableFrames = 0;
            }
            lastHeight = currentHeight;

            const elapsed = Date.now() - startedAt;
            const reachedStableMinimum = elapsed >= minimumDuration
                && stableFrames >= CONVERSATION_RESTORE_STABLE_FRAMES;
            if (!reachedStableMinimum && elapsed < CONVERSATION_RESTORE_SETTLE_MAX_MS) {
                requestAnimationFrame(settleRestoreFrame);
            }
        }

        requestAnimationFrame(settleRestoreFrame);
    }

    /** @param {boolean} wasPinned DOM 更新前是否应跟随（由 captureScrollPinState 传入） */
    function scrollChatMessagesToBottomIfPinned(wasPinned) {
        scheduleChatScrollToBottomIfFollowing(wasPinned);
    }

    function forceScrollChatToBottom() {
        cancelAnimationFrame(scrollFollowRaf);
        scrollFollowRaf = 0;
        setScrollFollowing();
        scrollChatToBottomInstant();
        updateTurnRailState();
    }

    function onUserSendMessage() {
        forceScrollChatToBottom();
    }

    function clearAllStreamingMarkers() {
        document.querySelectorAll('.progress-container.is-streaming, .process-details-container.is-streaming').forEach(function (el) {
            el.classList.remove('is-streaming');
        });
    }

    function markProgressStreaming(active, progressId) {
        if (!active) {
            clearAllStreamingMarkers();
            return;
        }
        if (!progressId) return;
        const progressEl = document.getElementById(progressId);
        const container = progressEl && progressEl.querySelector('.progress-container');
        if (container) container.classList.add('is-streaming');
    }

    function markProcessDetailsStreaming(active, assistantDomId) {
        if (!active) {
            document.querySelectorAll('.process-details-container.is-streaming').forEach(function (el) {
                el.classList.remove('is-streaming');
            });
            return;
        }
        if (!assistantDomId) return;
        const container = document.getElementById('process-details-' + assistantDomId);
        if (!container) return;
        container.classList.add('is-streaming');
        const timeline = container.querySelector('.progress-timeline');
        if (timeline) timeline.classList.add('expanded');
    }

    function onStreamEnd() {
        clearAllStreamingMarkers();
        try {
            window.__csTaskEventStream = { active: false, conversationId: null, assistantDomId: null, progressId: null };
        } catch (e) { /* ignore */ }
        scheduleTurnRailRefresh(true);
        updateTurnRailState();
    }

    /** 刷新后会话 task-events 补流开始时，与 sendMessage 主流程对齐 */
    function onTaskEventStreamBegin(conversationId, assistantDomId, progressId) {
        try {
            window.__csTaskEventStream = {
                active: true,
                conversationId: conversationId || null,
                assistantDomId: assistantDomId || null,
                progressId: progressId || null
            };
        } catch (e) { /* ignore */ }
        markProcessDetailsStreaming(true, assistantDomId);
        resumeFollowingIfAtBottom();
        scheduleTurnRailRefresh();
        updateTurnRailState();
    }

    function onTaskEventStreamEnd() {
        onStreamEnd();
    }

    function applyMessageScrollOption(options) {
        scheduleTurnRailRefresh();
        const opt = (options && options.scroll) || 'follow';
        if (opt === 'none') return;
        if (opt === 'force') {
            forceScrollChatToBottom();
            return;
        }
        scheduleChatScrollToBottomIfFollowing(captureScrollPinState());
    }

    /** 手动展开详情时同步滚动，随后记录位置供滚动事件区分用户拖动。 */
    function scrollElementIntoViewIfFollowing(el) {
        const messagesEl = getChatMessagesEl();
        if (!el || !messagesEl || !captureScrollPinState()) return;
        el.scrollIntoView({ behavior: 'instant', block: 'nearest' });
        rememberScrollPosition(messagesEl);
        updateTurnRailState();
    }

    function onChatMessagesScroll() {
        const el = getChatMessagesEl();
        if (!el) return;

        const st = el.scrollTop;
        const range = scrollRange(el);
        const topChange = st - lastScrollTop;
        // 原生滚动条可能不派发指针事件。移动超出布局变化能够解释的距离时，
        // 也视为用户滚动。脚本主动定位后会同步记录位置。
        const hasUserScrollIntent = Date.now() <= userScrollIntentUntil ||
            Math.abs(topChange) > Math.abs(range - lastScrollRange) + 1;
        const scrolledUp = topChange < -1 && range - st > CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX;
        const scrolledDown = topChange > 1;
        lastScrollTop = st;
        lastScrollRange = range;

        if (scrolledUp && hasUserScrollIntent) {
            setScrollDetached();
        } else if (scrolledDown && hasUserScrollIntent) {
            resumeFollowingIfAtBottom(CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX, true);
        }
        updateTurnRailState();
    }

    function nestedTimelineCanScroll(target, direction) {
        const timeline = target?.closest?.('.progress-timeline.expanded');
        if (!timeline || scrollRange(timeline) <= CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX) return false;
        return direction < 0
            ? timeline.scrollTop > CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX
            : distanceFromBottom(timeline) > CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX;
    }

    function bindChatScrollListeners() {
        if (listenersBound) return;
        const el = getChatMessagesEl();
        if (!el) return;
        listenersBound = true;
        el.classList.toggle('is-following-latest', scrollMode === 'following');
        rememberScrollPosition(el);

        el.addEventListener('wheel', function (e) {
            // 只有内层到达边界后，滚轮才会传给外层对话区。
            if (nestedTimelineCanScroll(e.target, e.deltaY)) return;
            if (Math.abs(e.deltaY) > 1) {
                userScrollIntentUntil = Date.now() + 1200;
            }
            if (e.deltaY < -1) {
                setScrollDetached();
            }
        }, { passive: true });

        // 拖动原生纵向滚动条不会产生 wheel；先记录指针意图，再由 scroll 事件确认方向。
        el.addEventListener('pointerdown', function (e) {
            const rect = el.getBoundingClientRect();
            if (e.clientX >= rect.right - 18) {
                userScrollIntentUntil = Date.now() + 1800;
            }
        }, { passive: true });

        el.addEventListener('keydown', function (e) {
            const scrollKeys = ['ArrowUp', 'PageUp', 'Home', 'ArrowDown', 'PageDown', 'End', ' '];
            if (e.defaultPrevented || !scrollKeys.includes(e.key)) return;
            const upwards = e.key === 'ArrowUp' || e.key === 'PageUp' || e.key === 'Home' || (e.key === ' ' && e.shiftKey);
            if (nestedTimelineCanScroll(e.target, upwards ? -1 : 1)) return;
            userScrollIntentUntil = Date.now() + 1200;
            if (upwards) {
                setScrollDetached();
            }
        });

        el.addEventListener('touchmove', function (e) {
            if (e.touches && e.touches.length === 1) {
                const y = e.touches[0].clientY;
                const delta = (el._csTouchLastY ?? y) - y;
                el._csTouchLastY = y;
                if (nestedTimelineCanScroll(e.target, delta)) return;
                userScrollIntentUntil = Date.now() + 1200;
                if (delta < -4) {
                    setScrollDetached();
                }
            }
        }, { passive: true });
        el.addEventListener('touchstart', function (e) {
            if (e.touches && e.touches.length) {
                el._csTouchLastY = e.touches[0].clientY;
            }
        }, { passive: true });
        el.addEventListener('touchend', function () {
            el._csTouchLastY = null;
        }, { passive: true });

        el.addEventListener('scroll', onChatMessagesScroll, { passive: true });

        const returnLatestButton = getReturnLatestButton();
        if (returnLatestButton) {
            returnLatestButton.addEventListener('pointerdown', isolateReturnLatestPointerEvent);
            returnLatestButton.addEventListener('pointerup', isolateReturnLatestPointerEvent);
            returnLatestButton.addEventListener('click', onReturnLatestClick);
        }

        const turnPreview = document.getElementById('chat-turn-rail-preview');
        if (turnPreview) {
            turnPreview.addEventListener('mouseenter', function () {
                if (turnPreviewHideTimer) {
                    window.clearTimeout(turnPreviewHideTimer);
                    turnPreviewHideTimer = 0;
                }
            });
            turnPreview.addEventListener('mouseleave', scheduleHideTurnPreview);
        }

        if (typeof MutationObserver === 'function') {
            turnRailObserver = new MutationObserver(function () {
                scheduleTurnRailRefresh();
                // 最终回复会替换消息气泡内部 HTML，任务详情也会在子树内持续增高。
                // 只在仍处于 following 时按帧合并粘底；用户上滑后的 detached 状态不受影响。
                if (scrollMode === 'following' && Date.now() >= detachLockUntil) {
                    scheduleChatScrollToBottomIfFollowing(true);
                }
            });
            turnRailObserver.observe(el, { childList: true, subtree: true, characterData: true });
        }

        if (typeof ResizeObserver === 'function') {
            chatMessagesResizeObserver = new ResizeObserver(function () {
                // 顶部运行任务条、输入框或视口变化会改变消息区 clientHeight，
                // 但不会触发消息子树 MutationObserver。跟随模式下需重新精确粘底。
                if (scrollMode === 'following' && Date.now() >= detachLockUntil) {
                    scheduleChatScrollToBottomIfFollowing(true);
                } else {
                    updateTurnRailState();
                }
            });
            chatMessagesResizeObserver.observe(el);
        }

        window.addEventListener('resize', function () {
            hideTurnPreview();
            if (scrollMode === 'following' && Date.now() >= detachLockUntil) {
                scheduleChatScrollToBottomIfFollowing(true);
            } else {
                updateTurnRailState();
            }
        }, { passive: true });
    }

    function initChatScroll() {
        bindChatScrollListeners();
        const el = getChatMessagesEl();
        if (el) rememberScrollPosition(el);
        scheduleTurnRailRefresh(true);
        updateTurnRailState();
    }

    window.CyberStrikeChatScroll = {
        init: initChatScroll,
        onUserSendMessage: onUserSendMessage,
        onStreamEnd: onStreamEnd,
        onTaskEventStreamBegin: onTaskEventStreamBegin,
        onTaskEventStreamEnd: onTaskEventStreamEnd,
        captureScrollPinState: captureScrollPinState,
        scheduleScroll: scheduleChatScrollToBottomIfFollowing,
        scrollIfPinned: scrollChatMessagesToBottomIfPinned,
        settleToBottomIfFollowing: settleChatToBottomIfFollowing,
        settleConversationRestoreToBottom: settleConversationRestoreToBottom,
        forceScrollToBottom: forceScrollChatToBottom,
        applyMessageScroll: applyMessageScrollOption,
        scrollIntoViewIfFollowing: scrollElementIntoViewIfFollowing,
        isPinnedToBottom: isChatMessagesPinnedToBottom,
        markProgressStreaming: markProgressStreaming,
        markProcessDetailsStreaming: markProcessDetailsStreaming,
        setScrollFollowing: setScrollFollowing,
        setScrollDetached: setScrollDetached,
        refreshReturnLatest: updateReturnLatestButton,
        refreshTurnRail: function () { scheduleTurnRailRefresh(true); },
    };

    window.isChatMessagesPinnedToBottom = isChatMessagesPinnedToBottom;
    window.captureScrollPinState = captureScrollPinState;
    window.scrollChatMessagesToBottomIfPinned = scrollChatMessagesToBottomIfPinned;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initChatScroll);
    } else {
        initChatScroll();
    }
})();
