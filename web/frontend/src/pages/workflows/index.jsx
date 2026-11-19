/** 工作流：列表、统计、新建/编辑/删除、详情抽屉、运行历史与包导入导出。 */
import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Input, Space, Switch, Tabs, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api, downloadFile } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useDebounced, useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard, StatGrid, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import WorkflowFormModal from './workflow-form';
import WorkflowDetailDrawer from './workflow-detail';
import RunsPanel from './runs-panel';
import PackageImportModal from './package-import';
import { graphSummary, errText, errorProp } from './helpers';
import './page.css';

const { Text } = Typography;

async function loadWorkflows() {
    const [list, pending] = await Promise.all([
        api.get('/workflows', { includeDisabled: 'true' }),
        api.get('/workflows/runs/pending').catch(() => null)
    ]);
    return {
        workflows: Array.isArray(list?.workflows) ? list.workflows : [],
        pendingRuns: Array.isArray(pending?.runs) ? pending.runs : []
    };
}

export default function WorkflowsPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('workflow:write');
    const canDelete = hasPermission('workflow:delete');
    const canExecute = hasPermission('workflow:execute');

    const [params, setParams] = useSearchParams();
    const selectedId = params.get('id') || '';
    const tab = params.get('tab') || 'list';

    const [search, setSearch] = useState('');
    const debounced = useDebounced(search, 250);
    const [includeDisabled, setIncludeDisabled] = useState(true);
    const [formState, setFormState] = useState({ visible: false, workflow: null });
    const [importVisible, setImportVisible] = useState(false);
    const [rev, setRev] = useState(0);

    const overview = useResource(loadWorkflows, [rev]);
    const workflows = overview.data?.workflows || [];
    const pendingRuns = overview.data?.pendingRuns || [];

    const navigate = useCallback((nextId, nextTab) => {
        const next = new URLSearchParams(params);
        if (nextId) next.set('id', nextId);
        else next.delete('id');
        if (nextTab) next.set('tab', nextTab);
        else next.delete('tab');
        if (nextTab && nextTab !== 'runs') next.delete('run');
        setParams(next, { replace: Boolean(nextId) });
    }, [params, setParams]);

    const refresh = () => setRev(value => value + 1);

    const filtered = useMemo(() => {
        const keyword = debounced.trim().toLowerCase();
        return workflows.filter(item => {
            if (!includeDisabled && item.enabled === false) return false;
            if (!keyword) return true;
            return [item.id, item.name, item.description].join(' ').toLowerCase().includes(keyword);
        });
    }, [workflows, debounced, includeDisabled]);

    const invalidCount = workflows.filter(item => Boolean(item.validation_error)).length;
    const enabledCount = workflows.filter(item => item.enabled).length;

    const metrics = [
        { id: 'total', label: t('workflows.processLibrary', null, '工作流列表'), value: workflows.length, loading: overview.loading },
        { id: 'enabled', label: t('workflows.statusEnabled', null, '启用'), value: enabledCount, tone: 'success', loading: overview.loading },
        {
            id: 'invalid',
            label: t('workflows.statusInvalid', null, '校验未通过'),
            value: invalidCount,
            tone: invalidCount ? 'danger' : undefined,
            loading: overview.loading
        },
        {
            id: 'pending',
            label: t('workflows.runsPending', null, '待审批运行'),
            value: pendingRuns.length,
            tone: pendingRuns.length ? 'warning' : undefined,
            loading: overview.loading
        }
    ];

    const exportPackage = async (record) => {
        try {
            await downloadFile(`/workflows/${encodeURIComponent(record.id)}/package`, { filename: `${record.id}.csapkg.zip` });
            toastSuccess(t('workflows.package.export', null, '导出'));
        } catch (error) {
            toastError(errText(error, t('workflows.package.errors.exportFailed', null, '导出失败，请稍后重试。')));
        }
    };

    const removeWorkflow = (record) => confirmAction({
        title: t('workflows.deleteWorkflow', null, '删除工作流'),
        content: t('workflows.confirmDelete', { id: record.id }, `确定删除工作流 ${record.id}？`),
        okText: t('common.delete', null, '删除'),
        onOk: async () => {
            try {
                await api.del(`/workflows/${encodeURIComponent(record.id)}`);
                toastSuccess(t('workflows.deleted', null, '工作流已删除'));
                if (selectedId === record.id) navigate(undefined, tab);
                refresh();
            } catch (error) {
                toastError(errText(error, t('workflows.deleteFailed', null, '删除工作流失败')));
            }
        }
    });

    const columns = [
        {
            title: t('workflows.metaName', null, '名称'),
            dataIndex: 'name',
            render: (value, record) => {
                const summary = graphSummary(record.graph_json);
                return (
                    <button
                        type="button"
                        className="workflow-name-button"
                        onClick={() => navigate(record.id, 'list')}
                        title={record.id}
                    >
                        <span className="workflow-name-text">{value || t('workflows.untitled', null, '未命名工作流')}</span>
                        <span className="workflow-name-sub">
                            <code>{record.id}</code>
                            {' · '}
                            {summary.error ? '图解析失败' : `${summary.nodes.length} 节点 / ${summary.edges.length} 连线`}
                        </span>
                    </button>
                );
            }
        },
        {
            title: t('projects.status', null, '状态'),
            dataIndex: 'enabled',
            width: 150,
            render: (value, record) => (
                <Space spacing={4} wrap>
                    <Tag size="small" color={value ? 'green' : 'grey'}>
                        {value ? t('workflows.statusEnabled', null, '启用') : t('workflows.statusDisabled', null, '禁用')}
                    </Tag>
                    {record.validation_error
                        ? <Tag size="small" color="red">{t('workflows.statusInvalid', null, '校验未通过')}</Tag>
                        : null}
                </Space>
            )
        },
        {
            title: '版本',
            dataIndex: 'version',
            width: 80,
            render: value => <Text size="small">v{value ?? 0}</Text>
        },
        {
            title: t('projects.updated', null, '更新'),
            dataIndex: 'updated_at',
            width: 165,
            render: value => <Text type="tertiary" size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 260,
            render: (value, record) => (
                <Space spacing={2} wrap>
                    <Button size="small" theme="borderless" onClick={() => navigate(record.id, 'list')}>
                        {t('projects.details', null, '详情')}
                    </Button>
                    <Button
                        size="small"
                        theme="borderless"
                        disabled={!canWrite}
                        onClick={() => setFormState({ visible: true, workflow: record })}
                    >
                        {t('common.edit', null, '编辑')}
                    </Button>
                    <Button size="small" theme="borderless" onClick={() => exportPackage(record)}>
                        {t('workflows.package.export', null, '导出')}
                    </Button>
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => removeWorkflow(record)}>
                        {t('common.delete', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="workflow-page">
            <PageHeader
                title={t('workflows.title', null, '工作流')}
                description={t('workflows.processLibraryHint', null, '工作流定义由节点与连线组成，可被角色或对话按需触发。')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} theme="borderless" loading={overview.loading} onClick={refresh}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button disabled={!canWrite} onClick={() => setImportVisible(true)}>
                            {t('workflows.package.importLocal', null, '导入本地包')}
                        </Button>
                        <Button type="primary" icon={<IconPlus />} disabled={!canWrite} onClick={() => setFormState({ visible: true, workflow: null })}>
                            {t('workflows.newGraph', null, '新建工作流')}
                        </Button>
                    </Space>
                )}
            />

            <ErrorBanner error={errorProp(overview.error)} onRetry={refresh} />
            <StatGrid metrics={metrics} columns={4} />

            <Tabs
                type="line"
                activeKey={tab}
                onChange={key => navigate(undefined, key)}
                tabList={[
                    { itemKey: 'list', tab: t('workflows.processLibrary', null, '工作流列表') },
                    { itemKey: 'runs', tab: t('workflows.runsPending', null, '运行历史') }
                ]}
                className="workflow-tabs"
            />

            {tab === 'runs' ? (
                <RunsPanel canExecute={canExecute} />
            ) : (
                <SectionCard
                    title={t('workflows.processLibrary', null, '工作流列表')}
                    description={t('workflows.metaIdHint', null, '创建后不可修改，用于 API 与角色绑定')}
                    extra={<Text type="tertiary" size="small">{filtered.length} / {workflows.length}</Text>}
                >
                    <div className="workflow-toolbar">
                        <Input
                            prefix={<IconSearch />}
                            value={search}
                            showClear
                            placeholder={t('workflows.searchPlaceholder', null, '搜索工作流…')}
                            onChange={value => setSearch(value)}
                            style={{ width: 260 }}
                        />
                        <Space spacing={6}>
                            <Switch checked={includeDisabled} onChange={setIncludeDisabled} size="small" />
                            <Text size="small">显示已禁用</Text>
                        </Space>
                    </div>
                    <DataTable
                        columns={columns}
                        dataSource={filtered}
                        rowKey="id"
                        loading={overview.loading}
                        pagination={{ pageSize: 10 }}
                        emptyText={t('workflows.emptyList', null, '暂无工作流')}
                    />
                </SectionCard>
            )}

            <WorkflowFormModal
                visible={formState.visible}
                workflow={formState.workflow}
                onCancel={() => setFormState({ visible: false, workflow: null })}
                onSaved={saved => {
                    setFormState({ visible: false, workflow: null });
                    refresh();
                    if (saved?.id) navigate(saved.id, 'list');
                }}
            />

            <WorkflowDetailDrawer
                workflowId={selectedId}
                visible={Boolean(selectedId)}
                canWrite={canWrite}
                canDelete={canDelete}
                onClose={() => navigate(undefined, tab)}
                onEdit={workflow => {
                    if (!workflow) return;
                    navigate(undefined, tab);
                    setFormState({ visible: true, workflow });
                }}
                onDeleted={() => {
                    navigate(undefined, tab);
                    refresh();
                }}
            />

            <PackageImportModal
                visible={importVisible}
                onCancel={() => setImportVisible(false)}
                onImported={() => refresh()}
            />
        </div>
    );
}
