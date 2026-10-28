package multiagent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestEinoTransientRunRetryHandlerPreparesRetry(t *testing.T) {
	baseMsgs := []adk.Message{schema.UserMessage("base")}
	accumulated := []adk.Message{
		schema.UserMessage("base"),
		schema.AssistantMessage("partial", nil),
	}
	runErr := errors.New("HTTP 503 Service Unavailable")
	var events []capturedTransientRetryEvent
	core, logs := observer.New(zap.WarnLevel)
	handler := newEinoTransientRunRetryHandler(einoTransientRunRetryHandlerConfig{
		ConversationID: "conv-1",
		OrchMode:       "deep_agent",
		Args:           &einoADKRunLoopArgs{},
		BaseMsgs:       baseMsgs,
		Progress: func(eventType, message string, data interface{}) {
			m, ok := data.(map[string]interface{})
			if !ok {
				t.Fatalf("progress data type = %T, want map[string]interface{}", data)
			}
			events = append(events, capturedTransientRetryEvent{eventType: eventType, message: message, data: m})
		},
		Logger: zap.New(core),
		Policy: einoTransientRunRetryPolicy{maxAttempts: 2, maxBackoff: time.Nanosecond},
	})

	result := handler.Prepare(runErr, accumulated, len(baseMsgs))
	if !result.Handled || !result.Restarted {
		t.Fatalf("result = %+v, want handled restarted", result)
	}
	if result.Fatal != nil {
		t.Fatalf("fatal = %v, want nil", result.Fatal)
	}
	if result.ContextSrc != einoRestartContextAccumulated {
		t.Fatalf("context source = %q, want %q", result.ContextSrc, einoRestartContextAccumulated)
	}
	if len(result.RestartMsgs) != len(accumulated) {
		t.Fatalf("restart messages = %d, want %d", len(result.RestartMsgs), len(accumulated))
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].eventType != "eino_run_retry" || events[1].eventType != "eino_run_retry" {
		t.Fatalf("event types = %q/%q", events[0].eventType, events[1].eventType)
	}
	if !strings.Contains(events[0].message, "attempt 1/2") || !strings.Contains(events[0].message, "retrying in") {
		t.Fatalf("first message = %q", events[0].message)
	}
	if events[1].message != "Context restored, retrying…" {
		t.Fatalf("second message = %q", events[1].message)
	}
	assertTransientRetryMapValue(t, events[0].data, "conversationId", "conv-1")
	assertTransientRetryMapValue(t, events[0].data, "source", "eino")
	assertTransientRetryMapValue(t, events[0].data, "orchestration", "deep_agent")
	assertTransientRetryMapValue(t, events[0].data, "error", runErr.Error())
	assertTransientRetryMapValue(t, events[0].data, "errorKind", "upstream_server")
	assertTransientRetryMapValue(t, events[0].data, "attempt", 1)
	assertTransientRetryMapValue(t, events[0].data, "maxAttempts", 2)
	assertTransientRetryMapValue(t, events[0].data, "backoffSec", 0)
	assertTransientRetryMapValue(t, events[1].data, "contextSource", string(einoRestartContextAccumulated))
	if logs.FilterMessage("eino transient error, retrying after backoff").Len() != 1 {
		t.Fatalf("expected one retry log, got %d", logs.Len())
	}
}

func TestEinoTransientRunRetryHandlerExhaustsAndFlushesPending(t *testing.T) {
	runErr := errors.New("HTTP 503 Service Unavailable")
	var progressEvents []string
	pending := newEinoPendingToolCalls("conv-1", func(eventType, _ string, _ interface{}) {
		progressEvents = append(progressEvents, eventType)
	})
	pending.Mark(toolCallPendingInfo{ToolCallID: "call-1", ToolName: "execute", EinoAgent: "agent"})
	core, logs := observer.New(zap.WarnLevel)
	handler := newEinoTransientRunRetryHandler(einoTransientRunRetryHandlerConfig{
		OrchMode: "deep_agent",
		Args:     &einoADKRunLoopArgs{},
		BaseMsgs: []adk.Message{schema.UserMessage("base")},
		Logger:   zap.New(core),
		Pending:  pending,
		Policy:   einoTransientRunRetryPolicy{maxAttempts: 1, maxBackoff: time.Nanosecond},
	})

	first := handler.Prepare(runErr, nil, 0)
	if !first.Restarted {
		t.Fatalf("first result = %+v, want restarted", first)
	}
	second := handler.Prepare(runErr, nil, 0)
	if !second.Handled || second.Fatal == nil {
		t.Fatalf("second result = %+v, want fatal exhaustion", second)
	}
	if pending.Count() != 0 {
		t.Fatalf("pending count = %d, want 0", pending.Count())
	}
	if len(progressEvents) != 1 || progressEvents[0] != "tool_result" {
		t.Fatalf("pending flush events = %#v, want one tool_result", progressEvents)
	}
	if logs.FilterMessage("eino transient retry exhausted").Len() != 1 {
		t.Fatalf("expected one exhausted log, got %d", logs.Len())
	}
}

func TestEinoTransientRunRetryHandlerConfirmRecoveryResetsAttempts(t *testing.T) {
	runErr := errors.New("HTTP 503 Service Unavailable")
	handler := newEinoTransientRunRetryHandler(einoTransientRunRetryHandlerConfig{
		Args:     &einoADKRunLoopArgs{},
		BaseMsgs: []adk.Message{schema.UserMessage("base")},
		Policy:   einoTransientRunRetryPolicy{maxAttempts: 1, maxBackoff: time.Nanosecond},
	})
	if result := handler.Prepare(runErr, nil, 0); !result.Restarted {
		t.Fatalf("first result = %+v, want restarted", result)
	}
	handler.ConfirmRecovery()
	if result := handler.Prepare(runErr, nil, 0); !result.Restarted || result.Fatal != nil {
		t.Fatalf("after reset result = %+v, want restarted without fatal", result)
	}
}

func TestEinoTransientRunRetryHandlerIgnoresOtherErrors(t *testing.T) {
	handler := newEinoTransientRunRetryHandler(einoTransientRunRetryHandlerConfig{})
	result := handler.Prepare(errors.New("invalid api key"), nil, 0)
	if result.Handled {
		t.Fatalf("result = %+v, want unhandled", result)
	}
}

type capturedTransientRetryEvent struct {
	eventType string
	message   string
	data      map[string]interface{}
}

func assertTransientRetryMapValue(t *testing.T, data map[string]interface{}, key string, want interface{}) {
	t.Helper()
	if got := data[key]; got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}
