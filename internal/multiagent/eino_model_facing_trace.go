package multiagent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// modelFacingTraceHolder 保存「即将送入 ChatModel」的消息快照（已走 summarization / reduction / orphan 修剪等），
// 用于 last_react_input 落库，使续跑与「上下文压缩后」的模型视角一致，而非仅依赖事件流 append 的 runAccumulatedMsgs。
type modelFacingTraceHolder struct {
	mu sync.Mutex
	// msgs 为深拷贝后的切片，避免框架后续原地修改污染快照
	msgs []adk.Message
}

func newModelFacingTraceHolder() *modelFacingTraceHolder {
	return &modelFacingTraceHolder{}
}

// Snapshot 返回当前快照的再一次深拷贝（供序列化落库，避免与 holder 互斥长期持锁）。
func (h *modelFacingTraceHolder) Snapshot() []adk.Message {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneADKMessagesForTrace(h.msgs)
}

func (h *modelFacingTraceHolder) storeFromState(state *adk.ChatModelAgentState) {
	if h == nil || state == nil || len(state.Messages) == 0 {
		return
	}
	cloned := cloneADKMessagesForTrace(state.Messages)
	if len(cloned) == 0 {
		return
	}
	h.mu.Lock()
	h.msgs = cloned
	h.mu.Unlock()
}

func (h *modelFacingTraceHolder) storeFromAgenticState(state *adk.TypedChatModelAgentState[*schema.AgenticMessage]) {
	if h == nil || state == nil || len(state.Messages) == 0 {
		return
	}
	cloned := cloneADKMessagesForTrace(AgenticMessagesToEino(state.Messages))
	if len(cloned) == 0 {
		return
	}
	h.mu.Lock()
	h.msgs = cloned
	h.mu.Unlock()
}

func cloneADKMessagesForTrace(msgs []adk.Message) []adk.Message {
	if len(msgs) == 0 {
		return nil
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		return nil
	}
	var out []adk.Message
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// modelFacingTraceMiddleware 必须在 Handlers 链中处于 **BeforeModel 最后**（telemetry 之后），
// 此时 state.Messages 即为本次 LLM 调用的最终入参。
type modelFacingTraceMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	holder *modelFacingTraceHolder
}

func newModelFacingTraceMiddleware(holder *modelFacingTraceHolder) adk.ChatModelAgentMiddleware {
	if holder == nil {
		return nil
	}
	return &modelFacingTraceMiddleware{holder: holder}
}

func (m *modelFacingTraceMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	if m.holder != nil && state != nil {
		m.holder.storeFromState(state)
		captureEinoTurnHistory(ctx, state.Messages)
	}
	return ctx, state, nil
}

type agenticModelFacingTraceMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	holder *modelFacingTraceHolder
}

func newAgenticModelFacingTraceMiddleware(holder *modelFacingTraceHolder) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	if holder == nil {
		return nil
	}
	return &agenticModelFacingTraceMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		holder:                            holder,
	}
}

func (m *agenticModelFacingTraceMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.AgenticMessage],
	mc *adk.TypedModelContext[*schema.AgenticMessage],
) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	if m.holder != nil && state != nil {
		m.holder.storeFromAgenticState(state)
		captureEinoTurnHistory(ctx, AgenticMessagesToEino(state.Messages))
	}
	return ctx, state, nil
}

// Capture completed output separately from the model-input trace: changing
// Snapshot's meaning would affect last_react_input persistence and retries.
func (m *modelFacingTraceMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state != nil {
		captureEinoTurnHistory(ctx, state.Messages)
	}
	return ctx, state, nil
}

func (m *agenticModelFacingTraceMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], _ *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	if state != nil {
		captureEinoTurnHistory(ctx, AgenticMessagesToEino(state.Messages))
	}
	return ctx, state, nil
}

func (m *modelFacingTraceMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	if runCtx != nil {
		ctx = context.WithValue(ctx, einoTurnInstructionKey{}, runCtx.Instruction)
	}
	return ctx, runCtx, nil
}

func (m *agenticModelFacingTraceMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	if runCtx != nil {
		ctx = context.WithValue(ctx, einoTurnInstructionKey{}, runCtx.Instruction)
	}
	return ctx, runCtx, nil
}
