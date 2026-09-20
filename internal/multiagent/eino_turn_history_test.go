package multiagent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type historyTool struct{ calls atomic.Int32 }

func (h *historyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "history_tool", Desc: "Record a completed test operation", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}
func (h *historyTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	h.calls.Add(1)
	return "completed-tool-evidence", nil
}

type historyModel struct {
	mu       sync.Mutex
	inputs   [][]*schema.Message
	started  chan int
	releases [2]chan struct{}
}

func (m *historyModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *historyModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	m.inputs = append(m.inputs, cloneSchemaMessages(input))
	n := len(m.inputs)
	m.mu.Unlock()
	m.started <- n
	if n == 1 {
		return schema.AssistantMessage("work started", []schema.ToolCall{{ID: "completed-call", Type: "function", Function: schema.FunctionCall{Name: "history_tool", Arguments: "{}"}}}), nil
	}
	if n == 2 || n == 3 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.releases[n-2]:
		}
	}
	return schema.AssistantMessage("completed-response", nil), nil
}
func (m *historyModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

type historyCompactor struct {
	adk.BaseChatModelAgentMiddleware
}

func (*historyCompactor) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	hasResult := false
	for _, m := range state.Messages {
		hasResult = hasResult || m.Role == schema.Tool
	}
	if !hasResult {
		return ctx, state, nil
	}
	out := *state
	out.Messages = nil
	for _, m := range state.Messages {
		if m.Content == "old-verbose-history" {
			summary := schema.UserMessage("compressed-progress-summary")
			summary.Extra = map[string]any{"_eino_adk_summarization_content_type": "summary"}
			out.Messages = append(out.Messages, summary)
		} else {
			out.Messages = append(out.Messages, m)
		}
	}
	return ctx, &out, nil
}

func TestEinoTurnHistoryRetainsCompletedWorkAcrossInterrupts(t *testing.T) {
	for _, safe := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			name := "timeout"
			if safe {
				name = "safe"
			}
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				m := &historyModel{started: make(chan int, 8), releases: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
				operation := &historyTool{}
				agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
					Name: "history-agent", Instruction: "stable-agent-instruction", Model: m,
					ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{operation}}},
					Handlers:    []adk.ChatModelAgentMiddleware{&historyCompactor{}, newSystemMessageNormalizerMiddleware(nil, "test"), newModelFacingTraceMiddleware(newModelFacingTraceHolder())},
				})
				if err != nil {
					t.Fatal(err)
				}
				timeout := 20 * time.Millisecond
				if safe {
					timeout = time.Second
				}
				runtime := NewEinoTurnLoopRuntime(EinoTurnLoopRuntimeConfig{Agent: agent, EnableStreaming: stream, InterruptTimeout: timeout, InitialMessages: []*schema.Message{schema.UserMessage("original-task"), schema.SystemMessage("durable-system-context"), schema.AssistantMessage("old-verbose-history", nil)}})
				runtime.Run(ctx)
				waitCall := func(want int) {
					t.Helper()
					select {
					case n := <-m.started:
						if n != want {
							t.Fatalf("call %d, want %d", n, want)
						}
					case <-ctx.Done():
						t.Fatal("model call timed out")
					}
				}
				waitCall(1)
				waitCall(2)
				if !runtime.PushInterruptContinue("first-supplement") {
					t.Fatal("push rejected")
				}
				if safe {
					close(m.releases[0])
				}
				waitCall(3)
				if !runtime.PushInterruptContinue("second-supplement") {
					t.Fatal("push rejected")
				}
				if safe {
					close(m.releases[1])
				}
				waitCall(4)
				runtime.StopWhenIdle()
				if state := runtime.Wait(); state.ExitReason != nil {
					t.Fatal(state.ExitReason)
				}
				if operation.calls.Load() != 1 {
					t.Fatalf("tool executed %d times", operation.calls.Load())
				}
				m.mu.Lock()
				defer m.mu.Unlock()
				for _, i := range []int{2, 3} {
					input := m.inputs[i]
					for _, marker := range []string{"original-task", "compressed-progress-summary", "completed-tool-evidence", "first-supplement", "durable-system-context", "stable-agent-instruction"} {
						count := 0
						for _, msg := range input {
							count += strings.Count(msg.Content, marker)
						}
						if count != 1 {
							t.Errorf("call %d: %q occurs %d times", i+1, marker, count)
						}
					}
					for _, msg := range input {
						if strings.Contains(msg.Content, "old-verbose-history") {
							t.Error("compacted history resurrected")
						}
					}
					if input[len(input)-1].Role != schema.User {
						t.Error("supplement must be last user message")
					}
					if safe {
						count := 0
						for _, msg := range input {
							if msg.Content == "completed-response" {
								count++
							}
						}
						if count != i-1 {
							t.Errorf("completed responses=%d, want %d", count, i-1)
						}
					}
				}
				if !strings.Contains(m.inputs[3][len(m.inputs[3])-1].Content, "second-supplement") {
					t.Error("second supplement lost")
				}
			})
		}
	}
}

