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

// Client 统一封装与OpenAI兼容模型交互的HTTP客户端。
type Client struct {
	httpClient *http.Client
	config     *config.OpenAIConfig
	logger     *zap.Logger
}

// APIError 表示OpenAI接口返回的非200错误。
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openai api error: status=%d body=%s", e.StatusCode, e.Body)
}

// normalizeStreamingDelta 将可能是“累计片段/重发片段”的内容归一化为“纯增量”。
// 部分兼容网关会返回累计 content；若直接 append 会出现重复文本。
//
// 注意：
//   - 不做「任意后缀与前缀重叠」合并；流式可能在重复字符边界分片（"194"+"43"→"19443"）。
//   - HasPrefix 仅在 incoming 严格长于 current 时视为累计全文，否则会把分片产生的第二个相同
//     单字/单码点（叠字、44、22 等）误判为「整段重复」而吞字。
//   - incoming==current 仅当 current 长度 >1 个码点时才视为整包重发；单码点重复必须走拼接。
//   - 不再使用「current 以 incoming 结尾则丢弃」：否则 "1943"+"43" 会误吞增量（19443 显示成 1943）。
//     若网关重复发送尾部片段，应重复送完整累计串，由 HasPrefix 分支去重。
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

// NewClient 创建一个新的OpenAI客户端。
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

// UpdateConfig 动态更新OpenAI配置。
func (c *Client) UpdateConfig(cfg *config.OpenAIConfig) {
	c.config = cfg
}

// ChatCompletion 调用 /chat/completions 接口。
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
	resp, err := c.httpClient.Do(req)
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

// ChatCompletionStream 调用 /chat/completions 的流式模式（stream=true），并在每个 delta 到达时回调 onDelta。
// 返回最终拼接的 content（只拼 content delta；工具调用 delta 未做处理）。
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
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call openai api: %w", err)
	}
	defer resp.Body.Close()

	// 非200：读完 body 返回
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
		// OpenAI 兼容流式通常使用 content；但部分兼容实现可能用 text。
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

	// 典型 SSE 结构：
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
			// 解析失败跳过（兼容各种兼容层的差异）
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

// StreamToolCall 流式工具调用的累积结果（arguments 以字符串形式拼接，留给上层再解析为 JSON）。
type StreamToolCall struct {
	Index           int
	ID              string
	Type            string
	FunctionName    string
	FunctionArgsStr string
}

// ChatCompletionStreamWithToolCalls 流式模式：同时把 content delta 实时回调，并在结束后返回 tool_calls 和 finish_reason。
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
	resp, err := c.httpClient.Do(req)
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

	// delta tool_calls 的增量结构
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
			// 兼容：解析失败跳过
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

	// 组装 tool calls
	indices := make([]int, 0, len(toolCallAccums))
	for idx := range toolCallAccums {
		indices = append(indices, idx)
	}
	// 手写简单排序（避免额外 import）
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

// ModelsListResponse 表示 OpenAI 兼容 GET /models 响应。
type ModelsListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object,omitempty"`
		OwnedBy string `json:"owned_by,omitempty"`
	} `json:"data"`
}

// ListModels 调用 GET {baseURL}/models 获取可用模型 id 列表（按字典序）。
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

	resp, err := c.httpClient.Do(req)
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
