package reasoning

import (
	"testing"

	"cyberstrike-ai/internal/config"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestOpenAINoneIntentPrecedence(t *testing.T) {
	no := false
	for _, tc := range []struct {
		name, mode, effort string
		client             *ClientIntent
		allow              *bool
		want               string
	}{
		{"off", "off", "high", nil, nil, "none"},
		{"server_none", "on", "none", nil, nil, "none"},
		{"client_off", "on", "high", &ClientIntent{Mode: "off"}, nil, "none"},
		{"client_none", "on", "high", &ClientIntent{Effort: " NONE "}, nil, "none"},
		{"client_high_overrides_server_none", "on", "none", &ClientIntent{Effort: "high"}, nil, "high"},
		{"client_none_disallowed", "on", "high", &ClientIntent{Effort: "none"}, &no, "high"},
		{"client_off_disallowed", "on", "high", &ClientIntent{Mode: "off"}, &no, "high"},
		{"server_none_client_disallowed", "on", "none", &ClientIntent{Effort: "high"}, &no, "none"},
		{"auto_unspecified", "auto", "", nil, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oa := &config.OpenAIConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol",
				Reasoning: config.OpenAIReasoningConfig{Mode: tc.mode, Effort: tc.effort, AllowClientReasoning: tc.allow}}
			cfg := &einoopenai.ChatModelConfig{}
			ApplyToEinoChatModelConfig(cfg, oa, tc.client)
			if string(cfg.ReasoningEffort) != tc.want {
				t.Fatalf("classic effort=%q, want %q", cfg.ReasoningEffort, tc.want)
			}
			fields := AgenticOpenAIExtraFields(oa, tc.client)
			got, _ := fields["reasoning_effort"].(string)
			if got != tc.want {
				t.Fatalf("agentic effort=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestNoneDoesNotChangeOtherProviders(t *testing.T) {
	for _, oa := range []config.OpenAIConfig{
		{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		{BaseURL: "https://gateway.example/v1", Model: "gpt-5.6-sol"},
		{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		{BaseURL: "https://api.openai.com/v1", Model: "gpt-6.1-sol"},
	} {
		t.Run(oa.BaseURL+oa.Model, func(t *testing.T) {
			oa.Reasoning = config.OpenAIReasoningConfig{Mode: "on", Effort: "high"}
			cfg := &einoopenai.ChatModelConfig{}
			baseline := &einoopenai.ChatModelConfig{}
			ApplyToEinoChatModelConfig(cfg, &oa, &ClientIntent{Effort: "none"})
			ApplyToEinoChatModelConfig(baseline, &oa, nil)
			if cfg.ReasoningEffort != baseline.ReasoningEffort {
				t.Fatalf("none changed unsupported model: %#v", cfg)
			}
			if cfg.ExtraFields["reasoning_effort"] != baseline.ExtraFields["reasoning_effort"] {
				t.Fatalf("none changed provider effort: %#v", cfg.ExtraFields)
			}
		})
	}
}
