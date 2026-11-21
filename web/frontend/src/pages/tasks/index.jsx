/** 任务管理：批量任务队列列表、统计、创建与队列详情抽屉，深链接 ?id=。 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
    Button, Input, Progress, Select, Space, Tabs, Tag, Typography
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useDebounced, useInterval, useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard, StatGrid, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import QueueFormModal from './queue-form';
import QueueDetailDrawer from './queue-detail';
import LiveTasks from './live-tasks';
import {
    QUEUE_STATUS_FILTERS, agentModeLabel, errText, errorProp, queueStats, queueStatusView
} from './helpers';
import './page.css';

const { Text } = Typography;

export default function TasksPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('tasks:write');
    const canDelete = hasPermission('tasks:delete');

    const [params, setParams] = useSearchParams();
    const selectedId = params.get('id') || '';
    const tab = params.get('tab') || 'queues';

    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(10);
    const [statusFilter, setStatusFilter] = useState('all');
    const [keyword, setKeyword] = useState('');
    const debounced = useDebounced(keyword, 300);
    const [createVisible, setCreateVisible] = useState(false);
    const [rev, setRev] = useState(0);

    const overview = useResource(() => api.get('/batch-tasks', { limit: 100, page: 1 }), [rev]);

    const list = useResource(() => api.get('/batch-tasks', {
        limit: pageSize,
        page,
        status: statusFilter === 'all' ? undefined : statusFilter,
        keyword: debounced.trim() || undefined
    }), [page, pageSize, statusFilter, debounced, rev]);

    const queues = Array.isArray(list.data?.queues) ? list.data.queues : [];
    const total = list.data?.total ?? 0;
    const anyRunning = useMemo(
        () => (overview.data?.queues || []).some(queue => queue.status === 'running'),
        [overview.data]
    );

    useInterval(() => {
        if (anyRunning) {
            overview.reload({ silent: true });
            list.reload({ silent: true });
        }
    }, anyRunning ? 5000 : 0);

    const navigate = useCallback((nextId, nextTab) => {
        const next = new URLSearchParams(params);
        if (nextId) next.set('id', nextId);
        else next.delete('id');
        if (nextTab) next.set('tab', nextTab);
        else next.delete('tab');
        setParams(next, { replace: Boolean(nextId) });
    }, [params, setParams]);

    useEffect(() => {
        if (tab !== 'queues' && tab !== 'live') navigate(undefined, 'queues');
    }, [tab, navigate]);

    const refresh = () => setRev(value => value + 1);

    const overviewQueues = overview.data?.queues || [];
    const overviewTotals = useMemo(() => {
        const stats = overviewQueues.map(queueStats);
        return {
            queues: overview.data?.total ?? overviewQueues.length,
            running: overviewQueues.filter(queue => queue.status === 'running').length,
            pendingTasks: stats.reduce((sum, item) => sum + item.pending, 0),
            failedTasks: stats.reduce((sum, item) => sum + item.failed, 0)
        };
    }, [overviewQueues, overview.data]);

    const startQueue = async (record) => {
        try {
            await api.post(`/batch-tasks/${record.id}/start`);
            toastSuccess('已开始执行');
            refresh();
        } catch (error) {
            toastError(errText(error, t('tasks.startBatchQueueFailed', null, '启动批量任务失败')));
        }
    };

    const pauseQueue = (record) => confirmAction({
        title: t('tasks.statusPaused', null, '已暂停'),
        content: t('tasks.pauseQueueConfirm', null, '确定要暂停这个批量任务队列吗？当前正在执行的任务将被停止，后续任务将保留待执行状态。'),
        okText: t('common.confirm', null, '确认'),
        onOk: async () => {
            try {
                await api.post(`/batch-tasks/${record.id}/pause`);
                toastSuccess(t('tasks.statusPaused', null, '已暂停'));
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.pauseQueueFailed', null, '暂停批量任务失败')));
            }
        }
    });

    const rerunQueue = (record) => confirmAction({
        title: t('tasks.retry', null, '重试'),
        content: t('tasks.rerunQueueConfirm', null, '确定要重跑一轮吗？所有子任务将被重置为待执行状态并重新开始执行。'),
        okText: t('common.confirm', null, '确认'),
        onOk: async () => {
            try {
                await api.post(`/batch-tasks/${record.id}/rerun`);
                toastSuccess('已开始重跑');
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.rerunQueueFailed', null, '重跑批量任务失败')));
            }
        }
    });

    const deleteQueue = (record) => confirmAction({
        title: t('tasks.deleteQueue', null, '删除队列'),
        content: t('tasks.deleteQueueConfirm', null, '确定要删除这个批量任务队列吗？此操作不可恢复。'),
        okText: t('common.delete', null, '删除'),
        onOk: async () => {
            try {
                await api.del(`/batch-tasks/${record.id}`);
                toastSuccess(t('tasks.deleteQueue', null, '删除队列'));
                if (selectedId === record.id) navigate(undefined, tab);
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.deleteQueueFailed', null, '删除批量任务队列失败')));
            }
        }
    });

    const metrics = [
        { id: 'queues', label: t('tasks.batchQueueTitle', null, '批量任务队列'), value: overviewTotals.queues, loading: overview.loading },
        {
            id: 'running',
            label: t('tasks.runningLabel', null, '执行中'),
            value: overviewTotals.running,
            tone: overviewTotals.running ? 'success' : undefined,
            loading: overview.loading
        },
        { id: 'pending', label: t('tasks.pendingLabel', null, '待执行'), value: overviewTotals.pendingTasks, loading: overview.loading },
        {
            id: 'failed',
            label: t('tasks.failedLabel', null, '失败'),
            value: overviewTotals.failedTasks,
            tone: overviewTotals.failedTasks ? 'danger' : undefined,
            loading: overview.loading
        }
    ];

    const columns = [
        {
            title: t('batchImportModal.queueTitle', null, '任务标题'),
            dataIndex: 'title',
            render: (value, record) => {
                const view = queueStatusView(record);
                return (
                    <button
                        type="button"
                        className="task-queue-name"
                        onClick={() => navigate(record.id, 'queues')}
                        title={record.id}
                    >
                        <span className="task-queue-title">{value || t('tasks.batchQueueUntitled', null, '未命名队列')}</span>
                        {view.sublabel ? <span className="task-queue-sub">{view.sublabel}</span> : null}
                    </button>
                );
            }
        },
        {
            title: t('projects.status', null, '状态'),
            dataIndex: 'status',
            width: 170,
            render: (value, record) => {
                const view = queueStatusView(record);
                return <Tag size="small" color={view.color}>{view.label}</Tag>;
            }
        },
        {
            title: '进度',
            dataIndex: 'progress',
            width: 190,
            render: (value, record) => {
                const stats = queueStats(record);
                return (
                    <div className="task-queue-progress">
                        <Progress percent={stats.percent} showInfo={false} size="small" />
                        <Text type="tertiary" size="small">{stats.percent}%（{stats.done}/{stats.total}）</Text>
                    </div>
                );
            }
        },
        {
            title: t('batchImportModal.agentMode', null, '代理模式'),
            dataIndex: 'agentMode',
            width: 150,
            render: value => <Text size="small">{agentModeLabel(value)}</Text>
        },
        {
            title: t('batchImportModal.scheduleMode', null, '调度方式'),
            dataIndex: 'scheduleMode',
            width: 128,
            render: (value, record) => (
                <Text size="small">
                    {value === 'cron'
                        ? `Cron · ${record.cronExpr || ''}`
                        : t('batchImportModal.scheduleModeManual', null, '手工执行')}
                </Text>
            )
        },
        {
            title: t('tasks.createdTimeLabel', null, '创建时间'),
            dataIndex: 'createdAt',
            width: 160,
            render: value => <Text type="tertiary" size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 250,
            render: (value, record) => (
                <Space spacing={2} wrap>
                    <Button size="small" theme="borderless" onClick={() => navigate(record.id, 'queues')}>
                        {t('projects.details', null, '详情')}
                    </Button>
                    {record.status === 'running' ? (
                        <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => pauseQueue(record)}>
                            {t('tasks.statusPaused', null, '已暂停')}
                        </Button>
                    ) : (
                        <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => startQueue(record)}>
                            开始执行
                        </Button>
                    )}
                    <Button
                        size="small"
                        theme="borderless"
                        disabled={!canWrite || !['completed', 'cancelled'].includes(record.status)}
                        onClick={() => rerunQueue(record)}
                    >
                        {t('tasks.retry', null, '重试')}
                    </Button>
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => deleteQueue(record)}>
                        {t('common.delete', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="task-page">
            <PageHeader
                title={t('tasks.title', null, '任务管理')}
                description={t('tasks.pageDesc', null, '批量任务队列按并发池执行子任务；队列详情可查看进度、单条执行与结果。')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} theme="borderless" loading={list.loading} onClick={refresh}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button type="primary" icon={<IconPlus />} disabled={!canWrite} onClick={() => setCreateVisible(true)}>
                            {t('batchImportModal.title', null, '新建任务')}
                        </Button>
                    </Space>
                )}
            />

            <ErrorBanner error={errorProp(overview.error || list.error)} onRetry={refresh} />
            <StatGrid metrics={metrics} columns={4} />

            <Tabs
                type="line"
                activeKey={tab}
                onChange={key => navigate(undefined, key)}
                tabList={[
                    { itemKey: 'queues', tab: t('tasks.batchQueueTitle', null, '批量任务队列') },
                    { itemKey: 'live', tab: `${t('tasks.runningLabel', null, '执行中')}任务` }
                ]}
                className="task-tabs"
            />

            {tab === 'live' ? (
                <LiveTasks />
            ) : (
                <SectionCard
                    title={t('tasks.batchQueueTitle', null, '批量任务队列')}
                    description={t('tasks.finishedProgressHint', null, '进度表示已结束的任务比例，包含失败和取消，不表示成功率。')}
                    extra={<Text type="tertiary" size="small">{t('tasks.taskCount', { count: total }, `共 ${total} 个任务`)}</Text>}
                >
                    <div className="task-toolbar">
                        <Input
                            prefix={<IconSearch />}
                            value={keyword}
                            showClear
                            placeholder={t('batchImportModal.queueTitlePlaceholder', null, '请输入任务标题（可选，用于标识和筛选）')}
                            onChange={value => { setKeyword(value); setPage(1); }}
                            style={{ width: 280 }}
                        />
                        <Select
                            value={statusFilter}
                            onChange={value => { setStatusFilter(value); setPage(1); }}
                            style={{ width: 150 }}
                            optionList={QUEUE_STATUS_FILTERS}
                        />
                    </div>
                    <DataTable
                        columns={columns}
                        dataSource={queues}
                        rowKey="id"
                        loading={list.loading}
                        emptyText={statusFilter !== 'all' || keyword
                            ? t('tasks.noMatchingTasks', null, '当前没有符合条件的任务')
                            : t('tasks.noBatchQueues', null, '当前没有批量任务队列')}
                        pagination={{
                            currentPage: page,
                            pageSize,
                            total,
                            onPageChange: nextPage => setPage(nextPage),
                            onPageSizeChange: nextSize => { setPageSize(nextSize); setPage(1); }
                        }}
                    />
                </SectionCard>
            )}

            <QueueFormModal
                visible={createVisible}
                onCancel={() => setCreateVisible(false)}
                onCreated={payload => {
                    setCreateVisible(false);
                    refresh();
                    if (payload?.queueId) navigate(payload.queueId, 'queues');
                }}
            />

            <QueueDetailDrawer
                queueId={selectedId}
                visible={Boolean(selectedId)}
                canWrite={canWrite}
                canDelete={canDelete}
                onClose={() => navigate(undefined, tab)}
                onChanged={refresh}
            />
        </div>
    );
}
