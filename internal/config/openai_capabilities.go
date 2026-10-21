package config

import (
	"net/url"
	"strings"
	"time"
)

// UsesOfficialOpenAIReasoning scopes model-specific rules to the native OpenAI
// service. An OpenAI-compatible provider or model name alone says nothing about
// a third-party gateway's capabilities. Explicit non-OpenAI profiles win.
func (c *OpenAIConfig) UsesOfficialOpenAIReasoning() bool {
	if c == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(c.Provider)) {
	case "", "openai", "openai_compatible":
	default:
		return false
	}
	switch c.Reasoning.ProfileEffective() {
	case "auto", "openai", "openai_compat":
	default:
		return false
	}
	u, err := url.Parse(strings.TrimSpace(c.BaseURL))
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "api.openai.com") &&
		strings.TrimRight(u.Path, "/") == "/v1" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

// SupportsOpenAIReasoningNone deliberately lists known models, not future GPT
// versions, Codex models, or pro variants. See the official model/reasoning docs:
// https://developers.openai.com/api/docs/guides/reasoning
func (c *OpenAIConfig) SupportsOpenAIReasoningNone() bool {
	return c.UsesOfficialOpenAIReasoning() && matchesOpenAIModel(c.Model,
		"gpt-5.5", "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-6-sol", "gpt-6-luna")
}

// OpenAIChatToolsNeedNone identifies models whose Chat Completions function
// tools require reasoning to be explicitly disabled.
func (c *OpenAIConfig) OpenAIChatToolsNeedNone() bool {
	return c.SupportsOpenAIReasoningNone() && !matchesOpenAIModel(c.Model, "gpt-5.5")
}

// OpenAIToolsRequireResponses identifies models for which disabling reasoning
// cannot make Chat Completions tool calling work.
func (c *OpenAIConfig) OpenAIToolsRequireResponses() bool {
	return c.UsesOfficialOpenAIReasoning() && matchesOpenAIModel(c.Model, "gpt-6-astra", "gpt-6.1-sol")
}

func matchesOpenAIModel(model string, names ...string) bool {
	for _, name := range names {
		if model == name {
			return true
		}
		if suffix, ok := strings.CutPrefix(model, name+"-"); ok {
			if _, err := time.Parse("2006-01-02", suffix); err == nil {
				return true
			}
		}
	}
	return false
}
