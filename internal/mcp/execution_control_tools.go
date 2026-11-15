package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/mcp/builtin"
)

const (
	defaultExecutionWaitTimeout = 60 * time.Second
	maxExecutionWaitTimeout     = 10 * time.Minute
	defaultPartialPreviewBytes  = 4096
	maxPartialPreviewBytes      = 64 * 1024
)

// RegisterExecutionControlTools exposes execution handle operations to Agent as
// ordinary MCP tools. This keeps the agent loop native: the model calls a tool,
// receives a bounded result, and may call wait_tool_execution again if needed.
func RegisterExecutionControlTools(server *Server, external *ExternalMCPManager) {
	if server == nil {
		return
	}

	server.RegisterTool(Tool{
		Name:             builtin.ToolGetToolExecution,
		Description:      "查询后台工具 execution 的当前状态、结果和错误。用于外部 MCP 工具等待超时后，凭 execution_id 继续查看进度。",
		ShortDescription: "查询后台工具执行状态",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id":             map[string]interface{}{"type": "string", "description": "工具执行 ID"},
				"include_partial_output":   map[string]interface{}{"type": "boolean", "description": "是否返回运行中已产生输出的尾部预览，默认 true"},
				"partial_output_max_bytes": map[string]interface{}{"type": "number", "description": "partial_output 最多返回字节数，默认 4096，最大 65536"},
			},
			"required": []string{"execution_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		id := stringArg(args, "execution_id")
		if id == "" {
			return textToolResult("execution_id 必填", true), nil
		}
		exec := lookupToolExecution(server, external, id)
		if exec == nil {
			return textToolResult("未找到该 execution_id: "+id, true), nil
		}
		return textToolResult(formatExecutionForModel(exec, executionFormatOptionsFromArgs(args)), false), nil
	})

	server.RegisterTool(Tool{
		Name:             builtin.ToolWaitToolExecution,
		Description:      "继续等待一个后台工具 execution 完成。每次等待都有 timeout_seconds 上限；若仍未完成，会返回当前状态，模型可稍后再次调用。",
		ShortDescription: "有界等待后台工具执行",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id":             map[string]interface{}{"type": "string", "description": "工具执行 ID"},
				"timeout_seconds":          map[string]interface{}{"type": "number", "description": "本次最多等待秒数，默认 60，最大 600"},
				"include_partial_output":   map[string]interface{}{"type": "boolean", "description": "是否返回运行中已产生输出的尾部预览，默认 true"},
				"partial_output_max_bytes": map[string]interface{}{"type": "number", "description": "partial_output 最多返回字节数，默认 4096，最大 65536"},
			},
			"required": []string{"execution_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		id := stringArg(args, "execution_id")
		if id == "" {
			return textToolResult("execution_id 必填", true), nil
		}
		wait := durationSecondsArg(args, "timeout_seconds", defaultExecutionWaitTimeout, maxExecutionWaitTimeout)
		snap, err := waitToolExecutionSnapshot(ctx, server, external, id, wait)
		if err != nil && !errors.Is(err, ErrExecutionWaitTimeout) {
			return textToolResult("等待 execution 失败: "+err.Error(), true), nil
		}
		if snap == nil || snap.Execution == nil {
			return textToolResult("未找到该 execution_id: "+id, true), nil
		}
		body := formatExecutionForModel(snap.Execution, executionFormatOptionsFromArgs(args))
		if errors.Is(err, ErrExecutionWaitTimeout) {
			body += "\n\n本次等待已到达 timeout_seconds，上述 execution 仍未完成。可继续等待、取消，或采用其他步骤。"
		}
		return textToolResult(body, false), nil
	})

	server.RegisterTool(Tool{
		Name:             builtin.ToolCancelToolExecution,
		Description:      "取消一个后台工具 execution。用于外部 MCP 工具长时间运行、误调用或用户要求停止时。",
		ShortDescription: "取消后台工具执行",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{"type": "string", "description": "工具执行 ID"},
				"reason":       map[string]interface{}{"type": "string", "description": "取消原因，可选，会写入终止说明"},
			},
			"required": []string{"execution_id"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
		id := stringArg(args, "execution_id")
		if id == "" {
			return textToolResult("execution_id 必填", true), nil
		}
		reason := stringArg(args, "reason")
		if server.CancelToolExecutionWithNote(id, reason) {
			return textToolResult("已请求取消内部工具 execution: "+id, false), nil
		}
		if external != nil && external.CancelToolExecutionWithNote(id, reason) {
			return textToolResult("已请求取消外部 MCP execution: "+id, false), nil
		}
		return textToolResult("未找到进行中的 execution，或该 execution 已结束: "+id, true), nil
	})
}

