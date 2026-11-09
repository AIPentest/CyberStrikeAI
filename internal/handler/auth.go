package handler

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AuthHandler handles authentication-related endpoints.
type AuthHandler struct {
	manager    *security.AuthManager
	config     *config.Config
	configPath string
	logger     *zap.Logger
	audit      *audit.Service
}

// SetAudit wires platform audit logging.
func (h *AuthHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(manager *security.AuthManager, cfg *config.Config, configPath string, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{
		manager:    manager,
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password" binding:"required"`
}

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// Login verifies password and returns a session token.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "密码不能为空", "password must not be empty")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || utf8.RuneCountInString(req.Username) > 64 {
		respondError(c, http.StatusBadRequest, "用户名不能为空，且不能超过 64 个字符", "username must not be empty and no more than 64 characters")
		return
	}

	token, expiresAt, err := h.manager.Authenticate(req.Username, req.Password)
	if err != nil {
		if h.audit != nil {
			h.audit.Record(c, audit.Entry{
				Level:    "warn",
				Category: "auth",
				Action:   "login",
				Result:   "failure",
				// Legacy Chinese audit message is stored byte-identically: frontend
				// audit.js AUDIT_MSG_BY_STORED_TEXT maps this exact text.
				Message:  "登录失败：密码错误",
				Actor:    strings.TrimSpace(req.Username),
			})
		}
		respondError(c, http.StatusUnauthorized, "密码错误", "wrong password")
		return
	}
	session, _ := h.manager.ValidateToken(token)

	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category:    "auth",
			Action:      "login",
			Result:      "success",
			SessionHint: audit.HintFromToken(token),
			Message:     "login succeeded",
			Actor:       session.Username,
			Detail: map[string]interface{}{
				"expires_at": expiresAt.UTC().Format(time.RFC3339),
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"token":               token,
		"expires_at":          expiresAt.UTC().Format(time.RFC3339),
		"session_duration_hr": h.manager.SessionDurationHours(),
		"user": gin.H{
			"id":           session.UserID,
			"username":     session.Username,
			"display_name": session.DisplayName,
		},
		"roles":             session.Roles,
		"permissions":       permissionKeys(session.Permissions),
		"permission_scopes": session.PermissionScopes,
		"scope":             session.Scope,
	})
}

// Logout revokes the current session token.
func (h *AuthHandler) Logout(c *gin.Context) {
	token := c.GetString(security.ContextAuthTokenKey)
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		} else {
			token = strings.TrimSpace(authHeader)
		}
	}

	h.manager.RevokeToken(token)
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category: "auth",
			Action:   "logout",
			Result:   "success",
			Message:  "logged out",
		})
	}
	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "已退出登录", "logged out")})
}

// ChangePassword updates the login password.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "参数无效", "invalid parameter")
		return
	}

	oldPassword := strings.TrimSpace(req.OldPassword)
	newPassword := strings.TrimSpace(req.NewPassword)

	if oldPassword == "" || newPassword == "" {
		respondError(c, http.StatusBadRequest, "当前密码和新密码均不能为空", "current and new passwords must not be empty")
		return
	}

	if len(newPassword) < 8 {
		respondError(c, http.StatusBadRequest, "新密码长度至少需要 8 位", "new password must be at least 8 characters")
		return
	}

	if oldPassword == newPassword {
		respondError(c, http.StatusBadRequest, "新密码不能与旧密码相同", "new password must differ from the old password")
		return
	}

	session, _ := security.CurrentSession(c)
	if session.Username == "" {
		session.Username = "admin"
	}
	if !h.manager.CheckUserPassword(session.Username, oldPassword) {
		if h.audit != nil {
			h.audit.Record(c, audit.Entry{
				Level:    "warn",
				Category: "auth",
				Action:   "change_password",
				Result:   "failure",
				// Legacy Chinese audit message is stored byte-identically: frontend
				// audit.js AUDIT_MSG_BY_STORED_TEXT maps this exact text.
				Message:  "修改密码失败：当前密码不正确",
			})
		}
		respondError(c, http.StatusBadRequest, "当前密码不正确", "current password is incorrect")
		return
	}

	if session.UserID == "" {
		session.UserID = "admin"
	}
	if err := h.manager.UpdateUserPassword(session.UserID, newPassword); err != nil {
		if h.logger != nil {
			h.logger.Error("failed to update user password", zap.Error(err))
		}
		respondError(c, http.StatusInternalServerError, "更新用户密码失败", "failed to update user password")
		return
	}

	if h.logger != nil {
		h.logger.Info("login password updated, all sessions revoked")
	}

	if h.audit != nil {
		h.audit.Record(c, audit.Entry{
			Category: "auth",
			Action:   "change_password",
			Result:   "success",
			Message:  "login password changed",
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": i18nText(requestWantsEnglish(c), "密码已更新，请使用新密码重新登录", "password updated, please log in again with the new password")})
}

// Validate returns the current session status.
func (h *AuthHandler) Validate(c *gin.Context) {
	token := c.GetString(security.ContextAuthTokenKey)
	if token == "" {
		respondError(c, http.StatusUnauthorized, "会话无效", "invalid session")
		return
	}

	session, ok := h.manager.ValidateToken(token)
	if !ok {
		respondError(c, http.StatusUnauthorized, "会话已过期", "session expired")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      session.Token,
		"expires_at": session.ExpiresAt.UTC().Format(time.RFC3339),
		"user": gin.H{
			"id":           session.UserID,
			"username":     session.Username,
			"display_name": session.DisplayName,
		},
		"roles":             session.Roles,
		"permissions":       permissionKeys(session.Permissions),
		"permission_scopes": session.PermissionScopes,
		"scope":             session.Scope,
	})
}

func permissionKeys(perms map[string]bool) []string {
	keys := make([]string, 0, len(perms))
	for key, ok := range perms {
		if ok {
			keys = append(keys, key)
		}
	}
	return keys
}
