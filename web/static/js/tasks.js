// Task management page features
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

/** Interpolated values are not converted to HTML entities (to avoid the / in dates becoming &#x2F; and then being garbled by escapeHtml) */
function _tPlain(key, opts) {
    if (typeof window.t !== 'function') return key;
    const base = opts && typeof opts === 'object' ? opts : {};
    const interp = base.interpolation && typeof base.interpolation === 'object' ? base.interpolation : {};
    return window.t(key, {
        ...base,
        interpolation: { escapeValue: false, ...interp }
    });
}

/** A valid agentMode consistent with the create queue / API */
const BATCH_QUEUE_AGENT_MODES = ['eino_single', 'deep', 'plan_execute', 'supervisor'];

function isBatchQueueAgentMode(mode) {
    return BATCH_QUEUE_AGENT_MODES.indexOf(String(mode || '').toLowerCase()) >= 0;
}

/** Batch queue agentMode display text (consistent with conversation mode naming) */
function batchQueueAgentModeLabel(mode) {
    const m = String(mode || 'eino_single').toLowerCase();
    if (m === 'eino_single') return _t('chat.agentModeEinoSingle');
    if (m === 'deep') return _t('chat.agentModeDeep');
    if (m === 'plan_execute') return _t('chat.agentModePlanExecuteLabel');
    if (m === 'supervisor') return _t('chat.agentModeSupervisorLabel');
    return _t('chat.agentModeEinoSingle');
}

/** Cron queue display text in states such as 「this round completed」 (underlying status unchanged, only UI emphasizes recurring scheduling) */
function getBatchQueueStatusPresentation(queue) {
    const map = {
        pending: { text: _t('tasks.statusPending'), class: 'batch-queue-status-pending' },
        running: { text: _t('tasks.statusRunning'), class: 'batch-queue-status-running' },
        paused: { text: _t('tasks.statusPaused'), class: 'batch-queue-status-paused' },
        completed: { text: _t('tasks.statusCompleted'), class: 'batch-queue-status-completed' },
        cancelled: { text: _t('tasks.statusCancelled'), class: 'batch-queue-status-cancelled' }
    };
    const base = map[queue.status] || { text: queue.status, class: 'batch-queue-status-unknown' };
    const cronOn = queue.scheduleMode === 'cron' && queue.scheduleEnabled !== false;
    const nextStr = queue.nextRunAt ? new Date(queue.nextRunAt).toLocaleString() : '';
    const empty = { sublabel: null, progressNote: null, callout: null };

    const failedCount = (queue.tasks || []).filter(task => task.status === 'failed').length;
    if (queue.status === 'completed' && failedCount > 0) {
        const allFailed = failedCount === (queue.tasks || []).length;
        return {
            text: allFailed ? _t('tasks.statusFailed') : _tPlain('tasks.statusEndedWithFailures', { count: failedCount }),
            class: 'batch-queue-status-failed',
            sublabel: cronOn && nextStr ? _tPlain('tasks.cronNextRunLine', { time: nextStr }) : null,
            progressNote: _t('tasks.finishedProgressHint'),
            callout: cronOn ? _t('tasks.cronRecurringCallout') : null
        };
    }

    if (cronOn && queue.status === 'completed') {
        return {
            text: _t('tasks.statusCronCycleIdle'),
            class: 'batch-queue-status-cron-cycle',
            sublabel: nextStr ? _tPlain('tasks.cronNextRunLine', { time: nextStr }) : null,
            progressNote: _t('tasks.cronRoundDoneProgressHint'),
            callout: _t('tasks.cronRecurringCallout')
        };
    }
    if (cronOn && queue.status === 'running') {
        return {
            text: _t('tasks.statusCronRunning'),
            class: 'batch-queue-status-running batch-queue-cron-active',
            sublabel: nextStr ? _tPlain('tasks.cronNextRunLine', { time: nextStr }) : null,
            progressNote: _t('tasks.cronRunningProgressHint'),
            callout: null
        };
    }
    if (cronOn && queue.status === 'pending' && nextStr) {
        return {
            ...base,
            ...empty,
            sublabel: _tPlain('tasks.cronPendingScheduled', { time: nextStr }),
            progressNote: _t('tasks.cronPendingProgressNote')
        };
    }
    return { ...base, ...empty };
}

/** Whether the queue is in the "subtask list/copy editable" idle state (aligned with backend batch_task_manager.queueAllowsTaskListMutationLocked) */
function batchQueueAllowsSubtaskMutation(queue) {
    if (!queue) return false;
    if (queue.status === 'running') return false;
    const hasRunningSubtask = Array.isArray(queue.tasks) && queue.tasks.some(t => t && t.status === 'running');
    if (hasRunningSubtask) return false;
    return queue.status === 'pending' || queue.status === 'paused' || queue.status === 'completed' || queue.status === 'cancelled';
}

/** Whether to allow initiating a single run for the specified subtask (aligned with backend queueAllowsSingleTaskRunLocked) */
function batchQueueCanRunSingleTask(queue, task) {
    if (!queue || !task) return false;
    if (task.status === 'running') return false;
    if (queue.status === 'running') return false;
    return queue.status === 'pending' || queue.status === 'paused' || queue.status === 'completed' || queue.status === 'cancelled';
}

function batchQueueRunSingleTaskDisabledReason(queue, task) {
    if (!queue || !task) return _t('tasks.runSingleTaskUnavailable');
    if (task.status === 'running') return _t('tasks.runSingleTaskUnavailableSelf');
    if (queue.status === 'running') return _t('tasks.runSingleTaskUnavailableQueue');
    return _t('tasks.runSingleTaskUnavailable');
}

