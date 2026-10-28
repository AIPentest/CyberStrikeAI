package handler

// addAgentRunSpec reuses each synchronous request schema without changing it.
func addAgentRunSpec(spec map[string]interface{}) {
	paths := spec["paths"].(map[string]interface{})
	for _, base := range []string{"/api/eino-agent", "/api/multi-agent"} {
		original := paths[base].(map[string]interface{})["post"].(map[string]interface{})
		paths[base+"/runs"] = map[string]interface{}{"post": map[string]interface{}{
			"tags": []string{"Conversation Interaction"}, "summary": "Asynchronously submit conversation task", "description": "Returns 202 after receiving the request; a client disconnect does not cancel the task. An Idempotency-Key isolated per user must be provided; the same key and request return the original receipt, a different request returns 409. Poll the final result via statusUrl; cancellation uses /api/agent-loop/cancel.",
			"requestBody": original["requestBody"],
			"parameters":  []map[string]interface{}{{"in": "header", "name": "Idempotency-Key", "required": true, "schema": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 200}}},
			"responses":   map[string]interface{}{"202": map[string]interface{}{"description": "Accepted; returns runId, conversationId, assistantMessageId, status, statusUrl; Location is the query address"}, "400": map[string]interface{}{"description": "Invalid parameters or mode"}, "403": map[string]interface{}{"description": "No permission"}, "409": map[string]interface{}{"description": "Idempotency key payload conflict or a session is currently executing"}, "500": map[string]interface{}{"description": "Failed to persist receipt record"}},
		}}
		paths[base+"/runs/{runId}"] = map[string]interface{}{"get": map[string]interface{}{
			"tags": []string{"Conversation Interaction"}, "summary": "Query asynchronous conversation task", "description": "Returns persistent status, result, conversation and message identifiers and timestamps. After a server restart, unfinished tasks become interrupted and are not automatically replayed.",
			"parameters": []map[string]interface{}{{"in": "path", "name": "runId", "required": true, "schema": map[string]interface{}{"type": "string"}}},
			"responses":  map[string]interface{}{"200": map[string]interface{}{"description": "Task status and result"}, "403": map[string]interface{}{"description": "No access to the conversation"}, "404": map[string]interface{}{"description": "Task not found"}},
		}}
	}
}
