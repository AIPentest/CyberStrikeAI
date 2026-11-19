/** 证据：原始回执、证据路径、历次测评对照与端点证据。 */
import { useEffect, useMemo, useState } from 'react';
import {
    Button, Input, InputNumber, Select, Space, Tag, Typography
} from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useResource } from '../../components/hooks';
import {
    SectionCard, DataTable, ErrorBanner, KeyValueList, JsonViewer, StatusTag, Drawer, toastWarning
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { kindText, parseMaybeJson, stateText, statusText } from './labels';

const { Text, Paragraph } = Typography;

const ALL_MODES = [
    { value: 'receipts', label: '执行回执' },
    { value: 'path', label: '证据路径' },
    { value: 'history', label: '历次测评', permission: 'asset:read' },
    { value: 'endpoints', label: '端点证据', permission: 'asset:read' }
];

const MAX_RECEIPT_CHARS = 200000;

const receiptValue = value => (
    typeof value === 'string' && value.length > MAX_RECEIPT_CHARS
        ? `${value.slice(0, MAX_RECEIPT_CHARS)}\n…（已截断 ${value.length - MAX_RECEIPT_CHARS} 字符，完整内容请下载原始回执）`
        : parseMaybeJson(value)
);

const PATH_TYPES = [
    { value: 'criterion', label: '验收项序号' },
    { value: 'asset', label: '资产 ID' },
    { value: 'fact', label: '事实 ID' }
];

export default function EvidenceTab({ id, criterion = 0, stepId = '', reloadToken }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const modes = useMemo(
        () => ALL_MODES.filter(item => !item.permission || hasPermission(item.permission)),
        [hasPermission]
    );
    const [mode, setMode] = useState(criterion > 0 ? 'path' : 'receipts');
    const activeMode = modes.some(item => item.value === mode) ? mode : 'receipts';

    const [offset, setOffset] = useState(0);
    const [receipt, setReceipt] = useState(null);

    const receipts = useResource(
        () => api.get(`/deep-assessments/${id}/receipts`, { offset, step_id: stepId }),
        [id, offset, stepId, reloadToken],
        { skip: activeMode !== 'receipts' }
    );

    const [pathType, setPathType] = useState('criterion');
    const [pathValue, setPathValue] = useState(criterion > 0 ? String(criterion) : '');
    const [pathView, setPathView] = useState(null);
    const [pathState, setPathState] = useState({ loading: false, error: null });

    const [assetId, setAssetId] = useState('');
    const [historyQuery, setHistoryQuery] = useState('');
    const [history, setHistory] = useState(null);
    const [historyState, setHistoryState] = useState({ loading: false, error: null });

    const [endpoints, setEndpoints] = useState(null);
    const [endpointState, setEndpointState] = useState({ loading: false, error: null });

    useEffect(() => { setOffset(0); }, [activeMode]);

    const loadPath = async () => {
        const value = pathValue.trim();
        let query = null;
        if (pathType === 'criterion') {
            const number = Number(value);
            if (!Number.isFinite(number) || number < 1) {
                toastWarning('请填写有效的验收项序号');
                return;
            }
            query = { criterion: Math.floor(number) };
        } else if (pathType === 'asset') {
            if (!value) { toastWarning('请填写资产 ID'); return; }
            query = { asset_id: value };
        } else {
            if (!value) { toastWarning('请填写事实 ID'); return; }
            query = { fact_id: value };
        }
        setPathState({ loading: true, error: null });
        try {
            const data = await api.get(`/deep-assessments/${id}/evidence-path`, query);
            setPathView(data);
            setPathState({ loading: false, error: null });
        } catch (error) {
            setPathView(null);
            setPathState({ loading: false, error });
        }
    };

    useEffect(() => {
        if (criterion > 0) loadPath();
        // A different link remounts this view via Workbench's key.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id, criterion]);

    const loadHistory = async () => {
        const value = assetId.trim();
        if (!value) { toastWarning('请填写资产 ID'); return; }
        setHistoryState({ loading: true, error: null });
        setHistoryQuery(value);
        try {
            const data = await api.get(`/deep-assessments/${id}/evidence-history`, { asset_id: value });
            setHistory(data);
            setHistoryState({ loading: false, error: null });
        } catch (error) {
            setHistory(null);
            setHistoryState({ loading: false, error });
        }
    };

    const loadEndpoints = async () => {
        const value = assetId.trim();
        if (!value) { toastWarning('请填写资产 ID'); return; }
        setEndpointState({ loading: true, error: null });
        try {
            const data = await api.get(`/deep-assessments/${id}/endpoint-evidence`, { asset_id: value });
            setEndpoints(data);
            setEndpointState({ loading: false, error: null });
        } catch (error) {
            setEndpoints(null);
            setEndpointState({ loading: false, error });
        }
    };

    const receiptRows = receipts.data?.receipts || [];

    const receiptColumns = [
        {
            title: t('deepAssessments.status', null, '状态'),
            dataIndex: 'status',
            width: 110,
            render: value => <StatusTag status={value} label={statusText(t, value)} />
        },
        {
            title: '工具',
            dataIndex: 'tool',
            width: 200,
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text strong>{value || '—'}</Text>
                    <Text type="tertiary" size="small" className="assessment-ellipsis">{record.id}</Text>
                </div>
            )
        },
        {
            title: '来源意图',
            dataIndex: 'step_id',
            width: 160,
            render: value => value || '—'
        },
        {
            title: '执行 ID',
            dataIndex: 'execution_id',
            render: value => value ? <Text type="tertiary" ellipsis={{ showTooltip: true }}>{value}</Text> : '—'
        },
        {
            title: '操作',
            width: 90,
            render: (_, record) => (
                <Button size="small" theme="borderless" onClick={() => setReceipt(record)}>查看</Button>
            )
        }
    ];

    return (
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.evidence', null, '证据回执')}
                description="回执是执行原始记录；路径与对照只读展示已保存的关联，不改变测评状态。"
                extra={(
                    <Space>
                        <Select
                            value={activeMode}
                            onChange={value => setMode(value || 'receipts')}
                            optionList={modes.map(item => ({ value: item.value, label: item.label }))}
                            style={{ width: 160 }}
                        />
                        <Button
                            size="small"
                            onClick={() => (activeMode === 'receipts' ? receipts.reload() : undefined)}
                            disabled={activeMode !== 'receipts'}
                        >
                            刷新
                        </Button>
                    </Space>
                )}
            >
                {activeMode === 'receipts' ? (
                    <>
                        <ErrorBanner error={receipts.error} onRetry={() => receipts.reload()} />
                        <DataTable
                            columns={receiptColumns}
                            dataSource={receiptRows}
                            rowKey="id"
                            loading={receipts.loading}
                            pagination={false}
                            onRow={record => ({ onClick: () => setReceipt(record), style: { cursor: 'pointer' } })}
                            emptyText={t('deepAssessments.noRecords', null, '暂无记录')}
                        />
                        <div className="assessment-pager">
                            <Space>
                                <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 50))}>
                                    {t('deepAssessments.previous', null, '上一页')}
                                </Button>
                                <Text type="tertiary">共 {receiptRows.length} 条</Text>
                                <Button size="small" disabled={!receipts.data?.has_more} onClick={() => setOffset(value => value + 50)}>
                                    {t('deepAssessments.next', null, '下一页')}
                                </Button>
                            </Space>
                        </div>
                    </>
                ) : null}

                {activeMode === 'path' ? (
                    <>
                        <div className="assessment-toolbar">
                            <Select
                                value={pathType}
                                onChange={value => setPathType(value || 'criterion')}
                                optionList={PATH_TYPES}
                                style={{ width: 150 }}
                            />
                            {pathType === 'criterion' ? (
                                <InputNumber
                                    value={pathValue === '' ? undefined : Number(pathValue)}
                                    onChange={value => setPathValue(value == null ? '' : String(value))}
                                    min={1}
                                    placeholder="验收项序号"
                                    style={{ width: 160 }}
                                />
                            ) : (
                                <Input
                                    value={pathValue}
                                    onChange={setPathValue}
                                    placeholder={pathType === 'asset' ? '资产 ID' : '事实 ID'}
                                    style={{ width: 320 }}
                                    showClear
                                />
                            )}
                            <Button size="small" theme="solid" type="primary" onClick={loadPath} loading={pathState.loading}>
                                读取证据路径
                            </Button>
                        </div>
                        <ErrorBanner error={pathState.error} onRetry={loadPath} />
                        {pathView ? (
                            <>
                                <KeyValueList
                                    columns={3}
                                    items={[
                                        { label: '根节点', value: pathView.title || pathView.root },
                                        { label: '节点数', value: pathView.total ?? (pathView.nodes || []).length },
                                        { label: '已折叠', value: pathView.folded ?? 0 },
                                        { label: '未归属回执', value: pathView.unattributed_receipts ?? 0 },
                                        { label: '相关意图', value: pathView.related_intent_count ?? 0 },
                                        { label: '还有更多', value: pathView.has_more ? '是' : '否' }
                                    ]}
                                />
                                <DataTable
                                    columns={[
                                        { title: '类型', dataIndex: 'kind', width: 120, render: (value, record) => kindText(t, value) || record.kind },
                                        { title: '标题', dataIndex: 'title', render: (value, record) => <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text> },
                                        { title: '状态', dataIndex: 'state', width: 120, render: value => stateText(t, value) },
                                        { title: '来源意图', dataIndex: 'step_id', width: 150, render: value => value || '—' }
                                    ]}
                                    dataSource={pathView.nodes || []}
                                    rowKey="id"
                                    pagination={false}
                                    size="small"
                                    emptyText="证据路径为空。"
                                />
                                <Text type="tertiary" size="small">关联边 {Array.isArray(pathView.edges) ? pathView.edges.length : 0} 条</Text>
                                <JsonViewer title="证据路径原始数据" value={pathView} collapsed />
                            </>
                        ) : null}
                    </>
                ) : null}

                {activeMode === 'history' ? (
                    <>
                        <div className="assessment-toolbar">
                            <Input
                                value={assetId}
                                onChange={setAssetId}
                                placeholder="资产 ID"
                                style={{ width: 340 }}
                                showClear
                            />
                            <Button size="small" theme="solid" type="primary" onClick={loadHistory} loading={historyState.loading}>
                                读取历次测评
                            </Button>
                        </div>
                        <ErrorBanner error={historyState.error} onRetry={loadHistory} />
                        {history ? (
                            <>
                                <Text strong>{history.title || history.asset_id} · 本次测评</Text>
                                <KeyValueList
                                    columns={3}
                                    items={[
                                        { label: '测评', value: history.current?.name || '—' },
                                        { label: '状态', value: statusText(t, history.current?.state) },
                                        { label: '有效观察', value: history.current?.observed_facts ?? 0 },
                                        { label: '证据端点', value: history.current?.observed_endpoints ?? 0 },
                                        { label: '待复核验收项', value: (history.current?.pending_criteria || []).join(', ') || '—' },
                                        { label: '最近观察', value: formatDateTime(history.current?.last_observation) }
                                    ]}
                                />
                                <DataTable
                                    columns={[
                                        { title: '较早测评', dataIndex: 'name', render: (value, record) => <Text ellipsis={{ showTooltip: true }}>{value || record.task_id}</Text> },
                                        { title: '状态', dataIndex: 'state', width: 110, render: value => statusText(t, value) },
                                        { title: '有效观察', dataIndex: 'observed_facts', width: 100 },
                                        { title: '证据端点', dataIndex: 'observed_endpoints', width: 100 },
                                        { title: '新增观察', dataIndex: 'new_observations', width: 100 },
                                        { title: '新增端点', dataIndex: 'new_endpoints', width: 100 },
                                        { title: '前次有本次无', dataIndex: 'unseen_now', width: 120 },
                                        { title: '可比较', dataIndex: 'comparable', width: 90, render: value => (value ? '是' : '否') },
                                        { title: '创建时间', dataIndex: 'created_at', width: 170, render: value => formatDateTime(value) }
                                    ]}
                                    dataSource={history.prior || []}
                                    rowKey="task_id"
                                    pagination={false}
                                    size="small"
                                    emptyText={historyQuery ? '暂无可读的较早测评记录。' : '—'}
                                />
                                <Paragraph type="tertiary" className="assessment-hint">
                                    差异基于两次已保存的证据记录；范围或项目口径不同时保留记录供人工对照。
                                </Paragraph>
                                <JsonViewer title="历次测评原始数据" value={history} collapsed />
                            </>
                        ) : null}
                    </>
                ) : null}

                {activeMode === 'endpoints' ? (
                    <>
                        <div className="assessment-toolbar">
                            <Input
                                value={assetId}
                                onChange={setAssetId}
                                placeholder="资产 ID"
                                style={{ width: 340 }}
                                showClear
                            />
                            <Button size="small" theme="solid" type="primary" onClick={loadEndpoints} loading={endpointState.loading}>
                                读取端点证据
                            </Button>
                        </div>
                        <ErrorBanner error={endpointState.error} onRetry={loadEndpoints} />
                        {endpoints ? (
                            <>
                                <KeyValueList
                                    columns={4}
                                    items={[
                                        { label: '资产', value: endpoints.title || '—' },
                                        { label: '已有观察', value: endpoints.counts?.observed ?? 0 },
                                        { label: '待复核', value: endpoints.counts?.needs_review ?? 0 },
                                        { label: '缺证据', value: endpoints.counts?.missing ?? 0 }
                                    ]}
                                />
                                <DataTable
                                    columns={[
                                        { title: '方法', dataIndex: 'method', width: 90 },
                                        { title: '路径', dataIndex: 'path', render: (value, record) => <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text> },
                                        { title: '名称', dataIndex: 'title', width: 220, render: value => value || '—' },
                                        { title: '证据状态', dataIndex: 'status', width: 120, render: value => <Tag size="small">{stateText(t, value)}</Tag> }
                                    ]}
                                    dataSource={endpoints.endpoints || []}
                                    rowKey="id"
                                    pagination={false}
                                    size="small"
                                    emptyText="该资产暂无登记端点。"
                                />
                                <Paragraph type="tertiary" className="assessment-hint">
                                    端点状态来自明确关联的事实；服务级观察不会计入端点。端点关联的写入在资产图与漏洞复核流程中完成。
                                </Paragraph>
                                <JsonViewer title="端点证据原始数据" value={endpoints} collapsed />
                            </>
                        ) : null}
                    </>
                ) : null}
            </SectionCard>

            <Drawer
                visible={Boolean(receipt)}
                onCancel={() => setReceipt(null)}
                title={receipt ? `${receipt.tool || '回执'} · ${receipt.step_id || ''}` : '回执详情'}
                width={720}
                footer={null}
            >
                {receipt ? (
                    <div className="assessment-drawer">
                        <KeyValueList
                            columns={2}
                            items={[
                                { label: t('deepAssessments.status', null, '状态'), value: <StatusTag status={receipt.status} label={statusText(t, receipt.status)} /> },
                                { label: '回执 ID', value: receipt.id },
                                { label: '来源意图', value: receipt.step_id || '—' },
                                { label: '执行 ID', value: receipt.execution_id || '—' }
                            ]}
                        />
                        <section className="assessment-section">
                            <Text strong>调用参数</Text>
                            <JsonViewer title="arguments" value={receiptValue(receipt.arguments)} collapsed={false} />
                        </section>
                        <section className="assessment-section">
                            <Text strong>调用结果</Text>
                            <JsonViewer title="result" value={receiptValue(receipt.result)} collapsed />
                        </section>
                    </div>
                ) : null}
            </Drawer>
        </div>
    );
}
