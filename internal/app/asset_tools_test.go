package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

func TestAssetToolsCRUDQueryAndPageLimit(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "asset-tools.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateRBACUser("asset-agent", "Asset Agent", "hash", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := authctx.NewPrincipal(user.ID, user.Username, database.RBACScopeAssigned, map[string]bool{
		"asset:read": true, "asset:write": true, "asset:delete": true,
	})
	ctx := authctx.WithPrincipal(context.Background(), principal)
	server := mcp.NewServer(zap.NewNop())
	server.SetToolAuthorizer(mcpToolAuthorizer(db))
	registerAssetTools(server, db, zap.NewNop())

	wantTools := map[string]bool{
		builtin.ToolCreateAsset: false, builtin.ToolGetAsset: false, builtin.ToolQueryAssets: false,
		builtin.ToolUpdateAsset: false, builtin.ToolDeleteAsset: false, builtin.ToolCompleteAssetScan: false,
	}
	for _, tool := range server.GetAllTools() {
		if _, ok := wantTools[tool.Name]; ok {
			wantTools[tool.Name] = true
		}
	}
	for name, found := range wantTools {
		if !found {
			t.Fatalf("asset tool not registered: %s", name)
		}
	}

	for _, tool := range server.GetAllTools() {
		if tool.Name != builtin.ToolCreateAsset {
			continue
		}
		for _, keyword := range []string{"oneOf", "allOf", "anyOf"} {
			if _, exists := tool.InputSchema[keyword]; exists {
				t.Fatalf("create asset schema contains Bedrock-incompatible top-level %s", keyword)
			}
		}
	}

	result, _, err := server.CallTool(ctx, builtin.ToolCreateAsset, map[string]interface{}{"title": "Missing target"})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("create asset accepted missing host/ip/domain: result=%#v err=%v", result, err)
	}

	result, _, err = server.CallTool(ctx, builtin.ToolCreateAsset, map[string]interface{}{
		"ip": "192.0.2.42", "port": 443, "protocol": "https", "title": "Before", "tags": []interface{}{"prod"},
	})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("create asset result=%#v err=%v", result, err)
	}
	assets, total, err := db.ListAssets(20, 0, database.AssetListFilter{}, database.RBACListAccess{UserID: user.ID, Scope: database.RBACScopeAssigned})
	if err != nil || total != 1 || len(assets) != 1 {
		t.Fatalf("saved assets total=%d len=%d err=%v", total, len(assets), err)
	}
	id := assets[0].ID

	result, _, err = server.CallTool(ctx, builtin.ToolUpdateAsset, map[string]interface{}{"id": id, "title": "After"})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("update asset result=%#v err=%v", result, err)
	}
	updated, err := db.GetAsset(id, database.RBACListAccess{UserID: user.ID, Scope: database.RBACScopeAssigned})
	if err != nil || updated.Title != "After" || updated.IP != "192.0.2.42" {
		t.Fatalf("partial update lost fields: %#v err=%v", updated, err)
	}

	result, _, err = server.CallTool(ctx, builtin.ToolQueryAssets, map[string]interface{}{
		"sort_by": "last_scan_at", "sort_order": "asc", "page": 1, "page_size": 1,
	})
	if err != nil || result == nil || result.IsError || !strings.Contains(toolResultText(result), "page 1/1") || !strings.Contains(toolResultText(result), "last_scan_at=never") {
		t.Fatalf("query asset result=%#v err=%v", result, err)
	}
	result, _, err = server.CallTool(ctx, builtin.ToolQueryAssets, map[string]interface{}{"page_size": agentAssetPageSizeMax + 1})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("oversized page was accepted: result=%#v err=%v", result, err)
	}

	conversation, err := db.CreateConversation("asset scan", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AssignResourceToUser(user.ID, "conversation", conversation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateVulnerability(&database.Vulnerability{ConversationID: conversation.ID, Title: "finding", Severity: "high", Target: "192.0.2.42"}); err != nil {
		t.Fatal(err)
	}
	scanCtx := mcp.WithMCPConversationID(ctx, conversation.ID)
	result, _, err = server.CallTool(scanCtx, builtin.ToolCompleteAssetScan, map[string]interface{}{"id": id})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("complete scan result=%#v err=%v", result, err)
	}
	scanned, err := db.GetAsset(id, database.RBACListAccess{UserID: user.ID, Scope: database.RBACScopeAssigned})
	if err != nil || scanned.LastScanAt == nil || scanned.LastScanConversationID != conversation.ID || scanned.VulnerabilityCount != 1 {
		t.Fatalf("scan fields not updated: %#v err=%v", scanned, err)
	}

	result, _, err = server.CallTool(ctx, builtin.ToolDeleteAsset, map[string]interface{}{"id": id})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("delete asset result=%#v err=%v", result, err)
	}
	if _, err := db.GetAsset(id, database.RBACListAccess{Scope: database.RBACScopeAll}); err == nil {
		t.Fatal("asset still exists after delete")
	}
}

