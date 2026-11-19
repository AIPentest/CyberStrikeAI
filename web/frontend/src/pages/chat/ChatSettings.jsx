import { useId, useState } from 'react';
import { Button, Select, SideSheet, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { useResource } from '../../components/hooks';
import { ErrorBanner, toastError } from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { REASONING_EFFORTS, REASONING_MODES } from './useModelPreferences';

const { Text } = Typography;

function SettingSelect({ label, ...props }) {
    const id = useId();
    return (
        <div className="chat-setting-field">
            <Text id={id} strong>{label}</Text>
            <Select aria-labelledby={id} {...props} />
        </div>
    );
}

export default function ChatSettings({
    onClose, narrow, busy, canReadConfig, canReadHitl, canEnableHitl,
    model, conversationId, draftApproval, onEnableDraftApproval
}) {
    const t = useT();
    const [saving, setSaving] = useState(false);
    const approvals = useResource(
        () => api.get(conversationId ? `/hitl/config/${encodeURIComponent(conversationId)}` : '/hitl/default-config'),
        [conversationId, canReadHitl],
        { skip: !canReadHitl }
    );
    const approvalMode = conversationId
        ? (approvals.data?.hitl?.enabled ? approvals.data.hitl.mode : 'off')
        : approvals.data?.defaultMode;
    const approvalActive = approvalMode === 'approval' || approvalMode === 'review_edit';
    const approvalLabel = approvalMode === 'review_edit'
        ? t('chat.hitlModeReviewEdit', null, '审查编辑')
        : t('chat.hitlModeApproval', null, '审批模式');
    const defaultChannel = model.config.data?.default_channel;
    const defaultModel = model.channels.find(([id]) => id === defaultChannel)?.[1]?.model;
    const defaultLabel = t('chat.aiChannelDefault', null, '跟随默认通道');
    const channelOptions = [
        { value: '', label: defaultModel ? `${defaultLabel} · ${defaultModel}` : defaultLabel },
        ...model.channels.map(([id, config]) => ({
            value: id,
            label: [config?.name || id, config?.model].filter(Boolean).join(' · ')
        }))
    ];
    const reasoningLabels = {
        default: t('chat.reasoningModeDefault', null, '跟随系统'),
        off: t('chat.reasoningModeOff', null, '关闭'),
        on: t('chat.reasoningModeOn', null, '开启'),
        auto: t('chat.reasoningModeAuto', null, '自动')
    };

    const enableApproval = async () => {
        const hitl = {
            enabled: true,
            mode: 'approval',
            reviewer: 'human',
            timeoutSeconds: approvals.data.defaultTimeoutSeconds ?? 300
        };
        if (!conversationId) {
            onEnableDraftApproval(hitl);
            return;
        }
        setSaving(true);
        try {
            await api.put('/hitl/config', { conversationId, ...hitl });
            approvals.setData(current => ({ ...current, hitl }));
        } catch (error) {
            toastError(error, t('chat.hitlApplyFail', null, '同步到服务器失败'));
        } finally {
            setSaving(false);
        }
    };

    return (
        <SideSheet
            visible
            title={t('chat.sessionSettingsTitle', null, '会话设置')}
            width={narrow ? '100%' : 420}
            closable={!saving}
            maskClosable={!saving}
            closeOnEsc={!saving}
            onCancel={onClose}
        >
            <div className="chat-settings-fields">
                {canReadConfig && (
                    <>
                        <SettingSelect
                            label={t('chat.aiChannelLabel', null, 'AI 通道')}
                            value={model.channel}
                            optionList={channelOptions}
                            onSelect={model.setChannel}
                            loading={model.config.loading}
                            disabled={busy || model.config.loading || Boolean(model.config.error)}
                        />
                        <ErrorBanner error={model.config.error} onRetry={() => model.config.reload()} />
                    </>
                )}
                <SettingSelect
                    label={t('chat.reasoningModeLabel', null, '模型推理')}
                    value={model.mode}
                    optionList={REASONING_MODES.map(value => ({ value, label: reasoningLabels[value] }))}
                    onSelect={model.setMode}
                    disabled={busy}
                />
                <SettingSelect
                    label={t('chat.reasoningEffortLabel', null, '推理强度')}
                    value={model.mode === 'off' ? '' : model.effort}
                    optionList={[
                        { value: '', label: t('chat.reasoningEffortUnset', null, '不指定') },
                        ...REASONING_EFFORTS.map(value => ({ value, label: value }))
                    ]}
                    onSelect={model.setEffort}
                    disabled={busy || model.mode === 'off'}
                />
                {canReadHitl && (
                    <section className="chat-setting-field">
                        <Text strong>{t('chat.hitlTitle', null, '人机协同')}</Text>
                        <ErrorBanner error={approvals.error} onRetry={() => approvals.reload()} />
                        {approvals.loading && <Spin />}
                        {approvals.data && !approvals.error && (
                            <div className="chat-setting-approval">
                                <Tag color={approvalActive || draftApproval ? 'green' : 'grey'}>
                                    {draftApproval && !conversationId
                                        ? t('chat.hitlFirstMessageEnabled', null, '首条消息将使用人工审批')
                                        : approvalActive ? approvalLabel : t('chat.hitlModeOff', null, '关闭')}
                                </Tag>
                                {canEnableHitl && !approvalActive && !draftApproval && (
                                    <Button disabled={busy || saving} loading={saving} onClick={enableApproval}>
                                        {conversationId
                                            ? t('chat.hitlEnableHuman', null, '开启人工审批')
                                            : t('chat.hitlEnableFirstMessage', null, '为首条消息开启人工审批')}
                                    </Button>
                                )}
                            </div>
                        )}
                    </section>
                )}
            </div>
        </SideSheet>
    );
}
