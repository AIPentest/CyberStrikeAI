package deepassessment

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/mcp"
)

func cancelledCallFixture(t *testing.T, s *Store, task *Task, status, conversation, tool string, ended bool) {
	t.Helper()
	if err := s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Status = "needs_review"
		v.AutoResume = true
		v.Steps = []Step{{ID: "one", Title: "fixture", Status: "uncertain", ConversationID: "stable-session"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCall{ID: "native-call", Name: "fixture_action", SessionEntryID: "entry-one", ProviderID: "provider-one", Arguments: map[string]any{}}
	if err := s.BeginCall(task.ID, "one", call); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCall(call, agentruntime.ToolResult{Content: "wait cancelled", ExecutionID: "exec-one", IsError: true}, true); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	execution := &mcp.ToolExecution{ID: "exec-one", ToolName: tool, Status: status, ConversationID: conversation, OwnerUserID: task.OwnerID, Arguments: map[string]any{}, StartTime: now.Add(-time.Second)}
	if ended {
		execution.EndTime = &now
	}
	if err := s.DB.SaveToolExecution(execution); err != nil {
		t.Fatal(err)
	}
}

func TestStartUsesConfirmedCancelledReceiptAndOriginalPiSession(t *testing.T) {
	s, task := fixture(t)
	cancelledCallFixture(t, s, task, "cancelled", "stable-session", "fixture_action", true)
	var runs atomic.Int32
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: func(_ context.Context, _ *Task, step Step, _ func(string, string) error) (Outcome, error) {
		runs.Add(1)
		if step.ConversationID != "stable-session" {
			return Outcome{}, fmt.Errorf("session lost")
		}
		return Outcome{Status: "completed", Summary: "fixture verified", ConversationID: step.ConversationID}, nil
	}})
	t.Cleanup(e.Close)
	before, _ := s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, before.Revision); err != nil {
		t.Fatal(err)
	}
	await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
	if runs.Load() != 1 {
		t.Fatal(runs.Load())
	}
	receipts, err := s.NativeReceipts(task.ID, "one")
	if err != nil || len(receipts) != 1 || !receipts[0].IsError || receipts[0].SessionEntryID != "entry-one" {
		t.Fatal(receipts, err)
	}
	if count, _ := s.CallCount(task.ID, "one"); count != 0 {
		t.Fatal("cancelled call became successful evidence")
	}
	if err := s.FinishCall(agentruntime.ToolCall{ID: "native-call"}, agentruntime.ToolResult{Content: "late cancelled wait", ExecutionID: "exec-one", IsError: true}, true); err != nil {
		t.Fatal(err)
	}
	unknown, err := s.UncertainSteps(task.ID)
	if err != nil || len(unknown) != 0 {
		t.Fatal("late cancellation erased settled receipt", unknown, err)
	}
}

func TestAutomaticReceiptRepairRejectsUnknownOrForeignExecutions(t *testing.T) {
	for _, tc := range []struct {
		name, status, session, tool string
		ended                       bool
	}{
		{"orphaned", "orphaned", "stable-session", "fixture_action", true},
		{"running", "running", "stable-session", "fixture_action", false},
		{"foreign-session", "cancelled", "other-session", "fixture_action", true},
		{"different-tool", "cancelled", "stable-session", "other_tool", true},
		{"no-terminal-time", "cancelled", "stable-session", "fixture_action", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, task := fixture(t)
			cancelledCallFixture(t, s, task, tc.status, tc.session, tc.tool, tc.ended)
			repaired, err := s.ReconcileKnownCalls(task.ID, "")
			if err != nil || len(repaired) != 0 {
				t.Fatal(repaired, err)
			}
			unknown, err := s.UncertainSteps(task.ID)
			if err != nil || !unknown["one"] {
				t.Fatal(unknown, err)
			}
		})
	}
}

func TestPauseCancelsPlannerAndAllWorkersAndRequiresManualResume(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.AutoResume = true
		v.Steps = []Step{{ID: "one", Status: "pending"}, {ID: "two", Status: "pending"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var runs, exited, plans, plannerExited atomic.Int32
	e := NewEngine(s, fakeRuntime{plan: func(ctx context.Context, _ *Task) (Proposal, error) {
		if plans.Add(1) == 1 {
			<-ctx.Done()
			plannerExited.Add(1)
			return Proposal{}, ErrRetryable
		}
		return Proposal{}, nil
	}, run: func(ctx context.Context, _ *Task, step Step, _ func(string, string) error) (Outcome, error) {
		if runs.Add(1) <= 2 {
			<-ctx.Done()
			exited.Add(1)
			return Outcome{}, ErrRetryable
		}
		return Outcome{Status: "completed", Summary: "done", ConversationID: "session-" + step.ID}, nil
	}})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool { return runs.Load() == 2 && plans.Load() == 1 })
	if err := e.Stop(task.ID, current.Revision, false); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return v.Status == "paused" && len(e.active) == 0
	})
	if exited.Load() != 2 || plannerExited.Load() != 1 || current.RestartPending || current.NextWake != nil {
		t.Fatalf("pause did not drain all activity: %+v", current)
	}
	if err := e.start(task.ID, task.OwnerID, current.Revision, true); err != ErrConflict {
		t.Fatalf("scheduler revived manual pause: %v", err)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if current.Status != "paused" || runs.Load() != 2 {
		t.Fatal(current, runs.Load())
	}
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
	if runs.Load() != 4 {
		t.Fatal(runs.Load())
	}
}

func TestQueuedRecoveryCanBePausedWithoutBeingRevived(t *testing.T) {
	for _, status := range []string{"recovering", "waiting_retry", "waiting_progress"} {
		t.Run(status, func(t *testing.T) {
			s, task := fixture(t)
			wake := time.Now().Add(time.Hour)
			if err := s.Change(task.ID, "", "", "", func(v *Task) error { v.Status = status; v.NextWake = &wake; v.RestartPending = true; return nil }); err != nil {
				t.Fatal(err)
			}
			e := NewEngine(s, fakeRuntime{})
			t.Cleanup(e.Close)
			current, _ := s.Get(task.ID)
			if err := e.Stop(task.ID, current.Revision, false); err != nil {
				t.Fatal(err)
			}
			current, _ = s.Get(task.ID)
			if current.Status != "paused" || current.NextWake != nil || current.RestartPending {
				t.Fatal(current)
			}
			if err := e.start(task.ID, task.OwnerID, current.Revision, true); err != ErrConflict {
				t.Fatal(err)
			}
		})
	}
}

func TestPartialRepairDoesNotBlockLaterStartAfterRemainingReview(t *testing.T) {
	s, task := fixture(t)
	cancelledCallFixture(t, s, task, "cancelled", "stable-session", "fixture_action", true)
	if err := s.Change(task.ID, "", "", "", func(v *Task) error { v.Steps = append(v.Steps, Step{ID: "unknown", Status: "uncertain"}); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginCall(task.ID, "unknown", agentruntime.ToolCall{ID: "unknown-call", Name: "fixture_action"}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: successful})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err == nil {
		t.Fatal("unresolved call was bypassed")
	}
	current, _ = s.Get(task.ID)
	if err := s.RetryStep(task.ID, "unknown", current.Revision, true); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
}
