/** 资产：本次测评关联的共享资产、观察覆盖度与手动关联。 */
import { useEffect, useMemo, useState } from 'react';
import { Button, Input, Modal, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useDebounced, useResource } from '../../components/hooks';
import { SectionCard, StatGrid, DataTable, ErrorBanner, KeyValueList, JsonViewer, toastError, toastSuccess, Drawer } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { PERM_WRITE } from './labels';

const { Text } = Typography;

function AssetPicker({ visible, task, onCancel, onSaved }) {
    const t = useT();
    const [query, setQuery] = useState('');
    const [page, setPage] = useState(1);
    const [selected, setSelected] = useState([]);
    const [saving, setSaving] = useState(false);
    const debounced = useDebounced(query, 300);

    useEffect(() => {
        if (!visible) return;
        setQuery('');
        setPage(1);
        setSelected([]);
    }, [visible]);

    useEffect(() => { setPage(1); }, [debounced]);

    const resource = useResource(
        () => api.get('/assets', { page, page_size: 10, q: debounced, project_id: task?.project_id || '' }),
        [page, debounced, task?.project_id, visible],
        { skip: !visible }
    );
    const assets = resource.data?.assets || [];

    const columns = [
        {
            title: '资产',
            dataIndex: 'host',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text strong>{value || record.ip || record.domain || record.id}</Text>
                    <Text type="tertiary" size="small">
                        {[record.ip, record.port ? `:${record.port}` : '', record.protocol].filter(Boolean).join(' ')}
                    </Text>
                </div>
            )
        },
        {
            title: '项目',
            dataIndex: 'project_name',
            width: 160,
            render: value => value || '—'
        },
        {
            title: '状态',
            dataIndex: 'status',
            width: 90,
            render: value => value || '—'
        }
    ];

    const submit = async () => {
        if (!selected.length) return;
        setSaving(true);
        try {
            await api.post(`/deep-assessments/${task.id}/assets`, { asset_ids: selected });
            toastSuccess('已关联资产');
            onSaved?.();
        } catch (error) {
            toastError(error, '关联资产失败');
        } finally {
            setSaving(false);
        }
    };

    return (
        <Modal
            visible={visible}
            title="关联共享资产"
            onCancel={onCancel}
            onOk={submit}
            confirmLoading={saving}
            okText={`关联${selected.length ? ` ${selected.length} 项` : ''}`}
            cancelText="取消"
            okButtonProps={{ disabled: !selected.length }}
            width={760}
        >
            <div className="assessment-toolbar">
                <Input
                    prefix={<IconSearch />}
                    value={query}
                    onChange={setQuery}
                    placeholder="按地址、IP 或域名搜索"
                    style={{ width: 280 }}
                    showClear
                />
                <Text type="tertiary">仅显示当前账号可读、且与测评同项目的资产。</Text>
            </div>
            <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
            <DataTable
                columns={columns}
                dataSource={assets}
                rowKey="id"
                loading={resource.loading}
                pagination={false}
                rowSelection={{
                    selectedRowKeys: selected,
                    onChange: keys => setSelected((keys || []).map(String))
                }}
                emptyText="没有匹配的资产，可先在资产库登记。"
            />
            <div className="assessment-pager">
                <Space>
                    <Button size="small" disabled={page <= 1} onClick={() => setPage(value => Math.max(1, value - 1))}>
                        上一页
                    </Button>
                    <Text type="tertiary">第 {page} 页</Text>
                    <Button size="small" disabled={!resource.data?.total_pages || page >= resource.data.total_pages} onClick={() => setPage(value => value + 1)}>
                        下一页
                    </Button>
                </Space>
            </div>
        </Modal>
    );
}

