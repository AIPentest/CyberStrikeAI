package deepassessment

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestArchiveKeepsFindingsAndProjectsBranchLineage(t *testing.T) {
	s, task := fixture(t)
	if err := s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "a", Title: "branch A", Instruction: "A", Status: "completed"}, {ID: "b", Title: "branch B", Instruction: "B", Status: "completed", DependsOn: []string{"a"}}, {ID: "other", Title: "separate", Instruction: "other", Status: "completed"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tx, _ := s.DB.Begin()
	for _, n := range []Node{{ID: "fact:b", StepID: "b", Kind: "fact", State: "observed", Title: "original fact", Body: "original"}, {ID: "finding:kept", StepID: "b", Kind: "finding", State: "confirmed", Title: "confirmed finding", Body: "proof"}} {
		if err := graphNode(tx, task.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	graphEdge(tx, task.ID, "step:b", "fact:b", "produces")
	graphEdge(tx, task.ID, "fact:b", "finding:kept", "supports")
	graphEdge(tx, task.ID, "step:b", "finding:kept", "produces")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareGraph(task.ID); err != nil {
		t.Fatal(err)
	}
	s.Change(task.ID, "", "advance", "", func(v *Task) error { v.Rounds = 10; return nil })
	g, err := s.GraphViewVisible(task.ID, "", "summary", 80, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]Node{}
	archives := 0
	for _, n := range g.Nodes {
		visible[n.ID] = n
		if n.Kind == "digest" {
			archives++
			record, _ := s.Node(task.ID, n.ID, 0)
			var source struct {
				Members []string `json:"members"`
			}
			json.Unmarshal(record["node"].(Node).Source, &source)
			if len(source.Members) > 1 && strings.Contains(strings.Join(source.Members, ","), "other") {
				t.Fatal("unrelated branches were batched")
			}
		}
	}
	if visible["finding:kept"].ID == "" || archives != 2 {
		t.Fatal(g)
	}
	proof := false
	for _, edge := range g.Edges {
		if edge.Target == "finding:kept" && edge.Relation == "supports" && edge.Projection == "archive" {
			proof = true
		}
	}
	if !proof {
		t.Fatal("folding disconnected finding evidence", g.Edges)
	}
	if _, err = s.Node(task.ID, "fact:b", 0); err != nil {
		t.Fatal("original evidence was removed", err)
	}
	hidden, err := s.GraphViewVisible(task.ID, "", "summary", 80, 0, func(n Node) bool { return n.Kind != "finding" })
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range hidden.Nodes {
		if n.Kind == "finding" {
			t.Fatal("private finding leaked")
		}
	}
}

func TestGraphPaginationRetainsBoundariesAndCompleteRelationPaging(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "work", Title: "work", Instruction: "work", Status: "pending"}}
		return nil
	})
	tx, _ := s.DB.Begin()
	for i := 0; i < 110; i++ {
		id := fmt.Sprintf("fact:%03d", i)
		if err := graphNode(tx, task.ID, Node{ID: id, Kind: "fact", State: "observed", Title: id}); err != nil {
			t.Fatal(err)
		}
		graphEdge(tx, task.ID, id, "step:work", "informs")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	g, err := s.GraphViewVisible(task.ID, "", "summary", 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.PrimaryCount != 1 || g.BoundaryTotal != 110 || g.BoundaryMore != 30 || len(g.Nodes) != 82 {
		t.Fatal(g)
	}
	for _, edge := range g.Edges {
		if edge.Source != "goal" && !strings.HasPrefix(edge.Source, "fact:") {
			t.Fatal(edge)
		}
	}
	seen := map[string]bool{}
	for offset := 0; ; {
		result, err := s.RelationsVisible(task.ID, "step:work", offset, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range result["relations"].([]RelationReference) {
			seen[ref.Node.ID] = true
		}
		if !result["relations_has_more"].(bool) {
			break
		}
		offset = result["relations_next_offset"].(int)
	}
	if len(seen) != 111 {
		t.Fatal("cross-page relations were lost", len(seen))
	}
}

func TestSummaryViewRetainsExecutionProvenanceAndFullView(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error { v.Steps = []Step{{ID: "one", Status: "running"}}; return nil })
	tx, _ := s.DB.Begin()
	graphNode(tx, task.ID, Node{ID: "receipt:one", Kind: "receipt", StepID: "one", Title: "output", Body: "original receipt"})
	graphNode(tx, task.ID, Node{ID: "fact:one", Kind: "fact", StepID: "one", Title: "fact"})
	graphEdge(tx, task.ID, "step:one", "receipt:one", "executes")
	graphEdge(tx, task.ID, "receipt:one", "fact:one", "supports")
	tx.Commit()
	summary, err := s.GraphViewVisible(task.ID, "", "summary", 80, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.DetailsHidden != 1 {
		t.Fatal(summary)
	}
	projected := false
	for _, n := range summary.Nodes {
		if n.Kind == "receipt" {
			t.Fatal("raw receipt flooded main view")
		}
	}
	for _, e := range summary.Edges {
		if e.Source == "step:one" && e.Target == "fact:one" && e.Projection == "details" {
			projected = true
		}
	}
	if !projected {
		t.Fatal(summary.Edges)
	}
	full, err := s.GraphViewVisible(task.ID, "receipt:one", "full", 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if full.Nodes[1].ID != "receipt:one" || full.Nodes[1].Boundary {
		t.Fatal("selected node was paginated away", full)
	}
}

func TestRevivedArchiveRequiresContinuousColdRoundsAndKeepsOldLinks(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "old", Title: "old", Instruction: "old direction", Status: "completed"}}
		return nil
	})
	if err := s.PrepareGraph(task.ID); err != nil {
		t.Fatal(err)
	}
	s.Change(task.ID, "", "advance", "", func(v *Task) error { v.Rounds = 7; return nil })
	graph, err := s.Graph(task.ID, "", 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	archive := ""
	for _, n := range graph.Nodes {
		if n.Kind == "digest" {
			archive = n.ID
		}
	}
	if archive == "" {
		t.Fatal("cold branch not archived")
	}
	s.Change(task.ID, "", "revive", "", func(v *Task) error {
		v.Rounds = 8
		return ApplyProposal(v, Proposal{Steps: []Step{{ID: "live", Title: "live", Instruction: "new direction", DependsOn: []string{"old"}}}})
	})
	if _, err = s.Graph(task.ID, "", 80, 0); err != nil {
		t.Fatal(err)
	}
	s.Change(task.ID, "", "settle", "", func(v *Task) error { v.Rounds = 9; v.Step("live").Status = "completed"; return nil })
	contains := func(g Graph, id string) bool {
		for _, n := range g.Nodes {
			if n.ID == id {
				return true
			}
		}
		return false
	}
	graph, _ = s.Graph(task.ID, "", 80, 0)
	if !contains(graph, "step:old") {
		t.Fatal("revived branch folded immediately")
	}
	s.Change(task.ID, "", "advance", "", func(v *Task) error { v.Rounds = 14; return nil })
	graph, _ = s.Graph(task.ID, "", 80, 0)
	if !contains(graph, "step:old") {
		t.Fatal("cold grace window lost")
	}
	s.Change(task.ID, "", "advance", "", func(v *Task) error { v.Rounds = 15; return nil })
	graph, _ = s.Graph(task.ID, "", 80, 0)
	if contains(graph, "step:old") {
		t.Fatal("cold archive did not return")
	}
	if _, err = s.Node(task.ID, archive, 0); err != nil {
		t.Fatal("archive link was removed", err)
	}
}

