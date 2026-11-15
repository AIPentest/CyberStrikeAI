/* Read-only graph renderer; task state and evidence remain owned by the backend. */
(function () {
    'use strict';
    let chart = null, host = null, key = '', onSelect = null, observer = null, theme = '', size = null, mapElement = null, selectedID = '', mapFrame = null;
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
        if(mapFrame!==null)cancelAnimationFrame(mapFrame);mapFrame=null;
        mapElement?.remove();mapElement=null;selectedID='';
        chart?.destroy(); chart = null; host = null; key = ''; theme = ''; size=null;
    }
    function relationLabel(key) {
        const path='deepAssessments.graphRelations.'+key,value=window.t?.(path);
        return value && value!==path ? value : ({via_archive:'经归档',via_details:'经执行记录',plans:'规划',depends_on:'依赖',informs:'派生',derives:'推导',produces:'产出',supports:'支持',reviews:'复核',executes:'执行',records:'记录',summarizes:'摘要',archives:'归档',contains:'包含',guides:'指导',exposes:'提供',associated_with:'关联',serves:'承载'})[key] || key;
    }
    function styles() {
        const css = getComputedStyle(document.documentElement);
        const text = css.getPropertyValue('--text-primary').trim() || '#172033';
        const surface = css.getPropertyValue('--bg-primary').trim() || '#fff';
        return [
            { selector: 'node', style: { shape: 'round-rectangle', label: 'data(label)', width: 224, height: 88, 'background-color': surface, 'border-width': 2, 'border-color': 'data(color)', color: text, 'font-size': 14, 'text-wrap': 'wrap', 'text-max-width': 208, 'text-overflow-wrap':'anywhere', 'text-valign': 'center', 'text-halign': 'center', padding: 9 } },
            { selector: 'node[kind = "goal"]', style: { 'border-width': 3 } },
            {selector:'node[?boundary]',style:{'border-style':'dashed','background-opacity':.6}},
            { selector: 'node[kind = "digest"]', style: { 'border-style': 'dashed' } },
            { selector: 'node[state = "running"]', style: { 'background-color': '#dbeafe', color: '#172033' } },
            { selector: 'node:selected', style: { 'border-width': 4, 'overlay-opacity': .08, 'overlay-color': '#3b82f6' } },
            { selector: 'edge', style: { width: 1.5, 'line-color': 'data(relationColor)', 'target-arrow-color': 'data(relationColor)', 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', label: '', opacity:.65, 'font-size': 10, color: text, 'text-background-color': surface, 'text-background-opacity': .9 } },
            { selector: 'edge.da-connected', style: { label:'data(relationLabel)',width:2.5,opacity:1 } },
            {selector:'edge[projection]',style:{'line-style':'dashed'}},
            { selector: 'edge[relation = "supports"], edge[relation = "informs"]', style: { 'line-style': 'dashed' } },
        ];
    }
    // Only layout ignores DFS back edges. Every original relation remains rendered.
    // Collapsing a strongly connected component into one rank made real evidence
    // cycles turn an entire assessment into a single unreadable vertical column.
    function computeLayout(nodes, edges, preferred = new Map()) {
        const kindOrder={goal:0,domain:0,step:1,subdomain:1,ip:2,service:3,fact:4,finding:5,memory:6};
        const ordered=[...nodes].sort((a,b)=>(kindOrder[a.kind]??7)-(kindOrder[b.kind]??7)||a.id.localeCompare(b.id));
        const ids=new Set(ordered.map(n=>n.id)), outgoing=new Map(ordered.map(n=>[n.id,[]]));
        const structural=new Set(['plans','depends_on','produces','executes','records','summarizes','exposes','serves','contains','associated_with']);
        const valid=edges.filter(e=>ids.has(e.source)&&ids.has(e.target)&&e.source!==e.target);
        for(const edge of [...valid].sort((a,b)=>Number(structural.has(b.relation))-Number(structural.has(a.relation))||a.target.localeCompare(b.target)))outgoing.get(edge.source).push(edge);
        const state=new Map(),back=new Set();
        const visit=id=>{
            state.set(id,1);
            for(const edge of outgoing.get(id)){
                if(state.get(edge.target)===1)back.add(edge);
                else if(!state.has(edge.target))visit(edge.target);
            }
            state.set(id,2);
        };
        ordered.forEach(n=>{if(!state.has(n.id))visit(n.id);});
        const indegree=new Map(ordered.map(n=>[n.id,0])),rank=new Map(ordered.map(n=>[n.id,0]));
        for(const edge of valid)if(!back.has(edge))indegree.set(edge.target,indegree.get(edge.target)+1);
        const queue=ordered.filter(n=>!indegree.get(n.id)).map(n=>n.id);
        for(let i=0;i<queue.length;i++)for(const edge of outgoing.get(queue[i]))if(!back.has(edge)){
            rank.set(edge.target,Math.max(rank.get(edge.target),rank.get(queue[i])+1));
            indegree.set(edge.target,indegree.get(edge.target)-1);
            if(!indegree.get(edge.target))queue.push(edge.target);
        }
        const columns=[];
        ordered.forEach(n=>(columns[rank.get(n.id)]??=[]).push(n.id));
        const neighbors=new Map(ordered.map(n=>[n.id,[]]));
        for(const e of valid){neighbors.get(e.source).push(e.target);neighbors.get(e.target).push(e.source);}
        // Stable barycenter sweeps reduce crossings without changing causal ranks.
        for(let pass=0;pass<4;pass++){
            const direction=pass%2?-1:1;
            for(let c=direction>0?1:columns.length-2;c>=0&&c<columns.length;c+=direction){
                const previous=new Map((columns[c-direction]||[]).map((id,i)=>[id,i]));
                const scored=(columns[c]||[]).map((id,i)=>{
                    const positions=neighbors.get(id).filter(n=>previous.has(n)).map(n=>previous.get(n));
                    return {id,index:i,score:positions.length?positions.reduce((a,b)=>a+b,0)/positions.length:i};
                });
                columns[c]=scored.sort((a,b)=>a.score-b.score||a.index-b.index).map(n=>n.id);
            }
        }
        const positions=new Map(),occupied=[],rows=Math.max(6,Math.ceil(Math.sqrt(nodes.length*.75)));
        for(const node of ordered)if(preferred.has(node.id)){
            const p=preferred.get(node.id);positions.set(node.id,p);occupied.push(p);
        }
        let x=145;
        for(const column of columns){
            if(!column?.length)continue;
            column.forEach((id,i)=>{
                if(positions.has(id))return;
                let p={x:x+Math.floor(i/rows)*300,y:70+(i%rows)*140};
                while(occupied.some(old=>Math.abs(old.x-p.x)<250&&Math.abs(old.y-p.y)<125))p={x:p.x,y:p.y+140};
                positions.set(id,p);occupied.push(p);
            });
            x+=Math.ceil(column.length/rows)*300;
        }
        return positions;
    }
    function layout(preferred = new Map()) {
        if(!chart)return;
        const positions=computeLayout(chart.nodes().map(n=>n.data()),chart.edges().map(e=>e.data()),preferred);
        chart.layout({name:'preset',positions:n=>positions.get(n.id()),fit:false}).run();
    }
    function updateMap() {
        if(!chart || !mapElement || !host?.clientWidth)return;
        mapElement.style.display=chart.nodes().length<8?'none':'';
        const bounds=chart.nodes().boundingBox(),pad=40;
        mapElement.setAttribute('viewBox',[bounds.x1-pad,bounds.y1-pad,bounds.w+pad*2,bounds.h+pad*2].join(' '));
        mapElement.replaceChildren();
        const rectangle=(x,y,width,height,fill,stroke)=>{
            const r=document.createElementNS('http://www.w3.org/2000/svg','rect');
            Object.entries({x,y,width,height,fill,stroke:stroke||'none'}).forEach(([k,v])=>r.setAttribute(k,String(v)));
            mapElement.append(r);return r;
        };
        chart.nodes().forEach(n=>{const p=n.position();rectangle(p.x-112,p.y-44,224,88,n.data('color'));});
        const e=chart.extent(),viewport=rectangle(e.x1,e.y1,e.w,e.h,'#3b82f61a','#2563eb');
        viewport.setAttribute('stroke-width','2');viewport.setAttribute('vector-effect','non-scaling-stroke');
    }
    function installMap() {
        mapElement=document.createElementNS('http://www.w3.org/2000/svg','svg');
        mapElement.classList.add('da-graph-minimap');mapElement.setAttribute('role','button');mapElement.setAttribute('tabindex','0');
        const path='deepAssessments.graphNavigator',label=window.t?.(path);
        mapElement.setAttribute('aria-label',label&&label!==path?label:'图导航：点击定位，方向键平移');
        host.append(mapElement);
        mapElement.addEventListener('pointerdown',e=>e.stopPropagation());
        mapElement.addEventListener('click',e=>{
            e.stopPropagation();const p=mapElement.createSVGPoint();p.x=e.clientX;p.y=e.clientY;
            const matrix=mapElement.getScreenCTM();if(!matrix)return;
            const center=p.matrixTransform(matrix.inverse()),zoom=chart.zoom();
            chart.pan({x:host.clientWidth/2-center.x*zoom,y:host.clientHeight/2-center.y*zoom});remember();
        });
        mapElement.addEventListener('keydown',e=>{
            const directions={ArrowLeft:[80,0],ArrowRight:[-80,0],ArrowUp:[0,80],ArrowDown:[0,-80]};
            if(directions[e.key]){e.preventDefault();chart.panBy({x:directions[e.key][0],y:directions[e.key][1]});remember();}
        });
        chart.on('pan zoom position',()=>{if(mapFrame===null)mapFrame=requestAnimationFrame(()=>{mapFrame=null;updateMap();});});updateMap();
    }
    function render(container, graph, taskKey, select, label, stateLabel = () => '') {
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
        const elements = nodes.map(node => ({ data: { id: node.id, kind: node.kind, state: node.state, boundary:!!node.boundary,label: label(node.kind) + (node.state&&stateLabel(node.state)?' · '+stateLabel(node.state):'') + '\n' + (Array.from(node.title).length > 42 ? Array.from(node.title).slice(0, 42).join('') + '…' : node.title), color: colors[node.kind] || colors.step } }));
        const ids = new Set(nodes.map(n => n.id));
        for (const edge of graph.edges || []) if (ids.has(edge.source) && ids.has(edge.target)) {
            const relationColor=({plans:'#6366f1',depends_on:'#6366f1',informs:'#059669',derives:'#059669',produces:'#d97706',supports:'#e11d48',reviews:'#e11d48',summarizes:'#a855f7',archives:'#a855f7'})[edge.relation]||'#94a3b8';
            elements.push({ data: { id: 'edge:' + JSON.stringify([edge.source, edge.relation, edge.target,edge.projection||'']), ...edge, relationLabel: relationLabel(edge.relation)+(edge.projection?' · '+relationLabel('via_'+edge.projection):'')+(edge.references>1?' ×'+edge.references:''),relationColor } });
        }
        if (!chart) {
            chart = window.cytoscape({ container: host, elements, style: styles(), layout: { name: 'preset' }, minZoom: .12, maxZoom: 2.2, wheelSensitivity: .25 });
            chart.on('tap', 'node', event => onSelect?.(event.target.id()));
            const saved=views.get(taskKey);
            layout(saved?.positions);size={width:host.clientWidth,height:host.clientHeight};
            if(saved){chart.zoom(saved.zoom);chart.pan({x:size.width/2-saved.center.x*saved.zoom,y:size.height/2-saved.center.y*saved.zoom});}else fit();
            installMap();
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
            resize();updateMap();
        }
        const currentTheme = document.documentElement.getAttribute('data-theme') || document.body.className;
        if (theme !== currentTheme) { chart.style(styles()); theme = currentTheme; }
    }
    function zoom(factor) {
        if (!chart || !host) return;
        chart.zoom({ level: chart.zoom() * factor, renderedPosition: { x: host.clientWidth / 2, y: host.clientHeight / 2 } });
    }
    function selectNode(id) {
        if(!chart)return;
        const changed=selectedID!==id;selectedID=id;
        chart.nodes().unselect();const node=chart.getElementById(id);node.select();
        chart.edges().removeClass('da-connected');node.connectedEdges().addClass('da-connected');
        if(changed&&node.length){
            const p=node.position(),zoom=Math.max(.65,chart.zoom()),rendered=node.renderedPosition();
            if(chart.zoom()<.65||rendered.x<0||rendered.x>host.clientWidth||rendered.y<0||rendered.y>host.clientHeight){
                chart.zoom(zoom);chart.pan({x:host.clientWidth/2-p.x*zoom,y:host.clientHeight/2-p.y*zoom});
            }
        }
    }
    window.DeepAssessmentGraph = { render, destroy, computeLayout, select:selectNode, fit, relayout: () => { layout(); fit(); }, zoomIn: () => zoom(1.25), zoomOut: () => zoom(.8) };
})();
