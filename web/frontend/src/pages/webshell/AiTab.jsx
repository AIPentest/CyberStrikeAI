/**
 * WebShell AI 会话：列出该连接绑定的 AI 对话并只读查看历史消息。
 */
import { useCallback, useEffect, useState } from 'react';
import { Button, Empty, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useT } from '../../lib/i18n';
import { ErrorBanner, MarkdownView } from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { getAiHistory, getConversation, listAiConversations } from './api';

const { Text } = Typography;

const ROLE_COLOR = { user: 'blue', assistant: 'green', system: 'grey', tool: 'violet' };

export default function AiTab({ connection }) {
    const t = useT();
    const [conversations, setConversations] = useState([]);
    const [activeId, setActiveId] = useState('');
    const [messages, setMessages] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);

    const load = useCallback(async () => {
        if (!connection?.id) return;
        setLoading(true);
        setError(null);
        try {
            const [list, history] = await Promise.all([
                listAiConversations(connection.id).catch(() => []),
                getAiHistory(connection.id).catch(() => null)
            ]);
            const items = Array.isArray(list) ? list : [];
            setConversations(items);
            const current = history?.conversationId || items[0]?.id || '';
            setActiveId(current);
            if (history?.conversationId) {
                setMessages(Array.isArray(history.messages) ? history.messages : []);
            } else if (current) {
                const conversation = await getConversation(current).catch(() => null);
                setMessages(Array.isArray(conversation?.messages) ? conversation.messages : []);
            } else {
                setMessages([]);
            }
        } catch (err) {
            setError(err);
        } finally {
            setLoading(false);
        }
    }, [connection?.id]);

    useEffect(() => { load(); }, [load]);

    const openConversation = async id => {
        if (!id) return;
        setActiveId(id);
        setLoading(true);
        try {
            const conversation = await getConversation(id);
            setMessages(Array.isArray(conversation?.messages) ? conversation.messages : []);
        } catch (err) {
            setError(err);
            setMessages([]);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="webshell-ai">
            <div className="webshell-ai-sidebar">
                <div className="webshell-ai-sidebar-head">
                    <Text strong>{t('webshell.aiPreviousConversation', null, '之前的对话')}</Text>
                    <Button size="small" theme="borderless" icon={<IconRefresh />} onClick={load} loading={loading} />
                </div>
                {conversations.length === 0 ? (
                    <Empty title={t('webshell.aiNoConversation', null, '暂无 AI 会话')} description={null} style={{ padding: 24 }} />
                ) : (
                    <ul className="webshell-ai-list">
                        {conversations.map(item => (
                            <li key={item.id}>
                                <button
                                    type="button"
                                    className={`webshell-ai-item ${item.id === activeId ? 'is-active' : ''}`}
                                    onClick={() => openConversation(item.id)}
                                >
                                    <span className="webshell-ai-title">{item.title || item.id}</span>
                                    <span className="webshell-ai-time">{formatDateTime(item.updatedAt)}</span>
                                </button>
                            </li>
                        ))}
                    </ul>
                )}
            </div>
            <div className="webshell-ai-main">
                <ErrorBanner error={error} onRetry={load} />
                {loading && <div className="webshell-ai-loading"><Spin /></div>}
                {!loading && messages.length === 0 && (
                    <Empty title={t('webshell.aiNoMessage', null, '暂无对话消息')} description={null} style={{ padding: 48 }} />
                )}
                {!loading && messages.map(message => (
                    <div className="webshell-ai-message" key={message.id || `${message.role}-${message.createdAt}`}>
                        <div className="webshell-ai-message-head">
                            <Tag size="small" color={ROLE_COLOR[message.role] || 'grey'}>{message.role || 'message'}</Tag>
                            <Text type="tertiary" size="small">{formatDateTime(message.createdAt)}</Text>
                        </div>
                        <MarkdownView content={message.content || ''} />
                    </div>
                ))}
            </div>
        </div>
    );
}
