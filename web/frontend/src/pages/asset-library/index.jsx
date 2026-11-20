/** 资产库：筛选、分页、行内操作、详情、新建/编辑、批量、导入、合并、绑定项目、导出。 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Banner, Button, Select, Space, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconCloudUploadStroked } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useDebounced, useResource } from '../../components/hooks';
import {
    ErrorBanner, PageHeader, SectionCard, confirmAction, toastError, toastSuccess, toastWarning
} from '../../components/ui';
import AssetFilterBar, { emptyAssetFilters } from './components/AssetFilterBar';
import AssetTable from './components/AssetTable';
import AssetDetailDrawer from './components/AssetDetailDrawer';
import AssetEditorModal from './components/AssetEditorModal';
import AssetImportModal from './components/AssetImportModal';
import AssetMergeModal from './components/AssetMergeModal';
import AssetProjectModal from './components/AssetProjectModal';
import AssetBulkEditModal from './components/AssetBulkEditModal';
import { useAssetProjects } from './components/useAssetProjects';
import { applyScanStateParam } from './components/assetConstants';
import { assetsToCsv, downloadTextFile } from './components/assetCsv';
import './page.css';

const { Text } = Typography;

export function buildAssetQuery(filters, page, pageSize) {
    let query = {
        page,
        page_size: pageSize,
        q: filters.q,
        protocol: filters.protocol,
        source: filters.source,
        tag: filters.tag,
        status: filters.status,
        risk_level: filters.risk_level,
        environment: filters.environment,
        criticality: filters.criticality,
        ip: filters.ip,
        domain: filters.domain,
        port: filters.port
    };
    query = applyScanStateParam(query, filters.scan);
    const [sortBy, sortOrder] = String(filters.sort || 'last_seen_at:desc').split(':');
    query.sort_by = sortBy;
    query.sort_order = sortOrder || 'desc';
    return query;
}

export default function AssetLibraryPage() {
    const [searchParams, setSearchParams] = useSearchParams();
    const projectResource = useAssetProjects();
    const projectId = searchParams.get('project_id') || '';
    const scope = projectId ? 'project' : searchParams.get('scope');
    return (
        <AssetLibraryWorkspace
            key={projectId || scope || ''}
            projectId={projectId}
            scope={scope}
            projectResource={projectResource}
            searchParams={searchParams}
            setSearchParams={setSearchParams}
        />
    );
}

function AssetLibraryWorkspace({ projectId, scope, projectResource, searchParams, setSearchParams }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('asset:read');
    const canWrite = hasPermission('asset:write');
    const canDelete = hasPermission('asset:delete');

    const deepId = searchParams.get('id') || '';
    const hasScope = Boolean(projectId) || scope === 'all' || scope === 'unassigned';
    const scopeValue = projectId ? `project:${projectId}` : (hasScope ? scope : undefined);
    const scopeQuery = useMemo(() => ({
        project_id: projectId,
        project_unassigned: scope === 'unassigned' ? true : undefined
    }), [projectId, scope]);

    const [filters, setFilters] = useState(() => ({
        ...emptyAssetFilters(),
        q: searchParams.get('q') || ''
    }));
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [selection, setSelection] = useState({ keys: [], rows: [] });
    const selectionRevision = useRef(0);
    const [detailAsset, setDetailAsset] = useState(null);
    const [editorOpen, setEditorOpen] = useState(false);
    const [editorAsset, setEditorAsset] = useState(null);
    const [importOpen, setImportOpen] = useState(false);
    const [mergeOpen, setMergeOpen] = useState(false);
    const [projectOpen, setProjectOpen] = useState(false);
    const [bulkOpen, setBulkOpen] = useState(false);
    const [exporting, setExporting] = useState(false);

    const debouncedFilters = useDebounced(filters, 300);
    const query = useMemo(
        () => ({ ...buildAssetQuery(debouncedFilters, page, pageSize), ...scopeQuery }),
        [debouncedFilters, page, pageSize, scopeQuery]
    );
    const { data, loading, error, reload } = useResource(
        () => api.get('/assets', query),
        [query, canRead, hasScope],
        { skip: !canRead || !hasScope }
    );
    const { projects, loading: projectsLoading, error: projectError, reload: reloadProjects } = projectResource;

    const assets = useMemo(() => (Array.isArray(data?.assets) ? data.assets : []), [data]);
    const total = error ? 0 : Number(data?.total || 0);
    const queryPending = loading || debouncedFilters !== filters;
    const projectName = projects.find(project => project.id === projectId)?.name || projectId;
    const scopeOptions = [
        ...projects.map(project => ({ value: `project:${project.id}`, label: project.name || project.id })),
        { value: 'unassigned', label: t('assets.scopeUnassigned', null, '未归属项目') },
        { value: 'all', label: t('assets.scopeAll', null, '全部资产') }
    ];
    if (projectId && !scopeOptions.some(option => option.value === scopeValue)) {
        scopeOptions.unshift({ value: scopeValue, label: projectName });
    }
    const scopeName = scopeOptions.find(option => option.value === scopeValue)?.label;

    const resolvedDeep = useRef('');

    useEffect(() => () => { selectionRevision.current += 1; }, []);

    useEffect(() => {
        if (!deepId) {
            resolvedDeep.current = '';
            setDetailAsset(null);
            return undefined;
        }
        if (!canRead || deepId === resolvedDeep.current) return undefined;
        let cancelled = false;
        api.get('/assets', { ...scopeQuery, id: deepId, page_size: 1 }).then(payload => {
            if (cancelled) return;
            resolvedDeep.current = deepId;
            setDetailAsset(payload?.assets?.find(item => item.id === deepId) || null);
        }).catch(detailError => {
            if (!cancelled) toastError(detailError, t('assets.loadFailed', null, '加载资产失败'));
        });
        return () => { cancelled = true; };
    }, [canRead, deepId, scopeQuery, t]);

    const changeScope = value => {
        const next = new URLSearchParams();
        if (value?.startsWith('project:')) next.set('project_id', value.slice('project:'.length));
        else if (value) next.set('scope', value);
        setSearchParams(next);
    };

    const clearSelection = () => {
        selectionRevision.current += 1;
        setSelection({ keys: [], rows: [] });
    };

    const changeFilters = patch => {
        setFilters(prev => ({ ...prev, ...patch }));
        setPage(1);
        clearSelection();
    };

    const resetFilters = () => {
        setFilters(emptyAssetFilters());
        setPage(1);
        clearSelection();
    };

    const refresh = () => {
        clearSelection();
        if (hasScope) reload();
        reloadProjects();
    };

    const writeDeepLink = assetId => {
        const next = new URLSearchParams(searchParams);
        if (assetId) next.set('id', assetId);
        else next.delete('id');
        setSearchParams(next, { replace: true });
    };

    const openDetail = asset => {
        resolvedDeep.current = asset.id;
        setDetailAsset(asset);
        if (asset?.id) writeDeepLink(asset.id);
    };

    const closeDetail = () => {
        setDetailAsset(null);
        if (deepId) {
            resolvedDeep.current = '';
            writeDeepLink('');
        }
    };

    const openEditor = asset => {
        setEditorAsset(asset || null);
        setEditorOpen(true);
        if (canWrite) reloadProjects();
    };

    const afterWrite = async () => {
        clearSelection();
        await reload();
    };

    const deleteAsset = async asset => {
        await api.del(`/assets/${encodeURIComponent(asset.id)}`);
        toastSuccess(t('assets.deleted', null, '资产已删除'));
        if (detailAsset?.id === asset.id) closeDetail();
        await afterWrite();
    };

    const batchDelete = () => {
        const ids = selection.keys;
        if (!ids.length) return;
        confirmAction({
            title: t('assets.deleteConfirm', null, '确定删除所选资产吗？'),
            content: t('assets.scopedDeleteConfirm', {
                count: ids.length,
                project: selection.rows[0]?.project_name || selection.rows[0]?.project_id || t('assets.scopeUnassigned', null, '未归属项目')
            }, `将永久删除所选的 ${ids.length} 个资产。`),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    const result = await api.post('/assets/batch-delete', { asset_ids: ids });
                    toastSuccess(`已删除 ${Number(result?.deleted || ids.length)} 个资产`);
                    await afterWrite();
                } catch (deleteError) {
                    toastError(deleteError, '批量删除失败');
                }
            }
        });
    };

    const selectionQuery = useMemo(() => {
        const { page: _page, page_size: _size, ...rest } = query;
        return rest;
    }, [query]);

    const selectAllMatching = async () => {
        const revision = ++selectionRevision.current;
        try {
            const payload = await api.get('/assets/selection', selectionQuery);
            if (revision !== selectionRevision.current) return;
            const rows = Array.isArray(payload?.assets) ? payload.assets : [];
            setSelection({ keys: rows.map(item => item.id), rows });
            toastSuccess(t('assets.allResultsSelected', { count: rows.length }, `已选择 ${rows.length} 项`));
        } catch (selectionError) {
            if (revision === selectionRevision.current) toastError(selectionError, '选择全部结果失败');
        }
    };

    const exportCsv = async all => {
        if (exporting) return;
        setExporting(true);
        try {
            let rows = selection.rows;
            if (all) {
                const payload = await api.get('/assets/selection', selectionQuery);
                rows = Array.isArray(payload?.assets) ? payload.assets : [];
            } else if (!rows.length) {
                rows = assets;
            }
            if (!rows.length) {
                toastWarning('没有可导出的资产');
                return;
            }
            const stamp = new Date().toISOString().slice(0, 10);
            downloadTextFile(`assets-${stamp}.csv`, assetsToCsv(rows));
            toastSuccess(`已导出 ${rows.length} 个资产`);
        } catch (exportError) {
            toastError(exportError, '导出失败');
        } finally {
            setExporting(false);
        }
    };

    const onSelectionChange = (keys, rows) => {
        selectionRevision.current += 1;
        setSelection({ keys: keys || [], rows: rows || [] });
    };

    const selectedCount = selection.keys.length;
    const mixedProjects = new Set(selection.rows.map(asset => asset.project_id || '')).size > 1;
    const canChangeSelection = selectedCount > 0 && !queryPending && !mixedProjects;

    if (!canRead) {
        return (
            <div className="asset-library-page">
                <PageHeader title={t('assets.libraryTitle', null, '资产库')} description="资产列表、批量操作与导入需要资产读取权限。" />
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    description={t('assets.readDenied', null, '当前账号没有 asset:read 权限，无法读取资产库；请联系管理员授权。')}
                />
            </div>
        );
    }

    return (
        <div className="asset-library-page">
            <PageHeader
                title={t('assets.libraryTitle', null, '资产库')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={refresh} loading={loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        {canWrite && (
                            <Button icon={<IconCloudUploadStroked />} onClick={() => setImportOpen(true)}>
                                {t('assets.bulkImport', null, '批量导入')}
                            </Button>
                        )}
                        {canWrite && (
                            <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => openEditor(null)}>
                                {t('assets.addAsset', null, '新增资产')}
                            </Button>
                        )}
                    </Space>
                )}
            />
            <SectionCard title={<span id="asset-library-scope-label">{t('assets.scopeLabel', null, '资产范围')}</span>}>
                <Select
                    aria-labelledby="asset-library-scope-label"
                    className="asset-library-scope"
                    value={scopeValue}
                    placeholder={t('assets.scopePlaceholder', null, '选择项目或查看全部资产')}
                    optionList={scopeOptions}
                    loading={projectsLoading}
                    onChange={changeScope}
                    filter
                />
                <ErrorBanner error={projectError} onRetry={reloadProjects} />
            </SectionCard>
            {hasScope && (
                <>
                    <ErrorBanner error={error} onRetry={refresh} />

                    <SectionCard title={t('assets.filterTitle', null, '筛选条件')}>
                        <AssetFilterBar
                            filters={filters}
                            onChange={changeFilters}
                            onReset={resetFilters}
                        />
                    </SectionCard>

                    <SectionCard
                        title={`${scopeName}（${total}）`}
                        extra={(
                            <Space>
                                <Button
                                    theme="borderless"
                                    disabled={!selectedCount}
                                    onClick={clearSelection}
                                >
                                    {t('assets.clearSelection', null, '清除选择')}
                                </Button>
                                <Button theme="borderless" disabled={queryPending || !total} onClick={() => exportCsv(false)} loading={exporting}>
                                    {t('assets.exportCsv', null, '导出 CSV')}
                                </Button>
                                <Button theme="borderless" disabled={queryPending || !total} onClick={() => exportCsv(true)} loading={exporting}>
                                    {t('assets.exportAll', null, '导出全部筛选结果')}
                                </Button>
                            </Space>
                        )}
                    >
                        <div className="asset-library-toolbar">
                            <Space>
                                <Text type="tertiary">
                                    {selectedCount
                                        ? t('assets.selectedCount', { count: selectedCount }, `已选择 ${selectedCount} 项`)
                                        : t('assets.selectedCount', { count: 0 }, '已选择 0 项')}
                                </Text>
                                <Button theme="borderless" size="small" disabled={queryPending || !total} onClick={selectAllMatching}>
                                    {t('assets.selectAllResults', { count: total }, `选择全部 ${total} 项`)}
                                </Button>
                            </Space>
                            <Space>
                                {canWrite && (
                                    <Button size="small" disabled={!canChangeSelection || selectedCount < 2 || !canDelete} onClick={() => setMergeOpen(true)}>
                                        {t('assets.mergeDuplicates', null, '合并重复资产')}
                                    </Button>
                                )}
                                {canWrite && (
                                    <Button size="small" disabled={!canChangeSelection} onClick={() => setProjectOpen(true)}>
                                        {t('assets.bindProject', null, '绑定项目')}
                                    </Button>
                                )}
                                {canWrite && (
                                    <Button size="small" disabled={!canChangeSelection} onClick={() => setBulkOpen(true)}>
                                        {t('assets.bulkEdit', null, '批量编辑')}
                                    </Button>
                                )}
                                {canDelete && (
                                    <Button size="small" type="danger" disabled={!canChangeSelection} onClick={batchDelete}>
                                        {t('assets.batchDelete', null, '批量删除…')}
                                    </Button>
                                )}
                            </Space>
                        </div>
                        {mixedProjects && (canWrite || canDelete) && (
                            <Text type="tertiary" size="small">
                                {t('assets.mixedProjectSelection', null, '批量修改前，请将选择缩小到同一项目或仅选择未归属资产。')}
                            </Text>
                        )}

                        <AssetTable
                            assets={queryPending || error ? [] : assets}
                            loading={queryPending}
                            total={total}
                            page={page}
                            pageSize={pageSize}
                            onPageChange={next => { setPage(next); clearSelection(); }}
                            onPageSizeChange={next => { setPageSize(next); setPage(1); clearSelection(); }}
                            selectedRowKeys={selection.keys}
                            onSelectionChange={onSelectionChange}
                            onView={openDetail}
                            onEdit={openEditor}
                            onDelete={deleteAsset}
                            onRefresh={refresh}
                            canWrite={canWrite}
                            canDelete={canDelete}
                        />
                    </SectionCard>
                </>
            )}

            <AssetDetailDrawer
                asset={detailAsset}
                visible={Boolean(detailAsset)}
                onClose={closeDetail}
                onEdit={asset => { closeDetail(); openEditor(asset); }}
                onDelete={asset => {
                    confirmAction({
                        title: t('assets.deleteConfirm', null, '确定删除该资产吗？'),
                        content: asset.host || asset.domain || asset.ip,
                        okText: t('common.delete', null, '删除'),
                        onOk: () => deleteAsset(asset).catch(deleteError => toastError(deleteError, t('assets.deleteFailed', null, '删除资产失败')))
                    });
                }}
                canWrite={canWrite}
                canDelete={canDelete}
            />

            <AssetEditorModal
                visible={editorOpen}
                asset={editorAsset}
                defaultProjectId={projectId}
                projects={projects}
                onClose={() => setEditorOpen(false)}
                onSaved={afterWrite}
            />
            <AssetImportModal
                visible={importOpen}
                defaultProjectId={scope === 'all' || !hasScope ? undefined : projectId}
                projects={projects}
                onClose={() => setImportOpen(false)}
                onDone={afterWrite}
            />
            <AssetMergeModal
                visible={mergeOpen}
                assets={selection.rows}
                onClose={() => setMergeOpen(false)}
                onDone={afterWrite}
            />
            <AssetProjectModal
                visible={projectOpen}
                projects={projects}
                assetIds={selection.keys}
                onClose={() => setProjectOpen(false)}
                onDone={afterWrite}
            />
            <AssetBulkEditModal
                visible={bulkOpen}
                assetIds={selection.keys}
                onClose={() => setBulkOpen(false)}
                onDone={afterWrite}
            />
        </div>
    );
}
