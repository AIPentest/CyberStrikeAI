package ilink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL    = "https://ilinkai.weixin.qq.com"
	DefaultBotType    = "3"
	DefaultBotAgent   = "CyberStrikeAI/1.0"
	ILinkAppID        = "bot"
	QRLongPollTimeout = 35 * time.Second
	APIDefaultTimeout = 15 * time.Second
	GetUpdatesTimeout = 35 * time.Second
)

// Client The WeChat iLink Bot HTTP client (with @tencent-weixin/openclaw-weixin Protocol compatibility)
type Client struct {
	BaseURL       string
	BotToken      string
	BotAgent      string
	ClientVersion uint32
	HTTP          *http.Client
}

func NewClient(baseURL, botToken, botAgent string, clientVersion uint32) *Client {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	agent := strings.TrimSpace(botAgent)
	if agent == "" {
		agent = DefaultBotAgent
	}
	return &Client{
		BaseURL:       strings.TrimRight(base, "/"),
		BotToken:      strings.TrimSpace(botToken),
		BotAgent:      sanitizeBotAgent(agent),
		ClientVersion: clientVersion,
		HTTP:          &http.Client{Timeout: 0},
	}
}

// BuildClientVersion Encodes the semver as iLink-App-ClientVersion (0x00MMNNPP)
func BuildClientVersion(version string) uint32 {
	parts := strings.Split(version, ".")
	parse := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
		if n < 0 {
			return 0
		}
		return n
	}
	major := parse(0) & 0xff
	minor := parse(1) & 0xff
	patch := parse(2) & 0xff
	return uint32((major << 16) | (minor << 8) | patch)
}

type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

func (c *Client) buildBaseInfo() baseInfo {
	return baseInfo{
		ChannelVersion: "1.0.0",
		BotAgent:       c.BotAgent,
	}
}

func randomWechatUIN() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	u := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(u), 10)))
}

func (c *Client) commonHeaders() http.Header {
	h := http.Header{}
	h.Set("iLink-App-Id", ILinkAppID)
	h.Set("iLink-App-ClientVersion", strconv.FormatUint(uint64(c.ClientVersion), 10))
	return h
}

func (c *Client) authHeaders() http.Header {
	h := c.commonHeaders()
	h.Set("Content-Type", "application/json")
	h.Set("AuthorizationType", "ilink_bot_token")
	h.Set("X-WECHAT-UIN", randomWechatUIN())
	if c.BotToken != "" {
		h.Set("Authorization", "Bearer "+c.BotToken)
	}
	return h
}

func (c *Client) endpointURL(path string) (string, error) {
	u, err := url.Parse(c.BaseURL + "/")
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte, headers http.Header, timeout time.Duration) ([]byte, error) {
	reqURL, err := c.endpointURL(path)
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	if timeout > 0 {
		ctx2, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req = req.WithContext(ctx2)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ilink %s %s: %d %s", method, path, resp.StatusCode, string(raw))
	}
	return raw, nil
}

// QRCodeResponse Get QR-code response
type QRCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

// GetBotQRCode Get the binding QR code
func (c *Client) GetBotQRCode(ctx context.Context, botType string, localTokenList []string) (*QRCodeResponse, error) {
	if strings.TrimSpace(botType) == "" {
		botType = DefaultBotType
	}
	body, _ := json.Marshal(map[string]interface{}{
		"local_token_list": localTokenList,
	})
	path := "ilink/bot/get_bot_qrcode?bot_type=" + url.QueryEscape(botType)
	raw, err := c.doRequest(ctx, http.MethodPost, path, body, c.authHeaders(), APIDefaultTimeout)
	if err != nil {
		return nil, err
	}
	var out QRCodeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// QRStatusResponse QR-code status polling response
type QRStatusResponse struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	ILinkBotID   string `json:"ilink_bot_id"`
	ILinkUserID  string `json:"ilink_user_id"`
	BaseURL      string `json:"baseurl"`
	RedirectHost string `json:"redirect_host"`
}

// GetQRCodeStatus Long-polling QR-code scan status
func (c *Client) GetQRCodeStatus(ctx context.Context, qrcode, verifyCode string) (*QRStatusResponse, error) {
	path := "ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(qrcode)
	if verifyCode != "" {
		path += "&verify_code=" + url.QueryEscape(verifyCode)
	}
	raw, err := c.doRequest(ctx, http.MethodGet, path, nil, c.commonHeaders(), QRLongPollTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return &QRStatusResponse{Status: "wait"}, nil
		}
		return &QRStatusResponse{Status: "wait"}, nil
	}
	var out QRStatusResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MessageItem Message content item
type MessageItem struct {
	Type     int `json:"type"`
	TextItem *struct {
		Text string `json:"text"`
	} `json:"text_item,omitempty"`
}

// WeixinMessage Inbound message
type WeixinMessage struct {
	FromUserID   string        `json:"from_user_id"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state"`
	ItemList     []MessageItem `json:"item_list"`
	ContextToken string        `json:"context_token"`
}

// GetUpdatesResponse Long-polling message response
type GetUpdatesResponse struct {
	Ret                  int             `json:"ret"`
	ErrCode              int             `json:"errcode"`
	ErrMsg               string          `json:"errmsg"`
	Msgs                 []WeixinMessage `json:"msgs"`
	GetUpdatesBuf        string          `json:"get_updates_buf"`
	LongPollingTimeoutMs int             `json:"longpolling_timeout_ms"`
}

// GetUpdates Long polling fetches new messages
func (c *Client) GetUpdates(ctx context.Context, getUpdatesBuf string) (*GetUpdatesResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"get_updates_buf": getUpdatesBuf,
		"base_info":       c.buildBaseInfo(),
	})
	raw, err := c.doRequest(ctx, http.MethodPost, "ilink/bot/getupdates", body, c.authHeaders(), GetUpdatesTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return &GetUpdatesResponse{Ret: 0, GetUpdatesBuf: getUpdatesBuf}, nil
		}
		return &GetUpdatesResponse{Ret: 0, GetUpdatesBuf: getUpdatesBuf}, nil
	}
	var out GetUpdatesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SendTextMessage Send a text reply
func (c *Client) SendTextMessage(ctx context.Context, toUserID, contextToken, text, clientID string) error {
	if clientID == "" {
		clientID = randomClientID()
	}
	payload := map[string]interface{}{
		"msg": map[string]interface{}{
			"to_user_id":    toUserID,
			"client_id":     clientID,
			"message_type":  2,
			"message_state": 2,
			"context_token": contextToken,
			"item_list": []map[string]interface{}{
				{"type": 1, "text_item": map[string]string{"text": text}},
			},
		},
		"base_info": c.buildBaseInfo(),
	}
	body, _ := json.Marshal(payload)
	_, err := c.doRequest(ctx, http.MethodPost, "ilink/bot/sendmessage", body, c.authHeaders(), APIDefaultTimeout)
	return err
}

func randomClientID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

func sanitizeBotAgent(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultBotAgent
	}
	if len(raw) > 256 {
		return raw[:256]
	}
	return raw
}

// ExtractText Extracts the first text from the message
func ExtractText(msg WeixinMessage) string {
	for _, item := range msg.ItemList {
		if item.Type == 1 && item.TextItem != nil {
			return strings.TrimSpace(item.TextItem.Text)
		}
	}
	return ""
}
