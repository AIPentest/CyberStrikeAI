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
	"cyberstrike-ai/internal/llm"
	"cyberstrike-ai/internal/openai"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

// Client Calls a standalone Vision ChatModel (single-shot Generate).
type Client struct {
	cfg    config.VisionConfig
	mainOA config.OpenAIConfig
}

// NewClient Construct the vision client.
func NewClient(visionCfg config.VisionConfig, mainOpenAI config.OpenAIConfig) *Client {
	return &Client{cfg: visionCfg, mainOA: mainOpenAI}
}

// Analyze Sends the image bytes to the VL model and returns a text description.
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
	detail := schema.ImageURLDetailLow
	switch c.cfg.DetailEffective() {
	case "high":
		detail = schema.ImageURLDetailHigh
	case "auto":
		detail = schema.ImageURLDetailAuto
	}

	prompt := buildVisionPrompt(question)
	if llm.IsClaudeProvider(oa.Provider) {
		nativeModel, err := llm.NewClaudeAgenticModel(
			ctx,
			oa,
			httpClient,
			oa.MaxCompletionTokensEffective(),
			nil,
		)
		if err != nil {
			return "", fmt.Errorf("vision native Claude model: %w", err)
		}
		resp, err := nativeModel.Generate(ctx, []*schema.AgenticMessage{{
			Role: schema.AgenticRoleTypeUser,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.UserInputText{Text: prompt}),
				schema.NewContentBlock(&schema.UserInputImage{
					Base64Data: b64,
					MIMEType:   mime,
					Detail:     detail,
				}),
			},
		}})
		if err != nil {
			return "", fmt.Errorf("vision native Claude generate: %w", err)
		}
		content, _ := llm.AgenticText(resp)
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("vision model returned empty content")
		}
		return strings.TrimSpace(content), nil
	}

	httpClient = openai.NewEinoHTTPClient(&oa, httpClient)
	maxCompletionTokens := oa.MaxCompletionTokensEffective()
	modelCfg := &einoopenai.ChatModelConfig{
		APIKey:              oa.APIKey,
		BaseURL:             strings.TrimSuffix(oa.BaseURL, "/"),
		Model:               oa.Model,
		HTTPClient:          httpClient,
		MaxCompletionTokens: &maxCompletionTokens,
	}
	chatModel, err := einoopenai.NewChatModel(ctx, modelCfg)
	if err != nil {
		return "", fmt.Errorf("vision chat model: %w", err)
	}
	userMsg := &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: prompt},
			{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						Base64Data: &b64,
						MIMEType:   mime,
					},
					Detail: detail,
				},
			},
		},
	}

	resp, err := chatModel.Generate(ctx, []*schema.Message{userMsg})
	if err != nil {
		return "", fmt.Errorf("vision generate: %w", err)
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return "", fmt.Errorf("vision model returned empty content")
	}
	return strings.TrimSpace(resp.Content), nil
}

func buildVisionPrompt(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		q = "Give a general description of the image, focusing on authorized security-testing scenarios (visible text, forms, buttons, CAPTCHAs, error messages, tech-stack clues)."
	}
	extra := ""
	if looksLikeCaptchaQuestion(q) {
		extra = "\n If it is a CAPTCHA: output only the character sequence you recognize, with no spaces, punctuation, or explanation; if you cannot read it clearly, state plainly that you cannot recognize it."
	}
	return `You are an authorized security-testing assistant. Answer the user's question based on the image, describing only what you can confirm from it, and do not fabricate.
User question: ` + q + extra
}

func looksLikeCaptchaQuestion(q string) bool {
	s := strings.ToLower(q)
	for _, kw := range []string{"CAPTCHA", "captcha", "verification code", "verify code", "vcode", "graphical code"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return strings.Contains(s, "Output only") && (strings.Contains(s, "characters") || strings.Contains(s, "character"))
}
