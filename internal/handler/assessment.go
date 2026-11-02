package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/assessment"
	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AssessmentHandler struct {
	Engine *assessment.Engine
	DB     *database.DB
	Config *config.Config
	Audit  *audit.Service
}

func (h *AssessmentHandler) fail(c *gin.Context, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, sql.ErrNoRows) {
		code = 404
	}
	if errors.Is(err, assessment.ErrConflict) {
		code = 409
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
func (h *AssessmentHandler) task(c *gin.Context) (*assessment.Task, bool) {
	session, ok := security.CurrentSession(c)
	if !ok {
		c.AbortWithStatus(401)
		return nil, false
	}
	t, err := h.Engine.Store.Get(c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return nil, false
	}
	if !h.DB.UserCanAccessResource(session.UserID, session.Scope, "assessment", t.ID) {
		c.AbortWithStatusJSON(403, gin.H{"error": "无权访问该测评"})
		return nil, false
	}
	return t, true
}
func (h *AssessmentHandler) Options(c *gin.Context) {
	options := []gin.H{}
	for id, ch := range h.Config.AI.Channels {
		options = append(options, gin.H{"id": id, "name": ch.Name, "model": ch.Model, "default": id == h.Config.AI.DefaultChannel})
	}
	if len(options) == 0 {
		options = append(options, gin.H{"id": "", "name": "Default", "model": h.Config.OpenAI.Model, "default": true})
	}
	sort.Slice(options, func(i, j int) bool { return options[i]["id"].(string) < options[j]["id"].(string) })
	tools, _ := h.Engine.Runtime.Tools(&assessment.Task{Tools: allAssessmentToolNames(h)})
	safe := []gin.H{}
	for _, tool := range tools {
		safe = append(safe, gin.H{"name": tool.Function.Name, "description": tool.Function.Description})
	}
	c.JSON(200, gin.H{"models": options, "tools": safe})
}
func allAssessmentToolNames(h *AssessmentHandler) []string {
	if r, ok := h.Engine.Runtime.(*assessment.CSAIRuntime); ok && r.Agent != nil {
		out := []string{}
		for _, t := range r.Agent.ToolsForRole(nil) {
			out = append(out, t.Function.Name)
		}
		return out
	}
	return nil
}
func (h *AssessmentHandler) List(c *gin.Context) {
	session, _ := security.CurrentSession(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := h.Engine.Store.List(session.UserID, session.Scope, limit, offset)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"assessments": rows})
}
func (h *AssessmentHandler) Get(c *gin.Context) {
	if t, ok := h.task(c); ok {
		c.JSON(200, gin.H{"assessment": t.Public()})
	}
}
func (h *AssessmentHandler) Create(c *gin.Context) {
	// Only creation fields are accepted. Runtime state/owner cannot be supplied.
	var req struct {
		ApprovalMode string   `json:"approval_mode"`
		Name         string   `json:"name"`
		Goal         string   `json:"goal"`
		Scope        string   `json:"scope"`
		Material     string   `json:"material"`
		ProjectID    string   `json:"project_id"`
		Model        string   `json:"model"`
		Tools        []string `json:"tools"`
		Concurrency  int      `json:"concurrency"`
		TokenBudget  int      `json:"token_budget"`
		Seconds      int64    `json:"remaining_seconds"`
		Rounds       int      `json:"max_rounds"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if err := c.ShouldBindJSON(&req); err != nil {
		h.fail(c, err)
		return
	}
	session, _ := security.CurrentSession(c)
	if req.ProjectID != "" && (!security.SessionHasPermission(c, "project:read") || !h.DB.UserCanAccessResource(session.UserID, session.ScopeFor("project:read"), "project", req.ProjectID)) {
		c.JSON(403, gin.H{"error": "无权访问关联项目"})
		return
	}
	if req.ProjectID != "" {
		p, err := h.DB.GetProject(req.ProjectID)
		if err != nil || p == nil {
			h.fail(c, fmt.Errorf("project does not exist"))
			return
		}
	}
	if req.Model != "" {
		if _, _, ok := h.Config.AI.ResolveChannel(req.Model); !ok {
			h.fail(c, fmt.Errorf("unknown model channel"))
			return
		}
	}
	if _, _, ok := h.Config.ResolveAIChannel(req.Model); !ok {
		h.fail(c, fmt.Errorf("unknown model channel"))
		return
	}
	t := &assessment.Task{ApprovalMode: req.ApprovalMode, Name: req.Name, Goal: req.Goal, Scope: req.Scope, Material: req.Material, ProjectID: req.ProjectID, OwnerID: session.UserID, Model: req.Model, Tools: req.Tools, Concurrency: req.Concurrency, TokenBudget: req.TokenBudget, RemainingSeconds: req.Seconds, MaxRounds: req.Rounds}
	if err := t.Validate(); err != nil {
		h.fail(c, err)
		return
	}
	tools, err := h.Engine.Runtime.Tools(t)
	if err != nil {
		h.fail(c, err)
		return
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Function.Name] = true
	}
	for _, name := range t.Tools {
		if !names[name] {
			h.fail(c, fmt.Errorf("unavailable or conversation-bound tool: %s", name))
			return
		}
	}
	if err = h.Engine.Store.Create(t); err != nil {
		h.fail(c, err)
		return
	}
	// Assigned-scope creators must be able to access their own new resource.
	if err = h.DB.AssignResourceToUser(session.UserID, "assessment", t.ID); err != nil {
		_ = h.Engine.Store.Delete(t.ID)
		h.fail(c, err)
		return
	}
	h.Audit.RecordOK(c, "assessment", "create", "Created assessment", "assessment", t.ID, nil)
	c.JSON(201, gin.H{"assessment": t.Public()})
}
func (h *AssessmentHandler) Control(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	action := c.Param("action")
	var err error
	switch action {
	case "start", "resume":
		if !security.SessionHasPermission(c, "agent:execute") {
			c.JSON(403, gin.H{"error": "agent:execute is required"})
			return
		}
		err = h.Engine.Start(t.ID)
	case "pause", "stop":
		err = h.Engine.Control(t.ID, action)
	case "steer":
		var req struct {
			Message string `json:"message"`
		}
		if err = c.ShouldBindJSON(&req); err == nil {
			text := strings.TrimSpace(req.Message)
			if len(text) == 0 || len(text) > 8000 {
				err = fmt.Errorf("message must contain 1..8000 bytes")
			} else {
				err = h.Engine.Store.Change(t.ID, "", "steer", text, func(t *assessment.Task) error {
					if t.Status != "draft" && t.Status != "paused" && t.Status != "blocked" {
						return assessment.ErrConflict
					}
					if len(t.Material)+len(text) > 24000 {
						return fmt.Errorf("materials too long")
					}
					t.Material += "\n\nHuman guidance:\n" + text
					return nil
				})
			}
		}
	default:
		err = fmt.Errorf("unknown assessment action")
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	h.Audit.RecordOK(c, "assessment", action, "Assessment control", "assessment", t.ID, nil)
	c.JSON(200, gin.H{"ok": true})
}
func (h *AssessmentHandler) Decision(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	if !security.SessionHasPermission(c, "hitl:write") {
		c.JSON(403, gin.H{"error": "hitl:write is required"})
		return
	}
	var req struct {
		Approved *bool `json:"approved"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.fail(c, err)
		return
	}
	if req.Approved == nil {
		h.fail(c, fmt.Errorf("approved is required"))
		return
	}
	if err := h.Engine.Decide(t.ID, c.Param("stepId"), *req.Approved); err != nil {
		h.fail(c, err)
		return
	}
	h.Audit.RecordOK(c, "assessment", "approval", "Assessment tool decision", "assessment", t.ID, map[string]interface{}{"step_id": c.Param("stepId"), "approved": *req.Approved})
	c.JSON(200, gin.H{"ok": true})
}
func (h *AssessmentHandler) Retry(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	if err := h.Engine.Retry(t.ID, c.Param("stepId")); err != nil {
		h.fail(c, err)
		return
	}
	h.Audit.RecordOK(c, "assessment", "retry", "Explicit assessment retry", "assessment", t.ID, nil)
	c.JSON(200, gin.H{"ok": true})
}
func (h *AssessmentHandler) Events(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	events, err := h.Engine.Store.Events(t.ID, after)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"events": events})
}
func (h *AssessmentHandler) Report(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	c.Header("Content-Disposition", `attachment; filename="assessment-`+t.ID+`.md"`)
	c.Data(200, "text/markdown; charset=utf-8", []byte(assessment.Report(t)))
}
func (h *AssessmentHandler) Delete(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	if err := h.Engine.Store.Delete(t.ID); err != nil {
		h.fail(c, err)
		return
	}
	h.Audit.RecordOK(c, "assessment", "delete", "Deleted assessment", "assessment", t.ID, nil)
	c.JSON(200, gin.H{"ok": true})
}

