/** 项目设置：元数据、测试范围 JSON、归档/恢复与删除。 */
import { useEffect, useState } from 'react';
import { Button, Divider, Input, Select, Space, Switch, TextArea, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { SectionCard, confirmAction, toastError, toastSuccess } from '../../components/ui';
import { PROJECT_STATUS, errText } from './helpers';

const { Text } = Typography;

const SCOPE_EXAMPLE = {
    targets: ['https://example.com'],
    exclude: ['*.cdn.example.com'],
    notes: '仅授权 Web 应用层测试'
};

export default function SettingsPanel({ project, canWrite, canDelete, onChanged, onDeleted }) {
    const t = useT();
    const [form, setForm] = useState({ name: '', description: '', scope_json: '', status: 'active', pinned: false });
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        if (!project) return;
        setForm({
            name: project.name || '',
            description: project.description || '',
            scope_json: project.scope_json || '',
            status: project.status || 'active',
            pinned: Boolean(project.pinned)
        });
    }, [project]);

    const update = patch => setForm(prev => ({ ...prev, ...patch }));

    const formatScope = () => {
        const raw = form.scope_json.trim();
        if (!raw) return;
        try {
            update({ scope_json: JSON.stringify(JSON.parse(raw), null, 2) });
        } catch (error) {
            toastError(t('projects.invalidJson', null, 'JSON 格式无效') + '：' + errText(error, ''));
        }
    };

    const insertExample = () => update({ scope_json: JSON.stringify(SCOPE_EXAMPLE, null, 2) });

    const save = async () => {
        const name = form.name.trim();
        if (!name) {
            toastError(t('projects.enterProjectName', null, '请输入项目名称'));
            return;
        }
        const scopeRaw = form.scope_json.trim();
        if (scopeRaw) {
            try {
                JSON.parse(scopeRaw);
            } catch (error) {
                toastError(t('projects.invalidScopeJson', null, '测试范围 JSON 无效，请先修正或点击「格式化」'));
                return;
            }
        }
        setSaving(true);
        try {
            await api.put(`/projects/${project.id}`, {
                name,
                description: form.description,
                scope_json: scopeRaw,
                status: form.status,
                pinned: Boolean(form.pinned)
            });
            toastSuccess(t('projects.saved', null, '已保存'));
            onChanged?.();
        } catch (error) {
            toastError(errText(error, t('projects.saveFailed', null, '保存失败')));
        } finally {
            setSaving(false);
        }
    };

    const archived = (project?.status || 'active') === 'archived';

    const toggleArchive = () => {
        confirmAction({
            title: archived ? t('projects.restoreProjectActive', null, '恢复为进行中') : t('projects.archiveProject', null, '归档'),
            content: archived
                ? t('projects.confirmRestoreProjectActive', null, '恢复为 active？')
                : t('projects.confirmArchiveProject', null, '归档后默认不再出现在活跃列表，是否继续？'),
            okText: t('common.confirm', null, '确认'),
            danger: !archived,
            onOk: async () => {
                try {
                    await api.put(`/projects/${project.id}`, { status: archived ? 'active' : 'archived' });
                    toastSuccess(t('projects.saved', null, '已保存'));
                    onChanged?.();
                } catch (error) {
                    toastError(errText(error, t('projects.operationFailed', null, '操作失败')));
                }
            }
        });
    };

    const remove = () => {
        confirmAction({
            title: t('projects.deleteProject', null, '删除项目'),
            content: t('projects.confirmDeleteProject', null, '确定删除该项目？事实将一并删除，对话将解除绑定。'),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/projects/${project.id}`);
                    toastSuccess(t('projects.deleteProject', null, '删除项目'));
                    onDeleted?.(project.id);
                } catch (error) {
                    toastError(errText(error, t('projects.deleteFailed', null, '删除失败')));
                }
            }
        });
    };

    if (!project) {
        return (
            <SectionCard title={t('projects.tabSettings', null, '设置')}>
                <Text type="tertiary">{t('projects.selectOrCreateTitle', null, '选择或创建项目')}</Text>
            </SectionCard>
        );
    }

    return (
        <SectionCard
            title={t('projects.settingsIntroTitle', null, '项目设置')}
            description={t('projects.settingsIntroHint', null, '配置项目元数据与 Agent 授权边界，保存后即时生效于绑定对话。')}
            extra={(
                <Button type="primary" loading={saving} disabled={!canWrite} onClick={save}>
                    {t('projects.saveSettings', null, '保存更改')}
                </Button>
            )}
        >
            <div className="project-form-grid">
                <div className="project-form-field">
                    <Text strong size="small">{t('projects.projectName', null, '项目名称')}</Text>
                    <Input value={form.name} disabled={!canWrite} onChange={value => update({ name: value })} />
                    <Text type="tertiary" size="small">{t('projects.saveChangesHint', null, '修改后请点击保存以同步到服务器')}</Text>
                </div>
                <div className="project-form-field">
                    <Text strong size="small">{t('projects.status', null, '状态')}</Text>
                    <Select
                        value={form.status}
                        disabled={!canWrite}
                        optionList={PROJECT_STATUS}
                        onChange={value => update({ status: value })}
                        style={{ width: '100%' }}
                    />
                    <div className="project-switch-row">
                        <Switch checked={form.pinned} disabled={!canWrite} onChange={checked => update({ pinned: checked })} />
                        <Text size="small">{t('projects.pinProject', null, '置顶项目（列表优先显示）')}</Text>
                    </div>
                </div>
            </div>

            <div className="project-form-field">
                <Text strong size="small">{t('projects.projectDescription', null, '项目描述')}</Text>
                <TextArea
                    value={form.description}
                    rows={3}
                    maxCount={4000}
                    disabled={!canWrite}
                    placeholder={t('projects.editDescriptionPlaceholder', null, '客户/任务备注、协作说明、联系人…')}
                    onChange={value => update({ description: value })}
                />
            </div>

            <div className="project-form-field">
                <Space>
                    <Text strong size="small">{t('projects.scopeJsonLabel', null, '范围 JSON')}</Text>
                    <Button size="small" theme="borderless" disabled={!canWrite} onClick={formatScope}>
                        {t('projects.formatJson', null, '格式化')}
                    </Button>
                    <Button size="small" theme="borderless" disabled={!canWrite} onClick={insertExample}>
                        {t('projects.example', null, '示例')}
                    </Button>
                </Space>
                <TextArea
                    value={form.scope_json}
                    rows={8}
                    disabled={!canWrite}
                    placeholder={'{\n  "targets": ["https://example.com"]\n}'}
                    onChange={value => update({ scope_json: value })}
                />
                <Text type="tertiary" size="small">
                    {t('projects.scopeFootnote', null, '支持 targets、exclude、notes 等字段，留空表示不限制范围。')}
                </Text>
            </div>

            <Divider margin="12px" />
            <div className="project-danger-zone">
                <div>
                    <Text strong>{t('projects.dangerZoneTitle', null, '危险操作')}</Text>
                    <div>
                        <Text type="tertiary" size="small">
                            {t('projects.dangerZoneHint', null, '归档后需在列表勾选「显示已归档」才能查看；删除将清除全部事实且不可恢复。')}
                        </Text>
                    </div>
                </div>
                <Space>
                    <Button disabled={!canWrite} onClick={toggleArchive}>
                        {archived ? t('projects.restoreProjectActive', null, '恢复为进行中') : t('projects.archiveProject', null, '归档')}
                    </Button>
                    <Button type="danger" disabled={!canDelete} onClick={remove}>
                        {t('projects.deleteProject', null, '删除项目')}
                    </Button>
                </Space>
            </div>
        </SectionCard>
    );
}
