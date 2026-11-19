/** FOFA/ZoomEye/Quake/Shodan 结果行 → 资产结构，并批量入库。 */
import { api } from '../../../lib/http';
import { isIPv4, isIPv6, normalizeDomain } from './assetTarget';

function cellValue(row, fields, name) {
    if (row && !Array.isArray(row) && typeof row === 'object') {
        const value = row[name];
        if (Array.isArray(value)) return value.filter(Boolean).join(',');
        return value == null ? '' : String(value).trim();
    }
    const index = Array.isArray(fields) ? fields.indexOf(name) : -1;
    if (index < 0 || !Array.isArray(row)) return '';
    const value = row[index];
    return value == null ? '' : String(value).trim();
}

function firstValue(row, fields, names) {
    for (const name of names) {
        const value = cellValue(row, fields, name);
        if (value) return value;
    }
    return '';
}

/** 把一条检索结果转换成 /assets/import 可接受的资产对象。 */
export function fofaRowToAsset(row, fields, provider = 'fofa') {
    const port = Number.parseInt(cellValue(row, fields, 'port'), 10);
    const rawIP = firstValue(row, fields, ['ip', 'ip_str']);
    const ip = isIPv4(rawIP) || isIPv6(rawIP) ? rawIP.toLowerCase() : '';
    const rawDomain = firstValue(row, fields, ['domain', 'hostname', 'hostnames', 'domains'])
        .split(',')[0]
        .replace(/^\[|\]$/g, '')
        .replace(/^"|"$/g, '');
    const domain = rawDomain && !isIPv4(rawDomain) && !isIPv6(rawDomain)
        ? normalizeDomain(rawDomain)
        : '';
    const rawProtocol = firstValue(row, fields, ['protocol', 'service.name', 'transport']).toLowerCase();
    const rawHost = firstValue(row, fields, ['host', 'hostname']);
    return {
        host: rawHost.split(',')[0].trim(),
        ip,
        port: Number.isFinite(port) ? port : 0,
        domain,
        protocol: /^[a-z][a-z0-9+.-]{0,31}$/.test(rawProtocol) ? rawProtocol : '',
        title: firstValue(row, fields, ['title', 'service.http.title']),
        server: firstValue(row, fields, ['server', 'product']),
        country: firstValue(row, fields, ['country', 'location.country_cn', 'location.country_name']),
        province: firstValue(row, fields, ['region', 'province', 'location.province_cn']),
        city: firstValue(row, fields, ['city', 'location.city_cn', 'location.city']),
        source: provider || 'fofa',
        status: 'active'
    };
}

/**
 * 将选中的结果行入库。
 * 返回 { assets, invalidCount, created, updated, skipped }。
 */
export async function importFofaRows(rows, { fields, provider, query }) {
    const converted = (rows || []).map(row => fofaRowToAsset(row, fields, provider));
    const assets = converted.filter(asset => asset.host || asset.ip || asset.domain);
    const invalidCount = converted.length - assets.length;
    if (!assets.length) {
        return { assets, invalidCount, created: 0, updated: 0, skipped: 0 };
    }
    const result = await api.post('/assets/import', {
        assets,
        source: provider || 'fofa',
        source_query: query || ''
    });
    return {
        assets,
        invalidCount,
        created: Number(result?.created || 0),
        updated: Number(result?.updated || 0),
        skipped: Number(result?.skipped || 0)
    };
}
