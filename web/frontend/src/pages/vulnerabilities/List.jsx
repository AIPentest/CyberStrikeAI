/** 漏洞列表：服务端分页表格 + 行内操作 + 多选。 */
import { Button, Select, Tag, Typography } from '@douyinfe/semi-ui';
import { formatDateTime } from '../../lib/format';
import { DataTable, SeverityTag, StatusTag } from '../../components/ui';
import { PAGE_SIZE_OPTIONS, shortId, sortOptions, statusText, targetText } from './shared';

const { Text } = Typography;

function SourceCell({ record }) {
    const tags = [record.conversation_tag, record.task_tag].filter(Boolean);
    if (tags.length) {
        return (
            <span className="vulnerability-source-tags">
                {tags.map(tag => (
                    <Tag key={tag} size="small" color="blue" type="light">{tag}</Tag>
                ))}
            </span>
        );
    }
    if (record.conversation_id) {
        return (
            <Text type="tertiary" className="vulnerability-inline-code" title={record.conversation_id}>
                {shortId(record.conversation_id, 10)}
            </Text>
        );
    }
    return <Text type="tertiary">—</Text>;
}

export default function VulnTable({
    t, data, loading, page, pageSize, total, selection, onSelectionChange,
    onOpen, onEdit, onDelete, onDownload, sort, onSort, canWrite, canDelete,
    onPageChange, onPageSizeChange, toolbar, onRefresh
}) {
    const columns = [
        {
            title: t('vulnerabilityPage.tableTitle', null, '漏洞名称'),
            dataIndex: 'title',
            render: (_, record) => (
                <div className="vulnerability-title-cell">
                    <span className="vulnerability-title-text" title={record.title}>{record.title || '—'}</span>
                    <span className="vulnerability-title-meta">
                        {record.type || '—'}
                        {record.case_id ? ` · ${t('vulnerabilityPage.mergedRecord', null, '已归并')}` : ''}
                    </span>
                </div>
            )
        },
        {
            title: t('vulnerabilityPage.tableSeverity', null, '严重度'),
            dataIndex: 'severity',
            width: 92,
            render: severity => <SeverityTag severity={severity} />
        },
        {
            title: t('vulnerabilityPage.tableStatus', null, '状态'),
            dataIndex: 'status',
            width: 100,
            render: status => <StatusTag status={status} label={statusText(t, status)} />
        },
        {
            title: t('vulnerabilityPage.detailTarget', null, '目标 / 宿主'),
            dataIndex: 'target',
            width: 220,
            render: (_, record) => {
                const target = targetText(record);
                const host = Array.isArray(record.targets) && record.targets.length
                    ? record.targets.map(item => item?.host).filter(Boolean).join(', ')
                    : '';
                return (
                    <div className="vulnerability-target-cell">
                        <span className="vulnerability-target-text" title={target || host}>{target || host || '—'}</span>
                        {target && host && host !== target ? (
                            <span className="vulnerability-target-host" title={host}>{host}</span>
                        ) : null}
                    </div>
                );
            }
        },
        {
            title: t('vulnerabilityPage.tableSource', null, '来源'),
            dataIndex: 'conversation_tag',
            width: 170,
            render: (_, record) => <SourceCell record={record} />
        },
        {
            title: t('vulnerabilityPage.tableDate', null, '创建时间'),
            dataIndex: 'created_at',
            width: 180,
            render: value => <span className="vulnerability-time">{formatDateTime(value)}</span>
        },
        {
            title: t('vulnerabilityPage.tableActions', null, '操作'),
            dataIndex: 'id',
            width: 200,
            render: (_, record) => (
                <div className="vulnerability-row-actions">
                    <Button size="small" theme="borderless" onClick={event => { event.stopPropagation(); onOpen(record.id); }}>
                        {t('vulnerabilityPage.rowView', null, '详情')}
                    </Button>
                    <Button
                        size="small"
                        theme="borderless"
                        onClick={event => { event.stopPropagation(); onDownload(record); }}
                    >
                        {t('vulnerabilityPage.tableDownload', null, '下载')}
                    </Button>
                    {canWrite && (
                        <Button
                            size="small"
                            theme="borderless"
                            onClick={event => { event.stopPropagation(); onEdit(record); }}
                        >
                            {t('common.edit', null, '编辑')}
                        </Button>
                    )}
                    {canDelete && (
                        <Button
                            size="small"
                            theme="borderless"
                            type="danger"
                            onClick={event => { event.stopPropagation(); onDelete(record); }}
                        >
                            {t('common.delete', null, '删除')}
                        </Button>
                    )}
                </div>
            )
        }
    ];

    const tableToolbar = toolbar ? (
        <div className="vulnerability-table-toolbar">
            {toolbar}
            <Select
                className="vulnerability-sort-select"
                size="small"
                value={sort}
                optionList={sortOptions(t)}
                onChange={value => onSort(value || '')}
            />
        </div>
    ) : null;

    return (
        <DataTable
            columns={columns}
            dataSource={Array.isArray(data) ? data : []}
            loading={loading}
            rowKey="id"
            toolbar={tableToolbar}
            onRefresh={onRefresh}
            emptyText={t('vulnerabilityPage.noRecords', null, '暂无漏洞记录')}
            scroll={{ x: 1200 }}
            rowSelection={canDelete ? {
                selectedRowKeys: selection,
                onChange: keys => onSelectionChange(Array.isArray(keys) ? keys : [])
            } : undefined}
            onRow={record => ({
                onClick: event => {
                    if (event.target?.closest?.('button, a, input, label, .semi-checkbox')) return;
                    onOpen(record.id);
                }
            })}
            pagination={{
                currentPage: page,
                pageSize,
                total: Number(total) || 0,
                showSizeChanger: true,
                pageSizeOpts: PAGE_SIZE_OPTIONS,
                onPageChange: nextPage => onPageChange(Number(nextPage)),
                onPageSizeChange: nextSize => onPageSizeChange(Number(nextSize))
            }}
        />
    );
}
