/** 资产库：筛选、分页、行内操作、详情、新建/编辑、批量、导入、合并、绑定项目、导出。 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useSearchParams } from 'react-router-dom';
import { Banner, Button, Space, Typography } from '@douyinfe/semi-ui';
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
        project_id: filters.project_id,
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
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('asset:read');
    const canWrite = hasPermission('asset:write');
    const canDelete = hasPermission('asset:delete');

    const [searchParams, setSearchParams] = useSearchParams();
    const location = useLocation();
    const deepId = searchParams.get('id') || '';
    const linkedAsset = location.state?.asset || null;

    const [filters, setFilters] = useState(() => ({
        ...emptyAssetFilters(),
        q: searchParams.get('q') || ''
    }));
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [tick, setTick] = useState(0);
    const [selection, setSelection] = useState({ keys: [], rows: [] });
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
        () => buildAssetQuery(debouncedFilters, page, pageSize),
        [debouncedFilters, page, pageSize]
    );
    const { data, loading, error, reload } = useResource(
        () => api.get('/assets', query),
        [query, tick],
        { skip: !canRead }
    );
    const { projects, reload: reloadProjects } = useAssetProjects();

    const assets = useMemo(() => (Array.isArray(data?.assets) ? data.assets : []), [data]);
    const total = Number(data?.total || 0);

    const resolvedDeep = useRef('');

    useEffect(() => {
        if (!deepId || !linkedAsset?.id || linkedAsset.id !== deepId || resolvedDeep.current === deepId) return;
        resolvedDeep.current = deepId;
        setDetailAsset(linkedAsset);
    }, [deepId, linkedAsset]);

    useEffect(() => {
        if (!deepId || deepId === resolvedDeep.current) return undefined;
        const inPage = assets.find(item => item.id === deepId);
        if (inPage) {
            resolvedDeep.current = deepId;
            setDetailAsset(inPage);
            return undefined;
        }
        let cancelled = false;
        (async () => {
            for (let current = 1; current <= 5 && !cancelled; current += 1) {
                const payload = await api.get('/assets', { page_size: 100, page: current });
                const hit = (payload?.assets || []).find(item => item.id === deepId);
                if (hit) {
                    if (!cancelled) {
                        resolvedDeep.current = deepId;
                        setDetailAsset(hit);
                    }
                    return;
                }
                if (current >= Number(payload?.total_pages || 1)) break;
            }
            if (!cancelled) resolvedDeep.current = deepId;
        })().catch(() => { resolvedDeep.current = deepId; });
        return () => { cancelled = true; };
    }, [deepId, assets]);

    const changeFilters = patch => {
        setFilters(prev => ({ ...prev, ...patch }));
        setPage(1);
        setSelection({ keys: [], rows: [] });
    };

    const resetFilters = () => {
        setFilters(emptyAssetFilters());
        setPage(1);
        setSelection({ keys: [], rows: [] });
    };

    const refresh = () => setTick(value => value + 1);

    const writeDeepLink = assetId => {
        const next = new URLSearchParams(searchParams);
        if (assetId) next.set('id', assetId);
        else next.delete('id');
        setSearchParams(next, { replace: true });
    };

    const openDetail = asset => {
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
        setSelection({ keys: [], rows: [] });
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
            content: `将永久删除 ${ids.length} 个资产。`,
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
        const { page: _page, page_size: _size, ...rest } = buildAssetQuery(debouncedFilters, 1, pageSize);
        return rest;
    }, [debouncedFilters, pageSize]);

    const selectAllMatching = async () => {
        try {
            const payload = await api.get('/assets/selection', selectionQuery);
            const rows = Array.isArray(payload?.assets) ? payload.assets : [];
            setSelection({ keys: rows.map(item => item.id), rows });
            toastSuccess(t('assets.allResultsSelected', { count: rows.length }, `已选择 ${rows.length} 项`));
        } catch (selectionError) {
            toastError(selectionError, '选择全部结果失败');
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
        setSelection({ keys: keys || [], rows: rows || [] });
    };

    const selectedCount = selection.keys.length;

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
                description="统一维护域名、IP、端口与服务资产，支持批量入库、合并与项目绑定。"
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
            <ErrorBanner error={error} onRetry={refresh} />

            <SectionCard title={t('assets.filterTitle', null, '筛选条件')}>
                <AssetFilterBar
                    filters={filters}
                    projects={projects}
                    onChange={changeFilters}
                    onReset={resetFilters}
                />
            </SectionCard>

            <SectionCard
                title={`${t('assets.libraryTitle', null, '资产库')}（${total}）`}
                extra={(
                    <Space>
                        <Button
                            theme="borderless"
                            disabled={!selectedCount}
                            onClick={() => setSelection({ keys: [], rows: [] })}
                        >
                            {t('assets.clearSelection', null, '清除选择')}
                        </Button>
                        <Button theme="borderless" onClick={() => exportCsv(false)} loading={exporting}>
                            {t('assets.exportCsv', null, '导出 CSV')}
                        </Button>
                        <Button theme="borderless" onClick={() => exportCsv(true)} loading={exporting}>
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
                        <Button theme="borderless" size="small" onClick={selectAllMatching}>
                            {t('assets.selectAllResults', { count: total }, `选择全部 ${total} 项`)}
                        </Button>
                    </Space>
                    <Space>
                        {canWrite && (
                            <Button size="small" disabled={selectedCount < 2} onClick={() => setMergeOpen(true)}>
                                {t('assets.mergeDuplicates', null, '合并重复资产')}
                            </Button>
                        )}
                        {canWrite && (
                            <Button size="small" disabled={!selectedCount} onClick={() => setProjectOpen(true)}>
                                {t('assets.bindProject', null, '绑定项目')}
                            </Button>
                        )}
                        {canWrite && (
                            <Button size="small" disabled={!selectedCount} onClick={() => setBulkOpen(true)}>
                                {t('assets.bulkEdit', null, '批量编辑')}
                            </Button>
                        )}
                        {canDelete && (
                            <Button size="small" type="danger" disabled={!selectedCount} onClick={batchDelete}>
                                {t('assets.batchDelete', null, '批量删除…')}
                            </Button>
                        )}
                    </Space>
                </div>

                <AssetTable
                    assets={assets}
                    loading={loading}
                    total={total}
                    page={page}
                    pageSize={pageSize}
                    onPageChange={next => { setPage(next); setSelection({ keys: [], rows: [] }); }}
                    onPageSizeChange={next => { setPageSize(next); setPage(1); setSelection({ keys: [], rows: [] }); }}
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
                projects={projects}
                onClose={() => setEditorOpen(false)}
                onSaved={afterWrite}
            />
            <AssetImportModal
                visible={importOpen}
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
