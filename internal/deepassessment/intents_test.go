package deepassessment

import (
	"context"
	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/database"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func intentReceipt(t *testing.T, s *Store, task, step, id string) {
	t.Helper()
	call := agentruntime.ToolCall{ID: id, Name: "fixture_read", Arguments: map[string]any{"file": "fixture"}}
	if err := s.BeginCall(task, step, call); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCall(call, agentruntime.ToolResult{Content: "original fixture result", ExecutionID: "exec-" + id}, false); err != nil {
		t.Fatal(err)
	}
}
func TestIntentFactsPersistLineageAndRejectForeignOrInventedEvidence(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "plan_updated", "", func(v *Task) error {
		v.Steps = []Step{{ID: "one", Title: "inspect", Status: "running"}, {ID: "other", Title: "other", Status: "running"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	intentReceipt(t, s, task.ID, "one", "own")
	intentReceipt(t, s, task.ID, "other", "other")
	for _, refs := range [][]string{nil, {"fake"}, {"other"}} {
		if err := s.AddFact(task.ID, &Fact{StepID: "one", Confidence: "observed", Summary: "claim", Evidence: refs}); err == nil {
			t.Fatal("accepted unsupported observation", refs)
		}
	}
	observed := &Fact{ID: "fact:one", StepID: "one", Confidence: "observed", Summary: "Read original bytes", Body: "fixture result", Evidence: []string{"exec-own"}}
	if err := s.AddFact(task.ID, observed); err != nil {
		t.Fatal(err)
	}
	version, _ := s.ChangeVersion(task.ID)
	if err := s.AddFact(task.ID, observed); err != nil {
		t.Fatal("replayed fact", err)
	}
	after, _ := s.ChangeVersion(task.ID)
	if version != after {
		t.Fatal("replay woke planner twice")
	}
	altered := *observed
	altered.Body = "replaced"
	if err := s.AddFact(task.ID, &altered); err == nil {
		t.Fatal("fact was editable")
	}
	inferred := &Fact{StepID: "one", Confidence: "inferred", Summary: "Hypothesis to test", Sources: []string{observed.ID}}
	if err := s.AddFact(task.ID, inferred); err != nil {
		t.Fatal(err)
	}
	rebuilt := &Store{DB: s.DB}
	facts, err := rebuilt.Facts(task.ID, "", 0)
	if err != nil || len(facts) != 2 {
		t.Fatal(facts, err)
	}
	graph, err := rebuilt.Graph(task.ID, "", 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, e := range graph.Edges {
		found[e.Source+"/"+e.Relation+"/"+e.Target] = true
	}
	for _, key := range []string{"step:one/produces/fact:one", "receipt:own/supports/fact:one", "fact:one/derives/" + inferred.ID} {
		if !found[key] {
			t.Fatal("missing lineage", key)
		}
	}
}
func TestIntentQueueAndAssetSnapshotSurviveNewStore(t *testing.T) {
	s, task := fixture(t)
	asset := &database.Asset{Host: "https://app.example.test", Title: "original"}
	if _, err := s.DB.UpsertAssets([]*database.Asset{asset}, task.OwnerID); err != nil {
		t.Fatal(err)
	}
	if err := s.Change(task.ID, "", "plan_updated", "", func(v *Task) error {
		return ApplyProposal(v, Proposal{Steps: []Step{
			{ID: "later", Title: "later", Instruction: "later direction", Priority: 200, AssetIDs: []string{asset.ID}},
			{ID: "first", Title: "first", Instruction: "first direction", Priority: 1, AssetIDs: []string{asset.ID}},
			{ID: "dependent", Title: "wait", Instruction: "wait for proof", Priority: 1, DependsOn: []string{"first"}},
		}})
	}); err != nil {
		t.Fatal(err)
	}
	rebuilt := &Store{DB: s.DB}
	saved, _ := rebuilt.Get(task.ID)
	q := saved.Queue()
	if q[0].ID != "first" || q[1].ID != "dependent" || saved.Ready(q[1]) {
		t.Fatal(q)
	}
	old, err := rebuilt.IntentAssets(task.ID, "first", []*database.Asset{asset})
	if err != nil {
		t.Fatal(err)
	}
	asset.Title = "changed"
	next, err := rebuilt.IntentAssets(task.ID, "first", []*database.Asset{asset})
	if err != nil || string(old[0]) != string(next[0]) {
		t.Fatal("snapshot drift", err)
	}
	var snapshot database.Asset
	json.Unmarshal(next[0], &snapshot)
	if snapshot.Title != "original" {
		t.Fatal(snapshot)
	}
	if _, err := s.DB.Exec("DELETE FROM assets WHERE id=?", asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuilt.Graph(task.ID, "", 80, 0); err != nil {
		t.Fatal("deleting a shared asset broke historical graph", err)
	}
}
func TestStalePlannerCannotCommitOverNewGraphState(t *testing.T) {
	s, task := fixture(t)
	version, _ := s.ChangeVersion(task.ID)
	tx, _ := s.DB.Begin()
	if err := Signal(tx, task.ID); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	err := s.ChangeAtVersion(task.ID, "", "plan_updated", "old", version, func(v *Task) error { v.Summary = "obsolete"; return nil })
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	saved, _ := s.Get(task.ID)
	if saved.Summary == "obsolete" {
		t.Fatal("stale plan committed")
	}
}
func TestGraphChangeWakesPlannerWhileWorkerIsRunning(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "plan_updated", "", func(v *Task) error {
		v.Steps = []Step{{ID: "long", Title: "long", Instruction: "hold fixture worker", Status: "pending"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.Get(task.ID)
	release := make(chan struct{})
	defer close(release)
	var plans atomic.Int32
	planned := make(chan struct{}, 10)
	runtime := fakeRuntime{
		plan: func(context.Context, *Task) (Proposal, error) {
			plans.Add(1)
			planned <- struct{}{}
			return Proposal{}, nil
		},
		run: func(c context.Context, t *Task, st Step, p func(string, string) error) (Outcome, error) {
			select {
			case <-release:
				return Outcome{Status: "blocked", Summary: "fixture end"}, nil
			case <-c.Done():
				return Outcome{}, c.Err()
			}
		},
	}
	engine := NewEngine(s, runtime)
	start(t, engine, task)
	select {
	case <-planned:
	case <-time.After(4 * time.Second):
		t.Fatal("initial planner did not run")
	}
	tx, _ := s.DB.Begin()
	Signal(tx, task.ID)
	tx.Commit()
	select {
	case <-planned:
	case <-time.After(4 * time.Second):
		t.Fatal("graph change not consumed while worker active")
	}
	if plans.Load() < 2 {
		t.Fatal("planner not woken")
	}
}

func TestGraphSignalWakesWaitingTaskButPreservesManualPause(t *testing.T) {
	s, task := fixture(t)
	future := time.Now().Add(time.Hour)
	if err := s.Change(task.ID, "", "waiting_progress", "", func(v *Task) error { v.Status = "waiting_progress"; v.NextWake = &future; return nil }); err != nil {
		t.Fatal(err)
	}
	paused := *task
	paused.ID = "paused"
	paused.Status = "paused"
	if err := s.Create(&paused); err != nil {
		t.Fatal(err)
	}
	tx, _ := s.DB.Begin()
	Signal(tx, task.ID)
	Signal(tx, paused.ID)
	tx.Commit()
	planned := make(chan string, 10)
	engine := NewEngine(s, fakeRuntime{plan: func(_ context.Context, v *Task) (Proposal, error) { planned <- v.ID; return Proposal{}, nil }})
	t.Cleanup(engine.Close)
	engine.StartRecovery()
	select {
	case id := <-planned:
		if id != task.ID {
			t.Fatal("manual pause resumed", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new graph state did not bypass the idle wake timer")
	}
	got, _ := s.Get(paused.ID)
	if got.Status != "paused" {
		t.Fatal(got.Status)
	}
}
