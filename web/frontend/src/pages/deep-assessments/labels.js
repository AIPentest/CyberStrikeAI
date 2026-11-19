/** 深度测评页面共用的状态文案、进度换算与动作可用性判断。 */

export const PAGE_SIZE = 20;

export const STATUS_LABEL = {
    draft: '草稿',
    running: '执行中',
    pending: '待执行',
    completed: '已完成',
    blocked: '受阻',
    failed: '失败',
    paused: '已暂停',
    pausing: '暂停中',
    cancelling: '停止中',
    cancelled: '已停止',
    recovering: '待恢复',
    waiting_retry: '等待模型恢复',
    waiting_input: '待答复',
    waiting_progress: '等待新进展',
    needs_review: '待核对',
    uncertain: '结果不明',
    interrupted: '已中断',
    cleanup_unconfirmed: '待确认资源停止',
    retired: '已撤下',
    reconciled: '已核对',
    yielded: '分段完成',
    archived: '已归档'
};

export const ACTIVE_STATUSES = ['running', 'pausing', 'cancelling'];
export const TERMINAL_STATUSES = ['completed', 'cancelled'];
export const REVIEW_STATUSES = ['needs_review', 'uncertain', 'cleanup_unconfirmed'];

export const KIND_LABEL = {
    goal: '目标',
    step: '探索意图',
    observation: '观察',
    receipt: '执行回执',
    memory: 'Pi 摘要',
    digest: '历史归档',
    guidance: '人工线索',
    fact: '事实',
    finding: '漏洞'
};

export const STATE_LABEL = {
    observed: '直接观察',
    inferred: '推断待证',
    reported: '待独立复核',
    confirmed: '复核确认',
    refuted: '复核否定',
    inconclusive: '复核未定',
    unreviewed: '历史记录待核对',
    summary: '压缩摘要',
    note: '观察记录',
    user: '用户提供',
    folded: '已折叠',
    superseded: '已替代的归档',
    completed: '已完成',
    running: '处理中',
    failed: '失败',
    uncertain: '结果待核对',
    cancelled: '已取消',
    reconciled: '已核对',
    interrupted: '已中断',
    error: '执行失败',
    missing: '缺证据',
    needs_review: '待复核',
    has_evidence: '已有证据'
};

export const EVENT_LABEL = {
    started: '开始/继续测评',
    reopened: '继续已停止的测评',
    step_started: '意图开始执行',
    planning: '更新计划',
    plan_updated: '计划已更新',
    checkpoint: '已保存进展',
    step_result: '意图执行结果',
    settled: '调度已结束',
    stop_requested: '暂停/停止请求',
    intent_pause: '暂停此意图',
    intent_resume: '意图已恢复排队',
    intent_priority: '优先级已调整',
    intent_guidance: '收到定向补充',
    waiting_progress: '等待新进展',
    waiting_retry: '等待模型恢复',
    waiting_input: '等待答复',
    blocked: '执行受阻',
    guidance: '收到补充方向',
    retry_scheduled: '稍后重试模型',
    review_gap: '验收仍有缺口',
    shutdown: '保存进度并关闭',
    retry: '步骤已重新排队',
    reconciled: '已确认核对'
};

export const CONFIDENCE_LABEL = { observed: '已观察', inferred: '推断' };
export const CONFIDENCE_COLOR = { observed: 'green', inferred: 'amber' };

export const ACCEPTANCE_LABEL = {
    has_evidence: '已有证据',
    missing: '缺证据',
    needs_review: '待人工复核'
};
export const ACCEPTANCE_COLOR = {
    has_evidence: 'green',
    missing: 'red',
    needs_review: 'amber'
};

export const PERM_READ = 'assessments:read';
export const PERM_WRITE = 'assessments:write';
export const PERM_DELETE = 'assessments:delete';

export function statusText(t, status) {
    if (!status) return '—';
    return t(`deepAssessments.states.${status}`, null, STATUS_LABEL[status] || status);
}

export function kindText(t, kind) {
    return t(`deepAssessments.graphKinds.${kind}`, null, KIND_LABEL[kind] || kind || '节点');
}

