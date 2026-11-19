/**
 * 模型连接与能力探测：/config/test-openai、/config/detect-context、/config/list-models、
 * /config/test-vision、/config/test-typesafe。密钥字段来自 GET /config，服务端用 ********
 * 作为「沿用已保存凭据」的标记。
 */
import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../../lib/http';
import { useT } from '../../../lib/i18n';
import { ErrorBanner, KeyValueList, SectionCard } from '../../../components/ui';
import { Hint } from './common';

const { Text } = Typography;

const MAIN_CHANNEL = '__main__';
const NO_CHANNELS = {};
const PROVIDERS = [
    { value: 'openai_compatible', label: 'OpenAI 兼容' },
    { value: 'claude', label: 'Claude（Anthropic Messages）' }
];
const CONTEXT_PROFILES = [
    { value: '', label: '默认（未知窗口）' },
    { value: 'auto', label: 'auto（自动）' },
    { value: 'local_auto', label: 'local_auto（本地服务）' },
    { value: 'custom', label: 'custom（手动运行时参数）' }
];

function numberValue(raw) {
    const value = parseInt(raw, 10);
    return Number.isFinite(value) && value > 0 ? value : 0;
}

function channelValues(config, id) {
    if (id === MAIN_CHANNEL) {
        const main = config.openai || {};
        return {
            provider: main.provider || 'openai_compatible',
            base_url: main.base_url || '',
            api_key: main.api_key || '',
            model: main.model || '',
            context_profile: main.context_profile || '',
            max_total_tokens: main.max_total_tokens || 0,
            max_completion_tokens: main.max_completion_tokens || 0,
            headers: main.headers || undefined
        };
    }
    const channel = config.ai?.channels?.[id] || {};
    return {
        provider: channel.provider || 'openai_compatible',
        base_url: channel.base_url || '',
        api_key: channel.api_key || '',
        model: channel.model || '',
        context_profile: channel.context_profile || '',
        max_total_tokens: channel.max_total_tokens || 0,
        max_completion_tokens: channel.max_completion_tokens || 0,
        headers: channel.headers || undefined
    };
}

function summarizeContext(payload) {
    const rows = [
        { label: 'success', value: String(Boolean(payload.success)) },
        { label: 'model', value: payload.model || '—' },
        { label: 'latency_ms', value: payload.latency_ms != null ? `${payload.latency_ms} ms` : '—' },
        { label: 'context_window', value: payload.context_window ?? '—' },
        { label: 'context_backend', value: payload.context_backend || '—' },
        { label: 'context_source', value: payload.context_source || '—' },
        { label: 'context_profile', value: payload.context_profile || '（空）' },
        { label: 'effective_max_total_tokens', value: payload.effective_max_total_tokens ?? '—' }
    ];
    return rows;
}

function ProbeOutput({ state }) {
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
                    description={payload.error || t('settingsBasic.modelsListFailed', '获取模型列表失败')}
                />
            );
        }
        const models = Array.isArray(payload.models) ? payload.models : [];
        return (
            <div className="settings-probe-result">
                <Space spacing={8}>
                    <Tag color="green">success</Tag>
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
                        items={[
                            { label: 'success', value: <Tag color="green">true</Tag> },
                            { label: 'model', value: payload.model || '—' },
                            { label: 'latency_ms', value: payload.latency_ms != null ? `${payload.latency_ms} ms` : '—' }
                        ]}
                    />
                ) : (
                    <Banner type="danger" bordered closeIcon={null} description={payload.error || '连接失败'} />
                )}
            </div>
        );
    }

    if (!payload.success) {
        return <Banner type="danger" bordered closeIcon={null} description={payload.error || '连接失败'} />;
    }
    const warnings = Array.isArray(payload.context_warnings) ? payload.context_warnings : [];
    return (
        <div className="settings-probe-result">
            <KeyValueList columns={3} items={summarizeContext(payload)} />
            {warnings.length > 0 && (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    description={`context_warnings：${warnings.join('、')}`}
                />
            )}
        </div>
    );
}

