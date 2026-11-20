/**
 * 规则试匹配：POST /tool-guard/test（后端用 RE2 编译并执行匹配，不调用任何工具）。
 * 同一组件用于「全部规则验证」和单条规则验证，区别只在提交的 config 范围。
 */
import { useState } from 'react';
import { Banner, Button, Input, Space, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { ErrorBanner } from '../../components/ui';
import { requestConfig, singleRuleConfig } from './utils';

const { Text } = Typography;

const DEFAULT_TOOL = 'http_request';
const DEFAULT_ARGS = '{"url": "https://example.gov.cn"}';

export default function TestPanel({ config, rule = null, disabled = false, compact = false }) {
    const t = useT();
    const single = Boolean(rule);
    const [toolName, setToolName] = useState(DEFAULT_TOOL);
    const [argsText, setArgsText] = useState(DEFAULT_ARGS);
    const [busy, setBusy] = useState(false);
    const [result, setResult] = useState(null);
    const [error, setError] = useState(null);

    const run = async () => {
        if (disabled || busy) return;
        setError(null);
        setResult(null);
        if (!single && !config) {
            setError(new Error(t('toolGuard.loadFirst', null, '请先加载拦截配置')));
            return;
        }
        if (!toolName.trim()) {
            setError(new Error(t('toolGuard.toolRequired', null, '请填写测试工具名称')));
            return;
        }
        let args;
        try {
            args = JSON.parse(argsText);
            if (!args || Array.isArray(args) || typeof args !== 'object') throw new Error('not an object');
        } catch (parseError) {
            setError(new Error(t('toolGuard.invalidArguments', null, '工具参数必须是有效的 JSON 对象')));
            return;
        }
        setBusy(true);
        try {
            const payload = {
                config: single ? singleRuleConfig(rule) : requestConfig(config),
                toolName: toolName.trim(),
                arguments: args
            };
            const response = await api.post('/tool-guard/test', payload);
            if (!response || typeof response.blocked !== 'boolean') {
                throw new Error(t('toolGuard.invalidTestResponse', null, '服务端返回的测试结果格式不正确'));
            }
            setResult(response);
        } catch (requestError) {
            setError(requestError);
        } finally {
            setBusy(false);
        }
    };

    const blocked = result?.blocked;
    const match = result?.match;
    const disabledScope = !single && config && config.enabled === false;

    return (
        <div className={compact ? 'tool-guard-test tool-guard-test-compact' : 'tool-guard-test'}>
            <Space vertical spacing={6} align="start">
                <Text strong>
                    {single
                        ? t('toolGuard.testScopeSingle', { name: rule.name || t('toolGuard.unnamedRule', null, '未命名规则') }, `单条验证：${rule.name || '未命名规则'}`)
                        : t('toolGuard.testScopeAll', null, '全部规则验证')}
                </Text>
                <Text type="tertiary" size="small">
                    {single
                        ? t('toolGuard.testSingleHint', null, '仅验证本条规则，忽略总开关和规则启停；不保存、不执行工具。')
                        : t('toolGuard.testAllHint', null, '按当前顺序与启停状态验证全部规则，不保存、不执行工具。')}
                </Text>
            </Space>

            <div className="tool-guard-test-form">
                <label className="tool-guard-field">
                    <Text type="tertiary" size="small">{t('toolGuard.testTool', null, '工具名称')}</Text>
                    <Input value={toolName} onChange={value => { setToolName(value); setResult(null); }} placeholder="http_request" />
                </label>
                <label className="tool-guard-field tool-guard-field-wide">
                    <Text type="tertiary" size="small">{t('toolGuard.testArguments', null, '工具参数（JSON 对象）')}</Text>
                    <TextArea
                        value={argsText}
                        onChange={value => { setArgsText(value); setResult(null); }}
                        rows={3}
                        spellCheck={false}
                        className="tool-guard-args"
                    />
                </label>
            </div>

            <Button
                theme="solid"
                type="primary"
                loading={busy}
                disabled={disabled}
                onClick={run}
                className="tool-guard-test-run"
            >
                {busy ? t('toolGuard.testing', null, '验证中…') : t('toolGuard.test', null, '测试匹配')}
            </Button>

            <ErrorBanner error={error} />

            {result && (
                <div className={`tool-guard-test-result ${blocked ? 'is-blocked' : 'is-pass'}`}>
                    <Space spacing={8} wrap>
                        <Tag color={blocked ? 'red' : (disabledScope ? 'grey' : 'green')} size="small">
                            {blocked ? 'blocked' : 'passed'}
                        </Tag>
                        <Text strong>
                            {single
                                ? (blocked
                                    ? t('toolGuard.singleMatched', null, '本条规则匹配成功')
                                    : t('toolGuard.singleNotMatched', null, '本条规则未匹配'))
                                : (blocked
                                    ? t('toolGuard.blocked', null, '已命中规则：将阻止调用')
                                    : (disabledScope
                                        ? t('toolGuard.disabledResult', null, '调用拦截已关闭：此配置不会阻止调用')
                                        : t('toolGuard.notBlocked', null, '未命中已启用的规则')))}
                        </Text>
                    </Space>
                    {single && (
                        <Text type="tertiary" size="small">
                            {t('toolGuard.singleResultHint', null, '这是单条规则验证结果，实际拦截还取决于总开关、规则启停和匹配顺序。')}
                        </Text>
                    )}
                    {blocked && match && (
                        <div className="tool-guard-match">
                            <div>
                                <Text type="tertiary" size="small">{t('toolGuard.matchedRule', null, '命中规则')}</Text>
                                <div>{match.ruleName || match.ruleId || '—'}</div>
                            </div>
                            <div>
                                <Text type="tertiary" size="small">{t('toolGuard.matchedText', null, '匹配文本')}</Text>
                                <pre>{match.matchedText || '—'}</pre>
                            </div>
                            <div>
                                <Text type="tertiary" size="small">{t('toolGuard.matchedMessage', null, '返回的拦截提醒')}</Text>
                                <div>{match.message || '—'}</div>
                            </div>
                        </div>
                    )}
                </div>
            )}
            {!result && !error && (
                <Text type="tertiary" size="small">{t('toolGuard.testReadyHint', null, '点击「测试匹配」，在这里查看验证结果。')}</Text>
            )}
            {single === false && !result && !error && disabledScope && (
                <Banner
                    type="info"
                    bordered
                    closeIcon={null}
                    description={t('toolGuard.disabledResult', null, '调用拦截已关闭：此配置不会阻止调用')}
                />
            )}
        </div>
    );
}