export function stateText(t, state) {
    if (!state) return '—';
    return t(`deepAssessments.graphStates.${state}`, null, STATE_LABEL[state] || statusText(t, state));
}

export function eventText(t, kind) {
    if (!kind) return '事件';
    return t(`deepAssessments.workbench.events.${kind}`, null, EVENT_LABEL[kind] || kind);
}

export function confidenceText(t, value) {
    return t(`deepAssessments.confidence.${value}`, null, CONFIDENCE_LABEL[value] || value || '—');
}

export function acceptanceText(t, value) {
    return t(`deepAssessments.acceptanceStatus.${value}`, null, ACCEPTANCE_LABEL[value] || value || '—');
}

export function isActive(task) {
    return ACTIVE_STATUSES.includes(task?.status);
}

export function isTerminal(task) {
    return TERMINAL_STATUSES.includes(task?.status);
}

export function needsReview(task) {
    if (!task) return false;
    if (Number(task.review_count) > 0) return true;
    if (REVIEW_STATUSES.includes(task.status)) return true;
    return (task.steps || []).some(step => REVIEW_STATUSES.includes(step.status));
}

export function canStart(task) {
    return Boolean(task) && !task.archived && !isTerminal(task) && !isActive(task)
        && !task.question && task.status !== 'waiting_input';
}

export function canPause(task) {
    return Boolean(task) && !task.archived
        && ['running', 'recovering', 'waiting_retry', 'waiting_progress'].includes(task.status);
}

export function canCancel(task) {
    return Boolean(task) && !task.archived && !isTerminal(task);
}

export function canContinueStopped(task) {
    return Boolean(task) && task.status === 'cancelled' && !task.archived;
}

export function canArchive(task) {
    return Boolean(task) && !task.archived && !isActive(task) && !needsReview(task)
        && !['recovering', 'needs_review', 'uncertain', 'cleanup_unconfirmed'].includes(task.status);
}

export function canRestore(task) {
    return Boolean(task?.archived);
}

export function canDeleteTask(task) {
    return Boolean(task) && !isActive(task);
}

/** 把步骤状态统计换算成完成度；retired 表示已撤下，同样不再占用进度。 */
export function countsProgress(counts, total) {
    const safeCounts = counts || {};
    const done = (safeCounts.completed || 0) + (safeCounts.retired || 0);
    const size = Number.isFinite(total) && total > 0 ? total : Object.values(safeCounts).reduce((sum, value) => sum + Number(value || 0), 0);
    const percent = size > 0 ? Math.round((done / size) * 100) : 0;
    return { done, total: size, percent: Math.min(100, Math.max(0, percent)) };
}

export function stepsProgress(task) {
    const counts = {};
    (task?.steps || []).forEach(step => {
        counts[step.status] = (counts[step.status] || 0) + 1;
    });
    return countsProgress(counts, (task?.steps || []).length);
}

export function formatElapsed(seconds) {
    const value = Math.floor(Number(seconds));
    if (!Number.isFinite(value) || value < 0) return '—';
    if (value < 60) return `${value} 秒`;
    const remainder = `${Math.floor(value / 60) % 60} 分 ${value % 60} 秒`;
    if (value < 3600) return remainder;
    const hours = Math.floor(value / 3600);
    return `${hours} 小时 ${remainder}`;
}

export function truncateText(value, max = 80) {
    const text = String(value ?? '');
    return text.length > max ? `${text.slice(0, max)}…` : text;
}

/** 回执参数与结果是字符串形式的 JSON；解析失败时按原文展示。 */
export function parseMaybeJson(value) {
    if (typeof value !== 'string') return value;
    const text = value.trim();
    if (!text || (text[0] !== '{' && text[0] !== '[')) return value;
    try {
        return JSON.parse(text);
    } catch (error) {
        return value;
    }
}

export const GRAPH_KIND_COLOR = {
    goal: '#0066ff',
    step: '#13a8a8',
    fact: '#00b42a',
    finding: '#f53f3f',
    observation: '#722ed1',
    receipt: '#86909c',
    memory: '#ff7d00',
    digest: '#c9cdd4',
    guidance: '#eb2f96'
};
