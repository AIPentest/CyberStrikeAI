/** 信息收集：FOFA/ZoomEye/Quake/Shodan 检索、分页、结果入库、自然语言解析。 */
import { useEffect, useMemo, useState } from 'react';
import {
    Banner, Button, Empty, Input, InputNumber, Pagination, Select, SideSheet, Space,
    Switch, TextArea, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch, IconCloudUploadStroked } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import { useLocalStorage } from '../../components/hooks';
import {
    ErrorBanner, JsonViewer, KeyValueList, PageHeader, SectionCard,
    toastError, toastSuccess, toastWarning
} from '../../components/ui';
import FofaResultTable from './components/FofaResultTable';
import FofaParseModal from './components/FofaParseModal';
import {
    FOFA_RESULT_FIELD_LIMIT, INFO_PROVIDERS, PROVIDER_OPTIONS, defaultVisibleFields,
    inferTarget, providerConfig, rowsForIndexes
} from './components/providers';
import { importFofaRows } from '../asset-library/components/fofaImport';
import './page.css';

const { Text } = Typography;

const STORAGE_KEY = 'csf.info-collect.form';
const DEFAULT_FORM = {
    provider: 'fofa',
    query: '',
    size: 100,
    fields: INFO_PROVIDERS.fofa.defaultFields,
    full: false
};

