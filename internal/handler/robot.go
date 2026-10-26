package handler

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/audit"
	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	robotCmdHelp          = "帮助"
	robotCmdList          = "列表"
	robotCmdListAlt       = "对话列表"
	robotCmdSwitch        = "切换"
	robotCmdContinue      = "继续"
	robotCmdNew           = "新对话"
	robotCmdClear         = "清空"
	robotCmdStatus        = "状态"
	robotCmdStop          = "停止"
	robotCmdRoles         = "角色"
	robotCmdRolesList     = "角色列表"
	robotCmdSwitchRole    = "切换角色"
	robotCmdModes         = "模式"
	robotCmdModesList     = "模式列表"
	robotCmdSwitchMode    = "切换模式"
	robotCmdDelete        = "删除"
	robotCmdVersion       = "版本"
	robotCmdProjects      = "项目"
	robotCmdProjectsList  = "项目列表"
	robotCmdBindProject   = "绑定项目"
	robotCmdNewProject    = "新建项目"
	robotCmdUnbindProject = "解除项目"
	robotCmdBindUser      = "绑定"
	robotCmdUnbindUser    = "解绑"
	robotCmdIdentity      = "身份"
	robotCmdTask          = "任务"
	robotCmdRename        = "重命名"
	robotCmdPermissions   = "权限"
	robotCmdDoctor        = "诊断"
	robotCmdConfirm       = "确认"
	robotCmdCancel        = "取消"
	robotCmdVulnAlerts    = "漏洞提醒"
	robotBindingCodeTTL   = 5 * time.Minute
)

type robotPendingConfirmation struct {
	Action    string
	Target    string
	ExpiresAt time.Time
}

// RobotHandler 企业微信/钉钉/飞书等机器人回调处理
type RobotHandler struct {
	config               *config.Config
	db                   *database.DB
	agentHandler         *AgentHandler
	logger               *zap.Logger
	mu                   sync.RWMutex
	sessions             map[string]string             // key: "platform_userID", value: conversationID
	sessionRoles         map[string]string             // key: "platform_userID", value: roleName（默认"默认"）
	sessionModes         map[string]string             // key: "platform_userID", value: agent mode
	cancelMu             sync.Mutex                    // 保护 runningCancels
	runningCancels       map[string]context.CancelFunc // key: "platform_userID", 用于停止命令中断任务
	wecomReplay          map[string]time.Time
	pendingConfirmations map[string]robotPendingConfirmation
	alertWake            chan struct{}
	audit                *audit.Service
}

// NewRobotHandler 创建机器人处理器
func NewRobotHandler(cfg *config.Config, db *database.DB, agentHandler *AgentHandler, logger *zap.Logger) *RobotHandler {
	return &RobotHandler{
		config:               cfg,
		db:                   db,
		agentHandler:         agentHandler,
		logger:               logger,
		sessions:             make(map[string]string),
		sessionRoles:         make(map[string]string),
		sessionModes:         make(map[string]string),
		runningCancels:       make(map[string]context.CancelFunc),
		wecomReplay:          make(map[string]time.Time),
		pendingConfirmations: make(map[string]robotPendingConfirmation),
		alertWake:            make(chan struct{}, 1),
	}
}

func (h *RobotHandler) SetAudit(s *audit.Service) {
	h.audit = s
}

func (h *RobotHandler) acceptFreshWecomRequest(timestamp, nonce, signature string) bool {
	unixSeconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false
	}
	now := time.Now()
	requestTime := time.Unix(unixSeconds, 0)
	if requestTime.Before(now.Add(-5*time.Minute)) || requestTime.After(now.Add(5*time.Minute)) {
		return false
	}
	key := strings.TrimSpace(timestamp) + "\x00" + strings.TrimSpace(nonce) + "\x00" + strings.TrimSpace(signature)
	if strings.TrimSpace(nonce) == "" || strings.TrimSpace(signature) == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for replayKey, seenAt := range h.wecomReplay {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(h.wecomReplay, replayKey)
		}
	}
	if _, exists := h.wecomReplay[key]; exists {
		return false
	}
	h.wecomReplay[key] = now
	return true
}

// sessionKey 生成会话 key
func (h *RobotHandler) sessionKey(platform, userID string) string {
	return platform + "_" + userID
}

func normalizeRobotBindingCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}

func hashRobotBindingCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRobotBindingCode(code)))
	return fmt.Sprintf("%x", sum[:])
}

func (h *RobotHandler) resolveRobotAccess(platform, userID string) (*database.RBACAccess, error) {
	if h.db == nil {
		return nil, fmt.Errorf("机器人鉴权服务不可用")
	}
	authorization := h.config.Robots.AuthorizationFor(platform)
	var access *database.RBACAccess
	var err error
	switch authorization.EffectiveMode() {
	case config.RobotAuthModeUserBinding:
		access, err = h.db.ResolveRobotRBACAccess(platform, userID)
	case config.RobotAuthModeServiceAccount:
		if !authorization.ExternalUserAllowed(userID) {
			return nil, fmt.Errorf("机器人发送者不在服务账号白名单中")
		}
		access, err = h.db.ResolveRBACAccess(strings.TrimSpace(authorization.ServiceUserID))
	default:
		return nil, fmt.Errorf("机器人鉴权模式无效")
	}
	if err != nil {
		return nil, err
	}
	if !access.User.Enabled {
		return nil, fmt.Errorf("绑定的平台账号已被禁用")
	}
	return access, nil
}

func robotPrincipal(access *database.RBACAccess) authctx.Principal {
	if access == nil {
		return authctx.Principal{}
	}
	return authctx.NewPrincipalWithScopes(access.User.ID, access.User.Username, access.Scope, access.Permissions, access.PermissionScopes)
}

func (h *RobotHandler) robotAccessDeniedMessage(platform string) string {
	if h.config.Robots.AuthorizationFor(platform).EffectiveMode() == config.RobotAuthModeServiceAccount {
		return "当前平台账号不在该机器人的服务账号白名单中，或服务账号不可用。"
	}
	return "当前平台账号尚未绑定 CyberStrikeAI 用户。请先在网页端生成绑定码，然后发送：绑定 XXXX-XXXX"
}

func (h *RobotHandler) loadSessionBinding(sk string) (convID, role, agentMode string) {
	if h.db == nil || strings.TrimSpace(sk) == "" {
		return "", "", ""
	}
	binding, err := h.db.GetRobotSessionBinding(sk)
	if err != nil {
		h.logger.Warn("读取机器人会话绑定失败", zap.String("session_key", sk), zap.Error(err))
		return "", "", ""
	}
	if binding == nil {
		return "", "", ""
	}
	return binding.ConversationID, binding.RoleName, binding.AgentMode
}

func (h *RobotHandler) persistSessionBinding(sk, convID, role, agentMode string) {
	if h.db == nil || strings.TrimSpace(sk) == "" || strings.TrimSpace(convID) == "" {
		return
	}
	if err := h.db.UpsertRobotSessionBinding(sk, convID, role, agentMode); err != nil {
		h.logger.Warn("写入机器人会话绑定失败", zap.String("session_key", sk), zap.Error(err))
	}
}

func (h *RobotHandler) deleteSessionBinding(sk string) {
	if h.db == nil || strings.TrimSpace(sk) == "" {
		return
	}
	if err := h.db.DeleteRobotSessionBinding(sk); err != nil {
		h.logger.Warn("删除机器人会话绑定失败", zap.String("session_key", sk), zap.Error(err))
	}
}

// getOrCreateConversation 获取或创建当前会话，title 用于新对话的标题（取用户首条消息前50字）
func (h *RobotHandler) getOrCreateConversation(platform, userID, title string, access *database.RBACAccess) (convID string, isNew bool) {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID = h.sessions[sk]
	h.mu.RUnlock()
	ownerID := access.User.ID
	readScope := robotPrincipal(access).ScopeFor("chat:read")
	if convID != "" && access.Permissions["chat:read"] && h.db.UserCanAccessResource(ownerID, readScope, "conversation", convID) {
		return convID, false
	}
	if persistedConvID, persistedRole, persistedMode := h.loadSessionBinding(sk); strings.TrimSpace(persistedConvID) != "" {
		if !access.Permissions["chat:read"] || !h.db.UserCanAccessResource(ownerID, readScope, "conversation", persistedConvID) {
			h.deleteSessionBinding(sk)
		} else {
			// 会话绑定持久化：服务重启后也可恢复当前对话和角色。
			h.mu.Lock()
			h.sessions[sk] = persistedConvID
			if strings.TrimSpace(persistedRole) != "" {
				h.sessionRoles[sk] = persistedRole
			}
			if strings.TrimSpace(persistedMode) != "" {
				h.sessionModes[sk] = config.NormalizeAgentMode(persistedMode)
			}
			h.mu.Unlock()
			return persistedConvID, false
		}
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = "新对话 " + time.Now().Format("01-02 15:04")
	} else {
		t = safeTruncateString(t, 50)
	}
	meta := database.ConversationCreateMeta{Source: "robot:" + platform}
	if !access.Permissions["chat:write"] {
		return "", false
	}
	meta.ProjectID = effectiveProjectID(h.config, "")
	if meta.ProjectID != "" && (!access.Permissions["project:read"] || !h.db.UserCanAccessResource(ownerID, robotPrincipal(access).ScopeFor("project:read"), "project", meta.ProjectID)) {
		meta.ProjectID = ""
	}
	conv, err := h.db.CreateConversation(t, meta)
	if err != nil {
		h.logger.Warn("创建机器人会话失败", zap.Error(err))
		return "", false
	}
	convID = conv.ID
	_ = h.db.SetResourceOwner("conversation", convID, ownerID)
	h.mu.Lock()
	role := h.sessionRoles[sk]
	agentMode := h.sessionModes[sk]
	h.sessions[sk] = convID
	h.mu.Unlock()
	if agentMode == "" {
		agentMode = config.NormalizeRobotAgentMode(h.config.MultiAgent)
	}
	h.persistSessionBinding(sk, convID, role, agentMode)
	return convID, true
}

