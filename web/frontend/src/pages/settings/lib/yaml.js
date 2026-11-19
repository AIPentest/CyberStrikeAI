/**
 * 极简 YAML 子集：只服务于「系统设置 → 原始配置」编辑框。
 *
 * 覆盖 config.yaml 实际用到的写法：块映射、块列表、行内注释、引号字符串、
 * 多行 `|` / `>` 文本块、以及 `[]` / `{}` 行内集合。不支持锚点、别名、多文档
 * 合并键——遇到直接报错并给出行号，避免静默丢字段。
 *
 * 解析语义对齐 Go 的 gopkg.in/yaml.v3（YAML 1.2 core schema）：
 * 只有 true/false 视为布尔，yes/no/on/off 保持字符串。
 */

export class YamlError extends Error {
    constructor(message, line) {
        super(line ? `第 ${line} 行：${message}` : message);
        this.name = 'YamlError';
        this.line = line || 0;
    }
}

/* ------------------------------------------------------------------ */
/* 序列化                                                              */
/* ------------------------------------------------------------------ */

const NEEDS_QUOTE = /^(?:[-?:,\[\]{}#&*!|>'"%@`]|.*[:\s]$|.*\s#|.*[\n\r\t])/;
const RESERVED = /^(?:true|false|null|~|yes|no|on|off|[-+]?(?:\d+|\d*\.\d+)(?:[eE][-+]?\d+)?)$/i;

function isPlain(value) {
    const text = String(value);
    if (text === '') return false;
    if (RESERVED.test(text) || /^[-+]?(?:\d+\.\d*|\.\d+|\d+)(?:[eE][-+]?\d+)?$/.test(text)) return false;
    if (NEEDS_QUOTE.test(text) || /^(?:\s)|:(?:\s|$)|[\r\n\t]/.test(text)) return false;
    if (/[,[\]{}]/.test(text)) return false;
    if (/^[!&*|>@`]/.test(text)) return false;
    return true;
}

function quote(value) {
    return JSON.stringify(String(value));
}

function keyText(key) {
    const text = String(key);
    return isPlain(text) ? text : quote(text);
}

function scalarText(value) {
    if (value === null || value === undefined) return 'null';
    if (typeof value === 'boolean') return value ? 'true' : 'false';
    if (typeof value === 'number') {
        if (!Number.isFinite(value)) return quote(String(value));
        if (Number.isInteger(value)) return String(value);
        return String(value);
    }
    const text = String(value);
    return isPlain(text) ? text : quote(text);
}

function isScalar(value) {
    return value === null || value === undefined || ['string', 'number', 'boolean'].includes(typeof value);
}

function dumpEntry(key, value, indent, lines) {
    const pad = ' '.repeat(indent);
    const name = keyText(key);
    if (isScalar(value)) {
        lines.push(`${pad}${name}: ${scalarText(value)}`);
        return;
    }
    if (Array.isArray(value)) {
        if (!value.length) { lines.push(`${pad}${name}: []`); return; }
        lines.push(`${pad}${name}:`);
        dumpArray(value, indent + 2, lines);
        return;
    }
    const entries = Object.entries(value);
    if (!entries.length) { lines.push(`${pad}${name}: {}`); return; }
    lines.push(`${pad}${name}:`);
    dumpMap(value, indent + 2, lines);
}

function dumpMap(value, indent, lines) {
    for (const [key, item] of Object.entries(value)) dumpEntry(key, item, indent, lines);
}

function dumpArrayItem(item, indent, lines) {
    const pad = ' '.repeat(indent);
    if (isScalar(item)) {
        lines.push(`${pad}- ${scalarText(item)}`);
        return;
    }
    if (Array.isArray(item)) {
        if (!item.length) { lines.push(`${pad}- []`); return; }
        lines.push(`${pad}-`);
        dumpArray(item, indent + 2, lines);
        return;
    }
    const entries = Object.entries(item);
    if (!entries.length) { lines.push(`${pad}- {}`); return; }
    const [firstKey, firstValue] = entries[0];
    const rest = entries.slice(1);
    if (isScalar(firstValue)) {
        lines.push(`${pad}- ${keyText(firstKey)}: ${scalarText(firstValue)}`);
    } else {
        lines.push(`${pad}-`);
        dumpEntry(firstKey, firstValue, indent + 2, lines);
    }
    for (const [key, value] of rest) dumpEntry(key, value, indent + 2, lines);
}

function dumpArray(value, indent, lines) {
    for (const item of value) dumpArrayItem(item, indent, lines);
}

/** 对象 → YAML 文本。空对象返回 `{}`，空数组返回 `[]`。 */
export function dumpYaml(value, { header } = {}) {
    const out = [];
    if (header) {
        for (const line of String(header).replace(/\r\n?/g, '\n').split('\n')) out.push(`# ${line}`.trimEnd());
        if (out.length) out.push('');
    }
    if (isScalar(value)) {
        out.push(scalarText(value));
        return `${out.join('\n')}\n`;
    }
    if (Array.isArray(value)) {
        if (!value.length) out.push('[]');
        else dumpArray(value, 0, out);
        return `${out.join('\n')}\n`;
    }
    if (!value || !Object.keys(value).length) {
        out.push('{}');
        return `${out.join('\n')}\n`;
    }
    dumpMap(value, 0, out);
    return `${out.join('\n')}\n`;
}

/* ------------------------------------------------------------------ */
/* 解析                                                                */
/* ------------------------------------------------------------------ */

function stripComment(text) {
    let quoteChar = '';
    for (let i = 0; i < text.length; i += 1) {
        const char = text[i];
        if (quoteChar) {
            if (char === '\\' && quoteChar === '"') { i += 1; continue; }
            if (char === quoteChar) quoteChar = '';
            continue;
        }
        if (char === '"' || char === "'") { quoteChar = char; continue; }
        if (char === '#' && (i === 0 || /\s/.test(text[i - 1]))) return text.slice(0, i).trimEnd();
    }
    return text.trimEnd();
}

function readQuoted(text, start, line) {
    const quoteChar = text[start];
    let out = '';
    let i = start + 1;
    while (i < text.length) {
        const char = text[i];
        if (quoteChar === "'") {
            if (char === "'") {
                if (text[i + 1] === "'") { out += "'"; i += 2; continue; }
                return { value: out, end: i + 1 };
            }
            out += char;
            i += 1;
            continue;
        }
        if (char === '\\') {
            const next = text[i + 1];
            const map = { n: '\n', r: '\r', t: '\t', '"': '"', '\\': '\\', '/': '/', '0': '\0', b: '\b', f: '\f' };
            if (next === 'u') {
                out += String.fromCharCode(parseInt(text.slice(i + 2, i + 6), 16));
                i += 6;
                continue;
            }
            if (next in map) { out += map[next]; i += 2; continue; }
            out += next || '';
            i += 2;
            continue;
        }
        if (char === '"') return { value: out, end: i + 1 };
        out += char;
        i += 1;
    }
    throw new YamlError('字符串引号未闭合', line);
}

function splitKey(content, line) {
    const text = content.trimEnd();
    if (text.startsWith('"') || text.startsWith("'")) {
        const quoted = readQuoted(text, 0, line);
        const rest = text.slice(quoted.end).trimStart();
        if (!rest.startsWith(':')) throw new YamlError('键名后缺少冒号', line);
        return { key: quoted.value, rest: stripComment(rest.slice(1).trim()) };
    }
    for (let i = 0; i < text.length; i += 1) {
        if (text[i] === ':' && (i + 1 >= text.length || /\s/.test(text[i + 1]))) {
            const key = text.slice(0, i).trim();
            if (!key) throw new YamlError('键名为空', line);
            if (/^[&*!|>@`]/.test(key)) throw new YamlError(`不支持的键名写法：${key}`, line);
            return { key, rest: stripComment(text.slice(i + 1).trim()) };
        }
    }
    throw new YamlError('缺少冒号，无法识别为键值', line);
}

function hasMappingColon(text, line) {
    if (text.startsWith('"') || text.startsWith("'")) {
        const { end } = readQuoted(text, 0, line);
        return text.slice(end).trimStart().startsWith(':');
    }
    if (/^[\[{]/.test(text)) return false;
    for (let i = 0; i < text.length; i += 1) {
        if (text[i] === ':' && (i + 1 >= text.length || /\s/.test(text[i + 1]))) return true;
    }
    return false;
}

function plainScalar(text) {
    const value = text.trim();
    if (value === '' || value === '~' || /^null$/i.test(value)) return null;
    if (/^true$/i.test(value)) return true;
    if (/^false$/i.test(value)) return false;
    if (/^[-+]?\d+$/.test(value) && value.length < 16) return Number(value);
    if (/^[-+]?(?:\d+\.\d*|\.\d+|\d+)(?:[eE][-+]?\d+)?$/.test(value)) return Number(value);
    return value;
}

function parseFlow(text, start, line) {
    let i = start;
    const skip = () => { while (i < text.length && /\s/.test(text[i])) i += 1; };
    const read = () => {
        skip();
        const char = text[i];
        if (char === '[' || char === '{') {
            const open = char;
            const close = open === '[' ? ']' : '}';
            i += 1;
            const isArray = open === '[';
            const out = isArray ? [] : {};
            for (;;) {
                skip();
                if (i >= text.length) throw new YamlError('行内集合未闭合', line);
                if (text[i] === close) { i += 1; return out; }
                if (isArray) {
                    out.push(read());
                } else {
                    const key = read();
                    skip();
                    if (text[i] !== ':') throw new YamlError('行内对象缺少冒号', line);
                    i += 1;
                    out[String(key)] = read();
                }
                skip();
                if (text[i] === ',') { i += 1; continue; }
                if (text[i] === close) { i += 1; return out; }
                throw new YamlError('行内集合分隔符不正确', line);
            }
        }
        if (char === '"' || char === "'") {
            const quoted = readQuoted(text, i, line);
            i = quoted.end;
            return quoted.value;
        }
        let end = i;
        while (end < text.length && !/[,\]}]/.test(text[end])) {
            if (text[end] === ':' && /[\s,\]}]/.test(text[end + 1] || '')) break;
            end += 1;
        }
        const raw = text.slice(i, end).trim();
        i = end;
        return plainScalar(raw);
    };
    const value = read();
    skip();
    if (i < text.length) {
        throw new YamlError('行内值后存在多余内容', line);
    }
    return value;
}

function parseValueText(text, line) {
    const value = text.trim();
    if (!value) return null;
    if (value.startsWith('[') || value.startsWith('{')) return parseFlow(value, 0, line);
    if (value.startsWith('"') || value.startsWith("'")) {
        const quoted = readQuoted(value, 0, line);
        if (value.slice(quoted.end).trim()) throw new YamlError('引号字符串后存在多余内容', line);
        return quoted.value;
    }
    if (/^[&*]/.test(value)) throw new YamlError('不支持锚点/别名', line);
    return plainScalar(value);
}

function isBlockHeader(text) {
    return /^[|>][-+0-9]*$/.test(text.trim());
}

function readBlockString(state, parentIndent, header, headerLine) {
    const style = header.trim()[0];
    const chomp = header.includes('-') ? 'strip' : header.includes('+') ? 'keep' : 'clip';
    const raws = state.rawLines;
    const collected = [];
    let blockIndent = null;
    let lastConsumed = headerLine;
    for (let j = headerLine; j < raws.length; j += 1) {
        const raw = raws[j];
        if (raw.trim() === '') {
            collected.push('');
            lastConsumed = j + 1;
            continue;
        }
        const indent = /^[ \t]*/.exec(raw)[0].length;
        if (indent <= parentIndent) break;
        if (blockIndent === null) blockIndent = indent;
        collected.push(' '.repeat(Math.max(0, indent - blockIndent)) + raw.slice(indent));
        lastConsumed = j + 1;
    }
    while (state.i < state.lines.length && state.lines[state.i].line <= lastConsumed) state.i += 1;
    if (!collected.length) return '';
    const raw = collected;
    let body;
    if (style === '|') {
        body = raw.join('\n');
    } else {
        // 折叠：相邻非空行合并为空格，空行保留为段落分隔（空行数 = 换行数）。
        body = raw[0] ?? '';
        for (let i = 1; i < raw.length; i += 1) {
            body += raw[i] === '' || raw[i - 1] === '' ? `\n${raw[i]}` : ` ${raw[i]}`;
        }
    }
    const trimmed = body.replace(/\n+$/, '');
    if (chomp === 'strip') return trimmed;
    if (chomp === 'keep') return body ? `${body}\n` : '';
    return trimmed ? `${trimmed}\n` : '';
}

function parseBlock(state, indent) {
    const line = state.lines[state.i];
    if (!line) return null;
    if (line.content === '-' || line.content.startsWith('- ')) return parseSequence(state, indent);
    return parseMapping(state, indent);
}

function parseMapping(state, indent, seed = null) {
    const map = seed || {};
    while (state.i < state.lines.length) {
        const ln = state.lines[state.i];
        if (ln.indent < indent) break;
        if (ln.indent > indent) throw new YamlError('缩进层级不一致', ln.line);
        if (ln.content === '-' || ln.content.startsWith('- ')) {
            throw new YamlError('同一层级不能混用列表与键值', ln.line);
        }
        const pair = splitKey(ln.content, ln.line);
        state.i += 1;
        map[pair.key] = parseAfterKey(state, indent, pair.rest, ln.line);
    }
    return map;
}

function parseAfterKey(state, indent, rest, line) {
    if (rest === '') {
        const next = state.lines[state.i];
        if (next && next.indent > indent) return parseBlock(state, next.indent);
        return null;
    }
    if (isBlockHeader(rest)) return readBlockString(state, indent, rest, line);
    return parseValueText(rest, line);
}

function parseSequence(state, indent) {
    const list = [];
    while (state.i < state.lines.length) {
        const ln = state.lines[state.i];
        if (ln.indent !== indent) {
            if (ln.indent > indent) throw new YamlError('缩进层级不一致', ln.line);
            break;
        }
        if (!(ln.content === '-' || ln.content.startsWith('- '))) break;
        const afterDash = ln.content === '-' ? '' : ln.content.slice(1).trimStart();
        const contentIndent = indent + (ln.content.length - afterDash.length);
        state.i += 1;
        if (afterDash === '') {
            const next = state.lines[state.i];
            if (next && next.indent > indent) list.push(parseBlock(state, next.indent));
            else list.push(null);
            continue;
        }
        if (hasMappingColon(afterDash, ln.line) && !/^[\[{]/.test(afterDash)) {
            const pair = splitKey(afterDash, ln.line);
            const map = {};
            map[pair.key] = parseAfterKey(state, contentIndent, pair.rest, ln.line);
            parseMapping(state, contentIndent, map);
            list.push(map);
            continue;
        }
        if (isBlockHeader(afterDash)) {
            list.push(readBlockString(state, indent, afterDash, ln.line));
            continue;
        }
        list.push(parseValueText(afterDash, ln.line));
    }
    return list;
}

/** YAML 文本 → JS 值。空文档返回 null；语法问题抛 YamlError（含行号）。 */
export function parseYaml(text) {
    const source = String(text ?? '').replace(/\r\n?/g, '\n');
    const rawLines = source.split('\n');
    const lines = [];
    rawLines.forEach((raw, index) => {
        if (/^\s*$/.test(raw)) return;
        if (/^\s*#/.test(raw)) return;
        if (/^\s*(---|\.\.\.)\s*(#.*)?$/.test(raw)) return;
        const lead = /^[ \t]*/.exec(raw)[0];
        if (lead.includes('\t')) throw new YamlError('缩进不能使用制表符', index + 1);
        lines.push({ indent: lead.length, content: raw.slice(lead.length), line: index + 1 });
    });
    if (!lines.length) return null;
    const state = { lines, rawLines, i: 0 };
    const value = parseBlock(state, lines[0].indent);
    if (state.i < lines.length) {
        throw new YamlError('存在无法归属的缩进内容', state.lines[state.i].line);
    }
    return value;
}
