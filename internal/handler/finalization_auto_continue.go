package handler

import (
	"context"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/agentfinalizer"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"

	"go.uber.org/zap"
)

const finalizationAutoContinueMaxAttempts = 2
const finalizationPendingToolCancelWait = 2 * time.Second
const finalizationPendingToolCancelPoll = 50 * time.Millisecond
const finalizationPendingToolCancelNote = "Agent iteration ended; automatically terminating unfinished tool executions before the final reply"

func shouldAutoContinueAfterFinalization(d agentfinalizer.Decision, attempt int) bool {
	if d.Finalizable || d.Finalized {
		return false
	}
	if attempt >= finalizationAutoContinueMaxAttempts {
		return false
	}
	return d.CompletionReason == agentfinalizer.ReasonMissingEvidence
}

func (h *AgentHandler) tryAutoContinueAfterFinalization(
	taskCtx context.Context,
	conversationID string,
	result *multiagent.RunResult,
	decision agentfinalizer.Decision,
	attempt *int,
	curHistory *[]agent.ChatMessage,
	curFinalMessage *string,
	progressCallback func(eventType, message string, data interface{}),
) bool {
	if !shouldAutoContinueAfterFinalization(decision, *attempt) || result == nil || !multiagent.HasEinoResumeTrace(result) {
		return false
	}
	*attempt++
	h.persistEinoAgentTraceForResume(conversationID, result)
	if hist, err := h.loadHistoryFromAgentTrace(conversationID); err == nil && len(hist) > 0 {
		*curHistory = hist
	} else if h.logger != nil {
		h.logger.Warn("finalization auto-continue could not restore trace",
			zap.String("conversationId", conversationID),
			zap.Error(err))
		return false
	}
	// Agent 无感续跑：不追加新的 user/system 文案，只使用上一段模型可见轨迹继续 Runner。
	*curFinalMessage = ""
	if progressCallback != nil {
		progressCallback("finalization_auto_continue", "The final-reply check has not converged yet; continuing execution based on the existing trajectory…", map[string]interface{}{
			"conversationId":      conversationID,
			"source":              "finalizer",
			"attempt":             *attempt,
			"maxAttempts":         finalizationAutoContinueMaxAttempts,
			"status":              decision.Status,
			"completionReason":    decision.CompletionReason,
			"missingChecks":       decision.MissingChecks,
			"pendingExecutionIds": decision.PendingExecutionIDs,
			"contextInjection":    false,
		})
	}
	select {
	case <-taskCtx.Done():
		return false
	case <-time.After(finalizationAutoContinueBackoff(*attempt)):
		return true
	}
}

func finalizationAutoContinueBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 500 * time.Millisecond
	}
	return time.Duration(attempt) * time.Second
}

func (h *AgentHandler) cleanupPendingToolExecutionsAfterIteration(
	taskCtx context.Context,
	conversationID string,
	decision agentfinalizer.Decision,
	progressCallback func(eventType, message string, data interface{}),
) []string {
	if h == nil || h.agent == nil || decision.CompletionReason != agentfinalizer.ReasonPendingTools {
		return nil
	}
	pending := uniqueNonEmptyStrings(decision.PendingExecutionIDs)
	if len(pending) == 0 {
		return nil
	}
	cancelled := make([]string, 0, len(pending))
	for _, executionID := range pending {
		if h.agent.CancelMCPToolExecutionWithNote(executionID, finalizationPendingToolCancelNote) {
			cancelled = append(cancelled, executionID)
		} else if h.logger != nil {
			h.logger.Warn("finalization pending tool cleanup could not cancel execution",
				zap.String("conversationId", conversationID),
				zap.String("executionId", executionID))
		}
	}
	if len(cancelled) == 0 {
		return nil
	}
	if progressCallback != nil {
		progressCallback("finalization_pending_tools_cancelled", "Iteration ended; still-running tool executions were terminated automatically.", map[string]interface{}{
			"conversationId":                   conversationID,
			"source":                           "finalizer",
			"autoCancelledPendingExecutionIds": cancelled,
			"pendingExecutionIds":              pending,
			"reason":                           agentfinalizer.ReasonPendingTools,
		})
	}
	h.waitForToolExecutionsToLeavePending(taskCtx, cancelled, finalizationPendingToolCancelWait)
	return cancelled
}

func (h *AgentHandler) waitForToolExecutionsToLeavePending(ctx context.Context, executionIDs []string, wait time.Duration) {
	if h == nil || h.db == nil || len(executionIDs) == 0 || wait <= 0 {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	ticker := time.NewTicker(finalizationPendingToolCancelPoll)
	defer ticker.Stop()
	for {
		if !h.hasPendingToolExecutions(executionIDs) {
			return
		}
		select {
		case <-contextDone(ctx):
			return
		case <-timer.C:
			return
		case <-ticker.C:
		}
	}
}

func (h *AgentHandler) hasPendingToolExecutions(executionIDs []string) bool {
	if h == nil || h.db == nil {
		return false
	}
	for _, executionID := range uniqueNonEmptyStrings(executionIDs) {
		exec, err := h.db.GetToolExecution(executionID)
		if err != nil || exec == nil {
			continue
		}
		switch strings.TrimSpace(exec.Status) {
		case mcp.ToolExecutionStatusQueued, mcp.ToolExecutionStatusRunning:
			return true
		}
	}
	return false
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func contextDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
