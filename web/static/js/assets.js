const ASSET_PAGE_SIZE_KEY = 'cyberstrike.asset_page_size';
const ASSET_SAVED_VIEWS_KEY = 'cyberstrike.asset_saved_views';
function getAssetPageSize() {
    try {
        const value = Number(localStorage.getItem(ASSET_PAGE_SIZE_KEY));
        return [10, 20, 50, 100].includes(value) ? value : 20;
    } catch (error) {
        return 20;
    }
}
const assetPageState = { page: 1, pageSize: getAssetPageSize(), total: 0, totalPages: 1, items: [], projects: [], projectsLoaded: false, detailIndex: -1, editIndex: -1, detailAsset: null, editAsset: null, selected: new Map(), selectionQuery: '', allMatchingSelected: false, scanMode: 'chat', scanAssets: [], editorTags: [], editorDirty: false, editorBusy: false, editorReturnFocus: null, editorInteractionsReady: false, editorParsedTarget: '', importRows: [], importFileName: '', importBusy: false, importInteractionsReady: false, importReturnFocus: null };
let assetOverviewDays = 30;

function assetEscapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

const ASSET_CUSTOM_SELECT_IDS = [
    'asset-status-filter',
    'asset-project-filter',
    'asset-risk-filter',
    'asset-scan-filter',
    'asset-environment-filter',
    'asset-criticality-filter',
    'asset-sort-filter',
    'asset-saved-view-select',
    'asset-batch-project',
    'asset-edit-project',
    'asset-edit-status',
    'asset-edit-environment',
    'asset-edit-criticality',
    'asset-bulk-status',
    'asset-bulk-environment',
    'asset-bulk-criticality',
    'asset-page-size-pagination'
];

function enhanceAssetSelect(select) {
    if (!select || typeof enhanceSettingsSelect !== 'function') return;
    enhanceSettingsSelect(select);
    const wrapper = select.closest('.settings-custom-select');
    if (!wrapper) return;
    wrapper.classList.add('asset-custom-select');
    wrapper.classList.toggle('asset-custom-select--filter', Boolean(select.closest('.asset-toolbar')));
    wrapper.classList.toggle('asset-custom-select--saved-view', select.id === 'asset-saved-view-select');
    wrapper.classList.toggle('asset-custom-select--pagination', select.id === 'asset-page-size-pagination');
}

function initAssetCustomSelects(root) {
    const scope = root || document;
    ASSET_CUSTOM_SELECT_IDS.forEach(id => {
        const select = scope.getElementById ? scope.getElementById(id) : scope.querySelector(`#${id}`);
        if (select) enhanceAssetSelect(select);
    });
}

function syncAssetSelect(selectOrId) {
    const select = typeof selectOrId === 'string' ? document.getElementById(selectOrId) : selectOrId;
    if (!select) return;
    enhanceAssetSelect(select);
    if (typeof syncSettingsCustomSelect === 'function') syncSettingsCustomSelect(select);
}

function closeAssetCustomSelects() {
    if (typeof closeAllSettingsCustomSelects === 'function') {
        closeAllSettingsCustomSelects();
    }
}

function assetT(key, fallback, options) {
    if (window.i18next && typeof window.i18next.t === 'function') {
        const value = window.i18next.t(key, options || {});
        if (value && value !== key) return value;
    }
    return fallback;
}

const ASSET_IMPORT_MAX_ROWS = 100000;
const ASSET_IMPORT_MAX_BYTES = 100 * 1024 * 1024;
const ASSET_IMPORT_COLUMNS = ['target', 'project', 'tags', 'host', 'ip', 'domain', 'port', 'protocol', 'title', 'server', 'country', 'province', 'city', 'responsible_person', 'department', 'business_system', 'environment', 'criticality', 'status'];
const ASSET_IMPORT_HEADER_ALIASES = {
    target: ['target', 'asset', 'assetaddress', 'Target', 'Asset', 'Asset address'],
    project: ['project', 'projectname', 'projectid', 'Project', 'Project name', 'Projectid', 'Project'],
    tags: ['tags', 'tag', 'Tag'],
    host: ['host', 'url', 'Completeurl', 'Host'],
    ip: ['ip', 'ipaddress', 'ip Address'],
    domain: ['domain', 'Domain'],
    port: ['port', 'Port'],
    protocol: ['protocol', 'scheme', 'Protocol'],
    title: ['title', 'pagetitle', 'Title', 'Page title'],
    server: ['server', 'service', 'product', 'Service', 'Product', 'Service fingerprint'],
    country: ['country', 'countryregion', 'Country', 'Country/region'],
    province: ['province', 'state', 'Province', 'State'],
    city: ['city', 'City'],
    responsible_person: ['responsibleperson', 'owner', 'Owner', 'Owner'],
    department: ['department', 'Department'],
    business_system: ['businesssystem', 'system', 'Business system', 'System'],
    environment: ['environment', 'env', 'Environment'],
    criticality: ['criticality', 'importance', 'Importance', 'Importance level'],
    status: ['status', 'Status']
};

const ASSET_EXPORT_LABELS = {
    target: 'target', host: 'hostUrl', ip: 'exportIP', domain: 'domain', port: 'port', protocol: 'protocol',
    title: 'pageTitle', server: 'server', project: 'project', responsible_person: 'responsiblePerson',
    department: 'department', business_system: 'businessSystem', environment: 'environment', criticality: 'criticality',
    country: 'country', province: 'province', city: 'city', source: 'source', status: 'status', tags: 'tagsLabel',
    risk_level: 'riskLevel', vulnerability_count: 'exportVulnerabilityCount', first_seen_at: 'firstSeen',
    last_seen_at: 'lastSeen', last_scan_at: 'lastScan'
};

function assetExportHeader(key) {
    return assetT('assets.' + ASSET_EXPORT_LABELS[key], key);
}

function assetLocalizedEnumMap(values, prefix) {
    const result = { ...values };
    for (const code of new Set(Object.values(values))) {
        if (!code) continue;
        const key = prefix + code[0].toUpperCase() + code.slice(1);
        result[assetT('assets.' + key, code).toLowerCase()] = code;
    }
    return result;
}

function assetExportValue(key, value) {
    if (['environment', 'criticality', 'status'].includes(key) && value) {
        const codes = { environment: ['production', 'staging', 'testing', 'development', 'other'], criticality: ['critical', 'high', 'medium', 'low'], status: ['active', 'inactive'] };
        if (codes[key].includes(value)) return assetT('assets.' + key + value[0].toUpperCase() + value.slice(1), value);
    }
    if (key === 'risk_level' && ['critical', 'high', 'medium', 'low', 'info', 'normal', 'unassessed'].includes(value)) return assetRiskPresentation(value).label;
    return value;
}

function normalizeAssetImportHeader(value) {
    return String(value == null ? '' : value).replace(/^\uFEFF/, '').trim().toLowerCase().replace(/[\s_\-/.（）()]+/g, '');
}

function assetImportColumnForHeader(value) {
    const normalized = normalizeAssetImportHeader(value);
    return ASSET_IMPORT_COLUMNS.find(column => (ASSET_IMPORT_HEADER_ALIASES[column].includes(normalized) || normalizeAssetImportHeader(assetExportHeader(column)) === normalized)) || '';
}

function assetImportTemplateHeaders() {
    return ASSET_IMPORT_COLUMNS.slice();
}

function downloadAssetTemplate(format) {
    const headers = assetImportTemplateHeaders();
    if (format === 'csv') {
        const csv = '\uFEFF' + headers.join(',') + '\r\n';
        const link = document.createElement('a');
        link.href = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
        link.download = 'asset-import-template.csv';
        link.click();
        setTimeout(() => URL.revokeObjectURL(link.href), 0);
        return;
    }
    if (!window.XLSX) {
        setAssetImportError(assetT('assets.spreadsheetUnavailable', 'The table component failed to load; please refresh and retry'));
        return;
    }
    const workbook = XLSX.utils.book_new();
    const assetSheet = XLSX.utils.aoa_to_sheet([headers]);
    assetSheet['!cols'] = headers.map(name => ({ wch: ['target', 'host'].includes(name) ? 28 : 16 }));
    assetSheet['!autofilter'] = { ref: `A1:${XLSX.utils.encode_col(headers.length - 1)}1` };
    const instructions = [
        [assetT('assets.templateGuideTitle', 'Asset bulk-import instructions')],
        [assetT('assets.templateGuideRequired', 'Required: target, or at least one of host / ip / domain')],
        [assetT('assets.templateGuideTarget', 'target Example: https://example.com:443, example.com, 1.1.1.1:22')],
        [assetT('assets.templateGuideProject', 'project Enter an existing project name or project ID; empty means no binding')],
        [assetT('assets.templateGuideTags', 'tags Comma-separated, up to 30')],
        [assetT('assets.templateGuideStatus', 'status Only active / inactive are supported; empty defaults to active')],
        [assetT('assets.templateGuideLimit', 'Please inAssetsfill in the worksheet, up to 100000 rows')]
    ];
    const guideSheet = XLSX.utils.aoa_to_sheet(instructions);
    guideSheet['!cols'] = [{ wch: 86 }];
    XLSX.utils.book_append_sheet(workbook, assetSheet, 'Assets');
    XLSX.utils.book_append_sheet(workbook, guideSheet, assetT('assets.instructionsSheet', 'Instructions').slice(0, 31));
    XLSX.writeFile(workbook, 'asset-import-template.xlsx');
}

async function openAssetImport() {
    assetPageState.importReturnFocus = document.activeElement;
    await ensureAssetProjects(true);
    resetAssetImport();
    ensureAssetImportInteractions();
    if (typeof openAppModal === 'function') openAppModal('asset-import-modal', { focusEl: document.getElementById('asset-import-dropzone') });
    else document.getElementById('asset-import-modal').style.display = 'flex';
}

function closeAssetImport(force) {
    if (assetPageState.importBusy && !force) return;
    if (typeof closeAppModal === 'function') closeAppModal('asset-import-modal');
    else document.getElementById('asset-import-modal').style.display = 'none';
    const returnFocus = assetPageState.importReturnFocus;
    if (returnFocus && typeof returnFocus.focus === 'function') requestAnimationFrame(() => returnFocus.focus());
}

function resetAssetImport() {
    assetPageState.importRows = [];
    assetPageState.importFileName = '';
    assetPageState.importBusy = false;
    const file = document.getElementById('asset-import-file');
    if (file) file.value = '';
    const name = document.getElementById('asset-import-file-name');
    if (name) name.textContent = assetT('assets.fileNotSelected', 'No file selected yet');
    const preview = document.getElementById('asset-import-preview-section');
    if (preview) preview.hidden = true;
    const body = document.getElementById('asset-import-preview-body');
    if (body) body.innerHTML = '';
    setAssetImportError('');
    setAssetImportBusy(false);
}

function ensureAssetImportInteractions() {
    if (assetPageState.importInteractionsReady) return;
    assetPageState.importInteractionsReady = true;
    const dropzone = document.getElementById('asset-import-dropzone');
    if (!dropzone) return;
    ['dragenter', 'dragover'].forEach(type => dropzone.addEventListener(type, event => {
        event.preventDefault();
        dropzone.classList.add('is-dragging');
    }));
    ['dragleave', 'drop'].forEach(type => dropzone.addEventListener(type, event => {
        event.preventDefault();
        dropzone.classList.remove('is-dragging');
    }));
    dropzone.addEventListener('drop', event => handleAssetImportFile(event.dataTransfer?.files?.[0]));
}

function setAssetImportError(message) {
    const root = document.getElementById('asset-import-error');
    if (!root) return;
    root.textContent = message || '';
    root.hidden = !message;
}

function setAssetImportBusy(busy) {
    assetPageState.importBusy = Boolean(busy);
    const submit = document.getElementById('asset-import-submit');
    if (submit) submit.disabled = Boolean(busy) || !assetPageState.importRows.some(row => !row.error);
    const dropzone = document.getElementById('asset-import-dropzone');
    if (dropzone) dropzone.disabled = Boolean(busy);
}

