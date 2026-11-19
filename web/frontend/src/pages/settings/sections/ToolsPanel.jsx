/** 工具开关：GET /config/tools 分页列表，PUT /config（tools 段）+ POST /config/apply 保存。 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Button, Input, Select, SideSheet, Space, Switch, Tag, Tooltip, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useDebounced, useResource } from '../../../components/hooks';
import {
    DataTable, ErrorBanner, JsonViewer, SectionCard, toastError, toastSuccess
} from '../../../components/ui';
import { truncate } from '../../../lib/format';
import { LoadingBlock, Mono, PermissionBanner } from './common';

const { Text } = Typography;

export function toolKey(tool) {
    return tool.is_external && tool.external_mcp ? `${tool.external_mcp}::${tool.name}` : tool.name;
}

function schemaRows(schema) {
    const props = schema && typeof schema === 'object' ? (schema.properties || {}) : {};
    const required = Array.isArray(schema?.required) ? schema.required : [];
    return Object.entries(props).map(([name, value]) => ({
        name,
        type: value?.type || (value?.enum ? 'enum' : '—'),
        required: required.includes(name),
        description: value?.description || ''
    }));
}

export default function ToolsPanel() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('config:read') || hasPermission('mcp:read');
    const canWrite = hasPermission('config:write');

    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [searchInput, setSearchInput] = useState('');
    const search = useDebounced(searchInput, 350);
    const [filter, setFilter] = useState('all');
    const [draft, setDraft] = useState({});
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState(null);
    const [schemaTool, setSchemaTool] = useState(null);

    const deps = [page, pageSize, search, filter];
    const loader = useCallback(
        () => api.get('/config/tools', {
            page,
            page_size: pageSize,
            search: search || undefined,
            tool_filter: filter === 'on' ? 'on' : filter === 'off' ? 'off' : undefined
        }),
        deps // eslint-disable-line react-hooks/exhaustive-deps
    );
    const tools = useResource(loader, [...deps, canRead], { skip: !canRead });

    useEffect(() => { setPage(1); }, [search, filter, pageSize]);

    const rows = useMemo(() => {
        const list = Array.isArray(tools.data?.tools) ? tools.data.tools : [];
        return list.map(tool => {
            const key = toolKey(tool);
            const changed = draft[key];
            const serverEnabled = Boolean(tool.enabled);
            return {
                ...tool,
                key,
                serverEnabled,
                enabled: changed ? changed.enabled : serverEnabled,
                dirty: Boolean(changed)
            };
        });
    }, [tools.data, draft]);

    const dirtyCount = Object.keys(draft).length;

    const toggle = (tool, next) => {
        if (!canWrite) return;
        setDraft(current => {
            const copy = { ...current };
            const key = tool.key;
            if (next === tool.serverEnabled) delete copy[key];
            else {
                copy[key] = {
                    name: tool.name,
                    enabled: next,
                    is_external: Boolean(tool.is_external),
                    external_mcp: tool.external_mcp || undefined
                };
            }
            return copy;
        });
    };

    const save = async () => {
        if (!canWrite || !dirtyCount) return;
        const payloadTools = Object.values(draft).map(item => ({ ...item }));
        setSaving(true);
        setSaveError(null);
        try {
            await api.put('/config', { tools: payloadTools });
            await api.post('/config/apply');
            toastSuccess(t('settings.apply.applySuccess', null, '配置已成功应用'));
            setDraft({});
            await tools.reload();
        } catch (error) {
            setSaveError(error);
            toastError(error, t('settings.apply.applyFailed', null, '保存工具开关失败'));
        } finally {
            setSaving(false);
        }
    };

    if (!canRead) {
        return <PermissionBanner permission="config:read" label={t('settings.toolsTitle', null, '读取工具列表')} />;
    }

    const columns = [
        {
            title: t('settings.toolName', null, '工具'),
            dataIndex: 'name',
            width: 260,
            render: (name, row) => (
                <Space vertical spacing={2} align="start">
                    <Text strong>{name}</Text>
                    <Space spacing={4}>
                        {row.is_external
                            ? <Tag size="small" color="violet">{`MCP · ${row.external_mcp}`}</Tag>
                            : <Tag size="small" color="grey">{t('settings.toolInternal', null, '内置')}</Tag>}
                        {row.role_enabled === false && (
                            <Tag size="small" color="orange">{t('settings.toolRoleOff', null, '当前角色未启用')}</Tag>
                        )}
                    </Space>
                </Space>
            )
        },
        {
            title: t('settings.toolDescription', null, '说明'),
            dataIndex: 'description',
            render: value => (
                <Tooltip content={value || '—'} position="topLeft">
                    <Text type="tertiary">{truncate(value || '—', 90)}</Text>
                </Tooltip>
            )
        },
        {
            title: t('settings.toolStatus', null, '启用'),
            dataIndex: 'enabled',
            width: 160,
            render: (enabled, row) => (
                <Space spacing={6}>
                    <Switch
                        size="small"
                        checked={enabled}
                        disabled={!canWrite || saving}
                        onChange={next => toggle(row, next)}
                        aria-label={t('settings.toolStatus', null, '启用')}
                    />
                    {row.dirty && <Tag size="small" color="amber">{t('settings.unsavedShort', null, '未保存')}</Tag>}
                </Space>
            )
        },
        {
            title: t('common.actions', null, '操作'),
            width: 110,
            render: (_, row) => (
                <Button size="small" theme="borderless" onClick={() => setSchemaTool(row)}>
                    {t('settings.toolSchema', null, '参数')}
                </Button>
            )
        }
    ];

    return (
        <>
            <SectionCard
                title={t('settings.toolsTitle', null, '工具开关')}
                description={t('settings.toolsDesc', null, '开关按工具名写入 config.security.tools；外部 MCP 工具的状态写回对应 MCP 配置。保存后立即应用。')}
                extra={(
                    <Space>
                        <Button
                            onClick={() => setDraft({})}
                            disabled={!dirtyCount || saving}
                        >
                            {t('toolGuard.reset', null, '撤销修改')}
                        </Button>
                        <Button
                            theme="solid"
                            type="primary"
                            loading={saving}
                            disabled={!canWrite || !dirtyCount}
                            onClick={save}
                        >
                            {t('settings.saveTools', { count: dirtyCount }, `保存工具开关${dirtyCount ? `（${dirtyCount}）` : ''}`)}
                        </Button>
                    </Space>
                )}
            >
                <ErrorBanner error={saveError} />
                <ErrorBanner error={tools.error} onRetry={tools.reload} />
                <DataTable
                    columns={columns}
                    dataSource={rows}
                    rowKey="key"
                    loading={tools.loading}
                    emptyText={t('settings.toolsEmpty', null, '没有匹配的工具')}
                    toolbar={(
                        <Space wrap>
                            <Input
                                prefix={<IconSearch />}
                                value={searchInput}
                                onChange={value => setSearchInput(value)}
                                placeholder={t('settings.toolSearch', null, '按工具名或说明搜索')}
                                style={{ width: 240 }}
                                showClear
                            />
                            <Select
                                value={filter}
                                onChange={value => setFilter(value)}
                                style={{ width: 140 }}
                                optionList={[
                                    { value: 'all', label: t('settings.filterAll', null, '全部状态') },
                                    { value: 'on', label: t('settings.filterOn', null, '仅已启用') },
                                    { value: 'off', label: t('settings.filterOff', null, '仅已停用') }
                                ]}
                            />
                            <Tag size="small" color="blue">
                                {t(
                                    'settings.toolsTotal',
                                    { total: tools.data?.total ?? 0, enabled: tools.data?.total_enabled ?? 0 },
                                    `共 ${tools.data?.total ?? 0} 个，已启用 ${tools.data?.total_enabled ?? 0} 个`
                                )}
                            </Tag>
                            <Button icon={<IconRefresh />} theme="borderless" onClick={tools.reload} loading={tools.loading}>
                                {t('common.refresh', null, '刷新')}
                            </Button>
                        </Space>
                    )}
                    pagination={{
                        currentPage: page,
                        pageSize,
                        total: tools.data?.total || 0,
                        showSizeChanger: true,
                        pageSizeOpts: [10, 20, 50, 100],
                        onPageChange: next => setPage(next),
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                />
            </SectionCard>

            <SideSheet
                title={schemaTool ? t('settings.toolSchemaTitle', { name: schemaTool.name }, `参数定义 · ${schemaTool.name}`) : ''}
                visible={Boolean(schemaTool)}
                onCancel={() => setSchemaTool(null)}
                width={620}
                placement="right"
            >
                {schemaTool && <SchemaBody tool={schemaTool} />}
            </SideSheet>
        </>
    );
}

function SchemaBody({ tool }) {
    const t = useT();
    const [state, setState] = useState({ loading: true, error: null, schema: null });
    useEffect(() => {
        let cancelled = false;
        setState({ loading: true, error: null, schema: null });
        api.get(`/config/tools/${encodeURIComponent(tool.name)}/schema`, {
            external_mcp: tool.external_mcp || undefined
        })
            .then(payload => { if (!cancelled) setState({ loading: false, error: null, schema: payload?.input_schema || null }); })
            .catch(error => { if (!cancelled) setState({ loading: false, error, schema: null }); });
        return () => { cancelled = true; };
    }, [tool.name, tool.external_mcp]);

    if (state.loading) return <LoadingBlock rows={5} />;
    if (state.error) return <ErrorBanner error={state.error} />;
    if (!state.schema) {
        return <Text type="tertiary">{t('settings.toolNoSchema', null, '该工具没有声明参数（无参数 Schema）。')}</Text>;
    }
    const params = schemaRows(state.schema);
    return (
        <div className="settings-schema">
            {params.length > 0 && (
                <table className="settings-schema-table">
                    <thead>
                        <tr>
                            <th>{t('settings.paramName', null, '参数')}</th>
                            <th>{t('settings.paramType', null, '类型')}</th>
                            <th>{t('settings.paramRequired', null, '必填')}</th>
                            <th>{t('settings.paramDesc', null, '说明')}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {params.map(param => (
                            <tr key={param.name}>
                                <td><Mono>{param.name}</Mono></td>
                                <td>{param.type}</td>
                                <td>{param.required ? <Tag size="small" color="red">是</Tag> : <Text type="tertiary">否</Text>}</td>
                                <td>{param.description || '—'}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
            <JsonViewer value={state.schema} collapsed title={t('settings.toolSchemaJson', null, 'JSON Schema')} />
        </div>
    );
}
