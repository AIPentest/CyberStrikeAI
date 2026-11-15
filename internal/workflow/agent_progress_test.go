package workflow

import "testing"

func TestWorkflowAgentProgressForwardsToolActivityWithNodeIdentity(t *testing.T) {
	state := newWorkflowLocalState(nil, "test-only-run")
	node := graphNode{ID: "agent-1", Type: "agent", Label: "Agent"}
	events := []string{}
	callback := workflowAgentProgress(func(kind, message string, data interface{}) {
		events = append(events, kind)
		m, ok := data.(map[string]interface{})
		if !ok || m["workflowNodeId"] != node.ID || m["workflowRunId"] != state.WorkflowRunID {
			t.Fatalf("missing workflow identity: %#v", data)
		}
	}, state, node)
	for _, kind := range []string{"iteration", "tool_call", "tool_result"} {
		callback(kind, "test-only activity", map[string]interface{}{"iteration": 1, "agentScope": "main"})
	}
	for _, kind := range []string{"response_start", "response_delta", "response", "done"} {
		callback(kind, "", nil)
	}
	if len(events) != 3 || events[0] != "iteration" || events[1] != "tool_call" || events[2] != "tool_result" {
		t.Fatalf("activity not forwarded or nested terminal leaked: %v", events)
	}
}
