/** MCP 状态监控：统计卡、调用趋势、工具排行、执行记录与详情。 */
import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Modal, Space, TextArea } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { PageHeader, SectionCard, toastError, toastSuccess, toastWarning, confirmAction } from '../../components/ui';
import { useDebounced, useInterval, useLocalStorage, useResource } from '../../components/hooks';
import { useAuth } from '../../lib/auth';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { MonitorStats, ToolRankCard } from './stats';
import { CallsTimeline } from './timeline';
import ExecutionTable from './executions';
import ExecutionDetail from './detail';
import { averageDurationMs, executionDurationMs, toolDisplayName } from './utils';
import './page.css';

export default function McpMonitorPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const [searchParams, setSearchParams] = useSearchParams();
    // 终止走 POST /monitor/execution/:id/cancel，RBAC 要求 monitor:write；兼容按 mcp:write 授权。
    const canWrite = hasPermission('monitor:write') || hasPermission('mcp:write');
    const canDelete = hasPermission('monitor:delete');

    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [status, setStatus] = useState('all');
    const [toolInput, setToolInput] = useState('');
    const tool = useDebounced(toolInput, 350);
    const [selected, setSelected] = useState([]);
    const [timelineRange, setTimelineRange] = useLocalStorage('mcp-monitor-timeline-range', '7d');
    const [cancelTarget, setCancelTarget] = useState(null);
    const [cancelNote, setCancelNote] = useState('');
    const [cancelling, setCancelling] = useState(false);

    const detailId = searchParams.get('id') || '';
    const range = ['24h', '7d', '30d'].includes(timelineRange) ? timelineRange : '7d';

    const list = useResource(
        () => api.get('/monitor', {
            page,
            page_size: pageSize,
            status: status === 'all' ? '' : status,
            tool
        }),
        [page, pageSize, status, tool]
    );
    const stats = useResource(() => api.get('/monitor/stats', { top: 12 }), []);
    const timeline = useResource(
        () => api.get('/monitor/calls-timeline', { range }),
        [range]
    );

    const executions = list.data?.executions || [];
    const summary = stats.data?.summary || list.data?.summary || null;
    const topTools = stats.data?.topTools || list.data?.topTools || [];
    const retentionDays = Number(list.data?.retentionDays) || 0;

    const durationSamples = useMemo(
        () => executions.filter(exec => executionDurationMs(exec) !== null).length,
        [executions]
    );
    const avgMs = useMemo(() => averageDurationMs(executions), [executions]);

    // 监控页按旧实现定时刷新；隐藏标签页时跳过，避免无意义请求。
    useInterval(() => {
        if (typeof document !== 'undefined' && document.hidden) return;
        list.reload({ silent: true });
        timeline.reload({ silent: true });
    }, 5000);
    useInterval(() => {
        if (typeof document !== 'undefined' && document.hidden) return;
        stats.reload({ silent: true });
    }, 20000);

    const reloadAll = useCallback(() => {
        list.reload();
        stats.reload({ silent: true });
        timeline.reload({ silent: true });
    }, [list.reload, stats.reload, timeline.reload]); // eslint-disable-line react-hooks/exhaustive-deps

    const openDetail = useCallback(id => {
        const next = new URLSearchParams(searchParams);
        next.set('id', id);
        setSearchParams(next, { replace: true });
    }, [searchParams, setSearchParams]);

    const closeDetail = useCallback(() => {
        const next = new URLSearchParams(searchParams);
        next.delete('id');
        setSearchParams(next, { replace: true });
    }, [searchParams, setSearchParams]);

    const handleQuery = useCallback(patch => {
        if (Object.prototype.hasOwnProperty.call(patch, 'toolInput')) setToolInput(patch.toolInput);
        if (patch.status !== undefined) setStatus(patch.status);
        setPage(1);
        setSelected([]);
    }, []);

    const handlePageChange = useCallback((nextPage, nextSize) => {
        setPage(nextPage);
        if (nextSize && nextSize !== pageSize) setPageSize(nextSize);
    }, [pageSize]);

    const filterByTool = useCallback(toolName => {
        setToolInput(toolDisplayName(toolName));
        setPage(1);
        setSelected([]);
    }, []);

    const requestDelete = useCallback(record => {
        confirmAction({
            title: t('mcpMonitor.deleteExecTitle', null, '删除此执行记录'),
            content: `确定删除 ${toolDisplayName(record.toolName) || '该'} 的执行记录？此操作不可恢复。`,
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/monitor/execution/${encodeURIComponent(record.id)}`);
                    setSelected(prev => prev.filter(id => id !== record.id));
                    toastSuccess(t('mcpMonitor.execDeleted', null, '执行记录已删除'));
                    reloadAll();
                } catch (error) {
                    toastError(error, t('mcpMonitor.deleteExecFailed', null, '删除执行记录失败'));
                }
            }
        });
    }, [reloadAll, t]);

    const batchDelete = useCallback(async () => {
        if (!selected.length) {
            toastWarning(t('mcpMonitor.selectExecFirst', null, '请先选择要删除的执行记录'));
            return;
        }
        let names = [];
        try {
            const result = await api.post('/monitor/executions/names', { ids: selected });
            names = [...new Set(Object.values(result || {}).map(item => toolDisplayName(item?.toolName)).filter(Boolean))];
        } catch (error) {
            names = [];
        }
        const nameHint = names.length
            ? `涉及工具：${names.slice(0, 3).join('、')}${names.length > 3 ? ' 等' : ''}。`
            : '';
        confirmAction({
            title: t('mcpMonitor.batchDeleteConfirm', { count: selected.length }, `确定删除选中的 ${selected.length} 条执行记录？`),
            content: `${nameHint}此操作不可恢复。`,
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del('/monitor/executions', { body: { ids: selected } });
                    toastSuccess(`已删除 ${selected.length} 条执行记录`);
                    setSelected([]);
                    reloadAll();
                } catch (error) {
                    toastError(error, t('mcp.batchDeleteFailed', null, '批量删除执行记录失败'));
                }
            }
        });
    }, [reloadAll, selected, t]);

    const openCancel = useCallback(record => {
        setCancelTarget(record);
        setCancelNote('');
    }, []);

    const submitCancel = useCallback(async () => {
        if (!cancelTarget?.id) return;
        setCancelling(true);
        try {
            await api.post(`/monitor/execution/${encodeURIComponent(cancelTarget.id)}/cancel`, {
                note: cancelNote.trim()
            });
            toastSuccess(t('mcpDetailModal.abortSuccess', null, '已发送终止请求'));
            setCancelTarget(null);
            reloadAll();
        } catch (error) {
            toastError(error, t('mcpDetailModal.abortFailed', null, '终止失败'));
        } finally {
            setCancelling(false);
        }
    }, [cancelNote, cancelTarget, reloadAll, t]);

    const description = retentionDays > 0
        ? t('mcpMonitor.retentionHint', { days: retentionDays }, `执行记录保留 ${retentionDays} 天，失败与拦截调用单独标记。`)
        : '统计全部 MCP 工具调用，失败与拦截调用单独标记。';

    return (
        <div className="mcp-monitor-page">
            <PageHeader
                title={t('mcp.monitorTitle', null, 'MCP 状态监控')}
                description={description}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={reloadAll} loading={list.loading || stats.loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                    </Space>
                )}
            />

            <MonitorStats
                summary={summary}
                loading={stats.loading && !summary}
                avgMs={avgMs}
                avgSamples={durationSamples}
            />

            <CallsTimeline
                data={timeline.data}
                loading={timeline.loading}
                error={timeline.error}
                range={range}
                onRangeChange={nextRange => { if (nextRange) setTimelineRange(nextRange); }}
                onRetry={() => timeline.reload()}
            />

            <ToolRankCard
                topTools={topTools}
                loading={stats.loading && !topTools.length}
                onFilterTool={filterByTool}
                activeTool={tool}
            />

            <SectionCard
                title={t('mcp.latestExecutions', null, '最新执行记录')}
                description={t('mcpMonitor.emptyHint', null, '在对话或任务中调用 MCP 工具后，执行记录将显示在此处')}
                extra={selected.length > 0 ? (
                    <Space spacing={8}>
                        <span className="mcp-monitor-selected">{t('mcp.selectedCount', { count: selected.length }, `已选择 ${selected.length} 项`)}</span>
                        <Button size="small" disabled={!canDelete} type="danger" onClick={batchDelete}>
                            {t('mcp.deleteSelected', null, '批量删除')}
                        </Button>
                        <Button size="small" theme="borderless" onClick={() => setSelected([])}>
                            {t('mcpMonitor.deselectAll', null, '取消全选')}
                        </Button>
                    </Space>
                ) : null}
            >
                <ExecutionTable
                    data={list.data}
                    loading={list.loading}
                    error={list.error}
                    page={page}
                    pageSize={pageSize}
                    status={status}
                    toolInput={toolInput}
                    selected={selected}
                    onQuery={handleQuery}
                    onPageChange={handlePageChange}
                    onRefresh={() => list.reload()}
                    onOpenDetail={openDetail}
                    onCancel={openCancel}
                    onDelete={requestDelete}
                    onSelectionChange={setSelected}
                    canWrite={canWrite}
                    canDelete={canDelete}
                />
            </SectionCard>

            <ExecutionDetail
                executionId={detailId}
                onClose={closeDetail}
                canWrite={canWrite}
                onCancelRequest={record => { closeDetail(); openCancel(record); }}
            />

            <Modal
                title={t('mcpMonitor.terminateExecution', null, '终止执行')}
                visible={Boolean(cancelTarget)}
                confirmLoading={cancelling}
                maskClosable={!cancelling}
                okText={t('mcpMonitor.terminateExecution', null, '终止')}
                cancelText={t('common.cancel', null, '取消')}
                onCancel={() => setCancelTarget(null)}
                onOk={submitCancel}
            >
                <p className="mcp-monitor-cancel-hint">
                    {t('mcpDetailModal.abortHint', null, '终止仅取消本次工具调用，不会停止整条对话；说明会与工具输出一起交给模型。')}
                </p>
                <TextArea
                    rows={4}
                    value={cancelNote}
                    placeholder={t('mcpDetailModal.abortNotePlaceholder', null, '终止说明（可选），例如：命令执行时间过长，先终止。')}
                    onChange={value => setCancelNote(value)}
                />
            </Modal>
        </div>
    );
}
