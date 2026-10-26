package handler

import (
	"net/http"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AuditHandler serves platform audit log APIs.
type AuditHandler struct {
	db     *database.DB
	audit  *audit.Service
	logger *zap.Logger
}

// NewAuditHandler creates an audit log handler.
func NewAuditHandler(db *database.DB, auditSvc *audit.Service, logger *zap.Logger) *AuditHandler {
	return &AuditHandler{db: db, audit: auditSvc, logger: logger}
}

// Meta GET /api/audit/meta
func (h *AuditHandler) Meta(c *gin.Context) {
	enabled := false
	retentionDays := 0
	if h.audit != nil {
		enabled = h.audit.Enabled()
		retentionDays = h.audit.RetentionDays()
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":           enabled,
		"retention_days":    retentionDays,
		"default_page_size": 20,
		"max_page_size":     100,
		"max_export":        5000,
	})
}

// Summary GET /api/audit/summary
func (h *AuditHandler) Summary(c *gin.Context) {
	if h.db == nil {
		respondError(c, http.StatusInternalServerError, "数据库不可用", "database unavailable")
		return
	}
	base := auditFilterForAccess(c, auditFilterFromQuery(c))
	total, err := h.db.CountAuditLogs(base)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	failFilter := base
	failFilter.Result = "failure"
	failures, err := h.db.CountAuditLogs(failFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	since := time.Now().AddDate(0, 0, -7)
	recentFilter := base
	recentFilter.Since = &since
	recent7d, err := h.db.CountAuditLogs(recentFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":     total,
		"failures":  failures,
		"recent_7d": recent7d,
		"has_filters": c.Query("category") != "" || c.Query("action") != "" || c.Query("result") != "" ||
			c.Query("q") != "" || c.Query("since") != "" || c.Query("until") != "",
	})
}

// ListLogs GET /api/audit/logs
func (h *AuditHandler) ListLogs(c *gin.Context) {
	if h.db == nil {
		respondError(c, http.StatusInternalServerError, "数据库不可用", "database unavailable")
		return
	}
	filter := auditFilterForAccess(c, auditFilterFromQuery(c))
	page, pageSize := auditPaginationFromQuery(c)
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize

	logs, err := h.db.ListAuditLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	total, err := h.db.CountAuditLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"logs":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetLog GET /api/audit/logs/:id
func (h *AuditHandler) GetLog(c *gin.Context) {
	if h.db == nil {
		respondError(c, http.StatusInternalServerError, "数据库不可用", "database unavailable")
		return
	}
	row, err := h.db.GetAuditLogByID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "审计记录不存在", "audit record not found")
		return
	}
	if session, ok := security.CurrentSession(c); !ok || (session.Scope != database.RBACScopeAll && row.Actor != session.Username) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	audit.ApplyResourceAvailability(h.db, row)
	c.JSON(http.StatusOK, gin.H{"log": row})
}

// ExportLogs GET /api/audit/logs/export — JSON or CSV (?format=csv), max 5000 rows.
func (h *AuditHandler) ExportLogs(c *gin.Context) {
	if h.db == nil {
		respondError(c, http.StatusInternalServerError, "数据库不可用", "database unavailable")
		return
	}
	filter := auditFilterForAccess(c, auditFilterFromQuery(c))
	filter.Limit = 5000
	filter.Offset = 0

	logs, err := h.db.ListAuditLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if c.Query("format") == "csv" {
		writeAuditLogsCSV(c, logs)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="audit-logs.json"`)
	c.JSON(http.StatusOK, gin.H{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"logs":        logs,
	})
}

func auditFilterForAccess(c *gin.Context, filter database.ListAuditLogsFilter) database.ListAuditLogsFilter {
	if session, ok := security.CurrentSession(c); ok && session.Scope != database.RBACScopeAll {
		filter.Actor = session.Username
	}
	return filter
}
