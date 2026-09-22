package handler

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
)

func normalizeHitlReviewer(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "audit_agent", "agent", "ai":
		return "audit_agent"
	default:
		return "human"
	}
}

func normalizeHitlDecidedBy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "audit_agent", "agent", "ai":
		return "audit_agent"
	case "system", "timeout":
		return "system"
	case "manual":
		return "manual"
	default:
		return "human"
	}
}

func (m *HITLManager) migrateHitlSchemaColumns() {
	_, _ = m.db.Exec(`ALTER TABLE hitl_interrupts ADD COLUMN decided_by TEXT NOT NULL DEFAULT 'human'`)
	_, _ = m.db.Exec(`ALTER TABLE hitl_interrupts ADD COLUMN reviewer TEXT NOT NULL DEFAULT 'human'`)
	_, _ = m.db.Exec(`UPDATE hitl_interrupts SET reviewer='audit_agent'
		WHERE COALESCE(decided_by, '') IN ('audit_agent', 'agent', 'ai')`)
	_, _ = m.db.Exec(`ALTER TABLE hitl_conversation_configs ADD COLUMN reviewer TEXT NOT NULL DEFAULT 'human'`)
}

func hitlInterruptRowToMap(
	id, cid, mode, toolName, toolCallID, payload, rowStatus, reviewer, decidedBy string,
	messageID sql.NullString,
	decision, comment sql.NullString,
	createdAt time.Time,
	decidedAt sql.NullTime,
) map[string]interface{} {
	msgID := ""
	if messageID.Valid {
		msgID = messageID.String
	}
	auditBackend, auditModel := hitlAuditBackendFromRecord(decidedBy, comment.String, payload)
	return map[string]interface{}{
		"id":             id,
		"conversationId": cid,
		"messageId":      msgID,
		"mode":           mode,
		"toolName":       toolName,
		"toolCallId":     toolCallID,
		"payload":        payload,
		"status":         rowStatus,
		"reviewer":       reviewer,
		"decision":       decision.String,
		"comment":        comment.String,
		"decidedBy":      decidedBy,
		"auditBackend":   auditBackend,
		"auditModel":     auditModel,
		"createdAt":      createdAt,
		"decidedAt": func() interface{} {
			if decidedAt.Valid {
				return decidedAt.Time
			}
			return nil
		}(),
	}
}

func (h *AgentHandler) buildHitlListQuery(logs bool) (string, []interface{}) {
	where, args := h.buildHitlLogsWhere(logs)
	q := `SELECT id, conversation_id, message_id, mode, tool_name, tool_call_id, payload, status, COALESCE(reviewer,'human'), decision, decision_comment, COALESCE(decided_by,'human'), created_at, decided_at FROM hitl_interrupts` + where
	return q, args
}

func (h *AgentHandler) buildHitlLogsWhere(logs bool) (string, []interface{}) {
	q := " WHERE 1=1"
	args := []interface{}{}
	if logs {
		q += " AND status != 'pending'"
	} else {
		// 该接口只返回真正等待用户操作的人工审批。Agent 审查即使正在运行，
		// 也不应触发弹窗、倒计时或项目待审批计数。
		q += " AND status = 'pending' AND COALESCE(reviewer,'human') = 'human'"
	}
	return q, args
}

func (h *AgentHandler) appendHitlListFilters(q string, args []interface{}, c *gin.Context) (string, []interface{}) {
	conversationID := strings.TrimSpace(c.Query("conversationId"))
	toolName := strings.TrimSpace(c.Query("toolName"))
	decision := strings.TrimSpace(c.Query("decision"))
	decidedBy := strings.TrimSpace(c.Query("decidedBy"))
	status := strings.TrimSpace(c.Query("status"))
	search := strings.TrimSpace(c.Query("q"))

	if conversationID != "" {
		q += " AND conversation_id = ?"
		args = append(args, conversationID)
	}
	if toolName != "" {
		q += " AND tool_name LIKE ?"
		args = append(args, "%"+toolName+"%")
	}
	if decision != "" && decision != "all" {
		q += " AND decision = ?"
		args = append(args, decision)
	}
	if decidedBy != "" && decidedBy != "all" {
		q += " AND COALESCE(decided_by,'human') = ?"
		args = append(args, normalizeHitlDecidedBy(decidedBy))
	}
	if status != "" && status != "all" {
		q += " AND status = ?"
		args = append(args, status)
	}
	if search != "" {
		like := "%" + search + "%"
		q += " AND (id LIKE ? OR conversation_id LIKE ? OR tool_name LIKE ? OR payload LIKE ? OR COALESCE(decision_comment,'') LIKE ?)"
		args = append(args, like, like, like, like, like)
	}
	return q, args
}

func (h *AgentHandler) scanHitlInterruptRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, cid, mode, toolName, toolCallID, payload, rowStatus, reviewer, decidedBy string
		var messageID sql.NullString
		var decision, comment sql.NullString
		var createdAt time.Time
		var decidedAt sql.NullTime
		if err := rows.Scan(&id, &cid, &messageID, &mode, &toolName, &toolCallID, &payload, &rowStatus, &reviewer, &decision, &comment, &decidedBy, &createdAt, &decidedAt); err != nil {
			continue
		}
		items = append(items, hitlInterruptRowToMap(id, cid, mode, toolName, toolCallID, payload, rowStatus, reviewer, decidedBy, messageID, decision, comment, createdAt, decidedAt))
	}
	return items, nil
}

