package multiagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
)

func TestEinoRunErrorHandlerCancelUsesNativeFallback(t *testing.T) {
	pending := newEinoPendingToolCalls("conv-1", nil)
	pending.Mark(toolCallPendingInfo{ToolCallID: "call-1", ToolName: "execute"})
	want := errors.New("native cancel")

	got := newEinoRunErrorHandler(einoRunErrorHandlerConfig{
		ConversationID: "conv-1",
		Pending:        pending,
		NativeCancelFallback: func() error {
			return want
		},
	}).Handle(&adk.CancelError{Info: &adk.AgentCancelInfo{}})

	if !errors.Is(got, want) {
		t.Fatalf("err = %v, want native fallback", got)
	}
	if pending.Count() != 0 {
		t.Fatalf("pending count = %d, want 0", pending.Count())
	}
}

func TestEinoRunErrorHandlerTimeoutAndGeneralErrorProgress(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		errorKind interface{}
	}{
		{name: "timeout", err: context.DeadlineExceeded, errorKind: "timeout"},
		{name: "general", err: errors.New("boom"), errorKind: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data map[string]interface{}
			got := newEinoRunErrorHandler(einoRunErrorHandlerConfig{
				ConversationID: "conv-1",
				Progress: func(eventType, _ string, raw interface{}) {
					if eventType == "error" {
						data, _ = raw.(map[string]interface{})
					}
				},
			}).Handle(tc.err)
			if !errors.Is(got, tc.err) {
				t.Fatalf("err = %v", got)
			}
			if data["conversationId"] != "conv-1" || data["source"] != "eino" {
				t.Fatalf("data = %#v", data)
			}
			if gotKind := data["errorKind"]; gotKind != tc.errorKind {
				t.Fatalf("errorKind = %#v, want %#v", gotKind, tc.errorKind)
			}
		})
	}
}

func TestEinoRunErrorHandlerRetryExhaustedEmptyOutputProgress(t *testing.T) {
	err := &adk.RetryExhaustedError{
		LastErr:      errors.New("model output rejected by ShouldRetry at attempt 5"),
		TotalRetries: 4,
	}
	var message string
	var data map[string]interface{}

	got := newEinoRunErrorHandler(einoRunErrorHandlerConfig{
		ConversationID: "conv-1",
		Progress: func(eventType, msg string, raw interface{}) {
			if eventType == "error" {
				message = msg
				data, _ = raw.(map[string]interface{})
			}
		},
	}).Handle(err)

	if !errors.Is(got, err) {
		t.Fatalf("err = %v", got)
	}
	if !strings.Contains(message, "Model call retries exhausted") ||
		!strings.Contains(message, "The model returned no original error; its output was rejected by the retry policy.") ||
		strings.Contains(message, "model output rejected by ShouldRetry at attempt 5") {
		t.Fatalf("message = %q", message)
	}
	if data["errorKind"] != "model_output_rejected" {
		t.Fatalf("errorKind = %#v", data["errorKind"])
	}
	if data["errorSummary"] != "The model returned no original error; its output was rejected by the retry policy." {
		t.Fatalf("errorSummary = %#v", data["errorSummary"])
	}
	if data["hasModelOriginalError"] != false {
		t.Fatalf("hasModelOriginalError = %#v", data["hasModelOriginalError"])
	}
	if data["retryExhausted"] != true || data["totalRetries"] != 4 {
		t.Fatalf("retry metadata = %#v", data)
	}
	if data["lastError"] != "model output rejected by ShouldRetry at attempt 5" {
		t.Fatalf("lastError = %#v", data["lastError"])
	}
	if data["technicalError"] != "model output rejected by ShouldRetry at attempt 5" {
		t.Fatalf("technicalError = %#v", data["technicalError"])
	}
	if _, ok := data["modelOriginalError"]; ok {
		t.Fatalf("modelOriginalError should be absent for ShouldRetry rejection, got %#v", data["modelOriginalError"])
	}
	if data["error"] != err.Error() {
		t.Fatalf("raw error = %#v, want %#v", data["error"], err.Error())
	}
}

