package multiagent

import (
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/components/model"
)

func TestStripReasoningFromSummarizationPayload(t *testing.T) {
	in := []byte(`{"model":"deepseek-chat","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
	out, err := stripReasoningFromSummarizationPayload(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "thinking") || strings.Contains(s, "reasoning_effort") {
		t.Fatalf("expected reasoning fields stripped, got %s", s)
	}
	if !strings.Contains(s, `"model":"deepseek-chat"`) {
		t.Fatalf("expected model preserved, got %s", s)
	}

	plain := []byte(`{"model":"gpt-4o","messages":[]}`)
	out2, err := stripReasoningFromSummarizationPayload(plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out2) != string(plain) {
		t.Fatalf("expected unchanged payload, got %s", out2)
	}
}

func TestStripReasoningFromSummarizationPayloadDisablesDeepSeekThinking(t *testing.T) {
	in := []byte(`{"model":"deepseek-v4-flash","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
	oa := &config.OpenAIConfig{
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-v4-flash",
	}
	out, err := stripReasoningFromSummarizationPayload(in, oa)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "reasoning_effort") {
		t.Fatalf("expected reasoning_effort stripped, got %s", s)
	}
	if !strings.Contains(s, `"thinking":{"type":"disabled"}`) {
		t.Fatalf("expected DeepSeek thinking disabled, got %s", s)
	}
}

func TestStripReasoningFromSummarizationPayloadDisablesDeepSeekEndpointEvenWithOpenAICompatProfile(t *testing.T) {
	in := []byte(`{"model":"deepseek-v4-flash","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
	oa := &config.OpenAIConfig{
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-v4-flash",
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
		},
	}
	out, err := stripReasoningFromSummarizationPayload(in, oa)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "reasoning_effort") {
		t.Fatalf("expected reasoning_effort stripped, got %s", s)
	}
	if !strings.Contains(s, `"thinking":{"type":"disabled"}`) {
		t.Fatalf("expected official DeepSeek endpoint thinking disabled, got %s", s)
	}
}

func TestStripReasoningFromSummarizationPayloadHonorsOpenAICompatProfileForNonDeepSeekEndpoint(t *testing.T) {
	in := []byte(`{"model":"deepseek-v4-flash","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
	oa := &config.OpenAIConfig{
		BaseURL: "https://compatible.example.com/v1",
		Model:   "deepseek-v4-flash",
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
		},
	}
	out, err := stripReasoningFromSummarizationPayload(in, oa)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "thinking") || strings.Contains(s, "reasoning_effort") {
		t.Fatalf("expected non-DeepSeek OpenAI-compatible endpoint to strip reasoning fields, got %s", s)
	}
}

func TestEinoSummarizationModelOptionsSetClaudeCommonMaxTokens(t *testing.T) {
	const outputReserve = 4096
	opts := newEinoSummarizationModelOptions(outputReserve, "claude-sonnet", "agentic", &config.OpenAIConfig{Provider: "claude"}, nil)
	common := model.GetCommonOptions(nil, opts...)
	if common == nil || common.MaxTokens == nil {
		t.Fatal("expected summarization options to set common max_tokens")
	}
	if *common.MaxTokens != outputReserve {
		t.Fatalf("max_tokens = %d, want %d", *common.MaxTokens, outputReserve)
	}
}
