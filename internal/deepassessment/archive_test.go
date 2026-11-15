package deepassessment

import (
	"context"
	"cyberstrike-ai/internal/database"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestArchivePreservesEvidenceAndRestoresWithoutStarting(t *testing.T) {
	s, task := fixture(t)
	conv, err := s.DB.CreateConversation("archive origin", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Change(task.ID, "", "checkpoint", "retained observation", func(v *Task) error {
		v.Status = "waiting_progress"
		v.Steps = []Step{{ID: "one", Title: "Evidence", Status: "completed", Summary: "retained", ConversationID: conv.ID}}
		wake := time.Now().Add(time.Hour)
		v.NextWake = &wake
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	intentReceipt(t, s, task.ID, "one", "archive-proof")
	finding, err := s.DB.CreateVulnerability(&database.Vulnerability{ConversationID: conv.ID, Title: "Retained finding", Severity: "info", Evidence: "original proof", ReproSteps: "original reproduction"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = s.Get(task.ID)
	var plans atomic.Int32
	e := NewEngine(s, fakeRuntime{plan: func(context.Context, *Task) (Proposal, error) { plans.Add(1); return Proposal{}, nil }})
	t.Cleanup(e.Close)
	if err := e.SetArchived(task.ID, task.Revision, true); err != nil {
		t.Fatal(err)
	}
	archived, _ := s.Get(task.ID)
	if !archived.Archived || archived.ArchivedAt == nil || archived.Status != "paused" || archived.NextWake != nil || archived.Steps[0].Summary != "retained" {
		t.Fatal(archived)
	}
	list, err := s.List(task.OwnerID, "own", "", "", 10, 0)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	list, err = s.List(task.OwnerID, "own", "", "", 10, 0, true)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if _, err = s.Graph(task.ID, "", 80, 0); err != nil {
		t.Fatal("archived graph lost", err)
	}
	var receipt string
	if err = s.DB.QueryRow("SELECT result FROM deep_assessment_calls WHERE id='archive-proof'").Scan(&receipt); err != nil || receipt != "original fixture result" {
		t.Fatal("archived receipt lost", receipt, err)
	}
	savedFinding, err := s.DB.GetVulnerability(finding.ID)
	if err != nil || savedFinding.AssessmentID != task.ID || savedFinding.AssessmentStepID != "one" || savedFinding.Evidence != "original proof" {
		t.Fatal("archived finding association lost", savedFinding, err)
	}
	if err = e.Start(task.ID, task.OwnerID, archived.Revision); err == nil {
		t.Fatal("archived task started")
	}
	if err = e.SetArchived(task.ID, task.Revision, false); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision restored", err)
	}
	if err = e.SetArchived(task.ID, archived.Revision, false); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.Get(task.ID)
	if restored.Archived || restored.ArchivedAt != nil || restored.Status != "paused" || plans.Load() != 0 {
		t.Fatal("restore restarted execution", restored)
	}
	events, _ := s.Events(task.ID, 0)
	if events[len(events)-1].Kind != "restored" {
		t.Fatal(events)
	}
}
func TestArchiveRejectsActiveAndUncertainExecution(t *testing.T) {
	s, task := fixture(t)
	e := NewEngine(s, fakeRuntime{})
	t.Cleanup(e.Close)
	for _, state := range []string{"running", "uncertain", "cleanup_unconfirmed"} {
		s.Change(task.ID, "", "fixture", "", func(v *Task) error { v.Status = "paused"; v.Steps = []Step{{ID: "one", Status: state}}; return nil })
		current, _ := s.Get(task.ID)
		if err := e.SetArchived(task.ID, current.Revision, true); err == nil {
			t.Fatal("archived unfinished execution", state)
		}
	}
	s.Change(task.ID, "", "fixture", "", func(v *Task) error { v.Status = "running"; v.Steps = nil; return nil })
	current, _ := s.Get(task.ID)
	if err := e.SetArchived(task.ID, current.Revision, true); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestArchiveRetainsPendingQuestion(t *testing.T) {
	s, task := fixture(t)
	e := NewEngine(s, fakeRuntime{})
	t.Cleanup(e.Close)
	s.Change(task.ID, "", "question", "", func(v *Task) error { v.Status = "waiting_input"; v.Question = "Choose an environment"; return nil })
	current, _ := s.Get(task.ID)
	if err := e.SetArchived(task.ID, current.Revision, true); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if err := e.SetArchived(task.ID, current.Revision, false); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Get(task.ID)
	if current.Status != "waiting_input" || current.Question != "Choose an environment" {
		t.Fatal(current)
	}
}
