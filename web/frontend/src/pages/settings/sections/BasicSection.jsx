/**
 * 基础配置：整份可编辑配置的 YAML 编辑区。
 *
 * 读取 GET /config（密钥已脱敏为 ********），保存走两步：
 *   PUT  /config       写入 config.yaml（后端按出现的字段增量更新）
 *   POST /config/apply 重新加载并重启相关服务
 * 选择 YAML 而不是逐项表单：GET /config 暴露 18 个段落，逐项表单必然漏字段。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Banner, Button, Collapse, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconTick } from '@douyinfe/semi-icons';
import { api, errorMessage } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { modelChannelName } from '../../../lib/model-channel';
import { useResource } from '../../../components/hooks';
import {
    CopyButton, ErrorBanner, JsonViewer, KeyValueList, SectionCard, confirmAction, toastError, toastSuccess, toastWarning
} from '../../../components/ui';
import { formatNumber } from '../../../lib/format';
import { dumpYaml, parseYaml } from '../lib/yaml';
import { Hint, LoadingBlock, PermissionBanner } from './common';

const { Paragraph } = Typography;

/** UpdateConfigRequest 接受的顶层段落（internal/handler/config.go）。 */
const WRITABLE_SECTIONS = [
    'proxy', 'agent_runtime', 'ai', 'openai', 'vision', 'fofa', 'zoomeye', 'quake', 'shodan',
    'mcp', 'tools', 'agent', 'hitl', 'knowledge', 'robots', 'multi_agent', 'c2', 'storage'
];

const EDITOR_HEADER = [
    '系统设置 · 原始配置（由 GET /config 生成）',
    '',
    '· 密钥字段显示为 ********：原样保存会沿用已保存的凭据，填写新值即替换。',
    '· 保存 = PUT /config（写入 config.yaml）+ POST /config/apply（重新加载）。',
    '· 只提交文本里有修改的段落，省略的段落沿用服务端配置。',
    '· storage（清理策略）不在 GET /config 里，请到「存储与更新」分区编辑。',
    '· server / auth / log / audit / monitor / database / security / external_mcp / update',
    '  不在本接口的写入范围，保存时会被忽略（这些段落请改 config.yaml 或其他页面）。'
].join('\n');

function normalizeTools(tools) {
    if (!Array.isArray(tools)) return undefined;
    return tools.map(tool => ({
        name: tool.name,
        enabled: Boolean(tool.enabled),
        is_external: Boolean(tool.is_external),
        external_mcp: tool.external_mcp || undefined
    }));
}