// HTML escape function (if not defined)
if (typeof escapeHtml === 'undefined') {
    function escapeHtml(text) {
        if (text == null) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

function escapeJsString(text) {
    return JSON.stringify(String(text == null ? '' : text));
}

function escapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function escapeJsStringAttr(text) {
    return escapeAttr(escapeJsString(text));
}

// Task management status
const tasksState = {
    allTasks: [],
    filteredTasks: [],
    selectedTasks: new Set(),
    autoRefresh: true,
    refreshInterval: null,
    durationUpdateInterval: null,
    completedTasksHistory: [], // Save history of recently completed tasks
    showHistory: true // Whether to show history records
};

// Load completed task history from localStorage
function loadCompletedTasksHistory() {
    try {
        const saved = localStorage.getItem('tasks-completed-history');
        if (saved) {
            const history = JSON.parse(saved);
            // Only keep tasks completed within the last 24 hours
            const now = Date.now();
            const oneDayAgo = now - 24 * 60 * 60 * 1000;
            tasksState.completedTasksHistory = history.filter(task => {
                const completedTime = task.completedAt || task.startedAt;
                return completedTime && new Date(completedTime).getTime() > oneDayAgo;
            });
            // Save the history after clearing
            saveCompletedTasksHistory();
        }
    } catch (error) {
        console.error('Failed to load completed task history:', error);
        tasksState.completedTasksHistory = [];
    }
}

// Save completed task history to localStorage
function saveCompletedTasksHistory() {
    try {
        localStorage.setItem('tasks-completed-history', JSON.stringify(tasksState.completedTasksHistory));
    } catch (error) {
        console.error('Failed to save completed task history:', error);
    }
}

// Update completed task history
function updateCompletedTasksHistory(currentTasks) {
    // Save all current tasks as a snapshot (for comparison next time)
    const currentTaskIds = new Set(currentTasks.map(t => t.conversationId));
    
    // If it's the first load, just save the current task snapshot
    if (tasksState.allTasks.length === 0) {
        return;
    }
    
    const previousTaskIds = new Set(tasksState.allTasks.map(t => t.conversationId));
    
    // Find the just-completed tasks (previously existed but now nonexistent)
    // Once the task disappears from the list, consider it completed
    const justCompleted = tasksState.allTasks.filter(task => {
        return previousTaskIds.has(task.conversationId) && !currentTaskIds.has(task.conversationId);
    });
    
    // Add the just-completed task to history
    justCompleted.forEach(task => {
        // Check if it already exists (to avoid duplicate additions)
        const exists = tasksState.completedTasksHistory.some(t => t.conversationId === task.conversationId);
        if (!exists) {
            // If the task status is not a final state, mark it as completed
            const finalStatus = ['completed', 'failed', 'timeout', 'cancelled', 'cleanup_unconfirmed'].includes(task.status)
                ? task.status 
                : 'completed';
            
            tasksState.completedTasksHistory.push({
                conversationId: task.conversationId,
                message: task.title || task.message || _t('tasks.unnamedTask'),
                startedAt: task.startedAt,
                status: finalStatus,
                completedAt: new Date().toISOString()
            });
        }
    });
    
    // Limit the number of history records (keep a maximum of 50 entries)
    if (tasksState.completedTasksHistory.length > 50) {
        tasksState.completedTasksHistory = tasksState.completedTasksHistory
            .sort((a, b) => new Date(b.completedAt || b.startedAt) - new Date(a.completedAt || a.startedAt))
            .slice(0, 50);
    }
    
    saveCompletedTasksHistory();
}

// Load task list
async function loadTasks() {
    const listContainer = document.getElementById('tasks-list');
    if (!listContainer) return;
    
    listContainer.innerHTML = '<div class="loading-spinner">' + _t('tasks.loadingTasks') + '</div>';

    try {
        // Load running tasks and completed task history in parallel
        const [activeResponse, completedResponse] = await Promise.allSettled([
            apiFetch('/api/agent-loop/tasks'),
            apiFetch('/api/agent-loop/tasks/completed').catch(() => null) // If the API does not exist, return null
        ]);

        // Process running tasks
        if (activeResponse.status === 'rejected' || !activeResponse.value || !activeResponse.value.ok) {
            throw new Error(_t('tasks.loadTaskListFailed'));
        }

        const activeResult = await activeResponse.value.json();
        const activeTasks = activeResult.tasks || [];
        
        // Load completed task history (if API is available)
        let completedTasks = [];
        if (completedResponse.status === 'fulfilled' && completedResponse.value && completedResponse.value.ok) {
            try {
                const completedResult = await completedResponse.value.json();
                completedTasks = completedResult.tasks || [];
            } catch (e) {
                console.warn('Failed to parse completed task history:', e);
            }
        }
        
        // Save all tasks
        tasksState.allTasks = activeTasks;
        
        // Update completed task history (fetched from backend API)
        if (completedTasks.length > 0) {
            // Merge backend history and local history (deduplicate)
            const backendTaskIds = new Set(completedTasks.map(t => t.conversationId));
            const localHistory = tasksState.completedTasksHistory.filter(t => 
                !backendTaskIds.has(t.conversationId)
            );
            
            // Backend history records take priority, then add those unique to local
            tasksState.completedTasksHistory = [
                ...completedTasks.map(t => ({
                    conversationId: t.conversationId,
                    message: t.message || _t('tasks.unnamedTask'),
                    startedAt: t.startedAt,
                    status: t.status || 'completed',
                    completedAt: t.completedAt || new Date().toISOString()
                })),
                ...localHistory
            ];
            
            // Limit history record count
            if (tasksState.completedTasksHistory.length > 50) {
                tasksState.completedTasksHistory = tasksState.completedTasksHistory
                    .sort((a, b) => new Date(b.completedAt || b.startedAt) - new Date(a.completedAt || a.startedAt))
                    .slice(0, 50);
            }
            
            saveCompletedTasksHistory();
        } else {
            // If the backend API is unavailable, still use frontend logic to update history
            updateCompletedTasksHistory(activeTasks);
        }
        
        updateTaskStats(activeTasks);
        filterAndSortTasks();
        startDurationUpdates();
    } catch (error) {
        console.error('Failed to load task:', error);
        listContainer.innerHTML = `
            <div class="tasks-empty">
                <p>${_t('tasks.loadFailedRetry')}: ${escapeHtml(error.message)}</p>
                <button class="btn-secondary" onclick="loadTasks()">${_t('tasks.retry')}</button>
            </div>
        `;
    }
}

// Update task statistics
function updateTaskStats(tasks) {
    const stats = {
        running: 0,
        cancelling: 0,
        completed: 0,
        failed: 0,
        timeout: 0,
        cancelled: 0,
        total: tasks.length
    };

    tasks.forEach(task => {
        if (task.status === 'running') {
            stats.running++;
        } else if (['cancelling', 'cleaning', 'cleanup_failed'].includes(task.status)) {
            stats.cancelling++;
        } else if (task.status === 'completed') {
            stats.completed++;
        } else if (task.status === 'failed') {
            stats.failed++;
        } else if (task.status === 'timeout') {
            stats.timeout++;
        } else if (task.status === 'cancelled') {
            stats.cancelled++;
        }
    });

    const statRunning = document.getElementById('stat-running');
    const statCancelling = document.getElementById('stat-cancelling');
    const statCompleted = document.getElementById('stat-completed');
    const statTotal = document.getElementById('stat-total');

    if (statRunning) statRunning.textContent = stats.running;
    if (statCancelling) statCancelling.textContent = stats.cancelling;
    if (statCompleted) statCompleted.textContent = stats.completed;
    if (statTotal) statTotal.textContent = stats.total;
}

// Filter task
function filterTasks() {
    filterAndSortTasks();
}

// Sorting task
function sortTasks() {
    filterAndSortTasks();
}

// Filter and sort tasks
function filterAndSortTasks() {
    const statusFilter = document.getElementById('tasks-status-filter')?.value || 'all';
    const sortBy = document.getElementById('tasks-sort-by')?.value || 'time-desc';
    
    // Merge current task and historical tasks
    let allTasks = [...tasksState.allTasks];
    
    // If displaying history, add history task
    if (tasksState.showHistory) {
        const historyTasks = tasksState.completedTasksHistory
            .filter(ht => !tasksState.allTasks.some(t => t.conversationId === ht.conversationId))
            .map(ht => ({ ...ht, isHistory: true }));
        allTasks = [...allTasks, ...historyTasks];
    }
    
    // Filter
    let filtered = allTasks;
    if (statusFilter === 'active') {
        // Only running tasks (excluding history)
        filtered = tasksState.allTasks.filter(task => 
            ['running', 'cancelling', 'cleaning', 'cleanup_failed'].includes(task.status)
        );
    } else if (statusFilter === 'history') {
        // History only
        filtered = allTasks.filter(task => task.isHistory);
    } else if (statusFilter !== 'all') {
        filtered = allTasks.filter(task => task.status === statusFilter);
    }
    
    // Sorting
    filtered.sort((a, b) => {
        const aTime = new Date(a.completedAt || a.startedAt);
        const bTime = new Date(b.completedAt || b.startedAt);
        
        switch (sortBy) {
            case 'time-asc':
                return aTime - bTime;
            case 'time-desc':
                return bTime - aTime;
            case 'status':
                return (a.status || '').localeCompare(b.status || '');
            case 'message':
                return (a.message || '').localeCompare(b.message || '');
            default:
                return 0;
        }
    });
    
    tasksState.filteredTasks = filtered;
    renderTasks(filtered);
    updateBatchActions();
}

// Toggle display history
function toggleShowHistory(show) {
    tasksState.showHistory = show;
    localStorage.setItem('tasks-show-history', show ? 'true' : 'false');
    filterAndSortTasks();
}

// Calculate execution duration
function calculateDuration(startedAt) {
    if (!startedAt) return _t('tasks.unknown');
    const start = new Date(startedAt);
    const now = new Date();
    const diff = Math.floor((now - start) / 1000);
    
    if (diff < 60) {
        return diff + _t('tasks.durationSeconds');
    } else if (diff < 3600) {
        const minutes = Math.floor(diff / 60);
        const seconds = diff % 60;
        return minutes + _t('tasks.durationMinutes') + ' ' + seconds + _t('tasks.durationSeconds');
    } else {
        const hours = Math.floor(diff / 3600);
        const minutes = Math.floor((diff % 3600) / 60);
        return hours + _t('tasks.durationHours') + ' ' + minutes + _t('tasks.durationMinutes');
    }
}

// Start duration update
function startDurationUpdates() {
    // Clear old timers
    if (tasksState.durationUpdateInterval) {
        clearInterval(tasksState.durationUpdateInterval);
    }
    
    // Update execution duration once per second
    tasksState.durationUpdateInterval = setInterval(() => {
        updateTaskDurations();
    }, 1000);
}

// Update task execution duration display
function updateTaskDurations() {
    const taskItems = document.querySelectorAll('.task-item[data-task-id]');
    taskItems.forEach(item => {
        const startedAt = item.dataset.startedAt;
        const status = item.dataset.status;
        const durationEl = item.querySelector('.task-duration');
        
        if (durationEl && startedAt && (['running', 'cancelling', 'cleaning', 'cleanup_failed'].includes(status))) {
            durationEl.textContent = calculateDuration(startedAt);
        }
    });
}

// Render task list
function renderTasks(tasks) {
    const listContainer = document.getElementById('tasks-list');
    if (!listContainer) return;

    if (tasks.length === 0) {
        listContainer.innerHTML = `
            <div class="tasks-empty">
                <p>${_t('tasks.noMatchingTasks')}</p>
                ${tasksState.allTasks.length === 0 && tasksState.completedTasksHistory.length > 0 ? 
                    '<p style="margin-top: 8px; color: var(--text-muted); font-size: 0.875rem;">' + _t('tasks.historyHint') + '</p>' : ''}
            </div>
        `;
        return;
    }

    // state mapping
    const statusMap = {
        'running': { text: _t('tasks.statusRunning'), class: 'task-status-running' },
        'cleaning': { text: _t('tasks.statusCleaning'), class: 'task-status-cancelling' },
        'cleanup_unconfirmed': { text: _t('tasks.statusCleanupUnconfirmed'), class: 'task-status-failed' },
        'cleanup_failed': { text: _t('tasks.statusCleanupFailed'), class: 'task-status-failed' },
        'cancelling': { text: _t('tasks.statusCancelling'), class: 'task-status-cancelling' },
        'failed': { text: _t('tasks.statusFailed'), class: 'task-status-failed' },
        'timeout': { text: _t('tasks.statusTimeout'), class: 'task-status-timeout' },
        'cancelled': { text: _t('tasks.statusCancelled'), class: 'task-status-cancelled' },
        'completed': { text: _t('tasks.statusCompleted'), class: 'task-status-completed' }
    };

    // Separate current tasks and historical tasks
    const activeTasks = tasks.filter(t => !t.isHistory);
    const historyTasks = tasks.filter(t => t.isHistory);

    let html = '';
    
    // Render the current task
    if (activeTasks.length > 0) {
        html += activeTasks.map(task => renderTaskItem(task, statusMap)).join('');
    }
    
    // Render history task
    if (historyTasks.length > 0) {
        html += `<div class="tasks-history-section">
            <div class="tasks-history-header">
                <span class="tasks-history-title">📜 ` + _t('tasks.recentCompletedTasks') + `</span>
                <button class="btn-secondary btn-small" onclick="clearTasksHistory()">` + _t('tasks.clearHistory') + `</button>
            </div>
            ${historyTasks.map(task => renderTaskItem(task, statusMap, true)).join('')}
        </div>`;
    }
    
    listContainer.innerHTML = html;
}

// Render a single task item
function renderTaskItem(task, statusMap, isHistory = false) {
    const startedTime = task.startedAt ? new Date(task.startedAt) : null;
    const completedTime = task.completedAt ? new Date(task.completedAt) : null;
    
    const timeText = startedTime && !isNaN(startedTime.getTime())
        ? startedTime.toLocaleString('zh-CN', { 
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit'
        })
        : _t('tasks.unknownTime');
    
    const completedText = completedTime && !isNaN(completedTime.getTime())
        ? completedTime.toLocaleString('zh-CN', { 
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit'
        })
        : '';

    const status = statusMap[task.status] || { text: task.status, class: 'task-status-unknown' };
    const isFinalStatus = ['failed', 'timeout', 'cancelled', 'completed', 'cleanup_unconfirmed'].includes(task.status);
    const canCancel = !isFinalStatus && !['cancelling', 'cleaning'].includes(task.status) && !isHistory;
    const isSelected = tasksState.selectedTasks.has(task.conversationId);
    const duration = (['running', 'cancelling', 'cleaning', 'cleanup_failed'].includes(task.status))
        ? calculateDuration(task.startedAt) 
        : '';

    return `
        <div class="task-item ${isHistory ? 'task-item-history' : ''}" data-task-id="${escapeAttr(task.conversationId)}" data-started-at="${escapeAttr(task.startedAt)}" data-status="${escapeAttr(task.status)}">
            <div class="task-header">
                <div class="task-info">
                    ${canCancel ? `
                        <label class="task-checkbox">
                            <input type="checkbox" ${isSelected ? 'checked' : ''} 
                                   onchange="toggleTaskSelection(${escapeJsStringAttr(task.conversationId)}, this.checked)">
                        </label>
                    ` : '<div class="task-checkbox-placeholder"></div>'}
                    <span class="task-status ${status.class}">${status.text}</span>
                    ${isHistory ? '<span class="task-history-badge" title="' + _t('tasks.historyBadge') + '">📜</span>' : ''}
                    <span class="task-message" title="${escapeAttr((task.title || task.message || _t('tasks.unnamedTask')))}">${escapeHtml((task.title || task.message || _t('tasks.unnamedTask')))}</span>
                </div>
                <div class="task-actions">
                    ${duration ? `<span class="task-duration" title="${_t('tasks.duration')}">⏱ ${duration}</span>` : ''}
                    <span class="task-time" title="${isHistory && completedText ? _t('tasks.completedAt') : _t('tasks.startedAt')}">
                        ${isHistory && completedText ? completedText : timeText}
                    </span>
                    ${canCancel ? `<button class="btn-secondary btn-small" onclick="cancelTask(${escapeJsStringAttr(task.conversationId)}, this)">` + _t('tasks.cancelTask') + `</button>` : ''}
                    ${task.conversationId ? `<button class="btn-secondary btn-small" onclick="navigateToVulnerabilitiesFromTasksPage('conversation', ${escapeJsStringAttr(task.conversationId)})">` + _t('tasks.viewVulnerabilities') + `</button>` : ''}
                    ${task.conversationId ? `<button class="btn-secondary btn-small" onclick="viewConversation(${escapeJsStringAttr(task.conversationId)})">` + _t('tasks.viewConversation') + `</button>` : ''}
                </div>
            </div>
            ${task.cleanupError ? `<div class="task-details">${escapeHtml(task.cleanupError)}</div>` : ''}
            ${task.conversationId ? `
                <div class="task-details">
                    <span class="task-id-label">` + _t('tasks.conversationIdLabel') + `:</span>
                    <span class="task-id-value" title="` + _t('tasks.clickToCopy') + `" onclick="copyTaskId(${escapeJsStringAttr(task.conversationId)})">${escapeHtml(task.conversationId)}</span>
                </div>
            ` : ''}
        </div>
    `;
}

// Clear task history
function clearTasksHistory() {
    if (!confirm(_t('tasks.clearHistoryConfirm'))) {
        return;
    }
    tasksState.completedTasksHistory = [];
    saveCompletedTasksHistory();
    filterAndSortTasks();
}

// Toggle task selection
function toggleTaskSelection(conversationId, selected) {
    if (selected) {
        tasksState.selectedTasks.add(conversationId);
    } else {
        tasksState.selectedTasks.delete(conversationId);
    }
    updateBatchActions();
}

// Update batch operation UI
function updateBatchActions() {
    const batchActions = document.getElementById('tasks-batch-actions');
    const selectedCount = document.getElementById('tasks-selected-count');
    
    if (!batchActions || !selectedCount) return;
    
    const count = tasksState.selectedTasks.size;
    if (count > 0) {
        batchActions.style.display = 'flex';
        selectedCount.textContent = typeof window.t === 'function' ? window.t('mcp.selectedCount', { count: count }) : `Selected \${count} items`;
    } else {
        batchActions.style.display = 'none';
    }
}

// Clear task selection
function clearTaskSelection() {
    tasksState.selectedTasks.clear();
    updateBatchActions();
    // Re-render to update the checkbox state
    filterAndSortTasks();
}

// Batch cancel tasks
async function batchCancelTasks() {
    const selected = Array.from(tasksState.selectedTasks);
    if (selected.length === 0) return;
    
    if (!confirm(_t('tasks.confirmCancelTasks', { n: selected.length }))) {
        return;
    }
    
    let successCount = 0;
    let failCount = 0;
    
    for (const conversationId of selected) {
        try {
            const response = await apiFetch('/api/agent-loop/cancel', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ conversationId }),
            });
            
            if (response.ok) {
                successCount++;
            } else {
                failCount++;
            }
        } catch (error) {
            console.error('Failed to cancel task:', conversationId, error);
            failCount++;
        }
    }
    
    // Clear selection
    clearTaskSelection();
    
    // Refresh task list
    await loadTasks();
    
    // Display result
    if (failCount > 0) {
        alert(_t('tasks.batchCancelResultPartial', { success: successCount, fail: failCount }));
    } else {
        alert(_t('tasks.batchCancelResultSuccess', { n: successCount }));
    }
}

// Copy Task ID
function copyTaskId(conversationId) {
    navigator.clipboard.writeText(conversationId).then(() => {
        // Show copy success message
        const tooltip = document.createElement('div');
        tooltip.textContent = _t('tasks.copiedToast');
        tooltip.style.cssText = 'position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: rgba(0,0,0,0.8); color: white; padding: 8px 16px; border-radius: 4px; z-index: 10000;';
        document.body.appendChild(tooltip);
        setTimeout(() => tooltip.remove(), 1000);
    }).catch(err => {
        console.error('Copy failed:', err);
    });
}

// Cancel task
async function cancelTask(conversationId, button) {
    if (!conversationId) return;
    
    const originalText = button.textContent;
    button.disabled = true;
    button.textContent = _t('tasks.cancelling');

    try {
        const response = await apiFetch('/api/agent-loop/cancel', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ conversationId }),
        });

        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.cancelTaskFailed'));
        }

        // Remove from selection
        tasksState.selectedTasks.delete(conversationId);
        updateBatchActions();
        
        // Reload task list
        await loadTasks();
    } catch (error) {
        console.error('Failed to cancel task:', error);
        alert(_t('tasks.cancelTaskFailed') + ': ' + error.message);
        button.disabled = false;
        button.textContent = originalText;
    }
}

// View conversation
function viewConversation(conversationId) {
    if (!conversationId) return;
    
    // Switch to conversation page
    if (typeof switchPage === 'function') {
        switchPage('chat');
        // Load and select this conversation - use global function
        setTimeout(() => {
            // Try multiple ways to load the conversation
            if (typeof loadConversation === 'function') {
                loadConversation(conversationId);
            } else if (typeof window.loadConversation === 'function') {
                window.loadConversation(conversationId);
            } else {
                // If the function does not exist, try redirecting via URL
                window.location.hash = `chat?conversation=${conversationId}`;
                console.log('Switch to conversation page, conversation ID:', conversationId);
            }
        }, 500);
    }
}

// Jump to vulnerability management and filter by conversation ID or batch queue ID (queue ID uses task_id, consistent with the list filter option)
function navigateToVulnerabilitiesFromTasksPage(kind, id) {
    if (!id) return;
    const enc = encodeURIComponent(id);
    if (kind === 'queue') {
        window.location.hash = 'vulnerabilities?task_id=' + enc;
    } else if (kind === 'conversation') {
        window.location.hash = 'vulnerabilities?conversation_id=' + enc;
    }
}

// Refresh task list
async function refreshTasks() {
    await loadTasks();
}

// Toggle auto refresh
function toggleTasksAutoRefresh(enabled) {
    tasksState.autoRefresh = enabled;
    
    // Save to localStorage
    localStorage.setItem('tasks-auto-refresh', enabled ? 'true' : 'false');
    
    if (enabled) {
        // Start auto refresh
        if (!tasksState.refreshInterval) {
            tasksState.refreshInterval = setInterval(() => {
                loadBatchQueues();
            }, 5000);
        }
    } else {
        // Stop auto refresh
        if (tasksState.refreshInterval) {
            clearInterval(tasksState.refreshInterval);
            tasksState.refreshInterval = null;
        }
    }
}

