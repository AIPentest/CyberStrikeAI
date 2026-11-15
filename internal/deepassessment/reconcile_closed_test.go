package deepassessment

import (
	"context"
	"sync/atomic"
	"testing"

	"cyberstrike-ai/internal/agentruntime"
)

func TestReconcileClosedAssessmentPreservesTerminalStateAndEvidence(t *testing.T) {
	for _, status := range []string{"cancelled", "completed"} {
		t.Run(status, func(t *testing.T) {
			s, task := fixture(t)
			s.Change(task.ID, "", "seed", "", func(v *Task) error {
				v.Status = status
				v.TokensUsed = 123
				v.Summary = "preserve final conclusions"
				v.Steps = []Step{{ID: "one", Status: "uncertain", ConversationID: "original-session", Summary: "original progress"}, {ID: "two", Status: "cleanup_unconfirmed"}, {ID: "done", Status: "completed"}}
				return nil
			})
			call := agentruntime.ToolCall{ID: "original-call", Name: "fixture-read"}
			s.BeginCall(task.ID, "one", call)
			s.FinishCall(call, agentruntime.ToolResult{Content: "original uncertain receipt", IsError: true}, true)
			before, _ := s.Get(task.ID)
			if err := s.ReconcileStep(task.ID, "one", before.Revision, false); err == nil {
				t.Fatal("confirmation missing")
			}
			if err := s.ReconcileStep(task.ID, "one", before.Revision-1, true); err != ErrConflict {
				t.Fatal(err)
			}
			if err := s.RetryStep(task.ID, "one", before.Revision, true); err != ErrConflict {
				t.Fatal("retry revived ended task", err)
			}
			if err := s.ReconcileStep(task.ID, "one", before.Revision, true); err != nil {
				t.Fatal(err)
			}
			current, _ := s.Get(task.ID)
			if current.Status != status || current.Step("one").Status != "reconciled" || current.Step("done").Status != "completed" || current.Step("one").ConversationID != "original-session" || current.TokensUsed != 123 || current.Summary != before.Summary {
				t.Fatal(current)
			}
			list, err := s.List(task.OwnerID, "own", "", "", 20, 0)
			if err != nil || len(list) != 1 || list[0].ReviewCount != 1 {
				t.Fatal(list, err)
			}
			original, err := s.Node(task.ID, "receipt:original-call", 0)
			if err != nil || original["node"].(Node).Body != "original uncertain receipt" || original["node"].(Node).State != "reconciled" {
				t.Fatal(original, err)
			}
			if err = s.ReconcileStep(task.ID, "two", current.Revision, true); err != nil {
				t.Fatal(err)
			}
			current, _ = s.Get(task.ID)
			rev := current.Revision
			if err = s.ReconcileStep(task.ID, "one", current.Revision, true); err != nil {
				t.Fatal(err)
			}
			if err = s.Recover(); err != nil {
				t.Fatal(err)
			}
			current, _ = s.Get(task.ID)
			if current.Status != status || current.Revision != rev || current.RestartPending || current.NextWake != nil {
				t.Fatal("acknowledgement started work or changed terminal state", current)
			}
			list, _ = s.List(task.OwnerID, "own", "", "", 20, 0)
			if list[0].ReviewCount != 0 {
				t.Fatal(list)
			}
		})
	}
}

func TestOnlyExplicitContinueReopensStoppedAssessment(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Status = "cancelled"
		v.AutoResume = true
		v.Steps = []Step{{ID: "done", Status: "completed", Summary: "verified result", ConversationID: "completed-session"}, {ID: "remaining", Status: "reconciled", ConversationID: "saved-session"}}
		return nil
	})
	var runs atomic.Int32
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: func(_ context.Context, _ *Task, st Step, _ func(string, string) error) (Outcome, error) {
		runs.Add(1)
		if st.ID != "remaining" || st.ConversationID != "saved-session" {
			t.Error("replayed completed work or lost session", st)
		}
		return Outcome{Status: "completed", Summary: "done", ConversationID: st.ConversationID}, nil
	}})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err != ErrConflict {
		t.Fatal(err)
	}
	if err := e.start(task.ID, task.OwnerID, current.Revision, true); err != ErrConflict {
		t.Fatal(err)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if current.Status != "cancelled" {
		t.Fatal(current)
	}
	if err := e.ContinueStopped(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool { return v.Status == "completed" })
	if runs.Load() != 1 || current.Step("done").Status != "completed" {
		t.Fatal(runs.Load(), current)
	}
	if err := e.ContinueStopped(task.ID, task.OwnerID, current.Revision); err == nil {
		t.Fatal("completed assessment reopened")
	}
}

func TestStoppedContinuationKeepsPendingQuestion(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error { v.Status = "cancelled"; v.Question = "Need fixture input"; return nil })
	e := NewEngine(s, fakeRuntime{})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	if err := e.ContinueStopped(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if current.Status != "waiting_input" || current.Question == "" || e.Live(task.ID).Active {
		t.Fatal(current)
	}
}