async function handleAssetImportFile(file) {
    setAssetImportError('');
    if (!file) return;
    assetPageState.importRows = [];
    assetPageState.importFileName = '';
    const previousPreview = document.getElementById('asset-import-preview-section');
    if (previousPreview) previousPreview.hidden = true;
    const selectedName = document.getElementById('asset-import-file-name');
    if (selectedName) selectedName.textContent = file.name || assetT('assets.fileNotSelected', 'No file selected yet');
    setAssetImportBusy(false);
    const extension = String(file.name || '').split('.').pop().toLowerCase();
    if (!['xlsx', 'csv'].includes(extension)) {
        setAssetImportError(assetT('assets.fileTypeInvalid', 'Only .xlsx and .csv files are supported'));
        return;
    }
    if (file.size > ASSET_IMPORT_MAX_BYTES) {
        setAssetImportError(assetT('assets.fileTooLarge', 'The file cannot exceed 100 MB'));
        return;
    }
    if (!window.XLSX) {
        setAssetImportError(assetT('assets.spreadsheetUnavailable', 'The table component failed to load; please refresh and retry'));
        return;
    }
    setAssetImportBusy(true);
    try {
        const workbook = XLSX.read(await file.arrayBuffer(), { type: 'array', cellDates: false });
        const preferred = workbook.SheetNames.find(name => /^(assets?|资产)$/i.test(String(name).trim()));
        const sheet = workbook.Sheets[preferred || workbook.SheetNames[0]];
        const matrix = sheet ? XLSX.utils.sheet_to_json(sheet, { header: 1, defval: '', raw: false, blankrows: false }) : [];
        parseAssetImportMatrix(matrix, file.name);
    } catch (error) {
        console.error('Failed to parse the asset import file:', error);
        setAssetImportError(error?.message || assetT('assets.fileParseFailed', 'Unable to parse the file; please make sure it is not corrupted and is in the correct format'));
    } finally {
        setAssetImportBusy(false);
    }
}

function parseAssetImportMatrix(matrix, fileName) {
    if (!Array.isArray(matrix) || matrix.length < 2) {
        throw new Error(assetT('assets.fileHasNoData', 'The file has no importable data'));
    }
    const headers = matrix[0].map(assetImportColumnForHeader);
    if (!headers.some(Boolean)) {
        throw new Error(assetT('assets.headerInvalid', 'No template fields were recognized; please use the downloaded template'));
    }
    const dataRows = matrix.slice(1).filter(row => Array.isArray(row) && row.some(value => String(value ?? '').trim() !== ''));
    if (!dataRows.length) throw new Error(assetT('assets.fileHasNoData', 'The file has no importable data'));
    if (dataRows.length > ASSET_IMPORT_MAX_ROWS) {
        throw new Error(assetT('assets.tooManyRows', `The data cannot exceed ${ASSET_IMPORT_MAX_ROWS} row`, { count: ASSET_IMPORT_MAX_ROWS }));
    }
    const seen = new Map();
    assetPageState.importRows = dataRows.map((row, index) => {
        const values = {};
        headers.forEach((column, columnIndex) => {
            if (column && values[column] == null) values[column] = String(row[columnIndex] ?? '').trim();
        });
        const parsed = assetImportRecord(values, index + 2);
        if (!parsed.error) {
            const asset = parsed.asset;
            const key = `${String(asset.domain || asset.ip || asset.host).toLowerCase()}|${asset.port || 0}|${asset.protocol || ''}`;
            if (seen.has(key)) parsed.error = assetT('assets.duplicateFileRow', `and row ${seen.get(key)} rows duplicated`, { row: seen.get(key) });
            else seen.set(key, parsed.rowNumber);
        }
        return parsed;
    });
    assetPageState.importFileName = fileName;
    const name = document.getElementById('asset-import-file-name');
    if (name) name.textContent = fileName;
    renderAssetImportPreview();
}

function assetImportRecord(values, rowNumber) {
    const fail = message => ({ rowNumber, values, asset: null, error: message });
    const target = values.target || values.host || values.domain || values.ip || '';
    if (!target) return fail(assetT('assets.importTargetRequired', 'Missing target address'));
    let parsed;
    try { parsed = parseAssetEditorTarget(target); }
    catch (error) { return fail(error.message); }
    const rawPort = values.port;
    const port = rawPort === '' || rawPort == null ? Number(parsed.port || 0) : Number(rawPort);
    if (!Number.isInteger(port) || port < 0 || port > 65535) return fail(assetT('assets.portInvalid', 'The port must be in 1–65535 between'));
    const ip = (values.ip || parsed.ip || '').toLowerCase();
    if (ip && !assetEditorIsIPv4(ip) && !assetEditorIsIPv6(ip)) return fail(assetT('assets.ipInvalid', 'IP Invalid address format'));
    const rawDomain = values.domain || parsed.domain || '';
    const domain = rawDomain ? assetEditorNormalizeDomain(rawDomain) : '';
    if (rawDomain && !domain) return fail(assetT('assets.domainInvalid', 'Invalid domain format'));
    const protocol = (values.protocol || parsed.protocol || '').toLowerCase();
    if (protocol && !/^[a-z][a-z0-9+.-]{0,31}$/.test(protocol)) return fail(assetT('assets.protocolInvalid', 'Invalid protocol format'));
    const statuses = assetLocalizedEnumMap({ active: 'active', inactive: 'inactive', 'Active': 'active', 'Disabled': 'inactive' }, 'status');
    const status = statuses[String(values.status || 'active').toLowerCase()];
    if (!status) return fail(assetT('assets.importStatusInvalid', 'The status only supports active or inactive'));
    const environments = assetLocalizedEnumMap({ '': '', production: 'production', staging: 'staging', testing: 'testing', development: 'development', other: 'other', 'Production': 'production', 'Pre-release': 'staging', 'Test': 'testing', 'Development': 'development', 'Other': 'other' }, 'environment');
    const environment = environments[String(values.environment || '').toLowerCase()];
    if (environment == null) return fail('Invalid environment value');
    const criticalities = assetLocalizedEnumMap({ '': '', critical: 'critical', high: 'high', medium: 'medium', low: 'low', 'Core': 'critical', 'Important': 'high', 'Normal': 'medium', 'Low': 'low' }, 'criticality');
    const criticality = criticalities[String(values.criticality || '').toLowerCase()];
    if (criticality == null) return fail('Invalid importance value');
    let projectId = '';
    let projectName = '';
    if (values.project) {
        const projectValue = String(values.project).trim().toLowerCase();
        const project = assetPageState.projects.find(item => String(item.id).toLowerCase() === projectValue || String(item.name).trim().toLowerCase() === projectValue);
        if (!project) return fail(assetT('assets.importProjectNotFound', `The project does not exist or is not accessible: ${values.project}`, { project: values.project }));
        projectId = project.id;
        projectName = project.name;
    }
    const tags = String(values.tags || '').split(/[,，;；|]/).map(tag => tag.trim()).filter(Boolean);
    if (tags.length > 30 || tags.some(tag => Array.from(tag).length > 64)) return fail(assetT('assets.importTagsInvalid', 'At most 30 tags, each up to 64 characters'));
    const asset = {
        host: values.host || parsed.host || '', ip, domain, port, protocol,
        title: values.title || '', server: values.server || '', country: values.country || '',
        province: values.province || '', city: values.city || '', responsible_person: values.responsible_person || '',
        department: values.department || '', business_system: values.business_system || '', environment, criticality, status, tags,
        project_id: projectId, source: 'manual-import'
    };
    const limits = { host: 500, title: 500, domain: 255, protocol: 255, server: 255, country: 255, province: 255, city: 255, responsible_person: 255, department: 255, business_system: 255 };
    const oversized = Object.keys(limits).find(key => Array.from(String(asset[key] || '')).length > limits[key]);
    if (oversized) return fail(assetT('assets.importFieldTooLong', `Field ${oversized} Content too long`, { field: oversized }));
    return { rowNumber, values, asset, projectName, error: '' };
}

function renderAssetImportPreview() {
    const rows = assetPageState.importRows;
    const valid = rows.filter(row => !row.error).length;
    const invalid = rows.length - valid;
    const preview = document.getElementById('asset-import-preview-section');
    if (preview) preview.hidden = false;
    const summary = document.getElementById('asset-import-summary');
    if (summary) summary.textContent = assetT('assets.importPreviewSummary', `total ${rows.length} row, ${valid} rows valid, ${invalid} rows need fixing`, { total: rows.length, valid, invalid });
    const body = document.getElementById('asset-import-preview-body');
    if (body) body.innerHTML = rows.slice(0, 100).map(row => {
        const asset = row.asset || {};
        const target = row.values.target || row.values.host || row.values.domain || row.values.ip || '-';
        const result = row.error
            ? `<span class="asset-import-result asset-import-result--error">${escapeHtml(row.error)}</span>`
            : `<span class="asset-import-result asset-import-result--ok">${escapeHtml(assetT('assets.validationPassed', 'Passed'))}</span>`;
        return `<tr class="${row.error ? 'has-error' : ''}"><td>${row.rowNumber}</td><td title="${escapeHtml(target)}">${escapeHtml(target)}</td><td>${escapeHtml(row.projectName || row.values.project || '-')}</td><td>${escapeHtml(asset.status || row.values.status || '-')}</td><td>${result}</td></tr>`;
    }).join('');
    const note = document.getElementById('asset-import-preview-note');
    if (note) note.textContent = rows.length > 100 ? assetT('assets.previewLimited', `Only the first 100 rows are shown; all will be processed on submit ${rows.length} row`, { count: rows.length }) : '';
    const submit = document.getElementById('asset-import-submit');
    if (submit) {
        submit.textContent = assetT('assets.importValidRowsCount', `Import ${valid} valid rows`, { count: valid });
        submit.disabled = valid === 0 || assetPageState.importBusy;
    }
}

async function submitAssetImport() {
    if (assetPageState.importBusy) return;
    const validRows = assetPageState.importRows.filter(row => !row.error);
    const assets = validRows.map(row => row.asset);
    if (!assets.length) return;
    setAssetImportError('');
    setAssetImportBusy(true);
    try {
        const response = await apiFetch('/api/assets/import', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ assets, source: 'manual-import', source_query: assetPageState.importFileName })
        });
        if (!response.ok) throw new Error(formatAssetImportSubmitError(await assetEditorResponseError(response), validRows));
        const result = await response.json();
        const invalid = assetPageState.importRows.length - assets.length;
        closeAssetImport(true);
        await loadAssets(1);
        if (typeof showInlineToast === 'function') {
            showInlineToast(assetT('assets.bulkImportDone', `Import complete: added ${result.created} rows, updated ${result.updated} rows, skipped ${Number(result.skipped || 0) + invalid} rows`, {
                created: result.created, updated: result.updated, skipped: Number(result.skipped || 0) + invalid
            }));
        }
    } catch (error) {
        setAssetImportError(assetT('assets.importFailed', 'Failed to store the asset') + ': ' + error.message);
    } finally {
        setAssetImportBusy(false);
    }
}

function formatAssetImportSubmitError(message, validRows) {
    const text = String(message || '').trim();
    const match = text.match(/^第\s*(\d+)\s*个资产无效[:：]\s*(.+)$/);
    if (!match) return text;
    const assetNumber = Number(match[1]);
    const row = Array.isArray(validRows) ? validRows[assetNumber - 1] : null;
    if (!row || !row.rowNumber) return text;
    return assetT('assets.importBackendRowError', `Excel Page ${row.rowNumber} rows (valid data submitted, row ${assetNumber} rows): ${match[2]}`, {
        row: row.rowNumber,
        index: assetNumber,
        message: match[2]
    });
}

async function loadAssetOverview() {
    try {
        const response = await apiFetch('/api/assets/stats?days=' + assetOverviewDays);
        if (!response.ok) throw new Error(await response.text());
        const stats = await response.json();
        ['total', 'ips', 'domains', 'ports', 'recent'].forEach(key => {
            const el = document.getElementById('asset-stat-' + key);
            if (el) el.textContent = Number(stats[key] || 0).toLocaleString();
        });
        renderAssetRecentSummary(Number(stats.recent || 0), Number(stats.total || 0));
        renderAssetTrendCharts(stats.asset_trend || [], stats.risk_trend || []);
        renderAssetCoverage(stats.coverage || {}, Number(stats.total || 0));
        renderAssetProtocolChart(stats.protocols || [], Number(stats.total || 0));
    } catch (error) {
        console.error('Failed to load the asset overview:', error);
        if (typeof showInlineToast === 'function') showInlineToast(assetT('assets.loadFailed', 'Failed to load assets') + ': ' + error.message);
    }
}

