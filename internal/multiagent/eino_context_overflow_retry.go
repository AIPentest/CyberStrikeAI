package multiagent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type einoContextOverflowRetryConfig struct {
	Context        context.Context
	ConversationID string
	OrchMode       string
	Args           *einoADKRunLoopArgs
	BaseMsgs       []adk.Message
	Progress       func(eventType, message string, data interface{})
	Logger         *zap.Logger
}

type einoContextOverflowRetryResult struct {
	Handled     bool
	RestartMsgs []adk.Message
	ContextSrc  einoRunRestartContextSource
}

type einoContextOverflowRetryHandler struct {
	cfg     einoContextOverflowRetryConfig
	retried bool
}

func newEinoContextOverflowRetryHandler(cfg einoContextOverflowRetryConfig) *einoContextOverflowRetryHandler {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	if cfg.Args == nil {
		cfg.Args = &einoADKRunLoopArgs{}
	}
	return &einoContextOverflowRetryHandler{cfg: cfg}
}

func (h *einoContextOverflowRetryHandler) Prepare(
	runErr error,
	accumulated []adk.Message,
	baseCount int,
) einoContextOverflowRetryResult {
	if h == nil || !isEinoContextOverflowError(runErr) || h.retried {
		return einoContextOverflowRetryResult{}
	}
	h.retried = true
	restartMsgs, ctxSource := einoMessagesForRunRestart(h.cfg.Args, h.cfg.BaseMsgs, accumulated, baseCount)
	restartMsgs = aggressiveCompactMessagesForOverflow(
		h.cfg.Context,
		restartMsgs,
		h.cfg.Args.MaxTotalTokens,
		h.cfg.Args.ModelName,
		h.cfg.Args.ToolMaxBytes,
		h.cfg.OrchMode,
		h.cfg.Logger,
	)
	if h.cfg.Logger != nil {
		h.cfg.Logger.Warn("eino context overflow, retrying with aggressive compaction",
			zap.Error(runErr),
			zap.String("orchestration", h.cfg.OrchMode),
			zap.String("contextSource", string(ctxSource)),
		)
	}
	emitEinoContextOverflowRetryProgress(h.cfg.Progress, h.cfg.ConversationID, h.cfg.OrchMode, ctxSource)
	return einoContextOverflowRetryResult{
		Handled:     true,
		RestartMsgs: restartMsgs,
		ContextSrc:  ctxSource,
	}
}

func emitEinoContextOverflowRetryProgress(
	progress func(eventType, message string, data interface{}),
	conversationID, orchMode string,
	ctxSource einoRunRestartContextSource,
) bool {
	if progress == nil {
		return false
	}
	progress("eino_context_overflow_retry", "Context overflow detected; retrying after aggressive compaction…", map[string]interface{}{
		"conversationId": conversationID,
		"source":         "eino",
		"orchestration":  orchMode,
		"contextSource":  string(ctxSource),
	})
	return true
}
