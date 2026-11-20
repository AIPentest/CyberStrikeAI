/** 调用拦截页的工具函数：配置归一化、字节长度、规则校验与草稿比较。 */

export const MAX_RULES = 100;
const NAME_LIMIT = 200;
const TEXT_LIMIT = 4096;

export function clone(value) {
    return JSON.parse(JSON.stringify(value));
}

export function normalizeConfig(raw) {
    if (!raw || typeof raw.enabled !== 'boolean' || (raw.rules != null && !Array.isArray(raw.rules))) {
        throw new Error('服务端返回的配置格式不正确');
    }
    return {
        enabled: raw.enabled,
        rules: (raw.rules || []).map(rule => ({
            id: String(rule.id || ''),
            name: String(rule.name || ''),
            enabled: Boolean(rule.enabled),
            pattern: String(rule.pattern || ''),
            message: String(rule.message || '')
        }))
    };
}

export function ruleKey(rule, index) {
    return rule.id || `rule-${index}`;
}

export function newRuleId() {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
        return crypto.randomUUID();
    }
    return `rule-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function byteLength(text) {
    return new TextEncoder().encode(String(text || '')).length;
}

/** 返回 null 表示通过，否则返回 { index, field, message }。 */
export function validateRules(config) {
    if (!config) return { index: -1, field: '', message: '请先加载拦截配置' };
    if (config.rules.length > MAX_RULES) {
        return { index: -1, field: '', message: `最多可配置 ${MAX_RULES} 条拦截规则` };
    }
    for (let index = 0; index < config.rules.length; index += 1) {
        const rule = config.rules[index];
        if (!rule.name.trim()) return { index, field: 'name', message: `第 ${index + 1} 条规则缺少名称` };
        if (!rule.pattern.trim()) return { index, field: 'pattern', message: `第 ${index + 1} 条规则缺少匹配正则` };
        if (byteLength(rule.name) > NAME_LIMIT) {
            return { index, field: 'name', message: `第 ${index + 1} 条规则名称超过 ${NAME_LIMIT} 字节` };
        }
        if (byteLength(rule.pattern) > TEXT_LIMIT) {
            return { index, field: 'pattern', message: `第 ${index + 1} 条规则正则超过 ${TEXT_LIMIT} 字节` };
        }
        if (byteLength(rule.message) > TEXT_LIMIT) {
            return { index, field: 'message', message: `第 ${index + 1} 条规则提醒超过 ${TEXT_LIMIT} 字节` };
        }
    }
    return null;
}

export function isDirty(config, saved) {
    if (!config || !saved) return false;
    return JSON.stringify(config) !== JSON.stringify(saved);
}

export function requestConfig(config) {
    return {
        enabled: Boolean(config?.enabled),
        rules: (config?.rules || []).map(rule => ({
            id: rule.id,
            name: rule.name,
            enabled: Boolean(rule.enabled),
            pattern: rule.pattern,
            message: rule.message
        }))
    };
}

/** 单条规则校验时使用：忽略总开关与规则启停，只看这一条。 */
export function singleRuleConfig(rule) {
    return {
        enabled: true,
        rules: [{ id: rule.id, name: rule.name, enabled: true, pattern: rule.pattern, message: rule.message }]
    };
}
