import { useEffect, useId, useRef, useState } from 'react';
import { RadioGroup, Select, SideSheet, Spin, TextArea, Typography } from '@douyinfe/semi-ui';
import { useResource } from '../../components/hooks';
import { ErrorBanner, toastError } from '../../components/ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { modelChannelName, modelChannelOptions, selectedModelChannelName } from '../../lib/model-channel';
import { REASONING_EFFORTS, REASONING_MODES } from './useModelPreferences';

const { Text } = Typography;
const HITL_MODES = ['off', 'approval', 'review_edit'];
const HITL_TIMEOUTS = [60, 300, 600, 0];
const DEFAULT_HITL_SESSION_TOOL_WHITELIST = 'tool_search, skill, task, write_todos, transfer_to_agent, exit, TaskCreate, TaskGet, TaskUpdate, TaskList, upsert_project_fact, get_project_fact';

function normalizeMode(mode, enabled) {
    if (enabled === false) return 'off';
    return HITL_MODES.includes(mode) ? mode : 'off';
}

function normalizeReviewer(reviewer) {
    return reviewer === 'audit_agent' ? 'audit_agent' : 'human';
}

function normalizeTimeout(value, fallback = 300) {
    if (value === undefined || value === null || value === '') return fallback;
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) return fallback;
    return Math.max(0, Math.min(86400, Math.round(parsed)));
}

function splitTools(value) {
    const tools = Array.isArray(value) ? value : String(value || '').split(/[,\n\r]+/);
    return tools.map(tool => String(tool || '').trim()).filter(Boolean);
}

function sessionToolsForDisplay(value, globalTools) {
    const global = new Set(splitTools(globalTools).map(tool => tool.toLowerCase()));
    return splitTools(value).filter(tool => !global.has(tool.toLowerCase())).join(', ');
}

function formFromConfig(data, conversationId, draft) {
    if (draft) {
        return {
            mode: normalizeMode(draft.mode, draft.enabled),
            reviewer: normalizeReviewer(draft.reviewer),
            timeoutSeconds: normalizeTimeout(draft.timeoutSeconds),
            sensitiveTools: splitTools(draft.sensitiveTools).join(', ')
        };
    }
    const stored = conversationId ? data?.hitl : null;
    return {
        mode: conversationId
            ? normalizeMode(stored?.mode, stored?.enabled)
            : normalizeMode(data?.defaultMode),
        reviewer: normalizeReviewer(conversationId ? stored?.reviewer : data?.defaultReviewer),
        timeoutSeconds: normalizeTimeout(conversationId
            ? stored?.timeoutSeconds ?? data?.defaultTimeoutSeconds
            : data?.defaultTimeoutSeconds),
        sensitiveTools: conversationId
            ? sessionToolsForDisplay(stored?.sensitiveTools, data?.hitlGlobalToolWhitelist)
            : DEFAULT_HITL_SESSION_TOOL_WHITELIST
    };
}

function requestFromForm(form) {
    return {
        enabled: form.mode !== 'off',
        mode: form.mode,
        reviewer: form.reviewer,
        sensitiveTools: splitTools(form.sensitiveTools),
        timeoutSeconds: form.timeoutSeconds
    };
}

function SettingSelect({ label, ...props }) {
    const id = useId();
    return (
        <div className="chat-setting-field">
            <Text id={id} strong>{label}</Text>
            <Select aria-labelledby={id} {...props} />
        </div>
    );
}

export function ModelSettings({ model, busy, canReadConfig }) {
    const t = useT();
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
    return (
        <>
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
        </>
    );
}

