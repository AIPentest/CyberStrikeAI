package multiagent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type einoAgenticChatModelAgentConfig struct {
	Name          string
	Description   string
	Instruction   string
	Model         model.AgenticModel
	ToolsConfig   adk.ToolsConfig
	MaxIterations int
	Exit          tool.BaseTool

	GenModelInput       adk.TypedGenModelInput[*schema.AgenticMessage]
	Handlers            []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	ModelRetryConfig    *adk.TypedModelRetryConfig[*schema.AgenticMessage]
	ModelFailoverConfig *adk.ModelFailoverConfig[*schema.AgenticMessage]
	OutputKey           string
}

func newEinoAgenticChatModelAgent(ctx context.Context, cfg einoAgenticChatModelAgentConfig) (adk.TypedResumableAgent[*schema.AgenticMessage], error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("eino agentic ChatModelAgent: model is required")
	}
	attachAgenticBuiltinActionToolMiddleware(&cfg.ToolsConfig)
	cfg.Exit = replaceClassicExitTool(cfg.Exit)
	typedCfg := &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:                cfg.Name,
		Description:         cfg.Description,
		Instruction:         cfg.Instruction,
		Model:               cfg.Model,
		ToolsConfig:         cfg.ToolsConfig,
		MaxIterations:       cfg.MaxIterations,
		Exit:                cfg.Exit,
		GenModelInput:       cfg.GenModelInput,
		Handlers:            cfg.Handlers,
		ModelRetryConfig:    cfg.ModelRetryConfig,
		ModelFailoverConfig: cfg.ModelFailoverConfig,
		OutputKey:           cfg.OutputKey,
	}
	typedAgent, err := adk.NewTypedChatModelAgent(ctx, typedCfg)
	if err != nil {
		return nil, fmt.Errorf("eino agentic NewTypedChatModelAgent: %w", err)
	}
	return typedAgent, nil
}

func newEinoAgenticChatModelAgentAdapter(ctx context.Context, cfg einoAgenticChatModelAgentConfig) (adk.Agent, error) {
	typedAgent, err := newEinoAgenticChatModelAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	agent := newEinoAgenticMessageAgentAdapter(typedAgent)
	if agent == nil {
		return nil, fmt.Errorf("eino agentic ChatModelAgent: adapter is nil")
	}
	return agent, nil
}
