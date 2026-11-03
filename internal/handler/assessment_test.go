package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/assessment"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type assessmentAPIRuntime struct{}

func (assessmentAPIRuntime) Context(ctx context.Context, _ *assessment.Task) (context.Context, error) {
	return ctx, nil
}
func (assessmentAPIRuntime) Tools(*assessment.Task) ([]agent.Tool, error) {
	return []agent.Tool{{Function: agent.FunctionDefinition{Name: "inspect"}}}, nil
}
func (assessmentAPIRuntime) Complete(context.Context, *assessment.Task, []agent.ChatMessage, []agent.Tool, int) (*assessment.ModelReply, error) {
	return nil, context.Canceled
}
func (assessmentAPIRuntime) Execute(context.Context, *assessment.Task, string, agent.ToolCall) (*agent.ToolExecutionResult, error) {
	return nil, context.Canceled
}
func assessmentAPI(t *testing.T) (*AssessmentHandler, *security.Session, func(string, string, string) *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "api.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err = db.BootstrapRBAC("fixture-password-hash", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	e := assessment.NewEngine(&assessment.Store{DB: db}, assessmentAPIRuntime{}, nil)
	t.Cleanup(func() { e.Close(); db.Close() })
	h := &AssessmentHandler{Engine: e, DB: db, Config: &config.Config{OpenAI: config.OpenAIConfig{Model: "fixture-model"}}}
	session := &security.Session{UserID: user.ID, Username: "admin", Scope: database.RBACScopeAll, Permissions: map[string]bool{"assessment:read": true, "assessment:write": true, "assessment:delete": true, "agent:execute": true, "hitl:write": true, "project:read": true, "project:write": true, "vulnerability:write": true}}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(security.ContextSessionKey, *session); c.Next() })
	router.Use(security.RBACMiddleware(db))
	router.POST("/api/assessments", h.Create)
	router.GET("/api/assessments", h.List)
	router.GET("/api/assessments/:id", h.Get)
	router.GET("/api/assessments/:id/events", h.Events)
	router.GET("/api/assessments/:id/report", h.Report)
	router.POST("/api/assessments/:id/control/:action", h.Control)
	router.POST("/api/assessments/:id/steps/:stepId/decision", h.Decision)
	router.POST("/api/assessments/:id/publish", h.Publish)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	return h, session, request
}
func createAPIAssessment(t *testing.T, req func(string, string, string) *httptest.ResponseRecorder) *assessment.Task {
	t.Helper()
	w := req("POST", "/api/assessments", `{"name":"fixture","goal":"inspect fixture","scope":"local fixture only","tools":["inspect"],"owner_id":"forged","status":"completed","tokens":-500}`)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *assessment.Task `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Task
}
func TestAssessmentAPIRejectsForgedStateAndCrossOwnerAccess(t *testing.T) {
	h, session, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	if task.OwnerID != session.UserID || task.Status != "draft" || task.Tokens != 0 {
		t.Fatalf("client forged state: %+v", task)
	}
	// Creator using assigned scope still sees the created resource.
	session.Scope = database.RBACScopeAssigned
	if w := request("GET", "/api/assessments/"+task.ID, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	session.UserID = "other"
	session.Scope = database.RBACScopeOwn
	for _, suffix := range []string{"", "/events", "/report"} {
		if w := request("GET", "/api/assessments/"+task.ID+suffix, ""); w.Code != 403 {
			t.Fatalf("cross-owner %s: %d", suffix, w.Code)
		}
	}
	w := request("GET", "/api/assessments", "")
	if strings.Contains(w.Body.String(), task.ID) {
		t.Fatal("cross-owner list leak")
	}
	var count int
	h.DB.QueryRow(`SELECT count(*) FROM conversations`).Scan(&count)
	if count != 0 {
		t.Fatal("created hidden chat")
	}
}
func TestAssessmentAPIEnforcesExecuteAndReviewPermissions(t *testing.T) {
	h, session, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	delete(session.Permissions, "agent:execute")
	w := request("POST", "/api/assessments/"+task.ID+"/control/start", `{}`)
	if w.Code != 403 {
		t.Fatalf("execute missing: %d", w.Code)
	}
	if err := h.Engine.Store.Change(task.ID, "", "", "", func(t *assessment.Task) error {
		t.Steps = []*assessment.Step{{ID: "step", Status: "awaiting_approval", Pending: &agent.ToolCall{ID: "call"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	delete(session.Permissions, "hitl:write")
	w = request("POST", "/api/assessments/"+task.ID+"/steps/step/decision", `{"approved":true}`)
	if w.Code != 403 {
		t.Fatalf("review missing: %d", w.Code)
	}
	session.Permissions["hitl:write"] = true
	w = request("POST", "/api/assessments/"+task.ID+"/steps/step/decision", `{}`)
	if w.Code != 400 {
		t.Fatalf("missing decision accepted: %d", w.Code)
	}
	delete(session.Permissions, "assessment:write")
	w = request("POST", "/api/assessments/"+task.ID+"/steps/step/decision", `{"approved":true}`)
	if w.Code != 403 {
		t.Fatalf("assessment write missing: %d", w.Code)
	}
}
func TestAssessmentAPIPublishEvidenceWithNoConversationAndStableIdentity(t *testing.T) {
	h, session, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	project, err := h.DB.CreateProject(&database.Project{Name: "fixture project"})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Engine.Store.Change(task.ID, "", "", "", func(t *assessment.Task) error {
		t.ProjectID = project.ID
		t.Steps = []*assessment.Step{{ID: "step", Status: "completed", Evidence: []assessment.Evidence{{Kind: "finding", Title: "Candidate", Detail: "Observed fixture", ExecutionIDs: []string{"exec-fixture"}}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		for _, kind := range []string{"fact", "vulnerability"} {
			w := publishFixture(t, h, request, task.ID, `{"step_id":"step","index":0,"kind":"`+kind+`"}`)
			if w.Code != 200 {
				t.Fatalf("publish %s: %d %s", kind, w.Code, w.Body.String())
			}
		}
	}
	var count int
	if err = h.DB.QueryRow(`SELECT count(*) FROM vulnerabilities WHERE project_id=?`, project.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate vulnerability", count, err)
	}
	h.DB.QueryRow(`SELECT count(*) FROM conversations`).Scan(&count)
	if count != 0 {
		t.Fatal("publishing created hidden chat")
	}
	// A global project read role must not widen a restricted project write role.
	session.PermissionScopes = map[string]string{"assessment:write": database.RBACScopeAll, "project:read": database.RBACScopeAll, "project:write": database.RBACScopeOwn}
	w := publishFixture(t, h, request, task.ID, `{"step_id":"step","index":0,"kind":"fact"}`)
	if w.Code != 403 {
		t.Fatalf("project scope widening: %d", w.Code)
	}
}
func TestAssessmentAPIRejectsUnknownChannelAndTool(t *testing.T) {
	_, _, request := assessmentAPI(t)
	for _, extra := range []string{`"model":"missing","tools":["inspect"]`, `"tools":["forbidden"]`, `"approval_mode":"bypass","tools":["inspect"]`} {
		w := request(http.MethodPost, "/api/assessments", `{"name":"fixture","goal":"inspect","scope":"fixture",`+extra+`}`)
		if w.Code != 400 {
			t.Fatalf("invalid create: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAssessmentAPIPublishesObservedAsset(t *testing.T) {
	h, session, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	session.Permissions["asset:write"] = true
	if err := h.Engine.Store.Change(task.ID, "", "", "", func(t *assessment.Task) error {
		t.Steps = []*assessment.Step{{ID: "step", Evidence: []assessment.Evidence{{Kind: "asset", Title: "Fixture service", Detail: "Observed local fixture service", ExecutionIDs: []string{"exec-fixture"}, Asset: &assessment.ObservedAsset{Host: "https://fixture.example.test", Domain: "fixture.example.test", Port: 443, Protocol: "https"}}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w := publishFixture(t, h, request, task.ID, `{"step_id":"step","index":0,"kind":"asset"}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var count int
	if err := h.DB.QueryRow(`SELECT count(*) FROM assets WHERE source='assessment' AND owner_user_id=?`, session.UserID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("asset duplicate/missing: %d %v", count, err)
	}
	delete(session.Permissions, "asset:write")
	w := publishFixture(t, h, request, task.ID, `{"step_id":"step","index":0,"kind":"asset"}`)
	if w.Code != 403 {
		t.Fatal("asset permission bypass", w.Code)
	}
}

