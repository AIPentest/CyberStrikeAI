/** 工作流页私有工具：图解析、节点字典、运行状态、工作流包错误码映射。 */

export const NODE_TYPES = [
    { value: 'start', label: '开始' },
    { value: 'tool', label: '工具' },
    { value: 'agent', label: 'Agent' },
    { value: 'condition', label: '条件' },
    { value: 'hitl', label: '审批' },
    { value: 'output', label: '输出' },
    { value: 'end', label: '结束' }
];

export const NODE_TYPE_LABEL = Object.fromEntries(NODE_TYPES.map(item => [item.value, item.label]));

export const RUN_STATUS = {
    running: { label: '执行中', color: 'blue' },
    awaiting_hitl: { label: '等待审批', color: 'amber' },
    completed: { label: '已完成', color: 'green' },
    failed: { label: '失败', color: 'red' },
    cancelled: { label: '已取消', color: 'grey' },
    pending: { label: '待执行', color: 'amber' }
};

export const GRAPH_TEMPLATE = {
    nodes: [
        { id: 'start', type: 'start', label: '开始', position: { x: 80, y: 140 }, config: {} },
        { id: 'out', type: 'output', label: '输出', position: { x: 380, y: 140 }, config: { output_key: 'result' } }
    ],
    edges: [{ id: 'e1', source: 'start', target: 'out', label: '' }],
    config: {}
};

export const PACKAGE_ERROR_KEYS = {
    WFPKG_FILE_REQUIRED: 'workflows.package.errors.fileRequired',
    WFPKG_FILE_TOO_LARGE: 'workflows.package.errors.fileTooLarge',
    WFPKG_UNSUPPORTED_FORMAT: 'workflows.package.errors.unsupportedFormat',
    WFPKG_INVALID_ARCHIVE: 'workflows.package.errors.invalidArchive',
    WFPKG_INVALID_MANIFEST: 'workflows.package.errors.invalidManifest',
    WFPKG_CHECKSUM_MISMATCH: 'workflows.package.errors.checksumMismatch',
    WFPKG_MULTIPLE_WORKFLOWS: 'workflows.package.errors.multipleWorkflows',
    WFPKG_WORKFLOW_INVALID: 'workflows.package.errors.workflowInvalid',
    WFPKG_INSPECTION_NOT_FOUND: 'workflows.package.errors.inspectionNotFound',
    WFPKG_INSPECTION_EXPIRED: 'workflows.package.errors.inspectionExpired',
    WFPKG_INSPECTION_CONSUMED: 'workflows.package.errors.inspectionConsumed',
    WFPKG_CONFLICT_CHANGED: 'workflows.package.errors.conflictChanged',
    WFPKG_ID_CONFLICT: 'workflows.package.errors.idConflict',
    WFPKG_INVALID_ACTION: 'workflows.package.errors.invalidAction',
    WFPKG_INVALID_RENAME_ID: 'workflows.package.errors.invalidRenameId',
    WFPKG_OVERWRITE_CONFIRMATION_REQUIRED: 'workflows.package.errors.overwriteConfirmationRequired',
    WFPKG_IDEMPOTENCY_KEY_REQUIRED: 'workflows.package.errors.idempotencyKeyRequired',
    WFPKG_IDEMPOTENCY_KEY_REUSED: 'workflows.package.errors.idempotencyKeyReused',
    WFPKG_IMPORT_FAILED: 'workflows.package.errors.importFailed'
};

export const PACKAGE_ACTIONS = {
    create: { label: '创建导入', hint: '导入后会创建一个新的本地工作流。' },
    keep_existing: { label: '保留本地版本', hint: '不修改当前本地工作流；导入记录会保留。' },
    overwrite: { label: '覆盖本地版本', hint: '替换名称、描述、图定义和启用状态；需要再次确认。' },
    rename: { label: '另存为新 ID', hint: '保留当前本地工作流，并以新的 ID 创建副本。' }
};

export const CONFLICT_ACTIONS = {
    none: ['create'],
    identical: ['keep_existing'],
    id_conflict: ['keep_existing', 'overwrite', 'rename']
};

export function allowedActions(conflictState) {
    return (CONFLICT_ACTIONS[conflictState] || []).slice();
}

export function parseGraph(raw) {
    if (!raw || !String(raw).trim()) return { graph: null, error: '图定义不能为空' };
    try {
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object') return { graph: null, error: '图定义必须是 JSON 对象' };
        return {
            graph: {
                nodes: Array.isArray(parsed.nodes) ? parsed.nodes : [],
                edges: Array.isArray(parsed.edges) ? parsed.edges : [],
                config: parsed.config || {}
            },
            error: null
        };
    } catch (error) {
        return { graph: null, error: error.message || 'JSON 解析失败' };
    }
}

export function graphSummary(raw) {
    const { graph, error } = parseGraph(raw);
    if (!graph) return { nodes: [], edges: [], error };
    return { nodes: graph.nodes, edges: graph.edges, error: null };
}

export function shortHash(value, size = 12) {
    const text = String(value || '');
    if (!text) return '—';
    const body = text.startsWith('sha256:') ? text.slice(7) : text;
    return body.length > size ? `${body.slice(0, size)}…` : body;
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

export function packageErrorCode(error) {
    const body = error?.payload?.error;
    if (body && typeof body === 'object' && typeof body.code === 'string') return body.code;
    return '';
}

/** 工作流包接口用 {error:{code,message}}，优先按 code 取本地化提示。 */
export function packageErrorText(error, t) {
    const code = packageErrorCode(error);
    if (code && PACKAGE_ERROR_KEYS[code]) {
        const text = t?.(PACKAGE_ERROR_KEYS[code], null, '');
        if (text) return text;
    }
    return errText(error, '操作未完成，请稍后重试。');
}

export function createIdempotencyKey() {
    const cryptoApi = typeof globalThis !== 'undefined' ? globalThis.crypto : null;
    if (!cryptoApi || typeof cryptoApi.randomUUID !== 'function') return '';
    return cryptoApi.randomUUID();
}
