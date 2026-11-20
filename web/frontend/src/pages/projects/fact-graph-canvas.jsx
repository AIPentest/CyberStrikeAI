import { useEffect, useRef, useState } from 'react';
import {
    IconAlertTriangle, IconBolt, IconBranch, IconFile, IconGlobe,
    IconHelpCircle, IconKey, IconSearch, IconServer, IconTickCircle
} from '@douyinfe/semi-icons';
import { NODE_COLORS } from './helpers';

const STAGE = { target: 0, infra: 1, auth: 1, finding: 2, chain: 3, exploit: 3, poc: 3, vulnerability: 4 };
const CARD_WIDTH = 216;
const CARD_HEIGHT = 98;
const COLUMN_GAP = 64;
const ROW_GAP = 16;

const NODE_ICONS = {
    target: IconGlobe, infra: IconServer, auth: IconKey, finding: IconSearch, chain: IconBranch,
    exploit: IconBolt, poc: IconTickCircle, vulnerability: IconAlertTriangle, note: IconFile, missing: IconHelpCircle
};

export function GraphNodeIcon({ type }) {
    const Icon = NODE_ICONS[type] || IconFile;
    return <span className="project-graph-node-icon" aria-hidden="true"><Icon size="small" /></span>;
}

function graphPositions(nodes, edges, selectedId) {
    const adjacent = new Map(nodes.map(node => [node.id, new Set()]));
    const outgoing = new Set();
    for (const edge of edges) {
        adjacent.get(edge.source).add(edge.target);
        adjacent.get(edge.target).add(edge.source);
        if (edge.source === selectedId) outgoing.add(edge.target);
    }

    const stageOf = node => STAGE[node.type] ?? 5;
    const stage = node => {
        if (!selectedId) return stageOf(node);
        if (node.id === selectedId) return 1;
        return outgoing.has(node.id) ? 2 : 0;
    };
    const sorted = [...nodes].sort((a, b) => stageOf(a) - stageOf(b) || a.id.localeCompare(b.id));
    const byId = new Map(sorted.map(node => [node.id, node]));
    const groups = new Map();
    const visited = new Set();
    // Keep related branches in the same order across the stage columns.
    for (const node of sorted) {
        if (visited.has(node.id)) continue;
        const queue = [node.id];
        visited.add(node.id);
        for (const id of queue) {
            const key = stage(byId.get(id));
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key).push(id);
            for (const neighbor of [...adjacent.get(id)].sort()) {
                if (visited.has(neighbor)) continue;
                visited.add(neighbor);
                queue.push(neighbor);
            }
        }
    }

    const positions = new Map();
    const rows = Math.max(5, Math.min(32, Math.ceil(Math.sqrt(nodes.length) * 1.25)));
    let x = 0;
    for (const key of [...groups.keys()].sort((a, b) => a - b)) {
        const group = groups.get(key);
        const columnSize = Math.ceil(group.length / Math.ceil(group.length / rows));
        for (let start = 0; start < group.length; start += columnSize) {
            const column = group.slice(start, start + columnSize);
            column.forEach((id, index) => positions.set(id, {
                x, y: (index - (column.length - 1) / 2) * (CARD_HEIGHT + ROW_GAP)
            }));
            x += CARD_WIDTH + COLUMN_GAP;
        }
        x += 24;
    }
    return positions;
}

const STYLE = [
    {
        selector: 'node',
        style: {
            shape: 'round-rectangle', width: CARD_WIDTH, height: CARD_HEIGHT,
            'background-color': 'data(color)', 'background-opacity': 0.44
        }
    },
    { selector: 'node.current', style: { 'background-opacity': 0.85 } },
    {
        selector: 'edge',
        style: {
            width: 1.9, 'line-color': '#748fbb', 'target-arrow-color': '#748fbb',
            'target-arrow-shape': 'triangle', 'arrow-scale': 0.85,
            'curve-style': 'bezier', opacity: 0.59
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
    { selector: 'edge.focused', style: { width: 2.3, opacity: 0.96 } },
    { selector: 'edge[type = "links_vuln"]', style: { 'line-color': '#c96b81', 'target-arrow-color': '#c96b81' } },
    { selector: 'edge[confidence = "tentative"]', style: { 'line-style': 'dashed', 'line-dash-pattern': [5, 4] } }
];

function graphElements(nodes, edges, selectedId) {
    const positions = graphPositions(nodes, edges, selectedId);
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
                data: { id: node.id, color: NODE_COLORS[node.type] || NODE_COLORS.missing },
                position: positions.get(node.id),
                classes: node.id === selectedId ? 'current' : ''
            })),
            ...links
        ]
    };
}

