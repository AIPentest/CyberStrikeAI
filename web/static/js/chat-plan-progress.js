(function (root) {
    'use strict';

    const PLAN_TOOL_NAMES = new Set(['taskcreate', 'taskupdate', 'tasklist', 'taskget']);
    const ACTIVE_POLL_INTERVAL_MS = 1500;
    const ERROR_RETRY_INTERVAL_MS = 4000;
    const FINAL_STATE_HOLD_MS = 2400;

    function normalizeTask(raw, index) {
        const task = raw && typeof raw === 'object' ? raw : {};
        const status = String(task.status || 'pending').trim().toLowerCase();
        return {
            id: String(task.id || (index + 1)),
            subject: String(task.subject || '').trim(),
            description: String(task.description || '').trim(),
            activeForm: String(task.activeForm || '').trim(),
            status: ['pending', 'in_progress', 'completed', 'deleted'].includes(status) ? status : 'pending'
        };
    }

    function deriveProgress(rawTasks) {
        const tasks = (Array.isArray(rawTasks) ? rawTasks : [])
            .map(normalizeTask)
            .filter((task) => task.status !== 'deleted');
        let activeIndex = tasks.findIndex((task) => task.status === 'in_progress');
        if (activeIndex < 0) activeIndex = tasks.findIndex((task) => task.status !== 'completed');
        if (activeIndex < 0 && tasks.length) activeIndex = tasks.length - 1;
        return {
            tasks,
            total: tasks.length,
            completed: tasks.filter((task) => task.status === 'completed').length,
            activeStep: activeIndex >= 0 ? activeIndex + 1 : 0,
            allCompleted: tasks.length > 0 && tasks.every((task) => task.status === 'completed')
        };
    }

    function applyTaskUpdate(rawTasks, args) {
        const update = args && typeof args === 'object' ? args : {};
        const taskID = String(update.taskId || update.taskID || '').trim();
        if (!taskID) return deriveProgress(rawTasks).tasks;
        return deriveProgress(rawTasks).tasks
            .filter((task) => !(String(update.status || '').toLowerCase() === 'deleted' && task.id === taskID))
            .map((task) => {
                if (task.id !== taskID) return task;
                return Object.assign({}, task, {
                    subject: String(update.subject || task.subject).trim(),
                    description: String(update.description || task.description).trim(),
                    activeForm: String(update.activeForm || task.activeForm).trim(),
                    status: String(update.status || task.status).trim().toLowerCase()
                });
            });
    }

    if (typeof module !== 'undefined' && module.exports) {
        module.exports = { normalizeTask, deriveProgress, applyTaskUpdate };
        return;
    }

    const state = {
        conversationId: '',
        tasks: [],
        signature: '',
        expanded: false,
        requestSequence: 0,
        abortController: null,
        refreshTimer: null,
        finalHoldUntil: 0,
        taskCalls: new Map()
    };

    const host = root.document && root.document.getElementById('agent-plan-progress');
    if (!host) return;

    let passiveHoverAnchor = null;

    function clearPassiveHoverVisual() {
        host.classList.remove('is-hover-active');
    }

    function resetPassiveHover() {
        clearPassiveHoverVisual();
        passiveHoverAnchor = null;
    }

    function disarmPassiveHover(event) {
        clearPassiveHoverVisual();
        if (!event || !Number.isFinite(event.clientX) || !Number.isFinite(event.clientY)) return;
        passiveHoverAnchor = { x: event.clientX, y: event.clientY };
    }

    function armHoverAfterPointerMove(event) {
        if (event && event.pointerType && event.pointerType !== 'mouse') return;
        if (passiveHoverAnchor) {
            if (!event || !Number.isFinite(event.clientX) || !Number.isFinite(event.clientY)) return;
            // Smooth scrolling and layout shifts may emit pointermove without the
            // physical pointer moving. Keep the panel locked until coordinates change.
            if (event.clientX === passiveHoverAnchor.x && event.clientY === passiveHoverAnchor.y) return;
            passiveHoverAnchor = null;
        }
        host.classList.add('is-hover-active');
    }

    host.addEventListener('pointermove', armHoverAfterPointerMove);
    host.addEventListener('pointerleave', clearPassiveHoverVisual);
    const returnLatestButton = root.document.getElementById('chat-return-latest');
    if (returnLatestButton) {
        // Clicking the return-to-latest control moves this plan chip downward.
        // Clear the hover gate before that layout shift so a stationary pointer
        // cannot accidentally reveal the plan panel underneath it.
        returnLatestButton.addEventListener('pointerdown', disarmPassiveHover);
        returnLatestButton.addEventListener('click', disarmPassiveHover);
    }

    function translate(key, fallback, params) {
        if (typeof root.t === 'function') {
            const value = root.t(key, params || {});
            if (value && value !== key) return value;
        }
        let value = fallback;
        Object.entries(params || {}).forEach(([name, replacement]) => {
            value = value.replaceAll('{{' + name + '}}', String(replacement));
        });
        return value;
    }

    function createSVG(className, pathData) {
        const namespace = 'http://www.w3.org/2000/svg';
        const svg = root.document.createElementNS(namespace, 'svg');
        svg.setAttribute('viewBox', '0 0 24 24');
        svg.setAttribute('aria-hidden', 'true');
        svg.classList.add(className);
        const path = root.document.createElementNS(namespace, 'path');
        path.setAttribute('d', pathData);
        svg.appendChild(path);
        return svg;
    }

    function statusIcon(status) {
        const icon = root.document.createElement('span');
        icon.className = 'agent-plan-task-status agent-plan-task-status--' + status;
        icon.setAttribute('aria-hidden', 'true');
        if (status === 'completed') {
            icon.appendChild(createSVG('agent-plan-task-check', 'M7.5 12.5 10.5 15.5 16.8 8.8'));
        }
        return icon;
    }

    function taskLabel(task) {
        if (task.status === 'in_progress' && task.activeForm) return task.activeForm;
        return task.subject || translate('chat.taskProgressUnnamed', 'Unnamed Task');
    }

    function render(force) {
        const progress = deriveProgress(state.tasks);
        const signature = JSON.stringify(progress.tasks) + '|' + state.expanded;
        if (!force && signature === state.signature) return;
        state.signature = signature;
        host.replaceChildren();
        if (!progress.total) {
            host.hidden = true;
            resetPassiveHover();
            return;
        }
        host.hidden = false;
        host.classList.toggle('is-open', state.expanded);

        const panel = root.document.createElement('div');
        panel.className = 'agent-plan-progress-panel';
        panel.id = 'agent-plan-progress-panel';
        panel.setAttribute('role', 'status');
        panel.setAttribute('aria-label', translate('chat.taskProgressDetails', 'Task Progress Details'));
        progress.tasks.forEach((task) => {
            const row = root.document.createElement('div');
            row.className = 'agent-plan-task agent-plan-task--' + task.status;
            row.appendChild(statusIcon(task.status));
            const label = root.document.createElement('span');
            label.className = 'agent-plan-task-label';
            label.textContent = taskLabel(task);
            if (task.description) label.title = task.description;
            row.appendChild(label);
            panel.appendChild(row);
        });

        const trigger = root.document.createElement('button');
        trigger.type = 'button';
        trigger.className = 'agent-plan-progress-trigger';
        trigger.setAttribute('aria-controls', panel.id);
        trigger.setAttribute('aria-expanded', state.expanded ? 'true' : 'false');
        trigger.setAttribute('aria-label', translate('chat.taskProgressOpen', 'View task progress'));
        trigger.appendChild(statusIcon(progress.allCompleted ? 'completed' : 'in_progress'));
        const count = root.document.createElement('span');
        count.className = 'agent-plan-progress-count';
        count.textContent = translate('chat.taskProgressStep', 'Step {{current}} / {{total}}', {
            current: progress.activeStep,
            total: progress.total
        });
        trigger.appendChild(count);
        trigger.addEventListener('click', () => {
            state.expanded = !state.expanded;
            render(true);
            if (state.expanded) host.querySelector('.agent-plan-progress-trigger')?.focus();
        });

        host.append(panel, trigger);
    }

    function currentConversationId() {
        return String(root.currentConversationId || '').trim();
    }

    function isCurrentConversationRunning() {
        const conversationId = currentConversationId();
        if (!conversationId) return false;
        try {
            const checker = typeof root.isConversationTaskRunning === 'function'
                ? root.isConversationTaskRunning
                : (typeof isConversationTaskRunning === 'function' ? isConversationTaskRunning : null);
            return !!(checker && checker(conversationId));
        } catch (e) {
            return false;
        }
    }

    function shouldContinuePolling(payload) {
        if (!currentConversationId() || root.document.hidden) return false;
        if (payload && payload.running === false) return false;
        if (payload && payload.running === true) {
            return state.tasks.length > 0 || Date.now() < state.finalHoldUntil;
        }
        return isCurrentConversationRunning() && state.tasks.length > 0;
    }

    function setConversation(conversationId) {
        const next = String(conversationId || '').trim();
        if (next === state.conversationId) return false;
        state.conversationId = next;
        state.tasks = [];
        state.signature = '';
        state.expanded = false;
        state.finalHoldUntil = 0;
        state.taskCalls.clear();
        resetPassiveHover();
        state.requestSequence += 1;
        if (state.abortController) state.abortController.abort();
        state.abortController = null;
        render(true);
        return true;
    }

    async function fetchPlanTasks() {
        const conversationId = currentConversationId();
        setConversation(conversationId);
        if (!conversationId || root.document.hidden) return;
        const sequence = ++state.requestSequence;
        if (state.abortController) state.abortController.abort();
        const controller = new AbortController();
        state.abortController = controller;
        let payload = null;
        let retryAfterError = false;
        try {
            const fetcher = typeof root.apiFetch === 'function' ? root.apiFetch : root.fetch.bind(root);
            const response = await fetcher('/api/conversations/' + encodeURIComponent(conversationId) + '/plan-tasks', {
                signal: controller.signal
            });
            if (!response.ok) throw new Error('HTTP ' + response.status);
            payload = await response.json();
            if (sequence !== state.requestSequence || conversationId !== state.conversationId) return;
            if (payload && payload.running === false) {
                state.tasks = [];
                state.expanded = false;
                state.finalHoldUntil = 0;
                render(false);
                return;
            }
            const tasks = deriveProgress(payload && payload.tasks).tasks;
            if (!tasks.length && state.tasks.length && Date.now() < state.finalHoldUntil) return;
            state.tasks = tasks;
            render(false);
        } catch (error) {
            if (error && error.name === 'AbortError') return;
            retryAfterError = true;
            // A task list is supplemental UI; a transient poll failure must not
            // interfere with the chat or erase the last known task state.
        } finally {
            if (sequence === state.requestSequence) {
                state.abortController = null;
                if (shouldContinuePolling(payload)) {
                    scheduleRefresh(retryAfterError ? ERROR_RETRY_INTERVAL_MS : ACTIVE_POLL_INTERVAL_MS);
                }
            }
        }
    }

    function scheduleRefresh(delay) {
        root.clearTimeout(state.refreshTimer);
        state.refreshTimer = root.setTimeout(fetchPlanTasks, Number(delay) || 0);
    }

    function handlePlanToolEvent(event) {
        const detail = event && event.detail && typeof event.detail === 'object' ? event.detail : {};
        const conversationId = String(detail.conversationId || '').trim();
        if (!conversationId || conversationId !== currentConversationId()) return;
        const data = detail.data && typeof detail.data === 'object' ? detail.data : {};
        const toolName = String(data.toolName || '').trim().toLowerCase();
        if (!PLAN_TOOL_NAMES.has(toolName)) return;
        const callId = String(data.toolCallId || '').trim();
        if (detail.eventType === 'tool_call') {
            if (callId) state.taskCalls.set(callId, { toolName, args: data.argumentsObj || {} });
            scheduleRefresh(60);
            return;
        }
        if (detail.eventType !== 'tool_result') return;
        const call = callId ? state.taskCalls.get(callId) : null;
        if (callId) state.taskCalls.delete(callId);
        if (data.success !== false && call && call.toolName === 'taskupdate') {
            state.tasks = applyTaskUpdate(state.tasks, call.args);
            const progress = deriveProgress(state.tasks);
            if (progress.allCompleted) state.finalHoldUntil = Date.now() + FINAL_STATE_HOLD_MS;
            render(false);
        }
        scheduleRefresh(100);
    }

    function handleConversationTaskStateChanged(event) {
        const detail = event && event.detail && typeof event.detail === 'object' ? event.detail : {};
        const conversationId = String(detail.conversationId || '').trim();
        const currentId = currentConversationId();
        if (conversationId && conversationId !== currentId) return;
        if (isCurrentConversationRunning() || state.tasks.length) {
            scheduleRefresh(0);
        }
    }

    root.addEventListener('agent-plan-task-event', handlePlanToolEvent);
    root.addEventListener('conversation-task-state-changed', handleConversationTaskStateChanged);
    root.addEventListener('conversation-changed', (event) => {
        setConversation(event && event.detail ? event.detail.conversationId : currentConversationId());
        scheduleRefresh(0);
    });
    root.document.addEventListener('visibilitychange', () => {
        if (!root.document.hidden && (isCurrentConversationRunning() || state.tasks.length)) scheduleRefresh(0);
    });
    root.document.addEventListener('keydown', (event) => {
        if (event.key !== 'Escape' || !state.expanded) return;
        state.expanded = false;
        render(true);
    });
    root.document.addEventListener('pointerdown', (event) => {
        if (!state.expanded || host.contains(event.target)) return;
        state.expanded = false;
        render(true);
    });

    setConversation(currentConversationId());
    fetchPlanTasks();
})(typeof window !== 'undefined' ? window : globalThis);
