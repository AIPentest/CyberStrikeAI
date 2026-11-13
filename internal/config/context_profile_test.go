package config

import (
	"reflect"
	"testing"
)

func TestEffectiveMaxTotalTokensNeverExceedsWindow(t *testing.T) {
	t.Parallel()
	for _, window := range []int{512, 1024, 2048, 4096, 8192} {
		oa := OpenAIConfig{ContextWindow: window, ContextProfile: "local", MaxTotalTokens: 120000}
		if got := oa.EffectiveMaxTotalTokens(); got > window {
			t.Fatalf("window %d: effective budget %d exceeds window", window, got)
		}
	}
}

func TestOutputLimitField(t *testing.T) {
	t.Parallel()
	if got := (OpenAIConfig{}).OutputLimitField(); got != "max_completion_tokens" {
		t.Fatalf("auto output field = %q", got)
	}
	if got := (OpenAIConfig{ContextProfile: "local"}).OutputLimitField(); got != "max_tokens" {
		t.Fatalf("local output field = %q", got)
	}
}

func TestEffectiveMaxTotalTokens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		oa   OpenAIConfig
		want int
	}{
		{
			name: "unknown window keeps configured",
			oa:   OpenAIConfig{MaxTotalTokens: 64000},
			want: 64000,
		},
		{
			name: "unknown window default 120k",
			oa:   OpenAIConfig{},
			want: 120000,
		},
		{
			name: "remote window reserves output",
			oa:   OpenAIConfig{ContextWindow: 131072, MaxTotalTokens: 120000},
			want: 114688,
		},
		{
			name: "remote explicit smaller budget wins",
			oa:   OpenAIConfig{ContextWindow: 131072, MaxTotalTokens: 64000},
			want: 64000,
		},
		{
			name: "local 128k uses 60 percent",
			oa:   OpenAIConfig{ContextWindow: 131072, ContextProfile: "local", MaxTotalTokens: 120000},
			want: 78643,
		},
		{
			name: "local explicit smaller budget wins",
			oa:   OpenAIConfig{ContextWindow: 131072, ContextProfile: "local", MaxTotalTokens: 20000},
			want: 20000,
		},
		{
			name: "local 32k reserves half for output",
			oa:   OpenAIConfig{ContextWindow: 32768, ContextProfile: "local", MaxTotalTokens: 120000},
			want: 16384,
		},
		{
			name: "local 32k keeps configured output",
			oa:   OpenAIConfig{ContextWindow: 32768, ContextProfile: "local", MaxTotalTokens: 120000, MaxCompletionTokens: 4096},
			want: 19660,
		},
		{
			name: "local missing window assumes 32k",
			oa:   OpenAIConfig{ContextProfile: "local", MaxTotalTokens: 120000},
			want: 16384,
		},
		{
			name: "local 256k caps budget",
			oa:   OpenAIConfig{ContextWindow: 262144, ContextProfile: "local", MaxTotalTokens: 200000},
			want: 131072,
		},
		{
			name: "local 8k stays within window",
			oa:   OpenAIConfig{ContextWindow: 8192, ContextProfile: "local", MaxTotalTokens: 120000},
			want: 4096,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.oa.EffectiveMaxTotalTokens(); got != tc.want {
				t.Fatalf("EffectiveMaxTotalTokens() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMaxCompletionTokensEffectiveClampsToWindow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		oa   OpenAIConfig
		want int
	}{
		{name: "no window uses configured", oa: OpenAIConfig{MaxCompletionTokens: 32768}, want: 32768},
		{name: "no window uses default", oa: OpenAIConfig{}, want: DefaultMaxCompletionTokens},
		{name: "32k window halves output", oa: OpenAIConfig{ContextWindow: 32768, MaxCompletionTokens: 32768}, want: 16384},
		{name: "local missing window assumes 32k", oa: OpenAIConfig{ContextProfile: "local", MaxCompletionTokens: 32768}, want: 16384},
		{name: "128k window keeps output", oa: OpenAIConfig{ContextWindow: 131072, MaxCompletionTokens: 16384}, want: 16384},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.oa.MaxCompletionTokensEffective(); got != tc.want {
				t.Fatalf("MaxCompletionTokensEffective() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestContextBudgetWarnings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		oa   OpenAIConfig
		want []string
	}{
		{
			name: "no window no warning",
			oa:   OpenAIConfig{MaxTotalTokens: 120000},
		},
		{
			name: "local missing window warns twice",
			oa:   OpenAIConfig{ContextProfile: "local", MaxTotalTokens: 120000},
			want: []string{ContextWarningLocalWindowUnknown, ContextWarningMaxTotalClamped},
		},
		{
			name: "local tiny window warns",
			oa:   OpenAIConfig{ContextProfile: "local", ContextWindow: 8192},
			want: []string{ContextWarningLocalWindowSmall, ContextWarningMaxTotalClamped},
		},
		{
			name: "remote oversized budget clamps",
			oa:   OpenAIConfig{ContextWindow: 32768, MaxTotalTokens: 64000},
			want: []string{ContextWarningMaxTotalClamped},
		},
		{
			name: "oversized output cap warns",
			oa:   OpenAIConfig{ContextWindow: 32768, MaxCompletionTokens: 32768},
			want: []string{ContextWarningMaxTotalClamped, ContextWarningMaxCompletionClamped},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.oa.ContextBudgetWarnings(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ContextBudgetWarnings() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestLocalRuntimeBudgetsPreserveTighterSettings(t *testing.T) {
	oa := OpenAIConfig{ContextWindow: 131072, ContextProfile: "local"}
	original := AgentTuningConfig{ReductionMaxLengthForTrunc: 100000, ToolSearchAlwaysVisible: 12}
	got := EffectiveRuntimeForChannel(original, oa)
	if !got.ReductionEnable || got.ReductionMaxLengthForTrunc != 21845 || !got.ToolSearchEnable || got.ToolSearchMinTools != 8 || got.ToolSearchAlwaysVisible != 8 || got.ToolSearchIncludeBuiltinsEffective() {
		t.Fatalf("unexpected local tuning: %+v", got)
	}
	if original.ReductionEnable || original.ToolSearchEnable {
		t.Fatal("runtime tuning mutated the saved config")
	}
	got.ReductionMaxLengthForTrunc, got.ToolSearchMinTools, got.ToolSearchAlwaysVisible = 2048, 4, 2
	if again := EffectiveRuntimeForChannel(got, oa); !reflect.DeepEqual(got, again) {
		t.Fatal("tighter settings were changed")
	}
}

func TestEffectiveAgentMiddlewareForChannelAutoUnchanged(t *testing.T) {
	t.Parallel()
	mw := AgentTuningConfig{
		SummarizationTriggerRatio:    0.8,
		ToolSearchAlwaysVisibleTools: []string{"read_file", "webshell_exec"},
	}
	if got := EffectiveRuntimeForChannel(mw, OpenAIConfig{}); !reflect.DeepEqual(got, mw) {
		t.Fatalf("auto profile changed middleware: %+v", got)
	}
}

func TestEconomyContextProfileSelectionAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name string
		oa   OpenAIConfig
		want bool
	}{
		{"official default", OpenAIConfig{BaseURL: "https://api.deepseek.com/v1"}, true},
		{"gateway explicit profile", OpenAIConfig{Reasoning: OpenAIReasoningConfig{Profile: "deepseek_compat"}}, true},
		{"gateway model name", OpenAIConfig{Model: "deepseek-flash", BaseURL: "https://gateway.example/v1"}, true},
		{"namespaced model", OpenAIConfig{Model: "deepseek-ai/DeepSeek-V4", Reasoning: OpenAIReasoningConfig{Profile: "openai_compat"}}, true},
		{"other model", OpenAIConfig{Model: "gpt-example", BaseURL: "https://gateway.example/v1"}, false},
		{"gateway custom opt out", OpenAIConfig{Model: "deepseek-flash", ContextProfile: "custom"}, false},
		{"lookalike host", OpenAIConfig{BaseURL: "https://api.deepseek.com.example/v1"}, false},
		{"custom opt out", OpenAIConfig{BaseURL: "https://api.deepseek.com", ContextProfile: "custom"}, false},
		{"local opt in", OpenAIConfig{BaseURL: "https://api.deepseek.com", ContextProfile: "local"}, false},
		{"detected local DeepSeek", OpenAIConfig{Model: "deepseek-flash", ContextProfile: "local_auto"}, false},
		{"economy opt in", OpenAIConfig{ContextProfile: " ECONOMY "}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.oa.IsEconomyContextProfile(); got != tc.want {
				t.Fatalf("economy=%v, want %v", got, tc.want)
			}
		})
	}
	includeBuiltins := true
	mw := AgentTuningConfig{ToolSearchMinTools: 20, ToolSearchAlwaysVisible: 12, ToolSearchIncludeBuiltins: &includeBuiltins,
		ToolSearchAlwaysVisibleTools: []string{"record_vulnerability"}, ReductionMaxLengthForTrunc: 12000}
	oa := OpenAIConfig{BaseURL: "https://api.deepseek.com/v1", MaxTotalTokens: 120000, MaxCompletionTokens: 32768,
		Reasoning: OpenAIReasoningConfig{Mode: "on", Effort: "high"}}
	out := EffectiveRuntimeForChannel(mw, oa)
	if !out.ToolSearchEnable || out.ToolSearchMinTools != 16 || out.ToolSearchAlwaysVisible != 6 || out.ToolSearchIncludeBuiltinsEffective() || !out.ReductionEnable || out.ReductionMaxLengthForTrunc != 8000 {
		t.Fatalf("unexpected economy runtime: %+v", out)
	}
	if !reflect.DeepEqual(out.ToolSearchAlwaysVisibleTools, mw.ToolSearchAlwaysVisibleTools) || !*mw.ToolSearchIncludeBuiltins || mw.ToolSearchEnable {
		t.Fatal("explicit visible tools or original config modified")
	}
	if again := EffectiveRuntimeForChannel(out, oa); !reflect.DeepEqual(out, again) {
		t.Fatal("economy profile is not idempotent")
	}
	out.ToolSearchMinTools, out.ToolSearchAlwaysVisible, out.ReductionMaxLengthForTrunc = 4, 2, 2048
	if again := EffectiveRuntimeForChannel(out, oa); !reflect.DeepEqual(out, again) {
		t.Fatal("economy profile loosened tighter limits")
	}
	for _, profile := range []string{"auto", "economy", "custom", "local", "local_auto"} {
		if err := ValidateContextSettings("channel", 0, profile); err != nil {
			t.Fatal(err)
		}
	}
	oa.ContextProfile = "custom"
	if got := EffectiveRuntimeForChannel(mw, oa); !reflect.DeepEqual(got, mw) {
		t.Fatal("custom profile did not preserve runtime settings")
	}
}

func TestDetectedLocalProfileUsesLocalBudgets(t *testing.T) {
	manual := OpenAIConfig{ContextProfile: "local", ContextWindow: 65536, MaxCompletionTokens: 8192}
	detected := manual
	detected.ContextProfile = "local_auto"
	if !detected.IsLocalContextProfile() || detected.EffectiveMaxTotalTokens() != manual.EffectiveMaxTotalTokens() || detected.OutputLimitField() != "max_tokens" {
		t.Fatalf("detected backend lost local limits: %+v", detected)
	}
	if !reflect.DeepEqual(EffectiveRuntimeForChannel(AgentTuningConfig{}, detected), EffectiveRuntimeForChannel(AgentTuningConfig{}, manual)) {
		t.Fatal("detected backend lost local runtime tuning")
	}
}
