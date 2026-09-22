// Package multiagent 使用 CloudWeGo Eino adk/prebuilt（deep / plan_execute / supervisor）编排多代理，MCP 工具经 einomcp 桥接到现有 Agent。
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/agents"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/einomcp"
	"cyberstrike-ai/internal/project"
	"cyberstrike-ai/internal/reasoning"
	"cyberstrike-ai/internal/security"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// RunResult 与单 Agent 循环结果字段对齐，便于复用存储与 SSE 收尾逻辑。
type RunResult struct {
	Response             string
	MCPExecutionIDs      []string
	LastAgentTraceInput  string // 已序列化的消息带（JSON）：原生循环或 Eino 均写入，供续跑/攻击链等恢复上下文
	LastAgentTraceOutput string // 本轮助手侧对外展示文本（摘要或最终回复）
	Finalized            bool
	Status               string
	CompletionReason     string
	EvidenceVerified     bool
	EvidenceRefs         []string
	PendingExecutionIDs  []string
	MissingChecks        []string
}

// toolCallPendingInfo tracks a tool_call emitted to the UI so we can later
// correlate tool_result events (even when the framework omits ToolCallID) and
// avoid leaving the UI stuck in "running" state on recoverable errors.
type toolCallPendingInfo struct {
	ToolCallID string
	ToolName   string
	Arguments  map[string]interface{}
	EinoAgent  string
	EinoRole   string
}

var fallbackToolCallSequence atomic.Uint64

