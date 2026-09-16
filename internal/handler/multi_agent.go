package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/agentfinalizer"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MultiAgentLoopStream Eino DeepAgent 流式对话（需 config.multi_agent.enabled）。
func (h *AgentHandler) MultiAgentLoopStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	if h.config == nil || !h.config.MultiAgent.Enabled {
		ev := StreamEvent{Type: "error", Message: "多代理未启用，请在设置或 config.yaml 中开启 multi_agent.enabled"}
		b, _ := json.Marshal(ev)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		done := StreamEvent{Type: "done", Message: ""}
		db, _ := json.Marshal(done)
		fmt.Fprintf(c.Writer, "data: %s\n\n", db)
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		event := StreamEvent{Type: "error", Message: "请求参数错误: " + err.Error()}
		b, _ := json.Marshal(event)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		done := StreamEvent{Type: "done", Message: ""}
		db, _ := json.Marshal(done)
		fmt.Fprintf(c.Writer, "data: %s\n\n", db)
		c.Writer.Flush()
		return
	}

	c.Header("X-Accel-Buffering", "no")

	// 用于在 sendEvent 中判断是否为用户主动停止导致的取消。
	// 注意：baseCtx 会在后面创建；该变量用于闭包提前捕获引用。
	var baseCtx context.Context

	clientDisconnected := false
	// 与 sseKeepalive 共用：禁止并发写 ResponseWriter，否则会破坏 chunked 编码（ERR_INVALID_CHUNKED_ENCODING）。
	var sseWriteMu sync.Mutex
	var ssePublishConversationID string
	sendEvent := func(eventType, message string, data interface{}) {
		// 用户主动停止时，Eino 可能仍会并发上报 eventType=="error"。
		// 为避免 UI 看到“取消错误 + cancelled 文案”两条回复，这里直接丢弃取消对应的 error。
		if eventType == "error" && baseCtx != nil {
			cause := context.Cause(baseCtx)
			if errors.Is(cause, ErrTaskCancelled) || errors.Is(cause, multiagent.ErrInterruptContinue) {
				return
			}
		}
		ev := StreamEvent{Type: eventType, Message: message, Data: data}
		b, errMarshal := json.Marshal(ev)
		if errMarshal != nil {
			b = []byte(`{"type":"error","message":"marshal failed"}`)
		}
		sseLine := make([]byte, 0, len(b)+8)
		sseLine = append(sseLine, []byte("data: ")...)
		sseLine = append(sseLine, b...)
		sseLine = append(sseLine, '\n', '\n')
		if ssePublishConversationID != "" && h.taskEventBus != nil {
			h.taskEventBus.Publish(ssePublishConversationID, sseLine)
		}
		if clientDisconnected {
			return
		}
		select {
		case <-c.Request.Context().Done():
			clientDisconnected = true
			return
		default:
		}
		sseWriteMu.Lock()
		_, err := c.Writer.Write(sseLine)
		if err != nil {
			sseWriteMu.Unlock()
			clientDisconnected = true
			return
		}
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		} else {
			c.Writer.Flush()
		}
		sseWriteMu.Unlock()
	}

	h.logger.Info("收到 Eino DeepAgent 流式请求",
		zap.String("conversationId", req.ConversationID),
	)

	prep, err := h.prepareMultiAgentSession(&req, c, "multi_agent_stream")
	if err != nil {
		sendEvent("error", err.Error(), nil)
		sendEvent("done", "", nil)
		return
	}
	ssePublishConversationID = prep.ConversationID
	if prep.CreatedNew {
		sendEvent("conversation", "会话已创建", map[string]interface{}{
			"conversationId": prep.ConversationID,
		})
	}

	conversationID := prep.ConversationID
	assistantMessageID := prep.AssistantMessageID
	h.activateHITLForConversation(conversationID, req.Hitl)
	if h.hitlManager != nil {
		defer h.hitlManager.DeactivateConversation(conversationID)
	}

	if prep.UserMessageID != "" {
		sendEvent("message_saved", "", map[string]interface{}{
			"conversationId": conversationID,
			"userMessageId":  prep.UserMessageID,
		})
	}
	if h.runRoleWorkflowStreamIfBound(c, &req, prep, sendEvent) {
		return
	}

	var cancelWithCause context.CancelCauseFunc
	curFinalMessage := prep.FinalMessage
	curHistory := prep.History
	roleTools := prep.RoleTools
	orch := strings.TrimSpace(req.Orchestration)

	taskStatus := "completed"
	// 仅在成功 StartTask 后再 FinishTask；避免「任务已存在」分支 return 时误删正在运行的同会话任务。
	taskOwned := false
	var taskRunID string
	defer func() {
		if taskOwned {
			h.tasks.FinishTaskRun(conversationID, taskRunID, taskStatus)
		}
	}()

	sendEvent("progress", "正在启动 Eino 多代理...", map[string]interface{}{
		"conversationId": conversationID,
	})

	stopKeepalive := runSSEKeepalive(c, &sseWriteMu)
	defer stopKeepalive()
	runCfg, _, err := h.configForAIChannel(req.AIChannelID)
	if err != nil {
		sendEvent("error", err.Error(), nil)
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		return
	}

	var result *multiagent.RunResult
	var runErr error

	baseCtx, cancelWithCause = context.WithCancelCause(detachedAgentContext(c.Request.Context()))
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)

	if startedTask, err := h.tasks.StartTask(conversationID, req.Message, cancelWithCause); err != nil {
		var errorMsg string
		if errors.Is(err, ErrTaskAlreadyRunning) {
			errorMsg = "⚠️ 当前会话已有任务正在执行中，请等待当前任务完成或点击「停止任务」后再尝试。"
			sendEvent("error", errorMsg, map[string]interface{}{
				"conversationId": conversationID,
				"errorType":      "task_already_running",
			})
		} else {
			errorMsg = "❌ 无法启动任务: " + err.Error()
			sendEvent("error", errorMsg, nil)
		}
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errorMsg, time.Now(), assistantMessageID)
		}
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		timeoutCancel()
		return
	} else {
		taskRunID = startedTask.RunID
	}
	baseCtx = h.tasks.BindProcessScope(baseCtx, conversationID, taskRunID)
	taskCtx = h.tasks.BindProcessScope(taskCtx, conversationID, taskRunID)
	taskOwned = true
	sendEvent = h.taskFinishingEventSender(sendEvent, conversationID, taskRunID, func() string { return taskStatus })

	// 同一 HTTP 流内多段 Run（如中断并继续）合并 MCP execution id，供最终 response / 库表与工具芯片展示完整列表
	var cumulativeMCPExecutionIDs []string
	// 同一请求内分段续跑时，主代理 iteration 事件按偏移累计，避免 UI 出现「第3轮 → 第1轮」回跳。
	var mainIterationOffset int
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
		segmentMainIterationMax := 0
		rawProgressCallback := h.createProgressCallback(taskCtx, cancelWithCause, conversationID, assistantMessageID, sendEvent)
		progressCallback := func(eventType, message string, data interface{}) {
			if eventType == "iteration" {
				if m, ok := data.(map[string]interface{}); ok {
					if scope, _ := m["einoScope"].(string); scope == "main" {
						raw := 0
						switch v := m["iteration"].(type) {
						case int:
							raw = v
						case int32:
							raw = int(v)
						case int64:
							raw = int(v)
						case float64:
							raw = int(v)
						case float32:
							raw = int(v)
						}
						if raw > 0 {
							if raw > segmentMainIterationMax {
								segmentMainIterationMax = raw
							}
							m["iteration"] = raw + mainIterationOffset
						}
					}
				}
			}
			rawProgressCallback(eventType, message, data)
		}
		taskCtxLoop := mcp.WithMCPConversationID(taskCtx, conversationID)
		taskCtxLoop = mcp.WithToolRunRegistry(taskCtxLoop, h.tasks)
		taskCtxLoop = mcp.WithEinoExecuteRunRegistry(taskCtxLoop, h.tasks)
		taskCtxLoop = multiagent.WithAgentRuntimeCancelRegistrar(taskCtxLoop, func(cancel func(error) bool) func() {
			return h.tasks.BindAgentRuntimeCancel(conversationID, cancel)
		})
		taskCtxLoop = multiagent.WithAgentTurnLoopInterruptRegistrar(taskCtxLoop, func(push func(string) bool) func() {
			return h.tasks.BindAgentTurnLoopInterrupt(conversationID, push)
		})
		taskCtxLoop = multiagent.WithHITLToolInterceptor(taskCtxLoop, func(ctx context.Context, toolName, arguments string) (string, error) {
			return h.interceptHITLForEinoTool(ctx, cancelWithCause, conversationID, assistantMessageID, sendEvent, toolName, arguments)
		})

		result, runErr = multiagent.RunDeepAgent(
			taskCtxLoop,
			runCfg,
			&runCfg.MultiAgent,
			h.agent,
			h.db,
			h.logger,
			conversationID,
			h.conversationProjectID(conversationID),
			curFinalMessage,
			curHistory,
			roleTools,
			progressCallback,
			h.agentsMarkdownDir,
			orch,
			chatReasoningToClientIntent(req.Reasoning),
			h.agentSessionContextBlock(conversationID),
		)

		if result != nil && len(result.MCPExecutionIDs) > 0 {
			cumulativeMCPExecutionIDs = mergeMCPExecutionIDLists(cumulativeMCPExecutionIDs, result.MCPExecutionIDs)
		}

		if runErr == nil {
			mw := &h.config.MultiAgent.EinoMiddleware
			if h.tryContinueOnEinoEmptyResponse(taskCtx, mw, conversationID, result, &emptyResponseContinueAttempt, &curHistory, &curFinalMessage, progressCallback) {
				mainIterationOffset += segmentMainIterationMax
				timeoutCancel()
				baseCtx, cancelWithCause, taskCtx, timeoutCancel = h.rebindEinoRunningTask(taskCtx, conversationID, timeoutCancel)
				continue
			}
			decision = h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, result, cumulativeMCPExecutionIDs, requestRequiresExecutionEvidence(&req))
			if cancelled := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, conversationID, decision, progressCallback); len(cancelled) > 0 {
				autoCancelledPendingExecutionIDs = mergeMCPExecutionIDLists(autoCancelledPendingExecutionIDs, cancelled)
				decision = h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, result, cumulativeMCPExecutionIDs, requestRequiresExecutionEvidence(&req))
			}
			if h.tryAutoContinueAfterFinalization(taskCtx, conversationID, result, decision, &finalizationAutoContinueAttempt, &curHistory, &curFinalMessage, progressCallback) {
				mainIterationOffset += segmentMainIterationMax
				timeoutCancel()
				baseCtx, cancelWithCause, taskCtx, timeoutCancel = h.rebindEinoRunningTask(taskCtx, conversationID, timeoutCancel)
				continue
			}
			timeoutCancel()
			break
		}

		cause := context.Cause(baseCtx)
		if cause == nil {
			switch {
			case errors.Is(runErr, multiagent.ErrInterruptContinue):
				cause = multiagent.ErrInterruptContinue
			case errors.Is(runErr, ErrTaskCancelled):
				cause = ErrTaskCancelled
			}
		}
		if errors.Is(cause, multiagent.ErrInterruptContinue) {
			if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
				h.persistEinoAgentTraceForResume(conversationID, result)
			}
			note := h.tasks.TakeInterruptContinueNote(conversationID)
			icSummary := interruptContinueTimelineSummary(note)
			progressCallback("user_interrupt_continue", icSummary, map[string]interface{}{
				"conversationId": conversationID,
				"rawReason":      strings.TrimSpace(note),
				"emptyReason":    strings.TrimSpace(note) == "",
				"kind":           "no_active_mcp_tool",
			})
			inject := formatInterruptContinueUserMessage(note)
			// 不写入 messages 表为 user 气泡：避免主对话流出现大段模板；说明已由 user_interrupt_continue 记入助手 process_details（迭代详情）。
			if hist, err := h.loadHistoryFromAgentTrace(conversationID); err == nil && len(hist) > 0 {
				curHistory = hist
			}
			curFinalMessage = inject
			sendEvent("progress", "已合并用户补充与最新轨迹，正在继续推理…", map[string]interface{}{
				"conversationId": conversationID,
				"source":         "interrupt_continue",
			})
			mainIterationOffset += segmentMainIterationMax
			timeoutCancel()
			baseCtx, cancelWithCause = context.WithCancelCause(detachedAgentContext(baseCtx))
			h.tasks.BindTaskCancel(conversationID, cancelWithCause)
			taskCtx, timeoutCancel = context.WithTimeout(baseCtx, 600*time.Minute)
			h.tasks.UpdateTaskStatus(conversationID, "running")
			continue
		}

		if shouldPersistEinoAgentTraceAfterRunError(baseCtx) {
			h.persistEinoAgentTraceForResume(conversationID, result)
		}
		if errors.Is(cause, ErrTaskCancelled) {
			taskStatus = "cancelled"
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)
			cancelMsg := "任务已被用户取消，后续操作已停止。"
			if assistantMessageID != "" {
				if result != nil {
					if err := h.mergeAssistantMessagePartialOnCancel(assistantMessageID, result.Response); err != nil {
						h.logger.Warn("合并取消前的部分回复失败", zap.Error(err))
					}
				}
				if err := h.appendAssistantMessageNotice(assistantMessageID, cancelMsg); err != nil {
					h.logger.Warn("更新取消后的助手消息失败", zap.Error(err))
				}
				_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil)
			}
			sendEvent("cancelled", cancelMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
			})
			sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
			timeoutCancel()
			return
		}

		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(context.Cause(taskCtx), context.DeadlineExceeded) {
			taskStatus = "timeout"
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)
			timeoutMsg := "任务执行超时，已自动终止。"
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
			timeoutCancel()
			return
		}

		h.logger.Error("Eino DeepAgent 执行失败", zap.Error(runErr))
		taskStatus = "failed"
		h.tasks.UpdateTaskStatus(conversationID, taskStatus)
		clientErr := multiagent.EinoClientRunErrorMessage(runErr)
		errMsg := "执行失败: " + clientErr
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ?, updated_at = ? WHERE id = ?", errMsg, time.Now(), assistantMessageID)
			_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errMsg, nil)
		}
		errData := multiagent.EinoClientRunErrorFields(runErr)
		errData["conversationId"] = conversationID
		errData["messageId"] = assistantMessageID
		errData["error"] = errMsg
		sendEvent("error", errMsg, errData)
		sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
		timeoutCancel()
		return
	}

	timeoutCancel()

	if decision.CompletionReason == "" {
		decision = h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, result, cumulativeMCPExecutionIDs, requestRequiresExecutionEvidence(&req))
		if cancelled := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, conversationID, decision, nil); len(cancelled) > 0 {
			autoCancelledPendingExecutionIDs = mergeMCPExecutionIDLists(autoCancelledPendingExecutionIDs, cancelled)
			decision = h.decideAgentRunForDeliveryWithPolicy(conversationID, assistantMessageID, agentMode, result, cumulativeMCPExecutionIDs, requestRequiresExecutionEvidence(&req))
		}
	}
	h.persistFinalizationDecision(conversationID, assistantMessageID, agentMode, cumulativeMCPExecutionIDs, multiagent.AggregatedReasoningFromTraceJSON(result.LastAgentTraceInput), decision)

	if result.LastAgentTraceInput != "" || result.LastAgentTraceOutput != "" {
		if err := h.db.SaveAgentTrace(conversationID, result.LastAgentTraceInput, result.LastAgentTraceOutput); err != nil {
			h.logger.Warn("保存代理轨迹失败", zap.Error(err))
		}
	}

	responseText := decision.FinalText
	if !decision.Finalizable {
		responseText = finalizationBlockedMessage(decision)
		sendEvent("finalization_check", responseText, decision)
		taskStatus = decision.Status
		h.tasks.UpdateTaskStatus(conversationID, taskStatus)
	}
	sendEvent("response", responseText, finalizationResponsePayload(decision, map[string]interface{}{
		"mcpExecutionIds":                  cumulativeMCPExecutionIDs,
		"conversationId":                   conversationID,
		"messageId":                        assistantMessageID,
		"agentMode":                        agentMode,
		"autoCancelledPendingExecutionIds": autoCancelledPendingExecutionIDs,
	}))
	sendEvent("done", "", map[string]interface{}{"conversationId": conversationID})
}

