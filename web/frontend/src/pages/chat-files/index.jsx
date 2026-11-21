/**
 * 对话附件管理：列表/分页/搜索、文件夹浏览、上传、建目录、预览、下载、导出与删除。
 */
import { useMemo, useRef, useState } from 'react';
import { Button, Input, Select, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch, IconUpload } from '@douyinfe/semi-icons';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useDebounced, useResource } from '../../components/hooks';
import {
    Drawer, PageHeader, SectionCard, DataTable, ErrorBanner,
    confirmAction, toastError, toastSuccess, toastWarning
} from '../../components/ui';
import { formatBytes, formatDateTime } from '../../lib/format';
import * as api from './api';
import PreviewDrawer from './PreviewDrawer';
import './page.css';

const { Text } = Typography;

const SOURCE_OPTIONS = [
    { value: 'all', label: '全部' },
    { value: 'upload', label: '对话附件' },
    { value: 'reduction', label: '工具输出' },
    { value: 'workspace', label: '工作目录' },
    { value: 'conversation_artifact', label: '会话产物' }
];

const SOURCE_LABEL = {
    upload: '对话附件',
    reduction: '工具输出',
    workspace: '工作目录',
    conversation_artifact: '会话产物'
};

function dirOf(relativePath) {
    const parts = String(relativePath || '').split('/');
    parts.pop();
    return parts.join('/');
}

function baseName(relativePath) {
    const parts = String(relativePath || '').split('/');
    return parts[parts.length - 1] || relativePath;
}

