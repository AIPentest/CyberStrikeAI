package multiagent

import (
	"encoding/json"
	"fmt"
	"strings"
)

const toolSearchToolName = "tool_search"

// HitlExemptMetaTools 为 HITL 内置免审批工具：包括编排/元工具，以及模型输出修复链路依赖的 write_file。
// tool_search 必须免审批，否则其 HITL 拒绝结果与 Agent toolsearch 中间件不兼容（会硬崩 ChatModel）；
// write_file 必须免审批，否则长脚本或请求体无法先安全落盘，模型输出修复链路会被再次阻塞。
var HitlExemptMetaTools = []string{
	toolSearchToolName,
	"skill",
	"task",
	"write_todos",
	"write_file",
	"transfer_to_agent",
	"exit",
	"TaskCreate",
	"TaskGet",
	"TaskUpdate",
	"TaskList",
	"upsert_project_fact",
	"get_project_fact",
}

// IsToolSearchTool reports whether name is the Agent dynamictool tool_search meta-tool.
func IsToolSearchTool(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), toolSearchToolName)
}

// MergeHitlExemptMetaTools unions configured whitelist with built-in meta-tool exemptions.
func MergeHitlExemptMetaTools(configured []string) []string {
	merged := make([]string, 0, len(configured)+len(HitlExemptMetaTools))
	seen := make(map[string]struct{}, len(configured)+len(HitlExemptMetaTools))
	add := func(name string) {
		n := strings.ToLower(strings.TrimSpace(name))
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		merged = append(merged, strings.TrimSpace(name))
	}
	for _, t := range configured {
		add(t)
	}
	for _, t := range HitlExemptMetaTools {
		add(t)
	}
	return merged
}

type toolSearchHitlRejectPayload struct {
	SelectedTools []string `json:"selectedTools"`
	HitlRejected  bool     `json:"_hitlRejected"`
	Reason        string   `json:"reason"`
}

// HitlRejectToolResult returns a tool result body safe for downstream consumers.
// tool_search must stay JSON-shaped so toolsearch.extractSelectedTools does not terminate the graph.
func HitlRejectToolResult(toolName, reason string) string {
	reason = strings.TrimSpace(reason)
	if !IsToolSearchTool(toolName) {
		if reason == "" {
			reason = "rejected by reviewer"
		}
		return fmt.Sprintf("[HITL Reject] Tool '%s' was rejected by reviewer. Reason: %s\nPlease adjust parameters/plan and continue without this call.",
			strings.TrimSpace(toolName), reason)
	}
	payload := toolSearchHitlRejectPayload{
		SelectedTools: []string{},
		HitlRejected:  true,
		Reason:        reason,
	}
	if payload.Reason == "" {
		payload.Reason = "tool_search rejected by reviewer; no dynamic tools unlocked"
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return `{"selectedTools":[],"_hitlRejected":true,"reason":"tool_search rejected by reviewer"}`
	}
	return string(out)
}
