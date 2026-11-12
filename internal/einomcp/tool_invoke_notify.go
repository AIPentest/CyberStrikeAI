package einomcp

import "sync"

// ToolInvokeNotifyHolder Shared by the Eino run loop and the MCP/execute bridge; Fire is triggered when the tool originally returns.
// UI whose tool_result must wait for the ADK schema.Tool event (post-reduction body); it is not pushed from this holder's callback.
type ToolInvokeNotifyHolder struct {
	mu sync.RWMutex
	fn func(toolCallID, toolName, einoAgent string, success bool, content string, invokeErr error)
}

// NewToolInvokeNotifyHolder Creates one that can be shared between ToolsFromDefinitions and the run loop holder.
func NewToolInvokeNotifyHolder() *ToolInvokeNotifyHolder {
	return &ToolInvokeNotifyHolder{}
}

// Set Called by runEinoADKAgentLoop before it starts consuming iter; may be overridden multiple times (usually only once).
func (h *ToolInvokeNotifyHolder) Set(fn func(toolCallID, toolName, einoAgent string, success bool, content string, invokeErr error)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fn = fn
}

// Fire Called by mcpBridgeTool when a tool call returns; ignored if it has not been Set or toolCallID is empty.
func (h *ToolInvokeNotifyHolder) Fire(toolCallID, toolName, einoAgent string, success bool, content string, invokeErr error) {
	if h == nil {
		return
	}
	h.mu.RLock()
	fn := h.fn
	h.mu.RUnlock()
	if fn == nil {
		return
	}
	fn(toolCallID, toolName, einoAgent, success, content, invokeErr)
}