func (h *AgentHandler) countHitlQuery(baseQ string, args []interface{}) (int, error) {
	countQ := "SELECT COUNT(*) FROM (" + baseQ + ") AS hitl_cnt"
	var total int
	if err := h.db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (h *AgentHandler) ListHITLLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	pageSize = int(math.Max(1, math.Min(float64(pageSize), 200)))
	offset := (page - 1) * pageSize

	q, args := h.buildHitlListQuery(true)
	q, args = h.appendHitlListFilters(q, args, c)
	q, args = appendConversationAccessSQL(q, args, "conversation_id", notificationAccessFromContext(c))
	total, err := h.countHitlQuery(q, args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	q += " ORDER BY COALESCE(decided_at, created_at) DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items, err := h.scanHitlInterruptRows(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "pageSize": pageSize, "total": total, "retentionDays": h.hitlRetentionDays()})
}

func (h *AgentHandler) hitlRetentionDays() int {
	if h.config != nil {
		return h.config.Hitl.RetentionDaysEffective()
	}
	return config.HitlConfig{}.RetentionDaysEffective()
}

// DeleteHITLLogs 批量删除或按筛选清空已决策的人机协同审计日志（不删除 pending）。
func (h *AgentHandler) DeleteHITLLogs(c *gin.Context) {
	var request struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效: " + err.Error()})
		return
	}

	var deleted int64
	var err error
	if request.All {
		where, args := h.buildHitlLogsWhere(true)
		where, args = h.appendHitlListFilters(where, args, c)
		where, args = appendConversationAccessSQL(where, args, "conversation_id", notificationAccessFromContext(c))
		deleted, err = h.db.DeleteHitlInterruptLogsMatching(where, args)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if h.audit != nil {
			h.audit.RecordOK(c, "hitl", "logs_clear", "清空人机协同审计日志", "hitl_interrupt", "", map[string]interface{}{
				"deleted": deleted,
			})
		}
	} else {
		if len(request.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "审计日志 ID 列表不能为空"})
			return
		}
		ids, filterErr := h.filterAllowedHitlInterruptIDs(c, request.IDs)
		if filterErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": filterErr.Error()})
			return
		}
		deleted, err = h.db.DeleteHitlInterruptLogsByIDs(ids)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if h.audit != nil {
			h.audit.RecordOK(c, "hitl", "logs_delete_batch", "批量删除人机协同审计日志", "hitl_interrupt", "", map[string]interface{}{
				"count":   len(request.IDs),
				"deleted": deleted,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功", "deleted": deleted})
}

func (h *AgentHandler) GetHITLLog(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	q := `SELECT id, conversation_id, message_id, mode, tool_name, tool_call_id, payload, status, COALESCE(reviewer,'human'), decision, decision_comment, COALESCE(decided_by,'human'), created_at, decided_at FROM hitl_interrupts WHERE id = ?`
	var rowID, cid, mode, toolName, toolCallID, payload, rowStatus, reviewer, decidedBy string
	var messageID sql.NullString
	var decision, comment sql.NullString
	var createdAt time.Time
	var decidedAt sql.NullTime
	err := h.db.QueryRow(q, id).Scan(&rowID, &cid, &messageID, &mode, &toolName, &toolCallID, &payload, &rowStatus, &reviewer, &decision, &comment, &decidedBy, &createdAt, &decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !h.hitlConversationAllowed(c, cid) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该资源"})
		return
	}
	c.JSON(http.StatusOK, hitlInterruptRowToMap(rowID, cid, mode, toolName, toolCallID, payload, rowStatus, reviewer, decidedBy, messageID, decision, comment, createdAt, decidedAt))
}

func (h *AgentHandler) filterAllowedHitlInterruptIDs(c *gin.Context, ids []string) ([]string, error) {
	clean := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return clean, nil
	}
	query := `SELECT id, conversation_id FROM hitl_interrupts WHERE id IN (` + buildPlaceholders(len(clean)) + `)`
	args := make([]interface{}, 0, len(clean))
	for _, id := range clean {
		args = append(args, id)
	}
	rows, err := h.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowed := make([]string, 0, len(clean))
	for rows.Next() {
		var id, conversationID string
		if err := rows.Scan(&id, &conversationID); err != nil {
			continue
		}
		if h.hitlConversationAllowed(c, conversationID) {
			allowed = append(allowed, id)
		}
	}
	return allowed, rows.Err()
}

func (h *AgentHandler) hitlInterruptAllowed(c *gin.Context, interruptID string) bool {
	var conversationID string
	if err := h.db.QueryRow(`SELECT conversation_id FROM hitl_interrupts WHERE id = ?`, strings.TrimSpace(interruptID)).Scan(&conversationID); err != nil {
		return false
	}
	return h.hitlConversationAllowed(c, conversationID)
}

func (h *AgentHandler) hitlConversationAllowed(c *gin.Context, conversationID string) bool {
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	return h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", conversationID)
}
