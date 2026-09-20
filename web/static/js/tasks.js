// 任务管理页面功能
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

/** 插值不转 HTML 实体（避免日期里的 / 变成 &#x2F; 再被 escapeHtml 成乱码） */
function _tPlain(key, opts) {
    if (typeof window.t !== 'function') return key;
    const base = opts && typeof opts === 'object' ? opts : {};
    const interp = base.interpolation && typeof base.interpolation === 'object' ? base.interpolation : {};
    return window.t(key, {
        ...base,
        interpolation: { escapeValue: false, ...interp }
    });
}

/** 与创建队列 / API 一致的合法 agentMode */
const BATCH_QUEUE_AGENT_MODES = ['eino_single', 'deep', 'plan_execute', 'supervisor'];

function isBatchQueueAgentMode(mode) {
    return BATCH_QUEUE_AGENT_MODES.indexOf(String(mode || '').toLowerCase()) >= 0;
}

/** 批量队列 agentMode 展示文案（与对话模式命名一致） */
function batchQueueAgentModeLabel(mode) {
    const m = String(mode || 'eino_single').toLowerCase();
    if (m === 'eino_single') return _t('chat.agentModeEinoSingle');
    if (m === 'deep') return _t('chat.agentModeDeep');
    if (m === 'plan_execute') return _t('chat.agentModePlanExecuteLabel');
    if (m === 'supervisor') return _t('chat.agentModeSupervisorLabel');
    return _t('chat.agentModeEinoSingle');
}

/** Cron 队列在「本轮 completed」等状态下的展示文案（底层 status 不变，仅 UI 强调循环调度） */
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

/** 队列是否处于「可改子任务列表/文案」的空闲态（与后端 batch_task_manager.queueAllowsTaskListMutationLocked 对齐） */
function batchQueueAllowsSubtaskMutation(queue) {
    if (!queue) return false;
    if (queue.status === 'running') return false;
    const hasRunningSubtask = Array.isArray(queue.tasks) && queue.tasks.some(t => t && t.status === 'running');
    if (hasRunningSubtask) return false;
    return queue.status === 'pending' || queue.status === 'paused' || queue.status === 'completed' || queue.status === 'cancelled';
}

/** 是否允许对指定子任务发起单条执行（与后端 queueAllowsSingleTaskRunLocked 对齐） */
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

// HTML转义函数（如果未定义）
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

// 任务管理状态
const tasksState = {
    allTasks: [],
    filteredTasks: [],
    selectedTasks: new Set(),
    autoRefresh: true,
    refreshInterval: null,
    durationUpdateInterval: null,
    completedTasksHistory: [], // 保存最近完成的任务历史
    showHistory: true // 是否显示历史记录
};

// 从localStorage加载已完成任务历史
function loadCompletedTasksHistory() {
    try {
        const saved = localStorage.getItem('tasks-completed-history');
        if (saved) {
            const history = JSON.parse(saved);
            // 只保留最近24小时内完成的任务
            const now = Date.now();
            const oneDayAgo = now - 24 * 60 * 60 * 1000;
            tasksState.completedTasksHistory = history.filter(task => {
                const completedTime = task.completedAt || task.startedAt;
                return completedTime && new Date(completedTime).getTime() > oneDayAgo;
            });
            // 保存清理后的历史
            saveCompletedTasksHistory();
        }
    } catch (error) {
        console.error('加载已完成任务历史失败:', error);
        tasksState.completedTasksHistory = [];
    }
}

// 保存已完成任务历史到localStorage
function saveCompletedTasksHistory() {
    try {
        localStorage.setItem('tasks-completed-history', JSON.stringify(tasksState.completedTasksHistory));
    } catch (error) {
        console.error('保存已完成任务历史失败:', error);
    }
}

