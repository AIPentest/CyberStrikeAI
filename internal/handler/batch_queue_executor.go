package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"

	"go.uber.org/zap"
)

const batchQueueWorkerIdlePoll = 200 * time.Millisecond

// executeBatchQueue 使用并发 worker 池执行批量任务队列。
func (h *AgentHandler) executeBatchQueue(queueID string) {
	defer h.batchTaskManager.UnmarkQueueExecutor(queueID)

	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		return
	}
	concurrency := normalizeBatchQueueConcurrency(queue.Concurrency)
	h.logger.Info("开始执行批量任务队列", zap.String("queueId", queueID), zap.Int("concurrency", concurrency))

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.runBatchQueueWorker(queueID)
		}()
	}
	wg.Wait()

	h.tryFinalizeBatchQueue(queueID)
}

func (h *AgentHandler) runBatchQueueWorker(queueID string) {
	for {
		queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
		if batchQueueExecutionShouldStop(queue, exists) {
			return
		}

		task, ok := h.batchTaskManager.ClaimNextPendingTask(queueID)
		if !ok {
			if !h.batchTaskManager.HasRunningTasks(queueID) {
				return
			}
			time.Sleep(batchQueueWorkerIdlePoll)
			continue
		}

		queue, _ = h.batchTaskManager.GetBatchQueue(queueID)
		if queue == nil {
			return
		}

		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusRunning, "", "")
		h.executeOneBatchSubTask(queueID, queue, task)

		if h.batchTaskManager.TakeSingleRunTaskIfMatch(queueID, task.ID) {
			h.batchTaskManager.UpdateQueueStatus(queueID, BatchQueueStatusPaused)
			h.logger.Info("单条执行完成，队列已暂停", zap.String("queueId", queueID), zap.String("taskId", task.ID))
			return
		}

		queue, exists = h.batchTaskManager.GetBatchQueue(queueID)
		if batchQueueExecutionShouldStop(queue, exists) {
			if !exists {
				h.logger.Warn("批量队列在执行收尾时已不存在，安全退出", zap.String("queueId", queueID))
			}
			return
		}
	}
}

func (h *AgentHandler) tryFinalizeBatchQueue(queueID string) {
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists || queue == nil {
		return
	}
	if queue.Status != BatchQueueStatusRunning {
		return
	}
	if h.batchTaskManager.HasPendingOrRunningTasks(queueID) {
		return
	}

	lastRunErr := ""
	for _, t := range queue.Tasks {
		if t != nil && t.Status == BatchTaskStatusFailed && t.Error != "" {
			lastRunErr = t.Error
		}
	}
	h.batchTaskManager.SetLastRunError(queueID, lastRunErr)
	h.batchTaskManager.UpdateQueueStatus(queueID, BatchQueueStatusCompleted)
	h.logger.Info("批量任务队列执行完成", zap.String("queueId", queueID))
}

