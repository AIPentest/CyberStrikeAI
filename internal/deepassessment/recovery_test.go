package deepassessment

import (
	"context"
	"cyberstrike-ai/internal/agentruntime"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveryBlocksUnreceiptedCallWithoutTaskDeadline(t *testing.T) {
	s, task := fixture(t)
	deadline := time.Now().UTC().Add(48 * time.Hour)
	if err := s.Change(task.ID, "", "", "", func(v *Task) error {
		v.Status = "running"
		v.AutoResume = true
		v.Deadline = &deadline
		v.Steps = []Step{{ID: "one", Status: "running", ConversationID: "stable-session", Summary: "saved progress"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCall{ID: "original-call", Name: "fixture_action", Arguments: map[string]any{"item": "fixture"}}
	if err := s.BeginCall(task.ID, "one", call); err != nil {
		t.Fatal(err)
	}
	reloaded := &Store{DB: s.DB}
	if err := reloaded.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if got.Status != "needs_review" || got.Step("one").Status != "uncertain" || got.Step("one").ConversationID != "stable-session" || got.Deadline != nil {
		t.Fatalf("unsafe recovery: %+v", got)
	}
	if err := s.RetryStep(task.ID, "one", got.Revision, false); err == nil {
		t.Fatal("uncertain action resumed without reconciliation")
	}
	if err := s.RetryStep(task.ID, "one", got.Revision, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(task.ID)
	if got.Step("one").Status != "pending" || got.Step("one").ConversationID != "stable-session" {
		t.Fatal(got)
	}
	unknown, err := s.UncertainSteps(task.ID)
	if err != nil || len(unknown) != 0 {
		t.Fatal(unknown, err)
	}
}

func TestSafeRestartAutomaticallyResumesOriginalSession(t *testing.T) {
	s, task := fixture(t)
	deadline := time.Now().UTC().Add(48 * time.Hour)
	s.Change(task.ID, "", "", "", func(v *Task) error {
		v.Status = "running"
		v.AutoResume = true
		v.Deadline = &deadline
		v.Steps = []Step{{ID: "one", Title: "resume", Instruction: "continue fixture", Status: "running", ConversationID: "stable-session"}}
		return nil
	})
	reloaded := &Store{DB: s.DB}
	if err := reloaded.Recover(); err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	runtime := fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: func(_ context.Context, _ *Task, step Step, _ func(string, string) error) (Outcome, error) {
		runs.Add(1)
		if step.ConversationID != "stable-session" {
			return Outcome{}, fmt.Errorf("lost session identity")
		}
		return Outcome{Status: "completed", Summary: "verified", ConversationID: step.ConversationID}, nil
	}}
	engine := NewEngine(reloaded, runtime)
	t.Cleanup(engine.Close)
	engine.StartRecovery()
	got := await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
	if runs.Load() != 1 || got.Deadline != nil {
		t.Fatal(got, runs.Load())
	}
}

func TestMultipleReviewsRemainBlockedAndNeverStartOnAcknowledgement(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "", "", func(v *Task) error {
		v.Status, v.AutoResume, v.RestartPending = "running", true, true
		v.Steps = []Step{{ID: "done", Status: "completed", ConversationID: "completed-session"}, {ID: "one", Status: "running", ConversationID: "session-one"}, {ID: "two", Status: "running", ConversationID: "session-two"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := s.BeginCall(task.ID, id, agentruntime.ToolCall{ID: "call-" + id, Name: "fixture_action", Arguments: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if err := s.RetryStep(task.ID, "one", got.Revision, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(task.ID)
	if got.Status != "needs_review" || got.Step("one").Status != "pending" || got.Step("two").Status != "uncertain" || got.RestartPending {
		t.Fatalf("remaining review lost: %+v", got)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(task.ID)
	if got.Status != "needs_review" {
		t.Fatal(got.Status)
	}
	if err := s.RetryStep(task.ID, "two", got.Revision, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(task.ID)
	if got.Status != "paused" || got.RestartPending || got.NextWake != nil || got.Step("one").ConversationID != "session-one" || got.Step("done").Status != "completed" {
		t.Fatalf("review should preserve history and await manual start: %+v", got)
	}
	unknown, err := s.UncertainSteps(task.ID)
	if err != nil || len(unknown) != 0 {
		t.Fatal(unknown, err)
	}
}

func TestRecoveryDoesNotRestartUserControlledStates(t *testing.T) {
	for _, status := range []string{"draft", "paused", "needs_review", "waiting_input", "completed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			s, task := fixture(t)
			if err := s.Change(task.ID, "", "", "", func(v *Task) error { v.Status = status; v.AutoResume = true; return nil }); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Get(task.ID)
			if err := s.Recover(); err != nil {
				t.Fatal(err)
			}
			after, _ := s.Get(task.ID)
			if after.Status != status || after.Revision != before.Revision {
				t.Fatalf("unexpected automatic transition: %+v", after)
			}
		})
	}
}

func TestReviewedStepRetainsPendingQuestion(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "", "", func(v *Task) error {
		v.Status, v.Question = "needs_review", "Clarify the fixture requirement"
		v.Steps = []Step{{ID: "one", Status: "cleanup_unconfirmed"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if err := s.RetryStep(task.ID, "one", got.Revision, true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(task.ID)
	if got.Status != "waiting_input" || got.Question == "" {
		t.Fatal(got)
	}
}

func TestSegmentYieldsContinueWithPersistedProgress(t *testing.T) {
	s, task := fixture(t)
	var runs atomic.Int32
	runtime := fakeRuntime{plan: func(_ context.Context, t *Task) (Proposal, error) {
		if len(t.Steps) == 0 {
			return Proposal{Steps: []Step{{ID: "one", Title: "work", Instruction: "fixture"}}}, nil
		}
		return Proposal{}, nil
	}, run: func(_ context.Context, _ *Task, step Step, checkpoint func(string, string) error) (Outcome, error) {
		n := runs.Add(1)
		if n > 1 && step.ConversationID != "persistent" {
			return Outcome{}, fmt.Errorf("lost identity")
		}
		if err := checkpoint("persistent", fmt.Sprint(n)); err != nil {
			return Outcome{}, err
		}
		state := "yielded"
		if n == 4 {
			state = "completed"
		}
		return Outcome{Status: state, Progress: fmt.Sprint(n), Summary: "checkpoint", ConversationID: "persistent"}, nil
	}}
	engine := NewEngine(s, runtime)
	start(t, engine, task)
	got := await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
	if runs.Load() != 4 || got.Step("one").Attempts != 4 {
		t.Fatal(got, runs.Load())
	}
}

func TestPiUsageSnapshotsIncludeCompactionWithoutDoubleCounting(t *testing.T) {
	s, task := fixture(t)
	for _, value := range []float64{100, 100, 160, 120} {
		if err := s.RecordMetrics(task.ID, "one", map[string]any{"sessionId": "pi-one", "tokens": value, "compactions": float64(2)}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Get(task.ID)
	if got.TokensUsed != 160 || got.Metrics["pi-one"].Compactions != 2 {
		t.Fatal(got)
	}
	if err := s.ValidateEvidence(task.ID, []string{"invented"}); err == nil {
		t.Fatal("invented evidence accepted")
	}
	call := agentruntime.ToolCall{ID: "receipt", SessionEntryID: "native-entry", ProviderID: "provider-call", Name: "fixture", Arguments: map[string]any{}}
	if err := s.BeginCall(task.ID, "one", call); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateEvidence(task.ID, []string{"receipt"}); err == nil {
		t.Fatal("in-flight receipt accepted")
	}
	if err := s.FinishCall(call, agentruntime.ToolResult{Content: "observed", ExecutionID: "execution"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateEvidence(task.ID, []string{"execution"}); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.NativeReceipts(task.ID, "one")
	if err != nil || len(receipts) != 1 || receipts[0].SessionEntryID != "native-entry" || receipts[0].ProviderID != "provider-call" {
		t.Fatal(receipts, err)
	}
	if err := s.ValidateEvidence("foreign", []string{"execution"}); err == nil {
		t.Fatal("cross-task evidence accepted")
	}
}

func TestExplicitPauseIsNotAutomaticallyResumed(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "", "", func(t *Task) error { t.AutoResume = true; return nil })
	task, _ = s.Get(task.ID)
	started := make(chan struct{})
	runtime := fakeRuntime{plan: func(_ context.Context, t *Task) (Proposal, error) {
		if len(t.Steps) == 0 {
			return Proposal{Steps: []Step{{ID: "one", Title: "wait", Instruction: "wait for fixture"}}}, nil
		}
		return Proposal{}, nil
	}, run: func(ctx context.Context, _ *Task, _ Step, _ func(string, string) error) (Outcome, error) {
		close(started)
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}}
	e := NewEngine(s, runtime)
	start(t, e, task)
	<-started
	current, _ := s.Get(task.ID)
	if err := e.Stop(task.ID, current.Revision, false); err != nil {
		t.Fatal(err)
	}
	got := await(t, s, task.ID, func(v *Task) bool { return v.RunStartedAt == nil })
	if got.Status != "paused" || got.RestartPending {
		t.Fatalf("explicit pause would restart: %+v", got)
	}
}

func TestGracefulServiceRestartRetainsAutomaticRecovery(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "", "", func(t *Task) error { t.AutoResume = true; return nil })
	task, _ = s.Get(task.ID)
	started := make(chan struct{})
	runtime := fakeRuntime{plan: func(_ context.Context, t *Task) (Proposal, error) {
		if len(t.Steps) == 0 {
			return Proposal{Steps: []Step{{ID: "one", Title: "wait", Instruction: "wait for fixture"}}}, nil
		}
		return Proposal{}, nil
	}, run: func(ctx context.Context, _ *Task, _ Step, _ func(string, string) error) (Outcome, error) {
		close(started)
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}}
	e := NewEngine(s, runtime)
	if err := e.Start(task.ID, task.OwnerID, task.Revision); err != nil {
		t.Fatal(err)
	}
	<-started
	e.Close()
	got, _ := s.Get(task.ID)
	if got.Status != "recovering" || !got.RestartPending {
		t.Fatalf("shutdown lost resume intent: %+v", got)
	}
}

func TestRestartDuringDrainDistinguishesUserPauseFromServiceShutdown(t *testing.T) {
	s, task := fixture(t)
	for _, restart := range []bool{false, true} {
		if err := s.Change(task.ID, "", "", "", func(v *Task) error { v.Status = "pausing"; v.AutoResume = true; v.RestartPending = restart; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := s.Recover(); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(task.ID)
		want := "paused"
		if restart {
			want = "recovering"
		}
		if got.Status != want {
			t.Fatalf("restart=%v got=%s want=%s", restart, got.Status, want)
		}
	}
}

func TestIdenticalSuccessfulCallsDoNotCountAsNewProgress(t *testing.T) {
	s, task := fixture(t)
	for _, id := range []string{"first", "second"} {
		call := agentruntime.ToolCall{ID: id, Name: "fixture", Arguments: map[string]any{"key": "same"}}
		if err := s.BeginCall(task.ID, "one", call); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishCall(call, agentruntime.ToolResult{Content: "same observation"}, false); err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.CallCount(task.ID, "one")
	if err != nil || count != 1 {
		t.Fatalf("duplicate progress: %d %v", count, err)
	}
}

func TestContinuousStepPassesFormerSegmentLimit(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "long", Title: "long", Instruction: "continue work", Status: "pending", Attempts: 512}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.Get(task.ID)
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: successful})
	start(t, e, task)
	got := await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
	if got.Step("long").Attempts != 513 {
		t.Fatal(got.Steps)
	}
}

func TestTransientFailureBeyondOldRetryCountWaitsForRecovery(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "retry_scheduled", "", func(v *Task) error { v.Status = "waiting_retry"; v.RetryCount = 9; return nil })
	task, _ = s.Get(task.ID)
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) {
		return Proposal{}, fmt.Errorf("%w: fixture outage", ErrRetryable)
	}, run: successful})
	start(t, e, task)
	got := await(t, s, task.ID, func(v *Task) bool { return v.Status == "waiting_retry" && v.RetryCount == 10 })
	if got.NextWake == nil {
		t.Fatal("retry was not scheduled")
	}
}
