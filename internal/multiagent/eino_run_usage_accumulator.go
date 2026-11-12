package multiagent

import (
	"sync"

	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type einoRunUsageSummary struct {
	ModelCalls       int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	CacheWriteTokens int
	ReasoningTokens  int
}

type einoRunUsageAccumulator struct {
	mu      sync.Mutex
	summary einoRunUsageSummary
	emitted bool
}

func newEinoRunUsageAccumulator() *einoRunUsageAccumulator {
	return &einoRunUsageAccumulator{}
}

func (a *einoRunUsageAccumulator) AddMessage(msg *schema.Message) bool {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return false
	}
	return a.AddUsage(msg.ResponseMeta.Usage)
}

func (a *einoRunUsageAccumulator) AddUsage(usage *schema.TokenUsage) bool {
	if a == nil || usage == nil || tokenUsageEmpty(usage) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.summary.ModelCalls++
	a.summary.PromptTokens += usage.PromptTokens
	a.summary.CompletionTokens += usage.CompletionTokens
	total := usage.TotalTokens
	if total == 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	a.summary.TotalTokens += total
	a.summary.CachedTokens += usage.PromptTokenDetails.CachedTokens
	a.summary.CacheWriteTokens += usage.PromptTokenDetails.CacheWriteTokens
	a.summary.ReasoningTokens += usage.CompletionTokensDetails.ReasoningTokens
	return true
}

func (a *einoRunUsageAccumulator) Summary() einoRunUsageSummary {
	if a == nil {
		return einoRunUsageSummary{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.summary
}

func (a *einoRunUsageAccumulator) EmitOnce(
	conversationID string,
	orchestration string,
	reason string,
	modelName string,
	progress func(eventType, message string, data interface{}),
	logger *zap.Logger,
) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	if a.emitted || a.summary.ModelCalls == 0 {
		a.mu.Unlock()
		return false
	}
	a.emitted = true
	s := a.summary
	a.mu.Unlock()

	data := map[string]interface{}{
		"conversationId":   conversationID,
		"source":           "eino",
		"orchestration":    orchestration,
		"reason":           reason,
		"model":            modelName,
		"modelCalls":       s.ModelCalls,
		"promptTokens":     s.PromptTokens,
		"completionTokens": s.CompletionTokens,
		"totalTokens":      s.TotalTokens,
		"cachedTokens":     s.CachedTokens,
		"cacheWriteTokens": s.CacheWriteTokens,
		"reasoningTokens":  s.ReasoningTokens,
	}
	if progress != nil {
		progress("eino_usage_summary", "Eino token usage summary", data)
	}
	if logger != nil {
		logger.Info("eino token usage summary",
			zap.String("conversationId", conversationID),
			zap.String("orchestration", orchestration),
			zap.String("reason", reason),
			zap.String("model", modelName),
			zap.Int("modelCalls", s.ModelCalls),
			zap.Int("promptTokens", s.PromptTokens),
			zap.Int("completionTokens", s.CompletionTokens),
			zap.Int("totalTokens", s.TotalTokens),
			zap.Int("cachedTokens", s.CachedTokens),
			zap.Int("cacheWriteTokens", s.CacheWriteTokens),
			zap.Int("reasoningTokens", s.ReasoningTokens),
		)
	}
	return true
}

func maxEinoTokenUsage(dst *schema.TokenUsage, src *schema.TokenUsage) *schema.TokenUsage {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneEinoTokenUsage(src)
	}
	if src.PromptTokens > dst.PromptTokens {
		dst.PromptTokens = src.PromptTokens
	}
	if src.CompletionTokens > dst.CompletionTokens {
		dst.CompletionTokens = src.CompletionTokens
	}
	if src.TotalTokens > dst.TotalTokens {
		dst.TotalTokens = src.TotalTokens
	}
	if src.PromptTokenDetails.CachedTokens > dst.PromptTokenDetails.CachedTokens {
		dst.PromptTokenDetails.CachedTokens = src.PromptTokenDetails.CachedTokens
	}
	if src.PromptTokenDetails.CacheWriteTokens > dst.PromptTokenDetails.CacheWriteTokens {
		dst.PromptTokenDetails.CacheWriteTokens = src.PromptTokenDetails.CacheWriteTokens
	}
	if src.CompletionTokensDetails.ReasoningTokens > dst.CompletionTokensDetails.ReasoningTokens {
		dst.CompletionTokensDetails.ReasoningTokens = src.CompletionTokensDetails.ReasoningTokens
	}
	return dst
}

func cloneEinoTokenUsage(src *schema.TokenUsage) *schema.TokenUsage {
	if src == nil {
		return nil
	}
	out := *src
	return &out
}

func tokenUsageEmpty(u *schema.TokenUsage) bool {
	return u == nil ||
		(u.PromptTokens == 0 &&
			u.CompletionTokens == 0 &&
			u.TotalTokens == 0 &&
			u.PromptTokenDetails.CachedTokens == 0 &&
			u.PromptTokenDetails.CacheWriteTokens == 0 &&
			u.CompletionTokensDetails.ReasoningTokens == 0)
}
