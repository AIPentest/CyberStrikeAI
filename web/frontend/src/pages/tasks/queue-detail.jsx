/** 队列详情抽屉：进度、子任务表格、启动/暂停/重跑、单条执行与结果查看。 */
import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
    Button, Progress, SideSheet, Space, Tag, TextArea, Typography
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import {
    Drawer, DataTable, ErrorBanner, KeyValueList, MarkdownView, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import {
    AGENT_MODES, HITL_POLICIES, TASK_STATUS, durationText, errorProp, errText,
    queueStats, queueStatusView
} from './helpers';

const { Text } = Typography;

function TaskStatusTag({ status }) {
    const meta = TASK_STATUS[String(status || '')] || { label: status || '—', color: 'grey' };
    return <Tag size="small" color={meta.color}>{meta.label}</Tag>;
}

export default function QueueDetailDrawer({ queueId, visible, canWrite, canDelete, onClose, onChanged }) {
    const t = useT();
    const navigate = useNavigate();
    const [rev, setRev] = useState(0);
    const [taskModal, setTaskModal] = useState({ visible: false, taskId: '', message: '' });
    const [resultModal, setResultModal] = useState({ visible: false, task: null });

    const detail = useResource(
        () => (queueId ? api.get(`/batch-tasks/${queueId}`) : Promise.resolve(null)),
        [queueId, rev]
    );
    const queue = detail.data?.queue || null;
    const stats = useMemo(() => queueStats(queue), [queue]);

    useInterval(() => {
        if (visible && queue?.status === 'running') detail.reload({ silent: true });
    }, visible && queue?.status === 'running' ? 4000 : 0);

    useEffect(() => {
        if (visible && queueId) detail.reload({ silent: true });
    }, [visible, queueId]); // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        if (!visible) {
            setTaskModal({ visible: false, taskId: '', message: '' });
            setResultModal({ visible: false, task: null });
        }
    }, [visible]);

    const refresh = () => {
        setRev(value => value + 1);
        onChanged?.();
    };

    const act = async (action, label) => {
        try {
            await api.post(`/batch-tasks/${queueId}/${action}`);
            toastSuccess(label);
            refresh();
        } catch (error) {
            toastError(errText(error, label + '失败'));
        }
    };

    const start = () => act('start', '已开始执行');
    const pause = () => confirmAction({
        title: t('tasks.statusPaused', null, '已暂停'),
        content: t('tasks.pauseQueueConfirm', null, '确定要暂停这个批量任务队列吗？当前正在执行的任务将被停止，后续任务将保留待执行状态。'),
        okText: t('common.confirm', null, '确认'),
        onOk: async () => {
            try {
                await api.post(`/batch-tasks/${queueId}/pause`);
                toastSuccess(t('tasks.statusPaused', null, '已暂停'));
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.pauseQueueFailed', null, '暂停批量任务失败')));
            }
        }
    });
    const rerun = () => confirmAction({
        title: t('tasks.retry', null, '重试'),
        content: t('tasks.rerunQueueConfirm', null, '确定要重跑一轮吗？所有子任务将被重置为待执行状态并重新开始执行。'),
        okText: t('common.confirm', null, '确认'),
        onOk: async () => {
            try {
                await api.post(`/batch-tasks/${queueId}/rerun`);
                toastSuccess(t('tasks.statusRunning', null, '执行中'));
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.rerunQueueFailed', null, '重跑批量任务失败')));
            }
        }
    });
    const removeQueue = () => confirmAction({
        title: t('tasks.deleteQueue', null, '删除队列'),
        content: t('tasks.deleteQueueConfirm', null, '确定要删除这个批量任务队列吗？此操作不可恢复。'),
        okText: t('common.delete', null, '删除'),
        onOk: async () => {
            try {
                await api.del(`/batch-tasks/${queueId}`);
                toastSuccess(t('tasks.deleteQueue', null, '删除队列'));
                onChanged?.();
                onClose?.();
            } catch (error) {
                toastError(errText(error, t('tasks.deleteQueueFailed', null, '删除批量任务队列失败')));
            }
        }
    });

    const openTaskModal = (record) => setTaskModal({
        visible: true,
        taskId: record?.id || '',
        message: record?.message || ''
    });

    const saveTask = async () => {
        const message = taskModal.message.trim();
        if (!message) {
            toastError(t('tasks.taskMessageRequired', null, '任务消息不能为空'));
            return;
        }
        try {
            if (taskModal.taskId) {
                await api.put(`/batch-tasks/${queueId}/tasks/${taskModal.taskId}`, { message });
            } else {
                await api.post(`/batch-tasks/${queueId}/tasks`, { message });
            }
            toastSuccess(t('common.save', null, '保存'));
            setTaskModal({ visible: false, taskId: '', message: '' });
            refresh();
        } catch (error) {
            toastError(errText(error, taskModal.taskId
                ? t('tasks.saveTaskFailed', null, '保存任务失败')
                : t('tasks.addTaskFailed', null, '添加任务失败')));
        }
    };

    const runSingle = (record) => confirmAction({
        title: t('tasks.runSingleTask', null, '单条执行'),
        content: t('tasks.confirmRunSingleTask', null, '确定执行该任务？仅运行这一条，完成后队列会自动暂停，不会继续执行其他待执行项。'),
        okText: t('common.confirm', null, '确认'),
        danger: false,
        onOk: async () => {
            try {
                await api.post(`/batch-tasks/${queueId}/tasks/${record.id}/run`);
                toastSuccess(t('tasks.runSingleTask', null, '单条执行'));
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.runSingleTaskFailed', null, '单条执行失败')));
            }
        }
    });

    const removeTask = (record) => confirmAction({
        title: t('common.delete', null, '删除'),
        content: t('tasks.confirmDeleteTask', null, '确定要删除这个任务吗？'),
        okText: t('common.delete', null, '删除'),
        onOk: async () => {
            try {
                await api.del(`/batch-tasks/${queueId}/tasks/${record.id}`);
                toastSuccess(t('common.delete', null, '删除'));
                refresh();
            } catch (error) {
                toastError(errText(error, t('tasks.deleteTaskFailed', null, '删除任务失败')));
            }
        }
    });

    const canRunSingle = (record) => Boolean(
        canWrite
        && queue
        && record.status !== 'running'
        && queue.status !== 'running'
        && ['pending', 'paused', 'completed', 'cancelled'].includes(queue.status)
    );

    const columns = [
        {
            title: '#',
            dataIndex: 'id',
            width: 58,
            render: (value, record) => {
                const index = (queue?.tasks || []).findIndex(item => item.id === record.id);
                return <Text type="tertiary" size="small">{index + 1}</Text>;
            }
        },
        {
            title: t('tasks.title', null, '任务'),
            dataIndex: 'message',
            render: (value, record) => (
                <span className="task-cell-message" title={value}>{value || t('tasks.taskIncomplete', null, '任务信息不完整')}</span>
            )
        },
        {
            title: t('projects.status', null, '状态'),
            dataIndex: 'status',
            width: 96,
            render: value => <TaskStatusTag status={value} />
        },
        {
            title: t('tasks.duration', null, '执行时长'),
            dataIndex: 'startedAt',
            width: 110,
            render: (value, record) => (
                <Text size="small">{value ? durationText(value, record.completedAt) : '—'}</Text>
            )
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 210,
            render: (value, record) => (
                <Space spacing={2} wrap>
                    <Button
                        size="small"
                        theme="borderless"
                        onClick={() => setResultModal({ visible: true, task: record })}
                    >
                        结果
                    </Button>
                    <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => openTaskModal(record)}>
                        {t('common.edit', null, '编辑')}
                    </Button>
                    <Button
                        size="small"
                        theme="borderless"
                        disabled={!canRunSingle(record)}
                        title={t('tasks.runSingleTaskUnavailable', null, '队列或任务执行中，暂无法单条执行')}
                        onClick={() => runSingle(record)}
                    >
                        {t('tasks.runSingleTask', null, '单条执行')}
                    </Button>
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => removeTask(record)}>
                        {t('common.delete', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    const statusView = queueStatusView(queue);
    const cronOn = queue?.scheduleMode === 'cron' && queue?.scheduleEnabled !== false;

    return (
        <SideSheet
            visible={visible}
            width={900}
            title={queue ? (queue.title || queue.id) : t('tasks.batchQueueTitle', null, '批量任务队列')}
            onCancel={onClose}
        >
            <ErrorBanner error={errorProp(detail.error)} onRetry={() => detail.reload()} />
            {queue ? (
                <div className="task-detail-stack">
                    <div className="task-detail-actions">
                        <Space wrap>
                            <Button
                                type="primary"
                                disabled={!canWrite || queue.status === 'running'}
                                onClick={start}
                            >
                                开始执行
                            </Button>
                            <Button disabled={!canWrite || queue.status !== 'running'} onClick={pause}>
                                暂停
                            </Button>
                            <Button
                                disabled={!canWrite || !['completed', 'cancelled'].includes(queue.status)}
                                onClick={rerun}
                            >
                                {t('tasks.retry', null, '重试')}
                            </Button>
                            <Button icon={<IconRefresh />} theme="borderless" onClick={() => detail.reload()} />
                        </Space>
                        <Space>
                            <Button icon={<IconPlus />} disabled={!canWrite} onClick={() => openTaskModal(null)}>
                                添加任务
                            </Button>
                            <Button type="danger" disabled={!canDelete} onClick={removeQueue}>
                                {t('tasks.deleteQueue', null, '删除队列')}
                            </Button>
                        </Space>
                    </div>

                    <div className="task-progress-block">
                        <div className="task-progress-head">
                            <Tag size="small" color={statusView.color}>{statusView.label}</Tag>
                            {statusView.sublabel ? <Text type="tertiary" size="small">{statusView.sublabel}</Text> : null}
                        </div>
                        <Progress percent={stats.percent} showInfo stroke="var(--semi-color-primary)" />
                        <div className="task-progress-legend">
                            <span>{t('tasks.totalLabel', null, '总计')} {stats.total}</span>
                            <span>{t('tasks.pendingLabel', null, '待执行')} {stats.pending}</span>
                            <span>{t('tasks.runningLabel', null, '执行中')} {stats.running}</span>
                            <span>{t('tasks.completedLabel', null, '已完成')} {stats.completed}</span>
                            <span>{t('tasks.failedLabel', null, '失败')} {stats.failed}</span>
                            <span>{t('tasks.cancelledLabel', null, '已取消')} {stats.cancelled}</span>
                            <span>受阻 {stats.blocked}</span>
                        </div>
                        <Text type="tertiary" size="small">
                            {cronOn
                                ? t('tasks.cronRunningProgressHint', null, '本轮执行中；下一整轮仍按 Cron 与「下次执行时间」排程')
                                : t('tasks.finishedProgressHint', null, '进度表示已结束的任务比例，包含失败和取消，不表示成功率。')}
                        </Text>
                    </div>

                    <KeyValueList
                        columns={3}
                        items={[
                            { label: t('tasks.queueIdLabel', null, '队列ID'), value: <code>{queue.id}</code> },
                            { label: t('tasks.createdTimeLabel', null, '创建时间'), value: formatDateTime(queue.createdAt) },
                            { label: 'Agent 模式', value: (AGENT_MODES.find(item => item.value === queue.agentMode) || AGENT_MODES[0]).label },
                            { label: '角色', value: queue.role || t('batchImportModal.defaultRole', null, '默认') },
                            { label: '项目', value: queue.projectId || '—' },
                            {
                                label: t('batchImportModal.hitlPolicy', null, '工具审批'),
                                value: (HITL_POLICIES.find(item => item.value === (queue.hitlPolicy || '')) || HITL_POLICIES[0]).label
                            },
                            { label: t('batchImportModal.concurrency', null, '并发数'), value: queue.concurrency || 1 },
                            {
                                label: t('batchImportModal.scheduleMode', null, '调度方式'),
                                value: queue.scheduleMode === 'cron'
                                    ? `Cron ${queue.cronExpr || ''}${cronOn ? '' : '（已关闭）'}`
                                    : t('batchImportModal.scheduleModeManual', null, '手工执行')
                            },
                            { label: '下一次执行', value: queue.nextRunAt ? formatDateTime(queue.nextRunAt) : '—' },
                            queue.lastRunError ? { label: '最近错误', value: <Text type="danger" size="small">{queue.lastRunError}</Text> } : null
                        ]}
                    />

                    <DataTable
                        columns={columns}
                        dataSource={queue.tasks || []}
                        rowKey="id"
                        size="small"
                        pagination={{ pageSize: 10 }}
                        emptyText={t('tasks.noValidTask', null, '没有有效的任务')}
                    />
                </div>
            ) : null}

            <Drawer
                visible={taskModal.visible}
                title={taskModal.taskId ? t('common.edit', null, '编辑') : '添加任务'}
                onCancel={() => setTaskModal({ visible: false, taskId: '', message: '' })}
                onOk={saveTask}
                okText={t('common.save', null, '保存')}
                cancelText={t('common.cancel', null, '取消')}
                width={620}
            >
                <TextArea
                    value={taskModal.message}
                    rows={5}
                    placeholder={t('batchImportModal.tasksListPlaceholder', null, '请输入任务列表，每行一个任务')}
                    onChange={value => setTaskModal(prev => ({ ...prev, message: value }))}
                />
            </Drawer>

            <Drawer
                visible={resultModal.visible}
                title={resultModal.task ? resultModal.task.message : '结果'}
                onCancel={() => setResultModal({ visible: false, task: null })}
                footer={(
                    <Space>
                        {resultModal.task?.conversationId ? (
                            <Button
                                onClick={() => {
                                    const id = resultModal.task.conversationId;
                                    setResultModal({ visible: false, task: null });
                                    onClose?.();
                                    navigate(`/chat?conversation=${encodeURIComponent(id)}`);
                                }}
                            >
                                {t('tasks.viewConversation', null, '查看对话')}
                            </Button>
                        ) : null}
                        <Button onClick={() => setResultModal({ visible: false, task: null })}>
                            {t('common.close', null, '关闭')}
                        </Button>
                    </Space>
                )}
                width={720}
            >
                <div className="task-detail-stack">
                    <KeyValueList
                        columns={2}
                        items={[
                            { label: t('projects.status', null, '状态'), value: <TaskStatusTag status={resultModal.task?.status} /> },
                            { label: t('tasks.startedAt', null, '开始时间'), value: formatDateTime(resultModal.task?.startedAt) },
                            { label: t('tasks.completedAt', null, '完成时间'), value: formatDateTime(resultModal.task?.completedAt) },
                            { label: t('tasks.duration', null, '执行时长'), value: durationText(resultModal.task?.startedAt, resultModal.task?.completedAt) },
                            resultModal.task?.conversationId
                                ? { label: t('tasks.conversationIdLabel', null, '对话ID'), value: <code>{resultModal.task.conversationId}</code> }
                                : null
                        ]}
                    />
                    {resultModal.task?.error ? (
                        <div>
                            <Text strong type="danger">
                                {resultModal.task.status === 'blocked' ? '受阻原因' : t('tasks.statusFailed', null, '执行失败')}
                            </Text>
                            <pre className="task-result-pre">{resultModal.task.error}</pre>
                        </div>
                    ) : null}
                    {resultModal.task?.result
                        ? <MarkdownView content={resultModal.task.result} />
                        : <Text type="tertiary">暂无结果</Text>}
                </div>
            </Drawer>
        </SideSheet>
    );
}
