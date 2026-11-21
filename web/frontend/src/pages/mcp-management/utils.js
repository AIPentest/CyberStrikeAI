/** MCP 管理页共用展示映射。 */

export const CONNECTION_STATUS = {
    connected: { color: 'green', label: '已连接' },
    connecting: { color: 'blue', label: '连接中' },
    disconnected: { color: 'amber', label: '未连接' },
    disabled: { color: 'grey', label: '已停用' },
    error: { color: 'red', label: '连接失败' }
};

export function connectionMeta(status) {
    return CONNECTION_STATUS[String(status || '').toLowerCase()] || { color: 'grey', label: status || '未知' };
}

export function transportOf(config) {
    if (!config) return 'http';
    return String(config.type || config.transport || (config.command ? 'stdio' : 'http')).toLowerCase();
}

export function toolKeyOf(tool) {
    if (!tool) return '';
    if (tool.is_external && tool.external_mcp) return `${tool.external_mcp}::${tool.name}`;
    return tool.name || '';
}

export function sourceLabel(tool) {
    if (!tool) return '—';
    return tool.is_external ? `外部 · ${tool.external_mcp || '未知'}` : '内置';
}

export function parseToolListInput(text) {
    return String(text || '')
        .split(/[\n,;]/)
        .map(item => item.trim())
        .filter(Boolean);
}
