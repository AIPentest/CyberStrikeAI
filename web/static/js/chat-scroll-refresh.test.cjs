const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

const scroll = fs.readFileSync('web/static/js/chat-scroll.js', 'utf8');
const monitor = fs.readFileSync('web/static/js/monitor.js', 'utf8');
const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');
const projects = fs.readFileSync('web/static/js/projects.js', 'utf8');
const router = fs.readFileSync('web/static/js/router.js', 'utf8');
const auth = fs.readFileSync('web/static/js/auth.js', 'utf8');
const webshell = fs.readFileSync('web/static/js/webshell.js', 'utf8');
const html = fs.readFileSync('web/templates/index.html', 'utf8');

function functionSource(source, name, nextName) {
    const start = source.indexOf(`function ${name}(`);
    const end = source.indexOf(`function ${nextName}(`, start);
    assert.notEqual(start, -1, `${name} should exist`);
    assert.notEqual(end, -1, `${nextName} should follow ${name}`);
    return source.slice(start, end);
}

function createScrollRuntime() {
    const listeners = new Map();
    const buttonListeners = new Map();
    const classList = { add() {}, remove() {}, toggle() {}, contains() { return false; } };
    const chatEl = {
        scrollTop: 500,
        scrollHeight: 1000,
        clientHeight: 500,
        children: [],
        classList,
        addEventListener(type, handler) { listeners.set(type, handler); },
        scrollTo(options) { this.scrollTop = Number(options && options.top) || 0; },
        getBoundingClientRect() { return { right: 1000 }; },
    };
    const returnLatest = {
        hidden: true,
        classList,
        addEventListener(type, handler) { buttonListeners.set(type, handler); },
        blur() {},
    };
    const rafQueue = new Map();
    let rafId = 0;
    const requestAnimationFrame = (handler) => {
        const id = ++rafId;
        rafQueue.set(id, handler);
        return id;
    };
    const cancelAnimationFrame = (id) => rafQueue.delete(id);
    const document = {
        readyState: 'complete',
        getElementById(id) {
            if (id === 'chat-messages') return chatEl;
            if (id === 'chat-return-latest') return returnLatest;
            return null;
        },
        querySelectorAll() { return []; },
        addEventListener() {},
    };
    const window = {
        document,
        addEventListener() {},
        setTimeout,
        clearTimeout,
        requestAnimationFrame,
        cancelAnimationFrame,
        innerWidth: 1440,
        innerHeight: 900,
    };
    const context = {
        window,
        document,
        requestAnimationFrame,
        cancelAnimationFrame,
        setTimeout,
        clearTimeout,
        console,
    };
    vm.runInNewContext(scroll, context);
    return {
        api: window.CyberStrikeChatScroll,
        chatEl,
        returnLatest,
        listeners,
        flushAnimationFrames() {
            while (rafQueue.size) {
                const pending = Array.from(rafQueue.values());
                rafQueue.clear();
                pending.forEach((handler) => handler(Date.now()));
            }
        },
    };
}

test('向上滚动立即解除粘底，只有滚到真实底部才恢复', () => {
    const runtime = createScrollRuntime();
    runtime.flushAnimationFrames();

    runtime.listeners.get('wheel')({ deltaY: -20 });
    runtime.chatEl.scrollTop = 480;
    runtime.listeners.get('scroll')();
    assert.equal(runtime.api.captureScrollPinState(), false);

    runtime.chatEl.scrollHeight = 1100;
    runtime.api.scrollIfPinned(true);
    runtime.flushAnimationFrames();
    assert.equal(runtime.chatEl.scrollTop, 480, '新输出不能抢回用户的阅读位置');

    runtime.chatEl.scrollTop = 597;
    runtime.listeners.get('scroll')();
    assert.equal(runtime.api.captureScrollPinState(), false, '距底部 2px 以上仍保持脱离');

    runtime.chatEl.scrollTop = 600;
    runtime.listeners.get('scroll')();
    assert.equal(runtime.api.captureScrollPinState(), true, '用户滚到真实底部后立即恢复跟随');

    runtime.chatEl.scrollHeight = 1200;
    runtime.api.scrollIfPinned(true);
    runtime.flushAnimationFrames();
    assert.equal(runtime.chatEl.scrollTop, 1200, '恢复后新增输出继续请求滚到最底部');
});

