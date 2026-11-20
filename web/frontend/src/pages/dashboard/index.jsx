import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Banner, Button, Empty, List, Progress, Space, Tag, Typography } from '@douyinfe/semi-ui';
import {
    IconArrowRight, IconBolt, IconCheckCircleStroked, IconCheckList,
    IconComment, IconRefresh, IconShield, IconTerminal
} from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import { PageHeader, SectionCard, StatGrid, ErrorBanner, SeverityTag } from '../../components/ui';
import { formatDateTime, formatNumber, formatPercent } from '../../lib/format';
import VulnerabilityOverview, { asCount, displayCount, severityCount } from './vulnerability-overview';
import './page.css';

const { Text } = Typography;
const compactNumber = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 });

async function safe(path, query) {
    try {
        return await api.get(path, query);
    } catch (error) {
        if (error.status === 503 && error.payload?.error === 'c2_disabled') return { enabled: false };
        return null;
    }
}

async function loadDashboard() {
    const [
        agentTasks, vulnStats, pendingVulnStats, vulnList, batchQueues, monitorStats, tokenUsage,
        knowledgeStats, skillsStats, roles, agents, hitlPending, externalMcp,
        webshell, c2Listeners, c2Sessions, projectSummary, toolsConfig
    ] = await Promise.all([
        safe('/agent-loop/tasks'),
        safe('/vulnerabilities/stats'),
        safe('/vulnerabilities/stats', { status: 'open' }),
        safe('/vulnerabilities', { limit: 8, page: 1 }),
        safe('/batch-tasks', { limit: 200, page: 1 }),
        safe('/monitor/stats', { top: 8 }),
        safe('/usage/tokens', { days: 7, limit: 20 }),
        safe('/knowledge/stats'),
        safe('/skills/stats'),
        safe('/roles'),
        safe('/multi-agent/markdown-agents'),
        safe('/hitl/pending'),
        safe('/external-mcp/stats'),
        safe('/webshell/connections'),
        safe('/c2/listeners'),
        safe('/c2/sessions', { limit: 200 }),
        safe('/projects/dashboard-summary', { fact_limit: 8 }),
        safe('/config/tools', { page: 1, page_size: 1, include_external: false })
    ]);
    const sources = {
        agentTasks, vulnStats, pendingVulnStats, vulnList, batchQueues, monitorStats, tokenUsage,
        knowledgeStats, skillsStats, roles, agents, hitlPending, externalMcp,
        webshell, c2Listeners, c2Sessions, projectSummary, toolsConfig
    };
    return {
        ...sources,
        unavailableCount: Object.values(sources).filter(value => value == null).length,
        sourceCount: Object.keys(sources).length,
        loadedAt: new Date().toISOString()
    };
}

function getList(value, ...keys) {
    if (Array.isArray(value)) return value;
    for (const key of keys) {
        if (Array.isArray(value?.[key])) return value[key];
    }
    return null;
}

function countList(value, ...keys) {
    return getList(value, ...keys)?.length ?? null;
}

function DashboardEmpty({ loading, unavailable, title, description }) {
    return (
        <Empty
            className="dashboard-empty"
            title={loading ? '正在加载数据' : unavailable ? '数据暂不可用' : title}
            description={loading ? '正在读取最新记录，请稍候。' : unavailable ? '未能取得数据，请稍后刷新重试。' : description}
        />
    );
}

function RecentVulnerabilities({ data, loading, onOpen }) {
    const list = getList(data, 'vulnerabilities');
    return (
        <List
            className="dashboard-list dashboard-feed"
            loading={loading}
            dataSource={list || []}
            emptyContent={<DashboardEmpty loading={loading} unavailable={list === null} title="暂无漏洞记录" description="测评发现的漏洞会汇集到这里，便于跟进与复核。" />}
            renderItem={item => (
                <List.Item
                    key={item.id}
                    main={(
                        <div className="dashboard-record">
                            <Button theme="borderless" className="dashboard-record-link" onClick={() => onOpen(item.id)}>
                                {item.title || item.name || '未命名漏洞'}
                            </Button>
                            <Text type="tertiary" size="small" className="dashboard-meta">
                                {item.host || item.target || '—'} · {formatDateTime(item.created_at)}
                            </Text>
                        </div>
                    )}
                    extra={<SeverityTag severity={item.severity} />}
                />
            )}
        />
    );
}

