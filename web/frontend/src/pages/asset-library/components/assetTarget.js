/**
 * 资产目标解析：URL / 域名 / IP / IPv6 / 端口互转。
 * 规则与旧版 assets.js（parseAssetEditorTarget）保持一致，供编辑器、导入与 FOFA 入库共用。
 */

export const DEFAULT_PORTS = {
    http: 80, https: 443, ssh: 22, ftp: 21, smtp: 25, rdp: 3389,
    mysql: 3306, postgresql: 5432, redis: 6379, mongodb: 27017
};

export const PROTOCOL_BY_PORT = Object.entries(DEFAULT_PORTS)
    .reduce((acc, [protocol, port]) => ({ ...acc, [port]: protocol }), {});

export function defaultPortForProtocol(protocol) {
    return DEFAULT_PORTS[String(protocol || '').toLowerCase()] || 0;
}

export function protocolForPort(port) {
    return PROTOCOL_BY_PORT[Number(port)] || '';
}

export function isIPv4(value) {
    const parts = String(value || '').split('.');
    return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

export function isIPv6(value) {
    const candidate = String(value || '').replace(/^\[|\]$/g, '');
    if (!candidate.includes(':') || !/^[0-9a-f:.]+$/i.test(candidate)) return false;
    try {
        return new URL(`http://[${candidate}]/`).hostname.length > 2;
    } catch (error) {
        return false;
    }
}

export function normalizeDomain(value) {
    const raw = String(value || '').trim().replace(/\.$/, '');
    if (!raw || raw.length > 253 || raw.includes('_') || /[/?#@]/.test(raw)) return '';
    try {
        const hostname = new URL(`http://${raw}/`).hostname.toLowerCase().replace(/\.$/, '');
        if (!hostname || hostname.length > 253) return '';
        const labels = hostname.split('.');
        const valid = labels.every(label => label
            && label.length <= 63
            && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i.test(label));
        return valid ? hostname : '';
    } catch (error) {
        return '';
    }
}

/**
 * 解析用户输入的资产目标。始终返回结构体，格式无法识别时按不透明 host 处理；
 * 端口越界会抛出错误（与旧版行为一致）。
 */
/** 把粘贴的通报拆成单个地址。支持换行、空格、逗号和分号。 */
export function splitAssetTargets(value) {
    const seen = new Set();
    const targets = [];
    for (const item of String(value || '').split(/[\s,，;；]+/)) {
        const target = item.trim();
        const key = target.toLowerCase();
        if (!target || seen.has(key)) continue;
        seen.add(key);
        targets.push(target);
    }
    return targets;
}

export function parseAssetTarget(value) {
    const raw = String(value || '').trim();
    if (!raw) throw new Error('请输入资产地址');
    if (/\s/.test(raw)) throw new Error('资产地址不能包含空格');
    const opaque = () => ({ host: raw, ip: '', domain: '', port: 0, protocol: '' });
    let hostname = '';
    let port = 0;
    let protocol = '';
    let host = '';
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(raw)) {
        let parsed;
        try {
            parsed = new URL(raw);
        } catch (error) {
            return opaque();
        }
        if (!parsed.hostname || parsed.username || parsed.password) return opaque();
        protocol = parsed.protocol.replace(':', '').toLowerCase();
        hostname = parsed.hostname.replace(/^\[|\]$/g, '');
        port = parsed.port ? Number(parsed.port) : defaultPortForProtocol(protocol);
        host = raw;
    } else {
        const authority = raw.replace(/\/$/, '');
        const bracketed = authority.match(/^\[([^\]]+)](?::(\d+))?$/);
        if (bracketed) {
            hostname = bracketed[1];
            port = Number(bracketed[2] || 0);
        } else if ((authority.match(/:/g) || []).length === 1 && /:\d+$/.test(authority)) {
            const splitAt = authority.lastIndexOf(':');
            hostname = authority.slice(0, splitAt);
            port = Number(authority.slice(splitAt + 1));
        } else {
            hostname = authority;
        }
        protocol = protocolForPort(port);
    }
    if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error('端口必须在 0–65535 之间');
    const ipAddress = isIPv4(hostname) || isIPv6(hostname);
    const domain = ipAddress ? '' : normalizeDomain(hostname);
    if (!ipAddress && (!domain || isIPv4(domain) || isIPv6(domain))) return opaque();
    return {
        host,
        ip: ipAddress ? hostname.toLowerCase() : '',
        domain,
        port,
        protocol
    };
}

export function assetTargetLabel(asset) {
    return asset?.host || asset?.domain || asset?.ip || asset?.target || '-';
}

export function assetServiceLabel(asset) {
    if (!asset) return '';
    const protocol = String(asset.protocol || '').trim();
    const port = Number(asset.port || 0);
    if (!protocol && !port) return '';
    return `${protocol}${port ? `:${port}` : ''}`;
}

/** 编辑弹窗的目标输入初始值：优先 host，其次 domain/ip + 端口。 */
export function targetInputFromAsset(asset) {
    if (!asset) return '';
    if (asset.host) return String(asset.host);
    const base = asset.domain || asset.ip || '';
    if (!base) return '';
    const wrapped = String(base).includes(':') && !String(base).startsWith('[') ? `[${base}]` : base;
    return `${wrapped}${Number(asset.port || 0) > 0 ? `:${asset.port}` : ''}`;
}

/** 资产去重键（与服务端 dedup_key 同构，仅用于导入前的前端提示）。 */
export function assetDedupKey(asset) {
    const target = String(asset?.domain || asset?.ip || asset?.host || '').toLowerCase();
    return `${target}|${Number(asset?.port || 0)}|${String(asset?.protocol || '').toLowerCase()}`;
}

export function splitTags(raw) {
    return String(raw || '')
        .split(/[,，;；|]/)
        .map(item => item.trim())
        .filter(Boolean);
}

export function validateAssetPayload(asset) {
    if (!asset) return '资产不能为空';
    if (!asset.host && !asset.ip && !asset.domain) return '资产目标不能为空';
    if (asset.ip && !isIPv4(asset.ip) && !isIPv6(asset.ip)) return 'IP 地址格式无效';
    if (asset.domain && (!normalizeDomain(asset.domain) || isIPv4(asset.domain) || isIPv6(asset.domain))) {
        return '域名格式无效';
    }
    if (asset.protocol && !/^[a-z][a-z0-9+.-]{0,31}$/.test(asset.protocol)) return '协议格式无效';
    if (Number(asset.port) < 0 || Number(asset.port) > 65535) return '端口必须在 0–65535 之间';
    const tags = Array.isArray(asset.tags) ? asset.tags : [];
    if (tags.length > 30) return '标签最多 30 个';
    if (tags.some(tag => Array.from(String(tag)).length > 64)) return '单个标签最多 64 个字符';
    return '';
}
