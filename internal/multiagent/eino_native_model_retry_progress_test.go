package multiagent

import (
	"testing"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestEmitEinoNativeModelRetryProgress(t *testing.T) {
	willRetry := &adk.WillRetryError{
		ErrStr:       "HTTP 429 Too Many Requests",
		RetryAttempt: 2,
	}
	var gotType, gotMessage string
	var gotData map[string]interface{}
	called := emitEinoNativeModelRetryProgress("conv-1", "deep_agent", willRetry, func(eventType, message string, data interface{}) {
		gotType = eventType
		gotMessage = message
		var ok bool
		gotData, ok = data.(map[string]interface{})
		if !ok {
			t.Fatalf("progress data type = %T, want map[string]interface{}", data)
		}
	}, nil, willRetry)
	if !called {
		t.Fatal("called = false, want true")
	}
	if gotType != "eino_model_retry" {
		t.Fatalf("event type = %q, want eino_model_retry", gotType)
	}
	if gotMessage != "Model call hit a transient issue; Eino is retrying natively…" {
		t.Fatalf("message = %q", gotMessage)
	}
	assertNativeRetryMapValue(t, gotData, "conversationId", "conv-1")
	assertNativeRetryMapValue(t, gotData, "source", "eino")
	assertNativeRetryMapValue(t, gotData, "orchestration", "deep_agent")
	assertNativeRetryMapValue(t, gotData, "attempt", 2)
	assertNativeRetryMapValue(t, gotData, "reason", "")
	assertNativeRetryMapValue(t, gotData, "error", "HTTP 429 Too Many Requests")
}

func TestEmitEinoNativeModelRetryProgressNilSafe(t *testing.T) {
	calledProgress := false
	called := emitEinoNativeModelRetryProgress("conv-1", "deep_agent", nil, func(string, string, interface{}) {
		calledProgress = true
	}, nil, nil)
	if called {
		t.Fatal("called = true, want false")
	}
	if calledProgress {
		t.Fatal("progress called for nil willRetry")
	}
}

func TestEmitEinoNativeModelRetryProgressLogsEvent(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)
	willRetry := &adk.WillRetryError{
		ErrStr:       "HTTP 500",
		RetryAttempt: 3,
	}

	emitEinoNativeModelRetryProgress("conv-1", "single_agent", willRetry, nil, logger, willRetry)

	entry := logs.FilterMessage("eino native model retry event").TakeAll()
	if len(entry) != 1 {
		t.Fatalf("log count = %d, want 1", len(entry))
	}
	fields := entry[0].ContextMap()
	if fields["orchestration"] != "single_agent" {
		t.Fatalf("orchestration field = %v", fields["orchestration"])
	}
	if fields["attempt"] != int64(3) {
		t.Fatalf("attempt field = %v", fields["attempt"])
	}
}

func assertNativeRetryMapValue(t *testing.T, data map[string]interface{}, key string, want interface{}) {
	t.Helper()
	if got := data[key]; got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}
