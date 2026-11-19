/** 关联漏洞：本测评登记的问题列表与独立复核详情。 */
import { useState } from 'react';
import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { SectionCard, DataTable, ErrorBanner, SeverityTag, KeyValueList, JsonViewer, toastError, Drawer } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { truncateText } from './labels';

const { Text, Paragraph } = Typography;

const VULN_STATUS = {
    open: '待处理',
    confirmed: '已确认',
    fixed: '已修复',
    false_positive: '误报',
    ignored: '已忽略'
};

export default function FindingsTab({ id, reloadToken }) {
    const t = useT();
    const [offset, setOffset] = useState(0);
    const [detail, setDetail] = useState(null);
    const [detailLoading, setDetailLoading] = useState(false);

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/vulnerabilities`, { limit: 20, offset }),
        [id, offset, reloadToken]
    );
    const data = resource.data;
    const findings = data?.vulnerabilities || [];

    const openDetail = async record => {
        setDetailLoading(true);
        try {
            const payload = await api.get(`/deep-assessments/${id}/vulnerabilities/${record.id}`);
            setDetail(payload);
        } catch (error) {
            toastError(error, '读取漏洞详情失败');
        } finally {
            setDetailLoading(false);
        }
    };

    const vuln = detail?.vulnerability || null;

    const columns = [
        {
            title: '漏洞标题',
            dataIndex: 'title',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text strong ellipsis={{ showTooltip: true }}>{value || record.id}</Text>
                    {record.target ? <Text type="tertiary" size="small" className="assessment-ellipsis">{record.target}</Text> : null}
                </div>
            )
        },
        {
            title: '严重级别',
            dataIndex: 'severity',
            width: 100,
            render: value => <SeverityTag severity={value} />
        },
        {
            title: t('deepAssessments.status', null, '状态'),
            dataIndex: 'status',
            width: 100,
            render: value => <Tag size="small">{VULN_STATUS[value] || value || '—'}</Tag>
        },
        {
            title: '来源意图',
            dataIndex: 'assessment_step_title',
            width: 220,
            render: (value, record) => value || record.assessment_step_id || '—'
        },
        {
            title: '登记时间',
            dataIndex: 'created_at',
            width: 170,
            render: value => formatDateTime(value)
        },
        {
            title: '操作',
            width: 90,
            render: (_, record) => (
                <Button size="small" theme="borderless" onClick={() => openDetail(record)} loading={detailLoading}>
                    详情
                </Button>
            )
        }
    ];

    return (
        <div className="assessment-tab">
            <SectionCard
                title={t('deepAssessments.findings', null, '关联漏洞')}
                description="已登记的发现仍需要独立复现；报告记录不等于独立确认。"
                extra={<Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>}
            >
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={findings}
                    rowKey="id"
                    loading={resource.loading}
                    pagination={false}
                    onRow={record => ({ onClick: () => openDetail(record), style: { cursor: 'pointer' } })}
                    emptyText="暂未登记漏洞。"
                />
                <div className="assessment-pager">
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 20))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">共 {data?.total ?? 0} 条</Text>
                        <Button size="small" disabled={!data?.has_more} onClick={() => setOffset(value => value + 20)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
            </SectionCard>

            <Drawer
                visible={Boolean(detail)}
                onCancel={() => setDetail(null)}
                title={vuln?.title || '漏洞详情'}
                width={680}
                footer={null}
            >
                {vuln ? (
                    <div className="assessment-drawer">
                        <KeyValueList
                            columns={2}
                            items={[
                                { label: '严重级别', value: <SeverityTag severity={vuln.severity} /> },
                                { label: t('deepAssessments.status', null, '状态'), value: VULN_STATUS[vuln.status] || vuln.status },
                                { label: '类型', value: vuln.type || '—' },
                                { label: '目标', value: vuln.target || '—' },
                                { label: '来源意图', value: detail.step?.title || vuln.assessment_step_title || vuln.assessment_step_id || '—' },
                                { label: '登记时间', value: formatDateTime(vuln.created_at) },
                                { label: '漏洞 ID', value: vuln.id }
                            ]}
                        />
                        {[
                            ['描述', vuln.description],
                            ['前置条件', vuln.preconditions],
                            ['复现步骤', vuln.reproduction_steps],
                            ['复现证据', vuln.evidence],
                            ['影响', vuln.impact],
                            ['修复建议', vuln.recommendation],
                            ['复核说明', vuln.retest_notes]
                        ].filter(([, value]) => value).map(([label, value]) => (
                            <section className="assessment-section" key={label}>
                                <Text strong>{label}</Text>
                                <Paragraph className="assessment-pre">{value}</Paragraph>
                            </section>
                        ))}
                        {detail.step ? (
                            <section className="assessment-section">
                                <Text strong>来源意图摘要</Text>
                                <Paragraph className="assessment-pre">{truncateText(detail.step.summary || detail.step.instruction, 600)}</Paragraph>
                            </section>
                        ) : null}
                        <JsonViewer title="原始漏洞记录" value={detail} collapsed />
                    </div>
                ) : null}
            </Drawer>
        </div>
    );
}
