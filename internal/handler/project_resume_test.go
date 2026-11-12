package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/project"
	"go.uber.org/zap"
)

func TestSessionContextReloadsProjectResultsEveryTurn(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "context.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := db.CreateProject(&database.Project{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Project: config.ProjectConfig{Enabled: true}}
	cfg.Agent.WorkspaceRootDir = t.TempDir()
	h := &AgentHandler{db: db, config: cfg, logger: zap.NewNop()}
	var ids []string
	for _, title := range []string{"yesterday", "today"} {
		c, err := db.CreateConversation(title, database.ConversationCreateMeta{ProjectID: p.ID})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	before := h.agentSessionContextBlock(ids[0])
	if strings.Contains(before, "parse_capture.py") {
		t.Fatal("unexpected old file")
	}
	root := project.WorkspaceRootDir(cfg.Agent.WorkspaceRootDir, p.ID, ids[0])
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", "parse_capture.py"), []byte("print('done')"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertProjectFact(&database.ProjectFact{ProjectID: p.ID, FactKey: "artifact/parser", Category: "note", Summary: "verified parser", Body: "artifacts/parse_capture.py input capture.txt; output verified", Confidence: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	v, err := db.CreateVulnerability(&database.Vulnerability{ProjectID: p.ID, Title: "tracked-finding", Severity: "low"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		block := h.agentSessionContextBlock(id)
		for _, want := range []string{"artifacts/parse_capture.py", "input capture.txt", v.ID, "tracked-finding", "先恢复成果"} {
			if !strings.Contains(block, want) {
				t.Errorf("conversation %s missing %s", id, want)
			}
		}
		if strings.Contains(block, "print('done')") {
			t.Fatal("script content injected without reading")
		}
	}
	// A separate project's workspace and findings must never be injected.
	other, err := db.CreateProject(&database.Project{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateConversation("isolated", database.ConversationCreateMeta{ProjectID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	isolated := h.agentSessionContextBlock(c.ID)
	if strings.Contains(isolated, "parse_capture.py") || strings.Contains(isolated, v.ID) {
		t.Fatal("project results leaked")
	}
}
