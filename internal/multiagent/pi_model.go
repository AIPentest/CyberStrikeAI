package multiagent

import (
	"cyberstrike-ai/internal/agentruntime"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/reasoning"
	"strings"
)

func piModelConfig(oa config.OpenAIConfig, intent *reasoning.ClientIntent, conversationID string) agentruntime.Model {
	thinking := reasoning.ThinkingLevel(&oa, intent)
	api := "openai-completions"
	provider := strings.ToLower(strings.TrimSpace(oa.Provider))
	if provider == "claude" || provider == "anthropic" {
		api = "anthropic-messages"
	} else if oa.OpenAIToolsRequireResponses() {
		api = "openai-responses"
	}
	base := strings.TrimRight(strings.TrimSpace(oa.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
		if api == "anthropic-messages" {
			base = "https://api.anthropic.com"
		}
	}
	headers := map[string]string{}
	for key, value := range oa.Headers {
		if strings.Contains(value, "%session_id%") && conversationID == "" {
			continue
		}
		headers[key] = strings.ReplaceAll(value, "%session_id%", conversationID)
	}
	budget := oa.EffectiveMaxTotalTokens()
	window := oa.EffectiveContextWindow()
	if window <= 0 {
		window = budget + oa.MaxCompletionTokensEffective()
	}
	extra := reasoning.ProviderExtraFields(&oa, intent)
	if api == "openai-responses" {
		extra = oa.Reasoning.ExtraRequestFields
	}
	compat := map[string]any{}
	// Omission is opt-in: removing an explicit store:false globally could
	// change retention behavior on otherwise working compatible gateways.
	// ProviderExtraFields returns a copy, so this cannot mutate channel config.
	if value, exists := extra["store"]; api == "openai-completions" && exists && value == nil {
		compat["supportsStore"] = false
		delete(extra, "store")
	}
	if oa.IsLocalContextProfile() {
		compat["maxTokensField"] = "max_tokens"
		compat["supportsStore"] = false
	}
	profile := oa.Reasoning.ProfileEffective()
	if api == "openai-completions" && (profile == "deepseek" || profile == "deepseek_compat" || (profile == "auto" && oa.IsDeepSeekEndpointOrModel())) {
		compat["thinkingFormat"] = "deepseek"
		compat["requiresReasoningContentOnAssistantMessages"] = true
		compat["maxTokensField"] = "max_tokens"
	}
	return agentruntime.Model{Provider: provider, API: api, ID: oa.Model, BaseURL: base, APIKey: oa.APIKey, Headers: headers, ContextWindow: window, InputBudget: budget, MaxTokens: oa.MaxCompletionTokensEffective(), Thinking: thinking, ExtraFields: extra, Compat: compat, CompactToolJSON: oa.IsEconomyContextProfile()}
}
