package deepassessment

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestIntentPauseAndResumeDoNotCancelSibling(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "one", Status: "pending"}, {ID: "two", Status: "pending"}}
		return nil
	})
	var one, two atomic.Int32
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: func(ctx context.Context, _ *Task, st Step, _ func(string, string) error) (Outcome, error) {
		if st.ID == "one" {
			one.Add(1)
		} else {
			two.Add(1)
		}
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool { return one.Load() == 1 && two.Load() == 1 })
	if err := e.ControlStep(task.ID, "one", "pause", "", 0, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool { return v.Step("one").Status == "paused" && e.Live(task.ID).Workers == 1 })
	if current.Status != "running" || current.Step("two").Status != "running" || !current.Step("one").UserPaused {
		t.Fatal(current)
	}
	if err := e.ControlStep(task.ID, "two", "priority", "", 1, current.Revision); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if err := e.ControlStep(task.ID, "one", "resume", "", 0, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool { return one.Load() == 2 })
	if two.Load() != 1 || current.Step("two").Priority != 1 {
		t.Fatal("sibling was restarted", current)
	}
	if err := e.Stop(task.ID, current.Revision, false); err != nil {
		t.Fatal(err)
	}
	await(t, s, task.ID, func(v *Task) bool { return v.Status == "paused" })
}

func TestIntentPauseSurvivesTaskStartAndPlannerCannotRetireIt(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "held", Instruction: "held", Status: "pending"}, {ID: "other", Instruction: "other", Status: "pending"}}
		return nil
	})
	var held, other atomic.Int32
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { return Proposal{}, nil }, run: func(ctx context.Context, _ *Task, st Step, _ func(string, string) error) (Outcome, error) {
		if st.ID == "held" {
			held.Add(1)
		} else {
			other.Add(1)
		}
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	if err := e.ControlStep(task.ID, "held", "pause", "", 0, current.Revision); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if err := ApplyProposal(current, Proposal{Retire: []string{"held"}}); err == nil {
		t.Fatal("planner overrode a user-paused intent")
	}
	current, _ = s.Get(task.ID)
	if err := e.Start(task.ID, task.OwnerID, current.Revision); err != nil {
		t.Fatal(err)
	}
	current = await(t, s, task.ID, func(v *Task) bool { return other.Load() == 1 })
	if held.Load() != 0 || current.Step("held").Status != "paused" {
		t.Fatal(current)
	}
	e.Close()
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if current.Step("held").Status != "paused" {
		t.Fatal(current)
	}
}

func TestIntentResumeCannotUseAnotherExecutorsPendingRecovery(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Status = "waiting_progress"
		v.Steps = []Step{{ID: "held", Status: "paused", UserPaused: true}}
		return nil
	})
	e := NewEngine(s, fakeRuntime{})
	t.Cleanup(e.Close)
	current, _ := s.Get(task.ID)
	for _, action := range []string{"resume", "guidance"} {
		if err := e.ControlStepAs(task.ID, "held", action, "direction", 100, current.Revision, "different-executor"); err == nil {
			t.Fatal("other executor controlled active recovery", action)
		}
	}
	current, _ = s.Get(task.ID)
	if !current.Step("held").UserPaused || len(current.Step("held").Guidance) > 0 {
		t.Fatal(current)
	}
}
