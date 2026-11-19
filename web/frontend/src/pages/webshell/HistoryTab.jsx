/**
 * WebShell 命令历史：记录本连接执行过的命令，随连接状态一起持久化。
 */
import { Button, Space, Typography } from '@douyinfe/semi-ui';
import { useT } from '../../lib/i18n';
import { DataTable, CopyButton, confirmAction } from '../../components/ui';
import { formatDateTime } from '../../lib/format';

const { Text } = Typography;

export default function HistoryTab({ history, onClear, onRemove, onRun }) {
    const t = useT();
    const rows = history.map((item, index) => ({ ...item, rowId: `${index}-${item.at}` }));

    const columns = [
        {
            title: t('webshell.colCommand', null, '命令'),
            dataIndex: 'command',
            render: command => <code className="webshell-history-cmd">{command}</code>
        },
        {
            title: t('webshell.colExecutedAt', null, '执行时间'),
            dataIndex: 'at',
            width: 200,
            render: at => formatDateTime(at)
        },
        {
            title: t('webshell.moreActions', null, '操作'),
            dataIndex: 'ops',
            width: 220,
            render: (_, row) => (
                <Space spacing={4}>
                    <CopyButton text={row.command} label={t('common.copied', null, '复制')} size="small" />
                    {onRun && <Button size="small" theme="borderless" onClick={() => onRun(row.command)}>{t('webshell.rerun', null, '再执行')}</Button>}
                    <Button
                        size="small"
                        theme="borderless"
                        type="danger"
                        onClick={() => confirmAction({
                            title: t('webshell.deleteHistory', null, '删除该条历史'),
                            content: row.command,
                            onOk: () => onRemove(row.rowId)
                        })}
                    >
                        {t('webshell.deleteFile', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="webshell-history">
            <div className="webshell-history-toolbar">
                <Text type="tertiary">{t('webshell.historyHint', null, '仅记录本连接在当前浏览器执行过的命令')}</Text>
                <Space spacing={8}>
                    {onClear && <Button size="small" disabled={!rows.length} onClick={() => onClear()}>{t('webshell.clearHistory', null, '清空历史')}</Button>}
                </Space>
            </div>
            <DataTable
                columns={columns}
                dataSource={rows}
                rowKey="rowId"
                pagination={{ pageSize: 15 }}
                emptyText={t('webshell.noHistory', null, '暂无命令历史')}
            />
        </div>
    );
}