// executeOneBatchSubTask 执行单条批量子任务（各自独立会话）。
func (h *AgentHandler) executeOneBatchSubTask(queueID string, queue *BatchTaskQueue, task *BatchTask) {
	ownerUserID := h.db.GetResourceOwner("batch_task", queueID)
	access, accessErr := h.db.ResolveRBACAccess(ownerUserID)
	if accessErr != nil || access == nil || !access.User.Enabled {
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", "队列所有者不存在或已禁用")
		return
	}
	principal := authctx.NewPrincipalWithScopes(access.User.ID, access.User.Username, access.Scope, access.Permissions, access.PermissionScopes)
	title := safeTruncateString(task.Message, 50)
	batchMeta := batchSubTaskConversationMeta(h.config, queue)
	conv, err := h.db.CreateConversation(title, batchMeta)
	if err != nil {
		h.logger.Error("创建对话失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", "创建对话失败: "+err.Error())
		return
	}
	conversationID := conv.ID
	_ = h.db.SetResourceOwner("conversation", conversationID, access.User.ID)
	_ = h.db.AssignResourceToUser(access.User.ID, "conversation", conversationID)

	h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, BatchTaskStatusRunning, "", "", conversationID)

	finalMessage := task.Message
	var roleTools []string
	if queue.Role != "" && queue.Role != "默认" {
		if h.config.Roles != nil {
			if role, exists := h.config.Roles[queue.Role]; exists && role.Enabled {
				if role.UserPrompt != "" {
					finalMessage = role.UserPrompt + "\n\n" + task.Message
					h.logger.Info("应用角色用户提示词", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role))
				}
				if len(role.Tools) > 0 {
					roleTools = role.Tools
					h.logger.Info("使用角色配置的工具列表", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role), zap.Int("toolCount", len(roleTools)))
				}
			}
		}
	}

	if _, err = h.db.AddMessage(conversationID, "user", task.Message, nil); err != nil {
		h.logger.Error("保存用户消息失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
	}

	assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "处理中...", nil)
	if err != nil {
		h.logger.Error("创建助手消息失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
		assistantMsg = nil
	}

	var assistantMessageID string
	if assistantMsg != nil {
		assistantMessageID = assistantMsg.ID
	}

	h.logger.Info("执行批量任务", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("message", task.Message), zap.String("role", queue.Role), zap.String("conversationId", conversationID))

	principalCtx := authctx.WithPrincipal(context.Background(), principal)
	baseCtx, cancelWithCause := context.WithCancelCause(principalCtx)
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 6*time.Hour)

	registered := false
	var taskRunID string
	finishStatus := "completed"

	defer func() {
		h.batchTaskManager.SetTaskCancel(queueID, task.ID, nil)
		timeoutCancel()
		if registered {
			h.tasks.FinishTaskRun(conversationID, taskRunID, finishStatus)
		}
		cancelWithCause(nil)
	}()

	sendEvent := func(eventType, message string, data interface{}) {
		if h.taskEventBus == nil {
			return
		}
		ev := StreamEvent{Type: eventType, Message: message, Data: data}
		b, err := json.Marshal(ev)
		if err != nil {
			b = []byte(`{"type":"error","message":"marshal failed"}`)
		}
		line := make([]byte, 0, len(b)+8)
		line = append(line, []byte("data: ")...)
		line = append(line, b...)
		line = append(line, '\n', '\n')
		h.taskEventBus.Publish(conversationID, line)
	}

	if startedTask, err := h.tasks.StartTask(conversationID, task.Message, cancelWithCause); err != nil {
		h.logger.Warn("批量队列子任务注册会话运行状态失败",
			zap.String("queueId", queueID),
			zap.String("taskId", task.ID),
			zap.String("conversationId", conversationID),
			zap.Error(err))
		failMsg := err.Error()
		if errors.Is(err, ErrTaskAlreadyRunning) {
			failMsg = "会话已有任务正在执行，无法在该会话上并行启动批量子任务"
		}
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", failMsg)
		return
	} else {
		taskRunID = startedTask.RunID
	}
	baseCtx = h.tasks.BindProcessScope(baseCtx, conversationID, taskRunID)
	taskCtx = h.tasks.BindProcessScope(taskCtx, conversationID, taskRunID)
	registered = true
	h.batchTaskManager.SetTaskCancel(queueID, task.ID, timeoutCancel)

	if err := validateBatchHITLPolicy(queue.HITLPolicy); err != nil {
		finishStatus = "failed"
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", err.Error())
		return
	}
	if h.hitlManager == nil {
		finishStatus = "failed"
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", "审批服务未初始化")
		return
	}
	hitlReq := h.batchHITLRequest(queue.HITLPolicy)
	if err := h.hitlManager.SaveConversationConfig(conversationID, hitlReq); err != nil {
		finishStatus = "failed"
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", "保存审批设置失败: "+err.Error())
		return
	}
	h.activateHITLForConversation(conversationID, hitlReq)
	defer h.hitlManager.DeactivateConversation(conversationID)
	taskCtx = multiagent.WithHITLToolInterceptor(taskCtx, func(ctx context.Context, toolName, arguments string) (string, error) {
		return h.interceptHITLForEinoTool(ctx, cancelWithCause, conversationID, assistantMessageID, sendEvent, toolName, arguments)
	})

	progressCallback := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, sendEvent)
	taskCtx = mcp.WithMCPConversationID(taskCtx, conversationID)
	taskCtx = mcp.WithToolRunRegistry(taskCtx, h.tasks)
	taskCtx = mcp.WithEinoExecuteRunRegistry(taskCtx, h.tasks)

	useBatchMulti := false
	batchOrch := "deep"
	am := strings.TrimSpace(strings.ToLower(queue.AgentMode))
	if am == "multi" {
		am = "deep"
	}
	if batchQueueWantsEino(queue.AgentMode) && h.config != nil && h.config.MultiAgent.Enabled {
		useBatchMulti = true
		batchOrch = config.NormalizeMultiAgentOrchestration(am)
	} else if queue.AgentMode == "" && h.config != nil && h.config.MultiAgent.Enabled && h.config.MultiAgent.BatchUseMultiAgent {
		useBatchMulti = true
		batchOrch = "deep"
	}
	if useBatchMulti {
		_ = h.db.SetConversationAgentMode(conversationID, batchOrch)
	} else {
		_ = h.db.SetConversationAgentMode(conversationID, "eino_single")
	}

	var resultMA *multiagent.RunResult
	var runErr error
	switch {
	case useBatchMulti:
		resultMA, runErr = multiagent.RunDeepAgent(taskCtx, h.config, &h.config.MultiAgent, h.agent, h.db, h.logger, conversationID, h.conversationProjectID(conversationID), finalMessage, []agent.ChatMessage{}, roleTools, progressCallback, h.agentsMarkdownDir, batchOrch, nil, h.agentSessionContextBlock(conversationID))
	default:
		if h.config == nil {
			runErr = fmt.Errorf("服务器配置未加载")
		} else {
			resultMA, runErr = multiagent.RunEinoSingleChatModelAgent(taskCtx, h.config, &h.config.MultiAgent, h.agent, h.db, h.logger, conversationID, h.conversationProjectID(conversationID), finalMessage, []agent.ChatMessage{}, roleTools, progressCallback, nil, h.agentSessionContextBlock(conversationID))
		}
	}

	if runErr != nil {
		h.handleBatchSubTaskRunError(queueID, task, conversationID, assistantMessageID, baseCtx, taskCtx, resultMA, runErr, &finishStatus)
		return
	}

	if resultMA == nil {
		h.logger.Error("批量任务执行成功但无结果对象",
			zap.String("queueId", queueID),
			zap.String("taskId", task.ID),
			zap.String("conversationId", conversationID))
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", "内部错误：无执行结果")
		return
	}

	h.logger.Info("批量任务执行成功", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))

	mcpIDs := resultMA.MCPExecutionIDs
	lastIn := resultMA.LastAgentTraceInput
	lastOut := resultMA.LastAgentTraceOutput
	reasoningContent := multiagent.AggregatedReasoningFromTraceJSON(lastIn)
	agentMode := "batch_eino_single"
	if useBatchMulti {
		agentMode = "batch_eino_" + batchOrch
	}
	decision := h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, resultMA, mcpIDs, true)
	autoCancelledPendingExecutionIDs := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, conversationID, decision, progressCallback)
	if len(autoCancelledPendingExecutionIDs) > 0 {
		decision = h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, resultMA, mcpIDs, true)
	}
	h.persistFinalizationDecision(conversationID, assistantMessageID, agentMode, mcpIDs, reasoningContent, decision)
	resText := decision.FinalText
	if !decision.Finalizable {
		resText = finalizationBlockedMessage(decision)
		finishStatus = decision.Status
		sendEvent("finalization_check", resText, decision)
	}
	sendEvent("response", resText, finalizationResponsePayload(decision, map[string]interface{}{
		"conversationId":                   conversationID,
		"messageId":                        assistantMessageID,
		"agentMode":                        agentMode,
		"mcpExecutionIds":                  mcpIDs,
		"batchQueueId":                     queueID,
		"batchTaskId":                      task.ID,
		"batchTaskStatus":                  map[bool]string{true: string(BatchTaskStatusCompleted), false: string(BatchTaskStatusFailed)}[decision.Finalizable],
		"candidatePreview":                 safeTruncateString(resultMA.Response, 500),
		"autoCancelledPendingExecutionIds": autoCancelledPendingExecutionIDs,
	}))

	if assistantMessageID == "" {
		_, err = h.db.AddMessage(conversationID, "assistant", resText, mcpIDs)
	} else if !decision.Finalizable {
		err = nil
	}
	if err != nil {
		h.logger.Error("保存助手消息失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
	}

	if lastIn != "" || lastOut != "" {
		if err := h.db.SaveAgentTrace(conversationID, lastIn, lastOut); err != nil {
			h.logger.Warn("保存代理轨迹失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
		}
	}

	if cleanupErr := h.tasks.FinishTaskRun(conversationID, taskRunID, finishStatus); cleanupErr != nil {
		h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, BatchTaskStatusFailed, resText, cleanupErr.Error(), conversationID)
		return
	}
	if !decision.Finalizable {
		h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, BatchTaskStatusFailed, resText, finalizationCheckMessage(decision), conversationID)
		return
	}
	h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, BatchTaskStatusCompleted, resText, "", conversationID)
}

