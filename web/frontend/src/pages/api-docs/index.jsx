import { useEffect, useMemo, useState } from 'react';
import {
    Button, Card, Divider, Input, Select, Space, Tabs, Tag, TextArea, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch, IconSend } from '@douyinfe/semi-icons';
import { api, errorMessage, request } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { PageHeader, SectionCard, JsonViewer, toastError } from '../../components/ui';
import './page.css';

const { Text, Paragraph } = Typography;

const METHOD_COLOR = {
    get: 'blue', post: 'green', put: 'orange', delete: 'red', patch: 'violet', head: 'grey', options: 'grey'
};

function buildOperations(spec) {
    const paths = spec?.paths || {};
    const operations = [];
    Object.entries(paths).forEach(([path, methods]) => {
        Object.entries(methods || {}).forEach(([method, op]) => {
            if (!op || typeof op !== 'object') return;
            operations.push({
                id: `${method.toUpperCase()} ${path}`,
                method: method.toUpperCase(),
                path,
                summary: op.summary || op.description || '',
                tag: (op.tags && op.tags[0]) || 'default',
                operation: op
            });
        });
    });
    return operations.sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method));
}

function pathParamNames(path) {
    return Array.from(path.matchAll(/\{([^}]+)\}/g)).map(match => match[1]);
}

function TryPanel({ operation, spec }) {
    const op = operation.operation || {};
    const params = op.parameters || [];
    const pathParams = pathParamNames(operation.path);
    const [values, setValues] = useState({});
    const [body, setBody] = useState('');
    const [result, setResult] = useState(null);
    const [sending, setSending] = useState(false);

    useEffect(() => {
        const next = {};
        pathParams.forEach(name => {
            const declared = params.find(item => item.name === name && item.in === 'path');
            next[name] = declared?.schema?.default ?? declared?.example ?? '';
        });
        params.filter(item => item.in === 'query').forEach(item => {
            if (item.schema?.default !== undefined) next[`query:${item.name}`] = String(item.schema.default);
        });
        setValues(next);
        setBody(op.requestBody ? '{\n  \n}' : '');
        setResult(null);
    }, [operation.id]);

    const send = async () => {
        setSending(true);
        setResult(null);
        try {
            if (pathParams.some(name => String(values[name] ?? '').trim() === '')) {
                toastError('请填写路径参数');
                return;
            }
            let path = operation.path;
            pathParams.forEach(name => { path = path.replace(`{${name}}`, encodeURIComponent(values[name] ?? '')); });
            const query = {};
            params.filter(item => item.in === 'query').forEach(item => {
                const value = values[`query:${item.name}`];
                if (value !== undefined && value !== '') query[item.name] = value;
            });
            const options = { method: operation.method, query, raw: true, timeout: 120000 };
            if (op.requestBody && body.trim()) {
                try {
                    options.body = JSON.parse(body);
                } catch (error) {
                    toastError('请求体不是合法 JSON');
                    setSending(false);
                    return;
                }
            }
            if (!path.startsWith('/') || path.startsWith('//')) throw new Error('接口地址必须为站内 API 路径');
            const response = await request(path.startsWith('/api/') ? path.slice(4) : path, options);
            const contentType = response.headers.get('content-type') || '';
            let payload;
            if (contentType.includes('application/json')) payload = await response.json();
            else payload = await response.text();
            setResult({ ok: true, status: response.status, payload });
        } catch (error) {
            setResult({ ok: false, status: error.status || 0, payload: errorMessage(error) });
        } finally {
            setSending(false);
        }
    };

    return (
        <div className="api-docs-try">
            <Divider margin="12px 0" />
            <Text strong>在线调试</Text>
            {pathParams.length > 0 && (
                <div className="api-docs-try-fields">
                    {pathParams.map(name => (
                        <label key={name} className="api-docs-try-field">
                            <span>路径参数 {name}</span>
                            <Input size="small" value={values[name] ?? ''} onChange={next => setValues(current => ({ ...current, [name]: next }))} />
                        </label>
                    ))}
                </div>
            )}
            {params.filter(item => item.in === 'query').length > 0 && (
                <div className="api-docs-try-fields">
                    {params.filter(item => item.in === 'query').map(item => (
                        <label key={item.name} className="api-docs-try-field">
                            <span>查询参数 {item.name}{item.required ? ' *' : ''}</span>
                            <Input
                                size="small"
                                value={values[`query:${item.name}`] ?? ''}
                                onChange={next => setValues(current => ({ ...current, [`query:${item.name}`]: next }))}
                                placeholder={item.description || ''}
                            />
                        </label>
                    ))}
                </div>
            )}
            {op.requestBody && (
                <div className="api-docs-try-body">
                    <span>请求体（JSON）</span>
                    <TextArea value={body} onChange={setBody} autosize={{ minRows: 5, maxRows: 14 }} />
                </div>
            )}
            <Space>
                <Button theme="solid" type="primary" icon={<IconSend />} loading={sending} onClick={send}>发送请求</Button>
                <Button onClick={() => setResult(null)}>清空结果</Button>
            </Space>
            {result && (
                <div className="api-docs-try-result">
                    <Tag color={result.ok ? 'green' : 'red'}>{result.ok ? `HTTP ${result.status}` : `失败 ${result.status || ''}`}</Tag>
                    <JsonViewer value={result.payload} title="响应" />
                </div>
            )}
        </div>
    );
}

