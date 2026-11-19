/**
 * 编辑所选模型通道，并使用表单中的值测试连接或探测模型能力。
 */
import { useId, useState } from 'react';
import { Banner, Button, Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { modelChannelOptions, selectedModelChannelName } from '../../../lib/model-channel';
import { ErrorBanner, KeyValueList, SectionCard, toastSuccess } from '../../../components/ui';
import { Hint } from './common';

const { Text } = Typography;

const MAIN_CHANNEL = '__main__';
const NO_CHANNELS = {};
const PROVIDERS = [
    { value: 'openai_compatible', key: 'settingsProbe.compatible', fallback: '兼容 OpenAI 的接口' },
    { value: 'claude', key: 'settingsBasic.providerClaude', fallback: 'Claude（Anthropic Messages）' }
];
const CONTEXT_PROFILES = ['', 'auto', 'local_auto', 'local', 'economy', 'custom'];
const TOKEN_LIMITS = [
    ['context_window', 'settingsBasic.contextWindow', '模型上下文窗口（Token）'],
    ['max_total_tokens', 'settingsBasic.maxTotalTokens', '最大上下文令牌数'],
    ['max_completion_tokens', 'settingsBasic.maxCompletionTokens', '最大输出令牌数']
];
const EMPTY_PROBE = { key: '', payload: null, error: null };
const CONTEXT_WARNING_KEYS = {
    context_window_unknown: 'contextWarnUnknown',
    context_runtime_unknown: 'contextWarnRuntimeUnknown',
    local_context_window_unknown: 'contextWarnWindowUnknown',
    local_context_window_small: 'contextWarnWindowSmall',
    max_total_tokens_clamped: 'contextWarnBudgetClamped',
    max_completion_tokens_clamped: 'contextWarnCompletionClamped',
    configured_budget_exceeds_context_window: 'contextWarnBudgetExceedsWindow',
    ollama_v1_ignores_num_ctx: 'contextWarnOllamaV1',
    ollama_runtime_context_unknown: 'contextWarnOllamaRuntime',
    llamacpp_parallel_slots: 'contextWarnLlamaCppSlots'
};
const BACKEND_NAMES = { ollama: 'Ollama', llamacpp: 'llama.cpp', lmstudio: 'LM Studio', omlx: 'oMLX', vllm: 'vLLM' };

function profileLabel(t, value) {
    return t('settingsProbe.profiles.' + (value || 'default'), null, value || '使用默认设置');
}

function numberValue(raw) {
    const value = Number(raw);
    return Number.isSafeInteger(value) && value >= 0 ? value : 0;
}

function channelValues(channel) {
    let provider = channel.provider || 'openai_compatible';
    if (provider === 'openai') provider = 'openai_compatible';
    if (provider === 'anthropic') provider = 'claude';
    return {
        name: channel.name || '',
        provider,
        base_url: channel.base_url || '',
        api_key: channel.api_key || '',
        model: channel.model || '',
        context_profile: channel.context_profile || '',
        context_window: channel.context_window > 0 ? String(channel.context_window) : '',
        max_total_tokens: channel.max_total_tokens || 0,
        max_completion_tokens: channel.max_completion_tokens || 0,
        headers: channel.headers || undefined
    };
}

function summarizeTest(t, payload) {
    return [
        { label: t('settingsProbe.result', null, '测试结果'), value: <Tag color="green">{t('settingsBasic.testSuccess', null, '连接成功')}</Tag> },
        { label: t('settingsBasic.model', null, '模型'), value: payload.model || '—' },
        {
            label: t('settingsProbe.latency', null, '响应耗时'),
            value: payload.latency_ms != null
                ? t('settingsProbe.milliseconds', { value: payload.latency_ms }, `${payload.latency_ms} 毫秒`)
                : '—'
        }
    ];
}

function summarizeContext(t, payload, fallbackWindow) {
    const source = payload.context_source;
    const sourceKey = source?.replace(/^omlx_models_(?=max_model_len|context_length|max_context_length|context_window)/, 'openai_models_');
    const backend = payload.context_backend;
    return [
        ...summarizeTest(t, payload),
        { label: t('settingsProbe.window', null, '上下文窗口'), value: payload.context_window || fallbackWindow || '—' },
        {
            label: t('settingsProbe.backend', null, '模型接口类型'),
            value: backend === 'openai_compatible'
                ? t('settingsProbe.compatible', null, '兼容 OpenAI 的接口')
                : BACKEND_NAMES[backend] || backend || '—'
        },
        {
            label: t('settingsProbe.source', null, '窗口信息来源'),
            value: fallbackWindow ? t('settingsProbe.windowDefault', null, '默认值') : (source ? t('settingsProbe.sources.' + sourceKey, null, source) : '—')
        },
        { label: t('settingsProbe.profile', null, '上下文配置'), value: profileLabel(t, payload.context_profile) },
        { label: t('settingsProbe.effectiveBudget', null, '实际可用上下文令牌数'), value: payload.effective_max_total_tokens ?? '—' }
    ];
}

function ProbeOutput({ state, defaultWindow }) {
    const t = useT();
    const { key, payload, error } = state;
    if (error) return <ErrorBanner error={error} />;
    if (!payload) return null;

    if (key === 'list') {
        if (!payload.success) {
            return (
                <Banner
                    type={payload.supported === false ? 'info' : 'danger'}
                    bordered
                    closeIcon={null}
                    description={payload.error || t('settingsBasic.modelsListFailed', null, '获取模型列表失败')}
                />
            );
        }
        const models = Array.isArray(payload.models) ? payload.models : [];
        return (
            <div className="settings-probe-result">
                <Space spacing={8}>
                    <Text>{t('settingsBasic.modelsListSuccess', { count: payload.count ?? models.length }, `已获取 ${payload.count ?? models.length} 个模型`)}</Text>
                </Space>
                <div className="settings-model-tags">
                    {models.slice(0, 120).map(model => <Tag key={String(model)} size="small">{String(model)}</Tag>)}
                    {models.length > 120 && <Tag size="small" color="grey">…</Tag>}
                </div>
            </div>
        );
    }

    if (key === 'typesafe' || key === 'vision') {
        return (
            <div className="settings-probe-result">
                {payload.success ? (
                    <KeyValueList
                        columns={3}
                        items={summarizeTest(t, payload)}
                    />
                ) : (
                    <Banner type="danger" bordered closeIcon={null} description={payload.error || t('settingsBasic.testFailed', null, '连接失败')} />
                )}
            </div>
        );
    }

    if (!payload.success) {
        return <Banner type="danger" bordered closeIcon={null} description={payload.error || t('settingsBasic.testFailed', null, '连接失败')} />;
    }
    const fallbackWindow = !payload.context_window && !payload.context_runtime_unknown ? defaultWindow : 0;
    const warnings = (Array.isArray(payload.context_warnings) ? payload.context_warnings : [])
        .filter(code => !fallbackWindow || code !== 'context_window_unknown');
    return (
        <div className="settings-probe-result">
            <KeyValueList columns={3} items={summarizeContext(t, payload, fallbackWindow)} />
            {warnings.length > 0 && (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    description={(
                        <div>
                            <Text strong>{t('settingsProbe.warnings', null, '上下文提示')}</Text>
                            <ul>
                                {[...new Set(warnings)].map(code => (
                                    <li key={code}>
                                        {Object.hasOwn(CONTEXT_WARNING_KEYS, code)
                                            ? t('settingsBasic.' + CONTEXT_WARNING_KEYS[code], null, code)
                                            : t('settingsProbe.unknownWarning', { code }, `收到未识别的上下文提示（${code}）`)}
                                    </li>
                                ))}
                            </ul>
                        </div>
                    )}
                />
            )}
        </div>
    );
}

export default function ProbePanel({ config, canWrite, reloadConfig }) {
    const t = useT();
    const channelLabelId = useId();
    const channels = config.ai?.channels || NO_CHANNELS;
    const channelIds = Object.keys(channels);
    const defaultId = config.ai?.default_channel;
    const [selectedId, setSelectedId] = useState('');
    const channelId = [selectedId, defaultId].find(id => Object.hasOwn(channels, id)) || channelIds[0] || MAIN_CHANNEL;
    const legacy = channelId === MAIN_CHANNEL;
    const baseline = channelValues((legacy ? config.openai : channels[channelId]) || {});
    const [drafts, setDrafts] = useState({});
    const draft = drafts[channelId] || {};
    const form = { ...baseline, ...draft };
    const defaultWindow = config.context_window_defaults?.[legacy ? '' : channelId];
    const useDefaultWindow = defaultWindow > 0 && !form.context_window && !Object.hasOwn(draft, 'context_window')
        && ['base_url', 'model', 'context_profile'].every(field => form[field] === baseline[field]);
    const dirty = Object.keys(draft).length > 0;
    const [busy, setBusy] = useState('');
    const [state, setState] = useState(EMPTY_PROBE);
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState(null);
    const [applyPending, setApplyPending] = useState(false);
    const locked = !canWrite || saving;
    const channelOptions = legacy
        ? [{ value: MAIN_CHANNEL, label: t('settingsProbe.mainChannel', null, '主模型通道') }]
        : modelChannelOptions(Object.entries(channels));
    const providerOptions = PROVIDERS.map(({ value, key, fallback }) => ({ value, label: t(key, null, fallback) }));
    if (!providerOptions.some(option => option.value === form.provider)) {
        providerOptions.push({ value: form.provider, label: form.provider });
    }

    const clearDraft = () => {
        setDrafts(current => {
            const next = { ...current };
            delete next[channelId];
            return next;
        });
    };
    const update = (key, value) => {
        setDrafts(current => {
            const next = { ...current[channelId] };
            if (value === String(baseline[key])) delete next[key];
            else next[key] = value;
            return { ...current, [channelId]: next };
        });
    };
    const selectChannel = id => {
        setSelectedId(id);
        setState(EMPTY_PROBE);
        if (!applyPending) setSaveError(null);
    };

    const discardDraft = () => {
        clearDraft();
        setState(EMPTY_PROBE);
        if (!applyPending) setSaveError(null);
    };

    const apply = async message => {
        try {
            await api.post('/config/apply');
            setApplyPending(false);
            toastSuccess(message);
        } catch (error) {
            setApplyPending(true);
            setSaveError(error);
        }
    };

    const save = async (makeDefault = false) => {
        if (locked || busy || (!dirty && !makeDefault)) return;
        setSaveError(null);
        const invalidLimit = TOKEN_LIMITS.find(([key]) => !/^\d*$/.test(String(form[key]).trim())
            || !Number.isSafeInteger(Number(form[key])));
        if (invalidLimit) {
            const [, key, fallback] = invalidLimit;
            setSaveError(new Error(t('settingsProbe.invalidLimit', { field: t(key, null, fallback) }, '请输入非负整数')));
            return;
        }
        const changes = { ...draft };
        for (const [key] of TOKEN_LIMITS) {
            if (Object.hasOwn(changes, key)) changes[key] = numberValue(form[key]);
        }

        setSaving(true);
        try {
            await api.put('/config', { ai_channel: { channel_id: legacy ? '' : channelId, make_default: makeDefault, ...changes } });
            clearDraft();
            await apply(makeDefault
                ? t('settingsBasic.aiChannelDefaultSaved', null, '已设为默认通道')
                : t('settingsBasic.aiChannelSaved', null, '通道已保存'));
            await reloadConfig({ silent: true });
        } catch (error) {
            setSaveError(error);
        } finally {
            setSaving(false);
        }
    };

    const retryApply = async () => {
        if (locked || !applyPending) return;
        setSaving(true);
        setSaveError(null);
        try {
            await apply(t('settings.apply.applySuccess', null, '配置已成功应用'));
        } finally {
            setSaving(false);
        }
    };

    const run = async (key, path, body) => {
        if (locked || busy) return;
        setBusy(key);
        setState(EMPTY_PROBE);
        try {
            const payload = await api.post(path, body);
            const detectedWindow = payload.context_window;
            const fillWindow = key === 'context' || (key === 'test' && ['', '0'].includes(form.context_window.trim()));
            if (fillWindow && payload.success && Number.isSafeInteger(detectedWindow) && detectedWindow > 0
                && !payload.context_runtime_unknown && !(useDefaultWindow && detectedWindow === defaultWindow)) {
                const value = String(detectedWindow);
                setDrafts(current => {
                    const next = { ...current[channelId] };
                    // The response may arrive after the user has changed the model or window.
                    if (['provider', 'base_url', 'api_key', 'model', 'context_window'].some(field => next[field] !== draft[field])) {
                        return current;
                    }
                    if (value === baseline.context_window) delete next.context_window;
                    else next.context_window = value;
                    return { ...current, [channelId]: next };
                });
            }
            setState({ key, payload, error: null });
        } catch (error) {
            setState({ key, payload: null, error });
        } finally {
            setBusy('');
        }
    };

    const probeBody = () => ({
        provider: form.provider,
        channel_id: legacy ? undefined : channelId,
        credential_scope: legacy ? 'openai' : undefined,
        base_url: form.base_url,
        api_key: form.api_key,
        model: form.model,
        headers: form.headers,
        max_total_tokens: numberValue(form.max_total_tokens),
        max_completion_tokens: numberValue(form.max_completion_tokens),
        context_profile: form.context_profile
    });

    // 视觉模型：vision 段优先，空字段回退 openai 段；密钥为 ******** 时由服务端恢复。
    const vision = config.vision || {};
    const [visionState, setVisionState] = useState({ key: '', payload: null, error: null });
    const [visionBusy, setVisionBusy] = useState(false);
    const testVision = async () => {
        if (!canWrite) return;
        setVisionBusy(true);
        setVisionState({ key: 'vision', payload: null, error: null });
        try {
            const payload = await api.post('/config/test-vision', {
                channel_id: channelId === MAIN_CHANNEL ? undefined : channelId,
                vision,
                openai: config.openai || {}
            });
            setVisionState({ key: 'vision', payload, error: null });
        } catch (error) {
            setVisionState({ key: 'vision', payload: null, error });
        } finally {
            setVisionBusy(false);
        }
    };

    // TypeSafe 审批引擎：默认取 hitl.audit_model，密钥留空则以 hitlAudit 作用域沿用已保存值。
    const auditModel = config.hitl?.audit_model || {};
    const [typeSafe, setTypeSafe] = useState({
        base_url: auditModel.base_url || '',
        api_key: auditModel.api_key || '',
        model: auditModel.model || ''
    });
    const [typeSafeState, setTypeSafeState] = useState({ key: '', payload: null, error: null });
    const [typeSafeBusy, setTypeSafeBusy] = useState(false);
    const testTypeSafe = async () => {
        if (!canWrite) return;
        setTypeSafeBusy(true);
        setTypeSafeState({ key: 'typesafe', payload: null, error: null });
        try {
            const payload = await api.post('/config/test-typesafe', {
                base_url: typeSafe.base_url,
                api_key: typeSafe.api_key,
                model: typeSafe.model,
                credential_scope: 'hitlAudit'
            });
            setTypeSafeState({ key: 'typesafe', payload, error: null });
        } catch (error) {
            setTypeSafeState({ key: 'typesafe', payload: null, error });
        } finally {
            setTypeSafeBusy(false);
        }
    };

    return (
        <SectionCard
            title={t('settingsProbe.channelTitle', null, '模型通道与连接测试')}
            description={t('settingsProbe.channelHelp', null, '保存后应用所选通道的设置。显示 ******** 时沿用已保存的密钥。')}
            className="settings-channel-panel"
            extra={canWrite ? (
                <Space wrap>
                    {dirty && (
                        <Button disabled={saving || !!busy} onClick={discardDraft}>
                            {t('toolGuard.reset', null, '撤销修改')}
                        </Button>
                    )}
                    {!legacy && channelId !== defaultId && (
                        <Button disabled={saving || !!busy} onClick={() => save(true)}>
                            {t('settingsBasic.aiChannelSetDefault', null, '设为默认')}
                        </Button>
                    )}
                    <Button theme="solid" type="primary" loading={saving} disabled={!dirty || !!busy} onClick={() => save()}>
                        {t('settingsProbe.saveChannel', null, '保存通道')}
                    </Button>
                </Space>
            ) : (
                <Tag color="grey" size="small">{t('settingsProbe.channelReadOnly', null, '只读')}</Tag>
            )}
        >
            <div className="settings-probe-form">
                <div className="settings-field">
                    <Space spacing={6}>
                        <Text id={channelLabelId} type="tertiary" size="small">{t('settings.checkChannel', null, '通道')}</Text>
                        {!legacy && channelId === defaultId && (
                            <Tag color="blue" size="small">{t('settingsBasic.aiChannelDefaultBadge', null, '默认')}</Tag>
                        )}
                    </Space>
                    <Select
                        value={channelId}
                        onChange={selectChannel}
                        optionList={channelOptions}
                        renderSelectedItem={selectedModelChannelName}
                        aria-labelledby={channelLabelId}
                        disabled={saving || !!busy}
                        style={{ width: '100%' }}
                    />
                </div>
                <label className="settings-field">
                    <Text type="tertiary" size="small">{t('settingsBasic.apiProvider', null, 'API 提供商')}</Text>
                    <Select
                        value={form.provider}
                        onChange={value => update('provider', value)}
                        optionList={providerOptions}
                        disabled={locked}
                        style={{ width: '100%' }}
                    />
                </label>
                <label className="settings-field settings-field-wide">
                    <Text type="tertiary" size="small">{t('settingsBasic.baseUrl', null, 'API 地址')}</Text>
                    <Input value={form.base_url} onChange={value => update('base_url', value)} readOnly={locked} placeholder="https://api.openai.com/v1" />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">{t('settingsBasic.model', null, '模型')}</Text>
                    <Input value={form.model} onChange={value => update('model', value)} readOnly={locked} placeholder="gpt-4o-mini" />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">{t('settingsBasic.aiChannelName', null, '通道名称')}</Text>
                    <Input value={form.name} onChange={value => update('name', value)} readOnly={locked} placeholder={legacy ? '' : channelId} />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">{t('settingsBasic.apiKey', null, 'API 密钥')}</Text>
                    <Input mode="password" autoComplete="new-password" value={form.api_key} onChange={value => update('api_key', value)} readOnly={locked} />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">{t('settingsProbe.profile', null, '上下文配置')}</Text>
                    <Select
                        value={form.context_profile || ''}
                        onChange={value => update('context_profile', value)}
                        disabled={locked}
                        optionList={[...new Set([...CONTEXT_PROFILES, form.context_profile || ''])].map(value => ({
                            value, label: profileLabel(t, value)
                        }))}
                        style={{ width: '100%' }}
                    />
                </label>
                {TOKEN_LIMITS.map(([field, key, fallback]) => {
                    const isWindow = field === 'context_window';
                    const showDefault = isWindow && useDefaultWindow;
                    const labelId = `${channelLabelId}-${field}`;
                    return (
                        <label className="settings-field" key={field}>
                            <Space spacing={6}>
                                <Text id={labelId} type="tertiary" size="small">{t(key, null, fallback)}</Text>
                                {showDefault && <Tag color="blue" size="small">{t('settingsProbe.windowDefault', null, '默认值')}</Tag>}
                            </Space>
                            <Input
                                aria-labelledby={labelId}
                                value={String(showDefault ? defaultWindow : form[field])}
                                onChange={value => update(field, value)}
                                inputMode="numeric"
                                readOnly={locked}
                                placeholder={isWindow
                                    ? t('settingsProbe.windowExample', { value: t('settingsBasic.contextWindowPlaceholder', null, '131072') }, '例如 131072')
                                    : '0'}
                            />
                        </label>
                    );
                })}
            </div>

            {(applyPending || saveError) && (
                <div className="settings-channel-notices">
                    {applyPending && (
                        <Banner type="warning" bordered closeIcon={null} description={(
                            <Space wrap>
                                {t('settingsProbe.savedApplyFailed', null, '通道已写入配置，应用失败。')}
                                <Button size="small" disabled={locked || !!busy} onClick={retryApply}>
                                    {t('settingsProbe.retryApply', null, '重试应用')}
                                </Button>
                            </Space>
                        )} />
                    )}
                    <ErrorBanner error={saveError} />
                </div>
            )}

            <Space wrap className="settings-probe-actions">
                <Button
                    loading={busy === 'test'}
                    disabled={locked || !!busy}
                    onClick={() => run('test', '/config/test-openai', probeBody())}
                >
                    {t('settingsBasic.test', null, '测试连接')}
                </Button>
                <Button
                    loading={busy === 'context'}
                    disabled={locked || !!busy}
                    onClick={() => run('context', '/config/detect-context', probeBody())}
                >
                    {t('settingsBasic.detectContext', null, '探测上下文窗口')}
                </Button>
                <Button
                    loading={busy === 'list'}
                    disabled={locked || !!busy}
                    onClick={() => run('list', '/config/list-models', {
                        provider: form.provider,
                        channel_id: legacy ? undefined : channelId,
                        credential_scope: legacy ? 'openai' : undefined,
                        base_url: form.base_url,
                        api_key: form.api_key
                    })}
                >
                    {t('settingsBasic.modelsList', null, '获取模型列表')}
                </Button>
            </Space>
            <ProbeOutput state={state} defaultWindow={useDefaultWindow ? defaultWindow : 0} />

            <div className="settings-probe-secondary">
                <div className="settings-probe-card">
                    <Text strong>{t('settings.visionTestTitle', null, '视觉模型连接测试')}</Text>
                    <Hint>
                        {t('settingsProbe.visionHint', { model: vision.model || '—' }, `当前视觉模型 ${vision.model || '—'}，未填写密钥时使用主模型配置。`)}
                    </Hint>
                    <Space>
                        <Button size="small" loading={visionBusy} disabled={!canWrite} onClick={testVision}>
                            {t('settingsBasic.testVision', null, '测试视觉模型')}
                        </Button>
                    </Space>
                    <ProbeOutput state={visionState} />
                </div>
                <div className="settings-probe-card">
                    <Text strong>{t('settings.typeSafeTestTitle', null, 'TypeSafe / Jev 审批引擎测试')}</Text>
                    <Hint>
                        {t('settingsProbe.auditHint', null, '审计模型使用独立的 API 密钥。可以填写新密钥，也可以沿用已保存的密钥。')}
                    </Hint>
                    <div className="settings-probe-inline">
                        {[
                            ['base_url', 'baseUrl', 'API 地址', 'https://api.typesafe.ai'],
                            ['api_key', 'apiKey', 'API 密钥', '********'],
                            ['model', 'model', '模型', 'jev-latest']
                        ].map(([field, key, fallback, placeholder]) => (
                            <label key={field} className="settings-field">
                                <Text type="tertiary" size="small">{t('settingsBasic.' + key, null, fallback)}</Text>
                                <Input value={typeSafe[field]} onChange={value => setTypeSafe(current => ({ ...current, [field]: value }))} placeholder={placeholder} />
                            </label>
                        ))}
                    </div>
                    <Space>
                        <Button size="small" loading={typeSafeBusy} disabled={!canWrite} onClick={testTypeSafe}>
                            {t('settingsProbe.testAuditModel', null, '测试审计模型')}
                        </Button>
                    </Space>
                    <ProbeOutput state={typeSafeState} />
                </div>
            </div>
        </SectionCard>
    );
}
