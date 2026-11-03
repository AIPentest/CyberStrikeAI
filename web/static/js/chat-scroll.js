/**
 * Main conversation area smart sticky-bottom scrolling: automatically follows during streaming output, doesn't steal focus when the user scrolls up to read.
 * The main POST flow (sendMessage) and the post-refresh task-events supplementary stream share the same strategy.
 */
(function () {
    'use strict';

    /** Only continue auto-following when the distance from the bottom is within this range (preferably small, to avoid "even a slight difference being dragged back") */
    const CHAT_SCROLL_FOLLOW_THRESHOLD_PX = 48;
    /** Only resume following when the bottom is truly reached; the 2px is to accommodate sub-pixel scrolling on high-resolution displays. */
    const CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX = 2;
    /** Reaching this range is treated as being in the last round */
    const CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX = 120;
    /** A brief lock after the user swipes up, preventing SSE and scroll events from racing to grab the scroll */
    const DETACH_LOCK_MS = 900;
    /** Refresh-recovery spans multiple rounds of asynchronous layout across history messages, process details, fonts, and stream subscriptions. */
    const CONVERSATION_RESTORE_SETTLE_MIN_MS = 3000;
    const CONVERSATION_RESTORE_SETTLE_MAX_MS = 6000;
    const CONVERSATION_RESTORE_STABLE_FRAMES = 12;

    /** @type {'following' | 'detached'} */
    let scrollMode = 'following';
    let scrollFollowRaf = 0;
    let scrollSettleGeneration = 0;
    let conversationRestoreGeneration = 0;
    /** After the user unfollows, whether there is unread new output below (not counted by SSE count) */
    let hasPendingNewBelow = false;
    let listenersBound = false;
    let lastScrollTop = 0;
    let lastScrollHeight = 0;
    let programmaticScroll = false;
    let detachLockUntil = 0;
    /** The most recent scroll intent initiated by the user; layout changes or scripted scrolls must not restore sticky bottom based on it. */
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

    /** Each user message starts a turn, and all assistant messages before the next user message belong to that turn. */
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
            : 'No.' + number + 'wheel';
        const safePrefix = prefix && prefix !== 'chat.turnNumber' ? prefix : ('No.' + number + 'wheel');
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
            summary = typeof window.t === 'function' ? window.t('chat.turnPending') : 'Processing...';
            if (!summary || summary === 'chat.turnPending') summary = 'Processing...';
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
        setScrollDetached();
        programmaticScroll = true;
        messagesEl.scrollTo({
            top: Math.max(0, turn.user.offsetTop - 20),
            behavior: 'smooth'
        });
        setActiveTurnMarker(index);
        hideTurnPreview();
        window.setTimeout(function () {
            programmaticScroll = false;
            lastScrollTop = messagesEl.scrollTop;
            updateTurnRailActive();
        }, 420);
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

        // Before the backend returns the conversationId for a newly created conversation, both sides are empty, and it still belongs to the current interface.
        if (!streamConversationId) return !visibleConversationId;
        return streamConversationId === visibleConversationId;
    }

    /** Only the main POST stream / task-events supplementary stream of the currently visible conversation is considered "currently outputting" */
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

    function distanceFromBottom(el) {
        if (!el) return 0;
        const { scrollTop, scrollHeight, clientHeight } = el;
        return scrollHeight - clientHeight - scrollTop;
    }

    function isNearBottom(thresholdPx) {
        const el = getChatMessagesEl();
        if (!el) return true;
        return distanceFromBottom(el) <= thresholdPx;
    }

    function isChatMessagesPinnedToBottom() {
        return isNearBottom(CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX);
    }

    /** Restore following when already at the bottom (fixes: manually scrolled to the bottom but scrollMode is still detached) */
    function resumeFollowingIfAtBottom(thresholdPx, userInitiated) {
        if (!userInitiated && Date.now() < detachLockUntil) return false;
        const threshold = Number.isFinite(Number(thresholdPx))
            ? Math.max(0, Number(thresholdPx))
            : CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX;
        if (!isNearBottom(threshold)) return false;
        // detached is the reading state after the user explicitly swipes up. Layout changes, streaming height increase, and mode switching
        // Even if the viewport temporarily approaches the bottom, it cannot recover on its own; it only recovers when the user explicitly scrolls down to the bottom.
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
        detachLockUntil = Date.now() + DETACH_LOCK_MS;
        cancelAnimationFrame(scrollFollowRaf);
        if (isStreamActive()) {
            hasPendingNewBelow = true;
        }
        updateTurnRailState();
    }

    function scrollChatToBottomInstant() {
        if (scrollMode !== 'following') return;
        const el = getChatMessagesEl();
        if (!el) return;
        programmaticScroll = true;
        el.scrollTop = el.scrollHeight;
        lastScrollTop = el.scrollTop;
        lastScrollHeight = el.scrollHeight;
        requestAnimationFrame(function () {
            programmaticScroll = false;
        });
    }

    function scrollChatToBottomSmooth() {
        const el = getChatMessagesEl();
        if (!el) return;
        programmaticScroll = true;
        el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
        requestAnimationFrame(function () {
            programmaticScroll = false;
            const node = getChatMessagesEl();
            if (node) {
                lastScrollTop = node.scrollTop;
                lastScrollHeight = node.scrollHeight;
            }
        });
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
        const atLatest = isNearBottom(CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX);
        const readingDetachedHistory = scrollMode === 'detached' && !atLatest;
        const farFromLatestFallback = !isNearBottom(CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX);
        const shouldShow = scrollable && (readingDetachedHistory || farFromLatestFallback);
        const streaming = shouldShow && isVisibleConversationTaskActive();
        button.hidden = !shouldShow;
        button.classList.toggle('is-streaming', streaming);
        button.classList.toggle('has-pending-new', shouldShow && hasPendingNewBelow);
    }

    function isolateReturnLatestPointerEvent(event) {
        if (!event) return;
        // This button will be hidden immediately after being clicked. Prevent the pointer event from continuing to bubble, avoiding in long history conversations
        // Click pass-through occurs when the button hiding and the bottom approval card re-arrangement happen in the same frame.
        event.stopPropagation();
    }

    function onReturnLatestClick(event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        forceScrollChatToBottom(true);
        const button = getReturnLatestButton();
        if (button) {
            button.hidden = true;
            button.blur();
        }
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
        cancelAnimationFrame(scrollFollowRaf);
        scrollFollowRaf = requestAnimationFrame(scrollChatToBottomInstant);
    }

    /**
     * Long detail restore / final state reconciliation will grow the DOM in batches across multiple requestAnimationFrame frames.
     * A single scroll-to-bottom may be earlier than the last batch of nodes; calibrate over several consecutive frames while still in following,
     * Once the user actively swipes up to enter detached, subsequent frames stop immediately, to avoid reclaiming the reading position.
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
     * When refreshing a restored long session, messages, details, and approval cards continue to grow taller across multiple frames.
     * When entering the recovery flow, explicitly return to following; if the user then actively swipes up, the existing input listener will immediately
     * Switches to detached, and causes subsequent calibration frames to stop, without stealing back the read position.
     */
    function settleConversationRestoreToBottom(frameCount) {
        setScrollFollowing();
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
            // wheel / touch / keyboard / scrollbar drag will enter detached; immediately respect the user's reading position.
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

    /** @param {boolean} wasPinned Whether it should follow before the DOM update (passed in by captureScrollPinState) */
    function scrollChatMessagesToBottomIfPinned(wasPinned) {
        scheduleChatScrollToBottomIfFollowing(wasPinned);
    }

    function forceScrollChatToBottom(smooth) {
        setScrollFollowing();
        cancelAnimationFrame(scrollFollowRaf);
        if (smooth) {
            scrollChatToBottomSmooth();
        } else {
            scrollChatToBottomInstant();
        }
    }

    function onUserSendMessage() {
        setScrollFollowing();
        scrollChatToBottomInstant();
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

    /** When the task-events backfill stream starts after a session refresh, align it with the sendMessage main flow */
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
            forceScrollChatToBottom(false);
            return;
        }
        scheduleChatScrollToBottomIfFollowing(captureScrollPinState());
    }

    /** When streaming or the user isn't following, disable scrollIntoView from hijacking the scroll */
    function scrollElementIntoViewIfFollowing(el, options) {
        if (!el || !captureScrollPinState()) return;
        el.scrollIntoView(options || { behavior: 'smooth', block: 'nearest' });
    }

    function onChatMessagesScroll() {
        const el = getChatMessagesEl();
        if (!el) return;

        const st = el.scrollTop;
        const sh = el.scrollHeight;
        const hasUserScrollIntent = Date.now() <= userScrollIntentUntil;

        if (programmaticScroll) {
            // While performing a restore/streaming sticky-bottom, the user may still scroll the wheel in reverse or drag the scrollbar.
            // Script scrolling to the bottom only increases scrollTop; a decrease here must be the user interrupting the follow.
            if (st < lastScrollTop - 1 && (scrollMode === 'detached' || hasUserScrollIntent)) {
                setScrollDetached();
            }
            lastScrollTop = st;
            lastScrollHeight = sh;
            updateTurnRailState();
            return;
        }

        const scrolledUp = st < lastScrollTop - 1;
        const scrolledDown = st > lastScrollTop + 1;
        const contentShrank = sh < lastScrollHeight - 1;

        // Refresh/final-state re-render first empties or collapses the old DOM, and the browser passively clamps scrollTop down.
        // This is not a user swipe up, should not exit following with an error.
        if (contentShrank) {
            lastScrollTop = st;
            lastScrollHeight = sh;
            updateTurnRailState();
            return;
        }

        // Refresh restore rebuilds messages and details, scroll anchoring may let scrollTop when there is no user input
        // Temporarily reduced. Only explicit scroll-wheel, touch, keyboard, or scroll-bar intent releases the sticky bottom.
        if (scrolledUp && (scrollMode === 'detached' || hasUserScrollIntent)) {
            setScrollDetached();
        } else if (
            scrolledDown &&
            hasUserScrollIntent &&
            resumeFollowingIfAtBottom(CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX, true)
        ) {
            // Only resume following when the user explicitly scrolls down and reaches the true bottom; do not proactively rewrite scrollTop.
            // Subsequent newly added content then naturally sticks to the bottom according to the following state, avoiding sudden jumps when approaching the bottom.
        }

        lastScrollTop = st;
        lastScrollHeight = sh;
        updateTurnRailState();
    }

    function bindChatScrollListeners() {
        if (listenersBound) return;
        const el = getChatMessagesEl();
        if (!el) return;
        listenersBound = true;
        lastScrollTop = el.scrollTop;
        lastScrollHeight = el.scrollHeight;

        el.addEventListener('wheel', function (e) {
            if (Math.abs(e.deltaY) > 1) {
                userScrollIntentUntil = Date.now() + 1200;
            }
            if (e.deltaY < -1) {
                setScrollDetached();
            }
        }, { passive: true });

        // Dragging the native vertical scrollbar does not produce wheel; first record the pointer's intent, then confirm the direction via the scroll event.
        el.addEventListener('pointerdown', function (e) {
            const rect = el.getBoundingClientRect();
            if (e.clientX >= rect.right - 18) {
                userScrollIntentUntil = Date.now() + 1800;
            }
        }, { passive: true });

        el.addEventListener('keydown', function (e) {
            const scrollKeys = ['ArrowUp', 'PageUp', 'Home', 'ArrowDown', 'PageDown', 'End', ' '];
            if (scrollKeys.includes(e.key)) {
                userScrollIntentUntil = Date.now() + 1200;
            }
            if (e.key === 'ArrowUp' || e.key === 'PageUp' || e.key === 'Home' || (e.key === ' ' && e.shiftKey)) {
                setScrollDetached();
            }
        });

        el.addEventListener('touchmove', function (e) {
            if (e.touches && e.touches.length === 1) {
                userScrollIntentUntil = Date.now() + 1200;
                el._csTouchLastY = el._csTouchLastY != null ? el._csTouchLastY : e.touches[0].clientY;
                if (e.touches[0].clientY > el._csTouchLastY + 4) {
                    setScrollDetached();
                }
                el._csTouchLastY = e.touches[0].clientY;
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
                // The final reply will replace the message bubble's inner HTML, and the task details will also keep growing taller within the subtree.
                // Only merge the sticky bottom per frame while still in the following state; the detached state after the user swipes up is not affected.
                if (scrollMode === 'following' && Date.now() >= detachLockUntil) {
                    scheduleChatScrollToBottomIfFollowing(true);
                }
            });
            turnRailObserver.observe(el, { childList: true, subtree: true, characterData: true });
        }

        if (typeof ResizeObserver === 'function') {
            chatMessagesResizeObserver = new ResizeObserver(function () {
                // Changes to the top running task bar, input box, or viewport will change the message area clientHeight,
                // But it will not trigger the message subtree MutationObserver. In follow mode, need to re-precisely stick to the bottom.
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
        if (el) {
            lastScrollTop = el.scrollTop;
            lastScrollHeight = el.scrollHeight;
        }
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