// setConversation 切换当前会话
func (h *RobotHandler) setConversation(platform, userID, convID string) {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	role := h.sessionRoles[sk]
	agentMode := h.sessionModes[sk]
	h.sessions[sk] = convID
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, role, agentMode)
}

// getRole 获取当前用户使用的角色，未设置时返回"默认"
func (h *RobotHandler) getRole(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	role := h.sessionRoles[sk]
	h.mu.RUnlock()
	if strings.TrimSpace(role) != "" {
		return role
	}
	if _, persistedRole, _ := h.loadSessionBinding(sk); strings.TrimSpace(persistedRole) != "" {
		h.mu.Lock()
		h.sessionRoles[sk] = persistedRole
		h.mu.Unlock()
		return persistedRole
	}
	return "默认"
}

// setRole 设置当前用户使用的角色
func (h *RobotHandler) setRole(platform, userID, roleName string) {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	h.sessionRoles[sk] = roleName
	convID := h.sessions[sk]
	agentMode := h.sessionModes[sk]
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, roleName, agentMode)
}

func (h *RobotHandler) getAgentMode(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	mode := h.sessionModes[sk]
	h.mu.RUnlock()
	if mode != "" {
		return config.NormalizeAgentMode(mode)
	}
	if _, _, persistedMode := h.loadSessionBinding(sk); persistedMode != "" {
		mode = config.NormalizeAgentMode(persistedMode)
		h.mu.Lock()
		h.sessionModes[sk] = mode
		h.mu.Unlock()
		return mode
	}
	return config.NormalizeRobotAgentMode(h.config.MultiAgent)
}

func (h *RobotHandler) setAgentMode(platform, userID, mode string) {
	sk := h.sessionKey(platform, userID)
	mode = config.NormalizeAgentMode(mode)
	h.mu.Lock()
	h.sessionModes[sk] = mode
	convID := h.sessions[sk]
	role := h.sessionRoles[sk]
	h.mu.Unlock()
	h.persistSessionBinding(sk, convID, role, mode)
}

// clearConversation 清空当前会话（切换到新对话）
func (h *RobotHandler) clearConversation(platform, userID string, access *database.RBACAccess) (newConvID string) {
	title := "新对话 " + time.Now().Format("01-02 15:04")
	meta := database.ConversationCreateMeta{Source: "robot:" + platform + ":new"}
	meta.ProjectID = effectiveProjectID(h.config, "")
	ownerID := access.User.ID
	if meta.ProjectID != "" && (!access.Permissions["project:read"] || !h.db.UserCanAccessResource(ownerID, robotPrincipal(access).ScopeFor("project:read"), "project", meta.ProjectID)) {
		meta.ProjectID = ""
	}
	conv, err := h.db.CreateConversation(title, meta)
	if err != nil {
		h.logger.Warn("创建新对话失败", zap.Error(err))
		return ""
	}
	_ = h.db.SetResourceOwner("conversation", conv.ID, ownerID)
	h.setConversation(platform, userID, conv.ID)
	return conv.ID
}

// HandleMessage 处理用户输入，返回回复文本（供各平台 webhook 调用）
func (h *RobotHandler) HandleMessage(platform, userID, text string) (reply string) {
	platform = strings.TrimSpace(platform)
	userID = strings.TrimSpace(userID)
	text = strings.TrimSpace(text)
	if platform == "" {
		platform = "unknown"
	}
	if userID == "" {
		h.logger.Warn("机器人消息缺少用户标识，已拒绝处理", zap.String("platform", platform))
		return "无法识别发送者身份，请检查机器人事件订阅权限（需返回可用的用户 ID）。"
	}
	if text == "" {
		return "请输入内容或发送「帮助」/ help 查看命令。"
	}

	// 先尝试作为命令处理（支持中英文）
	if cmdReply, ok := h.handleRobotCommand(platform, userID, text); ok {
		return cmdReply
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return h.robotAccessDeniedMessage(platform)
	}
	if !access.Permissions["agent:execute"] || !access.Permissions["chat:read"] || !access.Permissions["chat:write"] {
		return "权限不足：机器人对话需要 agent:execute、chat:read 和 chat:write 权限。"
	}
	if h.audit != nil && h.config.Robots.AuthorizationFor(platform).EffectiveMode() == config.RobotAuthModeServiceAccount {
		hint := sha256.Sum256([]byte(userID))
		h.audit.RecordSystem(audit.Entry{
			Category: "robot", Action: "service_account_execute", Result: "success", Actor: access.User.Username,
			ResourceType: "robot_sender", ResourceID: platform + ":" + fmt.Sprintf("%x", hint[:4]),
			Message: "白名单平台发送者使用机器人服务账号执行 Agent",
		})
	}

	// 普通消息：走 Agent
	convID, _ := h.getOrCreateConversation(platform, userID, text, access)
	if convID == "" {
		return "无法创建或获取对话，请稍后再试。"
	}
	// 若对话标题为「新对话 xx:xx」格式（由「新对话」命令创建），将标题更新为首条消息内容，与 Web 端体验一致
	if conv, err := h.db.GetConversation(convID); err == nil && strings.HasPrefix(conv.Title, "新对话 ") {
		newTitle := safeTruncateString(text, 50)
		if newTitle != "" {
			_ = h.db.UpdateConversationTitle(convID, newTitle)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.robotMessageTimeout())
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	h.runningCancels[sk] = cancel
	h.cancelMu.Unlock()
	defer func() {
		cancel()
		h.cancelMu.Lock()
		delete(h.runningCancels, sk)
		h.cancelMu.Unlock()
	}()
	role := h.getRole(platform, userID)
	agentMode := h.getAgentMode(platform, userID)
	resp, newConvID, err := h.agentHandler.ProcessMessageForRobot(ctx, platform, robotPrincipal(access), convID, text, role, agentMode)
	if err != nil {
		h.logger.Warn("机器人 Agent 执行失败", zap.String("platform", platform), zap.String("userID", userID), zap.Error(err))
		if errors.Is(err, context.Canceled) {
			return "任务已取消。"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "任务执行超时，请稍后重试或精简本次请求范围。"
		}
		return "处理失败: " + err.Error()
	}
	if newConvID != convID {
		h.setConversation(platform, userID, newConvID)
	}
	return resp
}

func (h *RobotHandler) robotMessageTimeout() time.Duration {
	// 机器人整次消息处理超时（与单次工具超时 agent.tool_timeout_minutes 解耦）。
	return 10 * time.Hour
}

func (h *RobotHandler) cmdHelp(platform, userID string) string {
	access, _ := h.resolveRobotAccess(platform, userID)
	can := func(permission string) bool {
		return access != nil && access.Permissions[permission]
	}
	var b strings.Builder
	b.WriteString("【CyberStrikeAI 机器人命令】\n\n")
	b.WriteString("【通用 General】\n")
	b.WriteString("· 帮助 / help — 显示本帮助\n")
	b.WriteString("· 版本 / version — 显示当前版本号\n")
	b.WriteString("· 绑定 <绑定码> / bind <code> — 绑定网页端 RBAC 用户\n")
	b.WriteString("· 解绑 / unbind — 请求解除账号绑定（需确认）\n")
	b.WriteString("· 身份 / whoami — 显示平台发送者、鉴权模式及当前实际 RBAC 身份\n")
	if can("chat:read") || can("chat:write") || can("chat:delete") {
		b.WriteString("\n【对话 Conversation】\n")
		if can("chat:read") {
			b.WriteString("· 列表 / list — 列出所有对话标题与 ID\n· 切换 <ID> / switch <ID> — 指定对话继续\n· 状态 / status — 汇总当前选择\n· 任务 / task — 查看当前任务状态\n")
		}
		if can("chat:write") {
			b.WriteString("· 新对话 / new；清空 / clear — 开启新对话\n· 重命名 <名称> / rename <name> — 修改当前对话标题\n")
		}
		if can("chat:delete") {
			b.WriteString("· 删除 <ID> / delete <ID> — 删除指定对话（需确认）\n")
		}
	}
	if can("roles:read") {
		b.WriteString("\n【角色 Role】\n· 角色 / roles — 列出所有可用角色\n· 角色 <名> / role <name> — 切换当前角色\n")
	}
	if can("agent:execute") {
		b.WriteString("\n【模式 Mode】\n· 模式 / modes — 列出对话模式与当前选择\n· 模式 <名称> / mode <name> — 切换对话模式\n· 停止 / stop — 中断当前任务\n")
	}
	if can("vulnerability:read") {
		b.WriteString("\n【漏洞提醒 Vulnerability alerts】\n· 漏洞提醒 — 查看订阅状态\n· 漏洞提醒 开启 / vuln alerts on — 开启提醒\n· 漏洞提醒 仅严重|高危以上|中危以上 / vuln alerts critical|high|medium — 设置最低级别\n· 漏洞提醒 关闭 / vuln alerts off — 关闭提醒\n")
	}
	b.WriteString("\n【诊断 Diagnostics】\n")
	b.WriteString("· 权限 / permissions — 查看当前业务权限\n")
	if can("config:read") {
		b.WriteString("· 诊断 / doctor — 检查机器人关键配置状态\n")
	}
	b.WriteString("· 确认 / confirm；取消 / cancel — 处理高风险操作确认\n")
	if h.projectsEnabled() && (can("project:read") || can("project:write")) {
		b.WriteString("\n【项目 Project】\n")
		if can("project:read") {
			b.WriteString("· 项目 / projects — 列出所有项目\n")
		}
		if can("project:write") {
			b.WriteString("· 新建项目 <名称> / new project <name> — 创建并绑定当前对话\n· 绑定项目 <ID或名称> / bind project <ID|name> — 绑定已有项目\n· 解除项目 / unbind project — 解除项目绑定\n")
		}
	}
	b.WriteString("\n──────────────\n")
	b.WriteString("除以上命令外，直接输入内容将发送给 AI 进行渗透测试/安全分析。")
	return b.String()
}

func (h *RobotHandler) projectsEnabled() bool {
	return h.config != nil && h.config.Project.Enabled
}

func (h *RobotHandler) resolveProjectByIDOrName(access *database.RBACAccess, idOrName string) (*database.Project, string) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return nil, "请指定项目 ID 或名称，例如：绑定项目 xxx-xxx"
	}
	ownerID := access.User.ID
	scope := robotPrincipal(access).ScopeFor("project:read")
	if p, err := h.db.GetProject(idOrName); err == nil {
		if h.db.UserCanAccessResource(ownerID, scope, "project", p.ID) {
			return p, ""
		}
		return nil, "项目不存在或无权访问。"
	}
	list, err := h.db.ListProjectsForAccess("", "", 200, 0, ownerID, scope)
	if err != nil {
		return nil, "查询项目失败: " + err.Error()
	}
	var matches []*database.Project
	for _, p := range list {
		if p.Name == idOrName {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Sprintf("项目「%s」不存在。发送「项目」查看列表。", idOrName)
	case 1:
		return matches[0], ""
	default:
		var b strings.Builder
		b.WriteString(fmt.Sprintf("名称「%s」匹配到多个项目，请使用 ID 绑定：\n", idOrName))
		for _, p := range matches {
			b.WriteString(fmt.Sprintf("· %s\n  ID: %s\n", p.Name, p.ID))
		}
		return nil, strings.TrimSuffix(b.String(), "\n")
	}
}

