package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestEinoSummarizationModelOptionsLocalUsesMaxTokens(t *testing.T) {
	const outputReserve = 4096
	oa := &config.OpenAIConfig{ContextProfile: "local", ContextWindow: 131072}
	opts := newEinoSummarizationModelOptions(outputReserve, "qwen3-8b", "agentic", oa, nil)

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
		Model:      "qwen3-8b",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newNonEmptySummaryChatModel(chatModel).Generate(context.Background(), []*schema.Message{schema.UserMessage("summarize")}, opts...); err != nil {
		t.Fatal(err)
	}
	body := <-bodyCh
	if _, ok := body["max_completion_tokens"]; ok {
		t.Fatalf("local request contained max_completion_tokens: %#v", body)
	}
	if got, ok := body["max_tokens"].(float64); !ok || int(got) != outputReserve {
		t.Fatalf("max_tokens = %#v, want %d", body["max_tokens"], outputReserve)
	}
}

// Exercise both production factories and their real SDK options, not just a
// fake model: a truncated summary must be replaced only by a complete retry.
func TestSummaryOutputLimitConciseRetry(t *testing.T) {
	for _, kind := range []string{"classic", "agentic"} {
		for _, tc := range []struct {
			name        string
			firstReason string
			retryReason string
			firstText   string
			wantCalls   int
			wantErr     bool
		}{
			{"complete", "stop", "", "complete summary", 1, false},
			{"recovers", "length", "stop", "truncated summary", 2, false},
			{"reasoning_only_recovers", "length", "stop", "", 2, false},
			{"still_truncated", "length", "length", "truncated summary", 2, true},
			{"retry_filtered", "length", "content_filter", "truncated summary", 2, true},
			{"filtered", "content_filter", "", "filtered summary", 1, true},
			{"missing_completion", "", "", "partial summary", 1, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				bodies := make(chan map[string]any, 3)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					n := calls.Add(1)
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
						w.WriteHeader(400)
						return
					}
					bodies <- body
					if r.Header.Get("X-CyberStrike-Summarization") != "1" {
						t.Error("missing summary request header")
					}
					text, reason := tc.firstText, tc.firstReason
					if n > 1 {
						text, reason = "complete retry summary", tc.retryReason
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\n", text)
					fmt.Fprintf(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", reason)
					fmt.Fprint(w, "data: [DONE]\n\n")
				}))
				defer server.Close()
				oa := config.OpenAIConfig{Provider: "openai", APIKey: "test-key", BaseURL: server.URL, Model: "deepseek-chat", MaxCompletionTokens: 128}
				const reserve = 512
				opts := newEinoSummarizationModelOptions(reserve, oa.Model, kind, &oa, nil)
				ctx := context.Background()
				var err error
				var text string
				if kind == "classic" {
					base, modelErr := newEinoToolCallingChatModelFactory(server.Client(), nil, nil)(ctx, oa, einoModelModeNormal)
					if modelErr != nil {
						t.Fatal(modelErr)
					}
					input := []*schema.Message{schema.UserMessage("original conversation")}
					out, genErr := newNonEmptySummaryChatModel(base).Generate(ctx, input, opts...)
					err = genErr
					text = classicAssistantTextContent(out)
					if len(input) != 1 || input[0].Content != "original conversation" {
						t.Fatal("mutated original history")
					}
				} else {
					base, modelErr := newEinoAgenticChatModelFactory(server.Client(), nil, nil)(ctx, oa, einoModelModeNormal)
					if modelErr != nil {
						t.Fatal(modelErr)
					}
					input := []*schema.AgenticMessage{schema.UserAgenticMessage("original conversation")}
					out, genErr := newNonEmptyAgenticSummaryModel(base).Generate(ctx, input, opts...)
					err = genErr
					text = agenticAssistantTextContent(out)
					if len(input) != 1 {
						t.Fatal("mutated original history")
					}
				}
				if (err != nil) != tc.wantErr {
					t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
				}
				if tc.wantErr && text != "" {
					t.Fatalf("accepted partial summary %q", text)
				}
				if !tc.wantErr && tc.wantCalls == 2 && text != "complete retry summary" {
					t.Fatalf("summary = %q", text)
				}
				if int(calls.Load()) != tc.wantCalls {
					t.Fatalf("calls = %d, want %d", calls.Load(), tc.wantCalls)
				}
				for i := 0; i < tc.wantCalls; i++ {
					body := <-bodies
					if body["max_completion_tokens"] != float64(reserve) {
						t.Fatalf("output budget changed: %#v", body)
					}
					if _, ok := body["max_tokens"]; ok {
						t.Fatal("duplicate output limits")
					}
					if _, ok := body["reasoning_effort"]; ok {
						t.Fatal("reasoning enabled for summary")
					}
					msgs := body["messages"].([]any)
					if i == 1 {
						last := msgs[len(msgs)-1].(map[string]any)
						if last["role"] != "user" || !strings.Contains(fmt.Sprint(last["content"]), conciseSummaryRetryInstruction) {
							t.Fatalf("missing concise retry instruction: %#v", last)
						}
						if strings.Contains(fmt.Sprint(msgs), "truncated summary") {
							t.Fatal("partial response leaked into retry history")
						}
					}
				}
			})
		}
	}
}
