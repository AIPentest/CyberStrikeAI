package multiagent

import (
	"context"
	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"github.com/google/uuid"
	"path/filepath"
)

// RunNativeControl uses the same Pi SDK for planner/reviewer tool loops. Its only
// tools are the explicit task-local catalog; it cannot discover user extensions.
func RunNativeControl(ctx context.Context, cfg *config.Config, storageKey, instruction string, native agentruntime.NativeWorkflow, definitions []agentruntime.Tool, execute agentruntime.ToolExecutor, emit func(agentruntime.Event)) (*agentruntime.Result, error) {
	req := agentruntime.Request{RunID: uuid.NewString(), SystemPrompt: instruction, Message: "Continue using the latest host state. Submit a structured decision with the provided tool.", Model: piModelConfig(cfg.OpenAI, nil, storageKey), Tools: definitions, SessionDir: filepath.Join(cfg.AgentSessionDir(), "native-control", agentruntime.ConversationStorageKey(storageKey)), MaxTurns: 12, NativeWorkflow: &native}
	req.RetryMax = cfg.MultiAgent.Runtime.ModelRetryMaxRetries
	req.RetryMaxDelayMS = cfg.MultiAgent.Runtime.ModelRetryMaxBackoffSec * 1000
	for _, id := range cfg.MultiAgent.Runtime.ModelFailoverChannels {
		if ch, _, ok := cfg.ResolveAIChannel(id); ok {
			req.FallbackModels = append(req.FallbackModels, piModelConfig(ch, nil, storageKey))
		}
	}
	return (agentruntime.Pi{NodePath: cfg.AgentRuntime.NodePath, WorkerPath: cfg.AgentRuntime.WorkerPath}).Run(ctx, req, execute, emit, nil)
}
