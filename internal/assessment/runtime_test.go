package assessment

import (
	"context"
	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/database"
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

func TestRuntimeCollaboratorDoesNotInheritOwnerPrivileges(t *testing.T) {
	store, task := fixture(t)
	if err := store.DB.BootstrapRBAC("fixture", security.PermissionCatalog); err != nil {
		t.Fatal(err)
	}
	owner, err := store.DB.GetRBACUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	role, err := store.DB.UpsertRBACRole("assessment-collaborator", "collaborator", "", database.RBACScopeAssigned, []string{"assessment:write", "agent:execute"})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := store.DB.CreateRBACUser("collaborator", "Collaborator", "fixture", true, []string{role.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DB.AssignResourceToUser(actor.ID, "assessment", task.ID); err != nil {
		t.Fatal(err)
	}
	task.OwnerID = owner.ID
	task.ExecutorID = actor.ID
	r := &CSAIRuntime{DB: store.DB}
	ctx, err := r.Context(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := authctx.PrincipalFromContext(ctx)
	if !ok || p.UserID != actor.ID || p.HasPermission("agent:local-execute") {
		t.Fatal("inherited owner privileges", p)
	}
	if _, err = store.DB.Exec(`DELETE FROM rbac_resource_assignments WHERE user_id=?`, actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Context(context.Background(), task); err == nil {
		t.Fatal("revoked assessment access was ignored")
	}
}
