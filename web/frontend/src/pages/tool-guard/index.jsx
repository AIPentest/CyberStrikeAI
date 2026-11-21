/**
 * 调用拦截（tool-guard）：GET /tool-guard（ETag 版本）读取规则，PUT /tool-guard 保存，
 * POST /tool-guard/test 试匹配。保存必须带 If-Match；409 表示他人已改，需要重新加载。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Button, Collapse, Space, Switch, Tag, Typography
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconTick } from '@douyinfe/semi-icons';
import { api } from '../../lib/http';
import { useAuth } from '../../lib/auth';
import { useT } from '../../lib/i18n';
import {
    ErrorBanner, PageHeader, SectionCard, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import RuleCard from './RuleCard';
import TestPanel from './TestPanel';
import {
    MAX_RULES, clone, isDirty, newRuleId, normalizeConfig, requestConfig, validateRules
} from './utils';
import './page.css';

const { Text } = Typography;

export default function ToolGuardPage() {
    const t = useT();
    const { hasPermission } = useAuth();
    const canRead = hasPermission('config:read');
    const canWrite = hasPermission('config:write');

    const [config, setConfig] = useState(null);
    const [saved, setSaved] = useState(null);
    const [etag, setEtag] = useState('');
    const [loading, setLoading] = useState(canRead);
    const [loadError, setLoadError] = useState(null);
    const [actionError, setActionError] = useState(null);
    const [saving, setSaving] = useState(false);
    const [conflict, setConflict] = useState(false);
    const [openIds, setOpenIds] = useState([]);

    const load = useCallback(async () => {
        if (!canRead) return;
        setLoading(true);
        setLoadError(null);
        setConflict(false);
        try {
            const response = await api.raw('/tool-guard');
            const nextEtag = response.headers.get('ETag') || '';
            const payload = await response.json();
            const normalized = normalizeConfig(payload);
            setConfig(normalized);
            setSaved(clone(normalized));
            setEtag(nextEtag);
        } catch (error) {
            setLoadError(error);
        } finally {
            setLoading(false);
        }
    }, [canRead]);

    useEffect(() => { load(); }, [load]);

    const dirty = useMemo(() => isDirty(config, saved), [config, saved]);
    const ruleCount = config?.rules?.length || 0;
    const enabledCount = config?.rules?.filter(rule => rule.enabled).length || 0;

    const reloadSafely = () => {
        if (!dirty) { load(); return; }
        confirmAction({
            title: t('toolGuard.reloadTitle', null, '重新加载拦截配置'),
            content: t('toolGuard.reloadConfirm', null, '当前有未保存修改，重新加载会丢弃这些修改。确认继续？'),
            okText: t('common.confirm', null, '确定'),
            onOk: load
        });
    };

    const updateRule = (index, patch) => {
        if (!canWrite) return;
        setConfig(current => ({
            ...current,
            rules: current.rules.map((rule, currentIndex) => (currentIndex === index ? { ...rule, ...patch } : rule))
        }));
        setActionError(null);
    };

    const addRule = () => {
        if (!canWrite || !config) return;
        if (config.rules.length >= MAX_RULES) {
            toastError(t('toolGuard.tooManyRules', null, `最多可配置 ${MAX_RULES} 条拦截规则`));
            return;
        }
        const id = newRuleId();
        setConfig(current => ({
            ...current,
            rules: [
                ...current.rules,
                {
                    id,
                    name: '',
                    enabled: true,
                    pattern: '',
                    message: t('toolGuard.defaultMessage', null, '识别到 {match}，调用被规则「{rule}」拦截。请检查目标与操作后重试。')
                }
            ]
        }));
        setOpenIds(current => [...current, id]);
        setActionError(null);
    };

    const deleteRule = index => {
        if (!canWrite || !config) return;
        setConfig(current => ({ ...current, rules: current.rules.filter((_, i) => i !== index) }));
        setActionError(null);
    };

    const moveRule = (index, delta) => {
        if (!canWrite || !config) return;
        const target = index + delta;
        if (target < 0 || target >= config.rules.length) return;
        setConfig(current => {
            const rules = [...current.rules];
            const [item] = rules.splice(index, 1);
            rules.splice(target, 0, item);
            return { ...current, rules };
        });
        setActionError(null);
    };

    const save = async () => {
        if (!canWrite || !config || !dirty) return;
        const invalid = validateRules(config);
        if (invalid) {
            setActionError(new Error(invalid.message));
            if (invalid.index >= 0) {
                const rule = config.rules[invalid.index];
                if (rule) setOpenIds(current => (current.includes(rule.id) ? current : [...current, rule.id]));
            }
            return;
        }
        if (!etag) {
            setActionError(new Error(t('toolGuard.versionRequired', null, '缺少调用拦截配置版本，请刷新页面后重试。')));
            return;
        }
        setSaving(true);
        setActionError(null);
        setConflict(false);
        try {
            const response = await api.raw('/tool-guard', {
                method: 'PUT',
                body: requestConfig(config),
                headers: { 'If-Match': etag }
            });
            const nextEtag = response.headers.get('ETag') || etag;
            const payload = await response.json();
            const normalized = normalizeConfig(payload);
            setConfig(normalized);
            setSaved(clone(normalized));
            setEtag(nextEtag);
            toastSuccess(t('toolGuard.saveSuccess', null, '调用拦截配置已保存，后续工具调用将使用新配置。'));
        } catch (error) {
            const code = error?.payload?.code;
            if (code === 'tool_guard_conflict' || code === 'tool_guard_version_required') {
                setConflict(true);
                setActionError(new Error(error.message));
            } else {
                setActionError(error);
            }
            toastError(error, t('toolGuard.saveFailed', null, '保存失败'));
        } finally {
            setSaving(false);
        }
    };

    const reset = () => {
        if (!canWrite || !saved) return;
        setConfig(clone(saved));
        setActionError(null);
        setConflict(false);
    };

    if (!canRead) {
        return (
            <div className="tool-guard-page">
                <PageHeader title={t('toolGuard.title', null, '调用拦截')} />
                <ErrorBanner error={new Error(t('toolGuard.noPermission', null, '当前账号缺少 config:read 权限，无法读取拦截配置。'))} />
            </div>
        );
    }

    return (
        <div className="tool-guard-page">
            <PageHeader
                title={t('toolGuard.title', null, '调用拦截')}
                description={t('toolGuard.description', null, '在 MCP 工具执行前检查工具名称和参数，命中规则立即阻止调用。人机协同审批和工具白名单不会跳过此检查。')}
                extra={(
                    <Space>
                        <Tag size="small" color={dirty ? 'amber' : 'green'}>
                            {saving
                                ? t('toolGuard.loading', null, '处理中…')
                                : dirty
                                    ? t('toolGuard.unsaved', null, '有未保存修改')
                                    : t('toolGuard.savedState', null, '已与服务端同步')}
                        </Tag>
                        <Button onClick={reset} disabled={!canWrite || !dirty || saving}>
                            {t('toolGuard.reset', null, '撤销修改')}
                        </Button>
                        <Button icon={<IconRefresh />} onClick={reloadSafely} loading={loading} disabled={saving}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button
                            theme="solid"
                            type="primary"
                            icon={<IconTick />}
                            loading={saving}
                            disabled={!canWrite || !dirty}
                            onClick={save}
                        >
                            {t('toolGuard.save', null, '保存并生效')}
                        </Button>
                    </Space>
                )}
            />

            {loading && !config && <SectionCard title={t('toolGuard.loading', null, '处理中…')}><Text type="tertiary">…</Text></SectionCard>}
            <ErrorBanner error={loadError} onRetry={load} />
            <ErrorBanner error={actionError} onRetry={conflict ? reloadSafely : undefined} />

            {config && (
                <>
                    <SectionCard
                        title={t('toolGuard.policyTitle', null, '拦截策略')}
                        description={t('toolGuard.enableHint', null, '在工具执行前检查风险，修改后保存生效。')}
                    >
                        <div className="tool-guard-policy">
                            <Space spacing={10} align="center">
                                <Switch
                                    checked={config.enabled}
                                    disabled={!canWrite || saving}
                                    onChange={next => { setConfig(current => ({ ...current, enabled: next })); setActionError(null); }}
                                    aria-label={t('toolGuard.enabled', null, '启用调用拦截')}
                                />
                                <Text strong>{t('toolGuard.enabled', null, '启用调用拦截')}</Text>
                                <Tag size="small" color={config.enabled ? 'green' : 'grey'}>
                                    {config.enabled
                                        ? t('toolGuard.protectionOn', null, '调用拦截已开启')
                                        : t('toolGuard.protectionOff', null, '调用拦截已关闭')}
                                </Tag>
                                <Tag size="small" color="blue">
                                    {t('toolGuard.ruleCount', { enabled: enabledCount, total: ruleCount }, `${enabledCount} / ${ruleCount} 条规则已启用`)}
                                </Tag>
                            </Space>
                            <Text type="tertiary" size="small">{t('toolGuard.scopeHint', null, '正则仅检查调用中可见的文本；不会解析 IP 归属、重定向目标或工具读取的文件内容。')}</Text>
                        </div>
                    </SectionCard>

                    <SectionCard
                        title={t('toolGuard.rulesTitle', null, '拦截规则')}
                        description={t('toolGuard.rulesHint', null, '按顺序匹配已启用的规则，使用首条命中规则的提醒。正则采用 RE2 语法，保存和试运行时由服务端校验。')}
                        extra={(
                            <Button
                                icon={<IconPlus />}
                                disabled={!canWrite || saving || ruleCount >= MAX_RULES}
                                onClick={addRule}
                            >
                                {t('toolGuard.addRule', null, '添加规则')}
                            </Button>
                        )}
                    >
                        {ruleCount === 0 && (
                            <Text type="tertiary">{t('toolGuard.emptyRules', null, '暂无规则。添加规则后保存，即可启用对应的拦截措施。')}</Text>
                        )}
                        {ruleCount > 0 && (
                            <Collapse
                                activeKey={openIds}
                                onChange={keys => setOpenIds([].concat(keys))}
                                keepDOM={false}
                            >
                                {config.rules.map((rule, index) => (
                                    <Collapse.Panel
                                        key={rule.id}
                                        itemKey={rule.id}
                                        header={(
                                            <Space spacing={8} align="center">
                                                <span className="tool-guard-rule-number">{String(index + 1).padStart(2, '0')}</span>
                                                <Text strong>{rule.name.trim() || t('toolGuard.unnamedRule', null, '未命名规则')}</Text>
                                                <Tag size="small" color={rule.enabled ? 'green' : 'grey'}>
                                                    {rule.enabled ? t('toolGuard.ruleOn', null, '已启用') : t('toolGuard.ruleOff', null, '已停用')}
                                                </Tag>
                                                {(() => {
                                                    const savedRule = saved?.rules?.find(item => item.id === rule.id);
                                                    if (!savedRule) return <Tag size="small" color="blue">{t('toolGuard.ruleNew', null, '新增')}</Tag>;
                                                    if (JSON.stringify(savedRule) !== JSON.stringify(rule)) {
                                                        return <Tag size="small" color="amber">{t('toolGuard.ruleModified', null, '已修改')}</Tag>;
                                                    }
                                                    return null;
                                                })()}
                                            </Space>
                                        )}
                                    >
                                        <RuleCard
                                            rule={rule}
                                            index={index}
                                            total={ruleCount}
                                            canWrite={canWrite && !saving}
                                            canTest={!saving}
                                            onChange={patch => updateRule(index, patch)}
                                            onDelete={() => deleteRule(index)}
                                            onMove={delta => moveRule(index, delta)}
                                        />
                                    </Collapse.Panel>
                                ))}
                            </Collapse>
                        )}
                        <Text type="tertiary" size="small">{t('toolGuard.listHint', null, '点击规则查看和编辑，按列表顺序匹配。')}</Text>
                    </SectionCard>

                    <SectionCard
                        title={t('toolGuard.testTitle', null, '全部规则验证')}
                        description={t('toolGuard.testHint', null, '使用页面上的当前配置（包括未保存修改），仅检查匹配结果，不执行任何工具。')}
                    >
                        <TestPanel config={config} disabled={saving} />
                    </SectionCard>
                </>
            )}
        </div>
    );
}
