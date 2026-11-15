package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ResumeWorkflowRun continues a run paused at HITL after human decision.
func ResumeWorkflowRun(ctx context.Context, args RunArgs, runID string, approved bool, comment string) (*RunResult, error) {
	run, err := args.DB.GetWorkflowRun(runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("工作流运行不存在")
	}
	if run.Status != "awaiting_hitl" {
		return nil, fmt.Errorf("工作流运行不在等待审批状态: %s", run.Status)
	}
	wf, err := args.DB.GetWorkflowDefinition(run.WorkflowID)
	if err != nil || wf == nil {
		return nil, fmt.Errorf("工作流定义不存在")
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
			errText = "人工审批拒绝"
		}
		_ = args.DB.FinishWorkflowRun(runID, "rejected", "", errText)
		if args.Progress != nil {
			args.Progress("workflow_hitl_rejected", fmt.Sprintf("工作流已在审批节点「%s」被拒绝。", run.PendingHITLNodeID), map[string]interface{}{
				"workflowRunId": runID,
				"nodeId":        run.PendingHITLNodeID,
				"comment":       errText,
			})
		}
		return &RunResult{
			RunID:    runID,
			Response: fmt.Sprintf("工作流已在审批节点「%s」被拒绝。", run.PendingHITLNodeID),
			Status:   "rejected",
		}, nil
	}

	if args.Progress != nil {
		args.Progress("workflow_hitl_resumed", "人工审批已通过，继续执行", map[string]interface{}{
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

	awaiting, err := invokeGraph(ctx, resumeArgs, runID, wf.ID, run.WorkflowVersion, graph, state, true)
	if err != nil {
		if IsAwaitingHITL(err) {
			return &RunResult{
				RunID:        runID,
				Status:       "awaiting_hitl",
				Response:     fmt.Sprintf("工作流在节点「%s」等待下一次人工确认。", err.(*AwaitingHITLError).NodeID),
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
		"engine":          "native_workflow",
	}
	outputJSON, _ := json.Marshal(output)
	response := renderWorkflowResponse(args.Role.Name, wf.Name, wf.Version, runID, state)
	_ = args.DB.FinishWorkflowRun(runID, "completed", string(outputJSON), "")
	if args.Progress != nil {
		args.Progress("workflow_done", fmt.Sprintf("流程「%s」运行完成", wf.Name), map[string]interface{}{
			"workflowRunId": runID,
			"workflowId":    wf.ID,
			"outputs":       state.Outputs,
			"metrics":       state.Metrics,
			"response":      response,
			"engine":        "native_workflow",
		})
	}
	return &RunResult{Response: response, RunID: runID, Status: "completed"}, nil
}
