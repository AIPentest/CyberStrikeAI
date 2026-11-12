package multiagent

import (
	"context"
	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/reasoning"
	"go.uber.org/zap"
)

func RunSingleAgent(
	ctx context.Context,
	appCfg *config.Config,
	ma *config.MultiAgentConfig,
	ag *agent.Agent,
	db *database.DB,
	logger *zap.Logger,
	conversationID string,
	projectID string,
	userMessage string,
	history []agent.ChatMessage,
	roleTools []string,
	progress func(eventType, message string, data interface{}),
	reasoningClient *reasoning.ClientIntent,
	systemPromptExtra string,
) (*RunResult, error) {
	if appCfg != nil && appCfg.AgentRuntime.UsesPi() {
		return runPiSingle(ctx, appCfg, ag, db, logger, conversationID, projectID, userMessage, history, roleTools, progress, reasoningClient, systemPromptExtra)
	}
	return RunEinoSingleChatModelAgent(ctx, appCfg, ma, ag, db, logger, conversationID, projectID, userMessage, history, roleTools, progress, reasoningClient, systemPromptExtra)
}
func RunMultiAgent(
	ctx context.Context,
	appCfg *config.Config,
	ma *config.MultiAgentConfig,
	ag *agent.Agent,
	db *database.DB,
	logger *zap.Logger,
	conversationID string,
	projectID string,
	userMessage string,
	history []agent.ChatMessage,
	roleTools []string,
	progress func(eventType, message string, data interface{}),
	agentsMarkdownDir string,
	orchestrationOverride string,
	reasoningClient *reasoning.ClientIntent,
	systemPromptExtra string,
) (*RunResult, error) {
	if appCfg != nil && appCfg.AgentRuntime.UsesPi() {
		return runPiMulti(ctx, appCfg, ma, ag, db, logger, conversationID, projectID, userMessage, history, roleTools, progress, agentsMarkdownDir, orchestrationOverride, reasoningClient, systemPromptExtra)
	}
	return RunDeepAgent(ctx, appCfg, ma, ag, db, logger, conversationID, projectID, userMessage, history, roleTools, progress, agentsMarkdownDir, orchestrationOverride, reasoningClient, systemPromptExtra)
}
