/** 项目管理：项目列表（含仪表盘统计）、项目详情、深链接 ?id=。 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Empty, Input, Pagination, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useDebounced, useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, PageHeader, SectionCard, StatGrid, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import ProjectFormModal from './project-form';
import ProjectDetail from './detail';
import { errorProp, errText } from './helpers';
import './page.css';

const { Text } = Typography;
const PROJECT_PAGE_SIZE = 10;

async function loadProjects() {
    const [list, summary] = await Promise.all([
        api.get('/projects', { limit: 500 }),
        api.get('/projects/dashboard-summary', { fact_limit: 6 }).catch(() => null)
    ]);
    return {
        projects: Array.isArray(list?.projects) ? list.projects : [],
        total: list?.total ?? 0,
        summary
    };
}

export default function ProjectsPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('project:write');
    const canDelete = hasPermission('project:delete');

    const [searchParams, setSearchParams] = useSearchParams();
    const selectedId = searchParams.get('id') || '';
    const activeTab = searchParams.get('tab') || 'facts';

    const [search, setSearch] = useState('');
    const debounced = useDebounced(search, 250);
    const [statusFilter, setStatusFilter] = useState('all');
    const [listPage, setListPage] = useState(1);
    const [modal, setModal] = useState({ visible: false, project: null });
    const [detailRev, setDetailRev] = useState(0);

    const overview = useResource(loadProjects, []);
    const projects = overview.data?.projects || [];
    const summary = overview.data?.summary;

    const navigate = useCallback((id, tab) => {
        const next = new URLSearchParams(searchParams);
        if (id) next.set('id', id);
        else next.delete('id');
        if (tab) next.set('tab', tab);
        else next.delete('tab');
        setSearchParams(next, { replace: Boolean(id) });
    }, [searchParams, setSearchParams]);

    const filtered = useMemo(() => {
        const keyword = debounced.trim().toLowerCase();
        return projects.filter(item => {
            const status = item.status || 'active';
            if (statusFilter !== 'all' && status !== statusFilter) return false;
            if (!keyword) return true;
            return [item.name, item.description, item.id].join(' ').toLowerCase().includes(keyword);
        });
    }, [projects, debounced, statusFilter]);
    const currentPage = Math.min(listPage, Math.max(1, Math.ceil(filtered.length / PROJECT_PAGE_SIZE)));
    const pageProjects = filtered.slice((currentPage - 1) * PROJECT_PAGE_SIZE, currentPage * PROJECT_PAGE_SIZE);

    // 首次进入且没有指定项目时，默认选中第一个可见项目，避免右侧面板空白。
    useEffect(() => {
        if (selectedId || !filtered.length) return;
        navigate(filtered[0].id, activeTab);
    }, [selectedId, filtered, activeTab, navigate]);

    const archivedCount = useMemo(
        () => projects.filter(item => (item.status || 'active') === 'archived').length,
        [projects]
    );

    const metrics = [
        {
            id: 'active',
            label: t('projects.statusActive', null, '进行中'),
            value: summary?.totals?.active_projects ?? projects.filter(item => (item.status || 'active') === 'active').length,
            loading: overview.loading
        },
        {
            id: 'facts',
            label: t('projects.facts', null, '事实'),
            value: summary?.totals?.total_facts ?? '—',
            detail: t('projects.sharedFactBoard', null, '共享事实黑板'),
            loading: overview.loading
        },
        { id: 'total', label: t('projects.projectList', null, '项目列表'), value: overview.data?.total ?? projects.length, loading: overview.loading },
        { id: 'archived', label: t('projects.statusArchived', null, '已归档'), value: archivedCount, loading: overview.loading }
    ];

    const removeProject = (record) => {
        confirmAction({
            title: t('projects.deleteProject', null, '删除项目'),
            content: t('projects.confirmDeleteProject', null, '确定删除该项目？事实将一并删除，对话将解除绑定。'),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/projects/${record.id}`);
                    toastSuccess(t('projects.deleteProject', null, '删除项目'));
                    if (selectedId === record.id) navigate('');
                    overview.reload({ silent: true });
                    setDetailRev(value => value + 1);
                } catch (error) {
                    toastError(errText(error, t('projects.deleteFailed', null, '删除失败')));
                }
            }
        });
    };

    const toggleArchive = (record) => {
        const archived = (record.status || 'active') === 'archived';
        confirmAction({
            title: archived ? t('projects.restoreProjectActive', null, '恢复为进行中') : t('projects.archiveProject', null, '归档'),
            content: archived
                ? t('projects.confirmRestoreProjectActive', null, '恢复为 active？')
                : t('projects.confirmArchiveProject', null, '归档后默认不再出现在活跃列表，是否继续？'),
            okText: t('common.confirm', null, '确认'),
            danger: !archived,
            onOk: async () => {
                try {
                    await api.put(`/projects/${record.id}`, { status: archived ? 'active' : 'archived' });
                    toastSuccess(t('projects.saved', null, '已保存'));
                    overview.reload({ silent: true });
                    setDetailRev(value => value + 1);
                } catch (error) {
                    toastError(errText(error, t('projects.operationFailed', null, '操作失败')));
                }
            }
        });
    };

    const columns = [
        {
            title: t('projects.projectName', null, '项目名称'),
            dataIndex: 'name',
            render: (value, record) => (
                <div className="project-list-name">
                    <div className="project-list-name-row">
                        <span className={`project-list-name-text${record.id === selectedId ? ' is-active' : ''}`}>{value || record.id}</span>
                        {record.pinned ? <Tag size="small" color="blue" type="light">{t('projects.pinned', null, '置顶')}</Tag> : null}
                    </div>
                    {record.description
                        ? <Text type="tertiary" size="small" className="project-list-desc">{record.description}</Text>
                        : null}
                </div>
            )
        },
        {
            title: t('projects.status', null, '状态'),
            dataIndex: 'status',
            width: 92,
            render: value => {
                const archived = (value || 'active') === 'archived';
                return (
                    <Tag size="small" color={archived ? 'grey' : 'green'}>
                        {archived ? t('projects.statusArchived', null, '已归档') : t('projects.statusActive', null, '进行中')}
                    </Tag>
                );
            }
        },
        {
            title: t('projects.updated', null, '更新'),
            dataIndex: 'updated_at',
            width: 150,
            render: value => <Text type="tertiary" size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 176,
            render: (value, record) => (
                <Space spacing={2} onClick={event => event.stopPropagation()}>
                    <Button size="small" theme="borderless" onClick={() => setModal({ visible: true, project: record })} disabled={!canWrite}>
                        {t('common.edit', null, '编辑')}
                    </Button>
                    <Button size="small" theme="borderless" onClick={() => toggleArchive(record)} disabled={!canWrite}>
                        {(record.status || 'active') === 'archived'
                            ? t('projects.restore', null, '恢复')
                            : t('projects.archiveProject', null, '归档')}
                    </Button>
                    <Button size="small" theme="borderless" type="danger" onClick={() => removeProject(record)} disabled={!canDelete}>
                        {t('common.delete', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="project-page">
            <PageHeader
                title={t('projects.title', null, '项目管理')}
                description={t('projects.selectOrCreateHint', null, '项目用于跨对话共享「事实黑板」：目标、环境、认证等信息会在绑定项目的对话中自动注入。')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} theme="borderless" loading={overview.loading} onClick={() => overview.reload()}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button type="primary" icon={<IconPlus />} disabled={!canWrite} onClick={() => setModal({ visible: true, project: null })}>
                            {t('projects.newProjectCta', null, '+ 新建项目')}
                        </Button>
                    </Space>
                )}
            />

            <ErrorBanner error={errorProp(overview.error)} onRetry={overview.reload} />
            <StatGrid metrics={metrics} columns={4} />

            <div className={`project-layout${selectedId && activeTab === 'graph' ? ' project-layout--graph' : ''}`}>
                <SectionCard
                    title={t('projects.projectList', null, '项目列表')}
                    className="project-list-card"
                    bodyClassName="project-list-body"
                    extra={<Text type="tertiary" size="small">{filtered.length} / {projects.length}</Text>}
                >
                    <div className="project-toolbar project-toolbar--stack">
                        <Input
                            prefix={<IconSearch />}
                            value={search}
                            showClear
                            placeholder={t('projects.searchProjectsPlaceholder', null, '搜索项目…')}
                            onChange={value => { setSearch(value); setListPage(1); }}
                            style={{ width: '100%' }}
                        />
                        <Select
                            value={statusFilter}
                            onChange={value => { setStatusFilter(value); setListPage(1); }}
                            style={{ width: '100%' }}
                            optionList={[
                                { value: 'all', label: t('projects.all', null, '全部') },
                                { value: 'active', label: t('projects.statusActive', null, '进行中') },
                                { value: 'archived', label: t('projects.statusArchived', null, '已归档') }
                            ]}
                        />
                    </div>
                    <DataTable
                        columns={columns}
                        dataSource={pageProjects}
                        rowKey="id"
                        loading={overview.loading}
                        pagination={false}
                        emptyText={search || statusFilter !== 'all'
                            ? t('projects.noMatchingProjects', null, '无匹配项目')
                            : t('projects.noProjects', null, '暂无项目')}
                        onRow={record => ({
                            onClick: () => navigate(record.id, activeTab),
                            style: { cursor: 'pointer' }
                        })}
                    />
                    {filtered.length > 0 && (
                        <div className="project-list-pagination">
                            <Pagination
                                size="small"
                                currentPage={currentPage}
                                pageSize={PROJECT_PAGE_SIZE}
                                total={filtered.length}
                                onPageChange={setListPage}
                            />
                        </div>
                    )}
                </SectionCard>

                <div className="project-detail-col">
                    {selectedId ? (
                        <ProjectDetail
                            key={selectedId}
                            projectId={selectedId}
                            tab={activeTab}
                            refreshToken={detailRev}
                            onTabChange={tab => navigate(selectedId, tab)}
                            onChanged={() => overview.reload({ silent: true })}
                            onDeleted={() => {
                                navigate('');
                                overview.reload({ silent: true });
                            }}
                        />
                    ) : (
                        <>
                            <SectionCard
                                title={t('projects.selectOrCreateTitle', null, '选择或创建项目')}
                                extra={canWrite ? null : <Text type="tertiary" size="small">{t('projects.writePermissionDenied', null, '当前账号仅有只读权限，无法创建或修改项目')}</Text>}
                            >
                                <div className="project-empty-state">
                                    <Empty
                                        title={t('projects.noProjectsClickCreate', null, '暂无项目，点击下方「新建项目」')}
                                        description={t('projects.selectOrCreateHint', null, '项目用于跨对话共享「事实黑板」：目标、环境、认证等信息会在绑定项目的对话中自动注入。')}
                                        style={{ padding: 24 }}
                                    >
                                        <Button type="primary" disabled={!canWrite} onClick={() => setModal({ visible: true, project: null })}>
                                            {t('projects.createFirstProject', null, '创建第一个项目')}
                                        </Button>
                                    </Empty>
                                </div>
                            </SectionCard>

                            <SectionCard
                                title={t('projects.sharedFactBoard', null, '共享事实黑板')}
                                description={t('projects.factToolbarHint', null, '索引仅含 key 与摘要；攻击链 / POC 写在 body，Agent 通过 get_project_fact 复现')}
                            >
                                {(summary?.recent_facts || []).length ? (
                                    <ul className="project-recent-facts">
                                        {summary.recent_facts.map(fact => (
                                            <li key={fact.id}>
                                                <button
                                                    type="button"
                                                    className="project-recent-fact"
                                                    onClick={() => navigate(fact.project_id, 'facts')}
                                                >
                                                    <span className="project-recent-fact-key">
                                                        <code>{fact.fact_key}</code>
                                                        <Tag size="small" color="cyan" type="light">{fact.project_name || fact.project_id}</Tag>
                                                    </span>
                                                    <span className="project-recent-fact-summary" title={fact.summary}>{fact.summary}</span>
                                                    <span className="project-recent-fact-time">{formatDateTime(fact.updated_at)}</span>
                                                </button>
                                            </li>
                                        ))}
                                    </ul>
                                ) : (
                                    <Text type="tertiary">{t('projects.noFacts', null, '暂无事实')}</Text>
                                )}
                            </SectionCard>
                        </>
                    )}
                </div>
            </div>

            <ProjectFormModal
                visible={modal.visible}
                project={modal.project}
                onCancel={() => setModal({ visible: false, project: null })}
                onSaved={saved => {
                    const wasEditing = Boolean(modal.project);
                    setModal({ visible: false, project: null });
                    overview.reload({ silent: true });
                    setDetailRev(value => value + 1);
                    if (saved?.id && !wasEditing) navigate(saved.id, 'facts');
                }}
            />
        </div>
    );
}
