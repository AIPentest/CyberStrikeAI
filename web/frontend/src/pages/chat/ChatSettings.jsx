import { useId, useState } from 'react';
import { Button, Select, SideSheet, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { useResource } from '../../components/hooks';
import { ErrorBanner, toastError } from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { modelChannelName, modelChannelOptions, selectedModelChannelName } from '../../lib/model-channel';
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
    const approvalMode = draftApproval ? 'approval' : (conversationId
        ? (approvals.data?.hitl?.enabled ? approvals.data.hitl.mode : 'off')
        : approvals.data?.defaultMode);
    const approvalActive = approvalMode === 'approval' || approvalMode === 'review_edit';
    const reviewer = draftApproval ? 'human' : (conversationId
        ? approvals.data?.hitl?.reviewer
        : approvals.data?.defaultReviewer);
    const usesAuditAgent = reviewer === 'audit_agent';
    const canSelectHuman = canEnableHitl && approvals.data?.conversationHumanApproval && !draftApproval
        && (!conversationId || approvalMode !== 'approval' || reviewer !== 'human');
    const approvalLabel = approvalMode === 'review_edit'
        ? t('chat.hitlModeReviewEdit', null, '审查编辑')
        : t('chat.hitlModeApproval', null, '审批模式');
    const reviewerLabel = usesAuditAgent
        ? t('chat.hitlReviewerAgent', null, '审计 Agent')
        : t('chat.hitlReviewerHuman', null, '人工审批');
    const defaultId = model.config.data?.default_channel;
    const defaultName = defaultId ? modelChannelName(defaultId, model.config.data?.channels?.[defaultId]) : '';
    const defaultLabel = t('chat.aiChannelDefault', null, '跟随默认通道');
    const channelOptions = [
        { value: '', label: defaultName ? `${defaultLabel} · ${defaultName}` : defaultLabel },
        ...modelChannelOptions(model.channels)
    ];
    const reasoningLabels = {
        default: t('chat.reasoningModeDefault', null, '跟随系统'),
        off: t('chat.reasoningModeOff', null, '关闭'),
        on: t('chat.reasoningModeOn', null, '开启'),
        auto: t('chat.reasoningModeAuto', null, '自动')
    };

    const enableApproval = async () => {
        if (!conversationId) {
            onEnableDraftApproval();
            return;
        }
        setSaving(true);
        try {
            const saved = await api.put(`/hitl/config/${encodeURIComponent(conversationId)}/human-approval`);
            approvals.setData(current => ({ ...current, hitl: saved.hitl }));
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
                            renderSelectedItem={selectedModelChannelName}
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
                            <>
                                <div className="chat-setting-approval">
                                    <Tag color={approvalActive ? 'green' : 'grey'}>
                                        {approvalActive ? approvalLabel : t('chat.hitlModeOff', null, '关闭')}
                                    </Tag>
                                    {approvalActive && reviewer && (
                                        <Text type="secondary">
                                            {t('chat.hitlCurrentReviewer', { reviewer: reviewerLabel }, '审批方 · {{reviewer}}')}
                                        </Text>
                                    )}
                                </div>
                                {draftApproval && (
                                    <Text type="tertiary" size="small">
                                        {t('chat.hitlConversationApprovalPending', null, '发送后将为此会话保存人工审批。')}
                                    </Text>
                                )}
                                {canSelectHuman && (
                                    <Button disabled={busy || saving} loading={saving} onClick={enableApproval}>
                                        {t('chat.hitlRequireHumanForConversation', null, '此会话使用人工审批')}
                                    </Button>
                                )}
                            </>
                        )}
                    </section>
                )}
            </div>
        </SideSheet>
    );
}