function RecentFacts({ summary, loading, onOpen }) {
    const facts = getList(summary, 'recent_facts', 'facts');
    return (
        <List
            className="dashboard-list dashboard-feed dashboard-facts"
            split={false}
            loading={loading}
            dataSource={facts || []}
            emptyContent={<DashboardEmpty loading={loading} unavailable={facts === null} title="暂无近期事实" description="项目沉淀的事实与证据将在这里展示。" />}
            renderItem={(fact, index) => (
                <List.Item
                    key={fact.id || fact.key || fact.fact_key || index}
                    main={(
                        <div className="dashboard-record">
                            <Text strong className="dashboard-fact-title">{fact.title || fact.summary || fact.key || fact.fact_key || fact.subject || '事实'}</Text>
                            <Text type="tertiary" size="small" className="dashboard-meta">
                                {fact.project_name || fact.projectName || fact.value || '—'}
                            </Text>
                        </div>
                    )}
                    extra={(
                        <Space spacing={8}>
                            {fact.status && <Tag size="small">{fact.status}</Tag>}
                            {fact.project_id && (
                                <Button theme="borderless" icon={<IconArrowRight />} aria-label="查看所属项目" onClick={() => onOpen(fact.project_id)} />
                            )}
                        </Space>
                    )}
                />
            )}
        />
    );
}

