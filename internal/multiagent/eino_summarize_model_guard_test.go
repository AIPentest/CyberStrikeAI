package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/schema/claude"
	schemaopenai "github.com/cloudwego/eino/schema/openai"
)

type guardClassicSummaryModel struct {
	out *schema.Message
}

func (m *guardClassicSummaryModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.out, nil
}

func (m *guardClassicSummaryModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{m.out}), nil
}

func TestNonEmptySummaryChatModelReportsEmptyContentDiagnostics(t *testing.T) {
	msg := schema.AssistantMessage("", nil)
	msg.ReasoningContent = "只返回了思考，没有最终摘要"
	msg.ResponseMeta = &schema.ResponseMeta{
		FinishReason: "stop",
		Usage: &schema.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 3,
			TotalTokens:      13,
			CompletionTokensDetails: schema.CompletionTokensDetails{
				ReasoningTokens: 3,
			},
		},
	}
	_, err := newNonEmptySummaryChatModel(&guardClassicSummaryModel{out: msg}).Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected empty summary error")
	}
	text := err.Error()
	for _, want := range []string{
		"summary content is empty",
		"reasoning_runes=",
		`finish_reason="stop"`,
		"reasoning_tokens=3",
		"DeepSeek thinking",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("error missing %q:\n%s", want, text)
		}
	}
}

type guardAgenticSummaryModel struct {
	out *schema.AgenticMessage
}

func (m *guardAgenticSummaryModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return m.out, nil
}

func (m *guardAgenticSummaryModel) Stream(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return schema.StreamReaderFromArray([]*schema.AgenticMessage{m.out}), nil
}

func TestNonEmptyAgenticSummaryModelReportsEmptyContentDiagnostics(t *testing.T) {
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{Text: "只返回了思考，没有最终摘要"}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{
			TokenUsage: &schema.TokenUsage{
				PromptTokens:     10,
				CompletionTokens: 3,
				TotalTokens:      13,
				CompletionTokensDetails: schema.CompletionTokensDetails{
					ReasoningTokens: 3,
				},
			},
		},
	}
	_, err := newNonEmptyAgenticSummaryModel(&guardAgenticSummaryModel{out: msg}).Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected empty summary error")
	}
	text := err.Error()
	for _, want := range []string{
		"summary content is empty",
		"reasoning_runes=",
		"reasoning_blocks=1",
		"reasoning_tokens=3",
		"DeepSeek thinking",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("error missing %q:\n%s", want, text)
		}
	}
}

// Generate deliberately reproduces the SDK rejection; summaries must use Stream.
type streamingSummaryTestModel[T any] struct {
	stream *schema.StreamReader[T]
	err    error
	input  []T
	opts   []model.Option
}

func (m *streamingSummaryTestModel[T]) Generate(context.Context, []T, ...model.Option) (T, error) {
	var zero T
	return zero, errors.New("streaming is required for operations that may take longer than 10 minutes")
}
func (m *streamingSummaryTestModel[T]) Stream(_ context.Context, input []T, opts ...model.Option) (*schema.StreamReader[T], error) {
	m.input, m.opts = input, opts
	return m.stream, m.err
}

