package handler

import (
	"context"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAsyncAdmissionDisconnectIdempotencyAndAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.NewDB(filepath.Join(t.TempDir(), "async.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateRBACUser("caller", "Caller", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.MultiAgent.Enabled = true
	cfg.OpenAI.Model = "test"
	h := &AgentHandler{db: db, logger: zap.NewNop(), tasks: NewAgentTaskManager(), config: cfg}
	defer h.tasks.Shutdown()
	session := security.Session{UserID: user.ID, Scope: database.RBACScopeAll, Permissions: map[string]bool{"chat:write": true, "agent:execute": true}}
	contexts := make(chan context.Context, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	executor := func(ctx, base context.Context, cancel context.CancelCauseFunc, timeout context.CancelFunc, req ChatRequest, p *multiAgentPrepared, r *database.AgentRun, multi bool, wantEnglish bool) {
		contexts <- ctx
		<-release
		status := "completed"
		if ctx.Err() != nil {
			status = "cancelled"
		}
		_ = h.tasks.FinishTaskRun(p.ConversationID, r.RunID, status)
		_ = db.FinishAgentRun(r.RunID, status, map[string]string{"response": "done"})
		timeout()
		cancel(nil)
		done <- struct{}{}
	}
	submit := func(ctx context.Context, key, body string, multi bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/eino-agent/runs", strings.NewReader(body)).WithContext(ctx)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Idempotency-Key", key)
		c.Set(security.ContextSessionKey, session)
		h.submitAgentRunWithExecutor(c, multi, executor)
		return w
	}
	parent, cancel := context.WithCancel(context.Background())
	w := submit(parent, "first", `{"message":"long task"}`, false)
	if w.Code != 202 {
		t.Fatalf("submit %d %s", w.Code, w.Body)
	}
	var receipt struct{ RunID, ConversationID string }
	if err = json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	ctx := <-contexts
	cancel()
	if ctx.Err() != nil {
		t.Fatal("client disconnect cancelled worker")
	}
	retry := submit(context.Background(), "first", `{"message":"long task"}`, false)
	if retry.Code != 202 || !strings.Contains(retry.Body.String(), receipt.RunID) {
		t.Fatalf("retry: %d %s", retry.Code, retry.Body)
	}
	if w = submit(context.Background(), "first", `{"message":"changed"}`, false); w.Code != 409 {
		t.Fatalf("mismatch %d", w.Code)
	}
	before, _ := db.GetMessages(receipt.ConversationID)
	body := `{"message":"conflict","conversationId":"` + receipt.ConversationID + `"}`
	if w = submit(context.Background(), "second", body, false); w.Code != 409 {
		t.Fatalf("conflict %d %s", w.Code, w.Body)
	}
	after, _ := db.GetMessages(receipt.ConversationID)
	if len(before) != len(after) {
		t.Fatal("conflict polluted conversation")
	}
	// GET uses resource permissions, even when a caller guesses a run ID.
	w = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "runId", Value: receipt.RunID}}
	c.Set(security.ContextSessionKey, security.Session{UserID: "stranger", Scope: database.RBACScopeAssigned})
	h.GetAgentRun(c)
	if w.Code != 403 {
		t.Fatalf("unauthorized get %d", w.Code)
	}
	if ok, err := h.tasks.CancelTask(receipt.ConversationID, ErrTaskCancelled); !ok || err != nil {
		t.Fatalf("cancel %v %v", ok, err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("explicit cancel did not stop worker")
	}
	close(release)
	<-done
	saved, err := db.GetAgentRun(receipt.RunID)
	if err != nil || saved.Status != "cancelled" {
		t.Fatalf("status %+v %v", saved, err)
	}
	// Multi-agent uses the same acceptance contract.
	w = submit(context.Background(), "multi", `{"message":"other"}`, true)
	if w.Code != 202 {
		t.Fatalf("multi %d %s", w.Code, w.Body)
	}
	<-contexts
	<-done
}

func TestAsyncWorkerFinalizesCancellationDeadlineAndPanic(t *testing.T) {
	for _, mode := range []string{"completed", "cancelled", "timeout", "panic"} {
		t.Run(mode, func(t *testing.T) {
			db, err := database.NewDB(filepath.Join(t.TempDir(), "run.db"), zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			h := &AgentHandler{db: db, logger: zap.NewNop(), tasks: NewAgentTaskManager()}
			defer h.tasks.Shutdown()
			base, cancel := context.WithCancelCause(context.Background())
			ctx, timeout := context.WithTimeout(base, time.Hour)
			if mode == "timeout" {
				timeout()
				ctx, timeout = context.WithDeadline(base, time.Now().Add(-time.Second))
			}
			task, err := h.tasks.StartTask("conv", "message", cancel)
			if err != nil {
				t.Fatal(err)
			}
			r := &database.AgentRun{RunID: task.RunID, UserID: "user", Key: mode, ConversationID: "conv", Status: "running"}
			if err = db.CreateAgentRun(r); err != nil {
				t.Fatal(err)
			}
			prep := &multiAgentPrepared{ConversationID: "conv"}
			h.executeAcceptedRunBody(ctx, base, cancel, timeout, prep, r, false, func(status *string, respond func(int, interface{})) {
				if mode == "panic" {
					panic("test")
				}
				if mode == "cancelled" {
					cancel(ErrTaskCancelled)
				}
				*status = "completed"
				respond(200, map[string]string{"response": "result"})
			})
			got, err := db.GetAgentRun(task.RunID)
			want := mode
			if mode == "panic" {
				want = "failed"
			}
			if err != nil || got.Status != want {
				t.Fatalf("got %+v err %v want %s", got, err, want)
			}
			if h.tasks.GetTask("conv") != nil {
				t.Fatal("task remained active")
			}
		})
	}
}

func TestOpenAPIAsyncRunEndpoints(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/openapi.json", nil)
	(&OpenAPIHandler{}).GetOpenAPISpec(c)
	if w.Code != 200 {
		t.Fatalf("spec %d", w.Code)
	}
	var spec map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]interface{})
	for _, path := range []string{"/api/eino-agent/runs", "/api/multi-agent/runs", "/api/eino-agent/runs/{runId}", "/api/multi-agent/runs/{runId}"} {
		if paths[path] == nil {
			t.Errorf("missing %s", path)
		}
	}
}
