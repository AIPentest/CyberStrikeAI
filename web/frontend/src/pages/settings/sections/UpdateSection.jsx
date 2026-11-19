/**
 * 一键更新（只读 + 检查更新）：GET /system/update、GET /system/update/job、
 * POST /system/update/check。应用更新/重启/回滚属于本页面之外的运维动作，这里只展示状态。
 */
import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Space, Tag, Timeline, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useInterval, useResource } from '../../../components/hooks';
import { ErrorBanner, KeyValueList, SectionCard, toastError, toastSuccess } from '../../../components/ui';
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

function jobStateColor(state) {
    if (state === 'succeeded') return 'green';
    if (state === 'failed') return 'red';
    if (state === 'running') return 'blue';
    return 'grey';
}

export default function UpdateSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('update:read');
    const canCheck = hasPermission('update:apply');

    const consoleRes = useResource(() => api.get('/system/update'), [canRead], { skip: !canRead });
    const [job, setJob] = useState(null);
    const [checking, setChecking] = useState(false);
    const [checkResult, setCheckResult] = useState(null);
    const [checkError, setCheckError] = useState(null);

    useEffect(() => {
        if (consoleRes.data) setJob(consoleRes.data.job || null);
    }, [consoleRes.data]);

    const pollJob = useCallback(async () => {
        try {
            const payload = await api.get('/system/update/job');
            if (payload?.job) setJob(payload.job);
        } catch (error) {
            /* 轮询失败不打断页面，下一次继续 */
        }
    }, []);

    const running = job?.state === 'running';
    useInterval(pollJob, running ? 2000 : 0);

    const check = async () => {
        if (!canCheck) return;
        setChecking(true);
        setCheckError(null);
        setCheckResult(null);
        try {
            const payload = await api.post('/system/update/check', {});
            setCheckResult(payload?.status || null);
            toastSuccess('已获取远端版本信息');
        } catch (error) {
            setCheckError(error);
            toastError(error, t('update.checkFailed', null, '检查更新失败'));
        } finally {
            setChecking(false);
        }
    };

    if (!canRead) {
        return <PermissionBanner permission="update:read" label={t('settings.nav.update', null, '读取安装树版本')} />;
    }
    if (consoleRes.loading && !consoleRes.data) return <LoadingBlock rows={6} />;

    const snapshot = checkResult || consoleRes.data?.status || {};
    const source = consoleRes.data?.source || {};
    const needsRestart = consoleRes.data?.needsRestart;
    const binaryBuiltAt = consoleRes.data?.binaryBuiltAt;

    const facts = [
        { label: t('update.root', null, '安装目录'), value: <Mono>{snapshot.root || '—'}</Mono> },
        { label: t('update.gitTree', null, 'Git 安装树'), value: yesNo(t, snapshot.installed) },
        { label: t('update.branch', null, '分支'), value: snapshot.branch || '—' },
        { label: 'repo', value: <Mono>{source.repo || source.defaultRepo || snapshot.repo || '—'}</Mono> },
        { label: t('update.commit', null, '当前提交'), value: <Mono>{shortCommit(snapshot.commit)}</Mono> },
        { label: t('update.subject', null, '提交说明'), value: truncate(snapshot.subject || '—', 80) },
        { label: t('update.committedAt', null, '提交时间'), value: snapshot.committedAt ? formatDateTime(snapshot.committedAt) : '—' },
        { label: t('update.runningBuild', null, '运行构建'), value: consoleRes.data?.buildRevision ? shortCommit(consoleRes.data.buildRevision) : '—' },
        { label: 'Go 工具链', value: yesNo(t, snapshot.canBuild) },
        { label: '已构建二进制', value: yesNo(t, snapshot.hasBinary) },
        { label: 'buildPending', value: yesNo(t, snapshot.buildPending) },
        {
            label: t('update.restartPending', null, '需重启激活'),
            value: needsRestart
                ? <Space spacing={4}><Tag size="small" color="amber">{t('common.yes', null, '是')}</Tag>
                    {binaryBuiltAt && <Text type="tertiary" size="small">{formatDateTime(binaryBuiltAt)}</Text>}</Space>
                : <Tag size="small" color="green">{t('common.no', null, '否')}</Tag>
        },
        { label: 'localChanges', value: formatNumber((snapshot.localChanges || []).length) },
        { label: 'blockingChanges', value: formatNumber((snapshot.blockingChanges || []).length) }
    ];

    const incoming = Array.isArray(snapshot.incoming) ? snapshot.incoming : [];

    return (
        <>
            <ErrorBanner error={consoleRes.error} onRetry={consoleRes.reload} />
            <SectionCard
                title={t('update.installTitle', null, '安装信息')}
                description={t('update.intro', null, '这一页只读取安装树版本与更新任务进度；应用更新、重启、回滚不在本页触发。')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={consoleRes.reload} loading={consoleRes.loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button
                            theme="solid"
                            type="primary"
                            icon={<IconSearch />}
                            loading={checking}
                            disabled={!canCheck}
                            onClick={check}
                        >
                            {t('update.checkBtn', null, '检查更新')}
                        </Button>
                    </Space>
                )}
            >
                {!canCheck && (
                    <Hint>{t('update.checkPermissionHint', null, '检查更新会访问远端仓库，需要 update:apply 权限。')}</Hint>
                )}
                <KeyValueList items={facts} columns={3} />
                {snapshot.goToolchain && (
                    <Hint>Go 工具链路径：<Mono>{snapshot.goToolchain}</Mono></Hint>
                )}
            </SectionCard>

            <SectionCard title={t('update.remoteTitle', null, '远端版本')}>
                <ErrorBanner error={checkError} onRetry={check} />
                {!checkResult && !checkError && (
                    <Text type="tertiary">尚未检查远端版本（GET /system/update 不联网）。</Text>
                )}
                {checkResult && (
                    <>
                        {checkResult.checkError ? (
                            <Banner
                                type="warning"
                                bordered
                                closeIcon={null}
                                description={`${t('update.checkFailed', null, '检查更新失败')}：${checkResult.checkError}`}
                            />
                        ) : (
                            <Space wrap spacing={8}>
                                <Tag size="small" color={checkResult.updateAvailable ? 'amber' : 'green'}>
                                    {checkResult.updateAvailable
                                        ? t('update.behindCount', { count: checkResult.behind }, `落后 ${checkResult.behind} 个提交`)
                                        : t('update.upToDate', null, '已是最新')}
                                </Tag>
                                {checkResult.ahead > 0 && (
                                    <Tag size="small" color="blue">{t('update.aheadCount', { count: checkResult.ahead }, `领先 ${checkResult.ahead} 个提交`)}</Tag>
                                )}
                                {checkResult.diverged && <Tag size="small" color="red">diverged</Tag>}
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
                                {checkResult.incomingTotal > incoming.length && (
                                    <Hint>{t('update.incomingTruncated', { count: checkResult.incomingTotal }, `共 ${checkResult.incomingTotal} 个提交，仅显示前 ${incoming.length} 个`)}</Hint>
                                )}
                            </div>
                        )}
                    </>
                )}
            </SectionCard>

            <SectionCard
                title={t('update.jobTitle', null, '更新任务进度')}
                extra={job ? <Tag size="small" color={jobStateColor(job.state)}>{job.state}</Tag> : null}
            >
                {!job && <Text type="tertiary">{t('update.noJob', null, '暂无更新任务记录。')}</Text>}
                {job && (
                    <>
                        <KeyValueList
                            columns={3}
                            items={[
                                { label: 'id', value: <Mono>{shortCommit(job.id)}</Mono> },
                                { label: 'kind', value: job.kind || '—' },
                                { label: 'started', value: job.started ? formatDateTime(job.started) : '—' },
                                { label: 'finished', value: job.finished ? formatDateTime(job.finished) : '—' },
                                { label: 'restartRequested', value: yesNo(t, job.restartRequested) },
                                { label: 'restartError', value: job.restartError || '—' }
                            ]}
                        />
                        {Array.isArray(job.steps) && job.steps.length > 0 && (
                            <Timeline className="settings-update-steps">
                                {job.steps.map((step, index) => (
                                    <Timeline.Item key={`${step.phase}-${index}`}>
                                        <Text strong>{step.phase}</Text>
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
                                    { label: 'fromCommit', value: <Mono>{shortCommit(job.result.fromCommit)}</Mono> },
                                    { label: 'toCommit', value: <Mono>{shortCommit(job.result.toCommit)}</Mono> },
                                    { label: 'commits', value: formatNumber(job.result.commits) },
                                    { label: 'filesTouched', value: formatNumber(job.result.filesTouched) },
                                    { label: 'keptContent', value: formatNumber((job.result.keptContent || []).length) },
                                    { label: 'overwritten', value: formatNumber((job.result.overwritten || []).length) }
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
