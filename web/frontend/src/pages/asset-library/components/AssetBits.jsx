/** 资产相关的轻量展示原子：截断文本、风险/状态/协议标签、标签列表。 */
import { Space, Tag, Tooltip, Typography } from '@douyinfe/semi-ui';
import { RISK_COLORS, riskLevelOf, riskLabel, STATUS_LABELS } from './assetConstants';

const { Text } = Typography;

export function TooltipText({ text, max = 42, className = '', placeholder = '—' }) {
    const value = String(text ?? '').trim();
    if (!value) return <Text type="tertiary">{placeholder}</Text>;
    if (value.length <= max) return <span className={className}>{value}</span>;
    const clipped = `${value.slice(0, max)}…`;
    return (
        <Tooltip content={value} position="topLeft">
            <span className={className}>{clipped}</span>
        </Tooltip>
    );
}

export function RiskTag({ asset, size = 'small' }) {
    const level = riskLevelOf(asset);
    return (
        <Tag size={size} color={RISK_COLORS[level] || 'grey'} shape="circle">
            {riskLabel(level)}
        </Tag>
    );
}

export function AssetStatusTag({ status, size = 'small' }) {
    const value = String(status || 'active').toLowerCase();
    return (
        <Tag size={size} color={value === 'inactive' ? 'grey' : 'green'} shape="square">
            {STATUS_LABELS[value] || status || '—'}
        </Tag>
    );
}

export function ProtocolTag({ protocol, port, size = 'small' }) {
    const value = String(protocol || '').trim();
    const suffix = Number(port || 0) > 0 ? `:${port}` : '';
    if (!value && !suffix) return <Text type="tertiary">—</Text>;
    return (
        <Space spacing={4}>
            {value ? <Tag size={size} color="blue" shape="square">{value}</Tag> : null}
            {suffix ? <Text type="tertiary" size="small">{suffix}</Text> : null}
        </Space>
    );
}

export function TagList({ tags, max = 3 }) {
    const list = Array.isArray(tags) ? tags.filter(Boolean) : [];
    if (!list.length) return <Text type="tertiary">—</Text>;
    const visible = list.slice(0, max);
    const rest = list.slice(max);
    return (
        <Space spacing={4} wrap>
            {visible.map(tag => <Tag key={tag} size="small" color="violet" shape="square">{tag}</Tag>)}
            {rest.length ? (
                <Tooltip content={rest.join('、')}>
                    <span><Tag size="small" shape="square">+{rest.length}</Tag></span>
                </Tooltip>
            ) : null}
        </Space>
    );
}
