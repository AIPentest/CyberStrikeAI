package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"go.uber.org/zap"
)

func TestProjectMemoryToolScopeAndHistoricalOptIn(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "memory.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, _ := db.CreateProject(&database.Project{Name: "current"})
	pOther, _ := db.CreateProject(&database.Project{Name: "other"})
	current, _ := db.CreateConversation("current", database.ConversationCreateMeta{ProjectID: p.ID})
	history, _ := db.CreateConversation("history", database.ConversationCreateMeta{ProjectID: p.ID})
	outside, _ := db.CreateConversation("outside", database.ConversationCreateMeta{ProjectID: pOther.ID})
	for _, f := range []*database.ProjectFact{
		{ProjectID: p.ID, FactKey: "note/context-own", SourceConversationID: current.ID, Summary: "own progress", Body: "own raw", Confidence: "tentative"},
		{ProjectID: p.ID, FactKey: "note/context-history", SourceConversationID: history.ID, Summary: "historic progress", Body: "HISTORIC_COOKIE", Pinned: true},
		{ProjectID: p.ID, FactKey: "note/context-legacy", Summary: "unknown old progress", Body: "UNKNOWN_RAW", Pinned: true},
		{ProjectID: p.ID, FactKey: "finding/shared", SourceConversationID: history.ID, Summary: "shared confirmed result", Body: "SHARED_PROOF", Confidence: "confirmed", Category: "finding"},
		{ProjectID: p.ID, FactKey: "note/manual", SourceConversationID: history.ID, Summary: "manual shared note"},
		{ProjectID: pOther.ID, FactKey: "note/context-outside", SourceConversationID: outside.ID, Summary: "other project", Body: "OUTSIDE_PROJECT"},
	} {
		if _, err := db.UpsertProjectFact(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.AddProjectFactEdge(p.ID, database.ProjectFactEdgeInput{To: "finding/shared", Type: "supports"}, "note/context-history", history.ID); err != nil {
		t.Fatal(err)
	}
	user, err := db.CreateRBACUser("memory-test", "Memory", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := authctx.NewPrincipal(user.ID, user.Username, database.RBACScopeAll, map[string]bool{"project:read": true, "project:write": true})
	ctx := authctx.WithPrincipal(mcp.WithMCPConversationID(context.Background(), current.ID), principal)
	server := mcp.NewServer(zap.NewNop())
	server.SetToolAuthorizer(mcpToolAuthorizer(db))
	registerProjectFactTools(server, db, &config.Config{Project: config.ProjectConfig{Enabled: true}}, zap.NewNop())
	call := func(name string, args map[string]interface{}) *mcp.ToolResult {
		t.Helper()
		result, _, err := server.CallTool(ctx, name, args)
		if err != nil || result == nil {
			t.Fatalf("%s: %v %v", name, result, err)
		}
		return result
	}
	text := func(result *mcp.ToolResult) string {
		var b strings.Builder
		for _, part := range result.Content {
			b.WriteString(part.Text)
		}
		return b.String()
	}
	for _, name := range []string{builtin.ToolListProjectFacts, builtin.ToolSearchProjectFacts} {
		args := map[string]interface{}{"query": "progress"}
		result := call(name, args)
		body := text(result)
		if result.IsError || strings.Contains(body, "note/context-history") || strings.Contains(body, "note/context-legacy") || !strings.Contains(body, "note/context-own") {
			t.Fatalf("default retrieval mixed sessions: %s", body)
		}
		args["include_history"] = true
		args["source_conversation_id"] = history.ID
		result = call(name, args)
		body = text(result)
		if result.IsError || !strings.Contains(body, "note/context-history") || !strings.Contains(body, "历史参考") || !strings.Contains(body, history.ID) || !strings.Contains(body, "更新时间=") || strings.Contains(body, "note/context-own") {
			t.Fatalf("explicit history lost provenance: %s", body)
		}
	}
	page := text(call(builtin.ToolListProjectFacts, map[string]interface{}{"limit": 1}))
	if strings.Contains(page, "共 0 条") || strings.Contains(page, "note/context-history") || strings.Contains(page, "note/context-legacy") {
		t.Fatalf("scope filtering occurred after pagination: %s", page)
	}
	shared := call(builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "finding/shared"})
	if shared.IsError || !strings.Contains(text(shared), "SHARED_PROOF") || strings.Contains(text(shared), "note/context-history") {
		t.Fatalf("shared fact missing or relation leaked history: %s", text(shared))
	}
	manual := call(builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "note/manual"})
	if manual.IsError {
		t.Fatal("ordinary project notes must remain shared")
	}
	hidden := call(builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "note/context-history"})
	if !hidden.IsError || strings.Contains(text(hidden), "HISTORIC_COOKIE") {
		t.Fatal("default get exposes foreign checkpoint")
	}
	visible := call(builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "note/context-history", "include_history": true})
	if visible.IsError || !strings.Contains(text(visible), "HISTORIC_COOKIE") || !strings.Contains(text(visible), "历史参考") {
		t.Fatalf("explicit history unavailable: %s", text(visible))
	}
	unknown := call(builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "note/context-legacy", "include_history": true})
	if unknown.IsError || !strings.Contains(text(unknown), "未知来源") || !strings.Contains(text(unknown), "历史参考") {
		t.Fatal("unknown origin presented as current memory")
	}
	outsideRead := call(builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "note/context-outside", "include_history": true, "project_id": pOther.ID})
	if !outsideRead.IsError || strings.Contains(text(outsideRead), "OUTSIDE_PROJECT") {
		t.Fatal("history opt-in crossed project boundary")
	}
	for _, name := range []string{builtin.ToolDeprecateProjectFact, builtin.ToolRestoreProjectFact, builtin.ToolUpsertProjectFact} {
		result := call(name, map[string]interface{}{"fact_key": "note/context-history", "include_history": true, "summary": "overwrite", "body": "bad"})
		if !result.IsError {
			t.Fatalf("%s modified another conversation checkpoint", name)
		}
	}
	denied := authctx.WithPrincipal(mcp.WithMCPConversationID(context.Background(), current.ID), authctx.NewPrincipal(user.ID, user.Username, database.RBACScopeAll, map[string]bool{}))
	_, _, err = server.CallTool(denied, builtin.ToolGetProjectFact, map[string]interface{}{"fact_key": "note/context-history", "include_history": true})
	if err == nil {
		t.Fatal("history flag bypassed project:read permission")
	}
}
