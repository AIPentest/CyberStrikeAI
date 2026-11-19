/** 对话工作台：会话列表 + 消息区 + 流式发送 + 内联 HITL + 用量/计划侧滑。 */
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Collapse, Progress, SideSheet, Typography } from '@douyinfe/semi-ui';

import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useDebounced, useInterval, useLocalStorage, useResource } from '../../components/hooks';
import { toastError, toastSuccess, toastWarning } from '../../components/ui';
import ProjectFormModal from '../projects/project-form';
import ChatHeader from './ChatHeader';
import ChatSettings from './ChatSettings';
import ConversationList from './ConversationList';
import MessageList from './MessageList';
import Composer, { AGENT_MODES } from './Composer';
import HitlDock from './HitlDock';
import { PlanTasksSheet, TokenUsageSheet } from './Sheets';
import { useAgentRun } from './useAgentRun';
import { useConversationActions } from './useConversationActions';
import { useModelPreferences } from './useModelPreferences';
import './page.css';

const { Text } = Typography;

const PAGE_SIZE = 20;
const MODE_STORAGE_KEY = 'csai-chat-agent-mode';
const EMPTY_PLAN = { tasks: [], total: 0, completed: 0, activeStep: 0, running: false };
const EMPTY_DETAIL = { conversation: null, messages: [], loading: false, error: null };

function useMediaQuery(query) {
    const [matches, setMatches] = useState(() => (
        typeof window !== 'undefined' && typeof window.matchMedia === 'function'
            ? window.matchMedia(query).matches
            : false
    ));
    useEffect(() => {
        if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined;
        const mql = window.matchMedia(query);
        const handler = event => setMatches(event.matches);
        setMatches(mql.matches);
        mql.addEventListener('change', handler);
        return () => mql.removeEventListener('change', handler);
    }, [query]);
    return matches;
}

