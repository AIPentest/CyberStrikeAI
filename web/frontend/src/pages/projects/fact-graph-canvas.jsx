import { useEffect, useMemo, useRef, useState } from 'react';
import { bindGraphCardWheel, CARD_HEIGHT, CARD_WIDTH, GraphNodeCard, visibleGraphCards, zoomGraph } from '../../components/graph-card';
import { NODE_COLORS } from './helpers';
import { projectGraphLayout } from './fact-graph-layout';

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

export default function FactGraphCanvas({ nodes, edges, selectedId, onSelect, chartRef, label, errorText, typeLabel }) {
    const canvas = useRef(null);
    const nodeLayer = useRef(null);
    const [cards, setCards] = useState([]);
    const [failed, setFailed] = useState(false);
    const nodeMap = useMemo(() => new Map(nodes.map(node => [node.id, node])), [nodes]);

    useEffect(() => {
        const container = canvas.current;
        const layer = nodeLayer.current;
        let disposed = false;
        let cy;
        let resize;
        let frame;
        let unbindWheel;
        let fitted = true;
        let fitting = false;
        setCards([]);
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
            const fit = () => {
                fitting = true;
                fitted = true;
                cy.fit(cy.elements(), Math.min(56, cy.width() / 8));
                if (cy.zoom() > 1) cy.zoom(1).center();
                fitting = false;
                place();
            };
            chartRef.current = { fit, zoom: factor => zoomGraph(cy, factor) };
            unbindWheel = bindGraphCardWheel(layer, cy);
            cy.on('viewport', () => {
                if (!fitting) fitted = false;
                place();
            });
            cy.on('tap', 'node', event => onSelect(event.target.id()));

            let width = cy.width();
            let height = cy.height();
            resize = new ResizeObserver(() => {
                cy.resize();
                const nextWidth = cy.width();
                const nextHeight = cy.height();
                if (width === nextWidth && height === nextHeight) return;
                if (fitted) fit();
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
    }, [nodes, edges, selectedId, onSelect, chartRef]);

    return (
        <div className="project-graph-canvas-wrap" role="group" aria-label={label}>
            <div ref={canvas} className="project-graph-canvas" />
            <div ref={nodeLayer} className="graph-node-layer">
                {cards.map(card => {
                    const node = nodeMap.get(card.id);
                    if (!node) return null;
                    const key = node.fact_key || node.id;
                    const title = (node.summary || (node.label !== '—' && node.label) || key).replace(/\s+/g, ' ').trim() || key;
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
                        />
                    );
                })}
            </div>
            {failed && <div className="project-graph-overlay" role="alert">{errorText}</div>}
        </div>
    );
}
