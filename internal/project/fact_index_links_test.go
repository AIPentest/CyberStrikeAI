package project

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestFormatIncomingLinksHint(t *testing.T) {
	t.Parallel()
	hint := FormatIncomingLinksHint([]*database.ProjectFactEdge{
		{EdgeType: "discovered_on", SourceFactKey: "finding/x", Confidence: "tentative"},
	})
	if !strings.Contains(hint, "incoming:") {
		t.Fatalf("expected incoming label: %q", hint)
	}
	if !strings.Contains(hint, "discovered_on←finding/x") {
		t.Fatalf("unexpected hint: %q", hint)
	}
	if !strings.Contains(hint, "tentative") {
		t.Fatalf("expected tentative in hint: %q", hint)
	}
}

func TestFormatIncomingLinksHint_allEdges(t *testing.T) {
	t.Parallel()
	edges := make([]*database.ProjectFactEdge, 0, 5)
	for i := 1; i <= 5; i++ {
		edges = append(edges, &database.ProjectFactEdge{
			EdgeType:      "discovered_on",
			SourceFactKey: fmt.Sprintf("finding/f%d", i),
			Confidence:    "tentative",
		})
	}
	hint := FormatIncomingLinksHint(edges)
	if strings.Contains(hint, "+") {
		t.Fatalf("should not truncate with +N: %q", hint)
	}
	for i := 1; i <= 5; i++ {
		if !strings.Contains(hint, fmt.Sprintf("finding/f%d", i)) {
			t.Fatalf("missing edge f%d in hint: %q", i, hint)
		}
	}
}

func TestFormatFactIndexLinksHint_incomingOnly(t *testing.T) {
	t.Parallel()
	in := []*database.ProjectFactEdge{
		{EdgeType: "discovered_on", SourceFactKey: "target/dev", Confidence: "tentative"},
		{EdgeType: "exploits", SourceFactKey: "exploit/rce", Confidence: "confirmed"},
	}
	hint := FormatFactIndexLinksHint("finding/sqli", in)
	if !strings.Contains(hint, "links:") {
		t.Fatalf("missing links label: %q", hint)
	}
	if !strings.Contains(hint, "discovered_on←target/dev") {
		t.Fatalf("missing discovered_on: %q", hint)
	}
	if !strings.Contains(hint, "exploits←exploit/rce") {
		t.Fatalf("missing exploits: %q", hint)
	}
	if strings.Contains(hint, "outgoing") || strings.Contains(hint, "incoming") {
		t.Fatalf("should not use legacy outgoing/incoming labels: %q", hint)
	}
}

func TestFormatFactIndexLinksHint_includesAuxiliaryEdgeTypes(t *testing.T) {
	t.Parallel()
	in := []*database.ProjectFactEdge{{EdgeType: "supports", SourceFactKey: "note/log"}}
	hint := FormatFactIndexLinksHint("finding/x", in)
	if !strings.Contains(hint, "supports←note/log") {
		t.Fatalf("supports edge should be included: %q", hint)
	}
}

func TestBuildFactPathOverviewSection(t *testing.T) {
	t.Parallel()
	edges := []*database.ProjectFactEdge{
		{EdgeType: "discovered_on", SourceFactKey: "target/dev", TargetFactKey: "finding/sqli", Confidence: "tentative"},
		{EdgeType: "exploits", SourceFactKey: "exploit/rce", TargetFactKey: "finding/sqli", Confidence: "confirmed"},
		{EdgeType: "supports", SourceFactKey: "note/log", TargetFactKey: "finding/sqli"},
	}
	keys := map[string]struct{}{
		"target/dev": {}, "finding/sqli": {}, "exploit/rce": {}, "note/log": {},
	}
	section := BuildFactPathOverviewSection(edges, keys, 800)
	if !strings.Contains(section, "### Attack path (fact relations)") {
		t.Fatalf("missing header: %q", section)
	}
	if !strings.Contains(section, "target/dev → finding/sqli") {
		t.Fatalf("missing discovered_on line: %q", section)
	}
	if !strings.Contains(section, "exploit/rce → finding/sqli") {
		t.Fatalf("missing exploits line: %q", section)
	}
	if !strings.Contains(section, "note/log → finding/sqli") {
		t.Fatalf("supports edge should be included: %q", section)
	}
}

func TestBuildFactPathOverviewSectionFailsClosedWithoutIndexedFacts(t *testing.T) {
	t.Parallel()
	edges := []*database.ProjectFactEdge{
		{EdgeType: "discovered_on", SourceFactKey: "target/dev", TargetFactKey: "finding/sqli"},
	}
	for name, keys := range map[string]map[string]struct{}{"nil": nil, "empty": {}} {
		if section := BuildFactPathOverviewSection(edges, keys, 800); section != "" {
			t.Fatalf("%s indexed set still related facts: %q", name, section)
		}
	}
}

func TestBuildFactIndexBlock_withLinksAndPathOverview(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "facts.db")
	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	proj, err := db.CreateProject(&database.Project{Name: "path-proj"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.UpsertProjectFact(&database.ProjectFact{
		ProjectID:  proj.ID,
		FactKey:    "target/dev",
		Category:   "target",
		Summary:    "dev 子域",
		Confidence: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.UpsertProjectFact(&database.ProjectFact{
		ProjectID:  proj.ID,
		FactKey:    "finding/sqli",
		Category:   "finding",
		Summary:    "时间盲注",
		Confidence: "tentative",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AddProjectFactEdge(proj.ID, database.ProjectFactEdgeInput{
		To:   "finding/sqli",
		Type: "discovered_on",
	}, "target/dev", "")
	if err != nil {
		t.Fatal(err)
	}

	block, err := BuildFactIndexBlock(db, proj.ID, config.ProjectConfig{Enabled: true, FactIndexMaxRunes: 6500, FactIndexPathMaxRunes: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "links: discovered_on←target/dev") {
		t.Fatalf("finding line should include relation hint: %q", block)
	}
	if !strings.Contains(block, "### Attack path (fact relations)") {
		t.Fatalf("missing relation overview: %q", block)
	}
	if !strings.Contains(block, "target/dev → finding/sqli") {
		t.Fatalf("missing overview edge: %q", block)
	}
}
