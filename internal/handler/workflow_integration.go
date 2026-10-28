package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/agentfinalizer"
	"cyberstrike-ai/internal/config"
	workflowrunner "cyberstrike-ai/internal/workflow"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *AgentHandler) finalizeWorkflowRunForDelivery(conversationID, messageID string, result *workflowrunner.RunResult) agentfinalizer.Decision {
	in := agentfinalizer.Input{ConversationID: conversationID, AssistantMessageID: messageID, AgentMode: "workflow"}
	if result == nil {
		in.Status = agentfinalizer.StatusFailed
		in.Response = "工作流未返回执行结果。"
	} else {
		in.Response = result.Response
		in.AwaitingHITL = result.AwaitingHITL
		in.Status = result.Status
		if result.Status == "rejected" {
			in.Status = agentfinalizer.StatusCancelled
			in.CompletionReason = "workflow_rejected"
		}
	}
	decision := agentfinalizer.Decide(h.db, in)
	h.persistFinalizationDecision(conversationID, messageID, "workflow", nil, "", decision)
	return decision
}

func (h *AgentHandler) roleForWorkflow(req *ChatRequest) (config.RoleConfig, bool) {
	if h == nil || h.config == nil || h.config.Roles == nil || req == nil {
		return config.RoleConfig{}, false
	}
	roleName := strings.TrimSpace(req.Role)
	if roleName == "" {
		return config.RoleConfig{}, false
	}
	role, ok := h.config.Roles[roleName]
	if !ok || !role.Enabled {
		return config.RoleConfig{}, false
	}
	if role.Name == "" {
		role.Name = roleName
	}
	if !workflowrunner.ShouldAutoRunRoleWorkflow(role) {
		return config.RoleConfig{}, false
	}
	return role, true
}

func (h *AgentHandler) runRoleWorkflowStreamIfBound(
	c *gin.Context,
	req *ChatRequest,
	prep *multiAgentPrepared,
	sendEvent func(eventType, message string, data interface{}),
	releaseAdmission ...func(),
) bool {
	role, ok := h.roleForWorkflow(req)
	if !ok || prep == nil {
		return false
	}

	conversationID := prep.ConversationID
	assistantMessageID := prep.AssistantMessageID
	userMessage := ""
	if req != nil {
		userMessage = req.Message
	}

	taskStatus := "completed"
	taskOwned := false
	var taskRunID string
	defer func() {
		if taskOwned {
			h.tasks.FinishTaskRun(conversationID, taskRunID, taskStatus)
		}
	}()

	if c == nil || c.Request == nil {
		return false
	}
	baseCtx, cancelWithCause := context.WithCancelCause(detachedAgentContext(c.Request.Context()))
	defer cancelWithCause(nil)
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)
	defer timeoutCancel()

	if startedTask, err := h.tasks.StartTask(conversationID, userMessage, cancelWithCause); err != nil {
		var errorMsg string
		if errors.Is(err, ErrTaskAlreadyRunning) {
			errorMsg = i18nText(requestWantsEnglish(c), "⚠️ 当前会话已有任务正在执行中，请等待当前任务完成或点击「停止任务」后再尝试。", "⚠️ a task is already running in this conversation; wait for it to complete or click “Stop Task” before trying again.")
			sendEvent("error", errorMsg, map[string]interface{}{
				"conversationId": conversationID,
				"errorType":      "task_already_running",
			})
		} else {
			errorMsg = fmt.Sprintf(i18nText(requestWantsEnglish(c), "❌ 无法启动任务: %s", "❌ failed to start task: %s"), err.Error())
			sendEvent("error", errorMsg, nil)
		}
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errorMsg, time.Now(), assistantMessageID)
		}
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		return true
	} else {
		taskRunID = startedTask.RunID
	}
	for _, release := range releaseAdmission {
		release()
	}
	baseCtx = h.tasks.BindProcessScope(baseCtx, conversationID, taskRunID)
	taskCtx = h.tasks.BindProcessScope(taskCtx, conversationID, taskRunID)
	taskOwned = true
	sendEvent = h.taskFinishingEventSender(sendEvent, conversationID, taskRunID, func() string { return taskStatus })

	progress := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, sendEvent)
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
			taskStatus = "cancelled"
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)
			cancelMsg := i18nText(requestWantsEnglish(c), "任务已被用户取消，后续操作已停止。", "task cancelled by the user, subsequent operations stopped.")
			if assistantMessageID != "" {
				if err := h.appendAssistantMessageNotice(assistantMessageID, cancelMsg); err != nil {
					h.logger.Warn("failed to update assistant message after cancel", zap.Error(err))
				}
				_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil)
			}
			sendEvent("cancelled", cancelMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
			})
			sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
			return true
		}
		if taskDeadlineExceeded(taskCtx) {
			taskStatus = "timeout"
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)
			timeoutMsg := "Task execution timed out; terminated automatically."
			if assistantMessageID != "" {
				_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", timeoutMsg, time.Now(), assistantMessageID)
				_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "timeout", timeoutMsg, nil)
			}
			sendEvent("error", timeoutMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
				"errorType":      "timeout",
			})
			sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
			return true
		}
		errMsg := "执行角色绑定流程失败: " + err.Error()
		taskStatus = "failed"
		h.tasks.UpdateTaskStatus(conversationID, taskStatus)
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), assistantMessageID)
			_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errMsg, nil)
		}
		sendEvent("error", errMsg, map[string]interface{}{"conversationId": conversationID})
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		return true
	}
	decision := h.finalizeWorkflowRunForDelivery(prep.ConversationID, prep.AssistantMessageID, result)
	responseText := decision.FinalText
	if !decision.Finalizable {
		responseText = finalizationBlockedMessage(decision)
		taskStatus = decision.Status
		h.tasks.UpdateTaskStatus(conversationID, taskStatus)
		sendEvent("finalization_check", responseText, decision)
	}
	payload := finalizationResponsePayload(decision, map[string]interface{}{
		"conversationId": prep.ConversationID,
		"messageId":      prep.AssistantMessageID,
		"agentMode":      "workflow",
		"workflowRunId":  result.RunID,
	})
	if result.AwaitingHITL {
		payload["workflowStatus"] = "awaiting_hitl"
		payload["awaitingHitl"] = true
	} else {
		payload["workflowStatus"] = result.Status
		payload["awaitingHitl"] = false
	}
	sendEvent("response", responseText, payload)
	sendEvent("done", "", map[string]interface{}{"conversationId": prep.ConversationID})
	return true
}

