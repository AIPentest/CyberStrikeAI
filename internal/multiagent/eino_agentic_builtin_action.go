package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const agenticExitToolName = "exit"

type agenticCompatibleExitTool struct{}

func (agenticCompatibleExitTool) Info(context.Context) (*schema.ToolInfo, error) {
	return adk.ToolInfoExit, nil
}

func (agenticCompatibleExitTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	return invokeAgenticBuiltinActionTool(ctx, agenticExitToolName, argumentsInJSON)
}

func replaceClassicExitTool(t tool.BaseTool) tool.BaseTool {
	switch t.(type) {
	case adk.ExitTool, *adk.ExitTool:
		return agenticCompatibleExitTool{}
	default:
		return t
	}
}

func attachAgenticBuiltinActionToolMiddleware(cfg *adk.ToolsConfig) {
	if cfg == nil {
		return
	}
	cfg.ToolsNodeConfig.ToolCallMiddlewares = append(
		cfg.ToolsNodeConfig.ToolCallMiddlewares,
		agenticBuiltinActionToolMiddleware(),
	)
}

func agenticBuiltinActionToolMiddleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				if input != nil && isAgenticBuiltinActionTool(input.Name) {
					result, err := invokeAgenticBuiltinActionTool(ctx, input.Name, input.Arguments)
					if err != nil {
						return nil, err
					}
					return &compose.ToolOutput{Result: result}, nil
				}
				return next(ctx, input)
			}
		},
	}
}

func isAgenticBuiltinActionTool(name string) bool {
	switch strings.TrimSpace(name) {
	case agenticExitToolName, adk.TransferToAgentToolName:
		return true
	default:
		return false
	}
}

func invokeAgenticBuiltinActionTool(ctx context.Context, name, argumentsInJSON string) (string, error) {
	switch strings.TrimSpace(name) {
	case agenticExitToolName:
		var params struct {
			FinalResult string `json:"final_result"`
		}
		if err := unmarshalBuiltinActionArgs(argumentsInJSON, &params); err != nil {
			return "", err
		}
		if err := sendADKToolGenAction(ctx, agenticExitToolName, adk.NewExitAction()); err != nil {
			return "", err
		}
		return params.FinalResult, nil
	case adk.TransferToAgentToolName:
		var params struct {
			AgentName string `json:"agent_name"`
		}
		if err := unmarshalBuiltinActionArgs(argumentsInJSON, &params); err != nil {
			return "", err
		}
		if err := sendADKToolGenAction(ctx, adk.TransferToAgentToolName, adk.NewTransferToAgentAction(params.AgentName)); err != nil {
			return "", err
		}
		return fmt.Sprintf("successfully transferred to agent [%s]", params.AgentName), nil
	default:
		return "", fmt.Errorf("unsupported agentic builtin action tool %q", name)
	}
}

func unmarshalBuiltinActionArgs(argumentsInJSON string, dest any) error {
	argumentsInJSON = strings.TrimSpace(argumentsInJSON)
	if argumentsInJSON == "" {
		argumentsInJSON = "{}"
	}
	return json.Unmarshal([]byte(argumentsInJSON), dest)
}
