import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconMenu, IconRefresh } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';

const { Text, Title } = Typography;

const MODE_TAG = {
    single: { key: 'chat.agentModePiSingle', label: '单代理', color: 'blue' },
    deep: { key: 'chat.agentModeDeep', label: 'Deep', color: 'purple' },
    plan_execute: { key: 'chat.agentModePlanExecuteLabel', label: 'Plan-Execute', color: 'orange' },
    supervisor: { key: 'chat.agentModeSupervisorLabel', label: 'Supervisor', color: 'green' }
};

export default function ChatHeader({
    conversation, mode, hitlCount, conversationId, narrow, onToggleSidebar, onRefresh
}) {
    const t = useT();
    const modeTag = MODE_TAG[mode] || MODE_TAG.single;
    const title = conversation?.title || t('chat.newChat', null, '新对话');

    return (
        <header className="chat-header">
            <div className="chat-header-main">
                <div className="chat-header-heading">
                    <Title heading={3} className="chat-header-title" title={title}>{title}</Title>
                    <Space spacing={6} wrap className="chat-header-tags">
                        {conversation?.pinned && <Tag size="small" color="amber">{t('chat.pinned', null, '已置顶')}</Tag>}
                        <Tag size="small" color={modeTag.color}>{t(modeTag.key, null, modeTag.label)}</Tag>
                        {hitlCount > 0 && (
                            <Tag size="small" color="orange">
                                {t('chat.pendingApprovals', { count: hitlCount }, `待审批 ${hitlCount}`)}
                            </Tag>
                        )}
                    </Space>
                </div>
                <Text type="tertiary" size="small" className="chat-header-description">
                    {conversationId
                        ? t('chat.conversationHint', null, '会话历史与工具执行过程会自动落库')
                        : t('chat.newConversationHint', null, '输入内容即可创建新对话')}
                </Text>
            </div>
            <Space spacing={8} wrap className="chat-header-actions">
                {narrow && (
                    <Button icon={<IconMenu />} onClick={onToggleSidebar}>
                        {t('chat.conversations', null, '会话')}
                    </Button>
                )}
                {conversationId && (
                    <Button
                        theme="borderless"
                        type="tertiary"
                        icon={<IconRefresh aria-hidden="true" />}
                        title={t('chat.refreshConversationHint', null, '重新读取会话列表、消息、待审批项和执行计划')}
                        onClick={onRefresh}
                    >
                        {t('chat.refreshConversation', null, '刷新对话')}
                    </Button>
                )}
            </Space>
        </header>
    );
}
