package multiagent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/llm"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/reasoning"

	agenticopenai "github.com/cloudwego/eino-ext/components/model/agenticopenai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type einoModelMode string

const (
	einoModelModeNormal  einoModelMode = "normal"
	einoModelModePlanner einoModelMode = "planner"
)

type einoModelFactory func(ctx context.Context, oa config.OpenAIConfig, mode einoModelMode) (model.ToolCallingChatModel, error)
type einoAgenticModelConfigFactory func(ctx context.Context, oa config.OpenAIConfig, mode einoModelMode) (model.AgenticModel, error)

func newEinoBaseHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Minute,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   300 * time.Second,
				KeepAlive: 300 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Minute,
		},
	}
}

func newEinoToolCallingChatModelFactory(
	baseHTTPClient *http.Client,
	reasoningClient *reasoning.ClientIntent,
	logger *zap.Logger,
) einoModelFactory {
	if baseHTTPClient == nil {
		baseHTTPClient = newEinoBaseHTTPClient()
	}
	return func(ctx context.Context, oa config.OpenAIConfig, mode einoModelMode) (model.ToolCallingChatModel, error) {
		if isEinoAgenticClaudeProvider(oa.Provider) {
			nativeModel, err := newEinoClaudeAgenticChatModel(ctx, oa, mode, baseHTTPClient, reasoningClient)
			if err != nil {
				return nil, err
			}
			return newAgenticToolCallingChatModelAdapter(nativeModel), nil
		}
		httpClient := openai.NewEinoHTTPClient(&oa, baseHTTPClient)
		openai.AttachSummarizationDiagTransport(httpClient, logger)
		maxCompletionTokens := oa.MaxCompletionTokensEffective()
		modelCfg := &einoopenai.ChatModelConfig{
			APIKey:              oa.APIKey,
			BaseURL:             strings.TrimSuffix(oa.BaseURL, "/"),
			Model:               oa.Model,
			HTTPClient:          httpClient,
			MaxCompletionTokens: &maxCompletionTokens,
		}
		if mode == einoModelModePlanner {
			reasoning.ApplyPlanExecutePlannerModelConfig(modelCfg, &oa)
		} else {
			reasoning.ApplyToEinoChatModelConfig(modelCfg, &oa, reasoningClient)
		}
		baseModel, err := einoopenai.NewChatModel(ctx, modelCfg)
		if err != nil {
			return nil, err
		}
		return newStreamToolCallIndexRepairModel(baseModel), nil
	}
}

func newEinoAgenticChatModelFactory(
	baseHTTPClient *http.Client,
	reasoningClient *reasoning.ClientIntent,
	logger *zap.Logger,
) einoAgenticModelConfigFactory {
	if baseHTTPClient == nil {
		baseHTTPClient = newEinoBaseHTTPClient()
	}
	return func(ctx context.Context, oa config.OpenAIConfig, mode einoModelMode) (model.AgenticModel, error) {
		if !supportsEinoAgenticBackend(oa) {
			return nil, fmt.Errorf("eino agentic model: provider %q is not supported", strings.TrimSpace(oa.Provider))
		}
		if isEinoAgenticClaudeProvider(oa.Provider) {
			nativeModel, err := newEinoClaudeAgenticChatModel(ctx, oa, mode, baseHTTPClient, reasoningClient)
			if err != nil {
				return nil, err
			}
			return newAgenticStreamBlockIndexRepairModel(nativeModel), nil
		}
		httpClient := openai.NewEinoHTTPClient(&oa, baseHTTPClient)
		openai.AttachSummarizationDiagTransport(httpClient, logger)
		maxCompletionTokens := oa.MaxCompletionTokensEffective()
		modelCfg := &agenticopenai.ChatConfig{
			APIKey:              oa.APIKey,
			BaseURL:             strings.TrimSuffix(oa.BaseURL, "/"),
			Model:               oa.Model,
			HTTPClient:          httpClient,
			MaxCompletionTokens: &maxCompletionTokens,
			ExtraFields:         reasoning.AgenticOpenAIExtraFields(&oa, reasoningClient),
		}
		if mode == einoModelModePlanner {
			modelCfg.ExtraFields = reasoning.AgenticOpenAIPlannerExtraFields(&oa)
		}
		chatModel, err := agenticopenai.NewChatModel(ctx, modelCfg)
		if err != nil {
			return nil, err
		}
		return newAgenticStreamBlockIndexRepairModel(chatModel), nil
	}
}

