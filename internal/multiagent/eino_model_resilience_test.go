package multiagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestNewEinoModelRetryConfigUsesNativeFieldsFirst(t *testing.T) {
	t.Parallel()
	mw := &config.MultiAgentEinoMiddlewareConfig{
		ModelRetryMaxRetries:    2,
		ModelRetryMaxBackoffSec: 7,
		RunRetryMaxAttempts:     9,
		RunRetryMaxBackoffSec:   11,
	}
	cfg := newEinoModelRetryConfig(mw, nil, "test")
	if cfg.MaxRetries != 2 {
		t.Fatalf("MaxRetries = %d, want 2", cfg.MaxRetries)
	}
	backoff := cfg.BackoffFunc(context.Background(), 1)
	if backoff < 500*time.Millisecond || backoff > 2*time.Second {
		t.Fatalf("attempt 1 backoff = %v, want first equal-jitter window", backoff)
	}
	if got := einoRunRetryMaxBackoffFromConfig(mw); got != 7*time.Second {
		t.Fatalf("backoff from config = %v, want 7s", got)
	}
}

func TestEinoModelRetryPolicyRetriesTransientAndEmptyOutput(t *testing.T) {
	t.Parallel()
	cfg := newEinoModelRetryConfig(&config.MultiAgentEinoMiddlewareConfig{ModelRetryMaxRetries: 1}, nil, "test")
	if got := cfg.ShouldRetry(context.Background(), &adk.RetryContext{Err: errors.New("HTTP 429 Too Many Requests")}); got == nil || !got.Retry {
		t.Fatal("transient model error should retry")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.RetryContext{OutputMessage: schema.AssistantMessage("", nil)}); got == nil || !got.Retry {
		t.Fatal("empty assistant output should retry")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.RetryContext{OutputMessage: schema.AssistantMessage("", []schema.ToolCall{{ID: "call_1"}})}); got == nil || got.Retry {
		t.Fatal("assistant tool call output should not be treated as empty")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.RetryContext{Err: errors.New("invalid api key")}); got == nil || got.Retry {
		t.Fatal("permanent auth error should not retry")
	}
}

func TestEinoAgenticModelRetryPolicyRetriesTransientAndEmptyOutput(t *testing.T) {
	t.Parallel()
	cfg := newEinoAgenticModelRetryConfig(&config.MultiAgentEinoMiddlewareConfig{ModelRetryMaxRetries: 1}, nil, "agentic")
	if got := cfg.ShouldRetry(context.Background(), &adk.TypedRetryContext[*schema.AgenticMessage]{Err: errors.New("HTTP 429 Too Many Requests")}); got == nil || !got.Retry {
		t.Fatal("transient agentic model error should retry")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.TypedRetryContext[*schema.AgenticMessage]{
		OutputMessage: &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant},
	}); got == nil || !got.Retry {
		t.Fatal("empty agentic assistant output should retry")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.TypedRetryContext[*schema.AgenticMessage]{
		OutputMessage: &schema.AgenticMessage{
			Role:          schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "ok"})},
		},
	}); got == nil || got.Retry {
		t.Fatal("agentic assistant text should not be treated as empty")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.TypedRetryContext[*schema.AgenticMessage]{
		OutputMessage: &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolCall{
				CallID: "call_1", Name: "search", Arguments: `{"q":"x"}`,
			})},
		},
	}); got == nil || got.Retry {
		t.Fatal("agentic tool call output should not be treated as empty")
	}
	if got := cfg.ShouldRetry(context.Background(), &adk.TypedRetryContext[*schema.AgenticMessage]{Err: errors.New("invalid api key")}); got == nil || got.Retry {
		t.Fatal("permanent auth error should not retry")
	}
}

func TestResolveEinoFailoverChannelsSkipsPrimaryDuplicateAndUnknown(t *testing.T) {
	t.Parallel()
	appCfg := &config.Config{
		OpenAI: config.OpenAIConfig{Provider: "openai", APIKey: "k1", BaseURL: "https://api.example/v1", Model: "primary"},
		AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{
			"same": {Provider: "openai", APIKey: "k1", BaseURL: "https://api.example/v1", Model: "primary"},
			"fb1":  {Provider: "openai", APIKey: "k2", BaseURL: "https://api.example/v1", Model: "fallback-1"},
			"fb2":  {Provider: "claude", APIKey: "k3", BaseURL: "https://api.anthropic.com/v1", Model: "claude-sonnet"},
		}},
	}
	got := resolveEinoFailoverChannels(appCfg, &config.MultiAgentEinoMiddlewareConfig{
		ModelFailoverChannels:   []string{"same", "missing", "fb1", "fb1", "fb2"},
		ModelFailoverMaxRetries: 1,
	})
	if len(got) != 2 {
		t.Fatalf("resolved channels len = %d, want 2 before max cap is applied by config builder", len(got))
	}
	if got[0].id != "fb1" || got[1].id != "fb2" {
		t.Fatalf("resolved channel order = %#v", got)
	}
}

