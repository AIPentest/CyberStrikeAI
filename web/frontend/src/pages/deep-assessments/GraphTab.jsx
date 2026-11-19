/** 探索图：cytoscape 渲染、节点下钻与证据路径。 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Empty, RadioGroup, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { SectionCard, ErrorBanner, JsonViewer, KeyValueList, toastError } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { GRAPH_KIND_COLOR, kindText, stateText, truncateText } from './labels';

const { Text, Paragraph } = Typography;

const CY_STYLE = [
    {
        selector: 'node',
        style: {
            'background-color': 'data(color)',
            label: 'data(label)',
            'font-size': 10,
            'text-wrap': 'wrap',
            'text-max-width': 120,
            'text-valign': 'bottom',
            'text-margin-y': 5,
            color: '#4e5969',
            width: 26,
            height: 26,
            'border-width': 2,
            'border-color': '#ffffff'
        }
    },
    {
        selector: 'node[boundary = 1]',
        style: { shape: 'round-rectangle', width: 18, height: 18, opacity: 0.7 }
    },
    {
        selector: 'node:selected',
        style: { 'border-color': '#0066ff', 'border-width': 4 }
    },
    {
        selector: 'edge',
        style: {
            width: 1,
            'line-color': '#c9cdd4',
            'target-arrow-color': '#c9cdd4',
            'target-arrow-shape': 'triangle',
            'arrow-scale': 0.7,
            'curve-style': 'bezier',
            opacity: 0.85
        }
    },
    {
        selector: 'edge[projection = "archive"]',
        style: { 'line-style': 'dashed', 'line-color': '#d9d9d9' }
    },
    {
        selector: 'edge[projection = "details"]',
        style: { 'line-style': 'dotted' }
    }
];

export default function GraphTab({ id, reloadToken }) {
    const t = useT();
    const [view, setView] = useState('summary');
    const [scope, setScope] = useState('');
    const [offset, setOffset] = useState(0);
    const [selectedId, setSelectedId] = useState('');
    const [node, setNode] = useState(null);
    const [relations, setRelations] = useState([]);
    const [linksOffset, setLinksOffset] = useState(0);
    const [linksMore, setLinksMore] = useState(false);
    const [bodyOffset, setBodyOffset] = useState(0);
    const [bodyMore, setBodyMore] = useState(false);
    const [nodeLoading, setNodeLoading] = useState(false);
    const [pathView, setPathView] = useState(null);
    const [pathLoading, setPathLoading] = useState(false);
    const containerRef = useRef(null);
    const cyRef = useRef(null);
    const selectRef = useRef(() => {});

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/graph`, { view, scope, offset, limit: 80 }),
        [id, view, scope, offset, reloadToken]
    );
    const graph = resource.data;
    const nodes = graph?.nodes || [];

    const selectNode = useCallback(async (nodeId, options = {}) => {
        const { appendBody = false, appendLinks = false } = options;
        if (!nodeId) return;
        setSelectedId(nodeId);
        const cy = cyRef.current;
        if (cy) {
            cy.elements().unselect();
            cy.$id(nodeId).select();
        }
        setNodeLoading(true);
        try {
            const data = await api.get(`/deep-assessments/${id}/graph/node`, {
                node_id: nodeId,
                offset: appendBody ? bodyOffset : 0,
                links_offset: appendLinks ? linksOffset : 0
            });
            const next = data?.node || null;
            setNode(prev => (
                appendBody && prev && next && prev.id === next.id
                    ? { ...next, body: `${prev.body || ''}${next.body || ''}` }
                    : next
            ));
            setBodyOffset(Number(data?.next_offset) || 0);
            setBodyMore(Boolean(data?.has_more));
            setRelations(prev => (appendLinks ? [...prev, ...(data?.relations || [])] : (data?.relations || [])));
            setLinksOffset(Number(data?.relations_next_offset) || 0);
            setLinksMore(Boolean(data?.relations_has_more));
            setPathView(null);
        } catch (error) {
            toastError(error, '读取节点失败');
        } finally {
            setNodeLoading(false);
        }
    }, [id, bodyOffset, linksOffset]);

    useEffect(() => { selectRef.current = selectNode; }, [selectNode]);

    // cytoscape 仅在图谱页按需加载，容器高度由页面样式固定。
    useEffect(() => {
        const source = graph?.nodes || [];
        if (!source.length || !containerRef.current) {
            if (cyRef.current) {
                cyRef.current.destroy();
                cyRef.current = null;
            }
            return undefined;
        }
        let disposed = false;
        let cy = null;
        const unique = [];
        const seen = new Set();
        source.forEach(item => {
            if (!seen.has(item.id)) {
                seen.add(item.id);
                unique.push(item);
            }
        });
        import('cytoscape').then(module => {
            const cytoscape = module.default || module;
            if (disposed || !containerRef.current) return;
            const elements = [
                ...unique.map(item => ({
                    data: {
                        id: item.id,
                        label: truncateText(item.title || item.id, 32),
                        kind: item.kind,
                        state: item.state,
                        color: GRAPH_KIND_COLOR[item.kind] || '#86909c',
                        boundary: item.boundary ? 1 : 0
                    }
                })),
                ...(graph.edges || [])
                    .filter(edge => seen.has(edge.source) && seen.has(edge.target))
                    .map((edge, index) => ({
                        data: {
                            id: `edge-${index}`,
                            source: edge.source,
                            target: edge.target,
                            relation: edge.relation,
                            projection: edge.projection || ''
                        }
                    }))
            ];
            cy = cytoscape({
                container: containerRef.current,
                elements,
                style: CY_STYLE,
                layout: {
                    name: unique.length > 45 ? 'cose' : 'breadthfirst',
                    directed: true,
                    padding: 28,
                    animate: false,
                    spacingFactor: 1.2
                },
                wheelSensitivity: 0.2,
                minZoom: 0.15,
                maxZoom: 3
            });
            cy.on('tap', 'node', event => selectRef.current?.(event.target.id()));
            cyRef.current = cy;
        }).catch(error => {
            toastError(error, '图谱渲染失败');
        });
        return () => {
            disposed = true;
            if (cy) cy.destroy();
            if (cyRef.current === cy) cyRef.current = null;
        };
    }, [graph]);

    const loadPath = async () => {
        if (!node) return;
        setPathLoading(true);
        try {
            const data = await api.get(`/deep-assessments/${id}/evidence-path`, { fact_id: node.id });
            setPathView(data);
        } catch (error) {
            toastError(error, '读取证据路径失败');
        } finally {
            setPathLoading(false);
        }
    };

    const fit = () => cyRef.current?.fit(undefined, 30);
    const relayout = () => {
        const cy = cyRef.current;
        if (!cy) return;
        cy.layout({
            name: cy.nodes().length > 45 ? 'cose' : 'breadthfirst',
            directed: true,
            padding: 28,
            animate: true,
            spacingFactor: 1.2
        }).run();
    };

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
            <Button size="small" onClick={fit}>适应画布</Button>
            <Button size="small" onClick={relayout}>重新布局</Button>
            <Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>
        </Space>
    );

    const nodeOptions = nodes.map(item => ({
        value: item.id,
        label: `${item.boundary ? '引用 · ' : ''}${kindText(t, item.kind)} · ${truncateText(item.title || item.id, 40)}`
    }));

    return (
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.graph', null, '探索图')}
                description="摘要用于续接；原始观察、执行回执与证据路径可展开查看。点击节点读取原文。"
                extra={headExtra}
            >
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
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
                </div>
                {resource.loading && !graph ? <Text type="tertiary">正在读取图谱…</Text> : null}
                <div className="assessment-graph-toolbar">
                    <Select
                        value={selectedId || ''}
                        onChange={value => { if (value) selectNode(String(value)); }}
                        optionList={nodeOptions}
                        placeholder="选择节点查看详情"
                        style={{ width: 320 }}
                        showClear={false}
                    />
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 80))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">偏移 {offset}</Text>
                        <Button size="small" disabled={!graph?.has_more} onClick={() => setOffset(value => value + 80)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
                {graph && nodes.length === 0 ? (
                    <Empty
                        title="暂无图谱节点"
                        description="开始测评后会生成目标、意图、事实与证据节点。"
                        style={{ padding: 48 }}
                    />
                ) : (
                    <div className="assessment-graph-layout">
                        <div ref={containerRef} className="assessment-graph-canvas" role="img" aria-label="探索图" />
                        <div className="assessment-node-panel" aria-live="polite">
                            {!selectedId ? (
                                <Empty title="选择节点" description="点击图中节点或上方下拉框，读取节点原文与证据路径。" style={{ padding: 24 }} />
                            ) : (
                                <div className="assessment-node-body">
                                    <div className="assessment-node-head">
                                        <Text strong>{node?.title || selectedId}</Text>
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
                                                                onClick={() => selectNode(relation.node?.id)}
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
                            )}
                        </div>
                    </div>
                )}
            </SectionCard>
        </div>
    );
}
