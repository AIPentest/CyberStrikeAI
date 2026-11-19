/** 单条助手输出：正文 Markdown + 过程块（推理/工具/迭代/提示）+ 原始事件折叠。 */
import { useState } from 'react';
import { Collapse, Tag, Typography } from '@douyinfe/semi-ui';
import { MarkdownView, JsonViewer } from '../../components/ui';
import { formatNumber } from '../../lib/format';
import { useI18n } from '../../lib/i18n';
import { prettyJson, truncateText } from './stream';

const { Text } = Typography;

const STATUS_TAG = {
    running: { color: 'blue', label: '执行中' },
    success: { color: 'green', label: '完成' },
    failed: { color: 'red', label: '失败' },
    cancelled: { color: 'grey', label: '已取消' },
    blocked: { color: 'orange', label: '已拦截' }
};

function ReasoningBlock({ block, defaultOpen }) {
    const text = block.text || '';
    if (!text.trim()) return null;
    const title = block.label === 'reasoning_chain' ? '🔗 推理过程' : '🤔 思考过程';
    return (
        <Collapse className="chat-block-collapse" defaultActiveKey={defaultOpen ? ['1'] : []}>
            <Collapse.Panel itemKey="1" header={<span className="chat-block-title">{title} · {text.length} 字符</span>}>
                <pre className="chat-pre">{truncateText(text)}</pre>
            </Collapse.Panel>
        </Collapse>
    );
}

function ToolBlock({ block }) {
    const meta = STATUS_TAG[block.status] || STATUS_TAG.running;
    const argsText = prettyJson(block.args);
    const resultText = block.result || '';
    const hasBody = Boolean(argsText || resultText);
    const header = (
        <span className="chat-tool-head">
            <span className="chat-tool-name">{block.agentName ? `${block.agentName} · ` : ''}{block.toolName || '未知工具'}</span>
            <Tag size="small" color={meta.color} shape="circle">{meta.label}</Tag>
        </span>
    );
    if (!hasBody) {
        return <div className="chat-tool"><div className="chat-tool-head-row">{header}</div></div>;
    }
    return (
        <div className="chat-tool">
            <Collapse className="chat-block-collapse">
                <Collapse.Panel itemKey="1" header={header}>
                    {argsText && (
                        <>
                            <Text type="tertiary" size="small">参数</Text>
                            <pre className="chat-pre">{argsText}</pre>
                        </>
                    )}
                    {resultText && (
                        <>
                            <Text type="tertiary" size="small">结果</Text>
                            <pre className="chat-pre">{resultText}</pre>
                        </>
                    )}
                </Collapse.Panel>
            </Collapse>
        </div>
    );
}

function NoteBlock({ block }) {
    const [open, setOpen] = useState(false);
    const tone = block.tone || 'info';
    return (
        <div className={`chat-note chat-note-${tone}`}>
            <div className="chat-note-head">
                <span className="chat-note-title">{block.title}</span>
                {block.data && (
                    <button type="button" className="chat-link-btn" onClick={() => setOpen(!open)}>
                        {open ? '收起 JSON' : '查看 JSON'}
                    </button>
                )}
            </div>
            {block.message && <div className="chat-note-body">{block.message}</div>}
            {open && block.data && <JsonViewer value={block.data} collapsed={false} title="事件数据" />}
        </div>
    );
}

function UsageSummaryBlock({ data }) {
    const { t, lang } = useI18n();
    const number = value => formatNumber(Math.max(0, Math.trunc(Number(value) || 0)), lang);
    const rows = [
        ['chat.piUsageModelCalls', data.modelCalls],
        ['chat.piUsagePromptTokens', data.promptTokens],
        ['chat.piUsageCompletionTokens', data.completionTokens],
        ['chat.piUsageTotalTokens', data.totalTokens]
    ];
    const optionalRows = [
        ['chat.piUsageCachedTokens', data.cachedTokens],
        ['chat.piUsageCacheWriteTokens', data.cacheWriteTokens],
        ['chat.piUsageReasoningTokens', data.reasoningTokens]
    ];
    const message = [...rows, ...optionalRows.filter(([, count]) => Number(count) > 0)]
        .map(([label, count]) => `${t(label)}: ${number(count)}`)
        .join('\n');
    const total = Number(data.totalTokens);
    const title = [
        data.agentName,
        t('chat.piUsageSummaryTitle'),
        Number.isFinite(total) && total > 0 ? number(total) : ''
    ].filter(Boolean).join(' · ');
    return <NoteBlock block={{ title, message, data }} />;
}

function EventLog({ events }) {
    if (!events || !events.length) return null;
    return (
        <Collapse className="chat-block-collapse chat-event-log">
            <Collapse.Panel itemKey="1" header={<span className="chat-block-title">🧾 原始事件 · {events.length} 条</span>}>
                <pre className="chat-pre">
                    {events.map((event, index) => `${index + 1}. ${JSON.stringify(event)}`).join('\n')}
                </pre>
            </Collapse.Panel>
        </Collapse>
    );
}

export default function TurnBody({
    text,
    blocks,
    events,
    streaming = false,
    openReasoning = false
}) {
    const list = Array.isArray(blocks) ? blocks : [];
    const body = text || '';
    return (
        <div className="chat-turn-body">
            {body && (streaming
                ? <pre className="chat-pre chat-stream-text">{body}</pre>
                : <MarkdownView content={body} />)}
            {!body && streaming && <div className="chat-stream-placeholder">正在生成…</div>}
            {list.map(block => {
                if (block.kind === 'reasoning') {
                    return <ReasoningBlock key={block.id} block={block} defaultOpen={openReasoning} />;
                }
                if (block.kind === 'tool') {
                    return <ToolBlock key={block.id} block={block} />;
                }
                if (block.kind === 'iteration') {
                    return (
                        <div className="chat-iteration" key={block.id}>
                            <span className="chat-iteration-label">{block.title}</span>
                        </div>
                    );
                }
                if (block.kind === 'note') {
                    return <NoteBlock key={block.id} block={block} />;
                }
                if (block.kind === 'usage') {
                    return <UsageSummaryBlock key={block.id} data={block.data} />;
                }
                return null;
            })}
            <EventLog events={events} />
        </div>
    );
}
