package multiagent

import (
	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// einoChatModelTailConfig configures middleware appended after reduction/skill/plantask
// and immediately before each ChatModel invocation pipeline completes.
//
// Order (best practice):
//  1. system merge — accurate token count for summarization
//  2. continuation user dedup — drop stale session-resume injections
//  3. malformed tool-call arguments repair
//  4. pre-summarization tool-call/result reconciliation
//  5. summarization
//  6. soft model-input budget (warn/compact only, never fail locally)
//  7. final malformed tool-call arguments repair
//  8. final tool-call/result reconciliation
//  9. orphan tool prune (defense in depth)
//  10. malformed tool_search history repair
//  11. telemetry
//  12. model-facing trace snapshot
type einoChatModelTailConfig struct {
	logger               *zap.Logger
	phase                string
	summarization        adk.ChatModelAgentMiddleware
	agenticSummarization adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	modelName            string
	maxTotalTokens       int
	tokenSafetyPercent   int
	toolMaxBytes         int
	conversationID       string
	trace                *modelFacingTraceHolder
	middlewareConfig     *config.MultiAgentEinoMiddlewareConfig
	skipOrphanPruner     bool
	skipTelemetry        bool
	skipTrace            bool
}

func appendEinoChatModelTailMiddlewares(handlers []adk.ChatModelAgentMiddleware, cfg einoChatModelTailConfig) []adk.ChatModelAgentMiddleware {
	handlers = append(handlers, newSystemMessageNormalizerMiddleware(cfg.logger, cfg.phase))
	handlers = append(handlers, newContinuationUserDedupMiddleware(cfg.logger, cfg.phase))
	handlers = append(handlers, newToolCallArgumentsSanitizerMiddleware(cfg.logger, cfg.phase+"_pre_summarization"))
	if cfg.summarization != nil {
		// Summarization invokes the model internally, so its input needs the same
		// structural guarantee as the agent's final model call.
		handlers = append(handlers, newToolPairReconcilerMiddleware(cfg.logger, cfg.phase+"_pre_summarization"))
		handlers = append(handlers, cfg.summarization)
	}
	handlers = append(handlers, newModelInputSoftBudgetMiddleware(cfg.maxTotalTokens, cfg.toolMaxBytes, cfg.modelName, cfg.tokenSafetyPercent, cfg.logger, cfg.phase))
	handlers = append(handlers, newToolCallArgumentsSanitizerMiddleware(cfg.logger, cfg.phase))
	handlers = append(handlers, newToolPairReconcilerMiddleware(cfg.logger, cfg.phase))
	if !cfg.skipOrphanPruner {
		handlers = append(handlers, newOrphanToolPrunerMiddleware(cfg.logger, cfg.phase))
	}
	handlers = append(handlers, newToolSearchResultSanitizerMiddleware(cfg.logger, cfg.phase))
	if !cfg.skipTelemetry {
		if teleMw := newEinoModelInputTelemetryMiddleware(cfg.logger, cfg.modelName, cfg.conversationID, cfg.phase); teleMw != nil {
			handlers = append(handlers, teleMw)
		}
	}
	if !cfg.skipTrace && cfg.trace != nil {
		if capMw := newModelFacingTraceMiddleware(cfg.trace); capMw != nil {
			handlers = append(handlers, capMw)
		}
	}
	return handlers
}

func toolMaxBytesFromMW(mwCfg *config.MultiAgentEinoMiddlewareConfig) int {
	if mwCfg != nil {
		return mwCfg.ReductionMaxLengthForTruncEffective()
	}
	return config.MultiAgentEinoMiddlewareConfig{}.ReductionMaxLengthForTruncEffective()
}
