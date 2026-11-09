package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type DryRunResult struct {
	Outputs      map[string]any            `json:"outputs"`
	NodeOutputs  map[string]map[string]any `json:"nodeOutputs"`
	Executed     []string                  `json:"executed"`
	Skipped      []string                  `json:"skipped"`
	Trace        []map[string]any          `json:"trace"`
	Metrics      map[string]any            `json:"metrics"`
	ReplayScript []map[string]any          `json:"replayScript"`
}

func DryRunGraphJSON(ctx context.Context, graphJSON string, inputs map[string]any) (*DryRunResult, error) {
	g, err := parseGraph(graphJSON)
	if err != nil {
		return nil, err
	}
	idx := indexGraph(g)
	if err := validateGraphDefinition(g, idx); err != nil {
		return nil, err
	}
	in := make(map[string]interface{}, len(inputs))
	for k, v := range inputs {
		in[k] = v
	}
	if _, ok := in["message"]; !ok {
		in["message"] = ""
	}
	state := newWorkflowLocalState(in, "dry-run")
	rt := &workflowRuntime{runID: "dry-run", idx: idx, state: state}
	trace := []map[string]any{}
	executedIDs := map[string]bool{}
	queue := findStartNodeIDs(idx)
	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		nodeID := queue[0]
		queue = queue[1:]
		if executedIDs[nodeID] {
			continue
		}
		node := idx.nodes[nodeID]
		if !dryRunPredecessorsReady(idx, nodeID, executedIDs) {
			queue = append(queue, nodeID)
			continue
		}
		if err := prepareNodeInputState(rt, node); err != nil {
			return nil, err
		}
		started := time.Now()
		out, proceed, status, errText := dryRunNode(node, state)
		out["duration_ms"] = time.Since(started).Milliseconds()
		out["status"] = status
		state.NodeOutputs[node.ID] = out
		state.LastOutput = out
		executedIDs[nodeID] = true
		if status == "skipped" {
			state.Skipped = append(state.Skipped, firstNonEmpty(node.Label, node.ID))
		} else {
			state.Executed = append(state.Executed, firstNonEmpty(node.Label, node.ID))
		}
		trace = append(trace, map[string]any{
			"nodeId":   node.ID,
			"label":    firstNonEmpty(node.Label, node.ID),
			"type":     node.Type,
			"status":   status,
			"error":    errText,
			"output":   out,
			"previous": state.LastOutput,
		})
		if !proceed {
			continue
		}
		for edgeIdx, edge := range idx.outgoing[nodeID] {
			if edgeAllowed(edge, node, edgeIdx, state) {
				queue = append(queue, edge.Target)
			}
		}
	}
	for id, node := range idx.nodes {
		if !executedIDs[id] {
			state.Skipped = append(state.Skipped, firstNonEmpty(node.Label, id))
		}
	}
	return &DryRunResult{
		Outputs:      state.Outputs,
		NodeOutputs:  state.NodeOutputs,
		Executed:     state.Executed,
		Skipped:      state.Skipped,
		Trace:        trace,
		Metrics:      state.Metrics,
		ReplayScript: buildReplayScript(trace),
	}, nil
}

func dryRunPredecessorsReady(idx *graphIndex, nodeID string, executed map[string]bool) bool {
	for _, edge := range idx.incoming[nodeID] {
		if !executed[edge.Source] {
			return false
		}
	}
	return true
}

func dryRunNode(node graphNode, state *WorkflowLocalState) (map[string]any, bool, string, string) {
	switch strings.ToLower(strings.TrimSpace(node.Type)) {
	case "start":
		return startOutputMap(node, state.Inputs["message"], state.Inputs["conversationId"], state.Inputs["projectId"]), true, "completed", ""
	case "condition":
		expr := cfgString(node.Config, "expression")
		matched := evalCondition(expr, state)
		return conditionOutputMap(node, expr, matched), true, "completed", ""
	case "output":
		key := cfgString(node.Config, "output_key")
		value := resolveOutputSourceBinding(node.Config, state)
		if static := cfgString(node.Config, "static_value"); static != "" {
			value = static
		}
		state.Outputs[key] = value
		return outputNodeOutputMap(node, key, value), true, "completed", ""
	case "end":
		value := resolveOutputSourceBinding(node.Config, state)
		if b, ok := parseFieldBinding(node.Config, "result_binding"); ok {
			value = resolveBinding(b, state)
		}
		return endOutputMap(node, value), false, "completed", ""
	case "tool":
		args, err := resolveToolArguments(node.Config, state)
		if err != nil {
			errText := fmt.Sprintf("tool arguments are not valid JSON: %v", err)
			return outputMap(envelope("tool", node.ID, node.Type, "failed", ""), map[string]any{"error": errText}), false, "failed", errText
		}
		return toolOutputMap(node, "[dry-run] tool call skipped", cfgString(node.Config, "tool_name"), args, "dry-run", false), true, "simulated", ""
	case "agent":
		mode := firstNonEmpty(cfgString(node.Config, "agent_mode"), "eino_single")
		response := "[dry-run] agent execution skipped"
		if key := cfgString(node.Config, "output_key"); key != "" {
			state.Outputs[key] = response
		}
		return agentOutputMap(node, response, mode, nil), true, "simulated", ""
	case "hitl":
		prompt := resolveHITLPromptBinding(node.Config, state)
		return hitlOutputMap(node, "simulated", prompt, prompt, firstNonEmpty(cfgString(node.Config, "reviewer"), "human"), true), true, "simulated", ""
	default:
		return outputMap(envelope("unknown", node.ID, node.Type, "skipped", ""), map[string]any{"reason": "unknown node type"}), true, "skipped", "unknown node type"
	}
}

func buildReplayScript(trace []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(trace))
	for i, step := range trace {
		raw, _ := json.Marshal(step["output"])
		out = append(out, map[string]any{
			"step":   i + 1,
			"nodeId": step["nodeId"],
			"type":   step["type"],
			"status": step["status"],
			"output": string(raw),
		})
	}
	return out
}
