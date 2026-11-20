import { useEffect, useMemo, useRef, useState } from 'react';
import { bindGraphCardWheel, CARD_HEIGHT, CARD_WIDTH, GraphNodeCard, GraphNodeIcon, visibleGraphCards, zoomGraph } from '../../components/graph-card';
import { NODE_COLORS } from './helpers';
import { projectGraphLayout } from './fact-graph-layout';

const READABLE_ZOOM = 0.78;
const PREVIEW_WIDTH = 340;

function nodeText(node) {
    const key = node.fact_key || node.id;
    const title = (node.summary || (node.label !== '—' && node.label) || key).replace(/\s+/g, ' ').trim() || key;
    return { key, title };
}

function fittedViewport(cy, selectedId, mode) {
    const width = cy.width();
    const height = cy.height();
    const nodes = cy.nodes();
    const elements = cy.elements();
    const viewport = cy.getFitViewport(elements, Math.min(36, width / 10, height / 10));
    if (!viewport) return null;
    if (viewport.zoom > 1) {
        return { zoom: 1, pan: cy.getCenterPan(elements, 1), view: mode === 'overview' ? mode : 'fitted' };
    }
    if (mode === 'overview' || viewport.zoom >= READABLE_ZOOM) {
        return { ...viewport, view: mode === 'overview' ? mode : 'fitted' };
    }

    // On dense graphs start at a readable size and keep the focused node or first branch in view.
    const anchor = selectedId ? cy.$id(selectedId) : nodes.reduce((first, node) => {
        const point = node.position();
        const current = first.position();
        return point.x < current.x || (point.x === current.x && point.y < current.y) ? node : first;
    }, nodes[0]);
    const { x, y } = anchor.position();
    const margin = 24;
    return {
        zoom: READABLE_ZOOM,
        pan: selectedId
            ? { x: width / 2 - x * READABLE_ZOOM, y: height / 2 - y * READABLE_ZOOM }
            : { x: margin - (x - CARD_WIDTH / 2) * READABLE_ZOOM, y: margin - (y - CARD_HEIGHT / 2) * READABLE_ZOOM },
        view: 'reading'
    };
}

function previewPosition(cy, node) {
    const { x, y } = node.renderedPosition();
    const width = Math.min(PREVIEW_WIDTH, cy.width() - 24);
    const left = Math.max(12, Math.min(x - width / 2, cy.width() - width - 12));
    const offset = node.renderedHeight() / 2 + 9;
    const maxInset = Math.max(8, cy.height() - 164);
    const clampInset = value => Math.max(8, Math.min(value, maxInset));
    return {
        width, left,
        ...(y <= cy.height() / 2
            ? { top: clampInset(y + offset) }
            : { bottom: clampInset(cy.height() - y + offset) })
    };
}

