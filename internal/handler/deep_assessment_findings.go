package handler

import (
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"database/sql"
	"github.com/gin-gonic/gin"
	"strconv"
)

func decorateAssessmentNavigation(db *database.DB, c *gin.Context, v *database.Vulnerability) {
	if v == nil || v.AssessmentID == "" || v.AssessmentDeleted {
		return
	}
	session, ok := security.CurrentSession(c)
	v.AssessmentCanOpen = ok && security.SessionHasPermission(c, "assessments:read") && db.UserCanAccessResource(session.UserID, session.ScopeFor("assessments:read"), "deep_assessment", v.AssessmentID)
}
func (h *AgentHandler) DeepAssessmentFindings(c *gin.Context) {
	if !security.SessionHasPermission(c, "vulnerability:read") {
		c.JSON(403, gin.H{"error": "缺少漏洞读取权限"})
		return
	}
	if _, err := h.deepAssessments.Store.Get(c.Param("id")); err != nil {
		deepAssessmentError(c, err)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.Query("offset"))
	if offset < 0 {
		offset = 0
	}
	session, _ := security.CurrentSession(c)
	access := database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("vulnerability:read")}
	filter := database.VulnerabilityListFilter{AssessmentID: c.Param("id"), SummaryOnly: true}
	total, err := h.db.CountVulnerabilitiesForAccess(filter, access)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	items, err := h.db.ListVulnerabilitiesByGroupForAccess(limit, offset, filter, access, "none", "latest")
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	for _, v := range items {
		decorateAssessmentNavigation(h.db, c, v)
	}
	c.JSON(200, gin.H{"vulnerabilities": items, "total": total, "next_offset": offset + len(items), "has_more": offset+len(items) < total})
}
func (h *AgentHandler) DeepAssessmentFinding(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok || !security.SessionHasPermission(c, "vulnerability:read") || !h.db.UserCanAccessResource(session.UserID, session.ScopeFor("vulnerability:read"), "vulnerability", c.Param("vulnerabilityId")) {
		c.JSON(403, gin.H{"error": "无权读取该漏洞"})
		return
	}
	task, err := h.deepAssessments.Store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	v, err := h.db.GetVulnerability(c.Param("vulnerabilityId"))
	if err != nil || v.AssessmentID != task.ID {
		deepAssessmentError(c, sql.ErrNoRows)
		return
	}
	decorateAssessmentNavigation(h.db, c, v)
	c.JSON(200, gin.H{"vulnerability": v, "step": task.Step(v.AssessmentStepID)})
}
func (h *AgentHandler) DeepAssessmentStep(c *gin.Context) {
	task, err := h.deepAssessments.Store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	step := task.Step(c.Param("stepId"))
	if step == nil {
		deepAssessmentError(c, sql.ErrNoRows)
		return
	}
	c.JSON(200, gin.H{"step": step, "revision": task.Revision, "task_status": task.Status})
}
