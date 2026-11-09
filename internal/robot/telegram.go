package robot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

const (
	telegramPlatform       = "telegram"
	telegramAPIBase        = "https://api.telegram.org"
	telegramLongPollSec    = 30
	telegramMaxMessageRunes = 4096
)

type telegramUpdate struct {
	UpdateID int              `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64           `json:"message_id"`
	Chat      telegramChat    `json:"chat"`
	From      *telegramUser   `json:"from"`
	Text      string          `json:"text"`
	Entities  []telegramEntity `json:"entities"`
}

type telegramChat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type telegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

type telegramEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type telegramGetUpdatesResp struct {
	OK          bool             `json:"ok"`
	Result      []telegramUpdate `json:"result"`
	Description string           `json:"description"`
}

type telegramBotMe struct {
	OK     bool `json:"ok"`
	Result struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"result"`
}

// StartTelegram 启动 Telegram Bot 长轮询（getUpdates，无需公网回调）。
func StartTelegram(ctx context.Context, robotsCfg config.RobotsConfig, h MessageHandler, logger *zap.Logger) {
	cfg := robotsCfg.Telegram
	if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
		return
	}
	go runTelegramLoop(ctx, cfg, h, logger)
}

func runTelegramLoop(ctx context.Context, cfg config.RobotTelegramConfig, h MessageHandler, logger *zap.Logger) {
	backoff := reconnectInitial
	for {
		err := runTelegramPoll(ctx, cfg, h, logger)
		if ctx.Err() != nil {
			logger.Info("Telegram long-polling closed by configuration")
			return
		}
		if err != nil {
			logger.Warn("Telegram long-polling error; reconnecting automatically", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		if !waitReconnect(ctx, &backoff) {
			return
		}
	}
}

func runTelegramPoll(ctx context.Context, cfg config.RobotTelegramConfig, h MessageHandler, logger *zap.Logger) error {
	token := strings.TrimSpace(cfg.BotToken)
	botUsername := strings.TrimSpace(cfg.BotUsername)
	if botUsername == "" {
		if name, err := telegramGetMe(ctx, token); err != nil {
			logger.Warn("Telegram getMe failed", zap.Error(err))
		} else {
			botUsername = name
		}
	}
	offset := cfg.UpdateOffset
	logger.Info("Telegram long-polling started", zap.String("bot", botUsername))
	client := &http.Client{Timeout: telegramLongPollSec*time.Second + 10*time.Second}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := telegramGetUpdates(ctx, client, token, offset)
		if err != nil {
			return err
		}
		for _, u := range updates {
			next := int64(u.UpdateID) + 1
			if next > offset {
				offset = next
			}
			if u.Message == nil || u.Message.From == nil || u.Message.From.IsBot {
				continue
			}
			text := strings.TrimSpace(u.Message.Text)
			if text == "" {
				continue
			}
			chatType := strings.ToLower(strings.TrimSpace(u.Message.Chat.Type))
			if chatType != "private" {
				if !cfg.AllowGroupMessages {
					continue
				}
				if botUsername != "" && !telegramMentionsBot(text, u.Message.Entities, botUsername) {
					continue
				}
			}
			userID := telegramSessionKey(chatType, u.Message.Chat.ID, u.Message.From.ID)
			logger.Info("Telegram received message", zap.String("from", userID), zap.String("content", text))
			reply := h.HandleMessage(telegramPlatform, userID, text)
			if err := telegramSendReply(ctx, client, token, u.Message.Chat.ID, reply); err != nil {
				logger.Warn("failed to send the Telegram reply", zap.String("to", userID), zap.Error(err))
			}
		}
	}
}

func telegramSessionKey(chatType string, chatID, fromUserID int64) string {
	if chatType == "private" {
		return fmt.Sprintf("u:%d", fromUserID)
	}
	return fmt.Sprintf("g:%d|u:%d", chatID, fromUserID)
}

func telegramMentionsBot(text string, entities []telegramEntity, botUsername string) bool {
	needle := "@" + strings.TrimPrefix(strings.ToLower(botUsername), "@")
	lower := strings.ToLower(text)
	if strings.Contains(lower, needle) {
		return true
	}
	for _, e := range entities {
		if e.Type != "mention" {
			continue
		}
		if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > len(text) {
			continue
		}
		mention := strings.ToLower(text[e.Offset : e.Offset+e.Length])
		if mention == needle {
			return true
		}
	}
	return false
}

func telegramAPIURL(token, method string) string {
	return fmt.Sprintf("%s/bot%s/%s", telegramAPIBase, token, method)
}

func telegramGetMe(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, telegramAPIURL(token, "getMe"), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed telegramBotMe
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if !parsed.OK {
		return "", fmt.Errorf("getMe failed: %s", string(body))
	}
	return parsed.Result.Username, nil
}

func telegramGetUpdates(ctx context.Context, client *http.Client, token string, offset int64) ([]telegramUpdate, error) {
	url := fmt.Sprintf("%s?timeout=%d&allowed_updates=%s", telegramAPIURL(token, "getUpdates"), telegramLongPollSec, `["message"]`)
	if offset > 0 {
		url += fmt.Sprintf("&offset=%d", offset)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed telegramGetUpdatesResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if !parsed.OK {
		if parsed.Description != "" {
			return nil, fmt.Errorf("getUpdates: %s", parsed.Description)
		}
		return nil, fmt.Errorf("getUpdates failed: %s", string(body))
	}
	return parsed.Result, nil
}

func telegramSendReply(ctx context.Context, client *http.Client, token string, chatID int64, reply string) error {
	reply = trimReply(reply)
	if reply == "" {
		return nil
	}
	for _, chunk := range splitTextChunks(reply, telegramMaxMessageRunes) {
		payload := map[string]interface{}{
			"chat_id": chatID,
			"text":    chunk,
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPIURL(token, "sendMessage"), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("sendMessage status %d", resp.StatusCode)
		}
	}
	return nil
}
