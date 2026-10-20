package multiagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

var contextBudgetWarned sync.Map

// logContextBudgetWarnings reports the clamp applied by WithEffectiveContextBudget.
// configured must be the pre-clamp channel config so the warning set and the
// configured value stay meaningful.
func logContextBudgetWarnings(configured, effective config.OpenAIConfig, logger *zap.Logger, phase string) {
	if logger == nil {
		return
	}
	warnings := configured.ContextBudgetWarnings()
	if len(warnings) == 0 {
		return
	}
	key := fmt.Sprintf("%d|%d|%d|%s",
		configured.EffectiveContextWindow(),
		configured.MaxTotalTokens,
		effective.MaxTotalTokens,
		strings.Join(warnings, ","),
	)
	if _, loaded := contextBudgetWarned.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	logger.Warn("model context budget adjusted",
		zap.String("phase", phase),
		zap.Int("context_window", configured.EffectiveContextWindow()),
		zap.Int("configured_max_total_tokens", configured.MaxTotalTokens),
		zap.Int("effective_max_total_tokens", effective.MaxTotalTokens),
		zap.Strings("warnings", warnings),
	)
}

const (
	toolOutputTruncationMarker = "\n\n...[tool output truncated; full text persisted in reduction cache or summarization transcript]...\n\n"
	aggressiveToolTruncDivisor = 4
)

// isEinoContextOverflowError reports API-side context window rejections.
func isEinoContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	markers := []string{
		"context length",
		"context_length",
		"maximum context",
		"max context",
		"context window",
		"context overflow",
		"too many tokens",
		"token limit",
		"tokens exceed",
		"exceeds the context",
		"exceeds the available context", // llama.cpp: request exceeds the available context size
		"available context size",
		"input is too long",
		"input length exceeds",
		"prompt is too long",
		"prompt too long",
		"max_model_len", // vLLM: input tokens exceed max_model_len
		"maximum model length",
		"request too large",
		"tokens to keep", // LM Studio truncation warning
	}
	for _, m := range markers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

func truncateBytesWithMarker(content string, maxBytes int, marker string) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	if marker == "" {
		marker = toolOutputTruncationMarker
	}
	budget := maxBytes - len(marker)
	if budget <= 0 {
		if len(marker) > maxBytes {
			return marker[:maxBytes]
		}
		return marker
	}
	head := budget / 2
	tail := budget - head
	for head > 0 && !utf8.RuneStart(content[head]) {
		head--
	}
	tailStart := len(content) - tail
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	return content[:head] + marker + content[tailStart:]
}

func cloneMessage(msg adk.Message) adk.Message {
	if msg == nil {
		return nil
	}
	cloned := *msg
	return &cloned
}

func truncateMessageToolContent(msg adk.Message, maxBytes int, spillRef string) adk.Message {
	if msg == nil || maxBytes <= 0 {
		return msg
	}
	out := cloneMessage(msg)
	marker := toolOutputTruncationMarker
	if spillRef != "" {
		marker = fmt.Sprintf("\n\n...[tool output truncated; retrieve full text via: %s]...\n\n", spillRef)
	}
	switch out.Role {
	case schema.Tool:
		out.Content = truncateBytesWithMarker(out.Content, maxBytes, marker)
	case schema.Assistant:
		if out.ReasoningContent != "" {
			out.ReasoningContent = truncateBytesWithMarker(out.ReasoningContent, maxBytes, marker)
		}
		if out.Content != "" {
			out.Content = truncateBytesWithMarker(out.Content, maxBytes, marker)
		}
	case schema.User:
		if out.Content != "" {
			out.Content = truncateBytesWithMarker(out.Content, maxBytes, marker)
		}
	}
	return out
}

