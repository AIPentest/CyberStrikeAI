import { useState } from 'react';
import { Button, Form, Input, Modal, Select, Space, Switch, Tag, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, JsonViewer, KeyValueList, MarkdownView, PageHeader, SectionCard, confirmAction, toastError, toastSuccess, Drawer } from '../../components/ui';

const { Text } = Typography;

export default function AgentsManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('agents:write');
    const canDelete = hasPermission('agents:delete');
    const [search, setSearch] = useState('');
    const [detail, setDetail] = useState(null);
    const [editor, setEditor] = useState(null);
    const [saving, setSaving] = useState(false);

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

    const save = async () => {
        const values = await editor.form.validate();
        setSaving(true);
        try {
            await api.post('/multi-agent/markdown-agents', {
                filename: values.filename?.trim() || '',
                name: values.name.trim(),
                description: values.description?.trim() || '',
                instruction: values.instruction,
                kind: values.kind || 'sub',
                tools: String(values.tools || '').split(',').map(item => item.trim()).filter(Boolean)
            });
            toastSuccess('智能体已保存');
            setEditor(null);
            agents.reload();
        } catch (error) {
            toastError(error, '保存智能体失败');
        } finally {
            setSaving(false);
        }
    };

    const remove = row => confirmAction({
        title: `删除智能体「${row.name || row.filename}」？`,
        onOk: async () => {
            await api.del(`/multi-agent/markdown-agents/${encodeURIComponent(row.filename)}`);
            toastSuccess('已删除');
            agents.reload();
        }
    });

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
                    {canWrite && <Button size="small" theme="borderless" icon={<IconEdit />} onClick={() => setEditor({ record, form: {} })} />}
                    {canDelete && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => remove(record)} />}
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
                        {canWrite && <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => setEditor({ record: null, form: {} })}>新建智能体</Button>}
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

            <Modal
                title={editor?.record ? '编辑智能体' : '新建智能体'}
                visible={Boolean(editor)}
                key={editor?.record?.filename || 'new-agent'}
                onCancel={() => setEditor(null)}
                onOk={save}
                confirmLoading={saving}
                width={760}
            >
                <Form
                    getFormApi={form => setEditor(current => (current ? { ...current, form } : current))}
                    initValues={editor?.record ? {
                        filename: editor.record.filename,
                        name: editor.record.name,
                        description: editor.record.description,
                        kind: editor.record.kind || 'sub'
                    } : { kind: 'sub' }}
                >
                    <Form.Input field="filename" label="文件名（留空自动生成）" placeholder="例如 recon.md" />
                    <Form.Input field="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} />
                    <Form.Input field="description" label="描述" />
                    <Form.Select field="kind" label="类型" optionList={[
                        { value: 'sub', label: '子智能体' },
                        { value: 'orchestrator', label: '编排器' },
                        { value: 'plan_execute', label: '计划执行' }
                    ]} />
                    <Form.Input field="tools" label="工具（逗号分隔）" placeholder="toolName, mcp::tool" />
                    <Form.TextArea field="instruction" label="指令（Markdown）" autosize={{ minRows: 8, maxRows: 20 }} />
                </Form>
            </Modal>

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