// 更新已完成任务历史
function updateCompletedTasksHistory(currentTasks) {
    // 保存当前所有任务作为快照（用于下次比较）
    const currentTaskIds = new Set(currentTasks.map(t => t.conversationId));
    
    // 如果是首次加载，只需要保存当前任务快照
    if (tasksState.allTasks.length === 0) {
        return;
    }
    
    const previousTaskIds = new Set(tasksState.allTasks.map(t => t.conversationId));
    
    // 找出刚完成的任务（之前存在但现在不存在的）
    // 只要任务从列表中消失了，就认为它已完成
    const justCompleted = tasksState.allTasks.filter(task => {
        return previousTaskIds.has(task.conversationId) && !currentTaskIds.has(task.conversationId);
    });
    
    // 将刚完成的任务添加到历史中
    justCompleted.forEach(task => {
        // 检查是否已存在（避免重复添加）
        const exists = tasksState.completedTasksHistory.some(t => t.conversationId === task.conversationId);
        if (!exists) {
            // 如果任务状态不是最终状态，标记为completed
            const finalStatus = ['completed', 'failed', 'timeout', 'cancelled', 'cleanup_unconfirmed'].includes(task.status)
                ? task.status 
                : 'completed';
            
            tasksState.completedTasksHistory.push({
                conversationId: task.conversationId,
                message: task.title || task.message || '未命名任务',
                startedAt: task.startedAt,
                status: finalStatus,
                completedAt: new Date().toISOString()
            });
        }
    });
    
    // 限制历史记录数量（最多保留50条）
    if (tasksState.completedTasksHistory.length > 50) {
        tasksState.completedTasksHistory = tasksState.completedTasksHistory
            .sort((a, b) => new Date(b.completedAt || b.startedAt) - new Date(a.completedAt || a.startedAt))
            .slice(0, 50);
    }
    
    saveCompletedTasksHistory();
}

// 加载任务列表
async function loadTasks() {
    const listContainer = document.getElementById('tasks-list');
    if (!listContainer) return;
    
    listContainer.innerHTML = '<div class="loading-spinner">' + _t('tasks.loadingTasks') + '</div>';

    try {
        // 并行加载运行中的任务和已完成的任务历史
        const [activeResponse, completedResponse] = await Promise.allSettled([
            apiFetch('/api/agent-loop/tasks'),
            apiFetch('/api/agent-loop/tasks/completed').catch(() => null) // 如果API不存在，返回null
        ]);

        // 处理运行中的任务
        if (activeResponse.status === 'rejected' || !activeResponse.value || !activeResponse.value.ok) {
            throw new Error(_t('tasks.loadTaskListFailed'));
        }

        const activeResult = await activeResponse.value.json();
        const activeTasks = activeResult.tasks || [];
        
        // 加载已完成任务历史（如果API可用）
        let completedTasks = [];
        if (completedResponse.status === 'fulfilled' && completedResponse.value && completedResponse.value.ok) {
            try {
                const completedResult = await completedResponse.value.json();
                completedTasks = completedResult.tasks || [];
            } catch (e) {
                console.warn('解析已完成任务历史失败:', e);
            }
        }
        
        // 保存所有任务
        tasksState.allTasks = activeTasks;
        
        // 更新已完成任务历史（从后端API获取）
        if (completedTasks.length > 0) {
            // 合并后端历史记录和本地历史记录（去重）
            const backendTaskIds = new Set(completedTasks.map(t => t.conversationId));
            const localHistory = tasksState.completedTasksHistory.filter(t => 
                !backendTaskIds.has(t.conversationId)
            );
            
            // 后端的历史记录优先，然后添加本地独有的
            tasksState.completedTasksHistory = [
                ...completedTasks.map(t => ({
                    conversationId: t.conversationId,
                    message: t.message || '未命名任务',
                    startedAt: t.startedAt,
                    status: t.status || 'completed',
                    completedAt: t.completedAt || new Date().toISOString()
                })),
                ...localHistory
            ];
            
            // 限制历史记录数量
            if (tasksState.completedTasksHistory.length > 50) {
                tasksState.completedTasksHistory = tasksState.completedTasksHistory
                    .sort((a, b) => new Date(b.completedAt || b.startedAt) - new Date(a.completedAt || a.startedAt))
                    .slice(0, 50);
            }
            
            saveCompletedTasksHistory();
        } else {
            // 如果后端API不可用，仍然使用前端逻辑更新历史
            updateCompletedTasksHistory(activeTasks);
        }
        
        updateTaskStats(activeTasks);
        filterAndSortTasks();
        startDurationUpdates();
    } catch (error) {
        console.error('加载任务失败:', error);
        listContainer.innerHTML = `
            <div class="tasks-empty">
                <p>${_t('tasks.loadFailedRetry')}: ${escapeHtml(error.message)}</p>
                <button class="btn-secondary" onclick="loadTasks()">${_t('tasks.retry')}</button>
            </div>
        `;
    }
}

// 更新任务统计
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

// 筛选任务
function filterTasks() {
    filterAndSortTasks();
}

// 排序任务
function sortTasks() {
    filterAndSortTasks();
}