func TestNewEinoModelFailoverConfigBuildsDistinctFallbackModel(t *testing.T) {
	t.Parallel()
	appCfg := &config.Config{
		OpenAI: config.OpenAIConfig{APIKey: "k1", BaseURL: "https://api.example/v1", Model: "primary"},
		AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{
			"fb1": {APIKey: "k2", BaseURL: "https://api.example/v1", Model: "fallback-1"},
			"fb2": {APIKey: "k3", BaseURL: "https://api.example/v1", Model: "fallback-2"},
		}},
	}
	var built []string
	cfg, err := newEinoModelFailoverConfig(
		context.Background(),
		appCfg,
		&config.MultiAgentEinoMiddlewareConfig{
			ModelFailoverChannels:   []string{"fb1", "fb2"},
			ModelFailoverMaxRetries: 1,
		},
		einoModelModeNormal,
		func(_ context.Context, oa config.OpenAIConfig, _ einoModelMode) (model.ToolCallingChatModel, error) {
			built = append(built, oa.Model)
			return &streamToolCallIndexFakeModel{}, nil
		},
		nil,
		"test",
		nil,
		"deep",
		"conv-1",
	)
	if err != nil {
		t.Fatalf("newEinoModelFailoverConfig: %v", err)
	}
	if cfg == nil || cfg.MaxRetries != 1 {
		t.Fatalf("failover cfg = %#v, want max retries 1", cfg)
	}
	m, msgs, err := cfg.GetFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{FailoverAttempt: 1})
	if err != nil || m == nil || msgs != nil {
		t.Fatalf("GetFailoverModel = (%v, %v, %v)", m, msgs, err)
	}
	if len(built) != 1 || built[0] != "fallback-1" {
		t.Fatalf("built models = %v, want [fallback-1]", built)
	}
	if !cfg.ShouldFailover(context.Background(), nil, &adk.RetryExhaustedError{LastErr: errors.New("upstream returned 503"), TotalRetries: 4}) {
		t.Fatal("retry-exhausted transient error should fail over")
	}
	if cfg.ShouldFailover(context.Background(), nil, &adk.RetryExhaustedError{LastErr: errors.New("invalid api key"), TotalRetries: 4}) {
		t.Fatal("retry-exhausted permanent error should not fail over")
	}
}

