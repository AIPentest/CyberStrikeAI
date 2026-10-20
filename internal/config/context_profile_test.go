package config

import (
	"reflect"
	"testing"
)

func TestTokenEstimateSafetyPercent(t *testing.T) {
	t.Parallel()
	if got := (OpenAIConfig{}).TokenEstimateSafetyPercent(); got != 100 {
		t.Fatalf("auto safety percent = %d, want 100", got)
	}
	if got := (OpenAIConfig{ContextProfile: "local"}).TokenEstimateSafetyPercent(); got != LocalTokenEstimateSafetyPercent {
		t.Fatalf("local safety percent = %d, want %d", got, LocalTokenEstimateSafetyPercent)
	}
}

func TestApplyExtraRequestFieldsKeepsCoreFields(t *testing.T) {
	t.Parallel()
	payload := map[string]interface{}{"model": "qwen3", "temperature": 0.1}
	ApplyExtraRequestFields(payload, map[string]interface{}{
		"model":                "override",
		"chat_template_kwargs": map[string]interface{}{"enable_thinking": false},
		"top_p":                0.9,
	})
	if payload["model"] != "qwen3" {
		t.Fatalf("core field overridden: %#v", payload)
	}
	if payload["top_p"] != 0.9 {
		t.Fatalf("extra field missing: %#v", payload)
	}
	if _, ok := payload["chat_template_kwargs"]; !ok {
		t.Fatalf("chat_template_kwargs missing: %#v", payload)
	}
	ApplyExtraRequestFields(nil, map[string]interface{}{"x": 1})
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

func TestEffectiveEinoMiddlewareForChannelLocalTightens(t *testing.T) {
	t.Parallel()
	mw := MultiAgentEinoMiddlewareConfig{
		SummarizationTriggerRatio:                  0.8,
		SummarizationOutputReserveTokens:           40960,
		ReductionMaxLengthForTrunc:                 100000,
		ReductionMaxTokensForClear:                 60000,
		SummarizationUserIntentLedgerMaxRunes:      96000,
		SummarizationUserIntentLedgerEntryMaxRunes: 16000,
		LatestUserMessageMaxRunes:                  48000,
		LatestUserMessageHeadRunes:                 24000,
		LatestUserMessageTailRunes:                 24000,
		ToolSearchAlwaysVisible:                    12,
		PlanExecuteMaxStepResultRunes:              4000,
		ToolSearchAlwaysVisibleTools: []string{
			"read_file", "glob", "grep", "analyze_image", "write_file", "edit_file",
			"execute", "task", "transfer_to_agent", "exit", "write_todos", "skill",
			"tool_search", "record_vulnerability", "http-framework-test", "webshell_exec",
		},
	}
	oa := OpenAIConfig{ContextWindow: 131072, ContextProfile: "local", MaxTotalTokens: 120000}
	out := EffectiveEinoMiddlewareForChannel(mw, oa)

	if out.SummarizationTriggerRatio != 0.8 {
		t.Fatalf("trigger ratio = %v, want 0.8", out.SummarizationTriggerRatio)
	}
	if out.SummarizationOutputReserveTokens != 4096 {
		t.Fatalf("output reserve = %d, want 4096", out.SummarizationOutputReserveTokens)
	}
	if out.ReductionMaxLengthForTrunc != 21845 {
		t.Fatalf("tool trunc bytes = %d, want 21845", out.ReductionMaxLengthForTrunc)
	}
	if out.ReductionMaxTokensForClear != 27525 {
		t.Fatalf("tool clear tokens = %d, want 27525", out.ReductionMaxTokensForClear)
	}
	if out.SummarizationUserIntentLedgerMaxRunes != 96000 {
		t.Fatalf("ledger runes = %d, want 96000", out.SummarizationUserIntentLedgerMaxRunes)
	}
	if out.SummarizationUserIntentLedgerEntryMaxRunes != 16000 {
		t.Fatalf("ledger entry runes = %d, want 16000", out.SummarizationUserIntentLedgerEntryMaxRunes)
	}
	if out.LatestUserMessageMaxRunes != 48000 {
		t.Fatalf("latest user runes = %d, want 48000", out.LatestUserMessageMaxRunes)
	}
	if out.ToolSearchMinTools != 8 || out.ToolSearchAlwaysVisible != 8 {
		t.Fatalf("tool search = (%d,%d), want (8,8)", out.ToolSearchMinTools, out.ToolSearchAlwaysVisible)
	}
	wantTools := []string{
		"read_file", "glob", "grep", "analyze_image", "write_file", "edit_file", "execute", "task",
		"transfer_to_agent", "exit", "write_todos", "skill", "tool_search",
		"record_vulnerability", "http-framework-test",
	}
	if !reflect.DeepEqual(out.ToolSearchAlwaysVisibleTools, wantTools) {
		t.Fatalf("always visible tools = %#v, want %#v", out.ToolSearchAlwaysVisibleTools, wantTools)
	}
	if out.ToolSearchIncludeBuiltinsEffective() {
		t.Fatal("local profile must let backend builtins go through tool_search")
	}
	if out.PlanExecuteMaxStepResultRunes != 2000 {
		t.Fatalf("plan step result runes = %d, want 2000", out.PlanExecuteMaxStepResultRunes)
	}
	if !out.ReductionEnable || !out.ReductionSubAgents {
		t.Fatal("local profile must enable reduction for main agent and sub-agents")
	}
}

func TestEffectiveEinoMiddlewareForChannelLocalNeverLoosens(t *testing.T) {
	t.Parallel()
	mw := MultiAgentEinoMiddlewareConfig{
		SummarizationTriggerRatio:                  0.6,
		SummarizationOutputReserveTokens:           1024,
		ReductionMaxLengthForTrunc:                 4096,
		ReductionMaxTokensForClear:                 2048,
		SummarizationUserIntentLedgerMaxRunes:      8000,
		SummarizationUserIntentLedgerEntryMaxRunes: 2000,
		LatestUserMessageMaxRunes:                  8000,
		LatestUserMessageHeadRunes:                 4000,
		LatestUserMessageTailRunes:                 4000,
		ToolSearchMinTools:                         4,
		ToolSearchAlwaysVisible:                    4,
		PlanExecuteMaxStepResultRunes:              1000,
	}
	oa := OpenAIConfig{ContextWindow: 131072, ContextProfile: "local"}
	out := EffectiveEinoMiddlewareForChannel(mw, oa)

	if out.SummarizationTriggerRatio != 0.6 ||
		out.SummarizationOutputReserveTokens != 1024 ||
		out.ReductionMaxLengthForTrunc != 4096 ||
		out.ReductionMaxTokensForClear != 2048 ||
		out.SummarizationUserIntentLedgerMaxRunes != 8000 ||
		out.SummarizationUserIntentLedgerEntryMaxRunes != 1333 ||
		out.LatestUserMessageMaxRunes != 8000 ||
		out.ToolSearchMinTools != 4 ||
		out.ToolSearchAlwaysVisible != 4 ||
		out.PlanExecuteMaxStepResultRunes != 1000 {
		t.Fatalf("local profile loosened explicit settings: %+v", out)
	}
}

func TestEffectiveEinoMiddlewareForChannelAutoUnchanged(t *testing.T) {
	t.Parallel()
	mw := MultiAgentEinoMiddlewareConfig{
		SummarizationTriggerRatio:    0.8,
		ToolSearchAlwaysVisibleTools: []string{"read_file", "webshell_exec"},
	}
	if got := EffectiveEinoMiddlewareForChannel(mw, OpenAIConfig{}); !reflect.DeepEqual(got, mw) {
		t.Fatalf("auto profile changed middleware: %+v", got)
	}
}

func TestWithEffectiveContextBudgetDoesNotMutateSource(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		OpenAI: OpenAIConfig{ContextWindow: 131072, ContextProfile: "local", MaxTotalTokens: 120000},
		MultiAgent: MultiAgentConfig{
			EinoMiddleware: MultiAgentEinoMiddlewareConfig{SummarizationTriggerRatio: 0.8},
		},
	}
	out := cfg.WithEffectiveContextBudget()
	if cfg.OpenAI.MaxTotalTokens != 120000 || cfg.MultiAgent.EinoMiddleware.SummarizationTriggerRatio != 0.8 {
		t.Fatalf("source config mutated: %+v", cfg)
	}
	if out.OpenAI.MaxTotalTokens != 78643 {
		t.Fatalf("effective budget = %d, want 78643", out.OpenAI.MaxTotalTokens)
	}
	if out.MultiAgent.EinoMiddleware.SummarizationTriggerRatio != 0.8 {
		t.Fatalf("effective trigger ratio = %v, want 0.8", out.MultiAgent.EinoMiddleware.SummarizationTriggerRatio)
	}
}