// 筛选和排序任务
function filterAndSortTasks() {
    const statusFilter = document.getElementById('tasks-status-filter')?.value || 'all';
    const sortBy = document.getElementById('tasks-sort-by')?.value || 'time-desc';
    
    // 合并当前任务和历史任务
    let allTasks = [...tasksState.allTasks];
    
    // 如果显示历史记录，添加历史任务
    if (tasksState.showHistory) {
        const historyTasks = tasksState.completedTasksHistory
            .filter(ht => !tasksState.allTasks.some(t => t.conversationId === ht.conversationId))
            .map(ht => ({ ...ht, isHistory: true }));
        allTasks = [...allTasks, ...historyTasks];
    }
    
    // 筛选
    let filtered = allTasks;
    if (statusFilter === 'active') {
        // 仅运行中的任务（不包括历史）
        filtered = tasksState.allTasks.filter(task => 
            ['running', 'cancelling', 'cleaning', 'cleanup_failed'].includes(task.status)
        );
    } else if (statusFilter === 'history') {
        // 仅历史记录
        filtered = allTasks.filter(task => task.isHistory);
    } else if (statusFilter !== 'all') {
        filtered = allTasks.filter(task => task.status === statusFilter);
    }
    
    // 排序
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

// 切换显示历史记录
function toggleShowHistory(show) {
    tasksState.showHistory = show;
    localStorage.setItem('tasks-show-history', show ? 'true' : 'false');
    filterAndSortTasks();
}

// 计算执行时长
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

// 开始时长更新
function startDurationUpdates() {
    // 清除旧的定时器
    if (tasksState.durationUpdateInterval) {
        clearInterval(tasksState.durationUpdateInterval);
    }
    
    // 每秒更新一次执行时长
    tasksState.durationUpdateInterval = setInterval(() => {
        updateTaskDurations();
    }, 1000);
}

// 更新任务执行时长显示
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

// 渲染任务列表
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

    // 状态映射
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

    // 分离当前任务和历史任务
    const activeTasks = tasks.filter(t => !t.isHistory);
    const historyTasks = tasks.filter(t => t.isHistory);

    let html = '';
    
    // 渲染当前任务
    if (activeTasks.length > 0) {
        html += activeTasks.map(task => renderTaskItem(task, statusMap)).join('');
    }
    
    // 渲染历史任务
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

// 渲染单个任务项
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

// 清空任务历史
function clearTasksHistory() {
    if (!confirm(_t('tasks.clearHistoryConfirm'))) {
        return;
    }
    tasksState.completedTasksHistory = [];
    saveCompletedTasksHistory();
    filterAndSortTasks();
}

// 切换任务选择
function toggleTaskSelection(conversationId, selected) {
    if (selected) {
        tasksState.selectedTasks.add(conversationId);
    } else {
        tasksState.selectedTasks.delete(conversationId);
    }
    updateBatchActions();
}

// 更新批量操作UI
function updateBatchActions() {
    const batchActions = document.getElementById('tasks-batch-actions');
    const selectedCount = document.getElementById('tasks-selected-count');
    
    if (!batchActions || !selectedCount) return;
    
    const count = tasksState.selectedTasks.size;
    if (count > 0) {
        batchActions.style.display = 'flex';
        selectedCount.textContent = typeof window.t === 'function' ? window.t('mcp.selectedCount', { count: count }) : `已选择 ${count} 项`;
    } else {
        batchActions.style.display = 'none';
    }
}

// 清除任务选择
function clearTaskSelection() {
    tasksState.selectedTasks.clear();
    updateBatchActions();
    // 重新渲染以更新复选框状态
    filterAndSortTasks();
}

// 批量取消任务
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
            console.error('取消任务失败:', conversationId, error);
            failCount++;
        }
    }
    
    // 清除选择
    clearTaskSelection();
    
    // 刷新任务列表
    await loadTasks();
    
    // 显示结果
    if (failCount > 0) {
        alert(_t('tasks.batchCancelResultPartial', { success: successCount, fail: failCount }));
    } else {
        alert(_t('tasks.batchCancelResultSuccess', { n: successCount }));
    }
}

// 复制任务ID
function copyTaskId(conversationId) {
    navigator.clipboard.writeText(conversationId).then(() => {
        // 显示复制成功提示
        const tooltip = document.createElement('div');
        tooltip.textContent = _t('tasks.copiedToast');
        tooltip.style.cssText = 'position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: rgba(0,0,0,0.8); color: white; padding: 8px 16px; border-radius: 4px; z-index: 10000;';
        document.body.appendChild(tooltip);
        setTimeout(() => tooltip.remove(), 1000);
    }).catch(err => {
        console.error('复制失败:', err);
    });
}

