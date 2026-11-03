package einomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/security"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// ExecutionRecorder 可选，在 MCP 工具成功返回且带有 execution id 时回调（用于汇总 mcpExecutionIds）。
// toolCallID 来自 Eino compose.GetToolCallID，用于与 reduction 后的展示结果关联。
type ExecutionRecorder func(executionID, toolCallID string)

// ToolErrorPrefix 用于把内部 MCP 执行结果中的 IsError 标记传递到多代理上层。
// Eino 工具通道目前只支持返回字符串，因此通过前缀标识，随后在多代理 runner 中解析为 success/isError。
const ToolErrorPrefix = "__CYBERSTRIKE_AI_TOOL_ERROR__\n"

// ToolsFromDefinitions 将单 Agent 使用的 OpenAI 风格工具定义转为 Eino InvokableTool，执行时走 Agent 的 MCP 路径。
// invokeNotify 可选：与 runEinoADKAgentLoop 共享，在 InvokableRun 返回时触发 UI 与 pending 清理（与 ADK Tool 事件去重）。
// einoAgentName 为该套工具所属 ChatModelAgent 的 Name（主代理或子代理 id），用于 SSE 上的 einoAgent 字段。
func ToolsFromDefinitions(
	ag *agent.Agent,
	holder *ConversationHolder,
	defs []agent.Tool,
	rec ExecutionRecorder,
	toolOutputChunk func(toolName, toolCallID, chunk string),
	invokeNotify *ToolInvokeNotifyHolder,
	einoAgentName string,
) ([]tool.BaseTool, error) {
	out := make([]tool.BaseTool, 0, len(defs))
	for _, d := range defs {
		if d.Type != "function" || d.Function.Name == "" {
			continue
		}
		info, err := toolInfoFromDefinition(d)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", d.Function.Name, err)
		}
		out = append(out, &mcpBridgeTool{
			info:          info,
			name:          d.Function.Name,
			agent:         ag,
			holder:        holder,
			record:        rec,
			chunk:         toolOutputChunk,
			invokeNotify:  invokeNotify,
			einoAgentName: strings.TrimSpace(einoAgentName),
		})
	}
	return out, nil
}

func toolInfoFromDefinition(d agent.Tool) (*schema.ToolInfo, error) {
	fn := d.Function
	raw, err := json.Marshal(fn.Parameters)
	if err != nil {
		return nil, err
	}
	var js jsonschema.Schema
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		if err := json.Unmarshal(raw, &js); err != nil {
			return nil, err
		}
	}
	if js.Type == "" {
		js.Type = string(schema.Object)
	}
	if js.Properties == nil && js.Type == string(schema.Object) {
		// 空参数对象
	}
	return &schema.ToolInfo{
		Name:        sanitizeOpenAIToolName(fn.Name),
		Desc:        fn.Description,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&js),
	}, nil
}

