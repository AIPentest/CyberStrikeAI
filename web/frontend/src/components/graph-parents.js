// Pick a layout parent for each node. Edges outside this forest remain in the graph.
export function graphParents(orderedEdges, isRoot) {
    const parents = new Map();
    const parentEdges = new Set();
    for (const edge of orderedEdges) {
        if (isRoot(edge.target) || parents.has(edge.target)) continue;
        let ancestor = edge.source;
        while (ancestor && ancestor !== edge.target) ancestor = parents.get(ancestor);
        if (ancestor === edge.target) continue;
        parents.set(edge.target, edge.source);
        parentEdges.add(edge);
    }
    return { parents, parentEdges };
}
