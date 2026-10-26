package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/c2"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// C2Handler 处理 C2 相关的 REST API（manager 可在运行时置 nil 以关闭 C2）
type C2Handler struct {
	mgrPtr atomic.Pointer[c2.Manager]
	logger *zap.Logger
	audit  *audit.Service
}

// SetAudit wires platform audit logging.
func (h *C2Handler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewC2Handler 创建 C2 处理器；manager 可为 nil（功能关闭时）
func NewC2Handler(manager *c2.Manager, logger *zap.Logger) *C2Handler {
	h := &C2Handler{logger: logger}
	if manager != nil {
		h.mgrPtr.Store(manager)
	}
	return h
}

func (h *C2Handler) mgr() *c2.Manager {
	return h.mgrPtr.Load()
}

// SetManager 运行时切换或清空 C2 Manager（与 App 启停同步）
func (h *C2Handler) SetManager(m *c2.Manager) {
	h.mgrPtr.Store(m)
}

// ============================================================================
// 监听器 API
// ============================================================================

// ListListeners 获取监听器列表
func (h *C2Handler) ListListeners(c *gin.Context) {
	listeners, err := h.mgr().DB().ListC2ListenersForAccess(c2AccessFromContext(c), c.Query("project_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 移除敏感字段
	for _, l := range listeners {
		l.EncryptionKey = ""
		l.ImplantToken = ""
	}
	c.JSON(http.StatusOK, gin.H{"listeners": listeners})
}

// CreateListener 创建监听器
func (h *C2Handler) CreateListener(c *gin.Context) {
	var req struct {
		Name         string             `json:"name"`
		ProjectID    string             `json:"project_id,omitempty"`
		Type         string             `json:"type"`
		BindHost     string             `json:"bind_host"`
		BindPort     int                `json:"bind_port"`
		ProfileID    string             `json:"profile_id,omitempty"`
		Remark       string             `json:"remark,omitempty"`
		CallbackHost string             `json:"callback_host,omitempty"`
		Config       *c2.ListenerConfig `json:"config,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	input := c2.CreateListenerInput{
		Name:         req.Name,
		ProjectID:    req.ProjectID,
		Type:         req.Type,
		BindHost:     req.BindHost,
		BindPort:     req.BindPort,
		ProfileID:    req.ProfileID,
		Remark:       req.Remark,
		Config:       req.Config,
		CallbackHost: strings.TrimSpace(req.CallbackHost),
	}
	if !h.canAccessProject(c, input.ProjectID) {
		respondError(c, http.StatusForbidden, "无权访问该项目", "project access denied")
		return
	}

	listener, err := h.mgr().CreateListener(input)
	if err != nil {
		code := http.StatusInternalServerError
		if e, ok := err.(*c2.CommonError); ok {
			code = e.HTTP
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	if session, ok := security.CurrentSession(c); ok {
		listener.OwnerUserID = session.UserID
		_ = h.mgr().DB().SetResourceOwner("c2_listener", listener.ID, session.UserID)
		_ = h.mgr().DB().AssignResourceToUser(session.UserID, "c2_listener", listener.ID)
	}
	implantToken := listener.ImplantToken
	listener.EncryptionKey = ""
	listener.ImplantToken = ""
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "listener_create", "创建 C2 监听器", "c2_listener", listener.ID, map[string]interface{}{
			"name": listener.Name, "bind": listener.BindHost, "port": listener.BindPort,
		})
	}
	c.JSON(http.StatusOK, gin.H{"listener": listener, "implant_token": implantToken})
}

// GetListener 获取单个监听器
func (h *C2Handler) GetListener(c *gin.Context) {
	id := c.Param("id")
	listener, err := h.mgr().DB().GetC2Listener(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if listener == nil {
		respondError(c, http.StatusNotFound, "监听器不存在", "listener not found")
		return
	}
	listener.EncryptionKey = ""
	listener.ImplantToken = ""
	c.JSON(http.StatusOK, gin.H{"listener": listener})
}

// UpdateListener 更新监听器
func (h *C2Handler) UpdateListener(c *gin.Context) {
	id := c.Param("id")
	listener, err := h.mgr().DB().GetC2Listener(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if listener == nil {
		respondError(c, http.StatusNotFound, "监听器不存在", "listener not found")
		return
	}

	var req struct {
		Name         string             `json:"name"`
		ProjectID    string             `json:"project_id"`
		BindHost     string             `json:"bind_host"`
		BindPort     int                `json:"bind_port"`
		ProfileID    string             `json:"profile_id"`
		Remark       string             `json:"remark"`
		CallbackHost *string            `json:"callback_host"`
		Config       *c2.ListenerConfig `json:"config,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusBadRequest, "监听器名称必填", "listener name is required")
		return
	}
	if err := c2.SafeBindPort(req.BindPort); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 若监听器在运行，不能修改关键字段
	if h.mgr().IsListenerRunning(id) {
		if req.BindHost != listener.BindHost || req.BindPort != listener.BindPort {
			respondError(c, http.StatusConflict, "监听器运行中无法修改绑定地址", "cannot modify bind address while listener is running")
			return
		}
	}

	listener.Name = req.Name
	listener.ProjectID = strings.TrimSpace(req.ProjectID)
	listener.BindHost = req.BindHost
	listener.BindPort = req.BindPort
	listener.ProfileID = req.ProfileID
	listener.Remark = req.Remark
	if req.Config != nil {
		cfgJSON, _ := json.Marshal(req.Config)
		listener.ConfigJSON = string(cfgJSON)
	}
	if !h.canAccessProject(c, listener.ProjectID) {
		respondError(c, http.StatusForbidden, "无权访问该项目", "project access denied")
		return
	}
	if req.CallbackHost != nil {
		cfg := &c2.ListenerConfig{}
		raw := strings.TrimSpace(listener.ConfigJSON)
		if raw == "" {
			raw = "{}"
		}
		_ = json.Unmarshal([]byte(raw), cfg)
		cfg.CallbackHost = strings.TrimSpace(*req.CallbackHost)
		cfg.ApplyDefaults()
		cfgJSON, err := json.Marshal(cfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		listener.ConfigJSON = string(cfgJSON)
	}

	if err := h.mgr().DB().UpdateC2Listener(listener); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	listener.EncryptionKey = ""
	listener.ImplantToken = ""
	c.JSON(http.StatusOK, gin.H{"listener": listener})
}

// DeleteListener 删除监听器
func (h *C2Handler) DeleteListener(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr().DeleteListener(id); err != nil {
		code := http.StatusInternalServerError
		if e, ok := err.(*c2.CommonError); ok {
			code = e.HTTP
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "listener_delete", "删除 C2 监听器", "c2_listener", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// StartListener 启动监听器
func (h *C2Handler) StartListener(c *gin.Context) {
	id := c.Param("id")
	listener, err := h.mgr().StartListener(id)
	if err != nil {
		code := http.StatusInternalServerError
		if e, ok := err.(*c2.CommonError); ok {
			code = e.HTTP
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	listener.EncryptionKey = ""
	listener.ImplantToken = ""
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "listener_start", "启动 C2 监听器", "c2_listener", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"listener": listener})
}

// StopListener 停止监听器
func (h *C2Handler) StopListener(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr().StopListener(id); err != nil {
		code := http.StatusInternalServerError
		if e, ok := err.(*c2.CommonError); ok {
			code = e.HTTP
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "listener_stop", "停止 C2 监听器", "c2_listener", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"stopped": true})
}

// ============================================================================
// 会话 API
// ============================================================================

// ListSessions 获取会话列表
func (h *C2Handler) ListSessions(c *gin.Context) {
	filter := database.ListC2SessionsFilter{
		ListenerID: c.Query("listener_id"),
		ProjectID:  c.Query("project_id"),
		Status:     c.Query("status"),
		OS:         c.Query("os"),
		Search:     c.Query("search"),
	}
	if limit := c.Query("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if c.Query("suspicious") == "1" || strings.EqualFold(c.Query("suspicious"), "true") {
		filter.Suspicious = true
	}

	sessions, err := h.mgr().DB().ListC2SessionsForAccess(filter, c2AccessFromContext(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// GetSession 获取单个会话
func (h *C2Handler) GetSession(c *gin.Context) {
	id := c.Param("id")
	session, err := h.mgr().DB().GetC2Session(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if session == nil {
		respondError(c, http.StatusNotFound, "C2 会话不存在", "session not found")
		return
	}

	// 获取最近任务
	tasks, _ := h.mgr().DB().ListC2TasksForAccess(database.ListC2TasksFilter{
		SessionID: id,
		Limit:     20,
	}, c2AccessFromContext(c))

	c.JSON(http.StatusOK, gin.H{
		"session": session,
		"tasks":   tasks,
	})
}

// DeleteSession 删除会话
func (h *C2Handler) DeleteSession(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr().DB().DeleteC2Session(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "session_delete", "删除 C2 会话", "c2_session", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// DeleteSessions 批量删除会话（请求体 JSON: {"ids":["s_xxx",...]}）
func (h *C2Handler) DeleteSessions(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorf(c, http.StatusBadRequest, "无效的 JSON: %s", "invalid json: %s", err.Error())
		return
	}
	if len(req.IDs) == 0 {
		respondError(c, http.StatusBadRequest, "ids 必填", "ids is required")
		return
	}
	n, err := h.mgr().DB().DeleteC2SessionsByIDsForAccess(req.IDs, c2AccessFromContext(c))
	if err != nil {
		if errors.Is(err, database.ErrNoValidC2SessionIDs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "session_delete", "批量删除 C2 会话", "c2_session", "", map[string]interface{}{
			"count": n, "ids": req.IDs,
		})
	}
	c.JSON(http.StatusOK, gin.H{"deleted": n})
}

// SetSessionSleep 设置会话的 sleep/jitter，并下发 sleep 任务到植入体
func (h *C2Handler) SetSessionSleep(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		SleepSeconds  int `json:"sleep_seconds"`
		JitterPercent int `json:"jitter_percent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SleepSeconds < 1 {
		respondError(c, http.StatusBadRequest, "sleep_seconds 必须 >= 1", "sleep_seconds must be >= 1")
		return
	}
	if req.JitterPercent < 0 || req.JitterPercent > 100 {
		respondError(c, http.StatusBadRequest, "jitter_percent 必须 0-100", "jitter_percent must be 0-100")
		return
	}

	task, err := h.mgr().SetSessionSleep(id, req.SleepSeconds, req.JitterPercent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusNotFound, "C2 会话不存在", "session not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := gin.H{
		"updated":        true,
		"sleep_seconds":  req.SleepSeconds,
		"jitter_percent": req.JitterPercent,
	}
	if task != nil {
		out["task_id"] = task.ID
	}
	c.JSON(http.StatusOK, out)
}

// SetSessionNote 更新会话备注（仅服务端元数据，不下发植入体）
func (h *C2Handler) SetSessionNote(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	note := strings.TrimSpace(req.Note)
	if len(note) > 2000 {
		respondError(c, http.StatusBadRequest, "备注过长（最多 2000 个字符）", "note too long (max 2000 characters)")
		return
	}

	session, err := h.mgr().DB().GetC2Session(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if session == nil {
		respondError(c, http.StatusNotFound, "C2 会话不存在", "session not found")
		return
	}

	if err := h.mgr().DB().SetC2SessionNote(id, note); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "session_note", "更新 C2 会话备注", "c2_session", id, map[string]interface{}{
			"note_len": len(note),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"updated": true,
		"note":    note,
	})
}

// ============================================================================
// 任务 API
// ============================================================================

// ListTasks 获取任务列表
func (h *C2Handler) ListTasks(c *gin.Context) {
	filter := database.ListC2TasksFilter{
		SessionID: c.Query("session_id"),
		ProjectID: c.Query("project_id"),
		Status:    c.Query("status"),
		TaskType:  c.Query("task_type"),
	}
	if since := c.Query("since"); since != "" {
		if t, err := database.ParseRFC3339Time(since); err == nil {
			filter.Since = &t
		}
	}

	paginated := false
	page := 1
	pageSize := 10
	if c.Query("page") != "" || c.Query("page_size") != "" {
		paginated = true
		if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
			page = p
		}
		if ps, err := strconv.Atoi(c.DefaultQuery("page_size", "10")); err == nil && ps > 0 {
			pageSize = ps
			if pageSize > 100 {
				pageSize = 100
			}
		}
		filter.Limit = pageSize
		filter.Offset = (page - 1) * pageSize
	} else {
		if limit := c.Query("limit"); limit != "" {
			if n, err := strconv.Atoi(limit); err == nil && n > 0 {
				filter.Limit = n
			}
		}
	}

	access := c2AccessFromContext(c)
	tasks, err := h.mgr().DB().ListC2TasksForAccess(filter, access)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 仪表盘「待审任务」为全局 queued/pending 数量，与列表 session 过滤无关
	pendingN, _ := h.mgr().DB().CountC2TasksQueuedOrPendingForAccess("", filter.ProjectID, access)

	if !paginated {
		c.JSON(http.StatusOK, gin.H{
			"tasks":                tasks,
			"pending_queued_count": pendingN,
		})
		return
	}

	total, err := h.mgr().DB().CountC2TasksForAccess(filter, access)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	statusCounts, err := h.mgr().DB().CountC2TasksByStatusForAccess(filter, access)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tasks":                tasks,
		"total":                total,
		"status_counts":        statusCounts,
		"page":                 page,
		"page_size":            pageSize,
		"pending_queued_count": pendingN,
	})
}

// DeleteTasks 批量删除任务（请求体 JSON: {"ids":["t_xxx",...]}）
func (h *C2Handler) DeleteTasks(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorf(c, http.StatusBadRequest, "无效的 JSON: %s", "invalid json: %s", err.Error())
		return
	}
	if len(req.IDs) == 0 {
		respondError(c, http.StatusBadRequest, "ids 必填", "ids is required")
		return
	}
	n, err := h.mgr().DB().DeleteC2TasksByIDsForAccess(req.IDs, c2AccessFromContext(c))
	if err != nil {
		if errors.Is(err, database.ErrNoValidC2TaskIDs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "task_delete", "批量删除 C2 任务", "c2_task", "", map[string]interface{}{
			"count": n, "ids": req.IDs,
		})
	}
	c.JSON(http.StatusOK, gin.H{"deleted": n})
}

// GetTask 获取单个任务
func (h *C2Handler) GetTask(c *gin.Context) {
	id := c.Param("id")
	task, err := h.mgr().DB().GetC2Task(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		respondError(c, http.StatusNotFound, "任务不存在", "task not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// CreateTask 创建任务
func (h *C2Handler) CreateTask(c *gin.Context) {
	var req struct {
		SessionID      string                 `json:"session_id"`
		TaskType       string                 `json:"task_type"`
		Payload        map[string]interface{} `json:"payload"`
		Source         string                 `json:"source"`
		ConversationID string                 `json:"conversation_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		req.SessionID = strings.TrimSpace(c.Param("id"))
	}
	if !h.c2ResourceAllowed(c, "c2_session", req.SessionID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	if conversationID := strings.TrimSpace(req.ConversationID); conversationID != "" {
		session, ok := security.CurrentSession(c)
		if !ok || !h.mgr().DB().UserCanAccessResource(session.UserID, session.Scope, "conversation", conversationID) {
			respondError(c, http.StatusForbidden, "无权关联目标对话", "no permission to link the target conversation")
			return
		}
		req.ConversationID = conversationID
	}

	input := c2.EnqueueTaskInput{
		SessionID:      req.SessionID,
		TaskType:       c2.TaskType(req.TaskType),
		Payload:        req.Payload,
		Source:         firstNonEmpty(req.Source, "manual"),
		ConversationID: req.ConversationID,
		UserCtx:        c.Request.Context(),
	}

	task, err := h.mgr().EnqueueTask(input)
	if err != nil {
		code := http.StatusInternalServerError
		if e, ok := err.(*c2.CommonError); ok {
			code = e.HTTP
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "task_create", "创建 C2 任务", "c2_task", task.ID, map[string]interface{}{
			"session_id": req.SessionID, "task_type": req.TaskType,
		})
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// CancelTask 取消任务
func (h *C2Handler) CancelTask(c *gin.Context) {
	id := c.Param("id")
	if !h.c2ResourceAllowed(c, "c2_task", id) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	if err := h.mgr().CancelTask(id); err != nil {
		code := http.StatusInternalServerError
		if e, ok := err.(*c2.CommonError); ok {
			code = e.HTTP
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "c2", "task_cancel", "取消 C2 任务", "c2_task", id, nil)
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": true})
}

// WaitTask 等待任务完成
func (h *C2Handler) WaitTask(c *gin.Context) {
	id := c.Param("id")
	if !h.c2ResourceAllowed(c, "c2_task", id) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	timeout := 60 * time.Second
	if t := c.Query("timeout"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := h.mgr().DB().GetC2Task(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			respondError(c, http.StatusNotFound, "任务不存在", "task not found")
			return
		}
		if task.Status == "success" || task.Status == "failed" || task.Status == "cancelled" {
			c.JSON(http.StatusOK, gin.H{"task": task})
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	respondError(c, http.StatusRequestTimeout, "等待任务完成时超时", "timeout waiting for task completion")
}

// ============================================================================
// Payload API
// ============================================================================

// PayloadOneliner 生成单行 payload
func (h *C2Handler) PayloadOneliner(c *gin.Context) {
	var req struct {
		ListenerID string `json:"listener_id"`
		Kind       string `json:"kind"` // bash, python, powershell, curl_beacon
		Host       string `json:"host"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	listener, err := h.mgr().DB().GetC2Listener(req.ListenerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if listener == nil {
		respondError(c, http.StatusNotFound, "监听器不存在", "listener not found")
		return
	}
	if !h.c2ResourceAllowed(c, "c2_listener", req.ListenerID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}

	host := c2.ResolveBeaconDialHost(listener, strings.TrimSpace(req.Host), h.logger, listener.ID)

	kind := c2.OnelinerKind(req.Kind)
	if !c2.IsOnelinerCompatible(listener.Type, kind) {
		compatible := c2.OnelinerKindsForListener(listener.Type)
		names := make([]string, len(compatible))
		for i, k := range compatible {
			names[i] = string(k)
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error":            fmt.Sprintf(i18nText(requestWantsEnglish(c), "监听器类型 %s 不支持 %s 类型的 oneliner，请选择兼容的类型", "listener type %s does not support oneliner of kind %s, choose a compatible type"), listener.Type, req.Kind),
			"compatible_kinds": names,
		})
		return
	}

	scheme := "http"
	if listener.Type == "https_beacon" {
		scheme = "https"
	}
	input := c2.OnelinerInput{
		Kind:         kind,
		Host:         host,
		Port:         listener.BindPort,
		HTTPBaseURL:  fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, strconv.Itoa(listener.BindPort))),
		ImplantToken: listener.ImplantToken,
	}

	oneliner, err := c2.GenerateOneliner(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"oneliner": oneliner,
		"kind":     req.Kind,
		"host":     host,
		"port":     listener.BindPort,
	})
}

// PayloadBuild 构建 beacon 二进制
func (h *C2Handler) PayloadBuild(c *gin.Context) {
	var req struct {
		ListenerID    string `json:"listener_id"`
		OS            string `json:"os"`
		Arch          string `json:"arch"`
		SleepSeconds  int    `json:"sleep_seconds"`
		JitterPercent int    `json:"jitter_percent"`
		Host          string `json:"host"` // 可选：编译进 Beacon 的回连地址，覆盖监听器 bind_host
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	listener, err := h.mgr().DB().GetC2Listener(req.ListenerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if listener == nil {
		respondError(c, http.StatusNotFound, "监听器不存在", "listener not found")
		return
	}
	if !h.c2ResourceAllowed(c, "c2_listener", req.ListenerID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}

	builder := c2.NewPayloadBuilder(h.mgr(), h.logger, "", "")
	input := c2.PayloadBuilderInput{
		ListenerID:    req.ListenerID,
		OS:            req.OS,
		Arch:          req.Arch,
		SleepSeconds:  req.SleepSeconds,
		JitterPercent: req.JitterPercent,
		Host:          strings.TrimSpace(req.Host),
	}

	result, err := builder.BuildBeacon(input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if session, ok := security.CurrentSession(c); ok {
		_ = h.mgr().DB().RecordC2PayloadArtifact(filepath.Base(result.OutputPath), result.PayloadID, result.ListenerID, session.UserID)
	}

	c.JSON(http.StatusOK, gin.H{
		"payload": result,
	})
}

// PayloadDownload 下载 payload
func (h *C2Handler) PayloadDownload(c *gin.Context) {
	id := c.Param("id")
	filename := id
	if !strings.HasPrefix(filename, "beacon_") {
		respondError(c, http.StatusBadRequest, "无效的 payload id", "invalid payload id")
		return
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
		respondError(c, http.StatusBadRequest, "无效的 payload id", "invalid payload id")
		return
	}
	session, ok := security.CurrentSession(c)
	if !ok || !h.mgr().DB().UserCanAccessC2Payload(session.UserID, session.Scope, filename) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}

	builder := c2.NewPayloadBuilder(h.mgr(), h.logger, "", "")
	storageDir := builder.GetPayloadStoragePath()
	targetPath := filepath.Join(storageDir, filename)

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		respondError(c, http.StatusBadRequest, "无效的 path", "invalid path")
		return
	}
	absDir, err := filepath.Abs(storageDir)
	if err != nil || !strings.HasPrefix(absTarget, absDir+string(filepath.Separator)) {
		respondError(c, http.StatusBadRequest, "无效的 payload id", "invalid payload id")
		return
	}

	c.FileAttachment(absTarget, filepath.Base(absTarget))
}

// ============================================================================
// 事件 API
// ============================================================================

// ListEvents 获取事件列表
func (h *C2Handler) ListEvents(c *gin.Context) {
	filter := database.ListC2EventsFilter{
		Level:     c.Query("level"),
		Category:  c.Query("category"),
		ProjectID: c.Query("project_id"),
		SessionID: c.Query("session_id"),
		TaskID:    c.Query("task_id"),
	}
	if since := c.Query("since"); since != "" {
		if t, err := database.ParseRFC3339Time(since); err == nil {
			filter.Since = &t
		}
	}

	paginated := false
	page := 1
	pageSize := 10
	if c.Query("page") != "" || c.Query("page_size") != "" {
		paginated = true
		if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
			page = p
		}
		if ps, err := strconv.Atoi(c.DefaultQuery("page_size", "10")); err == nil && ps > 0 {
			pageSize = ps
			if pageSize > 100 {
				pageSize = 100
			}
		}
		filter.Limit = pageSize
		filter.Offset = (page - 1) * pageSize
	} else {
		if limit := c.Query("limit"); limit != "" {
			if n, err := strconv.Atoi(limit); err == nil && n > 0 {
				filter.Limit = n
			}
		}
	}

	access := c2AccessFromContext(c)
	events, err := h.mgr().DB().ListC2EventsForAccess(filter, access)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !paginated {
		c.JSON(http.StatusOK, gin.H{"events": events})
		return
	}
	total, err := h.mgr().DB().CountC2EventsForAccess(filter, access)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	levelCounts, err := h.mgr().DB().CountC2EventsByLevelForAccess(filter, access)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"events":       events,
		"total":        total,
		"level_counts": levelCounts,
		"page":         page,
		"page_size":    pageSize,
	})
}

// DeleteEvents 批量删除事件（请求体 JSON: {"ids":["e_xxx",...]}）
func (h *C2Handler) DeleteEvents(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorf(c, http.StatusBadRequest, "无效的 JSON: %s", "invalid json: %s", err.Error())
		return
	}
	if len(req.IDs) == 0 {
		respondError(c, http.StatusBadRequest, "ids 必填", "ids is required")
		return
	}
	n, err := h.mgr().DB().DeleteC2EventsByIDsForAccess(req.IDs, c2AccessFromContext(c))
	if err != nil {
		if errors.Is(err, database.ErrNoValidC2EventIDs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": n})
}

// EventStream SSE 实时事件流
func (h *C2Handler) EventStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	sessionFilter := c.Query("session_id")
	categoryFilter := c.Query("category")
	levels := c.QueryArray("level")

	sub := h.mgr().EventBus().Subscribe(
		"sse-"+uuid.New().String(),
		128,
		sessionFilter,
		categoryFilter,
		levels,
	)
	defer h.mgr().EventBus().Unsubscribe(sub.ID)

	c.Stream(func(w io.Writer) bool {
		select {
		case e, ok := <-sub.Ch:
			if !ok {
				return false
			}
			if !h.c2EventAllowed(c, e) {
				return true
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

// ============================================================================
// Profile API
// ============================================================================

// ListProfiles 获取 Malleable Profile 列表
func (h *C2Handler) ListProfiles(c *gin.Context) {
	profiles, err := h.mgr().DB().ListC2Profiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

// GetProfile 获取单个 Profile
func (h *C2Handler) GetProfile(c *gin.Context) {
	id := c.Param("id")
	profile, err := h.mgr().DB().GetC2Profile(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if profile == nil {
		respondError(c, http.StatusNotFound, "profile 不存在", "profile not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile": profile})
}

// CreateProfile 创建 Profile
func (h *C2Handler) CreateProfile(c *gin.Context) {
	var req database.C2Profile
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.JitterMinMS < 0 || req.JitterMaxMS < req.JitterMinMS {
		respondError(c, http.StatusBadRequest, "Jitter 必须满足 0 <= min <= max", "Jitter must satisfy 0 <= min <= max")
		return
	}
	req.ID = "p_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
	req.CreatedAt = time.Now()

	if err := h.mgr().DB().CreateC2Profile(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile": req})
}

// UpdateProfile 更新 Profile
func (h *C2Handler) UpdateProfile(c *gin.Context) {
	id := c.Param("id")
	profile, err := h.mgr().DB().GetC2Profile(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if profile == nil {
		respondError(c, http.StatusNotFound, "profile 不存在", "profile not found")
		return
	}

	var req database.C2Profile
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.JitterMinMS < 0 || req.JitterMaxMS < req.JitterMinMS {
		respondError(c, http.StatusBadRequest, "Jitter 必须满足 0 <= min <= max", "Jitter must satisfy 0 <= min <= max")
		return
	}
	profile.Name = req.Name
	profile.UserAgent = req.UserAgent
	profile.URIs = req.URIs
	profile.RequestHeaders = req.RequestHeaders
	profile.ResponseHeaders = req.ResponseHeaders
	profile.BodyTemplate = req.BodyTemplate
	profile.JitterMinMS = req.JitterMinMS
	profile.JitterMaxMS = req.JitterMaxMS

	if err := h.mgr().DB().UpdateC2Profile(profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile": profile})
}

// DeleteProfile 删除 Profile
func (h *C2Handler) DeleteProfile(c *gin.Context) {
	id := c.Param("id")
	if err := h.mgr().DB().DeleteC2Profile(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusNotFound, "profile 不存在", "profile not found")
			return
		}
		h.logger.Error("删除 C2 Profile 失败", zap.Error(err), zap.String("id", id))
		respondError(c, http.StatusInternalServerError, "删除 profile 失败", "failed to delete profile")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ============================================================================
// 文件管理 API（C2 Upload 任务需要先通过此 API 上传文件到 downstream 目录）
// ============================================================================

// UploadFileForImplant 操作员上传文件，供 upload 任务推送给 implant
func (h *C2Handler) UploadFileForImplant(c *gin.Context) {
	sessionID := strings.TrimSpace(c.PostForm("session_id"))
	remotePath := strings.TrimSpace(c.PostForm("remote_path"))
	if sessionID == "" || remotePath == "" {
		respondError(c, http.StatusBadRequest, "session_id 与 remote_path 必填", "session_id and remote_path required")
		return
	}
	if !h.c2ResourceAllowed(c, "c2_session", sessionID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		respondErrorf(c, http.StatusBadRequest, "file 字段必填: %s", "file field required: %s", err.Error())
		return
	}
	defer file.Close()

	fileID := "f_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
	dir := filepath.Join(h.mgr().StorageDir(), "downstream")
	if err := osMkdirAll(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	dstPath := filepath.Join(dir, fileID+".bin")
	dst, err := osCreate(dstPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, err := io.Copy(dst, file)
	dst.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Record in DB
	dbFile := &database.C2File{
		ID:         fileID,
		SessionID:  sessionID,
		Direction:  "upload",
		RemotePath: remotePath,
		LocalPath:  dstPath,
		SizeBytes:  n,
		CreatedAt:  time.Now(),
	}
	_ = h.mgr().DB().CreateC2File(dbFile)

	c.JSON(http.StatusOK, gin.H{
		"file_id":     fileID,
		"size":        n,
		"filename":    header.Filename,
		"remote_path": remotePath,
	})
}

// ListFiles 列出某会话的文件记录
func (h *C2Handler) ListFiles(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		respondError(c, http.StatusBadRequest, "session_id 必填", "session_id required")
		return
	}
	if !h.c2ResourceAllowed(c, "c2_session", sessionID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	files, err := h.mgr().DB().ListC2FilesBySession(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// DownloadResultFile 下载任务结果文件（截图等 blob 结果）
func (h *C2Handler) DownloadResultFile(c *gin.Context) {
	taskID := c.Param("id")
	task, err := h.mgr().DB().GetC2Task(taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		respondError(c, http.StatusNotFound, "任务不存在", "task not found")
		return
	}
	if !h.c2ResourceAllowed(c, "c2_task", taskID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	if task.ResultBlobPath == "" {
		respondError(c, http.StatusNotFound, "该任务没有结果文件", "no result file for this task")
		return
	}
	c.FileAttachment(task.ResultBlobPath, filepath.Base(task.ResultBlobPath))
}

func osMkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

func osCreate(path string) (*os.File, error) {
	return os.Create(path)
}

func c2AccessFromContext(c *gin.Context) database.RBACListAccess {
	session, ok := security.CurrentSession(c)
	if !ok {
		return database.RBACListAccess{}
	}
	return database.RBACListAccess{UserID: session.UserID, Scope: session.Scope}
}

func (h *C2Handler) canAccessProject(c *gin.Context, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return true
	}
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	return h.mgr().DB().UserCanAccessResource(session.UserID, session.Scope, "project", projectID)
}

func (h *C2Handler) c2ResourceAllowed(c *gin.Context, resourceType, resourceID string) bool {
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	return h.mgr().DB().UserCanAccessResource(session.UserID, session.Scope, resourceType, resourceID)
}

func (h *C2Handler) c2EventAllowed(c *gin.Context, e *c2.Event) bool {
	if e == nil {
		return false
	}
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	if strings.TrimSpace(e.SessionID) != "" {
		return h.mgr().DB().UserCanAccessResource(session.UserID, session.Scope, "c2_session", e.SessionID)
	}
	if strings.TrimSpace(e.TaskID) != "" {
		return h.mgr().DB().UserCanAccessResource(session.UserID, session.Scope, "c2_task", e.TaskID)
	}
	return false
}

// ============================================================================
// 辅助函数（firstNonEmpty 已在 vulnerability.go 中定义）
// ============================================================================
