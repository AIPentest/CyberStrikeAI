/** Token 用量：GET /usage/tokens（days / limit / project_id），汇总 + 按日 + 按模型 + 最近记录。 */
import { useCallback, useMemo, useState } from 'react';
import { Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useDebounced, useResource } from '../../../components/hooks';
import {
    DataTable, ErrorBanner, SectionCard, StatGrid
} from '../../../components/ui';
import { formatDateTime, formatNumber, truncate } from '../../../lib/format';
import { LoadingBlock, PermissionBanner } from './common';

const { Text } = Typography;

function count(value) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? Math.trunc(number) : 0;
}

function cacheRate(summary) {
    const input = count(summary?.promptTokens);
    const cached = count(summary?.cachedTokens);
    if (!input) return '—';
    return `${(Math.min(cached, input) / input * 100).toFixed(1)}%`;
}

const BREAKDOWN_COLUMNS = [
    ['events', 'settingsDisplay.events', '记录数'],
    ['modelCalls', 'dashboard.usageCalls', '模型调用'],
    ['promptTokens', 'dashboard.usageInput', '输入令牌'],
    ['completionTokens', 'dashboard.usageOutput', '输出令牌'],
    ['totalTokens', 'dashboard.usageTotal', '总令牌数'],
    ['cachedTokens', 'dashboard.usageCacheRead', '缓存读取']
];

function breakdownColumns(t, firstTitle) {
    return [
        { title: firstTitle, dataIndex: 'key', render: (value, row) => <Text strong>{row.label || value || '—'}</Text> },
        ...BREAKDOWN_COLUMNS.map(([dataIndex, key, fallback]) => ({
            title: t(key, null, fallback),
            dataIndex,
            align: 'right',
            render: value => formatNumber(count(value))
        }))
    ];
}