func TestAssessmentAPIBindsRunToCurrentOperator(t *testing.T) {
	h, session, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	session.UserID = "assigned-operator"
	w := request("POST", "/api/assessments/"+task.ID+"/control/start", `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, err := h.Engine.Store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ExecutorID != session.UserID || current.OwnerID != task.OwnerID {
		t.Fatalf("wrong run identity: %+v", current)
	}
}

func publishFixture(t *testing.T, h *AssessmentHandler, request func(string, string, string) *httptest.ResponseRecorder, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	var input map[string]any
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	task, err := h.Engine.Store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	st := task.Step(input["step_id"].(string))
	input["revision"] = assessment.EvidenceRevision(st.Evidence[int(input["index"].(float64))])
	b, _ := json.Marshal(input)
	return request("POST", "/api/assessments/"+id+"/publish", string(b))
}

func TestAssessmentApprovalScopesAndStaleRequest(t *testing.T) {
	h, session, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	call := &agent.ToolCall{ID: "call", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{}}}
	h.Engine.Store.Change(task.ID, "", "", "", func(t *assessment.Task) error {
		t.Steps = []*assessment.Step{{ID: "step", Status: "awaiting_approval", Pending: call, ApprovalID: "new-request"}}
		return nil
	})
	// A broad assessment role cannot widen the separate own-scoped HITL role.
	session.UserID = "other-reviewer"
	session.PermissionScopes = map[string]string{"assessment:write": database.RBACScopeAll, "hitl:write": database.RBACScopeOwn}
	w := request("POST", "/api/assessments/"+task.ID+"/steps/step/decision", `{"approved":true,"approval_id":"new-request"}`)
	if w.Code != 403 {
		t.Fatal("HITL scope was widened", w.Code, w.Body.String())
	}
	session.UserID = task.OwnerID
	session.PermissionScopes = nil
	w = request("POST", "/api/assessments/"+task.ID+"/steps/step/decision", `{"approved":true,"approval_id":"old-request"}`)
	if w.Code != 409 {
		t.Fatal("stale approval accepted", w.Code, w.Body.String())
	}
}
func TestAssessmentPublishRejectsChangedEvidence(t *testing.T) {
	h, _, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	old := assessment.Evidence{Kind: "finding", Title: "Reviewed candidate", Detail: "old observation", ExecutionIDs: []string{"exec"}}
	revision := assessment.EvidenceRevision(old)
	changed := old
	changed.Detail = "Changed after review"
	h.Engine.Store.Change(task.ID, "", "", "", func(t *assessment.Task) error {
		t.Steps = []*assessment.Step{{ID: "step", Evidence: []assessment.Evidence{changed}}}
		return nil
	})
	body := `{"step_id":"step","index":0,"kind":"vulnerability","revision":"` + revision + `"}`
	w := request("POST", "/api/assessments/"+task.ID+"/publish", body)
	if w.Code != 409 {
		t.Fatal("unreviewed replacement published", w.Code)
	}
	var count int
	h.DB.QueryRow(`SELECT count(*) FROM vulnerabilities`).Scan(&count)
	if count != 0 {
		t.Fatal("stale request wrote candidate")
	}
}
func TestAssessmentJSONBodiesAreBounded(t *testing.T) {
	_, _, request := assessmentAPI(t)
	task := createAPIAssessment(t, request)
	w := request("POST", "/api/assessments/"+task.ID+"/control/steer", `{"message":"`+strings.Repeat("x", 256<<10)+`"}`)
	if w.Code != 413 {
		t.Fatal("large body was parsed without bound", w.Code)
	}
}
func TestAssessmentMutationOriginGuard(t *testing.T) {
	for _, tc := range []struct {
		origin, site, auth string
		want               bool
	}{{"https://evil.test", "cross-site", "", false}, {"null", "", "", false}, {"", "cross-site", "", false}, {"https://csai.test", "same-origin", "", true}, {"https://integration.test", "cross-site", "Bearer fixture", true}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "https://csai.test/api/assessments/x/control/start", nil)
		c.Request.Header.Set("Origin", tc.origin)
		c.Request.Header.Set("Sec-Fetch-Site", tc.site)
		c.Request.Header.Set("Authorization", tc.auth)
		if got := assessmentMutationOriginAllowed(c); got != tc.want {
			t.Fatalf("origin %q: got %v", tc.origin, got)
		}
	}
}
