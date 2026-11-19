/**
 * WebShell 文件管理：目录浏览、读取/编辑/下载、新建、重命名与删除。
 * 文件操作全部经 /api/webshell/file 代理到目标 Shell 执行。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Input, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { Drawer, DataTable, ErrorBanner, JsonViewer, confirmAction, toastError, toastSuccess, toastWarning } from '../../components/ui';
import { fileOp } from './api';
import { joinPath, normalizePath, parentPath, parseListing, pathSegments } from './fileList';

const { Text } = Typography;

function saveAsFile(name, content) {
    const blob = new Blob([content], { type: 'application/octet-stream' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = name || 'download.bin';
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export default function FilesTab({ connection, canWrite }) {
    const t = useT();
    const [path, setPath] = useState('.');
    const [pathInput, setPathInput] = useState('.');
    const [raw, setRaw] = useState('');
    const [items, setItems] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [filter, setFilter] = useState('');
    const [editor, setEditor] = useState({ visible: false, mode: 'read', path: '', content: '', loading: false });
    const [prompt, setPrompt] = useState({ visible: false, mode: 'mkdir', title: '', value: '', target: '' });

    const load = useCallback(async (nextPath) => {
        if (!connection?.id) return;
        const target = normalizePath(nextPath ?? path);
        setLoading(true);
        setError(null);
        try {
            const result = await fileOp({ connection, action: 'list', path: target });
            if (!result?.ok && result?.error) {
                setError(result.error);
                setItems([]);
                setRaw(result.output || '');
                return;
            }
            setRaw(result?.output || '');
            setItems(parseListing(result?.output || ''));
            setPath(target);
            setPathInput(target);
        } catch (err) {
            setError(err);
            setItems([]);
        } finally {
            setLoading(false);
        }
    }, [connection, path]);

    useEffect(() => {
        setPath('.');
        setPathInput('.');
        setItems([]);
        setRaw('');
        load('.');
    }, [connection?.id]); // eslint-disable-line react-hooks/exhaustive-deps

    const readFile = async targetPath => {
        try {
            const result = await fileOp({ connection, action: 'read', path: targetPath });
            if (result?.error && !result?.ok) {
                toastError(result.error);
                return null;
            }
            return result?.output ?? '';
        } catch (err) {
            toastError(err);
            return null;
        }
    };

    const openEditor = async (targetPath, mode) => {
        setEditor({ visible: true, mode, path: targetPath, content: '', loading: true });
        const content = await readFile(targetPath);
        setEditor(current => ({ ...current, content: content ?? '', loading: false }));
    };

    const saveEditor = async () => {
        try {
            const result = await fileOp({ connection, action: 'write', path: editor.path, content: editor.content });
            if (result?.error && !result?.ok) {
                toastError(result.error);
                return;
            }
            toastSuccess(t('webshell.saveFile', null, '已保存'));
            setEditor(current => ({ ...current, visible: false }));
            load(path);
        } catch (err) {
            toastError(err);
        }
    };

    const download = async (targetPath, name) => {
        const content = await readFile(targetPath);
        if (content === null) return;
        saveAsFile(name, content);
    };

    const remove = targetPath => {
        confirmAction({
            title: t('webshell.deleteFile', null, '删除文件'),
            content: targetPath,
            onOk: async () => {
                try {
                    const result = await fileOp({ connection, action: 'delete', path: targetPath });
                    if (result?.error && !result?.ok) {
                        toastError(result.error);
                        return;
                    }
                    toastSuccess(t('webshell.deleteFile', null, '已删除'));
                    load(path);
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const submitPrompt = async () => {
        const value = String(prompt.value || '').trim();
        if (!value) {
            toastWarning(t('webshell.filePath', null, '请填写路径'));
            return;
        }
        try {
            if (prompt.mode === 'mkdir') {
                const result = await fileOp({ connection, action: 'mkdir', path: joinPath(path, value) });
                if (result?.error && !result?.ok) {
                    toastError(result.error);
                    return;
                }
                toastSuccess(t('webshell.newDir', null, '目录已创建'));
            } else if (prompt.mode === 'create') {
                const result = await fileOp({ connection, action: 'write', path: joinPath(path, value), content: '' });
                if (result?.error && !result?.ok) {
                    toastError(result.error);
                    return;
                }
                toastSuccess(t('webshell.newFile', null, '文件已创建'));
            } else if (prompt.mode === 'rename') {
                const result = await fileOp({
                    connection,
                    action: 'rename',
                    path: prompt.target,
                    targetPath: joinPath(parentPath(prompt.target), value)
                });
                if (result?.error && !result?.ok) {
                    toastError(result.error);
                    return;
                }
                toastSuccess(t('webshell.rename', null, '已重命名'));
            }
            setPrompt(current => ({ ...current, visible: false }));
            load(path);
        } catch (err) {
            toastError(err);
        }
    };

    const visibleItems = useMemo(() => {
        const keyword = filter.trim().toLowerCase();
        if (!keyword) return items;
        return items.filter(item => item.name.toLowerCase().includes(keyword));
    }, [items, filter]);

    const breadcrumb = useMemo(() => pathSegments(path), [path]);

    const columns = [
        {
            title: t('webshell.colName', null, '名称'),
            dataIndex: 'name',
            render: (name, item) => (
                <button
                    type="button"
                    className={`webshell-file-name ${item.isDir ? 'is-dir' : 'is-file'}`}
                    onClick={() => (item.isDir ? load(joinPath(path, name)) : openEditor(joinPath(path, name), 'read'))}
                    title={name}
                >
                    {name}{item.isDir ? '/' : ''}
                </button>
            )
        },
        { title: t('webshell.colType', null, '类型'), dataIndex: 'type', width: 90 },
        { title: t('webshell.colSize', null, '大小'), dataIndex: 'size', width: 110 },
        { title: t('webshell.colModifiedAt', null, '修改时间'), dataIndex: 'mtime', width: 170 },
        { title: t('webshell.colOwner', null, '所有者'), dataIndex: 'owner', width: 120 },
        { title: t('webshell.colPerms', null, '权限'), dataIndex: 'mode', width: 130 },
        {
            title: t('webshell.moreActions', null, '操作'),
            dataIndex: 'ops',
            width: 240,
            render: (_, item) => {
                const target = joinPath(path, item.name);
                return (
                    <Space spacing={4} wrap>
                        {item.isDir ? (
                            <Button size="small" theme="borderless" onClick={() => load(target)}>{t('webshell.listDir', null, '打开')}</Button>
                        ) : (
                            <>
                                <Button size="small" theme="borderless" onClick={() => openEditor(target, 'read')}>{t('webshell.readFile', null, '读取')}</Button>
                                <Button size="small" theme="borderless" onClick={() => openEditor(target, 'write')}>{t('webshell.editFile', null, '编辑')}</Button>
                                <Button size="small" theme="borderless" onClick={() => download(target, item.name)}>{t('webshell.downloadFile', null, '下载')}</Button>
                            </>
                        )}
                        <Button
                            size="small"
                            theme="borderless"
                            disabled={!canWrite}
                            onClick={() => setPrompt({ visible: true, mode: 'rename', title: t('webshell.rename', null, '重命名'), value: item.name, target })}
                        >
                            {t('webshell.rename', null, '重命名')}
                        </Button>
                        {!item.isDir && (
                            <Button size="small" theme="borderless" type="danger" disabled={!canWrite} onClick={() => remove(target)}>
                                {t('webshell.deleteFile', null, '删除')}
                            </Button>
                        )}
                    </Space>
                );
            }
        }
    ];

    return (
        <div className="webshell-files">
            <div className="webshell-file-toolbar">
                <Space spacing={8} wrap>
                    <Button size="small" onClick={() => load(parentPath(path))}>{t('webshell.parentDir', null, '上级目录')}</Button>
                    <Button size="small" icon={<IconRefresh />} onClick={() => load(path)} loading={loading}>{t('webshell.refresh', null, '刷新')}</Button>
                    <Button
                        size="small"
                        disabled={!canWrite}
                        onClick={() => setPrompt({ visible: true, mode: 'mkdir', title: t('webshell.newDir', null, '新建目录'), value: '', target: '' })}
                    >
                        {t('webshell.newDir', null, '新建目录')}
                    </Button>
                    <Button
                        size="small"
                        disabled={!canWrite}
                        onClick={() => setPrompt({ visible: true, mode: 'create', title: t('webshell.newFile', null, '新建文件'), value: '', target: '' })}
                    >
                        {t('webshell.newFile', null, '新建文件')}
                    </Button>
                </Space>
            </div>

            <div className="webshell-file-pathbar">
                <Text type="tertiary">{t('webshell.filePath', null, '当前路径')}</Text>
                <Input
                    value={pathInput}
                    onChange={setPathInput}
                    onEnterPress={() => load(pathInput)}
                    size="small"
                    className="webshell-file-path-input"
                />
                <Button size="small" icon={<IconSearch />} onClick={() => load(pathInput)}>{t('webshell.listDir', null, '列出目录')}</Button>
                <Input
                    value={filter}
                    onChange={setFilter}
                    placeholder={t('webshell.filterPlaceholder', null, '过滤文件名')}
                    size="small"
                    className="webshell-file-filter"
                />
            </div>

            <nav className="webshell-breadcrumb" aria-label="breadcrumb">
                {breadcrumb.map((segment, index) => (
                    <span key={segment.path} className="webshell-breadcrumb-item">
                        {index > 0 && <span className="webshell-breadcrumb-sep">/</span>}
                        <button type="button" onClick={() => load(segment.path)} title={segment.path}>
                            {index === 0 ? t('webshell.breadcrumbHome', null, '根') : segment.label}
                        </button>
                    </span>
                ))}
            </nav>

            <ErrorBanner error={error} onRetry={() => load(path)} />

            {items.length === 0 && raw.trim() && !loading ? (
                <JsonViewer title={t('webshell.filePath', null, '原始输出')} value={raw} />
            ) : (
                <DataTable
                    columns={columns}
                    dataSource={visibleItems}
                    rowKey="name"
                    loading={loading}
                    pagination={{ pageSize: 20 }}
                    emptyText={t('webshell.emptyDir', null, '目录为空')}
                />
            )}

            <Drawer
                visible={editor.visible}
                title={editor.mode === 'write' ? t('webshell.editFile', null, '编辑文件') : t('webshell.readFile', null, '读取文件')}
                width={760}
                onCancel={() => setEditor(current => ({ ...current, visible: false }))}
                footer={
                    <Space>
                        <Button onClick={() => setEditor(current => ({ ...current, visible: false }))}>{t('webshell.cancelEdit', null, '关闭')}</Button>
                        {editor.mode === 'write' && (
                            <Button theme="solid" type="primary" disabled={!canWrite} onClick={saveEditor}>{t('webshell.saveFile', null, '保存')}</Button>
                        )}
                    </Space>
                }
            >
                <div className="webshell-editor-path">
                    <Tag size="small">{editor.path}</Tag>
                </div>
                <TextArea
                    value={editor.content}
                    onChange={content => setEditor(current => ({ ...current, content }))}
                    autosize={{ minRows: 12, maxRows: 24 }}
                    disabled={editor.mode !== 'write'}
                    className="webshell-editor-area"
                />
            </Drawer>

            <Drawer
                visible={prompt.visible}
                title={prompt.title}
                width={420}
                onCancel={() => setPrompt(current => ({ ...current, visible: false }))}
                onOk={submitPrompt}
            >
                <Input
                    value={prompt.value}
                    onChange={value => setPrompt(current => ({ ...current, value }))}
                    placeholder={prompt.mode === 'rename' ? t('webshell.rename', null, '新名称') : t('webshell.filePath', null, '名称或路径')}
                />
            </Drawer>
        </div>
    );
}
