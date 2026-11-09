package multiagent

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type einoTransientRunRetryHandlerConfig struct {
	Context        context.Context
	ConversationID string
	OrchMode       string
	Args           *einoADKRunLoopArgs
	BaseMsgs       []adk.Message
	Progress       func(eventType, message string, data interface{})
	Logger         *zap.Logger
	Pending        *einoPendingToolCalls
	Policy         einoTransientRunRetryPolicy
}

type einoTransientRunRetryResult struct {
	Handled     bool
	Restarted   bool
	RestartMsgs []adk.Message
	ContextSrc  einoRunRestartContextSource
	Fatal       error
}

type einoTransientRunRetryHandler struct {
	cfg     einoTransientRunRetryHandlerConfig
	retrier *einoTransientRunRetrier
}

func newEinoTransientRunRetryHandler(cfg einoTransientRunRetryHandlerConfig) *einoTransientRunRetryHandler {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	if cfg.Args == nil {
		cfg.Args = &einoADKRunLoopArgs{}
	}
	if cfg.Policy.maxAttempts <= 0 {
		cfg.Policy = einoTransientRunRetryPolicyFromArgs(cfg.Args)
	}
	return &einoTransientRunRetryHandler{
		cfg:     cfg,
		retrier: newEinoTransientRunRetrier(cfg.Policy),
	}
}

func (h *einoTransientRunRetryHandler) Prepare(
	runErr error,
	accumulated []adk.Message,
	baseCount int,
) einoTransientRunRetryResult {
	if h == nil || !isEinoTransientRunError(runErr) {
		return einoTransientRunRetryResult{}
	}
	restarted, restartMsgs, ctxSource, backoff, retErr := h.retrier.tryRetry(
		h.cfg.Context, runErr, h.cfg.Args, h.cfg.BaseMsgs, accumulated, baseCount,
	)
	if retErr != nil {
		if h.cfg.Pending != nil {
			h.cfg.Pending.FlushAsFailed(runErr)
		}
		if h.cfg.Logger != nil {
			h.cfg.Logger.Warn("eino transient retry exhausted",
				zap.Error(retErr),
				zap.String("orchestration", h.cfg.OrchMode),
				zap.Int("maxAttempts", h.retrier.maxAttempts()))
		}
		return einoTransientRunRetryResult{Handled: true, Fatal: retErr}
	}
	if !restarted {
		return einoTransientRunRetryResult{Handled: true}
	}
	attemptNo := h.retrier.attempt()
	maxAttempts := h.retrier.maxAttempts()
	if h.cfg.Logger != nil {
		h.cfg.Logger.Warn("eino transient error, retrying after backoff",
			zap.Error(runErr),
			zap.String("orchestration", h.cfg.OrchMode),
			zap.Int("attempt", attemptNo),
			zap.Int("maxAttempts", maxAttempts),
			zap.Duration("backoff", backoff))
	}
	emitEinoRunRetryProgress(
		h.cfg.Progress,
		h.cfg.ConversationID,
		h.cfg.OrchMode,
		runErr,
		attemptNo,
		maxAttempts,
		backoff,
		ctxSource,
	)
	return einoTransientRunRetryResult{
		Handled:     true,
		Restarted:   true,
		RestartMsgs: restartMsgs,
		ContextSrc:  ctxSource,
	}
}

func (h *einoTransientRunRetryHandler) ConfirmRecovery() {
	if h != nil && h.retrier != nil && h.retrier.attempt() > 0 {
		h.retrier.reset()
	}
}

func emitEinoRunRetryProgress(
	progress func(eventType, message string, data interface{}),
	conversationID, orchMode string,
	runErr error,
	attemptNo, maxAttempts int,
	backoff time.Duration,
	ctxSource einoRunRestartContextSource,
) int {
	if progress == nil || runErr == nil {
		return 0
	}
	errorKind, errorSummary := einoTransientRunErrorUserDetail(runErr)
	data := map[string]interface{}{
		"conversationId": conversationID,
		"source":         "eino",
		"orchestration":  orchMode,
		"error":          runErr.Error(),
		"errorKind":      errorKind,
		"errorSummary":   errorSummary,
		"attempt":        attemptNo,
		"maxAttempts":    maxAttempts,
		"backoffSec":     int(backoff.Seconds()),
	}
	progress("eino_run_retry", fmt.Sprintf("Transient error; retrying in %d seconds (attempt %d/%d). Reason: %s", int(backoff.Seconds()), attemptNo, maxAttempts, errorSummary), data)
	restartedData := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		restartedData[k] = v
	}
	restartedData["contextSource"] = string(ctxSource)
	progress("eino_run_retry", "Context restored, retrying…", restartedData)
	return 2
}
