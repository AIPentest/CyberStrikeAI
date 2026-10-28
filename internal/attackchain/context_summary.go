package attackchain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"cyberstrike-ai/internal/agent"

	"go.uber.org/zap"
)

// ContextSummary is read-only context from the conversation's latest persisted
// model trace. It is neither a graph node nor evidence of tool execution. A
// cached graph may predate this context; no snapshot files are opened here.
type ContextSummary struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Provenance string `json:"provenance"`
}

// contextSummaries always rereads the conversation's latest trace: a cached
// graph may predate it, so context must not be frozen for the Builder's lifetime.
func (b *Builder) contextSummaries(conversationID string) []ContextSummary {
	trace, _, err := b.db.GetAgentTrace(conversationID)
	if err != nil {
		b.logger.Warn("Failed to load the attack chain context summary", zap.String("conversationId", conversationID), zap.Error(err))
		return nil
	}
	return b.contextSummariesFromTrace(trace)
}

func (b *Builder) contextSummariesFromTrace(trace string) []ContextSummary {
	msgs, err := agent.ParseTraceMessages(trace)
	if err != nil {
		b.logger.Warn("Failed to parse the attack chain context summary", zap.Error(err))
		return nil
	}
	var summaries []ContextSummary
	seen := make(map[string]struct{})
	for _, msg := range msgs {
		// Require the host's allowlisted provenance. Human text, skills, and
		// execution templates are never classified by matching their bodies.
		if !strings.EqualFold(msg.Role, "user") || msg.SummarizationKind != "summary" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if _, ok := seen[msg.Content]; ok {
			continue
		}
		seen[msg.Content] = struct{}{}
		hash := sha256.Sum256([]byte(msg.Content))
		summaries = append(summaries, ContextSummary{
			ID:         "summary-" + hex.EncodeToString(hash[:])[:16],
			Content:    msg.Content,
			Provenance: "system_generated_summary",
		})
	}
	return summaries
}
