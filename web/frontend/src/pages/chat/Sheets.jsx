/** 会话详情侧滑：Token 用量、计划任务。面板与侧滑壳分离，便于复用与自检。 */
import { useCallback, useEffect, useState } from 'react';
import { Button, Empty, Progress, SideSheet, Spin, Table, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { ErrorBanner } from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { formatDateTime, formatNumber } from '../../lib/format';

const { Text } = Typography;

const TASK_STATUS = {
    pending: { color: 'grey', label: '待处理' },
    in_progress: { color: 'blue', label: '进行中' },
    completed: { color: 'green', label: '已完成' },
    blocked: { color: 'orange', label: '阻塞' },
    deleted: { color: 'grey', label: '已删除' }
};

function StatCell({ label, value }) {
    return (
        <div className="chat-usage-cell">
            <Text type="tertiary" size="small">{label}</Text>
            <div className="chat-usage-value">{formatNumber(value || 0)}</div>
        </div>
    );
}

export function TokenUsagePanel({ conversationId }) {
    const t = useT();
    const [data, setData] = useState(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);

    const load = useCallback(async () => {
        if (!conversationId) return;
        setLoading(true);
        setError(null);
        try {
            const payload = await api.get(`/conversations/${encodeURIComponent(conversationId)}/token-usage`, {
                days: 365,
                limit: 200
            });
            setData(payload);
        } catch (err) {
            setError(err);
            setData(null);
        } finally {
            setLoading(false);
        }
    }, [conversationId]);

    useEffect(() => {
        load();
    }, [load]);

    const summary = data?.summary || {};
    const byModel = Array.isArray(data?.byModel) ? data.byModel : [];
    const recent = Array.isArray(data?.recent) ? data.recent : [];

    const modelColumns = [
        { title: t('chat.usageModel', null, '模型'), dataIndex: 'key', render: value => <Text size="small">{value || '—'}</Text> },
        { title: t('chat.usageCalls', null, '调用'), dataIndex: 'modelCalls', width: 80, render: value => formatNumber(value || 0) },
        { title: t('chat.usagePrompt', null, '输入'), dataIndex: 'promptTokens', width: 100, render: value => formatNumber(value || 0) },
        { title: t('chat.usageCompletion', null, '输出'), dataIndex: 'completionTokens', width: 100, render: value => formatNumber(value || 0) },
        { title: t('chat.usageTotal', null, '合计'), dataIndex: 'totalTokens', width: 100, render: value => <Text strong>{formatNumber(value || 0)}</Text> }
    ];

    const recentColumns = [
        { title: t('chat.usageTime', null, '时间'), dataIndex: 'createdAt', render: value => <Text size="small">{formatDateTime(value)}</Text> },
        { title: t('chat.usageModel', null, '模型'), dataIndex: 'model' },
        { title: t('chat.usageOrchestration', null, '编排'), dataIndex: 'orchestration', render: value => (value ? <Tag size="small">{value}</Tag> : '—') },
        { title: t('chat.usageTotal', null, '合计'), dataIndex: 'totalTokens', width: 90, render: value => formatNumber(value || 0) }
    ];

    return (
        <div className="chat-usage">
            <div className="chat-usage-toolbar">
                <Text type="tertiary" size="small">
                    {t('chat.usageScopeHint', null, '统计范围为该会话近 365 天的模型调用。')}
                </Text>
                <Button size="small" icon={<IconRefresh />} onClick={load} loading={loading}>
                    {t('common.refresh', null, '刷新')}
                </Button>
            </div>
            <ErrorBanner error={error} onRetry={load} />
            {loading && !data && <div className="chat-loading"><Spin /></div>}
            {data && (
                <>
                    <div className="chat-usage-grid">
                        <StatCell label={t('chat.piUsageModelCalls', null, '模型调用')} value={summary.modelCalls} />
                        <StatCell label={t('chat.piUsagePromptTokens', null, '输入 tokens')} value={summary.promptTokens} />
                        <StatCell label={t('chat.piUsageCompletionTokens', null, '输出 tokens')} value={summary.completionTokens} />
                        <StatCell label={t('chat.piUsageTotalTokens', null, '总 tokens')} value={summary.totalTokens} />
                        <StatCell label={t('chat.piUsageCachedTokens', null, '缓存 tokens')} value={summary.cachedTokens} />
                        <StatCell label={t('chat.piUsageReasoningTokens', null, '推理 tokens')} value={summary.reasoningTokens} />
                    </div>
                    <Text strong className="chat-sheet-subtitle">{t('chat.usageByModel', null, '按模型')}</Text>
                    <Table
                        size="small"
                        rowKey="key"
                        columns={modelColumns}
                        dataSource={byModel}
                        pagination={false}
                        empty={<Empty title={t('common.noData', null, '暂无数据')} description={null} style={{ padding: 16 }} />}
                    />
                    <Text strong className="chat-sheet-subtitle">{t('chat.usageRecent', null, '最近调用')}</Text>
                    <Table
                        size="small"
                        rowKey={(row, index) => `${row.createdAt || ''}-${index}`}
                        columns={recentColumns}
                        dataSource={recent}
                        pagination={{ pageSize: 8 }}
                        empty={<Empty title={t('common.noData', null, '暂无数据')} description={null} style={{ padding: 16 }} />}
                    />
                </>
            )}
        </div>
    );
}

export function TokenUsageSheet({ visible, onClose, conversationId }) {
    const t = useT();
    return (
        <SideSheet
            title={t('chat.tokenUsage', null, 'Token 用量')}
            visible={visible}
            onCancel={onClose}
            width={680}
            placement="right"
        >
            <TokenUsagePanel conversationId={conversationId} />
        </SideSheet>
    );
}

export function PlanTasksPanel({ plan, loading, onRefresh }) {
    const t = useT();
    const tasks = Array.isArray(plan?.tasks)
        ? plan.tasks.filter(task => String(task.status || '').toLowerCase() !== 'deleted')
        : [];
    const total = Number(plan?.total) || tasks.length;
    const completed = Number(plan?.completed) || tasks.filter(task => task.status === 'completed').length;
    const percent = total ? Math.round((completed / total) * 100) : 0;

    return (
        <div className="chat-plan">
            <div className="chat-usage-toolbar">
                <Text type="tertiary" size="small">
                    {plan?.running ? t('chat.planRunning', null, '任务执行中') : t('chat.planIdle', null, '当前没有运行中的任务')}
                </Text>
                <Button size="small" icon={<IconRefresh />} onClick={onRefresh} loading={loading}>
                    {t('common.refresh', null, '刷新')}
                </Button>
            </div>
            <Progress percent={percent} showInfo />
            <Text type="tertiary" size="small">
                {t('chat.planSummary', null, '共 {{total}} 项，已完成 {{done}} 项', { total, done: completed })}
            </Text>
            {!tasks.length && (
                <Empty
                    title={t('chat.planEmpty', null, '暂无计划任务')}
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

export function PlanTasksSheet({ visible, onClose, plan, loading, onRefresh }) {
    const t = useT();
    return (
        <SideSheet
            title={t('chat.planTasks', null, '计划任务')}
            visible={visible}
            onCancel={onClose}
            width={560}
            placement="right"
        >
            <PlanTasksPanel plan={plan} loading={loading} onRefresh={onRefresh} />
        </SideSheet>
    );
}
