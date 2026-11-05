package handler

import (
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/agentfinalizer"
	"cyberstrike-ai/internal/multiagent"

	"go.uber.org/zap"
)

func (h *AgentHandler) finalizeAgentRunForDelivery(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	result *multiagent.RunResult,
	mcpExecutionIDs []string,
	reasoningContent string,
) agentfinalizer.Decision {
	return h.finalizeAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, result, mcpExecutionIDs, reasoningContent, false)
}

func (h *AgentHandler) finalizeAgentRunForDeliveryWithPolicy(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	result *multiagent.RunResult,
	mcpExecutionIDs []string,
	reasoningContent string,
	requireExecutionEvidence bool,
) agentfinalizer.Decision {
	decision := agentfinalizer.FromRunResult(h.db, result, agentfinalizer.Input{
		ConversationID:           conversationID,
		AssistantMessageID:       assistantMessageID,
		AgentMode:                agentMode,
		MCPExecutionIDs:          mcpExecutionIDs,
		RequireExecutionEvidence: requireExecutionEvidence,
	})
	h.persistFinalizationDecision(conversationID, assistantMessageID, agentMode, mcpExecutionIDs, reasoningContent, decision)
	return decision
}

func (h *AgentHandler) decideAgentRunForDeliveryWithPolicy(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	result *multiagent.RunResult,
	mcpExecutionIDs []string,
	requireExecutionEvidence bool,
) agentfinalizer.Decision {
	return agentfinalizer.FromRunResult(h.db, result, agentfinalizer.Input{
		ConversationID:           conversationID,
		AssistantMessageID:       assistantMessageID,
		AgentMode:                agentMode,
		MCPExecutionIDs:          mcpExecutionIDs,
		RequireExecutionEvidence: requireExecutionEvidence,
	})
}

func (h *AgentHandler) decideAgentRunForDelivery(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	result *multiagent.RunResult,
	mcpExecutionIDs []string,
) agentfinalizer.Decision {
	return agentfinalizer.FromRunResult(h.db, result, agentfinalizer.Input{
		ConversationID:           conversationID,
		AssistantMessageID:       assistantMessageID,
		AgentMode:                agentMode,
		MCPExecutionIDs:          mcpExecutionIDs,
		RequireExecutionEvidence: false,
	})
}

func (h *AgentHandler) persistFinalizationDecision(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	mcpExecutionIDs []string,
	reasoningContent string,
	decision agentfinalizer.Decision,
) {
	if assistantMessageID == "" || h.db == nil {
		return
	}
	_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "finalization_check", finalizationCheckMessage(decision), decision)
	if decision.Finalizable {
		if err := h.db.UpdateAssistantMessageFinalize(assistantMessageID, decision.FinalText, mcpExecutionIDs, reasoningContent); err != nil && h.logger != nil {
			h.logger.Warn("failed to update final assistant message", zap.Error(err), zap.String("conversationId", conversationID), zap.String("agentMode", agentMode))
		}
		return
	}
	_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", finalizationBlockedMessage(decision), time.Now(), assistantMessageID)
}

func (h *AgentHandler) finalizeCandidateForDelivery(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	response string,
	mcpExecutionIDs []string,
	awaitingHITL bool,
	reasoningContent string,
) agentfinalizer.Decision {
	return h.finalizeCandidateForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, response, mcpExecutionIDs, awaitingHITL, reasoningContent, false)
}

func (h *AgentHandler) finalizeCandidateForDeliveryWithPolicy(
	conversationID string,
	assistantMessageID string,
	agentMode string,
	response string,
	mcpExecutionIDs []string,
	awaitingHITL bool,
	reasoningContent string,
	requireExecutionEvidence bool,
) agentfinalizer.Decision {
	decision := agentfinalizer.Decide(h.db, agentfinalizer.Input{
		Response:                 response,
		ConversationID:           conversationID,
		AssistantMessageID:       assistantMessageID,
		AgentMode:                agentMode,
		MCPExecutionIDs:          mcpExecutionIDs,
		AwaitingHITL:             awaitingHITL,
		RequireExecutionEvidence: requireExecutionEvidence,
	})
	if assistantMessageID == "" || h.db == nil {
		return decision
	}
	_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "finalization_check", finalizationCheckMessage(decision), decision)
	if decision.Finalizable {
		if err := h.db.UpdateAssistantMessageFinalize(assistantMessageID, decision.FinalText, mcpExecutionIDs, reasoningContent); err != nil && h.logger != nil {
			h.logger.Warn("failed to update final assistant message", zap.Error(err), zap.String("conversationId", conversationID), zap.String("agentMode", agentMode))
		}
		return decision
	}
	_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", finalizationBlockedMessage(decision), time.Now(), assistantMessageID)
	return decision
}

func finalizationCheckMessage(d agentfinalizer.Decision) string {
	if d.Finalizable {
		return "The final-reply check passed."
	}
	return finalizationBlockedMessage(d)
}

func finalizationBlockedMessage(d agentfinalizer.Decision) string {
	if d.CompletionReason == "workflow_rejected" && strings.TrimSpace(d.FinalText) != "" {
		return d.FinalText
	}
	parts := []string{"The task has not met the final-reply condition; no success conclusion is generated."}
	if d.CompletionReason != "" {
		parts = append(parts, "Reason: "+d.CompletionReason)
	}
	if len(d.PendingExecutionIDs) > 0 {
		parts = append(parts, fmt.Sprintf("%d tool execution(s) still unfinished: %s", len(d.PendingExecutionIDs), strings.Join(d.PendingExecutionIDs, ", ")))
	}
	if len(d.MissingChecks) > 0 {
		parts = append(parts, "Missing checks: "+strings.Join(d.MissingChecks, "; "))
	}
	return strings.Join(parts, "\n")
}

func finalizationResponsePayload(d agentfinalizer.Decision, extra map[string]interface{}) map[string]interface{} {
	return agentfinalizer.ResponsePayload(d, extra)
}

func requestRequiresExecutionEvidence(req *ChatRequest) bool {
	return req != nil && req.Finalization.RequireExecutionEvidence != nil && *req.Finalization.RequireExecutionEvidence
}