function setAssetOverviewPeriod(days) {
    const normalized = [7, 30, 90].includes(Number(days)) ? Number(days) : 30;
    if (normalized === assetOverviewDays) return;
    assetOverviewDays = normalized;
    document.querySelectorAll('.asset-period-switch button').forEach(button => {
        const active = Number(button.dataset.days) === normalized;
        button.classList.toggle('active', active);
        button.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
    loadAssetOverview();
}

function renderAssetRecentSummary(recent, total) {
    const percent = total ? Math.min(100, Math.round(recent / total * 100)) : 0;
    const bar = document.getElementById('asset-recent-progress');
    const caption = document.getElementById('asset-recent-rate');
    if (bar) bar.style.width = percent + '%';
    if (caption) caption.textContent = assetT('assets.recentShare', `of the current total assets ${percent}%`, { percent });
}

function renderAssetTrendCharts(assetTrend, riskTrend) {
    renderAssetLineChart('asset-growth-chart', 'asset-growth-summary', assetTrend, [
        { key: 'added', label: assetT('assets.addedAssets', 'Add asset'), color: '#3b82f6', fill: 'rgba(59,130,246,.13)' },
        { key: 'inactive', label: assetT('assets.inactiveAssets', 'Disable asset'), color: '#8b5cf6' }
    ]);
    renderAssetLineChart('asset-risk-chart', 'asset-risk-summary', riskTrend, [
        { key: 'discovered', label: assetT('assets.discoveredRisks', 'Add vulnerability'), color: '#f59e0b', fill: 'rgba(245,158,11,.12)' },
        { key: 'high_risk', label: assetT('assets.highRisks', 'High and critical'), color: '#ef4444' }
    ]);
}

function renderAssetLineChart(rootId, summaryId, points, series) {
    const root = document.getElementById(rootId);
    if (!root) return;
    const data = Array.isArray(points) ? points : [];
    const totals = series.map(item => data.reduce((sum, point) => sum + Number(point[item.key] || 0), 0));
    const summaryRoot = document.getElementById(summaryId);
    const summaryContent = series.map((item, index) => `<div><span>${escapeHtml(item.label)}</span><strong style="color:${item.color}">${totals[index].toLocaleString()}</strong></div>`).join('');
    if (summaryRoot) summaryRoot.innerHTML = summaryContent;
    if (!data.length) {
        root.innerHTML = '<div class="muted">' + escapeHtml(assetT('common.noData', 'No data')) + '</div>';
        return;
    }
    const width = 680, height = 220, left = 34, right = 12, top = 18, bottom = 28;
    const plotWidth = width - left - right, plotHeight = height - top - bottom;
    const maxValue = Math.max(1, ...data.flatMap(point => series.map(item => Number(point[item.key] || 0))));
    const x = index => left + (data.length === 1 ? plotWidth / 2 : index / (data.length - 1) * plotWidth);
    const y = value => top + plotHeight - (Number(value || 0) / maxValue * plotHeight);
    const grid = [0, .25, .5, .75, 1].map(ratio => {
        const gy = top + plotHeight * ratio;
        const label = Math.round(maxValue * (1 - ratio));
        return `<line x1="${left}" y1="${gy}" x2="${width - right}" y2="${gy}"/><text x="${left - 8}" y="${gy + 4}" text-anchor="end">${label}</text>`;
    }).join('');
    const paths = series.map(item => {
        const coords = data.map((point, index) => `${x(index).toFixed(1)},${y(point[item.key]).toFixed(1)}`);
        const line = `<polyline points="${coords.join(' ')}" fill="none" stroke="${item.color}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`;
        if (!item.fill || !coords.length) return line;
        const area = `M ${x(0).toFixed(1)} ${top + plotHeight} L ${coords.join(' L ')} L ${x(data.length - 1).toFixed(1)} ${top + plotHeight} Z`;
        return `<path d="${area}" fill="${item.fill}"/>${line}`;
    }).join('');
    const labelIndexes = [...new Set([0, Math.floor((data.length - 1) / 2), data.length - 1])];
    const labels = labelIndexes.map(index => `<text x="${x(index)}" y="${height - 7}" text-anchor="${index === 0 ? 'start' : index === data.length - 1 ? 'end' : 'middle'}">${escapeHtml(String(data[index].date || '').slice(5))}</text>`).join('');
    const aria = series.map((item, index) => `${item.label} ${totals[index]}`).join('，');
    root.innerHTML = `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${escapeHtml(aria)}" preserveAspectRatio="none"><g class="asset-chart-grid">${grid}${labels}</g>${paths}</svg>`;
}

function renderAssetCoverage(coverage, total) {
    const rate = Math.max(0, Math.min(100, Number(coverage.rate || 0)));
    const recentRate = Math.max(0, Math.min(100, Number(coverage.recent_rate || 0)));
    const values = {
        scanned: Number(coverage.scanned || 0),
        recent: Number(coverage.scanned_30d || 0),
        never: Number(coverage.never_scanned || 0),
        stale: Number(coverage.stale || 0)
    };
    const gauge = document.getElementById('asset-coverage-gauge');
    const status = document.getElementById('asset-coverage-status');
    const coverageState = total > 0 ? (rate >= 80 ? 'healthy' : rate >= 50 ? 'warning' : 'critical') : '';
    if (gauge) {
        gauge.style.setProperty('--asset-coverage-value', rate + '%');
        gauge.classList.remove('is-healthy', 'is-warning', 'is-critical');
        if (coverageState) gauge.classList.add('is-' + coverageState);
    }
    if (status) {
        status.classList.remove('is-healthy', 'is-warning', 'is-critical');
        if (coverageState) status.classList.add('is-' + coverageState);
    }
    const setText = (id, value) => { const el = document.getElementById(id); if (el) el.textContent = value; };
    setText('asset-coverage-rate', rate + '%');
    setText('asset-coverage-scanned', values.scanned.toLocaleString());
    setText('asset-coverage-recent', values.recent.toLocaleString());
    setText('asset-coverage-recent-rate', assetT('assets.coverageOfTotal', `of all assets ${recentRate}%`, { percent: recentRate }));
    setText('asset-coverage-never', values.never.toLocaleString());
    setText('asset-coverage-stale', values.stale.toLocaleString());
    setText('asset-coverage-status', total ? assetT('assets.coverageMeta', `${values.scanned} / ${total} Overwritten`, { scanned: values.scanned, total }) : '—');
    document.getElementById('asset-coverage-never')?.parentElement?.classList.toggle('has-gap', values.never > 0);
    document.getElementById('asset-coverage-stale')?.parentElement?.classList.toggle('has-gap', values.stale > 0);
}

function renderAssetProtocolChart(items, total) {
    const root = document.getElementById('asset-protocol-chart');
    const meta = document.getElementById('asset-protocol-meta');
    if (!root) return;
    if (!items.length) {
        root.innerHTML = '<div class="asset-protocol-empty"><span>—</span><p>' + escapeHtml(assetT('common.noData', 'No data')) + '</p></div>';
        if (meta) meta.textContent = assetT('assets.protocolKinds', '0 protocols', { count: 0 });
        return;
    }
    if (meta) meta.textContent = assetT('assets.protocolKinds', `${items.length} protocols`, { count: items.length });
    const visibleItems = items.slice(0, 5);
    const max = Math.max(...visibleItems.map(item => Number(item.count || 0)), 1);
    const protocolTotal = items.reduce((sum, item) => sum + Number(item.count || 0), 0);
    const colors = ['#4f7df3', '#6d5df6', '#13b8a6', '#f59e0b', '#ec4899', '#06b6d4', '#8b5cf6', '#94a3b8'];
    root.innerHTML = visibleItems.map((item, index) => {
        const count = Number(item.count || 0);
        const width = Math.max(3, Math.round(count / max * 100));
        const percent = (total || protocolTotal) ? count / (total || protocolTotal) * 100 : 0;
        const displayPercent = percent > 0 && percent < 1 ? '&lt;1%' : Math.round(percent) + '%';
        const color = colors[index % colors.length];
        return `<div class="asset-bar-row" style="--protocol-color:${color}"><span class="asset-bar-label">${escapeHtml(item.name || 'unknown')}</span><div class="asset-bar-track"><i style="width:${width}%"></i></div><strong>${count.toLocaleString()}</strong><small>${displayPercent}</small></div>`;
    }).join('');
}

const ASSET_FILTER_FIELD_IDS = [
    'asset-search', 'asset-status-filter', 'asset-project-filter', 'asset-risk-filter', 'asset-vuln-min-filter',
    'asset-protocol-filter', 'asset-port-filter', 'asset-source-filter', 'asset-tag-filter', 'asset-scan-filter',
    'asset-country-filter', 'asset-province-filter', 'asset-city-filter', 'asset-responsible-filter',
    'asset-department-filter', 'asset-business-filter', 'asset-environment-filter', 'asset-criticality-filter',
    'asset-first-seen-after-filter', 'asset-first-seen-before-filter', 'asset-last-seen-after-filter',
    'asset-last-seen-before-filter', 'asset-sort-filter'
];

function assetFilterValues() {
    return Object.fromEntries(ASSET_FILTER_FIELD_IDS.map(id => [id, document.getElementById(id)?.value || '']));
}

function nextAssetFilterDay(value) {
    if (!value) return '';
    const date = new Date(value + 'T00:00:00Z');
    date.setUTCDate(date.getUTCDate() + 1);
    return date.toISOString().slice(0, 10);
}

function buildAssetFilterParams() {
    const params = new URLSearchParams();
    const map = {
        'asset-search': 'q', 'asset-status-filter': 'status', 'asset-project-filter': 'project_id',
        'asset-risk-filter': 'risk_level', 'asset-vuln-min-filter': 'min_vulnerabilities',
        'asset-protocol-filter': 'protocol', 'asset-port-filter': 'port', 'asset-source-filter': 'source',
        'asset-tag-filter': 'tag', 'asset-country-filter': 'country', 'asset-province-filter': 'province',
        'asset-city-filter': 'city', 'asset-responsible-filter': 'responsible_person',
        'asset-department-filter': 'department', 'asset-business-filter': 'business_system',
        'asset-environment-filter': 'environment', 'asset-criticality-filter': 'criticality',
        'asset-first-seen-after-filter': 'first_seen_after', 'asset-last-seen-after-filter': 'last_seen_after'
    };
    Object.entries(map).forEach(([id, key]) => {
        const value = document.getElementById(id)?.value.trim() || '';
        if (value) params.set(key, value);
    });
    [
        ['asset-first-seen-before-filter', 'first_seen_before'],
        ['asset-last-seen-before-filter', 'last_seen_before']
    ].forEach(([id, key]) => {
        const value = nextAssetFilterDay(document.getElementById(id)?.value || '');
        if (value) params.set(key, value);
    });
    const scan = document.getElementById('asset-scan-filter')?.value || '';
    if (scan === 'never' || scan === 'scanned') params.set('scan_state', scan);
    else if (/^\d+$/.test(scan)) params.set('scan_overdue_days', scan);
    const [sortBy, sortOrder] = (document.getElementById('asset-sort-filter')?.value || 'last_seen_at:desc').split(':');
    if (sortBy) params.set('sort_by', sortBy);
    if (sortOrder) params.set('sort_order', sortOrder);
    return params;
}

function toggleAssetAdvancedFilters() {
    const panel = document.getElementById('asset-advanced-filters');
    const button = document.getElementById('asset-advanced-toggle');
    if (!panel) return;
    panel.hidden = !panel.hidden;
    if (button) button.setAttribute('aria-expanded', String(!panel.hidden));
}

function resetAssetFilters() {
    ASSET_FILTER_FIELD_IDS.forEach(id => {
        const el = document.getElementById(id);
        if (!el) return;
        el.value = id === 'asset-sort-filter' ? 'last_seen_at:desc' : '';
        if (el.tagName === 'SELECT') syncAssetSelect(el);
    });
    const saved = document.getElementById('asset-saved-view-select');
    if (saved) saved.value = '';
    clearAssetSelection();
    loadAssets(1);
}

function readAssetSavedViews() {
    try {
        const views = JSON.parse(localStorage.getItem(ASSET_SAVED_VIEWS_KEY) || '[]');
        return Array.isArray(views) ? views : [];
    } catch (error) {
        return [];
    }
}

function renderAssetSavedViews(selectedName) {
    const select = document.getElementById('asset-saved-view-select');
    if (!select) return;
    const views = readAssetSavedViews();
    select.innerHTML = `<option value="">${escapeHtml(assetT('assets.savedViews', 'Saved filter views'))}</option>` + views.map(view => `<option value="${escapeHtml(view.name)}">${escapeHtml(view.name)}</option>`).join('');
    select.value = selectedName || '';
    syncAssetSelect(select);
}

function saveCurrentAssetView() {
    const name = prompt('Please enter a filter-view name');
    if (!name || !name.trim()) return;
    const cleanName = name.trim().slice(0, 60);
    const views = readAssetSavedViews().filter(view => view.name !== cleanName);
    views.push({ name: cleanName, values: assetFilterValues() });
    try { localStorage.setItem(ASSET_SAVED_VIEWS_KEY, JSON.stringify(views)); } catch (error) { return alert('Failed to save the filter view'); }
    renderAssetSavedViews(cleanName);
    if (typeof showInlineToast === 'function') showInlineToast('Filter view saved');
}

function applyAssetSavedView(name) {
    if (!name) return;
    const view = readAssetSavedViews().find(item => item.name === name);
    if (!view) return;
    Object.entries(view.values || {}).forEach(([id, value]) => {
        const el = document.getElementById(id);
        if (!el) return;
        el.value = value || '';
        if (el.tagName === 'SELECT') syncAssetSelect(el);
    });
    document.getElementById('asset-advanced-filters').hidden = false;
    document.getElementById('asset-advanced-toggle')?.setAttribute('aria-expanded', 'true');
    clearAssetSelection();
    loadAssets(1);
}

function deleteCurrentAssetView() {
    const select = document.getElementById('asset-saved-view-select');
    const name = select?.value || '';
    if (!name || !confirm(`Delete filter view${name}??`)) return;
    const views = readAssetSavedViews().filter(view => view.name !== name);
    try { localStorage.setItem(ASSET_SAVED_VIEWS_KEY, JSON.stringify(views)); } catch (error) { return; }
    renderAssetSavedViews();
}

async function loadAssets(page) {
    // 无显式页码表示进入资产库或点击顶部“刷新”，此时同步项目筛选项。
    // 翻页、搜索等带页码的操作继续复用缓存，避免重复请求项目列表。
    await ensureAssetProjects(page == null);
    assetPageState.page = Number(page || assetPageState.page || 1);
    const filterParams = buildAssetFilterParams();
    const filterQuery = filterParams.toString();
    if (assetPageState.selectionQuery && assetPageState.selectionQuery !== filterQuery) clearAssetSelection();
    assetPageState.selectionQuery = filterQuery;
    const params = new URLSearchParams(filterParams);
    params.set('page', assetPageState.page);
    params.set('page_size', assetPageState.pageSize);
    const body = document.getElementById('asset-table-body');
    if (body) body.innerHTML = '<tr><td colspan="10" class="muted">' + escapeHtml(assetT('common.loading', 'Loading...')) + '</td></tr>';
    try {
        const response = await apiFetch('/api/assets?' + params.toString());
        if (!response.ok) throw new Error(await response.text());
        const data = await response.json();
        assetPageState.items = data.assets || [];
        assetPageState.total = data.total || 0;
        assetPageState.totalPages = data.total_pages || 1;
        assetPageState.page = data.page || 1;
        if (assetPageState.page > assetPageState.totalPages) {
            return loadAssets(assetPageState.totalPages);
        }
        renderAssetRows();
        updateAssetSelectionUI();
        renderAssetPagination();
        const meta = document.getElementById('asset-list-meta');
        if (meta) meta.textContent = assetT('assets.totalMeta', `total ${data.total || 0} rows`, { count: data.total || 0 });
    } catch (error) {
        console.error('Failed to load assets:', error);
        assetPageState.items = [];
        assetPageState.total = 0;
        assetPageState.totalPages = 1;
        if (body) body.innerHTML = '<tr><td colspan="10" class="muted">' + escapeHtml(assetT('assets.loadFailed', 'Failed to load assets')) + '</td></tr>';
        renderAssetPagination();
    }
}

function assetTargetLabel(asset) {
    return asset.host || asset.domain || asset.ip || '-';
}

function assetRiskPresentation(level) {
    const normalized = ['critical', 'high', 'medium', 'low', 'info', 'normal'].includes(level) ? level : 'unassessed';
    const labels = {
        critical: assetT('assets.riskCritical', 'Critical'),
        high: assetT('assets.riskHigh', 'High'),
        medium: assetT('assets.riskMedium', 'Medium'),
        low: assetT('assets.riskLow', 'Low'),
        info: assetT('assets.riskInfo', 'Hint'),
        normal: assetT('assets.riskNormal', 'Normal'),
        unassessed: assetT('assets.riskUnassessed', 'Not assessed')
    };
    return { level: normalized, label: labels[normalized] };
}

function assetOwnershipMarkup(asset) {
    const owner = String(asset.responsible_person || '').trim();
    const department = String(asset.department || '').trim();
    if (!owner && !department) {
        return `<span class="asset-ownership-empty">${escapeHtml(assetT('assets.unassigned', 'Unassigned'))}</span>`;
    }
    const ownerLabel = assetT('assets.responsiblePerson', 'Owner');
    const departmentLabel = assetT('assets.department', 'Department');
    const title = [
        owner ? `${ownerLabel}: ${owner}` : '',
        department ? `${departmentLabel}: ${department}` : ''
    ].filter(Boolean).join(' · ');
    const primary = owner || department;
    return `<div class="asset-ownership" title="${escapeHtml(title)}">
        <span class="asset-ownership__primary">${escapeHtml(primary)}</span>
        ${owner && department ? `<span class="asset-ownership__secondary">${escapeHtml(department)}</span>` : ''}
    </div>`;
}

function ensureAssetOwnershipColumn() {
    const table = document.querySelector('#page-asset-library .asset-table');
    const headerRow = table?.querySelector('thead tr');
    if (!headerRow || headerRow.querySelector('[data-i18n="assets.ownership"]')) return;
    const lastScanHeader = headerRow.querySelector('[data-i18n="assets.lastScan"]');
    if (!lastScanHeader) return;
    const ownershipHeader = document.createElement('th');
    ownershipHeader.setAttribute('data-i18n', 'assets.ownership');
    ownershipHeader.textContent = assetT('assets.ownership', 'Owner');
    headerRow.insertBefore(ownershipHeader, lastScanHeader);
    table.querySelectorAll('#asset-table-body td[colspan="9"]').forEach(cell => cell.setAttribute('colspan', '10'));
}

function renderAssetRows() {
    const body = document.getElementById('asset-table-body');
    if (!body) return;
    if (!assetPageState.items.length) {
        body.innerHTML = '<tr><td colspan="10" class="muted">' + escapeHtml(assetT('common.noData', 'No data')) + '</td></tr>';
        return;
    }
    body.innerHTML = assetPageState.items.map((asset, index) => {
        const service = [asset.protocol, asset.port ? ':' + asset.port : ''].join('') || '-';
        const targetHint = [asset.host, asset.ip, asset.domain].filter(Boolean).filter((value, i, values) => values.indexOf(value) === i).join(' · ');
        const lastScan = asset.last_scan_at ? new Date(asset.last_scan_at).toLocaleString() : '-';
        const vulnerabilityCount = Number(asset.vulnerability_count || 0);
        const risk = assetRiskPresentation(asset.risk_level);
        const statusLabel = asset.status === 'inactive' ? assetT('assets.statusInactive', 'Disabled') : assetT('assets.statusActive', 'Active');
        return `<tr>
            <td class="asset-check-cell"><input type="checkbox" class="theme-checkbox" ${assetPageState.selected.has(asset.id) ? 'checked' : ''} onchange="toggleAssetSelection(${index},this.checked)" aria-label="${escapeHtml(assetT('assets.selectAsset', 'Select asset'))}"></td>
            <td><button class="asset-target-link" title="${escapeHtml(targetHint)}" onclick="openAssetDetail(${index})">${escapeHtml(assetTargetLabel(asset))}</button></td>
            <td><span class="asset-service" title="${escapeHtml(service)}">${escapeHtml(service)}</span></td>
            <td>${asset.project_name ? `<span class="asset-project-badge" title="${escapeHtml(asset.project_name)}">${escapeHtml(asset.project_name)}</span>` : '<span class="muted">-</span>'}</td>
            <td>${assetOwnershipMarkup(asset)}</td>
            <td>${escapeHtml(lastScan)}</td><td>${vulnerabilityCount > 0 ? `<button class="asset-vulnerability-link" onclick="openAssetVulnerabilities(${index})">${vulnerabilityCount}</button>` : '<span class="muted">0</span>'}</td>
            <td><span class="asset-risk asset-risk--${risk.level}">${escapeHtml(risk.label)}</span></td>
            <td><span class="asset-status asset-status--${escapeHtml(asset.status || 'active')}">${escapeHtml(statusLabel)}</span></td>
            <td class="asset-row-actions"><button class="btn-link" onclick="openAssetScanModal('chat',${index})">${escapeHtml(assetT('assets.sendToChatShort', 'Scan'))}</button><button class="btn-link" data-require-permission="asset:write" onclick="openAssetEditor(${index})">${escapeHtml(assetT('common.edit', 'Edit'))}</button><button class="btn-link asset-delete" data-require-permission="asset:delete" onclick="deleteAsset(${index})">${escapeHtml(assetT('common.delete', 'Delete'))}</button></td>
        </tr>`;
    }).join('');
    if (typeof applyRBACToUI === 'function') applyRBACToUI(body);
}

function toggleAssetSelection(index, checked) {
    const asset = assetPageState.items[Number(index)];
    if (!asset) return;
    if (checked) assetPageState.selected.set(asset.id, asset);
    else {
        assetPageState.selected.delete(asset.id);
        assetPageState.allMatchingSelected = false;
    }
    updateAssetSelectionUI();
}

function toggleAssetPageSelection(checked) {
    assetPageState.items.forEach(asset => {
        if (checked) assetPageState.selected.set(asset.id, asset);
        else assetPageState.selected.delete(asset.id);
    });
    if (!checked) assetPageState.allMatchingSelected = false;
    renderAssetRows();
    updateAssetSelectionUI();
}

function clearAssetSelection() {
    assetPageState.selected.clear();
    assetPageState.allMatchingSelected = false;
    closeAssetBatchMenu();
    renderAssetRows();
    updateAssetSelectionUI();
}

function closeAssetBatchMenu() {
    const menu = document.getElementById('asset-batch-more');
    if (menu) menu.open = false;
}

function updateAssetSelectionUI() {
    const count = assetPageState.selected.size;
    const actions = document.getElementById('asset-batch-actions');
    const label = document.getElementById('asset-selected-count');
    if (actions) actions.hidden = count === 0;
    if (label) label.textContent = assetT('assets.selectedCount', `Selected ${count} items`, { count });
    const selectAll = document.getElementById('asset-select-all-results');
    if (selectAll) {
        selectAll.hidden = count === 0 || count >= assetPageState.total || assetPageState.allMatchingSelected;
        selectAll.textContent = assetT('assets.selectAllResults', `Select all ${assetPageState.total} items`, { count: assetPageState.total });
    }
    const allSelected = document.getElementById('asset-all-results-selected');
    if (allSelected) {
        allSelected.hidden = !assetPageState.allMatchingSelected;
        allSelected.textContent = assetT('assets.allResultsSelected', `Selected from the current filter results ${count} items`, { count });
    }
    const merge = document.getElementById('asset-merge-selected');
    if (merge) {
        merge.disabled = count < 2;
        merge.setAttribute('aria-disabled', count < 2 ? 'true' : 'false');
        merge.title = count < 2
            ? assetT('assets.mergeRequiresMultiple', 'Please select at least two duplicate assets')
            : '';
    }
    const pageToggle = document.getElementById('asset-select-page');
    if (pageToggle) {
        const selectedOnPage = assetPageState.items.filter(asset => assetPageState.selected.has(asset.id)).length;
        pageToggle.checked = assetPageState.items.length > 0 && selectedOnPage === assetPageState.items.length;
        pageToggle.indeterminate = selectedOnPage > 0 && selectedOnPage < assetPageState.items.length;
    }
}

async function selectAllMatchingAssets() {
    const button = document.getElementById('asset-select-all-results');
    if (button) button.disabled = true;
    try {
        const params = buildAssetFilterParams();
        const response = await apiFetch('/api/assets/selection?' + params.toString());
        if (!response.ok) throw new Error(await assetEditorResponseError(response));
        const data = await response.json();
        assetPageState.selected.clear();
        (data.assets || []).forEach(asset => assetPageState.selected.set(asset.id, asset));
        assetPageState.allMatchingSelected = true;
        renderAssetRows();
        updateAssetSelectionUI();
    } catch (error) {
        alert('Failed to select all results: ' + error.message);
    } finally {
        if (button) button.disabled = false;
    }
}

async function openAssetProjectModal() {
    const assets = Array.from(assetPageState.selected.values());
    if (!assets.length) {
        alert(assetT('assets.selectAssetsFirst', 'Please select an asset first'));
        return;
    }
    await ensureAssetProjects(true);
    populateAssetProjectSelects();
    const select = document.getElementById('asset-batch-project');
    const projectIds = Array.from(new Set(assets.map(asset => asset.project_id || '')));
    if (select) {
        select.value = projectIds.length === 1 && projectIds[0] ? projectIds[0] : '';
        if (typeof syncSettingsCustomSelect === 'function') syncSettingsCustomSelect(select);
    }
    const subtitle = document.getElementById('asset-project-subtitle');
    if (subtitle) subtitle.textContent = assetT('assets.bindProjectCount', `will update ${assets.length} assets`, { count: assets.length });
    if (typeof openAppModal === 'function') openAppModal('asset-project-modal', { display: 'flex' });
    else document.getElementById('asset-project-modal').style.display = 'flex';
    if (select) select.focus();
}

function closeAssetProjectModal() {
    closeAssetCustomSelects();
    if (typeof closeAppModal === 'function') closeAppModal('asset-project-modal');
    else document.getElementById('asset-project-modal').style.display = 'none';
}

async function submitAssetProjectBinding() {
    const ids = Array.from(assetPageState.selected.keys());
    if (!ids.length) return;
    const selectedProject = document.getElementById('asset-batch-project')?.value || '';
    if (!selectedProject) {
        alert(assetT('assets.selectProjectRequired', 'Please select a project to bind'));
        return;
    }
    const projectId = selectedProject;
    const button = document.getElementById('asset-project-submit');
    if (button) button.disabled = true;
    try {
        const response = await apiFetch('/api/assets/project-binding', {
            method: 'PUT', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ asset_ids: ids, project_id: projectId })
        });
        if (!response.ok) throw new Error(await assetEditorResponseError(response));
        closeAssetProjectModal();
        clearAssetSelection();
        await loadAssets(assetPageState.page);
        if (typeof showInlineToast === 'function') {
            showInlineToast(assetT('assets.bindProjectDone', `Bound ${ids.length} assets`, { count: ids.length }));
        }
    } catch (error) {
        alert(assetT('assets.bindProjectFailed', 'Failed to bind the project') + ': ' + error.message);
    } finally {
        if (button) button.disabled = false;
    }
}

