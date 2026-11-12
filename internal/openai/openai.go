package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// Client A unified wrapper around the HTTP client that interacts with OpenAI-compatible models.
type Client struct {
	httpClient *http.Client
	config     *config.OpenAIConfig
	logger     *zap.Logger
}

// APIError Represents a non-200 error returned by the OpenAI API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openai api error: status=%d body=%s", e.StatusCode, e.Body)
}

// normalizeStreamingDelta will likely be"cumulative fragment / resent fragment"normalizes the content into"pure increment".
// Some compatible gateways return cumulative content; appending it directly would produce duplicated text.
//
// Note:
// - Does not merge by 'arbitrary suffix-prefix overlap'; streaming may fragment at the boundary of repeated characters ("194"+"43"→"19443").
// - HasPrefix Only treats it as a cumulative full text when incoming is strictly longer than current; otherwise the second identical chunk produced by fragmentation would
// A single character/code point (duplicated characters, 44, 22, etc.) being misjudged as a 'whole-segment repeat' and swallowed.
// - incoming==current Only treats it as a full-packet resend when current is more than 1 code point long; a single-code-point repeat must go through concatenation.
// - No longer uses 'discard if current ends with incoming': otherwise "1943"+"43" would mistakenly swallow the increment (19443 shown as 1943).
// If the gateway re-sends a trailing fragment, it should re-send the full cumulative string, and the HasPrefix branch deduplicates it.
func normalizeStreamingDelta(current, incoming string) (next, delta string) {
	if incoming == "" {
		return current, ""
	}
	if current == "" {
		return incoming, incoming
	}
	if strings.HasPrefix(incoming, current) && len(incoming) > len(current) {
		return incoming, incoming[len(current):]
	}
	if incoming == current && utf8.RuneCountInString(current) > 1 {
		return current, ""
	}
	return current + incoming, incoming
}

// NewClient Creates a new OpenAI client.
func NewClient(cfg *config.OpenAIConfig, httpClient *http.Client, logger *zap.Logger) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		httpClient: httpClient,
		config:     cfg,
		logger:     logger,
	}
}

// UpdateConfig Dynamically update the OpenAI configuration.
func (c *Client) UpdateConfig(cfg *config.OpenAIConfig) {
	c.config = cfg
}

// ChatCompletion Calls the /chat/completions endpoint.
func (c *Client) ChatCompletion(ctx context.Context, payload interface{}, out interface{}) error {
	if c == nil {
		return fmt.Errorf("openai client is not initialized")
	}
	if c.config == nil {
		return fmt.Errorf("openai config is nil")
	}
	if strings.TrimSpace(c.config.APIKey) == "" {
		return fmt.Errorf("openai api key is empty")
	}
	if c.isClaude() {
		return c.claudeNativeChatCompletion(ctx, payload, out)
	}

	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal openai payload: %w", err)
	}

	c.logger.Debug("sending OpenAI chat completion request",
		zap.Int("payloadSizeKB", len(body)/1024))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	applyCustomHeaders(req, c.config)

	requestStart := time.Now()
	resp, err := customHeaderHTTPClient(c.httpClient, c.config).Do(req)
	if err != nil {
		return fmt.Errorf("call openai api: %w", err)
	}
	defer resp.Body.Close()

	bodyChan := make(chan []byte, 1)
	errChan := make(chan error, 1)
	go func() {
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			errChan <- err
			return
		}
		bodyChan <- responseBody
	}()

	var respBody []byte
	select {
	case respBody = <-bodyChan:
	case err := <-errChan:
		return fmt.Errorf("read openai response: %w", err)
	case <-ctx.Done():
		return fmt.Errorf("read openai response timeout: %w", ctx.Err())
	case <-time.After(25 * time.Minute):
		return fmt.Errorf("read openai response timeout (25m)")
	}

	c.logger.Debug("received OpenAI response",
		zap.Int("status", resp.StatusCode),
		zap.Duration("duration", time.Since(requestStart)),
		zap.Int("responseSizeKB", len(respBody)/1024),
	)

	if resp.StatusCode != http.StatusOK {
		c.logger.Warn("OpenAI chat completion returned non-200",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBody)),
		)
		return &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			c.logger.Error("failed to unmarshal OpenAI response",
				zap.Error(err),
				zap.String("body", string(respBody)),
			)
			return fmt.Errorf("unmarshal openai response: %w", err)
		}
	}

	return nil
}

