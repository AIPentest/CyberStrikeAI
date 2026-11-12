package workflow

import (
	"context"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

type workflowRuntimeCtxKey struct{}

// workflowRuntime carries per-run execution context into Eino Workflow local state.
type workflowRuntime struct {
	args  RunArgs
	runID string
	idx   *graphIndex
	state *WorkflowLocalState
}

func withWorkflowRuntime(ctx context.Context, rt *workflowRuntime) context.Context {
	return context.WithValue(ctx, workflowRuntimeCtxKey{}, rt)
}

func workflowRuntimeFrom(ctx context.Context) *workflowRuntime {
	rt, _ := ctx.Value(workflowRuntimeCtxKey{}).(*workflowRuntime)
	return rt
}

func newWorkflowRuntime(args RunArgs, runID string, idx *graphIndex, inputs map[string]interface{}) *workflowRuntime {
	return &workflowRuntime{
		args:  args,
		runID: runID,
		idx:   idx,
		state: newWorkflowLocalState(inputs, runID),
	}
}

// RunArgs is the execution context for a role-bound workflow run.
type RunArgs struct {
	DB                 *database.DB
	Logger             *zap.Logger
	Role               config.RoleConfig
	AppCfg             *config.Config
	Agent              *agent.Agent
	ConversationID     string
	ProjectID          string
	UserMessage        string
	History            []agent.ChatMessage
	RoleTools          []string
	AgentsMarkdownDir  string
	SystemPromptExtra  string
	AssistantMessageID string
	Progress           agent.ProgressCallback
}

type RunResult struct {
	Response     string
	RunID        string
	Status       string
	AwaitingHITL bool
}
