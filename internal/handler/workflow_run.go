package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	workflowrunner "cyberstrike-ai/internal/workflow"

	"github.com/gin-gonic/gin"
)

func (h *WorkflowHandler) SetRuntime(agent *agent.Agent, cfg *config.Config) {
	h.agent = agent
	h.cfg = cfg
}

func (h *WorkflowHandler) GetRun(c *gin.Context) {
	runID := strings.TrimSpace(c.Param("runId"))
	if !h.workflowRunAllowed(c, runID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	run, err := h.db.GetWorkflowRun(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if run == nil {
		respondError(c, http.StatusNotFound, "工作流运行不存在", "workflow run not found")
		return
	}
	nodeRuns, err := h.db.ListWorkflowNodeRuns(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run, "nodeRuns": nodeRuns})
}

func (h *WorkflowHandler) ReplayRun(c *gin.Context) {
	runID := strings.TrimSpace(c.Param("runId"))
	if !h.workflowRunAllowed(c, runID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	nodeRuns, err := h.db.ListWorkflowNodeRuns(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	steps := make([]gin.H, 0, len(nodeRuns))
	for i, nodeRun := range nodeRuns {
		var input any
		var output any
		_ = json.Unmarshal([]byte(nodeRun.InputJSON), &input)
		_ = json.Unmarshal([]byte(nodeRun.OutputJSON), &output)
		steps = append(steps, gin.H{
			"step":       i + 1,
			"nodeRunId":  nodeRun.ID,
			"nodeId":     nodeRun.NodeID,
			"status":     nodeRun.Status,
			"input":      input,
			"output":     output,
			"error":      nodeRun.Error,
			"startedAt":  nodeRun.StartedAt,
			"finishedAt": nodeRun.FinishedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"workflowRunId": runID, "steps": steps})
}

func (h *WorkflowHandler) ListPendingRuns(c *gin.Context) {
	conversationID := strings.TrimSpace(c.Query("conversationId"))
	if conversationID != "" && !h.workflowConversationAllowed(c, conversationID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	runs, err := h.db.ListWorkflowRunsAwaitingHITLFiltered(conversationID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	runs = filterSlice(runs, func(run *database.WorkflowRun) bool {
		return run != nil && h.workflowConversationAllowed(c, run.ConversationID)
	})
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

type workflowResumeRequest struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment,omitempty"`
}

func (h *WorkflowHandler) ResumeRun(c *gin.Context) {
	if h.agent == nil || h.cfg == nil {
		respondError(c, http.StatusServiceUnavailable, "工作流运行时未初始化", "workflow runtime not initialized")
		return
	}
	runID := strings.TrimSpace(c.Param("runId"))
	if !h.workflowRunAllowed(c, runID) {
		respondError(c, http.StatusForbidden, "无权访问该资源", "no permission to access this resource")
		return
	}
	var req workflowResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "无效的请求参数: %s", "invalid request parameter: %s"), err.Error())})
		return
	}
	run, err := h.db.GetWorkflowRun(runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if run == nil {
		respondError(c, http.StatusNotFound, "工作流运行不存在", "workflow run not found")
		return
	}
	role := config.RoleConfig{Name: strings.TrimSpace(run.RoleID)}
	if role.Name != "" && h.cfg.Roles != nil {
		if r, ok := h.cfg.Roles[role.Name]; ok {
			role = r
			if role.Name == "" {
				role.Name = run.RoleID
			}
		}
	}
	if run.Status != "awaiting_hitl" {
		respondErrorf(c, http.StatusBadRequest, "工作流运行不在等待审批状态: %s", "workflow run is not awaiting approval, current status: %s", run.Status)
		return
	}
	if err := h.db.RecordWorkflowRunHITLDecision(runID, req.Approved, req.Comment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	decision := workflowrunner.HITLDecision{
		Approved: req.Approved,
		Comment:  strings.TrimSpace(req.Comment),
	}
	delegated := workflowrunner.NotifyHITLDecision(runID, decision)
	if !delegated {
		for i := 0; i < 10; i++ {
			time.Sleep(50 * time.Millisecond)
			if workflowrunner.NotifyHITLDecision(runID, decision) {
				delegated = true
				break
			}
		}
	}
	if delegated {
		c.JSON(http.StatusOK, gin.H{
			"workflowRunId":  runID,
			"status":         "delegated",
			"streamResuming": true,
			"approved":       req.Approved,
		})
		return
	}
	result, err := workflowrunner.ResumeWorkflowRun(c.Request.Context(), workflowrunner.RunArgs{
		DB:             h.db,
		Logger:         h.logger,
		Role:           role,
		AppCfg:         h.cfg,
		Agent:          h.agent,
		ConversationID: run.ConversationID,
		ProjectID:      run.ProjectID,
	}, runID, req.Approved, req.Comment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"response":      result.Response,
		"workflowRunId": result.RunID,
		"status":        result.Status,
		"awaitingHitl":  result.AwaitingHITL,
	})
}

func (h *WorkflowHandler) workflowConversationAllowed(c *gin.Context, conversationID string) bool {
	session, ok := security.CurrentSession(c)
	return ok && h.db.UserCanAccessResource(session.UserID, session.Scope, "conversation", strings.TrimSpace(conversationID))
}

func (h *WorkflowHandler) workflowRunAllowed(c *gin.Context, runID string) bool {
	session, ok := security.CurrentSession(c)
	if !ok {
		return false
	}
	if session.Scope == database.RBACScopeAll {
		return true
	}
	run, err := h.db.GetWorkflowRun(strings.TrimSpace(runID))
	return err == nil && run != nil && h.workflowConversationAllowed(c, run.ConversationID)
}
