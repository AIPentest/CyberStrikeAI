package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
)

func executeEinoGraph(ctx context.Context, args RunArgs, runID string, workflowID string, version int, g *graphDef, state *WorkflowLocalState) error {
	_, err := invokeEinoGraph(ctx, args, runID, workflowID, version, g, state, false)
	return err
}

func invokeEinoGraph(ctx context.Context, args RunArgs, runID string, workflowID string, version int, g *graphDef, state *WorkflowLocalState, resume bool) (bool, error) {
	wfInput := workflowInputFromMap(state.Inputs)
	if resume {
		wfInput = WorkflowInput{}
	}
	rt := &workflowRuntime{
		args:  args,
		runID: runID,
		idx:   indexGraph(g),
		state: state,
	}

	art, err := defaultEngine.getOrCompile(ctx, workflowID, version, g)
	if err != nil {
		return false, fmt.Errorf("compile Eino Workflow failed: %w", err)
	}
	rt.idx = art.idx

	runCtx := withWorkflowRuntime(ctx, rt)
	runCtx = attachWorkflowCallbacks(runCtx, args.AppCfg, args, workflowID)

	invokeOpts := []compose.Option{compose.WithCheckPointID(runID)}
	for {
		_, err = art.runnable.Invoke(runCtx, wfInput, invokeOpts...)
		if err == nil {
			return false, nil
		}
		if hitlErr := extractAwaitingHITL(err, art, runID, args, state); hitlErr != nil {
			return true, hitlErr
		}
		return false, err
	}
}

func extractAwaitingHITL(err error, art *compiledArtifact, runID string, args RunArgs, state *WorkflowLocalState) error {
	info, ok := compose.ExtractInterruptInfo(err)
	if !ok || len(art.hitlIDs) == 0 {
		return nil
	}
	nodeID := nextHITLNodeID(info, art.hitlIDs)
	node := art.idx.nodes[nodeID]
	if nodeID == "" {
		return nil
	}
	prompt := resolveHITLPromptBinding(node.Config, state)
	label := firstNonEmpty(node.Label, nodeID)
	if args.DB != nil {
		pending := map[string]any{
			"nodeId":        nodeID,
			"label":         label,
			"prompt":        prompt,
			"reviewer":      cfgString(node.Config, "reviewer"),
			"checkpointId":  runID,
			"interrupt":     workflowInterruptMetadata(info),
			"resumePayload": map[string]any{"approved": "bool", "comment": "string"},
		}
		pendingJSON, _ := json.Marshal(pending)
		_ = args.DB.SetWorkflowRunAwaitingHITL(runID, nodeID, string(pendingJSON))
	}
	if args.Progress != nil {
		args.Progress("workflow_hitl_waiting", fmt.Sprintf("waiting for human approval: %s", label), map[string]any{
			"workflowRunId": runID,
			"nodeId":        nodeID,
			"label":         label,
			"prompt":        prompt,
			"reviewer":      cfgString(node.Config, "reviewer"),
			"mode":          "interactive",
			"resumeApi":     fmt.Sprintf("/api/workflows/runs/%s/resume", runID),
		})
	}
	return &AwaitingHITLError{
		RunID:     runID,
		NodeID:    nodeID,
		NodeLabel: label,
		Prompt:    prompt,
		Reviewer:  cfgString(node.Config, "reviewer"),
	}
}

