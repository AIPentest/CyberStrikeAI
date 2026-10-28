package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ShouldAutoRunRoleWorkflow returns true when a role explicitly binds a workflow
// and does not turn it off. Empty policy defaults to auto to keep role UX simple.
func ShouldAutoRunRoleWorkflow(role config.RoleConfig) bool {
	if strings.TrimSpace(role.WorkflowID) == "" {
		return false
	}
	policy := strings.ToLower(strings.TrimSpace(role.WorkflowPolicy))
	return policy == "" || policy == "auto"
}

// RunRoleBoundWorkflow executes the persisted role-bound workflow via cached Eino Workflow.
func RunRoleBoundWorkflow(ctx context.Context, args RunArgs) (*RunResult, error) {
	if args.DB == nil {
		return nil, fmt.Errorf("workflow db is nil")
	}
	workflowID := strings.TrimSpace(args.Role.WorkflowID)
	if workflowID == "" {
		return nil, fmt.Errorf("role has no workflow bound")
	}
	wf, err := args.DB.GetWorkflowDefinition(workflowID)
	if err != nil {
		return nil, err
	}
	if wf == nil {
		return nil, fmt.Errorf("workflow bound to role not found: %s", workflowID)
	}
	if !wf.Enabled {
		return nil, fmt.Errorf("workflow bound to role is disabled: %s", workflowID)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	runID := uuid.NewString()
	input := map[string]interface{}{
		"message":         args.UserMessage,
		"conversationId":  args.ConversationID,
		"projectId":       args.ProjectID,
		"role":            args.Role.Name,
		"workflowId":      wf.ID,
		"workflowVersion": wf.Version,
	}
	inputJSON, _ := json.Marshal(input)
	run := &database.WorkflowRun{
		ID:              runID,
		WorkflowID:      wf.ID,
		WorkflowVersion: wf.Version,
		ConversationID:  args.ConversationID,
		ProjectID:       args.ProjectID,
		RoleID:          args.Role.Name,
		Status:          "running",
		InputJSON:       string(inputJSON),
		StartedAt:       time.Now(),
	}
	if err := args.DB.CreateWorkflowRun(run); err != nil {
		return nil, err
	}
	if args.Progress != nil {
		args.Progress("workflow_start", fmt.Sprintf("Starting workflow %q run", wf.Name), map[string]interface{}{
			"workflowId":      wf.ID,
			"workflowName":    wf.Name,
			"workflowVersion": wf.Version,
			"workflowRunId":   runID,
			"conversationId":  args.ConversationID,
			"engine":          "eino_workflow",
		})
	}

	graph, err := parseGraph(wf.GraphJSON)
	if err != nil {
		_ = args.DB.FinishWorkflowRun(runID, "failed", "", err.Error())
		return nil, err
	}
	state := newWorkflowLocalState(input, runID)
	streaming := args.Progress != nil
	resuming := false
	for {
		_, err := invokeEinoGraph(ctx, args, runID, wf.ID, wf.Version, graph, state, resuming)
		if err == nil {
			break
		}
		if !IsAwaitingHITL(err) {
			_ = args.DB.FinishWorkflowRun(runID, "failed", "", err.Error())
			return nil, err
		}
		hitl := err.(*AwaitingHITLError)
		partial := map[string]interface{}{
			"workflowId":      wf.ID,
			"workflowName":    wf.Name,
			"workflowVersion": wf.Version,
			"workflowRunId":   runID,
			"status":          "awaiting_hitl",
			"outputs":         state.Outputs,
			"executedNodes":   state.Executed,
			"skippedNodes":    state.Skipped,
			"pendingHitl": map[string]interface{}{
				"nodeId": hitl.NodeID,
				"label":  hitl.NodeLabel,
				"prompt": hitl.Prompt,
			},
			"engine": "eino_workflow",
		}
		partialJSON, _ := json.Marshal(partial)
		_ = args.DB.SetWorkflowRunAwaitingHITL(runID, hitl.NodeID, string(partialJSON))
			response := fmt.Sprintf("Workflow %q paused at node %q awaiting human approval.\nRun ID: %s", wf.Name, firstNonEmpty(hitl.NodeLabel, hitl.NodeID), runID)
		if args.Progress != nil {
			args.Progress("workflow_paused", response, map[string]interface{}{
				"workflowRunId": runID,
				"status":        "awaiting_hitl",
				"nodeId":        hitl.NodeID,
				"resumeApi":     fmt.Sprintf("/api/workflows/runs/%s/resume", runID),
			})
		}
		if !streaming {
			return &RunResult{
				Response:     response,
				RunID:        runID,
				Status:       "awaiting_hitl",
				AwaitingHITL: true,
			}, nil
		}
		ch := registerHITLWaiter(runID)
		decision, waitErr := waitWorkflowHITLDecisionWithChannel(ctx, args.DB, runID, ch)
		unregisterHITLWaiter(runID, ch)
		if waitErr != nil {
			_ = args.DB.FinishWorkflowRun(runID, "cancelled", "", waitErr.Error())
			return nil, waitErr
		}
		if !decision.Approved {
			errText := strings.TrimSpace(decision.Comment)
			if errText == "" {
					errText = "human approval rejected"
			}
			_ = args.DB.FinishWorkflowRun(runID, "rejected", "", errText)
				rejectResponse := fmt.Sprintf("Workflow rejected at approval node %q.", firstNonEmpty(hitl.NodeLabel, hitl.NodeID))
			if args.Progress != nil {
				args.Progress("workflow_hitl_rejected", rejectResponse, map[string]interface{}{
					"workflowRunId": runID,
					"nodeId":        hitl.NodeID,
					"comment":       errText,
				})
			}
			return &RunResult{
				Response: rejectResponse,
				RunID:    runID,
				Status:   "rejected",
			}, nil
		}
		if args.Progress != nil {
			args.Progress("workflow_hitl_resumed", "human approval passed, resuming execution", map[string]interface{}{
				"workflowRunId": runID,
				"nodeId":        hitl.NodeID,
				"comment":       decision.Comment,
			})
		}
		if state.Inputs == nil {
			state.Inputs = map[string]any{}
		}
		state.Inputs["_hitl_approved"] = true
		state.Inputs["_hitl_comment"] = decision.Comment
		state.Inputs["_hitl_node_id"] = hitl.NodeID
		_ = args.DB.SetWorkflowRunStatus(runID, "running")
		resuming = true
	}

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
	if err := args.DB.FinishWorkflowRun(runID, "completed", string(outputJSON), ""); err != nil {
		return nil, err
	}
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
	if args.Logger != nil {
		args.Logger.Info("role-bound workflow completed",
			zap.String("workflow_id", wf.ID),
			zap.String("workflow_run_id", runID),
			zap.String("conversation_id", args.ConversationID),
			zap.String("role", args.Role.Name),
			zap.String("engine", "eino_workflow"),
		)
	}
	return &RunResult{Response: response, RunID: runID, Status: "completed"}, nil
}
