package multiagent

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// injectToolNamesOnlyInstruction prepends a compact tool-name-only section into
// the system instruction so the model can reference current callable names.
// toolSearchMiddlewareActive must be true when prependEinoMiddlewares mounted toolsearch (dynamic tools); do not infer this
// by scanning tool names — tool_search is injected by middleware and is usually absent from the pre-split tools list.
func injectToolNamesOnlyInstruction(ctx context.Context, instruction string, tools []tool.BaseTool, toolSearchMiddlewareActive bool) string {
	names := collectToolNames(ctx, tools)
	if len(names) == 0 {
		return strings.TrimSpace(instruction)
	}
	hasToolSearch := toolSearchMiddlewareActive
	if !hasToolSearch {
		for _, n := range names {
			if strings.EqualFold(strings.TrimSpace(n), "tool_search") {
				hasToolSearch = true
				break
			}
		}
	}

	var sb strings.Builder
	// Model-visible tool-name index prompt; translated to English.
	// transcriptToolIndexStartMarker / transcriptToolIndexStartMarkerEnglish in
	// eino_summarize_transcript.go dual-accept this heading when stripping.
	sb.WriteString("The following is the tool-name index bound to this session (names only, no parameter JSON Schema).\n")
		sb.WriteString("Note: if tool_search is enabled the list may contain non-resident tools - they are not necessarily among the tool definitions sent to the model for the current turn; do not guess parameters from the name alone before the full schema is seen.\n")
	for _, name := range names {
		sb.WriteString("- ")
		sb.WriteString(name)
		sb.WriteByte('\n')
	}
	sb.WriteString("\n\nUsage rules:\n")
	sb.WriteString("1) The table above is a name index only, without parameter definitions. Do not guess parameter names, types, enum values, or whether they are required.\n")
	if hasToolSearch {
		sb.WriteString("[MANDATORY / HIGHEST PRIORITY] This session has tool_search enabled (a dynamic tool pool). Any tool that appears in the name index but whose full parameter schema you do not see in the attached tools definition of the current request must be resolved via tool_search first; skipping tool_search to save tokens or speed and calling a business tool directly is an explicitly forbidden error path.\n")
		sb.WriteString("2) Default strategy: whenever there is any uncertainty about the target tool's parameter definition, call tool_search first; prefer an extra tool_search over blindly calling a business tool whose schema you have not seen.\n")
		sb.WriteString("3) Call order: first tool_search (its only required parameter regex_pattern is a regex matched against tool names, e.g. the substring nuclei or ^exact_tool_name$) then confirm in a later turn that the target tool appears in the tools list and its schema has been read, then make the real call to that tool.\n")
		sb.WriteString("4) tool_search returns only the matched tool-name list; the schema is delivered only in the turn after unlocking. Never fabricate JSON parameters before the schema appears.\n")
		sb.WriteString("5) Do not invent tool names that do not exist.\n\n")
	} else {
		sb.WriteString("2) Before calling a specific tool, confirm its parameter requirements (per the tool definitions in the current request); clarify first when uncertain, then call.\n")
		sb.WriteString("3) Do not invent tool names that do not exist.\n\n")
	}
	return sb.String()
}

func collectToolNames(ctx context.Context, tools []tool.BaseTool) []string {
	if len(tools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

