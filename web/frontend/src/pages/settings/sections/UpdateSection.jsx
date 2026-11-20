/**
 * 更新源配置、远端版本检查和更新进度。
 */
import { useEffect, useState } from 'react';
import { Banner, Button, Input, Space, Tag, Timeline, Typography } from '@douyinfe/semi-ui';
import { IconDownload, IconRefresh } from '@douyinfe/semi-icons';
import { api, errorMessage } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../components/hooks';
import { ErrorBanner, KeyValueList, SectionCard, toastSuccess, toastWarning } from '../../../components/ui';
import { formatDateTime, formatNumber, truncate } from '../../../lib/format';
import { Hint, LoadingBlock, Mono, PermissionBanner } from './common';

const { Text } = Typography;

function shortCommit(commit) {
    const value = String(commit || '');
    return value ? value.slice(0, 12) : '—';
}

function yesNo(t, value) {
    return value
        ? <Tag size="small" color="green">{t('common.yes', null, '是')}</Tag>
        : <Tag size="small" color="grey">{t('common.no', null, '否')}</Tag>;
}

function jobStateLabel(t, state) {
    if (state === 'running') return t('update.stateRunning', null, '进行中');
    if (state === 'succeeded') return t('update.stateSucceeded', null, '已完成');
    if (state === 'failed') return t('update.stateFailed', null, '失败');
    return t('update.stateUnknown', null, '未知状态');
}

function jobStateColor(state) {
    if (state === 'succeeded') return 'green';
    if (state === 'failed') return 'red';
    if (state === 'running') return 'blue';
    return 'grey';
}

function updateBlocker(t, status) {
    if (!status.installed) return t('update.blockedNotGitTree');
    if (status.diverged) return t('update.blockedDiverged', { ahead: status.ahead, behind: status.behind });
    if (!status.updateAvailable && !status.buildPending) return '';
    if (status.rollbackPending) return t('updateUi.rollbackPending');
    if (status.blockingChanges?.length) return t('update.blockedLocalEdits', { n: status.blockingChanges.length });
    return '';
}

