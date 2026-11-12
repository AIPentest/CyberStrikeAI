package config

import "testing"

func TestOfficialOpenAIReasoningCapabilities(t *testing.T) {
	for _, tc := range []struct {
		model                      string
		none, toolsNone, responses bool
	}{
		{"gpt-5.5", true, false, false},
		{"gpt-5.5-2026-04-23", true, false, false},
		{"gpt-5.6", true, true, false},
		{"gpt-5.6-sol", true, true, false},
		{"gpt-5.6-terra", true, true, false},
		{"gpt-5.6-luna", true, true, false},
		{"gpt-6-sol", true, true, false},
		{"gpt-6-luna", true, true, false},
		{"gpt-6.1-sol", false, false, true},
		{"gpt-6-astra", false, false, true},
		{"gpt-5-nano", false, false, false},
		{"gpt-4o-mini", false, false, false},
		{"gpt-5.5-pro", false, false, false},
		{"gpt-5.6-sol-custom", false, false, false},
		{"gpt-5.6-sol-2026-99-99", false, false, false},
		{"gpt-6.2-sol", false, false, false},
		{"deepseek-chat", false, false, false},
	} {
		t.Run(tc.model, func(t *testing.T) {
			cfg := &OpenAIConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: tc.model}
			if cfg.SupportsOpenAIReasoningNone() != tc.none || cfg.OpenAIChatToolsNeedNone() != tc.toolsNone || cfg.OpenAIToolsRequireResponses() != tc.responses {
				t.Fatalf("unexpected capabilities: none=%v toolsNone=%v responses=%v", cfg.SupportsOpenAIReasoningNone(), cfg.OpenAIChatToolsNeedNone(), cfg.OpenAIToolsRequireResponses())
			}
		})
	}
}

func TestOpenAICapabilityServiceBoundary(t *testing.T) {
	for _, tc := range []struct {
		provider, url, profile string
		want                   bool
	}{
		{"", "https://api.openai.com/v1", "", true},
		{"openai_compatible", "https://api.openai.com/v1/", "openai_compat", true},
		{"openai", "https://api.openai.com/v1", "deepseek_compat", false},
		{"claude", "https://api.openai.com/v1", "openai_compat", false},
		{"openai", "https://api.deepseek.com/v1", "openai_compat", false},
		{"openai", "https://gateway.example/v1", "openai_compat", false},
		{"openai", "https://api.openai.com.evil.example/v1", "", false},
		{"openai", "https://api.openai.com/custom/v1", "", false},
		{"openai", "https://api.openai.com/v1?gateway=1", "", false},
		{"openai", "http://api.openai.com/v1", "", false},
		{"openai", "", "", false},
	} {
		t.Run(tc.provider+tc.url+tc.profile, func(t *testing.T) {
			cfg := &OpenAIConfig{Provider: tc.provider, BaseURL: tc.url, Model: "gpt-5.6-sol", Reasoning: OpenAIReasoningConfig{Profile: tc.profile}}
			if cfg.SupportsOpenAIReasoningNone() != tc.want {
				t.Fatalf("none=%v, want %v", cfg.SupportsOpenAIReasoningNone(), tc.want)
			}
		})
	}
	var cfg *OpenAIConfig
	if cfg.SupportsOpenAIReasoningNone() || cfg.OpenAIToolsRequireResponses() {
		t.Fatal("nil config has capabilities")
	}
}