func TestSummaryGenerateUsesStream(t *testing.T) {
	t.Run("classic", func(t *testing.T) {
		tail := schema.AssistantMessage("摘要", nil)
		tail.ResponseMeta = &schema.ResponseMeta{FinishReason: "stop", Usage: &schema.TokenUsage{TotalTokens: 42}}
		base := &streamingSummaryTestModel[*schema.Message]{stream: schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("完整", nil), nil, tail})}
		input := []*schema.Message{schema.UserMessage("history")}
		out, err := newNonEmptySummaryChatModel(base).Generate(context.Background(), input, model.WithMaxTokens(64000))
		if err != nil {
			t.Fatal(err)
		}
		if out.Content != "完整摘要" || out.ResponseMeta.Usage.TotalTokens != 42 {
			t.Fatalf("lost streamed content or usage: %+v", out)
		}
		if base.input[0] != input[0] || *model.GetCommonOptions(nil, base.opts...).MaxTokens != 64000 {
			t.Fatal("input/options not forwarded")
		}
	})
	t.Run("agentic", func(t *testing.T) {
		chunk := func(text string) *schema.AgenticMessage {
			block := schema.NewContentBlock(&schema.AssistantGenText{Text: text})
			block.StreamingMeta = &schema.StreamingMeta{Index: 0}
			return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{block}}
		}
		tail := chunk("摘要")
		tail.ResponseMeta = &schema.AgenticResponseMeta{ClaudeExtension: &claude.ResponseMetaExtension{StopReason: "end_turn"}, TokenUsage: &schema.TokenUsage{TotalTokens: 42}}
		base := &streamingSummaryTestModel[*schema.AgenticMessage]{stream: schema.StreamReaderFromArray([]*schema.AgenticMessage{chunk("完整"), nil, tail})}
		input := []*schema.AgenticMessage{chunk("history")}
		out, err := newNonEmptyAgenticSummaryModel(base).Generate(context.Background(), input, model.WithMaxTokens(64000))
		if err != nil {
			t.Fatal(err)
		}
		if agenticAssistantTextContent(out) != "完整摘要" || out.ResponseMeta.TokenUsage.TotalTokens != 42 {
			t.Fatalf("lost streamed content or usage: %+v", out)
		}
		if base.input[0] != input[0] || *model.GetCommonOptions(nil, base.opts...).MaxTokens != 64000 {
			t.Fatal("input/options not forwarded")
		}
	})
}

func TestSummaryStreamFailuresDoNotReturnPartialSummary(t *testing.T) {
	failure := errors.New("connection reset")
	for _, kind := range []string{"start", "receive", "empty", "nil", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			base := &streamingSummaryTestModel[*schema.Message]{}
			switch kind {
			case "start":
				base.err = failure
			case "receive":
				reader, writer := schema.Pipe[*schema.Message](2)
				writer.Send(schema.AssistantMessage("partial", nil), nil)
				writer.Send(nil, failure)
				writer.Close()
				base.stream = reader
			case "empty":
				base.stream = schema.StreamReaderFromArray([]*schema.Message{})
			case "cancel":
				base.stream = schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("partial", nil)})
				cancel()
			}
			out, err := newNonEmptySummaryChatModel(base).Generate(ctx, nil)
			if err == nil || out != nil {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			var wrapped *einoSummarizationModelError
			if !errors.As(err, &wrapped) {
				t.Fatalf("missing summary error wrapper: %v", err)
			}
			if (kind == "start" || kind == "receive") && !errors.Is(err, failure) {
				t.Fatal("lost original error")
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation")
			}
		})
	}
}