func TestNewEinoModelFailoverConfigEmitsProgressEvent(t *testing.T) {
	t.Parallel()
	appCfg := &config.Config{
		OpenAI: config.OpenAIConfig{APIKey: "k1", BaseURL: "https://api.example/v1", Model: "primary"},
		AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{
			"fb1": {APIKey: "k2", BaseURL: "https://api.example/v1", Model: "fallback-1"},
		}},
	}
	var events []struct {
		eventType string
		message   string
		data      interface{}
	}
	cfg, err := newEinoModelFailoverConfig(
		context.Background(),
		appCfg,
		&config.MultiAgentEinoMiddlewareConfig{ModelFailoverChannels: []string{"fb1"}},
		einoModelModeNormal,
		func(_ context.Context, _ config.OpenAIConfig, _ einoModelMode) (model.ToolCallingChatModel, error) {
			return &streamToolCallIndexFakeModel{}, nil
		},
		nil,
		"test",
		func(eventType, message string, data interface{}) {
			events = append(events, struct {
				eventType string
				message   string
				data      interface{}
			}{eventType: eventType, message: message, data: data})
		},
		"deep",
		"conv-1",
	)
	if err != nil {
		t.Fatalf("newEinoModelFailoverConfig: %v", err)
	}
	if _, _, err := cfg.GetFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{FailoverAttempt: 1}); err != nil {
		t.Fatalf("GetFailoverModel: %v", err)
	}
	if len(events) != 1 || events[0].eventType != "eino_model_failover" {
		t.Fatalf("events = %#v, want one eino_model_failover", events)
	}
	payload, ok := events[0].data.(map[string]interface{})
	if !ok {
		t.Fatalf("event payload type = %T", events[0].data)
	}
	if payload["conversationId"] != "conv-1" || payload["orchestration"] != "deep" || payload["channel"] != "fb1" || payload["model"] != "fallback-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestNewEinoAgenticModelFailoverConfigBuildsDistinctFallbackModel(t *testing.T) {
	t.Parallel()
	appCfg := &config.Config{
		OpenAI: config.OpenAIConfig{Provider: "openai", APIKey: "k1", BaseURL: "https://api.example/v1", Model: "primary"},
		AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{
			"fb1": {Provider: "openai", APIKey: "k2", BaseURL: "https://api.example/v1", Model: "fallback-1"},
			"fb2": {Provider: "openai", APIKey: "k3", BaseURL: "https://api.example/v1", Model: "fallback-2"},
		}},
	}
	var built []string
	cfg, err := newEinoAgenticModelFailoverConfig(
		context.Background(),
		appCfg,
		&config.MultiAgentEinoMiddlewareConfig{
			ModelFailoverChannels:   []string{"fb1", "fb2"},
			ModelFailoverMaxRetries: 1,
		},
		einoModelModeNormal,
		func(_ context.Context, oa config.OpenAIConfig, _ einoModelMode) (model.AgenticModel, error) {
			built = append(built, oa.Model)
			return &fakeAgenticGateModel{}, nil
		},
		nil,
		"agentic",
		nil,
		"eino_single_agentic",
		"conv-1",
	)
	if err != nil {
		t.Fatalf("newEinoAgenticModelFailoverConfig: %v", err)
	}
	if cfg == nil || cfg.MaxRetries != 1 {
		t.Fatalf("agentic failover cfg = %#v, want max retries 1", cfg)
	}
	m, msgs, err := cfg.GetFailoverModel(context.Background(), &adk.FailoverContext[*schema.AgenticMessage]{FailoverAttempt: 1})
	if err != nil || m == nil || msgs != nil {
		t.Fatalf("GetFailoverModel = (%v, %v, %v)", m, msgs, err)
	}
	if len(built) != 1 || built[0] != "fallback-1" {
		t.Fatalf("built models = %v, want [fallback-1]", built)
	}
	if !cfg.ShouldFailover(context.Background(), nil, &adk.RetryExhaustedError{LastErr: errors.New("upstream returned 503"), TotalRetries: 4}) {
		t.Fatal("retry-exhausted transient agentic error should fail over")
	}
	if cfg.ShouldFailover(context.Background(), nil, &adk.RetryExhaustedError{LastErr: errors.New("invalid api key"), TotalRetries: 4}) {
		t.Fatal("retry-exhausted permanent agentic error should not fail over")
	}
}

func TestNewEinoAgenticModelFailoverConfigEmitsProgressEvent(t *testing.T) {
	t.Parallel()
	appCfg := &config.Config{
		OpenAI: config.OpenAIConfig{Provider: "openai", APIKey: "k1", BaseURL: "https://api.example/v1", Model: "primary"},
		AI: config.AIConfig{Channels: map[string]config.AIChannelConfig{
			"fb1": {Provider: "openai", APIKey: "k2", BaseURL: "https://api.example/v1", Model: "fallback-1"},
		}},
	}
	var events []struct {
		eventType string
		message   string
		data      interface{}
	}
	cfg, err := newEinoAgenticModelFailoverConfig(
		context.Background(),
		appCfg,
		&config.MultiAgentEinoMiddlewareConfig{ModelFailoverChannels: []string{"fb1"}},
		einoModelModeNormal,
		func(_ context.Context, _ config.OpenAIConfig, _ einoModelMode) (model.AgenticModel, error) {
			return &fakeAgenticGateModel{}, nil
		},
		nil,
		"agentic",
		func(eventType, message string, data interface{}) {
			events = append(events, struct {
				eventType string
				message   string
				data      interface{}
			}{eventType: eventType, message: message, data: data})
		},
		"eino_single_agentic",
		"conv-1",
	)
	if err != nil {
		t.Fatalf("newEinoAgenticModelFailoverConfig: %v", err)
	}
	if _, _, err := cfg.GetFailoverModel(context.Background(), &adk.FailoverContext[*schema.AgenticMessage]{FailoverAttempt: 1}); err != nil {
		t.Fatalf("GetFailoverModel: %v", err)
	}
	if len(events) != 1 || events[0].eventType != "eino_model_failover" {
		t.Fatalf("events = %#v, want one eino_model_failover", events)
	}
	payload, ok := events[0].data.(map[string]interface{})
	if !ok {
		t.Fatalf("event payload type = %T", events[0].data)
	}
	if payload["conversationId"] != "conv-1" || payload["orchestration"] != "eino_single_agentic" || payload["channel"] != "fb1" || payload["model"] != "fallback-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestNewEinoAgenticChatModelFactoryBuildsOpenAIBackend(t *testing.T) {
	t.Parallel()
	factory := newEinoAgenticChatModelFactory(newEinoBaseHTTPClient(), nil, nil)
	m, err := factory(context.Background(), config.OpenAIConfig{
		Provider: "openai",
		APIKey:   "test-key",
		BaseURL:  "https://api.example/v1",
		Model:    "gpt-4o-mini",
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
			Mode:    "on",
			Effort:  "high",
		},
	}, einoModelModeNormal)
	if err != nil {
		t.Fatalf("agentic factory: %v", err)
	}
	if m == nil {
		t.Fatal("agentic factory returned nil model")
	}
	gate := evaluateEinoAgenticModelGate(agenticModelGateFactory(factory, config.OpenAIConfig{
		Provider: "openai",
		APIKey:   "test-key",
		BaseURL:  "https://api.example/v1",
		Model:    "gpt-4o-mini",
	}, einoModelModeNormal), einoAgenticRuntimeSupportV0914())
	if !gate.Ready {
		t.Fatalf("gate = %#v, want ready with buildable agentic backend", gate)
	}
}

