package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestApplyCustomHeadersOverridesBuiltinAndSkipsEmpty(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer builtin")
	applyCustomHeaders(req, &config.OpenAIConfig{Headers: map[string]string{
		"Authorization": "Bearer custom",
		"X-Api-Key":     "secret",
		"User-Agent":    "cyberstrike-ai/1.0",
		"X-Empty":       "",
		"  ":            "dropped",
	}})
	if got := req.Header.Get("Authorization"); got != "Bearer custom" {
		t.Fatalf("Authorization = %q, want custom override", got)
	}
	if got := req.Header.Get("X-Api-Key"); got != "secret" {
		t.Fatalf("X-Api-Key = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "cyberstrike-ai/1.0" {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := req.Header.Get("X-Empty"); got != "" {
		t.Fatalf("empty header value must be skipped, got %q", got)
	}
}

func TestApplyCustomHeadersNilConfigIsNoop(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	applyCustomHeaders(req, nil)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("unexpected Authorization %q", got)
	}
}

func TestHeadersRoundTripperInjectsHeaders(t *testing.T) {
	var auth, provider string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		provider = r.Header.Get("X-Provider")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.OpenAIConfig{Headers: map[string]string{
		"Authorization": "Bearer from-headers",
		"X-Provider":    "opencode",
	}}
	client := NewEinoHTTPClient(cfg, srv.Client())
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if auth != "Bearer from-headers" {
		t.Fatalf("Authorization = %q", auth)
	}
	if provider != "opencode" {
		t.Fatalf("X-Provider = %q", provider)
	}
}

func TestExpandHeaderMapEnv(t *testing.T) {
	t.Setenv("TEST_CS_HEADER_TOKEN", "tok-123")
	got := config.ExpandHeaderMap(map[string]string{
		"X-Api-Key": "${TEST_CS_HEADER_TOKEN}",
		"X-Default": "${TEST_CS_MISSING_VAR:-fallback}",
	})
	if got["X-Api-Key"] != "tok-123" {
		t.Fatalf("X-Api-Key = %q", got["X-Api-Key"])
	}
	if got["X-Default"] != "fallback" {
		t.Fatalf("X-Default = %q", got["X-Default"])
	}
	if config.ExpandHeaderMap(nil) != nil {
		t.Fatalf("nil headers should stay nil")
	}
}

func TestSessionHeaderPlaceholderRendering(t *testing.T) {
	cfg := &config.OpenAIConfig{Headers: map[string]string{
		"x-opencode-session": "%session_id%",
		"X-Prefixed":         "pfx-%session_id%",
		"X-Fixed":            "static",
	}}

	ctx := WithSessionID(context.Background(), "conv-abc-123")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	applyCustomHeaders(req, cfg)
	if got := req.Header.Get("x-opencode-session"); got != "conv-abc-123" {
		t.Fatalf("session header = %q, want conv-abc-123", got)
	}
	if got := req.Header.Get("X-Prefixed"); got != "pfx-conv-abc-123" {
		t.Fatalf("prefixed header = %q", got)
	}
	if got := req.Header.Get("X-Fixed"); got != "static" {
		t.Fatalf("fixed header = %q", got)
	}
}

func TestSessionHeaderSkippedWithoutSession(t *testing.T) {
	cfg := &config.OpenAIConfig{Headers: map[string]string{
		"x-opencode-session": "%session_id%",
		"X-Fixed":            "static",
	}}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", nil)
	applyCustomHeaders(req, cfg)
	if got := req.Header.Get("x-opencode-session"); got != "" {
		t.Fatalf("placeholder must be skipped without session, got %q", got)
	}
	if got := req.Header.Get("X-Fixed"); got != "static" {
		t.Fatalf("fixed header must still be sent, got %q", got)
	}
}

func TestSessionIDContextRoundTrip(t *testing.T) {
	if got := SessionIDFromContext(context.Background()); got != "" {
		t.Fatalf("empty context should yield empty id, got %q", got)
	}
	if got := SessionIDFromContext(WithSessionID(context.Background(), "  ")); got != "" {
		t.Fatalf("blank id must be ignored, got %q", got)
	}
	ctx := WithSessionID(context.Background(), " conv-1 ")
	if got := SessionIDFromContext(ctx); got != "conv-1" {
		t.Fatalf("id = %q, want conv-1", got)
	}
}

func TestRenderHeaderValueCases(t *testing.T) {
	cases := []struct {
		value string
		sid   string
		want  string
		ok    bool
	}{
		{"static", "s1", "static", true},
		{"%session_id%", "s1", "s1", true},
		{"a-%session_id%-b", "s1", "a-s1-b", true},
		{"%session_id%", "", "", false},
	}
	for _, c := range cases {
		got, ok := renderHeaderValue(c.value, c.sid)
		if ok != c.ok || got != c.want {
			t.Fatalf("renderHeaderValue(%q,%q) = (%q,%v), want (%q,%v)", c.value, c.sid, got, ok, c.want, c.ok)
		}
	}
}