func TestEinoRunErrorHandlerRetryExhaustedOriginalErrorProgress(t *testing.T) {
	err := &adk.RetryExhaustedError{
		LastErr:      errors.New("HTTP 429 Too Many Requests"),
		TotalRetries: 3,
	}
	var message string
	var data map[string]interface{}

	got := newEinoRunErrorHandler(einoRunErrorHandlerConfig{
		ConversationID: "conv-1",
		Progress: func(eventType, msg string, raw interface{}) {
			if eventType == "error" {
				message = msg
				data, _ = raw.(map[string]interface{})
			}
		},
	}).Handle(err)

	if !errors.Is(got, err) {
		t.Fatalf("err = %v", got)
	}
	if !strings.Contains(message, "HTTP 429 Too Many Requests") {
		t.Fatalf("message = %q", message)
	}
	if data["errorKind"] != "rate_limit" {
		t.Fatalf("errorKind = %#v", data["errorKind"])
	}
	if data["errorSummary"] != "HTTP 429 Too Many Requests" {
		t.Fatalf("errorSummary = %#v", data["errorSummary"])
	}
	if data["lastError"] != "HTTP 429 Too Many Requests" {
		t.Fatalf("lastError = %#v", data["lastError"])
	}
	if data["modelOriginalError"] != "HTTP 429 Too Many Requests" {
		t.Fatalf("modelOriginalError = %#v", data["modelOriginalError"])
	}
	if _, ok := data["hasModelOriginalError"]; ok {
		t.Fatalf("hasModelOriginalError should be absent when original error is present, got %#v", data["hasModelOriginalError"])
	}
}

func TestEinoClientRunErrorMessageUsesRawRetryExhaustedModelError(t *testing.T) {
	raw := "summary content is empty: role=assistant content_runes=0 reasoning_runes=42\nprovider request id: req_123"
	err := &adk.RetryExhaustedError{
		LastErr:      errors.New(raw),
		TotalRetries: 4,
	}

	got := EinoClientRunErrorMessage(err)
	if !strings.Contains(got, "Model call retries exhausted (4 attempts)") {
		t.Fatalf("message missing retry prefix: %q", got)
	}
	if !strings.Contains(got, raw) {
		t.Fatalf("message should include raw model error:\n%s", got)
	}

	fields := EinoClientRunErrorFields(err)
	if fields["modelOriginalError"] != raw || fields["lastError"] != raw {
		t.Fatalf("raw fields = %#v", fields)
	}
}

func TestEinoClientRunErrorMessageMarksSummarizationModelRetryError(t *testing.T) {
	raw := `POST "https://api.deepseek.com/v1/chat/completions": 429 Too Many Requests: {"error":{"message":"Rate limit reached"}}`
	err := &adk.RetryExhaustedError{
		LastErr:      newEinoSummarizationModelError(errors.New(raw)),
		TotalRetries: 4,
	}

	got := EinoClientRunErrorMessage(err)
	for _, want := range []string{
		"Summary-stage model call failed",
		"Model call retries exhausted (4 attempts)",
		"last raw model error",
		raw,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("message missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "summarization model error") {
		t.Fatalf("message leaked internal wrapper:\n%s", got)
	}

	fields := EinoClientRunErrorFields(err)
	if fields["errorPhase"] != "summarization" || fields["summarizationModelError"] != true {
		t.Fatalf("phase fields = %#v", fields)
	}
	if fields["modelOriginalError"] != raw || fields["lastError"] != raw {
		t.Fatalf("raw fields = %#v", fields)
	}
}

func TestEinoClientRunErrorMessageMarksDirectSummarizationModelError(t *testing.T) {
	raw := `POST "https://api.deepseek.com/v1/chat/completions": 400 Bad Request: {"error":{"message":"invalid thinking parameter"}}`
	err := newEinoSummarizationModelError(errors.New(raw))

	got := EinoClientRunErrorMessage(err)
	for _, want := range []string{
		"Summary-stage model call failed",
		"raw model error",
		raw,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("message missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "summarization model error") {
		t.Fatalf("message leaked internal wrapper:\n%s", got)
	}

	fields := EinoClientRunErrorFields(err)
	if fields["errorPhase"] != "summarization" ||
		fields["modelOriginalError"] != raw ||
		fields["lastError"] != raw {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestEinoRunErrorHandlerIterationLimitProgress(t *testing.T) {
	var events []string
	var errorKind interface{}
	err := errors.New("maximum iteration reached")

	got := newEinoRunErrorHandler(einoRunErrorHandlerConfig{
		ConversationID: "conv-1",
		OrchMode:       "deep",
		Progress: func(eventType, _ string, raw interface{}) {
			events = append(events, eventType)
			if eventType == "error" {
				data, _ := raw.(map[string]interface{})
				errorKind = data["errorKind"]
			}
		},
	}).Handle(err)

	if !errors.Is(got, err) {
		t.Fatalf("err = %v", got)
	}
	if len(events) != 2 || events[0] != "iteration_limit_reached" || events[1] != "error" {
		t.Fatalf("events = %#v", events)
	}
	if errorKind != "iteration_limit" {
		t.Fatalf("errorKind = %#v", errorKind)
	}
}

func TestEinoRunErrorHandlerNilSafe(t *testing.T) {
	var h *einoRunErrorHandler
	if h.Handle(nil) != nil {
		t.Fatal("nil handler nil err should return nil")
	}
	err := errors.New("boom")
	if got := h.Handle(err); !errors.Is(got, err) {
		t.Fatalf("nil handler err = %v", got)
	}
}
