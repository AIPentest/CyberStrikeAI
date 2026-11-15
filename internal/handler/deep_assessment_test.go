package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/deepassessment"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func deepAssessmentFixture(t *testing.T) (*AgentHandler, *deepassessment.Task) {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "tasks.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.BootstrapRBAC("fixture-password-hash", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	h := &AgentHandler{db: db, config: cfg, logger: zap.NewNop(), tasks: NewAgentTaskManager(), taskEventBus: NewTaskEventBus(), hitlManager: NewHITLManager(db, zap.NewNop())}
	if err = h.hitlManager.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if err = h.InitDeepAssessments(func() (*config.Config, error) { return cfg, nil }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.deepAssessments.Close)
	now := time.Now().UTC()
	task := &deepassessment.Task{ID: "task", OwnerID: user.ID, ExecutorID: user.ID, Name: "fixture", Objective: "Inspect the local fixture", Scope: "Local only", Criteria: "Record observed fixture result", Concurrency: 1, MaxRounds: 10, RemainingSeconds: 90, Status: "draft", Steps: []deepassessment.Step{}, Guidance: []string{}, CreatedAt: now, UpdatedAt: now}
	if err = h.deepAssessments.Store.Create(task); err != nil {
		t.Fatal(err)
	}
	return h, task
}
func assessmentRouter(h *AgentHandler, user string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{UserID: user, Scope: database.RBACScopeOwn, Permissions: map[string]bool{"assessments:read": true, "assessments:write": true, "assessments:delete": true}})
	})
	r.Use(security.RBACMiddleware(h.db))
	r.GET("/api/deep-assessments", h.ListDeepAssessments)
	r.GET("/api/deep-assessments/:id", h.GetDeepAssessment)
	r.GET("/api/deep-assessments/:id/events", h.DeepAssessmentEvents)
	r.GET("/api/deep-assessments/:id/receipts", h.DeepAssessmentReceipts)
	r.GET("/api/deep-assessments/:id/graph", h.DeepAssessmentGraph)
	r.GET("/api/deep-assessments/:id/graph/node", h.DeepAssessmentNode)
	r.GET("/api/deep-assessments/:id/report", h.DeepAssessmentReport)
	r.POST("/api/deep-assessments/:id/control/:action", h.ControlDeepAssessment)
	r.DELETE("/api/deep-assessments/:id", h.DeleteDeepAssessment)
	r.POST("/api/deep-assessments", h.CreateDeepAssessment)
	return r
}
func TestDeepAssessmentAccessRevisionAndOrigin(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	other, err := h.db.CreateRBACUser("other", "Other", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign := assessmentRouter(h, other.ID)
	for _, path := range []string{"/api/deep-assessments/task", "/api/deep-assessments/task/events", "/api/deep-assessments/task/report", "/api/deep-assessments/task/receipts", "/api/deep-assessments/task/graph", "/api/deep-assessments/task/graph/node?node_id=goal"} {
		w := httptest.NewRecorder()
		foreign.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 403 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	foreign.ServeHTTP(w, httptest.NewRequest("GET", "/api/deep-assessments", nil))
	if !strings.Contains(w.Body.String(), `"tasks":[]`) {
		t.Fatal(w.Body.String())
	}
	owned := assessmentRouter(h, task.OwnerID)
	req := httptest.NewRequest("POST", "/api/deep-assessments/task/control/guidance", strings.NewReader(`{"revision":99,"message":"change direction"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	owned.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	req = httptest.NewRequest("POST", "/api/deep-assessments/task/control/guidance", strings.NewReader(`{"revision":0,"message":"change direction"}`))
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: "fixture"})
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	owned.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := h.deepAssessments.Store.Get(task.ID)
	if got.Revision != 0 || len(got.Guidance) != 0 {
		t.Fatal("rejected writes changed state")
	}
	req = httptest.NewRequest("POST", "/api/deep-assessments/task/control/guidance", strings.NewReader(`{"revision":0,"message":"use supplied evidence"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	owned.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ = h.deepAssessments.Store.Get(task.ID)
	if len(got.Guidance) != 1 {
		t.Fatal(got)
	}
}

func TestDeepAssessmentMemoryKeepsTaskBoundaryAndPriorAttempts(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	var ids []string
	for _, name := range []string{"attempt-one", "attempt-two", "foreign"} {
		conv, err := h.db.CreateConversation(name, database.ConversationCreateMeta{})
		if err != nil {
			t.Fatal(err)
		}
		msg, err := h.db.AddMessage(conv.ID, "assistant", "fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = h.db.AddProcessDetail(msg.ID, conv.ID, "tool_result", name+" needle", nil); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, conv.ID)
	}
	h.deepAssessments.Store.Change(task.ID, "", "", "", func(t *deepassessment.Task) error {
		t.Steps = []deepassessment.Step{{ID: "s1", ConversationID: ids[1], Conversations: ids[:2], Summary: "observed needle"}}
		return nil
	})
	result, err := h.deepAssessmentRuntime.memory(task.ID, map[string]any{"query": "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "attempt-one") || !strings.Contains(result.Content, "attempt-two") || strings.Contains(result.Content, "foreign") {
		t.Fatal(result.Content)
	}
	var oldRef string
	if err = h.db.QueryRow(`SELECT id FROM process_details WHERE conversation_id=? LIMIT 1`, ids[0]).Scan(&oldRef); err != nil {
		t.Fatal(err)
	}
	prior, err := h.deepAssessmentRuntime.trace(task.ID, map[string]any{"trace_id": oldRef})
	if err != nil || !strings.Contains(prior.Content, `"step_status":"previous_attempt"`) {
		t.Fatalf("old attempt was promoted: %s %v", prior.Content, err)
	}
	result, err = h.deepAssessmentRuntime.memory(task.ID, map[string]any{"query": "needle", "step_id": "not-in-task"})
	if err != nil || strings.Contains(result.Content, "attempt-one") {
		t.Fatal(result, err)
	}
}

func deepAssessmentReply(w http.ResponseWriter, tool, id string, args map[string]any, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := map[string]any{"role": "assistant"}
	reason := "stop"
	if tool != "" {
		b, _ := json.Marshal(args)
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": tool, "arguments": string(b)}}}
		reason = "tool_calls"
	} else {
		delta["content"] = text
	}
	for _, choice := range []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": reason}} {
		b, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "model": "fixture", "choices": []any{choice}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func TestDeepAssessmentFullTraceAndTailSearch(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	var refs, convs []string
	for _, name := range []string{"owned", "foreign"} {
		conv, _ := h.db.CreateConversation(name, database.ConversationCreateMeta{})
		msg, _ := h.db.AddMessage(conv.ID, "assistant", "result", nil)
		ref, err := h.db.AddProcessDetailWithID(msg.ID, conv.ID, "tool_result", strings.Repeat("prefix ", 4000)+"tail-evidence", nil)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
		convs = append(convs, conv.ID)
	}
	if err := h.deepAssessments.Store.Change(task.ID, "", "", "", func(t *deepassessment.Task) error {
		t.Steps = []deepassessment.Step{{ID: "s1", Status: "completed", ConversationID: convs[0], Summary: "summary"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	found, err := h.deepAssessmentRuntime.memory(task.ID, map[string]any{"query": "tail-evidence"})
	if err != nil || !strings.Contains(found.Content, "tail-evidence") || strings.Contains(found.Content, refs[1]) {
		t.Fatalf("tail search: %s %v", found.Content, err)
	}
	trace, err := h.deepAssessmentRuntime.trace(task.ID, map[string]any{"trace_id": refs[0], "offset": float64(28000)})
	if err != nil || !strings.Contains(trace.Content, "tail-evidence") {
		t.Fatalf("full trace: %s %v", trace.Content, err)
	}
	if _, err = h.deepAssessmentRuntime.trace(task.ID, map[string]any{"trace_id": refs[1]}); err == nil {
		t.Fatal("foreign trace exposed")
	}
}

func TestDeepAssessmentCreateOnlySavesDraftAndRejectsOversizedBody(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	router := assessmentRouter(h, task.OwnerID)
	body := `{"name":"new","objective":"inspect fixture","scope":"local only","criteria":"observed evidence"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/deep-assessments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"status":"draft"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved struct {
		Task deepassessment.Task `json:"task"`
	}
	json.Unmarshal(w.Body.Bytes(), &saved)
	if !saved.Task.AutoResume || saved.Task.RemainingSeconds != 0 || saved.Task.TokenBudget != 0 || saved.Task.MaxRounds != 0 || saved.Task.Rounds != 0 || !h.db.UserCanAccessResource(task.OwnerID, database.RBACScopeAssigned, "deep_assessment", saved.Task.ID) {
		t.Fatal("draft executed or creator lost access")
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/deep-assessments", strings.NewReader(`{"objective":"`+strings.Repeat("x", 70<<10)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDeepAssessmentOptionalFieldsInheritObjective(t *testing.T) {
	h, owner := deepAssessmentFixture(t)
	router := assessmentRouter(h, owner.OwnerID)
	for _, tc := range []struct {
		name, objective         string
		extra                   map[string]string
		wantScope, wantCriteria string
		wantStatus              int
	}{
		{"omitted", "  inspect fixture  ", nil, "inspect fixture", "inspect fixture", 201},
		{"whitespace", "inspect fixture", map[string]string{"scope": " \n\t ", "criteria": " \t "}, "inspect fixture", "inspect fixture", 201},
		{"custom scope", "inspect fixture", map[string]string{"scope": " exclude payments "}, "exclude payments", "inspect fixture", 201},
		{"custom criteria", "inspect fixture", map[string]string{"criteria": " report evidence "}, "inspect fixture", "report evidence", 201},
		{"both supplied", "inspect fixture", map[string]string{"scope": " local only ", "criteria": " original evidence "}, "local only", "original evidence", 201},
		{"long inherited goal", strings.Repeat("目", 9000), nil, strings.Repeat("目", 9000), strings.Repeat("目", 9000), 201},
		{"missing goal", "", nil, "", "", 400},
		{"explicit criteria too long", "inspect fixture", map[string]string{"criteria": strings.Repeat("x", 8001)}, "", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"name": "optional " + tc.name, "objective": tc.objective}
			for k, v := range tc.extra {
				values[k] = v
			}
			body, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/api/deep-assessments", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d", w.Code, tc.wantStatus)
			}
			if w.Code != 201 {
				return
			}
			var result struct {
				Task deepassessment.Task `json:"task"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			saved, err := h.deepAssessments.Store.Get(result.Task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Scope != tc.wantScope || saved.Criteria != tc.wantCriteria || saved.Objective != strings.TrimSpace(tc.objective) {
				t.Fatal("optional fields were not inherited or explicit values were changed")
			}
			if saved.Status != "draft" || saved.Rounds != 0 || len(saved.Steps) != 0 {
				t.Fatal("creating an optional-fields draft started execution")
			}
		})
	}
}

func TestDeepAssessmentModelRetryClassification(t *testing.T) {
	for _, text := range []string{"429 rate limit", "502 upstream", "connection reset"} {
		if !transientDeepAssessmentModelError(fmt.Errorf("%s", text)) {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"401 invalid api key", "403 forbidden", "unknown model", "policy denied"} {
		if transientDeepAssessmentModelError(fmt.Errorf("%s", text)) {
			t.Fatal(text)
		}
	}
	var p deepassessment.Proposal
	if decodeDeepAssessmentJSON("{} trailing", &p) == nil || decodeDeepAssessmentJSON(`{"unknown":true}`, &p) == nil {
		t.Fatal("invalid proposal decoded")
	}
}

func TestDeepAssessmentControlRequiresRevision(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/deep-assessments/task/control/pause", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	assessmentRouter(h, task.OwnerID).ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := h.deepAssessments.Store.Get(task.ID)
	if got.Status != "draft" {
		t.Fatal(got)
	}
}
func TestDeepAssessmentJSONRepairCannotRetainPartialFields(t *testing.T) {
	var p deepassessment.Proposal
	if decodeDeepAssessmentJSON(`{"strategy":"should not persist","unknown":true}`, &p) == nil {
		t.Fatal("unknown field accepted")
	}
	if p.Strategy != "" {
		t.Fatal("partially decoded state escaped")
	}
}

func TestDeepAssessmentLegacyBudgetInputDoesNotCreateLimits(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	router := assessmentRouter(h, task.OwnerID)
	body := `{"name":"continuous","objective":"read fixtures","scope":"local","criteria":"verified evidence","remaining_seconds":1,"token_budget":1,"max_rounds":1}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/deep-assessments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, field := range []string{`"remaining_seconds"`, `"token_budget"`, `"max_rounds"`, `"deadline"`} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatal("cap exposed", field, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/deep-assessments/task/control/budget", strings.NewReader(`{"revision":0,"remaining_seconds":1,"max_rounds":1,"token_budget":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != 410 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDeepAssessmentNativeCompactionFeedsGraphMemory(t *testing.T) {
	h, task := deepAssessmentFixture(t)
	event := agentruntime.Event{Type: "compaction_end", Data: json.RawMessage(`{"sessionFile":"fixture-native-session","sessionEntryId":"entry-1","result":{"summary":"original Pi continuation context"}}`)}
	if err := h.deepAssessmentRuntime.piSummary(task.ID, "planner", event); err != nil {
		t.Fatal(err)
	}
	result, err := h.deepAssessmentRuntime.memory(task.ID, map[string]any{"query": "continuation"})
	if err != nil || !strings.Contains(result.Content, "original Pi continuation context") || !strings.Contains(result.Content, "pi_compaction") {
		t.Fatal(result, err)
	}
}
