/**
 * 资产 CSV/JSON 导入解析与 CSV 导出。
 * 旧版支持 .xlsx/.csv；新前端不引入表格库，改为 CSV/JSON 文本与文件上传统一解析。
 */
import {
    CRITICALITY_LABELS, ENVIRONMENT_LABELS, EXPORT_COLUMNS, IMPORT_COLUMNS,
    IMPORT_HEADER_ALIASES, MAX_IMPORT_ROWS, STATUS_LABELS,
    criticalityLabel, environmentLabel, riskLabel, statusLabel
} from './assetConstants';
import {
    assetDedupKey, assetTargetLabel, isIPv4, isIPv6, normalizeDomain, parseAssetTarget,
    splitTags, validateAssetPayload
} from './assetTarget';

const FIELD_LIMITS = {
    host: 500, title: 500, domain: 253, protocol: 32, server: 255, country: 255,
    province: 255, city: 255, responsible_person: 255, department: 255, business_system: 255
};

function normalizeHeader(value) {
    return String(value ?? '')
        .replace(/^\uFEFF/, '')
        .trim()
        .toLowerCase()
        .replace(/[\s_\-/.（）()]+/g, '');
}

export function columnForHeader(value) {
    const normalized = normalizeHeader(value);
    if (!normalized) return '';
    return IMPORT_COLUMNS.find(column => (IMPORT_HEADER_ALIASES[column] || [])
        .some(alias => normalizeHeader(alias) === normalized)) || '';
}

/** RFC4180 风格 CSV 解析（支持引号、转义引号、CRLF/BOM）。 */
export function parseCsv(text) {
    const source = String(text ?? '').replace(/^\uFEFF/, '');
    const rows = [];
    let row = [];
    let field = '';
    let quoted = false;
    for (let index = 0; index < source.length; index += 1) {
        const char = source[index];
        if (quoted) {
            if (char === '"') {
                if (source[index + 1] === '"') {
                    field += '"';
                    index += 1;
                } else {
                    quoted = false;
                }
            } else {
                field += char;
            }
            continue;
        }
        if (char === '"') {
            quoted = true;
        } else if (char === ',') {
            row.push(field);
            field = '';
        } else if (char === '\n') {
            row.push(field);
            rows.push(row);
            row = [];
            field = '';
        } else if (char !== '\r') {
            field += char;
        }
    }
    if (field !== '' || row.length) {
        row.push(field);
        rows.push(row);
    }
    return rows.filter(columns => columns.some(value => String(value ?? '').trim() !== ''));
}

function localizedEnum(value, labels, fallback = '') {
    const raw = String(value ?? '').trim();
    if (!raw) return fallback;
    const lower = raw.toLowerCase();
    if (labels[lower]) return lower;
    const matched = Object.entries(labels).find(([, label]) => label === raw);
    return matched ? matched[0] : null;
}

function resolveProject(value, projects) {
    const raw = String(value ?? '').trim();
    if (!raw) return { projectId: '', projectName: '', error: '' };
    const lower = raw.toLowerCase();
    const project = (projects || []).find(item => String(item.id).toLowerCase() === lower
        || String(item.name || '').trim().toLowerCase() === lower);
    if (!project) return { projectId: '', projectName: '', error: `项目不存在或无权访问：${raw}` };
    return { projectId: project.id, projectName: project.name, error: '' };
}

function buildRecord(values, rowNumber, { projects, projectId, source }) {
    const fail = message => ({ rowNumber, values, asset: null, error: message });
    const target = values.target || values.host || values.domain || values.ip || '';
    if (!target) return fail('缺少目标地址');
    let parsed;
    try {
        parsed = parseAssetTarget(target);
    } catch (error) {
        return fail(error.message);
    }
    const rawPort = String(values.port ?? '').trim();
    const port = rawPort === '' ? Number(parsed.port || 0) : Number(rawPort);
    if (!Number.isInteger(port) || port < 0 || port > 65535) return fail('端口必须在 0–65535 之间');
    const ip = String(values.ip || parsed.ip || '').trim().toLowerCase();
    if (ip && !isIPv4(ip) && !isIPv6(ip)) return fail('IP 地址格式无效');
    const rawDomain = String(values.domain || parsed.domain || '').trim();
    const domain = rawDomain ? normalizeDomain(rawDomain) : '';
    if (rawDomain && !domain) return fail('域名格式无效');
    const protocol = String(values.protocol || parsed.protocol || '').trim().toLowerCase();
    if (protocol && !/^[a-z][a-z0-9+.-]{0,31}$/.test(protocol)) return fail('协议格式无效');
    const status = localizedEnum(values.status, STATUS_LABELS, 'active');
    if (!status) return fail('状态仅支持 active 或 inactive');
    const environment = localizedEnum(values.environment, ENVIRONMENT_LABELS, '');
    if (environment === null) return fail('环境值无效');
    const criticality = localizedEnum(values.criticality, CRITICALITY_LABELS, '');
    if (criticality === null) return fail('重要性值无效');
    const project = resolveProject(values.project || projectId, projects);
    if (project.error) return fail(project.error);
    if (projectId !== undefined && project.projectId !== projectId) return fail('该行的项目与当前资产范围不一致');
    const tags = splitTags(values.tags);
    if (tags.length > 30) return fail('标签最多 30 个');
    if (tags.some(tag => Array.from(tag).length > 64)) return fail('单个标签最多 64 个字符');
    const asset = {
        host: String(values.host || parsed.host || '').trim(),
        ip,
        domain,
        port,
        protocol,
        title: String(values.title || '').trim(),
        server: String(values.server || '').trim(),
        country: String(values.country || '').trim(),
        province: String(values.province || '').trim(),
        city: String(values.city || '').trim(),
        responsible_person: String(values.responsible_person || '').trim(),
        department: String(values.department || '').trim(),
        business_system: String(values.business_system || '').trim(),
        environment,
        criticality,
        status,
        tags,
        project_id: project.projectId,
        source: source || 'manual-import'
    };
    const oversized = Object.keys(FIELD_LIMITS)
        .find(key => Array.from(String(asset[key] || '')).length > FIELD_LIMITS[key]);
    if (oversized) return fail(`字段 ${oversized} 内容过长`);
    const invalid = validateAssetPayload(asset);
    if (invalid) return fail(invalid);
    return { rowNumber, values, asset, projectName: project.projectName, error: '' };
}