func newEinoClaudeAgenticChatModel(
	ctx context.Context,
	oa config.OpenAIConfig,
	mode einoModelMode,
	httpClient *http.Client,
	reasoningClient *reasoning.ClientIntent,
) (model.AgenticModel, error) {
	extraFields := reasoning.AgenticOpenAIExtraFields(&oa, reasoningClient)
	if mode == einoModelModePlanner {
		extraFields = reasoning.AgenticOpenAIPlannerExtraFields(&oa)
	}
	return llm.NewClaudeAgenticModel(
		ctx,
		oa,
		httpClient,
		oa.MaxCompletionTokensEffective(),
		extraFields,
	)
}

func supportsEinoAgenticBackend(oa config.OpenAIConfig) bool {
	provider := strings.ToLower(strings.TrimSpace(oa.Provider))
	return provider == "" ||
		provider == "openai" ||
		provider == "openai_compatible" ||
		isEinoAgenticClaudeProvider(provider)
}

func isEinoAgenticClaudeProvider(provider string) bool {
	return llm.IsClaudeProvider(provider)
}

func agenticModelGateFactory(factory einoAgenticModelConfigFactory, oa config.OpenAIConfig, mode einoModelMode) einoAgenticModelFactory {
	if factory == nil {
		return nil
	}
	return func(ctx context.Context) (model.AgenticModel, error) {
		return factory(ctx, oa, mode)
	}
}

func newEinoModelRetryConfig(
	mw *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	scope string,
) *adk.ModelRetryConfig {
	maxRetries := RunRetryMaxAttemptsFromConfig(mw)
	maxBackoff := einoRunRetryMaxBackoffFromConfig(mw)
	return &adk.ModelRetryConfig{
		MaxRetries: maxRetries,
		BackoffFunc: func(_ context.Context, attempt int) time.Duration {
			return einoTransientRetryBackoff(attempt-1, maxBackoff)
		},
		ShouldRetry: func(ctx context.Context, retryCtx *adk.RetryContext) *adk.RetryDecision {
			if retryCtx == nil || ctx.Err() != nil {
				return &adk.RetryDecision{}
			}
			if retryCtx.Err != nil {
				if !isEinoTransientRunError(retryCtx.Err) {
					return &adk.RetryDecision{}
				}
				if logger != nil {
					kind, summary := einoTransientRunErrorUserDetail(retryCtx.Err)
					logger.Warn("eino native model retry",
						zap.String("scope", scope),
						zap.Int("attempt", retryCtx.RetryAttempt),
						zap.Int("maxRetries", maxRetries),
						zap.String("errorKind", kind),
						zap.String("errorSummary", summary),
					)
				}
				return &adk.RetryDecision{Retry: true, RejectReason: "transient_model_error"}
			}
			if isRetryableEmptyModelOutput(retryCtx.OutputMessage) {
				if logger != nil {
					logger.Warn("eino native model retry: empty model output",
						zap.String("scope", scope),
						zap.Int("attempt", retryCtx.RetryAttempt),
						zap.Int("maxRetries", maxRetries),
					)
				}
				return &adk.RetryDecision{Retry: true, RejectReason: "empty_model_output"}
			}
			return &adk.RetryDecision{}
		},
	}
}

