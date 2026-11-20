/** MCP 监控统计卡与工具调用排行。 */
import { useMemo } from 'react';
import { Tag } from '@douyinfe/semi-ui';
import { SectionCard, StatGrid, DataTable } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { formatDateTime, formatPercent } from '../../lib/format';
import { formatDurationMs, formatPercentValue, successRate, toolDisplayName } from './utils';

/**
 * 统计卡：调用次数 / 成功率 / 平均耗时 / 失败数。
 * GET /monitor/stats 不含耗时字段，平均耗时由调用方按当前列表的计时记录计算后传入。
 */
export function MonitorStats({ summary, loading, avgMs = null, avgSamples = 0 }) {
    const t = useT();
    const rate = successRate(summary);
    return (
        <StatGrid
            columns={4}
            metrics={[
                {
                    id: 'calls',
                    label: t('mcpMonitor.totalCalls', null, '调用次数'),
                    value: summary ? (summary.totalCalls ?? 0) : '—',
                    detail: summary
                        ? t('mcpMonitor.statsFromAllTools', null, '全部工具累计，含已拦截调用')
                        : undefined,
                    loading
                },
                {
                    id: 'rate',
                    label: t('mcpMonitor.successRate', null, '成功率'),
                    value: formatPercentValue(rate),
                    detail: summary
                        ? `${t('common.ok', null, '成功')} ${summary.successCalls ?? 0} / ${t('mcpMonitor.failedColumn', null, '失败')} ${summary.failedCalls ?? 0}`
                        : undefined,
                    tone: rate === null ? undefined : (rate >= 0.95 ? 'success' : rate >= 0.8 ? 'warning' : 'danger'),
                    loading
                },
                {
                    id: 'avg',
                    label: '平均耗时',
                    value: avgMs === null ? '—' : formatDurationMs(avgMs),
                    detail: avgSamples > 0
                        ? `按当前页 ${avgSamples} 条计时记录估算`
                        : '当前页暂无可计时的执行记录',
                    loading
                },
                {
                    id: 'failed',
                    label: t('mcpMonitor.failedMetric', null, '失败数'),
                    value: summary ? (summary.failedCalls ?? 0) : '—',
                    detail: summary
                        ? `${t('mcpMonitor.blockedCount', { n: summary.blockedCalls ?? 0 }, `安全拦截 ${summary.blockedCalls ?? 0}`)} · ${t('mcpMonitor.lastCall', null, '最近调用')} ${summary.lastCallTime ? formatDateTime(summary.lastCallTime) : '—'}`
                        : undefined,
                    tone: summary && summary.failedCalls > 0 ? 'danger' : undefined,
                    loading
                }
            ]}
        />
    );
}

export function ToolRankCard({ topTools, loading, onFilterTool, activeTool }) {
    const t = useT();
    const rows = useMemo(() => (topTools || []).map(item => {
        const total = Number(item.totalCalls) || 0;
        const blocked = Number(item.blockedCalls) || 0;
        const success = Number(item.successCalls) || 0;
        const denominator = total - blocked;
        return {
            ...item,
            key: item.toolName,
            displayName: toolDisplayName(item.toolName),
            rate: denominator > 0 ? success / denominator : null
        };
    }), [topTools]);

    const columns = [
        {
            title: t('mcpMonitor.columnTool', null, '工具'),
            dataIndex: 'displayName',
            render: (value, record) => (
                <button
                    type="button"
                    className={`mcp-monitor-tool-link ${activeTool && activeTool === record.toolName ? 'mcp-monitor-link-active' : ''}`}
                    onClick={() => onFilterTool?.(record.toolName)}
                >
                    {value || t('mcpMonitor.unknownTool', null, '未知工具')}
                </button>
            )
        },
        {
            title: t('mcpMonitor.columnCalls', null, '调用'),
            dataIndex: 'totalCalls',
            width: 90,
            render: value => value ?? 0
        },
        {
            title: t('mcpMonitor.successColumn', null, '成功'),
            dataIndex: 'successCalls',
            width: 90,
            render: value => value ?? 0
        },
        {
            title: t('mcpMonitor.failedColumn', null, '失败'),
            dataIndex: 'failedCalls',
            width: 90,
            render: value => <span className={value > 0 ? 'mcp-monitor-danger' : undefined}>{value ?? 0}</span>
        },
        {
            title: t('mcpMonitor.blockedColumn', null, '拦截'),
            dataIndex: 'blockedCalls',
            width: 90,
            render: value => (value ? <Tag size="small" color="orange">{value}</Tag> : 0)
        },
        {
            title: t('mcpMonitor.columnSuccessRate', null, '成功率'),
            dataIndex: 'rate',
            width: 110,
            render: value => (value === null ? '—' : formatPercent(value, 1))
        }
    ];

    return (
        <SectionCard
            title={t('mcpMonitor.toolStatsTitle', null, '工具统计')}
            description={t('mcpMonitor.toolStatsHint', null, '点击工具名筛选下方执行记录')}
        >
            <DataTable
                columns={columns}
                dataSource={rows}
                rowKey="key"
                loading={loading}
                pagination={false}
                size="small"
                emptyText={t('mcpMonitor.noStatsData', null, '暂无统计数据')}
            />
        </SectionCard>
    );
}
