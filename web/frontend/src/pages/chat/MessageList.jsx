/** 消息区：历史消息 + 当前流式输出，自动贴底滚动。 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Avatar, Button, Empty, Space, Spin, Tag, Tooltip, Typography } from '@douyinfe/semi-ui';
import { IconArrowDown, IconComment, IconDelete, IconPaperclip, IconRefresh, IconShield, IconUser } from '@douyinfe/semi-icons';
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
            <Text type="tertiary" className="chat-welcome-brand">CyberStrikeAI</Text>
            <Empty
                image={<span className="chat-welcome-mark" aria-hidden="true"><IconComment size="extra-large" /></span>}
                title={t('chat.welcomeHeading', null, '让安全探索，从对话开始')}
                description={(
                    <Paragraph type="tertiary" className="chat-welcome-description">
                        {t('chat.welcomeScopeDescription', null, '描述测试目标与授权范围，或上传相关材料，即可开始对话。')}
                    </Paragraph>
                )}
            >
                <Space spacing={8} wrap className="chat-welcome-tags">
                    <Tag size="large" type="light" color="blue" prefixIcon={<IconShield />}>
                        {t('chat.welcomeScopeTag', null, '明确授权范围')}
                    </Tag>
                    <Tag size="large" type="light" color="grey" prefixIcon={<IconPaperclip />}>
                        {t('chat.welcomeMaterialsTag', null, '附上相关材料')}
                    </Tag>
                </Space>
            </Empty>
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
                {content && <TurnBody text={content} blocks={blocks} events={null} />}
                {!content && !blocks.length && <Text type="tertiary">—</Text>}
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
    const [atBottom, setAtBottom] = useState(true);

    const list = Array.isArray(messages) ? messages : [];
    const signature = useMemo(() => {
        if (!live) return `${list.length}`;
        const blockSig = live.blocks.map(block => `${block.kind}:${block.id}:${block.text ? block.text.length : 0}:${block.status || ''}`).join('|');
        return `${list.length}#${live.status}#${live.text.length}#${blockSig}`;
    }, [list.length, live]);

    const scrollToBottom = useCallback((smooth = false) => {
        const el = scrollRef.current;
        if (!el) return;
        el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' });
    }, []);

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
        setAtBottom(true);
        const timer = setTimeout(() => scrollToBottom(false), 30);
        return () => clearTimeout(timer);
    }, [conversationId, scrollToBottom]);

    useEffect(() => {
        if (!stickRef.current) return;
        scrollToBottom(false);
    }, [signature, scrollToBottom]);

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
