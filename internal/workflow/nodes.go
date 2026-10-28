package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/multiagent"
)

func runBuiltinNode(ctx context.Context, args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	cfg := node.Config
	switch strings.ToLower(strings.TrimSpace(node.Type)) {
	case "start":
		return startOutputMap(node, state.Inputs["message"], state.Inputs["conversationId"], state.Inputs["projectId"]), true, "completed", ""
	case "condition":
		expr := cfgString(cfg, "expression")
		ok := evalCondition(expr, state)
		return conditionOutputMap(node, expr, ok), true, "completed", ""
	case "output":
		key := cfgString(cfg, "output_key")
		if key == "" {
			key = "result"
		}
		var value any
		if v := cfgString(cfg, "static_value"); v != "" {
			value = v
		} else {
			value = resolveOutputSourceBinding(cfg, state)
		}
		state.Outputs[key] = value
		return outputNodeOutputMap(node, key, value), true, "completed", ""
	case "end":
		value := resolveOutputSourceBinding(cfg, state)
		if b, ok := parseFieldBinding(cfg, "result_binding"); ok {
			value = resolveBinding(b, state)
		}
		return endOutputMap(node, value), false, "completed", ""
	case "tool":
		return runToolNode(ctx, args, node, state)
	case "agent":
		return runAgentNode(ctx, args, node, state)
	case "hitl":
		return runHITLNode(args, node, state)
	default:
		reason := "unknown node type"
		out := outputMap(envelope("unknown", node.ID, node.Type, "skipped", ""), map[string]any{"skipped": true, "reason": reason})
		return out, true, "skipped", reason
	}
}

func runToolNode(ctx context.Context, args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	toolName := cfgString(node.Config, "tool_name")
	if toolName == "" {
		errText := "tool node has no MCP tool selected"
		return outputMap(envelope("tool", node.ID, node.Type, "failed", ""), map[string]any{"error": errText}), false, "failed", errText
	}
	if args.Agent == nil {
		errText := "tool node execution failed: Agent is nil"
		return outputMap(envelope("tool", node.ID, node.Type, "failed", ""), map[string]any{"tool_name": toolName, "error": errText}), false, "failed", errText
	}
	toolArgs, err := resolveToolArguments(node.Config, state)
	if err != nil {
		errText := fmt.Sprintf("tool arguments are not valid JSON: %v", err)
		return outputMap(envelope("tool", node.ID, node.Type, "failed", ""), map[string]any{"tool_name": toolName, "error": errText}), false, "failed", errText
	}
	if args.Progress != nil {
		args.Progress("workflow_tool_start", fmt.Sprintf("calling tool: %s", toolName), map[string]any{
			"nodeId": node.ID,
			"tool":   toolName,
			"args":   toolArgs,
		})
	}
	toolCtx, cancel, err := workflowToolContext(ctx, cfgString(node.Config, "timeout_seconds"))
	if err != nil {
		errText := err.Error()
		return outputMap(envelope("tool", node.ID, node.Type, "failed", ""), map[string]any{"tool_name": toolName, "error": errText}), false, "failed", errText
	}
	defer cancel()
	result, err := args.Agent.ExecuteMCPToolForConversation(toolCtx, args.ConversationID, toolName, toolArgs)
	if err == nil && toolCtx.Err() != nil {
		err = toolCtx.Err()
	}
	// The MCP worker and caller have timers for the same absolute deadline.
	// The worker may finish first while the caller's Done notification is still
	// waiting to run; the deadline itself is authoritative in either ordering.
	if err == nil {
		if deadline, ok := toolCtx.Deadline(); ok && !time.Now().Before(deadline) {
			err = context.DeadlineExceeded
		}
	}
	if err != nil {
		errText := err.Error()
		return outputMap(envelope("tool", node.ID, node.Type, "failed", ""), map[string]any{"tool_name": toolName, "arguments": toolArgs, "error": errText}), false, "failed", errText
	}
	output := ""
	executionID := ""
	isError := false
	if result != nil {
		output = result.Result
		executionID = result.ExecutionID
		isError = result.IsError
	}
	maxToolOutputBytes := config.MultiAgentEinoMiddlewareConfig{}.ReductionMaxLengthForTruncEffective()
	if args.AppCfg != nil {
		maxToolOutputBytes = args.AppCfg.MultiAgent.EinoMiddleware.ReductionMaxLengthForTruncEffective()
	}
	output = truncateWorkflowToolOutput(output, maxToolOutputBytes, executionID)
	out := toolOutputMap(node, output, toolName, toolArgs, executionID, isError)
	if key := cfgString(node.Config, "output_key"); key != "" {
		state.Outputs[key] = output
	}
	if isError {
		errText := strings.TrimSpace(output)
		if errText == "" {
			errText = "tool returned an error"
		}
		return out, false, "failed", errText
	}
	return out, true, "completed", ""
}