func waitToolExecutionSnapshot(ctx context.Context, server *Server, external *ExternalMCPManager, id string, wait time.Duration) (*ExecutionSnapshot, error) {
	if server != nil && server.executionService != nil && server.executionService.getEntry(id) != nil {
		return server.executionService.Wait(ctx, id, wait)
	}
	if external != nil && external.executionService != nil && external.executionService.getEntry(id) != nil {
		return external.executionService.Wait(ctx, id, wait)
	}
	if server != nil && server.executionService != nil {
		if snap, err := server.executionService.Get(id); err == nil {
			return snap, nil
		}
	}
	if external != nil && external.executionService != nil {
		return external.executionService.Get(id)
	}
	exec := lookupToolExecution(server, external, id)
	if exec == nil {
		return nil, fmt.Errorf("execution not found: %s", id)
	}
	return &ExecutionSnapshot{Execution: exec}, nil
}

func lookupToolExecution(server *Server, external *ExternalMCPManager, id string) *ToolExecution {
	if server != nil {
		if exec, ok := server.GetExecution(id); ok && exec != nil {
			return exec
		}
	}
	if external != nil {
		if exec, ok := external.GetExecution(id); ok && exec != nil {
			return exec
		}
	}
	return nil
}

type executionFormatOptions struct {
	includePartialOutput bool
	partialMaxBytes      int
}

func executionFormatOptionsFromArgs(args map[string]interface{}) executionFormatOptions {
	includePartial := true
	if raw, ok := args["include_partial_output"]; ok {
		if b, ok := raw.(bool); ok {
			includePartial = b
		} else if s := strings.TrimSpace(fmt.Sprint(raw)); s != "" {
			includePartial = strings.EqualFold(s, "true") || s == "1" || strings.EqualFold(s, "yes")
		}
	}
	maxBytes := intArg(args, "partial_output_max_bytes", defaultPartialPreviewBytes, maxPartialPreviewBytes)
	return executionFormatOptions{includePartialOutput: includePartial, partialMaxBytes: maxBytes}
}

func formatExecutionForModel(exec *ToolExecution, opts executionFormatOptions) string {
	if exec == nil {
		return "execution: null"
	}
	payload := map[string]interface{}{
		"execution_id": exec.ID,
		"tool":         exec.ToolName,
		"status":       exec.Status,
		"started_at":   exec.StartTime.Format(time.RFC3339),
	}
	if exec.EndTime != nil {
		payload["ended_at"] = exec.EndTime.Format(time.RFC3339)
	}
	if exec.Duration > 0 {
		payload["duration"] = exec.Duration.String()
	}
	if exec.Error != "" {
		payload["error"] = exec.Error
	}
	if exec.Result != nil {
		payload["result"] = ToolResultPlainText(exec.Result)
		payload["is_error"] = exec.Result.IsError
		if exec.Result.Blocked {
			payload["blocked"] = true
		}
	}
	if opts.includePartialOutput && exec.PartialOutput != "" {
		partial := tailStringBytes(exec.PartialOutput, opts.partialMaxBytes)
		payload["partial_output"] = partial
		payload["partial_output_bytes"] = exec.PartialOutputBytes
		payload["partial_output_truncated"] = exec.PartialOutputTruncated || len([]byte(partial)) < len([]byte(exec.PartialOutput))
		if exec.PartialOutputUpdatedAt != nil {
			payload["partial_output_updated_at"] = exec.PartialOutputUpdatedAt.Format(time.RFC3339)
		}
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("execution_id: %s\nstatus: %s\nerror: %s", exec.ID, exec.Status, exec.Error)
	}
	return string(b)
}

func tailStringBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultPartialPreviewBytes
	}
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	return string(b[len(b)-maxBytes:])
}

func textToolResult(text string, isErr bool) *ToolResult {
	return &ToolResult{Content: []Content{{Type: "text", Text: text}}, IsError: isErr}
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func durationSecondsArg(args map[string]interface{}, key string, def, max time.Duration) time.Duration {
	if args == nil {
		return def
	}
	var seconds float64
	switch v := args[key].(type) {
	case int:
		seconds = float64(v)
	case int64:
		seconds = float64(v)
	case float64:
		seconds = v
	case json.Number:
		f, _ := v.Float64()
		seconds = f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		seconds = f
	}
	if seconds <= 0 {
		return def
	}
	d := time.Duration(seconds * float64(time.Second))
	if max > 0 && d > max {
		return max
	}
	return d
}

func intArg(args map[string]interface{}, key string, def, max int) int {
	if args == nil {
		return def
	}
	var n int
	switch v := args[key].(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	case json.Number:
		i, _ := v.Int64()
		n = int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(v))
		n = i
	}
	if n <= 0 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}
