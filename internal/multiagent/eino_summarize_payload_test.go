package multiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
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

func TestEinoSummarizationModelOptionsSetOnlyMaxCompletionTokens(t *testing.T) {
	const outputReserve = 40960
	opts := newEinoSummarizationModelOptions(outputReserve, "minimax-m3", "agentic", nil, nil)
	common := model.GetCommonOptions(nil, opts...)
	if common != nil && common.MaxTokens != nil {
		t.Fatalf("common max_tokens = %d, want unset", *common.MaxTokens)
	}

	bodyCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodyCh <- body
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"summary\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	chatModel, err := einoopenai.NewChatModel(context.Background(), &einoopenai.ChatModelConfig{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "gpt-4o",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := newNonEmptySummaryChatModel(chatModel).Generate(context.Background(), []*schema.Message{schema.UserMessage("summarize")}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.Content) != "summary" {
		t.Fatalf("summary content = %q", out.Content)
	}

	body := <-bodyCh
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("request contained max_tokens: %#v", body)
	}
	if got, ok := body["max_completion_tokens"].(float64); !ok || int(got) != outputReserve {
		t.Fatalf("max_completion_tokens = %#v, want %d", body["max_completion_tokens"], outputReserve)
	}
}
