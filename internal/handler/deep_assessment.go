package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *AgentHandler) InitDeepAssessments(snapshot func() (*config.Config, error)) error {
	s := &deepassessment.Store{DB: h.db}
	if err := s.MigrateContinuous(); err != nil {
		return err
	}
	if err := s.Recover(); err != nil {
		return err
	}
	r := &deepAssessmentRuntime{h: h, snapshot: snapshot}
	h.deepAssessmentRuntime = r
	h.deepAssessments = deepassessment.NewEngine(s, r)
	return nil
}

func (h *AgentHandler) StartDeepAssessmentRecovery() {
	if h.deepAssessments != nil {
		h.deepAssessments.StartRecovery()
	}
}

func (h *AgentHandler) DeepAssessmentReceipts(c *gin.Context) {
	offset, _ := strconv.Atoi(c.Query("offset"))
	if offset < 0 {
		offset = 0
	}
	stepID, receiptID := c.Query("step_id"), c.Query("receipt_id")
	if stepID != "" {
		task, err := h.deepAssessments.Store.Get(c.Param("id"))
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		if task.Step(stepID) == nil {
			deepAssessmentError(c, sql.ErrNoRows)
			return
		}
	}
	rows, err := h.db.Query(`SELECT id,step_id,tool,arguments,status,execution_id,result FROM deep_assessment_calls WHERE task_id=? AND (?='' OR step_id=?) AND (?='' OR id=?) ORDER BY updated_at DESC,id LIMIT 50 OFFSET ?`, c.Param("id"), stepID, stepID, receiptID, receiptID, offset)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, step, tool, args, status, execution, result string
		if err = rows.Scan(&id, &step, &tool, &args, &status, &execution, &result); err != nil {
			deepAssessmentError(c, err)
			return
		}
		out = append(out, gin.H{"id": id, "step_id": step, "tool": tool, "arguments": args, "status": status, "execution_id": execution, "result": result})
	}
	if err = rows.Err(); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if receiptID != "" && len(out) == 0 {
		deepAssessmentError(c, sql.ErrNoRows)
		return
	}
	c.JSON(200, gin.H{"receipts": out, "next_offset": offset + len(out), "has_more": len(out) == 50})
}

func deepAssessmentError(c *gin.Context, err error) {
	code := 400
	if errors.Is(err, sql.ErrNoRows) {
		code = 404
	}
	if errors.Is(err, deepassessment.ErrConflict) {
		c.JSON(409, gin.H{"error": "测评状态已更新，请刷新后重试。", "code": "task_changed"})
		return
	}
	c.JSON(code, gin.H{"error": err.Error()})
}