func (h *RobotHandler) formatProjectLabel(projectID string) string {
	if strings.TrimSpace(projectID) == "" {
		return "未绑定"
	}
	if p, err := h.db.GetProject(projectID); err == nil {
		return fmt.Sprintf("「%s」 (%s)", p.Name, p.ID)
	}
	return projectID
}

func (h *RobotHandler) cmdProjects(platform, userID string) string {
	if !h.projectsEnabled() {
		return "项目功能未启用（config.project.enabled）。"
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	list, err := h.db.ListProjectsForAccess("", "", 50, 0, access.User.ID, robotPrincipal(access).ScopeFor("project:read"))
	if err != nil {
		return "获取项目列表失败: " + err.Error()
	}
	if len(list) == 0 {
		return "暂无项目。发送「新建项目 <名称>」创建并绑定到当前对话。"
	}
	var b strings.Builder
	b.WriteString("【项目列表】\n")
	for i, p := range list {
		if i >= 20 {
			b.WriteString("… 仅显示前 20 条\n")
			break
		}
		status := p.Status
		if status == "" {
			status = "active"
		}
		b.WriteString(fmt.Sprintf("· %s [%s]\n  ID: %s\n", p.Name, status, p.ID))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdBindProject(platform, userID, idOrName string) string {
	if !h.projectsEnabled() {
		return "项目功能未启用（config.project.enabled）。"
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	p, errMsg := h.resolveProjectByIDOrName(access, idOrName)
	if p == nil {
		return errMsg
	}
	convID, _ := h.getOrCreateConversation(platform, userID, "", access)
	if convID == "" {
		return "无法获取当前对话，请稍后再试。"
	}
	if err := h.db.SetConversationProjectID(convID, p.ID); err != nil {
		return "绑定失败: " + err.Error()
	}
	return fmt.Sprintf("已将当前对话绑定到项目：「%s」\nID: %s", p.Name, p.ID)
}

func (h *RobotHandler) cmdNewProject(platform, userID, name string) string {
	if !h.projectsEnabled() {
		return "项目功能未启用（config.project.enabled）。"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "请指定项目名称，例如：新建项目 某目标渗透"
	}
	access, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "当前平台账号尚未绑定。"
	}
	p := &database.Project{Name: name, Status: "active"}
	created, err := h.db.CreateProject(p)
	if err != nil {
		return "创建项目失败: " + err.Error()
	}
	_ = h.db.SetResourceOwner("project", created.ID, access.User.ID)
	convID, _ := h.getOrCreateConversation(platform, userID, name, access)
	if convID == "" {
		return fmt.Sprintf("项目已创建：「%s」\nID: %s\n（绑定当前对话失败，请手动发送「绑定项目 %s」）", created.Name, created.ID, created.ID)
	}
	if err := h.db.SetConversationProjectID(convID, created.ID); err != nil {
		return fmt.Sprintf("项目已创建：「%s」\nID: %s\n绑定失败: %s", created.Name, created.ID, err.Error())
	}
	return fmt.Sprintf("已创建项目并绑定当前对话：「%s」\nID: %s", created.Name, created.ID)
}

func (h *RobotHandler) cmdUnbindProject(platform, userID string) string {
	if !h.projectsEnabled() {
		return "项目功能未启用（config.project.enabled）。"
	}
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID := h.sessions[sk]
	h.mu.RUnlock()
	if convID == "" {
		if persistedConvID, _, _ := h.loadSessionBinding(sk); persistedConvID != "" {
			convID = persistedConvID
		}
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	if !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:write"), "conversation", convID) {
		return "当前对话不存在或无权访问。"
	}
	if convID == "" {
		return "当前没有进行中的对话，无需解除绑定。"
	}
	projectID, err := h.db.GetConversationProjectID(convID)
	if err != nil {
		return "获取对话项目失败: " + err.Error()
	}
	if strings.TrimSpace(projectID) == "" {
		return "当前对话未绑定项目。"
	}
	if err := h.db.SetConversationProjectID(convID, ""); err != nil {
		return "解除绑定失败: " + err.Error()
	}
	return "已解除当前对话的项目绑定。"
}

func (h *RobotHandler) cmdList(platform, userID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	convs, err := h.db.ListConversationsForAccess(50, 0, "", "", "", access.User.ID, robotPrincipal(access).ScopeFor("chat:read"))
	if err != nil {
		return "获取对话列表失败: " + err.Error()
	}
	if len(convs) == 0 {
		return "暂无对话。发送任意内容将自动创建新对话。"
	}
	var b strings.Builder
	b.WriteString("【对话列表】\n")
	for i, c := range convs {
		if i >= 20 {
			b.WriteString("… 仅显示前 20 条\n")
			break
		}
		b.WriteString(fmt.Sprintf("· %s\n  ID: %s\n", c.Title, c.ID))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitch(platform, userID, convID string) string {
	if convID == "" {
		return "请指定对话 ID，例如：切换 xxx-xxx-xxx"
	}
	access, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "当前平台账号尚未绑定。"
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil || !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:read"), "conversation", convID) {
		return "对话不存在或 ID 错误。"
	}
	h.setConversation(platform, userID, conv.ID)
	return fmt.Sprintf("已切换到对话：「%s」\nID: %s", conv.Title, conv.ID)
}

func (h *RobotHandler) cmdNew(platform, userID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	newID := h.clearConversation(platform, userID, access)
	if newID == "" {
		return "创建新对话失败，请重试。"
	}
	return "已开启新对话，可直接发送内容。"
}

func (h *RobotHandler) cmdClear(platform, userID string) string {
	return h.cmdNew(platform, userID)
}

func (h *RobotHandler) cmdStop(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	cancel, ok := h.runningCancels[sk]
	if ok {
		delete(h.runningCancels, sk)
		cancel()
	}
	h.cancelMu.Unlock()
	if !ok {
		return "当前没有正在执行的任务。"
	}
	return "已停止当前任务。"
}

func (h *RobotHandler) cmdStatus(platform, userID string) string {
	convID := h.currentConversationID(platform, userID)
	if convID == "" {
		return fmt.Sprintf("【当前状态】\n当前对话: 无\n当前角色: %s\n当前模式: %s\n当前项目: 无\n\n发送任意内容将创建新对话。", h.getRole(platform, userID), robotAgentModeLabel(h.getAgentMode(platform, userID)))
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	if !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:read"), "conversation", convID) {
		return "当前对话不存在或无权访问。"
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil {
		return "当前对话 ID: " + convID + "（获取标题失败）"
	}
	role := h.getRole(platform, userID)
	reply := fmt.Sprintf("【当前状态】\n当前对话: %s\n对话 ID: %s\n当前模式: %s\n当前角色: %s", conv.Title, conv.ID, robotAgentModeLabel(h.getAgentMode(platform, userID)), role)
	if h.projectsEnabled() {
		projectID, _ := h.db.GetConversationProjectID(conv.ID)
		reply += "\n当前项目: " + h.formatProjectLabel(projectID)
	} else {
		reply += "\n当前项目: 未启用"
	}
	return reply
}

func (h *RobotHandler) currentConversationID(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	convID := h.sessions[sk]
	h.mu.RUnlock()
	if convID != "" {
		return convID
	}
	persistedConvID, persistedRole, persistedMode := h.loadSessionBinding(sk)
	if persistedConvID == "" {
		return ""
	}
	h.mu.Lock()
	h.sessions[sk] = persistedConvID
	h.sessionRoles[sk] = persistedRole
	h.sessionModes[sk] = config.NormalizeAgentMode(persistedMode)
	h.mu.Unlock()
	return persistedConvID
}

func (h *RobotHandler) cmdTask(platform, userID string) string {
	convID := h.currentConversationID(platform, userID)
	if convID == "" {
		return "【任务状态】\n当前没有对话，也没有正在执行的任务。"
	}
	if h.agentHandler == nil || h.agentHandler.tasks == nil {
		return "任务状态服务不可用。"
	}
	task := h.agentHandler.tasks.GetTaskSnapshot(convID)
	if task == nil {
		return "【任务状态】\n状态: 空闲\n当前没有正在执行的任务。"
	}
	elapsed := time.Since(task.StartedAt).Round(time.Second)
	return fmt.Sprintf("【任务状态】\n状态: %s\n已运行: %s\n对话 ID: %s\n模式: %s\n可用操作: 停止 / stop", task.Status, elapsed, convID, robotAgentModeLabel(h.getAgentMode(platform, userID)))
}

func (h *RobotHandler) cmdRename(platform, userID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "请指定新标题，例如：重命名 外网资产排查"
	}
	title = safeTruncateString(title, 100)
	convID := h.currentConversationID(platform, userID)
	if convID == "" {
		return "当前没有对话，无法重命名。"
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil || !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:write"), "conversation", convID) {
		return "当前对话不存在或无权修改。"
	}
	if err := h.db.UpdateConversationTitle(convID, title); err != nil {
		return "重命名失败: " + err.Error()
	}
	h.recordRobotCommandAudit(access, platform, "conversation_rename", "conversation", convID, "机器人重命名当前对话")
	return fmt.Sprintf("已将当前对话重命名为：「%s」", title)
}

func (h *RobotHandler) cmdPermissions(platform, userID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return h.robotAccessDeniedMessage(platform)
	}
	allowed := func(permission string) string {
		if access.Permissions[permission] {
			return "允许"
		}
		return "不允许"
	}
	return fmt.Sprintf("【当前权限】\n执行 Agent: %s\n读取对话: %s\n编辑对话: %s\n删除对话: %s\n读取角色: %s\n读取项目: %s\n编辑项目: %s\n资源范围: %s", allowed("agent:execute"), allowed("chat:read"), allowed("chat:write"), allowed("chat:delete"), allowed("roles:read"), allowed("project:read"), allowed("project:write"), access.Scope)
}

func (h *RobotHandler) cmdDoctor() string {
	configured := func(ok bool) string {
		if ok {
			return "正常"
		}
		return "未配置"
	}
	enabled := func(ok bool) string {
		if ok {
			return "已启用"
		}
		return "已关闭"
	}
	enabledInternalTools := 0
	for _, tool := range h.config.Security.Tools {
		if tool.Enabled {
			enabledInternalTools++
		}
	}
	enabledExternal := 0
	for _, server := range h.config.ExternalMCP.Servers {
		if server.ExternalMCPEnable && !server.Disabled {
			enabledExternal++
		}
	}
	return fmt.Sprintf("【配置诊断】\n主模型: %s\nEino 多代理: %s\n内置 MCP 工具: %d/%d 个已启用\nHTTP MCP 服务: %s\n外部 MCP: %d 个已启用\n知识库: %s\n项目功能: %s\n说明: 内置工具不依赖 HTTP MCP 服务；此命令只检查配置，不主动探测外部服务。", configured(strings.TrimSpace(h.config.OpenAI.Model) != "" && strings.TrimSpace(h.config.OpenAI.BaseURL) != ""), enabled(h.config.MultiAgent.Enabled), enabledInternalTools, len(h.config.Security.Tools), enabled(h.config.MCP.Enabled), enabledExternal, enabled(h.config.Knowledge.Enabled), enabled(h.config.Project.Enabled))
}

func (h *RobotHandler) recordRobotCommandAudit(access *database.RBACAccess, platform, action, resourceType, resourceID, message string) {
	if h.audit == nil || access == nil {
		return
	}
	h.audit.RecordSystem(audit.Entry{Category: "robot", Action: action, Result: "success", Actor: access.User.Username, ResourceType: resourceType, ResourceID: resourceID, Message: message + "（" + platform + "）"})
}

func (h *RobotHandler) cmdRoles() string {
	if h.config.Roles == nil || len(h.config.Roles) == 0 {
		return "暂无可用角色。"
	}
	names := make([]string, 0, len(h.config.Roles))
	for name, role := range h.config.Roles {
		if role.Enabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "暂无可用角色。"
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "默认" {
			return true
		}
		if names[j] == "默认" {
			return false
		}
		return names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString("【角色列表】\n")
	for _, name := range names {
		role := h.config.Roles[name]
		desc := role.Description
		if desc == "" {
			desc = "无描述"
		}
		b.WriteString(fmt.Sprintf("· %s — %s\n", name, desc))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitchRole(platform, userID, roleName string) string {
	if roleName == "" {
		return "请指定角色名称，例如：角色 渗透测试"
	}
	if h.config.Roles == nil {
		return "暂无可用角色。"
	}
	role, exists := h.config.Roles[roleName]
	if !exists {
		return fmt.Sprintf("角色「%s」不存在。发送「角色」查看可用角色。", roleName)
	}
	if !role.Enabled {
		return fmt.Sprintf("角色「%s」已禁用。", roleName)
	}
	h.setRole(platform, userID, roleName)
	return fmt.Sprintf("已切换到角色：「%s」\n%s", roleName, role.Description)
}

func robotAgentModeLabel(mode string) string {
	switch config.NormalizeAgentMode(mode) {
	case "deep":
		return "Deep"
	case "plan_execute":
		return "Plan-Execute"
	case "supervisor":
		return "Supervisor"
	default:
		return "Eino 单代理"
	}
}

func parseRobotAgentMode(input string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "eino_single", "eino-single", "single", "单代理", "eino单代理", "eino 单代理":
		return "eino_single", true
	case "deep":
		return "deep", true
	case "plan_execute", "plan-execute", "planexecute", "pe":
		return "plan_execute", true
	case "supervisor", "super", "sv":
		return "supervisor", true
	default:
		return "", false
	}
}

func (h *RobotHandler) cmdModes(platform, userID string) string {
	current := h.getAgentMode(platform, userID)
	multiStatus := "可用"
	if h.config == nil || !h.config.MultiAgent.Enabled {
		multiStatus = "不可用（需在系统设置中启用 Eino 多代理）"
	}
	return fmt.Sprintf("【对话模式】\n· Eino 单代理 — 可用\n· Deep — %s\n· Plan-Execute — %s\n· Supervisor — %s\n\n当前模式: %s\n切换示例：模式 deep", multiStatus, multiStatus, multiStatus, robotAgentModeLabel(current))
}

func (h *RobotHandler) cmdSwitchMode(platform, userID, input string) string {
	mode, ok := parseRobotAgentMode(input)
	if !ok {
		return fmt.Sprintf("不支持的对话模式「%s」。发送「模式」查看可用模式。", strings.TrimSpace(input))
	}
	if mode != "eino_single" && (h.config == nil || !h.config.MultiAgent.Enabled) {
		return fmt.Sprintf("无法切换到 %s：请先在系统设置中启用 Eino 多代理。", robotAgentModeLabel(mode))
	}
	h.setAgentMode(platform, userID, mode)
	return fmt.Sprintf("已切换对话模式：%s\n后续消息和新对话将使用该模式。", robotAgentModeLabel(mode))
}

func (h *RobotHandler) cmdDelete(platform, userID, convID string) string {
	if convID == "" {
		return "请指定对话 ID，例如：删除 xxx-xxx-xxx"
	}
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		return "当前平台账号尚未绑定。"
	}
	if !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:delete"), "conversation", convID) {
		return "对话不存在或无权访问。"
	}
	h.setPendingConfirmation(platform, userID, "delete_conversation", convID)
	return fmt.Sprintf("⚠️ 即将删除对话 ID: %s\n此操作不可撤销。请在 2 分钟内发送「确认」继续，或发送「取消」。", convID)
}

func (h *RobotHandler) executeDelete(platform, userID, convID string) string {
	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil || !h.db.UserCanAccessResource(access.User.ID, robotPrincipal(access).ScopeFor("chat:delete"), "conversation", convID) {
		return "对话不存在或无权删除。"
	}
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	currentConvID := h.sessions[sk]
	h.mu.RUnlock()
	if convID == currentConvID {
		// 删除当前对话时，先清空会话绑定
		h.mu.Lock()
		delete(h.sessions, sk)
		delete(h.sessionRoles, sk)
		delete(h.sessionModes, sk)
		h.mu.Unlock()
		h.deleteSessionBinding(sk)
	}
	if h.agentHandler != nil {
		h.agentHandler.CancelRunningTaskForConversation(convID)
	}
	if err := h.db.DeleteConversation(convID); err != nil {
		return "删除失败: " + err.Error()
	}
	h.recordRobotCommandAudit(access, platform, "conversation_delete", "conversation", convID, "机器人删除对话")
	return fmt.Sprintf("已删除对话 ID: %s", convID)
}

func (h *RobotHandler) cmdVersion() string {
	v := h.config.Version
	if v == "" {
		v = "未知"
	}
	return "CyberStrikeAI " + v
}

func (h *RobotHandler) cmdIdentity(platform, userID string) string {
	authorization := h.config.Robots.AuthorizationFor(platform)
	mode := authorization.EffectiveMode()
	modeLabel := "逐用户绑定（user_binding）"
	if mode == config.RobotAuthModeServiceAccount {
		modeLabel = "专用服务账号（service_account）"
	}
	var b strings.Builder
	b.WriteString("【机器人身份】\n")
	b.WriteString("平台：" + platform + "\n")
	b.WriteString("发送者 ID：" + userID + "\n")
	b.WriteString("鉴权模式：" + modeLabel + "\n")

	access, err := h.resolveRobotAccess(platform, userID)
	if err != nil {
		if mode == config.RobotAuthModeServiceAccount {
			b.WriteString("鉴权状态：拒绝（发送者不在白名单中，或服务账号不可用）")
		} else {
			b.WriteString("鉴权状态：未绑定\n")
			b.WriteString("操作提示：请在 Web 端生成绑定码，然后发送“绑定 XXXX-XXXX”")
		}
		return b.String()
	}

	name := strings.TrimSpace(access.User.DisplayName)
	if name == "" {
		name = access.User.Username
	}
	roleNames := make([]string, 0, len(access.Roles))
	for _, role := range access.Roles {
		roleNames = append(roleNames, role.Name)
	}
	if len(roleNames) == 0 {
		roleNames = append(roleNames, "未分配角色")
	}
	b.WriteString("鉴权状态：已授权\n")
	b.WriteString("实际身份：" + name + " (" + access.User.Username + ")\n")
	b.WriteString("RBAC User ID：" + access.User.ID + "\n")
	b.WriteString("平台角色：" + strings.Join(roleNames, "、") + "\n")
	b.WriteString("资源范围：" + access.Scope + "\n")
	b.WriteString(fmt.Sprintf("有效权限：%d 项", len(access.Permissions)))
	return b.String()
}

func robotCommandPermission(text string) (string, bool) {
	switch {
	case text == robotCmdHelp || text == "help" || text == "？" || text == "?", text == robotCmdVersion || text == "version", text == robotCmdIdentity || text == "whoami":
		return "", true
	case text == robotCmdList || text == robotCmdListAlt || text == "list",
		strings.HasPrefix(text, robotCmdSwitch+" "), strings.HasPrefix(text, robotCmdContinue+" "),
		strings.HasPrefix(text, "switch "), strings.HasPrefix(text, "continue "),
		text == robotCmdStatus || text == "status", text == robotCmdTask || text == "task":
		return "chat:read", true
	case text == robotCmdNew || text == "new", text == robotCmdClear || text == "clear",
		strings.HasPrefix(text, robotCmdRename+" "), strings.HasPrefix(text, "rename "):
		return "chat:write", true
	case strings.HasPrefix(text, robotCmdDelete+" "), strings.HasPrefix(text, "delete "):
		return "chat:delete", true
	case text == robotCmdStop || text == "stop":
		return "agent:execute", true
	case text == robotCmdRoles || text == robotCmdRolesList || text == "roles",
		strings.HasPrefix(text, robotCmdRoles+" "), strings.HasPrefix(text, robotCmdSwitchRole+" "), strings.HasPrefix(text, "role "):
		return "roles:read", true
	case text == robotCmdModes || text == robotCmdModesList || text == "modes",
		strings.HasPrefix(text, robotCmdModes+" "), strings.HasPrefix(text, robotCmdSwitchMode+" "), strings.HasPrefix(text, "mode "):
		return "agent:execute", true
	case text == robotCmdPermissions || text == "permissions":
		return "", true
	case text == robotCmdConfirm || text == "confirm", text == robotCmdCancel || text == "cancel":
		return "", true
	case text == robotCmdDoctor || text == "doctor":
		return "config:read", true
	case text == robotCmdProjects || text == robotCmdProjectsList || text == "projects":
		return "project:read", true
	case text == robotCmdVulnAlerts || strings.HasPrefix(text, robotCmdVulnAlerts+" "),
		text == "vuln alerts" || strings.HasPrefix(text, "vuln alerts "):
		return "vulnerability:read", true
	case text == robotCmdUnbindProject || text == "unbind project",
		strings.HasPrefix(text, robotCmdNewProject+" "), strings.HasPrefix(text, "new project "),
		strings.HasPrefix(text, robotCmdBindProject+" "), strings.HasPrefix(text, "bind project "):
		return "project:write", true
	default:
		return "", false
	}
}

func (h *RobotHandler) cmdBindUser(platform, userID, code string) string {
	if h.config.Robots.AuthorizationFor(platform).EffectiveMode() != config.RobotAuthModeUserBinding {
		return "该机器人使用受控服务账号模式，不接受用户绑定。"
	}
	code = normalizeRobotBindingCode(code)
	if code == "" {
		return "请提供绑定码，例如：绑定 ABCD-1234"
	}
	user, err := h.db.ConsumeRobotBindingCode(platform, userID, hashRobotBindingCode(code))
	if err != nil {
		return "绑定失败：绑定码无效、已使用或已过期。请在网页端重新生成。"
	}
	// Never carry an old synthetic-owner conversation into the RBAC identity.
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	delete(h.sessions, sk)
	delete(h.sessionRoles, sk)
	delete(h.sessionModes, sk)
	h.mu.Unlock()
	h.deleteSessionBinding(sk)
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Username
	}
	if h.audit != nil {
		hint := sha256.Sum256([]byte(userID))
		h.audit.RecordSystem(audit.Entry{
			Category: "auth", Action: "robot_bind", Result: "success", Actor: user.Username,
			ResourceType: "robot_binding", ResourceID: platform + ":" + fmt.Sprintf("%x", hint[:4]), Message: "机器人平台账号绑定成功",
		})
	}
	return fmt.Sprintf("绑定成功，当前身份：%s。后续操作将实时使用该用户的 RBAC 权限。", name)
}

func (h *RobotHandler) cmdUnbindUser(platform, userID string) string {
	if h.config.Robots.AuthorizationFor(platform).EffectiveMode() != config.RobotAuthModeUserBinding {
		return "该机器人使用受控服务账号模式，无需用户解绑。"
	}
	_, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "当前平台账号尚未绑定。"
	}
	h.setPendingConfirmation(platform, userID, "unbind_user", "")
	return "⚠️ 即将解除当前平台账号绑定。请在 2 分钟内发送「确认」继续，或发送「取消」。"
}

func (h *RobotHandler) executeUnbindUser(platform, userID string) string {
	access, accessErr := h.resolveRobotAccess(platform, userID)
	if accessErr != nil {
		return "当前平台账号尚未绑定。"
	}
	if err := h.db.DeleteRobotIdentityBinding(platform, userID); err != nil {
		return "解绑失败，请稍后重试。"
	}
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	delete(h.sessions, sk)
	delete(h.sessionRoles, sk)
	delete(h.sessionModes, sk)
	h.mu.Unlock()
	h.deleteSessionBinding(sk)
	if h.audit != nil {
		hint := sha256.Sum256([]byte(userID))
		h.audit.RecordSystem(audit.Entry{
			Category: "auth", Action: "robot_unbind", Result: "success", Actor: access.User.Username,
			ResourceType: "robot_binding", ResourceID: platform + ":" + fmt.Sprintf("%x", hint[:4]), Message: "机器人平台账号解绑成功",
		})
	}
	return "已解除当前平台账号与 CyberStrikeAI 用户的绑定。"
}

func (h *RobotHandler) setPendingConfirmation(platform, userID, action, target string) {
	sk := h.sessionKey(platform, userID)
	now := time.Now()
	h.mu.Lock()
	for key, pending := range h.pendingConfirmations {
		if now.After(pending.ExpiresAt) {
			delete(h.pendingConfirmations, key)
		}
	}
	h.pendingConfirmations[sk] = robotPendingConfirmation{Action: action, Target: target, ExpiresAt: now.Add(2 * time.Minute)}
	h.mu.Unlock()
}

func (h *RobotHandler) cmdConfirm(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	pending, ok := h.pendingConfirmations[sk]
	delete(h.pendingConfirmations, sk)
	h.mu.Unlock()
	if !ok || time.Now().After(pending.ExpiresAt) {
		return "当前没有待确认操作，或确认已超时。"
	}
	switch pending.Action {
	case "delete_conversation":
		return h.executeDelete(platform, userID, pending.Target)
	case "unbind_user":
		return h.executeUnbindUser(platform, userID)
	default:
		return "待确认操作无效，已取消。"
	}
}

func (h *RobotHandler) cmdCancelConfirmation(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.mu.Lock()
	_, ok := h.pendingConfirmations[sk]
	delete(h.pendingConfirmations, sk)
	h.mu.Unlock()
	if !ok {
		return "当前没有待确认操作。"
	}
	return "已取消待确认操作。"
}

// handleRobotCommand 处理机器人内置命令；若匹配到命令返回 (回复内容, true)，否则返回 ("", false)
func (h *RobotHandler) handleRobotCommand(platform, userID, text string) (string, bool) {
	if (strings.HasPrefix(text, robotCmdBindUser+" ") || strings.HasPrefix(text, "bind ")) && !strings.HasPrefix(text, "bind project ") {
		parts := strings.SplitN(text, " ", 2)
		return h.cmdBindUser(platform, userID, strings.TrimSpace(parts[1])), true
	}
	if text == robotCmdUnbindUser || text == "unbind" {
		return h.cmdUnbindUser(platform, userID), true
	}
	if permission, recognized := robotCommandPermission(text); recognized && permission != "" {
		access, err := h.resolveRobotAccess(platform, userID)
		if err != nil {
			return h.robotAccessDeniedMessage(platform), true
		}
		if !access.Permissions[permission] {
			return fmt.Sprintf("权限不足：缺少 %s 权限。", permission), true
		}
	}
	switch {
	case text == robotCmdVulnAlerts || text == "vuln alerts":
		return h.cmdVulnerabilityAlerts(platform, userID, ""), true
	case strings.HasPrefix(text, robotCmdVulnAlerts+" "):
		return h.cmdVulnerabilityAlerts(platform, userID, strings.TrimSpace(text[len(robotCmdVulnAlerts)+1:])), true
	case strings.HasPrefix(text, "vuln alerts "):
		return h.cmdVulnerabilityAlerts(platform, userID, strings.TrimSpace(text[len("vuln alerts "):])), true
	case text == robotCmdHelp || text == "help" || text == "？" || text == "?":
		return h.cmdHelp(platform, userID), true
	case text == robotCmdIdentity || text == "whoami":
		return h.cmdIdentity(platform, userID), true
	case text == robotCmdConfirm || text == "confirm":
		return h.cmdConfirm(platform, userID), true
	case text == robotCmdCancel || text == "cancel":
		return h.cmdCancelConfirmation(platform, userID), true
	case text == robotCmdList || text == robotCmdListAlt || text == "list":
		return h.cmdList(platform, userID), true
	case strings.HasPrefix(text, robotCmdSwitch+" ") || strings.HasPrefix(text, robotCmdContinue+" ") || strings.HasPrefix(text, "switch ") || strings.HasPrefix(text, "continue "):
		var id string
		switch {
		case strings.HasPrefix(text, robotCmdSwitch+" "):
			id = strings.TrimSpace(text[len(robotCmdSwitch)+1:])
		case strings.HasPrefix(text, robotCmdContinue+" "):
			id = strings.TrimSpace(text[len(robotCmdContinue)+1:])
		case strings.HasPrefix(text, "switch "):
			id = strings.TrimSpace(text[7:])
		default:
			id = strings.TrimSpace(text[9:])
		}
		return h.cmdSwitch(platform, userID, id), true
	case text == robotCmdNew || text == "new":
		return h.cmdNew(platform, userID), true
	case text == robotCmdClear || text == "clear":
		return h.cmdClear(platform, userID), true
	case text == robotCmdStatus || text == "status":
		return h.cmdStatus(platform, userID), true
	case text == robotCmdTask || text == "task":
		return h.cmdTask(platform, userID), true
	case strings.HasPrefix(text, robotCmdRename+" ") || strings.HasPrefix(text, "rename "):
		var title string
		if strings.HasPrefix(text, robotCmdRename+" ") {
			title = strings.TrimSpace(text[len(robotCmdRename)+1:])
		} else {
			title = strings.TrimSpace(text[len("rename "):])
		}
		return h.cmdRename(platform, userID, title), true
	case text == robotCmdStop || text == "stop":
		return h.cmdStop(platform, userID), true
	case text == robotCmdRoles || text == robotCmdRolesList || text == "roles":
		return h.cmdRoles(), true
	case strings.HasPrefix(text, robotCmdRoles+" ") || strings.HasPrefix(text, robotCmdSwitchRole+" ") || strings.HasPrefix(text, "role "):
		var roleName string
		switch {
		case strings.HasPrefix(text, robotCmdRoles+" "):
			roleName = strings.TrimSpace(text[len(robotCmdRoles)+1:])
		case strings.HasPrefix(text, robotCmdSwitchRole+" "):
			roleName = strings.TrimSpace(text[len(robotCmdSwitchRole)+1:])
		default:
			roleName = strings.TrimSpace(text[5:])
		}
		return h.cmdSwitchRole(platform, userID, roleName), true
	case text == robotCmdModes || text == robotCmdModesList || text == "modes":
		return h.cmdModes(platform, userID), true
	case strings.HasPrefix(text, robotCmdModes+" ") || strings.HasPrefix(text, robotCmdSwitchMode+" ") || strings.HasPrefix(text, "mode "):
		var mode string
		switch {
		case strings.HasPrefix(text, robotCmdModes+" "):
			mode = strings.TrimSpace(text[len(robotCmdModes)+1:])
		case strings.HasPrefix(text, robotCmdSwitchMode+" "):
			mode = strings.TrimSpace(text[len(robotCmdSwitchMode)+1:])
		default:
			mode = strings.TrimSpace(text[5:])
		}
		return h.cmdSwitchMode(platform, userID, mode), true
	case text == robotCmdPermissions || text == "permissions":
		return h.cmdPermissions(platform, userID), true
	case text == robotCmdDoctor || text == "doctor":
		return h.cmdDoctor(), true
	case strings.HasPrefix(text, robotCmdDelete+" ") || strings.HasPrefix(text, "delete "):
		var convID string
		if strings.HasPrefix(text, robotCmdDelete+" ") {
			convID = strings.TrimSpace(text[len(robotCmdDelete)+1:])
		} else {
			convID = strings.TrimSpace(text[7:])
		}
		return h.cmdDelete(platform, userID, convID), true
	case text == robotCmdVersion || text == "version":
		return h.cmdVersion(), true
	case text == robotCmdProjects || text == robotCmdProjectsList || text == "projects":
		return h.cmdProjects(platform, userID), true
	case text == robotCmdUnbindProject || text == "unbind project":
		return h.cmdUnbindProject(platform, userID), true
	case strings.HasPrefix(text, robotCmdNewProject+" ") || strings.HasPrefix(text, "new project "):
		var name string
		if strings.HasPrefix(text, robotCmdNewProject+" ") {
			name = strings.TrimSpace(text[len(robotCmdNewProject)+1:])
		} else {
			name = strings.TrimSpace(text[len("new project "):])
		}
		return h.cmdNewProject(platform, userID, name), true
	case strings.HasPrefix(text, robotCmdBindProject+" ") || strings.HasPrefix(text, "bind project "):
		var idOrName string
		if strings.HasPrefix(text, robotCmdBindProject+" ") {
			idOrName = strings.TrimSpace(text[len(robotCmdBindProject)+1:])
		} else {
			idOrName = strings.TrimSpace(text[len("bind project "):])
		}
		return h.cmdBindProject(platform, userID, idOrName), true
	default:
		return "", false
	}
}

// —————— 企业微信 ——————

// wecomXML 企业微信回调 XML（明文模式下的简化结构；加密模式需先解密再解析）
type wecomXML struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	MsgID        string `xml:"MsgId"`
	AgentID      int64  `xml:"AgentID"`
	Encrypt      string `xml:"Encrypt"` // 加密模式下消息在此
}