export default function InfoCollectPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canSearch = hasPermission('fofa:execute');
    const canImport = hasPermission('asset:write');

    const [form, setForm] = useLocalStorage(STORAGE_KEY, DEFAULT_FORM);
    const [naturalText, setNaturalText] = useState('');
    const [parseOpen, setParseOpen] = useState(false);
    const [result, setResult] = useState(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [selectedKeys, setSelectedKeys] = useState([]);
    const [visibleFields, setVisibleFields] = useState([]);
    const [detailRow, setDetailRow] = useState(null);
    const [importing, setImporting] = useState(false);
    const [projects, setProjects] = useState([]);
    const [projectId, setProjectId] = useState('');
    const [collectionName, setCollectionName] = useState('');

    const config = providerConfig(form.provider);
    const providerLabel = config.label;

    useEffect(() => {
        if (!hasPermission('project:read')) return undefined;
        let cancelled = false;
        api.get('/projects', { limit: 500 })
            .then(payload => { if (!cancelled) setProjects(Array.isArray(payload?.projects) ? payload.projects : []); })
            .catch(() => { if (!cancelled) setProjects([]); });
        return () => { cancelled = true; };
    }, [hasPermission]);

    useEffect(() => {
        // provider 切换后字段模板与数量上限需要跟随，避免残留其他 provider 的字段。
        setVisibleFields([]);
        setSelectedKeys([]);
        setResult(null);
        setError(null);
    }, [form.provider]);

    const update = patch => setForm(prev => ({ ...DEFAULT_FORM, ...prev, ...patch }));

    const changeProvider = provider => {
        const next = providerConfig(provider);
        update({
            provider,
            fields: next.defaultFields,
            size: Math.min(Number(form.size || 100), next.maxSize),
            full: false
        });
    };

    const effectiveSize = Math.min(Math.max(Number(form.size || 100), 1), config.maxSize);

    const runSearch = async (page = 1, queryOverride) => {
        if (!canSearch) {
            toastWarning(t('infoCollect.permissionDenied', null, '当前账号没有 fofa:execute 权限'));
            return;
        }
        const query = String(queryOverride ?? form.query ?? '').trim();
        if (!query) {
            toastWarning(t('infoCollect.enterFofaQuery', null, '请输入查询语法'));
            return;
        }
        setLoading(true);
        setError(null);
        try {
            const payload = await api.post('/fofa/search', {
                provider: form.provider,
                query,
                size: effectiveSize,
                page,
                fields: form.fields,
                full: Boolean(form.full)
            });
            setResult(payload);
            setSelectedKeys([]);
            setVisibleFields(defaultVisibleFields(payload?.fields || [], FOFA_RESULT_FIELD_LIMIT));
        } catch (searchError) {
            setError(searchError);
            setResult(null);
        } finally {
            setLoading(false);
        }
    };

    const reset = () => {
        setForm({ ...DEFAULT_FORM });
        setResult(null);
        setError(null);
        setSelectedKeys([]);
        setVisibleFields([]);
        setNaturalText('');
    };

    const fields = Array.isArray(result?.fields) ? result.fields : [];
    const results = Array.isArray(result?.results) ? result.results : [];
    const total = Number(result?.total || 0);
    const pageSize = Number(result?.size || effectiveSize);
    const currentPage = Number(result?.page || 1);

    const importSelected = async () => {
        if (importing) return;
        if (!canImport) {
            toastWarning(t('infoCollect.noImportPermission', null, '当前账号没有 asset:write 权限'));
            return;
        }
        if (!selectedKeys.length) {
            toastWarning(t('infoCollect.selectRowsFirst', null, '请先选择需要入库的结果'));
            return;
        }
        const rows = rowsForIndexes(results, selectedKeys);
        setImporting(true);
        try {
            const outcome = await importFofaRows(rows, {
                fields,
                provider: result?.provider || form.provider,
                query: result?.query || form.query,
                projectId,
                collectionName
            });
            if (!outcome.assets.length) {
                toastWarning(t('assets.noValidImportTarget', null, '所选结果中没有可入库的有效资产目标'));
                return;
            }
            const message = outcome.invalidCount
                ? t('assets.importDoneWithInvalid', {
                    created: outcome.created, updated: outcome.updated, invalid: outcome.invalidCount
                }, `已新增 ${outcome.created} 条，更新 ${outcome.updated} 条，跳过 ${outcome.invalidCount} 条无有效目标的结果`)
                : t('assets.importDone', { created: outcome.created, updated: outcome.updated },
                    `已新增 ${outcome.created} 条，更新 ${outcome.updated} 条`);
            const saved = outcome.collection?.name
                ? `，已写入资产库，并记下检索结果「${outcome.collection.name}」`
                : '';
            toastSuccess(`${message}${saved}`);
            setSelectedKeys([]);
        } catch (importError) {
            toastError(importError, t('assets.importFailed', null, '资产入库失败'));
        } finally {
            setImporting(false);
        }
    };

    const applyParsedQuery = (query, run) => {
        update({ query });
        setParseOpen(false);
        if (run) runSearch(1, query);
    };

    const detailItems = useMemo(() => {
        if (!detailRow) return [];
        return fields.map(field => {
            const value = detailRow[field];
            const text = Array.isArray(value) ? value.join(', ') : (value == null ? '—' : String(value));
            return { label: field, value: text };
        });
    }, [detailRow, fields]);

    const metaText = result
        ? t('infoCollect.resultsMeta', {
            total, count: results.length, page: currentPage, size: pageSize
        }, `${providerLabel} · 共 ${total} 条 · 第 ${currentPage} 页 · 每页 ${pageSize} 条 · 返回 ${results.length} 条`)
        : '';

    return (
        <div className="info-collect-page">
            <PageHeader
                title={t('infoCollectPage.title', null, '信息收集')}
                description="使用 FOFA 等空间测绘语法检索资产，并把结果直接入库。"
                extra={(
                    <Space>
                        <Button icon={<IconRefresh />} onClick={() => { if (result) runSearch(currentPage); }}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button onClick={reset}>{t('infoCollectPage.reset', null, '重置')}</Button>
                    </Space>
                )}
            />
            {!canSearch && (
                <Banner
                    type="warning"
                    bordered
                    closeIcon={null}
                    className="info-collect-permission"
                    description={t('infoCollect.permissionDeniedHint', null, '当前账号没有 fofa:execute 权限：查询、列字段与入库操作不可用，请联系管理员授权。')}
                />
            )}
            <ErrorBanner error={error} onRetry={() => runSearch(currentPage)} />

            <SectionCard title={t('infoCollectPage.fofaQuerySyntax', null, '查询语法')}>
                <div className="info-collect-form">
                    <Space className="info-collect-form-row">
                        <Select
                            value={form.provider}
                            optionList={PROVIDER_OPTIONS}
                            onChange={changeProvider}
                            style={{ width: 150 }}
                        />
                        <span className="info-collect-size">
                            <Text type="tertiary" size="small">{t('infoCollectPage.returnCount', null, '返回数量')}</Text>
                            <InputNumber
                                value={effectiveSize}
                                min={1}
                                max={config.maxSize}
                                onChange={value => update({ size: value })}
                                style={{ width: 120 }}
                            />
                        </span>
                        <Switch
                            checked={Boolean(form.full)}
                            disabled={!config.supportsFull}
                            onChange={checked => update({ full: checked })}
                        />
                        <Text type="tertiary" size="small">full</Text>
                        <Button theme="solid" type="primary" icon={<IconSearch />} onClick={() => runSearch(1)} loading={loading} disabled={!canSearch}>
                            {t('infoCollectPage.confirm', null, '查询')}
                        </Button>
                        <Button
                            disabled={!naturalText.trim() || !canSearch}
                            onClick={() => setParseOpen(true)}
                            title={t('infoCollectPage.parseBtnTitle', null, '将自然语言解析为查询语法')}
                        >
                            {t('infoCollectPage.parseBtn', null, 'AI 解析')}
                        </Button>
                    </Space>

                    <TextArea
                        value={form.query}
                        rows={3}
                        placeholder={`${providerLabel}：${config.placeholder}`}
                        className="info-collect-query"
                        onChange={value => update({ query: value })}
                    />

                    <Space wrap className="info-collect-presets">
                        <Text type="tertiary" size="small">{t('infoCollectPage.fillExample', null, '示例')}：</Text>
                        {config.queryPresets.map(([label, query]) => (
                            <Button key={label} size="small" theme="borderless" onClick={() => update({ query })}>
                                {label}
                            </Button>
                        ))}
                    </Space>

                    <Space className="info-collect-form-row">
                        <Text type="tertiary" size="small">{t('infoCollectPage.returnFields', null, '返回字段')}</Text>
                        <Input
                            value={form.fields}
                            onChange={value => update({ fields: value })}
                            className="info-collect-fields"
                            style={{ width: 420 }}
                        />
                        {config.fieldPresets.map(([label, value]) => (
                            <Button key={label} size="small" theme="borderless" onClick={() => update({ fields: value })}>
                                {label}
                            </Button>
                        ))}
                    </Space>

                    <Space className="info-collect-form-row">
                        <Select
                            value={projectId}
                            optionList={[{ value: '', label: '不归入项目集合' }, ...projects.map(item => ({ value: item.id, label: item.name || item.id }))]}
                            placeholder="归入项目"
                            style={{ width: 220 }}
                            onChange={value => setProjectId(value || '')}
                        />
                        <Input
                            value={collectionName}
                            disabled={!projectId}
                            placeholder="集合名称，留空则使用查询语句"
                            style={{ width: 280 }}
                            onChange={setCollectionName}
                        />
                    </Space>

                    <TextArea
                        value={naturalText}
                        rows={2}
                        placeholder={t('infoCollectPage.nlPlaceholder', null, '自然语言：例如 找中国 Apache 站点，标题包含登录')}
                        className="info-collect-natural"
                        onChange={setNaturalText}
                    />
                </div>
            </SectionCard>

            <SectionCard
                className="info-collect-results"
                title={t('infoCollectPage.queryResults', null, '查询结果')}
                extra={(
                    <Space className="info-collect-results-actions" wrap>
                        <Text type="tertiary" size="small">{metaText}</Text>
                        <Select
                            multiple
                            value={visibleFields}
                            optionList={fields.map(field => ({ value: field, label: field }))}
                            disabled={!fields.length}
                            placeholder={t('infoCollectPage.showColumns', null, '显示字段')}
                            maxTagCount={FOFA_RESULT_FIELD_LIMIT}
                            style={{ minWidth: 220 }}
                            onChange={values => {
                                const next = (values || []).slice(0, FOFA_RESULT_FIELD_LIMIT);
                                if ((values || []).length > FOFA_RESULT_FIELD_LIMIT) {
                                    toastWarning(`最多显示 ${FOFA_RESULT_FIELD_LIMIT} 个字段`);
                                }
                                setVisibleFields(next);
                            }}
                        />
                        <Button
                            icon={<IconCloudUploadStroked />}
                            disabled={!canImport || !selectedKeys.length}
                            loading={importing}
                            onClick={importSelected}
                        >
                            {t('assets.importSelected', null, '入库所选')}
                        </Button>
                    </Space>
                )}
            >
                {result?.warning && (
                    <Banner
                        type="warning"
                        bordered
                        closeIcon={null}
                        className="info-collect-warning"
                        description={result.warning}
                    />
                )}
                {!result && !loading && !error ? (
                    <Empty
                        title={t('infoCollect.noResults', null, '暂无结果')}
                        description={canSearch ? '输入查询语法后点击“查询”。' : '当前账号缺少 fofa:execute 权限。'}
                        style={{ padding: 40 }}
                    />
                ) : (
                    <FofaResultTable
                        fields={fields}
                        results={results}
                        visibleFields={visibleFields}
                        selectedKeys={selectedKeys}
                        onSelectionChange={setSelectedKeys}
                        onOpenDetail={setDetailRow}
                        loading={loading}
                    />
                )}
                {result && total > pageSize && (
                    <div className="info-collect-pagination">
                        <Pagination
                            total={total}
                            pageSize={pageSize}
                            currentPage={currentPage}
                            onPageChange={next => runSearch(next)}
                            showTotal
                        />
                    </div>
                )}
                {result && (
                    <Text type="tertiary" size="small" className="info-collect-selected-meta">
                        {t('infoCollectPage.selectedRows', { count: selectedKeys.length }, `已选择 ${selectedKeys.length} 条`)}
                    </Text>
                )}
            </SectionCard>

            <SideSheet
                visible={Boolean(detailRow)}
                onCancel={() => setDetailRow(null)}
                title={t('infoCollect.detailTitle', null, '结果详情')}
                width={600}
                placement="right"
            >
                {detailRow && (
                    <div className="info-collect-detail">
                        <Text strong>{inferTarget(detailRow, fields) || '—'}</Text>
                        <KeyValueList items={detailItems} columns={1} />
                        <JsonViewer value={detailRow} title="原始 JSON" />
                    </div>
                )}
            </SideSheet>

            <FofaParseModal
                visible={parseOpen}
                provider={form.provider}
                text={naturalText}
                onClose={() => setParseOpen(false)}
                onApply={applyParsedQuery}
            />
        </div>
    );
}
