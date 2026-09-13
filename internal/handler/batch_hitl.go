package handler

import "fmt"

// Empty policy preserves the global defaults for queues created before this setting existed.
func validateBatchHITLPolicy(policy string) error {
	switch policy {
	case "", "off", "human", "audit_agent", "review_edit":
		return nil
	default:
		return fmt.Errorf("不支持的队列审批设置: %s", policy)
	}
}

func (h *AgentHandler) batchHITLRequest(policy string) *HITLRequest {
	req := h.hitlEffectiveDefaultRequest()
	switch policy {
	case "off":
		req.Enabled, req.Mode = false, "off"
	case "human":
		req.Enabled, req.Mode, req.Reviewer = true, "approval", "human"
	case "audit_agent":
		req.Enabled, req.Mode, req.Reviewer = true, "approval", "audit_agent"
	case "review_edit":
		req.Enabled, req.Mode, req.Reviewer = true, "review_edit", "audit_agent"
	}
	return req
}