test('用户离开最新位置后回到底部按钮稳定显示到真实底部', () => {
    const runtime = createScrollRuntime();
    runtime.flushAnimationFrames();

    runtime.listeners.get('wheel')({ deltaY: -20 });
    runtime.chatEl.scrollTop = 455;
    runtime.listeners.get('scroll')();
    assert.equal(runtime.returnLatest.hidden, false, '120px 阈值内仍应显示回到最新入口');

    runtime.listeners.get('wheel')({ deltaY: 20 });
    runtime.chatEl.scrollTop = 499;
    runtime.listeners.get('scroll')();
    assert.equal(runtime.returnLatest.hidden, true, '滚到真实底部后才隐藏');
});

test('刷新重建详情引起的布局上移不会误判为用户上滑', () => {
    const runtime = createScrollRuntime();
    runtime.flushAnimationFrames();

    runtime.chatEl.scrollTop = 460;
    runtime.listeners.get('scroll')();
    assert.equal(runtime.api.captureScrollPinState(), true, '没有用户输入的布局滚动仍应保持跟随');

    runtime.chatEl.scrollHeight = 1100;
    runtime.api.scrollIfPinned(true);
    runtime.flushAnimationFrames();
    assert.equal(runtime.chatEl.scrollTop, 1100, '刷新恢复后的后续增量应继续粘底');
});

test('登录成功后重新加载曾因未授权失败的项目侧栏', () => {
    const refreshSource = functionSource(auth, 'refreshAppData', 'bootstrapApp');
    const conversationsIndex = refreshSource.indexOf('loadConversations()');
    const projectRetryIndex = refreshSource.indexOf('window.refreshChatProjectSelector({ reloadFolders: true })');

    assert.notEqual(conversationsIndex, -1);
    assert.ok(projectRetryIndex > conversationsIndex);
    assert.match(refreshSource, /typeof window\.refreshChatProjectSelector === 'function'/);
    assert.match(html, /\/static\/js\/auth\.js\?v=[A-Za-z0-9._-]+/);
});

