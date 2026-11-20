import { useId, useMemo, useRef } from 'react';
import { Banner, Button, Popover, Select, Tag, TextArea, Tooltip, Typography } from '@douyinfe/semi-ui';
import { IconArrowUp, IconChevronDown, IconDesktop, IconFolder, IconPaperclip, IconPlus, IconShield, IconStop, IconUser } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { modelChannelName } from '../../lib/model-channel';
import { ModelSettings } from './ChatSettings';
import { reasoningSummary } from './useModelPreferences';

const { Text } = Typography;

export const AGENT_MODES = ['single', 'deep', 'plan_execute', 'supervisor'];
const CREATE_PROJECT = '__create_project__';

export default function Composer({
    value,
    onChange,
    onSend,
    onStop,
    canStop,
    sending,
    checkingRun,
    canWrite,
    canExecute,
    mode,
    onModeChange,
    projectId,
    onProjectChange,
    onCreateProject,
    projects,
    projectDisabled,
    role,
    onRoleChange,
    roles,
    roleDisabled,
    model,
    canReadConfig,
    onOpenHitl,
    attachments,
    onPickFiles,
    onRemoveAttachment,
    uploading
}) {
    const t = useT();
    const fileRef = useRef(null);
    const modeLabelId = useId();
    const projectLabelId = useId();
    const roleLabelId = useId();
    const list = Array.isArray(attachments) ? attachments : [];
    const modeOptions = useMemo(() => [
        {
            value: 'single',
            title: t('chat.agentModePiSingle', null, '单代理'),
            description: t('chat.agentModePiSingleHint', null, '使用设置中选择的 Pi 或兼容运行时执行单代理任务')
        },
        {
            value: 'deep',
            title: t('chat.agentModeDeep', null, 'Deep（DeepAgent）'),
            description: t('chat.agentModeDeepHint', null, 'Pi 多代理，适合复杂安全测试、多阶段 task 子代理委派与汇总')
        },
        {
            value: 'plan_execute',
            title: t('chat.agentModePlanExecuteLabel', null, 'Plan-Execute'),
            description: t('chat.agentModePlanExecuteHint', null, '规划 → 执行 → 重规划（单执行器带工具）')
        },
        {
            value: 'supervisor',
            title: t('chat.agentModeSupervisorLabel', null, 'Supervisor（专家路由）'),
            description: t('chat.agentModeSupervisorHint', null, '专家路由场景：监督者通过 transfer 动态分派多个专业子代理')
        }
    ].map(({ value, title, description }) => ({
        value,
        title,
        label: (
            <span className="chat-mode-option">
                <span>{title}</span>
                <span className="chat-mode-option-description">{description}</span>
            </span>
        )
    })), [t]);
    const projectOptions = [
        { value: '', label: t('chat.defaultWorkspace', null, '默认工作区') },
        ...(Array.isArray(projects) ? projects : []).map(project => ({
            value: project.id,
            label: project.name || t('chat.boundProject', null, '已绑定项目')
        }))
    ];
    if (projectId && !projectOptions.some(option => option.value === projectId)) {
        projectOptions.push({ value: projectId, label: t('chat.boundProject', null, '已绑定项目') });
    }
    if (onCreateProject) {
        projectOptions.push({ value: CREATE_PROJECT, label: `+ ${t('projects.newProject', null, '新建项目')}` });
    }
    const roleOptions = useMemo(() => {
        const list = (Array.isArray(roles) ? roles : []).filter(item => item?.name);
        const named = list
            .filter(item => item.enabled !== false && item.name !== '默认')
            .sort((a, b) => a.name.localeCompare(b.name, 'zh-CN'));
        const fallback = list.find(item => item.name === '默认');
        const toOption = item => ({
            value: item.name === '默认' ? '' : item.name,
            name: item.name === '默认' ? t('chat.defaultRole', null, '默认') : item.name,
            icon: item.icon || '',
            description: item.description || '',
            label: (
                <span className="chat-role-option">
                    <span className="chat-role-option-title">
                        {item.icon ? <span className="chat-role-icon" aria-hidden="true">{item.icon}</span> : null}
                        <span>{item.name === '默认' ? t('chat.defaultRole', null, '默认') : item.name}</span>
                    </span>
                    {item.description ? <span className="chat-role-option-description">{item.description}</span> : null}
                </span>
            )
        });
        const options = [toOption({
            name: '默认',
            icon: fallback?.icon || '',
            description: fallback?.description || ''
        })];
        const seen = new Set(['']);
        for (const item of named) {
            if (seen.has(item.name)) continue;
            seen.add(item.name);
            options.push(toOption(item));
        }
        if (role && !seen.has(role)) {
            const current = list.find(item => item.name === role);
            options.push(toOption(current || { name: role, icon: '', description: '' }));
        }
        return options;
    }, [role, roles, t]);
    const selectedRole = roleOptions.find(option => option.value === (role || '')) || roleOptions[0];
    const uploadingCount = Number(uploading) || list.filter(item => item.uploading).length;
    const blocked = !canWrite || !canExecute;
    const busy = sending || checkingRun;
    const channelId = model.channel || model.config.data?.default_channel;
    const channelName = channelId
        ? modelChannelName(channelId, model.config.data?.channels?.[channelId])
        : t('chat.aiChannelDefaultShort', null, '默认通道');
    const reasoningLabel = model.overrideBlocked
        ? reasoningSummary(t, 'default', '')
        : reasoningSummary(t, model.mode, model.effort);

    const handleKeyDown = event => {
        if (event.key !== 'Enter' || event.shiftKey) return;
        if (event.nativeEvent?.isComposing) return;
        event.preventDefault();
        if (busy || blocked || roleDisabled || uploadingCount > 0) return;
        onSend();
    };

    return (
        <div className="chat-composer">
            {blocked && (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    className="chat-composer-banner"
                    description={!canWrite
                        ? t('chat.noWritePermission', null, '当前账号没有对话写入权限（chat:write），发送已禁用。')
                        : t('chat.noExecutePermission', null, '当前账号没有智能体执行权限（agent:execute），发送已禁用。')}
                />
            )}
            <div className="chat-composer-shell">
                <div className="chat-composer-box">
                    <div className="chat-composer-context">
                        <span id={modeLabelId} hidden>{t('chat.agentModeSelectAria', null, '选择对话执行模式')}</span>
                        <Select
                            value={mode}
                            optionList={modeOptions}
                            renderSelectedItem={option => <span title={option.title}>{option.title}</span>}
                            onChange={onModeChange}
                            prefix={<IconDesktop aria-hidden="true" />}
                            className="chat-context-select chat-composer-mode"
                            dropdownClassName="chat-mode-dropdown"
                            size="small"
                            position="topLeft"
                            aria-labelledby={modeLabelId}
                            disabled={busy || blocked}
                        />
                        <span className="chat-context-divider" aria-hidden="true" />
                        <span id={projectLabelId} hidden>{t('chat.chooseWorkspace', null, '选择会话工作区')}</span>
                        <Select
                            value={projectId}
                            optionList={projectOptions}
                            onChange={value => {
                                if (value === CREATE_PROJECT) onCreateProject?.();
                                else onProjectChange(value);
                            }}
                            prefix={<IconFolder aria-hidden="true" />}
                            className="chat-context-select chat-composer-project"
                            dropdownClassName="chat-project-dropdown"
                            dropdownMatchSelectWidth={false}
                            position="topLeft"
                            size="small"
                            renderSelectedItem={option => <span title={option.label}>{option.label}</span>}
                            aria-labelledby={projectLabelId}
                            disabled={busy || projectDisabled || !canWrite}
                        />
                        {onRoleChange && (
                            <>
                                <span className="chat-context-divider" aria-hidden="true" />
                                <span id={roleLabelId} hidden>{t('chat.selectRole', null, '选择角色')}</span>
                                <Select
                                    value={role || ''}
                                    optionList={roleOptions}
                                    onChange={onRoleChange}
                                    prefix={selectedRole?.icon
                                        ? <span className="chat-role-icon" aria-hidden="true">{selectedRole.icon}</span>
                                        : <IconUser aria-hidden="true" />}
                                    className="chat-context-select chat-composer-role"
                                    dropdownClassName="chat-role-dropdown"
                                    dropdownMatchSelectWidth={false}
                                    position="topLeft"
                                    size="small"
                                    renderSelectedItem={option => <span title={option.description || option.name}>{option.name}</span>}
                                    aria-labelledby={roleLabelId}
                                    disabled={busy || roleDisabled || !canWrite}
                                />
                            </>
                        )}
                    </div>
                    <input
                        ref={fileRef}
                        type="file"
                        multiple
                        hidden
                        onChange={event => {
                            if (event.target.files?.length) onPickFiles(Array.from(event.target.files));
                            event.target.value = '';
                        }}
                    />
                    {list.length > 0 && (
                        <div className="chat-file-list">
                            {list.map(file => (
                                <Tag
                                    key={file.id}
                                    className={`chat-file-chip ${file.error ? 'is-error' : ''} ${file.uploading ? 'is-uploading' : ''}`}
                                    size="large"
                                    color={file.error ? 'red' : (file.uploading ? 'amber' : 'blue')}
                                    closable
                                    onClose={() => onRemoveAttachment(file.id)}
                                >
                                    <IconPaperclip size="small" />
                                    <span className="chat-file-name" title={file.fileName}>{file.fileName}</span>
                                    <span className="chat-file-state">
                                        {file.uploading ? '上传中…' : (file.error ? '上传失败' : '已上传')}
                                    </span>
                                </Tag>
                            ))}
                        </div>
                    )}
                    <TextArea
                        className="chat-textarea"
                        borderless
                        value={value}
                        onChange={onChange}
                        onKeyDown={handleKeyDown}
                        disabled={blocked}
                        autosize={{ minRows: 2, maxRows: 8 }}
                        aria-label={t('chat.composerLabel', null, '对话内容')}
                        placeholder={t('chat.composerPlaceholder', null, '描述目标、授权范围与需求，或附上相关材料…')}
                    />
                    <div className="chat-composer-toolbar">
                        <div className="chat-composer-controls">
                            <Tooltip content={t('chat.selectFile', null, '选择附件')}>
                                <Button
                                    icon={<IconPlus />}
                                    theme="borderless"
                                    type="tertiary"
                                    aria-label={t('chat.attachmentsLabel', null, '附件')}
                                    disabled={blocked}
                                    onClick={() => fileRef.current?.click()}
                                />
                            </Tooltip>
                            <Button
                                icon={<IconShield />}
                                theme="borderless"
                                type="tertiary"
                                className="chat-composer-hitl"
                                aria-label={t('chat.hitlTitle', null, '人机协同')}
                                disabled={!onOpenHitl}
                                onClick={onOpenHitl}
                            >
                                {t('chat.hitlTitle', null, '人机协同')}
                            </Button>
                        </div>
                        <div className="chat-composer-submit">
                            <Popover
                                trigger="click"
                                position="topRight"
                                className="chat-model-popover"
                                condition={!busy}
                                content={<ModelSettings model={model} busy={busy || blocked} canReadConfig={canReadConfig} />}
                            >
                                <Button
                                    theme="borderless"
                                    type="tertiary"
                                    className="chat-composer-model"
                                    aria-label={t('chat.modelSettingsAria', null, '选择 AI 通道、模型与推理设置')}
                                    title={`${channelName} · ${reasoningLabel}`}
                                    disabled={busy}
                                >
                                    <span className="chat-composer-model-name">{channelName}</span>
                                    <span className="chat-composer-effort">{reasoningLabel}</span>
                                    <IconChevronDown size="small" />
                                </Button>
                            </Popover>
                            {sending ? (
                                <Button
                                    type="danger"
                                    className="chat-composer-send"
                                    icon={<IconStop />}
                                    aria-label={t('chat.stop', null, '停止')}
                                    onClick={onStop}
                                    disabled={!canStop}
                                />
                            ) : (
                                <Tooltip content={t('chat.sendHint', null, 'Enter 发送 · Shift+Enter 换行')}>
                                    <Button
                                        theme="solid"
                                        className="chat-composer-send"
                                        icon={<IconArrowUp />}
                                        aria-label={t('chat.send', null, '发送')}
                                        disabled={blocked || checkingRun || roleDisabled || uploadingCount > 0}
                                        onClick={onSend}
                                    />
                                </Tooltip>
                            )}
                        </div>
                    </div>
                    {uploadingCount > 0 && (
                        <Text type="tertiary" size="small" role="status">
                            {t('chat.waitingAttachmentsUpload', null, '附件上传中…')}
                        </Text>
                    )}
                </div>
            </div>
        </div>
    );
}
