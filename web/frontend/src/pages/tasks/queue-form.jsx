/** 新建批量任务队列：任务列表、角色、项目、代理模式、审批策略、调度与并发。 */
import { useEffect, useState } from 'react';
import { Checkbox, Input, Select, TextArea, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { Drawer, toastError, toastSuccess } from '../../components/ui';
import { AGENT_MODES, HITL_POLICIES, SCHEDULE_MODES, errText } from './helpers';

const { Text } = Typography;

const EMPTY = {
    title: '',
    tasksText: '',
    role: '',
    projectId: '',
    collectionId: '',
    agentMode: 'single',
    hitlPolicy: '',
    scheduleMode: 'manual',
    cronExpr: '',
    concurrency: 1,
    executeNow: false
};

const CRON_PATTERN = /^\S+\s+\S+\s+\S+\s+\S+\s+\S+$/;

export default function QueueFormModal({ visible, onCancel, onCreated }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const [form, setForm] = useState(EMPTY);
    const [roles, setRoles] = useState([]);
    const [projects, setProjects] = useState([]);
    const [collections, setCollections] = useState([]);
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        if (!visible) return;
        setForm(EMPTY);
        setSaving(false);
        let cancelled = false;
        if (hasPermission('roles:read')) {
            api.get('/roles')
                .then(payload => { if (!cancelled) setRoles(Array.isArray(payload?.roles) ? payload.roles : []); })
                .catch(() => { if (!cancelled) setRoles([]); });
        }
        if (hasPermission('project:read')) {
            api.get('/projects', { limit: 500 })
                .then(payload => { if (!cancelled) setProjects(Array.isArray(payload?.projects) ? payload.projects : []); })
                .catch(() => { if (!cancelled) setProjects([]); });
        }
        return () => { cancelled = true; };
    }, [visible, hasPermission]);

    useEffect(() => {
        if (!visible || !form.projectId || !hasPermission('asset:read')) {
            setCollections([]);
            return undefined;
        }
        let cancelled = false;
        api.get('/assets/collections', { project_id: form.projectId })
            .then(payload => { if (!cancelled) setCollections(Array.isArray(payload?.collections) ? payload.collections : []); })
            .catch(() => { if (!cancelled) setCollections([]); });
        return () => { cancelled = true; };
    }, [visible, form.projectId, hasPermission]);

    const update = patch => setForm(prev => ({ ...prev, ...patch }));

    const submit = async () => {
        const tasks = form.tasksText.split('\n').map(line => line.trim()).filter(Boolean);
        if (!tasks.length) {
            toastError(t('tasks.noValidTask', null, '没有有效的任务'));
            return;
        }
        if (form.scheduleMode === 'cron') {
            if (!form.cronExpr.trim()) {
                toastError(t('batchImportModal.cronExprRequired', null, '请选择 Cron 调度后填写 Cron 表达式'));
                return;
            }
            if (!CRON_PATTERN.test(form.cronExpr.trim())) {
                toastError(t('batchImportModal.cronExprInvalid', null, 'Cron 表达式格式错误，需要 5 段（分 时 日 月 周）'));
                return;
            }
        }
        let concurrency = Number(form.concurrency);
        if (!Number.isFinite(concurrency) || concurrency < 1) concurrency = 1;
        if (concurrency > 8) concurrency = 8;
        setSaving(true);
        try {
            const payload = await api.post('/batch-tasks', {
                title: form.title.trim(),
                tasks,
                role: form.role || '',
                projectId: form.projectId || '',
                collectionId: form.collectionId || '',
                agentMode: form.agentMode || 'single',
                hitlPolicy: form.hitlPolicy || '',
                scheduleMode: form.scheduleMode === 'cron' ? 'cron' : 'manual',
                cronExpr: form.scheduleMode === 'cron' ? form.cronExpr.trim() : '',
                concurrency,
                executeNow: Boolean(form.executeNow)
            });
            toastSuccess(t('tasks.batchQueueTitle', null, '批量任务队列'));
            onCreated?.(payload);
        } catch (error) {
            toastError(errText(error, t('tasks.createBatchQueueFailed', null, '创建批量任务队列失败')));
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={t('batchImportModal.title', null, '新建任务')}
            onCancel={onCancel}
            onOk={submit}
            okText={t('batchImportModal.createQueue', null, '创建队列')}
            cancelText={t('common.cancel', null, '取消')}
            confirmLoading={saving}
            width={760}
            maskClosable={false}
        >
            <div className="task-form-grid">
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.queueTitle', null, '任务标题')}</Text>
                    <Input
                        value={form.title}
                        maxLength={200}
                        placeholder={t('batchImportModal.queueTitlePlaceholder', null, '请输入任务标题（可选，用于标识和筛选）')}
                        onChange={value => update({ title: value })}
                    />
                </div>
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.role', null, '角色')}</Text>
                    <Select
                        value={form.role}
                        style={{ width: '100%' }}
                        optionList={[{ value: '', label: t('batchImportModal.defaultRole', null, '默认') },
                            ...roles.map(item => ({ value: item.name, label: item.name }))]}
                        onChange={value => update({ role: value })}
                    />
                    <Text type="tertiary" size="small">{t('batchImportModal.roleHint', null, '选择一个角色，所有任务将使用该角色的配置（提示词和工具）执行。')}</Text>
                </div>
            </div>

            <div className="task-form-grid">
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.project', null, '所属项目')}</Text>
                    <Select
                        value={form.projectId}
                        style={{ width: '100%' }}
                        optionList={[{ value: '', label: t('batchImportModal.projectNone', null, '（未绑定）') },
                            ...projects.map(item => ({ value: item.id, label: item.name || item.id }))]}
                        onChange={value => update({ projectId: value || '', collectionId: '' })}
                    />
                </div>
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.agentMode', null, '代理模式')}</Text>
                    <Select
                        value={form.agentMode}
                        style={{ width: '100%' }}
                        optionList={AGENT_MODES}
                        onChange={value => update({ agentMode: value })}
                    />
                    <Text type="tertiary" size="small">{t('batchImportModal.agentModeHint', null, '与对话页一致：Pi 单代理，或 Deep / Plan-Execute / Supervisor（后三种需已启用多代理）。')}</Text>
                </div>
            </div>

            <div className="task-form-field">
                <Text strong size="small">资产集合</Text>
                <Select
                    value={form.collectionId}
                    style={{ width: '100%' }}
                    disabled={!form.projectId}
                    optionList={[{ value: '', label: '不指定集合' },
                        ...collections.map(item => ({ value: item.id, label: `${item.name}（${item.member_count || 0}）` }))]}
                    onChange={value => update({ collectionId: value || '' })}
                />
                <Text type="tertiary" size="small">与信息收集、深度测评共用同一批资产。不改变队列的暂停和子任务行为。</Text>
            </div>

            <div className="task-form-field">
                <Text strong size="small">{t('batchImportModal.tasksList', null, '任务列表（每行一个任务）')}</Text>
                <TextArea
                    value={form.tasksText}
                    rows={7}
                    placeholder={t('batchImportModal.tasksListPlaceholderExample', null, '请输入任务列表，每行一个任务')}
                    onChange={value => update({ tasksText: value })}
                />
                <Text type="tertiary" size="small">{t('batchImportModal.tasksListHint', null, '每行输入一个任务指令，系统将按并发池执行这些任务。空行会被自动忽略。')}</Text>
            </div>

            <div className="task-form-grid">
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.hitlPolicy', null, '工具审批')}</Text>
                    <Select
                        value={form.hitlPolicy}
                        style={{ width: '100%' }}
                        optionList={HITL_POLICIES}
                        onChange={value => update({ hitlPolicy: value })}
                    />
                </div>
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.concurrency', null, '并发数')}</Text>
                    <Input
                        type="number"
                        min={1}
                        max={8}
                        value={String(form.concurrency)}
                        onChange={value => update({ concurrency: value })}
                    />
                    <Text type="tertiary" size="small">{t('batchImportModal.concurrencyHint', null, '同时执行的子任务数量（1-8）。默认 1 为串行；含扫描类工具时建议 1-2。')}</Text>
                </div>
            </div>

            <div className="task-form-grid">
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.scheduleMode', null, '调度方式')}</Text>
                    <Select
                        value={form.scheduleMode}
                        style={{ width: '100%' }}
                        optionList={SCHEDULE_MODES}
                        onChange={value => update({ scheduleMode: value })}
                    />
                </div>
                <div className="task-form-field">
                    <Text strong size="small">{t('batchImportModal.cronExpr', null, 'Cron 表达式')}</Text>
                    <Input
                        value={form.cronExpr}
                        disabled={form.scheduleMode !== 'cron'}
                        placeholder={t('batchImportModal.cronExprPlaceholder', null, '例如：0 */2 * * *（每2小时执行一次）')}
                        onChange={value => update({ cronExpr: value })}
                    />
                    <Text type="tertiary" size="small">{t('batchImportModal.cronExprHint', null, '采用标准 5 段 Cron：分 时 日 月 周。')}</Text>
                </div>
            </div>

            <div className="task-form-field">
                <Checkbox checked={form.executeNow} onChange={event => update({ executeNow: event.target.checked })}>
                    {t('batchImportModal.executeNow', null, '创建后立即执行')}
                </Checkbox>
                <Text type="tertiary" size="small">{t('batchImportModal.executeNowHint', null, '默认不立即执行；关闭后队列保持待执行，可在需要时手动开始。')}</Text>
            </div>
        </Drawer>
    );
}
