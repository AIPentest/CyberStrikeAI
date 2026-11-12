package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestAgenticToolArgumentsSanitizerPreservesHistory(t *testing.T) {
	for _, args := range []string{`{"command":"unterminated`, `[]`, `null`, ``, `{"command":`} {
		t.Run(args, func(t *testing.T) {
			good := schema.NewContentBlock(&schema.FunctionToolCall{CallID: "good", Name: "exec", Arguments: `{"command":"pwd"}`})
			bad := schema.NewContentBlock(&schema.FunctionToolCall{CallID: "bad", Name: "exec", Arguments: args})
			text := schema.NewContentBlock(&schema.AssistantGenText{Text: "keep findings"})
			user := schema.UserAgenticMessage("latest instruction")
			result := schema.UserAgenticMessage("execution failed")
			msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{nil, text, good, bad}}
			state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{nil, msg, result, user}}
			mw := newAgenticToolCallArgumentsSanitizerMiddleware(nil, "test")
			_, out, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			if out == state || out.Messages[1] == msg || out.Messages[1].ContentBlocks[3] == bad {
				t.Fatal("repair must copy history")
			}
			repaired := out.Messages[1].ContentBlocks[3].FunctionToolCall
			if repaired.Arguments != `{}` || repaired.Name != "exec" || repaired.CallID != "bad" {
				t.Fatalf("repair changed call identity: %+v", repaired)
			}
			if bad.FunctionToolCall.Arguments != args {
				t.Fatal("mutated original arguments")
			}
			if out.Messages[1].ContentBlocks[1] != text || out.Messages[1].ContentBlocks[2] != good || out.Messages[2] != result || out.Messages[3] != user {
				t.Fatal("changed unrelated history")
			}
			_, unchanged, err := mw.BeforeModelRewriteState(context.Background(), out, nil)
			if err != nil || unchanged != out {
				t.Fatal("valid history should remain unchanged")
			}
		})
	}
}

// A real SDK round trip reproduces a gateway that parses tool argument strings
// on subsequent turns: valid outer JSON alone does not prevent this HTTP 400.
func TestAgenticToolArgumentsSanitizerPreventsMultiTurnHTTP400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role      string `json:"role"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		calls := 0
		for _, msg := range body.Messages {
			for _, call := range msg.ToolCalls {
				calls++
				if !validToolArgumentsJSONObject(call.Function.Arguments) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":{"message":"Unterminated string starting at: line 1 column 13 (char 12)","type":"invalid_request_error"}}`)
					return
				}
				if call.ID != "broken" || call.Function.Arguments != `{}` {
					t.Errorf("unexpected history call: %+v", call)
				}
			}
		}
		if calls != 1 {
			t.Errorf("history calls=%d, want 1", calls)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"continued\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	ctx := context.Background()
	base, err := newEinoAgenticChatModelFactory(server.Client(), nil, nil)(ctx, config.OpenAIConfig{Provider: "openai", APIKey: "test-key", BaseURL: server.URL, Model: "space-bunny-free"}, einoModelModeNormal)
	if err != nil {
		t.Fatal(err)
	}
	broken := schema.AssistantMessage("", []schema.ToolCall{{ID: "broken", Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{"command":"unterminated`}}})
	input := []*schema.Message{schema.UserMessage("start"), broken, schema.ToolMessage("Invalid JSON arguments; tool was not executed", "broken"), schema.UserMessage("continue")}
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprint(repair), func(t *testing.T) {
			var handlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
			if repair {
				handlers = appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{phase: "test"})
			}
			agent, err := newEinoAgenticChatModelAgentAdapter(ctx, einoAgenticChatModelAgentConfig{Name: "test", Model: base, Handlers: handlers})
			if err != nil {
				t.Fatal(err)
			}
			it := agent.Run(ctx, &adk.AgentInput{Messages: input, EnableStreaming: true})
			var runErr error
			var answer strings.Builder
			for {
				ev, ok := it.Next()
				if !ok {
					break
				}
				if ev.Err != nil {
					runErr = ev.Err
					break
				}
				if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.MessageStream != nil {
					stream := ev.Output.MessageOutput.MessageStream
					for {
						msg, err := stream.Recv()
						if err != nil {
							if !errors.Is(err, io.EOF) {
								runErr = err
							}
							break
						}
						if msg != nil {
							answer.WriteString(msg.Content)
						}
					}
					stream.Close()
				}
			}
			if repair && runErr != nil {
				t.Fatalf("repaired history rejected: %v", runErr)
			}
			if repair && !strings.Contains(answer.String(), "continued") {
				t.Fatalf("agent did not continue: %q", answer.String())
			}
			if !repair && runErr == nil {
				t.Fatal("expected HTTP 400 without sanitizer")
			}
			if broken.ToolCalls[0].Function.Arguments != `{"command":"unterminated` {
				t.Fatal("original history mutated")
			}
		})
	}
}

// Summarization makes its own model call and can rewrite retained history, so
// guard both its input and the final model input rather than just one boundary.
type agenticSummaryArgumentsProbe struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	checked bool
}

func (p *agenticSummaryArgumentsProbe) BeforeModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], _ *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	for _, msg := range state.Messages {
		for _, call := range agenticFunctionToolCalls(msg) {
			if !validToolArgumentsJSONObject(call.Arguments) {
				return ctx, state, fmt.Errorf("invalid arguments reached summarization")
			}
			p.checked = true
		}
	}
	out := *state
	out.Messages = append([]*schema.AgenticMessage(nil), state.Messages...)
	for i, msg := range out.Messages {
		if len(agenticFunctionToolCalls(msg)) == 0 {
			continue
		}
		calls := []*schema.FunctionToolCall{{CallID: "broken", Name: "exec", Arguments: `{"command":"truncated`}}
		out.Messages[i] = cloneAgenticMessageWithCalls(msg, calls)
	}
	return ctx, &out, nil
}

func TestAgenticToolArgumentsSanitizerAroundSummarization(t *testing.T) {
	probe := &agenticSummaryArgumentsProbe{TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{}}
	history := EinoMessagesToAgentic([]*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "broken", Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{"command":"truncated`}}}),
		schema.ToolMessage("invalid JSON; not executed", "broken"),
		schema.UserMessage("continue"),
	})
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: history}
	handlers := appendEinoAgenticChatModelTailMiddlewares(nil, einoChatModelTailConfig{agenticSummarization: probe})
	ctx := context.Background()
	for _, handler := range handlers {
		var err error
		ctx, state, err = handler.BeforeModelRewriteState(ctx, state, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !probe.checked {
		t.Fatal("summary probe did not inspect retained call")
	}
	calls := agenticFunctionToolCalls(state.Messages[0])
	if len(calls) != 1 || calls[0].Arguments != `{}` {
		t.Fatalf("final input contains malformed call: %+v", calls)
	}
	if agenticFunctionToolCalls(history[0])[0].Arguments != `{"command":"truncated` {
		t.Fatal("mutated original history")
	}
}
