package multiagent

import (
	"context"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// PlanExecuteRootArgs 构建 Eino adk/prebuilt/planexecute 根 Agent 所需参数。
type PlanExecuteRootArgs struct {
	MainToolCallingModel model.ToolCallingChatModel
	AgenticExecModel     model.AgenticModel
	OrchInstruction      string
	ToolsCfg             adk.ToolsConfig
	ExecMaxIter          int
	LoopMaxIter          int
	// AppCfg / Logger 非空时为 Executor 挂载与 Deep/Supervisor 一致的 Eino summarization 中间件。
	AppCfg *config.Config
	MwCfg  *config.MultiAgentEinoMiddlewareConfig
	// ConversationID is used for transcript/isolation paths in middleware.
	ConversationID string
	DB             *database.DB
	ProjectID      string
	Logger         *zap.Logger
	// ModelName is used for model input token estimation logs.
	ModelName string
	// AgenticExecPreMiddlewares 是由 prependEinoAgenticMiddlewares 构建的前置中间件。
	AgenticExecPreMiddlewares   []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	AgenticSkillMiddleware      adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	AgenticFilesystemMiddleware adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	// PlannerReplannerRewriteHandlers applies BeforeModelRewriteState pipeline for planner/replanner input.
	PlannerReplannerRewriteHandlers []adk.ChatModelAgentMiddleware
	// ModelFacingTrace 可选：由 Executor Handlers 链末尾写入，供 last_react 与 summarization 后上下文对齐。
	ModelFacingTrace           *modelFacingTraceHolder
	AgenticModelRetryConfig    *adk.TypedModelRetryConfig[*schema.AgenticMessage]
	AgenticModelFailoverConfig *adk.ModelFailoverConfig[*schema.AgenticMessage]
}

// NewPlanExecuteRoot 返回 plan → execute → replan 预置编排根节点（与 Deep / Supervisor 并列）。
func NewPlanExecuteRoot(ctx context.Context, a *PlanExecuteRootArgs) (adk.ResumableAgent, error) {
	if a == nil {
		return nil, fmt.Errorf("plan_execute: args is nil")
	}
	if a.MainToolCallingModel == nil || a.AgenticExecModel == nil {
		return nil, fmt.Errorf("plan_execute: model is nil")
	}
	tcm, ok := interface{}(a.MainToolCallingModel).(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("plan_execute: main model must implement ToolCallingChatModel")
	}
	plannerCfg := &planexecute.PlannerConfig{
		ToolCallingChatModel: tcm,
		NewPlan:              newLenientPlan,
	}
	if fn := planExecutePlannerGenInput(a.OrchInstruction, a.AppCfg, a.MwCfg, a.Logger, a.ModelName, a.ConversationID, a.PlannerReplannerRewriteHandlers); fn != nil {
		plannerCfg.GenInputFn = fn
	}
	planner, err := planexecute.NewPlanner(ctx, plannerCfg)
	if err != nil {
		return nil, fmt.Errorf("plan_execute planner: %w", err)
	}
	replanner, err := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel:  tcm,
		GenInputFn: planExecuteReplannerGenInput(a.OrchInstruction, a.AppCfg, a.MwCfg, a.Logger, a.ModelName, a.ConversationID, a.PlannerReplannerRewriteHandlers),
		NewPlan:    newLenientPlan,
	})
	if err != nil {
		return nil, fmt.Errorf("plan_execute replanner: %w", err)
	}

	var executor adk.Agent
	agenticExecHandlers, herr := buildPlanExecuteAgenticExecutorHandlers(ctx, a)
	if herr != nil {
		return nil, herr
	}
	executor, err = newPlanExecuteAgenticExecutor(ctx, &planexecute.ExecutorConfig{
		ToolsConfig:   a.ToolsCfg,
		MaxIterations: a.ExecMaxIter,
		GenInputFn:    planExecuteExecutorGenInput(a.OrchInstruction, a.AppCfg, a.MwCfg, a.Logger, a.ModelName, a.ConversationID),
	}, a.AgenticExecModel, agenticExecHandlers, a.AgenticModelRetryConfig, a.AgenticModelFailoverConfig)
	if err != nil {
		return nil, fmt.Errorf("plan_execute executor: %w", err)
	}
	loopMax := a.LoopMaxIter
	if loopMax <= 0 {
		loopMax = 10
	}
	return planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		Replanner:     replanner,
		MaxIterations: loopMax,
	})
}