// RunDeepAgent 使用 Eino 多代理预置编排执行一轮对话（deep / plan_execute / supervisor；流式事件通过 progress 回调输出）。
// orchestrationOverride 非空时优先（如聊天/WebShell 请求体）；否则用 multi_agent.orchestration（遗留 yaml）；皆空则按 deep。
// reasoningClient 来自 ChatRequest.reasoning；可为 nil（机器人/批量等走全局 openai.reasoning）。
func RunDeepAgent(
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
	if appCfg == nil || ma == nil || ag == nil {
		return nil, fmt.Errorf("multiagent: 配置或 Agent 为空")
	}

	runtimeUserMessage := prepareLatestUserMessageForModel(userMessage, appCfg, &ma.EinoMiddleware, conversationID, logger)

	effectiveSubs := ma.SubAgents
	var markdownLoad *agents.MarkdownDirLoad
	var orch *agents.OrchestratorMarkdown
	if strings.TrimSpace(agentsMarkdownDir) != "" {
		load, merr := agents.LoadMarkdownAgentsDir(agentsMarkdownDir)
		if merr != nil {
			if logger != nil {
				logger.Warn("加载 agents 目录 Markdown 失败，沿用 config 中的 sub_agents", zap.Error(merr))
			}
		} else {
			markdownLoad = load
			effectiveSubs = agents.MergeYAMLAndMarkdown(ma.SubAgents, load.SubAgents)
			orch = load.Orchestrator
		}
	}
	orchMode := config.NormalizeMultiAgentOrchestration(ma.Orchestration)
	if o := strings.TrimSpace(orchestrationOverride); o != "" {
		orchMode = config.NormalizeMultiAgentOrchestration(o)
	}
	if orchMode != "plan_execute" && ma.WithoutGeneralSubAgent && len(effectiveSubs) == 0 {
		return nil, fmt.Errorf("multi_agent.without_general_sub_agent 为 true 时，必须在 multi_agent.sub_agents 或 agents 目录 Markdown 中配置至少一个子代理")
	}
	if orchMode == "supervisor" && len(effectiveSubs) == 0 {
		return nil, fmt.Errorf("multi_agent.orchestration=supervisor 时需至少配置一个子代理（sub_agents 或 agents 目录 Markdown）")
	}
	if orchMode == "supervisor" && len(effectiveSubs) == 1 && progress != nil {
		progress("progress", "Supervisor 是专家路由模式；当前仅 1 个子代理，专家路由空间有限，仍会继续执行。", map[string]interface{}{
			"conversationId": conversationID,
			"source":         "eino",
			"orchestration":  orchMode,
			"kind":           "supervisor_boundary_hint",
		})
	}

	agenticLoc, agenticSkillMW, agenticFSTools, agenticSkillsRoot, einoErr := prepareEinoAgenticSkills(ctx, appCfg.SkillsDir, ma, logger)
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
	einoExecBegin, einoExecAppendPartial, einoExecRegisterCancel, einoExecUnregisterCancel, einoExecFinish := newEinoExecuteMonitorCallbacks(ctx, ag, recorder)

	// 与单代理流式一致：在 response_start / response_delta 的 data 中带当前 mcpExecutionIds，供主聊天绑定复制与展示。
	snapshotMCPIDs := func() []string {
		mcpIDsMu.Lock()
		defer mcpIDsMu.Unlock()
		out := make([]string, len(mcpIDs))
		copy(out, mcpIDs)
		return out
	}

	toolInvokeNotify := einomcp.NewToolInvokeNotifyHolder()
	mainDefs := ag.ToolsForRole(roleTools)

	baseHTTPClient := newEinoBaseHTTPClient()
	modelFactory := newEinoToolCallingChatModelFactory(baseHTTPClient, reasoningClient, logger)
	agenticModelFactory := newEinoAgenticChatModelFactory(baseHTTPClient, reasoningClient, logger)
	agenticModelRetryCfg := newEinoAgenticModelRetryConfig(&ma.EinoMiddleware, logger, "multiagent")
	agenticModelFailoverCfg, err := newEinoAgenticModelFailoverConfig(ctx, appCfg, &ma.EinoMiddleware, einoModelModeNormal, agenticModelFactory, logger, "multiagent", progress, orchMode, conversationID)
	if err != nil {
		return nil, err
	}
	logEinoAgenticModelGate(
		logger,
		"multiagent",
		orchMode,
		evaluateEinoAgenticModelGate(agenticModelGateFactory(agenticModelFactory, appCfg.OpenAI, einoModelModeNormal), einoAgenticRuntimeSupportV0914()),
	)

	deepMaxIter := agentMaxIterations(appCfg)

	var subAgents []adk.TypedAgent[*schema.AgenticMessage]
	var supervisorSubAgents []adk.Agent
	if orchMode != "plan_execute" {
		subAgents = make([]adk.TypedAgent[*schema.AgenticMessage], 0, len(effectiveSubs))
		supervisorSubAgents = make([]adk.Agent, 0, len(effectiveSubs))
		for _, sub := range effectiveSubs {
			id := strings.TrimSpace(sub.ID)
			if id == "" {
				return nil, fmt.Errorf("multi_agent.sub_agents 中存在空的 id")
			}
			name := strings.TrimSpace(sub.Name)
			if name == "" {
				name = id
			}
			desc := strings.TrimSpace(sub.Description)
			if desc == "" {
				desc = fmt.Sprintf("Specialist agent %s for penetration testing workflow.", id)
			}
			instr := strings.TrimSpace(sub.Instruction)
			if instr == "" {
				instr = "你是 CyberStrikeAI 中的专业子代理，在授权渗透测试场景下协助完成用户委托的子任务。优先使用可用工具获取证据，回答简洁专业。"
			}

			roleTools := sub.RoleTools
			bind := strings.TrimSpace(sub.BindRole)
			if bind != "" && appCfg.Roles != nil {
				if r, ok := appCfg.Roles[bind]; ok && r.Enabled {
					if len(roleTools) == 0 && len(r.Tools) > 0 {
						roleTools = r.Tools
					}
				}
			}

			subModel, err := agenticModelFactory(ctx, appCfg.OpenAI, einoModelModeNormal)
			if err != nil {
				return nil, fmt.Errorf("子代理 %q AgenticModel: %w", id, err)
			}

			subDefs := ag.ToolsForRole(roleTools)
			subTools, err := einomcp.ToolsFromDefinitions(ag, holder, subDefs, recorder, nil, toolInvokeNotify, id)
			if err != nil {
				return nil, fmt.Errorf("子代理 %q 工具: %w", id, err)
			}

			subToolsForCfg, subPre, subToolSearchActive, err := prependEinoAgenticMiddlewares(ctx, &ma.EinoMiddleware, einoMWSub, subTools, agenticLoc, agenticSkillsRoot, conversationID, projectID, logger)
			if err != nil {
				return nil, fmt.Errorf("子代理 %q eino 中间件: %w", id, err)
			}

			subMax := resolveMaxIterations(appCfg, sub.MaxIterations)

			subSumMw, err := newEinoAgenticSummarizationMiddleware(ctx, subModel, appCfg, &ma.EinoMiddleware, conversationID, db, projectID, logger)
			if err != nil {
				return nil, fmt.Errorf("子代理 %q agentic summarization 中间件: %w", id, err)
			}

			var subHandlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
			if len(subPre) > 0 {
				subHandlers = append(subHandlers, subPre...)
			}
			if agenticSkillMW != nil {
				if agenticFSTools && agenticLoc != nil {
					subFs, fsErr := subAgentAgenticFilesystemMiddleware(ctx, agenticLoc, toolInvokeNotify, id, conversationID, projectID, ma.EinoMiddleware.ReductionRootDir, toolMaxBytesFromMW(&ma.EinoMiddleware), mcpExecBinder, einoExecBegin, einoExecAppendPartial, einoExecRegisterCancel, einoExecUnregisterCancel, einoExecFinish, agentToolTimeoutMinutes(appCfg), agentToolWaitTimeoutSeconds(appCfg), agentShellNoOutputTimeoutSeconds(appCfg), nil)
					if fsErr != nil {
						return nil, fmt.Errorf("子代理 %q filesystem 中间件: %w", id, fsErr)
					}
					subHandlers = append(subHandlers, subFs)
				}
				subHandlers = append(subHandlers, agenticSkillMW)
			}
			subHandlers = appendEinoAgenticChatModelTailMiddlewares(subHandlers, einoChatModelTailConfig{
				logger:               logger,
				phase:                "sub_agent:" + id,
				agenticSummarization: subSumMw,
				modelName:            appCfg.OpenAI.Model,
				maxTotalTokens:       appCfg.OpenAI.MaxTotalTokens,
				toolMaxBytes:         toolMaxBytesFromMW(&ma.EinoMiddleware),
				conversationID:       conversationID,
				middlewareConfig:     &ma.EinoMiddleware,
			})

			subInstrFinal := project.AppendVisionImageAnalysisIfReady(instr, appCfg.Vision.Ready())
			subInstrFinal = injectToolNamesOnlyInstruction(ctx, subInstrFinal, subTools, subToolSearchActive)
			if logger != nil {
				subNames := collectToolNames(ctx, subTools)
				mountedNames := collectToolNames(ctx, subToolsForCfg)
				logger.Info("eino tool-name injection",
					zap.String("scope", "sub_agent"),
					zap.String("agent", id),
					zap.Int("tool_names", len(subNames)),
					zap.Int("mounted_tool_names", len(mountedNames)),
					zap.Bool("tool_search_middleware", subToolSearchActive),
				)
			}
			sa, err := newEinoAgenticChatModelAgent(ctx, einoAgenticChatModelAgentConfig{
				Name:          id,
				Description:   desc,
				Instruction:   subInstrFinal,
				GenModelInput: literalAgenticInstructionGenModelInput,
				Model:         subModel,
				ToolsConfig: adk.ToolsConfig{
					ToolsNodeConfig: compose.ToolsNodeConfig{
						Tools:               subToolsForCfg,
						UnknownToolsHandler: einomcp.UnknownToolReminderHandler(),
						ToolCallMiddlewares: []compose.ToolMiddleware{
							modelOutputExecutionGuardMiddleware(),
							localToolRBACMiddleware(),
							hitlToolCallMiddleware(),
							softRecoveryToolMiddleware(),
						},
					},
					EmitInternalEvents: true,
				},
				MaxIterations:       subMax,
				Handlers:            subHandlers,
				ModelRetryConfig:    agenticModelRetryCfg,
				ModelFailoverConfig: agenticModelFailoverCfg,
			})
			if err != nil {
				return nil, fmt.Errorf("子代理 %q: %w", id, err)
			}
			subAgents = append(subAgents, sa)
			if adapted := newEinoAgenticMessageAgentAdapter(sa); adapted != nil {
				supervisorSubAgents = append(supervisorSubAgents, adapted)
			}
		}
	}

	modelFacingTrace := newModelFacingTraceHolder()

	// 与 deep.Config.Name / supervisor 主代理 Name 一致。
	orchestratorName := "cyberstrike-deep"
	orchDescription := "Coordinates specialist agents and MCP tools for authorized security testing."
	orchInstruction, orchMeta := resolveMainOrchestratorInstruction(orchMode, ma, markdownLoad)
	if orchMeta != nil {
		if strings.TrimSpace(orchMeta.EinoName) != "" {
			orchestratorName = strings.TrimSpace(orchMeta.EinoName)
		}
		if d := strings.TrimSpace(orchMeta.Description); d != "" {
			orchDescription = d
		}
	} else if orchMode == "deep" && orch != nil {
		if strings.TrimSpace(orch.EinoName) != "" {
			orchestratorName = strings.TrimSpace(orch.EinoName)
		}
		if d := strings.TrimSpace(orch.Description); d != "" {
			orchDescription = d
		}
	}

	mainTools, err := einomcp.ToolsFromDefinitions(ag, holder, mainDefs, recorder, nil, toolInvokeNotify, orchestratorName)
	if err != nil {
		return nil, err
	}
	var mainToolsForCfg []tool.BaseTool
	var mainToolSearchActive bool
	var mainAgenticOrchestratorPre []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	mainToolsForCfg, mainAgenticOrchestratorPre, mainToolSearchActive, err = prependEinoAgenticMiddlewares(ctx, &ma.EinoMiddleware, einoMWMain, mainTools, agenticLoc, agenticSkillsRoot, conversationID, projectID, logger)
	if err != nil {
		return nil, err
	}

	orchInstruction = project.AppendSystemPromptBlock(orchInstruction, systemPromptExtra)
	orchInstruction = project.AppendVisionImageAnalysisIfReady(orchInstruction, appCfg.Vision.Ready())
	orchInstruction = injectToolNamesOnlyInstruction(ctx, orchInstruction, mainTools, mainToolSearchActive)
	if logger != nil {
		mainNames := collectToolNames(ctx, mainTools)
		mountedNames := collectToolNames(ctx, mainToolsForCfg)
		logger.Info("eino tool-name injection",
			zap.String("scope", "orchestrator"),
			zap.String("orchestration", orchMode),
			zap.Int("tool_names", len(mainNames)),
			zap.Int("mounted_tool_names", len(mountedNames)),
			zap.Bool("tool_search_middleware", mainToolSearchActive),
		)
	}

	supInstr := strings.TrimSpace(orchInstruction)
	if orchMode == "supervisor" {
		var sb strings.Builder
		if supInstr != "" {
			sb.WriteString(supInstr)
			sb.WriteString("\n\n")
		}
		sb.WriteString("你是监督协调者：可将任务通过 transfer 工具委派给下列专家子代理（使用其在系统中的 Agent 名称）。专家列表：")
		for _, sa := range subAgents {
			if sa == nil {
				continue
			}
			sb.WriteString("\n- ")
			sb.WriteString(sa.Name(ctx))
		}
		sb.WriteString("\n\nSupervisor 是专家路由模式：仅当任务确实需要不同专家分工时才 transfer；简单查询、单步工具调用或无需专业分流的任务由你直接完成。避免在同一子代理之间反复 transfer；除非有新的、具体的补充目标。专家返回后，你必须自行汇总、裁剪、校验证据，再用 exit 交付最终答案。")
		sb.WriteString("\n\n当你已完成用户目标或需要将最终结论交付用户时，使用 exit 工具结束。")
		supInstr = sb.String()
	}

	var deepBackend filesystem.Backend
	var deepShell filesystem.StreamingShell
	if agenticLoc != nil && agenticFSTools {
		deepBackend = agenticLoc
		deepShell = &einoStreamingShellWrap{
			inner:                   security.NewEinoStreamingShell(),
			invokeNotify:            toolInvokeNotify,
			einoAgentName:           orchestratorName,
			outputChunk:             nil,
			beginMonitor:            einoExecBegin,
			appendPartialMonitor:    einoExecAppendPartial,
			registerCancelMonitor:   einoExecRegisterCancel,
			unregisterCancelMonitor: einoExecUnregisterCancel,
			finishMonitor:           einoExecFinish,
			toolTimeoutMinutes:      agentToolTimeoutMinutes(appCfg),
			toolWaitTimeoutSeconds:  agentToolWaitTimeoutSeconds(appCfg),
			shellNoOutputTimeoutSec: agentShellNoOutputTimeoutSeconds(appCfg),
		}
	}

	var mainModel model.AgenticModel
	var mainSumMw adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	if orchMode != "plan_execute" {
		mainModel, err = agenticModelFactory(ctx, appCfg.OpenAI, einoModelModeNormal)
		if err != nil {
			return nil, fmt.Errorf("多代理主 AgenticModel: %w", err)
		}
		mainSumMw, err = newEinoAgenticSummarizationMiddleware(ctx, mainModel, appCfg, &ma.EinoMiddleware, conversationID, db, projectID, logger)
		if err != nil {
			return nil, fmt.Errorf("多代理主 agentic summarization 中间件: %w", err)
		}
	}

	// noNestedTaskMiddleware 必须在最外层（最先拦截），防止 skill 或其他中间件内部触发 task 调用绕过检测。
	deepHandlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{newNoNestedAgenticTaskMiddleware()}
	var taskBlackboardSupplement string
	if appCfg.Project.Enabled && db != nil {
		if pid := strings.TrimSpace(projectID); pid != "" {
			if block, err := project.BuildFactIndexBlock(db, pid, appCfg.Project); err == nil {
				taskBlackboardSupplement = strings.TrimSpace(block)
			}
		}
	}
	if mw := newAgenticTaskContextEnrichMiddleware(runtimeUserMessage, history, ma.SubAgentUserContextMaxRunesEffective(), taskBlackboardSupplement); mw != nil {
		deepHandlers = append(deepHandlers, mw)
	}
	if len(mainAgenticOrchestratorPre) > 0 {
		deepHandlers = append(deepHandlers, mainAgenticOrchestratorPre...)
	}
	if agenticSkillMW != nil {
		deepHandlers = append(deepHandlers, agenticSkillMW)
	}
	deepHandlers = appendEinoAgenticChatModelTailMiddlewares(deepHandlers, einoChatModelTailConfig{
		logger:               logger,
		phase:                "deep_orchestrator",
		agenticSummarization: mainSumMw,
		modelName:            appCfg.OpenAI.Model,
		maxTotalTokens:       appCfg.OpenAI.MaxTotalTokens,
		toolMaxBytes:         toolMaxBytesFromMW(&ma.EinoMiddleware),
		conversationID:       conversationID,
		trace:                modelFacingTrace,
		middlewareConfig:     &ma.EinoMiddleware,
	})

	supHandlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{}
	if len(mainAgenticOrchestratorPre) > 0 {
		supHandlers = append(supHandlers, mainAgenticOrchestratorPre...)
	}
	if agenticSkillMW != nil {
		supHandlers = append(supHandlers, agenticSkillMW)
	}
	supHandlers = appendEinoAgenticChatModelTailMiddlewares(supHandlers, einoChatModelTailConfig{
		logger:               logger,
		phase:                "supervisor_orchestrator",
		agenticSummarization: mainSumMw,
		modelName:            appCfg.OpenAI.Model,
		maxTotalTokens:       appCfg.OpenAI.MaxTotalTokens,
		toolMaxBytes:         toolMaxBytesFromMW(&ma.EinoMiddleware),
		conversationID:       conversationID,
		trace:                modelFacingTrace,
		middlewareConfig:     &ma.EinoMiddleware,
	})

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
	attachAgenticBuiltinActionToolMiddleware(&mainToolsCfg)

	deepAgenticOutKey, agenticTaskGen := deepAgenticExtrasFromConfig(ma)

	var da adk.Agent
	switch orchMode {
	case "plan_execute":
		peMainModel, perr := modelFactory(ctx, appCfg.OpenAI, einoModelModePlanner)
		if perr != nil {
			return nil, fmt.Errorf("plan_execute 规划模型: %w", perr)
		}
		if logger != nil {
			logger.Info("plan_execute: planner/replanner 使用无 reasoning 的独立 ChatModel（ToolChoiceForced 兼容）",
				zap.String("model", appCfg.OpenAI.Model),
			)
		}
		execModel, perr := modelFactory(ctx, appCfg.OpenAI, einoModelModeNormal)
		if perr != nil {
			return nil, fmt.Errorf("plan_execute 执行器模型: %w", perr)
		}
		agenticExecModel, perr := agenticModelFactory(ctx, appCfg.OpenAI, einoModelModeNormal)
		if perr != nil {
			return nil, fmt.Errorf("plan_execute 执行器 AgenticModel: %w", perr)
		}
		planRewriteSumMw, perr := newEinoSummarizationMiddleware(ctx, execModel, appCfg, &ma.EinoMiddleware, conversationID, db, projectID, logger)
		if perr != nil {
			return nil, fmt.Errorf("plan_execute planner/replanner summarization: %w", perr)
		}
		var peFsMw adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
		if agenticSkillMW != nil && agenticFSTools && agenticLoc != nil {
			peFsMw, err = subAgentAgenticFilesystemMiddleware(ctx, agenticLoc, toolInvokeNotify, "executor", conversationID, projectID, ma.EinoMiddleware.ReductionRootDir, toolMaxBytesFromMW(&ma.EinoMiddleware), mcpExecBinder, einoExecBegin, einoExecAppendPartial, einoExecRegisterCancel, einoExecUnregisterCancel, einoExecFinish, agentToolTimeoutMinutes(appCfg), agentToolWaitTimeoutSeconds(appCfg), agentShellNoOutputTimeoutSeconds(appCfg), nil)
			if err != nil {
				return nil, fmt.Errorf("plan_execute agentic filesystem 中间件: %w", err)
			}
		}
		peRoot, perr := NewPlanExecuteRoot(ctx, &PlanExecuteRootArgs{
			MainToolCallingModel: peMainModel,
			AgenticExecModel:     agenticExecModel,
			OrchInstruction:      orchInstruction,
			ToolsCfg:             mainToolsCfg,
			ExecMaxIter:          deepMaxIter,
			LoopMaxIter:          ma.PlanExecuteLoopMaxIterations,
			AppCfg:               appCfg,
			MwCfg:                &ma.EinoMiddleware,
			ConversationID:       conversationID,
			DB:                   db,
			ProjectID:            projectID,
			Logger:               logger,
			ModelName:            appCfg.OpenAI.Model,
			// 与 Deep/Supervisor 主代理同源：typed patch / reduction / toolsearch / plantask（见 buildPlanExecuteAgenticExecutorHandlers）。
			AgenticExecPreMiddlewares:   mainAgenticOrchestratorPre,
			AgenticSkillMiddleware:      agenticSkillMW,
			AgenticFilesystemMiddleware: peFsMw,
			ModelFacingTrace:            modelFacingTrace,
			PlannerReplannerRewriteHandlers: appendEinoChatModelTailMiddlewares(nil, einoChatModelTailConfig{
				logger:           logger,
				phase:            "plan_execute_planner_replanner",
				summarization:    planRewriteSumMw,
				modelName:        appCfg.OpenAI.Model,
				maxTotalTokens:   appCfg.OpenAI.MaxTotalTokens,
				toolMaxBytes:     toolMaxBytesFromMW(&ma.EinoMiddleware),
				conversationID:   conversationID,
				skipTrace:        true,
				middlewareConfig: &ma.EinoMiddleware,
			}),
			AgenticModelRetryConfig:    agenticModelRetryCfg,
			AgenticModelFailoverConfig: agenticModelFailoverCfg,
		})
		if perr != nil {
			return nil, perr
		}
		da = peRoot
	case "supervisor":
		supCfg := einoAgenticChatModelAgentConfig{
			Name:                orchestratorName,
			Description:         orchDescription,
			Instruction:         supInstr,
			GenModelInput:       literalAgenticInstructionGenModelInput,
			Model:               mainModel,
			ToolsConfig:         mainToolsCfg,
			MaxIterations:       deepMaxIter,
			Handlers:            supHandlers,
			Exit:                agenticCompatibleExitTool{},
			ModelRetryConfig:    agenticModelRetryCfg,
			ModelFailoverConfig: agenticModelFailoverCfg,
		}
		if deepAgenticOutKey != "" {
			supCfg.OutputKey = deepAgenticOutKey
		}
		superChat, serr := newEinoAgenticChatModelAgentAdapter(ctx, supCfg)
		if serr != nil {
			return nil, fmt.Errorf("supervisor agentic 主代理: %w", serr)
		}
		supRoot, serr := supervisor.New(ctx, &supervisor.Config{
			Supervisor: superChat,
			SubAgents:  supervisorSubAgents,
		})
		if serr != nil {
			return nil, fmt.Errorf("supervisor.New: %w", serr)
		}
		da = supRoot
	default:
		dcfg := &deep.TypedConfig[*schema.AgenticMessage]{
			Name:                   orchestratorName,
			Description:            orchDescription,
			ChatModel:              mainModel,
			Instruction:            orchInstruction,
			SubAgents:              subAgents,
			WithoutGeneralSubAgent: ma.WithoutGeneralSubAgent,
			WithoutWriteTodos:      ma.WithoutWriteTodos,
			MaxIteration:           deepMaxIter,
			Backend:                deepBackend,
			StreamingShell:         deepShell,
			Handlers:               deepHandlers,
			ToolsConfig:            mainToolsCfg,
			ModelRetryConfig:       agenticModelRetryCfg,
			ModelFailoverConfig:    agenticModelFailoverCfg,
		}
		if deepAgenticOutKey != "" {
			dcfg.OutputKey = deepAgenticOutKey
		}
		if agenticTaskGen != nil {
			dcfg.TaskToolDescriptionGenerator = agenticTaskGen
		}
		dDeep, derr := deep.NewTyped[*schema.AgenticMessage](ctx, dcfg)
		if derr != nil {
			return nil, fmt.Errorf("deep.NewTyped[AgenticMessage]: %w", derr)
		}
		da = newEinoAgenticMessageAgentAdapter(dDeep)
	}

	baseMsgs := historyToMessages(history, appCfg, &ma.EinoMiddleware)
	baseMsgs = appendUserMessageIfNeeded(baseMsgs, runtimeUserMessage)

	streamsMainAssistant := func(agent string) bool {
		if orchMode == "plan_execute" {
			return planExecuteStreamsMainAssistant(agent)
		}
		return agent == "" || agent == orchestratorName
	}
	einoRoleTag := func(agent string) string {
		if orchMode == "plan_execute" {
			return planExecuteEinoRoleTag(agent)
		}
		if streamsMainAssistant(agent) {
			return "orchestrator"
		}
		return "sub"
	}

	return runEinoADKAgentLoop(ctx, &einoADKRunLoopArgs{
		OrchMode:             orchMode,
		OrchestratorName:     orchestratorName,
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
		DA:                      da,
		ModelFacingTrace:        modelFacingTrace,
		EinoCallbacks:           &ma.EinoCallbacks,
		MaxTotalTokens:          appCfg.OpenAI.MaxTotalTokens,
		ToolMaxBytes:            toolMaxBytesFromMW(&ma.EinoMiddleware),
		ModelName:               appCfg.OpenAI.Model,
		MiddlewareConfig:        &ma.EinoMiddleware,
		EmptyResponseMessage: "(Eino multi-agent orchestration completed but no assistant text was captured. Check process details or logs.) " +
			"（Eino 多代理编排已完成，但未捕获到助手文本输出。请查看过程详情或日志。）",
	}, baseMsgs)
}

