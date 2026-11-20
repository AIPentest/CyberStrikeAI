/**
 * 资产共用常量：枚举文案、筛选项、导入表头别名。
 * 仅被 asset-overview / asset-library / info-collect 三个页面目录引用。
 */

export const ASSET_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];
export const ASSET_DEFAULT_PAGE_SIZE = 20;

export const STATUS_LABELS = { active: '活跃', inactive: '停用' };

export const STATUS_OPTIONS = [
    { value: 'active', label: STATUS_LABELS.active },
    { value: 'inactive', label: STATUS_LABELS.inactive }
];

export const ENVIRONMENT_LABELS = {
    production: '生产',
    staging: '预发布',
    testing: '测试',
    development: '开发',
    other: '其他'
};

export const ENVIRONMENT_OPTIONS = Object.entries(ENVIRONMENT_LABELS)
    .map(([value, label]) => ({ value, label }));

export const CRITICALITY_LABELS = {
    critical: '核心',
    high: '重要',
    medium: '一般',
    low: '低'
};

export const CRITICALITY_OPTIONS = Object.entries(CRITICALITY_LABELS)
    .map(([value, label]) => ({ value, label }));

export const RISK_LABELS = {
    critical: '严重',
    high: '高危',
    medium: '中危',
    low: '低危',
    info: '提示',
    normal: '正常',
    unassessed: '未评估'
};

export const RISK_COLORS = {
    critical: 'red',
    high: 'orange',
    medium: 'amber',
    low: 'blue',
    info: 'grey',
    normal: 'green',
    unassessed: 'grey'
};

export const RISK_OPTIONS = Object.entries(RISK_LABELS)
    .map(([value, label]) => ({ value, label }));

export function riskLevelOf(asset) {
    const level = String(asset?.risk_level || '').toLowerCase();
    return RISK_LABELS[level] ? level : 'unassessed';
}

export function riskLabel(level) {
    return RISK_LABELS[String(level || '').toLowerCase()] || RISK_LABELS.unassessed;
}

/** 常见协议（FOFA/资产库里的 protocol 字段值）。 */
export const PROTOCOL_OPTIONS = [
    'http', 'https', 'ssh', 'ftp', 'smtp', 'dns', 'ldap', 'smb',
    'telnet', 'rdp', 'mysql', 'postgresql', 'mssql', 'redis', 'mongodb'
].map(value => ({ value, label: value }));

export const SORT_OPTIONS = [
    { value: 'last_seen_at:desc', label: '最近发现（新到旧）' },
    { value: 'last_seen_at:asc', label: '最近发现（旧到新）' },
    { value: 'last_scan_at:asc', label: '最久未扫描优先' },
    { value: 'last_scan_at:desc', label: '最近扫描优先' },
    { value: 'risk_level:desc', label: '风险从高到低' },
    { value: 'vulnerability_count:desc', label: '漏洞数量从多到少' },
    { value: 'first_seen_at:desc', label: '首次发现（新到旧）' },
    { value: 'host:asc', label: '目标名称' },
    { value: 'port:asc', label: '端口' }
];

/** 扫描状态筛选：never / scanned / overdue:N，对应 handler 的 scan_state 与 scan_overdue_days。 */
export const SCAN_STATE_OPTIONS = [
    { value: '', label: '全部' },
    { value: 'never', label: '从未扫描' },
    { value: 'scanned', label: '已扫描' },
    { value: 'overdue:30', label: '30 天未扫描' },
    { value: 'overdue:60', label: '60 天未扫描' },
    { value: 'overdue:90', label: '90 天未扫描' }
];

export function applyScanStateParam(query, value) {
    const raw = String(value || '');
    if (!raw) return query;
    const overdue = raw.match(/^overdue:(\d+)$/);
    if (overdue) return { ...query, scan_overdue_days: overdue[1] };
    return { ...query, scan_state: raw };
}

/** 批量导入列（与旧版模板一致）。 */
export const IMPORT_COLUMNS = [
    'target', 'project', 'tags', 'host', 'ip', 'domain', 'port', 'protocol', 'title',
    'server', 'country', 'province', 'city', 'responsible_person', 'department',
    'business_system', 'environment', 'criticality', 'status'
];

export const IMPORT_HEADER_ALIASES = {
    target: ['target', 'asset', 'assetaddress', '目标', '资产', '资产地址'],
    project: ['project', 'projectname', 'projectid', '项目', '项目名称', '项目id', '所属项目'],
    tags: ['tags', 'tag', '标签'],
    host: ['host', 'url', '完整url', '完整urlhost', '主机'],
    ip: ['ip', 'ipaddress', 'ip地址'],
    domain: ['domain', '域名'],
    port: ['port', '端口'],
    protocol: ['protocol', 'scheme', '协议'],
    title: ['title', 'pagetitle', '标题', '页面标题', '标题指纹'],
    server: ['server', 'service', 'product', '服务', '产品', '服务指纹'],
    country: ['country', 'countryregion', '国家', '国家地区'],
    province: ['province', 'state', '省份', '省份州', '州'],
    city: ['city', '城市'],
    responsible_person: ['responsibleperson', 'owner', '负责人', '责任人'],
    department: ['department', '部门'],
    business_system: ['businesssystem', 'system', '业务系统', '系统'],
    environment: ['environment', 'env', '环境'],
    source: ['source', '来源'],
    criticality: ['criticality', 'importance', '重要性', '重要等级'],
    status: ['status', '状态']
};

export const MAX_IMPORT_ROWS = 100000;

/** 导出列与表头（CSV 全字段）。 */
export const EXPORT_COLUMNS = [
    ['target', '目标'], ['host', '完整 URL / Host'], ['ip', 'IP 地址'], ['domain', '域名'],
    ['port', '端口'], ['protocol', '协议'], ['title', '标题/指纹'], ['server', '服务指纹'],
    ['project', '所属项目'], ['responsible_person', '负责人'], ['department', '部门'],
    ['business_system', '业务系统'], ['environment', '环境'], ['criticality', '重要性'],
    ['country', '国家/地区'], ['province', '省份/州'], ['city', '城市'], ['source', '来源'],
    ['status', '状态'], ['tags', '标签'], ['risk_level', '风险等级'],
    ['vulnerability_count', '漏洞数量'], ['first_seen_at', '首次发现'],
    ['last_seen_at', '最近发现'], ['last_scan_at', '上次扫描']
];

export function environmentLabel(value) {
    return ENVIRONMENT_LABELS[value] || value || '';
}

export function criticalityLabel(value) {
    return CRITICALITY_LABELS[value] || value || '';
}

export function statusLabel(value) {
    return STATUS_LABELS[value] || value || '';
}
