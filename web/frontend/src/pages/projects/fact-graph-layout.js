import { graphParents } from '../../components/graph-parents.js';

const CATEGORY_ORDER = { target: 0, infra: 1, auth: 1, finding: 2, chain: 3, exploit: 3, poc: 3, vulnerability: 4 };

export function projectGraphLayout(nodes, edges, selectedId, cardWidth, cardHeight) {
    const categoryOrder = node => CATEGORY_ORDER[node.type] ?? 5;
    const byId = new Map(nodes.map(node => [node.id, node]));
    const connected = new Set(edges.flatMap(edge => [edge.source, edge.target]));
    const sorted = [...nodes].sort((a, b) => Number(connected.has(b.id)) - Number(connected.has(a.id))
        || categoryOrder(a) - categoryOrder(b) || a.id.localeCompare(b.id));
    let levels;
    let parentEdges;

    if (selectedId) {
        const outgoing = new Set(edges.filter(edge => edge.source === selectedId).map(edge => edge.target));
        levels = [
            sorted.filter(node => node.id !== selectedId && !outgoing.has(node.id)),
            [byId.get(selectedId)],
            sorted.filter(node => node.id !== selectedId && outgoing.has(node.id))
        ];
        parentEdges = new Set(edges);
    } else {
        const priority = edge => {
            const source = categoryOrder(byId.get(edge.source));
            const target = categoryOrder(byId.get(edge.target));
            return 4 * Number(source > target) + 2 * Number(source !== 0) + Number(edge.confidence === 'tentative');
        };
        const ordered = [...edges].sort((a, b) => priority(a) - priority(b)
            || a.source.localeCompare(b.source) || a.target.localeCompare(b.target)
            || (a.type || '').localeCompare(b.type || '') || (a.id || '').localeCompare(b.id || ''));
        const layout = graphParents(ordered, id => byId.get(id).type === 'target');
        parentEdges = layout.parentEdges;
        const children = new Map(nodes.map(node => [node.id, []]));
        for (const node of sorted) {
            const parent = layout.parents.get(node.id);
            if (parent) children.get(parent).push(node);
        }
        // Parents determine depth and sibling order; the remaining edges keep their stored directions.
        levels = [sorted.filter(node => !layout.parents.has(node.id))];
        for (const level of levels) {
            const next = level.flatMap(node => children.get(node.id));
            if (next.length) levels.push(next);
        }
    }

    const positions = new Map();
    const maxRows = Math.max(5, Math.min(23, Math.ceil(Math.sqrt(nodes.length) * 1.18)));
    let x = 0;
    for (const level of levels) {
        if (!level.length) continue;
        const columnSize = Math.ceil(level.length / Math.ceil(level.length / maxRows));
        for (let start = 0; start < level.length; start += columnSize) {
            const column = level.slice(start, start + columnSize);
            column.forEach((node, index) => positions.set(node.id, {
                x, y: (index - (column.length - 1) / 2) * (cardHeight + 23)
            }));
            x += cardWidth + 62;
        }
        x += 22;
    }
    return { positions, parentEdges };
}
