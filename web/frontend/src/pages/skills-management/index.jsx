import { useEffect, useState } from 'react';
import { Button, Form, Input, List, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useDebounced, useResource } from '../../components/hooks';
import { DataTable, ErrorBanner, KeyValueList, MarkdownView, PageHeader, SectionCard, confirmAction, toastError, toastSuccess, Drawer } from '../../components/ui';
import { formatBytes, formatDateTime } from '../../lib/format';

const { Text } = Typography;

export default function SkillsManagementPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('skills:write');
    const canDelete = hasPermission('skills:delete');
    const [search, setSearch] = useState('');
    const debounced = useDebounced(search, 300);
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [detail, setDetail] = useState(null);
    const [editor, setEditor] = useState(null);
    const [saving, setSaving] = useState(false);

    const skills = useResource(
        () => api.get('/skills', { search: debounced || undefined, limit: pageSize, offset: (page - 1) * pageSize }),
        [debounced, page, pageSize]
    );
    const rows = Array.isArray(skills.data?.skills) ? skills.data.skills : [];
    const total = Number(skills.data?.total || 0);

    const save = async () => {
        if (saving || !editor?.form) return;
        const values = await editor.form.validate().catch(() => null);
        if (!values) return;
        setSaving(true);
        try {
            await api.post('/skills', {
                name: values.name.trim(),
                description: values.description.trim(),
                content: values.content
            });
            toastSuccess('技能已创建');
            setEditor(null);
            skills.reload();
        } catch (error) {
            toastError(error, '创建技能失败');
        } finally {
            setSaving(false);
        }
    };

    const remove = row => confirmAction({
        title: `删除技能「${row.name}」？`,
        content: '该技能目录及其全部文件会被移除。',
        onOk: async () => {
            await api.del(`/skills/${encodeURIComponent(row.id || row.name)}`);
            toastSuccess('已删除');
            skills.reload();
        }
    });

    const columns = [
        {
            title: '技能', dataIndex: 'name', render: (value, record) => (
                <Space vertical spacing={0} align="start">
                    <Text strong>{value || record.id}</Text>
                    <Text type="tertiary" ellipsis={{ showTooltip: true }} style={{ maxWidth: 420 }}>{record.description}</Text>
                </Space>
            )
        },
        { title: '版本', dataIndex: 'version', width: 100, render: value => value || '—' },
        { title: '文件', dataIndex: 'file_count', width: 80, render: value => String(value ?? '—') },
        { title: '大小', dataIndex: 'file_size', width: 100, render: value => formatBytes(value) },
        { title: '更新时间', dataIndex: 'mod_time', width: 170, render: value => (value ? formatDateTime(value) : '—') },
        {
            title: '操作', width: 140, render: (_, record) => (
                <Space>
                    <Button size="small" theme="borderless" onClick={() => setDetail(record)}>详情</Button>
                    {canDelete && <Button size="small" theme="borderless" type="danger" icon={<IconDelete />} onClick={() => remove(record)} />}
                </Space>
            )
        }
    ];

    return (
        <div>
            <PageHeader
                title={t('skills.managementTitle', null, '技能管理')}
                description="技能以目录形式存放在技能根目录，SKILL.md 描述触发条件与用法。"
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => skills.reload()} loading={skills.loading}>刷新</Button>
                        {canWrite && <Button theme="solid" type="primary" icon={<IconPlus />} onClick={() => setEditor({ form: {} })}>新建技能</Button>}
                    </Space>
                )}
            />
            <SectionCard title={`技能（${total}）`}>
                <ErrorBanner error={skills.error} onRetry={() => skills.reload()} />
                <DataTable
                    columns={columns}
                    dataSource={rows}
                    rowKey={record => record.id || record.name}
                    loading={skills.loading}
                    pagination={{
                        currentPage: page,
                        pageSize,
                        total,
                        onPageChange: setPage,
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                    toolbar={<Input prefix={<IconSearch />} value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="搜索技能名称或描述" style={{ width: 280 }} />}
                />
            </SectionCard>

            <Drawer title="新建技能" visible={Boolean(editor)} key="new-skill" onCancel={() => setEditor(null)} onOk={save} confirmLoading={saving} width={720}>
                <Form getFormApi={form => setEditor(current => (current ? { ...current, form } : current))}>
                    <Form.Input field="name" label="目录名" placeholder="小写字母、数字、连字符" rules={[{ required: true, message: '请输入目录名' }, { pattern: /^[a-z0-9-]+$/, message: '只允许小写字母、数字与连字符' }]} />
                    <Form.Input field="description" label="描述" rules={[{ required: true, message: '请输入描述' }]} />
                    <Form.TextArea field="content" label="SKILL.md 正文" rules={[{ required: true, message: '请输入正文' }]} autosize={{ minRows: 10, maxRows: 24 }} />
                </Form>
            </Drawer>

            <SkillDetail skill={detail} onClose={() => setDetail(null)} canWrite={canWrite} />
        </div>
    );
}

