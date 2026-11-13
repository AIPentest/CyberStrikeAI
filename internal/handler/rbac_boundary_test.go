package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func withTempWorkingDir(t *testing.T) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
	return dir
}

func TestDetachedAgentContextRetainsPrincipalWithoutParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	parent = authctx.WithPrincipal(parent, authctx.NewPrincipal("u1", "user", database.RBACScopeAssigned, map[string]bool{"agent:execute": true}))
	detached := detachedAgentContext(parent)
	cancel()
	if err := detached.Err(); err != nil {
		t.Fatalf("detached context inherited cancellation: %v", err)
	}
	principal, ok := authctx.PrincipalFromContext(detached)
	if !ok || principal.UserID != "u1" || !principal.HasPermission("agent:execute") {
		t.Fatalf("detached context lost principal: %#v, ok=%v", principal, ok)
	}
}

func TestPromoteAttackChainRequiresSourceConversationAccess(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "promote-rbac.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	project, _ := db.CreateProject(&database.Project{Name: "owned"})
	conversation, _ := db.CreateConversation("foreign", database.ConversationCreateMeta{})
	_ = db.SetResourceOwner("project", project.ID, "u1")
	_ = db.SetResourceOwner("conversation", conversation.ID, "u2")
	h := NewProjectHandler(db, zap.NewNop())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{UserID: "u1", Scope: database.RBACScopeOwn})
		c.Next()
	})
	router.POST("/api/projects/:id/promote-attack-chain/:conversationId", h.PromoteAttackChain)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/promote-attack-chain/"+conversation.ID, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestVulnerabilityCannotBeReparentedToForeignProject(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "vuln-reparent-rbac.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	owned, _ := db.CreateProject(&database.Project{Name: "owned"})
	foreign, _ := db.CreateProject(&database.Project{Name: "foreign"})
	_ = db.SetResourceOwner("project", owned.ID, "u1")
	_ = db.SetResourceOwner("project", foreign.ID, "u2")
	vulnerability, err := db.CreateVulnerability(&database.Vulnerability{Title: "v", Severity: "high", ProjectID: owned.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetResourceOwner("vulnerability", vulnerability.ID, "u1")
	h := NewVulnerabilityHandler(db, zap.NewNop())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{UserID: "u1", Scope: database.RBACScopeOwn})
		c.Next()
	})
	router.PUT("/api/vulnerabilities/:id", h.UpdateVulnerability)
	body, _ := json.Marshal(map[string]interface{}{"project_id": foreign.ID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/vulnerabilities/"+vulnerability.ID, bytes.NewReader(body)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestAgentTaskEndpointsFilterAndRejectForeignConversations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, user := setupConversationRBACTest(t)
	allowed, _ := db.CreateConversation("allowed", database.ConversationCreateMeta{})
	hidden, _ := db.CreateConversation("hidden", database.ConversationCreateMeta{})
	if err := db.AssignResourceToUser(user.ID, "conversation", allowed.ID); err != nil {
		t.Fatal(err)
	}
	tasks := NewAgentTaskManager()
	if _, err := tasks.StartTask(allowed.ID, "visible", func(error) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.StartTask(hidden.ID, "secret", func(error) {}); err != nil {
		t.Fatal(err)
	}
	h := &AgentHandler{db: db, tasks: tasks, logger: zap.NewNop()}

	w := performAssignedHandler(user, http.MethodGet, "/api/agent-loop/tasks", nil, h.ListAgentTasks)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Tasks []*AgentTask `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Tasks) != 1 || response.Tasks[0].ConversationID != allowed.ID {
		t.Fatalf("tasks = %#v, want only %s", response.Tasks, allowed.ID)
	}

	w = performAssignedHandler(user, http.MethodPost, "/api/agent-loop/cancel", map[string]string{"conversationId": hidden.ID}, h.CancelAgentLoop)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cancel status = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestChatUploadPathAuthorizationFollowsConversationAccess(t *testing.T) {
	db, user := setupConversationRBACTest(t)
	allowed, _ := db.CreateConversation("allowed", database.ConversationCreateMeta{})
	hidden, _ := db.CreateConversation("hidden", database.ConversationCreateMeta{})
	if err := db.AssignResourceToUser(user.ID, "conversation", allowed.ID); err != nil {
		t.Fatal(err)
	}
	h := NewChatUploadsHandler(zap.NewNop(), db)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned, Permissions: map[string]bool{"chat:write": true}})

	if !h.pathAllowed(c, filepath.ToSlash(filepath.Join("2026-07-10", allowed.ID, "a.txt"))) {
		t.Fatal("assigned conversation attachment should be accessible")
	}
	if h.pathAllowed(c, filepath.ToSlash(filepath.Join("2026-07-10", hidden.ID, "secret.txt"))) {
		t.Fatal("foreign conversation attachment should be denied")
	}
	if h.pathAllowed(c, "2026-07-10/_manual/secret.txt") {
		t.Fatal("unowned manual attachment should fail closed")
	}
}

func TestChatUploadsListIncludesAuthorizedProjectWorkspaceFiles(t *testing.T) {
	withTempWorkingDir(t)
	db, user := setupConversationRBACTest(t)
	fsBase := t.TempDir()
	workspaceBase := filepath.Join(fsBase, "workspace")
	reductionBase := filepath.Join(fsBase, "reduction")
	db.SetAgentConversationDirs("", "", reductionBase, workspaceBase)
	allowedProject, _ := db.CreateProject(&database.Project{Name: "allowed"})
	hiddenProject, _ := db.CreateProject(&database.Project{Name: "hidden"})
	conversation, _ := db.CreateConversation("project conversation", database.ConversationCreateMeta{ProjectID: allowedProject.ID})
	if err := db.AssignResourceToUser(user.ID, "project", allowedProject.ID); err != nil {
		t.Fatal(err)
	}
	allowedFile := filepath.Join(workspaceBase, "projects", allowedProject.ID, "csv", "assets.csv")
	hiddenFile := filepath.Join(workspaceBase, "projects", hiddenProject.ID, "csv", "secret.csv")
	for _, path := range []string{allowedFile, hiddenFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("name\nexample\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	h := NewChatUploadsHandler(zap.NewNop(), db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/chat-uploads?source=workspace&pageSize=all&conversation="+conversation.ID, nil)
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
	h.List(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var response struct {
		Files []ChatUploadFileItem `json:"files"`
		Total int                  `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Files) != 1 {
		t.Fatalf("files = %#v, total = %d, want only authorized workspace file", response.Files, response.Total)
	}
	got := response.Files[0]
	if got.Source != chatUploadSourceWorkspace || got.Name != "assets.csv" || got.ProjectID != allowedProject.ID {
		t.Fatalf("workspace file = %#v", got)
	}
	if got.ProjectName != allowedProject.Name {
		t.Fatalf("projectName = %q, want %q", got.ProjectName, allowedProject.Name)
	}
	if got.ConversationID != conversation.ID {
		t.Fatalf("conversationId = %q, want %q", got.ConversationID, conversation.ID)
	}
	if got.ConversationTitle != conversation.Title {
		t.Fatalf("conversationTitle = %q, want %q", got.ConversationTitle, conversation.Title)
	}
	if got.AbsolutePath != allowedFile {
		t.Fatalf("absolutePath = %q, want %q", got.AbsolutePath, allowedFile)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	resolveURL := "/api/chat-uploads/path?kind=directory&path=__workspace__%2Fprojects%2F" + allowedProject.ID
	c.Request = httptest.NewRequest(http.MethodGet, resolveURL, nil)
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
	h.ResolvePath(c)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resolved struct {
		AbsolutePath string `json:"absolutePath"`
		IsDir        bool   `json:"isDir"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(workspaceBase, "projects", allowedProject.ID)
	if !resolved.IsDir || resolved.AbsolutePath != wantDir {
		t.Fatalf("resolved = %#v, want dir %q", resolved, wantDir)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/chat-uploads/path?kind=directory&path=__workspace__%2Fprojects", nil)
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
	h.ResolvePath(c)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve projects container status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	wantContainer := filepath.Join(workspaceBase, "projects")
	if !resolved.IsDir || resolved.AbsolutePath != wantContainer {
		t.Fatalf("resolved container = %#v, want dir %q", resolved, wantContainer)
	}

	for _, tc := range []struct {
		path string
		want string
	}{
		{"__workspace__/", workspaceBase},
		{"__reduction__/", reductionBase},
		{"__conversation_artifact__/", db.ConversationArtifactsBaseDir()},
	} {
		if err := os.MkdirAll(tc.want, 0o755); err != nil {
			t.Fatal(err)
		}
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/chat-uploads/path?kind=directory&path="+url.QueryEscape(tc.path), nil)
		c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
		h.ResolvePath(c)
		if w.Code != http.StatusOK {
			t.Fatalf("resolve root %q status = %d, want 200: %s", tc.path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resolved); err != nil {
			t.Fatal(err)
		}
		wantAbs, _ := filepath.Abs(tc.want)
		if !resolved.IsDir || resolved.AbsolutePath != wantAbs {
			t.Fatalf("resolved root %q = %#v, want dir %q", tc.path, resolved, wantAbs)
		}
	}
}

func TestPrepareMultiAgentSessionRejectsForeignConversation(t *testing.T) {
	db, user := setupConversationRBACTest(t)
	hidden, _ := db.CreateConversation("hidden", database.ConversationCreateMeta{})
	h := &AgentHandler{db: db, logger: zap.NewNop()}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned, Permissions: map[string]bool{"chat:write": true}})

	_, err := h.prepareMultiAgentSession(&ChatRequest{ConversationID: hidden.ID, Message: "write"}, c, "test")
	if err == nil || err.Error() != "无权访问该对话" {
		t.Fatalf("err = %v, want unauthorized conversation", err)
	}
}

func TestMonitorExecutionDetailRejectsForeignOwner(t *testing.T) {
	db, user := setupConversationRBACTest(t)
	for _, exec := range []*mcp.ToolExecution{
		{ID: "exec-allowed", ToolName: "allowed", Status: "completed", StartTime: time.Now(), OwnerUserID: user.ID},
		{ID: "exec-hidden", ToolName: "hidden", Status: "completed", StartTime: time.Now(), OwnerUserID: "another-user"},
	} {
		if err := db.SaveToolExecution(exec); err != nil {
			t.Fatal(err)
		}
	}
	h := NewMonitorHandler(mcp.NewServerWithStorage(zap.NewNop(), db), nil, db, zap.NewNop())

	request := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/monitor/execution/"+id, nil)
		c.Params = gin.Params{{Key: "id", Value: id}}
		c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
		h.GetExecution(c)
		return w
	}
	if w := request("exec-hidden"); w.Code != http.StatusForbidden {
		t.Fatalf("hidden status = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if w := request("exec-allowed"); w.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func performAssignedHandler(user *database.RBACUser, method, path string, body interface{}, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		payload, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
	handler(c)
	return w
}
