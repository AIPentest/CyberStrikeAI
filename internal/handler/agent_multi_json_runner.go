package handler

import (
	"context"
	"cyberstrike-ai/internal/agentfinalizer"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/multiagent"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
	"strings"
	"time"
)

func (h *AgentHandler) runMultiJSON(taskCtx, baseCtx context.Context, cancelWithCause context.CancelCauseFunc, req *ChatRequest, prep *multiAgentPrepared, status *string, respond func(int, interface{})) {
	progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, prep.ConversationID, prep.AssistantMessageID, nil)
	taskCtx = multiagent.WithHITLToolInterceptor(taskCtx, func(ctx context.Context, toolName, arguments string) (string, error) {
		return h.interceptHITLForEinoTool(ctx, cancelWithCause, prep.ConversationID, prep.AssistantMessageID, nil, toolName, arguments)
	})
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
	effectiveOrch := config.NormalizeMultiAgentOrchestration(h.config.MultiAgent.Orchestration)
	if o := strings.TrimSpace(req.Orchestration); o != "" {
		effectiveOrch = config.NormalizeMultiAgentOrchestration(o)
	}
	agentMode := "eino_" + effectiveOrch
	var decision agentfinalizer.Decision
	var autoCancelledPendingExecutionIDs []string
	for {
		result, runErr = multiagent.RunDeepAgent(
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
			h.agentsMarkdownDir,
			strings.TrimSpace(req.Orchestration),
			chatReasoningToClientIntent(req.Reasoning),
			h.agentSessionContextBlock(prep.ConversationID),
		)
		if runErr != nil {
			if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
				h.persistEinoAgentTraceForResume(prep.ConversationID, result)
			}
			h.logger.Error("Eino DeepAgent 执行失败", zap.Error(runErr))
			clientErr := multiagent.EinoClientRunErrorMessage(runErr)
			errMsg := "执行失败: " + clientErr
			if prep.AssistantMessageID != "" {
				_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), prep.AssistantMessageID)
			}
			errData := multiagent.EinoClientRunErrorFields(runErr)
			errData["error"] = errMsg
			respond(http.StatusInternalServerError, errData)
			return
		}
		mw := &h.config.MultiAgent.EinoMiddleware
		if h.tryContinueOnEinoEmptyResponse(taskCtx, mw, prep.ConversationID, result, &emptyResponseContinueAttempt, &curHist, &curMsg, progressCallback) {
			continue
		}
		decision = h.decideAgentRunForDeliveryWithPolicy(prep.ConversationID, prep.AssistantMessageID, agentMode, result, result.MCPExecutionIDs, requestRequiresExecutionEvidence(req))
		if cancelled := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, prep.ConversationID, decision, progressCallback); len(cancelled) > 0 {
			autoCancelledPendingExecutionIDs = mergeMCPExecutionIDLists(autoCancelledPendingExecutionIDs, cancelled)
			decision = h.decideAgentRunForDeliveryWithPolicy(prep.ConversationID, prep.AssistantMessageID, agentMode, result, result.MCPExecutionIDs, requestRequiresExecutionEvidence(req))
		}
		if h.tryAutoContinueAfterFinalization(taskCtx, prep.ConversationID, result, decision, &finalizationAutoContinueAttempt, &curHist, &curMsg, progressCallback) {
			continue
		}
		break
	}

	h.persistFinalizationDecision(prep.ConversationID, prep.AssistantMessageID, agentMode, result.MCPExecutionIDs, multiagent.AggregatedReasoningFromTraceJSON(result.LastAgentTraceInput), decision)

	if result.LastAgentTraceInput != "" || result.LastAgentTraceOutput != "" {
		if err := h.db.SaveAgentTrace(prep.ConversationID, result.LastAgentTraceInput, result.LastAgentTraceOutput); err != nil {
			h.logger.Warn("保存代理轨迹失败", zap.Error(err))
		}
	}

	responseText := decision.FinalText
	if !decision.Finalizable {
		responseText = finalizationBlockedMessage(decision)
	}

	*status = decision.Status
	if *status == "" {
		*status = "completed"
	}
	respond(http.StatusOK, ChatResponse{
		Response:                         responseText,
		MCPExecutionIDs:                  result.MCPExecutionIDs,
		ConversationID:                   prep.ConversationID,
		Time:                             time.Now(),
		Finalizable:                      decision.Finalizable,
		Finalized:                        decision.Finalized,
		Status:                           decision.Status,
		CompletionReason:                 decision.CompletionReason,
		EvidenceVerified:                 decision.EvidenceVerified,
		EvidenceRefs:                     decision.EvidenceRefs,
		PendingExecutionIDs:              decision.PendingExecutionIDs,
		MissingChecks:                    decision.MissingChecks,
		AutoCancelledPendingExecutionIDs: autoCancelledPendingExecutionIDs,
	})
}
