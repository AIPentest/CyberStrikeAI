/** 内联 HITL 审批卡：显示待审批工具与参数，支持批准 / 拒绝 / 忽略。 */
import { useMemo, useState } from 'react';
import { Button, Card, Collapse, Input, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconTick, IconClose } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { formatDateTime } from '../../lib/format';
import { prettyJson } from './stream';

const { Text } = Typography;

const MODE_LABEL = {
    approval: '仅审批',
    feedback: '审批并反馈',
    followup: '审批并追问',
    review_edit: '审查编辑'
};

export function parseHitlItem(item) {
    if (!item) return null;
    let payload = item.payload;
    if (typeof payload === 'string') {
        try {
            payload = JSON.parse(payload);
        } catch (error) {
            payload = null;
        }
    }
    if (!payload || typeof payload !== 'object') payload = {};
    const args = payload.argumentsObj && typeof payload.argumentsObj === 'object'
        ? payload.argumentsObj
        : (payload.arguments || null);
    return {
        ...item,
        payload,
        interruptId: String(item.id || item.interruptId || ''),
        toolName: String(item.toolName || payload.toolName || '未知工具'),
        mode: String(item.mode || payload.mode || 'approval').toLowerCase(),
        args
    };
}

export default function HitlCard({ item, busy, readOnly, onApprove, onReject, onDismiss }) {
    const t = useT();
    const data = useMemo(() => parseHitlItem(item), [item]);
    const [comment, setComment] = useState('');
    if (!data) return null;
    const argsText = prettyJson(data.args, 4000);
    const modeLabel = MODE_LABEL[data.mode] || data.mode;
    const disabled = readOnly || busy || !data.interruptId;
    return (
        <Card
            className="chat-hitl-card"
            bordered
            title={(
                <span className="chat-hitl-title">
                    <span>{t('chat.hitlTitle', null, '人工审批')}</span>
                    <Tag size="small" color="orange" shape="circle">{modeLabel}</Tag>
                    <span className="chat-hitl-tool">{data.toolName}</span>
                </span>
            )}
            headerExtraContent={(
                <Text type="tertiary" size="small">
                    {data.createdAt ? formatDateTime(data.createdAt) : ''}
                </Text>
            )}
        >
            <div className="chat-hitl-meta">
                {data.reviewer && <Text type="tertiary" size="small">审批人：{data.reviewer === 'audit_agent' ? '审计代理' : '人工'}</Text>}
                {data.expiresAt && <Text type="tertiary" size="small">到期：{formatDateTime(data.expiresAt)}</Text>}
                {data.conversationId && <Text type="tertiary" size="small">会话：{String(data.conversationId).slice(0, 8)}</Text>}
            </div>
            {argsText && (
                <Collapse className="chat-block-collapse">
                    <Collapse.Panel itemKey="1" header={<span className="chat-block-title">调用参数</span>}>
                        <pre className="chat-pre">{argsText}</pre>
                    </Collapse.Panel>
                </Collapse>
            )}
            <div className="chat-hitl-actions">
                <Input
                    size="small"
                    placeholder={t('chat.hitlComment', null, '审批备注（拒绝时建议填写原因）')}
                    disabled={readOnly}
                    value={comment}
                    onChange={setComment}
                    className="chat-hitl-comment"
                />
                <Space>
                    <Button
                        theme="solid"
                        size="small"
                        icon={<IconTick />}
                        disabled={disabled}
                        onClick={() => onApprove(data, comment)}
                    >
                        {t('chat.hitlApprove', null, '批准')}
                    </Button>
                    <Button
                        type="danger"
                        size="small"
                        icon={<IconClose />}
                        disabled={disabled}
                        onClick={() => onReject(data, comment)}
                    >
                        {t('chat.hitlReject', null, '拒绝')}
                    </Button>
                    <Button size="small" theme="borderless" disabled={disabled} onClick={() => onDismiss(data)}>
                        {t('chat.hitlDismiss', null, '忽略')}
                    </Button>
                </Space>
            </div>
        </Card>
    );
}
