package multiagent

import (
	"context"
	"encoding/json"
	"fmt"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"go.uber.org/zap"
)

// CompleteBackgroundStep performs one model turn without entering chat/ADK run
// persistence. Durable callers own their budget, transcript and task lifecycle.
// Provider routing, headers and reasoning stay identical to the existing models.
func CompleteBackgroundStep(ctx context.Context, oa config.OpenAIConfig, msgs []agent.ChatMessage, tools []agent.Tool, maxTokens int, logger *zap.Logger) (*agent.ChatMessage, int, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if oa.MaxCompletionTokensEffective() > maxTokens {
		oa.MaxCompletionTokens = maxTokens
	}
	var chat model.ToolCallingChatModel
	var err error
	if oa.OpenAIToolsRequireResponses() {
		native, nerr := newEinoAgenticChatModelFactory(nil, nil, logger)(ctx, oa, einoModelModeNormal)
		if nerr != nil {
			return nil, 0, nerr
		}
		chat = newAgenticToolCallingChatModelAdapter(native)
	} else {
		chat, err = newEinoToolCallingChatModelFactory(nil, nil, logger)(ctx, oa, einoModelModeNormal)
		if err != nil {
			return nil, 0, err
		}
	}
	input := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		sm := &schema.Message{Role: schema.RoleType(m.Role), Content: m.Content, ReasoningContent: m.ReasoningContent, ToolCallID: m.ToolCallID, ToolName: m.ToolName}
		for _, tc := range m.ToolCalls {
			args, err := json.Marshal(tc.Function.Arguments)
			if err != nil {
				return nil, 0, err
			}
			sm.ToolCalls = append(sm.ToolCalls, schema.ToolCall{ID: tc.ID, Type: "function", Function: schema.FunctionCall{Name: tc.Function.Name, Arguments: string(args)}})
		}
		input = append(input, sm)
	}
	defs := []*schema.ToolInfo{}
	for _, t := range tools {
		b, err := json.Marshal(t.Function.Parameters)
		if err != nil {
			return nil, 0, err
		}
		var js jsonschema.Schema
		if err = json.Unmarshal(b, &js); err != nil {
			return nil, 0, err
		}
		defs = append(defs, &schema.ToolInfo{Name: t.Function.Name, Desc: t.Function.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&js)})
	}
	if len(defs) > 0 {
		chat, err = chat.WithTools(defs)
		if err != nil {
			return nil, 0, err
		}
	}
	response, err := chat.Generate(ctx, input, model.WithMaxTokens(oa.MaxCompletionTokensEffective()))
	if err != nil {
		return nil, 0, err
	}
	if response == nil {
		return nil, 0, fmt.Errorf("model returned no message")
	}
	out := &agent.ChatMessage{Role: string(response.Role), Content: response.Content, ReasoningContent: response.ReasoningContent}
	for _, tc := range response.ToolCalls {
		var args map[string]interface{}
		if err = json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return nil, 0, err
		}
		out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: tc.ID, Type: "function", Function: agent.FunctionCall{Name: tc.Function.Name, Arguments: args}})
	}
	tokens := 0
	if response.ResponseMeta != nil && response.ResponseMeta.Usage != nil {
		u := response.ResponseMeta.Usage
		tokens = u.TotalTokens
		if tokens == 0 {
			tokens = u.PromptTokens + u.CompletionTokens
		}
	}
	return out, tokens, nil
}