func deepAssessmentBody(c *gin.Context, out any) bool {
	if c.GetHeader("Authorization") == "" {
		if _, err := c.Cookie("auth_token"); err == nil {
			u, err := url.Parse(c.GetHeader("Origin"))
			if err != nil || u.Host != c.Request.Host || (u.Scheme != "http" && u.Scheme != "https") {
				c.JSON(403, gin.H{"error": "请求来源不匹配"})
				return false
			}
		}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if out != nil {
		if err := c.ShouldBindJSON(out); err != nil {
			deepAssessmentError(c, err)
			return false
		}
	}
	return true
}

func (h *AgentHandler) DeepAssessmentOptions(c *gin.Context) {
	cfg, err := h.deepAssessmentRuntime.snapshot()
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	models := []gin.H{}
	for id, ch := range cfg.AI.Channels {
		models = append(models, gin.H{"id": id, "name": ch.Name, "model": ch.Model})
	}
	sort.Slice(models, func(i, j int) bool { return models[i]["id"].(string) < models[j]["id"].(string) })
	roles := []string{}
	for name, role := range cfg.Roles {
		if role.Enabled {
			roles = append(roles, name)
		}
	}
	sort.Strings(roles)
	projects := []gin.H{}
	session, _ := security.CurrentSession(c)
	if security.SessionHasPermission(c, "project:read") {
		list, err := h.db.ListProjectsForAccess("active", "", 500, 0, session.UserID, session.ScopeFor("project:read"))
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		for _, p := range list {
			projects = append(projects, gin.H{"id": p.ID, "name": p.Name})
		}
	}
	c.JSON(200, gin.H{"models": models, "roles": roles, "projects": projects})
}

func (h *AgentHandler) CreateDeepAssessment(c *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		Objective   string `json:"objective"`
		Scope       string `json:"scope"`
		Criteria    string `json:"criteria"`
		ProjectID   string `json:"project_id"`
		Role        string `json:"role"`
		Model       string `json:"model"`
		HITLPolicy  string `json:"hitl_policy"`
		Concurrency int    `json:"concurrency"`
		AutoResume  *bool  `json:"auto_resume"`
	}
	if !deepAssessmentBody(c, &req) {
		return
	}
	session, ok := security.CurrentSession(c)
	if !ok {
		c.JSON(401, gin.H{"error": "请先登录"})
		return
	}
	if req.Name == "" {
		req.Name = deepassessment.Clip(req.Objective, 80)
	}
	if req.Concurrency == 0 {
		req.Concurrency = 2
	}
	if err := validateBatchHITLPolicy(req.HITLPolicy); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if req.ProjectID != "" && (!security.SessionHasPermission(c, "project:read") || !h.db.UserCanAccessResource(session.UserID, session.ScopeFor("project:read"), "project", req.ProjectID)) {
		c.JSON(403, gin.H{"error": "无权访问项目"})
		return
	}
	if strings.TrimSpace(req.Scope) == "" {
		req.Scope = req.Objective
	}
	if strings.TrimSpace(req.Criteria) == "" {
		req.Criteria = req.Objective
	}
	autoResume := true
	if req.AutoResume != nil {
		autoResume = *req.AutoResume
	}
	now := time.Now().UTC()
	t := &deepassessment.Task{ID: uuid.NewString(), AutoResume: autoResume, OwnerID: session.UserID, ExecutorID: session.UserID, Name: strings.TrimSpace(req.Name), Objective: strings.TrimSpace(req.Objective), Scope: strings.TrimSpace(req.Scope), Criteria: strings.TrimSpace(req.Criteria), ProjectID: req.ProjectID, Role: req.Role, Model: req.Model, HITLPolicy: req.HITLPolicy, Concurrency: req.Concurrency, Status: "draft", Steps: []deepassessment.Step{}, Guidance: []string{}, CreatedAt: now, UpdatedAt: now}
	if _, err := h.deepAssessmentRuntime.configuration(t); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if err := h.deepAssessments.Store.Create(t); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "create_deep_assessment", "创建深度测评", "deep_assessment", t.ID, nil)
	}
	c.JSON(201, gin.H{"task": t})
}

