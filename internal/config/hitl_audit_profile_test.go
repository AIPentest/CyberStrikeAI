package config

import "testing"

func TestAuditModelProfileInheritance(t *testing.T) {
	main := OpenAIConfig{Reasoning: OpenAIReasoningConfig{Profile: "deepseek_compat"}}
	for _, tc := range []struct {
		name  string
		audit OpenAIConfig
		want  string
	}{
		{"inherit", OpenAIConfig{}, "deepseek_compat"},
		{"key alone inherits", OpenAIConfig{APIKey: "other"}, "deepseek_compat"},
		{"model isolates", OpenAIConfig{Model: "glm"}, ""},
		{"url isolates", OpenAIConfig{BaseURL: "https://example.com"}, ""},
		{"provider isolates", OpenAIConfig{Provider: "openai"}, ""},
		{"explicit overrides", OpenAIConfig{Reasoning: OpenAIReasoningConfig{Profile: "openai_compat"}}, "openai_compat"},
		{"explicit auto overrides", OpenAIConfig{Reasoning: OpenAIReasoningConfig{Profile: "auto"}}, "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (HitlConfig{AuditModel: tc.audit}).AuditModelEffective(main).Reasoning.Profile
			if got != tc.want {
				t.Fatalf("profile=%q want %q", got, tc.want)
			}
		})
	}
}
