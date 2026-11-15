package deepassessment

import (
	"testing"
	"time"

	"cyberstrike-ai/internal/agentruntime"
)

func TestRecoverStoppedDrainNormalizesStepsWithoutRestarting(t *testing.T) {
	for _, status := range []string{"cancelling", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			s, task := fixture(t)
			if err := s.Change(task.ID, "", "seed", "", func(v *Task) error {
				v.Status, v.AutoResume, v.RestartPending = status, true, true
				wake := time.Now().Add(time.Hour)
				v.NextWake = &wake
				v.Steps = []Step{{ID: "unknown", Status: "running", ConversationID: "saved-session"}, {ID: "clear", Status: "running"}, {ID: "held", Status: "pausing", UserPaused: true}, {ID: "done", Status: "completed", Summary: "saved evidence"}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.BeginCall(task.ID, "unknown", agentruntime.ToolCall{ID: "unsettled", Name: "fixture"}); err != nil {
				t.Fatal(err)
			}
			if err := s.Recover(); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Get(task.ID)
			if got.Status != "cancelled" || got.RestartPending || got.NextWake != nil || got.Step("unknown").Status != "uncertain" || got.Step("clear").Status != "pending" || got.Step("held").Status != "paused" || got.Step("done").Summary != "saved evidence" {
				t.Fatalf("drain recovery stuck or restarted: %+v", got)
			}
			e := NewEngine(s, fakeRuntime{})
			t.Cleanup(e.Close)
			if err := e.ContinueStopped(task.ID, task.OwnerID, got.Revision); err == nil {
				t.Fatal("continued unknown work")
			}
			if err := s.ReconcileStep(task.ID, "unknown", got.Revision, true); err != nil {
				t.Fatal(err)
			}
			got, _ = s.Get(task.ID)
			if err := e.SetArchived(task.ID, got.Revision, true); err != nil {
				t.Fatal("recovered task cannot be archived", err)
			}
		})
	}
}

func TestRecoveryKeepsCleanupAndQuestionsOutOfAutomaticQueue(t *testing.T) {
	for _, reason := range []string{"cleanup", "question"} {
		t.Run(reason, func(t *testing.T) {
			s, task := fixture(t)
			s.Change(task.ID, "", "seed", "", func(v *Task) error {
				v.Status, v.AutoResume, v.RestartPending = "running", true, true
				if reason == "cleanup" {
					v.Steps = []Step{{ID: "one", Status: "cleanup_unconfirmed"}}
				} else {
					v.Question = "Which fixture?"
				}
				return nil
			})
			if err := s.Recover(); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Get(task.ID)
			want := "needs_review"
			if reason == "question" {
				want = "waiting_input"
			}
			if got.Status != want || got.RestartPending || got.NextWake != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestStalePauseCannotDowngradeStoppingToPaused(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error { v.Status = "cancelling"; return nil })
	e := NewEngine(s, fakeRuntime{})
	t.Cleanup(e.Close)
	got, _ := s.Get(task.ID)
	if err := e.Stop(task.ID, got.Revision, false); err != ErrConflict {
		t.Fatal("pause overwrote stop", err)
	}
	got, _ = s.Get(task.ID)
	if got.Status != "cancelling" {
		t.Fatal(got)
	}
}

func TestRestartRepairsKnownCancellationBeforeChoosingRecoveryState(t *testing.T) {
	s, task := fixture(t)
	cancelledCallFixture(t, s, task, "cancelled", "stable-session", "fixture_action", true)
	s.Change(task.ID, "", "seed", "", func(v *Task) error { v.Status = "recovering"; return nil })
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if got.Status != "recovering" || got.Step("one").Status != "pending" || got.Step("one").ConversationID != "stable-session" {
		t.Fatal(got)
	}
	receipts, err := s.NativeReceipts(task.ID, "one")
	if err != nil || len(receipts) != 1 || !receipts[0].IsError {
		t.Fatal(receipts, err)
	}
}
