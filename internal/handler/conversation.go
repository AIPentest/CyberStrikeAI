package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ConversationTaskStopper cancels in-flight agent work when a conversation is removed.
type ConversationTaskStopper interface {
	CancelRunningTaskForConversation(conversationID string)
}

// ConversationTaskStateProvider reports whether the in-memory agent task for
// a conversation is still genuinely running. Plan files may survive a service
// restart or cancellation, so their status alone is not authoritative.
type ConversationTaskStateProvider interface {
	ConversationTaskRuntimeState(conversationID string) (running bool, startedAt time.Time)
}

// ConversationHandler 对话处理器
type ConversationHandler struct {
	db          *database.DB
	logger      *zap.Logger
	audit       *audit.Service
	taskStopper ConversationTaskStopper
	taskState   ConversationTaskStateProvider
}

// SetAudit wires platform audit logging.
func (h *ConversationHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// SetTaskStopper wires cancellation of in-flight agent tasks on conversation delete.
func (h *ConversationHandler) SetTaskStopper(stopper ConversationTaskStopper) {
	h.taskStopper = stopper
}

// SetTaskStateProvider wires the live agent task registry used by supplemental
// conversation UI such as the agent-maintained plan list.
func (h *ConversationHandler) SetTaskStateProvider(provider ConversationTaskStateProvider) {
	h.taskState = provider
}

// NewConversationHandler 创建新的对话处理器
func NewConversationHandler(db *database.DB, logger *zap.Logger) *ConversationHandler {
	return &ConversationHandler{
		db:     db,
		logger: logger,
	}
}

// CreateConversationRequest 创建对话请求
type CreateConversationRequest struct {
	Title     string `json:"title"`
	ProjectID string `json:"projectId,omitempty"`
}

// SetConversationProjectRequest 设置对话所属项目
type SetConversationProjectRequest struct {
	ProjectID string `json:"projectId"` // 空字符串表示解除绑定
}

// CreateConversation 创建新对话
func (h *ConversationHandler) CreateConversation(c *gin.Context) {
	var req CreateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	title := req.Title
	if title == "" {
		title = "新对话"
	}

	meta := audit.ConversationCreateMetaFromGin(c, "api")
	meta.ProjectID = strings.TrimSpace(req.ProjectID)
	if !h.conversationProjectAllowed(c, meta.ProjectID) {
		respondError(c, http.StatusForbidden, "无权访问目标项目", "no permission to access the target project")
		return
	}
	conv, err := h.db.CreateConversation(title, meta)
	if err != nil {
		h.logger.Error("创建对话失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if session, ok := security.CurrentSession(c); ok {
		_ = h.db.SetResourceOwner("conversation", conv.ID, session.UserID)
		_ = h.db.AssignResourceToUser(session.UserID, "conversation", conv.ID)
		if conv.ProjectID != "" {
			_ = h.db.AssignResourceToUser(session.UserID, "project", conv.ProjectID)
		}
	}

	c.JSON(http.StatusOK, conv)
}

// SetConversationProject 设置或清除对话绑定的项目
func (h *ConversationHandler) SetConversationProject(c *gin.Context) {
	id := c.Param("id")
	var req SetConversationProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.db.GetConversation(id); err != nil {
		respondError(c, http.StatusNotFound, "对话不存在", "conversation not found")
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if !h.conversationProjectAllowed(c, projectID) {
		respondError(c, http.StatusForbidden, "无权访问目标项目", "no permission to access the target project")
		return
	}
	if err := h.db.SetConversationProjectID(id, projectID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "projectId": projectID})
}

func (h *ConversationHandler) conversationProjectAllowed(c *gin.Context, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return true
	}
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	return h.db.UserCanAccessResource(session.UserID, session.Scope, "project", projectID)
}

// ListConversations 列出对话
func (h *ConversationHandler) ListConversations(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	search := c.Query("search") // 获取搜索参数
	projectID := strings.TrimSpace(c.Query("project_id"))

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	sortBy := strings.TrimSpace(c.Query("sort_by"))
	session, _ := security.CurrentSession(c)

	var conversations []*database.Conversation
	var total int
	var err error
	conversations, err = h.db.ListConversationsForAccess(limit, offset, search, sortBy, projectID, session.UserID, session.Scope)
	if err == nil {
		total, err = h.db.CountConversationsForAccess(search, projectID, session.UserID, session.Scope)
	}
	if err != nil {
		h.logger.Error("获取对话列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if conversations == nil {
		conversations = []*database.Conversation{}
	}
	c.JSON(http.StatusOK, gin.H{
		"conversations": conversations,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

// UpdateConversationPinnedRequest 更新对话置顶状态请求
type UpdateConversationPinnedRequest struct {
	Pinned bool `json:"pinned"`
}

// UpdateConversationPinned 更新对话置顶状态
func (h *ConversationHandler) UpdateConversationPinned(c *gin.Context) {
	conversationID := c.Param("id")
	session, ok := security.CurrentSession(c)
	if !ok || !h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", conversationID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}

	var req UpdateConversationPinnedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.db.UpdateConversationPinned(conversationID, req.Pinned); err != nil {
		h.logger.Error("更新对话置顶状态失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "更新成功", "updated successfully")})
}

// GetConversation 获取对话
func (h *ConversationHandler) GetConversation(c *gin.Context) {
	id := c.Param("id")

	// 默认轻量加载，只有用户需要展开详情时再按需拉取
	// include_process_details=1/true 时返回全量 processDetails（兼容旧行为）
	includeStr := c.DefaultQuery("include_process_details", "0")
	include := includeStr == "1" || includeStr == "true" || includeStr == "yes"

	var (
		conv *database.Conversation
		err  error
	)
	if include {
		conv, err = h.db.GetConversation(id)
	} else {
		conv, err = h.db.GetConversationLite(id)
	}
	if err != nil {
		h.logger.Error("获取对话失败", zap.Error(err))
		respondError(c, http.StatusNotFound, "对话不存在", "conversation not found")
		return
	}

	conv.Retest, err = h.db.GetVulnerabilityRetest(id)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "读取复测分支失败", "failed to read retest branch")
		return
	}
	c.JSON(http.StatusOK, conv)
}

// GetConversationPlanTasks returns the task list maintained by the agent's
// TaskCreate/TaskUpdate tools for this conversation.
func (h *ConversationHandler) GetConversationPlanTasks(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	session, ok := security.CurrentSession(c)
	if !ok || !h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", id) {
		respondError(c, http.StatusForbidden, "无权访问该对话", "no permission to access the conversation")
		return
	}
	if _, err := h.db.GetConversationLite(id); err != nil {
		respondError(c, http.StatusNotFound, "对话不存在", "conversation not found")
		return
	}
	running := false
	startedAt := time.Time{}
	if h.taskState != nil {
		running, startedAt = h.taskState.ConversationTaskRuntimeState(id)
	}
	if !running {
		c.JSON(http.StatusOK, gin.H{
			"tasks": []database.ConversationPlanTask{}, "total": 0,
			"completed": 0, "activeStep": 0, "running": false,
		})
		return
	}
	tasks, err := h.db.ListConversationPlanTasksSince(id, startedAt)
	if err != nil {
		h.logger.Error("获取对话任务列表失败", zap.String("conversationId", id), zap.Error(err))
		respondError(c, http.StatusInternalServerError, "获取任务列表失败", "failed to get task list")
		return
	}

	completed := 0
	activeStep := 0
	for i, task := range tasks {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "completed" {
			completed++
		}
		if activeStep == 0 && status == "in_progress" {
			activeStep = i + 1
		}
	}
	if activeStep == 0 {
		for i, task := range tasks {
			if strings.ToLower(strings.TrimSpace(task.Status)) != "completed" {
				activeStep = i + 1
				break
			}
		}
	}
	if activeStep == 0 && len(tasks) > 0 {
		activeStep = len(tasks)
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks":      tasks,
		"total":      len(tasks),
		"completed":  completed,
		"activeStep": activeStep,
		"running":    true,
	})
}

const (
	defaultProcessDetailsPageLimit = 50
	maxProcessDetailsPageLimit     = 500
)

// GetMessageProcessDetails 获取指定消息的过程详情（按需加载）
// 查询参数：
//   - summary=1：仅返回摘要（total / iterationCount / maxIteration）
//   - limit + offset：分页返回 processDetails（未指定 limit 时默认 50 条）
//   - anchorId：返回包含该过程详情锚点的一页，适合从工具按钮精准定位
//   - full=1：显式返回全量 processDetails（用于导出/兼容旧集成，不建议 UI 展开时使用）
func (h *ConversationHandler) GetMessageProcessDetails(c *gin.Context) {
	messageID := c.Param("id")
	if messageID == "" {
		respondError(c, http.StatusBadRequest, "message id 必填", "message id required")
		return
	}

	summaryStr := strings.TrimSpace(c.Query("summary"))
	if summaryStr == "1" || strings.EqualFold(summaryStr, "true") || strings.EqualFold(summaryStr, "yes") {
		summary, err := h.db.GetProcessDetailsSummary(messageID)
		if err != nil {
			h.logger.Error("获取过程详情摘要失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"summary": summary})
		return
	}

	fullStr := strings.TrimSpace(c.Query("full"))
	if fullStr == "1" || strings.EqualFold(fullStr, "true") || strings.EqualFold(fullStr, "yes") {
		details, err := h.db.GetProcessDetails(messageID)
		if err != nil {
			h.logger.Error("获取过程详情失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		details = database.DedupeConsecutiveProcessDetails(details)
		out := processDetailsToJSON(h.logger, h.db, details, true)
		c.JSON(http.StatusOK, gin.H{
			"processDetails": out,
			"total":          len(out),
			"offset":         0,
			"limit":          len(out),
			"hasMore":        false,
		})
		return
	}

	limitStr := strings.TrimSpace(c.Query("limit"))
	limit := defaultProcessDetailsPageLimit
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit <= 0 {
			respondError(c, http.StatusBadRequest, "limit 无效", "invalid limit")
			return
		}
		limit = parsedLimit
	}
	if limit > maxProcessDetailsPageLimit {
		limit = maxProcessDetailsPageLimit
	}
	offset, _ := strconv.Atoi(strings.TrimSpace(c.Query("offset")))
	if offset < 0 {
		offset = 0
	}
	anchorID := strings.TrimSpace(c.Query("anchorId"))
	if anchorID != "" {
		anchorOffset, err := h.db.GetProcessDetailOffset(messageID, anchorID)
		if err != nil {
			h.logger.Warn("获取过程详情锚点位置失败", zap.Error(err), zap.String("messageID", messageID), zap.String("anchorID", anchorID))
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		offset = anchorOffset - limit/3
		if offset < 0 {
			offset = 0
		}
	}

	details, total, err := h.db.GetProcessDetailsPage(messageID, limit, offset)
	if err != nil {
		h.logger.Error("分页获取过程详情失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	details = database.DedupeConsecutiveProcessDetails(details)
	out := processDetailsToJSON(h.logger, h.db, details, false)
	// A page may end between tool_call and tool_result. Return the full-history
	// execution summary so the UI can render terminal status without pretending
	// that an unloaded result is still running.
	summary, summaryErr := h.db.GetProcessDetailsSummary(messageID)
	if summaryErr != nil {
		h.logger.Warn("获取分页工具执行状态失败", zap.Error(summaryErr), zap.String("messageID", messageID))
	}
	var toolExecutions []database.ProcessDetailsToolExecution
	if summary != nil {
		toolExecutions = summary.ToolExecutions
	}
	c.JSON(http.StatusOK, gin.H{
		"processDetails": out,
		"toolExecutions": toolExecutions,
		"total":          total,
		"offset":         offset,
		"limit":          limit,
		"hasMore":        offset+len(out) < total,
	})
}

// GetProcessDetail 获取单条完整过程详情。列表接口默认不给工具 payload，用户点开单条工具时再拉这里。
func (h *ConversationHandler) GetProcessDetail(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		respondError(c, http.StatusBadRequest, "process detail id 必填", "process detail id required")
		return
	}
	detail, err := h.db.GetProcessDetailByID(id)
	if err != nil {
		h.logger.Error("获取过程详情失败", zap.Error(err))
		respondError(c, http.StatusNotFound, "过程详情不存在", "process detail not found")
		return
	}
	out := processDetailsToJSON(h.logger, h.db, []database.ProcessDetail{*detail}, true)
	if len(out) == 0 {
		respondError(c, http.StatusNotFound, "过程详情不存在", "process detail not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"processDetail": out[0]})
}

func processDetailsToJSON(logger *zap.Logger, db *database.DB, details []database.ProcessDetail, includeToolPayload bool) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(details))
	for _, d := range details {
		var data interface{}
		if d.Data != "" {
			if err := json.Unmarshal([]byte(d.Data), &data); err != nil {
				logger.Warn("解析过程详情数据失败", zap.Error(err))
			}
		}
		if m, ok := data.(map[string]interface{}); ok {
			enrichEmptyToolCallArgumentsFromExecution(logger, db, d, m)
		}
		if !includeToolPayload {
			data = summarizeProcessDetailData(d.EventType, data)
		}
		out = append(out, map[string]interface{}{
			"id":             d.ID,
			"messageId":      d.MessageID,
			"conversationId": d.ConversationID,
			"eventType":      d.EventType,
			"message":        d.Message,
			"data":           data,
			"createdAt":      d.CreatedAt,
		})
	}
	return out
}

func enrichEmptyToolCallArgumentsFromExecution(logger *zap.Logger, db *database.DB, detail database.ProcessDetail, data map[string]interface{}) {
	if db == nil || detail.EventType != "tool_call" || !toolCallArgumentsEmpty(data) {
		return
	}
	toolName := strings.TrimSpace(fmt.Sprint(data["toolName"]))
	if toolName == "" || detail.ConversationID == "" || detail.CreatedAt.IsZero() {
		return
	}
	execID, args, err := db.FindNearestToolExecutionArguments(detail.ConversationID, toolName, detail.CreatedAt, 5*time.Second)
	if err != nil {
		if logger != nil {
			logger.Debug("未能从工具执行记录补全过程详情参数",
				zap.Error(err),
				zap.String("processDetailId", detail.ID),
				zap.String("toolName", toolName))
		}
		return
	}
	if len(args) == 0 {
		return
	}
	data["argumentsObj"] = args
	if b, err := json.Marshal(args); err == nil {
		data["arguments"] = string(b)
	}
	if strings.TrimSpace(execID) != "" {
		data["executionId"] = strings.TrimSpace(execID)
	}
}

func toolCallArgumentsEmpty(data map[string]interface{}) bool {
	if data == nil {
		return true
	}
	if args, ok := data["argumentsObj"].(map[string]interface{}); ok && len(args) > 0 {
		return false
	}
	if raw, ok := data["arguments"]; ok {
		s := strings.TrimSpace(fmt.Sprint(raw))
		return s == "" || s == "{}" || s == "null"
	}
	return true
}

func summarizeProcessDetailData(eventType string, data interface{}) interface{} {
	m, ok := data.(map[string]interface{})
	if !ok || (eventType != "tool_call" && eventType != "tool_result") {
		return data
	}
	allow := map[string]bool{
		"toolName": true, "toolCallId": true, "index": true, "total": true,
		"arguments": true, "argumentsObj": true,
		"success": true, "isError": true, "executionId": true,
		"einoAgent": true, "einoRole": true, "einoScope": true, "orchestration": true,
		"agentFacing": true,
		"status":      true, "modelFacingIsError": true, "resultPreview": true,
	}
	out := make(map[string]interface{}, len(allow)+1)
	for k, v := range m {
		if allow[k] {
			out[k] = v
		}
	}
	out["_payloadDeferred"] = true
	return out
}

// UpdateConversationRequest 更新对话请求
type UpdateConversationRequest struct {
	Title string `json:"title"`
}

// UpdateConversation 更新对话
func (h *ConversationHandler) UpdateConversation(c *gin.Context) {
	id := c.Param("id")

	var req UpdateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Title == "" {
		respondError(c, http.StatusBadRequest, "标题不能为空", "title must not be empty")
		return
	}

	if err := h.db.UpdateConversationTitle(id, req.Title); err != nil {
		h.logger.Error("更新对话失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 返回更新后的对话
	conv, err := h.db.GetConversation(id)
	if err != nil {
		h.logger.Error("获取更新后的对话失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, conv)
}

// DeleteConversation 删除对话
func (h *ConversationHandler) DeleteConversation(c *gin.Context) {
	id := c.Param("id")

	if h.taskStopper != nil {
		h.taskStopper.CancelRunningTaskForConversation(id)
	}

	if err := h.db.DeleteConversation(id); err != nil {
		h.logger.Error("删除对话失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:     "conversation",
			Action:       "delete",
			Result:       "success",
			ResourceType: "conversation",
			ResourceID:   id,
			Message:      "删除对话",
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "删除成功", "deleted successfully")})
}

// DeleteTurnRequest 删除一轮对话（POST /api/conversations/:id/delete-turn）
type DeleteTurnRequest struct {
	MessageID string `json:"messageId"`
}

// DeleteConversationTurn 删除锚点消息所在轮次（从该轮 user 到下一轮 user 之前），并清空 last_react_*。
func (h *ConversationHandler) DeleteConversationTurn(c *gin.Context) {
	conversationID := c.Param("id")
	if conversationID == "" {
		respondError(c, http.StatusBadRequest, "conversation id 必填", "conversation id required")
		return
	}

	var req DeleteTurnRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.MessageID == "" {
		respondError(c, http.StatusBadRequest, "messageId 必填", "messageId required")
		return
	}

	if _, err := h.db.GetConversation(conversationID); err != nil {
		respondError(c, http.StatusNotFound, "对话不存在", "conversation not found")
		return
	}

	deletedIDs, err := h.db.DeleteConversationTurn(conversationID, req.MessageID)
	if err != nil {
		h.logger.Warn("删除对话轮次失败",
			zap.String("conversationId", conversationID),
			zap.String("messageId", req.MessageID),
			zap.Error(err),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.audit != nil {
		h.audit.RecordOK(c, "conversation", "delete_turn", "删除对话轮次", "conversation", conversationID, map[string]interface{}{
			"message_id": req.MessageID,
			"deleted":    len(deletedIDs),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"deletedMessageIds": deletedIDs,
		"message":           "ok",
	})
}
