/**
 * 对话流式事件归约。
 * 后端（/agent/stream、/multi-agent/stream）逐条推送 { type, message, data }，
 * 这里把事件折叠成可直接渲染的块列表（正文 / 推理 / 工具 / 迭代 / 提示）。
 * 未识别的复杂事件不丢弃：统一收进 events，由界面折叠展示原始 JSON。
 */

export const PLAN_TOOL_NAMES = new Set(['taskcreate', 'taskupdate', 'tasklist', 'taskget', 'write_todos']);

const SILENT_EVENT_TYPES = new Set([
    'heartbeat',
    'response_delta',
    'thinking_stream_delta',
    'reasoning_chain_stream_delta',
    'tool_result_delta'
]);

const MAX_EVENTS = 200;
const MAX_BLOCK_TEXT = 20000;

function str(value) {
    return value === undefined || value === null ? '' : String(value);
}

function asObject(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : null;
}

export function truncateText(value, limit = MAX_BLOCK_TEXT) {
    const text = str(value);
    if (text.length <= limit) return text;
    return `${text.slice(0, limit)}\n…（已截断，共 ${text.length} 字符）`;
}

export function prettyJson(value, limit = 6000) {
    if (value === undefined || value === null) return '';
    try {
        const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
        return truncateText(text, limit);
    } catch (error) {
        return truncateText(String(value), limit);
    }
}

