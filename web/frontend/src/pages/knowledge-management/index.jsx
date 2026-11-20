import { useCallback, useEffect, useState } from 'react';
import { Button, Form, Input, Select, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, KeyValueList, MarkdownView, PageHeader, SectionCard, StatGrid, confirmAction, toastError, toastSuccess, Drawer } from '../../components/ui';
import { formatDateTime, formatNumber } from '../../lib/format';

const { Text } = Typography;

export default function KnowledgeManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('knowledge:write');
    const [category, setCategory] = useState('');
    const [keyword, setKeyword] = useState('');
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [editor, setEditor] = useState(null);
    const [detail, setDetail] = useState(null);
    const [saving, setSaving] = useState(false);
    const [searchOpen, setSearchOpen] = useState(false);
    const [searchResult, setSearchResult] = useState(null);

    const list = useResource(
        () => api.get('/knowledge/items', { category: category || undefined, search: keyword || undefined, limit: pageSize, offset: (page - 1) * pageSize }),
        [category, keyword, page, pageSize]
    );
    const stats = useResource(() => api.get('/knowledge/stats'), []);
    const categories = useResource(() => api.get('/knowledge/categories'), []);
    const indexStatus = useResource(() => api.get('/knowledge/index-status'), []);
    const disabled = list.data?.enabled === false;

    const items = Array.isArray(list.data?.items) ? list.data.items : [];
    const total = Number(list.data?.total || 0);
    const categoryOptions = (Array.isArray(categories.data?.categories) ? categories.data.categories : [])
        .map(item => (typeof item === 'string' ? { value: item, label: item } : { value: item.name || item.category, label: item.name || item.category }));

    const refreshAll = useCallback(() => {
        list.reload();
        stats.reload();
        indexStatus.reload();
    }, [list, stats, indexStatus]);

    const save = async () => {
        if (saving || !editor?.form) return;
        const values = await editor.form.validate().catch(() => null);
        if (!values) return;
        setSaving(true);
        try {
            const payload = { category: values.category.trim(), title: values.title.trim(), content: values.content };
            if (editor.item?.id) await api.put(`/knowledge/items/${encodeURIComponent(editor.item.id)}`, payload);
            else await api.post('/knowledge/items', payload);
            toastSuccess(editor.item?.id ? '知识项已更新' : '知识项已创建');
            setEditor(null);
            refreshAll();
        } catch (error) {
            toastError(error, '保存失败');
        } finally {
            setSaving(false);
        }
    };

    const remove = item => confirmAction({
        title: `删除知识项「${item.title}」？`,
        content: '删除后需要重新索引，且不可恢复。',
        onOk: async () => {
            await api.del(`/knowledge/items/${encodeURIComponent(item.id)}`);
            toastSuccess('已删除');
            refreshAll();
        }
    });

    const buildIndex = async full => {
        try {
            await api.post('/knowledge/index', full ? { full: true } : {});
            toastSuccess('索引任务已提交');
            indexStatus.reload();
        } catch (error) {
            toastError(error, '构建索引失败');
        }
    };

    const runSearch = async query => {
        try {
            setSearchResult(await api.post('/knowledge/search', { query, topK: 5 }));
        } catch (error) {
            toastError(error, '检索失败');
        }
    };

    const columns = [
        { title: '标题', dataIndex: 'title', render: (value, record) => <Text strong>{value || record.filePath}</Text> },
        { title: '分类', dataIndex: 'category', width: 160, render: value => (value ? <Tag size="small">{value}</Tag> : '—') },
        { title: '更新时间', dataIndex: 'updatedAt', width: 180, render: value => formatDateTime(value) },
        {
            title: '操作', width: 150, render: (_, record) => (
                <Space>
                    <Button size="small" theme="borderless" onClick={() => setDetail(record)}>查看</Button>
                    {canWrite && <Button size="small" theme="borderless" icon={<IconEdit />} onClick={() => setEditor({ item: record, form: {} })} />}
                    {canWrite && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => remove(record)} />}
                </Space>
            )
        }
    ];

    return (
        <div>
            <PageHeader
                title={t('knowledge.title', null, '知识管理')}
                description="知识条目会参与智能体检索；修改后需要重建索引才能生效。"
                extra={(
                    <Space wrap>
                        <Button icon={<IconRefresh />} onClick={refreshAll}>刷新</Button>
                        {canWrite && !disabled && <Button onClick={() => buildIndex(false)}>构建索引</Button>}
                        {canWrite && !disabled && <Button onClick={() => buildIndex(true)}>全量重建</Button>}
                        {canWrite && !disabled && <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => setEditor({ item: null, form: {} })}>新增知识</Button>}
                    </Space>
                )}
            />
            {disabled && <Typography.Paragraph type="tertiary">{list.data.message}</Typography.Paragraph>}
            <StatGrid
                columns={4}
                metrics={[
                    { id: 'items', label: '知识条目', value: formatNumber(stats.data?.total_items || 0), loading: stats.loading },
                    { id: 'categories', label: '分类数', value: formatNumber(stats.data?.total_categories || 0), loading: stats.loading },
                    { id: 'indexed', label: '已索引', value: formatNumber(indexStatus.data?.indexed_items ?? indexStatus.data?.total_indexed ?? 0), loading: indexStatus.loading },
                    { id: 'pending', label: '待索引', value: formatNumber(indexStatus.data?.pending_items ?? 0), loading: indexStatus.loading }
                ]}
            />
            <SectionCard title="知识条目">
                <ErrorBanner error={list.error} onRetry={() => list.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={items}
                    rowKey="id"
                    loading={list.loading}
                    pagination={{
                        currentPage: page,
                        pageSize,
                        total,
                        onPageChange: setPage,
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                    toolbar={(
                        <Space wrap>
                            <Input prefix={<IconSearch />} value={keyword} onChange={value => { setKeyword(value); setPage(1); }} placeholder="搜索标题或内容" style={{ width: 220 }} />
                            <Select
                                value={category}
                                onChange={value => { setCategory(value || ''); setPage(1); }}
                                placeholder="全部分类"
                                style={{ width: 180 }}
                                showClear
                                optionList={categoryOptions}
                            />
                            <Button disabled={disabled} onClick={() => setSearchOpen(true)}>检索测试</Button>
                        </Space>
                    )}
                />
            </SectionCard>

            <Drawer
                title={editor?.item ? '编辑知识项' : '新增知识项'}
                visible={Boolean(editor)}
                key={editor?.item?.id || 'new'}
                onCancel={() => setEditor(null)}
                onOk={save}
                confirmLoading={saving}
                width={760}
            >
                <Form
                    getFormApi={form => setEditor(current => (current ? { ...current, form } : current))}
                    initValues={editor?.item ? { category: editor.item.category, title: editor.item.title, content: editor.item.content } : {}}
                >
                    <Form.Input field="category" label="分类" rules={[{ required: true, message: '请输入分类' }]} />
                    <Form.Input field="title" label="标题" rules={[{ required: true, message: '请输入标题' }]} />
                    <Form.TextArea field="content" label="内容（Markdown）" rules={[{ required: true, message: '请输入内容' }]} autosize={{ minRows: 10, maxRows: 24 }} />
                </Form>
            </Drawer>

            <Drawer title={detail?.title || '知识项'} visible={Boolean(detail)} onCancel={() => setDetail(null)} width={640}>
                {detail && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                        <KeyValueList items={[
                            { label: '分类', value: detail.category },
                            { label: '文件路径', value: detail.filePath },
                            { label: '创建时间', value: formatDateTime(detail.createdAt) },
                            { label: '更新时间', value: formatDateTime(detail.updatedAt) }
                        ]} />
                        <MarkdownView content={detail.content || '（内容需要打开原文查看）'} />
                    </div>
                )}
            </Drawer>

            <Drawer title="知识检索测试" visible={searchOpen} onCancel={() => { setSearchOpen(false); setSearchResult(null); }} footer={null} width={720}>
                <Form onSubmit={({ query }) => runSearch(query)}>
                    <Form.Input field="query" label="检索语句" rules={[{ required: true, message: '请输入检索语句' }]} />
                    <Button htmlType="submit" theme="solid" type="primary">检索</Button>
                </Form>
                {searchResult && (
                    <div style={{ marginTop: 16, display: 'flex', flexDirection: 'column', gap: 8 }}>
                        {(searchResult.results || []).map((hit, index) => (
                            <div key={hit.id || index} style={{ border: '1px solid var(--semi-color-border)', borderRadius: 8, padding: 12 }}>
                                <Space>
                                    <Tag color="blue">#{index + 1}</Tag>
                                    <Text strong>{hit.item?.title || hit.title || hit.id}</Text>
                                    {hit.score !== undefined && <Text type="tertiary">相似度 {Number(hit.score).toFixed(3)}</Text>}
                                </Space>
                                <div style={{ marginTop: 8 }}>
                                    <MarkdownView content={String(hit.content || hit.item?.content || '').slice(0, 600)} />
                                </div>
                            </div>
                        ))}
                        {(searchResult.results || []).length === 0 && <Text type="tertiary">没有命中结果</Text>}
                    </div>
                )}
            </Drawer>
        </div>
    );
}
