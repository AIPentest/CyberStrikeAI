package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// softRecoveryToolCallMiddleware returns an InvokableToolMiddleware that catches
// specific recoverable errors from tool execution (JSON parse errors, tool-not-found,
// etc.) and converts them into soft errors: nil error + descriptive error content
// returned to the LLM. This allows the model to self-correct within the same
// iteration rather than crashing the entire graph and requiring a full replay.
//
// Without Invokable (+ Streamable where applicable) registration, a JSON parse failure
// in InvokableRun / StreamableRun propagates as a hard error through the Eino ToolsNode
// → [NodeRunError] → ev.Err, which
// either triggers the full-replay retry loop (expensive) or terminates the run
// entirely once retries are exhausted. With it, the LLM simply sees an error message
// in the tool result and can adjust its next tool call accordingly.
func softRecoveryToolCallMiddleware() compose.InvokableToolMiddleware {
	return func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
			if input != nil && strings.EqualFold(strings.TrimSpace(input.Name), "read_file") {
				input.Arguments = normalizeReadFileArguments(input.Arguments)
			}
			output, err := next(ctx, input)
			if err == nil {
				if output != nil && input != nil {
					output.Result = enrichReadFileToolResult(input.Name, input.Arguments, output.Result)
				}
				return output, nil
			}
			if !isSoftRecoverableToolError(err) {
				return output, err
			}
			// Convert the hard error into a soft error: the LLM will see this
			// message as the tool's output and can self-correct.
			msg := buildSoftRecoveryMessage(input.Name, input.Arguments, err)
			return &compose.ToolOutput{Result: msg}, nil
		}
	}
}

// softRecoveryStreamableToolCallMiddleware mirrors softRecoveryToolCallMiddleware for
// tools that implement StreamableTool only (e.g. Eino ADK filesystem execute).
// Eino applies Invokable vs Streamable middleware to disjoint code paths in ToolsNode;
// registering only Invokable leaves streaming tools uncovered — empty/malformed JSON
// then fails inside [LocalStreamFunc] before the inner endpoint runs.
func softRecoveryStreamableToolCallMiddleware() compose.StreamableToolMiddleware {
	return func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
			out, err := next(ctx, input)
			if err == nil {
				return out, nil
			}
			if !isSoftRecoverableToolError(err) {
				return out, err
			}
			toolName := ""
			args := ""
			if input != nil {
				toolName = input.Name
				args = input.Arguments
			}
			msg := buildSoftRecoveryMessage(toolName, args, err)
			return &compose.StreamToolOutput{
				Result: schema.StreamReaderFromArray([]string{msg}),
			}, nil
		}
	}
}

// softRecoveryToolMiddleware returns a ToolMiddleware with both Invokable and Streamable
// soft recovery (same semantics as hitlToolCallMiddleware bundling).
func softRecoveryToolMiddleware() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable:  softRecoveryToolCallMiddleware(),
		Streamable: softRecoveryStreamableToolCallMiddleware(),
	}
}

// isSoftRecoverableToolError determines whether a tool execution error should be
// silently converted to a tool-result message rather than crashing the graph.
//
// Design: default-soft (blacklist). Almost every tool execution error should be
// fed back to the LLM so it can self-correct or choose an alternative tool.
// Only a small set of "truly fatal" conditions (user cancellation) should
// propagate as hard errors that terminate the orchestration graph.
// This avoids the fragile whitelist approach where every new error pattern
// would need to be explicitly enumerated.
func isSoftRecoverableToolError(err error) bool {
	if err == nil {
		return false
	}

	// 用户主动取消 — 唯一应当终止编排的情况，不应重试。
	if errors.Is(err, context.Canceled) {
		return false
	}

	// 其他所有工具执行错误（超时、命令不存在、JSON 解析失败、工具未找到、
	// 权限不足、网络不可达……）一律转为 soft error，让 LLM 看到错误信息
	// 后自行决策：换工具、调整参数、或向用户说明。
	return true
}

// buildSoftRecoveryMessage creates a bilingual error message that the LLM can act on.
func buildSoftRecoveryMessage(toolName, arguments string, err error) string {
	// Truncate arguments preview to avoid flooding the context.
	argPreview := arguments
	if len(argPreview) > 300 {
		argPreview = argPreview[:300] + "... (truncated)"
	}

	// Try to determine if it's specifically a JSON parse error for a friendlier message.
	errStr := err.Error()
	var jsonErr *json.SyntaxError
	isJSONErr := strings.Contains(strings.ToLower(errStr), "json") ||
		strings.Contains(strings.ToLower(errStr), "unmarshal")
	_ = jsonErr // suppress unused

	if isJSONErr {
		return fmt.Sprintf(
			"[Tool Error] The arguments for tool '%s' are not valid JSON and could not be parsed.\n"+
				"Error: %s\n"+
				"Arguments received: %s\n\n"+
				"Please fix the JSON (ensure double-quoted keys, matched braces/brackets, no trailing commas, "+
				"no truncation) and call the tool again.\n\n"+
				"[工具错误] 工具 '%s' 的参数不是合法 JSON，无法解析。\n"+
				"错误：%s\n"+
				"收到的参数：%s\n\n"+
				"请修正 JSON（确保双引号键名、括号配对、无尾部逗号、无截断），然后重新调用工具。",
			toolName, errStr, argPreview,
			toolName, errStr, argPreview,
		)
	}

	return fmt.Sprintf(
		"[Tool Error] Tool '%s' execution failed: %s\n"+
			"Arguments: %s\n\n"+
			"Please review the available tools and their expected arguments, then retry.\n\n"+
			"[工具错误] 工具 '%s' 执行失败：%s\n"+
			"参数：%s\n\n"+
			"请检查可用工具及其参数要求，然后重试。",
		toolName, errStr, argPreview,
		toolName, errStr, argPreview,
	)
}
