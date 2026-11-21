/**
 * 漏洞管理：统计看板 + 服务端分页列表 + 详情抽屉 + 新建/编辑 + 删除/批量删除/导出。
 * 接口与字段以 internal/handler/vulnerability*.go 为准；筛选/深链接状态落在 hash 查询串。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, RadioGroup, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api, downloadFile } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import {
    ErrorBanner, PageHeader, SectionCard, StatGrid,
    confirmAction, toastError, toastSuccess, toastWarning
} from '../../components/ui';
import FilterBar from './FilterBar';
import { GroupNavigator, GroupOverview } from './GroupNavigator';
import VulnTable from './List';
import DetailDrawer from './DetailDrawer';
import EditModal from './EditModal';
import {
    DEFAULT_PAGE_SIZE, FILTER_KEYS, PAGE_SIZE_OPTIONS, SEVERITY_CODES,
    filterQuery, hasActiveFilters, percentOf, saveTextFile, severityText, statsQuery
} from './shared';
import './page.css';

const { Text } = Typography;

const VIEWS = { none: 'viewList', host: 'viewHost', task: 'viewTask' };
const EMPTY_GROUPS = [];

export default function VulnerabilitiesPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const [params, setParams] = useSearchParams();

    const filters = useMemo(() => ({
        q: params.get('q') || '',
        severity: params.get('severity') || '',
        status: params.get('status') || '',
        project_id: params.get('project_id') || '',
        conversation_tag: params.get('conversation_tag') || '',
        task_tag: params.get('task_tag') || '',
        // handler 只接受 severity / latest / oldest，非法值会让列表 400。
        sort: ['severity', 'latest', 'oldest'].includes(params.get('sort')) ? params.get('sort') : ''
    }), [params]);

    const page = Math.max(1, Number.parseInt(params.get('page') || '1', 10) || 1);
    const pageSize = PAGE_SIZE_OPTIONS.includes(Number(params.get('page_size')))
        ? Number(params.get('page_size'))
        : DEFAULT_PAGE_SIZE;
    const detailId = (params.get('id') || '').trim();
    const viewParam = params.get('group_by');
    const groupView = viewParam === 'host' || viewParam === 'task' ? viewParam : '';
    const groupSelection = groupView ? params.get('selection') || '' : '';
    const groupSearchKey = groupView === 'task' ? 'task_q' : 'host_q';
    const groupSearch = groupView ? params.get(groupSearchKey) || '' : '';
    const groupSearchQuery = groupSearch ? { [groupSearchKey]: groupSearch } : {};

    const canWrite = hasPermission('vulnerability:write');
    const canDelete = hasPermission('vulnerability:delete') || canWrite;

    const patchParams = useCallback((patch, options = {}) => {
        setParams(previous => {
            const next = new URLSearchParams(previous);
            Object.entries(patch).forEach(([key, value]) => {
                if (value === undefined || value === null || value === '') next.delete(key);
                else next.set(key, String(value));
            });
            if (options.resetPage) next.delete('page');
            return next.toString() === previous.toString() ? previous : next;
        }, { replace: true });
    }, [setParams]);

    const applyFilter = useCallback((key, value) => {
        patchParams({ [key]: value }, { resetPage: true });
    }, [patchParams]);

    const resetFilters = useCallback(() => {
        const patch = { sort: undefined, selection: undefined, host_q: undefined, task_q: undefined };
        FILTER_KEYS.forEach(key => { patch[key] = undefined; });
        patchParams(patch, { resetPage: true });
    }, [patchParams]);

    const changeGroupView = useCallback(view => {
        patchParams({
            group_by: view === 'none' ? undefined : view,
            selection: undefined,
            host_q: undefined,
            task_q: undefined
        }, { resetPage: true });
    }, [patchParams]);
    const selectGroup = useCallback(value => {
        patchParams({ selection: value }, { resetPage: true });
    }, [patchParams]);
    const searchGroups = useCallback(value => {
        patchParams({ [groupSearchKey]: value, selection: undefined }, { resetPage: true });
    }, [groupSearchKey, patchParams]);

    const openDetail = useCallback(id => patchParams({ id }), [patchParams]);
    const closeDetail = useCallback(() => patchParams({ id: undefined }), [patchParams]);

    // 列表只要摘要字段（handler 的 summary=true），详情/编辑另行按 id 取全量。
    const listQuery = {
        ...filterQuery(filters),
        ...groupSearchQuery,
        ...(groupView ? { group_by: groupView, pagination: 'records', selection: groupSelection } : {}),
        ...(filters.sort ? { sort: filters.sort } : {}),
        summary: 'true',
        page,
        limit: pageSize
    };
    const listKey = JSON.stringify(listQuery);
    // 把请求键随响应带回，避免用上一条筛选的响应纠正地址栏页码。
    const list = useResource(async () => {
        const payload = await api.get('/vulnerabilities', listQuery);
        return { ...(payload || {}), __requestKey: listKey, __groupView: groupView };
    }, [listKey]);
    const listData = list.data?.__requestKey === listKey ? list.data : null;
    const groups = list.data?.__groupView === groupView ? list.data?.navigation || EMPTY_GROUPS : EMPTY_GROUPS;
    const selectedGroup = listData?.selection
        ? groups.flatMap(group => [group, ...(group.children || [])]).find(group => group.key === listData.selection)
        : null;

    const statQuery = useMemo(() => statsQuery(filters), [filters]);
    const statKey = JSON.stringify(statQuery);
    const stats = useResource(() => api.get('/vulnerabilities/stats', statQuery), [statKey]);

    const [optionsVersion, setOptionsVersion] = useState(0);
    const options = useResource(() => api.get('/vulnerabilities/filter-options'), [optionsVersion]);

    const projects = useResource(async () => {
        try {
            const payload = await api.get('/projects', { limit: 500 });
            return Array.isArray(payload?.projects) ? payload.projects : [];
        } catch (error) {
            return []; // 项目名只用于展示，缺权限时回退显示项目 ID
        }
    }, []);

    const projectNames = useMemo(() => {
        const map = {};
        (projects.data || []).forEach(project => {
            if (project?.id) map[project.id] = project.name || project.id;
        });
        return map;
    }, [projects.data]);

    const projectOptions = useMemo(() => {
        const seen = new Map();
        (options.data?.project_ids || []).forEach(id => {
            if (id) seen.set(id, projectNames[id] || id);
        });
        if (filters.project_id && !seen.has(filters.project_id)) {
            seen.set(filters.project_id, projectNames[filters.project_id] || filters.project_id);
        }
        return [...seen.entries()].map(([value, label]) => ({ value, label }));
    }, [options.data, projectNames, filters.project_id]);

    const formProjectOptions = useMemo(() => {
        const seen = new Map();
        (projects.data || []).forEach(project => {
            if (project?.id) seen.set(project.id, project.name || project.id);
        });
        return [...seen.entries()].map(([value, label]) => ({ value, label }));
    }, [projects.data]);

    const [selection, setSelection] = useState([]);
    const [editor, setEditor] = useState({ visible: false, id: '' });
    const [detailVersion, setDetailVersion] = useState(0);
    const [exporting, setExporting] = useState(false);

    useEffect(() => { setSelection([]); }, [listKey]);

    // 服务端会把越界页码收敛到最后一页，这里同步回地址栏，刷新后仍能恢复。
    useEffect(() => {
        if (!listData) return;
        const patch = {};
        const serverPage = Number(listData.page);
        if (serverPage && serverPage !== page) patch.page = serverPage;
        if (groupSelection && !listData.selection) patch.selection = undefined;
        if (Object.keys(patch).length) patchParams(patch);
    }, [listData, groupSelection, page, patchParams]);

    const reloadList = list.reload;
    const reloadStats = stats.reload;
    const reloadOptions = options.reload;

    const refreshAll = useCallback(() => {
        reloadList();
        reloadStats();
        reloadOptions({ silent: true });
    }, [reloadList, reloadStats, reloadOptions]);

    const openEditor = useCallback(record => {
        setEditor({ visible: true, id: record?.id || '' });
    }, []);

    const handleSaved = useCallback(() => {
        setEditor({ visible: false, id: '' });
        toastSuccess(t('vulnerabilityPage.saveSuccess', null, '已保存'));
        setDetailVersion(value => value + 1);
        setOptionsVersion(value => value + 1);
        reloadList({ silent: true });
        reloadStats({ silent: true });
    }, [reloadList, reloadStats, t]);

    const handleDelete = useCallback(record => {
        if (!record?.id) return;
        confirmAction({
            title: t('vulnerabilityPage.deleteTitle', null, '删除漏洞'),
            content: `${t('vulnerability.deleteConfirm', null, '确定要删除此漏洞吗？')}${record.title ? ` ${record.title}` : ''}`,
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/vulnerabilities/${encodeURIComponent(record.id)}`);
                    toastSuccess(t('vulnerabilityPage.deleteSuccess', null, '已删除'));
                    if (detailId === record.id) closeDetail();
                    setDetailVersion(value => value + 1);
                    setOptionsVersion(value => value + 1);
                    await Promise.all([reloadList({ silent: true }), reloadStats({ silent: true })]);
                } catch (error) {
                    toastError(error, t('vulnerabilityPage.deleteFailed', null, '删除失败'));
                }
            }
        });
    }, [detailId, closeDetail, reloadList, reloadStats, t]);

    const handleBatchDelete = useCallback(() => {
        if (!selection.length) return;
        const ids = [...selection];
        confirmAction({
            title: t('vulnerabilityPage.batchDelete', null, '批量删除'),
            content: t(
                'vulnerabilityPage.batchDeleteConfirm',
                { count: ids.length },
                `确定要删除选中的 ${ids.length} 条漏洞吗？此操作不可恢复。`
            ),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                let deleted = 0;
                let failed = 0;
                for (const id of ids) {
                    try {
                        await api.del(`/vulnerabilities/${encodeURIComponent(id)}`);
                        deleted += 1;
                    } catch (error) {
                        failed += 1;
                    }
                }
                if (failed) {
                    toastWarning(t(
                        'vulnerabilityPage.batchDeletePartial',
                        { deleted, failed },
                        `已删除 ${deleted} 条，${failed} 条失败`
                    ));
                } else {
                    toastSuccess(t('vulnerabilityPage.batchDeleteSuccess', { count: deleted }, `成功删除 ${deleted} 条漏洞`));
                }
                setSelection([]);
                if (ids.includes(detailId)) closeDetail();
                setDetailVersion(value => value + 1);
                setOptionsVersion(value => value + 1);
                await Promise.all([reloadList({ silent: true }), reloadStats({ silent: true })]);
            }
        });
    }, [selection, detailId, closeDetail, reloadList, reloadStats, t]);

    // DELETE /vulnerabilities/batch 按筛选条件删除（服务端语义），与逐行多选删除互补。
    const handleDeleteFiltered = useCallback(async () => {
        const query = filterQuery(filters);
        try {
            const head = await api.get('/vulnerabilities', { ...query, page: 1, limit: 1 });
            const total = Number(head?.total) || 0;
            if (!total) {
                toastWarning(t('vulnerabilityPage.batchDeleteNoResults', null, '当前筛选条件下没有可删除的漏洞'));
                return;
            }
            confirmAction({
                title: t('vulnerabilityPage.deleteFilteredTitle', null, '删除当前筛选结果'),
                content: t(
                    'vulnerabilityPage.batchDeleteConfirm',
                    { count: total },
                    `确定要删除当前筛选条件下的 ${total} 条漏洞吗？此操作不可恢复。`
                ),
                okText: t('common.delete', null, '删除'),
                onOk: async () => {
                    try {
                        const result = await api.del('/vulnerabilities/batch', { query });
                        const deleted = Number(result?.deleted) || total;
                        toastSuccess(t('vulnerabilityPage.batchDeleteSuccess', { count: deleted }, `成功删除 ${deleted} 条漏洞`));
                        setSelection([]);
                        setDetailVersion(value => value + 1);
                        setOptionsVersion(value => value + 1);
                        await Promise.all([reloadList({ silent: true }), reloadStats({ silent: true })]);
                    } catch (error) {
                        toastError(error, t('vulnerabilityPage.batchDeleteFailed', null, '批量删除失败'));
                    }
                }
            });
        } catch (error) {
            toastError(error, t('vulnerabilityPage.fetchFailed', null, '获取漏洞失败'));
        }
    }, [filters, reloadList, reloadStats, t]);

    // 导出：GET /vulnerabilities/export 返回 {files:[{filename,content}]}，逐份落盘。
    const handleExport = useCallback(async () => {
        if (exporting) return;
        setExporting(true);
        try {
            const payload = await api.get('/vulnerabilities/export', {
                ...filterQuery(filters),
                ...groupSearchQuery,
                mode: 'summary',
                group_by: 'conversation'
            });
            const files = Array.isArray(payload?.files) ? payload.files : [];
            if (!files.length) {
                toastWarning(t('vulnerabilityPage.exportNoResults', null, '当前筛选条件下无可导出漏洞'));
                return;
            }
            files.forEach((file, index) => {
                setTimeout(
                    () => saveTextFile(file.filename || `vulnerability-export-${index + 1}.md`, file.content || ''),
                    index * 120
                );
            });
            toastSuccess(files.length > 1
                ? t('vulnerabilityPage.exportStarted', { count: files.length }, `已开始下载 ${files.length} 份报告`)
                : t('vulnerabilityPage.exportSuccess', null, '报告已开始下载'));
        } catch (error) {
            toastError(error, t('vulnerabilityPage.exportFailed', null, '导出失败'));
        } finally {
            setExporting(false);
        }
    }, [exporting, filters, groupSearchQuery, t]);

    const handleDownload = useCallback(async record => {
        try {
            const name = await downloadFile(`/vulnerabilities/${encodeURIComponent(record.id)}/download`, {
                query: { format: 'docx' }
            });
            toastSuccess(`${t('vulnerabilityPage.downloadOkTitle', null, '下载成功')}${name ? ` · ${name}` : ''}`);
        } catch (error) {
            toastError(error, t('vulnerabilityPage.downloadFailed', null, '下载失败'));
        }
    }, [t]);

    const handlePageChange = useCallback(nextPage => {
        patchParams({ page: String(nextPage) });
    }, [patchParams]);

    // 与旧版一致：切换每页条数时保持在当前数据位置（Semi 内部也按同一公式换算）。
    const handlePageSizeChange = useCallback(nextSize => {
        if (!nextSize || nextSize === pageSize) return;
        const nextPage = Math.max(1, Math.floor(((page - 1) * pageSize) / nextSize) + 1);
        patchParams({ page_size: String(nextSize), page: String(nextPage) });
    }, [page, pageSize, patchParams]);

    const statsPayload = stats.data || {};
    const bySeverity = statsPayload.by_severity || {};
    const statTotal = Number(statsPayload.total) || 0;
    const distinctCount = Number(statsPayload.distinct) || 0;

    const metrics = useMemo(() => {
        const totalMetric = {
            id: 'total',
            label: t('vulnerabilityPage.statTotal', null, '总漏洞数'),
            value: statTotal,
            detail: distinctCount && distinctCount !== statTotal
                ? t('vulnerabilityPage.distinctCount', { count: distinctCount }, `${distinctCount} 个独立漏洞`)
                : undefined,
            loading: stats.loading,
            onClick: () => applyFilter('severity', '')
        };
        const severityMetrics = SEVERITY_CODES.map(code => {
            const count = Number(bySeverity[code]) || 0;
            const pct = percentOf(count, statTotal);
            const active = filters.severity === code;
            return {
                id: code,
                label: severityText(t, code),
                value: count,
                severity: code,
                pressed: active,
                detail: active
                    ? `${t('vulnerabilityPage.filterActive', null, '已筛选')} · ${pct}%`
                    : `${pct}%`,
                loading: stats.loading,
                onClick: () => applyFilter('severity', active ? '' : code)
            };
        });
        return [totalMetric, ...severityMetrics];
    }, [t, statTotal, distinctCount, bySeverity, stats.loading, filters.severity, applyFilter]);

    const totalRecords = Number(listData?.total) || 0;
    const rangeStart = totalRecords ? (page - 1) * pageSize + 1 : 0;
    const rangeEnd = totalRecords ? Math.min(page * pageSize, totalRecords) : 0;
    const summaryText = totalRecords
        ? t('vulnerabilityPage.recordPagination', { start: rangeStart, end: rangeEnd, total: totalRecords }, `显示 ${rangeStart}–${rangeEnd} / 共 ${totalRecords} 条漏洞`)
        : t('vulnerabilityPage.noRecords', null, '暂无漏洞记录');

    const pageError = list.error || stats.error || options.error;

    const toolbar = (
        <div className="vulnerability-toolbar-left">
            <Text type="tertiary">
                {listData && selection.length
                    ? t('vulnerabilityPage.selectedCount', { count: selection.length }, `已选 ${selection.length} 项`)
                    : summaryText}
            </Text>
            {canDelete && (
                <Button
                    size="small"
                    type="danger"
                    theme="light"
                    disabled={!listData || !selection.length}
                    onClick={handleBatchDelete}
                >
                    {t('vulnerabilityPage.batchDelete', null, '批量删除')}
                </Button>
            )}
            {canDelete && !groupSelection && !groupSearch && hasActiveFilters(filters) && (
                <Button size="small" type="danger" theme="borderless" onClick={handleDeleteFiltered}>
                    {t('vulnerabilityPage.deleteFiltered', null, '删除筛选结果')}
                </Button>
            )}
        </div>
    );

    return (
        <div className="vulnerability-page">
            <PageHeader
                title={t('vulnerability.title', null, '漏洞管理')}
                description={t('vulnerabilityPage.workspaceSubtitle', null, '资产、上报与处置进展')}
                extra={(
                    <>
                        <Button icon={<IconRefresh />} onClick={refreshAll} loading={list.loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button onClick={handleExport} loading={exporting}>
                            {t(groupSelection ? 'vulnerabilityPage.exportAllGroups' : 'vulnerabilityPage.batchExport')}
                        </Button>
                        {canWrite && (
                            <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => openEditor(null)}>
                                {t('vulnerability.addVuln', null, '添加漏洞')}
                            </Button>
                        )}
                    </>
                )}
            />

            <StatGrid metrics={metrics} columns={6} />

            <ErrorBanner error={pageError} onRetry={refreshAll} />

            <SectionCard
                className="vulnerability-list-card"
                title={t('vulnerabilityPage.listTitle', null, '漏洞记录')}
                extra={(
                    <RadioGroup
                        aria-label={t('vulnerabilityPage.viewLabel')}
                        type="button"
                        value={groupView || 'none'}
                        options={Object.entries(VIEWS).map(([value, key]) => ({ value, label: t(`vulnerabilityPage.${key}`) }))}
                        onChange={event => changeGroupView(event.target.value)}
                    />
                )}
            >
                <FilterBar
                    t={t}
                    filters={filters}
                    options={options.data}
                    projectOptions={projectOptions}
                    hasGroupFilters={Boolean(groupSelection || groupSearch)}
                    onChange={applyFilter}
                    onReset={resetFilters}
                />
                <div className={groupView ? 'vulnerability-group-layout' : undefined}>
                    {groupView && (
                        <GroupNavigator
                            key={groupView}
                            t={t}
                            mode={groupView}
                            groups={groups}
                            selection={groupSelection}
                            search={groupSearch}
                            loading={list.loading}
                            onSearch={searchGroups}
                            onSelect={selectGroup}
                        />
                    )}
                    <div className="vulnerability-group-results">
                        {selectedGroup && <GroupOverview t={t} group={selectedGroup} />}
                        <VulnTable
                            t={t}
                            data={listData?.vulnerabilities}
                            loading={list.loading}
                            page={page}
                            pageSize={pageSize}
                            total={totalRecords}
                            selection={listData ? selection : []}
                            onSelectionChange={setSelection}
                            onOpen={openDetail}
                            onEdit={openEditor}
                            onDelete={handleDelete}
                            onDownload={handleDownload}
                            sort={filters.sort}
                            onSort={value => applyFilter('sort', value)}
                            canWrite={canWrite}
                            canDelete={canDelete}
                            onPageChange={handlePageChange}
                            onPageSizeChange={handlePageSizeChange}
                            toolbar={toolbar}
                            onRefresh={refreshAll}
                        />
                    </div>
                </div>
            </SectionCard>

            <DetailDrawer
                visible={Boolean(detailId)}
                vulnId={detailId}
                refreshKey={detailVersion}
                onClose={closeDetail}
                onOpenVuln={openDetail}
                onEdit={openEditor}
                onDelete={handleDelete}
                canWrite={canWrite}
                canDelete={canDelete}
                t={t}
                projectNames={projectNames}
            />

            <EditModal
                visible={editor.visible}
                vulnId={editor.id}
                t={t}
                projectOptions={formProjectOptions}
                onClose={() => setEditor({ visible: false, id: '' })}
                onSaved={handleSaved}
            />
        </div>
    );
}
