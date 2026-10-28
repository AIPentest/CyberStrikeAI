package handler

import (
	"context"
	"fmt"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/multiagent"

	"go.uber.org/zap"
)

// rebindEinoRunningTask 中断并继续 / 空正文续跑：重建 cancel 链与超时 ctx，保持任务 running。
func (h *AgentHandler) rebindEinoRunningTask(parent context.Context, conversationID string, timeoutCancel context.CancelFunc) (context.Context, context.CancelCauseFunc, context.Context, context.CancelFunc) {
	if timeoutCancel != nil {
		timeoutCancel()
	}
	baseCtx, cancelWithCause := context.WithCancelCause(detachedAgentContext(parent))
	h.tasks.BindTaskCancel(conversationID, cancelWithCause)
	taskCtx, newTimeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)
	h.tasks.UpdateTaskStatus(conversationID, "running")
	return baseCtx, cancelWithCause, taskCtx, newTimeoutCancel
}

// tryContinueOnEinoEmptyResponse Run 成功但 Response 为 emptyHint 时退避续跑；true 表示已准备下一段 Run。
func (h *AgentHandler) tryContinueOnEinoEmptyResponse(
	taskCtx context.Context,
	mw *config.MultiAgentEinoMiddlewareConfig,
	conversationID string,
	result *multiagent.RunResult,
	attempt *int,
	curHistory *[]agent.ChatMessage,
	curFinalMessage *string,
	progressCallback func(eventType, message string, data interface{}),
) bool {
	if result == nil || !multiagent.IsEinoEmptyResponseResult(result) || !multiagent.HasEinoResumeTrace(result) {
		return false
	}
	maxAttempts := multiagent.EmptyResponseContinueMaxAttemptsFromConfig(mw)
	if *attempt >= maxAttempts {
		if h.logger != nil {
			h.logger.Warn("eino empty response continue exhausted",
				zap.String("conversationId", conversationID),
				zap.Int("maxAttempts", maxAttempts))
		}
		return false
	}
	*attempt++
	h.persistEinoAgentTraceForResume(conversationID, result)

	backoff := multiagent.EmptyResponseContinueBackoff(*attempt-1, mw)
	waitMsg := fmt.Sprintf("Session ended but no assistant text was captured; auto-continue attempt %d/%d in %d seconds…",
		*attempt, maxAttempts, int(backoff.Seconds()))
	if progressCallback != nil {
		progressCallback("eino_empty_response_continue", waitMsg, map[string]interface{}{
			"conversationId": conversationID,
			"source":         "eino",
			"attempt":        *attempt,
			"maxAttempts":    maxAttempts,
			"backoffSec":     int(backoff.Seconds()),
		})
	}
	select {
	case <-taskCtx.Done():
		return false
	case <-time.After(backoff):
	}

	h.applyEinoTraceResumeSegment(conversationID, result, curHistory, curFinalMessage, "")
	if progressCallback != nil {
		progressCallback("eino_empty_response_continue", "Context restored; continuing…", map[string]interface{}{
			"conversationId":   conversationID,
			"source":           "eino",
			"attempt":          *attempt,
			"maxAttempts":      maxAttempts,
			"contextSource":    "empty_response_continue",
			"contextInjection": false,
		})
	}
	return true
}
