/** MCP 监控页展示工具：状态映射、工具名归一化、耗时与成功率计算。 */

export const STATUS_VALUES = ['completed', 'running', 'queued', 'failed', 'blocked', 'hard_timeout', 'cancelled', 'orphaned'];

const STATUS_KEY = {
    pending: 'statusPending',
    queued: 'statusQueued',
    running: 'statusRunning',
    completed: 'statusCompleted',
    failed: 'statusFailed',
    blocked: 'statusBlocked',
    cancelled: 'statusCancelled',
    hard_timeout: 'statusHardTimeout',
    orphaned: 'statusOrphaned'
};

const STATUS_FALLBACK = {
    pending: '等待中',
    queued: '排队中',
    running: '执行中',
    completed: '已完成',
    failed: '失败',
    blocked: '已拦截',
    cancelled: '已终止',
    hard_timeout: '执行超时',
    orphaned: '孤儿任务'
};

const STATUS_TAG_COLOR = {
    completed: 'green',
    running: 'blue',
    queued: 'amber',
    failed: 'red',
    blocked: 'orange',
    hard_timeout: 'red',
    cancelled: 'grey',
    orphaned: 'grey'
};

export function normalizeStatus(status) {
    return String(status || '').trim().toLowerCase();
}

export function statusLabel(t, status) {
    const key = normalizeStatus(status);
    const i18nKey = STATUS_KEY[key];
    if (i18nKey) return t(`mcpMonitor.${i18nKey}`, null, STATUS_FALLBACK[key] || key);
    return status || '—';
}

export function statusTagColor(status) {
    return STATUS_TAG_COLOR[normalizeStatus(status)] || 'grey';
}

/** 只有 running 才能终止（排队中的调用尚未建立取消通道）。 */
export function isRunning(status) {
    return normalizeStatus(status) === 'running';
}

/** 模型侧显示为 mcp__tool，内部存储为 mcp::tool；统一按模型侧展示。 */
export function toolDisplayName(name) {
    const value = String(name || '').trim();
    if (!value) return '';
    return value.includes('::') ? value.replace(/::/g, '__') : value;
}

/** 从 duration（Go time.Duration，纳秒）或起止时间推算毫秒。 */
export function executionDurationMs(exec) {
    if (!exec) return null;
    const raw = Number(exec.duration);
    if (Number.isFinite(raw) && raw > 0) return raw / 1e6;
    const start = exec.startTime ? new Date(exec.startTime).getTime() : NaN;
    const end = exec.endTime ? new Date(exec.endTime).getTime() : NaN;
    if (Number.isFinite(start) && Number.isFinite(end) && end >= start) return end - start;
    return null;
}

export function formatDurationMs(ms) {
    const value = Number(ms);
    if (!Number.isFinite(value) || value < 0) return '—';
    if (value < 1000) return `${Math.round(value)} ms`;
    if (value < 60_000) return `${(value / 1000).toFixed(2)} s`;
    const minutes = Math.floor(value / 60_000);
    const seconds = Math.round((value % 60_000) / 1000);
    if (minutes < 60) return `${minutes} 分 ${seconds} 秒`;
    const hours = Math.floor(minutes / 60);
    return `${hours} 小时 ${minutes % 60} 分`;
}

/** 成功率的分子排除被拦截调用（拦截既非成功也非执行失败）。 */
export function successRate(summary) {
    if (!summary) return null;
    const total = Number(summary.totalCalls) || 0;
    const blocked = Number(summary.blockedCalls) || 0;
    const denominator = total - blocked;
    if (denominator <= 0) return null;
    return (Number(summary.successCalls) || 0) / denominator;
}

export function formatPercentValue(rate, digits = 1) {
    if (rate === null || rate === undefined || !Number.isFinite(Number(rate))) return '—';
    return `${(Number(rate) * 100).toFixed(digits)}%`;
}

/** 当前页计时记录的平均耗时。 */
export function averageDurationMs(executions) {
    const samples = (executions || [])
        .map(executionDurationMs)
        .filter(value => Number.isFinite(value) && value >= 0);
    if (!samples.length) return null;
    return samples.reduce((sum, value) => sum + value, 0) / samples.length;
}

export function formatBucketLabel(iso, range) {
    const date = new Date(iso);
    if (!Number.isFinite(date.getTime())) return '';
    const pad = value => String(value).padStart(2, '0');
    const day = `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
    if (range === '30d') return day;
    if (range === '7d') return `${day} ${pad(date.getHours())}h`;
    return `${pad(date.getHours())}:00`;
}

export function formatBucketTooltip(point, t) {
    const time = point?.t ? new Date(point.t).toLocaleString('zh-CN', { hour12: false }) : '—';
    return t('mcpMonitor.timelineTooltip', {
        time,
        total: point?.total || 0,
        failed: point?.failed || 0,
        blocked: point?.blocked || 0
    }, `${time} 总调用 ${point?.total || 0}，失败 ${point?.failed || 0}，拦截 ${point?.blocked || 0}`);
}
