/** 事实关系图：cytoscape 动态加载，节点点击展示事实详情。 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Checkbox, Empty, Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { ErrorBanner, SectionCard, confirmAction, toastError, toastSuccess } from '../../components/ui';
import { CONFIDENCE_META, NODE_COLORS, NODE_TYPE_LABEL, errorProp, errText } from './helpers';

const { Text } = Typography;

function themeColors(container) {
    const styles = getComputedStyle(container);
    const read = (name, fallback) => styles.getPropertyValue(name).trim() || fallback;
    return {
        text: read('--semi-color-text-0', '#1f2329'),
        muted: read('--semi-color-text-2', '#646a73'),
        border: read('--semi-color-border', '#e5e6eb'),
        bg: read('--semi-color-bg-1', '#ffffff')
    };
}

function buildStyle(colors) {
    return [
        {
            selector: 'node',
            style: {
                'background-color': 'data(color)',
                label: 'data(label)',
                width: 26,
                height: 26,
                'font-size': 10,
                color: colors.text,
                'text-valign': 'bottom',
                'text-halign': 'center',
                'text-margin-y': 6,
                'text-wrap': 'wrap',
                'text-max-width': 132,
                'border-width': 2,
                'border-color': colors.bg,
                'text-outline-width': 0
            }
        },
        {
            selector: 'node[type = "missing"]',
            style: { 'border-style': 'dashed', 'border-color': '#94a3b8' }
        },
        {
            selector: 'node:selected',
            style: { 'border-width': 4, 'border-color': '#f59e0b' }
        },
        {
            selector: 'edge',
            style: {
                width: 1.5,
                'line-color': colors.border,
                'target-arrow-color': colors.border,
                'target-arrow-shape': 'triangle',
                'arrow-scale': 0.7,
                'curve-style': 'bezier',
                'font-size': 8,
                color: colors.muted,
                label: 'data(type)',
                'text-rotation': 'autorotate',
                'text-background-color': colors.bg,
                'text-background-opacity': 0.85,
                'text-background-padding': 1
            }
        },
        { selector: 'edge[confidence = "tentative"]', style: { 'line-style': 'dashed', 'line-dash-pattern': [4, 3] } },
        { selector: 'edge[type = "links_vuln"]', style: { 'line-color': '#dc2626', 'target-arrow-color': '#dc2626', 'line-style': 'dotted' } }
    ];
}

function GraphCanvas({ graph, search, onSelect, onReady }) {
    const containerRef = useRef(null);
    const cyRef = useRef(null);
    const selectRef = useRef(onSelect);
    const readyRef = useRef(onReady);
    const [ready, setReady] = useState(false);
    const [importError, setImportError] = useState(null);

    selectRef.current = onSelect;
    readyRef.current = onReady;

    useEffect(() => {
        const container = containerRef.current;
        if (!container || !graph?.nodes?.length) return undefined;
        let cancelled = false;
        let instance = null;
        setImportError(null);

        import('cytoscape')
            .then(mod => {
                if (cancelled || !containerRef.current) return;
                const cytoscape = mod.default || mod;
                const colors = themeColors(containerRef.current);
                const elements = [
                    ...graph.nodes.map(node => ({
                        group: 'nodes',
                        data: {
                            id: node.id,
                            label: node.label || node.fact_key,
                            fact_key: node.fact_key,
                            type: node.type || 'note',
                            category: node.category,
                            confidence: node.confidence,
                            summary: node.summary,
                            color: NODE_COLORS[node.type] || NODE_COLORS.missing
                        }
                    })),
                    ...(graph.edges || []).map((edge, index) => ({
                        group: 'edges',
                        data: {
                            id: edge.id || `e-${index}`,
                            source: edge.source,
                            target: edge.target,
                            type: edge.type,
                            confidence: edge.confidence
                        }
                    }))
                ];
                instance = cytoscape({
                    container: containerRef.current,
                    elements,
                    style: buildStyle(colors),
                    layout: { name: 'cose', animate: false, padding: 36, idealEdgeLength: 110, nodeRepulsion: 9000, fit: true },
                    wheelSensitivity: 0.2,
                    boxSelectionEnabled: false,
                    minZoom: 0.2,
                    maxZoom: 2.5
                });
                cyRef.current = instance;
                instance.on('tap', 'node', event => selectRef.current?.(event.target.data()));
                instance.on('tap', event => {
                    if (event.target === instance) selectRef.current?.(null);
                });
                readyRef.current?.(instance);
                setReady(true);
            })
            .catch(error => {
                if (!cancelled) setImportError(error);
            });

        return () => {
            cancelled = true;
            setReady(false);
            cyRef.current = null;
            readyRef.current?.(null);
            if (instance) instance.destroy();
        };
    }, [graph]);

    useEffect(() => {
        const cy = cyRef.current;
        if (!cy) return;
        const query = search.trim().toLowerCase();
        cy.batch(() => {
            cy.nodes().forEach(node => {
                const data = node.data();
                const hit = !query || [data.fact_key, data.label, data.summary, data.category]
                    .join(' ').toLowerCase().includes(query);
                node.style('opacity', hit ? 1 : 0.16);
            });
        });
    }, [search, ready, graph]);

    return (
        <div className="project-graph-canvas-wrap">
            <div ref={containerRef} className="project-graph-canvas" />
            {importError ? (
                <div className="project-graph-overlay">
                    <Text type="danger">cytoscape 加载失败，请刷新页面重试</Text>
                </div>
            ) : null}
        </div>
    );
}

export default function FactGraphPanel({
    projectId, canWrite, revision = 0, onOpenFact, onEditFact, onAddFact, onChanged
}) {
    const t = useT();
    const [view, setView] = useState('path');
    const [hideDeprecated, setHideDeprecated] = useState(true);
    const [search, setSearch] = useState('');
    const [selected, setSelected] = useState(null);
    const [showEdges, setShowEdges] = useState(false);
    const cyRef = useRef(null);

    const graph = useResource(() => api.get(`/projects/${projectId}/fact-graph`, {
        view,
        exclude_deprecated: hideDeprecated ? '1' : '0'
    }), [projectId, view, hideDeprecated, revision]);

    const nodes = graph.data?.nodes || [];
    const edges = graph.data?.edges || [];

    const edgeList = useResource(
        () => (showEdges ? api.get(`/projects/${projectId}/fact-edges`) : Promise.resolve(null)),
        [projectId, showEdges, revision]
    );

    const deleteEdge = (edge) => {
        confirmAction({
            title: t('projects.graphDeleteEdge', null, '删边'),
            content: t('projects.confirmDeleteGraphEdge', null, '确定删除此关系边？'),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/projects/${projectId}/fact-edges/${edge.id}`);
                    toastSuccess(t('projects.graphEdgeDeleteSuccess', null, '边已删除'));
                    graph.reload({ silent: true });
                    edgeList.reload({ silent: true });
                    onChanged?.();
                } catch (error) {
                    toastError(errText(error, t('projects.graphEdgeDeleteFailed', null, '删除边失败')));
                }
            }
        });
    };

    const selectedEdges = useMemo(() => {
        if (!selected) return [];
        return edges
            .filter(edge => edge.source === selected.id || edge.target === selected.id)
            .map(edge => ({
                ...edge,
                outgoing: edge.source === selected.id,
                peer: edge.source === selected.id ? edge.target : edge.source
            }));
    }, [edges, selected]);

    useEffect(() => { setSelected(null); }, [projectId, view, hideDeprecated]);

    return (
        <SectionCard
            title={t('projects.tabGraph', null, '攻击路径')}
            description={t('projects.graphToolbarHint', null, '攻击路径图箭头与事实存储方向一致（source → target）')}
            extra={(
                <Space>
                    <Text type="tertiary" size="small">
                        {t('projects.graphStats', { nodes: nodes.length, edges: edges.length }, `节点: ${nodes.length} | 边: ${edges.length}`)}
                    </Text>
                    <Button icon={<IconRefresh />} theme="borderless" loading={graph.loading} onClick={() => { graph.reload(); onChanged?.(); }} />
                </Space>
            )}
        >
            <ErrorBanner error={errorProp(graph.error)} onRetry={graph.reload} />
            <div className="project-toolbar">
                <Select
                    value={view}
                    onChange={setView}
                    style={{ width: 150 }}
                    optionList={[
                        { value: 'path', label: t('projects.graphViewPath', null, '攻击路径') },
                        { value: 'full', label: t('projects.graphViewFull', null, '完整关系') }
                    ]}
                />
                <Input
                    prefix={<IconSearch />}
                    value={search}
                    showClear
                    placeholder={t('projects.graphSearchPlaceholder', null, '搜索节点…')}
                    aria-label={t('projects.graphSearchSr', null, '搜索节点')}
                    onChange={setSearch}
                    style={{ width: 240 }}
                />
                <Checkbox checked={hideDeprecated} onChange={event => setHideDeprecated(event.target.checked)}>
                    {t('projects.hideDeprecated', null, '隐藏废弃')}
                </Checkbox>
                <Button
                    theme="borderless"
                    disabled={!graph.data}
                    onClick={() => cyRef.current?.fit(undefined, 36)}
                >
                    {t('projects.graphCenter', null, '居中')}
                </Button>
                <Button theme="borderless" onClick={() => setShowEdges(value => !value)}>
                    {t('projects.graphEdgesTitle', null, '关系边')}（{edges.length}）
                </Button>
            </div>

            {!graph.loading && !nodes.length ? (
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
            ) : (
                <div className="project-graph-layout">
                    <GraphCanvas
                        graph={graph.data}
                        search={search}
                        onSelect={setSelected}
                        onReady={instance => { cyRef.current = instance; }}
                    />
                    <div className={`project-graph-side${selected ? ' is-open' : ''}`}>
                        {selected ? (
                            <div className="project-detail-stack">
                                <div>
                                    <Text strong>{selected.fact_key}</Text>
                                    <div className="project-graph-side-tags">
                                        <Tag size="small" color="blue" type="light">{NODE_TYPE_LABEL[selected.type] || selected.type}</Tag>
                                        <Tag size="small" color={(CONFIDENCE_META[selected.confidence] || {}).color || 'grey'}>
                                            {(CONFIDENCE_META[selected.confidence] || {}).label || selected.confidence || '—'}
                                        </Tag>
                                    </div>
                                </div>
                                <Text type="tertiary" size="small">{selected.summary || '—'}</Text>
                                <div>
                                    <Text strong size="small">{t('projects.graphEdgesTitle', null, '关系边')}</Text>
                                    {selectedEdges.length ? (
                                        <ul className="project-link-list">
                                            {selectedEdges.map(edge => (
                                                <li key={edge.id}>
                                                    <span className="project-graph-edge-dir">
                                                        {edge.outgoing
                                                            ? t('projects.graphEdgeOutgoing', null, '出边')
                                                            : t('projects.graphEdgeIncoming', null, '入边')}
                                                    </span>
                                                    <code>{edge.type}</code>
                                                    {' '}
                                                    <span title={edge.peer}>{edge.outgoing ? '→' : '←'} {edge.peer}</span>
                                                </li>
                                            ))}
                                        </ul>
                                    ) : <Text type="tertiary" size="small">{t('projects.graphEdgesEmpty', null, '暂无关系边')}</Text>}
                                </div>
                                <Space>
                                    <Button size="small" onClick={() => onOpenFact(selected.fact_key)}>{t('projects.details', null, '详情')}</Button>
                                    <Button size="small" disabled={!canWrite} onClick={() => onEditFact(selected.fact_key)}>
                                        {t('common.edit', null, '编辑')}
                                    </Button>
                                </Space>
                            </div>
                        ) : (
                            <Text type="tertiary" size="small">点击节点查看事实详情</Text>
                        )}
                    </div>
                </div>
            )}

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
