package project

import (
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestParseFactLinksText(t *testing.T) {
	t.Parallel()
	inputs, err := ParseFactLinksText("discovered_on: target/api\nleads_to: finding/swagger")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("want 2 links, got %d", len(inputs))
	}
	if inputs[0].Type != "discovered_on" || inputs[0].From != "target/api" {
		t.Fatalf("unexpected first link: %+v", inputs[0])
	}
}

func TestParseFactIncomingLinksText(t *testing.T) {
	t.Parallel()
	inputs, err := ParseFactIncomingLinksText("leads_to: finding/swagger\ndepends_on: target/api")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("want 2 links, got %d", len(inputs))
	}
	if inputs[0].Type != "leads_to" || inputs[0].From != "finding/swagger" {
		t.Fatalf("unexpected first link: %+v", inputs[0])
	}
}

func TestFormatFactIncomingLinksText(t *testing.T) {
	t.Parallel()
	text := FormatFactIncomingLinksText([]*database.ProjectFactEdge{
		{EdgeType: "leads_to", SourceFactKey: "finding/a"},
		{EdgeType: "depends_on", SourceFactKey: "target/b"},
	})
	want := "leads_to: finding/a\ndepends_on: target/b"
	if text != want {
		t.Fatalf("got %q want %q", text, want)
	}
}

func TestParseFactLinkInputsEmptyClears(t *testing.T) {
	t.Parallel()
	parsed, err := ParseFactLinkInputs([]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed == nil || parsed.Incoming == nil || len(parsed.Incoming) != 0 {
		t.Fatalf("empty array should clear incoming links, got %v", parsed)
	}
}

func TestParseFactLinkInputsFrom(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"from": "target/primary_domain",
			"type": "discovered_on",
		},
	}
	parsed, err := ParseFactLinkInputs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Incoming) != 1 || parsed.Incoming[0].From != "target/primary_domain" {
		t.Fatalf("unexpected incoming: %+v", parsed.Incoming)
	}
}

func TestParseFactLinkInputsRequiresFrom(t *testing.T) {
	t.Parallel()
	raw := []interface{}{
		map[string]interface{}{
			"to":   "target/primary_domain",
			"type": "discovered_on",
		},
	}
	_, err := ParseFactLinkInputs(raw)
	if err == nil {
		t.Fatal("expected error when from is missing")
	}
}

func TestGraphNodeType(t *testing.T) {
	t.Parallel()
	if GraphNodeType("chain", "chain/x") != "chain" {
		t.Fatal("chain category")
	}
	if GraphNodeType("finding", "finding/x") != "finding" {
		t.Fatal("finding category")
	}
	if GraphNodeType("exploit", "exploit/x") != "exploit" {
		t.Fatal("exploit category")
	}
	if GraphNodeType("finding", "evidence/x") != "finding" {
		t.Fatal("category should override evidence key prefix")
	}
	if GraphNodeType("note", "target/x") != "note" {
		t.Fatal("category should override target key prefix")
	}
	if GraphNodeType("vuln", "finding/x") != "vulnerability" {
		t.Fatal("vuln category maps to vulnerability node type")
	}
	if GraphNodeType("", "target/x") != "target" {
		t.Fatal("empty category falls back to target key prefix")
	}
}

