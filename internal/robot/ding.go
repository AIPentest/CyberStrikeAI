package robot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	dingutils "github.com/open-dingtalk/dingtalk-stream-sdk-go/utils"
	"go.uber.org/zap"
)

const (
	dingReconnectInitial = 5 * time.Second  // 首次重连间隔
	dingReconnectMax     = 60 * time.Second // 最大重连间隔
)

// StartDing 启动钉钉 Stream 长连接（无需公网），收到消息后调用 handler 并通过 SessionWebhook 回复。
// 断线（如笔记本睡眠、网络中断）后会自动重连；ctx 被取消时退出，便于配置变更时重启。
func StartDing(ctx context.Context, robotsCfg config.RobotsConfig, h MessageHandler, logger *zap.Logger) {
	cfg := robotsCfg.Dingtalk
	if !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return
	}
	go runDingLoop(ctx, cfg, robotsCfg.Session.StrictUserIdentityEnabled(), h, logger)
}

// runDingLoop 循环维持钉钉长连接：断开且 ctx 未取消时按退避间隔重连。
func runDingLoop(ctx context.Context, cfg config.RobotDingtalkConfig, strictUserIdentity bool, h MessageHandler, logger *zap.Logger) {
	backoff := dingReconnectInitial
	for {
		streamClient := client.NewStreamClient(
			client.WithAppCredential(client.NewAppCredentialConfig(cfg.ClientID, cfg.ClientSecret)),
			client.WithSubscription(dingutils.SubscriptionTypeKCallback, "/v1.0/im/bot/messages/get",
				chatbot.NewDefaultChatBotFrameHandler(func(ctx context.Context, msg *chatbot.BotCallbackDataModel) ([]byte, error) {
					go handleDingMessage(ctx, msg, cfg, strictUserIdentity, h, logger)
					return nil, nil
				}).OnEventReceived),
		)
		logger.Info("DingTalk Stream connecting…", zap.String("client_id", cfg.ClientID))
		err := streamClient.Start(ctx)
		if ctx.Err() != nil {
			logger.Info("DingTalk Stream closed by configuration restart")
			return
		}
		if err != nil {
			logger.Warn("DingTalk Stream long connection dropped (e.g. sleep/network outage); reconnecting automatically", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			// 下次重连间隔递增，上限 60 秒，避免频繁重试
			if backoff < dingReconnectMax {
				backoff *= 2
				if backoff > dingReconnectMax {
					backoff = dingReconnectMax
				}
			}
		}
	}
}

func handleDingMessage(ctx context.Context, msg *chatbot.BotCallbackDataModel, cfg config.RobotDingtalkConfig, strictUserIdentity bool, h MessageHandler, logger *zap.Logger) {
	if msg == nil || msg.SessionWebhook == "" {
		return
	}
	content := ""
	if msg.Text.Content != "" {
		content = strings.TrimSpace(msg.Text.Content)
	}
	if content == "" && msg.Msgtype == "richText" {
		if cMap, ok := msg.Content.(map[string]interface{}); ok {
			if rich, ok := cMap["richText"].([]interface{}); ok {
				for _, c := range rich {
					if m, ok := c.(map[string]interface{}); ok {
						if txt, ok := m["text"].(string); ok {
							content = strings.TrimSpace(txt)
							break
						}
					}
				}
			}
		}
	}
	if content == "" {
		logger.Debug("DingTalk message content is empty; ignored", zap.String("msgtype", msg.Msgtype))
		return
	}
	logger.Info("DingTalk received message", zap.String("sender", msg.SenderId), zap.String("content", content))
	tenantKey := strings.TrimSpace(cfg.ClientID)
	if tenantKey == "" {
		tenantKey = "default"
	}
	userID := strings.TrimSpace(msg.SenderId)
	if userID != "" {
		userID = "t:" + tenantKey + "|u:" + userID
	} else if cfg.AllowConversationIDFallback && !strictUserIdentity {
		conversationID := strings.TrimSpace(msg.ConversationId)
		if conversationID != "" {
			userID = "t:" + tenantKey + "|c:" + conversationID
		}
	}
	if userID == "" {
		logger.Warn("DingTalk message has no usable user identifier; ignored")
		return
	}
	reply := h.HandleMessage("dingtalk", userID, content)
	// 使用 markdown 类型以便正确展示标题、列表、代码块等格式
	title := reply
	if idx := strings.IndexAny(reply, "\n"); idx > 0 {
		title = strings.TrimSpace(reply[:idx])
	}
	if len(title) > 50 {
		title = title[:50] + "…"
	}
	if title == "" {
		title = "Reply"
	}
	body := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  reply,
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, msg.SessionWebhook, bytes.NewReader(bodyBytes))
	if err != nil {
		logger.Warn("failed to build the DingTalk reply request", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Warn("DingTalk reply request failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Warn("DingTalk reply was not 200", zap.Int("status", resp.StatusCode))
		return
	}
	logger.Debug("DingTalk reply delivered", zap.String("content_preview", reply))
}
