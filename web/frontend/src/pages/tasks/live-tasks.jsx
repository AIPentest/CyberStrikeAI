/** 运行中任务（Agent Loop）：只读视图 + 自动刷新 + 跳转会话。 */
import { useNavigate } from 'react-router-dom';
import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, SectionCard } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { durationText, errorProp } from './helpers';

const { Text } = Typography;

const RUN_STATUS = {
    running: { label: '执行中', color: 'blue' },
    cancelling: { label: '取消中', color: 'amber' },
    cleaning: { label: '清理中', color: 'amber' }
};

export default function LiveTasks() {
    const t = useT();
    const navigate = useNavigate();
    const tasks = useResource(() => api.get('/agent-loop/tasks'), []);

    useInterval(() => tasks.reload({ silent: true }), 6000);

    const rows = Array.isArray(tasks.data?.tasks) ? tasks.data.tasks : [];

    const columns = [
        {
            title: t('tasks.title', null, '任务'),
            dataIndex: 'title',
            render: (value, record) => (
                <div className="task-live-title">
                    <span className="task-cell-message" title={value || record.message}>
                        {value || record.message || t('tasks.unnamedTask', null, '未命名任务')}
                    </span>
                    <Text type="tertiary" size="small"><code>{record.runId || '—'}</code></Text>
                </div>
            )
        },
        {
            title: t('projects.status', null, '状态'),
            dataIndex: 'status',
            width: 110,
            render: value => {
                const meta = RUN_STATUS[String(value || '')] || { label: value || '—', color: 'blue' };
                return <Tag size="small" color={meta.color}>{meta.label}</Tag>;
            }
        },
        {
            title: t('tasks.conversationIdLabel', null, '对话ID'),
            dataIndex: 'conversationId',
            width: 230,
            render: value => <Text type="tertiary" size="small"><code>{value || '—'}</code></Text>
        },
        {
            title: t('tasks.startedAt', null, '开始时间'),
            dataIndex: 'startedAt',
            width: 165,
            render: value => <Text size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('tasks.duration', null, '执行时长'),
            dataIndex: 'startedAt',
            width: 110,
            render: value => <Text size="small">{durationText(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'conversationId',
            width: 110,
            render: value => (
                <Space>
                    <Button
                        size="small"
                        theme="borderless"
                        disabled={!value}
                        onClick={() => navigate(`/chat?conversation=${encodeURIComponent(value)}`)}
                    >
                        {t('tasks.viewConversation', null, '查看对话')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <SectionCard
            title={`${t('tasks.runningLabel', null, '执行中')}任务（Agent Loop）`}
            description="当前进程中正在执行的 Agent 任务；每 6 秒自动刷新。"
            extra={(
                <Button icon={<IconRefresh />} theme="borderless" loading={tasks.loading} onClick={() => tasks.reload()}>
                    {t('common.refresh', null, '刷新')}
                </Button>
            )}
        >
            <ErrorBanner error={errorProp(tasks.error)} onRetry={() => tasks.reload()} />
            <DataTable
                columns={columns}
                dataSource={rows}
                rowKey={record => record.runId || record.conversationId || 'unknown-task'}
                loading={tasks.loading}
                pagination={{ pageSize: 10 }}
                emptyText={t('tasks.noMatchingTasks', null, '当前没有符合条件的任务')}
            />
        </SectionCard>
    );
}
