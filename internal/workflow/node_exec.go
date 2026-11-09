package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"

	"github.com/google/uuid"
)

func executeNode(ctx context.Context, args RunArgs, runID string, node graphNode, state *WorkflowLocalState) (map[string]any, bool, error) {
	label := node.Label
	if strings.TrimSpace(label) == "" {
		label = node.ID
	}
	nodeRunID := uuid.NewString()
	startedAt := time.Now()
	incomingCount := 0
	if rt := workflowRuntimeFrom(ctx); rt != nil && rt.idx != nil {
		incomingCount = len(rt.idx.incoming[node.ID])
	}
	input := map[string]any{
		"nodeId":   node.ID,
		"nodeType": node.Type,
		"label":    label,
		"inputs":   state.Inputs,
		"previous": state.LastOutput,
		"join": map[string]any{
			"strategy": joinStrategy(node),
			"incoming": incomingCount,
		},
	}
	inputJSON, _ := json.Marshal(input)
	if err := args.DB.CreateWorkflowNodeRun(&database.WorkflowNodeRun{
		ID:        nodeRunID,
		RunID:     runID,
		NodeID:    node.ID,
		Status:    "running",
		InputJSON: string(inputJSON),
		StartedAt: startedAt,
	}); err != nil {
		return nil, false, err
	}
	if args.Progress != nil {
		args.Progress("workflow_node_start", fmt.Sprintf("Starting node: %s", label), map[string]any{
			"workflowRunId": runID,
			"nodeRunId":     nodeRunID,
			"nodeId":        node.ID,
			"nodeType":      node.Type,
			"label":         label,
		})
	}

	result, proceed, status, errText := runBuiltinNode(ctx, args, node, state)
	duration := time.Since(startedAt)
	if result == nil {
		result = map[string]any{}
	}
	result["duration_ms"] = duration.Milliseconds()
	result["finished_at"] = time.Now().Format(time.RFC3339Nano)
	result["status"] = status
	accumulateWorkflowMetric(state, "node_count", 1)
	accumulateWorkflowMetric(state, "duration_ms", duration.Milliseconds())
	if strings.EqualFold(node.Type, "tool") {
		accumulateWorkflowMetric(state, "tool_call_count", 1)
	}
	outputJSON, _ := json.Marshal(result)
	if err := args.DB.FinishWorkflowNodeRun(nodeRunID, status, string(outputJSON), errText); err != nil {
		return nil, false, err
	}
	if status == "skipped" {
		state.Skipped = append(state.Skipped, label)
	} else {
		state.Executed = append(state.Executed, label)
	}
	if args.Progress != nil {
		progressData := map[string]any{
			"workflowRunId": runID,
			"nodeRunId":     nodeRunID,
			"nodeId":        node.ID,
			"nodeType":      node.Type,
			"label":         label,
			"status":        status,
			"durationMs":    duration.Milliseconds(),
			"output":        result,
		}
		progressMsg := fmt.Sprintf("Node completed: %s (%s)", label, status)
		if strings.EqualFold(node.Type, "condition") {
			matched := false
			if v, ok := result["matched"].(bool); ok {
				matched = v
			}
			expr := cfgString(node.Config, "expression")
			if matched {
				progressMsg = fmt.Sprintf("Condition evaluated: %s → yes", label)
			} else {
				progressMsg = fmt.Sprintf("Condition evaluated: %s → no", label)
			}
			progressData["expression"] = expr
			progressData["matched"] = matched
		}
		args.Progress("workflow_node_result", progressMsg, progressData)
	}
	state.NodeProceed[node.ID] = proceed
	return result, proceed, nil
}

func emitConditionBranchProgress(args RunArgs, runID string, node graphNode, edges []graphEdge, nodes map[string]graphNode, state *WorkflowLocalState) {
	if args.Progress == nil || len(edges) == 0 {
		return
	}
	for edgeIdx, edge := range edges {
		allowed := edgeAllowed(edge, node, edgeIdx, state)
		target := nodes[edge.Target]
		targetLabel := strings.TrimSpace(target.Label)
		if targetLabel == "" {
			targetLabel = edge.Target
		}
		branchLabel := strings.TrimSpace(edge.Label)
		if branchLabel == "" {
			// Wire marker: legacy Chinese 是/否 fallback labels stay byte-identical
			// for historical graphs; conditionBranchHint dual-accepts them (yes/no/y/n).
			switch edgeIdx {
			case 0:
				branchLabel = "是"
			case 1:
				branchLabel = "否"
			default:
				branchLabel = fmt.Sprintf("branch %d", edgeIdx+1)
			}
		}
		cond := firstNonEmpty(cfgString(edge.Config, "condition"), cfgString(edge.Config, "expression"))
		eventType := "workflow_branch_skipped"
		msg := fmt.Sprintf("skipped branch %q → %s", branchLabel, targetLabel)
		if allowed {
			eventType = "workflow_branch_taken"
			msg = fmt.Sprintf("executing branch %q → %s", branchLabel, targetLabel)
		}
		args.Progress(eventType, msg, map[string]any{
			"workflowRunId": runID,
			"nodeId":        node.ID,
			"nodeType":      node.Type,
			"label":         node.Label,
			"branchLabel":   branchLabel,
			"targetId":      edge.Target,
			"targetLabel":   targetLabel,
			"edgeCondition": cond,
			"matched":       conditionMatched(state),
		})
	}
}
