package multiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func TestEinoSummarizationSendsOneTokenLimit(t *testing.T) {
	for _, kind := range []string{"classic", "agentic"} {
		t.Run(kind, func(t *testing.T) {
			const outputReserve = 4096
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if _, exists := body["max_tokens"]; exists {
					t.Errorf("summary request includes max_tokens alongside max_completion_tokens: %s", body["max_tokens"])
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"message":"max_tokens and max_completion_tokens cannot be set at the same time","type":"invalid_request_error"}}`))
					return
				}
				if string(body["max_completion_tokens"]) != "4096" {
					t.Errorf("summary output budget = %s, want 4096", body["max_completion_tokens"])
				}
				_, _ = w.Write([]byte(`{"id":"summary","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"summary"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			ctx := context.Background()
			oa := &config.OpenAIConfig{Provider: "openai_compatible", Model: "test-model"}
			opts := newEinoSummarizationModelOptions(outputReserve, oa.Model, kind, oa, nil)
			defaultLimit := 8192
			if kind == "classic" {
				chat, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
					APIKey: "test", BaseURL: server.URL, Model: oa.Model, HTTPClient: server.Client(), MaxCompletionTokens: &defaultLimit,
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := chat.Generate(ctx, []*schema.Message{schema.UserMessage("summarize")}, opts...); err != nil {
					t.Fatal(err)
				}
			} else {
				chat, err := agenticopenai.NewChatModel(ctx, &agenticopenai.ChatConfig{
					APIKey: "test", BaseURL: server.URL, Model: oa.Model, HTTPClient: server.Client(), MaxCompletionTokens: &defaultLimit,
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := chat.Generate(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("summarize")}, opts...); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestEinoSummarizationPreservesClaudeTokenLimit(t *testing.T) {
	for _, kind := range []string{"classic", "agentic"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if string(body["max_tokens"]) != "4096" {
					t.Errorf("Claude summary budget = %s, want 4096", body["max_tokens"])
				}
				if _, exists := body["max_completion_tokens"]; exists {
					t.Error("Claude request includes OpenAI token limit")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"summary","type":"message","role":"assistant","model":"claude-sonnet","content":[{"type":"text","text":"summary"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer server.Close()
			ctx := context.Background()
			oa := config.OpenAIConfig{Provider: "claude", APIKey: "test", BaseURL: server.URL, Model: "claude-sonnet", MaxCompletionTokens: 8192}
			opts := newEinoSummarizationModelOptions(4096, oa.Model, kind, &oa, nil)
			if kind == "classic" {
				chat, err := newEinoToolCallingChatModelFactory(server.Client(), nil, nil)(ctx, oa, einoModelModeNormal)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := chat.Generate(ctx, []*schema.Message{schema.UserMessage("summarize")}, opts...); err != nil {
					t.Fatal(err)
				}
			} else {
				chat, err := newEinoAgenticChatModelFactory(server.Client(), nil, nil)(ctx, oa, einoModelModeNormal)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := chat.Generate(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("summarize")}, opts...); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
