package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestNotificationReadStateIsPerUser(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "notification-rbac.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewNotificationHandler(db, nil, zap.NewNop())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{UserID: "u1", Scope: database.RBACScopeAssigned})
		c.Next()
	})
	router.POST("/notifications/read", h.MarkRead)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/notifications/read", bytes.NewBufferString(`{"eventIds":["vuln:v1"]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mark read status = %d: %s", w.Code, w.Body.String())
	}
	u1, err := h.readStatesByIDs("u1", []string{"vuln:v1"})
	if err != nil || !u1["vuln:v1"] {
		t.Fatalf("u1 read state = %#v, err=%v", u1, err)
	}
	u2, err := h.readStatesByIDs("u2", []string{"vuln:v1"})
	if err != nil || u2["vuln:v1"] {
		t.Fatalf("u2 inherited u1 read state = %#v, err=%v", u2, err)
	}
}

func TestLongRunningNotificationCanBeMarkedRead(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "notifications.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const conversationID = "conversation-1"
	if _, err := db.Exec(`
		INSERT INTO hitl_interrupts (id, conversation_id, mode, tool_name, status, created_at)
		VALUES ('approval-1', ?, 'approval', 'fixture', 'pending', CURRENT_TIMESTAMP)
	`, conversationID); err != nil {
		t.Fatal(err)
	}

	tasks := NewAgentTaskManager()
	t.Cleanup(tasks.Shutdown)
	startLongRun := func() *AgentTask {
		t.Helper()
		task, err := tasks.StartTask(conversationID, "fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		tasks.mu.Lock()
		task.StartedAt = time.Now().Add(-16 * time.Minute)
		tasks.mu.Unlock()
		return task
	}
	first := startLongRun()
	h := NewNotificationHandler(db, &AgentHandler{tasks: tasks}, zap.NewNop())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(security.ContextSessionKey, security.Session{
			UserID: c.GetHeader("X-Test-User"), Scope: database.RBACScopeAll,
			Permissions: map[string]bool{"tasks:read": true, "hitl:read": true},
		})
	})
	router.GET("/notifications/summary", h.GetSummary)
	router.POST("/notifications/read", h.MarkRead)
	request := func(method, path, userID string, body []byte) []byte {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("X-Test-User", userID)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d: %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	summary := func(userID string) NotificationSummaryResponse {
		t.Helper()
		var result NotificationSummaryResponse
		if err := json.Unmarshal(request(http.MethodGet, "/notifications/summary", userID, nil), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	longRunningID := func(result NotificationSummaryResponse) string {
		for _, item := range result.Items {
			if item.Type == "long_running_tasks" && item.ConversationID == conversationID {
				return item.ID
			}
		}
		return ""
	}

	before := summary("u1")
	id := longRunningID(before)
	if id == "" || before.UnreadCount != 2 {
		t.Fatalf("expected a long-running and an approval notification, got %+v", before)
	}
	body, err := json.Marshal(markReadRequest{EventIDs: []string{id, "hitl:approval-1"}})
	if err != nil {
		t.Fatal(err)
	}
	var marked struct {
		Marked int `json:"marked"`
	}
	if err := json.Unmarshal(request(http.MethodPost, "/notifications/read", "u1", body), &marked); err != nil {
		t.Fatal(err)
	}
	if marked.Marked != 1 {
		t.Fatalf("marked = %d, want only the long-running notification marked", marked.Marked)
	}

	after := summary("u1")
	if after.UnreadCount != 1 || len(after.Items) != 1 || after.Items[0].Type != "hitl_pending" || after.Counts["longRunningTasks"] != 1 {
		t.Fatalf("expected only the approval notification while the task keeps running, got %+v", after)
	}
	if otherID := longRunningID(summary("u2")); otherID != id {
		t.Fatalf("other user saw notification %q, want %q", otherID, id)
	}
	if err := tasks.FinishTaskRun(conversationID, first.RunID, "completed"); err != nil {
		t.Fatal(err)
	}
	startLongRun()
	if nextID := longRunningID(summary("u1")); nextID == "" || nextID == id {
		t.Fatalf("new run must have a new notification, got %q after %q", nextID, id)
	}
}
