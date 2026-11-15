package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// HTTPReranker calls a hosted rerank API (DashScope or Cohere-compatible).
type HTTPReranker struct {
	provider string
	model    string
	baseURL  string
	apiKey   string
	client   *http.Client
	logger   *zap.Logger
}

// NewHTTPReranker builds a rerank client from knowledge retrieval config; openAI supplies fallback credentials.
func NewHTTPReranker(rc *config.RerankConfig, openAI *config.OpenAIConfig, logger *zap.Logger) (*HTTPReranker, error) {
	if rc == nil {
		return nil, fmt.Errorf("rerank config is nil")
	}
	baseURL := strings.TrimSpace(rc.BaseURL)
	apiKey := strings.TrimSpace(rc.APIKey)
	if openAI != nil {
		if baseURL == "" {
			baseURL = strings.TrimSpace(openAI.BaseURL)
		}
		if apiKey == "" {
			apiKey = strings.TrimSpace(openAI.APIKey)
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("rerank api_key is required")
	}
	provider := rc.ProviderEffective(baseURL)
	model := rc.ModelEffective(provider)
	return &HTTPReranker{
		provider: provider,
		model:    model,
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 60 * time.Second},
		logger:   logger,
	}, nil
}

func (r *HTTPReranker) Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error) {
	if r == nil {
		return docs, nil
	}
	q := strings.TrimSpace(query)
	if q == "" || len(docs) == 0 {
		return docs, nil
	}
	if len(docs) == 1 {
		return docs, nil
	}
	texts := make([]string, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			texts = append(texts, "")
			continue
		}
		texts = append(texts, d.Content)
	}
	var order []int
	var err error
	switch r.provider {
	case "dashscope":
		order, err = r.rerankDashScope(ctx, q, texts, len(docs))
	default:
		order, err = r.rerankCohere(ctx, q, texts, len(docs))
	}
	if err != nil {
		return nil, err
	}
	out := make([]*Document, 0, len(order))
	for _, idx := range order {
		if idx < 0 || idx >= len(docs) || docs[idx] == nil {
			continue
		}
		out = append(out, docs[idx])
	}
	if len(out) == 0 {
		return docs, nil
	}
	return out, nil
}

func (r *HTTPReranker) rerankCohere(ctx context.Context, query string, documents []string, topN int) ([]int, error) {
	url := r.cohereRerankURL()
	body := map[string]any{
		"model":     r.model,
		"query":     query,
		"documents": documents,
		"top_n":     topN,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rerank http %d: %s", resp.StatusCode, truncateForRerankLog(string(respBody)))
	}
	var parsed struct {
		Results []struct {
			Index int `json:"index"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("rerank decode: %w", err)
	}
	order := make([]int, 0, len(parsed.Results))
	for _, row := range parsed.Results {
		order = append(order, row.Index)
	}
	return order, nil
}

func (r *HTTPReranker) rerankDashScope(ctx context.Context, query string, documents []string, topN int) ([]int, error) {
	url := r.dashscopeRerankURL()
	body := map[string]any{
		"model": r.model,
		"input": map[string]any{
			"query":     query,
			"documents": documents,
		},
		"parameters": map[string]any{
			"return_documents": false,
			"top_n":            topN,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dashscope rerank http %d: %s", resp.StatusCode, truncateForRerankLog(string(respBody)))
	}
	var parsed struct {
		Output struct {
			Results []struct {
				Index int `json:"index"`
			} `json:"results"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("dashscope rerank decode: %w", err)
	}
	order := make([]int, 0, len(parsed.Output.Results))
	for _, row := range parsed.Output.Results {
		order = append(order, row.Index)
	}
	return order, nil
}

func (r *HTTPReranker) cohereRerankURL() string {
	base := r.baseURL
	if base == "" {
		base = "https://api.cohere.com"
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/rerank"
	}
	return base + "/v1/rerank"
}

func (r *HTTPReranker) dashscopeRerankURL() string {
	base := strings.TrimSpace(r.baseURL)
	if base == "" {
		return "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
	}
	if strings.Contains(base, "/api/v1/services/rerank") {
		return base
	}
	if strings.Contains(base, "dashscope.aliyuncs.com") || strings.Contains(base, "compatible-mode") {
		return "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
	}
	return strings.TrimSuffix(base, "/")
}

func truncateForRerankLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512] + "..."
	}
	return s
}

var _ DocumentReranker = (*HTTPReranker)(nil)
