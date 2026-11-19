/** 深度测评工作台：状态控制、事件轮询与分区标签页。 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Modal, Space, Tabs, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useInterval, useResource } from '../../components/hooks';
import {
    PageHeader, ErrorBanner, StatusTag, confirmAction, toastError, toastSuccess, toastWarning
} from '../../components/ui';
import { formatDateTime, formatNumber } from '../../lib/format';
import {
    PERM_DELETE, PERM_WRITE, canArchive, canCancel, canContinueStopped, canDeleteTask, canPause,
    canRestore, canStart, needsReview, statusText, truncateText
} from './labels';
import OverviewTab from './OverviewTab';
import IntentsTab from './IntentsTab';
import FactsTab from './FactsTab';
import AssetsTab from './AssetsTab';
import GraphTab from './GraphTab';
import FindingsTab from './FindingsTab';
import AcceptanceTab from './AcceptanceTab';
import EvidenceTab from './EvidenceTab';
import ReportTab from './ReportTab';

const { Text } = Typography;

const TABS = [
    { key: 'overview', i18n: 'deepAssessments.overview', fallback: '概览' },
    { key: 'intents', i18n: 'deepAssessments.intents', fallback: '意图' },
    { key: 'facts', i18n: 'deepAssessments.facts', fallback: '事实' },
    { key: 'assets', i18n: 'deepAssessments.assets', fallback: '资产', permission: 'asset:read' },
    { key: 'graph', i18n: 'deepAssessments.graph', fallback: '图谱' },
    { key: 'findings', i18n: 'deepAssessments.findings', fallback: '漏洞', permission: 'vulnerability:read' },
    { key: 'acceptance', i18n: 'deepAssessments.acceptance', fallback: '验收' },
    { key: 'evidence', i18n: 'deepAssessments.evidence', fallback: '证据' },
    { key: 'report', i18n: 'deepAssessments.reportTab', fallback: '报告' }
];

export default function Workbench({ id, tab, criterion, evidencePath, evidenceStepId, observation, onTabChange, onBack, onDeleted }) {
    const t = useT();
    const { hasPermission } = useAuth();
    const resource = useResource(() => api.get(`/deep-assessments/${id}`), [id]);
    const task = resource.data?.task || null;
    const [events, setEvents] = useState([]);
    const [eventsError, setEventsError] = useState(null);
    const [autoRefresh, setAutoRefresh] = useState(true);
    const [busy, setBusy] = useState('');
    const [reloadToken, setReloadToken] = useState(0);
    const [intentFilter, setIntentFilter] = useState('');
    const [messageMode, setMessageMode] = useState('');
    const [messageText, setMessageText] = useState('');
    const lastEventRef = useRef(0);
    const eventsBusyRef = useRef(false);

    const canWrite = hasPermission(PERM_WRITE);
    const canDelete = hasPermission(PERM_DELETE);
    const canExecute = hasPermission('agent:execute');

    const visibleTabs = useMemo(
        () => TABS.filter(item => !item.permission || hasPermission(item.permission)),
        [hasPermission]
    );
    const activeTab = visibleTabs.some(item => item.key === tab) ? tab : 'overview';

    const loadEvents = useCallback(async (initial = false) => {
        if (!initial && eventsBusyRef.current) return;
        if (!initial && (document.hidden || !autoRefresh)) return;
        eventsBusyRef.current = true;
        try {
            const data = await api.get(
                `/deep-assessments/${id}/events`,
                initial ? { latest: 'true' } : { after: lastEventRef.current }
            );
            const list = Array.isArray(data?.events) ? data.events : [];
            if (initial) {
                setEvents(list);
                lastEventRef.current = list.length ? Number(list[list.length - 1].id) || 0 : 0;
            } else if (list.length) {
                setEvents(prev => [...prev, ...list].slice(-500));
                lastEventRef.current = Number(list[list.length - 1].id) || lastEventRef.current;
            }
            setEventsError(null);
        } catch (error) {
            if (initial) setEventsError(error);
        } finally {
            eventsBusyRef.current = false;
        }
    }, [id, autoRefresh]);

    useEffect(() => {
        lastEventRef.current = 0;
        setEvents([]);
        setEventsError(null);
        loadEvents(true);
        return () => { lastEventRef.current = 0; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [id]);

    useEffect(() => {
        if (activeTab === 'overview') loadEvents(false);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeTab]);

    // 10 秒轮询：事件增量 + 静默刷新任务状态；页面隐藏、关闭自动刷新或控制请求进行中时跳过。
    useInterval(() => {
        if (document.hidden) return;
        loadEvents(false);
        if (!busy) resource.reload({ silent: true });
    }, activeTab === 'overview' && autoRefresh ? 10000 : 0);

    const refreshAll = async () => {
        await resource.reload();
        setReloadToken(value => value + 1);
        loadEvents(true);
    };

    const runControl = async (action, extra = {}) => {
        if (!task || !canWrite) return;
        setBusy(action);
        try {
            const data = await api.post(`/deep-assessments/${id}/control/${action}`, {
                revision: task.revision,
                ...extra
            });
            if (data?.task) resource.setData({ task: data.task });
            if (data?.start_error) toastWarning(data.start_error);
            else toastSuccess(t('deepAssessments.updated', null, '已更新'));
            setReloadToken(value => value + 1);
            loadEvents(true);
            resource.reload({ silent: true });
        } catch (error) {
            toastError(error, t('deepAssessments.controlFailed', null, '操作失败'));
            resource.reload();
        } finally {
            setBusy('');
        }
    };

    const submitMessage = async () => {
        if (!task) return;
        const message = messageText.trim();
        if (!message) {
            toastWarning('请填写补充内容');
            return;
        }
        setBusy('message');
        try {
            const data = await api.post(`/deep-assessments/${id}/control/${messageMode}`, {
                revision: task.revision,
                message
            });
            if (data?.task) resource.setData({ task: data.task });
            if (data?.start_error) toastWarning(data.start_error);
            else toastSuccess(messageMode === 'answer' ? '已答复并继续' : '已提交方向');
            setMessageMode('');
            setMessageText('');
            setReloadToken(value => value + 1);
            loadEvents(true);
            resource.reload({ silent: true });
        } catch (error) {
            toastError(error, '提交失败');
        } finally {
            setBusy('');
        }
    };

    const removeTask = () => {
        if (!task) return;
        confirmAction({
            title: t('deepAssessments.delete', null, '删除'),
            content: '删除本条测评？关联执行会话仍保留。',
            okText: t('deepAssessments.delete', null, '删除'),
            onOk: async () => {
                try {
                    await api.del(`/deep-assessments/${id}`);
                    toastSuccess(t('deepAssessments.deleted', null, '已删除'));
                    onDeleted?.();
                } catch (error) {
                    toastError(error, '删除失败');
                }
            }
        });
    };

    const headerExtra = [
        <Button key="back" theme="borderless" onClick={onBack}>返回列表</Button>,
        <Button key="refresh" icon={<IconRefresh />} onClick={refreshAll} loading={resource.loading && !busy}>刷新</Button>,
        <Button
            key="start"
            theme="solid"
            type="primary"
            disabled={!canWrite || !canExecute || Boolean(busy) || !canStart(task)}
            loading={busy === 'start'}
            onClick={() => runControl('start')}
        >
            {t('deepAssessments.start', null, '开始 / 继续')}
        </Button>,
        <Button
            key="pause"
            disabled={!canWrite || Boolean(busy) || !canPause(task)}
            loading={busy === 'pause'}
            onClick={() => runControl('pause')}
        >
            {t('deepAssessments.pause', null, '暂停')}
        </Button>,
        <Button
            key="cancel"
            type="danger"
            disabled={!canWrite || Boolean(busy) || !canCancel(task)}
            loading={busy === 'cancel'}
            onClick={() => runControl('cancel')}
        >
            {t('deepAssessments.cancel', null, '停止')}
        </Button>,
        canContinueStopped(task) ? (
            <Button key="continue" disabled={!canWrite || !canExecute || Boolean(busy)} loading={busy === 'continue-stopped'} onClick={() => runControl('continue-stopped')}>
                继续测评
            </Button>
        ) : null,
        <Button
            key="archive"
            disabled={!canWrite || Boolean(busy) || (task?.archived ? !canRestore(task) : !canArchive(task))}
            loading={busy === 'archive' || busy === 'restore'}
            onClick={() => runControl(task?.archived ? 'restore' : 'archive')}
        >
            {task?.archived ? '恢复到列表' : '归档'}
        </Button>,
        <Button
            key="delete"
            type="danger"
            theme="borderless"
            icon={<IconDelete />}
            disabled={!canDelete || Boolean(busy) || !canDeleteTask(task)}
            onClick={removeTask}
        >
            {t('deepAssessments.delete', null, '删除')}
        </Button>
    ];

    return (
        <div className="assessment-page">
            <PageHeader
                title={task?.name || t('deepAssessments.title', null, '深度测评')}
                description={task?.objective ? truncateText(task.objective, 160) : null}
                extra={headerExtra}
            >
                <Space spacing={8} wrap className="assessment-meta">
                    {task ? <StatusTag status={task.status} label={statusText(t, task.status)} /> : null}
                    {task?.archived ? <StatusTag status="archived" label={statusText(t, 'archived')} /> : null}
                    {task && needsReview(task) ? <StatusTag status="needs_review" label={statusText(t, 'needs_review')} /> : null}
                    {task ? <Text type="tertiary">模型 {task.model || '系统默认'}</Text> : null}
                    {task?.role ? <Text type="tertiary">角色 {task.role}</Text> : null}
                    {task ? <Text type="tertiary">规划轮次 {formatNumber(task.rounds || 0)}</Text> : null}
                    {task ? <Text type="tertiary">累计 Token {formatNumber(task.tokens_used || 0)}</Text> : null}
                    {task?.created_at ? <Text type="tertiary">创建于 {formatDateTime(task.created_at)}</Text> : null}
                </Space>
            </PageHeader>

            <ErrorBanner error={resource.error} onRetry={refreshAll} />

            {task?.archived ? (
                <Banner
                    type="info"
                    bordered
                    closeIcon={null}
                    description="测评已归档。恢复到列表后才能继续控制、关联资产或补充方向。"
                />
            ) : null}
            {task?.status === 'blocked' ? (
                <Banner
                    type="danger"
                    bordered
                    closeIcon={null}
                    className="assessment-banner"
                    description={task.summary || '测评受阻，请查看运行记录定位原因；排除后可手动继续。'}
                >
                    <Button size="small" onClick={() => onTabChange?.('overview')}>查看运行记录</Button>
                </Banner>
            ) : null}
            {task?.question ? (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    className="assessment-banner"
                    description={`执行需要人工确认：${task.question}`}
                >
                    <Button size="small" theme="solid" type="primary" disabled={!canWrite || !canExecute} onClick={() => { setMessageMode('answer'); setMessageText(''); }}>
                        答复并继续
                    </Button>
                </Banner>
            ) : null}
            {task && !task.archived && needsReview(task) ? (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    className="assessment-banner"
                    description={`有结果不明的工具调用待核对（${task.review_count || (task.steps || []).filter(step => ['uncertain', 'cleanup_unconfirmed'].includes(step.status)).length} 项）。核对只更新记录，不会自动执行。`}
                >
                    <Button size="small" onClick={() => { setIntentFilter('uncertain'); onTabChange?.('intents'); }}>
                        处理待核对
                    </Button>
                </Banner>
            ) : null}

            <Tabs
                type="line"
                activeKey={activeTab}
                onChange={key => onTabChange?.(key)}
                tabList={visibleTabs.map(item => ({ itemKey: item.key, tab: t(item.i18n, null, item.fallback) }))}
                className="assessment-tabs"
            >
                {!task ? null : activeTab === 'overview' ? (
                    <OverviewTab
                        task={task}
                        canWrite={canWrite && canExecute}
                        events={events}
                        eventsError={eventsError}
                        autoRefresh={autoRefresh}
                        onAutoRefreshChange={setAutoRefresh}
                        onRefreshEvents={() => loadEvents(true)}
                        onGoTab={onTabChange}
                        onGuidance={() => { setMessageMode('guidance'); setMessageText(''); }}
                        onAnswer={() => { setMessageMode('answer'); setMessageText(''); }}
                    />
                ) : activeTab === 'intents' ? (
                    <IntentsTab
                        id={id}
                        reloadToken={reloadToken}
                        status={intentFilter}
                        onStatusChange={setIntentFilter}
                        archived={Boolean(task.archived)}
                        onChanged={() => {
                            setReloadToken(value => value + 1);
                            resource.reload({ silent: true });
                        }}
                    />
                ) : activeTab === 'facts' ? (
                    <FactsTab id={id} reloadToken={reloadToken} task={task} />
                ) : activeTab === 'assets' ? (
                    <AssetsTab id={id} task={task} observation={observation} reloadToken={reloadToken} />
                ) : activeTab === 'graph' ? (
                    <GraphTab id={id} reloadToken={reloadToken} />
                ) : activeTab === 'findings' ? (
                    <FindingsTab id={id} task={task} reloadToken={reloadToken} />
                ) : activeTab === 'acceptance' ? (
                    <AcceptanceTab id={id} criterion={criterion} canWrite={canWrite && !task.archived} reloadToken={reloadToken} />
                ) : activeTab === 'evidence' ? (
                    <EvidenceTab key={`${evidencePath}-${criterion}-${evidenceStepId}`} id={id} criterion={evidencePath ? criterion : 0} stepId={evidenceStepId} reloadToken={reloadToken} />
                ) : (
                    <ReportTab id={id} task={task} reloadToken={reloadToken} />
                )}
            </Tabs>

            {!task && resource.loading ? <div className="assessment-loading">正在读取测评…</div> : null}

            <Modal
                visible={Boolean(messageMode)}
                title={messageMode === 'answer' ? '答复并继续' : '补充线索或调整方向'}
                onCancel={() => setMessageMode('')}
                onOk={submitMessage}
                confirmLoading={busy === 'message'}
                okText="提交"
                cancelText="取消"
                width={560}
            >
                <p className="assessment-hint">
                    {messageMode === 'answer'
                        ? '答复会保存到人工补充并清除待答复状态，随后自动继续测评。'
                        : '补充方向会在下一轮规划中使用；执行中的测评需当前账号继续后才可补充。'}
                </p>
                <TextArea
                    value={messageText}
                    onChange={setMessageText}
                    rows={5}
                    maxLength={8000}
                    placeholder="例如：优先验证登录接口的越权风险，并附上原始请求回执。"
                />
            </Modal>
        </div>
    );
}