func batchSubTaskConversationMeta(cfg *config.Config, queue *BatchTaskQueue) database.ConversationCreateMeta {
	meta := audit.ConversationCreateMeta("batch_task")
	if queue == nil {
		meta.ProjectID = effectiveProjectID(cfg, "")
		return meta
	}
	meta.ProjectID = effectiveProjectID(cfg, queue.ProjectID)
	meta.RoleName = strings.TrimSpace(queue.Role)
	return meta
}

func (h *AgentHandler) handleBatchSubTaskRunError(
	queueID string,
	task *BatchTask,
	conversationID, assistantMessageID string,
	baseCtx, taskCtx context.Context,
	resultMA *multiagent.RunResult,
	runErr error,
	finishStatus *string,
) {
	if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
		h.persistEinoAgentTraceForResume(conversationID, resultMA)
	}
	errStr := runErr.Error()
	partialResp := ""
	if resultMA != nil {
		partialResp = resultMA.Response
	}
	isCancelled := errors.Is(context.Cause(baseCtx), ErrTaskCancelled) ||
		errors.Is(runErr, context.Canceled) ||
		strings.Contains(strings.ToLower(errStr), "context canceled") ||
		strings.Contains(strings.ToLower(errStr), "context cancelled") ||
		(partialResp != "" && (strings.Contains(partialResp, "任务已被取消") || strings.Contains(partialResp, "任务执行中断")))
	isTimeout := errors.Is(runErr, context.DeadlineExceeded) || errors.Is(context.Cause(taskCtx), context.DeadlineExceeded)

	if isTimeout {
		*finishStatus = "timeout"
	} else if isCancelled {
		*finishStatus = "cancelled"
	} else {
		*finishStatus = "failed"
	}

	if isCancelled {
		h.logger.Info("批量任务被取消", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))
		cancelMsg := "任务已被用户取消，后续操作已停止。"
		if partialResp != "" && (strings.Contains(partialResp, "任务已被取消") || strings.Contains(partialResp, "任务执行中断")) {
			cancelMsg = partialResp
		}
		if assistantMessageID != "" {
			if updateErr := h.appendAssistantMessageNotice(assistantMessageID, cancelMsg); updateErr != nil {
				h.logger.Warn("更新取消后的助手消息失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
			}
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil); err != nil {
				h.logger.Warn("保存取消详情失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
			}
		} else if _, errMsg := h.db.AddMessage(conversationID, "assistant", cancelMsg, nil); errMsg != nil {
			h.logger.Warn("保存取消消息失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(errMsg))
		}
		h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, BatchTaskStatusCancelled, cancelMsg, "", conversationID)
		return
	}

	h.logger.Error("批量任务执行失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(runErr))
	clientErr := multiagent.EinoClientRunErrorMessage(runErr)
	errorMsg := "执行失败: " + clientErr
	if assistantMessageID != "" {
		if _, updateErr := h.db.Exec(
			"UPDATE messages SET content = ?, updated_at = ? WHERE id = ?",
			errorMsg,
			time.Now(), assistantMessageID,
		); updateErr != nil {
			h.logger.Warn("更新失败后的助手消息失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
		}
		if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errorMsg, nil); err != nil {
			h.logger.Warn("保存错误详情失败", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
		}
	}
	h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, BatchTaskStatusFailed, "", clientErr)
}
