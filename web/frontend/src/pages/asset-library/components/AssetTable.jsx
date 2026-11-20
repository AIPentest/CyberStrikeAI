/** 资产库表格：7 列以内，长字段截断，行内查看/编辑/删除。 */
import { Button, Space, Typography } from '@douyinfe/semi-ui';
import { formatDateTime } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { DataTable, confirmAction, toastError } from '../../../components/ui';
import { AssetStatusTag, ProtocolTag, RiskTag, TooltipText } from './AssetBits';
import { assetTargetLabel } from './assetTarget';

const { Text } = Typography;

export default function AssetTable({
    assets, loading, total, page, pageSize, onPageChange, onPageSizeChange,
    selectedRowKeys, onSelectionChange, onView, onEdit, onDelete, onRefresh, canWrite, canDelete
}) {
    const t = useT();

    const handleDelete = record => {
        confirmAction({
            title: t('assets.deleteConfirm', null, '确定删除该资产吗？'),
            content: assetTargetLabel(record),
            okText: t('common.delete', null, '删除'),
            onOk: () => {
                Promise.resolve(onDelete(record)).catch(error => toastError(error, t('assets.deleteFailed', null, '删除资产失败')));
            }
        });
    };

    const columns = [
        {
            title: t('assets.target', null, '目标'),
            dataIndex: 'host',
            width: 260,
            render: (_, record) => (
                <div className="asset-library-target">
                    <TooltipText text={assetTargetLabel(record)} max={38} />
                    <Text type="tertiary" size="small">
                        {[record.ip, record.domain].filter(Boolean).join(' · ') || '—'}
                    </Text>
                </div>
            )
        },
        {
            title: t('assets.service', null, '服务'),
            dataIndex: 'protocol',
            width: 150,
            render: (_, record) => <ProtocolTag protocol={record.protocol} port={record.port} />
        },
        {
            title: t('assets.project', null, '所属项目'),
            dataIndex: 'project_name',
            width: 160,
            render: (value, record) => <TooltipText text={value || record.project_id || t('assets.scopeUnassigned', null, '未归属项目')} max={18} />
        },
        {
            title: t('assets.source', null, '来源'),
            dataIndex: 'source',
            width: 120,
            render: value => <TooltipText text={value} max={14} />
        },
        {
            title: t('assets.riskLevel', null, '风险等级'),
            dataIndex: 'risk_level',
            width: 150,
            render: (_, record) => (
                <Space spacing={6}>
                    <RiskTag asset={record} />
                    <Text type="tertiary" size="small">{Number(record.vulnerability_count || 0)}</Text>
                </Space>
            )
        },
        {
            title: t('assets.lastSeen', null, '最近发现'),
            dataIndex: 'last_seen_at',
            width: 170,
            render: value => <Text size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 190,
            render: (_, record) => (
                <Space spacing={4}>
                    <Button theme="borderless" size="small" onClick={() => onView(record)}>
                        {t('common.view', null, '查看')}
                    </Button>
                    {canWrite && (
                        <Button theme="borderless" size="small" onClick={() => onEdit(record)}>
                            {t('common.edit', null, '编辑')}
                        </Button>
                    )}
                    {canDelete && (
                        <Button theme="borderless" type="danger" size="small" onClick={() => handleDelete(record)}>
                            {t('common.delete', null, '删除')}
                        </Button>
                    )}
                </Space>
            )
        }
    ];

    return (
        <DataTable
            columns={columns}
            dataSource={assets}
            loading={loading}
            rowKey="id"
            onRefresh={onRefresh}
            rowSelection={{
                selectedRowKeys,
                onChange: (keys, rows) => onSelectionChange(keys, rows)
            }}
            pagination={{
                currentPage: page,
                pageSize,
                total: total || 0,
                showSizeChanger: true,
                pageSizeOpts: [10, 20, 50, 100],
                onPageChange,
                onPageSizeChange
            }}
            emptyText={t('common.noData', null, '暂无数据')}
        />
    );
}