func TestBuildProjectFactGraphPreservesStoredEdgeDirection(t *testing.T) {
	dir := t.TempDir()
	db, err := database.NewDB(filepath.Join(dir, "test.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p, err := db.CreateProject(&database.Project{Name: "path-edges"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ key, cat string }{
		{"target/primary_domain", "target"},
		{"chain/full_attack_path", "chain"},
		{"finding/mysql_public", "finding"},
		{"exploit/mysql_creds_extract", "exploit"},
	} {
		if _, err := db.UpsertProjectFact(&database.ProjectFact{
			ProjectID: p.ID, FactKey: spec.key, Category: spec.cat, Summary: spec.key, Confidence: "confirmed",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ReplaceIncomingProjectFactEdges(p.ID, "finding/mysql_public", []database.ProjectFactEdgeFromInput{
		{From: "target/primary_domain", Type: "discovered_on"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceIncomingProjectFactEdges(p.ID, "finding/mysql_public", []database.ProjectFactEdgeFromInput{
		{From: "target/primary_domain", Type: "discovered_on"},
		{From: "exploit/mysql_creds_extract", Type: "exploits"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceIncomingProjectFactEdges(p.ID, "chain/full_attack_path", []database.ProjectFactEdgeFromInput{
		{From: "target/primary_domain", Type: "discovered_on"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceIncomingProjectFactEdges(p.ID, "exploit/mysql_creds_extract", []database.ProjectFactEdgeFromInput{
		{From: "chain/full_attack_path", Type: "leads_to"},
	}); err != nil {
		t.Fatal(err)
	}

	graph, err := BuildProjectFactGraph(db, p.ID, "path", true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{
		"target/primary_domain|discovered_on|finding/mysql_public":    {},
		"exploit/mysql_creds_extract|exploits|finding/mysql_public":   {},
		"target/primary_domain|discovered_on|chain/full_attack_path":  {},
		"chain/full_attack_path|leads_to|exploit/mysql_creds_extract": {},
	}
	for _, e := range graph.Edges {
		key := e.Source + "|" + e.Type + "|" + e.Target
		delete(want, key)
	}
	if len(want) > 0 {
		t.Fatalf("missing expected stored-direction edges: %v", want)
	}
	countInOut := func(factKey string) (out, in int) {
		for _, e := range graph.Edges {
			if e.Source == factKey {
				out++
			}
			if e.Target == factKey {
				in++
			}
		}
		return out, in
	}
	if out, in := countInOut("chain/full_attack_path"); out != 1 || in != 1 {
		t.Fatalf("chain/full_attack_path want out=1 in=1 got out=%d in=%d", out, in)
	}
	if out, in := countInOut("exploit/mysql_creds_extract"); out != 1 || in != 1 {
		t.Fatalf("exploit/mysql_creds_extract want out=1 in=1 got out=%d in=%d", out, in)
	}
}

func TestPersistFactLinksFromUsesFromAsIncoming(t *testing.T) {
	dir := t.TempDir()
	db, err := database.NewDB(filepath.Join(dir, "test.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p, err := db.CreateProject(&database.Project{Name: "from-links"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ key, cat string }{
		{"target/primary_domain", "target"},
		{"finding/sqli", "finding"},
	} {
		if _, err := db.UpsertProjectFact(&database.ProjectFact{
			ProjectID: p.ID, FactKey: spec.key, Category: spec.cat, Summary: spec.key, Confidence: "confirmed",
		}); err != nil {
			t.Fatal(err)
		}
	}
	parsed := &ParsedFactLinks{
		Incoming: []database.ProjectFactEdgeFromInput{
			{From: "target/primary_domain", Type: "discovered_on"},
		},
	}
	if err := PersistFactLinksFromParsed(db, p.ID, "finding/sqli", "", parsed, false); err != nil {
		t.Fatal(err)
	}
	graph, err := BuildProjectFactGraph(db, p.ID, "path", true)
	if err != nil {
		t.Fatal(err)
	}
	want := "target/primary_domain|discovered_on|finding/sqli"
	for _, e := range graph.Edges {
		key := e.Source + "|" + e.Type + "|" + e.Target
		if key == want {
			return
		}
	}
	t.Fatalf("expected edge %s, got %+v", want, graph.Edges)
}

func TestFormatOutgoingLinksHint(t *testing.T) {
	t.Parallel()
	hint := FormatOutgoingLinksHint([]*database.ProjectFactEdge{
		{EdgeType: "discovered_on", TargetFactKey: "target/a"},
	})
	if hint == "" || hint[0] != ' ' {
		t.Fatalf("unexpected hint: %q", hint)
	}
}

func TestReplaceIncomingAllowsNotYetCreatedSource(t *testing.T) {
	dir := t.TempDir()
	db, err := database.NewDB(filepath.Join(dir, "test.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p, err := db.CreateProject(&database.Project{Name: "parallel-links"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertProjectFact(&database.ProjectFact{
		ProjectID: p.ID, FactKey: "exploit/sqli", Category: "exploit", Summary: "exploit", Confidence: "confirmed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceIncomingProjectFactEdges(p.ID, "exploit/sqli", []database.ProjectFactEdgeFromInput{
		{From: "finding/sqli_endpoint", Type: "exploits"},
	}); err != nil {
		t.Fatalf("incoming edge should not require source fact to exist yet: %v", err)
	}
	if _, err := db.UpsertProjectFact(&database.ProjectFact{
		ProjectID: p.ID, FactKey: "finding/sqli_endpoint", Category: "finding", Summary: "finding", Confidence: "confirmed",
	}); err != nil {
		t.Fatal(err)
	}
	in, err := db.ListIncomingProjectFactEdges(p.ID, "exploit/sqli")
	if err != nil || len(in) != 1 || in[0].SourceFactKey != "finding/sqli_endpoint" {
		t.Fatalf("expected persisted edge from finding, got %+v err=%v", in, err)
	}
}

func TestValidateProjectFactEdgeType(t *testing.T) {
	t.Parallel()
	if err := database.ValidateProjectFactEdgeType("leads_to"); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateProjectFactEdgeType("invalid"); err == nil {
		t.Fatal("expected error")
	}
}
