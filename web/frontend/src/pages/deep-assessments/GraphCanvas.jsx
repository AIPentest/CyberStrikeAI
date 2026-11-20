import { Fragment, useEffect, useRef, useState } from 'react';
import { toastError } from '../../components/ui';
import { GRAPH_KIND_COLOR } from './labels';
import { GRAPH_COLUMN_GAP, graphPositions } from './graphLayout';

const STYLE = [
    {
        selector: 'node',
        style: {
            'background-color': 'data(color)',
            width: 23, height: 23,
            'border-width': 2, 'border-color': '#ffffff'
        }
    },
    { selector: 'node[kind = "goal"]', style: { width: 31, height: 31 } },
    { selector: 'node[boundary = 1]', style: { shape: 'round-rectangle', width: 17, height: 17 } },
    { selector: 'node.current', style: { 'border-color': '#0066ff', 'border-width': 4 } },
    {
        selector: 'edge',
        style: {
            width: 1.8,
            'line-color': 'data(color)',
            'target-arrow-color': 'data(color)',
            'target-arrow-shape': 'triangle',
            'arrow-scale': 0.8,
            'curve-style': 'bezier',
            opacity: 0.74
        }
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
    { selector: 'node.dim', style: { opacity: 0.32 } },
    { selector: 'edge.dim', style: { opacity: 0.12 } },
    {
        selector: 'edge.related',
        style: { width: 2.8, opacity: 1, 'z-index': 2 }
    },
    { selector: '.hidden', style: { display: 'none' } }
];

const LABEL_WIDTH = 148;
const LABEL_HEIGHT = 22;
const LABEL_PADDING = 7;
const LABEL_PRIORITY = { goal: 0, finding: 1, step: 2, guidance: 3, fact: 4, digest: 5 };
const overlaps = (a, b) => a.x < b.x + b.width && a.x + a.width > b.x
    && a.y < b.y + b.height && a.y + a.height > b.y;

function visibleLabels(cy, model, selected, hovered, measure) {
    const width = cy.width();
    const height = cy.height();
    const zoom = cy.zoom();
    const items = cy.nodes(':visible').map(node => {
        const item = model.nodes.get(node.id());
        const { x, y } = node.renderedPosition();
        const radius = node.renderedOuterWidth() / 2 + 2;
        const priority = node === selected ? -2 : node === hovered ? -1
            : (LABEL_PRIORITY[item.kind] ?? 6) + (item.boundary ? 8 : 0);
        return { node, item, x, y, radius, priority };
    }).filter(({ x, y, radius }) => x >= -radius && x <= width + radius && y >= -radius && y <= height + radius);
    const occupied = items.map(({ x, y, radius }) => ({
        x: x - radius, y: y - radius, width: radius * 2, height: radius * 2
    }));
    const labels = [];
    items.sort((a, b) => a.priority - b.priority);

    for (const { node, item, x, y, radius, priority } of items) {
        if (zoom < 0.64 && priority > 3) continue;
        if (node.hasClass('dim') && priority >= 0) continue;
        const title = (item.title || item.id).replace(/\s+/g, ' ').trim() || item.id;
        const labelWidth = Math.min(LABEL_WIDTH, Math.ceil(measure(title)) + LABEL_PADDING * 2);
        const left = Math.max(4, Math.min(x - labelWidth / 2, width - labelWidth - 4));
        const top = y + radius + 4;
        const candidates = [{ x: left, y: top, width: labelWidth, height: LABEL_HEIGHT }];
        if (priority <= 3) {
            candidates.push(
                { ...candidates[0], y: y - radius - LABEL_HEIGHT - 4 },
                { ...candidates[0], x: x + radius + 5, y: y - LABEL_HEIGHT / 2 },
                { ...candidates[0], x: x - radius - labelWidth - 5, y: y - LABEL_HEIGHT / 2 }
            );
        }
        if (priority <= 0) {
            // A fitted overview can leave only a few pixels between nodes on a phone.
            for (const offset of [28, 54, 80, 106, 132]) {
                candidates.push(
                    { ...candidates[0], y: top + offset, displaced: true },
                    { ...candidates[0], y: y - radius - LABEL_HEIGHT - 4 - offset, displaced: true }
                );
            }
        }
        const box = candidates.find(rect => rect.x >= 3 && rect.x + rect.width <= width - 3
            && rect.y >= 3 && rect.y + rect.height <= height - 3
            && !occupied.some(other => overlaps(rect, other)));
        if (!box) continue;
        occupied.push({ x: box.x - 3, y: box.y - 2, width: box.width + 6, height: LABEL_HEIGHT + 4 });
        const below = box.y > y;
        const leader = box.displaced ? {
            left: x,
            top: below ? y + radius : box.y + LABEL_HEIGHT,
            height: below ? box.y - y - radius : y - radius - box.y - LABEL_HEIGHT
        } : null;
        labels.push({ id: item.id, title, box, leader, selected: priority === -2 });
    }
    return labels;
}

export function fitGraph(cy) {
    const nodes = cy.nodes(':visible');
    if (nodes.empty()) return;
    cy.fit(nodes, 45);
    if (cy.zoom() > 1) cy.zoom(1).center(nodes);
}

export function zoomGraph(cy, factor) {
    cy.zoom({
        level: cy.zoom() * factor,
        renderedPosition: { x: cy.width() / 2, y: cy.height() / 2 }
    });
}

export default function GraphCanvas({ model, graphKey, selectedId, showAll, locate, onSelect, cyRef }) {
    const containerRef = useRef(null);
    const chartRef = useRef(null);
    const viewRef = useRef(null);
    const controlsRef = useRef({ selectedId, showAll, locate, onSelect });
    const [labels, setLabels] = useState([]);

    useEffect(() => {
        controlsRef.current = { selectedId, showAll, locate, onSelect };
        chartRef.current?.syncSelection();
    }, [selectedId, showAll, locate, onSelect]);

    useEffect(() => {
        const container = containerRef.current;
        if (!model.nodes.size) {
            setLabels([]);
            return undefined;
        }
        let disposed = false;
        let cy;
        let frame;
        let resize;
        let hovered;
        let centeredRequest;
        let previousSelection;
        let previousShowAll = controlsRef.current.showAll;
        setLabels([]);

        import('cytoscape').then(({ default: cytoscape }) => {
            if (disposed) return;
            const positions = graphPositions(model);
            const elements = [
                ...[...model.nodes.values()].map(node => ({
                    data: {
                        id: node.id, kind: node.kind, boundary: Number(Boolean(node.boundary)),
                        color: GRAPH_KIND_COLOR[node.kind] || '#86909c'
                    },
                    position: positions.get(node.id)
                })),
                ...model.edges.map(edge => {
                    const source = positions.get(edge.source);
                    const target = positions.get(edge.target);
                    const spansColumns = target.x - source.x > GRAPH_COLUMN_GAP * 1.5;
                    return {
                        data: {
                            ...edge, projection: edge.projection || '',
                            bend: target.y < source.y ? -28 : 28
                        },
                        classes: model.layoutEdges.has(edge.id) ? (spansColumns ? 'long-flow' : 'flow') : ''
                    };
                })
            ];
            cy = cytoscape({
                container, elements, style: STYLE,
                layout: { name: 'preset', fit: false },
                wheelSensitivity: 0.2, minZoom: 0.005, maxZoom: 3,
                boxSelectionEnabled: false,
                autounselectify: true
            });
            cyRef.current = cy;
            const measurement = document.createElement('canvas').getContext('2d');
            measurement.font = `12px ${getComputedStyle(container).fontFamily}`;
            const widths = new Map();
            const measure = title => {
                if (!widths.has(title)) widths.set(title, measurement.measureText(title).width);
                return widths.get(title);
            };
            const selectedNode = () => cy.$id(controlsRef.current.selectedId)[0];
            const updateLabels = () => {
                cancelAnimationFrame(frame);
                frame = requestAnimationFrame(() => setLabels(visibleLabels(cy, model, selectedNode(), hovered, measure)));
            };
            const syncSelection = () => {
                const { showAll, locate } = controlsRef.current;
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
                } else if (!selected && (showAll !== previousShowAll || (overview && previousSelection?.hidden()))) {
                    fitGraph(cy);
                }
                previousSelection = selected;
                previousShowAll = showAll;
                updateLabels();
            };
            chartRef.current = { syncSelection };
            const saved = viewRef.current;
            const restore = saved?.key === graphKey;
            if (restore) {
                cy.viewport(saved);
                centeredRequest = controlsRef.current.locate;
            }
            syncSelection();
            if (!restore && !centeredRequest) fitGraph(cy);
            cy.on('pan zoom position', updateLabels);
            cy.on('tap', event => {
                if (event.target === cy) controlsRef.current.onSelect('');
                else if (event.target.isNode()) controlsRef.current.onSelect(event.target.id());
            });
            cy.on('mouseover', 'node', event => { hovered = event.target; updateLabels(); });
            cy.on('mouseout', 'node', () => { hovered = undefined; updateLabels(); });

            let width = cy.width();
            let height = cy.height();
            resize = new ResizeObserver(() => {
                const previousWidth = width;
                const previousHeight = height;
                cy.resize();
                const nextWidth = cy.width();
                const nextHeight = cy.height();
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
                updateLabels();
            });
            resize.observe(container);
        }).catch(error => { if (!disposed) toastError(error, '图谱渲染失败'); });

        return () => {
            disposed = true;
            cancelAnimationFrame(frame);
            resize?.disconnect();
            if (cy) {
                viewRef.current = { key: graphKey, zoom: cy.zoom(), pan: cy.pan() };
                cy.destroy();
            }
            chartRef.current = null;
            cyRef.current = null;
        };
    }, [model, graphKey, cyRef]);

    return (
        <div className="assessment-graph-viewport" role="group" aria-label="探索图">
            <div ref={containerRef} className="assessment-graph-canvas" />
            <div className="assessment-graph-labels">
                {labels.map(({ id, title, box, leader, selected }) => (
                    <Fragment key={id}>
                        {leader ? <span aria-hidden="true" className="assessment-graph-label-leader" style={leader} /> : null}
                        <button
                            type="button"
                            className="assessment-graph-label"
                            aria-pressed={selected}
                            style={{ left: box.x, top: box.y, width: box.width }}
                            title={title}
                            dir="auto"
                            onClick={() => onSelect(id)}
                        >
                            {title}
                        </button>
                    </Fragment>
                ))}
            </div>
        </div>
    );
}
