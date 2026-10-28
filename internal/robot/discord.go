package robot

import (
	"context"
	"strings"

	"cyberstrike-ai/internal/config"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

const (
	discordPlatform        = "discord"
	discordMaxMessageRunes = 2000
)

// StartDiscord 启动 Discord Gateway（WebSocket，无需公网回调）。
func StartDiscord(ctx context.Context, robotsCfg config.RobotsConfig, h MessageHandler, logger *zap.Logger) {
	cfg := robotsCfg.Discord
	if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
		return
	}
	go runDiscordLoop(ctx, cfg, h, logger)
}

func runDiscordLoop(ctx context.Context, cfg config.RobotDiscordConfig, h MessageHandler, logger *zap.Logger) {
	backoff := reconnectInitial
	for {
		err := runDiscordSession(ctx, cfg, h, logger)
		if ctx.Err() != nil {
			logger.Info("Discord Gateway closed by configuration")
			return
		}
		if err != nil {
			logger.Warn("Discord Gateway error; reconnecting automatically", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		if !waitReconnect(ctx, &backoff) {
			return
		}
	}
}

func runDiscordSession(ctx context.Context, cfg config.RobotDiscordConfig, h MessageHandler, logger *zap.Logger) error {
	token := strings.TrimSpace(cfg.BotToken)
	if !strings.HasPrefix(token, "Bot ") {
		token = "Bot " + token
	}
	session, err := discordgo.New(token)
	if err != nil {
		return err
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentMessageContent

	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m == nil || m.Author == nil || m.Author.Bot {
			return
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			return
		}
		if m.GuildID != "" {
			if !cfg.AllowGuildMessages {
				return
			}
			if s.State.User == nil || !discordMentionsBot(m, s.State.User.ID) {
				return
			}
		}
		userID := discordSessionKey(m.GuildID, m.Author.ID)
		logger.Info("Discord received message", zap.String("from", userID), zap.String("content", text))
		reply := h.HandleMessage(discordPlatform, userID, text)
		discordPostReply(s, m.ChannelID, reply, logger)
	})

	if err := session.Open(); err != nil {
		return err
	}
	logger.Info("Discord Gateway connected; waiting for messages")
	defer session.Close()

	<-ctx.Done()
	return ctx.Err()
}

func discordMentionsBot(m *discordgo.MessageCreate, botUserID string) bool {
	if m == nil || botUserID == "" {
		return false
	}
	for _, mention := range m.Mentions {
		if mention != nil && mention.ID == botUserID {
			return true
		}
	}
	return strings.Contains(m.Content, "<@"+botUserID+">") || strings.Contains(m.Content, "<@!"+botUserID+">")
}

func discordSessionKey(guildID, userID string) string {
	guildID = strings.TrimSpace(guildID)
	userID = strings.TrimSpace(userID)
	if guildID == "" {
		return "u:" + userID
	}
	return "g:" + guildID + "|u:" + userID
}

func discordPostReply(s *discordgo.Session, channelID, reply string, logger *zap.Logger) {
	reply = trimReply(reply)
	if reply == "" {
		return
	}
	for _, chunk := range splitTextChunks(reply, discordMaxMessageRunes) {
		if _, err := s.ChannelMessageSend(channelID, chunk); err != nil {
			logger.Warn("failed to send the Discord reply", zap.String("channel", channelID), zap.Error(err))
			return
		}
	}
}
