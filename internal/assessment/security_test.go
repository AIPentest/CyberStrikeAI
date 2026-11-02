package assessment

import (
	"context"
	"cyberstrike-ai/internal/runlease"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"
)

func TestApprovalReplayAndArgumentBinding(t *testing.T) {
	store, task := fixture(t)
	e := NewEngine(store, normalRuntime(), nil)
	call := &agent.ToolCall{ID: "reused-model-id", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"target": "first"}}}
	create := func() string {
		if err := store.Change(task.ID, "", "", "", func(t *Task) error {
			t.ExecutorID = "operator"
			t.Steps = []*Step{{ID: "step"}}
			requestApproval(t, t.Steps[0], call)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		current, _ := store.Get(task.ID)
		return current.Steps[0].ApprovalID
	}
	first := create()
	if err := e.Decide(task.ID, "step", first, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	current, _ := store.Get(task.ID)
	ok, err := e.approvedCall(context.Background(), current, current.Steps[0], *call)
	if err != nil || !ok {
		t.Fatal("valid grant rejected", err)
	}
	call.Function.Arguments["target"] = "second"
	ok, err = e.approvedCall(context.Background(), current, current.Steps[0], *call)
	if err != nil || ok {
		t.Fatal("argument change inherited approval")
	}
	second := create()
	if first == second {
		t.Fatal("host request IDs reused")
	}
	if err = e.Decide(task.ID, "step", first, "reviewer", true); !errors.Is(err, ErrConflict) {
		t.Fatal("old approval approved a different command", err)
	}
	if err = e.Decide(task.ID, "step", second, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	current, _ = store.Get(task.ID)
	current.ExecutorID = "different-operator"
	ok, err = e.approvedCall(context.Background(), current, current.Steps[0], *call)
	if err != nil || ok {
		t.Fatal("grant crossed executor identity")
	}
}
func TestApprovalReviewerRevocationAndScope(t *testing.T) {
	store, task := fixture(t)
	if err := store.DB.BootstrapRBAC("fixture", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	admin, err := store.DB.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	role, err := store.DB.UpsertRBACRole("review", "review", "", database.RBACScopeOwn, []string{"hitl:write", "assessment:write"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.DB.CreateRBACUser("reviewer", "Reviewer", "fixture", true, []string{role.ID})
	if err != nil {
		t.Fatal(err)
	}
	rt := &CSAIRuntime{DB: store.DB, Config: &config.Config{}}
	st := &Step{ApprovedBy: user.ID}
	task.OwnerID = admin.ID
	if ok, err := rt.VerifyApproval(context.Background(), task, st); err != nil || ok {
		t.Fatal("own scope approved somebody else's task", err)
	}
	if err = store.DB.AssignResourceToUser(user.ID, "assessment", task.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := rt.VerifyApproval(context.Background(), task, st); err != nil || !ok {
		t.Fatal("assigned reviewer rejected", err)
	}
	if _, err = store.DB.Exec(`DELETE FROM rbac_resource_assignments WHERE user_id=?`, user.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := rt.VerifyApproval(context.Background(), task, st); err != nil || ok {
		t.Fatal("revoked review assignment stayed valid", err)
	}
}
func TestPolicyGrantDoesNotBypassNewManualPolicy(t *testing.T) {
	store, task := fixture(t)
	rt := &CSAIRuntime{Config: &config.Config{}}
	e := NewEngine(store, rt, nil)
	task.ApprovalMode = "policy"
	task.ExecutorID = "operator"
	call := agent.ToolCall{ID: "call", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{}}}
	s := &Step{Approved: true, Pending: &call, ApprovalKind: "policy", ApprovalDigest: callDigest(call), ApprovedExecutorID: "operator"}
	if ok, err := e.approvedCall(context.Background(), task, s, call); err != nil || !ok {
		t.Fatal("valid policy denied", err)
	}
	rt.Config.Hitl.DefaultMode = "approval"
	if ok, err := e.approvedCall(context.Background(), task, s, call); err != nil || ok {
		t.Fatal("old policy grant bypassed newly required approval", err)
	}
}
func TestToolCallBatchCannotReuseIDsAfterCompaction(t *testing.T) {
	c := agent.ToolCall{ID: "old", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{}}}
	if err := validateToolCalls(nil, []agent.ToolCall{c}, []string{"old"}); err == nil {
		t.Fatal("reused archived tool ID accepted")
	}
	calls := []agent.ToolCall{}
	for i := 0; i < 9; i++ {
		v := c
		v.ID = fmt.Sprint(i)
		calls = append(calls, v)
	}
	if err := validateToolCalls(nil, calls, nil); err == nil {
		t.Fatal("unbounded tool fan-out accepted")
	}
}
func TestLargeTraceListAndEventPagingRemainBounded(t *testing.T) {
	store, task := fixture(t)
	trace, _ := json.Marshal([]agent.ChatMessage{{Role: "assistant", Content: strings.Repeat("x", 2<<20)}})
	store.Change(task.ID, "", "", "", func(t *Task) error { t.Steps = []*Step{{ID: "large", Trace: trace}}; return nil })
	rows, err := store.List("owner", database.RBACScopeOwn, 50, 0)
	if err != nil || len(rows) != 1 || len(rows[0].Steps) != 0 {
		t.Fatal("list exposed trace", err)
	}
	for i := 0; i < 12; i++ {
		if err = store.Event(task.ID, "large", "tool_result", "body", map[string]string{"output": strings.Repeat("x", 200000)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.Events(task.ID, 0)
	if err != nil || len(first) == 0 || len(first) >= 12 {
		t.Fatal("event page is unbounded", len(first), err)
	}
	next, err := store.Events(task.ID, first[len(first)-1].ID)
	if err != nil || len(next) == 0 || next[0].ID <= first[len(first)-1].ID {
		t.Fatal("bounded paging lost events", err)
	}
}

func TestPauseClosesDetachedToolAdmissionBeforeAcknowledgement(t *testing.T) {
	store, task := fixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	late := make(chan error, 1)
	r := normalRuntime()
	r.execute = func(ctx context.Context, _ *Task, _ string, _ agent.ToolCall) (*agent.ToolExecutionResult, error) {
		close(entered)
		<-release
		unregister, err := runlease.FromContext(ctx).Register("late-tool", func() {})
		if err == nil {
			unregister()
		}
		late <- err
		return nil, ctx.Err()
	}
	e := NewEngine(store, autonomousFixture{r}, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tool dispatch not reached")
	}
	if err := e.Control(task.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-late; !errors.Is(err, runlease.ErrClosed) {
		t.Fatal("tool submitted after pause acknowledgement", err)
	}
	waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "paused" })
}
