/** 格式化工具：时间、数字、字节、状态色板。 */

export const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low', 'info'];

export const SEVERITY_COLOR = {
    critical: 'red',
    high: 'orange',
    medium: 'amber',
    low: 'blue',
    info: 'grey'
};

export const SEVERITY_LABEL = {
    critical: '严重',
    high: '高危',
    medium: '中危',
    low: '低危',
    info: '信息'
};

export const STATUS_COLOR = {
    running: 'blue',
    pending: 'amber',
    queued: 'amber',
    paused: 'grey',
    success: 'green',
    succeeded: 'green',
    completed: 'green',
    done: 'green',
    failed: 'red',
    error: 'red',
    cancelled: 'grey',
    canceled: 'grey',
    stopped: 'grey',
    active: 'green',
    inactive: 'grey',
    online: 'green',
    offline: 'grey',
    open: 'red',
    closed: 'green',
    archived: 'grey'
};

export function formatDateTime(value, locale = 'zh-CN') {
    if (!value) return '—';
    const date = value instanceof Date ? value : new Date(value);
    if (!Number.isFinite(date.getTime())) return '—';
    return date.toLocaleString(locale, { hour12: false });
}

export function formatDate(value, locale = 'zh-CN') {
    if (!value) return '—';
    const date = value instanceof Date ? value : new Date(value);
    if (!Number.isFinite(date.getTime())) return '—';
    return date.toLocaleDateString(locale);
}

export function formatTime(value, locale = 'zh-CN') {
    if (!value) return '—';
    const date = value instanceof Date ? value : new Date(value);
    if (!Number.isFinite(date.getTime())) return '—';
    return date.toLocaleTimeString(locale, { hour12: false });
}

export function relativeTime(value, t) {
    if (!value) return '—';
    const date = new Date(value);
    if (!Number.isFinite(date.getTime())) return '—';
    const diff = Date.now() - date.getTime();
    if (diff < 60_000) return t?.('common.justNow', null, '刚刚') || '刚刚';
    if (diff < 3_600_000) return t?.('common.minutesAgo', { n: Math.floor(diff / 60_000) }, `${Math.floor(diff / 60_000)} 分钟前`);
    if (diff < 86_400_000) return t?.('common.hoursAgo', { n: Math.floor(diff / 3_600_000) }, `${Math.floor(diff / 3_600_000)} 小时前`);
    if (diff < 30 * 86_400_000) return t?.('common.daysAgo', { n: Math.floor(diff / 86_400_000) }, `${Math.floor(diff / 86_400_000)} 天前`);
    return formatDate(value);
}

export function formatBytes(bytes) {
    const value = Number(bytes);
    if (!Number.isFinite(value) || value < 0) return '—';
    if (value < 1024) return `${value} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let size = value / 1024;
    let index = 0;
    while (size >= 1024 && index < units.length - 1) { size /= 1024; index += 1; }
    return `${size.toFixed(size >= 100 ? 0 : 1)} ${units[index]}`;
}

export function formatNumber(value, locale = 'zh-CN') {
    const number = Number(value);
    if (!Number.isFinite(number)) return '—';
    return number.toLocaleString(locale);
}

export function formatDuration(seconds) {
    const value = Number(seconds);
    if (!Number.isFinite(value) || value < 0) return '—';
    if (value < 1) return `${Math.round(value * 1000)} ms`;
    if (value < 60) return `${value.toFixed(1)} s`;
    const minutes = Math.floor(value / 60);
    const rest = Math.round(value % 60);
    if (minutes < 60) return `${minutes}m ${rest}s`;
    return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

export function formatPercent(value, digits = 1) {
    const number = Number(value);
    if (!Number.isFinite(number)) return '—';
    return `${(number * (number <= 1 ? 100 : 1)).toFixed(digits)}%`;
}

export function severityColor(severity) {
    return SEVERITY_COLOR[String(severity || '').toLowerCase()] || 'grey';
}

export function statusColor(status) {
    return STATUS_COLOR[String(status || '').toLowerCase()] || 'grey';
}

export function truncate(text, length = 120) {
    const value = String(text ?? '');
    return value.length > length ? `${value.slice(0, length)}…` : value;
}

export function humanizeKey(key) {
    return String(key || '')
        .replace(/[_-]+/g, ' ')
        .replace(/\s+/g, ' ')
        .trim()
        .replace(/^\w/, match => match.toUpperCase());
}