// Initialize task management page
function initTasksPage() {
    initBatchQueuesFilterSelects();
    initBatchFormSelects();
    // Restore auto refresh settings
    const autoRefreshCheckbox = document.getElementById('tasks-auto-refresh');
    if (autoRefreshCheckbox) {
        const saved = localStorage.getItem('tasks-auto-refresh');
        const enabled = saved !== null ? saved === 'true' : true;
        autoRefreshCheckbox.checked = enabled;
        toggleTasksAutoRefresh(enabled);
    } else {
        toggleTasksAutoRefresh(true);
    }
    
    // Only load batch task queue
    loadBatchQueues();
}

// Clear timers (called when switching pages)
function cleanupTasksPage() {
    if (tasksState.refreshInterval) {
        clearInterval(tasksState.refreshInterval);
        tasksState.refreshInterval = null;
    }
    if (tasksState.durationUpdateInterval) {
        clearInterval(tasksState.durationUpdateInterval);
        tasksState.durationUpdateInterval = null;
    }
    tasksState.selectedTasks.clear();
    stopBatchQueueRefresh();
}

// Export function for global use
window.loadTasks = loadTasks;
window.cancelTask = cancelTask;
window.viewConversation = viewConversation;
window.refreshTasks = refreshTasks;
window.initTasksPage = initTasksPage;
window.cleanupTasksPage = cleanupTasksPage;
window.filterTasks = filterTasks;
window.sortTasks = sortTasks;
window.toggleTaskSelection = toggleTaskSelection;
window.clearTaskSelection = clearTaskSelection;
window.batchCancelTasks = batchCancelTasks;
window.copyTaskId = copyTaskId;
window.toggleTasksAutoRefresh = toggleTasksAutoRefresh;
window.toggleShowHistory = toggleShowHistory;
window.clearTasksHistory = clearTasksHistory;

// ==================== Batch Task Functionality ====================

// Batch task status
const batchQueuesState = {
    queues: [],
    currentQueueId: null,
    refreshInterval: null,
    // Filter and pagination state
    filterStatus: 'all', // 'all', 'pending', 'running', 'paused', 'completed', 'cancelled'
    searchKeyword: '',
    currentPage: 1,
    pageSize: 10,
    total: 0,
    totalPages: 1
};

async function refreshBatchProjectSelectOptions() {
    const projectSelect = document.getElementById('batch-queue-project-id');
    if (!projectSelect) return;

    const noneLabel = _t('batchImportModal.projectNone');
    projectSelect.innerHTML = `<option value="">${escapeHtml(noneLabel)}</option>`;

    try {
        let list = [];
        if (typeof fetchAllProjects === 'function') {
            list = await fetchAllProjects(false);
        } else {
            const response = await apiFetch('/api/projects?status=active&limit=500');
            if (!response.ok) {
                throw new Error(_t('projects.loadProjectsFailed'));
            }
            const data = await response.json();
            list = typeof parseProjectsListResponse === 'function'
                ? parseProjectsListResponse(data).items
                : (Array.isArray(data) ? data : (data.projects || []));
        }
        const activeProjectId = typeof getActiveProjectId === 'function' ? getActiveProjectId() || '' : '';

        list.forEach((project) => {
            if (!project || !project.id) return;
            const option = document.createElement('option');
            option.value = project.id;
            option.textContent = project.name || project.id;
            if (activeProjectId && project.id === activeProjectId) {
                option.selected = true;
            }
            projectSelect.appendChild(option);
        });
    } catch (error) {
        console.warn('Failed to load project list:', error);
    }
}

// Show the new task modal
async function showBatchImportModal() {
    const modal = document.getElementById('batch-import-modal');
    const input = document.getElementById('batch-tasks-input');
    const titleInput = document.getElementById('batch-queue-title');
    const roleSelect = document.getElementById('batch-queue-role');
    const projectSelect = document.getElementById('batch-queue-project-id');
    const agentModeSelect = document.getElementById('batch-queue-agent-mode');
    const scheduleModeSelect = document.getElementById('batch-queue-schedule-mode');
    const cronExprInput = document.getElementById('batch-queue-cron-expr');
    const executeNowCheckbox = document.getElementById('batch-queue-execute-now');
    if (modal && input) {
        input.value = '';
        if (titleInput) {
            titleInput.value = '';
        }
        const hitlSelect = document.getElementById('batch-queue-hitl-policy');
        if (hitlSelect) hitlSelect.value = '';
        // Reset role selection to default
        if (roleSelect) {
            roleSelect.value = '';
        }
        if (projectSelect) {
            projectSelect.value = '';
        }
        if (agentModeSelect) {
            agentModeSelect.value = 'eino_single';
        }
        if (scheduleModeSelect) {
            scheduleModeSelect.value = 'manual';
        }
        if (cronExprInput) {
            cronExprInput.value = '';
        }
        if (executeNowCheckbox) {
            executeNowCheckbox.checked = false;
        }
        handleBatchScheduleModeChange();
        updateBatchImportStats('');
        
        // Load and populate the role list
        if (roleSelect && typeof loadRoles === 'function') {
            try {
                const loadedRoles = await loadRoles();
                // Clear existing options (except the default option)
                roleSelect.innerHTML = '<option value="">' + _t('batchImportModal.defaultRole') + '</option>';
                
                // Add enabled roles
                const sortedRoles = loadedRoles.sort((a, b) => {
                    if (a.name === 'Default') return -1;
                    if (b.name === 'Default') return 1;
                    return (a.name || '').localeCompare(b.name || '', 'zh-CN');
                });
                
                sortedRoles.forEach(role => {
                    if (role.name !== 'Default' && role.enabled !== false) {
                        const option = document.createElement('option');
                        option.value = role.name;
                        option.textContent = role.name;
                        roleSelect.appendChild(option);
                    }
                });
            } catch (error) {
                console.error('Failed to load role list:', error);
            }
        }
        await refreshBatchProjectSelectOptions();
        refreshBatchFormSelects();

        openAppModal('batch-import-modal', { focusEl: input });
    }
}

// Close the new task modal
function closeBatchImportModal() {
    closeAppModal('batch-import-modal');
}

function handleBatchScheduleModeChange() {
    const scheduleModeSelect = document.getElementById('batch-queue-schedule-mode');
    const cronGroup = document.getElementById('batch-queue-cron-group');
    const cronExprInput = document.getElementById('batch-queue-cron-expr');
    const isCron = scheduleModeSelect && scheduleModeSelect.value === 'cron';
    if (cronGroup) {
        cronGroup.style.display = isCron ? 'block' : 'none';
    }
    if (cronExprInput) {
        if (isCron) {
            cronExprInput.setAttribute('required', 'required');
        } else {
            cronExprInput.removeAttribute('required');
            cronExprInput.value = '';
        }
    }
}

// Update new task statistics
function updateBatchImportStats(text) {
    const statsEl = document.getElementById('batch-import-stats');
    if (!statsEl) return;
    
    const lines = text.split('\n').filter(line => line.trim() !== '');
    const count = lines.length;
    
    if (count > 0) {
        statsEl.innerHTML = '<div class="batch-import-stat">' + _t('tasks.taskCount', { count: count }) + '</div>';
        statsEl.style.display = 'block';
    } else {
        statsEl.style.display = 'none';
    }
}

// Listen for batch task input
document.addEventListener('DOMContentLoaded', function() {
    const input = document.getElementById('batch-tasks-input');
    if (input) {
        input.addEventListener('input', function() {
            updateBatchImportStats(this.value);
        });
    }
});

// Create batch task queue
async function createBatchQueue() {
    if (typeof requirePermission === 'function' && !requirePermission('tasks:write')) return;
    const input = document.getElementById('batch-tasks-input');
    const titleInput = document.getElementById('batch-queue-title');
    const roleSelect = document.getElementById('batch-queue-role');
    const projectSelect = document.getElementById('batch-queue-project-id');
    const agentModeSelect = document.getElementById('batch-queue-agent-mode');
    const concurrencyInput = document.getElementById('batch-queue-concurrency');
    const scheduleModeSelect = document.getElementById('batch-queue-schedule-mode');
    const cronExprInput = document.getElementById('batch-queue-cron-expr');
    const executeNowCheckbox = document.getElementById('batch-queue-execute-now');
    if (!input) return;
    
    const text = input.value.trim();
    if (!text) {
        alert(_t('tasks.enterTaskPrompt'));
        return;
    }
    
    // Split task by line
    const tasks = text.split('\n').map(line => line.trim()).filter(line => line !== '');
    if (tasks.length === 0) {
        alert(_t('tasks.noValidTask'));
        return;
    }
    
    // Get title (optional)
    const title = titleInput ? titleInput.value.trim() : '';
    
    // Get role (optional, empty string means default role)
    const role = roleSelect ? roleSelect.value || '' : '';
    const projectId = projectSelect ? (projectSelect.value || '').trim() : '';
    const rawMode = agentModeSelect ? agentModeSelect.value : 'eino_single';
    const agentMode = isBatchQueueAgentMode(rawMode) ? rawMode : 'eino_single';
    const scheduleMode = scheduleModeSelect ? (scheduleModeSelect.value === 'cron' ? 'cron' : 'manual') : 'manual';
    const cronExpr = cronExprInput ? cronExprInput.value.trim() : '';
    const executeNow = executeNowCheckbox ? !!executeNowCheckbox.checked : false;
    let concurrency = concurrencyInput ? parseInt(concurrencyInput.value, 10) : 1;
    if (!Number.isFinite(concurrency) || concurrency < 1) concurrency = 1;
    if (concurrency > 8) concurrency = 8;
    if (scheduleMode === 'cron' && !cronExpr) {
        alert(_t('batchImportModal.cronExprRequired'));
        return;
    }
    if (scheduleMode === 'cron' && !/^\S+\s+\S+\s+\S+\s+\S+\s+\S+$/.test(cronExpr)) {
        alert(_t('batchImportModal.cronExprInvalid') || 'Cron expression format error, requires');
        return;
    }

    try {
        const response = await apiFetch('/api/batch-tasks', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                title,
                tasks,
                role,
                agentMode,
                hitlPolicy: document.getElementById('batch-queue-hitl-policy')?.value || '',
                scheduleMode,
                cronExpr,
                executeNow,
                projectId,
                concurrency,
            }),
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.createBatchQueueFailed'));
        }
        
        const result = await response.json();
        closeBatchImportModal();
        
        // Display queue details
        showBatchQueueDetail(result.queueId);
        
        // Refresh batch queue list
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to create batch task queue:', error);
        alert(_t('tasks.createBatchQueueFailed') + ': ' + error.message);
    }
}

// Get role icon (helper function)
function getRoleIconForDisplay(roleName, rolesList) {
    if (!roleName || roleName === '') {
        return '🔵'; // Default role icon
    }
    
    if (Array.isArray(rolesList) && rolesList.length > 0) {
        const role = rolesList.find(r => r.name === roleName);
        if (role && role.icon) {
            let icon = role.icon;
            // Check if it is a Unicode escape format (may contain quotes)
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // Conversion failed, using default icon
                    console.warn('Failed to convert icon Unicode escape:', icon, e);
                    return '👤';
                }
            }
            return icon;
        }
    }
    return '👤'; // Default icon
}

// Load batch task queue list
async function loadBatchQueues(page) {
    const section = document.getElementById('batch-queues-section');
    if (!section) return;
    
    // If page is specified, use it; otherwise use the current page
    if (page !== undefined) {
        batchQueuesState.currentPage = page;
    }
    
    // Load role list (for displaying the correct role icon)
    let loadedRoles = [];
    if (typeof loadRoles === 'function') {
        try {
            loadedRoles = await loadRoles();
        } catch (error) {
            console.warn('Failed to load role list, will use default icon:', error);
        }
    }
    batchQueuesState.loadedRoles = loadedRoles; // Save to state for rendering use
    
    // Build query parameters
    const params = new URLSearchParams();
    params.append('page', batchQueuesState.currentPage.toString());
    params.append('limit', batchQueuesState.pageSize.toString());
    if (batchQueuesState.filterStatus && batchQueuesState.filterStatus !== 'all') {
        params.append('status', batchQueuesState.filterStatus);
    }
    if (batchQueuesState.searchKeyword) {
        params.append('keyword', batchQueuesState.searchKeyword);
    }
    
    try {
        const response = await apiFetch(`/api/batch-tasks?${params.toString()}`);
        if (!response.ok) {
            throw new Error(_t('tasks.loadFailedRetry'));
        }
        
        const result = await response.json();
        batchQueuesState.queues = result.queues || [];
        batchQueuesState.total = result.total || 0;
        batchQueuesState.totalPages = result.total_pages || 1;
        renderBatchQueues();
    } catch (error) {
        console.error('Failed to load batch task queue:', error);
        section.style.display = 'block';
        const list = document.getElementById('batch-queues-list');
        if (list) {
            list.innerHTML = '<div class="tasks-empty"><p>' + _t('tasks.loadFailedRetry') + ': ' + escapeHtml(error.message) + '</p><button class="btn-secondary" onclick="refreshBatchQueues()">' + _t('tasks.retry') + '</button></div>';
        }
    }
}

const BATCH_QUEUES_FILTER_SELECT_IDS = ['batch-queues-status-filter'];
const batchQueuesFilterSelectMap = {};
let batchQueuesFilterSelectDocBound = false;

const TASKS_FILTER_SELECT_CARET = '<svg class="tasks-filter-select-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function closeAllBatchQueuesFilterSelects() {
    Object.keys(batchQueuesFilterSelectMap).forEach(function (id) {
        const reg = batchQueuesFilterSelectMap[id];
        if (!reg || !reg.wrapper) return;
        reg.wrapper.classList.remove('open');
        if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    });
}

