package multiagent

import (
	"context"
	"cyberstrike-ai/internal/agentruntime"
)

type taskToolsKey struct{}
type taskTools struct {
	definitions []agentruntime.Tool
	execute     agentruntime.ToolExecutor
}

type piOptionsKey struct{}
type PiRunOptions struct {
	AuthorizeContext func(context.Context, agentruntime.ToolCall) (context.Context, error)
	Native           *agentruntime.NativeWorkflow
	BeforeTool       func(context.Context, agentruntime.ToolCall) error
	AfterTool        func(context.Context, agentruntime.ToolCall, agentruntime.ToolResult) error
	Metrics          func(map[string]any)
	Event            func(agentruntime.Event)
	Bind             func(agentruntime.Control) func()
}

func WithPiRunOptions(ctx context.Context, options PiRunOptions) context.Context {
	return context.WithValue(ctx, piOptionsKey{}, options)
}

// WithTaskTools adds host-owned, task-scoped memory/result tools to a single
// Agent run. Tools still pass through the standard authorization/HITL hook.
// They are not part of the global catalog and cannot leak into another run.
func WithTaskTools(ctx context.Context, definitions []agentruntime.Tool, execute agentruntime.ToolExecutor) context.Context {
	return context.WithValue(ctx, taskToolsKey{}, taskTools{append([]agentruntime.Tool(nil), definitions...), execute})
}
