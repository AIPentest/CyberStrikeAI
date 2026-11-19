/** 消息区：运行中跟随输出，结束后定位到最终总结的开头。 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Avatar, Button, Empty, Spin, Tag, Tooltip, Typography } from '@douyinfe/semi-ui';
import { IconArrowDown, IconComment, IconDelete, IconRefresh, IconUser } from '@douyinfe/semi-icons';
import { CopyButton, ErrorBanner } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { formatDateTime } from '../../lib/format';
import TurnBody from './TurnBody';
import { LIVE_STATUS_META, buildHistoryBlocks } from './stream';

const { Paragraph, Text } = Typography;

function ChatWelcome() {
    const t = useT();
    return (
        <div className="chat-welcome">
            <Empty
                image={<span className="chat-welcome-mark" aria-hidden="true"><IconComment size="extra-large" /></span>}
                title={t('chat.welcomeHeading', null, '让安全探索，从对话开始')}
                description={(
                    <Paragraph type="tertiary" className="chat-welcome-description">
                        {t('chat.welcomeScopeDescription', null, '描述测试目标与授权范围，或上传相关材料，即可开始对话。')}
                    </Paragraph>
                )}
            />
        </div>
    );
}

function useElapsed(startedAt, endedAt, active) {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        if (!active) return undefined;
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, [active]);
    if (!startedAt) return '';
    const end = active ? now : (endedAt || startedAt);
    const seconds = Math.max(0, Math.round((end - startedAt) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    return `${minutes}m${seconds % 60}s`;
}

function MessageRow({ message, canDelete, onDeleteTurn }) {
    const t = useT();
    const isUser = message.role === 'user';
    const isSystem = message.role === 'system';
    const blocks = useMemo(() => buildHistoryBlocks(message), [message]);
    const time = message.createdAt ? formatDateTime(message.createdAt) : '';
    const content = String(message.content || '');

    if (isSystem) {
        return (
            <div className="chat-system-row" key={message.id}>
                <Text type="tertiary">{content}</Text>
            </div>
        );
    }

    return (
        <div className={`chat-msg ${isUser ? 'chat-msg-user' : 'chat-msg-assistant'}`} key={message.id}>
            <Avatar size="small" color={isUser ? 'grey' : 'blue'} className="chat-avatar" aria-hidden="true">
                {isUser ? <IconUser /> : 'AI'}
            </Avatar>
            <div className="chat-bubble">
                <div className="chat-bubble-head">
                    <span className="chat-bubble-role">{isUser ? t('chat.you', null, '我') : t('chat.assistant', null, '助手')}</span>
                    {time && <span className="chat-bubble-time">{time}</span>}
                    <span className="chat-bubble-actions">
                        {!isUser && content && <CopyButton text={content} size="small" />}
                        {canDelete && isUser && message.id && (
                            <Tooltip content={t('chat.deleteTurnTitle', null, '删除本轮')}>
                                <Button
                                    size="small"
                                    theme="borderless"
                                    type="danger"
                                    icon={<IconDelete />}
                                    aria-label={t('chat.deleteTurnTitle', null, '删除本轮')}
                                    onClick={() => onDeleteTurn(message)}
                                />
                            </Tooltip>
                        )}
                    </span>
                </div>
                {content || blocks.length > 0
                    ? <TurnBody text={content} blocks={blocks} events={null} isAssistant={!isUser} />
                    : <Text type="tertiary">—</Text>}
            </div>
        </div>
    );
}

function LiveRow({ live }) {
    const t = useT();
    const meta = LIVE_STATUS_META[live.status] || LIVE_STATUS_META.streaming;
    const elapsed = useElapsed(live.startedAt, live.endedAt, live.status === 'streaming');
    return (
        <div className="chat-msg chat-msg-assistant chat-msg-live">
            <Avatar size="small" color="blue" className="chat-avatar">AI</Avatar>
            <div className="chat-bubble">
                <div className="chat-bubble-head">
                    <span className="chat-bubble-role">{t('chat.assistant', null, '助手')}</span>
                    <Tag size="small" color={meta.color} shape="circle">{meta.label}</Tag>
                    {elapsed && <span className="chat-bubble-time">{elapsed}</span>}
                    {live.statusText && <span className="chat-live-status">{live.statusText}</span>}
                </div>
                <TurnBody
                    isAssistant
                    text={live.text}
                    blocks={live.blocks}
                    events={live.events}
                    streaming={live.status === 'streaming'}
                    openReasoning={live.status === 'streaming'}
                />
                {live.error && <div className="chat-live-error">{live.error}</div>}
                {live.notice && <div className="chat-live-notice">{live.notice}</div>}
                {live.finalizable === false && (
                    <div className="chat-live-notice">本轮回复未通过最终化校验，内容可能不完整。</div>
                )}
            </div>
        </div>
    );
}

export default function MessageList({
    conversationId,
    messages,
    live,
    loading,
    error,
    onRetry,
    canDelete,
    onDeleteTurn
}) {
    const t = useT();
    const scrollRef = useRef(null);
    const stickRef = useRef(true);
    const positionedSummaryRef = useRef('');
    const [atBottom, setAtBottom] = useState(true);

    const list = Array.isArray(messages) ? messages : [];
    const lastMessage = list[list.length - 1];
    const historySignature = `${list.length}:${lastMessage?.id || ''}:${lastMessage?.content || ''}:${lastMessage?.processDetails?.length || 0}`;
    const summaryKey = live
        ? (live.status !== 'streaming' && live.text.trim() ? `live:${conversationId}:${live.messageId || live.id}:${live.text}` : '')
        : (!loading && lastMessage?.role === 'assistant' && String(lastMessage.content || '').trim() ? `history:${conversationId}:${lastMessage.id}:${lastMessage.content}` : '');
    const signature = useMemo(() => {
        if (!live) return historySignature;
        const blockSig = live.blocks.map(block => `${block.kind}:${block.id}:${block.text ? block.text.length : 0}:${block.status || ''}`).join('|');
        return `${historySignature}#${live.id}#${live.status}#${live.text.length}#${blockSig}`;
    }, [historySignature, live]);

    const scrollToBottom = useCallback((smooth = false) => {
        const el = scrollRef.current;
        if (!el) return;
        el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' });
    }, []);

    const scrollToLatest = useCallback((smooth = false) => {
        const el = scrollRef.current;
        if (!el) return;
        const rows = el.querySelectorAll('.chat-msg');
        const summary = rows[rows.length - 1]?.querySelector('[data-chat-summary]');
        if (!summary) {
            scrollToBottom(smooth);
            return;
        }
        // Use the summary's start, so long reports open at their heading rather
        // than at their last paragraph. Only scroll the message pane.
        const top = summary.getBoundingClientRect().top - el.getBoundingClientRect().top + el.scrollTop - 12;
        el.scrollTo({ top: Math.max(0, top), behavior: smooth ? 'smooth' : 'auto' });
    }, [scrollToBottom]);

    const handleScroll = useCallback(() => {
        const el = scrollRef.current;
        if (!el) return;
        const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
        const stick = gap < 120;
        stickRef.current = stick;
        setAtBottom(stick);
    }, []);

    useEffect(() => {
        stickRef.current = true;
        positionedSummaryRef.current = '';
        setAtBottom(true);
        const timer = setTimeout(() => scrollToLatest(false), 30);
        return () => clearTimeout(timer);
    }, [conversationId, scrollToLatest]);

    useEffect(() => {
        if (live?.id) stickRef.current = true;
    }, [live?.id]);

    useEffect(() => {
        if (loading) return;
        const newlyCompleted = summaryKey && positionedSummaryRef.current !== summaryKey;
        if (newlyCompleted) positionedSummaryRef.current = summaryKey;
        if (newlyCompleted || stickRef.current) scrollToLatest(false);
    }, [signature, summaryKey, loading, scrollToLatest]);

    const hasContent = Boolean(conversationId || live || list.length);

    if (!hasContent) {
        return (
            <div className="chat-message-wrap">
                <div className="chat-messages" ref={scrollRef}>
                    <ChatWelcome />
                </div>
            </div>
        );
    }

    return (
        <div className="chat-message-wrap">
            <div className="chat-messages" ref={scrollRef} onScroll={handleScroll}>
                <ErrorBanner error={error} onRetry={onRetry} />
                {loading && (
                    <div className="chat-loading"><Spin size="middle" /></div>
                )}
                {!loading && !list.length && !live && (
                    <ChatWelcome />
                )}
                {list.map(message => (
                    <MessageRow
                        key={message.id}
                        message={message}
                        canDelete={canDelete}
                        onDeleteTurn={onDeleteTurn}
                    />
                ))}
                {live && <LiveRow live={live} />}
                {error && !loading && (
                    <Button icon={<IconRefresh />} theme="borderless" onClick={onRetry} className="chat-retry-inline">
                        {t('common.refresh', null, '刷新')}
                    </Button>
                )}
            </div>
            {!atBottom && (
                <Button
                    className="chat-scroll-bottom"
                    icon={<IconArrowDown />}
                    size="small"
                    theme="solid"
                    onClick={() => {
                        stickRef.current = true;
                        setAtBottom(true);
                        scrollToBottom(true);
                    }}
                >
                    {t('chat.returnToLatest', null, '回到底部')}
                </Button>
            )}
        </div>
    );
}
