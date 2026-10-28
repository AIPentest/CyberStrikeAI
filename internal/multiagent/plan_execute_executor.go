package multiagent

import (
	"context"
	"fmt"

	"cyberstrike-ai/internal/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func newPlanExecuteAgenticExecutor(
	ctx context.Context,
	cfg *planexecute.ExecutorConfig,
	agenticModel model.AgenticModel,
	handlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage],
	modelRetryCfg *adk.TypedModelRetryConfig[*schema.AgenticMessage],
	modelFailoverCfg *adk.ModelFailoverConfig[*schema.AgenticMessage],
) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("plan_execute: ExecutorConfig is nil")
	}
	if agenticModel == nil {
		return nil, fmt.Errorf("plan_execute: Executor AgenticModel is nil")
	}
	genInputFn := cfg.GenInputFn
	if genInputFn == nil {
		genInputFn = planExecuteDefaultGenExecutorInput
	}
	genInput := func(ctx context.Context, instruction string, _ *adk.TypedAgentInput[*schema.AgenticMessage]) ([]*schema.AgenticMessage, error) {
		plan, ok := adk.GetSessionValue(ctx, planexecute.PlanSessionKey)
		if !ok {
			return nil, fmt.Errorf("plan_execute executor: session value %q missing (possible session corruption)", planexecute.PlanSessionKey)
		}
		plan_, ok := plan.(planexecute.Plan)
		if !ok {
			return nil, fmt.Errorf("plan_execute executor: session value %q has invalid type %T", planexecute.PlanSessionKey, plan)
		}

		userInput, ok := adk.GetSessionValue(ctx, planexecute.UserInputSessionKey)
		if !ok {
			return nil, fmt.Errorf("plan_execute executor: session value %q missing (possible session corruption)", planexecute.UserInputSessionKey)
		}
		userInput_, ok := userInput.([]adk.Message)
		if !ok {
			return nil, fmt.Errorf("plan_execute executor: session value %q has invalid type %T", planexecute.UserInputSessionKey, userInput)
		}

		var executedSteps_ []planexecute.ExecutedStep
		executedStep, ok := adk.GetSessionValue(ctx, planexecute.ExecutedStepsSessionKey)
		if ok {
			executedSteps_, ok = executedStep.([]planexecute.ExecutedStep)
			if !ok {
				return nil, fmt.Errorf("plan_execute executor: session value %q has invalid type %T", planexecute.ExecutedStepsSessionKey, executedStep)
			}
		}

		in := &planexecute.ExecutionContext{
			UserInput:     userInput_,
			Plan:          plan_,
			ExecutedSteps: executedSteps_,
		}
		msgs, err := genInputFn(ctx, in)
		if err != nil {
			return nil, err
		}
		if instruction != "" {
			msgs = normalizeSingleLeadingSystemMessage(msgs, instruction)
		}
		return EinoMessagesToAgentic(msgs), nil
	}

	agentCfg := einoAgenticChatModelAgentConfig{
		Name:                "executor",
		Description:         "an executor agent",
		Model:               agenticModel,
		ToolsConfig:         cfg.ToolsConfig,
		GenModelInput:       genInput,
		MaxIterations:       cfg.MaxIterations,
		OutputKey:           planexecute.ExecutedStepSessionKey,
		Handlers:            handlers,
		ModelRetryConfig:    modelRetryCfg,
		ModelFailoverConfig: modelFailoverCfg,
	}
	return newEinoAgenticChatModelAgentAdapter(ctx, agentCfg)
}

// planExecuteDefaultGenExecutorInput 对齐 Eino planexecute.defaultGenExecutorInputFn（包外不可引用默认实现）。
func planExecuteDefaultGenExecutorInput(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
	planContent, err := in.Plan.MarshalJSON()
	if err != nil {
		return nil, err
	}
	msgs, err := planexecute.ExecutorPrompt.Format(ctx, map[string]any{
		"input":          planExecuteFormatInput(in.UserInput),
		"plan":           string(planContent),
		"executed_steps": planExecuteFormatExecutedSteps(in.ExecutedSteps, nil, nil),
		"step":           in.Plan.FirstStep(),
	})
	if err != nil {
		return nil, err
	}
	return withPlanExecuteInputProvenance(msgs, in.UserInput, nil, nil, agent.PlanExecuteExecutorContext), nil
}
