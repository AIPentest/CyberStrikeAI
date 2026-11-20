/**
 * 存储清理：GET /storage/status、GET /storage/meta、POST /storage/cleanup（预览/执行）、
 * PUT /config（storage 段）保存保留策略。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Banner, Button, Input, Progress, Space, Switch, Tooltip, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../../../lib/http';
import { useAuth } from '../../../lib/auth';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../components/hooks';
import {
    DataTable, ErrorBanner, KeyValueList, SectionCard, confirmAction, toastError, toastSuccess
} from '../../../components/ui';
import { formatBytes, formatDateTime, formatNumber } from '../../../lib/format';
import { Hint, LoadingBlock, Mono, PermissionBanner } from './common';

const { Text } = Typography;

function intOr(raw, fallback) {
    const value = parseInt(raw, 10);
    return Number.isFinite(value) && value >= 0 ? value : fallback;
}

function categoryLabel(t, item) {
    return t(`settingsStorage.cat.${item.key}`, null, item.label || item.key);
}

function categoryHint(t, item) {
    return t(`settingsStorage.catHint.${item.key}`, null, item.hint || '');
}

export default function StorageSection() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('storage:read');
    const canClean = hasPermission('storage:write');
    const canSavePolicy = hasPermission('config:write');

    const meta = useResource(() => api.get('/storage/meta'), [canRead], { skip: !canRead });
    const status = useResource(() => api.get('/storage/status'), [canRead], { skip: !canRead });

    const [policy, setPolicy] = useState(null);
    const [busy, setBusy] = useState('');
    const [actionError, setActionError] = useState(null);
    const [report, setReport] = useState(null);

    useEffect(() => {
        if (!meta.data) return;
        const categories = {};
        for (const item of meta.data.categories || []) {
            categories[item.key] = { enabled: Boolean(item.enabled), retention_days: Number(item.retention_days) || 0 };
        }
        setPolicy({
            auto_clean: Boolean(meta.data.auto_clean),
            interval_minutes: Number(meta.data.interval_minutes) || 0,
            orphan_grace_days: Number(meta.data.orphan_grace_days) || 0,
            active_grace_hours: Number(meta.data.active_grace_hours) || 0,
            categories
        });
    }, [meta.data]);

    const refreshStatus = useCallback(async () => {
        if (!canRead) return;
        setBusy('status');
        setActionError(null);
        try {
            const fresh = await api.get('/storage/status', { refresh: 1 });
            status.setData(fresh);
        } catch (error) {
            setActionError(error);
        } finally {
            setBusy('');
        }
    }, [canRead, status.setData]);

    const savePolicy = async () => {
        if (!canSavePolicy || !policy) return;
        setBusy('policy');
        setActionError(null);
        const categories = {};
        for (const [key, value] of Object.entries(policy.categories || {})) {
            categories[key] = { enabled: Boolean(value.enabled), retention_days: intOr(value.retention_days, 0) };
        }
        const payload = {
            auto_clean: Boolean(policy.auto_clean),
            interval_minutes: intOr(policy.interval_minutes, 5),
            orphan_grace_days: intOr(policy.orphan_grace_days, 0),
            active_grace_hours: intOr(policy.active_grace_hours, 1),
            categories
        };
        try {
            await api.put('/config', { storage: payload });
            toastSuccess(t('settingsStorage.saved', null, '清理策略已保存'));
            await meta.reload();
        } catch (error) {
            setActionError(error);
            toastError(error, t('settingsStorage.saveFailed', null, '保存失败'));
        } finally {
            setBusy('');
        }
    };

    const runCleanup = async (dryRun) => {
        if (!canClean) return;
        setBusy(dryRun ? 'preview' : 'clean');
        setActionError(null);
        try {
            const payload = dryRun
                ? await api.post('/storage/cleanup', { dry_run: true })
                : await api.post('/storage/cleanup', { dry_run: false, confirm: true });
            setReport(payload);
            if (dryRun) toastSuccess(t('settingsStorage.previewTitle', null, '预览完成（未删除任何文件）'));
            else {
                toastSuccess(t('settingsStorage.cleanDone', null, '清理完成'));
                const fresh = await api.get('/storage/status', { refresh: 1 });
                status.setData(fresh);
            }
        } catch (error) {
            setActionError(error);
            toastError(error, t('settingsStorage.cleanupFailed', null, '清理请求失败'));
        } finally {
            setBusy('');
        }
    };

    const totals = status.data?.totals || {};
    const filesystem = status.data?.filesystem || {};

    const overview = useMemo(() => ([
        {
            label: t('settingsStorage.diskUsed', null, '磁盘已用'),
            value: filesystem.available ? formatBytes(filesystem.used_bytes) : t('settingsStorage.unavailable', null, '不可用')
        },
        {
            label: t('settingsStorage.runtimeTotal', null, '运行空间占用'),
            value: `${formatBytes(totals.bytes)}（${formatNumber(totals.units)} ${t('settingsStorage.itemsUnit', null, '项')}）`
        },
        {
            label: t('settingsStorage.reclaimable', null, '可回收'),
            value: `${formatBytes(totals.reclaimable_bytes)}（${formatNumber(totals.reclaimable_units)} ${t('settingsStorage.itemsUnit', null, '项')}）`
        },
        {
            label: t('settingsStorage.inodes', null, 'inode 使用'),
            value: filesystem.available && filesystem.inodes_total > 0
                ? `${formatNumber(filesystem.inodes_total - filesystem.inodes_free)} / ${formatNumber(filesystem.inodes_total)}`
                : t('settingsStorage.unavailable', null, '不可用')
        }
    ]), [filesystem, totals, t]);

    if (!canRead) {
        return <PermissionBanner permission="storage:read" label={t('settings.nav.storage', null, '读取存储状态')} />;
    }
    if ((meta.loading && !meta.data) || (status.loading && !status.data)) return <LoadingBlock rows={6} />;

    const categoryRows = (meta.data?.categories || []).map(item => {
        const stat = (status.data?.categories || []).find(entry => entry.key === item.key) || {};
        return { ...item, stat, key: item.key };
    });

    const columns = [
        {
            title: t('settingsStorage.colCategory', null, '类别'),
            dataIndex: 'label',
            width: 200,
            render: (_, row) => (
                <Tooltip content={categoryHint(t, row)} position="topLeft">
                    <Space vertical spacing={2} align="start">
                        <Text strong>{categoryLabel(t, row)}</Text>
                        {row.stat.missing && <Text type="tertiary" size="small">{t('settingsStorage.unused', null, '未使用')}</Text>}
                    </Space>
                </Tooltip>
            )
        },
        {
            title: t('settingsStorage.colRoot', null, '目录'),
            dataIndex: 'key',
            render: (_, row) => (
                <Tooltip content={row.stat.root || ''} position="topLeft">
                    <Mono>{row.stat.missing ? '—' : (row.stat.root || '—')}</Mono>
                </Tooltip>
            )
        },
        {
            title: t('settingsStorage.colUnits', null, '项目数'),
            align: 'right',
            width: 100,
            render: (_, row) => (row.stat.missing ? '—' : formatNumber(row.stat.units))
        },
        {
            title: t('settingsStorage.colSize', null, '占用'),
            align: 'right',
            width: 110,
            render: (_, row) => (row.stat.missing ? '—' : formatBytes(row.stat.bytes))
        },
        {
            title: t('settingsStorage.colReclaimable', null, '可回收'),
            align: 'right',
            width: 190,
            render: (_, row) => (row.stat.missing ? '—' : (
                <Space vertical spacing={2} align="end">
                    <Text strong>{formatBytes(row.stat.reclaimable_bytes)}</Text>
                    <Text type="tertiary" size="small">
                        {`${formatNumber(row.stat.reclaimable_units)} ${t('settingsStorage.itemsUnit', null, '项')}`}
                        {row.stat.orphan_units ? ` · ${t('settingsStorage.orphanSuffix', null, '含孤儿')} ${row.stat.orphan_units}` : ''}
                        {row.stat.skipped_active ? ` · ${t('settingsStorage.skippedActive', null, '跳过活跃')} ${row.stat.skipped_active}` : ''}
                    </Text>
                </Space>
            ))
        },
        {
            title: t('settingsStorage.colRetention', null, '保留天数'),
            width: 130,
            render: (_, row) => (
                <Input
                    value={String(policy?.categories?.[row.key]?.retention_days ?? 0)}
                    disabled={!canSavePolicy || busy === 'policy'}
                    onChange={value => setPolicy(current => current && ({
                        ...current,
                        categories: {
                            ...current.categories,
                            [row.key]: {
                                ...(current.categories[row.key] || {}),
                                retention_days: value === '' ? '' : intOr(value, 0)
                            }
                        }
                    }))}
                    style={{ width: 96 }}
                />
            )
        },
        {
            title: t('settingsStorage.colEnabled', null, '启用'),
            width: 90,
            render: (_, row) => (
                <Switch
                    size="small"
                    checked={Boolean(policy?.categories?.[row.key]?.enabled)}
                    disabled={!canSavePolicy || busy === 'policy'}
                    onChange={next => setPolicy(current => current && ({
                        ...current,
                        categories: {
                            ...current.categories,
                            [row.key]: { ...(current.categories[row.key] || {}), enabled: next }
                        }
                    }))}
                    aria-label={t('settingsStorage.colEnabled', null, '启用')}
                />
            )
        }
    ];

    return (
        <>
            <ErrorBanner error={meta.error} onRetry={meta.reload} />
            <ErrorBanner error={status.error} onRetry={status.reload} />
            <ErrorBanner error={actionError} />

            <SectionCard
                title={t('settingsStorage.title', null, '存储清理')}
                description={t('settingsStorage.subtitle', null, '统计并清理运行期间产生的磁盘垃圾。删除不可逆，建议先预览再执行。')}
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} loading={busy === 'status'} onClick={refreshStatus}>
                            {t('settingsStorage.refresh', null, '重新扫描')}
                        </Button>
                        <Button
                            loading={busy === 'preview'}
                            disabled={!canClean}
                            onClick={() => runCleanup(true)}
                        >
                            {t('settingsStorage.preview', null, '预览可清理项')}
                        </Button>
                        <Button
                            type="danger"
                            theme="light"
                            loading={busy === 'clean'}
                            disabled={!canClean}
                            onClick={() => confirmAction({
                                title: t('settingsStorage.cleanNow', null, '立即清理'),
                                content: t(
                                    'settingsStorage.confirmClean',
                                    { size: formatBytes(totals.reclaimable_bytes), count: formatNumber(totals.reclaimable_units) },
                                    `将永久删除约 ${formatBytes(totals.reclaimable_bytes)}（${formatNumber(totals.reclaimable_units)} 项）运行空间文件，无法恢复。确认继续？`
                                ),
                                okText: t('settingsStorage.cleanNow', null, '立即清理'),
                                onOk: () => runCleanup(false)
                            })}
                        >
                            {t('settingsStorage.cleanNow', null, '立即清理')}
                        </Button>
                    </Space>
                )}
            >
                {filesystem.available && (
                    <div className="settings-disk">
                        <Progress
                            percent={Math.max(0, Math.min(100, Number(filesystem.used_percent) || 0))}
                            stroke={Number(filesystem.used_percent) >= 85 ? 'var(--semi-color-danger)' : undefined}
                            showInfo
                        />
                        <Hint>
                            {`${formatBytes(filesystem.free_bytes)} ${t('settingsStorage.free', null, '可用')} / ${formatBytes(filesystem.total_bytes)}`}
                            {status.data?.scanned_at
                                ? ` · ${t('settingsStorage.scannedAt', null, '扫描于')} ${formatDateTime(status.data.scanned_at)}`
                                : ''}
                        </Hint>
                    </div>
                )}
                <KeyValueList items={overview} columns={4} />
            </SectionCard>

            <SectionCard
                title={t('settingsStorage.policyTitle', null, '自动清理策略')}
                extra={(
                    <Button
                        theme="solid"
                        type="primary"
                        loading={busy === 'policy'}
                        disabled={!canSavePolicy || !policy}
                        onClick={savePolicy}
                    >
                        {t('settingsStorage.save', null, '保存策略')}
                    </Button>
                )}
            >
                <div className="settings-policy-grid">
                    <label className="settings-field">
                        <Text type="tertiary" size="small">{t('settingsStorage.autoClean', null, '开启后台定时清理')}</Text>
                        <Switch
                            checked={Boolean(policy?.auto_clean)}
                            disabled={!canSavePolicy}
                            onChange={next => setPolicy(current => current && ({ ...current, auto_clean: next }))}
                        />
                    </label>
                    <label className="settings-field">
                        <Text type="tertiary" size="small">{t('settingsStorage.intervalMinutes', null, '清理间隔（分钟）')}</Text>
                        <Input
                            value={String(policy?.interval_minutes ?? '')}
                            disabled={!canSavePolicy}
                            onChange={value => setPolicy(current => current && ({ ...current, interval_minutes: value === '' ? '' : intOr(value, 5) }))}
                        />
                    </label>
                    <label className="settings-field">
                        <Text type="tertiary" size="small">{t('settingsStorage.orphanGraceDays', null, '孤儿目录宽限（天）')}</Text>
                        <Input
                            value={String(policy?.orphan_grace_days ?? '')}
                            disabled={!canSavePolicy}
                            onChange={value => setPolicy(current => current && ({ ...current, orphan_grace_days: value === '' ? '' : intOr(value, 0) }))}
                        />
                    </label>
                    <label className="settings-field">
                        <Text type="tertiary" size="small">{t('settingsStorage.activeGraceHours', null, '活跃会话保护（小时）')}</Text>
                        <Input
                            value={String(policy?.active_grace_hours ?? '')}
                            disabled={!canSavePolicy}
                            onChange={value => setPolicy(current => current && ({ ...current, active_grace_hours: value === '' ? '' : intOr(value, 1) }))}
                        />
                    </label>
                </div>
                <Hint>{t('settingsStorage.policyHint', null, '保留天数为 0 表示不按保留期清理，但孤儿目录仍会回收；最近有活动的会话不会被清理。')}</Hint>
            </SectionCard>

            <SectionCard title={t('settingsStorage.categoriesTitle', null, '分类占用与保留策略')}>
                <DataTable
                    columns={columns}
                    dataSource={categoryRows}
                    rowKey="key"
                    loading={status.loading || meta.loading}
                    pagination={false}
                    emptyText={t('settingsStorage.noCategories', null, '暂无可清理类别')}
                />
            </SectionCard>

            {report && (
                <SectionCard
                    title={report.dry_run
                        ? t('settingsStorage.previewTitle', null, '预览结果（未删除任何文件）')
                        : t('settingsStorage.cleanDone', null, '清理完成')}
                >
                    <KeyValueList
                        columns={3}
                        items={[
                            { label: t('settingsStorage.colUnits', null, '项目数'), value: formatNumber(report.dry_run ? report.totals?.reclaimable_units : report.totals?.removed_units) },
                            { label: report.dry_run ? t('settingsStorage.reclaimable', null, '可回收') : t('settingsDisplay.freed', null, '已释放空间'), value: formatBytes(report.dry_run ? report.totals?.reclaimable_bytes : report.totals?.freed_bytes) },
                            { label: t('settingsDisplay.errors', null, '错误数'), value: formatNumber(report.totals?.errors) },
                            { label: t('settingsStorage.skippedActive', null, '跳过活跃'), value: formatNumber(report.totals?.skipped_active) },
                            { label: t('settingsDisplay.cleanupDuration', null, '清理耗时'),
                                value: t('settingsProbe.milliseconds', { value: formatNumber(report.duration_ms ?? 0) }, `${formatNumber(report.duration_ms ?? 0)} 毫秒`) }
                        ]}
                    />
                    {(report.categories || []).filter(item => item.errors?.length).length > 0 && (
                        <Banner
                            type="warning"
                            bordered
                            closeIcon={null}
                            description={(report.categories || [])
                                .filter(item => item.errors?.length)
                                .map(item => `${categoryLabel(t, item)}：${item.errors.slice(0, 3).join('；')}`)
                                .join(' / ')}
                        />
                    )}
                </SectionCard>
            )}
        </>
    );
}
