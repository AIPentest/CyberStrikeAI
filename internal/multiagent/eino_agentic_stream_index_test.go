package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Replay the screenshot's four TaskCreate calls followed by exec, with text
// deltas between calls as emitted by some OpenAI-compatible gateways. Exercise
// the production HTTP transport, SDK and AgenticMessage converter together.
func TestEinoAgenticFactoryInterleavedToolStream(t *testing.T) {
	for _, provider := range []string{"openai", "openai_compatible"} {
		for _, mode := range []einoModelMode{einoModelModeNormal, einoModelModePlanner} {
			t.Run(provider+"/"+string(mode), func(t *testing.T) {
				t.Parallel()
				client := &http.Client{Transport: reasoningWireTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader(interleavedToolSSE(t))),
						Request:    r,
					}, nil
				})}
				factory := newEinoAgenticChatModelFactory(client, nil, nil)
				m, err := factory(context.Background(), config.OpenAIConfig{
					Provider: provider, APIKey: "test-key", BaseURL: "https://gateway.example/v1", Model: "test-model",
				}, mode)
				if err != nil {
					t.Fatal(err)
				}
				stream, err := m.Stream(context.Background(), EinoMessagesToAgentic([]*schema.Message{schema.UserMessage("plan and run")}))
				if err != nil {
					t.Fatal(err)
				}
				msg := concatAgenticTestStream(t, stream)
				if isRetryableEmptyAgenticModelOutput(msg) {
					t.Fatal("tool-call response was classified as empty")
				}
				if len(msg.ContentBlocks) != 11 {
					t.Fatalf("blocks = %d, want initial text + five calls and intervening text", len(msg.ContentBlocks))
				}
				for i := 0; i < 5; i++ {
					call := msg.ContentBlocks[2*i+1].FunctionToolCall
					name := "TaskCreate"
					if i == 4 {
						name = "exec"
					}
					if call == nil || call.CallID != fmt.Sprintf("call_%d", i) || call.Name != name || call.Arguments != fmt.Sprintf(`{"step":%d}`, i) {
						t.Fatalf("call %d = %#v", i, call)
					}
					if text := msg.ContentBlocks[2*i+2].AssistantGenText; text == nil || text.Text != fmt.Sprintf("\nafter %d\n", i) {
						t.Fatalf("interleaved text %d = %#v", i, text)
					}
				}
			})
		}
	}
}

func interleavedToolSSE(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	write := func(delta map[string]any, finish any) {
		t.Helper()
		frame, err := json.Marshal(map[string]any{
			"id": "test-response", "object": "chat.completion.chunk", "model": "test-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "data: %s\n\n", frame)
	}
	write(map[string]any{"role": "assistant", "content": "Planning\n"}, nil)
	for i := 0; i < 5; i++ {
		name := "TaskCreate"
		if i == 4 {
			name = "exec"
		}
		write(map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
			"function": map[string]any{"name": name, "arguments": `{"step":`},
		}}}, nil)
		write(map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "function": map[string]any{"arguments": fmt.Sprintf("%d}", i)},
		}}}, nil)
		write(map[string]any{"content": fmt.Sprintf("\nafter %d\n", i)}, nil)
	}
	write(map[string]any{}, "tool_calls")
	out.WriteString("data: [DONE]\n\n")
	return out.String()
}

func concatAgenticTestStream(t *testing.T, stream *schema.StreamReader[*schema.AgenticMessage]) *schema.AgenticMessage {
	t.Helper()
	defer stream.Close()
	var frames []*schema.AgenticMessage
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	msg, err := schema.ConcatAgenticMessages(frames)
	if err != nil {
		t.Fatalf("ConcatAgenticMessages: %v", err)
	}
	return msg
}

