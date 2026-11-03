package assessment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"
	"go.uber.org/zap"
)

// CSAIRuntime reuses the existing model transport and Agent->MCP execution path.
// The execution namespace is an assessment/step ID, never a chat database row.
type CSAIRuntime struct {
	SkillsRoot     string
	ConfigSnapshot func() (*config.Config, error)
	DB             *database.DB
	Agent          *agent.Agent
	Config         *config.Config
	Logger         *zap.Logger
}

func (r *CSAIRuntime) Context(ctx context.Context, t *Task) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actorID := t.ExecutorID
	if actorID == "" {
		actorID = t.OwnerID
	}
	access, err := r.DB.ResolveRBACAccess(actorID)
	if err != nil {
		return nil, err
	}
	if access == nil || !access.User.Enabled || !access.Permissions["assessment:write"] || !access.Permissions["agent:execute"] {
		return nil, fmt.Errorf("assessment operator is disabled or lacks assessment:write / agent:execute")
	}
	principal := authctx.NewPrincipalWithScopes(access.User.ID, access.User.Username, access.Scope, access.Permissions, access.PermissionScopes)
	if !r.DB.UserCanAccessResource(principal.UserID, principal.ScopeFor("assessment:write"), "assessment", t.ID) {
		return nil, fmt.Errorf("operator no longer has write access to this assessment")
	}
	if t.ProjectID != "" && (!principal.HasPermission("project:read") || !r.DB.UserCanAccessResource(principal.UserID, principal.ScopeFor("project:read"), "project", t.ProjectID)) {
		return nil, fmt.Errorf("operator no longer has access to the linked project")
	}
	return mcp.WithMCPProjectID(authctx.WithPrincipal(ctx, principal), t.ProjectID), nil
}
func (r *CSAIRuntime) Tools(t *Task) ([]agent.Tool, error) {
	if r.Agent == nil {
		return nil, fmt.Errorf("agent runtime unavailable")
	}
	// Conversation/project-memory mutation tools require chat provenance and cannot
	// be called under a fabricated conversation. Publishing is an explicit API action.
	available := r.Agent.ToolsForRole(nil)
	out := []agent.Tool{}
	for _, tool := range available {
		selected := false
		for _, key := range t.Tools {
			if key == tool.Function.Name {
				selected = true
			}
		}
		if !selected {
			continue
		}
		if _, ok := tool.Function.Parameters["properties"].(map[string]interface{}); ok {
			props := tool.Function.Parameters["properties"].(map[string]interface{})
			if props["conversation_id"] != nil || props["conversationId"] != nil {
				continue
			}
		}
		name := strings.ToLower(tool.Function.Name)
		if name == memoryTool || name == checkpointTool {
			continue
		}
		if strings.Contains(name, "project_fact") || strings.Contains(name, "context_checkpoint") || name == "record_vulnerability" {
			continue
		}
		out = append(out, tool)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no selected conversation-independent tools are available")
	}
	return out, nil
}
func (r *CSAIRuntime) Complete(ctx context.Context, t *Task, msgs []agent.ChatMessage, tools []agent.Tool, max int) (*ModelReply, error) {
	ctx, err := r.Context(ctx, t)
	if err != nil {
		return nil, err
	}
	snapshot, err := r.configuration()
	if err != nil {
		return nil, err
	}
	channel := t.Model
	p := requestPurpose(ctx)
	if p.Phase == "review" && t.ReviewerModel != "" {
		channel = t.ReviewerModel
	} else if p.Profile != "" && t.WorkerModels[p.Profile] != "" {
		channel = t.WorkerModels[p.Profile]
	}
	if channel != "" {
		if _, _, ok := snapshot.AI.ResolveChannel(channel); !ok {
			return nil, fmt.Errorf("model channel no longer exists")
		}
	}
	cfg, _, ok := snapshot.ResolveAIChannel(channel)
	if !ok {
		return nil, fmt.Errorf("unknown model channel %q", t.Model)
	}
	message, tokens, err := multiagent.CompleteBackgroundStep(ctx, cfg, msgs, tools, max, r.Logger)
	if err != nil {
		return nil, err
	}
	return &ModelReply{Message: *message, Tokens: tokens}, nil
}
func (r *CSAIRuntime) Execute(ctx context.Context, t *Task, sid string, call agent.ToolCall) (*agent.ToolExecutionResult, error) {
	ctx, err := r.Context(ctx, t)
	if err != nil {
		return nil, err
	}
	allowed := false
	defs, err := r.Tools(t)
	if s := t.Step(sid); s != nil {
		defs, err = r.WorkerTools(t, s)
	}
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		if def.Function.Name == call.Function.Name {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("tool is outside the assessment allowlist")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Reserved execution ID only supplies MCP trace attribution; no chat is created.
	result, err := r.Agent.ExecuteMCPToolForConversation(ctx, "assessment-"+t.ID+"-"+sid, call.Function.Name, call.Function.Arguments)
	if err != nil || result == nil {
		return result, err
	}
	if result.ExecutionID == "" {
		result.IsError = true
		return result, nil
	}
	// An MCP wait timeout is a running handle, not execution evidence. Wait for
	// the recorded terminal state before the worker can cite this operation.
	for {
		status := r.Agent.MCPExecutionStatus(result.ExecutionID)
		if status == mcp.ToolExecutionStatusRunning || status == mcp.ToolExecutionStatusQueued {
			select {
			case <-ctx.Done():
				r.Agent.CancelMCPToolExecutionWithNote(result.ExecutionID, "Assessment run cancelled")
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		if stored := r.Agent.MCPExecutionResultText(result.ExecutionID); stored != "" {
			result.Result = stored
		}
		result.IsError = status != mcp.ToolExecutionStatusCompleted
		result.Blocked = status == mcp.ToolExecutionStatusBlocked
		if status == "" {
			result.Result = "Execution status unavailable; cannot verify completion"
		}
		return result, nil
	}
}

// RequiresApproval honors the global HITL default/whitelist, with an optional
// stricter per-assessment policy. Review/edit is represented as explicit approval
// here: edited calls require replanning rather than silently changing arguments.
func (r *CSAIRuntime) RequiresApproval(t *Task, name string) bool {
	if t.ApprovalMode == "manual" {
		return true
	}
	snapshot, err := r.configuration()
	if err != nil {
		return true
	}
	if snapshot.Hitl.EffectiveDefaultMode() == "off" {
		return false
	}
	for _, allowed := range snapshot.Hitl.ToolWhitelist {
		if strings.EqualFold(strings.TrimSpace(allowed), name) {
			return false
		}
	}
	return true
}

func (r *CSAIRuntime) configuration() (*config.Config, error) {
	if r.ConfigSnapshot != nil {
		return r.ConfigSnapshot()
	}
	if r.Config == nil {
		return nil, fmt.Errorf("configuration unavailable")
	}
	return r.Config, nil
}