// 取消任务
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

        // 从选择中移除
        tasksState.selectedTasks.delete(conversationId);
        updateBatchActions();
        
        // 重新加载任务列表
        await loadTasks();
    } catch (error) {
        console.error('取消任务失败:', error);
        alert(_t('tasks.cancelTaskFailed') + ': ' + error.message);
        button.disabled = false;
        button.textContent = originalText;
    }
}

// 查看对话
function viewConversation(conversationId) {
    if (!conversationId) return;
    
    // 切换到对话页面
    if (typeof switchPage === 'function') {
        switchPage('chat');
        // 加载并选中该对话 - 使用全局函数
        setTimeout(() => {
            // 尝试多种方式加载对话
            if (typeof loadConversation === 'function') {
                loadConversation(conversationId);
            } else if (typeof window.loadConversation === 'function') {
                window.loadConversation(conversationId);
            } else {
                // 如果函数不存在，尝试通过URL跳转
                window.location.hash = `chat?conversation=${conversationId}`;
                console.log('切换到对话页面，对话ID:', conversationId);
            }
        }, 500);
    }
}

// 跳转漏洞管理并按对话 ID 或批量队列 ID 筛选（队列 ID 走 task_id，与列表筛选项一致）
function navigateToVulnerabilitiesFromTasksPage(kind, id) {
    if (!id) return;
    const enc = encodeURIComponent(id);
    if (kind === 'queue') {
        window.location.hash = 'vulnerabilities?task_id=' + enc;
    } else if (kind === 'conversation') {
        window.location.hash = 'vulnerabilities?conversation_id=' + enc;
    }
}

// 刷新任务列表
async function refreshTasks() {
    await loadTasks();
}

// 切换自动刷新
function toggleTasksAutoRefresh(enabled) {
    tasksState.autoRefresh = enabled;
    
    // 保存到localStorage
    localStorage.setItem('tasks-auto-refresh', enabled ? 'true' : 'false');
    
    if (enabled) {
        // 启动自动刷新
        if (!tasksState.refreshInterval) {
            tasksState.refreshInterval = setInterval(() => {
                loadBatchQueues();
            }, 5000);
        }
    } else {
        // 停止自动刷新
        if (tasksState.refreshInterval) {
            clearInterval(tasksState.refreshInterval);
            tasksState.refreshInterval = null;
        }
    }
}

// 初始化任务管理页面
function initTasksPage() {
    initBatchQueuesFilterSelects();
    initBatchFormSelects();
    // 恢复自动刷新设置
    const autoRefreshCheckbox = document.getElementById('tasks-auto-refresh');
    if (autoRefreshCheckbox) {
        const saved = localStorage.getItem('tasks-auto-refresh');
        const enabled = saved !== null ? saved === 'true' : true;
        autoRefreshCheckbox.checked = enabled;
        toggleTasksAutoRefresh(enabled);
    } else {
        toggleTasksAutoRefresh(true);
    }
    
    // 只加载批量任务队列
    loadBatchQueues();
}

// 清理定时器（页面切换时调用）
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

// 导出函数供全局使用
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

// ==================== 批量任务功能 ====================

// 批量任务状态
const batchQueuesState = {
    queues: [],
    currentQueueId: null,
    refreshInterval: null,
    // 筛选和分页状态
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
        console.warn('加载项目列表失败:', error);
    }
}

// 显示新建任务模态框
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
        // 重置角色选择为默认
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
        
        // 加载并填充角色列表
        if (roleSelect && typeof loadRoles === 'function') {
            try {
                const loadedRoles = await loadRoles();
                // 清空现有选项（除了默认选项）
                roleSelect.innerHTML = '<option value="">' + _t('batchImportModal.defaultRole') + '</option>';
                
                // 添加已启用的角色
                const sortedRoles = loadedRoles.sort((a, b) => {
                    if (a.name === '默认') return -1;
                    if (b.name === '默认') return 1;
                    return (a.name || '').localeCompare(b.name || '', 'zh-CN');
                });
                
                sortedRoles.forEach(role => {
                    if (role.name !== '默认' && role.enabled !== false) {
                        const option = document.createElement('option');
                        option.value = role.name;
                        option.textContent = role.name;
                        roleSelect.appendChild(option);
                    }
                });
            } catch (error) {
                console.error('加载角色列表失败:', error);
            }
        }
        await refreshBatchProjectSelectOptions();
        refreshBatchFormSelects();

        openAppModal('batch-import-modal', { focusEl: input });
    }
}

