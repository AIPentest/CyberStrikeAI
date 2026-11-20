/** 工作流新建 / 编辑：元数据 + graph JSON 文本域（校验、格式化、模板）。 */
import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Checkbox, Input, InputNumber, Space, TextArea, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { Drawer, toastError, toastSuccess } from '../../components/ui';
import { GRAPH_TEMPLATE, NODE_TYPES, errText, graphSummary } from './helpers';

const { Text } = Typography;

const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;

const EMPTY = { id: '', name: '', description: '', version: 1, enabled: true, graph: '' };

export default function WorkflowFormModal({ visible, workflow, onCancel, onSaved }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const canValidate = hasPermission('workflow:execute');

    const [form, setForm] = useState(EMPTY);
    const [saving, setSaving] = useState(false);
    const [validating, setValidating] = useState(false);
    const [check, setCheck] = useState(null);

    const editing = Boolean(workflow?.id);

    useEffect(() => {
        if (!visible) return;
        setCheck(null);
        setSaving(false);
        if (workflow) {
            let graphText = workflow.graph_json || '';
            try {
                graphText = JSON.stringify(JSON.parse(graphText), null, 2);
            } catch (error) { /* 保留原始文本，交给校验提示 */ }
            setForm({
                id: workflow.id || '',
                name: workflow.name || '',
                description: workflow.description || '',
                version: workflow.version || 1,
                enabled: workflow.enabled !== false,
                graph: graphText
            });
            return;
        }
        setForm({ ...EMPTY, graph: JSON.stringify(GRAPH_TEMPLATE, null, 2) });
    }, [visible, workflow]);

    const update = patch => setForm(prev => ({ ...prev, ...patch }));

    const summary = useMemo(() => graphSummary(form.graph), [form.graph]);

    const format = () => {
        try {
            update({ graph: JSON.stringify(JSON.parse(form.graph), null, 2) });
            setCheck(null);
        } catch (error) {
            toastError(`${t('projects.invalidJson', null, 'JSON 格式无效')}：${errText(error, '')}`);
        }
    };

    const insertTemplate = () => {
        update({ graph: JSON.stringify(GRAPH_TEMPLATE, null, 2) });
        setCheck(null);
    };

    const validate = async () => {
        if (!form.graph.trim()) {
            setCheck({ type: 'danger', text: t('workflows.validation.serverFailed', null, '图定义不能为空') });
            return;
        }
        let parsed;
        try {
            parsed = JSON.parse(form.graph);
        } catch (error) {
            setCheck({ type: 'danger', text: `JSON 解析失败：${errText(error, '')}` });
            return;
        }
        setValidating(true);
        setCheck(null);
        try {
            await api.post('/workflows/validate', { graph: parsed });
            setCheck({ type: 'success', text: '结构校验通过' });
        } catch (error) {
            setCheck({ type: 'danger', text: errText(error, t('workflows.validation.serverFailed', null, '后端图校验失败')) });
        } finally {
            setValidating(false);
        }
    };

    const submit = async () => {
        const id = (editing ? workflow.id : form.id).trim();
        const name = form.name.trim();
        if (!id || !name) {
            toastError(t('workflows.idNameRequired', null, '工作流 ID 和名称不能为空'));
            return;
        }
        if (!editing && !ID_PATTERN.test(id)) {
            toastError('工作流 ID 必须以字母或数字开头，仅含字母、数字、下划线、连字符');
            return;
        }
        if (form.graph.trim()) {
            try {
                JSON.parse(form.graph);
            } catch (error) {
                toastError(`graph 必须是合法 JSON：${errText(error, '')}`);
                return;
            }
        }
        setSaving(true);
        try {
            const body = {
                id,
                name,
                description: form.description,
                version: Number(form.version) || 0,
                enabled: Boolean(form.enabled),
                graph: form.graph.trim() ? JSON.parse(form.graph) : undefined
            };
            const saved = editing
                ? await api.put(`/workflows/${encodeURIComponent(id)}`, body)
                : await api.post('/workflows', body);
            toastSuccess(t('workflows.saved', null, '工作流已保存'));
            onSaved?.(saved?.workflow || null);
        } catch (error) {
            toastError(errText(error, t('workflows.saveFailed', null, '保存工作流失败')));
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={editing ? t('workflows.metaModalTitle', null, '工作流信息') : t('workflows.newGraph', null, '新建工作流')}
            onCancel={onCancel}
            onOk={submit}
            okText={t('common.save', null, '保存')}
            cancelText={t('common.cancel', null, '取消')}
            confirmLoading={saving}
            width={860}
        >
            <div className="workflow-form-grid">
                <div className="workflow-form-field">
                    <Text strong size="small">{t('workflows.metaId', null, 'ID')}</Text>
                    <Input
                        value={form.id}
                        disabled={editing}
                        placeholder="web-scan-basic"
                        onChange={value => update({ id: value })}
                    />
                    <Text type="tertiary" size="small">{t('workflows.metaIdHint', null, '创建后不可修改，用于 API 与角色绑定')}</Text>
                </div>
                <div className="workflow-form-field">
                    <Text strong size="small">{t('workflows.metaName', null, '名称')}</Text>
                    <Input
                        value={form.name}
                        placeholder={t('workflows.namePlaceholder', null, '基础 Web 扫描')}
                        onChange={value => update({ name: value })}
                    />
                </div>
            </div>

            <div className="workflow-form-grid">
                <div className="workflow-form-field">
                    <Text strong size="small">{t('workflows.metaDescription', null, '描述')}</Text>
                    <Input
                        value={form.description}
                        placeholder={t('workflows.descriptionPlaceholder', null, '可选')}
                        onChange={value => update({ description: value })}
                    />
                </div>
                <div className="workflow-form-grid workflow-form-grid--tight">
                    <div className="workflow-form-field">
                        <Text strong size="small">版本</Text>
                        <InputNumber
                            value={form.version}
                            min={0}
                            style={{ width: '100%' }}
                            onChange={value => update({ version: value || 0 })}
                        />
                    </div>
                    <div className="workflow-form-field">
                        <Text strong size="small">{t('workflows.metaEnabled', null, '启用')}</Text>
                        <Checkbox checked={form.enabled} onChange={event => update({ enabled: event.target.checked })}>
                            {t('workflows.metaEnabledHint', null, '禁用后无法被角色自动触发')}
                        </Checkbox>
                    </div>
                </div>
            </div>

            <div className="workflow-form-field">
                <Space>
                    <Text strong size="small">graph JSON</Text>
                    <Text type="tertiary" size="small">
                        {summary.error
                            ? `解析失败：${summary.error}`
                            : `${summary.nodes.length} 个节点 · ${summary.edges.length} 条连线`}
                    </Text>
                </Space>
                <TextArea
                    value={form.graph}
                    rows={16}
                    spellCheck={false}
                    className="workflow-graph-input"
                    placeholder='{"nodes":[],"edges":[],"config":{}}'
                    onChange={value => { update({ graph: value }); setCheck(null); }}
                />
                <Space wrap>
                    <Button size="small" onClick={format}>{t('projects.formatJson', null, '格式化')}</Button>
                    <Button size="small" onClick={insertTemplate}>插入最小模板</Button>
                    <Button
                        size="small"
                        loading={validating}
                        disabled={!canValidate}
                        title={canValidate ? '' : '需要 workflow:execute 权限'}
                        onClick={validate}
                    >
                        {t('workflows.validate', null, '校验图定义')}
                    </Button>
                    <Text type="tertiary" size="small">
                        节点类型：{NODE_TYPES.map(item => item.label).join(' / ')}
                    </Text>
                </Space>
            </div>

            {check ? (
                <Banner
                    type={check.type}
                    bordered
                    closeIcon={null}
                    description={check.text}
                    style={{ marginTop: 12 }}
                />
            ) : null}
        </Drawer>
    );
}
