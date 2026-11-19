/** 发送区：文本、附件、代理模式、项目选择、发送/停止。 */
import { useRef } from 'react';
import { Banner, Button, Select, Space, Tag, TextArea, Tooltip, Typography } from '@douyinfe/semi-ui';
import { IconPaperclip, IconSend, IconSetting, IconStop } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';

const { Text } = Typography;

export const AGENT_MODES = ['single', 'deep', 'plan_execute', 'supervisor'];

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
    onOpenSettings,
    projectId,
    onProjectChange,
    projects,
    showProjectSelect,
    attachments,
    onPickFiles,
    onRemoveAttachment,
    uploading
}) {
    const t = useT();
    const fileRef = useRef(null);
    const list = Array.isArray(attachments) ? attachments : [];
    const modeOptions = [
        { value: 'single', label: t('chat.agentModePiSingle', null, '单代理') },
        { value: 'deep', label: t('chat.agentModeDeep', null, '深度协作（deep）') },
        { value: 'plan_execute', label: t('chat.agentModePlanExecuteLabel', null, '先规划再执行') },
        { value: 'supervisor', label: t('chat.agentModeSupervisorLabel', null, '监督者（supervisor）') }
    ];
    const projectOptions = [
        { value: '', label: t('chat.noProject', null, '不绑定项目') },
        ...(Array.isArray(projects) ? projects : []).map(project => ({ value: project.id, label: project.name || project.id }))
    ];
    const uploadingCount = Number(uploading) || list.filter(item => item.uploading).length;
    const blocked = !canWrite || !canExecute;

    const handleKeyDown = event => {
        if (event.key !== 'Enter' || event.shiftKey) return;
        if (event.nativeEvent?.isComposing) return;
        event.preventDefault();
        if (sending || checkingRun || blocked || uploadingCount > 0) return;
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
            <div className="chat-composer-box">
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
                    <Space spacing={8} wrap className="chat-composer-controls">
                        <Tooltip content={t('chat.selectFile', null, '选择附件')}>
                            <Button
                                icon={<IconPaperclip />}
                                theme="borderless"
                                disabled={blocked}
                                onClick={() => fileRef.current?.click()}
                            >
                                {t('chat.attachmentsLabel', null, '附件')}
                            </Button>
                        </Tooltip>
                        <Select
                            value={mode}
                            optionList={modeOptions}
                            onChange={onModeChange}
                            className="chat-composer-select"
                            aria-label={t('chat.agentModeSelectAria', null, '选择对话执行模式')}
                            disabled={sending}
                        />
                        {showProjectSelect && (
                            <Select
                                value={projectId}
                                optionList={projectOptions}
                                onChange={onProjectChange}
                                className="chat-composer-select"
                                aria-label={t('chat.filterByProject', null, '选择项目')}
                                placeholder={t('chat.filterByProject', null, '选择项目')}
                                disabled={sending}
                            />
                        )}
                        {onOpenSettings && (
                            <Button icon={<IconSetting aria-hidden="true" />} theme="borderless" onClick={onOpenSettings}>
                                {t('chat.sessionSettingsTitle', null, '会话设置')}
                            </Button>
                        )}
                    </Space>
                    <Space spacing={8} wrap className="chat-composer-submit">
                        <Text type="tertiary" size="small" className="chat-composer-hint">
                            {uploadingCount > 0
                                ? t('chat.waitingAttachmentsUpload', null, '附件上传中…')
                                : t('chat.sendHint', null, 'Enter 发送 · Shift+Enter 换行')}
                        </Text>
                        {sending ? (
                            <Button type="danger" icon={<IconStop />} onClick={onStop} disabled={!canStop}>
                                {t('chat.stop', null, '停止')}
                            </Button>
                        ) : (
                            <Button
                                theme="solid"
                                icon={<IconSend />}
                                disabled={blocked || checkingRun || uploadingCount > 0}
                                onClick={onSend}
                            >
                                {t('chat.send', null, '发送')}
                            </Button>
                        )}
                    </Space>
                </div>
            </div>
        </div>
    );
}
