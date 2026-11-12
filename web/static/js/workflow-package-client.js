(function (root, factory) {
    const client = factory(root);
    if (typeof module === 'object' && module.exports) module.exports = client;
    if (root) root.WorkflowPackageClient = client;
})(typeof globalThis !== 'undefined' ? globalThis : this, function (root) {
    const ACTIONS_BY_CONFLICT = {
        none: ['create'],
        identical: ['keep_existing'],
        id_conflict: ['keep_existing', 'overwrite', 'rename']
    };

    class WorkflowPackageError extends Error {
        constructor(code, message, details) {
            super(message || code || 'WFPKG_REQUEST_FAILED');
            this.name = 'WorkflowPackageError';
            this.code = code || '';
            this.details = details || {};
        }
    }

    function allowedActions(conflictState) {
        return (ACTIONS_BY_CONFLICT[conflictState] || []).slice();
    }

    function buildImportRequest(input) {
        const data = input || {};
        const inspectionId = String(data.inspectionId || '').trim();
        const action = String(data.action || '').trim();
        const newWorkflowId = String(data.newWorkflowId || '').trim();
        const confirmOverwrite = data.confirmOverwrite === true;
        if (!inspectionId) throw new WorkflowPackageError('WFPKG_INSPECTION_REQUIRED', 'Missing preflight record');
        if (!['create', 'keep_existing', 'overwrite', 'rename'].includes(action)) {
            throw new WorkflowPackageError('WFPKG_INVALID_ACTION', 'Invalid import-handling mode');
        }
        if (action === 'rename' && !newWorkflowId) {
            throw new WorkflowPackageError('WFPKG_INVALID_RENAME_ID', 'Please enter a new workflow ID');
        }
        if (action !== 'rename' && newWorkflowId) {
            throw new WorkflowPackageError('WFPKG_INVALID_ACTION', 'The current handling mode does not allow entering a new workflow ID');
        }
        if (action === 'overwrite' && !confirmOverwrite) {
            throw new WorkflowPackageError('WFPKG_OVERWRITE_CONFIRMATION_REQUIRED', 'Please confirm overwriting the local workflow');
        }
        return {
            inspection_id: inspectionId,
            resolution: {
                action: action,
                new_workflow_id: action === 'rename' ? newWorkflowId : ''
            },
            confirm_overwrite: action === 'overwrite'
        };
    }

    function createIdempotencyKey() {
        const cryptoApi = root && root.crypto;
        if (!cryptoApi || typeof cryptoApi.randomUUID !== 'function') {
            throw new WorkflowPackageError('WFPKG_IDEMPOTENCY_KEY_REQUIRED', 'The browser could not generate an import-request identifier');
        }
        return cryptoApi.randomUUID();
    }

    async function readApiError(response) {
        const payload = await response.json().catch(function () { return {}; });
        const error = payload && payload.error ? payload.error : {};
        return {
            code: error.code || '',
            message: error.message || '',
            details: error.details || {}
        };
    }

    async function readJsonOrThrow(response) {
        if (response.ok) return response.json();
        const error = await readApiError(response);
        throw new WorkflowPackageError(error.code, error.message, error.details);
    }

    async function createInspection(apiFetch, file) {
        if (!file) throw new WorkflowPackageError('WFPKG_FILE_REQUIRED', 'Please select a local workflow package');
        const body = new FormData();
        body.append('file', file, file.name || 'workflow.csapkg.zip');
        const response = await apiFetch('/api/workflow-package-inspections', { method: 'POST', body: body });
        return readJsonOrThrow(response);
    }

    async function getInspection(apiFetch, inspectionId) {
        const response = await apiFetch('/api/workflow-package-inspections/' + encodeURIComponent(inspectionId));
        return readJsonOrThrow(response);
    }

    async function applyImport(apiFetch, request, idempotencyKey) {
        if (!idempotencyKey) throw new WorkflowPackageError('WFPKG_IDEMPOTENCY_KEY_REQUIRED', 'Missing import-request identifier');
        const response = await apiFetch('/api/workflow-package-imports', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
            body: JSON.stringify(request)
        });
        return readJsonOrThrow(response);
    }

    async function getImport(apiFetch, importId) {
        const response = await apiFetch('/api/workflow-package-imports/' + encodeURIComponent(importId));
        return readJsonOrThrow(response);
    }

    return {
        WorkflowPackageError: WorkflowPackageError,
        allowedActions: allowedActions,
        buildImportRequest: buildImportRequest,
        createIdempotencyKey: createIdempotencyKey,
        readApiError: readApiError,
        createInspection: createInspection,
        getInspection: getInspection,
        applyImport: applyImport,
        getImport: getImport
    };
});
