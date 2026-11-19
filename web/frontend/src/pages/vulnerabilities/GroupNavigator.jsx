import { useEffect, useMemo, useState } from 'react';
import { Input, Spin, Tag, Tree, Typography } from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { SEVERITY_CODES, severityText } from './shared';

const { Text } = Typography;
const severityColors = { critical: 'red', high: 'orange', medium: 'amber', low: 'blue', info: 'grey' };
const NO_EXPANDED_GROUPS = [];

function groupLabel(t, node) {
    if (node.key.startsWith('task:')) {
        return t('vulnerabilityPage.viewTaskGroup', { id: node.label || node.key.slice(5) });
    }
    if (node.label) return node.label;
    if (node.key.startsWith('host:')) return t('vulnerabilityPage.unknownHost');
    if (node.key.startsWith('queue:')) return t('vulnerabilityPage.taskQueueGroup', { id: node.key.slice(6) });
    if (node.key.startsWith('assessment:')) return node.key.slice(11);
    return t('vulnerabilityPage.viewNoTask');
}

function treeNode(t, node) {
    const name = groupLabel(t, node);
    return {
        key: node.key,
        value: node.key,
        label: (
            <span className="vulnerability-group-node">
                <span className="vulnerability-group-name" title={name}>{name}</span>
                <Tag size="small" color="grey">{node.count}</Tag>
            </span>
        ),
        children: node.children?.map(child => treeNode(t, child))
    };
}

export function GroupNavigator({ t, mode, groups, selection, loading, search, onSearch, onSelect }) {
    const [keyword, setKeyword] = useState(search);
    const [expandedKeys, setExpandedKeys] = useState(null);
    const isTask = mode === 'task';
    const heading = t(isTask ? 'vulnerabilityPage.viewTask' : 'vulnerabilityPage.viewHost');
    const searchLabel = t(isTask ? 'vulnerabilityPage.taskSearch' : 'vulnerabilityPage.hostSearch');
    const treeData = useMemo(() => {
        const all = t(isTask ? 'vulnerabilityPage.allTasks' : 'vulnerabilityPage.allHosts');
        return [{ key: 'all', value: 'all', label: all }, ...groups.map(node => treeNode(t, node))];
    }, [t, groups, isTask]);
    const selectedParent = groups.find(node => node.children?.some(child => child.key === selection));
    const expanded = isTask
        ? expandedKeys ?? (selectedParent ? [selectedParent.key] : NO_EXPANDED_GROUPS)
        : treeData.filter(node => node.children?.length).map(node => node.key);

    useEffect(() => setKeyword(search), [search]);
    useEffect(() => {
        if (keyword.trim() === search) return;
        const timeout = setTimeout(() => onSearch(keyword.trim()), 350);
        return () => clearTimeout(timeout);
    }, [keyword, search, onSearch]);

    return (
        <aside className="vulnerability-group-sidebar" aria-label={heading}>
            <Text strong>{heading}</Text>
            <Input
                prefix={<IconSearch />}
                aria-label={searchLabel}
                placeholder={searchLabel}
                value={keyword}
                showClear
                onChange={setKeyword}
            />
            <Spin spinning={loading} wrapperClassName="vulnerability-group-scroll">
                <Tree
                    aria-label={heading}
                    treeData={treeData}
                    value={selection || 'all'}
                    onSelect={key => onSelect(key === 'all' ? '' : key)}
                    expandedKeys={expanded}
                    onExpand={isTask ? setExpandedKeys : undefined}
                    expandIcon={isTask ? undefined : () => <span className="semi-tree-empty-icon" aria-hidden="true" />}
                    expandAction={false}
                    blockNode
                />
                {!groups.length && !loading && (
                    <Text className="vulnerability-group-empty" type="tertiary" size="small">
                        {t('vulnerabilityPage.noMatchingGroups')}
                    </Text>
                )}
            </Spin>
        </aside>
    );
}

export function GroupOverview({ t, group }) {
    return (
        <div className="vulnerability-group-overview">
            <Text strong className="vulnerability-group-name">{groupLabel(t, group)}</Text>
            <div className="vulnerability-group-badges">
                {SEVERITY_CODES.filter(code => group.severities?.[code]).map(code => (
                    <Tag key={code} color={severityColors[code]}>
                        {severityText(t, code)} {group.severities[code]}
                    </Tag>
                ))}
                <Tag color="grey">{t('vulnerabilityPage.groupPending', { count: group.pending })}</Tag>
            </div>
        </div>
    );
}
