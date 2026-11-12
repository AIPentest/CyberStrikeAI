package robot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/egress"

	"github.com/bwmarrin/discordgo"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/slack-go/slack"
)

// SendProactive sends a message without an inbound event. Platforms whose
// reply credentials are event-scoped deliberately return an error instead of
// pretending delivery succeeded.
func SendProactive(ctx context.Context, cfg config.RobotsConfig, platform, externalUserID, message string) error {
	platform = strings.ToLower(strings.TrimSpace(platform))
	userID := robotIdentityUserPart(externalUserID)
	if userID == "" {
		return fmt.Errorf("invalid robot recipient")
	}
	switch platform {
	case "telegram":
		if !cfg.Telegram.Enabled || strings.TrimSpace(cfg.Telegram.BotToken) == "" {
			return fmt.Errorf("telegram is not configured")
		}
		id, err := strconv.ParseInt(userID, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid telegram user id: %w", err)
		}
		return telegramSendReply(ctx, nilSafeHTTPClient(), strings.TrimSpace(cfg.Telegram.BotToken), id, message)
	case "slack":
		if !cfg.Slack.Enabled || strings.TrimSpace(cfg.Slack.BotToken) == "" {
			return fmt.Errorf("slack is not configured")
		}
		api := slack.New(strings.TrimSpace(cfg.Slack.BotToken))
		channel, _, _, err := api.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: []string{userID}})
		if err != nil {
			return err
		}
		for _, chunk := range splitTextChunks(message, slackMaxMessageRunes) {
			if _, _, err = api.PostMessageContext(ctx, channel.ID, slack.MsgOptionText(chunk, false)); err != nil {
				return err
			}
		}
		return nil
	case "discord":
		if !cfg.Discord.Enabled || strings.TrimSpace(cfg.Discord.BotToken) == "" {
			return fmt.Errorf("discord is not configured")
		}
		token := strings.TrimSpace(cfg.Discord.BotToken)
		if !strings.HasPrefix(token, "Bot ") {
			token = "Bot " + token
		}
		session, err := discordgo.New(token)
		if err != nil {
			return err
		}
		channel, err := session.UserChannelCreate(userID)
		if err != nil {
			return err
		}
		for _, chunk := range splitTextChunks(message, discordMaxMessageRunes) {
			if _, err = session.ChannelMessageSend(channel.ID, chunk); err != nil {
				return err
			}
		}
		return nil
	case "wecom":
		return sendWecomProactive(ctx, cfg.Wecom, userID, message)
	case "lark":
		return sendLarkProactive(ctx, cfg.Lark, externalUserID, message)
	default:
		return fmt.Errorf("platform %s does not support proactive alerts yet", platform)
	}
}

func SupportsProactive(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "telegram", "slack", "discord", "wecom", "lark":
		return true
	default:
		return false
	}
}

func nilSafeHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: egress.Default().Transport()}
}

func sendWecomProactive(ctx context.Context, cfg config.RobotWecomConfig, userID, message string) error {
	if !cfg.Enabled || strings.TrimSpace(cfg.CorpID) == "" || strings.TrimSpace(cfg.Secret) == "" || cfg.AgentID == 0 {
		return fmt.Errorf("wecom proactive API is not configured")
	}
	tokenURL := "https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=" + cfg.CorpID + "&corpsecret=" + cfg.Secret
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return err
	}
	resp, err := nilSafeHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var tokenResp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return err
	}
	if tokenResp.ErrCode != 0 || tokenResp.AccessToken == "" {
		return fmt.Errorf("wecom token: %s", tokenResp.ErrMsg)
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"touser": userID, "msgtype": "text", "agentid": cfg.AgentID,
		"text": map[string]string{"content": message}, "safe": 0,
	})
	sendReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token="+tokenResp.AccessToken, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	sendReq.Header.Set("Content-Type", "application/json")
	sendResp, err := nilSafeHTTPClient().Do(sendReq)
	if err != nil {
		return err
	}
	defer sendResp.Body.Close()
	result, _ := io.ReadAll(sendResp.Body)
	var parsed struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return err
	}
	if parsed.ErrCode != 0 {
		return fmt.Errorf("wecom send: %s", parsed.ErrMsg)
	}
	return nil
}

func sendLarkProactive(ctx context.Context, cfg config.RobotLarkConfig, identity, message string) error {
	if !cfg.Enabled || strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return fmt.Errorf("lark is not configured")
	}
	receiveIDType, receiveID := "user_id", robotIdentityUserPart(identity)
	if idx := strings.LastIndex(identity, "|o:"); idx >= 0 {
		receiveIDType, receiveID = "open_id", strings.TrimSpace(identity[idx+3:])
	}
	if idx := strings.LastIndex(identity, "|n:"); idx >= 0 {
		receiveIDType, receiveID = "union_id", strings.TrimSpace(identity[idx+3:])
	}
	if receiveID == "" {
		return fmt.Errorf("invalid lark recipient")
	}
	content, _ := json.Marshal(larkTextContent{Text: message})
	client := lark.NewClient(cfg.AppID, cfg.AppSecret)
	resp, err := client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(receiveID).MsgType(larkim.MsgTypeText).Content(string(content)).Build()).Build())
	if err != nil {
		return err
	}
	if resp == nil || !resp.Success() {
		return fmt.Errorf("lark send failed")
	}
	return nil
}

func robotIdentityUserPart(identity string) string {
	identity = strings.TrimSpace(identity)
	if i := strings.LastIndex(identity, "|u:"); i >= 0 {
		return strings.TrimSpace(identity[i+3:])
	}
	if strings.HasPrefix(identity, "u:") {
		return strings.TrimSpace(identity[2:])
	}
	return identity
}