function openAssetBulkEdit() {
    const count = assetPageState.selected.size;
    if (!count) return alert('Please select an asset first');
    ['asset-bulk-status', 'asset-bulk-responsible', 'asset-bulk-department', 'asset-bulk-business', 'asset-bulk-environment', 'asset-bulk-criticality', 'asset-bulk-add-tags', 'asset-bulk-remove-tags'].forEach(id => {
        const el = document.getElementById(id);
        if (el) el.value = '';
    });
    document.getElementById('asset-bulk-edit-subtitle').textContent = `will modify ${count} assets; blank fields keep their original values`;
    ['asset-bulk-status', 'asset-bulk-environment', 'asset-bulk-criticality'].forEach(syncAssetSelect);
    if (typeof openAppModal === 'function') openAppModal('asset-bulk-edit-modal');
    else document.getElementById('asset-bulk-edit-modal').style.display = 'flex';
}

function closeAssetBulkEdit() {
    closeAssetCustomSelects();
    if (typeof closeAppModal === 'function') closeAppModal('asset-bulk-edit-modal');
    else document.getElementById('asset-bulk-edit-modal').style.display = 'none';
}

function assetCommaValues(id) {
    return (document.getElementById(id)?.value || '').split(/[,，]/).map(value => value.trim()).filter(Boolean);
}

