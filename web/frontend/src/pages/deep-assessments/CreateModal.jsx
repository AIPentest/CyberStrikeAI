/** 新建深度测评表单：字段与 internal/handler/deep_assessment.go 的创建结构体一致。 */
import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Form, Modal, Spin } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { ErrorBanner, toastError, toastSuccess } from '../../components/ui';

const HITL_OPTIONS = [
    { value: '', label: '系统默认' },
    { value: 'off', label: '关闭审批' },
    { value: 'human', label: '人工审批' },
    { value: 'audit_agent', label: '审计代理审批' },
    { value: 'review_edit', label: '复核并允许编辑' }
];

export default function CreateAssessmentModal({ visible, onCancel, onCreated }) {
    const t = useT();
    const [options, setOptions] = useState(null);
    const [error, setError] = useState(null);
    const [saving, setSaving] = useState(false);

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

    const submit = async values => {
        setSaving(true);
        try {
            const body = {
                name: String(values.name || '').trim(),
                objective: String(values.objective || '').trim(),
                scope: String(values.scope || '').trim(),
                criteria: String(values.criteria || '').trim(),
                project_id: values.project_id || '',
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

    const models = (options?.models || []).map(item => ({
        value: item.id,
        label: [item.name || item.id, item.model].filter(Boolean).join(' · ')
    }));
    const roles = (options?.roles || []).map(role => ({ value: role, label: role }));
    const projects = (options?.projects || []).map(project => ({ value: project.id, label: project.name || project.id }));

    return (
        <Modal
            visible={visible}
            title={t('deepAssessments.create', null, '新建测评')}
            onCancel={onCancel}
            footer={null}
            width={680}
            closeOnEsc={!saving}
            maskClosable={!saving}
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
                    initValues={{ concurrency: 2, auto_resume: true, hitl_policy: '' }}
                    labelPosition="top"
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
                            style={{ width: '100%' }}
                        />
                        <Form.Select
                            field="role"
                            label={t('deepAssessments.role', null, '角色')}
                            optionList={[{ value: '', label: '系统默认' }, ...roles]}
                            style={{ width: '100%' }}
                        />
                        <Form.Select
                            field="project_id"
                            label={t('deepAssessments.project', null, '关联项目')}
                            optionList={[{ value: '', label: '不关联' }, ...projects]}
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
                    <div className="assessment-form-actions">
                        <Button theme="borderless" onClick={onCancel} disabled={saving}>
                            {t('deepAssessments.close', null, '关闭')}
                        </Button>
                        <Button htmlType="submit" theme="solid" type="primary" loading={saving}>
                            {t('deepAssessments.save', null, '保存草稿')}
                        </Button>
                    </div>
                </Form>
            )}
        </Modal>
    );
}