func countMessagesTokens(
	ctx context.Context,
	msgs []adk.Message,
	counter summarization.TokenCounterFunc,
	tools []*schema.ToolInfo,
) (int, error) {
	if counter == nil {
		return 0, nil
	}
	n, err := counter(ctx, &summarization.TokenCounterInput{Messages: msgs, Tools: tools})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func truncateRoundMessagesToTokenBudget(
	ctx context.Context,
	round messageRound,
	tokenBudget int,
	counter summarization.TokenCounterFunc,
	toolMaxBytes int,
	spillRef string,
) ([]adk.Message, error) {
	if tokenBudget <= 0 || len(round.messages) == 0 {
		return nil, nil
	}
	msgs := append([]adk.Message(nil), round.messages...)
	if n, err := countMessagesTokens(ctx, msgs, counter, nil); err != nil {
		return nil, err
	} else if n <= tokenBudget {
		return msgs, nil
	}
	if toolMaxBytes <= 0 {
		toolMaxBytes = 12000
	}
	for pass := 0; pass < 8 && toolMaxBytes >= 32; pass++ {
		out := make([]adk.Message, 0, len(msgs))
		for _, msg := range msgs {
			switch {
			case msg != nil && msg.Role == schema.Tool:
				out = append(out, truncateMessageToolContent(msg, toolMaxBytes, spillRef))
			case msg != nil && msg.Role == schema.Assistant:
				out = append(out, truncateMessageToolContent(msg, toolMaxBytes, spillRef))
			default:
				out = append(out, msg)
			}
		}
		n, err := countMessagesTokens(ctx, out, counter, nil)
		if err != nil {
			return nil, err
		}
		if n <= tokenBudget {
			return out, nil
		}
		msgs = out
		toolMaxBytes /= 2
	}
	return msgs, nil
}

type compactMessagesOpts struct {
	maxTokens    int
	counter      summarization.TokenCounterFunc
	tools        []*schema.ToolInfo
	toolMaxBytes int
	spillRef     string
	aggressive   bool
	logger       *zap.Logger
	phase        string
}

func countToolTokens(ctx context.Context, counter summarization.TokenCounterFunc, tools []*schema.ToolInfo) int {
	if counter == nil || len(tools) == 0 {
		return 0
	}
	n, err := countMessagesTokens(ctx, nil, counter, tools)
	if err != nil {
		return 0
	}
	return n
}

func countMessagesTokensNoTools(ctx context.Context, msgs []adk.Message, counter summarization.TokenCounterFunc) int {
	if counter == nil {
		return 0
	}
	n, err := countMessagesTokens(ctx, msgs, counter, nil)
	if err != nil {
		return 0
	}
	return n
}

func compactMessagesByDroppingRounds(
	ctx context.Context,
	messages []adk.Message,
	opts compactMessagesOpts,
) ([]adk.Message, bool) {
	if opts.maxTokens <= 0 || len(messages) == 0 || opts.counter == nil {
		return messages, false
	}
	before, err := countMessagesTokens(ctx, messages, opts.counter, opts.tools)
	if err != nil || before <= opts.maxTokens {
		return messages, false
	}
	toolTokens := countToolTokens(ctx, opts.counter, opts.tools)
	if toolTokens >= opts.maxTokens {
		if opts.logger != nil {
			opts.logger.Warn("model input budget exhausted by tool schemas",
				zap.String("phase", opts.phase),
				zap.Int("tool_tokens_estimated", toolTokens),
				zap.Int("message_tokens_estimated", countMessagesTokensNoTools(ctx, messages, opts.counter)),
				zap.Int("max_tokens", opts.maxTokens),
				zap.Int("tools", len(opts.tools)),
			)
		}
		return messages, false
	}

	systems := make([]adk.Message, 0, 1)
	contextMsgs := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil && msg.Role == schema.System && len(contextMsgs) == 0 {
			systems = append(systems, msg)
			continue
		}
		if msg != nil {
			contextMsgs = append(contextMsgs, msg)
		}
	}
	rounds := splitMessagesIntoRounds(contextMsgs)
	if len(rounds) == 0 {
		return messages, false
	}

	startIdx := 0
	if opts.aggressive {
		startIdx = len(rounds) - 1
		if startIdx < 0 {
			startIdx = 0
		}
	}
	dropped := 0
	for len(rounds) > 1 || (opts.aggressive && len(rounds) == 1) {
		if !opts.aggressive && len(rounds) <= 1 {
			break
		}
		if opts.aggressive && len(rounds) == 1 {
			// Fall through to latest-round truncation below.
			break
		}
		rounds = rounds[1:]
		dropped++
		candidate := append([]adk.Message(nil), systems...)
		for _, round := range rounds {
			candidate = append(candidate, round.messages...)
		}
		after, countErr := countMessagesTokens(ctx, candidate, opts.counter, opts.tools)
		if countErr != nil {
			break
		}
		if after <= opts.maxTokens {
			if opts.logger != nil {
				opts.logger.Warn("eino context compacted by dropping older rounds",
					zap.String("phase", opts.phase),
					zap.Int("tokens_before", before),
					zap.Int("tokens_after", after),
					zap.Int("max_tokens", opts.maxTokens),
					zap.Int("tool_tokens_estimated", toolTokens),
					zap.Int("dropped_rounds", dropped),
					zap.Bool("aggressive", opts.aggressive),
				)
			}
			return candidate, true
		}
		if opts.aggressive {
			break
		}
	}

	if len(rounds) == 0 {
		return messages, false
	}
	latest := rounds[len(rounds)-1]
	messageBudget := opts.maxTokens - toolTokens
	if messageBudget < 1024 {
		messageBudget = 1024
	}
	truncated, truncErr := truncateRoundMessagesToTokenBudget(
		ctx, latest, messageBudget, opts.counter, opts.toolMaxBytes, opts.spillRef,
	)
	if truncErr != nil || len(truncated) == 0 {
		if opts.logger != nil {
			opts.logger.Warn("eino context still above budget after round compaction; passing through without local error",
				zap.String("phase", opts.phase),
				zap.Int("tokens_before", before),
				zap.Int("max_tokens", opts.maxTokens),
				zap.Int("tool_tokens_estimated", toolTokens),
				zap.Bool("aggressive", opts.aggressive),
			)
		}
		return messages, false
	}
	candidate := append([]adk.Message(nil), systems...)
	if dropped > 0 || startIdx > 0 {
		for _, round := range rounds[:len(rounds)-1] {
			candidate = append(candidate, round.messages...)
		}
	}
	candidate = append(candidate, truncated...)
	after, countErr := countMessagesTokens(ctx, candidate, opts.counter, opts.tools)
	if countErr != nil {
		return messages, false
	}
	if opts.logger != nil {
		opts.logger.Warn("eino context compacted by truncating latest round tool output",
			zap.String("phase", opts.phase),
			zap.Int("tokens_before", before),
			zap.Int("tokens_after", after),
			zap.Int("max_tokens", opts.maxTokens),
			zap.Int("tool_tokens_estimated", toolTokens),
			zap.Int("dropped_rounds", dropped),
			zap.Bool("aggressive", opts.aggressive),
		)
	}
	return candidate, true
}

func aggressiveCompactMessagesForOverflow(
	ctx context.Context,
	messages []adk.Message,
	maxTotalTokens int,
	modelName string,
	tokenSafetyPercent int,
	toolMaxBytes int,
	phase string,
	logger *zap.Logger,
) []adk.Message {
	if len(messages) == 0 || maxTotalTokens <= 0 {
		return messages
	}
	budget := maxTotalTokens * 70 / 100
	if budget < 4096 {
		budget = 4096
	}
	aggressiveToolMax := toolMaxBytes / aggressiveToolTruncDivisor
	if aggressiveToolMax < 2048 {
		aggressiveToolMax = 2048
	}
	out, _ := compactMessagesByDroppingRounds(ctx, messages, compactMessagesOpts{
		maxTokens:    budget,
		counter:      einoSummarizationTokenCounterWithSafety(modelName, tokenSafetyPercent),
		toolMaxBytes: aggressiveToolMax,
		aggressive:   true,
		logger:       logger,
		phase:        phase,
	})
	return out
}
