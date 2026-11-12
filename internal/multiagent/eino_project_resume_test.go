package multiagent

import (
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

func TestCompactionRefreshRestoresLatestProjectResults(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "resume.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.ProjectConfig{Enabled: true}
	old, err := project.BuildFactIndexBlock(db, p.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: p.ID, FactKey: "artifact/parser", Category: "note", Summary: "parser-ready", Body: "artifacts/parser.py --input file", Confidence: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: p.ID, FactKey: "test/entry", Category: "note", Summary: "entry-negative", Body: "bounded-test-evidence", Confidence: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	v, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: p.ID, Title: "existing-finding", Severity: "low", Status: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	msgs := []adk.Message{schema.SystemMessage("custom-instruction\n" + old), schema.UserMessage("continue")}
	out := refreshFactIndexInMessages(msgs, db, p.ID, cfg, nil)
	for _, want := range []string{"custom-instruction", "artifacts/parser.py --input file", "bounded-test-evidence", v.ID, `"status":"fixed"`} {
		if !strings.Contains(out[0].Content, want) {
			t.Errorf("missing %s in refreshed prompt", want)
		}
	}
	if msgs[0].Content != "custom-instruction\n"+old {
		t.Fatal("refresh mutated original system message")
	}
	if out[1] != msgs[1] {
		t.Fatal("refresh changed user intent")
	}
}
