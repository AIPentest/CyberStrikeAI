import { useEffect, useMemo, useState } from 'react';
import {
    Button, Card, Input, Select, Space, Tabs, Tag, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api, errorMessage } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { PageHeader, JsonViewer } from '../../components/ui';
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
            if (!Object.hasOwn(METHOD_COLOR, method) || !op) return;
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
                extra={(
                    <Space wrap>
                        <Button icon={<IconRefresh />} onClick={load} loading={loading}>{t('common.refresh', null, '刷新')}</Button>
                        <Text link={{ href: '/api-docs' }}>{t('apiDocs.openConsole', null, '打开 API 控制台')}</Text>
                    </Space>
                )}
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
                        </Card>
                    ) : (
                        <Card bordered={false}><Text type="tertiary">{loading ? '加载中…' : '选择左侧接口查看详情'}</Text></Card>
                    )}
                </div>
            </div>
        </div>
    );
}
