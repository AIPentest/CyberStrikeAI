package project

import (
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"go.uber.org/zap"
)

func TestBlackboardWorkingMemoryIsConversationScopedIncludingEdges(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "scope.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*database.ProjectFact{
		{ProjectID: p.ID, FactKey: "finding/shared", Category: "finding", Confidence: "confirmed", Summary: "shared result", Body: "RAW_SHARED"},
		{ProjectID: p.ID, FactKey: "note/context-a", SourceConversationID: "a", Summary: "progress a", Body: "RAW_A"},
		{ProjectID: p.ID, FactKey: "note/context-b", SourceConversationID: "b", Summary: "progress b", Body: "RAW_B", Pinned: true, Confidence: "confirmed"},
		{ProjectID: p.ID, FactKey: "note/context-unknown", Summary: "unknown origin", Pinned: true},
		{ProjectID: p.ID, FactKey: "note/manual", SourceConversationID: "b", Summary: "shared manual note"},
	} {
		if _, err := db.UpsertProjectFact(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.AddProjectFactEdge(p.ID, database.ProjectFactEdgeInput{To: "finding/shared", Type: "supports"}, "note/context-b", "b"); err != nil {
		t.Fatal(err)
	}
	cfg := config.ProjectConfig{Enabled: true, FactIndexMaxRunes: 3500, FactIndexPathMaxRunes: 1000}
	for _, conv := range []string{"a", "b", ""} {
		block, err := BuildFactIndexBlock(db, p.ID, cfg, conv)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(block, "finding/shared") || !strings.Contains(block, "note/manual") || strings.Contains(block, "note/context-unknown") || strings.Contains(block, "RAW_") {
			t.Fatalf("scope altered shared facts or injected bodies: %s", block)
		}
		for _, source := range []string{"a", "b"} {
			if strings.Contains(block, "note/context-"+source) != (source == conv) {
				t.Fatalf("conversation %q mixes source %q: %s", conv, source, block)
			}
		}
		if len([]rune(block)) > cfg.FactIndexMaxRunes {
			t.Fatal("scope labels escaped index budget")
		}
	}
	generic, err := BuildFactIndexBlock(db, p.ID, cfg)
	if err != nil || strings.Contains(generic, "note/context-") {
		t.Fatalf("missing conversation defaults to global working memory: %s %v", generic, err)
	}
	// Human/admin raw queries retain the existing project-wide data view.
	raw, err := db.ListProjectFacts(p.ID, database.ProjectFactListFilter{}, 100, 0)
	if err != nil || len(raw) != 5 {
		t.Fatalf("scope destroyed or globally hid stored notes: %v %v", raw, err)
	}
}

func TestBodyLinkMirrorNeverCarriesConversationWorkingMemory(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "bodyleak.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*database.ProjectFact{
		{ProjectID: p.ID, FactKey: "finding/shared", Category: "finding", Summary: "shared result", Body: "RAW_SHARED"},
		{ProjectID: p.ID, FactKey: "note/context-b", SourceConversationID: "b", Summary: "progress b"},
		{ProjectID: p.ID, FactKey: "target/other", Category: "target", Summary: "ordinary source"},
	} {
		if _, err := db.UpsertProjectFact(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.AddProjectFactEdge(p.ID, database.ProjectFactEdgeInput{To: "finding/shared", Type: "supports"}, "note/context-b", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddProjectFactEdge(p.ID, database.ProjectFactEdgeInput{To: "finding/shared", Type: "discovered_on"}, "target/other", ""); err != nil {
		t.Fatal(err)
	}
	in, err := db.ListIncomingProjectFactEdges(p.ID, "finding/shared")
	if err != nil {
		t.Fatal(err)
	}
	// get_project_fact returns this body verbatim, and bodies have no scope filter.
	body := SyncBodyLinksSection("RAW_SHARED", in)
	if !strings.Contains(body, "target/other") {
		t.Fatalf("body mirror dropped an ordinary relation: %s", body)
	}
	if strings.Contains(body, "note/context-b") {
		t.Fatalf("body mirror leaked conversation-scoped working memory: %s", body)
	}
	if parsed := ParseLinksFromBody(body); len(parsed) != 1 || parsed[0].From != "target/other" {
		t.Fatalf("body mirror re-materialized a hidden edge: %#v from %s", parsed, body)
	}
}