func chatToolCallsToSchema(tcs []agent.ToolCall) []schema.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]schema.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		if strings.TrimSpace(tc.ID) == "" {
			continue
		}
		argsStr := ""
		if tc.Function.Arguments != nil {
			b, err := json.Marshal(tc.Function.Arguments)
			if err == nil {
				argsStr = string(b)
			}
		}
		// Some OpenAI-compatible gateways require `function.arguments` to exist
		// on every assistant tool_call message. When args are empty, omitempty may
		// drop the field during serialization and cause "missing field arguments"
		// on the next turn history replay.
		if strings.TrimSpace(argsStr) == "" {
			argsStr = "{}"
		}
		typ := tc.Type
		if typ == "" {
			typ = "function"
		}
		out = append(out, schema.ToolCall{
			ID:   tc.ID,
			Type: typ,
			Function: schema.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: argsStr,
			},
		})
	}
	return out
}

// historyToMessages 将已保存的 model-facing 轨迹转为 Eino ADK 消息。
// 新轨迹应已是模型实际看到的内容；对旧版本遗留的超大 tool 正文再做一次上限规范化，
// 防止原始工具输出通过 last_react/checkpoint 绕过 reduction。
func historyToMessages(history []agent.ChatMessage, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) []adk.Message {
	toolContentMax := config.MultiAgentEinoMiddlewareConfig{}.ReductionMaxLengthForTruncEffective()
	userContentMaxRunes := config.MultiAgentEinoMiddlewareConfig{}.LatestUserMessageMaxRunesEffective()
	if mwCfg != nil {
		toolContentMax = mwCfg.ReductionMaxLengthForTruncEffective()
		userContentMaxRunes = mwCfg.LatestUserMessageMaxRunesEffective()
	}
	if appCfg != nil {
		userContentMaxRunes = minPositiveInt(userContentMaxRunes, modelFacingRuneBudget(appCfg.OpenAI.MaxTotalTokens, 0.20))
	}
	if len(history) == 0 {
		return nil
	}
	raw := make([]adk.Message, 0, len(history))
	for _, h := range history {
		role := strings.ToLower(strings.TrimSpace(h.Role))
		switch role {
		case "user":
			if strings.TrimSpace(h.Content) != "" {
				content := h.Content
				if !h.ModelFacingTrace {
					content = normalizeRestoredUserContent(content, userContentMaxRunes)
				}
				raw = append(raw, schema.UserMessage(content))
			}
		case "assistant":
			toolSchema := chatToolCallsToSchema(h.ToolCalls)
			hasRC := strings.TrimSpace(h.ReasoningContent) != ""
			if len(toolSchema) > 0 || strings.TrimSpace(h.Content) != "" || hasRC {
				am := schema.AssistantMessage(h.Content, toolSchema)
				if hasRC {
					am.ReasoningContent = strings.TrimSpace(h.ReasoningContent)
				}
				raw = append(raw, am)
			}
		case "tool":
			if strings.TrimSpace(h.ToolCallID) == "" && strings.TrimSpace(h.Content) == "" {
				continue
			}
			var opts []schema.ToolMessageOption
			if tn := strings.TrimSpace(h.ToolName); tn != "" {
				opts = append(opts, schema.WithToolName(tn))
			}
			content := h.Content
			if !h.ModelFacingTrace || (toolContentMax > 0 && len(content) > toolContentMax) {
				content = normalizeRestoredToolContent(content, toolContentMax)
			}
			raw = append(raw, schema.ToolMessage(content, h.ToolCallID, opts...))
		default:
			continue
		}
	}
	return raw
}

