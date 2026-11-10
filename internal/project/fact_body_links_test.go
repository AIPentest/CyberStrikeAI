package project

import (
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestParseLinksFromBodyDependsOn(t *testing.T) {
	t.Parallel()
	body := "## Related\n- Dependency facts: target/api\n- Related fact_key: auth/session"
	links := ParseLinksFromBody(body)
	if len(links) != 2 {
		t.Fatalf("want 2 links, got %d", len(links))
	}
}

func TestSyncBodyLinksSection(t *testing.T) {
	t.Parallel()
	body := "## Conclusion\nx\n\n## Related\n- Dependency facts: old/key"
	edges := []*database.ProjectFactEdge{{EdgeType: "discovered_on", SourceFactKey: "target/a"}}
	out := SyncBodyLinksSection(body, edges)
	if !strings.Contains(out, "discovered_on: target/a") {
		t.Fatalf("missing synced edge: %q", out)
	}
}

func TestFactGraphIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p, err := db.CreateProject(&database.Project{Name: "g"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ key, cat, summary string }{
		{"target/root", "target", "root"},
		{"finding/x", "finding", "finding x"},
	} {
		_, err := db.UpsertProjectFact(&database.ProjectFact{
			ProjectID: p.ID, FactKey: spec.key, Category: spec.cat, Summary: spec.summary, Confidence: "confirmed",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ReplaceIncomingProjectFactEdges(p.ID, "finding/x", []database.ProjectFactEdgeFromInput{
		{From: "target/root", Type: "discovered_on"},
	}); err != nil {
		t.Fatal(err)
	}
	graph, err := BuildProjectFactGraph(db, p.ID, "path", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) < 2 || len(graph.Edges) < 1 {
		t.Fatalf("expected graph nodes/edges, got %d/%d", len(graph.Nodes), len(graph.Edges))
	}
}
