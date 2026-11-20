/** 调用趋势：纯 DOM 柱状条（不引图表库），按 24h / 7d / 30d 分桶。 */
import { useMemo } from 'react';
import { Select } from '@douyinfe/semi-ui';
import { SectionCard, ErrorBanner, StatCard } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { formatNumber } from '../../lib/format';
import { formatBucketLabel, formatBucketTooltip } from './utils';

const RANGE_OPTIONS = [
    { value: '24h', labelKey: 'timelineRange24h', fallback: '24 小时' },
    { value: '7d', labelKey: 'timelineRange7d', fallback: '7 天' },
    { value: '30d', labelKey: 'timelineRange30d', fallback: '30 天' }
];

export function CallsTimeline({ data, loading, error, range, onRangeChange, onRetry }) {
    const t = useT();
    const points = Array.isArray(data?.points) ? data.points : [];
    const summary = data?.summary || {};
    const peak = Number(summary.peak) || 0;
    const max = useMemo(() => points.reduce((acc, point) => Math.max(acc, Number(point.total) || 0), 0), [points]);
    const totalCalls = Number(summary.totalCalls) || 0;
    const tickStep = Math.max(1, Math.ceil(points.length / 8));

    const options = RANGE_OPTIONS.map(item => ({
        value: item.value,
        label: t(`mcpMonitor.${item.labelKey}`, null, item.fallback)
    }));

    return (
        <SectionCard
            title={t('mcpMonitor.timelineTitle', null, '调用趋势')}
            description={t('mcpMonitor.timelineHint', null, '全部工具合计，失败与拦截单独着色')}
            extra={(
                <Select
                    size="small"
                    value={range}
                    optionList={options}
                    style={{ width: 120 }}
                    onChange={onRangeChange}
                />
            )}
        >
            <ErrorBanner error={error} onRetry={onRetry} />
            <div className="mcp-monitor-timeline-summary">
                <StatCard
                    label={t('mcpMonitor.timelineTotalLegend', null, '总调用')}
                    value={formatNumber(totalCalls)}
                    loading={loading}
                />
                <StatCard
                    label="峰值 / 桶"
                    value={formatNumber(peak)}
                    detail={t('mcpMonitor.scopeTimeline', null, '当前时间范围')}
                    loading={loading}
                />
                <div className="mcp-monitor-timeline-legend">
                    <span className="mcp-monitor-legend-item">
                        <i className="mcp-monitor-legend-dot mcp-monitor-legend-total" />
                        {t('mcpMonitor.timelineTotalLegend', null, '总调用')}
                    </span>
                    <span className="mcp-monitor-legend-item">
                        <i className="mcp-monitor-legend-dot mcp-monitor-legend-failed" />
                        {t('mcpMonitor.timelineFailedLegend', null, '失败')}
                    </span>
                    <span className="mcp-monitor-legend-item">
                        <i className="mcp-monitor-legend-dot mcp-monitor-legend-blocked" />
                        {t('mcpMonitor.timelineBlockedLegend', null, '安全拦截')}
                    </span>
                </div>
            </div>

            {loading && !points.length ? (
                <div className="mcp-monitor-timeline-empty">{t('mcpMonitor.timelineLoading', null, '趋势加载中…')}</div>
            ) : totalCalls === 0 || max === 0 ? (
                <div className="mcp-monitor-timeline-empty">
                    <p>{t('mcpMonitor.timelineNoData', null, '该时段暂无调用')}</p>
                    <p className="mcp-monitor-timeline-hint">{t('mcpMonitor.timelineEmptyHint', null, '调用 MCP 工具后此处会出现趋势')}</p>
                </div>
            ) : (
                <div className="mcp-monitor-timeline-chart" role="img" aria-label={t('mcpMonitor.timelineTitle', null, '调用趋势')}>
                    {points.map((point, index) => {
                        const total = Number(point.total) || 0;
                        const failed = Number(point.failed) || 0;
                        const blocked = Number(point.blocked) || 0;
                        const height = total > 0 ? Math.max(3, (total / max) * 100) : 0;
                        const failedShare = total > 0 ? (failed / total) * 100 : 0;
                        const blockedShare = total > 0 ? (blocked / total) * 100 : 0;
                        const label = formatBucketLabel(point.t, range);
                        return (
                            <div
                                className="mcp-monitor-timeline-slot"
                                key={point.t || index}
                                title={formatBucketTooltip(point, t)}
                            >
                                <div className="mcp-monitor-timeline-bar-wrap">
                                    <div className="mcp-monitor-timeline-bar" style={{ height: `${height}%` }}>
                                        {blockedShare > 0 && (
                                            <span className="mcp-monitor-timeline-seg mcp-monitor-seg-blocked" style={{ height: `${blockedShare}%` }} />
                                        )}
                                        {failedShare > 0 && (
                                            <span className="mcp-monitor-timeline-seg mcp-monitor-seg-failed" style={{ height: `${failedShare}%` }} />
                                        )}
                                    </div>
                                </div>
                                <span className="mcp-monitor-timeline-tick">
                                    {index % tickStep === 0 ? label : ''}
                                </span>
                            </div>
                        );
                    })}
                </div>
            )}
        </SectionCard>
    );
}
