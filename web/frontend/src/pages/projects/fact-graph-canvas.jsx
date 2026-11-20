import { useEffect, useRef, useState } from 'react';
import { NODE_COLORS } from './helpers';

const STAGE = { target: 0, infra: 1, auth: 1, finding: 2, chain: 3, exploit: 3, poc: 3, vulnerability: 4 };
const LABEL_WIDTH = 144;
const LABEL_HEIGHT = 23;
const overlaps = (a, b) => a.x < b.x + b.width && b.x < a.x + a.width
    && a.y < b.y + b.height && b.y < a.y + a.height;

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
                x, y: (index - (column.length - 1) / 2) * 63
            }));
            x += 172;
        }
        x += 34;
    }
    return positions;
}

const STYLE = [
    {
        selector: 'node',
        style: { 'background-color': 'data(color)', width: 21, height: 21 }
    },
    { selector: 'node[type = "target"]', style: { width: 25, height: 25 } },
    {
        selector: 'node[type = "missing"]',
        style: { 'background-opacity': 0.15, 'border-width': 2, 'border-color': 'data(color)', 'border-style': 'dashed' }
    },
    { selector: 'node.current', style: { width: 30, height: 30, 'underlay-color': 'data(color)', 'underlay-opacity': 0.22, 'underlay-padding': 6 } },
    {
        selector: 'edge',
        style: {
            width: 1.75, 'line-color': '#8191a9', 'target-arrow-color': '#8191a9',
            'target-arrow-shape': 'triangle', 'arrow-scale': 0.9,
            'curve-style': 'bezier', opacity: 0.55
        }
    },
    { selector: 'edge.focused', style: { width: 2, opacity: 0.92 } },
    { selector: 'edge[type = "links_vuln"]', style: { 'line-color': '#d45363', 'target-arrow-color': '#d45363' } },
    { selector: 'edge[confidence = "tentative"]', style: { 'line-style': 'dashed', 'line-dash-pattern': [5, 4] } }
];

function placeLabels(cy, nodes, selectedId, hoveredId, measure) {
    const width = cy.width();
    const height = cy.height();
    const scale = cy.zoom();
    const points = cy.nodes().map(element => {
        const { x, y } = element.renderedPosition();
        return { id: element.id(), x, y, radius: Math.max(3, element.renderedOuterWidth() / 2) };
    }).filter(({ x, y }) => x >= 0 && x <= width && y >= 0 && y <= height);
    const occupied = points.map(({ x, y, radius }) => ({
        x: x - radius - 1, y: y - radius - 1, width: radius * 2 + 2, height: radius * 2 + 2
    }));
    const priority = ({ id }) => {
        if (id === selectedId) return 0;
        if (id === hoveredId) return 1;
        const type = nodes.get(id).type;
        if (type === 'target') return 2;
        if (type === 'chain') return 3;
        return 4;
    };
    const labels = [];
    for (const point of points.sort((a, b) => priority(a) - priority(b))) {
        const { id, x, y, radius } = point;
        if (scale < 0.46 && priority(point) > 3) continue;
        const node = nodes.get(id);
        const title = ((node.label !== '—' && node.label) || node.fact_key || id).replace(/\s+/g, ' ').trim() || id;
        const maxWidth = scale < 0.55 && hoveredId !== id && selectedId !== id ? 96 : LABEL_WIDTH;
        const boxWidth = Math.min(maxWidth, Math.ceil(measure(title)) + 21);
        const center = Math.max(4, Math.min(x - boxWidth / 2, width - boxWidth - 4));
        const candidates = [
            { x: center, y: y + radius + 4 },
            { x: center, y: y - radius - LABEL_HEIGHT - 4 },
            { x: x + radius + 5, y: y - LABEL_HEIGHT / 2 },
            { x: x - radius - boxWidth - 5, y: y - LABEL_HEIGHT / 2 }
        ];
        if (!selectedId) candidates.push(...candidates.splice(0, 2));
        const box = candidates.map(candidate => ({ ...candidate, width: boxWidth, height: LABEL_HEIGHT }))
            .find(rect => rect.x >= 3 && rect.x + rect.width <= width - 3
                && rect.y >= 3 && rect.y + rect.height <= height - 3
                && !occupied.some(other => overlaps(rect, other)));
        if (!box) continue;
        occupied.push({ x: box.x - 3, y: box.y - 2, width: box.width + 6, height: box.height + 4 });
        labels.push({ id, title, box });
    }
    return labels;
}