export default function ChatFilesPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('files:write');
    const canDelete = hasPermission('files:delete') || canWrite;
    const fileInputRef = useRef(null);

    const [mode, setMode] = useState('list');
    const [conversation, setConversation] = useState('');
    const [project, setProject] = useState('');
    const [source, setSource] = useState('all');
    const [keyword, setKeyword] = useState('');
    const [currentDir, setCurrentDir] = useState('');
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [preview, setPreview] = useState(null);
    const [mkdir, setMkdir] = useState(null);
    const [uploading, setUploading] = useState(false);
    const search = useDebounced(keyword, 300);

    const query = useMemo(() => {
        const base = {
            conversation: conversation.trim() || undefined,
            project: project.trim() || undefined,
            source: source === 'all' ? undefined : source,
            search: search.trim() || undefined
        };
        return mode === 'browse'
            ? { ...base, page: 1, pageSize: 'all' }
            : { ...base, page, pageSize };
    }, [mode, conversation, project, source, search, page, pageSize]);

    const { data, loading, error, reload } = useResource(() => api.listFiles(query), [query]);
    const files = Array.isArray(data?.files) ? data.files : [];
    const folders = Array.isArray(data?.folders) ? data.folders : [];
    const total = Number(data?.total ?? files.length);

    const browsed = useMemo(() => {
        if (mode !== 'browse') return { folders: [], files };
        return {
            folders: folders.filter(folder => dirOf(folder) === currentDir),
            files: files.filter(file => dirOf(file.relativePath) === currentDir)
        };
    }, [mode, folders, files, currentDir]);

    const breadcrumb = useMemo(() => {
        const parts = currentDir ? currentDir.split('/') : [];
        const items = [{ label: t('chatFilesPage.browseRoot', null, '文件'), path: '' }];
        parts.forEach((part, index) => items.push({ label: part, path: parts.slice(0, index + 1).join('/') }));
        return items;
    }, [currentDir, t]);

    const copyPath = async item => {
        try {
            const payload = await api.resolvePath(item.relativePath, 'file');
            await navigator.clipboard.writeText(payload?.absolutePath || item.relativePath);
            toastSuccess(t('chatFilesPage.pathCopied', null, '路径已复制，可到对话中粘贴使用'));
        } catch (err) {
            toastError(err, t('chatFilesPage.errorGeneric', null, '操作失败，请稍后重试。'));
        }
    };

    const download = async item => {
        try {
            await api.downloadFileByPath(item.relativePath, item.name);
        } catch (err) {
            toastError(err, t('chatFilesPage.errorGeneric', null, '操作失败，请稍后重试。'));
        }
    };

    const remove = item => {
        confirmAction({
            title: t('chatFilesPage.confirmDelete', null, '确定删除该文件？'),
            content: item.relativePath,
            onOk: async () => {
                try {
                    await api.deleteFile(item.relativePath);
                    toastSuccess(t('chatFilesPage.deleted', null, '已删除'));
                    reload();
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const pickFiles = () => fileInputRef.current?.click();

    const onFilesPicked = async event => {
        const list = Array.from(event.target.files || []);
        event.target.value = '';
        if (!list.length) return;
        setUploading(true);
        try {
            for (const file of list) {
                await api.uploadFile(file, { relativeDir: mode === 'browse' ? currentDir : '' });
            }
            toastSuccess(t('chatFilesPage.uploadOkHint', null, '上传成功。点击「复制路径」可复制绝对路径到剪贴板。'));
            reload();
        } catch (err) {
            toastError(err, t('chatFilesPage.errorGeneric', null, '操作失败，请稍后重试。'));
        } finally {
            setUploading(false);
        }
    };

    const submitMkdir = async () => {
        const name = String(mkdir?.name || '').trim();
        if (!name || name.includes('/') || name.includes('\\') || name === '.' || name === '..') {
            toastWarning(t('chatFilesPage.mkdirInvalidName', null, '名称无效：不能为空，且不能包含 /、\\ 或 . / ..'));
            return;
        }
        try {
            await api.makeDirectory(mode === 'browse' ? currentDir : '', name);
            toastSuccess(t('chatFilesPage.mkdirOk', null, '文件夹已创建'));
            setMkdir(null);
            reload();
        } catch (err) {
            toastError(err, t('chatFilesPage.errorGeneric', null, '操作失败，请稍后重试。'));
        }
    };

    const exportCurrent = async () => {
        try {
            await api.exportFiles({
                conversation: conversation.trim() || undefined,
                project: project.trim() || undefined,
                source: source === 'all' ? undefined : source,
                search: search.trim() || undefined
            });
            toastSuccess(t('chatFilesPage.exportStarted', null, '已开始导出当前文件'));
        } catch (err) {
            toastError(err, t('chatFilesPage.exportEmpty', null, '没有可导出的文件'));
        }
    };

    const fileColumns = [
        {
            title: t('chatFilesPage.colName', null, '文件名'),
            dataIndex: 'name',
            render: (name, item) => (
                <button type="button" className="chat-files-name" onClick={() => setPreview(item)} title={item.relativePath}>
                    {name || baseName(item.relativePath)}
                </button>
            )
        },
        {
            title: t('chatFilesPage.sourceFilter', null, '来源'),
            dataIndex: 'source',
            width: 110,
            render: value => <Tag size="small">{(SOURCE_LABEL[value] || value || '对话附件')}</Tag>
        },
        {
            title: t('chatFilesPage.colConversation', null, '会话'),
            dataIndex: 'conversationTitle',
            width: 180,
            render: (value, item) => <Text type="tertiary" size="small" ellipsis={{ showTooltip: true }}>{value || item.conversationId || '—'}</Text>
        },
        {
            title: t('chatFilesPage.colSubPath', null, '路径'),
            dataIndex: 'relativePath',
            width: 220,
            render: value => <span className="chat-files-path">{value}</span>
        },
        { title: t('chatFilesPage.colSize', null, '大小'), dataIndex: 'size', width: 100, render: value => formatBytes(value) },
        {
            title: t('chatFilesPage.colModified', null, '修改时间'),
            dataIndex: 'modifiedUnix',
            width: 170,
            render: value => formatDateTime(value ? value * 1000 : null)
        },
        {
            title: t('chatFilesPage.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 260,
            render: (_, item) => (
                <Space spacing={4} wrap>
                    <Button size="small" theme="borderless" onClick={() => setPreview(item)}>{t('chatFilesPage.preview', null, '预览')}</Button>
                    <Button size="small" theme="borderless" onClick={() => copyPath(item)}>{t('chatFilesPage.copyPath', null, '复制路径')}</Button>
                    <Button size="small" theme="borderless" onClick={() => download(item)}>{t('chatFilesPage.download', null, '下载')}</Button>
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => remove(item)}>
                        {t('chatFilesPage.delete', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    const browseColumns = [
        {
            title: t('chatFilesPage.colName', null, '文件名'),
            dataIndex: 'name',
            render: (name, item) => (item.isDir ? (
                <button type="button" className="chat-files-name is-dir" onClick={() => setCurrentDir(item.path)}>
                    {name}/
                </button>
            ) : (
                <button type="button" className="chat-files-name" onClick={() => setPreview(item)} title={item.relativePath}>
                    {name}
                </button>
            ))
        },
        { title: t('chatFilesPage.colSize', null, '大小'), dataIndex: 'size', width: 110, render: value => (value === undefined ? '—' : formatBytes(value)) },
        {
            title: t('chatFilesPage.colModified', null, '修改时间'),
            dataIndex: 'modifiedUnix',
            width: 180,
            render: value => (value ? formatDateTime(value * 1000) : '—')
        },
        {
            title: t('chatFilesPage.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 240,
            render: (_, item) => (item.isDir ? (
                <Space spacing={4}>
                    <Button size="small" theme="borderless" onClick={() => setCurrentDir(item.path)}>{t('chatFilesPage.enterFolder', null, '进入')}</Button>
                </Space>
            ) : (
                <Space spacing={4} wrap>
                    <Button size="small" theme="borderless" onClick={() => setPreview(item)}>{t('chatFilesPage.preview', null, '预览')}</Button>
                    <Button size="small" theme="borderless" onClick={() => copyPath(item)}>{t('chatFilesPage.copyPath', null, '复制路径')}</Button>
                    <Button size="small" theme="borderless" onClick={() => download(item)}>{t('chatFilesPage.download', null, '下载')}</Button>
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => remove(item)}>
                        {t('chatFilesPage.delete', null, '删除')}
                    </Button>
                </Space>
            ))
        }
    ];

    const browseRows = [
        ...browsed.folders.map(folder => ({
            rowKey: `dir:${folder}`,
            isDir: true,
            path: folder,
            name: baseName(folder)
        })),
        ...browsed.files.map(item => ({ ...item, rowKey: item.relativePath }))
    ];

    const rows = mode === 'browse' ? browseRows : files;

    return (
        <div className="chat-files-page" id="page-chat-files">
            <PageHeader
                title={t('chatFilesPage.title', null, '文件管理')}
                description={t('chatFilesPage.intro', null, '管理在对话中上传的文件，可复制路径供 AI 引用')}
                extra={[
                    <Button key="export" onClick={exportCurrent}>{t('chatFilesPage.export', null, '导出当前结果')}</Button>,
                    <Button key="mkdir" icon={<IconPlus />} disabled={!canWrite} onClick={() => setMkdir({ name: '' })}>
                        {t('chatFilesPage.newFolderButton', null, '新建文件夹')}
                    </Button>,
                    <Button key="upload" theme="solid" type="primary" icon={<IconUpload />} loading={uploading} disabled={!canWrite} onClick={pickFiles}>
                        {t('chatFilesPage.upload', null, '上传文件')}
                    </Button>,
                    <Button key="refresh" icon={<IconRefresh />} onClick={reload} loading={loading}>{t('common.refresh', null, '刷新')}</Button>
                ]}
            />

            <input ref={fileInputRef} type="file" multiple hidden onChange={onFilesPicked} />

            <ErrorBanner error={error} onRetry={reload} />

            <SectionCard title={t('chatFilesPage.filterTitle', null, '筛选')}>
                <div className="chat-files-fields">
                    <div className="chat-files-field">
                        <span className="chat-files-field-label">{t('chatFilesPage.conversationFilter', null, '会话 ID')}</span>
                        <Input
                            value={conversation}
                            onChange={value => { setConversation(value); setPage(1); }}
                            placeholder={t('chatFilesPage.conversationPlaceholder', null, '留空表示全部')}
                            style={{ width: 200 }}
                        />
                    </div>
                    <div className="chat-files-field">
                        <span className="chat-files-field-label">{t('chatFilesPage.projectFilter', null, '项目 ID')}</span>
                        <Input
                            value={project}
                            onChange={value => { setProject(value); setPage(1); }}
                            placeholder={t('chatFilesPage.projectPlaceholder', null, '留空表示全部')}
                            style={{ width: 200 }}
                        />
                    </div>
                    <div className="chat-files-field">
                        <span className="chat-files-field-label">{t('chatFilesPage.searchName', null, '文件名')}</span>
                        <Input
                            value={keyword}
                            onChange={value => { setKeyword(value); setPage(1); }}
                            prefix={<IconSearch />}
                            placeholder={t('chatFilesPage.searchNamePlaceholder', null, '筛选文件名')}
                            style={{ width: 200 }}
                        />
                    </div>
                    <div className="chat-files-field">
                        <span className="chat-files-field-label">{t('chatFilesPage.sourceFilter', null, '来源')}</span>
                        <Select
                            value={source}
                            onChange={value => { setSource(value || 'all'); setPage(1); }}
                            optionList={SOURCE_OPTIONS}
                            style={{ width: 160 }}
                        />
                    </div>
                    <div className="chat-files-field">
                        <span className="chat-files-field-label">{t('chatFilesPage.groupBy', null, '分组方式')}</span>
                        <Select
                            value={mode}
                            onChange={value => { setMode(value || 'list'); setPage(1); setCurrentDir(''); }}
                            optionList={[
                                { value: 'list', label: t('chatFilesPage.groupNone', null, '不分组（平铺）') },
                                { value: 'browse', label: t('chatFilesPage.groupByFolder', null, '按文件夹（路径浏览）') }
                            ]}
                            style={{ width: 200 }}
                        />
                    </div>
                </div>
            </SectionCard>

            {mode === 'browse' && (
                <nav className="chat-files-breadcrumb" aria-label="breadcrumb">
                    {breadcrumb.map((segment, index) => (
                        <span key={segment.path || 'root'} className="chat-files-breadcrumb-item">
                            {index > 0 && <span className="chat-files-breadcrumb-sep">/</span>}
                            <button type="button" onClick={() => setCurrentDir(segment.path)}>{segment.label}</button>
                        </span>
                    ))}
                </nav>
            )}

            <SectionCard
                title={mode === 'browse'
                    ? t('chatFilesPage.browseRoot', null, '文件')
                    : t('chatFilesPage.listTitle', null, '附件列表')}
                extra={<Text type="tertiary" size="small">{t('chatFilesPage.paginationInfo', null, '显示 {{start}}-{{end}} / {{total}} 个文件')
                    .replace('{{start}}', rows.length ? '1' : '0')
                    .replace('{{end}}', String(rows.length))
                    .replace('{{total}}', String(mode === 'browse' ? rows.length : total))}</Text>}
            >
                <DataTable
                    columns={mode === 'browse' ? browseColumns : fileColumns}
                    dataSource={rows}
                    rowKey={item => item.rowKey || item.relativePath}
                    loading={loading}
                    onRefresh={reload}
                    pagination={mode === 'browse' ? false : {
                        currentPage: page,
                        pageSize,
                        total,
                        onPageChange: next => setPage(next),
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                    emptyText={mode === 'browse'
                        ? t('chatFilesPage.folderEmpty', null, '此文件夹为空')
                        : t('chatFilesPage.empty', null, '暂无对话附件')}
                />
            </SectionCard>

            <PreviewDrawer item={preview} onClose={() => setPreview(null)} />

            <Drawer
                visible={Boolean(mkdir)}
                title={t('chatFilesPage.newFolderTitle', null, '新建文件夹')}
                width={420}
                onCancel={() => setMkdir(null)}
                onOk={submitMkdir}
            >
                <Text type="tertiary" size="small">
                    {t('chatFilesPage.newFolderLocation', null, '位置')}：{mode === 'browse' && currentDir ? currentDir : '/'}
                </Text>
                <Input
                    value={mkdir?.name || ''}
                    onChange={value => setMkdir({ name: value })}
                    placeholder={t('chatFilesPage.newFolderNamePlaceholder', null, '仅名称，不含 /')}
                    style={{ marginTop: 8 }}
                />
            </Drawer>
        </div>
    );
}