function syncBatchQueuesFilterSelect(selectId) {
    const reg = batchQueuesFilterSelectMap[selectId];
    if (!reg) return;
    const select = reg.select;
    const dropdown = reg.dropdown;
    const trigger = reg.trigger;
    const valueSpan = trigger.querySelector('.tasks-filter-select-value');

    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'tasks-filter-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        } else {
            item.setAttribute('aria-selected', 'false');
        }
        const check = document.createElement('span');
        check.className = 'tasks-filter-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'tasks-filter-select-label';
        label.textContent = opt.textContent;
        item.appendChild(check);
        item.appendChild(label);
        dropdown.appendChild(item);
    });

    const selectedOpt = select.options[select.selectedIndex];
    if (valueSpan) {
        valueSpan.textContent = selectedOpt ? selectedOpt.textContent : '';
    }
    trigger.disabled = !!select.disabled;
    reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
}

function syncAllBatchQueuesFilterSelects() {
    BATCH_QUEUES_FILTER_SELECT_IDS.forEach(syncBatchQueuesFilterSelect);
}

function enhanceBatchQueuesFilterSelect(selectId) {
    const select = document.getElementById(selectId);
    if (!select) return;
    if (select.dataset.tasksCustomSelect === '1') {
        syncBatchQueuesFilterSelect(selectId);
        return;
    }
    select.dataset.tasksCustomSelect = '1';
    select.classList.add('tasks-filter-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'tasks-filter-select-ui';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'tasks-filter-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'tasks-filter-select-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', TASKS_FILTER_SELECT_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'tasks-filter-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    batchQueuesFilterSelectMap[selectId] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        const open = wrapper.classList.contains('open');
        closeAllBatchQueuesFilterSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
        }
    });

    dropdown.addEventListener('click', function (e) {
        const opt = e.target.closest('.tasks-filter-select-option');
        if (!opt) return;
        e.stopPropagation();
        const val = opt.getAttribute('data-value');
        if (val === null) return;
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        wrapper.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');
        syncBatchQueuesFilterSelect(selectId);
    });

    select.addEventListener('change', function () {
        syncBatchQueuesFilterSelect(selectId);
    });
}

function initBatchQueuesFilterSelects() {
    if (!batchQueuesFilterSelectDocBound) {
        document.addEventListener('click', closeAllBatchQueuesFilterSelects);
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllBatchQueuesFilterSelects();
        });
        batchQueuesFilterSelectDocBound = true;
    }
    BATCH_QUEUES_FILTER_SELECT_IDS.forEach(function (id) {
        enhanceBatchQueuesFilterSelect(id);
        const select = document.getElementById(id);
        if (select && !select.dataset.tasksFilterBound) {
            select.dataset.tasksFilterBound = '1';
            select.addEventListener('change', filterBatchQueues);
        }
    });
    syncAllBatchQueuesFilterSelects();
}

const BATCH_IMPORT_FORM_SELECT_IDS = [
    'batch-queue-role',
    'batch-queue-project-id',
    'batch-queue-agent-mode',
    'batch-queue-hitl-policy',
    'batch-queue-schedule-mode',
];
const batchFormSelectMap = {};
let batchFormSelectDocBound = false;
const BATCH_FORM_SELECT_CARET = '<svg class="batch-form-select-caret" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 9l6 6 6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function closeAllBatchFormSelects() {
    Object.keys(batchFormSelectMap).forEach(function (id) {
        const reg = batchFormSelectMap[id];
        if (!reg || !reg.wrapper) return;
        reg.wrapper.classList.remove('open');
        if (reg.trigger) reg.trigger.setAttribute('aria-expanded', 'false');
    });
}

// Keep the menu inside the modal scrollport without changing the modal's overflow.
function positionBatchFormDropdown(wrapper, trigger, dropdown) {
    const body = wrapper.closest('.modal-body');
    const bounds = body ? body.getBoundingClientRect() : { top: 0, bottom: window.innerHeight };
    const rect = trigger.getBoundingClientRect();
    const above = Math.max(0, rect.top - Math.max(0, bounds.top) - 8);
    const below = Math.max(0, Math.min(window.innerHeight, bounds.bottom) - rect.bottom - 8);
    const desired = Math.min(280, dropdown.scrollHeight + 2);
    const openAbove = below < desired && above > below;
    dropdown.style.top = openAbove ? 'auto' : 'calc(100% + 4px)';
    dropdown.style.bottom = openAbove ? 'calc(100% + 4px)' : 'auto';
    dropdown.style.maxHeight = Math.min(280, openAbove ? above : below) + 'px';
    dropdown.style.boxSizing = 'border-box';
}

function syncBatchFormSelect(selectId) {
    const reg = batchFormSelectMap[selectId];
    if (!reg) return;
    const select = reg.select;
    const dropdown = reg.dropdown;
    const trigger = reg.trigger;
    const valueSpan = trigger.querySelector('.batch-form-select-value');

    dropdown.innerHTML = '';
    Array.prototype.forEach.call(select.options, function (opt) {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'batch-form-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-value', opt.value);
        if (opt.value === select.value) {
            item.classList.add('is-selected');
            item.setAttribute('aria-selected', 'true');
        } else {
            item.setAttribute('aria-selected', 'false');
        }
        const check = document.createElement('span');
        check.className = 'batch-form-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'batch-form-select-label';
        label.textContent = opt.textContent;
        item.appendChild(check);
        item.appendChild(label);
        dropdown.appendChild(item);
    });

    const selectedOpt = select.options[select.selectedIndex];
    if (valueSpan) {
        valueSpan.textContent = selectedOpt ? selectedOpt.textContent : '';
    }
    trigger.disabled = !!select.disabled;
    reg.wrapper.classList.toggle('is-disabled', !!select.disabled);
}

function syncAllBatchImportFormSelects() {
    BATCH_IMPORT_FORM_SELECT_IDS.forEach(syncBatchFormSelect);
}

function isBatchFormSelectFocused(selectId) {
    const active = document.activeElement;
    if (!active) return false;
    if (active.id === selectId) return true;
    const reg = batchFormSelectMap[selectId];
    return !!(reg && reg.wrapper && reg.wrapper.contains(active));
}

function focusBatchFormSelect(selectId) {
    const reg = batchFormSelectMap[selectId];
    if (reg && reg.trigger) {
        reg.trigger.focus();
        return;
    }
    const select = document.getElementById(selectId);
    if (select) select.focus();
}

function bindBatchInlineEditFocusOut(container, saveFn, isCancelledFn) {
    if (!container) return;
    container.addEventListener('focusout', function () {
        setTimeout(function () {
            if (isCancelledFn && isCancelledFn()) return;
            if (container.contains(document.activeElement)) return;
            saveFn();
        }, 100);
    });
}

function enhanceBatchFormSelect(selectId, options) {
    options = options || {};
    const select = document.getElementById(selectId);
    if (!select) return;
    const existing = batchFormSelectMap[selectId];
    if (existing && existing.select !== select) {
        delete batchFormSelectMap[selectId];
    }
    if (select.dataset.batchFormCustom === '1') {
        syncBatchFormSelect(selectId);
        return;
    }
    select.dataset.batchFormCustom = '1';
    select.classList.add('batch-form-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'batch-form-select-ui' + (options.inline ? ' batch-form-select-ui--inline' : '');

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'batch-form-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    const valueSpan = document.createElement('span');
    valueSpan.className = 'batch-form-select-value';
    trigger.appendChild(valueSpan);
    trigger.insertAdjacentHTML('beforeend', BATCH_FORM_SELECT_CARET);

    const dropdown = document.createElement('div');
    dropdown.className = 'batch-form-select-dropdown';
    dropdown.setAttribute('role', 'listbox');

    const parent = select.parentNode;
    parent.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(dropdown);
    wrapper.appendChild(select);

    batchFormSelectMap[selectId] = { wrapper: wrapper, trigger: trigger, dropdown: dropdown, select: select };

    trigger.addEventListener('click', function (e) {
        e.stopPropagation();
        if (select.disabled) return;
        const open = wrapper.classList.contains('open');
        closeAllBatchFormSelects();
        if (!open) {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
            positionBatchFormDropdown(wrapper, trigger, dropdown);
        }
    });

    dropdown.addEventListener('click', function (e) {
        const opt = e.target.closest('.batch-form-select-option');
        if (!opt) return;
        e.stopPropagation();
        const val = opt.getAttribute('data-value');
        if (val === null) return;
        if (select.value !== val) {
            select.value = val;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        wrapper.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');
        syncBatchFormSelect(selectId);
    });

    select.addEventListener('change', function () {
        syncBatchFormSelect(selectId);
    });

    syncBatchFormSelect(selectId);
}

function refreshBatchFormSelect(selectId, options) {
    const select = document.getElementById(selectId);
    if (!select) {
        delete batchFormSelectMap[selectId];
        return;
    }
    enhanceBatchFormSelect(selectId, options);
}

function refreshBatchFormSelects() {
    Object.keys(batchFormSelectMap).forEach(function (id) {
        if (!document.getElementById(id)) delete batchFormSelectMap[id];
    });
    BATCH_IMPORT_FORM_SELECT_IDS.forEach(function (id) {
        enhanceBatchFormSelect(id, { inline: false });
    });
    if (!batchFormSelectDocBound) {
        batchFormSelectDocBound = true;
        document.addEventListener('click', function (e) {
            if (e.target.closest('.batch-form-select-ui')) return;
            closeAllBatchFormSelects();
        });
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') closeAllBatchFormSelects();
        });
    }
    syncAllBatchImportFormSelects();
}

function initBatchFormSelects() {
    refreshBatchFormSelects();
}

// Filter batch task queue
function filterBatchQueues() {
    const statusFilter = document.getElementById('batch-queues-status-filter');
    const searchInput = document.getElementById('batch-queues-search');
    
    if (statusFilter) {
        batchQueuesState.filterStatus = statusFilter.value;
    }
    if (searchInput) {
        batchQueuesState.searchKeyword = searchInput.value.trim();
    }
    
    // Reset to first page and reload
    batchQueuesState.currentPage = 1;
    loadBatchQueues(1);
}

// Render batch task queue list
function renderBatchQueues() {
    const section = document.getElementById('batch-queues-section');
    const list = document.getElementById('batch-queues-list');
    const pagination = document.getElementById('batch-queues-pagination');
    
    if (!section || !list) return;
    
    section.style.display = 'block';
    
    const queues = batchQueuesState.queues;
    
    if (queues.length === 0) {
        list.innerHTML = '<div class="tasks-empty"><p>' + _t('tasks.noBatchQueues') + '</p></div>';
        if (pagination) pagination.style.display = 'none';
        return;
    }
    
    // Ensure the pagination control is visible (reset any display: none that may have been set previously)
    if (pagination) {
        pagination.style.display = '';
    }
    
    list.innerHTML = queues.map(queue => {
        const pres = getBatchQueueStatusPresentation(queue);
        
        // Task status statistics
        const stats = {
            total: queue.tasks.length,
            pending: 0,
            running: 0,
            completed: 0,
            failed: 0,
            cancelled: 0
        };
        
        queue.tasks.forEach(task => {
            if (task.status === 'pending') stats.pending++;
            else if (task.status === 'running') stats.running++;
            else if (task.status === 'completed') stats.completed++;
            else if (task.status === 'failed') stats.failed++;
            else if (task.status === 'cancelled') stats.cancelled++;
        });
        
        const progress = stats.total > 0 ? Math.round((stats.completed + stats.failed + stats.cancelled) / stats.total * 100) : 0;
        // Allow deleting queues in pending, completed, or cancelled status
        const canDelete = queue.status === 'pending' || queue.status === 'completed' || queue.status === 'cancelled';
        // The action column always shows "View Vulnerabilities"; no longer uses --no-actions to hide the entire column (otherwise you can't jump to the vulnerability page from the running queue)
        const noActionsClass = '';
        
        const loadedRoles = batchQueuesState.loadedRoles || [];
        const roleIcon = getRoleIconForDisplay(queue.role, loadedRoles);
        const roleName = queue.role && queue.role !== '' ? queue.role : _t('batchQueueDetailModal.defaultRole');
        const isCronCycleIdle = queue.scheduleMode === 'cron' && queue.scheduleEnabled !== false && queue.status === 'completed';
        const cardMod = isCronCycleIdle ? ' batch-queue-item--cron-wait' : '';
        const progressFillMod = isCronCycleIdle ? ' batch-queue-progress-fill--cron-wait' : '';

        const agentLabel = batchQueueAgentModeLabel(queue.agentMode);
        let scheduleLabel = queue.scheduleMode === 'cron' ? _t('batchImportModal.scheduleModeCron') : _t('batchImportModal.scheduleModeManual');
        if (queue.scheduleMode === 'cron' && queue.cronExpr) {
            scheduleLabel += ` (${queue.cronExpr})`;
        }
        const configLine = [roleName, agentLabel, scheduleLabel].map(s => escapeHtml(s)).join(' · ');
        const cronPausedNote = queue.scheduleMode === 'cron' && queue.scheduleEnabled === false
            ? ` <span class="batch-queue-inline-warn" title="${escapeHtml(_t('batchQueueDetailModal.scheduleCronAutoHint'))}">(${escapeHtml(_t('batchQueueDetailModal.cronSchedulePausedBadge'))})</span>`
            : '';
        const shortId = queue.id.length > 14 ? escapeHtml(queue.id.slice(0, 12)) + '\u2026' : escapeHtml(queue.id);
        const titleBlock = queue.title
            ? `<h4 class="batch-queue-card-title">${escapeHtml(queue.title)}</h4>`
            : `<h4 class="batch-queue-card-title batch-queue-card-title--muted">${escapeHtml(_t('tasks.batchQueueUntitled'))}</h4>`;
        const doneCount = stats.completed + stats.failed + stats.cancelled;

        return `
            <div class="batch-queue-item batch-queue-item--compact${cardMod}${noActionsClass}" data-queue-id="${escapeAttr(queue.id)}" onclick="showBatchQueueDetail(${escapeJsStringAttr(queue.id)})">
                <div class="batch-queue-item__inner batch-queue-item__inner--grid">
                    <div class="batch-queue-item__lead">
                        <div class="batch-queue-item__title-row">
                            <span class="batch-queue-item__role-icon" aria-hidden="true">${escapeHtml(roleIcon)}</span>
                            <div class="batch-queue-item__titles">${titleBlock}</div>
                        </div>
                        <p class="batch-queue-item__config">${configLine}${cronPausedNote}</p>
                        <p class="batch-queue-item__idline batch-queue-item__idline--lead"><code title="${escapeAttr(queue.id)}">${shortId}</code><span class="batch-queue-item__idsep">\u00b7</span><span>${escapeHtml(_t('tasks.createdTimeLabel'))}\u00a0${escapeHtml(new Date(queue.createdAt).toLocaleString())}</span></p>
                    </div>
                    <div class="batch-queue-item__cluster">
                        <div class="batch-queue-item__status-inline">
                            <span class="batch-queue-status ${pres.class}">${escapeHtml(pres.text)}</span>
                            <span class="batch-queue-item__pct">${progress}%\u00a0<span class="batch-queue-item__pct-frac">(${doneCount}/${stats.total})</span></span>
                        </div>
                        ${pres.sublabel ? `<span class="batch-queue-item__sublabel">${escapeHtml(pres.sublabel)}</span>` : ''}
                    </div>
                    <div class="batch-queue-item__progress-col">
                        <div class="batch-queue-progress-bar batch-queue-progress-bar--card batch-queue-progress-bar--list batch-queue-progress-bar--card-row">
                            <div class="batch-queue-progress-fill${progressFillMod}" style="width: ${progress}%"></div>
                        </div>
                    </div>
                    <div class="batch-queue-item__actions-col" onclick="event.stopPropagation();">
                        <button type="button" class="batch-queue-icon-btn" onclick="navigateToVulnerabilitiesFromTasksPage('queue', ${escapeJsStringAttr(queue.id)})" title="${escapeHtml(_t('tasks.viewVulnerabilitiesQueueTitle'))}" aria-label="${escapeHtml(_t('tasks.viewVulnerabilitiesQueueTitle'))}"><svg class="batch-queue-icon-btn__svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg></button>
                        ${canDelete ? `<button type="button" class="batch-queue-icon-btn batch-queue-icon-btn--danger" onclick="deleteBatchQueueFromList(${escapeJsStringAttr(queue.id)})" title="${escapeHtml(_t('tasks.deleteQueue'))}" aria-label="${escapeHtml(_t('tasks.deleteQueue'))}"><svg class="batch-queue-icon-btn__svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M10 11v6"/><path d="M14 11v6"/></svg></button>` : ''}
                    </div>
                </div>
            </div>
        `;

    }).join('');
    
    // Render pagination control
    renderBatchQueuesPagination();
}

