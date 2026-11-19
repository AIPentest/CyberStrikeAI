import { useState } from 'react';
import { Button, Form, Input, Modal, Space, Switch, Tag, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard, confirmAction, toastError, toastSuccess
} from '../../components/ui';

const { Text } = Typography;

export default function RolesManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('roles:write');
    const canDelete = hasPermission('roles:delete');
    const [search, setSearch] = useState('');
    const [editor, setEditor] = useState(null);
    const [saving, setSaving] = useState(false);

    const roles = useResource(() => api.get('/roles'), []);
    const rows = (Array.isArray(roles.data?.roles) ? roles.data.roles : []).filter(role => {
        if (!search) return true;
        const needle = search.toLowerCase();
        return [role.name, role.description].some(value => String(value || '').toLowerCase().includes(needle));
    });

    const save = async () => {
        const values = await editor.form.validate();
        setSaving(true);
        try {
            const payload = {
                name: values.name.trim(),
                description: values.description?.trim() || '',
                user_prompt: values.user_prompt || '',
                tools: String(values.tools || '').split(',').map(item => item.trim()).filter(Boolean),
                enabled: values.enabled !== false
            };
            const original = editor.role?.name;
            if (original && original !== payload.name) {
                await api.post('/roles', payload);
                await api.del(`/roles/${encodeURIComponent(original)}`);
            } else if (original) {
                await api.put(`/roles/${encodeURIComponent(original)}`, payload);
            } else {
                await api.post('/roles', payload);
            }
            toastSuccess('角色已保存');
            setEditor(null);
            roles.reload();
        } catch (error) {
            toastError(error, '保存角色失败');
        } finally {
            setSaving(false);
        }
    };

    const remove = role => confirmAction({
        title: `删除角色「${role.name}」？`,
        content: '已绑定该角色的对话将失去对应提示词与工具配置。',
        onOk: async () => {
            await api.del(`/roles/${encodeURIComponent(role.name)}`);
            toastSuccess('已删除');
            roles.reload();
        }
    });

    const columns = [
        {
            title: '角色', dataIndex: 'name', render: (value, record) => (
                <Space vertical spacing={0} align="start">
                    <Space>
                        <Text strong>{value}</Text>
                        {record.enabled === false && <Tag size="small" color="grey">已停用</Tag>}
                    </Space>
                    <Text type="tertiary" ellipsis={{ showTooltip: true }} style={{ maxWidth: 460 }}>{record.description || '—'}</Text>
                </Space>
            )
        },
        {
            title: '工具', dataIndex: 'tools', width: 260,
            render: value => (Array.isArray(value) && value.length
                ? <Space wrap spacing={4}>{value.slice(0, 4).map(tool => <Tag size="small" key={tool}>{tool}</Tag>)}{value.length > 4 && <Text type="tertiary">+{value.length - 4}</Text>}</Space>
                : <Text type="tertiary">未绑定</Text>)
        },
        { title: '工作流', dataIndex: 'workflow_id', width: 160, render: value => value || '—' },
        {
            title: '操作', width: 150, render: (_, record) => (
                <Space>
                    {canWrite && <Button size="small" theme="borderless" icon={<IconEdit />} onClick={() => setEditor({ role: record, form: {} })}>编辑</Button>}
                    {canDelete && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => remove(record)} />}
                </Space>
            )
        }
    ];

    return (
        <div>
            <PageHeader
                title={t('roles.title', null, '角色管理')}
                description="角色定义对话的系统提示词、可用工具与绑定工作流。"
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => roles.reload()} loading={roles.loading}>刷新</Button>
                        {canWrite && <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => setEditor({ role: null, form: {} })}>新建角色</Button>}
                    </Space>
                )}
            />
            <SectionCard title={`角色（${rows.length}）`}>
                <ErrorBanner error={roles.error} onRetry={() => roles.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={rows}
                    rowKey="name"
                    loading={roles.loading}
                    pagination={{ pageSize: 20 }}
                    toolbar={<Input prefix={<IconSearch />} value={search} onChange={setSearch} placeholder="搜索角色名称" style={{ width: 260 }} />}
                />
            </SectionCard>

            <Modal
                title={editor?.role ? '编辑角色' : '新建角色'}
                visible={Boolean(editor)}
                key={editor?.role?.name || 'new-role'}
                onCancel={() => setEditor(null)}
                onOk={save}
                confirmLoading={saving}
                width={760}
            >
                <Form
                    getFormApi={form => setEditor(current => (current ? { ...current, form } : current))}
                    initValues={editor?.role ? {
                        name: editor.role.name,
                        description: editor.role.description,
                        user_prompt: editor.role.user_prompt,
                        tools: Array.isArray(editor.role.tools) ? editor.role.tools.join(', ') : '',
                        enabled: editor.role.enabled !== false
                    } : { enabled: true }}
                >
                    <Form.Input field="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]} />
                    <Form.Input field="description" label="描述" />
                    <Form.Input field="tools" label="工具（逗号分隔）" placeholder="toolName, mcp::toolName" />
                    <Form.TextArea field="user_prompt" label="用户提示词（追加到用户消息前）" autosize={{ minRows: 6, maxRows: 18 }} />
                    <Form.Slot label="启用">
                        <Form.Switch field="enabled" />
                    </Form.Slot>
                </Form>
            </Modal>
        </div>
    );
}
