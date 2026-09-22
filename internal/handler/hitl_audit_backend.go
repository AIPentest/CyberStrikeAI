package handler

import (
	"encoding/json"
	"strings"

	"cyberstrike-ai/internal/config"
)

func (h *AgentHandler) hitlAuditEngineInfo() (backend, model string) {
	backend = config.HitlAuditBackendOpenAI
	if h == nil || h.config == nil {
		return backend, ""
	}
	backend = h.config.Hitl.EffectiveAuditBackend()
	if backend == config.HitlAuditBackendTypeSafe {
		_, _, model = h.config.Hitl.TypeSafeConfigEffective()
		return backend, model
	}
	return backend, strings.TrimSpace(h.config.Hitl.AuditModelEffective(h.config.OpenAI).Model)
}

func stringifyHitlJSON(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(b))
}

func inferHitlAuditBackendFromComment(comment string) string {
	c := strings.ToLower(comment)
	if strings.Contains(comment, "TypeSafe") || strings.Contains(comment, "破坏分") ||
		strings.Contains(c, "choice=") || strings.Contains(comment, "Jev") {
		return config.HitlAuditBackendTypeSafe
	}
	if strings.TrimSpace(comment) == "" {
		return ""
	}
	return config.HitlAuditBackendOpenAI
}

func hitlAuditBackendFromRecord(decidedBy, comment, payloadJSON string) (backend, model string) {
	if normalizeHitlDecidedBy(decidedBy) != "audit_agent" {
		return "", ""
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &root); err == nil {
		if appr, ok := root["hitlApproval"].(map[string]any); ok {
			raw := stringifyHitlJSON(appr["auditBackend"])
			if raw != "" {
				backend = (config.HitlConfig{AuditBackend: raw}).EffectiveAuditBackend()
			}
			model = stringifyHitlJSON(appr["auditModel"])
		}
	}
	if backend == "" {
		backend = inferHitlAuditBackendFromComment(comment)
	}
	if backend == "" {
		backend = config.HitlAuditBackendOpenAI
	}
	return backend, model
}
