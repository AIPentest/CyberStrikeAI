package assessment

import (
	"context"
	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"go.uber.org/zap"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
)

func TestAssessmentApprovalPolicy(t *testing.T) {
	r := &CSAIRuntime{Config: &config.Config{}}
	if r.RequiresApproval(&Task{ApprovalMode: "policy"}, "inspect") {
		t.Fatal("global off cannot force per-call review")
	}
	if !r.RequiresApproval(&Task{ApprovalMode: "manual"}, "inspect") {
		t.Fatal("manual policy bypassed")
	}
	r.Config.Hitl.DefaultMode = "approval"
	r.Config.Hitl.ToolWhitelist = []string{"inspect"}
	if r.RequiresApproval(&Task{ApprovalMode: "policy"}, "inspect") {
		t.Fatal("global whitelist ignored")
	}
	if !r.RequiresApproval(&Task{ApprovalMode: "policy"}, "execute") {
		t.Fatal("global approval bypassed")
	}
	if !r.RequiresApproval(&Task{ApprovalMode: "manual"}, "inspect") {
		t.Fatal("manual policy weakened by whitelist")
	}
}

func TestRuntimeWaitsForActualMCPCompletion(t *testing.T) {
	store, task := fixture(t)
	if err := store.DB.BootstrapRBAC("fixture", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	owner, err := store.DB.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	task.OwnerID = owner.ID
	server := mcp.NewServer(zap.NewNop())
	server.ConfigureToolWaitTimeoutSeconds(1)
	server.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error { return nil })
	server.RegisterTool(mcp.Tool{Name: "inspect"}, func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		select {
		case <-time.After(1200 * time.Millisecond):
			return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "actually completed"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	cfg := &config.Config{}
	ag := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, server, nil, zap.NewNop(), 5)
	r := &CSAIRuntime{DB: store.DB, Agent: ag, Config: cfg, Logger: zap.NewNop()}
	started := time.Now()
	result, err := r.Execute(context.Background(), task, "step", agent.ToolCall{ID: "call", Function: agent.FunctionCall{Name: "inspect", Arguments: map[string]interface{}{}}})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 1200*time.Millisecond || result.IsError || !strings.Contains(result.Result, "actually completed") || ag.MCPExecutionStatus(result.ExecutionID) != "completed" {
		t.Fatalf("background submission mistaken for completion: %+v", result)
	}
}
