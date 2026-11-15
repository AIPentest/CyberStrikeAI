/* Read-only graph renderer; task state and evidence remain owned by the backend. */
(function () {
    'use strict';
    let chart = null, host = null, key = '', onSelect = null, observer = null, theme = '';
    const colors = { fact:'#0d9488',finding:'#e11d48',domain:'#6366f1',subdomain:'#818cf8',ip:'#0891b2',service:'#2563eb',application:'#c026d3',endpoint:'#0d9488', goal: '#6366f1', step: '#3b82f6', observation: '#14b8a6', receipt: '#64748b', memory: '#a855f7', digest: '#d97706', guidance: '#0891b2' };
    function destroy() {
        observer?.disconnect(); observer = null;
        chart?.destroy(); chart = null; host = null; key = ''; theme = '';
    }
    function relationLabel(key) {
        const path='deepAssessments.graphRelations.'+key,value=window.t?.(path);
        return value && value!==path ? value : ({plans:'规划',depends_on:'依赖',informs:'派生',derives:'推导',produces:'产出',supports:'支持',reviews:'复核',executes:'执行',records:'记录',summarizes:'摘要',archives:'归档',contains:'包含',guides:'指导',exposes:'提供',associated_with:'关联',serves:'承载'})[key] || key;
    }
    function styles() {
        const css = getComputedStyle(document.documentElement);
        const text = css.getPropertyValue('--text-primary').trim() || '#172033';
        const surface = css.getPropertyValue('--bg-primary').trim() || '#fff';
        return [
            { selector: 'node', style: { shape: 'round-rectangle', label: 'data(label)', width: 170, height: 68, 'background-color': surface, 'border-width': 2, 'border-color': 'data(color)', color: text, 'font-size': 14, 'text-wrap': 'wrap', 'text-max-width': 150, 'text-valign': 'center', 'text-halign': 'center', padding: 9 } },
            { selector: 'node[kind = "goal"]', style: { 'border-width': 3 } },
            { selector: 'node[kind = "digest"]', style: { 'border-style': 'dashed' } },
            { selector: 'node[state = "running"]', style: { 'background-color': '#dbeafe', color: '#172033' } },
            { selector: 'node:selected', style: { 'border-width': 4, 'overlay-opacity': .08, 'overlay-color': '#3b82f6' } },
            { selector: 'edge', style: { width: 1.5, 'line-color': '#94a3b8', 'target-arrow-color': '#94a3b8', 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', label: 'data(relationLabel)', 'font-size': 10, color: text, 'text-background-color': surface, 'text-background-opacity': .8 } },
            { selector: 'edge[relation = "supports"], edge[relation = "informs"]', style: { 'line-style': 'dashed' } },
        ];
    }
    function layout() {
        if (!chart) return;
        const nodes = chart.nodes().toArray();
        const depth = new Map(nodes.map(n => [n.id(), 0]));
        const incoming = new Map(nodes.map(n => [n.id(), 0]));
        const next = new Map(nodes.map(n => [n.id(), []]));
        chart.edges().forEach(edge => { incoming.set(edge.target().id(), incoming.get(edge.target().id()) + 1); next.get(edge.source().id()).push(edge.target().id()); });
        const queue = nodes.filter(n => incoming.get(n.id()) === 0).map(n => n.id());
        for (let i = 0; i < queue.length; i++) {
            const id = queue[i];
            for (const child of next.get(id)) {
                depth.set(child, Math.max(depth.get(child), depth.get(id) + 1));
                incoming.set(child, incoming.get(child) - 1);
                if (incoming.get(child) === 0) queue.push(child);
            }
        }
        const rows = new Map(), positions = new Map();
        for (const node of nodes) {
            const column = depth.get(node.id()), row = rows.get(column) || 0;
            positions.set(node.id(), { x: 120 + column * 230, y: 60 + row * 110 }); rows.set(column, row + 1);
        }
        chart.layout({ name: 'preset', positions: node => positions.get(node.id()), fit: false }).run();
        chart.fit(undefined, 24);
    }
    function render(container, graph, taskKey, select, label) {
        onSelect = select;
        const nodes = graph?.nodes || [];
        if (host !== container || key !== taskKey) { destroy(); host = container; key = taskKey; }
        if (typeof window.cytoscape !== 'function') {
            host.replaceChildren();
            for (const node of nodes) {
                const button = document.createElement('button'); button.type = 'button'; button.className = 'btn-secondary';
                button.textContent = label(node.kind) + ' · ' + node.title; button.onclick = () => select(node.id); host.append(button);
            }
            return;
        }
        const elements = nodes.map(node => ({ data: { id: node.id, kind: node.kind, state: node.state, label: label(node.kind) + '\n' + (Array.from(node.title).length > 28 ? Array.from(node.title).slice(0, 28).join('') + '…' : node.title), color: colors[node.kind] || colors.step } }));
        const ids = new Set(nodes.map(n => n.id));
        for (const edge of graph.edges || []) if (ids.has(edge.source) && ids.has(edge.target)) {
            elements.push({ data: { id: 'edge:' + JSON.stringify([edge.source, edge.relation, edge.target]), ...edge, relationLabel: relationLabel(edge.relation) } });
        }
        if (!chart) {
            chart = window.cytoscape({ container: host, elements, style: styles(), layout: { name: 'preset' }, minZoom: .12, maxZoom: 2.2, wheelSensitivity: .25 });
            chart.on('tap', 'node', event => onSelect?.(event.target.id()));
            layout();
            if (typeof ResizeObserver === 'function') { observer = new ResizeObserver(() => { if (host?.getClientRects().length) chart?.resize(); }); observer.observe(host); }
        } else {
            const existing = new Set(chart.elements().map(e => e.id()));
            const next = new Set(elements.map(e => e.data.id));
            const changed = existing.size !== next.size || [...next].some(id => !existing.has(id));
            chart.batch(() => {
                chart.elements().filter(e => !next.has(e.id())).remove();
                for (const element of elements) {
                    const old = chart.getElementById(element.data.id);
                    if (old.length) old.data(element.data); else chart.add(element);
                }
            });
            if (changed) {
                const zoom = chart.zoom(), pan = chart.pan();
                const positions = new Map(chart.nodes().filter(n => existing.has(n.id())).map(n => [n.id(), { ...n.position() }]));
                layout();
                chart.batch(() => { for (const [id, position] of positions) chart.getElementById(id).position(position); });
                chart.zoom(zoom); chart.pan(pan);
            }
            chart.resize();
        }
        const currentTheme = document.documentElement.getAttribute('data-theme') || document.body.className;
        if (theme !== currentTheme) { chart.style(styles()); theme = currentTheme; }
    }
    function zoom(factor) {
        if (!chart || !host) return;
        chart.zoom({ level: chart.zoom() * factor, renderedPosition: { x: host.clientWidth / 2, y: host.clientHeight / 2 } });
    }
    window.DeepAssessmentGraph = { render, destroy, select: id => { if (chart) { chart.nodes().unselect(); chart.getElementById(id).select(); } }, fit: () => chart?.fit(undefined, 28), zoomIn: () => zoom(1.25), zoomOut: () => zoom(.8) };
})();
