package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type staticConversationTaskState struct {
	running   bool
	startedAt time.Time
}

func (s staticConversationTaskState) ConversationTaskRuntimeState(string) (bool, time.Time) {
	return s.running, s.startedAt
}

func TestGetConversationPlanTasksRequiresAccessAndReportsProgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "conversation-plantask.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("plan", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	user, err := db.CreateRBACUser("plan-user", "Plan User", "hash", true, nil)
	if err != nil {
		t.Fatalf("CreateRBACUser: %v", err)
	}
	base := filepath.Join(tmp, "plantask")
	db.SetAgentConversationDirs(base, "", "", "")
	dir := filepath.Join(base, conversation.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for name, content := range map[string]string{
		"1.json": `{"id":"1","subject":"完成项","status":"completed"}`,
		"2.json": `{"id":"2","subject":"当前项","status":"in_progress"}`,
		"3.json": `{"id":"3","subject":"等待项","status":"pending"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	handler := NewConversationHandler(db, zap.NewNop())
	handler.SetTaskStateProvider(staticConversationTaskState{running: true})
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/conversations/"+conversation.ID+"/plan-tasks", nil)
		c.Params = gin.Params{{Key: "id", Value: conversation.ID}}
		c.Set(security.ContextSessionKey, security.Session{
			UserID: user.ID,
			Scope:  database.RBACScopeAssigned,
		})
		handler.GetConversationPlanTasks(c)
		return w
	}

	w := request()
	if w.Code != http.StatusForbidden {
		t.Fatalf("unassigned status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if err := db.AssignResourceToUser(user.ID, "conversation", conversation.ID); err != nil {
		t.Fatalf("AssignResourceToUser: %v", err)
	}
	w = request()
	if w.Code != http.StatusOK {
		t.Fatalf("assigned status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Total      int                             `json:"total"`
		Completed  int                             `json:"completed"`
		ActiveStep int                             `json:"activeStep"`
		Tasks      []database.ConversationPlanTask `json:"tasks"`
		Running    bool                            `json:"running"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 3 || response.Completed != 1 || response.ActiveStep != 2 || !response.Running {
		t.Fatalf("progress = %#v", response)
	}
}

func TestGetConversationPlanTasksReportsStoppedLiveTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	db, err := database.NewDB(filepath.Join(tmp, "conversation-plantask-stopped.db"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conversation, err := db.CreateConversation("stopped plan", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	user, err := db.CreateRBACUser("stopped-plan-user", "Stopped Plan User", "hash", true, nil)
	if err != nil {
		t.Fatalf("CreateRBACUser: %v", err)
	}
	if err := db.AssignResourceToUser(user.ID, "conversation", conversation.ID); err != nil {
		t.Fatalf("AssignResourceToUser: %v", err)
	}
	base := filepath.Join(tmp, "plantask")
	db.SetAgentConversationDirs(base, "", "", "")
	dir := filepath.Join(base, conversation.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1.json"), []byte(`{"id":"1","subject":"残留项","status":"in_progress"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	handler := NewConversationHandler(db, zap.NewNop())
	handler.SetTaskStateProvider(staticConversationTaskState{running: false})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/conversations/"+conversation.ID+"/plan-tasks", nil)
	c.Params = gin.Params{{Key: "id", Value: conversation.ID}}
	c.Set(security.ContextSessionKey, security.Session{UserID: user.ID, Scope: database.RBACScopeAssigned})
	handler.GetConversationPlanTasks(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Running bool `json:"running"`
		Total   int  `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Running || response.Total != 0 {
		t.Fatalf("response = %#v", response)
	}
}
