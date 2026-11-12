package openai

import (
	"fmt"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/config"

	"github.com/bytedance/sonic"
)

// patchNativeOpenAIReasoning runs before generic gateway cleanup. In
// particular, forced tool_choice must not erase an explicit none and restore
// the model's server-side default reasoning. Inspect the actual body model so
// per-call model overrides cannot inherit another model's capabilities.
func patchNativeOpenAIReasoning(req *http.Request, raw []byte, cfg *config.OpenAIConfig) ([]byte, bool, error) {
	if !cfg.UsesOfficialOpenAIReasoning() || req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, "api.openai.com") {
		return raw, false, nil
	}
	var payload map[string]any
	if err := sonic.Unmarshal(raw, &payload); err != nil {
		return raw, false, nil
	}
	actual := *cfg
	actual.Model, _ = payload["model"].(string)
	hasFunctionTools := false
	if tools, ok := payload["tools"].([]any); ok {
		for _, tool := range tools {
			if definition, ok := tool.(map[string]any); ok && definition["type"] == "function" {
				hasFunctionTools = true
			}
		}
	}
	if actual.OpenAIToolsRequireResponses() && hasFunctionTools {
		return nil, true, fmt.Errorf("OpenAI model %q requires /v1/responses for function tools and does not support reasoning_effort=none; this backend uses /v1/chat/completions: select a compatible model", actual.Model)
	}
	if !actual.SupportsOpenAIReasoningNone() {
		return raw, false, nil
	}
	effort, _ := payload["reasoning_effort"].(string)
	if actual.OpenAIChatToolsNeedNone() && hasFunctionTools && effort != "none" {
		return nil, true, fmt.Errorf("OpenAI model %q requires reasoning_effort=none for function tools in /v1/chat/completions: set reasoning mode to off; reasoning with function tools requires /v1/responses", actual.Model)
	}
	if effort != "none" || !forcedToolChoiceIncompatibleWithThinking(payload) {
		return raw, false, nil
	}
	stripReasoningFields(payload)
	payload["reasoning_effort"] = "none"
	out, err := sonic.Marshal(payload)
	return out, true, err
}