export default function BasicSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('config:read');
    const canWrite = hasPermission('config:write');
    const loader = useCallback(() => api.get('/config'), []);
    const config = useResource(loader, [canRead], { skip: !canRead });

    const [text, setText] = useState('');
    const [baseline, setBaseline] = useState('');
    const [parseError, setParseError] = useState('');
    const [saveError, setSaveError] = useState(null);
    const [notice, setNotice] = useState('');
    const [phase, setPhase] = useState('idle');

    useEffect(() => {
        if (!config.data) return;
        const next = dumpYaml(config.data, { header: EDITOR_HEADER });
        setText(next);
        setBaseline(next);
        setParseError('');
    }, [config.data]);

    const dirty = baseline !== '' && text !== baseline;
    const busy = phase !== 'idle';

    const summary = useMemo(() => {
        const data = config.data;
        if (!data) return [];
        const channels = data.ai?.channels || {};
        const channelNames = Object.entries(channels).map(([id, channel]) => modelChannelName(id, channel));
        const defaultId = data.ai?.default_channel;
        const tools = Array.isArray(data.tools) ? data.tools : [];
        const enabledTools = tools.filter(tool => tool.enabled).length;
        return [
            { label: t('settings.defaultChannel', null, '默认 AI 通道'), value: defaultId ? modelChannelName(defaultId, channels[defaultId]) : '—' },
            {
                label: t('settingsBasic.aiChannelSavedList', null, '已保存通道'),
                value: `${formatNumber(channelNames.length)}${channelNames.length ? `（${channelNames.slice(0, 4).join('、')}）` : ''}`
            },
            {
                label: t('settings.toolsEnabled', null, '已启用工具'),
                value: `${formatNumber(enabledTools)} / ${formatNumber(tools.length)}`
            },
            {
                label: t('settings.visionLabel', null, '视觉模型'),
                value: data.vision?.enabled ? (data.vision.model || '已启用') : '未启用'
            },
            { label: t('settings.knowledge.title', null, '知识库'), value: data.knowledge?.enabled ? '已启用' : '未启用' },
            { label: 'C2', value: data.c2?.enabled ? '已启用' : '未启用' }
        ];
    }, [config.data, t]);

    const doReload = useCallback(async () => {
        setSaveError(null);
        setNotice('');
        await config.reload();
    }, [config.reload]);

    const reload = useCallback(() => {
        if (dirty) {
            confirmAction({
                title: '重新加载配置',
                content: '当前有未保存修改，重新加载会丢弃这些修改。确认继续？',
                okText: '确定',
                onOk: doReload
            });
            return;
        }
        doReload();
    }, [doReload, dirty]);

    const formatEditor = () => {
        try {
            const value = parseYaml(text);
            if (!value || typeof value !== 'object' || Array.isArray(value)) {
                setParseError('顶层必须是键值映射（key: value），不能是列表或标量。');
                return;
            }
            setText(dumpYaml(value, { header: EDITOR_HEADER }));
            setParseError('');
        } catch (error) {
            setParseError(errorMessage(error, 'YAML 解析失败'));
        }
    };

    const save = async () => {
        let parsed;
        try {
            parsed = parseYaml(text);
        } catch (error) {
            setParseError(errorMessage(error, 'YAML 解析失败'));
            return;
        }
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            setParseError('顶层必须是键值映射（key: value），不能是列表或标量。');
            return;
        }
        setParseError('');
        setSaveError(null);
        setNotice('');

        const ignored = Object.keys(parsed).filter(key => !WRITABLE_SECTIONS.includes(key));
        const payload = {};
        for (const key of WRITABLE_SECTIONS) {
            if (parsed[key] !== undefined && JSON.stringify(parsed[key]) !== JSON.stringify(config.data?.[key])) {
                payload[key] = parsed[key];
            }
        }
        if (payload.tools) payload.tools = normalizeTools(payload.tools);
        if (!Object.keys(payload).length) {
            setNotice('没有需要保存的配置修改。');
            if (ignored.length) toastWarning(`已忽略不支持的字段：${ignored.join('、')}`);
            return;
        }

        setPhase('saving');
        try {
            await api.put('/config', payload);
        } catch (error) {
            setPhase('idle');
            setSaveError(error);
            toastError(error, t('settings.apply.applyFailed', null, '保存配置失败'));
            return;
        }

        let applyError = null;
        setPhase('applying');
        try {
            await api.post('/config/apply');
        } catch (error) {
            applyError = error;
        }
        setPhase('idle');

        if (applyError) {
            setSaveError(applyError);
            setNotice(t('settings.apply.appliedWithError', null, '配置已写入 config.yaml，但应用失败；部分改动需要重启后才能生效。'));
            toastWarning(t('settings.apply.appliedWithError', null, '配置已写入，但应用失败'));
        } else {
            toastSuccess(t('settings.apply.applySuccess', null, '配置已成功应用'));
        }
        if (ignored.length) {
            toastWarning(t('settings.apply.ignoredSections', { list: ignored.join('、') }, `已忽略后端不接收的字段：${ignored.join('、')}`));
        }
        await config.reload();
    };

    if (!canRead) {
        return <PermissionBanner permission="config:read" label={t('settings.nav.basic', null, '读取基础配置')} />;
    }
    if (config.loading && !config.data) return <LoadingBlock rows={8} />;

    return (
        <>
            <ErrorBanner error={config.error} onRetry={reload} />
            {config.data && (
                <>
                    <SectionCard title={t('settings.overviewTitle', null, '当前配置概览')}>
                        <KeyValueList items={summary} columns={3} />
                    </SectionCard>

                    <SectionCard
                        title={t('settings.rawTitle', null, '原始 YAML 配置')}
                        description={t('settingsDisplay.rawYamlHelp', null, '查看或批量修改服务端高级配置。保存会写入支持的改动并尝试应用。')}
                        extra={(
                            <Space>
                                <Tag color={dirty ? 'amber' : 'green'} size="small">
                                    {dirty ? t('settings.unsaved', null, '有未保存修改') : t('settings.savedState', null, '已与服务端同步')}
                                </Tag>
                                <CopyButton text={text} label={t('common.copy', null, '复制')} />
                            </Space>
                        )}
                    >
                        {notice && (
                            <Banner type="warning" bordered closeIcon={null} className="settings-yaml-notice" description={notice} />
                        )}
                        {parseError && <ErrorBanner error={parseError} />}
                        <ErrorBanner error={saveError} onRetry={reload} />
                        <div className="settings-yaml-toolbar">
                            <Space>
                                <Button
                                    theme="solid"
                                    type="primary"
                                    icon={<IconTick />}
                                    loading={busy}
                                    disabled={!canWrite || !dirty}
                                    onClick={save}
                                >
                                    {phase === 'applying'
                                        ? t('settings.applying', null, '正在应用…')
                                        : t('settings.saveAndApply', null, '保存并应用')}
                                </Button>
                                <Button onClick={formatEditor} disabled={busy}>
                                    {t('settings.formatAndValidate', null, '格式化并校验')}
                                </Button>
                                <Button
                                    onClick={() => { setText(baseline); setParseError(''); setNotice(''); }}
                                    disabled={!dirty || busy}
                                >
                                    {t('toolGuard.reset', null, '撤销修改')}
                                </Button>
                                <Button icon={<IconRefresh />} onClick={reload} loading={config.loading} disabled={busy}>
                                    {t('common.refresh', null, '刷新')}
                                </Button>
                            </Space>
                            <Hint>{t('settings.lineCount', { count: text.split('\n').length }, `${text.split('\n').length} 行`)}</Hint>
                        </div>
                        {!canWrite && (
                            <Paragraph type="tertiary" size="small" className="settings-hint">
                                {t('settings.readOnlyHint', null, '当前账号没有 config:write 权限，只能查看配置，无法保存。')}
                            </Paragraph>
                        )}
                        <TextArea
                            className="settings-yaml-input"
                            value={text}
                            onChange={value => { setText(value); setNotice(''); }}
                            readOnly={!canWrite || busy}
                            spellCheck={false}
                            autosize={false}
                            rows={24}
                        />
                    </SectionCard>

                    <SectionCard title={t('settings.jsonTitle', null, '只读参考')}>
                        <Collapse>
                            <Collapse.Panel
                                header={t('settings.jsonPanel', null, 'GET /config 原始响应（JSON，便于核对字段名）')}
                                itemKey="json"
                            >
                                <JsonViewer value={config.data} collapsed title="GET /config" />
                            </Collapse.Panel>
                        </Collapse>
                    </SectionCard>
                </>
            )}
        </>
    );
}