// wecomReplyXML 被动回复 XML（仅用于兼容，当前使用手动构造 XML）
type wecomReplyXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
}

// wecomRequireToken 企业微信回调必须配置 Token；未配置时拒绝请求，防止未授权触发 Agent。
func (h *RobotHandler) wecomRequireToken(c *gin.Context) (string, bool) {
	token := strings.TrimSpace(h.config.Robots.Wecom.Token)
	if token == "" {
		h.logger.Warn("企业微信已启用但未配置 token，已拒绝回调（请在配置中设置 robots.wecom.token）")
		c.String(http.StatusForbidden, "")
		return "", false
	}
	return token, true
}

// HandleWecomGET 企业微信 URL 校验（GET）
func (h *RobotHandler) HandleWecomGET(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		c.String(http.StatusNotFound, "")
		return
	}
	token, ok := h.wecomRequireToken(c)
	if !ok {
		return
	}
	// Gin 的 Query() 会自动 URL 解码，拿到的就是正确的 base64 字符串
	echostr := c.Query("echostr")
	msgSignature := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")

	// 验证签名：将 token、timestamp、nonce、echostr 四个参数排序后拼接计算 SHA1
	signature := h.signWecomRequest(token, timestamp, nonce, echostr)
	if signature != msgSignature {
		h.logger.Warn("企业微信 URL 验证签名失败", zap.String("expected", msgSignature), zap.String("got", signature))
		c.String(http.StatusBadRequest, "invalid signature")
		return
	}

	if echostr == "" {
		c.String(http.StatusBadRequest, "missing echostr")
		return
	}

	// 如果配置了 EncodingAESKey，说明是加密模式，需要解密 echostr
	if h.config.Robots.Wecom.EncodingAESKey != "" {
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, echostr)
		if err != nil {
			h.logger.Warn("企业微信 echostr 解密失败", zap.Error(err))
			c.String(http.StatusBadRequest, "decrypt failed")
			return
		}
		c.String(http.StatusOK, string(decrypted))
		return
	}

	// 明文模式直接返回 echostr
	c.String(http.StatusOK, echostr)
}