export default function ProbePanel({ config, canWrite }) {
    const t = useT();
    const channels = config.ai?.channels || NO_CHANNELS;
    const [channelId, setChannelId] = useState(() => config.ai?.default_channel || MAIN_CHANNEL);
    const [form, setForm] = useState(() => channelValues(config, config.ai?.default_channel || MAIN_CHANNEL));
    const [busy, setBusy] = useState('');
    const [state, setState] = useState({ key: '', payload: null, error: null });

    useEffect(() => {
        setForm(channelValues(config, channelId));
    }, [channelId, config]);

    const channelOptions = useMemo(() => [
        { value: MAIN_CHANNEL, label: t('settings.mainChannel', null, '主模型（openai 段）') },
        ...Object.keys(channels).map(id => ({
            value: id,
            label: `${id}${id === config.ai?.default_channel ? '（默认）' : ''}`
        }))
    ], [channels, config.ai?.default_channel, t]);

    const update = (key, value) => setForm(current => ({ ...current, [key]: value }));

    const run = async (key, path, body) => {
        if (!canWrite) return;
        setBusy(key);
        setState({ key: '', payload: null, error: null });
        try {
            const payload = await api.post(path, body);
            setState({ key, payload, error: null });
        } catch (error) {
            setState({ key, payload: null, error });
        } finally {
            setBusy('');
        }
    };

    const probeBody = () => ({
        provider: form.provider,
        channel_id: channelId === MAIN_CHANNEL ? undefined : channelId,
        credential_scope: channelId === MAIN_CHANNEL ? 'openai' : undefined,
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
            title={t('settings.probeTitle', null, '模型连接与能力探测')}
            description={t('settings.probeDesc', null, '直接调用后端的探测接口，不会保存任何改动；密钥显示 ******** 时表示沿用已保存凭据。')}
            extra={canWrite ? null : (
                <Tag color="grey" size="small">{t('settings.probeReadOnly', null, '缺少 config:write，探测接口不可用')}</Tag>
            )}
        >
            <div className="settings-probe-form">
                <label className="settings-field">
                    <Text type="tertiary" size="small">{t('settings.checkChannel', null, '通道')}</Text>
                    <Select
                        value={channelId}
                        onChange={value => setChannelId(value)}
                        optionList={channelOptions}
                        style={{ width: '100%' }}
                    />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">provider</Text>
                    <Select
                        value={form.provider}
                        onChange={value => update('provider', value)}
                        optionList={PROVIDERS}
                        style={{ width: '100%' }}
                    />
                </label>
                <label className="settings-field settings-field-wide">
                    <Text type="tertiary" size="small">base_url</Text>
                    <Input value={form.base_url} onChange={value => update('base_url', value)} placeholder="https://api.openai.com/v1" />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">model</Text>
                    <Input value={form.model} onChange={value => update('model', value)} placeholder="gpt-4o-mini" />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">api_key</Text>
                    <Input value={form.api_key} onChange={value => update('api_key', value)} placeholder="********" />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">context_profile</Text>
                    <Select
                        value={form.context_profile || ''}
                        onChange={value => update('context_profile', value)}
                        optionList={CONTEXT_PROFILES}
                        style={{ width: '100%' }}
                    />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">max_total_tokens</Text>
                    <Input
                        value={String(form.max_total_tokens ?? '')}
                        onChange={value => update('max_total_tokens', value)}
                        placeholder="0"
                    />
                </label>
                <label className="settings-field">
                    <Text type="tertiary" size="small">max_completion_tokens</Text>
                    <Input
                        value={String(form.max_completion_tokens ?? '')}
                        onChange={value => update('max_completion_tokens', value)}
                        placeholder="0"
                    />
                </label>
            </div>

            <Space wrap className="settings-probe-actions">
                <Button
                    theme="solid"
                    type="primary"
                    loading={busy === 'test'}
                    disabled={!canWrite}
                    onClick={() => run('test', '/config/test-openai', probeBody())}
                >
                    {t('settingsBasic.test', null, '测试连接')}
                </Button>
                <Button
                    loading={busy === 'context'}
                    disabled={!canWrite}
                    onClick={() => run('context', '/config/detect-context', probeBody())}
                >
                    {t('settingsBasic.detectContext', null, '探测上下文窗口')}
                </Button>
                <Button
                    loading={busy === 'list'}
                    disabled={!canWrite}
                    onClick={() => run('list', '/config/list-models', {
                        provider: form.provider,
                        channel_id: channelId === MAIN_CHANNEL ? undefined : channelId,
                        credential_scope: channelId === MAIN_CHANNEL ? 'openai' : undefined,
                        base_url: form.base_url,
                        api_key: form.api_key
                    })}
                >
                    {t('settingsBasic.modelsList', null, '获取模型列表')}
                </Button>
            </Space>
            <ProbeOutput state={state} />

            <div className="settings-probe-secondary">
                <div className="settings-probe-card">
                    <Text strong>{t('settings.visionTestTitle', null, '视觉模型连接测试')}</Text>
                    <Hint>
                        {t('settings.visionTestHint', { model: vision.model || '—' }, `当前 vision.model：${vision.model || '未配置'}；api_key 为空时回退主模型。`)}
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
                        {t('settings.typeSafeTestHint', null, '读取 hitl.audit_model；Jev 不继承主模型密钥，api_key 必填或沿用已保存值。')}
                    </Hint>
                    <div className="settings-probe-inline">
                        <Input value={typeSafe.base_url} onChange={value => setTypeSafe(s => ({ ...s, base_url: value }))} placeholder="https://api.typesafe.ai" />
                        <Input value={typeSafe.api_key} onChange={value => setTypeSafe(s => ({ ...s, api_key: value }))} placeholder="********" />
                        <Input value={typeSafe.model} onChange={value => setTypeSafe(s => ({ ...s, model: value }))} placeholder="jev-latest" />
                    </div>
                    <Space>
                        <Button size="small" loading={typeSafeBusy} disabled={!canWrite} onClick={testTypeSafe}>
                            {t('settings.hitl.testAuditModel', null, '测试审计模型')}
                        </Button>
                    </Space>
                    <ProbeOutput state={typeSafeState} />
                </div>
            </div>
        </SectionCard>
    );
}
