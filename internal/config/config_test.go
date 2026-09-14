package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureLocalConfigCreatesFromExample(t *testing.T) {
	dir := t.TempDir()
	examplePath := filepath.Join(dir, "config.example.yaml")
	configPath := filepath.Join(dir, "config.yaml")

	example := []byte(`auth:
  session_duration_hours: 12
server:
  host: 127.0.0.1
  port: 8080
`)
	if err := os.WriteFile(examplePath, example, 0644); err != nil {
		t.Fatalf("write example: %v", err)
	}

	result, err := EnsureLocalConfig(configPath)
	if err != nil {
		t.Fatalf("EnsureLocalConfig: %v", err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true")
	}
	if result.ExamplePath != examplePath {
		t.Fatalf("ExamplePath = %q, want %q", result.ExamplePath, examplePath)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load generated config: %v", err)
	}
	if cfg.Auth.SessionDurationHours != 12 {
		t.Fatalf("SessionDurationHours = %d, want 12", cfg.Auth.SessionDurationHours)
	}

	second, err := EnsureLocalConfig(configPath)
	if err != nil {
		t.Fatalf("EnsureLocalConfig existing: %v", err)
	}
	if second.Created {
		t.Fatal("Created = true for existing config, want false")
	}
}

func TestLoadIgnoresLegacyAuthPasswordField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := strings.Join([]string{
		"auth:",
		`  password: "legacy-password"`,
		"  session_duration_hours: 12",
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.SessionDurationHours != 12 {
		t.Fatalf("SessionDurationHours = %d, want 12", cfg.Auth.SessionDurationHours)
	}
}

func TestHitlAuditModelEffectiveFallsBackToMainConfig(t *testing.T) {
	main := OpenAIConfig{
		Provider: "openai",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "main-key",
		Model:    "large-model",
	}

	got := (HitlConfig{
		AuditModel: OpenAIConfig{Model: "small-reviewer"},
	}).AuditModelEffective(main)

	if got.Provider != main.Provider || got.BaseURL != main.BaseURL || got.APIKey != main.APIKey {
		t.Fatalf("expected provider/base_url/api_key to inherit main config, got %+v", got)
	}
	if got.Model != "small-reviewer" {
		t.Fatalf("expected audit model override, got %q", got.Model)
	}
}

func TestHitlDefaultConfigEffectiveValues(t *testing.T) {
	if got := (HitlConfig{}).EffectiveDefaultMode(); got != "off" {
		t.Fatalf("empty default mode = %q, want off", got)
	}
	if got := (HitlConfig{DefaultMode: "review-edit"}).EffectiveDefaultMode(); got != "off" {
		t.Fatalf("unknown default mode = %q, want off", got)
	}
	if got := (HitlConfig{DefaultMode: "review_edit"}).EffectiveDefaultMode(); got != "review_edit" {
		t.Fatalf("review_edit default mode = %q, want review_edit", got)
	}
	if got := (HitlConfig{}).EffectiveDefaultTimeoutSeconds(); got != 300 {
		t.Fatalf("empty default timeout = %d, want 300", got)
	}
	zero := 0
	if got := (HitlConfig{DefaultTimeoutSeconds: &zero}).EffectiveDefaultTimeoutSeconds(); got != 0 {
		t.Fatalf("zero default timeout = %d, want 0", got)
	}
	neg := -1
	if got := (HitlConfig{DefaultTimeoutSeconds: &neg}).EffectiveDefaultTimeoutSeconds(); got != 0 {
		t.Fatalf("negative default timeout = %d, want 0", got)
	}
}

func TestLoadUsesAIDefaultChannelAsRuntimeOpenAI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := strings.Join([]string{
		"ai:",
		"  default_channel: deepseek",
		"  channels:",
		"    qwen:",
		"      name: Qwen",
		"      provider: openai_compatible",
		"      base_url: https://dashscope.example/v1",
		"      api_key: qwen-key",
		"      model: qwen-max",
		"    deepseek:",
		"      name: DeepSeek",
		"      provider: openai_compatible",
		"      base_url: https://deepseek.example/v1",
		"      api_key: deepseek-key",
		"      model: deepseek-chat",
		"      max_total_tokens: 64000",
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OpenAI.Model != "deepseek-chat" || cfg.OpenAI.APIKey != "deepseek-key" || cfg.OpenAI.MaxTotalTokens != 64000 {
		t.Fatalf("runtime OpenAI config did not follow ai.default_channel: %+v", cfg.OpenAI)
	}
	oa, id, ok := cfg.ResolveAIChannel("qwen")
	if !ok || id != "qwen" || oa.Model != "qwen-max" || oa.APIKey != "qwen-key" {
		t.Fatalf("ResolveAIChannel(qwen) = (%+v, %q, %v)", oa, id, ok)
	}
}

func TestNormalizeAIProviderProfilesForOfficialDeepSeekEndpoint(t *testing.T) {
	cfg := &Config{
		OpenAI: OpenAIConfig{
			BaseURL: "https://api.deepseek.com/v1",
			Model:   "deepseek-chat",
			Reasoning: OpenAIReasoningConfig{
				Profile: "openai_compat",
			},
		},
		AI: AIConfig{
			Channels: map[string]AIChannelConfig{
				"official": {
					BaseURL: "api.deepseek.com/v1",
					Model:   "deepseek-chat",
					Reasoning: OpenAIReasoningConfig{
						Profile: "auto",
					},
				},
				"gateway": {
					BaseURL: "https://compatible.example.com/v1",
					Model:   "deepseek-chat",
					Reasoning: OpenAIReasoningConfig{
						Profile: "openai_compat",
					},
				},
			},
		},
	}

	cfg.NormalizeAIProviderProfiles()

	if cfg.OpenAI.Reasoning.Profile != "deepseek" {
		t.Fatalf("openai profile = %q, want deepseek", cfg.OpenAI.Reasoning.Profile)
	}
	if got := cfg.AI.Channels["official"].Reasoning.Profile; got != "deepseek" {
		t.Fatalf("official channel profile = %q, want deepseek", got)
	}
	if got := cfg.AI.Channels["gateway"].Reasoning.Profile; got != "openai_compat" {
		t.Fatalf("gateway profile should be preserved, got %q", got)
	}
}

func TestLoadNormalizesDefaultChannelForOfficialDeepSeekEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := strings.Join([]string{
		"ai:",
		"  default_channel: deepseek",
		"  channels:",
		"    deepseek:",
		"      name: DeepSeek",
		"      provider: openai_compatible",
		"      base_url: https://api.deepseek.com/v1",
		"      api_key: deepseek-key",
		"      model: deepseek-chat",
		"      reasoning:",
		"        profile: openai_compat",
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OpenAI.Reasoning.Profile != "deepseek" {
		t.Fatalf("runtime OpenAI profile = %q, want deepseek", cfg.OpenAI.Reasoning.Profile)
	}
	if got := cfg.AI.Channels["deepseek"].Reasoning.Profile; got != "deepseek" {
		t.Fatalf("channel profile = %q, want deepseek", got)
	}
}

func TestSummarizationUserIntentLedgerRunesEffective(t *testing.T) {
	var zero MultiAgentEinoMiddlewareConfig
	if got := zero.SummarizationUserIntentLedgerMaxRunesEffective(); got != DefaultSummarizationUserIntentLedgerMaxRunes {
		t.Fatalf("default ledger max runes = %d, want %d", got, DefaultSummarizationUserIntentLedgerMaxRunes)
	}
	if got := zero.SummarizationUserIntentLedgerEntryMaxRunesEffective(); got != DefaultSummarizationUserIntentLedgerEntryMaxRunes {
		t.Fatalf("default ledger entry max runes = %d, want %d", got, DefaultSummarizationUserIntentLedgerEntryMaxRunes)
	}

	custom := MultiAgentEinoMiddlewareConfig{
		SummarizationUserIntentLedgerMaxRunes:      12345,
		SummarizationUserIntentLedgerEntryMaxRunes: 2345,
	}
	if got := custom.SummarizationUserIntentLedgerMaxRunesEffective(); got != 12345 {
		t.Fatalf("custom ledger max runes = %d", got)
	}
	if got := custom.SummarizationUserIntentLedgerEntryMaxRunesEffective(); got != 2345 {
		t.Fatalf("custom ledger entry max runes = %d", got)
	}
}

func TestSummarizationOutputReserveTokensEffective(t *testing.T) {
	var zero MultiAgentEinoMiddlewareConfig
	if got := zero.SummarizationOutputReserveTokensEffective(); got != DefaultSummarizationOutputReserveTokens {
		t.Fatalf("default output reserve = %d, want %d", got, DefaultSummarizationOutputReserveTokens)
	}
	custom := MultiAgentEinoMiddlewareConfig{SummarizationOutputReserveTokens: 4096}
	if got := custom.SummarizationOutputReserveTokensEffective(); got != 4096 {
		t.Fatalf("custom output reserve = %d", got)
	}
}

func TestOpenAIOutputLimitValidation(t *testing.T) {
	if got := (OpenAIConfig{}).MaxCompletionTokensEffective(); got != DefaultMaxCompletionTokens {
		t.Fatalf("max completion default=%d", got)
	}
	if err := validateOpenAIOutputLimits(OpenAIConfig{MaxCompletionTokens: -1}); err == nil {
		t.Fatal("negative completion limit must fail")
	}
}

func TestLatestUserMessageRunesEffective(t *testing.T) {
	var zero MultiAgentEinoMiddlewareConfig
	if got := zero.LatestUserMessageMaxRunesEffective(); got != DefaultLatestUserMessageMaxRunes {
		t.Fatalf("default latest user max runes = %d, want %d", got, DefaultLatestUserMessageMaxRunes)
	}
	if got := zero.LatestUserMessageHeadRunesEffective(); got != DefaultLatestUserMessageHeadRunes {
		t.Fatalf("default latest user head runes = %d, want %d", got, DefaultLatestUserMessageHeadRunes)
	}
	if got := zero.LatestUserMessageTailRunesEffective(); got != DefaultLatestUserMessageTailRunes {
		t.Fatalf("default latest user tail runes = %d, want %d", got, DefaultLatestUserMessageTailRunes)
	}

	custom := MultiAgentEinoMiddlewareConfig{
		LatestUserMessageMaxRunes:  100,
		LatestUserMessageHeadRunes: 40,
		LatestUserMessageTailRunes: 60,
	}
	if got := custom.LatestUserMessageMaxRunesEffective(); got != 100 {
		t.Fatalf("custom latest user max runes = %d", got)
	}
	if got := custom.LatestUserMessageHeadRunesEffective(); got != 40 {
		t.Fatalf("custom latest user head runes = %d", got)
	}
	if got := custom.LatestUserMessageTailRunesEffective(); got != 60 {
		t.Fatalf("custom latest user tail runes = %d", got)
	}
}

func TestOrcaRouterProviderName(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"orcarouter", true},
		{"OrcaRouter", true},
		{"  orcarouter  ", true},
		{"openai", false},
		{"claude", false},
		{"", false},
	} {
		if got := IsOrcaRouterProviderName(tc.in); got != tc.want {
			t.Errorf("IsOrcaRouterProviderName(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOrcaRouterBaseURLFor(t *testing.T) {
	if got := OrcaRouterBaseURLFor("orcarouter"); got != OrcaRouterBaseURL {
		t.Fatalf("OrcaRouterBaseURLFor(orcarouter) = %q, want %q", got, OrcaRouterBaseURL)
	}
	if got := OrcaRouterBaseURLFor("openai"); got != "" {
		t.Fatalf("OrcaRouterBaseURLFor(openai) = %q, want empty", got)
	}
}

func TestOpenAIConfigIsOrcaRouterProvider(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  OpenAIConfig
		want bool
	}{
		{
			name: "provider name",
			cfg:  OpenAIConfig{Provider: "orcarouter", BaseURL: "https://custom.example/v1"},
			want: true,
		},
		{
			name: "base url contains orcarouter",
			cfg:  OpenAIConfig{Provider: "openai", BaseURL: "https://api.orcarouter.ai/v1"},
			want: true,
		},
		{
			name: "provider name case-insensitive",
			cfg:  OpenAIConfig{Provider: "OrcaRouter", BaseURL: ""},
			want: true,
		},
		{
			name: "unrelated provider",
			cfg:  OpenAIConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1"},
			want: false,
		},
	} {
		if got := tc.cfg.IsOrcaRouterProvider(); got != tc.want {
			t.Errorf("%s: IsOrcaRouterProvider() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