export default function UpdateSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('update:read');
    const canManage = hasPermission('update:apply');

    const consoleRes = useResource(() => api.get('/system/update'), [canRead], { skip: !canRead });
    const [job, setJob] = useState(null);
    const [action, setAction] = useState('');
    const [checkResult, setCheckResult] = useState(null);
    const [actionError, setActionError] = useState(null);
    const [activationBootID, setActivationBootID] = useState(null);
    const [sourceDraft, setSourceDraft] = useState(null);
    const [savingSource, setSavingSource] = useState(false);
    const [sourceError, setSourceError] = useState(null);

    useEffect(() => {
        if (consoleRes.data) setJob(consoleRes.data.job || null);
    }, [consoleRes.data]);

    const running = job?.state === 'running';
    const restarting = job?.state === 'succeeded' && job.restartRequested;
    const watchedJob = running || restarting ? job.id : null;
    const previousBootID = activationBootID || consoleRes.data?.bootID;
    const targetCommit = job?.result?.toCommit;
    const reloadStatus = consoleRes.reload;
    const needsRestart = consoleRes.data?.needsRestart;
    const source = consoleRes.data?.source;
    const sourceRepo = source?.repo || '';
    const sourceValue = sourceDraft ?? sourceRepo;
    const sourceChanged = sourceValue.trim() !== sourceRepo;
    const busy = Boolean(action || running || restarting);
    const sourceBusy = savingSource || busy || consoleRes.loading;

    useEffect(() => {
        if (!watchedJob) return;
        let cancelled = false;
        let timer;
        const poll = async () => {
            try {
                const { job: latest } = await api.get('/system/update/job', undefined, { timeout: 5000 });
                if (cancelled) return;
                if (latest?.id === watchedJob) {
                    setJob(latest);
                    if (latest.state !== 'running' && !latest.restartRequested) {
                        setCheckResult(null);
                        if (latest.state === 'succeeded') {
                            if (latest.result?.needsRestart) toastWarning(t('update.needsRestartToast'));
                            else toastSuccess(t('update.jobDoneToast'));
                        }
                        reloadStatus({ silent: true });
                        return;
                    }
                } else {
                    // The new process starts with an empty job list. Confirm the
                    // new boot before reloading the frontend bundle.
                    const status = await api.get('/system/update', undefined, { timeout: 5000 });
                    if (cancelled) return;
                    const newBoot = previousBootID && status.bootID && status.bootID !== previousBootID;
                    const matchesBuild = !targetCommit || !status.buildRevision || status.buildRevision.startsWith(targetCommit);
                    if (newBoot && matchesBuild && !status.needsRestart) {
                        window.location.reload();
                        return;
                    }
                }
            } catch {
                // Temporary connection failures during a restart are expected.
            }
            if (!cancelled) timer = setTimeout(poll, 2000);
        };
        poll();
        return () => {
            cancelled = true;
            clearTimeout(timer);
        };
    }, [watchedJob, previousBootID, targetCommit, reloadStatus, t]);

    const update = async () => {
        if (!canManage || sourceBusy || sourceChanged) return;
        setAction('checking');
        setActionError(null);
        setCheckResult(null);
        let applyRequested = false;
        try {
            const { status } = await api.post('/system/update/check', {});
            if (!status) throw new Error(t('updateUi.statusMissing'));
            setCheckResult(status);
            if (status.checkError) return;

            const blocker = updateBlocker(t, status);
            if (blocker) {
                setActionError(new Error(blocker));
                return;
            }
            if (!status.updateAvailable && !status.buildPending) {
                if (needsRestart) toastWarning(t('update.needsRestartToast'));
                else if (status.ahead > 0) toastSuccess(t('updateUi.noNewerVersion'));
                else toastSuccess(t('updateUi.currentVersion'));
                return;
            }

            applyRequested = true;
            setAction('starting');
            const restart = Boolean(consoleRes.data?.autoRestart ?? consoleRes.data?.canRestart);
            const response = await api.post('/system/update/apply', { restart });
            if (!response?.job_id) throw new Error(t('updateUi.jobMissing'));
            setActivationBootID(response.bootID || null);
            setJob({ id: response.job_id, kind: 'apply', state: response.state || 'running', restartRequested: restart });
        } catch (error) {
            const existingJob = error.status === 409 && error.payload?.job;
            if (applyRequested && existingJob?.id) {
                setActivationBootID(error.payload.bootID || null);
                setJob(existingJob);
                return;
            }
            setActionError(new Error(t(applyRequested ? 'update.applyFailed' : 'update.checkFailed', { reason: errorMessage(error) })));
            if (applyRequested) reloadStatus({ silent: true });
        } finally {
            setAction('');
        }
    };

    const saveSource = async event => {
        event.preventDefault();
        if (!source || !canManage || !sourceChanged || sourceBusy) return;
        setSavingSource(true);
        setSourceError(null);
        try {
            const payload = await api.post('/system/update/source', { repo: sourceValue.trim() });
            if (!payload?.source || typeof payload.source.repo !== 'string') {
                throw new Error(t('updateUi.sourceResponseMissing', null, '服务端未返回更新源，刷新后可核对配置。'));
            }
            consoleRes.setData(previous => ({ ...previous, source: payload.source }));
            setSourceDraft(null);
            setCheckResult(null);
            setActionError(null);
            toastSuccess(t('update.sourceSaveOk', null, '更新源已保存'));
        } catch (error) {
            setSourceError(error);
        } finally {
            setSavingSource(false);
        }
    };

    if (!canRead) {
        return <PermissionBanner permission="update:read" label={t('settings.nav.update', null, '读取安装树版本')} />;
    }
    if (consoleRes.loading && !consoleRes.data) return <LoadingBlock rows={6} />;

    const snapshot = checkResult || consoleRes.data?.status || {};
    const binaryBuiltAt = consoleRes.data?.binaryBuiltAt;

    const facts = [
        { label: t('update.root', null, '安装目录'), value: <Mono>{snapshot.root || '—'}</Mono> },
        { label: t('update.gitTree', null, 'Git 安装树'), value: yesNo(t, snapshot.installed) },
        { label: t('update.branch', null, '分支'), value: snapshot.branch || '—' },
        { label: t('updateUi.repo', null, '更新源仓库'), value: <Mono>{source?.repo || source?.defaultRepo || snapshot.repo || '—'}</Mono> },
        { label: t('update.commit', null, '当前提交'), value: <Mono>{shortCommit(snapshot.commit)}</Mono> },
        { label: t('update.subject', null, '提交说明'), value: truncate(snapshot.subject || '—', 80) },
        { label: t('update.committedAt', null, '提交时间'), value: snapshot.committedAt ? formatDateTime(snapshot.committedAt) : '—' },
        { label: t('update.runningBuild', null, '运行构建'), value: consoleRes.data?.buildRevision ? shortCommit(consoleRes.data.buildRevision) : '—' },
        { label: t('update.toolchainFound', null, 'Go 工具链'), value: yesNo(t, snapshot.canBuild) },
        { label: t('update.binaryPresent', null, '已构建二进制'), value: yesNo(t, snapshot.hasBinary) },
        { label: t('updateUi.buildPending', null, '待编译'), value: yesNo(t, snapshot.buildPending) },
        {
            label: t('update.restartPending', null, '需重启激活'),
            value: needsRestart
                ? <Space spacing={4}><Tag size="small" color="amber">{t('common.yes', null, '是')}</Tag>
                    {binaryBuiltAt && <Text type="tertiary" size="small">{formatDateTime(binaryBuiltAt)}</Text>}</Space>
                : <Tag size="small" color="green">{t('common.no', null, '否')}</Tag>
        },
        { label: t('updateUi.localChanges', null, '本机改动文件数'), value: formatNumber((snapshot.localChanges || []).length) },
        { label: t('updateUi.blockingChanges', null, '阻止更新的文件数'), value: formatNumber((snapshot.blockingChanges || []).length) }
    ];

    const incoming = Array.isArray(checkResult?.incoming) ? checkResult.incoming : [];

    return (
        <>
            <ErrorBanner error={consoleRes.error} onRetry={consoleRes.reload} />
            <SectionCard
                title={t('update.installTitle', null, '安装信息')}
                description={t('updateUi.pageHint')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => reloadStatus()} loading={consoleRes.loading} disabled={savingSource || busy}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button
                            theme="solid"
                            type="primary"
                            icon={<IconDownload aria-hidden="true" />}
                            loading={Boolean(action)}
                            disabled={!canManage || sourceBusy || sourceChanged}
                            onClick={update}
                        >
                            {t(action === 'checking' ? 'update.checking' : action === 'starting' ? 'updateUi.starting' : 'updateUi.updateButton')}
                        </Button>
                    </Space>
                )}
            >
                {!canManage && (
                    <Hint>{t('updateUi.updatePermission')}</Hint>
                )}
                {sourceChanged && <Hint>{t('updateUi.saveSourceFirst')}</Hint>}
                <KeyValueList items={facts} columns={3} />
                {snapshot.goToolchain && (
                    <Hint>{t('updateUi.toolchainPath', null, 'Go 工具链路径')} <Mono>{snapshot.goToolchain}</Mono></Hint>
                )}
            </SectionCard>

            {source && (
                <SectionCard title={t('update.sourceTitle', null, '更新源')}>
                    <form className="settings-update-source-form" onSubmit={saveSource}>
                        <label className="settings-field">
                            <Text size="small" type="tertiary">{t('update.sourceUrlLabel', null, '更新源仓库地址')}</Text>
                            <Input
                                value={sourceValue}
                                onChange={value => {
                                    setSourceDraft(value);
                                    setSourceError(null);
                                }}
                                placeholder={source.defaultRepo}
                                readOnly={!canManage}
                                disabled={savingSource}
                            />
                        </label>
                        {canManage ? (
                            <>
                                <Hint>{t('update.sourceRepoHint', null, '留空即使用官方仓库。')}</Hint>
                                <Hint>{t('updateUi.sourceSaveOnly', null, '保存会修改配置文件中的 update.repo。')}</Hint>
                                <div>
                                    <Button htmlType="submit" theme="solid" loading={savingSource}
                                        disabled={!sourceChanged || sourceBusy}>
                                        {t('update.sourceSaveBtn', null, '保存更新源')}
                                    </Button>
                                </div>
                                {running && <Hint>{t('update.blockedJobRunning', null, '已有更新在进行中，请等它结束。')}</Hint>}
                            </>
                        ) : (
                            <Hint>{t('updateUi.sourcePermission', null, '需要更新管理权限才能在这里保存地址。')}</Hint>
                        )}
                        <ErrorBanner error={sourceError} />
                    </form>
                </SectionCard>
            )}

            <SectionCard title={t('update.remoteTitle', null, '远端版本')}>
                <ErrorBanner error={actionError} onRetry={canManage && !sourceBusy && !sourceChanged ? update : undefined} />
                {!checkResult && !actionError && (
                    <Text type="tertiary">{t('updateUi.versionUnchecked', null, '尚未检查远端版本。')}</Text>
                )}
                {checkResult && (
                    <>
                        {checkResult.checkError ? (
                            <Banner
                                type="warning"
                                bordered
                                closeIcon={null}
                                description={t('update.checkFailed', { reason: checkResult.checkError }, `检查失败：${checkResult.checkError}`)}
                            />
                        ) : (
                            <Space wrap spacing={8}>
                                {checkResult.installed && (
                                    <Tag size="small" color={checkResult.updateAvailable || checkResult.buildPending || needsRestart ? 'amber' : 'green'}>
                                        {checkResult.updateAvailable ? t('update.behindCount', { n: checkResult.behind })
                                            : checkResult.buildPending ? t('updateUi.buildPending')
                                            : needsRestart ? t('update.restartNeeded')
                                            : t(checkResult.ahead > 0 ? 'updateUi.noNewerVersion' : 'updateUi.currentVersion')}
                                    </Tag>
                                )}
                                {checkResult.ahead > 0 && (
                                    <Tag size="small" color="blue">{t('update.aheadCount', { n: checkResult.ahead }, `领先 ${checkResult.ahead} 个提交`)}</Tag>
                                )}
                                {checkResult.diverged && <Tag size="small" color="red">{t('updateUi.diverged', null, '本机与远端提交历史已分叉')}</Tag>}
                                <Text type="tertiary">
                                    {t('update.remoteHead', null, '远端 HEAD')}：<Mono>{shortCommit(checkResult.remoteCommit)}</Mono>
                                    {checkResult.targetBranch ? `（${checkResult.targetBranch}）` : ''}
                                </Text>
                            </Space>
                        )}
                        {(checkResult.remoteSubject || incoming.length > 0) && (
                            <div className="settings-incoming">
                                <Text type="tertiary" size="small">{t('update.incomingTitle', null, '待合入提交')}</Text>
                                <ul>
                                    {incoming.slice(0, 10).map((item, index) => (
                                        <li key={`${item.commit || 'commit'}-${index}`}>
                                            <Mono>{shortCommit(item.commit)}</Mono> {truncate(item.subject || '', 90)}
                                        </li>
                                    ))}
                                </ul>
                                {checkResult.incomingTotal > Math.min(10, incoming.length) && (
                                    <Hint>{t('update.incomingTruncated', { total: checkResult.incomingTotal, shown: Math.min(10, incoming.length) })}</Hint>
                                )}
                            </div>
                        )}
                    </>
                )}
            </SectionCard>

            <SectionCard
                title={t('update.jobTitle', null, '更新任务进度')}
                extra={job ? <Tag size="small" color={jobStateColor(job.state)}>{jobStateLabel(t, job.state)}</Tag> : null}
            >
                {restarting && <Hint>{t('update.restartRequestedToast')}</Hint>}
                {needsRestart && !busy && <Hint>{t('update.needsRestartToast')}</Hint>}
                {snapshot.buildPending && !busy && <Hint>{t('updateUi.resumeBuild')}</Hint>}
                {!job && <Text type="tertiary">{t('update.noJob', null, '暂无更新任务记录。')}</Text>}
                {job && (
                    <>
                        <KeyValueList
                            columns={3}
                            items={[
                                { label: t('updateUi.taskId', null, '任务编号'), value: <Mono>{shortCommit(job.id)}</Mono> },
                                { label: t('updateUi.kind', null, '任务类型'), value: job.kind ? t('updateUi.kinds.' + job.kind, null, job.kind) : '—' },
                                { label: t('updateUi.started', null, '开始时间'), value: job.started ? formatDateTime(job.started) : '—' },
                                { label: t('updateUi.finished', null, '结束时间'), value: job.finished ? formatDateTime(job.finished) : '—' },
                                { label: t('updateUi.restartRequested', null, '已请求重启'), value: yesNo(t, job.restartRequested) },
                                { label: t('updateUi.restartError', null, '重启错误'), value: job.restartError || '—' }
                            ]}
                        />
                        {Array.isArray(job.steps) && job.steps.length > 0 && (
                            <Timeline className="settings-update-steps">
                                {job.steps.map((step, index) => (
                                    <Timeline.Item key={`${step.phase}-${index}`}>
                                        <Text strong>{t('updateUi.phases.' + step.phase, null, step.phase)}</Text>
                                        <div><Text type="tertiary" size="small">{truncate(step.message || '', 160)}</Text></div>
                                        <Text type="tertiary" size="small">{step.at ? formatDateTime(step.at) : ''}</Text>
                                    </Timeline.Item>
                                ))}
                            </Timeline>
                        )}
                        {job.result && (
                            <KeyValueList
                                columns={3}
                                items={[
                                    { label: t('updateUi.fromCommit', null, '更新前提交'), value: <Mono>{shortCommit(job.result.fromCommit)}</Mono> },
                                    { label: t('updateUi.toCommit', null, '更新后提交'), value: <Mono>{shortCommit(job.result.toCommit)}</Mono> },
                                    { label: t('update.resultCommits', null, '并入提交数'), value: formatNumber(job.result.commits) },
                                    { label: t('update.resultFiles', null, '改动文件数'), value: formatNumber(job.result.filesTouched) },
                                    { label: t('updateUi.keptContent', null, '保留的本机文件数'), value: formatNumber((job.result.keptContent || []).length) },
                                    { label: t('updateUi.overwritten', null, '覆盖的文件数'), value: formatNumber((job.result.overwritten || []).length) }
                                ]}
                            />
                        )}
                        {job.failure && (
                            <Banner
                                type="danger"
                                bordered
                                closeIcon={null}
                                description={(
                                    <div>
                                        <div>{job.failure.message || job.failure.reason}</div>
                                        {(job.failure.items || []).length > 0 && (
                                            <ul>
                                                {job.failure.items.slice(0, 6).map(item => <li key={item}>{item}</li>)}
                                            </ul>
                                        )}
                                    </div>
                                )}
                            />
                        )}
                    </>
                )}
            </SectionCard>
        </>
    );
}
