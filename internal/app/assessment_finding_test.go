package app

import (
	"context"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"encoding/json"
	"go.uber.org/zap"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordVulnerabilityAssessmentEvidenceCannotBeRedirected(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "fixture.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deep, _ := db.CreateConversation("deep", database.ConversationCreateMeta{})
	ordinary, _ := db.CreateConversation("ordinary", database.ConversationCreateMeta{})
	payload, _ := json.Marshal(map[string]any{"id": "assessment", "steps": []map[string]any{{"id": "intent", "status": "running", "conversation_id": deep.ID}}})
	if _, err = db.Exec("INSERT INTO deep_assessments VALUES('assessment','','','fixture','running',?,?)", string(payload), time.Now()); err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO deep_assessment_sessions VALUES('assessment','intent',?)", deep.ID)
	db.Exec("INSERT INTO deep_assessment_calls(id,task_id,step_id,tool,arguments,status,result,updated_at) VALUES('proof','assessment','intent','fixture_check','{}','completed','original bytes',?)", time.Now())
	server := mcp.NewServer(zap.NewNop())
	server.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error { return nil })
	registerVulnerabilityTools(server, db, zap.NewNop())
	args := map[string]interface{}{"title": "fixture", "description": "fixture impact", "severity": "low", "vulnerability_type": "test", "target": "https://example.test", "reproduction_steps": "read fixture", "evidence": "original bytes", "impact": "fixture only", "recommendation": "correct fixture", "conversation_id": ordinary.ID}
	ctx := mcp.WithMCPConversationID(context.Background(), deep.ID)
	res, _, err := server.CallTool(ctx, builtin.ToolRecordVulnerability, args)
	if err == nil && res != nil && !res.IsError {
		t.Fatal("redirect bypassed assessment evidence")
	}
	args["evidence_receipt_ids"] = []any{"invented"}
	res, _, err = server.CallTool(ctx, builtin.ToolRecordVulnerability, args)
	if err == nil && res != nil && !res.IsError {
		t.Fatal("invented receipt accepted")
	}
	args["evidence_receipt_ids"] = []any{"proof"}
	res, _, err = server.CallTool(ctx, builtin.ToolRecordVulnerability, args)
	if err != nil || res == nil || res.IsError {
		t.Fatal(err, res)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM vulnerability_assessment_sources WHERE assessment_id='assessment'").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	delete(args, "evidence_receipt_ids")
	res, _, err = server.CallTool(mcp.WithMCPConversationID(context.Background(), ordinary.ID), builtin.ToolRecordVulnerability, args)
	if err != nil || res == nil || res.IsError {
		t.Fatal("ordinary/batch behavior changed", err, res)
	}
}
