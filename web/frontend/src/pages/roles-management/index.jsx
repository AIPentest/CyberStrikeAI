import { useState } from 'react';
import { Button, Input, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard
} from '../../components/ui';

const { Text } = Typography;

export default function RolesManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canManage = hasPermission('roles:write roles:delete');
    const [search, setSearch] = useState('');

    const roles = useResource(() => api.get('/roles'), []);
    const rows = (Array.isArray(roles.data?.roles) ? roles.data.roles : []).filter(role => {
        if (!search) return true;
        const needle = search.toLowerCase();
        return [role.name, role.description].some(value => String(value || '').toLowerCase().includes(needle));
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
    ];

    return (
        <div>
            <PageHeader
                title={t('roles.title', null, '角色管理')}
                description="角色定义对话的系统提示词、可用工具与绑定工作流。"
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => roles.reload()} loading={roles.loading}>刷新</Button>
                        {canManage && <Button href="/classic#roles-management">管理已有角色配置</Button>}
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
        </div>
    );
}
