package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
)

func TestAssessmentQuestionReadsCurrentTaskWithoutChangingIt(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	project, err := h.db.CreateProject(&database.Project{Name: "Inventory"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.SetResourceOwner("project", project.ID, task.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.UpsertAssets([]*database.Asset{{ProjectID: project.ID, Host: "https://inventory.example.test/records?ref=private-query"}}, task.OwnerID, true); err != nil {
		t.Fatal(err)
	}
	requests := make(chan map[string]any, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"任务仍在运行"}}]}`))
	}))
	defer provider.Close()
	h.config.AI.Channels = map[string]config.AIChannelConfig{
		"review": {Model: "fixture-model", BaseURL: provider.URL, APIKey: "fixture"},
	}
	if err := h.deepAssessments.Store.Change(task.ID, "", "", "", func(saved *deepassessment.Task) error {
		saved.Status, saved.Model, saved.ProjectID = "running", "review", project.ID
		saved.Summary = "fixture initial progress"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	permissions := map[string]bool{"assessments:read": true, "agent:execute": true, "project:read": true, "asset:read": true}
	actor := task.OwnerID
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{UserID: actor, Scope: database.RBACScopeOwn, Permissions: permissions})
	})
	r.Use(security.RBACMiddleware(h.db))
	r.POST("/api/deep-assessments/:id/question", security.RequirePermission("agent:execute"), h.AskDeepAssessment)
	ask := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/deep-assessments/task/question", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(response, req)
		return response
	}
	input := `{"question":"当前进展？","history":[{"question":"之前呢","answer":"fixture previous turn"}]}`
	for _, canReadAssets := range []bool{true, false} {
		permissions["asset:read"] = canReadAssets
		before, err := h.deepAssessments.Store.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := ask(input)
		if got.Code != 200 || !strings.Contains(got.Body.String(), "任务仍在运行") {
			t.Fatal(got.Code, got.Body.String())
		}
		payload := <-requests
		wire, _ := json.Marshal(payload["messages"])
		if strings.Contains(string(wire), "private-query") || payload["tools"] != nil || payload["model"] != "fixture-model" || !strings.Contains(string(wire), before.Summary) ||
			!strings.Contains(string(wire), "fixture previous turn") || strings.Contains(string(wire), "inventory.example.test") != canReadAssets {
			t.Fatalf("unexpected snapshot: %s", wire)
		}
		after, err := h.deepAssessments.Store.Get(task.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("question changed running task: %v", err)
		}
		if err = h.deepAssessments.Store.Change(task.ID, "", "", "", func(saved *deepassessment.Task) error {
			saved.Summary = "fixture latest progress"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := ask(`{"question":" "}`); got.Code != 400 {
		t.Fatal(got.Code)
	}
	permissions["agent:execute"] = false
	if got := ask(input); got.Code != 403 {
		t.Fatal(got.Code)
	}
	permissions["agent:execute"] = true
	actor = "other"
	if got := ask(input); got.Code != 403 {
		t.Fatal(got.Code)
	}
	if len(requests) != 0 {
		t.Fatal("denied question reached the model")
	}
}
