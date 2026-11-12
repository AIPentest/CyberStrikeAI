package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"

	einoembedopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Embedder Use CloudWeGo Eino's OpenAI Embedding component, keeping rate limiting and retries.
type Embedder struct {
	eino   embedding.Embedder
	config *config.KnowledgeConfig
	logger *zap.Logger

	rateLimiter    *rate.Limiter
	rateLimitDelay time.Duration
	maxRetries     int
	retryDelay     time.Duration
	mu             sync.Mutex
}

// NewEmbedder Based on the Eino eino-ext OpenAI Embedder; openAIConfig is used as a fallback when the knowledge base has no separately configured key API Key.
func NewEmbedder(ctx context.Context, cfg *config.KnowledgeConfig, openAIConfig *config.OpenAIConfig, logger *zap.Logger) (*Embedder, error) {
	if cfg == nil {
		return nil, fmt.Errorf("knowledge config is nil")
	}

	var rateLimiter *rate.Limiter
	var rateLimitDelay time.Duration
	if cfg.Indexing.MaxRPM > 0 {
		rpm := cfg.Indexing.MaxRPM
		rateLimiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), rpm)
		if logger != nil {
			logger.Info("Knowledge-base index rate limiting enabled", zap.Int("maxRPM", rpm))
		}
	} else if cfg.Indexing.RateLimitDelayMs > 0 {
		rateLimitDelay = time.Duration(cfg.Indexing.RateLimitDelayMs) * time.Millisecond
		if logger != nil {
			logger.Info("Knowledge-base index fixed delay enabled", zap.Duration("delay", rateLimitDelay))
		}
	}

	maxRetries := 3
	retryDelay := 1000 * time.Millisecond
	if cfg.Indexing.MaxRetries > 0 {
		maxRetries = cfg.Indexing.MaxRetries
	}
	if cfg.Indexing.RetryDelayMs > 0 {
		retryDelay = time.Duration(cfg.Indexing.RetryDelayMs) * time.Millisecond
	}

	model := strings.TrimSpace(cfg.Embedding.Model)
	if model == "" {
		model = "text-embedding-3-small"
	}

	baseURL := strings.TrimSpace(cfg.Embedding.BaseURL)
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := strings.TrimSpace(cfg.Embedding.APIKey)
	if apiKey == "" && openAIConfig != nil {
		apiKey = strings.TrimSpace(openAIConfig.APIKey)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("embedding API key Not configured")
	}

	timeout := 120 * time.Second
	if cfg.Indexing.RequestTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Indexing.RequestTimeoutSeconds) * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}

	inner, err := einoembedopenai.NewEmbedder(ctx, &einoembedopenai.EmbeddingConfig{
		APIKey:     apiKey,
		BaseURL:    baseURL,
		ByAzure:    false,
		Model:      model,
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("eino OpenAI embedder: %w", err)
	}

	return &Embedder{
		eino:           inner,
		config:         cfg,
		logger:         logger,
		rateLimiter:    rateLimiter,
		rateLimitDelay: rateLimitDelay,
		maxRetries:     maxRetries,
		retryDelay:     retryDelay,
	}, nil
}

// EmbeddingModelName Return the configured embedding model name (used for tiktoken chunking and vector-row metadata).
func (e *Embedder) EmbeddingModelName() string {
	if e == nil || e.config == nil {
		return ""
	}
	s := strings.TrimSpace(e.config.Embedding.Model)
	if s != "" {
		return s
	}
	return "text-embedding-3-small"
}

func (e *Embedder) waitRateLimiter() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.rateLimiter != nil {
		ctx := context.Background()
		if err := e.rateLimiter.Wait(ctx); err != nil && e.logger != nil {
			e.logger.Warn("Rate limiter wait failed", zap.Error(err))
		}
	}
	if e.rateLimitDelay > 0 {
		time.Sleep(e.rateLimitDelay)
	}
}

// EmbedText Single embedding (float32, consistent with the historical storage format).
func (e *Embedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("unexpected embedding count: %d", len(vecs))
	}
	return vecs[0], nil
}

// EmbedStrings Batch embedding with retries; implements [embedding.Embedder], usable by the Eino Indexer.
func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float32, error) {
	if e == nil || e.eino == nil {
		return nil, fmt.Errorf("embedder not initialized")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	var lastErr error
	for attempt := 0; attempt < e.maxRetries; attempt++ {
		if attempt > 0 {
			wait := e.retryDelay * time.Duration(attempt)
			if e.logger != nil {
				e.logger.Debug("Wait before retrying the embedding", zap.Int("attempt", attempt+1), zap.Duration("wait", wait))
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		} else {
			e.waitRateLimiter()
		}

		raw, err := e.eino.EmbedStrings(ctx, texts, opts...)
		if err == nil {
			out := make([][]float32, len(raw))
			for i, row := range raw {
				out[i] = make([]float32, len(row))
				for j, v := range row {
					out[i][j] = float32(v)
				}
			}
			return out, nil
		}
		lastErr = err
		if !e.isRetryableError(err) {
			return nil, err
		}
		if e.logger != nil {
			e.logger.Debug("Embedding failed, will retry", zap.Int("attempt", attempt+1), zap.Error(err))
		}
	}
	return nil, fmt.Errorf("Reached the maximum number of retries (%d): %v", e.maxRetries, lastErr)
}

// EmbedTexts Batch float32 embedding (compatible with old calls; batch per request to reduce latency).
func (e *Embedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	return e.EmbedStrings(ctx, texts)
}

func (e *Embedder) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
		return true
	}
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") || strings.Contains(errStr, "504") {
		return true
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "network") || strings.Contains(errStr, "EOF") {
		return true
	}
	return false
}

// einoFloatEmbedder adapts [][]float32 embedder to Eino's [][]float64 [embedding.Embedder] for Indexer.Store.
type einoFloatEmbedder struct {
	inner *Embedder
}

func (w *einoFloatEmbedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	vec32, err := w.inner.EmbedStrings(ctx, texts, opts...)
	if err != nil {
		return nil, err
	}
	out := make([][]float64, len(vec32))
	for i, row := range vec32 {
		out[i] = make([]float64, len(row))
		for j, v := range row {
			out[i][j] = float64(v)
		}
	}
	return out, nil
}

func (w *einoFloatEmbedder) GetType() string {
	return "CyberStrikeKnowledgeEmbedder"
}

func (w *einoFloatEmbedder) IsCallbacksEnabled() bool {
	return false
}

// EinoEmbeddingComponent returns an [embedding.Embedder] that uses the same retry/rate-limit path
// and produces float64 vectors expected by generic Eino indexer helpers.
func (e *Embedder) EinoEmbeddingComponent() embedding.Embedder {
	return &einoFloatEmbedder{inner: e}
}