function SkillDetail({ skill, onClose, canWrite }) {
    const [files, setFiles] = useState([]);
    const [activeFile, setActiveFile] = useState('');
    const [content, setContent] = useState('');
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        if (!skill) return;
        setFiles([]);
        setActiveFile('');
        setContent('');
        api.get(`/skills/${encodeURIComponent(skill.id || skill.name)}/files`)
            .then(payload => setFiles(Array.isArray(payload?.files) ? payload.files : []))
            .catch(() => setFiles([]));
    }, [skill]);

    const openFile = async path => {
        setLoading(true);
        try {
            const payload = await api.get(`/skills/${encodeURIComponent(skill.id || skill.name)}/file`, { path });
            setActiveFile(path);
            setContent(payload?.content || '');
        } catch (error) {
            toastError(error, '读取文件失败');
        } finally {
            setLoading(false);
        }
    };

    const saveFile = async () => {
        setSaving(true);
        try {
            await api.put(`/skills/${encodeURIComponent(skill.id || skill.name)}/file`, { path: activeFile, content });
            toastSuccess('文件已保存');
        } catch (error) {
            toastError(error, '保存文件失败');
        } finally {
            setSaving(false);
        }
    };

    const fileList = files.map(item => (typeof item === 'string' ? item : item.path || item.name));

    return (
        <Drawer title={skill?.name || '技能详情'} visible={Boolean(skill)} onCancel={onClose} width={860}>
            {skill && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                    <KeyValueList items={[
                        { label: '目录名', value: skill.id || skill.dir_name },
                        { label: '版本', value: skill.version || '—' },
                        { label: '路径', value: skill.path },
                        { label: '文件数', value: skill.file_count },
                        { label: '触发词', value: Array.isArray(skill.triggers) ? skill.triggers.join('、') : '—' },
                        { label: '标签', value: Array.isArray(skill.tags) ? skill.tags.join('、') : '—' }
                    ]} />
                    <Text type="tertiary">{skill.description}</Text>
                    <div style={{ display: 'grid', gridTemplateColumns: '220px minmax(0, 1fr)', gap: 12 }}>
                        <div style={{ border: '1px solid var(--semi-color-border)', borderRadius: 8, maxHeight: 420, overflow: 'auto' }}>
                            <List
                                dataSource={fileList}
                                renderItem={item => (
                                    <List.Item
                                        main={<Text size="small" ellipsis={{ showTooltip: true }}>{item}</Text>}
                                        onClick={() => openFile(item)}
                                        style={{ cursor: 'pointer', background: item === activeFile ? 'var(--semi-color-primary-light-default)' : undefined }}
                                    />
                                )}
                                emptyContent="暂无文件"
                            />
                        </div>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                            <Space>
                                <Text strong>{activeFile || '选择左侧文件查看内容'}</Text>
                                {canWrite && activeFile && <Button size="small" loading={saving} onClick={saveFile}>保存</Button>}
                            </Space>
                            {activeFile
                                ? <TextArea value={content} onChange={setContent} autosize={{ minRows: 20, maxRows: 30 }} disabled={!canWrite} />
                                : <MarkdownView content={skill.description || '—'} />}
                            {loading && <Text type="tertiary">读取中…</Text>}
                        </div>
                    </div>
                </div>
            )}
        </Drawer>
    );
}
