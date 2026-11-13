package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type claudeNativePayload struct {
	Model               string                `json:"model"`
	Messages            []claudeNativeMessage `json:"messages"`
	Temperature         *float32              `json:"temperature,omitempty"`
	TopP                *float32              `json:"top_p,omitempty"`
	MaxCompletionTokens int                   `json:"max_completion_tokens,omitempty"`
	MaxTokens           int                   `json:"max_tokens,omitempty"`
	Thinking            any                   `json:"thinking,omitempty"`
	OutputConfig        any                   `json:"output_config,omitempty"`
}
type claudeNativeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (c *Client) isClaude() bool {
	return c != nil && c.config != nil && (strings.EqualFold(c.config.Provider, "claude") || strings.EqualFold(c.config.Provider, "anthropic"))
}
func (c *Client) parseClaudeNativePayload(payload any) (*claudeNativePayload, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var req claudeNativePayload
	if err = json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.Model == "" || len(req.Messages) == 0 {
		return nil, fmt.Errorf("Claude model/messages are required")
	}
	return &req, nil
}
func (p *claudeNativePayload) maxTokens(fallback int) int {
	if p.MaxCompletionTokens > 0 {
		return p.MaxCompletionTokens
	}
	if p.MaxTokens > 0 {
		return p.MaxTokens
	}
	return fallback
}
func claudeNativeTextContent(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var out []string
		for _, p := range parts {
			if p.Type == "text" {
				out = append(out, p.Text)
			}
		}
		return strings.Join(out, "\n")
	}
	return string(raw)
}
func claudeNativeBlocks(raw json.RawMessage) any {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return claudeNativeTextContent(raw)
	}
	var out []map[string]any
	for _, b := range blocks {
		if b["type"] == "text" {
			out = append(out, b)
			continue
		}
		if b["type"] == "image_url" {
			image, _ := b["image_url"].(map[string]any)
			url, _ := image["url"].(string)
			if strings.HasPrefix(url, "data:") {
				parts := strings.SplitN(strings.TrimPrefix(url, "data:"), ";base64,", 2)
				if len(parts) == 2 {
					out = append(out, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": parts[0], "data": parts[1]}})
				}
			} else if url != "" {
				out = append(out, map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": url}})
			}
		}
	}
	return out
}
func (c *Client) claudeRequest(ctx context.Context, payload any, stream bool) (*http.Response, error) {
	p, err := c.parseClaudeNativePayload(payload)
	if err != nil {
		return nil, err
	}
	var messages []map[string]any
	var system []string
	for _, m := range p.Messages {
		if m.Role == "system" || m.Role == "developer" {
			system = append(system, claudeNativeTextContent(m.Content))
			continue
		}
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		messages = append(messages, map[string]any{"role": role, "content": claudeNativeBlocks(m.Content)})
	}
	body := map[string]any{"model": p.Model, "messages": messages, "max_tokens": p.maxTokens(c.config.MaxCompletionTokensEffective()), "stream": stream}
	if len(system) > 0 {
		body["system"] = strings.Join(system, "\n\n")
	}
	if p.Temperature != nil {
		body["temperature"] = *p.Temperature
	}
	if p.TopP != nil {
		body["top_p"] = *p.TopP
	}
	if p.Thinking != nil {
		body["thinking"] = p.Thinking
	}
	if p.OutputConfig != nil {
		body["output_config"] = p.OutputConfig
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(c.config.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	applyCustomHeaders(req, c.config)
	res, err := customHeaderHTTPClient(c.httpClient, c.config).Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 16384))
		return nil, &APIError{StatusCode: res.StatusCode, Body: string(b)}
	}
	return res, nil
}
func (c *Client) claudeNativeChatCompletion(ctx context.Context, payload, out any) error {
	res, err := c.claudeRequest(ctx, payload, false)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var response struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Usage      map[string]any `json:"usage"`
		StopReason string         `json:"stop_reason"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 32*1024*1024)).Decode(&response); err != nil {
		return err
	}
	var text, thinking strings.Builder
	for _, b := range response.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
		if b.Type == "thinking" {
			thinking.WriteString(b.Thinking)
		}
	}
	finish := "stop"
	if response.StopReason == "max_tokens" {
		finish = "length"
	}
	wire := map[string]any{"id": response.ID, "model": response.Model, "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text.String(), "reasoning_content": thinking.String()}, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": response.Usage["input_tokens"], "completion_tokens": response.Usage["output_tokens"]}}
	if out == nil {
		return nil
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func (c *Client) claudeNativeChatCompletionStream(ctx context.Context, payload any, onDelta func(string) error) (string, error) {
	res, err := c.claudeRequest(ctx, payload, true)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var full strings.Builder
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	stopped := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Error json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return full.String(), err
		}
		if event.Type == "error" {
			return full.String(), fmt.Errorf("Claude stream error: %s", event.Error)
		}
		if event.Type == "message_stop" {
			stopped = true
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			full.WriteString(event.Delta.Text)
			if onDelta != nil {
				if err = onDelta(event.Delta.Text); err != nil {
					return full.String(), err
				}
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return full.String(), err
	}
	if !stopped {
		return full.String(), io.ErrUnexpectedEOF
	}
	return full.String(), nil
}