export default function DashboardPage() {
    const t = useT();
    const navigate = useNavigate();
    const { data, loading, error, reload } = useResource(loadDashboard, []);
    useInterval(() => reload({ silent: true }), 60000);

    const metrics = useMemo(() => {
        const runningAgents = countList(data?.agentTasks, 'tasks');
        const tokenCount = asCount(data?.tokenUsage?.summary?.totalTokens);
        const critical = severityCount(data?.vulnStats, 'critical');
        const pendingHigh = severityCount(data?.pendingVulnStats, 'high');
        const summary = data?.monitorStats?.summary;
        const successCalls = asCount(summary?.successCalls);
        const failedCalls = asCount(summary?.failedCalls);
        const effective = successCalls === null || failedCalls === null ? null : successCalls + failedCalls;
        const rate = effective > 0 ? successCalls / effective : null;
        const queues = getList(data?.batchQueues, 'queues');
        const batchRunning = queues?.filter(queue => String(queue.status).toLowerCase() === 'running').length;
        return [
            {
                id: 'tasks',
                label: t('dashboard.runningConversations', null, '运行中任务'),
                value: displayCount(runningAgents),
                detail: `运行中批量队列 ${displayCount(batchRunning)}`,
                icon: <IconCheckList />,
                onClick: () => navigate('/tasks')
            },
            {
                id: 'vulnerabilities',
                label: t('dashboard.vulnTotal', null, '漏洞总数'),
                value: displayCount(data?.vulnStats?.total),
                detail: t('dashboard.openHighCountLabel', { count: displayCount(pendingHigh) }, `待处理高危 ${displayCount(pendingHigh)}`),
                tone: critical > 0 ? 'danger' : undefined,
                icon: <IconShield />,
                onClick: () => navigate('/vulnerabilities')
            },
            {
                id: 'calls',
                label: t('dashboard.toolCalls', null, '工具调用'),
                value: displayCount(summary?.totalCalls),
                detail: `涉及工具 ${displayCount(summary?.toolCount)}`,
                icon: <IconTerminal />,
                onClick: () => navigate('/mcp-monitor')
            },
            {
                id: 'rate',
                label: t('dashboard.successRate', null, '执行成功率'),
                value: rate === null ? '—' : formatPercent(rate),
                detail: effective === null ? '执行结果暂不可用' : effective === 0 ? '尚无已完成调用' : `失败 ${displayCount(failedCalls)} 次`,
                tone: rate !== null && rate < 0.8 ? 'warning' : undefined,
                icon: <IconCheckCircleStroked />,
                onClick: () => navigate('/mcp-monitor')
            },
            {
                id: 'tokens',
                label: t('dashboard.tokenUsage', null, 'Token 用量（7 天）'),
                value: tokenCount >= 1_000_000 ? compactNumber.format(tokenCount) : displayCount(tokenCount),
                valueTitle: tokenCount >= 1_000_000 ? displayCount(tokenCount) : undefined,
                detail: `调用次数 ${displayCount(data?.tokenUsage?.summary?.modelCalls ?? data?.tokenUsage?.summary?.calls)}`,
                icon: <IconBolt />,
                onClick: () => navigate('/settings?section=token-usage')
            }
        ].map(metric => ({ ...metric, loading, detail: loading ? '正在刷新数据' : metric.detail }));
    }, [data, loading, navigate, t]);

    const alerts = useMemo(() => {
        const list = [];
        const critical = severityCount(data?.vulnStats, 'critical');
        if (critical > 0) list.push({ id: 'critical', text: `${displayCount(critical)} 条严重漏洞`, page: '/vulnerabilities' });
        const pending = countList(data?.hitlPending, 'items', 'pending');
        if (pending > 0) list.push({ id: 'pending', text: `${displayCount(pending)} 项审批待处理`, page: '/hitl' });
        const failed = asCount(data?.monitorStats?.summary?.failedCalls);
        if (failed > 0) list.push({ id: 'failed', text: `工具调用失败 ${displayCount(failed)} 次`, page: '/mcp-monitor' });
        const mcpTotal = asCount(data?.externalMcp?.total);
        const mcpConnected = asCount(data?.externalMcp?.connected);
        if (mcpTotal !== null && mcpConnected !== null && mcpTotal > mcpConnected) {
            list.push({ id: 'mcp', text: `${displayCount(mcpTotal - mcpConnected)} 个外部 MCP 未连接`, page: '/mcp-management' });
        }
        return list;
    }, [data]);

    const capability = useMemo(() => [
        { id: 'tools', label: 'MCP 工具', value: data?.toolsConfig?.total, page: '/mcp-management' },
        { id: 'knowledge', label: '知识条目', value: data?.knowledgeStats?.total_items, page: '/knowledge-management' },
        { id: 'skills', label: '技能', value: data?.skillsStats?.total_skills, page: '/skills-management' },
        { id: 'roles', label: '角色', value: countList(data?.roles, 'roles'), page: '/roles-management' },
        { id: 'agents', label: '智能体', value: countList(data?.agents, 'agents'), page: '/agents-management' },
        { id: 'webshell', label: 'WebShell 连接', value: countList(data?.webshell, 'connections'), page: '/webshell' },
        { id: 'listeners', label: 'C2 监听器', value: countList(data?.c2Listeners, 'listeners'), page: '/c2-listeners' },
        { id: 'sessions', label: 'C2 会话', value: countList(data?.c2Sessions, 'sessions', 'items'), page: '/c2-sessions' }
    ].filter(item => (item.id !== 'listeners' || data?.c2Listeners?.enabled !== false)
        && (item.id !== 'sessions' || data?.c2Sessions?.enabled !== false)), [data]);

    const lastUpdated = data?.loadedAt ? formatDateTime(data.loadedAt) : '—';
    const openProject = id => navigate(`/projects?id=${encodeURIComponent(id)}`);

    return (
        <div className="dashboard-page" aria-busy={loading}>
            <PageHeader
                title={t('dashboard.title', null, '仪表盘')}
                description={`最近刷新 ${lastUpdated} · 每 60 秒自动刷新`}
                extra={<>
                    <Button icon={<IconRefresh />} onClick={() => reload()} loading={loading}>{t('common.refresh', null, '刷新')}</Button>
                    <Button theme="solid" icon={<IconComment />} onClick={() => navigate('/chat')}>进入对话</Button>
                </>}
            />
            <StatGrid metrics={metrics} columns={5} />

            <ErrorBanner error={error} onRetry={() => reload()} />
            {data?.unavailableCount > 0 && (
                <Banner
                    className="dashboard-notice"
                    type="info"
                    fullMode={false}
                    closeIcon={null}
                    title={data.unavailableCount === data.sourceCount ? '数据暂不可用' : '部分数据不可用'}
                    description={data.unavailableCount === data.sourceCount
                        ? '本次未能取得仪表盘数据，请检查服务连接后刷新重试。'
                        : `${data.unavailableCount} 项数据源未能加载，相关数值以 — 显示。已返回的数据仍可查看，请稍后刷新重试。`}
                />
            )}
            {alerts.length > 0 && (
                <Banner
                    className="dashboard-notice"
                    type="warning"
                    fullMode={false}
                    closeIcon={null}
                    description={(
                        <Space wrap spacing={8}>
                            <Text strong>需要关注</Text>
                            {alerts.map(item => (
                                <Button key={item.id} theme="borderless" type="warning" icon={<IconArrowRight />} iconPosition="right" onClick={() => navigate(item.page)}>
                                    {item.text}
                                </Button>
                            ))}
                        </Space>
                    )}
                />
            )}

            <div className="dashboard-grid">
                <div className="dashboard-column">
                    <VulnerabilityOverview
                        stats={data?.vulnStats}
                        pendingStats={data?.pendingVulnStats}
                        latest={data?.vulnList?.vulnerabilities?.[0]?.created_at}
                        loadedAt={data?.loadedAt}
                        loading={loading}
                    />
                    <SectionCard
                        title={t('dashboard.recentVulns', null, '最近漏洞')}
                        extra={<Button theme="borderless" size="small" icon={<IconArrowRight />} iconPosition="right" onClick={() => navigate('/vulnerabilities')}>查看全部</Button>}
                    >
                        <RecentVulnerabilities data={data?.vulnList} loading={loading} onOpen={id => navigate(`/vulnerabilities?id=${encodeURIComponent(id)}`)} />
                    </SectionCard>
                </div>
                <div className="dashboard-column">
                    <SectionCard
                        title={t('dashboard.projects', null, '项目概览')}
                        extra={<Button theme="borderless" size="small" icon={<IconArrowRight />} iconPosition="right" onClick={() => navigate('/projects')}>全部项目</Button>}
                    >
                        <ProjectSummary summary={data?.projectSummary} loading={loading} onOpen={openProject} />
                    </SectionCard>
                    <SectionCard
                        title={t('dashboard.toolsTop', null, '工具调用 Top')}
                        extra={<Button theme="borderless" size="small" onClick={() => navigate('/mcp-monitor')}>调用监控</Button>}
                    >
                        <TopTools data={data?.monitorStats} loading={loading} />
                    </SectionCard>
                    <SectionCard
                        className="dashboard-capability-card"
                        bodyClassName="dashboard-capability-body"
                        title={t('dashboard.capability', null, '能力与运行态')}
                    >
                        <List
                            className="dashboard-capabilities"
                            split={false}
                            dataSource={capability}
                            renderItem={item => (
                                <List.Item key={item.id}>
                                    <Button block theme="borderless" type="tertiary" className="dashboard-capability" onClick={() => navigate(item.page)}>
                                        <span>{item.label}</span>
                                        <span className="dashboard-capability-value">{loading ? '…' : displayCount(item.value)}</span>
                                    </Button>
                                </List.Item>
                            )}
                        />
                    </SectionCard>
                </div>
            </div>
            <SectionCard className="dashboard-facts-card" title={t('dashboard.recentFacts', null, '近期事实')}>
                <RecentFacts summary={data?.projectSummary} loading={loading} onOpen={openProject} />
            </SectionCard>
        </div>
    );
}