func newEinoAgenticModelRetryConfig(
	mw *config.MultiAgentEinoMiddlewareConfig,
	logger *zap.Logger,
	scope string,
) *adk.TypedModelRetryConfig[*schema.AgenticMessage] {
	maxRetries := RunRetryMaxAttemptsFromConfig(mw)
	maxBackoff := einoRunRetryMaxBackoffFromConfig(mw)
	return &adk.TypedModelRetryConfig[*schema.AgenticMessage]{
		MaxRetries: maxRetries,
		BackoffFunc: func(_ context.Context, attempt int) time.Duration {
			return einoTransientRetryBackoff(attempt-1, maxBackoff)
		},
		ShouldRetry: func(ctx context.Context, retryCtx *adk.TypedRetryContext[*schema.AgenticMessage]) *adk.TypedRetryDecision[*schema.AgenticMessage] {
			if retryCtx == nil || ctx.Err() != nil {
				return &adk.TypedRetryDecision[*schema.AgenticMessage]{}
			}
			if retryCtx.Err != nil {
				if !isEinoTransientRunError(retryCtx.Err) {
					return &adk.TypedRetryDecision[*schema.AgenticMessage]{}
				}
				if logger != nil {
					kind, summary := einoTransientRunErrorUserDetail(retryCtx.Err)
					logger.Warn("eino native agentic model retry",
						zap.String("scope", scope),
						zap.Int("attempt", retryCtx.RetryAttempt),
						zap.Int("maxRetries", maxRetries),
						zap.String("errorKind", kind),
						zap.String("errorSummary", summary),
					)
				}
				return &adk.TypedRetryDecision[*schema.AgenticMessage]{Retry: true, RejectReason: "transient_model_error"}
			}
			if isRetryableEmptyAgenticModelOutput(retryCtx.OutputMessage) {
				if logger != nil {
					logger.Warn("eino native agentic model retry: empty model output",
						zap.String("scope", scope),
						zap.Int("attempt", retryCtx.RetryAttempt),
						zap.Int("maxRetries", maxRetries),
					)
				}
				return &adk.TypedRetryDecision[*schema.AgenticMessage]{Retry: true, RejectReason: "empty_model_output"}
			}
			return &adk.TypedRetryDecision[*schema.AgenticMessage]{}
		},
	}
}

func newEinoModelFailoverConfig(
	ctx context.Context,
	appCfg *config.Config,
	mw *config.MultiAgentEinoMiddlewareConfig,
	mode einoModelMode,
	factory einoModelFactory,
	logger *zap.Logger,
	scope string,
	progress func(eventType, message string, data interface{}),
	orchestration string,
	conversationID string,
) (*adk.ModelFailoverConfig[*schema.Message], error) {
	channels := resolveEinoFailoverChannels(appCfg, mw)
	if len(channels) == 0 {
		return nil, nil
	}
	if factory == nil {
		return nil, fmt.Errorf("eino model failover: 模型工厂为空")
	}

	maxRetries := len(channels)
	if mw != nil && mw.ModelFailoverMaxRetries > 0 && mw.ModelFailoverMaxRetries < maxRetries {
		maxRetries = mw.ModelFailoverMaxRetries
	}
	channels = channels[:maxRetries]

	cache := make(map[string]model.BaseModel[*schema.Message], len(channels))
	var mu sync.Mutex
	return &adk.ModelFailoverConfig[*schema.Message]{
		MaxRetries: uint(maxRetries),
		ShouldFailover: func(ctx context.Context, _ *schema.Message, err error) bool {
			if ctx.Err() != nil || err == nil {
				return false
			}
			err = unwrapEinoRetryExhausted(err)
			return isEinoTransientRunError(err)
		},
		GetFailoverModel: func(ctx context.Context, failoverCtx *adk.FailoverContext[*schema.Message]) (model.BaseModel[*schema.Message], []*schema.Message, error) {
			if failoverCtx == nil || failoverCtx.FailoverAttempt == 0 {
				return nil, nil, fmt.Errorf("eino model failover: invalid failover attempt")
			}
			idx := int(failoverCtx.FailoverAttempt) - 1
			if idx < 0 || idx >= len(channels) {
				return nil, nil, fmt.Errorf("eino model failover: no channel for attempt %d", failoverCtx.FailoverAttempt)
			}
			ch := channels[idx]
			mu.Lock()
			cached := cache[ch.id]
			mu.Unlock()
			if cached != nil {
				emitEinoModelFailoverEvent(progress, conversationID, orchestration, scope, ch.id, ch.cfg.Model, failoverCtx.FailoverAttempt)
				if logger != nil {
					logger.Warn("eino native model failover",
						zap.String("scope", scope),
						zap.String("channel", ch.id),
						zap.String("model", ch.cfg.Model),
						zap.Uint("attempt", failoverCtx.FailoverAttempt),
					)
				}
				return cached, nil, nil
			}
			m, err := factory(ctx, ch.cfg, mode)
			if err != nil {
				return nil, nil, fmt.Errorf("eino model failover channel %q: %w", ch.id, err)
			}
			mu.Lock()
			cache[ch.id] = m
			mu.Unlock()
			emitEinoModelFailoverEvent(progress, conversationID, orchestration, scope, ch.id, ch.cfg.Model, failoverCtx.FailoverAttempt)
			if logger != nil {
				logger.Warn("eino native model failover",
					zap.String("scope", scope),
					zap.String("channel", ch.id),
					zap.String("model", ch.cfg.Model),
					zap.Uint("attempt", failoverCtx.FailoverAttempt),
				)
			}
			return m, nil, nil
		},
	}, nil
}

