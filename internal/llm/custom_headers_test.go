// Regression tests: AI channel custom_headers must reach EVERY outbound model
// request on both protocol channels (PR #379, extended to claude/anthropic).
// Covered call shapes, matching the real wiring:
//   - claude probe shape:   llm.NewClaudeAgenticModel(ctx, cfg, nil, ...)   (handler/config.go:1338)
//   - claude agentic shape: same constructor with an explicit client       (multiagent / vision / claude_native)
//   - openai eino shape:    openai.NewEinoHTTPClient + einoopenai.ChatModel (multiagent wiring)
//   - openai client shape:  openai.NewClient(...).ChatCompletion            (legacy client / HITL audit)
package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/llm"
	"cyberstrike-ai/internal/openai"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

const (
	regProbeHeader   = "x-gap-probe"
	regSessionHeader = "x-gap-session"
	regProbeValue    = "sentinel-gap"
	regSessionID     = "conv-gap-42"
)

const regAnthropicOKBody = `{
	"id":"msg_1",
	"type":"message",
	"role":"assistant",
	"model":"claude-test",
	"content":[{"type":"text","text":"native ok"}],
	"stop_reason":"end_turn",
	"usage":{"input_tokens":1,"output_tokens":2}
}`

const regChatOKBody = `{
	"id":"chatcmpl-1",
	"object":"chat.completion",
	"created":1700000000,
	"model":"gpt-test",
	"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
	"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
}`

type regHeaderCapture struct {
	mu   sync.Mutex
	hdr  http.Header
	path string
}

func (c *regHeaderCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hdr = r.Header.Clone()
	c.path = r.URL.Path
}

func (c *regHeaderCapture) seen() http.Header {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hdr
}

