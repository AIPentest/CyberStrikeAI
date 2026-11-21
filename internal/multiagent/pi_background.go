package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func CompleteBackgroundStep(ctx context.Context, oa config.OpenAIConfig, msgs []agent.ChatMessage, tools []agent.Tool, maxTokens int, logger *zap.Logger) (*agent.ChatMessage, int, error) {
	return CompleteBackgroundStepWithRuntime(ctx, config.AgentRuntimeConfig{}, oa, msgs, tools, maxTokens, logger)
}

// Generate one proposal only. The caller owns tool execution,
// approvals, retries, budgets and transcript persistence; the SDK cannot execute here.
func CompleteBackgroundStepWithRuntime(ctx context.Context, runtime config.AgentRuntimeConfig, oa config.OpenAIConfig, msgs []agent.ChatMessage, tools []agent.Tool, maxTokens int, logger *zap.Logger) (*agent.ChatMessage, int, error) {
	if err := runtime.Validate(); err != nil {
		return nil, 0, err
	}

	if maxTokens <= 0 {
		return nil, 0, fmt.Errorf("background generation budget is exhausted")
	}
	if oa.MaxCompletionTokensEffective() > maxTokens {
		oa.MaxCompletionTokens = maxTokens
	}
	var system []string
	var history []agent.ChatMessage
	for _, m := range msgs {
		if m.Role == "system" {
			system = append(system, m.Content)
		} else {
			history = append(history, m)
		}
	}
	raw, err := piHistoryJSON(history)
	if err != nil {
		return nil, 0, err
	}
	defs := make([]agentruntime.Tool, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, agentruntime.Tool{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters})
	}
	req := agentruntime.Request{RunID: uuid.NewString(), GenerateOnly: true, SystemPrompt: strings.Join(system, "\n\n"), History: raw, Model: piModelConfig(oa, nil, openai.SessionIDFromContext(ctx)), Tools: defs, MaxTurns: 1}
	result, err := (agentruntime.Pi{NodePath: runtime.NodePath, WorkerPath: runtime.WorkerPath}).Run(ctx, req, nil, nil, nil)
	if err != nil {
		return nil, 0, err
	}
	var messages []agent.ChatMessage
	if err = json.Unmarshal(result.History, &messages); err != nil {
		return nil, 0, err
	}
	if len(messages) != 1 {
		return nil, 0, fmt.Errorf("Pi returned no background proposal")
	}
	return &messages[0], result.TotalTokens, nil
}
