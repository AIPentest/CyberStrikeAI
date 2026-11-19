/**
 * C2 事件审计：按级别/类别/会话/时间窗筛选，5 秒轮询并支持批量删除。
 */
import { useMemo, useState } from 'react';
import { Button, Input, Select, SideSheet, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useInterval, useResource } from '../../components/hooks';
import {
    PageHeader, SectionCard, StatGrid, DataTable, ErrorBanner, JsonViewer,
    confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import * as c2 from '../c2-sessions/components/c2-api';
import {
    EVENT_CATEGORY_OPTIONS, EVENT_LEVEL_COLOR, EVENT_LEVEL_OPTIONS, TimePresetBar,
    TimeText, presetToSince
} from '../c2-sessions/components/c2-shared';
import './page.css';

const { Text } = Typography;

export default function C2EventsPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canDelete = hasPermission('c2:delete') || hasPermission('c2:write');

    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [level, setLevel] = useState('');
    const [category, setCategory] = useState('');
    const [preset, setPreset] = useState('24h');
    const [sessionInput, setSessionInput] = useState('');
    const [sessionFilter, setSessionFilter] = useState('');
    const [selectedKeys, setSelectedKeys] = useState([]);
    const [detail, setDetail] = useState(null);

    const query = useMemo(() => ({
        page,
        page_size: pageSize,
        level: level || undefined,
        category: category || undefined,
        session_id: sessionFilter || undefined,
        since: presetToSince(preset) || undefined
    }), [page, pageSize, level, category, sessionFilter, preset]);

    const { data, loading, error, reload } = useResource(() => c2.listEvents(query), [query]);
    const events = Array.isArray(data?.events) ? data.events : [];
    const total = Number(data?.total ?? events.length);
    const counts = data?.level_counts || {};

    useInterval(() => reload({ silent: true }), 5000);

    const applyFilters = () => {
        setSessionFilter(sessionInput.trim());
        setPage(1);
        setSelectedKeys([]);
    };

    const resetFilters = () => {
        setLevel('');
        setCategory('');
        setPreset('24h');
        setSessionInput('');
        setSessionFilter('');
        setPage(1);
        setSelectedKeys([]);
    };

    const metrics = [
        { id: 'total', label: t('c2.events.statTotal', null, '当前筛选'), value: total },
        { id: 'info', label: t('c2.events.info', null, '信息'), value: counts.info || 0 },
        { id: 'warn', label: t('c2.events.warn', null, '警告'), value: counts.warn || 0, tone: 'warning' },
        { id: 'critical', label: t('c2.events.critical', null, '严重'), value: counts.critical || 0, tone: 'danger' }
    ];

    const remove = ids => {
        confirmAction({
            title: t('c2.events.batchDelete', null, '删除事件'),
            content: ids.length > 1
                ? t('c2.events.confirmBatchDelete', null, '确定删除选中的事件吗？此操作不可恢复。').replace('{{n}}', String(ids.length))
                : t('c2.events.confirmDeleteOne', null, '确定删除该条事件吗？此操作不可恢复。'),
            onOk: async () => {
                try {
                    const result = await c2.deleteEvents(ids);
                    toastSuccess(t('c2.events.toastDeleted', null, '已删除').replace('{{n}}', String(result?.deleted ?? ids.length)));
                    setSelectedKeys(current => current.filter(key => !ids.includes(key)));
                    reload();
                } catch (err) {
                    toastError(err);
                }
            }
        });
    };

    const columns = [
        {
            title: t('c2.events.level', null, '级别'),
            dataIndex: 'level',
            width: 90,
            render: value => <Tag size="small" color={EVENT_LEVEL_COLOR[value] || 'grey'}>{t(`c2.events.${value || 'info'}`, null, value || '—')}</Tag>
        },
        { title: t('c2.events.category', null, '类别'), dataIndex: 'category', width: 110 },
        {
            title: t('c2.events.message', null, '消息'),
            dataIndex: 'message',
            render: (value, record) => (
                <button type="button" className="c2-event-message" onClick={() => setDetail(record)} title={value}>
                    {value || '—'}
                </button>
            )
        },
        {
            title: t('c2.events.colSession', null, '会话'),
            dataIndex: 'sessionId',
            width: 150,
            render: value => <span className="c2-mono">{value || '—'}</span>
        },
        {
            title: t('c2.events.colTask', null, '任务'),
            dataIndex: 'taskId',
            width: 150,
            render: value => <span className="c2-mono">{value || '—'}</span>
        },
        { title: t('c2.events.time', null, '时间'), dataIndex: 'createdAt', width: 130, render: value => <TimeText value={value} /> },
        {
            title: t('c2.tasks.colActions', null, '操作'),
            dataIndex: 'actions',
            width: 130,
            render: (_, record) => (
                <Space spacing={4}>
                    <Button size="small" theme="borderless" onClick={() => setDetail(record)}>{t('c2.events.view', null, '查看')}</Button>
                    <Button size="small" theme="borderless" type="danger" disabled={!canDelete} onClick={() => remove([record.id])}>
                        {t('c2.events.deleteOne', null, '删除')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <div className="c2-page" id="page-c2-events">
            <PageHeader
                title={t('c2.events.title', null, '事件审计')}
                description={t('c2.events.pageDesc', null, '监听器、会话、任务与 OPSEC 事件留痕，5 秒自动刷新')}
                extra={[
                    <Button key="refresh" icon={<IconRefresh />} onClick={() => reload()} loading={loading}>{t('c2.events.refresh', null, '刷新')}</Button>,
                    <Button
                        key="delete"
                        type="danger"
                        disabled={!canDelete || !selectedKeys.length}
                        onClick={() => remove(selectedKeys)}
                    >
                        {t('c2.events.batchDelete', null, '批量删除')}
                    </Button>
                ]}
            />

            <StatGrid metrics={metrics} columns={4} />
            <ErrorBanner error={error} onRetry={() => reload()} />

            <SectionCard
                title={t('c2.events.filterTitle', null, '筛选')}
                extra={<TimePresetBar value={preset} onChange={value => { setPreset(value); setPage(1); }} ns="events" />}
            >
                <div className="c2-toolbar-fields">
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.events.level', null, '级别')}</span>
                        <Select
                            value={level}
                            onChange={value => { setLevel(value || ''); setPage(1); }}
                            style={{ width: 150 }}
                            optionList={[{ value: '', label: t('c2.events.filterAllLevels', null, '全部级别') }, ...EVENT_LEVEL_OPTIONS]}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.events.category', null, '类别')}</span>
                        <Select
                            value={category}
                            onChange={value => { setCategory(value || ''); setPage(1); }}
                            style={{ width: 150 }}
                            optionList={[{ value: '', label: t('c2.events.filterAllCategories', null, '全部类别') }, ...EVENT_CATEGORY_OPTIONS]}
                        />
                    </div>
                    <div className="c2-field">
                        <span className="c2-field-label">{t('c2.events.filterSession', null, '会话 ID')}</span>
                        <Input
                            value={sessionInput}
                            onChange={setSessionInput}
                            onEnterPress={applyFilters}
                            placeholder={t('c2.events.filterSessionPlaceholder', null, 's_xxxxxxxx')}
                            style={{ width: 190 }}
                        />
                    </div>
                    <Space spacing={8}>
                        <Button theme="solid" type="primary" onClick={applyFilters}>{t('c2.events.applyFilters', null, '应用筛选')}</Button>
                        <Button onClick={resetFilters}>{t('c2.events.resetFilters', null, '重置')}</Button>
                    </Space>
                </div>
            </SectionCard>

            <SectionCard title={t('c2.events.listTitle', null, '事件列表')}>
                <DataTable
                    columns={columns}
                    dataSource={events}
                    rowKey="id"
                    loading={loading}
                    onRefresh={() => reload()}
                    rowSelection={{
                        selectedRowKeys: selectedKeys,
                        onChange: keys => setSelectedKeys(keys)
                    }}
                    pagination={{
                        currentPage: page,
                        pageSize,
                        total,
                        onPageChange: next => setPage(next),
                        onPageSizeChange: size => { setPageSize(size); setPage(1); }
                    }}
                    emptyText={t('c2.events.empty', null, '暂无事件')}
                />
            </SectionCard>

            <SideSheet
                visible={Boolean(detail)}
                title={t('c2.events.detailTitle', null, '事件详情')}
                width={560}
                onCancel={() => setDetail(null)}
            >
                {detail && (
                    <div className="c2-event-detail">
                        <div className="c2-event-detail-head">
                            <Tag size="small" color={EVENT_LEVEL_COLOR[detail.level] || 'grey'}>{detail.level}</Tag>
                            <Tag size="small">{detail.category}</Tag>
                            <Text type="tertiary" size="small">{formatDateTime(detail.createdAt)}</Text>
                        </div>
                        <Text>{detail.message}</Text>
                        <div className="c2-field-label">{t('c2.events.colId', null, '事件 ID')}</div>
                        <Text className="c2-mono" copyable={{ text: detail.id }}>{detail.id}</Text>
                        {detail.sessionId && (
                            <>
                                <div className="c2-field-label">{t('c2.events.colSession', null, '会话')}</div>
                                <Text className="c2-mono">{detail.sessionId}</Text>
                            </>
                        )}
                        {detail.data && Object.keys(detail.data).length > 0 && (
                            <>
                                <div className="c2-field-label">{t('c2.events.detailData', null, '附加数据')}</div>
                                <JsonViewer value={detail.data} />
                            </>
                        )}
                    </div>
                )}
            </SideSheet>
        </div>
    );
}
