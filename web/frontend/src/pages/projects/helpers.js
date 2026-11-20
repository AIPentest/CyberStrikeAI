/**
 * 项目页私有工具：分类/置信度字典、错误文本归一化、图谱配色。
 * 仅本项目目录内使用，不进入公共模块。
 */

export const FACT_CATEGORIES = [
    { value: 'target', label: 'target（目标）' },
    { value: 'auth', label: 'auth（认证）' },
    { value: 'infra', label: 'infra（基础设施）' },
    { value: 'business', label: 'business（业务）' },
    { value: 'finding', label: 'finding（发现）' },
    { value: 'chain', label: 'chain（攻击链）' },
    { value: 'exploit', label: 'exploit（利用）' },
    { value: 'poc', label: 'poc（POC）' },
    { value: 'note', label: 'note（备注）' }
];

export const CATEGORY_LABEL = {
    target: '目标',
    auth: '认证',
    infra: '基础设施',
    business: '业务',
    finding: '发现',
    chain: '攻击链',
    exploit: '利用',
    poc: 'POC',
    note: '备注',
    vuln: '漏洞',
    missing: '缺失'
};

export const CONFIDENCE_OPTIONS = [
    { value: 'tentative', label: '待确认' },
    { value: 'confirmed', label: '已确认' },
    { value: 'deprecated', label: '已废弃' }
];

export const CONFIDENCE_META = {
    confirmed: { label: '已确认', color: 'green' },
    tentative: { label: '待确认', color: 'amber' },
    deprecated: { label: '已废弃', color: 'grey' }
};

export const PROJECT_STATUS = [
    { value: 'active', label: '进行中' },
    { value: 'archived', label: '已归档' }
];

/** 图谱节点配色：与后端 GraphNodeType 对齐。 */
export const NODE_COLORS = {
    target: '#2563eb',
    auth: '#0891b2',
    infra: '#0d9488',
    finding: '#d97706',
    chain: '#db2777',
    exploit: '#7c3aed',
    poc: '#7c3aed',
    vulnerability: '#dc2626',
    note: '#64748b',
    missing: '#94a3b8'
};

export const NODE_TYPE_LABEL = {
    target: 'TARGET · 目标',
    auth: 'AUTH · 认证',
    infra: 'INFRA · 基础设施',
    finding: 'FINDING · 发现',
    chain: 'CHAIN · 攻击链',
    exploit: 'EXPLOIT · 利用',
    poc: 'POC · 验证',
    vulnerability: 'VULN · 漏洞',
    note: 'NOTE · 备注',
    missing: 'MISSING · 缺失'
};

export const EDGE_TYPE_HINTS = ['discovered_on', 'depends_on', 'leads_to', 'enables', 'exploits'];

/**
 * ApiError 的 message 可能是对象（工作流包等接口返回 {error:{code,message}}）。
 * 统一压成可展示字符串，避免把对象塞进 Banner/Toast。
 */
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

/** ErrorBanner 只接受带 message 的错误对象。 */
export function errorProp(error, fallback = '加载失败') {
    if (!error) return null;
    return { message: errText(error, fallback) };
}

export function factKeyOf(record) {
    return record?.fact_key || record?.key || record?.id || '';
}

export function isDeprecated(record) {
    return String(record?.confidence || '').toLowerCase() === 'deprecated';
}

/** 关系边（入边）文本框：每行 `type: source_fact_key`。 */
export function formatIncomingLinks(links) {
    if (!Array.isArray(links) || !links.length) return '';
    return links
        .map(edge => `${edge.edge_type || edge.type || ''}: ${edge.source_fact_key || edge.from || ''}`.trim())
        .join('\n');
}
