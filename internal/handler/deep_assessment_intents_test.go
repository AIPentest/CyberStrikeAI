package handler

import (
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"encoding/json"
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