// Render batch task queue pagination control (structure and style aligned with MCP monitoring .monitor-pagination)
function renderBatchQueuesPagination() {
    const paginationContainer = document.getElementById('batch-queues-pagination');
    if (!paginationContainer) return;
    
    const { currentPage, pageSize, total, totalPages } = batchQueuesState;
    
    // Show pagination info even when there is only one page (consistent with MCP monitoring)
    if (total === 0) {
        paginationContainer.innerHTML = '';
        if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
        return;
    }
    
    // Calculate display range
    const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(currentPage * pageSize, total);
    
    let paginationHTML = '<div class="monitor-pagination">';
    
    // Left side: display range information and per-page quantity selector (refer to Skills style)
    paginationHTML += `
        <div class="pagination-info">
            <span>` + _t('tasks.paginationShow', { start: start, end: end, total: total }) + `</span>
            <label class="pagination-page-size">
                ` + _t('tasks.paginationPerPage') + `
                <select id="batch-queues-page-size-pagination" onchange="changeBatchQueuesPageSize()">
                    <option value="10" ${pageSize === 10 ? 'selected' : ''}>10</option>
                    <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                    <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                    <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                </select>
            </label>
        </div>
    `;
    
    // Right side: pagination buttons (following the Skills style: First Page, Previous Page, Page X/Y, Next Page, Last Page)
    paginationHTML += `
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="goBatchQueuesPage(1)" ${currentPage === 1 || total === 0 ? 'disabled' : ''}>` + _t('tasks.paginationFirst') + `</button>
            <button class="btn-secondary" onclick="goBatchQueuesPage(${currentPage - 1})" ${currentPage === 1 || total === 0 ? 'disabled' : ''}>` + _t('tasks.paginationPrev') + `</button>
            <span class="pagination-page">` + _t('tasks.paginationPage', { current: currentPage, total: totalPages || 1 }) + `</span>
            <button class="btn-secondary" onclick="goBatchQueuesPage(${currentPage + 1})" ${currentPage >= totalPages || total === 0 ? 'disabled' : ''}>` + _t('tasks.paginationNext') + `</button>
            <button class="btn-secondary" onclick="goBatchQueuesPage(${totalPages || 1})" ${currentPage >= totalPages || total === 0 ? 'disabled' : ''}>` + _t('tasks.paginationLast') + `</button>
        </div>
    `;
    
    paginationHTML += '</div>';
    
    paginationContainer.innerHTML = paginationHTML;
    if (typeof initPaginationSelects === 'function') initPaginationSelects(paginationContainer);
}