const STYLE = [
    {
        selector: 'node',
        style: {
            shape: 'round-rectangle', width: CARD_WIDTH, height: CARD_HEIGHT,
            'background-color': 'data(color)', 'background-opacity': 0.13,
            'border-color': 'data(color)', 'border-opacity': 0.42, 'border-width': 1.2
        }
    },
    { selector: 'node.current', style: { 'background-opacity': 0.85 } },
    {
        selector: 'edge',
        style: {
            width: 2.15, 'line-color': '#8197b6', 'target-arrow-color': '#8197b6',
            'target-arrow-shape': 'triangle', 'arrow-scale': 0.85,
            'curve-style': 'bezier', opacity: 0.73
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
        selector: 'edge.curved',
        style: {
            'curve-style': 'unbundled-bezier', 'edge-distances': 'endpoints',
            'control-point-weights': '0.30 0.70', 'control-point-distances': 'data(bend)'
        }
    },
    { selector: 'edge.cross-link', style: { opacity: 0.28 } },
    { selector: 'edge.focused', style: { width: 2.3, opacity: 0.96 } },
    { selector: 'edge[type = "links_vuln"]', style: { 'line-color': '#c96b81', 'target-arrow-color': '#c96b81' } },
    { selector: 'edge[confidence = "tentative"]', style: { 'line-style': 'dashed', 'line-dash-pattern': [5, 4] } }
];

function graphElements(nodes, edges, selectedId) {
    const { positions, parentEdges } = projectGraphLayout(nodes, edges, selectedId, CARD_WIDTH, CARD_HEIGHT);
    const ports = new Map(nodes.map(node => [node.id, new Set()]));
    // Preserve Cytoscape's normal separation for parallel links.
    const pairKey = edge => JSON.stringify([edge.source, edge.target].sort());
    const pairCounts = new Map();
    for (const edge of edges) {
        const key = pairKey(edge);
        pairCounts.set(key, (pairCounts.get(key) || 0) + 1);
    }
    const links = edges.map((edge, index) => {
        const source = positions.get(edge.source);
        const target = positions.get(edge.target);
        const dx = target.x - source.x;
        const dy = target.y - source.y;
        const classes = [];
        if (selectedId) classes.push('focused');
        else if (!parentEdges.has(edge)) classes.push('cross-link');
        if (dx !== 0) {
            const forward = dx > 0;
            classes.push(forward ? 'forward' : 'backward');
            ports.get(edge.source).add(forward ? 'right' : 'left');
            ports.get(edge.target).add(forward ? 'left' : 'right');
            if (pairCounts.get(pairKey(edge)) === 1) classes.push('curved');
        }
        const bend = Math.sign(dy * dx) * Math.min(Math.abs(dy) * 0.3, 60);
        return {
            group: 'edges', classes: classes.join(' '),
            data: { ...edge, id: `edge|${index}|${edge.id || ''}`, bend: [-bend, bend] }
        };
    });
    return {
        ports,
        elements: [
            ...nodes.map(node => ({
                group: 'nodes',
                data: { id: node.id, type: node.type || 'note', color: NODE_COLORS[node.type] || NODE_COLORS.missing },
                position: positions.get(node.id),
                classes: node.id === selectedId ? 'current' : ''
            })),
            ...links
        ]
    };
}

export default function FactGraphCanvas({ nodes, edges, selectedId, onSelect, chartRef, label, errorText, typeLabel, onFitChange }) {
    const canvas = useRef(null);
    const nodeLayer = useRef(null);
    const [cards, setCards] = useState([]);
    const [preview, setPreview] = useState(null);
    const [failed, setFailed] = useState(false);
    const nodeMap = useMemo(() => new Map(nodes.map(node => [node.id, node])), [nodes]);

    const inspectCard = (id, visible) => chartRef.current?.inspect(id, visible);

    useEffect(() => {
        const container = canvas.current;
        const layer = nodeLayer.current;
        let disposed = false;
        let cy;
        let resize;
        let frame;
        let unbindWheel;
        let fittedMode = 'reading';
        let fitting = false;
        setCards([]);
        setPreview(null);
        setFailed(false);

        import('cytoscape').then(({ default: cytoscape }) => {
            if (disposed) return;
            const { ports, elements } = graphElements(nodes, edges, selectedId);
            cy = cytoscape({
                container,
                elements,
                style: STYLE,
                layout: { name: 'preset', fit: false },
                minZoom: 0.006, maxZoom: 3, wheelSensitivity: 0.2,
                boxSelectionEnabled: false, autounselectify: true, autoungrabify: true
            });
            const place = () => {
                cancelAnimationFrame(frame);
                frame = requestAnimationFrame(() => {
                    const visible = visibleGraphCards(cy, {
                        allowCompact: element => selectedId || nodes.length <= 24
                            || ['target', 'chain'].includes(element.data('type'))
                    });
                    setCards(visible.map(card => ({ ...card, ports: ports.get(card.id) })));
                });
            };
            const fit = (mode = 'reading') => {
                const viewport = fittedViewport(cy, selectedId, mode);
                if (!viewport) return;
                fitting = true;
                fittedMode = mode;
                setPreview(null);
                cy.viewport({ zoom: viewport.zoom, pan: viewport.pan });
                onFitChange(viewport.view);
                fitting = false;
                place();
            };
            const inspect = (id, visible) => {
                const node = cy.$id(id);
                if (visible && node.nonempty()) {
                    setPreview({ id, box: previewPosition(cy, node) });
                } else if (!visible) {
                    setPreview(previous => previous?.id === id ? null : previous);
                }
            };
            chartRef.current = {
                fit: () => fit(),
                overview: () => fit('overview'),
                zoom: factor => zoomGraph(cy, factor),
                inspect
            };
            unbindWheel = bindGraphCardWheel(layer, cy);
            cy.on('viewport', () => {
                if (!fitting) fittedMode = null;
                setPreview(null);
                place();
            });
            cy.on('zoom', () => { if (!fitting) onFitChange('manual'); });
            cy.on('mouseover', 'node', event => inspect(event.target.id(), true));
            cy.on('mouseout', 'node', event => inspect(event.target.id(), false));
            cy.on('tap', 'node', event => onSelect(event.target.id()));

            let width = cy.width();
            let height = cy.height();
            resize = new ResizeObserver(() => {
                cy.resize();
                const nextWidth = cy.width();
                const nextHeight = cy.height();
                if (width === nextWidth && height === nextHeight) return;
                if (fittedMode) fit(fittedMode);
                else cy.panBy({ x: (nextWidth - width) / 2, y: (nextHeight - height) / 2 });
                width = nextWidth;
                height = nextHeight;
            });
            resize.observe(container);
            fit();
        }).catch(() => { if (!disposed) setFailed(true); });

        return () => {
            disposed = true;
            cancelAnimationFrame(frame);
            resize?.disconnect();
            unbindWheel?.();
            cy?.destroy();
            chartRef.current = null;
        };
    }, [nodes, edges, selectedId, onSelect, chartRef, onFitChange]);

    const previewNode = preview && nodeMap.get(preview.id);
    const previewText = previewNode && nodeText(previewNode);
    return (
        <div className="project-graph-canvas-wrap" role="group" aria-label={label}>
            <div ref={canvas} className="project-graph-canvas" />
            <div ref={nodeLayer} className="graph-node-layer">
                {cards.map(card => {
                    const node = nodeMap.get(card.id);
                    if (!node) return null;
                    const { key, title } = nodeText(node);
                    return (
                        <GraphNodeCard
                            card={card}
                            key={node.id}
                            id={node.id}
                            title={title}
                            type={node.type}
                            category={typeLabel(node.type || 'note')}
                            color={NODE_COLORS[node.type] || NODE_COLORS.missing}
                            factKey={key}
                            ports={card.ports}
                            selected={node.id === selectedId}
                            onSelect={onSelect}
                            onPreview={inspectCard}
                        />
                    );
                })}
            </div>
            {previewNode && (
                <div
                    className="project-graph-node-preview"
                    role="tooltip"
                    style={{ ...preview.box, '--graph-node-color': NODE_COLORS[previewNode.type] || NODE_COLORS.missing }}
                >
                    <span className="project-graph-preview-type">
                        <GraphNodeIcon type={previewNode.type} />
                        {typeLabel(previewNode.type || 'note')}
                    </span>
                    <strong dir="auto">{previewText.title}</strong>
                    <code dir="auto">{previewText.key}</code>
                </div>
            )}
            {failed && <div className="project-graph-overlay" role="alert">{errorText}</div>}
        </div>
    );
}