func TestLegacyArchiveIsSupersededWithoutDeletingItsMembers(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "one", Instruction: "one", Status: "completed"}}
		return nil
	})
	tx, _ := s.DB.Begin()
	graphNode(tx, task.ID, Node{ID: "digest:legacy", Kind: "digest", State: "folded", Title: "legacy", Source: json.RawMessage(`{"members":["step:one"]}`)})
	graphEdge(tx, task.ID, "digest:legacy", "step:one", "contains")
	tx.Commit()
	if _, err := s.Graph(task.ID, "", 80, 0); err != nil {
		t.Fatal(err)
	}
	old, err := s.Node(task.ID, "digest:legacy", 0)
	if err != nil || old["node"].(Node).State != "superseded" {
		t.Fatal(old, err)
	}
	expanded, err := s.Graph(task.ID, "digest:legacy", 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range expanded.Nodes {
		found = found || n.ID == "step:one"
	}
	if !found {
		t.Fatal("old archive no longer expands")
	}
}

func TestScopedGraphRetainsUpstreamChainAcrossPages(t *testing.T) {
	s, task := fixture(t)
	s.Change(task.ID, "", "seed", "", func(v *Task) error {
		v.Steps = []Step{{ID: "a", Status: "pending"}, {ID: "b", Status: "pending", DependsOn: []string{"a"}}, {ID: "c", Status: "pending", DependsOn: []string{"b"}}}
		return nil
	})
	graph, err := s.GraphViewVisible(task.ID, "step:c", "summary", 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]bool{}
	for _, n := range graph.Nodes {
		nodes[n.ID] = true
	}
	if !nodes["goal"] || !nodes["step:a"] || !nodes["step:b"] || !nodes["step:c"] || len(graph.Edges) != 3 {
		t.Fatal("upstream path was cut at the page boundary", graph)
	}
}
