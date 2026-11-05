package robot

import (
	"context"
	"strings"
	"sync"

	"cyberstrike-ai/internal/config"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
	"go.uber.org/zap"
)

const (
	qqPlatform        = "qq"
	qqMaxMessageRunes = 3500
)

var (
	qqHandlerMu sync.Mutex
	qqHandler   MessageHandler
	qqLogger    *zap.Logger
	qqAPI       openapi.OpenAPI
)

// StartQQ 启动 QQ 机器人 WebSocket（C2C 与群 @，出站连接，无需公网回调）。
func StartQQ(ctx context.Context, robotsCfg config.RobotsConfig, h MessageHandler, logger *zap.Logger) {
	cfg := robotsCfg.QQ
	if !cfg.Enabled || strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return
	}
	go runQQLoop(ctx, cfg, h, logger)
}

func runQQLoop(ctx context.Context, cfg config.RobotQQConfig, h MessageHandler, logger *zap.Logger) {
	backoff := reconnectInitial
	for {
		if ctx.Err() != nil {
			logger.Info("QQ bot WebSocket closed by configuration")
			return
		}
		err := runQQSession(ctx, cfg, h, logger)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("QQ bot WebSocket error; reconnecting automatically", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		if !waitReconnect(ctx, &backoff) {
			return
		}
	}
}

func runQQSession(ctx context.Context, cfg config.RobotQQConfig, h MessageHandler, logger *zap.Logger) error {
	appID := strings.TrimSpace(cfg.AppID)
	secret := strings.TrimSpace(cfg.ClientSecret)
	credentials := &token.QQBotCredentials{AppID: appID, AppSecret: secret}
	tokenSource := token.NewQQBotTokenSource(credentials)

	if err := token.StartRefreshAccessToken(ctx, tokenSource); err != nil {
		return err
	}

	var api openapi.OpenAPI
	if cfg.Sandbox {
		api = botgo.NewSandboxOpenAPI(appID, tokenSource)
	} else {
		api = botgo.NewOpenAPI(appID, tokenSource)
	}

	qqHandlerMu.Lock()
	qqHandler = h
	qqLogger = logger
	qqAPI = api
	qqHandlerMu.Unlock()
	defer func() {
		qqHandlerMu.Lock()
		qqHandler = nil
		qqLogger = nil
		qqAPI = nil
		qqHandlerMu.Unlock()
	}()

	intents := event.RegisterHandlers(
		event.C2CMessageEventHandler(handleQQC2CMessage),
		event.GroupATMessageEventHandler(handleQQGroupATMessage),
	)

	wsInfo, err := api.WS(ctx, nil, "")
	if err != nil {
		return err
	}
	logger.Info("QQ bot WebSocket connecting…", zap.String("app_id", appID), zap.Bool("sandbox", cfg.Sandbox))

	done := make(chan error, 1)
	go func() {
		done <- botgo.NewSessionManager().Start(wsInfo, tokenSource, &intents)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return err
		}
		return nil
	}
}

func handleQQC2CMessage(payload *dto.WSPayload, data *dto.WSC2CMessageData) error {
	if data == nil || data.Author == nil {
		return nil
	}
	text := strings.TrimSpace(data.Content)
	if text == "" {
		return nil
	}
	userOpenID := strings.TrimSpace(data.Author.ID)
	if userOpenID == "" {
		return nil
	}
	userID := "u:" + userOpenID
	qqHandlerMu.Lock()
	h := qqHandler
	logger := qqLogger
	api := qqAPI
	qqHandlerMu.Unlock()
	if h == nil || api == nil {
		return nil
	}
	logger.Info("QQ received C2C message", zap.String("from", userID), zap.String("content", text))
	reply := h.HandleMessage(qqPlatform, userID, text)
	return qqPostC2CReply(context.Background(), api, userOpenID, payload, data.ID, reply, logger)
}

func handleQQGroupATMessage(payload *dto.WSPayload, data *dto.WSGroupATMessageData) error {
	if data == nil || data.Author == nil {
		return nil
	}
	text := strings.TrimSpace(data.Content)
	if text == "" {
		return nil
	}
	userOpenID := strings.TrimSpace(data.Author.ID)
	groupID := strings.TrimSpace(data.GroupID)
	if userOpenID == "" {
		return nil
	}
	userID := "g:" + groupID + "|u:" + userOpenID
	qqHandlerMu.Lock()
	h := qqHandler
	logger := qqLogger
	api := qqAPI
	qqHandlerMu.Unlock()
	if h == nil || api == nil {
		return nil
	}
	logger.Info("QQ received group @ mention", zap.String("from", userID), zap.String("content", text))
	reply := h.HandleMessage(qqPlatform, userID, text)
	return qqPostGroupReply(context.Background(), api, groupID, payload, data.ID, reply, logger)
}

func qqPostC2CReply(ctx context.Context, api openapi.OpenAPI, userOpenID string, payload *dto.WSPayload, msgID, reply string, logger *zap.Logger) error {
	reply = trimReply(reply)
	if reply == "" {
		return nil
	}
	for _, chunk := range splitTextChunks(reply, qqMaxMessageRunes) {
		msg := &dto.MessageToCreate{
			Content: chunk,
			MsgID:   msgID,
		}
		if payload != nil && payload.EventID != "" {
			msg.EventID = payload.EventID
		}
		if _, err := api.PostC2CMessage(ctx, userOpenID, msg); err != nil {
			logger.Warn("failed to send the QQ C2C reply", zap.String("to", userOpenID), zap.Error(err))
			return err
		}
	}
	return nil
}

func qqPostGroupReply(ctx context.Context, api openapi.OpenAPI, groupID string, payload *dto.WSPayload, msgID, reply string, logger *zap.Logger) error {
	reply = trimReply(reply)
	if reply == "" {
		return nil
	}
	for _, chunk := range splitTextChunks(reply, qqMaxMessageRunes) {
		msg := &dto.MessageToCreate{
			Content: chunk,
			MsgID:   msgID,
		}
		if payload != nil && payload.EventID != "" {
			msg.EventID = payload.EventID
		}
		if _, err := api.PostGroupMessage(ctx, groupID, msg); err != nil {
			logger.Warn("failed to send the QQ group reply", zap.String("group", groupID), zap.Error(err))
			return err
		}
	}
	return nil
}
