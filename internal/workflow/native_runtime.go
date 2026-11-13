package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// Native checkpoints store explicit execution state, independent of an Agent framework.
type nativeCheckpoint struct {
	Version   int                 `json:"version"`
	GraphHash string              `json:"graphHash"`
	State     *WorkflowLocalState `json:"state"`
	Done      map[string]bool     `json:"done"`
	Active    map[string]bool     `json:"active"`
	InFlight  string              `json:"inFlight,omitempty"`
}

func executeGraph(ctx context.Context, args RunArgs, runID, workflowID string, version int, g *graphDef, state *WorkflowLocalState) error {
	_, err := invokeGraph(ctx, args, runID, workflowID, version, g, state, false)
	return err
}

// The compatibility name keeps callers stable while the implementation uses the native DAG scheduler.
func invokeGraph(ctx context.Context, args RunArgs, runID, workflowID string, version int, g *graphDef, state *WorkflowLocalState, resume bool) (bool, error) {
	art, err := defaultEngine.getOrCompile(ctx, workflowID, version, g)
	if err != nil {
		return false, err
	}
	store, err := defaultEngine.checkpointStore()
	if err != nil {
		return false, err
	}
	graphJSON, _ := json.Marshal(g)
	hash := fmt.Sprintf("%x", sha256.Sum256(graphJSON))
	cp := nativeCheckpoint{Version: 1, GraphHash: hash, State: state, Done: map[string]bool{}, Active: map[string]bool{}}
	for _, id := range findStartNodeIDs(art.idx) {
		cp.Active[id] = true
	}
	if resume {
		body, found, err := store.Get(ctx, runID)
		if err != nil {
			return false, err
		}
		if !found {
			return false, fmt.Errorf("workflow checkpoint is missing")
		}
		if err = json.Unmarshal(body, &cp); err != nil || cp.Version != 1 || cp.State == nil {
			return false, fmt.Errorf("legacy workflow checkpoint cannot be resumed by this Pi-only version; export prior results and start a new run")
		}
		if cp.GraphHash != hash {
			return false, fmt.Errorf("workflow changed since the checkpoint; refusing to replay a different graph")
		}
		if cp.InFlight != "" {
			return false, fmt.Errorf("node %s was interrupted during execution; reconcile its execution receipt before resuming", cp.InFlight)
		}
		approvals := map[string]any{}
		for _, key := range []string{"_hitl_approved", "_hitl_comment", "_hitl_node_id"} {
			if v, ok := state.Inputs[key]; ok {
				approvals[key] = v
			}
		}
		*state = *cp.State
		for key, v := range approvals {
			state.Inputs[key] = v
		}
		cp.State = state
	}
	persist := func() error {
		b, err := json.Marshal(cp)
		if err != nil {
			return err
		}
		return store.Set(ctx, runID, b)
	}
	rt := &workflowRuntime{args: args, runID: runID, idx: art.idx, state: state}
	runCtx := withWorkflowRuntime(ctx, rt)
	for _, id := range art.order {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if cp.Done[id] || !cp.Active[id] {
			continue
		}
		node := art.idx.nodes[id]
		if strings.EqualFold(node.Type, "hitl") {
			approved, _ := state.Inputs["_hitl_approved"].(bool)
			approvedID, _ := state.Inputs["_hitl_node_id"].(string)
			if !approved || approvedID != id {
				if err := persist(); err != nil {
					return false, err
				}
				prompt := resolveHITLPromptBinding(node.Config, state)
				label := firstNonEmpty(node.Label, id)
				payload, _ := json.Marshal(map[string]any{"nodeId": id, "label": label, "prompt": prompt, "checkpointId": runID, "checkpointVersion": 1})
				if args.DB != nil {
					if err := args.DB.SetWorkflowRunAwaitingHITL(runID, id, string(payload)); err != nil {
						return false, err
					}
				}
				if args.Progress != nil {
					args.Progress("workflow_hitl_waiting", "等待人工确认："+label, map[string]any{"workflowRunId": runID, "nodeId": id, "label": label, "prompt": prompt, "mode": "interactive", "resumeApi": fmt.Sprintf("/api/workflows/runs/%s/resume", runID)})
				}
				return true, &AwaitingHITLError{RunID: runID, NodeID: id, NodeLabel: label, Prompt: prompt}
			}
		}
		// Record intent before a side effect. A crash never silently repeats an uncertain operation.
		cp.InFlight = id
		if err := persist(); err != nil {
			return false, err
		}
		if len(art.idx.incoming[id]) == 1 {
			state.LastOutput = state.NodeOutputs[art.idx.incoming[id][0].Source]
		}
		if _, err := runWorkflowNodeLambda(runCtx, node); err != nil {
			return false, err
		}
		cp.InFlight = ""
		cp.Done[id] = true
		edges := art.idx.outgoing[id]
		if strings.EqualFold(node.Type, "condition") {
			emitConditionBranchProgress(args, runID, node, edges, art.idx.nodes, state)
		}
		conditional := strings.EqualFold(node.Type, "condition") || hasConditionalOutgoingEdges(art.idx, id)
		for i, edge := range edges {
			if edgeAllowed(edge, node, i, state) {
				cp.Active[edge.Target] = true
				if conditional {
					break
				}
			}
		}
		if err := persist(); err != nil {
			return false, err
		}
	}
	return false, nil
}

func hasConditionalOutgoingEdges(idx *graphIndex, nodeID string) bool {
	for _, e := range idx.outgoing[nodeID] {
		if firstNonEmpty(cfgString(e.Config, "condition"), cfgString(e.Config, "expression")) != "" {
			return true
		}
	}
	return false
}
