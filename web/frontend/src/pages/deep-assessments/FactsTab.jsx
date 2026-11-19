/** 事实：已观察 / 推断记录，含原始回执引用。 */
import { useEffect, useState } from 'react';
import { Button, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { SectionCard, DataTable, ErrorBanner, KeyValueList, JsonViewer, Drawer } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { CONFIDENCE_COLOR, confidenceText, truncateText } from './labels';

const { Text, Paragraph } = Typography;

export default function FactsTab({ id, reloadToken }) {
    const t = useT();
    const [confidence, setConfidence] = useState('');
    const [offset, setOffset] = useState(0);
    const [selected, setSelected] = useState(null);

    useEffect(() => { setOffset(0); }, [confidence]);

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/facts`, { confidence, offset }),
        [id, confidence, offset, reloadToken]
    );
    const data = resource.data;
    const facts = data?.facts || [];

    const columns = [
        {
            title: '置信度',
            dataIndex: 'confidence',
            width: 110,
            render: value => <Tag color={CONFIDENCE_COLOR[value] || 'grey'} shape="circle">{confidenceText(t, value)}</Tag>
        },
        {
            title: '摘要',
            dataIndex: 'summary',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text ellipsis={{ showTooltip: true }}>{value || record.id}</Text>
                    {record.body ? <Text type="tertiary" size="small" className="assessment-ellipsis">{truncateText(record.body, 140)}</Text> : null}
                </div>
            )
        },
        {
            title: '来源意图',
            dataIndex: 'step_id',
            width: 140,
            render: value => value || '—'
        },
        {
            title: t('deepAssessments.assets', null, '关联资产'),
            dataIndex: 'asset_ids',
            width: 140,
            render: value => (value || []).length ? <Text ellipsis={{ showTooltip: true }}>{value.join(', ')}</Text> : '—'
        },
        {
            title: '证据回执',
            dataIndex: 'evidence',
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
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.facts', null, '事实')}
                description="observed 表示有原始成功回执支撑；inferred 只是推断，不是证据。"
                extra={<Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>}
            >
                <div className="assessment-toolbar">
                    <Select
                        value={confidence}
                        onChange={value => setConfidence(value || '')}
                        optionList={[
                            { value: '', label: t('deepAssessments.all', null, '全部状态') },
                            { value: 'observed', label: confidenceText(t, 'observed') },
                            { value: 'inferred', label: confidenceText(t, 'inferred') }
                        ]}
                        style={{ width: 160 }}
                    />
                </div>
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={facts}
                    rowKey="id"
                    loading={resource.loading}
                    pagination={false}
                    onRow={record => ({
                        onClick: () => setSelected(record),
                        style: { cursor: 'pointer' }
                    })}
                    emptyText={t('deepAssessments.noFacts', null, '暂无事实记录。')}
                />
                <div className="assessment-pager">
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 20))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">{facts.length ? `${offset + 1} - ${offset + facts.length}` : '0'}</Text>
                        <Button size="small" disabled={!data?.has_more} onClick={() => setOffset(value => value + 20)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
            </SectionCard>

            <Drawer
                visible={Boolean(selected)}
                onCancel={() => setSelected(null)}
                title="事实详情"
                width={620}
                footer={null}
            >
                {selected ? (
                    <div className="assessment-drawer">
                        <KeyValueList
                            columns={2}
                            items={[
                                { label: '置信度', value: <Tag color={CONFIDENCE_COLOR[selected.confidence] || 'grey'}>{confidenceText(t, selected.confidence)}</Tag> },
                                { label: '事实 ID', value: selected.id },
                                { label: '来源意图', value: selected.step_id || '—' },
                                { label: '时间', value: formatDateTime(selected.created_at) },
                                { label: t('deepAssessments.assets', null, '关联资产'), value: (selected.asset_ids || []).join(', ') || '—' },
                                { label: '证据回执', value: (selected.evidence || []).join(', ') || '—' },
                                { label: '来源节点', value: (selected.source_ids || []).join(', ') || '—' }
                            ]}
                        />
                        <section className="assessment-section">
                            <Text strong>摘要</Text>
                            <Paragraph className="assessment-pre">{selected.summary || '—'}</Paragraph>
                        </section>
                        <section className="assessment-section">
                            <Text strong>正文</Text>
                            <Paragraph className="assessment-pre">{selected.body || '—'}</Paragraph>
                        </section>
                        <JsonViewer title="原始记录" value={selected} collapsed />
                    </div>
                ) : null}
            </Drawer>
        </div>
    );
}
