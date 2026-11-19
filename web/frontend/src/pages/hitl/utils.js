/** HITL 页共用：决策/审批方映射、载荷解析、审批时效文案。 */
import { formatDateTime } from '../../lib/format';

export const HITL_MODES = [
    { value: 'off', label: '关闭', hint: '不拦截工具调用' },
    { value: 'approval', label: '审批模式', hint: '只裁决通过/拒绝' },
    { value: 'review_edit', label: '审查编辑模式', hint: '可收窄参数后放行' }
];

const DECISION_LABEL = { approve: '通过', reject: '拒绝' };
const DECISION_COLOR = { approve: 'green', reject: 'red' };
const DECIDED_BY_LABEL = {
    human: '人工',
    audit_agent: '审计 Agent',
    system: '系统',
    manual: '手动录入'
};

export function normalizeMode(mode) {
    const value = String(mode || '').trim().toLowerCase().replace(/-/g, '_');
    if (value === 'feedback' || value === 'followup') return 'approval';
    return ['off', 'approval', 'review_edit'].includes(value) ? value : 'off';
}

export function modeLabel(mode) {
    const value = normalizeMode(mode);
    const found = HITL_MODES.find(item => item.value === value);
    return found ? found.label : value;
}

export function reviewerValue(reviewer) {
    const value = String(reviewer || '').trim().toLowerCase();
    return value === 'audit_agent' || value === 'agent' || value === 'ai' ? 'audit_agent' : 'human';
}

export function reviewerLabel(reviewer) {
    return reviewerValue(reviewer) === 'audit_agent' ? '审计 Agent' : '人工审批';
}

export function decisionLabel(decision) {
    const value = String(decision || '').trim().toLowerCase();
    return DECISION_LABEL[value] || value || '—';
}

export function decisionColor(decision) {
    return DECISION_COLOR[String(decision || '').toLowerCase()] || 'grey';
}

export function decidedByLabel(value) {
    const key = String(value || '').trim().toLowerCase();
    return DECIDED_BY_LABEL[key] || value || '—';
}

export function decisionStatusLabel(status) {
    const key = String(status || '').trim().toLowerCase();
    if (key === 'pending') return '待审批';
    if (key === 'decided') return '已裁决';
    if (key === 'timeout') return '超时自动拒绝';
    if (key === 'cancelled') return '已取消';
    return status || '—';
}

/** payload 在库中是 JSON 字符串；旧会话也可能是对象。 */
export function parsePayload(raw) {
    if (!raw) return {};
    if (typeof raw === 'object') return raw;
    try {
        const parsed = JSON.parse(String(raw));
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch (error) {
        return {};
    }
}

/** 优先取结构化参数 argumentsObj，其次解析 arguments 字符串。 */
export function payloadArguments(payload) {
    if (!payload || typeof payload !== 'object') return null;
    if (payload.argumentsObj && typeof payload.argumentsObj === 'object') return payload.argumentsObj;
    if (typeof payload.arguments === 'string' && payload.arguments.trim()) {
        try {
            const parsed = JSON.parse(payload.arguments);
            if (parsed && typeof parsed === 'object') return parsed;
        } catch (error) {
            return { raw: payload.arguments };
        }
    }
    if (payload.arguments && typeof payload.arguments === 'object') return payload.arguments;
    return null;
}

export function contextBlocks(payload) {
    if (!payload || typeof payload !== 'object') return [];
    return [
        { key: 'userMessage', label: '用户消息', value: payload.userMessage },
        { key: 'thinking', label: '思考', value: payload.thinking },
        { key: 'reasoningChain', label: '推理链', value: payload.reasoningChain },
        { key: 'planning', label: '计划', value: payload.planning }
    ].filter(item => String(item.value || '').trim());
}

export function executionResult(payload) {
    const result = payload?.executionResult;
    if (!result || typeof result !== 'object') return null;
    const text = String(result.result || '').trim();
    if (!text) return null;
    return { success: result.success === true, text };
}

export function approvalDeadlineText(payload) {
    const approval = payload?.hitlApproval;
    if (!approval || typeof approval !== 'object') return '';
    const timeoutSeconds = Number(approval.timeoutSeconds) || 0;
    if (timeoutSeconds <= 0) return '无超时限制';
    const expiresAt = approval.expiresAt ? new Date(approval.expiresAt) : null;
    if (expiresAt && Number.isFinite(expiresAt.getTime())) {
        if (expiresAt.getTime() <= Date.now()) return `已于 ${formatDateTime(expiresAt)} 到期`;
        return `${formatDateTime(expiresAt)} 截止`;
    }
    return `${timeoutSeconds} 秒内需裁决`;
}

export function formatHitlTime(value) {
    return value ? formatDateTime(value) : '—';
}

export function parseToolTokens(text) {
    return String(text || '')
        .split(/[\n,，;；]/)
        .map(item => item.trim())
        .filter(Boolean)
        .filter((item, index, list) => list.indexOf(item) === index);
}

export function summarizePayload(payload) {
    const parts = [];
    if (payload?.userMessage) parts.push('用户消息');
    if (payload?.thinking) parts.push('思考');
    if (payload?.executionResult) parts.push('执行结果');
    return parts.length ? parts.join(' · ') : '—';
}
