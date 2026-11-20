/** 新建深度测评表单：字段与 internal/handler/deep_assessment.go 的创建结构体一致。 */
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { Banner, Button, Form, Spin } from '@douyinfe/semi-ui';
import { IconPlus } from '@douyinfe/semi-icons';
import { useAuth } from '../../lib/auth';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { modelChannelOptions, selectedModelChannelName } from '../../lib/model-channel';
import { Drawer, ErrorBanner, toastError, toastSuccess } from '../../components/ui';
import ProjectFormModal from '../projects/project-form';

const HITL_OPTIONS = [
    { value: '', label: '系统默认' },
    { value: 'off', label: '关闭审批' },
    { value: 'human', label: '人工审批' },
    { value: 'audit_agent', label: '审计代理审批' },
    { value: 'review_edit', label: '复核并允许编辑' }
];

export default function CreateAssessmentDrawer({ visible, onCancel, onCreated }) {
    const t = useT();
    const title = t('deepAssessments.create', null, '新建测评');
    const { hasPermission } = useAuth();
    const canCreateProject = hasPermission('project:read') && hasPermission('project:write');
    const formId = useId();
    const formApi = useRef(null);
    const [options, setOptions] = useState(null);
    const [error, setError] = useState(null);
    const [saving, setSaving] = useState(false);
    const [projectFormOpen, setProjectFormOpen] = useState(false);
    const [collections, setCollections] = useState([]);
    const busy = saving || projectFormOpen;

    const loadOptions = useCallback(() => {
        setOptions(null);
        setError(null);
        api.get('/deep-assessments/options')
            .then(data => setOptions(data || {}))
            .catch(err => setError(err));
    }, []);

    useEffect(() => {
        if (!visible) return;
        loadOptions();
    }, [visible, loadOptions]);

    const loadCollections = projectId => {
        if (!projectId) {
            setCollections([]);
            return;
        }
        api.get('/assets/collections', { project_id: projectId })
            .then(data => setCollections(Array.isArray(data?.collections) ? data.collections : []))
            .catch(() => setCollections([]));
    };

    const submit = async values => {
        if (busy) return;
        setSaving(true);
        try {
            const body = {
                name: String(values.name || '').trim(),
                objective: String(values.objective || '').trim(),
                scope: String(values.scope || '').trim(),
                criteria: String(values.criteria || '').trim(),
                project_id: values.project_id || '',
                collection_id: values.collection_id || '',
                role: values.role || '',
                model: values.model || '',
                hitl_policy: values.hitl_policy || '',
                concurrency: Number(values.concurrency) || 2,
                auto_resume: values.auto_resume !== false
            };
            const data = await api.post('/deep-assessments', body);
            toastSuccess(t('deepAssessments.saved', null, '已创建测评'));
            onCreated?.(data?.task);
        } catch (err) {
            toastError(err, t('deepAssessments.saveFailed', null, '创建失败'));
        } finally {
            setSaving(false);
        }
    };

    const handleProjectCreated = project => {
        setProjectFormOpen(false);
        if (!project?.id) return;
        setOptions(current => ({ ...current, projects: [project, ...(current?.projects || [])] }));
        formApi.current?.setValue('project_id', project.id);
        formApi.current?.setValue('collection_id', '');
        loadCollections(project.id);
    };

    const models = modelChannelOptions((options?.models || []).map(item => [item.id, item]));
    const roles = (options?.roles || []).map(role => ({ value: role, label: role }));
    const projects = (options?.projects || []).map(project => ({ value: project.id, label: project.name || project.id }));

    return (
        <Drawer
            visible={visible}
            title={title}
            aria-label={title}
            onCancel={onCancel}
            width={680}
            closable={!busy}
            closeOnEsc={!busy}
            maskClosable={!busy}
            footer={(
                <div className="assessment-form-actions">
                    <Button theme="borderless" onClick={onCancel} disabled={busy}>
                        {t('deepAssessments.close', null, '关闭')}
                    </Button>
                    <Button
                        htmlType="submit"
                        form={formId}
                        theme="solid"
                        type="primary"
                        loading={saving}
                        disabled={busy || (!options && !error)}
                    >
                        {t('deepAssessments.save', null, '保存草稿')}
                    </Button>
                </div>
            )}
        >
            {error ? (
                <div className="assessment-error-block">
                    <ErrorBanner error={error} />
                    <Button size="small" onClick={loadOptions}>重试</Button>
                </div>
            ) : null}
            {!options && !error ? (
                <div className="assessment-loading"><Spin /></div>
            ) : (
                <Form
                    id={formId}
                    getFormApi={instance => { formApi.current = instance; }}
                    initValues={{ concurrency: 2, auto_resume: true, hitl_policy: '' }}
                    labelPosition="top"
                    autoScrollToError
                    onSubmit={submit}
                >
                    <Form.Input
                        field="name"
                        label={t('deepAssessments.name', null, '测评名称')}
                        maxLength={200}
                        placeholder="留空则取目标前 80 个字符"
                    />
                    <Form.TextArea
                        field="objective"
                        label={t('deepAssessments.objective', null, '目标')}
                        rows={3}
                        maxLength={16000}
                        placeholder="要达成的最终结果，例如：评估 example.com 对外接口的认证与越权风险并给出可复现证据。"
                        rules={[{ required: true, message: '请填写测评目标' }]}
                    />
                    <Form.TextArea
                        field="scope"
                        label={t('deepAssessments.scopeOptional', null, '补充限制与排除项（选填）')}
                        rows={2}
                        maxLength={16000}
                        placeholder="例如：排除支付接口；不修改生产数据；不做压力测试。留空沿用目标。"
                    />
                    <Form.TextArea
                        field="criteria"
                        label={t('deepAssessments.criteriaOptional', null, '验收条件（选填）')}
                        rows={3}
                        maxLength={8000}
                        placeholder="每行一条验收条件，例如：确认的漏洞附复现步骤和原始证据。留空沿用目标。"
                    />
                    <div className="assessment-form-grid">
                        <Form.Select
                            field="model"
                            label={t('deepAssessments.model', null, '模型渠道')}
                            optionList={[{ value: '', label: '系统默认' }, ...models]}
                            renderSelectedItem={selectedModelChannelName}
                            style={{ width: '100%' }}
                        />
                        <Form.Select
                            field="role"
                            label={t('deepAssessments.role', null, '角色')}
                            optionList={[{ value: '', label: '系统默认' }, ...roles]}
                            style={{ width: '100%' }}
                        />
                        <div className="assessment-project-field">
                            <Form.Select
                                field="project_id"
                                label={t('deepAssessments.project', null, '关联项目')}
                                optionList={[{ value: '', label: '不关联' }, ...projects]}
                                style={{ width: '100%' }}
                                onChange={value => {
                                    formApi.current?.setValue('collection_id', '');
                                    loadCollections(value || '');
                                }}
                            />
                            {canCreateProject && (
                                <Button
                                    icon={<IconPlus aria-hidden />}
                                    theme="light"
                                    type="primary"
                                    disabled={saving}
                                    onClick={() => setProjectFormOpen(true)}
                                >
                                    {t('projects.newProject', null, '新建项目')}
                                </Button>
                            )}
                        </div>
                        <Form.Select
                            field="collection_id"
                            label="资产集合"
                            optionList={[{ value: '', label: '不指定集合' }, ...collections.map(item => ({ value: item.id, label: `${item.name}（${item.member_count || 0}）` }))]}
                            style={{ width: '100%' }}
                        />
                        <Form.InputNumber
                            field="concurrency"
                            label={t('deepAssessments.concurrency', null, '并行步骤')}
                            min={1}
                            max={4}
                            style={{ width: '100%' }}
                        />
                    </div>
                    <Form.Select
                        field="hitl_policy"
                        label="人工审批策略"
                        optionList={HITL_OPTIONS}
                        style={{ width: '100%' }}
                    />
                    <Form.Switch
                        field="auto_resume"
                        label={t('deepAssessments.autoResume', null, '服务重启后自动恢复结果明确的步骤')}
                    />
                    <Banner
                        type="info"
                        bordered
                        closeIcon={null}
                        className="assessment-form-note"
                        description="创建后保存为草稿；打开页面不会自动启动，需要手动点击“开始 / 继续”。"
                    />
                </Form>
            )}
            {projectFormOpen && (
                <ProjectFormModal
                    visible
                    onCancel={() => setProjectFormOpen(false)}
                    onSaved={handleProjectCreated}
                />
            )}
        </Drawer>
    );
}
