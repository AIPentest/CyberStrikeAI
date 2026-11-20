import { useEffect, useRef, useState } from 'react';
import { toastError } from '../../components/ui';
import { bindGraphCardWheel, CARD_HEIGHT, CARD_WIDTH, GraphNodeCard, visibleGraphCards, zoomGraph } from '../../components/graph-card';
import { useT } from '../../lib/i18n';
import { GRAPH_KIND_COLOR, kindText } from './labels';
import { GRAPH_COLUMN_GAP, graphPositions, updatedGraphPositions } from './graphLayout';

const STYLE = [
    {
        selector: 'node',
        style: {
            shape: 'round-rectangle', width: CARD_WIDTH, height: CARD_HEIGHT,
            'background-color': 'data(color)', 'background-opacity': 0.13,
            'border-color': 'data(color)', 'border-opacity': 0.42, 'border-width': 1.2
        }
    },
    { selector: 'node.current', style: { 'background-opacity': 0.78 } },
    {
        selector: 'edge',
        style: {
            width: 2.1,
            'line-color': 'data(color)',
            'target-arrow-color': 'data(color)',
            'target-arrow-shape': 'triangle',
            'arrow-scale': 0.8,
            'curve-style': 'bezier',
            opacity: 0.66
        }
    },
    {
        selector: 'edge.forward',
        style: { 'source-endpoint': '50% 0px', 'target-endpoint': '-50% 0px' }
    },
    {
        selector: 'edge.backward',
        style: { 'source-endpoint': '-50% 0px', 'target-endpoint': '50% 0px' }
    },
    {
        selector: 'edge.flow',
        style: { 'curve-style': 'round-taxi', 'taxi-direction': 'rightward', 'taxi-radius': 16 }
    },
    {
        selector: 'edge.long-flow',
        style: {
            'curve-style': 'unbundled-bezier', 'control-point-weights': 0.45,
            'control-point-distances': 'data(bend)'
        }
    },
    { selector: 'edge[projection = "archive"]', style: { 'line-style': 'dashed' } },
    { selector: 'edge[projection = "details"]', style: { 'line-style': 'dotted' } },
    { selector: 'edge.cross-link', style: { opacity: 0.31 } },
    { selector: 'node.dim', style: { opacity: 0.32 } },
    { selector: 'edge.dim', style: { opacity: 0.12 } },
    {
        selector: 'edge.related',
        style: { width: 2.8, opacity: 1, 'z-index': 2 }
    },
    { selector: '.hidden', style: { display: 'none' } }
];

const COMPACT_KINDS = new Set(['goal', 'step', 'finding', 'guidance']);

function fitGraph(cy) {
    const nodes = cy.nodes(':visible');
    if (nodes.empty()) return;
    cy.fit(nodes, 45);
    if (cy.zoom() > 1) cy.zoom(1).center(nodes);
}

function graphElements(model, positions) {
    return [
        ...[...model.nodes.values()].map(node => ({
            data: {
                id: node.id, kind: node.kind,
                color: GRAPH_KIND_COLOR[node.kind] || '#86909c'
            },
            position: positions.get(node.id)
        })),
        ...model.edges.map(edge => {
            const source = positions.get(edge.source);
            const target = positions.get(edge.target);
            const dx = target.x - source.x;
            const classes = [];
            if (dx !== 0) classes.push(dx > 0 ? 'forward' : 'backward');
            if (dx > 0 && model.layoutEdges.has(edge.id)) classes.push(dx > GRAPH_COLUMN_GAP * 1.5 ? 'long-flow' : 'flow');
            if (!model.layoutEdges.has(edge.id)) classes.push('cross-link');
            return {
                data: {
                    ...edge, projection: edge.projection || '',
                    bend: target.y < source.y ? -28 : 28
                },
                classes: classes.join(' ')
            };
        })
    ];
}

