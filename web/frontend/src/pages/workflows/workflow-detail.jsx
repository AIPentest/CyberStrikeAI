/** 工作流详情：元数据、图结构摘要、graph JSON 原文与导出/删除入口。 */
import { useEffect, useMemo, useState } from 'react';
import { Button, SideSheet, Space, Table, Tag, Typography } from '@douyinfe/semi-ui';
import { api, downloadFile } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { ErrorBanner, JsonViewer, KeyValueList, confirmAction, toastError, toastSuccess } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import {
    NODE_TYPE_LABEL, errorProp, errText, graphSummary
} from './helpers';

const { Text } = Typography;

function GraphPreview({ raw }) {
    const summary = useMemo(() => graphSummary(raw), [raw]);
    if (summary.error) {
        return <Text type="danger" size="small">graph 解析失败：{summary.error}</Text>;
    }
    const rows = summary.nodes.map(node => ({
        id: node.id,
        type: node.type || 'tool',
        label: node.label || '',
        config: node.config || {}
    }));
    const columns = [
        { title: '节点', dataIndex: 'id', width: 160, render: value => <code>{value}</code> },
        {
            title: '类型',
            dataIndex: 'type',
            width: 90,
            render: value => <Tag size="small" color="blue" type="light">{NODE_TYPE_LABEL[value] || value}</Tag>
        },
        {
            title: '名称',
            dataIndex: 'label',
            render: (value, record) => (
                <span className="workflow-cell-ellipsis" title={value || record.id}>{value || '—'}</span>
            )
        }
    ];
    return (
        <div className="workflow-detail-stack">
            <Table
                columns={columns}
                dataSource={rows}
                rowKey="id"
                size="small"
                pagination={false}
                empty={<Text type="tertiary" size="small">图内没有节点</Text>}
            />
            <div className="workflow-edge-list">
                <Text strong size="small">连线（{summary.edges.length}）</Text>
                {summary.edges.length ? (
                    <ul className="workflow-link-list">
                        {summary.edges.map((edge, index) => (
                            <li key={edge.id || `${edge.source}-${edge.target}-${index}`}>
                                <code>{edge.source}</code> → <code>{edge.target}</code>
                                {edge.label ? <span className="workflow-edge-label">{edge.label}</span> : null}
                            </li>
                        ))}
                    </ul>
                ) : <Text type="tertiary" size="small">暂无连线</Text>}
            </div>
        </div>
    );
}

export default function WorkflowDetailDrawer({ workflowId, visible, canWrite, canDelete, onClose, onEdit, onDeleted }) {
    const t = useT();
    const [exporting, setExporting] = useState(false);

    const detail = useResource(
        () => (workflowId ? api.get(`/workflows/${encodeURIComponent(workflowId)}`) : Promise.resolve(null)),
        [workflowId]
    );
    const workflow = detail.data?.workflow || null;

    useEffect(() => {
        if (visible && workflowId) detail.reload({ silent: true });
    }, [visible, workflowId]); // eslint-disable-line react-hooks/exhaustive-deps

    const exportPackage = async () => {
        if (!workflow) return;
        setExporting(true);
        try {
            await downloadFile(`/workflows/${encodeURIComponent(workflow.id)}/package`, {
                filename: `${workflow.id}.csapkg.zip`
            });
            toastSuccess(t('workflows.package.export', null, '导出'));
        } catch (error) {
            toastError(errText(error, t('workflows.package.errors.exportFailed', null, '导出失败，请稍后重试。')));
        } finally {
            setExporting(false);
        }
    };

    const remove = () => confirmAction({
        title: t('workflows.deleteWorkflow', null, '删除工作流'),
        content: t('workflows.confirmDelete', { id: workflow?.id }, `确定删除工作流 ${workflow?.id}？`),
        okText: t('common.delete', null, '删除'),
        onOk: async () => {
            try {
                await api.del(`/workflows/${encodeURIComponent(workflow.id)}`);
                toastSuccess(t('workflows.deleted', null, '工作流已删除'));
                onDeleted?.(workflow.id);
            } catch (error) {
                toastError(errText(error, t('workflows.deleteFailed', null, '删除工作流失败')));
            }
        }
    });

    return (
        <SideSheet
            visible={visible}
            width={760}
            title={workflow ? workflow.name || workflow.id : t('workflows.metaModalTitle', null, '工作流信息')}
            onCancel={onClose}
            footer={(
                <Space>
                    <Button disabled={!canWrite || !workflow} onClick={() => onEdit?.(workflow)}>
                        {t('common.edit', null, '编辑')}
                    </Button>
                    <Button loading={exporting} disabled={!workflow} onClick={exportPackage}>
                        {t('workflows.package.export', null, '导出')}
                    </Button>
                    <Button type="danger" disabled={!canDelete || !workflow} onClick={remove}>
                        {t('workflows.deleteWorkflow', null, '删除工作流')}
                    </Button>
                    <Button onClick={onClose}>{t('common.close', null, '关闭')}</Button>
                </Space>
            )}
        >
            <ErrorBanner error={errorProp(detail.error)} onRetry={() => detail.reload()} />
            {workflow ? (
                <div className="workflow-detail-stack">
                    <div className="workflow-detail-tags">
                        <Tag size="small" color={workflow.enabled ? 'green' : 'grey'}>
                            {workflow.enabled ? t('workflows.statusEnabled', null, '启用') : t('workflows.statusDisabled', null, '禁用')}
                        </Tag>
                        {workflow.validation_error
                            ? <Tag size="small" color="red">{t('workflows.statusInvalid', null, '校验未通过')}</Tag>
                            : <Tag size="small" color="green" type="light">结构有效</Tag>}
                        <Text type="tertiary" size="small">版本 {workflow.version || 0}</Text>
                    </div>

                    {workflow.validation_error ? (
                        <ErrorBanner error={{ message: workflow.validation_error }} />
                    ) : null}

                    <KeyValueList
                        columns={2}
                        items={[
                            { label: t('workflows.metaId', null, 'ID'), value: <code>{workflow.id}</code> },
                            { label: t('workflows.metaName', null, '名称'), value: workflow.name || '—' },
                            { label: t('workflows.updated', null, '更新'), value: formatDateTime(workflow.updated_at) },
                            { label: '创建', value: formatDateTime(workflow.created_at) },
                            { label: t('workflows.metaDescription', null, '描述'), value: workflow.description || '—' }
                        ]}
                    />

                    <div>
                        <Text strong>{t('workflows.nodeProperties', null, '节点属性')}</Text>
                        <GraphPreview raw={workflow.graph_json} />
                    </div>

                    <JsonViewer value={workflow.graph_json ? safeParse(workflow.graph_json) : {}} title="graph JSON" collapsed />
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