func TestEinoTurnHistoryPendingToolBatch(t *testing.T) {
	h := &einoTurnHistory{}
	ctx := context.WithValue(context.Background(), einoTurnHistoryKey{}, h)
	calls := []schema.ToolCall{{ID: "done", Function: schema.FunctionCall{Name: "tool"}}, {ID: "pending", Function: schema.FunctionCall{Name: "tool"}}}
	captureEinoTurnHistory(ctx, []*schema.Message{schema.UserMessage("summary"), schema.AssistantMessage("", calls)})
	h.events = []*schema.Message{schema.AssistantMessage("discarded-old-output", nil), schema.ToolMessage("actual-result", "done")}
	got := h.nextInput()
	if len(got) != 4 || got[2].Content != "actual-result" || got[3].Content != patchedMissingToolResult {
		t.Fatalf("bad reconciled messages: %#v", got)
	}
	if got[2].ToolCallID != "done" || got[3].ToolCallID != "pending" {
		t.Fatal("tool IDs lost")
	}
}

func TestEinoTurnHistoryAgenticSnapshotAndIsolation(t *testing.T) {
	first, second := &einoTurnHistory{}, &einoTurnHistory{}
	first.begin([]*schema.Message{schema.UserMessage("first-task")})
	second.begin([]*schema.Message{schema.UserMessage("second-task")})
	ctx := context.WithValue(context.Background(), einoTurnHistoryKey{}, first)
	mw := newAgenticModelFacingTraceMiddleware(newModelFacingTraceHolder())
	ctx, _, err := mw.BeforeAgent(ctx, &adk.ChatModelAgentContext{Instruction: "agent-instruction"})
	if err != nil {
		t.Fatal(err)
	}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: EinoMessagesToAgentic([]*schema.Message{
		schema.SystemMessage("agent-instruction\n\nsystem-summary"), schema.UserMessage("compacted-first-task"),
	})}
	if _, _, err = mw.BeforeModelRewriteState(ctx, state, nil); err != nil {
		t.Fatal(err)
	}
	state.Messages = append(state.Messages, EinoMessagesToAgentic([]*schema.Message{schema.AssistantMessage("finished-step", nil)})[0])
	if _, _, err = mw.AfterModelRewriteState(ctx, state, nil); err != nil {
		t.Fatal(err)
	}
	got := first.nextInput()
	if len(got) != 3 || got[0].Content != "system-summary" || got[2].Content != "finished-step" {
		t.Fatalf("agentic state lost: %#v", got)
	}
	other := second.nextInput()
	if len(other) != 1 || other[0].Content != "second-task" {
		t.Fatalf("conversation leaked: %#v", other)
	}
}

func TestEinoTurnHistoryFallbackKeepsStreamedOutput(t *testing.T) {
	h := &einoTurnHistory{}
	h.begin([]*schema.Message{schema.UserMessage("initial-task")})
	events, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(&adk.AgentEvent{Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
		IsStreaming: true, MessageStream: schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("completed-", nil), schema.AssistantMessage("stream", nil)}),
	}}})
	gen.Close()
	if err := h.wrapEvents(nil)(context.Background(), nil, events); err != nil {
		t.Fatal(err)
	}
	got := h.nextInput()
	if len(got) != 2 || got[1].Content != "completed-stream" {
		t.Fatalf("stream history lost: %#v", got)
	}
}
