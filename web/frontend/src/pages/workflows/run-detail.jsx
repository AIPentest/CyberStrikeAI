/** 运行详情：run + nodeRuns、replay 轨迹与审批通过/拒绝。 */
import { useEffect, useState } from 'react';
import { Button, SideSheet, Space, Tag, TextArea, Timeline, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { ErrorBanner, JsonViewer, KeyValueList, confirmAction, toastError, toastSuccess } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { RUN_STATUS, errorProp, errText, shortHash } from './helpers';

const { Text } = Typography;

function statusTag(status) {
    const meta = RUN_STATUS[String(status || '')] || { label: status || '—', color: 'grey' };
    return <Tag size="small" color={meta.color}>{meta.label}</Tag>;
}

export default function RunDetailDrawer({ runId, visible, canExecute, onClose, onChanged }) {
    const t = useT();
    const [comment, setComment] = useState('');
    const [busy, setBusy] = useState(false);

    const detail = useResource(
        () => (runId ? api.get(`/workflows/runs/${encodeURIComponent(runId)}`) : Promise.resolve(null)),
        [runId]
    );
    const replay = useResource(
        () => (runId ? api.get(`/workflows/runs/${encodeURIComponent(runId)}/replay`) : Promise.resolve(null)),
        [runId]
    );

    const run = detail.data?.run || null;

    useEffect(() => {
        if (visible && runId) {
            detail.reload({ silent: true });
            replay.reload({ silent: true });
        }
    }, [visible, runId]); // eslint-disable-line react-hooks/exhaustive-deps

    const nodeRuns = detail.data?.nodeRuns || [];
    const steps = replay.data?.steps || [];

    const decide = (approved) => confirmAction({
        title: approved ? '通过审批' : '拒绝审批',
        content: approved
            ? '确认通过该工作流的审批节点并继续执行？'
            : '确认拒绝该工作流的审批节点？运行将以失败结束。',
        okText: t('common.confirm', null, '确认'),
        danger: !approved,
        onOk: async () => {
            setBusy(true);
            try {
                await api.post(`/workflows/runs/${encodeURIComponent(runId)}/resume`, { approved, comment });
                toastSuccess(approved ? '已通过审批' : '已拒绝审批');
                setComment('');
                detail.reload({ silent: true });
                replay.reload({ silent: true });
                onChanged?.();
            } catch (error) {
                toastError(errText(error, '审批提交失败'));
            } finally {
                setBusy(false);
            }
        }
    });

    return (
        <SideSheet
            visible={visible}
            width={820}
            title={t('workflows.dryRunTrace', null, '运行轨迹')}
            onCancel={onClose}
            footer={(
                <Space>
                    {run?.status === 'awaiting_hitl' && canExecute ? (
                        <>
                            <TextArea
                                value={comment}
                                rows={1}
                                placeholder="审批备注（可选）"
                                onChange={value => setComment(value)}
                                style={{ width: 240 }}
                            />
                            <Button type="primary" loading={busy} onClick={() => decide(true)}>通过</Button>
                            <Button type="danger" loading={busy} onClick={() => decide(false)}>拒绝</Button>
                        </>
                    ) : null}
                    <Button onClick={onClose}>{t('common.close', null, '关闭')}</Button>
                </Space>
            )}
        >
            <ErrorBanner error={errorProp(detail.error)} onRetry={() => detail.reload()} />
            {run ? (
                <div className="workflow-detail-stack">
                    <div className="workflow-detail-tags">
                        {statusTag(run.status)}
                        <Text type="tertiary" size="small">
                            运行 <code>{shortHash(run.id, 16)}</code>
                        </Text>
                    </div>

                    <KeyValueList
                        columns={2}
                        items={[
                            { label: '工作流', value: <code>{run.workflow_id}</code> },
                            { label: '版本', value: run.workflow_version ?? '—' },
                            { label: '会话', value: run.conversation_id || '—' },
                            { label: '项目', value: run.project_id || '—' },
                            { label: '开始时间', value: formatDateTime(run.started_at) },
                            { label: '结束时间', value: formatDateTime(run.finished_at) },
                            run.pending_hitl_node_id
                                ? { label: '待审批节点', value: <code>{run.pending_hitl_node_id}</code> }
                                : null,
                            run.error ? { label: '错误', value: <Text type="danger" size="small">{run.error}</Text> } : null
                        ]}
                    />

                    <div>
                        <Text strong>节点执行记录（{nodeRuns.length}）</Text>
                        <Timeline className="workflow-timeline">
                            {nodeRuns.length ? nodeRuns.map(node => (
                                <Timeline.Item
                                    key={node.id}
                                    time={formatDateTime(node.started_at)}
                                    type={node.status === 'failed' ? 'error' : node.status === 'completed' ? 'success' : 'ongoing'}
                                >
                                    <div className="workflow-timeline-item">
                                        <div>
                                            <code>{node.node_id}</code> {statusTag(node.status)}
                                        </div>
                                        {node.error ? <Text type="danger" size="small">{node.error}</Text> : null}
                                        {node.output_json
                                            ? <JsonViewer value={safeParse(node.output_json)} title="output" collapsed />
                                            : null}
                                    </div>
                                </Timeline.Item>
                            )) : <Text type="tertiary" size="small">暂无节点执行记录</Text>}
                        </Timeline>
                    </div>

                    <div>
                        <Text strong>{t('workflows.dryRunTrace', null, '回放轨迹')}（{steps.length} 步）</Text>
                        <Timeline className="workflow-timeline">
                            {steps.length ? steps.map(step => (
                                <Timeline.Item
                                    key={step.nodeRunId || `${step.step}-${step.nodeId}`}
                                    time={formatDateTime(step.startedAt)}
                                    type={step.status === 'failed' ? 'error' : step.status === 'completed' ? 'success' : 'ongoing'}
                                >
                                    <div className="workflow-timeline-item">
                                        <div>
                                            <Text strong size="small">#{step.step}</Text> <code>{step.nodeId}</code> {statusTag(step.status)}
                                        </div>
                                        {step.error ? <Text type="danger" size="small">{step.error}</Text> : null}
                                        <JsonViewer value={{ input: step.input, output: step.output }} title="step" collapsed />
                                    </div>
                                </Timeline.Item>
                            )) : <Text type="tertiary" size="small">{t('workflows.dryRunNoTrace', null, '暂无轨迹')}</Text>}
                        </Timeline>
                    </div>

                    <JsonViewer value={run} title="run" collapsed />
                </div>
            ) : null}
        </SideSheet>
    );
}

function safeParse(raw) {
    try {
        return JSON.parse(raw);
    } catch (error) {
        return { raw };
    }
}