// Jump to specified page
function goBatchQueuesPage(page) {
    const { totalPages } = batchQueuesState;
    if (page < 1 || page > totalPages) return;
    
    loadBatchQueues(page);
    
    // Scroll to top of list
    const list = document.getElementById('batch-queues-list');
    if (list) {
        list.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
}

// Change the number of items displayed per page
function changeBatchQueuesPageSize() {
    const pageSizeSelect = document.getElementById('batch-queues-page-size-pagination');
    if (!pageSizeSelect) return;
    
    const newPageSize = parseInt(pageSizeSelect.value, 10);
    if (newPageSize && newPageSize > 0) {
        batchQueuesState.pageSize = newPageSize;
        batchQueuesState.currentPage = 1; // Reset to first page
        loadBatchQueues(1);
    }
}

// Show batch task queue details
async function showBatchQueueDetail(queueId) {
    const modal = document.getElementById('batch-queue-detail-modal');
    const title = document.getElementById('batch-queue-detail-title');
    const content = document.getElementById('batch-queue-detail-content');
        const startBtn = document.getElementById('batch-queue-start-btn');
        const cancelBtn = document.getElementById('batch-queue-cancel-btn');
        const deleteBtn = document.getElementById('batch-queue-delete-btn');
        const addTaskBtn = document.getElementById('batch-queue-add-task-btn');
        
        if (!modal || !content) return;

        const alreadyOpen = isAppModalOpen('batch-queue-detail-modal');
        if (!alreadyOpen) {
            if (content) content.innerHTML = '<p style="color:#64748b;margin:0;">…</p>';
            openAppModal('batch-queue-detail-modal', { focus: false });
        }

        try {
        // Load the role list (if not already loaded)
        let loadedRoles = [];
        if (typeof loadRoles === 'function') {
            try {
                loadedRoles = await loadRoles();
            } catch (error) {
                console.warn('Failed to load role list, will use default icon:', error);
            }
        }
        
        const response = await apiFetch(`/api/batch-tasks/${queueId}`);
        if (!response.ok) {
            throw new Error(_t('tasks.getQueueDetailFailed'));
        }
        
        const result = await response.json();
        const queue = result.queue;
        batchQueuesState.currentQueueId = queueId;
        const pres = getBatchQueueStatusPresentation(queue);
        const allowSubtaskMutation = batchQueueAllowsSubtaskMutation(queue);

        if (title) {
            // textContent does escaping by itself; don't escapeHtml here again, otherwise it would display && as &amp;... (which looks like "deformed/garbled text")
            title.textContent = queue.title ? _t('tasks.batchQueueTitle') + ' - ' + String(queue.title) : _t('tasks.batchQueueTitle');
        }
        
        // Update button display
        const pauseBtn = document.getElementById('batch-queue-pause-btn');
        if (addTaskBtn) {
            addTaskBtn.style.display = allowSubtaskMutation ? 'inline-block' : 'none';
        }
        if (startBtn) {
            // pending state shows "start executing", paused state shows "continue executing"
            startBtn.style.display = (queue.status === 'pending' || queue.status === 'paused') ? 'inline-block' : 'none';
            if (startBtn && queue.status === 'paused') {
                startBtn.textContent = _t('tasks.resumeExecute');
            } else if (startBtn && queue.status === 'pending') {
                const isCronPending = queue.scheduleMode === 'cron' && queue.scheduleEnabled !== false;
                startBtn.textContent = isCronPending
                    ? _t('batchQueueDetailModal.startExecuteNow')
                    : _t('batchQueueDetailModal.startExecute');
            }
        }
        const rerunBtn = document.getElementById('batch-queue-rerun-btn');
        if (rerunBtn) {
            // Completed or cancelled status shows "rerun"
            rerunBtn.style.display = (queue.status === 'completed' || queue.status === 'cancelled') ? 'inline-block' : 'none';
        }
        if (pauseBtn) {
            // running state displays "paused queue"
            pauseBtn.style.display = queue.status === 'running' ? 'inline-block' : 'none';
        }
        if (deleteBtn) {
            // Allow deleting queues in pending, completed, or cancelled status
            deleteBtn.style.display = (queue.status === 'pending' || queue.status === 'completed' || queue.status === 'cancelled' || queue.status === 'paused') ? 'inline-block' : 'none';
        }
        
        // Task status mapping
        const taskStatusMap = {
            'pending': { text: _t('tasks.statusPending'), class: 'batch-task-status-pending' },
            'running': { text: _t('tasks.statusRunning'), class: 'batch-task-status-running' },
            'completed': { text: _t('tasks.statusCompleted'), class: 'batch-task-status-completed' },
            'failed': { text: _t('tasks.failedLabel'), class: 'batch-task-status-failed' },
            'cancelled': { text: _t('tasks.statusCancelled'), class: 'batch-task-status-cancelled' }
        };
        
        let roleLineVal = '';
        if (queue.role && queue.role !== '') {
            let roleName = queue.role;
            let roleIcon = '\uD83D\uDC64';
            if (Array.isArray(loadedRoles) && loadedRoles.length > 0) {
                const role = loadedRoles.find(r => r.name === roleName);
                if (role && role.icon) {
                    let icon = role.icon;
                    const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
                    if (unicodeMatch) {
                        try {
                            const codePoint = parseInt(unicodeMatch[1], 16);
                            icon = String.fromCodePoint(codePoint);
                        } catch (e) {
                            // ignore
                        }
                    }
                    roleIcon = icon;
                }
            }
            roleLineVal = roleIcon + ' ' + escapeHtml(roleName);
        } else {
            roleLineVal = '\uD83D\uDD35 ' + escapeHtml(_t('batchQueueDetailModal.defaultRole'));
        }
        const agentModeText = batchQueueAgentModeLabel(queue.agentMode);
        const scheduleModeText = queue.scheduleMode === 'cron' ? _t('batchImportModal.scheduleModeCron') : _t('batchImportModal.scheduleModeManual');
        const scheduleDetail = escapeHtml(scheduleModeText) + (queue.scheduleMode === 'cron' && queue.cronExpr ? `（${escapeHtml(queue.cronExpr)}）` : '');
        const showProgressNoteInModal = !!(pres.progressNote && !pres.callout);

        
        // Save scroll position to prevent the scrollbar from jumping back to the top on refresh
        const modalBody = content.closest('.modal-body');
        const tasksList = content.querySelector('.batch-queue-tasks-list');
        const savedModalBodyScrollTop = modalBody ? modalBody.scrollTop : 0;
        const savedTasksListScrollTop = tasksList ? tasksList.scrollTop : 0;
        const prevTechDetails = content.querySelector('details.batch-queue-detail-tech');
        const prevLayout = content.querySelector('.batch-queue-detail-layout[data-bq-detail-for]');
        const prevDetailFor = prevLayout ? prevLayout.getAttribute('data-bq-detail-for') : null;
        const sameQueueAsBefore = prevDetailFor === queue.id;
        const savedTechDetailsOpen = sameQueueAsBefore && !!(prevTechDetails && prevTechDetails.open);

        deferModalContent(function () {
        content.innerHTML = `
            <div class="batch-queue-detail-layout" data-bq-detail-for="${escapeAttr(queue.id)}">
            <section class="batch-queue-detail-hero">
                <span class="batch-queue-status ${pres.class}">${escapeHtml(pres.text)}</span>
                ${pres.sublabel ? `<p class="batch-queue-detail-hero__sub">${escapeHtml(pres.sublabel)}</p>` : ''}
                ${showProgressNoteInModal ? `<p class="batch-queue-detail-hero__note">${escapeHtml(pres.progressNote)}</p>` : ''}
            </section>
            <section class="batch-queue-detail-kv">
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.queueTitle'))}</span><span class="bq-kv__v" id="bq-title-val">${allowSubtaskMutation ? `<span class="bq-inline-editable" onclick="startInlineEditTitle()" title="${escapeHtml(_t('common.edit'))}">${escapeHtml(queue.title || _t('tasks.batchQueueUntitled'))}</span>` : escapeHtml(queue.title || _t('tasks.batchQueueUntitled'))}</span></div>
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.role'))}</span><span class="bq-kv__v" id="bq-role-val">${allowSubtaskMutation ? `<span class="bq-inline-editable" onclick="startInlineEditRole()" title="${escapeHtml(_t('common.edit'))}">${roleLineVal}</span>` : roleLineVal}</span></div>
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchImportModal.agentMode'))}</span><span class="bq-kv__v" id="bq-agentmode-val">${allowSubtaskMutation ? `<span class="bq-inline-editable" onclick="startInlineEditAgentMode()" title="${escapeHtml(_t('common.edit'))}">${escapeHtml(agentModeText)}</span>` : escapeHtml(agentModeText)}</span></div>
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchImportModal.hitlPolicy'))}</span><span class="bq-kv__v" id="bq-hitl-val">${allowSubtaskMutation ? `<button type="button" class="btn-link" onclick="startInlineEditHITLPolicy()" title="${escapeAttr(_t('common.edit'))}">${escapeHtml(batchHITLPolicyLabel(queue.hitlPolicy))}</button>` : escapeHtml(batchHITLPolicyLabel(queue.hitlPolicy))}</span></div>
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchImportModal.scheduleMode'))}</span><span class="bq-kv__v" id="bq-schedule-val">${allowSubtaskMutation ? `<span class="bq-inline-editable" onclick="startInlineEditSchedule()" title="${escapeHtml(_t('common.edit'))}">${scheduleDetail}</span>` : scheduleDetail}</span></div>
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.concurrency'))}</span><span class="bq-kv__v" id="bq-concurrency-val">${allowSubtaskMutation ? `<span class="bq-inline-editable" onclick="startInlineEditConcurrency()" title="${escapeHtml(_t('common.edit'))}">${escapeHtml(String(queue.concurrency && queue.concurrency > 0 ? queue.concurrency : 1))}</span>` : escapeHtml(String(queue.concurrency && queue.concurrency > 0 ? queue.concurrency : 1))}</span></div>
                <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.taskTotal'))}</span><span class="bq-kv__v">${queue.tasks.length}</span></div>
                ${queue.scheduleMode === 'cron' ? `<div class="bq-kv bq-kv--block"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.scheduleCronAuto'))}</span><span class="bq-kv__v bq-kv__v--control"><label class="bq-cron-toggle"><input type="checkbox" ${queue.scheduleEnabled !== false ? 'checked' : ''} onchange="updateBatchQueueScheduleEnabled(this.checked)" /><span class="bq-cron-toggle__hint">${escapeHtml(_t('batchQueueDetailModal.scheduleCronAutoHint'))}</span></label></span></div>` : ''}
            </section>
            ${queue.lastScheduleError ? `<div class="bq-alert bq-alert--err"><strong>${escapeHtml(_t('batchQueueDetailModal.lastScheduleError'))}</strong><p>${escapeHtml(queue.lastScheduleError)}</p></div>` : ''}
            ${queue.lastRunError ? `<div class="bq-alert bq-alert--err"><strong>${escapeHtml(_t('batchQueueDetailModal.lastRunError'))}</strong><p>${escapeHtml(queue.lastRunError)}</p></div>` : ''}
            ${pres.callout ? `<div class="batch-queue-cron-callout batch-queue-cron-callout--compact"><span class="batch-queue-cron-callout-icon" aria-hidden="true">\u21BB</span><p>${escapeHtml(pres.callout)}</p></div>` : ''}
            <details class="batch-queue-detail-tech">
                <summary class="batch-queue-detail-tech__sum">${escapeHtml(_t('batchQueueDetailModal.technicalDetails'))}</summary>
                <div class="batch-queue-detail-tech__body">
                    <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.queueId'))}</span><span class="bq-kv__v"><code>${escapeHtml(queue.id)}</code></span></div>
                    <div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.createdAt'))}</span><span class="bq-kv__v">${escapeHtml(new Date(queue.createdAt).toLocaleString())}</span></div>
                    ${queue.startedAt ? `<div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.startedAt'))}</span><span class="bq-kv__v">${escapeHtml(new Date(queue.startedAt).toLocaleString())}</span></div>` : ''}
                    ${queue.completedAt ? `<div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.completedAt'))}</span><span class="bq-kv__v">${escapeHtml(new Date(queue.completedAt).toLocaleString())}</span></div>` : ''}
                    ${queue.scheduleMode === 'cron' && queue.nextRunAt && !pres.sublabel ? `<div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.nextRunAt'))}</span><span class="bq-kv__v">${escapeHtml(new Date(queue.nextRunAt).toLocaleString())}</span></div>` : ''}
                    ${queue.lastScheduleTriggerAt ? `<div class="bq-kv"><span class="bq-kv__k">${escapeHtml(_t('batchQueueDetailModal.lastScheduleTriggerAt'))}</span><span class="bq-kv__v">${escapeHtml(new Date(queue.lastScheduleTriggerAt).toLocaleString())}</span></div>` : ''}
                </div>
            </details>
            </div>
            <div class="batch-queue-tasks-list">
                <h4>` + _t('batchQueueDetailModal.taskList') + `</h4>
                ${queue.tasks.map((task, index) => {
                    const taskStatus = taskStatusMap[task.status] || { text: task.status, class: 'batch-task-status-unknown' };
                    const canEdit = allowSubtaskMutation && task.status !== 'running';
                    const canRunSingle = batchQueueCanRunSingleTask(queue, task);
                    const runSingleUnavailableTitle = escapeHtml(batchQueueRunSingleTaskDisabledReason(queue, task));
                    const taskMessageEscaped = escapeAttr(task.message).replace(/\n/g, "\\n");
                    return `
                        <div class="batch-task-item ${task.status === 'running' ? 'batch-task-item-active' : ''}" data-queue-id="${escapeAttr(queue.id)}" data-task-id="${escapeAttr(task.id)}" data-task-message="${taskMessageEscaped}">
                            <div class="batch-task-header">
                                <span class="batch-task-index">#${index + 1}</span>
                                <span class="batch-task-status ${taskStatus.class}">${taskStatus.text}</span>
                                <span class="batch-task-message" title="${escapeAttr(task.message)}">${escapeHtml(task.message)}</span>
                                <button class="btn-secondary btn-small batch-task-run-btn" ${canRunSingle ? `onclick="runSingleBatchTask(${escapeJsStringAttr(queue.id)}, ${escapeJsStringAttr(task.id)}); event.stopPropagation();"` : `disabled title="${runSingleUnavailableTitle}"`}>` + _t('tasks.runSingleTask') + `</button>
                                ${task.conversationId ? `<button class="btn-secondary btn-small" onclick="viewBatchTaskConversation(${escapeJsStringAttr(task.conversationId)}); event.stopPropagation();">` + _t('tasks.viewConversation') + `</button>` : ''}
                                ${canEdit ? `<button class="btn-secondary btn-small batch-task-edit-btn" onclick="editBatchTaskFromElement(this); event.stopPropagation();">` + _t('common.edit') + `</button>` : ''}
                                ${canEdit ? `<button class="btn-secondary btn-small btn-danger batch-task-delete-btn" onclick="deleteBatchTaskFromElement(this); event.stopPropagation();">` + _t('common.delete') + `</button>` : ''}
                            </div>
                            ${task.startedAt ? `<div class="batch-task-time">` + _t('batchQueueDetailModal.startLabel') + `: ${new Date(task.startedAt).toLocaleString()}</div>` : ''}
                            ${task.completedAt ? `<div class="batch-task-time">` + _t('batchQueueDetailModal.completeLabel') + `: ${new Date(task.completedAt).toLocaleString()}</div>` : ''}
                            ${task.error ? `<div class="batch-task-error">` + _t('batchQueueDetailModal.errorLabel') + `: ${escapeHtml(task.error)}</div>` : ''}
                            ${task.result ? `<div class="batch-task-result">` + _t('batchQueueDetailModal.resultLabel') + `: ${escapeHtml(task.result.substring(0, 200))}${task.result.length > 200 ? '...' : ''}</div>` : ''}
                        </div>
                    `;
                }).join('')}
            </div>
        `;
        
        // Restore scroll position
        if (savedModalBodyScrollTop > 0 && modalBody) {
            modalBody.scrollTop = savedModalBodyScrollTop;
        }
        const newTasksList = content.querySelector('.batch-queue-tasks-list');
        if (savedTasksListScrollTop > 0 && newTasksList) {
            newTasksList.scrollTop = savedTasksListScrollTop;
        }

        const newTechDetails = content.querySelector('details.batch-queue-detail-tech');
        if (newTechDetails && savedTechDetailsOpen) {
            newTechDetails.open = true;
        }
        });

        // Only poll for details periodically while running; other states should stop, to avoid innerHTML re-rendering resetting <details> and other UI back to their default state
        if (queue.status === 'running') {
            startBatchQueueRefresh(queueId);
        } else {
            stopBatchQueueRefresh();
        }
    } catch (error) {
        console.error('Failed to get queue details:', error);
        closeBatchQueueDetailModal();
        alert(_t('tasks.getQueueDetailFailed') + ': ' + error.message);
    }
}

// Start batch task queue
async function startBatchQueue() {
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    const btn = document.getElementById('batch-queue-start-btn');
    if (btn) { btn.disabled = true; }
    try {
        // Clicking "Start Execution" in the Cron queue will immediately run one round; a secondary confirmation here prevents accidental triggering
        const queueResponse = await apiFetch(`/api/batch-tasks/${queueId}`);
        if (!queueResponse.ok) {
            throw new Error(_t('tasks.getQueueDetailFailed'));
        }
        const queueResult = await queueResponse.json();
        const queue = queueResult && queueResult.queue ? queueResult.queue : null;
        const isCronPending = queue && queue.status === 'pending' && queue.scheduleMode === 'cron' && queue.scheduleEnabled !== false;
        if (isCronPending) {
            const okNow = confirm(_t('batchQueueDetailModal.startExecuteNowConfirm'));
            if (!okNow) return;
        }

        const response = await apiFetch(`/api/batch-tasks/${queueId}/start`, {
            method: 'POST',
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.startBatchQueueFailed'));
        }
        
        // Refresh details
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to start batch task:', error);
        alert(_t('tasks.startBatchQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// Pause batch task queue
async function pauseBatchQueue() {
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;

    if (!confirm(_t('tasks.pauseQueueConfirm'))) {
        return;
    }
    const btn = document.getElementById('batch-queue-pause-btn');
    if (btn) { btn.disabled = true; }
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/pause`, {
            method: 'POST',
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.pauseQueueFailed'));
        }
        
        // Refresh details
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to pause batch task:', error);
        alert(_t('tasks.pauseQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// Re-run batch task queue
async function rerunBatchQueue() {
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;

    if (!confirm(_t('tasks.rerunQueueConfirm'))) {
        return;
    }
    const btn = document.getElementById('batch-queue-rerun-btn');
    if (btn) { btn.disabled = true; }
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/rerun`, {
            method: 'POST',
        });

        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.rerunQueueFailed'));
        }

        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (error) {
        console.error('Re-run batch task failed:', error);
        alert(_t('tasks.rerunQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// Delete batch task queue (from details modal)
async function deleteBatchQueue() {
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;

    if (!confirm(_t('tasks.deleteQueueConfirm'))) {
        return;
    }
    const btn = document.getElementById('batch-queue-delete-btn');
    if (btn) { btn.disabled = true; }
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}`, {
            method: 'DELETE',
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.deleteQueueFailed'));
        }
        
        closeBatchQueueDetailModal();
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to delete batch task queue:', error);
        alert(_t('tasks.deleteQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// Delete batch task queue from list
async function deleteBatchQueueFromList(queueId) {
    if (!queueId) return;
    
    if (!confirm(_t('tasks.deleteQueueConfirm'))) {
        return;
    }
    
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}`, {
            method: 'DELETE',
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.deleteQueueFailed'));
        }
        
        // If currently viewing this queue's details, close the details modal
        if (batchQueuesState.currentQueueId === queueId) {
            closeBatchQueueDetailModal();
        }
        
        // Refresh queue list
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to delete batch task queue:', error);
        alert(_t('tasks.deleteQueueFailed') + ': ' + error.message);
    }
}

// Close batch task queue details modal
function closeBatchQueueDetailModal() {
    closeAppModal('batch-queue-detail-modal');
    batchQueuesState.currentQueueId = null;
    stopBatchQueueRefresh();
}

// Start batch queue refresh
function startBatchQueueRefresh(queueId) {
    if (batchQueuesState.refreshInterval) {
        clearInterval(batchQueuesState.refreshInterval);
    }

    batchQueuesState.refreshInterval = setInterval(() => {
        // If an inline edit or add task modal is open, skip this refresh to prevent losing edit content
        const addModal = document.getElementById('add-batch-task-modal');
        const content = document.getElementById('batch-queue-detail-content');
        const hasInlineEdit = content && (
            content.querySelector('.bq-inline-edit-controls') ||
            content.querySelector('.batch-task-inline-edit')
        );
        if ((addModal && isAppModalOpen('add-batch-task-modal')) || hasInlineEdit) {
            return;
        }
        if (batchQueuesState._bqDetailRefreshing) {
            return;
        }
        if (batchQueuesState.currentQueueId !== queueId) {
            stopBatchQueueRefresh();
            return;
        }
        batchQueuesState._bqDetailRefreshing = true;
        (async () => {
            try {
                await showBatchQueueDetail(queueId);
                await refreshBatchQueues();
            } catch (e) {
                console.warn('Batch queue scheduled refresh failed:', e);
            } finally {
                batchQueuesState._bqDetailRefreshing = false;
            }
        })();
    }, 3000); // Refresh every 3 seconds
}

// Stop batch queue refresh
function stopBatchQueueRefresh() {
    if (batchQueuesState.refreshInterval) {
        clearInterval(batchQueuesState.refreshInterval);
        batchQueuesState.refreshInterval = null;
    }
}

// Refresh batch task queue list
async function refreshBatchQueues() {
    await loadBatchQueues(batchQueuesState.currentPage);
}

// View batch task conversations
function viewBatchTaskConversation(conversationId) {
    if (!conversationId) return;
    
    // Close batch task details modal
    closeBatchQueueDetailModal();
    
    // Directly use URL hash navigation, let the router handle page switching and conversation loading
    // This is more reliable, because the router will ensure the page switch completes before loading the conversation
    window.location.hash = `chat?conversation=${conversationId}`;
}

// --- Inline editing: task message ---
// Get task information from element and start inline editing
function editBatchTaskFromElement(button) {
    const taskItem = button.closest('.batch-task-item');
    if (!taskItem) return;

    const queueId = taskItem.getAttribute('data-queue-id');
    const taskId = taskItem.getAttribute('data-task-id');
    const taskMessage = taskItem.getAttribute('data-task-message');
    if (!queueId || !taskId) return;

    // Decode HTML entities
    const decodedMessage = taskMessage
        .replace(/&#39;/g, "'")
        .replace(/&quot;/g, '"')
        .replace(/\\n/g, '\n');

    // Find the buttons in .batch-task-message and header
    const msgSpan = taskItem.querySelector('.batch-task-message');
    const header = taskItem.querySelector('.batch-task-header');
    if (!msgSpan || !header) return;

    // Hide edit/delete button
    header.querySelectorAll('.batch-task-edit-btn, .batch-task-delete-btn').forEach(b => b.style.display = 'none');

    // Replace message with inline edit area
    const editDiv = document.createElement('div');
    editDiv.className = 'batch-task-inline-edit';
    editDiv.innerHTML = `<textarea id="bq-task-edit-${escapeAttr(taskId)}">${escapeHtml(decodedMessage)}</textarea>`;
    msgSpan.style.display = 'none';
    msgSpan.parentNode.insertBefore(editDiv, msgSpan.nextSibling);

    const textarea = editDiv.querySelector('textarea');
    if (textarea) {
        let taskCancelled = false;
        textarea.focus();
        textarea.setSelectionRange(textarea.value.length, textarea.value.length);
        textarea.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                taskCancelled = true;
                cancelInlineTask();
            }
        });
        textarea.addEventListener('blur', () => {
            if (!taskCancelled) saveInlineTask(queueId, taskId);
        });
    }
}

function cancelInlineTask() {
    // Refresh the entire details to restore it
    const queueId = batchQueuesState.currentQueueId;
    if (queueId) showBatchQueueDetail(queueId);
}

async function saveInlineTask(queueId, taskId) {
    if (_bqInlineSaving) return;
    _bqInlineSaving = true;
    const textarea = document.getElementById(`bq-task-edit-${taskId}`);
    if (!textarea) { _bqInlineSaving = false; return; }

    const message = textarea.value.trim();
    if (!message) {
        _bqInlineSaving = false;
        alert(_t('tasks.taskMessageRequired'));
        return;
    }

    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/tasks/${taskId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message }),
        });

        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.updateTaskFailed'));
        }

        _bqInlineSaving = false;
        // Refresh queue details
        if (batchQueuesState.currentQueueId === queueId) {
            showBatchQueueDetail(queueId);
        }

        // Refresh queue list
        refreshBatchQueues();
    } catch (error) {
        _bqInlineSaving = false;
        console.error('Failed to save task:', error);
        alert(_t('tasks.saveTaskFailed') + ': ' + error.message);
    }
}

// Show add batch task modal
function showAddBatchTaskModal() {
    if (typeof requirePermission === 'function' && !requirePermission('tasks:write')) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) {
        alert(_t('tasks.queueInfoMissing'));
        return;
    }
    
    const modal = document.getElementById('add-batch-task-modal');
    const messageInput = document.getElementById('add-task-message');
    
    if (!modal || !messageInput) {
        console.error('Add task modal element does not exist');
        return;
    }
    
    messageInput.value = '';
    openAppModal('add-batch-task-modal', { focusEl: messageInput });
    
    // Clear old event listeners
    if (showAddBatchTaskModal._escHandler) {
        document.removeEventListener('keydown', showAddBatchTaskModal._escHandler);
    }
    if (showAddBatchTaskModal._saveHandler && messageInput) {
        messageInput.removeEventListener('keydown', showAddBatchTaskModal._saveHandler);
    }

    // Add ESC key listener
    showAddBatchTaskModal._escHandler = (e) => {
        if (e.key === 'Escape') {
            closeAddBatchTaskModal();
        }
    };
    document.addEventListener('keydown', showAddBatchTaskModal._escHandler);

    // Add Enter+Ctrl/Cmd save functionality
    showAddBatchTaskModal._saveHandler = (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            saveAddBatchTask();
        }
    };
    messageInput.addEventListener('keydown', showAddBatchTaskModal._saveHandler);
}

// Close the add batch task modal
function closeAddBatchTaskModal() {
    // Clean up event listeners
    if (showAddBatchTaskModal._escHandler) {
        document.removeEventListener('keydown', showAddBatchTaskModal._escHandler);
        showAddBatchTaskModal._escHandler = null;
    }
    if (showAddBatchTaskModal._saveHandler) {
        const messageInput = document.getElementById('add-task-message');
        if (messageInput) {
            messageInput.removeEventListener('keydown', showAddBatchTaskModal._saveHandler);
        }
        showAddBatchTaskModal._saveHandler = null;
    }
    const modal = document.getElementById('add-batch-task-modal');
    const messageInput = document.getElementById('add-task-message');
    closeAppModal('add-batch-task-modal');
    if (messageInput) {
        messageInput.value = '';
    }
}

// Save the added batch tasks
async function saveAddBatchTask() {
    const queueId = batchQueuesState.currentQueueId;
    const messageInput = document.getElementById('add-task-message');
    
    if (!queueId) {
        alert(_t('tasks.queueInfoMissing'));
        return;
    }
    
    if (!messageInput) {
        alert(_t('tasks.cannotGetTaskMessageInput'));
        return;
    }
    
    const message = messageInput.value.trim();
    if (!message) {
        alert(_t('tasks.taskMessageRequired'));
        return;
    }
    
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/tasks`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ message: message }),
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.addTaskFailed'));
        }
        
        // Close the add task modal dialog
        closeAddBatchTaskModal();
        
        // Refresh queue details
        if (batchQueuesState.currentQueueId === queueId) {
            showBatchQueueDetail(queueId);
        }
        
        // Refresh queue list
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to add task:', error);
        alert(_t('tasks.addTaskFailed') + ': ' + error.message);
    }
}

// Get task information from the element and delete the task
function deleteBatchTaskFromElement(button) {
    const taskItem = button.closest('.batch-task-item');
    if (!taskItem) {
        console.error('Unable to find task item element');
        return;
    }
    
    const queueId = taskItem.getAttribute('data-queue-id');
    const taskId = taskItem.getAttribute('data-task-id');
    const taskMessage = taskItem.getAttribute('data-task-message');
    
    if (!queueId || !taskId) {
        console.error('Task information is incomplete');
        return;
    }
    
    // Decode HTML entities to display the message
    const decodedMessage = taskMessage
        .replace(/&#39;/g, "'")
        .replace(/&quot;/g, '"')
        .replace(/\\n/g, '\n');
    
    // Truncate long message for confirmation dialog
    const displayMessage = decodedMessage.length > 50 
        ? decodedMessage.substring(0, 50) + '...' 
        : decodedMessage;
    
    if (!confirm(_t('tasks.confirmDeleteTask', { message: displayMessage }))) {
        return;
    }
    
    deleteBatchTask(queueId, taskId);
}

// Delete batch task
async function deleteBatchTask(queueId, taskId) {
    if (!queueId || !taskId) {
        alert(_t('tasks.taskIncomplete'));
        return;
    }
    
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/tasks/${taskId}`, {
            method: 'DELETE',
        });
        
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.deleteTaskFailed'));
        }
        
        // Refresh queue details
        if (batchQueuesState.currentQueueId === queueId) {
            showBatchQueueDetail(queueId);
        }
        
        // Refresh queue list
        refreshBatchQueues();
    } catch (error) {
        console.error('Failed to delete task:', error);
        alert(_t('tasks.deleteTaskFailed') + ': ' + error.message);
    }
}

async function updateBatchQueueScheduleEnabled(enabled) {
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/schedule-enabled`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ scheduleEnabled: enabled }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('batchQueueDetailModal.scheduleToggleFailed'));
        }
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        console.error(e);
        alert(_t('batchQueueDetailModal.scheduleToggleFailed') + ': ' + e.message);
        showBatchQueueDetail(queueId);
    }
}

// --- Inline editing: cancel all inline regions currently being edited ---
function cancelAllInlineEdits() {
    _bqInlineSaving = true; // prevent blur from triggering save
    const queueId = batchQueuesState.currentQueueId;
    if (queueId) showBatchQueueDetail(queueId);
    _bqInlineSaving = false;
}

// --- Inline edit: Title ---
let _bqInlineSaving = false;
function startInlineEditTitle() {
    const container = document.getElementById('bq-title-val');
    if (!container) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    const currentTitle = (container.querySelector('.bq-inline-editable') || container).textContent.trim();
    const untitledText = _t('tasks.batchQueueUntitled');
    const val = currentTitle === untitledText ? '' : currentTitle;
    container.innerHTML = `<span class="bq-inline-edit-controls">
        <input type="text" id="bq-edit-title" value="${escapeAttr(val)}" placeholder="${escapeAttr(_t('batchImportModal.queueTitleHint') || '')}" style="width:180px;" />
    </span>`;
    const inp = document.getElementById('bq-edit-title');
    if (inp) {
        inp.focus();
        inp.select();
        let cancelled = false;
        inp.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') { e.preventDefault(); inp.blur(); }
            if (e.key === 'Escape') { cancelled = true; cancelAllInlineEdits(); }
        });
        inp.addEventListener('blur', () => {
            if (cancelled) return;
            saveInlineTitle();
        });
    }
}
async function saveInlineTitle() {
    if (_bqInlineSaving) return;
    _bqInlineSaving = true;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) { _bqInlineSaving = false; return; }
    const inp = document.getElementById('bq-edit-title');
    const title = inp ? inp.value.trim() : '';
    try {
        // Get the current role (unchanged)
        const detailResp = await apiFetch(`/api/batch-tasks/${queueId}`);
        const detail = await detailResp.json();
        const role = detail.queue ? (detail.queue.role || '') : '';
        const response = await apiFetch(`/api/batch-tasks/${queueId}/metadata`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title, role }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.updateTaskFailed'));
        }
        _bqInlineSaving = false;
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        _bqInlineSaving = false;
        console.error(e);
        alert(e.message);
    }
}

