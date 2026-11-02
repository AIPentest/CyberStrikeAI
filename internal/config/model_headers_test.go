package config

import "testing"

func TestAuditModelHeaderIdentityBoundary(t *testing.T) {
	t.Setenv("CSAI_AUDIT_TEST_HEADER", "audit-secret")
	main := OpenAIConfig{Provider: "openai", BaseURL: "https://main.invalid/v1", APIKey: "main-key", Headers: map[string]string{"Authorization": "Bearer main"}}
	for _, tc := range []struct {
		name  string
		audit OpenAIConfig
		want  string
	}{
		{"inherit", OpenAIConfig{}, "Bearer main"},
		{"model only", OpenAIConfig{Model: "small"}, "Bearer main"},
		{"provider", OpenAIConfig{Provider: "openai"}, ""},
		{"endpoint", OpenAIConfig{BaseURL: "https://audit.invalid/v1"}, ""},
		{"key", OpenAIConfig{APIKey: "audit-key"}, ""},
		{"explicit", OpenAIConfig{BaseURL: "https://audit.invalid", Headers: map[string]string{"Authorization": "${CSAI_AUDIT_TEST_HEADER}"}}, "audit-secret"},
		{"empty follows inheritance", OpenAIConfig{Headers: map[string]string{}}, "Bearer main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (HitlConfig{AuditModel: tc.audit}).AuditModelEffective(main)
			if got.Headers["Authorization"] != tc.want {
				t.Fatalf("headers=%v", got.Headers)
			}
			if got.Headers != nil {
				got.Headers["Authorization"] = "changed"
			}
			if main.Headers["Authorization"] != "Bearer main" {
				t.Fatal("mutated main headers")
			}
		})
	}
}
