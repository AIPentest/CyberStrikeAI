import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { Button, Checkbox, Empty, Input, Select, Space, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { IconMinus, IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { ErrorBanner, SectionCard, confirmAction, toastError, toastSuccess } from '../../components/ui';
import { CONFIDENCE_META, NODE_COLORS, NODE_TYPE_LABEL, errorProp, errText } from './helpers';
import { GraphNodeIcon } from '../../components/graph-card';
import FactGraphCanvas from './fact-graph-canvas';

const { Text } = Typography;
const EMPTY = [];
const nodeTitle = node => node.summary || node.label || node.fact_key || node.id;

function GraphSearch({ nodes, onSelect, t }) {
    const [query, setQuery] = useState('');
    const [open, setOpen] = useState(false);
    const resultsId = useId();
    const search = query.trim().toLowerCase();
    const ordered = useMemo(() => [...nodes].sort((a, b) =>
        (a.fact_key || a.id).localeCompare(b.fact_key || b.id)), [nodes]);
    const matches = useMemo(() => {
        if (!search) return ordered;
        const results = ordered.filter(node => [node.fact_key, node.label, node.summary, node.category]
            .join(' ').toLowerCase().includes(search));
        const exact = results.findIndex(node => (node.fact_key || node.id).toLowerCase() === search);
        if (exact > 0) results.unshift(...results.splice(exact, 1));
        return results;
    }, [ordered, search]);

    const select = id => {
        onSelect(id);
        setOpen(false);
        setQuery('');
    };

    return (
        <div className="project-graph-search" onBlur={event => {
            if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
        }}>
            <Input
                prefix={<IconSearch />}
                value={query}
                showClear
                placeholder={t('projects.graphSearchPlaceholder', null, '搜索节点…')}
                aria-label={t('projects.graphSearchSr', null, '搜索节点')}
                aria-controls={open ? resultsId : undefined}
                onChange={value => { setQuery(value); setOpen(true); }}
                onFocus={() => setOpen(true)}
                onClick={() => setOpen(true)}
                onKeyDown={event => {
                    if (event.key === 'Escape' && open) setOpen(false);
                    if (event.key === 'Enter' && open && matches.length && !event.nativeEvent.isComposing) {
                        event.preventDefault();
                        select(matches[0].id);
                    }
                }}
            />
            {open && (
                <div className="project-graph-search-results" id={resultsId}>
                    <div className="project-graph-search-count" role="status">
                        {matches.length
                            ? t('projects.graphSearchCount', { count: matches.length }, `${matches.length} 个节点`)
                            : t('projects.graphNoMatches', null, '未找到匹配节点')}
                    </div>
                    {matches.length > 0 && <ul className="project-graph-search-list">
                        {matches.map(node => (
                            <li key={node.id}>
                                <button className="project-graph-search-hit" type="button" onClick={() => select(node.id)}>
                                    <span title={nodeTitle(node)}>{nodeTitle(node)}</span>
                                    <code dir="auto">{node.fact_key || node.id}</code>
                                </button>
                            </li>
                        ))}
                    </ul>}
                </div>
            )}
        </div>
    );
}

export default function FactGraphPanel({
    projectId, canWrite, revision = 0, onOpenFact, onEditFact, onAddFact, onChanged
}) {
    const t = useT();
    const [view, setView] = useState('path');
    const [hideDeprecated, setHideDeprecated] = useState(true);
    const [selectedId, setSelectedId] = useState('');
    const [showEdges, setShowEdges] = useState(false);
    const [fitView, setFitView] = useState('fitted');
    const chartRef = useRef(null);
    const sideRef = useRef(null);
    const scope = `${view}:${hideDeprecated}`;
    const graph = useResource(async () => ({
        scope,
        data: await api.get(`/projects/${projectId}/fact-graph`, {
            view, exclude_deprecated: hideDeprecated ? '1' : '0'
        })
    }), [projectId, scope, revision]);

    const currentGraph = graph.data?.scope === scope ? graph.data.data : null;
    const nodes = currentGraph?.nodes || EMPTY;
    const byId = useMemo(() => new Map(nodes.map(node => [node.id, node])), [nodes]);
    const edges = useMemo(() => (currentGraph?.edges || EMPTY)
        .filter(edge => byId.has(edge.source) && byId.has(edge.target)), [currentGraph, byId]);

    const selected = byId.get(selectedId);
    useEffect(() => {
        if (sideRef.current) sideRef.current.scrollTop = 0;
    }, [selected?.id]);
    const shownEdges = useMemo(() => selected
        ? edges.filter(edge => edge.source === selected.id || edge.target === selected.id)
        : edges, [edges, selected]);
    const shownNodes = useMemo(() => {
        if (!selected) return nodes;
        const neighborhood = new Set([selected.id]);
        for (const edge of shownEdges) {
            neighborhood.add(edge.source);
            neighborhood.add(edge.target);
        }
        return nodes.filter(node => neighborhood.has(node.id));
    }, [nodes, selected, shownEdges]);
    const types = Object.keys(NODE_COLORS).filter(type => shownNodes.some(node => node.type === type));
    const typeLabel = type => t(`projects.graphNodeTypes.${type}`, null, NODE_TYPE_LABEL[type] || type);

    const openNode = useCallback(id => setSelectedId(id), []);
    const edgeList = useResource(
        () => (showEdges ? api.get(`/projects/${projectId}/fact-edges`) : Promise.resolve(null)),
        [projectId, showEdges, revision]
    );

    const refresh = () => {
        if (onChanged) onChanged();
        else {
            graph.reload();
            if (showEdges) edgeList.reload();
        }
    };

    const deleteEdge = edge => {
        confirmAction({
            title: t('projects.graphDeleteEdge', null, '删边'),
            content: t('projects.confirmDeleteGraphEdge', null, '确定删除此关系边？'),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/projects/${projectId}/fact-edges/${edge.id}`);
                    toastSuccess(t('projects.graphEdgeDeleteSuccess', null, '边已删除'));
                    refresh();
                } catch (error) {
                    toastError(errText(error, t('projects.graphEdgeDeleteFailed', null, '删除边失败')));
                }
            }
        });
    };
    const stats = (nodeCount, edgeCount) => t(
        'projects.graphStats', { nodes: nodeCount, edges: edgeCount }, `${nodeCount} 个节点 · ${edgeCount} 条关系`
    );

    const graphLabel = t('projects.tabGraph', null, '攻击路径');
    const viewHint = {
        reading: t('projects.graphReadingHint', null, '已保留可读字号，拖动画布浏览其他节点。'),
        overview: t('projects.graphFullOverviewHint', null, '悬停预览节点摘要，点选查看详情。')
    }[fitView];
    const factSelected = selected && selected.type !== 'missing' && !selected.id.startsWith('vuln:');
    return (
        <SectionCard
            title={graphLabel}
            className="project-graph-card"
            extra={(
                <Space>
                    <Text type="tertiary" size="small">{stats(nodes.length, edges.length)}</Text>
                    <Button
                        icon={<IconRefresh />}
                        aria-label={t('common.refresh', null, '刷新')}
                        theme="borderless"
                        loading={graph.loading}
                        onClick={refresh}
                    />
                </Space>
            )}
        >
            <ErrorBanner error={errorProp(graph.error)} onRetry={graph.reload} />
            <div className="project-toolbar project-graph-toolbar">
                <Select
                    value={view}
                    aria-label={t('projects.graphScope', null, '关系范围')}
                    onChange={next => { setSelectedId(''); setView(next); }}
                    style={{ width: 134 }}
                    optionList={[
                        { value: 'path', label: t('projects.graphViewPath', null, '攻击路径') },
                        { value: 'full', label: t('projects.graphViewFull', null, '完整关系') }
                    ]}
                />
                <GraphSearch nodes={nodes} onSelect={openNode} t={t} />
                <Checkbox checked={hideDeprecated} onChange={event => {
                    setSelectedId('');
                    setHideDeprecated(event.target.checked);
                }}>
                    {t('projects.hideDeprecated', null, '隐藏废弃')}
                </Checkbox>
                <Button className="project-graph-edges-toggle" aria-expanded={showEdges} onClick={() => setShowEdges(value => !value)}>
                    {t('projects.graphEdgesTitle', null, '关系边')}
                </Button>
            </div>
            {nodes.length > 0 ? (
                <>
                    <div className="project-graph-status">
                        {selected ? (
                            <>
                                <Button size="small" theme="light" type="primary" onClick={() => setSelectedId('')}>
                                    {t('projects.graphBackToOverview', null, '返回全图')}
                                </Button>
                                <Text size="small" aria-live="polite">
                                    {t('projects.graphFocus', { stats: stats(shownNodes.length, shownEdges.length) }, `聚焦直接关系 · ${stats(shownNodes.length, shownEdges.length)}`)}
                                </Text>
                            </>
                        ) : null}
                        {(!selected || viewHint) && (
                            <Text type="tertiary" size="small">
                                {viewHint || t('projects.graphOverviewHint', null, '点选节点查看直接关系和摘要。')}
                            </Text>
                        )}
                    </div>
                    <div className={`project-graph-layout${selected && shownNodes.length <= 6 ? ' is-compact' : ''}`}>
                        <div className="project-graph-viewport">
                            <FactGraphCanvas
                                nodes={shownNodes}
                                edges={shownEdges}
                                selectedId={selected?.id}
                                onSelect={openNode}
                                chartRef={chartRef}
                                typeLabel={typeLabel}
                                onFitChange={setFitView}
                                label={t('projects.graphCanvas', null, '攻击路径画布')}
                                errorText={t('projects.graphRenderError', null, '图谱加载失败，请刷新重试。')}
                            />
                            <div className="project-graph-footer">
                                <div className="project-graph-legend">
                                    {types.map(type => (
                                        <span key={type} style={{ '--graph-node-color': NODE_COLORS[type] }}>
                                            <GraphNodeIcon type={type} />
                                            {typeLabel(type)}
                                        </span>
                                    ))}
                                    {shownEdges.some(edge => edge.confidence === 'tentative') && (
                                        <span><i className="project-graph-legend-dash" aria-hidden="true" />{t('projects.graphTentativeEdge', null, '待确认关系')}</span>
                                    )}
                                </div>
                                <Space spacing={4} wrap>
                                    <Button size="small" icon={<IconMinus />} aria-label={t('projects.graphZoomOut', null, '缩小')} onClick={() => chartRef.current?.zoom(1 / 1.3)} />
                                    <Button size="small" icon={<IconPlus />} aria-label={t('projects.graphZoomIn', null, '放大')} onClick={() => chartRef.current?.zoom(1.3)} />
                                    <Button
                                        size="small"
                                        aria-pressed={fitView === 'fitted' || fitView === 'reading'}
                                        onClick={() => chartRef.current?.fit()}
                                    >
                                        {t('projects.graphFit', null, '适合画布')}
                                    </Button>
                                    <Button
                                        size="small"
                                        aria-pressed={fitView === 'overview'}
                                        onClick={() => chartRef.current?.overview()}
                                    >
                                        {t('projects.graphFullOverview', null, '全图总览')}
                                    </Button>
                                </Space>
                            </div>
                        </div>
                        {selected && (
                            <aside ref={sideRef} className="project-graph-side" aria-label={t('projects.graphNodeDetails', null, '节点详情')}>
                                <div className="project-detail-stack">
                                    <div>
                                        <Text strong>{nodeTitle(selected)}</Text>
                                        <code className="project-graph-node-key" dir="auto">{selected.fact_key || selected.id}</code>
                                        <div className="project-graph-side-tags">
                                            <Tag size="small" color="blue" type="light">{typeLabel(selected.type)}</Tag>
                                            <Tag size="small" color={CONFIDENCE_META[selected.confidence]?.color || 'grey'}>
                                                {CONFIDENCE_META[selected.confidence]
                                                    ? t(`projects.graphConfidence.${selected.confidence}`, null, CONFIDENCE_META[selected.confidence].label)
                                                    : selected.confidence || '—'}
                                            </Tag>
                                        </div>
                                    </div>
                                    {factSelected && (
                                        <Space>
                                            <Button size="small" onClick={() => onOpenFact(selected.fact_key)}>{t('projects.details', null, '详情')}</Button>
                                            <Button size="small" disabled={!canWrite} onClick={() => onEditFact(selected.fact_key)}>
                                                {t('common.edit', null, '编辑')}
                                            </Button>
                                        </Space>
                                    )}
                                    <div>
                                        <Text strong size="small">{t('projects.graphDirectLinks', null, '直接关系')} · {shownEdges.length}</Text>
                                        {shownEdges.length ? (
                                            <ul className="project-graph-neighbors">
                                                {shownEdges.map((edge, index) => {
                                                    const outgoing = edge.source === selected.id;
                                                    const peer = byId.get(outgoing ? edge.target : edge.source);
                                                    return (
                                                        <li key={edge.id || index}>
                                                            <div className="project-graph-edge-meta">
                                                                <span>{outgoing ? '→' : '←'} {outgoing ? t('projects.graphEdgeOutgoing', null, '出边') : t('projects.graphEdgeIncoming', null, '入边')}</span>
                                                                <code>{edge.type}</code>
                                                                {edge.confidence === 'tentative' && <span>{t('projects.graphConfidence.tentative', null, '待确认')}</span>}
                                                            </div>
                                                            <button type="button" className="project-graph-peer project-link-button" onClick={() => openNode(peer.id)} title={peer.fact_key}>
                                                                {nodeTitle(peer)}
                                                            </button>
                                                        </li>
                                                    );
                                                })}
                                            </ul>
                                        ) : <div><Text type="tertiary" size="small">{t('projects.graphEdgesEmpty', null, '暂无关系边')}</Text></div>}
                                    </div>
                                </div>
                            </aside>
                        )}
                    </div>
                </>
            ) : graph.loading ? (
                <div className="project-graph-loading"><Spin /></div>
            ) : currentGraph && !graph.error ? (
                <Empty
                    title={t('projects.graphEmptyTitle', null, '构建攻击路径图')}
                    description={t('projects.graphEmpty', null, '暂无路径图数据。为 finding/exploit 类事实添加关系边（discovered_on → target/*）后将在此展示。')}
                    style={{ padding: 40 }}
                >
                    <div className="project-graph-empty-steps">
                        <Text type="tertiary">{t('projects.graphEmptyStep1', null, '添加 target 类事实（目标、域名、入口）')}</Text>
                        <Text type="tertiary">{t('projects.graphEmptyStep2', null, '记录 finding / exploit 并在 links 中连边')}</Text>
                        <Text type="tertiary">{t('projects.graphEmptyStep3', null, '使用「连边」模式或编辑事实手动补关系')}</Text>
                    </div>
                    <Button type="primary" disabled={!canWrite} onClick={onAddFact}>
                        {t('projects.graphEmptyCta', null, '添加第一条事实')}
                    </Button>
                </Empty>
            ) : null}

            {showEdges ? (
                <div className="project-edge-table">
                    <ErrorBanner error={errorProp(edgeList.error)} onRetry={() => edgeList.reload()} />
                    {(edgeList.data || []).length ? (
                        <ul className="project-edge-rows">
                            {edgeList.data.map(edge => (
                                <li key={edge.id} className="project-edge-row">
                                    <code>{edge.source_fact_key}</code>
                                    <span className="project-edge-type">{edge.edge_type}</span>
                                    <span className="project-edge-arrow">→</span>
                                    <code>{edge.target_fact_key}</code>
                                    <Tag size="small" color={edge.confidence === 'tentative' ? 'amber' : 'green'}>
                                        {(CONFIDENCE_META[edge.confidence] || {}).label || edge.confidence || '—'}
                                    </Tag>
                                    <Button
                                        size="small"
                                        theme="borderless"
                                        type="danger"
                                        disabled={!canWrite}
                                        onClick={() => deleteEdge(edge)}
                                    >
                                        {t('projects.graphDeleteEdge', null, '删边')}
                                    </Button>
                                </li>
                            ))}
                        </ul>
                    ) : (
                        <Text type="tertiary" size="small">{t('projects.graphEdgesEmpty', null, '暂无关系边')}</Text>
                    )}
                </div>
            ) : null}
        </SectionCard>
    );
}
