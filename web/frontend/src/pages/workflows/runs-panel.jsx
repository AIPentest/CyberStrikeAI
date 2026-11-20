/** 运行历史：待审批运行列表 + 运行详情（replay）抽屉。 */
import { useSearchParams } from 'react-router-dom';
import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, SectionCard } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { RUN_STATUS, errorProp, shortHash } from './helpers';
import RunDetailDrawer from './run-detail';

const { Text } = Typography;

export default function RunsPanel({ canExecute }) {
    const t = useT();
    const [params, setParams] = useSearchParams();
    const runId = params.get('run') || '';

    const pending = useResource(() => api.get('/workflows/runs/pending'), []);
    useInterval(() => pending.reload({ silent: true }), 15000);

    const runs = Array.isArray(pending.data?.runs) ? pending.data.runs : [];

    const openRun = (id) => {
        const next = new URLSearchParams(params);
        if (id) next.set('run', id);
        else next.delete('run');
        setParams(next, { replace: true });
    };

    const columns = [
        {
            title: '运行',
            dataIndex: 'id',
            width: 190,
            render: value => (
                <Button size="small" theme="borderless" onClick={() => openRun(value)}>
                    <code>{shortHash(value, 14)}</code>
                </Button>
            )
        },
        {
            title: '工作流',
            dataIndex: 'workflow_id',
            width: 200,
            render: value => <Text size="small"><code>{value}</code></Text>
        },
        {
            title: '会话',
            dataIndex: 'conversation_id',
            width: 220,
            render: value => <Text type="tertiary" size="small"><code>{value || '—'}</code></Text>
        },
        {
            title: t('projects.status', null, '状态'),
            dataIndex: 'status',
            width: 110,
            render: value => {
                const meta = RUN_STATUS[String(value || '')] || { label: value || '—', color: 'grey' };
                return <Tag size="small" color={meta.color}>{meta.label}</Tag>;
            }
        },
        {
            title: '待审批节点',
            dataIndex: 'pending_hitl_node_id',
            width: 160,
            render: value => (value ? <code>{value}</code> : <Text type="tertiary">—</Text>)
        },
        {
            title: '开始时间',
            dataIndex: 'started_at',
            width: 165,
            render: value => <Text size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 120,
            render: value => (
                <Space>
                    <Button size="small" theme="borderless" onClick={() => openRun(value)}>
                        {t('projects.details', null, '详情')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <>
            <SectionCard
                title={t('workflows.runsPending', null, '待审批运行')}
                description={`等待审批（awaiting_hitl）的工作流运行，每 15 秒自动刷新。${canExecute ? ' 可直接在此通过或拒绝。' : ''}`}
                extra={(
                    <Button icon={<IconRefresh />} theme="borderless" loading={pending.loading} onClick={() => pending.reload()}>
                        {t('common.refresh', null, '刷新')}
                    </Button>
                )}
            >
                <ErrorBanner error={errorProp(pending.error)} onRetry={() => pending.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={runs}
                    rowKey="id"
                    loading={pending.loading}
                    pagination={{ pageSize: 10 }}
                    emptyText="当前没有等待审批的工作流运行"
                />
            </SectionCard>

            <RunDetailDrawer
                runId={runId}
                visible={Boolean(runId)}
                canExecute={canExecute}
                onClose={() => openRun('')}
                onChanged={() => pending.reload({ silent: true })}
            />
        </>
    );
}
