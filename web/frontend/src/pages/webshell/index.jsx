/**
 * WebShell 管理：左侧连接列表，右侧按连接展示终端、文件、命令历史与 AI 会话。
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Empty, Input, Space, Spin, Tabs, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useDebounced, useResource } from '../../components/hooks';
import {
    PageHeader, ErrorBanner, confirmAction, toastError, toastSuccess, toastWarning
} from '../../components/ui';
import * as shellApi from './api';
import ConnectionForm from './ConnectionForm';
import FilesTab from './FilesTab';
import TerminalTab from './TerminalTab';
import HistoryTab from './HistoryTab';
import AiTab from './AiTab';
import './page.css';

const { Text } = Typography;

const encodingLabel = { auto: '自动', 'utf-8': 'UTF-8', gbk: 'GBK', gb18030: 'GB18030' };

function statusColor(state) {
    if (state === 'online') return 'green';
    if (state === 'probing') return 'amber';
    if (state === 'offline') return 'red';
    return 'grey';
}

export default function WebshellPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('webshell:write');
    const canDelete = hasPermission('webshell:delete') || canWrite;
    const [searchParams, setSearchParams] = useSearchParams();
    const selectedId = searchParams.get('id') || '';

    const [keyword, setKeyword] = useState('');
    const debouncedKeyword = useDebounced(keyword, 250);
    const [probeState, setProbeState] = useState({});
    const [probingAll, setProbingAll] = useState(false);
    const [form, setForm] = useState({ visible: false, record: null });
    const [submitting, setSubmitting] = useState(false);
    const [projects, setProjects] = useState([]);
    const [memo, setMemo] = useState('');
    const [history, setHistory] = useState([]);
    const persistTimer = useRef(null);

    const { data, loading, error, reload } = useResource(() => shellApi.listConnections(), []);
    const connections = useMemo(() => (Array.isArray(data) ? data : []), [data]);

    useEffect(() => {
        shellApi.listProjects().then(setProjects);
    }, []);

    const selected = useMemo(
        () => connections.find(item => item.id === selectedId) || null,
        [connections, selectedId]
    );

    const select = id => {
        const next = new URLSearchParams(searchParams);
        if (id) next.set('id', id);
        else next.delete('id');
        setSearchParams(next, { replace: true });
    };

    /** 状态持久化（备忘录 + 命令历史），仅在客户端改动后写回。 */
    const persist = useCallback(payload => {
        if (!canWrite || !selectedId) return;
        if (persistTimer.current) clearTimeout(persistTimer.current);
        persistTimer.current = setTimeout(() => {
            shellApi.saveConnectionState(selectedId, payload).catch(() => {});
        }, 700);
    }, [canWrite, selectedId]);

    useEffect(() => {
        let cancelled = false;
        setMemo('');
        setHistory([]);
        if (!selectedId) return () => { cancelled = true; };
        shellApi.getConnectionState(selectedId)
            .then(payload => {
                if (cancelled) return;
                const state = payload?.state || {};
                setMemo(typeof state.memo === 'string' ? state.memo : '');
                setHistory(Array.isArray(state.commands) ? state.commands : []);
            })
            .catch(() => { /* 状态读取失败不影响主流程 */ });
        return () => { cancelled = true; };
    }, [selectedId]);

    const updateMemo = value => {
        setMemo(value);
        persist({ memo: value, commands: history });
    };

    const recordCommand = command => {
        const next = [...history, { command, at: new Date().toISOString() }].slice(-200);
        setHistory(next);
        persist({ memo, commands: next });
    };

    const removeHistory = rowId => {
        const next = history.filter((item, index) => `${index}-${item.at}` !== rowId);
        setHistory(next);
        persist({ memo, commands: next });
    };

    const clearHistory = () => {
        confirmAction({
            title: t('webshell.clearHistory', null, '清空历史'),
            content: t('webshell.clearHistoryConfirm', null, '确定清空该连接的命令历史？'),
            onOk: () => {
                setHistory([]);
                persist({ memo, commands: [] });
            }
        });
    };

    const probe = async connection => {
        setProbeState(current => ({ ...current, [connection.id]: 'probing' }));
        try {
            const result = await shellApi.probeConnection(connection);
            setProbeState(current => ({ ...current, [connection.id]: result.ok ? 'online' : 'offline' }));
            return result.ok;
        } catch (err) {
            setProbeState(current => ({ ...current, [connection.id]: 'offline' }));
            return false;
        }
    };

    const probeAll = async () => {
        if (!connections.length) {
            toastWarning(t('webshell.probeNoConnections', null, '暂无可探活连接'));
            return;
        }
        setProbingAll(true);
        for (const connection of connections) {
            // 逐条探活，避免瞬间打满目标 Shell
            await probe(connection);
        }
        setProbingAll(false);
    };

    const submitForm = async values => {
        setSubmitting(true);
        try {
            if (form.record?.id) {
                await shellApi.updateConnection(form.record.id, values);
                toastSuccess(t('webshell.toastUpdated', null, '连接已更新'));
            } else {
                const created = await shellApi.createConnection(values);
                toastSuccess(t('webshell.toastCreated', null, '连接已创建'));
                if (created?.id) select(created.id);
            }
            setForm({ visible: false, record: null });
            reload();
        } catch (err) {
            toastError(err, t('webshell.saveFailed', null, '保存失败'));
        } finally {
            setSubmitting(false);
        }
    };

    const remove = connection => {
        confirmAction({
            title: t('webshell.deleteConfirm', null, '确定要删除该连接吗？'),
            content: connection.url,
            onOk: async () => {
                try {
                    await shellApi.deleteConnection(connection.id);
                    toastSuccess(t('webshell.toastDeleted', null, '连接已删除'));
                    if (connection.id === selectedId) select('');
                    reload();
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const filtered = useMemo(() => {
        const value = debouncedKeyword.trim().toLowerCase();
        if (!value) return connections;
        return connections.filter(item => [item.url, item.remark, item.type, item.id]
            .filter(Boolean)
            .some(field => String(field).toLowerCase().includes(value)));
    }, [connections, debouncedKeyword]);

    const tabs = [
        {
            tab: t('webshell.tabTerminal', null, '虚拟终端'),
            itemKey: 'terminal',
            children: selected
                ? <TerminalTab connection={selected} canWrite={canWrite} onRecordCommand={recordCommand} />
                : null
        },
        {
            tab: t('webshell.tabFileManager', null, '文件管理'),
            itemKey: 'files',
            children: selected ? <FilesTab connection={selected} canWrite={canWrite} /> : null
        },
        {
            tab: t('webshell.tabHistory', null, '命令历史'),
            itemKey: 'history',
            children: (
                <HistoryTab
                    history={history}
                    onRemove={removeHistory}
                    onClear={canWrite ? clearHistory : null}
                    onRun={command => (selected ? recordCommand(command) : null)}
                />
            )
        },
        {
            tab: t('webshell.tabAiSessions', null, 'AI 会话'),
            itemKey: 'ai',
            children: selected ? <AiTab connection={selected} /> : null
        },
        {
            tab: t('webshell.tabMemo', null, '备忘录'),
            itemKey: 'memo',
            children: (
                <TextArea
                    value={memo}
                    onChange={updateMemo}
                    autosize={{ minRows: 10, maxRows: 20 }}
                    placeholder={t('webshell.aiMemoPlaceholder', null, '记录关键命令、测试思路、复现步骤...')}
                    disabled={!canWrite}
                />
            )
        }
    ];

    return (
        <div className="webshell-page" id="page-webshell">
            <PageHeader
                title={t('webshell.title', null, 'WebShell 管理')}
                description={t('webshell.pageDesc', null, '统一管理 WebShell 连接，代理执行命令并浏览目标文件')}
                extra={[
                    <Button key="refresh" icon={<IconRefresh />} onClick={reload} loading={loading}>
                        {t('webshell.refresh', null, '刷新')}
                    </Button>,
                    <Button key="add" theme="solid" type="primary" icon={<IconPlus />} disabled={!canWrite} onClick={() => setForm({ visible: true, record: null })}>
                        {t('webshell.addConnection', null, '添加连接')}
                    </Button>
                ]}
            />

            <ErrorBanner error={error} onRetry={reload} />

            <div className="webshell-layout">
                <aside className="webshell-sidebar">
                    <div className="webshell-sidebar-head">
                        <Text strong>{t('webshell.connections', null, '连接列表')}</Text>
                        <Tag size="small">{connections.length}</Tag>
                    </div>
                    <Input
                        value={keyword}
                        onChange={setKeyword}
                        prefix={<IconSearch />}
                        placeholder={t('webshell.searchPlaceholder', null, '搜索连接...')}
                        size="small"
                    />
                    <Button size="small" theme="borderless" loading={probingAll} onClick={probeAll} disabled={!connections.length}>
                        {t('webshell.batchProbe', null, '一键批量探活')}
                    </Button>
                    <div className="webshell-conn-list">
                        {loading && !connections.length && <div className="webshell-conn-loading"><Spin /></div>}
                        {!loading && filtered.length === 0 && (
                            <Text type="tertiary" className="webshell-conn-empty">
                                {connections.length ? t('webshell.noMatchConnections', null, '暂无匹配连接') : t('webshell.noConnections', null, '暂无连接，请点击「添加连接」')}
                            </Text>
                        )}
                        {filtered.map(item => (
                            <button
                                type="button"
                                key={item.id}
                                className={`webshell-conn-item ${item.id === selectedId ? 'is-active' : ''}`}
                                onClick={() => select(item.id)}
                            >
                                <span className="webshell-conn-url" title={item.url}>{item.remark || item.url}</span>
                                <span className="webshell-conn-meta">
                                    <Tag size="small" color="blue">{String(item.type || 'php').toUpperCase()}</Tag>
                                    {probeState[item.id] && (
                                        <Tag size="small" color={statusColor(probeState[item.id])}>
                                            {probeState[item.id] === 'online'
                                                ? t('webshell.probeOnline', null, '在线')
                                                : probeState[item.id] === 'probing'
                                                    ? t('webshell.probeRunning', null, '探活中')
                                                    : t('webshell.probeOffline', null, '离线')}
                                        </Tag>
                                    )}
                                </span>
                            </button>
                        ))}
                    </div>
                </aside>

                <section className="webshell-main">
                    {!selected ? (
                        <Empty
                            title={t('webshell.selectOrAdd', null, '请从左侧选择连接，或添加新的 WebShell 连接')}
                            description={null}
                            style={{ padding: 64 }}
                        />
                    ) : (
                        <div className="webshell-workspace">
                            <div className="webshell-workspace-head">
                                <div className="webshell-workspace-title">
                                    <Text strong ellipsis={{ showTooltip: true }} className="webshell-workspace-url">{selected.url}</Text>
                                    <Space spacing={6} wrap>
                                        <Tag size="small" color="blue">{String(selected.type || 'php').toUpperCase()}</Tag>
                                        <Tag size="small">{String(selected.method || 'post').toUpperCase()}</Tag>
                                        <Tag size="small">{encodingLabel[selected.encoding] || '自动'}</Tag>
                                        <Tag size="small">{selected.os === 'windows' ? 'Windows' : selected.os === 'linux' ? 'Linux' : 'OS 自动'}</Tag>
                                        {selected.remark && <Text type="tertiary" size="small">{selected.remark}</Text>}
                                    </Space>
                                </div>
                                <Space spacing={8}>
                                    <Button size="small" loading={probeState[selected.id] === 'probing'} onClick={() => probe(selected)}>
                                        {t('webshell.testConnectivity', null, '测试连通性')}
                                    </Button>
                                    <Button size="small" disabled={!canWrite} onClick={() => setForm({ visible: true, record: selected })}>
                                        {t('webshell.editConnection', null, '编辑')}
                                    </Button>
                                    <Button size="small" type="danger" disabled={!canDelete} onClick={() => remove(selected)}>
                                        {t('webshell.deleteFile', null, '删除')}
                                    </Button>
                                </Space>
                            </div>
                            <Tabs
                                key={selected.id}
                                type="line"
                                defaultActiveKey="terminal"
                                className="webshell-tabs"
                                items={tabs}
                            />
                        </div>
                    )}
                </section>
            </div>

            <ConnectionForm
                visible={form.visible}
                record={form.record}
                projects={projects}
                submitting={submitting}
                onCancel={() => setForm({ visible: false, record: null })}
                onSubmit={submitForm}
            />
        </div>
    );
}
