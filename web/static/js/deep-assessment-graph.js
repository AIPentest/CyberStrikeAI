/* Read-only graph renderer; task state and evidence remain owned by the backend. */
(function () {
    'use strict';
    let chart = null, host = null, key = '', onSelect = null, observer = null, theme = '', size = null;
    const views = new Map();
    function remember() {
        if(!chart || !key || !size?.width || !size?.height)return;
        const pan=chart.pan(),zoom=chart.zoom();
        views.delete(key);views.set(key,{zoom,center:{x:(size.width/2-pan.x)/zoom,y:(size.height/2-pan.y)/zoom},positions:new Map(chart.nodes().map(n=>[n.id(),{...n.position()}]))});
        if(views.size>40)views.delete(views.keys().next().value);
    }
    function resize() {
        if(!chart || !host?.getClientRects().length)return;
        const next={width:host.clientWidth,height:host.clientHeight};
        if(size && next.width===size.width && next.height===size.height)return;
        const pan=chart.pan(),previous=size;
        chart.resize();size=next;
        if(previous)chart.pan({x:pan.x+(next.width-previous.width)/2,y:pan.y+(next.height-previous.height)/2});
    }
    function fit() { if(!chart)return;resize();chart.fit(undefined,32);if(chart.zoom()>1)chart.zoom({level:1,renderedPosition:{x:host.clientWidth/2,y:host.clientHeight/2}});remember(); }
    const colors = { fact:'#0d9488',finding:'#e11d48',domain:'#6366f1',subdomain:'#818cf8',ip:'#0891b2',service:'#2563eb',application:'#c026d3',endpoint:'#0d9488', goal: '#6366f1', step: '#3b82f6', observation: '#14b8a6', receipt: '#64748b', memory: '#a855f7', digest: '#d97706', guidance: '#0891b2' };
    function destroy() {
        remember();
        observer?.disconnect(); observer = null;
        chart?.destroy(); chart = null; host = null; key = ''; theme = ''; size=null;
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
            { selector: 'node', style: { shape: 'round-rectangle', label: 'data(label)', width: 224, height: 88, 'background-color': surface, 'border-width': 2, 'border-color': 'data(color)', color: text, 'font-size': 14, 'text-wrap': 'wrap', 'text-max-width': 208, 'text-overflow-wrap':'anywhere', 'text-valign': 'center', 'text-halign': 'center', padding: 9 } },
            { selector: 'node[kind = "goal"]', style: { 'border-width': 3 } },
            { selector: 'node[kind = "digest"]', style: { 'border-style': 'dashed' } },
            { selector: 'node[state = "running"]', style: { 'background-color': '#dbeafe', color: '#172033' } },
            { selector: 'node:selected', style: { 'border-width': 4, 'overlay-opacity': .08, 'overlay-color': '#3b82f6' } },
            { selector: 'edge', style: { width: 1.5, 'line-color': '#94a3b8', 'target-arrow-color': '#94a3b8', 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', label: 'data(relationLabel)', 'font-size': 10, color: text, 'text-background-color': surface, 'text-background-opacity': .8 } },
            { selector: 'edge[relation = "supports"], edge[relation = "informs"]', style: { 'line-style': 'dashed' } },
        ];
    }
    function layout(preferred = new Map()) {
        if (!chart) return;
        const nodes=chart.nodes().toArray().sort((a,b)=>a.id().localeCompare(b.id()));
        const adjacency=new Map(nodes.map(n=>[n.id(),[]]));
        chart.edges().forEach(e=>adjacency.get(e.source().id()).push(e.target().id()));
        // Condense evidence/review cycles before assigning columns.
        let nextIndex=0;const indices=new Map(),low=new Map(),stack=[],onStack=new Set(),components=[],component=new Map();
        const visit=id=>{
            indices.set(id,nextIndex);low.set(id,nextIndex++);stack.push(id);onStack.add(id);
            for(const child of adjacency.get(id)){
                if(!indices.has(child)){visit(child);low.set(id,Math.min(low.get(id),low.get(child)));}
                else if(onStack.has(child))low.set(id,Math.min(low.get(id),indices.get(child)));
            }
            if(low.get(id)===indices.get(id)){
                const members=[];let child;
                do{child=stack.pop();onStack.delete(child);component.set(child,components.length);members.push(child);}while(child!==id);
                components.push(members);
            }
        };
        nodes.forEach(n=>{if(!indices.has(n.id()))visit(n.id());});
        const outgoing=components.map(()=>new Set()),incoming=components.map(()=>0),depth=components.map(()=>0);
        chart.edges().forEach(e=>{const from=component.get(e.source().id()),to=component.get(e.target().id());if(from!==to&&!outgoing[from].has(to)){outgoing[from].add(to);incoming[to]++;}});
        const queue=components.map((_,i)=>i).filter(i=>incoming[i]===0);
        for(let i=0;i<queue.length;i++)for(const child of outgoing[queue[i]]){depth[child]=Math.max(depth[child],depth[queue[i]]+1);if(--incoming[child]===0)queue.push(child);}
        const positions=new Map(),occupied=[];
        for(const node of nodes)if(preferred.has(node.id())){const pos=preferred.get(node.id());positions.set(node.id(),pos);occupied.push(pos);}
        for(const node of nodes){
            if(positions.has(node.id()))continue;
            const x=145+depth[component.get(node.id())]*300;let y=70;
            while(occupied.some(p=>Math.abs(p.x-x)<250&&Math.abs(p.y-y)<120))y+=132;
            const pos={x,y};positions.set(node.id(),pos);occupied.push(pos);
        }
        chart.layout({name:'preset',positions:n=>positions.get(n.id()),fit:false}).run();
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
        const elements = nodes.map(node => ({ data: { id: node.id, kind: node.kind, state: node.state, label: label(node.kind) + '\n' + (Array.from(node.title).length > 42 ? Array.from(node.title).slice(0, 42).join('') + '…' : node.title), color: colors[node.kind] || colors.step } }));
        const ids = new Set(nodes.map(n => n.id));
        for (const edge of graph.edges || []) if (ids.has(edge.source) && ids.has(edge.target)) {
            elements.push({ data: { id: 'edge:' + JSON.stringify([edge.source, edge.relation, edge.target]), ...edge, relationLabel: relationLabel(edge.relation) } });
        }
        if (!chart) {
            chart = window.cytoscape({ container: host, elements, style: styles(), layout: { name: 'preset' }, minZoom: .12, maxZoom: 2.2, wheelSensitivity: .25 });
            chart.on('tap', 'node', event => onSelect?.(event.target.id()));
            const saved=views.get(taskKey);
            layout(saved?.positions);size={width:host.clientWidth,height:host.clientHeight};
            if(saved){chart.zoom(saved.zoom);chart.pan({x:size.width/2-saved.center.x*saved.zoom,y:size.height/2-saved.center.y*saved.zoom});}else fit();
            if (typeof ResizeObserver === 'function') { observer = new ResizeObserver(resize); observer.observe(host); }
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
                layout(positions);
                chart.zoom(zoom); chart.pan(pan);
            }
            resize();
        }
        const currentTheme = document.documentElement.getAttribute('data-theme') || document.body.className;
        if (theme !== currentTheme) { chart.style(styles()); theme = currentTheme; }
    }
    function zoom(factor) {
        if (!chart || !host) return;
        chart.zoom({ level: chart.zoom() * factor, renderedPosition: { x: host.clientWidth / 2, y: host.clientHeight / 2 } });
    }
    window.DeepAssessmentGraph = { render, destroy, select: id => { if (chart) { chart.nodes().unselect(); chart.getElementById(id).select(); } }, fit, relayout: () => { layout(); fit(); }, zoomIn: () => zoom(1.25), zoomOut: () => zoom(.8) };
})();
