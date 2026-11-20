/** 关联会话：打开会话、沉淀攻击链、解除项目绑定。 */
import { useNavigate } from 'react-router-dom';
import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { useAuth } from '../../lib/auth';
import { useResource } from '../../components/hooks';
import {
    DataTable, ErrorBanner, SectionCard, confirmAction, toastError, toastSuccess
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { errorProp, errText } from './helpers';

const { Text } = Typography;

export default function ConversationsPanel({ projectId, canWrite, onChanged }) {
    const t = useT();
    const navigate = useNavigate();
    const { hasPermission } = useAuth();
    const canWriteChat = hasPermission('chat:write');

    const conversations = useResource(
        () => api.get(`/projects/${projectId}/conversations`, { limit: 100 }),
        [projectId]
    );

    const rows = conversations.data?.conversations || [];

    const promote = (record) => {
        confirmAction({
            title: t('projects.promoteAttackChain', null, '沉淀攻击链'),
            content: t('projects.confirmPromoteAttackChain', null, '将该对话的攻击链沉淀到本项目？会创建/更新事实与关系边。'),
            okText: t('projects.promoteAttackChain', null, '沉淀攻击链'),
            danger: false,
            onOk: async () => {
                try {
                    const result = await api.post(`/projects/${projectId}/promote-attack-chain/${record.id}`);
                    toastSuccess(t('projects.promoteAttackChainSuccess', {
                        facts_created: result?.facts_created || 0,
                        facts_updated: result?.facts_updated || 0,
                        edges_created: result?.edges_created || 0
                    }, `已沉淀：新建 ${result?.facts_created || 0} / 更新 ${result?.facts_updated || 0} / 边 ${result?.edges_created || 0}`));
                    conversations.reload({ silent: true });
                    onChanged?.();
                } catch (error) {
                    toastError(errText(error, t('projects.promoteAttackChainFailed', null, '沉淀失败')));
                }
            }
        });
    };

    const unbind = (record) => {
        confirmAction({
            title: t('projects.unbind', null, '解绑'),
            content: t('projects.confirmUnbindConversation', null, '解除该对话与当前项目的绑定？'),
            okText: t('projects.unbind', null, '解绑'),
            onOk: async () => {
                try {
                    await api.put(`/conversations/${record.id}/project`, { projectId: '' });
                    toastSuccess(t('projects.projectUnbound', null, '已解除项目绑定'));
                    conversations.reload({ silent: true });
                    onChanged?.();
                } catch (error) {
                    toastError(errText(error, t('projects.unbindFailed', null, '解绑失败')));
                }
            }
        });
    };

    const columns = [
        {
            title: t('projects.titleLabel', null, '标题'),
            dataIndex: 'title',
            render: (value, record) => (
                <button
                    type="button"
                    className="project-link-button"
                    onClick={() => navigate(`/chat?conversation=${encodeURIComponent(record.id)}`)}
                    title={value || record.id}
                >
                    {value || t('projects.untitledConversation', null, '未命名对话')}
                </button>
            )
        },
        {
            title: 'ID',
            dataIndex: 'id',
            width: 240,
            render: value => <Text type="tertiary" size="small"><code>{value}</code></Text>
        },
        {
            title: t('projects.updated', null, '更新'),
            dataIndex: 'updated_at',
            width: 170,
            render: (value, record) => (
                <Text size="small">{formatDateTime(value || record.updatedAt || record.created_at || record.createdAt)}</Text>
            )
        },
        {
            title: t('common.actions', null, '操作'),
            dataIndex: 'id',
            width: 280,
            render: (value, record) => (
                <Space spacing={2} wrap>
                    <Button
                        size="small"
                        theme="borderless"
                        onClick={() => navigate(`/chat?conversation=${encodeURIComponent(record.id)}`)}
                    >
                        {t('projects.open', null, '打开')}
                    </Button>
                    <Button
                        size="small"
                        theme="borderless"
                        disabled={!canWrite}
                        title={t('projects.promoteAttackChainTitle', null, '将对话攻击链沉淀为项目事实与边')}
                        onClick={() => promote(record)}
                    >
                        {t('projects.promoteAttackChain', null, '沉淀攻击链')}
                    </Button>
                    <Button
                        size="small"
                        theme="borderless"
                        disabled={!canWriteChat}
                        title={t('projects.unbindProjectTitle', null, '解除项目绑定')}
                        onClick={() => unbind(record)}
                    >
                        {t('projects.unbind', null, '解绑')}
                    </Button>
                </Space>
            )
        }
    ];

    return (
        <SectionCard
            title={t('projects.tabConversations', null, '关联对话')}
            description={t('projects.boundConversationsHint', null, '绑定到本项目的对话；点击可打开会话')}
            extra={<Tag size="small" color="blue" type="light">{rows.length}</Tag>}
        >
            <ErrorBanner error={errorProp(conversations.error)} onRetry={conversations.reload} />
            <DataTable
                columns={columns}
                dataSource={rows}
                rowKey="id"
                loading={conversations.loading}
                pagination={{ pageSize: 10 }}
                emptyText={t('projects.noBoundConversations', null, '暂无绑定对话；在对话页选择本项目即可关联')}
                onRefresh={() => conversations.reload()}
            />
        </SectionCard>
    );
}
