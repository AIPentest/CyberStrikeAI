/** 单条拦截规则的编辑体：启停、上下移、删除、三个字段与单条验证。 */
import { Button, Input, Space, Switch, TextArea, Typography } from '@douyinfe/semi-ui';
import { IconArrowDown, IconArrowUp, IconDelete } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import TestPanel from './TestPanel';

const { Text } = Typography;

export default function RuleCard({ rule, index, total, canWrite, canTest, onChange, onDelete, onMove }) {
    const t = useT();
    return (
        <div className="tool-guard-rule-body">
            <div className="tool-guard-rule-toolbar">
                <Space spacing={8}>
                    <Switch
                        size="small"
                        checked={rule.enabled}
                        disabled={!canWrite}
                        onChange={next => onChange({ enabled: next })}
                        aria-label={t('toolGuard.ruleEnabled', null, '启用')}
                    />
                    <Text size="small" type="tertiary">
                        {rule.enabled ? t('toolGuard.ruleOn', null, '已启用') : t('toolGuard.ruleOff', null, '已停用')}
                    </Text>
                </Space>
                <Space spacing={6}>
                    <Button
                        size="small"
                        theme="borderless"
                        icon={<IconArrowUp />}
                        disabled={!canWrite || index === 0}
                        onClick={() => onMove(-1)}
                    >
                        {t('toolGuard.moveUp', null, '上移')}
                    </Button>
                    <Button
                        size="small"
                        theme="borderless"
                        icon={<IconArrowDown />}
                        disabled={!canWrite || index === total - 1}
                        onClick={() => onMove(1)}
                    >
                        {t('toolGuard.moveDown', null, '下移')}
                    </Button>
                    <Button
                        size="small"
                        type="danger"
                        theme="borderless"
                        icon={<IconDelete />}
                        disabled={!canWrite}
                        onClick={onDelete}
                    >
                        {t('toolGuard.deleteRule', null, '删除')}
                    </Button>
                </Space>
            </div>

            <div className="tool-guard-editor-grid">
                <label className="tool-guard-field">
                    <Text type="tertiary" size="small">{t('toolGuard.ruleName', null, '规则名称')}</Text>
                    <Input
                        value={rule.name}
                        maxLength={200}
                        disabled={!canWrite}
                        onChange={value => onChange({ name: value })}
                        placeholder={t('toolGuard.ruleName', null, '规则名称')}
                    />
                </label>
                <label className="tool-guard-field tool-guard-field-wide">
                    <Text type="tertiary" size="small">{t('toolGuard.pattern', null, '匹配正则（RE2）')}</Text>
                    <TextArea
                        value={rule.pattern}
                        rows={3}
                        spellCheck={false}
                        disabled={!canWrite}
                        onChange={value => onChange({ pattern: value })}
                        className="tool-guard-mono"
                        placeholder="(?i)(169\\.254\\.169\\.254|metadata\\.google\\.internal)"
                    />
                </label>
                <label className="tool-guard-field tool-guard-field-wide">
                    <Text type="tertiary" size="small">{t('toolGuard.message', null, '拦截提醒')}</Text>
                    <TextArea
                        value={rule.message}
                        rows={2}
                        disabled={!canWrite}
                        onChange={value => onChange({ message: value })}
                        placeholder={t('toolGuard.defaultPreview', null, '命中后使用默认拦截提醒')}
                    />
                    <Text type="tertiary" size="small">
                        {t('toolGuard.messageHint', null, '拦截提醒可使用 {match}（匹配文本）、{tool}（工具名称）、{rule}（规则名称）。')}
                    </Text>
                </label>
            </div>

            <TestPanel config={null} rule={rule} compact disabled={!canTest} />
        </div>
    );
}
