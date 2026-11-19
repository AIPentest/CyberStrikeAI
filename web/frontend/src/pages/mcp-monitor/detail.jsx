/** 执行详情侧滑面板：入参 / 出参 / 耗时 / 错误 / 关联会话。 */
import { Banner, Button, SideSheet, Space, Spin, Typography } from '@douyinfe/semi-ui';
import { useResource } from '../../components/hooks';
import { CopyButton, ErrorBanner, JsonViewer, KeyValueList } from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { formatDateTime } from '../../lib/format';
import { executionDurationMs, formatDurationMs, isRunning, statusLabel, toolDisplayName } from './utils';
import { ExecutionStatusTag } from './executions';

const { Text } = Typography;

export default function ExecutionDetail({ executionId, onClose, canWrite, onCancelRequest }) {
    const t = useT();
    const { data, loading, error, reload } = useResource(
        () => api.get(`/monitor/execution/${encodeURIComponent(executionId)}`),
        [executionId],
        { skip: !executionId }
    );
    const exec = executionId ? data : null;
    const running = exec ? isRunning(exec.status) : false;

    return (
        <SideSheet
            visible={Boolean(executionId)}
            onCancel={onClose}
            title={t('mcpMonitor.viewDetail', null, '执行详情')}
            width={720}
            footer={exec ? (
                <Space>
                    {running && (
                        <Button theme="solid" disabled={!canWrite} onClick={() => onCancelRequest(exec)}>
                            {t('mcpMonitor.terminateExecution', null, '终止')}
                        </Button>
                    )}
                    <Button onClick={onClose}>{t('common.close', null, '关闭')}</Button>
                </Space>
            ) : null}
        >
            {!exec && loading && (
                <div className="mcp-monitor-detail-loading"><Spin /></div>
            )}
            <ErrorBanner error={error} onRetry={reload} />
            {exec && (
                <div className="mcp-monitor-detail">
                    <KeyValueList
                        columns={2}
                        items={[
                            { label: '工具', value: <Text strong>{toolDisplayName(exec.toolName) || '—'}</Text> },
                            { label: t('mcpMonitor.columnStatus', null, '状态'), value: <ExecutionStatusTag status={exec.status} /> },
                            { label: '执行 ID', value: <span className="mcp-monitor-mono">{exec.id}</span> },
                            {
                                label: t('mcpMonitor.columnStartTime', null, '开始时间'),
                                value: formatDateTime(exec.startTime)
                            },
                            {
                                label: '结束时间',
                                value: exec.endTime ? formatDateTime(exec.endTime) : '—'
                            },
                            {
                                label: t('mcpMonitor.columnDuration', null, '耗时'),
                                value: formatDurationMs(executionDurationMs(exec))
                            },
                            {
                                label: '关联会话',
                                value: exec.conversationId
                                    ? (
                                        <Space spacing={4}>
                                            <span className="mcp-monitor-mono">{exec.conversationId}</span>
                                            <CopyButton text={exec.conversationId} size="small" />
                                        </Space>
                                    )
                                    : '—'
                            },
                            {
                                label: '结果状态',
                                value: exec.result
                                    ? (exec.result.isError ? '错误结果' : (exec.result.blocked ? '已拦截' : '正常返回'))
                                    : '无返回'
                            }
                        ]}
                    />

                    {exec.error && (
                        <Banner
                            type="danger"
                            className="mcp-monitor-detail-error"
                            description={<span className="mcp-monitor-mono">{exec.error}</span>}
                        />
                    )}

                    <div className="mcp-monitor-detail-section">
                        <JsonViewer title={t('mcpDetailModal.requestParams', null, '入参')} value={exec.arguments ?? {}} />
                    </div>

                    <div className="mcp-monitor-detail-section">
                        {exec.result ? (
                            <JsonViewer title={t('mcpDetailModal.responseResult', null, '出参')} value={exec.result} />
                        ) : (
                            <div className="mcp-monitor-detail-empty">
                                {running
                                    ? t('mcpDetailModal.runningNoResponseYet', null, '尚无返回，工具可能仍在执行。')
                                    : t('chat.noResponseData', null, '暂无响应数据')}
                            </div>
                        )}
                    </div>

                    {exec.partialOutput && (
                        <div className="mcp-monitor-detail-section">
                            <div className="mcp-monitor-detail-section-head">
                                <Text strong>实时输出尾部</Text>
                                <Space spacing={4}>
                                    {exec.partialOutputBytes ? <Text type="tertiary">{exec.partialOutputBytes} B</Text> : null}
                                    <CopyButton text={exec.partialOutput} size="small" />
                                </Space>
                            </div>
                            <pre className="mcp-monitor-detail-pre">{exec.partialOutput}</pre>
                        </div>
                    )}

                    <div className="mcp-monitor-detail-footnote">
                        {statusLabel(t, exec.status)} · {t('mcpMonitor.statsFromAllTools', null, '全部工具累计')}
                    </div>
                </div>
            )}
        </SideSheet>
    );
}