function markDuplicates(records) {
    const seen = new Map();
    return records.map(record => {
        if (record.error || !record.asset) return record;
        const key = assetDedupKey(record.asset);
        if (seen.has(key)) {
            return { ...record, error: `与第 ${seen.get(key)} 行重复` };
        }
        seen.set(key, record.rowNumber);
        return record;
    });
}

/** CSV 文本 → 导入行（含逐行校验）。 */
export function recordsFromCsv(text, options = {}) {
    const matrix = parseCsv(text);
    if (matrix.length < 2) throw new Error('文件中没有可导入的数据');
    const headers = matrix[0].map(columnForHeader);
    if (!headers.some(Boolean)) throw new Error('未识别到模板字段，请检查表头');
    const dataRows = matrix.slice(1);
    if (dataRows.length > MAX_IMPORT_ROWS) throw new Error(`数据不能超过 ${MAX_IMPORT_ROWS} 行`);
    const records = dataRows.map((row, index) => {
        const values = {};
        headers.forEach((column, columnIndex) => {
            if (column && values[column] == null) values[column] = String(row[columnIndex] ?? '').trim();
        });
        return buildRecord(values, index + 2, options);
    });
    return markDuplicates(records);
}

/** JSON 文本 → 导入行；支持 {assets:[...]}、数组与单个对象。 */
export function recordsFromJson(text, options = {}) {
    let parsed;
    try {
        parsed = JSON.parse(String(text ?? ''));
    } catch (error) {
        throw new Error('JSON 解析失败：' + error.message);
    }
    const list = Array.isArray(parsed) ? parsed : (Array.isArray(parsed?.assets) ? parsed.assets : [parsed]);
    const items = list.filter(item => item && typeof item === 'object');
    if (!items.length) throw new Error('文件中没有可导入的数据');
    if (items.length > MAX_IMPORT_ROWS) throw new Error(`数据不能超过 ${MAX_IMPORT_ROWS} 行`);
    const records = items.map((item, index) => {
        const values = {
            target: item.target || item.host || item.domain || item.ip || '',
            host: item.host, ip: item.ip, domain: item.domain, port: item.port,
            protocol: item.protocol, title: item.title, server: item.server,
            country: item.country, province: item.province, city: item.city,
            responsible_person: item.responsible_person, department: item.department,
            business_system: item.business_system, environment: item.environment,
            criticality: item.criticality, status: item.status,
            tags: Array.isArray(item.tags) ? item.tags.join(',') : item.tags,
            project: item.project_id || item.project || item.project_name
        };
        return buildRecord(values, index + 1, options);
    });
    return markDuplicates(records);
}

export function csvTemplate() {
    return `${IMPORT_COLUMNS.join(',')}\r\n`;
}

function csvCell(value) {
    const text = String(value ?? '');
    return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

function exportValue(asset, key) {
    if (key === 'target') return assetTargetLabel(asset);
    if (key === 'project') return asset.project_name || asset.project_id || '';
    if (key === 'tags') return (asset.tags || []).join(',');
    if (key === 'status') return statusLabel(asset.status);
    if (key === 'environment') return environmentLabel(asset.environment);
    if (key === 'criticality') return criticalityLabel(asset.criticality);
    if (key === 'risk_level') return riskLabel(asset.risk_level);
    if (key === 'vulnerability_count') return Number(asset.vulnerability_count || 0);
    return asset[key] ?? '';
}

export function assetsToCsv(assets) {
    const rows = [EXPORT_COLUMNS.map(([, label]) => csvCell(label)).join(',')];
    (assets || []).forEach(asset => {
        rows.push(EXPORT_COLUMNS.map(([key]) => csvCell(exportValue(asset, key))).join(','));
    });
    return `\uFEFF${rows.join('\r\n')}`;
}

/** 触发浏览器下载（仅创建临时 a 元素，不查询 DOM）。 */
export function downloadTextFile(filename, content, mime = 'text/csv;charset=utf-8') {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
}
