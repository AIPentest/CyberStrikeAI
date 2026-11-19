/** 信息收集 provider 配置：默认字段、字段模板与查询示例（与后端 fofa.go 对齐）。 */
export const INFO_PROVIDERS = {
    fofa: {
        label: 'FOFA',
        maxSize: 10000,
        defaultFields: 'host,ip,port,domain,title,protocol,country,region,city,server',
        placeholder: 'app="Apache" && country="CN"',
        fieldPresets: [
            ['最小字段', 'host,ip,port,domain'],
            ['Web 常用', 'host,title,ip,port,domain,protocol,server,icp,country,region,city'],
            ['情报增强', 'host,ip,port,domain,title,protocol,country,region,city,server,asn,org,icp,header,banner']
        ],
        queryPresets: [
            ['Apache + 中国', 'app="Apache" && country="CN"'],
            ['登录页 + 中国', 'title="登录" && country="CN"'],
            ['指定域名', 'domain="example.com"'],
            ['指定 IP', 'ip="1.1.1.1"']
        ],
        supportsFull: true
    },
    zoomeye: {
        label: 'ZoomEye',
        maxSize: 10000,
        defaultFields: 'ip,port,domain,hostname,title,service,app,country,city',
        placeholder: 'app="Apache" && country="CN"',
        fieldPresets: [
            ['最小字段', 'ip,port,domain,hostname'],
            ['Web 常用', 'ip,port,domain,hostname,title,service,app,country,city'],
            ['情报增强', 'ip,port,domain,hostname,title,service,app,country,city,org,isp,ssl']
        ],
        queryPresets: [
            ['Apache + 中国', 'app="Apache" && country="CN"'],
            ['SSH 服务', 'service="ssh"'],
            ['指定域名', 'domain="example.com"'],
            ['指定 IP', 'ip="1.1.1.1"']
        ],
        supportsFull: false
    },
    quake: {
        label: 'Quake',
        maxSize: 10000,
        defaultFields: 'ip,port,domain,service.name,service.http.title,location.country_cn,location.province_cn,location.city_cn',
        placeholder: 'service.name:"http" AND country_cn:"中国"',
        fieldPresets: [
            ['最小字段', 'ip,port,domain'],
            ['Web 常用', 'ip,port,domain,service.name,service.http.title,location.country_cn,location.city_cn'],
            ['情报增强', 'ip,port,domain,service.name,service.http.title,service.http.server,location.country_cn,location.province_cn,location.city_cn,asn']
        ],
        queryPresets: [
            ['HTTP + 中国', 'service.name:"http" AND country_cn:"中国"'],
            ['443 端口', 'port:443'],
            ['指定域名', 'domain:"example.com"'],
            ['指定 IP', 'ip:"1.1.1.1"']
        ],
        supportsFull: true
    },
    shodan: {
        label: 'Shodan',
        maxSize: 1000,
        defaultFields: 'ip_str,port,hostnames,domains,org,isp,location.country_name,location.city,product,transport',
        placeholder: 'product:nginx country:CN',
        fieldPresets: [
            ['最小字段', 'ip_str,port,hostnames,domains'],
            ['Web 常用', 'ip_str,port,hostnames,domains,product,org,location.country_name,location.city'],
            ['情报增强', 'ip_str,port,hostnames,domains,org,isp,asn,location.country_name,location.city,product,transport,ssl.cert.subject.cn']
        ],
        queryPresets: [
            ['Nginx + 中国', 'product:nginx country:CN'],
            ['SSH 服务', 'port:22'],
            ['证书域名', 'ssl.cert.subject.cn:example.com'],
            ['Amazon 443', 'org:"Amazon" port:443']
        ],
        supportsFull: false
    }
};

export const PROVIDER_OPTIONS = Object.entries(INFO_PROVIDERS)
    .map(([value, config]) => ({ value, label: config.label }));

export function providerConfig(provider) {
    return INFO_PROVIDERS[provider] || INFO_PROVIDERS.fofa;
}

export function splitFields(raw) {
    return String(raw || '')
        .split(',')
        .map(field => field.trim())
        .filter(Boolean);
}

export function defaultVisibleFields(fields, limit = 5) {
    const preferred = ['host', 'ip', 'port', 'domain', 'title', 'protocol', 'server', 'country', 'city'];
    const picked = preferred.filter(field => fields.includes(field));
    const rest = fields.filter(field => !picked.includes(field));
    return [...picked, ...rest].slice(0, limit);
}

/** 从结果行推断目标地址（host/域名/IP）。 */
export function inferTarget(row, fields) {
    if (!row || typeof row !== 'object') return '';
    const pick = names => {
        for (const name of names) {
            const value = row[name];
            if (Array.isArray(value) && value.length) return String(value[0]);
            if (value) return String(value);
        }
        return '';
    };
    const host = pick(['host', 'url', 'hostname']);
    if (host) return host;
    const domain = pick(['domain', 'domains', 'hostnames']);
    if (domain) return domain;
    const ip = pick(['ip', 'ip_str']);
    const port = row.port;
    if (ip) return port ? `${ip}:${port}` : ip;
    return '';
}

export function rowsForIndexes(results, indexes) {
    const set = new Set(indexes.map(Number));
    return (results || []).filter((_, index) => set.has(index));
}

export const FOFA_RESULT_FIELD_LIMIT = 5;
