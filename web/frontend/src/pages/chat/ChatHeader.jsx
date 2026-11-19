/** 对话页头：标题、会话标签、用量/计划/刷新入口。 */
import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconBolt, IconHistory, IconMenu, IconRefresh, IconSetting } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';

const { Text, Title } = Typography;

const MODE_TAG = {
    single: { key: 'chat.agentModePiSingle', label: '单代理', color: 'blue' },
    deep: { key: 'chat.agentModeDeep', label: 'Deep', color: 'purple' },
    plan_execute: { key: 'chat.agentModePlanExecuteLabel', label: 'Plan-Execute', color: 'orange' },
    supervisor: { key: 'chat.agentModeSupervisorLabel', label: 'Supervisor', color: 'green' }
};

export default function ChatHeader({
    conversation, mode, hitlCount, conversationId, plan, narrow,
    onToggleSidebar, onOpenUsage, onOpenPlan, onOpenSettings, onRefresh
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
                        {conversation?.projectId && (
                            <Tag size="small" type="ghost">项目 {String(conversation.projectId).slice(0, 8)}</Tag>
                        )}
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
                <Button theme="borderless" type="tertiary" icon={<IconHistory />} disabled={!conversationId} onClick={onOpenUsage}>
                    {t('chat.tokenUsage', null, 'Token 用量')}
                </Button>
                <Button theme="borderless" type="tertiary" icon={<IconBolt />} disabled={!conversationId} onClick={onOpenPlan}>
                    {t('chat.planTasks', null, '计划任务')}{plan?.total ? ` (${plan.completed}/${plan.total})` : ''}
                </Button>
                {onOpenSettings && (
                    <Button theme="borderless" type="tertiary" icon={<IconSetting aria-hidden="true" />} onClick={onOpenSettings}>
                        {t('chat.sessionSettingsTitle', null, '会话设置')}
                    </Button>
                )}
                <Button theme="borderless" type="tertiary" icon={<IconRefresh />} onClick={onRefresh}>
                    {t('common.refresh', null, '刷新')}
                </Button>
            </Space>
        </header>
    );
}
