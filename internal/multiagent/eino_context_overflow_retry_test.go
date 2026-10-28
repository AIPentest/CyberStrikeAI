package multiagent

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestEinoContextOverflowRetryHandlerPreparesOnce(t *testing.T) {
	baseMsgs := []adk.Message{
		schema.UserMessage("base"),
	}
	accumulated := []adk.Message{
		schema.UserMessage("base"),
		schema.AssistantMessage("partial", nil),
	}
	var gotType, gotMessage string
	var gotData map[string]interface{}
	core, logs := observer.New(zap.WarnLevel)
	handler := newEinoContextOverflowRetryHandler(einoContextOverflowRetryConfig{
		Context:        context.Background(),
		ConversationID: "conv-1",
		OrchMode:       "deep_agent",
		Args:           &einoADKRunLoopArgs{},
		BaseMsgs:       baseMsgs,
		Progress: func(eventType, message string, data interface{}) {
			gotType = eventType
			gotMessage = message
			var ok bool
			gotData, ok = data.(map[string]interface{})
			if !ok {
				t.Fatalf("progress data type = %T, want map[string]interface{}", data)
			}
		},
		Logger: zap.New(core),
	})

	result := handler.Prepare(errors.New("context length exceeded"), accumulated, len(baseMsgs))
	if !result.Handled {
		t.Fatal("handled = false, want true")
	}
	if result.ContextSrc != einoRestartContextAccumulated {
		t.Fatalf("context source = %q, want %q", result.ContextSrc, einoRestartContextAccumulated)
	}
	if len(result.RestartMsgs) != len(accumulated) {
		t.Fatalf("restart message count = %d, want %d", len(result.RestartMsgs), len(accumulated))
	}
	if gotType != "eino_context_overflow_retry" {
		t.Fatalf("event type = %q, want eino_context_overflow_retry", gotType)
	}
	if gotMessage != "Context overflow detected; retrying after aggressive compaction…" {
		t.Fatalf("message = %q", gotMessage)
	}
	assertContextOverflowMapValue(t, gotData, "conversationId", "conv-1")
	assertContextOverflowMapValue(t, gotData, "source", "eino")
	assertContextOverflowMapValue(t, gotData, "orchestration", "deep_agent")
	assertContextOverflowMapValue(t, gotData, "contextSource", string(einoRestartContextAccumulated))
	if logs.FilterMessage("eino context overflow, retrying with aggressive compaction").Len() != 1 {
		t.Fatalf("expected one context overflow retry log, got %d", logs.Len())
	}

	second := handler.Prepare(errors.New("maximum context length"), accumulated, len(baseMsgs))
	if second.Handled {
		t.Fatalf("second result = %+v, want unhandled after first retry", second)
	}
}

func TestEinoContextOverflowRetryHandlerIgnoresOtherErrors(t *testing.T) {
	handler := newEinoContextOverflowRetryHandler(einoContextOverflowRetryConfig{
		Context:  context.Background(),
		Args:     &einoADKRunLoopArgs{},
		BaseMsgs: []adk.Message{schema.UserMessage("base")},
	})
	result := handler.Prepare(errors.New("HTTP 429 Too Many Requests"), nil, 0)
	if result.Handled {
		t.Fatalf("result = %+v, want unhandled", result)
	}
}

func assertContextOverflowMapValue(t *testing.T, data map[string]interface{}, key string, want interface{}) {
	t.Helper()
	if got := data[key]; got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}
