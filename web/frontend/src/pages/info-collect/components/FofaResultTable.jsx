/** 检索结果表格：目标列 + 最多 5 个可见字段 + 详情，行多选用于入库。 */
import { Button, Space, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../../lib/i18n';
import { DataTable } from '../../../components/ui';
import { TooltipText } from '../../asset-library/components/AssetBits';
import { inferTarget } from './providers';

const { Text } = Typography;

function renderCell(value) {
    if (value === null || value === undefined || value === '') return <Text type="tertiary">—</Text>;
    const text = Array.isArray(value)
        ? value.join(', ')
        : (typeof value === 'object' ? JSON.stringify(value) : String(value));
    return <TooltipText text={text} max={44} />;
}

function renderTarget(row, fields) {
    const target = inferTarget(row, fields);
    if (!target) return <Text type="tertiary">—</Text>;
    if (/^https?:\/\//i.test(target)) {
        return (
            <a href={target} target="_blank" rel="noreferrer" className="info-collect-target-link">
                <TooltipText text={target} max={44} />
            </a>
        );
    }
    return <TooltipText text={target} max={44} />;
}

export default function FofaResultTable({
    fields, results, visibleFields, selectedKeys, onSelectionChange, onOpenDetail, loading
}) {
    const t = useT();
    const fieldList = Array.isArray(fields) ? fields : [];
    const visible = (Array.isArray(visibleFields) ? visibleFields : []).filter(field => fieldList.includes(field));

    const columns = [
        {
            title: t('assets.target', null, '目标'),
            dataIndex: '__target',
            width: 240,
            render: (_, record) => renderTarget(record, fieldList)
        },
        ...visible.map(field => ({
            title: field,
            dataIndex: field,
            render: (_, record) => renderCell(record[field])
        })),
        {
            title: t('common.actions', null, '操作'),
            dataIndex: '__actions',
            width: 90,
            render: (_, record) => (
                <Button theme="borderless" size="small" onClick={() => onOpenDetail(record)}>
                    {t('common.view', null, '详情')}
                </Button>
            )
        }
    ];

    const dataSource = (results || []).map((row, index) => ({ ...row, __key: String(index), __index: index }));

    return (
        <DataTable
            columns={columns}
            dataSource={dataSource}
            rowKey="__key"
            loading={loading}
            pagination={false}
            rowSelection={{
                selectedRowKeys: selectedKeys,
                onChange: keys => onSelectionChange(keys)
            }}
            emptyText={t('infoCollect.noResults', null, '暂无结果')}
        />
    );
}
