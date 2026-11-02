package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

type fakeRuntime struct {
	review   func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error)
	complete func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error)
	execute  func(context.Context, *Task, string, agent.ToolCall) (*agent.ToolExecutionResult, error)
	calls    atomic.Int32
}

func (f *fakeRuntime) Context(ctx context.Context, _ *Task) (context.Context, error) { return ctx, nil }
func (f *fakeRuntime) Tools(*Task) ([]agent.Tool, error) {
	return []agent.Tool{{Type: "function", Function: agent.FunctionDefinition{Name: "inspect", Parameters: map[string]interface{}{"type": "object"}}}}, nil
}
func (f *fakeRuntime) Complete(ctx context.Context, t *Task, m []agent.ChatMessage, tools []agent.Tool, _ int) (*ModelReply, error) {
	if requestPurpose(ctx).Phase == "review" {
		if f.review != nil {
			return f.review(ctx, t, m, tools)
		}
		out := Review{Summary: "Fixture acceptance reviewed"}
		refs := []string{}
		for _, s := range t.Steps {
			refs = append(refs, s.SuccessfulIDs...)
		}
		for i := range criteria(t) {
			status := "satisfied"
			if len(refs) == 0 {
				status = "insufficient"
			}
			out.Criteria = append(out.Criteria, CriterionResult{Index: i, Status: status, Reason: "Fixture checks", ExecutionIDs: refs})
		}
		for _, s := range t.Steps {
			for i, v := range s.Evidence {
				if v.Kind == "finding" {
					out.Findings = append(out.Findings, FindingReview{StepID: s.ID, EvidenceIndex: i, Status: "needs_retest", Reason: "Fixture candidate"})
				}
			}
		}
		return reply(out), nil
	}
	return f.complete(ctx, t, m, tools)
}
func (f *fakeRuntime) Execute(ctx context.Context, t *Task, id string, c agent.ToolCall) (*agent.ToolExecutionResult, error) {
	f.calls.Add(1)
	if f.execute != nil {
		return f.execute(ctx, t, id, c)
	}
	return &agent.ToolExecutionResult{ExecutionID: "exec-1", Result: "confirmed observation"}, nil
}
func fixture(t *testing.T) (*Store, *Task) {
	t.Helper()
	db, err := database.NewDB(filepath.Join(t.TempDir(), "assessment.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Store{DB: db}
	task := &Task{Name: "Local fixture", Goal: "Assess fixture", Scope: "Only local test fixture", Tools: []string{"inspect"}, OwnerID: "owner", ApprovalMode: "manual"}
	if err = task.Validate(); err != nil {
		t.Fatal(err)
	}
	if err = s.Create(task); err != nil {
		t.Fatal(err)
	}
	return s, task
}
func reply(v any) *ModelReply {
	b, _ := json.Marshal(v)
	return &ModelReply{Message: agent.ChatMessage{Role: "assistant", Content: string(b)}, Tokens: 100}
}
func waitTask(t *testing.T, s *Store, id string, predicate func(*Task) bool) *Task {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if predicate(task) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := s.Get(id)
	t.Fatalf("timed out: %+v", task)
	return nil
}
func normalRuntime() *fakeRuntime {
	return &fakeRuntime{complete: func(ctx context.Context, t *Task, m []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
		if strings.Contains(m[0].Content, "You plan an authorized") {
			if len(t.Steps) == 0 {
				return reply(map[string]any{"status": "continue", "summary": "inspect fixture", "steps": []map[string]any{{"key": "inspect", "title": "Inspect", "instruction": "inspect fixture"}}}), nil
			}
			return reply(map[string]any{"status": "completed", "summary": "Evidence confirms goal"}), nil
		}
		if m[len(m)-1].Role == "tool" {
			return reply(map[string]any{"status": "completed", "summary": "Observed fixture", "evidence": []Evidence{{Kind: "fact", Title: "Observation", Detail: "Test fixture observed", ExecutionIDs: []string{"exec-1"}}}}), nil
		}
		return &ModelReply{Message: agent.ChatMessage{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Type: "function", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{"target": "local-fixture"}}}}}, Tokens: 100}, nil
	}}
}
func TestAssessmentApprovalCompletionAndChatIndependence(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	waiting := waitTask(t, store, task.ID, func(t *Task) bool { return len(t.Steps) == 1 && t.Steps[0].Status == "awaiting_approval" })
	if r.calls.Load() != 0 {
		t.Fatal("tool ran before approval")
	}
	if err := e.Start(task.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate start: %v", err)
	}
	if err := store.Delete(task.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("active task deleted: %v", err)
	}
	st := waiting.Steps[0]
	if st.Pending == nil || len(st.Trace) == 0 {
		t.Fatal("approval transcript not durable")
	}
	if waiting.Public().Steps[0].Trace != nil {
		t.Fatal("private transcript exposed")
	}
	if err := e.Decide(task.ID, st.ID, st.ApprovalID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	if err := e.Decide(task.ID, st.ID, st.ApprovalID, "reviewer", true); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate decision accepted")
	}
	completed := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "completed" && t.Deadline == 0 })
	if r.calls.Load() != 1 || completed.Tokens != 500 || completed.Reserved != 0 {
		t.Fatalf("unexpected accounting/calls: %+v, %d", completed, r.calls.Load())
	}
	var count int
	if err := store.DB.QueryRow(`SELECT count(*) FROM conversations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("chat rows: %d %v", count, err)
	}
	report := Report(completed)
	if !strings.Contains(report, "exec-1") || !strings.Contains(report, "Test fixture observed") {
		t.Fatal("report missing evidence")
	}
	events, err := store.Events(task.ID, 0)
	if err != nil || len(events) < 8 {
		t.Fatalf("events: %d %v", len(events), err)
	}
	for _, v := range events {
		if v.ID <= 0 {
			t.Fatal("nonmonotonic cursor")
		}
	}
	if err := store.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	events, err = store.Events(task.ID, 0)
	if err != nil || len(events) != 0 {
		t.Fatal("event cascade failed")
	}
}
func TestRecoveryBlocksUncertainOperationsAndPreservesApprovals(t *testing.T) {
	store, task := fixture(t)
	if err := store.Change(task.ID, "", "", "", func(t *Task) error {
		t.Status = "running"
		t.Deadline = time.Now().Unix() + 120
		t.Reserved = 123
		t.Steps = []*Step{{ID: "inflight", Status: "running", Trace: json.RawMessage(`[]`)}, {ID: "approval", Status: "awaiting_approval", Pending: &agent.ToolCall{ID: "pending"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(task.ID)
	if got.Status != "paused" || got.Steps[0].Status != "blocked" || got.Steps[1].Status != "awaiting_approval" || got.Tokens != 123 || got.Reserved != 0 {
		t.Fatalf("bad recovery: %+v", got)
	}
	r := normalRuntime()
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Retry(task.ID, "inflight"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(task.ID)
	if got.Steps[0].Status != "pending" || got.Steps[0].Trace != nil {
		t.Fatal("explicit retry not reset")
	}
	if r.calls.Load() != 0 {
		t.Fatal("recovery executed an external action")
	}
}
func TestPauseCancelsInFlightWorkAndRequiresExplicitRetry(t *testing.T) {
	store, task := fixture(t)
	entered := make(chan struct{})
	r := normalRuntime()
	r.execute = func(ctx context.Context, _ *Task, _ string, _ agent.ToolCall) (*agent.ToolExecutionResult, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	got := waitTask(t, store, task.ID, func(t *Task) bool { return len(t.Steps) > 0 && t.Steps[0].Status == "awaiting_approval" })
	if err := e.Decide(task.ID, got.Steps[0].ID, got.Steps[0].ApprovalID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tool did not start")
	}
	if err := e.Control(task.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	got = waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "paused" })
	if got.Steps[0].Status != "blocked" || got.Deadline != 0 || got.RemainingSeconds <= 0 {
		t.Fatalf("bad pause: %+v", got)
	}
}
func TestConcurrentBudgetReservationAndNoSpendOnFailure(t *testing.T) {
	store, task := fixture(t)
	if err := store.Change(task.ID, "", "", "", func(t *Task) error { t.Status = "running"; t.TokenBudget = 12000; return nil }); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	r := &fakeRuntime{complete: func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		entered <- struct{}{}
		<-release
		return reply(map[string]string{"status": "blocked"}), nil
	}}
	e := NewEngine(store, r, nil)
	msgs := []agent.ChatMessage{{Role: "user", Content: strings.Repeat("x", 2000)}}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := e.call(context.Background(), task.ID, msgs, nil); errs <- err }()
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no model call")
	}
	select {
	case <-entered:
		t.Fatal("budget oversubscribed by parallel workers")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	close(errs)
	budget := 0
	for err := range errs {
		if errors.Is(err, ErrBudget) {
			budget++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if budget != 0 {
		t.Fatalf("budget rejections: %d", budget)
	}
	got, _ := store.Get(task.ID)
	if got.Tokens != 200 || got.Reserved != 0 {
		t.Fatalf("accounting: %+v", got)
	}
}
func TestPlannerCannotCompleteWithoutToolEvidence(t *testing.T) {
	store, task := fixture(t)
	r := &fakeRuntime{complete: func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		return reply(map[string]string{"status": "completed", "summary": "fabricated success"}), nil
	}}
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "blocked" && t.Deadline == 0 })
	if got.Reason != "completion_missing_evidence" {
		t.Fatalf("completion reason: %s", got.Reason)
	}
}
func TestWorkerRejectsFabricatedEvidence(t *testing.T) {
	store, task := fixture(t)
	r := normalRuntime()
	base := r.complete
	r.complete = func(ctx context.Context, t *Task, m []agent.ChatMessage, tools []agent.Tool) (*ModelReply, error) {
		if !strings.Contains(m[0].Content, "You plan an authorized") && m[len(m)-1].Role == "tool" {
			return reply(map[string]any{"status": "completed", "evidence": []Evidence{{Kind: "finding", ExecutionIDs: []string{"invented-id"}}}}), nil
		}
		return base(ctx, t, m, tools)
	}
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	got := waitTask(t, store, task.ID, func(t *Task) bool { return len(t.Steps) > 0 && t.Steps[0].Status == "awaiting_approval" })
	if err := e.Decide(task.ID, got.Steps[0].ID, got.Steps[0].ApprovalID, "reviewer", true); err != nil {
		t.Fatal(err)
	}
	got = waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "blocked" && t.Deadline == 0 })
	if got.Steps[0].Status != "blocked" || len(got.Steps[0].Evidence) > 0 {
		t.Fatal("fabricated finding persisted")
	}
}
func TestUnknownDependencyRollsBackPlan(t *testing.T) {
	store, task := fixture(t)
	r := &fakeRuntime{complete: func(context.Context, *Task, []agent.ChatMessage, []agent.Tool) (*ModelReply, error) {
		return reply(map[string]any{"status": "continue", "steps": []map[string]any{{"key": "bad", "title": "bad", "instruction": "bad", "depends_on": []string{"future"}}}}), nil
	}}
	e := NewEngine(store, r, nil)
	defer e.Close()
	if err := e.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	got := waitTask(t, store, task.ID, func(t *Task) bool { return t.Status == "failed" && t.Deadline == 0 })
	if len(got.Steps) != 0 || !strings.Contains(got.Reason, "dependency") {
		t.Fatalf("invalid plan persisted: %+v", got)
	}
}
func TestStoreConcurrentMutationAndAccessIsolation(t *testing.T) {
	store, task := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Change(task.ID, "", "", "", func(t *Task) error { t.Tokens++; return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _ := store.Get(task.ID)
	if got.Tokens != 12 {
		t.Fatalf("lost updates: %d", got.Tokens)
	}
	rows, err := store.List("other", database.RBACScopeOwn, 50, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("cross-owner list leak: %d %v", len(rows), err)
	}
	rows, err = store.List("owner", database.RBACScopeOwn, 50, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("own list: %d %v", len(rows), err)
	}
	if !store.DB.UserCanAccessResource("owner", database.RBACScopeOwn, "assessment", task.ID) {
		t.Fatal("assessment resource owner lookup missing")
	}
	if store.DB.UserCanAccessResource("other", database.RBACScopeOwn, "assessment", task.ID) {
		t.Fatal("resource access leaked")
	}
	opts, err := store.DB.ListAssignableRBACResources("assessment", "Local", 50)
	if err != nil || len(opts) != 1 {
		t.Fatalf("assignment picker: %v %v", opts, err)
	}
	count, err := store.DB.CountAssignableRBACResources("assessment", "Local")
	if err != nil || count != 1 {
		t.Fatal(fmt.Sprint(count, err))
	}
}

func TestCandidatePublicationIsAtomicAndIdempotent(t *testing.T) {
	store, _ := fixture(t)
	if err := store.DB.BootstrapRBAC("fixture", map[string]string{"assessment:read": "Read"}); err != nil {
		t.Fatal(err)
	}
	owner, err := store.DB.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.PublishCandidate("stable-id", "", owner.ID, "Candidate", "Observed", "exec-1", "assessment:fixture"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count int
	store.DB.QueryRow(`SELECT count(*) FROM vulnerabilities WHERE id='stable-id' AND owner_user_id=?`, owner.ID).Scan(&count)
	if count != 1 {
		t.Fatal("candidate or owner missing")
	}
	store.DB.QueryRow(`SELECT count(*) FROM rbac_resource_assignments WHERE resource_type='vulnerability' AND resource_id='stable-id' AND user_id=?`, owner.ID).Scan(&count)
	if count != 1 {
		t.Fatal("assignment missing or duplicated")
	}
	if _, err = store.DB.Exec(`UPDATE vulnerabilities SET title='Reviewed' WHERE id='stable-id'`); err != nil {
		t.Fatal(err)
	}
	if err = store.PublishCandidate("stable-id", "", owner.ID, "Candidate", "Observed", "exec-1", "assessment:fixture"); err != nil {
		t.Fatal(err)
	}
	v, err := store.DB.GetVulnerability("stable-id")
	if err != nil || v.Title != "Reviewed" {
		t.Fatal("reviewer edits overwritten", err)
	}
}

func TestRecoveryHandlesCrashDuringTerminalCleanup(t *testing.T) {
	store, task := fixture(t)
	if err := store.Change(task.ID, "", "", "", func(t *Task) error { t.Status = "completed"; t.Deadline = time.Now().Unix() + 100; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(task.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("deleted before cleanup completed", err)
	}
	if err := store.Recover(); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "paused" || current.Deadline != 0 {
		t.Fatalf("unsettled terminal task stuck after restart: %+v", current)
	}
	if err = store.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
}
