/**
 * C2 任务管理：服务端分页筛选、任务详情、取消、下载结果文件与批量删除。
 */
import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import {
    Drawer, PageHeader, SectionCard, StatGrid, DataTable, ErrorBanner,
    confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import * as c2 from '../c2-sessions/components/c2-api';
import {
    TASK_STATUS_COLOR, TASK_STATUS_OPTIONS, TASK_TYPES, TimePresetBar, TimeText,
    formatDurationMs, presetToSince
} from '../c2-sessions/components/c2-shared';
import CreateTaskModal from './CreateTaskModal';
import './page.css';

const { Text } = Typography;

export default function C2TasksPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('c2:write');
    const canDelete = hasPermission('c2:delete') || canWrite;
    const [searchParams, setSearchParams] = useSearchParams();

    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [status, setStatus] = useState('');
    const [taskType, setTaskType] = useState('');
    const [preset, setPreset] = useState('all');
    const [sessionInput, setSessionInput] = useState(searchParams.get('session_id') || '');
    const [sessionFilter, setSessionFilter] = useState(searchParams.get('session_id') || '');
    const [detail, setDetail] = useState(null);
    const [creating, setCreating] = useState(false);
    const [submitting, setSubmitting] = useState(false);

    const query = useMemo(() => ({
        page,
        page_size: pageSize,
        status: status || undefined,
        task_type: taskType || undefined,
        session_id: sessionFilter || undefined,
        since: presetToSince(preset) || undefined
    }), [page, pageSize, status, taskType, sessionFilter, preset]);

    const { data, loading, error, reload } = useResource(() => c2.listTasks(query), [query]);
    const tasks = Array.isArray(data?.tasks) ? data.tasks : [];
    const total = Number(data?.total ?? tasks.length);
    const counts = data?.status_counts || {};

    useInterval(() => reload({ silent: true }), 5000);

    const applyFilters = () => {
        setSessionFilter(sessionInput.trim());
        setPage(1);
        const next = new URLSearchParams(searchParams);
        if (sessionInput.trim()) next.set('session_id', sessionInput.trim());
        else next.delete('session_id');
        setSearchParams(next, { replace: true });
    };

    const resetFilters = () => {
        setStatus('');
        setTaskType('');
        setPreset('all');
        setSessionInput('');
        setSessionFilter('');
        setPage(1);
        setSearchParams(new URLSearchParams(), { replace: true });
    };

    const metrics = [
        { id: 'total', label: t('c2.tasks.statTotal', null, '当前筛选'), value: total },
        { id: 'success', label: t('c2.tasks.success', null, '成功'), value: counts.success || 0, tone: 'success' },
        { id: 'failed', label: t('c2.tasks.failed', null, '失败'), value: counts.failed || 0, tone: 'danger' },
        {
            id: 'pending',
            label: t('c2.tasks.pending', null, '待处理'),
            value: (counts.queued || 0) + (counts.sent || 0) + (counts.running || 0),
            tone: 'warning'
        }
    ];

    const cancel = async task => {
        try {
            await c2.cancelTask(task.id);
            toastSuccess(t('c2.tasks.toastCancelled', null, '任务已取消'));
            reload({ silent: true });
        } catch (err) {
            toastError(err);
        }
    };

    const remove = task => {
        confirmAction({
            title: t('c2.tasks.deleteOne', null, '删除任务'),
            content: t('c2.tasks.confirmDeleteOne', null, '确定从服务器删除该任务记录吗？此操作不可恢复。'),
            onOk: async () => {
                try {
                    await c2.deleteTasks([task.id]);
                    toastSuccess(t('c2.tasks.toastDeleted', null, '已删除 {{n}} 条').replace('{{n}}', '1'));
                    reload();
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const waitForTask = async task => {
        try {
            const payload = await c2.waitTask(task.id, 20);
            if (payload?.task) setDetail(payload.task);
            toastSuccess(t('c2.tasks.toastWaited', null, '已获取最新任务状态'));
            reload({ silent: true });
        } catch (err) {
            toastError(err, t('c2.tasks.waitTimeout', null, '等待任务完成超时'));
        }
    };

    const downloadResult = async task => {
        try {
            await c2.downloadTaskResult(task.id);
            toastSuccess(t('c2.tasks.downloadOk', null, '已开始下载'));
        } catch (err) {
            toastError(err, t('c2.tasks.labelResult', null, '下载结果文件失败'));
        }
    };

    const createTask = async values => {
        setSubmitting(true);
        try {
            await c2.createTask(values);
            toastSuccess(t('c2.tasks.toastCreated', null, '任务已下发'));
            setCreating(false);
            reload();
        } catch (err) {
            toastError(err, t('c2.tasks.createFailed', null, '下发任务失败'));
        } finally {
            setSubmitting(false);
        }
    };

    const columns = [
        {
            title: t('c2.tasks.colTask', null, '任务'),
            dataIndex: 'id',
            width: 150,
            render: id => <span className="c2-mono">{id}</span>
        },
        {
            title: t('c2.tasks.colSession', null, '会话'),
            dataIndex: 'sessionId',
            width: 150,
            render: value => <span className="c2-mono">{value || '—'}</span>
        },
        { title: t('c2.tasks.colType', null, '类型'), dataIndex: 'taskType', width: 110 },
        {
            title: t('c2.tasks.colCommand', null, '命令'),
            dataIndex: 'payload',
            render: value => {
                const command = value?.command || value?.path || '';
                return <span className="c2-mono c2-task-command" title={command}>{command || '—'}</span>;
            }
        },
        {
            title: t('c2.tasks.colStatus', null, '状态'),
            dataIndex: 'status',
            width: 110,
            render: value => (
                <Tag size="small" color={TASK_STATUS_COLOR[value] || 'grey'}>
                    {t(`c2.tasks.${value || 'queued'}`, null, value || '—')}
                </Tag>
            )
        },
        { title: t('c2.tasks.colDuration', null, '耗时'), dataIndex: 'durationMs', width: 100, render: value => formatDurationMs(value) },
        { title: t('c2.tasks.colCreated', null, '创建时间'), dataIndex: 'createdAt', width: 130, render: value => <TimeText value={value} /> },
        {
            title: t('c2.tasks.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 210,
            render: (_, task) => (
                <Space spacing={4} wrap>
                    <Button size="small" theme="borderless" onClick={() => setDetail(task)}>{t('c2.tasks.view', null, '查看')}</Button>
                    {['queued', 'sent', 'running'].includes(task.status) && (
                        <Button size="small" theme="borderless" onClick={() => cancel(task)}>{t('c2.tasks.cancelBtn', null, '取消')}</Button>
                    )}
                    {task.resultBlobPath && (
                        <Button size="small" theme="borderless" onClick={() => downloadResult(task)}>{t('c2.tasks.downloadResult', null, '结果文件')}</Button>
                    )}
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => remove(task)}>
                        {t('c2.tasks.deleteBtn', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="c2-page" id="page-c2-tasks">
            <PageHeader
                title={t('c2.tasks.title', null, '任务管理')}
                description={t('c2.tasks.pageDesc', null, '按状态、类型、会话与时间窗筛选任务，5 秒自动轮询最新状态')}
                extra={[
                    <Button key="refresh" icon={<IconRefresh />} onClick={() => reload()} loading={loading}>{t('c2.tasks.refresh', null, '刷新任务')}</Button>,
                    <Button key="create" theme="solid" type="primary" icon={<IconPlus />} disabled={!canWrite} onClick={() => setCreating(true)}>
                        {t('c2.tasks.createTitle', null, '下发任务')}
                    </Button>
                ]}
            />

            <StatGrid metrics={metrics} columns={4} />
            <ErrorBanner error={error} onRetry={() => reload()} />

            <SectionCard
                title={t('c2.tasks.filterTitle', null, '筛选')}
                extra={<TimePresetBar value={preset} onChange={value => { setPreset(value); setPage(1); }} ns="tasks" />}
            >
                <div className="c2-toolbar-fields">
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.tasks.colStatus', null, '状态')}</span>
                        <Select
                            value={status}
                            onChange={value => { setStatus(value || ''); setPage(1); }}
                            style={{ width: 160 }}
                            optionList={[{ value: '', label: t('c2.tasks.filterAllStatuses', null, '全部状态') }, ...TASK_STATUS_OPTIONS]}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.tasks.colType', null, '类型')}</span>
                        <Select
                            value={taskType}
                            onChange={value => { setTaskType(value || ''); setPage(1); }}
                            style={{ width: 160 }}
                            optionList={[{ value: '', label: t('c2.tasks.filterAllTypes', null, '全部类型') }, ...TASK_TYPES]}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.tasks.filterSession', null, '会话 ID')}</span>
                        <Input
                            value={sessionInput}
                            onChange={setSessionInput}
                            onEnterPress={applyFilters}
                            placeholder={t('c2.tasks.filterSessionPlaceholder', null, 's_xxxxxxxx')}
                            style={{ width: 190 }}
                        />
                    </div>
                    <Space spacing={8}>
                        <Button theme="solid" type="primary" onClick={applyFilters}>{t('c2.tasks.applyFilters', null, '应用筛选')}</Button>
                        <Button onClick={resetFilters}>{t('c2.tasks.resetFilters', null, '重置')}</Button>
                    </Space>
                </div>
            </SectionCard>

            <SectionCard title={t('c2.tasks.listTitle', null, '任务列表')}>
                <DataTable
                    columns={columns}
                    dataSource={tasks}
                    rowKey="id"
                    loading={loading}
                    onRefresh={() => reload()}
                    pagination={{
                        currentPage: page,
                        pageSize,
                        total,
                        onPageChange: next => setPage(next),
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                    emptyText={t('c2.tasks.emptyAll', null, '暂无任务')}
                />
            </SectionCard>

            <CreateTaskModal
                visible={creating}
                defaultSessionId={sessionFilter}
                submitting={submitting}
                onCancel={() => setCreating(false)}
                onSubmit={createTask}
            />

            <Drawer
                visible={Boolean(detail)}
                title={t('c2.tasks.modalTitle', null, '任务详情')}
                width={760}
                onCancel={() => setDetail(null)}
                footer={(
                    <Space>
                        {detail && ['queued', 'sent', 'running'].includes(detail.status) && (
                            <Button onClick={() => waitForTask(detail)}>{t('c2.tasks.waitBtn', null, '等待完成')}</Button>
                        )}
                        {detail?.resultBlobPath && <Button onClick={() => downloadResult(detail)}>{t('c2.tasks.downloadResult', null, '下载结果文件')}</Button>}
                        <Button onClick={() => setDetail(null)}>{t('c2.tasks.close', null, '关闭')}</Button>
                    </Space>
                )}
            >
                {detail && (
                    <div className="c2-task-detail">
                        <div className="c2-task-detail-meta">
                            <span className="c2-mono">{detail.id}</span>
                            <Tag size="small" color={TASK_STATUS_COLOR[detail.status] || 'grey'}>{detail.status}</Tag>
                            <Text type="tertiary" size="small">{formatDateTime(detail.createdAt)}</Text>
                            <Text type="tertiary" size="small">{formatDurationMs(detail.durationMs)}</Text>
                        </div>
                        <div className="c2-field-label">{t('c2.tasks.labelPayload', null, '参数')}</div>
                        <pre className="c2-pre">{JSON.stringify(detail.payload || {}, null, 2)}</pre>
                        {detail.resultText && (
                            <>
                                <div className="c2-field-label">{t('c2.tasks.labelResult', null, '输出')}</div>
                                <pre className="c2-pre">{detail.resultText}</pre>
                            </>
                        )}
                        {detail.error && (
                            <>
                                <div className="c2-field-label">{t('c2.tasks.labelError', null, '错误')}</div>
                                <pre className="c2-pre c2-pre-error">{detail.error}</pre>
                            </>
                        )}
                    </div>
                )}
            </Drawer>
        </div>
    );
}
