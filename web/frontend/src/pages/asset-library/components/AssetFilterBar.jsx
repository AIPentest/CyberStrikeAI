/** 资产库筛选栏：常用条件平铺，其余条件收进“高级筛选”。 */
import { useEffect, useId, useState } from 'react';
import { Button, Collapse, Input, Select } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { useT } from '../../../lib/i18n';
import {
    CRITICALITY_OPTIONS, ENVIRONMENT_OPTIONS, PROTOCOL_OPTIONS, RISK_OPTIONS,
    SCAN_STATE_OPTIONS, SORT_OPTIONS, STATUS_OPTIONS
} from './assetConstants';

const EMPTY_FILTERS = {
    q: '', protocol: '', source: '', tag: '',
    status: '', risk_level: '', environment: '', criticality: '',
    ip: '', domain: '', port: '', scan: '', sort: 'last_seen_at:desc'
};

export function emptyAssetFilters() {
    return { ...EMPTY_FILTERS };
}

export default function AssetFilterBar({ scope, filters, onChange, onReset, disabled }) {
    const t = useT();
    const scopeLabelId = useId();
    const [keyword, setKeyword] = useState(filters.q || '');

    useEffect(() => { setKeyword(filters.q || ''); }, [filters.q]);

    const advancedCount = ['status', 'risk_level', 'environment', 'criticality', 'ip', 'domain', 'port', 'scan']
        .filter(key => filters[key]).length;

    return (
        <div className="asset-library-filters">
            <div className="asset-library-filter-row">
                <span id={scopeLabelId} hidden>{t('assets.scopeLabel', null, '资产范围')}</span>
                <Select
                    aria-labelledby={scopeLabelId}
                    value={scope.value}
                    optionList={scope.options}
                    loading={scope.loading}
                    disabled={disabled}
                    onChange={scope.onChange}
                    filter
                />
                <Input
                    className="asset-library-filter-search"
                    prefix={<IconSearch />}
                    value={keyword}
                    disabled={disabled}
                    placeholder={t('assets.searchPlaceholder', null, '搜索主机、IP、域名、标题、服务或标签')}
                    onChange={value => { setKeyword(value); onChange({ q: value }); }}
                    showClear
                />
                <Select
                    value={filters.protocol || undefined}
                    placeholder={t('assets.assetType', null, '类型 / 协议')}
                    optionList={PROTOCOL_OPTIONS}
                    filter
                    allowCreate
                    showClear
                    disabled={disabled}
                    onChange={value => onChange({ protocol: value || '' })}
                />
                <Input
                    value={filters.source}
                    placeholder={t('assets.source', null, '来源')}
                    disabled={disabled}
                    onChange={value => onChange({ source: value })}
                    showClear
                />
                <Input
                    value={filters.tag}
                    placeholder={t('assets.tagsLabel', null, '标签')}
                    disabled={disabled}
                    onChange={value => onChange({ tag: value })}
                    showClear
                />
                <Button onClick={onReset} disabled={disabled}>
                    {t('assets.resetFilters', null, '重置')}
                </Button>
            </div>

            <Collapse className="asset-library-filter-advanced" keepDOM={false}>
                <Collapse.Panel
                    itemKey="advanced"
                    header={`${t('assets.advancedFilters', null, '高级筛选')}${advancedCount ? `（${advancedCount}）` : ''}`}
                >
                    <div className="asset-library-filter-grid">
                        <Select
                            value={filters.status || undefined}
                            placeholder={t('assets.allStatuses', null, '全部状态')}
                            optionList={STATUS_OPTIONS}
                            showClear
                            disabled={disabled}
                            onChange={value => onChange({ status: value || '' })}
                        />
                        <Select
                            value={filters.risk_level || undefined}
                            placeholder={t('assets.allRisks', null, '全部风险')}
                            optionList={RISK_OPTIONS}
                            showClear
                            disabled={disabled}
                            onChange={value => onChange({ risk_level: value || '' })}
                        />
                        <Select
                            value={filters.environment || undefined}
                            placeholder={t('assets.allEnvironments', null, '全部环境')}
                            optionList={ENVIRONMENT_OPTIONS}
                            showClear
                            disabled={disabled}
                            onChange={value => onChange({ environment: value || '' })}
                        />
                        <Select
                            value={filters.criticality || undefined}
                            placeholder={t('assets.allCriticalities', null, '全部级别')}
                            optionList={CRITICALITY_OPTIONS}
                            showClear
                            disabled={disabled}
                            onChange={value => onChange({ criticality: value || '' })}
                        />
                        <Input
                            value={filters.ip}
                            placeholder="IP"
                            disabled={disabled}
                            onChange={value => onChange({ ip: value })}
                            showClear
                        />
                        <Input
                            value={filters.domain}
                            placeholder={t('assets.domain', null, '域名')}
                            disabled={disabled}
                            onChange={value => onChange({ domain: value })}
                            showClear
                        />
                        <Input
                            value={filters.port}
                            placeholder={t('assets.port', null, '端口')}
                            disabled={disabled}
                            onChange={value => onChange({ port: String(value || '').replace(/[^\d]/g, '') })}
                            showClear
                        />
                        <Select
                            value={filters.scan || ''}
                            placeholder={t('assets.scanState', null, '扫描状态')}
                            optionList={SCAN_STATE_OPTIONS}
                            disabled={disabled}
                            onChange={value => onChange({ scan: value || '' })}
                        />
                        <Select
                            value={filters.sort || 'last_seen_at:desc'}
                            optionList={SORT_OPTIONS}
                            disabled={disabled}
                            onChange={value => onChange({ sort: value || 'last_seen_at:desc' })}
                        />
                    </div>
                </Collapse.Panel>
            </Collapse>
        </div>
    );
}
