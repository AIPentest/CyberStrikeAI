package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
)

// Client calls TypeSafe System One (Jev).
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

// APIError is a non-2xx TypeSafe HTTP response.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("typesafe api error: status=%d body=%s", e.StatusCode, e.Body)
}

// NewClient builds a System One client. Empty baseURL/model use TypeSafe defaults.
func NewClient(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		model:      model,
	}
}

// Question is a typed System One question (noul / choice / score).
type Question map[string]any

// Noul builds a yes/no question.
func Noul(instructions string, trueMean, falseMean string) Question {
	q := Question{
		"type":         "noul",
		"instructions": instructions,
	}
	if strings.TrimSpace(trueMean) != "" || strings.TrimSpace(falseMean) != "" {
		q["criteria"] = map[string]string{
			"true":  trueMean,
			"false": falseMean,
		}
	}
	return q
}

// Choice builds a closed-set question.
func Choice(instructions string, criteria map[string]string) Question {
	return Question{
		"type":         "choice",
		"instructions": instructions,
		"criteria":     criteria,
	}
}

// Result is a System One evaluation response.
type Result struct {
	Model   string                    `json:"model"`
	Answers map[string]map[string]any `json:"answers"`
	Usage   Usage                     `json:"usage"`
}

// Usage reports token counts.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Noul returns the probability that question id is yes.
func (r *Result) Noul(id string) float64 {
	if r == nil {
		return 0
	}
	ans, ok := r.Answers[id]
	if !ok || ans == nil {
		return 0
	}
	switch v := ans["noul"].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

// Choice returns the selected option and confidence.
func (r *Result) Choice(id string) (choice string, confidence float64) {
	if r == nil {
		return "", 0
	}
	ans, ok := r.Answers[id]
	if !ok || ans == nil {
		return "", 0
	}
	choice, _ = ans["choice"].(string)
	switch v := ans["confidence"].(type) {
	case float64:
		confidence = v
	case json.Number:
		confidence, _ = v.Float64()
	}
	return strings.TrimSpace(choice), confidence
}

type systemOneRequest struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// SystemOne evaluates state against questions.
func (c *Client) SystemOne(ctx context.Context, state any, questions map[string]Question) (*Result, error) {
	if c == nil {
		return nil, fmt.Errorf("typesafe client is not initialized")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("typesafe api key is empty")
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("typesafe questions are empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	body, err := json.Marshal(systemOneRequest{
		State:     state,
		Model:     c.model,
		Questions: questions,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal typesafe payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build typesafe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call typesafe api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read typesafe response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var out Result
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode typesafe response: %w", err)
	}
	if out.Answers == nil {
		out.Answers = map[string]map[string]any{}
	}
	return &out, nil
}
