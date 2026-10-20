package multiagent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"go.uber.org/zap"
)

// modelInputSoftBudgetMiddleware is the final guard before a normal model call.
// It drops oldest complete rounds and truncates oversized tool output in the latest
// round, but never fails locally — API context limits are handled by overflow retry.
type modelInputSoftBudgetMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	maxTokens    int
	toolMaxBytes int
	counter      summarization.TokenCounterFunc
	logger       *zap.Logger
	phase        string
}

func newModelInputSoftBudgetMiddleware(
	maxTotalTokens int,
	toolMaxBytes int,
	modelName string,
	tokenSafetyPercent int,
	logger *zap.Logger,
	phase string,
) adk.ChatModelAgentMiddleware {
	if maxTotalTokens <= 0 {
		maxTotalTokens = 120000
	}
	if toolMaxBytes <= 0 {
		toolMaxBytes = 12000
	}
	return &modelInputSoftBudgetMiddleware{
		maxTokens:    maxTotalTokens,
		toolMaxBytes: toolMaxBytes,
		counter:      einoSummarizationTokenCounterWithSafety(modelName, tokenSafetyPercent),
		logger:       logger,
		phase:        phase,
	}
}

func (m *modelInputSoftBudgetMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	if m == nil || state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	compacted, changed := compactMessagesByDroppingRounds(ctx, state.Messages, compactMessagesOpts{
		maxTokens:    m.maxTokens,
		counter:      m.counter,
		tools:        state.ToolInfos,
		toolMaxBytes: m.toolMaxBytes,
		phase:        m.phase,
		logger:       m.logger,
	})
	if !changed {
		return ctx, state, nil
	}
	out := *state
	out.Messages = compacted
	return ctx, &out, nil
}