// Run the real ADK retry checker and tool loop, not just schema concat. The raw
// backend reproduces the misleading rejection; the production factory executes
// every call exactly once and sends all five results to the next model turn.
func TestEinoAgenticInterleavedStreamNativeRetry(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprintf("repair=%t", repair), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sse := interleavedToolSSE(t)
			var requests atomic.Int32
			client := &http.Client{Transport: reasoningWireTransport(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				var body struct {
					Messages []struct {
						Role       string `json:"role"`
						ToolCallID string `json:"tool_call_id"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				results := map[string]bool{}
				for _, msg := range body.Messages {
					if msg.Role == "tool" {
						results[msg.ToolCallID] = true
					}
				}
				response := sse
				if len(results) == 5 {
					response = "data: {\"id\":\"final\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"completed\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
				}
				return &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body:   io.NopCloser(strings.NewReader(response)), Request: r,
				}, nil
			})}
			m, err := newEinoAgenticChatModelFactory(client, nil, nil)(ctx, config.OpenAIConfig{
				Provider: "openai_compatible", APIKey: "test-key", BaseURL: "https://gateway.example/v1", Model: "test-model",
			}, einoModelModeNormal)
			if err != nil {
				t.Fatal(err)
			}
			if !repair {
				m = m.(*agenticStreamIndexRepairModel).base
			}
			var calls [5]atomic.Int32
			retry := newEinoAgenticModelRetryConfig(&config.MultiAgentEinoMiddlewareConfig{ModelRetryMaxRetries: 1}, nil, "test")
			retry.BackoffFunc = func(context.Context, int) time.Duration { return time.Nanosecond }
			a, err := newEinoAgenticChatModelAgent(ctx, einoAgenticChatModelAgentConfig{
				Name: "stream-regression", Description: "stream regression", Model: m, MaxIterations: 3,
				ModelRetryConfig: retry,
				ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{
					&agenticStreamTestTool{name: "TaskCreate", calls: &calls},
					&agenticStreamTestTool{name: "exec", calls: &calls},
				}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			iter := a.Run(ctx, &adk.TypedAgentInput[*schema.AgenticMessage]{
				EnableStreaming: true, Messages: EinoMessagesToAgentic([]*schema.Message{schema.UserMessage("plan and run")}),
			})
			var runErrors []string
			var completed bool
			for {
				ev, ok := iter.Next()
				if !ok {
					break
				}
				if ev.Err != nil {
					runErrors = append(runErrors, ev.Err.Error())
				}
				if ev.Output == nil || ev.Output.MessageOutput == nil {
					continue
				}
				output := ev.Output.MessageOutput
				if output.IsStreaming {
					for {
						msg, recvErr := output.MessageStream.Recv()
						if recvErr != nil {
							if recvErr != io.EOF {
								runErrors = append(runErrors, recvErr.Error())
							}
							break
						}
						if agenticMessageText(msg) == "completed" {
							completed = true
						}
					}
					output.MessageStream.Close()
				} else if agenticMessageText(output.Message) == "completed" {
					completed = true
				}
			}
			if repair {
				if len(runErrors) != 0 || !completed {
					t.Fatalf("completed = %t, errors = %v", completed, runErrors)
				}
			} else if !strings.Contains(strings.Join(runErrors, "\n"), "model output rejected by ShouldRetry at attempt 1") {
				t.Fatalf("raw backend did not reproduce screenshot error: %v", runErrors)
			}
			if requests.Load() != 2 {
				t.Fatalf("requests = %d, want 2 (raw: retry; repaired: tool round + final answer)", requests.Load())
			}
			for i := range calls {
				want := int32(0)
				if repair {
					want = 1
				}
				if got := calls[i].Load(); got != want {
					t.Fatalf("tool %d executed %d times, want %d", i, got, want)
				}
			}
		})
	}
}

type agenticStreamTestTool struct {
	name  string
	calls *[5]atomic.Int32
}

func (s *agenticStreamTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: s.name, Desc: "Record a test call", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"step": {Type: schema.Integer, Required: true},
	})}, nil
}

func (s *agenticStreamTestTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var input struct{ Step int }
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", err
	}
	if input.Step < 0 || input.Step >= len(s.calls) || (s.name == "exec") != (input.Step == 4) {
		return "", fmt.Errorf("unexpected %s arguments: %s", s.name, args)
	}
	s.calls[input.Step].Add(1)
	return "recorded: " + args, nil
}

type agenticStreamIndexTestModel struct {
	frames []*schema.AgenticMessage
	err    error
}

func (m *agenticStreamIndexTestModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return m.frames[0], m.err
}

func (m *agenticStreamIndexTestModel) Stream(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if m.err != nil {
		return nil, m.err
	}
	return schema.StreamReaderFromArray(m.frames), nil
}

func TestAgenticStreamIndexRepairPreservesValidStreams(t *testing.T) {
	// Indices need not be consecutive or arrive in sorted order. Metadata-only,
	// complete non-streaming and nil frames must also pass through unchanged.
	frames := []*schema.AgenticMessage{
		agenticIndexTestFrame(4, schema.NewContentBlock(&schema.Reasoning{Text: "thinking"})),
		agenticIndexTestFrame(1, schema.NewContentBlock(&schema.AssistantGenText{Text: "hello"})),
		agenticIndexTestFrame(1, schema.NewContentBlock(&schema.AssistantGenText{Text: " world"})),
		{ResponseMeta: &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{TotalTokens: 12}}},
		{ContentBlocks: []*schema.ContentBlock{nil, schema.NewContentBlock(&schema.AssistantGenText{Text: "complete"})}},
		nil,
	}
	m := newAgenticStreamIndexRepairModel(&agenticStreamIndexTestModel{frames: frames})
	for run := 0; run < 2; run++ {
		stream, err := m.Stream(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range frames {
			got, err := stream.Recv()
			if err != nil || got != want {
				t.Fatalf("run %d frame %d changed: got %p, want %p, error %v", run, i, got, want, err)
			}
		}
		stream.Close()
	}
}

func TestAgenticStreamIndexRepairSeparatesAllBlockTypes(t *testing.T) {
	frames := []*schema.AgenticMessage{
		agenticIndexTestFrame(0, schema.NewContentBlock(&schema.Reasoning{Text: "think"})),
		agenticIndexTestFrame(0, schema.NewContentBlock(&schema.AssistantGenText{Text: "one"})),
		agenticIndexTestFrame(0, schema.NewContentBlock(&schema.AssistantGenText{Text: " two"})),
		agenticIndexTestFrame(1, schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call", Name: "exec", Arguments: "{}"})),
	}
	before, err := json.Marshal(frames)
	if err != nil {
		t.Fatal(err)
	}
	m := newAgenticStreamIndexRepairModel(&agenticStreamIndexTestModel{frames: frames})
	for run := 0; run < 2; run++ {
		stream, err := m.Stream(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		msg := concatAgenticTestStream(t, stream)
		if len(msg.ContentBlocks) != 3 || msg.ContentBlocks[0].Reasoning.Text != "think" ||
			msg.ContentBlocks[1].AssistantGenText.Text != "one two" || msg.ContentBlocks[2].FunctionToolCall.CallID != "call" {
			t.Fatalf("repaired blocks = %#v", msg.ContentBlocks)
		}
	}
	after, err := json.Marshal(frames)
	if err != nil || string(before) != string(after) {
		t.Fatalf("provider frames mutated: before %s, after %s, error %v", before, after, err)
	}
}

func TestAgenticStreamIndexRepairPassesThroughErrorsAndGenerate(t *testing.T) {
	wantErr := errors.New("provider failed")
	want := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}
	m := newAgenticStreamIndexRepairModel(&agenticStreamIndexTestModel{frames: []*schema.AgenticMessage{want}, err: wantErr})
	if msg, err := m.Generate(context.Background(), nil); msg != want || !errors.Is(err, wantErr) {
		t.Fatalf("Generate = (%p, %v), want (%p, %v)", msg, err, want, wantErr)
	}
	if stream, err := m.Stream(context.Background(), nil); stream != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Stream = (%v, %v), want provider error", stream, err)
	}
}

func agenticIndexTestFrame(index int, block *schema.ContentBlock) *schema.AgenticMessage {
	block.StreamingMeta = &schema.StreamingMeta{Index: index}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{block}}
}
