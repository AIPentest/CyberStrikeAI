package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/robot/ilink"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const wechatLoginTTL = 5 * time.Minute

// WechatConfigSaver 绑定成功后写入配置并重启机器人连接
type WechatConfigSaver interface {
	ApplyWechatRobotBinding(cfg config.RobotWechatConfig) error
}

type wechatLoginSession struct {
	QRCode           string
	QRCodeImgURL     string
	PendingVerify    string
	CurrentBaseURL   string
	StartedAt        time.Time
}

// WechatRobotHandler 微信 iLink 机器人（扫码绑定 + 配置）
type WechatRobotHandler struct {
	config       *config.Config
	configSaver  WechatConfigSaver
	logger       *zap.Logger
	mu           sync.Mutex
	logins       map[string]*wechatLoginSession
}

// NewWechatRobotHandler 创建微信机器人处理器
func NewWechatRobotHandler(cfg *config.Config, saver WechatConfigSaver, logger *zap.Logger) *WechatRobotHandler {
	return &WechatRobotHandler{
		config:      cfg,
		configSaver: saver,
		logger:      logger,
		logins:      make(map[string]*wechatLoginSession),
	}
}

func (h *WechatRobotHandler) purgeExpiredLogins() {
	now := time.Now()
	for k, v := range h.logins {
		if now.Sub(v.StartedAt) > wechatLoginTTL {
			delete(h.logins, k)
		}
	}
}

func (h *WechatRobotHandler) ilinkClient(baseURL string) *ilink.Client {
	ver := h.config.Version
	if ver == "" {
		ver = "1.0.0"
	}
	ver = strings.TrimPrefix(strings.TrimSpace(ver), "v")
	ver = strings.TrimPrefix(ver, "V")
	wc := h.config.Robots.Wechat
	return ilink.NewClient(baseURL, wc.BotToken, wc.BotAgent, ilink.BuildClientVersion(ver))
}

// HandleWechatQRCode POST /api/robot/wechat/qrcode — 生成绑定二维码
func (h *WechatRobotHandler) HandleWechatQRCode(c *gin.Context) {
	h.mu.Lock()
	h.purgeExpiredLogins()
	h.mu.Unlock()

	var req struct {
		BotType string `json:"bot_type"`
	}
	_ = c.ShouldBindJSON(&req)

	botType := req.BotType
	if botType == "" {
		botType = h.config.Robots.Wechat.BotType
	}
	if botType == "" {
		botType = ilink.DefaultBotType
	}
	baseURL := h.config.Robots.Wechat.BaseURL
	if baseURL == "" {
		baseURL = ilink.DefaultBaseURL
	}

	var localTokens []string
	if t := h.config.Robots.Wechat.BotToken; t != "" {
		localTokens = []string{t}
	}

	client := h.ilinkClient(baseURL)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	qr, err := client.GetBotQRCode(ctx, botType, localTokens)
	if err != nil {
		h.logger.Warn("获取微信二维码失败", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "获取二维码失败: %s", "failed to get QR code: %s"), err.Error())})
		return
	}
	if qr.QRCode == "" || qr.QRCodeImgContent == "" {
		respondError(c, http.StatusBadGateway, "微信服务器未返回有效二维码", "WeChat server did not return a valid QR code")
		return
	}

	sessionKey := uuid.New().String()
	h.mu.Lock()
	h.logins[sessionKey] = &wechatLoginSession{
		QRCode:         qr.QRCode,
		QRCodeImgURL:   qr.QRCodeImgContent,
		CurrentBaseURL: baseURL,
		StartedAt:      time.Now(),
	}
	h.mu.Unlock()

	resp := gin.H{
		"session_key":     sessionKey,
		"qrcode":          qr.QRCode,
		"qrcode_open_url": qr.QRCodeImgContent,
		"message":         i18nText(requestWantsEnglish(c), "请使用微信扫描二维码并确认绑定", "scan the QR code with WeChat and confirm binding"),
	}
	if dataURL, err := ilink.QRCodeDataURL(qr.QRCodeImgContent, 256); err != nil {
		h.logger.Warn("生成二维码图片失败", zap.Error(err))
	} else {
		resp["qrcode_image_data_url"] = dataURL
	}

	c.JSON(http.StatusOK, resp)
}

