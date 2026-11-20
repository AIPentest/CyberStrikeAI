/** 执行记录列表：筛选、分页、批量选择、行操作。 */
import { Button, Input, Select, Space, Tag } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { DataTable, ErrorBanner } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { formatDateTime } from '../../lib/format';
import {
    STATUS_VALUES, executionDurationMs, formatDurationMs, isRunning, statusLabel, statusTagColor, toolDisplayName
} from './utils';

export function ExecutionStatusTag({ status }) {
    const t = useT();
    return (
        <Tag size="small" color={statusTagColor(status)} shape="square">
            {statusLabel(t, status)}
        </Tag>
    );
}

export default function ExecutionTable({
    data, loading, error, page, pageSize, status, toolInput,
    selected, onQuery, onPageChange, onRefresh, onOpenDetail, onCancel, onDelete,
    onSelectionChange, canWrite, canDelete
}) {
    const t = useT();
    const executions = data?.executions || [];
    const total = Number(data?.total) || 0;

    const statusOptions = [
        { value: 'all', label: t('mcp.filterAll', null, '全部') },
        ...STATUS_VALUES.map(value => ({ value, label: statusLabel(t, value) }))
    ];

    const columns = [
        {
            title: t('mcpMonitor.columnTool', null, '工具'),
            dataIndex: 'toolName',
            render: value => (
                <span className="mcp-monitor-cell-tool">{toolDisplayName(value) || t('mcpMonitor.unknownTool', null, '未知工具')}</span>
            )
        },
        {
            title: t('mcpMonitor.columnStatus', null, '状态'),
            dataIndex: 'status',
            width: 110,
            render: value => <ExecutionStatusTag status={value} />
        },
        {
            title: t('mcpMonitor.columnStartTime', null, '开始时间'),
            dataIndex: 'startTime',
            width: 180,
            render: value => formatDateTime(value)
        },
        {
            title: t('mcpMonitor.columnDuration', null, '耗时'),
            dataIndex: 'duration',
            width: 110,
            render: (value, record) => formatDurationMs(executionDurationMs(record))
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 230,
            render: (id, record) => (
                <Space spacing={4} wrap>
                    <Button size="small" theme="borderless" onClick={() => onOpenDetail(id)}>
                        {t('mcpMonitor.viewDetail', null, '详情')}
                    </Button>
                    {isRunning(record.status) && (
                        <Button
                            size="small"
                            theme="borderless"
                            disabled={!canWrite}
                            onClick={() => onCancel(record)}
                        >
                            {t('mcpMonitor.terminateExecution', null, '终止')}
                        </Button>
                    )}
                    <Button
                        size="small"
                        theme="borderless"
                        type="danger"
                        disabled={!canDelete}
                        onClick={() => onDelete(record)}
                    >
                        {t('common.delete', null, '删除')}
                    </Button>
                </Space>
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
        onPageChange: next => onPageChange(next, pageSize),
        onPageSizeChange: size => onPageChange(1, size)
    };

    return (
        <>
            <ErrorBanner error={error} onRetry={onRefresh} />
            <DataTable
                columns={columns}
                dataSource={executions}
                rowKey="id"
                loading={loading}
                pagination={pagination}
                onRefresh={onRefresh}
                emptyText={t('mcpMonitor.noExecutions', null, '暂无执行记录')}
                rowSelection={{
                    selectedRowKeys: selected,
                    onChange: keys => onSelectionChange(keys || [])
                }}
                toolbar={(
                    <Space spacing={8} wrap>
                        <Input
                            prefix={<IconSearch />}
                            value={toolInput}
                            showClear
                            style={{ width: 220 }}
                            placeholder={t('mcp.toolSearchPlaceholder', null, '输入工具名称…')}
                            onChange={value => onQuery({ toolInput: value })}
                        />
                        <Select
                            value={status}
                            style={{ width: 150 }}
                            optionList={statusOptions}
                            onChange={value => onQuery({ status: value })}
                        />
                    </Space>
                )}
            />
        </>
    );
}
