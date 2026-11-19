/** 深度测评列表：筛选、进度、新建、删除与进入工作台。 */
import { useEffect, useMemo, useState } from 'react';
import { Button, Input, Progress, RadioGroup, Select, Space, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useDebounced, useResource } from '../../components/hooks';
import {
    PageHeader, SectionCard, DataTable, ErrorBanner, StatusTag, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime, formatNumber } from '../../lib/format';
import { PERM_DELETE, PERM_WRITE, STATUS_LABEL, canDeleteTask, countsProgress, needsReview, statusText } from './labels';
import CreateAssessmentDrawer from './CreateDrawer';

const { Text } = Typography;

const STATUS_OPTIONS = [
    'draft', 'running', 'paused', 'blocked', 'failed', 'waiting_input', 'waiting_retry',
    'recovering', 'uncertain', 'cleanup_unconfirmed', 'completed', 'cancelled'
]
    .map(value => ({ value, label: STATUS_LABEL[value] }));

export default function AssessmentList({ onOpen }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const [query, setQuery] = useState('');
    const [status, setStatus] = useState('');
    const [archived, setArchived] = useState(false);
    const [page, setPage] = useState(0);
    const [createOpen, setCreateOpen] = useState(false);
    const [summaries, setSummaries] = useState({});
    const debouncedQuery = useDebounced(query, 300);

    const canWrite = hasPermission(PERM_WRITE);
    const canDelete = hasPermission(PERM_DELETE);

    const listQuery = useMemo(() => ({
        q: debouncedQuery,
        status,
        limit: 20,
        offset: page * 20,
        archived: archived ? 'true' : ''
    }), [debouncedQuery, status, page, archived]);

    const resource = useResource(() => api.get('/deep-assessments', listQuery), [listQuery]);
    const tasks = useMemo(() => resource.data?.tasks || [], [resource.data]);

    useEffect(() => { setPage(0); }, [debouncedQuery, status, archived]);

    // 列表投影不含目标与步骤，按行补一次 step_limit=1 的轻量详情，用于真实进度与目标展示。
    useEffect(() => {
        const pending = tasks.filter(task => !summaries[`${task.id}:${task.updated_at}`]);
        if (!pending.length) return undefined;
        let cancelled = false;
        let cursor = 0;
        const collected = {};
        const worker = async () => {
            while (!cancelled && cursor < pending.length) {
                const task = pending[cursor];
                cursor += 1;
                try {
                    const data = await api.get(`/deep-assessments/${task.id}`, { step_limit: 1 });
                    collected[`${task.id}:${task.updated_at}`] = {
                        objective: data?.task?.objective || '',
                        counts: data?.intent_counts || {},
                        total: data?.step_total ?? (data?.task?.steps || []).length
                    };
                } catch (error) {
                    collected[`${task.id}:${task.updated_at}`] = { objective: '', counts: {}, total: 0, failed: true };
                }
            }
        };
        Promise.all([worker(), worker(), worker(), worker()]).then(() => {
            if (!cancelled && Object.keys(collected).length) {
                setSummaries(prev => ({ ...prev, ...collected }));
            }
        });
        return () => { cancelled = true; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [tasks]);

    const removeTask = task => {
        confirmAction({
            title: t('deepAssessments.delete', null, '删除'),
            content: `删除测评「${task.name || task.id}」？关联执行会话仍保留。`,
            okText: t('deepAssessments.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/deep-assessments/${task.id}`);
                    toastSuccess(t('deepAssessments.deleted', null, '已删除'));
                    await resource.reload();
                } catch (error) {
                    toastError(error, t('deepAssessments.deleteFailed', null, '删除失败'));
                }
            }
        });
    };

    const columns = [
        {
            title: t('deepAssessments.name', null, '测评名称'),
            dataIndex: 'name',
            width: 260,
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Button theme="borderless" className="assessment-link" onClick={() => onOpen?.(record.id)}>
                        {value || record.id}
                    </Button>
                    <Text type="tertiary" size="small" className="assessment-ellipsis">{record.id}</Text>
                </div>
            )
        },
        {
            title: t('deepAssessments.status', null, '状态'),
            dataIndex: 'status',
            width: 130,
            render: (value, record) => (
                <Space spacing={4}>
                    <StatusTag status={value} label={statusText(t, value)} />
                    {record.archived ? <StatusTag status="archived" label={statusText(t, 'archived')} /> : null}
                    {needsReview(record) ? <StatusTag status="needs_review" label={statusText(t, 'needs_review')} /> : null}
                </Space>
            )
        },
        {
            title: t('deepAssessments.objective', null, '目标'),
            dataIndex: 'summary',
            render: (value, record) => {
                const summary = summaries[`${record.id}:${record.updated_at}`];
                const text = summary?.objective || value || '—';
                return <Text ellipsis={{ showTooltip: true }} className="assessment-ellipsis">{text}</Text>;
            }
        },
        {
            title: '进度',
            width: 190,
            align: 'left',
            render: (_, record) => {
                const summary = summaries[`${record.id}:${record.updated_at}`];
                if (!summary) return <Text type="tertiary">—</Text>;
                const progress = countsProgress(summary.counts, summary.total);
                return (
                    <div className="assessment-progress">
                        <Progress percent={progress.percent} showInfo size="small" strokeWidth={6} />
                        <Text type="tertiary" size="small">{progress.done} / {progress.total} 意图</Text>
                    </div>
                );
            }
        },
        {
            title: t('deepAssessments.rounds', null, '规划轮次'),
            dataIndex: 'rounds',
            width: 100,
            render: value => formatNumber(value || 0)
        },
        {
            title: t('deepAssessments.createdAt', null, '创建时间'),
            dataIndex: 'created_at',
            width: 170,
            render: value => formatDateTime(value)
        },
        {
            title: '操作',
            width: 150,
            render: (_, record) => (
                <Space spacing={4}>
                    <Button theme="borderless" size="small" onClick={() => onOpen?.(record.id)}>
                        进入
                    </Button>
                    <Button
                        theme="borderless"
                        type="danger"
                        size="small"
                        icon={<IconDelete />}
                        disabled={!canDelete || !canDeleteTask(record)}
                        onClick={() => removeTask(record)}
                    >
                        {t('deepAssessments.delete', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="assessment-page">
            <PageHeader
                title={t('deepAssessments.title', null, '深度测评')}
                description={t('deepAssessments.subtitle', null, '围绕一个目标持续探索，保存证据与进度，跨会话继续执行。')}
                extra={[
                    <Button key="refresh" icon={<IconRefresh />} onClick={() => resource.reload()} loading={resource.loading}>
                        {t('deepAssessments.refresh', null, '刷新')}
                    </Button>,
                    <Button key="create" icon={<IconPlus />} theme="solid" type="primary" disabled={!canWrite} onClick={() => setCreateOpen(true)}>
                        {t('deepAssessments.create', null, '新建测评')}
                    </Button>
                ]}
            />
            <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
            <SectionCard
                className="assessment-list-card"
                title={t('deepAssessments.listTitle', null, '测评列表')}
                extra={(
                    <RadioGroup
                        className="assessment-view-options"
                        aria-label={t('deepAssessments.archiveFilter', null, '归档状态')}
                        type="button"
                        value={archived ? 'archived' : 'unarchived'}
                        options={[
                            { value: 'unarchived', label: t('deepAssessments.unarchivedTasks', null, '未归档') },
                            { value: 'archived', label: t('deepAssessments.archivedTasks', null, '已归档') }
                        ]}
                        onChange={event => setArchived(event.target.value === 'archived')}
                    />
                )}
            >
                <div className="assessment-toolbar">
                    <Input
                        prefix={<IconSearch />}
                        value={query}
                        onChange={setQuery}
                        placeholder={t('deepAssessments.search', null, '搜索测评名称')}
                        style={{ width: 260 }}
                        showClear
                    />
                    <Select
                        value={status}
                        onChange={value => setStatus(value || '')}
                        optionList={[{ value: '', label: t('deepAssessments.all', null, '全部状态') }, ...STATUS_OPTIONS]}
                        style={{ width: 160 }}
                    />
                </div>
                <DataTable
                    columns={columns}
                    dataSource={tasks}
                    rowKey="id"
                    loading={resource.loading}
                    pagination={false}
                    emptyText={t('deepAssessments.empty', null, '暂无测评，先创建一个目标。')}
                />
                <div className="assessment-pager">
                    <Space>
                        <Button size="small" disabled={page === 0} onClick={() => setPage(value => Math.max(0, value - 1))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">第 {page + 1} 页</Text>
                        <Button size="small" disabled={!resource.data?.has_more} onClick={() => setPage(value => value + 1)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
            </SectionCard>
            <CreateAssessmentDrawer
                visible={createOpen}
                onCancel={() => setCreateOpen(false)}
                onCreated={task => {
                    setCreateOpen(false);
                    resource.reload();
                    if (task?.id) onOpen?.(task.id);
                }}
            />
        </div>
    );
}
