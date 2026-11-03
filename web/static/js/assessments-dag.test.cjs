const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const context = { window: { t: key => key }, document: {} };
vm.createContext(context);
let source = fs.readFileSync('web/static/js/assessments.js', 'utf8');
const end = source.lastIndexOf('})();');
source = source.slice(0, end) + '\nwindow.buildDag = buildDag;\n' + source.slice(end);
vm.runInContext(source, context);
const step = (id, deps = []) => ({ id, key: id, title: id, status: 'pending', depends_on: deps });
const build = steps => JSON.parse(JSON.stringify(context.window.buildDag({ steps })));

function assertForwardEdges(data) {
    const nodes = new Map(data.elements.filter(e => e.position).map(e => [e.data.id, e]));
    for (const e of data.elements.filter(e => e.data.source)) {
        assert.ok(nodes.get(e.data.source).position.x < nodes.get(e.data.target).position.x, 'dependency must precede successor: ' + JSON.stringify(e.data));
    }
    const positions = [...nodes.values()].map(n => JSON.stringify(n.position));
    assert.equal(new Set(positions).size, positions.length, 'nodes overlap');
}

test('DAG lays out parallel branches and joins after the longest prerequisite chain', () => {
    const data = build([step('join', ['left', 'deep']), step('deep', ['right']), step('left', ['root']), step('right', ['root']), step('root')]);
    assert.equal(data.invalid, false);
    assertForwardEdges(data);
    const nodes = new Map(data.elements.filter(e => e.position).map(e => [e.data.stepID, e]));
    assert.equal(nodes.get('left').position.x, nodes.get('right').position.x);
    assert.ok(nodes.get('join').position.x > nodes.get('deep').position.x);
});

test('DAG supports the full 100-step limit without losing edges or overlapping nodes', () => {
    const steps = Array.from({ length: 100 }, (_, i) => step(String(i), i > 1 ? [String(i - 1), String(i - 2)] : []));
    const data = build(steps);
    assert.equal(data.elements.filter(e => e.position).length, 101);
    assert.equal(data.elements.filter(e => e.data.source && e.data.source !== 'goal').length, 196);
    assertForwardEdges(data);
});

test('missing references and cycles are flagged while retaining all steps for inspection', () => {
    for (const steps of [[step('a', ['missing'])], [step('a', ['b']), step('b', ['a']), step('downstream', ['b'])], [step('self', ['self'])]]) {
        const data = build(steps);
        assert.equal(data.invalid, true);
        assert.equal(data.elements.filter(e => e.position).length, steps.length + 1);
        assert.ok(data.elements.filter(e => e.position).every(n => Number.isFinite(n.position.x) && Number.isFinite(n.position.y)));
    }
});

test('model-provided keys cannot collide with the goal or object prototype', () => {
    const data = build([step('goal'), step('__proto__', ['goal', 'goal']), step('constructor', ['__proto__'])]);
    assert.equal(data.invalid, false);
    assert.equal(data.elements.filter(e => e.data.source).length, 3);
    assertForwardEdges(data);
});

test('status changes keep topology identity; replanning changes it', () => {
    const steps = [step('a'), step('b', ['a'])];
    const first = build(steps);
    steps[0].status = 'completed'; steps[1].title = 'Updated title';
    assert.equal(build(steps).signature, first.signature);
    steps.push(step('c', ['b']));
    assert.notEqual(build(steps).signature, first.signature);
});