func (h *AgentHandler) ListDeepAssessments(c *gin.Context) {
	session, _ := security.CurrentSession(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	list, err := h.deepAssessments.Store.List(session.UserID, session.ScopeFor("assessments:read"), c.Query("status"), c.Query("q"), limit+1, offset, c.Query("archived") == "true")
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	more := len(list) > limit
	if more {
		list = list[:limit]
	}
	c.JSON(200, gin.H{"tasks": list, "has_more": more})
}

// Explicit paging keeps long-lived workspaces small without limiting stored work.
func deepAssessmentView(c *gin.Context, t *deepassessment.Task) gin.H {
	copy := *t
	copy.ReviewCount = 0
	for _, step := range t.Steps {
		if step.Status == "uncertain" || step.Status == "cleanup_unconfirmed" {
			copy.ReviewCount++
		}
	}
	t = &copy
	limit, _ := strconv.Atoi(c.Query("step_limit"))
	offset, _ := strconv.Atoi(c.Query("step_offset"))
	if limit <= 0 {
		return gin.H{"task": t}
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	steps := t.Steps
	counts := map[string]int{}
	for _, step := range t.Steps {
		counts[step.Status]++
	}
	counts["needs_review"] = counts["uncertain"] + counts["cleanup_unconfirmed"]
	queue := c.Query("step_order") == "queue"
	if queue {
		steps = []deepassessment.Step{}
		for _, step := range t.Queue() {
			if asset := c.Query("step_asset"); asset != "" {
				found := false
				for _, id := range step.AssetIDs {
					found = found || id == asset
				}
				if !found {
					continue
				}
			}
			filter := c.Query("step_status")
			if filter == "" || step.Status == filter || filter == "needs_review" && (step.Status == "uncertain" || step.Status == "cleanup_unconfirmed") {
				steps = append(steps, step)
			}
		}
	}
	total := len(steps)
	if total > 0 && offset >= total {
		offset = (total - 1) / limit * limit
	} else {
		offset = min(offset, total)
	}
	end := min(total, offset+limit)
	view := *t
	view.Steps = []deepassessment.Step{}
	for i := offset; i < end; i++ {
		if queue {
			view.Steps = append(view.Steps, steps[i])
		} else {
			view.Steps = append(view.Steps, steps[total-1-i])
		}
	}
	return gin.H{"task": &view, "step_total": total, "step_offset": offset, "step_next_offset": end, "step_has_more": end < total, "intent_counts": counts}
}

func (h *AgentHandler) GetDeepAssessment(c *gin.Context) {
	t, err := h.deepAssessments.Store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	view := deepAssessmentView(c, t)
	view["model_channel_name"] = h.deepAssessmentModelChannelName(t.Model)
	if c.Query("runtime") == "true" {
		live, readErr := h.deepAssessments.Store.RuntimeOverview(t)
		if readErr != nil {
			deepAssessmentError(c, readErr)
			return
		}
		live.Live = h.deepAssessments.Live(t.ID)
		live.Sessions = h.deepAssessmentRuntime.sessionActivity(t.ID)
		session, _ := security.CurrentSession(c)
		if security.SessionHasPermission(c, "vulnerability:read") {
			live.FindingsVisible = true
			live.Findings, readErr = h.db.CountVulnerabilitiesForAccess(database.VulnerabilityListFilter{AssessmentID: t.ID}, database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("vulnerability:read")})
			if readErr != nil {
				deepAssessmentError(c, readErr)
				return
			}
		}
		if security.SessionHasPermission(c, "asset:read") {
			for _, step := range t.Steps {
				if step.Status != "running" && step.Status != "pausing" {
					continue
				}
				for i, id := range step.AssetIDs {
					if i >= 3 {
						break
					}
					a, readErr := h.db.GetAsset(id, database.RBACListAccess{UserID: session.UserID, Scope: session.ScopeFor("asset:read")})
					if readErr == nil {
						live.AssetNames[id] = a.Host
						if a.Host == "" {
							live.AssetNames[id] = a.IP
						}
					}
				}
			}
		}
		view["runtime"] = live
	}
	c.JSON(200, view)
}

func (h *AgentHandler) deepAssessmentModelChannelName(channelID string) string {
	cfg, err := h.deepAssessmentRuntime.snapshot()
	if err != nil || cfg == nil {
		return ""
	}
	if strings.TrimSpace(channelID) == "" {
		channelID = cfg.AI.DefaultChannel
	}
	id := config.NormalizeAIChannelID(channelID)
	channel, ok := cfg.AI.Channels[id]
	if !ok {
		return ""
	}
	if name := strings.TrimSpace(channel.Name); name != "" {
		return name
	}
	return id
}

func (h *AgentHandler) DeepAssessmentEvents(c *gin.Context) {
	if c.Query("latest") == "true" {
		events, err := h.deepAssessments.Store.RecentEvents(c.Param("id"), 100)
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
		c.JSON(200, gin.H{"events": events})
		return
	}
	after, _ := strconv.ParseInt(c.Query("after"), 10, 64)
	list, err := h.deepAssessments.Store.Events(c.Param("id"), after)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.JSON(200, gin.H{"events": list})
}
func (h *AgentHandler) DeleteDeepAssessment(c *gin.Context) {
	if !deepAssessmentBody(c, nil) {
		return
	}
	if err := h.deepAssessments.Delete(c.Param("id")); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "delete_deep_assessment", "删除深度测评", "deep_assessment", c.Param("id"), nil)
	}
	c.JSON(200, gin.H{"deleted": true})
}

