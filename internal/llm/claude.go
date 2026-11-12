package llm

import (
	"context"
	"net/http"
	"strings"

	"cyberstrike-ai/internal/config"

	agenticclaude "github.com/cloudwego/eino-ext/components/model/agenticclaude"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func IsClaudeProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "claude" || provider == "anthropic"
}

func NewClaudeAgenticModel(
	ctx context.Context,
	cfg config.OpenAIConfig,
	httpClient *http.Client,
	maxTokens int,
	extraFields map[string]any,
) (model.AgenticModel, error) {
	if maxTokens <= 0 {
		maxTokens = cfg.MaxCompletionTokensEffective()
	}
	if cfg.IsDeepSeekEndpointOrModel() {
		httpClient = newDeepSeekAnthropicCompatibleClient(httpClient)
	}
	// Claude 渠道同样尊重渠道自定义请求头（%session_id% 占位符 + 跨源重定向防护），
	// 与 OpenAI 协议路径保持一致；主聊天、视觉、原生分发、探针等所有消费端
	// 都经由这里构造，一处收口。
	httpClient = customHeaderHTTPTransport(httpClient, &cfg)
	return agenticclaude.New(ctx, &agenticclaude.Config{
		APIKey:      strings.TrimSpace(cfg.APIKey),
		BaseURL:     strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/"),
		Model:       strings.TrimSpace(cfg.Model),
		MaxTokens:   maxTokens,
		HTTPClient:  httpClient,
		ExtraFields: extraFields,
	})
}

func AgenticText(msg *schema.AgenticMessage) (content, reasoning string) {
	if msg == nil {
		return "", ""
	}
	var contentParts, reasoningParts []string
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		switch {
		case block.AssistantGenText != nil:
			contentParts = append(contentParts, block.AssistantGenText.Text)
		case block.Reasoning != nil:
			reasoningParts = append(reasoningParts, block.Reasoning.Text)
		}
	}
	return strings.Join(contentParts, ""), strings.Join(reasoningParts, "")
}
