package reasoning

import (
	"testing"

	"cyberstrike-ai/internal/config"
)

var reasoningPayloadKeysForTest = []string{"thinking", "reasoning_effort", "output_config", "reasoning"}

func TestEffortStringForAPI_passthrough(t *testing.T) {
	cases := map[string]string{
		"max":    "max",
		"xhigh":  "xhigh",
		"HIGH":   "high",
		"Medium": "medium",
	}
	for in, want := range cases {
		if got := effortStringForAPI(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeEffort_maxAndXhigh(t *testing.T) {
	if normalizeEffort("xhigh") != "xhigh" {
		t.Fatal("xhigh not accepted")
	}
	if normalizeEffort("max") != "max" {
		t.Fatal("max not accepted")
	}
}

func TestProviderExtraFields_openAICompatReasoningEffort(t *testing.T) {
	oa := &config.OpenAIConfig{
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
			Mode:    "on",
			Effort:  "high",
			ExtraRequestFields: map[string]interface{}{
				"vendor_option": true,
			},
		},
	}
	got := ProviderExtraFields(oa, nil)
	if got["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort=%#v, want high in %#v", got["reasoning_effort"], got)
	}
	if got["vendor_option"] != true {
		t.Fatalf("vendor option not preserved: %#v", got)
	}
}

func TestProviderExtraFields_reasoningOffPreservesUnrelatedFields(t *testing.T) {
	oa := &config.OpenAIConfig{
		Model: "gpt-4o-mini",
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
			Mode:    "off",
			Effort:  "high",
			ExtraRequestFields: map[string]interface{}{
				"reasoning_effort": "high",
				"thinking":         map[string]any{"type": "enabled"},
				"vendor_option":    true,
			},
		},
	}
	got := ProviderExtraFields(oa, nil)
	for _, key := range reasoningPayloadKeysForTest {
		if _, ok := got[key]; ok {
			t.Fatalf("agentic fields unexpectedly contain %q: %#v", key, got)
		}
	}
	if got["vendor_option"] != true {
		t.Fatalf("vendor option not preserved: %#v", got)
	}
}

func TestProviderExtraFields_openAICompatDeepseekModelOmitsAllReasoningFields(t *testing.T) {
	oa := &config.OpenAIConfig{
		Provider: "openai_compatible",
		BaseURL:  "http://your-gateway:port/v1",
		Model:    "deepseek-v4-flash-0731",
		Reasoning: config.OpenAIReasoningConfig{
			Mode:    "off",
			Effort:  "high",
			Profile: "openai_compat",
			ExtraRequestFields: map[string]interface{}{
				"thinking":         map[string]any{"type": "disabled"},
				"reasoning_effort": "high",
				"vendor_option":    true,
			},
		},
	}
	got := ProviderExtraFields(oa, nil)
	for _, key := range reasoningPayloadKeysForTest {
		if _, ok := got[key]; ok {
			t.Fatalf("agentic fields unexpectedly contain %q: %#v", key, got)
		}
	}
	if got["vendor_option"] != true {
		t.Fatalf("vendor option not preserved: %#v", got)
	}
}
