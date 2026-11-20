/** 外部 MCP 配置：列表、启停、JSON 表单新增/编辑、删除。 */
import { useCallback, useMemo, useState } from 'react';
import { Banner, Button, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import {
    Drawer, SectionCard, ErrorBanner, CopyButton, toastSuccess, toastError, confirmAction
} from '../../components/ui';
import { useInterval, useResource } from '../../components/hooks';
import { api, errorMessage } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { connectionMeta, transportOf } from './utils';

const { Text } = Typography;

const EXAMPLE_CONFIG = {
    'my-stdio-server': {
        command: 'python3',
        args: ['${HOME}/mcp-servers/main.py', '--port', '${MCP_PORT:-3000}'],
        env: { API_KEY: '${API_KEY}', LOG_LEVEL: '${LOG_LEVEL:-INFO}' },
        timeout: 300
    },
    'my-http-server': {
        type: 'http',
        url: 'https://mcp.example.com/mcp',
        headers: { Authorization: 'Bearer ${MCP_TOKEN}' }
    },
    'my-sse-server': {
        type: 'sse',
        url: 'http://127.0.0.1:8081/mcp/sse'
    }
};

function validateConfigMap(raw, { editingName }) {
    if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) return { error: '配置必须是 JSON 对象' };
    const names = Object.keys(raw);
    if (!names.length) return { error: '至少需要一个 MCP 配置' };
    if (editingName && !raw[editingName]) return { error: `编辑模式下 JSON 必须包含配置名称 "${editingName}"` };
    for (const name of names) {
        if (!name.trim()) return { error: '配置名称不能为空' };
        const config = raw[name];
        if (config === null || typeof config !== 'object' || Array.isArray(config)) {
            return { error: `"${name}" 的配置必须是对象` };
        }
        delete config.external_mcp_enable;
        const transport = config.type || config.transport || (config.command ? 'stdio' : (config.url ? 'http' : ''));
        if (!transport) return { error: `"${name}" 需要指定 command（stdio）或 url + type（http/sse）` };
        if (transport === 'stdio' && !config.command) return { error: `"${name}" 的 stdio 模式需要 command` };
        if ((transport === 'http' || transport === 'sse') && !config.url) return { error: `"${name}" 的 ${transport} 模式需要 url` };
    }
    return { error: '', names };
}