// ChatCompletionStream Calls the streaming mode of /chat/completions (stream=true) and invokes the callback as each delta arrives onDelta.
// Returns the final assembled content (only content deltas are concatenated; tool-call deltas are not processed).
func (c *Client) ChatCompletionStream(ctx context.Context, payload interface{}, onDelta func(delta string) error) (string, error) {
	if c == nil {
		return "", fmt.Errorf("openai client is not initialized")
	}
	if c.config == nil {
		return "", fmt.Errorf("openai config is nil")
	}
	if strings.TrimSpace(c.config.APIKey) == "" {
		return "", fmt.Errorf("openai api key is empty")
	}
	if c.isClaude() {
		return c.claudeNativeChatCompletionStream(ctx, payload, onDelta)
	}

	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal openai payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	applyCustomHeaders(req, c.config)

	requestStart := time.Now()
	resp, err := customHeaderHTTPClient(c.httpClient, c.config).Do(req)
	if err != nil {
		return "", fmt.Errorf("call openai api: %w", err)
	}
	defer resp.Body.Close()

	// Non-200: reads the full body and returns
	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			c.logger.Warn("failed to read OpenAI error response body", zap.Error(readErr))
		}
		return "", &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	type streamDelta struct {
		// OpenAI Compatible streaming usually uses content; but some compatible implementations may use text.
		Content string `json:"content,omitempty"`
		Text    string `json:"text,omitempty"`
	}
	type streamChoice struct {
		Delta        streamDelta `json:"delta"`
		FinishReason *string     `json:"finish_reason,omitempty"`
	}
	type streamResponse struct {
		ID      string         `json:"id,omitempty"`
		Choices []streamChoice `json:"choices"`
		Error   *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error,omitempty"`
	}

	reader := bufio.NewReader(resp.Body)
	var full strings.Builder
	fullText := ""

	// A typical SSE structure:
	// data: {...}\n\n
	// data: [DONE]\n\n
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return full.String(), fmt.Errorf("read openai stream: %w", readErr)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if dataStr == "[DONE]" {
			break
		}

		var chunk streamResponse
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			// Skips on parse failure (to tolerate the differences of various compatibility layers)
			continue
		}
		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return full.String(), fmt.Errorf("openai stream error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			delta = chunk.Choices[0].Delta.Text
		}
		if delta == "" {
			continue
		}

		var deltaOut string
		fullText, deltaOut = normalizeStreamingDelta(fullText, delta)
		if deltaOut == "" {
			continue
		}
		full.WriteString(deltaOut)
		if onDelta != nil {
			if err := onDelta(deltaOut); err != nil {
				return full.String(), err
			}
		}
	}

	c.logger.Debug("received OpenAI stream completion",
		zap.Duration("duration", time.Since(requestStart)),
		zap.Int("contentLen", full.Len()),
	)

	return full.String(), nil
}

// StreamToolCall The accumulated result of streaming tool calls (arguments are concatenated as a string, left for the upper layer to parse into JSON).
type StreamToolCall struct {
	Index           int
	ID              string
	Type            string
	FunctionName    string
	FunctionArgsStr string
}