// --- Inline editing: Role ---
function startInlineEditRole() {
    const container = document.getElementById('bq-role-val');
    if (!container) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    // Get the role name in the current detail — it cannot be obtained from layout's data, so we fetch it via the API
    apiFetch(`/api/batch-tasks/${queueId}`).then(r => r.json()).then(detail => {
        const queue = detail.queue;
        const currentRole = queue.role || '';
        const roles = (Array.isArray(batchQueuesState.loadedRoles) ? batchQueuesState.loadedRoles : []).filter(r => r.name !== 'Default' && r.enabled !== false).sort((a, b) => (a.name || '').localeCompare(b.name || '', 'zh-CN'));
        const currentInList = !currentRole || roles.some(r => r.name === currentRole);
        const orphanOpt = !currentInList ? `<option value="${escapeAttr(currentRole)}" selected>${escapeHtml(currentRole)} (${escapeHtml(_t('batchQueueDetailModal.roleNotFound') || 'Removed')})</option>` : '';
        const opts = roles.map(r => `<option value="${escapeAttr(r.name)}" ${r.name === currentRole ? 'selected' : ''}>${escapeHtml(r.name)}</option>`).join('');
        container.innerHTML = `<span class="bq-inline-edit-controls">
            <select id="bq-edit-role">
                <option value="">${escapeHtml(_t('batchImportModal.defaultRole'))}</option>
                ${orphanOpt}${opts}
            </select>
        </span>`;
        refreshBatchFormSelect('bq-edit-role', { inline: true });
        const sel = document.getElementById('bq-edit-role');
        const controls = container.querySelector('.bq-inline-edit-controls');
        const roleReg = batchFormSelectMap['bq-edit-role'];
        if (sel) {
            focusBatchFormSelect('bq-edit-role');
            let cancelled = false;
            const onEscape = (e) => {
                if (e.key === 'Escape') { cancelled = true; cancelAllInlineEdits(); }
            };
            sel.addEventListener('keydown', onEscape);
            if (roleReg && roleReg.trigger) roleReg.trigger.addEventListener('keydown', onEscape);
            sel.addEventListener('change', () => { if (!cancelled) saveInlineRole(); });
            bindBatchInlineEditFocusOut(controls, () => { if (!cancelled) saveInlineRole(); }, () => cancelled);
        }
    });
}
async function saveInlineRole() {
    if (_bqInlineSaving) return;
    _bqInlineSaving = true;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) { _bqInlineSaving = false; return; }
    const sel = document.getElementById('bq-edit-role');
    const role = sel ? sel.value.trim() : '';
    try {
        const detailResp = await apiFetch(`/api/batch-tasks/${queueId}`);
        const detail = await detailResp.json();
        const title = detail.queue ? (detail.queue.title || '') : '';
        const response = await apiFetch(`/api/batch-tasks/${queueId}/metadata`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title, role }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.updateTaskFailed'));
        }
        _bqInlineSaving = false;
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        _bqInlineSaving = false;
        console.error(e);
        alert(e.message);
    }
}