async function submitAssetBulkEdit() {
    const ids = Array.from(assetPageState.selected.keys());
    if (!ids.length) return;
    const body = { asset_ids: ids, add_tags: assetCommaValues('asset-bulk-add-tags'), remove_tags: assetCommaValues('asset-bulk-remove-tags') };
    const fields = {
        status: 'asset-bulk-status', responsible_person: 'asset-bulk-responsible', department: 'asset-bulk-department',
        business_system: 'asset-bulk-business', environment: 'asset-bulk-environment', criticality: 'asset-bulk-criticality'
    };
    Object.entries(fields).forEach(([key, id]) => {
        const value = document.getElementById(id)?.value.trim() || '';
        if (value) body[key] = value;
    });
    if (Object.keys(body).length === 3 && body.add_tags.length === 0 && body.remove_tags.length === 0) return alert('Please fill in at least one change');
    const button = document.getElementById('asset-bulk-edit-submit');
    if (button) button.disabled = true;
    try {
        const response = await apiFetch('/api/assets/bulk', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (!response.ok) throw new Error(await assetEditorResponseError(response));
        closeAssetBulkEdit();
        clearAssetSelection();
        await loadAssets(assetPageState.page);
        if (typeof showInlineToast === 'function') showInlineToast(`Updated ${ids.length} assets`);
    } catch (error) {
        alert('Bulk edit failed: ' + error.message);
    } finally {
        if (button) button.disabled = false;
    }
}

function assetExportRows(assets) {
    return assets.map(asset => ({
        target: assetTargetLabel(asset), host: asset.host || '', ip: asset.ip || '', domain: asset.domain || '', port: asset.port || '',
        protocol: asset.protocol || '', title: asset.title || '', server: asset.server || '', project: asset.project_name || '',
        responsible_person: asset.responsible_person || '', department: asset.department || '', business_system: asset.business_system || '',
        environment: asset.environment || '', criticality: asset.criticality || '', country: asset.country || '', province: asset.province || '',
        city: asset.city || '', source: asset.source || '', status: asset.status || '', tags: (asset.tags || []).join(','),
        risk_level: asset.risk_level || '', vulnerability_count: Number(asset.vulnerability_count || 0),
        first_seen_at: asset.first_seen_at || '', last_seen_at: asset.last_seen_at || '', last_scan_at: asset.last_scan_at || ''
    }));
}

function assetCsvCell(value) {
    const text = String(value == null ? '' : value);
    return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

function downloadAssetBlob(blob, filename) {
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = filename;
    link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 0);
}

function exportSelectedAssets(format) {
    const rows = assetExportRows(Array.from(assetPageState.selected.values()));
    if (!rows.length) return alert('Please select an asset first');
    const stamp = new Date().toISOString().slice(0, 10);
    if (format === 'csv') {
        const headers = Object.keys(rows[0]);
        const csv = '\uFEFF' + [headers.map(key => assetCsvCell(assetExportHeader(key))).join(','), ...rows.map(row => headers.map(key => assetCsvCell(assetExportValue(key, row[key]))).join(','))].join('\r\n');
        downloadAssetBlob(new Blob([csv], { type: 'text/csv;charset=utf-8' }), `assets-${stamp}.csv`);
        return;
    }
    if (!window.XLSX) return alert('The table component failed to load; please refresh and retry');
    const workbook = XLSX.utils.book_new();
    const sheet = XLSX.utils.aoa_to_sheet([Object.keys(rows[0]).map(assetExportHeader), ...rows.map(row => Object.keys(row).map(key => assetExportValue(key, row[key])))]);
    sheet['!autofilter'] = { ref: sheet['!ref'] };
    sheet['!cols'] = Object.keys(rows[0]).map(key => ({ wch: ['target', 'host', 'title'].includes(key) ? 30 : 16 }));
    XLSX.utils.book_append_sheet(workbook, sheet, 'Assets');
    XLSX.writeFile(workbook, `assets-${stamp}.xlsx`);
}

async function deleteSelectedAssets() {
    const ids = Array.from(assetPageState.selected.keys());
    if (!ids.length || !confirm(`Are you sure you want to permanently delete the selected ${ids.length} assets??`)) return;
    try {
        const response = await apiFetch('/api/assets/batch-delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ asset_ids: ids }) });
        if (!response.ok) throw new Error(await assetEditorResponseError(response));
        clearAssetSelection();
        await loadAssets(1);
        if (typeof showInlineToast === 'function') showInlineToast(`Deleted ${ids.length} assets`);
    } catch (error) {
        alert('Bulk delete failed: ' + error.message);
    }
}

async function mergeSelectedAssets() {
    const assets = Array.from(assetPageState.selected.values());
    if (assets.length < 2) return alert('Please select at least two duplicate assets');
    const primary = assets[0];
    if (!confirm(`will keep${assetTargetLabel(primary)}as the primary asset and merge the rest ${assets.length - 1} assets. Continue??`)) return;
    try {
        const response = await apiFetch('/api/assets/merge', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ asset_ids: assets.map(asset => asset.id), primary_id: primary.id })
        });
        if (!response.ok) throw new Error(await assetEditorResponseError(response));
        clearAssetSelection();
        await loadAssets(1);
        if (typeof showInlineToast === 'function') showInlineToast(`Merged ${assets.length} duplicate assets`);
    } catch (error) {
        alert('Failed to merge assets: ' + error.message);
    }
}

function assetScanPromptDefault() {
    const placeholders = { asset_id: '{{asset_id}}', target: '{{target}}', host: '{{host}}', ip: '{{ip}}', domain: '{{domain}}', port: '{{port}}' };
    return assetT('assets.defaultScanPrompt', 'For the asset {{target}} (AssetID: {{asset_id}})Run an authorized security scan, prioritizing exposed services, known vulnerabilities, weak credentials, and common web risks; save confirmed vulnerabilities with record_vulnerability, and when done call complete_asset_scan(id={{asset_id}}) Writes back the last scan time and related vulnerabilities.', placeholders);
}

function openAssetScanModal(mode, index) {
    const one = Number.isInteger(index) ? assetPageState.items[index] : null;
    const assets = one ? [one] : Array.from(assetPageState.selected.values());
    if (!assets.length) {
        alert(assetT('assets.selectAssetsFirst', 'Please select an asset first'));
        return;
    }
    assetPageState.scanMode = mode === 'task' ? 'task' : 'chat';
    assetPageState.scanAssets = assets;
    const taskMode = assetPageState.scanMode === 'task';
    document.getElementById('asset-scan-title').textContent = taskMode ? assetT('assets.createScanTask', 'Create scan task') : assetT('assets.sendToChat', 'Send to conversation');
    document.getElementById('asset-scan-subtitle').textContent = assetT('assets.scanAssetCount', `${assets.length} assets`, { count: assets.length });
    document.getElementById('asset-scan-targets').innerHTML = assets.slice(0, 12).map(asset => `<span class="asset-scan-target-chip">${escapeHtml(assetTargetLabel(asset))}</span>`).join('') + (assets.length > 12 ? `<span class="muted">+${assets.length - 12}</span>` : '');
    document.getElementById('asset-scan-prompt').value = assetScanPromptDefault();
    document.getElementById('asset-scan-hint').textContent = assetT('assets.promptHint', 'available {{asset_id}}, {{target}}, {{host}}, {{ip}}, {{domain}}, {{port}} placeholder; when creating tasks, one task is generated per asset.', { asset_id: '{{asset_id}}', target: '{{target}}', host: '{{host}}', ip: '{{ip}}', domain: '{{domain}}', port: '{{port}}' });
    const executeWrap = document.getElementById('asset-scan-execute-wrap');
    executeWrap.hidden = !taskMode;
    document.getElementById('asset-scan-submit').textContent = taskMode ? assetT('assets.confirmCreate', 'Create task') : assetT('assets.confirmSend', 'Confirm send');
    if (typeof openAppModal === 'function') openAppModal('asset-scan-modal');
    else document.getElementById('asset-scan-modal').style.display = 'flex';
}

function closeAssetScanModal() {
    if (typeof closeAppModal === 'function') closeAppModal('asset-scan-modal');
    else document.getElementById('asset-scan-modal').style.display = 'none';
}

function renderAssetScanPrompt(template, asset) {
    const values = {
        asset_id: asset.id || '', target: assetTargetLabel(asset), host: asset.host || '', ip: asset.ip || '', domain: asset.domain || '', port: asset.port || ''
    };
    const prompt = Object.keys(values).reduce((text, key) => text.replaceAll(`{{${key}}}`, String(values[key])), template);
    if (!asset.id || prompt.includes('complete_asset_scan')) return prompt;
    const completion = assetT('assets.scanCompletionInstruction', 'Stop probing once the task goals above are met, report the existing results truthfully, and call complete_asset_scan(id={{asset_id}}) Writes back this asset-scan time and the associated results. This recording action does not expand the probing scope above.', { asset_id: String(asset.id) });
    return `${prompt}\n\n${completion}`;
}

function commonAssetProjectId(assets) {
    const ids = Array.from(new Set(assets.map(asset => asset.project_id || '')));
    return ids.length === 1 ? ids[0] : '';
}

async function recordAssetScanLinks(scans) {
    const response = await apiFetch('/api/assets/scan-links', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ scans }) });
    if (!response.ok) throw new Error(await response.text());
}

