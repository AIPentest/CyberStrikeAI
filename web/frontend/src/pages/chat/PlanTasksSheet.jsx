import { Button, Empty, Progress, SideSheet, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';

const { Text } = Typography;

const TASK_STATUS = {
    pending: { color: 'grey', label: '待处理' },
    in_progress: { color: 'blue', label: '进行中' },
    completed: { color: 'green', label: '已完成' },
    blocked: { color: 'orange', label: '阻塞' },
    deleted: { color: 'grey', label: '已删除' }
};

function PlanTasksPanel({ plan, loading, onRefresh }) {
    const t = useT();
    const tasks = Array.isArray(plan?.tasks)
        ? plan.tasks.filter(task => String(task.status || '').toLowerCase() !== 'deleted')
        : [];
    const total = Number(plan?.total) || tasks.length;
    const completed = Number(plan?.completed) || tasks.filter(task => task.status === 'completed').length;
    const percent = total ? Math.round((completed / total) * 100) : 0;

    return (
        <div className="chat-plan">
            <div className="chat-plan-toolbar">
                <Text type="tertiary" size="small">
                    {plan?.running ? t('chat.planRunning', null, '任务执行中') : t('chat.planIdle', null, '当前没有运行中的任务')}
                </Text>
                <Button size="small" icon={<IconRefresh />} onClick={onRefresh} loading={loading}>
                    {t('common.refresh', null, '刷新')}
                </Button>
            </div>
            <Progress percent={percent} showInfo />
            <Text type="tertiary" size="small">
                {t('chat.planSummary', { total, done: completed }, `共 ${total} 项，已完成 ${completed} 项`)}
            </Text>
            {!tasks.length && (
                <Empty
                    title={t('chat.planEmpty', null, '暂无执行计划')}
                    description={t('chat.planEmptyHint', null, '代理调用任务工具后，这里会显示任务看板。')}
                    style={{ padding: 24 }}
                />
            )}
            <div className="chat-plan-list">
                {tasks.map((task, index) => {
                    const status = String(task.status || 'pending').toLowerCase();
                    const meta = TASK_STATUS[status] || TASK_STATUS.pending;
                    return (
                        <div className={`chat-plan-item ${status === 'in_progress' ? 'is-active' : ''}`} key={task.id || index}>
                            <div className="chat-plan-item-head">
                                <span className="chat-plan-index">{index + 1}</span>
                                <span className="chat-plan-subject">{task.subject || task.activeForm || '—'}</span>
                                <Tag size="small" color={meta.color} shape="circle">{meta.label}</Tag>
                            </div>
                            {task.description && <div className="chat-plan-desc">{task.description}</div>}
                        </div>
                    );
                })}
            </div>
        </div>
    );
}

export default function PlanTasksSheet({ visible, onClose, plan, loading, onRefresh }) {
    const t = useT();
    const title = t('chat.planTasks', null, '执行计划');
    return (
        <SideSheet
            title={title}
            aria-label={title}
            visible={visible}
            onCancel={onClose}
            width="min(100%, 560px)"
            placement="right"
        >
            <PlanTasksPanel plan={plan} loading={loading} onRefresh={onRefresh} />
        </SideSheet>
    );
}
