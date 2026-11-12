const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, 'assets.js'), 'utf8');
function context(locale) {
    const translations = JSON.parse(fs.readFileSync(path.join(__dirname, '../i18n', locale + '.json'))).assets;
    const c = vm.createContext({ window: { i18next: { t: key => translations[key.slice(7)] || key } } });
    vm.runInContext(source.slice(source.indexOf('function assetT('), source.indexOf('function downloadAssetTemplate(')), c);
    vm.runInContext(source.slice(source.indexOf('function assetRiskPresentation('), source.indexOf('function assetOwnershipMarkup(')), c);
    return c;
}
for (const locale of ['en-US', 'ru-RU']) {
    test(locale + ' export headers and enum values can be imported again', () => {
        const c = context(locale);
        const columns = vm.runInContext('ASSET_IMPORT_COLUMNS.slice()', c);
        for (const column of columns) {
            const header = c.assetExportHeader(column);
            assert.equal(c.assetImportColumnForHeader(header), column);
        }
        for (const [key, codes] of Object.entries({ environment: ['production', 'staging', 'testing', 'development', 'other'], criticality: ['critical', 'high', 'medium', 'low'], status: ['active', 'inactive'] })) {
            for (const code of codes) {
                const display = c.assetExportValue(key, code);
                assert.equal(c.assetLocalizedEnumMap({ [code]: code }, key)[display.toLowerCase()], code);
            }
        }
        assert.equal(c.assetExportValue('country', 'CN'), 'CN');
        assert.equal(c.assetExportValue('environment', 'custom-value'), 'custom-value');
        assert.equal(c.assetExportValue('vulnerability_count', 2), 2);
        assert.equal(c.assetImportColumnForHeader('responsible_person'), 'responsible_person');
    });
}
test('CSV and XLSX both use localized headers and values', () => {
    const exportFunction = source.slice(source.indexOf('function exportSelectedAssets('), source.indexOf('async function deleteSelectedAssets('));
    assert.match(exportFunction, /headers\.map\(key => assetCsvCell\(assetExportHeader\(key\)\)\)/);
    assert.match(exportFunction, /XLSX\.utils\.aoa_to_sheet/);
    assert.match(exportFunction, /assetExportValue\(key, row\[key\]\)/);
});
for (const locale of ['en-US', 'ru-RU']) {
    test(locale + ' actual CSV and XLSX exports round trip through the import parser', async () => {
        const c = context(locale);
        Object.assign(c, { URL, Blob, document: { getElementById: () => null }, alert: message => { throw new Error(message); } });
        vm.runInContext(fs.readFileSync(path.join(__dirname, '../vendor/xlsx.full.min.js'), 'utf8'), c);
        c.window.XLSX = c.XLSX;
        c.assetPageState = { projects: [{ id: 'p1', name: '项目一' }], selected: new Map() };
        const asset = { host: 'https://example.com:443', domain: 'example.com', port: 443, protocol: 'https', project_name: '项目一', title: 'title,"quoted"\nnext', status: 'inactive', environment: 'production', criticality: 'high', country: 'CN', province: 'Beijing', tags: ['one', 'two'] };
        c.assetPageState.selected.set('a1', asset);
        vm.runInContext(source.slice(source.indexOf('function assetTargetLabel('), source.indexOf('function assetRiskPresentation(')), c);
        vm.runInContext(source.slice(source.indexOf('function assetExportRows('), source.indexOf('function downloadAssetBlob(')), c);
        vm.runInContext(source.slice(source.indexOf('function exportSelectedAssets('), source.indexOf('async function deleteSelectedAssets(')), c);
        vm.runInContext(source.slice(source.indexOf('function assetEditorDefaultPort('), source.indexOf('async function assetEditorResponseError(')), c);
        vm.runInContext(source.slice(source.indexOf('function parseAssetImportMatrix('), source.indexOf('function renderAssetImportPreview(')), c);
        c.renderAssetImportPreview = () => {};
        let csv, workbook;
        c.downloadAssetBlob = blob => { csv = blob; };
        c.XLSX.writeFile = book => { workbook = book; };
        c.exportSelectedAssets('csv');
        const csvBook = c.XLSX.read(await csv.text(), { type: 'string' });
        c.exportSelectedAssets('xlsx');
        const bytes = c.XLSX.write(workbook, { type: 'array', bookType: 'xlsx' });
        const xlsxBook = c.XLSX.read(bytes, { type: 'array' });
        for (const book of [csvBook, xlsxBook]) {
            const matrix = c.XLSX.utils.sheet_to_json(book.Sheets[book.SheetNames[0]], { header: 1, defval: '', raw: false });
            assert.equal(matrix[0][0], c.assetExportHeader('target'));
            c.parseAssetImportMatrix(matrix, 'export');
            const imported = c.assetPageState.importRows[0];
            assert.equal(imported.error, '');
            for (const key of ['host', 'domain', 'port', 'protocol', 'title', 'status', 'environment', 'criticality', 'country', 'province']) assert.equal(imported.asset[key], asset[key], key);
            assert.equal(imported.asset.project_id, 'p1');
            assert.deepEqual(Array.from(imported.asset.tags), asset.tags);
        }
    });
}
