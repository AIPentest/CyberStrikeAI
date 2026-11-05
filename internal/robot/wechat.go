package robot

import (
	"context"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/robot/ilink"

	"go.uber.org/zap"
)

const (
	wechatReconnectInitial = 5 * time.Second
	wechatReconnectMax     = 60 * time.Second
	wechatPlatform         = "wechat"
)

// StartWechat 启动微信 iLink 长轮询（无需公网回调），收到消息后调用 handler 并回复。
func StartWechat(ctx context.Context, robotsCfg config.RobotsConfig, h MessageHandler, appVersion string, logger *zap.Logger) {
	cfg := robotsCfg.Wechat
	if !cfg.Enabled || cfg.BotToken == "" {
		return
	}
	go runWechatLoop(ctx, cfg, h, appVersion, logger)
}

func runWechatLoop(ctx context.Context, cfg config.RobotWechatConfig, h MessageHandler, appVersion string, logger *zap.Logger) {
	backoff := wechatReconnectInitial
	for {
		err := runWechatPoll(ctx, cfg, h, appVersion, logger)
		if ctx.Err() != nil {
			logger.Info("WeChat iLink long-polling closed by configuration")
			return
		}
		if err != nil {
			logger.Warn("WeChat iLink long-polling error; reconnecting automatically", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			if backoff < wechatReconnectMax {
				backoff *= 2
				if backoff > wechatReconnectMax {
					backoff = wechatReconnectMax
				}
			}
		}
	}
}

func runWechatPoll(ctx context.Context, cfg config.RobotWechatConfig, h MessageHandler, appVersion string, logger *zap.Logger) error {
	client := ilink.NewClient(cfg.BaseURL, cfg.BotToken, cfg.BotAgent, ilink.BuildClientVersion(appVersion))
	buf := cfg.GetUpdatesBuf
	logger.Info("WeChat iLink long-polling started", zap.String("ilink_bot_id", cfg.ILinkBotID))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := client.GetUpdates(ctx, buf)
		if err != nil {
			return err
		}
		if resp.ErrCode != 0 && resp.Ret != 0 {
			logger.Warn("WeChat getUpdates returned an error", zap.Int("errcode", resp.ErrCode), zap.String("errmsg", resp.ErrMsg))
		}
		if resp.GetUpdatesBuf != "" {
			buf = resp.GetUpdatesBuf
		}
		for _, msg := range resp.Msgs {
			if msg.MessageType != 1 {
				continue
			}
			text := ilink.ExtractText(msg)
			if text == "" {
				continue
			}
			userID := strings.TrimSpace(msg.FromUserID)
			if userID == "" {
				continue
			}
			logger.Info("WeChat received message", zap.String("from", userID), zap.String("content", text))
			reply := h.HandleMessage(wechatPlatform, userID, text)
			if strings.TrimSpace(reply) == "" {
				continue
			}
			if err := client.SendTextMessage(ctx, userID, msg.ContextToken, reply, ""); err != nil {
				logger.Warn("failed to send the WeChat reply", zap.String("to", userID), zap.Error(err))
			}
		}
	}
}
