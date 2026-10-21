package reasoning

import (
	"strings"

	"cyberstrike-ai/internal/config"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
)

// effectiveModeForModel accepts effort=none only for known native OpenAI
// models. Other providers keep their existing effort normalization and fallback.
func effectiveModeForModel(oa *config.OpenAIConfig, client *ClientIntent) string {
	sr := &oa.Reasoning
	allowClient := sr.AllowClientReasoningEffective()
	mode := effectiveMode(sr, client, allowClient)
	if !oa.SupportsOpenAIReasoningNone() {
		return mode
	}
	effort := sr.Effort
	if allowClient && client != nil && (normalizeEffort(client.Effort) != "" || strings.EqualFold(strings.TrimSpace(client.Effort), "none")) {
		effort = client.Effort
	}
	if strings.EqualFold(strings.TrimSpace(effort), "none") {
		return "off"
	}
	return mode
}

func applyOpenAIReasoningDisabled(cfg *einoopenai.ChatModelConfig, oa *config.OpenAIConfig) {
	if oa.SupportsOpenAIReasoningNone() {
		cfg.ReasoningEffort = einoopenai.ReasoningEffortLevel("none")
	}
}

func openAIReasoningDisabledFields(fields map[string]any, oa *config.OpenAIConfig) map[string]any {
	if oa.SupportsOpenAIReasoningNone() {
		if fields == nil {
			fields = make(map[string]any)
		}
		fields["reasoning_effort"] = "none"
	}
	return fields
}
