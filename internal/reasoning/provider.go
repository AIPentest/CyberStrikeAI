// Package reasoning resolves provider-neutral reasoning intent and request extensions.
package reasoning

import (
	"strings"

	"cyberstrike-ai/internal/config"
)

// ClientIntent is optional per-request override from ChatRequest.reasoning.
type ClientIntent struct {
	Mode   string
	Effort string
}

// ThinkingLevel is the SDK-level selection, independent of provider wire fields.
func ThinkingLevel(oa *config.OpenAIConfig, client *ClientIntent) string {
	if oa == nil {
		return "off"
	}
	mode := effectiveModeForModel(oa, client)
	if mode == "off" {
		return "off"
	}
	effort := effectiveEffort(&oa.Reasoning, client, oa.Reasoning.AllowClientReasoningEffective())
	if effort != "" {
		return effort
	}
	if mode == "on" {
		return "medium"
	}
	return "off"
}

type wireProfile int

const (
	wireNone wireProfile = iota
	wireClaude
	wireDeepseek
	wireOpenAI
	wireOutputConfig
)

// ProviderExtraFields preserves channel-specific request options for Pi provider calls.
func ProviderExtraFields(oa *config.OpenAIConfig, client *ClientIntent) map[string]any {
	if oa == nil {
		return nil
	}
	sr := &oa.Reasoning
	allowClient := sr.AllowClientReasoningEffective()
	mode := effectiveModeForModel(oa, client)
	fields := cloneExtraRequestFields(sr.ExtraRequestFields)
	if mode == "off" {
		clearReasoningExtraFields(fields)
		if resolveWireProfile(oa, sr) == wireDeepseek {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["thinking"] = map[string]any{"type": "disabled"}
		}
		return openAIReasoningDisabledFields(fields, oa)
	}
	if strings.EqualFold(strings.TrimSpace(oa.Provider), "claude") ||
		strings.EqualFold(strings.TrimSpace(oa.Provider), "anthropic") {
		return fields
	}
	effort := effectiveEffort(sr, client, allowClient)
	switch resolveWireProfile(oa, sr) {
	case wireDeepseek:
		if mode == "auto" || mode == "on" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["thinking"] = map[string]any{"type": "enabled"}
		}
		if effort != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["reasoning_effort"] = effortStringForAPI(effort)
		}
	case wireOutputConfig:
		e := effort
		if mode == "on" && e == "" {
			e = "high"
		}
		if e != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["output_config"] = map[string]any{"effort": effortStringForAPI(e)}
		}
	default:
		e := effort
		if mode == "on" && e == "" {
			e = "medium"
		}
		if e != "" {
			if fields == nil {
				fields = make(map[string]any)
			}
			fields["reasoning_effort"] = effortStringForAPI(e)
		}
	}
	return fields
}

func cloneExtraRequestFields(fields map[string]interface{}) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}

func clearReasoningExtraFields(fields map[string]any) {
	for _, key := range []string{"thinking", "reasoning_effort", "output_config", "reasoning"} {
		delete(fields, key)
	}
}

// claudeSonnet37DefaultBudgetTokens matches Anthropic extended-thinking documentation examples (budget_tokens with max_tokens 16000).

func effectiveMode(sr *config.OpenAIReasoningConfig, client *ClientIntent, allowClient bool) string {
	server := strings.ToLower(strings.TrimSpace(sr.ModeEffective()))
	if server == "" || server == "default" {
		server = "auto"
	}
	if !allowClient || client == nil {
		return server
	}
	cm := strings.ToLower(strings.TrimSpace(client.Mode))
	if cm == "" || cm == "default" {
		return server
	}
	return cm
}

func effectiveEffort(sr *config.OpenAIReasoningConfig, client *ClientIntent, allowClient bool) string {
	se := normalizeEffort(sr.Effort)
	if !allowClient || client == nil {
		return se
	}
	ce := normalizeEffort(client.Effort)
	if ce != "" {
		return ce
	}
	return se
}

func normalizeEffort(s string) string {
	e := strings.ToLower(strings.TrimSpace(s))
	switch e {
	case "low", "medium", "high", "max", "xhigh":
		return e
	default:
		return ""
	}
}

func resolveWireProfile(oa *config.OpenAIConfig, sr *config.OpenAIReasoningConfig) wireProfile {
	provider := strings.TrimSpace(oa.Provider)
	if strings.EqualFold(provider, "claude") || strings.EqualFold(provider, "anthropic") {
		return wireClaude
	}
	p := strings.ToLower(strings.TrimSpace(sr.ProfileEffective()))
	switch p {
	case "output_config", "output_config_effort":
		return wireOutputConfig
	case "openai", "openai_compat":
		return wireOpenAI
	case "deepseek", "deepseek_compat":
		return wireDeepseek
	case "auto", "":
		if oa.IsDeepSeekEndpointOrModel() {
			return wireDeepseek
		}
		return wireOpenAI
	default:
		return wireOpenAI
	}
}

func effortStringForAPI(e string) string {
	// 原样透传：OpenAI 官方多为 xhigh，部分兼容网关为 max，由配置/对话 effort 选择。
	return strings.ToLower(strings.TrimSpace(e))
}