func TestNewEinoAgenticChatModelFactoryBuildsNativeClaudeBackend(t *testing.T) {
	t.Parallel()
	factory := newEinoAgenticChatModelFactory(newEinoBaseHTTPClient(), nil, nil)
	m, err := factory(context.Background(), config.OpenAIConfig{
		Provider: "claude",
		APIKey:   "test-key",
		BaseURL:  "https://api.anthropic.com/v1",
		Model:    "claude-sonnet-4",
	}, einoModelModeNormal)
	if err != nil {
		t.Fatalf("claude agentic factory: %v", err)
	}
	if m == nil {
		t.Fatal("claude agentic factory returned nil model")
	}
	if _, ok := m.(*agenticStreamBlockIndexRepairModel); !ok {
		t.Fatalf("claude agentic factory returned %T, want block-index-repair wrapper around native agenticclaude.Model", m)
	}
	gate := evaluateEinoAgenticModelGate(agenticModelGateFactory(factory, config.OpenAIConfig{
		Provider: "claude",
		APIKey:   "test-key",
		BaseURL:  "https://api.anthropic.com/v1",
		Model:    "claude-sonnet-4",
	}, einoModelModeNormal), einoAgenticRuntimeSupportV0914())
	if !gate.Ready {
		t.Fatalf("gate = %#v, want ready with native Claude backend", gate)
	}
}

func TestNewEinoToolCallingChatModelFactoryUsesNativeClaudeAdapter(t *testing.T) {
	t.Parallel()
	factory := newEinoToolCallingChatModelFactory(newEinoBaseHTTPClient(), nil, nil)
	m, err := factory(context.Background(), config.OpenAIConfig{
		Provider: "claude",
		APIKey:   "test-key",
		BaseURL:  "https://api.anthropic.com",
		Model:    "claude-sonnet-4",
	}, einoModelModePlanner)
	if err != nil {
		t.Fatalf("claude planner factory: %v", err)
	}
	if _, ok := m.(*agenticToolCallingChatModelAdapter); !ok {
		t.Fatalf("claude planner factory returned %T, want native agentic adapter", m)
	}
}

func TestEinoNativeRetryErrorsDoNotTriggerRunLevelTransientRetry(t *testing.T) {
	t.Parallel()
	err := &adk.WillRetryError{ErrStr: "HTTP 429 Too Many Requests", RetryAttempt: 1}
	if isEinoTransientRunError(err) {
		t.Fatal("WillRetryError should be observed, not treated as a run-level transient failure")
	}
	exhausted := &adk.RetryExhaustedError{LastErr: errors.New("HTTP 429 Too Many Requests"), TotalRetries: 4}
	if isEinoTransientRunError(exhausted) {
		t.Fatal("RetryExhaustedError should not trigger a second run-level retry layer")
	}
	if got := unwrapEinoRetryExhausted(exhausted); got == exhausted {
		t.Fatal("unwrapEinoRetryExhausted should return the underlying model error")
	}
}