export function createLiveState({ conversationId = '', id } = {}) {
    return {
        id: id || `live-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
        conversationId,
        status: 'streaming',
        text: '',
        blocks: [],
        events: [],
        error: '',
        notice: '',
        statusText: '',
        messageId: '',
        userMessageId: '',
        finalizable: null,
        startedAt: Date.now(),
        endedAt: 0,
        seq: 0
    };
}

function nextKey(state, prefix) {
    state.seq += 1;
    return `${prefix}-${state.seq}`;
}

function pushRawEvent(state, event) {
    const type = str(event.type);
    if (SILENT_EVENT_TYPES.has(type)) return;
    state.events.push({
        at: Date.now(),
        type,
        message: str(event.message),
        data: event.data === undefined ? null : event.data
    });
    if (state.events.length > MAX_EVENTS) state.events.splice(0, state.events.length - MAX_EVENTS);
}

function pushNote(state, { title, message, tone = 'info', data = null }) {
    const last = state.blocks[state.blocks.length - 1];
    if (last && last.kind === 'note' && last.title === title && last.message === str(message)) return last;
    const block = {
        kind: 'note',
        id: nextKey(state, 'note'),
        title,
        message: truncateText(message),
        tone,
        data
    };
    state.blocks.push(block);
    return block;
}

function upsertReasoning(state, streamId, kind = 'thinking') {
    const key = str(streamId) || nextKey(state, 'reasoning');
    let block = key ? state.blocks.find(item => item.kind === 'reasoning' && item.key === key) : null;
    if (!block) {
        block = { kind: 'reasoning', id: nextKey(state, 'reason'), key, stream: str(streamId), label: kind, text: '' };
        state.blocks.push(block);
    }
    return block;
}

function upsertTool(state, data) {
    const toolCallId = str(data.toolCallId);
    let tool = null;
    if (toolCallId) {
        tool = state.blocks.find(item => item.kind === 'tool' && item.toolCallId === toolCallId);
    }
    if (!tool) {
        tool = {
            kind: 'tool',
            id: nextKey(state, 'tool'),
            toolCallId,
            toolName: str(data.toolName) || '未知工具',
            args: null,
            result: '',
            status: 'running',
            processDetailId: str(data.processDetailId),
            agentName: str(data.agentName)
        };
        state.blocks.push(tool);
    }
    if (data.toolName && (tool.toolName === '未知工具' || !tool.toolName)) tool.toolName = str(data.toolName);
    if (data.agentName && !tool.agentName) tool.agentName = str(data.agentName);
    if (data.processDetailId && !tool.processDetailId) tool.processDetailId = str(data.processDetailId);
    return tool;
}

function toolStatusFromData(data) {
    const explicit = str(data.displayStatus || data.status).toLowerCase();
    if (explicit === 'cancelled' || explicit === 'canceled') return 'cancelled';
    if (explicit === 'background_running') return 'running';
    if (explicit === 'blocked') return 'blocked';
    if (data.isError === true || data.success === false) return 'failed';
    return 'success';
}

/**
 * 归约单个 SSE 事件（原地修改 state，调用方负责触发重渲染）。
 * 返回 state 本身，便于链式使用。
 */
export function applyStreamEvent(state, event) {
    if (!state || !event || typeof event !== 'object') return state;
    const type = str(event.type);
    const data = asObject(event.data) || {};

    pushRawEvent(state, event);

    switch (type) {
        case 'heartbeat':
            break;
        case 'conversation':
            if (data.conversationId) state.conversationId = str(data.conversationId);
            break;
        case 'message_saved':
            if (data.userMessageId) state.userMessageId = str(data.userMessageId);
            break;
        case 'progress':
            state.statusText = str(event.message) || state.statusText;
            break;
        case 'iteration': {
            const n = Number(data.iteration) || 1;
            const scope = str(data.piScope).trim();
            const agentName = str(data.agentName).trim();
            const phase = scope === 'main'
                ? (agentName ? `主代理 · ${agentName}` : '主代理')
                : (scope === 'sub' ? `子代理${agentName ? ` · ${agentName}` : ''}` : '');
            state.blocks.push({
                kind: 'iteration',
                id: nextKey(state, 'iter'),
                title: `第 ${n} 轮迭代${phase ? ` · ${phase}` : ''}`,
                message: truncateText(event.message, 2000)
            });
            break;
        }
        case 'thinking_stream_start':
        case 'reasoning_chain_stream_start': {
            const block = upsertReasoning(state, data.streamId, type === 'reasoning_chain_stream_start' ? 'reasoning_chain' : 'thinking');
            block.text = '';
            break;
        }
        case 'thinking_stream_delta':
        case 'reasoning_chain_stream_delta': {
            const block = upsertReasoning(state, data.streamId, type === 'reasoning_chain_stream_delta' ? 'reasoning_chain' : 'thinking');
            const accumulated = typeof data.accumulated === 'string' ? data.accumulated : null;
            block.text = truncateText(accumulated !== null ? accumulated : block.text + str(event.message));
            break;
        }
        case 'thinking':
        case 'reasoning_chain': {
            if (data.streamId) {
                const block = upsertReasoning(state, data.streamId, type === 'reasoning_chain' ? 'reasoning_chain' : 'thinking');
                if (str(event.message)) block.text = truncateText(event.message);
                break;
            }
            state.blocks.push({
                kind: 'reasoning',
                id: nextKey(state, 'reason'),
                key: nextKey(state, 'reasonkey'),
                stream: '',
                label: type === 'reasoning_chain' ? 'reasoning_chain' : 'thinking',
                text: truncateText(event.message)
            });
            break;
        }
        case 'response_start':
            if (data.conversationId) state.conversationId = str(data.conversationId);
            if (!state.statusText) state.statusText = '正在生成回复…';
            break;
        case 'response_delta': {
            const accumulated = typeof data.accumulated === 'string' ? data.accumulated : null;
            state.text = truncateText(accumulated !== null ? accumulated : state.text + str(event.message), 60000);
            break;
        }
        case 'response': {
            const finalText = str(event.message);
            if (finalText) state.text = truncateText(finalText, 60000);
            if (data.messageId) state.messageId = str(data.messageId);
            if (data.conversationId) state.conversationId = str(data.conversationId);
            if (data.finalizable === false) state.finalizable = false;
            else if (data.finalizable === true) state.finalizable = true;
            break;
        }
        case 'tool_call': {
            const tool = upsertTool(state, data);
            tool.args = data.argumentsObj !== undefined ? data.argumentsObj : (data.arguments !== undefined ? data.arguments : tool.args);
            break;
        }
        case 'tool_result': {
            const tool = upsertTool(state, data);
            tool.status = toolStatusFromData(data);
            const preview = data.resultPreview !== undefined ? data.resultPreview : data.result;
            tool.result = truncateText(preview !== undefined && preview !== null ? preview : event.message, 8000);
            break;
        }
        case 'tool_calls_detected':
            pushNote(state, { title: '🔧 检测到工具调用', message: event.message, data });
            break;
        case 'hitl_interrupt':
            pushNote(state, { title: '🧑‍⚖️ 等待人工审批', message: `${str(data.toolName) || '工具'} · ${str(event.message) || '命中审批策略'}`, tone: 'warning', data });
            break;
        case 'hitl_resumed':
            pushNote(state, { title: '✅ 审批通过', message: str(event.message), tone: 'success', data });
            break;
        case 'hitl_rejected':
            pushNote(state, { title: '⛔ 审批被拒绝', message: str(event.message), tone: 'danger', data });
            break;
        case 'hitl_audit_agent_started':
            pushNote(state, { title: '🛡 审计代理审查中', message: str(event.message), tone: 'info', data });
            break;
        case 'hitl_audit_agent':
            pushNote(state, {
                title: '🛡 审计代理已裁决',
                message: `${str(data.decision) || '-'}${data.comment ? ` · ${str(data.comment)}` : ''}`,
                tone: 'info',
                data
            });
            break;
        case 'warning':
        case 'iteration_limit_reached':
        case 'pi_pending_orphaned':
            pushNote(state, { title: `⚠️ ${str(event.message) || '提示'}`, message: str(event.message), tone: 'warning', data });
            break;
        case 'finalization_check':
            pushNote(state, { title: '🔎 最终化校验未通过', message: str(event.message), tone: 'warning', data });
            if (data.finalizable === false) state.finalizable = false;
            break;
        case 'finalization_auto_continue':
        case 'finalization_pending_tools_cancelled':
        case 'agent_empty_response_continue':
        case 'agent_run_retry':
        case 'pi_model_retry':
        case 'pi_model_failover':
        case 'pi_stream_error':
            pushNote(state, { title: str(event.message) || '运行提示', message: str(event.message), tone: 'warning', data });
            break;
        case 'agent_usage_summary': {
            const calls = Number(data.modelCalls) || 0;
            const total = Number(data.totalTokens) || 0;
            pushNote(state, {
                title: '📊 本轮用量',
                message: `${calls} 次模型调用 · ${total} tokens`,
                tone: 'info',
                data
            });
            break;
        }
        case 'user_interrupt_continue':
            pushNote(state, { title: '⏸️ 用户中断并继续', message: str(event.message), tone: 'info', data });
            break;
        case 'agent_reply':
            pushNote(state, { title: `💬 ${str(data.agentName) || '子代理'}回复`, message: str(event.message), data });
            break;
        case 'workflow_start':
        case 'workflow_done':
        case 'workflow_node_start':
        case 'workflow_node_result':
        case 'workflow_branch_taken':
        case 'workflow_branch_skipped':
        case 'workflow_tool_start':
        case 'workflow_agent_output':
        case 'workflow_hitl_checkpoint':
        case 'workflow_hitl_waiting':
        case 'workflow_hitl_resumed':
        case 'workflow_hitl_rejected':
        case 'workflow_paused':
            pushNote(state, { title: `🧭 ${str(event.message) || type}`, message: str(event.message), data });
            break;
        case 'cancelled':
            state.status = 'cancelled';
            state.notice = str(event.message) || '任务已取消';
            state.endedAt = Date.now();
            break;
        case 'error':
            state.status = 'error';
            state.error = str(event.message) || '执行失败';
            state.endedAt = Date.now();
            break;
        case 'done':
            if (state.status === 'streaming') {
                state.status = data.status === 'blocked' || state.finalizable === false ? 'blocked' : 'done';
            }
            if (data.workflowStatus === 'awaiting_hitl') state.statusText = '工作流等待审批';
            state.endedAt = Date.now();
            break;
        default:
            // 已知但未单独建模的事件（agent_trace_*、iteration 之外的诊断事件）只进原始日志。
            break;
    }
    return state;
}

export function isLiveStreaming(live) {
    return Boolean(live) && live.status === 'streaming';
}

export const LIVE_STATUS_META = {
    streaming: { label: '生成中', color: 'blue' },
    done: { label: '已完成', color: 'green' },
    blocked: { label: '未完成', color: 'orange' },
    disconnected: { label: '连接已结束', color: 'grey' },
    error: { label: '失败', color: 'red' },
    cancelled: { label: '已取消', color: 'grey' }
};

/** 历史消息的过程详情 → 统一渲染块（与流式块的字段保持一致）。 */
export function buildHistoryBlocks(message) {
    const blocks = [];
    let seq = 0;
    const key = prefix => {
        seq += 1;
        return `h-${prefix}-${seq}`;
    };

    const reasoningContent = str(message?.reasoningContent).trim();
    if (reasoningContent) {
        blocks.push({ kind: 'reasoning', id: key('reason'), key: 'reasoningContent', stream: '', label: 'thinking', text: truncateText(reasoningContent) });
    }

    const details = Array.isArray(message?.processDetails) ? message.processDetails : [];
    const toolIndex = new Map();

    details.forEach(detail => {
        const eventType = str(detail?.eventType);
        const data = asObject(detail?.data) || {};
        const text = str(detail?.message);

        switch (eventType) {
            case 'iteration': {
                const n = Number(data.iteration) || 1;
                const scope = str(data.piScope).trim();
                const agent = str(data.agentName).trim();
                blocks.push({
                    kind: 'iteration',
                    id: key('iter'),
                    title: `第 ${n} 轮迭代${scope === 'main' ? ' · 主代理' : (scope === 'sub' ? ' · 子代理' : '')}${agent ? ` · ${agent}` : ''}`,
                    message: truncateText(text, 2000)
                });
                break;
            }
            case 'thinking':
            case 'reasoning_chain': {
                if (!text.trim()) break;
                blocks.push({
                    kind: 'reasoning',
                    id: key('reason'),
                    key: str(data.streamId) || key('reasonkey'),
                    stream: str(data.streamId),
                    label: eventType,
                    text: truncateText(text)
                });
                break;
            }
            case 'tool_call': {
                const toolCallId = str(data.toolCallId);
                const tool = {
                    kind: 'tool',
                    id: key('tool'),
                    toolCallId,
                    toolName: str(data.toolName) || '未知工具',
                    args: data.argumentsObj !== undefined ? data.argumentsObj : (data.arguments !== undefined ? data.arguments : null),
                    result: '',
                    status: 'running',
                    processDetailId: str(detail?.id),
                    agentName: str(data.agentName)
                };
                if (toolCallId) toolIndex.set(toolCallId, tool);
                blocks.push(tool);
                break;
            }
            case 'tool_result': {
                const toolCallId = str(data.toolCallId);
                let tool = toolCallId ? toolIndex.get(toolCallId) : null;
                if (!tool) {
                    tool = {
                        kind: 'tool',
                        id: key('tool'),
                        toolCallId,
                        toolName: str(data.toolName) || '未知工具',
                        args: null,
                        result: '',
                        status: 'running',
                        processDetailId: '',
                        agentName: str(data.agentName)
                    };
                    if (toolCallId) toolIndex.set(toolCallId, tool);
                    blocks.push(tool);
                }
                tool.status = toolStatusFromData(data);
                const preview = data.resultPreview !== undefined ? data.resultPreview : data.result;
                tool.result = truncateText(preview !== undefined && preview !== null ? preview : text, 8000);
                break;
            }
            case 'planning': {
                if (!text.trim()) break;
                blocks.push({ kind: 'note', id: key('note'), title: '📝 规划', message: truncateText(text, 4000), tone: 'info', data: null });
                break;
            }
            case 'hitl_interrupt':
            case 'hitl_resumed':
            case 'hitl_rejected':
            case 'hitl_audit_agent':
            case 'hitl_audit_agent_started': {
                const tone = eventType === 'hitl_rejected' ? 'danger' : (eventType === 'hitl_resumed' ? 'success' : 'info');
                const title = eventType === 'hitl_interrupt'
                    ? '🧑‍⚖️ 人工审批'
                    : (eventType === 'hitl_rejected' ? '⛔ 审批被拒绝' : (eventType === 'hitl_resumed' ? '✅ 审批通过' : '🛡 审计代理'));
                blocks.push({
                    kind: 'note',
                    id: key('note'),
                    title,
                    message: `${text}${data.toolName ? ` · ${str(data.toolName)}` : ''}${data.decision ? ` · ${str(data.decision)}` : ''}`,
                    tone,
                    data: null
                });
                break;
            }
            case 'error':
            case 'timeout':
            case 'cancelled':
                blocks.push({ kind: 'note', id: key('note'), title: eventType === 'cancelled' ? '⏹ 已取消' : '❌ 执行异常', message: text, tone: 'danger', data: null });
                break;
            case 'agent_reply':
                blocks.push({ kind: 'note', id: key('note'), title: '💬 子代理回复', message: truncateText(text, 6000), tone: 'info', data: null });
                break;
            case 'progress':
                // 心跳/进度文案不入时间线，避免历史回放噪音。
                break;
            default:
                if (text.trim() || Object.keys(data).length) {
                    blocks.push({
                        kind: 'note',
                        id: key('note'),
                        title: str(eventType) || '过程事件',
                        message: truncateText(text, 4000),
                        tone: 'info',
                        data: Object.keys(data).length ? data : null
                    });
                }
                break;
        }
    });

    return blocks;
}
