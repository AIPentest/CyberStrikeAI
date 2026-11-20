/**
 * 审计日志：GET /audit/logs（分页/筛选）、GET /audit/logs/:id、GET /audit/logs/export、
 * GET /audit/summary、GET /audit/meta。表格 + 详情 SideSheet + JSON/CSV 导出。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Banner, Button, DatePicker, Input, Select, SideSheet, Space, Tag, Tooltip, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api, downloadFile } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../components/hooks';
import {
    DataTable, ErrorBanner, JsonViewer, KeyValueList, SectionCard, StatGrid, toastError, toastSuccess
} from '../../../components/ui';
import { formatDateTime, formatNumber, truncate } from '../../../lib/format';
import { LoadingBlock, Mono, PermissionBanner } from './common';

const { Text } = Typography;

/** 与 web/static/js/audit.js 及后端 category/action 取值一致。 */
const ACTIONS_BY_CATEGORY = {
    auth: ['login', 'logout', 'change_password'],
    config: ['apply', 'update'],
    c2: ['listener_create', 'listener_delete', 'listener_start', 'listener_stop',
        'session_delete', 'task_create', 'task_cancel', 'task_delete'],
    webshell: ['connection_create', 'connection_delete'],
    knowledge: ['item_delete', 'index_build', 'index_rebuild_full'],
    conversation: ['create', 'delete', 'delete_turn'],
    vulnerability: ['create', 'update', 'delete'],
    external_mcp: ['upsert', 'delete'],
    task: ['create_queue', 'start_queue', 'delete_queue', 'pause_queue', 'rerun_queue', 'delete_batch_task'],
    tool: ['execution_delete', 'execution_delete_batch'],
    file: ['upload', 'delete'],
    hitl: ['decision', 'audit_strategy_update'],
    role: ['create', 'update', 'delete'],
    skill: ['create', 'update', 'delete'],
    agent: ['markdown_create', 'markdown_update', 'markdown_delete']
};

const EMPTY_FILTERS = { category: '', action: '', result: '', q: '', since: null, until: null };

function categoryI18nKey(category) {
    return category === 'external_mcp' ? 'externalMcp' : category;
}

function buildQuery(filters) {
    const query = {};
    if (filters.category) query.category = filters.category;
    if (filters.action) query.action = filters.action;
    if (filters.result) query.result = filters.result;
    if (filters.q) query.q = filters.q;
    if (filters.since) query.since = filters.since.toISOString();
    if (filters.until) query.until = filters.until.toISOString();
    return query;
}

function ResultTag({ result, label }) {
    const key = String(result || '').toLowerCase();
    const color = key === 'failure' || key === 'failed' || key === 'error' ? 'red' : key === 'success' ? 'green' : 'grey';
    return <Tag size="small" color={color}>{label || result || '—'}</Tag>;
}

