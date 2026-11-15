package deepassessment

import (
	"fmt"
	"testing"
)

func TestOverviewKeepsTaskBlockerAfterWorkerCleanupAndGuidance(t *testing.T) {
	s, task := fixture(t)
	const reason = "planning failed: fixture protocol ended early"
	if err := s.Change(task.ID, "", "blocked", reason, func(v *Task) error {
		v.Status, v.Summary = "blocked", reason
		v.Steps = []Step{{ID: "done", Status: "completed"}, {ID: "pending", Status: "pending"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if err := s.Change(task.ID, "pending", "step_result", fmt.Sprint("cleanup ", i), func(*Task) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	s.Change(task.ID, "", "guidance", "new direction", func(v *Task) error { v.Summary = "received new guidance"; return nil })
	got, _ := s.Get(task.ID)
	view, err := s.RuntimeOverview(got)
	if err != nil || view.Blocker == nil || view.Blocker.Message != reason || view.Counts["blocked"] != 0 {
		t.Fatal(view, err)
	}
	for _, event := range view.Recent {
		if event.Kind == "blocked" {
			t.Fatal("fixture must place the cause outside recent cleanup events")
		}
	}
	foreign := *task
	foreign.ID = "other"
	foreign.Status = "blocked"
	view, err = s.RuntimeOverview(&foreign)
	if err != nil || view.Blocker != nil {
		t.Fatal("cross-assessment blocker", view, err)
	}
	got.Status = "running"
	view, err = s.RuntimeOverview(got)
	if err != nil || view.Blocker != nil {
		t.Fatal("old blocker shown after continuation", view, err)
	}
}