export default function ExternalMcpPanel({ canWrite, onChanged, refreshKey = 0 }) {
    const t = useT();
    const list = useResource(() => api.get('/external-mcp'), [refreshKey]);
    const [busy, setBusy] = useState('');
    const [modalOpen, setModalOpen] = useState(false);
    const [editingName, setEditingName] = useState(null);
    const [json, setJson] = useState('');
    const [jsonError, setJsonError] = useState('');
    const [saving, setSaving] = useState(false);

    const servers = useMemo(() => {
        const raw = list.data?.servers || {};
        return Object.entries(raw).sort(([a], [b]) => a.localeCompare(b));
    }, [list.data]);

    const reload = useCallback(options => list.reload(options), [list.reload]); // eslint-disable-line react-hooks/exhaustive-deps

    useInterval(() => {
        if (typeof document !== 'undefined' && document.hidden) return;
        reload({ silent: true });
    }, 8000);

    const openAdd = useCallback(() => {
        setEditingName(null);
        setJson('');
        setJsonError('');
        setModalOpen(true);
    }, []);

    const openEdit = useCallback(async name => {
        setEditingName(name);
        setJson('');
        setJsonError('');
        setModalOpen(true);
        try {
            const payload = await api.get(`/external-mcp/${encodeURIComponent(name)}`);
            const config = { ...(payload?.config || {}) };
            delete config.tool_count;
            delete config.external_mcp_enable;
            setJson(JSON.stringify({ [name]: config }, null, 2));
        } catch (error) {
            setModalOpen(false);
            toastError(error, t('mcp.getConfigFailed', null, '获取外部MCP配置失败'));
        }
    }, [t]);

    const closeModal = useCallback(() => {
        setModalOpen(false);
        setEditingName(null);
        setJsonError('');
    }, []);

    const toggle = useCallback(async (name, status) => {
        const action = status === 'connected' ? 'stop' : 'start';
        setBusy(name);
        try {
            await api.post(`/external-mcp/${encodeURIComponent(name)}/${action}`, {});
            toastSuccess(action === 'start' ? '已提交启动请求，正在后台连接' : '外部 MCP 已停止');
            reload({ silent: true });
            onChanged?.();
        } catch (error) {
            toastError(error, t('mcp.operationFailed', null, '操作失败'));
        } finally {
            setBusy('');
        }
    }, [onChanged, reload, t]);

    const remove = useCallback(name => {
        confirmAction({
            title: t('mcp.deleteExternalConfirm', { name }, `确定要删除外部MCP "${name}" 吗？`),
            content: '删除后该 MCP 的工具将从对话中移除。',
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/external-mcp/${encodeURIComponent(name)}`);
                    toastSuccess(t('mcp.deleteSuccess', null, '删除成功'));
                    reload();
                    onChanged?.();
                } catch (error) {
                    toastError(error, t('mcp.operationFailed', null, '删除失败'));
                }
            }
        });
    }, [onChanged, reload, t]);

    const submit = useCallback(async () => {
        const text = json.trim();
        if (!text) {
            setJsonError(t('mcp.jsonEmpty', null, 'JSON不能为空'));
            return;
        }
        let parsed;
        try {
            parsed = JSON.parse(text);
        } catch (error) {
            setJsonError(`${t('mcp.jsonError', null, 'JSON格式错误')}: ${error.message}`);
            return;
        }
        const { error, names } = validateConfigMap(parsed, { editingName });
        if (error) {
            setJsonError(error);
            return;
        }
        setJsonError('');
        setSaving(true);
        try {
            const targets = editingName ? [editingName] : names;
            for (const name of targets) {
                await api.put(`/external-mcp/${encodeURIComponent(name)}`, { config: parsed[name] });
            }
            toastSuccess(t('mcp.saveSuccess', null, '保存成功'));
            closeModal();
            reload();
            onChanged?.();
        } catch (err) {
            setJsonError(`${t('mcp.operationFailed', null, '保存失败')}: ${errorMessage(err)}`);
        } finally {
            setSaving(false);
        }
    }, [closeModal, editingName, json, onChanged, reload, t]);

    const extra = (
        <Space spacing={8}>
            <Button size="small" icon={<IconRefresh />} theme="borderless" onClick={() => reload()} loading={list.loading}>
                {t('common.refresh', null, '刷新')}
            </Button>
            <Button size="small" icon={<IconPlus />} disabled={!canWrite} onClick={openAdd}>
                {t('mcp.addExternalMCP', null, '添加外部MCP')}
            </Button>
        </Space>
    );

    return (
        <SectionCard
            title={t('mcp.externalConfig', null, '外部 MCP 配置')}
            description="stdio 传 command/args/env，http 与 sse 传 url/headers；状态每 8 秒自动刷新。"
            extra={extra}
        >
            <ErrorBanner error={list.error} onRetry={() => reload()} />
            {!servers.length && !list.loading && (
                <div className="mcp-management-empty">
                    <p>{t('mcp.noExternalMCP', null, '暂无外部 MCP')}</p>
                    <Button size="small" disabled={!canWrite} onClick={openAdd}>
                        {t('mcp.clickToAddExternal', null, '点击添加外部 MCP')}
                    </Button>
                </div>
            )}
            <div className="mcp-management-mcp-grid">
                {servers.map(([name, server]) => {
                    const config = server?.config || {};
                    const status = String(server?.status || 'disconnected');
                    const meta = connectionMeta(status);
                    const transport = transportOf(config);
                    const connected = status === 'connected';
                    return (
                        <div className="mcp-management-mcp-card" key={name}>
                            <div className="mcp-management-mcp-head">
                                <div className="mcp-management-mcp-title">
                                    <Text strong>{name}</Text>
                                    <Tag size="small" color={meta.color}>{meta.label}</Tag>
                                    <Tag size="small" color="grey">{transport.toUpperCase()}</Tag>
                                    {Number(server?.tool_count) > 0 && (
                                        <Tag size="small" color="blue">{`${server.tool_count} 个工具`}</Tag>
                                    )}
                                </div>
                                <Space spacing={4} wrap>
                                    <Button
                                        size="small"
                                        theme="borderless"
                                        disabled={!canWrite || busy === name || status === 'connecting'}
                                        loading={busy === name}
                                        onClick={() => toggle(name, status)}
                                    >
                                        {status === 'connecting'
                                            ? t('mcp.connecting', null, '连接中')
                                            : (connected ? t('mcp.stop', null, '停止') : t('mcp.start', null, '启动'))}
                                    </Button>
                                    <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => openEdit(name)}>
                                        {t('common.edit', null, '编辑')}
                                    </Button>
                                    <Button size="small" type="danger" theme="borderless" disabled={!canWrite} onClick={() => remove(name)}>
                                        {t('common.delete', null, '删除')}
                                    </Button>
                                </Space>
                            </div>

                            {(status === 'error' || status === 'disconnected') && server?.error && (
                                <Banner
                                    type={status === 'error' ? 'danger' : 'warning'}
                                    className="mcp-management-mcp-error"
                                    description={<span className="mcp-management-mono">{server.error}</span>}
                                />
                            )}

                            <div className="mcp-management-mcp-meta">
                                <div>
                                    <span className="mcp-management-meta-label">{t('mcp.transportMode', null, '传输方式')}</span>
                                    <span>{transport.toUpperCase()}</span>
                                </div>
                                {config.description && (
                                    <div>
                                        <span className="mcp-management-meta-label">{t('mcp.description', null, '描述')}</span>
                                        <span>{config.description}</span>
                                    </div>
                                )}
                                {transport === 'stdio' && config.command && (
                                    <div>
                                        <span className="mcp-management-meta-label">{t('mcp.command', null, '命令')}</span>
                                        <span className="mcp-management-mono">{config.command}</span>
                                    </div>
                                )}
                                {transport !== 'stdio' && config.url && (
                                    <div>
                                        <span className="mcp-management-meta-label">URL</span>
                                        <span className="mcp-management-mono">{config.url}</span>
                                    </div>
                                )}
                                {Number(config.timeout) > 0 && (
                                    <div>
                                        <span className="mcp-management-meta-label">{t('mcp.timeout', null, '超时')}</span>
                                        <span>{config.timeout} {t('mcp.secondsUnit', null, '秒')}</span>
                                    </div>
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>

            <Drawer
                title={editingName
                    ? `${t('mcp.editExternalMCP', null, '编辑外部MCP')} · ${editingName}`
                    : t('mcp.addExternalMCP', null, '添加外部 MCP')}
                visible={modalOpen}
                onCancel={closeModal}
                onOk={submit}
                confirmLoading={saving}
                width={760}
                okText={t('common.save', null, '保存')}
                cancelText={t('common.cancel', null, '取消')}
            >
                <div className="mcp-management-form">
                    <div className="mcp-management-form-head">
                        <Text type="tertiary">
                            {editingName
                                ? '编辑模式下只提交当前名称的配置；external_mcp_enable 由启停按钮维护。'
                                : '支持一次粘贴多个 MCP：{"name": {...}}；保存后逐个写入 config.yaml。'}
                        </Text>
                        {!editingName && (
                            <Button size="small" theme="borderless" onClick={() => setJson(JSON.stringify(EXAMPLE_CONFIG, null, 2))}>
                                载入示例
                            </Button>
                        )}
                        {json && <CopyButton text={json} size="small" />}
                    </div>
                    <TextArea
                        className="mcp-management-json"
                        rows={16}
                        value={json}
                        spellCheck={false}
                        placeholder='{"my-http-server": {"type": "http", "url": "https://mcp.example.com/mcp"}}'
                        onChange={value => { setJson(value); if (jsonError) setJsonError(''); }}
                    />
                    {jsonError && <div className="mcp-management-form-error">{jsonError}</div>}
                </div>
            </Drawer>
        </SectionCard>
    );
}
