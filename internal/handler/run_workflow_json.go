package handler

import (
	"context"
	"cyberstrike-ai/internal/config"
	workflowrunner "cyberstrike-ai/internal/workflow"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (h *AgentHandler) runWorkflowJSON(taskCtx, baseCtx context.Context, cancelWithCause context.CancelCauseFunc, req *ChatRequest, prep *multiAgentPrepared, role config.RoleConfig, status *string, respond func(int, interface{})) {
	*status = "completed"
	conversationID := prep.ConversationID
	assistantMessageID := prep.AssistantMessageID
	progress := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, nil)
	result, err := workflowrunner.RunRoleBoundWorkflow(taskCtx, workflowrunner.RunArgs{
		DB:                 h.db,
		Logger:             h.logger,
		Role:               role,
		AppCfg:             h.config,
		Agent:              h.agent,
		ConversationID:     conversationID,
		ProjectID:          h.conversationProjectID(conversationID),
		UserMessage:        prep.FinalMessage,
		History:            prep.History,
		RoleTools:          prep.RoleTools,
		AgentsMarkdownDir:  h.agentsMarkdownDir,
		SystemPromptExtra:  h.agentSessionContextBlock(conversationID),
		AssistantMessageID: assistantMessageID,
		Progress:           progress,
	})
	if err != nil {
		cause := context.Cause(baseCtx)
		if errors.Is(cause, ErrTaskCancelled) {
			*status = "cancelled"
			cancelMsg := "任务已被用户取消，后续操作已停止。"
			if assistantMessageID != "" {
				_ = h.appendAssistantMessageNotice(assistantMessageID, cancelMsg)
				_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil)
			}
			respond(http.StatusOK, gin.H{
				"status":         "cancelled",
				"message":        cancelMsg,
				"conversationId": conversationID,
			})
			return
		}
		errMsg := "执行角色绑定流程失败: " + err.Error()
		*status = "failed"
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), assistantMessageID)
		}
		respond(http.StatusInternalServerError, gin.H{"error": errMsg, "conversationId": conversationID})
		return
	}
	decision := h.finalizeWorkflowRunForDelivery(prep.ConversationID, prep.AssistantMessageID, result)
	responseText := decision.FinalText
	if !decision.Finalizable {
		responseText = finalizationBlockedMessage(decision)
		*status = decision.Status
	}
	respond(http.StatusOK, gin.H{
		"response":            responseText,
		"conversationId":      prep.ConversationID,
		"assistantMessageId":  prep.AssistantMessageID,
		"agentMode":           "workflow",
		"workflowRunId":       result.RunID,
		"workflowStatus":      result.Status,
		"awaitingHitl":        result.AwaitingHITL,
		"finalized":           decision.Finalized,
		"finalizable":         decision.Finalizable,
		"status":              decision.Status,
		"completionReason":    decision.CompletionReason,
		"evidenceVerified":    decision.EvidenceVerified,
		"evidenceRefs":        decision.EvidenceRefs,
		"pendingExecutionIds": decision.PendingExecutionIDs,
		"missingChecks":       decision.MissingChecks,
	})
	return
}
