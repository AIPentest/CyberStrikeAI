package mcp

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/toolguard"
)

// toolGuardBlockedPrefix is a legacy wire/DB protocol marker, not display text:
// frontend monitor.js legacy parsing and the tool_guard_migration DB scanner both
// pattern-match this exact byte sequence on historical rows, so it must stay
// byte-identical even as user-visible strings are translated.
// The "规则: ... / 匹配内容: ..." envelope below is likewise matched verbatim
// by internal/database/tool_guard_migration.go on stored rows; keep it
// byte-identical.
const toolGuardBlockedPrefix = "工具调用已被安全规则拦截"
const toolGuardBlockedMetaKey = "cyberstrike.ai/blocked"

// toolGuardBlockError carries structured policy results through pre-run hooks.
type toolGuardBlockError struct{ result *ToolResult }

func (e *toolGuardBlockError) Error() string { return ToolResultPlainText(e.result) }

func toolResultProtocolMeta(result *ToolResult) map[string]interface{} {
	if result != nil && result.Blocked {
		return map[string]interface{}{toolGuardBlockedMetaKey: true}
	}
	return nil
}

// toolGuardBlockedResult uses the standard MCP error result so the refusal is
// visible both to the model and in persisted execution monitoring records.
func toolGuardBlockedResult(guard *toolguard.Manager, toolName string, args map[string]interface{}) *ToolResult {
	if guard == nil {
		return nil
	}
	match := guard.Check(toolName, args)
	if match == nil {
		return nil
	}
	message := toolGuardBlockedPrefix
	if custom := strings.TrimSpace(match.Message); custom != "" {
		message += "：" + custom
	}
	message += fmt.Sprintf("\n规则: %s (%s)\n匹配内容: %q", match.RuleName, match.RuleID, match.MatchedText)
	return &ToolResult{Content: []Content{{Type: "text", Text: message}}, IsError: true, Blocked: true}
}
