package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *AgentHandler) SubmitSingleAgentRun(c *gin.Context) { h.submitAgentRun(c, false) }
func (h *AgentHandler) SubmitMultiAgentRun(c *gin.Context)  { h.submitAgentRun(c, true) }
func (h *AgentHandler) submitAgentRun(c *gin.Context, multi bool) {
	h.submitAgentRunWithExecutor(c, multi, h.executeAcceptedRun)
}

type acceptedRunExecutor func(context.Context, context.Context, context.CancelCauseFunc, context.CancelFunc, ChatRequest, *multiAgentPrepared, *database.AgentRun, bool, bool)

func (h *AgentHandler) submitAgentRunWithExecutor(c *gin.Context, multi bool, execute acceptedRunExecutor) {
	session, ok := security.CurrentSession(c)
	if !ok || !session.Permissions["chat:write"] || !session.Permissions["agent:execute"] {
		respondError(c, http.StatusForbidden, "无权访问", "forbidden")
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		respondError(c, http.StatusBadRequest, "Idempotency-Key 需为 1–200 字字节", "Idempotency-Key must contain 1–200 bytes")
		return
	}
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	raw, _ := json.Marshal(struct {
		Request ChatRequest
		Multi   bool
	}{req, multi})
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	// Serialize acceptance, not execution. A retry cannot create messages before its receipt is checked.
	h.asyncSubmitMu.Lock()
	defer h.asyncSubmitMu.Unlock()
	old, err := h.db.FindAgentRun(session.UserID, key)
	if err == nil {
		if old.Fingerprint != fingerprint {
			respondError(c, http.StatusConflict, "该 Idempotency-Key 已使用，但与本次请求不一致", "idempotency key reused with different request")
			return
		}
		if !h.agentConversationAllowed(c, old.ConversationID) {
			respondError(c, http.StatusForbidden, "无权访问", "forbidden")
			return
		}
		h.agentRunReceipt(c, old)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusInternalServerError, "读取运行凭证失败", "cannot read run receipt")
		return
	}
	if h.config == nil || (multi && !h.config.MultiAgent.Enabled) {
		respondError(c, http.StatusBadRequest, "代理模式不可用", "agent mode unavailable")
		return
	}
	if _, _, err := h.configForAIChannel(req.AIChannelID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if id := strings.TrimSpace(req.ConversationID); id != "" {
		if !h.agentConversationAllowed(c, id) {
			respondError(c, http.StatusForbidden, "无权访问", "forbidden")
			return
		}
		if h.tasks.GetTask(id) != nil {
			respondError(c, http.StatusConflict, "该对话已有任务正在执行", "conversation already running")
			return
		}
	}
	source := "eino_agent_async"
	if multi {
		source = "multi_agent_async"
	}
	prep, err := h.prepareMultiAgentSession(&req, c, source)
	if err != nil {
		code, msg := multiAgentHTTPErrorStatus(err)
		c.JSON(code, gin.H{"error": msg})
		return
	}
	base, cancel := context.WithCancelCause(detachedAgentContext(c.Request.Context()))
	ctx, timeoutCancel := context.WithTimeout(base, 600*time.Minute)
	task, err := h.tasks.StartTask(prep.ConversationID, req.Message, cancel)
	if err != nil {
		timeoutCancel()
		cancel(nil)
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	record := &database.AgentRun{RunID: task.RunID, UserID: session.UserID, Key: key, Fingerprint: fingerprint, ConversationID: prep.ConversationID, AssistantMessageID: prep.AssistantMessageID, Status: "running"}
	if err = h.db.CreateAgentRun(record); err != nil {
		timeoutCancel()
		cancel(nil)
		_ = h.tasks.FinishTaskRun(prep.ConversationID, task.RunID, "failed")
		respondError(c, http.StatusInternalServerError, "保存运行凭证失败", "cannot persist run receipt")
		return
	}
	ctx = h.tasks.BindProcessScope(ctx, prep.ConversationID, task.RunID)
	ctx = mcp.WithMCPConversationID(ctx, prep.ConversationID)
	ctx = mcp.WithToolRunRegistry(ctx, h.tasks)
	ctx = mcp.WithEinoExecuteRunRegistry(ctx, h.tasks)
	// c 可在 goroutine 启动后才结束；先取语言偏好，避免读取请求状态。
	wantEnglish := requestWantsEnglish(c)
	h.asyncWorkers.Add(1)
	go func() {
		defer h.asyncWorkers.Done()
		execute(ctx, base, cancel, timeoutCancel, req, prep, record, multi, wantEnglish)
	}()
	h.agentRunReceipt(c, record)
}
func (h *AgentHandler) agentRunReceipt(c *gin.Context, r *database.AgentRun) {
	url := "/api/eino-agent/runs/" + r.RunID
	c.Header("Location", url)
	c.JSON(http.StatusAccepted, gin.H{"runId": r.RunID, "conversationId": r.ConversationID, "assistantMessageId": r.AssistantMessageID, "status": r.Status, "statusUrl": url})
}
func (h *AgentHandler) GetAgentRun(c *gin.Context) {
	r, err := h.db.GetAgentRun(c.Param("runId"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "运行不存在", "run not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "读取运行失败", "cannot read run")
		return
	}
	if !h.agentConversationAllowed(c, r.ConversationID) {
		respondError(c, http.StatusForbidden, "无权访问", "forbidden")
		return
	}
	c.JSON(200, r)
}
func (h *AgentHandler) executeAcceptedRun(ctx, base context.Context, cancel context.CancelCauseFunc, timeoutCancel context.CancelFunc, req ChatRequest, prep *multiAgentPrepared, record *database.AgentRun, multi bool, wantEnglish bool) {
	h.executeAcceptedRunBody(ctx, base, cancel, timeoutCancel, prep, record, wantEnglish, func(status *string, respond func(int, interface{})) {
		h.activateHITLForConversation(prep.ConversationID, req.Hitl)
		if h.hitlManager != nil {
			defer h.hitlManager.DeactivateConversation(prep.ConversationID)
		}
		if role, ok := h.roleForWorkflow(&req); ok {
			h.runWorkflowJSON(ctx, base, cancel, &req, prep, role, status, wantEnglish, respond)
			return
		}
		if multi {
			h.runMultiJSON(ctx, base, cancel, &req, prep, status, wantEnglish, respond)
		} else {
			h.runSingleJSON(ctx, base, cancel, &req, prep, status, wantEnglish, respond)
		}
	})
}

func (h *AgentHandler) executeAcceptedRunBody(ctx, base context.Context, cancel context.CancelCauseFunc, timeoutCancel context.CancelFunc, prep *multiAgentPrepared, record *database.AgentRun, wantEnglish bool, body func(*string, func(int, interface{}))) {
	status := "failed"
	var result interface{} = gin.H{"error": i18nText(wantEnglish, "执行结束但没有结果", "execution ended without a result")}
	defer func() {
		if p := recover(); p != nil {
			status = "failed"
			result = gin.H{"error": i18nText(wantEnglish, "内部执行失败", "internal execution failure")}
			h.logger.Error("async agent panic", zap.Any("panic", p))
		}
		if errors.Is(context.Cause(base), ErrTaskCancelled) {
			status = "cancelled"
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
		}
		if err := h.tasks.FinishTaskRun(prep.ConversationID, record.RunID, status); err != nil {
			status = taskCleanupStatus(err)
			result = gin.H{"error": err.Error()}
		}
		// Cleanup may race an explicit cancellation; use the registry's settled outcome.
		for _, finished := range h.tasks.GetCompletedTasks() {
			if finished.RunID == record.RunID {
				status = finished.Status
				break
			}
		}
		if err := h.db.FinishAgentRun(record.RunID, status, result); err != nil {
			h.logger.Error("persist async run result", zap.String("runId", record.RunID), zap.Error(err))
		}
		timeoutCancel()
		cancel(nil)
	}()
	body(&status, func(code int, payload interface{}) {
		result = payload
		if code >= 400 {
			status = "failed"
		}
	})
}

// lockAgentAdmission covers preparation through registration across all chat transports.
func (h *AgentHandler) lockAgentAdmission() func() {
	h.asyncSubmitMu.Lock()
	var once sync.Once
	return func() { once.Do(h.asyncSubmitMu.Unlock) }
}
