/** 新建/编辑漏洞：字段与 CreateVulnerabilityRequest / UpdateVulnerabilityRequest 对齐。 */
import { useEffect, useId, useRef, useState } from 'react';
import { Form, Spin, Typography } from '@douyinfe/semi-ui';
import { api, errorMessage } from '../../lib/http';
import { Drawer, toastError } from '../../components/ui';
import { severityOptions, statusOptions } from './shared';

const { Text } = Typography;

const EMPTY_VALUES = {
    title: '',
    severity: '',
    status: 'open',
    type: '',
    target: '',
    project_id: '',
    conversation_id: '',
    conversation_tag: '',
    task_tag: '',
    description: '',
    preconditions: '',
    reproduction_steps: '',
    evidence: '',
    impact: '',
    recommendation: '',
    retest_notes: ''
};

function pickValues(vuln) {
    if (!vuln) return { ...EMPTY_VALUES };
    return {
        title: vuln.title || '',
        severity: vuln.severity || '',
        status: vuln.status || 'open',
        type: vuln.type || '',
        target: vuln.target || '',
        project_id: vuln.project_id || '',
        conversation_id: vuln.conversation_id || '',
        conversation_tag: vuln.conversation_tag || '',
        task_tag: vuln.task_tag || '',
        description: vuln.description || '',
        preconditions: vuln.preconditions || '',
        reproduction_steps: vuln.reproduction_steps || '',
        evidence: vuln.evidence || '',
        impact: vuln.impact || '',
        recommendation: vuln.recommendation || '',
        retest_notes: vuln.retest_notes || ''
    };
}

function trim(value) {
    return String(value ?? '').trim();
}