// signWecomRequest 生成企业微信请求签名
// 企业微信签名算法：将 token、timestamp、nonce、echostr 四个值排序后拼接成字符串，再计算 SHA1
func (h *RobotHandler) signWecomRequest(token, timestamp, nonce, echostr string) string {
	strs := []string{token, timestamp, nonce, echostr}
	sort.Strings(strs)
	s := strings.Join(strs, "")
	hash := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", hash)
}

// wecomDecrypt 企业微信消息解密（AES-256-CBC，PKCS7，明文格式：16字节随机+4字节长度+消息+corpID）
func wecomDecrypt(encodingAESKey, encryptedB64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encoding_aes_key 解码后应为 32 字节")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := key[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("密文长度不是块大小的倍数")
	}
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	// 去除 PKCS7 填充
	n := int(plain[len(plain)-1])
	if n < 1 || n > 32 {
		return nil, fmt.Errorf("无效的 PKCS7 填充")
	}
	plain = plain[:len(plain)-n]
	// 企业微信格式：16 字节随机 + 4 字节长度(大端) + 消息 + corpID
	if len(plain) < 20 {
		return nil, fmt.Errorf("明文过短")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if int(20+msgLen) > len(plain) {
		return nil, fmt.Errorf("消息长度越界")
	}
	return plain[20 : 20+msgLen], nil
}