func workflowInterruptMetadata(info *compose.InterruptInfo) map[string]any {
	if info == nil {
		return map[string]any{}
	}
	before := append([]string(nil), info.BeforeNodes...)
	return map[string]any{
		"beforeNodes":  before,
		"resumeTarget": firstString(before),
		"address": map[string]any{
			"kind":        "compose_interrupt",
			"beforeNodes": before,
			"path":        strings.Join(before, "/"),
		},
		"raw": fmt.Sprintf("%+v", info),
	}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func nextHITLNodeID(info *compose.InterruptInfo, hitlIDs []string) string {
	if info != nil && len(info.BeforeNodes) > 0 {
		for _, id := range info.BeforeNodes {
			for _, hitl := range hitlIDs {
				if id == hitl {
					return id
				}
			}
		}
		return info.BeforeNodes[0]
	}
	if len(hitlIDs) == 0 {
		return ""
	}
	return hitlIDs[0]
}

// ResumeWorkflowRun continues a run paused at HITL after human decision.
func ResumeWorkflowRun(ctx context.Context, args RunArgs, runID string, approved bool, comment string) (*RunResult, error) {
	run, err := args.DB.GetWorkflowRun(runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("workflow run not found")
	}
	if run.Status != "awaiting_hitl" {
		return nil, fmt.Errorf("workflow run is not awaiting approval: %s", run.Status)
	}
	wf, err := args.DB.GetWorkflowDefinition(run.WorkflowID)
	if err != nil || wf == nil {
		return nil, fmt.Errorf("workflow definition not found")
	}
	graph, err := parseGraph(wf.GraphJSON)
	if err != nil {
		return nil, err
	}

	var input map[string]interface{}
	_ = json.Unmarshal([]byte(run.InputJSON), &input)
	state := newWorkflowLocalState(input, runID)
	if state.Inputs == nil {
		state.Inputs = map[string]any{}
	}
	state.Inputs["_hitl_approved"] = approved
	state.Inputs["_hitl_comment"] = strings.TrimSpace(comment)
	state.Inputs["_hitl_node_id"] = run.PendingHITLNodeID

	if !approved {
		errText := strings.TrimSpace(comment)
		if errText == "" {
			errText = "human approval rejected"
		}
		_ = args.DB.FinishWorkflowRun(runID, "rejected", "", errText)
		if args.Progress != nil {
			args.Progress("workflow_hitl_rejected", fmt.Sprintf("Workflow rejected at approval node %q.", run.PendingHITLNodeID), map[string]interface{}{
				"workflowRunId": runID,
				"nodeId":        run.PendingHITLNodeID,
				"comment":       errText,
			})
		}
		return &RunResult{
			RunID:    runID,
			Response: fmt.Sprintf("Workflow rejected at approval node %q.", run.PendingHITLNodeID),
			Status:   "rejected",
		}, nil
	}

	if args.Progress != nil {
		args.Progress("workflow_hitl_resumed", "human approval passed, resuming execution", map[string]interface{}{
			"workflowRunId": runID,
			"nodeId":        run.PendingHITLNodeID,
			"comment":       strings.TrimSpace(comment),
		})
	}

	_ = args.DB.SetWorkflowRunStatus(runID, "running")
	resumeArgs := args
	if strings.TrimSpace(resumeArgs.ConversationID) == "" {
		resumeArgs.ConversationID = run.ConversationID
	}

	awaiting, err := invokeEinoGraph(ctx, resumeArgs, runID, wf.ID, run.WorkflowVersion, graph, state, true)
	if err != nil {
		if IsAwaitingHITL(err) {
			return &RunResult{
				RunID:        runID,
				Status:       "awaiting_hitl",
				Response:     fmt.Sprintf("Workflow is waiting for the next human confirmation at node %q.", err.(*AwaitingHITLError).NodeID),
				AwaitingHITL: true,
			}, nil
		}
		_ = args.DB.FinishWorkflowRun(runID, "failed", "", err.Error())
		return nil, err
	}
	_ = awaiting

	output := map[string]interface{}{
		"workflowId":      wf.ID,
		"workflowName":    wf.Name,
		"workflowVersion": wf.Version,
		"workflowRunId":   runID,
		"status":          "completed",
		"outputs":         state.Outputs,
		"metrics":         state.Metrics,
		"executedNodes":   state.Executed,
		"skippedNodes":    state.Skipped,
		"engine":          "eino_workflow",
	}
	outputJSON, _ := json.Marshal(output)
	response := renderWorkflowResponse(args.Role.Name, wf.Name, wf.Version, runID, state)
	_ = args.DB.FinishWorkflowRun(runID, "completed", string(outputJSON), "")
	if args.Progress != nil {
		args.Progress("workflow_done", fmt.Sprintf("Workflow %q run completed", wf.Name), map[string]interface{}{
			"workflowRunId": runID,
			"workflowId":    wf.ID,
			"outputs":       state.Outputs,
			"metrics":       state.Metrics,
			"response":      response,
			"engine":        "eino_workflow",
		})
	}
	return &RunResult{Response: response, RunID: runID, Status: "completed"}, nil
}
