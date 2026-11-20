/**
 * WebShell 文件浏览辅助：解析 `ls -la` / Windows `dir` 输出并处理路径拼接。
 * 解析规则与旧版 web/static/js/webshell.js 的 parseWebshellListItems 保持一致。
 */

const MONTH_INDEX = {
    Jan: 0, Feb: 1, Mar: 2, Apr: 3, May: 4, Jun: 5,
    Jul: 6, Aug: 7, Sep: 8, Oct: 9, Nov: 10, Dec: 11
};

function normalizeMtime(month, day, timeOrYear) {
    if (!month || !day || !timeOrYear) return '';
    const token = String(timeOrYear).trim();
    if (/^\d{4}$/.test(token)) return `${token} ${month} ${day}`;
    const now = new Date();
    let year = now.getFullYear();
    if (/^\d{1,2}:\d{2}$/.test(token)) {
        const monthIdx = MONTH_INDEX[month];
        const dayNum = parseInt(day, 10);
        if (monthIdx != null && !Number.isNaN(dayNum)) {
            const inferred = new Date(year, monthIdx, dayNum);
            if (inferred.getTime() > now.getTime()) year -= 1;
        }
        return `${year} ${month} ${day} ${token}`;
    }
    return `${month} ${day} ${token}`;
}

function modeToType(mode) {
    if (!mode) return '';
    const head = mode.charAt(0);
    const map = { d: 'dir', '-': 'file', l: 'link', c: 'char', b: 'block', s: 'socket', p: 'pipe' };
    return map[head] || head;
}

function parseWindowsEntry(line) {
    const match = String(line || '').match(
        /^(\d{4}[/-]\d{1,2}[/-]\d{1,2})\s+(\d{1,2}:\d{2})(?:\s*(AM|PM))?\s+(<[^>]+>|[\d,]+)\s+(.+?)\s*$/i
    );
    if (!match) return null;
    const kind = (match[4] || '').trim();
    const name = (match[5] || '').trim();
    if (!name || name === '.' || name === '..') return null;
    const isDir = /^<(dir|junction|symlinkd)>$/i.test(kind);
    return {
        name,
        isDir,
        size: isDir ? '' : kind.replace(/,/g, ''),
        mtime: `${match[1]} ${match[2]}${match[3] ? ` ${match[3].toUpperCase()}` : ''}`.trim(),
        mode: isDir ? 'd' : '-',
        owner: '',
        group: '',
        type: isDir ? 'dir' : 'file'
    };
}

const SKIP_LINE = [
    /^(total|总计)\s+\d+$/i,
    /^(驱动器|卷的序列号是|volume in drive|volume serial number is|directory of)/i,
    /^[A-Za-z]:\\.*\s+的目录$/i,
    /^\d+\s+(个文件|file\(s\))\s+[\d,]+\s+(字节|bytes?)$/i,
    /^\d+\s+(个目录|dir\(s\))\s+[\d,]+\s+(可用字节|bytes free)$/i,
    /^[^>\n]*>\s*dir(?:\s|$)/i
];

/** 把目录列表原始输出解析为文件项数组。 */
export function parseListing(rawOutput) {
    const lines = String(rawOutput || '').split(/\n/).filter(line => line.trim());
    const items = [];
    for (const line of lines) {
        const trimmed = line.trim();
        if (SKIP_LINE.some(pattern => pattern.test(trimmed))) continue;

        const unix = line.match(/^(\S+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\d+)\s+([A-Za-z]{3})\s+(\d{1,2})\s+(\S+)\s+(.+)$/);
        if (unix) {
            const name = (unix[9] || '').trim();
            if (name === '.' || name === '..') continue;
            items.push({
                name,
                isDir: unix[1].startsWith('d'),
                size: unix[5],
                mode: unix[1],
                mtime: normalizeMtime(unix[6], unix[7], unix[8]),
                owner: unix[3],
                group: unix[4],
                type: modeToType(unix[1]),
                line
            });
            continue;
        }

        const win = parseWindowsEntry(line);
        if (win) {
            items.push({ ...win, line });
            continue;
        }

        if (!/^[bcdlps-][rwxStTs-]{9}[+.@]?\s/.test(line)) continue;
        const parts = trimmed.split(/\s+/);
        const name = (parts.length >= 9 ? parts.slice(8).join(' ') : parts[parts.length - 1] || '').trim();
        if (!name || name === '.' || name === '..') continue;
        items.push({
            name,
            isDir: line.startsWith('d'),
            size: parts[4] || '',
            mode: parts[0] || '',
            owner: parts[2] || '',
            group: parts[3] || '',
            mtime: parts[5] && /^[A-Za-z]{3}$/.test(parts[5]) ? normalizeMtime(parts[5], parts[6], parts[7]) : '',
            type: modeToType(parts[0]),
            line
        });
    }
    return items;
}

/** 归一化路径：去掉重复斜杠与尾部斜杠，空路径回落为 "."。 */
export function normalizePath(input) {
    let value = String(input ?? '').trim();
    if (!value) return '.';
    value = value.replace(/\\/g, '/').replace(/\/{2,}/g, '/');
    if (value.length > 1) value = value.replace(/\/+$/, '');
    // Windows 盘符根（C: / C:/）保留尾斜杠，避免退化成相对盘符
    if (/^[A-Za-z]:$/.test(value)) return `${value}/`;
    return value || '.';
}

export function joinPath(base, name) {
    const dir = normalizePath(base);
    if (!name) return dir;
    if (name.startsWith('/') || /^[A-Za-z]:/.test(name)) return normalizePath(name);
    if (dir === '.') return normalizePath(name);
    if (dir === '/') return normalizePath(`/${name}`);
    return normalizePath(`${dir}/${name}`);
}

export function parentPath(input) {
    const dir = normalizePath(input);
    if (dir === '.' || dir === '/') return dir;
    if (/^[A-Za-z]:\/$/.test(dir)) return dir;
    const idx = dir.lastIndexOf('/');
    if (idx < 0) return '.';
    if (idx === 0) return '/';
    const head = dir.slice(0, idx);
    return /^[A-Za-z]:$/.test(head) ? `${head}/` : head;
}

/** 面包屑分段：返回 [{label, path}]，首段为根。 */
export function pathSegments(input) {
    const dir = normalizePath(input);
    const drive = /^[A-Za-z]:/.test(dir);
    const absolute = !drive && dir.startsWith('/');
    const body = drive ? dir.replace(/^[A-Za-z]:\/?/, '') : dir.replace(/^\//, '');
    const parts = dir === '.' || dir === '/' || !body ? [] : body.split('/');
    const rootLabel = drive ? dir.slice(0, 2) + '/' : absolute ? '/' : '.';
    const rootPath = drive ? `${dir.slice(0, 2)}/` : absolute ? '/' : '.';
    const segments = [{ label: rootLabel, path: rootPath }];
    parts.forEach((part, index) => {
        const prefix = parts.slice(0, index + 1).join('/');
        segments.push({ label: part, path: drive ? `${dir.slice(0, 2)}/${prefix}` : absolute ? `/${prefix}` : prefix });
    });
    return segments;
}
