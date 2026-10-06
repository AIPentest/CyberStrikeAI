const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');

const source = fs.readFileSync(path.join(__dirname, 'workflows.js'), 'utf8');

function createEditor() {
    const elements = {
        'workflow-id': { value: '' },
        'workflow-name': { value: '' },
        'workflow-description': { value: '' },
        'workflow-enabled': { checked: true },
        'workflow-canvas': {}
    };
    const nodes = [
        { id: 'start', type: 'start', config: {} },
        { id: 'output', type: 'output', config: { output_key: 'result' } }
    ].map(data => ({ id: () => data.id, data: key => data[key], position: () => ({ x: 0, y: 0 }) }));
    const state = { calls: [], notifications: [], workflows: [], modalOpen: false };
    const context = {
        document: { getElementById: id => elements[id] || null, querySelector: () => null, addEventListener() {} },
        cytoscape: () => ({
            nodes: () => nodes,
            edges: () => [{ id: () => 'edge', source: () => nodes[0], target: () => nodes[1], data: () => '' }],
            on() {}, resize() {}
        }),
        openAppModal: () => { state.modalOpen = true; },
        closeAppModal: () => { state.modalOpen = false; },
        showNotification: (message, type) => state.notifications.push({ message, type }),
        apiFetch: async (url, options = {}) => {
            const body = options.body ? JSON.parse(options.body) : undefined;
            state.calls.push({ url, method: options.method || 'GET', body });
            if (url === '/api/workflows/validate') {
                if (state.validationGate) await state.validationGate;
                return { ok: true };
            }
            if (options.method === 'POST' || options.method === 'PUT') {
                const workflow = { ...body, graph_json: JSON.stringify(body.graph), version: 1 };
                state.workflows = [workflow];
                return { ok: true, json: async () => ({ workflow }) };
            }
            if (url.startsWith('/api/workflows?')) return { ok: true, json: async () => ({ workflows: state.workflows }) };
            throw new Error('Unexpected API: ' + url);
        }
    };
    context.window = context;
    vm.createContext(context);
    vm.runInContext(source, context, { filename: 'workflows.js' });
    const fillMeta = () => {
        elements['workflow-id'].value = 'save-regression';
        elements['workflow-name'].value = 'Save regression';
    };
    const saves = () => state.calls.filter(call => call.url === '/api/workflows' && call.method === 'POST');
    return { context, state, elements, fillMeta, saves };
}

test('Save resumes after metadata confirmation and submits the graph once', async () => {
    const editor = createEditor();
    await editor.context.saveWorkflowDraft();
    assert.equal(editor.state.modalOpen, true);
    assert.equal(editor.state.calls.length, 0);
    editor.fillMeta();
    let finishValidation;
    editor.state.validationGate = new Promise(resolve => { finishValidation = resolve; });
    const saving = editor.context.applyWorkflowMetaModal();
    await editor.context.applyWorkflowMetaModal();
    assert.equal(editor.state.calls.filter(call => call.url === '/api/workflows/validate').length, 1);
    assert.equal(editor.saves().length, 0);
    finishValidation();
    await saving;
    assert.equal(editor.saves().length, 1);
    assert.equal(editor.saves()[0].body.name, 'Save regression');
    assert.deepEqual(editor.saves()[0].body.graph.nodes.map(node => node.id), ['start', 'output']);
    assert.equal(editor.state.modalOpen, false);
    assert.equal(editor.state.notifications.at(-1).message, 'workflows.saved');
    assert.ok(editor.state.calls.some(call => call.url.startsWith('/api/workflows?')));
});

test('Cancel clears a pending save; reopening metadata only edits the draft', async () => {
    const editor = createEditor();
    await editor.context.saveWorkflowDraft();
    editor.context.closeWorkflowMetaModal();
    editor.fillMeta();
    await editor.context.applyWorkflowMetaModal();
    assert.equal(editor.state.calls.length, 0);
    await editor.context.saveWorkflowDraft();
    assert.equal(editor.saves().length, 1);
    const callCount = editor.state.calls.length;
    editor.context.openWorkflowMetaModal();
    await editor.context.applyWorkflowMetaModal();
    assert.equal(editor.state.calls.length, callCount);
});

test('Opening metadata normally replaces pending save intent', async () => {
    const editor = createEditor();
    await editor.context.saveWorkflowDraft();
    editor.context.openWorkflowMetaModal();
    editor.fillMeta();
    await editor.context.applyWorkflowMetaModal();
    assert.equal(editor.state.calls.length, 0);
});

test('Invalid metadata keeps the pending save until corrected', async () => {
    const editor = createEditor();
    await editor.context.saveWorkflowDraft();
    await editor.context.applyWorkflowMetaModal();
    assert.equal(editor.state.modalOpen, true);
    assert.equal(editor.state.calls.length, 0);
    editor.fillMeta();
    await editor.context.applyWorkflowMetaModal();
    assert.equal(editor.saves().length, 1);
});
