import { useEffect, useId, useState } from 'react';
import { Form, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { Drawer, ErrorBanner, toastSuccess } from '../../components/ui';

const { Text } = Typography;
const MAX_NAME = 120;
const MAX_DESCRIPTION = 4000;

export default function ProjectFormModal({ visible, project, onCancel, onSaved }) {
    const t = useT();
    const formId = useId();
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState(null);
    const editing = Boolean(project?.id);

    useEffect(() => { setError(null); }, [visible, project?.id]);

    const submit = async values => {
        if (saving) return;
        setSaving(true);
        setError(null);
        try {
            const body = { name: values.name.trim(), description: (values.description || '').slice(0, MAX_DESCRIPTION) };
            const saved = editing
                ? await api.put(`/projects/${project.id}`, body)
                : await api.post('/projects', body);
            toastSuccess(editing ? t('projects.saved', null, '已保存') : t('projects.createProject', null, '创建项目'));
            onSaved?.(saved);
        } catch (err) {
            setError(err);
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            width={560}
            closable={!saving}
            closeOnEsc={!saving}
            onCancel={onCancel}
            cancelButtonProps={{ disabled: saving }}
            title={editing ? t('projects.modalEditTitle', null, '编辑项目') : t('projects.modalNewTitle', null, '新建项目')}
            formId={formId}
            okText={editing ? t('projects.saveChanges', null, '保存修改') : t('projects.createProject', null, '创建项目')}
            confirmLoading={saving}
        >
            <Text type="tertiary" size="small">
                {editing
                    ? t('projects.modalEditSubtitle', null, '修改项目名称与描述')
                    : t('projects.modalNewSubtitle', null, '创建后可绑定对话，跨会话共享事实黑板')}
            </Text>
            <Form id={formId} key={`${visible}-${project?.id || 'new'}`} onSubmit={submit}
                initValues={{ name: project?.name || '', description: project?.description || '' }}>
                <Form.Input id={`${formId}-name`} field="name" maxLength={MAX_NAME}
                    label={t('projects.projectName', null, '项目名称')}
                    placeholder={t('projects.projectNamePlaceholder', null, '例如：某客户 Web 渗透')}
                    rules={[{ required: true, whitespace: true, message: t('projects.enterProjectName', null, '请输入项目名称') }]} />
                <Form.TextArea id={`${formId}-description`} field="description" rows={4} maxCount={MAX_DESCRIPTION}
                    label={t('projects.projectDescription', null, '项目描述')}
                    placeholder={t('projects.projectDescriptionPlaceholder', null, '客户/任务备注、协作说明…')}
                    extraText={t('projects.descriptionLengthHint', null, '仅供团队成员查阅，Agent 不会读取；需让 Agent 知晓的内容请写入事实黑板')} />
                <ErrorBanner error={error} />
            </Form>
        </Drawer>
    );
}