function visibleCards(cy, byId, ports) {
    const width = cy.width();
    const height = cy.height();
    const cardWidth = CARD_WIDTH * cy.zoom();
    const cardHeight = CARD_HEIGHT * cy.zoom();
    // Below this scale Cytoscape's tiles preserve the overall shape of the graph.
    if (cardWidth < 49) return [];
    const density = cardHeight >= 68 ? 'detail' : cardHeight >= 36 ? 'standard' : 'compact';
    return cy.nodes().map(element => {
        const { x, y } = element.renderedPosition();
        const id = element.id();
        return {
            node: byId.get(id), ports: ports.get(id), density,
            box: { left: x - cardWidth / 2, top: y - cardHeight / 2, width: cardWidth, height: cardHeight }
        };
    }).filter(({ box }) => box.left + box.width > 0 && box.left < width
        && box.top + box.height > 0 && box.top < height);
}

function GraphNodeCard({ card, selected, typeLabel, onSelect }) {
    const { node, ports, density, box } = card;
    const detail = density === 'detail';
    const key = node.fact_key || node.id;
    const title = (node.summary || (node.label !== '—' && node.label) || key).replace(/\s+/g, ' ').trim() || key;
    const category = typeLabel(node.type || 'note');
    const textScale = Math.min(Math.max(box.width / CARD_WIDTH, 1), 1.6);
    return (
        <button
            type="button"
            className={`project-graph-node project-graph-node--${density}`}
            title={title}
            aria-label={title}
            aria-pressed={selected}
            style={{
                ...box,
                '--graph-node-color': NODE_COLORS[node.type] || NODE_COLORS.missing,
                '--graph-node-text-scale': textScale
            }}
            onClick={() => onSelect(node.id)}
        >
            <span className="project-graph-node-content">
                {density !== 'compact' && (
                    <span className="project-graph-node-kind">
                        <GraphNodeIcon type={node.type} />
                        <span>{category}</span>
                    </span>
                )}
                <span className="project-graph-node-title" dir="auto">{title}</span>
                {detail && box.height >= 84 && title !== key && <span className="project-graph-node-keyline" dir="auto">{key}</span>}
            </span>
            {detail && [...ports].map(side => (
                <span key={side} className={`project-graph-node-port is-${side}`} aria-hidden="true" />
            ))}
        </button>
    );
}

export default function FactGraphCanvas({ nodes, edges, selectedId, onSelect, chartRef, label, errorText, typeLabel }) {
    const canvas = useRef(null);
    const nodeLayer = useRef(null);
    const [cards, setCards] = useState([]);
    const [failed, setFailed] = useState(false);

    useEffect(() => {
        const container = canvas.current;
        const layer = nodeLayer.current;
        let disposed = false;
        let cy;
        let resize;
        let frame;
        let onCardWheel;
        let fitted = true;
        let fitting = false;
        setCards([]);
        setFailed(false);

        import('cytoscape').then(({ default: cytoscape }) => {
            if (disposed) return;
            const byId = new Map(nodes.map(node => [node.id, node]));
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
                frame = requestAnimationFrame(() => setCards(visibleCards(cy, byId, ports)));
            };
            const fit = () => {
                fitting = true;
                fitted = true;
                cy.fit(cy.elements(), Math.min(56, cy.width() / 8));
                if (cy.zoom() > 1) cy.zoom(1).center();
                fitting = false;
                place();
            };
            const zoom = (factor, renderedPosition = { x: cy.width() / 2, y: cy.height() / 2 }) => {
                cy.zoom({ level: cy.zoom() * factor, renderedPosition });
            };
            chartRef.current = { fit, zoom };
            // Card buttons sit above the canvas, so mirror its wheel zoom at the pointer.
            onCardWheel = event => {
                if (!event.deltaY) return;
                event.preventDefault();
                const { left, top, height } = container.getBoundingClientRect();
                const units = event.deltaMode === 1 ? 33 : event.deltaMode === 2 ? height : 1;
                zoom(10 ** (-event.deltaY * units / 1250), { x: event.clientX - left, y: event.clientY - top });
            };
            layer.addEventListener('wheel', onCardWheel, { passive: false });
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
            if (onCardWheel) layer.removeEventListener('wheel', onCardWheel);
            cy?.destroy();
            chartRef.current = null;
        };
    }, [nodes, edges, selectedId, onSelect, chartRef]);

    return (
        <div className="project-graph-canvas-wrap" role="group" aria-label={label}>
            <div ref={canvas} className="project-graph-canvas" />
            <div ref={nodeLayer} className="project-graph-nodes">
                {cards.map(card => (
                    <GraphNodeCard
                        card={card}
                        key={card.node.id}
                        selected={card.node.id === selectedId}
                        typeLabel={typeLabel}
                        onSelect={onSelect}
                    />
                ))}
            </div>
            {failed && <div className="project-graph-overlay" role="alert">{errorText}</div>}
        </div>
    );
}