// wecomEncrypt 企业微信消息加密（AES-256-CBC，PKCS7，明文格式：16字节随机+4字节长度+消息+corpID）
func wecomEncrypt(encodingAESKey, message, corpID string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return "", err
	}
	if len(key) != 32 {
		return "", fmt.Errorf("encoding_aes_key 解码后应为 32 字节")
	}
	// 构造明文：16 字节随机 + 4 字节长度 (大端) + 消息 + corpID
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		// 降级方案：使用时间戳生成随机数
		for i := range random {
			random[i] = byte(time.Now().UnixNano() % 256)
		}
	}
	msgLen := len(message)
	msgBytes := []byte(message)
	corpBytes := []byte(corpID)
	plain := make([]byte, 16+4+msgLen+len(corpBytes))
	copy(plain[:16], random)
	binary.BigEndian.PutUint32(plain[16:20], uint32(msgLen))
	copy(plain[20:20+msgLen], msgBytes)
	copy(plain[20+msgLen:], corpBytes)
	// PKCS7 填充
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	pad := bytes.Repeat([]byte{byte(padding)}, padding)
	plain = append(plain, pad...)
	// AES-256-CBC 加密
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:16]
	ciphertext := make([]byte, len(plain))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plain)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// HandleWecomPOST 企业微信消息回调（POST），支持明文与加密模式
