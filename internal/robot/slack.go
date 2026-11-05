package robot

import (
	"context"
	"strings"

	"cyberstrike-ai/internal/config"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"go.uber.org/zap"
)

const (
	slackPlatform        = "slack"
	slackMaxMessageRunes = 3900
)

// StartSlack 启动 Slack Socket Mode（出站 WebSocket，无需公网回调）。
func StartSlack(ctx context.Context, robotsCfg config.RobotsConfig, h MessageHandler, logger *zap.Logger) {
	cfg := robotsCfg.Slack
	if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" || strings.TrimSpace(cfg.AppToken) == "" {
		return
	}
	go runSlackLoop(ctx, cfg, h, logger)
}

func runSlackLoop(ctx context.Context, cfg config.RobotSlackConfig, h MessageHandler, logger *zap.Logger) {
	backoff := reconnectInitial
	for {
		err := runSlackSocket(ctx, cfg, h, logger)
		if ctx.Err() != nil {
			logger.Info("Slack Socket Mode closed by configuration")
			return
		}
		if err != nil {
			logger.Warn("Slack Socket Mode error; reconnecting automatically", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		if !waitReconnect(ctx, &backoff) {
			return
		}
	}
}

func runSlackSocket(ctx context.Context, cfg config.RobotSlackConfig, h MessageHandler, logger *zap.Logger) error {
	api := slack.New(
		strings.TrimSpace(cfg.BotToken),
		slack.OptionAppLevelToken(strings.TrimSpace(cfg.AppToken)),
	)
	client := socketmode.New(api)
	logger.Info("Slack Socket Mode connecting…")

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				client.Ack(*evt.Request)
				if eventsAPIEvent.Type != slackevents.CallbackEvent {
					continue
				}
				switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					handleSlackMessage(ctx, api, eventsAPIEvent.TeamID, ev, h, logger)
				case *slackevents.AppMentionEvent:
					handleSlackAppMention(ctx, api, eventsAPIEvent.TeamID, ev, h, logger)
				}
			case socketmode.EventTypeConnecting:
				logger.Info("Slack Socket Mode connecting…")
			case socketmode.EventTypeConnected:
				logger.Info("Slack Socket Mode connected; waiting for messages")
			}
		}
	}()

	return client.RunContext(ctx)
}

func handleSlackMessage(ctx context.Context, api *slack.Client, teamID string, ev *slackevents.MessageEvent, h MessageHandler, logger *zap.Logger) {
	if ev == nil || ev.BotID != "" || ev.SubType != "" {
		return
	}
	if ev.ChannelType != "im" {
		return
	}
	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return
	}
	userID := slackSessionKey(teamID, ev.User)
	logger.Info("Slack received message", zap.String("from", userID), zap.String("content", text))
	reply := h.HandleMessage(slackPlatform, userID, text)
	slackPostReply(ctx, api, ev.Channel, reply, logger)
}

func handleSlackAppMention(ctx context.Context, api *slack.Client, teamID string, ev *slackevents.AppMentionEvent, h MessageHandler, logger *zap.Logger) {
	if ev == nil || ev.BotID != "" {
		return
	}
	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return
	}
	userID := slackSessionKey(teamID, ev.User)
	logger.Info("Slack received @ mention", zap.String("from", userID), zap.String("content", text))
	reply := h.HandleMessage(slackPlatform, userID, text)
	slackPostReply(ctx, api, ev.Channel, reply, logger)
}

func slackSessionKey(teamID, userID string) string {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if teamID == "" {
		teamID = "default"
	}
	return "t:" + teamID + "|u:" + userID
}

func slackPostReply(ctx context.Context, api *slack.Client, channel, reply string, logger *zap.Logger) {
	reply = trimReply(reply)
	if reply == "" {
		return
	}
	for _, chunk := range splitTextChunks(reply, slackMaxMessageRunes) {
		_, _, err := api.PostMessageContext(ctx, channel, slack.MsgOptionText(chunk, false))
		if err != nil {
			logger.Warn("failed to send the Slack reply", zap.String("channel", channel), zap.Error(err))
			return
		}
	}
}
