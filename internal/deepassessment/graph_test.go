package deepassessment

import (
	"cyberstrike-ai/internal/agentruntime"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGraphFoldsColdHistoryAndRevivesActiveDependencies(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "seed", "fixture", func(v *Task) error {
		for i := 0; i < 8; i++ {
			v.Steps = append(v.Steps, Step{ID: fmt.Sprint(i), Title: fmt.Sprintf("Direction %d", i), Instruction: fmt.Sprintf("Check %d", i), Summary: "observed negative evidence", Status: "completed", ConversationID: fmt.Sprint(i)})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCall{ID: "call-one", Name: "read_fixture", Arguments: map[string]any{}}
	if err := s.BeginCall(task.ID, "0", call); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCall(call, agentruntime.ToolResult{Content: "original observation", ExecutionID: "receipt-one"}, false); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("history ", 3000) + "native-tail-evidence"
	if err := s.SavePiSummary(task.ID, "0", "fixture-session", "entry", full); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareGraph(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Change(task.ID, "", "planning", "advance", func(v *Task) error { v.Rounds = 10; return nil }); err != nil {
		t.Fatal(err)
	}
	g, err := s.Graph(task.ID, "", 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	digest := ""
	for _, n := range g.Nodes {
		if n.Kind == "digest" {
			digest = n.ID
		}
		if n.Kind == "step" {
			t.Fatal("cold step not folded", n)
		}
	}
	if digest == "" || g.Folded < 8 {
		t.Fatal(g)
	}
	expanded, err := s.Graph(task.ID, digest, 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	memory := ""
	for _, n := range expanded.Nodes {
		if n.Kind == "memory" {
			memory = n.ID
		}
	}
	if memory == "" {
		t.Fatal("native compaction summary lost")
	}
	record, err := s.Node(task.ID, memory, 20000)
	if err != nil || !strings.Contains(record["node"].(Node).Body, "native-tail-evidence") {
		t.Fatal(record, err)
	}
	if _, err = s.Node("foreign", memory, 0); err == nil {
		t.Fatal("cross-task memory exposed")
	}
	if err = s.Change(task.ID, "", "plan_updated", "new branch", func(v *Task) error {
		return ApplyProposal(v, Proposal{Steps: []Step{{ID: "live", Title: "Resume known route", Instruction: "Use new evidence", DependsOn: []string{"0"}, Sources: []string{memory}}}})
	}); err != nil {
		t.Fatal(err)
	}
	g, err = s.Graph(task.ID, "", 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]bool{}
	for _, n := range g.Nodes {
		visible[n.ID] = true
	}
	if !visible["step:0"] || !visible[memory] || !visible["step:live"] {
		t.Fatal("active evidence chain was hidden", g)
	}
	// Reopening the store reads persisted nodes; no model recomputation is needed.
	reloaded := &Store{DB: s.DB}
	if _, err = reloaded.Node(task.ID, memory, 0); err != nil {
		t.Fatal(err)
	}
	nodes, err := s.SearchNodes(task.ID, "native-tail-evidence", "", 0)
	if err != nil || len(nodes) == 0 {
		t.Fatal(nodes, err)
	}
}

func TestUnlimitedStepsAndLegacyBudgetMigration(t *testing.T) {
	s, task := fixture(t)
	base := Task{}
	for i := 0; i < 2050; i++ {
		base.Steps = append(base.Steps, Step{ID: fmt.Sprint(i), Instruction: fmt.Sprintf("prior %d", i), Status: "completed"})
	}
	if err := ApplyProposal(&base, Proposal{Steps: []Step{{ID: "beyond-old-limit", Title: "next", Instruction: "new direction"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE deep_assessments SET status='budget_exhausted',payload=json_set(payload,'$.status','budget_exhausted','$.max_rounds',512,'$.remaining_seconds',1,'$.token_budget',1,'$.deadline','2000-01-01T00:00:00Z') WHERE id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateContinuous(); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(task.OwnerID, "own", "paused", "", 20, 0)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	var raw string
	s.DB.QueryRow(`SELECT payload FROM deep_assessments WHERE id=?`, task.ID).Scan(&raw)
	var data map[string]any
	json.Unmarshal([]byte(raw), &data)
	for _, key := range []string{"deadline", "max_rounds", "token_budget", "remaining_seconds"} {
		if _, ok := data[key]; ok {
			t.Fatal("legacy cap remains", key)
		}
	}
}

func TestGraphSourcesCannotReferToAnotherTask(t *testing.T) {
	s, task := fixture(t)
	if err := s.ValidateSources(task.ID, []Step{{Sources: []string{"foreign-node"}}}); err == nil {
		t.Fatal("foreign source accepted")
	}
	if err := s.Change(task.ID, "", "plan_updated", "invalid", func(v *Task) error {
		v.Steps = []Step{{ID: "s", Title: "s", Status: "pending", Sources: []string{"foreign-node"}}}
		return nil
	}); err == nil {
		t.Fatal("invalid graph source committed")
	}
	got, _ := s.Get(task.ID)
	if len(got.Steps) != 0 {
		t.Fatal("state changed despite graph transaction failure")
	}
}
