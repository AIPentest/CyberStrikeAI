/** 探索图：cytoscape 渲染、节点下钻与证据路径。 */
import { useCallback, useMemo, useRef, useState } from 'react';
import { Button, Empty, RadioGroup, Select, Space, Switch, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import { SectionCard, ErrorBanner, JsonViewer, KeyValueList, toastError } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { GraphNodeIcon } from '../../components/graph-card';
import { GRAPH_KIND_COLOR, kindText, stateText, truncateText } from './labels';
import GraphCanvas from './GraphCanvas';
import GraphBrief from './GraphBrief';
import { prepareGraph } from './graphLayout';

const { Text, Paragraph } = Typography;

export default function GraphTab({
    id, task, reloadToken, onGoIntents, autoRefresh, onAutoRefreshChange, onRefreshTask, refreshPaused
}) {
    const t = useT();
    const [view, setView] = useState('summary');
    const [scope, setScope] = useState('');
    const [offset, setOffset] = useState(0);
    const [selectedId, setSelectedId] = useState('');
    const [showAll, setShowAll] = useState(false);
    const [locate, setLocate] = useState(null);
    const [node, setNode] = useState(null);
    const [relations, setRelations] = useState([]);
    const [linksOffset, setLinksOffset] = useState(0);
    const [linksMore, setLinksMore] = useState(false);
    const [bodyOffset, setBodyOffset] = useState(0);
    const [bodyMore, setBodyMore] = useState(false);
    const [nodeLoading, setNodeLoading] = useState(false);
    const [pathView, setPathView] = useState(null);
    const [pathLoading, setPathLoading] = useState(false);
    const chartRef = useRef(null);
    const lastGraph = useRef(null);
    const polling = useRef(false);
    const nodeRequest = useRef(0);

    const graphKey = JSON.stringify([id, view, scope, offset]);
    const resource = useResource(
        async () => {
            const graph = await api.get(`/deep-assessments/${id}/graph`, { view, scope, offset, limit: 80 });
            const snapshot = JSON.stringify(graph);
            const previous = lastGraph.current;
            if (previous?.key === graphKey && previous.snapshot === snapshot) return previous;
            const next = { key: graphKey, snapshot, graph };
            lastGraph.current = next;
            return next;
        },
        [id, view, scope, offset, reloadToken]
    );
    const graph = resource.data?.graph;
    const model = useMemo(() => prepareGraph(graph), [graph]);
    const nodes = [...model.nodes.values()];
    const collapsedNodes = model.overview.size ? model.nodes.size - model.overview.size : 0;
    const canExpand = model.dense && (collapsedNodes > 0 || model.edges.length > model.backbone.size);

    const selectNode = useCallback(async (nodeId, options = {}) => {
        const { appendBody = false, appendLinks = false, center = false } = options;
        const request = ++nodeRequest.current;
        setSelectedId(nodeId);
        if (center) setLocate({ id: nodeId });
        if (!appendBody && !appendLinks) {
            setNode(null);
            setRelations([]);
            setPathView(null);
        }
        setPathLoading(false);
        setNodeLoading(Boolean(nodeId));
        if (!nodeId) return;
        try {
            const data = await api.get(`/deep-assessments/${id}/graph/node`, {
                node_id: nodeId,
                offset: appendBody ? bodyOffset : 0,
                links_offset: appendLinks ? linksOffset : 0
            });
            if (request !== nodeRequest.current) return;
            if (!appendLinks) {
                const next = data?.node || null;
                setNode(prev => (
                    appendBody && prev && next && prev.id === next.id
                        ? { ...next, body: `${prev.body || ''}${next.body || ''}` }
                        : next
                ));
                setBodyOffset(Number(data?.next_offset) || 0);
                setBodyMore(Boolean(data?.has_more));
            }
            if (!appendBody) {
                setRelations(prev => (appendLinks ? [...prev, ...(data?.relations || [])] : (data?.relations || [])));
                setLinksOffset(Number(data?.relations_next_offset) || 0);
                setLinksMore(Boolean(data?.relations_has_more));
            }
        } catch (error) {
            if (request === nodeRequest.current) toastError(error, '读取节点失败');
        } finally {
            if (request === nodeRequest.current) setNodeLoading(false);
        }
    }, [id, bodyOffset, linksOffset]);

    const loadPath = async () => {
        if (!node) return;
        const request = nodeRequest.current;
        setPathLoading(true);
        try {
            const data = await api.get(`/deep-assessments/${id}/evidence-path`, { fact_id: node.id });
            if (request === nodeRequest.current) setPathView(data);
        } catch (error) {
            if (request === nodeRequest.current) toastError(error, '读取证据路径失败');
        } finally {
            if (request === nodeRequest.current) setPathLoading(false);
        }
    };

    const refresh = options => Promise.all([resource.reload(options), onRefreshTask(options)]);

    useInterval(async () => {
        if (document.hidden || refreshPaused || resource.loading || polling.current) return;
        polling.current = true;
        try {
            await refresh({ silent: true });
        } finally {
            polling.current = false;
        }
    }, autoRefresh ? 10000 : 0);

    const headExtra = (
        <Space wrap>
            <RadioGroup
                className="assessment-view-options"
                aria-label={t('deepAssessments.graphView.label', null, '图谱视图')}
                type="button"
                value={view}
                options={[
                    { value: 'summary', label: t('deepAssessments.graphView.summary', null, '主链路') },
                    { value: 'full', label: t('deepAssessments.graphView.full', null, '执行记录') }
                ]}
                onChange={event => { setView(event.target.value); setOffset(0); }}
            />
            <Button size="small" onClick={() => { setScope(''); setOffset(0); }} disabled={!scope && offset === 0}>全图概览</Button>
            <Button size="small" onClick={() => refresh()} loading={resource.loading}>刷新</Button>
        </Space>
    );

    const nodeOptions = nodes.map(item => ({
        value: item.id,
        title: item.title || item.id,
        label: `${item.boundary ? '引用 · ' : ''}${kindText(t, item.kind)} · ${truncateText(item.title || item.id, 40)}`
    }));

    return (
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.graph', null, '探索图')}
                description="点击节点查看原文与直接关联，放大可查看更多卡片内容。"
                extra={headExtra}
            >
                <ErrorBanner error={resource.error} onRetry={() => refresh()} />
                <GraphBrief
                    task={task}
                    t={t}
                    onIntent={stepId => selectNode(`step:${stepId}`, { center: true })}
                    onGoIntents={onGoIntents}
                />
                <div className="assessment-graph-meta">
                    <Text type="tertiary">
                        当前页节点 {graph?.primary_count ?? Math.max(0, nodes.length - 1)} / {graph?.total ?? 0}
                        {' · '}已折叠 {graph?.folded ?? 0}
                        {' · '}隐藏明细 {graph?.details_hidden ?? 0}
                        {' · '}跨页引用 {graph?.boundary_total ?? 0}
                    </Text>
                    {scope ? (
                        <Space>
                            <Tag size="small" color="blue">已聚焦 {scope}</Tag>
                            <Button size="small" theme="borderless" onClick={() => { setScope(''); setOffset(0); }}>返回全图</Button>
                        </Space>
                    ) : null}
                    <Space spacing={6}>
                        <Text size="small" type="tertiary">自动刷新</Text>
                        <Switch size="small" aria-label="自动刷新探索图" checked={autoRefresh} onChange={onAutoRefreshChange} />
                    </Space>
                </div>
                {resource.loading && !graph ? <Text type="tertiary">正在读取图谱…</Text> : null}
                <div className="assessment-graph-toolbar">
                    <Select
                        aria-label="搜索图谱节点"
                        value={model.nodes.has(selectedId) ? selectedId : undefined}
                        onChange={value => selectNode(value ? String(value) : '', { center: Boolean(value) })}
                        optionList={nodeOptions}
                        placeholder="搜索并定位节点"
                        className="assessment-graph-search"
                        filter={(input, option) => `${option.label} ${option.title} ${option.value}`.toLowerCase().includes(input.toLowerCase().trim())}
                        showClear
                    />
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 80))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">第 {Math.floor(offset / 80) + 1} 页</Text>
                        <Button size="small" disabled={!graph?.has_more} onClick={() => setOffset(value => value + 80)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
                {graph && nodes.length === 0 && !selectedId ? (
                    <Empty
                        title="暂无图谱节点"
                        description="开始测评后会生成目标、意图、事实与证据节点。"
                        style={{ padding: 48 }}
                    />
                ) : (
                    <>
                        <div className={`assessment-graph-layout${selectedId ? ' with-details' : ''}`}>
                            <GraphCanvas model={model} graphKey={resource.data?.key} selectedId={selectedId} showAll={showAll} locate={locate} onSelect={selectNode} chartRef={chartRef} />
                            {selectedId ? (
                                <div className="assessment-node-panel" aria-live="polite">
                                    <div className="assessment-node-body">
                                        <div className="assessment-node-head">
                                            <div className="assessment-node-title">
                                                <Text strong>{node?.title || model.nodes.get(selectedId)?.title || selectedId}</Text>
                                                <Button size="small" theme="borderless" onClick={() => selectNode('')}>关闭详情</Button>
                                            </div>
                                            <Space spacing={4}>
                                                {node ? <Tag size="small" color="blue">{kindText(t, node.kind)}</Tag> : null}
                                                {node?.state ? <Tag size="small" color="grey">{stateText(t, node.state)}</Tag> : null}
                                                {node?.boundary ? <Tag size="small" color="amber">跨页引用</Tag> : null}
                                            </Space>
                                        </div>
                                        {nodeLoading ? <Text type="tertiary">正在读取节点…</Text> : null}
                                        {node ? (
                                            <>
                                                <Paragraph className="assessment-pre assessment-node-content">{node.body || '—'}</Paragraph>
                                                {bodyMore ? (
                                                    <Button size="small" onClick={() => selectNode(node.id, { appendBody: true })} loading={nodeLoading}>
                                                        继续读取原文
                                                    </Button>
                                                ) : null}
                                                <KeyValueList
                                                    columns={1}
                                                    items={[
                                                        { label: '节点 ID', value: node.id },
                                                        { label: '意图', value: node.step_id || '—' },
                                                        { label: '更新轮次', value: node.updated_round ?? '—' },
                                                        { label: '更新时间', value: formatDateTime(node.updated_at) }
                                                    ]}
                                                />
                                                {relations.length ? (
                                                    <section className="assessment-section">
                                                        <Text strong>关联节点</Text>
                                                        <div className="assessment-relations">
                                                            {relations.map((relation, index) => (
                                                                <Button
                                                                    key={`${relation.node?.id}-${index}`}
                                                                    size="small"
                                                                    theme="borderless"
                                                                    onClick={() => relation.node?.id && selectNode(relation.node.id, { center: true })}
                                                                >
                                                                    {relation.direction === 'upstream' ? '上游' : '下游'} · {relation.relation} · {truncateText(relation.node?.title, 30)}
                                                                </Button>
                                                            ))}
                                                        </div>
                                                        {linksMore ? (
                                                            <Button size="small" onClick={() => selectNode(node.id, { appendLinks: true })} loading={nodeLoading}>
                                                                继续查看关联
                                                            </Button>
                                                        ) : null}
                                                    </section>
                                                ) : null}
                                                {node.kind === 'fact' ? (
                                                    <Button size="small" onClick={loadPath} loading={pathLoading}>查看证据路径</Button>
                                                ) : null}
                                                {node.step_id ? (
                                                    <Button size="small" theme="borderless" onClick={() => { setScope(node.id); setOffset(0); }}>
                                                        展开此节点分支
                                                    </Button>
                                                ) : null}
                                                {pathView ? (
                                                    <section className="assessment-section">
                                                        <Text strong>证据路径 · {pathView.title || pathView.root}</Text>
                                                        <Text type="tertiary" size="small">
                                                            节点 {Array.isArray(pathView.nodes) ? pathView.nodes.length : 0} · 未归属回执 {pathView.unattributed_receipts || 0}
                                                        </Text>
                                                        <div className="assessment-path-list">
                                                            {(pathView.nodes || []).map(item => (
                                                                <div key={item.id} className="assessment-path-node">
                                                                    <Tag size="small">{kindText(t, item.kind)}</Tag>
                                                                    <Text ellipsis={{ showTooltip: true }}>{item.title || item.id}</Text>
                                                                </div>
                                                            ))}
                                                        </div>
                                                        <JsonViewer title="证据路径原始数据" value={pathView} collapsed />
                                                    </section>
                                                ) : null}
                                                <JsonViewer title="节点来源" value={node.source || {}} collapsed />
                                            </>
                                        ) : null}
                                    </div>
                                </div>
                            ) : null}
                        </div>
                        <div className="assessment-graph-tools">
                            <Space wrap>
                                <Button size="small" aria-label="缩小图谱" disabled={!nodes.length} onClick={() => chartRef.current?.zoom(0.8)}>−</Button>
                                <Button size="small" aria-label="放大图谱" disabled={!nodes.length} onClick={() => chartRef.current?.zoom(1.25)}>+</Button>
                                <Button size="small" disabled={!nodes.length} onClick={() => chartRef.current?.fit()}>适应画布</Button>
                                <Button size="small" disabled={!nodes.length} onClick={() => chartRef.current?.resetLayout()}>重置布局</Button>
                            </Space>
                            {canExpand ? (
                                <Space wrap>
                                    <Text size="small" type="tertiary">
                                        {showAll ? `全部 ${model.edges.length} 条连线` : `主干 ${model.backbone.size} / ${model.edges.length} 条连线`}
                                        {!showAll && collapsedNodes > 0 ? ` · 收起 ${collapsedNodes} 个明细节点` : null}
                                    </Text>
                                    <Button size="small" onClick={() => setShowAll(value => !value)}>{showAll ? '仅看主干' : '显示全部节点与连线'}</Button>
                                </Space>
                            ) : null}
                        </div>
                        {nodes.length ? (
                            <div className="assessment-graph-legend" role="group" aria-label="节点图例">
                                {[...new Set(nodes.map(node => node.kind))].map(kind => (
                                    <span key={kind} style={{ '--graph-node-color': GRAPH_KIND_COLOR[kind] || '#86909c' }}>
                                        <GraphNodeIcon type={kind} />{kindText(t, kind)}
                                    </span>
                                ))}
                            </div>
                        ) : null}
                        {model.legend.length ? (
                            <div className="assessment-graph-legend" role="group" aria-label="连线图例">
                                {model.legend.map(({ label, color }) => (
                                    <span key={label}><i aria-hidden="true" style={{ backgroundColor: color }} />{label}</span>
                                ))}
                            </div>
                        ) : null}
                    </>
                )}
            </SectionCard>
        </div>
    );
}
