/** 任务页私有工具：状态字典、进度计算、错误文本归一化。 */

export const QUEUE_STATUS = {
    pending: { label: '待执行', color: 'amber' },
    running: { label: '执行中', color: 'blue' },
    paused: { label: '已暂停', color: 'grey' },
    completed: { label: '已完成', color: 'green' },
    cancelled: { label: '已取消', color: 'grey' }
};

export const TASK_STATUS = {
    pending: { label: '待执行', color: 'amber' },
    running: { label: '执行中', color: 'blue' },
    completed: { label: '已完成', color: 'green' },
    failed: { label: '失败', color: 'red' },
    cancelled: { label: '已取消', color: 'grey' },
    blocked: { label: '受阻', color: 'orange' }
};

export const AGENT_MODES = [
    { value: 'single', label: '单代理（Pi SDK）' },
    { value: 'deep', label: 'Deep（DeepAgent）' },
    { value: 'plan_execute', label: 'Plan-Execute' },
    { value: 'supervisor', label: 'Supervisor（专家路由）' }
];

export const HITL_POLICIES = [
    { value: '', label: '沿用全局设置' },
    { value: 'off', label: '关闭审批' },
    { value: 'human', label: '人工审批' },
    { value: 'audit_agent', label: 'Agent 审批' },
    { value: 'review_edit', label: 'Agent 审查编辑' }
];

export const SCHEDULE_MODES = [
    { value: 'manual', label: '手工执行' },
    { value: 'cron', label: 'Cron 调度' }
];

export const QUEUE_STATUS_FILTERS = [
    { value: 'all', label: '全部状态' },
    { value: 'pending', label: '待执行' },
    { value: 'running', label: '执行中' },
    { value: 'paused', label: '已暂停' },
    { value: 'completed', label: '已完成' },
    { value: 'cancelled', label: '已取消' }
];

export function queueStats(queue) {
    const tasks = Array.isArray(queue?.tasks) ? queue.tasks : [];
    const total = tasks.length;
    const completed = tasks.filter(task => task.status === 'completed').length;
    const failed = tasks.filter(task => task.status === 'failed').length;
    const cancelled = tasks.filter(task => task.status === 'cancelled').length;
    const blocked = tasks.filter(task => task.status === 'blocked').length;
    const running = tasks.filter(task => task.status === 'running').length;
    const pending = tasks.filter(task => task.status === 'pending').length;
    const done = completed + failed + cancelled + blocked;
    const percent = total ? Math.round((done / total) * 100) : 0;
    return { total, completed, failed, cancelled, blocked, running, pending, done, percent };
}

export function agentModeLabel(mode) {
    return (AGENT_MODES.find(item => item.value === String(mode || 'single')) || AGENT_MODES[0]).label;
}

/** 状态展示：Cron 队列在「本轮完成」时强调循环调度，底层 status 不变。 */
export function queueStatusView(queue) {
    const base = QUEUE_STATUS[String(queue?.status || '')] || { label: queue?.status || '—', color: 'grey' };
    const cronOn = queue?.scheduleMode === 'cron' && queue?.scheduleEnabled !== false;
    const nextRun = queue?.nextRunAt ? new Date(queue.nextRunAt) : null;
    const nextText = nextRun && Number.isFinite(nextRun.getTime()) ? nextRun.toLocaleString('zh-CN', { hour12: false }) : '';
    const stats = queueStats(queue);
    const blockedNote = stats.blocked > 0 ? (queue?.lastRunError || `${stats.blocked} 项受阻`) : '';
    if (stats.blocked > 0 && stats.completed === 0 && stats.failed === 0) {
        return { ...base, label: `受阻（${stats.blocked} 项）`, color: 'orange', sublabel: blockedNote };
    }
    if (cronOn && queue?.status === 'completed') {
        const unfinished = stats.failed > 0 || stats.blocked > 0;
        return {
            ...base,
            label: unfinished ? '本轮已结束 · 定时循环中' : '本轮已完成 · 定时循环中',
            color: stats.failed > 0 ? 'red' : (stats.blocked > 0 ? 'orange' : base.color),
            sublabel: blockedNote || (nextText ? `下次执行：${nextText}` : ''),
            note: '定时队列：子任务已跑完，到点将自动下一轮'
        };
    }
    if (cronOn && queue?.status === 'pending' && nextText) {
        return { ...base, sublabel: `Cron 已排程 · 下次 ${nextText}` };
    }
    if (queue?.status === 'completed' && stats.failed > 0) {
        const blockedText = stats.blocked > 0 ? `，${stats.blocked} 项受阻` : '';
        return { ...base, label: `已结束（${stats.failed} 项失败${blockedText}）`, color: 'red', sublabel: blockedNote };
    }
    if (queue?.status === 'completed' && stats.blocked > 0) {
        return { ...base, label: `已结束（${stats.blocked} 项受阻）`, color: 'orange', sublabel: blockedNote };
    }
    return { ...base, sublabel: '' };
}

export function durationText(start, end) {
    if (!start) return '—';
    const from = new Date(start).getTime();
    const to = end ? new Date(end).getTime() : Date.now();
    if (!Number.isFinite(from) || !Number.isFinite(to) || to < from) return '—';
    const seconds = Math.round((to - from) / 1000);
    if (seconds < 60) return `${seconds} 秒`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
    return `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`;
}

export function errText(error, fallback = '操作失败') {
    if (!error) return fallback;
    if (typeof error === 'string') return error;
    const payload = error.payload;
    if (payload && typeof payload === 'object') {
        const body = payload.error;
        if (typeof body === 'string' && body) return body;
        if (body && typeof body === 'object') {
            const message = typeof body.message === 'string' ? body.message : '';
            const code = typeof body.code === 'string' ? body.code : '';
            if (message) return code ? `${message}（${code}）` : message;
        }
        if (typeof payload.message === 'string' && payload.message) return payload.message;
    }
    if (typeof error.message === 'string' && error.message && error.message !== '[object Object]') return error.message;
    return fallback;
}

export function errorProp(error, fallback = '加载失败') {
    if (!error) return null;
    return { message: errText(error, fallback) };
}
