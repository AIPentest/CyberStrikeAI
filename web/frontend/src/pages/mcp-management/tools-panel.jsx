/** MCP 工具配置：内置/外部工具清单、启用状态与保存（PUT /config + POST /config/apply）。 */
import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Checkbox, Input, Select, Space, Switch, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { DataTable, ErrorBanner, SectionCard, toastError, toastSuccess } from '../../components/ui';
import { useDebounced, useResource } from '../../components/hooks';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { toolKeyOf, sourceLabel } from './utils';

const { Text } = Typography;

export default function ToolsPanel({ canWrite, refreshKey = 0 }) {
    const t = useT();
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [searchInput, setSearchInput] = useState('');
    const search = useDebounced(searchInput, 350);
    const [filter, setFilter] = useState('all');
    const [includeExternal, setIncludeExternal] = useState(true);
    const [drafts, setDrafts] = useState({});
    const [saving, setSaving] = useState(false);
    const catalogRef = useRef(new Map());

    const list = useResource(
        () => api.get('/config/tools', {
            page,
            page_size: pageSize,
            search,
            tool_filter: filter === 'all' ? '' : filter,
            include_external: includeExternal ? '' : '0'
        }),
        [page, pageSize, search, filter, includeExternal, refreshKey]
    );

    const tools = list.data?.tools || [];
    const total = Number(list.data?.total) || 0;
    const deferredTools = useDeferredValue(tools);

    useEffect(() => {
        (tools || []).forEach(tool => catalogRef.current.set(toolKeyOf(tool), tool));
    }, [tools]);

    const dirtyKeys = useMemo(() => Object.keys(drafts), [drafts]);

    const displayEnabled = useCallback(
        tool => (Object.prototype.hasOwnProperty.call(drafts, toolKeyOf(tool)) ? drafts[toolKeyOf(tool)] : tool.enabled),
        [drafts]
    );

    const setDraft = useCallback((tool, enabled) => {
        const key = toolKeyOf(tool);
        setDrafts(prev => {
            const next = { ...prev };
            if (enabled === tool.enabled) delete next[key];
            else next[key] = enabled;
            return next;
        });
    }, []);

    const setAllOnPage = useCallback((enabled) => {
        setDrafts(prev => {
            const next = { ...prev };
            (tools || []).forEach(raw => {
                const key = toolKeyOf(raw);
                if (raw.enabled === enabled) delete next[key];
                else next[key] = enabled;
            });
            return next;
        });
    }, [tools]);

    const resetDrafts = useCallback(() => setDrafts({}), []);

    const save = useCallback(async () => {
        const entries = Object.entries(drafts);
        if (!entries.length) return;
        setSaving(true);
        try {
            const payload = entries.map(([key, enabled]) => {
                const meta = catalogRef.current.get(key);
                return {
                    name: meta?.name || key.split('::').pop() || key,
                    enabled,
                    is_external: Boolean(meta?.is_external),
                    external_mcp: meta?.external_mcp || ''
                };
            });
            await api.put('/config', { tools: payload });
            await api.post('/config/apply', {});
            toastSuccess(t('mcp.toolsConfigSaved', null, '工具配置已成功保存'));
            setDrafts({});
            list.reload({ silent: true });
        } catch (error) {
            toastError(error, t('mcp.saveToolsConfigFailed', null, '保存工具配置失败'));
        } finally {
            setSaving(false);
        }
    }, [drafts, list.reload, t]); // eslint-disable-line react-hooks/exhaustive-deps

    const columns = [
        {
            title: t('mcpMonitor.columnTool', null, '工具'),
            dataIndex: 'name',
            render: value => <span className="mcp-management-mono mcp-management-tool-name">{value}</span>
        },
        {
            title: '来源',
            dataIndex: 'is_external',
            width: 180,
            render: (value, record) => (
                <Tag size="small" color={value ? 'blue' : 'grey'}>{sourceLabel(record)}</Tag>
            )
        },
        {
            title: t('mcp.filterEnabled', null, '已启用'),
            dataIndex: 'enabled',
            width: 110,
            render: (value, record) => (
                <Switch
                    size="small"
                    checked={displayEnabled(record)}
                    disabled={!canWrite}
                    onChange={checked => setDraft(record, checked)}
                />
            )
        },
        {
            title: t('mcp.description', null, '描述'),
            dataIndex: 'description',
            render: value => (
                <Text type="tertiary" ellipsis={{ showTooltip: true }} style={{ maxWidth: 560 }}>
                    {value || t('mcp.noDescription', null, '暂无描述')}
                </Text>
            )
        }
    ];

    const pagination = {
        currentPage: page,
        pageSize,
        total,
        showSizeChanger: true,
        pageSizeOpts: [20, 50, 100],
        showTotal: true,
        onPageChange: next => setPage(next),
        onPageSizeChange: size => { setPageSize(size); setPage(1); }
    };

    const filterOptions = [
        { value: 'all', label: t('mcp.filterAll', null, '全部') },
        { value: 'on', label: t('mcp.filterEnabled', null, '已启用') },
        { value: 'off', label: t('mcp.filterDisabled', null, '已停用') }
    ];

    return (
        <SectionCard
            title={t('mcp.toolConfig', null, 'MCP 工具配置')}
            description={`${t('mcp.currentPageEnabled', null, '当前页已启用')} ${(deferredTools || []).filter(item => displayEnabled(item)).length} / ${deferredTools.length} · ${t('mcp.totalEnabled', null, '已启用工具总数')} ${list.data?.total_enabled ?? '—'}`}
            extra={(
                <Space spacing={8}>
                    {dirtyKeys.length > 0 && (
                        <Tag size="small" color="amber">{`未保存 ${dirtyKeys.length} 项`}</Tag>
                    )}
                    {dirtyKeys.length > 0 && (
                        <Button size="small" theme="borderless" onClick={resetDrafts} disabled={saving}>
                            放弃修改
                        </Button>
                    )}
                    <Button size="small" icon={<IconRefresh />} theme="borderless" onClick={() => list.reload()} loading={list.loading}>
                        {t('common.refresh', null, '刷新')}
                    </Button>
                    <Button
                        size="small"
                        theme="solid"
                        disabled={!canWrite || !dirtyKeys.length}
                        loading={saving}
                        onClick={save}
                    >
                        {t('mcp.saveToolConfig', null, '保存工具配置')}
                    </Button>
                </Space>
            )}
        >
            <ErrorBanner error={list.error} onRetry={() => list.reload()} />
            <DataTable
                columns={columns}
                dataSource={tools}
                rowKey={record => toolKeyOf(record)}
                loading={list.loading}
                pagination={pagination}
                size="small"
                emptyText={t('mcp.noTools', null, '暂无工具')}
                toolbar={(
                    <Space spacing={8} wrap>
                        <Input
                            prefix={<IconSearch />}
                            showClear
                            style={{ width: 240 }}
                            value={searchInput}
                            placeholder={t('mcp.toolSearchPlaceholder', null, '搜索工具…')}
                            onChange={value => { setSearchInput(value); setPage(1); }}
                        />
                        <Select
                            value={filter}
                            style={{ width: 140 }}
                            optionList={filterOptions}
                            onChange={value => { setFilter(value); setPage(1); }}
                        />
                        <Checkbox
                            checked={includeExternal}
                            onChange={event => { setIncludeExternal(event.target.checked); setPage(1); }}
                        >
                            包含外部 MCP 工具
                        </Checkbox>
                        <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => setAllOnPage(true)}>
                            {t('mcp.selectAll', null, '全选')}
                        </Button>
                        <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => setAllOnPage(false)}>
                            {t('mcp.deselectAll', null, '全不选')}
                        </Button>
                    </Space>
                )}
            />
            <p className="mcp-management-hint">
                保存只提交本次改动的工具项；应用后由后端重新注册 MCP 工具。直接注册（未写入 config.yaml）的工具切换不会落盘。
            </p>
        </SectionCard>
    );
}
