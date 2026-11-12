package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestNativeReasoningActualModelAndToolChoice(t *testing.T) {
	for _, tc := range []struct {
		name, url, model, effort, choice, wantEffort, wantErr string
	}{
		{"auto_none", "https://api.openai.com/v1", "gpt-5.6-sol", "none", "auto", "none", ""},
		{"auto_high_rejected", "https://api.openai.com/v1", "gpt-5.6-sol", "high", "auto", "", "set reasoning mode to off"},
		{"object_none", "https://api.openai.com/v1", "gpt-5.6-sol", "none", `{"type":"function","function":{"name":"lookup"}}`, "none", ""},
		{"override_requires_responses", "https://api.openai.com/v1", "gpt-6.1-sol", "none", "auto", "", "requires /v1/responses"},
		{"override_older_model", "https://api.openai.com/v1", "gpt-4o-mini", "high", "auto", "high", ""},
		{"gateway_auto_high_unchanged", "https://gateway.example/v1", "gpt-5.6-sol", "high", "auto", "high", ""},
		{"gateway_responses_model_unchanged", "https://gateway.example/v1", "gpt-6.1-sol", "high", "auto", "high", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.OpenAIConfig{Provider: "openai", BaseURL: tc.url, Model: "gpt-5.6-sol"}
			choice := any(tc.choice)
			if strings.HasPrefix(tc.choice, "{") {
				choice = json.RawMessage(tc.choice)
			}
			body, err := json.Marshal(map[string]any{"model": tc.model, "reasoning_effort": tc.effort, "tool_choice": choice,
				"messages": []any{}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}}})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			rt := &reasoningToolChoiceCompatRoundTripper{cfg: cfg, base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				var got map[string]any
				if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if got["reasoning_effort"] != tc.wantEffort {
					t.Fatalf("effort=%v, want %q", got["reasoning_effort"], tc.wantEffort)
				}
				if got["model"] != tc.model {
					t.Fatalf("request model changed: %#v", got)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			req, err := http.NewRequest(http.MethodPost, tc.url+"/chat/completions", strings.NewReader(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := rt.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || calls != 0 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}