export default function FactGraphCanvas({ nodes, edges, selectedId, onSelect, chartRef, label, errorText }) {
    const canvas = useRef(null);
    const [labels, setLabels] = useState([]);
    const [failed, setFailed] = useState(false);

    useEffect(() => {
        const container = canvas.current;
        let disposed = false;
        let cy;
        let resize;
        let frame;
        let hoveredId;
        let fitted = true;
        let fitting = false;
        setLabels([]);
        setFailed(false);

        import('cytoscape').then(({ default: cytoscape }) => {
            if (disposed) return;
            const byId = new Map(nodes.map(node => [node.id, node]));
            const positions = graphPositions(nodes, edges, selectedId);
            cy = cytoscape({
                container,
                elements: [
                    ...nodes.map(node => ({
                        group: 'nodes',
                        data: { id: node.id, type: node.type || 'note', color: NODE_COLORS[node.type] || NODE_COLORS.missing },
                        position: positions.get(node.id),
                        classes: node.id === selectedId ? 'current' : ''
                    })),
                    ...edges.map((edge, index) => ({
                        group: 'edges',
                        classes: selectedId ? 'focused' : '',
                        data: { ...edge, id: `edge|${index}|${edge.id || ''}` }
                    }))
                ],
                style: STYLE,
                layout: { name: 'preset', fit: false },
                minZoom: 0.006, maxZoom: 3, wheelSensitivity: 0.2,
                boxSelectionEnabled: false, autounselectify: true, autoungrabify: true
            });
            const measure = document.createElement('canvas').getContext('2d');
            measure.font = `500 12px ${getComputedStyle(container).fontFamily}`;
            const place = () => {
                cancelAnimationFrame(frame);
                frame = requestAnimationFrame(() => setLabels(
                    placeLabels(cy, byId, selectedId, hoveredId, title => measure.measureText(title).width)
                ));
            };
            const fit = () => {
                fitting = true;
                fitted = true;
                cy.fit(cy.elements(), Math.min(56, cy.width() / 8));
                if (cy.zoom() > 1) cy.zoom(1).center();
                fitting = false;
                place();
            };
            chartRef.current = {
                fit,
                zoom: factor => cy.zoom({
                    level: cy.zoom() * factor,
                    renderedPosition: { x: cy.width() / 2, y: cy.height() / 2 }
                })
            };
            cy.on('viewport', () => {
                if (!fitting) fitted = false;
                place();
            });
            cy.on('tap', 'node', event => onSelect(event.target.id()));
            cy.on('mouseover', 'node', event => { hoveredId = event.target.id(); place(); });
            cy.on('mouseout', 'node', () => { hoveredId = undefined; place(); });
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
            cy?.destroy();
            chartRef.current = null;
        };
    }, [nodes, edges, selectedId, onSelect, chartRef]);

    return (
        <div className="project-graph-canvas-wrap" role="group" aria-label={label}>
            <div ref={canvas} className="project-graph-canvas" />
            <div className="project-graph-labels">
                {labels.map(({ id, title, box }) => (
                    <button
                        type="button"
                        className="project-graph-label"
                        key={id}
                        title={title}
                        aria-pressed={id === selectedId}
                        style={{ left: box.x, top: box.y, width: box.width }}
                        onClick={() => onSelect(id)}
                        dir="auto"
                    >
                        {title}
                    </button>
                ))}
            </div>
            {failed && <div className="project-graph-overlay" role="alert">{errorText}</div>}
        </div>
    );
}
