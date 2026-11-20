import { useState } from 'react';
import { Button, Input, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, JsonViewer, KeyValueList, PageHeader, SectionCard, confirmAction, toastError, toastSuccess, Drawer } from '../../components/ui';
import { formatDateTime } from '../../lib/format';

const { Text } = Typography;

export default function KnowledgeRetrievalLogsPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canDelete = hasPermission('knowledge:delete');
    const [conversationId, setConversationId] = useState('');
    const [limit, setLimit] = useState(50);
    const [detail, setDetail] = useState(null);

    const logs = useResource(
        () => api.get('/knowledge/retrieval-logs', { conversationId: conversationId || undefined, limit }),
        [conversationId, limit]
    );
    const rows = Array.isArray(logs.data?.logs) ? logs.data.logs : [];

    const remove = row => confirmAction({
        title: '删除该检索日志？',
        content: row.query || row.id,
        onOk: async () => {
            await api.del(`/knowledge/retrieval-logs/${encodeURIComponent(row.id)}`);
            toastSuccess('已删除');
            logs.reload();
        }
    });

    const columns = [
        {
            title: '检索语句', dataIndex: 'query', render: (value, record) => (
                <button type="button" className="link-button" onClick={() => setDetail(record)} style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', textAlign: 'left', color: 'var(--semi-color-primary)' }}>
                    {value || '—'}
                </button>
            )
        },
        { title: '风险类型', dataIndex: 'riskType', width: 140, render: value => (value ? <Tag size="small">{value}</Tag> : '—') },
        {
            title: '命中知识项', dataIndex: 'retrievedItems', width: 120,
            render: value => <Text type="tertiary">{Array.isArray(value) ? value.length : 0} 条</Text>
        },
        { title: '会话', dataIndex: 'conversationId', width: 200, render: value => <Text type="tertiary" ellipsis={{ showTooltip: true }}>{value || '—'}</Text> },
        { title: '时间', dataIndex: 'createdAt', width: 180, render: value => formatDateTime(value) },
        {
            title: '操作', width: 110, render: (_, record) => (
                <Space>
                    <Button size="small" theme="borderless" onClick={() => setDetail(record)}>详情</Button>
                    {canDelete && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => remove(record)} />}
                </Space>
            )
        }
    ];

    return (
        <div>
            <PageHeader
                title={t('knowledge.retrievalLogsTitle', null, '检索历史')}
                description="记录智能体每次知识库检索的语句、命中项与会话，用于回溯回答依据。"
                extra={<Button icon={<IconRefresh />} onClick={() => logs.reload()} loading={logs.loading}>刷新</Button>}
            />
            <SectionCard title={`检索记录（${rows.length}）`}>
                <ErrorBanner error={logs.error} onRetry={() => logs.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={rows}
                    rowKey="id"
                    loading={logs.loading}
                    pagination={false}
                    toolbar={(
                        <Space>
                            <Input
                                prefix={<IconSearch />}
                                placeholder="按会话 ID 过滤"
                                value={conversationId}
                                onChange={setConversationId}
                                style={{ width: 260 }}
                            />
                            <Input
                                placeholder="条数"
                                value={String(limit)}
                                onChange={value => setLimit(Math.min(Math.max(Number(value) || 50, 1), 500))}
                                style={{ width: 100 }}
                                suffix={<Text type="tertiary">条</Text>}
                            />
                        </Space>
                    )}
                />
            </SectionCard>
            <Drawer title="检索日志详情" visible={Boolean(detail)} onCancel={() => setDetail(null)} width={560}>
                {detail && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                        <KeyValueList items={[
                            { label: '日志 ID', value: detail.id },
                            { label: '检索语句', value: detail.query },
                            { label: '风险类型', value: detail.riskType || '—' },
                            { label: '会话 ID', value: detail.conversationId || '—' },
                            { label: '消息 ID', value: detail.messageId || '—' },
                            { label: '时间', value: formatDateTime(detail.createdAt) }
                        ]} />
                        <JsonViewer value={detail.retrievedItems || []} title="命中知识项" />
                    </div>
                )}
            </Drawer>
        </div>
    );
}
