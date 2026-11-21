/** 漏洞筛选栏：关键词 + 级别/状态/项目/来源/任务标签，选项来自 filter-options。 */
import { useEffect, useRef, useState } from 'react';
import { Button, Input, Select } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useDebounced } from '../../components/hooks';
import { hasActiveFilters, severityOptions, statusOptions } from './shared';

function withEmpty(label, options) {
    return [{ value: '', label }, ...options];
}

export default function FilterBar({ t, filters, options, projectOptions, hasGroupFilters, onChange, onReset }) {
    const [keyword, setKeyword] = useState(filters.q);
    const debounced = useDebounced(keyword, 380);
    const lastEmitted = useRef(filters.q);

    // 输入停顿后再写回 URL，避免每次按键都触发请求。
    useEffect(() => {
        if (debounced === lastEmitted.current) return;
        lastEmitted.current = debounced;
        onChange('q', debounced);
    }, [debounced, onChange]);

    // 外部重置/深链接改变 q 时回填输入框。
    useEffect(() => {
        if (filters.q === lastEmitted.current) return;
        lastEmitted.current = filters.q;
        setKeyword(filters.q);
    }, [filters.q]);

    const conversationTags = Array.isArray(options?.conversation_tags) ? options.conversation_tags : [];
    const taskTags = Array.isArray(options?.task_tags) ? options.task_tags : [];

    return (
        <div className="vulnerability-filter-bar">
            <Input
                className="vulnerability-filter-search"
                prefix={<IconSearch />}
                value={keyword}
                showClear
                placeholder={t('vulnerabilityPage.searchKeyword', null, '搜索标题、描述、类型、目标…')}
                aria-label={t('vulnerabilityPage.searchKeyword', null, '搜索标题、描述、类型、目标…')}
                onChange={value => setKeyword(value)}
            />
            <Select
                className="vulnerability-filter-select"
                value={filters.severity}
                placeholder={t('vulnerabilityPage.severity', null, '严重程度')}
                optionList={withEmpty(t('vulnerabilityPage.allSeverities', null, '全部级别'), severityOptions(t))}
                onChange={value => onChange('severity', value || '')}
            />
            <Select
                className="vulnerability-filter-select"
                value={filters.status}
                placeholder={t('vulnerabilityPage.status', null, '状态')}
                optionList={withEmpty(t('dashboard.allStatuses', null, '全部状态'), statusOptions(t))}
                onChange={value => onChange('status', value || '')}
            />
            <Select
                className="vulnerability-filter-select vulnerability-filter-select--wide"
                value={filters.project_id}
                placeholder={t('vulnerabilityPage.detailProject', null, '所属项目')}
                filter
                optionList={withEmpty(t('vulnerabilityPage.allProjects', null, '全部项目'), projectOptions)}
                onChange={value => onChange('project_id', value || '')}
            />
            <Select
                className="vulnerability-filter-select"
                value={filters.conversation_tag}
                placeholder={t('vulnerabilityPage.conversationTag', null, '来源（对话标签）')}
                filter
                optionList={withEmpty(
                    t('vulnerabilityPage.allSources', null, '全部来源'),
                    conversationTags.map(tag => ({ value: tag, label: tag }))
                )}
                onChange={value => onChange('conversation_tag', value || '')}
            />
            <Select
                className="vulnerability-filter-select"
                value={filters.task_tag}
                placeholder={t('vulnerabilityPage.taskTag', null, '任务标签')}
                filter
                optionList={withEmpty(
                    t('vulnerabilityPage.allTaskTags', null, '全部任务标签'),
                    taskTags.map(tag => ({ value: tag, label: tag }))
                )}
                onChange={value => onChange('task_tag', value || '')}
            />
            <div className="vulnerability-filter-actions">
                <Button
                    icon={<IconRefresh />}
                    theme="borderless"
                    disabled={!hasActiveFilters(filters) && !filters.sort && !hasGroupFilters}
                    onClick={onReset}
                >
                    {t('vulnerabilityPage.clearAll', null, '重置全部')}
                </Button>
            </div>
        </div>
    );
}
