const RELATION_STYLES = [
    { label: '规划与依赖', color: '#548df0', relations: ['plans', 'depends_on'] },
    { label: '产出与证据', color: '#24aa94', relations: ['produces', 'supports', 'derives', 'reviews'] },
    { label: '线索与引用', color: '#aa78e8', relations: ['guides', 'informs'] },
    { label: '执行与记录', color: '#dc913a', relations: ['executes', 'records', 'summarizes'] },
    { label: '归档与其他', color: '#8b9bb3', relations: [] }
];

const isDetail = node => node.boundary || ['receipt', 'observation', 'memory'].includes(node.kind);

function edgePriority(edge, nodes) {
    const source = nodes.get(edge.source);
    const target = nodes.get(edge.target);
    if (edge.projection) return 4;
    if (source.kind === 'step' && source.step_id && source.step_id === target.step_id) return 0;
    if (['plans', 'depends_on', 'contains', 'archives'].includes(edge.relation)) return 1;
    if (['informs', 'supports', 'derives'].includes(edge.relation)) return 3;
    return 2;
}

function hasUpstream(upstream, start, target) {
    const pending = [start];
    const seen = new Set();
    while (pending.length) {
        const id = pending.pop();
        if (id === target) return true;
        if (seen.has(id)) continue;
        seen.add(id);
        pending.push(...upstream.get(id));
    }
    return false;
}

/** Pick real parent edges for positioning. Keep feedback and extra relations for inspection. */
export function prepareGraph(graph) {
    const nodes = new Map((graph?.nodes || []).map(node => [node.id, node]));
    const styles = new Set();
    const edges = (graph?.edges || [])
        .filter(edge => nodes.has(edge.source) && nodes.has(edge.target))
        .map((edge, index) => {
            const style = RELATION_STYLES.find(item => item.relations.includes(edge.relation)) || RELATION_STYLES.at(-1);
            styles.add(style);
            return { ...edge, id: `edge-${index}`, color: style.color };
        });
    const parents = new Map();
    const ordered = edges.map(edge => ({ edge, priority: edgePriority(edge, nodes) }));
    ordered.sort((a, b) => a.priority - b.priority
        || Number(Boolean(nodes.get(a.edge.source).boundary)) - Number(Boolean(nodes.get(b.edge.source).boundary))
        || a.edge.source.localeCompare(b.edge.source)
        || a.edge.target.localeCompare(b.edge.target)
        || (a.edge.relation || '').localeCompare(b.edge.relation || ''));
    const layoutEdges = new Set();
    const backbone = new Set();
    for (const { edge } of ordered) {
        const target = nodes.get(edge.target);
        if (target.kind === 'goal' || parents.has(edge.target)) continue;
        let ancestor = edge.source;
        while (ancestor && ancestor !== edge.target) ancestor = parents.get(ancestor);
        if (ancestor === edge.target) continue;
        parents.set(edge.target, edge.source);
        layoutEdges.add(edge.id);
        if (!isDetail(target)) backbone.add(edge.id);
    }
    const upstream = new Map([...nodes.keys()].map(id => [id, new Set()]));
    for (const [target, source] of parents) upstream.get(target).add(source);
    for (const { edge } of ordered) {
        if (edge.relation !== 'depends_on' || edge.projection || layoutEdges.has(edge.id)) continue;
        if (nodes.get(edge.target).kind === 'goal' || hasUpstream(upstream, edge.source, edge.target)) continue;
        upstream.get(edge.target).add(edge.source);
        layoutEdges.add(edge.id);
        if (!isDetail(nodes.get(edge.target))) backbone.add(edge.id);
    }

    const overview = new Set([...nodes.values()].filter(node => !isDetail(node)).map(node => node.id));
    for (const edge of edges) {
        if (backbone.has(edge.id)) {
            overview.add(edge.source);
            overview.add(edge.target);
        }
    }
    return {
        nodes,
        edges,
        upstream,
        layoutEdges,
        backbone,
        overview,
        legend: RELATION_STYLES.filter(style => styles.has(style)),
        dense: nodes.size > 20 || edges.length > nodes.size * 1.5
    };
}

export const GRAPH_COLUMN_GAP = 168;
const GRAPH_LEVEL_GAP = 36;

export function graphPositions({ nodes, upstream }) {
    const depths = new Map();
    const levels = [];
    const hasGoal = [...nodes.values()].some(node => node.kind === 'goal');
    const depthOf = id => {
        if (!depths.has(id)) {
            const parents = upstream.get(id);
            const depth = parents.size
                ? Math.max(...[...parents].map(depthOf)) + 1
                : Number(hasGoal && nodes.get(id).kind !== 'goal');
            depths.set(id, depth);
        }
        return depths.get(id);
    };
    for (const id of nodes.keys()) {
        const depth = depthOf(id);
        (levels[depth] ??= []).push(id);
    }

    const positions = new Map();
    const rowLimit = Math.min(17, Math.max(3, Math.ceil(Math.sqrt(nodes.size * 1.6))));
    let x = 0;
    for (const level of levels) {
        // Ordering by the mean of incoming positions keeps connected branches together.
        const weighted = level.map(id => {
            const linked = upstream.get(id);
            const center = linked.size
                ? [...linked].reduce((sum, parent) => sum + positions.get(parent).y, 0) / linked.size
                : 0;
            return { id, center };
        });
        weighted.sort((a, b) => a.center - b.center);
        const ordered = weighted.map(node => node.id);
        // Keep sibling order. Put dense execution records to the right of the decisions
        // they accompany so the main paths stay together.
        const groups = ordered.length > rowLimit
            ? [ordered.filter(id => !isDetail(nodes.get(id))), ordered.filter(id => isDetail(nodes.get(id)))]
            : [ordered];
        let columnIndex = 0;
        for (const group of groups) {
            if (!group.length) continue;
            const columns = Math.ceil(group.length / rowLimit);
            const columnSize = Math.ceil(group.length / columns);
            for (let start = 0; start < group.length; start += columnSize) {
                const column = group.slice(start, start + columnSize);
                // Offset wrapped columns to keep long connectors from running through other markers.
                const stagger = [0, 23, -23, 35, -35][columnIndex % 5];
                column.forEach((id, index) => positions.set(id, {
                    x,
                    y: (index - (column.length - 1) / 2) * 76 + stagger
                }));
                x += GRAPH_COLUMN_GAP;
                columnIndex += 1;
            }
        }
        x += GRAPH_LEVEL_GAP;
    }
    return positions;
}
