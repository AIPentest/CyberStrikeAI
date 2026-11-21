/**
 * WebShell 页面接口封装：连接管理、状态持久化、命令执行、文件操作与 AI 历史。
 * 所有请求走统一 http 层，不直接使用 fetch。
 */
import { api } from '../../lib/http';

/** 后端返回口令时使用的掩码，回写时原样提交表示“保持原口令”。 */
export const MASKED_SECRET = '********';

export function listConnections(query) {
    return api.get('/webshell/connections', query);
}

export function createConnection(body) {
    return api.post('/webshell/connections', body);
}

export function updateConnection(id, body) {
    return api.put(`/webshell/connections/${encodeURIComponent(id)}`, body);
}

export function deleteConnection(id) {
    return api.del(`/webshell/connections/${encodeURIComponent(id)}`);
}

export function getConnectionState(id) {
    return api.get(`/webshell/connections/${encodeURIComponent(id)}/state`);
}

export function saveConnectionState(id, state) {
    return api.put(`/webshell/connections/${encodeURIComponent(id)}/state`, { state });
}

export function execCommand({ connection, command }) {
    return api.post('/webshell/exec', {
        connection_id: connection.id,
        url: connection.url,
        type: connection.type || 'php',
        method: (connection.method || 'post').toLowerCase(),
        cmd_param: connection.cmdParam || '',
        encoding: connection.encoding || 'auto',
        os: connection.os || 'auto',
        command
    });
}

export function fileOp({ connection, action, path, targetPath, content, chunkIndex }) {
    const body = {
        connection_id: connection.id,
        url: connection.url,
        type: connection.type || 'php',
        method: (connection.method || 'post').toLowerCase(),
        cmd_param: connection.cmdParam || '',
        encoding: connection.encoding || 'auto',
        os: connection.os || 'auto',
        action
    };
    if (path !== undefined) body.path = path;
    if (targetPath !== undefined) body.target_path = targetPath;
    if (content !== undefined) body.content = content;
    if (chunkIndex !== undefined) body.chunk_index = chunkIndex;
    return api.post('/webshell/file', body);
}

export function listAiConversations(id) {
    return api.get(`/webshell/connections/${encodeURIComponent(id)}/ai-conversations`);
}

export function getAiHistory(id) {
    return api.get(`/webshell/connections/${encodeURIComponent(id)}/ai-history`);
}

export function getConversation(id) {
    return api.get(`/conversations/${encodeURIComponent(id)}`, { include_process_details: 0 });
}

export async function listProjects() {
    try {
        const payload = await api.get('/projects', { limit: 200 });
        return Array.isArray(payload?.projects) ? payload.projects : [];
    } catch (error) {
        return [];
    }
}

/** 探活令牌：随机串，避免固定回显被外层包装误判。 */
export function probeToken() {
    return `__CSAI_PROBE_${Math.random().toString(36).slice(2, 10)}_${Date.now().toString(36)}__`;
}

/** 通过一次 echo 判定连接是否可用。 */
export async function probeConnection(connection) {
    const token = probeToken();
    const result = await execCommand({ connection, command: `echo ${token}` });
    const ok = Boolean(result?.ok) && String(result?.output || '').includes(token);
    return { ok, output: result?.output || '', error: result?.error || '' };
}
