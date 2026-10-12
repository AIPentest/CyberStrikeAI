package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestLearningLifecycleHookRunsOnceBeforeConversationReleased(t *testing.T) {
	m := NewAgentTaskManager()
	var starts, finishes atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	m.SetRunPersistenceHooks(func(*AgentTask) { starts.Add(1) }, func(id, status string) { finishes.Add(1); close(entered); <-release })
	task, err := m.StartTask("conversation", "synthetic", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.FinishTaskRun("conversation", task.RunID, "completed") }()
	<-entered
	if _, err := m.StartTask("conversation", "new task", nil); err == nil {
		t.Fatal("conversation released before persisted terminal slice")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.FinishTaskRun("conversation", task.RunID, "failed"); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 || finishes.Load() != 1 {
		t.Fatal("duplicate lifecycle persistence")
	}
}

func TestLearningAPIRequiresConversationAndProjectReadScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "api.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateConversation("synthetic", database.ConversationCreateMeta{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetResourceOwner("conversation", c.ID, "owner")
	_ = db.SetResourceOwner("project", p.ID, "project-owner")
	h := &AgentHandler{db: db}
	for _, tc := range []struct {
		name, uid, chatScope, projectScope string
		projectRead                        bool
		code                               int
	}{
		{"other conversation", "other", "own", "all", true, 403},
		{"missing project permission", "owner", "own", "all", false, 403},
		{"broad chat cannot widen project", "owner", "all", "own", true, 403},
		{"authorized", "owner", "own", "all", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(ctx *gin.Context) {
				ctx.Set(security.ContextSessionKey, security.Session{UserID: tc.uid, Permissions: map[string]bool{"chat:read": true, "project:read": tc.projectRead}, PermissionScopes: map[string]string{"chat:read": tc.chatScope, "project:read": tc.projectScope}})
				ctx.Next()
			})
			r.Use(security.RBACMiddleware(nil))
			r.GET("/api/conversations/:id/learning", h.ListConversationLearning)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/conversations/"+c.ID+"/learning", nil))
			if w.Code != tc.code {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