func buildPlanExecuteAgenticExecutorHandlers(ctx context.Context, a *PlanExecuteRootArgs) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	if a == nil {
		return nil, fmt.Errorf("plan_execute: args is nil")
	}
	var execHandlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	if len(a.AgenticExecPreMiddlewares) > 0 {
		execHandlers = append(execHandlers, a.AgenticExecPreMiddlewares...)
	}
	if a.AgenticFilesystemMiddleware != nil {
		execHandlers = append(execHandlers, a.AgenticFilesystemMiddleware)
	}
	if a.AgenticSkillMiddleware != nil {
		execHandlers = append(execHandlers, a.AgenticSkillMiddleware)
	}
	if a.AppCfg != nil {
		sumMw, sumErr := newEinoAgenticSummarizationMiddleware(ctx, a.AgenticExecModel, a.AppCfg, a.MwCfg, a.ConversationID, a.DB, a.ProjectID, a.Logger, "plan_execute/executor")
		if sumErr != nil {
			return nil, fmt.Errorf("plan_execute agentic executor summarization: %w", sumErr)
		}
		execHandlers = appendEinoAgenticChatModelTailMiddlewares(execHandlers, einoChatModelTailConfig{
			logger:               a.Logger,
			phase:                "plan_execute_executor",
			agenticSummarization: sumMw,
			modelName:            a.ModelName,
			maxTotalTokens:       a.AppCfg.OpenAI.MaxTotalTokens,
			toolMaxBytes:         toolMaxBytesFromMW(a.MwCfg),
			conversationID:       a.ConversationID,
			trace:                a.ModelFacingTrace,
			middlewareConfig:     a.MwCfg,
		})
	}
	return execHandlers, nil
}

// planExecutePlannerGenInput 将 orchestrator instruction 作为 SystemMessage 注入 planner 输入。
// 返回 nil 时 Eino 使用内置默认 planner prompt。
func planExecutePlannerGenInput(
	orchInstruction string,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	modelName string,
	conversationID string,
	rewriteHandlers []adk.ChatModelAgentMiddleware,
) planexecute.GenPlannerModelInputFn {
	oi := strings.TrimSpace(orchInstruction)
	if oi == "" && appCfg == nil {
		return nil
	}
	return func(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
		userInput = capPlanExecuteUserInputMessages(userInput, appCfg, mwCfg)
		msgs := make([]adk.Message, 0, len(userInput))
		msgs = append(msgs, userInput...)
		if rewritten, rerr := applyBeforeModelRewriteHandlers(ctx, msgs, rewriteHandlers); rerr == nil && len(rewritten) > 0 {
			msgs = rewritten
		}
		msgs = normalizeSingleLeadingSystemMessage(msgs, oi)
		logPlanExecuteModelInputEstimate(logger, modelName, conversationID, "plan_execute_planner", msgs)
		return msgs, nil
	}
}

func planExecuteExecutorGenInput(
	orchInstruction string,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	modelName string,
	conversationID string,
) planexecute.GenModelInputFn {
	oi := strings.TrimSpace(orchInstruction)
	return func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
		planContent, err := in.Plan.MarshalJSON()
		if err != nil {
			return nil, err
		}
		userMsgs, err := planexecute.ExecutorPrompt.Format(ctx, map[string]any{
			"input":          planExecuteFormatInput(capPlanExecuteUserInputMessages(in.UserInput, appCfg, mwCfg)),
			"plan":           string(planContent),
			"executed_steps": planExecuteFormatExecutedSteps(in.ExecutedSteps, appCfg, mwCfg),
			"step":           in.Plan.FirstStep(),
		})
		if err != nil {
			return nil, err
		}
		userMsgs = withPlanExecuteInputProvenance(userMsgs, in.UserInput, appCfg, mwCfg, agent.PlanExecuteExecutorContext)
		userMsgs = normalizeSingleLeadingSystemMessage(userMsgs, oi)
		logPlanExecuteModelInputEstimate(logger, modelName, conversationID, "plan_execute_executor_gen_input", userMsgs)
		return userMsgs, nil
	}
}