function TopTools({ data, loading }) {
    const tools = getList(data, 'topTools');
    const items = tools?.slice(0, 8).map(tool => ({
        name: tool.name || tool.toolName || '未命名工具',
        count: asCount(tool.count ?? tool.calls ?? tool.totalCalls)
    })) || [];
    const max = Math.max(...items.map(tool => tool.count ?? 0), 1);
    return (
        <List
            className="dashboard-list"
            loading={loading}
            dataSource={items}
            emptyContent={<DashboardEmpty loading={loading} unavailable={tools === null} title="暂无工具调用" description="执行任务后，可在这里查看使用最多的工具。" />}
            renderItem={(tool, index) => (
                <List.Item key={`${tool.name}-${index}`} className="dashboard-tool-row">
                    <Text type="tertiary" size="small" className="dashboard-tool-rank">{index + 1}</Text>
                    <div className="dashboard-record">
                        <div className="dashboard-tool-heading">
                            <Text className="dashboard-tool-name" title={tool.name}>{tool.name}</Text>
                            <Text type="secondary" size="small" className="dashboard-count">{displayCount(tool.count)} 次</Text>
                        </div>
                        {tool.count !== null && (
                            <Progress percent={Math.round((tool.count / max) * 100)} showInfo={false} size="small" stroke="var(--semi-color-primary)" aria-label={`${tool.name}调用次数`} aria-valuetext={`${displayCount(tool.count)} 次`} />
                        )}
                    </div>
                </List.Item>
            )}
        />
    );
}

function ProjectSummary({ summary, loading, onOpen }) {
    const projects = getList(summary, 'projects');
    const facts = getList(summary, 'recent_facts', 'facts');
    const totalsSource = summary?.totals || summary;
    const totals = [
        ['活跃项目', totalsSource?.active_projects ?? totalsSource?.activeProjects],
        ['事实总数', totalsSource?.total_facts ?? totalsSource?.totalFacts],
        ['最近事实', facts?.length]
    ];
    return (
        <div>
            <dl className="dashboard-project-totals">
                {totals.map(([label, value]) => (
                    <div key={label}>
                        <dt>{label}</dt>
                        <dd>{loading ? '…' : displayCount(value)}</dd>
                    </div>
                ))}
            </dl>
            <List
                className="dashboard-list dashboard-feed"
                loading={loading}
                dataSource={projects?.slice(0, 6) || []}
                emptyContent={(
                    <DashboardEmpty
                        loading={loading}
                        unavailable={!summary}
                        title={projects === null ? '在项目中继续工作' : '暂无项目'}
                        description={projects === null ? '当前摘要提供项目与事实统计，完整列表请查看全部项目。' : '创建项目后，在这里跟进项目进展。'}
                    />
                )}
                renderItem={project => (
                    <List.Item
                        key={project.id}
                        main={(
                            <div className="dashboard-record">
                                <Button theme="borderless" className="dashboard-record-link" onClick={() => onOpen(project.id)}>{project.name || '未命名项目'}</Button>
                                <Text type="tertiary" size="small" className="dashboard-meta">
                                    {project.status || '—'} · {formatDateTime(project.updated_at)}
                                </Text>
                            </div>
                        )}
                    />
                )}
            />
        </div>
    );
}
