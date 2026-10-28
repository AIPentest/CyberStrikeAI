package handler

import (
	"context"
	"cyberstrike-ai/internal/agentfinalizer"
	"cyberstrike-ai/internal/multiagent"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *AgentHandler) runSingleJSON(taskCtx, baseCtx context.Context, cancelWithCause context.CancelCauseFunc, req *ChatRequest, prep *multiAgentPrepared, status *string, wantEnglish bool, respond func(int, interface{})) {
	progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, prep.ConversationID, prep.AssistantMessageID, nil)
	taskCtx = multiagent.WithHITLToolInterceptor(taskCtx, func(ctx context.Context, toolName, arguments string) (string, error) {
		return h.interceptHITLForEinoTool(ctx, cancelWithCause, prep.ConversationID, prep.AssistantMessageID, nil, toolName, arguments)
	})

	if h.config == nil {
		respond(http.StatusInternalServerError, gin.H{"error": i18nText(wantEnglish, "服务器配置未加载", "server config not loaded")})
		return
	}
	runCfg, _, err := h.configForAIChannel(req.AIChannelID)
	if err != nil {
		respond(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	curHist := prep.History
	curMsg := prep.FinalMessage
	var result *multiagent.RunResult
	var runErr error
	var emptyResponseContinueAttempt int
	var finalizationAutoContinueAttempt int
	var decision agentfinalizer.Decision
	var autoCancelledPendingExecutionIDs []string
	for {
		result, runErr = multiagent.RunEinoSingleChatModelAgent(
			taskCtx,
			runCfg,
			&runCfg.MultiAgent,
			h.agent,
			h.db,
			h.logger,
			prep.ConversationID,
			h.conversationProjectID(prep.ConversationID),
			curMsg,
			curHist,
			prep.RoleTools,
			progressCallback,
			chatReasoningToClientIntent(req.Reasoning),
			h.agentSessionContextBlock(prep.ConversationID),
		)
		if runErr != nil {
			if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
				h.persistEinoAgentTraceForResume(prep.ConversationID, result)
			}
			respond(http.StatusInternalServerError, gin.H{"error": runErr.Error()})
			return
		}
		mw := &h.config.MultiAgent.EinoMiddleware
		if h.tryContinueOnEinoEmptyResponse(taskCtx, mw, prep.ConversationID, result, &emptyResponseContinueAttempt, &curHist, &curMsg, progressCallback) {
			continue
		}
		decision = h.decideAgentRunForDeliveryWithPolicy(prep.ConversationID, prep.AssistantMessageID, "eino_single", result, result.MCPExecutionIDs, requestRequiresExecutionEvidence(req))
		if cancelled := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, prep.ConversationID, decision, progressCallback); len(cancelled) > 0 {
			autoCancelledPendingExecutionIDs = mergeMCPExecutionIDLists(autoCancelledPendingExecutionIDs, cancelled)
			decision = h.decideAgentRunForDeliveryWithPolicy(prep.ConversationID, prep.AssistantMessageID, "eino_single", result, result.MCPExecutionIDs, requestRequiresExecutionEvidence(req))
		}
		if h.tryAutoContinueAfterFinalization(taskCtx, prep.ConversationID, result, decision, &finalizationAutoContinueAttempt, &curHist, &curMsg, progressCallback) {
			continue
		}
		break
	}

	h.persistFinalizationDecision(prep.ConversationID, prep.AssistantMessageID, "eino_single", result.MCPExecutionIDs, multiagent.AggregatedReasoningFromTraceJSON(result.LastAgentTraceInput), decision)
	if result.LastAgentTraceInput != "" || result.LastAgentTraceOutput != "" {
		_ = h.db.SaveAgentTrace(prep.ConversationID, result.LastAgentTraceInput, result.LastAgentTraceOutput)
	}

	responseText := decision.FinalText
	if !decision.Finalizable {
		responseText = finalizationBlockedMessage(decision)
	}

	*status = decision.Status
	if *status == "" {
		*status = "completed"
	}
	respond(http.StatusOK, gin.H{
		"response":                         responseText,
		"conversationId":                   prep.ConversationID,
		"mcpExecutionIds":                  result.MCPExecutionIDs,
		"assistantMessageId":               prep.AssistantMessageID,
		"agentMode":                        "eino_single",
		"finalized":                        decision.Finalized,
		"finalizable":                      decision.Finalizable,
		"status":                           decision.Status,
		"completionReason":                 decision.CompletionReason,
		"evidenceVerified":                 decision.EvidenceVerified,
		"evidenceRefs":                     decision.EvidenceRefs,
		"pendingExecutionIds":              decision.PendingExecutionIDs,
		"missingChecks":                    decision.MissingChecks,
		"autoCancelledPendingExecutionIds": autoCancelledPendingExecutionIDs,
	})
}