export default function Page() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canWrite = hasPermission('chat:write');
    const canCreateProject = hasPermission('project:write');
    const canExecute = hasPermission('agent:execute');
    const canDelete = hasPermission('chat:delete');
    const canReadTasks = hasPermission('tasks:read');
    const canStop = hasPermission('tasks:write');
    const canReadHitl = hasPermission('hitl:read');
    const canDecideHitl = hasPermission('hitl:write');
    const canReadConfig = hasPermission('config:read');

    const [searchParams, setSearchParams] = useSearchParams();
    const conversationId = searchParams.get('conversation') || '';
    const conversationRef = useRef(conversationId);
    const boundByRun = useRef(null);
    useLayoutEffect(() => { conversationRef.current = conversationId; }, [conversationId]);

    const [search, setSearch] = useState('');
    const debouncedSearch = useDebounced(search, 350);
    const [projectFilter, setProjectFilter] = useState('');
    const [page, setPage] = useState(1);
    const [newProjectId, setNewProjectId] = useState('');
    const [projectFormOpen, setProjectFormOpen] = useState(false);
    const [storedMode, setStoredMode] = useLocalStorage(MODE_STORAGE_KEY, 'single');
    const mode = AGENT_MODES.includes(storedMode) ? storedMode : 'single';
    const model = useModelPreferences(canReadConfig);
    const [settingsOpen, setSettingsOpen] = useState(false);
    const [draftHitl, setDraftHitl] = useState(null);

    const [detail, setDetail] = useState(EMPTY_DETAIL);
    const [composers, setComposers] = useState(() => new Map());
    const { draft = '', attachments = [] } = composers.get(conversationId) || {};
    const updateComposer = useCallback((id, change) => {
        setComposers(previous => {
            const next = new Map(previous);
            next.set(id, change(previous.get(id) || {}));
            return next;
        });
    }, []);
    const setDraft = value => updateComposer(conversationId, current => ({ ...current, draft: value }));
    const [approvalSnapshot, setApprovalSnapshot] = useState(null);
    const hitlPending = canReadHitl && approvalSnapshot?.id === conversationId ? approvalSnapshot.items : [];
    const [hitlBusy, setHitlBusy] = useState('');
    const [plan, setPlan] = useState(EMPTY_PLAN);
    const [planLoading, setPlanLoading] = useState(false);
    const [usageOpen, setUsageOpen] = useState(false);
    const [planOpen, setPlanOpen] = useState(false);
    const [sidebarOpen, setSidebarOpen] = useState(false);
    const narrow = useMediaQuery('(max-width: 900px)');

    const detailSeq = useRef(0);
    const hitlSeq = useRef(0);
    const planSeq = useRef(0);
    const activeTask = useResource(async () => {
        const payload = await api.get('/agent-loop/tasks');
        return { id: conversationId, running: Array.isArray(payload?.tasks) && payload.tasks.some(task => task.conversationId === conversationId) };
    }, [conversationId, canReadTasks], { skip: !conversationId || !canReadTasks });
    const backgroundRunning = canReadTasks && activeTask.data?.id === conversationId && activeTask.data.running;
    const checkingRun = Boolean(canReadTasks && conversationId && activeTask.data?.id !== conversationId && !activeTask.error);
    const lastActive = useRef(null);

    const conversations = useResource(
        () => api.get('/conversations', {
            limit: PAGE_SIZE,
            offset: (page - 1) * PAGE_SIZE,
            search: debouncedSearch || undefined,
            project_id: projectFilter || undefined
        }),
        [page, debouncedSearch, projectFilter]
    );

    const reloadConversations = useRef(conversations.reload);
    useEffect(() => { reloadConversations.current = conversations.reload; }, [conversations.reload]);

    const projectsResource = useResource(() => api.get('/projects', { limit: 200 }), []);
    const projects = useMemo(() => (
        Array.isArray(projectsResource.data?.projects) ? projectsResource.data.projects : []
    ), [projectsResource.data]);
    const handleProjectCreated = project => {
        setProjectFormOpen(false);
        if (!project?.id) return;
        projectsResource.setData(previous => ({
            ...previous,
            projects: [project, ...(previous?.projects || [])]
        }));
        setNewProjectId(project.id);
    };

    const listItems = Array.isArray(conversations.data?.conversations) ? conversations.data.conversations : [];

    const loadDetail = useCallback(async (id, { silent = false } = {}) => {
        const seq = ++detailSeq.current;
        if (!id) {
            setDetail(EMPTY_DETAIL);
            return null;
        }
        if (!silent) setDetail(prev => ({ ...prev, loading: true, error: null }));
        try {
            const payload = await api.get(`/conversations/${encodeURIComponent(id)}`, { include_process_details: 1 });
            if (seq !== detailSeq.current || conversationRef.current !== id) return null;
            setDetail({
                conversation: payload || null,
                messages: Array.isArray(payload?.messages) ? payload.messages : [],
                loading: false,
                error: null
            });
            return payload;
        } catch (error) {
            if (seq === detailSeq.current && conversationRef.current === id) {
                setDetail(prev => ({ ...prev, loading: false, error }));
            }
            return null;
        }
    }, []);

    const refreshHitl = useCallback(async () => {
        const id = conversationRef.current;
        const seq = ++hitlSeq.current;
        if (!id || !canReadHitl) return;
        try {
            const payload = await api.get('/hitl/pending', { conversationId: id, pageSize: 50 });
            if (seq !== hitlSeq.current || conversationRef.current !== id) return;
            const items = (Array.isArray(payload?.items) ? payload.items : []).filter(item =>
                !item.conversationId || item.conversationId === id
            );
            setApprovalSnapshot({ id, items });
        } catch (error) {
            /* 审批轮询失败不打断对话 */
        }
    }, [canReadHitl]);

    const refreshPlan = useCallback(async () => {
        const id = conversationRef.current;
        if (!id) {
            setPlan(EMPTY_PLAN);
            return;
        }
        const seq = ++planSeq.current;
        setPlanLoading(true);
        try {
            const payload = await api.get(`/conversations/${encodeURIComponent(id)}/plan-tasks`);
            if (seq === planSeq.current && conversationRef.current === id) setPlan(payload && typeof payload === 'object' ? payload : EMPTY_PLAN);
        } catch (error) {
            /* 计划看板按需刷新，失败静默 */
        } finally {
            if (seq === planSeq.current && conversationRef.current === id) setPlanLoading(false);
        }
    }, []);

    const bindConversation = useCallback(id => {
        setDraftHitl(null);
        boundByRun.current = id;
        conversationRef.current = id;
        setSearchParams({ conversation: id }, { replace: true });
        reloadConversations.current?.({ silent: true });
    }, [setSearchParams]);

    const settleRun = useCallback(() => {
        reloadConversations.current?.({ silent: true });
        refreshHitl();
        refreshPlan();
    }, [refreshHitl, refreshPlan]);

    const run = useAgentRun({
        conversationRef,
        bindConversation,
        requestDetailReload: loadDetail,
        onSettled: settleRun,
        onHitlEvent: refreshHitl,
        onPlanTool: refreshPlan,
        t
    });
    const { live, sending, lastEvents, reset: resetRun, isStreaming } = run;

    // 会话切换：清空流式状态并按需加载历史
    useEffect(() => {
        // 极速回复可能先于路由提交完成，绑定会话时仍须保留最终事件。
        const newlyBound = conversationId && boundByRun.current === conversationId;
        boundByRun.current = null;
        if (newlyBound || isStreaming(conversationId)) return;
        resetRun();
        detailSeq.current++;
        hitlSeq.current++;
        planSeq.current++;
        setDetail(EMPTY_DETAIL);
        setApprovalSnapshot(null);
        setHitlBusy('');
        setPlan(EMPTY_PLAN);
        setPlanLoading(false);
        setPlanOpen(false);
        if (!conversationId) return;
        loadDetail(conversationId);
        refreshHitl();
        refreshPlan();
    }, [conversationId, isStreaming, resetRun, loadDetail, refreshHitl, refreshPlan]);

    useInterval(() => {
        refreshHitl();
        refreshPlan();
        if (backgroundRunning) {
            activeTask.reload({ silent: true });
            if (!sending && conversationRef.current) loadDetail(conversationRef.current, { silent: true });
        }
    }, sending || backgroundRunning ? 4000 : 0);

    useEffect(() => {
        if (lastActive.current === conversationId && !backgroundRunning) {
            loadDetail(conversationId, { silent: true });
            reloadConversations.current?.({ silent: true });
        }
        lastActive.current = backgroundRunning ? conversationId : null;
    }, [backgroundRunning, conversationId, loadDetail]);

    const stop = async () => {
        if (!canStop) return;
        await run.stop();
        activeTask.reload({ silent: true });
    };

    const selectConversation = useCallback(id => {
        if (!id) return;
        if (id !== conversationRef.current) setSearchParams({ conversation: id });
        setSidebarOpen(false);
    }, [setSearchParams]);

    const reloadList = useCallback(() => {
        reloadConversations.current?.({ silent: true });
    }, []);

    const clearSelection = useCallback(() => setDetail(EMPTY_DETAIL), []);

    const {
        createConversation, renameConversation, togglePin, deleteConversation, batchDelete, deleteTurn
    } = useConversationActions({
        t,
        conversationRef,
        newProjectId,
        canWrite,
        reloadList,
        requestDetailReload: loadDetail,
        navigate: setSearchParams,
        onSelectionCleared: clearSelection
    });

    const decideHitl = useCallback(async (item, decision, comment) => {
        const id = conversationRef.current;
        if (!canDecideHitl || !item?.interruptId || approvalSnapshot?.id !== id ||
            !approvalSnapshot.items.some(row => String(row.id || row.interruptId) === item.interruptId)) return;
        setHitlBusy(item.interruptId);
        try {
            await api.post('/hitl/decision', {
                interruptId: item.interruptId,
                decision,
                comment: comment || ''
            });
            toastSuccess(decision === 'approve'
                ? t('chat.hitlApproved', null, '已批准本次调用')
                : t('chat.hitlRejected', null, '已拒绝本次调用'));
            setApprovalSnapshot(previous => previous?.id === id
                ? { id, items: previous.items.filter(row => String(row.id || row.interruptId) !== item.interruptId) }
                : previous);
        } catch (error) {
            toastError(error, t('chat.hitlDecisionFailed', null, '提交审批失败'));
        } finally {
            if (conversationRef.current === id) {
                setHitlBusy('');
                refreshHitl();
            }
        }
    }, [approvalSnapshot, canDecideHitl, refreshHitl, t]);

    const dismissHitl = useCallback(async item => {
        const id = conversationRef.current;
        if (!canDecideHitl || !item?.interruptId || approvalSnapshot?.id !== id ||
            !approvalSnapshot.items.some(row => String(row.id || row.interruptId) === item.interruptId)) return;
        setHitlBusy(item.interruptId);
        try {
            await api.post('/hitl/dismiss', { interruptId: item.interruptId });
            toastSuccess(t('chat.hitlDismissed', null, '已忽略该审批'));
            setApprovalSnapshot(previous => previous?.id === id
                ? { id, items: previous.items.filter(row => String(row.id || row.interruptId) !== item.interruptId) }
                : previous);
        } catch (error) {
            toastError(error, t('chat.hitlDismissFailed', null, '忽略审批失败'));
        } finally {
            if (conversationRef.current === id) {
                setHitlBusy('');
                refreshHitl();
            }
        }
    }, [approvalSnapshot, canDecideHitl, refreshHitl, t]);

    const uploadFiles = useCallback(async files => {
        const id = conversationRef.current;
        const updateAttachments = change => updateComposer(id, current => ({
            ...current, attachments: change(current.attachments || [])
        }));
        const entries = files.map((file, index) => ({
            id: `att-${Date.now()}-${index}`,
            fileName: file.name,
            mimeType: file.type || '',
            serverPath: '',
            uploading: true,
            error: false
        }));
        updateAttachments(prev => [...prev, ...entries]);
        await Promise.all(entries.map(async (entry, index) => {
            const form = new FormData();
            form.append('file', files[index]);
            if (id) form.append('conversationId', id);
            try {
                const payload = await api.post('/chat-uploads', form);
                const absolutePath = String(payload?.absolutePath || '').trim();
                if (!absolutePath) throw new Error('服务端未返回文件路径');
                updateAttachments(prev => prev.map(item => (
                    item.id === entry.id ? { ...item, serverPath: absolutePath, uploading: false, error: false } : item
                )));
            } catch (error) {
                updateAttachments(prev => prev.map(item => (
                    item.id === entry.id ? { ...item, uploading: false, error: true } : item
                )));
                toastError(error, t('chat.attachmentUploadFailed', { name: entry.fileName }, `附件 ${entry.fileName} 上传失败`));
            }
        }));
    }, [updateComposer, t]);

    const removeAttachment = useCallback(id => {
        updateComposer(conversationId, current => ({
            ...current, attachments: (current.attachments || []).filter(item => item.id !== id)
        }));
    }, [conversationId, updateComposer]);

    const submit = useCallback(() => {
        if (!canWrite || !canExecute || sending || backgroundRunning || checkingRun || model.waitingForChannel) return;
        const text = draft.trim();
        if (!text && !attachments.length) {
            toastWarning(t('chat.emptyMessage', null, '请输入内容或添加附件'));
            return;
        }
        if (attachments.some(item => item.uploading)) {
            toastWarning(t('chat.waitingAttachmentsUpload', null, '附件仍在上传，请稍候'));
            return;
        }
        const ready = attachments.filter(item => item.serverPath && !item.error);
        if (attachments.length && ready.length !== attachments.length) {
            toastWarning(t('chat.attachmentsUploadIncomplete', null, '存在上传失败的附件，请移除后再发送'));
            return;
        }

        const message = text || t('chat.filePrompt', null, '请解析我上传的附件。');
        const displayContent = ready.length
            ? `${message}\n${ready.map(item => `📎 ${item.fileName}`).join('\n')}`
            : message;
        setDetail(prev => ({
            ...prev,
            messages: [
                ...(prev.messages || []),
                { id: `local-${Date.now()}`, role: 'user', content: displayContent, createdAt: new Date().toISOString() }
            ]
        }));
        resetRun();
        updateComposer(conversationId, () => ({ draft: '', attachments: [] }));
        run.send({
            message,
            attachments: ready,
            mode,
            projectId: newProjectId,
            ...model.request,
            hitl: !conversationId && canDecideHitl ? draftHitl || undefined : undefined
        });
    }, [attachments, backgroundRunning, canWrite, canExecute, canDecideHitl, checkingRun, conversationId, draft, draftHitl, mode, model, newProjectId, resetRun, run, sending, t, updateComposer]);

    const conversation = detail.conversation;

    const sidebar = (
        <ConversationList
            items={listItems}
            total={conversations.data?.total || 0}
            loading={conversations.loading}
            error={conversations.error}
            onRetry={() => conversations.reload()}
            search={search}
            onSearch={value => { setSearch(value); setPage(1); }}
            projectId={projectFilter}
            onProjectChange={value => { setProjectFilter(value || ''); setPage(1); }}
            projects={projects}
            page={page}
            pageSize={PAGE_SIZE}
            onPageChange={setPage}
            selectedId={conversationId}
            onSelect={selectConversation}
            onCreate={createConversation}
            onRename={renameConversation}
            onPin={togglePin}
            onDelete={deleteConversation}
            onBatchDelete={batchDelete}
            canWrite={canWrite}
            canDelete={canDelete}
        />
    );

    return (
        <div className="chat-page">
            {!narrow && <aside className="chat-sidebar">{sidebar}</aside>}
            <section className="chat-main">
                <ChatHeader
                    conversation={conversation}
                    mode={mode}
                    hitlCount={hitlPending.length}
                    conversationId={conversationId}
                    plan={plan}
                    narrow={narrow}
                    onToggleSidebar={() => setSidebarOpen(true)}
                    onOpenUsage={() => setUsageOpen(true)}
                    onOpenSettings={canReadConfig || canReadHitl || (canWrite && canExecute)
                        ? () => setSettingsOpen(true) : undefined}
                    onOpenPlan={() => {
                        refreshPlan();
                        setPlanOpen(true);
                    }}
                    onRefresh={() => {
                        reloadConversations.current?.();
                        if (conversationId) loadDetail(conversationId);
                        refreshHitl();
                        refreshPlan();
                    }}
                />
                <div className="chat-panel">
                    <MessageList
                        conversationId={conversationId}
                        messages={detail.messages}
                        live={live}
                        loading={detail.loading || Boolean(conversationId && !detail.conversation && !detail.error)}
                        error={detail.error}
                        onRetry={() => (conversationId ? loadDetail(conversationId) : null)}
                        canDelete={canDelete}
                        onDeleteTurn={deleteTurn}
                    />
                    {plan.running && plan.total > 0 && (
                        <div className="chat-plan-strip">
                            <Text size="small">{t('chat.planTasks', null, '计划任务')}</Text>
                            <Progress
                                percent={Math.round((Number(plan.completed || 0) / Math.max(1, Number(plan.total))) * 100)}
                                showInfo={false}
                                size="small"
                                className="chat-plan-strip-bar"
                            />
                            <Text size="small" type="tertiary">
                                {plan.completed}/{plan.total}
                                {plan.activeStep ? ` · ${t('chat.planStep', { step: plan.activeStep }, `第 ${plan.activeStep} 步`)}` : ''}
                            </Text>
                            <Button
                                size="small"
                                theme="borderless"
                                onClick={() => {
                                    refreshPlan();
                                    setPlanOpen(true);
                                }}
                            >
                                {t('chat.planDetails', null, '详情')}
                            </Button>
                        </div>
                    )}
                    {lastEvents.length > 0 && !live && (
                        <Collapse className="chat-events-strip">
                            <Collapse.Panel itemKey="1" header={`${t('chat.rawEvents', null, '本轮原始事件')} · ${lastEvents.length}`}>
                                <pre className="chat-pre">
                                    {lastEvents.map((event, index) => `${index + 1}. ${JSON.stringify(event)}`).join('\n')}
                                </pre>
                            </Collapse.Panel>
                        </Collapse>
                    )}
                    <HitlDock
                        items={hitlPending}
                        readOnly={!canDecideHitl}
                        busyId={hitlBusy}
                        onRefresh={refreshHitl}
                        onDecide={decideHitl}
                        onDismiss={dismissHitl}
                    />
                    <Composer
                        value={draft}
                        onChange={setDraft}
                        onSend={submit}
                        onStop={stop}
                        canStop={canStop}
                        sending={sending || backgroundRunning}
                        checkingRun={checkingRun || model.waitingForChannel}
                        canWrite={canWrite}
                        canExecute={canExecute}
                        mode={mode}
                        onModeChange={value => setStoredMode(AGENT_MODES.includes(value) ? value : 'single')}
                        projectId={newProjectId}
                        onProjectChange={value => setNewProjectId(value || '')}
                        onCreateProject={canCreateProject && !projectsResource.loading
                            ? () => setProjectFormOpen(true) : undefined}
                        projects={projects}
                        showProjectSelect={!conversationId}
                        attachments={attachments}
                        onPickFiles={uploadFiles}
                        onRemoveAttachment={removeAttachment}
                        uploading={attachments.filter(item => item.uploading).length}
                    />
                </div>
            </section>
            {projectFormOpen && (
                <ProjectFormModal
                    visible
                    onCancel={() => setProjectFormOpen(false)}
                    onSaved={handleProjectCreated}
                />
            )}
            {settingsOpen && (
                <ChatSettings
                    key={conversationId}
                    onClose={() => setSettingsOpen(false)}
                    narrow={narrow}
                    busy={sending || backgroundRunning || checkingRun || !(canWrite && canExecute)}
                    canReadConfig={canReadConfig}
                    canReadHitl={canReadHitl}
                    canEnableHitl={canDecideHitl}
                    model={model}
                    conversationId={conversationId}
                    draftHitl={!conversationId ? draftHitl : null}
                    onDraftHitlChange={setDraftHitl}
                />
            )}
            <SideSheet
                visible={narrow && sidebarOpen}
                placement="left"
                width={300}
                onCancel={() => setSidebarOpen(false)}
                title={t('chat.conversations', null, '会话列表')}
                bodyStyle={{ padding: 0 }}
            >
                {sidebar}
            </SideSheet>
            <TokenUsageSheet
                visible={usageOpen}
                onClose={() => setUsageOpen(false)}
                conversationId={conversationId}
            />
            <PlanTasksSheet
                visible={planOpen}
                onClose={() => setPlanOpen(false)}
                plan={plan}
                loading={planLoading}
                onRefresh={refreshPlan}
            />
        </div>
    );
}