// 关闭新建任务模态框
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

// 更新新建任务统计
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

// 监听批量任务输入
document.addEventListener('DOMContentLoaded', function() {
    const input = document.getElementById('batch-tasks-input');
    if (input) {
        input.addEventListener('input', function() {
            updateBatchImportStats(this.value);
        });
    }
});

// 创建批量任务队列
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
    
    // 按行分割任务
    const tasks = text.split('\n').map(line => line.trim()).filter(line => line !== '');
    if (tasks.length === 0) {
        alert(_t('tasks.noValidTask'));
        return;
    }
    
    // 获取标题（可选）
    const title = titleInput ? titleInput.value.trim() : '';
    
    // 获取角色（可选，空字符串表示默认角色）
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
        alert(_t('batchImportModal.cronExprInvalid') || 'Cron 表达式格式错误，需要 5 段（分 时 日 月 周）');
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
        
        // 显示队列详情
        showBatchQueueDetail(result.queueId);
        
        // 刷新批量队列列表
        refreshBatchQueues();
    } catch (error) {
        console.error('创建批量任务队列失败:', error);
        alert(_t('tasks.createBatchQueueFailed') + ': ' + error.message);
    }
}

// 获取角色图标（辅助函数）
function getRoleIconForDisplay(roleName, rolesList) {
    if (!roleName || roleName === '') {
        return '🔵'; // 默认角色图标
    }
    
    if (Array.isArray(rolesList) && rolesList.length > 0) {
        const role = rolesList.find(r => r.name === roleName);
        if (role && role.icon) {
            let icon = role.icon;
            // 检查是否是 Unicode 转义格式（可能包含引号）
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // 转换失败，使用默认图标
                    console.warn('转换 icon Unicode 转义失败:', icon, e);
                    return '👤';
                }
            }
            return icon;
        }
    }
    return '👤'; // 默认图标
}

