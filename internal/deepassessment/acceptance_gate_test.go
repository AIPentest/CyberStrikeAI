package deepassessment

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/agentruntime"
)

func TestPerItemAcceptanceFourStatesUseOneVersion(t *testing.T) {
	s, task := perItemTask(t)
	if gate := mustGate(t, s, task.ID); !gate.Blocking || gate.Legacy || !strings.Contains(gate.Reason, "没有对应证据") {
		t.Fatalf("all empty: %+v", gate)
	}
	if err := s.BeginCall(task.ID, "s", agentruntime.ToolCall{ID: "receipt-1", Name: "fixture_read"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCall(agentruntime.ToolCall{ID: "receipt-1"}, agentruntime.ToolResult{Content: "observed"}, false); err != nil {
		t.Fatal(err)
	}
	current := mustTask(t, s, task.ID)
	if err := s.SaveAcceptanceMapping(current, 1, AcceptanceMapping{ReceiptIDs: []string{"receipt-1"}}); err != nil {
		t.Fatal(err)
	}
	if gate := mustGate(t, s, task.ID); !gate.Blocking || !strings.Contains(gate.Reason, "没有对应证据") {
		t.Fatalf("partial: %+v", gate)
	}
	current = mustTask(t, s, task.ID)
	if err := s.SaveAcceptanceMapping(current, 2, AcceptanceMapping{ReceiptIDs: []string{"receipt-1"}, NeedsReview: true}); err != nil {
		t.Fatal(err)
	}
	if gate := mustGate(t, s, task.ID); !gate.Blocking || !strings.Contains(gate.Reason, "待复核") {
		t.Fatalf("needs review: %+v", gate)
	}
	current = mustTask(t, s, task.ID)
	if err := s.SaveAcceptanceMapping(current, 2, AcceptanceMapping{Revision: 1, ReceiptIDs: []string{"receipt-1"}}); err != nil {
		t.Fatal(err)
	}
	satisfied := mustGate(t, s, task.ID)
	if satisfied.Blocking || satisfied.Legacy || satisfied.Version == "" {
		t.Fatalf("all satisfied: %+v", satisfied)
	}
	current = mustTask(t, s, task.ID)
	if err := s.SaveAcceptanceMapping(current, 1, AcceptanceMapping{Revision: 1, ReceiptIDs: []string{"receipt-1"}, NeedsReview: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteAtGate(task.ID, 0, current.Revision, "should not finish", satisfied); err != ErrConflict {
		t.Fatalf("stale acceptance snapshot finished the task: %v", err)
	}
	if got := mustTask(t, s, task.ID); got.Status != "running" {
		t.Fatalf("stale completion changed status to %s", got.Status)
	}
	current = mustTask(t, s, task.ID)
	if err := s.SaveAcceptanceMapping(current, 1, AcceptanceMapping{Revision: 2, ReceiptIDs: []string{"receipt-1"}}); err != nil {
		t.Fatal(err)
	}
	fresh := mustGate(t, s, task.ID)
	current = mustTask(t, s, task.ID)
	if err := s.CompleteAtGate(task.ID, 0, current.Revision, "every line has evidence", fresh); err != nil {
		t.Fatal(err)
	}
	if got := mustTask(t, s, task.ID); got.Status != "completed" || got.Summary != "every line has evidence" {
		t.Fatalf("current acceptance snapshot did not complete: %+v", got)
	}
}

func TestLegacyAcceptanceStaysOpenUntilAMappingIsSaved(t *testing.T) {
	s, task := fixture(t)
	gate := mustGate(t, s, task.ID)
	if !gate.Legacy || gate.Blocking {
		t.Fatalf("old task without a saved mapping: %+v", gate)
	}
	if err := s.SaveAcceptanceMapping(task, 1, AcceptanceMapping{NeedsReview: true}); err != nil {
		t.Fatal(err)
	}
	gate = mustGate(t, s, task.ID)
	if gate.Legacy || !gate.Blocking || !strings.Contains(gate.Reason, "待复核") {
		t.Fatalf("saved mapping did not enable the gate: %+v", gate)
	}
}

func perItemTask(t *testing.T) (*Store, *Task) {
	t.Helper()
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "mode", "", func(v *Task) error {
		v.AcceptanceMode = AcceptanceModePerItem
		v.Criteria = "inventory holds\nreview holds"
		v.Status = "running"
		v.Steps = []Step{{ID: "s", Status: "completed", Summary: "done", ConversationID: "c"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, mustTask(t, s, task.ID)
}

func mustTask(t *testing.T, s *Store, id string) *Task {
	t.Helper()
	task, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func mustGate(t *testing.T, s *Store, id string) AcceptanceGate {
	t.Helper()
	gate, err := s.AcceptanceGate(mustTask(t, s, id))
	if err != nil {
		t.Fatal(err)
	}
	return gate
}
