import { useState } from 'react';
import { Button, Input, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, JsonViewer, KeyValueList, MarkdownView, PageHeader, SectionCard, toastError, Drawer } from '../../components/ui';

const { Text } = Typography;

export default function AgentsManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('agents:write');
    const [search, setSearch] = useState('');
    const [detail, setDetail] = useState(null);

    const agents = useResource(() => api.get('/multi-agent/markdown-agents'), []);
    const rows = (Array.isArray(agents.data?.agents) ? agents.data.agents : []).filter(item => {
        if (!search) return true;
        const needle = search.toLowerCase();
        return [item.name, item.id, item.filename, item.description].some(value => String(value || '').toLowerCase().includes(needle));
    });

    const openDetail = async record => {
        try {
            setDetail(await api.get(`/multi-agent/markdown-agents/${encodeURIComponent(record.filename)}`));
        } catch (error) {
            toastError(error, '读取智能体失败');
        }
    };

    const columns = [
        {
            title: '智能体', dataIndex: 'name', render: (value, record) => (
                <Space vertical spacing={0} align="start">
                    <Space>
                        <Text strong>{value || record.id}</Text>
                        {record.is_orchestrator && <Tag size="small" color="purple">编排器</Tag>}
                    </Space>
                    <Text type="tertiary">{record.filename}</Text>
                </Space>
            )
        },
        { title: '类型', dataIndex: 'kind', width: 110, render: value => <Tag size="small">{value || 'sub'}</Tag> },
        { title: '描述', dataIndex: 'description', render: value => <Text type="tertiary" ellipsis={{ showTooltip: true }} style={{ maxWidth: 460 }}>{value || '—'}</Text> },
        {
            title: '操作', width: 180, render: (_, record) => (
                <Space>
                    <Button size="small" theme="borderless" onClick={() => openDetail(record)}>详情</Button>
                </Space>
            )
        }
    ];

    return (
        <div>
            <PageHeader
                title={t('agents.title', null, '智能体管理')}
                description={agents.data?.dir ? `目录：${agents.data.dir}` : '以 Markdown 文件定义的子智能体与编排器。'}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => agents.reload()} loading={agents.loading}>刷新</Button>
                        {canWrite && <Button href="/classic#agents-management">管理智能体配置</Button>}
                    </Space>
                )}
            />
            {agents.data?.error && <ErrorBanner error={agents.data.error} />}
            <SectionCard title={`智能体（${rows.length}）`}>
                <ErrorBanner error={agents.error} onRetry={() => agents.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={rows}
                    rowKey="filename"
                    loading={agents.loading}
                    pagination={{ pageSize: 20 }}
                    toolbar={<Input prefix={<IconSearch />} value={search} onChange={setSearch} placeholder="搜索名称或文件名" style={{ width: 280 }} />}
                />
            </SectionCard>

            <Drawer title={detail?.name || '智能体详情'} visible={Boolean(detail)} onCancel={() => setDetail(null)} width={720}>
                {detail && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                        <KeyValueList items={[
                            { label: '文件名', value: detail.filename },
                            { label: 'ID', value: detail.id },
                            { label: '类型', value: detail.kind },
                            { label: '绑定角色', value: detail.bind_role || '—' },
                            { label: '最大迭代', value: detail.max_iterations ?? '—' },
                            { label: '工具', value: Array.isArray(detail.tools) && detail.tools.length ? detail.tools.join('、') : '—' }
                        ]} />
                        <MarkdownView content={detail.instruction || detail.raw || ''} />
                        <JsonViewer value={detail.raw || detail} title="原始 Markdown" collapsed />
                    </div>
                )}
            </Drawer>
        </div>
    );
}