func planExecuteFormatInput(input []adk.Message) string {
	var sb strings.Builder
	for _, msg := range input {
		if msg == nil {
			continue
		}
		appendTranscriptMessage(&sb, msg)
	}
	return sb.String()
}

func planExecuteFormatExecutedSteps(results []planexecute.ExecutedStep, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) string {
	capped := capPlanExecuteExecutedStepsWithConfig(results, mwCfg)
	return renderPlanExecuteStepsByBudget(capped, appCfg, mwCfg)
}

// planExecuteReplannerGenInput 与 Eino 默认 Replanner 输入一致，但 executed_steps 经 cap 后再写入 prompt，
// 且在 orchInstruction 非空时 prepend SystemMessage 使 replanner 也能接收全局指令。
func planExecuteReplannerGenInput(
	orchInstruction string,
	appCfg *config.Config,
	mwCfg *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	modelName string,
	conversationID string,
	rewriteHandlers []adk.ChatModelAgentMiddleware,
) planexecute.GenModelInputFn {
	oi := strings.TrimSpace(orchInstruction)
	return func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
		planContent, err := in.Plan.MarshalJSON()
		if err != nil {
			return nil, err
		}
		msgs, err := planexecute.ReplannerPrompt.Format(ctx, map[string]any{
			"plan":           string(planContent),
			"input":          planExecuteFormatInput(capPlanExecuteUserInputMessages(in.UserInput, appCfg, mwCfg)),
			"executed_steps": planExecuteFormatExecutedSteps(in.ExecutedSteps, appCfg, mwCfg),
			"plan_tool":      planexecute.PlanToolInfo.Name,
			"respond_tool":   planexecute.RespondToolInfo.Name,
		})
		if err != nil {
			return nil, err
		}
		msgs = withPlanExecuteInputProvenance(msgs, in.UserInput, appCfg, mwCfg, agent.PlanExecuteReplannerContext)
		if rewritten, rerr := applyBeforeModelRewriteHandlers(ctx, msgs, rewriteHandlers); rerr == nil && len(rewritten) > 0 {
			msgs = rewritten
		}
		msgs = normalizeSingleLeadingSystemMessage(msgs, oi)
		logPlanExecuteModelInputEstimate(logger, modelName, conversationID, "plan_execute_replanner", msgs)
		return msgs, nil
	}
}

// normalizeSingleLeadingSystemMessage enforces a provider-friendly message shape:
// exactly one system message at index 0 (when any system context exists).
// For strict OpenAI-compatible backends (e.g. qwen/vllm templates), this avoids
// "System message must be at the beginning" caused by multiple/disordered system messages.
func normalizeSingleLeadingSystemMessage(msgs []adk.Message, extraSystem string) []adk.Message {
	extraSystem = strings.TrimSpace(extraSystem)
	if len(msgs) == 0 {
		if extraSystem == "" {
			return msgs
		}
		return []adk.Message{schema.SystemMessage(extraSystem)}
	}

	systemParts := make([]string, 0, 2)
	if extraSystem != "" {
		systemParts = append(systemParts, extraSystem)
	}
	nonSystem := make([]adk.Message, 0, len(msgs))
	var ledgerEntries []string
	ledgerPresent, canonicalLedgerPresent := false, false
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role == schema.System {
			entries := trustedUserIntentLedgerEntries(msg)
			if _, hasProvenance := msg.Extra[userIntentLedgerExtraKey]; hasProvenance {
				ledgerEntries, ledgerPresent, canonicalLedgerPresent = entries, true, true
			} else if !canonicalLedgerPresent && len(entries) > 0 {
				ledgerEntries, ledgerPresent = entries, true
			}
			if s := strings.TrimSpace(msg.Content); s != "" {
				systemParts = append(systemParts, s)
			}
			continue
		}
		nonSystem = append(nonSystem, msg)
	}
	if len(systemParts) == 0 {
		return nonSystem
	}
	out := make([]adk.Message, 0, len(nonSystem)+1)
	system := schema.SystemMessage(strings.Join(systemParts, "\n\n"))
	if ledgerPresent {
		system.Extra = map[string]interface{}{userIntentLedgerExtraKey: append([]string(nil), ledgerEntries...)}
	}
	out = append(out, system)
	out = append(out, nonSystem...)
	return out
}