func TestClaudeSummaryLargeBudgetStreamsThroughNativeSDK(t *testing.T) {
	for _, mode := range []string{"complete", "truncated", "early_eof", "cancel_read"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream    bool `json:"stream"`
					MaxTokens int  `json:"max_tokens"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if !body.Stream || body.MaxTokens != 64000 {
					t.Errorf("unexpected request: %+v", body)
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				events := []string{
					`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
					`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"完整"}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"摘要"}}`,
					`{"type":"content_block_stop","index":0}`,
					`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
					`{"type":"message_stop"}`,
				}
				switch mode {
				case "truncated":
					events[5] = strings.ReplaceAll(events[5], "end_turn", "max_tokens")
				case "early_eof", "cancel_read":
					events = events[:4]
				}
				for _, event := range events {
					var header struct {
						Type string `json:"type"`
					}
					_ = json.Unmarshal([]byte(event), &header)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", header.Type, event)
				}
				if mode == "cancel_read" {
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			factory := newEinoAgenticChatModelFactory(server.Client(), nil, nil)
			native, err := factory(ctx, config.OpenAIConfig{Provider: "claude", APIKey: "test-key", BaseURL: server.URL, Model: "claude-sonnet-4-20250514"}, einoModelModeNormal)
			if err != nil {
				t.Fatal(err)
			}
			input := EinoMessagesToAgentic([]*schema.Message{schema.UserMessage("summarize history")})
			opts := newEinoSummarizationModelOptions(64000, "claude-sonnet-4-20250514", "agentic", nil, nil)
			if _, err = native.Generate(ctx, input, opts...); err == nil || !strings.Contains(err.Error(), "streaming is required") {
				t.Fatalf("expected original SDK rejection, got %v", err)
			}
			if mode == "cancel_read" {
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
				// Bound the test even if cancellation stops propagating to the HTTP request.
				timer := time.AfterFunc(5*time.Second, cancel)
				defer timer.Stop()
			}
			out, err := newNonEmptyAgenticSummaryModel(native).Generate(ctx, input, opts...)
			if mode != "complete" {
				if out != nil || err == nil {
					t.Fatalf("accepted partial summary: out=%+v err=%v", out, err)
				}
				if mode == "cancel_read" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if agenticAssistantTextContent(out) != "完整摘要" {
				t.Fatalf("unexpected summary: %+v", out)
			}
			if out.ResponseMeta == nil || out.ResponseMeta.TokenUsage == nil || out.ResponseMeta.TokenUsage.CompletionTokens != 2 {
				t.Fatalf("missing usage: %+v", out.ResponseMeta)
			}
		})
	}
}

func TestSummaryCompletionValidation(t *testing.T) {
	for _, reason := range []string{"stop", "", "length", "content_filter", "tool_calls", "unknown"} {
		t.Run("classic/"+reason, func(t *testing.T) {
			msg := schema.AssistantMessage("partial or complete summary", nil)
			msg.ResponseMeta = &schema.ResponseMeta{FinishReason: reason}
			out, err := newNonEmptySummaryChatModel(&guardClassicSummaryModel{out: msg}).Generate(context.Background(), nil)
			if reason == "stop" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if out != nil || err == nil {
				t.Fatalf("out=%+v err=%v", out, err)
			}
		})
	}
	for _, reason := range []string{"end_turn", "", "max_tokens", "stop_sequence", "tool_use", "pause_turn", "refusal"} {
		t.Run("claude/"+reason, func(t *testing.T) {
			msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "summary"})},
				ResponseMeta:  &schema.AgenticResponseMeta{ClaudeExtension: &claude.ResponseMetaExtension{StopReason: reason}}}
			out, err := newNonEmptyAgenticSummaryModel(&guardAgenticSummaryModel{out: msg}).Generate(context.Background(), nil)
			if reason == "end_turn" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if out != nil || err == nil {
				t.Fatalf("out=%+v err=%v", out, err)
			}
		})
	}
	for _, status := range []schemaopenai.ResponseStatus{"completed", "incomplete", "failed", "cancelled", "in_progress", ""} {
		t.Run("openai/"+string(status), func(t *testing.T) {
			msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "summary"})},
				ResponseMeta:  &schema.AgenticResponseMeta{OpenAIExtension: &schemaopenai.ResponseMetaExtension{Status: status}}}
			out, err := newNonEmptyAgenticSummaryModel(&guardAgenticSummaryModel{out: msg}).Generate(context.Background(), nil)
			if status == "completed" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if out != nil || err == nil {
				t.Fatalf("out=%+v err=%v", out, err)
			}
		})
	}
}

// Exercise the actual Chat Completions adapter: its finish reason is stored in
// ResponseMeta.Extension, unlike the OpenAI Responses API's OpenAIExtension.
func TestOpenAIChatSummaryStreamCompletion(t *testing.T) {
	for _, reason := range []string{"stop", "length", ""} {
		t.Run(reason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Stream {
					t.Errorf("expected streamed request: %+v, %v", body, err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"summary\"},\"finish_reason\":null}]}\n\n")
				if reason != "" {
					fmt.Fprintf(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", reason)
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}))
			defer server.Close()
			factory := newEinoAgenticChatModelFactory(server.Client(), nil, nil)
			native, err := factory(context.Background(), config.OpenAIConfig{Provider: "openai", APIKey: "test-key", BaseURL: server.URL, Model: "gpt-4o"}, einoModelModeNormal)
			if err != nil {
				t.Fatal(err)
			}
			out, err := newNonEmptyAgenticSummaryModel(native).Generate(context.Background(), []*schema.AgenticMessage{schema.UserAgenticMessage("summarize")})
			if reason == "stop" {
				if err != nil || agenticAssistantTextContent(out) != "summary" {
					t.Fatalf("out=%+v err=%v", out, err)
				}
			} else if err == nil || out != nil {
				t.Fatalf("accepted incomplete summary: out=%+v err=%v", out, err)
			}
		})
	}
}