test('用户真正滑到底部后恢复自动跟随且不会提前强制跳底', () => {
    const resumeSource = functionSource(scroll, 'resumeFollowingIfAtBottom', 'captureScrollPinState');
    const captureSource = functionSource(scroll, 'captureScrollPinState', 'setScrollFollowing');
    const autoSource = functionSource(scroll, 'canAutoScrollNow', 'scheduleChatScrollToBottomIfFollowing');
    const scrollSource = functionSource(scroll, 'onChatMessagesScroll', 'bindChatScrollListeners');

    assert.match(resumeSource, /thresholdPx/);
    assert.match(scroll, /CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX = 2/);
    assert.doesNotMatch(captureSource, /resumeFollowingIfAtBottom/);
    assert.doesNotMatch(autoSource, /resumeFollowingIfAtBottom/);
    assert.match(resumeSource, /if \(!userInitiated\) return false/);
    assert.match(scrollSource, /scrolledDown/);
    assert.match(scrollSource, /hasUserScrollIntent/);
    assert.match(scrollSource, /resumeFollowingIfAtBottom\(CHAT_SCROLL_FOLLOW_RESUME_THRESHOLD_PX, true\)/);
    assert.doesNotMatch(scrollSource, /resumeFollowingIfAtBottom\(CHAT_SCROLL_NAV_BOTTOM_THRESHOLD_PX\)/);
    assert.doesNotMatch(scrollSource, /else if \(resumeFollowingIfAtBottom\(\)\)/);
    assert.doesNotMatch(scrollSource, /scheduleChatScrollToBottomIfFollowing\(true\)/);
    assert.doesNotMatch(scrollSource, /else if \(resumeFollowingIfAtBottom\(\)\)/);
    assert.match(scrollSource, /contentShrank/);
    assert.match(scrollSource, /sh < lastScrollHeight - 1/);
    assert.match(scrollSource, /if \(scrolledUp && \(scrollMode === 'detached' \|\| hasUserScrollIntent\)\) \{[\s\S]*?setScrollDetached\(\)/);
    assert.match(scrollSource, /if \(programmaticScroll\) \{[\s\S]*?st < lastScrollTop - 1 && \(scrollMode === 'detached' \|\| hasUserScrollIntent\)[\s\S]*?setScrollDetached\(\)/);
});

test('切换对话模式引起的布局滚动不会重新开启粘底', () => {
    const scrollSource = functionSource(scroll, 'onChatMessagesScroll', 'bindChatScrollListeners');
    const bindSource = functionSource(scroll, 'bindChatScrollListeners', 'initChatScroll');
    const selectModeSource = functionSource(chat, 'selectAgentMode', 'initChatAgentModeFromConfig');

    assert.match(scroll, /let userScrollIntentUntil = 0/);
    assert.match(scrollSource, /const hasUserScrollIntent = Date\.now\(\) <= userScrollIntentUntil/);
    assert.match(scrollSource, /scrolledDown &&[\s\S]*?hasUserScrollIntent &&[\s\S]*?resumeFollowingIfAtBottom/);
    assert.doesNotMatch(scrollSource, /else if \(resumeFollowingIfAtBottom\(\)\)/);
    assert.match(bindSource, /Math\.abs\(e\.deltaY\) > 1/);
    assert.match(bindSource, /userScrollIntentUntil = Date\.now\(\) \+ 1800/);
    assert.doesNotMatch(selectModeSource, /setScrollFollowing|forceScrollToBottom|scrollTop/);
});

test('刷新运行中任务补齐最新详情后保持粘底但尊重用户上滑', () => {
    const attachSource = functionSource(monitor, 'attachRunningTaskEventStream', 'parseToolCallArgsFromData');
    const settleSource = functionSource(scroll, 'settleChatToBottomIfFollowing', 'scrollChatMessagesToBottomIfPinned');

    assert.match(attachSource, /window\.captureScrollPinState\(\)/);
    assert.match(attachSource, /settleToBottomIfFollowing\(12\)/);
    assert.match(attachSource, /settleToBottomIfFollowing\(18\)/);
    assert.match(attachSource, /用户期间没有主动上滑/);
    assert.match(attachSource, /keepFollowingFinalRender/);
    assert.match(attachSource, /最终消息和详情重绘都会增高 DOM/);
    assert.match(settleSource, /scrollMode !== 'following'/);
    assert.match(settleSource, /Date\.now\(\) < detachLockUntil/);
    assert.match(settleSource, /settleFrame\(remaining - 1\)/);
    assert.match(settleSource, /scrollChatToBottomInstant\(\)/);
    assert.match(scroll, /function settleConversationRestoreToBottom\(frameCount\)/);
    assert.match(scroll, /CONVERSATION_RESTORE_SETTLE_MIN_MS = 3000/);
    assert.match(scroll, /CONVERSATION_RESTORE_SETTLE_MAX_MS = 6000/);
    assert.match(scroll, /const generation = \+\+conversationRestoreGeneration/);
    assert.match(scroll, /scrollMode !== 'following'/);
    assert.match(scroll, /stableFrames >= CONVERSATION_RESTORE_STABLE_FRAMES/);
    assert.match(scroll, /requestAnimationFrame\(settleRestoreFrame\)/);
    assert.match(chat, /settleConversationRestoreToBottom\(30\)/);
});

test('刷新后迭代思考区独立跟随最新内容且允许用户上滑解除', () => {
    const startSource = functionSource(monitor, 'startProcessDetailsLatestFollow', 'loadProcessDetailsPaginated');
    const loadSource = functionSource(monitor, 'loadProcessDetailsPaginated', 'shouldInitiallyOpenProcessDetailsAtLatest');
    const attachSource = functionSource(monitor, 'attachRunningTaskEventStream', 'parseToolCallArgsFromData');
    const returnLatestSource = functionSource(monitor, 'ensureProcessDetailsReturnLatestControl', 'scrollProcessDetailsToLatest');

    assert.match(monitor, /const processDetailsReturnLatestControls = new WeakMap\(\)/);
    assert.match(returnLatestSource, /className = 'process-details-return-latest'/);
    assert.match(monitor, /function getProcessDetailsLatestFollowStateForTimeline\(timeline\)/);
    assert.match(monitor, /const followingLatest = !!\(followState && !followState\.detached\)/);
    assert.match(monitor, /const shouldShow = !followingLatest && expanded && scrollable && awayFromLatest/);
    assert.match(returnLatestSource, /timeline\.scrollTo\(\{ top: targetTop, behavior: 'smooth' \}\)/);
    assert.match(returnLatestSource, /timeline\.addEventListener\('scroll', onScroll/);
    assert.match(returnLatestSource, /window\.ensureProcessDetailsReturnLatestControl = ensureProcessDetailsReturnLatestControl/);
    assert.match(startSource, /new MutationObserver\(scheduleFollowLatest\)/);
    assert.match(startSource, /characterData: true/);
    assert.match(startSource, /new ResizeObserver\(scheduleFollowLatest\)/);
    assert.match(startSource, /ensureProcessDetailsReturnLatestControl\(timeline\)/);
    assert.match(startSource, /markProcessDetailsReturnLatestPending\(timeline\)/);
    assert.match(startSource, /scrollProcessDetailsToLatest\(String\(assistantMessageId \|\| ''\), false\)/);
    assert.match(startSource, /event\.deltaY < -1/);
    assert.match(startSource, /state\.userScrollIntentUntil = Date\.now\(\) \+ 1200/);
    assert.match(startSource, /event\.clientX >= rect\.right - PROCESS_DETAILS_FOLLOW_SCROLLBAR_GUTTER_PX/);
    assert.match(startSource, /event\.key === 'ArrowUp'/);
    assert.match(startSource, /cancelAnimationFrame\(state\.rafId\)/);
    assert.match(startSource, /if \(scrolledUp && \(state\.detached \|\| Date\.now\(\) <= state\.userScrollIntentUntil\)\) \{[\s\S]*?detachForUserNavigation\(\)/);
    assert.match(startSource, /state\.detached &&[\s\S]*?scrolledDown &&[\s\S]*?Date\.now\(\) <= state\.userScrollIntentUntil/);
    assert.match(monitor, /PROCESS_DETAILS_FOLLOW_RESUME_THRESHOLD_PX = 2/);
    assert.match(startSource, /distance <= PROCESS_DETAILS_FOLLOW_RESUME_THRESHOLD_PX/);
    assert.match(startSource, /state\.detached = false/);
    assert.doesNotMatch(startSource, /if \(distance <= PROCESS_DETAILS_FOLLOW_RESUME_THRESHOLD_PX\) \{\s*state\.detached = false/);
    assert.match(loadSource, /startProcessDetailsLatestFollow\(assistantMessageId/);
    assert.match(attachSource, /startProcessDetailsLatestFollow\(asEl\.id, \{ persistent: true \}\)/);
    assert.match(attachSource, /stopProcessDetailsLatestFollow\(asEl\.id\)/);
    assert.match(chat, /window\.ensureProcessDetailsReturnLatestControl\(timeline\)/);
    assert.doesNotMatch(returnLatestSource, /chat-return-latest/);
});

test('刷新后的工具调用恢复与实时一致的成功失败徽标', () => {
    const renderSource = functionSource(chat, 'renderProcessDetails', 'finishProcessDetailsRender');
    const presentationSource = functionSource(monitor, 'getToolCallStatusPresentation', 'applyToolCallStatus');
    const applySource = functionSource(monitor, 'applyToolCallStatus', 'updateToolCallStatus');
    const addSource = functionSource(monitor, 'addTimelineItem', 'loadActiveTasks');

    assert.match(renderSource, /toolStatusByProcessDetailId/);
    assert.match(renderSource, /timelineOpts\.toolStatus = toolStatusByProcessDetailId\.get/);
    assert.match(presentationSource, /normalized === 'completed'/);
    assert.match(presentationSource, /normalized === 'failed'/);
    assert.match(applySource, /tool-status-badge/);
    assert.match(applySource, /item\.dataset\.toolDisplayStatus = presentation\.status/);
    assert.match(addSource, /initialToolStatus = item\.dataset\.toolDisplayStatus/);
    assert.match(addSource, /applyToolCallStatus\(item, initialToolStatus\)/);
    assert.match(monitor, /refreshProgressAndTimelineI18n\(\)[\s\S]*?applyToolCallStatus\(item, item\.dataset\.toolDisplayStatus\)/);
});

test('首次实时输出与刷新恢复都保留独立迭代滚动并跟随最新内容', () => {
    const css = fs.readFileSync('web/static/css/style.css', 'utf8');
    const addSource = functionSource(monitor, 'addProgressMessage', 'toggleProgressDetails');
    const liveSource = functionSource(monitor, 'startLiveProgressLatestFollow', 'stopLiveProgressLatestFollow');

    assert.match(css, /\.progress-container\.is-streaming \.progress-timeline\.expanded,[\s\S]{0,360}max-height: min\(64vh, 720px\);[\s\S]{0,180}overflow-y: auto;/);
    assert.match(css, /\.message\.progress-message \.progress-timeline\.expanded \{[\s\S]{0,260}max-height: min\(64vh, 720px\);[\s\S]{0,160}overflow-y: auto;/);
    assert.match(css, /\.process-details-return-latest \{[\s\S]{0,260}position: absolute;[\s\S]{0,260}border-radius: 50%;/);
    assert.match(css, /\.process-details-return-latest\.has-pending-new::after,/);
    assert.doesNotMatch(css, /流式执行中[\s\S]{0,320}overflow-y: visible;/);
    assert.match(addSource, /startLiveProgressLatestFollow\(id\)/);
    assert.match(liveSource, /stateKey: liveProgressLatestFollowKey\(id\)/);
    assert.match(liveSource, /persistent: true/);
    assert.match(liveSource, /target\.scrollTop = Math\.max\(0, target\.scrollHeight - target\.clientHeight\)/);
    assert.match(monitor, /function finalizeProgressTask\(progressId, finalLabel\) \{[\s\S]{0,120}stopLiveProgressLatestFollow\(progressId\)/);
});

test('同一会话的其他标签页自动补流且发送前阻止重复任务', () => {
    const syncSource = functionSource(monitor, 'syncVisibleConversationTaskReplay', 'getActiveTaskDisplayName');
    const sendSource = functionSource(chat, 'sendMessage', 'renderChatFileChips');

    assert.match(monitor, /new BroadcastChannel\(CHAT_TASK_SYNC_CHANNEL_NAME\)/);
    assert.match(monitor, /payload\.type !== 'task-started'/);
    assert.match(monitor, /conversationExecutionTracker\.markRunning\(id\)/);
    assert.match(syncSource, /await window\.loadConversation\(conversationId\)/);
    assert.match(syncSource, /return attachRunningTaskEventStream\(conversationId\)/);
    assert.match(monitor, /syncVisibleConversationTaskReplay\(normalizedTasks\)/);
    assert.match(sendSource, /await loadActiveTasks\(\)/);
    assert.match(sendSource, /if \(isCurrentChatTaskActive\(\)\)/);
    assert.ok(sendSource.indexOf('if (isCurrentChatTaskActive())') < sendSource.indexOf("addMessage('user'"));
    assert.match(sendSource, /window\.notifyConversationTaskStarted\(streamConversationId\)/);
});

test('刷新补流在订阅竞态或终态帧丢失时从数据库对账最终正文', () => {
    const attachSource = functionSource(monitor, 'attachRunningTaskEventStream', 'parseToolCallArgsFromData');
    const reconcileSource = functionSource(monitor, 'reconcileConversationAfterTaskReplay', 'cancelRunningTaskEventStream');

    assert.match(attachSource, /const eventStreamResponsePromise = apiFetch\(url/);
    assert.ok(attachSource.indexOf('const eventStreamResponsePromise') < attachSource.indexOf('loadProcessDetailsPaginated'));
    assert.match(attachSource, /if \(!active\) \{[\s\S]*?assistantMessageNeedsTaskReplayReconcile\(staleAssistant\)[\s\S]*?reconcileConversationAfterTaskReplay\(conversationId, true\)/);
    assert.match(attachSource, /if \(!response\.ok\) \{[\s\S]*?reconcileConversationAfterTaskReplay\(conversationId, true\)/);
    assert.match(attachSource, /if \(!replaySawDone\) \{[\s\S]*?reconcileConversationAfterTaskReplay/);
    assert.match(reconcileSource, /updateAssistantBubbleContent\(assistantEl\.id, finalMessage\.content \|\| '', true\)/);
    assert.match(reconcileSource, /loadProcessDetailsPaginated\(assistantEl\.id, finalMessage\.id,[\s\S]*?initialLatest: true,[\s\S]*?autoLoadAll: false/);
});

test('消息气泡内部流式增高时仅在跟随模式继续粘底', () => {
    const bindSource = functionSource(scroll, 'bindChatScrollListeners', 'initChatScroll');

    assert.match(bindSource, /scrollMode === 'following'/);
    assert.match(bindSource, /scheduleChatScrollToBottomIfFollowing\(true\)/);
    assert.match(bindSource, /\{ childList: true, subtree: true, characterData: true \}/);
    assert.match(bindSource, /new ResizeObserver/);
    assert.match(bindSource, /chatMessagesResizeObserver\.observe\(el\)/);
    assert.match(bindSource, /改变消息区 clientHeight/);
    assert.match(bindSource, /Math\.abs\(e\.deltaY\) > 1/);
    assert.match(bindSource, /e\.deltaY < -1/);
    assert.match(bindSource, /e\.clientX >= rect\.right - 18/);
    assert.match(bindSource, /e\.key === 'ArrowUp'/);
});

test('页面在任务补流脚本之前加载智能滚动控制器', () => {
    const scrollIndex = html.indexOf('/static/js/chat-scroll.js?v=20260815-1');
    const monitorIndex = html.indexOf('/static/js/monitor.js?v=20261011-token-usage');

    assert.notEqual(scrollIndex, -1);
    assert.notEqual(monitorIndex, -1);
    assert.ok(scrollIndex < monitorIndex);
});

test('直接点击项目对话也会写入 hash 以便刷新后恢复并补流', () => {
    const loadSource = functionSource(chat, 'loadConversation', 'attachDeleteTurnButton');
    const syncSource = functionSource(chat, 'syncChatConversationHash', 'getConversationLiteFromCache');
    const streamSource = functionSource(monitor, 'setCurrentConversationIdFromStream', 'shouldSkipTaskEventReplayAttach');

    assert.match(syncSource, /window\.location\.hash\.split\('\?'\)\[0\] !== '#chat'/);
    assert.match(syncSource, /#chat\?conversation=/);
    assert.match(syncSource, /window\.history\.replaceState/);
    assert.match(loadSource, /syncChatConversationHash\(conversationId\)/);
    assert.match(streamSource, /window\.syncChatConversationHash\(cid\)/);
});

test('任务计划进度按事件唤醒，空闲时不固定轮询 plan-tasks', () => {
    const planSource = fs.readFileSync('web/static/js/chat-plan-progress.js', 'utf8');
    assert.doesNotMatch(planSource, /setInterval\(fetchPlanTasks/);
    assert.match(planSource, /ACTIVE_POLL_INTERVAL_MS = 1500/);
    assert.match(planSource, /function shouldContinuePolling\(payload\)/);
    assert.match(planSource, /payload && payload\.running === false/);
    assert.match(planSource, /payload && payload\.running === true[\s\S]*?state\.tasks\.length > 0/);
    assert.match(planSource, /isCurrentConversationRunning\(\) && state\.tasks\.length > 0/);
    assert.match(planSource, /conversation-task-state-changed/);
    assert.match(monitor, /window\.dispatchEvent\(new CustomEvent\('conversation-task-state-changed'/);
    assert.match(monitor, /window\.isConversationTaskRunning = isConversationTaskRunning/);
    assert.match(html, /chat-plan-progress\.js\?v=20260815-1/);
});

test('任务计划进度事件在活跃任务列表变化和新任务开始时发出', () => {
    const notifySource = functionSource(monitor, 'notifyConversationTaskStarted', 'initChatTaskSyncChannel');
    const renderSource = functionSource(monitor, 'renderActiveTasks', 'reconcileHitlApprovalStateWithActiveTasks');

    assert.match(notifySource, /conversation-task-state-changed/);
    assert.match(notifySource, /detail: \{ conversationId: id, running: true \}/);
    assert.match(renderSource, /conversationExecutionTracker\.update\(normalizedTasks\)/);
    assert.match(renderSource, /detail: \{ tasks: normalizedTasks \}/);
});

test('活跃任务按启动时间稳定排列且无变化刷新不重建停止按钮', () => {
    const sortSource = functionSource(monitor, 'stableActiveTasksForDisplay', 'activeTasksRenderSignature');
    const sortTasks = vm.runInNewContext(`(${sortSource.trim()})`);
    const tasks = [
        { conversationId: 'conversation-z', startedAt: '2026-08-19T10:00:00Z' },
        { conversationId: 'conversation-late', startedAt: '2026-08-19T10:01:00Z' },
        { conversationId: 'conversation-a', startedAt: '2026-08-19T10:00:00Z' }
    ];
    assert.deepEqual(
        Array.from(sortTasks(tasks), task => task.conversationId),
        ['conversation-a', 'conversation-z', 'conversation-late']
    );

    const renderSource = functionSource(monitor, 'renderActiveTasks', 'reconcileHitlApprovalStateWithActiveTasks');
    assert.match(renderSource, /nextVisualSignature === activeTasksVisualSignature/);
    assert.match(renderSource, /bar\.querySelectorAll\('\.active-task-item'\)\.length === normalizedTasks\.length/);
    assert.match(renderSource, /const previousScrollLeft = bar\.scrollLeft/);
    assert.match(renderSource, /bar\.scrollLeft = previousScrollLeft/);
});

test('新对话初始化期间切换会话后旧流事件不能把页面拉回', () => {
    const guardSource = functionSource(chat, 'shouldIgnoreLiveChatStreamEvent', 'clearLiveChatStreamIfOwned');
    const shouldIgnore = vm.runInNewContext(`(${guardSource.trim()})`);
    const activeStream = { active: true, detached: false, navigationSeq: 7 };

    assert.equal(shouldIgnore(activeStream, activeStream, 7), false);
    activeStream.detached = true;
    assert.equal(shouldIgnore(activeStream, activeStream, 7), true);
    activeStream.detached = false;
    activeStream.active = false;
    assert.equal(shouldIgnore(activeStream, activeStream, 7), true);
    activeStream.active = true;
    assert.equal(shouldIgnore(activeStream, activeStream, 8), true);
    assert.equal(shouldIgnore({ active: true, detached: false, navigationSeq: 7 }, activeStream, 7), true);

    const sendSource = functionSource(chat, 'sendMessage', 'renderChatFileChips');
    const guardIndex = sendSource.indexOf('shouldIgnoreLiveChatStreamEvent(liveStreamState)');
    const handlerIndex = sendSource.indexOf('handleStreamEvent(eventData');
    assert.notEqual(guardIndex, -1);
    assert.notEqual(handlerIndex, -1);
    assert.ok(guardIndex < handlerIndex);
    assert.match(sendSource, /const requestNavigationSeq = chatConversationNavigationSeq;[\s\S]*?await loadActiveTasks\(\)/);
    assert.match(sendSource, /if \(requestNavigationSeq !== chatConversationNavigationSeq\) \{[\s\S]{0,80}return;/);
    assert.match(sendSource, /navigationSeq: requestNavigationSeq/);
    assert.match(sendSource, /if \(!streamConversationId\) \{[\s\S]{0,180}liveStreamState\.conversationId = eventConvId/);
    assert.match(sendSource, /if \(eventConvId\) updateProgressConversation\(progressId, eventConvId\);[\s\S]{0,80}return;/);

    const loadSource = functionSource(chat, 'loadConversation', 'attachDeleteTurnButton');
    const newConversationSource = functionSource(chat, 'startNewConversation', 'loadConversations');
    assert.match(loadSource, /markChatConversationNavigation\(conversationId\)/);
    assert.match(loadSource, /window\.cancelScheduledChatConversationFromHash\(\)/);
    assert.match(newConversationSource, /markChatConversationNavigation\('', true\)/);
    assert.match(newConversationSource, /clearChatConversationHash\(\)/);
    assert.match(router, /function cancelScheduledChatConversationFromHash\(\)[\s\S]{0,160}chatConversationFromHashSeq\+\+/);
    assert.match(chat, /function abandonChatConversationForPageNavigation\(\)[\s\S]{0,260}markChatConversationNavigation\('', true\)/);
    assert.match(chat, /abandonChatConversationForPageNavigation\(\)[\s\S]{0,420}detachLiveChatStreamForNavigation\('', true\)/);
    assert.match(router, /currentPage === 'chat'[\s\S]{0,140}window\.abandonChatConversationForPageNavigation\(\)/);
    assert.match(chat, /const targetConversationId = String\(item\.dataset\.conversationId \|\| ''\)\.trim\(\);[\s\S]{0,80}loadConversation\(targetConversationId\)/);
    assert.match(projects, /const targetConversationId = String\(event\.currentTarget && event\.currentTarget\.dataset\.conversationId \|\| ''\)\.trim\(\)/);
    assert.match(projects, /window\.loadConversation\(targetConversationId\)/);
    assert.match(chat, /let loadConversationPendingId = ''/);
    assert.match(chat, /window\.isChatConversationLoadPending = isChatConversationLoadPending/);
    const immediateSelectionIndex = loadSource.indexOf('currentConversationId = conversationId;');
    const conversationFetchIndex = loadSource.indexOf('await apiFetch(`/api/conversations/${conversationId}?include_process_details=0`');
    assert.notEqual(immediateSelectionIndex, -1);
    assert.notEqual(conversationFetchIndex, -1);
    assert.ok(immediateSelectionIndex < conversationFetchIndex);
    assert.match(monitor, /String\(window\.currentConversationId \|\| ''\) !== conversationId[\s\S]{0,300}window\.isChatConversationLoadPending\(conversationId\)/);
});

test('刷新指定对话时立即恢复且加载完成前不闪出无项目状态', () => {
    const scheduleSource = functionSource(router, 'scheduleChatConversationFromHash', 'navigateToConversation');
    const restoreStateSource = functionSource(router, 'setChatConversationRestorePending', 'finishChatConversationRestore');
    const loadSource = functionSource(chat, 'loadConversation', 'attachDeleteTurnButton');
    const css = fs.readFileSync('web/static/css/style.css', 'utf8');

    assert.match(router, /scheduleChatConversationFromHash\(0\)/);
    assert.doesNotMatch(router, /scheduleChatConversationFromHash\((200|500)\)/);
    assert.match(scheduleSource, /setChatConversationRestorePending\(conversationId, true\)/);
    assert.match(restoreStateSource, /is-conversation-restoring/);
    assert.match(restoreStateSource, /aria-busy/);
    assert.match(loadSource, /finally \{[\s\S]*?finishChatConversationRestore\(conversationId\)/);
    assert.match(css, /\.chat-container\.is-conversation-restoring #chat-messages/);
    assert.match(css, /\.chat-container\.is-conversation-restoring #chat-input-container/);
    assert.match(html, /router\.js\?v=[A-Za-z0-9._-]+/);
    assert.match(html, /chat\.js\?v=[A-Za-z0-9._-]+/);
});

test('刷新运行中回复会复用已持久化 planning 并继续追加未来增量', () => {
    const findSource = functionSource(monitor, 'findRestoredMainResponseStreamItem', 'responseStreamStateFromRestoredItem');
    const handleSource = functionSource(monitor, 'handleStreamEvent', 'hitlApprovalTranslate');

    assert.match(findSource, /timeline-item-planning/);
    assert.match(findSource, /dataset\.responseStreamId/);
    assert.match(handleSource, /case 'response_start':[\s\S]*?findRestoredMainResponseStreamItem/);
    assert.match(handleSource, /case 'response_delta':[\s\S]*?responseStreamStateFromRestoredItem/);
    assert.match(monitor, /item\.dataset\.responseStreamId = String\(options\.data\.streamId\)/);
});

test('Eino 原生模型 retry/failover 事件在主聊天和 WebShell 中可见', () => {
    const handleSource = functionSource(monitor, 'handleStreamEvent', 'hitlApprovalTranslate');
    assert.match(handleSource, /case 'eino_model_retry'/);
    assert.match(handleSource, /formatEinoModelRetryTitle/);
    assert.match(handleSource, /case 'eino_model_failover'/);
    assert.match(handleSource, /formatEinoModelFailoverTitle/);
    assert.match(monitor, /function formatEinoModelRetryMessage/);
    assert.match(monitor, /function formatEinoModelFailoverMessage/);
    assert.match(webshell, /_et === 'eino_model_retry'/);
    assert.match(webshell, /_et === 'eino_model_failover'/);
});

test('非仪表盘 hash 首屏在路由确定前隐藏默认仪表盘', () => {
    const css = fs.readFileSync('web/static/css/style.css', 'utf8');
    assert.match(html, /document\.documentElement\.classList\.add\('initial-route-pending'\)/);
    assert.match(router, /document\.documentElement\.classList\.remove\('initial-route-pending'\)/);
    assert.match(css, /html\.initial-route-pending \.content-area \{[\s\S]*?visibility: hidden;/);
});

test('刷新恢复运行中助手消息时隐藏处理中占位且终态正文会重新显示', () => {
    const loadSource = functionSource(chat, 'loadConversation', 'attachDeleteTurnButton');
    const updateSource = functionSource(monitor, 'updateAssistantBubbleContent', 'isConversationTaskRunning');

    assert.match(loadSource, /hideAssistantPlaceholder: isAssistantPlaceholder/);
    assert.match(chat, /bubble\.hidden = true/);
    assert.match(updateSource, /assistant-placeholder-content/);
    assert.match(updateSource, /bubble\.hidden = false/);
});

test('刷新补流任务完成后强制折叠自动展开的迭代详情', () => {
    const collapseSource = functionSource(monitor, 'collapseAllProgressDetails', 'getAssistantId');
    const attachSource = functionSource(monitor, 'attachRunningTaskEventStream', 'parseToolCallArgsFromData');

    assert.match(collapseSource, /options/);
    assert.match(collapseSource, /forceCollapse/);
    assert.match(collapseSource, /delete detailsContainer\.dataset\.userExpanded/);
    assert.match(attachSource, /collapseAllProgressDetails\(finalAssistant\.id, progressId, \{ force: true \}\)/);
    assert.doesNotMatch(attachSource, /if \(keepExpanded\)/);
});

test('暗色模式用户气泡使用协调的深蓝灰层级', () => {
    const css = fs.readFileSync('web/static/css/style.css', 'utf8');
    assert.match(css, /html\[data-theme="dark"\] \.message\.user \.message-bubble \{[\s\S]*?background: #1b2638;/);
    assert.match(css, /border-color: rgba\(96, 165, 250, 0\.18\)/);
});

test('暗色模式对话三点悬浮不会触发浅色父行背景', () => {
    const css = fs.readFileSync('web/static/css/style.css', 'utf8');
    assert.match(css, /html\[data-theme="dark"\] \.project-conversation-row:hover \.project-conversation-item/);
    assert.match(css, /html\[data-theme="dark"\] \.project-folder-action:hover,[\s\S]*?background: rgba\(71, 85, 105, 0\.28\);[\s\S]*?box-shadow: none;/);
    assert.match(html, /style\.css\?v=[A-Za-z0-9._-]+/);
});