func (h *RobotHandler) HandleWecomPOST(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		h.logger.Debug("企业微信机器人未启用，跳过请求")
		c.String(http.StatusOK, "")
		return
	}
	// 从 URL 获取签名参数（加密模式回复时需要用到）
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	msgSignature := c.Query("msg_signature")

	// 先读取请求体，后续解析/签名验证都会用到
	bodyRaw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Warn("企业微信 POST 读取请求体失败", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	h.logger.Debug("企业微信 POST 收到请求", zap.String("body", string(bodyRaw)))

	// 验证请求签名防止伪造。企业微信签名算法同 URL 验证，使用 token、timestamp、nonce、 Encrypt 四个字段。
	// 启用企业微信时必须配置 token 并校验签名，避免未授权请求触发 Agent。
	token, ok := h.wecomRequireToken(c)
	if !ok {
		return
	}
	if msgSignature == "" {
		h.logger.Warn("企业微信 POST 缺少签名，已拒绝（需确保回调携带 msg_signature）")
		c.String(http.StatusOK, "")
		return
	}
	var tmp wecomXML
	if err := xml.Unmarshal(bodyRaw, &tmp); err != nil {
		h.logger.Warn("企业微信 POST 签名验证前解析 XML 失败", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	expected := h.signWecomRequest(token, timestamp, nonce, tmp.Encrypt)
	if expected != msgSignature {
		h.logger.Warn("企业微信 POST 签名验证失败", zap.String("expected", expected), zap.String("got", msgSignature))
		c.String(http.StatusOK, "")
		return
	}
	if !h.acceptFreshWecomRequest(timestamp, nonce, msgSignature) {
		h.logger.Warn("企业微信 POST 时间戳过期或请求重放，已拒绝")
		c.String(http.StatusOK, "")
		return
	}

	var body wecomXML
	if err := xml.Unmarshal(bodyRaw, &body); err != nil {
		h.logger.Warn("企业微信 POST 解析 XML 失败", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	h.logger.Debug("企业微信 XML 解析成功", zap.String("ToUserName", body.ToUserName), zap.String("FromUserName", body.FromUserName), zap.String("MsgType", body.MsgType), zap.String("Content", body.Content), zap.String("Encrypt", body.Encrypt))

	// 保存企业 ID（用于明文模式回复）
	enterpriseID := body.ToUserName

	// 配置了 EncodingAESKey 时必须走加密消息，拒绝明文 XML 绕过
	if strings.TrimSpace(h.config.Robots.Wecom.EncodingAESKey) != "" && strings.TrimSpace(body.Encrypt) == "" {
		h.logger.Warn("企业微信已配置加密模式但收到明文消息，已拒绝")
		c.String(http.StatusOK, "")
		return
	}

	// 加密模式：先解密再解析内层 XML
	if body.Encrypt != "" && h.config.Robots.Wecom.EncodingAESKey != "" {
		h.logger.Debug("企业微信进入加密模式解密流程")
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, body.Encrypt)
		if err != nil {
			h.logger.Warn("企业微信消息解密失败", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		h.logger.Debug("企业微信解密成功", zap.String("decrypted", string(decrypted)))
		if err := xml.Unmarshal(decrypted, &body); err != nil {
			h.logger.Warn("企业微信解密后 XML 解析失败", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		h.logger.Debug("企业微信内层 XML 解析成功", zap.String("FromUserName", body.FromUserName), zap.String("Content", body.Content))
	}

	tenantKey := strings.TrimSpace(enterpriseID)
	if tenantKey == "" {
		tenantKey = strings.TrimSpace(h.config.Robots.Wecom.CorpID)
	}
	if tenantKey == "" {
		tenantKey = "default"
	}
	rawUserID := strings.TrimSpace(body.FromUserName)
	replyUserID := rawUserID
	userID := ""
	if rawUserID != "" {
		userID = "t:" + tenantKey + "|u:" + rawUserID
	}
	text := strings.TrimSpace(body.Content)
	if userID == "" {
		h.logger.Warn("企业微信消息缺少可用用户标识，已忽略")
		c.String(http.StatusOK, "success")
		return
	}

	// 限制回复内容长度（企业微信限制 2048 字节）
	maxReplyLen := 2000
	limitReply := func(s string) string {
		if len(s) > maxReplyLen {
			return s[:maxReplyLen] + "\n\n（内容过长，已截断）"
		}
		return s
	}

	if body.MsgType != "text" {
		h.logger.Debug("企业微信收到非文本消息", zap.String("MsgType", body.MsgType))
		h.sendWecomReply(c, replyUserID, enterpriseID, limitReply("暂仅支持文本消息，请发送文字。"), timestamp, nonce)
		return
	}

	// 文本消息：先判断是否为内置命令（如 帮助/列表/新对话 等），这类命令处理很快，可以直接走被动回复，避免依赖主动发送 API。
	if cmdReply, ok := h.handleRobotCommand("wecom", userID, text); ok {
		h.logger.Debug("企业微信收到命令消息，走被动回复", zap.String("userID", userID), zap.String("text", text))
		h.sendWecomReply(c, replyUserID, enterpriseID, limitReply(cmdReply), timestamp, nonce)
		return
	}

	h.logger.Debug("企业微信开始处理消息（异步 AI）", zap.String("userID", userID), zap.String("text", text))

	// 企业微信被动回复有 5 秒超时限制，而 AI 调用通常超过该时长。
	// 这里采用推荐做法：立即返回 success（或空串），然后通过主动发送接口推送完整回复。
	c.String(http.StatusOK, "success")

	// 异步处理消息并通过企业微信主动消息接口发送结果
	go func() {
		reply := h.HandleMessage("wecom", userID, text)
		reply = limitReply(reply)
		h.logger.Debug("企业微信消息处理完成", zap.String("userID", userID), zap.String("reply", reply))
		// 调用企业微信 API 主动发送消息
		h.sendWecomMessageViaAPI(rawUserID, enterpriseID, reply)
	}()
}

// sendWecomReply 发送企业微信回复（加密模式自动加密）
// 参数：toUser=用户 ID, fromUser=企业 ID（明文模式）/CorpID（加密模式）, content=回复内容，timestamp/nonce=请求参数
func (h *RobotHandler) sendWecomReply(c *gin.Context, toUser, fromUser, content, timestamp, nonce string) {
	// 加密模式：判断 EncodingAESKey 是否配置
	if h.config.Robots.Wecom.EncodingAESKey != "" {
		// 加密模式使用 CorpID 进行加密
		corpID := h.config.Robots.Wecom.CorpID
		if corpID == "" {
			h.logger.Warn("企业微信加密模式缺少 CorpID 配置")
			c.String(http.StatusOK, "")
			return
		}

		// 构造完整的明文 XML 回复（格式严格按企业微信文档要求）
		plainResp := fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)

		encrypted, err := wecomEncrypt(h.config.Robots.Wecom.EncodingAESKey, plainResp, corpID)
		if err != nil {
			h.logger.Warn("企业微信回复加密失败", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		// 使用请求中的 timestamp/nonce 生成签名（企业微信要求回复时使用与请求相同的 timestamp 和 nonce）
		msgSignature := h.signWecomRequest(h.config.Robots.Wecom.Token, timestamp, nonce, encrypted)

		h.logger.Debug("企业微信发送加密回复",
			zap.String("Encrypt", encrypted[:50]+"..."),
			zap.String("MsgSignature", msgSignature),
			zap.String("TimeStamp", timestamp),
			zap.String("Nonce", nonce))

		// 加密模式仅返回 4 个核心字段（企业微信官方要求）
		xmlResp := fmt.Sprintf(`<xml><Encrypt><![CDATA[%s]]></Encrypt><MsgSignature><![CDATA[%s]]></MsgSignature><TimeStamp><![CDATA[%s]]></TimeStamp><Nonce><![CDATA[%s]]></Nonce></xml>`, encrypted, msgSignature, timestamp, nonce)
		// also log the final response body so we can cross-check with the
		// network traffic or developer console
		h.logger.Debug("企业微信加密回复包", zap.String("xml", xmlResp))
		// for additional confidence, decrypt the payload ourselves and log it
		if dec, err2 := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, encrypted); err2 == nil {
			h.logger.Debug("企业微信加密回复解密检查", zap.String("plain", string(dec)))
		} else {
			h.logger.Warn("企业微信加密回复解密检查失败", zap.Error(err2))
		}

		// 使用 c.Writer.Write 直接写入响应，避免 c.String 的转义问题
		c.Writer.WriteHeader(http.StatusOK)
		// use text/xml as that's what WeCom examples show
		c.Writer.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = c.Writer.Write([]byte(xmlResp))
		h.logger.Debug("企业微信加密回复已发送")
		return
	}

	// 明文模式
	h.logger.Debug("企业微信发送明文回复", zap.String("ToUserName", toUser), zap.String("FromUserName", fromUser), zap.String("Content", content[:50]+"..."))

	// 手动构造 XML 响应（使用 CDATA 包裹所有字段，并包含 AgentID）
	xmlResp := fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)

	// log the exact plaintext response for debugging
	h.logger.Debug("企业微信明文回复包", zap.String("xml", xmlResp))

	// use text/xml as recommended by WeCom docs
	c.Header("Content-Type", "text/xml; charset=utf-8")
	c.String(http.StatusOK, xmlResp)
	h.logger.Debug("企业微信明文回复已发送")
}

// —————— 测试接口（需登录，用于验证机器人逻辑，无需钉钉/飞书客户端） ——————

// CreateRobotBindingCode creates a short-lived, single-use secret for the
// currently authenticated RBAC user. Only its hash is persisted.
func (h *RobotHandler) CreateRobotBindingCode(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok || strings.TrimSpace(session.UserID) == "" {
		respondError(c, http.StatusUnauthorized, "未授权访问", "unauthorized access")
		return
	}
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		respondError(c, http.StatusInternalServerError, "生成绑定码失败", "failed to generate binding code")
		return
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	code := raw[:4] + "-" + raw[4:]
	expiresAt := time.Now().Add(robotBindingCodeTTL)
	if err := h.db.CreateRobotBindingCode(session.UserID, hashRobotBindingCode(code), expiresAt); err != nil {
		h.logger.Warn("创建机器人绑定码失败", zap.String("user_id", session.UserID), zap.Error(err))
		respondError(c, http.StatusInternalServerError, "生成绑定码失败", "failed to generate binding code")
		return
	}
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{Category: "auth", Action: "robot_binding_code_create", Result: "success", ResourceType: "user", ResourceID: session.UserID, Message: "生成机器人一次性绑定码"})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"code": code, "expires_at": expiresAt.UTC().Format(time.RFC3339), "expires_in_seconds": int(robotBindingCodeTTL.Seconds()),
	})
}

func (h *RobotHandler) ListMyRobotBindings(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "未授权访问", "unauthorized access")
		return
	}
	bindings, err := h.db.ListRobotUserBindings(session.UserID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "获取机器人绑定失败", "failed to get robot binding")
		return
	}
	items := make([]gin.H, 0, len(bindings))
	for _, binding := range bindings {
		sum := sha256.Sum256([]byte(binding.ExternalUserID))
		items = append(items, gin.H{
			"id": binding.ID, "platform": binding.Platform, "external_user_hint": fmt.Sprintf("%x", sum[:4]),
			"enabled": binding.Enabled, "created_at": binding.CreatedAt, "updated_at": binding.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"bindings": items})
}

func (h *RobotHandler) DeleteMyRobotBinding(c *gin.Context) {
	session, ok := security.CurrentSession(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "未授权访问", "unauthorized access")
		return
	}
	if err := h.db.DeleteRobotUserBindingForUser(c.Param("id"), session.UserID); err != nil {
		respondError(c, http.StatusNotFound, "绑定不存在", "binding not found")
		return
	}
	if h.audit != nil {
		h.audit.Record(c, audit.Entry{Category: "auth", Action: "robot_binding_revoke", Result: "success", ResourceType: "robot_binding", ResourceID: c.Param("id"), Message: "撤销机器人平台账号绑定"})
	}
	c.Status(http.StatusNoContent)
}

