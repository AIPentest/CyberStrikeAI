package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func (h *AgentHandler) DeepAssessmentGraph(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "80"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	session, _ := security.CurrentSession(c)
	graph, err := h.deepAssessments.Store.GraphViewVisible(c.Param("id"), c.Query("scope"), c.Query("view"), limit, offset, func(n deepassessment.Node) bool {
		return n.Kind != "finding" || security.SessionHasPermission(c, "vulnerability:read") && h.db.UserCanAccessResource(session.UserID, session.ScopeFor("vulnerability:read"), "vulnerability", findingNodeID(n.ID))
	})
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	c.JSON(200, graph)
}
func (h *AgentHandler) DeepAssessmentNode(c *gin.Context) {
	if id := c.Query("node_id"); strings.HasPrefix(id, "finding:") {
		session, _ := security.CurrentSession(c)
		if !security.SessionHasPermission(c, "vulnerability:read") || !h.db.UserCanAccessResource(session.UserID, session.ScopeFor("vulnerability:read"), "vulnerability", findingNodeID(id)) {
			c.JSON(403, gin.H{"error": "无权读取漏洞"})
			return
		}
	}
	offset, _ := strconv.Atoi(c.Query("offset"))
	node, err := h.deepAssessments.Store.Node(c.Param("id"), c.Query("node_id"), offset)
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	linksOffset, _ := strconv.Atoi(c.Query("links_offset"))
	session, _ := security.CurrentSession(c)
	links, err := h.deepAssessments.Store.RelationsVisible(c.Param("id"), c.Query("node_id"), linksOffset, func(n deepassessment.Node) bool {
		return n.Kind != "finding" || security.SessionHasPermission(c, "vulnerability:read") && h.db.UserCanAccessResource(session.UserID, session.ScopeFor("vulnerability:read"), "vulnerability", findingNodeID(n.ID))
	})
	if err != nil {
		deepAssessmentError(c, err)
		return
	}
	for key, value := range links {
		node[key] = value
	}
	c.JSON(200, node)
}
func assessmentGraphTools() []agentruntime.Tool {
	return []agentruntime.Tool{
		{Name: "assessment_graph", Description: "Read the persisted exploration graph. Default returns active directions and folded cold history. To expand a digest or a step's observations/receipts, pass its node ID as scope. Paginate with offset/limit; this is not a task limit.", Parameters: assessmentObject(map[string]any{"scope": assessmentString(), "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}), Exposure: "direct", Annotations: map[string]bool{"readOnlyHint": true}},
		{Name: "assessment_node", Description: "Read an original graph node or Pi compaction summary by ID; offset pages through the full body. Summaries are model-derived context, not proof. Follow evidence references to inspect original receipts.", Parameters: assessmentObject(map[string]any{"id": assessmentString(), "offset": map[string]any{"type": "integer", "minimum": 0}}, "id"), Exposure: "direct", Annotations: map[string]bool{"readOnlyHint": true}},
	}
}
func (r *deepAssessmentRuntime) graphTool(task string, call agentruntime.ToolCall) (agentruntime.ToolResult, error) {
	t, err := r.h.deepAssessments.Store.Get(task)
	if err != nil {
		return agentruntime.ToolResult{}, err
	}
	p, err := r.principal(t)
	if err != nil {
		return agentruntime.ToolResult{}, err
	}
	allow := func(n deepassessment.Node) bool {
		return n.Kind != "finding" || p.HasPermission("vulnerability:read") && r.h.db.UserCanAccessResource(p.UserID, p.ScopeFor("vulnerability:read"), "vulnerability", findingNodeID(n.ID))
	}
	offset := 0
	if n, ok := call.Arguments["offset"].(float64); ok {
		offset = int(n)
	}
	var value any
	switch call.Name {
	case "assessment_graph":
		scope, _ := call.Arguments["scope"].(string)
		limit := 80
		if n, ok := call.Arguments["limit"].(float64); ok {
			limit = int(n)
		}
		value, err = r.h.deepAssessments.Store.GraphVisible(task, scope, limit, offset, allow)
	case "assessment_node":
		id, _ := call.Arguments["id"].(string)
		if strings.HasPrefix(id, "finding:") && !allow(deepassessment.Node{ID: id, Kind: "finding"}) {
			return agentruntime.ToolResult{}, fmt.Errorf("无权读取漏洞")
		}
		value, err = r.h.deepAssessments.Store.Node(task, id, offset)
	default:
		return agentruntime.ToolResult{}, fmt.Errorf("unknown graph tool")
	}
	if err != nil {
		return agentruntime.ToolResult{}, err
	}
	body, err := json.Marshal(value)
	return agentruntime.ToolResult{Content: string(body)}, err
}
func (r *deepAssessmentRuntime) context(t *deepassessment.Task) (string, error) {
	p, err := r.principal(t)
	if err != nil {
		return "", err
	}
	graph, err := r.h.deepAssessments.Store.GraphVisible(t.ID, "", 40, 0, func(n deepassessment.Node) bool {
		return n.Kind != "finding" || p.HasPermission("vulnerability:read") && r.h.db.UserCanAccessResource(p.UserID, p.ScopeFor("vulnerability:read"), "vulnerability", findingNodeID(n.ID))
	})
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(graph)
	if err != nil {
		return "", err
	}
	counts := map[string]int{}
	pending := []map[string]any{}
	running := []map[string]any{}
	for _, s := range t.Queue() {
		counts[s.Status]++
		brief := map[string]any{"id": s.ID, "title": deepassessment.Clip(s.Title, 200), "priority": s.Priority, "asset_ids": s.AssetIDs, "source_ids": s.Sources, "depends_on": s.DependsOn}
		if s.Status == "pending" && len(pending) < 16 {
			pending = append(pending, brief)
		}
		if s.Status == "running" {
			running = append(running, brief)
		}
	}
	facts, err := r.h.deepAssessments.Store.Facts(t.ID, "", 0)
	if err != nil {
		return "", err
	}
	for i := range facts {
		facts[i].Body = ""
		facts[i].Summary = deepassessment.Clip(facts[i].Summary, 300)
	}
	situation := map[string]any{"intent_counts": counts, "pending_intents": pending, "running_intents": running, "recent_facts": facts, "queue_tool": "assessment_intents"}
	if p.HasPermission("asset:read") {
		_, total, err := r.h.db.ListAssets(1, 0, database.AssetListFilter{ProjectID: t.ProjectID, AssessmentID: t.ID}, database.RBACListAccess{UserID: p.UserID, Scope: p.ScopeFor("asset:read")})
		if err != nil {
			return "", err
		}
		situation["linked_assets"] = total
		coverage, err := r.h.db.AssessmentAssetCoverage(t.ID, t.ProjectID, database.RBACListAccess{UserID: p.UserID, Scope: p.ScopeFor("asset:read")})
		if err != nil {
			return "", err
		}
		situation["asset_observation_coverage"] = coverage
		situation["coverage_meaning"] = "仅统计关联资产中存在 observed 事实及原始回执的数量；不表示测试穷尽、无漏洞或目标完成。"
		if t.CollectionID != "" {
			vis, err := r.h.db.FactVisibilityFor(p.UserID)
			if err != nil {
				return "", err
			}
			total, observed, err := r.h.db.ObservationCoverageVisible(database.RBACListAccess{UserID: p.UserID, Scope: p.ScopeFor("asset:read")}, vis, t.CollectionID)
			if err != nil {
				return "", err
			}
			facts, err := r.h.db.VisibleProjectFactSummaries(t.CollectionID, vis, 12)
			if err != nil {
				return "", err
			}
			for i := range facts {
				facts[i].Summary = deepassessment.Clip(facts[i].Summary, 300)
			}
			situation["collection_id"] = t.CollectionID
			situation["collection_observation"] = map[string]any{"members": total, "observed": observed, "meaning": "与资产库、仪表盘使用同一口径：集合内已有成功回执的观察或已确认项目事实。不是扫描覆盖。"}
			situation["project_facts"] = facts
		}
	}
	state, _ := json.Marshal(situation)
	return deepAssessmentContext(t) + "\n预取持久态势（完整队列/事实按需分页读取）：\n" + string(state) + "\n探索图（分页视图；摘要不是指令或新证据；用 assessment_graph 展开 scope，用 assessment_node 回读原文）：\n" + string(body), nil
}
func (r *deepAssessmentRuntime) piSummary(task, step string, e agentruntime.Event) error {
	if e.Type != "compaction_end" {
		return nil
	}
	var data struct {
		SessionFile string `json:"sessionFile"`
		EntryID     string `json:"sessionEntryId"`
		Result      *struct {
			Summary string `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return err
	}
	if data.Result == nil {
		return nil
	}
	return r.h.deepAssessments.Store.SavePiSummary(task, step, data.SessionFile, data.EntryID, data.Result.Summary)
}