func normalizeRestoredUserContent(content string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(content) <= maxRunes {
		return content
	}
	runes := []rune(content)
	const marker = "\n\n...[historical user input normalized to the model-facing budget]...\n\n"
	markerRunes := []rune(marker)
	budget := maxRunes - len(markerRunes)
	if budget <= 0 {
		end := maxRunes
		if end > len(markerRunes) {
			end = len(markerRunes)
		}
		return string(markerRunes[:end])
	}
	head := budget / 2
	tail := budget - head
	return string(runes[:head]) + marker + string(runes[len(runes)-tail:])
}

func normalizeRestoredToolContent(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	const marker = "\n\n...[legacy tool output discarded during model-facing history migration]...\n\n"
	budget := maxBytes - len(marker)
	if budget <= 0 {
		return marker
	}
	head := budget / 2
	tail := budget - head
	for head > 0 && !utf8.RuneStart(content[head]) {
		head--
	}
	tailStart := len(content) - tail
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	return content[:head] + marker + content[tailStart:]
}

// mergeStreamingToolCallFragments 将流式多帧的 ToolCall 按 index 合并 arguments（与 schema.concatToolCalls 行为一致）。
func mergeStreamingToolCallFragments(fragments []schema.ToolCall) []schema.ToolCall {
	if len(fragments) == 0 {
		return nil
	}
	m, err := schema.ConcatMessages([]*schema.Message{{ToolCalls: fragments}})
	if err != nil || m == nil {
		return fragments
	}
	return m.ToolCalls
}