func capPlanExecuteUserInputMessages(input []adk.Message, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) []adk.Message {
	if len(input) == 0 {
		return input
	}
	maxTotal := 120000
	modelName := "gpt-4o"
	if appCfg != nil {
		if appCfg.OpenAI.MaxTotalTokens > 0 {
			maxTotal = appCfg.OpenAI.MaxTotalTokens
		}
		if m := strings.TrimSpace(appCfg.OpenAI.Model); m != "" {
			modelName = m
		}
	}
	// Reserve most tokens for planner/replanner prompt and tool schema.
	ratio := 0.35
	if mwCfg != nil {
		ratio = mwCfg.PlanExecuteUserInputBudgetRatioEffective()
	}
	budget := int(float64(maxTotal) * ratio)
	if budget < 4096 {
		budget = 4096
	}
	tc := agent.NewTikTokenCounter()
	out := make([]adk.Message, 0, len(input))
	used := 0
	for i := len(input) - 1; i >= 0; i-- {
		msg := input[i]
		if msg == nil {
			continue
		}
		n, err := tc.Count(modelName, string(msg.Role)+"\n"+msg.Content)
		if err != nil {
			n = (len(msg.Content) + 3) / 4
		}
		if n <= 0 {
			n = 1
		}
		if used+n > budget {
			break
		}
		used += n
		out = append(out, msg)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) == 0 {
		// Keep the latest user message at least.
		return []adk.Message{input[len(input)-1]}
	}
	return out
}

func renderPlanExecuteStepsByBudget(steps []planexecute.ExecutedStep, appCfg *config.Config, mwCfg *config.MultiAgentEinoMiddlewareConfig) string {
	if len(steps) == 0 {
		return ""
	}
	maxTotal := 120000
	modelName := "gpt-4o"
	if appCfg != nil {
		if appCfg.OpenAI.MaxTotalTokens > 0 {
			maxTotal = appCfg.OpenAI.MaxTotalTokens
		}
		if m := strings.TrimSpace(appCfg.OpenAI.Model); m != "" {
			modelName = m
		}
	}
	ratio := 0.2
	if mwCfg != nil {
		ratio = mwCfg.PlanExecuteExecutedStepsBudgetRatioEffective()
	}
	budget := int(float64(maxTotal) * ratio)
	if budget < 3072 {
		budget = 3072
	}
	tc := agent.NewTikTokenCounter()
	var kept []string
	used := 0
	skipped := 0
	for i := len(steps) - 1; i >= 0; i-- {
		block := fmt.Sprintf("Step: %s\nResult: %s\n\n", steps[i].Step, steps[i].Result)
		n, err := tc.Count(modelName, block)
		if err != nil {
			n = (len(block) + 3) / 4
		}
		if n <= 0 {
			n = 1
		}
		if used+n > budget {
			skipped = i + 1
			break
		}
		used += n
		kept = append(kept, block)
	}
	var sb strings.Builder
	if skipped > 0 {
		sb.WriteString(fmt.Sprintf("Earlier executed steps omitted due to context budget: %d steps.\n\n", skipped))
	}
	for i := len(kept) - 1; i >= 0; i-- {
		sb.WriteString(kept[i])
	}
	return sb.String()
}

// planExecuteStreamsMainAssistant 将规划/执行/重规划各阶段助手流式输出映射到主对话区。
func planExecuteStreamsMainAssistant(agent string) bool {
	if agent == "" {
		return true
	}
	switch agent {
	case "planner", "executor", "replanner", "execute_replan", "plan_execute_replan":
		return true
	default:
		return false
	}
}

func planExecuteEinoRoleTag(agent string) string {
	_ = agent
	return "orchestrator"
}