export default function AssetsTab({ id, task, observation, reloadToken }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const [offset, setOffset] = useState(0);
    const [pickerOpen, setPickerOpen] = useState(false);
    const [selected, setSelected] = useState(null);

    const resource = useResource(
        () => api.get(`/deep-assessments/${id}/assets`, { offset, observation }),
        [id, offset, observation, reloadToken]
    );
    const graph = resource.data;
    const assets = graph?.assets || [];
    const coverage = graph?.coverage || {};
    const linked = coverage.linked ?? graph?.total ?? 0;
    const observed = coverage.with_observed_evidence ?? 0;
    const percent = linked ? Math.round((observed / linked) * 100) : 0;

    const canWrite = hasPermission(PERM_WRITE);

    const metrics = useMemo(() => [
        { id: 'linked', label: '关联资产', value: linked },
        { id: 'observed', label: '已有观察证据', value: observed },
        { id: 'coverage', label: '观察覆盖率', value: `${percent}%`, detail: '只统计当前账号可读的关联资产' },
        { id: 'nodes', label: '资产图节点', value: graph?.nodes?.length ?? 0 }
    ], [linked, observed, percent, graph?.nodes?.length]);

    const columns = [
        {
            title: '资产',
            dataIndex: 'host',
            render: (value, record) => (
                <div className="assessment-list-name">
                    <Text strong>{value || record.ip || record.domain || record.id}</Text>
                    <Text type="tertiary" size="small">{[record.ip, record.domain].filter(Boolean).join(' · ') || record.id}</Text>
                </div>
            )
        },
        {
            title: '项目',
            dataIndex: 'project_name',
            width: 160,
            render: value => value || '—'
        },
        {
            title: '端口 / 协议',
            width: 130,
            render: (_, record) => [record.port || null, record.protocol || null].filter(Boolean).join(' / ') || '—'
        },
        {
            title: '状态',
            dataIndex: 'status',
            width: 100,
            render: value => value ? <Tag size="small">{value}</Tag> : '—'
        },
        {
            title: '风险',
            dataIndex: 'risk_level',
            width: 90,
            render: value => value || '—'
        },
        {
            title: '最近发现',
            dataIndex: 'last_seen_at',
            width: 170,
            render: value => formatDateTime(value)
        }
    ];

    return (
        <div className="assessment-tab">
            <StatGrid metrics={metrics} columns={4} />
            <SectionCard
                title={t('deepAssessments.assets', null, '资产图')}
                description="覆盖情况按已保存的 observed 事实计算，不代表测试穷尽或无漏洞。"
                extra={(
                    <Space>
                        <Button
                            icon={<IconPlus />}
                            size="small"
                            disabled={!canWrite || task?.archived}
                            onClick={() => setPickerOpen(true)}
                        >
                            添加资产
                        </Button>
                        <Button size="small" onClick={() => resource.reload()} loading={resource.loading}>刷新</Button>
                    </Space>
                )}
            >
                <ErrorBanner error={resource.error} onRetry={() => resource.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={assets}
                    rowKey="id"
                    loading={resource.loading}
                    pagination={false}
                    onRow={record => ({ onClick: () => setSelected(record), style: { cursor: 'pointer' } })}
                    emptyText="本次测评尚未关联资产。"
                />
                <div className="assessment-pager">
                    <Space>
                        <Button size="small" disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 20))}>
                            {t('deepAssessments.previous', null, '上一页')}
                        </Button>
                        <Text type="tertiary">共 {graph?.total ?? 0} 项</Text>
                        <Button size="small" disabled={!graph?.has_more} onClick={() => setOffset(value => value + 20)}>
                            {t('deepAssessments.next', null, '下一页')}
                        </Button>
                    </Space>
                </div>
            </SectionCard>

            <AssetPicker
                visible={pickerOpen}
                task={task}
                onCancel={() => setPickerOpen(false)}
                onSaved={() => { setPickerOpen(false); resource.reload(); }}
            />

            <Drawer
                visible={Boolean(selected)}
                onCancel={() => setSelected(null)}
                title="资产详情"
                width={560}
                footer={null}
            >
                {selected ? (
                    <div className="assessment-drawer">
                        <KeyValueList
                            columns={2}
                            items={[
                                { label: '资产', value: selected.host || selected.ip || selected.id },
                                { label: 'ID', value: selected.id },
                                { label: 'IP', value: selected.ip || '—' },
                                { label: '域名', value: selected.domain || '—' },
                                { label: '端口', value: selected.port || '—' },
                                { label: '协议', value: selected.protocol || '—' },
                                { label: '项目', value: selected.project_name || '—' },
                                { label: '状态', value: selected.status || '—' },
                                { label: '风险', value: selected.risk_level || '—' },
                                { label: '最近发现', value: formatDateTime(selected.last_seen_at) }
                            ]}
                        />
                        <JsonViewer title="原始资产记录" value={selected} collapsed />
                    </div>
                ) : null}
            </Drawer>
        </div>
    );
}
