/** 意图（步骤）队列：服务端分页、状态筛选与单条意图详情。 */
import { useEffect, useMemo, useState } from 'react';
import { Button, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useResource } from '../../components/hooks';
import {
    SectionCard, DataTable, ErrorBanner, StatusTag, KeyValueList, Drawer,
    confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatNumber } from '../../lib/format';
import { PERM_WRITE, statusText, truncateText } from './labels';

const { Text, Paragraph } = Typography;

const STATUS_ORDER = [
    'running', 'pending', 'pausing', 'paused', 'completed', 'blocked', 'failed',
    'uncertain', 'cleanup_unconfirmed', 'interrupted', 'retired', 'reconciled'
];

export default function IntentsTab({ id, reloadToken, status, onStatusChange, onChanged, archived }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission(PERM_WRITE);
    const [reconciling, setReconciling] = useState(false);
    const [offset, setOffset] = useState(0);
    const [selected, setSelected] = useState(null);

    useEffect(() => { setOffset(0); }, [status]);

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/intents`, { status, offset }),
        [id, status, offset, reloadToken]
    );
    const data = resource.data;
    const intents = data?.intents || [];
    const counts = data?.counts || {};

    const reconcileStep = step => {
        confirmAction({
            title: '确认已核对',
            content: '确认只解除本步骤的待核对状态，不会自动执行；原有进度和证据保留。',
            okText: '确认已核对',
            danger: false,
            onOk: async () => {
                setReconciling(true);
                try {
                    const fresh = await api.get(`/deep-assessments/${id}/steps/${step.id}`);
                    await api.post(`/deep-assessments/${id}/control/reconcile`, {
                        revision: fresh?.revision,
                        step_id: step.id,
                        reconciled: true,
                        cleanup_confirmed: true
                    });
                    toastSuccess('已记录核对，待核对列表已更新');
                    setSelected(null);
                    await resource.reload();
                    onChanged?.();
                } catch (error) {
                    toastError(error, '核对失败');
                } finally {
                    setReconciling(false);
                }
            }
        });
    };

    const statusOptions = useMemo(() => {
        const keys = STATUS_ORDER.filter(key => counts[key]);
        Object.keys(counts).forEach(key => { if (!keys.includes(key)) keys.push(key); });
        return [
            { value: '', label: `${t('deepAssessments.all', null, '全部状态')} · ${data?.total ?? 0}` },
            ...keys.map(key => ({ value: key, label: `${statusText(t, key)} · ${counts[key]}` }))
        ];
    }, [counts, data?.total, t]);

    const columns = [
        {
            title: t('deepAssessments.status', null, '状态'),
            dataIndex: 'status',
            width: 140,
            render: value => <StatusTag status={value} label={statusText(t, value)} />
        },
        {
            title: '意图',
            dataIndex: 'title',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text>
                    {record.summary ? <Text type="tertiary" size="small" className="assessment-ellipsis">{truncateText(record.summary, 120)}</Text> : null}
                </div>
            )
        },
        {
            title: t('deepAssessments.priority', null, '优先级'),
            dataIndex: 'priority',
            width: 90,
            render: value => value || 100
        },
        {
            title: t('deepAssessments.assets', null, '关联资产'),
            dataIndex: 'asset_ids',
            width: 100,
            render: value => (value || []).length || '—'
        },
        {
            title: '证据回执',
            dataIndex: 'evidence',
            width: 100,
            render: value => (value || []).length || '—'
        },
        {
            title: '执行分段',
            dataIndex: 'attempts',
            width: 90,
            render: value => formatNumber(value || 0)
        },
        {
            title: '最近进展',
            dataIndex: 'last_progress',
            render: value => value ? <Text type="tertiary" ellipsis={{ showTooltip: true }}>{value}</Text> : '—'
        }
    ];

    return (
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.plan', null, '意图队列')}
                description="队列按优先级排序；状态只表示执行情况，不代表漏洞或完成结论。"
                extra={<Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>}
            >
                <div className="assessment-toolbar">
                    <Select
                        value={status}
                        onChange={value => onStatusChange?.(value || '')}
                        optionList={statusOptions}
                        style={{ width: 200 }}
                    />
                    <Text type="tertiary">共 {data?.total ?? 0} 条意图</Text>
                </div>
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={intents}
                    rowKey="id"
                    loading={resource.loading}
                    pagination={false}
                    onRow={record => ({
                        onClick: () => setSelected(record),
                        style: { cursor: 'pointer' }
                    })}
                    emptyText={t('deepAssessments.noIntents', null, '暂无意图，开始测评后由规划器逐步生成探索方向。')}
                />
                <div className="assessment-pager">
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 20))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">{intents.length ? `${offset + 1} - ${offset + intents.length}` : '0'}</Text>
                        <Button size="small" disabled={!data?.has_more} onClick={() => setOffset(value => value + 20)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
            </SectionCard>

            <Drawer
                visible={Boolean(selected)}
                onCancel={() => setSelected(null)}
                title={selected?.title || '意图详情'}
                width={560}
                footer={null}
            >
                {selected ? (
                    <div className="assessment-drawer">
                        <KeyValueList
                            columns={2}
                            items={[
                                { label: t('deepAssessments.status', null, '状态'), value: <StatusTag status={selected.status} label={statusText(t, selected.status)} /> },
                                { label: '意图 ID', value: selected.id },
                                { label: t('deepAssessments.priority', null, '优先级'), value: selected.priority || 100 },
                                { label: '执行分段', value: selected.attempts || 0 },
                                { label: '手动暂停', value: selected.user_paused ? '是' : '否' },
                                { label: '执行会话', value: selected.conversation_id || '—' },
                                { label: t('deepAssessments.assets', null, '关联资产'), value: (selected.asset_ids || []).join(', ') || '—' },
                                { label: '来源节点', value: (selected.source_ids || []).join(', ') || '—' },
                                { label: '前置意图', value: (selected.depends_on || []).join(', ') || '—' },
                                { label: '证据引用', value: (selected.evidence || []).join(', ') || '—' }
                            ]}
                        />
                        <section className="assessment-section">
                            <Text strong>执行说明</Text>
                            <Paragraph className="assessment-pre">{selected.instruction || '—'}</Paragraph>
                        </section>
                        <section className="assessment-section">
                            <Text strong>结果摘要</Text>
                            <Paragraph className="assessment-pre">{selected.summary || '—'}</Paragraph>
                        </section>
                        {selected.last_progress ? (
                            <section className="assessment-section">
                                <Text strong>最近进展</Text>
                                <Paragraph className="assessment-pre">{selected.last_progress}</Paragraph>
                            </section>
                        ) : null}
                        {Array.isArray(selected.guidance) && selected.guidance.length ? (
                            <section className="assessment-section">
                                <Text strong>定向补充</Text>
                                {selected.guidance.map((note, index) => (
                                    <div key={`${index}-${note.slice(0, 8)}`} className="assessment-guidance-item">
                                        <Tag size="small" color="grey">#{index + 1}</Tag>
                                        <Paragraph className="assessment-pre">{note}</Paragraph>
                                    </div>
                                ))}
                            </section>
                        ) : null}
                        {['uncertain', 'cleanup_unconfirmed'].includes(selected.status) ? (
                            <section className="assessment-section">
                                <Text strong>待核对</Text>
                                <Paragraph type="tertiary" className="assessment-hint">
                                    确认只解除本步骤的待核对状态，不会自动执行；仍有结果不明的调用时请保持停止。
                                </Paragraph>
                                <Button
                                    size="small"
                                    theme="solid"
                                    type="primary"
                                    disabled={!canWrite || archived}
                                    loading={reconciling}
                                    onClick={() => reconcileStep(selected)}
                                >
                                    确认已核对
                                </Button>
                            </section>
                        ) : null}
                        {selected.conversations?.length ? (
                            <section className="assessment-section">
                                <Text strong>历史执行会话</Text>
                                <Paragraph className="assessment-pre">{selected.conversations.join('\n')}</Paragraph>
                            </section>
                        ) : null}
                    </div>
                ) : null}
            </Drawer>
        </div>
    );
}
