/** 概览：统计、目标与验收要求、事件时间线。 */
import { useState } from 'react';
import { Button, Empty, Space, Switch, Tag, Timeline, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { useInterval } from '../../components/hooks';
import { SectionCard, StatGrid, ErrorBanner, StatusTag, KeyValueList } from '../../components/ui';
import { formatDateTime, formatNumber } from '../../lib/format';
import { countsProgress, eventText, formatElapsed, isActive, statusText } from './labels';

const { Text, Paragraph } = Typography;

const TIMELINE_TYPE = {
    blocked: 'error',
    waiting_input: 'warning',
    completed: 'success',
    stop_requested: 'warning',
    retry: 'warning'
};

function ActiveElapsed({ startedAt, seconds }) {
    const [now, setNow] = useState(Date.now);
    useInterval(() => setNow(Date.now()), 1000);
    const currentRunSeconds = Math.max(0, Math.floor((now - startedAt) / 1000));
    return formatElapsed(seconds + currentRunSeconds);
}

export default function OverviewTab({
    task, canWrite, events, eventsError, autoRefresh, onAutoRefreshChange,
    onRefreshEvents, onGoTab, onGuidance, onAnswer
}) {
    const t = useT();
    const counts = {};
    (task.steps || []).forEach(step => {
        counts[step.status] = (counts[step.status] || 0) + 1;
    });
    const progress = countsProgress(counts, (task.steps || []).length);
    const reviewCount = task.review_count
        || (task.steps || []).filter(step => ['uncertain', 'cleanup_unconfirmed'].includes(step.status)).length;
    const activeSeconds = task.active_seconds ?? 0;
    const startedAt = isActive(task) ? Date.parse(task.run_started_at) : NaN;
    let statusDetail = formatElapsed(activeSeconds);
    if (task.archived) {
        statusDetail = '已归档';
    } else if (Number.isFinite(startedAt)) {
        statusDetail = <ActiveElapsed key={startedAt} startedAt={startedAt} seconds={activeSeconds} />;
    }
    const metrics = [
        {
            id: 'status',
            label: t('deepAssessments.status', null, '状态'),
            value: <StatusTag status={task.status} label={statusText(t, task.status)} />,
            detail: statusDetail
        },
        {
            id: 'progress',
            label: '意图完成度',
            value: `${progress.percent}%`,
            detail: `${progress.done} / ${progress.total} 已结束`
        },
        {
            id: 'rounds',
            label: t('deepAssessments.rounds', null, '规划轮次'),
            value: formatNumber(task.rounds || 0)
        },
        {
            id: 'tokens',
            label: t('deepAssessments.tokens', null, '累计 Token'),
            value: formatNumber(task.tokens_used || 0)
        },
        {
            id: 'review',
            label: '待核对',
            value: formatNumber(reviewCount),
            detail: reviewCount ? '需要人工确认工具结果' : '无待核对记录'
        }
    ];

    const briefItems = [
        {
            label: t('deepAssessments.objective', null, '目标'),
            value: <Paragraph className="assessment-pre">{task.objective || '—'}</Paragraph>
        },
        {
            label: t('deepAssessments.scope', null, '补充限制与排除项'),
            value: <Paragraph className="assessment-pre">{task.scope || '—'}</Paragraph>
        },
        {
            label: t('deepAssessments.criteria', null, '验收条件'),
            value: <Paragraph className="assessment-pre">{task.criteria || '—'}</Paragraph>
        },
        {
            label: t('deepAssessments.strategy', null, '最近规划路线'),
            value: <Paragraph className="assessment-pre">{task.strategy || '—'}</Paragraph>
        },
        {
            label: t('deepAssessments.summary', null, '当前结论 / 缺口'),
            value: <Paragraph className="assessment-pre">{task.summary || '—'}</Paragraph>
        }
    ];
    if (task.question) {
        briefItems.push({
            label: t('deepAssessments.question', null, '待补充信息'),
            value: <Paragraph className="assessment-pre">{task.question}</Paragraph>
        });
    }

    const ordered = events.slice().reverse();
    const shown = ordered.slice(0, 120);

    return (
        <div className="assessment-tab">
            <StatGrid metrics={metrics} columns={5} />

            <SectionCard
                title="目标与测评要求"
                extra={(
                    <Space>
                        {canWrite && !task.archived && task.status !== 'completed' && task.status !== 'cancelled' ? (
                            <Button size="small" onClick={onGuidance}>补充方向</Button>
                        ) : null}
                        {task.question ? <Button size="small" theme="solid" type="primary" onClick={onAnswer}>答复</Button> : null}
                        <Button size="small" onClick={() => onGoTab?.('intents')}>查看意图</Button>
                    </Space>
                )}
            >
                <KeyValueList items={briefItems} columns={1} />
                <div className="assessment-facts-row">
                    <Text type="tertiary">自动续跑：{task.auto_resume ? '已开启' : '已关闭'}</Text>
                    <Text type="tertiary">并行步骤：{task.concurrency || '—'}</Text>
                    <Text type="tertiary">人工审批：{task.hitl_policy || '系统默认'}</Text>
                    <Text type="tertiary">最近更新：{formatDateTime(task.updated_at)}</Text>
                </div>
                {Array.isArray(task.guidance) && task.guidance.length ? (
                    <div className="assessment-guidance">
                        <Text strong>人工补充</Text>
                        {task.guidance.map((note, index) => (
                            <div key={`${index}-${note.slice(0, 8)}`} className="assessment-guidance-item">
                                <Text type="tertiary">#{index + 1}</Text>
                                <Paragraph className="assessment-pre">{note}</Paragraph>
                            </div>
                        ))}
                    </div>
                ) : null}
                <Paragraph type="tertiary" className="assessment-hint">
                    打开页面不会自动启动任务；自动续跑只针对服务重启前的执行，草稿、手动暂停和待核对任务不会自动开始。
                </Paragraph>
            </SectionCard>

            <SectionCard
                title={t('deepAssessments.activity', null, '运行记录')}
                description="事件按时间倒序展示；运行中每 10 秒自动拉取，页面隐藏时跳过。"
                extra={(
                    <Space>
                        <Text type="tertiary" size="small">自动刷新</Text>
                        <Switch size="small" checked={autoRefresh} onChange={value => onAutoRefreshChange?.(Boolean(value))} />
                        <Button size="small" onClick={onRefreshEvents}>立即刷新</Button>
                    </Space>
                )}
            >
                <ErrorBanner error={eventsError} onRetry={onRefreshEvents} />
                {shown.length === 0 ? (
                    <Empty title="暂无运行记录" description="开始测评后，规划、执行与核对事件会显示在这里。" style={{ padding: 32 }} />
                ) : (
                    <div className="assessment-timeline">
                        <Timeline>
                            {shown.map(event => (
                                <Timeline.Item
                                    key={event.id}
                                    time={formatDateTime(event.created_at)}
                                    type={TIMELINE_TYPE[event.kind] || 'default'}
                                >
                                    <div className="assessment-event-head">
                                        <Text strong>{eventText(t, event.kind)}</Text>
                                        {event.step_id ? <Tag size="small" color="grey">{event.step_id}</Tag> : null}
                                    </div>
                                    {event.message ? <div className="assessment-event-message">{event.message}</div> : null}
                                </Timeline.Item>
                            ))}
                        </Timeline>
                    </div>
                )}
            </SectionCard>
        </div>
    );
}
