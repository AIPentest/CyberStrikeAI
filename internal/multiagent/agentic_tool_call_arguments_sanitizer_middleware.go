package multiagent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Apply the classic history repair to native function-call blocks too. Providers
// may parse arguments embedded in otherwise valid request JSON and reject the
// next turn if a previous model response contained truncated arguments. Repair
// only historical input; never repair or execute an incomplete new tool call.
type agenticToolCallArgumentsSanitizerMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	logger *zap.Logger
	phase  string
}

func newAgenticToolCallArgumentsSanitizerMiddleware(logger *zap.Logger, phase string) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &agenticToolCallArgumentsSanitizerMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		logger:                            logger, phase: phase,
	}
}

func (m *agenticToolCallArgumentsSanitizerMiddleware) BeforeModelRewriteState(
	ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage],
	_ *adk.TypedModelContext[*schema.AgenticMessage],
) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	if m == nil || state == nil {
		return ctx, state, nil
	}
	var out []*schema.AgenticMessage
	repaired := 0
	for i, msg := range state.Messages {
		if msg == nil || msg.Role != schema.AgenticRoleTypeAssistant {
			continue
		}
		var blocks []*schema.ContentBlock
		for j, block := range msg.ContentBlocks {
			if block == nil || block.FunctionToolCall == nil || validToolArgumentsJSONObject(block.FunctionToolCall.Arguments) {
				continue
			}
			if blocks == nil {
				blocks = append([]*schema.ContentBlock(nil), msg.ContentBlocks...)
			}
			clonedBlock := *block
			clonedCall := *block.FunctionToolCall
			clonedCall.Arguments = repairedMalformedToolArguments
			clonedBlock.FunctionToolCall = &clonedCall
			blocks[j] = &clonedBlock
			repaired++
		}
		if blocks == nil {
			continue
		}
		if out == nil {
			out = append([]*schema.AgenticMessage(nil), state.Messages...)
		}
		clonedMessage := *msg
		clonedMessage.ContentBlocks = blocks
		out[i] = &clonedMessage
	}
	if repaired == 0 {
		return ctx, state, nil
	}
	if m.logger != nil {
		m.logger.Warn("eino agentic malformed tool-call arguments repaired before model call", zap.String("phase", m.phase), zap.Int("repaired_calls", repaired))
	}
	clonedState := *state
	clonedState.Messages = out
	return ctx, &clonedState, nil
}
