/**
 * 漏洞页共用常量与工具：
 * 枚举取自 internal/database/vulnerability.go（severity / status 注释），
 * 查询参数取自 internal/handler/vulnerability.go 的 parseVulnerabilityListFilter。
 */
import { SEVERITY_LABEL } from '../../lib/format';

export const SEVERITY_CODES = ['critical', 'high', 'medium', 'low', 'info'];
export const STATUS_CODES = ['open', 'confirmed', 'fixed', 'ignored', 'false_positive'];
export const FILTER_KEYS = ['q', 'severity', 'status', 'project_id', 'conversation_tag', 'task_tag'];
export const DEFAULT_PAGE_SIZE = 20;
export const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

const SEVERITY_KEYS = {
    critical: 'dashboard.severityCritical',
    high: 'dashboard.severityHigh',
    medium: 'dashboard.severityMedium',
    low: 'dashboard.severityLow',
    info: 'dashboard.severityInfo'
};

const STATUS_KEYS = {
    open: 'vulnerabilityPage.statusOpen',
    confirmed: 'vulnerabilityPage.statusConfirmed',
    fixed: 'vulnerabilityPage.statusFixed',
    ignored: 'vulnerabilityPage.statusIgnored',
    false_positive: 'vulnerabilityPage.statusFalsePositive'
};

const STATUS_FALLBACK = {
    open: '待处理',
    confirmed: '已确认',
    fixed: '已修复',
    ignored: '已忽略',
    false_positive: '误报'
};

const HISTORY_FALLBACK = {
    retest: '复测结果',
    retest_started: '复测分支已创建',
    merge: '归并上报',
    split: '拆分归并组',
    detach: '解除关联'
};

const SORT_OPTIONS = [
    { value: '', key: 'vulnerabilityPage.sortDefault', fallback: '默认顺序' },
    { value: 'severity', key: 'vulnerabilityPage.sortSeverity', fallback: '高危优先' },
    { value: 'latest', key: 'vulnerabilityPage.sortLatest', fallback: '创建时间：最新优先' },
    { value: 'oldest', key: 'vulnerabilityPage.sortOldest', fallback: '创建时间：最早优先' }
];

export function severityText(t, code) {
    if (!code) return '—';
    const key = SEVERITY_KEYS[code];
    return key ? t(key, null, SEVERITY_LABEL[code] || code) : String(code);
}

export function statusText(t, code) {
    if (!code) return '—';
    const key = STATUS_KEYS[code];
    return key ? t(key, null, STATUS_FALLBACK[code] || code) : String(code);
}

export function historyText(t, kind) {
    if (!kind) return '—';
    return t(`vulnerabilityPage.history_${kind}`, null, HISTORY_FALLBACK[kind] || kind);
}

export function severityOptions(t) {
    return SEVERITY_CODES.map(code => ({ value: code, label: severityText(t, code) }));
}

export function statusOptions(t) {
    return STATUS_CODES.map(code => ({ value: code, label: statusText(t, code) }));
}

export function sortOptions(t) {
    return SORT_OPTIONS.map(item => ({ value: item.value, label: t(item.key, null, item.fallback) }));
}

/** 列表/统计/导出共用的筛选查询；空值不发送，避免后端把空串当条件。 */
export function filterQuery(filters) {
    const query = {};
    FILTER_KEYS.forEach(key => {
        const value = String(filters?.[key] ?? '').trim();
        if (value) query[key] = value;
    });
    return query;
}

/** 看板统计保留项目/标签等筛选，但不带严重度（严重度由卡片本身切换）。 */
export function statsQuery(filters) {
    const query = filterQuery(filters);
    delete query.severity;
    return query;
}

export function hasActiveFilters(filters) {
    return FILTER_KEYS.some(key => String(filters?.[key] ?? '').trim() !== '');
}

export function percentOf(count, total) {
    const value = Number(count) || 0;
    const base = Number(total) || 0;
    return base > 0 ? Math.round((value / base) * 100) : 0;
}

/** 漏洞的主目标：target 原文 + 解析后的 host（后端 targets[]）。 */
export function targetText(vuln) {
    return String(vuln?.target || '').trim();
}

export function hostText(vuln) {
    const targets = Array.isArray(vuln?.targets) ? vuln.targets : [];
    const host = targets.map(item => item?.host).find(Boolean);
    return host || vuln?.target_host || vuln?.target_domain || '';
}

export function hostPortText(asset) {
    if (!asset) return '—';
    const host = asset.host || asset.id || '—';
    return asset.port ? `${host}:${asset.port}` : host;
}

export function shortId(value, size = 8) {
    const text = String(value || '');
    return text.length > size ? `${text.slice(0, size)}…` : text;
}

/**
 * 落盘文本文件。导出接口返回的是 {files:[{filename,content}]} JSON，
 * 只有单条下载才是二进制，所以这里按内容生成 Blob（与 lib/http downloadFile 同套路）。
 */
export function saveTextFile(filename, content) {
    const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    return filename;
}