export default function TokenUsageSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('dashboard:read');
    const [days, setDays] = useState(7);
    const [limit, setLimit] = useState(20);
    const [projectInput, setProjectInput] = useState('');
    const projectId = useDebounced(projectInput.trim(), 400);

    const loader = useCallback(
        () => api.get('/usage/tokens', { days, limit, project_id: projectId || undefined }),
        [days, limit, projectId]
    );
    const usage = useResource(loader, [days, limit, projectId, canRead], { skip: !canRead });

    const summary = usage.data?.summary || {};
    const metrics = useMemo(() => ([
        { id: 'total', label: t('tokenUsageUI.usageTotal', null, '总 Token'), value: formatNumber(count(summary.totalTokens)) },
        { id: 'input', label: t('tokenUsageUI.usageInput', null, '输入 Token'), value: formatNumber(count(summary.promptTokens)) },
        { id: 'output', label: t('tokenUsageUI.usageOutput', null, '输出 Token'), value: formatNumber(count(summary.completionTokens)) },
        { id: 'cache', label: t('tokenUsageUI.usageCacheRead', null, '缓存读取'), value: formatNumber(count(summary.cachedTokens)) },
        { id: 'cacheWrite', label: t('tokenUsageUI.usageCacheWrite', null, '缓存写入'), value: formatNumber(count(summary.cacheWriteTokens)) },
        { id: 'reasoning', label: t('tokenUsageUI.usageReasoning', null, '推理 Token'), value: formatNumber(count(summary.reasoningTokens)) },
        { id: 'calls', label: t('tokenUsageUI.usageCalls', null, '模型调用'), value: formatNumber(count(summary.modelCalls)) },
        {
            id: 'rate',
            label: t('tokenUsageUI.usageCacheRate', null, '缓存命中率'),
            value: cacheRate(summary),
            detail: t('tokenUsageUI.cacheNote', null, '缓存读取是输入的一部分，不重复计入总量')
        }
    ]), [summary, t]);

    if (!canRead) {
        return <PermissionBanner permission="dashboard:read" label={t('settings.nav.tokenUsage', null, '读取 Token 用量')} />;
    }
    if (usage.loading && !usage.data) return <LoadingBlock rows={6} />;

    const byDay = Array.isArray(usage.data?.byDay) ? usage.data.byDay : [];
    const byModel = Array.isArray(usage.data?.byModel) ? usage.data.byModel : [];
    const recent = Array.isArray(usage.data?.recent) ? usage.data.recent : [];
    const today = usage.data?.today || {};

    return (
        <>
            <ErrorBanner error={usage.error} onRetry={usage.reload} />
            <SectionCard
                title={t('settings.nav.tokenUsage', null, 'Token 用量')}
                description={t('tokenUsageUI.scopeNote', null, '统计口径与说明：按服务端本地日期聚合，当前登录用户可见的会话范围内统计。')}
                extra={(
                    <Space>
                        <Text type="tertiary" size="small">{t('settings.usageDays', null, '时间范围')}</Text>
                        <Select
                            value={days}
                            onChange={value => setDays(Number(value))}
                            style={{ width: 120 }}
                            optionList={[
                                { value: 7, label: '7 天' },
                                { value: 30, label: '30 天' },
                                { value: 90, label: '90 天' }
                            ]}
                        />
                        <Select
                            value={limit}
                            onChange={value => setLimit(Number(value))}
                            style={{ width: 130 }}
                            optionList={[
                                { value: 20, label: '最近 20 条' },
                                { value: 50, label: '最近 50 条' },
                                { value: 100, label: '最近 100 条' }
                            ]}
                        />
                        <Input
                            value={projectInput}
                            onChange={value => setProjectInput(value)}
                            placeholder={t('settingsDisplay.projectIdOptional', null, '项目 ID（可选）')}
                            style={{ width: 180 }}
                            showClear
                        />
                    </Space>
                )}
            >
                <StatGrid metrics={metrics} columns={4} />
                <Space spacing={8}>
                    <Tag size="small" color="blue">
                        {t('tokenUsageUI.todayTotal', { total: formatNumber(count(today.totalTokens)) }, `今日 ${formatNumber(count(today.totalTokens))} tokens`)}
                    </Tag>
                    <Tag size="small" color="grey">
                        {t('tokenUsageUI.todayCalls', { calls: formatNumber(count(today.modelCalls)) }, `今日 ${formatNumber(count(today.modelCalls))} 次模型调用`)}
                    </Tag>
                    <Tag size="small" color="green">
                        {t('tokenUsageUI.todayCache', { rate: cacheRate(today) }, `今日缓存命中 ${cacheRate(today)}`)}
                    </Tag>
                </Space>
            </SectionCard>

            <SectionCard title={t('tokenUsageUI.trendTitle', null, '每日用量')}>
                <DataTable
                    columns={breakdownColumns(t, t('tokenUsageUI.colDay', null, '日期'))}
                    dataSource={byDay}
                    rowKey="key"
                    loading={usage.loading}
                    pagination={false}
                    emptyText={t('tokenUsageUI.noDailyData', null, '暂无每日用量记录')}
                />
            </SectionCard>

            <SectionCard title={t('tokenUsageUI.modelTitle', null, '按模型')}>
                <DataTable
                    columns={breakdownColumns(t, t('tokenUsageUI.colModel', null, '模型'))}
                    dataSource={byModel}
                    rowKey="key"
                    loading={usage.loading}
                    pagination={false}
                    emptyText={t('common.noData', null, '暂无数据')}
                />
            </SectionCard>

            <SectionCard title={t('tokenUsageUI.recentTitle', null, '最近记录')}>
                <DataTable
                    columns={[
                        {
                            title: t('tokenUsageUI.colTime', null, '时间'),
                            dataIndex: 'createdAt',
                            width: 180,
                            render: value => formatDateTime(value)
                        },
                        { title: t('tokenUsageUI.colModel', null, '模型'), dataIndex: 'model', render: value => value || '—' },
                        { title: t('settingsDisplay.usageSource', null, '用量来源'), dataIndex: 'source', render: value => value || '—' },
                        { title: t('settingsDisplay.orchestration', null, '协作方式'), dataIndex: 'orchestration', render: value => value || '—' },
                        {
                            title: t('settingsDisplay.tokensBreakdown', null, '输入 / 输出 / 总令牌数'),
                            align: 'right',
                            render: (_, row) => `${formatNumber(count(row.promptTokens))} / ${formatNumber(count(row.completionTokens))} / ${formatNumber(count(row.totalTokens))}`
                        },
                        {
                            title: t('settingsDisplay.conversation', null, '对话'),
                            dataIndex: 'conversationId',
                            width: 200,
                            render: value => <Text type="tertiary">{truncate(value || '—', 18)}</Text>
                        }
                    ]}
                    dataSource={recent}
                    rowKey="id"
                    loading={usage.loading}
                    pagination={{ pageSize: 10, showSizeChanger: false }}
                    emptyText={t('common.noData', null, '暂无数据')}
                />
            </SectionCard>
        </>
    );
}
