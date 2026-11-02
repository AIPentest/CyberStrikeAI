package app

import (
	"context"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/assessment"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/security"
	"go.uber.org/zap"
)

func TestAssessmentMCPUsesProjectWithoutConversation(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "assessment.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.BootstrapRBAC("fixture", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	user, err := db.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := db.CreateProject(&database.Project{Name: "bound"})
	b, _ := db.CreateProject(&database.Project{Name: "other"})
	own := &database.Asset{Host: "https://bound.example.test", ProjectID: a.ID}
	other := &database.Asset{Host: "https://other.example.test", ProjectID: b.ID}
	if _, err = db.UpsertAssets([]*database.Asset{own, other}, user.ID, true); err != nil {
		t.Fatal(err)
	}
	task := &assessment.Task{ID: "assessment-fixture", ExecutorID: user.ID, OwnerID: user.ID, ProjectID: a.ID, Tools: []string{builtin.ToolGetAsset}}
	server := mcp.NewServer(zap.NewNop())
	server.SetToolAuthorizer(mcpToolAuthorizer(db))
	registerAssetTools(server, db, zap.NewNop())
	cfg := &config.Config{}
	ag := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, server, nil, zap.NewNop(), 5)
	rt := &assessment.CSAIRuntime{DB: db, Agent: ag, Config: cfg}
	for _, test := range []struct {
		id      string
		allowed bool
	}{{own.ID, true}, {other.ID, false}} {
		result, err := rt.Execute(context.Background(), task, "worker", agent.ToolCall{ID: "call", Function: agent.FunctionCall{Name: builtin.ToolGetAsset, Arguments: map[string]interface{}{"id": test.id}}})
		if err != nil || result == nil {
			t.Fatal(err)
		}
		if test.allowed == result.IsError {
			t.Fatalf("project boundary: id=%s error=%t result=%s", test.id, result.IsError, result.Result)
		}
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM conversations`).Scan(&count)
	if count != 0 {
		t.Fatal("hidden conversation created")
	}
}

func TestBoundAssetMutationCannotChangeAnotherProject(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "bound-mutation.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := db.CreateProject(&database.Project{Name: "a"})
	b, _ := db.CreateProject(&database.Project{Name: "b"})
	asset := &database.Asset{Host: "https://other.example.test", ProjectID: b.ID}
	if _, err = db.UpsertAssets([]*database.Asset{asset}, "admin", true); err != nil {
		t.Fatal(err)
	}
	ctx := authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("admin", "admin", database.RBACScopeAll, map[string]bool{"asset:read": true, "asset:write": true, "asset:delete": true}))
	ctx = mcp.WithMCPProjectID(ctx, a.ID)
	authorize := mcpToolAuthorizer(db)
	for _, name := range []string{builtin.ToolGetAsset, builtin.ToolUpdateAsset, builtin.ToolDeleteAsset} {
		if err = authorize(ctx, name, map[string]interface{}{"id": asset.ID}); err == nil {
			t.Fatal("foreign asset authorized", name)
		}
	}
	if err = authorize(ctx, builtin.ToolCreateAsset, map[string]interface{}{"host": "https://fixture.test", "project_id": b.ID}); err == nil {
		t.Fatal("foreign destination accepted")
	}
}