export default function ApiDocsPage() {
    const t = useT();
    const [spec, setSpec] = useState(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [keyword, setKeyword] = useState('');
    const [method, setMethod] = useState('all');
    const [selectedId, setSelectedId] = useState(null);

    const load = async () => {
        setLoading(true);
        setError(null);
        try {
            const payload = await api.get('/openapi/spec');
            setSpec(payload);
        } catch (err) {
            setError(err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => { load(); }, []);

    const operations = useMemo(() => buildOperations(spec), [spec]);
    const filtered = useMemo(() => operations.filter(op => {
        if (method !== 'all' && op.method !== method) return false;
        if (!keyword) return true;
        const needle = keyword.toLowerCase();
        return op.path.toLowerCase().includes(needle) || op.summary.toLowerCase().includes(needle) || op.method.toLowerCase().includes(needle);
    }), [operations, keyword, method]);

    const selected = filtered.find(op => op.id === selectedId) || filtered[0] || null;
    const grouped = useMemo(() => {
        const map = new Map();
        filtered.forEach(op => {
            if (!map.has(op.tag)) map.set(op.tag, []);
            map.get(op.tag).push(op);
        });
        return [...map.entries()];
    }, [filtered]);

    return (
        <div className="api-docs-page">
            <PageHeader
                title={t('apiDocs.title', null, 'API 文档')}
                description={spec?.info?.description || 'CyberStrikeAI 开放接口'}
                extra={<Button icon={<IconRefresh />} onClick={load} loading={loading}>重新加载</Button>}
            />
            <div className="api-docs-layout">
                <Card className="api-docs-sidebar" bordered={false}>
                    <Space vertical style={{ width: '100%' }} spacing={8}>
                        <Input prefix={<IconSearch />} value={keyword} onChange={setKeyword} placeholder="搜索路径或说明" />
                        <Select
                            value={method}
                            onChange={setMethod}
                            style={{ width: '100%' }}
                            optionList={[
                                { value: 'all', label: '全部方法' },
                                ...['GET', 'POST', 'PUT', 'DELETE', 'PATCH'].map(item => ({ value: item, label: item }))
                            ]}
                        />
                    </Space>
                    <div className="api-docs-list">
                        {grouped.map(([tag, items]) => (
                            <div className="api-docs-group" key={tag}>
                                <Text type="tertiary" className="api-docs-group-title">{tag}</Text>
                                {items.map(op => (
                                    <button
                                        type="button"
                                        key={op.id}
                                        className={`api-docs-item ${selected?.id === op.id ? 'is-active' : ''}`}
                                        onClick={() => setSelectedId(op.id)}
                                    >
                                        <Tag size="small" color={METHOD_COLOR[op.method.toLowerCase()]}>{op.method}</Tag>
                                        <span className="api-docs-item-path">{op.path}</span>
                                    </button>
                                ))}
                            </div>
                        ))}
                        {!loading && grouped.length === 0 && <Text type="tertiary">没有匹配的接口</Text>}
                    </div>
                </Card>
                <div className="api-docs-detail">
                    {error && <Card bordered={false}><Text type="danger">{errorMessage(error)}</Text></Card>}
                    {selected ? (
                        <Card bordered={false}>
                            <div className="api-docs-detail-head">
                                <Tag color={METHOD_COLOR[selected.method.toLowerCase()]}>{selected.method}</Tag>
                                <code className="api-docs-detail-path">{selected.path}</code>
                            </div>
                            <Paragraph>{selected.summary || '无说明'}</Paragraph>
                            <Tabs type="line">
                                <Tabs.TabPane itemKey="params" tab="参数">
                                    <JsonViewer value={selected.operation.parameters || []} title="Request Parameters" collapsed />
                                </Tabs.TabPane>
                                <Tabs.TabPane itemKey="body" tab="请求体">
                                    <JsonViewer value={selected.operation.requestBody || {}} title="Request Body" collapsed />
                                </Tabs.TabPane>
                                <Tabs.TabPane itemKey="responses" tab="响应">
                                    <JsonViewer value={selected.operation.responses || {}} title="Responses" collapsed />
                                </Tabs.TabPane>
                            </Tabs>
                            <TryPanel operation={selected} spec={spec} />
                        </Card>
                    ) : (
                        <Card bordered={false}><Text type="tertiary">{loading ? '加载中…' : '选择左侧接口查看详情'}</Text></Card>
                    )}
                </div>
            </div>
        </div>
    );
}
