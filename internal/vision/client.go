package vision

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"
)

// Client 调用独立 Vision ChatModel（单次 Generate）。
type Client struct {
	cfg    config.VisionConfig
	mainOA config.OpenAIConfig
}

// NewClient 构造视觉客户端。
func NewClient(visionCfg config.VisionConfig, mainOpenAI config.OpenAIConfig) *Client {
	return &Client{cfg: visionCfg, mainOA: mainOpenAI}
}

// Analyze 将图片字节送入 VL 模型并返回文本描述。
func (c *Client) Analyze(ctx context.Context, img ImagePayload, question string) (string, error) {
	if len(img.Bytes) == 0 {
		return "", fmt.Errorf("empty image payload")
	}
	mime := strings.TrimSpace(img.MIMEType)
	if mime == "" {
		mime = "image/jpeg"
	}
	oa := c.cfg.OpenAICfgEffective(c.mainOA)
	if strings.TrimSpace(oa.APIKey) == "" {
		return "", fmt.Errorf("vision API key is empty (set vision.api_key or openai.api_key)")
	}
	if strings.TrimSpace(oa.Model) == "" {
		return "", fmt.Errorf("vision model is empty")
	}

	timeout := time.Duration(c.cfg.TimeoutSecondsEffective()) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpClient := &http.Client{
		Timeout: timeout + 15*time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   60 * time.Second,
				KeepAlive: 60 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: timeout + 10*time.Second,
		},
	}

	b64 := base64.StdEncoding.EncodeToString(img.Bytes)
	payload := map[string]any{"model": oa.Model, "max_completion_tokens": oa.MaxCompletionTokensEffective(), "messages": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": buildVisionPrompt(question)}, {"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + b64, "detail": c.cfg.DetailEffective()}}}}}}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	client := openai.NewClient(&oa, httpClient, nil)
	if err := client.ChatCompletion(ctx, payload, &reply); err != nil {
		return "", err
	}
	if len(reply.Choices) == 0 || strings.TrimSpace(reply.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("vision model returned empty content")
	}
	return strings.TrimSpace(reply.Choices[0].Message.Content), nil
}

func buildVisionPrompt(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		q = "请对图片做通用描述，侧重授权安全测试场景（可见文本、表单、按钮、验证码、错误信息、技术栈线索）。"
	}
	extra := ""
	if looksLikeCaptchaQuestion(q) {
		extra = "\n若为验证码：仅输出你辨认出的字符序列，不要空格、标点、解释；看不清则明确说无法识别。"
	}
	return `你是授权安全测试助手。请根据图片回答用户问题，只描述你能从图中确认的内容，不要编造。
用户问题：` + q + extra
}

func looksLikeCaptchaQuestion(q string) bool {
	s := strings.ToLower(q)
	for _, kw := range []string{"验证码", "captcha", "verification code", "verify code", "vcode", "图形码"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return strings.Contains(s, "只输出") && (strings.Contains(s, "字符") || strings.Contains(s, "character"))
}
