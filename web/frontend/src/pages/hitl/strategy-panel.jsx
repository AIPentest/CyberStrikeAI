/** 审核策略：全局默认配置、审计 Agent 策略、会话配置查询。 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Button, Input, Radio, RadioGroup, Select, Space, Tag, TextArea, Typography
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import {
    ErrorBanner, KeyValueList, SectionCard, toastError, toastSuccess
} from '../../components/ui';
import { useResource } from '../../components/hooks';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { HITL_MODES, modeLabel, normalizeMode, reviewerLabel } from './utils';

const { Text } = Typography;

export default function StrategyPanel({ canWrite, initialConversationId = '', refreshKey = 0 }) {
    const t = useT();
    const config = useResource(() => api.get('/hitl/default-config'), [refreshKey]);
    const reviewerRes = useResource(() => api.get('/hitl/default-reviewer'), [refreshKey]);
    const strategy = useResource(() => api.get('/hitl/audit-strategy'), [refreshKey]);

    const [mode, setMode] = useState('off');
    const [reviewer, setReviewer] = useState('human');
    const [timeoutText, setTimeoutText] = useState('0');
    const [savingConfig, setSavingConfig] = useState(false);
    const [savingReviewer, setSavingReviewer] = useState(false);

    const [approvalPrompt, setApprovalPrompt] = useState('');
    const [reviewPrompt, setReviewPrompt] = useState('');
    const [savingStrategy, setSavingStrategy] = useState(false);

    const [convId, setConvId] = useState(initialConversationId || '');
    const [convData, setConvData] = useState(null);
    const [convError, setConvError] = useState(null);
    const [convLoading, setConvLoading] = useState(false);

    useEffect(() => {
        if (!config.data) return;
        setMode(normalizeMode(config.data.defaultMode));
        setTimeoutText(String(config.data.defaultTimeoutSeconds ?? 0));
    }, [config.data]);

    useEffect(() => {
        const source = reviewerRes.data?.defaultReviewer ?? config.data?.defaultReviewer;
        if (source) setReviewer(source === 'audit_agent' ? 'audit_agent' : 'human');
    }, [config.data, reviewerRes.data]);

    useEffect(() => {
        if (!strategy.data) return;
        setApprovalPrompt(strategy.data.auditAgentPrompt || '');
        setReviewPrompt(strategy.data.auditAgentPromptReviewEdit || '');
    }, [strategy.data]);

    const engineLabel = useMemo(() => {
        const backend = config.data?.auditBackend;
        const model = config.data?.auditModel;
        if (!backend && !model) return '';
        return [backend, model].filter(Boolean).join(' · ');
    }, [config.data]);

    const saveConfig = useCallback(async () => {
        setSavingConfig(true);
        try {
            const payload = await api.put('/hitl/default-config', {
                mode: normalizeMode(mode),
                reviewer,
                timeoutSeconds: Math.max(0, Number.parseInt(timeoutText, 10) || 0)
            });
            toastSuccess(t('hitl.pageReviewerSaved', null, '默认审批配置已保存'));
            config.reload({ silent: true });
            if (payload) {
                setMode(normalizeMode(payload.defaultMode));
                setReviewer(payload.defaultReviewer === 'audit_agent' ? 'audit_agent' : 'human');
                setTimeoutText(String(payload.defaultTimeoutSeconds ?? 0));
            }
        } catch (error) {
            toastError(error, '保存默认配置失败');
        } finally {
            setSavingConfig(false);
        }
    }, [config.reload, mode, reviewer, t, timeoutText]); // eslint-disable-line react-hooks/exhaustive-deps

    const saveReviewer = useCallback(async () => {
        setSavingReviewer(true);
        try {
            await api.put('/hitl/default-reviewer', { reviewer });
            toastSuccess(t('hitl.pageReviewerSaved', null, '默认审批方已保存'));
            reviewerRes.reload({ silent: true });
            config.reload({ silent: true });
        } catch (error) {
            toastError(error, '保存默认审批方失败');
        } finally {
            setSavingReviewer(false);
        }
    }, [config.reload, reviewer, reviewerRes.reload, t]); // eslint-disable-line react-hooks/exhaustive-deps

    const saveStrategy = useCallback(async () => {
        setSavingStrategy(true);
        try {
            await api.put('/hitl/audit-strategy', {
                auditAgentPrompt: approvalPrompt,
                auditAgentPromptReviewEdit: reviewPrompt
            });
            toastSuccess(t('hitl.strategySaved', null, '审计策略已保存'));
            strategy.reload({ silent: true });
        } catch (error) {
            toastError(error, t('hitl.strategySaveFailed', null, '保存审计策略失败'));
        } finally {
            setSavingStrategy(false);
        }
    }, [approvalPrompt, reviewPrompt, strategy.reload, t]); // eslint-disable-line react-hooks/exhaustive-deps

    const resetPrompts = useCallback(() => {
        setApprovalPrompt(strategy.data?.defaultAuditAgentPrompt || '');
        setReviewPrompt(strategy.data?.defaultAuditAgentPromptReviewEdit || '');
    }, [strategy.data]);

    const loadConversation = useCallback(async id => {
        const target = String(id || '').trim();
        if (!target) return;
        setConvLoading(true);
        setConvError(null);
        try {
            const data = await api.get(`/hitl/config/${encodeURIComponent(target)}`);
            setConvData(data);
        } catch (error) {
            setConvData(null);
            setConvError(error);
        } finally {
            setConvLoading(false);
        }
    }, []);

    useEffect(() => {
        if (initialConversationId) {
            setConvId(initialConversationId);
            loadConversation(initialConversationId);
        }
    }, [initialConversationId, loadConversation]);

    const hitlConfig = convData?.hitl || null;

    return (
        <div className="hitl-strategy-panel">
            <SectionCard
                title={t('hitl.strategyLabel', null, '审计策略')}
                description="作用于全局默认配置；会话级配置在对话侧栏覆盖这里的选择。"
                extra={(
                    <Space spacing={8}>
                        <Button size="small" icon={<IconRefresh />} theme="borderless" onClick={() => config.reload()} loading={config.loading}>
                            {t('common.refresh', null, '刷新')}
                        </Button>
                        <Button size="small" disabled={!canWrite} loading={savingConfig} onClick={saveConfig}>
                            {t('common.save', null, '保存')}
                        </Button>
                    </Space>
                )}
            >
                <ErrorBanner error={config.error} onRetry={() => config.reload()} />
                <div className="hitl-strategy-form">
                    <label className="hitl-field">
                        <span className="hitl-field-label">默认模式</span>
                        <Select
                            value={mode}
                            style={{ width: 220 }}
                            disabled={!canWrite}
                            optionList={HITL_MODES.map(item => ({ value: item.value, label: item.label }))}
                            onChange={value => setMode(value)}
                        />
                        <Text type="tertiary" size="small">
                            {HITL_MODES.find(item => item.value === mode)?.hint || ''}
                        </Text>
                    </label>
                    <div className="hitl-field hitl-field-wide">
                        <span className="hitl-field-label">默认审批方</span>
                        <Space spacing={10} wrap>
                            <RadioGroup
                                value={reviewer}
                                disabled={!canWrite}
                                onChange={event => setReviewer(event.target.value)}
                            >
                                <Radio value="human">人工审批</Radio>
                                <Radio value="audit_agent">审计 Agent</Radio>
                            </RadioGroup>
                            <Button size="small" theme="light" disabled={!canWrite} loading={savingReviewer} onClick={saveReviewer}>
                                保存审批方
                            </Button>
                        </Space>
                        <Text type="tertiary" size="small">{`当前：${reviewerLabel(reviewer)}（PUT /hitl/default-reviewer）`}</Text>
                    </div>
                    <label className="hitl-field">
                        <span className="hitl-field-label">审批超时（秒，0 = 无限等待）</span>
                        <Input
                            value={timeoutText}
                            style={{ width: 160 }}
                            disabled={!canWrite}
                            onChange={value => setTimeoutText(value.replace(/[^\d]/g, ''))}
                        />
                    </label>
                    {engineLabel && (
                        <div className="hitl-field">
                            <span className="hitl-field-label">审计引擎</span>
                            <Tag size="small" color="blue">{engineLabel}</Tag>
                        </div>
                    )}
                </div>
                {Array.isArray(config.data?.hitlGlobalToolWhitelist) && (
                    <p className="hitl-strategy-hint">
                        {`全局免审批工具 ${config.data.hitlGlobalToolWhitelist.length} 项；白名单在「白名单」页签维护。`}
                    </p>
                )}
            </SectionCard>

            <SectionCard
                title="审计 Agent 提示词"
                description={t('hitl.strategyHintApproval', null, '白名单内工具免审批；审批模式下审计 Agent 仅裁决通过/拒绝。')}
                extra={(
                    <Space spacing={8}>
                        <Button size="small" theme="borderless" disabled={!canWrite} onClick={resetPrompts}>
                            {t('hitl.strategyReset', null, '恢复默认')}
                        </Button>
                        <Button size="small" disabled={!canWrite} loading={savingStrategy} onClick={saveStrategy}>
                            {t('common.save', null, '保存')}
                        </Button>
                    </Space>
                )}
            >
                <ErrorBanner error={strategy.error} onRetry={() => strategy.reload()} />
                <div className="hitl-prompt-grid">
                    <div className="hitl-prompt-field">
                        <div className="hitl-prompt-head">
                            <Text strong>{t('hitl.strategyTabApproval', null, '审批模式')}</Text>
                            {strategy.data?.auditAgentPromptCustom && <Tag size="small" color="amber">自定义</Tag>}
                        </div>
                        <TextArea
                            rows={12}
                            value={approvalPrompt}
                            disabled={!canWrite}
                            spellCheck={false}
                            className="hitl-prompt-textarea"
                            onChange={value => setApprovalPrompt(value)}
                        />
                    </div>
                    <div className="hitl-prompt-field">
                        <div className="hitl-prompt-head">
                            <Text strong>{t('hitl.strategyTabReviewEdit', null, '审查编辑模式')}</Text>
                            {strategy.data?.auditAgentPromptReviewEditCustom && <Tag size="small" color="amber">自定义</Tag>}
                        </div>
                        <TextArea
                            rows={12}
                            value={reviewPrompt}
                            disabled={!canWrite}
                            spellCheck={false}
                            className="hitl-prompt-textarea"
                            onChange={value => setReviewPrompt(value)}
                        />
                    </div>
                </div>
                <p className="hitl-strategy-hint">
                    {t('hitl.strategyHintReviewEdit', null, '审查编辑模式下审计 Agent 可通过 editedArguments 收窄参数后放行；无法安全改参时应拒绝。')}
                </p>
            </SectionCard>

            <SectionCard
                title="会话配置查询"
                description={`GET /hitl/config/:conversationId · 当前默认 ${modeLabel(mode)} / ${reviewerLabel(reviewer)}`}
            >
                <div className="hitl-conv-query">
                    <Input
                        prefix={<IconSearch />}
                        value={convId}
                        style={{ width: 360 }}
                        placeholder="输入会话 ID 查询该会话的 HITL 配置"
                        onChange={value => setConvId(value)}
                        onEnterPress={() => loadConversation(convId)}
                    />
                    <Button size="small" loading={convLoading} onClick={() => loadConversation(convId)}>
                        查询
                    </Button>
                </div>
                <ErrorBanner error={convError} onRetry={() => loadConversation(convId)} />
                {hitlConfig && (
                    <div className="hitl-conv-result">
                        <KeyValueList
                            columns={2}
                            items={[
                                { label: '会话 ID', value: <span className="hitl-mono">{convData.conversationId}</span> },
                                { label: '启用', value: hitlConfig.enabled ? '是' : '否' },
                                { label: '模式', value: modeLabel(hitlConfig.mode) },
                                { label: '审批方', value: reviewerLabel(hitlConfig.reviewer) },
                                { label: '超时（秒）', value: String(hitlConfig.timeoutSeconds ?? 0) },
                                {
                                    label: '会话白名单',
                                    value: Array.isArray(hitlConfig.sensitiveTools) && hitlConfig.sensitiveTools.length
                                        ? hitlConfig.sensitiveTools.join(', ')
                                        : '—'
                                },
                                { label: '全局默认模式', value: modeLabel(convData.defaultMode) },
                                { label: '全局默认审批方', value: reviewerLabel(convData.defaultReviewer) }
                            ]}
                        />
                    </div>
                )}
            </SectionCard>
        </div>
    );
}