// --- Inline editing: proxy mode ---
function startInlineEditAgentMode() {
    const container = document.getElementById('bq-agentmode-val');
    if (!container) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    apiFetch(`/api/batch-tasks/${queueId}`).then(r => r.json()).then(detail => {
        const queue = detail.queue;
        let currentMode = (queue.agentMode || 'eino_single').toLowerCase();
        if (!isBatchQueueAgentMode(currentMode)) currentMode = 'eino_single';
        container.innerHTML = `<span class="bq-inline-edit-controls">
            <select id="bq-edit-agentmode">
                <option value="eino_single" ${currentMode === 'eino_single' ? 'selected' : ''}>${escapeHtml(_t('chat.agentModeEinoSingle'))}</option>
                <option value="deep" ${currentMode === 'deep' ? 'selected' : ''}>${escapeHtml(_t('chat.agentModeDeep'))}</option>
                <option value="plan_execute" ${currentMode === 'plan_execute' ? 'selected' : ''}>${escapeHtml(_t('chat.agentModePlanExecuteLabel'))}</option>
                <option value="supervisor" ${currentMode === 'supervisor' ? 'selected' : ''}>${escapeHtml(_t('chat.agentModeSupervisorLabel'))}</option>
            </select>
        </span>`;
        refreshBatchFormSelect('bq-edit-agentmode', { inline: true });
        const sel = document.getElementById('bq-edit-agentmode');
        const controls = container.querySelector('.bq-inline-edit-controls');
        const modeReg = batchFormSelectMap['bq-edit-agentmode'];
        if (sel) {
            focusBatchFormSelect('bq-edit-agentmode');
            let cancelled = false;
            const onEscape = (e) => {
                if (e.key === 'Escape') { cancelled = true; cancelAllInlineEdits(); }
            };
            sel.addEventListener('keydown', onEscape);
            if (modeReg && modeReg.trigger) modeReg.trigger.addEventListener('keydown', onEscape);
            sel.addEventListener('change', () => { if (!cancelled) saveInlineAgentMode(); });
            bindBatchInlineEditFocusOut(controls, () => { if (!cancelled) saveInlineAgentMode(); }, () => cancelled);
        }
    });
}
async function saveInlineAgentMode() {
    if (_bqInlineSaving) return;
    _bqInlineSaving = true;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) { _bqInlineSaving = false; return; }
    const sel = document.getElementById('bq-edit-agentmode');
    const raw = sel ? sel.value : 'eino_single';
    const agentMode = isBatchQueueAgentMode(raw) ? raw : 'eino_single';
    try {
        const detailResp = await apiFetch(`/api/batch-tasks/${queueId}`);
        const detail = await detailResp.json();
        const title = detail.queue ? (detail.queue.title || '') : '';
        const role = detail.queue ? (detail.queue.role || '') : '';
        const response = await apiFetch(`/api/batch-tasks/${queueId}/metadata`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title, role, agentMode }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.updateTaskFailed'));
        }
        _bqInlineSaving = false;
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        _bqInlineSaving = false;
        console.error(e);
        alert(e.message);
    }
}

function normalizeBatchQueueConcurrencyInput(raw) {
    let n = parseInt(raw, 10);
    if (!Number.isFinite(n) || n < 1) n = 1;
    if (n > 8) n = 8;
    return n;
}

// --- Inline editing: concurrency count ---
function startInlineEditConcurrency() {
    const container = document.getElementById('bq-concurrency-val');
    if (!container) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    apiFetch(`/api/batch-tasks/${queueId}`).then(r => r.json()).then(detail => {
        const queue = detail.queue || {};
        const current = normalizeBatchQueueConcurrencyInput(queue.concurrency || 1);
        container.innerHTML = `<span class="bq-inline-edit-controls">
            <input type="number" id="bq-edit-concurrency" min="1" max="8" value="${current}" style="width:72px;" />
        </span>`;
        const inp = document.getElementById('bq-edit-concurrency');
        if (!inp) return;
        inp.focus();
        inp.select();
        let cancelled = false;
        inp.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') { e.preventDefault(); inp.blur(); }
            if (e.key === 'Escape') { cancelled = true; cancelAllInlineEdits(); }
        });
        inp.addEventListener('blur', () => {
            if (!cancelled) saveInlineConcurrency();
        });
    });
}

async function saveInlineConcurrency() {
    if (_bqInlineSaving) return;
    _bqInlineSaving = true;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) { _bqInlineSaving = false; return; }
    const inp = document.getElementById('bq-edit-concurrency');
    const concurrency = inp && inp.value.trim() !== '' ? Number(inp.value) : NaN;
    if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 8) {
        _bqInlineSaving = false;
        alert(_t('tasks.concurrencyInvalid'));
        if (inp) inp.focus();
        return;
    }
    try {
        const detailResp = await apiFetch(`/api/batch-tasks/${queueId}`);
        const detail = await detailResp.json();
        const q = detail.queue || {};
        const response = await apiFetch(`/api/batch-tasks/${queueId}/metadata`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                title: q.title || '',
                role: q.role || '',
                agentMode: q.agentMode || 'eino_single',
                concurrency,
            }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('tasks.updateTaskFailed'));
        }
        _bqInlineSaving = false;
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        _bqInlineSaving = false;
        console.error(e);
        alert(e.message);
    }
}

// --- Single item execution ---
async function runSingleBatchTask(queueId, taskId) {
    if (!queueId || !taskId) return;
    if (!confirm(_t('tasks.confirmRunSingleTask'))) return;
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/tasks/${taskId}/run`, {
            method: 'POST',
        });
        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
            throw new Error(result.error || _t('tasks.runSingleTaskFailed'));
        }
        if (result.autoStarted === false && result.message) {
            alert(result.message);
        }
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        console.error('Single execution failed:', e);
        alert(e.message);
    }
}

// --- Inline edit: scheduling configuration ---
function startInlineEditSchedule() {
    const container = document.getElementById('bq-schedule-val');
    if (!container) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    apiFetch(`/api/batch-tasks/${queueId}`).then(r => r.json()).then(detail => {
        const queue = detail.queue;
        const isCron = queue.scheduleMode === 'cron';
        container.innerHTML = `<span class="bq-inline-edit-controls">
            <select id="bq-edit-schedule-mode">
                <option value="manual" ${!isCron ? 'selected' : ''}>${escapeHtml(_t('batchImportModal.scheduleModeManual'))}</option>
                <option value="cron" ${isCron ? 'selected' : ''}>${escapeHtml(_t('batchImportModal.scheduleModeCron'))}</option>
            </select>
            <input type="text" id="bq-edit-cron-expr" class="bq-edit-cron-expr" value="${escapeAttr(queue.cronExpr || '')}" placeholder="${escapeAttr(_t('batchImportModal.cronExprPlaceholder', { interpolation: { escapeValue: false } }))}" style="${!isCron ? 'display:none;' : ''}" />
        </span>`;
        refreshBatchFormSelect('bq-edit-schedule-mode', { inline: true });
        let schedCancelled = false;
        const sel = document.getElementById('bq-edit-schedule-mode');
        const cronInp = document.getElementById('bq-edit-cron-expr');
        const controls = container.querySelector('.bq-inline-edit-controls');
        const schedReg = batchFormSelectMap['bq-edit-schedule-mode'];
        const onSchedEscape = (e) => { if (e.key === 'Escape') { schedCancelled = true; cancelAllInlineEdits(); } };
        if (sel) {
            focusBatchFormSelect('bq-edit-schedule-mode');
            sel.addEventListener('keydown', onSchedEscape);
            if (schedReg && schedReg.trigger) schedReg.trigger.addEventListener('keydown', onSchedEscape);
            sel.addEventListener('change', () => {
                toggleInlineScheduleCron();
                if (sel.value !== 'cron' && !schedCancelled) saveInlineSchedule();
            });
        }
        if (cronInp) {
            cronInp.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') { e.preventDefault(); cronInp.blur(); }
                if (e.key === 'Escape') { schedCancelled = true; cancelAllInlineEdits(); }
            });
        }
        bindBatchInlineEditFocusOut(controls, () => {
            if (schedCancelled) return;
            saveInlineSchedule();
        }, () => schedCancelled);
    });
}
function toggleInlineScheduleCron() {
    const modeSelect = document.getElementById('bq-edit-schedule-mode');
    const cronInput = document.getElementById('bq-edit-cron-expr');
    if (modeSelect && cronInput) {
        cronInput.style.display = modeSelect.value === 'cron' ? '' : 'none';
        if (modeSelect.value === 'cron') cronInput.focus();
    }
}
async function saveInlineSchedule() {
    if (_bqInlineSaving) return;
    _bqInlineSaving = true;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) { _bqInlineSaving = false; return; }
    const modeSelect = document.getElementById('bq-edit-schedule-mode');
    const cronInput = document.getElementById('bq-edit-cron-expr');
    if (!modeSelect) { _bqInlineSaving = false; return; }
    const scheduleMode = modeSelect.value;
    const cronExpr = cronInput ? cronInput.value.trim() : '';
    if (scheduleMode === 'cron' && !cronExpr) {
        _bqInlineSaving = false;
        alert(_t('batchImportModal.cronExprRequired'));
        return;
    }
    if (scheduleMode === 'cron' && !/^\S+\s+\S+\s+\S+\s+\S+\s+\S+$/.test(cronExpr)) {
        _bqInlineSaving = false;
        alert(_t('batchImportModal.cronExprInvalid') || 'Cron expression format error, requires');
        return;
    }
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}/schedule`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ scheduleMode, cronExpr }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || _t('batchQueueDetailModal.editScheduleError'));
        }
        _bqInlineSaving = false;
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (e) {
        _bqInlineSaving = false;
        console.error(e);
        alert(_t('batchQueueDetailModal.editScheduleError') + ': ' + e.message);
    }
}

// exported function
window.showBatchImportModal = showBatchImportModal;
window.closeBatchImportModal = closeBatchImportModal;
window.createBatchQueue = createBatchQueue;
window.showBatchQueueDetail = showBatchQueueDetail;
window.startBatchQueue = startBatchQueue;
window.pauseBatchQueue = pauseBatchQueue;
window.rerunBatchQueue = rerunBatchQueue;
window.deleteBatchQueue = deleteBatchQueue;
window.closeBatchQueueDetailModal = closeBatchQueueDetailModal;
window.refreshBatchQueues = refreshBatchQueues;
window.viewBatchTaskConversation = viewBatchTaskConversation;
window.editBatchTaskFromElement = editBatchTaskFromElement;
window.cancelInlineTask = cancelInlineTask;
window.saveInlineTask = saveInlineTask;
window.filterBatchQueues = filterBatchQueues;
window.goBatchQueuesPage = goBatchQueuesPage;
window.changeBatchQueuesPageSize = changeBatchQueuesPageSize;
window.showAddBatchTaskModal = showAddBatchTaskModal;
window.closeAddBatchTaskModal = closeAddBatchTaskModal;
window.saveAddBatchTask = saveAddBatchTask;
window.deleteBatchTaskFromElement = deleteBatchTaskFromElement;
window.deleteBatchQueueFromList = deleteBatchQueueFromList;
window.handleBatchScheduleModeChange = handleBatchScheduleModeChange;
window.updateBatchQueueScheduleEnabled = updateBatchQueueScheduleEnabled;
window.cancelAllInlineEdits = cancelAllInlineEdits;
window.startInlineEditTitle = startInlineEditTitle;
window.saveInlineTitle = saveInlineTitle;
window.startInlineEditRole = startInlineEditRole;
window.saveInlineRole = saveInlineRole;
window.startInlineEditAgentMode = startInlineEditAgentMode;
window.saveInlineAgentMode = saveInlineAgentMode;
window.startInlineEditConcurrency = startInlineEditConcurrency;
window.saveInlineConcurrency = saveInlineConcurrency;
window.runSingleBatchTask = runSingleBatchTask;
window.startInlineEditSchedule = startInlineEditSchedule;
window.toggleInlineScheduleCron = toggleInlineScheduleCron;
window.saveInlineSchedule = saveInlineSchedule;

// After switching the language, text rendered by JS in the list/pagination/detail popups needs to be re-rendered in the current language (applyTranslations does not handle innerHTML content)
document.addEventListener('languagechange', function () {
    try {
        syncAllBatchQueuesFilterSelects();
        syncAllBatchImportFormSelects();
        const tasksPage = document.getElementById('page-tasks');
        if (!tasksPage || !tasksPage.classList.contains('active')) {
            return;
        }
        if (document.getElementById('batch-queues-list')) {
            renderBatchQueues();
        }
        const detailModal = document.getElementById('batch-queue-detail-modal');
        if (
            detailModal &&
            isAppModalOpen('batch-queue-detail-modal') &&
            batchQueuesState.currentQueueId
        ) {
            showBatchQueueDetail(batchQueuesState.currentQueueId);
        }
    } catch (e) {
        console.warn('languagechange tasks refresh failed', e);
    }
});

document.addEventListener('DOMContentLoaded', function () {
    initBatchQueuesFilterSelects();
    initBatchFormSelects();
});


const BATCH_HITL_POLICIES = {
    '': 'hitlInherit', off: 'hitlOff', human: 'hitlHuman',
    audit_agent: 'hitlAgent', review_edit: 'hitlReviewEdit'
};

function batchHITLPolicyLabel(policy) {
    return _t('batchImportModal.' + (BATCH_HITL_POLICIES[policy || ''] || 'hitlInherit'));
}

async function startInlineEditHITLPolicy() {
    if (typeof requirePermission === 'function' && !requirePermission('tasks:write')) return;
    const queueId = batchQueuesState.currentQueueId;
    const container = document.getElementById('bq-hitl-val');
    if (!queueId || !container) return;
    try {
        const response = await apiFetch(`/api/batch-tasks/${queueId}`);
        if (!response.ok) throw new Error(_t('tasks.loadTaskListFailed'));
        const { queue } = await response.json();
        if (batchQueuesState.currentQueueId !== queueId || !batchQueueAllowsSubtaskMutation(queue)) return;
        container.innerHTML = `<select id="bq-edit-hitl" aria-label="${escapeAttr(_t('batchImportModal.hitlPolicy'))}">${Object.keys(BATCH_HITL_POLICIES).map(policy => `<option value="${policy}" ${policy === (queue.hitlPolicy || '') ? 'selected' : ''}>${escapeHtml(batchHITLPolicyLabel(policy))}</option>`).join('')}</select>`;
        const select = document.getElementById('bq-edit-hitl');
        select.focus();
        select.addEventListener('keydown', e => {
            if (e.key === 'Escape') showBatchQueueDetail(queueId);
        });
        select.addEventListener('change', async () => {
            select.disabled = true;
            try {
                const result = await apiFetch(`/api/batch-tasks/${queueId}/metadata`, {
                    method: 'PUT', headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        title: queue.title || '', role: queue.role || '',
                        hitlPolicy: select.value
                    })
                });
                if (!result.ok) {
                    const error = await result.json().catch(() => ({}));
                    throw new Error(error.error || _t('tasks.updateTaskFailed'));
                }
                if (batchQueuesState.currentQueueId === queueId) showBatchQueueDetail(queueId);
                refreshBatchQueues();
            } catch (error) {
                select.disabled = false;
                alert(error.message);
            }
        });
    } catch (error) { alert(error.message); }
}
window.startInlineEditHITLPolicy = startInlineEditHITLPolicy;