// mergeMessageToolCalls 非流式路径上若仍带分片式 tool_calls，合并后再上报 UI。
func mergeMessageToolCalls(msg *schema.Message) *schema.Message {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return msg
	}
	m, err := schema.ConcatMessages([]*schema.Message{msg})
	if err != nil || m == nil {
		return msg
	}
	out := *msg
	out.ToolCalls = m.ToolCalls
	return &out
}

// toolCallStableID 用于流式阶段去重；OpenAI 流式常先给 index 后补 id。
func toolCallStableID(tc schema.ToolCall) string {
	if tc.ID != "" {
		return tc.ID
	}
	if tc.Index != nil {
		return fmt.Sprintf("idx:%d", *tc.Index)
	}
	return ""
}

// toolCallDisplayName returns the visible tool name once the model stream has
// produced a concrete function name. Anonymous stream fragments are filtered
// before progress emission instead of being guessed as task calls.
func toolCallDisplayName(tc schema.ToolCall) string {
	if n := strings.TrimSpace(tc.Function.Name); n != "" {
		return n
	}
	if n := strings.TrimSpace(tc.Type); n != "" && !strings.EqualFold(n, "function") {
		return n
	}
	return ""
}

// toolCallsSignatureFlush 用于去重键；无 id/index 时用占位 pos，避免流末帧缺 id 时整条工具事件丢失。
func toolCallsSignatureFlush(msg *schema.Message) string {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return ""
	}
	visible := filterVisibleToolCallsForProgress(msg.ToolCalls)
	if len(visible) == 0 {
		return ""
	}
	parts := make([]string, 0, len(visible))
	for i, tc := range visible {
		id := toolCallStableID(tc)
		if id == "" {
			id = fmt.Sprintf("pos:%d", i)
		}
		name := toolCallDisplayName(tc)
		if name == "" {
			continue
		}
		parts = append(parts, id+"|"+name)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// toolCallsRichSignature 用于去重：同一次流式已上报后，紧随其后的非流式消息常带相同 tool_calls。
func toolCallsRichSignature(msg *schema.Message) string {
	base := toolCallsSignatureFlush(msg)
	if base == "" {
		return ""
	}
	visible := filterVisibleToolCallsForProgress(msg.ToolCalls)
	parts := make([]string, 0, len(visible))
	for _, tc := range visible {
		id := toolCallStableID(tc)
		arg := tc.Function.Arguments
		if len(arg) > 240 {
			arg = arg[:240]
		}
		parts = append(parts, id+":"+arg)
	}
	sort.Strings(parts)
	return base + "|" + strings.Join(parts, ";")
}

func einoMainIterationKey(agentName, orchestratorName string) string {
	key := strings.TrimSpace(agentName)
	if key == "" {
		key = strings.TrimSpace(orchestratorName)
	}
	if key == "" {
		return "_main"
	}
	return key
}

func tryEmitToolCallsOnce(
	msg *schema.Message,
	agentName, orchestratorName, conversationID, orchMode string,
	progress func(string, string, interface{}),
	seen map[string]struct{},
	subAgentToolStep, mainAgentToolStep map[string]int,
	markPending func(toolCallPendingInfo),
) {
	if msg == nil || len(msg.ToolCalls) == 0 || progress == nil || seen == nil {
		return
	}
	if toolCallsSignatureFlush(msg) == "" {
		return
	}
	sig := agentName + "\x1e" + toolCallsRichSignature(msg)
	if _, ok := seen[sig]; ok {
		return
	}
	if idSig := toolCallsStableIDSignature(msg); idSig != "" {
		idKey := agentName + "\x1eids\x1e" + idSig
		if _, ok := seen[idKey]; ok {
			return
		}
		seen[idKey] = struct{}{}
	}
	seen[sig] = struct{}{}
	emitToolCallsFromMessage(msg, agentName, orchestratorName, conversationID, orchMode, progress, subAgentToolStep, mainAgentToolStep, markPending)
}

func toolCallsStableIDSignature(msg *schema.Message) string {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return ""
	}
	visible := filterVisibleToolCallsForProgress(msg.ToolCalls)
	ids := make([]string, 0, len(visible))
	for _, tc := range visible {
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return strings.Join(ids, ";")
}

func emitToolCallsFromMessage(
	msg *schema.Message,
	agentName, orchestratorName, conversationID, orchMode string,
	progress func(string, string, interface{}),
	subAgentToolStep, mainAgentToolStep map[string]int,
	markPending func(toolCallPendingInfo),
) {
	if msg == nil || len(msg.ToolCalls) == 0 || progress == nil {
		return
	}
	visibleToolCalls := filterVisibleToolCallsForProgress(msg.ToolCalls)
	if len(visibleToolCalls) == 0 {
		return
	}
	if subAgentToolStep == nil {
		subAgentToolStep = make(map[string]int)
	}
	isSubToolRound := agentName != "" && agentName != orchestratorName
	if isSubToolRound {
		subAgentToolStep[agentName]++
		n := subAgentToolStep[agentName]
		progress("iteration", "", map[string]interface{}{
			"iteration":      n,
			"einoScope":      "sub",
			"einoRole":       "sub",
			"einoAgent":      agentName,
			"conversationId": conversationID,
			"source":         "eino",
		})
	} else if mainAgentToolStep != nil {
		key := einoMainIterationKey(agentName, orchestratorName)
		mainAgentToolStep[key]++
		n := mainAgentToolStep[key]
		// 第 1 轮已在主代理进入时发出；此后每次工具批次对应新一轮 ReAct（与子代理按工具计步一致）。
		if n > 1 {
			progress("iteration", "", map[string]interface{}{
				"iteration":      n,
				"einoScope":      "main",
				"einoRole":       "orchestrator",
				"einoAgent":      agentName,
				"orchestration":  orchMode,
				"conversationId": conversationID,
				"source":         "eino",
			})
		}
	}
	role := "orchestrator"
	if isSubToolRound {
		role = "sub"
	}
	progress("tool_calls_detected", fmt.Sprintf("检测到 %d 个工具调用", len(visibleToolCalls)), map[string]interface{}{
		"count":          len(visibleToolCalls),
		"conversationId": conversationID,
		"source":         "eino",
		"einoAgent":      agentName,
		"einoRole":       role,
	})
	for idx, tc := range visibleToolCalls {
		argStr := strings.TrimSpace(tc.Function.Arguments)
		if argStr == "" && len(tc.Extra) > 0 {
			if b, mErr := json.Marshal(tc.Extra); mErr == nil {
				argStr = string(b)
			}
		}
		var argsObj map[string]interface{}
		if argStr != "" {
			if uErr := json.Unmarshal([]byte(argStr), &argsObj); uErr != nil || argsObj == nil {
				argsObj = map[string]interface{}{"_raw": argStr}
			}
		}
		display := toolCallDisplayName(tc)
		toolCallID := tc.ID
		if toolCallID == "" && tc.Index != nil {
			// Stream indexes restart from zero for every model turn. Include a
			// process-wide sequence so pending/result de-duplication cannot collide
			// with an earlier batch in the same agent run.
			toolCallID = fmt.Sprintf("eino-stream-%d-%d", fallbackToolCallSequence.Add(1), *tc.Index)
		}
		// Record visible pending tool calls for later tool_result correlation / recovery flushing.
		if markPending != nil && toolCallID != "" {
			markPending(toolCallPendingInfo{
				ToolCallID: toolCallID,
				ToolName:   display,
				Arguments:  argsObj,
				EinoAgent:  agentName,
				EinoRole:   role,
			})
		}
		progress("tool_call", fmt.Sprintf("正在调用工具: %s", display), map[string]interface{}{
			"toolName":       display,
			"arguments":      argStr,
			"argumentsObj":   argsObj,
			"toolCallId":     toolCallID,
			"index":          idx + 1,
			"total":          len(visibleToolCalls),
			"conversationId": conversationID,
			"source":         "eino",
			"einoAgent":      agentName,
			"einoRole":       role,
		})
	}
}

func filterVisibleToolCallsForProgress(calls []schema.ToolCall) []schema.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]schema.ToolCall, 0, len(calls))
	for _, tc := range calls {
		if _, ok := modelOutputRecoveryFromToolCall(tc); ok {
			continue
		}
		if toolCallDisplayName(tc) == "" {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// dedupeRepeatedParagraphs 去掉完全相同的连续/重复段落，缓解多代理各自复述同一列表。
func dedupeRepeatedParagraphs(s string, minLen int) string {
	if s == "" || minLen <= 0 {
		return s
	}
	paras := strings.Split(s, "\n\n")
	var out []string
	seen := make(map[string]bool)
	for _, p := range paras {
		t := strings.TrimSpace(p)
		if len(t) < minLen {
			out = append(out, p)
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, p)
	}
	return strings.TrimSpace(strings.Join(out, "\n\n"))
}

// dedupeParagraphsByLineFingerprint 去掉「正文行集合相同」的重复段落（开场白略不同也会合并），缓解多代理各写一遍目录清单。
func dedupeParagraphsByLineFingerprint(s string, minParaLen int) string {
	if s == "" || minParaLen <= 0 {
		return s
	}
	paras := strings.Split(s, "\n\n")
	var out []string
	seen := make(map[string]bool)
	for _, p := range paras {
		t := strings.TrimSpace(p)
		if len(t) < minParaLen {
			out = append(out, p)
			continue
		}
		fp := paragraphLineFingerprint(t)
		// 指纹仅在「≥4 条非空行」时有效；单行/短段落长回复（如自我介绍）fp 为空，必须保留，否则会误删全文并触发「未捕获到助手文本」占位。
		if fp == "" {
			out = append(out, p)
			continue
		}
		if seen[fp] {
			continue
		}
		seen[fp] = true
		out = append(out, p)
	}
	return strings.TrimSpace(strings.Join(out, "\n\n"))
}

func paragraphLineFingerprint(t string) string {
	lines := strings.Split(t, "\n")
	norm := make([]string, 0, len(lines))
	for _, L := range lines {
		s := strings.TrimSpace(L)
		if s == "" {
			continue
		}
		norm = append(norm, s)
	}
	if len(norm) < 4 {
		return ""
	}
	sort.Strings(norm)
	return strings.Join(norm, "\x1e")
}
