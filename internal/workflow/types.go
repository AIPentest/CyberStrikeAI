package workflow

import (
	"fmt"
	"strconv"
)

// WorkflowInput is the typed entry for Agent compose.Workflow[I,O].
type WorkflowInput struct {
	Message         string `json:"message"`
	ConversationID  string `json:"conversationId"`
	ProjectID       string `json:"projectId"`
	Role            string `json:"role"`
	WorkflowID      string `json:"workflowId"`
	WorkflowVersion int    `json:"workflowVersion"`
}

// WorkflowOutput aggregates terminal node payloads keyed by canvas node id.
type WorkflowOutput map[string]any

// WorkflowNodeOutput is the per-node lambda payload (alias for Agent edge type alignment).
type WorkflowNodeOutput = map[string]interface{}

func workflowInputFromMap(m map[string]interface{}) WorkflowInput {
	in := WorkflowInput{}
	if m == nil {
		return in
	}
	if v, ok := m["message"].(string); ok {
		in.Message = v
	} else if m["message"] != nil {
		in.Message = fmt.Sprint(m["message"])
	}
	if v, ok := m["conversationId"].(string); ok {
		in.ConversationID = v
	}
	if v, ok := m["projectId"].(string); ok {
		in.ProjectID = v
	}
	if v, ok := m["role"].(string); ok {
		in.Role = v
	}
	if v, ok := m["workflowId"].(string); ok {
		in.WorkflowID = v
	}
	switch v := m["workflowVersion"].(type) {
	case int:
		in.WorkflowVersion = v
	case int64:
		in.WorkflowVersion = int(v)
	case float64:
		in.WorkflowVersion = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			in.WorkflowVersion = n
		}
	}
	return in
}

func (in WorkflowInput) toStateInputs() map[string]any {
	return map[string]any{
		"message":         in.Message,
		"conversationId":  in.ConversationID,
		"projectId":       in.ProjectID,
		"role":            in.Role,
		"workflowId":      in.WorkflowID,
		"workflowVersion": in.WorkflowVersion,
	}
}

func cacheKey(workflowID string, version int) string {
	return workflowID + ":" + strconv.Itoa(version)
}