// MultiAgentLoop Eino DeepAgent 非流式对话（需 multi_agent.enabled）。
func (h *AgentHandler) MultiAgentLoop(c *gin.Context) {
	if h.config == nil || !h.config.MultiAgent.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "多代理未启用，请在 config.yaml 中设置 multi_agent.enabled: true"})
		return
	}

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("收到 Eino DeepAgent 非流式请求", zap.String("conversationId", req.ConversationID))

	prep, err := h.prepareMultiAgentSession(&req, c, "multi_agent")
	if err != nil {
		status, msg := multiAgentHTTPErrorStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	h.activateHITLForConversation(prep.ConversationID, req.Hitl)
	if h.hitlManager != nil {
		defer h.hitlManager.DeactivateConversation(prep.ConversationID)
	}
	if h.runRoleWorkflowJSONIfBound(c, &req, prep) {
		return
	}

	baseCtx, cancelWithCause := context.WithCancelCause(c.Request.Context())
	defer cancelWithCause(nil)
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)
	defer timeoutCancel()
	jsonTask, startErr := h.tasks.StartTask(prep.ConversationID, req.Message, cancelWithCause)
	if startErr != nil {
		c.JSON(http.StatusConflict, gin.H{"error": startErr.Error()})
		return
	}
	taskCtx = h.tasks.BindProcessScope(taskCtx, prep.ConversationID, jsonTask.RunID)
	taskCtx = mcp.WithMCPConversationID(taskCtx, prep.ConversationID)
	taskCtx = mcp.WithToolRunRegistry(taskCtx, h.tasks)
	taskCtx = mcp.WithEinoExecuteRunRegistry(taskCtx, h.tasks)
	jsonTaskStatus := "failed"
	defer func() { _ = h.tasks.FinishTaskRun(prep.ConversationID, jsonTask.RunID, jsonTaskStatus) }()
	respond := h.taskFinishingJSONResponder(c, prep.ConversationID, jsonTask.RunID, func() string { return jsonTaskStatus })

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
		decision = h.decideAgentRunForDeliveryWithPolicy(prep.ConversationID, prep.AssistantMessageID, agentMode, result, result.MCPExecutionIDs, requestRequiresExecutionEvidence(&req))
		if cancelled := h.cleanupPendingToolExecutionsAfterIteration(taskCtx, prep.ConversationID, decision, progressCallback); len(cancelled) > 0 {
			autoCancelledPendingExecutionIDs = mergeMCPExecutionIDLists(autoCancelledPendingExecutionIDs, cancelled)
			decision = h.decideAgentRunForDeliveryWithPolicy(prep.ConversationID, prep.AssistantMessageID, agentMode, result, result.MCPExecutionIDs, requestRequiresExecutionEvidence(&req))
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

	jsonTaskStatus = decision.Status
	if jsonTaskStatus == "" {
		jsonTaskStatus = "completed"
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

// persistEinoAgentTraceForResume 在 Eino 运行异常结束时写入代理轨迹（库列 last_react_*），供下一请求 loadHistoryFromAgentTrace 软续跑。
func (h *AgentHandler) persistEinoAgentTraceForResume(conversationID string, result *multiagent.RunResult) {
	if h == nil || result == nil {
		return
	}
	if result.LastAgentTraceInput == "" && result.LastAgentTraceOutput == "" {
		return
	}
	if err := h.db.SaveAgentTrace(conversationID, result.LastAgentTraceInput, result.LastAgentTraceOutput); err != nil {
		h.logger.Warn("保存 Eino 续跑上下文失败", zap.String("conversationId", conversationID), zap.Error(err))
	}
}

// mergeMCPExecutionIDLists 去重合并多段 Run 的 MCP execution id（顺序：先 dst 后 more）。
func mergeMCPExecutionIDLists(dst []string, more []string) []string {
	seen := make(map[string]struct{}, len(dst)+len(more))
	out := make([]string, 0, len(dst)+len(more))
	add := func(ids []string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	add(dst)
	add(more)
	return out
}

// interruptContinueTimelineSummary 时间线 / process_details 中展示的简短正文（完整模板已写入另一条用户消息）。
func interruptContinueTimelineSummary(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "用户选择「中断并继续」，未填写说明；已按默认渗透补充模板合并上下文并续跑。"
	}
	return "用户中断说明（原文）：\n\n" + note
}

// formatInterruptContinueUserMessage 将「中断并继续」弹窗中的说明格式化为新一轮 user 消息（渗透场景下强调路径补充与端口复扫）。
func formatInterruptContinueUserMessage(note string) string {
	var b strings.Builder
	b.WriteString("【用户补充 / 中断后继续】\n")
	if s := strings.TrimSpace(note); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	b.WriteString("【请在本轮落实】\n")
	b.WriteString("- 将用户提供的接口路径、参数、业务变化纳入后续测试与推理。\n")
	b.WriteString("- 若资产或目标信息有更新，请对目标重新执行端口/服务探测，再基于新结果规划下一步。\n")
	b.WriteString("- 在已有轨迹基础上推进，避免无意义重复已完成的步骤。\n")
	return strings.TrimSpace(b.String())
}

func multiAgentHTTPErrorStatus(err error) (int, string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "无权访问"):
		return http.StatusForbidden, msg
	case strings.Contains(msg, "对话不存在"):
		return http.StatusNotFound, msg
	case strings.Contains(msg, "未找到该 WebShell"):
		return http.StatusBadRequest, msg
	case strings.Contains(msg, "附件最多"):
		return http.StatusBadRequest, msg
	case strings.Contains(msg, "保存用户消息失败"), strings.Contains(msg, "创建对话失败"):
		return http.StatusInternalServerError, msg
	case strings.Contains(msg, "保存上传文件失败"):
		return http.StatusInternalServerError, msg
	default:
		return http.StatusBadRequest, msg
	}
}