// 加载批量任务队列列表
async function loadBatchQueues(page) {
    const section = document.getElementById('batch-queues-section');
    if (!section) return;
    
    // 如果指定了page，使用它；否则使用当前页
    if (page !== undefined) {
        batchQueuesState.currentPage = page;
    }
    
    // 加载角色列表（用于显示正确的角色图标）
    let loadedRoles = [];
    if (typeof loadRoles === 'function') {
        try {
            loadedRoles = await loadRoles();
        } catch (error) {
            console.warn('加载角色列表失败，将使用默认图标:', error);
        }
    }
    batchQueuesState.loadedRoles = loadedRoles; // 保存到状态中供渲染使用
    
    // 构建查询参数
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
        console.error('加载批量任务队列失败:', error);
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

// 筛选批量任务队列
function filterBatchQueues() {
    const statusFilter = document.getElementById('batch-queues-status-filter');
    const searchInput = document.getElementById('batch-queues-search');
    
    if (statusFilter) {
        batchQueuesState.filterStatus = statusFilter.value;
    }
    if (searchInput) {
        batchQueuesState.searchKeyword = searchInput.value.trim();
    }
    
    // 重置到第一页并重新加载
    batchQueuesState.currentPage = 1;
    loadBatchQueues(1);
}

// 渲染批量任务队列列表
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
    
    // 确保分页控件可见（重置之前可能设置的 display: none）
    if (pagination) {
        pagination.style.display = '';
    }
    
    list.innerHTML = queues.map(queue => {
        const pres = getBatchQueueStatusPresentation(queue);
        
        // 统计任务状态
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
        // 允许删除待执行、已完成或已取消状态的队列
        const canDelete = queue.status === 'pending' || queue.status === 'completed' || queue.status === 'cancelled';
        // 操作列常驻「查看漏洞」，不再使用 --no-actions 隐藏整列（否则无法从运行中队列跳转漏洞页）
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
    
    // 渲染分页控件
    renderBatchQueuesPagination();
}

// 渲染批量任务队列分页控件（结构与样式对齐 MCP 监控 .monitor-pagination）
function renderBatchQueuesPagination() {
    const paginationContainer = document.getElementById('batch-queues-pagination');
    if (!paginationContainer) return;
    
    const { currentPage, pageSize, total, totalPages } = batchQueuesState;
    
    // 即使只有一页也显示分页信息（与 MCP 监控一致）
    if (total === 0) {
        paginationContainer.innerHTML = '';
        return;
    }
    
    // 计算显示范围
    const start = total === 0 ? 0 : (currentPage - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(currentPage * pageSize, total);
    
    let paginationHTML = '<div class="monitor-pagination">';
    
    // 左侧：显示范围信息和每页数量选择器（参考Skills样式）
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
    
    // 右侧：分页按钮（参考Skills样式：首页、上一页、第X/Y页、下一页、末页）
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
}

// 跳转到指定页面
function goBatchQueuesPage(page) {
    const { totalPages } = batchQueuesState;
    if (page < 1 || page > totalPages) return;
    
    loadBatchQueues(page);
    
    // 滚动到列表顶部
    const list = document.getElementById('batch-queues-list');
    if (list) {
        list.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
}

// 改变每页显示数量
function changeBatchQueuesPageSize() {
    const pageSizeSelect = document.getElementById('batch-queues-page-size-pagination');
    if (!pageSizeSelect) return;
    
    const newPageSize = parseInt(pageSizeSelect.value, 10);
    if (newPageSize && newPageSize > 0) {
        batchQueuesState.pageSize = newPageSize;
        batchQueuesState.currentPage = 1; // 重置到第一页
        loadBatchQueues(1);
    }
}

// 显示批量任务队列详情
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
        // 加载角色列表（如果还未加载）
        let loadedRoles = [];
        if (typeof loadRoles === 'function') {
            try {
                loadedRoles = await loadRoles();
            } catch (error) {
                console.warn('加载角色列表失败，将使用默认图标:', error);
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
            // textContent 本身会做转义；这里不要再 escapeHtml，否则会把 && 显示成 &amp;...（看起来像“变形/乱码”）
            title.textContent = queue.title ? _t('tasks.batchQueueTitle') + ' - ' + String(queue.title) : _t('tasks.batchQueueTitle');
        }
        
        // 更新按钮显示
        const pauseBtn = document.getElementById('batch-queue-pause-btn');
        if (addTaskBtn) {
            addTaskBtn.style.display = allowSubtaskMutation ? 'inline-block' : 'none';
        }
        if (startBtn) {
            // pending状态显示"开始执行"，paused状态显示"继续执行"
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
            // 已完成或已取消状态显示"重跑一轮"
            rerunBtn.style.display = (queue.status === 'completed' || queue.status === 'cancelled') ? 'inline-block' : 'none';
        }
        if (pauseBtn) {
            // running状态显示"暂停队列"
            pauseBtn.style.display = queue.status === 'running' ? 'inline-block' : 'none';
        }
        if (deleteBtn) {
            // 允许删除待执行、已完成或已取消状态的队列
            deleteBtn.style.display = (queue.status === 'pending' || queue.status === 'completed' || queue.status === 'cancelled' || queue.status === 'paused') ? 'inline-block' : 'none';
        }
        
        // 任务状态映射
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

        
        // 保存滚动位置，防止刷新时滚动条弹回顶部
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
        
        // 恢复滚动位置
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

        // 仅运行中定时拉取详情；其它状态应停止，避免 innerHTML 重绘把 <details> 等 UI 打回默认态
        if (queue.status === 'running') {
            startBatchQueueRefresh(queueId);
        } else {
            stopBatchQueueRefresh();
        }
    } catch (error) {
        console.error('获取队列详情失败:', error);
        closeBatchQueueDetailModal();
        alert(_t('tasks.getQueueDetailFailed') + ': ' + error.message);
    }
}

// 开始批量任务队列
async function startBatchQueue() {
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    const btn = document.getElementById('batch-queue-start-btn');
    if (btn) { btn.disabled = true; }
    try {
        // Cron 队列点击“开始执行”会立即运行一轮，这里二次确认避免误触
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
        
        // 刷新详情
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (error) {
        console.error('启动批量任务失败:', error);
        alert(_t('tasks.startBatchQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// 暂停批量任务队列
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
        
        // 刷新详情
        showBatchQueueDetail(queueId);
        refreshBatchQueues();
    } catch (error) {
        console.error('暂停批量任务失败:', error);
        alert(_t('tasks.pauseQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// 重跑批量任务队列
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
        console.error('重跑批量任务失败:', error);
        alert(_t('tasks.rerunQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// 删除批量任务队列（从详情模态框）
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
        console.error('删除批量任务队列失败:', error);
        alert(_t('tasks.deleteQueueFailed') + ': ' + error.message);
    } finally {
        if (btn) { btn.disabled = false; }
    }
}

// 从列表删除批量任务队列
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
        
        // 如果当前正在查看这个队列的详情，关闭详情模态框
        if (batchQueuesState.currentQueueId === queueId) {
            closeBatchQueueDetailModal();
        }
        
        // 刷新队列列表
        refreshBatchQueues();
    } catch (error) {
        console.error('删除批量任务队列失败:', error);
        alert(_t('tasks.deleteQueueFailed') + ': ' + error.message);
    }
}

// 关闭批量任务队列详情模态框
function closeBatchQueueDetailModal() {
    closeAppModal('batch-queue-detail-modal');
    batchQueuesState.currentQueueId = null;
    stopBatchQueueRefresh();
}

// 开始批量队列刷新
function startBatchQueueRefresh(queueId) {
    if (batchQueuesState.refreshInterval) {
        clearInterval(batchQueuesState.refreshInterval);
    }

    batchQueuesState.refreshInterval = setInterval(() => {
        // 如果有内联编辑或添加任务模态框正在打开，跳过本次刷新防止丢失编辑内容
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
                console.warn('批量队列定时刷新失败:', e);
            } finally {
                batchQueuesState._bqDetailRefreshing = false;
            }
        })();
    }, 3000); // 每3秒刷新一次
}

// 停止批量队列刷新
function stopBatchQueueRefresh() {
    if (batchQueuesState.refreshInterval) {
        clearInterval(batchQueuesState.refreshInterval);
        batchQueuesState.refreshInterval = null;
    }
}

// 刷新批量任务队列列表
async function refreshBatchQueues() {
    await loadBatchQueues(batchQueuesState.currentPage);
}

// 查看批量任务的对话
function viewBatchTaskConversation(conversationId) {
    if (!conversationId) return;
    
    // 关闭批量任务详情模态框
    closeBatchQueueDetailModal();
    
    // 直接使用URL hash跳转，让router处理页面切换和对话加载
    // 这样更可靠，因为router会确保页面切换完成后再加载对话
    window.location.hash = `chat?conversation=${conversationId}`;
}

// --- 内联编辑：任务消息 ---
// 从元素获取任务信息并启动内联编辑
function editBatchTaskFromElement(button) {
    const taskItem = button.closest('.batch-task-item');
    if (!taskItem) return;

    const queueId = taskItem.getAttribute('data-queue-id');
    const taskId = taskItem.getAttribute('data-task-id');
    const taskMessage = taskItem.getAttribute('data-task-message');
    if (!queueId || !taskId) return;

    // 解码HTML实体
    const decodedMessage = taskMessage
        .replace(/&#39;/g, "'")
        .replace(/&quot;/g, '"')
        .replace(/\\n/g, '\n');

    // 找到 .batch-task-message 和 header 中的按钮
    const msgSpan = taskItem.querySelector('.batch-task-message');
    const header = taskItem.querySelector('.batch-task-header');
    if (!msgSpan || !header) return;

    // 隐藏编辑/删除按钮
    header.querySelectorAll('.batch-task-edit-btn, .batch-task-delete-btn').forEach(b => b.style.display = 'none');

    // 替换消息为内联编辑区域
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
    // 刷新整个详情来还原
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
        // 刷新队列详情
        if (batchQueuesState.currentQueueId === queueId) {
            showBatchQueueDetail(queueId);
        }

        // 刷新队列列表
        refreshBatchQueues();
    } catch (error) {
        _bqInlineSaving = false;
        console.error('保存任务失败:', error);
        alert(_t('tasks.saveTaskFailed') + ': ' + error.message);
    }
}

// 显示添加批量任务模态框
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
        console.error('添加任务模态框元素不存在');
        return;
    }
    
    messageInput.value = '';
    openAppModal('add-batch-task-modal', { focusEl: messageInput });
    
    // 清理旧的事件监听器
    if (showAddBatchTaskModal._escHandler) {
        document.removeEventListener('keydown', showAddBatchTaskModal._escHandler);
    }
    if (showAddBatchTaskModal._saveHandler && messageInput) {
        messageInput.removeEventListener('keydown', showAddBatchTaskModal._saveHandler);
    }

    // 添加ESC键监听
    showAddBatchTaskModal._escHandler = (e) => {
        if (e.key === 'Escape') {
            closeAddBatchTaskModal();
        }
    };
    document.addEventListener('keydown', showAddBatchTaskModal._escHandler);

    // 添加Enter+Ctrl/Cmd保存功能
    showAddBatchTaskModal._saveHandler = (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            saveAddBatchTask();
        }
    };
    messageInput.addEventListener('keydown', showAddBatchTaskModal._saveHandler);
}

// 关闭添加批量任务模态框
function closeAddBatchTaskModal() {
    // 清理事件监听器
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

// 保存添加的批量任务
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
        
        // 关闭添加任务模态框
        closeAddBatchTaskModal();
        
        // 刷新队列详情
        if (batchQueuesState.currentQueueId === queueId) {
            showBatchQueueDetail(queueId);
        }
        
        // 刷新队列列表
        refreshBatchQueues();
    } catch (error) {
        console.error('添加任务失败:', error);
        alert(_t('tasks.addTaskFailed') + ': ' + error.message);
    }
}

// 从元素获取任务信息并删除任务
function deleteBatchTaskFromElement(button) {
    const taskItem = button.closest('.batch-task-item');
    if (!taskItem) {
        console.error('无法找到任务项元素');
        return;
    }
    
    const queueId = taskItem.getAttribute('data-queue-id');
    const taskId = taskItem.getAttribute('data-task-id');
    const taskMessage = taskItem.getAttribute('data-task-message');
    
    if (!queueId || !taskId) {
        console.error('任务信息不完整');
        return;
    }
    
    // 解码HTML实体以显示消息
    const decodedMessage = taskMessage
        .replace(/&#39;/g, "'")
        .replace(/&quot;/g, '"')
        .replace(/\\n/g, '\n');
    
    // 截断长消息用于确认对话框
    const displayMessage = decodedMessage.length > 50 
        ? decodedMessage.substring(0, 50) + '...' 
        : decodedMessage;
    
    if (!confirm(_t('tasks.confirmDeleteTask', { message: displayMessage }))) {
        return;
    }
    
    deleteBatchTask(queueId, taskId);
}

// 删除批量任务
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
        
        // 刷新队列详情
        if (batchQueuesState.currentQueueId === queueId) {
            showBatchQueueDetail(queueId);
        }
        
        // 刷新队列列表
        refreshBatchQueues();
    } catch (error) {
        console.error('删除任务失败:', error);
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

// --- 内联编辑：取消所有正在编辑的内联区域 ---
function cancelAllInlineEdits() {
    _bqInlineSaving = true; // 防止 blur 触发保存
    const queueId = batchQueuesState.currentQueueId;
    if (queueId) showBatchQueueDetail(queueId);
    _bqInlineSaving = false;
}

// --- 内联编辑：标题 ---
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
        // 获取当前角色（保持不变）
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

// --- 内联编辑：角色 ---
function startInlineEditRole() {
    const container = document.getElementById('bq-role-val');
    if (!container) return;
    const queueId = batchQueuesState.currentQueueId;
    if (!queueId) return;
    // 获取当前详情中角色名 — 从 layout 的 data 中无法获取，故使用 API 拉取
    apiFetch(`/api/batch-tasks/${queueId}`).then(r => r.json()).then(detail => {
        const queue = detail.queue;
        const currentRole = queue.role || '';
        const roles = (Array.isArray(batchQueuesState.loadedRoles) ? batchQueuesState.loadedRoles : []).filter(r => r.name !== '默认' && r.enabled !== false).sort((a, b) => (a.name || '').localeCompare(b.name || '', 'zh-CN'));
        const currentInList = !currentRole || roles.some(r => r.name === currentRole);
        const orphanOpt = !currentInList ? `<option value="${escapeAttr(currentRole)}" selected>${escapeHtml(currentRole)} (${escapeHtml(_t('batchQueueDetailModal.roleNotFound') || '已移除')})</option>` : '';
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

// --- 内联编辑：代理模式 ---
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

// --- 内联编辑：并发数 ---
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
    const concurrency = normalizeBatchQueueConcurrencyInput(inp ? inp.value : 1);
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

// --- 单条执行 ---
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
        console.error('单条执行失败:', e);
        alert(e.message);
    }
}

// --- 内联编辑：调度配置 ---
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
        alert(_t('batchImportModal.cronExprInvalid') || 'Cron 表达式格式错误，需要 5 段（分 时 日 月 周）');
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

// 导出函数
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

// 语言切换后，列表/分页/详情弹窗由 JS 渲染的文案需用当前语言重绘（applyTranslations 不会处理 innerHTML 内容）
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
