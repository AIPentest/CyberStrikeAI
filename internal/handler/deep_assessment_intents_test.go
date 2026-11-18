package handler

import (
	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func explorationRouter(h *AgentHandler, user string, perms map[string]bool) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{UserID: user, Scope: database.RBACScopeOwn, Permissions: perms})
	})
	r.Use(security.RBACMiddleware(h.db))
	r.GET("/api/deep-assessments/:id/assets", h.DeepAssessmentAssets)
	r.POST("/api/deep-assessments/:id/assets", h.DeepAssessmentAssets)
	r.GET("/api/deep-assessments/:id/facts", h.DeepAssessmentFacts)
	r.GET("/api/deep-assessments/:id/intents", h.DeepAssessmentIntents)
	r.GET("/api/deep-assessments/:id/graph", h.DeepAssessmentGraph)
	r.GET("/api/deep-assessments/:id/graph/node", h.DeepAssessmentNode)
	r.GET("/api/deep-assessments/:id", h.GetDeepAssessment)
	return r
}

func TestDeepAssessmentAssetEvidenceReview(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	store := h.deepAssessments.Store
	project, err := h.db.CreateProject(&database.Project{Name: "Fixture project"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Change(task.ID, "", "fixture", "", func(v *deepassessment.Task) error {
		v.ProjectID = project.ID
		v.Steps = []deepassessment.Step{{ID: "one", Status: "running"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	other, err := h.db.CreateRBACUser("coverage-other", "Other", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	assets := make([]*database.Asset, 22)
	for i := range assets {
		assets[i] = &database.Asset{ProjectID: project.ID, Host: fmt.Sprintf("https://coverage-%02d.example.test", i)}
	}
	unlinked := &database.Asset{ProjectID: project.ID, Host: "https://unlinked.example.test"}
	outside := &database.Asset{Host: "https://outside.example.test"}
	private := &database.Asset{ProjectID: project.ID, Host: "https://private.example.test"}
	if _, err := h.db.UpsertAssets(append(assets, unlinked, outside), task.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.UpsertAssets([]*database.Asset{private}, other.ID); err != nil {
		t.Fatal(err)
	}
	linked := []string{outside.ID, private.ID}
	for _, asset := range assets {
		linked = append(linked, asset.ID)
	}
	if err = store.BindAssets(task.ID, linked); err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCall{ID: "fixture-proof", Name: "fixture_read"}
	if err = store.BeginCall(task.ID, "one", call); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishCall(call, agentruntime.ToolResult{Content: "fixture bytes"}, false); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []deepassessment.Fact{
		{Confidence: "observed", Summary: "Observed fixture", AssetIDs: []string{assets[0].ID}},
		{Confidence: "observed", Summary: "Second fact", AssetIDs: []string{assets[0].ID}},
		{Confidence: "inferred", Summary: "Inferred only", AssetIDs: []string{assets[1].ID}},
		{Confidence: "observed", Summary: "Private or outside", AssetIDs: []string{private.ID, outside.ID}},
	} {
		fact.StepID, fact.Evidence = "one", []string{call.ID}
		if err = store.AddFact(task.ID, &fact); err != nil {
			t.Fatal(err)
		}
	}
	router := explorationRouter(h, task.OwnerID, map[string]bool{"assessments:read": true, "asset:read": true})
	router.GET("/api/deep-assessments/:id/report", h.DeepAssessmentReport)
	get := func(r *gin.Engine, path string, code int) string {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/"+path, nil))
		if w.Code != code {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	for _, check := range []struct {
		query       string
		total, page int
	}{
		{"observation=observed", 1, 1},
		{"observation=missing", 21, 20},
		{"observation=missing&offset=20", 21, 1},
		{"observation=missing&q=coverage-01", 1, 1},
		{"observation=missing&q=coverage-00", 0, 0},
	} {
		var graph database.AssetGraph
		body := get(router, "assets?"+check.query, 200)
		if err = json.Unmarshal([]byte(body), &graph); err != nil {
			t.Fatal(err)
		}
		if graph.Total != check.total || len(graph.Assets) != check.page ||
			graph.Coverage["linked"] != 22 || graph.Coverage["with_observed_evidence"] != 1 {
			t.Fatalf("%s: wrong coverage or page: total=%d page=%d %v", check.query, graph.Total, len(graph.Assets), graph.Coverage)
		}
	}
	get(router, "assets?scope=all&observation=missing", 400)
	get(router, "assets?observation=invalid", 400)
	body := get(router, "facts?confidence=observed&asset_id="+assets[0].ID, 200)
	var facts struct {
		Facts []deepassessment.Fact
	}
	if err = json.Unmarshal([]byte(body), &facts); err != nil || len(facts.Facts) != 2 || len(facts.Facts[0].Evidence) != 1 || facts.Facts[0].Evidence[0] != call.ID {
		t.Fatal(body, err)
	}
	if body = get(router, "facts?confidence=observed&asset_id="+assets[1].ID, 200); !strings.Contains(body, `"facts":[]`) {
		t.Fatal(body)
	}
	for _, asset := range []*database.Asset{private, outside, unlinked} {
		get(router, "facts?asset_id="+asset.ID, 404)
	}
	if body = get(router, "graph/node?node_id=receipt:"+call.ID, 200); !strings.Contains(body, "fixture bytes") {
		t.Fatal(body)
	}
	if body = get(router, "report", 200); !strings.Contains(body, "暂无观察事实 21 项") || !strings.Contains(body, "tab=assets&observation=missing") {
		t.Fatal(body)
	}
	reader := explorationRouter(h, task.OwnerID, map[string]bool{"assessments:read": true})
	reader.GET("/api/deep-assessments/:id/report", h.DeepAssessmentReport)
	get(reader, "facts?asset_id="+assets[0].ID, 403)
	if body = get(reader, "report", 200); strings.Contains(body, "## 资产观察证据") {
		t.Fatal("coverage was exported without asset read permission")
	}
}
func TestDeepAssessmentIntentAssetAndFindingGraphPermissions(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	other, _ := h.db.CreateRBACUser("asset-other", "other", "hash", true, nil)
	owned := &database.Asset{Host: "https://owned.example.test"}
	foreign := &database.Asset{Host: "https://secret.example.test"}
	h.db.UpsertAssets([]*database.Asset{owned}, task.OwnerID)
	h.db.UpsertAssets([]*database.Asset{foreign}, other.ID)
	p := map[string]bool{"assessments:read": true, "assessments:write": true, "asset:read": true}
	router := explorationRouter(h, task.OwnerID, p)
	post := func(id string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/deep-assessments/task/assets", strings.NewReader(`{"asset_ids":["`+id+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer fixture")
		router.ServeHTTP(w, req)
		return w.Code
	}
	if code := post(foreign.ID); code != 400 {
		t.Fatal("foreign anchor accepted", code)
	}
	if code := post(owned.ID); code != 200 {
		t.Fatal(code)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/assets?scope=all", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret.example.test") {
		t.Fatal(w.Code, w.Body.String())
	}
	conv, _ := h.db.CreateConversation("private finding", database.ConversationCreateMeta{})
	h.deepAssessments.Store.Change(task.ID, "", "plan_updated", "", func(v *deepassessment.Task) error {
		v.Steps = []deepassessment.Step{{ID: "one", Title: "intent", Status: "running", ConversationID: conv.ID, Priority: 3}, {ID: "two", Title: "first", Status: "pending", Priority: 1}}
		return nil
	})
	v, err := h.db.CreateVulnerability(&database.Vulnerability{ConversationID: conv.ID, Title: "private finding title", Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/graph", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "private finding title") {
		t.Fatal("graph leaked finding", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/graph/node?node_id=finding:"+v.ID, nil))
	if w.Code != 403 {
		t.Fatal("node bypassed finding access", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task?step_limit=50&step_order=queue&step_status=pending", nil))
	var view struct {
		Task deepassessment.Task `json:"task"`
	}
	json.Unmarshal(w.Body.Bytes(), &view)
	if w.Code != 200 || len(view.Task.Steps) != 1 || view.Task.Steps[0].ID != "two" {
		t.Fatal(w.Code, w.Body.String())
	}
	reader := explorationRouter(h, task.OwnerID, map[string]bool{"assessments:read": true})
	w = httptest.NewRecorder()
	reader.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/assets", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	unrelated := explorationRouter(h, other.ID, p)
	for _, endpoint := range []string{"assets", "facts", "intents", "graph"} {
		w = httptest.NewRecorder()
		unrelated.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments/task/"+endpoint, nil))
		if w.Code != 403 {
			t.Fatal(endpoint, w.Code)
		}
	}
}