export default function ChatSettings({
    onClose, narrow, busy, canReadConfig, canReadHitl, canEnableHitl,
    model, conversationId, draftHitl, onDraftHitlChange, section = 'all'
}) {
    const t = useT();
    const [saving, setSaving] = useState(false);
    const [saved, setSaved] = useState(false);
    const [form, setForm] = useState(null);
    const [toolsDirty, setToolsDirty] = useState(false);
    const saveSequence = useRef(0);
    const draftRef = useRef(draftHitl);
    const reviewerId = useId();
    const toolsId = useId();
    draftRef.current = draftHitl;

    const approvals = useResource(
        () => api.get(conversationId ? `/hitl/config/${encodeURIComponent(conversationId)}` : '/hitl/default-config'),
        [conversationId, canReadHitl],
        { skip: !canReadHitl }
    );

    useEffect(() => {
        if (!approvals.data) return;
        setForm(formFromConfig(approvals.data, conversationId, draftRef.current));
        setToolsDirty(false);
    }, [approvals.data, conversationId]);

    const modeLabels = {
        off: t('chat.hitlModeOff', null, '关闭'),
        approval: t('chat.hitlModeApproval', null, '审批模式'),
        review_edit: t('chat.hitlModeReviewEdit', null, '审查编辑')
    };
    const timeoutLabels = {
        60: t('chat.hitlTimeoutOneMinute', null, '1 分钟'),
        300: t('chat.hitlTimeoutFiveMinutes', null, '5 分钟'),
        600: t('chat.hitlTimeoutTenMinutes', null, '10 分钟'),
        0: t('chat.hitlTimeoutUnlimited', null, '不限制')
    };
    const timeoutOptions = HITL_TIMEOUTS.map(value => ({ value, label: timeoutLabels[value] }));
    if (form && !HITL_TIMEOUTS.includes(form.timeoutSeconds)) {
        timeoutOptions.unshift({ value: form.timeoutSeconds, label: `${form.timeoutSeconds}s` });
    }
    const hitlDisabled = !canEnableHitl || saving;

    const persist = async next => {
        const sequence = ++saveSequence.current;
        const hitl = requestFromForm(next);
        setSaving(true);
        setSaved(false);
        try {
            await api.put('/hitl/config', { conversationId, ...hitl });
            if (sequence !== saveSequence.current) return;
            approvals.setData(current => ({ ...current, hitl }));
            setSaved(true);
        } catch (error) {
            if (sequence === saveSequence.current) {
                toastError(error, t('chat.hitlApplyFail', null, '同步到服务器失败'));
                approvals.reload({ silent: true });
            }
        } finally {
            if (sequence === saveSequence.current) setSaving(false);
        }
    };

    const updateConfig = patch => {
        const next = { ...form, ...patch };
        setForm(next);
        setToolsDirty(false);
        setSaved(false);
        if (conversationId) {
            void persist(next);
        } else {
            onDraftHitlChange(requestFromForm(next));
        }
    };

    const updateTools = value => {
        const next = { ...form, sensitiveTools: value };
        setForm(next);
        setToolsDirty(true);
        setSaved(false);
        if (!conversationId) onDraftHitlChange(requestFromForm(next));
    };

    const saveTools = () => {
        if (!toolsDirty || !conversationId || hitlDisabled) return;
        setToolsDirty(false);
        void persist(form);
    };

    return (
        <SideSheet
            visible
            title={section === 'hitl' ? t('chat.hitlTitle', null, '人机协同') : t('chat.sessionSettingsTitle', null, '会话设置')}
            width={narrow ? '100%' : 420}
            closable={!saving}
            maskClosable={!saving}
            closeOnEsc={!saving}
            onCancel={onClose}
        >
            <div className="chat-settings-fields">
                {section !== 'hitl' && <ModelSettings model={model} busy={busy} canReadConfig={canReadConfig} />}
                {canReadHitl && (
                    <section className="chat-setting-field chat-setting-hitl">
                        {section !== 'hitl' && <Text strong>{t('chat.hitlTitle', null, '人机协同')}</Text>}
                        <Text type="tertiary" size="small">
                            {t('chat.sessionSettingsHint', null, '人机协同设置只影响后续消息。')}
                        </Text>
                        {(saving || saved) && (
                            <Text type="tertiary" size="small" role="status">
                                {saving
                                    ? t('chat.systemModelSaving', null, '正在保存…')
                                    : t('chat.hitlApplyOkSync', null, '人机协同配置已保存并同步到服务器。')}
                            </Text>
                        )}
                        <ErrorBanner error={approvals.error} onRetry={() => approvals.reload()} />
                        {approvals.loading && <Spin />}
                        {form && approvals.data && !approvals.error && (
                            <>
                                <SettingSelect
                                    label={t('chat.hitlModeLabel', null, '模式')}
                                    value={form.mode}
                                    optionList={HITL_MODES.map(value => ({ value, label: modeLabels[value] }))}
                                    onSelect={value => updateConfig({ mode: value })}
                                    disabled={hitlDisabled}
                                />
                                <div className="chat-setting-field">
                                    <Text id={reviewerId} strong>{t('chat.hitlReviewerLabel', null, '审批方')}</Text>
                                    <RadioGroup
                                        aria-labelledby={reviewerId}
                                        type="button"
                                        className="chat-setting-reviewer"
                                        value={form.reviewer}
                                        options={[
                                            { value: 'human', label: t('chat.hitlReviewerHuman', null, '人工审批') },
                                            { value: 'audit_agent', label: t('chat.hitlReviewerAgent', null, '审计 Agent') }
                                        ]}
                                        onChange={event => updateConfig({ reviewer: event.target.value })}
                                        disabled={hitlDisabled}
                                    />
                                    <Text type="tertiary" size="small">
                                        {t('chat.hitlReviewerHint', null, '可在人工与审计 Agent 之间随时切换；规则与白名单不变。人机协同为「关闭」时也可预先选择。')}
                                    </Text>
                                </div>
                                <SettingSelect
                                    label={t('chat.hitlTimeoutLabel', null, '审批等待时限')}
                                    value={form.timeoutSeconds}
                                    optionList={timeoutOptions}
                                    onSelect={value => updateConfig({ timeoutSeconds: Number(value) })}
                                    disabled={hitlDisabled}
                                />
                                <div className="chat-setting-field">
                                    <Text id={toolsId} strong>
                                        {t('chat.hitlWhitelistTools', null, '白名单工具（免审批，逗号分隔）')}
                                    </Text>
                                    <TextArea
                                        aria-labelledby={toolsId}
                                        value={form.sensitiveTools}
                                        placeholder={t('chat.hitlWhitelistPlaceholder', null, '例：read_file, grep 或每行一个工具名（与 config 全局白名单合并）')}
                                        rows={5}
                                        disabled={hitlDisabled}
                                        onChange={updateTools}
                                        onBlur={saveTools}
                                    />
                                    <Text type="tertiary" size="small">
                                        {t('chat.hitlWhitelistHint', null, '白名单内工具免审批；每行一个或逗号分隔，与 config 全局白名单合并。')}
                                    </Text>
                                </div>
                            </>
                        )}
                    </section>
                )}
            </div>
        </SideSheet>
    );
}