func (h *AgentHandler) ControlDeepAssessment(c *gin.Context) {
	var req struct {
		Revision         *int64 `json:"revision"`
		CleanupConfirmed bool   `json:"cleanup_confirmed"`
		Message          string `json:"message"`
		StepID           string `json:"step_id"`
		Reconciled       bool   `json:"reconciled"`
		Priority         int    `json:"priority"`
	}
	if !deepAssessmentBody(c, &req) {
		return
	}
	if req.Revision == nil || *req.Revision < 0 {
		deepAssessmentError(c, fmt.Errorf("revision is required"))
		return
	}
	id, action := c.Param("id"), c.Param("action")
	session, _ := security.CurrentSession(c)
	t, err := h.deepAssessments.Store.Get(id)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	// Old clients used retry for the acknowledgement button. Keep those clicks
	// compatible while treating them as review-only, even on stopped tasks.
	if action == "retry" && (req.Reconciled || req.CleanupConfirmed) {
		if step := t.Step(req.StepID); step != nil {
			if step.Status == "uncertain" && req.Reconciled {
				action = "reconcile"
			}
			if step.Status == "cleanup_unconfirmed" {
				action = "reconcile"
				req.Reconciled = req.Reconciled || req.CleanupConfirmed
			}
		}
	}
	if t.Archived && action != "restore" {
		deepAssessmentError(c, fmt.Errorf("请先恢复归档，再操作测评"))
		return
	}
	automaticPending := deepassessment.Active(t.Status) || t.Status == "recovering" || t.Status == "waiting_retry" || t.Status == "waiting_progress"
	if (action == "guidance" || action == "intent-guidance" || action == "intent-resume") && automaticPending && t.ExecutorID != session.UserID {
		c.JSON(403, gin.H{"error": "请先暂停，再以当前账号继续任务后补充方向"})
		return
	}
	if action == "intent-guidance" && automaticPending {
		if _, err = h.deepAssessmentRuntime.principal(t); err != nil {
			c.JSON(403, gin.H{"error": err.Error()})
			return
		}
	}
	if action == "start" || action == "continue-stopped" || action == "answer" || action == "guidance" {
		if action == "continue-stopped" && !security.SessionHasPermission(c, "agent:execute") {
			c.JSON(403, gin.H{"error": "缺少执行权限"})
			return
		}
		t.ExecutorID = session.UserID
		if _, err = h.deepAssessmentRuntime.principal(t); err != nil {
			c.JSON(403, gin.H{"error": err.Error()})
			return
		}
	}
	if action == "retry" || action == "reconcile" {
		if action == "reconcile" && !req.Reconciled {
			deepAssessmentError(c, fmt.Errorf("请确认已核对调用结果与执行资源状态"))
			return
		}
		if step := t.Step(req.StepID); step != nil && step.Status == "uncertain" && !req.Reconciled {
			deepAssessmentError(c, fmt.Errorf("请核对上次工具回执，并明确确认已处理结果不明的调用"))
			return
		}
		if step := t.Step(req.StepID); step != nil && step.Status == "cleanup_unconfirmed" {
			if !req.CleanupConfirmed {
				deepAssessmentError(c, fmt.Errorf("请先确认上次执行资源已停止"))
				return
			}
		}
		if action == "reconcile" || t.Step(req.StepID) != nil && t.Step(req.StepID).Status == "cleanup_unconfirmed" {
			for _, running := range h.tasks.GetActiveTasks() {
				step := t.Step(req.StepID)
				if step == nil {
					break
				}
				if running.ConversationID == step.ConversationID {
					deepAssessmentError(c, fmt.Errorf("原执行仍在清理，请先在工具监控中确认停止"))
					return
				}
			}
		}
	}
	switch action {
	case "intent-pause", "intent-resume", "intent-priority", "intent-guidance":
		if action == "intent-resume" && automaticPending {
			if _, err = h.deepAssessmentRuntime.principal(t); err != nil {
				c.JSON(403, gin.H{"error": err.Error()})
				return
			}
		}
		err = h.deepAssessments.ControlStepAs(id, req.StepID, strings.TrimPrefix(action, "intent-"), req.Message, req.Priority, *req.Revision, session.UserID)
	case "archive", "restore":
		err = h.deepAssessments.SetArchived(id, *req.Revision, action == "archive")
	case "start":
		err = h.deepAssessments.Start(id, session.UserID, *req.Revision)
	case "continue-stopped":
		err = h.deepAssessments.ContinueStopped(id, session.UserID, *req.Revision)
	case "pause", "cancel":
		err = h.deepAssessments.Stop(id, *req.Revision, action == "cancel")
	case "retry":
		err = h.deepAssessments.Store.RetryStep(id, req.StepID, *req.Revision, req.Reconciled || req.CleanupConfirmed)
	case "reconcile":
		err = h.deepAssessments.Store.ReconcileStep(id, req.StepID, *req.Revision, req.Reconciled)
	case "budget":
		c.JSON(410, gin.H{"error": "深度测评已改为持续执行，不再设置任务预算"})
		return
	case "guidance", "answer":
		err = h.deepAssessments.Store.Change(id, req.StepID, action, req.Message, func(t *deepassessment.Task) error {
			if t.Archived || (action != "guidance" && t.Revision != *req.Revision) || *req.Revision > t.Revision || t.Status == "completed" || t.Status == "cancelled" {
				return deepassessment.ErrConflict
			}
			if action != "guidance" && deepassessment.Active(t.Status) {
				return fmt.Errorf("请先暂停并等待执行收尾")
			}
			switch action {
			case "guidance", "answer":
				if strings.TrimSpace(req.Message) == "" || len(req.Message) > 8000 {
					return fmt.Errorf("补充内容为空或过长")
				}
				if action == "answer" && t.Question == "" {
					return deepassessment.ErrConflict
				}
				t.Guidance = append(t.Guidance, strings.TrimSpace(req.Message))
				t.Summary = "收到补充方向，将在下一轮规划中使用。"
				if action == "answer" {
					t.Question = ""
					t.Status = "paused"
				}

			}
			return nil
		})
	default:
		err = fmt.Errorf("未知操作")
	}
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	t, err = h.deepAssessments.Store.Get(id)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	if action == "guidance" {
		h.deepAssessmentRuntime.steer(id, req.Message)
	}
	response := deepAssessmentView(c, t)
	if action == "continue-stopped" && t.Status == "waiting_input" {
		response["start_error"] = "测评已恢复，待答复：" + t.Question
	}
	if req.StepID != "" {
		response["step"] = t.Step(req.StepID)
	}
	if action == "intent-guidance" {
		delivered, deliveryErr := h.deepAssessmentRuntime.steerStep(id, req.StepID)
		response["guidance_delivered"] = delivered
		if deliveryErr != nil {
			response["guidance_queued"] = true
		}
	}
	if action == "answer" {
		if err = h.deepAssessments.Start(id, session.UserID, t.Revision); err != nil {
			response["start_error"] = err.Error()
		} else {
			t, _ = h.deepAssessments.Store.Get(id)
			response = deepAssessmentView(c, t)
		}
	}
	if h.audit != nil {
		h.audit.RecordOK(c, "task", "deep_assessment_"+action, "更新深度测评", "deep_assessment", id, nil)
	}
	c.JSON(200, response)
}

