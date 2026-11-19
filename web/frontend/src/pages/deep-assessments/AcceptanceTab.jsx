/** 验收证据：覆盖度、证据关联与来源检索。 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Checkbox, Input, Progress, Select, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useDebounced, useResource } from '../../components/hooks';
import {
    SectionCard, StatGrid, DataTable, ErrorBanner, KeyValueList, JsonViewer, Drawer, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { ACCEPTANCE_COLOR, acceptanceText, stateText, truncateText } from './labels';

const { Text, Paragraph } = Typography;

const STATUS_FILTERS = ['', 'has_evidence', 'missing', 'needs_review'];

function SourceBrowser({ visible, id, onCancel, edit, onEdit, onSave, saving, saveError }) {
    const t = useT();
    const title = edit ? '编辑验收证据关联' : '证据来源';
    const [kind, setKind] = useState('fact');
    const [query, setQuery] = useState('');
    const [offset, setOffset] = useState(0);
    const debounced = useDebounced(query, 300);

    useEffect(() => { setOffset(0); }, [kind, debounced]);

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/acceptance/sources`, { kind, q: debounced, offset }),
        [id, kind, debounced, offset, visible],
        { skip: !visible }
    );
    const data = resource.data;
    const sources = data?.sources || [];
    const field = kind === 'fact' ? 'fact_ids' : 'receipt_ids';
    const toggle = (sourceId, checked) => onEdit(field,
        checked ? [...edit[field], sourceId] : edit[field].filter(value => value !== sourceId)
    );

    const columns = [
        ...(edit ? [{
            key: 'selected', title: '关联', width: 80, render: (_, item) =>
                <Checkbox checked={edit[field].includes(item.id)} disabled={saving}
                    aria-label={`关联 ${item.title || item.id}`}
                    onChange={event => toggle(item.id, event.target.checked)} />
        }] : []),
        {
            title: '证据',
            dataIndex: 'title',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text>
                    <Text type="tertiary" size="small" className="assessment-ellipsis">{record.id}</Text>
                </div>
            )
        },
        {
            title: t('deepAssessments.status', null, '状态'),
            dataIndex: 'state',
            width: 120,
            render: value => <Tag size="small">{stateText(t, value)}</Tag>
        },
        {
            title: '来源意图',
            dataIndex: 'step_id',
            width: 140,
            render: value => value || '—'
        },
        {
            title: '回执引用',
            dataIndex: 'receipt_ids',
            width: 100,
            render: value => (value || []).length || '—'
        },
        {
            title: '时间',
            dataIndex: 'created_at',
            width: 170,
            render: value => formatDateTime(value)
        }
    ];

    return (
        <Drawer
            visible={visible}
            title={title}
            aria-label={title}
            onCancel={onCancel}
            width={900}
            closable={!saving}
            closeOnEsc={!saving}
            maskClosable={!saving}
            footer={edit && (
                <div className="assessment-form-actions">
                    <Button onClick={onCancel} disabled={saving}>取消</Button>
                    <Button theme="solid" onClick={onSave} loading={saving} disabled={saving}>保存证据关联</Button>
                </div>
            )}
        >
            <div className="assessment-toolbar">
                <Select
                    value={kind}
                    onChange={value => setKind(value || 'fact')}
                    optionList={[
                        { value: 'fact', label: '事实' },
                        { value: 'receipt', label: '原始回执' }
                    ]}
                    style={{ width: 140 }}
                />
                <Input
                    prefix={<IconSearch />}
                    value={query}
                    onChange={setQuery}
                    placeholder="按名称、来源意图或 ID 搜索"
                    style={{ width: 280 }}
                    showClear
                />
                <Text type="tertiary">只有原始回执与 observed 事实可作为验收依据。</Text>
            </div>
            <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
            <DataTable
                columns={columns}
                dataSource={sources}
                rowKey="id"
                loading={resource.loading}
                pagination={false}
                emptyText="没有匹配的证据记录。"
            />
            <div className="assessment-pager">
                <Space>
                    <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 20))}>
                        {t('deepAssessments.previous', null, '上一页')}
                    </Button>
                    <Text type="tertiary">{sources.length ? `${offset + 1} - ${offset + sources.length}` : '0'}</Text>
                    <Button size="small" disabled={!data?.has_more} onClick={() => setOffset(value => value + 20)}>
                        {t('deepAssessments.next', null, '下一页')}
                    </Button>
                </Space>
            </div>
            {edit && <div className="assessment-section">
                <Space wrap>
                    {['fact_ids', 'receipt_ids'].flatMap(key => edit[key].map(id => (
                        <Tag key={key + id} closable={!saving} onClose={() => onEdit(key, edit[key].filter(value => value !== id))}>
                            {(key === 'fact_ids' ? '事实' : '回执') + ' · ' + (edit.titles[id] || id)}
                        </Tag>
                    )))}
                </Space>
                <Checkbox checked={edit.needs_review} disabled={saving}
                    onChange={event => onEdit('needs_review', event.target.checked)}>标记待人工复核</Checkbox>
                <TextArea aria-label="复核说明" placeholder="复核说明" value={edit.review_note} disabled={saving}
                    onChange={value => onEdit('review_note', value)} />
                <ErrorBanner error={saveError} />
            </div>}
        </Drawer>
    );
}

export default function AcceptanceTab({ id, criterion, canWrite, reloadToken }) {
    const t = useT();
    const [filter, setFilter] = useState('');
    const [rowNumber, setRowNumber] = useState(null);
    const [edit, setEdit] = useState(null);
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState(null);
    const linkedCriterion = useRef(null);
    const pathSeq = useRef(0);
    const [sourcesOpen, setSourcesOpen] = useState(false);
    const [pathView, setPathView] = useState(null);
    const [pathLoading, setPathLoading] = useState(false);

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/acceptance`),
        [id, reloadToken]
    );
    const rows = resource.data?.rows || [];
    const row = rows.find(item => item.number === rowNumber);
    useEffect(() => {
        if (!criterion || linkedCriterion.current === criterion || !rows.some(item => item.number === criterion)) return;
        linkedCriterion.current = criterion;
        pathSeq.current++;
        setPathView(null);
        setPathLoading(false);
        setRowNumber(criterion);
    }, [criterion, rows]);

    const counts = useMemo(() => {
        const result = { has_evidence: 0, missing: 0, needs_review: 0 };
        rows.forEach(item => { result[item.status] = (result[item.status] || 0) + 1; });
        return result;
    }, [rows]);

    const percent = rows.length ? Math.round((counts.has_evidence / rows.length) * 100) : 0;
    const visibleRows = filter ? rows.filter(item => item.status === filter) : rows;

    const metrics = [
        { id: 'total', label: '验收项', value: rows.length },
        { id: 'has_evidence', label: acceptanceText(t, 'has_evidence'), value: counts.has_evidence || 0 },
        { id: 'missing', label: acceptanceText(t, 'missing'), value: counts.missing || 0 },
        { id: 'needs_review', label: acceptanceText(t, 'needs_review'), value: counts.needs_review || 0 }
    ];

    const openRow = record => {
        pathSeq.current++;
        setRowNumber(record?.number ?? null);
        setPathView(null);
        setPathLoading(false);
    };

    const startEdit = () => {
        if (!row || !canWrite) return;
        setSaveError(null);
        setEdit({
            number: row.number, revision: row.revision,
            fact_ids: row.fact_ids || [], receipt_ids: row.receipt_ids || [],
            needs_review: Boolean(row.needs_review), review_note: row.review_note || '',
            titles: Object.fromEntries([...(row.facts || []), ...(row.receipts || [])].map(item => [item.id, item.title]))
        });
        setSourcesOpen(true);
    };

    const closeSources = () => {
        setSourcesOpen(false);
        setEdit(null);
    };

    const saveMapping = async () => {
        if (!canWrite || !edit || saving) return;
        const { number, revision, fact_ids, receipt_ids, needs_review, review_note } = edit;
        setSaving(true);
        setSaveError(null);
        try {
            await api.put(`/deep-assessments/${encodeURIComponent(id)}/acceptance/${number}`, {
                revision, fact_ids, receipt_ids, needs_review, review_note
            });
            toastSuccess('验收证据关联已保存');
            closeSources();
            await resource.reload();
        } catch (error) {
            setSaveError(error.status === 409 ? new Error('验收项已有修改。请取消编辑，重新打开后再保存。') : error);
            if (error.status === 409) resource.reload();
        } finally {
            setSaving(false);
        }
    };

    const loadPath = async () => {
        if (!row) return;
        const seq = ++pathSeq.current;
        setPathLoading(true);
        try {
            const data = await api.get(`/deep-assessments/${id}/evidence-path`, { criterion: row.number });
            if (seq === pathSeq.current) setPathView(data);
        } catch (error) {
            if (seq === pathSeq.current) toastError(error, '读取证据路径失败');
        } finally {
            if (seq === pathSeq.current) setPathLoading(false);
        }
    };

    const columns = [
        {
            title: '序号',
            dataIndex: 'number',
            width: 70
        },
        {
            title: '验收项',
            dataIndex: 'text',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text ellipsis={{ showTooltip: true }}>{value || '—'}</Text>
                    {record.review_note ? <Text type="tertiary" size="small" className="assessment-ellipsis">复核说明：{truncateText(record.review_note, 80)}</Text> : null}
                </div>
            )
        },
        {
            title: t('deepAssessments.status', null, '状态'),
            dataIndex: 'status',
            width: 120,
            render: value => <Tag color={ACCEPTANCE_COLOR[value] || 'grey'} shape="circle">{acceptanceText(t, value)}</Tag>
        },
        {
            title: '事实',
            dataIndex: 'facts',
            width: 80,
            render: value => (value || []).length
        },
        {
            title: '回执',
            dataIndex: 'receipts',
            width: 80,
            render: value => (value || []).length
        },
        {
            title: '操作',
            width: 110,
            render: (_, record) => (
                <Button size="small" theme="borderless" onClick={() => openRow(record)}>查看证据</Button>
            )
        }
    ];

    return (
        <div className="assessment-tab">
            <StatGrid metrics={metrics} columns={4} />
            <SectionCard
                title={t('deepAssessments.acceptance', null, '验收证据')}
                description="状态反映证据关联情况，验收结论由审阅人核对；观察事实需有本意图的原始成功回执。"
                extra={(
                    <Space>
                        <Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>
                        <Button size="small" onClick={() => setSourcesOpen(true)}>浏览证据来源</Button>
                    </Space>
                )}
            >
                <div className="assessment-toolbar">
                    <Space>
                        <Progress percent={percent} showInfo size="small" strokeWidth={8} style={{ width: 220 }} />
                        <Text type="tertiary">已有证据覆盖 {percent}%</Text>
                    </Space>
                    <Select
                        value={filter}
                        onChange={value => setFilter(value || '')}
                        optionList={STATUS_FILTERS.map(value => ({
                            value,
                            label: value ? `${acceptanceText(t, value)} · ${counts[value] || 0}` : `${t('deepAssessments.all', null, '全部')} · ${rows.length}`
                        }))}
                        style={{ width: 190 }}
                    />
                </div>
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={visibleRows}
                    rowKey="number"
                    loading={resource.loading}
                    pagination={false}
                    onRow={record => ({ onClick: () => openRow(record), style: { cursor: 'pointer' } })}
                    emptyText="没有符合筛选条件的验收项。"
                />
            </SectionCard>

            <SourceBrowser visible={sourcesOpen} id={id} onCancel={closeSources} edit={edit}
                onEdit={(key, value) => setEdit(previous => ({ ...previous, [key]: value }))}
                onSave={saveMapping} saving={saving} saveError={saveError} />

            <Drawer
                visible={Boolean(row)}
                onCancel={() => openRow(null)}
                title={row ? `验收项 ${row.number}` : '验收项'}
                width={720}
                closeOnEsc={!sourcesOpen}
                footer={null}
            >
                {row ? (
                    <div className="assessment-drawer">
                        <KeyValueList
                            columns={1}
                            items={[
                                { label: '验收条件', value: <Paragraph className="assessment-pre">{row.text || '—'}</Paragraph> },
                                { label: t('deepAssessments.status', null, '状态'), value: <Tag color={ACCEPTANCE_COLOR[row.status] || 'grey'}>{acceptanceText(t, row.status)}</Tag> },
                                { label: '复核说明', value: row.review_note || '—' },
                                { label: '人工标记待复核', value: row.needs_review ? '是' : '否' }
                            ]}
                        />
                        <section className="assessment-section">
                            <Text strong>关联事实</Text>
                            <DataTable
                                columns={[
                                    { title: '事实', dataIndex: 'title', render: (value, record) => <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text> },
                                    { title: '状态', dataIndex: 'state', width: 120, render: value => stateText(t, value) },
                                    { title: '回执引用', dataIndex: 'receipt_ids', width: 100, render: value => (value || []).length }
                                ]}
                                dataSource={row.facts || []}
                                rowKey="id"
                                pagination={false}
                                size="small"
                                emptyText="暂无关联事实。"
                            />
                        </section>
                        <section className="assessment-section">
                            <Text strong>关联回执</Text>
                            <DataTable
                                columns={[
                                    { title: '回执', dataIndex: 'title', render: (value, record) => <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text> },
                                    { title: '状态', dataIndex: 'state', width: 120, render: value => stateText(t, value) },
                                    { title: '来源意图', dataIndex: 'step_id', width: 140 }
                                ]}
                                dataSource={row.receipts || []}
                                rowKey="id"
                                pagination={false}
                                size="small"
                                emptyText="暂无关联回执。"
                            />
                        </section>
                        <Space>
                            {canWrite && <Button size="small" theme="solid" onClick={startEdit}>编辑证据关联</Button>}
                            <Button size="small" onClick={loadPath} loading={pathLoading}>读取局部证据图</Button>
                            <Button size="small" theme="borderless" onClick={() => setSourcesOpen(true)}>浏览证据来源</Button>
                        </Space>
                        {pathView ? (
                            <section className="assessment-section">
                                <Text strong>证据路径 · {pathView.title || pathView.root}</Text>
                                <div className="assessment-path-list">
                                    {(pathView.nodes || []).map(item => (
                                        <div key={item.id} className="assessment-path-node">
                                            <Tag size="small">{item.kind}</Tag>
                                            <Text ellipsis={{ showTooltip: true }}>{item.title || item.id}</Text>
                                        </div>
                                    ))}
                                </div>
                                <JsonViewer title="证据路径原始数据" value={pathView} collapsed />
                            </section>
                        ) : null}
                    </div>
                ) : null}
            </Drawer>
        </div>
    );
}
