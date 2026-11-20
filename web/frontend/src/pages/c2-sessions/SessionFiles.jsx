/**
 * C2 会话文件浏览：通过 ls / download 任务读取远端目录与文件。
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Input, Space, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { DataTable, ErrorBanner, toastError, toastSuccess, toastWarning } from '../../components/ui';
import * as c2 from './components/c2-api';
import { joinRemotePath, normalizeRemotePath, parseC2Listing, remoteParentPath } from './components/c2-ls';

const { Text } = Typography;

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

async function waitTask(taskId) {
    let delay = 600;
    for (let attempt = 0; attempt < 40; attempt += 1) {
        const payload = await c2.getTask(taskId);
        const task = payload?.task;
        if (!task) return null;
        if (['success', 'failed', 'cancelled'].includes(task.status)) return task;
        await sleep(delay);
        delay = Math.min(Math.round(delay * 1.4), 4000);
    }
    return null;
}

export default function SessionFiles({ session, canWrite }) {
    const t = useT();
    const [path, setPath] = useState('.');
    const [pathInput, setPathInput] = useState('.');
    const [items, setItems] = useState([]);
    const [raw, setRaw] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [filter, setFilter] = useState('');
    const alive = useRef(true);

    useEffect(() => () => { alive.current = false; }, []);

    const load = useCallback(async nextPath => {
        const target = normalizeRemotePath(nextPath ?? path);
        setLoading(true);
        setError(null);
        try {
            const created = await c2.createTask({ session_id: session.id, task_type: 'ls', payload: { path: target } });
            const taskId = created?.task?.id;
            if (!taskId) throw new Error(t('c2.files.failed', null, '列目录失败'));
            const task = await waitTask(taskId);
            if (!alive.current) return;
            if (!task) {
                setError(t('c2.files.timeout', null, '加载文件超时'));
                return;
            }
            if (task.status !== 'success') {
                setError(task.error || t('c2.files.failed', null, '列目录失败'));
                setItems([]);
                setRaw(task.resultText || '');
                return;
            }
            setRaw(task.resultText || '');
            setItems(parseC2Listing(task.resultText || ''));
            setPath(target);
            setPathInput(target);
        } catch (err) {
            if (alive.current) setError(err);
        } finally {
            if (alive.current) setLoading(false);
        }
    }, [session.id, path, t]);

    useEffect(() => {
        setPath('.');
        setPathInput('.');
        setItems([]);
        load('.');
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [session.id]);

    const download = async item => {
        if (!canWrite) {
            toastWarning(t('c2.files.writeRequired', null, '当前账号没有下发任务的权限'));
            return;
        }
        const remote = joinRemotePath(path, item.name);
        try {
            const created = await c2.createTask({ session_id: session.id, task_type: 'download', payload: { path: remote } });
            const taskId = created?.task?.id;
            if (!taskId) throw new Error(t('c2.files.failed', null, '下载失败'));
            const task = await waitTask(taskId);
            if (!task || task.status !== 'success') {
                throw new Error(task?.error || t('c2.files.failed', null, '下载失败'));
            }
            await c2.downloadTaskResult(task.id, item.name);
            toastSuccess(t('c2.files.downloadOk', null, '下载成功'));
        } catch (err) {
            toastError(err, t('c2.files.failed', null, '下载失败'));
        }
    };

    const visible = items.filter(item => {
        const keyword = filter.trim().toLowerCase();
        return !keyword || item.name.toLowerCase().includes(keyword);
    });

    const columns = [
        {
            title: t('c2.files.colName', null, '名称'),
            dataIndex: 'name',
            render: (name, item) => (
                <button
                    type="button"
                    className={`c2-file-name ${item.isDir ? 'is-dir' : ''}`}
                    onClick={() => (item.isDir ? load(joinRemotePath(path, name)) : download(item))}
                >
                    {name}{item.isDir ? '/' : ''}
                </button>
            )
        },
        { title: t('c2.files.colSize', null, '大小'), dataIndex: 'size', width: 120 },
        { title: t('c2.files.colMode', null, '权限'), dataIndex: 'mode', width: 160 },
        {
            title: t('c2.files.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 160,
            render: (_, item) => (
                item.isDir
                    ? <Button size="small" theme="borderless" onClick={() => load(joinRemotePath(path, item.name))}>{t('c2.files.open', null, '打开')}</Button>
                    : <Button size="small" theme="borderless" disabled={!canWrite} onClick={() => download(item)}>{t('c2.files.download', null, '下载')}</Button>
            )
        }
    ];

    return (
        <div className="c2-files">
            <div className="c2-files-toolbar">
                <Space spacing={8} wrap>
                    <Button size="small" onClick={() => load(remoteParentPath(path))}>{t('c2.files.parent', null, '上级目录')}</Button>
                    <Button size="small" icon={<IconRefresh />} loading={loading} onClick={() => load(path)}>{t('c2.files.refresh', null, '刷新')}</Button>
                    <Input
                        size="small"
                        value={pathInput}
                        onChange={setPathInput}
                        onEnterPress={() => load(pathInput)}
                        className="c2-files-path"
                    />
                    <Button size="small" onClick={() => load(pathInput)}>{t('c2.files.go', null, '跳转')}</Button>
                    <Input
                        size="small"
                        value={filter}
                        onChange={setFilter}
                        placeholder={t('c2.files.filter', null, '过滤名称')}
                        className="c2-files-filter"
                    />
                </Space>
            </div>
            <Text type="tertiary" size="small">{t('c2.files.currentPath', null, '当前目录')}：{path}</Text>
            <ErrorBanner error={error} onRetry={() => load(path)} />
            {items.length === 0 && raw.trim() && !loading ? (
                <pre className="c2-pre">{raw}</pre>
            ) : (
                <DataTable
                    columns={columns}
                    dataSource={visible}
                    rowKey="name"
                    loading={loading}
                    pagination={{ pageSize: 20 }}
                    emptyText={t('c2.files.emptyDir', null, '空目录')}
                />
            )}
        </div>
    );
}