export default function EditModal({ visible, vulnId, t, projectOptions, onClose, onSaved }) {
    const editing = Boolean(vulnId);
    const formId = useId();
    const formApi = useRef(null);
    const [saving, setSaving] = useState(false);
    const [loadError, setLoadError] = useState(null);
    const [initial, setInitial] = useState({ ...EMPTY_VALUES });
    // 记录 initial 属于哪条记录：切换记录时先渲染 Spin，避免旧表单值闪现。
    const [loadedFor, setLoadedFor] = useState('');

    useEffect(() => {
        if (!visible) return undefined;
        setLoadError(null);
        if (!vulnId) {
            setLoadedFor('');
            return undefined;
        }
        let cancelled = false;
        api.get(`/vulnerabilities/${encodeURIComponent(vulnId)}`)
            .then(payload => {
                if (cancelled) return;
                setInitial(pickValues(payload));
                setLoadedFor(vulnId);
            })
            .catch(error => { if (!cancelled) setLoadError(error); });
        return () => { cancelled = true; };
    }, [visible, vulnId]);

    const ready = !editing || loadedFor === vulnId;
    const formValues = editing ? initial : { ...EMPTY_VALUES };
    // Semi's Select commits onChange after the popover closes. Save must also work during that transition.
    const commitSelection = field => value => formApi.current?.setValue(field, value);

    const submit = async values => {
        if (saving) return;
        const body = {
            title: trim(values.title),
            severity: values.severity,
            status: values.status || 'open',
            type: trim(values.type),
            target: trim(values.target),
            description: values.description || '',
            preconditions: values.preconditions || '',
            reproduction_steps: values.reproduction_steps || '',
            evidence: values.evidence || '',
            impact: values.impact || '',
            recommendation: values.recommendation || '',
            retest_notes: values.retest_notes || '',
            project_id: trim(values.project_id),
            conversation_tag: trim(values.conversation_tag),
            task_tag: trim(values.task_tag)
        };
        if (!editing) body.conversation_id = trim(values.conversation_id);

        setSaving(true);
        try {
            const saved = editing
                ? await api.put(`/vulnerabilities/${encodeURIComponent(vulnId)}`, body)
                : await api.post('/vulnerabilities', body);
            onSaved?.(saved);
        } catch (error) {
            toastError(error, t('vulnerabilityPage.saveFailed', null, '保存失败'));
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            title={editing
                ? t('vulnerability.editVuln', null, '编辑漏洞')
                : t('vulnerability.addVuln', null, '添加漏洞')}
            visible={visible}
            onCancel={onClose}
            formId={ready && !loadError ? formId : undefined}
            confirmLoading={saving}
            okText={t('common.save', null, '保存')}
            cancelText={t('common.cancel', null, '取消')}
            width={780}
        >
            {!visible ? null : loadError ? (
                <Text type="danger">{errorMessage(loadError, t('vulnerabilityPage.fetchFailed', null, '获取漏洞失败'))}</Text>
            ) : !ready ? (
                <div className="vulnerability-modal-loading"><Spin size="large" /></div>
            ) : (
                <Form
                    id={formId}
                    getFormApi={instance => { formApi.current = instance; }}
                    key={editing ? `${vulnId}-${loadedFor}` : 'new'}
                    initValues={formValues}
                    labelPosition="top"
                    onSubmit={submit}
                >
                    <div className="vulnerability-form-grid">
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.Input
                                field="title"
                                label={t('vulnerabilityModal.title', null, '标题')}
                                placeholder={t('vulnerabilityModal.titlePlaceholder', null, '/api/login 存在 SQL 注入')}
                                rules={[{ required: true, message: t('vulnerabilityModal.titleRequired', null, '请填写漏洞标题') }]}
                                maxLength={500}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Select
                                field="severity"
                                onSelect={commitSelection('severity')}
                                label={t('vulnerabilityModal.severity', null, '严重程度')}
                                placeholder={t('vulnerabilityModal.pleaseSelect', null, '请选择')}
                                style={{ width: '100%' }}
                                optionList={severityOptions(t)}
                                rules={[{ required: true, message: t('vulnerabilityModal.severityRequired', null, '请选择严重程度') }]}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Select
                                field="status"
                                onSelect={commitSelection('status')}
                                label={t('vulnerabilityModal.status', null, '状态')}
                                style={{ width: '100%' }}
                                optionList={statusOptions(t)}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Input
                                field="target"
                                label={t('vulnerabilityModal.target', null, '目标')}
                                placeholder={t('vulnerabilityModal.targetPlaceholder', null, '精确到 URL/IP:端口/接口路径/参数名')}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Input
                                field="type"
                                label={t('vulnerabilityModal.type', null, '漏洞类型')}
                                placeholder={t('vulnerabilityModal.typePlaceholder', null, '如：SQL注入、XSS、CSRF等')}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Select
                                field="project_id"
                                onSelect={commitSelection('project_id')}
                                label={t('vulnerabilityModal.project', null, '所属项目')}
                                placeholder={t('vulnerabilityModal.projectNone', null, '（未绑定）')}
                                style={{ width: '100%' }}
                                filter
                                optionList={[{ value: '', label: t('vulnerabilityModal.projectNone', null, '（未绑定）') }, ...projectOptions]}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Input
                                field="conversation_id"
                                label={t('vulnerabilityModal.conversationId', null, '会话ID')}
                                placeholder={t('vulnerabilityModal.conversationIdPlaceholder', null, '可选：输入已有会话ID，留空可独立添加漏洞')}
                                disabled={editing}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Input
                                field="conversation_tag"
                                label={t('vulnerabilityModal.conversationTag', null, '对话标签')}
                                placeholder={t('vulnerabilityModal.conversationTagPlaceholder', null, '如：红队演练A、客户A周报')}
                            />
                        </div>
                        <div className="vulnerability-field">
                            <Form.Input
                                field="task_tag"
                                label={t('vulnerabilityModal.taskTag', null, '任务标签')}
                                placeholder={t('vulnerabilityModal.taskTagPlaceholder', null, '如：批量扫描Q2、专项复测')}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="description"
                                label={t('vulnerabilityModal.description', null, '描述')}
                                placeholder={t('vulnerabilityModal.descriptionPlaceholder', null, '说明漏洞摘要、触发点、异常现象和为什么可被利用。')}
                                autosize={{ minRows: 3, maxRows: 8 }}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="preconditions"
                                label={t('vulnerabilityModal.preconditions', null, '前置条件')}
                                placeholder={t('vulnerabilityModal.preconditionsPlaceholder', null, '登录状态、权限、账号、Header/Cookie、特定数据、环境/版本；无则写无。')}
                                autosize={{ minRows: 2, maxRows: 6 }}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="reproduction_steps"
                                label={t('vulnerabilityModal.reproductionSteps', null, '复现步骤')}
                                placeholder={t('vulnerabilityModal.reproductionStepsPlaceholder', null, '按 1/2/3 编号，写清入口、参数、payload、执行命令、观察点。')}
                                autosize={{ minRows: 3, maxRows: 10 }}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="evidence"
                                label={t('vulnerabilityModal.evidence', null, '证据 / POC')}
                                placeholder={t('vulnerabilityModal.evidencePlaceholder', null, '原始请求/响应、curl/工具命令、截图说明、日志、回连记录等。')}
                                autosize={{ minRows: 3, maxRows: 10 }}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="impact"
                                label={t('vulnerabilityModal.impact', null, '影响')}
                                placeholder={t('vulnerabilityModal.impactPlaceholder', null, '结合已验证事实说明实际影响，例如越权读取哪些数据。')}
                                autosize={{ minRows: 2, maxRows: 6 }}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="recommendation"
                                label={t('vulnerabilityModal.recommendation', null, '修复建议')}
                                placeholder={t('vulnerabilityModal.recommendationPlaceholder', null, '写具体修复点和复测标准。')}
                                autosize={{ minRows: 2, maxRows: 8 }}
                            />
                        </div>
                        <div className="vulnerability-field vulnerability-field--full">
                            <Form.TextArea
                                field="retest_notes"
                                label={t('vulnerabilityModal.retestNotes', null, '复测方式')}
                                placeholder={t('vulnerabilityModal.retestNotesPlaceholder', null, '修复后如何验证漏洞已关闭，包括应返回的状态码、错误信息或访问控制结果。')}
                                autosize={{ minRows: 2, maxRows: 6 }}
                            />
                        </div>
                    </div>
                </Form>
            )}
        </Drawer>
    );
}
