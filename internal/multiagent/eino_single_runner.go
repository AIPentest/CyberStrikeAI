package multiagent

import (
	"context"
	"fmt"
	"sync"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/project"
	"cyberstrike-ai/internal/reasoning"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// einoSingleAgentName 与 ChatModelAgent.Name 一致，供流式事件映射主对话区。
const einoSingleAgentName = "cyberstrike-eino-single"

// RunEinoSingleChatModelAgent 使用 Eino TypedChatModelAgent[*schema.AgenticMessage] + adk.NewRunner.Run（官方 Quick Start 的 Query 同属 Runner API；此处用历史 + 用户消息切片等价于多轮 Query）。
// 与 RunDeepAgent 共享 runEinoADKAgentLoop 的 SSE 映射与 MCP 桥。
func RunEinoSingleChatModelAgent(
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
	if appCfg == nil || ag == nil {
		return nil, fmt.Errorf("eino single: config or Agent is nil")
	}
	if ma == nil {
		return nil, fmt.Errorf("eino single: multi_agent config is nil")
	}
	runtimeUserMessage := prepareLatestUserMessageForModel(userMessage, appCfg, &ma.EinoMiddleware, conversationID, logger)

	einoLoc, einoSkillMW, einoFSTools, skillsRoot, einoErr := prepareEinoAgenticSkills(ctx, appCfg.SkillsDir, ma, logger)
	if einoErr != nil {
		return nil, einoErr
	}

	holder := &einomcp.ConversationHolder{}
	holder.Set(conversationID)

	var mcpIDsMu sync.Mutex
	var mcpIDs []string
	mcpExecBinder := NewMCPExecutionBinder()
	recorder := func(id, toolCallID string) {
		if id == "" {
			return
		}
		mcpExecBinder.Bind(toolCallID, id)
		mcpIDsMu.Lock()
		mcpIDs = append(mcpIDs, id)
		mcpIDsMu.Unlock()
	}

	snapshotMCPIDs := func() []string {
		mcpIDsMu.Lock()
		defer mcpIDsMu.Unlock()
		out := make([]string, len(mcpIDs))
		copy(out, mcpIDs)
		return out
	}

	toolInvokeNotify := einomcp.NewToolInvokeNotifyHolder()
	einoExecBegin, einoExecAppendPartial, einoExecRegisterCancel, einoExecUnregisterCancel, einoExecFinish := newEinoExecuteMonitorCallbacks(ctx, ag, recorder)
	mainDefs := ag.ToolsForRole(roleTools)
	mainTools, err := einomcp.ToolsFromDefinitions(ag, holder, mainDefs, recorder, nil, toolInvokeNotify, einoSingleAgentName)
	if err != nil {
		return nil, err
	}

	mainToolsForCfg, mainOrchestratorPre, singleToolSearchActive, err := prependEinoAgenticMiddlewares(ctx, &ma.EinoMiddleware, einoMWMain, mainTools, einoLoc, skillsRoot, conversationID, projectID, logger)
	if err != nil {
		return nil, fmt.Errorf("eino single middlewares: %w", err)
	}

	baseHTTPClient := newEinoBaseHTTPClient()
	agenticModelFactory := newEinoAgenticChatModelFactory(baseHTTPClient, reasoningClient, logger)
	mainModel, err := agenticModelFactory(ctx, appCfg.OpenAI, einoModelModeNormal)
	if err != nil {
		return nil, fmt.Errorf("eino single agentic model: %w", err)
	}
	modelRetryCfg := newEinoAgenticModelRetryConfig(&ma.EinoMiddleware, logger, "eino_single")
	modelFailoverCfg, err := newEinoAgenticModelFailoverConfig(ctx, appCfg, &ma.EinoMiddleware, einoModelModeNormal, agenticModelFactory, logger, "eino_single", progress, "eino_single", conversationID)
	if err != nil {
		return nil, err
	}
	logEinoAgenticModelGate(
		logger,
		"eino_single",
		"eino_single",
		evaluateEinoAgenticModelGate(agenticModelGateFactory(agenticModelFactory, appCfg.OpenAI, einoModelModeNormal), einoAgenticRuntimeSupportV0914()),
	)

	mainSumMw, err := newEinoAgenticSummarizationMiddleware(ctx, mainModel, appCfg, &ma.EinoMiddleware, conversationID, db, projectID, logger)
	if err != nil {
		return nil, fmt.Errorf("eino single agentic summarization: %w", err)
	}

	modelFacingTrace := newModelFacingTraceHolder()

	handlers := make([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], 0, 8)
	if len(mainOrchestratorPre) > 0 {
		handlers = append(handlers, mainOrchestratorPre...)
	}
	if einoSkillMW != nil {
		if einoFSTools && einoLoc != nil {
			fsMw, fsErr := subAgentAgenticFilesystemMiddleware(ctx, einoLoc, toolInvokeNotify, einoSingleAgentName, conversationID, projectID, ma.EinoMiddleware.ReductionRootDir, toolMaxBytesFromMW(&ma.EinoMiddleware), mcpExecBinder, einoExecBegin, einoExecAppendPartial, einoExecRegisterCancel, einoExecUnregisterCancel, einoExecFinish, agentToolTimeoutMinutes(appCfg), agentToolWaitTimeoutSeconds(appCfg), agentShellNoOutputTimeoutSeconds(appCfg), nil)
			if fsErr != nil {
				return nil, fmt.Errorf("eino single filesystem middleware: %w", fsErr)
			}
			handlers = append(handlers, fsMw)
		}
		handlers = append(handlers, einoSkillMW)
	}
	handlers = appendEinoAgenticChatModelTailMiddlewares(handlers, einoChatModelTailConfig{
		logger:               logger,
		phase:                "eino_single",
		agenticSummarization: mainSumMw,
		modelName:            appCfg.OpenAI.Model,
		maxTotalTokens:       appCfg.OpenAI.MaxTotalTokens,
		toolMaxBytes:         toolMaxBytesFromMW(&ma.EinoMiddleware),
		conversationID:       conversationID,
		trace:                modelFacingTrace,
		middlewareConfig:     &ma.EinoMiddleware,
	})

	maxIter := agentMaxIterations(appCfg)

	mainToolsCfg := adk.ToolsConfig{
		ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               mainToolsForCfg,
			UnknownToolsHandler: einomcp.UnknownToolReminderHandler(),
			ToolCallMiddlewares: []compose.ToolMiddleware{
				modelOutputExecutionGuardMiddleware(),
				localToolRBACMiddleware(),
				hitlToolCallMiddleware(),
				softRecoveryToolMiddleware(),
			},
		},
		EmitInternalEvents: true,
	}
	ins := project.AppendSystemPromptBlock(ag.EinoSingleAgentSystemInstruction(), systemPromptExtra)
	ins = project.AppendVisionImageAnalysisIfReady(ins, appCfg.Vision.Ready())
	ins = injectToolNamesOnlyInstruction(ctx, ins, mainTools, singleToolSearchActive)
	if logger != nil {
		names := collectToolNames(ctx, mainTools)
		mountedNames := collectToolNames(ctx, mainToolsForCfg)
		logger.Info("eino tool-name injection",
			zap.String("scope", "eino_single"),
			zap.Int("tool_names", len(names)),
			zap.Int("mounted_tool_names", len(mountedNames)),
			zap.Bool("tool_search_middleware", singleToolSearchActive),
		)
	}

	chatCfg := einoAgenticChatModelAgentConfig{
		Name:                einoSingleAgentName,
		Description:         "Eino ADK ChatModelAgent with MCP tools for authorized security testing.",
		Instruction:         ins,
		GenModelInput:       literalAgenticInstructionGenModelInput,
		Model:               mainModel,
		ToolsConfig:         mainToolsCfg,
		MaxIterations:       maxIter,
		Handlers:            handlers,
		ModelRetryConfig:    modelRetryCfg,
		ModelFailoverConfig: modelFailoverCfg,
	}
	outKey, _ := deepExtrasFromConfig(ma)
	if outKey != "" {
		chatCfg.OutputKey = outKey
	}

	chatAgent, err := newEinoAgenticChatModelAgentAdapter(ctx, chatCfg)
	if err != nil {
		return nil, fmt.Errorf("eino single Agentic ChatModelAgent: %w", err)
	}

	baseMsgs := historyToMessages(history, appCfg, &ma.EinoMiddleware)
	baseMsgs = appendUserMessageIfNeeded(baseMsgs, runtimeUserMessage)

	streamsMainAssistant := func(agent string) bool {
		return agent == "" || agent == einoSingleAgentName
	}
	einoRoleTag := func(agent string) string {
		_ = agent
		return "orchestrator"
	}

	return runEinoADKAgentLoop(ctx, &einoADKRunLoopArgs{
		OrchMode:             "eino_single",
		OrchestratorName:     einoSingleAgentName,
		ConversationID:       conversationID,
		Progress:             progress,
		Logger:               logger,
		SnapshotMCPIDs:       snapshotMCPIDs,
		StreamsMainAssistant: streamsMainAssistant,
		EinoRoleTag:          einoRoleTag,
		// Chat history recovery is intentionally centralized in last_react_*.
		// ADK checkpoints are a second persisted model-state channel and make
		// stale-context bugs hard to reason about across user turns.
		CheckpointDir:           "",
		RunRetryMaxAttempts:     RunRetryMaxAttemptsFromConfig(&ma.EinoMiddleware),
		RunRetryMaxBackoffSec:   int(einoRunRetryMaxBackoffFromConfig(&ma.EinoMiddleware).Seconds()),
		McpIDsMu:                &mcpIDsMu,
		McpIDs:                  &mcpIDs,
		FilesystemMonitorAgent:  ag,
		FilesystemMonitorRecord: recorder,
		MCPExecutionBinder:      mcpExecBinder,
		ToolInvokeNotify:        toolInvokeNotify,
		DA:                      chatAgent,
		ModelFacingTrace:        modelFacingTrace,
		EinoCallbacks:           &ma.EinoCallbacks,
		MaxTotalTokens:          appCfg.OpenAI.MaxTotalTokens,
		ToolMaxBytes:            toolMaxBytesFromMW(&ma.EinoMiddleware),
		ModelName:               appCfg.OpenAI.Model,
		MiddlewareConfig:        &ma.EinoMiddleware,
		// Legacy Chinese marker 未捕获到助手文本输出 stays matched byte-identically
		// (dual-accept); the new display text is English.
		EmptyResponseMessage: "(Eino ADK single-agent session completed but no assistant text was captured. Check process details or logs.) " +
			"（Eino ADK single-agent session completed, but no assistant text output was captured. Please check the process details or logs.)",
	}, baseMsgs)
}