// RobotTestRequest 模拟机器人消息请求
type RobotTestRequest struct {
	Platform string `json:"platform"` // 如 "dingtalk"、"lark"、"wecom"
	UserID   string `json:"user_id"`
	Text     string `json:"text"`
}

// HandleRobotTest 供本地验证：POST JSON { "platform", "user_id", "text" }，返回 { "reply": "..." }
func (h *RobotHandler) HandleRobotTest(c *gin.Context) {
	var req RobotTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "请求体需为 JSON，包含 platform、user_id、text", "request body must be JSON containing platform, user_id, and text")
		return
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = "test"
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "test_user"
	}
	reply := h.HandleMessage(platform, userID, req.Text)
	c.JSON(http.StatusOK, gin.H{"reply": reply})
}

// sendWecomMessageViaAPI 通过企业微信 API 主动发送消息（用于异步处理后的结果发送）
func (h *RobotHandler) sendWecomMessageViaAPI(toUser, toParty, content string) {
	if !h.config.Robots.Wecom.Enabled {
		return
	}

	secret := h.config.Robots.Wecom.Secret
	corpID := h.config.Robots.Wecom.CorpID
	agentID := h.config.Robots.Wecom.AgentID

	if secret == "" || corpID == "" {
		h.logger.Warn("企业微信主动 API 缺少 secret 或 corpID 配置")
		return
	}

	// 第 1 步：获取 access_token
	tokenURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", corpID, secret)
	resp, err := http.Get(tokenURL)
	if err != nil {
		h.logger.Warn("企业微信获取 token 失败", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		h.logger.Warn("企业微信 token 响应解析失败", zap.Error(err))
		return
	}
	if tokenResp.ErrCode != 0 {
		h.logger.Warn("企业微信 token 获取错误", zap.String("errmsg", tokenResp.ErrMsg), zap.Int("errcode", tokenResp.ErrCode))
		return
	}

	// 第 2 步：构造发送消息请求
	msgReq := map[string]interface{}{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": agentID,
		"text": map[string]interface{}{
			"content": content,
		},
	}

	msgBody, err := json.Marshal(msgReq)
	if err != nil {
		h.logger.Warn("企业微信消息序列化失败", zap.Error(err))
		return
	}

	// 第 3 步：发送消息
	sendURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", tokenResp.AccessToken)
	msgResp, err := http.Post(sendURL, "application/json", bytes.NewReader(msgBody))
	if err != nil {
		h.logger.Warn("企业微信主动发送消息失败", zap.Error(err))
		return
	}
	defer msgResp.Body.Close()

	var sendResp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		InvalidUser string `json:"invaliduser"`
		MsgID       string `json:"msgid"`
	}
	if err := json.NewDecoder(msgResp.Body).Decode(&sendResp); err != nil {
		h.logger.Warn("企业微信发送响应解析失败", zap.Error(err))
		return
	}

	if sendResp.ErrCode == 0 {
		h.logger.Debug("企业微信主动发送消息成功", zap.String("msgid", sendResp.MsgID))
	} else {
		h.logger.Warn("企业微信主动发送消息失败", zap.String("errmsg", sendResp.ErrMsg), zap.Int("errcode", sendResp.ErrCode), zap.String("invaliduser", sendResp.InvalidUser))
	}
}

// —————— 钉钉 ——————

// HandleDingtalkPOST 钉钉事件回调（流式接入等）；当前为占位，返回 200
func (h *RobotHandler) HandleDingtalkPOST(c *gin.Context) {
	if !h.config.Robots.Dingtalk.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	// 钉钉流式/事件回调格式需按官方文档解析并异步回复，此处仅返回 200
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// —————— 飞书 ——————

// HandleLarkPOST 飞书事件回调；当前为占位，返回 200；验证时需返回 challenge
func (h *RobotHandler) HandleLarkPOST(c *gin.Context) {
	if !h.config.Robots.Lark.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	var body struct {
		Challenge string `json:"challenge"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.Challenge != "" {
		c.JSON(http.StatusOK, gin.H{"challenge": body.Challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}
