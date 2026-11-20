/** 审批日志：GET /hitl/logs、GET /hitl/logs/:id、DELETE /hitl/logs（批量/清空）。 */
import { useCallback, useMemo, useState } from 'react';
import {
    Button, Input, Select, SideSheet, Space, Spin, Tag, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import {
    CopyButton, DataTable, ErrorBanner, JsonViewer, KeyValueList, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { useDebounced, useResource } from '../../components/hooks';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import ContextBlocks from './blocks';
import {
    decidedByLabel, decisionColor, decisionLabel, decisionStatusLabel, formatHitlTime,
    parsePayload, reviewerLabel, summarizePayload
} from './utils';

const { Text } = Typography;

function LogDetail({ logId, onClose }) {
    const t = useT();
    const { data, loading, error, reload } = useResource(
        () => api.get(`/hitl/logs/${encodeURIComponent(logId)}`),
        [logId],
        { skip: !logId }
    );
    const payload = useMemo(() => parsePayload(data?.payload), [data?.payload]);
    const engineLabel = [data?.auditBackend, data?.auditModel].filter(Boolean).join(' · ');

    return (
        <SideSheet
            visible={Boolean(logId)}
            onCancel={onClose}
            width={720}
            title={t('hitl.logModalView', null, '审批日志详情')}
            footer={<Button onClick={onClose}>{t('common.close', null, '关闭')}</Button>}
        >
            <ErrorBanner error={error} onRetry={reload} />
            {loading && !data && <div className="hitl-panel-loading"><Spin /></div>}
            {data && (
                <div className="hitl-log-detail">
                    <KeyValueList
                        columns={2}
                        items={[
                            { label: t('hitl.colTool', null, '工具'), value: <Text strong>{data.toolName || '—'}</Text> },
                            {
                                label: t('hitl.colDecision', null, '决策'),
                                value: <Tag size="small" color={decisionColor(data.decision)}>{decisionLabel(data.decision)}</Tag>
                            },
                            { label: '状态', value: decisionStatusLabel(data.status) },
                            { label: t('hitl.colDecidedBy', null, '审批方'), value: decidedByLabel(data.decidedBy) },
                            { label: '审批方类型', value: reviewerLabel(data.reviewer) },
                            {
                                label: '审批引擎',
                                value: engineLabel || '—'
                            },
                            {
                                label: t('hitl.colConversation', null, '会话'),
                                value: data.conversationId
                                    ? (
                                        <Space spacing={4}>
                                            <span className="hitl-mono">{data.conversationId}</span>
                                            <CopyButton text={data.conversationId} size="small" />
                                        </Space>
                                    )
                                    : '—'
                            },
                            { label: '模式', value: data.mode || '—' },
                            { label: '创建时间', value: formatHitlTime(data.createdAt) },
                            { label: '裁决时间', value: formatHitlTime(data.decidedAt) },
                            { label: 'ID', value: <span className="hitl-mono">{data.id}</span> },
                            { label: t('hitl.fieldComment', null, '备注'), value: data.comment || '—' }
                        ]}
                    />
                    <ContextBlocks payload={payload} />
                    <JsonViewer title={t('hitl.fieldPayload', null, '载荷')} value={payload} collapsed />
                </div>
            )}
        </SideSheet>
    );
}

export default function LogsPanel({ canWrite, detailId, onOpenDetail, onCloseDetail, refreshKey = 0 }) {
    const t = useT();
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [searchInput, setSearchInput] = useState('');
    const q = useDebounced(searchInput, 350);
    const [decision, setDecision] = useState('all');
    const [decidedBy, setDecidedBy] = useState('all');
    const [selected, setSelected] = useState([]);

    const query = useMemo(() => ({
        q,
        decision: decision === 'all' ? '' : decision,
        decidedBy: decidedBy === 'all' ? '' : decidedBy
    }), [q, decision, decidedBy]);

    const list = useResource(
        () => api.get('/hitl/logs', { page, pageSize, ...query }),
        [page, pageSize, query, refreshKey]
    );

    const items = list.data?.items || [];
    const total = Number(list.data?.total) || 0;
    const retentionDays = Number(list.data?.retentionDays) || 0;

    const reload = useCallback(() => list.reload(), [list.reload]); // eslint-disable-line react-hooks/exhaustive-deps

    const deleteSelected = useCallback(() => {
        if (!selected.length) return;
        confirmAction({
            title: '批量删除审批日志',
            content: `确定删除选中的 ${selected.length} 条审批日志？此操作不可恢复。`,
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del('/hitl/logs', { body: { ids: selected } });
                    toastSuccess(`已删除 ${selected.length} 条审批日志`);
                    setSelected([]);
                    reload();
                } catch (error) {
                    toastError(error, '批量删除审批日志失败');
                }
            }
        });
    }, [reload, selected, t]);

    const clearAll = useCallback(() => {
        confirmAction({
            title: t('hitl.clearAll', null, '清空审计日志'),
            content: q || decision !== 'all' || decidedBy !== 'all'
                ? '将清空当前筛选条件下的全部已裁决日志（不会删除待审批项）。'
                : '将清空全部已裁决审批日志（不会删除待审批项）。',
            okText: t('hitl.clearAll', null, '清空'),
            onOk: async () => {
                try {
                    await api.del('/hitl/logs', { query, body: { all: true } });
                    toastSuccess('审批日志已清空');
                    setSelected([]);
                    setPage(1);
                    reload();
                } catch (error) {
                    toastError(error, '清空审批日志失败');
                }
            }
        });
    }, [decidedBy, decision, q, query, reload, t]);

    const columns = [
        {
            title: t('hitl.colTool', null, '工具'),
            dataIndex: 'toolName',
            render: value => <span className="hitl-mono">{value || '—'}</span>
        },
        {
            title: t('hitl.colConversation', null, '会话'),
            dataIndex: 'conversationId',
            width: 200,
            render: value => <span className="hitl-mono hitl-ellipsis" title={value || ''}>{value || '—'}</span>
        },
        {
            title: t('hitl.colDecision', null, '决策'),
            dataIndex: 'decision',
            width: 100,
            render: value => <Tag size="small" color={decisionColor(value)}>{decisionLabel(value)}</Tag>
        },
        {
            title: t('hitl.colDecidedBy', null, '审批方'),
            dataIndex: 'decidedBy',
            width: 140,
            render: (value, record) => (
                <div className="hitl-log-by">
                    <span>{decidedByLabel(value)}</span>
                    {record.auditBackend && (
                        <span className="hitl-log-engine">{[record.auditBackend, record.auditModel].filter(Boolean).join(' · ')}</span>
                    )}
                </div>
            )
        },
        {
            title: '备注',
            dataIndex: 'comment',
            render: (value, record) => (
                <span title={value || ''}>{value || summarizePayload(parsePayload(record.payload))}</span>
            )
        },
        {
            title: t('hitl.colTime', null, '时间'),
            dataIndex: 'decidedAt',
            width: 180,
            render: (value, record) => formatHitlTime(value || record.createdAt)
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 90,
            render: id => (
                <Button size="small" theme="borderless" onClick={() => onOpenDetail(id)}>
                    {t('common.view', null, '详情')}
                </Button>
            )
        }
    ];

    const pagination = {
        currentPage: page,
        pageSize,
        total,
        showSizeChanger: true,
        pageSizeOpts: [20, 50, 100],
        showTotal: true,
        onPageChange: next => setPage(next),
        onPageSizeChange: size => { setPageSize(size); setPage(1); }
    };

    return (
        <div className="hitl-logs-panel">
            {retentionDays > 0 && (
                <p className="hitl-retention-hint">
                    {t('hitl.retentionHint', { days: retentionDays }, `审批日志保留 ${retentionDays} 天，到期自动清理。`)}
                </p>
            )}
            <ErrorBanner error={list.error} onRetry={reload} />
            <DataTable
                columns={columns}
                dataSource={items}
                rowKey="id"
                loading={list.loading}
                pagination={pagination}
                size="small"
                emptyText={t('hitl.logsEmpty', null, '暂无审批日志')}
                rowSelection={{
                    selectedRowKeys: selected,
                    onChange: keys => setSelected(keys || [])
                }}
                toolbar={(
                    <Space spacing={8} wrap>
                        <Input
                            showClear
                            style={{ width: 220 }}
                            value={searchInput}
                            placeholder={t('hitl.searchPlaceholder', null, '搜索工具、会话或载荷…')}
                            onChange={value => { setSearchInput(value); setPage(1); }}
                        />
                        <Select
                            value={decision}
                            style={{ width: 120 }}
                            optionList={[
                                { value: 'all', label: t('hitl.filterAll', null, '全部') },
                                { value: 'approve', label: t('hitl.decisionApprove', null, '通过') },
                                { value: 'reject', label: t('hitl.decisionReject', null, '拒绝') }
                            ]}
                            onChange={value => { setDecision(value); setPage(1); }}
                        />
                        <Select
                            value={decidedBy}
                            style={{ width: 150 }}
                            optionList={[
                                { value: 'all', label: t('hitl.filterAll', null, '全部') },
                                { value: 'human', label: t('hitl.reviewerHuman', null, '人工') },
                                { value: 'audit_agent', label: t('hitl.reviewerAgent', null, '审计 Agent') },
                                { value: 'system', label: t('hitl.reviewerSystem', null, '系统') },
                                { value: 'manual', label: t('hitl.reviewerManual', null, '手动录入') }
                            ]}
                            onChange={value => { setDecidedBy(value); setPage(1); }}
                        />
                        <Button icon={<IconRefresh />} theme="borderless" size="small" onClick={reload} loading={list.loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button
                            size="small"
                            type="danger"
                            theme="borderless"
                            disabled={!canWrite || !selected.length}
                            onClick={deleteSelected}
                        >
                            批量删除{selected.length ? `（${selected.length}）` : ''}
                        </Button>
                        <Button size="small" type="danger" theme="borderless" disabled={!canWrite || !total} onClick={clearAll}>
                            {t('hitl.clearAll', null, '清空')}
                        </Button>
                    </Space>
                )}
            />
            <LogDetail logId={detailId} onClose={onCloseDetail} />
        </div>
    );
}