func toolResultText(result *mcp.ToolResult) string {
	var b strings.Builder
	if result == nil {
		return ""
	}
	for _, content := range result.Content {
		b.WriteString(content.Text)
	}
	return b.String()
}

func TestAssetReadToolsRespectConversationProjectScope(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "asset-project-scope.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projectA, err := db.CreateProject(&database.Project{Name: "Project A"})
	if err != nil {
		t.Fatal(err)
	}
	projectB, err := db.CreateProject(&database.Project{Name: "Project B"})
	if err != nil {
		t.Fatal(err)
	}
	assets := []*database.Asset{
		{ProjectID: projectA.ID, IP: "192.0.2.10", Protocol: "https"},
		{ProjectID: projectB.ID, IP: "192.0.2.20", Protocol: "https"},
		{IP: "192.0.2.30", Protocol: "https"},
	}
	if result, err := db.UpsertAssets(assets, "", true); err != nil || result.Created != len(assets) {
		t.Fatalf("seed assets result=%#v err=%v", result, err)
	}

	bound, err := db.CreateConversation("bound", database.ConversationCreateMeta{ProjectID: projectA.ID})
	if err != nil {
		t.Fatal(err)
	}
	unbound, err := db.CreateConversation("unbound", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	principal := authctx.NewPrincipal("admin", "admin", database.RBACScopeAll, map[string]bool{"asset:read": true})
	ctx := authctx.WithPrincipal(context.Background(), principal)
	server := mcp.NewServer(zap.NewNop())
	server.SetToolAuthorizer(mcpToolAuthorizer(db))
	registerAssetTools(server, db, zap.NewNop())

	boundCtx := mcp.WithMCPConversationID(ctx, bound.ID)
	result, _, err := server.CallTool(boundCtx, builtin.ToolQueryAssets, map[string]interface{}{})
	text := toolResultText(result)
	if err != nil || result == nil || result.IsError || !strings.Contains(text, assets[0].ID) || strings.Contains(text, assets[1].ID) || strings.Contains(text, assets[2].ID) {
		t.Fatalf("bound query escaped project scope: result=%#v text=%q err=%v", result, text, err)
	}

	// Even an explicit foreign project_id cannot override the conversation boundary.
	result, _, err = server.CallTool(boundCtx, builtin.ToolQueryAssets, map[string]interface{}{"project_id": projectB.ID})
	text = toolResultText(result)
	if err != nil || result == nil || result.IsError || !strings.Contains(text, assets[0].ID) || strings.Contains(text, assets[1].ID) {
		t.Fatalf("project_id overrode conversation scope: result=%#v text=%q err=%v", result, text, err)
	}

	result, _, err = server.CallTool(boundCtx, builtin.ToolGetAsset, map[string]interface{}{"id": assets[1].ID})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("bound get read a foreign-project asset: result=%#v err=%v", result, err)
	}

	unboundCtx := mcp.WithMCPConversationID(ctx, unbound.ID)
	result, _, err = server.CallTool(unboundCtx, builtin.ToolQueryAssets, map[string]interface{}{"page_size": 10})
	text = toolResultText(result)
	if err != nil || result == nil || result.IsError || !strings.Contains(text, assets[0].ID) || !strings.Contains(text, assets[1].ID) || !strings.Contains(text, assets[2].ID) {
		t.Fatalf("unbound query did not retain all-assets behavior: result=%#v text=%q err=%v", result, text, err)
	}
}