func truncateWorkflowToolOutput(output string, maxBytes int, executionID string) string {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output
	}
	marker := fmt.Sprintf("\n\n...[workflow tool output truncated; full result is stored in execution %s]...\n\n", strings.TrimSpace(executionID))
	if strings.TrimSpace(executionID) == "" {
		marker = "\n\n...[workflow tool output truncated; full result remains in the tool execution record]...\n\n"
	}
	budget := maxBytes - len(marker)
	if budget <= 0 {
		return marker
	}
	head := budget / 2
	tail := budget - head
	for head > 0 && !utf8.RuneStart(output[head]) {
		head--
	}
	tailStart := len(output) - tail
	for tailStart < len(output) && !utf8.RuneStart(output[tailStart]) {
		tailStart++
	}
	return output[:head] + marker + output[tailStart:]
}

func workflowToolContext(parent context.Context, raw string) (context.Context, context.CancelFunc, error) {
	if strings.TrimSpace(raw) == "" {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	seconds, err := parsePositiveInt(raw)
	if err != nil || int64(seconds) > int64((1<<63-1)/int64(time.Second)) {
		return nil, nil, fmt.Errorf("tool node timeout must be a valid positive integer of seconds")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(seconds)*time.Second)
	deadline, _ := ctx.Deadline()
	ctx = mcp.WithToolExecutionDeadline(ctx, deadline)
	return ctx, cancel, nil
}

func runAgentNode(ctx context.Context, args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	if args.AppCfg == nil || args.Agent == nil {
		errText := "Agent node execution failed: app config or Agent is nil"
		return outputMap(envelope("agent", node.ID, node.Type, "failed", ""), map[string]any{"error": errText}), false, "failed", errText
	}
	mode := strings.ToLower(cfgString(node.Config, "agent_mode"))
	if mode == "" {
		mode = "eino_single"
	}
	inputSource := resolveNodeInputBinding(node.Config, state)
	message := buildAgentNodeMessage(node, state, inputSource)
	var result *multiagent.RunResult
	var err error
	state.SegmentMaxIteration = 0
	agentProgress := workflowAgentProgress(args.Progress, state, node)
	switch mode {
	case "eino_single", "single", "chat":
		result, err = multiagent.RunEinoSingleChatModelAgent(
			ctx,
			args.AppCfg,
			&args.AppCfg.MultiAgent,
			args.Agent,
			args.DB,
			args.Logger,
			args.ConversationID,
			args.ProjectID,
			message,
			args.History,
			args.RoleTools,
			agentProgress,
			nil,
			args.SystemPromptExtra,
		)
	default:
		result, err = multiagent.RunDeepAgent(
			ctx,
			args.AppCfg,
			&args.AppCfg.MultiAgent,
			args.Agent,
			args.DB,
			args.Logger,
			args.ConversationID,
			args.ProjectID,
			message,
			args.History,
			args.RoleTools,
			agentProgress,
			args.AgentsMarkdownDir,
			mode,
			nil,
			args.SystemPromptExtra,
		)
	}
	if err != nil {
		errText := err.Error()
		state.MainIterationOffset += state.SegmentMaxIteration
		return outputMap(envelope("agent", node.ID, node.Type, "failed", ""), map[string]any{"mode": mode, "error": errText}), false, "failed", errText
	}
	state.MainIterationOffset += state.SegmentMaxIteration
	response := ""
	mcpIDs := []string{}
	if result != nil {
		response = result.Response
		mcpIDs = result.MCPExecutionIDs
	}
	if args.Progress != nil {
		args.Progress("workflow_agent_output", response, map[string]any{
			"nodeId":          node.ID,
			"label":           firstNonEmpty(node.Label, node.ID),
			"mode":            mode,
			"inputSource":     inputSource,
			"inputPreview":    truncateWorkflowPreview(inputSource, 500),
			"mcpExecutionIds": mcpIDs,
		})
	}
	if key := cfgString(node.Config, "output_key"); key != "" {
		state.Outputs[key] = response
	}
	return agentOutputMap(node, response, mode, mcpIDs), true, "completed", ""
}

func buildAgentNodeMessage(node graphNode, state *WorkflowLocalState, upstreamInput string) string {
	instruction := strings.TrimSpace(cfgString(node.Config, "instruction"))
	upstreamInput = strings.TrimSpace(upstreamInput)
	if instruction == "" {
		if upstreamInput != "" {
			return fmt.Sprintf("Continue processing based on upstream node output:\n%s", upstreamInput)
		}
		return fmt.Sprintf("Continue processing based on upstream node output:\n%v", state.LastOutput["output"])
	}
	if upstreamInput == "" {
		return instruction
	}
	return strings.TrimSpace(fmt.Sprintf("Downstream input:\n%s\n\nNode instruction:\n%s", upstreamInput, instruction))
}

func workflowAgentProgress(progress agent.ProgressCallback, state *WorkflowLocalState, node graphNode) agent.ProgressCallback {
	if progress == nil {
		return nil
	}
	return func(eventType, message string, data interface{}) {
		switch eventType {
		case "response_start", "response_delta", "response", "done":
			return
		default:
			enrichWorkflowAgentEventData(data, state, node)
			collectAgentMetrics(state, data)
			if eventType == "iteration" {
				applyWorkflowMainIterationOffset(data, state)
			}
			progress(eventType, message, data)
		}
	}
}

func enrichWorkflowAgentEventData(data interface{}, state *WorkflowLocalState, node graphNode) {
	m, ok := data.(map[string]interface{})
	if !ok || m == nil {
		return
	}
	if node.ID != "" {
		m["workflowNodeId"] = node.ID
	}
	if state != nil && strings.TrimSpace(state.WorkflowRunID) != "" {
		m["workflowRunId"] = state.WorkflowRunID
	}
}

func applyWorkflowMainIterationOffset(data interface{}, state *WorkflowLocalState) {
	if state == nil {
		return
	}
	m, ok := data.(map[string]interface{})
	if !ok || m == nil {
		return
	}
	scope, _ := m["einoScope"].(string)
	if strings.TrimSpace(scope) != "main" {
		return
	}
	raw := iterationNumberFromProgressData(m)
	if raw <= 0 {
		return
	}
	if raw > state.SegmentMaxIteration {
		state.SegmentMaxIteration = raw
	}
	m["iteration"] = raw + state.MainIterationOffset
}

func iterationNumberFromProgressData(m map[string]interface{}) int {
	switch v := m["iteration"].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func runHITLNode(args RunArgs, node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	prompt := resolveHITLPromptBinding(node.Config, state)
	reviewer := cfgString(node.Config, "reviewer")
	if reviewer == "" {
		reviewer = "human"
	}
	approved := true
	if state != nil && state.Inputs != nil {
		if v, ok := state.Inputs["_hitl_approved"]; ok {
			approved = fmt.Sprint(v) == "true"
		}
	}
	if !approved {
		reason := "human approval rejected"
		if state != nil && state.Inputs != nil {
			if v, ok := state.Inputs["_hitl_comment"]; ok {
				if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
					reason = s
				}
			}
		}
		return hitlOutputMap(node, "failed", "", prompt, reviewer, false), false, "failed", reason
	}
	if args.Progress != nil {
		args.Progress("workflow_hitl_checkpoint", "human approval checkpoint passed", map[string]any{
			"nodeId":   node.ID,
			"prompt":   prompt,
			"reviewer": reviewer,
			"mode":     "interactive",
			"approved": true,
		})
	}
	return hitlOutputMap(node, "completed", prompt, prompt, reviewer, true), true, "completed", ""
}