// sanitizeOpenAIToolName converts MCP names to OpenAI's ^[a-zA-Z0-9_-]+$ format.
// The original name remains on mcpBridgeTool and is still used for MCP routing.
func sanitizeOpenAIToolName(name string) string {
	name = strings.ReplaceAll(name, "::", "__")
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

type mcpBridgeTool struct {
	info          *schema.ToolInfo
	name          string
	agent         *agent.Agent
	holder        *ConversationHolder
	record        ExecutionRecorder
	chunk         func(toolName, toolCallID, chunk string)
	invokeNotify  *ToolInvokeNotifyHolder
	einoAgentName string
}

func (m *mcpBridgeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	_ = ctx
	return m.info, nil
}

func (m *mcpBridgeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (out string, err error) {
	_ = opts
	toolCallID := compose.GetToolCallID(ctx)
	defer func() {
		if m.invokeNotify == nil {
			return
		}
		tid := strings.TrimSpace(toolCallID)
		if tid == "" {
			return
		}
		success := err == nil && !strings.HasPrefix(out, ToolErrorPrefix)
		body := out
		if err != nil {
			success = false
		} else if strings.HasPrefix(out, ToolErrorPrefix) {
			success = false
			body = strings.TrimPrefix(out, ToolErrorPrefix)
		}
		m.invokeNotify.Fire(tid, m.name, m.einoAgentName, success, body, err)
	}()
	return runMCPToolInvocation(ctx, m.agent, m.holder, m.name, argumentsInJSON, m.record, m.chunk)
}

// runMCPToolInvocation 与 mcpBridgeTool.InvokableRun 共用。
func runMCPToolInvocation(
	ctx context.Context,
	ag *agent.Agent,
	holder *ConversationHolder,
	toolName string,
	argumentsInJSON string,
	record ExecutionRecorder,
	chunk func(toolName, toolCallID, chunk string),
) (string, error) {
	var args map[string]interface{}
	if argumentsInJSON != "" && argumentsInJSON != "null" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			// Return soft error (nil error) so the eino graph continues and the LLM can self-correct,
			// instead of a hard error that terminates the iteration loop.
			return ToolErrorPrefix + fmt.Sprintf(
				"Invalid tool arguments JSON: %s\n\nPlease ensure the arguments are a valid JSON object "+
					"(double-quoted keys, matched braces, no trailing commas) and retry.\n\n"+
					"（工具参数 JSON 解析失败：%s。请确保 arguments 是合法的 JSON 对象并重试。）",
				err.Error(), err.Error()), nil
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	if chunk != nil {
		toolCallID := compose.GetToolCallID(ctx)
		if toolCallID != "" {
			if existing, ok := ctx.Value(security.ToolOutputCallbackCtxKey).(security.ToolOutputCallback); ok && existing != nil {
				ctx = context.WithValue(ctx, security.ToolOutputCallbackCtxKey, security.ToolOutputCallback(func(c string) {
					existing(c)
					if strings.TrimSpace(c) == "" {
						return
					}
					chunk(toolName, toolCallID, c)
				}))
			} else {
				ctx = context.WithValue(ctx, security.ToolOutputCallbackCtxKey, security.ToolOutputCallback(func(c string) {
					if strings.TrimSpace(c) == "" {
						return
					}
					chunk(toolName, toolCallID, c)
				}))
			}
		}
	}

	res, err := ag.ExecuteMCPToolForConversation(ctx, holder.Get(), toolName, args)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	if res.ExecutionID != "" && record != nil {
		record(res.ExecutionID, compose.GetToolCallID(ctx))
	}
	if res.IsError {
		return ToolErrorPrefix + res.Result, nil
	}
	return res.Result, nil
}

// UnknownToolReminderHandler 供 compose.ToolsNodeConfig.UnknownToolsHandler 使用：
// 模型请求了未注册的工具名时，返回一个「软错误」工具结果（nil error），
// 让模型在同一轮继续自我修正，避免触发 run-loop 级别的 full rerun。
// 不进行名称猜测或映射，避免误执行。
func UnknownToolReminderHandler() func(ctx context.Context, name, input string) (string, error) {
	return func(ctx context.Context, name, input string) (string, error) {
		_ = ctx
		_ = input
		requested := strings.TrimSpace(name)
		// Return a soft tool-result error so the graph keeps running and the LLM
		// can correct tool name/arguments within the same run.
		return ToolErrorPrefix + unknownToolReminderText(requested), nil
	}
}

func unknownToolReminderText(requested string) string {
	if requested == "" {
		requested = "(empty)"
	}
	return fmt.Sprintf(`The tool name %q is not registered for this agent.

Please retry using only names that appear in the tool definitions for this turn (exact match, case-sensitive). Do not invent or rename tools; adjust your plan and continue.

（工具 %q 未注册：请仅使用本回合上下文中给出的工具名称，须完全一致；请勿自行改写或猜测名称，并继续后续步骤。）`, requested, requested)
}