func newEinoAgenticModelFailoverConfig(
	ctx context.Context,
	appCfg *config.Config,
	mw *config.MultiAgentEinoMiddlewareConfig,
	mode einoModelMode,
	factory einoAgenticModelConfigFactory,
	logger *zap.Logger,
	scope string,
	progress func(eventType, message string, data interface{}),
	orchestration string,
	conversationID string,
) (*adk.ModelFailoverConfig[*schema.AgenticMessage], error) {
	channels := resolveEinoFailoverChannels(appCfg, mw)
	if len(channels) == 0 {
		return nil, nil
	}
	if factory == nil {
		return nil, fmt.Errorf("eino agentic model failover: 模型工厂为空")
	}

	maxRetries := len(channels)
	if mw != nil && mw.ModelFailoverMaxRetries > 0 && mw.ModelFailoverMaxRetries < maxRetries {
		maxRetries = mw.ModelFailoverMaxRetries
	}
	channels = channels[:maxRetries]

	cache := make(map[string]model.BaseModel[*schema.AgenticMessage], len(channels))
	var mu sync.Mutex
	return &adk.ModelFailoverConfig[*schema.AgenticMessage]{
		MaxRetries: uint(maxRetries),
		ShouldFailover: func(ctx context.Context, _ *schema.AgenticMessage, err error) bool {
			if ctx.Err() != nil || err == nil {
				return false
			}
			err = unwrapEinoRetryExhausted(err)
			return isEinoTransientRunError(err)
		},
		GetFailoverModel: func(ctx context.Context, failoverCtx *adk.FailoverContext[*schema.AgenticMessage]) (model.BaseModel[*schema.AgenticMessage], []*schema.AgenticMessage, error) {
			if failoverCtx == nil || failoverCtx.FailoverAttempt == 0 {
				return nil, nil, fmt.Errorf("eino agentic model failover: invalid failover attempt")
			}
			idx := int(failoverCtx.FailoverAttempt) - 1
			if idx < 0 || idx >= len(channels) {
				return nil, nil, fmt.Errorf("eino agentic model failover: no channel for attempt %d", failoverCtx.FailoverAttempt)
			}
			ch := channels[idx]
			mu.Lock()
			cached := cache[ch.id]
			mu.Unlock()
			if cached != nil {
				emitEinoModelFailoverEvent(progress, conversationID, orchestration, scope, ch.id, ch.cfg.Model, failoverCtx.FailoverAttempt)
				if logger != nil {
					logger.Warn("eino native agentic model failover",
						zap.String("scope", scope),
						zap.String("channel", ch.id),
						zap.String("model", ch.cfg.Model),
						zap.Uint("attempt", failoverCtx.FailoverAttempt),
					)
				}
				return cached, nil, nil
			}
			m, err := factory(ctx, ch.cfg, mode)
			if err != nil {
				return nil, nil, fmt.Errorf("eino agentic model failover channel %q: %w", ch.id, err)
			}
			mu.Lock()
			cache[ch.id] = m
			mu.Unlock()
			emitEinoModelFailoverEvent(progress, conversationID, orchestration, scope, ch.id, ch.cfg.Model, failoverCtx.FailoverAttempt)
			if logger != nil {
				logger.Warn("eino native agentic model failover",
					zap.String("scope", scope),
					zap.String("channel", ch.id),
					zap.String("model", ch.cfg.Model),
					zap.Uint("attempt", failoverCtx.FailoverAttempt),
				)
			}
			return m, nil, nil
		},
	}, nil
}