// Publish exports a reviewed observation with stable provenance, without chat rows.
func (h *AssessmentHandler) Publish(c *gin.Context) {
	t, ok := h.task(c)
	if !ok {
		return
	}
	var req struct {
		StepID string `json:"step_id"`
		Index  int    `json:"index"`
		Kind   string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.fail(c, err)
		return
	}
	st := t.Step(req.StepID)
	if st == nil || req.Index < 0 || req.Index >= len(st.Evidence) {
		h.fail(c, fmt.Errorf("evidence not found"))
		return
	}
	v := st.Evidence[req.Index]
	if len(v.ExecutionIDs) == 0 || v.Kind == "gap" {
		h.fail(c, fmt.Errorf("execution-backed observation required"))
		return
	}
	session, _ := security.CurrentSession(c)
	if t.ProjectID != "" {
		p, err := h.DB.GetProject(t.ProjectID)
		if err != nil || p == nil {
			h.fail(c, fmt.Errorf("linked project no longer exists"))
			return
		}
	}
	body := fmt.Sprintf("Assessment: %s\nStep: %s\nExecution IDs: %s\n\n%s", t.ID, st.ID, strings.Join(v.ExecutionIDs, ", "), v.Detail)
	var result any
	if req.Kind == "fact" {
		if t.ProjectID == "" || !security.SessionHasPermission(c, "project:write") || !h.DB.UserCanAccessResource(session.UserID, session.ScopeFor("project:write"), "project", t.ProjectID) {
			c.JSON(403, gin.H{"error": "project:write access to a linked project is required"})
			return
		}
		fact, err := h.DB.UpsertProjectFact(&database.ProjectFact{ProjectID: t.ProjectID, FactKey: fmt.Sprintf("assessment/%s/%s/%d", t.ID, st.ID, req.Index), Category: "note", Summary: v.Title, Body: body, Confidence: "tentative"})
		if err != nil {
			h.fail(c, err)
			return
		}
		result = fact
	} else if req.Kind == "vulnerability" && v.Kind == "finding" {
		if !security.SessionHasPermission(c, "vulnerability:write") {
			c.JSON(403, gin.H{"error": "vulnerability:write is required"})
			return
		}
		if t.ProjectID != "" && (!security.SessionHasPermission(c, "project:write") || !h.DB.UserCanAccessResource(session.UserID, session.ScopeFor("project:write"), "project", t.ProjectID)) {
			c.JSON(403, gin.H{"error": "project:write access is required"})
			return
		}
		vid := uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("assessment/%s/%s/%d", t.ID, st.ID, req.Index))).String()
		if err := h.Engine.Store.PublishCandidate(vid, t.ProjectID, session.UserID, v.Title, v.Detail, body, "assessment:"+t.ID); err != nil {
			h.fail(c, err)
			return
		}
		result = gin.H{"id": vid}
	} else if req.Kind == "asset" && v.Kind == "asset" && v.Asset != nil {
		if !security.SessionHasPermission(c, "asset:write") {
			c.JSON(403, gin.H{"error": "asset:write is required"})
			return
		}
		if t.ProjectID != "" && (!security.SessionHasPermission(c, "project:write") || !h.DB.UserCanAccessResource(session.UserID, session.ScopeFor("project:write"), "project", t.ProjectID)) {
			c.JSON(403, gin.H{"error": "project:write access is required"})
			return
		}
		a := v.Asset
		imported, err := h.DB.UpsertAssets([]*database.Asset{{ProjectID: t.ProjectID, Host: a.Host, IP: a.IP, Domain: a.Domain, Port: a.Port, Protocol: a.Protocol, Server: a.Server, Title: v.Title, Source: "assessment", SourceQuery: body, Tags: []string{"assessment:" + t.ID}}}, session.UserID, session.ScopeFor("asset:write") == database.RBACScopeAll)
		if err != nil {
			h.fail(c, err)
			return
		}
		if imported.Skipped > 0 {
			h.fail(c, fmt.Errorf("asset already exists outside your ownership or lacks an address"))
			return
		}
		result = imported
	} else {
		h.fail(c, fmt.Errorf("kind must be fact, vulnerability for a finding, or asset for an observed asset"))
		return
	}
	h.Audit.RecordOK(c, "assessment", "publish", "Published reviewed observation", "assessment", t.ID, map[string]interface{}{"step_id": st.ID, "kind": req.Kind})
	c.JSON(200, gin.H{"result": result})
}