async function submitAssetScan() {
    const assets = assetPageState.scanAssets.slice();
    const template = document.getElementById('asset-scan-prompt').value.trim();
    if (!assets.length || !template) {
        alert(assetT('assets.promptRequired', 'Please enter a user prompt'));
        return;
    }
    const button = document.getElementById('asset-scan-submit');
    button.disabled = true;
    try {
        if (assetPageState.scanMode === 'task') {
            await createAssetScanTasks(assets, template);
        } else {
            await sendAssetsToChat(assets, template);
        }
        closeAssetScanModal();
        clearAssetSelection();
        await loadAssets(assetPageState.page);
    } catch (error) {
        console.error('Failed to submit the asset scan:', error);
        alert(assetT('assets.scanSubmitFailed', 'Failed to submit the scan') + ': ' + error.message);
    } finally {
        button.disabled = false;
    }
}

async function sendAssetsToChat(assets, template) {
    const targets = assets.map(assetTargetLabel).join(', ');
    const message = assets.map(asset => renderAssetScanPrompt(template, asset)).join('\n\n---\n\n');
    const response = await apiFetch('/api/conversations', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title: assetT('assets.scanConversationTitle', `Asset scan: ${targets}`, { targets }), projectId: commonAssetProjectId(assets) }) });
    if (!response.ok) throw new Error(await response.text());
    const conversation = await response.json();
    await recordAssetScanLinks(assets.map(asset => ({ asset_id: asset.id, conversation_id: conversation.id })));
    switchPage('chat');
    await loadConversation(conversation.id);
    const input = document.getElementById('chat-input');
    input.value = message;
    if (typeof adjustTextareaHeight === 'function') adjustTextareaHeight(input);
    // The message stream may last a long time; sending returns immediately so the submit dialog closes right away.
    window.__csNextChatFinalizationPolicy = { requireExecutionEvidence: true };
    void sendMessage();
}

async function createAssetScanTasks(assets, template) {
    const tasks = assets.map(asset => renderAssetScanPrompt(template, asset));
    const executeNow = !!document.getElementById('asset-scan-execute-now').checked;
    const response = await apiFetch('/api/batch-tasks', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title: assetT('assets.scanQueueTitle', 'Asset bulk scan'), tasks, executeNow, projectId: commonAssetProjectId(assets), concurrency: 1, agentMode: 'eino_single', scheduleMode: 'manual' }) });
    if (!response.ok) throw new Error(await response.text());
    const result = await response.json();
    const queueTasks = result.queue && Array.isArray(result.queue.tasks) ? result.queue.tasks : [];
    if (queueTasks.length !== assets.length) throw new Error(assetT('assets.scanTaskLinkFailed', 'The task was created, but asset association failed'));
    await recordAssetScanLinks(assets.map((asset, index) => ({ asset_id: asset.id, queue_id: result.queueId, task_id: queueTasks[index].id })));
    switchPage('tasks');
    if (typeof showBatchQueueDetail === 'function') showBatchQueueDetail(result.queueId);
}

function openAssetVulnerabilities(index) {
    const asset = assetPageState.items[Number(index)];
    if (!asset) return;
    if (asset.last_scan_task_id) {
        window.location.hash = `vulnerabilities?task_id=${encodeURIComponent(asset.last_scan_task_id)}`;
        return;
    }
    window.location.hash = asset.last_scan_conversation_id
        ? `vulnerabilities?conversation_id=${encodeURIComponent(asset.last_scan_conversation_id)}`
        : 'vulnerabilities';
}

function renderAssetPagination() {
    const root = document.getElementById('asset-pagination');
    if (!root) return;
    const page = assetPageState.page;
    const totalPages = assetPageState.totalPages || 1;
    const total = assetPageState.total || 0;
    const pageSize = assetPageState.pageSize;
    const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const end = total === 0 ? 0 : Math.min(page * pageSize, total);
    const atFirst = page <= 1 || total === 0;
    const atLast = page >= totalPages || total === 0;
    root.innerHTML = `<div class="pagination">
        <div class="pagination-info">
            <span>${escapeHtml(assetT('skillsPage.paginationShow', `Show ${start}-${end} / total ${total} rows`, { start, end, total }))}</span>
            <label class="pagination-page-size">${escapeHtml(assetT('skillsPage.perPageLabel', 'Per page'))}
                <select id="asset-page-size-pagination" onchange="changeAssetPageSize()">
                    ${[10, 20, 50, 100].map(size => `<option value="${size}" ${size === pageSize ? 'selected' : ''}>${size}</option>`).join('')}
                </select>
            </label>
        </div>
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="loadAssets(1)" ${atFirst ? 'disabled' : ''}>${escapeHtml(assetT('skillsPage.firstPage', 'First'))}</button>
            <button class="btn-secondary" onclick="loadAssets(${Math.max(1, page - 1)})" ${atFirst ? 'disabled' : ''}>${escapeHtml(assetT('skillsPage.prevPage', 'Previous'))}</button>
            <span class="pagination-page">${escapeHtml(assetT('skillsPage.pageOf', `Page ${page} / ${totalPages} pages`, { current: page, total: totalPages }))}</span>
            <button class="btn-secondary" onclick="loadAssets(${Math.min(totalPages, page + 1)})" ${atLast ? 'disabled' : ''}>${escapeHtml(assetT('skillsPage.nextPage', 'Next'))}</button>
            <button class="btn-secondary" onclick="loadAssets(${totalPages})" ${atLast ? 'disabled' : ''}>${escapeHtml(assetT('skillsPage.lastPage', 'Last'))}</button>
        </div>
    </div>`;
    syncAssetSelect('asset-page-size-pagination');
}

function changeAssetPageSize() {
    const select = document.getElementById('asset-page-size-pagination');
    const size = Number(select?.value);
    if (![10, 20, 50, 100].includes(size)) return;
    assetPageState.pageSize = size;
    try { localStorage.setItem(ASSET_PAGE_SIZE_KEY, String(size)); } catch (error) { /* ignore */ }
    loadAssets(1);
}

async function ensureAssetProjects(force) {
    if (assetPageState.projectsLoaded && !force) return;
    try {
        const response = await apiFetch('/api/projects?limit=500');
        if (!response.ok) throw new Error(await response.text());
        const data = await response.json();
        assetPageState.projects = data.projects || [];
        assetPageState.projectsLoaded = true;
        populateAssetProjectSelects();
    } catch (error) {
        console.warn('Failed to load the asset project options:', error);
        assetPageState.projectsLoaded = true;
    }
}

function populateAssetProjectSelects() {
    const configs = [
        ['asset-project-filter', assetT('assets.allProjects', 'All projects')],
        ['asset-edit-project', assetT('assets.unboundProject', 'Do not bind for now')]
    ];
    configs.forEach(([id, emptyLabel]) => {
        const el = document.getElementById(id);
        if (!el) return;
        const current = el.value;
        el.innerHTML = `<option value="">${escapeHtml(emptyLabel)}</option>` + assetPageState.projects.map(project => `<option value="${assetEscapeAttr(project.id)}">${escapeHtml(project.name)}${project.status === 'archived' ? '  -  ' + escapeHtml(assetT('assets.archived', 'Archived')) : ''}</option>`).join('');
        el.value = current;
        syncAssetSelect(el);
    });
    const batch = document.getElementById('asset-batch-project');
    if (batch) {
        const current = batch.value;
        batch.innerHTML = `<option value="" disabled hidden>${escapeHtml(assetT('assets.chooseProject', 'Please select a project'))}</option>` + assetPageState.projects.map(project => `<option value="${assetEscapeAttr(project.id)}">${escapeHtml(project.name)}${project.status === 'archived' ? '  -  ' + escapeHtml(assetT('assets.archived', 'Archived')) : ''}</option>`).join('');
        batch.value = current;
        syncAssetSelect(batch);
    }
}

async function openAssetEditor(indexOrAsset) {
    // Projects may be added, edited, or archived after the asset page first loads, so they are re-fetched when the editor opens, 
    // Avoids the dropdown reusing the projectsLoaded cache indefinitely so it can only be updated by a full page refresh.
    await ensureAssetProjects(true);
    const isIndex = Number.isInteger(indexOrAsset);
    const asset = isIndex ? assetPageState.items[indexOrAsset] : (indexOrAsset && typeof indexOrAsset === 'object' ? indexOrAsset : null);
    assetPageState.editIndex = isIndex && asset ? indexOrAsset : -1;
    assetPageState.editAsset = asset;
    assetPageState.editorReturnFocus = document.activeElement;
    document.getElementById('asset-edit-id').value = asset?.id || '';
    document.getElementById('asset-edit-host').value = asset?.host || '';
    document.getElementById('asset-edit-ip').value = asset?.ip || '';
    document.getElementById('asset-edit-domain').value = asset?.domain || '';
    document.getElementById('asset-edit-port').value = asset?.port || '';
    document.getElementById('asset-edit-protocol').value = asset?.protocol || '';
    document.getElementById('asset-edit-server').value = asset?.server || '';
    document.getElementById('asset-edit-project').value = asset?.project_id || '';
    document.getElementById('asset-edit-country').value = asset?.country || '';
    document.getElementById('asset-edit-province').value = asset?.province || '';
    document.getElementById('asset-edit-city').value = asset?.city || '';
    document.getElementById('asset-edit-responsible').value = asset?.responsible_person || '';
    document.getElementById('asset-edit-department').value = asset?.department || '';
    document.getElementById('asset-edit-business').value = asset?.business_system || '';
    document.getElementById('asset-edit-environment').value = asset?.environment || '';
    document.getElementById('asset-edit-criticality').value = asset?.criticality || '';
    document.getElementById('asset-edit-title-value').value = asset?.title || '';
    document.getElementById('asset-edit-tags').value = '';
    assetPageState.editorTags = Array.from(new Set((asset?.tags || []).map(value => String(value).trim()).filter(Boolean)));
    renderAssetEditorTags();
    document.getElementById('asset-edit-status').value = asset?.status || 'active';
    syncAssetSelect('asset-edit-project');
    syncAssetSelect('asset-edit-status');
    syncAssetSelect('asset-edit-environment');
    syncAssetSelect('asset-edit-criticality');
    document.getElementById('asset-edit-target').value = assetEditorTargetFromAsset(asset);
    assetPageState.editorParsedTarget = document.getElementById('asset-edit-target').value.trim();
    clearAssetEditorErrors();
    document.getElementById('asset-editor-title').textContent = asset ? assetT('assets.editAssetTitle', 'Edit asset') : assetT('assets.addAssetTitle', 'Add asset');
    const submit = document.getElementById('asset-editor-submit');
    submit.textContent = asset ? assetT('common.save', 'Save') : assetT('assets.addAssetAction', 'Add asset');
    ensureAssetEditorInteractions();
    assetPageState.editorDirty = false;
    assetPageState.editorBusy = false;
    setAssetEditorBusy(false);
    if (typeof openAppModal === 'function') openAppModal('asset-editor-modal', { focusEl: document.getElementById('asset-edit-target') });
    else document.getElementById('asset-editor-modal').style.display = 'flex';
}

function closeAssetEditor(force) {
    if (!force && assetPageState.editorDirty && !confirm(assetT('assets.discardChanges', 'Discard unsaved changes??'))) return;
    closeAssetCustomSelects();
    if (typeof closeAppModal === 'function') closeAppModal('asset-editor-modal');
    else document.getElementById('asset-editor-modal').style.display = 'none';
    const returnFocus = assetPageState.editorReturnFocus;
    assetPageState.editorDirty = false;
    if (returnFocus && typeof returnFocus.focus === 'function') requestAnimationFrame(() => returnFocus.focus());
}

function assetEditorTargetFromAsset(asset) {
    if (!asset) return '';
    if (asset.host) return String(asset.host);
    const target = asset.domain || asset.ip || '';
    if (!target) return '';
    const wrapped = String(target).includes(':') && !String(target).startsWith('[') ? `[${target}]` : target;
    return `${wrapped}${Number(asset.port || 0) > 0 ? ':' + Number(asset.port) : ''}`;
}

function assetEditorDefaultPort(protocol) {
    return ({ http: 80, https: 443, ssh: 22, ftp: 21, smtp: 25, rdp: 3389, mysql: 3306, postgresql: 5432, redis: 6379, mongodb: 27017 })[protocol] || 0;
}

function assetEditorProtocolForPort(port) {
    return ({ 80: 'http', 443: 'https', 22: 'ssh', 21: 'ftp', 25: 'smtp', 3389: 'rdp', 3306: 'mysql', 5432: 'postgresql', 6379: 'redis', 27017: 'mongodb' })[port] || '';
}