func (h *AgentHandler) runRoleWorkflowJSONIfBound(c *gin.Context, req *ChatRequest, prep *multiAgentPrepared, releaseAdmission ...func()) bool {
	role, ok := h.roleForWorkflow(req)
	if !ok || prep == nil {
		return false
	}

	conversationID := prep.ConversationID
	userMessage := ""
	if req != nil {
		userMessage = req.Message
	}

	taskStatus := "completed"
	taskOwned := false
	var taskRunID string
	defer func() {
		if taskOwned {
			h.tasks.FinishTaskRun(conversationID, taskRunID, taskStatus)
		}
	}()

	baseCtx, cancelWithCause := context.WithCancelCause(c.Request.Context())
	defer cancelWithCause(nil)
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)
	defer timeoutCancel()

	if startedTask, err := h.tasks.StartTask(conversationID, userMessage, cancelWithCause); err != nil {
		if errors.Is(err, ErrTaskAlreadyRunning) {
			c.JSON(http.StatusConflict, gin.H{
				"error":          i18nText(requestWantsEnglish(c), "⚠️ 当前会话已有任务正在执行中，请等待当前任务完成或点击「停止任务」后再尝试。", "⚠️ a task is already running in this conversation; wait for it to complete or click “Stop Task” before trying again."),
				"conversationId": conversationID,
				"errorType":      "task_already_running",
			})
		} else {
			respondErrorf(c, http.StatusInternalServerError, "❌ 无法启动任务: %s", "❌ failed to start task: %s", err.Error())
		}
		return true
	} else {
		taskRunID = startedTask.RunID
	}
	for _, release := range releaseAdmission {
		release()
	}
	baseCtx = h.tasks.BindProcessScope(baseCtx, conversationID, taskRunID)
	taskCtx = h.tasks.BindProcessScope(taskCtx, conversationID, taskRunID)
	taskOwned = true
	respond := h.taskFinishingJSONResponder(c, conversationID, taskRunID, func() string { return taskStatus })

	h.runWorkflowJSON(taskCtx, baseCtx, cancelWithCause, req, prep, role, &taskStatus, requestWantsEnglish(c), respond)
	return true
}
