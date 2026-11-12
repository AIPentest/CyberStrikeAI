package handler

import (
	"strings"

	"cyberstrike-ai/internal/config"
)

// auditDisableThinking selects only the audit request's provider-specific field.
func auditDisableThinking(cfg config.OpenAIConfig) bool {
	switch cfg.Reasoning.ProfileEffective() {
	case "deepseek", "deepseek_compat":
		return true
	case "auto":
	default:
		return false
	}
	identity := strings.ToLower(cfg.Model + " " + cfg.BaseURL)
	if strings.Contains(identity, "glm") {
		return false
	}
	return strings.Contains(identity, "deepseek") || strings.Contains(identity, "qwen")
}