export default function GraphCanvas({ model, graphKey, selectedId, showAll, locate, onSelect, chartRef }) {
    const t = useT();
    const containerRef = useRef(null);
    const layerRef = useRef(null);
    const controlsRef = useRef({ model, selectedId, showAll, locate, onSelect });
    const [cards, setCards] = useState([]);

    useEffect(() => {
        controlsRef.current = { model, selectedId, showAll, locate, onSelect };
        chartRef.current?.sync();
    }, [model, selectedId, showAll, locate, onSelect, chartRef]);

    useEffect(() => {
        const container = containerRef.current;
        const layer = layerRef.current;
        if (!graphKey) {
            setCards([]);
            return undefined;
        }
        let disposed = false;
        let cy;
        let frame;
        let resize;
        let unbindWheel;
        let anchorId;
        let appliedModel;
        let centeredRequest;
        let previousSelection;
        let previousShowAll = controlsRef.current.showAll;
        setCards([]);

        import('cytoscape').then(({ default: cytoscape }) => {
            if (disposed) return;
            cy = cytoscape({
                container, style: STYLE,
                layout: { name: 'preset', fit: false },
                wheelSensitivity: 0.2, minZoom: 0.005, maxZoom: 3,
                boxSelectionEnabled: false,
                autounselectify: true
            });
            unbindWheel = bindGraphCardWheel(layer, cy);
            const selectedNode = () => cy.$id(controlsRef.current.selectedId)[0];
            const updateCards = () => {
                cancelAnimationFrame(frame);
                frame = requestAnimationFrame(() => setCards(visibleGraphCards(cy, {
                    anchorId,
                    allowCompact: element => COMPACT_KINDS.has(element.data('kind'))
                })));
            };
            const syncSelection = () => {
                const { model, selectedId, showAll, locate } = controlsRef.current;
                const selected = selectedNode();
                const related = selected?.connectedEdges() || cy.collection();
                const neighbors = related.connectedNodes();
                const overview = !showAll && model.dense && model.overview.size > 0;
                cy.batch(() => {
                    // Selection is controlled by the detail panel, including label and search picks.
                    cy.nodes().forEach(node => {
                        const nearby = node === selected || neighbors.has(node);
                        node.toggleClass('current', node === selected);
                        node.toggleClass('dim', Boolean(selected && !nearby));
                        node.toggleClass('hidden', overview && !nearby && !model.overview.has(node.id()));
                    });
                    cy.edges().forEach(edge => {
                        const incident = related.has(edge);
                        edge.toggleClass('related', incident);
                        edge.toggleClass('dim', Boolean(selected && !incident));
                        edge.toggleClass('hidden', !showAll && model.dense && !model.backbone.has(edge.id()) && !incident);
                    });
                });
                if (locate && locate !== centeredRequest && selected?.id() === locate.id) {
                    centeredRequest = locate;
                    const zoom = Math.max(cy.zoom(), 1);
                    cy.zoom(zoom).center(selected);
                } else if (!selected && (showAll !== previousShowAll
                    || (!selectedId && overview && previousSelection?.hidden()))) {
                    fitGraph(cy);
                }
                previousSelection = selected;
                previousShowAll = showAll;
                updateCards();
            };
            const sync = (reset = false) => {
                const { model } = controlsRef.current;
                const initial = !appliedModel;
                if (reset || model !== appliedModel) {
                    const positions = reset ? graphPositions(model) : updatedGraphPositions(
                        model, new Map(cy.nodes().map(node => [node.id(), node.position()]))
                    );
                    appliedModel = model;
                    anchorId = [...model.nodes.values()].find(node => node.kind === 'goal')?.id;
                    cy.json({ elements: graphElements(model, positions) });
                }
                syncSelection();
                if (reset || (initial && !centeredRequest)) fitGraph(cy);
            };
            chartRef.current = {
                sync,
                fit: () => fitGraph(cy),
                zoom: factor => zoomGraph(cy, factor),
                resetLayout: () => sync(true)
            };
            sync();
            cy.on('pan zoom position', updateCards);
            cy.on('tap', event => {
                if (event.target === cy) controlsRef.current.onSelect('');
                else if (event.target.isNode()) controlsRef.current.onSelect(event.target.id());
            });


            let width = cy.width();
            let height = cy.height();
            resize = new ResizeObserver(() => {
                const previousWidth = width;
                const previousHeight = height;
                cy.resize();
                const nextWidth = cy.width();
                const nextHeight = cy.height();
                if (nextWidth === width && nextHeight === height) return;
                cy.panBy({ x: (nextWidth - width) / 2, y: (nextHeight - height) / 2 });
                width = nextWidth;
                height = nextHeight;
                const selected = selectedNode();
                const point = selected?.renderedPosition();
                if (point && (point.x < 30 || point.x > width - 30 || point.y < 30 || point.y > height - 30)) {
                    cy.center(selected);
                } else if (!selected && (nextWidth < previousWidth || nextHeight < previousHeight)) {
                    fitGraph(cy);
                }
                updateCards();
            });
            resize.observe(container);
        }).catch(error => { if (!disposed) toastError(error, '图谱渲染失败'); });

        return () => {
            disposed = true;
            cancelAnimationFrame(frame);
            resize?.disconnect();
            unbindWheel?.();
            cy?.destroy();
            chartRef.current = null;
        };
    }, [graphKey, chartRef]);

    return (
        <div className="assessment-graph-viewport" role="group" aria-label="探索图">
            <div ref={containerRef} className="assessment-graph-canvas" />
            <div ref={layerRef} className="graph-node-layer">
                {cards.map(card => {
                    const node = model.nodes.get(card.id);
                    if (!node) return null;
                    const title = (node.title || node.id).replace(/\s+/g, ' ').trim() || node.id;
                    return (
                        <GraphNodeCard
                            card={card}
                            key={node.id}
                            id={node.id}
                            title={title}
                            type={node.kind}
                            category={kindText(t, node.kind)}
                            color={GRAPH_KIND_COLOR[node.kind] || '#86909c'}
                            factKey={node.id}
                            boundary={node.boundary}
                            selected={node.id === selectedId}
                            onSelect={onSelect}
                        />
                    );
                })}
            </div>
        </div>
    );
}
