package multiagent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// appendEinoAgenticChatModelTailMiddlewares appends protocol-neutral handlers for
// TypedChatModelAgent[*schema.AgenticMessage]. History repair operates
// directly on native function-call/result blocks without converting protocols.
func appendEinoAgenticChatModelTailMiddlewares(
	handlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage],
	cfg einoChatModelTailConfig,
) []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	handlers = append(handlers, newAgenticSystemMessageNormalizerMiddleware(cfg.logger, cfg.phase))
	handlers = append(handlers, newAgenticContinuationUserDedupMiddleware(cfg.logger, cfg.phase))
	handlers = append(handlers, newAgenticToolCallArgumentsSanitizerMiddleware(cfg.logger, cfg.phase+"_pre_summarization"))
	if cfg.agenticSummarization != nil {
		handlers = append(handlers, newAgenticToolPairReconcilerMiddleware(cfg.logger, cfg.phase+"_pre_summarization"))
		handlers = append(handlers, cfg.agenticSummarization)
	}
	handlers = append(handlers, newAgenticToolCallArgumentsSanitizerMiddleware(cfg.logger, cfg.phase))
	handlers = append(handlers, newAgenticToolPairReconcilerMiddleware(cfg.logger, cfg.phase))
	if !cfg.skipOrphanPruner {
		handlers = append(handlers, newAgenticOrphanToolPrunerMiddleware(cfg.logger, cfg.phase))
	}
	if !cfg.skipTrace && cfg.trace != nil {
		if capMw := newAgenticModelFacingTraceMiddleware(cfg.trace); capMw != nil {
			handlers = append(handlers, capMw)
		}
	}
	return handlers
}

type agenticSystemMessageNormalizerMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	logger *zap.Logger
	phase  string
}

func newAgenticSystemMessageNormalizerMiddleware(logger *zap.Logger, phase string) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &agenticSystemMessageNormalizerMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		logger:                            logger,
		phase:                             phase,
	}
}

func (m *agenticSystemMessageNormalizerMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.AgenticMessage],
	mc *adk.TypedModelContext[*schema.AgenticMessage],
) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	_ = mc
	if m == nil || state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	before := countAgenticSystemMessages(state.Messages)
	if before <= 1 {
		return ctx, state, nil
	}
	normalized := normalizeSingleLeadingAgenticSystemMessage(state.Messages)
	if len(normalized) == len(state.Messages) && countAgenticSystemMessages(normalized) >= before {
		return ctx, state, nil
	}
	if m.logger != nil {
		m.logger.Info("eino agentic system messages merged",
			zap.String("phase", m.phase),
			zap.Int("system_before", before),
			zap.Int("system_after", countAgenticSystemMessages(normalized)),
			zap.Int("messages_before", len(state.Messages)),
			zap.Int("messages_after", len(normalized)),
		)
	}
	out := *state
	out.Messages = normalized
	return ctx, &out, nil
}

type agenticContinuationUserDedupMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	logger *zap.Logger
	phase  string
}

func newAgenticContinuationUserDedupMiddleware(logger *zap.Logger, phase string) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &agenticContinuationUserDedupMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		logger:                            logger,
		phase:                             phase,
	}
}

func (m *agenticContinuationUserDedupMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.AgenticMessage],
	mc *adk.TypedModelContext[*schema.AgenticMessage],
) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	_ = mc
	if m == nil || state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	deduped, dropped := dedupAgenticContinuationUserMessages(state.Messages)
	if dropped == 0 {
		return ctx, state, nil
	}
	if m.logger != nil {
		m.logger.Info("eino agentic continuation user messages deduplicated",
			zap.String("phase", m.phase),
			zap.Int("dropped", dropped),
			zap.Int("messages_before", len(state.Messages)),
			zap.Int("messages_after", len(deduped)),
		)
	}
	out := *state
	out.Messages = deduped
	return ctx, &out, nil
}

func countAgenticSystemMessages(msgs []*schema.AgenticMessage) int {
	n := 0
	for _, msg := range msgs {
		if msg != nil && msg.Role == schema.AgenticRoleTypeSystem {
			n++
		}
	}
	return n
}

func normalizeSingleLeadingAgenticSystemMessage(msgs []*schema.AgenticMessage) []*schema.AgenticMessage {
	var systemParts []string
	out := make([]*schema.AgenticMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role == schema.AgenticRoleTypeSystem {
			if text := strings.TrimSpace(agenticMessageText(msg)); text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		out = append(out, msg)
	}
	if len(systemParts) == 0 {
		return out
	}
	merged := schema.SystemAgenticMessage(strings.Join(systemParts, "\n\n"))
	return append([]*schema.AgenticMessage{merged}, out...)
}

func dedupAgenticContinuationUserMessages(msgs []*schema.AgenticMessage) ([]*schema.AgenticMessage, int) {
	lastIdx := -1
	contCount := 0
	for i, msg := range msgs {
		if !isAgenticContinuationUserMessage(msg) {
			continue
		}
		contCount++
		lastIdx = i
	}
	if contCount <= 1 {
		return msgs, 0
	}
	out := make([]*schema.AgenticMessage, 0, len(msgs)-(contCount-1))
	dropped := 0
	for i, msg := range msgs {
		if isAgenticContinuationUserMessage(msg) && i != lastIdx {
			dropped++
			continue
		}
		out = append(out, msg)
	}
	return out, dropped
}

func isAgenticContinuationUserMessage(msg *schema.AgenticMessage) bool {
	if msg == nil || msg.Role != schema.AgenticRoleTypeUser {
		return false
	}
	return strings.Contains(agenticMessageText(msg), continuationSessionMarker)
}

func agenticMessageText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var b strings.Builder
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		switch {
		case block.UserInputText != nil:
			if s := strings.TrimSpace(block.UserInputText.Text); s != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(s)
			}
		case block.AssistantGenText != nil:
			if s := strings.TrimSpace(block.AssistantGenText.Text); s != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(s)
			}
		}
	}
	return b.String()
}