// HandleWechatQRCodeStatus GET /api/robot/wechat/qrcode/status — 轮询扫码状态
func (h *WechatRobotHandler) HandleWechatQRCodeStatus(c *gin.Context) {
	sessionKey := c.Query("session_key")
	verifyCode := c.Query("verify_code")
	if sessionKey == "" {
		respondError(c, http.StatusBadRequest, "缺少 session_key", "session_key missing")
		return
	}

	h.mu.Lock()
	sess, ok := h.logins[sessionKey]
	h.mu.Unlock()
	if !ok {
		respondError(c, http.StatusNotFound, "登录会话不存在或已过期，请重新生成二维码", "login session not found or expired; generate a new QR code")
		return
	}
	if time.Since(sess.StartedAt) > wechatLoginTTL {
		h.mu.Lock()
		delete(h.logins, sessionKey)
		h.mu.Unlock()
		respondError(c, http.StatusGone, "二维码已过期，请重新生成", "QR code expired; generate a new one")
		return
	}

	baseURL := sess.CurrentBaseURL
	if baseURL == "" {
		baseURL = ilink.DefaultBaseURL
	}
	vc := verifyCode
	if vc == "" {
		vc = sess.PendingVerify
	}

	client := h.ilinkClient(baseURL)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()

	st, err := client.GetQRCodeStatus(ctx, sess.QRCode, vc)
	if err != nil {
		h.logger.Warn("轮询微信二维码状态失败", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	switch st.Status {
	case "wait", "scaned":
		c.JSON(http.StatusOK, gin.H{"status": st.Status})
		return
	case "need_verifycode":
		c.JSON(http.StatusOK, gin.H{
			"status":  st.Status,
			"message": i18nText(requestWantsEnglish(c), "请在手机微信查看配对数字，并在下方输入", "check the pairing digits in WeChat on your phone and enter them below"),
		})
		return
	case "scaned_but_redirect":
		if st.RedirectHost != "" {
			h.mu.Lock()
			if s, ok := h.logins[sessionKey]; ok {
				s.CurrentBaseURL = "https://" + st.RedirectHost
			}
			h.mu.Unlock()
		}
		c.JSON(http.StatusOK, gin.H{"status": st.Status})
		return
	case "binded_redirect":
		h.mu.Lock()
		delete(h.logins, sessionKey)
		h.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"status":            st.Status,
			"already_connected": true,
			"message":           i18nText(requestWantsEnglish(c), "该微信已绑定过，无需重复绑定", "this WeChat account was already bound; no need to bind again"),
		})
		return
	case "confirmed":
		if st.BotToken == "" || st.ILinkBotID == "" {
			respondError(c, http.StatusBadGateway, "绑定确认成功但缺少 bot_token", "binding confirmed but bot_token missing")
			return
		}
		saveBase := st.BaseURL
		if saveBase == "" {
			saveBase = baseURL
		}
		wc := h.config.Robots.Wechat
		wc.Enabled = true
		wc.BotToken = st.BotToken
		wc.ILinkBotID = st.ILinkBotID
		wc.ILinkUserID = st.ILinkUserID
		wc.BaseURL = saveBase
		if wc.BotType == "" {
			wc.BotType = ilink.DefaultBotType
		}
		if wc.BotAgent == "" {
			wc.BotAgent = ilink.DefaultBotAgent
		}
		if h.configSaver != nil {
			if err := h.configSaver.ApplyWechatRobotBinding(wc); err != nil {
				h.logger.Warn("保存微信机器人配置失败", zap.Error(err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf(i18nText(requestWantsEnglish(c), "保存配置失败: %s", "failed to save config: %s"), err.Error())})
				return
			}
		} else {
			h.config.Robots.Wechat = wc
		}
		h.mu.Lock()
		delete(h.logins, sessionKey)
		h.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"status":        "confirmed",
			"message":       i18nText(requestWantsEnglish(c), "绑定成功，微信机器人已启用", "binding successful, WeChat robot enabled"),
			"ilink_bot_id":  st.ILinkBotID,
			"ilink_user_id": st.ILinkUserID,
		})
		return
	default:
		c.JSON(http.StatusOK, gin.H{"status": st.Status})
	}
}

// HandleWechatVerifyCode POST /api/robot/wechat/qrcode/verify — 提交手机配对数字
func (h *WechatRobotHandler) HandleWechatVerifyCode(c *gin.Context) {
	var req struct {
		SessionKey string `json:"session_key"`
		VerifyCode string `json:"verify_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.SessionKey == "" || req.VerifyCode == "" {
		respondError(c, http.StatusBadRequest, "需要 session_key 与 verify_code", "session_key and verify_code are required")
		return
	}
	h.mu.Lock()
	sess, ok := h.logins[req.SessionKey]
	if ok {
		sess.PendingVerify = req.VerifyCode
	}
	h.mu.Unlock()
	if !ok {
		respondError(c, http.StatusNotFound, "登录会话不存在或已过期", "login session not found or expired")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "已提交配对码，请继续等待绑定", "pairing code submitted, keep waiting for binding")})
}

// HandleWechatStatus GET /api/robot/wechat/status — 当前绑定状态（供前端展示）
func (h *WechatRobotHandler) HandleWechatStatus(c *gin.Context) {
	wc := h.config.Robots.Wechat
	bound := wc.BotToken != "" && wc.ILinkBotID != ""
	c.JSON(http.StatusOK, gin.H{
		"enabled":       wc.Enabled,
		"bound":         bound,
		"ilink_bot_id":  wc.ILinkBotID,
		"ilink_user_id": wc.ILinkUserID,
		"base_url":      wc.BaseURL,
	})
}