func (h *AgentHandler) DeepAssessmentReport(c *gin.Context) {
	t, err := h.deepAssessments.Store.Get(c.Param("id"))
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "生成时间：%s\n\n[打开本次测评](%s)\n\n", time.Now().UTC().Format(time.RFC3339), assessmentReportLink(c, t.ID, "", "overview"))
	fmt.Fprintf(&b, "# %s\n\n状态：%s\n\n## 目标\n%s\n\n## 范围\n%s\n\n## 验收条件\n%s\n\n## 当前结论\n%s\n\n## 路线\n%s\n", t.Name, t.Status, t.Objective, t.Scope, t.Criteria, t.Summary, t.Strategy)
	fmt.Fprintf(&b, "\n## 运行记录\n\n类型：长程测评（当前状态见上方）\n\n累计规划：%d\n\n累计 Token：%d\n", t.Rounds, t.TokensUsed)
	if err = h.appendAssessmentCoverageReport(c, t, &b); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if err = h.appendAcceptanceReport(c, t, &b); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if err = h.appendEvidenceComparisonReport(c, t, &b); err != nil {
		deepAssessmentError(c, err)
		return
	}
	if len(t.Guidance) > 0 {
		b.WriteString("\n## 人工补充\n")
		for _, note := range t.Guidance {
			reportLiteral(&b, note)
		}
	}
	for _, s := range t.Steps {
		fmt.Fprintf(&b, "\n## %s · %s [%s]\n\n%s\n\n前置：%s\n\n%s\n\n执行会话：%s\n\n证据回执：%s\n", s.ID, s.Title, s.Status, s.Instruction, strings.Join(s.DependsOn, ", "), s.Summary, s.ConversationID, strings.Join(s.Evidence, ", "))
		priority := s.Priority
		if priority == 0 {
			priority = 100
		}
		fmt.Fprintf(&b, "\n优先级：%d · 用户暂停：%t\n\n[打开来源意图与回执](%s)\n", priority, s.UserPaused, assessmentReportLink(c, t.ID, s.ID, "evidence"))
		if len(s.Guidance) > 0 {
			b.WriteString("\n### 对此意图的人工补充\n")
			for _, note := range s.Guidance {
				reportLiteral(&b, note)
			}
		}
	}
	for offset := 0; ; offset += 20 {
		facts, err := h.deepAssessments.Store.Facts(t.ID, "", offset)
		if err != nil {
			deepAssessmentError(c, err)
			return
		}
		for _, fact := range facts {
			fmt.Fprintf(&b, "\n## 事实 %s · %s [%s]\n\n%s\n\n来源意图：%s\n\n原始回执：%s\n", fact.ID, fact.Summary, fact.Confidence, fact.Body, fact.StepID, strings.Join(fact.Evidence, ", "))
		}
		if len(facts) < 20 {
			break
		}
	}
	if err = h.appendAssessmentFindingsReport(c, t, &b); err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="task-`+t.ID+`.md"`)
	c.Data(200, "text/markdown; charset=utf-8", []byte(b.String()))
}
