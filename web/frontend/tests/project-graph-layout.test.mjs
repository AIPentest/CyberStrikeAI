import assert from 'node:assert/strict';
import test from 'node:test';
import { projectGraphLayout } from '../src/pages/projects/fact-graph-layout.js';

test('project graph follows saved links while retaining targets and handling cycles', () => {
    const nodes = [
        { id: 'target', type: 'target' }, { id: 'finding', type: 'finding' }, { id: 'vulnerability', type: 'vulnerability' },
        { id: 'chain-a', type: 'chain' }, { id: 'chain-b', type: 'chain' }, { id: 'isolated', type: 'note' }
    ];
    const edge = (source, target) => ({ id: `${source}-${target}`, source, target });
    const discovery = edge('target', 'finding');
    const evidence = edge('finding', 'vulnerability');
    const feedback = edge('finding', 'target');
    const cycle = [edge('chain-a', 'chain-b'), edge('chain-b', 'chain-a')];
    const edges = [feedback, cycle[1], evidence, discovery, cycle[0]];
    const { positions, parentEdges } = projectGraphLayout(nodes, edges, '', 216, 98);

    assert.equal(positions.size, nodes.length);
    assert.ok(positions.get('target').x < positions.get('finding').x);
    assert.ok(positions.get('finding').x < positions.get('vulnerability').x);
    assert.deepEqual(parentEdges, new Set([discovery, evidence, cycle[0]]));
    const reversed = projectGraphLayout([...nodes].reverse(), [...edges].reverse(), '', 216, 98);
    assert.deepEqual(reversed.positions, positions);
});
