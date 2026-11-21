import { Button, Space, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard, StatGrid, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatNumber, formatPercent } from '../../lib/format';

const { Text } = Typography;

export default function SkillsMonitorPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canDelete = hasPermission('skills:delete');
    const stats = useResource(() => api.get('/skills/stats'), []);
    const rows = Array.isArray(stats.data?.stats) ? stats.data.stats : [];
    const total = Number(stats.data?.total_calls || 0);
    const success = Number(stats.data?.total_success || 0);
    const rate = total > 0 ? success / total : null;

    const clearAll = () => confirmAction({
        title: '清空所有技能调用统计？',
        content: '只影响统计计数，不删除技能文件。',
        onOk: async () => {
            await api.del('/skills/stats');
            toastSuccess('统计已清空');
            stats.reload();
        }
    });

    const clearOne = name => confirmAction({
        title: `清空「${name}」的统计？`,
        onOk: async () => {
            await api.del(`/skills/${encodeURIComponent(name)}/stats`);
            toastSuccess('已清空');
            stats.reload();
        }
    });

    const columns = [
        { title: '技能', dataIndex: 'skill_name', render: value => <Text strong>{value}</Text> },
        { title: '调用次数', dataIndex: 'total_calls', width: 120, render: value => formatNumber(value || 0) },
        { title: '成功', dataIndex: 'success_calls', width: 100, render: value => formatNumber(value || 0) },
        {
            title: '失败', dataIndex: 'failed_calls', width: 100,
            render: value => (Number(value) > 0 ? <Text type="danger">{formatNumber(value)}</Text> : '0')
        },
        { title: '最后调用', dataIndex: 'last_call_time', width: 180, render: value => value || '—' },
        {
            title: '操作', width: 110, render: (_, record) => (
                canDelete && Number(record.total_calls) > 0
                    ? <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => clearOne(record.skill_name)}>清空</Button>
                    : null
            )
        }
    ];

    return (
        <div>
            <PageHeader
                title={t('skills.monitorTitle', null, '技能状态')}
                description={stats.data?.skills_dir ? `技能目录：${stats.data.skills_dir}` : '统计技能被调用的次数、成功率与最近使用时间。'}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => stats.reload()} loading={stats.loading}>刷新</Button>
                        {canDelete && <Button type="danger" theme="light" onClick={clearAll}>清空全部统计</Button>}
                    </Space>
                )}
            />
            <StatGrid
                columns={4}
                metrics={[
                    { id: 'skills', label: '技能总数', value: formatNumber(stats.data?.total_skills || 0), loading: stats.loading },
                    { id: 'calls', label: '调用总数', value: formatNumber(total), loading: stats.loading },
                    { id: 'success', label: '成功次数', value: formatNumber(success), loading: stats.loading },
                    { id: 'rate', label: '成功率', value: rate === null ? '—' : formatPercent(rate), tone: rate !== null && rate < 0.8 ? 'warning' : 'success', loading: stats.loading }
                ]}
            />
            <SectionCard title="按技能统计">
                <ErrorBanner error={stats.error} onRetry={() => stats.reload()} />
                <DataTable columns={columns} dataSource={rows} rowKey="skill_name" loading={stats.loading} pagination={{ pageSize: 20 }} />
            </SectionCard>
        </div>
    );
}