// ChatCompletionStreamWithToolCalls Streaming mode: invokes the callback with content deltas in real time, and after finishing returns tool_calls and finish_reason.
func (c *Client) ChatCompletionStreamWithToolCalls(
	ctx context.Context,
	payload interface{},
	onContentDelta func(delta string) error,
) (string, []StreamToolCall, string, error) {
	if c == nil {
		return "", nil, "", fmt.Errorf("openai client is not initialized")
	}
	if c.config == nil {
		return "", nil, "", fmt.Errorf("openai config is nil")
	}
	if strings.TrimSpace(c.config.APIKey) == "" {
		return "", nil, "", fmt.Errorf("openai api key is empty")
	}
	if c.isClaude() {
		return "", nil, "", fmt.Errorf("native Claude tool-call streaming requires Eino AgenticModel")
	}

	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, "", fmt.Errorf("marshal openai payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, "", fmt.Errorf("build openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	applyCustomHeaders(req, c.config)

	requestStart := time.Now()
	resp, err := customHeaderHTTPClient(c.httpClient, c.config).Do(req)
	if err != nil {
		return "", nil, "", fmt.Errorf("call openai api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			c.logger.Warn("failed to read OpenAI error response body", zap.Error(readErr))
		}
		return "", nil, "", &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	// delta tool_calls the incremental structure of
	type toolCallFunctionDelta struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	}
	type toolCallDelta struct {
		Index    int                   `json:"index,omitempty"`
		ID       string                `json:"id,omitempty"`
		Type     string                `json:"type,omitempty"`
		Function toolCallFunctionDelta `json:"function,omitempty"`
	}
	type streamDelta2 struct {
		Content   string          `json:"content,omitempty"`
		Text      string          `json:"text,omitempty"`
		ToolCalls []toolCallDelta `json:"tool_calls,omitempty"`
	}
	type streamChoice2 struct {
		Delta        streamDelta2 `json:"delta"`
		FinishReason *string      `json:"finish_reason,omitempty"`
	}
	type streamResponse2 struct {
		Choices []streamChoice2 `json:"choices"`
		Error   *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error,omitempty"`
	}

	type toolCallAccum struct {
		id   string
		typ  string
		name string
		args strings.Builder
	}
	toolCallAccums := make(map[int]*toolCallAccum)

	reader := bufio.NewReader(resp.Body)
	var full strings.Builder
	fullText := ""
	finishReason := ""

	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return full.String(), nil, finishReason, fmt.Errorf("read openai stream: %w", readErr)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if dataStr == "[DONE]" {
			break
		}

		var chunk streamResponse2
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			// Compatibility: skip on parse failure
			continue
		}
		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return full.String(), nil, finishReason, fmt.Errorf("openai stream error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			finishReason = strings.TrimSpace(*choice.FinishReason)
		}

		delta := choice.Delta

		content := delta.Content
		if content == "" {
			content = delta.Text
		}
		if content != "" {
			var contentOut string
			fullText, contentOut = normalizeStreamingDelta(fullText, content)
			if contentOut != "" {
				full.WriteString(contentOut)
				if onContentDelta != nil {
					if err := onContentDelta(contentOut); err != nil {
						return full.String(), nil, finishReason, err
					}
				}
			}
		}

		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				acc, ok := toolCallAccums[tc.Index]
				if !ok {
					acc = &toolCallAccum{}
					toolCallAccums[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Type != "" {
					acc.typ = tc.Type
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.args.WriteString(tc.Function.Arguments)
				}
			}
		}
	}

	// Assemble tool calls
	indices := make([]int, 0, len(toolCallAccums))
	for idx := range toolCallAccums {
		indices = append(indices, idx)
	}
	// A hand-written simple sort (to avoid an extra import)
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			if indices[j] < indices[i] {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	toolCalls := make([]StreamToolCall, 0, len(indices))
	for _, idx := range indices {
		acc := toolCallAccums[idx]
		tc := StreamToolCall{
			Index:           idx,
			ID:              acc.id,
			Type:            acc.typ,
			FunctionName:    acc.name,
			FunctionArgsStr: acc.args.String(),
		}
		toolCalls = append(toolCalls, tc)
	}

	c.logger.Debug("received OpenAI stream completion (tool_calls)",
		zap.Duration("duration", time.Since(requestStart)),
		zap.Int("contentLen", full.Len()),
		zap.Int("toolCalls", len(toolCalls)),
		zap.String("finishReason", finishReason),
	)

	if strings.TrimSpace(finishReason) == "" {
		finishReason = "stop"
	}

	return full.String(), toolCalls, finishReason, nil
}

// ModelsListResponse Represents an OpenAI-compatible GET /models response.
type ModelsListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object,omitempty"`
		OwnedBy string `json:"owned_by,omitempty"`
	} `json:"data"`
}

// ListModels Calls GET {baseURL}/models to get the list of available model ids (in lexicographic order).
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("openai client is not initialized")
	}
	if c.config == nil {
		return nil, fmt.Errorf("openai config is nil")
	}
	if strings.TrimSpace(c.config.APIKey) == "" {
		return nil, fmt.Errorf("openai api key is empty")
	}
	if c.isClaude() {
		return nil, fmt.Errorf("claude provider does not support models list API")
	}

	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build openai models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	applyCustomHeaders(req, c.config)

	resp, err := customHeaderHTTPClient(c.httpClient, c.config).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call openai models api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai models response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var list ModelsListResponse
	if err := json.Unmarshal(respBody, &list); err != nil {
		return nil, fmt.Errorf("decode openai models response: %w", err)
	}

	seen := make(map[string]struct{}, len(list.Data))
	models := make([]string, 0, len(list.Data))
	for _, item := range list.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("models list is empty")
	}
	return models, nil
}