func TestChannelCustomHeadersReachEveryOutboundCallShape(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		exercise func(t *testing.T, cfg *config.OpenAIConfig, ctx context.Context, srv *httptest.Server)
	}{
		{
			name:     "claude_probe_shape_nil_client",
			provider: "claude",
			model:    "claude-test",
			exercise: func(t *testing.T, cfg *config.OpenAIConfig, ctx context.Context, _ *httptest.Server) {
				// Mirrors internal/handler/config.go:1338 — the probe passes a nil client.
				m, err := llm.NewClaudeAgenticModel(ctx, *cfg, nil, 5, nil)
				if err != nil {
					t.Fatalf("NewClaudeAgenticModel: %v", err)
				}
				if _, err := m.Generate(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("hi")}); err != nil {
					t.Fatalf("claude Generate: %v", err)
				}
			},
		},
		{
			name:     "claude_agentic_shape_explicit_client",
			provider: "claude",
			model:    "claude-test",
			exercise: func(t *testing.T, cfg *config.OpenAIConfig, ctx context.Context, srv *httptest.Server) {
				// Mirrors multiagent/vision wiring that passes a non-nil client.
				m, err := llm.NewClaudeAgenticModel(ctx, *cfg, srv.Client(), 5, nil)
				if err != nil {
					t.Fatalf("NewClaudeAgenticModel: %v", err)
				}
				if _, err := m.Generate(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("hi")}); err != nil {
					t.Fatalf("claude Generate: %v", err)
				}
			},
		},
		{
			name:     "openai_eino_chat_model_control",
			provider: "openai",
			model:    "gpt-test",
			exercise: func(t *testing.T, cfg *config.OpenAIConfig, ctx context.Context, srv *httptest.Server) {
				// Control: PR #379 behavior on the eino OpenAI path must be unchanged.
				httpClient := openai.NewEinoHTTPClient(cfg, srv.Client())
				maxCompletionTokens := cfg.MaxCompletionTokensEffective()
				chatModel, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
					APIKey:              cfg.APIKey,
					BaseURL:             strings.TrimSuffix(cfg.BaseURL, "/"),
					Model:               cfg.Model,
					HTTPClient:          httpClient,
					MaxCompletionTokens: &maxCompletionTokens,
				})
				if err != nil {
					t.Fatalf("einoopenai.NewChatModel: %v", err)
				}
				if _, err := chatModel.Generate(ctx, []*schema.Message{{Role: schema.User, Content: "hi"}}); err != nil {
					t.Fatalf("openai Generate: %v", err)
				}
			},
		},
		{
			name:     "openai_builtin_client_control",
			provider: "openai",
			model:    "gpt-test",
			exercise: func(t *testing.T, cfg *config.OpenAIConfig, ctx context.Context, _ *httptest.Server) {
				// Control: PR #379 behavior on the built-in client path must be unchanged.
				client := openai.NewClient(cfg, nil, nil)
				var out struct {
					Choices []struct {
						Message struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
				}
				payload := map[string]interface{}{
					"model":                 cfg.Model,
					"messages":              []map[string]string{{"role": "user", "content": "hi"}},
					"max_completion_tokens": 5,
				}
				if err := client.ChatCompletion(ctx, payload, &out); err != nil {
					t.Fatalf("openai ChatCompletion: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &regHeaderCapture{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capture.record(r)
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/messages") {
					_, _ = w.Write([]byte(regAnthropicOKBody))
					return
				}
				_, _ = w.Write([]byte(regChatOKBody))
			}))
			defer srv.Close()

			cfg := config.AIChannelConfig{
				Name:     "custom-headers-regression",
				Provider: tc.provider,
				APIKey:   "test-key",
				BaseURL:  srv.URL,
				Model:    tc.model,
				Headers: map[string]string{
					regProbeHeader:   regProbeValue,
					regSessionHeader: "%session_id%",
					"x-gap-empty":    "", // empty-valued headers must never be sent
				},
			}.ToOpenAIConfig()

			// Injected exactly as the platform does (task_manager.go:590, probe config.go:1334).
			ctx := openai.WithSessionID(context.Background(), regSessionID)

			tc.exercise(t, &cfg, ctx, srv)

			h := capture.seen()
			if h == nil {
				t.Fatalf("no request reached %s", srv.URL)
			}
			if got := h.Get(regProbeHeader); got != regProbeValue {
				t.Fatalf("custom header %s = %q, want %q (configured headers must be sent on the %s channel)", regProbeHeader, got, regProbeValue, tc.provider)
			}
			if got := h.Get(regSessionHeader); got != regSessionID {
				t.Fatalf("session header %s = %q, want %q (%%session_id%% must render on the %s channel)", regSessionHeader, got, regSessionID, tc.provider)
			}
			if got := h.Get("x-gap-empty"); got != "" {
				t.Fatalf("empty-valued header must not be sent, got %q", got)
			}
			for name, values := range h {
				for _, v := range values {
					if strings.Contains(v, "%session_id%") {
						t.Fatalf("literal %%session_id%% leaked in header %s: %q", name, v)
					}
				}
			}
			// Built-in auth must survive when not overridden by configuration.
			if tc.provider == "claude" {
				if got := h.Get("x-api-key"); got != "test-key" {
					t.Fatalf("x-api-key = %q, want builtin test-key", got)
				}
			} else {
				if got := h.Get("Authorization"); got != "Bearer test-key" {
					t.Fatalf("Authorization = %q, want builtin Bearer test-key", got)
				}
			}
		})
	}
}

func TestClaudeAgenticModelWithoutHeadersKeepsClientNil(t *testing.T) {
	// No headers configured: NewClaudeAgenticModel must not invent a wrapped
	// client; nil stays nil and agenticclaude uses its SDK default (pre-PR behavior).
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(regAnthropicOKBody))
	}))
	defer srv.Close()

	cfg := config.AIChannelConfig{
		Name:     "no-headers",
		Provider: "claude",
		APIKey:   "test-key",
		BaseURL:  srv.URL,
		Model:    "claude-test",
	}.ToOpenAIConfig()

	m, err := llm.NewClaudeAgenticModel(context.Background(), cfg, nil, 5, nil)
	if err != nil {
		t.Fatalf("NewClaudeAgenticModel: %v", err)
	}
	if _, err := m.Generate(context.Background(), []*schema.AgenticMessage{schema.UserAgenticMessage("hi")}); err != nil {
		t.Fatalf("claude Generate: %v", err)
	}
	if !called {
		t.Fatalf("request did not reach the test server")
	}
}