function assetEditorIsIPv4(value) {
    const parts = String(value).split('.');
    return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

function assetEditorIsIPv6(value) {
    const candidate = String(value).replace(/^\[|\]$/g, '');
    if (!candidate.includes(':') || !/^[0-9a-f:.]+$/i.test(candidate)) return false;
    try { return new URL(`http://[${candidate}]/`).hostname.length > 2; } catch (error) { return false; }
}

function assetEditorNormalizeDomain(value) {
    const raw = String(value || '').trim().replace(/\.$/, '');
    if (!raw || raw.length > 253 || raw.includes('_') || /[\/?#@]/.test(raw)) return '';
    try {
        const hostname = new URL(`http://${raw}/`).hostname.toLowerCase().replace(/\.$/, '');
        if (!hostname || hostname.length > 253 || hostname.split('.').some(label => !label || label.length > 63 || !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i.test(label))) return '';
        return hostname;
    } catch (error) { return ''; }
}

function parseAssetEditorTarget(value) {
    const raw = String(value || '').trim();
    if (!raw) throw new Error(assetT('assets.targetRequired', 'Please enter an asset address'));
    const opaqueTarget = () => ({ host: raw, ip: '', domain: '', port: 0, protocol: '' });
    let hostname = '';
    let port = 0;
    let protocol = '';
    let host = '';
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(raw)) {
        let parsed;
        try { parsed = new URL(raw); } catch (error) { return opaqueTarget(); }
        if (!parsed.hostname || parsed.username || parsed.password) return opaqueTarget();
        protocol = parsed.protocol.replace(':', '').toLowerCase();
        hostname = parsed.hostname.replace(/^\[|\]$/g, '');
        port = parsed.port ? Number(parsed.port) : assetEditorDefaultPort(protocol);
        host = raw;
    } else {
        let authority = raw.replace(/\/$/, '');
        const bracketed = authority.match(/^\[([^\]]+)](?::(\d+))?$/);
        if (bracketed) {
            hostname = bracketed[1];
            port = Number(bracketed[2] || 0);
        } else if ((authority.match(/:/g) || []).length === 1 && /:\d+$/.test(authority)) {
            const splitAt = authority.lastIndexOf(':');
            hostname = authority.slice(0, splitAt);
            port = Number(authority.slice(splitAt + 1));
        } else {
            hostname = authority;
        }
        protocol = assetEditorProtocolForPort(port);
    }
    if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error(assetT('assets.portInvalid', 'The port must be in 1–65535 between'));
    const isIP = assetEditorIsIPv4(hostname) || assetEditorIsIPv6(hostname);
    const domain = isIP ? '' : assetEditorNormalizeDomain(hostname);
    if (!isIP && (!domain || assetEditorIsIPv4(domain) || assetEditorIsIPv6(domain))) return opaqueTarget();
    return { host, ip: isIP ? hostname.toLowerCase() : '', domain, port, protocol };
}

function applyAssetEditorTarget(showError) {
    const input = document.getElementById('asset-edit-target');
    try {
        const parsed = parseAssetEditorTarget(input.value);
        document.getElementById('asset-edit-host').value = parsed.host;
        document.getElementById('asset-edit-ip').value = parsed.ip;
        document.getElementById('asset-edit-domain').value = parsed.domain;
        document.getElementById('asset-edit-port').value = parsed.port || '';
        document.getElementById('asset-edit-protocol').value = parsed.protocol;
        assetPageState.editorParsedTarget = input.value.trim();
        setAssetEditorFieldError('asset-edit-target', '');
        return parsed;
    } catch (error) {
        if (showError) setAssetEditorFieldError('asset-edit-target', error.message);
        return null;
    }
}

function setAssetEditorFieldError(inputId, message) {
    const input = document.getElementById(inputId);
    const error = document.getElementById(inputId + '-error');
    if (input) input.setAttribute('aria-invalid', message ? 'true' : 'false');
    if (error) { error.textContent = message || ''; error.hidden = !message; }
}

function setAssetEditorFormError(message) {
    const error = document.getElementById('asset-editor-form-error');
    if (!error) return;
    error.textContent = message || '';
    error.hidden = !message;
}

function clearAssetEditorErrors() {
    ['asset-edit-target', 'asset-edit-host', 'asset-edit-ip', 'asset-edit-domain', 'asset-edit-port', 'asset-edit-protocol'].forEach(id => setAssetEditorFieldError(id, ''));
    setAssetEditorFormError('');
}

function addAssetEditorTags(raw) {
    String(raw || '').split(/[,, ]/).map(value => value.trim()).filter(Boolean).forEach(tag => {
        if (!assetPageState.editorTags.includes(tag) && assetPageState.editorTags.length < 30) assetPageState.editorTags.push(tag);
    });
    document.getElementById('asset-edit-tags').value = '';
    assetPageState.editorDirty = true;
    renderAssetEditorTags();
}

function removeAssetEditorTag(index) {
    assetPageState.editorTags.splice(Number(index), 1);
    assetPageState.editorDirty = true;
    renderAssetEditorTags();
}

function renderAssetEditorTags() {
    const root = document.getElementById('asset-tag-chips');
    if (!root) return;
    root.replaceChildren(...assetPageState.editorTags.map((tag, index) => {
        const chip = document.createElement('span');
        chip.className = 'asset-tag-chip';
        const text = document.createElement('span');
        text.textContent = tag;
        const button = document.createElement('button');
        button.type = 'button';
        button.textContent = '×';
        button.setAttribute('aria-label', assetT('assets.removeTag', `Remove tag ${tag}`, { tag }));
        button.onclick = () => removeAssetEditorTag(index);
        chip.append(text, button);
        return chip;
    }));
}

function ensureAssetEditorInteractions() {
    if (assetPageState.editorInteractionsReady) return;
    assetPageState.editorInteractionsReady = true;
    const form = document.getElementById('asset-editor-form');
    const target = document.getElementById('asset-edit-target');
    const tagInput = document.getElementById('asset-edit-tags');
    form.addEventListener('input', event => {
        if (event.target !== tagInput) assetPageState.editorDirty = true;
        if (event.target === target) setAssetEditorFieldError('asset-edit-target', '');
        setAssetEditorFormError('');
    });
    target.addEventListener('blur', () => { if (target.value.trim()) applyAssetEditorTarget(true); });
    tagInput.addEventListener('keydown', event => {
        if ((event.key === 'Enter' || event.key === ',') && tagInput.value.trim()) { event.preventDefault(); addAssetEditorTags(tagInput.value); }
        else if (event.key === 'Backspace' && !tagInput.value && assetPageState.editorTags.length) removeAssetEditorTag(assetPageState.editorTags.length - 1);
    });
    tagInput.addEventListener('blur', () => { if (tagInput.value.trim()) addAssetEditorTags(tagInput.value); });
    document.addEventListener('keydown', event => {
        if (typeof isAppModalOpen !== 'function' || !isAppModalOpen('asset-editor-modal')) return;
        if (event.key === 'Escape') { event.preventDefault(); closeAssetEditor(); return; }
        if (event.key !== 'Tab') return;
        const focusable = Array.from(form.querySelectorAll('button:not([disabled]), input:not([disabled]), select:not([disabled]), summary, [tabindex]:not([tabindex="-1"])')).filter(el => !el.hidden && el.offsetParent !== null);
        if (!focusable.length) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    });
}

function collectAssetEditor() {
    const existing = assetPageState.editAsset || {};
    return {
        host: document.getElementById('asset-edit-host').value.trim(), ip: document.getElementById('asset-edit-ip').value.trim(),
        domain: document.getElementById('asset-edit-domain').value.trim(), port: Number(document.getElementById('asset-edit-port').value || 0),
        protocol: document.getElementById('asset-edit-protocol').value.trim(), server: document.getElementById('asset-edit-server').value.trim(),
        title: document.getElementById('asset-edit-title-value').value.trim(), status: document.getElementById('asset-edit-status').value,
        source: existing.source || 'manual', source_query: existing.source_query || '',
        country: document.getElementById('asset-edit-country').value.trim(), province: document.getElementById('asset-edit-province').value.trim(), city: document.getElementById('asset-edit-city').value.trim(),
        responsible_person: document.getElementById('asset-edit-responsible').value.trim(), department: document.getElementById('asset-edit-department').value.trim(),
        business_system: document.getElementById('asset-edit-business').value.trim(), environment: document.getElementById('asset-edit-environment').value,
        criticality: document.getElementById('asset-edit-criticality').value,
        project_id: document.getElementById('asset-edit-project').value,
        tags: assetPageState.editorTags.slice()
    };
}

function validateAssetEditor() {
    clearAssetEditorErrors();
    const target = document.getElementById('asset-edit-target').value.trim();
    let parsed = null;
    if (target !== assetPageState.editorParsedTarget) parsed = applyAssetEditorTarget(true);
    else {
        try { parsed = parseAssetEditorTarget(target); }
        catch (error) { setAssetEditorFieldError('asset-edit-target', error.message); }
    }
    if (!parsed) { document.getElementById('asset-edit-target').focus(); return false; }
    const failAdvanced = (id, message) => {
        setAssetEditorFieldError(id, message);
        requestAnimationFrame(() => document.getElementById(id).focus());
        return false;
    };
    const ip = document.getElementById('asset-edit-ip').value.trim();
    if (ip && !assetEditorIsIPv4(ip) && !assetEditorIsIPv6(ip)) return failAdvanced('asset-edit-ip', assetT('assets.ipInvalid', 'IP Invalid address format'));
    const domainInput = document.getElementById('asset-edit-domain');
    const domain = domainInput.value.trim();
    if (domain) {
        const normalizedDomain = assetEditorNormalizeDomain(domain);
        if (!normalizedDomain) return failAdvanced('asset-edit-domain', assetT('assets.domainInvalid', 'Invalid domain format'));
        domainInput.value = normalizedDomain;
    }
    const portInput = document.getElementById('asset-edit-port');
    if (portInput.value !== '' && (!/^\d+$/.test(portInput.value) || Number(portInput.value) < 1 || Number(portInput.value) > 65535)) {
        return failAdvanced('asset-edit-port', assetT('assets.portInvalid', 'The port must be in 1–65535 between'));
    }
    const protocol = document.getElementById('asset-edit-protocol').value.trim().toLowerCase();
    if (protocol && !/^[a-z][a-z0-9+.-]{0,31}$/.test(protocol)) return failAdvanced('asset-edit-protocol', assetT('assets.protocolInvalid', 'Invalid protocol format'));
    document.getElementById('asset-edit-protocol').value = protocol;
    return true;
}

function setAssetEditorBusy(busy) {
    assetPageState.editorBusy = Boolean(busy);
    const submit = document.getElementById('asset-editor-submit');
    if (!submit) return;
    submit.disabled = Boolean(busy);
    submit.classList.toggle('asset-editor-submit-busy', Boolean(busy));
}

async function assetEditorResponseError(response) {
    const text = await response.text();
    try { return JSON.parse(text).error || text; } catch (error) { return text; }
}

async function saveAsset() {
    if (assetPageState.editorBusy || !validateAssetEditor()) return;
    const pendingTag = document.getElementById('asset-edit-tags').value.trim();
    if (pendingTag) addAssetEditorTags(pendingTag);
    const id = document.getElementById('asset-edit-id').value;
    const asset = collectAssetEditor();
    setAssetEditorBusy(true);
    try {
        const response = id
            ? await apiFetch('/api/assets/' + encodeURIComponent(id), { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(asset) })
            : await apiFetch('/api/assets/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ assets: [asset], source: 'manual' }) });
        if (!response.ok) throw new Error(await assetEditorResponseError(response));
        const result = await response.json();
        if (!id && Number(result.skipped || 0) > 0 && Number(result.created || 0) === 0 && Number(result.updated || 0) === 0) {
            throw new Error(assetT('assets.assetSkipped', 'The asset was not saved; it may already exist and you lack update permission'));
        }
        closeAssetEditor(true);
        await loadAssets(id ? assetPageState.page : 1);
        const projectAssetsPanel = document.getElementById('project-panel-assets');
        if (projectAssetsPanel && !projectAssetsPanel.hidden && typeof window.loadProjectAssets === 'function') await window.loadProjectAssets();
        const message = id
            ? assetT('assets.updatedSuccessfully', 'Asset updated')
            : Number(result.updated || 0) > 0
                ? assetT('assets.duplicateMerged', 'The asset already exists; its info has been safely merged')
                : assetT('assets.createdSuccessfully', 'Asset added');
        if (typeof showInlineToast === 'function') showInlineToast(message);
    } catch (error) {
        setAssetEditorFormError(assetT('assets.saveFailed', 'Failed to save the asset') + ': ' + error.message);
    } finally {
        setAssetEditorBusy(false);
    }
}

async function deleteAsset(index) {
    const asset = assetPageState.items[index];
    if (!asset || !confirm(assetT('assets.deleteConfirm', 'Are you sure you want to delete this asset??'))) return;
    const response = await apiFetch('/api/assets/' + encodeURIComponent(asset.id), { method: 'DELETE' });
    if (!response.ok) {
        alert(assetT('assets.deleteFailed', 'Failed to delete the asset'));
        return;
    }
    await loadAssets(assetPageState.page);
}

function fofaResultToAsset(row, fields) {
    const value = name => {
        if (row && !Array.isArray(row) && typeof row === 'object') {
            return row[name] != null ? String(row[name]).trim() : '';
        }
        const idx = Array.isArray(fields) ? fields.indexOf(name) : -1;
        return idx >= 0 && Array.isArray(row) && row[idx] != null ? String(row[idx]).trim() : '';
    };
    const firstValue = names => {
        for (const name of names) {
            const v = value(name);
            if (v) return v;
        }
        return '';
    };
    const port = Number.parseInt(value('port'), 10);
    const rawIP = firstValue(['ip', 'ip_str']);
    const ip = assetEditorIsIPv4(rawIP) || assetEditorIsIPv6(rawIP) ? rawIP.toLowerCase() : '';
    const rawDomain = firstValue(['domain', 'hostname', 'hostnames', 'domains']);
    const domain = rawDomain && !assetEditorIsIPv4(rawDomain) && !assetEditorIsIPv6(rawDomain)
        ? assetEditorNormalizeDomain(rawDomain.split(',')[0].replace(/^\[|\]$/g, '').replace(/^"|"$/g, ''))
        : '';
    const rawProtocol = firstValue(['protocol', 'service.name', 'transport']).toLowerCase();
    return {
        host: firstValue(['host', 'hostname']), ip, port: Number.isFinite(port) ? port : 0, domain,
        protocol: /^[a-z][a-z0-9+.-]{0,31}$/.test(rawProtocol) ? rawProtocol : '', title: firstValue(['title', 'service.http.title']), server: firstValue(['server', 'product']),
        country: firstValue(['country', 'location.country_cn', 'location.country_name']), province: firstValue(['region', 'province', 'location.province_cn']), city: firstValue(['city', 'location.city_cn', 'location.city']),
        source: (window.infoCollectState?.currentPayload?.provider || 'fofa'), status: 'active'
    };
}

async function importFofaAssetsByIndexes(indexes) {
    const payload = window.infoCollectState || infoCollectState;
    const current = payload && payload.currentPayload;
    if (!current || !current.results || !indexes.length) {
        alert(assetT('assets.selectFirst', 'Please select the results to store first'));
        return;
    }
    const converted = indexes.map(index => fofaResultToAsset(current.results[index], current.fields));
    const assets = converted.filter(asset => asset.host || asset.ip || asset.domain);
    const invalidCount = converted.length - assets.length;
    if (!assets.length) {
        alert(assetT('assets.noValidImportTarget', 'The selected results have no valid asset targets to store'));
        return;
    }
    const source = current.provider || 'fofa';
    const response = await apiFetch('/api/assets/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ assets, source, source_query: current.query || '' }) });
    if (!response.ok) {
        alert(assetT('assets.importFailed', 'Failed to store the asset') + ': ' + await response.text());
        return;
    }
    const result = await response.json();
    if (typeof showInlineToast === 'function') {
        const message = invalidCount > 0
            ? assetT('assets.importDoneWithInvalid', `Added ${result.created} rows, updated ${result.updated} rows, skipped ${invalidCount} results with no valid target`, { ...result, invalid: invalidCount })
            : assetT('assets.importDone', `Added ${result.created} rows, updated ${result.updated} rows`, result);
        showInlineToast(message);
    }
}

function importSelectedFofaAssets() {
    const indexes = Array.from(infoCollectState.selectedRowIndexes || []).sort((a, b) => a - b);
    return importFofaAssetsByIndexes(indexes);
}

function importFofaRowAsset(index) {
    return importFofaAssetsByIndexes([Number(index)]);
}

function assetDetailItem(label, value, wide, options) {
    const itemClass = ['asset-detail-item'];
    if (wide) itemClass.push('asset-detail-item--wide');
    if (options?.className) itemClass.push(options.className);
    return `<div class="${itemClass.join(' ')}"><span>${escapeHtml(label)}</span><div>${value || '<span class="muted">-</span>'}</div></div>`;
}

function assetDetailBadge(label, value, modifier) {
    if (!value) return '';
    const badgeClass = modifier ? ` asset-detail-overview-badge--${modifier}` : '';
    return `<span class="asset-detail-overview-badge${badgeClass}"><span>${escapeHtml(label)}</span>${escapeHtml(value)}</span>`;
}

function assetDetailOverview(asset) {
    const service = [asset.protocol, asset.port ? ':' + asset.port : ''].join('');
    const location = [asset.country, asset.province, asset.city].filter(Boolean).join(' / ');
    const statusLabel = asset.status === 'inactive' ? assetT('assets.statusInactive', 'Disabled') : assetT('assets.statusActive', 'Active');
    const risk = assetRiskPresentation(asset.risk_level);
    return `<section class="asset-detail-overview">
        <div class="asset-detail-overview-main">
            <div class="asset-detail-target">${escapeHtml(assetTargetLabel(asset))}</div>
            <div class="asset-detail-meta-line">${escapeHtml([asset.ip, asset.domain, service].filter(Boolean).join(' · ') || '-')}</div>
        </div>
        <div class="asset-detail-overview-badges">
            ${assetDetailBadge(assetT('assets.status', 'Status'), statusLabel, asset.status === 'inactive' ? 'neutral' : 'success')}
            ${assetDetailBadge(assetT('assets.risk', 'Risk'), risk.label, risk.level)}
            ${assetDetailBadge(assetT('assets.source', 'Source'), asset.source || '', 'source')}
            ${assetDetailBadge(assetT('assets.location', 'Region'), location, 'location')}
        </div>
    </section>`;
}

function openAssetDetail(index) {
    const asset = assetPageState.items[Number(index)];
    assetPageState.detailIndex = Number(index);
    openAssetDetailRecord(asset);
}

function openAssetDetailRecord(asset) {
    if (!asset) return;
    assetPageState.detailAsset = asset;
    const subtitle = document.getElementById('asset-detail-subtitle');
    if (subtitle) subtitle.textContent = assetTargetLabel(asset);
    const tags = (asset.tags || []).map(tag => `<span class="asset-tag">${escapeHtml(tag)}</span>`).join('');
    const values = [
        assetDetailItem(assetT('assets.project', 'Project'), escapeHtml(asset.project_name || '')),
        assetDetailItem(assetT('assets.status', 'Status'), escapeHtml(asset.status === 'inactive' ? assetT('assets.statusInactive', 'Disabled') : assetT('assets.statusActive', 'Active'))),
        assetDetailItem('Host', escapeHtml(asset.host || '')),
        assetDetailItem('IP', escapeHtml(asset.ip || '')),
        assetDetailItem(assetT('assets.domain', 'Domain'), escapeHtml(asset.domain || '')),
        assetDetailItem(assetT('assets.service', 'Service'), escapeHtml([asset.protocol, asset.port ? ':' + asset.port : ''].join(''))),
        assetDetailItem(assetT('assets.title', 'Title/fingerprint'), escapeHtml(asset.title || ''), true),
        assetDetailItem(assetT('assets.server', 'Service fingerprint'), escapeHtml(asset.server || '')),
        assetDetailItem(assetT('assets.location', 'Region'), escapeHtml([asset.country, asset.province, asset.city].filter(Boolean).join(' / '))),
        assetDetailItem(assetT('assets.responsiblePerson', 'Owner'), escapeHtml(asset.responsible_person || '')),
        assetDetailItem(assetT('assets.department', 'Department'), escapeHtml(asset.department || '')),
        assetDetailItem(assetT('assets.businessSystem', 'Business system'), escapeHtml(asset.business_system || '')),
        assetDetailItem(assetT('assets.environment', 'Environment'), escapeHtml(asset.environment || '')),
        assetDetailItem(assetT('assets.criticality', 'Importance'), escapeHtml(asset.criticality || '')),
        assetDetailItem(assetT('assets.source', 'Source'), escapeHtml(asset.source || '')),
        assetDetailItem(assetT('assets.sourceQuery', 'Source query'), asset.source_query ? `<code>${escapeHtml(asset.source_query)}</code>` : '', true, { className: 'asset-detail-item--code' }),
        assetDetailItem(assetT('assets.tagsLabel', 'Tag'), tags, true),
        assetDetailItem(assetT('assets.firstSeen', 'First seen'), escapeHtml(asset.first_seen_at ? new Date(asset.first_seen_at).toLocaleString() : '')),
        assetDetailItem(assetT('assets.lastSeen', 'Last seen'), escapeHtml(asset.last_seen_at ? new Date(asset.last_seen_at).toLocaleString() : '')),
        assetDetailItem(assetT('assets.lastScan', 'Last scan'), escapeHtml(asset.last_scan_at ? new Date(asset.last_scan_at).toLocaleString() : '')),
        assetDetailItem(assetT('assets.relatedVulnerabilities', 'Related vulnerabilities'), String(Number(asset.vulnerability_count || 0)))
    ];
    const grid = document.getElementById('asset-detail-grid');
    if (grid) grid.innerHTML = assetDetailOverview(asset) + `<div class="asset-detail-fields">${values.join('')}</div>`;
    if (typeof applyRBACToUI === 'function') applyRBACToUI(document.getElementById('asset-detail-modal'));
    if (typeof openAppModal === 'function') openAppModal('asset-detail-modal');
    else document.getElementById('asset-detail-modal').style.display = 'flex';
}

function closeAssetDetail() {
    if (typeof closeAppModal === 'function') closeAppModal('asset-detail-modal');
    else document.getElementById('asset-detail-modal').style.display = 'none';
}

function editAssetFromDetail() {
    const asset = assetPageState.detailAsset;
    closeAssetDetail();
    if (asset) openAssetEditor(asset);
}

window.loadAssetOverview = loadAssetOverview;
window.loadAssets = loadAssets;
window.openAssetScanModal = openAssetScanModal;
window.closeAssetScanModal = closeAssetScanModal;
window.submitAssetScan = submitAssetScan;
window.toggleAssetSelection = toggleAssetSelection;
window.toggleAssetPageSelection = toggleAssetPageSelection;
window.clearAssetSelection = clearAssetSelection;
window.closeAssetBatchMenu = closeAssetBatchMenu;
window.selectAllMatchingAssets = selectAllMatchingAssets;
window.toggleAssetAdvancedFilters = toggleAssetAdvancedFilters;
window.resetAssetFilters = resetAssetFilters;
window.saveCurrentAssetView = saveCurrentAssetView;
window.applyAssetSavedView = applyAssetSavedView;
window.deleteCurrentAssetView = deleteCurrentAssetView;
window.openAssetBulkEdit = openAssetBulkEdit;
window.closeAssetBulkEdit = closeAssetBulkEdit;
window.submitAssetBulkEdit = submitAssetBulkEdit;
window.exportSelectedAssets = exportSelectedAssets;
window.deleteSelectedAssets = deleteSelectedAssets;
window.mergeSelectedAssets = mergeSelectedAssets;
window.openAssetVulnerabilities = openAssetVulnerabilities;
window.changeAssetPageSize = changeAssetPageSize;
window.openAssetEditor = openAssetEditor;
window.closeAssetEditor = closeAssetEditor;
window.saveAsset = saveAsset;
window.deleteAsset = deleteAsset;
window.openAssetImport = openAssetImport;
window.closeAssetImport = closeAssetImport;
window.downloadAssetTemplate = downloadAssetTemplate;
window.handleAssetImportFile = handleAssetImportFile;
window.submitAssetImport = submitAssetImport;
window.importSelectedFofaAssets = importSelectedFofaAssets;
window.importFofaRowAsset = importFofaRowAsset;
window.openAssetDetail = openAssetDetail;
window.openAssetDetailRecord = openAssetDetailRecord;
window.closeAssetDetail = closeAssetDetail;
window.editAssetFromDetail = editAssetFromDetail;

document.addEventListener('DOMContentLoaded', () => {
    ensureAssetOwnershipColumn();
    initAssetCustomSelects();
    renderAssetSavedViews();
    document.addEventListener('click', event => {
        const menu = document.getElementById('asset-batch-more');
        if (menu?.open && event.target instanceof Node && !menu.contains(event.target)) closeAssetBatchMenu();
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') closeAssetBatchMenu();
    });
});
document.addEventListener('languagechange', () => ASSET_CUSTOM_SELECT_IDS.forEach(syncAssetSelect));
