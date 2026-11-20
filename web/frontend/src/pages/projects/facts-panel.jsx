/** 事实黑板：筛选、表格与增删改/废弃恢复入口。 */
import { useMemo, useState } from 'react';
import {
    Button, Checkbox, Input, Popconfirm, Select, Space, Tag, Typography
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useDebounced, useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, SectionCard, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import {
    CATEGORY_LABEL, CONFIDENCE_META, CONFIDENCE_OPTIONS, FACT_CATEGORIES, errorProp, errText
} from './helpers';

const { Text } = Typography;

function ConfidenceTag({ value }) {
    const meta = CONFIDENCE_META[String(value || '').toLowerCase()] || { label: value || '—', color: 'grey' };
    return <Tag size="small" color={meta.color}>{meta.label}</Tag>;
}

export default function FactsPanel({
    projectId, canWrite, revision = 0, onAddFact, onEditFact, onViewFact, onMutated
}) {
    const t = useT();
    const [search, setSearch] = useState('');
    const debounced = useDebounced(search, 300);
    const [category, setCategory] = useState('all');
    const [confidence, setConfidence] = useState('all');
    const [hideDeprecated, setHideDeprecated] = useState(true);
    const [sparseOnly, setSparseOnly] = useState(false);

    const facts = useResource(() => api.get(`/projects/${projectId}/facts`, {
        limit: 200,
        include_link_counts: 'true',
        exclude_deprecated: hideDeprecated ? 'true' : undefined,
        search: debounced.trim() || undefined,
        category: category === 'all' ? undefined : category,
        confidence: confidence === 'all' ? undefined : confidence,
        sparse_only: sparseOnly ? 'true' : undefined
    }), [projectId, debounced, category, confidence, hideDeprecated, sparseOnly, revision]);

    const rows = useMemo(() => (Array.isArray(facts.data) ? facts.data : []), [facts.data]);

    const deprecate = async (record) => {
        try {
            await api.post(`/projects/${projectId}/facts/deprecate`, { fact_key: record.fact_key });
            toastSuccess(t('projects.saved', null, '已保存'));
            if (onMutated) onMutated();
            else facts.reload({ silent: true });
        } catch (error) {
            toastError(errText(error, t('projects.operationFailed', null, '操作失败')));
        }
    };

    const restore = async (record) => {
        try {
            await api.post(`/projects/${projectId}/facts/restore`, { fact_key: record.fact_key, confidence: 'tentative' });
            toastSuccess(t('projects.saved', null, '已保存'));
            if (onMutated) onMutated();
            else facts.reload({ silent: true });
        } catch (error) {
            toastError(errText(error, t('projects.operationFailed', null, '操作失败')));
        }
    };

    const remove = (record) => {
        confirmAction({
            title: t('projects.deleteForeverTitle', null, '永久删除'),
            content: t('projects.confirmDeleteFact', null, '删除该事实？'),
            okText: t('common.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/projects/${projectId}/facts/${record.id}`);
                    toastSuccess(t('common.delete', null, '删除'));
                    if (onMutated) onMutated();
                    else facts.reload({ silent: true });
                } catch (error) {
                    toastError(errText(error, t('projects.operationFailed', null, '操作失败')));
                }
            }
        });
    };

    const columns = [
        {
            title: 'fact_key',
            dataIndex: 'fact_key',
            width: 230,
            render: (value, record) => (
                <div className="project-fact-key">
                    <code className="project-fact-key-chip" title={value}>{value}</code>
                    <div className="project-fact-key-meta">
                        {record.pinned && <Tag size="small" color="blue" type="light">{t('projects.pinned', null, '置顶')}</Tag>}
                        {record.related_vulnerability_id && (
                            <Tag size="small" color="violet" type="light" title={record.related_vulnerability_id}>
                                {t('projects.relatedVulnIdTitle', null, '关联漏洞 ID')}
                            </Tag>
                        )}
                    </div>
                </div>
            )
        },
        {
            title: t('projects.category', null, '分类'),
            dataIndex: 'category',
            width: 108,
            render: value => (
                <Tag size="small" color="cyan" type="light">
                    {CATEGORY_LABEL[String(value || '').toLowerCase()] || value || '—'}
                </Tag>
            )
        },
        {
            title: t('projects.summary', null, '摘要'),
            dataIndex: 'summary',
            render: (value, record) => (
                <button type="button" className="project-cell-ellipsis project-link-button" title={value} onClick={() => onViewFact?.(record.fact_key)}>
                    {value || '—'}
                </button>
            )
        },
        {
            title: t('projects.linksColumn', null, '关系'),
            dataIndex: 'link_counts',
            width: 92,
            render: value => {
                const out = value?.outgoing || 0;
                const inc = value?.incoming || 0;
                if (!out && !inc) return <Text type="tertiary">—</Text>;
                return (
                    <span className="project-link-counts" title={t('projects.linkCountsTitle', null, '出边数 / 入边数')}>
                        ↑{out} ↓{inc}
                    </span>
                );
            }
        },
        {
            title: t('projects.confidence', null, '置信度'),
            dataIndex: 'confidence',
            width: 92,
            render: value => <ConfidenceTag value={value} />
        },
        {
            title: t('projects.updated', null, '更新'),
            dataIndex: 'updated_at',
            width: 158,
            render: value => <Text size="small">{formatDateTime(value)}</Text>
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 224,
            render: (value, record) => {
                const deprecated = String(record.confidence || '').toLowerCase() === 'deprecated';
                return (
                    <Space spacing={2} wrap>
                        <Button size="small" theme="borderless" onClick={() => onViewFact?.(record.fact_key)}>
                            {t('projects.details', null, '详情')}
                        </Button>
                        <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => onEditFact?.(record.fact_key)}>
                            {t('common.edit', null, '编辑')}
                        </Button>
                        {deprecated ? (
                            <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => restore(record)}>
                                {t('projects.restore', null, '恢复')}
                            </Button>
                        ) : (
                            <Popconfirm
                                title={t('projects.confirmDeprecateFact', { factKey: record.fact_key }, `将事实 ${record.fact_key} 标记为已废弃？`)}
                                onConfirm={() => deprecate(record)}
                                disabled={!canWrite}
                            >
                                <Button
                                    size="small"
                                    theme="borderless"
                                    disabled={!canWrite}
                                    title={t('projects.deprecateTitle', null, '标记为已废弃')}
                                >
                                    {t('projects.deprecate', null, '废弃')}
                                </Button>
                            </Popconfirm>
                        )}
                        <Button size="small" theme="borderless" type="danger" disabled={!canWrite} onClick={() => remove(record)}>
                            {t('common.delete', null, '删除')}
                        </Button>
                    </Space>
                );
            }
        }
    ];

    return (
        <SectionCard
            title={t('projects.tabFacts', null, '事实黑板')}
            description={t('projects.factToolbarHint', null, '索引仅含 key 与摘要；攻击链 / POC 写在 body，Agent 通过 get_project_fact 复现')}
            extra={(
                <Space>
                    <Button
                        icon={<IconRefresh />}
                        theme="borderless"
                        loading={facts.loading}
                        onClick={() => { if (onMutated) onMutated(); else facts.reload(); }}
                    />
                    <Button type="primary" icon={<IconPlus />} disabled={!canWrite} onClick={onAddFact}>
                        {t('projects.addFact', null, '添加事实')}
                    </Button>
                </Space>
            )}
        >
            <ErrorBanner error={errorProp(facts.error)} onRetry={facts.reload} />
            <div className="project-toolbar">
                <Input
                    prefix={<IconSearch />}
                    value={search}
                    showClear
                    aria-label={t('projects.searchFactsSr', null, '搜索事实')}
                    placeholder={t('projects.searchFactsPlaceholder', null, '搜索 key、摘要、body…')}
                    onChange={value => setSearch(value)}
                    style={{ width: 260 }}
                />
                <Select
                    value={category}
                    onChange={setCategory}
                    style={{ width: 146 }}
                    optionList={[{ value: 'all', label: t('projects.all', null, '全部') }, ...FACT_CATEGORIES]}
                />
                <Select
                    value={confidence}
                    onChange={setConfidence}
                    style={{ width: 128 }}
                    optionList={[{ value: 'all', label: t('projects.all', null, '全部') }, ...CONFIDENCE_OPTIONS]}
                />
                <Checkbox checked={hideDeprecated} onChange={event => setHideDeprecated(event.target.checked)}>
                    {t('projects.hideDeprecated', null, '隐藏废弃')}
                </Checkbox>
                <Checkbox checked={sparseOnly} onChange={event => setSparseOnly(event.target.checked)}>
                    {t('projects.sparseOnly', null, '仅待补全')}
                </Checkbox>
            </div>
            <DataTable
                columns={columns}
                dataSource={rows}
                rowKey="id"
                loading={facts.loading}
                pagination={{ pageSize: 20 }}
                emptyText={search || category !== 'all' || confidence !== 'all'
                    ? t('projects.noMatchingFacts', null, '无匹配事实，请调整筛选条件')
                    : t('projects.noFacts', null, '暂无事实，点击「添加事实」或由 Agent 自动写入')}
            />
        </SectionCard>
    );
}