export default function AuditSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('audit:read');

    const meta = useResource(() => api.get('/audit/meta'), [canRead], { skip: !canRead });

    const [draft, setDraft] = useState(EMPTY_FILTERS);
    const [applied, setApplied] = useState(EMPTY_FILTERS);
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [detailId, setDetailId] = useState('');
    const [exporting, setExporting] = useState('');

    const query = useMemo(() => buildQuery(applied), [applied]);
    const summaryLoader = useCallback(() => api.get('/audit/summary', query), [query]);
    const summary = useResource(summaryLoader, [query, canRead], { skip: !canRead });

    const logsLoader = useCallback(
        () => api.get('/audit/logs', { ...query, page, page_size: pageSize }),
        [query, page, pageSize]
    );
    const logs = useResource(logsLoader, [query, page, pageSize, canRead], { skip: !canRead });

    const categoryLabel = useCallback(category => (
        category ? t(`settingsAudit.cat.${categoryI18nKey(category)}`, null, category) : '—'
    ), [t]);
    const actionLabel = useCallback(action => (
        action ? t(`settingsAudit.act.${action}`, null, action) : '—'
    ), [t]);
    const resultLabel = useCallback(result => (
        result ? t(`settingsAudit.result.${result}`, null, result) : '—'
    ), [t]);

    const categories = useMemo(() => [
        { value: '', label: t('settingsAudit.filterAllCategories', null, '全部类别') },
        ...Object.keys(ACTIONS_BY_CATEGORY).map(key => ({ value: key, label: categoryLabel(key) }))
    ], [categoryLabel, t]);

    const actions = useMemo(() => {
        const list = draft.category ? (ACTIONS_BY_CATEGORY[draft.category] || []) : [];
        return [
            { value: '', label: t('settingsAudit.filterAllActions', null, '全部操作') },
            ...list.map(key => ({ value: key, label: actionLabel(key) }))
        ];
    }, [actionLabel, draft.category, t]);

    const applyFilters = () => {
        const next = { ...draft };
        if (!next.category) next.action = '';
        setApplied(next);
        setPage(1);
    };

    const resetFilters = () => {
        setDraft(EMPTY_FILTERS);
        setApplied(EMPTY_FILTERS);
        setPage(1);
    };

    const refreshAll = () => {
        summary.reload();
        logs.reload();
    };

    const exportLogs = async format => {
        if (!canRead) return;
        setExporting(format);
        try {
            const date = new Date().toISOString().slice(0, 10);
            const filename = `audit-logs-${date}.${format === 'csv' ? 'csv' : 'json'}`;
            await downloadFile('/audit/logs/export', {
                query: { ...query, format: format === 'csv' ? 'csv' : undefined },
                filename
            });
            toastSuccess(t('settingsAudit.exportDone', null, '导出完成'));
        } catch (error) {
            toastError(error, t('common.downloadFailed', null, '导出失败'));
        } finally {
            setExporting('');
        }
    };

    if (!canRead) {
        return <PermissionBanner permission="audit:read" label={t('settings.nav.audit', null, '读取审计日志')} />;
    }
    if (meta.loading && !meta.data) return <LoadingBlock rows={6} />;

    const total = Number(summary.data?.total) || 0;
    const failures = Number(summary.data?.failures) || 0;
    const recent7d = Number(summary.data?.recent_7d) || 0;

    const columns = [
        {
            title: t('settingsAudit.colTime', null, '时间'),
            dataIndex: 'createdAt',
            width: 180,
            render: value => formatDateTime(value)
        },
        {
            title: t('settingsAudit.colMessage', null, '说明'),
            dataIndex: 'message',
            render: (value, row) => (
                <Tooltip content={value || '—'} position="topLeft">
                    <Space spacing={6}>
                        {row.level && row.level !== 'info' && (
                            <Tag size="small" color={row.level === 'error' ? 'red' : 'amber'}>{row.level}</Tag>
                        )}
                        <Text>{truncate(value || '—', 68)}</Text>
                    </Space>
                </Tooltip>
            )
        },
        {
            title: t('settingsAudit.colCategory', null, '类别'),
            dataIndex: 'category',
            width: 120,
            render: value => <Tag size="small" color="blue">{categoryLabel(value)}</Tag>
        },
        {
            title: t('settingsAudit.colAction', null, '操作'),
            dataIndex: 'action',
            width: 170,
            render: value => <Text type="tertiary">{actionLabel(value)}</Text>
        },
        {
            title: t('settingsAudit.colResult', null, '结果'),
            dataIndex: 'result',
            width: 100,
            render: value => <ResultTag result={value} label={resultLabel(value)} />
        },
        {
            title: t('settingsAudit.colIp', null, 'IP'),
            dataIndex: 'clientIp',
            width: 140,
            render: value => <Mono>{value || '—'}</Mono>
        },
        {
            title: t('settingsAudit.colResource', null, '资源 ID'),
            dataIndex: 'resourceId',
            width: 200,
            render: (value, row) => (
                <Tooltip content={[row.resourceType, value].filter(Boolean).join(' · ') || '—'} position="topLeft">
                    <Text type="tertiary">{truncate(value || '—', 22)}</Text>
                </Tooltip>
            )
        }
    ];

    return (
        <>
            <ErrorBanner error={meta.error} onRetry={meta.reload} />
            {meta.data?.enabled === false && (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    className="settings-audit-banner"
                    description={t('settingsAudit.disabledHint', null, '审计功能已关闭，新操作不会写入审计表。')}
                />
            )}
            <StatGrid
                columns={4}
                metrics={[
                    { id: 'total', label: t('settingsAudit.statTotal', null, '当前筛选'), value: formatNumber(total) },
                    { id: 'ok', label: t('settingsAudit.statSuccess', null, '成功'), value: formatNumber(Math.max(0, total - failures)) },
                    { id: 'fail', label: t('settingsAudit.statFailures', null, '失败'), value: formatNumber(failures), tone: failures ? 'danger' : undefined },
                    { id: 'recent', label: t('settingsAudit.statRecent7d', null, '近 7 天'), value: formatNumber(recent7d) }
                ]}
            />
            <ErrorBanner error={summary.error} onRetry={summary.reload} />

            <SectionCard
                title={t('settingsAudit.title', null, '日志审计')}
                description={t('settingsAudit.description', null, '记录平台管理类操作（登录、配置、删除等），不记录对话正文与每次工具调用。')}
                extra={(
                    <Space>
                        <Tag size="small" color="grey">
                            {t('settingsAudit.retentionHint', { days: meta.data?.retention_days ?? 0 }, `审计记录保留 ${meta.data?.retention_days ?? 0} 天`)}
                        </Tag>
                        <Button loading={exporting === 'json'} onClick={() => exportLogs('json')}>
                            {t('settingsAudit.exportJson', null, '导出 JSON')}
                        </Button>
                        <Button loading={exporting === 'csv'} onClick={() => exportLogs('csv')}>
                            {t('settingsAudit.exportCsv', null, '导出 CSV')}
                        </Button>
                    </Space>
                )}
            >
                <div className="settings-audit-filters">
                    <Select
                        value={draft.category}
                        onChange={value => setDraft(current => ({ ...current, category: value, action: '' }))}
                        optionList={categories}
                        style={{ width: 150 }}
                    />
                    <Select
                        value={draft.action}
                        onChange={value => setDraft(current => ({ ...current, action: value }))}
                        optionList={actions}
                        disabled={!draft.category}
                        placeholder={draft.category
                            ? t('settingsAudit.filterAllActions', null, '全部操作')
                            : t('settingsAudit.filterCascadeHint', null, '选择类别后可筛选具体操作')}
                        style={{ width: 170 }}
                    />
                    <Select
                        value={draft.result}
                        onChange={value => setDraft(current => ({ ...current, result: value }))}
                        style={{ width: 130 }}
                        optionList={[
                            { value: '', label: t('settingsAudit.filterAll', null, '全部') },
                            { value: 'success', label: t('settingsAudit.result.success', null, '成功') },
                            { value: 'failure', label: t('settingsAudit.result.failure', null, '失败') }
                        ]}
                    />
                    <Input
                        prefix={<IconSearch />}
                        value={draft.q}
                        onChange={value => setDraft(current => ({ ...current, q: value }))}
                        placeholder={t('settingsAudit.filterQueryPlaceholder', null, '消息 / 资源 ID / 操作名')}
                        style={{ width: 240 }}
                        showClear
                    />
                    <DatePicker
                        type="dateTime"
                        value={draft.since || undefined}
                        onChange={value => setDraft(current => ({ ...current, since: value || null }))}
                        placeholder={t('settingsAudit.filterSince', null, '开始时间')}
                        style={{ width: 200 }}
                    />
                    <DatePicker
                        type="dateTime"
                        value={draft.until || undefined}
                        onChange={value => setDraft(current => ({ ...current, until: value || null }))}
                        placeholder={t('settingsAudit.filterUntil', null, '结束时间')}
                        style={{ width: 200 }}
                    />
                    <Button theme="solid" type="primary" onClick={applyFilters}>
                        {t('settingsAudit.filterBtn', null, '筛选')}
                    </Button>
                    <Button onClick={resetFilters}>
                        {t('settingsAudit.resetBtn', null, '重置')}
                    </Button>
                    <Button icon={<IconRefresh />} theme="borderless" onClick={refreshAll} loading={logs.loading || summary.loading}>
                        {t('common.refresh', null, '刷新')}
                    </Button>
                </div>
                <ErrorBanner error={logs.error} onRetry={logs.reload} />
                <DataTable
                    columns={columns}
                    dataSource={Array.isArray(logs.data?.logs) ? logs.data.logs : []}
                    rowKey="id"
                    loading={logs.loading}
                    emptyText={t('settingsAudit.empty', null, '暂无审计记录')}
                    onRow={record => ({
                        onClick: () => setDetailId(record.id),
                        style: { cursor: 'pointer' }
                    })}
                    pagination={{
                        currentPage: page,
                        pageSize,
                        total: Number(logs.data?.total) || 0,
                        showSizeChanger: true,
                        pageSizeOpts: [10, 20, 50, 100],
                        onPageChange: next => setPage(next),
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                />
            </SectionCard>

            <SideSheet
                title={t('settingsAudit.detailTitle', null, '审计详情')}
                visible={Boolean(detailId)}
                onCancel={() => setDetailId('')}
                width={640}
                placement="right"
            >
                {detailId && <AuditDetail id={detailId} categoryLabel={categoryLabel} actionLabel={actionLabel} resultLabel={resultLabel} />}
            </SideSheet>
        </>
    );
}

function AuditDetail({ id, categoryLabel, actionLabel, resultLabel }) {
    const t = useT();
    const [state, setState] = useState({ loading: true, error: null, log: null });
    useEffect(() => {
        let cancelled = false;
        setState({ loading: true, error: null, log: null });
        api.get(`/audit/logs/${encodeURIComponent(id)}`)
            .then(payload => { if (!cancelled) setState({ loading: false, error: null, log: payload?.log || null }); })
            .catch(error => { if (!cancelled) setState({ loading: false, error, log: null }); });
        return () => { cancelled = true; };
    }, [id]);

    if (state.loading) return <LoadingBlock rows={6} />;
    if (state.error) return <ErrorBanner error={state.error} />;
    const log = state.log;
    if (!log) return <Text type="tertiary">{t('settingsAudit.empty', null, '暂无审计记录')}</Text>;

    return (
        <div className="settings-audit-detail">
            <KeyValueList
                columns={2}
                items={[
                    { label: t('settingsAudit.detailTime', null, '时间'), value: formatDateTime(log.createdAt) },
                    { label: t('settingsAudit.detailResult', null, '结果'), value: <ResultTag result={log.result} label={resultLabel(log.result)} /> },
                    { label: t('settingsAudit.detailCategory', null, '类别'), value: categoryLabel(log.category) },
                    { label: t('settingsAudit.colAction', null, '操作'), value: actionLabel(log.action) },
                    { label: t('settingsDisplay.actor', null, '操作者'), value: log.actor || '—' },
                    { label: t('settingsAudit.colIp', null, 'IP'), value: <Mono>{log.clientIp || '—'}</Mono> },
                    { label: t('settingsDisplay.level', null, '日志级别'), value: log.level || '—' },
                    { label: t('settingsDisplay.resourceType', null, '资源类型'), value: log.resourceType || '—' },
                    { label: t('settingsAudit.resourceIdLabel', null, '资源 ID'), value: <Mono>{log.resourceId || '—'}</Mono> },
                    {
                        label: t('settingsDisplay.resourceAvailable', null, '资源是否可访问'),
                        value: log.resourceAvailable == null ? '—' : log.resourceAvailable ? t('common.yes', null, '是') : t('common.no', null, '否')
                    },
                    { label: t('settingsDisplay.session', null, '会话标识'), value: <Mono>{log.sessionHint || '—'}</Mono> },
                    { label: t('settingsDisplay.userAgent', null, '客户端标识'), value: truncate(log.userAgent || '—', 80) }
                ]}
            />
            <div className="settings-audit-message-block">
                <Text type="tertiary" size="small">{t('settingsAudit.detailMessage', null, '说明')}</Text>
                <Text>{log.message || '—'}</Text>
            </div>
            {log.detail && <JsonViewer value={log.detail} collapsed title={t('settingsDisplay.details', null, '详情')} />}
        </div>
    );
}
