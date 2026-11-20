/** 资产概览：统计卡、协议分布、扫描覆盖、最近资产。 */
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Banner, Button, Empty, Progress, RadioGroup, Space, Typography } from '@douyinfe/semi-ui';
import { IconArrowRight, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { formatNumber, relativeTime } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard, StatGrid
} from '../../components/ui';
import { ProtocolTag, RiskTag, TooltipText } from '../asset-library/components/AssetBits';
import { assetTargetLabel } from '../asset-library/components/assetTarget';
import './page.css';

const { Text } = Typography;

const PERIODS = [
    { value: 7, label: '7 天' },
    { value: 30, label: '30 天' },
    { value: 90, label: '90 天' }
];

function percentOf(count, total) {
    const value = Number(count || 0);
    const base = Number(total || 0);
    if (!base) return 0;
    return Math.round((value / base) * 1000) / 10;
}

export default function AssetOverviewPage() {
    const t = useT();
    const navigate = useNavigate();
    const { hasPermission, hasAnyPermission } = useAuth();
    const canRead = hasPermission('asset:read');
    const canCollect = hasAnyPermission(['asset:read', 'fofa:execute']);
    const [days, setDays] = useState(30);

    const statsRes = useResource(() => api.get('/assets/stats', { days }), [days], { skip: !canRead });
    const recentRes = useResource(
        () => api.get('/assets', { page: 1, page_size: 10, sort_by: 'last_seen_at', sort_order: 'desc' }),
        [],
        { skip: !canRead }
    );

    const stats = statsRes.data || {};
    const coverage = stats.coverage || {};
    const protocols = Array.isArray(stats.protocols) ? stats.protocols : [];
    const protocolTotal = useMemo(
        () => protocols.reduce((sum, item) => sum + Number(item.count || 0), 0),
        [protocols]
    );
    const recentAssets = Array.isArray(recentRes.data?.assets) ? recentRes.data.assets : [];

    const metrics = [
        {
            id: 'total',
            label: t('assets.totalAssets', null, '资产总数'),
            value: formatNumber(stats.total || 0),
            detail: t('assets.totalAssetsHint', null, '当前纳入持续监测的资产'),
            loading: statsRes.loading
        },
        {
            id: 'ips',
            label: t('assets.ipCount', null, 'IP 数量'),
            value: formatNumber(stats.ips || 0),
            loading: statsRes.loading
        },
        {
            id: 'domains',
            label: t('assets.domainCount', null, '域名数量'),
            value: formatNumber(stats.domains || 0),
            loading: statsRes.loading
        },
        {
            id: 'ports',
            label: t('assets.portCount', null, '端口数量'),
            value: formatNumber(stats.ports || 0),
            loading: statsRes.loading
        },
        {
            id: 'protocols',
            label: t('assets.serviceTypes', null, '服务类型'),
            value: formatNumber(protocols.length),
            detail: protocols[0] ? `${t('assets.topProtocol', null, '主要协议')}：${protocols[0].name}` : undefined,
            loading: statsRes.loading
        },
        {
            id: 'recent',
            label: t('assets.recentCount', null, '近 7 天发现'),
            value: formatNumber(stats.recent || 0),
            detail: t('assets.recentShare', { percent: percentOf(stats.recent, stats.total) }, `占当前资产总量的 ${percentOf(stats.recent, stats.total)}%`),
            loading: statsRes.loading
        }
    ];

    const goLibrary = asset => {
        const suffix = asset?.id ? `?id=${encodeURIComponent(asset.id)}` : '';
        navigate(`/asset-library${suffix}`, asset ? { state: { asset } } : undefined);
    };

    const columns = [
        {
            title: t('assets.target', null, '目标'),
            dataIndex: 'host',
            render: (_, record) => (
                <div className="asset-overview-target">
                    <TooltipText text={assetTargetLabel(record)} max={34} />
                    <Text type="tertiary" size="small">{[record.ip, record.domain].filter(Boolean).join(' · ') || '—'}</Text>
                </div>
            )
        },
        {
            title: t('assets.service', null, '服务'),
            dataIndex: 'protocol',
            width: 150,
            render: (_, record) => <ProtocolTag protocol={record.protocol} port={record.port} />
        },
        {
            title: t('assets.source', null, '来源'),
            dataIndex: 'source',
            width: 120,
            render: value => <TooltipText text={value} max={14} />
        },
        {
            title: t('assets.riskLevel', null, '风险等级'),
            dataIndex: 'risk_level',
            width: 120,
            render: (_, record) => <RiskTag asset={record} />
        },
        {
            title: t('assets.lastSeen', null, '最近发现'),
            dataIndex: 'last_seen_at',
            width: 130,
            render: value => <Text size="small">{relativeTime(value, t)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 100,
            render: (_, record) => (
                <Button theme="borderless" size="small" onClick={() => goLibrary(record)}>
                    {t('common.view', null, '查看')}
                </Button>
            )
        }
    ];

    const coverageItems = [
        { label: t('assets.scannedAssets', null, '已扫描资产'), value: formatNumber(coverage.scanned || 0) },
        { label: t('assets.recentlyScanned', null, '近 30 天覆盖'), value: formatNumber(coverage.scanned_30d || 0) },
        { label: t('assets.neverScanned', null, '从未扫描'), value: formatNumber(coverage.never_scanned || 0) },
        { label: t('assets.staleScans', null, '超过 30 天未扫描'), value: formatNumber(coverage.stale || 0) }
    ];

    return (
        <div className="asset-overview-page">
            <PageHeader
                title={t('assets.overviewTitle', null, '资产概览')}
                description="汇总资产规模、服务类型、扫描覆盖与最近发现情况。"
                extra={(
                    <Space wrap>
                        <RadioGroup
                            type="button"
                            value={days}
                            options={PERIODS}
                            onChange={event => setDays(Number(event.target.value) || 30)}
                        />
                        <Button icon={<IconRefresh />} onClick={() => { statsRes.reload(); recentRes.reload(); }}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button icon={<IconArrowRight />} onClick={() => navigate('/asset-library')}>
                            {t('assets.viewLibrary', null, '查看资产库')}
                        </Button>
                        {canCollect && (
                            <Button theme="solid" type="primary" icon={<IconSearch />} onClick={() => navigate('/info-collect')}>
                                {t('nav.infoCollect', null, '信息收集')}
                            </Button>
                        )}
                    </Space>
                )}
            />
            {!canRead && (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    description={t('assets.readDenied', null, '当前账号没有 asset:read 权限，无法读取资产统计；请联系管理员授权。')}
                />
            )}
            {canRead && <ErrorBanner error={statsRes.error} onRetry={() => statsRes.reload()} />}
            {canRead && <StatGrid metrics={metrics} columns={6} />}

            {canRead && (<div className="asset-overview-columns">
                <SectionCard
                    className="asset-overview-card"
                    title={t('assets.protocolDistribution', null, '协议分布')}
                    description={t('assets.protocolDistributionHint', null, '按已识别服务查看资产暴露构成')}
                    extra={<Text type="tertiary" size="small">{t('assets.protocolKinds', { count: protocols.length }, `${protocols.length} 种协议`)}</Text>}
                >
                    {protocols.length === 0 ? (
                        <Empty title={t('common.noData', null, '暂无数据')} description={null} style={{ padding: 24 }} />
                    ) : (
                        <div className="asset-overview-bars">
                            {protocols.map(item => {
                                const percent = percentOf(item.count, stats.total || protocolTotal);
                                return (
                                    <div className="asset-overview-bar-row" key={item.name}>
                                        <Text size="small" className="asset-overview-bar-label">
                                            {item.name || 'unknown'}
                                        </Text>
                                        <Progress percent={percent} showInfo={false} stroke="var(--semi-color-primary)" />
                                        <Text type="tertiary" size="small" className="asset-overview-bar-value">
                                            {formatNumber(item.count || 0)} · {percent}%
                                        </Text>
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </SectionCard>

                <SectionCard
                    className="asset-overview-card"
                    title={t('assets.scanCoverage', null, '扫描覆盖')}
                    description={t('assets.scanCoverageHint', null, '识别未扫描与长期未复查的资产缺口')}
                    extra={<Text type="tertiary" size="small">{t('assets.coverageMeta', { scanned: coverage.scanned || 0, total: stats.total || 0 }, `${coverage.scanned || 0} / ${stats.total || 0} 已覆盖`)}</Text>}
                >
                    <div className="asset-overview-coverage">
                        <div className="asset-overview-coverage-main">
                            <Text type="tertiary" size="small">{t('assets.overallCoverage', null, '总体覆盖率')}</Text>
                            <Progress
                                percent={Number(coverage.rate || 0)}
                                stroke={Number(coverage.rate || 0) >= 80 ? 'var(--semi-color-success)' : undefined}
                                format={percent => `${percent}%`}
                            />
                        </div>
                        <div className="asset-overview-coverage-grid">
                            {coverageItems.map(item => (
                                <div key={item.label} className="asset-overview-coverage-item">
                                    <Text type="tertiary" size="small">{item.label}</Text>
                                    <Text strong>{item.value}</Text>
                                </div>
                            ))}
                        </div>
                    </div>
                </SectionCard>
            </div>)}

            {canRead && (<SectionCard
                title={t('assets.recentAssets', null, '最近资产')}
                description="按最近发现时间排列的前 10 条资产。"
                extra={(
                    <Button theme="borderless" onClick={() => navigate('/asset-library')}>
                        {t('assets.viewLibrary', null, '查看资产库')}
                    </Button>
                )}
            >
                <ErrorBanner error={recentRes.error} onRetry={() => recentRes.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={recentAssets}
                    loading={recentRes.loading}
                    rowKey="id"
                    pagination={false}
                    onRefresh={() => recentRes.reload()}
                    emptyText={t('common.noData', null, '暂无数据')}
                />
            </SectionCard>)}
        </div>
    );
}
