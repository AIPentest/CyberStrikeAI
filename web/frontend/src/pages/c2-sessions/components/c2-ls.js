/**
 * 解析 C2 `ls` 任务输出：优先识别 Beacon 结构化行（type\tmode\tsize\tname），
 * 否则回退到原生 `ls -l` 输出。
 */

export function parseC2Listing(rawOutput) {
    const lines = String(rawOutput || '').split(/\r?\n/);
    const items = [];
    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed || /^total\s+\d+/i.test(trimmed)) continue;

        const tabParts = trimmed.split('\t');
        if (tabParts.length >= 4) {
            const name = tabParts.slice(3).join('\t').trim();
            if (!name || name === '.' || name === '..') continue;
            const type = tabParts[0].trim();
            const mode = tabParts[1].trim();
            items.push({
                name,
                size: tabParts[2].trim(),
                mode: mode || type,
                isDir: type.startsWith('d') || mode.startsWith('d')
            });
            continue;
        }

        const match = trimmed.match(/^\S{1,10}\s+\d+\s+\S+\s+\S+\s+\d+\s+\S+\s+\S+\s+\S+\s+(.+)$/);
        if (!match) continue;
        let name = match[1].trim();
        const arrow = name.indexOf(' -> ');
        if (arrow > 0) name = name.slice(0, arrow).trim();
        if (!name || name === '.' || name === '..') continue;
        const parts = trimmed.split(/\s+/);
        items.push({
            name,
            size: parts[4] || '',
            mode: parts[0] || '',
            isDir: (parts[0] || '').startsWith('d')
        });
    }
    return items;
}

/** 规范化远端路径，兼容 Windows 反斜杠与相对路径。 */
export function normalizeRemotePath(input) {
    let value = String(input ?? '').trim();
    if (!value) return '.';
    if (/^[A-Za-z]:/.test(value)) return value.replace(/\\+$/, '') || value;
    value = value.replace(/\\/g, '/').replace(/\/{2,}/g, '/');
    if (value.length > 1) value = value.replace(/\/+$/, '');
    return value || '.';
}

export function joinRemotePath(base, name) {
    const dir = normalizeRemotePath(base);
    if (!name) return dir;
    if (/^[A-Za-z]:/.test(name) || name.startsWith('/')) return normalizeRemotePath(name);
    if (dir === '.' || dir === '') return normalizeRemotePath(name);
    const sep = dir.includes('\\') ? '\\' : '/';
    return normalizeRemotePath(`${dir}${sep}${name}`);
}

export function remoteParentPath(input) {
    const value = normalizeRemotePath(input);
    if (value === '.' || value === '/') return value;
    if (/^[A-Za-z]:$/.test(value)) return value;
    const sep = value.includes('\\') ? '\\' : '/';
    const index = value.lastIndexOf(sep);
    if (index < 0) return '.';
    if (index === 0) return '/';
    const head = value.slice(0, index);
    return /^[A-Za-z]:$/.test(head) ? `${head}${sep}` : head;
}
