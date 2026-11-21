/**
 * C2 会话任务列表：查看结果、取消与删除单条任务。
 */
import { useState } from 'react';
import { Button, Space, Tag } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import { Drawer, DataTable, ErrorBanner, JsonViewer, confirmAction, toastError, toastSuccess } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import * as c2 from './components/c2-api';
import { TASK_STATUS_COLOR, TimeText, formatDurationMs } from './components/c2-shared';

export default function SessionTasks({ session, canDelete, onChanged }) {
    const t = useT();
    const [detail, setDetail] = useState(null);
    const { data, loading, error, reload } = useResource(
        () => c2.listTasks({ session_id: session.id, limit: 100 }),
        [session.id]
    );
    const tasks = Array.isArray(data?.tasks) ? data.tasks : [];

    useInterval(() => reload({ silent: true }), 5000);

    const cancel = async task => {
        try {
            await c2.cancelTask(task.id);
            toastSuccess(t('c2.tasks.toastCancelled', null, '任务已取消'));
            reload({ silent: true });
            onChanged?.();
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
                    reload({ silent: true });
                    onChanged?.();
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const columns = [
        { title: t('c2.tasks.colType', null, '类型'), dataIndex: 'taskType', width: 120 },
        {
            title: t('c2.tasks.colCommand', null, '命令'),
            dataIndex: 'payload',
            render: value => {
                const command = value?.command || value?.path || '';
                return <span className="c2-mono">{command || '—'}</span>;
            }
        },
        {
            title: t('c2.tasks.colStatus', null, '状态'),
            dataIndex: 'status',
            width: 110,
            render: status => <Tag size="small" color={TASK_STATUS_COLOR[status] || 'grey'}>{t(`c2.tasks.${status || 'queued'}`, null, status || '—')}</Tag>
        },
        {
            title: t('c2.tasks.colDuration', null, '耗时'),
            dataIndex: 'durationMs',
            width: 100,
            render: value => formatDurationMs(value)
        },
        { title: t('c2.tasks.colCreated', null, '创建时间'), dataIndex: 'createdAt', width: 140, render: value => <TimeText value={value} /> },
        {
            title: t('c2.tasks.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 200,
            render: (_, task) => (
                <Space spacing={4} wrap>
                    <Button size="small" theme="borderless" onClick={() => setDetail(task)}>{t('c2.tasks.view', null, '查看')}</Button>
                    {['queued', 'sent', 'running'].includes(task.status) && (
                        <Button size="small" theme="borderless" onClick={() => cancel(task)}>{t('c2.tasks.cancelBtn', null, '取消')}</Button>
                    )}
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => remove(task)}>
                        {t('c2.tasks.deleteBtn', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="c2-session-tasks">
            <ErrorBanner error={error} onRetry={() => reload()} />
            <DataTable
                columns={columns}
                dataSource={tasks}
                rowKey="id"
                loading={loading}
                onRefresh={() => reload()}
                pagination={{ pageSize: 10 }}
                emptyText={t('c2.tasks.emptySession', null, '该会话暂无任务')}
            />

            <Drawer
                visible={Boolean(detail)}
                title={t('c2.tasks.modalTitle', null, '任务详情')}
                width={720}
                onCancel={() => setDetail(null)}
                footer={<Button onClick={() => setDetail(null)}>{t('c2.tasks.close', null, '关闭')}</Button>}
            >
                {detail && (
                    <div className="c2-task-detail">
                        <div className="c2-task-detail-meta">
                            <span className="c2-mono">{detail.id}</span>
                            <Tag size="small" color={TASK_STATUS_COLOR[detail.status] || 'grey'}>{detail.status}</Tag>
                            <span className="c2-muted">{formatDateTime(detail.createdAt)}</span>
                        </div>
                        {detail.payload && <JsonViewer title={t('c2.tasks.labelPayload', null, '参数')} value={detail.payload} collapsed />}
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