type resolvedEinoFailoverChannel struct {
	id  string
	cfg config.OpenAIConfig
}

func resolveEinoFailoverChannels(appCfg *config.Config, mw *config.MultiAgentEinoMiddlewareConfig) []resolvedEinoFailoverChannel {
	if appCfg == nil || mw == nil || len(mw.ModelFailoverChannels) == 0 {
		return nil
	}
	primary := appCfg.OpenAI
	seen := map[string]struct{}{}
	out := make([]resolvedEinoFailoverChannel, 0, len(mw.ModelFailoverChannels))
	for _, raw := range mw.ModelFailoverChannels {
		id := config.NormalizeAIChannelID(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		oa, resolvedID, ok := appCfg.AI.ResolveChannel(id)
		if !ok {
			continue
		}
		if sameOpenAIModelEndpoint(primary, oa) {
			continue
		}
		seen[resolvedID] = struct{}{}
		out = append(out, resolvedEinoFailoverChannel{id: resolvedID, cfg: oa})
	}
	return out
}

func sameOpenAIModelEndpoint(a, b config.OpenAIConfig) bool {
	return strings.EqualFold(strings.TrimSpace(a.Provider), strings.TrimSpace(b.Provider)) &&
		strings.TrimRight(strings.TrimSpace(a.BaseURL), "/") == strings.TrimRight(strings.TrimSpace(b.BaseURL), "/") &&
		strings.TrimSpace(a.APIKey) == strings.TrimSpace(b.APIKey) &&
		strings.TrimSpace(a.Model) == strings.TrimSpace(b.Model)
}

func isRetryableEmptyModelOutput(msg *schema.Message) bool {
	if msg == nil {
		return true
	}
	return strings.TrimSpace(msg.Content) == "" &&
		strings.TrimSpace(msg.ReasoningContent) == "" &&
		len(msg.ToolCalls) == 0 &&
		len(msg.MultiContent) == 0 &&
		len(msg.UserInputMultiContent) == 0 &&
		len(msg.AssistantGenMultiContent) == 0
}

func isRetryableEmptyAgenticModelOutput(msg *schema.AgenticMessage) bool {
	if msg == nil {
		return true
	}
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		switch {
		case block.Reasoning != nil:
			if strings.TrimSpace(block.Reasoning.Text) != "" {
				return false
			}
		case block.UserInputText != nil:
			if strings.TrimSpace(block.UserInputText.Text) != "" {
				return false
			}
		case block.AssistantGenText != nil:
			if strings.TrimSpace(block.AssistantGenText.Text) != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func unwrapEinoRetryExhausted(err error) error {
	var retryErr *adk.RetryExhaustedError
	if errors.As(err, &retryErr) && retryErr.LastErr != nil {
		return retryErr.LastErr
	}
	return err
}

func isEinoNativeWillRetry(err error) (*adk.WillRetryError, bool) {
	var willRetry *adk.WillRetryError
	if errors.As(err, &willRetry) {
		return willRetry, true
	}
	return nil, false
}

func emitEinoModelFailoverEvent(
	progress func(eventType, message string, data interface{}),
	conversationID, orchestration, scope, channelID, modelName string,
	attempt uint,
) {
	if progress == nil {
		return
	}
	msg := fmt.Sprintf("主模型重试耗尽，正在切换备用模型 %s。", modelName)
	progress("eino_model_failover", msg, map[string]interface{}{
		"conversationId": conversationID,
		"source":         "eino",
		"orchestration":  orchestration,
		"scope":          scope,
		"channel":        channelID,
		"model":          modelName,
		"attempt":        attempt,
	})
}
